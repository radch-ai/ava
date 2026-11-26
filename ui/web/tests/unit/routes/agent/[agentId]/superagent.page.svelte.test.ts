import { afterEach, describe, expect, it, vi } from 'vitest'
import { page, userEvent } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import type {
	AgentSocketEvent,
	AgentSocketState,
	AgentThread,
	LifecycleEvent
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
		// The gateway's claim snapshot after admission shows the item
		// running, and the start commits the user row into the thread:
		// the page's echo hangs off those, exactly as the server
		// orders them.
		capturedOnEvent?.({
			type: 'queue_state',
			data: {
				items: [],
				paused: false,
				active: {
					id: 'q-test',
					content,
					kind: 'prompt',
					user_message_id: 'stored-u'
				}
			}
		})
		sendLifecycle({
			kind: 'item_started',
			itemId: 'q-test',
			threadId: 'thread-1',
			userMessageId: 'stored-u',
			rows: [row('stored-u', 'user', content)]
		})
		return Promise.resolve(acceptedOp)
	}),
	sendNow: vi.fn((_itemId: string, _selection: unknown) =>
		Promise.resolve(acceptedOp)
	),
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
			// The fake lane skips the handshake's wire traffic but not
			// its result: the surface is live from the start, so the
			// composer's readiness gate opens exactly as it does after
			// a real sync_ready.
			onState?.('live')
			return socketMock
		}
	)
}))

import { type ApiResponsePromise, api } from '#lib/services/api'

/** One display row as the lifecycle wire carries it. */
function row(
	id: string,
	role: 'user' | 'assistant' | 'tool' | 'error',
	content: string
) {
	return { id, role, content, createdAt: '2026-11-21T00:00:00.000Z' }
}

let lifecyclePos = 0

/** Delivers one committed lifecycle batch: the events arrive inside a
 * single frame, the cursor advancing past them. */
function sendLifecycle(...events: Partial<LifecycleEvent>[]) {
	lifecyclePos += events.length
	capturedOnEvent?.({
		type: 'lifecycle',
		data: {
			cursor: `v1:test:${lifecyclePos}`,
			events: events.map((event, index) => ({
				pos: lifecyclePos - events.length + index + 1,
				batchEnd: index === events.length - 1,
				...event
			}))
		}
	})
}

/** A committed turn's terminal event: the rows ride the event, so the
 * display never refetches to learn them. */
function itemFinished(
	itemId: string,
	rows: Array<ReturnType<typeof row>>,
	overrides: Partial<LifecycleEvent> = {}
) {
	return {
		kind: 'item_finished',
		itemId,
		outcome: 'completed',
		threadId: 'thread-1',
		rows,
		...overrides
	}
}

/** A committed thread switch with its display divider. */
function threadChanged(to: string, from: string) {
	return {
		kind: 'thread_changed',
		itemId: `cmd-${to}`,
		threadId: to,
		previousThreadId: from,
		divider: {
			id: to,
			role: 'divider' as const,
			content: 'New session',
			createdAt: '2026-11-21T00:00:00.000Z'
		}
	}
}

/** One streamed piece of the running turn's answer. */
function sendChunk(id: string, delta: string, seq: number) {
	capturedOnEvent?.({
		type: 'message_chunk',
		data: { id, delta, seq }
	})
}

/** Ends the handshake: the runtime hand-off replaces the queue
 * projection and the live tail, and the socket goes live. */
function sendSyncReady(
	queue: {
		items?: Array<{ id: string; content: string; kind: string }>
		active?: {
			id: string
			content: string
			kind: string
			user_message_id?: string
		} | null
		next?: { id: string; content: string; kind: string }
		paused?: boolean
	} = {},
	prefix?: { item_id: string; text: string; last_seq: number }
) {
	lifecyclePos += 1
	capturedOnEvent?.({
		type: 'sync_ready',
		data: {
			cursor: `v1:test:${lifecyclePos}`,
			queue: {
				items: queue.items ?? [],
				paused: queue.paused ?? false,
				active: queue.active ?? undefined,
				next: queue.next
			},
			lane: {
				active: false,
				working: false,
				compacting: false,
				reasoning: false,
				model: ''
			},
			prefix
		}
	})
	capturedOnState?.('live')
}

