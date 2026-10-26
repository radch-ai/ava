import { afterEach, describe, expect, it } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import ModelSelector from '#components/ModelSelector.svelte'

describe('ModelSelector', () => {
	afterEach(async () => {
		cleanup()
	})

	// Helper to get trigger (role=combobox is nameFrom:author, so no name filter)
	function getTrigger() {
		return page.getByRole('combobox')
	}

	it('renders with default model selected (auto)', async () => {
		render(ModelSelector)

		const trigger = getTrigger()
		await expect.element(trigger).toBeVisible()
		await expect.element(trigger).toHaveTextContent('auto')
	})

	it('shows tick only on selected item', async () => {
		render(ModelSelector)

		const trigger = getTrigger()
		await trigger.click()

		await expect.element(page.getByRole('listbox')).toBeVisible()

		// First option (Auto) should have the check icon (rendered as SVG)
		const firstOption = page.getByRole('option').nth(0)
		// Check that the option contains an SVG (the check icon)
		const optionElement = firstOption.element()
		const svg = optionElement.querySelector('svg')
		expect(svg).not.toBeNull()
	})

	it('closes dropdown after selecting an item', async () => {
		render(ModelSelector)

		const trigger = getTrigger()
		await trigger.click()

		await expect.element(page.getByRole('listbox')).toBeVisible()

		// Click on second item (GPT-4, since Auto is now first)
		const secondItem = page.getByRole('option').nth(1)
		await secondItem.click()

		// Check trigger state is closed
		await expect.element(trigger).toHaveAttribute('data-state', 'closed')
	})

	it('shows selected model name in trigger', async () => {
		render(ModelSelector)

		const trigger = getTrigger()
		await trigger.click()

		await expect.element(page.getByRole('listbox')).toBeVisible()

		// Click on "GPT-4" (second item after Auto)
		const gpt4Item = page.getByRole('option').nth(1)
		await gpt4Item.click()

		// Trigger should now show "GPT-4"
		await expect.element(trigger).toHaveTextContent('GPT-4')
	})

	it('selected item is highlighted when reopening', async () => {
		render(ModelSelector)

		const trigger = getTrigger()
		await trigger.click()

		await expect.element(page.getByRole('listbox')).toBeVisible()

		// Click on third item (GPT-3.5 Turbo, since Auto is first and GPT-4 is second)
		const thirdItem = page.getByRole('option').nth(2)
		await thirdItem.click()

		// Open dropdown again
		await trigger.click()

		await expect.element(page.getByRole('listbox')).toBeVisible()

		// Third item should have check icon (indicates selected)
		const selectedItem = page.getByRole('option').nth(2)
		const optionElement = selectedItem.element()
		const svg = optionElement.querySelector('svg')
		expect(svg).not.toBeNull()
	})

	it('displays models grouped by provider with Auto option first', async () => {
		render(ModelSelector)

		const trigger = getTrigger()
		await trigger.click()

		await expect.element(page.getByRole('listbox')).toBeVisible()

		// Check that group headings are present
		await expect
			.element(page.getByRole('group', { name: /OpenAI/ }))
			.toBeVisible()
		await expect
			.element(page.getByRole('group', { name: /Anthropic/ }))
			.toBeVisible()
		await expect
			.element(page.getByRole('group', { name: /Gemini/ }))
			.toBeVisible()
		await expect
			.element(page.getByRole('group', { name: /Grok/ }))
			.toBeVisible()

		// Verify all options are present (auto + 6 models = 7 total)
		const options = page.getByRole('option')
		await expect.element(options.nth(0)).toHaveTextContent('auto')
		await expect.element(options.nth(1)).toHaveTextContent('GPT-4')
		await expect.element(options.nth(2)).toHaveTextContent('GPT-3.5 Turbo')
		await expect.element(options.nth(3)).toHaveTextContent('Claude 3 Sonnet')
		await expect.element(options.nth(4)).toHaveTextContent('Gemini Pro')
		await expect.element(options.nth(5)).toHaveTextContent('Gemini Ultra')
		await expect.element(options.nth(6)).toHaveTextContent('Grok-1')
	})
})
