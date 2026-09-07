#!/usr/bin/env bash
# Build and tag immutable ProductFlow release images from the current worktree.
# Does not start or stop Compose stacks. Does not touch shared project volumes.
#
# Usage:
#   bash scripts/release-build-images.sh
#   PRODUCTFLOW_VERSION=0.1.0 PRODUCTFLOW_REGISTRY=localhost:5000/ bash scripts/release-build-images.sh
#   RELEASE_BUILD_TARGETS=go,web bash scripts/release-build-images.sh
#
# Environment limits (TLS to registry.npmjs.org / proxy.golang.org, etc.) are
# reported honestly; a failed build must not be recorded as a successful HEAD build.

set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=release_common.sh
source "${script_dir}/release_common.sh"

root="$(release_repo_root)"
cd "$root"
release_resolve_meta "$root"

targets_csv="${RELEASE_BUILD_TARGETS:-go,agent,web}"
IFS=',' read -r -a targets <<<"$targets_csv"

echo "[release-build] repo_root=$root"
release_print_meta | sed 's/^/[release-build] /'

build_one() {
  local name="$1"
  local dockerfile="$2"
  local image="$3"
  echo "[release-build] building $name -> $image (dockerfile=$dockerfile)"
  docker build -f "$dockerfile" -t "$image" "$root"
}

failed=0
for target in "${targets[@]}"; do
  target="$(echo "$target" | tr -d '[:space:]')"
  case "$target" in
    go)
      if ! build_one go go/Dockerfile "$PRODUCTFLOW_GO_IMAGE"; then
        echo "[release-build] FAILED: go image ($PRODUCTFLOW_GO_IMAGE)" >&2
        failed=1
      fi
      ;;
    agent)
      if ! build_one agent agent-service/Dockerfile "$PRODUCTFLOW_AGENT_IMAGE"; then
        echo "[release-build] FAILED: agent image ($PRODUCTFLOW_AGENT_IMAGE)" >&2
        failed=1
      fi
      ;;
    web)
      if ! build_one web web/Dockerfile "$PRODUCTFLOW_WEB_IMAGE"; then
        echo "[release-build] FAILED: web image ($PRODUCTFLOW_WEB_IMAGE)" >&2
        failed=1
      fi
      ;;
    "")
      ;;
    *)
      echo "[release-build] unknown RELEASE_BUILD_TARGETS entry: $target" >&2
      failed=1
      ;;
  esac
done

if [[ "$failed" -ne 0 ]]; then
  echo "[release-build] one or more image builds failed; do not claim a full HEAD release build" >&2
  exit 1
fi

echo "[release-build] all requested images tagged"
release_print_meta
