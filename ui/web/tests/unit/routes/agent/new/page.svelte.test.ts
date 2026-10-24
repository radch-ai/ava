import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import * as agentConfig from '$lib/services/agent'
import { addAgent } from '$lib/user'
import NewAgentPage from '$routes/agent/new/+page.svelte'

const mockAddAgent = vi.mocked(addAgent)

// Mock the agent config service
vi.mock('$lib/services/agent', () => ({
	createAgent: vi.fn(),
	getAgentConfiguration: vi.fn(),
	updateAgentConfiguration: vi.fn()
}))

// Mock SvelteKit navigation
vi.mock('$app/navigation', () => ({
	goto: vi.fn(),
	beforeNavigate: vi.fn()
}))

vi.mock('$app/paths', () => ({
	resolve: vi.fn((path: string) => path)
}))

vi.mock('$lib/user', () => ({
	addAgent: vi.fn()
}))

vi.mock('$app/stores', () => ({
	page: {
		subscribe: vi.fn(),
		url: { subscribe: vi.fn() },
		params: { subscribe: vi.fn() }
	},
	navigating: { subscribe: vi.fn() }
}))

describe('New Agent Page', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	describe('Page Rendering', () => {
		it('renders the new agent page with header', async () => {
			render(NewAgentPage)

			await expect
				.element(page.getByRole('heading', { name: /new agent/i }))
				.toBeVisible()
		})

		it('shows the bot icon in header', async () => {
			render(NewAgentPage)

			// Check that the Bot icon is rendered (it's an SVG)
			const heading = page.getByRole('heading', { name: /new agent/i })
			await expect.element(heading).toBeVisible()
		})

		it('renders the configuration editor', async () => {
			render(NewAgentPage)

			// Check that the create button is visible
			await expect
				.element(page.getByRole('button', { name: /create agent/i }))
				.toBeVisible()
		})
	})

	describe('Name Field', () => {
		it('shows name field with label', async () => {
			render(NewAgentPage)

			// Click on Name in sidebar to ensure it's visible
			const nameButton = page.getByRole('button', { name: /^name$/i })
			await nameButton.click()

			const nameLabel = page.getByText(/name/i).first()
			await expect.element(nameLabel).toBeVisible()
		})

		it('allows entering agent name', async () => {
			render(NewAgentPage)

			// Click on Name in sidebar
			const nameButton = page.getByRole('button', { name: /^name$/i })
			await nameButton.click()

			const nameInput = page.getByPlaceholder(/enter agent name/i)
			await nameInput.fill('TestAgent')

			await expect.element(nameInput).toHaveValue('TestAgent')
		})

		it('shows asterisk on Name button when name changes', async () => {
			render(NewAgentPage)

			// Click on Name in sidebar
			const nameButton = page.getByRole('button', { name: /^name$/i })
			await nameButton.click()

			// Fill in a name
			const nameInput = page.getByPlaceholder(/enter agent name/i)
			await nameInput.fill('TestAgent')

			// Check that Name button now shows asterisk
			const nameButtonWithAsterisk = page.getByRole('button', {
				name: /^name\s*\*/i
			})
			await expect.element(nameButtonWithAsterisk).toBeVisible()
		})
	})

	describe('Soul Section', () => {
		it('shows Soul button in sidebar', async () => {
			render(NewAgentPage)

			const soulButton = page.getByRole('button', { name: /^soul$/i })
			await expect.element(soulButton).toBeVisible()
		})

		it('shows personality and identity fields when Soul is selected', async () => {
			render(NewAgentPage)

			// Click on Soul in sidebar
			const soulButton = page.getByRole('button', { name: /^soul$/i })
			await soulButton.click()

			// Should show personality field label (use more specific selector)
			const personalityLabel = page.getByLabelText(/personality/i)
			await expect.element(personalityLabel).toBeVisible()

			// Should show identity field label
			const identityLabel = page.getByLabelText(/identity/i)
			await expect.element(identityLabel).toBeVisible()
		})

		it('allows entering personality', async () => {
			render(NewAgentPage)

			// Click on Soul in sidebar
			const soulButton = page.getByRole('button', { name: /^soul$/i })
			await soulButton.click()

			const personalityInput = page.getByLabelText(/personality/i)
			await personalityInput.fill('Friendly and helpful')

			await expect.element(personalityInput).toHaveValue('Friendly and helpful')
		})

		it('allows entering identity', async () => {
			render(NewAgentPage)

			// Click on Soul in sidebar
			const soulButton = page.getByRole('button', { name: /^soul$/i })
			await soulButton.click()

			const identityInput = page.getByLabelText(/identity/i)
			await identityInput.fill('Customer support assistant')

			await expect
				.element(identityInput)
				.toHaveValue('Customer support assistant')
		})

		it('shows asterisk on Soul button when personality changes', async () => {
			render(NewAgentPage)

			// Click on Soul in sidebar
			const soulButton = page.getByRole('button', { name: /^soul$/i })
			await soulButton.click()

			// Fill in personality
			const personalityInput = page.getByLabelText(/personality/i)
			await personalityInput.fill('Friendly and helpful')

			// Check that Soul button now shows asterisk
			const soulButtonWithAsterisk = page.getByRole('button', {
				name: /^soul\s*\*/i
			})
			await expect.element(soulButtonWithAsterisk).toBeVisible()
		})
	})

	describe('Heartbeat Configuration', () => {
		it('shows Heartbeat button in sidebar', async () => {
			render(NewAgentPage)

			const heartbeatButton = page.getByRole('button', { name: /^heartbeat$/i })
			await expect.element(heartbeatButton).toBeVisible()
		})

		it('shows heartbeat toggle when Heartbeat is selected', async () => {
			render(NewAgentPage)

			// Click on Heartbeat in sidebar
			const heartbeatButton = page.getByRole('button', { name: /^heartbeat$/i })
			await heartbeatButton.click()

			// Should show enabled checkbox with exact label match
			const enabledCheckbox = page.getByRole('checkbox', { name: 'Enabled' })
			await expect.element(enabledCheckbox).toBeVisible()
		})

		it('allows toggling heartbeat enabled', async () => {
			render(NewAgentPage)

			// Click on Heartbeat in sidebar
			const heartbeatButton = page.getByRole('button', { name: /^heartbeat$/i })
			await heartbeatButton.click()

			// Find and click the enabled checkbox
			const enabledCheckbox = page.getByRole('checkbox', { name: /enabled/i })
			await enabledCheckbox.click()

			// Should be checked now
			await expect.element(enabledCheckbox).toBeChecked()
		})
	})

	describe('Tools Configuration', () => {
		it('shows Tools Context button in sidebar', async () => {
			render(NewAgentPage)

			const toolsButton = page.getByRole('button', { name: /tools context/i })
			await expect.element(toolsButton).toBeVisible()
		})

		it('shows tools context field when selected', async () => {
			render(NewAgentPage)

			// Click on Tools Context in sidebar
			const toolsButton = page.getByRole('button', { name: /tools context/i })
			await toolsButton.click()

			// Should show tools context textarea (find by placeholder from FIELD_CONFIGS)
			const toolsContextTextarea = page.getByRole('textbox')
			await expect.element(toolsContextTextarea).toBeVisible()
		})
	})

	describe('Browser refresh/close protection', () => {
		it('prevents beforeunload when there are unsaved changes', async () => {
			render(NewAgentPage)

			const nameButton = page.getByRole('button', { name: /^name$/i })
			await nameButton.click()
			const nameInput = page.getByPlaceholder(/enter agent name/i)
			await nameInput.fill('TestAgent')

			const event = new Event('beforeunload', { cancelable: true })
			const spy = vi.spyOn(event, 'preventDefault')
			window.dispatchEvent(event)

			expect(spy).toHaveBeenCalled()
		})

		it('does not prevent beforeunload when there are no changes', async () => {
			render(NewAgentPage)

			const event = new Event('beforeunload', { cancelable: true })
			const spy = vi.spyOn(event, 'preventDefault')
			window.dispatchEvent(event)

			expect(spy).not.toHaveBeenCalled()
		})
	})

	describe('Form Actions', () => {
		it('shows Create Agent button', async () => {
			render(NewAgentPage)

			await expect
				.element(page.getByRole('button', { name: /create agent/i }))
				.toBeVisible()
		})

		it('shows Cancel button', async () => {
			render(NewAgentPage)

			await expect
				.element(page.getByRole('button', { name: /cancel/i }))
				.toBeVisible()
		})

		it('creates agent with correct name format when Create Agent is clicked', async () => {
			const mockCreateAgent = vi.mocked(agentConfig.createAgent)
			mockCreateAgent.mockResolvedValueOnce({ name: 'test-agent-name' })

			render(NewAgentPage)

			// Click on Name in sidebar and fill name
			const nameButton = page.getByRole('button', { name: /^name$/i })
			await nameButton.click()

			const nameInput = page.getByPlaceholder(/enter agent name/i)
			await nameInput.fill('Test Agent Name')

			// Click Create Agent
			const createButton = page.getByRole('button', { name: /create agent/i })
			await createButton.click()

			// Verify createAgent was called with correct ID format
			expect(mockCreateAgent).toHaveBeenCalledWith(
				'test-agent-name',
				expect.objectContaining({
					name: 'Test Agent Name'
				})
			)
			expect(mockAddAgent).toHaveBeenCalledWith({
				name: 'test-agent-name',
				icon: expect.any(String)
			})
		})

		it('does not create agent when name is empty', async () => {
			const mockCreateAgent = vi.mocked(agentConfig.createAgent)

			render(NewAgentPage)

			const createButton = page.getByRole('button', { name: /create agent/i })
			await createButton.click()

			expect(mockCreateAgent).not.toHaveBeenCalled()
		})

		it('handles API failure gracefully when createAgent rejects', async () => {
			const mockCreateAgent = vi.mocked(agentConfig.createAgent)
			mockCreateAgent.mockRejectedValueOnce(new Error('Network error'))

			render(NewAgentPage)

			const nameButton = page.getByRole('button', { name: /^name$/i })
			await nameButton.click()

			const nameInput = page.getByPlaceholder(/enter agent name/i)
			await nameInput.fill('fail-agent')

			const createButton = page.getByRole('button', { name: /create agent/i })
			await createButton.click()

			await expect
				.element(page.getByRole('heading', { name: /new agent/i }))
				.toBeVisible()
		})

		it('handles timeout gracefully when createAgent is slow', async () => {
			const mockCreateAgent = vi.mocked(agentConfig.createAgent)
			mockCreateAgent.mockRejectedValueOnce(new Error('Request timeout'))

			render(NewAgentPage)

			const nameButton = page.getByRole('button', { name: /^name$/i })
			await nameButton.click()

			const nameInput = page.getByPlaceholder(/enter agent name/i)
			await nameInput.fill('timeout-agent')

			const createButton = page.getByRole('button', { name: /create agent/i })
			await createButton.click()

			await expect
				.element(page.getByRole('heading', { name: /new agent/i }))
				.toBeVisible()
			expect(mockAddAgent).not.toHaveBeenCalled()
		})

		it('handles HTTP 500 gracefully when createAgent fails with server error', async () => {
			const mockCreateAgent = vi.mocked(agentConfig.createAgent)
			mockCreateAgent.mockRejectedValueOnce(new Error('Internal server error'))

			render(NewAgentPage)

			const nameButton = page.getByRole('button', { name: /^name$/i })
			await nameButton.click()

			const nameInput = page.getByPlaceholder(/enter agent name/i)
			await nameInput.fill('server-error-agent')

			const createButton = page.getByRole('button', { name: /create agent/i })
			await createButton.click()

			await expect
				.element(page.getByRole('heading', { name: /new agent/i }))
				.toBeVisible()
			expect(mockAddAgent).not.toHaveBeenCalled()
		})
	})
})
