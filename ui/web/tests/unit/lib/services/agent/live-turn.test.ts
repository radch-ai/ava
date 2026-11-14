import { describe, expect, it } from 'vitest'

import { createLiveTurns } from '#lib/services/agent/live-turn.svelte'
import type { AgentSocketEvent } from '#lib/services/agent/ws'

function chunk(id: string, delta: string): AgentSocketEvent {
	return { type: 'message_chunk', agent_id: '_superagent', data: { id, delta } }
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
