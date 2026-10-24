import { describe, expect, it, afterEach } from 'vitest';
import { render, cleanup } from 'vitest-browser-svelte';
import { page } from 'vitest/browser';
import ModelSelector from '$components/ModelSelector.svelte';
import type { ModelGroup } from '$lib/models';

describe('ModelSelector', () => {
	const testModelGroups: ModelGroup[] = [
		{
			provider: 'OpenAI',
			models: [
				{ id: 'gpt-4', name: 'GPT-4' },
				{ id: 'gpt-3.5-turbo', name: 'GPT-3.5 Turbo' }
			]
		},
		{
			provider: 'Anthropic',
			models: [{ id: 'claude-3-sonnet', name: 'Claude 3 Sonnet' }]
		}
	];

	afterEach(async () => {
		cleanup();
	});

	describe('rendering', () => {
		it('renders the selector', async () => {
			render(ModelSelector, { modelGroups: testModelGroups });

			const button = page.getByRole('button');
			await expect.element(button).toBeVisible();
		});

		it('shows default model (auto) when no value is provided', async () => {
			render(ModelSelector, { modelGroups: testModelGroups });

			const button = page.getByRole('button');
			await expect.element(button).toHaveTextContent('auto');
		});

		it('shows current selected model name when value is set', async () => {
			render(ModelSelector, {
				value: 'gpt-4',
				modelGroups: testModelGroups
			});

			const button = page.getByRole('button');
			await expect.element(button).toHaveTextContent('GPT-4');
		});

		it('shows "Select model" for unknown model id', async () => {
			render(ModelSelector, {
				value: 'unknown-model',
				modelGroups: testModelGroups
			});

			const button = page.getByRole('button');
			await expect.element(button).toHaveTextContent('Select model');
		});

		it('applies custom class to trigger', async () => {
			render(ModelSelector, {
				class: 'custom-class',
				modelGroups: testModelGroups
			});

			const button = page.getByRole('button');
			const buttonElement = button.element();
			expect(buttonElement.className).toContain('custom-class');
		});
	});

	describe('dropdown behavior', () => {
		it('opens dropdown on click', async () => {
			render(ModelSelector, { modelGroups: testModelGroups });

			const button = page.getByRole('button');
			await button.click();

			await expect.element(page.getByText('OpenAI')).toBeVisible();
		});

		it('shows available models in dropdown', async () => {
			render(ModelSelector, { modelGroups: testModelGroups });

			const button = page.getByRole('button');
			await button.click();

			await expect.element(page.getByText('GPT-4')).toBeVisible();
			await expect.element(page.getByText('GPT-3.5 Turbo')).toBeVisible();
			await expect.element(page.getByText('Claude 3 Sonnet')).toBeVisible();
		});

		it('shows provider group headings', async () => {
			render(ModelSelector, { modelGroups: testModelGroups });

			const button = page.getByRole('button');
			await button.click();

			await expect.element(page.getByText('OpenAI')).toBeVisible();
			await expect.element(page.getByText('Anthropic')).toBeVisible();
		});

		it('shows default model option when showDefault is true', async () => {
			render(ModelSelector, {
				modelGroups: testModelGroups,
				showDefault: true
			});

			const button = page.getByRole('button');
			await button.click();

			const autoOption = page.getByRole('option', { name: 'auto' });
			await expect.element(autoOption).toBeVisible();
		});

		it('hides default model option when showDefault is false', async () => {
			render(ModelSelector, {
				modelGroups: testModelGroups,
				showDefault: false,
				value: 'gpt-4'
			});

			const button = page.getByRole('button');
			await button.click();

			await expect.element(page.getByRole('option', { name: 'GPT-4' })).toBeVisible();

			const options = await page.getByRole('option').all();
			const optionTexts = options.map((el) => el.element().textContent);
			expect(optionTexts.some((text) => text?.includes('auto'))).toBe(false);
		});
	});

	describe('selection behavior', () => {
		it('selects model on click and updates display', async () => {
			render(ModelSelector, { modelGroups: testModelGroups });

			const button = page.getByRole('button');
			await button.click();

			const gpt4Option = page.getByText('GPT-4');
			await gpt4Option.click();

			await expect.element(button).toHaveTextContent('GPT-4');
		});

		it('closes dropdown after selection', async () => {
			render(ModelSelector, { modelGroups: testModelGroups });

			const button = page.getByRole('button');
			await button.click();

			await expect.element(page.getByRole('option', { name: 'GPT-3.5 Turbo' })).toBeVisible();

			const option = page.getByRole('option', { name: 'GPT-3.5 Turbo' });
			await option.click();

			const buttonElement = button.element() as HTMLButtonElement;
			expect(buttonElement.getAttribute('aria-expanded')).toBe('false');
		});

		it('selects different models in sequence', async () => {
			render(ModelSelector, { modelGroups: testModelGroups });

			const button = page.getByRole('button');

			await button.click();
			await page.getByText('GPT-3.5 Turbo').click();
			await expect.element(button).toHaveTextContent('GPT-3.5 Turbo');

			await button.click();
			await page.getByText('Claude 3 Sonnet').click();
			await expect.element(button).toHaveTextContent('Claude 3 Sonnet');
		});
	});

	describe('accessibility', () => {
		it('has aria-haspopup listbox attribute', async () => {
			render(ModelSelector, { modelGroups: testModelGroups });

			const button = page.getByRole('button');
			const buttonElement = button.element() as HTMLButtonElement;
			expect(buttonElement.getAttribute('aria-haspopup')).toBe('listbox');
		});

		it('has aria-expanded attribute that changes on click', async () => {
			render(ModelSelector, { modelGroups: testModelGroups });

			const button = page.getByRole('button');
			const buttonElement = button.element() as HTMLButtonElement;
			expect(buttonElement.getAttribute('aria-expanded')).toBe('false');

			await button.click();
			expect(buttonElement.getAttribute('aria-expanded')).toBe('true');
		});
	});
});
