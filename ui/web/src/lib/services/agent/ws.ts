/**
 * Gateway WebSocket client
 *
 * One connection per conversation surface. Every (re)connection runs the
 * synchronization handshake before the surface may mutate: `sync` with the
 * last applied cursor, then whichever answer the gateway gives —
 * `sync_snapshot` (the authoritative baseline), `sync_batch` (retained
 * lifecycle events after the cursor), and finally `sync_ready` (the runtime
 * snapshot plus the live subscription). Frames from the durable feed are
 * forwarded verbatim; the surface validates and reduces them.
 *
 * Streaming stays transient: chunk frames carry the stream's own sequence,
 * so a gap closes the socket and the handshake replays from the last durable
 * cursor instead of displaying torn text.
 */

import type { ModelSelection } from '#lib/models'
import {
	type ConnectedEvent,
	type Envelope,
	type ErrorEvent,
	EventTypeConnected,
	EventTypeError,
	EventTypeLaneStatus,
	EventTypeLifecycle,
	EventTypeMessageChunk,
	EventTypeQueueResult,
	EventTypeQueueState,
	EventTypeSyncBatch,
	EventTypeSyncReady,
	EventTypeSyncSnapshot,
	type LaneStatus,
	type LifecycleBatchEvent,
	type MessageChunkEvent,
	type PromptMessage,
	type QueueResultEvent,
	type QueueStateEvent,
	type SendNowMessage,
	type SyncMessage,
	type SyncReadyEvent,
	type SyncSnapshotEvent
} from '#lib/types/gen/wstypes.gen'

export type { LaneStatus, QueueStateEvent }

/** Events forwarded to the owning surface. Only these types pass the
 * decode boundary to `onEvent`; anything else — including a future
 * frame this client predates — is dropped once, with a single debug
 * line. */
export type AgentSocketEvent =
	| { type: 'lane_status'; data: LaneStatus }
	| { type: 'message_chunk'; data: MessageChunkEvent }
	| { type: 'error'; data: ErrorEvent }
	| { type: 'queue_state'; data: QueueStateEvent }
	| { type: 'sync_snapshot'; data: SyncSnapshotEvent }
	| { type: 'sync_batch'; data: LifecycleBatchEvent }
	| { type: 'sync_ready'; data: SyncReadyEvent }
	| { type: 'lifecycle'; data: LifecycleBatchEvent }

/** Frames the socket consumes itself: the connected greeting drives
 * the handshake, and queue acks settle their request-correlated
 * operations. Neither is forwarded to the owning surface. */
type InternalEvent =
	| { type: 'connected'; data: ConnectedEvent }
	| { type: 'queue_result'; data: QueueResultEvent }

/** One decoded frame, whatever consumes it. */
type DecodedEvent = AgentSocketEvent | InternalEvent

/** Client wire frames: the envelope head plus the per-kind payload
 * (gateway/wstypes → wstypes.gen). */
type ClientMessage =
	| (Envelope & { type: 'prompt'; data: PromptMessage })
	| (Envelope & { type: 'send_now'; data: SendNowMessage })
	| (Envelope & { type: 'sync'; data: SyncMessage })
	| (Envelope & { type: 'interrupt' })

/** Connection state the owning surface renders. `live` is declared by the
 * gateway's sync_ready, never by the TCP handshake: until then the socket
 * has not replayed what the device missed. `failed` is terminal for this
 * socket handle — nothing reconnects; a remount or agent switch mints a
 * fresh socket, which is the retry path. */
export type AgentSocketState =
	| 'connecting'
	| 'reconnecting'
	| 'syncing'
	| 'live'
	| 'failed'

/** One queue operation's outcome, correlated by request id. */
export interface QueueOperation {
	accepted: boolean
	/** The admitted or targeted item's stable id, when known. */
	itemId?: string
	/** User-facing rejection reason; empty when accepted. */
	error?: string
	/** Set when the answer never arrived (connection lost, ack
	 * timeout): the mutation may or may not have landed, so the queue
	 * snapshot — not this result — is the truth. A caller must not
	 * present an unconfirmed operation as a rejection. */
	unconfirmed?: true
}

/** The handle a conversation surface owns: send frames, drop the
 * connection. Reconnects happen inside. */
