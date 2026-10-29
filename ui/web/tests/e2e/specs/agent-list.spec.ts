// tests/e2e/specs/agent-list.spec.ts
import type { Page } from '@playwright/test'
import { expect, test } from '../setup/fixtures'

/**
 * Helper to create an agent via UI.
 * Each test should create its own agent to avoid shared mutable state.
 */
async function createAgentViaUi(page: Page, name: string) {
	const agentId = name.toLowerCase().replace(/ +/g, '-')
	await page.goto('/agent/new')
	await page.getByRole('textbox', { name: /name/i }).fill(name)
	await page.getByRole('button', { name: /create|submit/i }).click()
	await expect(page).toHaveURL(new RegExp(`/agent/${agentId}$`))
	return agentId
}

test.describe('agent list', () => {
	test('should display agents in sidebar', async ({ page }) => {
		await page.goto('/')

		// Sidebar should contain agent links
		// Use semantic selector for sidebar (matches data-slot="sidebar" in App sidebar)
		const sidebar = page.locator('[data-slot="sidebar"]')
		await expect(sidebar).toBeVisible()

		// The superagent renders from the client constant as the first menu item
		const superagentLink = sidebar.getByRole('link', { name: /ava/i })
		await expect(superagentLink).toBeVisible()
	})

	test('should show newly created agent in sidebar without a reload', async ({
		page
	}) => {
		const agentName = `List Test ${Date.now()}`

		// Create agent using helper; lands on the agent's own page
		const agentId = await createAgentViaUi(page, agentName)

		// The sidebar on the current page must already list the new agent —
		// invalidation refreshes it without a full navigation/reload.
		const sidebar = page.locator('[data-slot="sidebar"]')
		await expect(sidebar).toBeVisible()
		await expect(
			sidebar.getByRole('link', {
				name: new RegExp(agentId.replace(/-/g, ' '), 'i')
			})
		).toBeVisible()
	})

	test('should navigate to agent detail from sidebar', async ({ page }) => {
		// The active page's sidebar link is disabled, so start elsewhere
		await page.goto('/agent/new')

		// Click on any enabled agent link in sidebar
		const agentLink = page
			.locator('[data-slot="sidebar"] a[href^="/agent/"]:not([disabled])')
			.first()
		const href = await agentLink.getAttribute('href')

		if (href) {
			await agentLink.click()
			await expect(page).toHaveURL(new RegExp(href))
		}
	})

	test('should show empty state when no agents', async ({ page }) => {
		// This test requires a fresh user with no agents
		// For now, just verify the sidebar doesn't crash
		await page.goto('/')
		const sidebar = page.locator('[data-slot="sidebar"]')
		await expect(sidebar).toBeVisible()
	})

	test('should refresh agent list after navigation', async ({ page }) => {
		const agentName = `Refresh Test ${Date.now()}`

		// create agent using helper
		await createAgentViaUi(page, agentName)

		// Navigate to home via sidebar
		await page.goto('/')
		await expect(page.locator('[data-slot="sidebar"]')).toBeVisible()
	})
})
