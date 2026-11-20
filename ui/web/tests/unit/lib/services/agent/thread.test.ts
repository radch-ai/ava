import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('#lib/services/api', () => ({
	api: {
		get: vi.fn()
	}
}))

import {
	type AgentThread,
	getAgentThread,
	type ThreadMessage
} from '#lib/services/agent'
import { type ApiResponsePromise, api } from '#lib/services/api'

function createThreadMessage(
	overrides?: Partial<ThreadMessage>
): ThreadMessage {
	return {
		id: 'msg-1',
		role: 'user',
		content: 'Hello',
		createdAt: '2026-01-01T00:00:00Z',
		...overrides
	}
}

function createThread(overrides?: Partial<AgentThread>): AgentThread {
	return {
		items: [createThreadMessage()],
		nextCursor: null,
		threadId: 'thread-1',
		...overrides
	}
}

describe('agent/thread service', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	describe('getAgentThread', () => {
		it('fetches the thread for an agent id', async () => {
			const thread = createThread()

			vi.mocked(api.get).mockReturnValue({
				json: async () => thread
			} as ApiResponsePromise)

			const result = await getAgentThread('_superagent')

			expect(api.get).toHaveBeenCalledWith('agent/_superagent/thread?limit=20')
			expect(result).toEqual(thread)
		})

		it('serves any agent id — the superagent is not special-cased', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => createThread()
			} as ApiResponsePromise)

			await getAgentThread('research')

			expect(api.get).toHaveBeenCalledWith('agent/research/thread?limit=20')
		})

		it('passes limit and cursor as query params', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => createThread()
			} as ApiResponsePromise)

			await getAgentThread('_superagent', 50, 'msg-9')

			expect(api.get).toHaveBeenCalledWith(
				'agent/_superagent/thread?limit=50&cursor=msg-9'
			)
		})

		it('rejects malformed thread payloads', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => ({ items: 'nope' })
			} as ApiResponsePromise)

			await expect(getAgentThread('_superagent')).rejects.toThrow(
				'Invalid thread'
			)
		})

		it('rejects malformed messages', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => ({
					items: [{ id: 1 }],
					nextCursor: null,
					threadId: 'thread-1'
				})
			} as ApiResponsePromise)

			await expect(getAgentThread('_superagent')).rejects.toThrow(
				'malformed message'
			)
		})

		it('accepts stored error rows', async () => {
			const thread = createThread({
				items: [
					createThreadMessage(),
					createThreadMessage({
						id: 'stored-e',
						role: 'error',
						content: 'no default model set'
					})
				]
			})

			vi.mocked(api.get).mockReturnValue({
				json: async () => thread
			} as ApiResponsePromise)

			await expect(getAgentThread('_superagent')).resolves.toEqual(thread)
		})

		it('accepts divider rows from the thread boundary', async () => {
			const thread = createThread({
				items: [
					createThreadMessage({
						id: 'thread-1',
						role: 'divider',
						content: 'New session'
					}),
					createThreadMessage({ id: 'msg-2', role: 'assistant' })
				]
			})

			vi.mocked(api.get).mockReturnValue({
				json: async () => thread
			} as ApiResponsePromise)

			await expect(getAgentThread('_superagent')).resolves.toEqual(thread)
		})

		it('requires the page to name its thread', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => ({ items: [], nextCursor: null })
			} as ApiResponsePromise)

			await expect(getAgentThread('_superagent')).rejects.toThrow(
				'threadId must be a string'
			)
		})

		it('still rejects roles outside the wire contract', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => ({
					items: [createThreadMessage({ role: 'system' as never })],
					nextCursor: null,
					threadId: 'thread-1'
				})
			} as ApiResponsePromise)

			await expect(getAgentThread('_superagent')).rejects.toThrow(
				'malformed message'
			)
		})

		it('serves the page as received — visibility is decided before paging', async () => {
			const thread = createThread({
				items: [
					createThreadMessage(),
					createThreadMessage({
						id: 'tool-row',
						role: 'tool',
						content: '{"name":"echo"}'
					}),
					createThreadMessage({
						id: 'answer-row',
						role: 'assistant',
						content: 'final answer'
					})
				]
			})

			vi.mocked(api.get).mockReturnValue({
				json: async () => thread
			} as ApiResponsePromise)

			const result = await getAgentThread('_superagent')
			expect(result.items.map((m) => m.id)).toEqual([
				'msg-1',
				'tool-row',
				'answer-row'
			])
		})
	})
})
