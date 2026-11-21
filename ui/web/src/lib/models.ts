/** Model catalog — the selector-facing module. */

import type {
	GetModelsResponse,
	ModelEntry,
	ModelGroup
} from '#lib/types/gen/apitypes.gen'

/** Selected model pointer; null = follow the default routing. */
export type ModelSelection = ModelEntry | null

/** A selection only counts when it names a catalog entry's two
 * strings; anything else a storage or key round-trip produced is
 * default routing. */
export function isModelSelection(value: unknown): value is ModelSelection {
	if (value === null) return true
	return (
		typeof value === 'object' &&
		typeof (value as ModelEntry).id === 'string' &&
		typeof (value as ModelEntry).providerId === 'string'
	)
}

export { modelsStore } from './models.svelte'
export type { GetModelsResponse, ModelEntry, ModelGroup }
