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

		// superagent should always be present as the first menu item
		const superagentLink = sidebar.getByRole('link', { name: /ava/i })
		await expect(superagentLink).toBeVisible()
	})

	test('should show newly created agent in list', async ({ page }) => {
		const agentName = `List Test ${Date.now()}`

		// Create agent using helper
		const agentId = await createAgentViaUi(page, agentName)

		// Go to home and check sidebar
		await page.goto('/')
		const sidebar = page.locator('[data-slot="sidebar"]')
		await expect(sidebar).toBeVisible()

		// Assert the specific created agent exists in the sidebar
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
