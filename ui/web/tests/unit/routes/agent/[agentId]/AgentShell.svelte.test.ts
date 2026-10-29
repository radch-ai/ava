import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import type { ActivityFeed, AgentActivityItem } from '#lib/services/agent'
import type { Agent } from '#lib/types/agent'
import TestWrapper from './TestWrapper.svelte'

vi.mock('#lib/services/agent', () => ({
	getAgentActivityFeed: vi.fn(),
	mergePastFeed: vi.fn(),
	getAgentThread: vi.fn(),
	mergePastThread: vi.fn(),
	getAgentConfiguration: vi.fn(),
	updateAgentConfiguration: vi.fn(),
	createAgent: vi.fn()
}))

import {
	getAgentActivityFeed,
	getAgentConfiguration,
	mergePastFeed
} from '#lib/services/agent'
import type { SuperagentConfiguration } from '#lib/types/agent'

const mockGetAgentActivityFeed = vi.mocked(getAgentActivityFeed)
const mockMergePastFeed = vi.mocked(mergePastFeed)
const mockGetAgentConfiguration = vi.mocked(getAgentConfiguration)

interface ThreadItem {
	id: string
	role: 'user' | 'assistant'
	content: string
}

const superagent: Agent = {
	id: '_superagent',
	name: 'Ava',
	icon: 'message-square'
}

const agent: Agent = {
	id: 'support',
	name: 'Support',
	icon: 'bot'
}

function createTestActivityItem(
	overrides: Partial<AgentActivityItem> = {}
): AgentActivityItem {
	return {
		id: `activity-${Math.random()}`,
		createdAt: new Date().toISOString(),
		triggeredBy: 'user',
		action: 'Test action',
		status: 'completed',
		...overrides
	}
}

function createTestFeed(
	items: AgentActivityItem[] = [],
	nextCursor: string | null = null
): ActivityFeed {
	return { items, nextCursor }
}

function createDefaultTestConfig(): SuperagentConfiguration {
	return {
		name: 'Test',
		icon: 'bot',
		soul: { personality: '', identity: '' },
		userContext: '',
		operatingInstructions: '',
		toolsContext: '',
		toolsEnabled: [],
		cronJobs: [],
		heartbeat: {
			enabled: false,
			duration: '1hr',
			content: '',
			model: 'default'
		}
	}
}

const mockMessages: ThreadItem[] = [
	{ id: '1', role: 'user', content: 'Hello' },
	{ id: '2', role: 'assistant', content: 'Hi there!' }
]

