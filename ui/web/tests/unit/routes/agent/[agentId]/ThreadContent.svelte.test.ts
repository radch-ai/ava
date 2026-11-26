import { afterEach, describe, expect, it } from 'vitest'
import { page } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import ThreadContent from '#routes/agent/[agentId]/ThreadContent.svelte'

interface ThreadItem {
	id: string
	role: 'user' | 'assistant' | 'tool' | 'error' | 'divider' | 'compacting'
	content: string
	pending?: boolean
	name?: string
	args?: string
	result?: string
	error?: string | null
	isSummary?: boolean
}

const mockMessages: ThreadItem[] = [
	{ id: '1', role: 'user', content: 'Hello' },
	{ id: '2', role: 'assistant', content: 'Hi there!' }
]

describe('ThreadContent', () => {
	afterEach(async () => {
		cleanup()
	})

	describe('User messages', () => {
		it('renders user message with right alignment', async () => {
			render(ThreadContent, { threadItems: [mockMessages[0]] })

			const message = page.getByText('Hello')
			const element = await message.element()

			expect(element.className).toContain('ml-auto')
		})

		it('applies user-specific rounded styling', async () => {
			render(ThreadContent, { threadItems: [mockMessages[0]] })

			const message = page.getByText('Hello')
			const element = await message.element()

			expect(element.className).toContain('rounded-br-[4px]')
		})

		it('applies user-specific background styling', async () => {
			render(ThreadContent, { threadItems: [mockMessages[0]] })

			const message = page.getByText('Hello')
			const element = await message.element()

			expect(element.className).toContain('bg-overlay-dark/70')
		})

		it('renders multiple user messages', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'user', content: 'First message' },
					{ id: '2', role: 'user', content: 'Second message' }
				]
			})

			await expect.element(page.getByText('First message')).toBeVisible()
			await expect.element(page.getByText('Second message')).toBeVisible()
		})
	})

	describe('Assistant messages', () => {
		// Assistant text renders as markdown, so styling pins target the
		// message container, not the paragraph the text resolves to.
		it('renders assistant message full-width in the column', async () => {
			render(ThreadContent, { threadItems: [mockMessages[1]] })

			const element = await page.getByText('Hi there!').element()

			expect(element.closest('div')?.className).toContain('w-full')
		})

		it('renders assistant text at full foreground', async () => {
			render(ThreadContent, { threadItems: [mockMessages[1]] })

			const element = await page.getByText('Hi there!').element()

			expect(element.closest('div')?.className).toContain(
				'text-foreground mb-4'
			)
		})

		it('applies spacing to assistant messages', async () => {
			render(ThreadContent, { threadItems: [mockMessages[1]] })

			const element = await page.getByText('Hi there!').element()

			expect(element.closest('div')?.className).toContain('mb-4')
		})

		it('renders multiple assistant messages', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'assistant', content: 'Response one' },
					{ id: '2', role: 'assistant', content: 'Response two' }
				]
			})

			await expect.element(page.getByText('Response one')).toBeVisible()
			await expect.element(page.getByText('Response two')).toBeVisible()
		})
	})

	describe('Empty items', () => {
		it('renders nothing when threadItems is empty', async () => {
			render(ThreadContent, { threadItems: [] })

			expect(
				page.getByText('No messages yet. Start a conversation!').query()
			).toBeNull()
		})

		it('renders nothing when threadItems is empty — no buttons', async () => {
			render(ThreadContent, { threadItems: [] })

			expect(page.getByRole('button', { name: 'Load more' }).query()).toBeNull()
		})
	})

	describe('Multiple messages', () => {
		it('renders conversation with alternating roles', async () => {
			render(ThreadContent, { threadItems: mockMessages })

			await expect.element(page.getByText('Hello')).toBeVisible()
			await expect.element(page.getByText('Hi there!')).toBeVisible()
		})

		it('renders long conversation', async () => {
			const longConversation: ThreadItem[] = Array.from(
				{ length: 10 },
				(_, i) => ({
					id: `msg-${i}`,
					role: i % 2 === 0 ? ('user' as const) : ('assistant' as const),
					content: `Message ${i + 1}`
				})
			)

			render(ThreadContent, { threadItems: longConversation })

			await expect
				.element(page.getByText('Message 1', { exact: true }))
				.toBeVisible()
			await expect
				.element(page.getByText('Message 5', { exact: true }))
				.toBeVisible()
			await expect
				.element(page.getByText('Message 10', { exact: true }))
				.toBeVisible()
		})

		it('maintains message order', async () => {
			render(ThreadContent, { threadItems: mockMessages })

			const firstMessage = page.getByText('Hello')
			const secondMessage = page.getByText('Hi there!')

			await expect.element(firstMessage).toBeVisible()
			await expect.element(secondMessage).toBeVisible()
		})
	})

	describe('Live turn rendering', () => {
		it('renders an error message with destructive styling', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: 'e1', role: 'error', content: 'read default model: no rows' }
				]
			})

			const message = page.getByText('read default model: no rows')
			const element = await message.element()

			expect(element.className).toContain('text-destructive')
		})

		it('streams without a pulse caret — the composer Stop is the indicator', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: 'a1', role: 'assistant', content: 'hel', pending: true }
				]
			})

			const element = await page.getByText('hel', { exact: true }).element()
			expect(element.closest('div')?.querySelector('.animate-pulse')).toBeNull()
		})
	})

	describe('Long message handling', () => {
		it('renders very long message content', async () => {
			const longContent = 'A'.repeat(1000)
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'user', content: longContent }]
			})

			await expect.element(page.getByText(longContent)).toBeVisible()
		})

		it('applies max-width constraint to messages', async () => {
			const longContent = 'A'.repeat(500)
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'user', content: longContent }]
			})

			const message = page.getByText(longContent)
			const element = await message.element()

			expect(element.className).toContain('max-w-[75%]')
		})

		it('renders multi-line messages line by line', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'assistant', content: 'Line 1\nLine 2\nLine 3' }
				]
			})

			await expect.element(page.getByText('Line 1')).toBeVisible()
			await expect.element(page.getByText('Line 2')).toBeVisible()
			await expect.element(page.getByText('Line 3')).toBeVisible()
		})

		it('handles very long words without breaking layout', async () => {
			const longWord = 'supercalifragilisticexpialidocious'.repeat(5)
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'user', content: longWord }]
			})

			await expect.element(page.getByText(longWord)).toBeVisible()
		})
	})

	describe('Special characters in messages', () => {
		it('renders HTML-like content safely', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'user', content: '<script>alert("xss")</script>' }
				]
			})

			await expect
				.element(page.getByText('<script>alert("xss")</script>'))
				.toBeVisible()
		})

		it('renders quotes and apostrophes', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'assistant', content: `It's a "quoted" message` }
				]
			})

			await expect
				.element(page.getByText(`It's a "quoted" message`))
				.toBeVisible()
		})

		it('renders unicode characters', async () => {
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'user', content: 'Hello 世界 🌍 emoji' }]
			})

			await expect.element(page.getByText('Hello 世界 🌍 emoji')).toBeVisible()
		})

		it('renders special symbols', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'assistant', content: 'Math: ∑ ∫ π ∞ ≤ ≥ ≠' }
				]
			})

			await expect.element(page.getByText('Math: ∑ ∫ π ∞ ≤ ≥ ≠')).toBeVisible()
		})

		it('renders ampersands and angle brackets', async () => {
			render(ThreadContent, {
				threadItems: [
					{
						id: '1',
						role: 'user',
						content: 'Use && for AND, || for OR, < > compare'
					}
				]
			})

			await expect
				.element(page.getByText('Use && for AND, || for OR, < > compare'))
				.toBeVisible()
		})

		it('renders markdown emphasis as real elements', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'assistant', content: '**bold** _italic_ `code`' }
				]
			})

			await expect.element(page.getByText('bold')).toBeVisible()
			const bold = await page.getByText('bold').element()
			expect(bold.tagName).toBe('STRONG')
			const code = await page.getByText('code').element()
			expect(code.tagName).toBe('CODE')
		})
	})

	describe('Props handling', () => {
		it('handles only threadItems prop', async () => {
			expect(() => {
				render(ThreadContent, { threadItems: mockMessages })
			}).not.toThrow()
		})

		it('handles empty threadItems array', async () => {
			expect(() => {
				render(ThreadContent, { threadItems: [] })
			}).not.toThrow()
		})
	})

	describe('Assistant markdown', () => {
		it('renders a markdown heading as a real heading element', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'assistant', content: '## Section title' }
				]
			})

			await expect
				.element(page.getByRole('heading', { name: 'Section title' }))
				.toBeVisible()
		})

		it('renders a fenced code block as preformatted code', async () => {
			render(ThreadContent, {
				threadItems: [
					{
						id: '1',
						role: 'assistant',
						content: '```go\nfmt.Println("hi")\n```'
					}
				]
			})

			await expect.element(page.getByText('fmt.Println("hi")')).toBeVisible()
			const element = await page.getByText('fmt.Println("hi")').element()
			expect(element.closest('pre')).not.toBeNull()
		})

		it('does not survive a script tag in model output', async () => {
			render(ThreadContent, {
				threadItems: [
					{
						id: '1',
						role: 'assistant',
						content: 'ok <script>window.pwned=1</script>'
					}
				]
			})

			await expect.element(page.getByText('ok')).toBeVisible()
			const container = (await page.getByText('ok').element()).closest('div')
			expect(container?.querySelectorAll('script')).toHaveLength(0)
		})
	})

	describe('Tool entries', () => {
		it('renders a persisted tool row as a single dimmed line that expands to one box', async () => {
			render(ThreadContent, {
				threadItems: [
					{
						id: 't1',
						role: 'tool',
						content:
							'{"name":"web_search","args":{"query":"otters"},"result":"\\"2 hits\\""}'
					}
				]
			})

			await expect.element(page.getByText(/web_search/)).toBeVisible()
			// Collapsed: the result stays out of view.
			await expect.element(page.getByText(/2 hits/)).not.toBeVisible()

			await page.getByRole('button').click()
			await expect.element(page.getByText(/2 hits/)).toBeVisible()
			// One box, no Args/Result section labels.
			expect(page.getByText('Args').elements()).toHaveLength(0)
			expect(page.getByText('Result').elements()).toHaveLength(0)
			const boxes = document.querySelectorAll('pre')
			expect(boxes).toHaveLength(1)
		})

		it('renders a live pending tool entry with its pulse dot', async () => {
			render(ThreadContent, {
				threadItems: [
					{
						id: 'call-1',
						role: 'tool',
						content: 'web_search',
						name: 'web_search',
						args: '{"query":"otters"}',
						pending: true
					}
				]
			})

			await expect.element(page.getByText(/web_search/)).toBeVisible()
			const line = await page.getByRole('button').element()
			expect(line.querySelector('.animate-pulse')).not.toBeNull()
		})

		it('marks a failed tool row with a red failure icon', async () => {
			render(ThreadContent, {
				threadItems: [
					{
						id: 't2',
						role: 'tool',
						content:
							'{"name":"web_search","args":{},"result":"","error":"search backend down"}'
					}
				]
			})

			const trigger = await page.getByRole('button').element()
			const icon = trigger.querySelector('svg')
			expect(icon?.className.baseVal).toContain('text-destructive')
		})

		it('tints a failed tool result', async () => {
			render(ThreadContent, {
				threadItems: [
					{
						id: 't2',
						role: 'tool',
						content:
							'{"name":"web_search","args":{},"result":"","error":"search backend down"}'
					}
				]
			})

			await page.getByRole('button').click()
			const result = await page.getByText('search backend down').element()
			expect(result.className).toContain('text-destructive')
		})
	})

	describe('Reasoning marker', () => {
		it('blinks while the lane reasons — the marker stands in for the answer row', async () => {
			render(ThreadContent, {
				threadItems: [{ id: 'm1', role: 'assistant', content: '' }]
			})

			const marker = await page.getByText('Thinking').element()
			expect(
				marker.closest('div')?.querySelector('.animate-pulse')
			).not.toBeNull()
		})

		it('does not render once the answer exists', async () => {
			render(ThreadContent, {
				threadItems: [{ id: 'm2', role: 'assistant', content: 'the answer' }]
			})

			expect(page.getByText('Thinking').elements()).toHaveLength(0)
			await expect.element(page.getByText('the answer')).toBeVisible()
		})
	})

	describe('Compacting marker', () => {
		it('blinks in-stream while the lane summarizes — the same indicator the thinking pulse renders', async () => {
			render(ThreadContent, {
				threadItems: [{ id: 'm1', role: 'compacting', content: '' }]
			})

			const marker = await page.getByText('Compacting').element()
			expect(
				marker.closest('div')?.querySelector('.animate-pulse')
			).not.toBeNull()
		})
	})

	describe('Compacted summary', () => {
		it('renders collapsed behind its boundary — the label, never the prose', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: 't2', role: 'divider', content: 'Compacted' },
					{
						id: 'm1',
						role: 'assistant',
						content: 'the summary of everything before',
						isSummary: true
					}
				]
			})

			await expect.element(page.getByText('Compacted summary')).toBeVisible()
			await expect
				.element(page.getByText(/summary of everything/))
				.not.toBeVisible()
		})

		it('expands in place to the summary text', async () => {
			render(ThreadContent, {
				threadItems: [
					{
						id: 'm1',
						role: 'assistant',
						content: 'the summary of everything before',
						isSummary: true
					}
				]
			})

			await page.getByText('Compacted summary').click()
			await expect
				.element(page.getByText(/summary of everything/))
				.toBeVisible()
		})
	})

	describe('Session dividers', () => {
		it('renders the boundary label between messages', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'user', content: 'before' },
					{ id: 'thread-2', role: 'divider', content: 'New session' },
					{ id: '2', role: 'assistant', content: 'after' }
				]
			})

			await expect.element(page.getByText('New session')).toBeVisible()
			await expect.element(page.getByText('before')).toBeVisible()
			await expect.element(page.getByText('after')).toBeVisible()
		})

		it('keeps the divider keyed apart from a message with the same id', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: 'thread-2', role: 'user', content: 'same id row' },
					{ id: 'thread-2', role: 'divider', content: 'New session' }
				]
			})

			await expect.element(page.getByText('same id row')).toBeVisible()
			await expect.element(page.getByText('New session')).toBeVisible()
		})
	})
})
