# ── Build stage ──────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

# Build-time metadata
ARG BUILD_DATE
ARG VCS_REF
ARG VERSION=dev

# Pinned to specific Alpine 3.21 revisions (satisfies hadolint DL3018).
# Refresh cadence: before each release, query the apk index that ships
# inside the base image and bump to whatever is reported there:
#
#   docker run --rm golang:1.24-alpine apk policy gcc musl-dev
#
# Reasoning: golang:1.24-alpine and alpine:3.21 ship with FROZEN apk
# indices. A revision that exists in the live pkgs.alpinelinux.org main
# repo but was published *after* the base image was tagged will not be
# installable — `apk add` will fail with "unsatisfiable constraints" at
# `docker build` time on release.yml. The values below were sourced from
# the live v3.21 main index as of this commit.
RUN apk add --no-cache \
        gcc=14.2.0-r4 \
        musl-dev=1.2.5-r11

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
# ca-certificates + curl pinned to specific Alpine 3.21 revisions
# (satisfies hadolint DL3018); bumped the same way as the builder stage
# (see that RUN block for the exact refresh procedure — the runtime
# alpine:3.21 image also has a frozen apk index, just a different
# snapshot date than golang:1.24-alpine). adduser chained into the
# same RUN to address hadolint DL3059 (multiple consecutive RUN
# instructions) and to keep the runtime image a single layer.
RUN apk add --no-cache \
        ca-certificates=20260413-r0 \
        curl=8.14.1-r2 && \
    adduser -D -h /data hermem

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
