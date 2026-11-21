import { describe, expect, it } from 'vitest'

import { createLiveTurns } from '#lib/services/agent/live-turn.svelte'
import type { AgentSocketEvent } from '#lib/services/agent/ws'
import type { ToolInvocationState } from '#lib/types/gen/wstypes.gen'

let chunkSeq = 0

function chunk(id: string, delta: string): AgentSocketEvent {
	chunkSeq += 1
	return {
		type: 'message_chunk',
		data: { id, delta, seq: chunkSeq }
	}
}

function laneStatus(working: boolean, reasoning = false): AgentSocketEvent {
	return {
		type: 'lane_status',
		data: {
			active: true,
			working,
			reasoning,
			model: ''
		}
	}
}

function errorEvent(
	message: string,
	tool?: string,
	ids?: { turnId?: string }
): AgentSocketEvent {
	return {
		type: 'error',
		data: { message, tool, turn_id: ids?.turnId }
	}
}

function invocation(
	invocationId: string,
	overrides: Partial<ToolInvocationState> = {}
): ToolInvocationState {
	return {
		invocation_id: invocationId,
		name: 'web_search',
		args: '{"query":"otters"}',
		pending: true,
		...overrides
	}
}

describe('live turns', () => {
	it('accumulates chunk deltas into one assistant message', () => {
		const live = createLiveTurns()
		live.applyEvent(chunk('msg-1', 'hel'))
		live.applyEvent(chunk('msg-1', 'lo'))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'msg-1',
			role: 'assistant',
			content: 'hello'
		})
	})

	it('an error event appends an error message with the reason', () => {
		const live = createLiveTurns()
		live.applyEvent(errorEvent('read default model: no rows'))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0].role).toBe('error')
		expect(live.messages[0].content).toBe('read default model: no rows')
	})

	it('a transient failure stays local — durable errors ride lifecycle rows', () => {
		// The wire's error event is transient-only now: a stored failure
		// arrives as an item_finished row through the surface's
		// lifecycle reducer, so the live tail never mints its id.
		const live = createLiveTurns()
		live.applyEvent(errorEvent('turn stopped'))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0].id).toMatch(/^local-/)
	})

	it('the failed turn’s streamed text stands beside the error', () => {
		const live = createLiveTurns()
		live.applyEvent(chunk('msg-1', 'partial '))
		live.applyEvent(chunk('msg-1', 'answer'))
		live.applyEvent(
			errorEvent('model stream broke', undefined, { turnId: 'msg-1' })
		)

		// The terminal event replaces the streamed copy with the row the
		// backend committed, so the tail does not track a pending flag.
		expect(live.messages).toHaveLength(2)
		expect(live.messages[0]).toMatchObject({
			id: 'msg-1',
			content: 'partial answer'
		})
		expect(live.messages[1].role).toBe('error')
	})

	it('a tool-attributed error adds no bubble — the tool row shows it', () => {
		const live = createLiveTurns()
		live.applyEvent(errorEvent('no results found', 'duckduckgo_search'))

		expect(live.messages.some((m) => m.role === 'error')).toBe(false)
	})

	it('a tool start creates a pending invocation row the finish completes', () => {
		const live = createLiveTurns()
		live.beginTool({
			invocationId: 'inv-1',
			turnId: 'msg-1',
			name: 'web_search',
			args: '{"query":"otters"}'
		})

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'inv-1',
			turnId: 'msg-1',
			role: 'tool',
			name: 'web_search',
			args: '{"query":"otters"}',
			pending: true
		})

		live.finishTool('inv-1', '"2 hits"')

		expect(live.messages[0]).toMatchObject({
			pending: false,
			result: '"2 hits"',
			error: null
		})
	})

	it('a repeated tool start for the same invocation never duplicates', () => {
		const live = createLiveTurns()
		live.beginTool({
			invocationId: 'inv-1',
			turnId: 'msg-1',
			name: 'web_search',
			args: '{}'
		})
		live.beginTool({
			invocationId: 'inv-1',
			turnId: 'msg-1',
			name: 'web_search',
			args: '{}'
		})

		expect(live.messages).toHaveLength(1)
	})

	it('a repeated provider call id lands on its own invocation row', () => {
		// The server assigns one invocation id per executing call, so
		// the same provider call id cannot collide across turns.
		const live = createLiveTurns()
		live.beginTool({
			invocationId: 'inv-1',
			turnId: 'msg-1',
			name: 'web_search',
			args: '{}'
		})
		live.beginTool({
			invocationId: 'inv-2',
			turnId: 'msg-2',
			name: 'web_search',
			args: '{}'
		})

		expect(live.messages).toHaveLength(2)
		expect(live.messages[0].id).toBe('inv-1')
		expect(live.messages[1].id).toBe('inv-2')

		live.finishTool('inv-2', '"2 hits"')
		expect(live.messages[0].pending).toBe(true)
		expect(live.messages[1].pending).toBe(false)
	})

	it('a tool error rides the completed entry', () => {
		const live = createLiveTurns()
		live.beginTool({
			invocationId: 'inv-9',
			turnId: 'msg-1',
			name: 'web_search',
			args: '{}'
		})
		live.finishTool('inv-9', '', 'search backend down')

		expect(live.messages[0]).toMatchObject({
			pending: false,
			error: 'search backend down'
		})
	})

	it('a finish for an unknown invocation is ignored', () => {
		const live = createLiveTurns()
		live.finishTool('inv-404', '"late result"')

		expect(live.messages).toHaveLength(0)
	})

	it('ignores events that are not part of a turn', () => {
		const live = createLiveTurns()
		live.applyEvent(laneStatus(false))
		live.applyEvent({
			type: 'queue_state',
			data: { items: [], paused: false }
		})
		live.applyEvent({
			type: 'lifecycle',
			data: { cursor: 'v1:h:1', events: [] }
		})

		expect(live.messages).toHaveLength(0)
	})
})

