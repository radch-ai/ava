import type { Mock } from 'vitest'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import { UnsavedChangesGuards } from '#components/unsaved-changes'

// Mock SvelteKit navigation — beforeNavigate captures the guard's
// interceptor so tests can fire it with synthetic navigations.
vi.mock('$app/navigation', () => ({
	goto: vi.fn(),
	invalidate: vi.fn(),
	beforeNavigate: vi.fn()
}))

vi.mock('$app/paths', () => ({
	resolve: vi.fn((path: string) => path)
}))

import { type BeforeNavigate, beforeNavigate, goto } from '$app/navigation'

const mockGoto = vi.mocked(goto)

function fireBeforeUnload() {
	const event = new Event('beforeunload', { cancelable: true })
	const spy = vi.spyOn(event, 'preventDefault')
	window.dispatchEvent(event)
	return spy
}

function navCallback(): (nav: BeforeNavigate) => void {
	const mock = vi.mocked(beforeNavigate)
	expect(mock).toHaveBeenCalled()
	return mock.mock.calls[mock.mock.calls.length - 1][0] as unknown as (
		nav: BeforeNavigate
	) => void
}

function makeNavigation(type = 'link'): BeforeNavigate & { cancel: Mock } {
	return {
		type,
		to: { url: new URL('http://localhost/agent/_superagent') },
		cancel: vi.fn(),
		willUnload: type === 'leave'
	} as unknown as BeforeNavigate & { cancel: Mock }
}

describe('UnsavedChangesGuards', () => {
	beforeEach(() => {
		vi.clearAllMocks()
	})

	afterEach(() => {
		cleanup()
	})

	describe('browser-level guard (beforeunload)', () => {
		it('prevents beforeunload when there are unsaved changes', async () => {
			await render(UnsavedChangesGuards, { hasChanges: true })

			expect(fireBeforeUnload()).toHaveBeenCalled()
		})

		it('does not prevent beforeunload when there are no changes', async () => {
			await render(UnsavedChangesGuards, { hasChanges: false })

			expect(fireBeforeUnload()).not.toHaveBeenCalled()
		})
	})

	describe('in-app navigation guard (beforeNavigate)', () => {
		it('cancels the navigation and shows the dialog when dirty', async () => {
			await render(UnsavedChangesGuards, { hasChanges: true })

			const nav = makeNavigation('link')
			navCallback()(nav)

			expect(nav.cancel).toHaveBeenCalled()
			await expect.element(page.getByRole('alertdialog')).toBeVisible()
		})

		it('does not intercept navigation when clean', async () => {
			await render(UnsavedChangesGuards, { hasChanges: false })

			const nav = makeNavigation('link')
			navCallback()(nav)

			expect(nav.cancel).not.toHaveBeenCalled()
			expect(page.getByRole('alertdialog').query()).toBeNull()
		})

		it('does not cancel leave navigations — beforeunload owns those', async () => {
			await render(UnsavedChangesGuards, { hasChanges: true })

			const nav = makeNavigation('leave')
			navCallback()(nav)

			expect(nav.cancel).not.toHaveBeenCalled()
		})
	})

	describe('dialog actions', () => {
		it('discard runs onDiscard and resumes the navigation', async () => {
			const onDiscard = vi.fn()
			await render(UnsavedChangesGuards, { hasChanges: true, onDiscard })

			navCallback()(makeNavigation('link'))
			await page.getByRole('button', { name: 'Discard' }).click()

			expect(onDiscard).toHaveBeenCalledOnce()
			expect(mockGoto).toHaveBeenCalledWith('/agent/_superagent')
		})

		it('keep editing hides the dialog and does not navigate', async () => {
			await render(UnsavedChangesGuards, { hasChanges: true })

			navCallback()(makeNavigation('link'))
			await page.getByRole('button', { name: 'Keep Editing' }).click()
			expect(page.getByRole('alertdialog').query()).toBeNull()
			expect(mockGoto).not.toHaveBeenCalled()
		})
	})

	describe('bypassNavigation', () => {
		it('runs the callback without the guard interfering, then re-arms', async () => {
			const { component } = await render(UnsavedChangesGuards, {
				hasChanges: true
			})
			const action = vi.fn()
			await component.bypassNavigation(action)

			expect(action).toHaveBeenCalledOnce()

			// still dirty afterwards: the next navigation must be cancelled again
			const nav = makeNavigation('link')
			navCallback()(nav)
			expect(nav.cancel).toHaveBeenCalled()
		})

		it('keeps the bypass open while the leave it started is unloading', async () => {
			const { component } = await render(UnsavedChangesGuards, {
				hasChanges: true
			})

			// history.back() returns before the browser starts unloading —
			// one microtask later the leave is still in flight, so the
			// guard must not block it with its own beforeunload dialog.
			const leaving = component.bypassNavigation(() => {})
			await Promise.resolve()
			expect(fireBeforeUnload()).not.toHaveBeenCalled()

			await leaving
			// window closed: still dirty means guarded again
			expect(fireBeforeUnload()).toHaveBeenCalled()
		})
	})
})
