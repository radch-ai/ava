// tests/e2e/specs/navigation.spec.ts
import type { Page } from '@playwright/test'
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

		test('should warn on navigation with a dirty edit sheet and discard navigates', async ({
			page
		}) => {
			// SPA history for the back button: hard loads create document
			// entries (back = unload, the beforeunload guard's domain), so
			// seed one client-side navigation before dirtying the sheet.
			await page.goto('/agent/research')
			await page.locator('a[href="/agent/_superagent"]').first().click()
			await expect(page).toHaveURL(/\/agent\/_superagent$/)
			await page
				.getByRole('button', { name: 'Open agent configuration' })
				.click()
			const sheet = page.getByRole('dialog', {
				name: /agent configuration/i
			})
			await expect(sheet).toBeVisible({ timeout: 10000 })
			const name = sheet.getByPlaceholder(/enter agent name/i)
			await name.fill('')
			await name.pressSequentially('nav-away-test')

			// The sheet overlay covers the sidebar, so back is the realistic
			// in-app navigation away from a dirty sheet (popstate route).
			// A cancelled navigation's promise never settles — unawaited.
			void page.goBack().catch(() => {})

			await expect(page.getByRole('alertdialog')).toBeVisible()
			await page.getByRole('button', { name: 'Discard' }).click()

			await expect(page).toHaveURL(/\/agent\/research$/)
			await expect(page.getByPlaceholder(/enter agent name/i)).toHaveCount(0)
		})
	})

	test.describe('unsaved changes protection — native beforeunload', () => {
		// The guard above covers SPA navigation; these cover the
		// browser-level guard (reload/close). `fill()` does not grant
		// the user activation Chromium requires to fire beforeunload, so
		// the name is typed with real key events.
		//
		// The dialog must always be answered explicitly: without a
		// `dialog` listener Playwright bypasses beforeunload and the
		// navigation proceeds silently — a real browser would show the
		// modal and wait. A cancelled reload's promise never settles in
		// the driver, so it is fired unawaited and the outcome is
		// asserted from document state.

		// Both editing surfaces sit on the same UnsavedChangesGuards, so the
		// same scenarios run against each through one shared definition.
		const guardSurfaces = [
			{
				name: 'create form',
				openAndDirty: async (page: Page) => {
					await page.goto('/agent/new')
					const name = page.getByLabel(/name/i)
					await name.click()
					await name.pressSequentially('guard-test')
				},
				dirtyInput: (page: Page) => page.getByLabel(/name/i),
				// Leaving tore the document down: create form resets empty
				assertLeft: async (page: Page) => {
					await expect(page.getByLabel(/name/i)).toHaveValue('')
				}
			},
			{
				name: 'edit sheet',
				openAndDirty: async (page: Page) => {
					await page.goto('/agent/_superagent')
					await page
						.getByRole('button', { name: 'Open agent configuration' })
						.click()
					const sheet = page.getByRole('dialog', {
						name: /agent configuration/i
					})
					await expect(sheet).toBeVisible({ timeout: 10000 })
					const name = sheet.getByPlaceholder(/enter agent name/i)
					await name.fill('')
					await name.pressSequentially('guard-test')
				},
				dirtyInput: (page: Page) =>
					page
						.getByRole('dialog', { name: /agent configuration/i })
						.getByPlaceholder(/enter agent name/i),
				// Leaving tore the document down: fresh page has no sheet
				assertLeft: async (page: Page) => {
					await expect(page.getByPlaceholder(/enter agent name/i)).toHaveCount(
						0
					)
				}
			}
		]

		for (const surface of guardSurfaces) {
			test(`should reload when the user leaves (${surface.name})`, async ({
				page
			}) => {
				await surface.openAndDirty(page)

				// Record the dialog: without this, the test would pass even
				// if the guard were removed (reload proceeds, form resets).
				const dialogs: string[] = []
				await page.once('dialog', (dialog) => {
					dialogs.push(dialog.type())
					dialog.accept()
				})
				await page.reload()

				expect(dialogs).toEqual(['beforeunload'])
				await surface.assertLeft(page)
			})

			test(`should stay on the page when the user cancels (${surface.name})`, async ({
				page
			}) => {
				await surface.openAndDirty(page)

				await page.once('dialog', (dialog) => dialog.dismiss())
				// A cancelled reload's promise never settles in the driver —
				// fire it unawaited; assertions prove the document survived.
				void page.reload().catch(() => {})

				// Cancelling kept the document alive: edit intact
				await expect(surface.dirtyInput(page)).toHaveValue('guard-test')
			})
		}

		test('should leave via Cancel→Discard without a second, native dialog', async ({
			page
		}) => {
			// Cancel runs history.back(); hard-loaded entries make that a
			// document unload, so a leaky bypass would surface here as a
			// beforeunload dialog after the user already confirmed Discard.
			await page.goto('/agent/research')
			await page.goto('/agent/new')
			const name = page.getByLabel(/name/i)
			await name.click()
			await name.pressSequentially('cancel-test')

			const dialogs: string[] = []
			await page.once('dialog', (dialog) => {
				dialogs.push(dialog.type())
				dialog.accept()
			})

			await page.getByRole('button', { name: 'Cancel' }).click()
			await page.getByRole('button', { name: 'Discard' }).click()

			// The bypass covers the leave: no native dialog, back lands
			expect(dialogs).toEqual([])
			await expect(page).toHaveURL(/\/agent\/research$/)
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
