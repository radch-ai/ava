import { beforeEach, describe, expect, it, vi } from 'vitest'
import type {
	AgentConfigurationWithIdentity,
	AgentIdentity,
	CronJob,
	HeartbeatConfig,
	SuperagentConfiguration
} from '#lib/types/agent'

vi.mock('#lib/services/api', () => ({
	api: {
		get: vi.fn(),
		patch: vi.fn(),
		post: vi.fn()
	}
}))

import {
	type AgentConfigurationPatch,
	createAgent,
	getAgentConfiguration,
	updateAgentConfiguration
} from '#lib/services/agent'
import { type ApiResponsePromise, api } from '#lib/services/api'

function createTestConfig(
	overrides?: Partial<
		SuperagentConfiguration & Pick<AgentIdentity, 'name' | 'icon'>
	>
): SuperagentConfiguration & Pick<AgentIdentity, 'name' | 'icon'> {
	const defaultHeartbeat: HeartbeatConfig = {
		enabled: false,
		duration: '1hr',
		content: '',
		model: 'default'
	}

	return {
		name: 'Test Agent',
		icon: 'bot',
		soul: { personality: 'Test personality', identity: 'Test identity' },
		userContext: 'Test context',
		operatingInstructions: 'Test instructions',
		toolsContext: 'Test tools',
		toolsEnabled: [],
		cronJobs: [],
		heartbeat: defaultHeartbeat,
		...overrides
	}
}

/** The detail body the API answers: an agent plus its configuration. */
const toDetail = (
	config: SuperagentConfiguration & Pick<AgentIdentity, 'name' | 'icon'>,
	id = 'ava'
): AgentConfigurationWithIdentity<SuperagentConfiguration> => ({
	id,
	name: config.name,
	icon: config.icon,
	config
})

