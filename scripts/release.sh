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
compose_ingress_services=(
  productflow-web
  productflow-worker
  productflow-backend
)
frozen_compose_services=()
frozen_legacy_services=()
cutover_committed=0

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
    echo "[release] LEGACY_SYSTEMD_ACTION=skip，不主动停止 legacy user-level systemd 服务"
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
    if systemctl --user is-active --quiet "$service" 2>/dev/null; then
      frozen_legacy_services+=("$service")
    fi
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

freeze_compose_ingress() {
  local running_services=""
  running_services="$(docker compose ps --status running --services 2>/dev/null || true)"

  local service
  for service in "${compose_ingress_services[@]}"; do
    if grep -qx "$service" <<<"$running_services"; then
      frozen_compose_services+=("$service")
    fi
  done
  if (( ${#frozen_compose_services[@]} == 0 )); then
    echo "[release] 未检测到运行中的旧 Compose Web/Backend/Worker 入口"
    return 0
  fi

  echo "[release] 冻结新 Agent Turn 入口: ${frozen_compose_services[*]}"
  docker compose stop "${frozen_compose_services[@]}"
}

assert_agent_ingress_closed() {
  if curl -fsS --max-time 2 "http://127.0.0.1:${backend_port}/healthz" >/dev/null 2>&1; then
    echo "[release] backend 端口在冻结后仍可访问；拒绝在可能接收新 Agent Turn 时切换工具目录" >&2
    return 1
  fi
  echo "[release] 新 Agent Turn 入口已冻结"
}

restore_cutover_freeze() {
  if [[ "$cutover_committed" == "1" ]]; then
    return 0
  fi

  local restored=0
  if (( ${#frozen_compose_services[@]} > 0 )); then
    echo "[release] 发布前检查未完成，恢复原 Compose 服务: ${frozen_compose_services[*]}" >&2
    docker compose start "${frozen_compose_services[@]}" >&2 || true
    restored=1
  fi
  if (( ${#frozen_legacy_services[@]} > 0 )); then
    echo "[release] 发布前检查未完成，恢复原 legacy systemd 服务: ${frozen_legacy_services[*]}" >&2
    systemctl --user start "${frozen_legacy_services[@]}" >&2 || true
    restored=1
  fi
  if [[ "$restored" == "1" ]]; then
    echo "[release] 原服务恢复命令已执行；请确认旧 Turn 完成后重新发布" >&2
  fi
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
  echo "[release] 可用 docker compose ps 和 docker compose logs productflow-backend productflow-worker productflow-agent-service productflow-web 排查" >&2
  return 1
}

wait_for_agent_health() {
  local attempts="${1:-60}"
  local delay_seconds="${2:-2}"

  echo "[release] 等待 agent service /healthz（Compose 内部网络）"
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    local body=""
    if body="$(docker compose exec -T productflow-agent-service wget -qO- http://127.0.0.1:29284/healthz 2>/dev/null)" &&
      grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"' <<<"$body"; then
      echo "[release] agent service /healthz OK: $body"
      return 0
    fi
    sleep "$delay_seconds"
  done

  echo "[release] agent service health check failed after $((attempts * delay_seconds))s" >&2
  echo "[release] 可用 docker compose ps 和 docker compose logs productflow-agent-service 排查" >&2
  return 1
}

check_agent_catalog_cutover() {
  local running_services=""
  running_services="$(docker compose ps --status running --services 2>/dev/null || true)"
  if ! grep -qx "productflow-postgres" <<<"$running_services"; then
    echo "[release] 未检测到运行中的旧 Compose PostgreSQL；按首次部署处理，跳过 Agent Turn cutover 检查"
    return 0
  fi

  echo "[release] 检查旧 Agent Turn 是否允许切换到工具目录 v2"
  docker compose run --rm --no-deps productflow-backend python -m productflow_backend.agent_cutover
}

if [[ "$dry_run" == "1" ]]; then
  cat <<EOF
[release] dry-run 通过。实际 just release 将执行：
  1. docker compose build productflow-backend
  2. 停止旧 Compose Web/Backend/Worker 和 legacy systemd 入口，冻结新 Agent Turn
  3. 确认 backend 端口已关闭
  4. 若旧 Compose PostgreSQL 正在运行，使用候选镜像和旧 Agent service 复核所有未完成 Turn
  5. 门禁失败时恢复原服务；通过后 docker compose up -d --build --remove-orphans
  6. docker compose ps
  7. curl http://127.0.0.1:${backend_port}/healthz
  8. docker compose exec -T productflow-agent-service wget -qO- http://127.0.0.1:29284/healthz
  9. curl http://127.0.0.1:${web_port}/healthz
  10. curl http://127.0.0.1:${web_port}/api/healthz

[release] dry-run 不会删除 Docker volumes；实际 release 也不会执行 docker compose down -v。
EOF
  exit 0
fi

echo "[release] 构建候选 backend 镜像以执行发布前检查"
docker compose build productflow-backend

trap restore_cutover_freeze EXIT
freeze_compose_ingress
stop_legacy_services
assert_agent_ingress_closed
check_agent_catalog_cutover

echo "[release] 使用 Docker Compose 重建并启动生产自托管栈"
cutover_committed=1
docker compose up -d --build --remove-orphans

echo "[release] 当前 Compose 服务状态"
docker compose ps

wait_for_health "backend /healthz" "http://127.0.0.1:${backend_port}/healthz" '"status"[[:space:]]*:[[:space:]]*"ok"'
wait_for_agent_health
wait_for_health "web /healthz" "http://127.0.0.1:${web_port}/healthz" '^ok$'
wait_for_health "web proxy /api/healthz" "http://127.0.0.1:${web_port}/api/healthz" '"status"[[:space:]]*:[[:space:]]*"ok"'

echo "[release] 发布成功"
echo "[release] backend=http://127.0.0.1:${backend_port}"
echo "[release] web=http://127.0.0.1:${web_port}"
