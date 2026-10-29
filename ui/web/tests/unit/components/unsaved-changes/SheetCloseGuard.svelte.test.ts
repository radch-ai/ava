import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import SheetCloseGuard from '#components/unsaved-changes/SheetCloseGuard.svelte'

/**
 * Level-4 handler coverage: the guard's exported escape/outside handlers are
 * wired by the sheet (ConfigSheet) to Sheet.Content's close hooks, so they are
 * exercised here directly — bits-ui's dismissible-layer dispatch needs real
 * trusted pointer input that a component fixture cannot aim outside the sheet
 * (its content fills the fixture viewport). The end-to-end backdrop click is
 * covered by the e2e suite.
 */
describe('SheetCloseGuard', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(async () => {
		cleanup()
	})

	describe('onInteractOutside', () => {
		it('prevents the close and shows the dialog when there are changes', async () => {
			const onDiscard = vi.fn()
			const { component } = await render(SheetCloseGuard, {
				hasChanges: true,
				onDiscard
			})

			const event = new PointerEvent('pointerdown', { cancelable: true })
			const preventDefault = vi.spyOn(event, 'preventDefault')
			component.onInteractOutside(event)

			expect(preventDefault).toHaveBeenCalled()
			await expect
				.element(
					page.getByRole('alertdialog', { name: /discard unsaved changes/i })
				)
				.toBeVisible()
			expect(onDiscard).not.toHaveBeenCalled()
		})

		it('does nothing when there are no changes', async () => {
			const onDiscard = vi.fn()
			const { component } = await render(SheetCloseGuard, {
				hasChanges: false,
				onDiscard
			})

			const event = new PointerEvent('pointerdown', { cancelable: true })
			const preventDefault = vi.spyOn(event, 'preventDefault')
			component.onInteractOutside(event)

			expect(preventDefault).not.toHaveBeenCalled()
			expect(
				await page.getByRole('alertdialog', { name: /discard/i }).query()
			).toBeNull()
			expect(onDiscard).not.toHaveBeenCalled()
		})

		it('discards (closing the sheet) when Discard is confirmed', async () => {
			const onDiscard = vi.fn()
			const { component } = await render(SheetCloseGuard, {
				hasChanges: true,
				onDiscard
			})

			component.onInteractOutside(new PointerEvent('pointerdown'))
			await page.getByRole('button', { name: /discard/i }).click()

			expect(onDiscard).toHaveBeenCalledTimes(1)
		})

		it('keeps the sheet open when Keep Editing is confirmed', async () => {
			const onDiscard = vi.fn()
			const { component } = await render(SheetCloseGuard, {
				hasChanges: true,
				onDiscard
			})

			component.onInteractOutside(new PointerEvent('pointerdown'))
			await page.getByRole('button', { name: /keep editing/i }).click()

			expect(onDiscard).not.toHaveBeenCalled()
			expect(
				await page.getByRole('alertdialog', { name: /discard/i }).query()
			).toBeNull()
		})
	})

	describe('onEscapeKeydown', () => {
		it('prevents the close and shows the dialog when there are changes', async () => {
			const onDiscard = vi.fn()
			const { component } = await render(SheetCloseGuard, {
				hasChanges: true,
				onDiscard
			})

			const event = new KeyboardEvent('keydown', { cancelable: true })
			const preventDefault = vi.spyOn(event, 'preventDefault')
			component.onEscapeKeydown(event)

			expect(preventDefault).toHaveBeenCalled()
			await expect
				.element(
					page.getByRole('alertdialog', { name: /discard unsaved changes/i })
				)
				.toBeVisible()
			expect(onDiscard).not.toHaveBeenCalled()
		})
	})
})
