/**
 * Live turn state: the conversation tail assembled from gateway frames while
 * turns run. Ephemeral by design — nothing here persists; a snapshot or a
 * settled turn hands the rows to the stored thread.
 *
 * Rows carry server identities: the user row a prompt's start committed
 * (user_message_id), the queue item id that streams the answer, and one
 * server-assigned id per tool invocation, so repeated provider call ids
 * cannot collide and a reconnecting device restores pending calls instead of
 * losing them.
 */

import type { ThreadItem } from '#lib/types/agent'
import type { ToolInvocationState } from '#lib/types/gen/wstypes.gen'
import type { AgentSocketEvent } from './ws'

/** The live tail's rows are display rows: one shape with the stored
 * thread's. */
type LiveMessage = ThreadItem

/** The runtime state a synchronization hand-off restores: the turn in
 * flight, its answer so far, and every invocation it recorded. */
interface LiveSnapshot {
	itemId: string
	/** The started turn's persisted user row, when the item is a prompt. */
	userRow?: { id: string; content: string }
	text: string
	invocations: ToolInvocationState[]
}

/** Creates the live-tail store for one conversation surface. */
export function createLiveTurns() {
	let messages = $state<LiveMessage[]>([])

	/** Adds one invocation's row while it runs; the server's invocation
	 * id is the row key. */
	function beginTool(inv: {
		invocationId: string
		turnId: string
		name: string
		args: string
	}) {
		if (messages.some((m) => m.id === inv.invocationId)) {
			return
		}
		messages.push({
			id: inv.invocationId,
			role: 'tool',
			turnId: inv.turnId,
			content: inv.name,
			name: inv.name,
			args: inv.args,
			pending: true
		})
	}

	/** Completes one invocation's row, keyed by its server identity. */
	function finishTool(
		invocationId: string,
		result: string,
		error?: string | null
	) {
		const existing = messages.find((m) => m.id === invocationId)
		if (!existing) {
			return
		}
		existing.result = result
		existing.error = error ?? null
		existing.pending = false
	}

	/** Retires one invocation's live row: its outcome committed, so the
	 * stored display carries it and the tail must not repeat it. */
	function retireTool(invocationId: string) {
		messages = messages.filter((m) => m.id !== invocationId)
	}

	/** Folds one transient gateway frame into the tail; durable facts
	 * arrive through the surface's lifecycle reducer instead. */
	function applyEvent(event: AgentSocketEvent) {
		switch (event.type) {
			case 'message_chunk': {
				// The gateway never streams an empty delta: one would
				// mint an empty assistant row, which the reasoning
				// marker's structural predicate would then remove as
				// its own stand-in.
				if (event.data.delta === '') return
				const existing = messages.find((m) => m.id === event.data.id)
				if (existing) {
					existing.content += event.data.delta
				} else {
					messages.push({
						id: event.data.id,
						role: 'assistant',
						content: event.data.delta
					})
				}
				break
			}
			case 'error':
				// A tool-attributed failure adds no bubble — the tool's own
				// row shows it. The failed turn's streamed text stands: the
				// terminal event replaces it with the row the backend
				// committed, so there is nothing to unpin here.
				if (!event.data.tool) {
					messages.push({
						id: `local-${crypto.randomUUID()}`,
						role: 'error',
						content: event.data.message
					})
				}
				break
			case 'lane_status': {
				// While the lane reports the model reasoning, a marker
				// stands in for the answer row that does not exist yet;
				// the lane always lowers the signal before the turn
				// ends, which removes it. Any empty assistant row is
				// the marker: the gateway never streams an empty
				// delta, and a restored prefix's empty answer row is
				// the stand-in rather than a duplicate.
				if (event.data.reasoning) {
					const standsIn = messages.some(
						(m) => m.role === 'assistant' && !m.content
					)
					if (!standsIn) {
						messages.push({
							id: `local-${crypto.randomUUID()}`,
							role: 'assistant',
							content: ''
						})
					}
				} else {
					messages = messages.filter(
						(m) => !(m.role === 'assistant' && !m.content)
					)
				}
				break
			}
		}
	}

	/** Replaces the tail with the synchronization hand-off's runtime state:
	 * an authoritative replacement, not an append. Stale live-only rows —
	 * a previous process's prefix, spinners, markers — are gone, and the
	 * restored invocations keep their pending state. */
	function replaceWith(snapshot: LiveSnapshot | undefined) {
		if (!snapshot) {
			// No turn runs: the tail holds nothing authoritative.
			messages = []
			return
		}
		const restored: LiveMessage[] = []
		if (snapshot.userRow) {
			restored.push({
				id: snapshot.userRow.id,
				role: 'user',
				content: snapshot.userRow.content
			})
		}
		for (const inv of snapshot.invocations) {
			restored.push({
				id: inv.invocation_id,
				role: 'tool',
				turnId: snapshot.itemId,
				content: inv.name,
				name: inv.name,
				args: inv.args,
				result: inv.result || undefined,
				error: inv.error ?? null,
				pending: inv.pending
			})
		}
		if (snapshot.text || snapshot.invocations.length === 0) {
			restored.push({
				id: snapshot.itemId,
				role: 'assistant',
				content: snapshot.text
			})
		}
		messages = restored
	}

	/** Settles a finished turn: its rows are committed now, so the live
	 * copies go — the streamed answer by item id, the user rows the event
	 * carried by their ids, and the error rows it committed. Rows of turns
	 * still running stay. */
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

	/** Takes the tail's rows for freezing at a thread boundary: returns
	 * them and clears the tail, dropping empty assistant markers (pure
	 * transient UI that must not be pinned into stored display). */
	function takeRows(): LiveMessage[] {
		const taken = messages.filter(
			(m) => !(m.role === 'assistant' && !m.content)
		)
		messages = []
		return taken
	}

	return {
		get messages() {
			return messages
		},
		beginTool,
		finishTool,
		retireTool,
		applyEvent,
		replaceWith,
		settleTurn,
		takeRows
	}
}
