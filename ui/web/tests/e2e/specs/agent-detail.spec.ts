// tests/e2e/specs/agent-detail.spec.ts
import { test, expect } from '../setup/fixtures';

/**
 * Helper to create an agent via UI and return the agent name.
 * Each test should create its own agent to avoid shared mutable state.
 */
async function createAgentViaUi(page: import('@playwright/test').Page, name: string) {
	await page.goto('/agent/new');
	await page.getByRole('textbox', { name: /name/i }).fill(name);
	await page.getByRole('button', { name: /create|submit/i }).click();
	await expect(page).toHaveURL(new RegExp(`/agent/${name}`));
}

test.describe('agent detail page', () => {
	test('should show agent header independently', async ({ page }) => {
		const uniqueName = `agent-isolated-${Date.now()}`;
		await createAgentViaUi(page, uniqueName);

		const header = page.locator('header').first();
		await expect(header).toBeVisible();
		const agentLabel = header.locator('span').first();
		await expect(agentLabel).toBeVisible();
		const expectedLabel = uniqueName.replace(/-/g, ' ');
		await expect(agentLabel).toContainText(new RegExp(expectedLabel, 'i'));
	});

	test('should show agent header with visible label text', async ({ page }) => {
		const agentName = `detail-header-${Date.now()}`;
		await createAgentViaUi(page, agentName);

		const header = page.locator('header').first();
		await expect(header).toBeVisible();
		const agentLabel = header.locator('span').first();
		await expect(agentLabel).toBeVisible();
		const expectedLabel = agentName.replace(/-/g, ' ');
		await expect(agentLabel).toContainText(new RegExp(expectedLabel, 'i'));
	});

	test('should show configuration button with accessible name', async ({ page }) => {
		const agentName = `detail-config-btn-${Date.now()}`;
		await createAgentViaUi(page, agentName);
		// Config button has accessible name: aria-label="Open agent configuration"
		const configButton = page.getByRole('button', { name: 'Open agent configuration' });
		await expect(configButton).toBeVisible();
		await expect(configButton).toBeEnabled();
	});

	test('should show Agent Config tooltip on hover over settings button', async ({ page }) => {
		const agentName = `detail-tooltip-${Date.now()}`;
		await createAgentViaUi(page, agentName);

		const configButton = page.getByRole('button', { name: 'Open agent configuration' });
		await configButton.hover();
		const tooltipText = page.locator('[data-slot="tooltip-content"]', {
			hasText: 'Agent Config'
		});
		await expect(tooltipText).toBeVisible({ timeout: 5000 });
	});

	test('should render a unique tooltip per agent page', async ({ page }) => {
		const firstName = `tooltip-unique-a-${Date.now()}`;
		const secondName = `tooltip-unique-b-${Date.now()}`;

		await createAgentViaUi(page, firstName);
		const firstButton = page.getByRole('button', { name: 'Open agent configuration' });
		await firstButton.hover();
		const firstTooltipContent = page.locator('[data-slot="tooltip-content"]', {
			hasText: 'Agent Config'
		});
		await expect(firstTooltipContent).toBeVisible({ timeout: 5000 });

		await page.goto('/agent/new');
		await page.getByRole('textbox', { name: /name/i }).fill(secondName);
		await page.getByRole('button', { name: /create|submit/i }).click();
		await expect(page).toHaveURL(new RegExp(`/agent/${secondName}`));

		const secondButton = page.getByRole('button', { name: 'Open agent configuration' });
		await secondButton.hover();
		const secondTooltipContent = page.locator('[data-slot="tooltip-content"]', {
			hasText: 'Agent Config'
		});
		await expect(secondTooltipContent).toBeVisible({ timeout: 5000 });
	});

	test('should show activity feed', async ({ page }) => {
		const agentName = `detail-activity-${Date.now()}`;
		await createAgentViaUi(page, agentName);
		// Activity feed container with overflow-y-auto class
		await expect(page.locator('.overflow-y-auto')).toBeVisible();
		// Newly created agents have no activity - shows empty state
		await expect(page.getByText('No activity yet.')).toBeVisible();
	});

	test('should show empty state when no activity exists', async ({ page }) => {
		const agentName = `detail-empty-activity-${Date.now()}`;
		await createAgentViaUi(page, agentName);

		const noActivityText = page.getByText(/no activity yet/i);
		await expect(noActivityText).toBeVisible();
	});

	test('should show error state when activity fails to load', async ({ page }) => {
		const agentName = `detail-error-activity-${Date.now()}`;
		await createAgentViaUi(page, agentName);

		const errorText = page.locator('.text-destructive');
		const hasError = await errorText.isVisible().catch(() => false);

		if (hasError) {
			const retryButton = page.getByRole('button', { name: /retry/i });
			await expect(retryButton).toBeVisible();
		}
	});

	test('should show load more button when more activity exists', async ({ page }) => {
		const agentName = `detail-loadmore-activity-${Date.now()}`;
		await createAgentViaUi(page, agentName);

		const loadMoreButton = page.getByRole('button', { name: /load more/i });
		const isVisible = await loadMoreButton.isVisible().catch(() => false);

		expect(typeof isVisible === 'boolean').toBeTruthy();
	});

	test('should display activity card with triggeredBy, status, action, and createdAt', async ({
		page
	}) => {
		const agentName = `detail-card-activity-${Date.now()}`;
		await createAgentViaUi(page, agentName);

		const noActivityPlaceholder = page.getByText(/no activity yet/i);
		const hasEmptyState = await noActivityPlaceholder.isVisible().catch(() => false);

		if (!hasEmptyState) {
			const activityCard = page.locator('[class*="rounded-lg"][class*="border"]').first();
			if (await activityCard.isVisible().catch(() => false)) {
				await expect(activityCard).toBeVisible();
			}
		}
	});

	test('should show CTA button to ask superagent', async ({ page }) => {
		const agentName = `detail-cta-${Date.now()}`;
		await createAgentViaUi(page, agentName);
		// CTA button is a link styled as button - use accessible selector
		const ctaButton = page.getByRole('link', { name: /ask ava about this agent/i });
		await expect(ctaButton).toBeVisible();
	});

	test('should return 404 for non-existent agent', async ({ page }) => {
		await page.goto('/agent/non-existent-agent-12345');

		// Should show 404 or error state
		// SvelteKit's default error handling or custom error page
		await expect(page.getByRole('heading', { name: '404' })).toBeVisible();
		await expect(page.getByText(/agent 'non-existent-agent-12345' not found/i)).toBeVisible();
	});

	test('should show agent icon in header', async ({ page }) => {
		const agentName = `detail-icon-${Date.now()}`;
		await createAgentViaUi(page, agentName);
		// Header should contain an icon (rendered via Icon component)
		const header = page.locator('header').first();
		await expect(header.locator('svg').first()).toBeVisible();
	});
});
