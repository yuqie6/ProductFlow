#!/usr/bin/env bash
# Restore a ProductFlow backup into a new compose project / directory (B3 runbook).
#
# Restores: PostgreSQL dump, storage media, agent /data, deploy .env;
# optional Redis and agent traces when present in the backup.
#
# Usage:
#   COMPOSE_PROJECT_NAME=pf-restore-20260907 \
#   PRODUCTFLOW_RELEASE_DIR=/opt/productflow \
#   BACKUP_DIR=/var/backups/productflow-backup-... \
#   bash scripts/release-restore.sh
#
# Safety:
#   - Refuses target project "productflow" unless PRODUCTFLOW_ALLOW_SHARED_PROJECT=1
#   - Never runs compose down -v on shared stacks; never calls just dev-stop
#   - Default: start postgres (+ redis if needed), restore data, then full up
#
# Prints D3 business assertion checklist for operators / B4 drill.
# Does not claim D3 pass, R6, RPO, RTO, or SLA.

set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=release_backup_common.sh
source "${script_dir}/release_backup_common.sh"

release_backup_require_cmd docker tar sha256sum date

usage() {
  cat <<'EOF'
Usage: release-restore.sh

Required:
  COMPOSE_PROJECT_NAME   NEW compose project for the restore target
  BACKUP_DIR             Path to a productflow-backup-* directory with MANIFEST

Optional:
  PRODUCTFLOW_RELEASE_DIR   Install dir that will receive env/.env and run compose
  SKIP_COMPOSE_UP           0 (default) | 1 — restore volumes/dump only
  VERIFY_CHECKSUMS          1 (default) | 0
  INCLUDE_REDIS             auto from MANIFEST unless overridden 0|1
  PRODUCTFLOW_USE_PROD_PORTS  1 (default) when overlay exists
  PRODUCTFLOW_ALLOW_SHARED_PROJECT  must be 1 to restore into project "productflow"
  PRODUCTFLOW_RESTORE_FORCE_HOST_PATH  1 to overwrite non-empty STORAGE_HOST_PATH

Steps performed:
  1. Verify MANIFEST / optional CHECKSUMS
  2. Copy env/.env into RELEASE_DIR (mode 0600)
  3. Ensure postgres (and redis if restoring redis) are up
  4. Drop/recreate logical DB and pg_restore
  5. Restore storage + agent-data volumes (and optional redis/traces)
  6. docker compose up -d (migrate runs via depends_on)
  7. Print health + D3 assertion checklist (operator / B4 evidence)
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-}"
BACKUP_DIR="${BACKUP_DIR:-}"
[[ -n "$COMPOSE_PROJECT_NAME" ]] || release_backup_die "COMPOSE_PROJECT_NAME is required"
[[ -n "$BACKUP_DIR" ]] || release_backup_die "BACKUP_DIR is required"
BACKUP_DIR="$(cd "$BACKUP_DIR" && pwd)"
[[ -f "${BACKUP_DIR}/MANIFEST" ]] || release_backup_die "MANIFEST not found in $BACKUP_DIR"

release_backup_guard_shared_project "$COMPOSE_PROJECT_NAME" "restore"
release_backup_resolve_dir
release_backup_load_identity

SKIP_COMPOSE_UP="${SKIP_COMPOSE_UP:-0}"
VERIFY_CHECKSUMS="${VERIFY_CHECKSUMS:-1}"

manifest_get() {
  local key="$1"
  awk -F= -v key="$key" '
    /^[[:space:]]*#/ { next }
    $1 == key { sub(/^[^=]*=/, ""); print; exit }
  ' "${BACKUP_DIR}/MANIFEST"
}

backup_format="$(manifest_get PRODUCTFLOW_BACKUP_FORMAT)"
[[ "$backup_format" == "1" ]] || release_backup_die "unsupported PRODUCTFLOW_BACKUP_FORMAT: ${backup_format:-empty}"

objects="$(manifest_get OBJECTS)"
jobs_drained="$(manifest_get JOBS_DRAINED)"
consistency_mode="$(manifest_get CONSISTENCY_MODE)"
src_sha="$(manifest_get PRODUCTFLOW_GIT_SHA)"
src_tag="$(manifest_get PRODUCTFLOW_IMAGE_TAG)"
env_present="$(manifest_get ENV_PRESENT)"
manifest_include_redis="$(manifest_get INCLUDE_REDIS)"
manifest_include_traces="$(manifest_get INCLUDE_AGENT_TRACES)"
storage_source_recorded="$(manifest_get STORAGE_SOURCE)"

