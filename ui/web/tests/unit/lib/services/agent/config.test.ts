import { beforeEach, describe, expect, it, vi } from 'vitest'
import type {
	AgentConfig,
	AgentConfigWithIdentity,
	AgentIdentity,
	CronJob,
	HeartbeatConfig
} from '#lib/types/agent'

vi.mock('#lib/services/api', () => ({
	api: {
		get: vi.fn(),
		patch: vi.fn(),
		post: vi.fn()
	}
}))

import {
	type AgentConfigPatch,
	createAgent,
	getAgentConfig,
	updateAgentConfig,
	updateCronJobs,
	updateHeartbeat
} from '#lib/services/agent'
import { type ApiResponsePromise, api } from '#lib/services/api'

function createTestConfig(
	overrides?: Partial<AgentConfig & Pick<AgentIdentity, 'name' | 'icon'>>
): AgentConfig & Pick<AgentIdentity, 'name' | 'icon'> {
	return {
		name: 'Test Agent',
		icon: 'bot',
		soul: { personality: 'Test personality', identity: 'Test identity' },
		userContext: 'Test context',
		operatingInstructions: 'Test instructions',
		toolsContext: 'Test tools',
		toolsEnabled: [],
		heartbeat: defaultHeartbeat,
		cronJobs: [],
		...overrides
	}
}

const defaultHeartbeat: HeartbeatConfig = {
	enabled: false,
	duration: '1hr',
	content: '',
	model: 'default'
}

/** The detail body the API answers: AgentConfigWithIdentity —
 * identity and every config field flat at one level, entities
 * included. */
const toDetail = (
	config: AgentConfig & Pick<AgentIdentity, 'name' | 'icon'>,
	id = 'ava',
	heartbeat: HeartbeatConfig = defaultHeartbeat,
	cronJobs: CronJob[] = []
): AgentConfigWithIdentity => ({
	id,
	...config,
	heartbeat,
	cronJobs
})

