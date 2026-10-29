import { error } from '@sveltejs/kit'
import { SUPERAGENT_AGENT_ID } from '#lib/config'
import {
	type ActivityFeed,
	type AgentThread,
	getAgentActivityFeed,
	getAgentThread
} from '#lib/services/agent'
import { type Agent, SUPERAGENT_AGENT } from '#lib/types/agent'
import type { PageLoad } from './$types'

export const load: PageLoad = async ({ params, parent }) => {
	// The parent layout load fetches the agents list; parent() reuses that
	// data without a second fetch and stays fresh after invalidation.
	const { agents } = await parent()

	// The superagent is a client constant, so its page renders even when
	// the agents list failed to load; every other id must be in the list.
	const agent =
		params.agentId === SUPERAGENT_AGENT_ID
			? SUPERAGENT_AGENT
			: agents.find((a: Agent) => a.id === params.agentId)

	if (!agent) {
		error(404, `Agent '${params.agentId}' not found`)
	}

	let activityFeed: ActivityFeed = { items: [], nextCursor: null }
	let activityError: string | null = null
	let thread: AgentThread | null = null
	let threadError: string | null = null

	if (agent.id === SUPERAGENT_AGENT_ID) {
		// The conversation is the superagent's primary surface; its activity
		// feed loads lazily when the activity tab is first opened.
		try {
			thread = await getAgentThread(agent.id)
		} catch (e) {
			threadError =
				e instanceof Error ? e.message : 'Failed to load conversation'
		}
	} else {
		try {
			activityFeed = await getAgentActivityFeed(agent.id)
		} catch (e) {
			activityError = e instanceof Error ? e.message : 'Failed to load activity'
		}
	}

	return { agent, thread, threadError, activityFeed, activityError }
}

export const prerender = false
