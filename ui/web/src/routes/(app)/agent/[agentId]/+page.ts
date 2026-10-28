import { error } from '@sveltejs/kit'
import { SUPERAGENT_AGENT_ID } from '#lib/config'
import {
	type ActivityFeed,
	type AgentThread,
	getAgentActivityFeed,
	getAgentThread
} from '#lib/services/agent'
import { type Agent, SUPERAGENT_AGENT } from '#lib/types/agent'
import { agents } from '#lib/user'
import type { PageLoad } from './$types'

export const load: PageLoad = async ({ params, parent }) => {
	// Wait for the parent layout load to finish populating shared agent state
	// before reading from it here. The page still uses the shared store as the
	// source of truth so newly created agents remain available without
	// duplicating data.
	const { agentsError } = (await parent()) as { agentsError?: string | null }

	// The superagent is preconfigured and always exists; if the list itself
	// failed to load, fall back to the client constant so the layout's error
	// banner surfaces instead of a bogus 404.
	const agent =
		agents.find((a: Agent) => a.id === params.agentId) ??
		(params.agentId === SUPERAGENT_AGENT_ID && agentsError
			? SUPERAGENT_AGENT
			: undefined)

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