describe('AgentShell', () => {
	beforeEach(() => {
		vi.clearAllMocks()
		mockGetAgentActivityFeed.mockResolvedValue(createTestFeed())
		mockMergePastFeed.mockImplementation((current) => current)
		mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
	})

	afterEach(async () => {
		cleanup()
	})

	describe('Header rendering', () => {
		it('renders agent header with provided agent', async () => {
			render(TestWrapper, {
				agent: superagent
			})

			await expect.element(page.getByText('Ava')).toBeVisible()
		})

		it('renders separator', async () => {
			const { container } = render(TestWrapper, {
				agent: superagent
			})

			const separator = container.querySelector('[role="separator"]')
			expect(separator).toBeInTheDocument()
		})

		it('displays agent name in header', async () => {
			render(TestWrapper, {
				agent
			})

			await expect.element(page.getByText('Support')).toBeVisible()
		})
	})

	describe('Tab visibility', () => {
		it('shows tabs for superagent agent when activeTab and onTabChange are provided', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				onTabChange: vi.fn()
			})

			await expect
				.element(page.getByRole('tab', { name: 'Conversation' }))
				.toBeVisible()
			await expect
				.element(page.getByRole('tab', { name: 'Activity' }))
				.toBeVisible()
		})

		it('does NOT show tabs for non-superagent agents', async () => {
			render(TestWrapper, {
				agent,
				activeTab: 'conversation',
				onTabChange: vi.fn()
			})

			expect(page.getByRole('tab', { name: 'Conversation' }).query()).toBeNull()
			expect(page.getByRole('tab', { name: 'Activity' }).query()).toBeNull()
		})

		it('does NOT show tabs for superagent agent when activeTab is undefined', async () => {
			render(TestWrapper, {
				agent: superagent,
				onTabChange: vi.fn()
			})

			expect(page.getByRole('tab', { name: 'Conversation' }).query()).toBeNull()
			expect(page.getByRole('tab', { name: 'Activity' }).query()).toBeNull()
		})

		it('does NOT show tabs for superagent agent when onTabChange is undefined', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation'
			})

			expect(page.getByRole('tab', { name: 'Conversation' }).query()).toBeNull()
			expect(page.getByRole('tab', { name: 'Activity' }).query()).toBeNull()
		})
	})

	describe('Tab switching behavior', () => {
		it('calls onTabChange when tab is clicked', async () => {
			const onTabChange = vi.fn()

			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				onTabChange
			})

			await page.getByRole('tab', { name: 'Activity' }).click()

			expect(onTabChange).toHaveBeenCalledWith('activity')
		})

		it('shows conversation tab as selected when activeTab is conversation', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				onTabChange: vi.fn()
			})

			const conversationTab = page.getByRole('tab', { name: 'Conversation' })
			await expect
				.element(conversationTab)
				.toHaveAttribute('aria-selected', 'true')
		})

		it('shows activity tab as selected when activeTab is activity', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'activity',
				onTabChange: vi.fn()
			})

			const activityTab = page.getByRole('tab', { name: 'Activity' })
			await expect.element(activityTab).toHaveAttribute('aria-selected', 'true')
		})
	})

	describe('ThreadContent rendering (superagent conversation tab)', () => {
		it('renders ThreadContent when superagent agent on conversation tab with threadItems', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages
			})

			await expect.element(page.getByText('Hello')).toBeVisible()
			await expect.element(page.getByText('Hi there!')).toBeVisible()
		})

		it('shows empty state when threadItems is empty array', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: []
			})

			await expect
				.element(page.getByText('No messages yet. Start a conversation!'))
				.toBeVisible()
		})

		it('does NOT render ThreadPanel when activeTab is activity', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'activity',
				threadItems: mockMessages
			})

			expect(page.getByText('Hello').query()).toBeNull()
		})

		it('does NOT render ThreadPanel for non-superagent agents', async () => {
			render(TestWrapper, {
				agent,
				threadItems: mockMessages
			})

			expect(page.getByText('Hello').query()).toBeNull()
		})
	})

	describe('ActivityContent rendering', () => {
		it('renders ActivityContent for non-superagent agents', async () => {
			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([
					createTestActivityItem({ action: 'Test activity' })
				])
			})

			await expect.element(page.getByText('Test activity')).toBeVisible()
		})

		it('renders ActivityContent for superagent agent on activity tab', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'activity',
				activityFeed: createTestFeed([
					createTestActivityItem({ action: 'Ava activity' })
				])
			})

			await expect.element(page.getByText('Ava activity')).toBeVisible()
		})

		it('shows empty state when no activities', async () => {
			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([])
			})

			await expect.element(page.getByText('No activity yet.')).toBeVisible()
		})
	})

	describe('Children/footer slot', () => {
		it('renders footer element', async () => {
			const { container } = render(TestWrapper, {
				agent: agent
			})

			const footer = container.querySelector('footer')
			expect(footer).toBeInTheDocument()
		})

		it('footer has correct styling classes', async () => {
			const { container } = render(TestWrapper, {
				agent: agent
			})

			const footer = container.querySelector('footer')
			expect(footer).toHaveClass('max-md:px-6')
		})
	})

	describe('Thread error states', () => {
		it('shows thread error message when threadError is provided', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadError: 'Failed to load thread'
			})

			await expect
				.element(page.getByText('Failed to load thread'))
				.toBeVisible()
		})

		it('shows thread error with destructive styling', async () => {
			const { container } = render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadError: 'Connection failed'
			})

			const errorElement = container.querySelector('.bg-destructive\\/10')
			expect(errorElement).toBeInTheDocument()
		})

		it('hides ThreadContent when there is a thread error', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages,
				threadError: 'Error occurred'
			})

			expect(page.getByText('Hello').query()).toBeNull()
			await expect.element(page.getByText('Error occurred')).toBeVisible()
		})
	})

	describe('Activity error states', () => {
		it('shows activity error message when activityError is provided', async () => {
			render(TestWrapper, {
				agent,
				activityError: 'Failed to load activity'
			})

			await expect
				.element(page.getByText('Failed to load activity'))
				.toBeVisible()
		})

		it('shows activity error with destructive styling', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'activity',
				activityError: 'Network error'
			})

			const errorElement = page.getByText('Network error')
			await expect.element(errorElement).toBeInTheDocument()
		})

		it('hides ActivityContent when there is an activity error', async () => {
			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([
					createTestActivityItem({ action: 'Test' })
				]),
				activityError: 'Error loading'
			})

			expect(page.getByText('Test').query()).toBeNull()
		})
	})

	describe('Loading states', () => {
		it('shows loading message when isLoadingActivity is true', async () => {
			render(TestWrapper, {
				agent,
				isLoadingActivity: true
			})

			await expect.element(page.getByText('Loading...')).toBeVisible()
		})

		it('hides ActivityContent when loading', async () => {
			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([
					createTestActivityItem({ action: 'Test' })
				]),
				isLoadingActivity: true
			})

			expect(page.getByText('Test').query()).toBeNull()
		})

		it('does not show loading for thread (shell handles load-more state)', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages,
				threadIsLoadingMore: true
			})

			await expect.element(page.getByText('Hello')).toBeVisible()
		})
	})

	describe('Shared shell - conversation tab load more', () => {
		it('renders load more button in shell when threadNextCursor is provided', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages,
				threadNextCursor: 'next-page-token',
				onThreadLoadMore: vi.fn()
			})

			await expect
				.element(page.getByRole('button', { name: 'Load more' }))
				.toBeVisible()
		})

		it('does not render load more button when threadNextCursor is null', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages,
				threadNextCursor: null
			})

			expect(page.getByRole('button', { name: 'Load more' }).query()).toBeNull()
		})

		it('calls onThreadLoadMore when load more button is clicked', async () => {
			const onThreadLoadMore = vi.fn()

			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages,
				threadNextCursor: 'next-page-token',
				onThreadLoadMore
			})

			const loadMoreButton = page
				.getByRole('button', { name: 'Load more' })
				.query() as HTMLElement | null
			expect(loadMoreButton).not.toBeNull()
			loadMoreButton?.click()

			expect(onThreadLoadMore).toHaveBeenCalled()
		})

		it('shows load more error with try again message in shell', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages,
				threadNextCursor: 'next-cursor',
				threadLoadMoreError: 'Failed to load more',
				onThreadLoadMore: vi.fn()
			})

			await expect
				.element(page.getByText('Failed to load more. Try again.'))
				.toBeVisible()
		})

		it('calls onThreadLoadMore when load more button is clicked after error', async () => {
			const onThreadLoadMore = vi.fn()

			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages,
				threadNextCursor: 'next-cursor',
				threadLoadMoreError: 'Failed to load more',
				onThreadLoadMore
			})

			await page.getByRole('button', { name: 'Load more' }).click()

			expect(onThreadLoadMore).toHaveBeenCalled()
		})

		it('disables load more button when threadIsLoadingMore is true', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages,
				threadNextCursor: 'next-cursor',
				threadIsLoadingMore: true,
				onThreadLoadMore: vi.fn()
			})

			const button = page.getByRole('button', { name: 'Loading...' })
			await expect.element(button).toBeVisible()
			await expect.element(button).toBeDisabled()
		})
	})

	describe('Shared shell - activity tab load more', () => {
		it('renders load more button in shell when activityFeed has nextCursor', async () => {
			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([createTestActivityItem()], 'next-cursor')
			})

			await expect
				.element(page.getByRole('button', { name: 'Load more' }))
				.toBeVisible()
		})

		it('does not render load more button when activityFeed has no nextCursor', async () => {
			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([createTestActivityItem()])
			})

			expect(page.getByRole('button', { name: 'Load more' }).query()).toBeNull()
		})

		it('fetches activity feed and calls onActivityLoadMore with merged result', async () => {
			const onActivityLoadMore = vi.fn()
			const olderItems = [createTestActivityItem({ action: 'Older activity' })]
			const olderFeed = createTestFeed(olderItems, null)
			mockGetAgentActivityFeed.mockResolvedValue(olderFeed)
			const mergedFeed = createTestFeed(
				[...olderItems, createTestActivityItem({ action: 'Current' })],
				null
			)
			mockMergePastFeed.mockReturnValue(mergedFeed)

			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed(
					[createTestActivityItem({ action: 'Current' })],
					'next-cursor'
				),
				onActivityLoadMore
			})

			await page.getByRole('button', { name: 'Load more' }).click()

			expect(mockGetAgentActivityFeed).toHaveBeenCalledWith(
				'support',
				20,
				'next-cursor'
			)
			expect(mockMergePastFeed).toHaveBeenCalled()
			expect(onActivityLoadMore).toHaveBeenCalledWith(mergedFeed)
		})

		it('shows load more error when fetch fails', async () => {
			mockGetAgentActivityFeed.mockRejectedValue(new Error('Network failure'))

			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([createTestActivityItem()], 'next-cursor')
			})

			await page.getByRole('button', { name: 'Load more' }).click()

			await expect.element(page.getByText('Network failure')).toBeVisible()
		})

		it('shows load more error with try again message', async () => {
			mockGetAgentActivityFeed.mockRejectedValue(new Error('Failed'))

			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([createTestActivityItem()], 'next-cursor')
			})

			await page.getByRole('button', { name: 'Load more' }).click()

			await expect.element(page.getByText('Failed. Try again.')).toBeVisible()
		})

		it('retries fetch when load more button is clicked after error', async () => {
			const onActivityLoadMore = vi.fn()
			mockGetAgentActivityFeed
				.mockRejectedValueOnce(new Error('Failed'))
				.mockResolvedValueOnce(createTestFeed())

			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([createTestActivityItem()], 'next-cursor'),
				onActivityLoadMore
			})

			await page.getByRole('button', { name: 'Load more' }).click()

			await expect.element(page.getByText('Failed. Try again.')).toBeVisible()
			await page.getByRole('button', { name: 'Load more' }).click()

			expect(mockGetAgentActivityFeed).toHaveBeenCalledTimes(2)
		})

		it('does not fetch when nextCursor is null', async () => {
			const onActivityLoadMore = vi.fn()

			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([createTestActivityItem()]),
				onActivityLoadMore
			})

			expect(mockGetAgentActivityFeed).not.toHaveBeenCalled()
		})

		it('disables load more button while fetching', async () => {
			let resolveFetch: ((value: ActivityFeed) => void) | undefined
			mockGetAgentActivityFeed.mockReturnValue(
				new Promise<ActivityFeed>((resolve) => {
					resolveFetch = resolve
				})
			)

			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([createTestActivityItem()], 'next-cursor')
			})

			await page.getByRole('button', { name: 'Load more' }).click()

			const button = page.getByRole('button', { name: 'Loading...' })
			await expect.element(button).toBeVisible()
			await expect.element(button).toBeDisabled()

			resolveFetch?.(createTestFeed())
		})

		it('uses generic error message when error is not an Error instance', async () => {
			mockGetAgentActivityFeed.mockRejectedValue('string error')

			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([createTestActivityItem()], 'next-cursor')
			})

			await page.getByRole('button', { name: 'Load more' }).click()

			await expect.element(page.getByText('Failed to load more')).toBeVisible()
		})
	})

	describe('Shared shell rendering order', () => {
		it('renders load more before items for conversation tab', async () => {
			const { container } = render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages,
				threadNextCursor: 'cursor',
				onThreadLoadMore: vi.fn()
			})

			const loadMoreButton = page.getByRole('button', { name: 'Load more' })
			await expect.element(loadMoreButton).toBeVisible()

			const itemsContainer = container.querySelector('.flex.flex-col.gap-4')
			expect(itemsContainer).toBeInTheDocument()
		})

		it('renders load more before items for activity tab', async () => {
			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([createTestActivityItem()], 'cursor')
			})

			await expect
				.element(page.getByRole('button', { name: 'Load more' }))
				.toBeVisible()
			await expect.element(page.getByText('Test action')).toBeVisible()
		})

		it('renders empty state when conversation items are empty', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: []
			})

			await expect
				.element(page.getByText('No messages yet. Start a conversation!'))
				.toBeVisible()
			expect(page.getByText('Hello').query()).toBeNull()
		})

		it('renders empty state when activity items are empty', async () => {
			render(TestWrapper, {
				agent,
				activityFeed: createTestFeed([])
			})

			await expect.element(page.getByText('No activity yet.')).toBeVisible()
		})
	})

	describe('Layout structure', () => {
		it('keeps header and footer outside the scroll region', async () => {
			const { container } = render(TestWrapper, {
				agent: agent
			})

			const shell = container.firstElementChild
			expect(shell).toHaveClass('h-full')
			expect(shell).toHaveClass('flex-col')

			const header = container.querySelector('header')
			expect(header).toBeInTheDocument()

			const scrollRegion = container.querySelector(
				'.custom-scrollbar.flex-1.overflow-y-auto'
			)
			expect(scrollRegion).toBeInTheDocument()
			expect(scrollRegion?.previousElementSibling?.tagName).toBe('HEADER')
			expect(scrollRegion?.nextElementSibling?.tagName).toBe('FOOTER')

			const footer = container.querySelector('footer')
			expect(footer).toBeInTheDocument()
		})

		it('uses a full-width outer scroll region and a separate inner content container', async () => {
			const { container } = render(TestWrapper, {
				agent: agent
			})

			const scrollRegion = container.querySelector(
				'.custom-scrollbar.flex-1.overflow-y-auto'
			)
			expect(scrollRegion).toBeInTheDocument()
			expect(scrollRegion).toHaveClass('flex-1')
			expect(scrollRegion).toHaveClass('overflow-y-auto')

			const contentContainer = scrollRegion?.querySelector(
				'.content-container.py-4'
			)
			expect(contentContainer).toBeInTheDocument()
			expect(contentContainer).not.toHaveClass('overflow-y-auto')
		})

		it('content area uses custom-scrollbar styling', async () => {
			const { container } = render(TestWrapper, {
				agent: agent
			})

			const content = container.querySelector('.custom-scrollbar')
			expect(content).toBeInTheDocument()
			expect(content).toHaveClass('custom-scrollbar')
		})
	})

	describe('Conditional rendering priority', () => {
		it('prioritizes thread error over threadItems for superagent conversation', async () => {
			render(TestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				threadItems: mockMessages,
				threadError: 'Thread error'
			})

			expect(page.getByText('Hello').query()).toBeNull()
			await expect.element(page.getByText('Thread error')).toBeVisible()
		})

		it('prioritizes activity error over loading state', async () => {
			render(TestWrapper, {
				agent,
				isLoadingActivity: true,
				activityError: 'Activity error'
			})

			await expect.element(page.getByText('Activity error')).toBeVisible()
			expect(page.getByText('Loading...').query()).toBeNull()
		})

		it('shows ActivityContent for non-superagent when activityFeed is explicitly null', async () => {
			const { container } = render(TestWrapper, {
				agent,
				activityFeed: null
			})

			const scrollRegion = container.querySelector(
				'.custom-scrollbar.flex-1.overflow-y-auto'
			)
			expect(scrollRegion).toBeInTheDocument()
		})
	})
})
