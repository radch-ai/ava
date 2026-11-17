// tests/e2e/specs/agent-tools-panel.spec.ts
import type { Locator, Page } from '@playwright/test'
import { expect, test } from '../setup/fixtures'

/**
 * The agent Tools panel saves each row on its own — no Save/Cancel —
 * and a globally disabled tool stays listed, badged, and inert while
 * its saved grant survives the global switch.
 *
 * Serial: both tests read and mutate the one shared mock catalog.
 */
test.describe.configure({ mode: 'serial' })

const toolSwitchName = 'Enable DuckDuckGo Search for this agent'

/** Create an agent through the UI; answers the id the app derived. */
async function createAgentViaUi(page: Page, name: string): Promise<string> {
	const agentId = name.toLowerCase().replace(/ +/g, '-')
	await page.goto('/agent/new')
	await page.getByLabel(/name/i).fill(name)
	await page.getByRole('button', { name: 'Create Agent' }).click()
	await expect(page).toHaveURL(new RegExp(`/agent/${agentId}$`))
	return agentId
}

/** Open the agent config sheet on its Tools tab. */
async function openToolsPanel(page: Page): Promise<Locator> {
	await page.getByRole('button', { name: 'Open agent configuration' }).click()
	const sheet = page.getByRole('dialog', { name: /agent configuration/i })
	await expect(sheet).toBeVisible({ timeout: 10000 })
	await sheet.getByRole('button', { name: 'Tools' }).click()
	return sheet
}

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
		// The built-in starts granted.
		await expect(sw).toHaveAttribute('aria-checked', 'true')

		await sw.click()
		await expect(sw).toHaveAttribute('aria-checked', 'false')

		// No Save: closing and reopening re-fetches, and the grant the
		// toggle deleted stays deleted.
		await closeSheet(sheet)
		sheet = await openToolsPanel(page)
		sw = sheet.getByRole('switch', { name: toolSwitchName })
		await expect(sw).toHaveAttribute('aria-checked', 'false')
	})

	test('keeps a globally disabled tool inert without losing its grant', async ({
		page
	}) => {
		const agentId = await createAgentViaUi(page, `Tools Global ${Date.now()}`)
		try {
			await setGlobalEnabled(page, false)

			// The panel still lists the tool, badged and inert.
			await page.goto(`/agent/${agentId}`)
			let sheet = await openToolsPanel(page)
			let sw = sheet.getByRole('switch', { name: toolSwitchName })
			await expect(sw).toBeVisible()
			await expect(
				sheet.getByText('Disabled globally', { exact: true })
			).toBeVisible()
			await expect(sw).toBeDisabled()
			await expect(sw).toHaveAttribute('aria-checked', 'false')
			await closeSheet(sheet)

			// Re-enabling globally restores the switch in its saved
			// state: the grant itself was never touched.
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
