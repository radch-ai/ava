import { describe, expect, it } from 'vitest'
import { page } from 'vitest/browser'
import { render } from 'vitest-browser-svelte'
import AgentIconPicker from '#components/agent-config/AgentIconPicker.svelte'
import type { AgentIconName } from '#components/agent-icons/index.ts'

describe('AgentIconPicker', () => {
	async function openDropdown() {
		await page.getByRole('button', { name: 'Icon: bot. Change icon.' }).click()
	}

	it('renders icons grouped by category', async () => {
		render(AgentIconPicker, {
			value: 'bot',
			onselect: () => {}
		})

		await openDropdown()

		// Check category headers exist
		await expect.element(page.getByText('Communication')).toBeVisible()
		await expect.element(page.getByText('Ai')).toBeVisible()

		// Check some icon buttons exist (by their accessible name via aria-label)
		await expect
			.element(page.getByRole('button', { name: 'message-square' }))
			.toBeVisible()
		await expect
			.element(page.getByRole('button', { name: 'bot', exact: true }))
			.toBeVisible()
	})

	it('highlights the currently selected icon', async () => {
		render(AgentIconPicker, {
			value: 'bot',
			onselect: () => {}
		})

		await openDropdown()

		const selectedIcon = page.getByRole('button', { name: 'bot', exact: true })
		// Selected icon should have ring-2 class
		await expect.element(selectedIcon).toHaveClass('ring-2')
	})

	it('calls onselect when an icon is clicked', async () => {
		let selectedIcon: AgentIconName | undefined

		render(AgentIconPicker, {
			value: 'bot',
			onselect: (icon: AgentIconName) => {
				selectedIcon = icon
			}
		})

		await openDropdown()
		await page.getByRole('button', { name: 'sparkles' }).click()

		expect(selectedIcon).toBe('sparkles')
	})

	it('shows the selected icon in the trigger button', async () => {
		render(AgentIconPicker, {
			value: 'brain',
			onselect: () => {}
		})

		// The trigger announces the selected icon in its accessible name
		const trigger = page.getByRole('button', {
			name: 'Icon: brain. Change icon.'
		})
		await expect.element(trigger).toBeVisible()
	})

	it('closes dropdown after selecting an icon', async () => {
		render(AgentIconPicker, {
			value: 'bot',
			onselect: () => {}
		})

		await openDropdown()
		// Verify dropdown is open by checking category header
		await expect.element(page.getByText('Communication')).toBeVisible()

		await page.getByRole('button', { name: 'sparkles' }).click()

		// Dropdown should be closed - icon buttons in dropdown should be gone
		const dropdownIconButtons = await page
			.getByRole('button', { name: 'message-square' })
			.elements()
		expect(dropdownIconButtons).toHaveLength(0)
	})

	it('drops the panel under the trigger inside a containing block (bits-ui sheet sets contain: layout style)', async () => {
		render(AgentIconPicker, {
			value: 'bot',
			onselect: () => {}
		})

		const trigger = page.getByRole('button', {
			name: 'Icon: bot. Change icon.'
		})
		await expect.element(trigger).toBeVisible()

		// Reparent the picker into a containing block offset from the viewport
		// origin, mimicking the config sheet's dialog (contain: layout style
		// makes it the containing block for fixed-position descendants).
		const triggerEl = (await trigger.elements())[0]
		const pickerRoot = triggerEl?.parentElement
		if (!pickerRoot) throw new Error('picker root not found')
		const host = document.createElement('div')
		host.style.cssText =
			'position: fixed; left: 600px; top: 100px; width: 600px; height: 400px; contain: layout style;'
		document.body.appendChild(host)
		host.appendChild(pickerRoot)

		await trigger.click()

		const item = (
			await page.getByRole('button', { name: 'sparkles' }).elements()
		)[0]
		const panel = item?.closest('[data-slot="dropdown-menu-content"]')
		if (!panel) throw new Error('dropdown panel not found')
		const panelRect = panel.getBoundingClientRect()
		const triggerX = Math.round(triggerEl.getBoundingClientRect().x)
		const panelX = Math.round(panelRect.x)

		// The panel must stay near the trigger and inside the viewport — the
		// pre-portal hand-rolled fixed panel landed at triggerX + 600 (the
		// containing block's own offset), off-screen to the right.
		expect(Math.abs(panelX - triggerX)).toBeLessThan(300)
		expect(panelX).toBeGreaterThanOrEqual(0)
		expect(panelX + panelRect.width).toBeLessThanOrEqual(window.innerWidth)
	})
})
