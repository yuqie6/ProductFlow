#!/usr/bin/env bash
# Consistent-point backup for ProductFlow (B3 / D2 checklist).
#
# Covers: PostgreSQL, storage media, agent /data, deploy .env;
# optional Redis AOF volume and agent-evolution traces.
# Records image/commit identity and whether in-flight jobs were drained.
#
# Usage (install dir or source tree):
#   COMPOSE_PROJECT_NAME=pf-site bash scripts/release-backup.sh
#   COMPOSE_PROJECT_NAME=pf-site BACKUP_OUT=/var/backups/pf bash scripts/release-backup.sh
#   INCLUDE_REDIS=1 CONSISTENCY_MODE=crash bash scripts/release-backup.sh
#
# Safety:
#   - Refuses compose project "productflow" unless PRODUCTFLOW_ALLOW_SHARED_PROJECT=1
#   - Never runs compose down / down -v / just dev-stop
#   - Writes .env into the backup directory only (operator secret; not for git)
#
# Does not claim D3 restore drill, R6, RPO, RTO, or SLA.

set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=release_backup_common.sh
source "${script_dir}/release_backup_common.sh"

release_backup_require_cmd docker tar sha256sum date

usage() {
  cat <<'EOF'
Usage: release-backup.sh

Required:
  COMPOSE_PROJECT_NAME   Compose project of the running (or reachable) stack

Optional:
  PRODUCTFLOW_RELEASE_DIR   Install / compose directory (default: cwd or repo root)
  BACKUP_OUT                Parent directory for backup folders (default: ./dist/backups)
  CONSISTENCY_MODE          drain (default) | crash
  INCLUDE_REDIS             0 (default) | 1
  INCLUDE_AGENT_TRACES      0 (default) | 1
  PRODUCTFLOW_USE_PROD_PORTS  1 (default) when prod-ports overlay exists
  PRODUCTFLOW_ALLOW_SHARED_PROJECT  must be 1 to backup project name "productflow"
  SKIP_RESTART_AFTER_DRAIN  0 (default) | 1  — leave drained services stopped

Output layout under BACKUP_OUT/productflow-backup-<UTC>-<project>/:
  MANIFEST                  identity, objects, drain flag, timestamps
  CHECKSUMS                 sha256 of payload files (not .env contents listed as secret)
  postgres/productflow.dump pg_dump -Fc
  storage/storage.tar.gz
  agent-data/agent-data.tar.gz
  env/.env                  copy of deploy secrets (mode 0600)
  redis/redis-data.tar.gz   if INCLUDE_REDIS=1
  agent-traces/...          if INCLUDE_AGENT_TRACES=1
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-}"
[[ -n "$COMPOSE_PROJECT_NAME" ]] || release_backup_die "COMPOSE_PROJECT_NAME is required (see --help)"

release_backup_guard_shared_project "$COMPOSE_PROJECT_NAME" "backup"
release_backup_resolve_dir
release_backup_load_identity

CONSISTENCY_MODE="${CONSISTENCY_MODE:-drain}"
case "$CONSISTENCY_MODE" in
  drain|crash) ;;
  *) release_backup_die "CONSISTENCY_MODE must be drain or crash (got: $CONSISTENCY_MODE)" ;;
esac

INCLUDE_REDIS="${INCLUDE_REDIS:-0}"
INCLUDE_AGENT_TRACES="${INCLUDE_AGENT_TRACES:-0}"
SKIP_RESTART_AFTER_DRAIN="${SKIP_RESTART_AFTER_DRAIN:-0}"

backup_root="${BACKUP_OUT:-${RELEASE_DIR}/dist/backups}"
mkdir -p "$backup_root"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_dir="${backup_root}/productflow-backup-${stamp}-${COMPOSE_PROJECT_NAME}"
mkdir -p \
  "${backup_dir}/postgres" \
  "${backup_dir}/storage" \
  "${backup_dir}/agent-data" \
  "${backup_dir}/env"

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
jobs_drained="false"
drained_services=()
restart_services=()

release_backup_info "release_dir=$RELEASE_DIR"
release_backup_info "compose_project=$COMPOSE_PROJECT_NAME"
release_backup_info "backup_dir=$backup_dir"
release_backup_info "consistency_mode=$CONSISTENCY_MODE"
release_backup_info "git_sha=$PRODUCTFLOW_GIT_SHA image_tag=$PRODUCTFLOW_IMAGE_TAG"

