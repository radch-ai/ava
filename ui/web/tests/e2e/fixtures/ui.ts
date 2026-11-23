/**
 * tests/e2e/fixtures/ui.ts
 *
 * Shared UI helpers for the specs that drive the agent surfaces: the
 * create-agent form and the agent configuration sheet.
 */
import type { Locator, Page } from '@playwright/test'
import { expect } from '../setup/fixtures'

/** Create an agent through the UI; answers the id the app derived. */
export async function createAgentViaUi(
	page: Page,
	name: string
): Promise<string> {
	const agentId = name.toLowerCase().replace(/ +/g, '-')
	await page.goto('/agent/new')
	await page.getByLabel(/name/i).fill(name)
	await page.getByRole('button', { name: 'Create Agent' }).click()
	await expect(page).toHaveURL(new RegExp(`/agent/${agentId}$`))
	return agentId
}

/** Open the agent config sheet on its Tools tab. */
export async function openToolsPanel(page: Page): Promise<Locator> {
	await page.getByRole('button', { name: 'Open agent configuration' }).click()
	const sheet = page.getByRole('dialog', { name: /agent configuration/i })
	await expect(sheet).toBeVisible({ timeout: 10000 })
	await sheet.getByRole('button', { name: 'Tools' }).click()
	return sheet
}
