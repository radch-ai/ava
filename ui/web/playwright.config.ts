import { defineConfig } from '@playwright/test'

export default defineConfig({
	webServer: {
		command: 'bun run build && bun run preview',
		port: 4173,
		timeout: 180_000,
		reuseExistingServer: true
	},
	testDir: 'tests/e2e/specs',
	testIgnore: ['**/testutil/**/*.test.ts', '**/*.test.ts'],
	reporter: 'list',
	workers: 1,
	use: {
		baseURL: 'http://127.0.0.1:4173'
	}
})
