import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import type { Agent, SuperagentConfiguration } from '#lib/types/agent'
import AgentHeaderPairTestWrapper from './AgentHeaderPairTestWrapper.svelte'
import AgentHeaderTestWrapper from './AgentHeaderTestWrapper.svelte'

const superagent: Agent = {
	id: '_superagent',
	name: 'Ava',
	icon: 'message-square'
}

const workerAgent: Agent = {
	id: 'support',
	name: 'Support',
	icon: 'bot'
}

function createDefaultTestConfig(): SuperagentConfiguration {
	return {
		name: 'Test',
		icon: 'bot',
		soul: { personality: '', identity: '' },
		userContext: '',
		operatingInstructions: '',
		toolsContext: '',
		toolsEnabled: [],
		cronJobs: [],
		heartbeat: {
			enabled: false,
			duration: '1hr',
			content: '',
			model: 'default'
		}
	}
}

vi.mock('#lib/services/agent', () => ({
	getAgentConfiguration: vi.fn(),
	updateAgentConfiguration: vi.fn()
}))

vi.mock('$app/navigation', () => ({
	invalidate: vi.fn()
}))

import { getAgentConfiguration } from '#lib/services/agent'

const mockGetAgentConfiguration = vi.mocked(getAgentConfiguration)

describe('AgentHeader', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	describe('rendering', () => {
		it('displays agent name', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: superagent })

			await expect.element(page.getByText('Ava')).toBeVisible()
		})

		it('displays worker agent name', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: workerAgent })

			await expect.element(page.getByText('Support')).toBeVisible()
		})

		it('renders exactly one settings button with accessible name', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: workerAgent })

			await expect
				.element(page.getByRole('button', { name: 'Open agent configuration' }))
				.toBeVisible()
		})

		it('renders settings button with accessible label', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: workerAgent })

			await expect
				.element(page.getByLabelText('Open agent configuration'))
				.toBeVisible()
		})

		it('shows an agent config tooltip for the settings button', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: workerAgent })

			const settingsButton = page.getByLabelText('Open agent configuration')
			await settingsButton.hover()

			await expect.element(page.getByText('Agent Config')).toBeVisible()
		})

		it('uses a unique tooltip id for each settings button', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderPairTestWrapper, {
				agents: [workerAgent, { ...workerAgent, name: 'support-two' }]
			})

			const settingsButtons = page.getByLabelText('Open agent configuration')
			const firstButton = settingsButtons.nth(0)
			const secondButton = settingsButtons.nth(1)
			const tooltipText = page.getByText('Agent Config')

			await firstButton.hover({ force: true })
			await expect.element(tooltipText).toBeVisible()
			const firstTooltipId = (await tooltipText.element())?.parentElement?.id

			await secondButton.hover({ force: true })
			await expect.element(tooltipText).toBeVisible()
			const secondTooltipId = (await tooltipText.element())?.parentElement?.id

			expect(firstTooltipId).toBeTruthy()
			expect(secondTooltipId).toBeTruthy()
			expect(firstTooltipId).not.toBe(secondTooltipId)
		})
	})

	describe('tab switcher', () => {
		it('renders tabs for superagent agent when activeTab and onTabChange provided', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				onTabChange: vi.fn()
			})

			await expect
				.element(page.getByRole('tab', { name: 'Conversation' }))
				.toBeVisible()
			await expect
				.element(page.getByRole('tab', { name: 'Activity' }))
				.toBeVisible()
		})

		it('does not render tabs for worker agents', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: workerAgent })

			expect(page.getByRole('tab', { name: 'Conversation' }).query()).toBeNull()
			expect(page.getByRole('tab', { name: 'Activity' }).query()).toBeNull()
		})

		it('does not render tabs when onTabChange is not provided', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, {
				agent: superagent,
				activeTab: 'conversation'
			})

			expect(page.getByRole('tab', { name: 'Conversation' }).query()).toBeNull()
		})
	})

	describe('tab interaction', () => {
		it('calls onTabChange when switching tabs', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			const onTabChange = vi.fn()
			render(AgentHeaderTestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				onTabChange
			})

			await page.getByRole('tab', { name: 'Activity' }).click()

			expect(onTabChange).toHaveBeenCalledWith('activity')
		})

		it('does not call onTabChange when clicking active tab', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			const onTabChange = vi.fn()
			render(AgentHeaderTestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				onTabChange
			})

			await page
				.getByRole('tab', { name: 'Conversation' })
				.click({ force: true })

			expect(onTabChange).not.toHaveBeenCalled()
		})
	})

	describe('tab accessibility', () => {
		it('sets aria-selected correctly', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				onTabChange: vi.fn()
			})

			const conversationTab = page.getByRole('tab', { name: 'Conversation' })
			const activityTab = page.getByRole('tab', { name: 'Activity' })

			await expect
				.element(conversationTab)
				.toHaveAttribute('aria-selected', 'true')
			await expect
				.element(activityTab)
				.toHaveAttribute('aria-selected', 'false')
		})

		it('sets aria-controls correctly', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				onTabChange: vi.fn()
			})

			await expect
				.element(page.getByRole('tab', { name: 'Conversation' }))
				.toHaveAttribute('aria-controls', 'conversation-panel')
			await expect
				.element(page.getByRole('tab', { name: 'Activity' }))
				.toHaveAttribute('aria-controls', 'activity-panel')
		})

		it('has tablist role', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, {
				agent: superagent,
				activeTab: 'conversation',
				onTabChange: vi.fn()
			})

			await expect.element(page.getByRole('tablist')).toBeVisible()
		})
	})

	describe('settings button - successful load', () => {
		it('loads config and opens sheet on click', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: superagent })

			await page.getByLabelText('Open agent configuration').click()

			await vi.waitFor(() => {
				expect(mockGetAgentConfiguration).toHaveBeenCalledWith('_superagent')
			})

			await expect
				.element(page.getByRole('button', { name: /save/i }))
				.toBeVisible()
		})

		it('disables button while loading', async () => {
			let resolveConfig: ((value: SuperagentConfiguration) => void) | undefined
			mockGetAgentConfiguration.mockImplementation(
				() => new Promise((resolve) => (resolveConfig = resolve))
			)
			render(AgentHeaderTestWrapper, { agent: superagent })

			const settingsBtn = page.getByLabelText('Open agent configuration')
			await settingsBtn.click()

			await expect.element(settingsBtn).toBeDisabled()

			resolveConfig?.(createDefaultTestConfig())
			await vi.waitFor(() => {
				expect(
					page.getByRole('button', { name: /save/i }).query()
				).not.toBeNull()
			})
		})

		it('caches config after first load', async () => {
			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: superagent })

			await page.getByLabelText('Open agent configuration').click()
			await vi.waitFor(() => {
				expect(mockGetAgentConfiguration).toHaveBeenCalledTimes(1)
			})

			await page.getByRole('button', { name: /cancel/i }).click()

			await page.getByLabelText('Open agent configuration').click()
			await vi.waitFor(() => {
				expect(mockGetAgentConfiguration).toHaveBeenCalledTimes(1)
			})
		})
	})

	describe('settings button - API error', () => {
		it('does not open sheet when API fails', async () => {
			mockGetAgentConfiguration.mockRejectedValue(new Error('Network error'))
			render(AgentHeaderTestWrapper, { agent: superagent })

			await page.getByLabelText('Open agent configuration').click()

			await vi.waitFor(() => {
				expect(mockGetAgentConfiguration).toHaveBeenCalledWith('_superagent')
			})

			expect(page.getByRole('button', { name: /save/i }).query()).toBeNull()
		})

		it('re-enables button after error', async () => {
			mockGetAgentConfiguration.mockRejectedValue(new Error('Network error'))
			render(AgentHeaderTestWrapper, { agent: superagent })

			const settingsBtn = page.getByLabelText('Open agent configuration')
			await settingsBtn.click()

			await vi.waitFor(() => {
				expect(mockGetAgentConfiguration).toHaveBeenCalled()
			})

			await expect.element(settingsBtn).not.toBeDisabled()
		})

		it('allows retry after error', async () => {
			mockGetAgentConfiguration.mockRejectedValueOnce(
				new Error('Network error')
			)
			render(AgentHeaderTestWrapper, { agent: superagent })

			const settingsBtn = page.getByLabelText('Open agent configuration')
			await settingsBtn.click()

			await vi.waitFor(() => {
				expect(page.getByRole('button', { name: /save/i }).query()).toBeNull()
			})

			mockGetAgentConfiguration.mockResolvedValue(createDefaultTestConfig())
			await settingsBtn.click()

			await expect
				.element(page.getByRole('button', { name: /save/i }))
				.toBeVisible()
		})
	})
})
