set shell := ["/bin/bash", "-euo", "pipefail", "-c"]

tauri_cli := "bunx @tauri-apps/cli"
tauri_config := "tauri.conf.json"

# Regenerate ui/src/lib/bindings.ts (tauri-specta)
bindings:
	cargo test --lib commands::tests::export_bindings -- --exact

## --- desktop (Tauri) ---

# Vite (ui, port 1420) + desktop app via Tauri CLI
[default]
dev:
	{{ tauri_cli }} dev --config {{ tauri_config }}

# Production UI build + packaged desktop app
build: bindings
	{{ tauri_cli }} build --config {{ tauri_config }}
