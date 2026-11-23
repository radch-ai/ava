import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import ToolsEditor from '#components/agent-config/ToolsEditor.svelte'
import {
	getAgentTools,
	toggleAgentMcp,
	toggleAgentTool
} from '#lib/services/agent/config'
import { getMcpToolDefinition } from '#lib/services/mcp'
import type { AgentMcpPreference, AgentTool } from '#lib/types/tool'

// Mock the agent config + MCP services — the panel never hits the
// network. The MCP service stays in the mock on purpose: the editor must
// never call it (server facts ride the tool listing; definition
// inspection lives on the settings page).
vi.mock('#lib/services/agent/config', () => ({
	getAgentTools: vi.fn(),
	toggleAgentTool: vi.fn(),
	toggleAgentMcp: vi.fn()
}))

vi.mock('#lib/services/mcp', () => ({
	getMcpServers: vi.fn(),
	getMcpToolDefinition: vi.fn()
}))

const available: AgentTool = {
	id: 'duckduckgo_search',
	name: 'DuckDuckGo Search',
	description: 'Searches the web and returns the top results.',
	globalEnabled: true,
	locallyDisabled: false,
	kind: 'builtin'
}

const blocked: AgentTool = {
	...available,
	id: 'ghost_tool',
	name: 'Ghost Tool',
	globalEnabled: false,
	kind: 'builtin'
}

// Mocks are set before render — the fetch effect runs on mount. One
// listing supplies the tools and every server fact the panel renders.
function renderEditor(
	tools: AgentTool[] = [available],
	agentId = 'research',
	servers: AgentMcpPreference[] = []
) {
	vi.mocked(getAgentTools).mockResolvedValue({ tools, servers })
	render(ToolsEditor, { agentId, value: '', onchange: () => {} })
	return page.getByRole('switch', {
		name: 'Enable DuckDuckGo Search for this agent'
	})
}

const notesServer: AgentMcpPreference = {
	serverId: 'notes-mcp',
	name: 'Notes MCP',
	agentEnabled: false,
	serverEnabled: true,
	catalogState: 'active'
}

const notesTool: AgentTool = {
	id: 'notes-mcp:search',
	name: 'search',
	description: 'Searches the notes server.',
	globalEnabled: true,
	locallyDisabled: false,
	kind: 'mcp',
	serverId: 'notes-mcp'
}

