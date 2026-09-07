#!/usr/bin/env bash
# N→N+1 in-place upgrade for ProductFlow release installs (B5 / D4).
#
# Supported from the first stable release tag onward. Pre-stable may use an
# equivalent fixture (retag / same schema Apply) to exercise this script; that
# does not invent a second frozen stable pair.
#
# Minimal steps:
#   1. Precheck (project guard, pins, images, postgres healthy)
#   2. Consistent-point backup (release-backup.sh, drain)
#   3. Snapshot N pins; apply N+1 VERSION / images.env
#   4. Recreate productflow-migrate with the N+1 go image; fail-stop on non-zero
#   5. Full stack up + four healthz smokes
#
# On migrate failure: stop app-tier services, leave data plane, restore N pins
# from the pre-upgrade snapshot, and print rollback via release-restore.sh.
# Never introduces retired V1/v2 paths.
#
# Usage:
#   COMPOSE_PROJECT_NAME=pf-site \
#   PRODUCTFLOW_RELEASE_DIR=/opt/productflow \
#   PRODUCTFLOW_TARGET_RELEASE_DIR=/opt/productflow-n1 \
#   bash scripts/release-upgrade.sh
#
# Safety:
#   - Refuses compose project "productflow" unless PRODUCTFLOW_ALLOW_SHARED_PROJECT=1
#   - Never runs compose down / down -v / just dev-stop
#
# Does not claim R6, RPO, RTO, or SLA.

set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=release_backup_common.sh
source "${script_dir}/release_backup_common.sh"

release_backup_require_cmd docker tar sha256sum date

usage() {
  cat <<'EOF'
Usage: release-upgrade.sh

Required:
  COMPOSE_PROJECT_NAME              Compose project of the running N stack
  PRODUCTFLOW_TARGET_RELEASE_DIR    N+1 install package (VERSION + images.env)
    OR PRODUCTFLOW_TARGET_IMAGES_ENV  Path to an N+1 images.env (optional VERSION)

Optional:
  PRODUCTFLOW_RELEASE_DIR           Current N install dir (default: cwd / package / repo)
  BACKUP_OUT                        Parent for pre-upgrade backup (default: RELEASE_DIR/dist/backups)
  SKIP_BACKUP                       0 (default) | 1 — only for local fixture; not for production
  UPGRADE_SIMULATE_MIGRATE_FAIL     0 (default) | 1 — fail after pin swap without mutating schema
  PRODUCTFLOW_USE_PROD_PORTS        1 (default) when overlay exists
  PRODUCTFLOW_ALLOW_SHARED_PROJECT  must be 1 to upgrade project name "productflow"
  SMOKE_TIMEOUT_SECONDS             default 120

Outputs:
  Pre-upgrade pin snapshot under RELEASE_DIR/.productflow-upgrade-pre/
  Backup directory path printed on success/failure
  On migrate failure: app tier stopped; N pins restored; rollback commands printed
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

release_upgrade_die() {
  echo "[release-upgrade] ERROR: $*" >&2
  exit 1
}

release_upgrade_info() {
  echo "[release-upgrade] $*"
}

release_upgrade_warn() {
  echo "[release-upgrade] WARN: $*" >&2
}

COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-}"
[[ -n "$COMPOSE_PROJECT_NAME" ]] || release_upgrade_die "COMPOSE_PROJECT_NAME is required (see --help)"

TARGET_DIR="${PRODUCTFLOW_TARGET_RELEASE_DIR:-}"
TARGET_IMAGES_ENV="${PRODUCTFLOW_TARGET_IMAGES_ENV:-}"
[[ -n "$TARGET_DIR" || -n "$TARGET_IMAGES_ENV" ]] || \
  release_upgrade_die "set PRODUCTFLOW_TARGET_RELEASE_DIR or PRODUCTFLOW_TARGET_IMAGES_ENV"

release_backup_guard_shared_project "$COMPOSE_PROJECT_NAME" "upgrade"
release_backup_resolve_dir
release_backup_load_identity

FROM_TAG="${PRODUCTFLOW_IMAGE_TAG:-unknown}"
FROM_SHA="${PRODUCTFLOW_GIT_SHA:-unknown}"
FROM_GO="${PRODUCTFLOW_GO_IMAGE:-unknown}"

