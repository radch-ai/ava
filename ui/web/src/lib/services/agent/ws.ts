/**
 * Gateway WebSocket client
 *
 * One connection per agent conversation, connected eagerly on
 * creation and owned by the surface that created it — later
 * conversation modes hold their own socket the same way.
 *
 * The gateway speaks one tagged envelope; this module decodes frames
 * into typed events and types outgoing messages so invalid ones do
 * not compile. Wire shapes come from the shared contract generated
 * by `just typegen` (gateway/wstypes → wstypes.gen).
 *
 * Browsers never auto-reconnect WebSockets, so this module owns the
 * retry loop. Mutations (prompt, send_now) are request-correlated
 * and answered by queue_result; they are never buffered across a
 * reconnect — an admission is the gateway's to confirm, and a frame
 * replayed after a gap could arrive in a different world than the
 * one it was typed in. A lost connection settles outstanding
 * operations as unconfirmed; the caller keeps the draft, and the
 * queue snapshot that follows the next live transition is the truth.
 * Liveness itself is the gateway's job (protocol-level pings); a
 * closed socket here means the gateway went away or the network did.
 */

import type { ModelSelection } from '#lib/models'
import type {
	CommandCompletedEvent,
	ConnectedEvent,
	Envelope,
	ErrorEvent,
	Event,
	LaneStatus,
	MessageChunkEvent,
	MessageEvent,
	PromptMessage,
	QueueResultEvent,
	QueueStateEvent,
	SendNowMessage,
	ToolCallEvent,
	ToolResultEvent
} from '#lib/types/gen/wstypes.gen'

export type { LaneStatus, QueueResultEvent, QueueStateEvent }

/** Events the gateway sends. Types without an arm here are dropped at
 * the decode boundary. */
export type AgentSocketEvent =
	| (Event & { type: 'connected'; data: ConnectedEvent })
	| (Event & { type: 'lane_status'; data: LaneStatus })
	| (Event & { type: 'message_chunk'; data: MessageChunkEvent })
	| (Event & { type: 'tool_call'; data: ToolCallEvent })
	| (Event & { type: 'tool_result'; data: ToolResultEvent })
	| (Event & { type: 'message'; data: MessageEvent })
	| (Event & { type: 'error'; data: ErrorEvent })
	| (Event & { type: 'queue_result'; data: QueueResultEvent })
	| (Event & { type: 'queue_state'; data: QueueStateEvent })
	| (Event & { type: 'command_completed'; data: CommandCompletedEvent })

/** Client wire frames: the envelope head plus the per-kind payload
 * (gateway/wstypes → wstypes.gen). */
export type ClientMessage =
	| (Envelope & { type: 'prompt'; data: PromptMessage })
	| (Envelope & { type: 'send_now'; data: SendNowMessage })
	| (Envelope & { type: 'status' })
	| (Envelope & { type: 'interrupt' })

/** Connection state the owning surface renders. `live` is declared by
 * the gateway's connected event, not by the TCP handshake. */
export type AgentSocketState = 'connecting' | 'live' | 'reconnecting'

/** One queue operation's outcome, correlated by request id. */
export interface QueueOperation {
	accepted: boolean
	/** The admitted or targeted item's stable id, when known. */
	itemId?: string
	/** User-facing rejection reason; empty when accepted. */
	error?: string
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
	/** The gateway replies with a `lane_status` event. */
	requestStatus(): void
	/** Cancels the running turn; the lane broadcasts its end. */
	sendInterrupt(): void
	close(): void
}

const RECONNECT_BASE_MS = 1000
const RECONNECT_CAP_MS = 15000
/** How long a mutation waits for its queue_result before resolving
 * as unconfirmed — a lost ack must not disable the composer forever. */
const ACK_TIMEOUT_MS = 15_000

/** Decode one wire frame into its typed event; the only place raw
 * payload data is cast. */