export interface AgentSocket {
	/** Enqueues a prompt; resolves when the gateway answers. */
	sendPrompt(
		content: string,
		selection: ModelSelection
	): Promise<QueueOperation>
	/** Promotes one pending item to run next; the selection rides the
	 * frame so a paused queue can rebind its failed-model entries.
	 * Resolves on the answer. */
	sendNow(itemId: string, selection: ModelSelection): Promise<QueueOperation>
	/** Cancels the running turn; the lane broadcasts its end. */
	sendInterrupt(): void
	close(): void
}

const RECONNECT_BASE_MS = 1000
const RECONNECT_CAP_MS = 15000
/** Reconnect ceiling: ten attempts ≈ 100 s of 1s→15s backoff with the
 * jitter applied, after which the socket declares itself failed. */
const MAX_RECONNECT_ATTEMPTS = 10
/** How long a mutation waits for its queue_result before resolving
 * as unconfirmed — a lost ack must not disable the composer forever. */
const ACK_TIMEOUT_MS = 15_000

/** The frame types this client consumes: the gateway's whole event
 * vocabulary, straight from the generated constants. Anything else is
 * a frame from a newer gateway and is ignored. */
const KNOWN_TYPES: Record<string, true> = {
	[EventTypeConnected]: true,
	[EventTypeLaneStatus]: true,
	[EventTypeMessageChunk]: true,
	[EventTypeError]: true,
	[EventTypeQueueResult]: true,
	[EventTypeQueueState]: true,
	[EventTypeSyncSnapshot]: true,
	[EventTypeSyncBatch]: true,
	[EventTypeSyncReady]: true,
	[EventTypeLifecycle]: true
}

/** Frame types already reported as unknown: one debug line each, not one
 * per frame. */
const warnedTypes = new Set<string>()

/** Decode one wire frame into its typed event; the only place raw
 * payload data is cast. */
function decodeEvent(frame: unknown): DecodedEvent | undefined {
	if (typeof frame !== 'object' || frame === null) {
		return undefined
	}
	const typed = frame as { type?: unknown; data?: unknown }
	if (typeof typed.type !== 'string' || !KNOWN_TYPES[typed.type]) {
		if (typeof typed.type === 'string' && !warnedTypes.has(typed.type)) {
			warnedTypes.add(typed.type)
			console.debug('agent socket: unknown event type', typed.type)
		}
		return undefined
	}
	if (typed.type === EventTypeConnected) {
		return { type: 'connected', data: (typed.data ?? {}) as ConnectedEvent }
	}
	if (typed.type === EventTypeMessageChunk) {
		// A chunk without its identity or sequence cannot be placed in
		// the stream, and must not advance gap detection: it is dropped
		// before it can be mistaken for text the surface applied.
		const chunk = typed.data as Partial<MessageChunkEvent> | undefined
		if (typeof chunk?.id !== 'string' || typeof chunk?.seq !== 'number') {
			return undefined
		}
		return { type: 'message_chunk', data: chunk as MessageChunkEvent }
	}
	return { type: typed.type, data: typed.data } as DecodedEvent
}

