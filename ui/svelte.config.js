// Tauri doesn't have a Node.js server to do proper SSR
// so we use adapter-static with a fallback to index.html to put the site in SPA mode
// See: https://svelte.dev/docs/kit/single-page-apps
// See: https://v2.tauri.app/start/frontend/sveltekit/ for more info

import { relative, sep } from 'node:path'
import adapter from '@sveltejs/adapter-static'
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte'

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	compilerOptions: {
		// Force runes mode for all project files; node_modules falls back to autodetect
		// so legacy-mode deps keep compiling. Needed on Svelte 5 (which supports both
		// modes and auto-detects per-file). Remove once a Svelte release makes runes
		// the only mode.
		runes: ({ filename }) => {
			const relativePath = relative(import.meta.dirname, filename)
			const pathSegments = relativePath.toLowerCase().split(sep)
			const isExternalLibrary = pathSegments.includes('node_modules')

			return isExternalLibrary ? undefined : true
		}
	},
	kit: {
		adapter: adapter({
			fallback: 'index.html'
		}),
		alias: {
			$components: './src/components'
		}
	}
}

export default config
