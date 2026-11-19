import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import * as mcpService from '#lib/services/mcp'
import * as toolsService from '#lib/services/tools'
import type { McpServer } from '#lib/types/mcp'
import type { Tool } from '#lib/types/tool'
import ToolsPage from '#routes/settings/tools/+page.svelte'

// Mock the tool catalog + MCP server services — the page never hits
// the network.
vi.mock('#lib/services/tools', () => ({
	getTools: vi.fn(),
	toggleTool: vi.fn()
}))

vi.mock('#lib/services/mcp', () => ({
	getMcpServers: vi.fn(),
	createMcpServer: vi.fn(),
	updateMcpServer: vi.fn(),
	deleteMcpServer: vi.fn(),
	refreshMcpServer: vi.fn(),
	setMcpServerEnabled: vi.fn()
}))

// Mock SvelteKit navigation: the page invalidates after every mutation.
vi.mock('$app/navigation', () => ({
	invalidateAll: vi.fn()
}))

const ddg: Tool = {
	id: 'duckduckgo_search',
	name: 'DuckDuckGo Search',
	description: 'Searches the web and returns the top results.',
	builtin: true,
	enabled: true,
	kind: 'builtin'
}

// PageData folds in the (app) layout's load — agents fields arrive
// from there; this page never reads them, but the prop type needs
// them present.
function renderPage(
	tools: Tool[],
	toolsError: string | null = null,
	servers: McpServer[] = []
): void {
	render(ToolsPage, {
		data: {
			agents: [],
			agentsError: null,
			tools,
			toolsError,
			servers,
			serversError: null
		}
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
		await expect
			.element(page.getByText('Built-in', { exact: true }))
			.toBeVisible()
		await expect
			.element(page.getByText('Searches the web and returns the top results.'))
			.toBeVisible()
	})

	it('offers MCP server management with the built-ins-only note', async () => {
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
			.element(page.getByRole('button', { name: 'Add MCP server' }))
			.toBeEnabled()
		// The default-grant copy names built-ins only, covering wasm and
		// every future kind.
		await expect
			.element(
				page.getByText(
					/Only built-in tools are granted to agents by default — enable others per\s+agent/
				)
			)
			.toBeVisible()
	})

	const notesServer: McpServer = {
		id: 'notes-mcp',
		name: 'Notes MCP',
		transport: 'stdio',
		command: 'notes-mcp',
		args: ['--stdio'],
		envKeys: ['NOTES_TOKEN'],
		headerKeys: [],
		enabled: true,
		toolCount: 1,
		lastError: 'mcp server notes-mcp: initialize: connection refused'
	}

	const notesTool: Tool = {
		id: 'notes-mcp:search',
		name: 'search',
		description: 'Searches the notes server.',
		builtin: false,
		enabled: true,
		kind: 'mcp',
		serverId: 'notes-mcp'
	}

	it('renders a server as a collapsible row with transport, count, and diagnostics', async () => {
		renderPage([ddg, notesTool], null, [notesServer])

		await expect
			.element(page.getByRole('heading', { name: 'Notes MCP' }))
			.toBeVisible()
		await expect.element(page.getByText('MCP · stdio')).toBeVisible()
		await expect.element(page.getByText('1 tool')).toBeVisible()
		await expect.element(page.getByText(/connection refused/)).toBeVisible()
		await expect
			.element(page.getByRole('button', { name: 'Refresh tools' }))
			.toBeEnabled()
		// The MCP tool is nested, not a sibling flat row.
		await expect
			.element(page.getByRole('switch', { name: 'Enable search globally' }))
			.not.toBeInTheDocument()
	})

	it('expands a server to its nested tool switches', async () => {
		renderPage([ddg, notesTool], null, [notesServer])

		await page.getByRole('button', { name: 'Notes MCP' }).click()

		await expect
			.element(page.getByRole('switch', { name: 'Enable search globally' }))
			.toBeVisible()
	})

	it('saves the server switch through the service', async () => {
		vi.mocked(mcpService.setMcpServerEnabled).mockResolvedValue({
			server: { ...notesServer, enabled: false },
			tools: []
		})
		renderPage([ddg], null, [notesServer])

		await page
			.getByRole('switch', { name: 'Enable Notes MCP globally' })
			.click()

		await vi.waitFor(() => {
			expect(mcpService.setMcpServerEnabled).toHaveBeenCalledWith(
				'notes-mcp',
				false
			)
		})
	})

	it("hides a disabled server's nested rows but keeps its trigger", async () => {
		renderPage([ddg, notesTool], null, [{ ...notesServer, enabled: false }])

		await expect
			.element(page.getByRole('button', { name: 'Notes MCP' }))
			.toBeVisible()
		await expect
			.element(page.getByRole('switch', { name: 'Enable search globally' }))
			.not.toBeInTheDocument()
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
