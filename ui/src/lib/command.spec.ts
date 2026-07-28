import { describe, expect, it } from 'vitest'
import { type CommandResult, unwrapCommand } from './command'

describe('unwrapCommand', () => {
	it('returns data when status is ok', () => {
		const result: CommandResult<string> = { status: 'ok', data: 'hi' }
		expect(unwrapCommand(result)).toBe('hi')
	})

	it('throws Error with message when error has a string message', () => {
		const result: CommandResult<string, { message: string }> = {
			status: 'error',
			error: { message: 'boom' }
		}
		expect(() => unwrapCommand(result)).toThrow('boom')
	})

	it('uses the raw string when error is a non-empty string', () => {
		const result: CommandResult<string, string> = {
			status: 'error',
			error: 'nope'
		}
		expect(() => unwrapCommand(result)).toThrow('nope')
	})

	it('falls back to a generic message for empty/error-shaped errors', () => {
		const empty: CommandResult<string, string> = { status: 'error', error: '' }
		const obj: CommandResult<string, { message: unknown }> = {
			status: 'error',
			error: { message: 42 }
		}
		expect(() => unwrapCommand(empty)).toThrow('Command failed')
		expect(() => unwrapCommand(obj)).toThrow('Command failed')
	})
})
