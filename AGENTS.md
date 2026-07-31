# AGENTS.md

**Scope: repo root** — monorepo for a single project. Multiple services, binaries, and apps across multiple languages.

## ava

**ava** (`radch-ai/ava`) — personal superagent. Platform-side control plane plus multi-device clients — UI clients for interaction and dashboards (also bundling local tools), and headless clients that only surface their local tools to the control plane.

## Layout

```
ava/
├── Cargo.toml            # Rust workspace manifest (member: ui/src-tauri)
├── Cargo.lock
├── go.mod / go.sum       # Single Go module for all Go code under pkg/ and services/
├── justfile              # Repo-root task runner — commands for the Rust workspace and Go module
├── go-services.Dockerfile # Shared Dockerfile for Go services (build via `just docker <service>`)
├── .env.example
├── pkg/                  # Shared Go libraries
│   ├── config/
│   └── zitadelvalidate/
├── services/             # Deployable Go services
│   └── backend/
└── ui/                   # tauri app root — see ui/AGENTS.md
    ├── justfile          # ui-side/tauri task runner
    ├── package.json      # ava-ui (SvelteKit)
    └── src-tauri/        # ava-tauri (workspace member)
```

## Go

**Module:** `github.com/radch-ai/ava` (single module at repo root; no nested `go.mod` files). Go 1.26+.

### Commands

Use `just` recipes from the repo root (run `just --list`). After every change: `just validate && just test <service>` (plus `just test-integration <service>` if the service has integration tests). Do not invoke `go build` / `go test` / `go run` / `go mod tidy` / `go fmt` or others directly when a recipe covers the task.

### services/backend

- REST API for user and agent configuration (soul, agents, threads) (`internal/api/{user,agent}`).
- PostgreSQL database with migrations under `services/backend/schema/migrations/`; sqlc-generated query layer (`internal/db`), `testcontainers-go` for integration tests.
- HTTP server with CORS and Zitadel auth middleware (`internal/server`).
- Zitadel Actions v2 webhook receiver for back-syncing user/agent state (`internal/webhooks/zitadel`).
- RevenueCat webhook handling for subscription management (`internal/billing`) — to be added.
- Per-agent run history for usage tracking and analytics (table exists, queries/code to be added) (`agent_runs`).

### pkg/config

Env-driven config loader. Reads env vars (server addr, allowed origins, Zitadel, DB URL, etc.) into a `Config` struct. Currently shaped around backend's needs; other services can extend or share as they land. See `.env.example` at the repo root for the variables.

### pkg/zitadelvalidate

Shared Zitadel OIDC token-introspection helper used by backend and gateway. Provides auth: token introspection and webhook signature verification.

## Service relationships

How services depend on and call each other. Add new services here as they land.

- **backend** ↔ **gateway**: backend is the central API; gateway calls backend for user config loading.
- **gateway** ← **ui**: the desktop app talks to gateway over WebSocket for real-time agent communication.
- **gateway** → **llm-proxy**: gateway routes LLM calls (quota enforcement, model routing) through llm-proxy for agent runs.

**UI / Tauri:** launch agents in `ui/` (auto-loads its `AGENTS.md`).
