import { describe, expect, it } from 'vitest'

import { createLiveTurns } from '#lib/services/agent/live-turn.svelte'
import type { AgentSocketEvent } from '#lib/services/agent/ws'

function chunk(id: string, delta: string): AgentSocketEvent {
	return { type: 'message_chunk', agent_id: '_superagent', data: { id, delta } }
}

function reasoningChunk(id: string, reasoning: string): AgentSocketEvent {
	return {
		type: 'message_chunk',
		agent_id: '_superagent',
		data: { id, reasoning }
	}
}

function message(id: string, content: string): AgentSocketEvent {
	return { type: 'message', agent_id: '_superagent', data: { id, content } }
}

function errorEvent(message: string): AgentSocketEvent {
	return { type: 'error', agent_id: '_superagent', data: { message } }
}

describe('live turns', () => {
	it('echoes the submitted prompt as a live user message', () => {
		const live = createLiveTurns()
		live.echoUser('hello')

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0].role).toBe('user')
		expect(live.messages[0].content).toBe('hello')
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

	it('reasoning chunks accumulate into a thinking message the answer follows', () => {
		const live = createLiveTurns()
		live.applyEvent(reasoningChunk('msg-1', 'thinking '))
		live.applyEvent(reasoningChunk('msg-1', 'hard'))
		live.applyEvent(chunk('msg-1', 'the answer'))

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0]).toMatchObject({
			id: 'msg-1',
			role: 'assistant',
			reasoning: 'thinking hard',
			content: 'the answer',
			pending: true
		})
	})

	it('the settled message retires the reasoning — it is live-only', () => {
		const live = createLiveTurns()
		live.applyEvent(reasoningChunk('msg-1', 'thinking'))
		live.applyEvent(chunk('msg-1', 'the answer'))
		live.applyEvent(message('msg-1', 'the answer'))

		expect(live.messages[0].reasoning).toBeUndefined()
		expect(live.messages[0].content).toBe('the answer')
		expect(live.messages[0].pending).toBe(false)
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
		live.applyEvent({
			type: 'connected',
			data: { conn_id: 'c1' }
		})
		live.applyEvent({
			type: 'lane_status',
			agent_id: '_superagent',
			data: { active: true, model: '', queue_len: 0, conn_count: 1 }
		})

		expect(live.messages).toHaveLength(0)
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

	it('settle keeps error bubbles — they are live-only', () => {
		const live = createLiveTurns()
		live.echoUser('hi')
		live.applyEvent(errorEvent('read default model: no rows'))
		live.applyEvent(message('msg-1', 'answer'))

		live.settleTurn('msg-1', ['hi'])

		expect(live.messages).toHaveLength(1)
		expect(live.messages[0].role).toBe('error')
	})
})
