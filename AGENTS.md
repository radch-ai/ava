# AGENTS.md

**Scope: repo root** — monorepo for a single project. Multiple services, binaries, and apps across multiple languages.

## ava

**ava** (`radch-ai/ava`) — personal superagent. Platform-side control plane plus multi-device clients — UI clients for interaction and dashboards (also bundling local tools), and headless clients that only surface their local tools to the control plane.

## Layout

```
ava/
├── Cargo.toml            # Rust workspace manifest (member: ui/src-tauri)
├── Cargo.lock
├── .env.example
└── ui/                   # tauri app root — see ui/AGENTS.md
    ├── package.json      # ava-ui (SvelteKit)
    └── src-tauri/        # ava-tauri (workspace member)
```

| Path | Role |
|------|------|
| `Cargo.toml` | Rust workspace manifest. |
| `ui/` | Desktop app. Has its own `AGENTS.md` — prefer launching agents directly in `ui/` so they load it and run from there automatically. |

**UI / Tauri:** launch agents in `ui/` (auto-loads its `AGENTS.md`).
