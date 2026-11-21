import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-svelte'
import MarkdownBody from '#routes/agent/[agentId]/MarkdownBody.svelte'

// The throttle is a fixed deadline, not a reset-per-change debounce: a
// stream of interim chunks must neither postpone the deadline nor render
// per chunk, and the deadline renders the newest content.

describe('MarkdownBody', () => {
	afterEach(() => {
		cleanup()
		vi.useRealTimers()
	})

	it('renders the mount content synchronously', () => {
		const { container } = render(MarkdownBody, { content: 'alpha' })
		expect(container.textContent).toContain('alpha')
	})

	it('re-parses on a fixed 100ms deadline that renders the newest content', async () => {
		vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] })
		const { container, rerender } = render(MarkdownBody, { content: 'alpha' })
		expect(container.textContent).toContain('alpha')

		// A change inside the window arms one deadline.
		await rerender({ content: 'bravo' })
		await vi.advanceTimersByTimeAsync(40)
		expect(container.textContent).toContain('alpha')

		// An interim chunk must not postpone the armed deadline.
		await rerender({ content: 'charlie' })
		await vi.advanceTimersByTimeAsync(40)
		expect(container.textContent).toContain('alpha')

		// The deadline fired with the newest content — never the
		// intermediate one.
		await vi.advanceTimersByTimeAsync(20)
		expect(container.textContent).toContain('charlie')
		expect(container.textContent).not.toContain('alpha')

		// After the deadline the next change renders on its own fresh
		// window; idle past it, the final content stands.
		await rerender({ content: 'delta' })
		await vi.advanceTimersByTimeAsync(200)
		expect(container.textContent).toContain('delta')
		expect(container.textContent).not.toContain('charlie')
	})
})
