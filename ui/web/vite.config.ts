import 'dotenv/config';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vitest/config';
import { sveltekit } from '@sveltejs/kit/vite';
import { playwright } from '@vitest/browser-playwright';

const allowedHosts = new Set<string>();
const e2eAppBaseUrl = process.env.E2E_APP_BASE_URL || 'http://127.0.0.1:4173';

try {
	allowedHosts.add(new URL(e2eAppBaseUrl).hostname);
} catch {
	// Ignore invalid URLs here; Playwright config validation will surface the real issue.
}

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	preview: {
		host: '0.0.0.0',
		port: 4173,
		allowedHosts: [...allowedHosts]
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
						provider: playwright({
							connectOptions: {
								wsEndpoint: process.env.TEST_CHROMIUM_WS_URL || 'ws://127.0.0.1:9223',
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
});
