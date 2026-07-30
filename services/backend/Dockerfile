# syntax=docker/dockerfile:1

# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.25.7-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

ENV GOTOOLCHAIN=auto

WORKDIR /app

# Copy vendor directory for offline builds
COPY go.mod go.sum vendor/ ./

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor \
    -ldflags="-w -s -extldflags '-static'" \
    -trimpath \
    -o /bin/app \
    ./cmd/backend

# ── Stage 2: Run ─────────────────────────────────────────────────────────────
FROM alpine:3.23

# ca-certificates for TLS, tzdata for time zones
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /bin/app .
COPY --chown=app:app schema/migrations ./migrations/

USER app

ENV BACKEND_ADDR=:8080

EXPOSE 8080

ENTRYPOINT ["/app/app"]
