import type { AgentConfiguration } from '#lib/types/agent'
import { api } from '../api'

export interface CreateAgentResponse {
	id: string
	name: string
}

export async function getAgentConfiguration(
	agentId: string
): Promise<AgentConfiguration> {
	return api.get(`agent/${agentId}`).json<AgentConfiguration>()
}

export async function updateAgentConfiguration(
	agentId: string,
	configuration: Partial<AgentConfiguration>
): Promise<AgentConfiguration> {
	const response = await api
		.patch(`agent/${agentId}`, { json: configuration })
		.json<{ data?: AgentConfiguration } | AgentConfiguration>()

	if (response && typeof response === 'object' && 'data' in response) {
		return response.data as AgentConfiguration
	}
	return response as AgentConfiguration
}

/**
 * Create an agent from a configuration. The server derives the agent id
 * from the display name (`deriveAgentId` rules) and rejects invalid or
 * duplicate names — the client never invents an id.
 */
export async function createAgent(
	configuration: AgentConfiguration
): Promise<CreateAgentResponse> {
	return api.post('agent', { json: configuration }).json<CreateAgentResponse>()
}
