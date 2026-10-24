# Playwright Coverage

`bun test:e2e` runs browser-level coverage from `tests/e2e/specs/` against the built app preview and the Docker Chromium instance configured in `playwright.config.ts`.

## Coverage Model

- Playwright covers the real app shell served during E2E runs; a running backend provides the API the specs exercise.
- The suite exercises browser routing, UI interaction, network requests, and backend-facing flows together.
- This is the place to check whether a user can actually complete a flow in the running app, not just whether a component or helper behaves in isolation.

## What It Covers

- `tests/e2e/specs/agent-create.spec.ts`: covers the create-agent journey from opening the page through name entry, section editing, submission, duplicate-name handling, slug normalization, whitespace/special-character cleanup, and failure handling when creation requests break.
- `tests/e2e/specs/agent-list.spec.ts`: checks that the sidebar reflects the current user agent list, shows newly created agents, supports navigation into agent detail pages, and tolerates empty or refreshed list states.
- `tests/e2e/specs/agent-detail.spec.ts`: covers worker-agent detail rendering, accessible header/config actions, activity-feed presence, Ask superagent CTA behavior, icon rendering, and 404 behavior for missing agents.
- `tests/e2e/specs/agent-config.spec.ts`: exercises opening the configuration sheet, selecting editable sections, changing persisted fields, saving updates, and confirming saved values survive reload/navigation.
- `tests/e2e/specs/navigation.spec.ts`: checks unsaved-changes guards, discard vs keep-editing decisions, mobile sidebar interactions, and deep-link loading directly into an agent page.
- `tests/e2e/specs/error-handling.spec.ts`: covers 404 behavior for missing agents, error banners when the agents API fails or is unreachable, concurrent-edit handling, and large-input tolerance.
- `tests/e2e/specs/api.spec.ts`: verifies the running app emits the expected list/create/update requests, JSON payloads, and content-type headers.
- `tests/e2e/specs/superagent.spec.ts`: checks the superagent home view, welcome message, model selector presence, model persistence, and navigation back to the root chat entry point.

## Main User Flows Covered

- Agent creation: open new agent page -> fill required or extended config -> submit -> land on the created agent using the backend-resolved name.
- Agent editing: open config sheet from detail page -> edit fields -> save -> reload or revisit -> confirm persisted state.
- Worker navigation: move between sidebar, worker pages, superagent, and deep links while preserving correct route behavior.
- Failure recovery: broken or unreachable API responses keep the app understandable instead of silently failing.

## Shared Test Infrastructure

- `tests/e2e/setup/fixtures.ts` re-exports the base Playwright test and `expect` so specs share a single import point.
- `playwright.config.ts` runs the built preview app against the Docker browser and points the frontend at the backend URL used during E2E runs.

## What This Does Not Try To Replace

- Playwright is for end-to-end confidence, not fine-grained component behavior.
- It does not replace Vitest coverage for component rendering details, local state transitions, or service-level unit contracts.

## Overlap With Vitest

- There is intentional overlap in feature areas like superagent, agent detail, agent creation, and agent configuration.
- The difference is level: Playwright proves the whole browser-to-app flow works together, while Vitest proves smaller units and mocked boundaries behave correctly.
- If a check can be expressed without real browser bootstrapping, routing, or network interaction, it usually belongs in Vitest instead of being duplicated here.

## Current Scope Snapshot

- E2E specs live under `tests/e2e/specs/*.spec.ts`.
- The stabilized suite currently covers 65 Playwright tests.
