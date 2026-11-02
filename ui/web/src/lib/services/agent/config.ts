import type {
	AgentConfiguration,
	AgentConfigurationWithIdentity,
	AgentIdentity,
	SuperagentConfiguration
} from '#lib/types/agent'
import { api } from '../api'

export interface CreateAgentResponse {
	id: string
}

/** Patch body: identity fields and/or configuration fields */
export interface AgentConfigurationPatch {
	name?: string
	icon?: AgentIdentity['icon']
	config?: Partial<SuperagentConfiguration>
}

export async function getAgentConfiguration(
	agentId: string
): Promise<AgentConfigurationWithIdentity<SuperagentConfiguration>> {
	return api
		.get(`agent/${agentId}`)
		.json<AgentConfigurationWithIdentity<SuperagentConfiguration>>()
}

export async function updateAgentConfiguration(
	agentId: string,
	patch: AgentConfigurationPatch
): Promise<AgentConfigurationWithIdentity<SuperagentConfiguration>> {
	const response = await api
		.patch(`agent/${agentId}`, { json: patch })
		.json<
			| { data?: AgentConfigurationWithIdentity<SuperagentConfiguration> }
			| AgentConfigurationWithIdentity<SuperagentConfiguration>
		>()

	if (response && typeof response === 'object' && 'data' in response) {
		return response.data as AgentConfigurationWithIdentity<SuperagentConfiguration>
	}
	return response as AgentConfigurationWithIdentity<SuperagentConfiguration>
}

/**
 * Create an agent from a configuration. The server derives the agent id
 * from the display name (`deriveAgentId` rules) and rejects invalid or
 * duplicate names — the client never invents an id.
 */
export async function createAgent(
	configuration: AgentConfiguration & Pick<AgentIdentity, 'name' | 'icon'>
): Promise<CreateAgentResponse> {
	const { name, icon, ...config } = configuration
	return api
		.post('agent', { json: { name, icon, config } })
		.json<CreateAgentResponse>()
}
