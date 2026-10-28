/**
 * Playwright test fixtures.
 *
 * Re-exports the base Playwright test so specs share a single import point.
 *
 * When the preview runs with `VITE_USE_MOCKS=true`, every test gets the mock
 * session up front by exchanging the seeded token (`ava-mock-token`) for the
 * `ava_session` cookie, so specs enter the app directly instead of landing
 * on the login page. Real-backend runs are unaffected.
 */

import { test as base } from '@playwright/test'

const useMocks = process.env.VITE_USE_MOCKS === 'true'
const appBaseUrl = process.env.E2E_APP_BASE_URL || 'http://127.0.0.1:4173'

export const test = base.extend<{
	/** Exchanged once per test; `context.request` shares the cookie jar. */
	mockSession: undefined
}>({
	mockSession: [
		async ({ context }, use) => {
			if (useMocks) {
				await context.request.post(`${appBaseUrl}/api/auth/exchange`, {
					data: { token: 'ava-mock-token' }
				})
			}
			await use(undefined)
		},
		{ auto: true }
	]
})

export { expect } from '@playwright/test'
