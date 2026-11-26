import { describe, expect, it, vi } from 'vitest'
import type { AgentSocketEvent, AgentSocketState } from '#lib/services/agent/ws'
import type { ThreadMessage } from '#lib/types/agent'

// The store's socket is a recording fake; the thread fetch is gated so a
// loadMore can straddle a newer snapshot — the race the epoch guard
// closes.

const captured = vi.hoisted(() => ({
	onEvent: [] as ((event: AgentSocketEvent) => void)[],
	get: vi.fn()
}))

vi.mock('#lib/services/agent/ws', () => ({
	createAgentSocket: vi.fn(
		(
			_agentId: string,
			onEvent: (event: AgentSocketEvent) => void,
			_onState: (state: AgentSocketState) => void
		) => {
			captured.onEvent.push(onEvent)
			return {
				sendPrompt: vi.fn(),
				sendNow: vi.fn(),
				sendInterrupt: vi.fn(),
				close: vi.fn()
			}
		}
	)
}))

vi.mock('#lib/services/api', () => ({ api: { get: captured.get } }))

import { createConversation } from '#lib/services/agent/conversation.svelte'

function item(id: string, content: string): ThreadMessage {
	return {
		id,
		role: 'user',
		content,
		createdAt: '2026-11-21T00:00:00.000Z'
	}
}

interface ThreadPage {
	threadId: string
	items: ThreadMessage[]
	nextCursor: string | null
}

function page(
	threadId: string,
	items: ThreadMessage[],
	nextCursor: string | null
): ThreadPage {
	return { threadId, items, nextCursor }
}

describe('createConversation loadMore', () => {
	it('merges an older page above the current segment on the happy path', async () => {
		captured.get.mockImplementationOnce(() => ({
			json: async () => page('t-1', [item('old-1', 'older')], null)
		}))
		const store = createConversation('a', {
			thread: page('t-1', [item('m-1', 'newest')], 'c-1')
		})
		await store.loadMore()
		expect(store.threadItems.map((m) => m.content)).toEqual(['older', 'newest'])
		expect(store.historyCursor).toBeNull()
		expect(store.isLoadingMore).toBe(false)
	})

	it('discards a page that lands after a newer snapshot replaced the projection', async () => {
		let release: (() => void) | undefined
		captured.get.mockImplementationOnce(() => ({
			json: async () => {
				await new Promise<void>((resolve) => {
					release = resolve
				})
				return page('t-1', [item('old-1', 'stale page')], 'c-0')
			}
		}))
		const store = createConversation('a', {
			thread: page('t-1', [item('m-1', 'before')], 'c-1')
		})

		const pending = store.loadMore()
		// While the page is in flight, the authoritative baseline
		// replaces the projection and its cursor.
		captured.onEvent.at(-1)?.({
			type: 'sync_snapshot',
			data: {
				cursor: 'v1:h:9',
				page: page('t-2', [item('m-9', 'snapshot row')], 'c-2')
			}
		})
		release?.()
		await pending

		expect(store.threadItems.map((m) => m.content)).toEqual(['snapshot row'])
		expect(store.historyCursor).toBe('c-2')
		expect(store.isLoadingMore).toBe(false)
	})
})

describe('createConversation committed-row visibility', () => {
	it('never places a structure-only empty assistant row — the marker predicate must not pin it as a permanent Thinking stand-in', () => {
		const store = createConversation('a', {
			thread: page('t-1', [item('m-1', 'before')], null)
		})
		captured.onEvent.at(-1)?.({
			type: 'lifecycle',
			data: {
				cursor: 'v1:h:3',
				events: [
					{
						kind: 'message_committed',
						itemId: 'turn-1',
						threadId: 't-1',
						pos: 3,
						batchEnd: true,
						rows: [{ id: 'struct-1', role: 'assistant', content: '' }]
					}
				]
			}
		})
		expect(store.threadItems.map((m) => m.id)).toEqual(['m-1'])

		// The round's visible rows still place, and a later snapshot
		// converges on the same projection.
		captured.onEvent.at(-1)?.({
			type: 'lifecycle',
			data: {
				cursor: 'v1:h:4',
				events: [
					{
						kind: 'tool_finished',
						itemId: 'turn-1',
						threadId: 't-1',
						invocationId: 'inv-1',
						pos: 4,
						batchEnd: true,
						rows: [
							{
								id: 'tool-1',
								role: 'tool',
								content: '{"name":"ddg","result":"ok"}'
							}
						]
					}
				]
			}
		})
		expect(store.threadItems.map((m) => m.id)).toEqual(['m-1', 'tool-1'])
	})

	it('places the rows an itemless rotation seeds on thread_changed — the summary lands with its boundary', () => {
		const store = createConversation('a', {
			thread: page('t-1', [item('m-1', 'before')], null)
		})
		captured.onEvent.at(-1)?.({
			type: 'lifecycle',
			data: {
				cursor: 'v1:h:5',
				events: [
					{
						kind: 'thread_changed',
						itemId: 'turn-2',
						threadId: 't-2',
						pos: 5,
						batchEnd: true,
						divider: {
							id: 'd-1',
							role: 'divider',
							content: 'Compacted'
						},
						rows: [{ id: 'sum-1', role: 'assistant', content: 'auto summary' }]
					}
				]
			}
		})
		// The seeded summary renders below the boundary it opened —
		// without this placement a live client sees only the divider
		// until a full reload.
		expect(store.threadItems.map((m) => m.id)).toEqual(['m-1', 'd-1', 'sum-1'])
		expect(store.threadItems[2]?.content).toBe('auto summary')
	})
})

