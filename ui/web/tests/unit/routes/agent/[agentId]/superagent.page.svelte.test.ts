import { afterEach, describe, expect, it, vi } from 'vitest'
import { page, userEvent } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import type {
	AgentSocketEvent,
	AgentSocketState,
	AgentThread
} from '#lib/services/agent'
import { SUPERAGENT_AGENT } from '#lib/types/agent'
import AgentPageTestWrapper from './AgentPageTestWrapper.svelte'

vi.mock('#lib/services/api', () => ({
	api: {
		// The shared model catalog the selector's store loads once.
		get: vi.fn((url: string) => ({
			json: async () => {
				if (url === 'models') {
					return {
						groups: [
							{
								providerName: 'OpenAI',
								models: [
									{ id: 'gpt-4o', providerId: 'p-1' },
									{ id: 'gpt-4o-mini', providerId: 'p-1' }
								]
							}
						]
					}
				}
				return {}
			}
		}))
	}
}))

const acceptedOp = { accepted: true, itemId: 'q-test' }
const socketMock = {
	sendPrompt: vi.fn((content: string) => {
		// The gateway's snapshot after admission shows the item
		// running: the page's user-row echo hangs off that
		// transition, exactly as the real server orders it.
		capturedOnEvent?.({
			type: 'queue_state',
			agent_id: '_superagent',
			data: {
				items: [],
				paused: false,
				active: { id: 'q-test', content, user_message_id: 'stored-u' }
			}
		})
		return Promise.resolve(acceptedOp)
	}),
	sendNow: vi.fn((_itemId: string, _selection: unknown) =>
		Promise.resolve(acceptedOp)
	),
	requestStatus: vi.fn(),
	sendInterrupt: vi.fn(),
	close: vi.fn()
}
let capturedOnEvent: ((event: AgentSocketEvent) => void) | undefined
let capturedOnState: ((state: AgentSocketState) => void) | undefined

vi.mock('#lib/services/agent/ws', () => ({
	createAgentSocket: vi.fn(
		(
			_agentId: string,
			onEvent: (event: unknown) => void,
			onState?: (state: unknown) => void
		) => {
			capturedOnEvent = onEvent as typeof capturedOnEvent
			capturedOnState = onState as typeof capturedOnState
			return socketMock
		}
	)
}))

import { type ApiResponsePromise, api } from '#lib/services/api'

