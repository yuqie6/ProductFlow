#!/usr/bin/env bash
# Shared helpers for ProductFlow release backup / restore (B3).
# Sourced by release-backup.sh / release-restore.sh.
#
# Does not invent RPO/RTO/SLA. Does not claim D3 full drill or R6.

set -euo pipefail

# shellcheck source=release_common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release_common.sh"

# Services that accept or execute background work. Stopping them establishes a
# drain-style consistency window without `compose down` or volume deletion.
RELEASE_BACKUP_DRAIN_SERVICES=(
  productflow-go-worker
  productflow-go-dispatcher
  productflow-agent-service
)

release_backup_die() {
  echo "[release-backup] ERROR: $*" >&2
  exit 1
}

release_backup_warn() {
  echo "[release-backup] WARN: $*" >&2
}

release_backup_info() {
  echo "[release-backup] $*"
}

release_backup_require_cmd() {
  local cmd
  for cmd in "$@"; do
    command -v "$cmd" >/dev/null 2>&1 || release_backup_die "required command not found: $cmd"
  done
}

# Resolve install / compose directory.
# Prefer PRODUCTFLOW_RELEASE_DIR, else CWD when it looks like a release package,
# else repository root (dev tree with docker-compose.yml).
release_backup_resolve_dir() {
  local explicit="${PRODUCTFLOW_RELEASE_DIR:-}"
  local cwd
  cwd="$(pwd)"

  if [[ -n "$explicit" ]]; then
    [[ -d "$explicit" ]] || release_backup_die "PRODUCTFLOW_RELEASE_DIR is not a directory: $explicit"
    RELEASE_DIR="$(cd "$explicit" && pwd)"
  elif [[ -f "${cwd}/docker-compose.yml" && -f "${cwd}/.env.example" ]]; then
    RELEASE_DIR="$cwd"
  else
    RELEASE_DIR="$(release_repo_root)"
  fi

  [[ -f "${RELEASE_DIR}/docker-compose.yml" ]] || release_backup_die "docker-compose.yml not found under $RELEASE_DIR"
}

# Compose file list: release packages may stack prod-ports; source tree may too.
release_backup_compose_files() {
  local files=("-f" "${RELEASE_DIR}/docker-compose.yml")
  if [[ "${PRODUCTFLOW_USE_PROD_PORTS:-1}" == "1" && -f "${RELEASE_DIR}/docker-compose.prod-ports.yml" ]]; then
    files+=("-f" "${RELEASE_DIR}/docker-compose.prod-ports.yml")
  fi
  printf '%s\n' "${files[@]}"
}

release_backup_env_files() {
  local args=()
  if [[ -f "${RELEASE_DIR}/images.env" ]]; then
    args+=(--env-file "${RELEASE_DIR}/images.env")
  fi
  if [[ -f "${RELEASE_DIR}/.env" ]]; then
    args+=(--env-file "${RELEASE_DIR}/.env")
  elif [[ -f "${RELEASE_DIR}/.env.example" ]]; then
    release_backup_warn "no .env; using .env.example for compose variable resolution only"
    args+=(--env-file "${RELEASE_DIR}/.env.example")
  fi
  printf '%s\n' "${args[@]}"
}

# Build a docker compose invocation bound to RELEASE_DIR and COMPOSE_PROJECT_NAME.
release_backup_compose() {
  local project="${COMPOSE_PROJECT_NAME:?COMPOSE_PROJECT_NAME must be set}"
  local -a files envf
  mapfile -t files < <(release_backup_compose_files)
  mapfile -t envf < <(release_backup_env_files)
  (
    cd "$RELEASE_DIR"
    # shellcheck disable=SC2086
    docker compose -p "$project" "${files[@]}" "${envf[@]}" "$@"
  )
}

release_backup_guard_shared_project() {
  local project="$1"
  local action="$2"
  if [[ "$project" == "productflow" ]]; then
    if [[ "${PRODUCTFLOW_ALLOW_SHARED_PROJECT:-0}" != "1" ]]; then
      release_backup_die \
        "refusing ${action} on shared compose project 'productflow' (set PRODUCTFLOW_ALLOW_SHARED_PROJECT=1 to override; never use down -v on shared stacks)"
    fi
    release_backup_warn "PRODUCTFLOW_ALLOW_SHARED_PROJECT=1: operating on shared project 'productflow' for ${action}"
  fi
}

release_backup_read_dotenv() {
  local file="$1"
  local key="$2"
  [[ -f "$file" ]] || return 1
  awk -v key="$key" '
    /^[[:space:]]*(#|$)/ { next }
    {
      line = $0
      sub(/^[[:space:]]*export[[:space:]]+/, "", line)
      pattern = "^[[:space:]]*" key "[[:space:]]*="
      if (line ~ pattern) {
        sub(pattern "[[:space:]]*", "", line)
        sub(/[[:space:]]+#.*$/, "", line)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", line)
        if ((substr(line, 1, 1) == "\"" && substr(line, length(line), 1) == "\"") ||
            (substr(line, 1, 1) == "'"'"'" && substr(line, length(line), 1) == "'"'"'")) {
          line = substr(line, 2, length(line) - 2)
        }
        print line
        exit
      }
    }
  ' "$file"
}

release_backup_load_kv_file() {
  local file="$1"
  local key value
  [[ -f "$file" ]] || return 0
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]] || continue
    key="${line%%=*}"
    value="${line#*=}"
    case "$key" in
      PRODUCTFLOW_VERSION|PRODUCTFLOW_GIT_SHA|PRODUCTFLOW_GIT_SHA_SHORT|PRODUCTFLOW_IMAGE_TAG|\
      PRODUCTFLOW_REGISTRY|PRODUCTFLOW_GO_IMAGE|PRODUCTFLOW_AGENT_IMAGE|PRODUCTFLOW_WEB_IMAGE|\
      PRODUCTFLOW_POSTGRES_IMAGE|PRODUCTFLOW_REDIS_IMAGE)
        printf -v "$key" '%s' "$value"
        ;;
    esac
  done <"$file"
}

