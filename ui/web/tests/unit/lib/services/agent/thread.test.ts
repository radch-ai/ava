import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('#lib/services/api', () => ({
	api: {
		get: vi.fn()
	}
}))

import {
	type AgentThread,
	getAgentThread,
	mergePastThread,
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
				json: async () => ({ items: [{ id: 1 }], nextCursor: null })
			} as ApiResponsePromise)

			await expect(getAgentThread('_superagent')).rejects.toThrow(
				'malformed message'
			)
		})
	})

	describe('mergePastThread', () => {
		it('prepends an older page above the current one (newest-first list)', () => {
			const current = createThread({
				items: [createThreadMessage({ id: 'msg-2' })],
				nextCursor: 'msg-2'
			})
			const older = createThread({
				items: [createThreadMessage({ id: 'msg-1' })],
				nextCursor: null
			})

			const merged = mergePastThread(current, older)

			expect(merged.items.map((message) => message.id)).toEqual([
				'msg-1',
				'msg-2'
			])
			expect(merged.nextCursor).toBeNull()
		})
	})
})