SKIP_BACKUP="${SKIP_BACKUP:-0}"
SIMULATE_FAIL="${UPGRADE_SIMULATE_MIGRATE_FAIL:-0}"
SMOKE_TIMEOUT_SECONDS="${SMOKE_TIMEOUT_SECONDS:-120}"

pre_dir="${RELEASE_DIR}/.productflow-upgrade-pre"
mkdir -p "$pre_dir"

# Resolve target pins.
TARGET_VERSION_FILE=""
TARGET_IMAGES_FILE=""
if [[ -n "$TARGET_DIR" ]]; then
  TARGET_DIR="$(cd "$TARGET_DIR" && pwd)"
  [[ -f "${TARGET_DIR}/images.env" || -f "${TARGET_DIR}/VERSION" ]] || \
    release_upgrade_die "target release missing images.env/VERSION: $TARGET_DIR"
  TARGET_IMAGES_FILE="${TARGET_DIR}/images.env"
  [[ -f "$TARGET_IMAGES_FILE" ]] || TARGET_IMAGES_FILE=""
  if [[ -f "${TARGET_DIR}/VERSION" ]]; then
    TARGET_VERSION_FILE="${TARGET_DIR}/VERSION"
    if [[ -z "$TARGET_IMAGES_FILE" ]]; then
      TARGET_IMAGES_FILE="${pre_dir}/target-images.env.generated"
      grep -E '^(PRODUCTFLOW_(GO|AGENT|WEB|POSTGRES|REDIS)_IMAGE)=' "$TARGET_VERSION_FILE" >"$TARGET_IMAGES_FILE"
    fi
  fi
fi
if [[ -n "$TARGET_IMAGES_ENV" ]]; then
  TARGET_IMAGES_FILE="$(cd "$(dirname "$TARGET_IMAGES_ENV")" && pwd)/$(basename "$TARGET_IMAGES_ENV")"
  [[ -f "$TARGET_IMAGES_FILE" ]] || release_upgrade_die "PRODUCTFLOW_TARGET_IMAGES_ENV not found: $TARGET_IMAGES_ENV"
fi
[[ -n "$TARGET_IMAGES_FILE" && -f "$TARGET_IMAGES_FILE" ]] || \
  release_upgrade_die "could not resolve target images.env"

# Load target identity for logging (do not overwrite FROM_* yet).
TO_TAG=""
TO_SHA=""
TO_GO=""
while IFS= read -r line || [[ -n "$line" ]]; do
  [[ "$line" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]] || continue
  key="${line%%=*}"
  value="${line#*=}"
  case "$key" in
    PRODUCTFLOW_IMAGE_TAG) TO_TAG="$value" ;;
    PRODUCTFLOW_GIT_SHA) TO_SHA="$value" ;;
    PRODUCTFLOW_GO_IMAGE) TO_GO="$value" ;;
  esac
done <"$TARGET_IMAGES_FILE"
if [[ -n "$TARGET_VERSION_FILE" ]]; then
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]] || continue
    key="${line%%=*}"
    value="${line#*=}"
    case "$key" in
      PRODUCTFLOW_IMAGE_TAG) TO_TAG="${TO_TAG:-$value}" ;;
      PRODUCTFLOW_GIT_SHA) TO_SHA="${TO_SHA:-$value}" ;;
      PRODUCTFLOW_GO_IMAGE) TO_GO="${TO_GO:-$value}" ;;
    esac
  done <"$TARGET_VERSION_FILE"
fi
TO_GO="$(awk -F= '/^PRODUCTFLOW_GO_IMAGE=/{print $2; exit}' "$TARGET_IMAGES_FILE")"
TO_TAG="${TO_TAG:-${TO_GO##*:}}"
TO_SHA="${TO_SHA:-unknown}"

release_upgrade_info "project=$COMPOSE_PROJECT_NAME"
release_upgrade_info "release_dir=$RELEASE_DIR"
release_upgrade_info "from tag=$FROM_TAG sha=$FROM_SHA go=$FROM_GO"
release_upgrade_info "to   tag=$TO_TAG sha=$TO_SHA go=$TO_GO"

