# ── Build stage ──────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

# Build-time metadata
ARG BUILD_DATE
ARG VCS_REF
ARG VERSION=dev

RUN apk add --no-cache gcc musl-dev

WORKDIR /build

# Dependency layer — cached until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

# Source layer — invalidates on any source change.
COPY . .

# Static build with stripped symbols and version info.
# CGO_ENABLED=1 required for github.com/mattn/go-sqlite3.
RUN CGO_ENABLED=1 \
    go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildDate=${BUILD_DATE} -X main.gitCommit=${VCS_REF}" \
    -o /hermem \
    ./src

# ── Runtime stage ────────────────────────────────────────────
FROM alpine:3.21

# Image metadata (OCI labels).
LABEL org.opencontainers.image.title="Hermem"
LABEL org.opencontainers.image.description="Lightweight, single-binary graph memory for LLM agents"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.revision="${VCS_REF}"
LABEL org.opencontainers.image.source="https://github.com/pavelveter/hermem"

# Runtime deps: ca-certificates for TLS to OpenAI/Ollama APIs.
RUN apk add --no-cache ca-certificates curl

# Non-root user with home directory for the SQLite DB.
RUN adduser -D -h /data hermem

COPY --from=builder --chown=hermem:hermem /hermem /usr/local/bin/hermem

USER hermem
WORKDIR /data
VOLUME ["/data"]

EXPOSE 8420

# Health check using the liveness endpoint.
# start-period gives the binary + DB init time to settle.
# retries=2 fails fast — orchestrators restart quickly on unhealthy.
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=2 \
    CMD curl -sf http://localhost:8420/health/live || exit 1

ENTRYPOINT ["hermem"]
CMD ["serve"]
