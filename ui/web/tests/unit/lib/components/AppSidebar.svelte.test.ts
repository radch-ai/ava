import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import { authStore } from '#lib/auth'
import type { Agent } from '#lib/types/agent'
import AppSidebarTestWrapper from './AppSidebarTestWrapper.svelte'

const { agents, mockSetAgents, mockGetAgents } = vi.hoisted(() => {
	const stored: Array<{ name: string; label: string; icon: string }> = []
	return {
		agents: stored,
		mockSetAgents: vi.fn(
			(agents: Array<{ name: string; label: string; icon: string }>) => {
				stored.length = 0
				stored.push(...agents)
			}
		),
		mockGetAgents: vi.fn(() => stored)
	}
})

const { pageStore, setPathname } = vi.hoisted(() => {
	const pageState = { url: { pathname: '/' } }
	const subscribers = new Set<(value: typeof pageState) => void>()

	const store = {
		subscribe(run: (value: typeof pageState) => void) {
			run(pageState)
			subscribers.add(run)
			return () => subscribers.delete(run)
		}
	}

	const updatePathname = (pathname: string) => {
		pageState.url.pathname = pathname
		for (const run of subscribers) run(pageState)
	}

	return {
		pageStore: store,
		setPathname: updatePathname
	}
})

vi.mock('#lib/user', () => ({
	agents,
	getAgents: mockGetAgents,
	setAgents: mockSetAgents
}))

// Mock stores and modules
vi.mock('$app/stores', () => ({
	page: pageStore
}))

vi.mock('$app/paths', () => ({
	resolve: (path: string) => path
}))

vi.mock('#lib/config', () => ({
	APP_NAME: 'TestApp'
}))

vi.mock('#lib/auth', () => ({
	authStore: { logout: vi.fn() }
}))

vi.mock('#lib/types/agent', () => ({
	SUPERAGENT_AGENT: {
		name: 'ava',
		label: 'Chat with Ava',
		icon: 'message-square'
	}
}))

// Mock localStorage
const localStorageMock = {
	getItem: vi.fn(),
	setItem: vi.fn(),
	removeItem: vi.fn(),
	clear: vi.fn()
}
vi.stubGlobal('localStorage', localStorageMock)

function createMockAgents(overrides: Partial<Agent>[] = []): Agent[] {
	return [
		{ name: 'support', label: 'Support Bot', icon: 'bot', ...overrides[0] },
		{
			name: 'analyst',
			label: 'Data Analyst',
			icon: 'chart-column',
			...overrides[1]
		}
	]
}

function collapseButton() {
	return page.getByRole('button', { name: /collapse sidebar/i }).first()
}

function expandButton() {
	return page.getByRole('button', { name: /expand sidebar/i }).first()
}

describe('AppSidebar', () => {
	beforeEach(() => {
		vi.clearAllMocks()
		localStorageMock.getItem.mockReturnValue(null)
	})

	afterEach(async () => {
		cleanup()
	})

	describe('rendering', () => {
		it('renders app name when sidebar is open', async () => {
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			await expect
				.element(page.getByRole('heading', { name: 'TestApp' }))
				.toBeVisible()
		})

		it('renders superagent agent link', async () => {
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			await expect.element(page.getByText('Chat with Ava')).toBeVisible()
		})

		it('renders worker agents section', async () => {
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			await expect.element(page.getByText('Worker Agents')).toBeVisible()
			await expect.element(page.getByText('Support Bot')).toBeVisible()
			await expect.element(page.getByText('Data Analyst')).toBeVisible()
		})

		it('renders New Agent button', async () => {
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			await expect.element(page.getByText('New Agent')).toBeVisible()
		})
	})

	describe('footer logout', () => {
		it('renders a logout button with an accessible name', async () => {
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			await expect
				.element(page.getByRole('button', { name: 'Sign Out' }))
				.toBeVisible()
		})

		it('logs out when the logout button is clicked', async () => {
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			await page.getByRole('button', { name: 'Sign Out' }).click()

			expect(vi.mocked(authStore.logout)).toHaveBeenCalledTimes(1)
		})
	})

	describe('navigation', () => {
		it('highlights superagent link when on home page', async () => {
			setPathname('/')
			mockSetAgents(createMockAgents())
			const { container } = render(AppSidebarTestWrapper)

			// superagent link should be active
			const superagentLink = container.querySelector('a[href="/"]')
			expect(superagentLink).toBeInTheDocument()
			expect(
				superagentLink?.closest('[data-active="true"]')
			).toBeInTheDocument()
		})

		it('highlights agent link when on agent page', async () => {
			setPathname('/agent/support')
			mockSetAgents(createMockAgents())
			const { container } = render(AppSidebarTestWrapper)

			// Support link should be active
			const supportLink = container.querySelector('a[href="/agent/support"]')
			expect(supportLink).toBeInTheDocument()
			expect(supportLink?.closest('[data-active="true"]')).toBeInTheDocument()
		})

		it('highlights New Agent button when on new agent page', async () => {
			setPathname('/agent/new')
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			const newAgentButton = page.getByRole('link', { name: /new agent/i })
			// Should have full opacity when active
			await expect.element(newAgentButton).not.toHaveClass(/hover:opacity-100/)
		})
	})

	describe('sidebar collapse/expand', () => {
		it('toggles sidebar when collapse button is clicked', async () => {
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			// App name should be visible initially
			await expect.element(page.getByText('TestApp')).toBeVisible()

			// Click collapse button
			await collapseButton().click()

			// App name should be hidden when collapsed
			await expect.element(page.getByText('TestApp')).not.toBeInTheDocument()
		})

		it('saves collapsed state to localStorage', async () => {
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			await collapseButton().click()

			expect(localStorageMock.setItem).toHaveBeenCalledWith(
				'testapp-sidebar-state',
				'collapsed'
			)
		})

		it('removes collapsed state from localStorage when expanded', async () => {
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			// First collapse
			await collapseButton().click()

			// Then expand
			await expandButton().click()

			expect(localStorageMock.removeItem).toHaveBeenCalledWith(
				'testapp-sidebar-state'
			)
		})
	})

	describe('empty state', () => {
		it('renders correctly with no worker agents', async () => {
			mockSetAgents([])
			render(AppSidebarTestWrapper)

			// Should still show superagent and New Agent button
			await expect.element(page.getByText('Chat with Ava')).toBeVisible()
			await expect.element(page.getByText('New Agent')).toBeVisible()

			// Worker Agents section should be empty
			await expect
				.element(page.getByText('Support Bot'))
				.not.toBeInTheDocument()
		})
	})

	describe('agent links', () => {
		it('generates correct links for agents', async () => {
			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			const supportLink = page.getByRole('link', { name: 'Support Bot' })
			await expect
				.element(supportLink)
				.toHaveAttribute('href', '/agent/support')

			const analystLink = page.getByRole('link', { name: 'Data Analyst' })
			await expect
				.element(analystLink)
				.toHaveAttribute('href', '/agent/analyst')
		})
	})

	describe('localStorage integration', () => {
		it('initializes sidebar as collapsed when localStorage has collapsed state', async () => {
			localStorageMock.getItem.mockReturnValue('collapsed')

			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			// App name should be hidden (collapsed state)
			await expect.element(page.getByText('TestApp')).not.toBeInTheDocument()
		})

		it('initializes sidebar as open when localStorage is empty', async () => {
			localStorageMock.getItem.mockReturnValue(null)

			mockSetAgents(createMockAgents())
			render(AppSidebarTestWrapper)

			// App name should be visible (open state)
			await expect.element(page.getByText('TestApp')).toBeVisible()
		})
	})
})
