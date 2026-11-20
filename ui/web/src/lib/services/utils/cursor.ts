export interface CursorPage<T> {
	items: T[]
	nextCursor: string | null
}

/**
 * Merges a past (older) cursor page with a current (newer) cursor page.
 * Deduplicates items by key, keeping the first occurrence (from past
 * page).
 *
 * @param current - The newer page with fresher data
 * @param past - The older page to prepend
 * @param keyFn - The identity of one item; defaults to its id. Pass the
 * display key when ids live in more than one namespace.
 * @returns A merged page with past items first, then current items (deduplicated)
 */
export function mergePastPages<T extends { id: string }>(
	current: CursorPage<T>,
	past: CursorPage<T>,
	keyFn: (item: T) => string = (item) => item.id
): CursorPage<T> {
	const seen = new Set<string>()
	const items = [...past.items, ...current.items].filter((item) => {
		const key = keyFn(item)
		if (seen.has(key)) return false
		seen.add(key)
		return true
	})
	return { items, nextCursor: past.nextCursor }
}
