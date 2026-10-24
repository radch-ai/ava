export interface CursorPage<T> {
	items: T[]
	nextCursor: string | null
}

/**
 * Merges a past (older) cursor page with a current (newer) cursor page.
 * Deduplicates items by ID, keeping the first occurrence (from past page).
 *
 * @param current - The newer page with fresher data
 * @param past - The older page to prepend
 * @returns A merged page with past items first, then current items (deduplicated)
 */
export function mergePastPages<T extends { id: string }>(
	current: CursorPage<T>,
	past: CursorPage<T>
): CursorPage<T> {
	const items = [...past.items, ...current.items].filter(
		(item, index, all) =>
			all.findIndex((candidate) => candidate.id === item.id) === index
	)
	return { items, nextCursor: past.nextCursor }
}