# --- 1. Precheck ---
release_upgrade_info "precheck: compose config"
release_backup_compose config --quiet

release_upgrade_info "precheck: postgres healthy"
pg_ok=0
for ((i = 1; i <= 30; i++)); do
  if release_backup_compose exec -T productflow-postgres \
    pg_isready -U productflow -d productflow >/dev/null 2>&1; then
    pg_ok=1
    break
  fi
  sleep 2
done
[[ "$pg_ok" == "1" ]] || release_upgrade_die "postgres not healthy; refuse upgrade"

release_upgrade_info "precheck: target go image present ($TO_GO)"
docker image inspect "$TO_GO" >/dev/null 2>&1 || \
  release_upgrade_die "target go image missing locally: $TO_GO (pull or docker load first)"

for img_key in PRODUCTFLOW_AGENT_IMAGE PRODUCTFLOW_WEB_IMAGE; do
  img_val="$(awk -F= -v k="$img_key" '$1==k{print $2; exit}' "$TARGET_IMAGES_FILE")"
  if [[ -n "$img_val" ]]; then
    docker image inspect "$img_val" >/dev/null 2>&1 || \
      release_upgrade_die "target image missing locally: $img_val"
  fi
done

# Snapshot N pins before mutation.
release_upgrade_info "snapshot N pins -> $pre_dir"
[[ -f "${RELEASE_DIR}/images.env" ]] && cp -a "${RELEASE_DIR}/images.env" "${pre_dir}/images.env"
[[ -f "${RELEASE_DIR}/VERSION" ]] && cp -a "${RELEASE_DIR}/VERSION" "${pre_dir}/VERSION"
date -u +%Y-%m-%dT%H:%M:%SZ >"${pre_dir}/SNAPSHOT_AT"
printf '%s\n' "$FROM_TAG" >"${pre_dir}/FROM_IMAGE_TAG"
printf '%s\n' "$TO_TAG" >"${pre_dir}/TO_IMAGE_TAG"
cat >"${pre_dir}/MANIFEST" <<EOF
PRODUCTFLOW_UPGRADE_FORMAT=1
FROM_IMAGE_TAG=${FROM_TAG}
FROM_GIT_SHA=${FROM_SHA}
TO_IMAGE_TAG=${TO_TAG}
TO_GIT_SHA=${TO_SHA}
COMPOSE_PROJECT=${COMPOSE_PROJECT_NAME}
SNAPSHOT_AT=$(cat "${pre_dir}/SNAPSHOT_AT")
R6_CLAIM=false
D4_CLAIM=false
EOF

BACKUP_DIR_RESULT=""
if [[ "$SKIP_BACKUP" == "1" ]]; then
  release_upgrade_warn "SKIP_BACKUP=1: skipping release-backup (fixture only; not for production)"
else
  # --- 2. Backup ---
  release_upgrade_info "backup (CONSISTENCY_MODE=drain)"
  backup_parent="${BACKUP_OUT:-${RELEASE_DIR}/dist/backups}"
  mkdir -p "$backup_parent"
  COMPOSE_PROJECT_NAME="$COMPOSE_PROJECT_NAME" \
  PRODUCTFLOW_RELEASE_DIR="$RELEASE_DIR" \
  BACKUP_OUT="$backup_parent" \
  CONSISTENCY_MODE=drain \
  PRODUCTFLOW_USE_PROD_PORTS="${PRODUCTFLOW_USE_PROD_PORTS:-1}" \
  PRODUCTFLOW_ALLOW_SHARED_PROJECT="${PRODUCTFLOW_ALLOW_SHARED_PROJECT:-0}" \
  bash "${script_dir}/release-backup.sh"
  # Newest backup for this project.
  BACKUP_DIR_RESULT="$(
    find "$backup_parent" -maxdepth 1 -type d -name "productflow-backup-*-${COMPOSE_PROJECT_NAME}" \
      | sort | tail -n 1
  )"
  [[ -n "$BACKUP_DIR_RESULT" && -f "${BACKUP_DIR_RESULT}/MANIFEST" ]] || \
    release_upgrade_die "could not locate backup directory under $backup_parent"
  release_upgrade_info "backup_dir=$BACKUP_DIR_RESULT"
  # Keep pin snapshot inside backup for operator rollback kits.
  mkdir -p "${BACKUP_DIR_RESULT}/pre-upgrade-pin"
  cp -a "${pre_dir}/." "${BACKUP_DIR_RESULT}/pre-upgrade-pin/"
