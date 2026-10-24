import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('$lib/services/api', () => ({
	api: {
		get: vi.fn()
	}
}));

import { getAgentActivityFeed, mergePastFeed, type ActivityFeed } from '$lib/services/agent';
import type { AgentActivityItem } from '$lib/services/agent/activity';
import { api, type ApiResponsePromise } from '$lib/services/api';

function createActivityItem(overrides?: Partial<AgentActivityItem>): AgentActivityItem {
	return {
		id: 'act-1',
		createdAt: '2024-01-01T00:00:00Z',
		triggeredBy: 'user',
		action: 'run',
		status: 'completed',
		...overrides
	};
}

function createActivityFeed(overrides?: Partial<ActivityFeed>): ActivityFeed {
	return {
		items: [createActivityItem()],
		nextCursor: null,
		...overrides
	};
}

describe('agent/activity service', () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe('getAgentActivityFeed', () => {
		it('accepts camelCase activity payloads from backend', async () => {
			const backendPage = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user',
						action: 'run',
						status: 'completed',
						message: 'Finished',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => backendPage
			} as ApiResponsePromise);

			const result = await getAgentActivityFeed('ava');

			expect(api.get).toHaveBeenCalledWith('user/agent/ava/activity?limit=20');
			expect(result).toEqual(
				createActivityFeed({
					items: [
						createActivityItem({
							status: 'completed',
							message: 'Finished'
						})
					]
				})
			);
		});

		it('passes cursor and limit through to the activity endpoint', async () => {
			const backendPage = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'heartbeat',
						action: 'digest',
						status: 'running',
						message: 'Still running',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: 'next-cursor-xyz'
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => backendPage
			} as ApiResponsePromise);

			const result = await getAgentActivityFeed('ava', 50, 'cursor-abc');

			expect(api.get).toHaveBeenCalledWith('user/agent/ava/activity?limit=50&cursor=cursor-abc');
			expect(result.nextCursor).toBe('next-cursor-xyz');
			expect(result.items[0]?.status).toBe('running');
		});

		it('throws when the activity request fails', async () => {
			const error = new Error('Network error');

			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow('Network error');
		});

		it('throws when any activity item is malformed', async () => {
			const malformedPage = {
				items: [{ id: 'act-1', action: 123 }],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when activity status is missing', async () => {
			const malformedPage = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user',
						action: 'run',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when activity status is outside the supported contract values', async () => {
			const malformedPage = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user',
						action: 'run',
						status: 'success',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when nextCursor is missing from the activity payload', async () => {
			const malformedPage = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user',
						action: 'run',
						status: 'completed',
						createdAt: '2024-01-01T00:00:00Z'
					}
				]
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when nextCursor is not a string or null in the activity payload', async () => {
			const malformedPage = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user',
						action: 'run',
						status: 'completed',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: 123
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when the activity payload uses old snake_case backend fields', async () => {
			const oldPayload = {
				items: [
					{
						id: 'act-1',
						triggered_by: 'user',
						action: 'run',
						status: 'completed',
						created_at: '2024-01-01T00:00:00Z'
					}
				],
				next_cursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => oldPayload
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});
	});

	describe('mergePastFeed', () => {
		it('prepends older activity items without duplicating ids', () => {
			const current: ActivityFeed = createActivityFeed({
				items: [
					createActivityItem({ id: 'act-3', createdAt: '2024-01-03T00:00:00Z' }),
					createActivityItem({ id: 'act-4', createdAt: '2024-01-04T00:00:00Z' })
				],
				nextCursor: null
			});

			const older: ActivityFeed = createActivityFeed({
				items: [
					createActivityItem({ id: 'act-1', createdAt: '2024-01-01T00:00:00Z' }),
					createActivityItem({ id: 'act-2', createdAt: '2024-01-02T00:00:00Z' }),
					createActivityItem({ id: 'act-3', createdAt: '2024-01-03T00:00:00Z' })
				],
				nextCursor: 'older-cursor'
			});

			const merged = mergePastFeed(current, older);

			expect(merged.items.map((i) => i.id)).toEqual(['act-1', 'act-2', 'act-3', 'act-4']);
			expect(merged.nextCursor).toBe('older-cursor');
		});

		it('returns current page when older is empty', () => {
			const current: ActivityFeed = createActivityFeed({
				items: [createActivityItem({ id: 'act-1' })],
				nextCursor: null
			});

			const older: ActivityFeed = { items: [], nextCursor: null };

			const merged = mergePastFeed(current, older);

			expect(merged.items).toEqual(current.items);
			expect(merged.nextCursor).toBeNull();
		});

		it('returns older items when current is empty', () => {
			const current: ActivityFeed = { items: [], nextCursor: null };
			const older: ActivityFeed = createActivityFeed({
				items: [createActivityItem({ id: 'act-1' })],
				nextCursor: 'cursor-123'
			});

			const merged = mergePastFeed(current, older);

			expect(merged.items).toEqual(older.items);
			expect(merged.nextCursor).toBe('cursor-123');
		});

		it('handles both pages being empty', () => {
			const current: ActivityFeed = { items: [], nextCursor: null };
			const older: ActivityFeed = { items: [], nextCursor: null };

			const merged = mergePastFeed(current, older);

			expect(merged.items).toEqual([]);
			expect(merged.nextCursor).toBeNull();
		});

		it('keeps past version when items overlap', () => {
			const current: ActivityFeed = createActivityFeed({
				items: [createActivityItem({ id: 'act-1', status: 'running', action: 'new-action' })],
				nextCursor: null
			});

			const older: ActivityFeed = createActivityFeed({
				items: [createActivityItem({ id: 'act-1', status: 'completed', action: 'old-action' })],
				nextCursor: 'old-cursor'
			});

			const merged = mergePastFeed(current, older);

			expect(merged.items).toHaveLength(1);
			expect(merged.items[0]?.status).toBe('completed');
			expect(merged.items[0]?.action).toBe('old-action');
		});
	});

	describe('getAgentActivityFeed - extensive scenarios', () => {
		it('handles empty items array', async () => {
			const emptyPage = {
				items: [],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => emptyPage
			} as ApiResponsePromise);

			const result = await getAgentActivityFeed('ava');

			expect(result.items).toEqual([]);
			expect(result.nextCursor).toBeNull();
		});

		it('handles all valid status values', async () => {
			for (const status of ['running', 'completed', 'failed'] as const) {
				const page = {
					items: [
						{
							id: `act-${status}`,
							triggeredBy: 'user',
							action: 'test',
							status,
							createdAt: '2024-01-01T00:00:00Z'
						}
					],
					nextCursor: null
				};

				vi.mocked(api.get).mockReturnValue({
					json: async () => page
				} as ApiResponsePromise);

				const result = await getAgentActivityFeed('ava');

				expect(result.items[0]?.status).toBe(status);
			}
		});

		it('handles activity with optional message field', async () => {
			const page = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user',
						action: 'run',
						status: 'completed',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise);

			const result = await getAgentActivityFeed('ava');

			expect(result.items[0]?.message).toBeUndefined();
		});

		it('handles activity with message field present', async () => {
			const page = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user',
						action: 'run',
						status: 'completed',
						message: 'Task completed successfully',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise);

			const result = await getAgentActivityFeed('ava');

			expect(result.items[0]?.message).toBe('Task completed successfully');
		});

		it('handles large limit values', async () => {
			const page = { items: [], nextCursor: null };

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise);

			await getAgentActivityFeed('ava', 1000);

			expect(api.get).toHaveBeenCalledWith('user/agent/ava/activity?limit=1000');
		});

		it('handles default limit parameter', async () => {
			const page = { items: [], nextCursor: null };

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise);

			await getAgentActivityFeed('ava');

			expect(api.get).toHaveBeenCalledWith('user/agent/ava/activity?limit=20');
		});

		it('handles special characters in triggeredBy field', async () => {
			const page = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user@example.com',
						action: 'run',
						status: 'completed',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise);

			const result = await getAgentActivityFeed('ava');

			expect(result.items[0]?.triggeredBy).toBe('user@example.com');
		});

		it('throws on null items array', async () => {
			const malformedPage = {
				items: null,
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow('items must be an array');
		});

		it('throws on non-object items', async () => {
			const malformedPage = {
				items: ['not-an-object'],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when id field is missing', async () => {
			const malformedPage = {
				items: [
					{
						triggeredBy: 'user',
						action: 'run',
						status: 'completed',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when id is not a string', async () => {
			const malformedPage = {
				items: [
					{
						id: 123,
						triggeredBy: 'user',
						action: 'run',
						status: 'completed',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when triggeredBy is missing', async () => {
			const malformedPage = {
				items: [
					{
						id: 'act-1',
						action: 'run',
						status: 'completed',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when action is missing', async () => {
			const malformedPage = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user',
						status: 'completed',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when createdAt is missing', async () => {
			const malformedPage = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user',
						action: 'run',
						status: 'completed'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws when message is not a string', async () => {
			const malformedPage = {
				items: [
					{
						id: 'act-1',
						triggeredBy: 'user',
						action: 'run',
						status: 'completed',
						message: 123,
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow();
		});

		it('throws on non-object payload', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => null
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow('expected an object');
		});

		it('throws on array payload', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => []
			} as ApiResponsePromise);

			await expect(getAgentActivityFeed('ava')).rejects.toThrow('items must be an array');
		});

		it('handles different agent names', async () => {
			const page = { items: [], nextCursor: null };

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise);

			await getAgentActivityFeed('custom-agent');

			expect(api.get).toHaveBeenCalledWith('user/agent/custom-agent/activity?limit=20');
		});

		it('handles URL encoding in agent names', async () => {
			const page = { items: [], nextCursor: null };

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise);

			await getAgentActivityFeed('agent with spaces');

			expect(api.get).toHaveBeenCalledWith('user/agent/agent with spaces/activity?limit=20');
		});

		it('handles complex cursor values', async () => {
			const page = {
				items: [],
				nextCursor: 'eyJpdGVtIjoiMTIzNDU2NzgiLCJ0aW1lIjoiMjAyNC0wMS0wMVQwMDowMDowMFoifQ=='
			};

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise);

			const result = await getAgentActivityFeed('ava');

			expect(result.nextCursor).toBe(
				'eyJpdGVtIjoiMTIzNDU2NzgiLCJ0aW1lIjoiMjAyNC0wMS0wMVQwMDowMDowMFoifQ=='
			);
		});
	});
});
