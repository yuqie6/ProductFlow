#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
artifact_root=${SAAS_CAPACITY_ARTIFACT_ROOT:-$repo_root/storage-dev/saas-capacity-0908}
manifest_path=${SAAS_CAPACITY_MANIFEST:-$artifact_root/fixture-manifest.json}
identity_path=${SAAS_CAPACITY_IDENTITY:-$artifact_root/source-identity.json}
db_url=${SAAS_CAPACITY_DATABASE_URL:-postgres://productflow:capacity-db-password-0908@127.0.0.1:30183/productflow}
base_url=${SAAS_CAPACITY_BASE_URL:-http://127.0.0.1:30182}
label=${1:?usage: run_round.sh L1|L2 ROUND}
round=${2:?usage: run_round.sh L1|L2 ROUND}

case "$label" in
    L1)
        clients=20
        rate=5
        ;;
    L2)
        clients=50
        rate=15
        ;;
    *)
        echo "label must be L1 or L2" >&2
        exit 2
        ;;
esac

out_dir=${SAAS_CAPACITY_LOAD_DIR:-$artifact_root/mixed-load}
mkdir -p "$out_dir"
metrics_json="$out_dir/${label,,}-round-${round}-resources.json"
window_file="$out_dir/${label,,}-round-${round}-window.json"

python3 "$repo_root/scripts/saas_capacity/capacity_identity.py" \
    --repo-root "$repo_root" \
    --verify "$identity_path" >/dev/null

python3 "$repo_root/scripts/saas_capacity/collect_metrics.py" \
    --db-url "$db_url" \
    --redis-port 30184 \
    --duration 900 \
    --window-file "$window_file" \
    --startup-timeout 120 \
    --grace 5 \
    --storage-root "$artifact_root" \
    --out "$metrics_json" \
    --label "$label-round-$round" \
    --identity "$identity_path" &
metrics_pid=$!

cleanup_metrics() {
    if kill -0 "$metrics_pid" 2>/dev/null; then
        kill "$metrics_pid" 2>/dev/null || true
    fi
}
trap cleanup_metrics EXIT

load_status=0
if python3 "$repo_root/scripts/saas_capacity/run_load.py" \
        --base-url "$base_url" \
        --db-url "$db_url" \
        --manifest "$manifest_path" \
        --label "$label" \
        --round "$round" \
        --clients "$clients" \
        --rate "$rate" \
        --warmup 60 \
        --duration 300 \
        --out-dir "$out_dir" \
        --provider-events "$artifact_root/mock/provider-events.jsonl" \
        --identity "$identity_path" \
        --window-file "$window_file"; then
    :
else
    load_status=$?
fi

metrics_status=0
if wait "$metrics_pid"; then
    :
else
    metrics_status=$?
fi
trap - EXIT

if (( load_status != 0 )); then
    exit "$load_status"
fi
if (( metrics_status != 0 )); then
    exit "$metrics_status"
fi