describe('createConversation conversation_refresh', () => {
	function refresh(
		threadId: string,
		rows: Array<
			Partial<ThreadMessage> & { id: string; role: string; content: string }
		>,
		nextCursor: string | null
	) {
		return {
			kind: 'conversation_refresh',
			itemId: 'turn-1',
			threadId,
			pos: 7,
			batchEnd: true,
			page: {
				threadId,
				items: rows.map((r) => ({
					createdAt: '2026-11-21T00:00:00.000Z',
					...r
				})),
				nextCursor
			}
		}
	}

	it('replaces the projection and its cursor while the live tail survives', () => {
		const store = createConversation('a', {
			thread: page('t-1', [item('m-1', 'before')], 'c-1')
		})
		// The running turn streams on below whatever the display caches.
		captured.onEvent.at(-1)?.({
			type: 'message_chunk',
			data: { id: 'live-1', delta: 'streaming answer', seq: 1 }
		})
		expect(store.liveItems.map((m) => m.content)).toEqual(['streaming answer'])

		captured.onEvent.at(-1)?.({
			type: 'lifecycle',
			data: {
				cursor: 'v1:h:7',
				events: [
					refresh(
						't-2',
						[
							{ id: 'd-1', role: 'divider', content: 'Compacted' },
							{ id: 'sum-1', role: 'assistant', content: 'the summary' },
							{ id: 'ref-1', role: 'user', content: 'the prompt' }
						],
						'c-2'
					)
				]
			}
		})

		expect(store.threadItems.map((m) => m.id)).toEqual([
			'd-1',
			'sum-1',
			'ref-1'
		])
		expect(store.historyCursor).toBe('c-2')
		// The refresh never touches the live tail: only sync_ready
		// replaces it.
		expect(store.liveItems.map((m) => m.content)).toEqual(['streaming answer'])
	})

	it('discards an in-flight older page that straddles the refresh', async () => {
		let release: (() => void) | undefined
		captured.get.mockImplementationOnce(() => ({
			json: async () => {
				await new Promise<void>((resolve) => {
					release = resolve
				})
				return page('t-1', [item('old-1', 'stale page')], 'c-0')
			}
		}))
		const store = createConversation('a', {
			thread: page('t-1', [item('m-1', 'before')], 'c-1')
		})
		const pending = store.loadMore()
		captured.onEvent.at(-1)?.({
			type: 'lifecycle',
			data: {
				cursor: 'v1:h:7',
				events: [refresh('t-2', [item('n-1', 'fresh')], 'c-2')]
			}
		})
		release?.()
		await pending
		expect(store.threadItems.map((m) => m.content)).toEqual(['fresh'])
		expect(store.historyCursor).toBe('c-2')
	})

	it('applies repeated refreshes last-wins in arrival order', () => {
		const store = createConversation('a', { thread: null })
		captured.onEvent.at(-1)?.({
			type: 'lifecycle',
			data: {
				cursor: 'v1:h:7',
				events: [refresh('t-2', [item('a-1', 'first')], null)]
			}
		})
		captured.onEvent.at(-1)?.({
			type: 'lifecycle',
			data: {
				cursor: 'v1:h:8',
				events: [refresh('t-3', [item('b-1', 'second')], null)]
			}
		})
		expect(store.threadItems.map((m) => m.content)).toEqual(['second'])
	})

	it('drops the event when the page is malformed and keeps the view unchanged', () => {
		const store = createConversation('a', {
			thread: page('t-1', [item('m-1', 'before')], 'c-1')
		})
		captured.onEvent.at(-1)?.({
			type: 'lifecycle',
			data: {
				cursor: 'v1:h:7',
				events: [
					{
						kind: 'conversation_refresh',
						itemId: 'turn-1',
						threadId: 't-2',
						pos: 7,
						batchEnd: true,
						page: { threadId: 't-2', items: 'not-an-array' }
					}
				]
			}
		})
		expect(store.threadItems.map((m) => m.id)).toEqual(['m-1'])
		expect(store.historyCursor).toBe('c-1')
	})
})