describe('agent/config service', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	describe('getAgentConfiguration', () => {
		it('fetches one agent with its configuration', async () => {
			const mockConfig = createTestConfig()

			vi.mocked(api.get).mockReturnValue({
				json: async () => toDetail(mockConfig)
			} as ApiResponsePromise)

			const result = await getAgentConfiguration('ava')

			expect(api.get).toHaveBeenCalledWith('agent/ava')
			expect(result).toEqual(toDetail(mockConfig))
		})

		it('propagates errors from api', async () => {
			const error = new Error('Network error')

			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(getAgentConfiguration('unknown')).rejects.toThrow(
				'Network error'
			)
		})

		it('handles config with all optional fields', async () => {
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
			const fullConfig = createTestConfig({
				toolsEnabled: ['web-search', 'code-execution'],
				cronJobs,
				heartbeat: {
					enabled: true,
					duration: '2hr',
					content: 'Daily heartbeat',
					model: 'gpt-4'
				}
			})

			vi.mocked(api.get).mockReturnValue({
				json: async () => toDetail(fullConfig)
			} as ApiResponsePromise)

			const result = await getAgentConfiguration('ava')

			expect(result.config.toolsEnabled).toEqual([
				'web-search',
				'code-execution'
			])
			expect(result.config.cronJobs).toHaveLength(2)
			expect(result.config.heartbeat?.enabled).toBe(true)
		})

		it('handles minimal config with only required fields', async () => {
			const minimalConfig = createTestConfig({
				heartbeat: undefined
			})

			vi.mocked(api.get).mockReturnValue({
				json: async () => toDetail(minimalConfig)
			} as ApiResponsePromise)

			const result = await getAgentConfiguration('ava')

			expect(result.config.heartbeat).toBeUndefined()
		})

		it('fetches config for different agent names', async () => {
			const mockConfig = createTestConfig({ name: 'Test Bot' })

			vi.mocked(api.get).mockReturnValue({
				json: async () => toDetail(mockConfig, 'test-bot')
			} as ApiResponsePromise)

			await getAgentConfiguration('test-bot')

			expect(api.get).toHaveBeenCalledWith('agent/test-bot')
		})

		it('handles special characters in agent name', async () => {
			const mockConfig = createTestConfig()

			vi.mocked(api.get).mockReturnValue({
				json: async () => toDetail(mockConfig, 'agent-with-dashes')
			} as ApiResponsePromise)

			await getAgentConfiguration('agent-with-dashes')

			expect(api.get).toHaveBeenCalledWith('agent/agent-with-dashes')
		})
	})

	describe('updateAgentConfiguration', () => {
		it('updates one agent config with wrapped response', async () => {
			const detail = toDetail(createTestConfig({ name: 'Updated' }))
			const patch: AgentConfigurationPatch = { name: 'Updated' }

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: detail })
			} as ApiResponsePromise)

			const result = await updateAgentConfiguration('ava', patch)

			expect(api.patch).toHaveBeenCalledWith('agent/ava', { json: patch })
			expect(result).toEqual(detail)
		})

		it('updates one agent config with direct response', async () => {
			const detail = toDetail(createTestConfig())
			const patch: AgentConfigurationPatch = {
				config: { operatingInstructions: 'Updated instructions' }
			}

			vi.mocked(api.patch).mockReturnValue({
				json: async () => detail
			} as ApiResponsePromise)

			const result = await updateAgentConfiguration('ava', patch)

			expect(result).toEqual(detail)
		})

		it('handles name-only updates', async () => {
			const patch: AgentConfigurationPatch = { name: 'Updated Name' }
			const detail = toDetail(createTestConfig({ name: 'Updated Name' }))

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: detail })
			} as ApiResponsePromise)

			const result = await updateAgentConfiguration('ava', patch)

			expect(api.patch).toHaveBeenCalledWith('agent/ava', { json: patch })
			expect(result.name).toBe('Updated Name')
		})

		it('updates soul configuration', async () => {
			const soul = { personality: 'New personality', identity: 'New identity' }
			const patch: AgentConfigurationPatch = { config: { soul } }
			const detail = toDetail(createTestConfig({ soul }))

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: detail })
			} as ApiResponsePromise)

			const result = await updateAgentConfiguration('ava', patch)

			expect(result.config.soul.personality).toBe('New personality')
			expect(result.config.soul.identity).toBe('New identity')
		})

		it('updates heartbeat configuration', async () => {
			const heartbeat = {
				enabled: true,
				duration: '30m',
				content: 'Heartbeat content',
				model: 'gpt-4'
			} as const
			const patch: AgentConfigurationPatch = { config: { heartbeat } }
			const detail = toDetail(createTestConfig({ heartbeat }))

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: detail })
			} as ApiResponsePromise)

			const result = await updateAgentConfiguration('ava', patch)

			expect(result.config.heartbeat?.enabled).toBe(true)
			expect(result.config.heartbeat?.duration).toBe('30m')
		})

		it('updates tools configuration', async () => {
			const configUpdate = {
				toolsEnabled: ['web-search', 'code-execution', 'file-system'],
				toolsContext: 'Updated tools context'
			}
			const patch: AgentConfigurationPatch = { config: configUpdate }
			const detail = toDetail(createTestConfig(configUpdate))

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: detail })
			} as ApiResponsePromise)

			const result = await updateAgentConfiguration('ava', patch)

			expect(result.config.toolsEnabled).toEqual([
				'web-search',
				'code-execution',
				'file-system'
			])
			expect(result.config.toolsContext).toBe('Updated tools context')
		})

		it('updates cron jobs configuration', async () => {
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
			const patch: AgentConfigurationPatch = { config: { cronJobs } }
			const detail = toDetail(createTestConfig({ cronJobs }))

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: detail })
			} as ApiResponsePromise)

			const result = await updateAgentConfiguration('ava', patch)

			expect(result.config.cronJobs).toHaveLength(2)
		})

		it('propagates errors from patch', async () => {
			const error = new Error('Update failed')

			vi.mocked(api.patch).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(
				updateAgentConfiguration('ava', { name: 'New Name' })
			).rejects.toThrow('Update failed')
		})

		it('handles updates for different agents', async () => {
			const detail = toDetail(createTestConfig(), 'test-bot')

			vi.mocked(api.patch).mockReturnValue({
				json: async () => detail
			} as ApiResponsePromise)

			await updateAgentConfiguration('test-bot', {})

			expect(api.patch).toHaveBeenCalledWith('agent/test-bot', { json: {} })
		})
	})

	describe('createAgent', () => {
		it('posts name, icon and config as one body', async () => {
			const mockConfig = createTestConfig({ name: 'New Agent' })
			const response = { id: 'new-agent', name: 'New Agent' }

			vi.mocked(api.post).mockReturnValue({
				json: async () => response
			} as ApiResponsePromise)

			const result = await createAgent(mockConfig)

			const { name, icon, ...config } = mockConfig
			expect(api.post).toHaveBeenCalledWith('agent', {
				json: { name, icon, config }
			})
			expect(result).toEqual(response)
		})

		it('returns the server-derived id for display names with spaces', async () => {
			const mockConfig = createTestConfig({ name: 'Research Team 2' })
			const response = { id: 'research-team-2', name: 'Research Team 2' }

			vi.mocked(api.post).mockReturnValue({
				json: async () => response
			} as ApiResponsePromise)

			const result = await createAgent(mockConfig)

			expect(result.id).toBe('research-team-2')
			expect(result.name).toBe('Research Team 2')
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
