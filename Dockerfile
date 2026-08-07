# syntax=docker/dockerfile:1.7

FROM node:24-bookworm-slim AS web-builder

WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci

COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS server-builder

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=web-builder /src/web/dist ./internal/webui/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/home-gateway \
      ./cmd/server && \
    mkdir -p /out/config /out/data/db /out/data/downloads && \
    touch /out/data/.keep /out/config/.keep

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=server-builder --chown=nonroot:nonroot /out/home-gateway ./home-gateway
COPY --from=server-builder --chown=nonroot:nonroot /out/config /config
COPY --from=server-builder --chown=nonroot:nonroot /out/data /data
COPY --chown=nonroot:nonroot config.example.yaml /config/config.yaml

ENV GIN_MODE=release \
    SERVER_ADDR=:8080 \
    DATA=/data \
    DB_DRIVER=sqlite

EXPOSE 8080

VOLUME ["/config", "/data"]

STOPSIGNAL SIGTERM

ENTRYPOINT ["/app/home-gateway"]
CMD ["run"]
