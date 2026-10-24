import { error } from '@sveltejs/kit'
import { type ActivityFeed, getAgentActivityFeed } from '$lib/services/agent'
import type { Agent } from '$lib/types/agent'
import { agents } from '$lib/user'
import type { PageLoad } from './$types'

export const load: PageLoad = async ({ params, parent }) => {
	// Wait for the parent layout load to finish populating shared agent state before
	// reading from it here. The detail page still uses the shared store as the source
	// of truth so newly created agents remain available without duplicating data.
	await parent()

	const agentListItem = agents.find((a: Agent) => a.name === params.agentName)

	if (!agentListItem) {
		throw error(404, `Agent '${params.agentName}' not found`)
	}

	const agent: Agent = {
		name: params.agentName,
		label: agentListItem.label,
		icon: agentListItem.icon
	}

	let activityFeed: ActivityFeed = { items: [], nextCursor: null }
	let activityError: string | null = null

	try {
		activityFeed = await getAgentActivityFeed(params.agentName)
	} catch (e) {
		activityError = e instanceof Error ? e.message : 'Failed to load activity'
	}

	return { agent, activityFeed, activityError }
}

export const prerender = false
