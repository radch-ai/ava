import { describe, expect, it } from 'vitest'

import { createLiveTurns } from '#lib/services/agent/live-turn.svelte'
import type { AgentSocketEvent } from '#lib/services/agent/ws'

function chunk(id: string, delta: string): AgentSocketEvent {
	return { type: 'message_chunk', agent_id: '_superagent', data: { id, delta } }
}

function laneStatus(working: boolean, reasoning = false): AgentSocketEvent {
	return {
		type: 'lane_status',
		agent_id: '_superagent',
		data: {
			active: true,
			working,
			reasoning,
			model: '',
			queue_len: 0,
			conn_count: 1
		}
	}
}

function message(id: string, content: string): AgentSocketEvent {
	return { type: 'message', agent_id: '_superagent', data: { id, content } }
}

function errorEvent(
	message: string,
	tool?: string,
	ids?: { id?: string; turnId?: string }
): AgentSocketEvent {
	return {
		type: 'error',
		agent_id: '_superagent',
		data: { message, tool, id: ids?.id, turn_id: ids?.turnId }
	}
}

function toolCall(
	callId: string,
	turnId: string,
	name = 'web_search'
): AgentSocketEvent {
	return {
		type: 'tool_call',
		agent_id: '_superagent',
		data: { id: callId, turn_id: turnId, name, args: '{"query":"otters"}' }
	}
}

function toolResult(
	callId: string,
	turnId: string,
	data: { result?: string; error?: string } = {}
): AgentSocketEvent {
	return {
		type: 'tool_result',
		agent_id: '_superagent',
		data: {
			id: callId,
			turn_id: turnId,
			result: data.result ?? '"2 hits"',
			error: data.error
		}
	}
}

describe('live turns', () => {
	it('adds the running item’s user row under its server id', () => {
		const live = createLiveTurns()
		live.beginUser('user-row-1', 'hello')

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'user-row-1',
			role: 'user',
			content: 'hello'
		})
	})

	it('never duplicates a user row on repeated snapshots', () => {
		const live = createLiveTurns()
		live.beginUser('user-row-1', 'hello')
		live.beginUser('user-row-1', 'hello')

		expect(live.messages).toHaveLength(1)
	})

	it('keeps both user rows when two prompts read alike', () => {
		const live = createLiveTurns()
		live.beginUser('user-row-1', 'same words')
		live.beginUser('user-row-2', 'same words')

		expect(live.messages.filter((m) => m.role === 'user')).toHaveLength(2)
	})

	it('accumulates chunk deltas into one pending assistant message', () => {
		const live = createLiveTurns()
		live.applyEvent(chunk('msg-1', 'hel'))
		live.applyEvent(chunk('msg-1', 'lo'))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'msg-1',
			role: 'assistant',
			content: 'hello',
			pending: true
		})
	})

	it('the final message event settles the assistant message', () => {
		const live = createLiveTurns()
		live.applyEvent(chunk('msg-1', 'hel'))
		live.applyEvent(message('msg-1', 'hello streamed world'))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'msg-1',
			role: 'assistant',
			content: 'hello streamed world',
			pending: false
		})
	})

	it('a message event without prior chunks creates the message settled', () => {
		const live = createLiveTurns()
		live.applyEvent(message('msg-1', 'plain answer'))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0].content).toBe('plain answer')
		expect(live.messages[0].pending).toBe(false)
	})

	it('an error event appends an error message with the reason', () => {
		const live = createLiveTurns()
		live.beginUser('user-row-1', 'hi')
		live.applyEvent(errorEvent('read default model: no rows'))

		expect(live.messages).toHaveLength(2)
		expect(live.messages[1].role).toBe('error')
		expect(live.messages[1].content).toBe('read default model: no rows')
	})

	it('a stored failure keeps the wire row id; a transient one stays local', () => {
		const stored = createLiveTurns()
		stored.applyEvent(
			errorEvent('no default model set', undefined, {
				id: 'err-1',
				turnId: 'msg-1'
			})
		)
		expect(stored.messages[0].id).toBe('err-1')

		const transient = createLiveTurns()
		transient.applyEvent(errorEvent('turn stopped'))
		expect(transient.messages[0].id).toMatch(/^local-/)
	})

	it('the failed turn’s streamed prefix stops pending', () => {
		const live = createLiveTurns()
		live.applyEvent(chunk('msg-1', 'partial '))
		live.applyEvent(chunk('msg-1', 'answer'))
		live.applyEvent(
			errorEvent('model stream broke', undefined, { turnId: 'msg-1' })
		)

		expect(live.messages).toHaveLength(2)
		expect(live.messages[0]).toMatchObject({
			id: 'msg-1',
			pending: false,
			content: 'partial answer'
		})
		expect(live.messages[1].role).toBe('error')
	})

	it('a tool-attributed error adds no bubble — the tool row shows it', () => {
		const live = createLiveTurns()
		live.beginUser('user-row-1', 'search for otters')
		live.applyEvent(errorEvent('no results found', 'duckduckgo_search'))

		expect(live.messages.some((m) => m.role === 'error')).toBe(false)
	})

	it('a tool call creates a pending tool entry the result completes', () => {
		const live = createLiveTurns()
		live.applyEvent(toolCall('call-1', 'msg-1'))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'msg-1:call-1',
			turnId: 'msg-1',
			role: 'tool',
			name: 'web_search',
			args: '{"query":"otters"}',
			pending: true
		})

		live.applyEvent(toolResult('call-1', 'msg-1'))

		expect(live.messages[0]).toMatchObject({
			pending: false,
			result: '"2 hits"',
			error: null
		})
	})

	it('a reused tool-call id lands on its own turn’s row', () => {
		const live = createLiveTurns()
		live.applyEvent(toolCall('call-1', 'msg-1'))
		live.applyEvent(toolCall('call-1', 'msg-2'))

		expect(live.messages).toHaveLength(2)
		expect(live.messages[0].id).toBe('msg-1:call-1')
		expect(live.messages[1].id).toBe('msg-2:call-1')

		live.applyEvent(toolResult('call-1', 'msg-2'))
		expect(live.messages[0].pending).toBe(true)
		expect(live.messages[1].pending).toBe(false)
	})

	it('a tool error rides the completed entry', () => {
		const live = createLiveTurns()
		live.applyEvent(toolCall('call-9', 'msg-1'))
		live.applyEvent(
			toolResult('call-9', 'msg-1', {
				result: '',
				error: 'search backend down'
			})
		)

		expect(live.messages[0]).toMatchObject({
			pending: false,
			error: 'search backend down'
		})
	})

	it('ignores events that are not part of a turn', () => {
		const live = createLiveTurns()
		live.applyEvent({ type: 'connected', data: { conn_id: 'c1' } })
		live.applyEvent(laneStatus(false))
		live.applyEvent({
			type: 'queue_state',
			agent_id: '_superagent',
			data: { items: [], paused: false }
		})

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
			content: 'the answer',
			pending: true
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

	it('trails the user row even when the join order inverts them', () => {
		const live = createLiveTurns()
		// A (re)connect mid-reasoning delivers the lane status before
		// the queue snapshot that mints the user row — the marker must
		// still render below it, never above.
		live.applyEvent(laneStatus(true, true))
		live.beginUser('user-row-1', 'prompt')

		expect(live.messages.map((m) => m.role)).toEqual(['user', 'assistant'])
		expect(live.messages[1].content).toBe('')
	})
})

