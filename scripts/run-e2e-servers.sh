#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_ROOT="${PRODUCTFLOW_E2E_TMP:-$(mktemp -d /tmp/productflow-e2e.XXXXXX)}"
BACKEND_PORT="${PRODUCTFLOW_E2E_BACKEND_PORT:-29282}"
WEB_PORT="${PRODUCTFLOW_E2E_WEB_PORT:-29283}"

export ADMIN_ACCESS_KEY="${ADMIN_ACCESS_KEY:-super-secret-admin-key}"
export SETTINGS_ACCESS_TOKEN="${SETTINGS_ACCESS_TOKEN:-super-secret-settings-token}"
export SESSION_SECRET="${SESSION_SECRET:-super-secret-session-key-123}"
export SESSION_COOKIE_SECURE="false"
export DATABASE_URL="sqlite:///${TMP_ROOT}/productflow-e2e.db"
export REDIS_URL="redis://localhost:6379/9"
export STORAGE_ROOT="${TMP_ROOT}/storage"
export LOG_DIR="${TMP_ROOT}/logs"
export TEXT_PROVIDER_KIND="mock"
export IMAGE_PROVIDER_KIND="mock"
export POSTER_GENERATION_MODE="template"
export LAUNCH_KIT_INLINE_GENERATION="true"
export BACKEND_CORS_ORIGINS="http://127.0.0.1:${WEB_PORT},http://localhost:${WEB_PORT}"
export WEB_PORT
export VITE_DEV_PROXY_TARGET="http://127.0.0.1:${BACKEND_PORT}"

mkdir -p "${STORAGE_ROOT}" "${LOG_DIR}"

cd "${ROOT_DIR}/backend"
"${ROOT_DIR}/.venv/bin/python" -m alembic upgrade head
"${ROOT_DIR}/.venv/bin/python" -m uvicorn productflow_backend.presentation.api:create_app \
  --factory \
  --host 127.0.0.1 \
  --port "${BACKEND_PORT}" &
BACKEND_PID=$!

cleanup() {
  kill "${BACKEND_PID}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cd "${ROOT_DIR}/web"
exec corepack pnpm@10.18.3 dev --host 127.0.0.1
