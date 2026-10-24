import { api } from '../api';
import type { Agent } from '$lib/types/agent';

export async function getAgents(): Promise<Agent[]> {
	const response = await api.get('user/agents').json<{ agents: Agent[] }>();
	return response.agents;
}
