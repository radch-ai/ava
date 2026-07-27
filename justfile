set shell := ["/bin/bash", "-euo", "pipefail", "-c"]

tauri_cli := "bunx @tauri-apps/cli"
tauri_config := "tauri.conf.json"

## --- desktop (Tauri) ---

# Vite (ui, port 1420) + desktop app via Tauri CLI
[default]
dev:
	{{ tauri_cli }} dev --config {{ tauri_config }}

# Production UI build + packaged desktop app
build:
	{{ tauri_cli }} build --config {{ tauri_config }}