cleanup_restart() {
  if [[ "$CONSISTENCY_MODE" != "drain" ]]; then
    return 0
  fi
  if [[ "$SKIP_RESTART_AFTER_DRAIN" == "1" ]]; then
    release_backup_warn "SKIP_RESTART_AFTER_DRAIN=1: leaving drained services stopped"
    return 0
  fi
  if [[ ${#restart_services[@]} -eq 0 ]]; then
    return 0
  fi
  release_backup_info "restarting drained services: ${restart_services[*]}"
  release_backup_compose start "${restart_services[@]}" || \
    release_backup_warn "failed to restart some drained services; start them manually"
}

trap cleanup_restart EXIT

if [[ "$CONSISTENCY_MODE" == "drain" ]]; then
  release_backup_info "stopping workers that accept/execute jobs (not compose down)"
  for svc in "${RELEASE_BACKUP_DRAIN_SERVICES[@]}"; do
    # Only stop if the service exists in the project and is running.
    if release_backup_compose ps -q "$svc" 2>/dev/null | grep -q .; then
      state="$(release_backup_compose ps --status running -q "$svc" 2>/dev/null || true)"
      if [[ -n "$state" ]]; then
        release_backup_compose stop "$svc"
        drained_services+=("$svc")
        restart_services+=("$svc")
      fi
    else
      release_backup_warn "service not present or not created: $svc (skip)"
    fi
  done
  if [[ ${#drained_services[@]} -gt 0 ]]; then
    jobs_drained="true"
    release_backup_info "drained: ${drained_services[*]}"
  else
    release_backup_warn "no drain services were running; recording jobs_drained=false"
  fi
else
  release_backup_warn "CONSISTENCY_MODE=crash: hot backup; in-flight leases/recovery risk recorded in MANIFEST"
fi

# --- PostgreSQL ---
release_backup_info "dumping PostgreSQL (pg_dump -Fc)"
if ! release_backup_compose ps -q productflow-postgres 2>/dev/null | grep -q .; then
  release_backup_die "productflow-postgres is not running in project $COMPOSE_PROJECT_NAME"
fi
release_backup_compose exec -T productflow-postgres \
  pg_dump -U productflow -Fc --no-owner --no-acl productflow \
  >"${backup_dir}/postgres/productflow.dump"
[[ -s "${backup_dir}/postgres/productflow.dump" ]] || release_backup_die "postgres dump is empty"

# --- Storage ---
storage_src="$(release_backup_storage_source "$COMPOSE_PROJECT_NAME")"
release_backup_info "archiving storage ($storage_src)"
case "$storage_src" in
  host:*)
    release_backup_tar_from_host "${storage_src#host:}" "${backup_dir}/storage/storage.tar.gz"
    ;;
  volume:*)
    release_backup_tar_from_volume "${storage_src#volume:}" "${backup_dir}/storage/storage.tar.gz"
    ;;
  *)
    release_backup_die "unknown storage source: $storage_src"
    ;;
esac

# --- Agent /data ---
agent_vol="$(release_backup_volume_name "$COMPOSE_PROJECT_NAME" productflow-agent-data)"
release_backup_info "archiving agent /data ($agent_vol)"
release_backup_tar_from_volume "$agent_vol" "${backup_dir}/agent-data/agent-data.tar.gz"

# --- .env ---
if [[ -f "${RELEASE_DIR}/.env" ]]; then
  install -m 0600 "${RELEASE_DIR}/.env" "${backup_dir}/env/.env"
  env_present="true"
else
  release_backup_warn "no .env at ${RELEASE_DIR}/.env — writing placeholder note (startup secrets missing)"
  printf '%s\n' \
    "# MISSING: deploy .env was not found at backup time." \
    "# Restore cannot start Compose without operator-supplied secrets." \
    >"${backup_dir}/env/.env.MISSING"
  chmod 0600 "${backup_dir}/env/.env.MISSING"
  env_present="false"
fi

objects="postgres,storage,agent-data"
if [[ "$env_present" == "true" ]]; then
  objects+=",env"
else
  objects+=",env_missing"
fi

# --- Optional Redis ---
if [[ "$INCLUDE_REDIS" == "1" ]]; then
  redis_vol="$(release_backup_volume_name "$COMPOSE_PROJECT_NAME" productflow-redis-data)"
  release_backup_info "archiving Redis volume ($redis_vol)"
  mkdir -p "${backup_dir}/redis"
  release_backup_tar_from_volume "$redis_vol" "${backup_dir}/redis/redis-data.tar.gz"
  objects+=",redis"
fi

# --- Optional agent traces ---
if [[ "$INCLUDE_AGENT_TRACES" == "1" ]]; then
  traces_vol="$(release_backup_volume_name "$COMPOSE_PROJECT_NAME" productflow-agent-traces)"
  release_backup_info "archiving agent traces ($traces_vol)"
  mkdir -p "${backup_dir}/agent-traces"
  release_backup_tar_from_volume "$traces_vol" "${backup_dir}/agent-traces/agent-traces.tar.gz"
  objects+=",agent-traces"
fi

finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

go_digest="$(release_backup_image_digest "${PRODUCTFLOW_GO_IMAGE:-}")"
agent_digest="$(release_backup_image_digest "${PRODUCTFLOW_AGENT_IMAGE:-}")"
web_digest="$(release_backup_image_digest "${PRODUCTFLOW_WEB_IMAGE:-}")"

# Capture migrate image identity from running go image when available.
migrate_note="schema Apply via productflow-migrate using PRODUCTFLOW_GO_IMAGE; not AutoMigrate"

cat >"${backup_dir}/MANIFEST" <<EOF
# ProductFlow backup manifest (format 1). Operator-held; may contain secret paths.
# This file does not define RPO/RTO/SLA. D3 full restore drill is a separate task (B4).
PRODUCTFLOW_BACKUP_FORMAT=1
BACKUP_STARTED_AT=${started_at}
BACKUP_FINISHED_AT=${finished_at}
COMPOSE_PROJECT=${COMPOSE_PROJECT_NAME}
RELEASE_DIR=${RELEASE_DIR}
CONSISTENCY_MODE=${CONSISTENCY_MODE}
JOBS_DRAINED=${jobs_drained}
DRAINED_SERVICES=$(IFS=,; echo "${drained_services[*]}")
OBJECTS=${objects}
PRODUCTFLOW_VERSION=${PRODUCTFLOW_VERSION}
PRODUCTFLOW_GIT_SHA=${PRODUCTFLOW_GIT_SHA}
PRODUCTFLOW_IMAGE_TAG=${PRODUCTFLOW_IMAGE_TAG}
PRODUCTFLOW_GO_IMAGE=${PRODUCTFLOW_GO_IMAGE:-unknown}
PRODUCTFLOW_AGENT_IMAGE=${PRODUCTFLOW_AGENT_IMAGE:-unknown}
PRODUCTFLOW_WEB_IMAGE=${PRODUCTFLOW_WEB_IMAGE:-unknown}
PRODUCTFLOW_GO_IMAGE_DIGEST=${go_digest}
PRODUCTFLOW_AGENT_IMAGE_DIGEST=${agent_digest}
PRODUCTFLOW_WEB_IMAGE_DIGEST=${web_digest}
POSTGRES_DUMP=postgres/productflow.dump
STORAGE_ARCHIVE=storage/storage.tar.gz
STORAGE_SOURCE=${storage_src}
AGENT_DATA_ARCHIVE=agent-data/agent-data.tar.gz
ENV_FILE=$([ "$env_present" = "true" ] && echo env/.env || echo env/.env.MISSING)
ENV_PRESENT=${env_present}
INCLUDE_REDIS=${INCLUDE_REDIS}
INCLUDE_AGENT_TRACES=${INCLUDE_AGENT_TRACES}
MIGRATE_NOTE=${migrate_note}
IN_FLIGHT_JOB_NOTE=Holding Graph/ImageSession/Agent/Delivery/LocalEdit leases converge via existing recovery after restore; unprovable provider results stay unknown and are not auto-replayed as failure.
D2_CHECKLIST=objects include postgres+storage+agent-data+env (or env_missing); commit/digest recorded; jobs_drained flag recorded
D3_CLAIM=false
R6_CLAIM=false
EOF

# Checksums hash payloads for integrity; do not print .env contents to the terminal.
{
  echo "# sha256 of backup payloads under $(basename "$backup_dir")"
  (
    cd "$backup_dir"
    files=(postgres/productflow.dump storage/storage.tar.gz agent-data/agent-data.tar.gz MANIFEST)
    [[ -f env/.env ]] && files+=(env/.env)
    [[ -f env/.env.MISSING ]] && files+=(env/.env.MISSING)
    [[ -f redis/redis-data.tar.gz ]] && files+=(redis/redis-data.tar.gz)
    [[ -f agent-traces/agent-traces.tar.gz ]] && files+=(agent-traces/agent-traces.tar.gz)
    sha256sum "${files[@]}"
  )
} >"${backup_dir}/CHECKSUMS"

release_backup_info "wrote MANIFEST and CHECKSUMS"
release_backup_info "backup complete: $backup_dir"
release_backup_info "D2 objects=${objects} jobs_drained=${jobs_drained} mode=${CONSISTENCY_MODE}"
release_backup_info "not claiming D3 full drill or R6"
