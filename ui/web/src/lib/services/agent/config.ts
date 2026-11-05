import type {
	AgentConfig,
	AgentConfigWithIdentity,
	AgentEditableFields,
	AgentIdentity,
	CreateAgentResponse,
	CronJob,
	HeartbeatConfig
} from '#lib/types/agent'
import { api } from '../api'

export async function getAgentConfig(
	agentId: string
): Promise<AgentConfigWithIdentity> {
	return api.get(`agent/${agentId}/config`).json<AgentConfigWithIdentity>()
}

export async function updateAgentConfig(
	agentId: string,
	patch: AgentEditableFields
): Promise<AgentConfigWithIdentity> {
	const response = await api
		.patch(`agent/${agentId}/config`, { json: patch })
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

/** Fetch the agent's cron jobs — only the Cron Jobs panel calls this. */
export async function getCronJobs(agentId: string): Promise<CronJob[]> {
	const { cronJobs } = await api
		.get(`agent/${agentId}/cron-jobs`)
		.json<{ cronJobs: CronJob[] }>()
	return cronJobs
}

/** Create one cron job; the server mints the id. */
export async function createCronJob(
	agentId: string,
	job: CronJob
): Promise<CronJob> {
	return api.post(`agent/${agentId}/cron-jobs`, { json: job }).json<CronJob>()
}

/** Save the full edited cron job by id. */
export async function updateCronJob(
	agentId: string,
	job: CronJob
): Promise<CronJob> {
	return api
		.patch(`agent/${agentId}/cron-jobs/${job.id}`, { json: job })
		.json<CronJob>()
}

/** Delete one cron job by id. */
export async function deleteCronJob(agentId: string, jobId: string) {
	await api.delete(`agent/${agentId}/cron-jobs/${jobId}`)
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
