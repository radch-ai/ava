// tests/e2e/specs/agent-tools-panel.spec.ts
import type { Locator, Page } from '@playwright/test'
import { createAgentViaUi, openToolsPanel } from '../fixtures/ui'
import { expect, test } from '../setup/fixtures'

/**
 * The agent Tools panel saves each row on its own — no Save/Cancel —
 * and a globally disabled tool stays listed, badged, inert, and linked to
 * the page that owns the switch.
 *
 * Serial: both tests read and mutate the one shared catalog.
 */
test.describe.configure({ mode: 'serial' })

const useMocks = process.env.VITE_USE_MOCKS === 'true'
const toolSwitchName = 'Enable DuckDuckGo Search for this agent'

/** Close the sheet; tool rows save live, so no unsaved-changes guard. */
async function closeSheet(sheet: Locator): Promise<void> {
	await sheet.getByRole('button', { name: 'Cancel' }).click()
	await expect(sheet).not.toBeVisible({ timeout: 5000 })
}

/** Flip the global switch on the MCP & Tools page and wait for the save. */
async function setGlobalEnabled(page: Page, enabled: boolean): Promise<void> {
	await page.goto('/settings/tools')
	const sw = page.getByRole('switch', {
		name: 'Enable DuckDuckGo Search globally'
	})
	await expect(sw).toBeVisible()
	if ((await sw.getAttribute('aria-checked')) !== String(enabled)) {
		// The switch renders the desired flip before the debounced
		// request lands; leaving the page earlier would race the save.
		const saved = page.waitForResponse(
			(response) =>
				response.request().method() === 'PATCH' &&
				response.url().includes('/api/tools/')
		)
		await sw.click()
		await saved
	}
	await expect(sw).toHaveAttribute('aria-checked', String(enabled))
}

test.describe('agent tools panel', () => {
	test('saves a toggle live and keeps it across panel reopen', async ({
		page
	}) => {
		await createAgentViaUi(page, `Tools Panel ${Date.now()}`)

		let sheet = await openToolsPanel(page)
		let sw = sheet.getByRole('switch', { name: toolSwitchName })
		await expect(sw).toBeVisible()
		// The built-in starts inherited (globally on, no opt-out).
		await expect(sw).toHaveAttribute('aria-checked', 'true')

		await sw.click()
		await expect(sw).toHaveAttribute('aria-checked', 'false')

		// No Save: closing and reopening re-fetches, and the opt-out the
		// toggle wrote stays in force.
		await closeSheet(sheet)
		sheet = await openToolsPanel(page)
		sw = sheet.getByRole('switch', { name: toolSwitchName })
		await expect(sw).toHaveAttribute('aria-checked', 'false')
	})

	test('labels a globally disabled tool and links to the page that owns it', async ({
		page
	}) => {
		const agentId = await createAgentViaUi(page, `Tools Global ${Date.now()}`)
		try {
			await setGlobalEnabled(page, false)

			// The panel still lists the tool: badged, inert, and the
			// blocker names itself and links to where the switch lives.
			await page.goto(`/agent/${agentId}`)
			let sheet = await openToolsPanel(page)
			let sw = sheet.getByRole('switch', { name: toolSwitchName })
			await expect(sw).toBeVisible()
			await expect(
				sheet.getByText('Disabled globally', { exact: true })
			).toBeVisible()
			await expect(sw).toBeDisabled()
			await expect(sw).toHaveAttribute('aria-checked', 'false')
			const link = sheet.getByRole('link', {
				name: 'Disabled globally — Manage in MCP & Tools'
			})
			await expect(link).toBeVisible()
			await expect(link).toHaveAttribute('href', '/settings/tools')
			await closeSheet(sheet)

			// Re-enabling globally restores the switch in its saved
			// state: the agent's own choice was never touched.
			await setGlobalEnabled(page, true)
			await page.goto(`/agent/${agentId}`)
			sheet = await openToolsPanel(page)
			sw = sheet.getByRole('switch', { name: toolSwitchName })
			await expect(sw).toBeEnabled()
			await expect(sw).toHaveAttribute('aria-checked', 'true')
		} finally {
			await setGlobalEnabled(page, true)
		}
	})
})

/**
 * A globally off MCP tool under a healthy server labels itself and links
 * to the page that owns the switch. Mock mode seeds the Notes MCP server
 * with its tools globally off, so no UI setup can create that state
 * against the real backend's empty catalog.
 */
test('labels a globally off MCP tool and links to the tools page', async ({
	page
}) => {
	test.skip(
		!useMocks,
		'seeded MCP server with globally off tools: mock mode only'
	)
	// Server facts ride the tool listing: the panel must not fetch the
	// management catalog to render its rows.
	const catalogFetches: string[] = []
	page.on('request', (request) => {
		if (request.url().includes('/api/mcp/servers')) {
			catalogFetches.push(request.url())
		}
	})
	await page.goto('/agent/research')
	const sheet = await openToolsPanel(page)

	// The seeded gate is off: turn it on so the nested tools render.
	await sheet
		.getByRole('switch', { name: 'Enable Notes MCP for this agent' })
		.click()
	await sheet.getByRole('button', { name: 'Notes MCP' }).click()

	const link = sheet.getByRole('link', {
		name: 'Disabled globally — Manage in MCP & Tools'
	})
	await expect(link.first()).toBeVisible()
	expect(catalogFetches).toEqual([])
	await link.first().click()
	await expect(page).toHaveURL(/\/settings\/tools$/)
})
