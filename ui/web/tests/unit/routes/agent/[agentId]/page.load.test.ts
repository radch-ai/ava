import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SUPERAGENT_AGENT_ID } from '#lib/config'
import type { Agent } from '#lib/types/agent'

vi.mock('#lib/services/agent', () => ({
	getAgentActivityFeed: vi.fn(),
	getAgentThread: vi.fn()
}))

import { getAgentActivityFeed, getAgentThread } from '#lib/services/agent'
import { load } from '../../../../../src/routes/(app)/agent/[agentId]/+page'

const mockGetAgentActivityFeed = vi.mocked(getAgentActivityFeed)
const mockGetAgentThread = vi.mocked(getAgentThread)

const agentsList: Agent[] = [{ id: 'support', name: 'Support', icon: 'bot' }]

type ParentData = { agents: Agent[]; agentsError: string | null }

function loadEvent(
	params: { agentId: string },
	parentData: ParentData = { agents: agentsList, agentsError: null }
): Parameters<typeof load>[0] {
	return {
		params,
		parent: vi.fn().mockResolvedValue(parentData)
	} as unknown as Parameters<typeof load>[0]
}

describe('agent page load', () => {
	beforeEach(() => {
		vi.clearAllMocks()
		mockGetAgentActivityFeed.mockResolvedValue({ items: [], nextCursor: null })
		mockGetAgentThread.mockResolvedValue({ items: [], nextCursor: null })
	})

	it('returns agent data with its activity feed', async () => {
		const result = (await load(loadEvent({ agentId: 'support' }))) as {
			agent: Agent
			thread: unknown
		}

		expect(result.agent.id).toBe('support')
		expect(result.thread).toBeNull()
		expect(mockGetAgentActivityFeed).toHaveBeenCalledWith('support')
	})

	it('loads the thread instead of activity for the superagent id', async () => {
		const result = (await load(
			loadEvent({ agentId: SUPERAGENT_AGENT_ID })
		)) as {
			agent: Agent
			thread: unknown
			threadError: string | null
			activityError: string | null
		}

		expect(result.agent.id).toBe(SUPERAGENT_AGENT_ID)
		expect(result.thread).toEqual({ items: [], nextCursor: null })
		expect(result.threadError).toBeNull()
		expect(mockGetAgentThread).toHaveBeenCalledWith(SUPERAGENT_AGENT_ID)
		expect(mockGetAgentActivityFeed).not.toHaveBeenCalled()
	})

	it('surfaces thread errors without failing navigation', async () => {
		mockGetAgentThread.mockRejectedValueOnce(new Error('Thread unavailable'))

		const result = (await load(
			loadEvent({ agentId: SUPERAGENT_AGENT_ID })
		)) as {
			thread: unknown
			threadError: string | null
		}

		expect(result.thread).toBeNull()
		expect(result.threadError).toBe('Thread unavailable')
	})

	it('resolves the superagent entry from the client constant when the list failed to load', async () => {
		// The agents API failed and the layout returned an empty list + error
		const result = (await load(
			loadEvent(
				{ agentId: SUPERAGENT_AGENT_ID },
				{ agents: [], agentsError: 'Failed to load agents' }
			)
		)) as {
			agent: { id: string }
		}

		expect(result.agent.id).toBe(SUPERAGENT_AGENT_ID)
	})

	it('resolves the superagent entry from the client constant without the list', async () => {
		// The list never carries the superagent; the id resolves from the
		// client constant regardless of the fetch outcome.
		const result = (await load(
			loadEvent(
				{ agentId: SUPERAGENT_AGENT_ID },
				{ agents: [], agentsError: null }
			)
		)) as {
			agent: { id: string }
		}

		expect(result.agent.id).toBe(SUPERAGENT_AGENT_ID)
		expect(mockGetAgentThread).toHaveBeenCalledWith(SUPERAGENT_AGENT_ID)
	})

	it('surfaces activity errors without failing navigation', async () => {
		mockGetAgentActivityFeed.mockRejectedValueOnce(
			new Error('Activity unavailable')
		)

		const result = (await load(loadEvent({ agentId: 'support' }))) as {
			activityError: string | null
		}

		expect(result.activityError).toBe('Activity unavailable')
	})

	it('throws 404 when the agent id is missing from the list', async () => {
		await expect(load(loadEvent({ agentId: 'ghost' }))).rejects.toMatchObject({
			status: 404,
			body: { message: "Agent 'ghost' not found" }
		})
	})
})
