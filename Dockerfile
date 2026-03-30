# ─────────────────────────────────────────────────────────────
# Flint — multi-stage Dockerfile
# Build:  docker build --target server -t flint-server .
# Targets: server, worker, controller, agent
# ─────────────────────────────────────────────────────────────

# ── Build stage ─────────────────────────────────────────────
FROM golang:1.26-alpine AS build

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ENV CGO_ENABLED=0

RUN go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /bin/flint-server ./cmd/server && \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /bin/flint-worker ./cmd/worker && \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /bin/flint-controller ./cmd/controller && \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /bin/flint-agent ./cmd/agent

# ── Server ──────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot AS server
COPY --from=build /bin/flint-server /usr/local/bin/flint-server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["flint-server"]

# ── Worker ──────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot AS worker
COPY --from=build /bin/flint-worker /usr/local/bin/flint-worker
USER nonroot:nonroot
ENTRYPOINT ["flint-worker"]

# ── Controller ──────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot AS controller
COPY --from=build /bin/flint-controller /usr/local/bin/flint-controller
USER nonroot:nonroot
ENTRYPOINT ["flint-controller"]

# ── Agent ───────────────────────────────────────────────────
# The agent runs pipeline steps — it needs git, a shell, and
# common CI tools. Alpine, not distroless.
FROM alpine:3.21 AS agent

RUN apk add --no-cache \
    ca-certificates \
    git \
    openssh-client \
    curl \
    jq \
    bash \
    && adduser -D -u 1000 flint

COPY --from=build /bin/flint-agent /usr/local/bin/flint-agent

USER flint
WORKDIR /home/flint
ENTRYPOINT ["flint-agent"]
