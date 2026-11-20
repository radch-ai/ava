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

/** Opens the latest socket so mutations can ride it. */
function openSocket() {
	lastSocket().readyState = FakeWebSocket.OPEN
	lastSocket().onopen?.()
}

function sentFrames(): unknown[] {
	return lastSocket().sent.map((frame) => JSON.parse(frame))
}

afterEach(() => {
	sockets.length = 0
})

describe('agent socket', () => {
	it('decodes connected frames into typed events', () => {
		const events: unknown[] = []
		createAgentSocket('_superagent', (event) =>
			events.push(event)
		).requestStatus()

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
				data: {
					active: true,
					working: true,
					reasoning: false,
					model: 'gpt-4o',
					queue_len: 2,
					conn_count: 1
				}
			})
		})

		expect(events).toEqual([
			{
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: true,
					reasoning: false,
					model: 'gpt-4o',
					queue_len: 2,
					conn_count: 1
				}
			}
		])
	})

	it('decodes queue_state frames as full-replacement snapshots', () => {
		const events: unknown[] = []
		createAgentSocket('_superagent', (event) =>
			events.push(event)
		).requestStatus()

		lastSocket().onmessage?.({
			data: JSON.stringify({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [{ id: 'q-1', content: 'waiting' }],
					paused: true
				}
			})
		})

		expect(events).toEqual([
			{
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [{ id: 'q-1', content: 'waiting' }],
					paused: true
				}
			}
		])
	})

	it('decodes command_completed frames as thread switches', () => {
		const events: unknown[] = []
		createAgentSocket('_superagent', (event) =>
			events.push(event)
		).requestStatus()

		lastSocket().onmessage?.({
			data: JSON.stringify({
				type: 'command_completed',
				agent_id: '_superagent',
				data: {
					item_id: 'cmd-1',
					thread_id: 'thread-2',
					previous_thread_id: 'thread-1',
					divider: {
						id: 'thread-2',
						content: 'New session',
						created_at: '2026-11-19T00:00:00.000Z'
					}
				}
			})
		})

		expect(events).toEqual([
			{
				type: 'command_completed',
				agent_id: '_superagent',
				data: {
					item_id: 'cmd-1',
					thread_id: 'thread-2',
					previous_thread_id: 'thread-1',
					divider: {
						id: 'thread-2',
						content: 'New session',
						created_at: '2026-11-19T00:00:00.000Z'
					}
				}
			}
		])
	})

	it('resolves a pending prompt through its queue_result', async () => {
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()

		const pending = socket.sendPrompt('hi', null)
		const frame = sentFrames()[0] as {
			data: { request_id: string }
		}
		expect(frame).toMatchObject({
			type: 'prompt',
			agent_id: '_superagent',
			data: { content: 'hi' }
		})

		lastSocket().onmessage?.({
			data: JSON.stringify({
				type: 'queue_result',
				agent_id: '_superagent',
				data: {
					request_id: frame.data.request_id,
					accepted: true,
					item_id: 'q-9'
				}
			})
		})

		await expect(pending).resolves.toEqual({
			accepted: true,
			itemId: 'q-9',
			error: undefined
		})
	})

	it('resolves a send_now through its queue_result and frames item id and selection', async () => {
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()

		const pending = socket.sendNow('q-1', {
			id: 'm-fix',
			providerId: 'prov'
		})
		expect(sentFrames()[0]).toMatchObject({
			type: 'send_now',
			agent_id: '_superagent',
			data: {
				item_id: 'q-1',
				model: { id: 'm-fix', provider_id: 'prov' }
			}
		})

		const frame = sentFrames()[0] as { data: { request_id: string } }
		lastSocket().onmessage?.({
			data: JSON.stringify({
				type: 'queue_result',
				agent_id: '_superagent',
				data: {
					request_id: frame.data.request_id,
					accepted: false,
					error: 'item is not pending in this queue'
				}
			})
		})

		await expect(pending).resolves.toEqual({
			accepted: false,
			itemId: undefined,
			error: 'item is not pending in this queue'
		})
	})

	it('ignores queue_results for other request ids', async () => {
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()

		const pending = socket.sendPrompt('hi', null)
		const frame = sentFrames()[0] as { data: { request_id: string } }
		lastSocket().onmessage?.({
			data: JSON.stringify({
				type: 'queue_result',
				data: { request_id: 'not-mine', accepted: true, item_id: 'q-x' }
			})
		})
		// A foreign result cannot settle it; only the matching id can.
		lastSocket().onmessage?.({
			data: JSON.stringify({
				type: 'queue_result',
				data: {
					request_id: frame.data.request_id,
					accepted: false,
					error: 'queue is full'
				}
			})
		})

		await expect(pending).resolves.toEqual({
			accepted: false,
			itemId: undefined,
			error: 'queue is full'
		})
	})

	it('settles outstanding operations as unconfirmed when the connection drops', async () => {
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()

		const pending = socket.sendPrompt('hi', null)
		lastSocket().close()

		await expect(pending).resolves.toMatchObject({
			accepted: false,
			error: expect.stringContaining('connection lost')
		})
	})

	it('rejects mutations while the socket is not live — nothing buffers', async () => {
		const socket = createAgentSocket('_superagent', () => {})

		await expect(socket.sendPrompt('queued prompt', null)).rejects.toThrow(
			'not connected'
		)
		socket.requestStatus()
		expect(lastSocket().sent).toEqual([])
	})

	it('settles an unanswered operation as unconfirmed after 15 seconds', async () => {
		vi.useFakeTimers()
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()

		const pending = socket.sendPrompt('no answer', null)
		const settled = expect(pending).resolves.toMatchObject({
			accepted: false,
			error: expect.stringContaining('unconfirmed')
		})

		vi.advanceTimersByTime(15_000)
		await settled

		vi.useRealTimers()
	})

	it('close is terminal: sends reject and no connection is resurrected', async () => {
		vi.useFakeTimers()
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()
		socket.close()
		const socketCount = sockets.length

		await expect(socket.sendPrompt('after close', null)).rejects.toThrow(
			'not connected'
		)
		socket.requestStatus()
		socket.sendInterrupt()

		vi.advanceTimersByTime(120_000)
		expect(sockets).toHaveLength(socketCount)

		vi.useRealTimers()
	})

	it('a concrete selection rides the frame as the model pair', () => {
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()

		socket.sendPrompt('hi', { id: 'openai/gpt-4o', providerId: 'p-1' })

		expect(sentFrames()[0]).toMatchObject({
			type: 'prompt',
			agent_id: '_superagent',
			data: {
				content: 'hi',
				model: { id: 'openai/gpt-4o', provider_id: 'p-1' }
			}
		})
	})

	it('sendInterrupt rides the wire as an interrupt envelope when live', () => {
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()

		socket.sendInterrupt()

		expect(sentFrames()[0]).toEqual({
			type: 'interrupt',
			agent_id: '_superagent',
			data: undefined
		})
	})

	it('close settles pending operations and closes the socket', async () => {
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()
		const pending = socket.sendPrompt('never answered', null)

		socket.close()
		expect(lastSocket().readyState).toBe(3)
		await expect(pending).resolves.toMatchObject({ accepted: false })
	})

	it('reports connection state: connecting, then live on the greeting', () => {
		const states: string[] = []
		createAgentSocket(
			'_superagent',
			() => {},
			(state) => states.push(state)
		)

		lastSocket().onmessage?.({
			data: JSON.stringify({ type: 'connected', data: { conn_id: 'c1' } })
		})

		expect(states).toEqual(['connecting', 'live'])
	})

	it('reconnects after a drop with growing backoff', () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		const states: string[] = []
		createAgentSocket(
			'_superagent',
			() => {},
			(state) => states.push(state)
		)
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
		createAgentSocket('_superagent', () => {})

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
		const before = sockets.length

		lastSocket().close()
		socket.close()
		vi.advanceTimersByTime(60_000)
		expect(sockets).toHaveLength(before)

		vi.useRealTimers()
	})

	it('a mutation during a drop never replays after reconnect', async () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()

		lastSocket().close()
		const rejected = socket.sendPrompt('typed while down', null)
		await expect(rejected).rejects.toThrow('not connected')

		vi.advanceTimersByTime(1000)
		openSocket()

		expect(lastSocket().sent).toEqual([])

		vi.useRealTimers()
		vi.restoreAllMocks()
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
})
