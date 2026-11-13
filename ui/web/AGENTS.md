# AGENTS.md

**Scope: `ui/web/`** — ava-webui, the web client of the ava monorepo (`radch-ai/ava`).

## ava-webui

**ava-webui** — SvelteKit web app in SPA mode (`adapter-static`), Tailwind CSS 4, shadcn-svelte/bits-ui components. Talks to the platform API via a ky client (`src/lib/services/api.ts`) at same-origin `/api` — the vite dev/preview servers proxy it to the ava binary's `AVA_ADDR` (see `.env.example`; same env `just run ava` reads, defaults to `:8080`); production serves UI and API from the single ava binary.

### Commands

Run from `ui/web/` with [bun](https://bun.sh):

- `bun install` — installs deps; the postinstall hook provisions the local test Chromium. Prerequisite once per system (root, usually baked into the dev image): `sudo bunx playwright install-deps chromium`
- Wire types come from `backend/apitypes` via tygo into gitignored `src/lib/types/gen/` — `dev`/`check`/`check:watch`/`test:unit`/`test:e2e`/`build` regenerate them automatically (`bun run typegen` alone also works). Requires `tygo` on PATH from the root `just install-tools`; edit `backend/apitypes/*.go`, never the generated file
- `bun run dev` — dev server
- `bun run build` — production build
- `bun run check` — svelte-check
- `bun run biome-check` — biome (lint + format check; `bun run biome-write` auto-fixes)
- `bun run test:unit` — vitest (unit + component), runs backend-free via msw/node — see `docs/vitest.md`
- `bun run test:e2e` — Playwright e2e (browser provisioned by the postinstall hook); real backend by default, mock mode without one — see `docs/playwright.md`

### UI verification

After changing anything user-facing (routes, components, styles, interactions), verify in a real browser before reporting done: run `bun run dev`, open the changed surface, and confirm the behavior visually. Without a backend: `VITE_USE_MOCKS=true bun run dev` serves the deterministic mock API from the dev server itself (log in with the token `ava-mock-token`) — otherwise run the backend (`just run ava` from the repo root) and the dev server proxies `/api` to the backend's `AVA_ADDR` (defaults to `:8080`; pass the same `AVA_ADDR` to `bun run dev` when the backend doesn't use the default). Use Paseo's browser tools (browser_new_tab / browser_snapshot / browser_click / browser_screenshot) to drive and inspect the page — they are already available. Unit and component tests are not a substitute for this check.

## Svelte MCP

`.mcp.json` registers the official Svelte MCP server, run locally over stdio via `npx -y @sveltejs/mcp` ([local setup](https://svelte.dev/docs/ai/local-setup)). You are able to use it, giving you access to comprehensive Svelte 5 and SvelteKit documentation. Here's how to use the available tools effectively:

### 1. list-sections

Use this FIRST to discover all available documentation sections. Returns a structured list with titles, use_cases, and paths.
When asked about Svelte or SvelteKit topics, ALWAYS use this tool at the start of the chat to find relevant sections.

### 2. get-documentation

Retrieves full documentation content for specific sections. Accepts single or multiple sections.
After calling the list-sections tool, you MUST analyze the returned documentation sections (especially the use_cases field) and then use the get-documentation tool to fetch ALL documentation sections that are relevant for the user's task.

### 3. svelte-autofixer

Analyzes Svelte code and returns issues and suggestions.
You MUST use this tool whenever writing Svelte code before sending it to the user. Keep calling it until no issues or suggestions are returned.

### 4. playground-link

Generates a Svelte Playground link with the provided code.
After completing the code, ask the user if they want a playground link. Only call this tool after user confirmation and NEVER if code was written to files in their project.
