set dotenv-load := true

agent-service-install:
    pnpm --dir agent-service install --frozen-lockfile

agent-service-run:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service dev'

agent-service-test:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service test'

agent-service-check-contracts:
    pnpm --dir agent-service run check-contract-artifacts

# Opt-in real-model skill/tool evals. Requires AGENT_PROVIDER_API_KEY.
agent-evals-live:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts run-live --trials 3'

agent-evals-smoke skill:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts run-live --trials 1 --filter "$1"' -- '{{skill}}'

agent-evals-report run:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts report "$1"' -- '{{run}}'

agent-evals-diff baseline candidate:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts diff "$1" "$2"' -- '{{baseline}}' '{{candidate}}'

agent-evals-coverage:
    pnpm --dir agent-service exec tsx evals/cli.ts coverage

agent-evals-mutate:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts mutate'

agent-evals-state:
    bash scripts/with_dev_env.sh bash -lc 'PRODUCTFLOW_RUN_AGENT_EVALS_L2=1 go test -C go ./internal/agent -run "^TestAgentEvalStateL2Live$" -count=1 -timeout 4h -v'

agent-evals-smoke-state skill:
    bash scripts/with_dev_env.sh bash -lc 'PRODUCTFLOW_RUN_AGENT_EVALS_L2=1 PRODUCTFLOW_AGENT_EVAL_TRIALS=1 PRODUCTFLOW_AGENT_EVAL_FILTER="$1" go test -C go ./internal/agent -run "^TestAgentEvalStateL2Live$" -count=1 -timeout 30m -v' -- '{{skill}}'

agent-evals-sim:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts run-sim --trials 1'

agent-evals-judge run:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts judge "$1"' -- '{{run}}'

agent-evals-export-labels run:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts export-labels "$1"' -- '{{run}}'

agent-evals-import-labels file:
    pnpm --dir agent-service exec tsx evals/cli.ts import-labels "$1"

agent-evals-judge-calibrate human judge:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts judge-calibrate --human "$1" --judge "$2"' -- '{{human}}' '{{judge}}'

agent-evals-adversarial:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts run-adversarial --trials 1'

agent-evals-mine days="7":
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-agent-evals mine --days "$1"' -- '{{days}}'

agent-evals-export-turn turn:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-agent-evals export --turn "$1"' -- '{{turn}}'

# Opt-in nightly: L1 k=3, L2, L5, then report whatever latest.json points at.
# Failures in an earlier layer do not skip later layers. Requires AGENT_PROVIDER_API_KEY.
agent-evals-nightly:
    bash scripts/with_dev_env.sh bash -lc 'status=0; pnpm --dir agent-service exec tsx evals/cli.ts run-live --trials 3 || status=1; PRODUCTFLOW_RUN_AGENT_EVALS_L2=1 go test -C go ./internal/agent -run "^TestAgentEvalStateL2Live$" -count=1 -timeout 4h -v || status=1; pnpm --dir agent-service exec tsx evals/cli.ts run-adversarial --trials 1 || status=1; root="${STORAGE_ROOT:-storage-dev}"; if [ -f "$root/agent-evals/latest.json" ]; then run_id=$(python3 -c "import json,os; print(json.load(open(os.path.join(os.environ.get(\"STORAGE_ROOT\",\"storage-dev\"), \"agent-evals\", \"latest.json\")))[\"run_id\"])"); pnpm --dir agent-service exec tsx evals/cli.ts report "$run_id" || status=1; fi; exit $status'

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

# Opt-in PostgreSQL journal depth/concurrency/P95 and 100-SSE connection gate.
go-test-agent-journal-capacity:
    bash scripts/with_dev_env.sh bash -lc 'PRODUCTFLOW_RUN_AGENT_JOURNAL_CAPACITY=1 go test -C go ./internal/agent -run "^(TestAgentJournalCapacityGate|TestAgentSSEHTTPConnectionCapacityGate)$" -count=1 -v -timeout 6m'

