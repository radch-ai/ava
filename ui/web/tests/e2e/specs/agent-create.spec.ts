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
		const agentName = `Test Agent ${Date.now()}`

		// Fill in required name field
		await page.getByLabel(/name/i).fill(agentName)
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Should navigate to the id derived from the display name
		await expect(page).toHaveURL(
			new RegExp(`/agent/${agentName.toLowerCase().replace(/ +/g, '-')}$`)
		)
	})

	test('should create agent with full configuration', async ({ page }) => {
		const agentName = `Full Config Agent ${Date.now()}`

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
		await expect(page).toHaveURL(
			new RegExp(`/agent/${agentName.toLowerCase().replace(/ +/g, '-')}$`)
		)
	})

	test('should show random icon selected by default', async ({ page }) => {
		// Trigger announces the (randomly chosen) default icon in its name
		const iconButton = page.getByRole('button', {
			name: /^Icon: [\w-]+\. Change icon\.$/
		})
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

	test('should reject names with symbols and show a hint', async ({ page }) => {
		await page.getByLabel(/name/i).fill('My Test Agent!')
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Symbols are invalid: stay on the form and explain the rule
		await expect(page).toHaveURL(/\/agent\/new/)
		await expect(
			page.getByText(/letters, digits and single spaces only/i)
		).toBeVisible()
	})

	test('should surface the duplicate-name error without auto-suffixing', async ({
		page
	}) => {
		const baseName = `Duplicate Test ${Date.now()}`
		const agentId = baseName.toLowerCase().replace(/ +/g, '-')

		// Create first agent
		await page.getByLabel(/name/i).fill(baseName)
		await page.getByRole('button', { name: 'Create Agent' }).click()
		await expect(page).toHaveURL(new RegExp(`/agent/${agentId}$`))

		// Create second agent with the same name
		await page.goto('/agent/new')
		await page.getByLabel(/name/i).fill(baseName)
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// The server rejects the duplicate; the user must rename manually
		await expect(page).toHaveURL(/\/agent\/new/)
		await expect(page.getByText(/already exists/i)).toBeVisible()
	})

	test('should trim whitespace from name', async ({ page }) => {
		await page.getByLabel(/name/i).fill('  Trimmed Agent  ')
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Should navigate to the trimmed, derived id
		await expect(page).toHaveURL(/\/agent\/trimmed-agent$/)
	})

	test('should reject names with mixed symbols', async ({ page }) => {
		await page.getByLabel(/name/i).fill('Test@Agent#123!')
		await page.getByRole('button', { name: 'Create Agent' }).click()

		await expect(page).toHaveURL(/\/agent\/new/)
		await expect(
			page.getByText(/letters, digits and single spaces only/i)
		).toBeVisible()
	})
})
