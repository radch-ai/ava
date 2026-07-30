# syntax=docker/dockerfile:1

# Shared Dockerfile for Go services under services/. Build via `just docker <service>`.

ARG SERVICE

# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.26.5-alpine AS builder
ARG SERVICE

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

ENV GOTOOLCHAIN=auto

WORKDIR /app

# Copy module files first for layer caching
COPY go.mod go.sum ./

# Copy the whole repo so internal cross-package builds resolve, then build
# only the requested service.
COPY . .

# Generate the sqlc query layer (untracked in repo; produced fresh here).
RUN go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 && \
    cd services/${SERVICE} && sqlc generate

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -extldflags '-static'" \
    -trimpath \
    -o /bin/${SERVICE} \
    ./services/${SERVICE}

# ── Stage 2: Run ─────────────────────────────────────────────────────────────
FROM alpine:3.23
ARG SERVICE

# ca-certificates for TLS, tzdata for time zones
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app

WORKDIR /app

ENV SERVICE=${SERVICE}
COPY --from=builder /bin/${SERVICE} /app/${SERVICE}

USER app

EXPOSE 8080

ENTRYPOINT /app/${SERVICE}
