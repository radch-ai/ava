import type { Agent } from '#lib/types/agent'
import { api } from '../api'

/**
 * Fetch the user's agents.
 *
 * The server never includes the system superagent in this list — it either
 * filters the entry out before sending the response or excludes it in the
 * SQL query in the first place. Clients render the superagent from their
 * own constant (`SUPERAGENT_AGENT`), not from this list.
 */

export async function getAgents(): Promise<Agent[]> {
	const response = await api.get('agents').json<{ agents: Agent[] }>()
	return response.agents
}
