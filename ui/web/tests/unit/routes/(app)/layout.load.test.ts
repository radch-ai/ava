import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getAgents } from '#lib/services/agent'
import type { Agent } from '#lib/types/agent'
import { load } from '../../../../src/routes/(app)/+layout'

const { mockAuthStore } = vi.hoisted(() => ({
	mockAuthStore: { isAuthenticated: true, initialize: vi.fn() }
}))

vi.mock('#lib/auth', () => ({
	authStore: mockAuthStore
}))

vi.mock('#lib/services/agent', () => ({
	getAgents: vi.fn()
}))

const mockGetAgents = vi.mocked(getAgents)

type LayoutResult = { agents: Agent[]; agentsError: string | null }

function loadEvent(): Parameters<typeof load>[0] {
	return { depends: vi.fn() } as unknown as Parameters<typeof load>[0]
}

describe('app layout load', () => {
	beforeEach(() => {
		vi.clearAllMocks()
		mockAuthStore.isAuthenticated = true
	})

	it('declares the agents invalidation dependency', async () => {
		mockGetAgents.mockResolvedValueOnce([])
		const event = loadEvent()

		await load(event)

		expect(event.depends).toHaveBeenCalledWith('app:agents')
	})

	it('fetches agents and returns them as-is', async () => {
		const agents: Agent[] = [
			{
				id: '_superagent',
				name: 'Ava',
				icon: 'message-square'
			},
			{ id: 'support-bot', name: 'Support Bot', icon: 'bot' }
		]
		mockGetAgents.mockResolvedValueOnce(agents)

		const result = (await load(loadEvent())) as LayoutResult

		expect(mockGetAgents).toHaveBeenCalled()
		expect(result.agentsError).toBeNull()
		expect(result.agents).toEqual(agents)
	})

	it('returns empty agents and the error when getAgents fails', async () => {
		mockGetAgents.mockRejectedValueOnce(new Error('Network error'))

		const result = (await load(loadEvent())) as LayoutResult

		expect(result.agents).toEqual([])
		expect(result.agentsError).toBe('Network error')
	})

	it('returns a fallback message for non-Error thrown values', async () => {
		mockGetAgents.mockRejectedValueOnce('some string')

		const result = (await load(loadEvent())) as LayoutResult

		expect(result.agents).toEqual([])
		expect(result.agentsError).toBe('Failed to load agents')
	})

	it('skips the agent fetch and returns empty agents when there is no session', async () => {
		mockAuthStore.isAuthenticated = false

		const result = (await load(loadEvent())) as LayoutResult

		expect(mockAuthStore.initialize).toHaveBeenCalledTimes(1)
		expect(mockGetAgents).not.toHaveBeenCalled()
		expect(result.agents).toEqual([])
		expect(result.agentsError).toBeNull()
	})
})
