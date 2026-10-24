# Vitest Coverage

`bun test:unit` runs two Vitest projects from `vite.config.ts`:

- `client`: browser-mode Svelte tests under `tests/**/*.svelte.test.ts`
- `server`: Node-based tests under `tests/unit/**/*.{test,spec}.ts` (browser-mode `.svelte.test.ts` files excluded)

## Coverage Model

- Vitest covers logic closer to the unit/component boundary than Playwright.
- It is used for fast feedback on stores, services, route components, editor interactions, and Playwright helper code without requiring the full app stack.
- Browser-mode tests validate Svelte UI behavior, while server-mode tests validate non-DOM logic and test utility helpers.

## What It Covers

- `tests/unit/lib/auth/store.test.ts`: covers session restore from `GET /auth/me`, unauthenticated start on 401, token exchange via `POST /auth/exchange`, invalid-token rejection with the server's message, logout via `POST /auth/logout`, and session-expiry clearing without network.
- `tests/unit/lib/services/api.test.ts` and `tests/unit/lib/services/user/agents.test.ts`: cover API client construction, the 401 auth signal with the server's error message, agent list fetching, backend response shapes, and surfaced errors.
- Route/page rendering tests such as `tests/unit/routes/superagent/page.svelte.test.ts`, `tests/unit/routes/agent/new/page.svelte.test.ts`, and `tests/unit/routes/agent/[agentName]/page.svelte.test.ts`: cover what each route renders, which controls appear, how key user interactions work, and how superagent pages differ from worker-agent pages.
- Agent configuration tests such as `tests/unit/components/agentconfig/AgentConfigSheet.svelte.test.ts`, `tests/unit/components/agentconfig/AgentConfigEditor.svelte.test.ts`, and related route/component specs: cover editor layout, field switching, worker-vs-superagent field availability, save/cancel flows, unsaved-change dialogs, retry behavior after API failure, and post-save callbacks.
- Supporting UI tests such as `tests/unit/components/agentconfig/IconPicker.svelte.test.ts`, `tests/unit/lib/components/AgentHeader.svelte.test.ts`, `tests/unit/lib/components/TextArea.svelte.test.ts`, and model-selector specs: cover smaller presentation and control-level behaviors that would be too granular for Playwright.

## Main Logic Areas Covered

- Auth logic: session restore, token exchange, logout, and session-expiry clearing.
- Service logic: correct API path usage, error propagation, and shape handling for agent configuration responses.
- Route/component logic: rendering the new-agent experience, worker page layout, superagent page layout, and config editor interactions.
- Helper logic: cursor-based pagination and date formatting.

## What It Is Best For

- Fast feedback on isolated rendering and state changes.
- Verifying service contracts without a full backend/browser stack.

## What It Does Not Try To Replace

- Vitest does not prove the full authenticated app works end-to-end in the browser.
- It does not replace Playwright checks for real routing, seeded browser auth bootstrap, backend integration, or multi-page user flows.

## Overlap With Playwright

- There is intentional overlap in feature areas like auth, superagent, agent pages, and configuration editing.
- The difference is boundary: Vitest checks component/service correctness in isolation, while Playwright checks that the same areas work together in the running app.
- If both files mention the same feature, that is not necessarily duplication; duplication only becomes a problem when both suites assert the same thing at the same level.

## Current Scope Snapshot

- Vitest currently spans browser-mode Svelte specs and Node-based unit tests.
- The suite is broad on component and helper behavior, especially around auth state, editor interactions, and route rendering.
