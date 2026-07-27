# AGENTS.md

## ava

**ava** (`radch-ai/ava`) — Tauri 2 desktop app with a SvelteKit frontend in `ui/`.

## Layout

One Cargo project at the repo root — Tauri, extra crates, services, and binaries all live here (not in a sibling `src-tauri/` like subproject). Frontend is separate under `ui/`.

| Path | Role |
|------|------|
| `Cargo.toml`, `src/`, `build.rs` | Root Rust workspace — desktop app today; room for more crates/binaries in the same project. |
| `tauri.conf.json`, `icons/` | Tauri config and bundle assets. |
| `ui/` | SvelteKit SPA (adapter-static, port **1420**). |
| `justfile` | Source of truth for project commands. |

## Commands

**Always read `justfile` before running any project-related command.** Use `just` recipes when they exist; do not hardcode `cargo`, `bun`, or `bunx` unless no recipe covers the task.

```bash
just --list
```
