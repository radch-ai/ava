set shell := ["/bin/bash", "-euo", "pipefail", "-c"]

tauri_cli := "bunx --bun @tauri-apps/cli"
tauri_config := "tauri.conf.json"

# Regenerate ui/src/lib/bindings.ts (tauri-specta)
bindings:
	cargo test --lib commands::tests::export_bindings -- --exact

## --- test ---

# Rust tests.
test-rs:
	cargo test

# UI (Vitest) tests. Bindings first so specs can import bindings.ts.
test-ui: bindings
	( cd ui && bun run test )

## --- validate ---

# svelte-check → knip → biome check --write (auto-fixes format + lint).
validate-ui:
	( cd ui && bun run validate )

# clippy with warnings as errors → fmt.
validate-rs:
	cargo clippy --all-targets -- -D warnings && cargo fmt

## --- desktop (Tauri) ---

# Vite (ui, port 1420) + desktop app via Tauri CLI
[default]
dev:
	{{ tauri_cli }} dev --config {{ tauri_config }}

# Production UI build + packaged desktop app
build: bindings
	{{ tauri_cli }} build --config {{ tauri_config }}