fi

restore_n_pins() {
  release_upgrade_info "restoring N pins from $pre_dir"
  if [[ -f "${pre_dir}/images.env" ]]; then
    cp -a "${pre_dir}/images.env" "${RELEASE_DIR}/images.env"
  fi
  if [[ -f "${pre_dir}/VERSION" ]]; then
    cp -a "${pre_dir}/VERSION" "${RELEASE_DIR}/VERSION"
  fi
}

print_rollback() {
  cat <<EOF

=== UPGRADE STOPPED (migrate failed or simulated) ===
App-tier services were stopped. Data plane (postgres/redis) left running.
N image pins restored under ${RELEASE_DIR} when a pre-upgrade snapshot existed.

Rollback to the pre-upgrade backup (preferred: NEW compose project):
  COMPOSE_PROJECT_NAME=<new-isolated-project> \\
  PRODUCTFLOW_RELEASE_DIR=<N-install-dir-with-N-pins> \\
  BACKUP_DIR=${BACKUP_DIR_RESULT:-<pre-upgrade-backup>} \\
  PRODUCTFLOW_RESTORE_OVERWRITE_ENV=1 \\
  bash scripts/release-restore.sh

Same-project rollback is allowed only when you accept dropping the current
logical DB contents and replacing volumes from that backup (still never
compose down -v on shared project name productflow).

Explicit non-claims: not R6; no RPO/RTO/SLA numbers.

EOF
}

fail_stop_after_migrate() {
  local reason="$1"
  release_upgrade_warn "$reason"
  release_upgrade_info "fail-stop: stopping app tier (not compose down / not down -v)"
  for svc in productflow-web productflow-go-api productflow-go-worker \
    productflow-go-dispatcher productflow-agent-service productflow-migrate; do
    if release_backup_compose ps -q "$svc" 2>/dev/null | grep -q .; then
      release_backup_compose stop "$svc" >/dev/null 2>&1 || true
    fi
  done
  restore_n_pins
  print_rollback
  release_upgrade_die "$reason"
}

# --- 3. Apply N+1 pins ---
release_upgrade_info "applying N+1 pins"
cp -a "$TARGET_IMAGES_FILE" "${RELEASE_DIR}/images.env"
if [[ -n "$TARGET_VERSION_FILE" ]]; then
  cp -a "$TARGET_VERSION_FILE" "${RELEASE_DIR}/VERSION"
elif [[ -f "${TARGET_DIR:-}/VERSION" ]]; then
  cp -a "${TARGET_DIR}/VERSION" "${RELEASE_DIR}/VERSION"
else
  # Synthesize a minimal VERSION from images.env + known TO_* fields.
  cat >"${RELEASE_DIR}/VERSION" <<EOF
# Synthesized by release-upgrade.sh (target package had no VERSION file)
PRODUCTFLOW_IMAGE_TAG=${TO_TAG}
PRODUCTFLOW_GIT_SHA=${TO_SHA}
PRODUCTFLOW_GO_IMAGE=${TO_GO}
$(grep -E '^PRODUCTFLOW_(AGENT|WEB|POSTGRES|REDIS)_IMAGE=' "$TARGET_IMAGES_FILE" || true)
PRODUCTFLOW_RELEASE_NOTES=applied-by-release-upgrade; not an R6 claim
EOF
fi

# Reload identity after pin swap for compose env files.
release_backup_load_identity

# Stop app tier before recreate (keep postgres/redis).
release_upgrade_info "stopping app tier before migrate recreate"
for svc in productflow-web productflow-go-api productflow-go-worker \
  productflow-go-dispatcher productflow-agent-service; do
  if release_backup_compose ps -q "$svc" 2>/dev/null | grep -q .; then
    release_backup_compose stop "$svc" >/dev/null 2>&1 || true
  fi
