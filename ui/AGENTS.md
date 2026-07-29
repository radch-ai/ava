# AGENTS.md

**Scope: `ui/` only** — SvelteKit frontend and Tauri host. The rest of the monorepo is out of scope here.

## ava

**ava** (`radch-ai/ava`) — desktop app under `ui/`:

- **`ava-ui`** — SvelteKit frontend (`package.json`, `src/`)
- **`ava-tauri`** — Tauri Rust host (`src-tauri/`)

## Layout

```
ava/                    # repo root — monorepo (workspace, env, other top-level dirs)
├── Cargo.toml
├── Cargo.lock
├── .env
└── ui/                 # app root — run commands from here
    ├── justfile
    ├── package.json    # ava-ui
    ├── src/
    └── src-tauri/      # ava-tauri (workspace member)
```

| Path | Role |
|------|------|
| `package.json` (`ava-ui`) | SvelteKit SPA (adapter-static, port **1420**), `justfile`. |
| `src-tauri/` (`ava-tauri`) | Tauri crate — `Cargo.toml`, `src/`, `tauri.conf.json`, `icons/`, `capabilities/`. Built via repo-root workspace (`../Cargo.toml`). |
| `src/` | SvelteKit routes and components. |
| `../.env` | Env files at repo root. |

## Commands

Read `justfile` first. Run from `ui/`:

```bash
just --list
```

Use `just` recipes; do not hardcode `cargo`, `bun`, or `bunx` unless no recipe covers the task.
