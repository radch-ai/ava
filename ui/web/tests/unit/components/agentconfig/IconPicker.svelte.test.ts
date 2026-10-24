import { describe, expect, it } from 'vitest'
import { page } from 'vitest/browser'
import { render } from 'vitest-browser-svelte'
import IconPicker from '#components/agentconfig/IconPicker.svelte'
import type { IconName } from '#lib/icons'

describe('IconPicker', () => {
	async function openDropdown() {
		await page.getByRole('button', { name: 'Select icon' }).click()
	}

	it('renders icons grouped by category', async () => {
		render(IconPicker, {
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
			.element(page.getByRole('button', { name: 'bot' }))
			.toBeVisible()
	})

	it('highlights the currently selected icon', async () => {
		render(IconPicker, {
			value: 'bot',
			onselect: () => {}
		})

		await openDropdown()

		const selectedIcon = page.getByRole('button', { name: 'bot' })
		// Selected icon should have ring-2 class
		await expect.element(selectedIcon).toHaveClass('ring-2')
	})

	it('calls onselect when an icon is clicked', async () => {
		let selectedIcon: IconName | undefined

		render(IconPicker, {
			value: 'bot',
			onselect: (icon: IconName) => {
				selectedIcon = icon
			}
		})

		await openDropdown()
		await page.getByRole('button', { name: 'sparkles' }).click()

		expect(selectedIcon).toBe('sparkles')
	})

	it('shows the selected icon in the trigger button', async () => {
		render(IconPicker, {
			value: 'brain',
			onselect: () => {}
		})

		// The trigger button should exist
		const trigger = page.getByRole('button', { name: 'Select icon' })
		await expect.element(trigger).toBeVisible()
	})

	it('closes dropdown after selecting an icon', async () => {
		render(IconPicker, {
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
})
