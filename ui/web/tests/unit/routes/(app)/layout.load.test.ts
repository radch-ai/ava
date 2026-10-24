import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getAgents } from '$lib/services/user'
import type { Agent } from '$lib/types/agent'
import { setAgents } from '$lib/user'
import { load } from '../../../../src/routes/(app)/+layout'

const { mockAuthStore } = vi.hoisted(() => ({
	mockAuthStore: { isAuthenticated: true, initialize: vi.fn() }
}))

vi.mock('$lib/auth', () => ({
	authStore: mockAuthStore
}))

vi.mock('$lib/services/user', () => ({
	getAgents: vi.fn()
}))

vi.mock('$lib/user', () => ({
	setAgents: vi.fn()
}))

const mockGetAgents = vi.mocked(getAgents)
const mockSetAgents = vi.mocked(setAgents)

describe('app layout load', () => {
	beforeEach(() => {
		vi.clearAllMocks()
		mockAuthStore.isAuthenticated = true
	})

	it('fetches agents and sets them in shared state', async () => {
		const agents: Agent[] = [
			{ name: 'ava', icon: 'message-square', label: 'Ava' }
		]
		mockGetAgents.mockResolvedValueOnce(agents)

		const result = (await load({} as Parameters<typeof load>[0])) as {
			agentsError: string | null
		}

		expect(mockGetAgents).toHaveBeenCalled()
		expect(mockSetAgents).toHaveBeenCalledWith(agents)
		expect(result.agentsError).toBeNull()
	})

	it('sets empty agents and returns error when getAgents fails', async () => {
		mockGetAgents.mockRejectedValueOnce(new Error('Network error'))

		const result = (await load({} as Parameters<typeof load>[0])) as {
			agentsError: string | null
		}

		expect(mockSetAgents).toHaveBeenCalledWith([])
		expect(result.agentsError).toBe('Network error')
	})

	it('sets empty agents and returns error when getAgents times out', async () => {
		mockGetAgents.mockRejectedValueOnce(new Error('Request timeout'))

		const result = (await load({} as Parameters<typeof load>[0])) as {
			agentsError: string | null
		}

		expect(mockSetAgents).toHaveBeenCalledWith([])
		expect(result.agentsError).toBe('Request timeout')
	})

	it('sets empty agents and returns error when getAgents returns HTTP 500', async () => {
		mockGetAgents.mockRejectedValueOnce(new Error('Internal server error'))

		const result = (await load({} as Parameters<typeof load>[0])) as {
			agentsError: string | null
		}

		expect(mockSetAgents).toHaveBeenCalledWith([])
		expect(result.agentsError).toBe('Internal server error')
	})

	it('sets empty agents and returns error when getAgents returns malformed response', async () => {
		mockGetAgents.mockRejectedValueOnce(new SyntaxError('Unexpected token'))

		const result = (await load({} as Parameters<typeof load>[0])) as {
			agentsError: string | null
		}

		expect(mockSetAgents).toHaveBeenCalledWith([])
		expect(result.agentsError).toBe('Unexpected token')
	})

	it('returns fallback message for non-Error thrown values', async () => {
		mockGetAgents.mockRejectedValueOnce('some string')

		const result = (await load({} as Parameters<typeof load>[0])) as {
			agentsError: string | null
		}

		expect(mockSetAgents).toHaveBeenCalledWith([])
		expect(result.agentsError).toBe('Failed to load agents')
	})

	it('skips the agent fetch when there is no session', async () => {
		mockAuthStore.isAuthenticated = false

		const result = (await load({} as Parameters<typeof load>[0])) as {
			agentsError: string | null
		}

		expect(mockAuthStore.initialize).toHaveBeenCalledTimes(1)
		expect(mockGetAgents).not.toHaveBeenCalled()
		expect(mockSetAgents).not.toHaveBeenCalled()
		expect(result.agentsError).toBeNull()
	})
})
