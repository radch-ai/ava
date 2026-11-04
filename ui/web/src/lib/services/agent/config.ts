import type {
	AgentConfig,
	AgentConfigWithIdentity,
	AgentIdentity,
	CronJob,
	HeartbeatConfig
} from '#lib/types/agent'
import { api } from '../api'

export interface CreateAgentResponse {
	id: string
}

/** Identity and config text fields; heartbeat/cronJobs save via
 * their own endpoints. */
export type AgentConfigPatch = Partial<
	Pick<AgentIdentity, 'name' | 'icon'> &
		Omit<AgentConfig, 'heartbeat' | 'cronJobs'>
>

export async function getAgentConfig(
	agentId: string
): Promise<AgentConfigWithIdentity> {
	return api.get(`agent/${agentId}`).json<AgentConfigWithIdentity>()
}

export async function updateAgentConfig(
	agentId: string,
	patch: AgentConfigPatch
): Promise<AgentConfigWithIdentity> {
	const response = await api
		.patch(`agent/${agentId}`, { json: patch })
		.json<{ data?: AgentConfigWithIdentity } | AgentConfigWithIdentity>()

	if (response && typeof response === 'object' && 'data' in response) {
		return response.data as AgentConfigWithIdentity
	}
	return response as AgentConfigWithIdentity
}

/** Save the agent's single heartbeat row. */
export async function updateHeartbeat(
	agentId: string,
	heartbeat: HeartbeatConfig
): Promise<AgentConfigWithIdentity> {
	return api
		.patch(`agent/${agentId}/heartbeat`, { json: heartbeat })
		.json<AgentConfigWithIdentity>()
}

/** Save the cron job list; replaced wholesale. */
export async function updateCronJobs(
	agentId: string,
	cronJobs: CronJob[]
): Promise<AgentConfigWithIdentity> {
	return api
		.patch(`agent/${agentId}/cron-jobs`, { json: { cronJobs } })
		.json<AgentConfigWithIdentity>()
}

/**
 * Create an agent. The server derives the id from the display name
 * (`deriveAgentId`) and rejects invalid or duplicate names; fields
 * that aren't columns are ignored.
 */
export async function createAgent(
	configuration: AgentConfig & Pick<AgentIdentity, 'name' | 'icon'>
): Promise<CreateAgentResponse> {
	return api.post('agent', { json: configuration }).json<CreateAgentResponse>()
}