describe('synchronization hand-off', () => {
	it('restores the user row and the streamed prefix', () => {
		const live = createLiveTurns()
		live.replaceWith({
			itemId: 'turn-1',
			userRow: { id: 'user-row-1', content: 'hello' },
			text: 'partial answer',
			invocations: []
		})

		expect(live.messages).toHaveLength(2)
		expect(live.messages[0]).toMatchObject({
			id: 'user-row-1',
			role: 'user',
			content: 'hello'
		})
		expect(live.messages[1]).toMatchObject({
			id: 'turn-1',
			role: 'assistant',
			content: 'partial answer'
		})
	})

	it('restores invocations with their pending state and outcomes', () => {
		const live = createLiveTurns()
		live.replaceWith({
			itemId: 'turn-1',
			text: '',
			invocations: [
				invocation('inv-1', { result: '"2 hits"', pending: false }),
				invocation('inv-2')
			]
		})

		expect(live.messages).toHaveLength(2)
		expect(live.messages[0]).toMatchObject({
			id: 'inv-1',
			role: 'tool',
			turnId: 'turn-1',
			pending: false,
			result: '"2 hits"'
		})
		expect(live.messages[1]).toMatchObject({
			id: 'inv-2',
			role: 'tool',
			pending: true
		})
	})

	it('adds no answer row while only tools have run', () => {
		const live = createLiveTurns()
		live.replaceWith({
			itemId: 'turn-1',
			text: '',
			invocations: [invocation('inv-1')]
		})

		expect(live.messages.filter((m) => m.role === 'assistant')).toHaveLength(0)
	})

	it('replaces stale live rows wholesale — not an append', () => {
		const live = createLiveTurns()
		live.applyEvent(chunk('turn-9', 'torn text from a dead process'))
		live.applyEvent(laneStatus(true, true))

		live.replaceWith({
			itemId: 'turn-1',
			userRow: { id: 'user-row-2', content: 'real prompt' },
			text: 'restored prefix',
			invocations: []
		})

		expect(live.messages).toHaveLength(2)
		expect(live.messages.map((m) => m.id)).toEqual(['user-row-2', 'turn-1'])
	})

	it('clears the tail when no turn runs', () => {
		const live = createLiveTurns()
		live.applyEvent(chunk('turn-9', 'leftover'))

		live.replaceWith(undefined)

		expect(live.messages).toHaveLength(0)
	})
})

describe('reasoning marker', () => {
	it('stands in for the answer row while the lane reasons — once', () => {
		const live = createLiveTurns()
		live.applyEvent(laneStatus(true, true))
		live.applyEvent(laneStatus(true, true))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			role: 'assistant',
			content: ''
		})
	})

	it('is removed when the lane stops reasoning', () => {
		const live = createLiveTurns()
		live.applyEvent(laneStatus(true, true))
		live.applyEvent(laneStatus(true, false))

		expect(live.messages).toHaveLength(0)
	})

	it('gives way to the streamed answer — the signal lowers first', () => {
		const live = createLiveTurns()
		live.applyEvent(laneStatus(true, true))
		live.applyEvent(laneStatus(true, false))
		live.applyEvent(chunk('msg-1', 'the answer'))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'msg-1',
			role: 'assistant',
			content: 'the answer'
		})
	})

	it('is gone by the error that ends an unanswered turn', () => {
		const live = createLiveTurns()
		live.applyEvent(laneStatus(true, true))
		live.applyEvent(laneStatus(false, false))
		live.applyEvent(errorEvent('turn stopped'))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0].role).toBe('error')
	})

	it('trails the user row of a restored prefix', () => {
		const live = createLiveTurns()
		// A (re)connect mid-reasoning delivers the lane's reasoning
		// signal with the restored prefix; the marker stands in below
		// the user row, never above it.
		live.replaceWith({
			itemId: 'turn-1',
			userRow: { id: 'user-row-1', content: 'prompt' },
			text: '',
			invocations: []
		})
		live.applyEvent(laneStatus(true, true))

		expect(live.messages.map((m) => m.role)).toEqual(['user', 'assistant'])
		expect(live.messages[1].content).toBe('')
	})
})

