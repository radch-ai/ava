// tests/e2e/specs/mcp-servers.spec.ts
import type { Locator, Page } from '@playwright/test'
import { type McpFixture, startMcpFixture } from '../fixtures/mcp-server'
import { createAgentViaUi, openToolsPanel } from '../fixtures/ui'
import { expect, test } from '../setup/fixtures'

/**
 * The MCP server lifecycle on the MCP & Tools page: a create that
 * cannot connect writes nothing, a healthy create lands its tools
 * globally off, the switches decide availability, a refresh never
 * disturbs a healthy server, and deleting the server cascades it out of
 * every panel.
 *
 * Serial: the tests read and mutate the one shared catalog.
 */
test.describe.configure({ mode: 'serial' })

const SERVER_NAME = 'Live Helper'

/** Mock mode drives the mock's URL sentinels; a real-backend run starts
 * its own MCP fixture on an ephemeral port (AVA_E2E_MCP_URL points the
 * suite at an external server instead), which also serves the pause
 * control. */
const useMocks = process.env.VITE_USE_MOCKS === 'true'
const externalFixtureUrl = process.env.AVA_E2E_MCP_URL
let fixtureUrl = 'http://127.0.0.1:3333'
let fixture: McpFixture | undefined

test.beforeAll(async () => {
	if (useMocks) return
	if (externalFixtureUrl) {
		fixtureUrl = externalFixtureUrl
		return
	}
	fixture = await startMcpFixture()
	fixtureUrl = fixture.url
})

test.afterAll(async () => {
	await fixture?.close()
})

/** One server entry: the divider row whose header names it. */
function serverRow(page: Page, name: string): Locator {
	return page
		.locator('[data-testid="mcp-server-row"]')
		.filter({ has: page.getByRole('heading', { name }) })
}

/** Flip the server's global switch on the settings page and wait for its save. */
async function setServerEnabled(page: Page, enabled: boolean): Promise<void> {
	await page.goto('/settings/tools')
	const sw = page.getByRole('switch', {
		name: `Enable ${SERVER_NAME} globally`
	})
	await expect(sw).toBeVisible()
	if ((await sw.getAttribute('aria-checked')) !== String(enabled)) {
		const saved = page.waitForResponse(
			(r) =>
				r.request().method() === 'PATCH' &&
				r.url().includes('/api/mcp/servers/')
		)
		await sw.click()
		await saved
	}
	await expect(sw).toHaveAttribute('aria-checked', String(enabled))
}

/** Flip the echo tool's global switch on the settings page and wait for its save. */
async function setEchoEnabled(page: Page, enabled: boolean): Promise<void> {
	await page.goto('/settings/tools')
	const row = serverRow(page, SERVER_NAME)
	await row.getByRole('button', { name: SERVER_NAME }).first().click()
	const sw = row.getByRole('switch', { name: 'Enable echo globally' })
	await expect(sw).toBeVisible()
	if ((await sw.getAttribute('aria-checked')) !== String(enabled)) {
		const saved = page.waitForResponse(
			(r) => r.request().method() === 'PATCH' && r.url().includes('/api/tools/')
		)
		await sw.click()
		await saved
	}
	await expect(sw).toHaveAttribute('aria-checked', String(enabled))
}

test('the page states the inheritance rule', async ({ page }) => {
	await page.goto('/settings/tools')
	await expect(
		page.getByText(/available to every agent using that server/)
	).toBeVisible()
})

test('the create form asks for an endpoint only', async ({ page }) => {
	await page.goto('/settings/tools')
	await page.getByRole('button', { name: 'Add MCP server' }).click()

	await expect(page.getByLabel('URL')).toBeVisible()
})

test('a create that cannot connect writes nothing', async ({ page }) => {
	await page.goto('/settings/tools')
	await page.getByRole('button', { name: 'Add MCP server' }).click()

	await page.getByLabel('Name').fill(SERVER_NAME)
	await page.getByLabel('URL').fill('not-a-url')
	await page.getByRole('button', { name: 'Add server' }).click()

	await expect(page.getByText(/must be an http\(s\) URL/)).toBeVisible()
	await expect(page.getByRole('heading', { name: SERVER_NAME })).toHaveCount(0)
})