INCLUDE_REDIS="${INCLUDE_REDIS:-$manifest_include_redis}"
INCLUDE_AGENT_TRACES="${INCLUDE_AGENT_TRACES:-$manifest_include_traces}"
INCLUDE_REDIS="${INCLUDE_REDIS:-0}"
INCLUDE_AGENT_TRACES="${INCLUDE_AGENT_TRACES:-0}"

release_backup_info "backup_dir=$BACKUP_DIR"
release_backup_info "target_project=$COMPOSE_PROJECT_NAME"
release_backup_info "release_dir=$RELEASE_DIR"
release_backup_info "backup_identity sha=$src_sha tag=$src_tag drained=$jobs_drained mode=$consistency_mode"
release_backup_info "objects=$objects"

if [[ "$VERIFY_CHECKSUMS" == "1" && -f "${BACKUP_DIR}/CHECKSUMS" ]]; then
  release_backup_info "verifying CHECKSUMS"
  (
    cd "$BACKUP_DIR"
    # MANIFEST is listed in CHECKSUMS; verifying after we only read it is fine.
    sha256sum -c CHECKSUMS
  )
fi

# --- .env into install dir ---
if [[ -f "${BACKUP_DIR}/env/.env" ]]; then
  if [[ -f "${RELEASE_DIR}/.env" && "${PRODUCTFLOW_RESTORE_OVERWRITE_ENV:-0}" != "1" ]]; then
    release_backup_die "${RELEASE_DIR}/.env already exists (set PRODUCTFLOW_RESTORE_OVERWRITE_ENV=1 to replace from backup)"
  fi
  install -m 0600 "${BACKUP_DIR}/env/.env" "${RELEASE_DIR}/.env"
  release_backup_info "restored .env -> ${RELEASE_DIR}/.env (mode 0600)"
elif [[ "$env_present" == "false" || -f "${BACKUP_DIR}/env/.env.MISSING" ]]; then
  release_backup_warn "backup has no .env; ensure ${RELEASE_DIR}/.env exists before compose up"
  [[ -f "${RELEASE_DIR}/.env" ]] || release_backup_die "missing ${RELEASE_DIR}/.env and backup env"
else
  release_backup_die "backup env/.env not found"
fi

# Ensure images.env exists for release packages.
if [[ ! -f "${RELEASE_DIR}/images.env" && -f "${RELEASE_DIR}/VERSION" ]]; then
  grep -E '^(PRODUCTFLOW_(GO|AGENT|WEB|POSTGRES|REDIS)_IMAGE)=' "${RELEASE_DIR}/VERSION" >"${RELEASE_DIR}/images.env" || true
fi

# Stop app services that may hold volume mounts before rewriting volumes.
# Do not compose down -v; leave named volumes intact for rewrite helpers.
release_backup_info "stopping application services that mount storage/agent data (not down -v)"
for svc in productflow-web productflow-go-api productflow-go-worker \
  productflow-go-dispatcher productflow-agent-service productflow-migrate; do
  if release_backup_compose ps -q "$svc" 2>/dev/null | grep -q .; then
    release_backup_compose stop "$svc" >/dev/null 2>&1 || true
  fi
done

# --- Bring up data plane first ---
release_backup_info "starting postgres for restore"
data_services=(productflow-postgres)
if [[ "$INCLUDE_REDIS" == "1" ]]; then
  data_services+=(productflow-redis)
fi
release_backup_compose up -d --no-deps "${data_services[@]}"

# Wait for postgres healthy.
for ((i = 1; i <= 60; i++)); do
  if release_backup_compose exec -T productflow-postgres pg_isready -U productflow -d productflow >/dev/null 2>&1; then
    break
  fi
  sleep 2
  if [[ $i -eq 60 ]]; then
    release_backup_die "postgres not ready"
  fi
done

# --- Restore PostgreSQL ---
dump="${BACKUP_DIR}/postgres/productflow.dump"
[[ -s "$dump" ]] || release_backup_die "missing postgres dump: $dump"
release_backup_info "restoring PostgreSQL from dump"
# Terminate sessions, drop and recreate DB, then pg_restore from stdin (-).
release_backup_compose exec -T productflow-postgres \
  psql -U productflow -d postgres -v ON_ERROR_STOP=1 <<'SQL'
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = 'productflow' AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS productflow;
CREATE DATABASE productflow OWNER productflow;
SQL

set +e
# Custom-format dump via stdin (do not pass "-" — docker exec treats it as a path).
release_backup_compose exec -T productflow-postgres \
  pg_restore -U productflow -d productflow --no-owner --no-acl \
  <"$dump"