function decodeEvent(frame: unknown): AgentSocketEvent | undefined {
	if (typeof frame !== 'object' || frame === null) {
		return undefined
	}
	const typed = frame as { type?: unknown; agent_id?: unknown; data?: unknown }
	if (typeof typed.type !== 'string') {
		return undefined
	}
	switch (typed.type) {
		case 'connected':
			return {
				type: 'connected',
				data: (typed.data ?? {}) as ConnectedEvent
			}
		case 'lane_status':
			return {
				type: 'lane_status',
				agent_id: typed.agent_id as string,
				data: typed.data as LaneStatus
			}
		case 'message_chunk':
			return {
				type: 'message_chunk',
				agent_id: typed.agent_id as string,
				data: typed.data as MessageChunkEvent
			}
		case 'tool_call':
			return {
				type: 'tool_call',
				agent_id: typed.agent_id as string,
				data: typed.data as ToolCallEvent
			}
		case 'tool_result':
			return {
				type: 'tool_result',
				agent_id: typed.agent_id as string,
				data: typed.data as ToolResultEvent
			}
		case 'message':
			return {
				type: 'message',
				agent_id: typed.agent_id as string,
				data: typed.data as MessageEvent
			}
		case 'error':
			return {
				type: 'error',
				agent_id: typed.agent_id as string,
				data: typed.data as ErrorEvent
			}
		case 'queue_result':
			return {
				type: 'queue_result',
				agent_id: typed.agent_id as string,
				data: typed.data as QueueResultEvent
			}
		case 'queue_state':
			return {
				type: 'queue_state',
				agent_id: typed.agent_id as string,
				data: typed.data as QueueStateEvent
			}
		case 'command_completed':
			return {
				type: 'command_completed',
				agent_id: typed.agent_id as string,
				data: typed.data as CommandCompletedEvent
			}
		default:
			console.debug('agent socket: untyped event', frame)
			return undefined
	}
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

	// Outstanding request-correlated operations: settled by the
	// matching queue_result, as unconfirmed when the connection they
	// rode is gone, or as unconfirmed when the ack times out.
	interface PendingOp {
		settle: (result: QueueOperation) => void
		timer: ReturnType<typeof setTimeout>
	}
	const pendingOps = new Map<string, PendingOp>()

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
			op.settle({ accepted: false, error: detail })
		}
	}

	function connect() {
		// Same-origin /ws like the api client's /api: proxied by the
		// vite dev/preview servers; prod serves it from the single
		// binary.
		onState?.(reconnectAttempts > 0 ? 'reconnecting' : 'connecting')
		socket = new WebSocket('/ws')
		socket.onmessage = (message) => {
			try {
				const event = decodeEvent(JSON.parse(message.data))
				if (!event) {
					return
				}
				if (event.type === 'connected') {
					reconnectAttempts = 0
					onState?.('live')
				}
				if (event.type === 'queue_result') {
					settleOp(event.data.request_id, {
						accepted: event.data.accepted,
						itemId: event.data.item_id || undefined,
						error: event.data.error || undefined
					})
				}
				onEvent(event)
			} catch {
				console.warn('agent socket: unparseable frame', message.data)
			}
		}
		socket.onclose = () => {
			socket = undefined
			// A mutation that never heard back is unconfirmed, not
			// rejected: the reconnect's fresh queue snapshot is the
			// truth, so no frame ever replays from here.
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
		onState?.('reconnecting')
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
		const requestId = crypto.randomUUID()
		const { promise, resolve } = Promise.withResolvers<QueueOperation>()
		const timer = setTimeout(() => {
			settleOp(requestId, {
				accepted: false,
				error: 'unconfirmed: the gateway did not answer in time'
			})
		}, ACK_TIMEOUT_MS)
		pendingOps.set(requestId, { settle: resolve, timer })
		socket.send(JSON.stringify(build(requestId)))
		return promise
	}

	function sendFrame(msg: ClientMessage) {
		if (socket && socket.readyState === WebSocket.OPEN) {
			socket.send(JSON.stringify(msg))
		}
	}

	function ensureConnected() {
		if (!closedByUs && !socket && !reconnectTimer) {
			connect()
		}
	}

	// Connect eagerly: a freshly mounted page needs the connection
	// up for its lane-status join — connecting on first send would be
	// too late.
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
		requestStatus() {
			sendFrame({ type: 'status', agent_id: agentId })
		},
		sendInterrupt() {
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
