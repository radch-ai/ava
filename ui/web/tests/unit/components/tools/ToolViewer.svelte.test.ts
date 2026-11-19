import { afterEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import ToolViewer from '#components/tools/ToolViewer.svelte'
import type { ViewerEntry } from '#components/tools/viewer-types'

// The shared viewer owns layout only: switches, collapse, and the
// manage-button slot. Both surfaces are covered through it here.

afterEach(() => {
	cleanup()
})

function builtinEntry(checked = true): ViewerEntry {
	return {
		kind: 'tool',
		entry: {
			row: {
				id: 'duckduckgo_search',
				name: 'DuckDuckGo Search',
				description: 'Searches the web.'
			},
			state: { checked, disabled: false },
			label: 'Enable DuckDuckGo Search globally',
			ontoggle: () => {}
		}
	}
}

function serverEntry(overrides: {
	manage?: boolean
	toolDisabled?: boolean
}): ViewerEntry {
	return {
		kind: 'server',
		entry: {
			row: { id: 'notes-mcp', name: 'Notes MCP' },
			transport: 'stdio',
			toolCount: 1,
			state: { checked: true, disabled: false },
			label: 'Enable Notes MCP globally',
			ontoggle: () => {},
			tools: [
				{
					row: { id: 'notes-mcp:search', name: 'search' },
					state: {
						checked: !overrides.toolDisabled,
						disabled: overrides.toolDisabled ?? false,
						dimmed: overrides.toolDisabled
					},
					label: 'Enable search globally',
					ontoggle: () => {}
				}
			],
			manage: overrides.manage
				? {
						refreshing: false,
						onrefresh: vi.fn(),
						onedit: vi.fn(),
						ondelete: vi.fn()
					}
				: undefined
		}
	}
}

describe('ToolViewer', () => {
	it('renders builtin rows flat and keeps nested switches collapsed', async () => {
		render(ToolViewer, { entries: [builtinEntry(), serverEntry({})] })

		await expect
			.element(
				page.getByRole('switch', { name: 'Enable DuckDuckGo Search globally' })
			)
			.toBeVisible()
		await expect
			.element(page.getByRole('heading', { name: 'Notes MCP' }))
			.toBeVisible()
		await expect
			.element(page.getByRole('switch', { name: 'Enable search globally' }))
			.not.toBeInTheDocument()
	})

	it('expands a server through its header trigger', async () => {
		render(ToolViewer, { entries: [serverEntry({})] })

		await page.getByRole('button', { name: 'Notes MCP' }).click()

		await expect
			.element(page.getByRole('switch', { name: 'Enable search globally' }))
			.toBeVisible()
	})

	it('shows the chevron only while the row is on', async () => {
		const on = serverEntry({})
		render(ToolViewer, { entries: [on] })
		const onTrigger = await page
			.getByRole('button', { name: 'Notes MCP' })
			.element()
		expect(onTrigger.querySelector('svg')).not.toBeNull()

		cleanup()
		const off = serverEntry({})
		off.entry.state = { checked: false, disabled: false }
		render(ToolViewer, { entries: [off] })
		const offTrigger = await page
			.getByRole('button', { name: 'Notes MCP' })
			.element()
		expect(offTrigger.querySelector('svg')).toBeNull()
	})

	it('shows manage buttons only when the surface provides them', async () => {
		render(ToolViewer, {
			entries: [serverEntry({ manage: true }), builtinEntry()]
		})
		await expect
			.element(page.getByRole('button', { name: 'Refresh tools' }))
			.toBeEnabled()
		await expect
			.element(page.getByRole('button', { name: 'Edit server' }))
			.toBeEnabled()
		await expect
			.element(page.getByRole('button', { name: 'Delete server' }))
			.toBeEnabled()

		cleanup()
		render(ToolViewer, { entries: [serverEntry({})] })
		await expect
			.element(page.getByRole('button', { name: 'Refresh tools' }))
			.not.toBeInTheDocument()
	})

	it('renders a nested tool disabled and dimmed when its state says so', async () => {
		render(ToolViewer, { entries: [serverEntry({ toolDisabled: true })] })

		await page.getByRole('button', { name: 'Notes MCP' }).click()

		await expect
			.element(page.getByRole('switch', { name: 'Enable search globally' }))
			.toBeDisabled()
	})

	it('empty entries render the empty-state copy', async () => {
		render(ToolViewer, { entries: [] })

		await expect.element(page.getByText('No tools available.')).toBeVisible()
	})

	it("hides a switched-off server's nested rows but keeps its trigger", async () => {
		const off = serverEntry({})
		off.entry.state = { checked: false, disabled: false }
		render(ToolViewer, { entries: [off] })

		await expect
			.element(page.getByRole('button', { name: 'Notes MCP' }))
			.toBeVisible()
		await expect
			.element(page.getByRole('switch', { name: 'Enable search globally' }))
			.not.toBeInTheDocument()
	})
})
