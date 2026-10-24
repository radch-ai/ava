import { describe, expect, it } from 'vitest'
import { type CursorPage, mergePastPages } from '#lib/services/utils/cursor'

interface TestItem {
	id: string
	value: string
}

function createPage(
	items: TestItem[],
	opts: { nextCursor?: string | null } = {}
): CursorPage<TestItem> {
	return {
		items,
		nextCursor: opts.nextCursor ?? null
	}
}

describe('mergePastPages', () => {
	it('merges two pages without duplicates', () => {
		const current = createPage([
			{ id: '3', value: 'item3' },
			{ id: '4', value: 'item4' }
		])

		const past = createPage([
			{ id: '1', value: 'item1' },
			{ id: '2', value: 'item2' }
		])

		const result = mergePastPages(current, past)

		expect(result.items).toEqual([
			{ id: '1', value: 'item1' },
			{ id: '2', value: 'item2' },
			{ id: '3', value: 'item3' },
			{ id: '4', value: 'item4' }
		])
	})

	it('deduplicates items by id, keeping first occurrence from past', () => {
		const current = createPage([
			{ id: '3', value: 'current3' },
			{ id: '4', value: 'current4' }
		])

		const past = createPage([
			{ id: '1', value: 'past1' },
			{ id: '2', value: 'past2' },
			{ id: '3', value: 'past3' }, // Duplicate id '3'
			{ id: '4', value: 'past4' } // Duplicate id '4'
		])

		const result = mergePastPages(current, past)

		// Should keep past versions of duplicated items
		expect(result.items).toEqual([
			{ id: '1', value: 'past1' },
			{ id: '2', value: 'past2' },
			{ id: '3', value: 'past3' },
			{ id: '4', value: 'past4' }
		])
	})

	it('returns current page when past is empty', () => {
		const current = createPage([{ id: '1', value: 'item1' }])
		const past = createPage([])

		const result = mergePastPages(current, past)

		expect(result.items).toEqual([{ id: '1', value: 'item1' }])
		expect(result.nextCursor).toBeNull()
	})

	it('returns past items when current is empty', () => {
		const current = createPage([])
		const past = createPage([{ id: '1', value: 'item1' }])

		const result = mergePastPages(current, past)

		expect(result.items).toEqual([{ id: '1', value: 'item1' }])
	})

	it('handles both pages being empty', () => {
		const current = createPage([])
		const past = createPage([])

		const result = mergePastPages(current, past)

		expect(result.items).toEqual([])
		expect(result.nextCursor).toBeNull()
	})

	it('uses past page cursor metadata', () => {
		const current = createPage([{ id: '3', value: 'item3' }])

		const past = createPage([{ id: '1', value: 'item1' }], {
			nextCursor: 'past-cursor'
		})

		const result = mergePastPages(current, past)

		expect(result.nextCursor).toBe('past-cursor')
	})

	it('handles last page (nextCursor: null) from past', () => {
		const current = createPage([{ id: '3', value: 'item3' }])

		const past = createPage([{ id: '1', value: 'item1' }], {
			nextCursor: null
		})

		const result = mergePastPages(current, past)

		expect(result.nextCursor).toBeNull()
	})

	it('preserves order: past items first, then current items', () => {
		const current = createPage([
			{ id: '10', value: 'z' },
			{ id: '20', value: 'y' }
		])

		const past = createPage([
			{ id: '1', value: 'a' },
			{ id: '2', value: 'b' },
			{ id: '3', value: 'c' }
		])

		const result = mergePastPages(current, past)

		expect(result.items.map((i) => i.value)).toEqual(['a', 'b', 'c', 'z', 'y'])
	})

	it('handles all duplicate items', () => {
		const current = createPage([
			{ id: '1', value: 'current1' },
			{ id: '2', value: 'current2' }
		])

		const past = createPage([
			{ id: '1', value: 'past1' },
			{ id: '2', value: 'past2' }
		])

		const result = mergePastPages(current, past)

		expect(result.items).toEqual([
			{ id: '1', value: 'past1' },
			{ id: '2', value: 'past2' }
		])
		expect(result.items).toHaveLength(2)
	})
})
