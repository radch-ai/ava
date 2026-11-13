/**
 * API Client
 *
 * A configured ky instance for backend API requests.
 *
 * ## What ky handles by default (no manual code needed):
 * - Auto-parses JSON responses via `.json()` method
 * - Throws HTTPError on non-2xx responses
 * - Stringifies request bodies to JSON
 * - Handles query string serialization
 * - Built-in retry mechanism (configurable)
 * - Proper FormData handling
 * - Response compression (gzip, brotli, deflate)
 *
 * ## Our additions:
 * - Error body extraction for better error messages
 * - Request timeout
 * - Auth signal on 401
 */

import type { ResponsePromise } from 'ky'
import ky from 'ky'
import { authStore } from '#lib/auth'

/** What every `api.*` request method returns: a chainable ky response promise. */
export type ApiResponsePromise = ResponsePromise

/**
 * Configured ky instance for API requests
 */
export const api = ky.create({
	// Same-origin /api: proxied by the vite dev/preview servers; prod serves
	// UI and API from the single binary.
	prefixUrl: '/api',

	// 30 second timeout - prevents hung requests
	timeout: 30000,

	// Send cookies: the auth session cookie is the credential
	credentials: 'same-origin',

	// Only retry GET requests, never mutations (POST/PATCH/PUT/DELETE)
	retry: {
		limit: 2,
		methods: ['GET']
	},
	hooks: {
		// Signal auth on 401; let ky raise HTTPError (its retry defaults
		// exclude 401, so this stays single-shot)
		afterResponse: [
			async (_request, _options, response) => {
				if (response.status === 401) {
					// Session gone mid-app: flip the store so the layout guard
					// sends the user back to the login surface
					authStore.handleSessionExpired()
				}
				return response
			}
		],

		// Extract the server's error message into the thrown error
		beforeError: [
			async (error) => {
				const body = (await error.response
					.clone()
					.json()
					.catch(() => ({}))) as {
					message?: string
					error?: string
				}
				error.message = body.message || body.error || error.message
				return error
			}
		]
	}
})
