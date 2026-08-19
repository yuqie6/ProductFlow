#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
dry_run="${DRY_RUN:-0}"

cd "$repo_root"

read_dotenv_value() {
  local key="$1"
  if [[ ! -f "$repo_root/.env" ]]; then
    return 1
  fi

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
            (substr(line, 1, 1) == "'" && substr(line, length(line), 1) == "'")) {
          line = substr(line, 2, length(line) - 2)
        }
        print line
        exit
      }
    }
  ' "$repo_root/.env"
}

dotenv_app_host_port="$(read_dotenv_value APP_HOST_PORT || true)"
dotenv_app_port="$(read_dotenv_value APP_PORT || true)"
dotenv_web_port="$(read_dotenv_value WEB_PORT || true)"

backend_port="${APP_HOST_PORT:-${dotenv_app_host_port:-${APP_PORT:-${dotenv_app_port:-29280}}}}"
web_port="${WEB_PORT:-${dotenv_web_port:-29281}}"

echo "[release] repo_root=$repo_root"
echo "[release] backend_url=http://127.0.0.1:${backend_port}"
echo "[release] web_url=http://127.0.0.1:${web_port}"
echo "[release] 校验 Docker Compose 配置"
docker compose config --quiet

if [[ "$dry_run" == "1" ]]; then
  printf '%s\n' \
    "[release] dry-run 通过。实际发布将重建当前 Compose 服务并执行四项健康检查：" \
    "  backend /healthz" \
    "  agent service /healthz" \
    "  web /healthz" \
    "  web proxy /api/healthz" \
    "[release] 发布不会删除 Docker volumes。"
  exit 0
fi

wait_for_health() {
  local label="$1"
  local url="$2"
  local expected_pattern="$3"
  local attempts="${4:-60}"
  local delay_seconds="${5:-2}"

  echo "[release] 等待 ${label}: ${url}"
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    local body=""
    if body="$(curl -fsS --max-time 5 "$url" 2>/dev/null)" && grep -Eq "$expected_pattern" <<<"$body"; then
      echo "[release] ${label} OK: $body"
      return 0
    fi
    sleep "$delay_seconds"
  done

  echo "[release] ${label} health check failed after $((attempts * delay_seconds))s: ${url}" >&2
  echo "[release] 使用 docker compose ps 和 docker compose logs 排查" >&2
  return 1
}

wait_for_agent_health() {
  local attempts="${1:-60}"
  local delay_seconds="${2:-2}"

  echo "[release] 等待 agent service /healthz"
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    local body=""
    if body="$(docker compose exec -T productflow-agent-service node -e 'fetch("http://127.0.0.1:29284/healthz").then(async response => { const body = await response.text(); if (!response.ok) process.exit(1); process.stdout.write(body); }).catch(() => process.exit(1))' 2>/dev/null)" &&
      grep -Eq '"runtime"[[:space:]]*:[[:space:]]*"productflow-pi"' <<<"$body"; then
      echo "[release] agent service /healthz OK: $body"
      return 0
    fi
    sleep "$delay_seconds"
  done

  echo "[release] agent service health check failed after $((attempts * delay_seconds))s" >&2
  echo "[release] 使用 docker compose logs productflow-agent-service 排查" >&2
  return 1
}

echo "[release] 重建并启动当前 Compose 栈"
docker compose up -d --build --remove-orphans
docker compose ps

wait_for_health "backend /healthz" "http://127.0.0.1:${backend_port}/healthz" '"status"[[:space:]]*:[[:space:]]*"ok"'
wait_for_agent_health
wait_for_health "web /healthz" "http://127.0.0.1:${web_port}/healthz" '^ok$'
wait_for_health "web proxy /api/healthz" "http://127.0.0.1:${web_port}/api/healthz" '"status"[[:space:]]*:[[:space:]]*"ok"'

echo "[release] 发布成功"
echo "[release] backend=http://127.0.0.1:${backend_port}"
echo "[release] web=http://127.0.0.1:${web_port}"
