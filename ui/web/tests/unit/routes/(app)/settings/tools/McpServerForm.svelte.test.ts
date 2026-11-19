import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import * as mcpService from '#lib/services/mcp'
import type { McpServer } from '#lib/types/mcp'
import McpServerForm from '#routes/settings/tools/McpServerForm.svelte'

// The form never hits the network; the service layer is a spy.
vi.mock('#lib/services/mcp', () => ({
	createMcpServer: vi.fn(),
	updateMcpServer: vi.fn()
}))

const server: McpServer = {
	id: 'notes-mcp',
	name: 'Notes MCP',
	transport: 'stdio',
	command: 'notes-mcp',
	args: ['--stdio'],
	envKeys: ['NOTES_TOKEN'],
	enabled: true,
	toolCount: 2
}

function renderForm(oncancel = vi.fn()): {
	onsaved: ReturnType<typeof vi.fn>
	oncancel: ReturnType<typeof vi.fn>
} {
	const onsaved = vi.fn()
	render(McpServerForm, { server, onsaved, oncancel })
	return { onsaved, oncancel }
}

describe('McpServerForm edit patch', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	it('sends only the name for a rename — the connection fields stay absent', async () => {
		const { onsaved } = renderForm()
		const input = page.getByLabelText('Name')
		await input.fill('Notes MCP Renamed')
		await page.getByRole('button', { name: 'Save changes' }).click()

		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{ name: 'Notes MCP Renamed' }
		)
		expect(onsaved).toHaveBeenCalledOnce()
	})

	it('closes without a request when nothing changed', async () => {
		const { oncancel, onsaved } = renderForm()
		await page.getByRole('button', { name: 'Save changes' }).click()

		expect(mcpService.updateMcpServer).not.toHaveBeenCalled()
		expect(onsaved).not.toHaveBeenCalled()
		expect(oncancel).toHaveBeenCalledOnce()
	})

	it('replaces typed env and sends the empty map when the clear box is checked', async () => {
		renderForm()
		await page
			.getByLabelText('Environment — KEY: VALUE per line')
			.fill('NOTES_TOKEN: abc')
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{ env: { NOTES_TOKEN: 'abc' } }
		)

		vi.clearAllMocks()
		cleanup()
		renderForm()
		await page.getByRole('checkbox', { name: /Clear the 1 sealed/ }).click()
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{ env: {} }
		)
	})

	it('clears a stored timeout when the field is emptied', async () => {
		const onsaved = vi.fn()
		render(McpServerForm, {
			server: { ...server, timeoutMs: 30000 },
			onsaved,
			oncancel: vi.fn()
		})
		await page.getByLabelText('Per-call timeout (ms, optional)').fill('')
		await page.getByRole('button', { name: 'Save changes' }).click()
		expect(mcpService.updateMcpServer).toHaveBeenCalledExactlyOnceWith(
			'notes-mcp',
			{ clearTimeoutMs: true }
		)
	})

	// Out-of-range input is blocked before any request: the input's native
	// min/max plus the form's own range guard (mirroring the backend rule).
	it('never sends an out-of-range timeout', async () => {
		const { oncancel } = renderForm()
		const field = page.getByLabelText('Per-call timeout (ms, optional)')
		for (const value of ['0', '600001']) {
			await field.fill(value)
			await page.getByRole('button', { name: 'Save changes' }).click()
		}

		await vi.waitFor(() =>
			expect(mcpService.updateMcpServer).not.toHaveBeenCalled()
		)
		expect(oncancel).not.toHaveBeenCalled()
	})

	it('names malformed secret lines instead of dropping them', async () => {
		renderForm()
		await page
			.getByLabelText('Environment — KEY: VALUE per line')
			.fill('TOKEN=abc')
		await page.getByRole('button', { name: 'Save changes' }).click()

		expect(mcpService.updateMcpServer).not.toHaveBeenCalled()
		await expect
			.element(page.getByText(/Environment: "TOKEN=abc" needs "KEY: VALUE"/))
			.toBeInTheDocument()
	})
})