test('a healthy create lands a server row whose tool starts off', async ({
	page
}) => {
	await page.goto('/settings/tools')
	await page.getByRole('button', { name: 'Add MCP server' }).click()

	await page.getByLabel('Name').fill(SERVER_NAME)
	await page.getByLabel('URL').fill(`${fixtureUrl}/mcp`)
	await page
		.getByLabel('Headers — KEY: VALUE per line')
		.fill('Authorization: Bearer canary-value')
	await page
		.getByRole('checkbox', { name: /Allow non-public destinations/ })
		.click()
	await page.getByRole('checkbox', { name: /Allow insecure HTTP/ }).click()
	await expect(page.locator('p', { hasText: 'cleartext' })).toBeVisible()
	await page.getByRole('button', { name: 'Add server' }).click()

	// The row appears with its count; the discovered tool nests under it.
	const row = serverRow(page, SERVER_NAME)
	await expect(row).toBeVisible()
	await expect(row.getByText(/^\d+\s+tools?$/)).toBeVisible()
	await expect(row.getByText('MCP', { exact: true })).toBeVisible()
	await expect(
		row.getByRole('switch', { name: 'Enable echo globally' })
	).toHaveCount(0)
	await row.getByRole('button', { name: SERVER_NAME }).click()
	// Discovery installs a new tool globally off: the switch is the
	// operator's decision, never assumed.
	await expect(
		row.getByRole('switch', { name: 'Enable echo globally' })
	).toHaveAttribute('aria-checked', 'false')
	// Sealed values never render — key names only.
	await expect(page.getByText('canary-value')).toHaveCount(0)
})

