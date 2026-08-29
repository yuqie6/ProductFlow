FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go/go.mod go/go.sum ./
RUN go mod download
COPY go/ ./
RUN CGO_ENABLED=0 go build -o /out/productflow-api ./cmd/productflow-api \
 && CGO_ENABLED=0 go build -o /out/productflow-worker ./cmd/productflow-worker \
 && CGO_ENABLED=0 go build -o /out/productflow-dispatcher ./cmd/productflow-dispatcher \
 && CGO_ENABLED=0 go build -o /out/productflow-migrate ./cmd/productflow-migrate

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/productflow-api /usr/local/bin/productflow-api
COPY --from=build /out/productflow-worker /usr/local/bin/productflow-worker
COPY --from=build /out/productflow-dispatcher /usr/local/bin/productflow-dispatcher
COPY --from=build /out/productflow-migrate /usr/local/bin/productflow-migrate
WORKDIR /app
CMD ["productflow-api"]
