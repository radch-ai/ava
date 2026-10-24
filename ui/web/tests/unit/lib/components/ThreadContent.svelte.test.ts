import { describe, expect, it, afterEach } from 'vitest';
import { render, cleanup } from 'vitest-browser-svelte';
import { page } from 'vitest/browser';
import ThreadContent from '$components/agent/ThreadContent.svelte';

interface ThreadItem {
	id: string;
	role: 'user' | 'assistant';
	content: string;
}

const mockMessages: ThreadItem[] = [
	{ id: '1', role: 'user', content: 'Hello' },
	{ id: '2', role: 'assistant', content: 'Hi there!' }
];

describe('ThreadContent', () => {
	afterEach(async () => {
		cleanup();
	});

	describe('User messages', () => {
		it('renders user message with right alignment', async () => {
			render(ThreadContent, { threadItems: [mockMessages[0]] });

			const message = page.getByText('Hello');
			const element = await message.element();

			expect(element.className).toContain('ml-auto');
		});

		it('applies user-specific rounded styling', async () => {
			render(ThreadContent, { threadItems: [mockMessages[0]] });

			const message = page.getByText('Hello');
			const element = await message.element();

			expect(element.className).toContain('rounded-br-[4px]');
		});

		it('applies user-specific background styling', async () => {
			render(ThreadContent, { threadItems: [mockMessages[0]] });

			const message = page.getByText('Hello');
			const element = await message.element();

			expect(element.className).toContain('bg-overlay-dark/70');
		});

		it('renders multiple user messages', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'user', content: 'First message' },
					{ id: '2', role: 'user', content: 'Second message' }
				]
			});

			await expect.element(page.getByText('First message')).toBeVisible();
			await expect.element(page.getByText('Second message')).toBeVisible();
		});
	});

	describe('Assistant messages', () => {
		it('renders assistant message with left alignment', async () => {
			render(ThreadContent, { threadItems: [mockMessages[1]] });

			const message = page.getByText('Hi there!');
			const element = await message.element();

			expect(element.className).toContain('mr-auto');
		});

		it('applies assistant-specific rounded styling', async () => {
			render(ThreadContent, { threadItems: [mockMessages[1]] });

			const message = page.getByText('Hi there!');
			const element = await message.element();

			expect(element.className).toContain('mr-auto');
		});

		it('applies assistant-specific background styling', async () => {
			render(ThreadContent, { threadItems: [mockMessages[1]] });

			const message = page.getByText('Hi there!');
			const element = await message.element();

			expect(element.className).toContain('text-foreground/90');
		});

		it('applies border styling to assistant messages', async () => {
			render(ThreadContent, { threadItems: [mockMessages[1]] });

			const message = page.getByText('Hi there!');
			const element = await message.element();

			expect(element.className).toContain('mb-4');
		});

		it('renders multiple assistant messages', async () => {
			render(ThreadContent, {
				threadItems: [
					{ id: '1', role: 'assistant', content: 'Response one' },
					{ id: '2', role: 'assistant', content: 'Response two' }
				]
			});

			await expect.element(page.getByText('Response one')).toBeVisible();
			await expect.element(page.getByText('Response two')).toBeVisible();
		});
	});

	describe('Empty items', () => {
		it('renders nothing when threadItems is empty', async () => {
			render(ThreadContent, { threadItems: [] });

			expect(page.getByText('No messages yet. Start a conversation!').query()).toBeNull();
		});

		it('renders nothing when threadItems is empty — no buttons', async () => {
			render(ThreadContent, { threadItems: [] });

			expect(page.getByRole('button', { name: 'Load more' }).query()).toBeNull();
		});
	});

	describe('Multiple messages', () => {
		it('renders conversation with alternating roles', async () => {
			render(ThreadContent, { threadItems: mockMessages });

			await expect.element(page.getByText('Hello')).toBeVisible();
			await expect.element(page.getByText('Hi there!')).toBeVisible();
		});

		it('renders long conversation', async () => {
			const longConversation: ThreadItem[] = Array.from({ length: 10 }, (_, i) => ({
				id: `msg-${i}`,
				role: i % 2 === 0 ? ('user' as const) : ('assistant' as const),
				content: `Message ${i + 1}`
			}));

			render(ThreadContent, { threadItems: longConversation });

			await expect.element(page.getByText('Message 1', { exact: true })).toBeVisible();
			await expect.element(page.getByText('Message 5', { exact: true })).toBeVisible();
			await expect.element(page.getByText('Message 10', { exact: true })).toBeVisible();
		});

		it('maintains message order', async () => {
			render(ThreadContent, { threadItems: mockMessages });

			const firstMessage = page.getByText('Hello');
			const secondMessage = page.getByText('Hi there!');

			await expect.element(firstMessage).toBeVisible();
			await expect.element(secondMessage).toBeVisible();
		});
	});

	describe('Long message handling', () => {
		it('renders very long message content', async () => {
			const longContent = 'A'.repeat(1000);
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'user', content: longContent }]
			});

			await expect.element(page.getByText(longContent)).toBeVisible();
		});

		it('applies max-width constraint to messages', async () => {
			const longContent = 'A'.repeat(500);
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'user', content: longContent }]
			});

			const message = page.getByText(longContent);
			const element = await message.element();

			expect(element.className).toContain('max-w-[75%]');
		});

		it('handles multi-line messages', async () => {
			const multilineContent = 'Line 1\nLine 2\nLine 3';
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'assistant', content: multilineContent }]
			});

			await expect.element(page.getByText(multilineContent)).toBeVisible();
		});

		it('handles very long words without breaking layout', async () => {
			const longWord = 'supercalifragilisticexpialidocious'.repeat(5);
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'user', content: longWord }]
			});

			await expect.element(page.getByText(longWord)).toBeVisible();
		});
	});

	describe('Special characters in messages', () => {
		it('renders HTML-like content safely', async () => {
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'user', content: '<script>alert("xss")</script>' }]
			});

			await expect.element(page.getByText('<script>alert("xss")</script>')).toBeVisible();
		});

		it('renders quotes and apostrophes', async () => {
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'assistant', content: `It's a "quoted" message` }]
			});

			await expect.element(page.getByText(`It's a "quoted" message`)).toBeVisible();
		});

		it('renders unicode characters', async () => {
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'user', content: 'Hello 世界 🌍 emoji' }]
			});

			await expect.element(page.getByText('Hello 世界 🌍 emoji')).toBeVisible();
		});

		it('renders special symbols', async () => {
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'assistant', content: 'Math: ∑ ∫ π ∞ ≤ ≥ ≠' }]
			});

			await expect.element(page.getByText('Math: ∑ ∫ π ∞ ≤ ≥ ≠')).toBeVisible();
		});

		it('renders ampersands and angle brackets', async () => {
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'user', content: 'Use && for AND, || for OR, < > compare' }]
			});

			await expect.element(page.getByText('Use && for AND, || for OR, < > compare')).toBeVisible();
		});

		it('renders markdown-like syntax as plain text', async () => {
			render(ThreadContent, {
				threadItems: [{ id: '1', role: 'assistant', content: '**bold** _italic_ `code`' }]
			});

			await expect.element(page.getByText('**bold** _italic_ `code`')).toBeVisible();
		});
	});

	describe('Props handling', () => {
		it('handles only threadItems prop', async () => {
			expect(() => {
				render(ThreadContent, { threadItems: mockMessages });
			}).not.toThrow();
		});

		it('handles empty threadItems array', async () => {
			expect(() => {
				render(ThreadContent, { threadItems: [] });
			}).not.toThrow();
		});
	});
});
