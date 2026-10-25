# Vitest Coverage

`bun run test:unit` runs two Vitest projects from `vite.config.ts`:

- `client`: browser-mode Svelte tests under `tests/unit/**/*.svelte.{test,spec}.ts` (launches the local Chromium via Playwright)
- `server`: Node-based tests under `tests/unit/**/*.{test,spec}.ts`

## Testing Conventions

- Unit tests run against the mock API, never a real backend.
- Service and handler tests intercept HTTP with `msw/node` against `createHandlers(createSeedData())` from `src/lib/mocks` — the same handlers the dev server mounts in mock mode.
- Component tests mock the `api` module directly.
- End-to-end integration belongs in Playwright against the real backend — see `docs/playwright.md`.

## Coverage Model

- Vitest covers logic closer to the unit/component boundary than Playwright.
- Browser-mode tests validate Svelte UI behavior; server-mode tests validate non-DOM logic (services, mock handlers, utilities).

## What It Covers

- `tests/unit/lib/mocks/handlers.test.ts`: the mock API contract — agent listing, cursor pagination, configuration CRUD, and error responses (400/404/409).
- `tests/unit/lib/auth/store.test.ts`: covers session restore from `GET /auth/me`, unauthenticated start on 401, token exchange via `POST /auth/exchange`, invalid-token rejection with the server's message, logout via `POST /auth/logout`, and session-expiry clearing without network.
- `tests/unit/lib/services/api.test.ts` and `tests/unit/lib/services/user/agents.test.ts`: cover API client construction, the 401 auth signal with the server's error message, agent list fetching, backend response shapes, and surfaced errors.
- Route/page rendering tests such as `tests/unit/routes/superagent/page.svelte.test.ts`, `tests/unit/routes/agent/new/page.svelte.test.ts`, and `tests/unit/routes/agent/[agentName]/page.svelte.test.ts`: cover what each route renders, which controls appear, how key user interactions work, and how superagent pages differ from worker-agent pages.
- Agent configuration tests such as `tests/unit/components/agentconfig/AgentConfigSheet.svelte.test.ts` and `tests/unit/components/agentconfig/AgentConfigEditor.svelte.test.ts`: cover editor layout, field switching, worker-vs-superagent field availability, save/cancel flows, unsaved-change dialogs, retry behavior after API failure, and post-save callbacks.
- Supporting UI tests such as `tests/unit/components/agentconfig/IconPicker.svelte.test.ts`, `tests/unit/lib/components/AgentHeader.svelte.test.ts`, `tests/unit/lib/components/AppSidebar.svelte.test.ts`, `tests/unit/lib/components/TextArea.svelte.test.ts`, and model-selector specs: cover smaller presentation and control-level behaviors that would be too granular for Playwright.

## Main Logic Areas Covered

- Auth logic: session restore, token exchange, logout, and session-expiry clearing.
- Service logic: correct API path usage, error propagation, and shape handling for agent configuration responses.
- Mock API logic: pagination, persistence, and error semantics of the handlers in `src/lib/mocks`.
- Route/component logic: rendering the new-agent experience, worker page layout, superagent page layout, and config editor interactions.
- Helper logic: cursor-based pagination and date formatting.

## What It Is Best For

- Fast feedback on isolated rendering and state changes.
- Verifying service and mock contracts without a full backend/browser stack.

## What It Does Not Try To Replace

- Vitest does not prove the app works end-to-end in a real browser against a real backend — that is Playwright's job.

## Overlap With Playwright

- There is intentional overlap in feature areas like agent pages and configuration editing.
- The difference is boundary: Vitest checks component/service correctness in isolation, while Playwright checks that the same areas work together in the running app.
- If both files mention the same feature, that is not necessarily duplication; duplication only becomes a problem when both suites assert the same thing at the same level.

## Current Scope Snapshot

- Vitest currently spans browser-mode Svelte specs and Node-based unit tests.
- The suite is broad on component and helper behavior, especially around auth state, mock handlers, editor interactions, and route rendering.
