#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
artifact_root=${SAAS_CAPACITY_ARTIFACT_ROOT:-$repo_root/storage-dev/saas-capacity-0908}
manifest_path=${SAAS_CAPACITY_MANIFEST:-$artifact_root/fixture-manifest.json}
identity_path=${SAAS_CAPACITY_IDENTITY:-$artifact_root/source-identity.json}
db_url=${SAAS_CAPACITY_DATABASE_URL:-postgres://productflow:capacity-db-password-0908@127.0.0.1:30183/productflow}

python3 "$repo_root/scripts/saas_capacity/capacity_identity.py" \
    --repo-root "$repo_root" \
    --verify "$identity_path" >/dev/null

for label in L1 L2; do
    for round in 1 2 3; do
        "$repo_root/scripts/saas_capacity/run_round.sh" "$label" "$round"
    done
done

python3 "$repo_root/scripts/saas_capacity/run_contention.py" \
    --db-url "$db_url" \
    --manifest "$manifest_path" \
    --provider-events "$artifact_root/mock/provider-events.jsonl" \
    --identity "$identity_path" \
    --out "$artifact_root/contention.json"

python3 "$repo_root/scripts/saas_capacity/run_fault_recovery.py" \
    --db-url "$db_url" \
    --manifest "$manifest_path" \
    --identity "$identity_path" \
    --provider-events "$artifact_root/mock/provider-events.jsonl" \
    --out "$artifact_root/fault-recovery.json"

echo "capacity campaign evidence: $artifact_root"
