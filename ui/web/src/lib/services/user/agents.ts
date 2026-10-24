import type { Agent } from '$lib/types/agent'
import { api } from '../api'

export async function getAgents(): Promise<Agent[]> {
	const response = await api.get('user/agents').json<{ agents: Agent[] }>()
	return response.agents
}
