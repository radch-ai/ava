import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SUPERAGENT_AGENT_ID } from '#lib/config'
import type { Agent } from '#lib/types/agent'

const { mockAgents } = vi.hoisted(() => ({
	mockAgents: [] as Agent[]
}))

vi.mock('#lib/services/agent', () => ({
	getAgentActivityFeed: vi.fn(),
	getAgentThread: vi.fn()
}))

vi.mock('#lib/agents.svelte', () => ({
	agents: mockAgents
}))

import { getAgentActivityFeed, getAgentThread } from '#lib/services/agent'
import { load } from '../../../../../src/routes/(app)/agent/[agentId]/+page'

const mockGetAgentActivityFeed = vi.mocked(getAgentActivityFeed)
const mockGetAgentThread = vi.mocked(getAgentThread)

const agentsList: Agent[] = [
	{
		id: '_superagent',
		name: 'Ava',
		label: 'Chat with Ava',
		icon: 'message-square'
	},
	{ id: 'support', name: 'Support', label: 'Support Bot', icon: 'bot' }
]

describe('agent page load', () => {
	beforeEach(() => {
		vi.clearAllMocks()
		mockAgents.length = 0
		mockGetAgentActivityFeed.mockResolvedValue({ items: [], nextCursor: null })
		mockGetAgentThread.mockResolvedValue({ items: [], nextCursor: null })
	})

	it('returns worker agent data with its activity feed', async () => {
		mockAgents.push(...agentsList)

		const result = (await load({
			params: { agentId: 'support' },
			parent: vi.fn().mockResolvedValue({})
		} as unknown as Parameters<typeof load>[0])) as {
			agent: Agent
			thread: unknown
		}

		expect(result.agent.id).toBe('support')
		expect(result.thread).toBeNull()
		expect(mockGetAgentActivityFeed).toHaveBeenCalledWith('support')
	})

	it('loads the thread instead of activity for the superagent id', async () => {
		mockAgents.push(...agentsList)

		const result = (await load({
			params: { agentId: SUPERAGENT_AGENT_ID },
			parent: vi.fn().mockResolvedValue({})
		} as unknown as Parameters<typeof load>[0])) as {
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
		mockAgents.push(...agentsList)
		mockGetAgentThread.mockRejectedValueOnce(new Error('Thread unavailable'))

		const result = (await load({
			params: { agentId: SUPERAGENT_AGENT_ID },
			parent: vi.fn().mockResolvedValue({})
		} as unknown as Parameters<typeof load>[0])) as {
			thread: unknown
			threadError: string | null
		}

		expect(result.thread).toBeNull()
		expect(result.threadError).toBe('Thread unavailable')
	})

	it('falls back to the client superagent entry when the list failed to load', async () => {
		// Store empty: the agents API failed and the layout returned agentsError
		const result = (await load({
			params: { agentId: SUPERAGENT_AGENT_ID },
			parent: vi
				.fn()
				.mockResolvedValue({ agentsError: 'Failed to load agents' })
		} as unknown as Parameters<typeof load>[0])) as {
			agent: { id: string }
		}

		expect(result.agent.id).toBe(SUPERAGENT_AGENT_ID)
	})

	it('surfaces activity errors without failing navigation', async () => {
		mockAgents.push(...agentsList)
		mockGetAgentActivityFeed.mockRejectedValueOnce(
			new Error('Activity unavailable')
		)

		const result = (await load({
			params: { agentId: 'support' },
			parent: vi.fn().mockResolvedValue({})
		} as unknown as Parameters<typeof load>[0])) as {
			activityError: string | null
		}

		expect(result.activityError).toBe('Activity unavailable')
	})

	it('throws 404 when the agent id is missing from shared state', async () => {
		mockAgents.push(...agentsList)

		await expect(
			load({
				params: { agentId: 'ghost' },
				parent: vi.fn().mockResolvedValue({})
			} as unknown as Parameters<typeof load>[0])
		).rejects.toMatchObject({
			status: 404,
			body: { message: "Agent 'ghost' not found" }
		})
	})
})
