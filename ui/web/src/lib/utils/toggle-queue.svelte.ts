import { SvelteMap, SvelteSet } from 'svelte/reactivity'

import { errorMessage } from '#lib/utils/errors'

/**
 * One debounced, per-row toggle queue shared by every tool switch UI.
 *
 * A click flips the row's desired state instantly (target); the request
 * fires once the burst settles. A row is disabled while its request is
 * in flight, so a second mechanism for "clicks that land mid-flight" is
 * deliberately absent — the disabled switch is the only guard.
 */

/** How long a burst of clicks collapses into one request. */
const DEBOUNCE_MS = 250

export interface ToggleQueueOptions<T extends { id: string }> {
	/** Persist one row's desired state; resolve with the confirmed value. */
	apply: (row: T, enabled: boolean) => Promise<boolean>
	/** The row's last confirmed state. */
	persisted: (row: T) => boolean
	/** Resolve a queued row id back to the row currently rendered. */
	rowFor: (id: string) => T | undefined
	/** Surface a failure message (null clears it). */
	onError: (message: string | null) => void
}

export interface ToggleQueue<T extends { id: string }> {
	/** Render value for a row: a queued target when set, else confirmed. */
	displayed: (row: T) => boolean
	toggle: (row: T) => void
	/** Rows with a request in flight — bind the switch's disabled to it. */
	flushing: SvelteSet<string>
	/** Flush pending targets and drop timers (unmount cleanup). */
	teardown: () => void
	/** Drop queued state for rows that no longer exist. */
	prune: (live: (id: string) => boolean) => void
}

export function createToggleQueue<T extends { id: string }>(
	opts: ToggleQueueOptions<T>
): ToggleQueue<T> {
	const target = new SvelteMap<string, boolean>()
	const flushing = new SvelteSet<string>()
	const timers = new Map<string, number>()

	function displayed(row: T): boolean {
		return target.get(row.id) ?? opts.persisted(row)
	}

	function schedule(id: string): void {
		if (timers.has(id)) return
		timers.set(
			id,
			window.setTimeout(() => {
				timers.delete(id)
				void flush(id)
			}, DEBOUNCE_MS)
		)
	}

	function toggle(row: T): void {
		target.set(row.id, !displayed(row))
		schedule(row.id)
	}

	async function flush(id: string): Promise<void> {
		if (flushing.has(id)) return
		const want = target.get(id)
		const row = opts.rowFor(id)
		if (!row || want === undefined || want === opts.persisted(row)) {
			target.delete(id)
			return
		}
		flushing.add(id)
		try {
			const confirmed = await opts.apply(row, want)
			if (target.get(id) === confirmed) target.delete(id)
			opts.onError(null)
		} catch (e) {
			// Back to the confirmed state; only the failure surfaces.
			target.delete(id)
			opts.onError(errorMessage(e, 'Failed to update tool'))
		} finally {
			flushing.delete(id)
		}
	}

	function teardown(): void {
		for (const timer of timers.values()) window.clearTimeout(timer)
		timers.clear()
		for (const [id, want] of target) {
			const row = opts.rowFor(id)
			if (row && want !== opts.persisted(row)) void flush(id)
		}
	}

	function prune(live: (id: string) => boolean): void {
		for (const id of [...target.keys()]) if (!live(id)) target.delete(id)
		for (const [id, timer] of timers) {
			if (live(id)) continue
			window.clearTimeout(timer)
			timers.delete(id)
		}
	}

	return { displayed, toggle, flushing, teardown, prune }
}
