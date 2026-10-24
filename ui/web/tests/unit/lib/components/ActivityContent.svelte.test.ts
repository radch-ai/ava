import { describe, expect, it, afterEach } from 'vitest';
import { render, cleanup } from 'vitest-browser-svelte';
import { page } from 'vitest/browser';
import ActivityContent from '$components/agent/ActivityContent.svelte';
import type { AgentActivityItem } from '$lib/services/agent';

function createTestActivityItem(overrides: Partial<AgentActivityItem> = {}): AgentActivityItem {
	return {
		id: `activity-${Math.random()}`,
		createdAt: new Date().toISOString(),
		triggeredBy: 'user',
		action: 'Test action',
		status: 'completed',
		...overrides
	};
}

describe('ActivityContent', () => {
	afterEach(async () => {
		cleanup();
	});

	describe('Activity list rendering', () => {
		it('renders list of activities', async () => {
			render(ActivityContent, {
				items: [
					createTestActivityItem({ id: '1', action: 'Created thread' }),
					createTestActivityItem({ id: '2', action: 'Updated config' })
				]
			});

			await expect.element(page.getByText('Created thread')).toBeVisible();
			await expect.element(page.getByText('Updated config')).toBeVisible();
		});

		it('renders activity with message', async () => {
			render(ActivityContent, {
				items: [createTestActivityItem({ action: 'Failed task', message: 'Connection timeout' })]
			});

			await expect.element(page.getByText('Failed task')).toBeVisible();
			await expect.element(page.getByText('Connection timeout')).toBeVisible();
		});

		it('displays triggered by info', async () => {
			render(ActivityContent, {
				items: [createTestActivityItem({ triggeredBy: 'system', action: 'Scheduled task' })]
			});

			await expect.element(page.getByText(/system/)).toBeVisible();
		});
	});

	describe('Activity status indicators', () => {
		it('displays completed status text', async () => {
			render(ActivityContent, {
				items: [createTestActivityItem({ action: 'Completed task', status: 'completed' })]
			});

			const statusBadge = page.getByText('completed', { exact: true });
			await expect.element(statusBadge).toBeVisible();
		});

		it('displays failed status text', async () => {
			render(ActivityContent, {
				items: [createTestActivityItem({ action: 'Failed task', status: 'failed' })]
			});

			const statusBadge = page.getByText('failed', { exact: true });
			await expect.element(statusBadge).toBeVisible();
		});

		it('displays running status text', async () => {
			render(ActivityContent, {
				items: [createTestActivityItem({ action: 'Running task', status: 'running' })]
			});

			const statusBadge = page.getByText('running', { exact: true });
			await expect.element(statusBadge).toBeVisible();
		});
	});

	describe('Empty items', () => {
		it('renders nothing when items is empty', async () => {
			render(ActivityContent, { items: [] });

			expect(page.getByText('No activity yet.').query()).toBeNull();
		});
	});

	describe('Edge cases', () => {
		it('handles activities without message gracefully', async () => {
			render(ActivityContent, {
				items: [createTestActivityItem({ action: 'Simple action', message: undefined })]
			});

			await expect.element(page.getByText('Simple action')).toBeVisible();
		});

		it('handles very long activity messages', async () => {
			const longMessage = 'A'.repeat(1000);
			render(ActivityContent, {
				items: [createTestActivityItem({ action: 'Long message', message: longMessage })]
			});

			await expect.element(page.getByText(longMessage)).toBeVisible();
		});

		it('handles special characters in activity messages', async () => {
			const specialMessage = 'Test with <script> & "quotes" and \'apostrophes\'';
			render(ActivityContent, {
				items: [createTestActivityItem({ action: 'Special chars', message: specialMessage })]
			});

			await expect.element(page.getByText('Special chars')).toBeVisible();
		});
	});
});
