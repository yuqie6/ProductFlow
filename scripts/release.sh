#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
dry_run="${DRY_RUN:-0}"
legacy_action="${LEGACY_SYSTEMD_ACTION:-stop}"

legacy_services=(
  productflow-backend.service
  productflow-worker.service
  productflow-web.service
)

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
            (substr(line, 1, 1) == "'"'"'" && substr(line, length(line), 1) == "'"'"'")) {
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

if [[ "$dry_run" == "1" ]]; then
  echo "[release] DRY_RUN=1，不会停止 legacy systemd 服务，也不会构建、启动或切换运行中的服务"
fi

echo "[release] 校验 Docker Compose 配置"
docker compose config --quiet

stop_legacy_services() {
  if [[ "$legacy_action" == "skip" ]]; then
    echo "[release] LEGACY_SYSTEMD_ACTION=skip，跳过 legacy user-level systemd 停止步骤"
    return 0
  fi

  if ! command -v systemctl >/dev/null 2>&1; then
    echo "[release] 未找到 systemctl，跳过 legacy user-level systemd 停止步骤"
    return 0
  fi

  echo "[release] 停止可能占用生产端口的 legacy user-level systemd 服务"
  local service
  for service in "${legacy_services[@]}"; do
    local stop_output=""
    if stop_output="$(systemctl --user stop "$service" 2>&1)"; then
      echo "[release] legacy systemd service stopped or already inactive: $service"
    else
      echo "[release] legacy systemd service 未停止（服务不存在、未运行或 user bus 不可用时可忽略）: $service" >&2
      if [[ -n "$stop_output" ]]; then
        echo "$stop_output" | sed 's/^/[release]   /' >&2
      fi
    fi
  done
}

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
  echo "[release] 可用 docker compose ps 和 docker compose logs productflow-backend productflow-worker productflow-web 排查" >&2
  return 1
}

if [[ "$dry_run" == "1" ]]; then
  cat <<EOF
[release] dry-run 通过。实际 just release 将执行：
  1. systemctl --user stop ${legacy_services[*]}   # 可用 LEGACY_SYSTEMD_ACTION=skip 跳过
  2. docker compose up -d --build --remove-orphans
  3. docker compose ps
  4. curl http://127.0.0.1:${backend_port}/healthz
  5. curl http://127.0.0.1:${web_port}/healthz
  6. curl http://127.0.0.1:${web_port}/api/healthz

[release] dry-run 不会删除 Docker volumes；实际 release 也不会执行 docker compose down -v。
EOF
  exit 0
fi

stop_legacy_services

echo "[release] 使用 Docker Compose 重建并启动生产自托管栈"
docker compose up -d --build --remove-orphans

echo "[release] 当前 Compose 服务状态"
docker compose ps

wait_for_health "backend /healthz" "http://127.0.0.1:${backend_port}/healthz" '"status"[[:space:]]*:[[:space:]]*"ok"'
wait_for_health "web /healthz" "http://127.0.0.1:${web_port}/healthz" '^ok$'
wait_for_health "web proxy /api/healthz" "http://127.0.0.1:${web_port}/api/healthz" '"status"[[:space:]]*:[[:space:]]*"ok"'

echo "[release] 发布成功"
echo "[release] backend=http://127.0.0.1:${backend_port}"
echo "[release] web=http://127.0.0.1:${web_port}"
