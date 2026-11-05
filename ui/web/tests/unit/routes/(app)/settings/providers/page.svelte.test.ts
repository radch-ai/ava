import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import * as providersService from '#lib/services/providers'
import type { Provider } from '#lib/types/provider'
import ProvidersPage from '#routes/settings/providers/+page.svelte'

// Mock the provider catalog service — the page never hits the network.
vi.mock('#lib/services/providers', () => ({
	getProviders: vi.fn(),
	createProvider: vi.fn(),
	updateProvider: vi.fn(),
	deleteProvider: vi.fn()
}))

// Mock SvelteKit navigation: the page invalidates after every mutation.
vi.mock('$app/navigation', () => ({
	invalidateAll: vi.fn()
}))

vi.mock('$app/paths', () => ({
	resolve: vi.fn((path: string, params?: Record<string, string>) => {
		const result = path.replace(/^\/?\([^)]*\)\//, '/')
		return params?.agentId
			? result.replace('[agentId]', params.agentId)
			: result
	})
}))

const openai: Provider = {
	id: 'provider-1',
	name: 'OpenAI',
	baseUrl: 'https://api.openai.com/v1',
	hasKey: true,
	models: ['openai/gpt-4', 'openai/gpt-4o']
}

const mistral: Provider = {
	id: 'provider-2',
	name: 'Mistral',
	baseUrl: 'https://api.mistral.ai/v1',
	hasKey: false,
	models: []
}

function renderPage(providers: Provider[]): void {
	render(ProvidersPage, {
		data: { agents: [], agentsError: null, providers, providersError: null }
	})
}

describe('Providers & Models page', () => {
	beforeEach(() => {
		vi.clearAllMocks()
		vi.mocked(providersService.getProviders).mockResolvedValue({
			defaultModel: 'auto',
			providers: []
		})
	})

	afterEach(() => {
		cleanup()
	})

	it('lists providers with base URL, key state and model ids', async () => {
		renderPage([openai, mistral])

		await expect
			.element(page.getByRole('heading', { name: 'OpenAI' }))
			.toBeVisible()
		await expect
			.element(page.getByText('Base URL: https://api.openai.com/v1'))
			.toBeVisible()
		// The stored key never leaves the API; the row says whether one is set.
		await expect
			.element(
				page.getByText('Base URL: https://api.mistral.ai/v1 · no API key set')
			)
			.toBeVisible()
		await expect
			.element(page.getByText('openai/gpt-4o', { exact: true }))
			.toBeVisible()
		await expect
			.element(page.getByText('openai/gpt-4', { exact: true }))
			.toBeVisible()
	})

	it('opens the blank form on Add New API / Models and returns on Cancel', async () => {
		renderPage([])

		await page.getByRole('button', { name: 'Add New API / Models' }).click()
		// A new provider has no models until saved — the form says so.
		await expect
			.element(page.getByText('Save the provider first, then add its models.'))
			.toBeVisible()

		await page.getByRole('button', { name: 'Cancel' }).click()
		await expect
			.element(page.getByPlaceholder('OpenRouter'))
			.not.toBeInTheDocument()
	})

	it('saves an edited provider with plain model ids and no key when blank', async () => {
		vi.mocked(providersService.updateProvider).mockResolvedValue(openai)
		renderPage([openai])

		await page.getByRole('button', { name: 'Edit' }).click()
		await expect
			.element(page.getByLabelText('Base URL'))
			.toHaveValue('https://api.openai.com/v1')
		// One box per model, prefilled with its id.
		await expect
			.element(page.getByLabelText('Model').first())
			.toHaveValue('openai/gpt-4')
		await page.getByRole('button', { name: 'Save' }).click()

		await vi.waitFor(() => {
			expect(providersService.updateProvider).toHaveBeenCalledWith(
				'provider-1',
				{
					name: 'OpenAI',
					baseUrl: 'https://api.openai.com/v1',
					apiKey: undefined,
					models: ['openai/gpt-4', 'openai/gpt-4o']
				}
			)
		})
	})

	it('surfaces the service error when saving fails', async () => {
		vi.mocked(providersService.updateProvider).mockRejectedValue(
			new Error('Model "openai/gpt-4" already belongs to another provider')
		)
		renderPage([openai])

		await page.getByRole('button', { name: 'Edit' }).click()
		await page.getByRole('button', { name: 'Save' }).click()

		await expect
			.element(
				page.getByText(
					'Model "openai/gpt-4" already belongs to another provider'
				)
			)
			.toBeVisible()
	})
})
