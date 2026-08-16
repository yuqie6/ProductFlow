#!/usr/bin/env bash
# 前端设计体系机械检查
# 规则来源: web/AGENTS.md 的 "Design System And Styling" 一节。
#
# 用法:
#   scripts/check-web-design.sh            检查工作区相对 HEAD 的改动（含已暂存）
#   scripts/check-web-design.sh <file...>  只检查给定文件（切片验证用，路径相对仓库根）
#
# 策略:
#   - web/src/**/*.tsx 在白名单内: 只检查本次新增行，存量违规行不报错。
#   - web/src/**/*.tsx 不在白名单内: 必须全文件零违规。
#   - web/src/index.css: 禁止新增 `:root.dark .*` 全局深色兜底规则；
#     token 与共享类定义是 index.css 的 owner 面，不做全文件色值检查。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALLOWLIST="$ROOT/scripts/web-design-allowlist.txt"
cd "$ROOT"

if [ "$#" -gt 0 ]; then
  files=("$@")
else
  mapfile -t changed < <(
    {
      git diff --name-only --diff-filter=ACM HEAD
      git diff --name-only --diff-filter=ACM --cached
    } | sort -u
  )

  files=()
  for file in "${changed[@]}"; do
    case "$file" in
      web/src/*.tsx | web/src/index.css) files+=("$file") ;;
    esac
  done
fi

if [ "${#files[@]}" -eq 0 ]; then
  echo "web-design-check: no changed frontend style files"
  exit 0
fi

errors=0
warns=0
banned='slate-|zinc-|gray-|neutral-|#6366f1|#8b5cf6'
important_patch='![a-z][a-z0-9]*-'

report() {
  local level="$1" file="$2" line="$3" text="$4"
  printf '%s %s:%s: %s\n' "$level" "$file" "$line" "$text"
}

for file in "${files[@]}"; do
  if [ "$file" = "web/src/index.css" ]; then
    while IFS=: read -r line text; do
      [ -z "$line" ] && continue
      report "ERROR" "$file" "$line" "$text"
      errors=$((errors + 1))
    done < <(git diff --unified=0 HEAD -- "$file" | grep -E '^\+[^+]' | grep -nE '^[0-9]+:\+:root\.dark \.' | sed 's/^\([0-9]*\):+\+/\1:/' || true)
    continue
  fi

  in_allowlist=0
  if grep -qxF "$file" "$ALLOWLIST"; then
    in_allowlist=1
    echo "web-design-check: allowlisted (incremental) $file"
  fi

  if [ "$in_allowlist" -eq 1 ]; then
    diff_lines="$(git diff --unified=0 HEAD -- "$file" | grep -E '^\+[^+]' || true)"
    if [ -n "$diff_lines" ]; then
      while IFS=: read -r num text; do
        [ -z "$num" ] && continue
        report "ERROR" "$file" "$num" "$text"
        errors=$((errors + 1))
      done < <(printf '%s\n' "$diff_lines" | grep -nE "$banned" | sed 's/^\([0-9]*\):\+/\1:/')
      while IFS=: read -r num text; do
        [ -z "$num" ] && continue
        report "WARN" "$file" "$num" "$text"
        warns=$((warns + 1))
      done < <(printf '%s\n' "$diff_lines" | grep -nE "$important_patch" | sed 's/^\([0-9]*\):\+/\1:/')
    fi
  else
    while IFS=: read -r line text; do
      [ -z "$line" ] && continue
      report "ERROR" "$file" "$line" "$text"
      errors=$((errors + 1))
    done < <(grep -nE "$banned" "$file" || true)
    while IFS=: read -r line text; do
      [ -z "$line" ] && continue
      report "WARN" "$file" "$line" "$text"
      warns=$((warns + 1))
    done < <(grep -nE "$important_patch" "$file" || true)
  fi
done

echo "web-design-check: changed=${#files[@]} errors=$errors warns=$warns"
if [ "$errors" -gt 0 ]; then
  echo "违反 web/AGENTS.md 'Design System And Styling'；把文件迁出白名单前需全文件零违规。"
  exit 1
fi
