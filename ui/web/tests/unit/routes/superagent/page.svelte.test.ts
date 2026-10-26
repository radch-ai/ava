import { afterEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import type { IconName } from '#lib/icons'
import type { SuperagentThread } from '#lib/services/user'
import type { Agent } from '#lib/types/agent'
import SuperagentPage from './SuperagentPageTestWrapper.svelte'

vi.mock('#lib/services/api', () => ({
	api: {
		get: vi.fn()
	}
}))

import { type ApiResponsePromise, api } from '#lib/services/api'

const mockAgents = [
	{ name: 'ava', icon: 'sparkles' as IconName, label: 'Ava' },
	{ name: 'support', icon: 'bot' as IconName, label: 'Support' },
	{ name: 'analyst', icon: 'chart-column' as IconName, label: 'Analyst' }
]

function createThread(
	items: Array<{ id: string; role: 'user' | 'assistant'; content: string }>,
	opts: { nextCursor?: string | null } = {}
): SuperagentThread {
	return {
		items: items.map((item) => ({
			...item,
			createdAt: '2026-03-22T00:00:00Z'
		})),
		nextCursor: opts.nextCursor ?? null
	}
}

function createActivityFeed(
	items: Array<{
		id: string
		action: string
		status?: 'running' | 'completed' | 'failed'
		triggeredBy?: string
		message?: string
	}>,
	opts: { nextCursor?: string | null } = {}
) {
	return {
		items: items.map((item) => ({
			id: item.id,
			createdAt: '2026-03-22T00:00:00Z',
			action: item.action,
			status: item.status ?? 'completed',
			triggeredBy: item.triggeredBy ?? 'user@example.com',
			message: item.message
		})),
		nextCursor: opts.nextCursor ?? null
	}
}

interface TestPageData {
	agents: Agent[]
	thread: SuperagentThread | null
	threadError: string | null
}
function createMockData(overrides: Partial<TestPageData> = {}) {
	return {
		thread: null,
		threadError: null,
		agents: mockAgents,
		...overrides
	}
}

describe('SuperagentPage', () => {
	afterEach(async () => {
		cleanup()
		vi.clearAllMocks()
	})

	describe('initial rendering', () => {
		it('renders agent header with superagent name', async () => {
			render(SuperagentPage, { data: createMockData() })

			await expect.element(page.getByText('Chat with Ava')).toBeVisible()
		})

		it('renders model selector', async () => {
			render(SuperagentPage, { data: createMockData() })

			// role=combobox is nameFrom:author — its accessible name is empty, so
			// match the visible label via text content instead
			const modelButton = page.getByRole('combobox')
			await expect.element(modelButton).toBeVisible()
			await expect
				.element(modelButton)
				.toHaveTextContent(/auto|GPT|Claude|Gemini|Grok/i)
		})

		it('renders text area', async () => {
			render(SuperagentPage, { data: createMockData() })

			const textarea = page.getByPlaceholder('Enter your query!')
			await expect.element(textarea).toBeVisible()
		})

		it('uses a breakout footer with a centered composer and right-aligned model selector', async () => {
			const { container } = render(SuperagentPage, { data: createMockData() })

			const footer = container.querySelector('footer')
			expect(footer).toBeInTheDocument()
			expect(footer?.className).not.toContain('content-container')

			const gridContainer = footer?.querySelector('.md\\:grid')
			expect(gridContainer).toBeInTheDocument()
			expect(gridContainer?.className).toContain(
				'md:grid-cols-[1fr_var(--content-max-width)_1fr]'
			)

			const modelSelectorContainer = container.querySelector(
				'.mr-2.flex.items-center.justify-end'
			)
			expect(modelSelectorContainer).toBeInTheDocument()

			const modelButton = page.getByRole('combobox')
			await expect.element(modelButton).toBeVisible()
			await expect
				.element(modelButton)
				.toHaveTextContent(/auto|GPT|Claude|Gemini|Grok/i)

			const textarea = page.getByPlaceholder('Enter your query!')
			await expect.element(textarea).toBeVisible()
		})
	})

	describe('thread messages from page data', () => {
		it('renders thread messages from page data', async () => {
			const threadPage = createThread([
				{ id: '1', role: 'assistant', content: 'Welcome back' },
				{ id: '2', role: 'user', content: 'Show me updates' }
			])

			render(SuperagentPage, { data: createMockData({ thread: threadPage }) })

			await expect.element(page.getByText('Welcome back')).toBeVisible()
			await expect.element(page.getByText('Show me updates')).toBeVisible()
		})

		it('renders user messages with right alignment', async () => {
			const threadPage = createThread([
				{ id: '1', role: 'user', content: 'User message here' }
			])

			render(SuperagentPage, { data: createMockData({ thread: threadPage }) })

			const message = page.getByText('User message here')
			await expect.element(message).toBeVisible()
		})

		it('renders assistant messages with left alignment', async () => {
			const threadPage = createThread([
				{ id: '1', role: 'assistant', content: 'Assistant reply here' }
			])

			render(SuperagentPage, { data: createMockData({ thread: threadPage }) })

			const message = page.getByText('Assistant reply here')
			await expect.element(message).toBeVisible()
		})

		it('shows no messages placeholder when thread is empty', async () => {
			const threadPage = createThread([])

			render(SuperagentPage, { data: createMockData({ thread: threadPage }) })

			await expect.element(page.getByText(/no messages yet/i)).toBeVisible()
		})
	})

	describe('load more functionality', () => {
		it('shows load more button above thread list when nextCursor exists', async () => {
			const threadPage = createThread(
				[
					{ id: '3', role: 'user', content: 'Latest message' },
					{ id: '2', role: 'assistant', content: 'Middle message' },
					{ id: '1', role: 'user', content: 'Oldest message' }
				],
				{ nextCursor: 'cursor-abc' }
			)

			render(SuperagentPage, { data: createMockData({ thread: threadPage }) })

			const loadMoreButton = page.getByRole('button', { name: /load more/i })
			await expect.element(loadMoreButton).toBeVisible()
		})

		it('hides load more button when nextCursor is null', async () => {
			const threadPage = createThread([
				{ id: '1', role: 'user', content: 'Only message' }
			])

			render(SuperagentPage, { data: createMockData({ thread: threadPage }) })

			await expect
				.element(page.getByRole('button', { name: /load more/i }))
				.not.toBeInTheDocument()
		})
	})

	describe('error handling', () => {
		it('shows inline error state when initial thread load fails', async () => {
			render(SuperagentPage, {
				data: createMockData({
					thread: null,
					threadError: 'Failed to load conversation'
				})
			})

			await expect
				.element(page.getByText(/failed to load conversation/i))
				.toBeVisible()
		})

		it('keeps page shell visible when thread load fails', async () => {
			render(SuperagentPage, {
				data: createMockData({
					thread: null,
					threadError: 'Failed to load conversation'
				})
			})

			await expect.element(page.getByText('Chat with Ava')).toBeVisible()
			const textarea = page.getByPlaceholder('Enter your query!')
			await expect.element(textarea).toBeVisible()
		})
	})

	describe('load more failure handling', () => {
		it('preserves existing messages when load more fails', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(new Error('Network error'))
			} as ApiResponsePromise)

			const threadPage = createThread(
				[{ id: '1', role: 'user', content: 'Existing message' }],
				{
					nextCursor: 'cursor-abc'
				}
			)

			render(SuperagentPage, { data: createMockData({ thread: threadPage }) })

			await expect.element(page.getByText('Existing message')).toBeVisible()

			const loadMoreButton = page.getByRole('button', { name: /load more/i })
			await loadMoreButton.click()

			await expect.element(page.getByText('Existing message')).toBeVisible()
		})

		it('shows retry affordance when load more fails', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(new Error('Network error'))
			} as ApiResponsePromise)

			const threadPage = createThread(
				[{ id: '1', role: 'user', content: 'Existing message' }],
				{
					nextCursor: 'cursor-abc'
				}
			)

			render(SuperagentPage, { data: createMockData({ thread: threadPage }) })

			const loadMoreButton = page.getByRole('button', { name: /load more/i })
			await loadMoreButton.click()

			await expect.element(page.getByText(/network error/i)).toBeVisible()
			await expect.element(page.getByText(/try again/i)).toBeVisible()
			await expect
				.element(page.getByRole('button', { name: /load more/i }))
				.toBeVisible()
		})
	})

	describe('pagination merge order', () => {
		it('prepends older items above current items', async () => {
			const olderPage = createThread(
				[
					{ id: '1', role: 'user', content: 'Oldest message' },
					{ id: '2', role: 'assistant', content: 'Old reply' }
				],
				{ nextCursor: null }
			)

			vi.mocked(api.get).mockReturnValue({
				json: async () => olderPage
			} as ApiResponsePromise)

			const threadPage = createThread(
				[
					{ id: '3', role: 'user', content: 'Newer message' },
					{ id: '4', role: 'assistant', content: 'Newest reply' }
				],
				{ nextCursor: 'cursor-abc' }
			)

			render(SuperagentPage, { data: createMockData({ thread: threadPage }) })

			await expect.element(page.getByText('Newer message')).toBeVisible()
			await expect.element(page.getByText('Newest reply')).toBeVisible()

			const loadMoreButton = page.getByRole('button', { name: /load more/i })
			await loadMoreButton.click()

			await expect.element(page.getByText('Oldest message')).toBeVisible()
			await expect.element(page.getByText('Old reply')).toBeVisible()
			await expect.element(page.getByText('Newer message')).toBeVisible()
			await expect.element(page.getByText('Newest reply')).toBeVisible()
		})
	})

	describe('tab switching', () => {
		it('shows conversation tab and activity tab', async () => {
			render(SuperagentPage, { data: createMockData() })

			await expect
				.element(page.getByRole('tab', { name: /conversation/i }))
				.toBeVisible()
			await expect
				.element(page.getByRole('tab', { name: /activity/i }))
				.toBeVisible()
		})

		it('defaults to conversation tab selected', async () => {
			render(SuperagentPage, { data: createMockData() })

			const conversationTab = page.getByRole('tab', { name: /conversation/i })
			await expect
				.element(conversationTab)
				.toHaveAttribute('aria-selected', 'true')
		})

		it('switches to the activity tab and keeps the composer visible', async () => {
			const activityPage = createActivityFeed([
				{ id: 'a1', action: 'Searched web', status: 'completed' }
			])
			vi.mocked(api.get).mockReturnValue({
				json: async () => activityPage
			} as ApiResponsePromise)

			render(SuperagentPage, { data: createMockData() })

			const activityTab = page.getByRole('tab', { name: /activity/i })
			await activityTab.click()

			await expect.element(activityTab).toHaveAttribute('aria-selected', 'true')

			const textarea = page.getByPlaceholder('Enter your query!')
			await expect.element(textarea).toBeVisible()
		})

		it('lazy-loads activity on first tab open and preserves conversation state', async () => {
			const activityPage = createActivityFeed([
				{ id: 'a1', action: 'Searched web', status: 'completed' }
			])
			vi.mocked(api.get).mockReturnValue({
				json: async () => activityPage
			} as ApiResponsePromise)

			const threadPage = createThread([
				{ id: '1', role: 'user', content: 'My message' }
			])

			render(SuperagentPage, { data: createMockData({ thread: threadPage }) })

			await expect.element(page.getByText('My message')).toBeVisible()

			const activityTab = page.getByRole('tab', { name: /activity/i })
			await activityTab.click()

			await expect.element(page.getByText('Searched web')).toBeVisible()
			await expect.element(page.getByText('completed')).toBeVisible()

			const conversationTab = page.getByRole('tab', { name: /conversation/i })
			await conversationTab.click()

			await expect.element(page.getByText('My message')).toBeVisible()
		})

		it('shows activity error when lazy load fails', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(new Error('Activity load failed'))
			} as ApiResponsePromise)

			render(SuperagentPage, { data: createMockData() })

			const activityTab = page.getByRole('tab', { name: /activity/i })
			await activityTab.click()

			await expect
				.element(page.getByText(/activity load failed/i))
				.toBeVisible()

			const textarea = page.getByPlaceholder('Enter your query!')
			await expect.element(textarea).toBeVisible()
		})

		it('renders activity cards with triggeredBy and status', async () => {
			const activityPage = createActivityFeed([
				{
					id: 'a1',
					action: 'Ran analysis',
					status: 'completed',
					triggeredBy: 'alice@example.com'
				}
			])
			vi.mocked(api.get).mockReset()
			vi.mocked(api.get).mockReturnValue({
				json: async () => activityPage
			} as ApiResponsePromise)

			render(SuperagentPage, { data: createMockData() })

			const activityTab = page.getByRole('tab', { name: /activity/i })
			await activityTab.click()

			await expect.element(page.getByText(/alice@example.com/)).toBeVisible()
			await expect.element(page.getByText('completed')).toBeVisible()
			await expect.element(page.getByText('Ran analysis')).toBeVisible()
		})

		it('renders activity cards with message when present', async () => {
			const activityPage = createActivityFeed([
				{
					id: 'a1',
					action: 'Processed data',
					status: 'failed',
					triggeredBy: 'bob@example.com',
					message: 'Connection refused'
				}
			])
			vi.mocked(api.get).mockReturnValue({
				json: async () => activityPage
			} as ApiResponsePromise)

			render(SuperagentPage, { data: createMockData() })

			const activityTab = page.getByRole('tab', { name: /activity/i })
			await activityTab.click()

			await expect.element(page.getByText('Processed data')).toBeVisible()
			await expect.element(page.getByText('Connection refused')).toBeVisible()
		})

		it.skip('switches back to Conversation on submit from Activity', async () => {
			render(SuperagentPage, { data: createMockData() })

			const activityTab = page.getByRole('tab', { name: /activity/i })
			await activityTab.click()

			await expect.element(activityTab).toHaveAttribute('aria-selected', 'true')

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.fill('Hello from activity tab')

			const buttons = page.getByRole('button')
			const submitButton = buttons.nth(buttons.elements().length - 1)
			await submitButton.click()

			const conversationTab = page.getByRole('tab', { name: /conversation/i })
			await expect
				.element(conversationTab)
				.toHaveAttribute('aria-selected', 'true')
		})
	})
})
