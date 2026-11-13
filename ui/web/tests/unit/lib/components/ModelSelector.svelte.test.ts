import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import ModelSelector from '#components/ModelSelector.svelte'
import {
	type ModelEntry,
	type ModelGroup,
	type ModelSelection,
	modelsStore
} from '#lib/models'
import { getModels } from '#lib/services/providers'
import type { GetModelsResponse } from '#lib/types/provider'

vi.mock('#lib/services/providers', () => ({
	getModels: vi.fn(async () => ({ groups: [] }))
}))

const openai = (id: string): ModelEntry => ({ id, providerId: 'p-openai' })
const anthropic = (id: string): ModelEntry => ({
	id,
	providerId: 'p-anthropic'
})

describe('ModelSelector', () => {
	const testModelGroups: ModelGroup[] = [
		{
			providerName: 'OpenAI',
			models: [openai('gpt-4'), openai('gpt-3.5-turbo')]
		},
		{
			providerName: 'Anthropic',
			models: [anthropic('claude-3-sonnet')]
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

		it('shows Default when no value is provided', async () => {
			render(ModelSelector, { modelGroups: testModelGroups })

			const button = page.getByRole('combobox')
			await expect.element(button).toHaveTextContent('Default')
		})

		it('shows current selected model name when value is set', async () => {
			render(ModelSelector, {
				value: openai('gpt-4'),
				modelGroups: testModelGroups
			})

			const button = page.getByRole('combobox')
			await expect.element(button).toHaveTextContent('gpt-4')
		})

		it('shows "Select model" for a selection outside the catalog', async () => {
			render(ModelSelector, {
				value: { id: 'unknown-model', providerId: 'p-gone' },
				modelGroups: testModelGroups
			})

			const button = page.getByRole('combobox')
			await expect.element(button).toHaveTextContent('Select model')
		})

		it('renders same-id entries with plain, identical labels', async () => {
			// Two provider rows share the group name and a model id:
			// the entries look alike but stay distinct — each carries
			// its own providerId as its selection key.
			const merged: ModelGroup[] = [
				{
					providerName: 'OpenAI',
					models: [openai('gpt-4'), { id: 'gpt-4', providerId: 'p-proxy' }]
				}
			]
			render(ModelSelector, {
				value: { id: 'gpt-4', providerId: 'p-proxy' },
				modelGroups: merged
			})

			const button = page.getByRole('combobox')
			await expect.element(button).toHaveTextContent('gpt-4')
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

		it('shows the default option when showDefault is true', async () => {
			render(ModelSelector, {
				modelGroups: testModelGroups,
				showDefault: true
			})

			const button = page.getByRole('combobox')
			await button.click()

			const defaultOption = page.getByRole('option', { name: 'Default' })
			await expect.element(defaultOption).toBeVisible()
		})

		it('hides the default option when showDefault is false', async () => {
			render(ModelSelector, {
				modelGroups: testModelGroups,
				showDefault: false,
				value: openai('gpt-4')
			})

			const button = page.getByRole('combobox')
			await button.click()

			await expect
				.element(page.getByRole('option', { name: 'gpt-4' }))
				.toBeVisible()

			const options = await page.getByRole('option').all()
			const optionTexts = options.map((el) => el.element().textContent)
			expect(optionTexts.some((text) => text?.includes('Default'))).toBe(false)
		})
	})

	describe('selection behavior', () => {
		it('selects model on click, updating display and callback', async () => {
			const onChange = vi.fn<(selection: ModelSelection) => void>()
			render(ModelSelector, {
				modelGroups: testModelGroups,
				onChange
			})

			const button = page.getByRole('combobox')
			await button.click()

			const gpt4Option = page.getByText('gpt-4')
			await gpt4Option.click()

			await expect.element(button).toHaveTextContent('gpt-4')
			expect(onChange).toHaveBeenCalledWith(openai('gpt-4'))
		})

		it('selecting Default reports null', async () => {
			const onChange = vi.fn<(selection: ModelSelection) => void>()
			render(ModelSelector, {
				modelGroups: testModelGroups,
				value: openai('gpt-4'),
				onChange
			})

			const button = page.getByRole('combobox')
			await button.click()
			await page.getByRole('option', { name: 'Default' }).click()

			await expect.element(button).toHaveTextContent('Default')
			expect(onChange).toHaveBeenCalledWith(null)
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

describe('ModelSelector catalog refresh', () => {
	/** One provider group with the given model ids, as GET /api/models. */
	function catalog(models: string[]): GetModelsResponse {
		return {
			groups: [
				{
					providerName: 'OpenAI',
					models: models.map((id) => ({ id, providerId: 'p-openai' }))
				}
			]
		}
	}

	beforeEach(() => {
		// Fresh store: every selector in the app shares this instance.
		modelsStore.groups = []
		modelsStore.loaded = false
		modelsStore.error = null
	})

	afterEach(async () => {
		cleanup()
		vi.clearAllMocks()
	})

	it('shows the refresh control when reading the shared store', async () => {
		vi.mocked(getModels).mockResolvedValueOnce(catalog(['gpt-4']))
		render(ModelSelector, {})

		await page.getByRole('combobox').click()

		await expect
			.element(page.getByRole('button', { name: 'Refresh model list' }))
			.toBeVisible()
		await expect
			.element(page.getByRole('option', { name: 'gpt-4' }))
			.toBeVisible()
	})

	it('hides the refresh control when the caller supplies groups', async () => {
		render(ModelSelector, {
			modelGroups: [{ providerName: 'OpenAI', models: [openai('gpt-4')] }]
		})

		await page.getByRole('combobox').click()

		await expect
			.element(page.getByRole('option', { name: 'gpt-4' }))
			.toBeVisible()
		const refreshButtons = await page
			.getByRole('button', { name: 'Refresh model list' })
			.elements()
		expect(refreshButtons).toHaveLength(0)
	})

	it('refresh refetches the catalog and keeps the dropdown open', async () => {
		vi.mocked(getModels).mockResolvedValueOnce(catalog(['gpt-4']))
		render(ModelSelector, {})
		const trigger = page.getByRole('combobox')
		await trigger.click()
		await expect
			.element(page.getByRole('option', { name: 'gpt-4' }))
			.toBeVisible()

		vi.mocked(getModels).mockResolvedValueOnce(catalog(['gpt-4', 'claude-3']))
		await page.getByRole('button', { name: 'Refresh model list' }).click()

		await expect
			.element(page.getByRole('option', { name: 'claude-3' }))
			.toBeVisible()
		expect(getModels).toHaveBeenCalledTimes(2)
		const triggerElement = trigger.element() as HTMLButtonElement
		expect(triggerElement.getAttribute('aria-expanded')).toBe('true')
	})
})
