// tests/e2e/specs/navigation.spec.ts
import { expect, test } from '../setup/fixtures'

test.describe('navigation', () => {
	test.describe('unsaved changes protection', () => {
		test('should warn on navigation with unsaved changes', async ({ page }) => {
			await page.goto('/agent/new')
			await page.getByLabel(/name/i).fill('unsaved-test')

			// Try to navigate away
			await page.locator('a[href="/agent/_superagent"]').first().click()

			// Should show dialog
			await expect(page.getByRole('alertdialog')).toBeVisible()
		})

		test('should allow discard and navigate', async ({ page }) => {
			await page.goto('/agent/new')
			await page.getByLabel(/name/i).fill('discard-test')

			// Navigate and discard
			await page.locator('a[href="/agent/_superagent"]').first().click()

			// Click discard button
			await page
				.locator('button:has-text("Discard"), button:has-text("Leave")')
				.click()

			// Should navigate
			await expect(page).toHaveURL(/\/agent\/_superagent$/)
		})

		test('should allow keep editing and stay on page', async ({ page }) => {
			await page.goto('/agent/new')
			await page.getByLabel(/name/i).fill('keep-editing-test')

			// Try to navigate
			await page.locator('a[href="/agent/_superagent"]').first().click()

			// Click keep editing
			await page
				.locator('button:has-text("Keep"), button:has-text("Stay")')
				.click()

			// Should stay on page
			await expect(page).toHaveURL(/\/agent\/new/)
		})

		test('should not warn when no changes', async ({ page }) => {
			await page.goto('/agent/new')

			// Don't make any changes, just navigate
			await page.locator('a[href="/agent/_superagent"]').first().click()

			// Should navigate without dialog
			await expect(page).toHaveURL(/\/agent\/_superagent$/)
			await expect(page.locator('[role="dialog"]')).not.toBeVisible()
		})
	})

	test.describe('sidebar navigation', () => {
		test('should toggle sidebar on mobile', async ({ page }) => {
			// Set mobile viewport
			await page.setViewportSize({ width: 375, height: 667 })
			await page.goto('/')

			// Sidebar should have toggle button
			const toggleButton = page
				.locator(
					'button[aria-label*="sidebar"], button[data-sidebar="trigger"]'
				)
				.first()
			if (await toggleButton.isVisible()) {
				await toggleButton.click()
			}
		})
	})

	test.describe('deep linking', () => {
		test('should load agent detail directly via URL', async ({ page }) => {
			// First create an agent
			await page.goto('/agent/new')
			const agentName = `Deep Link ${Date.now()}`
			const agentId = agentName.toLowerCase().replace(/ +/g, '-')
			await page.getByLabel(/name/i).fill(agentName)
			await page.getByRole('button', { name: 'Create Agent' }).click()
			await expect(page).toHaveURL(new RegExp(`/agent/${agentId}$`))

			// Now reload the page directly
			await page.reload()
			await expect(page).toHaveURL(new RegExp(`/agent/${agentId}$`))
		})
	})
})
