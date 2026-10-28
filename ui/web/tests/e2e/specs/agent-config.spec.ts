// tests/e2e/specs/agent-config.spec.ts
import type { Page } from '@playwright/test'
import { expect, test } from '../setup/fixtures'

/**
 * Helper to create an agent via UI.
 * Each test should create its own agent to avoid shared mutable state.
 */
async function createAgentViaUi(page: Page, name: string) {
	const agentId = name.toLowerCase().replace(/ +/g, '-')
	await page.goto('/agent/new')
	await page.getByLabel(/name/i).fill(name)
	await page.getByRole('button', { name: 'Create Agent' }).click()
	await expect(page).toHaveURL(new RegExp(`/agent/${agentId}$`))
}

test.describe('agent configuration', () => {
	test.describe('configuration sheet', () => {
		test('should open configuration sheet from accessible button', async ({
			page
		}) => {
			const agentName = `Config Sheet ${Date.now()}`
			await createAgentViaUi(page, agentName)

			// Click config button using accessible name
			const configButton = page.getByRole('button', {
				name: 'Open agent configuration'
			})
			await expect(configButton).toBeVisible()
			await configButton.click()

			// Wait for config sheet to open (async config fetch may occur)
			// Sheet uses role="dialog" via the Sheet component
			const sheet = page.getByRole('dialog', { name: /agent configuration/i })
			await expect(sheet).toBeVisible({ timeout: 10000 })
		})
	})

	test.describe('unsaved changes protection', () => {
		async function openSheetWithChanges(page: Page, agentName: string) {
			await createAgentViaUi(page, agentName)
			const configButton = page.getByRole('button', {
				name: 'Open agent configuration'
			})
			await configButton.click()
			const sheet = page.getByRole('dialog', { name: /agent configuration/i })
			await expect(sheet).toBeVisible({ timeout: 10000 })
			await sheet.getByRole('button', { name: 'Soul' }).click()
			await sheet.getByLabel(/personality/i).fill('Modified personality')
			return sheet
		}

		test('should show dialog on Cancel with unsaved changes and discard closes sheet', async ({
			page
		}) => {
			const agentName = `Cancel Dialog ${Date.now()}`
			const sheet = await openSheetWithChanges(page, agentName)

			await sheet.getByRole('button', { name: 'Cancel' }).click()

			await expect(page.getByRole('alertdialog')).toBeVisible()

			await page.getByRole('button', { name: 'Discard' }).click()
			await expect(sheet).not.toBeVisible({ timeout: 5000 })
		})

		test('should keep editing when Keep Editing clicked', async ({ page }) => {
			const agentName = `Keep Editing ${Date.now()}`
			const sheet = await openSheetWithChanges(page, agentName)

			await sheet.getByRole('button', { name: 'Cancel' }).click()
			await page.getByRole('button', { name: 'Keep Editing' }).click()

			await expect(page.getByRole('alertdialog')).not.toBeVisible()
			await expect(sheet).toBeVisible()
		})
	})

	test.describe('field editing', () => {
		test('should edit personality field', async ({ page }) => {
			const agentName = `Config Personality ${Date.now()}`
			await createAgentViaUi(page, agentName)

			// Open config sheet first
			const configButton = page.getByRole('button', {
				name: 'Open agent configuration'
			})
			await configButton.click()

			// Wait for sheet to appear
			const sheet = page.getByRole('dialog', { name: /agent configuration/i })
			await expect(sheet).toBeVisible({ timeout: 10000 })

			await sheet.getByRole('button', { name: 'Soul' }).click()
			const personalityField = sheet.getByLabel(/personality/i)
			await expect(personalityField).toBeVisible()
			await personalityField.fill('Updated personality for testing')
			await sheet.getByRole('button', { name: 'Save' }).click()
		})

		test('should edit operating instructions', async ({ page }) => {
			const agentName = `Config Instructions ${Date.now()}`
			await createAgentViaUi(page, agentName)

			// Open config sheet first
			const configButton = page.getByRole('button', {
				name: 'Open agent configuration'
			})
			await configButton.click()

			// Wait for sheet to appear
			const sheet = page.getByRole('dialog', { name: /agent configuration/i })
			await expect(sheet).toBeVisible({ timeout: 10000 })

			await sheet
				.getByRole('button', { name: 'Operating Instructions' })
				.click()
			const instructionsField = sheet.getByPlaceholder(
				"Define operating rules, priorities, and do/don't guidelines..."
			)
			await expect(instructionsField).toBeVisible()
			await instructionsField.fill('New operating instructions')
			await sheet.getByRole('button', { name: 'Save' }).click()
		})

		test('should preserve changes after save', async ({ page }) => {
			const testValue = `Preserved value ${Date.now()}`
			const agentName = `Config Preserve ${Date.now()}`
			await createAgentViaUi(page, agentName)

			// Open config sheet first
			const configButton = page.getByRole('button', {
				name: 'Open agent configuration'
			})
			await configButton.click()

			// Wait for sheet to appear
			const sheet = page.getByRole('dialog', { name: /agent configuration/i })
			await expect(sheet).toBeVisible({ timeout: 10000 })

			await sheet
				.getByRole('button', { name: 'Operating Instructions' })
				.click()
			const field = sheet.getByPlaceholder(
				"Define operating rules, priorities, and do/don't guidelines..."
			)
			await expect(field).toBeVisible()
			await field.fill(testValue)
			await sheet.getByRole('button', { name: 'Save' }).click()

			await expect(sheet).not.toBeVisible({ timeout: 5000 })

			await page.reload()
			await page
				.getByRole('button', { name: 'Open agent configuration' })
				.click()
			await expect(sheet).toBeVisible({ timeout: 10000 })
			await sheet
				.getByRole('button', { name: 'Operating Instructions' })
				.click()
			await expect(
				sheet.getByPlaceholder(
					"Define operating rules, priorities, and do/don't guidelines..."
				)
			).toHaveValue(testValue)
		})
	})
})
