import { api } from '../api'
import type { CursorPage } from '../utils/cursor'
import { mergePastPages } from '../utils/cursor'

export interface AgentActivityItem {
	id: string
	createdAt: string
	triggeredBy: string
	action: string
	status: 'running' | 'completed' | 'failed'
	message?: string
}

export type ActivityFeed = CursorPage<AgentActivityItem>

export const mergePastFeed = mergePastPages<AgentActivityItem>

export async function getAgentActivityFeed(
	agentId: string,
	limit = 20,
	cursor?: string
): Promise<ActivityFeed> {
	const params = new URLSearchParams({ limit: String(limit) })
	if (cursor) {
		params.set('cursor', cursor)
	}
	const response = await api
		.get(`user/agent/${agentId}/activity?${params.toString()}`)
		.json<unknown>()
	return validateActivityFeed(response)
}

function validateActivityFeed(feed: unknown): ActivityFeed {
	if (typeof feed !== 'object' || feed === null) {
		throw new Error('Invalid activity feed: expected an object')
	}
	const f = feed as Record<string, unknown>
	if (!Array.isArray(f.items)) {
		throw new Error('Invalid activity feed: items must be an array')
	}
	if (
		!('nextCursor' in f) ||
		(typeof f.nextCursor !== 'string' && f.nextCursor !== null)
	) {
		throw new Error(
			'Invalid activity feed: nextCursor must be a string or null'
		)
	}
	for (const item of f.items) {
		if (typeof item !== 'object' || item === null) {
			throw new Error('Invalid activity item: malformed activity structure')
		}
		const act = item as Record<string, unknown>
		if (
			typeof act.id !== 'string' ||
			typeof act.createdAt !== 'string' ||
			typeof act.triggeredBy !== 'string' ||
			typeof act.action !== 'string' ||
			(act.status !== 'running' &&
				act.status !== 'completed' &&
				act.status !== 'failed') ||
			(act.message !== undefined && typeof act.message !== 'string')
		) {
			throw new Error('Invalid activity item: malformed activity structure')
		}
	}
	return {
		items: f.items as AgentActivityItem[],
		nextCursor: f.nextCursor as string | null
	}
}
