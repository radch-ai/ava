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
			null
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
				data: { active: true, model: 'gpt-4o', queue_len: 2, conn_count: 1 }
			})
		})

		expect(events).toEqual([
			{
				type: 'lane_status',
				agent_id: '_superagent',
				data: { active: true, model: 'gpt-4o', queue_len: 2, conn_count: 1 }
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

		socket.sendPrompt('queued prompt', null)
		socket.requestStatus()
		expect(lastSocket().sent).toEqual([])

		lastSocket().readyState = FakeWebSocket.OPEN
		lastSocket().onopen?.()

		expect(lastSocket().sent.map((frame) => JSON.parse(frame))).toEqual([
			{
				type: 'prompt',
				agent_id: '_superagent',
				data: { content: 'queued prompt' }
			},
			{ type: 'status', agent_id: '_superagent' }
		])
	})
	it('a concrete selection rides the frame as the model pair', () => {
		const socket = createAgentSocket('_superagent', () => {})

		socket.sendPrompt('hi', { id: 'openai/gpt-4o', providerId: 'p-1' })
		lastSocket().readyState = FakeWebSocket.OPEN
		lastSocket().onopen?.()

		expect(lastSocket().sent.map((frame) => JSON.parse(frame))).toEqual([
			{
				type: 'prompt',
				agent_id: '_superagent',
				data: {
					content: 'hi',
					model: { id: 'openai/gpt-4o', provider_id: 'p-1' }
				}
			}
		])
	})
	it('close drops queued messages and closes the socket', () => {
		const socket = createAgentSocket('_superagent', () => {})
		socket.sendPrompt('never sent', null)

		socket.close()
		expect(lastSocket().readyState).toBe(3)

		lastSocket().onopen?.()
		expect(lastSocket().sent).toEqual([])
	})

	it('reports connection state: connecting, then live on the greeting', () => {
		const states: string[] = []
		createAgentSocket(
			'_superagent',
			() => {},
			(state) => states.push(state)
		).sendPrompt('hi', null)

		lastSocket().onmessage?.({
			data: JSON.stringify({ type: 'connected', data: { conn_id: 'c1' } })
		})

		expect(states).toEqual(['connecting', 'live'])
	})

	it('reconnects after a drop with growing backoff', () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		const states: string[] = []
		const socket = createAgentSocket(
			'_superagent',
			() => {},
			(state) => states.push(state)
		)
		socket.sendPrompt('hi', null)
		lastSocket().onmessage?.({
			data: JSON.stringify({ type: 'connected', data: { conn_id: 'c1' } })
		})
		const before = sockets.length

		lastSocket().close()
		expect(states).toEqual(['connecting', 'live', 'reconnecting'])
		expect(sockets).toHaveLength(before)

		// First retry waits the full base delay (jitter pinned to 1.0).
		vi.advanceTimersByTime(999)
		expect(sockets).toHaveLength(before)
		vi.advanceTimersByTime(1)
		expect(sockets).toHaveLength(before + 1)
		// A retry connects as reconnecting, not connecting.
		expect(states.at(-1)).toBe('reconnecting')

		vi.useRealTimers()
		vi.restoreAllMocks()
	})

	it('caps the backoff at 15 seconds', () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		const socket = createAgentSocket('_superagent', () => {})
		socket.sendPrompt('hi', null)

		// Six drops without a successful greeting in between.
		for (let drop = 0; drop < 6; drop++) {
			lastSocket().close()
			// Delays grow 1s, 2s, 4s, 8s, then cap at 15s.
			const wait = Math.min(1000 * 2 ** drop, 15000)
			vi.advanceTimersByTime(wait - 1)
			expect(sockets).toHaveLength(drop + 1)
			vi.advanceTimersByTime(1)
			expect(sockets).toHaveLength(drop + 2)
		}

		vi.useRealTimers()
		vi.restoreAllMocks()
	})

	it('close stops the reconnect loop', () => {
		vi.useFakeTimers()
		const socket = createAgentSocket('_superagent', () => {})
		socket.sendPrompt('hi', null)
		const before = sockets.length

		lastSocket().close()
		socket.close()
		vi.advanceTimersByTime(60_000)
		expect(sockets).toHaveLength(before)

		vi.useRealTimers()
	})

	it('messages sent during a drop flush after reconnect', () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		const socket = createAgentSocket('_superagent', () => {})
		socket.sendPrompt('first', null)
		lastSocket().readyState = FakeWebSocket.OPEN
		lastSocket().onopen?.()
		expect(lastSocket().sent).toHaveLength(1)

		lastSocket().close()
		socket.sendPrompt('queued while down', null)

		vi.advanceTimersByTime(1000)
		lastSocket().readyState = FakeWebSocket.OPEN
		lastSocket().onopen?.()

		expect(lastSocket().sent.map((frame) => JSON.parse(frame))).toEqual([
			{
				type: 'prompt',
				agent_id: '_superagent',
				data: { content: 'queued while down' }
			}
		])

		vi.useRealTimers()
		vi.restoreAllMocks()
	})
})
