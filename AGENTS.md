# AGENTS.md

**Scope: repo root** — monorepo: ava Go services + its web UI.

## ava

**ava** — personal superagent. One app, one `ava` binary: serves the API now, packs the web client later. Env vars use the `AVA_*` prefix.

## ava-webui

The web client (SvelteKit). Details live in `ui/web/` — see `ui/web/AGENTS.md`.

## Go

Single module `github.com/radch-ai/ava`. The justfile is the only way to build and run binaries (output: `bin/`, gitignored) and covers test/validate — run `just --list`. `utils/` holds shared helpers (env access, logger setup); env binding lives in `cmd/ava` (`loadConfig`).

**SQL tests** — `backend/internal/schema` tests cover SQL-level invariants only: seed guarantees and DB-enforced rules (e.g. `superagent_test.go`: seed row + delete trigger), named by concern, not by migration number. All other DB behavior is verified through `internal/api` endpoint tests — never re-pinned in schema.

## Layout

```
ava/
├── go.mod / go.sum       # single Go module
├── justfile              # task runner — build/run/test/validate
├── .env.example          # wired env vars
├── utils/                # shared helpers (env access, logger setup, serve incl. healthz probe)
├── cmd/ava/              # the ava binary; binds AVA_* envs, mounts backend + gateway
├── backend/              # control plane (public surface: backend.go)
├── gateway/              # realtime component — ws handler scaffold (public surface: gateway.go)
└── ui/web/               # ava-webui — API contract source (lib/mocks, lib/types)
```