test('the server switch flattens the row when off', async ({ page }) => {
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

test('a rejected refresh pauses the catalog until a healthy one', async ({
	page
}) => {
	// A rejected generation pauses the catalog: the page shows the paused
	// status and the nested tools go inert.
	if (useMocks) {
		// The mock fails the connection check on a "paused" endpoint.
		await page.goto('/settings/tools')
		await serverRow(page, SERVER_NAME)
			.getByRole('button', { name: 'Edit server' })
			.click()
		await page.getByLabel('URL').fill('http://127.0.0.1:4444/paused')
		await page.getByRole('button', { name: 'Save changes' }).click()
	} else {
		// The real fixture serves a security-invalid listing on demand.
		await page.request.get(`${fixtureUrl}/__fixture/pause`)
	}
	await page.goto('/settings/tools')
	const pausedRefresh = page.waitForResponse(
		(r) => r.request().method() === 'POST' && r.url().includes('/refresh')
	)
	await serverRow(page, SERVER_NAME)
		.getByRole('button', { name: 'Refresh tools' })
		.click()
	await pausedRefresh

	await expect(
		page.getByText(/Catalog paused after a rejected refresh — refresh to retry/)
	).toBeVisible()

	// A valid refresh recovers it: the catalog unpauses.
	if (useMocks) {
		await serverRow(page, SERVER_NAME)
			.getByRole('button', { name: 'Edit server' })
			.click()
		await page.getByLabel('URL').fill('http://127.0.0.1:3333/mcp')
		await page.getByRole('button', { name: 'Save changes' }).click()
	} else {
		await page.request.get(`${fixtureUrl}/__fixture/ok`)
	}
	// The backend throttles per-server refresh at 500 ms; the recovery
	// click retries until it lands outside that window (429 answers are
	// the throttle, not a failure).
	const healthyRefresh = page.waitForResponse(
		(r) =>
			r.request().method() === 'POST' &&
			r.url().includes('/refresh') &&
			r.status() === 200
	)
	for (let attempt = 0; attempt < 10; attempt++) {
		await serverRow(page, SERVER_NAME)
			.getByRole('button', { name: 'Refresh tools' })
			.click()
		try {
			await Promise.race([
				healthyRefresh,
				page.waitForTimeout(300).then(() => {
					throw new Error('throttled')
				})
			])
			break
		} catch {
			// throttled — retry outside the window
		}
	}
	await healthyRefresh
	await expect(
		page.getByText(/Catalog paused after a rejected refresh — refresh to retry/)
	).toHaveCount(0)
})

test('refresh keeps a healthy server healthy', async ({ page }) => {
	await page.goto('/settings/tools')
	const refreshed = page.waitForResponse(
		(r) => r.request().method() === 'POST' && r.url().includes('/refresh')
	)
	await serverRow(page, SERVER_NAME)
		.getByRole('button', { name: 'Refresh tools' })
		.click()
	await refreshed
	// No error banner appears and the row stays.
	await expect(page.getByText(/Connection check failed/)).toHaveCount(0)
	await expect(page.getByRole('heading', { name: SERVER_NAME })).toBeVisible()
})

test('a global enable reaches a gated agent except where it opted out', async ({
	page
}) => {
	test.slow()
	// The operator enables the tool globally; nothing is written per agent.
	await setEchoEnabled(page, true)
	const toolUser = await createAgentViaUi(page, 'Tool User')

	// The agent's gate inherits the tool.
	let sheet = await openToolsPanel(page)
	const gate = sheet.getByRole('switch', {
		name: `Enable ${SERVER_NAME} for this agent`
	})
	await expect(gate).toHaveAttribute('aria-checked', 'false')
	await gate.click()
	await expect(gate).toHaveAttribute('aria-checked', 'true')
	await sheet.getByRole('button', { name: SERVER_NAME }).click()
	const tool = sheet.getByRole('switch', {
		name: 'Enable echo for this agent'
	})
	await expect(tool).toHaveAttribute('aria-checked', 'true')

	// The agent opts out of the tool.
	const off = page.waitForResponse(
		(r) =>
			r.request().method() === 'PATCH' &&
			r.url().includes(`/api/agent/${toolUser}/tools/`)
	)
	await tool.click()
	await off
	await expect(tool).toHaveAttribute('aria-checked', 'false')

	// Gate off -> on preserves the opt-out: only the agent's own switch
	// changes it.
	await gate.click()
	await gate.click()
	await expect(gate).toHaveAttribute('aria-checked', 'true')
	await expect(tool).toHaveAttribute('aria-checked', 'false')
	await page.keyboard.press('Escape')
	await expect(sheet).not.toBeVisible({ timeout: 5000 })

	// A second agent inherits the same globally enabled tool.
	await createAgentViaUi(page, 'Tool Two')
	sheet = await openToolsPanel(page)
	const secondGate = sheet.getByRole('switch', {
		name: `Enable ${SERVER_NAME} for this agent`
	})
	await secondGate.click()
	await expect(secondGate).toHaveAttribute('aria-checked', 'true')
	await sheet.getByRole('button', { name: SERVER_NAME }).click()
	await expect(
		sheet.getByRole('switch', { name: 'Enable echo for this agent' })
	).toHaveAttribute('aria-checked', 'true')
	await page.keyboard.press('Escape')
	await expect(sheet).not.toBeVisible({ timeout: 5000 })
})

test('a global blocker names itself and links to the page that owns it', async ({
	page
}) => {
	await setServerEnabled(page, false)

	await page.goto('/agent/tool-two')
	const sheet = await openToolsPanel(page)
	const gate = sheet.getByRole('switch', {
		name: `Enable ${SERVER_NAME} for this agent`
	})
	await expect(gate).toBeDisabled()
	await expect(gate).toHaveAttribute('aria-checked', 'false')

	const link = sheet.getByRole('link', {
		name: 'Server disabled globally — Manage in MCP & Tools'
	})
	await expect(link).toBeVisible()
	await link.click()
	await expect(page).toHaveURL(/\/settings\/tools$/)

	// Restore for the remaining tests.
	await setServerEnabled(page, true)
})

test('the settings page inspects a stored definition', async ({ page }) => {
	await page.goto('/settings/tools')
	const row = serverRow(page, SERVER_NAME)
	await row.getByRole('button', { name: SERVER_NAME }).click()
	await row.getByRole('button', { name: 'Show definition of echo' }).click()

	await expect(row.getByText(/"name":"echo"/)).toBeVisible()
	// The authority line: the endpoint and what the connection carries
	// (the paused-refresh pass above reset both approvals, so only the
	// endpoint and credential presence are asserted here).
	await expect(
		row.getByText(
			new RegExp(
				`${fixtureUrl.replace(/[.*+?^${}()|[\]\\]/g, String.raw`\$&`)}/mcp · credentials: yes`
			)
		)
	).toBeVisible()
})

test('deleting the server cascades it out of the agent panel', async ({
	page
}) => {
	await page.goto('/settings/tools')
	await serverRow(page, SERVER_NAME)
		.getByRole('button', { name: 'Delete server' })
		.click()
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
	await page.goto('/agent/tool-two')
	const sheet = await openToolsPanel(page)
	await expect(
		sheet.getByRole('switch', {
			name: `Enable ${SERVER_NAME} for this agent`
		})
	).toHaveCount(0)
	await expect(
		sheet.getByRole('switch', { name: 'Enable echo for this agent' })
	).toHaveCount(0)
})

test('a discovery pass that skipped a tool says so, and the notice dismisses', async ({
	page
}) => {
	// The skip sentinel is a fixture behavior, so this flow is a mock-mode
	// contract: a real-backend run has no endpoint that reports a cosmetic
	// skip without a hostile MCP server fixture.
	test.skip(
		process.env.VITE_USE_MOCKS !== 'true',
		'mock-only: the skip sentinel is a fixture behavior'
	)
	// The mock reports one cosmetic skip for a "skip" endpoint: a name the
	// provider wire contract cannot carry.
	await page.goto('/settings/tools')
	await page.getByRole('button', { name: 'Add MCP server' }).click()
	await page.getByLabel('Name').fill('Skip Sentinel')
	await page.getByLabel('URL').fill('http://127.0.0.1:3333/skip')
	await page.getByRole('button', { name: 'Add server' }).click()

	await expect(page.getByText('1 tool was skipped')).toBeVisible()
	await expect(page.getByText(/exceeds 64 characters/)).toBeVisible()

	await page.getByRole('button', { name: 'Dismiss skipped tools' }).click()
	await expect(page.getByText('1 tool was skipped')).toHaveCount(0)
})
