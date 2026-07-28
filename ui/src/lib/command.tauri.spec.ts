import { mockIPC } from '@tauri-apps/api/mocks'
import { describe, expect, it } from 'vitest'
import { commands, unwrapCommand } from './command'

// End-to-end exercise of the tauri-specta command layer with Tauri's official
// mockIPC: validates that the typed bindings call invoke with the right
// command name + payload, that the Result envelope is unwrapped, and that
// backend rejections surface as thrown Errors. No Rust process involved.
// clearMocks() runs in vitest-setup.ts afterEach.

describe('commands.greet via mockIPC', () => {
	it('unwraps the greeting returned by the Rust greet command', async () => {
		const seen: Array<{ cmd: string; args: unknown }> = []
		mockIPC((cmd, args) => {
			seen.push({ cmd, args })
			if (cmd === 'greet') return `Hello, ${(args as { name: string }).name}!`
		})

		const result = await commands.greet('Ava')

		expect(seen).toEqual([{ cmd: 'greet', args: { name: 'Ava' } }])
		expect(unwrapCommand(result)).toBe('Hello, Ava!')
	})

	it('surfaces a backend rejection as a thrown Error', async () => {
		mockIPC(() => {
			throw 'name is empty'
		})

		const result = await commands.greet('')

		expect(result.status).toBe('error')
		expect(() => unwrapCommand(result)).toThrow('name is empty')
	})

	it('falls back to the generic message when the rejection has no string form', async () => {
		mockIPC(() => {
			throw { code: 42 }
		})

		const result = await commands.greet('Ava')

		expect(result.status).toBe('error')
		expect(() => unwrapCommand(result)).toThrow('Command failed')
	})
})
