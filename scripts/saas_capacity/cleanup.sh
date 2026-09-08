#!/usr/bin/env bash
set -euo pipefail

network_name=${SAAS_CAPACITY_NETWORK:-pf-capacity-0908-net}
containers=(pf-capacity-0908-api pf-capacity-0908-worker pf-capacity-0908-dispatcher pf-capacity-0908-mock pf-capacity-0908-redis pf-capacity-0908-postgres)

for name in "${containers[@]}"; do
    if docker inspect "$name" >/dev/null 2>&1; then
        docker rm --force "$name" >/dev/null
    fi
done

if docker network inspect "$network_name" >/dev/null 2>&1; then
    docker network rm "$network_name" >/dev/null
fi

echo "capacity containers and network removed; storage artifacts were retained"
