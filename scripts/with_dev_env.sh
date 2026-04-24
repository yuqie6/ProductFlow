#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"
if [[ -f "$repo_root/.env.dev" ]]; then
  set -a
  . "$repo_root/.env.dev"
  set +a
fi
exec "$@"
