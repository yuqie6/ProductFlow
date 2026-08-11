FROM golang:1.26.5-alpine AS build

WORKDIR /src/agent-service

COPY agent-service/go.mod agent-service/go.sum ./
COPY agent-service/third_party/agent-harness ./third_party/agent-harness
RUN go mod download

COPY agent-service/cmd ./cmd
COPY agent-service/internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/productflow-agent-service ./cmd/productflow-agent-service

FROM alpine:3.22 AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 productflow \
    && adduser -S -D -H -u 10001 -G productflow productflow \
    && mkdir -p /data \
    && chown productflow:productflow /data

COPY --from=build /out/productflow-agent-service /usr/local/bin/productflow-agent-service

USER productflow

ENV AGENT_LISTEN_ADDRESS=:29284 \
    AGENT_DATA_ROOT=/data

EXPOSE 29284
VOLUME ["/data"]

HEALTHCHECK --interval=10s --timeout=5s --retries=12 --start-period=10s \
    CMD wget -qO- http://127.0.0.1:29284/healthz | grep -q '"status":"ok"'

ENTRYPOINT ["/usr/local/bin/productflow-agent-service"]
