import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import type { ActivityFeed, AgentActivityItem } from '$lib/services/agent'
import WorkerPageTestWrapper from './WorkerPageTestWrapper.svelte'

vi.mock('$lib/services/agent', () => ({
	getAgentActivityFeed: vi.fn(),
	mergePastFeed: vi.fn(),
	getAgentConfiguration: vi.fn(),
	updateAgentConfiguration: vi.fn(),
	createAgent: vi.fn()
}))

vi.mock('$app/paths', () => ({
	resolve: vi.fn((path: string) => path)
}))

import { getAgentActivityFeed, mergePastFeed } from '$lib/services/agent'

const mockGetAgentActivityFeed = vi.mocked(getAgentActivityFeed)
const mockMergePastFeed = vi.mocked(mergePastFeed)

const workerAgents = {
	support: { name: 'support', label: 'Support Bot', icon: 'bot' as const },
	analyst: {
		name: 'analyst',
		label: 'Data Analyst',
		icon: 'chart-column' as const
	}
}

function createActivityItem(
	overrides: Partial<AgentActivityItem> = {}
): AgentActivityItem {
	return {
		id: '1',
		createdAt: new Date().toISOString(),
		action: 'Test action',
		status: 'completed',
		triggeredBy: 'user@example.com',
		...overrides
	}
}

function createActivityFeed(
	overrides: Partial<ActivityFeed> = {}
): ActivityFeed {
	return {
		items: [],
		nextCursor: null,
		...overrides
	}
}

function createPageData(overrides: Record<string, unknown> = {}) {
	return {
		agentsError: null,
		agent: workerAgents.support,
		activityFeed: createActivityFeed(),
		activityError: null,
		...overrides
	}
}

describe('WorkerPage', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(async () => {
		cleanup()
	})

	describe('Agent header', () => {
		it('renders support agent header', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({ agent: workerAgents.support })
			})

			await expect.element(page.getByText('Support Bot')).toBeVisible()
		})

		it('renders analyst agent header', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({ agent: workerAgents.analyst })
			})

			await expect.element(page.getByText('Data Analyst')).toBeVisible()
		})
	})

	describe('Activity feed - real data', () => {
		it('renders activity items from page data', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityFeed: createActivityFeed({
						items: [createActivityItem({ action: 'Synced inbox' })]
					})
				})
			})

			await expect.element(page.getByText('Synced inbox')).toBeVisible()
		})

		it('shows a load more button when nextCursor exists', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityFeed: createActivityFeed({
						items: [createActivityItem()],
						nextCursor: 'cursor-abc'
					})
				})
			})

			await expect
				.element(page.getByRole('button', { name: /load more/i }))
				.toBeVisible()
		})

		it('does not show load more button when nextCursor is null', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityFeed: createActivityFeed({
						items: [createActivityItem()]
					})
				})
			})

			await expect
				.element(page.getByRole('button', { name: /load more/i }))
				.not.toBeInTheDocument()
		})

		it('shows an empty state when no activity items exist', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData()
			})

			await expect.element(page.getByText('No activity yet.')).toBeVisible()
		})

		it('shows an error state when activityError is set', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityError: 'Failed to load activity'
				})
			})

			await expect
				.element(page.getByText('Failed to load activity'))
				.toBeVisible()
		})

		it('shows triggeredBy and status in activity cards', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityFeed: createActivityFeed({
						items: [
							createActivityItem({
								action: 'Processed ticket',
								triggeredBy: 'admin@example.com'
							})
						]
					})
				})
			})

			await expect.element(page.getByText(/admin@example.com/)).toBeVisible()
			await expect.element(page.getByText('completed')).toBeVisible()
		})

		it('shows failed status indicator', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityFeed: createActivityFeed({
						items: [
							createActivityItem({
								action: 'Error task',
								status: 'failed',
								message: 'Connection timeout'
							})
						]
					})
				})
			})

			await expect
				.element(page.getByText('failed', { exact: true }))
				.toBeVisible()
			await expect.element(page.getByText('Connection timeout')).toBeVisible()
		})

		it('shows running status indicator', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityFeed: createActivityFeed({
						items: [
							createActivityItem({
								action: 'Active task',
								status: 'running'
							})
						]
					})
				})
			})

			await expect
				.element(page.getByText('running', { exact: true }))
				.toBeVisible()
		})
	})

	describe('Activity feed - load more', () => {
		it('load more button is clickable when nextCursor exists', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityFeed: createActivityFeed({
						items: [createActivityItem()],
						nextCursor: 'cursor-abc'
					})
				})
			})

			const button = page.getByRole('button', { name: /load more/i })
			await expect.element(button).toBeVisible()
			await expect.element(button).toBeEnabled()
		})

		it('load more button shows loading state when clicked', async () => {
			let resolvePromise: ((value: ActivityFeed) => void) | undefined
			mockGetAgentActivityFeed.mockImplementation(
				() =>
					new Promise((resolve) => {
						resolvePromise = resolve
					})
			)
			mockMergePastFeed.mockReturnValue(
				createActivityFeed({ items: [createActivityItem()] })
			)

			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityFeed: createActivityFeed({
						items: [createActivityItem()],
						nextCursor: 'cursor-abc'
					})
				})
			})

			const button = page.getByRole('button', { name: /load more/i })
			await button.click()

			await new Promise((resolve) => setTimeout(resolve, 50))

			await expect.element(page.getByText('Loading...')).toBeVisible()

			resolvePromise?.(createActivityFeed())
			await new Promise((resolve) => setTimeout(resolve, 100))
		}, 20000)
	})

	describe('CTA button', () => {
		it('renders CTA button', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData()
			})

			await expect
				.element(page.getByText('Ask Ava about this agent'))
				.toBeVisible()
		})

		it('does not render model selector for workers', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData()
			})

			const modelButton = page.getByRole('button', {
				name: /Auto|GPT|Claude|Gemini|Grok/
			})
			await expect.element(modelButton).not.toBeInTheDocument()
		})

		it('does not render text area for workers', async () => {
			render(WorkerPageTestWrapper, {
				data: createPageData({ agent: workerAgents.analyst })
			})

			const textarea = page.getByPlaceholder('Enter your query!')
			await expect.element(textarea).not.toBeInTheDocument()
		})
	})

	describe('Activity feed - load more error', () => {
		it('load more failure preserves existing items', async () => {
			mockGetAgentActivityFeed.mockRejectedValueOnce(new Error('Network error'))

			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityFeed: createActivityFeed({
						items: [createActivityItem({ action: 'Existing item' })],
						nextCursor: 'cursor-abc'
					})
				})
			})

			await expect.element(page.getByText('Existing item')).toBeVisible()

			const loadMoreButton = page.getByRole('button', { name: /load more/i })
			await loadMoreButton.click()

			await new Promise((resolve) => setTimeout(resolve, 100))

			await expect.element(page.getByText('Existing item')).toBeVisible()
		})

		it('shows error message when load more fails', async () => {
			mockGetAgentActivityFeed.mockRejectedValueOnce(new Error('Network error'))

			render(WorkerPageTestWrapper, {
				data: createPageData({
					activityFeed: createActivityFeed({
						items: [createActivityItem()],
						nextCursor: 'cursor-abc'
					})
				})
			})

			const loadMoreButton = page.getByRole('button', { name: /load more/i })
			await loadMoreButton.click()

			await new Promise((resolve) => setTimeout(resolve, 100))

			await expect.element(page.getByText('Network error')).toBeVisible()
		})
	})
})
