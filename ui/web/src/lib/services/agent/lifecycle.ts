/**
 * Lifecycle event validation: the durable conversation feed arrives as raw
 * JSON payloads, so the surface checks shape and kind here before reducing
 * them. Unknown kinds are dropped — a newer gateway must never crash an older
 * page — and a malformed event is dropped rather than partially applied.
 */

import type { ThreadItem } from '#lib/types/agent'
import type { LifecycleEvent } from '#lib/types/gen/apitypes.gen'

export type { LifecycleEvent }

// Kinds this surface reduces. The admission fact — item_accepted — mints
// no row and reduces to no case: the decoder drops it rather than pass
// it through.
const KNOWN_KINDS: Record<string, true> = {
	item_started: true,
	message_committed: true,
	tool_started: true,
	tool_finished: true,
	item_finished: true,
	thread_changed: true
}

const TERMINAL_OUTCOMES: Record<string, true> = {
	completed: true,
	failed: true,
	stopped: true,
	cancelled: true,
	interrupted: true
}

/** A validated event of a kind that keys its thread: the thread
 * identity is present and every committed row is renderable. */
interface ThreadLifecycleEvent
	extends Omit<LifecycleEvent, 'rows' | 'threadId'> {
	kind: 'item_started' | 'message_committed' | 'thread_changed'
	threadId: string
	rows?: ThreadItem[]
}

/** A validated terminal fact. A cancellation of a submission that never
 * started names no thread — its notice is display-only — while every
 * other outcome, and a cancellation that carries rows, keys the thread
 * its rows belong to. */
interface ItemFinishedEvent extends Omit<LifecycleEvent, 'rows' | 'threadId'> {
	kind: 'item_finished'
	outcome: 'completed' | 'failed' | 'stopped' | 'cancelled' | 'interrupted'
	threadId?: string
	rows?: ThreadItem[]
}

/** A validated tool fact: its invocation identity is present; a
 * finished call that carries committed rows also names the thread they
 * belong to. */
interface ToolLifecycleEvent
	extends Omit<LifecycleEvent, 'rows' | 'invocationId' | 'threadId'> {
	kind: 'tool_started' | 'tool_finished'
	invocationId: string
	threadId?: string
	rows?: ThreadItem[]
}

/** The decoder's output: downstream code reduces without re-checking
 * shape, kind, or identity. */
export type ValidatedLifecycleEvent =
	| ThreadLifecycleEvent
	| ItemFinishedEvent
	| ToolLifecycleEvent

/** Parses one lifecycle payload; undefined when the shape, kind, or
 * per-kind identity is not one this surface reduces. */
export function parseLifecycleEvent(
	raw: unknown
): ValidatedLifecycleEvent | undefined {
	if (typeof raw !== 'object' || raw === null) {
		return undefined
	}
	const event = raw as Partial<LifecycleEvent>
	if (
		typeof event.kind !== 'string' ||
		!KNOWN_KINDS[event.kind] ||
		typeof event.itemId !== 'string' ||
		event.itemId === '' ||
		typeof event.pos !== 'number'
	) {
		return undefined
	}
	// Per-kind identity: a tool fact without its invocation cannot be
	// placed anywhere the display reduces; a tool_finished that carries
	// committed rows names their thread (the backend always sends it);
	// a thread fact without its thread cannot be placed — except a
	// cancellation that committed no row, whose notice is display-only
	// and lands in the newest displayed thread.
	if (event.kind === 'tool_started' || event.kind === 'tool_finished') {
		if (typeof event.invocationId !== 'string' || event.invocationId === '') {
			return undefined
		}
		if (
			event.kind === 'tool_finished' &&
			(event.rows?.length ?? 0) > 0 &&
			(typeof event.threadId !== 'string' || event.threadId === '')
		) {
			return undefined
		}
	} else if (event.kind === 'item_finished') {
		const threadless =
			event.outcome === 'cancelled' && (event.rows?.length ?? 0) === 0
		if (
			!threadless &&
			(typeof event.threadId !== 'string' || event.threadId === '')
		) {
			return undefined
		}
	} else if (typeof event.threadId !== 'string' || event.threadId === '') {
		return undefined
	}
	if (event.rows !== undefined && !Array.isArray(event.rows)) {
		return undefined
	}
	// A row the display cannot render is an event the surface cannot
	// reduce: whole-event drop, the same policy as a malformed kind, so a
	// half-shaped row never reaches renderMarkdown.
	if (Array.isArray(event.rows)) {
		for (const row of event.rows) {
			if (
				typeof row !== 'object' ||
				row === null ||
				typeof (row as Partial<ThreadItem>).id !== 'string' ||
				typeof (row as Partial<ThreadItem>).content !== 'string' ||
				typeof (row as Partial<ThreadItem>).role !== 'string'
			) {
				return undefined
			}
		}
	}
	if (
		event.kind === 'item_finished' &&
		(typeof event.outcome !== 'string' || !TERMINAL_OUTCOMES[event.outcome])
	) {
		return undefined
	}
	return event as ValidatedLifecycleEvent
}
