import { api } from '../api'
import type { CursorPage } from '../utils/cursor'
import { mergePastPages } from '../utils/cursor'

export interface ThreadMessage {
	id: string
	role: 'user' | 'assistant'
	content: string
	createdAt: string
}

export type AgentThread = CursorPage<ThreadMessage>

export const mergePastThread = mergePastPages<ThreadMessage>

export async function getAgentThread(
	agentId: string,
	limit = 20,
	cursor?: string
): Promise<AgentThread> {
	const params = new URLSearchParams({ limit: String(limit) })
	if (cursor) {
		params.set('cursor', cursor)
	}
	const response = await api
		.get(`agent/${agentId}/thread?${params.toString()}`)
		.json<unknown>()
	return validateThread(response)
}

function validateThread(thread: unknown): AgentThread {
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
	for (const item of t.items) {
		if (typeof item !== 'object' || item === null) {
			throw new Error('Invalid message: malformed message structure')
		}
		const msg = item as Record<string, unknown>
		if (
			typeof msg.id !== 'string' ||
			typeof msg.content !== 'string' ||
			typeof msg.createdAt !== 'string' ||
			(msg.role !== 'user' && msg.role !== 'assistant')
		) {
			throw new Error('Invalid message: malformed message structure')
		}
	}
	return {
		items: t.items as ThreadMessage[],
		nextCursor: t.nextCursor as string | null
	}
}
