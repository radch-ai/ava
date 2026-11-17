/**
 * Live turn state: the conversation tail assembled from gateway
 * events while turns run. Ephemeral by design — nothing here
 * persists; reloading the page shows only the stored thread.
 *
 * Rows carry server ids: the user row the claim minted
 * (user_message_id) and the queue item id that streams the answer,
 * so turns reconcile by identity even when two prompts read alike.
 * Tool rows key on turn + call: a provider may reuse a tool-call id
 * across turns, and a late result must land on its own turn's row.
 */

import type { AgentSocketEvent } from './ws'

export interface LiveMessage {
	id: string
	role: 'user' | 'assistant' | 'tool' | 'error'
	content: string
	/** True while chunks are still arriving for an assistant message. */
	pending?: boolean
	/** Tool rows only: the invocation's turn (queue item) id. */
	turnId?: string
	name?: string
	args?: string
	result?: string
	error?: string | null
}

/** The composite identity of one tool invocation. */
function toolRowId(turnId: string, callId: string): string {
	return `${turnId}:${callId}`
}

/** Creates the live-tail store for one conversation surface. */
export function createLiveTurns() {
	let messages = $state<LiveMessage[]>([])

	/** Adds a turn's user row under its server-minted id; repeats for
	 * the same id (snapshot replays) never duplicate. */
	function beginUser(id: string, text: string) {
		if (messages.some((m) => m.id === id)) {
			return
		}
		messages.push({
			id,
			role: 'user',
			content: text
		})
		// The reasoning marker is the running turn's answer stand-in,
		// so it trails the newest user row. A join can deliver the
		// lane's reasoning signal before the snapshot that mints this
		// row — relocate markers that landed above it.
		const markers = messages.filter(
			(m) => m.role === 'assistant' && !m.content && m.id.startsWith('local-')
		)
		if (markers.length > 0) {
			messages = messages.filter((m) => !markers.includes(m))
			messages.push(...markers)
		}
	}

	/** Folds one gateway event into the tail; non-turn events are ignored. */
	function applyEvent(event: AgentSocketEvent) {
		switch (event.type) {
			case 'tool_call': {
				messages.push({
					id: toolRowId(event.data.turn_id ?? '', event.data.id),
					role: 'tool',
					turnId: event.data.turn_id,
					content: event.data.name,
					name: event.data.name,
					args: event.data.args,
					pending: true
				})
				break
			}
			case 'tool_result': {
				const rowId = toolRowId(event.data.turn_id ?? '', event.data.id)
				const existing = messages.find((m) => m.id === rowId)
				if (existing) {
					existing.result = event.data.result
					existing.error = event.data.error ?? null
					existing.pending = false
				}
				break
			}
			case 'message_chunk': {
				const existing = messages.find((m) => m.id === event.data.id)
				if (existing) {
					existing.content += event.data.delta
					existing.pending = true
				} else {
					messages.push({
						id: event.data.id,
						role: 'assistant',
						content: event.data.delta,
						pending: true
					})
				}
				break
			}
			case 'message': {
				const existing = messages.find((m) => m.id === event.data.id)
				if (existing) {
					existing.content = event.data.content
					existing.pending = false
				} else {
					messages.push({
						id: event.data.id,
						role: 'assistant',
						content: event.data.content,
						pending: false
					})
				}
				break
			}
			case 'error':
				{
					// A tool-attributed failure adds no bubble — the tool's
					// own row shows it.
					if (!event.data.tool) {
						messages.push({
							// A stored failure keeps its row id, so the
							// settled copy and this one reconcile; transient
							// errors stay locally keyed.
							id: event.data.id || `local-${crypto.randomUUID()}`,
							role: 'error',
							content: event.data.message
						})
					}
					// The failed turn's streamed prefix is done: it never
					// becomes a pending row.
					const failed = messages.find(
						(m) => m.id === event.data.turn_id && m.role === 'assistant'
					)
					if (failed) failed.pending = false
				}
				break
			case 'lane_status': {
				// While the lane reports the model reasoning, a marker
				// stands in for the answer row that does not exist yet;
				// the lane always lowers the signal before the turn ends,
				// which removes it.
				const marker = messages.find(
					(m) => m.role === 'assistant' && !m.content
				)
				if (event.data.reasoning && !marker) {
					messages.push({
						id: `local-${crypto.randomUUID()}`,
						role: 'assistant',
						content: ''
					})
				} else if (!event.data.reasoning && marker) {
					messages = messages.filter((m) => m !== marker)
				}
				break
			}
		}
	}

	/** Settles a finished turn: its rows are in the stored thread now,
	 * so the live copies go — the settled assistant entry and the
	 * user rows the refetched page carries, both by their server ids,
	 * the tool rows of this turn, whose wire ids never match the
	 * sink-minted row ids, and error bubbles the page carries by
	 * their stored id. Rows the page does not carry yet belong to
	 * turns still running; they stay. Transient errors have no stored
	 * row and never retire here. */
	function settleTurn(
		id: string,
		persistedUserIds: string[],
		persistedErrors: string[] = []
	) {
		const users = new Set(persistedUserIds)
		const errors = new Set(persistedErrors)
		messages = messages.filter(
			(m) =>
				m.id !== id &&
				!(m.role === 'tool' && (m.turnId ?? '') === id) &&
				!(m.role === 'user' && users.has(m.id)) &&
				!(m.role === 'error' && errors.has(m.id))
		)
	}
	return {
		get messages() {
			return messages
		},
		beginUser,
		applyEvent,
		settleTurn
	}
}