describe('live turn settlement', () => {
	it('settle drops the settled assistant and its user row', () => {
		const live = createLiveTurns()
		live.beginUser('user-row-1', 'hi')
		live.applyEvent(chunk('msg-1', 'ans'))
		live.settleTurn('msg-1', ['user-row-1'])

		expect(live.messages).toHaveLength(0)
	})

	it('settle drops its entries but keeps rows the thread does not carry yet', () => {
		const live = createLiveTurns()
		live.beginUser('user-row-1', 'settled prompt')
		live.applyEvent(chunk('msg-1', 'ans'))
		live.applyEvent(message('msg-1', 'ans settled'))
		live.beginUser('user-row-2', 'still running')
		live.applyEvent(chunk('msg-2', 'partial'))

		live.settleTurn('msg-1', ['user-row-1'])

		// The settled turn lives in the stored thread now; the running
		// turn — user row and partial answer — stays live.
		expect(live.messages).toHaveLength(2)
		expect(live.messages[0]).toMatchObject({
			role: 'user',
			content: 'still running'
		})
		expect(live.messages[1]).toMatchObject({
			id: 'msg-2',
			role: 'assistant',
			content: 'partial',
			pending: true
		})
	})

	it('settle drops only its own turn’s tool entries', () => {
		const live = createLiveTurns()
		live.beginUser('user-row-1', 'search')
		live.applyEvent(toolCall('call-1', 'msg-1'))
		live.applyEvent(message('msg-1', 'found it'))
		live.applyEvent(toolCall('call-2', 'msg-2'))

		live.settleTurn('msg-1', ['user-row-1'])

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'msg-2:call-2',
			role: 'tool',
			pending: true
		})
	})

	it('settle retires a durable error the page carries, keeps uncovered ones', () => {
		const live = createLiveTurns()
		live.beginUser('user-row-1', 'hi')
		live.applyEvent(
			errorEvent('no default model set', undefined, {
				id: 'err-stored',
				turnId: 'msg-1'
			})
		)
		live.applyEvent(errorEvent('turn stopped'))
		live.applyEvent(
			errorEvent('old failure', undefined, { id: 'err-not-loaded' })
		)

		// The refetched page carries err-stored only: its live copy
		// retires. The transient stop and the error whose row the page
		// did not carry (older than its window) both stay.
		live.settleTurn('msg-1', ['user-row-1'], ['err-stored'])

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
		live.applyEvent(
			errorEvent('no default model set', undefined, { id: 'err-1' })
		)
		live.beginUser('user-row-2', 'next prompt')
		live.applyEvent(chunk('msg-2', 'streaming'))

		live.settleTurn('msg-x', [], ['err-1'])

		expect(live.messages).toHaveLength(2)
		expect(live.messages[0]).toMatchObject({
			role: 'user',
			content: 'next prompt'
		})
		expect(live.messages[1]).toMatchObject({
			id: 'msg-2',
			pending: true
		})
	})
})