export function createAgentSocket(
	agentId: string,
	onEvent: (event: AgentSocketEvent) => void,
	onState?: (state: AgentSocketState) => void
): AgentSocket {
	let socket: WebSocket | undefined
	let closedByUs = false
	let reconnectAttempts = 0
	let reconnectTimer: ReturnType<typeof setTimeout> | undefined
	let state: AgentSocketState | undefined

	// Durable synchronization coordinates. They live for the whole socket
	// handle, so a reconnect resumes from what the surface actually
	// applied: the cursor only advances with a frame the surface received,
	// and the chunk sequence only with text it displayed.
	let lastCursor: string | undefined
	let lastChunkItemId: string | undefined
	let lastChunkSeq = 0

	// Outstanding request-correlated operations: settled by the
	// matching queue_result, as unconfirmed when the connection they
	// rode is gone, or as unconfirmed when the ack times out.
	interface PendingOp {
		settle: (result: QueueOperation) => void
		timer: ReturnType<typeof setTimeout>
	}
	const pendingOps = new Map<string, PendingOp>()

	function setState(next: AgentSocketState) {
		if (state === next) {
			return
		}
		state = next
		onState?.(next)
	}

	function settleOp(requestId: string, result: QueueOperation) {
		const op = pendingOps.get(requestId)
		if (!op) {
			return
		}
		pendingOps.delete(requestId)
		clearTimeout(op.timer)
		op.settle(result)
	}

	function settleUnconfirmed(detail: string) {
		for (const [requestId, op] of pendingOps) {
			pendingOps.delete(requestId)
			clearTimeout(op.timer)
			op.settle({ accepted: false, error: detail, unconfirmed: true })
		}
	}

	function sendFrame(msg: ClientMessage) {
		if (socket && socket.readyState === WebSocket.OPEN) {
			socket.send(JSON.stringify(msg))
		}
	}

	/** Asks the gateway for the next synchronization step from the last
	 * applied cursor. */
	function requestSync() {
		sendFrame({
			type: 'sync',
			agent_id: agentId,
			data: lastCursor ? { cursor: lastCursor } : {}
		})
	}

	type ChunkVerdict = 'apply' | 'drop' | 'gap'

	/** One chunk's place in the running stream. Three verdicts: a
	 * duplicate sequence is `drop` — a repeated frame the surface
	 * already displayed; `apply` advances the running stream; `gap`
	 * means text was lost, so the socket closes and the reconnect hands
	 * the retained cursor back to the gateway instead of showing a torn
	 * answer. */
	function classifyChunk(itemId: string, seq: number): ChunkVerdict {
		if (itemId !== lastChunkItemId) {
			if (seq > 1) {
				return 'gap'
			}
			lastChunkItemId = itemId
			lastChunkSeq = seq
			return 'apply'
		}
		if (seq <= lastChunkSeq) {
			return 'drop'
		}
		if (seq > lastChunkSeq + 1) {
			return 'gap'
		}
		lastChunkSeq = seq
		return 'apply'
	}

	function connect() {
		// Same-origin /ws like the api client's /api: proxied by the
		// vite dev/preview servers; prod serves it from the single
		// binary.
		setState(reconnectAttempts > 0 ? 'reconnecting' : 'connecting')
		try {
			socket = new WebSocket('/ws')
		} catch (err) {
			// The constructor throws when the URL is unusable or the
			// browser refuses the connection outright: treat it like any
			// other drop and retry on the backoff schedule.
			socket = undefined
			console.warn('agent socket: connect failed', err)
			scheduleReconnect()
			return
		}
		socket.onmessage = (message) => {
			// A socket closed by its owner stops consuming: late frames
			// from a replaced or stale connection never mutate the
			// current projection.
			if (closedByUs) {
				return
			}
			try {
				const event = decodeEvent(JSON.parse(message.data))
				if (!event) {
					return
				}
				if (event.type === 'connected') {
					// The greeting is not readiness: the surface stays
					// non-live until the committed state has been
					// replayed, and the retry budget is not spent yet —
					// a server that greets and dies before sync still
					// counts.
					// The socket consumes it here — no surface consumes
					// a connected frame.
					setState('syncing')
					requestSync()
					return
				}
				if (event.type === 'queue_result') {
					settleOp(event.data.request_id ?? '', {
						accepted: event.data.accepted,
						itemId: event.data.item_id || undefined,
						error: event.data.error || undefined
					})
					return
				}
				if (event.type === 'message_chunk') {
					const verdict = classifyChunk(event.data.id, event.data.seq)
					if (verdict === 'gap') {
						// A gap: never display incomplete text. The
						// reconnect replays from the durable cursor and
						// the prefix snapshot restores the stream.
						lastChunkItemId = undefined
						socket?.close()
						return
					}
					if (verdict === 'drop') return
				}
				// Apply before checkpointing: the cursor advances only
				// past frames the surface actually reduced. A frame that
				// fails application closes the socket instead of being
				// skipped — the reconnect resynchronizes from the last
				// applied cursor rather than silently falling behind.
				try {
					onEvent(event)
				} catch (err) {
					console.warn('agent socket: frame not applied', event.type, err)
					lastChunkItemId = undefined
					socket?.close()
					return
				}
				if ('cursor' in event.data) {
					lastCursor = event.data.cursor
				}
				if (event.type === 'sync_snapshot' || event.type === 'sync_batch') {
					// One step at a time: the next request decides between
					// more replay and the runtime hand-off.
					requestSync()
				}
				if (event.type === 'sync_ready') {
					lastChunkItemId = event.data.prefix?.item_id
					lastChunkSeq = event.data.prefix?.last_seq ?? 0
					// Readiness is the first confirmed round trip: only
					// now does a later drop reconnect with a fresh
					// budget.
					reconnectAttempts = 0
					setState('live')
				}
			} catch {
				console.warn('agent socket: unparseable frame', message.data)
			}
		}
		socket.onclose = () => {
			socket = undefined
			// A mutation that never heard back is unconfirmed, not
			// rejected: the handshake's snapshot is the truth, so no
			// frame ever replays from here.
			settleUnconfirmed('connection lost before the gateway answered')
			if (closedByUs) {
				return
			}
			scheduleReconnect()
		}
	}

	function scheduleReconnect() {
		const delay =
			Math.min(RECONNECT_BASE_MS * 2 ** reconnectAttempts, RECONNECT_CAP_MS) *
			(0.75 + Math.random() / 2)
		reconnectAttempts += 1
		if (reconnectAttempts >= MAX_RECONNECT_ATTEMPTS) {
			// Terminal: no timer, no further connects. The surface swaps
			// to failed and a remount is the retry path.
			setState('failed')
			return
		}
		setState('reconnecting')
		reconnectTimer = setTimeout(() => {
			reconnectTimer = undefined
			if (!socket) {
				connect()
			}
		}, delay)
	}

	/** Mutations require a live connection; they are never buffered,
	 * and a socket closed by us never reopens. */
	function submitMutation(
		build: (requestId: string) => ClientMessage
	): Promise<QueueOperation> {
		if (closedByUs || !socket || socket.readyState !== WebSocket.OPEN) {
			return Promise.reject(new Error('not connected'))
		}
		if (state !== 'live') {
			// Recovery has not finished: the surface does not yet know the
			// conversation it would act on, so nothing is sent.
			return Promise.reject(new Error('not synchronized'))
		}
		const requestId = crypto.randomUUID()
		const { promise, resolve } = Promise.withResolvers<QueueOperation>()
		const timer = setTimeout(() => {
			settleOp(requestId, {
				accepted: false,
				error: 'unconfirmed: the gateway did not answer in time',
				unconfirmed: true
			})
		}, ACK_TIMEOUT_MS)
		pendingOps.set(requestId, { settle: resolve, timer })
		socket.send(JSON.stringify(build(requestId)))
		return promise
	}

	function ensureConnected() {
		if (!closedByUs && !socket && !reconnectTimer) {
			connect()
		}
	}

	// Connect eagerly: a freshly mounted page needs the handshake running
	// before it can render the current turn — connecting on first send
	// would be too late.
	ensureConnected()

	return {
		sendPrompt(content: string, selection: ModelSelection) {
			return submitMutation((requestId) => ({
				type: 'prompt',
				agent_id: agentId,
				data: {
					request_id: requestId,
					content,
					// ws frames use provider_id casing
					model: selection
						? { id: selection.id, provider_id: selection.providerId }
						: undefined
				}
			}))
		},
		sendNow(itemId: string, selection: ModelSelection) {
			return submitMutation((requestId) => ({
				type: 'send_now',
				agent_id: agentId,
				data: {
					request_id: requestId,
					item_id: itemId,
					// ws frames use provider_id casing
					model: selection
						? { id: selection.id, provider_id: selection.providerId }
						: undefined
				}
			}))
		},
		sendInterrupt() {
			if (state !== 'live') {
				return
			}
			sendFrame({ type: 'interrupt', agent_id: agentId })
		},

		/** Terminal: a closed socket never reopens — the page owns a
		 * fresh socket per mount. */
		close() {
			closedByUs = true
			settleUnconfirmed('closed before the gateway answered')
			if (reconnectTimer) {
				clearTimeout(reconnectTimer)
				reconnectTimer = undefined
			}
			socket?.close()
			socket = undefined
		}
	}
}