release_backup_load_identity() {
  # Prefer package VERSION / images.env; fall back to git when present.
  PRODUCTFLOW_GIT_SHA="${PRODUCTFLOW_GIT_SHA:-}"
  PRODUCTFLOW_IMAGE_TAG="${PRODUCTFLOW_IMAGE_TAG:-}"
  PRODUCTFLOW_GO_IMAGE="${PRODUCTFLOW_GO_IMAGE:-}"
  PRODUCTFLOW_AGENT_IMAGE="${PRODUCTFLOW_AGENT_IMAGE:-}"
  PRODUCTFLOW_WEB_IMAGE="${PRODUCTFLOW_WEB_IMAGE:-}"
  PRODUCTFLOW_VERSION="${PRODUCTFLOW_VERSION:-}"

  if [[ -f "${RELEASE_DIR}/VERSION" ]]; then
    release_backup_load_kv_file "${RELEASE_DIR}/VERSION"
  fi
  if [[ -f "${RELEASE_DIR}/images.env" ]]; then
    release_backup_load_kv_file "${RELEASE_DIR}/images.env"
  fi

  if [[ -z "${PRODUCTFLOW_GIT_SHA:-}" ]]; then
    if git -C "$RELEASE_DIR" rev-parse --verify HEAD >/dev/null 2>&1; then
      PRODUCTFLOW_GIT_SHA="$(git -C "$RELEASE_DIR" rev-parse HEAD)"
    elif git -C "$(release_repo_root)" rev-parse --verify HEAD >/dev/null 2>&1; then
      PRODUCTFLOW_GIT_SHA="$(git -C "$(release_repo_root)" rev-parse HEAD)"
    else
      PRODUCTFLOW_GIT_SHA="unknown"
    fi
  fi

  if [[ -z "${PRODUCTFLOW_IMAGE_TAG:-}" && -n "${PRODUCTFLOW_GO_IMAGE:-}" ]]; then
    PRODUCTFLOW_IMAGE_TAG="${PRODUCTFLOW_GO_IMAGE##*:}"
  fi
  PRODUCTFLOW_IMAGE_TAG="${PRODUCTFLOW_IMAGE_TAG:-unknown}"
  PRODUCTFLOW_VERSION="${PRODUCTFLOW_VERSION:-unknown}"
}

release_backup_volume_name() {
  local project="$1"
  local logical="$2"
  # Compose v2 named volume: <project>_<volume>
  printf '%s_%s\n' "$project" "$logical"
}

release_backup_storage_source() {
  # Echo either "host:<path>" or "volume:<docker-volume>".
  local project="$1"
  local host_path=""
  if [[ -f "${RELEASE_DIR}/.env" ]]; then
    host_path="$(release_backup_read_dotenv "${RELEASE_DIR}/.env" STORAGE_HOST_PATH || true)"
  fi
  host_path="${STORAGE_HOST_PATH:-$host_path}"
  if [[ -n "$host_path" ]]; then
    printf 'host:%s\n' "$host_path"
    return 0
  fi
  printf 'volume:%s\n' "$(release_backup_volume_name "$project" productflow-storage)"
}

release_backup_tar_from_volume() {
  local volume="$1"
  local archive="$2"
  local subdir="${3:-.}"
  docker volume inspect "$volume" >/dev/null 2>&1 || release_backup_die "docker volume not found: $volume"
  mkdir -p "$(dirname "$archive")"
  docker run --rm \
    -v "${volume}:/src:ro" \
    -v "$(dirname "$archive"):/out" \
    alpine:3.20 \
    tar -C /src -czf "/out/$(basename "$archive")" "$subdir"
}

release_backup_tar_from_host() {
  local host_path="$1"
  local archive="$2"
  [[ -d "$host_path" ]] || release_backup_die "storage host path not found: $host_path"
  mkdir -p "$(dirname "$archive")"
  tar -C "$host_path" -czf "$archive" .
}

release_backup_untar_to_volume() {
  local archive="$1"
  local volume="$2"
  [[ -f "$archive" ]] || release_backup_die "archive not found: $archive"
  docker volume create "$volume" >/dev/null
  docker run --rm \
    -v "${volume}:/dst" \
    -v "$(dirname "$archive"):/in:ro" \
    alpine:3.20 \
    sh -c "rm -rf /dst/* /dst/.[!.]* /dst/..?* 2>/dev/null; tar -C /dst -xzf \"/in/$(basename "$archive")\""
}

release_backup_untar_to_host() {
  local archive="$1"
  local host_path="$2"
  [[ -f "$archive" ]] || release_backup_die "archive not found: $archive"
  mkdir -p "$host_path"
  # Empty destination contents carefully; refuse non-empty without force.
  if [[ -n "$(ls -A "$host_path" 2>/dev/null || true)" && "${PRODUCTFLOW_RESTORE_FORCE_HOST_PATH:-0}" != "1" ]]; then
    release_backup_die "host path not empty: $host_path (set PRODUCTFLOW_RESTORE_FORCE_HOST_PATH=1 to overwrite)"
  fi
  find "$host_path" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
  tar -C "$host_path" -xzf "$archive"
}

release_backup_image_digest() {
  local image="$1"
  [[ -n "$image" && "$image" != "unknown" ]] || { echo "unknown"; return 0; }
  docker image inspect --format '{{index .RepoDigests 0}}' "$image" 2>/dev/null || echo "unresolved:${image}"
}
