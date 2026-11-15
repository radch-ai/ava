/**
 * Live turn state: the conversation tail assembled from gateway
 * events while a turn runs. Ephemeral by design — nothing here
 * persists; reloading the page shows only the stored thread.
 */

import type { AgentSocketEvent } from './ws'

export interface LiveMessage {
	id: string
	role: 'user' | 'assistant' | 'tool' | 'error'
	content: string
	/** True while chunks are still arriving for an assistant message. */
	pending?: boolean
	name?: string
	args?: string
	result?: string
	error?: string | null
}

/** Creates the live-tail store for one conversation surface. */
export function createLiveTurns() {
	let messages = $state<LiveMessage[]>([])

	/** Adds the user's own prompt optimistically, keyed locally. */
	function echoUser(text: string) {
		messages.push({
			id: `local-${crypto.randomUUID()}`,
			role: 'user',
			content: text
		})
	}

	/** Folds one gateway event into the tail; non-turn events are ignored. */
	function applyEvent(event: AgentSocketEvent) {
		switch (event.type) {
			case 'tool_call': {
				messages.push({
					id: event.data.id,
					role: 'tool',
					content: event.data.name,
					name: event.data.name,
					args: event.data.args,
					pending: true
				})
				break
			}
			case 'tool_result': {
				const existing = messages.find((m) => m.id === event.data.id)
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
				messages.push({
					id: `local-${crypto.randomUUID()}`,
					role: 'error',
					content: event.data.message
				})
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
	 * so the live copies go — the settled assistant entry, the echoed
	 * prompts the refetched page carries, and the tool entries, whose
	 * wire ids never match the sink-minted row ids. Echoes the page
	 * does not carry yet belong to turns still running; they stay.
	 * Error bubbles are live-only and survive. */
	function settleTurn(id: string, persistedPrompts: string[]) {
		const persisted = new Set(persistedPrompts)
		messages = messages.filter(
			(m) =>
				m.id !== id &&
				m.role !== 'tool' &&
				!(m.role === 'user' && persisted.has(m.content))
		)
	}
	return {
		get messages() {
			return messages
		},
		echoUser,
		applyEvent,
		settleTurn
	}
}
