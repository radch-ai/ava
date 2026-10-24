import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { SuperagentConfiguration, HeartbeatConfig, CronJob } from '$lib/types/agent';

vi.mock('$lib/services/api', () => ({
	api: {
		get: vi.fn(),
		patch: vi.fn(),
		post: vi.fn()
	}
}));

import { getAgentConfiguration, updateAgentConfiguration, createAgent } from '$lib/services/agent';
import { api, type ApiResponsePromise } from '$lib/services/api';

function createTestConfig(overrides?: Partial<SuperagentConfiguration>): SuperagentConfiguration {
	const defaultHeartbeat: HeartbeatConfig = {
		enabled: false,
		duration: '1hr',
		content: '',
		model: 'default'
	};

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
	};
}

describe('agent/config service', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe('getAgentConfiguration', () => {
		it('fetches one agent config', async () => {
			const mockConfig = createTestConfig();

			vi.mocked(api.get).mockReturnValue({
				json: async () => mockConfig
			} as ApiResponsePromise);

			const result = await getAgentConfiguration('ava');

			expect(api.get).toHaveBeenCalledWith('user/agent/ava');
			expect(result).toEqual(mockConfig);
		});

		it('propagates errors from api', async () => {
			const error = new Error('Network error');

			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise);

			await expect(getAgentConfiguration('unknown')).rejects.toThrow('Network error');
		});

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
			];
			const fullConfig = createTestConfig({
				toolsEnabled: ['web-search', 'code-execution'],
				cronJobs,
				heartbeat: {
					enabled: true,
					duration: '2hr',
					content: 'Daily heartbeat',
					model: 'gpt-4'
				}
			});

			vi.mocked(api.get).mockReturnValue({
				json: async () => fullConfig
			} as ApiResponsePromise);

			const result = (await getAgentConfiguration('ava')) as SuperagentConfiguration;

			expect(result.toolsEnabled).toEqual(['web-search', 'code-execution']);
			expect(result.cronJobs).toHaveLength(2);
			expect(result.heartbeat?.enabled).toBe(true);
		});

		it('handles minimal config with only required fields', async () => {
			const minimalConfig = createTestConfig({
				heartbeat: undefined
			});

			vi.mocked(api.get).mockReturnValue({
				json: async () => minimalConfig
			} as ApiResponsePromise);

			const result = await getAgentConfiguration('ava');

			expect(result.heartbeat).toBeUndefined();
		});

		it('fetches config for different agent names', async () => {
			const mockConfig = createTestConfig({ name: 'Worker Bot' });

			vi.mocked(api.get).mockReturnValue({
				json: async () => mockConfig
			} as ApiResponsePromise);

			await getAgentConfiguration('worker-bot');

			expect(api.get).toHaveBeenCalledWith('user/agent/worker-bot');
		});

		it('handles special characters in agent name', async () => {
			const mockConfig = createTestConfig();

			vi.mocked(api.get).mockReturnValue({
				json: async () => mockConfig
			} as ApiResponsePromise);

			await getAgentConfiguration('agent-with-dashes');

			expect(api.get).toHaveBeenCalledWith('user/agent/agent-with-dashes');
		});
	});

	describe('updateAgentConfiguration', () => {
		it('updates one agent config with wrapped response', async () => {
			const mockConfig = createTestConfig({
				name: 'Updated',
				soul: { personality: 'P', identity: 'I' }
			});

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: mockConfig })
			} as ApiResponsePromise);

			const result = await updateAgentConfiguration('ava', mockConfig);

			expect(api.patch).toHaveBeenCalledWith('user/agent/ava', { json: mockConfig });
			expect(result).toEqual(mockConfig);
		});

		it('updates one agent config with direct response', async () => {
			const mockConfig = createTestConfig();

			vi.mocked(api.patch).mockReturnValue({
				json: async () => mockConfig
			} as ApiResponsePromise);

			const result = await updateAgentConfiguration('ava', mockConfig);

			expect(result).toEqual(mockConfig);
		});

		it('handles partial updates', async () => {
			const partialUpdate = { name: 'Updated Name' };
			const mockConfig = createTestConfig({ name: 'Updated Name' });

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: mockConfig })
			} as ApiResponsePromise);

			const result = await updateAgentConfiguration('ava', partialUpdate);

			expect(api.patch).toHaveBeenCalledWith('user/agent/ava', { json: partialUpdate });
			expect(result).toEqual(mockConfig);
		});

		it('updates soul configuration', async () => {
			const soulUpdate = {
				soul: { personality: 'New personality', identity: 'New identity' }
			};
			const mockConfig = createTestConfig(soulUpdate);

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: mockConfig })
			} as ApiResponsePromise);

			const result = await updateAgentConfiguration('ava', soulUpdate);

			expect(result.soul.personality).toBe('New personality');
			expect(result.soul.identity).toBe('New identity');
		});

		it('updates heartbeat configuration', async () => {
			const heartbeatUpdate = {
				heartbeat: {
					enabled: true,
					duration: '30m' as const,
					content: 'Heartbeat content',
					model: 'gpt-4'
				}
			};
			const mockConfig = createTestConfig(heartbeatUpdate);

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: mockConfig })
			} as ApiResponsePromise);

			const result = await updateAgentConfiguration('ava', heartbeatUpdate);

			expect(result.heartbeat?.enabled).toBe(true);
			expect(result.heartbeat?.duration).toBe('30m');
		});

		it('updates tools configuration', async () => {
			const toolsUpdate = {
				toolsEnabled: ['web-search', 'code-execution', 'file-system'],
				toolsContext: 'Updated tools context'
			};
			const mockConfig = createTestConfig(toolsUpdate);

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: mockConfig })
			} as ApiResponsePromise);

			const result = await updateAgentConfiguration('ava', toolsUpdate);

			expect(result.toolsEnabled).toEqual(['web-search', 'code-execution', 'file-system']);
			expect(result.toolsContext).toBe('Updated tools context');
		});

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
			];
			const cronUpdate = { cronJobs };
			const mockConfig = createTestConfig(cronUpdate);

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: mockConfig })
			} as ApiResponsePromise);

			const result = (await updateAgentConfiguration('ava', cronUpdate)) as SuperagentConfiguration;

			expect(result.cronJobs).toHaveLength(2);
		});

		it('propagates errors from patch', async () => {
			const error = new Error('Update failed');
			const configUpdate = { name: 'New Name' };

			vi.mocked(api.patch).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise);

			await expect(updateAgentConfiguration('ava', configUpdate)).rejects.toThrow('Update failed');
		});

		it('handles updates for different agents', async () => {
			const mockConfig = createTestConfig();

			vi.mocked(api.patch).mockReturnValue({
				json: async () => mockConfig
			} as ApiResponsePromise);

			await updateAgentConfiguration('worker-bot', mockConfig);

			expect(api.patch).toHaveBeenCalledWith('user/agent/worker-bot', { json: mockConfig });
		});

		it('handles empty update object', async () => {
			const emptyUpdate = {};
			const mockConfig = createTestConfig();

			vi.mocked(api.patch).mockReturnValue({
				json: async () => ({ data: mockConfig })
			} as ApiResponsePromise);

			const result = await updateAgentConfiguration('ava', emptyUpdate);

			expect(api.patch).toHaveBeenCalledWith('user/agent/ava', { json: {} });
			expect(result).toEqual(mockConfig);
		});
	});

	describe('createAgent', () => {
		it('creates one agent config', async () => {
			const mockConfig = createTestConfig({ name: 'New Agent' });
			const response = { name: 'new-agent-2' };

			vi.mocked(api.post).mockReturnValue({
				json: async () => response
			} as ApiResponsePromise);

			const result = await createAgent('new-agent', mockConfig);

			expect(api.post).toHaveBeenCalledWith('user/agent/new-agent', { json: mockConfig });
			expect(result).toEqual(response);
		});

		it('propagates errors from create responses', async () => {
			const mockConfig = createTestConfig({ name: 'New Agent' });
			const error = new Error('Create failed');

			vi.mocked(api.post).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise);

			await expect(createAgent('new-agent', mockConfig)).rejects.toThrow('Create failed');
		});

		it('creates agent with full configuration', async () => {
			const cronJobs: CronJob[] = [
				{
					id: 'cron-1',
					time: '09:00',
					frequency: 'daily',
					model: 'gpt-4',
					prompt: 'Daily digest'
				}
			];
			const fullConfig = createTestConfig({
				name: 'Full Agent',
				icon: 'cpu',
				userContext: 'Custom context',
				operatingInstructions: 'Custom instructions',
				toolsEnabled: ['web-search'],
				cronJobs,
				heartbeat: {
					enabled: true,
					duration: '1hr',
					content: 'Heartbeat',
					model: 'gpt-4'
				}
			});
			const response = { name: 'full-agent' };

			vi.mocked(api.post).mockReturnValue({
				json: async () => response
			} as ApiResponsePromise);

			const result = await createAgent('full-agent', fullConfig);

			expect(api.post).toHaveBeenCalledWith('user/agent/full-agent', { json: fullConfig });
			expect(result.name).toBe('full-agent');
		});

		it('creates agent with minimal configuration', async () => {
			const minimalConfig = createTestConfig({
				name: 'Minimal Agent'
			});
			const response = { name: 'minimal-agent' };

			vi.mocked(api.post).mockReturnValue({
				json: async () => response
			} as ApiResponsePromise);

			const result = await createAgent('minimal-agent', minimalConfig);

			expect(result.name).toBe('minimal-agent');
		});

		it('handles special characters in agent names', async () => {
			const mockConfig = createTestConfig();
			const response = { name: 'agent-with-dashes-123' };

			vi.mocked(api.post).mockReturnValue({
				json: async () => response
			} as ApiResponsePromise);

			const result = await createAgent('agent-with-dashes-123', mockConfig);

			expect(api.post).toHaveBeenCalledWith('user/agent/agent-with-dashes-123', {
				json: mockConfig
			});
			expect(result.name).toBe('agent-with-dashes-123');
		});

		it('handles different icon types', async () => {
			for (const icon of ['bot', 'cpu', 'database', 'globe', 'zap'] as const) {
				const config = createTestConfig({ icon });
				const response = { name: `agent-${icon}` };

				vi.mocked(api.post).mockReturnValue({
					json: async () => response
				} as ApiResponsePromise);

				await createAgent(`agent-${icon}`, config);

				expect(api.post).toHaveBeenCalledWith(`user/agent/agent-${icon}`, {
					json: config
				});
			}
		});

		it('handles network timeout errors', async () => {
			const mockConfig = createTestConfig();
			const error = new Error('Request timeout');

			vi.mocked(api.post).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise);

			await expect(createAgent('test-agent', mockConfig)).rejects.toThrow('Request timeout');
		});

		it('handles validation errors from backend', async () => {
			const mockConfig = createTestConfig();
			const error = new Error('Invalid configuration: name already exists');

			vi.mocked(api.post).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise);

			await expect(createAgent('existing-agent', mockConfig)).rejects.toThrow(
				'Invalid configuration: name already exists'
			);
		});
	});
});
