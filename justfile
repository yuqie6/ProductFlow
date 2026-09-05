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

agent-evals-freeze-collection plan:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts freeze-collection "$1"' -- '{{plan}}'

agent-evals-run-collection manifest purpose:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts run-live --collection "$1" --purpose "$2" --trials 3' -- '{{manifest}}' '{{purpose}}'

agent-evals-export-development run manifest:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir agent-service exec tsx evals/cli.ts export-development "$1" --collection "$2"' -- '{{run}}' '{{manifest}}'

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

# Opt-in listing-quality eval. Needs PRODUCTFLOW_RUN_IMAGE_EVALS=1, live API/worker, real prompt/image bindings.
# Bytes stay under STORAGE_ROOT/image-evals/. Gate: docs/audits/agent-eval-system.md#image-quality
image-evals-ingest json:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-image-evals ingest --json "$1"' -- '{{json}}'

image-evals-sample n="20" seed="1":
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-image-evals sample --n "$1" --seed "$2"' -- '{{n}}' '{{seed}}'

image-evals-run n="8" seed="1":
    bash scripts/with_dev_env.sh bash -lc 'PRODUCTFLOW_RUN_IMAGE_EVALS=1 go run -C go ./cmd/productflow-image-evals run --n "$1" --seed "$2"' -- '{{n}}' '{{seed}}'

image-evals-report run:
    bash scripts/with_dev_env.sh bash -lc 'go run -C go ./cmd/productflow-image-evals report "$1"' -- '{{run}}'

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

# Opt-in deeper document-authority action search (default gate uses 8 walks).
go-test-canvas-search:
    bash scripts/with_dev_env.sh bash -lc 'PRODUCTFLOW_CANVAS_SEARCH_WALKS=80 go test -C go ./internal/graph -run "^TestDocumentAuthoritySearch$" -count=1 -p 1 -timeout 20m'

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

# Opt-in authenticated Agent Session/Turn HTTP width and history-depth gate.
go-test-agent-read-load:
    PRODUCTFLOW_RUN_AGENT_READ_LOAD=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/agent -run TestAgentReadHTTPTargetScale -count=1 -p 1 -v -timeout 4m'

# Opt-in real dispatcher/Redis latency with and without a slow recovery backlog.
go-test-dispatch-latency:
    PRODUCTFLOW_RUN_DISPATCH_LATENCY=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/platform/queue -run TestDispatcherPendingSentLatency -count=1 -p 1 -v -timeout 4m'

# Opt-in queue overview dense/sparse active Graph latency and EXPLAIN gate.
go-test-queue-overview-load:
    PRODUCTFLOW_RUN_QUEUE_OVERVIEW_LOAD=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/platform/generation -run TestQueueOverviewActiveGraphScale -count=1 -p 1 -v -timeout 4m'

# Opt-in target-scale ImageSession list/history EXPLAIN gate in a disposable migrated database.
go-test-imagesession-query-plan:
    PRODUCTFLOW_RUN_IMAGE_SESSION_QUERY_PLAN=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -run TestImageSessionQueryPlanTargetScale -count=1 -p 1 -v -timeout 6m'

# Opt-in authenticated ImageSession HTTP payload/latency gate in a disposable migrated database.
go-test-imagesession-http-load:
    PRODUCTFLOW_RUN_IMAGE_SESSION_HTTP_LOAD=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -run TestImageSessionHTTPTargetScale -count=1 -p 1 -v -timeout 6m'

# Opt-in ImageSession SSE snapshot bytes, fallback, heartbeat and reconnect gate.
go-test-imagesession-sse-load:
    PRODUCTFLOW_RUN_IMAGE_SESSION_SSE_LOAD=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -run TestImageSessionSSESnapshotLoad -count=1 -p 1 -v -timeout 1m'

# Opt-in ImageSession Status/SSE active-set payload gate in a disposable migrated database.
go-test-imagesession-active-status:
    PRODUCTFLOW_RUN_IMAGE_SESSION_ACTIVE_STATUS=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession -run TestImageSessionStatusActiveSetScale -count=1 -p 1 -v -timeout 3m'

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

# Local two-replica field gates: notify loss, two dispatchers, SIGKILL survivor, capacity, Graph lease/fencing.
# SIGKILL spawn uses isolated PostgreSQL plus Redis DB 14; docker topology remains `just staging-up`.
go-test-staging-field:
    PRODUCTFLOW_RUN_STAGING_FIELD=1 bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/platform/notify ./internal/platform/queue ./internal/imagesession ./internal/graph -run "TestReplicaField|TestGraphRunLeaseTakesOverExpiredOwner|TestGraphRunLeaseFencesLateProviderResult|TestSubscribeSharesOneListenerAcrossChannels" -count=1 -p 1 -v -timeout 6m'

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

# Chromium: real Agent approval card → one WorkflowRun → real generated image. just dev must be running.
web-e2e-live-agent-workflow:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir web exec playwright install chromium && PRODUCTFLOW_RUN_LIVE_AGENT_WORKFLOW=1 pnpm --dir web exec playwright test e2e/live-agent-workflow.spec.ts --config playwright.config.ts'

# Chromium document rewrite/candidate: temporarily binds mock prompt/image then restores; just dev must be running.
web-e2e-canvas-document:
    bash scripts/with_dev_env.sh bash -lc 'pnpm --dir web exec playwright install chromium && PRODUCTFLOW_RUN_CANVAS_DOCUMENT=1 pnpm --dir web exec playwright test e2e/canvas-document-mock.spec.ts --config playwright.config.ts'

# Chromium scene and retry contracts; requires a running mock stack, never changes provider bindings.
web-e2e-canvas-run-recovery *args:
    PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 pnpm --dir web exec playwright test e2e/canvas-run-recovery.spec.ts --config playwright.config.ts {{args}}

# Chromium delivery files and ZIP lineage; requires mock bindings and unzip, does not switch providers.
web-e2e-canvas-delivery *args:
    PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 pnpm --dir web exec playwright test e2e/canvas-delivery.spec.ts --config playwright.config.ts {{args}}

# Chromium asset identity and recipe confirmation; requires isolated mock bindings.
web-e2e-canvas-asset-recipe *args:
    PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 pnpm --dir web exec playwright test e2e/canvas-asset-recipe.spec.ts --config playwright.config.ts {{args}}

# Chromium local edit adopt/lineage; requires isolated mock image bindings.
web-e2e-canvas-local-edit *args:
    PRODUCTFLOW_RUN_CANVAS_WORKFLOW=1 pnpm --dir web exec playwright test e2e/canvas-local-edit.spec.ts --config playwright.config.ts {{args}}

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