describe('live turn settlement', () => {
	it('settle drops the settled assistant and its user row', () => {
		const live = createLiveTurns()
		live.replaceWith({
			itemId: 'msg-1',
			userRow: { id: 'user-row-1', content: 'hi' },
			text: 'ans',
			invocations: []
		})
		live.settleTurn('msg-1', ['user-row-1'])

		expect(live.messages).toHaveLength(0)
	})

	it('settle drops its entries but keeps rows the thread does not carry yet', () => {
		const live = createLiveTurns()
		live.replaceWith({
			itemId: 'msg-1',
			userRow: { id: 'user-row-1', content: 'settled prompt' },
			text: 'ans',
			invocations: []
		})
		live.beginTool({
			invocationId: 'inv-2',
			turnId: 'msg-2',
			name: 's',
			args: '{}'
		})

		live.settleTurn('msg-1', ['user-row-1'])

		// The settled turn lives in the stored thread now; the later
		// turn's row stays live.
		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'inv-2',
			role: 'tool',
			pending: true
		})
	})

	it('settle drops only its own turn’s tool entries', () => {
		const live = createLiveTurns()

		live.beginTool({
			invocationId: 'inv-1',
			turnId: 'msg-1',
			name: 's',
			args: '{}'
		})
		live.beginTool({
			invocationId: 'inv-2',
			turnId: 'msg-2',
			name: 's',
			args: '{}'
		})

		live.settleTurn('msg-1', ['user-row-1'])

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'inv-2',
			role: 'tool',
			pending: true
		})
	})

	it('settle retires a durable error the page carries, keeps uncovered ones', () => {
		const live = createLiveTurns()
		live.applyEvent(errorEvent('no default model set'))
		live.applyEvent(errorEvent('turn stopped'))
		live.applyEvent(errorEvent('old failure'))

		// The settlement's rows cover the first error only: its live
		// copy retires. The transient stop and the error whose row the
		// event did not carry both stay.
		live.settleTurn('msg-1', [], [live.messages[0].id])

		expect(live.messages).toHaveLength(2)
		expect(live.messages[0]).toMatchObject({
			role: 'error',
			content: 'turn stopped'
		})
		expect(live.messages[1]).toMatchObject({
			role: 'error',
			content: 'old failure'
		})
	})

	it('settle keeps a running later turn while retiring a stored error', () => {
		const live = createLiveTurns()
		live.replaceWith({
			itemId: 'msg-2',
			userRow: { id: 'user-row-2', content: 'next prompt' },
			text: 'streaming',
			invocations: []
		})
		live.applyEvent(errorEvent('no default model set'))
		const errorId = live.messages[live.messages.length - 1].id

		live.settleTurn('msg-x', [], [errorId])

		expect(live.messages).toHaveLength(2)
		expect(live.messages[0]).toMatchObject({
			role: 'user',
			content: 'next prompt'
		})
		expect(live.messages[1]).toMatchObject({
			id: 'msg-2',
			role: 'assistant'
		})
	})
})

describe('taking rows at a boundary', () => {
	it('returns the rows and clears the tail', () => {
		const live = createLiveTurns()
		live.replaceWith({
			itemId: 'msg-1',
			userRow: { id: 'user-row-1', content: 'hi' },
			text: 'ans',
			invocations: []
		})

		const taken = live.takeRows()

		expect(taken.map((m) => m.id)).toEqual(['user-row-1', 'msg-1'])
		expect(live.messages).toHaveLength(0)
	})

	it('drops empty assistant markers — transient UI never freezes', () => {
		const live = createLiveTurns()
		live.replaceWith({
			itemId: 'msg-1',
			userRow: { id: 'user-row-1', content: 'hi' },
			text: '',
			invocations: []
		})
		live.applyEvent(laneStatus(true, true))
		live.applyEvent(chunk('msg-1', 'ans'))

		const taken = live.takeRows()

		expect(taken.map((m) => m.role)).toEqual(['user', 'assistant'])
		expect(live.messages).toHaveLength(0)
	})
})