describe('agent/config service', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	describe('getAgentConfig', () => {
		it('fetches one agent with its configuration', async () => {
			const mockConfig = createTestConfig()

			vi.mocked(api.get).mockReturnValue({
				json: async () => toDetail(mockConfig)
			} as ApiResponsePromise)

			const result = await getAgentConfig('ava')

			expect(api.get).toHaveBeenCalledWith('agent/ava')
			expect(result).toEqual(toDetail(mockConfig))
		})

		it('propagates errors from api', async () => {
			const error = new Error('Network error')

			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(getAgentConfig('unknown')).rejects.toThrow('Network error')
		})

		it('carries the separate-table entities beside config', async () => {
			const cronJobs: CronJob[] = [
				{
					id: 'cron-1',
					time: '09:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Morning digest'
				},
				{
					id: 'cron-2',
					time: '17:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Evening report'
				}
			]
			const detail = toDetail(
				createTestConfig({ toolsEnabled: ['web-search', 'code-execution'] }),
				'ava',
				{
					enabled: true,
					duration: '2hr',
					content: 'Daily heartbeat',
					model: 'gpt-4'
				},
				cronJobs
			)

			vi.mocked(api.get).mockReturnValue({
				json: async () => detail
			} as ApiResponsePromise)

			const result = await getAgentConfig('ava')

			expect(result.toolsEnabled).toEqual(['web-search', 'code-execution'])
			expect(result.heartbeat?.enabled).toBe(true)
			expect(result.cronJobs).toHaveLength(2)
		})

		it('fetches config for different agent names', async () => {
			const mockConfig = createTestConfig({ name: 'Test Bot' })

			vi.mocked(api.get).mockReturnValue({
				json: async () => toDetail(mockConfig, 'test-bot')
			} as ApiResponsePromise)

			await getAgentConfig('test-bot')

			expect(api.get).toHaveBeenCalledWith('agent/test-bot')
		})

		it('handles special characters in agent name', async () => {
			const mockConfig = createTestConfig()

			vi.mocked(api.get).mockReturnValue({
				json: async () => toDetail(mockConfig, 'agent-with-dashes')
			} as ApiResponsePromise)

			await getAgentConfig('agent-with-dashes')

			expect(api.get).toHaveBeenCalledWith('agent/agent-with-dashes')
		})
	})

	describe('updateAgentConfig', () => {
		it('updates one agent config with wrapped response', async () => {
			const detail = toDetail(createTestConfig({ name: 'Updated' }))
			const patch: AgentConfigPatch = { name: 'Updated' }

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: detail })
			} as ApiResponsePromise)

			const result = await updateAgentConfig('ava', patch)

			expect(api.patch).toHaveBeenCalledWith('agent/ava', { json: patch })
			expect(result).toEqual(detail)
		})

		it('updates one agent config with direct response', async () => {
			const detail = toDetail(createTestConfig())
			const patch: AgentConfigPatch = {
				operatingInstructions: 'Updated instructions'
			}

			vi.mocked(api.patch).mockReturnValue({
				json: async () => detail
			} as ApiResponsePromise)

			const result = await updateAgentConfig('ava', patch)

			expect(result).toEqual(detail)
		})

		it('handles name-only updates', async () => {
			const patch: AgentConfigPatch = { name: 'Updated Name' }
			const detail = toDetail(createTestConfig({ name: 'Updated Name' }))

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: detail })
			} as ApiResponsePromise)

			const result = await updateAgentConfig('ava', patch)

			expect(api.patch).toHaveBeenCalledWith('agent/ava', { json: patch })
			expect(result.name).toBe('Updated Name')
		})

		it('updates soul configuration', async () => {
			const soul = { personality: 'New personality', identity: 'New identity' }
			const patch: AgentConfigPatch = { soul }
			const detail = toDetail(createTestConfig({ soul }))

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: detail })
			} as ApiResponsePromise)

			const result = await updateAgentConfig('ava', patch)

			expect(result.soul.personality).toBe('New personality')
			expect(result.soul.identity).toBe('New identity')
		})

		it('updates tools configuration', async () => {
			const configUpdate = {
				toolsEnabled: ['web-search', 'code-execution', 'file-system'],
				toolsContext: 'Updated tools context'
			}
			const patch: AgentConfigPatch = configUpdate
			const detail = toDetail(createTestConfig(configUpdate))

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: detail })
			} as ApiResponsePromise)

			const result = await updateAgentConfig('ava', patch)

			expect(result.toolsEnabled).toEqual([
				'web-search',
				'code-execution',
				'file-system'
			])
			expect(result.toolsContext).toBe('Updated tools context')
		})

		it('propagates errors from patch', async () => {
			const error = new Error('Update failed')

			vi.mocked(api.patch).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(
				updateAgentConfig('ava', { name: 'New Name' })
			).rejects.toThrow('Update failed')
		})

		it('handles updates for different agents', async () => {
			const detail = toDetail(createTestConfig(), 'test-bot')

			vi.mocked(api.patch).mockReturnValue({
				json: async () => detail
			} as ApiResponsePromise)

			await updateAgentConfig('test-bot', {})

			expect(api.patch).toHaveBeenCalledWith('agent/test-bot', { json: {} })
		})
	})

	describe('updateHeartbeat', () => {
		it('saves the heartbeat row to its own endpoint', async () => {
			const heartbeat: HeartbeatConfig = {
				enabled: true,
				duration: '30m',
				content: 'Heartbeat content',
				model: 'gpt-4'
			}
			const detail = toDetail(createTestConfig(), 'ava', heartbeat)

			vi.mocked(api.patch).mockReturnValue({
				json: async () => detail
			} as ApiResponsePromise)

			const result = await updateHeartbeat('ava', heartbeat)

			expect(api.patch).toHaveBeenCalledWith('agent/ava/heartbeat', {
				json: heartbeat
			})
			expect(result.heartbeat?.enabled).toBe(true)
			expect(result.heartbeat?.duration).toBe('30m')
		})

		it('propagates errors from patch', async () => {
			const error = new Error('Update failed')

			vi.mocked(api.patch).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(updateHeartbeat('ava', defaultHeartbeat)).rejects.toThrow(
				'Update failed'
			)
		})
	})

	describe('updateCronJobs', () => {
		it('replaces the cron list on its own endpoint', async () => {
			const cronJobs: CronJob[] = [
				{
					id: 'cron-1',
					time: '09:00',
					frequency: 'weekly',
					model: 'gpt-4',
					prompt: 'Weekly digest'
				},
				{
					id: 'cron-2',
					time: '00:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Cleanup'
				}
			]
			const detail = toDetail(
				createTestConfig(),
				'ava',
				defaultHeartbeat,
				cronJobs
			)

			vi.mocked(api.patch).mockReturnValue({
				json: async () => detail
			} as ApiResponsePromise)

			const result = await updateCronJobs('ava', cronJobs)

			expect(api.patch).toHaveBeenCalledWith('agent/ava/cron-jobs', {
				json: { cronJobs }
			})
			expect(result.cronJobs).toHaveLength(2)
		})
	})
	describe('createAgent', () => {
		it('posts the flat body — identity and config fields at one level', async () => {
			const mockConfig = createTestConfig({ name: 'New Agent' })
			const response = { id: 'new-agent' }

			vi.mocked(api.post).mockReturnValue({
				json: async () => response
			} as ApiResponsePromise)

			const result = await createAgent(mockConfig)

			expect(api.post).toHaveBeenCalledWith('agent', {
				json: mockConfig
			})
			expect(result).toEqual(response)
		})

		it('returns the server-derived id for display names with spaces', async () => {
			const mockConfig = createTestConfig({ name: 'Research Team 2' })
			const response = { id: 'research-team-2' }

			vi.mocked(api.post).mockReturnValue({
				json: async () => response
			} as ApiResponsePromise)

			const result = await createAgent(mockConfig)

			expect(result.id).toBe('research-team-2')
		})

		it('propagates duplicate-name errors from the server', async () => {
			const mockConfig = createTestConfig({ name: 'Research' })
			const error = new Error('An agent named "Research" already exists')

			vi.mocked(api.post).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(createAgent(mockConfig)).rejects.toThrow(
				'An agent named "Research" already exists'
			)
		})

		it('propagates invalid-name errors from the server', async () => {
			const mockConfig = createTestConfig({ name: 'bad_name' })
			const error = new Error(
				'Invalid agent name: use letters, digits and spaces only'
			)

			vi.mocked(api.post).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(createAgent(mockConfig)).rejects.toThrow(
				'Invalid agent name: use letters, digits and spaces only'
			)
		})
	})
})
