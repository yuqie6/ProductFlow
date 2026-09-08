#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
artifact_root=${SAAS_CAPACITY_ARTIFACT_ROOT:-$repo_root/storage-dev/saas-capacity-0908}
network_name=${SAAS_CAPACITY_NETWORK:-pf-capacity-0908-net}
identity_tool="$repo_root/scripts/saas_capacity/capacity_identity.py"
identity_path="$artifact_root/source-identity.json"
git_commit=$(git -C "$repo_root" rev-parse HEAD)
source_hash=$(python3 "$identity_tool" --repo-root "$repo_root" --source-hash)
image_name=${SAAS_CAPACITY_GO_IMAGE:-pf-capacity-0908-go:${git_commit:0:12}-${source_hash:0:12}}
pg_name=pf-capacity-0908-postgres
redis_name=pf-capacity-0908-redis
mock_name=pf-capacity-0908-mock
api_name=pf-capacity-0908-api
worker_name=pf-capacity-0908-worker
dispatcher_name=pf-capacity-0908-dispatcher
pg_password=${SAAS_CAPACITY_PG_PASSWORD:-capacity-db-password-0908}

for port in 30182 30183 30184 30185 30186 30190; do
    if ss -ltn "sport = :$port" | tail -n +2 | grep -q .; then
        echo "required capacity port is already occupied: $port" >&2
        exit 1
    fi
done

mkdir -p "$artifact_root/postgres" "$artifact_root/redis" "$artifact_root/mock" "$artifact_root/logs"

if [[ -e "$artifact_root/mock/provider-events.jsonl" ]]; then
    echo "provider event evidence already exists; use a fresh capacity artifact root" >&2
    exit 1
fi

python3 "$identity_tool" \
    --repo-root "$repo_root" \
    --write "$identity_path" \
    --image-name "$image_name" >/dev/null

if ! docker network inspect "$network_name" >/dev/null 2>&1; then
    docker network create "$network_name" >/dev/null
fi

if docker inspect "$pg_name" >/dev/null 2>&1 || docker inspect "$redis_name" >/dev/null 2>&1 || docker inspect "$mock_name" >/dev/null 2>&1 || docker inspect "$api_name" >/dev/null 2>&1 || docker inspect "$worker_name" >/dev/null 2>&1 || docker inspect "$dispatcher_name" >/dev/null 2>&1; then
    echo "a capacity container already exists; inspect it before reusing or cleaning it" >&2
    exit 1
fi

docker build \
    --label "capacity.source_identity=$source_hash" \
    --label "capacity.git_commit=$git_commit" \
    --tag "$image_name" \
    --file "$repo_root/go/Dockerfile" \
    "$repo_root"

image_id=$(docker image inspect --format '{{.Id}}' "$image_name")
repo_digests=$(docker image inspect --format '{{join .RepoDigests ","}}' "$image_name")
python3 "$identity_tool" \
    --repo-root "$repo_root" \
    --attach-image "$identity_path" \
    --image-name "$image_name" \
    --image-id "$image_id" \
    --repo-digests "$repo_digests" >/dev/null

docker run --detach --name "$pg_name" --hostname "$pg_name" --network "$network_name" \
    --cpus=0.75 --memory=1536m \
    --env POSTGRES_USER=productflow \
    --env POSTGRES_PASSWORD="$pg_password" \
    --env POSTGRES_DB=productflow \
    --publish 127.0.0.1:30183:5432 \
    --volume "$artifact_root/postgres:/var/lib/postgresql/data" \
    postgres:16 \
    postgres -c max_connections=100 -c shared_buffers=256MB >/dev/null

until docker exec "$pg_name" pg_isready -U productflow -d productflow >/dev/null 2>&1; do
    sleep 1
done

docker run --detach --name "$redis_name" --hostname "$redis_name" --network "$network_name" \
    --cpus=0.25 --memory=512m \
    --publish 127.0.0.1:30184:6379 \
    --volume "$artifact_root/redis:/data" \
    redis:7 redis-server --appendonly yes --save "" >/dev/null

until docker exec "$redis_name" redis-cli ping >/dev/null 2>&1; do
    sleep 1
done

