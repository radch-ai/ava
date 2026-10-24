import type { AgentConfiguration } from '#lib/types/agent'
import { api } from '../api'

export interface CreateAgentResponse {
	name: string
}

const AGENT_PATH = 'user/agent'

export async function getAgentConfiguration(
	agentName: string
): Promise<AgentConfiguration> {
	return api.get(`${AGENT_PATH}/${agentName}`).json<AgentConfiguration>()
}

export async function updateAgentConfiguration(
	agentName: string,
	configuration: Partial<AgentConfiguration>
): Promise<AgentConfiguration> {
	const response = await api
		.patch(`${AGENT_PATH}/${agentName}`, { json: configuration })
		.json<{ data?: AgentConfiguration } | AgentConfiguration>()

	if (response && typeof response === 'object' && 'data' in response) {
		return response.data as AgentConfiguration
	}
	return response as AgentConfiguration
}

export async function createAgent(
	agentName: string,
	configuration: AgentConfiguration
): Promise<CreateAgentResponse> {
	return api
		.post(`${AGENT_PATH}/${agentName}`, { json: configuration })
		.json<CreateAgentResponse>()
}