done

# --- 4. Migrate ---
if [[ "$SIMULATE_FAIL" == "1" ]]; then
  release_upgrade_warn "UPGRADE_SIMULATE_MIGRATE_FAIL=1: invoking failing migrate entrypoint"
  set +e
  release_backup_compose run --rm --no-deps --entrypoint /bin/sh \
    productflow-migrate -c 'echo "[release-upgrade] simulated migrate failure"; exit 1'
  sim_rc=$?
  set -e
  [[ "$sim_rc" -ne 0 ]] || release_upgrade_die "simulated migrate unexpectedly succeeded"
  fail_stop_after_migrate "simulated migrate failure (exit ${sim_rc})"
fi

release_upgrade_info "running productflow-migrate with N+1 image (compose run --rm)"
# Ensure postgres is up; migrate needs a healthy DB.
release_backup_compose up -d --no-deps productflow-postgres
for ((i = 1; i <= 30; i++)); do
  if release_backup_compose exec -T productflow-postgres \
    pg_isready -U productflow -d productflow >/dev/null 2>&1; then
    break
  fi
  sleep 2
  [[ $i -lt 30 ]] || release_upgrade_die "postgres not ready before migrate"
done

# Remove any prior one-shot container so identity stays clear in compose ps.
release_backup_compose rm -f productflow-migrate >/dev/null 2>&1 || true

set +e
release_backup_compose run --rm --no-deps productflow-migrate
migrate_exit=$?
set -e
release_upgrade_info "migrate exit=${migrate_exit}"

if [[ "${migrate_exit}" != "0" ]]; then
  fail_stop_after_migrate "productflow-migrate exited ${migrate_exit}"
fi

# --- 5. Full stack + smoke ---
release_upgrade_info "starting full stack on N+1 pins"
release_backup_compose up -d

app_port="$(release_backup_read_dotenv "${RELEASE_DIR}/.env" APP_HOST_PORT || true)"
web_port="$(release_backup_read_dotenv "${RELEASE_DIR}/.env" WEB_PORT || true)"
app_port="${app_port:-29280}"
web_port="${web_port:-29281}"

smoke_deadline=$((SECONDS + SMOKE_TIMEOUT_SECONDS))
smoke_ok=0
while (( SECONDS < smoke_deadline )); do
  if curl -fsS "http://127.0.0.1:${app_port}/healthz" >/dev/null 2>&1 \
    && curl -fsS "http://127.0.0.1:${web_port}/healthz" >/dev/null 2>&1 \
    && curl -fsS "http://127.0.0.1:${web_port}/api/healthz" >/dev/null 2>&1; then
    if release_backup_compose exec -T productflow-agent-service \
      node -e 'fetch("http://127.0.0.1:29284/healthz").then(r=>r.text()).then(t=>{if(!String(t).includes("productflow-pi"))process.exit(2)}).catch(()=>process.exit(1))' \
      >/dev/null 2>&1; then
      smoke_ok=1
      break
    fi
  fi
  sleep 3
done

if [[ "$smoke_ok" != "1" ]]; then
  release_upgrade_warn "smoke healthz incomplete within ${SMOKE_TIMEOUT_SECONDS}s"
  print_rollback
  release_upgrade_die "post-upgrade smoke failed"
fi

cat <<EOF

[release-upgrade] SUCCESS (main path)
  project=${COMPOSE_PROJECT_NAME}
  from=${FROM_TAG} -> to=${TO_TAG}
  backup_dir=${BACKUP_DIR_RESULT:-skipped}
  pre_pin_snapshot=${pre_dir}

=== Operator checklist (not auto-claimed as D4/R6 pass) ===
1. migrate Exit 0 (recorded above)
2. four healthz (API / web / web API proxy / Agent productflow-pi)
3. login + probe media still readable when you seeded them
4. keep the pre-upgrade backup until you accept the new pin

=== Explicit non-claims ===
- R6 not claimed
- No RPO / RTO / SLA numbers
- Retired V1/v2 upgrade paths remain unsupported
- Pre-stable equivalent fixtures do not invent a frozen stable version pair

EOF
