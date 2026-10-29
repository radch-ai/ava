# Playwright Coverage

`bun run test:e2e` runs browser-level coverage from `tests/e2e/specs/` against the built app preview and the Chromium instance configured in `playwright.config.ts`.

## Testing Conventions

- E2E is integration testing: the specs run against the **real backend** (`E2E_BACKEND_API_URL`) and a real browser. They never use mocks.
- Unit-level correctness against the mock API belongs in Vitest — see `docs/vitest.md`.
- To author or debug a spec without a backend, run `E2E_BACKEND_API_URL=http://127.0.0.1:4173/api VITE_USE_MOCKS=true bun run test:e2e`: the build bakes the backend URL, so pointing it at the preview origin makes the app self-served by the mock middleware, and the shared fixtures exchange the seeded token for a session cookie so specs enter the app directly. CI always runs against the real backend.

## Coverage Model

- The suite exercises browser routing, UI interaction, network requests, and backend-facing flows together.
- This is the place to check whether a user can actually complete a flow in the running app, not just whether a component or service behaves in isolation.
- Until the backend serves the API, the specs document the intended flows; they become executable as backend endpoints land.

## What It Covers

- `tests/e2e/specs/agent-create.spec.ts`: covers the create-agent journey from opening the page through name entry, section editing, submission, id derivation from the display name, duplicate-name rejection without auto-suffixing, invalid-name rejection, whitespace trimming, and failure handling when creation requests break.
- `tests/e2e/specs/agent-list.spec.ts`: checks that the sidebar reflects the current user agent list, shows newly created agents, supports navigation into agent detail pages, and tolerates empty or refreshed list states.
- `tests/e2e/specs/agent-detail.spec.ts`: covers non-superagent agent detail rendering, accessible header/config actions, activity-feed presence, Ask superagent CTA behavior, icon rendering, and 404 behavior for missing agents.
- `tests/e2e/specs/agent-config.spec.ts`: exercises opening the configuration sheet, selecting editable sections, changing persisted fields, saving updates, and confirming saved values survive reload/navigation.
- `tests/e2e/specs/navigation.spec.ts`: checks unsaved-changes guards, discard vs keep-editing decisions, mobile sidebar interactions, and deep-link loading directly into an agent page.
- `tests/e2e/specs/error-handling.spec.ts`: covers 404 behavior for missing agents, error banners when the agents API fails or is unreachable, concurrent-edit handling, and large-input tolerance.
- `tests/e2e/specs/api.spec.ts`: verifies the running app emits the expected list/create/update requests, JSON payloads, and content-type headers.
- `tests/e2e/specs/superagent.spec.ts`: checks the superagent page at `/agent/_superagent` (including the `/` redirect), welcome message, model selector presence, model persistence, and navigation to the superagent from the sidebar.

## Main User Flows Covered

- Agent creation: open new agent page -> fill required or extended config -> submit -> land on `/agent/{id}` using the server-derived id (`name.toLowerCase()` with spaces as hyphens).
- Agent editing: open config sheet from detail page -> edit fields -> save -> reload or revisit -> confirm persisted state.
- Agent navigation: move between sidebar, agent pages, the superagent page, and deep links while preserving correct route behavior.
- Failure recovery: broken or unreachable API responses keep the app understandable instead of silently failing.

## Shared Test Infrastructure

- `tests/e2e/setup/fixtures.ts` re-exports the base Playwright test so specs share a single import point.
- `playwright.config.ts` runs the built preview app in a locally launched Chromium and points the frontend at the backend URL used during E2E runs.

## What This Does Not Try To Replace

- Playwright is for end-to-end confidence, not fine-grained component behavior.
- It does not replace Vitest coverage for component rendering details, local state transitions, or service-level unit contracts.

## Overlap With Vitest

- There is intentional overlap in feature areas like agent detail, agent creation, and agent configuration.
- The difference is level: Playwright proves the whole browser-to-backend flow works together, while Vitest proves smaller units and mocked boundaries behave correctly.
- If a check can be expressed without real browser routing, network interaction, or backend integration, it usually belongs in Vitest instead of being duplicated here.

## Current Scope Snapshot

- E2E specs live under `tests/e2e/specs/*.spec.ts`.
- The stabilized suite currently covers 65 Playwright tests.
