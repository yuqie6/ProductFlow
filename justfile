set dotenv-load := true

backend-install:
    uv sync --directory backend --extra dev

backend-run:
    bash scripts/with_dev_env.sh bash -lc 'uv run --directory backend uvicorn productflow_backend.main:app --reload --host 0.0.0.0 --port "${APP_PORT:-29282}"'

backend-run-prod:
    uv run --directory backend uvicorn productflow_backend.main:app --host ${APP_HOST:-0.0.0.0} --port ${APP_PORT:-29280}

backend-worker:
    bash scripts/with_dev_env.sh uv run --directory backend dramatiq --processes 2 --threads 4 productflow_backend.workers

backend-async-dispatcher:
    bash scripts/with_dev_env.sh uv run --directory backend python -m productflow_backend.commands.run_async_dispatcher --watch

agent-service-install:
    pnpm --dir agent-service install --frozen-lockfile

agent-service-run:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service dev'

agent-service-test:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service test'

backend-migrate:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-migrate'

backend-migrate-prod:
    go run -C go ./cmd/productflow-migrate

go-migrate:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-migrate'

backend-worker-prod:
    uv run --directory backend dramatiq --processes 2 --threads 4 productflow_backend.workers

backend-async-dispatcher-prod:
    uv run --directory backend python -m productflow_backend.commands.run_async_dispatcher --watch

backend-test:
    uv run --directory backend pytest

docs-check:
    python3 scripts/check_docs.py

go-test:
    bash scripts/with_dev_env.sh bash -lc 'go test -C go ./...'

go-api:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-api'

go-worker:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-worker'

go-dispatcher:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-dispatcher --watch'

backend-test-live-recovery:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres productflow-redis
    PRODUCTFLOW_RUN_LIVE_RECOVERY=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_workflow_recovery.py

backend-test-live-agent-redis-restart:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres productflow-redis
    PRODUCTFLOW_RUN_LIVE_RECOVERY=1 PRODUCTFLOW_RUN_LIVE_AGENT_REDIS_RESTART=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_workflow_recovery.py -k redis_server_restart

backend-test-live-agent-redis-connection:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres productflow-redis
    PRODUCTFLOW_RUN_LIVE_RECOVERY=1 PRODUCTFLOW_RUN_LIVE_AGENT_REDIS_CONNECTION=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_workflow_recovery.py -k dramatiq_worker_reconnects

backend-test-live-agent-dispatcher-watch:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres productflow-redis
    PRODUCTFLOW_RUN_LIVE_RECOVERY=1 PRODUCTFLOW_RUN_LIVE_AGENT_DISPATCHER_WATCH=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_workflow_recovery.py -k resident_dispatcher

backend-test-live-image-session-media-migration:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres
    PRODUCTFLOW_RUN_LIVE_IMAGE_SESSION_MEDIA_MIGRATION=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_image_session_media_migration.py

backend-test-live-delivery-renditions:
    PRODUCTFLOW_RUN_LIVE_DELIVERY_RENDITIONS=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_delivery_renditions.py

backend-test-live-delivery-rendition-worker-effects:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres productflow-redis
    PRODUCTFLOW_RUN_LIVE_DELIVERY_RENDITIONS=1 PRODUCTFLOW_RUN_LIVE_DELIVERY_RENDITION_WORKER_EFFECTS=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_delivery_renditions.py -k worker_termination_replays_one_result

backend-test-live-async-delivery:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres
    PRODUCTFLOW_RUN_LIVE_ASYNC_DELIVERY=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_async_delivery.py

backend-run-async-dispatcher:
    bash scripts/with_dev_env.sh uv run --directory backend python -m productflow_backend.commands.run_async_dispatcher

backend-test-live-agent-product-intake:
    PRODUCTFLOW_RUN_LIVE_AGENT_PRODUCT_INTAKE=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_agent_product_intake.py

backend-test-live-agent-execution:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres
    PRODUCTFLOW_RUN_LIVE_AGENT_EXECUTION=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_agent_execution.py

backend-test-live-agent-question-postgres-restart:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres
    PRODUCTFLOW_RUN_LIVE_AGENT_EXECUTION=1 PRODUCTFLOW_RUN_LIVE_AGENT_QUESTION_POSTGRES_RESTART=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_agent_execution.py -k question_continues

backend-test-live-agent-worker-effects:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres
    PRODUCTFLOW_RUN_LIVE_AGENT_EXECUTION=1 PRODUCTFLOW_RUN_LIVE_AGENT_WORKER_EFFECTS=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_agent_execution.py -k agent_sync_worker_termination

backend-test-live-agent-effects:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres
    PRODUCTFLOW_RUN_LIVE_AGENT_EFFECTS=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_agent_effects.py

backend-test-live-agent-postgres-restart:
    bash scripts/with_dev_env.sh docker compose up -d --wait productflow-postgres
    PRODUCTFLOW_RUN_LIVE_AGENT_POSTGRES_RESTART=1 bash scripts/with_dev_env.sh uv run --directory backend pytest -q -m live_dependencies tests/test_live_agent_postgres_restart.py

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
