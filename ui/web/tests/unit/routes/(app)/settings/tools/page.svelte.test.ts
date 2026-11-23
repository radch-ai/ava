import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import * as mcpService from '#lib/services/mcp'
import * as toolsService from '#lib/services/tools'
import type { McpServer, McpToolDefinitionResponse } from '#lib/types/mcp'
import type { Tool } from '#lib/types/tool'
import ToolsPage from '#routes/settings/tools/+page.svelte'
import { invalidateAll } from '$app/navigation'

// Mock the tool catalog + MCP server services — the page never hits
// the network.
vi.mock('#lib/services/tools', () => ({
	getTools: vi.fn(),
	toggleTool: vi.fn()
}))

vi.mock('#lib/services/mcp', () => ({
	getMcpToolDefinition: vi.fn(),
	getMcpServers: vi.fn(),
	createMcpServer: vi.fn(),
	updateMcpServer: vi.fn(),
	deleteMcpServer: vi.fn(),
	refreshMcpServer: vi.fn()
}))

// Mock SvelteKit navigation: the page invalidates after every mutation.
vi.mock('$app/navigation', () => ({
	invalidateAll: vi.fn()
}))

const ddg: Tool = {
	id: 'duckduckgo_search',
	name: 'DuckDuckGo Search',
	description: 'Searches the web and returns the top results.',
	enabled: true,
	kind: 'builtin'
}

// The notes server and its one tool, shared by every describe below:
// each block spreads the base and states only its own delta.
const notesServer: McpServer = {
	id: 'notes-mcp',
	name: 'Notes MCP',
	url: 'http://127.0.0.1:3333/mcp',
	headerKeys: ['Authorization'],
	enabled: true,
	toolCount: 1,
	catalogState: 'active',
	allowPrivateNetwork: true,
	allowPlaintext: true,
	lastError: 'transient: mcp server notes-mcp: initialize: connection refused'
}

