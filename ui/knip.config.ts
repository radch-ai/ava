import type { KnipConfig } from 'knip'

export default {
	// shadcn-svelte primitives — public component surface by design
	// is-mobile hook: reactive breakpoint helper, consumed by Sidebar + app code
	ignore: [
		'src/components/ui/**',
		'src/lib/shadcn/utils.ts',
		'src/lib/hooks/is-mobile.svelte.ts'
	],
	// @tauri-apps/plugin-opener: paired with the Rust `tauri-plugin-opener` host
	// plugin; JS bindings will be consumed once UI features call into it.
	// @testing-library/user-event: staged for component tests; no consumer yet.
	ignoreDependencies: [
		'@tauri-apps/plugin-opener',
		'@testing-library/user-event',
		'@lucide/svelte'
	]
} satisfies KnipConfig
