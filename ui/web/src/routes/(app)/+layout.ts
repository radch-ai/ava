import { authStore } from '#lib/auth'
import { getAgents } from '#lib/services/agent'
import type { LayoutLoad } from './$types'

export const load: LayoutLoad = async ({ depends }) => {
	// The ky client bypasses the load event's fetch, so URL dependencies
	// cannot be tracked automatically — declare the invalidation id by hand.
	depends('app:agents')

	await authStore.initialize()
	if (!authStore.isAuthenticated) {
		// No session — the layout guard sends the user to login; skip the
		// agent list, which would just 401.
		return { agents: [], agentsError: null }
	}

	try {
		const agents = await getAgents()
		return { agents, agentsError: null }
	} catch (e) {
		return {
			agents: [],
			agentsError: e instanceof Error ? e.message : 'Failed to load agents'
		}
	}
}