function createThread(
	items: Array<{ id: string; role: 'user' | 'assistant'; content: string }>,
	opts: { nextCursor?: string | null } = {}
): AgentThread {
	return {
		items: items.map((item, index) => ({
			id: item.id,
			role: item.role,
			content: item.content,
			// The wire order: the first item is the oldest.
			createdAt: new Date(
				Date.now() - (items.length - 1 - index) * 60_000
			).toISOString()
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
		items: items.map((item, index) => ({
			id: item.id,
			createdAt: new Date(Date.now() - index * 60_000).toISOString(),
			action: item.action,
			status: item.status ?? 'completed',
			triggeredBy: item.triggeredBy ?? 'cron',
			message: item.message
		})),
		nextCursor: opts.nextCursor ?? null
	}
}

function createMockData(overrides: Record<string, unknown> = {}) {
	return {
		agent: SUPERAGENT_AGENT,
		activityFeed: { items: [], nextCursor: null },
		activityError: null,
		thread: null,
		threadError: null,
		...overrides
	}
}

describe('AgentPage (superagent branch)', () => {
	afterEach(async () => {
		cleanup()
		vi.clearAllMocks()
		localStorage.clear()
	})

	describe('initial rendering', () => {
		it('renders agent header with superagent name', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })

			await expect.element(page.getByText('Ava')).toBeVisible()
		})

		it('renders model selector', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })

			// role=combobox is nameFrom:author — its accessible name is empty, so
			// match the visible label via text content instead
			const modelButton = page.getByRole('combobox')
			await expect.element(modelButton).toBeVisible()
			await expect
				.element(modelButton)
				.toHaveTextContent(/Default|GPT|Claude|Gemini|Grok/i)
		})

		it('renders text area', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })

			const textarea = page.getByPlaceholder('Enter your query!')
			await expect.element(textarea).toBeVisible()
		})

		it('uses a breakout footer with a centered composer and right-aligned model selector', async () => {
			const { container } = render(AgentPageTestWrapper, {
				data: createMockData()
			})
			const footer = container.querySelector('footer')
			expect(footer).toBeInTheDocument()
			expect(footer?.className).not.toContain('content-container')

			// The composer column: full-width wrapper capping at the
			// content max width, selector row stacked above it.
			const composerColumn = footer?.querySelector(
				'.mx-auto.w-full.md\\:max-w-\\[calc\\(var\\(--content-max-width\\)\\+2rem\\)\\]'
			)
			expect(composerColumn).toBeInTheDocument()

			const modelSelectorRow = composerColumn?.querySelector(
				'.mr-3\\.25.flex.items-center.justify-end'
			)
			expect(modelSelectorRow).toBeInTheDocument()
		})
	})

	describe('thread messages from page data', () => {
		it('renders thread messages from page data', async () => {
			const threadPage = createThread([
				{ id: '1', role: 'assistant', content: 'Welcome back' },
				{ id: '2', role: 'user', content: 'Show me updates' }
			])

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

			await expect.element(page.getByText('Welcome back')).toBeVisible()
			await expect.element(page.getByText('Show me updates')).toBeVisible()
		})

		it('renders user messages with right alignment', async () => {
			const threadPage = createThread([
				{ id: '1', role: 'user', content: 'User message here' }
			])

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

			const message = page.getByText('User message here')
			await expect.element(message).toBeVisible()
		})

		it('renders assistant messages with left alignment', async () => {
			const threadPage = createThread([
				{ id: '1', role: 'assistant', content: 'Assistant reply here' }
			])

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

			const message = page.getByText('Assistant reply here')
			await expect.element(message).toBeVisible()
		})

		it('shows no messages placeholder when thread is empty', async () => {
			const threadPage = createThread([])

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

			await expect.element(page.getByText(/no messages yet/i)).toBeVisible()
		})
	})

	describe('load more functionality', () => {
		it('shows load more button above thread list when nextCursor exists', async () => {
			const threadPage = createThread(
				[
					{ id: '1', role: 'user', content: 'Oldest message' },
					{ id: '2', role: 'assistant', content: 'Middle message' },
					{ id: '3', role: 'user', content: 'Latest message' }
				],
				{ nextCursor: 'cursor-abc' }
			)

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

			const loadMoreButton = page.getByRole('button', { name: /load more/i })
			await expect.element(loadMoreButton).toBeVisible()
		})

		it('hides load more button when nextCursor is null', async () => {
			const threadPage = createThread([
				{ id: '1', role: 'user', content: 'Only message' }
			])

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

			await expect
				.element(page.getByRole('button', { name: /load more/i }))
				.not.toBeInTheDocument()
		})
	})

	describe('error handling', () => {
		it('shows inline error state when initial thread load fails', async () => {
			render(AgentPageTestWrapper, {
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
			render(AgentPageTestWrapper, {
				data: createMockData({
					thread: null,
					threadError: 'Failed to load conversation'
				})
			})

			await expect.element(page.getByText('Ava')).toBeVisible()
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

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

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

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

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

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

			await expect.element(page.getByText('Newer message')).toBeVisible()

			const loadMoreButton = page.getByRole('button', { name: /load more/i })
			await loadMoreButton.click()

			await expect.element(page.getByText('Oldest message')).toBeVisible()
			// The merged thread still reads oldest→newest, top to bottom.
			await vi.waitFor(() => {
				const texts = Array.from(
					container.querySelectorAll('div.flex.flex-col.gap-4 > *')
				).map((el) => (el.textContent ?? '').trim())
				expect(texts).toEqual([
					'Oldest message',
					'Old reply',
					'Newer message',
					'Newest reply'
				])
			})
		})
	})

	describe('tab switching', () => {
		it('shows conversation tab and activity tab', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })

			await expect
				.element(page.getByRole('tab', { name: /conversation/i }))
				.toBeVisible()
			await expect
				.element(page.getByRole('tab', { name: /activity/i }))
				.toBeVisible()
		})

		it('defaults to conversation tab selected', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })

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

			render(AgentPageTestWrapper, { data: createMockData() })

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

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

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

			render(AgentPageTestWrapper, { data: createMockData() })

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

			render(AgentPageTestWrapper, { data: createMockData() })

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

			render(AgentPageTestWrapper, { data: createMockData() })

			const activityTab = page.getByRole('tab', { name: /activity/i })
			await activityTab.click()

			await expect.element(page.getByText('Processed data')).toBeVisible()
			await expect.element(page.getByText('Connection refused')).toBeVisible()
		})
	})

	describe('composer submit over websocket', () => {
		it('sends the prompt on submit; the lane resync rides the live transition', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'hello over ws{Enter}')

			await vi.waitFor(() => {
				expect(socketMock.sendPrompt).toHaveBeenCalledWith(
					'hello over ws',
					null
				)
			})
			// Submit does not ask for lane status; going live does.
			expect(socketMock.requestStatus).not.toHaveBeenCalled()
			capturedOnState?.('live')
			await vi.waitFor(() => {
				expect(socketMock.requestStatus).toHaveBeenCalledTimes(1)
			})
		})

		it('renders live state from the socket state callback', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'connect me{Enter}')

			capturedOnState?.('live')

			await expect.element(page.getByText('connected')).toBeVisible()
		})

		it('renders reconnecting state and drops the stale lane', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'status please{Enter}')

			capturedOnState?.('live')
			capturedOnEvent?.({
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: false,
					reasoning: false,
					model: null,
					queue_len: 0,
					conn_count: 1
				}
			})
			await expect
				.element(page.getByText(/lane active · Default · queue 0/))
				.toBeVisible()

			capturedOnState?.('reconnecting')
			await expect.element(page.getByText('reconnecting…')).toBeVisible()
			await vi.waitFor(() => {
				expect(page.getByText(/lane active/).elements()).toHaveLength(0)
			})
		})

		it('renders lane status from the lane_status event', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'status please{Enter}')

			capturedOnState?.('live')
			capturedOnEvent?.({
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: false,
					reasoning: false,
					model: null,
					queue_len: 0,
					conn_count: 1
				}
			})

			await expect
				.element(page.getByText(/lane active · Default · queue 0/))
				.toBeVisible()
		})

		it('counts the send-now reservation in the lane strip', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'status please{Enter}')

			capturedOnState?.('live')
			capturedOnEvent?.({
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: true,
					reasoning: false,
					model: null,
					queue_len: 0,
					conn_count: 1
				}
			})

			// Waiting rows are pending plus the pinned reservation.
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [{ id: 'q-1', content: 'first waiting' }],
					next: { id: 'q-2', content: 'second waiting' },
					paused: false
				}
			})
			await expect.element(page.getByText(/queue 2/)).toBeVisible()

			// A stale lane_status count never lowers the projection.
			capturedOnEvent?.({
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: true,
					reasoning: false,
					model: null,
					queue_len: 1,
					conn_count: 1
				}
			})
			await expect.element(page.getByText(/queue 2/)).toBeVisible()

			// The claim empties the queue and the count follows.
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [],
					active: {
						id: 'q-2',
						content: 'second waiting',
						user_message_id: 'u-2'
					},
					paused: false
				}
			})
			await expect.element(page.getByText(/queue 0/)).toBeVisible()
		})

		it('echoes the prompt and streams the answer into the tail', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'stream me{Enter}')

			await expect.element(page.getByText('stream me')).toBeVisible()

			capturedOnEvent?.({
				type: 'message_chunk',
				agent_id: '_superagent',
				data: { id: 'msg-1', delta: 'hello ' }
			})
			capturedOnEvent?.({
				type: 'message_chunk',
				agent_id: '_superagent',
				data: { id: 'msg-1', delta: 'streamed' }
			})
			await expect.element(page.getByText('hello streamed')).toBeVisible()

			capturedOnEvent?.({
				type: 'message',
				agent_id: '_superagent',
				data: { id: 'msg-1', content: 'hello streamed world' }
			})
			await expect.element(page.getByText('hello streamed world')).toBeVisible()
		})

		it('shows the thinking marker only while the lane reports reasoning', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'think hard{Enter}')
			await expect.element(page.getByText('think hard')).toBeVisible()

			capturedOnEvent?.({
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: true,
					reasoning: true,
					model: null,
					queue_len: 0,
					conn_count: 1
				}
			})
			await expect.element(page.getByText('Thinking')).toBeVisible()

			capturedOnEvent?.({
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: true,
					reasoning: false,
					model: null,
					queue_len: 0,
					conn_count: 1
				}
			})
			await vi.waitFor(() => {
				expect(page.getByText('Thinking').elements()).toHaveLength(0)
			})
		})

		it('reconciles the stored thread when a turn settles', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => ({
					items: [
						{
							id: 'stored-u',
							role: 'user',
							content: 'recon me',
							createdAt: '2026-01-01T00:00:00.000Z'
						},
						{
							id: 'stored-a',
							role: 'assistant',
							content: 'stored answer',
							createdAt: '2026-01-01T00:00:01.000Z'
						}
					],
					nextCursor: null
				})
			} as ApiResponsePromise)

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData()
			})
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'recon me{Enter}')
			await expect.element(page.getByText('recon me')).toBeVisible()

			capturedOnEvent?.({
				type: 'message_chunk',
				agent_id: '_superagent',
				data: { id: 'msg-1', delta: 'stored ' }
			})
			capturedOnEvent?.({
				type: 'message',
				agent_id: '_superagent',
				data: { id: 'msg-1', content: 'stored answer' }
			})

			// The settle refetch pulls the persisted turn.
			await vi.waitFor(() => {
				expect(vi.mocked(api.get)).toHaveBeenCalledWith(
					expect.stringContaining('agent/_superagent/thread')
				)
			})
			// The stored page renders the turn; the live copies are
			// gone — nothing appears twice — and the settled turn sits at
			// the bottom, still oldest→newest.
			await vi.waitFor(() => {
				expect(page.getByText('recon me').elements()).toHaveLength(1)
				expect(page.getByText('stored answer').elements()).toHaveLength(1)
			})
			const texts = Array.from(
				container.querySelectorAll('div.flex.flex-col.gap-4 > *')
			).map((el) => (el.textContent ?? '').trim())
			const promptIdx = texts.findIndex((t) => t.includes('recon me'))
			const answerIdx = texts.findIndex((t) => t.includes('stored answer'))
			expect(answerIdx).toBeGreaterThan(promptIdx)
		})

		it('reconciles a tool turn: live tool line swaps for the persisted row', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => ({
					items: [
						{
							id: 'stored-u',
							role: 'user',
							content: 'search otters',
							createdAt: '2026-01-01T00:00:00.000Z'
						},
						{
							id: 'stored-t',
							role: 'tool',
							content:
								'{"name":"web_search","args":{"query":"otters"},"result":"\\"2 hits\\""}',
							createdAt: '2026-01-01T00:00:01.000Z'
						},
						{
							id: 'stored-a',
							role: 'assistant',
							content: 'found 2 hits',
							createdAt: '2026-01-01T00:00:02.000Z'
						}
					],
					nextCursor: null
				})
			} as ApiResponsePromise)

			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'search otters{Enter}')

			// The turn runs a tool before answering.
			capturedOnEvent?.({
				type: 'tool_call',
				agent_id: '_superagent',
				data: {
					id: 'call-1',
					turn_id: 'msg-1',
					name: 'web_search',
					args: '{"query":"otters"}'
				}
			})
			capturedOnEvent?.({
				type: 'tool_result',
				agent_id: '_superagent',
				data: { id: 'call-1', turn_id: 'msg-1', result: '"2 hits"' }
			})
			capturedOnEvent?.({
				type: 'message',
				agent_id: '_superagent',
				data: { id: 'msg-1', content: 'found 2 hits' }
			})

			// The settle refetch carries the persisted tool row; the
			// live tool line is gone — the name appears exactly once.
			await vi.waitFor(() => {
				expect(page.getByText(/web_search/).elements()).toHaveLength(1)
			})
		})

		it('reconciles a stored failure into one durable error row', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => ({
					items: [
						{
							id: 'stored-u',
							role: 'user',
							content: 'fail me',
							createdAt: '2026-01-01T00:00:00.000Z'
						},
						{
							id: 'stored-e',
							role: 'error',
							content: 'no default model set — pick a model in the composer',
							createdAt: '2026-01-01T00:00:01.000Z'
						}
					],
					nextCursor: null
				})
			} as ApiResponsePromise)
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'fail me{Enter}')

			capturedOnEvent?.({
				type: 'error',
				agent_id: '_superagent',
				data: {
					id: 'stored-e',
					turn_id: 'msg-1',
					message: 'no default model set — pick a model in the composer'
				}
			})

			// The stored row replaces the live copy: prompt and failure
			// each render exactly once.
			await vi.waitFor(() => {
				expect(
					page
						.getByText('no default model set — pick a model in the composer')
						.elements()
				).toHaveLength(1)
				expect(page.getByText('fail me').elements()).toHaveLength(1)
			})
		})

		it('keeps the live error through a failed refetch, retires it on the next settle', async () => {
			const failing = {
				json: async () => {
					throw new Error('refetch down')
				}
			} as unknown as ApiResponsePromise
			const recovered = {
				json: async () => ({
					items: [
						{
							id: 'stored-u',
							role: 'user',
							content: 'fail me',
							createdAt: '2026-01-01T00:00:00.000Z'
						},
						{
							id: 'stored-e',
							role: 'error',
							content: 'read provider p-1: no rows',
							createdAt: '2026-01-01T00:00:01.000Z'
						},
						{
							id: 'stored-a',
							role: 'assistant',
							content: 'later answer',
							createdAt: '2026-01-01T00:00:02.000Z'
						}
					],
					nextCursor: null
				})
			} as ApiResponsePromise

			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'fail me{Enter}')

			vi.mocked(api.get).mockReturnValueOnce(failing)
			capturedOnEvent?.({
				type: 'error',
				agent_id: '_superagent',
				data: {
					id: 'stored-e',
					turn_id: 'msg-1',
					message: 'read provider p-1: no rows'
				}
			})
			// The refetch fails: the live copy stays the truth.
			await expect
				.element(page.getByText('read provider p-1: no rows'))
				.toBeVisible()

			vi.mocked(api.get).mockReturnValue(recovered)
			capturedOnEvent?.({
				type: 'message',
				agent_id: '_superagent',
				data: { id: 'msg-2', content: 'later answer' }
			})
			// The next terminal event retries the pending stored error:
			// its row lands, the live copy retires — exactly one bubble.
			await vi.waitFor(() => {
				expect(
					page.getByText('read provider p-1: no rows').elements()
				).toHaveLength(1)
			})
		})

		it('renders repeated identical failures as distinct rows', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => ({
					items: [
						{
							id: 'u-1',
							role: 'user',
							content: 'fail me',
							createdAt: '2026-01-01T00:00:00.000Z'
						},
						{
							id: 'e-1',
							role: 'error',
							content: 'same failure',
							createdAt: '2026-01-01T00:00:01.000Z'
						},
						{
							id: 'e-2',
							role: 'error',
							content: 'same failure',
							createdAt: '2026-01-01T00:00:02.000Z'
						}
					],
					nextCursor: null
				})
			} as ApiResponsePromise)
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'fail me{Enter}')

			for (const id of ['e-1', 'e-2']) {
				capturedOnEvent?.({
					type: 'error',
					agent_id: '_superagent',
					data: { id, turn_id: 'msg-1', message: 'same failure' }
				})
			}

			await vi.waitFor(() => {
				expect(page.getByText('same failure').elements()).toHaveLength(2)
			})
		})

		it('keeps following the stream as chunks grow the answer', async () => {
			const scrollTo = vi.spyOn(HTMLElement.prototype, 'scrollTo')
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'follow me{Enter}')

			capturedOnEvent?.({
				type: 'message_chunk',
				agent_id: '_superagent',
				data: { id: 'msg-1', delta: 'first' }
			})
			await expect.element(page.getByText('first')).toBeVisible()

			// A chunk that grows the existing bubble must scroll again —
			// the follow cannot depend on the message count alone.
			scrollTo.mockClear()
			capturedOnEvent?.({
				type: 'message_chunk',
				agent_id: '_superagent',
				data: { id: 'msg-1', delta: ' second' }
			})
			await expect.element(page.getByText('first second')).toBeVisible()
			await vi.waitFor(() => {
				expect(scrollTo).toHaveBeenCalled()
			})
			scrollTo.mockRestore()
		})

		it('renders the stop control while the lane works and sends interrupt on click', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'long turn{Enter}')

			capturedOnEvent?.({
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: true,
					reasoning: false,
					model: null,
					queue_len: 0,
					conn_count: 1
				}
			})

			const stop = page.getByRole('button', { name: 'Stop' })
			await expect.element(stop).toBeVisible()
			await stop.click()
			await vi.waitFor(() => {
				expect(socketMock.sendInterrupt).toHaveBeenCalledTimes(1)
			})
		})

		it('returns the send button when working clears', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'long turn{Enter}')

			capturedOnEvent?.({
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: true,
					reasoning: false,
					model: null,
					queue_len: 0,
					conn_count: 1
				}
			})
			await expect
				.element(page.getByRole('button', { name: 'Stop' }))
				.toBeVisible()

			capturedOnEvent?.({
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: false,
					reasoning: false,
					model: null,
					queue_len: 0,
					conn_count: 1
				}
			})
			// The settle snapshot clears the claim, as the gateway
			// publishes it when the turn finishes.
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: { items: [], paused: false }
			})
			await expect
				.element(page.getByRole('button', { name: 'Stop' }))
				.not.toBeInTheDocument()
			await expect
				.element(page.getByPlaceholder('Enter your query!'))
				.toBeVisible()
		})
	})

	describe('queue surface', () => {
		it('renders pending items from snapshots, oldest first', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })

			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [
						{ id: 'q-1', content: 'first waiting' },
						{ id: 'q-2', content: 'second waiting' }
					],
					paused: false
				}
			})

			await expect.element(page.getByText('first waiting')).toBeVisible()
			await expect.element(page.getByText('second waiting')).toBeVisible()

			// The strip's queue count follows snapshots, not just
			// lane_status broadcasts.
			capturedOnState?.('live')
			capturedOnEvent?.({
				type: 'lane_status',
				agent_id: '_superagent',
				data: {
					active: true,
					working: false,
					reasoning: false,
					model: null,
					queue_len: 0,
					conn_count: 1
				}
			})
			await expect.element(page.getByText(/queue 2/)).toBeVisible()
		})

		it('targets the clicked item with send_now', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [
						{ id: 'q-1', content: 'first waiting' },
						{ id: 'q-2', content: 'second waiting' }
					],
					paused: false
				}
			})

			await page
				.getByRole('button', { name: /send immediately/i })
				.nth(1)
				.click()

			await vi.waitFor(() => {
				expect(socketMock.sendNow).toHaveBeenCalledWith('q-2', null)
			})
		})

		it('shows the paused label with waiting work and hides the block without it', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [{ id: 'q-1', content: 'waiting' }],
					paused: true
				}
			})

			await expect
				.element(
					page.getByText(/^Paused — choose Send immediately to continue\.$/)
				)
				.toBeVisible()

			// The queue block exists only with waiting work — an empty,
			// unpaused snapshot removes it whole, label included.
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: { items: [], paused: false }
			})
			await vi.waitFor(() => {
				expect(
					page
						.getByText(/^Paused — choose Send immediately to continue\.$/)
						.elements()
				).toHaveLength(0)
			})
		})

		it('replaces the projection wholesale on each snapshot', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [{ id: 'q-1', content: 'about to run' }],
					paused: false
				}
			})
			await expect.element(page.getByText('about to run')).toBeVisible()

			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [],
					paused: false,
					active: { id: 'q-1', content: 'about to run', user_message_id: 'u-1' }
				}
			})
			await vi.waitFor(() => {
				expect(page.getByText('about to run').elements()).toHaveLength(1)
			})
		})

		it('never re-flashes an executed item as waiting (stale snapshot replay)', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [{ id: 'q-1', content: 'claimed already' }],
					paused: false
				}
			})
			await expect.element(page.getByText('claimed already')).toBeVisible()

			// The claim snapshot arrives; the item executes and its
			// user row takes over the transcript.
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'q-1',
						content: 'claimed already',
						user_message_id: 'u-1'
					}
				}
			})
			// A stale pre-claim snapshot replays (reconnect backlog
			// order): it must not resurrect the waiting entry.
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [{ id: 'q-1', content: 'claimed already' }],
					paused: false
				}
			})

			await vi.waitFor(() => {
				expect(
					page.getByRole('list', { name: 'Pending prompts' }).elements()
				).toHaveLength(0)
			})
			await expect
				.element(page.getByText('claimed already', { exact: true }))
				.toBeVisible()
		})

		it('renders the reserved item as a pinned action-less next row', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [{ id: 'q-1', content: 'still waiting' }],
					next: { id: 'n-1', content: 'chosen next' },
					paused: false
				}
			})

			// The reservation stays visible, pinned above the waiting
			// items, with no send-now control of its own.
			await expect.element(page.getByText('chosen next')).toBeVisible()
			const reserved = await page.getByText('chosen next').element()
			const reservedRow = reserved.closest('li')
			expect(reservedRow?.querySelector('button')).toBeNull()

			// The claim snapshot retires the pinned row: the item
			// leaves the queue surface for its transcript user row.
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [{ id: 'q-1', content: 'still waiting' }],
					active: {
						id: 'n-1',
						content: 'chosen next',
						user_message_id: 'u-next'
					},
					paused: false
				}
			})
			const queueList = await page
				.getByRole('list', { name: 'Pending prompts' })
				.element()
			await vi.waitFor(() => {
				expect(queueList.textContent ?? '').not.toContain('chosen next')
			})
		})

		it('shows the user row the moment the claim snapshot arrives', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'queue me then run{Enter}')

			// The mock replays the claim-shaped snapshot the gateway
			// now publishes at claim time; the row exists before any
			// message chunk is fed.
			await expect.element(page.getByText('queue me then run')).toBeVisible()
		})

		it('swaps Send for Stop while a queue item executes, without lane_status', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'q-1',
						content: 'running',
						user_message_id: 'u-1'
					}
				}
			})

			await expect
				.element(page.getByRole('button', { name: /^Stop$/ }))
				.toBeVisible()
			await expect
				.element(page.getByRole('button', { name: /^Send$/ }))
				.not.toBeInTheDocument()
		})

		it('shows Send while an error-paused queue holds pending items', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				agent_id: '_superagent',
				data: {
					items: [{ id: 'q-1', content: 'held by pause' }],
					paused: true
				}
			})

			await expect
				.element(page.getByRole('button', { name: /^Send$/ }))
				.toBeVisible()
			await expect
				.element(page.getByRole('button', { name: /^Stop$/ }))
				.not.toBeInTheDocument()
		})
	})

	describe('model selector browser persistence', () => {
		const storageKey = 'ava.model._superagent'

		it('restores the saved selection on load', async () => {
			localStorage.setItem(
				storageKey,
				JSON.stringify({ id: 'gpt-4o', providerId: 'p-1' })
			)

			render(AgentPageTestWrapper, { data: createMockData() })

			await expect
				.element(page.getByRole('combobox'))
				.toHaveTextContent('gpt-4o')
		})

		it('persists the picked model', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })

			await page.getByRole('combobox').click()
			await page.getByRole('option', { name: 'gpt-4o-mini' }).click()

			expect(localStorage.getItem(storageKey)).toBe(
				JSON.stringify({ id: 'gpt-4o-mini', providerId: 'p-1' })
			)
		})

		it('picking Default clears the stored selection', async () => {
			localStorage.setItem(
				storageKey,
				JSON.stringify({ id: 'gpt-4o', providerId: 'p-1' })
			)
			render(AgentPageTestWrapper, { data: createMockData() })

			await page.getByRole('combobox').click()
			await page.getByRole('option', { name: 'Default' }).click()

			expect(localStorage.getItem(storageKey)).toBeNull()
			await expect
				.element(page.getByRole('combobox'))
				.toHaveTextContent('Default')
		})

		it('the selection rides the prompt frame', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			await page.getByRole('combobox').click()
			await page.getByRole('option', { name: 'gpt-4o-mini' }).click()

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'hello{Enter}')

			await vi.waitFor(() => {
				expect(socketMock.sendPrompt).toHaveBeenCalledWith('hello', {
					id: 'gpt-4o-mini',
					providerId: 'p-1'
				})
			})
		})
	})
})
