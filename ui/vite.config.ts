import { sveltekit } from '@sveltejs/kit/vite'
import { svelteTesting } from '@testing-library/svelte/vite'
import { defineConfig } from 'vitest/config'

const host = process.env.TAURI_DEV_HOST

// https://vite.dev/config/
export default defineConfig({
	plugins: [sveltekit(), svelteTesting({ autoCleanup: false })],

	// Vite options tailored for Tauri development and only applied in `tauri dev` or `tauri build`
	//
	// 1. prevent Vite from obscuring rust errors
	clearScreen: false,
	// 2. tauri expects a fixed port, fail if that port is not available
	server: {
		port: 1420,
		strictPort: true,
		host: host || false,
		hmr: host
			? {
					protocol: 'ws',
					host,
					port: 1421
				}
			: undefined,
		watch: {
			// 3. tell Vite to ignore watching `src-tauri`
			ignored: ['**/src-tauri/**']
		}
	},

	test: {
		// Two lanes: pure-logic tests run without a DOM; `*.svelte.*` specs and
		// `*.tauri.*` specs run with happy-dom because they touch `window`
		// (rendering, mockIPC's window.__TAURI_INTERNALS__).
		projects: [
			{
				extends: './vite.config.ts',
				test: {
					name: 'unit',
					environment: 'node',
					include: ['src/lib/**/*.{test,spec}.{js,ts}'],
					exclude: ['src/lib/**/*.tauri.{test,spec}.{js,ts}']
				}
			},
			{
				extends: './vite.config.ts',
				test: {
					name: 'client',
					environment: 'happy-dom',
					include: [
						'src/**/*.svelte.{test,spec}.{js,ts}',
						'src/**/*.tauri.{test,spec}.{js,ts}'
					],
					setupFiles: ['./src/vitest-setup.ts']
				}
			}
		]
	}
})
