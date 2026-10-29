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

	test.describe('unsaved changes protection — native beforeunload', () => {
		// The guard above covers SPA navigation; these cover the
		// browser-level guard on /agent/new (reload/close). `fill()` does
		// not grant the user activation Chromium requires to fire
		// beforeunload, so the name is typed with real key events.
		//
		// The dialog must always be answered explicitly: without a
		// `dialog` listener Playwright bypasses beforeunload and the
		// navigation proceeds silently — a real browser would show the
		// modal and wait. A cancelled reload's promise never settles in
		// the driver, so it is fired unawaited and the outcome is
		// asserted from document state.
		test('should reload when the user leaves', async ({ page }) => {
			await page.goto('/agent/new')
			const name = page.getByLabel(/name/i)
			await name.click()
			await name.pressSequentially('accept-reload-test')

			// Record the dialog: without this, the test would pass even
			// if the guard were removed (reload proceeds, form resets).
			const dialogs: string[] = []
			await page.once('dialog', (dialog) => {
				dialogs.push(dialog.type())
				dialog.accept()
			})
			await page.reload()

			// Leaving tore the document down: fresh form, name cleared
			expect(dialogs).toEqual(['beforeunload'])
			await expect(name).toHaveValue('')
			expect(await page.evaluate(() => location.pathname)).toBe('/agent/new')
		})

		test('should stay on the page when the user cancels', async ({ page }) => {
			await page.goto('/agent/new')
			const name = page.getByLabel(/name/i)
			await name.click()
			await name.pressSequentially('dismiss-reload-test')

			await page.once('dialog', (dialog) => dialog.dismiss())
			// A cancelled reload's promise never settles in the driver —
			// fire it unawaited; the assertions prove the document survived.
			void page.reload().catch(() => {})

			// Cancelling kept the document alive: typed name intact
			await expect(name).toHaveValue('dismiss-reload-test')
			expect(await page.evaluate(() => location.pathname)).toBe('/agent/new')
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
