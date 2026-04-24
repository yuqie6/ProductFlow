#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
release_root="${RELEASE_ROOT:-$repo_root/.release}"
release_id="${RELEASE_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"
release_dir="$release_root/releases/$release_id"
current_link="$release_root/current"
dry_run="${DRY_RUN:-0}"

previous_current=""
if [[ -L "$current_link" || -e "$current_link" ]]; then
  previous_current="$(readlink -f "$current_link" || true)"
fi

switched=0
rollback() {
  local exit_code=$?
  if [[ $exit_code -eq 0 ]]; then
    return
  fi

  echo "[release] 失败，exit_code=$exit_code" >&2
  if [[ "$switched" == "1" && -n "$previous_current" && -d "$previous_current" && "$dry_run" != "1" ]]; then
    echo "[release] 回滚 current -> $previous_current" >&2
    ln -sfn "$previous_current" "$current_link"
    systemctl --user restart productflow-backend.service productflow-worker.service productflow-web.service || true
  fi
}
trap rollback EXIT

cd "$repo_root"

if [[ -f "$repo_root/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  . "$repo_root/.env"
  set +a
fi

backend_port="${APP_PORT:-29280}"
web_port="${WEB_PORT:-29281}"
backend_python="${BACKEND_PYTHON:-$repo_root/backend/.venv/bin/python}"

echo "[release] repo_root=$repo_root"
echo "[release] release_dir=$release_dir"
if [[ "$dry_run" == "1" ]]; then
  echo "[release] DRY_RUN=1，不会切换 current 或重启服务"
fi

pnpm --dir web build

mkdir -p "$release_dir"
tar \
  --exclude='.git' \
  --exclude='.release' \
  --exclude='.env' \
  --exclude='.env.dev' \
  --exclude='web/.env' \
  --exclude='.trellis/tasks' \
  --exclude='.trellis/workspace' \
  --exclude='.trellis/.current-task' \
  --exclude='.trellis/.developer' \
  --exclude='web/node_modules' \
  --exclude='backend/.venv' \
  --exclude='backend/.pytest_cache' \
  --exclude='backend/.ruff_cache' \
  --exclude='backend/.mypy_cache' \
  --exclude='backend/storage' \
  --exclude='backend/backend/storage' \
  --exclude='backend/storage-dev' \
  --exclude='backend/backend/storage-dev' \
  --exclude='web/.vite' \
  --exclude='web/.cache' \
  --exclude='.debug' \
  --exclude='.ruff_cache' \
  --exclude='*/__pycache__' \
  --exclude='*.pyc' \
  -cf - . | tar -C "$release_dir" -xf -

cat > "$release_dir/RELEASE_INFO.txt" <<EOF
release_id=$release_id
created_at_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)
source_repo=$repo_root
source_branch=$(git branch --show-current 2>/dev/null || true)
source_head=$(git rev-parse HEAD 2>/dev/null || true)
source_head_short=$(git rev-parse --short HEAD 2>/dev/null || true)
source_dirty=$(if [[ -n "$(git status --short 2>/dev/null || true)" ]]; then echo yes; else echo no; fi)
previous_current=$previous_current
EOF

if [[ "$dry_run" == "1" ]]; then
  echo "[release] 已生成 dry-run 快照：$release_dir"
  exit 0
fi

if [[ ! -x "$backend_python" ]]; then
  echo "[release] 找不到可执行的后端 Python：$backend_python" >&2
  echo "[release] 请先运行 just backend-install，或通过 BACKEND_PYTHON 指定解释器" >&2
  exit 1
fi

echo "[release] 执行数据库迁移"
(
  cd "$release_dir/backend"
  PYTHONPATH="$release_dir/backend/src${PYTHONPATH:+:$PYTHONPATH}" \
    "$backend_python" -m alembic -c alembic.ini upgrade head
)

ln -sfn "$release_dir" "$current_link"
switched=1

echo "[release] current -> $(readlink -f "$current_link")"
systemctl --user restart productflow-backend.service productflow-worker.service productflow-web.service
sleep 2

backend_health="$(curl -fsS "http://127.0.0.1:${backend_port}/healthz")"
frontend_html_head="$(curl -fsS "http://127.0.0.1:${web_port}/" | sed -n '1,5p')"
frontend_auth="$(curl -fsS "http://127.0.0.1:${web_port}/api/auth/session")"

echo "$backend_health" | rg '"status"\s*:\s*"ok"' >/dev/null
echo "$frontend_html_head" | rg '<!doctype html>' >/dev/null
echo "$frontend_auth" | rg '"authenticated"' >/dev/null

echo "[release] 发布成功"
echo "[release] backend_health=$backend_health"
echo "[release] frontend_auth=$frontend_auth"
echo "[release] release_dir=$release_dir"
