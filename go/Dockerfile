FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go/go.mod go/go.sum ./
RUN go mod download
COPY go/ ./
# 交付 WebP 走 chai2010/webp，把 vendored libwebp C 编进二进制。关掉 CGO 会 undefined webpGetInfo/Encode。
ENV CGO_ENABLED=1
RUN go build -o /out/productflow-api ./cmd/productflow-api \
 && go build -o /out/productflow-worker ./cmd/productflow-worker \
 && go build -o /out/productflow-dispatcher ./cmd/productflow-dispatcher \
 && go build -o /out/productflow-migrate ./cmd/productflow-migrate

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/productflow-api /usr/local/bin/productflow-api
COPY --from=build /out/productflow-worker /usr/local/bin/productflow-worker
COPY --from=build /out/productflow-dispatcher /usr/local/bin/productflow-dispatcher
COPY --from=build /out/productflow-migrate /usr/local/bin/productflow-migrate
WORKDIR /app
CMD ["productflow-api"]
