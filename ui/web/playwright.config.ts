import 'dotenv/config';
import { defineConfig } from '@playwright/test';

const backendApiUrl = process.env.E2E_BACKEND_API_URL || 'http://127.0.0.1:8080';
const chromiumWsUrl = process.env.TEST_CHROMIUM_WS_URL || 'ws://127.0.0.1:9223';
const baseUrl = process.env.E2E_APP_BASE_URL || 'http://127.0.0.1:4173';

export default defineConfig({
	webServer: {
		command: `VITE_BACKEND_API_URL=${backendApiUrl} bun run build && VITE_BACKEND_API_URL=${backendApiUrl} bun run preview`,
		port: 4173,
		timeout: 180_000,
		reuseExistingServer: true
	},
	testDir: 'tests/e2e/specs',
	testIgnore: ['**/testutil/**/*.test.ts', '**/*.test.ts'],
	reporter: 'list',
	workers: 1,
	use: {
		baseURL: baseUrl,
		connectOptions: {
			wsEndpoint: chromiumWsUrl
		}
	}
});
