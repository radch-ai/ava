import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('#lib/services/api', () => ({
	api: {
		get: vi.fn()
	}
}))

import { getAgents } from '#lib/services/agent/agents'
import { type ApiResponsePromise, api } from '#lib/services/api'

describe('agents service', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	describe('getAgents', () => {
		it('fetches the agent list', async () => {
			const mockAgents = [
				{
					id: '_superagent',
					name: 'Ava',
					icon: 'message-square'
				},
				{ id: 'research', name: 'Research', icon: 'bot' }
			]

			vi.mocked(api.get).mockReturnValue({
				json: async () => ({ agents: mockAgents })
			} as ApiResponsePromise)

			const result = await getAgents()

			expect(api.get).toHaveBeenCalledWith('agents')
			expect(result).toEqual(mockAgents)
		})

		it('returns empty array when no agents', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => ({ agents: [] })
			} as ApiResponsePromise)

			const result = await getAgents()

			expect(result).toEqual([])
		})

		it('propagates errors from api', async () => {
			const error = new Error('Network error')

			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(getAgents()).rejects.toThrow('Network error')
		})

		it('preserves existing label when provided by API', async () => {
			const mockAgents = [
				{ name: 'custom-agent', label: 'Custom Agent', icon: 'bot' }
			]

			vi.mocked(api.get).mockReturnValue({
				json: async () => ({ agents: mockAgents })
			} as ApiResponsePromise)

			const result = await getAgents()

			expect(result).toEqual([
				{ name: 'custom-agent', label: 'Custom Agent', icon: 'bot' }
			])
		})

		it('handles large agent lists', async () => {
			const mockAgents = Array.from({ length: 50 }, (_, i) => ({
				name: `agent-${i}`,
				icon: 'bot',
				label: `Agent ${i}`
			}))

			vi.mocked(api.get).mockReturnValue({
				json: async () => ({ agents: mockAgents })
			} as ApiResponsePromise)

			const result = await getAgents()

			expect(result).toHaveLength(50)
			expect(result[0]).toEqual({
				name: 'agent-0',
				icon: 'bot',
				label: 'Agent 0'
			})
			expect(result[49]).toEqual({
				name: 'agent-49',
				icon: 'bot',
				label: 'Agent 49'
			})
		})

		it('handles timeout errors', async () => {
			const error = new Error('Request timeout')

			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(getAgents()).rejects.toThrow('Request timeout')
		})

		it('handles unauthorized errors', async () => {
			const error = new Error('Unauthorized')

			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(getAgents()).rejects.toThrow('Unauthorized')
		})

		it('handles malformed (non-JSON) response', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => {
					throw new SyntaxError('Unexpected token')
				}
			} as unknown as ApiResponsePromise)

			await expect(getAgents()).rejects.toThrow()
		})

		it('handles HTTP 500 error', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => {
					throw new Error('Internal server error')
				}
			} as unknown as ApiResponsePromise)

			await expect(getAgents()).rejects.toThrow('Internal server error')
		})
	})
})
