#!/usr/bin/env bash
set -euo pipefail

# Stop leftover ProductFlow app processes from a previous `just dev`.
# PostgreSQL and Redis containers are left running.
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
self="$$"
parent="${PPID:-0}"

is_repo_dev_process() {
  local pid="$1"
  local cwd cmd
  cwd="$(readlink "/proc/${pid}/cwd" 2>/dev/null || true)"
  if [[ "${cwd}" != "${repo_root}" && "${cwd}" != "${repo_root}/"* ]]; then
    return 1
  fi
  cmd="$(tr '\0' ' ' < "/proc/${pid}/cmdline" 2>/dev/null || true)"
  case "${cmd}" in
    *productflow_backend.workers* | *productflow_backend.main:app* | *productflow_backend.commands.run_async_dispatcher* | *cmd/productflow-api* | *cmd/productflow-worker* | *cmd/productflow-dispatcher* | *productflow-api* | *productflow-worker* | *productflow-dispatcher* | *"pnpm --dir agent-service"* | *"pnpm --dir web"* | *" --dir web dev"* | *"/node_modules/.bin/vite"*)
      return 0
      ;;
  esac
  return 1
}

pids=()
for proc in /proc/[0-9]*; do
  pid="${proc#/proc/}"
  if [[ "${pid}" == "${self}" || "${pid}" == "${parent}" ]]; then
    continue
  fi
  if is_repo_dev_process "${pid}"; then
    pids+=("${pid}")
  fi
done

if [[ "${#pids[@]}" -eq 0 ]]; then
  exit 0
fi

echo "stopping leftover ProductFlow app processes: ${pids[*]}" >&2
kill -TERM "${pids[@]}" 2>/dev/null || true
for _ in 1 2 3 4 5 6 7 8 9 10; do
  remaining=()
  for pid in "${pids[@]}"; do
    if [[ -d "/proc/${pid}" ]]; then
      remaining+=("${pid}")
    fi
  done
  if [[ "${#remaining[@]}" -eq 0 ]]; then
    exit 0
  fi
  sleep 0.2
done

echo "force-killing leftover ProductFlow app processes: ${remaining[*]}" >&2
kill -KILL "${remaining[@]}" 2>/dev/null || true
