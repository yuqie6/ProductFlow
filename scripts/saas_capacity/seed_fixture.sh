#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
artifact_root=${SAAS_CAPACITY_ARTIFACT_ROOT:-$repo_root/storage-dev/saas-capacity-0908}
manifest_path=${SAAS_CAPACITY_MANIFEST:-$artifact_root/fixture-manifest.json}
db_url=${SAAS_CAPACITY_DATABASE_URL:-postgres://productflow:capacity-db-password-0908@127.0.0.1:30183/productflow}
identity_path=${SAAS_CAPACITY_IDENTITY:-$artifact_root/source-identity.json}
identity_tool="$repo_root/scripts/saas_capacity/capacity_identity.py"

if [[ -e "$manifest_path" ]]; then
    echo "capacity fixture manifest already exists: $manifest_path" >&2
    exit 1
fi

python3 "$identity_tool" --repo-root "$repo_root" --verify "$identity_path" >/dev/null
source_identity_sha256=$(python3 "$identity_tool" --repo-root "$repo_root" --verify "$identity_path" --field source_identity_sha256)
git_commit=$(python3 "$identity_tool" --repo-root "$repo_root" --verify "$identity_path" --field git_commit)
image_name=$(python3 "$identity_tool" --repo-root "$repo_root" --verify "$identity_path" --field image.name)
image_id=$(python3 "$identity_tool" --repo-root "$repo_root" --verify "$identity_path" --field image.id)

PRODUCTFLOW_RUN_SAAS_CAPACITY_FIXTURE=1 \
SAAS_CAPACITY_MANIFEST="$manifest_path" \
SAAS_CAPACITY_MOCK_PROVIDER_URL=http://pf-capacity-0908-mock:30190 \
SAAS_CAPACITY_COMMIT="$git_commit" \
SAAS_CAPACITY_SOURCE_IDENTITY_SHA256="$source_identity_sha256" \
SAAS_CAPACITY_IMAGE_NAME="$image_name" \
SAAS_CAPACITY_IMAGE_ID="$image_id" \
DATABASE_URL="$db_url" \
go test -C "$repo_root/go" ./internal/auth -run '^TestSaaSCapacityFixture$' -count=1 -timeout 10m -v

echo "fixture manifest: $manifest_path"
