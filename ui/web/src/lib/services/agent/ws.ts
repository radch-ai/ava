/**
 * Gateway WebSocket client
 *
 * One connection per agent conversation, created lazily on first send and
 * owned by the surface that created it — later conversation modes hold
 * their own socket the same way.
 *
 * The gateway speaks one tagged envelope; this module decodes frames
 * into typed events and types outgoing messages so invalid ones do
 * not compile. Wire shapes come from the shared contract generated
 * by `just typegen` (gateway/wstypes → wstypes.gen).
 *
 * Browsers never auto-reconnect WebSockets, so this module owns the
 * retry loop: any drop schedules a backoff reconnect, and messages
 * queued in the gap flush on reopen. Liveness itself is the gateway's
 * job (protocol-level pings); a closed socket here means the gateway
 * went away or the network did.
 */

import type {
	ConnectedEvent,
	Envelope,
	Event,
	LaneStatus,
	PromptMessage
} from '#lib/types/gen/wstypes.gen'

export type { LaneStatus }

/** Events the gateway sends. Types without an arm here are dropped at
 * the decode boundary. */
export type AgentSocketEvent =
	| (Event & { type: 'connected'; data: ConnectedEvent })
	| (Event & { type: 'lane_status'; data: LaneStatus })

/** Client wire frames: the envelope head plus the per-kind payload
 * (gateway/wstypes → wstypes.gen). */
export type ClientMessage =
	| (Envelope & { type: 'prompt'; data: PromptMessage })
	| (Envelope & { type: 'status' })
	| (Envelope & { type: 'interrupt' })

/** Connection state the owning surface renders. `live` is declared by
 * the gateway's connected event, not by the TCP handshake. */
export type AgentSocketState = 'connecting' | 'live' | 'reconnecting'

/** The handle a conversation surface owns: send frames, drop the
 * connection. Reconnects happen inside. */
export interface AgentSocket {
	sendPrompt(content: string, model: string): void
	/** The gateway replies with a `lane_status` event. */
	requestStatus(): void
	close(): void
}

const RECONNECT_BASE_MS = 1000
const RECONNECT_CAP_MS = 15000

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
	const pending: ClientMessage[] = []

	function setState(state: AgentSocketState) {
		onState?.(state)
	}

	function connect() {
		// Same-origin /ws like the api client's /api: proxied by the vite
		// dev/preview servers; prod serves it from the single binary.
		setState(reconnectAttempts > 0 ? 'reconnecting' : 'connecting')
		socket = new WebSocket('/ws')
		socket.onopen = () => {
			for (const msg of pending.splice(0)) {
				send(msg)
			}
		}
		socket.onmessage = (message) => {
			try {
				const event = decodeEvent(JSON.parse(message.data))
				if (event) {
					if (event.type === 'connected') {
						reconnectAttempts = 0
						setState('live')
					}
					onEvent(event)
				}
			} catch {
				console.warn('agent socket: unparseable frame', message.data)
			}
		}
		socket.onclose = () => {
			socket = undefined
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
		setState('reconnecting')
		reconnectTimer = setTimeout(() => {
			reconnectTimer = undefined
			if (!socket) {
				connect()
			}
		}, delay)
	}

	function send(msg: ClientMessage) {
		if (!socket || socket.readyState !== WebSocket.OPEN) {
			pending.push(msg)
			return
		}
		socket.send(JSON.stringify(msg))
	}

	function ensureConnected() {
		if (!socket && !reconnectTimer) {
			connect()
		}
	}

	return {
		sendPrompt(content: string, model: string) {
			ensureConnected()
			send({
				type: 'prompt',
				agent_id: agentId,
				data: { content, model }
			})
		},

		/** The gateway replies with a `lane_status` event. */
		requestStatus() {
			ensureConnected()
			send({ type: 'status', agent_id: agentId })
		},

		close() {
			closedByUs = true
			pending.length = 0
			if (reconnectTimer) {
				clearTimeout(reconnectTimer)
				reconnectTimer = undefined
			}
			socket?.close()
			socket = undefined
		}
	}
}
