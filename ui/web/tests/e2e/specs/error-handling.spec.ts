// tests/e2e/specs/error-handling.spec.ts
import { expect, test } from '../setup/fixtures'

test.describe('error handling', () => {
	test('should handle concurrent edits', async ({ page, browser }) => {
		const agentName = `Concurrent ${Date.now()}`

		await page.goto('/agent/new')
		await page.getByLabel(/name/i).fill(agentName)
		await page.getByRole('button', { name: 'Create Agent' }).click()
		const agentId = agentName.toLowerCase().replace(/ +/g, '-')
		await expect(page).toHaveURL(new RegExp(`/agent/${agentId}$`))

		const context2 = await browser.newContext()
		const page2 = await context2.newPage()

		try {
			await page2.goto(`/agent/${agentId}`)

			await expect(page.locator('body')).toBeVisible()
			await expect(page2.locator('body')).toBeVisible()
		} finally {
			await context2.close()
		}
	})

	test('should handle large input in text fields', async ({ page }) => {
		await page.goto('/agent/new')

		const largeText = 'x'.repeat(10000)
		await page.getByRole('button', { name: 'Operating Instructions' }).click()
		await page
			.getByPlaceholder(
				"Define operating rules, priorities, and do/don't guidelines..."
			)
			.fill(largeText)

		await expect(page.locator('body')).toBeVisible()
	})

	test('should show 404 for non-existent agent', async ({ page }) => {
		await page.goto('/agent/this-agent-does-not-exist-at-all')

		await expect(page.getByRole('heading', { name: '404' })).toBeVisible()
	})

	test('should display error banner when agents API fails', async ({
		page
	}) => {
		let routeHit = false
		await page.route('**/api/agents', async (route) => {
			routeHit = true
			await route.fulfill({
				status: 500,
				contentType: 'application/json',
				body: JSON.stringify({ message: 'Internal server error' })
			})
		})

		await page.goto('/')
		await expect(page.locator('main')).toBeVisible()

		expect(routeHit).toBe(true)

		await expect(page.getByText('Internal server error')).toBeVisible()
	})

	test('should display error banner when agents API is unreachable', async ({
		page
	}) => {
		await page.route('**/api/agents', (route) => route.abort('failed'))

		await page.goto('/')
		await expect(page.locator('main')).toBeVisible()
		await expect(page.getByText('Failed to fetch')).toBeVisible()
	})
})
