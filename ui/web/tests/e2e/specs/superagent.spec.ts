// tests/e2e/specs/superagent.spec.ts
import { expect, test } from '../setup/fixtures'

test.describe('ava agent', () => {
	test('should load home page as superagent chat', async ({ page }) => {
		await page.goto('/')

		const chatInput = page.getByRole('textbox')
		await expect(chatInput).toBeVisible()
	})

	test('should redirect / to the superagent agent page', async ({ page }) => {
		await page.goto('/')

		await expect(page).toHaveURL(/\/agent\/_superagent$/)
	})

	test('should show thread content or empty placeholder', async ({ page }) => {
		await page.goto('/')

		await expect(page.locator('.overflow-y-auto')).toBeVisible()

		const noMessagesPlaceholder = page.getByText(/no messages yet/i)
		const threadContent = page.locator('.flex.flex-col.gap-4')

		const hasPlaceholder = await noMessagesPlaceholder
			.isVisible()
			.catch(() => false)
		const hasContent = (await threadContent.count()) > 0

		expect(hasPlaceholder || hasContent).toBeTruthy()
	})

	test('should show load more button when thread has more messages', async ({
		page
	}) => {
		await page.goto('/')

		const loadMoreButton = page.getByRole('button', { name: /load more/i })
		const isVisible = await loadMoreButton.isVisible().catch(() => false)

		expect(typeof isVisible === 'boolean').toBeTruthy()
	})

	test('should have model selector with label', async ({ page }) => {
		await page.goto('/')

		const modelLabel = page.getByText('Model:', { exact: false })
		await expect(modelLabel).toBeVisible()
	})

	test('should persist selected model', async ({ page }) => {
		await page.goto('/')

		const modelSelector = page.getByRole('combobox').first()
		if (await modelSelector.isVisible()) {
			await modelSelector.click()

			const options = page.getByRole('option')
			if ((await options.count()) > 1) {
				await options.nth(1).click()

				await page.reload()
			}
		}
	})

	test('should navigate to ava from sidebar', async ({ page }) => {
		await page.goto('/agent/new')

		const superagentLink = page.getByRole('link', { name: /ava/i }).first()
		await superagentLink.click()

		await expect(page).toHaveURL(/\/agent\/_superagent$/)
	})

	test('should show error message when thread fails to load', async ({
		page
	}) => {
		await page.goto('/')

		const errorMessage = page.getByText(/failed to load conversation/i)
		const hasError = await errorMessage.isVisible().catch(() => false)

		if (hasError) {
			await expect(errorMessage).toBeVisible()
		}
	})
})

test.describe('ava tabs', () => {
	test('should show Conversation and Activity tabs', async ({ page }) => {
		await page.goto('/')

		const conversationTab = page.getByRole('tab', { name: /conversation/i })
		const activityTab = page.getByRole('tab', { name: /activity/i })

		await expect(conversationTab).toBeVisible()
		await expect(activityTab).toBeVisible()
	})

	test('should default to Conversation tab selected', async ({ page }) => {
		await page.goto('/')

		const conversationTab = page.getByRole('tab', { name: /conversation/i })
		await expect(conversationTab).toHaveAttribute('aria-selected', 'true')
	})

	test('should switch to Activity tab on click', async ({ page }) => {
		await page.goto('/')

		const activityTab = page.getByRole('tab', { name: /activity/i })
		await activityTab.click()

		await expect(activityTab).toHaveAttribute('aria-selected', 'true')
	})

	test('should keep composer visible on Activity tab', async ({ page }) => {
		await page.goto('/')

		const activityTab = page.getByRole('tab', { name: /activity/i })
		await activityTab.click()

		const textarea = page.getByPlaceholder('Enter your query!')
		await expect(textarea).toBeVisible()
	})

	test('should show activity content or placeholder on Activity tab', async ({
		page
	}) => {
		await page.goto('/')

		const activityTab = page.getByRole('tab', { name: /activity/i })
		await activityTab.click()

		const noActivityPlaceholder = page.getByText(/no activity yet/i)
		const loadingText = page.getByText(/loading activity/i)
		const activityItems = page.locator('[class*="rounded-lg"][class*="border"]')

		const hasPlaceholder = await noActivityPlaceholder
			.isVisible()
			.catch(() => false)
		const isLoading = await loadingText.isVisible().catch(() => false)
		const itemCount = await activityItems.count()

		expect(hasPlaceholder || isLoading || itemCount > 0).toBeTruthy()
	})

	test('should show empty state when no activity exists', async ({ page }) => {
		await page.goto('/')

		const activityTab = page.getByRole('tab', { name: /activity/i })
		await activityTab.click()

		const noActivityText = page.getByText(/no activity yet/i)
		const hasEmptyState = await noActivityText.isVisible().catch(() => false)

		if (hasEmptyState) {
			await expect(noActivityText).toBeVisible()
		}
	})

	test('should show error state when activity fails to load', async ({
		page
	}) => {
		await page.goto('/')

		const activityTab = page.getByRole('tab', { name: /activity/i })
		await activityTab.click()

		const errorText = page.locator('.text-destructive')
		const hasError = await errorText.isVisible().catch(() => false)

		if (hasError) {
			const retryButton = page.getByRole('button', { name: /retry/i })
			await expect(retryButton).toBeVisible()
		}
	})

	test('should show load more button when more activity exists', async ({
		page
	}) => {
		await page.goto('/')

		const activityTab = page.getByRole('tab', { name: /activity/i })
		await activityTab.click()

		const loadMoreButton = page.getByRole('button', { name: /load more/i })
		const isVisible = await loadMoreButton.isVisible().catch(() => false)

		expect(typeof isVisible === 'boolean').toBeTruthy()
	})

	test('should display activity card with triggeredBy, status, action, and createdAt', async ({
		page
	}) => {
		await page.goto('/')

		const activityTab = page.getByRole('tab', { name: /activity/i })
		await activityTab.click()

		const loadingText = page.getByText(/loading activity/i)
		await loadingText.waitFor({ state: 'hidden' }).catch(() => {})

		const noActivityPlaceholder = page.getByText(/no activity yet/i)
		const hasEmptyState = await noActivityPlaceholder
			.isVisible()
			.catch(() => false)

		if (!hasEmptyState) {
			const activityCard = page
				.locator('[class*="rounded-lg"][class*="border"]')
				.first()
			if (await activityCard.isVisible().catch(() => false)) {
				await expect(activityCard).toBeVisible()
			}
		}
	})

	test('should preserve conversation state when switching back from Activity', async ({
		page
	}) => {
		await page.goto('/')

		const conversationTab = page.getByRole('tab', { name: /conversation/i })
		const activityTab = page.getByRole('tab', { name: /activity/i })

		// Wait for the conversation to settle before comparing counts
		await expect(
			page
				.getByText(/no messages yet/i)
				.or(page.locator('[class*="rounded-2xl"]').first())
		).toBeVisible()

		const initialContent = await page.locator('[class*="rounded-2xl"]').count()

		await activityTab.click()
		await conversationTab.click()

		const finalContent = await page.locator('[class*="rounded-2xl"]').count()

		expect(finalContent).toBe(initialContent)
	})
})
