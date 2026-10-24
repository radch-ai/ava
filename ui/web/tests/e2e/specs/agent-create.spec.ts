// tests/e2e/specs/agent-create.spec.ts
import { expect, test } from '../setup/fixtures'

test.describe('agent creation', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/agent/new')
	})

	test('should display new agent form', async ({ page }) => {
		await expect(page.getByRole('heading', { name: 'New Agent' })).toBeVisible()
		await expect(page.getByLabel(/name/i)).toBeVisible()
		await expect(
			page.getByRole('button', { name: 'Create Agent' })
		).toBeVisible()
	})

	test('should create agent with minimal required fields', async ({ page }) => {
		const agentName = `test-agent-${Date.now()}`

		// Fill in required name field
		await page.getByLabel(/name/i).fill(agentName)
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Should navigate to agent detail page
		await expect(page).toHaveURL(new RegExp(`/agent/${agentName}`))
	})

	test('should create agent with full configuration', async ({ page }) => {
		const agentName = `full-config-agent-${Date.now()}`

		// Fill all fields
		await page.getByLabel(/name/i).fill(agentName)

		// Personality
		await page.getByRole('button', { name: 'Soul' }).click()
		await page.getByLabel(/personality/i).fill('Helpful and friendly')
		await page.getByLabel(/identity/i).fill('AI assistant')

		// Operating instructions
		await page.getByRole('button', { name: 'Operating Instructions' }).click()
		await page
			.getByPlaceholder(
				"Define operating rules, priorities, and do/don't guidelines..."
			)
			.fill('Always be helpful')

		// Tools context
		await page.getByRole('button', { name: 'Tools Context' }).click()
		await page
			.getByPlaceholder('E.g., "Calendar: Google Calendar. CRM: HubSpot."')
			.fill('Calendar: Google Calendar')

		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Should navigate to agent detail page
		await expect(page).toHaveURL(new RegExp(`/agent/${agentName}`))
	})

	test('should show random icon selected by default', async ({ page }) => {
		// Icon picker should show a selected icon
		const iconButton = page.getByRole('button', { name: 'Select icon' })
		await expect(iconButton).toBeVisible()
		// Icon should have some icon element inside
		await expect(iconButton.locator('svg')).toBeVisible()
	})
})

test.describe('agent creation validation', () => {
	test.beforeEach(async ({ page }) => {
		await page.goto('/agent/new')
	})

	test('should not submit with empty name', async ({ page }) => {
		// Try to submit without name
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Should stay on same page
		await expect(page).toHaveURL(/\/agent\/new/)
	})

	test('should sanitize agent name - lowercase and dashes', async ({
		page
	}) => {
		await page.getByLabel(/name/i).fill('My Test Agent!')
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Should navigate to sanitized name
		await expect(page).toHaveURL(/\/agent\/my-test-agent/)
	})

	test('should handle duplicate agent names', async ({ page }) => {
		const baseName = `duplicate-test-${Date.now()}`

		// Create first agent
		await page.getByLabel(/name/i).fill(baseName)
		await page.getByRole('button', { name: 'Create Agent' }).click()
		await expect(page).toHaveURL(new RegExp(`/agent/${baseName}`))

		// Create second agent with same name
		await page.goto('/agent/new')
		await page.getByLabel(/name/i).fill(baseName)
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Should navigate to agent with suffix (e.g., duplicate-test-123-2)
		await expect(page).toHaveURL(new RegExp(`/agent/${baseName}-\\d+$`))
	})

	test('should trim whitespace from name', async ({ page }) => {
		await page.getByLabel(/name/i).fill('  trimmed-agent  ')
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Should navigate to trimmed name
		await expect(page).toHaveURL(/\/agent\/trimmed-agent/)
	})

	test('should handle special characters in name gracefully', async ({
		page
	}) => {
		await page.getByLabel(/name/i).fill('Test@Agent#123!')
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Special characters should be stripped
		await expect(page).toHaveURL(/\/agent\/test-agent-123/)
	})
})
