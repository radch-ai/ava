import { redirect } from '@sveltejs/kit'
import { SUPERAGENT_AGENT_ID } from '#lib/config'
import { resolve } from '$app/paths'
import type { PageLoad } from './$types'

export const load: PageLoad = () => {
	// The superagent lives at /agent/[agentId] like every other agent; its
	// id is the reserved `_superagent` slug.
	redirect(
		307,
		resolve('/(app)/agent/[agentId]', { agentId: SUPERAGENT_AGENT_ID })
	)
}
