// tests/e2e/specs/api.spec.ts
import { expect, test } from '../setup/fixtures'

test.describe('API integration', () => {
	test('should call agents list API on app load', async ({ page }) => {
		const requestPromise = page.waitForRequest('**/api/user/agents')

		await page.goto('/')
		const request = await requestPromise

		expect(request.url()).toContain('/api/user/agents')
	})

	test('should call create agent API with correct payload', async ({
		page
	}) => {
		let requestBody: unknown

		await page.route('**/api/user/agent', (route, request) => {
			if (request.method() === 'POST') {
				requestBody = request.postDataJSON()
			}
			route.continue()
		})

		const agentName = `Api Test ${Date.now()}`
		await page.goto('/agent/new')
		await page.getByLabel(/name/i).fill(agentName)

		// Verified intermediate step guards against pre-hydration clicks
		await page.getByRole('button', { name: 'Soul' }).click()
		await expect(page.getByLabel(/personality/i)).toBeVisible()

		await page.getByRole('button', { name: 'Operating Instructions' }).click()
		await page
			.getByPlaceholder(
				"Define operating rules, priorities, and do/don't guidelines..."
			)
			.fill('Test instructions')
		const responsePromise = page.waitForResponse('**/api/user/agent')
		await page.getByRole('button', { name: 'Create Agent' }).click()

		// Wait for API call
		await responsePromise
		expect(requestBody).toBeDefined()
		expect(requestBody).toHaveProperty(
			'operatingInstructions',
			'Test instructions'
		)
		expect(requestBody).toHaveProperty('name', agentName)
	})

	test('should handle PATCH for agent update', async ({ page }) => {
		// Create agent first
		const agentName = `Patch Test ${Date.now()}`
		const agentId = agentName.toLowerCase().replace(/ +/g, '-')
		await page.goto('/agent/new')
		await page.getByLabel(/name/i).fill(agentName)
		await page.getByRole('button', { name: 'Create Agent' }).click()
		await expect(page).toHaveURL(new RegExp(`/agent/${agentId}$`))

		// Now update it
		let patchCalled = false
		await page.route('**/api/user/agent/*', (route, request) => {
			if (request.method() === 'PATCH') {
				patchCalled = true
			}
			route.continue()
		})

		const editButton = page.getByRole('button', {
			name: 'Open agent configuration'
		})
		await expect(editButton).toBeVisible()
		await editButton.click()
		await page.getByRole('button', { name: 'Operating Instructions' }).click()
		await page
			.getByPlaceholder(
				"Define operating rules, priorities, and do/don't guidelines..."
			)
			.fill('Patched instructions')
		await page.getByRole('button', { name: 'Save' }).click()
		expect(patchCalled).toBe(true)
	})

	test('should include correct Content-Type header', async ({ page }) => {
		let contentType: string | undefined

		await page.route('**/api/user/agent', (route, request) => {
			contentType = request.headers()['content-type']
			route.continue()
		})
		await page.goto('/agent/new')
		await page.getByLabel(/name/i).fill(`Content Type ${Date.now()}`)
		const responsePromise = page.waitForResponse('**/api/user/agent')
		await page.getByRole('button', { name: 'Create Agent' }).click()

		await responsePromise

		expect(contentType).toContain('application/json')
	})
})
