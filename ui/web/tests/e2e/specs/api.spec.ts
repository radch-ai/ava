// tests/e2e/specs/api.spec.ts
import { test, expect } from '../setup/fixtures';

test.describe('API integration', () => {
	test('should call agents list API on app load', async ({ page }) => {
		const requestPromise = page.waitForRequest('**/api/user/agents');

		await page.goto('/');
		const request = await requestPromise;

		expect(request.url()).toContain('/api/user/agents');
	});

	test('should call create agent API with correct payload', async ({ page }) => {
		let requestBody: unknown;

		await page.route('**/api/user/agent/*', (route, request) => {
			if (request.method() === 'POST') {
				requestBody = request.postDataJSON();
			}
			route.continue();
		});

		const agentName = `api-test-${Date.now()}`;
		await page.goto('/agent/new');
		await page.getByLabel(/name/i).fill(agentName);
		await page.getByRole('button', { name: 'Operating Instructions' }).click();
		await page
			.getByPlaceholder("Define operating rules, priorities, and do/don't guidelines...")
			.fill('Test instructions');
		await page.getByRole('button', { name: 'Create Agent' }).click();

		// Wait for API call
		await page.waitForResponse('**/api/user/agent/*');

		expect(requestBody).toBeDefined();
		expect(requestBody).toHaveProperty('operatingInstructions', 'Test instructions');
	});

	test('should handle PATCH for agent update', async ({ page }) => {
		// Create agent first
		const agentName = `patch-test-${Date.now()}`;
		await page.goto('/agent/new');
		await page.getByLabel(/name/i).fill(agentName);
		await page.getByRole('button', { name: 'Create Agent' }).click();
		await expect(page).toHaveURL(new RegExp(`/agent/${agentName}`));

		// Now update it
		let patchCalled = false;
		await page.route('**/api/user/agent/*', (route, request) => {
			if (request.method() === 'PATCH') {
				patchCalled = true;
			}
			route.continue();
		});

		const editButton = page.getByRole('button', { name: 'Open agent configuration' });
		await expect(editButton).toBeVisible();
		await editButton.click();
		await page.getByRole('button', { name: 'Operating Instructions' }).click();
		await page
			.getByPlaceholder("Define operating rules, priorities, and do/don't guidelines...")
			.fill('Patched instructions');
		await page.getByRole('button', { name: 'Save' }).click();
		expect(patchCalled).toBe(true);
	});

	test('should include correct Content-Type header', async ({ page }) => {
		let contentType: string | undefined;

		await page.route('**/api/user/agent/*', (route, request) => {
			contentType = request.headers()['content-type'];
			route.continue();
		});

		await page.goto('/agent/new');
		await page.getByLabel(/name/i).fill(`content-type-${Date.now()}`);
		await page.getByRole('button', { name: 'Create Agent' }).click();

		await page.waitForResponse('**/api/user/agent/*');

		expect(contentType).toContain('application/json');
	});
});
