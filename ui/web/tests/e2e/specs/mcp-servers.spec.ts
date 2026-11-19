// tests/e2e/specs/mcp-servers.spec.ts
import type { Locator, Page } from '@playwright/test'
import { expect, test } from '../setup/fixtures'

/**
 * The MCP server lifecycle on the MCP & Tools page: a create that
 * cannot connect writes nothing, a healthy create lands its tools as a
 * collapsible row, the global switch gates them, the per-agent gate
 * seeds grants on first enable and preserves choices afterwards, and
 * deleting the server cascades it out of every panel.
 *
 * Serial: the tests read and mutate the one shared mock catalog.
 */
test.describe.configure({ mode: 'serial' })

const SERVER_NAME = 'Live Helper'
const SLUG = 'live-helper'
const TOOL_SWITCH = 'Enable echo for this agent'

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

test('the page states that only built-ins are granted by default', async ({
	page
}) => {
	await page.goto('/settings/tools')
	await expect(
		page.getByText(/Only built-in tools are granted to agents by default/)
	).toBeVisible()
})

test('a create that cannot connect writes nothing', async ({ page }) => {
	await page.goto('/settings/tools')
	await page.getByRole('button', { name: 'Add MCP server' }).click()

	await page.getByLabel('Name').fill(SERVER_NAME)
	await page.getByLabel('Command').fill('/nonexistent-helper')
	await page.getByRole('button', { name: 'Add server' }).click()

	await expect(page.getByText(/Connection check failed/)).toBeVisible()
	await expect(page.getByRole('heading', { name: SERVER_NAME })).toHaveCount(0)
})

test('a healthy create lands a collapsible server row with its tool', async ({
	page
}) => {
	await page.goto('/settings/tools')
	await page.getByRole('button', { name: 'Add MCP server' }).click()

	await page.getByLabel('Name').fill(SERVER_NAME)
	await page.getByLabel('Command').fill('helper-mcp')
	await page.getByLabel(/Environment/).fill('HELPER_TOKEN: canary-value')
	await page.getByRole('button', { name: 'Add server' }).click()

	// The row appears with its transport and tool count; the
	// discovered tool nests under it, not flat in the catalog.
	await expect(page.getByRole('heading', { name: SERVER_NAME })).toBeVisible()
	await expect(page.getByText('1 tool').first()).toBeVisible()
	await expect(page.getByText('MCP · stdio').first()).toBeVisible()
	await expect(
		page.getByRole('switch', { name: 'Enable echo globally' })
	).toHaveCount(0)
	await page.getByRole('button', { name: SERVER_NAME }).click()
	await expect(
		page.getByRole('switch', { name: 'Enable echo globally' })
	).toBeVisible()
	// Sealed values never render — key names only.
	await expect(page.getByText('canary-value')).toHaveCount(0)
})

test('the global switch flattens the server row when off', async ({ page }) => {
	await page.goto('/settings/tools')
	const gate = page.getByRole('switch', {
		name: `Enable ${SERVER_NAME} globally`
	})
	await expect(gate).toHaveAttribute('aria-checked', 'true')
	// The row is expandable while on.
	await page.getByRole('button', { name: SERVER_NAME }).click()
	await expect(
		page.getByRole('switch', { name: 'Enable echo globally' })
	).toBeVisible()

	await gate.click()
	await expect(gate).toHaveAttribute('aria-checked', 'false')
	// Off: the nested switches disappear; the trigger row stays.
	await expect(page.getByRole('button', { name: SERVER_NAME })).toBeVisible()
	await expect(
		page.getByRole('switch', { name: 'Enable echo globally' })
	).toHaveCount(0)

	// Restore for the remaining tests.
	await gate.click()
	await expect(gate).toHaveAttribute('aria-checked', 'true')
})

test('refresh keeps a healthy server healthy', async ({ page }) => {
	await page.goto('/settings/tools')
	await page.getByRole('button', { name: 'Refresh tools' }).nth(1).click()
	// No error banner appears and the row stays.
	await expect(page.getByText(/Connection check failed/)).toHaveCount(0)
	await expect(page.getByRole('heading', { name: SERVER_NAME })).toBeVisible()
})

