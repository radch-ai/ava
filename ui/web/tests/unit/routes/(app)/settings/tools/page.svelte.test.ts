import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import * as toolsService from '#lib/services/tools'
import type { Tool } from '#lib/types/tool'
import ToolsPage from '#routes/settings/tools/+page.svelte'

// Mock the tool catalog service — the page never hits the network.
vi.mock('#lib/services/tools', () => ({
	getTools: vi.fn(),
	toggleTool: vi.fn()
}))

const ddg: Tool = {
	id: 'duckduckgo_search',
	name: 'DuckDuckGo Search',
	description: 'Searches the web and returns the top results.',
	builtin: true,
	enabled: true
}

// PageData folds in the (app) layout's load — agents fields arrive
// from there; this page never reads them, but the prop type needs
// them present.
function renderPage(tools: Tool[], toolsError: string | null = null): void {
	render(ToolsPage, {
		data: { agents: [], agentsError: null, tools, toolsError }
	})
}

describe('MCP & Tools page', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	it('lists the installed tool with its description and built-in badge', async () => {
		renderPage([ddg])

		await expect
			.element(page.getByRole('heading', { name: 'DuckDuckGo Search' }))
			.toBeVisible()
		await expect.element(page.getByText('Built-in')).toBeVisible()
		await expect
			.element(page.getByText('Searches the web and returns the top results.'))
			.toBeVisible()
	})

	it('explains the global-disable semantics and placeholder management', async () => {
		renderPage([ddg])

		// Whitespace-tolerant: the copy wraps in the source template.
		await expect
			.element(
				page.getByText(
					/Disabling a tool globally blocks access for all\s+agents without changing their preferences\./
				)
			)
			.toBeVisible()
		await expect
			.element(page.getByRole('button', { name: 'Add tool' }))
			.toBeDisabled()
		await expect
			.element(
				page.getByText(
					"Adding, editing, and removing custom tools and MCP connections isn't available yet."
				)
			)
			.toBeVisible()
	})

	it('saves a toggle through the service', async () => {
		vi.mocked(toolsService.toggleTool).mockResolvedValue({
			...ddg,
			enabled: false
		})
		renderPage([ddg])

		await page
			.getByRole('switch', { name: 'Enable DuckDuckGo Search globally' })
			.click()

		await vi.waitFor(() => {
			expect(toolsService.toggleTool).toHaveBeenCalledWith(
				'duckduckgo_search',
				{ enabled: false }
			)
		})
	})

	it('collapses rapid clicks into a no-op without firing a request', async () => {
		vi.useFakeTimers()
		try {
			renderPage([ddg])
			const sw = page.getByRole('switch', {
				name: 'Enable DuckDuckGo Search globally'
			})

			// Two synchronous clicks are the machine-speed burst:
			// both flips land inside one debounce window.
			// Let the mount effect seed the saved state before the burst.
			await vi.advanceTimersByTimeAsync(0)
			const handle = await sw.element()
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))

			// Past the debounce window: two flips cancel out — no
			// request, and the switch settles back on the saved state.
			await vi.advanceTimersByTimeAsync(300)
			expect(toolsService.toggleTool).not.toHaveBeenCalled()
			await expect.element(sw).toHaveAttribute('aria-checked', 'true')
		} finally {
			vi.useRealTimers()
		}
	})

	it('shows the load error instead of an empty catalog', async () => {
		renderPage([], 'Failed to load tools')

		await expect.element(page.getByText('Failed to load tools')).toBeVisible()
		// A failed read is not an empty catalog: the list shell,
		// including its empty-state copy, stays hidden.
		await expect
			.element(page.getByText('No tools available.'))
			.not.toBeInTheDocument()
	})

	it('flushes a pending toggle when the page unmounts', async () => {
		vi.useFakeTimers()
		try {
			vi.mocked(toolsService.toggleTool).mockResolvedValue({
				...ddg,
				enabled: false
			})
			renderPage([ddg])
			const sw = page.getByRole('switch', {
				name: 'Enable DuckDuckGo Search globally'
			})
			await vi.advanceTimersByTimeAsync(0)

			const handle = await sw.element()
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))
			// Navigation happens before the debounce would fire.
			cleanup()

			await vi.advanceTimersByTimeAsync(0)
			expect(toolsService.toggleTool).toHaveBeenCalledWith(
				'duckduckgo_search',
				{ enabled: false }
			)
		} finally {
			vi.useRealTimers()
		}
	})

	it('drops a burst that returned to the load state when the page unmounts', async () => {
		vi.useFakeTimers()
		try {
			renderPage([ddg])
			const sw = page.getByRole('switch', {
				name: 'Enable DuckDuckGo Search globally'
			})
			const handle = await sw.element()
			// Click-unclick, then navigate inside the window: the net
			// zero flushes nothing.
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))
			cleanup()

			await vi.advanceTimersByTimeAsync(0)
			expect(toolsService.toggleTool).not.toHaveBeenCalled()
		} finally {
			vi.useRealTimers()
		}
	})

	it('surfaces a failed save and keeps the last saved switch state', async () => {
		vi.mocked(toolsService.toggleTool).mockRejectedValue(
			new Error('tools store unreachable')
		)
		renderPage([ddg])

		await page
			.getByRole('switch', { name: 'Enable DuckDuckGo Search globally' })
			.click()

		await expect
			.element(page.getByText('tools store unreachable'))
			.toBeVisible()
		// The switch renders the saved state — still on, not the failed flip.
		await expect
			.element(
				page.getByRole('switch', { name: 'Enable DuckDuckGo Search globally' })
			)
			.toHaveAttribute('aria-checked', 'true')
	})
})
