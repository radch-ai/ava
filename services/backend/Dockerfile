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
COPY services/backend/ ./services/backend/

# sqlc query layer is untracked; generate it here
RUN go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 && \
    cd services/backend && sqlc generate

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -extldflags '-static' -X main.Version=${VERSION}" \
    -trimpath \
    -o /bin/backend \
    ./services/backend

# ── Stage 2: Run ─────────────────────────────────────────────────────────────
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /bin/backend /app/backend

USER app

EXPOSE 8080

ENTRYPOINT ["/app/backend"]
