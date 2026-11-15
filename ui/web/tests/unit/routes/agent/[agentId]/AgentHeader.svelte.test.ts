import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import type { AgentConfigWithIdentity, AgentIdentity } from '#lib/types/agent'
import AgentHeaderPairTestWrapper from './AgentHeaderPairTestWrapper.svelte'
import AgentHeaderTestWrapper from './AgentHeaderTestWrapper.svelte'

const superagent: AgentIdentity = {
	id: '_superagent',
	name: 'Ava',
	icon: 'message-square'
}

const agent: AgentIdentity = {
	id: 'support',
	name: 'Support',
	icon: 'bot'
}

function createDefaultTestConfig(): AgentConfigWithIdentity {
	return {
		id: '_superagent',
		name: 'Test',
		icon: 'bot',
		soul: { personality: '', identity: '' },
		userContext: '',
		operatingInstructions: '',
		toolsContext: '',
		toolsEnabled: [],
		heartbeat: {
			enabled: false,
			duration: '1hr',
			content: '',
			model: undefined
		}
	}
}

vi.mock('#lib/services/agent', () => ({
	getAgentConfig: vi.fn(),
	updateAgentConfig: vi.fn(),
	updateHeartbeat: vi.fn(),
	updateCronJobs: vi.fn()
}))

vi.mock('$app/navigation', () => ({
	goto: vi.fn(),
	invalidate: vi.fn(),
	beforeNavigate: vi.fn()
}))

import { getAgentConfig } from '#lib/services/agent'

const mockGetAgentConfig = vi.mocked(getAgentConfig)

describe('AgentHeader', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	describe('rendering', () => {
		it('displays agent name', async () => {
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: superagent })

			await expect.element(page.getByText('Ava')).toBeVisible()
		})

		it('displays non-superagent agent name', async () => {
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent })

			await expect.element(page.getByText('Support')).toBeVisible()
		})

		it('renders exactly one settings button with accessible name', async () => {
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent })

			await expect
				.element(page.getByRole('button', { name: 'Open agent configuration' }))
				.toBeVisible()
		})

		it('renders settings button with accessible label', async () => {
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent })

			await expect
				.element(page.getByLabelText('Open agent configuration'))
				.toBeVisible()
		})

		it('shows an agent config tooltip for the settings button', async () => {
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent })

			const settingsButton = page.getByLabelText('Open agent configuration')
			await settingsButton.hover()

			await expect.element(page.getByText('Agent Config')).toBeVisible()
		})

		it('uses a unique tooltip id for each settings button', async () => {
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderPairTestWrapper, {
				agents: [agent, { ...agent, name: 'support-two' }]
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
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
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

		it('does not render tabs for non-superagent agents', async () => {
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent })

			expect(page.getByRole('tab', { name: 'Conversation' }).query()).toBeNull()
			expect(page.getByRole('tab', { name: 'Activity' }).query()).toBeNull()
		})

		it('does not render tabs when onTabChange is not provided', async () => {
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, {
				agent: superagent,
				activeTab: 'conversation'
			})

			expect(page.getByRole('tab', { name: 'Conversation' }).query()).toBeNull()
		})
	})

	describe('tab interaction', () => {
		it('calls onTabChange when switching tabs', async () => {
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
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
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
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
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
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
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
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
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
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
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: superagent })

			await page.getByLabelText('Open agent configuration').click()

			await vi.waitFor(() => {
				expect(mockGetAgentConfig).toHaveBeenCalledWith('_superagent')
			})

			await expect
				.element(page.getByRole('button', { name: /save/i }))
				.toBeVisible()
		})

		it('disables button while loading', async () => {
			let resolveConfig: ((value: AgentConfigWithIdentity) => void) | undefined
			mockGetAgentConfig.mockImplementation(
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
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			render(AgentHeaderTestWrapper, { agent: superagent })

			await page.getByLabelText('Open agent configuration').click()
			await vi.waitFor(() => {
				expect(mockGetAgentConfig).toHaveBeenCalledTimes(1)
			})

			await page.getByRole('button', { name: /cancel/i }).click()

			await page.getByLabelText('Open agent configuration').click()
			await vi.waitFor(() => {
				expect(mockGetAgentConfig).toHaveBeenCalledTimes(1)
			})
		})

		it('refetches the config when the agent changes', async () => {
			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			const { rerender } = render(AgentHeaderTestWrapper, {
				agent: superagent
			})

			await page.getByLabelText('Open agent configuration').click()
			await vi.waitFor(() => {
				expect(mockGetAgentConfig).toHaveBeenCalledWith('_superagent')
			})

			await rerender({ agent })
			await page.getByLabelText('Open agent configuration').click()
			await vi.waitFor(() => {
				expect(mockGetAgentConfig).toHaveBeenLastCalledWith('support')
			})
		})

		it('drops a fetch that resolves after the agent changed', async () => {
			let resolveConfig!: (config: AgentConfigWithIdentity) => void
			mockGetAgentConfig.mockImplementation(
				() => new Promise((resolve) => (resolveConfig = resolve))
			)
			const { rerender } = render(AgentHeaderTestWrapper, {
				agent: superagent
			})

			await page.getByLabelText('Open agent configuration').click()

			await rerender({ agent })
			resolveConfig(createDefaultTestConfig())

			await vi.waitFor(() => {
				expect(page.getByRole('button', { name: /save/i }).query()).toBeNull()
			})
		})
	})

	describe('settings button - API error', () => {
		it('does not open sheet when API fails', async () => {
			mockGetAgentConfig.mockRejectedValue(new Error('Network error'))
			render(AgentHeaderTestWrapper, { agent: superagent })

			await page.getByLabelText('Open agent configuration').click()

			await vi.waitFor(() => {
				expect(mockGetAgentConfig).toHaveBeenCalledWith('_superagent')
			})

			expect(page.getByRole('button', { name: /save/i }).query()).toBeNull()
		})

		it('re-enables button after error', async () => {
			mockGetAgentConfig.mockRejectedValue(new Error('Network error'))
			render(AgentHeaderTestWrapper, { agent: superagent })

			const settingsBtn = page.getByLabelText('Open agent configuration')
			await settingsBtn.click()

			await vi.waitFor(() => {
				expect(mockGetAgentConfig).toHaveBeenCalled()
			})

			await expect.element(settingsBtn).not.toBeDisabled()
		})

		it('allows retry after error', async () => {
			mockGetAgentConfig.mockRejectedValueOnce(new Error('Network error'))
			render(AgentHeaderTestWrapper, { agent: superagent })

			const settingsBtn = page.getByLabelText('Open agent configuration')
			await settingsBtn.click()

			await vi.waitFor(() => {
				expect(page.getByRole('button', { name: /save/i }).query()).toBeNull()
			})

			mockGetAgentConfig.mockResolvedValue(createDefaultTestConfig())
			await settingsBtn.click()

			await expect
				.element(page.getByRole('button', { name: /save/i }))
				.toBeVisible()
		})
	})
})
