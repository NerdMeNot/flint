# ─────────────────────────────────────────────────────────────
# Flint — multi-stage Dockerfile
# Build:  docker build --target flint -t flint .
# Targets: flint (control plane + CLI), flint-agent (machine daemon)
# ─────────────────────────────────────────────────────────────

# ── Build stage ─────────────────────────────────────────────
FROM golang:1.27-alpine AS build

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ENV CGO_ENABLED=0
ENV LDFLAGS="-s -w -X github.com/NerdMeNot/flint/internal/version.Version=${VERSION} -X github.com/NerdMeNot/flint/internal/version.Commit=${COMMIT}"

RUN go build -trimpath -ldflags="${LDFLAGS}" -o /bin/flint ./cmd/flint && \
    go build -trimpath -ldflags="${LDFLAGS}" -o /bin/flint-agent ./cmd/flint-agent

# ── Control plane ───────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot AS flint
COPY --from=build /bin/flint /usr/local/bin/flint
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["flint", "server"]

# ── Agent ───────────────────────────────────────────────────
# The agent runs pipeline steps on a machine — it needs git, a shell, and
# common CI tools. Alpine, not distroless.
FROM alpine:3.21 AS flint-agent

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