function createThread(
	items: Array<{
		id: string
		role: 'user' | 'assistant' | 'divider'
		content: string
	}>,
	opts: { nextCursor?: string | null; threadId?: string } = {}
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
		nextCursor: opts.nextCursor ?? null,
		threadId: opts.threadId ?? 'thread-1'
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

/** The transcript's top-level rows, in display order. */
function rowTexts(container: HTMLElement): string[] {
	return Array.from(
		container.querySelectorAll('div.flex.flex-col.gap-4 > *')
	).map((el) => (el.textContent ?? '').trim())
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

		it('keeps the displayed copy when a load-more page repeats it', async () => {
			// A thread already on screen must keep its own row: the
			// incoming page's copy of the same key never renders.
			const olderPage = createThread(
				[
					{ id: '2', role: 'user', content: 'Oldest message' },
					{ id: '3', role: 'user', content: 'server copy of the newer row' }
				],
				{ nextCursor: null }
			)

			vi.mocked(api.get).mockReturnValue({
				json: async () => olderPage
			} as ApiResponsePromise)

			const threadPage = createThread(
				[{ id: '3', role: 'user', content: 'Newer message' }],
				{ nextCursor: 'thread-1:3' }
			)

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})

			await expect.element(page.getByText('Newer message')).toBeVisible()

			await page.getByRole('button', { name: /load more/i }).click()

			await vi.waitFor(() => {
				const texts = Array.from(
					container.querySelectorAll('div.flex.flex-col.gap-4 > *')
				).map((el) => (el.textContent ?? '').trim())
				expect(texts).toEqual(['Oldest message', 'Newer message'])
			})
			await expect
				.element(page.getByText('server copy of the newer row'))
				.not.toBeInTheDocument()
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
		it('sends the prompt on submit; going live asks the gateway for nothing', async () => {
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
			// The lane needs no resync request anymore: readiness is the
			// sync hand-off itself, so going live only renders.
			capturedOnState?.('live')
			await expect.element(page.getByText('connected')).toBeVisible()
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
				data: {
					active: true,
					working: false,
					compacting: false,
					reasoning: false,
					model: ''
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
				data: {
					active: true,
					working: false,
					compacting: false,
					reasoning: false,
					model: ''
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
				data: {
					active: true,
					working: true,
					compacting: false,
					reasoning: false,
					model: ''
				}
			})

			// Waiting rows are pending plus the pinned reservation.
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [{ id: 'q-1', content: 'first waiting', kind: 'prompt' }],
					next: { id: 'q-2', content: 'second waiting', kind: 'prompt' },
					paused: false
				}
			})
			await expect.element(page.getByText(/queue 2/)).toBeVisible()

			// A stale lane_status count never lowers the projection.
			capturedOnEvent?.({
				type: 'lane_status',
				data: {
					active: true,
					working: true,
					compacting: false,
					reasoning: false,
					model: ''
				}
			})
			await expect.element(page.getByText(/queue 2/)).toBeVisible()

			// The claim empties the queue and the count follows.
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					active: {
						id: 'q-2',
						content: 'second waiting',
						kind: 'prompt',
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

			sendChunk('q-test', 'hello ', 1)
			sendChunk('q-test', 'streamed', 2)
			await expect.element(page.getByText('hello streamed')).toBeVisible()

			// The turn commits: its rows ride the terminal event, and the
			// streamed copy gives way to the committed one.
			sendLifecycle(
				itemFinished('q-test', [
					row('stored-u', 'user', 'stream me'),
					row('stored-a', 'assistant', 'hello streamed world')
				])
			)
			await vi.waitFor(() => {
				expect(page.getByText('stream me').elements()).toHaveLength(1)
				expect(page.getByText('hello streamed world').elements()).toHaveLength(
					1
				)
			})
		})

		it('shows the thinking marker only while the lane reports reasoning', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'think hard{Enter}')
			await expect.element(page.getByText('think hard')).toBeVisible()

			capturedOnEvent?.({
				type: 'lane_status',
				data: {
					active: true,
					working: true,
					compacting: false,
					reasoning: true,
					model: ''
				}
			})
			await expect.element(page.getByText('Thinking')).toBeVisible()

			capturedOnEvent?.({
				type: 'lane_status',
				data: {
					active: true,
					working: true,
					compacting: false,
					reasoning: false,
					model: ''
				}
			})
			await vi.waitFor(() => {
				expect(page.getByText('Thinking').elements()).toHaveLength(0)
			})
		})

		it('places the committed rows when a turn settles — no refetch', async () => {
			const { container } = render(AgentPageTestWrapper, {
				data: createMockData()
			})
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'recon me{Enter}')
			await expect.element(page.getByText('recon me')).toBeVisible()

			sendChunk('q-test', 'stored ', 1)

			// The terminal event carries the persisted turn: the rows
			// ride it, so the display never refetches.
			sendLifecycle(
				itemFinished('q-test', [
					row('stored-u', 'user', 'recon me'),
					row('stored-a', 'assistant', 'stored answer')
				])
			)

			const urls = vi.mocked(api.get).mock.calls.map(([url]) => String(url))
			expect(urls.filter((url) => url.includes('thread?'))).toEqual([])
			// The stored rows render the turn; the live copies are gone —
			// nothing appears twice — and the settled turn sits at the
			// bottom, still oldest→newest.
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

		it('settles a tool turn: committed rounds ride their own events', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'search otters{Enter}')

			// The turn runs a tool before answering: one invocation,
			// keyed by its server id from start to finish.
			sendLifecycle({
				kind: 'tool_started',
				itemId: 'q-test',
				invocationId: 'inv-1',
				callId: 'call-1',
				toolName: 'web_search',
				toolArgs: '{"query":"otters"}'
			})
			// The gateway shape: tool_finished carries the committed
			// tool row itself, and the live invocation line retires.
			sendLifecycle({
				kind: 'tool_finished',
				itemId: 'q-test',
				invocationId: 'inv-1',
				threadId: 'thread-1',
				toolResult: '"2 hits"',
				rows: [
					row(
						'stored-t',
						'tool',
						'{"name":"web_search","args":{"query":"otters"},"result":"\\"2 hits\\""}'
					)
				]
			})
			await vi.waitFor(() => {
				expect(page.getByText(/web_search/).elements()).toHaveLength(1)
			})

			// A mid-run assistant round commits before the terminal one:
			// its row rides message_committed and must survive
			// settlement without reload or duplication.
			sendLifecycle({
				kind: 'message_committed',
				itemId: 'q-test',
				threadId: 'thread-1',
				rows: [row('stored-r1', 'assistant', 'round one text')]
			})
			await vi.waitFor(() => {
				expect(page.getByText('round one text').elements()).toHaveLength(1)
			})

			// The terminal event carries only the terminal rows; the
			// committed tool and mid-run rows are already placed.
			sendLifecycle(
				itemFinished('q-test', [
					row('stored-u', 'user', 'search otters'),
					row('stored-a', 'assistant', 'found 2 hits')
				])
			)
			await vi.waitFor(() => {
				expect(page.getByText(/web_search/).elements()).toHaveLength(1)
				expect(page.getByText('round one text').elements()).toHaveLength(1)
				expect(page.getByText('found 2 hits').elements()).toHaveLength(1)
			})
		})

		it('settles a stored failure into one durable error row', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'fail me{Enter}')

			// The failure commits durably: its row rides the terminal
			// event instead of a transient wire error.
			sendLifecycle(
				itemFinished(
					'q-test',
					[
						row('stored-u', 'user', 'fail me'),
						row(
							'stored-e',
							'error',
							'no default model set — pick a model in the composer'
						)
					],
					{ outcome: 'failed' }
				)
			)

			// Prompt and failure each render exactly once.
			await vi.waitFor(() => {
				expect(
					page
						.getByText('no default model set — pick a model in the composer')
						.elements()
				).toHaveLength(1)
				expect(page.getByText('fail me').elements()).toHaveLength(1)
			})
		})

		it('keeps a transient error bubble through a later turn’s settlement', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'fail me{Enter}')

			// The journal write failed: the failure surfaces as a
			// transient bubble only — no durable row exists for a later
			// settle to carry or retire.
			capturedOnEvent?.({
				type: 'error',
				data: {
					turn_id: 'q-test',
					message: 'read provider p-1: no rows'
				}
			})
			await expect
				.element(page.getByText('read provider p-1: no rows'))
				.toBeVisible()

			// A later turn settles from its event; the uncovered bubble
			// stays visible alongside the new answer.
			sendLifecycle(
				itemFinished('turn-2', [row('stored-a', 'assistant', 'later answer')])
			)
			await vi.waitFor(() => {
				expect(page.getByText('later answer').elements()).toHaveLength(1)
			})
			await expect
				.element(page.getByText('read provider p-1: no rows'))
				.toBeVisible()
		})

		it('renders repeated identical failures as distinct rows', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'fail me{Enter}')

			// Two turns fail with the same message: each commits its own
			// row under its own id.
			sendLifecycle(
				itemFinished(
					'turn-1',
					[row('u-1', 'user', 'fail me'), row('e-1', 'error', 'same failure')],
					{ outcome: 'failed' }
				),
				itemFinished(
					'turn-2',
					[row('u-2', 'user', 'fail me'), row('e-2', 'error', 'same failure')],
					{ outcome: 'failed' }
				)
			)

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

			sendChunk('q-test', 'first', 1)
			await expect.element(page.getByText('first')).toBeVisible()

			// A chunk that grows the existing bubble must scroll again —
			// the follow cannot depend on the message count alone.
			scrollTo.mockClear()
			sendChunk('q-test', ' second', 2)
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
				data: {
					active: true,
					working: true,
					compacting: false,
					reasoning: false,
					model: ''
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
				data: {
					active: true,
					working: true,
					compacting: false,
					reasoning: false,
					model: ''
				}
			})
			await expect
				.element(page.getByRole('button', { name: 'Stop' }))
				.toBeVisible()

			capturedOnEvent?.({
				type: 'lane_status',
				data: {
					active: true,
					working: false,
					compacting: false,
					reasoning: false,
					model: ''
				}
			})
			// The settle snapshot clears the claim, as the gateway
			// publishes it when the turn finishes.
			capturedOnEvent?.({
				type: 'queue_state',
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
				data: {
					items: [
						{ id: 'q-1', content: 'first waiting', kind: 'prompt' },
						{ id: 'q-2', content: 'second waiting', kind: 'prompt' }
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
				data: {
					active: true,
					working: false,
					compacting: false,
					reasoning: false,
					model: ''
				}
			})
			await expect.element(page.getByText(/queue 2/)).toBeVisible()
		})

		it('targets the clicked item with send_now', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [
						{ id: 'q-1', content: 'first waiting', kind: 'prompt' },
						{ id: 'q-2', content: 'second waiting', kind: 'prompt' }
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
				data: {
					items: [{ id: 'q-1', content: 'waiting', kind: 'prompt' }],
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

		it('shows the auto-compacting chip and disables send-now while compacting', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [
						{ id: 'q-1', content: 'waiting behind compaction', kind: 'prompt' }
					],
					paused: false
				}
			})
			capturedOnState?.('live')
			capturedOnEvent?.({
				type: 'lane_status',
				data: {
					active: true,
					working: false,
					compacting: true,
					reasoning: false,
					model: ''
				}
			})

			await expect.element(page.getByText('Auto-compacting…')).toBeVisible()
			await expect
				.element(page.getByRole('button', { name: /send immediately/i }))
				.toBeDisabled()

			// The chip and the disable ride the flag: clearing it restores
			// the actions and drops the chip.
			capturedOnEvent?.({
				type: 'lane_status',
				data: {
					active: true,
					working: true,
					compacting: false,
					reasoning: false,
					model: ''
				}
			})
			await vi.waitFor(() => {
				expect(page.getByText('Auto-compacting…').elements()).toHaveLength(0)
			})
			await expect
				.element(page.getByRole('button', { name: /send immediately/i }))
				.toBeEnabled()
		})

		it('replaces the projection wholesale on each snapshot', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [{ id: 'q-1', content: 'about to run', kind: 'prompt' }],
					paused: false
				}
			})
			await expect.element(page.getByText('about to run')).toBeVisible()

			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'q-1',
						content: 'about to run',
						kind: 'prompt',
						user_message_id: 'u-1'
					}
				}
			})
			await vi.waitFor(() => {
				expect(page.getByText('about to run').elements()).toHaveLength(1)
			})
		})

		it('retires the waiting row when the claim snapshot arrives', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [{ id: 'q-1', content: 'claimed already', kind: 'prompt' }],
					paused: false
				}
			})
			await expect.element(page.getByText('claimed already')).toBeVisible()

			// The claim snapshot is the queue's whole truth: the item
			// leaves the waiting list for the running projection, and the
			// start commits its user row into the transcript.
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'q-1',
						content: 'claimed already',
						kind: 'prompt',
						user_message_id: 'u-1'
					}
				}
			})
			sendLifecycle({
				kind: 'item_started',
				itemId: 'q-1',
				threadId: 'thread-1',
				userMessageId: 'u-1',
				rows: [row('u-1', 'user', 'claimed already')]
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

		it('drops the previous agent’s projection on an in-place switch', async () => {
			const first = render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [
						{ id: 'q-1', content: 'belongs to the old lane', kind: 'prompt' }
					],
					paused: false
				}
			})
			capturedOnEvent?.({
				type: 'message_chunk',
				data: { id: 'msg-1', delta: 'old lane text', seq: 1 }
			})
			await expect
				.element(page.getByText('belongs to the old lane'))
				.toBeVisible()
			await expect.element(page.getByText('old lane text')).toBeVisible()

			// Away and back: the superagent surface remounts without any
			// frames, so anything still on screen is the previous lane's
			// leaked projection.
			await first.rerender({
				data: createMockData({
					agent: { id: 'other-agent', name: 'Other', icon: 'bot' }
				})
			})
			await first.rerender({ data: createMockData() })

			await vi.waitFor(() => {
				expect(
					page.getByRole('list', { name: 'Pending prompts' }).elements()
				).toHaveLength(0)
			})
			expect(page.getByText('belongs to the old lane').elements()).toHaveLength(
				0
			)
			expect(page.getByText('old lane text').elements()).toHaveLength(0)
		})

		it('renders the reserved item as a pinned action-less next row', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [{ id: 'q-1', content: 'still waiting', kind: 'prompt' }],
					next: { id: 'n-1', content: 'chosen next', kind: 'prompt' },
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
				data: {
					items: [{ id: 'q-1', content: 'still waiting', kind: 'prompt' }],
					active: {
						id: 'n-1',
						content: 'chosen next',
						kind: 'prompt',
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
				data: {
					items: [],
					paused: false,
					active: {
						id: 'q-1',
						content: 'running',
						kind: 'prompt',
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
				data: {
					items: [{ id: 'q-1', content: 'held by pause', kind: 'prompt' }],
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

		it('reduces replayed sync batches exactly like live events', async () => {
			render(AgentPageTestWrapper, {
				data: createMockData({ thread: createThread([]) })
			})

			// A reconnect replays what the device missed; the batch's events
			// go through the same reducer as live frames, so the committed
			// interruption notice lands in the thread.
			lifecyclePos += 1
			capturedOnEvent?.({
				type: 'sync_batch',
				data: {
					cursor: `v1:test:${lifecyclePos}`,
					events: [
						{
							pos: lifecyclePos,
							batchEnd: true,
							kind: 'item_finished',
							itemId: 'q-replayed',
							itemKind: 'prompt',
							threadId: 'thread-1',
							outcome: 'interrupted',
							reason: 'service_restart',
							rows: [
								{
									id: 'err-replayed',
									role: 'error',
									content: 'interrupted by service restart',
									createdAt: new Date().toISOString()
								}
							]
						}
					]
				}
			})

			await expect
				.element(page.getByText('interrupted by service restart'))
				.toBeVisible()
		})

		it('renders a replayed cancellation notice once, never a transcript row', async () => {
			// A cancelled item minted no row, so the event itself is the
			// notice: it names the submission that never ran, and a
			// repeated replay of the same event renders it once.
			render(AgentPageTestWrapper, {
				data: createMockData({
					thread: {
						threadId: 'thread-1',
						items: [row('msg-1', 'user', 'existing turn')],
						nextCursor: null
					}
				})
			})
			const deliver = () => {
				capturedOnEvent?.({
					type: 'sync_batch',
					data: {
						cursor: `v1:test:${lifecyclePos}`,
						events: [
							{
								pos: lifecyclePos,
								batchEnd: true,
								kind: 'item_finished',
								itemId: 'q-cancelled',
								itemKind: 'prompt',
								threadId: 'thread-1',
								outcome: 'cancelled',
								reason: 'service_restart',
								content: 'queued words that never ran'
							}
						]
					}
				})
			}
			deliver()
			deliver()

			await expect
				.element(
					page.getByText(
						'cancelled (service_restart): queued words that never ran'
					)
				)
				.toBeVisible()
			const all = [...document.querySelectorAll('*')].filter(
				(el) =>
					el.textContent ===
						'cancelled (service_restart): queued words that never ran' &&
					el.children.length === 0
			).length
			expect(all).toBe(1)
		})

		it('applies the sync hand-off: queue and running turn replace stale state', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [{ id: 'q-stale', content: 'stale waiting', kind: 'prompt' }],
					paused: false
				}
			})
			await expect.element(page.getByText('stale waiting')).toBeVisible()

			sendSyncReady(
				{
					active: {
						id: 'q-live',
						content: 'running prompt',
						kind: 'prompt',
						user_message_id: 'u-live'
					}
				},
				{ item_id: 'q-live', text: 'partial ', last_seq: 2 }
			)

			// The hand-off replaces the queue wholesale — the stale
			// waiting row is gone — and restores the running turn's
			// user row and streamed prefix.
			await vi.waitFor(() => {
				expect(page.getByText('stale waiting').elements()).toHaveLength(0)
			})
			await expect
				.element(page.getByText('running prompt', { exact: true }))
				.toBeVisible()
			await expect
				.element(page.getByText('partial', { exact: true }))
				.toBeVisible()

			// The restored stream continues after the prefix's last
			// sequence, and the turn keeps the composer busy.
			sendChunk('q-live', 'answer', 3)
			await expect.element(page.getByText('partial answer')).toBeVisible()
			await expect
				.element(page.getByRole('button', { name: 'Stop' }))
				.toBeVisible()
		})
	})

	describe('session commands', () => {
		it('renders the divider and follows the committed thread', async () => {
			const threadPage = createThread(
				[
					{ id: 'thread-1', role: 'divider', content: 'New session' },
					{ id: 'msg-1', role: 'user', content: 'first session' }
				],
				{ threadId: 'thread-1' }
			)

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})
			await expect.element(page.getByText('first session')).toBeVisible()

			// /new commits: the divider lands in the display at once.
			sendLifecycle(threadChanged('thread-2', 'thread-1'))
			await vi.waitFor(() => {
				expect(page.getByText('New session').elements()).toHaveLength(2)
			})

			// The next turn runs in the new thread: its start and settle
			// ride lifecycle events pinned to thread-2, landing below the
			// boundary — no refetch tells the display where they go.
			sendLifecycle(
				{
					kind: 'item_started',
					itemId: 'turn-2',
					threadId: 'thread-2',
					userMessageId: 'u-2',
					rows: [row('u-2', 'user', 'second session')]
				},
				itemFinished(
					'turn-2',
					[
						row('u-2', 'user', 'second session'),
						row('a-2', 'assistant', 'answer')
					],
					{ threadId: 'thread-2' }
				)
			)
			await vi.waitFor(() => {
				expect(page.getByText('second session').elements()).toHaveLength(1)
				expect(page.getByText('New session').elements()).toHaveLength(2)
				const texts = rowTexts(container)
				const firstIdx = texts.findIndex((t) => t.includes('first session'))
				const dividerIdx = texts
					.map((t) => t.includes('New session'))
					.lastIndexOf(true)
				const secondIdx = texts.findIndex((t) => t.includes('second session'))
				expect(dividerIdx).toBeGreaterThan(firstIdx)
				expect(secondIdx).toBeGreaterThan(dividerIdx)
			})
		})

		it('keeps a delayed old-thread settle above the newer divider', async () => {
			const threadPage = createThread(
				[
					{ id: 'm-1', role: 'user', content: 'old prompt' },
					{ id: 'm-2', role: 'assistant', content: 'old answer' }
				],
				{ threadId: 'thread-1' }
			)

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})
			await expect.element(page.getByText('old answer')).toBeVisible()

			// The old turn claims and starts in thread-1, then /new commits
			// while it runs, then its terminal event lands late.
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'turn-1',
						content: 'old prompt',
						kind: 'prompt',
						user_message_id: 'm-1'
					}
				}
			})
			sendLifecycle({
				kind: 'item_started',
				itemId: 'turn-1',
				threadId: 'thread-1',
				userMessageId: 'm-1',
				rows: [row('m-1', 'user', 'old prompt')]
			})
			sendLifecycle(threadChanged('thread-2', 'thread-1'))
			sendLifecycle(
				itemFinished('turn-1', [row('m-2', 'assistant', 'old answer')])
			)

			await vi.waitFor(() => {
				const texts = rowTexts(container)
				const dividerIdx = texts.findIndex((t) => t.includes('New session'))
				const oldIdx = texts.findIndex((t) => t.includes('old answer'))
				expect(oldIdx).toBeGreaterThanOrEqual(0)
				expect(dividerIdx).toBeGreaterThan(oldIdx)
			})
			// The late rows stayed pinned to the thread the turn ran in.
			expect(page.getByText('New session').elements()).toHaveLength(1)
			expect(page.getByText('old answer').elements()).toHaveLength(1)
		})

		it('mints no user row for a queued command and stays busy', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })

			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: { id: 'cmd-1', content: '/new', kind: 'new_thread' }
				}
			})

			await vi.waitFor(() => {
				// The command renders as a queue row, never as a prompt
				// in the transcript.
				expect(page.getByText('/new').elements()).toHaveLength(0)
			})
			await expect
				.element(page.getByRole('button', { name: 'Stop' }))
				.toBeVisible()
		})

		it('keeps the load-more cursor when a turn settles', async () => {
			const threadPage = createThread(
				[
					{ id: 'msg-1', role: 'user', content: 'older prompt' },
					{ id: 'msg-2', role: 'assistant', content: 'older answer' }
				],
				{ nextCursor: 'thread-2:msg-1' }
			)

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})
			await expect.element(page.getByText('older prompt')).toBeVisible()
			await expect
				.element(page.getByRole('button', { name: /load more/i }))
				.toBeVisible()

			sendLifecycle(
				itemFinished(
					'turn-2',
					[
						row('msg-3', 'user', 'newer prompt'),
						row('msg-4', 'assistant', 'newer answer')
					],
					{ threadId: 'thread-2' }
				)
			)

			await vi.waitFor(() => {
				expect(page.getByText('newer answer').elements()).toHaveLength(1)
			})
			// The reader's loaded depth survives the settle: the button
			// still offers the older page the display started with.
			await expect
				.element(page.getByRole('button', { name: /load more/i }))
				.toBeVisible()
			expect(page.getByText('older prompt').elements()).toHaveLength(1)
		})

		it('settles once from the event, even when the queue transition follows', async () => {
			render(AgentPageTestWrapper, { data: createMockData() })
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'turn-1',
						content: 'one prompt',
						kind: 'prompt',
						user_message_id: 'u-1'
					}
				}
			})
			sendLifecycle(
				{
					kind: 'item_started',
					itemId: 'turn-1',
					threadId: 'thread-1',
					userMessageId: 'u-1',
					rows: [row('u-1', 'user', 'one prompt')]
				},
				itemFinished('turn-1', [
					row('u-1', 'user', 'one prompt'),
					row('a-1', 'assistant', 'one answer')
				])
			)
			capturedOnEvent?.({
				type: 'queue_state',
				data: { items: [], paused: false }
			})

			await vi.waitFor(() => {
				expect(page.getByText('one answer').elements()).toHaveLength(1)
			})
			// The settle never refetches: only the model selector's
			// catalog request ever hits the API.
			const urls = vi.mocked(api.get).mock.calls.map(([url]) => String(url))
			expect(urls.filter((url) => url.includes('thread?'))).toEqual([])
		})

		it('keeps the divider above the new thread’s settled rows', async () => {
			const threadPage = createThread(
				[
					{ id: 'm-1', role: 'user', content: 'old prompt' },
					{ id: 'm-2', role: 'assistant', content: 'old answer' }
				],
				{ threadId: 'thread-1' }
			)

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})
			await expect.element(page.getByText('old answer')).toBeVisible()

			sendLifecycle(threadChanged('thread-2', 'thread-1'))
			// The new thread's turn settles from its event: its rows
			// carry no divider of their own — the boundary belongs to
			// the thread_changed event.
			sendLifecycle(
				itemFinished(
					'turn-2',
					[
						row('u-2', 'user', 'new prompt'),
						row('a-2', 'assistant', 'new answer')
					],
					{ threadId: 'thread-2' }
				)
			)

			await vi.waitFor(() => {
				expect(rowTexts(container).map((t) => t.split('\n')[0])).toEqual([
					'old prompt',
					'old answer',
					'New session',
					'new prompt',
					'new answer'
				])
			})
			expect(page.getByText('New session').elements()).toHaveLength(1)
		})

		it('keeps consecutive dividers in order above the newest thread’s rows', async () => {
			const threadPage = createThread(
				[
					{ id: 'm-1', role: 'user', content: 'old prompt' },
					{ id: 'm-2', role: 'assistant', content: 'old answer' }
				],
				{ threadId: 'thread-1' }
			)

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})
			await expect.element(page.getByText('old answer')).toBeVisible()

			// Both boundaries arrived as command events — repeated empty
			// sessions — so the turn's rows must land below both.
			sendLifecycle(threadChanged('thread-2', 'thread-1'))
			sendLifecycle(threadChanged('thread-3', 'thread-2'))
			// A prompt in the newest thread settles: its rows land below
			// both boundaries, which keep their order.
			sendLifecycle(
				itemFinished(
					'turn-3',
					[
						row('u-3', 'user', 'newest prompt'),
						row('a-3', 'assistant', 'newest answer')
					],
					{ threadId: 'thread-3' }
				)
			)

			await vi.waitFor(() => {
				expect(rowTexts(container).map((t) => t.split('\n')[0])).toEqual([
					'old prompt',
					'old answer',
					'New session',
					'New session',
					'newest prompt',
					'newest answer'
				])
			})
			expect(page.getByText('New session').elements()).toHaveLength(2)
		})

		it('keeps the loaded cursor when a turn adopts a new thread', async () => {
			const threadPage = createThread(
				[
					{ id: 'm-1', role: 'user', content: 'old prompt' },
					{ id: 'm-2', role: 'assistant', content: 'old answer' }
				],
				{ nextCursor: 'thread-1:m-1', threadId: 'thread-1' }
			)
			vi.mocked(api.get).mockReturnValue({
				json: async () => ({
					items: [
						{
							id: 'u-2',
							role: 'user',
							content: 'new prompt',
							createdAt: '2026-11-19T00:00:01.000Z'
						}
					],
					nextCursor: 'thread-1:',
					threadId: 'thread-2'
				})
			} as ApiResponsePromise)

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})
			await expect.element(page.getByText('old answer')).toBeVisible()

			sendLifecycle(
				itemFinished('turn-2', [row('u-2', 'user', 'new prompt')], {
					threadId: 'thread-2'
				})
			)
			await vi.waitFor(() => {
				expect(page.getByText('new prompt').elements()).toHaveLength(1)
			})

			// The reader was mid-pagination: the settle must not rewind
			// the position to the new thread's own continuation.
			await page.getByRole('button', { name: /load more/i }).click()
			await vi.waitFor(() => {
				const calls = vi.mocked(api.get).mock.calls
				expect(calls.at(-1)?.[1]).toMatchObject({
					searchParams: { cursor: 'thread-1:m-1' }
				})
			})
		})

		it('schedules no refresh for a command transition', async () => {
			const threadPage = createThread(
				[{ id: 'm-1', role: 'user', content: 'old prompt' }],
				{ threadId: 'thread-1' }
			)
			render(AgentPageTestWrapper, {
				data: createMockData({ thread: threadPage })
			})
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: { id: 'cmd-1', content: '/new', kind: 'new_thread' }
				}
			})
			sendLifecycle(threadChanged('thread-2', 'thread-1'))
			capturedOnEvent?.({
				type: 'queue_state',
				data: { items: [], paused: false }
			})

			// The command's artifact is the divider event; only the
			// model selector's catalog request ever hits the API.
			await vi.waitFor(() => {
				const urls = vi.mocked(api.get).mock.calls.map(([url]) => String(url))
				expect(urls.filter((url) => url.includes('thread?'))).toEqual([])
			})
			expect(page.getByText('New session').elements()).toHaveLength(1)
		})

		it('keeps a late old-thread settle above two newer boundaries', async () => {
			const seed = createThread(
				[{ id: 'm-1', role: 'user', content: 'old prompt' }],
				{ threadId: 'thread-1' }
			)

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData({ thread: seed })
			})
			await expect.element(page.getByText('old prompt')).toBeVisible()

			// The turn claims and starts in thread-1 …
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'turn-1',
						content: 'old prompt',
						kind: 'prompt',
						user_message_id: 'm-1'
					}
				}
			})
			sendLifecycle({
				kind: 'item_started',
				itemId: 'turn-1',
				threadId: 'thread-1',
				userMessageId: 'm-1',
				rows: [row('m-1', 'user', 'old prompt')]
			})
			// … two switches commit, then its terminal event lands late.
			sendLifecycle(threadChanged('thread-2', 'thread-1'))
			sendLifecycle(threadChanged('thread-3', 'thread-2'))
			sendLifecycle(
				itemFinished('turn-1', [row('m-2', 'assistant', 'late answer')])
			)

			// The late rows land inside thread-1's position — above both
			// boundaries — and the boundaries keep their chain order.
			await vi.waitFor(() => {
				expect(rowTexts(container)).toEqual([
					'old prompt',
					'late answer',
					'New session',
					'New session'
				])
			})
			expect(page.getByText('late answer').elements()).toHaveLength(1)

			// The next prompt runs in the thread the reader follows: its
			// rows land below both boundaries.
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'turn-2',
						content: 'next prompt',
						kind: 'prompt',
						user_message_id: 'u-9'
					}
				}
			})
			sendLifecycle(
				{
					kind: 'item_started',
					itemId: 'turn-2',
					threadId: 'thread-3',
					userMessageId: 'u-9',
					rows: [row('u-9', 'user', 'next prompt')]
				},
				itemFinished(
					'turn-2',
					[
						row('u-9', 'user', 'next prompt'),
						row('a-9', 'assistant', 'next answer')
					],
					{ threadId: 'thread-3' }
				)
			)
			await vi.waitFor(() => {
				expect(rowTexts(container)).toEqual([
					'old prompt',
					'late answer',
					'New session',
					'New session',
					'next prompt',
					'next answer'
				])
			})
		})

		it('lands a late middle-thread settle between its boundaries', async () => {
			const seed = createThread(
				[{ id: 'm-1', role: 'user', content: 'old prompt' }],
				{ threadId: 'thread-1' }
			)

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData({ thread: seed })
			})
			await expect.element(page.getByText('old prompt')).toBeVisible()

			// The switch into thread-2 commits, then the turn claims and
			// starts there …
			sendLifecycle(threadChanged('thread-2', 'thread-1'))
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'turn-1',
						content: 'ask T2',
						kind: 'prompt',
						user_message_id: 'm-2'
					}
				}
			})
			sendLifecycle({
				kind: 'item_started',
				itemId: 'turn-1',
				threadId: 'thread-2',
				userMessageId: 'm-2',
				rows: [row('m-2', 'user', 'ask T2')]
			})
			// … and the surface moves on to thread-3 before the turn's
			// terminal event arrives.
			sendLifecycle(threadChanged('thread-3', 'thread-2'))
			sendLifecycle(
				itemFinished(
					'turn-1',
					[
						row('m-2', 'user', 'ask T2'),
						row('m-3', 'assistant', 'late T2 answer')
					],
					{ threadId: 'thread-2' }
				)
			)

			// The late rows land inside thread-2's position — above the
			// thread-3 boundary, below thread-2's own.
			await vi.waitFor(() => {
				expect(rowTexts(container)).toEqual([
					'old prompt',
					'New session',
					'ask T2',
					'late T2 answer',
					'New session'
				])
			})
			expect(page.getByText('late T2 answer').elements()).toHaveLength(1)

			// The next prompt runs in the thread the reader follows: its
			// rows land below the newest boundary.
			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'turn-2',
						content: 'next prompt',
						kind: 'prompt',
						user_message_id: 'u-9'
					}
				}
			})
			sendLifecycle(
				{
					kind: 'item_started',
					itemId: 'turn-2',
					threadId: 'thread-3',
					userMessageId: 'u-9',
					rows: [row('u-9', 'user', 'next prompt')]
				},
				itemFinished(
					'turn-2',
					[
						row('u-9', 'user', 'next prompt'),
						row('a-9', 'assistant', 'next answer')
					],
					{ threadId: 'thread-3' }
				)
			)
			await vi.waitFor(() => {
				expect(rowTexts(container)).toEqual([
					'old prompt',
					'New session',
					'ask T2',
					'late T2 answer',
					'New session',
					'next prompt',
					'next answer'
				])
			})
		})

		it('keeps an exhausted oldest cursor exhausted across settles and commands', async () => {
			const seed = createThread(
				[{ id: 'm-1', role: 'user', content: 'only prompt' }],
				{ nextCursor: null, threadId: 'thread-1' }
			)

			render(AgentPageTestWrapper, {
				data: createMockData({ thread: seed })
			})
			await expect.element(page.getByText('only prompt')).toBeVisible()

			sendLifecycle(
				itemFinished('turn-1', [
					row('m-1', 'user', 'only prompt'),
					row('a-1', 'assistant', 'only answer')
				])
			)
			await vi.waitFor(() => {
				expect(page.getByText('only answer').elements()).toHaveLength(1)
			})
			await expect
				.element(page.getByRole('button', { name: /load more/i }))
				.not.toBeInTheDocument()

			sendLifecycle(threadChanged('thread-2', 'thread-1'))

			// Still exhausted: the command adds a boundary, not history.
			await expect.element(page.getByText('New session')).toBeVisible()
			await expect
				.element(page.getByRole('button', { name: /load more/i }))
				.not.toBeInTheDocument()
		})

		it('orders equal-timestamp boundaries by chain, not createdAt', async () => {
			const seed = createThread(
				[{ id: 'm-1', role: 'user', content: 'old prompt' }],
				{ threadId: 'thread-1' }
			)

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData({ thread: seed })
			})
			await expect.element(page.getByText('old prompt')).toBeVisible()

			capturedOnEvent?.({
				type: 'queue_state',
				data: {
					items: [],
					paused: false,
					active: {
						id: 'turn-1',
						content: 'old prompt',
						kind: 'prompt',
						user_message_id: 'm-1'
					}
				}
			})
			sendLifecycle({
				kind: 'item_started',
				itemId: 'turn-1',
				threadId: 'thread-1',
				userMessageId: 'm-1',
				rows: [row('m-1', 'user', 'old prompt')]
			})
			// Two switches whose boundaries carry the SAME timestamp: the
			// chain order is the only truth.
			sendLifecycle(threadChanged('thread-2', 'thread-1'))
			sendLifecycle(threadChanged('thread-3', 'thread-2'))
			// The older thread's terminal event lands afterwards.
			sendLifecycle(
				itemFinished('turn-1', [row('a-1', 'assistant', 'old answer')])
			)

			await vi.waitFor(() => {
				expect(rowTexts(container)).toEqual([
					'old prompt',
					'old answer',
					'New session',
					'New session'
				])
			})
			expect(page.getByText('old answer').elements()).toHaveLength(1)
		})

		it('freezes a persist-failed turn’s live rows above the boundary', async () => {
			const seed = createThread(
				[{ id: 'm-1', role: 'user', content: 'old prompt' }],
				{ threadId: 'thread-1' }
			)

			const { container } = render(AgentPageTestWrapper, {
				data: createMockData({ thread: seed })
			})
			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'ask A{Enter}')
			await expect.element(page.getByText('ask A')).toBeVisible()

			// The turn runs a tool and streams an answer in thread-1 —
			// then its journal write fails, so no terminal event will
			// ever commit rows for it. The queued /new still runs.
			sendLifecycle({
				kind: 'tool_started',
				itemId: 'q-test',
				invocationId: 'inv-1',
				callId: 'call-1',
				toolName: 'search',
				toolArgs: '{"q":"x"}'
			})
			sendChunk('q-test', 'answer ', 1)
			sendLifecycle(threadChanged('thread-2', 'thread-1'))

			// The tail is frozen above the boundary at once: one user
			// row, one tool line, one answer, then the boundary — the
			// live rows are all the turn will ever have.
			await vi.waitFor(() => {
				expect(rowTexts(container)).toHaveLength(5)
			})
			expect(rowTexts(container)[0]).toBe('old prompt')
			expect(rowTexts(container)[1]).toContain('ask A')
			expect(rowTexts(container)[2]).toContain('search')
			expect(rowTexts(container)[3]).toContain('answer')
			expect(rowTexts(container)[4]).toBe('New session')
			expect(page.getByText('ask A').elements()).toHaveLength(1)
			expect(page.getByText(/search/).elements()).toHaveLength(1)

			// The boundary does not swallow later committed work: the
			// next turn in the new thread settles from its event, below
			// the divider, still once each.
			sendLifecycle(
				itemFinished(
					'turn-2',
					[
						row('u-2', 'user', 'next prompt'),
						row('a-2', 'assistant', 'next answer')
					],
					{ threadId: 'thread-2' }
				)
			)
			await vi.waitFor(() => {
				expect(rowTexts(container)).toEqual([
					'old prompt',
					'ask A',
					'search {"q":"x"}',
					'answer',
					'New session',
					'next prompt',
					'next answer'
				])
			})
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

		it('forgets a stored selection the catalog no longer carries', async () => {
			localStorage.setItem(
				storageKey,
				JSON.stringify({ id: 'gone-model', providerId: 'nope' })
			)

			render(AgentPageTestWrapper, { data: createMockData() })

			// The phantom cannot be displayed or submitted: the
			// catalog check reverts the lane to default routing and
			// drops the stored pair.
			await vi.waitFor(() => {
				expect(localStorage.getItem(storageKey)).toBeNull()
			})
			await expect
				.element(page.getByRole('combobox'))
				.toHaveTextContent('Default')
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