# Opt-in local fsync WAL depth/P95 gate.
agent-service-test-local-journal-capacity:
    cd agent-service && PRODUCTFLOW_RUN_AGENT_LOCAL_JOURNAL_CAPACITY=1 pnpm vitest run src/store.test.ts -t "appends and reloads 10k durable WAL events"

# Opt-in read-only HTTP regression gate. Set HTTP_GATE_GO_PRODUCT and HTTP_GATE_GO_GRAPH;
# set HTTP_GATE_MAIN_BASE and HTTP_GATE_MAIN_PRODUCT to include the legacy comparison.
http-ab-gates:
    bash scripts/with_dev_env.sh python3 scripts/bench_workbench_http.py

# Opt-in target-scale GraphRun list EXPLAIN gate in a disposable migrated database.
go-test-graph-query-plan:
    PRODUCTFLOW_RUN_GRAPH_QUERY_PLAN=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/graph -run TestGraphSummaryQueryPlanTargetScale -count=1 -p 1 -v -timeout 6m'

# Opt-in target-scale Agent Session list EXPLAIN gate in a disposable migrated database.
go-test-agent-query-plan:
    PRODUCTFLOW_RUN_AGENT_QUERY_PLAN=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/agent -run TestAgentSessionQueryPlanTargetScale -count=1 -p 1 -v -timeout 6m'

# Opt-in target-scale ImageSession list/history EXPLAIN gate in a disposable migrated database.
go-test-imagesession-query-plan:
    PRODUCTFLOW_RUN_IMAGE_SESSION_QUERY_PLAN=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -run TestImageSessionQueryPlanTargetScale -count=1 -p 1 -v -timeout 6m'

# Opt-in 100 near-limit image ZIP extra MaxRSS/heap gate (mediaarchive streaming writer).
go-test-zip-rss:
    PRODUCTFLOW_RUN_ZIP_RSS=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/mediaarchive -run TestStreamingZipNearLimitImagesKeepsExtraRSSUnderBudget -count=1 -p 1 -v -timeout 4m'

go-test-live-providers:
    PRODUCTFLOW_RUN_LIVE_PROVIDERS=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/providers -count=1 -timeout 8m -run Live'

go-api:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-api'

go-worker:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-worker'

go-dispatcher:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-dispatcher --watch'

# 2 API + 2 worker + 2 dispatcher against shared PostgreSQL/Redis/storage.
staging-up:
    bash scripts/with_dev_env.sh docker compose -p productflow-staging -f docker-compose.yml -f docker-compose.staging.yml up -d --wait

staging-down:
    bash scripts/with_dev_env.sh docker compose -p productflow-staging -f docker-compose.yml -f docker-compose.staging.yml down

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
    python3 scripts/check_web_bundle_budget.py

web-e2e-live-graph:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir web exec playwright install chromium && PRODUCTFLOW_RUN_LIVE_BROWSER_GRAPH=1 pnpm --dir web exec playwright test e2e/direct-create-full-graph.spec.ts --config playwright.config.ts'

# Chromium Agent SSE: duplicate seq1 then seq2 on one generation; connection stays open.
web-e2e-agent-sse:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir web exec playwright install chromium && PRODUCTFLOW_RUN_LIVE_BROWSER_GRAPH=1 pnpm --dir web exec playwright test e2e/agent-conversation-runtime.spec.ts e2e/agent-sse-reconnect.spec.ts --config playwright.config.ts'

# Go+PostgreSQL+Web, Node Agent off: claim a running Turn then persist a canvas node title.
web-e2e-running-turn-canvas:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir web exec playwright install chromium && PRODUCTFLOW_RUN_RUNNING_TURN_CANVAS=1 pnpm --dir web exec playwright test e2e/running-turn-canvas.spec.ts --config playwright.config.ts'

# Opt-in browser TTI, duplicate-read, and on-demand rich run-detail gate.
web-e2e-workbench-performance:
    bash scripts/with_dev_env.sh bash -lc 'PRODUCTFLOW_RUN_WORKBENCH_PERF=1 pnpm --dir web exec playwright test e2e/workbench-performance.spec.ts --config playwright.config.ts'

release:
    bash scripts/release.sh

release-dry-run:
    DRY_RUN=1 bash scripts/release.sh
