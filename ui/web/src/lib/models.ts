/**
 * Model catalog — the selector-facing module.
 *
 * `defaultModel` is the reserved default-routing id ('auto', the
 * seeded catalog row); it exists before any fetch and is what
 * selectors fall back to. The catalog itself is user-saved via the
 * API: `modelsStore` loads it from /api/providers and the providers
 * page edits it. Group entries are plain model ids.
 */

export interface ModelGroup {
	provider: string
	models: string[]
}

export const defaultModel = 'auto'

export { modelsStore } from './models.svelte'
