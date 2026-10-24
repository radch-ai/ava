import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { Agent } from '$lib/types/agent';

const { mockAgents } = vi.hoisted(() => ({
	mockAgents: [] as Agent[]
}));

vi.mock('$lib/services/agent', () => ({
	getAgentActivityFeed: vi.fn()
}));

vi.mock('$lib/user', () => ({
	agents: mockAgents
}));

import { load } from '../../../../../src/routes/(app)/agent/[agentName]/+page';
import { getAgentActivityFeed } from '$lib/services/agent';

const mockGetAgentActivityFeed = vi.mocked(getAgentActivityFeed);

const agentsList: Agent[] = [
	{ name: 'support', label: 'Support Bot', icon: 'bot' },
	{ name: 'analyst', label: 'Data Analyst', icon: 'chart-column' }
];

describe('agent detail page load', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockAgents.length = 0;
	});

	it('returns agent data from shared state', async () => {
		mockAgents.push(...agentsList);
		mockGetAgentActivityFeed.mockResolvedValue({ items: [], nextCursor: null });

		const result = (await load({
			params: { agentName: 'support' },
			parent: vi.fn().mockResolvedValue({})
		} as unknown as Parameters<typeof load>[0])) as { agent: Agent };

		expect(result.agent).toEqual(agentsList[0]);
	});

	it('throws 404 when the agent is missing from shared state', async () => {
		mockAgents.push(...agentsList);
		mockGetAgentActivityFeed.mockResolvedValue({ items: [], nextCursor: null });

		await expect(
			load({
				params: { agentName: 'missing-agent' },
				parent: vi.fn().mockResolvedValue({})
			} as unknown as Parameters<typeof load>[0])
		).rejects.toMatchObject({
			status: 404,
			body: { message: "Agent 'missing-agent' not found" }
		});
	});
});
