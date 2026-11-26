import { afterEach, describe, expect, it, vi } from 'vitest'

import {
	type AgentSocketEvent,
	createAgentSocket
} from '#lib/services/agent/ws'

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

/** One client frame the fake serialized back; the shapes are the
 * test's own, so the decode boundary is the only cast. */
interface SentFrame {
	type: string
	agent_id?: string
	data?: { request_id?: string; [key: string]: unknown }
}

function sentFrames(): SentFrame[] {
	return lastSocket().sent.map((frame) => JSON.parse(frame) as SentFrame)
}

function openSocket() {
	lastSocket().readyState = FakeWebSocket.OPEN
	lastSocket().onopen?.()
}

/** Delivers one server frame on the current connection. */
function receive(frame: unknown) {
	lastSocket().onmessage?.({ data: JSON.stringify(frame) })
}

function connected() {
	receive({ type: 'connected', data: { conn_id: 'c1' } })
}

const idleLane = {
	active: false,
	working: false,
	compacting: false,
	reasoning: false,
	model: ''
}

function syncReady(
	cursor = 'v1:h1:9',
	prefix?: { item_id: string; text: string; last_seq: number }
) {
	receive({
		type: 'sync_ready',
		data: {
			cursor,
			queue: { items: [], paused: false },
			lane: idleLane,
			prefix
		}
	})
}

/** Drives one full handshake on a fresh connection: greeting, then the
 * ready hand-off. After it the socket is live and mutations ride it. */
function goLive(cursor?: string) {
	openSocket()
	connected()
	syncReady(cursor)
}

function chunk(id: string, delta: string, seq: number) {
	receive({ type: 'message_chunk', data: { id, delta, seq } })
}

afterEach(() => {
	sockets.length = 0
})

