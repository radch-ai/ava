import 'dotenv/config'
import type { IncomingMessage, ServerResponse } from 'node:http'
import { Readable } from 'node:stream'
import adapter from '@sveltejs/adapter-static'
import { sveltekit } from '@sveltejs/kit/vite'
import tailwindcss from '@tailwindcss/vite'
import { playwright } from '@vitest/browser-playwright'
import { getResponse } from 'msw'
import type { Plugin } from 'vite'
import { defineConfig } from 'vitest/config'
import { createSeedData } from './src/lib/mocks/data'
import { createHandlers } from './src/lib/mocks/handlers'

const allowedHosts = new Set<string>()
const e2eAppBaseUrl = process.env.E2E_APP_BASE_URL || 'http://127.0.0.1:4173'

try {
	allowedHosts.add(new URL(e2eAppBaseUrl).hostname)
} catch {
	// Ignore invalid URLs here; Playwright config validation will surface the real issue.
}

type ConnectNext = (err?: unknown) => void

/**
 * Serve the mock handlers from `src/lib/mocks` as Connect middleware on the
 * vite dev/preview servers. Requests no handler matches fall through to vite.
 */
function mockApiMiddleware() {
	const handlers = createHandlers(createSeedData())
	return async (
		req: IncomingMessage,
		res: ServerResponse,
		next: ConnectNext
	) => {
		const method = req.method ?? 'GET'
		const hasBody = method !== 'GET' && method !== 'HEAD'
		const request = new Request(
			new URL(req.url ?? '/', `http://${req.headers.host}`),
			{
				method,
				headers: new Headers(req.headers as Record<string, string>),
				...(hasBody
					? {
							duplex: 'half',
							body: Readable.toWeb(req) as unknown as ReadableStream<Uint8Array>
						}
					: {})
			}
		)

		let response: Response | undefined
		try {
			response = await getResponse(handlers, request)
		} catch (error) {
			console.error('[ava-api-mocks] handler error:', error)
			res.statusCode = 500
			res.setHeader('content-type', 'application/json')
			res.end(JSON.stringify({ message: 'Mock handler error' }))
			return
		}

		if (!response) {
			next()
			return
		}

		res.statusCode = response.status
		response.headers.forEach((value, name) => {
			res.setHeader(name, value)
		})
		if (response.body) {
			Readable.fromWeb(
				response.body as import('node:stream/web').ReadableStream
			).pipe(res)
		} else {
			res.end()
		}
	}
}

/**
 * Mock API: mounts the deterministic handlers from `src/lib/mocks` on the
 * vite dev and preview servers so the UI runs without a backend. State is
 * in-memory; restarting the server reseeds it. Enabled by VITE_USE_MOCKS=true.
 */
function avaApiMocks(): Plugin {
	return {
		name: 'ava-api-mocks',
		configureServer(server) {
			server.middlewares.use(mockApiMiddleware())
		},
		configurePreviewServer(server) {
			server.middlewares.use(mockApiMiddleware())
		}
	}
}

const useMocks = process.env.VITE_USE_MOCKS === 'true'

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			adapter: adapter({
				fallback: '200.html'
			}),
			compilerOptions: {
				runes: ({ filename }) =>
					filename.includes('node_modules') ? undefined : true
			}
		}),
		...(useMocks ? [avaApiMocks()] : [])
	],
	preview: {
		host: '0.0.0.0',
		port: 4173,
		allowedHosts: [...allowedHosts]
	},
	test: {
		env: {
			VITE_BACKEND_API_URL:
				process.env.VITE_BACKEND_API_URL ?? 'http://localhost:8080'
		},
		expect: { requireAssertions: true },
		projects: [
			{
				extends: './vite.config.ts',
				test: {
					name: 'client',
					browser: {
						enabled: true,
						provider: playwright({
							connectOptions: {
								wsEndpoint:
									process.env.TEST_CHROMIUM_WS_URL || 'ws://127.0.0.1:9223',
								exposeNetwork: '<loopback>'
							}
						}),
						instances: [{ browser: 'chromium', headless: true }]
					},
					include: ['tests/unit/**/*.svelte.{test,spec}.{js,ts}']
				}
			},

			{
				extends: './vite.config.ts',
				test: {
					name: 'server',
					environment: 'node',
					include: ['tests/unit/**/*.{test,spec}.{js,ts}'],
					exclude: ['tests/unit/**/*.svelte.{test,spec}.{js,ts}']
				}
			}
		]
	}
})