test('first enable seeds the agent gate and keeps per-tool choices', async ({
	page
}) => {
	test.slow() // two sheet opens + agent create in one flow
	await createAgentViaUi(page, 'Tool User')
	const sheet = await openToolsPanel(page)

	// Default-deny: the gate is off and the row is flat — no nested
	// switches until the agent approves the server.
	const gate = sheet.getByRole('switch', {
		name: `Enable ${SERVER_NAME} for this agent`
	})
	await expect(gate).toHaveAttribute('aria-checked', 'false')
	await expect(sheet.getByRole('switch', { name: TOOL_SWITCH })).toHaveCount(0)

	// First enable approves the server's current tools; the row
	// becomes expandable and the tools arrive granted.
	await gate.click()
	await expect(gate).toHaveAttribute('aria-checked', 'true')
	await sheet.getByRole('button', { name: SERVER_NAME }).click()
	const tool = sheet.getByRole('switch', { name: TOOL_SWITCH })
	await expect(tool).toHaveAttribute('aria-checked', 'true')

	// Revoke one tool, gate off -> on: the revoke survives.
	await tool.click()
	await expect(tool).toHaveAttribute('aria-checked', 'false')
	await gate.click()
	await gate.click()
	await expect(gate).toHaveAttribute('aria-checked', 'true')
	await expect(tool).toHaveAttribute('aria-checked', 'false')

	// Tool access saved live; close the sheet without touching Save
	// (the expanded list can push the footer out of the viewport).
	await page.keyboard.press('Escape')
	await expect(sheet).not.toBeVisible({ timeout: 5000 })
})

test('a globally disabled server shows inert in the agent panel', async ({
	page
}) => {
	// Disable globally on the MCP & Tools page. The switch flips
	// optimistically and the debounced save flushes on navigation --
	// wait for the PATCH itself so the panel fetch below cannot race it.
	await page.goto('/settings/tools')
	const disablePatch = page.waitForResponse(
		(r) =>
			r.request().method() === 'PATCH' &&
			r.url().includes(`/api/mcp/servers/${SLUG}`)
	)
	await page
		.getByRole('switch', { name: `Enable ${SERVER_NAME} globally` })
		.click()
	await disablePatch
	await expect(
		page.getByRole('switch', { name: `Enable ${SERVER_NAME} globally` })
	).toHaveAttribute('aria-checked', 'false')

	// The agent panel cannot show the dead server as active: its gate
	// renders off and inert with a Disabled-globally badge, and the row
	// is flat.
	await page.goto('/agent/tool-user')
	const sheet = await openToolsPanel(page)
	const gate = sheet.getByRole('switch', {
		name: `Enable ${SERVER_NAME} for this agent`
	})
	await expect(gate).toHaveAttribute('aria-checked', 'false')
	await expect(gate).toBeDisabled()
	await expect(sheet.getByText('Disabled globally')).toBeVisible()
	await expect(sheet.getByRole('switch', { name: TOOL_SWITCH })).toHaveCount(0)
	await page.keyboard.press('Escape')
	await expect(sheet).not.toBeVisible({ timeout: 5000 })

	// Restore for the delete test.
	await page.goto('/settings/tools')
	const enablePatch = page.waitForResponse(
		(r) =>
			r.request().method() === 'PATCH' &&
			r.url().includes(`/api/mcp/servers/${SLUG}`)
	)
	await page
		.getByRole('switch', { name: `Enable ${SERVER_NAME} globally` })
		.click()
	await enablePatch
	await expect(
		page.getByRole('switch', { name: `Enable ${SERVER_NAME} globally` })
	).toHaveAttribute('aria-checked', 'true')
})

test('deleting the server cascades it out of the agent panel', async ({
	page
}) => {
	await page.goto('/settings/tools')
	await page.getByRole('button', { name: 'Delete server' }).nth(1).click()
	const dialog = page.getByRole('alertdialog', {
		name: `Delete ${SERVER_NAME}?`
	})
	await expect(dialog).toBeVisible()
	await dialog.getByRole('button', { name: 'Delete' }).click()
	await expect(page.getByRole('heading', { name: SERVER_NAME })).toHaveCount(0)
	await expect(
		page.getByRole('switch', { name: 'Enable echo globally' })
	).toHaveCount(0)

	// The agent's panel lost the server row with its tools.
	await page.goto('/agent/tool-user')
	const sheet = await openToolsPanel(page)
	await expect(
		sheet.getByRole('switch', {
			name: `Enable ${SERVER_NAME} for this agent`
		})
	).toHaveCount(0)
	await expect(sheet.getByRole('switch', { name: TOOL_SWITCH })).toHaveCount(0)
})
