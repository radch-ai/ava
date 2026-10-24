import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('$lib/services/api', () => ({
	api: {
		get: vi.fn()
	}
}))

import { type ApiResponsePromise, api } from '$lib/services/api'
import {
	getSuperagentThread,
	mergePastSuperagentThread,
	type SuperagentThread,
	type SuperagentThreadMessage
} from '$lib/services/user'

function createSuperagentThreadMessage(
	overrides?: Partial<SuperagentThreadMessage>
): SuperagentThreadMessage {
	return {
		id: 'msg-1',
		role: 'user',
		content: 'Hello',
		createdAt: '2024-01-01T00:00:00Z',
		...overrides
	}
}

function createSuperagentThread(
	overrides?: Partial<SuperagentThread>
): SuperagentThread {
	return {
		items: [createSuperagentThreadMessage()],
		nextCursor: null,
		...overrides
	}
}

describe('user/superagent-thread service', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	describe('getSuperagentThread', () => {
		it('fetches the superagent thread from the backend using camelCase', async () => {
			const backendPage = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: 'Hello',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => backendPage
			} as ApiResponsePromise)

			const result = await getSuperagentThread()

			expect(api.get).toHaveBeenCalledWith('user/superagent-thread?limit=20')
			expect(result).toEqual(createSuperagentThread())
		})

		it('passes cursor and limit through to the thread endpoint', async () => {
			const backendPage = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: 'Hello',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: 'next-cursor-123'
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => backendPage
			} as ApiResponsePromise)

			const result = await getSuperagentThread(50, 'cursor-abc')

			expect(api.get).toHaveBeenCalledWith(
				'user/superagent-thread?limit=50&cursor=cursor-abc'
			)
			expect(result.nextCursor).toBe('next-cursor-123')
		})

		it('throws when the thread request fails', async () => {
			const error = new Error('Network error')

			vi.mocked(api.get).mockReturnValue({
				json: async () => Promise.reject(error)
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow('Network error')
		})

		it('throws when any thread item is malformed', async () => {
			const malformedPage = {
				items: [{ id: 'msg-1', role: 'invalid-role' }],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when nextCursor is missing from the thread payload', async () => {
			const malformedPage = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: 'Hello',
						createdAt: '2024-01-01T00:00:00Z'
					}
				]
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when nextCursor is not a string or null in the thread payload', async () => {
			const malformedPage = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: 'Hello',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: 123
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when the thread payload uses old snake_case backend fields', async () => {
			const oldPayload = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: 'Hello',
						created_at: '2024-01-01T00:00:00Z'
					}
				],
				next_cursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => oldPayload
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})
	})

	describe('mergePastSuperagentThread', () => {
		it('prepends older thread items without duplicating ids', () => {
			const current: SuperagentThread = createSuperagentThread({
				items: [
					createSuperagentThreadMessage({
						id: 'msg-3',
						createdAt: '2024-01-03T00:00:00Z'
					}),
					createSuperagentThreadMessage({
						id: 'msg-4',
						createdAt: '2024-01-04T00:00:00Z'
					})
				],
				nextCursor: null
			})

			const older: SuperagentThread = createSuperagentThread({
				items: [
					createSuperagentThreadMessage({
						id: 'msg-1',
						createdAt: '2024-01-01T00:00:00Z'
					}),
					createSuperagentThreadMessage({
						id: 'msg-2',
						createdAt: '2024-01-02T00:00:00Z'
					}),
					createSuperagentThreadMessage({
						id: 'msg-3',
						createdAt: '2024-01-03T00:00:00Z'
					})
				],
				nextCursor: 'older-cursor'
			})

			const merged = mergePastSuperagentThread(current, older)

			expect(merged.items.map((i) => i.id)).toEqual([
				'msg-1',
				'msg-2',
				'msg-3',
				'msg-4'
			])
			expect(merged.nextCursor).toBe('older-cursor')
		})

		it('returns current page when older is empty', () => {
			const current: SuperagentThread = createSuperagentThread({
				items: [createSuperagentThreadMessage({ id: 'msg-1' })],
				nextCursor: null
			})

			const older: SuperagentThread = { items: [], nextCursor: null }

			const merged = mergePastSuperagentThread(current, older)

			expect(merged.items).toEqual(current.items)
			expect(merged.nextCursor).toBeNull()
		})

		it('returns older items when current is empty', () => {
			const current: SuperagentThread = { items: [], nextCursor: null }
			const older: SuperagentThread = createSuperagentThread({
				items: [createSuperagentThreadMessage({ id: 'msg-1' })],
				nextCursor: 'cursor-123'
			})

			const merged = mergePastSuperagentThread(current, older)

			expect(merged.items).toEqual(older.items)
			expect(merged.nextCursor).toBe('cursor-123')
		})

		it('handles both pages being empty', () => {
			const current: SuperagentThread = { items: [], nextCursor: null }
			const older: SuperagentThread = { items: [], nextCursor: null }

			const merged = mergePastSuperagentThread(current, older)

			expect(merged.items).toEqual([])
			expect(merged.nextCursor).toBeNull()
		})

		it('keeps past version when messages overlap', () => {
			const current: SuperagentThread = createSuperagentThread({
				items: [
					createSuperagentThreadMessage({
						id: 'msg-1',
						role: 'assistant',
						content: 'Updated content'
					})
				],
				nextCursor: null
			})

			const older: SuperagentThread = createSuperagentThread({
				items: [
					createSuperagentThreadMessage({
						id: 'msg-1',
						role: 'user',
						content: 'Original content'
					})
				],
				nextCursor: 'old-cursor'
			})

			const merged = mergePastSuperagentThread(current, older)

			expect(merged.items).toHaveLength(1)
			expect(merged.items[0]?.content).toBe('Original content')
			expect(merged.items[0]?.role).toBe('user')
		})

		it('handles messages with different roles', () => {
			const current: SuperagentThread = createSuperagentThread({
				items: [
					createSuperagentThreadMessage({ id: 'msg-3', role: 'user' }),
					createSuperagentThreadMessage({ id: 'msg-4', role: 'assistant' })
				],
				nextCursor: null
			})

			const older: SuperagentThread = createSuperagentThread({
				items: [
					createSuperagentThreadMessage({ id: 'msg-1', role: 'user' }),
					createSuperagentThreadMessage({ id: 'msg-2', role: 'assistant' })
				],
				nextCursor: 'cursor'
			})

			const merged = mergePastSuperagentThread(current, older)

			expect(merged.items.map((i) => i.role)).toEqual([
				'user',
				'assistant',
				'user',
				'assistant'
			])
		})
	})

	describe('getSuperagentThread - extensive scenarios', () => {
		it('handles empty items array', async () => {
			const emptyPage = {
				items: [],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => emptyPage
			} as ApiResponsePromise)

			const result = await getSuperagentThread()

			expect(result.items).toEqual([])
			expect(result.nextCursor).toBeNull()
		})

		it('handles all valid role values', async () => {
			for (const role of ['user', 'assistant'] as const) {
				const page = {
					items: [
						{
							id: `msg-${role}`,
							role,
							content: 'Test message',
							createdAt: '2024-01-01T00:00:00Z'
						}
					],
					nextCursor: null
				}

				vi.mocked(api.get).mockReturnValue({
					json: async () => page
				} as ApiResponsePromise)

				const result = await getSuperagentThread()

				expect(result.items[0]?.role).toBe(role)
			}
		})

		it('handles messages with special characters in content', async () => {
			const page = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: 'Hello! @user #hashtag https://example.com',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise)

			const result = await getSuperagentThread()

			expect(result.items[0]?.content).toBe(
				'Hello! @user #hashtag https://example.com'
			)
		})

		it('handles multiline content', async () => {
			const page = {
				items: [
					{
						id: 'msg-1',
						role: 'assistant',
						content: 'Line 1\nLine 2\nLine 3',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise)

			const result = await getSuperagentThread()

			expect(result.items[0]?.content).toContain('\n')
		})

		it('handles large limit values', async () => {
			const page = { items: [], nextCursor: null }

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise)

			await getSuperagentThread(500)

			expect(api.get).toHaveBeenCalledWith('user/superagent-thread?limit=500')
		})

		it('handles default limit parameter', async () => {
			const page = { items: [], nextCursor: null }

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise)

			await getSuperagentThread()

			expect(api.get).toHaveBeenCalledWith('user/superagent-thread?limit=20')
		})

		it('handles cursor parameter', async () => {
			const page = { items: [], nextCursor: 'next-page-cursor' }

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise)

			const result = await getSuperagentThread(20, 'my-cursor')

			expect(api.get).toHaveBeenCalledWith(
				'user/superagent-thread?limit=20&cursor=my-cursor'
			)
			expect(result.nextCursor).toBe('next-page-cursor')
		})

		it('handles ISO 8601 timestamps with different formats', async () => {
			const timestamps = [
				'2024-01-01T00:00:00Z',
				'2024-01-01T00:00:00.000Z',
				'2024-01-01T00:00:00+00:00',
				'2024-12-31T23:59:59.999Z'
			]

			for (const timestamp of timestamps) {
				const page = {
					items: [
						{
							id: 'msg-1',
							role: 'user',
							content: 'Test',
							createdAt: timestamp
						}
					],
					nextCursor: null
				}

				vi.mocked(api.get).mockReturnValue({
					json: async () => page
				} as ApiResponsePromise)

				const result = await getSuperagentThread()

				expect(result.items[0]?.createdAt).toBe(timestamp)
			}
		})

		it('throws on null items array', async () => {
			const malformedPage = {
				items: null,
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow(
				'items must be an array'
			)
		})

		it('throws on non-object items', async () => {
			const malformedPage = {
				items: ['not-an-object'],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when id field is missing', async () => {
			const malformedPage = {
				items: [
					{
						role: 'user',
						content: 'Hello',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when id is not a string', async () => {
			const malformedPage = {
				items: [
					{
						id: 123,
						role: 'user',
						content: 'Hello',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when role is missing', async () => {
			const malformedPage = {
				items: [
					{
						id: 'msg-1',
						content: 'Hello',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when role is invalid', async () => {
			const malformedPage = {
				items: [
					{
						id: 'msg-1',
						role: 'system',
						content: 'Hello',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when content is missing', async () => {
			const malformedPage = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when content is not a string', async () => {
			const malformedPage = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: 123,
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when createdAt is missing', async () => {
			const malformedPage = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: 'Hello'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws when createdAt is not a string', async () => {
			const malformedPage = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: 'Hello',
						createdAt: 123
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => malformedPage
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow()
		})

		it('throws on non-object payload', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => null
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow('expected an object')
		})

		it('throws on array payload', async () => {
			vi.mocked(api.get).mockReturnValue({
				json: async () => []
			} as ApiResponsePromise)

			await expect(getSuperagentThread()).rejects.toThrow(
				'items must be an array'
			)
		})

		it('handles complex cursor values', async () => {
			const page = {
				items: [],
				nextCursor:
					'eyJtc2dfaWQiOiIxMjM0NTY3ODkwIiwidGltZXN0YW1wIjoiMjAyNC0wMS0wMVQwMDowMDowMC4wMDBaIn0='
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise)

			const result = await getSuperagentThread()

			expect(result.nextCursor).toBe(
				'eyJtc2dfaWQiOiIxMjM0NTY3ODkwIiwidGltZXN0YW1wIjoiMjAyNC0wMS0wMVQwMDowMDowMC4wMDBaIn0='
			)
		})

		it('handles mixed role messages in thread', async () => {
			const page = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: 'Hello',
						createdAt: '2024-01-01T00:00:00Z'
					},
					{
						id: 'msg-2',
						role: 'assistant',
						content: 'Hi there!',
						createdAt: '2024-01-01T00:01:00Z'
					},
					{
						id: 'msg-3',
						role: 'user',
						content: 'How are you?',
						createdAt: '2024-01-01T00:02:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise)

			const result = await getSuperagentThread()

			expect(result.items).toHaveLength(3)
			expect(result.items[0]?.role).toBe('user')
			expect(result.items[1]?.role).toBe('assistant')
			expect(result.items[2]?.role).toBe('user')
		})

		it('handles empty content strings', async () => {
			const page = {
				items: [
					{
						id: 'msg-1',
						role: 'user',
						content: '',
						createdAt: '2024-01-01T00:00:00Z'
					}
				],
				nextCursor: null
			}

			vi.mocked(api.get).mockReturnValue({
				json: async () => page
			} as ApiResponsePromise)

			const result = await getSuperagentThread()

			expect(result.items[0]?.content).toBe('')
		})
	})
})
