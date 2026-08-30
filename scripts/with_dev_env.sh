#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if [[ -f "$repo_root/.env.dev" ]]; then
  set -a
  . "$repo_root/.env.dev"
  set +a
fi

# Local-only defaults keep the API, worker, and Agent service on one authenticated loopback contract.
export AGENT_SERVICE_INTERNAL_TOKEN="${AGENT_SERVICE_INTERNAL_TOKEN:-productflow-local-agent-internal-token-v1}"
export AGENT_SERVICE_BASE_URL="${AGENT_SERVICE_BASE_URL:-http://127.0.0.1:29284}"
export AGENT_LISTEN_ADDRESS="${AGENT_LISTEN_ADDRESS:-127.0.0.1:29284}"
export PRODUCTFLOW_INTERNAL_BASE_URL="${PRODUCTFLOW_INTERNAL_BASE_URL:-http://127.0.0.1:${APP_PORT:-29282}}"

# STORAGE_ROOT is relative to the repo root, not the process cwd (`go run -C go`).
if [[ -z "${STORAGE_ROOT:-}" ]]; then
  STORAGE_ROOT="./storage-dev"
fi
if [[ "${STORAGE_ROOT}" != /* ]]; then
  STORAGE_ROOT="${repo_root}/${STORAGE_ROOT#./}"
fi
export STORAGE_ROOT
mkdir -p "${STORAGE_ROOT}" "${STORAGE_ROOT}/logs"

exec python3 "$repo_root/scripts/exec_with_pdeathsig.py" "$@"
