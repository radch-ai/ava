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
	onEvent: (event: AgentSocketEvent) => void
) {
	let socket: WebSocket | undefined
	const pending: ClientMessage[] = []

	function connect() {
		// Same-origin /ws like the api client's /api: proxied by the vite
		// dev/preview servers; prod serves it from the single binary.
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
					onEvent(event)
				}
			} catch {
				console.warn('agent socket: unparseable frame', message.data)
			}
		}
		socket.onclose = () => {
			socket = undefined
		}
	}

	function send(msg: ClientMessage) {
		if (!socket || socket.readyState !== WebSocket.OPEN) {
			pending.push(msg)
			return
		}
		socket.send(JSON.stringify(msg))
	}

	return {
		sendPrompt(content: string, model: string) {
			if (!socket) {
				connect()
			}
			send({
				type: 'prompt',
				agent_id: agentId,
				data: { content, model }
			})
		},

		/** The gateway replies with a `lane_status` event. */
		requestStatus() {
			if (!socket) {
				connect()
			}
			send({ type: 'status', agent_id: agentId })
		},

		close() {
			pending.length = 0
			socket?.close()
			socket = undefined
		}
	}
}