describe('ToolsEditor', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	it('keeps the context textarea above the live tool list', async () => {
		renderEditor()

		await expect
			.element(
				page.getByPlaceholder(
					'E.g., "Calendar: Google Calendar. CRM: HubSpot."'
				)
			)
			.toBeVisible()
		await expect.element(page.getByText('DuckDuckGo Search')).toBeVisible()
	})

	it('shows the create-first placeholder before the agent exists', async () => {
		// Rendered directly: an explicit undefined would trip the
		// helper's default parameter.
		render(ToolsEditor, { value: '', onchange: () => {} })

		await expect
			.element(page.getByText('Create the agent first, then manage tools'))
			.toBeVisible()
		expect(getAgentTools).not.toHaveBeenCalled()
	})

	it('saves a toggle through the service once the debounce settles', async () => {
		const sw = renderEditor()
		vi.mocked(toggleAgentTool).mockResolvedValue({
			tools: [{ ...available, locallyDisabled: true }],
			servers: []
		})

		await sw.click()

		await vi.waitFor(() => {
			expect(toggleAgentTool).toHaveBeenCalledWith('research', available.id, {
				enabled: false
			})
		})
		await expect.element(sw).toHaveAttribute('aria-checked', 'false')
	})

	it('collapses rapid clicks into a no-op without firing a request', async () => {
		vi.useFakeTimers()
		try {
			const sw = renderEditor()

			// Two synchronous clicks are the machine-speed burst: both
			// flips land inside one debounce window.
			await vi.advanceTimersByTimeAsync(0)
			const handle = await sw.element()
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))

			vi.advanceTimersByTimeAsync(300)
			expect(toggleAgentTool).not.toHaveBeenCalled()
			await expect.element(sw).toHaveAttribute('aria-checked', 'true')
		} finally {
			vi.useRealTimers()
		}
	})

	it('flushes a pending toggle when the panel closes', async () => {
		vi.useFakeTimers()
		try {
			const sw = renderEditor()
			await vi.advanceTimersByTimeAsync(0)
			vi.mocked(toggleAgentTool).mockResolvedValue({
				tools: [{ ...available, locallyDisabled: true }],
				servers: []
			})

			const handle = await sw.element()
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))
			// The panel closes before the debounce would fire.
			cleanup()

			await vi.advanceTimersByTimeAsync(0)
			expect(toggleAgentTool).toHaveBeenCalledWith('research', available.id, {
				enabled: false
			})
		} finally {
			vi.useRealTimers()
		}
	})

	it('surfaces a failed save and keeps the last saved state', async () => {
		const sw = renderEditor()
		vi.mocked(toggleAgentTool).mockRejectedValue(
			new Error('tools store unreachable')
		)

		await sw.click()

		await expect
			.element(page.getByText('tools store unreachable'))
			.toBeVisible()
		// The switch renders the saved state — still available, not the
		// failed flip.
		await expect.element(sw).toHaveAttribute('aria-checked', 'true')
	})

	it('shows the load error instead of an empty list', async () => {
		vi.mocked(getAgentTools).mockRejectedValue(new Error('catalog down'))
		render(ToolsEditor, { agentId: 'research', value: '', onchange: () => {} })

		await expect.element(page.getByText('catalog down')).toBeVisible()
	})

	it('drops a burst that returned to the saved state when the panel closes', async () => {
		vi.useFakeTimers()
		try {
			const sw = renderEditor()
			await vi.advanceTimersByTimeAsync(0)
			const handle = await sw.element()
			// Click-unclick, then close inside the window: the net
			// zero flushes nothing.
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))
			cleanup()

			await vi.advanceTimersByTimeAsync(0)
			expect(toggleAgentTool).not.toHaveBeenCalled()
		} finally {
			vi.useRealTimers()
		}
	})

	it('renders a server gate with nested tools and no manage buttons', async () => {
		renderEditor([available, notesTool], 'research', [
			{ ...notesServer, agentEnabled: true }
		])

		await expect
			.element(page.getByRole('heading', { name: 'Notes MCP' }))
			.toBeVisible()
		const gate = page.getByRole('switch', {
			name: 'Enable Notes MCP for this agent'
		})
		await expect.element(gate).toHaveAttribute('aria-checked', 'true')
		await expect
			.element(page.getByRole('button', { name: 'Refresh tools' }))
			.not.toBeInTheDocument()
		await expect
			.element(page.getByRole('button', { name: 'Delete server' }))
			.not.toBeInTheDocument()
	})

	it("hides a gate-off server's nested rows but keeps its trigger", async () => {
		renderEditor([available, notesTool], 'research', [notesServer])

		await expect
			.element(page.getByRole('button', { name: 'Notes MCP' }))
			.toBeVisible()
		await expect
			.element(
				page.getByRole('switch', { name: 'Enable search for this agent' })
			)
			.not.toBeInTheDocument()
	})

	it('turns the server gate on through the service', async () => {
		vi.mocked(toggleAgentMcp).mockResolvedValue({
			tools: [available, notesTool],
			servers: [{ ...notesServer, agentEnabled: true }]
		})
		renderEditor([available, notesTool], 'research', [notesServer])

		await page
			.getByRole('switch', { name: 'Enable Notes MCP for this agent' })
			.click()

		await vi.waitFor(() => {
			expect(toggleAgentMcp).toHaveBeenCalledWith('research', 'notes-mcp', {
				enabled: true
			})
		})
		// The response listing re-renders the per-tool switches.
		await page.getByRole('button', { name: 'Notes MCP' }).click()
		await expect
			.element(
				page.getByRole('switch', { name: 'Enable search for this agent' })
			)
			.toHaveAttribute('aria-checked', 'true')
	})

	it('renders a globally disabled server inert even with the gate on', async () => {
		renderEditor([available, notesTool], 'research', [
			{ ...notesServer, agentEnabled: true, serverEnabled: false }
		])

		const gate = page.getByRole('switch', {
			name: 'Enable Notes MCP for this agent'
		})
		await expect.element(gate).toHaveAttribute('aria-checked', 'false')
		await expect.element(gate).toBeDisabled()
		await expect
			.element(page.getByText('Disabled globally', { exact: true }))
			.toBeVisible()
		// Nothing to expand while the server is dead globally; the
		// trigger itself stays mounted.
		await expect
			.element(page.getByRole('button', { name: 'Notes MCP' }))
			.toBeVisible()
	})
})