describe('agent socket', () => {
	it('decodes lane_status frames with their payload as data', () => {
		const events: AgentSocketEvent[] = []
		createAgentSocket('_superagent', (event) => events.push(event))

		receive({
			type: 'lane_status',
			data: {
				active: true,
				working: true,
				compacting: false,
				reasoning: false,
				model: 'gpt-4o'
			}
		})

		expect(events).toEqual([
			{
				type: 'lane_status',
				data: {
					active: true,
					working: true,
					compacting: false,
					reasoning: false,
					model: 'gpt-4o'
				}
			}
		])
	})

	it('decodes queue_state frames as full-replacement snapshots', () => {
		const events: AgentSocketEvent[] = []
		createAgentSocket('_superagent', (event) => events.push(event))

		receive({
			type: 'queue_state',
			data: {
				items: [{ id: 'q-1', content: 'waiting' }],
				paused: true
			}
		})

		expect(events).toEqual([
			{
				type: 'queue_state',
				data: {
					items: [{ id: 'q-1', content: 'waiting' }],
					paused: true
				}
			}
		])
	})

	it('decodes lifecycle frames and forwards their events raw', () => {
		const events: AgentSocketEvent[] = []
		createAgentSocket('_superagent', (event) => events.push(event))

		receive({
			type: 'lifecycle',
			data: {
				cursor: 'v1:h1:12',
				events: [
					{ pos: 12, batchEnd: true, kind: 'item_started', itemId: 'q-1' }
				]
			}
		})

		expect(events).toEqual([
			{
				type: 'lifecycle',
				data: {
					cursor: 'v1:h1:12',
					events: [
						{ pos: 12, batchEnd: true, kind: 'item_started', itemId: 'q-1' }
					]
				}
			}
		])
	})

	it('sends a sync frame with no cursor on the greeting and enters syncing', () => {
		const states: string[] = []
		createAgentSocket(
			'_superagent',
			() => {},
			(state) => states.push(state)
		)

		openSocket()
		connected()

		expect(states).toEqual(['connecting', 'syncing'])
		expect(sentFrames()).toEqual([
			{ type: 'sync', agent_id: '_superagent', data: {} }
		])
	})

	it('advances the cursor through the replay and goes live on sync_ready', () => {
		const states: string[] = []
		const events: AgentSocketEvent[] = []
		createAgentSocket(
			'_superagent',
			(event) => events.push(event),
			(state) => states.push(state)
		)
		openSocket()
		connected()

		receive({ type: 'sync_snapshot', data: { cursor: 'v1:h1:5', page: {} } })
		expect(sentFrames().at(-1)).toEqual({
			type: 'sync',
			agent_id: '_superagent',
			data: { cursor: 'v1:h1:5' }
		})

		receive({
			type: 'sync_batch',
			data: { cursor: 'v1:h1:9', has_more: true, events: [] }
		})
		expect(sentFrames().at(-1)).toEqual({
			type: 'sync',
			agent_id: '_superagent',
			data: { cursor: 'v1:h1:9' }
		})

		syncReady('v1:h1:9')
		expect(states).toEqual(['connecting', 'syncing', 'live'])
		expect(events.map((event) => event.type)).toEqual([
			'sync_snapshot',
			'sync_batch',
			'sync_ready'
		])
	})

	it('a reconnect resumes from the stored cursor', () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		createAgentSocket('_superagent', () => {})
		goLive('v1:h1:9')

		lastSocket().close()
		vi.advanceTimersByTime(1000)
		expect(sockets).toHaveLength(2)

		openSocket()
		connected()
		expect(sentFrames()).toEqual([
			{ type: 'sync', agent_id: '_superagent', data: { cursor: 'v1:h1:9' } }
		])

		vi.useRealTimers()
		vi.restoreAllMocks()
	})

	it('a lifecycle frame advances the stored cursor', () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		createAgentSocket('_superagent', () => {})
		goLive('v1:h1:9')

		receive({
			type: 'lifecycle',
			data: { cursor: 'v1:h1:12', events: [] }
		})

		lastSocket().close()
		vi.advanceTimersByTime(1000)
		openSocket()
		connected()
		expect(sentFrames()).toEqual([
			{ type: 'sync', agent_id: '_superagent', data: { cursor: 'v1:h1:12' } }
		])

		vi.useRealTimers()
		vi.restoreAllMocks()
	})

	it('resolves a pending prompt through its queue_result', async () => {
		const socket = createAgentSocket('_superagent', () => {})
		goLive()

		const pending = socket.sendPrompt('hi', null)
		const frame = sentFrames()[1]
		expect(frame).toMatchObject({
			type: 'prompt',
			agent_id: '_superagent',
			data: { content: 'hi' }
		})

		receive({
			type: 'queue_result',
			data: {
				request_id: frame.data?.request_id,
				accepted: true,
				item_id: 'q-9'
			}
		})

		await expect(pending).resolves.toEqual({
			accepted: true,
			itemId: 'q-9',
			error: undefined
		})
	})

	it('resolves a send_now through its queue_result and frames item id and selection', async () => {
		const socket = createAgentSocket('_superagent', () => {})
		goLive()

		const pending = socket.sendNow('q-1', {
			id: 'm-fix',
			providerId: 'prov'
		})
		const frame = sentFrames()[1]
		expect(frame).toMatchObject({
			type: 'send_now',
			agent_id: '_superagent',
			data: {
				item_id: 'q-1',
				model: { id: 'm-fix', provider_id: 'prov' }
			}
		})

		receive({
			type: 'queue_result',
			data: {
				request_id: frame.data?.request_id,
				accepted: false,
				error: 'item is not pending in this queue'
			}
		})

		await expect(pending).resolves.toEqual({
			accepted: false,
			itemId: undefined,
			error: 'item is not pending in this queue'
		})
	})

	it('ignores queue_results for other request ids', async () => {
		const socket = createAgentSocket('_superagent', () => {})
		goLive()

		const pending = socket.sendPrompt('hi', null)
		const frame = sentFrames()[1]
		receive({
			type: 'queue_result',
			data: { request_id: 'not-mine', accepted: true, item_id: 'q-x' }
		})
		// A foreign result cannot settle it; only the matching id can.
		receive({
			type: 'queue_result',
			data: {
				request_id: frame.data?.request_id,
				accepted: false,
				error: 'queue is full'
			}
		})

		await expect(pending).resolves.toEqual({
			accepted: false,
			itemId: undefined,
			error: 'queue is full'
		})
	})

	it('settles outstanding operations as unconfirmed when the connection drops', async () => {
		const socket = createAgentSocket('_superagent', () => {})
		goLive()

		const pending = socket.sendPrompt('hi', null)
		lastSocket().close()

		await expect(pending).resolves.toMatchObject({
			accepted: false,
			error: expect.stringContaining('connection lost')
		})
	})

	it('rejects mutations before the handshake completes — nothing buffers', async () => {
		const socket = createAgentSocket('_superagent', () => {})

		// No connection at all yet.
		await expect(socket.sendPrompt('offline', null)).rejects.toThrow(
			'not connected'
		)

		// Connected but still replaying: the surface does not know the
		// conversation it would act on.
		openSocket()
		connected()
		await expect(socket.sendPrompt('too early', null)).rejects.toThrow(
			'not synchronized'
		)
		await expect(socket.sendNow('q-1', null)).rejects.toThrow(
			'not synchronized'
		)
		expect(sentFrames()).toEqual([
			{ type: 'sync', agent_id: '_superagent', data: {} }
		])
	})

	it('settles an unanswered operation as unconfirmed after 15 seconds', async () => {
		vi.useFakeTimers()
		const socket = createAgentSocket('_superagent', () => {})
		goLive()

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
		goLive()
		socket.close()
		const socketCount = sockets.length

		await expect(socket.sendPrompt('after close', null)).rejects.toThrow(
			'not connected'
		)
		socket.sendInterrupt()

		vi.advanceTimersByTime(120_000)
		expect(sockets).toHaveLength(socketCount)

		vi.useRealTimers()
	})

	it('a concrete selection rides the frame as the model pair', () => {
		const socket = createAgentSocket('_superagent', () => {})
		goLive()

		socket.sendPrompt('hi', { id: 'openai/gpt-4o', providerId: 'p-1' })

		expect(sentFrames()[1]).toMatchObject({
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
		goLive()

		socket.sendInterrupt()

		expect(sentFrames()[1]).toEqual({
			type: 'interrupt',
			agent_id: '_superagent'
		})
	})

	it('sendInterrupt is a no-op until the handshake completes', () => {
		const socket = createAgentSocket('_superagent', () => {})
		openSocket()
		connected()

		socket.sendInterrupt()

		expect(sentFrames()).toEqual([
			{ type: 'sync', agent_id: '_superagent', data: {} }
		])
	})

	it('close settles pending operations and closes the socket', async () => {
		const socket = createAgentSocket('_superagent', () => {})
		goLive()
		const pending = socket.sendPrompt('never answered', null)

		socket.close()
		expect(lastSocket().readyState).toBe(3)
		await expect(pending).resolves.toMatchObject({ accepted: false })
	})

	it('reports connection state: syncing on the greeting, then live on ready', () => {
		const states: string[] = []
		createAgentSocket(
			'_superagent',
			() => {},
			(state) => states.push(state)
		)

		connected()
		syncReady()

		expect(states).toEqual(['connecting', 'syncing', 'live'])
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
		goLive()
		const before = sockets.length

		lastSocket().close()
		expect(states).toEqual(['connecting', 'syncing', 'live', 'reconnecting'])
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

	it('declares itself failed after ten attempts and stops', () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		const states: string[] = []
		const socket = createAgentSocket(
			'_superagent',
			() => {},
			(state) => states.push(state)
		)
		const before = sockets.length

		// Ten cycles where every socket closes before the greeting: no
		// attempt ever counts as a success, so the ceiling is reached.
		for (let attempt = 0; attempt < 10; attempt++) {
			lastSocket().close()
			// Longer than the 15s backoff cap, so each cycle lands exactly
			// one retry.
			vi.advanceTimersByTime(15_000)
		}

		expect(states.at(-1)).toBe('failed')
		// The handle lives ten connections: the first one plus nine
		// retries. The tenth failed attempt declares the ceiling.
		expect(sockets).toHaveLength(before + 9)
		// Terminal: further time creates no eleventh connection.
		vi.advanceTimersByTime(300_000)
		expect(sockets).toHaveLength(before + 9)

		socket.close()
		expect(states.at(-1)).toBe('failed')

		vi.useRealTimers()
		vi.restoreAllMocks()
	})

	it('spends the retry budget on greeting-then-close cycles until sync_ready', () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		const states: string[] = []
		createAgentSocket(
			'_superagent',
			() => {},
			(state) => states.push(state)
		)
		const before = sockets.length

		// The greeting does not reset the budget: a server that
		// greets and dies before synchronization keeps counting toward
		// the ceiling.
		for (let attempt = 0; attempt < 10; attempt++) {
			openSocket()
			connected()
			lastSocket().close()
			vi.advanceTimersByTime(15_000)
		}

		expect(states.at(-1)).toBe('failed')
		expect(sockets).toHaveLength(before + 9)
		vi.advanceTimersByTime(300_000)
		expect(sockets).toHaveLength(before + 9)

		vi.useRealTimers()
		vi.restoreAllMocks()
	})

	it('resets the retry budget at sync readiness', () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		createAgentSocket('_superagent', () => {})
		goLive()

		// Nine drops, each greeted but never synchronized — one short
		// of the ceiling.
		for (let attempt = 0; attempt < 9; attempt++) {
			openSocket()
			connected()
			lastSocket().close()
			vi.advanceTimersByTime(15_000)
		}

		// The next connection reaches sync_ready: the budget starts
		// fresh, so a later drop reconnects on the base delay, not the
		// ceiling.
		openSocket()
		connected()
		syncReady()
		const before = sockets.length
		lastSocket().close()
		vi.advanceTimersByTime(999)
		expect(sockets).toHaveLength(before)
		vi.advanceTimersByTime(1)
		expect(sockets).toHaveLength(before + 1)

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
		goLive()

		lastSocket().close()
		const rejected = socket.sendPrompt('typed while down', null)
		await expect(rejected).rejects.toThrow('not connected')

		vi.advanceTimersByTime(1000)
		openSocket()
		connected()

		// The fresh connection carries the handshake only — the typed
		// prompt never rides it.
		expect(sentFrames()).toEqual([
			{ type: 'sync', agent_id: '_superagent', data: { cursor: 'v1:h1:9' } }
		])

		vi.useRealTimers()
		vi.restoreAllMocks()
	})

	it('a chunk gap closes the socket; the next handshake replays from the durable cursor', () => {
		vi.useFakeTimers()
		vi.spyOn(Math, 'random').mockReturnValue(0.5)
		createAgentSocket('_superagent', () => {})
		goLive('v1:h1:9')

		chunk('turn-1', 'par', 1)
		chunk('turn-1', 'tial', 2)
		// A missing sequence means text was lost: never display it.
		chunk('turn-1', ' torn', 4)
		expect(lastSocket().readyState).toBe(3)

		vi.advanceTimersByTime(1000)
		openSocket()
		connected()
		expect(sentFrames()).toEqual([
			{ type: 'sync', agent_id: '_superagent', data: { cursor: 'v1:h1:9' } }
		])

		vi.useRealTimers()
		vi.restoreAllMocks()
	})

	it('a stream resumed from a prefix continues after its last sequence', () => {
		const events: AgentSocketEvent[] = []
		createAgentSocket('_superagent', (event) => events.push(event))
		openSocket()
		connected()
		syncReady('v1:h1:9', {
			item_id: 'turn-1',
			text: 'partial ',
			last_seq: 2
		})

		chunk('turn-1', 'answer', 3)
		expect(
			events.filter((event) => event.type === 'message_chunk')
		).toHaveLength(1)
	})

	it('a duplicate chunk sequence is dropped, not displayed twice', () => {
		const events: AgentSocketEvent[] = []
		createAgentSocket('_superagent', (event) => events.push(event))
		goLive()

		chunk('turn-1', 'hel', 1)
		chunk('turn-1', 'lo', 1)

		const chunks = events.filter((event) => event.type === 'message_chunk')
		expect(chunks.map((event) => event.data.delta)).toEqual(['hel'])
	})

	it('a new turn’s stream restarts at sequence one', () => {
		const events: AgentSocketEvent[] = []
		createAgentSocket('_superagent', (event) => events.push(event))
		goLive()

		chunk('turn-1', 'first ', 1)
		chunk('turn-1', 'answer', 2)
		chunk('turn-2', 'next answer', 1)

		const chunks = events.filter((event) => event.type === 'message_chunk')
		expect(chunks.map((event) => [event.data.id, event.data.delta])).toEqual([
			['turn-1', 'first '],
			['turn-1', 'answer'],
			['turn-2', 'next answer']
		])
	})

	it('drops frames whose type has no event arm', () => {
		const events: AgentSocketEvent[] = []
		createAgentSocket('_superagent', (event) => events.push(event))

		receive({ type: 'message_delta', data: { text: 'x' } })
		receive({ data: 'not json' })

		expect(events).toEqual([])
	})

	it('a frame the surface cannot apply closes the socket without checkpointing', () => {
		// The cursor advances only past frames the surface reduced: a
		// snapshot that fails application closes the connection instead
		// of skipping ahead, so the reconnect resynchronizes from the
		// last applied cursor.
		const socket = createAgentSocket('_superagent', () => {
			throw new Error('projection rejected the frame')
		})
		openSocket()
		connected()

		receive({ type: 'sync_snapshot', data: { cursor: 'v1:h1:5', page: null } })

		expect(lastSocket().readyState).toBe(3)
		const followUps = sentFrames().filter(
			(frame) => frame.type === 'sync' && frame.data?.cursor === 'v1:h1:5'
		)
		expect(followUps).toEqual([])
		socket.close()
	})

	it('drops a malformed chunk without disabling gap detection', async () => {
		vi.useFakeTimers()
		const events: AgentSocketEvent[] = []
		createAgentSocket('_superagent', (event) => events.push(event))
		goLive()

		// A chunk without its sequence or identity cannot be placed in the
		// stream, so it is dropped before it can look like applied text.
		receive({ type: 'message_chunk', data: { id: 'turn-1', delta: 'orphan' } })
		receive({ type: 'message_chunk', data: { delta: 'no id', seq: 1 } })
		expect(events.filter((event) => event.type === 'message_chunk')).toEqual([])

		// The stream then starts cleanly, and a real gap still closes the
		// socket instead of displaying torn text.
		chunk('turn-1', 'first', 1)
		chunk('turn-1', 'lost', 3)
		expect(
			events.filter((event) => event.type === 'message_chunk')
		).toHaveLength(1)
		expect(lastSocket().readyState).toBe(3)

		vi.useRealTimers()
	})

	it('retries on the backoff schedule when the constructor throws', () => {
		vi.useFakeTimers()
		const states: string[] = []
		const throwing = vi.fn(() => {
			throw new Error('websocket unavailable')
		})
		vi.stubGlobal('WebSocket', throwing)

		createAgentSocket(
			'_superagent',
			() => {},
			(state) => states.push(state)
		)

		// The first attempt fails inside the constructor: the socket stays
		// closed and the handle reports a reconnect instead of dying.
		expect(states).toEqual(['connecting', 'reconnecting'])
		expect(throwing).toHaveBeenCalledTimes(1)

		vi.advanceTimersByTime(2000)
		expect(throwing).toHaveBeenCalledTimes(2)

		vi.stubGlobal('WebSocket', FakeWebSocket)
		vi.useRealTimers()
	})

	it('a socket closed by its owner ignores late frames', () => {
		const events: AgentSocketEvent[] = []
		const socket = createAgentSocket('_superagent', (event) =>
			events.push(event)
		)
		goLive()
		socket.close()

		receive({ type: 'lifecycle', data: { cursor: 'v1:h1:99', events: [] } })
		chunk('turn-1', 'stale text', 9)

		expect(events.filter((event) => event.type === 'lifecycle')).toEqual([])
	})
})
