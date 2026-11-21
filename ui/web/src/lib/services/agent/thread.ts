import type { ThreadMessage } from '#lib/types/agent'
import { api } from '../api'

export type { ThreadMessage }

/**
 * One page of one thread, oldest→newest. A page is thread-scoped: the
 * cursor walks inside a thread and crosses into its predecessor when
 * the thread is exhausted, where the divider arrives.
 */
export interface AgentThread {
	items: ThreadMessage[]
	nextCursor: string | null
	threadId: string
}

/**
 * The display key of one row. Divider ids live in the thread-id
 * namespace, so a divider and a message can share a raw id —
 * namespacing both keeps them distinct rows.
 */
export function threadRowKey(message: { id: string; role: string }): string {
	return message.role === 'divider'
		? `divider:${message.id}`
		: `msg:${message.id}`
}

export async function getAgentThread(
	agentId: string,
	limit = 20,
	cursor?: string
): Promise<AgentThread> {
	const response = await api
		.get(`agent/${agentId}/thread`, {
			searchParams: { limit: String(limit), ...(cursor ? { cursor } : {}) }
		})
		.json<unknown>()
	return parseAgentThread(response)
}

/** Validates one page body — the display endpoint's or the synchronization
 * snapshot's, which carry the same shape. Unknown shapes are rejected rather
 * than partially rendered. */
export function parseAgentThread(thread: unknown): AgentThread {
	if (typeof thread !== 'object' || thread === null) {
		throw new Error('Invalid thread: expected an object')
	}
	const t = thread as Record<string, unknown>
	if (!Array.isArray(t.items)) {
		throw new Error('Invalid thread: items must be an array')
	}
	if (
		!('nextCursor' in t) ||
		(typeof t.nextCursor !== 'string' && t.nextCursor !== null)
	) {
		throw new Error('Invalid thread: nextCursor must be a string or null')
	}
	if (typeof t.threadId !== 'string') {
		throw new Error('Invalid thread: threadId must be a string')
	}
	for (const item of t.items) {
		if (typeof item !== 'object' || item === null) {
			throw new Error('Invalid message: malformed message structure')
		}
		const msg = item as Record<string, unknown>
		if (
			typeof msg.id !== 'string' ||
			typeof msg.content !== 'string' ||
			typeof msg.createdAt !== 'string' ||
			(msg.role !== 'user' &&
				msg.role !== 'assistant' &&
				msg.role !== 'tool' &&
				msg.role !== 'error' &&
				msg.role !== 'divider')
		) {
			throw new Error('Invalid message: malformed message structure')
		}
	}
	return {
		items: t.items as ThreadMessage[],
		nextCursor: t.nextCursor as string | null,
		threadId: t.threadId
	}
}
