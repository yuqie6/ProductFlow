FROM node:22.20.0-alpine AS build

WORKDIR /src/agent-service

RUN corepack enable
COPY agent-service/package.json agent-service/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY agent-service/tsconfig.json ./
COPY agent-service/src ./src
COPY agent-service/.pi ./.pi
COPY agent-service/harness ./harness
COPY agent-service/scripts ./scripts
COPY go/prompts/agent/runtime-policy.md /src/go/prompts/agent/runtime-policy.md
COPY go/internal/agent/tool_manifest_generated.go /src/go/internal/agent/tool_manifest_generated.go
COPY go/internal/graph/graph_command_schema_generated.go /src/go/internal/graph/graph_command_schema_generated.go
COPY web/src/pages/workbench/agent/toolManifest.generated.ts /src/web/src/pages/workbench/agent/toolManifest.generated.ts
RUN pnpm run build

FROM node:22.20.0-alpine AS runtime

WORKDIR /app/agent-service

RUN apk add --no-cache ca-certificates flock \
    && addgroup -S -g 10001 productflow \
    && adduser -S -D -H -u 10001 -G productflow productflow \
    && mkdir -p /data /app/storage/agent-evolution-traces \
    && chown productflow:productflow /data /app/storage/agent-evolution-traces \
    && chmod 700 /app/storage/agent-evolution-traces

RUN corepack enable
COPY --from=build /src/agent-service/package.json /src/agent-service/pnpm-lock.yaml ./
RUN pnpm install --prod --frozen-lockfile
COPY --from=build /src/agent-service/dist ./dist
COPY --from=build /src/agent-service/.pi ./.pi
COPY --from=build /src/agent-service/harness ./harness
COPY go/prompts /app/go/prompts

USER productflow

ENV AGENT_LISTEN_ADDRESS=0.0.0.0:29284 \
    AGENT_DATA_ROOT=/data

EXPOSE 29284
VOLUME ["/data"]

HEALTHCHECK --interval=10s --timeout=5s --retries=12 --start-period=10s \
    CMD node -e "fetch('http://127.0.0.1:29284/healthz').then(async r => { const b = await r.json(); if (!r.ok || b.runtime !== 'productflow-pi') process.exit(1) }).catch(() => process.exit(1))"

ENTRYPOINT ["node", "dist/main.js"]
