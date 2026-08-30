set dotenv-load := true

agent-service-install:
    pnpm --dir agent-service install --frozen-lockfile

agent-service-run:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service dev'

agent-service-test:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service test'

go-migrate:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-migrate'

# Truncate live business rows. Keeps provider_profiles / provider_bindings / app_settings
# (dumped first to storage-dev/settings-keep.sql). Does not drop the schema.
wipe-dev-data:
    bash scripts/with_dev_env.sh python3 scripts/wipe_dev_data.py --yes

seed-dev-providers:
    bash scripts/with_dev_env.sh python3 scripts/wipe_dev_data.py --seed-from-env

docs-check:
    python3 scripts/check_docs.py

go-test:
    bash scripts/with_dev_env.sh bash -lc 'go test -C go ./... -p 1'

go-test-live-providers:
    PRODUCTFLOW_RUN_LIVE_PROVIDERS=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/providers -count=1 -timeout 8m -run Live'

go-api:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-api'

go-worker:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-worker'

go-dispatcher:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-dispatcher --watch'

web-install:
    pnpm --dir web install

web-dev:
    bash scripts/with_dev_env.sh bash -lc 'web_port="${WEB_PORT:-29283}"; api_target="${VITE_DEV_PROXY_TARGET:-http://127.0.0.1:${APP_PORT:-29282}}"; VITE_API_BASE_URL= VITE_DEV_PROXY_TARGET="$api_target" pnpm --dir web dev -- --host 0.0.0.0 --port "$web_port" --strictPort'

[parallel]
[private]
dev-services: go-api go-worker go-dispatcher agent-service-run web-dev

dev-stop:
    bash scripts/stop_dev_app_processes.sh

dev:
    bash scripts/stop_dev_app_processes.sh
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres productflow-redis
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-migrate'
    just dev-services

web-preview-prod:
    pnpm --dir web preview -- --host 0.0.0.0 --port ${WEB_PORT:-29281} --strictPort

web-build:
    pnpm --dir web build

web-e2e-live-graph:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir web exec playwright install chromium && PRODUCTFLOW_RUN_LIVE_BROWSER_GRAPH=1 pnpm --dir web exec playwright test --config playwright.config.ts'

release:
    bash scripts/release.sh

release-dry-run:
    DRY_RUN=1 bash scripts/release.sh
