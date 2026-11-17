import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import ToolsEditor from '#components/agent-config/ToolsEditor.svelte'
import { getAgentTools, toggleAgentTool } from '#lib/services/agent/config'
import type { AgentTool } from '#lib/types/tool'

// Mock the agent config service — the panel never hits the network.
vi.mock('#lib/services/agent/config', () => ({
	getAgentTools: vi.fn(),
	toggleAgentTool: vi.fn()
}))

const enabled: AgentTool = {
	id: 'duckduckgo_search',
	name: 'DuckDuckGo Search',
	description: 'Searches the web and returns the top results.',
	globalEnabled: true,
	agentEnabled: true
}

const blocked: AgentTool = {
	...enabled,
	id: 'ghost_tool',
	name: 'Ghost Tool',
	globalEnabled: false,
	agentEnabled: true
}

// Mocks are set before render — the fetch effect runs on mount.
function renderEditor(tools: AgentTool[] = [enabled], agentId = 'research') {
	vi.mocked(getAgentTools).mockResolvedValue(tools)
	render(ToolsEditor, { agentId, value: '', onchange: () => {} })
	return page.getByRole('switch', {
		name: 'Enable DuckDuckGo Search for this agent'
	})
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

	it('marks a globally disabled tool: badge, off, inert switch', async () => {
		renderEditor([blocked])

		const sw = page.getByRole('switch', {
			name: 'Enable Ghost Tool for this agent'
		})
		await expect.element(sw).toBeDisabled()
		await expect.element(sw).toHaveAttribute('aria-checked', 'false')
		await expect.element(page.getByText('Disabled globally')).toBeVisible()
	})

	it('saves a toggle through the service once the debounce settles', async () => {
		const sw = renderEditor()
		vi.mocked(toggleAgentTool).mockResolvedValue({
			...enabled,
			agentEnabled: false
		})

		await sw.click()

		await vi.waitFor(() => {
			expect(toggleAgentTool).toHaveBeenCalledWith('research', enabled.id, {
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
				...enabled,
				agentEnabled: false
			})

			const handle = await sw.element()
			handle.dispatchEvent(new MouseEvent('click', { bubbles: true }))
			// The panel closes before the debounce would fire.
			cleanup()

			await vi.advanceTimersByTimeAsync(0)
			expect(toggleAgentTool).toHaveBeenCalledWith('research', enabled.id, {
				enabled: false
			})
		} finally {
			vi.useRealTimers()
		}
	})

	it('surfaces a failed save and keeps the last saved grant state', async () => {
		const sw = renderEditor()
		vi.mocked(toggleAgentTool).mockRejectedValue(
			new Error('tools store unreachable')
		)

		await sw.click()

		await expect
			.element(page.getByText('tools store unreachable'))
			.toBeVisible()
		// The switch renders the saved state — still granted, not the
		// failed flip.
		await expect.element(sw).toHaveAttribute('aria-checked', 'true')
	})

	it('shows the load error instead of an empty list', async () => {
		vi.mocked(getAgentTools).mockRejectedValue(new Error('catalog down'))
		render(ToolsEditor, { agentId: 'research', value: '', onchange: () => {} })

		await expect.element(page.getByText('catalog down')).toBeVisible()
	})
	it('drops a burst that returned to the saved grant when the panel closes', async () => {
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
})