// A global switch the agent cannot influence renders as a blocker label
// with a link to the page that owns it — and the editor never fetches a
// definition to explain it.
describe('ToolsEditor global blockers', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	it('labels a globally disabled builtin and links to the settings page', async () => {
		renderEditor([blocked])

		const sw = page.getByRole('switch', {
			name: 'Enable Ghost Tool for this agent'
		})
		await expect.element(sw).toBeDisabled()
		await expect.element(sw).toHaveAttribute('aria-checked', 'false')
		await expect
			.element(page.getByText('Disabled globally', { exact: true }))
			.toBeVisible()

		const link = page.getByRole('link', {
			name: 'Disabled globally — Manage in MCP & Tools'
		})
		await expect.element(link).toBeVisible()
		await expect.element(link).toHaveAttribute('href', '/settings/tools')
		expect(getMcpToolDefinition).not.toHaveBeenCalled()
	})

	it('labels a server disabled globally', async () => {
		renderEditor([available, notesTool], 'research', [
			{ ...notesServer, agentEnabled: true, serverEnabled: false }
		])

		const link = page.getByRole('link', {
			name: 'Server disabled globally — Manage in MCP & Tools'
		})
		await expect.element(link).toBeVisible()
		await expect.element(link).toHaveAttribute('href', '/settings/tools')
	})

	it('labels a paused catalog as unavailable globally', async () => {
		renderEditor([available, notesTool], 'research', [
			{ ...notesServer, agentEnabled: true, catalogState: 'paused' }
		])

		await expect
			.element(
				page.getByRole('link', {
					name: 'Unavailable globally — Manage in MCP & Tools'
				})
			)
			.toBeVisible()
	})

	it('renders an opt-out as a plain switch, not a blocker', async () => {
		renderEditor([{ ...available, locallyDisabled: true }])

		const sw = page.getByRole('switch', {
			name: 'Enable DuckDuckGo Search for this agent'
		})
		await expect.element(sw).toBeEnabled()
		await expect.element(sw).toHaveAttribute('aria-checked', 'false')
		await expect
			.element(page.getByText('Disabled globally', { exact: true }))
			.not.toBeInTheDocument()
	})

	it('never fetches a definition from the agent editor', async () => {
		renderEditor([available, notesTool], 'research', [
			{ ...notesServer, agentEnabled: true }
		])

		await page.getByRole('button', { name: 'Notes MCP' }).click()
		await expect
			.element(
				page.getByRole('switch', { name: 'Enable search for this agent' })
			)
			.toBeVisible()
		await expect
			.element(page.getByRole('button', { name: /definition of search/ }))
			.not.toBeInTheDocument()
		expect(getMcpToolDefinition).not.toHaveBeenCalled()
	})
})

// The listing carries every server fact the panel renders: one fetch
// supplies the rows, their gates and their global blockers.
describe('ToolsEditor listing-derived server facts', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	it('labels a globally off MCP tool under a healthy server', async () => {
		renderEditor(
			[available, { ...notesTool, globalEnabled: false }],
			'research',
			[{ ...notesServer, agentEnabled: true }]
		)

		await page.getByRole('button', { name: 'Notes MCP' }).click()
		const sw = page.getByRole('switch', {
			name: 'Enable search for this agent'
		})
		await expect.element(sw).toBeDisabled()
		await expect.element(sw).toHaveAttribute('aria-checked', 'false')
		const link = page.getByRole('link', {
			name: 'Disabled globally — Manage in MCP & Tools'
		})
		await expect.element(link).toBeVisible()
		await expect.element(link).toHaveAttribute('href', '/settings/tools')
	})
})