docker run --detach --name "$mock_name" --hostname "$mock_name" --network "$network_name" \
    --cpus=0.25 --memory=512m \
    --publish 127.0.0.1:30190:30190 \
    --volume "$repo_root/scripts/saas_capacity/mock_provider.py:/app/mock_provider.py:ro" \
    --volume "$artifact_root/mock:/data" \
    python:3.12-slim python /app/mock_provider.py --port 30190 --delay 2 --long-delay 15 >/dev/null

docker run --rm --name pf-capacity-0908-migrate --network "$network_name" \
    --cpus=0.25 --memory=512m \
    --env DATABASE_URL="postgres://productflow:${pg_password}@${pg_name}:5432/productflow" \
    "$image_name" productflow-migrate

common_env=(
    --env APP_HOST=0.0.0.0
    --env APP_PORT=29280
    --env SESSION_COOKIE_SECURE=false
    --env BACKEND_CORS_ORIGINS=http://127.0.0.1:30182
    --env AUTH_RATE_LIMIT_NAMESPACE=productflow:capacity-0908
    --env AUTH_RATE_LIMIT_WINDOW_SECONDS=900
    --env AUTH_RATE_LIMIT_IP_MAX=1000
    --env AUTH_RATE_LIMIT_SUBJECT_MAX=1000
    --env ADMIN_ACCESS_KEY=capacity-admin-key-0908
    --env SESSION_SECRET=capacity-session-secret-0908
    --env DATABASE_URL="postgres://productflow:${pg_password}@${pg_name}:5432/productflow"
    --env REDIS_URL="redis://${redis_name}:6379/0"
    --env STORAGE_ROOT=/app/storage
    --env LOG_DIR=/app/storage/logs
    --env LOG_LEVEL=INFO
    --env LOG_FORMAT=json
    --env AGENT_SERVICE_BASE_URL=http://pf-capacity-0908-agent-disabled:29284
    --env AGENT_SERVICE_INTERNAL_TOKEN=capacity-agent-token-0908
    --env AGENT_SERVICE_CONNECT_TIMEOUT_SECONDS=1
    --env AGENT_SERVICE_READ_TIMEOUT_SECONDS=5
    --env AGENT_TURN_SYNC_POLL_SECONDS=1
    --env UPLOAD_MAX_IMAGE_BYTES=10485760
    --env UPLOAD_MAX_BATCH_BYTES=52428800
    --env UPLOAD_MAX_BATCH_FILES=20
    --env UPLOAD_MAX_REFERENCE_IMAGES=6
    --env UPLOAD_MAX_PIXELS=16000000
    --env UPLOAD_ALLOWED_IMAGE_MIME_TYPES=image/png,image/jpeg,image/webp
)

docker run --detach --name "$api_name" --hostname "$api_name" --network "$network_name" \
    --cpus=0.75 --memory=1536m \
    "${common_env[@]}" \
    --publish 127.0.0.1:30182:29280 \
    --volume "$artifact_root:/app/storage" \
    "$image_name" productflow-api >/dev/null

docker run --detach --name "$worker_name" --hostname "$worker_name" --network "$network_name" \
    --cpus=1.5 --memory=3g \
    "${common_env[@]}" \
    --env WORKER_METRICS_ADDR=0.0.0.0:29286 \
    --publish 127.0.0.1:30186:29286 \
    --volume "$artifact_root:/app/storage" \
    "$image_name" productflow-worker >/dev/null

docker run --detach --name "$dispatcher_name" --hostname "$dispatcher_name" --network "$network_name" \
    --cpus=0.5 --memory=1g \
    "${common_env[@]}" \
    --env DISPATCHER_METRICS_ADDR=0.0.0.0:29285 \
    --publish 127.0.0.1:30185:29285 \
    --volume "$artifact_root:/app/storage" \
    "$image_name" productflow-dispatcher --watch --interval 1 --recovery-interval 10 >/dev/null

for attempt in $(seq 1 60); do
    if curl --fail --silent http://127.0.0.1:30182/healthz >/dev/null 2>&1; then
        echo "capacity stack ready: API http://127.0.0.1:30182"
        exit 0
    fi
    sleep 1
done

echo "capacity API did not become healthy" >&2
docker logs "$api_name" >&2 || true
exit 1
