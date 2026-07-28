import type { KnipConfig } from 'knip'

export default {
	// @tauri-apps/plugin-opener: paired with the Rust `tauri-plugin-opener` host
	// plugin; JS bindings will be consumed once UI features call into it.
	// @testing-library/user-event: staged for component tests; no consumer yet.
	ignoreDependencies: [
		'@tauri-apps/plugin-opener',
		'@testing-library/user-event'
	]
} satisfies KnipConfig
