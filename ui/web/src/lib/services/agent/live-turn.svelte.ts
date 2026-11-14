/**
 * Live turn state: the conversation tail assembled from gateway
 * events while a turn runs. Ephemeral by design — nothing here
 * persists; reloading the page shows only the stored thread.
 */

import type { AgentSocketEvent } from './ws'

export interface LiveMessage {
	id: string
	role: 'user' | 'assistant' | 'error'
	content: string
	/** True while chunks are still arriving for an assistant message. */
	pending?: boolean
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
		}
	}
	return {
		get messages() {
			return messages
		},
		echoUser,
		applyEvent
	}
}
