set dotenv-load := true

backend-install:
    uv sync --directory backend --extra dev

backend-run:
    bash scripts/with_dev_env.sh bash -lc 'uv run --directory backend uvicorn productflow_backend.main:app --reload --host 0.0.0.0 --port "${APP_PORT:-29282}"'

backend-run-prod:
    uv run --directory backend uvicorn productflow_backend.main:app --host ${APP_HOST:-0.0.0.0} --port ${APP_PORT:-29280}

backend-worker:
    bash scripts/with_dev_env.sh uv run --directory backend dramatiq --processes 2 --threads 4 productflow_backend.workers

agent-service-run:
    bash scripts/with_dev_env.sh bash -lc 'cd agent-service && go run ./cmd/productflow-agent-service'

agent-service-test:
    bash scripts/with_dev_env.sh bash -lc 'cd agent-service && go test ./...'

agent-service-test-live:
    PRODUCTFLOW_RUN_LIVE_AGENT=1 bash scripts/with_dev_env.sh bash -lc 'cd agent-service && go test -run TestLiveProviderTwoTurnTranscript -count=1 ./internal/app'

backend-migrate:
    bash scripts/with_dev_env.sh uv run --directory backend alembic upgrade head

backend-migrate-prod:
    uv run --directory backend alembic upgrade head

backend-worker-prod:
    uv run --directory backend dramatiq --processes 2 --threads 4 productflow_backend.workers

backend-test:
    uv run --directory backend pytest

docs-check:
    python3 scripts/check_docs.py

backend-test-live-recovery:
    docker compose up -d --wait productflow-postgres productflow-redis
    PRODUCTFLOW_RUN_LIVE_RECOVERY=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_workflow_recovery.py

backend-test-live-delivery-renditions:
    PRODUCTFLOW_RUN_LIVE_DELIVERY_RENDITIONS=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_delivery_renditions.py

backend-test-live-agent-product-intake:
    PRODUCTFLOW_RUN_LIVE_AGENT_PRODUCT_INTAKE=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_agent_product_intake.py

web-install:
    pnpm --dir web install

web-dev:
    bash scripts/with_dev_env.sh bash -lc 'web_port="${WEB_PORT:-29283}"; api_target="${VITE_DEV_PROXY_TARGET:-http://127.0.0.1:${APP_PORT:-29282}}"; VITE_API_BASE_URL= VITE_DEV_PROXY_TARGET="$api_target" pnpm --dir web dev -- --host 0.0.0.0 --port "$web_port" --strictPort'

web-preview-prod:
    pnpm --dir web preview -- --host 0.0.0.0 --port ${WEB_PORT:-29281} --strictPort

web-build:
    pnpm --dir web build
release:
    bash scripts/release.sh

release-dry-run:
    DRY_RUN=1 bash scripts/release.sh
