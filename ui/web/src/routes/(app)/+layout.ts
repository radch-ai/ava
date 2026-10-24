import { authStore } from '$lib/auth'
import { getAgents } from '$lib/services/user'
import { setAgents } from '$lib/user'
import type { LayoutLoad } from './$types'

export const load: LayoutLoad = async () => {
	await authStore.initialize()
	if (!authStore.isAuthenticated) {
		// No session — the layout guard sends the user to login; skip the
		// agent list, which would just 401.
		return { agentsError: null }
	}

	let agentsError: string | null = null

	try {
		const agents = await getAgents()
		setAgents(agents)
	} catch (e) {
		setAgents([])
		agentsError = e instanceof Error ? e.message : 'Failed to load agents'
	}

	return { agentsError }
}
