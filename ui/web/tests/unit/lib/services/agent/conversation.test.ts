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
})