const notesTool: Tool = {
	id: 'notes-mcp:search',
	name: 'search',
	description: 'Searches the notes server.',
	enabled: true,
	kind: 'mcp',
	serverId: 'notes-mcp'
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

	it('offers MCP server management with the inheritance note', async () => {
		renderPage([ddg])

		// Whitespace-tolerant: the copy wraps in the source template.
		await expect
			.element(
				page.getByText(
					/Disabling a tool globally blocks access for\s+every agent, whatever its own switch says\./
				)
			)
			.toBeVisible()
		await expect
			.element(page.getByRole('button', { name: 'Add MCP server' }))
			.toBeEnabled()
		// The copy states the inheritance rule: a global enable reaches
		// agents that did not opt out.
		await expect
			.element(page.getByText(/except agents that disabled\s+it\./))
			.toBeVisible()
	})

	it('renders a server as a collapsible row with count and diagnostics', async () => {
		renderPage([ddg, notesTool], null, [notesServer])

		await expect
			.element(page.getByRole('heading', { name: 'Notes MCP' }))
			.toBeVisible()
		await expect.element(page.getByText('MCP', { exact: true })).toBeVisible()
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
		vi.mocked(mcpService.updateMcpServer).mockResolvedValue({
			server: { ...notesServer, enabled: false },
			tools: []
		})
		renderPage([ddg], null, [notesServer])

		await page
			.getByRole('switch', { name: 'Enable Notes MCP globally' })
			.click()

		await vi.waitFor(() => {
			expect(mcpService.updateMcpServer).toHaveBeenCalledWith('notes-mcp', {
				enabled: false
			})
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

// Policy state the switches cannot express: a paused catalog leaves its
// tools visible but inert, and the definition inspector renders the
// stored definition with the authority behind it.
describe('MCP page policy statuses', () => {
	// A rejected generation: paused, no credentials, nothing to explain.
	const pausedServer: McpServer = {
		...notesServer,
		headerKeys: [],
		catalogState: 'paused',
		lastError: undefined
	}

	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	it('marks a paused catalog and blocks its nested tools', async () => {
		renderPage([ddg, notesTool], null, [pausedServer])

		await expect
			.element(
				page.getByText(
					/Catalog paused after a rejected refresh — refresh to retry/
				)
			)
			.toBeVisible()
		// Expanded, the nested tool stays visible but inert.
		await page.getByRole('heading', { name: 'Notes MCP' }).click()
		await expect
			.element(page.getByRole('switch', { name: 'Enable search globally' }))
			.toBeDisabled()
	})

	it('shows a stored definition with its endpoint authority', async () => {
		vi.mocked(mcpService.getMcpToolDefinition).mockResolvedValue({
			definitionJson:
				'{"name":"search","description":"Searches the notes server."}',
			endpoint: 'http://127.0.0.1:3333/mcp',
			hasCredentials: true,
			allowPlaintext: true,
			allowPrivateNetwork: true
		})
		renderPage([ddg, notesTool], null, [
			{ ...pausedServer, catalogState: 'active' }
		])

		await page.getByRole('heading', { name: 'Notes MCP' }).click()
		await page
			.getByRole('button', { name: 'Show definition of search' })
			.click()

		await vi.waitFor(() => {
			expect(mcpService.getMcpToolDefinition).toHaveBeenCalledWith(
				'notes-mcp',
				'notes-mcp:search'
			)
		})
		await expect
			.element(
				page.getByText(
					/http:\/\/127\.0\.0\.1:3333\/mcp · credentials: yes · plaintext: yes · private network: yes/
				)
			)
			.toBeVisible()
		await expect
			.element(page.getByText(/"description":"Searches the notes server\."/))
			.toBeVisible()
	})

	// A fetch that started before a refresh must not repopulate the
	// cache when it resolves after — the listing it served is dead.
	it('drops a definition fetch that resolves after the cache was cleared', async () => {
		const definition = {
			definitionJson: '{"name":"search"}',
			endpoint: 'http://127.0.0.1:3333/mcp',
			hasCredentials: false,
			allowPlaintext: true,
			allowPrivateNetwork: true
		} satisfies McpToolDefinitionResponse
		let resolveFirst!: (value: McpToolDefinitionResponse) => void
		const firstFetch = new Promise<McpToolDefinitionResponse>((resolve) => {
			resolveFirst = resolve
		})
		vi.mocked(mcpService.getMcpToolDefinition)
			.mockReturnValueOnce(firstFetch)
			.mockResolvedValueOnce(definition)
		vi.mocked(mcpService.refreshMcpServer).mockResolvedValue({
			server: { ...pausedServer, catalogState: 'active' },
			tools: []
		})
		renderPage([ddg, notesTool], null, [
			{ ...pausedServer, catalogState: 'active' }
		])

		// The fetch starts, then the refresh clears the cache under it.
		await page.getByRole('heading', { name: 'Notes MCP' }).click()
		await page
			.getByRole('button', { name: 'Show definition of search' })
			.click()
		await page.getByRole('button', { name: 'Refresh tools' }).click()
		// invalidateAll runs beside the clear in the refresh's finally,
		// so it firing proves the generation already bumped.
		await vi.waitFor(() => expect(invalidateAll).toHaveBeenCalled())
		resolveFirst({
			...definition,
			definitionJson: '{"name":"stale"}'
		})

		// A repopulated cache would answer from memory; the honest
		// page fetches again for the live listing.
		await page
			.getByRole('button', { name: 'Show definition of search' })
			.click()
		await vi.waitFor(() => {
			expect(mcpService.getMcpToolDefinition).toHaveBeenCalledTimes(2)
		})
		await expect.element(page.getByText(/"name":"search"/)).toBeVisible()
	})
})

// A discovery pass that leaves tools out reports them — a cosmetic skip
// is visible with its reason, never a silent drop.
describe('MCP & Tools page skipped tools', () => {
	// A healthy server, so the skips are the only thing to explain.
	const skipServer: McpServer = { ...notesServer, lastError: undefined }

	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	it('clears a shown skipped banner on the next clean refresh', async () => {
		vi.mocked(mcpService.refreshMcpServer)
			.mockResolvedValueOnce({
				server: skipServer,
				tools: [],
				skippedTools: [
					{
						name: 'oversized_discovery_tool_name_that_exceeds_the_provider_limit',
						reason:
							'model name "oversized_discovery_tool_name_that_exceeds_the_provider_limit" exceeds 64 characters'
					}
				]
			})
			.mockResolvedValueOnce({
				server: skipServer,
				tools: []
			})
		renderPage([ddg, notesTool], null, [skipServer])

		await page.getByRole('button', { name: 'Refresh tools' }).click()

		await expect.element(page.getByText('1 tool was skipped')).toBeVisible()

		// The next refresh reports nothing skipped — a stale banner
		// cannot survive a clean save.
		await page.getByRole('button', { name: 'Refresh tools' }).click()

		await expect
			.element(page.getByText(/were skipped|was skipped/))
			.not.toBeInTheDocument()
	})

	it('drops the skipped banner when the next refresh fails', async () => {
		vi.mocked(mcpService.refreshMcpServer)
			.mockResolvedValueOnce({
				server: skipServer,
				tools: [],
				skippedTools: [
					{
						name: 'oversized_discovery_tool_name_that_exceeds_the_provider_limit',
						reason:
							'model name "oversized_discovery_tool_name_that_exceeds_the_provider_limit" exceeds 64 characters'
					}
				]
			})
			.mockRejectedValueOnce(new Error('refresh refused'))
		renderPage([ddg, notesTool], null, [skipServer])

		await page.getByRole('button', { name: 'Refresh tools' }).click()
		await expect.element(page.getByText('1 tool was skipped')).toBeVisible()

		// A failed pass replaces the banner with its own error: the
		// stale skips do not survive it.
		await page.getByRole('button', { name: 'Refresh tools' }).click()

		await expect
			.element(page.getByText(/were skipped|was skipped/))
			.not.toBeInTheDocument()
		await expect.element(page.getByText('refresh refused')).toBeVisible()
	})

	it('renders no panel when a refresh reports nothing skipped', async () => {
		vi.mocked(mcpService.refreshMcpServer).mockResolvedValue({
			server: skipServer,
			tools: []
		})
		renderPage([ddg, notesTool], null, [skipServer])

		await page.getByRole('button', { name: 'Refresh tools' }).click()

		await expect
			.element(page.getByText(/were skipped|was skipped/))
			.not.toBeInTheDocument()
	})
})
