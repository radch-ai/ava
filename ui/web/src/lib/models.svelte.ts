/**
 * Model-catalog store — the selector-facing shape of GET
 * /api/models. Selectors render the reserved default ('auto') plus
 * these groups; the providers page edits the catalog and calls
 * refresh().
 */

import { getModels } from '#lib/services/providers'
import type { ModelGroup } from './models'

export class ModelsStore {
	/** Provider-name groups in server order; empty until loaded. */
	groups = $state<ModelGroup[]>([])
	loaded = $state(false)
	refreshing = $state(false)
	error = $state<string | null>(null)
	#inflight: Promise<void> | null = null

	/** Load once for a session; concurrent callers share the
	 * in-flight request. */
	async ensureLoaded(): Promise<void> {
		if (this.loaded) return
		if (!this.#inflight) this.#inflight = this.#fetch()
		return this.#inflight
	}

	/** Reload after catalog edits on the providers page. */
	async refresh(): Promise<void> {
		if (!this.#inflight) this.#inflight = this.#fetch()
		return this.#inflight
	}

	async #fetch(): Promise<void> {
		this.refreshing = true
		try {
			const { groups } = await getModels()
			this.groups = groups ?? []
			this.loaded = true
			this.error = null
		} catch (e) {
			this.error = e instanceof Error ? e.message : 'Failed to load models'
		} finally {
			this.refreshing = false
			this.#inflight = null
		}
	}
}

/** The app-wide catalog store. */
export const modelsStore = new ModelsStore()
