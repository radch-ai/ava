import { afterEach, describe, expect, it, vi } from 'vitest'
import { page, userEvent } from 'vitest/browser'
import { cleanup, render } from 'vitest-browser-svelte'
import TextArea from '#routes/agent/[agentId]/TextArea.svelte'

describe('TextArea', () => {
	afterEach(async () => {
		cleanup()
	})

	describe('Rendering', () => {
		it('renders textarea input with placeholder', async () => {
			render(TextArea)

			const textarea = page.getByPlaceholder('Enter your query!')
			await expect.element(textarea).toBeVisible()
		})

		it('renders plus button for attachments', async () => {
			render(TextArea)

			const plusButton = page.getByRole('button').first()
			await expect.element(plusButton).toBeVisible()
		})

		it('renders send button (arrow up)', async () => {
			render(TextArea)

			const sendButton = page.getByRole('button').nth(1)
			await expect.element(sendButton).toBeVisible()
		})
	})

	describe('Send button state', () => {
		it('is disabled when input is empty', async () => {
			render(TextArea)

			const sendButton = page.getByRole('button').nth(1)
			await expect.element(sendButton).toBeDisabled()
		})

		it('is enabled when input has text', async () => {
			render(TextArea)

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.fill('Hello world')

			const sendButton = page.getByRole('button').nth(1)
			await expect.element(sendButton).not.toBeDisabled()
		})

		it('is disabled when input is only whitespace', async () => {
			render(TextArea)

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.fill('   ')

			const sendButton = page.getByRole('button').nth(1)
			await expect.element(sendButton).toBeDisabled()
		})
	})

	describe('Keyboard submission', () => {
		it('submits on Enter key', async () => {
			const mockSubmit = vi.fn()
			render(TextArea, { onsubmit: mockSubmit })

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'Test message{Enter}')

			expect(mockSubmit).toHaveBeenCalledWith('Test message')
		})

		it('does NOT submit on Shift+Enter (allows newlines)', async () => {
			const mockSubmit = vi.fn()
			render(TextArea, { onsubmit: mockSubmit })

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'Test message{Shift>}{Enter}{/Shift}')

			expect(mockSubmit).not.toHaveBeenCalled()
		})

		it('does not submit empty input on Enter', async () => {
			const mockSubmit = vi.fn()
			render(TextArea, { onsubmit: mockSubmit })

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.keyboard('{Enter}')

			expect(mockSubmit).not.toHaveBeenCalled()
		})

		it('does not submit whitespace-only input on Enter', async () => {
			const mockSubmit = vi.fn()
			render(TextArea, { onsubmit: mockSubmit })

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.fill('   ')
			await textarea.click()
			await userEvent.keyboard('{Enter}')

			expect(mockSubmit).not.toHaveBeenCalled()
		})
	})

	describe('Click submission', () => {
		it('clears input after submit via button click', async () => {
			render(TextArea)

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.fill('Hello world')

			const sendButton = page.getByRole('button').nth(1)
			await sendButton.click()

			await expect.element(textarea).toHaveValue('')
		})

		it('clears input after submit via Enter key', async () => {
			render(TextArea)

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'Hello world{Enter}')

			await expect.element(textarea).toHaveValue('')
		})
	})

	describe('Queue-aware submission', () => {
		it('keeps the draft and shows the reason when submission rejects', async () => {
			render(TextArea, {
				onsubmit: () => Promise.reject(new Error('queue is full'))
			})

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'overflow{Enter}')

			await expect.element(page.getByText('queue is full')).toBeVisible()
			await expect.element(textarea).toHaveValue('overflow')
		})

		it('keeps text typed while waiting; only the accepted draft clears', async () => {
			const { promise, resolve } = Promise.withResolvers<void>()
			render(TextArea, { onsubmit: () => promise })

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, 'first{Enter}')
			await textarea.fill('edited while waiting')

			resolve()
			await expect.element(textarea).toHaveValue('edited while waiting')
		})

		it('replaces Send with Stop while a turn runs', async () => {
			render(TextArea, {
				onsubmit: () => Promise.resolve(),
				working: true
			})

			await expect
				.element(page.getByRole('button', { name: 'Stop' }))
				.toBeVisible()
			await expect
				.element(page.getByRole('button', { name: 'Send' }))
				.not.toBeInTheDocument()
		})

		it('keeps exactly one submit control in the slot', async () => {
			render(TextArea, {
				onsubmit: () => Promise.resolve(),
				working: true
			})

			const controls = page.getByRole('button', { name: /^(Stop|Send)$/ })
			await vi.waitFor(() => {
				expect(controls.elements()).toHaveLength(1)
			})
		})
	})

	describe('onsubmit callback', () => {
		it('calls onsubmit with trimmed text', async () => {
			const mockSubmit = vi.fn()
			render(TextArea, { onsubmit: mockSubmit })

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.click()
			await userEvent.type(textarea, '  Hello world  {Enter}')

			expect(mockSubmit).toHaveBeenCalledWith('Hello world')
		})

		it('calls onsubmit when send button is clicked', async () => {
			const mockSubmit = vi.fn()
			render(TextArea, { onsubmit: mockSubmit })

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.fill('Test message')

			const sendButton = page.getByRole('button').nth(1)
			await sendButton.click()

			expect(mockSubmit).toHaveBeenCalledWith('Test message')
		})

		it('does not throw when onsubmit is not provided', async () => {
			render(TextArea)

			const textarea = page.getByPlaceholder('Enter your query!')
			await textarea.fill('Test message')

			const sendButton = page.getByRole('button').nth(1)
			await expect(async () => {
				await sendButton.click()
			}).not.toThrow()
		})
	})

	describe('Styling', () => {
		it('has textarea with min-h-[40px] for proper sizing', async () => {
			render(TextArea)

			const textarea = page.getByPlaceholder('Enter your query!')
			const textareaElement = textarea.element()
			expect(textareaElement.className).toContain('min-h-[40px]')
		})

		it('has text-lg for larger text content', async () => {
			render(TextArea)

			const textarea = page.getByPlaceholder('Enter your query!')
			const textareaElement = textarea.element()
			expect(textareaElement.className).toContain('!text-lg')
		})

		it('has attach button with size-9 class', async () => {
			render(TextArea)

			const attachButton = page.getByRole('button').first()
			const buttonElement = attachButton.element()
			expect(buttonElement.className).toContain('size-9')
		})

		it('has send button with size-9 class', async () => {
			render(TextArea)

			const sendButton = page.getByRole('button').nth(1)
			const buttonElement = sendButton.element()
			expect(buttonElement.className).toContain('size-9')
		})

		it('has rounded-3xl container', async () => {
			render(TextArea)

			const textarea = page.getByPlaceholder('Enter your query!')
			const textareaElement = textarea.element()

			const container = textareaElement.closest('.rounded-3xl')
			expect(container).not.toBeNull()
		})
	})
})
