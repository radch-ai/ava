/**
 * Playwright test fixtures.
 *
 * Re-exports the base Playwright test so specs share a single import point.
 * Every test starts authenticated: the fixture exchanges a UI token for the
 * `ava_session` cookie via `context.request` (shared cookie jar), so specs
 * enter the app directly instead of landing on the login page.
 *
 * - Mock runs (`VITE_USE_MOCKS=true`): the seeded `ava-mock-token`.
 * - Real-backend runs: `AVA_UI_TOKEN` — the same var the backend itself
 *   reads, so one shell exporting it can run both `just run ava` and e2e.
 */

import { test as base } from '@playwright/test'

const useMocks = process.env.VITE_USE_MOCKS === 'true'
const appBaseUrl = 'http://127.0.0.1:4173'
const uiToken = useMocks ? 'ava-mock-token' : process.env.AVA_UI_TOKEN

export const test = base.extend<{
	/** Exchanged once per test; `context.request` shares the cookie jar. */
	session: undefined
}>({
	session: [
		async ({ context }, use) => {
			if (!uiToken) {
				throw new Error(
					'real-backend e2e needs AVA_UI_TOKEN (the token the backend was started with) — or run with VITE_USE_MOCKS=true'
				)
			}
			await context.request.post(`${appBaseUrl}/api/auth/exchange`, {
				data: { token: uiToken }
			})
			await use(undefined)
		},
		{ auto: true }
	]
})

export { expect } from '@playwright/test'
