import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatRelativeTime } from '#lib/utils/date'

describe('formatRelativeTime', () => {
	beforeEach(() => {
		vi.useFakeTimers()
		vi.setSystemTime(new Date('2024-01-15T12:00:00Z'))
	})

	afterEach(() => {
		vi.useRealTimers()
	})

	describe('less than 1 minute ago', () => {
		it('returns "Just now" for very recent times', () => {
			const result = formatRelativeTime('2024-01-15T11:59:30Z')
			expect(result).toBe('Just now')
		})

		it('returns "Just now" for 59 seconds ago', () => {
			const result = formatRelativeTime('2024-01-15T11:59:01Z')
			expect(result).toBe('Just now')
		})
	})

	describe('minutes ago', () => {
		it('returns "1 min ago" for 1 minute ago', () => {
			const result = formatRelativeTime('2024-01-15T11:59:00Z')
			expect(result).toBe('1 min ago')
		})

		it('returns "X min ago" for multiple minutes', () => {
			const result = formatRelativeTime('2024-01-15T11:45:00Z')
			expect(result).toBe('15 min ago')
		})

		it('returns "59 min ago" for 59 minutes', () => {
			const result = formatRelativeTime('2024-01-15T11:01:00Z')
			expect(result).toBe('59 min ago')
		})
	})

	describe('hours ago', () => {
		it('returns "1 hour ago" for 1 hour ago', () => {
			const result = formatRelativeTime('2024-01-15T11:00:00Z')
			expect(result).toBe('1 hour ago')
		})

		it('returns "X hours ago" for multiple hours', () => {
			const result = formatRelativeTime('2024-01-15T08:00:00Z')
			expect(result).toBe('4 hours ago')
		})

		it('returns "23 hours ago" for 23 hours', () => {
			const result = formatRelativeTime('2024-01-14T13:00:00Z')
			expect(result).toBe('23 hours ago')
		})
	})

	describe('days ago', () => {
		it('returns "1 day ago" for 1 day ago', () => {
			const result = formatRelativeTime('2024-01-14T12:00:00Z')
			expect(result).toBe('1 day ago')
		})

		it('returns "X days ago" for multiple days', () => {
			const result = formatRelativeTime('2024-01-10T12:00:00Z')
			expect(result).toBe('5 days ago')
		})

		it('handles many days ago', () => {
			const result = formatRelativeTime('2023-12-15T12:00:00Z')
			expect(result).toBe('31 days ago')
		})
	})

	describe('edge cases', () => {
		it('handles same second', () => {
			const result = formatRelativeTime('2024-01-15T12:00:00Z')
			expect(result).toBe('Just now')
		})

		it('handles dates with milliseconds', () => {
			const result = formatRelativeTime('2024-01-15T11:30:00.500Z')
			expect(result).toBe('29 min ago')
		})

		it('handles future dates gracefully', () => {
			const result = formatRelativeTime('2024-01-15T13:00:00Z')
			// Future dates will produce negative diffs, which floor to 0
			// The function should handle this without errors
			expect(result).toBeTruthy()
		})
	})
})
