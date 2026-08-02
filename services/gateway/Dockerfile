# syntax=docker/dockerfile:1

# Build args: GO_VERSION (required), VERSION (optional, -ldflags → main.Version)

ARG GO_VERSION

# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:${GO_VERSION}-alpine AS builder
ARG VERSION=dev

RUN apk add --no-cache git ca-certificates tzdata

ENV GOTOOLCHAIN=auto

WORKDIR /app

# Copy module files first for layer caching
COPY go.mod go.sum ./
RUN go mod download

COPY pkg/ ./pkg/
COPY services/gateway/ ./services/gateway/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -extldflags '-static' -X main.Version=${VERSION}" \
    -trimpath \
    -o /bin/gateway \
    ./services/gateway

# ── Stage 2: Run ─────────────────────────────────────────────────────────────
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /bin/gateway /app/gateway

USER app

EXPOSE 8080

ENTRYPOINT ["/app/gateway"]
