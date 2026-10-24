# AGENTS.md

**Scope: `ui/web/`** — ava-webui, the web client of the ava monorepo (`radch-ai/ava`).

## ava-webui

**ava-webui** — SvelteKit web app in SPA mode (`adapter-static`), Tailwind CSS 4, shadcn-svelte/bits-ui components. Talks to the platform API via a ky client (`src/lib/services/api.ts`) pointed at `VITE_BACKEND_API_URL`.

### Commands

Run from `ui/web/` with [bun](https://bun.sh):

- `bun install`
- `bun run dev` — dev server
- `bun run build` — production build
- `bun run check` — svelte-check
- `bun run lint` — prettier + eslint
- `bun run test:unit` — vitest (unit + component)
- `bun run test:e2e` — Playwright e2e (requires a running backend and Chromium at `TEST_CHROMIUM_WS_URL`)
