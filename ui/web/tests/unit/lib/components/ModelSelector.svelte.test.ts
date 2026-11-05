import { afterEach, describe, expect, it } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import ModelSelector from '#components/ModelSelector.svelte'
import type { ModelGroup } from '#lib/models'

describe('ModelSelector', () => {
	const testModelGroups: ModelGroup[] = [
		{
			provider: 'OpenAI',
			models: ['gpt-4', 'gpt-3.5-turbo']
		},
		{
			provider: 'Anthropic',
			models: ['claude-3-sonnet']
		}
	]

	afterEach(async () => {
		cleanup()
	})

	describe('rendering', () => {
		it('renders the selector', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')
			await expect.element(button).toBeVisible()
		})

		it('shows default model (auto) when no value is provided', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')
			await expect.element(button).toHaveTextContent('auto')
		})

		it('shows current selected model name when value is set', async () => {
			render(ModelSelector, {
				value: 'gpt-4',
				modelGroups: testModelGroups
			})

			const button = page.getByRole('combobox')
			await expect.element(button).toHaveTextContent('gpt-4')
		})

		it('shows "Select model" for unknown model id', async () => {
			render(ModelSelector, {
				value: 'unknown-model',
				modelGroups: testModelGroups
			})

			const button = page.getByRole('combobox')
			await expect.element(button).toHaveTextContent('Select model')
		})

		it('applies custom class to trigger', async () => {
			render(ModelSelector, {
				class: 'custom-class',
				modelGroups: testModelGroups
			})

			const button = page.getByRole('combobox')
			const buttonElement = button.element()
			expect(buttonElement.className).toContain('custom-class')
		})
	})

	describe('dropdown behavior', () => {
		it('opens dropdown on click', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')
			await button.click()

			await expect.element(page.getByText('OpenAI')).toBeVisible()
		})

		it('shows available models in dropdown', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')
			await button.click()

			await expect.element(page.getByText('gpt-4')).toBeVisible()
			await expect.element(page.getByText('gpt-3.5-turbo')).toBeVisible()
			await expect.element(page.getByText('claude-3-sonnet')).toBeVisible()
		})

		it('shows provider group headings', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')
			await button.click()

			await expect.element(page.getByText('OpenAI')).toBeVisible()
			await expect.element(page.getByText('Anthropic')).toBeVisible()
		})

		it('shows default model option when showDefault is true', async () => {
			render(ModelSelector, {
				modelGroups: testModelGroups,
				showDefault: true
			})

			const button = page.getByRole('combobox')
			await button.click()

			const autoOption = page.getByRole('option', { name: 'auto' })
			await expect.element(autoOption).toBeVisible()
		})

		it('hides default model option when showDefault is false', async () => {
			render(ModelSelector, {
				modelGroups: testModelGroups,
				showDefault: false,
				value: 'gpt-4'
			})

			const button = page.getByRole('combobox')
			await button.click()

			await expect
				.element(page.getByRole('option', { name: 'gpt-4' }))
				.toBeVisible()

			const options = await page.getByRole('option').all()
			const optionTexts = options.map((el) => el.element().textContent)
			expect(optionTexts.some((text) => text?.includes('auto'))).toBe(false)
		})
	})

	describe('selection behavior', () => {
		it('selects model on click and updates display', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')
			await button.click()

			const gpt4Option = page.getByText('gpt-4')
			await gpt4Option.click()

			await expect.element(button).toHaveTextContent('gpt-4')
		})

		it('closes dropdown after selection', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')
			await button.click()

			await expect
				.element(page.getByRole('option', { name: 'gpt-3.5-turbo' }))
				.toBeVisible()

			const option = page.getByRole('option', { name: 'gpt-3.5-turbo' })
			await option.click()

			const buttonElement = button.element() as HTMLButtonElement
			expect(buttonElement.getAttribute('aria-expanded')).toBe('false')
		})

		it('selects different models in sequence', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')

			await button.click()
			await page.getByText('gpt-3.5-turbo').click()
			await expect.element(button).toHaveTextContent('gpt-3.5-turbo')

			await button.click()
			await page.getByText('claude-3-sonnet').click()
			await expect.element(button).toHaveTextContent('claude-3-sonnet')
		})
	})

	describe('accessibility', () => {
		it('has aria-haspopup listbox attribute', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')
			const buttonElement = button.element() as HTMLButtonElement
			expect(buttonElement.getAttribute('aria-haspopup')).toBe('listbox')
		})

		it('has aria-expanded attribute that changes on click', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')
			const buttonElement = button.element() as HTMLButtonElement
			expect(buttonElement.getAttribute('aria-expanded')).toBe('false')

			await button.click()
			expect(buttonElement.getAttribute('aria-expanded')).toBe('true')
		})
	})
})
