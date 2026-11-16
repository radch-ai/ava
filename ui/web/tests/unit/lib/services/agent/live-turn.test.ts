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

describe('live turns', () => {
	it('echoes the prompt optimistically', () => {
		const live = createLiveTurns()
		live.echoUser('hello')

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({ role: 'user', content: 'hello' })
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
		live.echoUser('hi')
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
		live.echoUser('search for otters')
		live.applyEvent(errorEvent('no results found', 'duckduckgo_search'))

		expect(live.messages.some((m) => m.role === 'error')).toBe(false)
	})

	it('a tool-attributed error still drops the thinking placeholder', () => {
		const live = createLiveTurns()
		live.echoUser('search for otters')
		live.applyEvent(errorEvent('no results found', 'duckduckgo_search'))

		expect(
			live.messages.filter((m) => m.role === 'assistant' && !m.content)
		).toHaveLength(0)
	})

	it('a tool call creates a pending tool entry the result completes', () => {
		const live = createLiveTurns()
		live.applyEvent({
			type: 'tool_call',
			agent_id: '_superagent',
			data: { id: 'call-1', name: 'web_search', args: '{"query":"otters"}' }
		})

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'call-1',
			role: 'tool',
			name: 'web_search',
			args: '{"query":"otters"}',
			pending: true
		})

		live.applyEvent({
			type: 'tool_result',
			agent_id: '_superagent',
			data: { id: 'call-1', result: '"2 hits"' }
		})

		expect(live.messages[0]).toMatchObject({
			pending: false,
			result: '"2 hits"',
			error: null
		})
	})

	it('a tool error rides the completed entry', () => {
		const live = createLiveTurns()
		live.applyEvent({
			type: 'tool_call',
			agent_id: '_superagent',
			data: { id: 'call-9', name: 'web_search', args: '{}' }
		})
		live.applyEvent({
			type: 'tool_result',
			agent_id: '_superagent',
			data: { id: 'call-9', result: '', error: 'search backend down' }
		})

		expect(live.messages[0]).toMatchObject({
			pending: false,
			error: 'search backend down'
		})
	})

	it('ignores events that are not part of a turn', () => {
		const live = createLiveTurns()
		live.applyEvent({ type: 'connected', data: { conn_id: 'c1' } })
		live.applyEvent(laneStatus(false))

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
})

describe('live turn settlement', () => {
	it('settle drops the settled assistant and its echoed prompt', () => {
		const live = createLiveTurns()
		live.echoUser('hi')
		live.applyEvent(chunk('msg-1', 'ans'))
		live.settleTurn('msg-1', ['hi'])

		expect(live.messages).toHaveLength(0)
	})

	it('settle drops its entries but keeps echoes the thread does not carry yet', () => {
		const live = createLiveTurns()
		live.echoUser('settled prompt')
		live.applyEvent(chunk('msg-1', 'ans'))
		live.applyEvent(message('msg-1', 'ans settled'))
		live.echoUser('still running')
		live.applyEvent(chunk('msg-2', 'partial'))

		live.settleTurn('msg-1', ['settled prompt'])

		// The settled turn lives in the stored thread now; the running
		// turn — echo and partial answer — stays live.
		expect(live.messages).toHaveLength(2)
		expect(live.messages[0]).toMatchObject({
			role: 'user',
			content: 'still running'
		})
		expect(live.messages[1]).toMatchObject({
			role: 'assistant',
			id: 'msg-2',
			content: 'partial',
			pending: true
		})
	})

	it('settle drops the turn’s tool entries — their ids never match the thread rows', () => {
		const live = createLiveTurns()
		live.echoUser('search')
		live.applyEvent({
			type: 'tool_call',
			agent_id: '_superagent',
			data: { id: 'call-1', name: 'web_search', args: '{}' }
		})
		live.applyEvent(message('msg-1', 'found it'))

		live.settleTurn('msg-1', ['search'])

		expect(live.messages).toHaveLength(0)
	})

	it('settle retires a durable error the page carries, keeps uncovered ones', () => {
		const live = createLiveTurns()
		live.echoUser('hi')
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
		live.settleTurn('msg-1', ['hi'], ['err-stored'])

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
		live.echoUser('next prompt')
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