pg_restore_rc=$?
set -e
# pg_restore uses exit 1 for non-fatal warnings; exit >=2 is hard failure.
if [[ "$pg_restore_rc" -ge 2 ]]; then
  release_backup_die "pg_restore failed with exit ${pg_restore_rc}"
elif [[ "$pg_restore_rc" -eq 1 ]]; then
  release_backup_warn "pg_restore reported warnings (exit 1); verifying table count"
fi

rel_count="$(release_backup_compose exec -T productflow-postgres \
  psql -U productflow -d productflow -Atc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';" \
  | tr -d '[:space:]')"
[[ "${rel_count:-0}" -gt 0 ]] || release_backup_die "postgres restore produced zero public tables"

# --- Storage ---
storage_archive="${BACKUP_DIR}/storage/storage.tar.gz"
[[ -f "$storage_archive" ]] || release_backup_die "missing storage archive"
target_storage="$(release_backup_storage_source "$COMPOSE_PROJECT_NAME")"
release_backup_info "restoring storage -> $target_storage (backup recorded $storage_source_recorded)"
case "$target_storage" in
  host:*)
    release_backup_untar_to_host "$storage_archive" "${target_storage#host:}"
    ;;
  volume:*)
    release_backup_untar_to_volume "$storage_archive" "${target_storage#volume:}"
    ;;
  *)
    release_backup_die "unknown storage target: $target_storage"
    ;;
esac

# --- Agent data ---
agent_archive="${BACKUP_DIR}/agent-data/agent-data.tar.gz"
[[ -f "$agent_archive" ]] || release_backup_die "missing agent-data archive"
agent_vol="$(release_backup_volume_name "$COMPOSE_PROJECT_NAME" productflow-agent-data)"
release_backup_info "restoring agent /data -> $agent_vol"
release_backup_untar_to_volume "$agent_archive" "$agent_vol"

# --- Optional Redis ---
if [[ "$INCLUDE_REDIS" == "1" ]]; then
  redis_archive="${BACKUP_DIR}/redis/redis-data.tar.gz"
  if [[ -f "$redis_archive" ]]; then
    # Stop redis before replacing volume contents.
    release_backup_compose stop productflow-redis || true
    redis_vol="$(release_backup_volume_name "$COMPOSE_PROJECT_NAME" productflow-redis-data)"
    release_backup_info "restoring redis -> $redis_vol"
    release_backup_untar_to_volume "$redis_archive" "$redis_vol"
    release_backup_compose start productflow-redis || release_backup_compose up -d productflow-redis
  else
    release_backup_warn "INCLUDE_REDIS=1 but redis archive missing; continuing with empty/broker redis"
  fi
fi

# --- Optional traces ---
if [[ "$INCLUDE_AGENT_TRACES" == "1" ]]; then
  traces_archive="${BACKUP_DIR}/agent-traces/agent-traces.tar.gz"
  if [[ -f "$traces_archive" ]]; then
    traces_vol="$(release_backup_volume_name "$COMPOSE_PROJECT_NAME" productflow-agent-traces)"
    release_backup_info "restoring agent traces -> $traces_vol"
    release_backup_untar_to_volume "$traces_archive" "$traces_vol"
  else
    release_backup_warn "INCLUDE_AGENT_TRACES=1 but archive missing"
  fi
fi

if [[ "$SKIP_COMPOSE_UP" == "1" ]]; then
  release_backup_info "SKIP_COMPOSE_UP=1: data restored; stack not fully started"
else
  release_backup_info "starting full stack (migrate via depends_on)"
  release_backup_compose up -d
fi

cat <<EOF

[release-restore] restore data plane finished for project=${COMPOSE_PROJECT_NAME}
[release-restore] backup jobs_drained=${jobs_drained} consistency_mode=${consistency_mode}
[release-restore] source identity sha=${src_sha} tag=${src_tag}

=== D3 assertion checklist (operator / B4 evidence; this script does NOT claim pass) ===
1. migrate completed successfully (compose ps productflow-migrate Exit 0)
2. health: API /healthz, web /healthz, web /api/healthz, agent runtime=productflow-pi
3. login with the restored ADMIN_ACCESS_KEY from .env
4. probe media download is not missing_file / verification_status=missing
5. provider profile has_api_key true when backup contained keys (PG plaintext)
6. async_dispatches / task rows match restore policy; Redis emptiness is OK (re-dispatch from PG)
7. agent /healthz and publisher/session materials under /data are explainable
8. unknown provider outcomes remain unknown (not auto-failed)

=== Explicit non-claims ===
- D3 full isolated drill: not claimed by this run unless you record evidence (B4)
- R6 / RPO / RTO / SLA: not claimed

EOF
