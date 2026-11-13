import type { IncomingMessage, ServerResponse } from 'node:http'
import { Readable } from 'node:stream'
import type { ReadableStream as WebReadableStream } from 'node:stream/web'
import adapter from '@sveltejs/adapter-static'
import { sveltekit } from '@sveltejs/kit/vite'
import tailwindcss from '@tailwindcss/vite'
import { playwright } from '@vitest/browser-playwright'
import { getResponse } from 'msw'
import { loadEnv, type Plugin } from 'vite'
import { defineConfig } from 'vitest/config'
import { createSeedData } from './src/lib/mocks/data.ts'
import { createHandlers } from './src/lib/mocks/handlers.ts'

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
			Readable.fromWeb(response.body as WebReadableStream).pipe(res)
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

export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, process.cwd(), ['AVA_', 'VITE_'])

	// Where the dev/preview servers proxy /api to (the client stays
	// same-origin): derived from the ava binary's listen address — the same
	// AVA_ADDR `just run ava` reads. An empty host (= all interfaces)
	// connects via localhost; the fallback mirrors the binary's default.
	const avaAddr = env.AVA_ADDR || ':8080'
	const backendOrigin = `http://${avaAddr.startsWith(':') ? `localhost${avaAddr}` : avaAddr}`
	const apiProxy = { '/api': { target: backendOrigin, changeOrigin: true } }
	const useMocks = env.VITE_USE_MOCKS === 'true'

	return {
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
		server: {
			proxy: apiProxy
		},
		preview: {
			host: '0.0.0.0',
			port: 4173,
			proxy: apiProxy
		},
		test: {
			expect: { requireAssertions: true },
			projects: [
				{
					extends: './vite.config.ts',
					test: {
						name: 'client',
						browser: {
							enabled: true,
							viewport: { width: 1280, height: 720 },
							provider: playwright(),
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
						setupFiles: ['tests/setup/fetch-base.ts'],
						include: ['tests/unit/**/*.{test,spec}.{js,ts}'],
						exclude: ['tests/unit/**/*.svelte.{test,spec}.{js,ts}']
					}
				}
			]
		}
	}
})
