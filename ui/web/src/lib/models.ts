/** Model catalog — the selector-facing module. */

import type {
	GetModelsResponse,
	ModelEntry,
	ModelGroup
} from '#lib/types/gen/apitypes.gen'

/** Selected model pointer; null = follow the default routing. */
export type ModelSelection = ModelEntry | null

export { modelsStore } from './models.svelte'
export type { GetModelsResponse, ModelEntry, ModelGroup }
