import { afterEach, describe, expect, it, vi } from 'vitest'

import { createAgentSocket } from '#lib/services/agent/ws'

class FakeWebSocket {
	static OPEN = 1
	readyState = 0
	sent: string[] = []
	onopen: (() => void) | null = null
	onmessage: ((message: { data: string }) => void) | null = null
	onclose: (() => void) | null = null

	constructor() {
		sockets.push(this)
	}

	close() {
		this.readyState = 3
		this.onclose?.()
	}

	send(data: string) {
		this.sent.push(data)
	}
}

const sockets: FakeWebSocket[] = []

vi.stubGlobal('WebSocket', FakeWebSocket)

function lastSocket(): FakeWebSocket {
	return sockets[sockets.length - 1]
}

afterEach(() => {
	sockets.length = 0
})

describe('agent socket', () => {
	it('decodes connected frames into typed events', () => {
		const events: unknown[] = []
		createAgentSocket('_superagent', (event) => events.push(event)).sendPrompt(
			'hi',
			'auto'
		)

		lastSocket().onmessage?.({
			data: JSON.stringify({ type: 'connected', data: { conn_id: 'c1' } })
		})

		expect(events).toEqual([{ type: 'connected', data: { conn_id: 'c1' } }])
	})

	it('decodes lane_status frames with their payload as data', () => {
		const events: unknown[] = []
		createAgentSocket('_superagent', (event) =>
			events.push(event)
		).requestStatus()

		lastSocket().onmessage?.({
			data: JSON.stringify({
				type: 'lane_status',
				agent_id: '_superagent',
				data: { active: true, model: 'auto', queue_len: 2, conn_count: 1 }
			})
		})

		expect(events).toEqual([
			{
				type: 'lane_status',
				agent_id: '_superagent',
				data: { active: true, model: 'auto', queue_len: 2, conn_count: 1 }
			}
		])
	})

	it('drops frames whose type has no event arm', () => {
		const events: unknown[] = []
		createAgentSocket('_superagent', (event) =>
			events.push(event)
		).requestStatus()

		lastSocket().onmessage?.({
			data: JSON.stringify({ type: 'message_delta', data: { text: 'x' } })
		})
		lastSocket().onmessage?.({ data: 'not json' })

		expect(events).toEqual([])
	})

	it('queues client messages until the socket is open, then flushes', () => {
		const socket = createAgentSocket('_superagent', () => {})

		socket.sendPrompt('queued prompt', 'auto')
		socket.requestStatus()
		expect(lastSocket().sent).toEqual([])

		lastSocket().readyState = FakeWebSocket.OPEN
		lastSocket().onopen?.()

		expect(lastSocket().sent.map((frame) => JSON.parse(frame))).toEqual([
			{
				type: 'prompt',
				agent_id: '_superagent',
				data: { content: 'queued prompt', model: 'auto' }
			},
			{ type: 'status', agent_id: '_superagent' }
		])
	})
	it('close drops queued messages and closes the socket', () => {
		const socket = createAgentSocket('_superagent', () => {})
		socket.sendPrompt('never sent', 'auto')

		socket.close()
		expect(lastSocket().readyState).toBe(3)

		lastSocket().onopen?.()
		expect(lastSocket().sent).toEqual([])
	})
})
