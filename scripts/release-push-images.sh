#!/usr/bin/env bash
# Push (or retag into a local registry) immutable ProductFlow release images.
#
# Usage:
#   PRODUCTFLOW_REGISTRY=localhost:5000/ bash scripts/release-push-images.sh
#   PRODUCTFLOW_REGISTRY=ghcr.io/example/ bash scripts/release-push-images.sh
#
# Requires images already present locally under PRODUCTFLOW_*_IMAGE names
# (from release-build-images.sh or an intentional retag). Does not build.

set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=release_common.sh
source "${script_dir}/release_common.sh"

root="$(release_repo_root)"
cd "$root"
release_resolve_meta "$root"

if [[ -z "${PRODUCTFLOW_REGISTRY}" ]]; then
  echo "[release-push] PRODUCTFLOW_REGISTRY or REGISTRY is required (e.g. localhost:5000/ or ghcr.io/org/)" >&2
  exit 1
fi

echo "[release-push] repo_root=$root"
release_print_meta | sed 's/^/[release-push] /'

push_one() {
  local image="$1"
  echo "[release-push] pushing $image"
  docker image inspect "$image" >/dev/null
  docker push "$image"
}

failed=0
for image in "$PRODUCTFLOW_GO_IMAGE" "$PRODUCTFLOW_AGENT_IMAGE" "$PRODUCTFLOW_WEB_IMAGE"; do
  if ! push_one "$image"; then
    echo "[release-push] FAILED: $image" >&2
    failed=1
  fi
done

if [[ "$failed" -ne 0 ]]; then
  echo "[release-push] push incomplete; install hosts may use docker save/load instead (see release/README.md)" >&2
  exit 1
fi

echo "[release-push] all product images pushed"
