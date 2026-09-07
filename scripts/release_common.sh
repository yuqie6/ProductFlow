#!/usr/bin/env bash
# Shared helpers for ProductFlow release image tagging and packaging.
# Sourced by release-build-images.sh / release-push-images.sh / release-pack.sh.

set -euo pipefail

release_repo_root() {
  cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd
}

# Immutable image tag convention (pre-stable and stable):
#   PRODUCTFLOW_IMAGE_TAG = <PRODUCTFLOW_VERSION>-<git-sha12>
# where PRODUCTFLOW_VERSION defaults to 0.0.0 until the first stable cut.
# Install packages pin this tag; do not install from floating "latest".
#
# Image repository names (optional REGISTRY prefix, with trailing slash if set):
#   ${REGISTRY}productflow-go:${TAG}
#   ${REGISTRY}productflow-agent:${TAG}
#   ${REGISTRY}productflow-web:${TAG}
# Upstream pins stay as upstream tags: postgres:16, redis:7, nginx base inside web image.
release_resolve_meta() {
  local root="$1"
  local version="${PRODUCTFLOW_VERSION:-0.0.0}"
  local sha="${PRODUCTFLOW_GIT_SHA:-}"
  local short=""

  if [[ -z "$sha" ]]; then
    if git -C "$root" rev-parse --verify HEAD >/dev/null 2>&1; then
      sha="$(git -C "$root" rev-parse HEAD)"
    else
      echo "[release] PRODUCTFLOW_GIT_SHA is required when git HEAD is unavailable" >&2
      return 1
    fi
  fi

  if [[ ! "$sha" =~ ^[0-9a-fA-F]{7,40}$ ]]; then
    echo "[release] invalid PRODUCTFLOW_GIT_SHA: $sha" >&2
    return 1
  fi

  short="$(printf '%s' "$sha" | cut -c1-12 | tr '[:upper:]' '[:lower:]')"
  local tag="${PRODUCTFLOW_IMAGE_TAG:-${version}-${short}}"

  if [[ ! "$tag" =~ ^[A-Za-z0-9._-]+$ ]]; then
    echo "[release] invalid PRODUCTFLOW_IMAGE_TAG: $tag" >&2
    return 1
  fi

  local registry="${PRODUCTFLOW_REGISTRY:-${REGISTRY:-}}"
  if [[ -n "$registry" && "$registry" != */ ]]; then
    registry="${registry}/"
  fi

  PRODUCTFLOW_VERSION="$version"
  PRODUCTFLOW_GIT_SHA="$sha"
  PRODUCTFLOW_GIT_SHA_SHORT="$short"
  PRODUCTFLOW_IMAGE_TAG="$tag"
  PRODUCTFLOW_REGISTRY="$registry"
  PRODUCTFLOW_GO_IMAGE="${PRODUCTFLOW_GO_IMAGE:-${registry}productflow-go:${tag}}"
  PRODUCTFLOW_AGENT_IMAGE="${PRODUCTFLOW_AGENT_IMAGE:-${registry}productflow-agent:${tag}}"
  PRODUCTFLOW_WEB_IMAGE="${PRODUCTFLOW_WEB_IMAGE:-${registry}productflow-web:${tag}}"
  PRODUCTFLOW_POSTGRES_IMAGE="${PRODUCTFLOW_POSTGRES_IMAGE:-postgres:16}"
  PRODUCTFLOW_REDIS_IMAGE="${PRODUCTFLOW_REDIS_IMAGE:-redis:7}"
}

release_print_meta() {
  cat <<EOF
PRODUCTFLOW_VERSION=${PRODUCTFLOW_VERSION}
PRODUCTFLOW_GIT_SHA=${PRODUCTFLOW_GIT_SHA}
PRODUCTFLOW_GIT_SHA_SHORT=${PRODUCTFLOW_GIT_SHA_SHORT}
PRODUCTFLOW_IMAGE_TAG=${PRODUCTFLOW_IMAGE_TAG}
PRODUCTFLOW_REGISTRY=${PRODUCTFLOW_REGISTRY}
PRODUCTFLOW_GO_IMAGE=${PRODUCTFLOW_GO_IMAGE}
PRODUCTFLOW_AGENT_IMAGE=${PRODUCTFLOW_AGENT_IMAGE}
PRODUCTFLOW_WEB_IMAGE=${PRODUCTFLOW_WEB_IMAGE}
PRODUCTFLOW_POSTGRES_IMAGE=${PRODUCTFLOW_POSTGRES_IMAGE}
PRODUCTFLOW_REDIS_IMAGE=${PRODUCTFLOW_REDIS_IMAGE}
EOF
}

release_write_version_file() {
  local out="$1"
  local built_at="${PRODUCTFLOW_BUILT_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
  local notes="${PRODUCTFLOW_RELEASE_NOTES:-pre-stable artifact; not an R6 claim}"

  cat >"$out" <<EOF
# ProductFlow release identity. Install packages pin PRODUCTFLOW_*_IMAGE values.
# Do not treat floating tags (e.g. latest) as the supported install pin.
PRODUCTFLOW_VERSION=${PRODUCTFLOW_VERSION}
PRODUCTFLOW_GIT_SHA=${PRODUCTFLOW_GIT_SHA}
PRODUCTFLOW_GIT_SHA_SHORT=${PRODUCTFLOW_GIT_SHA_SHORT}
PRODUCTFLOW_IMAGE_TAG=${PRODUCTFLOW_IMAGE_TAG}
PRODUCTFLOW_REGISTRY=${PRODUCTFLOW_REGISTRY}
PRODUCTFLOW_GO_IMAGE=${PRODUCTFLOW_GO_IMAGE}
PRODUCTFLOW_AGENT_IMAGE=${PRODUCTFLOW_AGENT_IMAGE}
PRODUCTFLOW_WEB_IMAGE=${PRODUCTFLOW_WEB_IMAGE}
PRODUCTFLOW_POSTGRES_IMAGE=${PRODUCTFLOW_POSTGRES_IMAGE}
PRODUCTFLOW_REDIS_IMAGE=${PRODUCTFLOW_REDIS_IMAGE}
PRODUCTFLOW_BUILT_AT=${built_at}
PRODUCTFLOW_RELEASE_NOTES=${notes}
EOF
}
