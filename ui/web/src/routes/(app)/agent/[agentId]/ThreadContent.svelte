<script lang="ts">
	import { renderMarkdown } from '#lib/services/agent/markdown'
	import ThinkingLine from './ThinkingLine.svelte'
	import ToolLine from './ToolLine.svelte'

	interface ThreadItem {
		id: string
		role: 'user' | 'assistant' | 'tool' | 'error'
		content: string
		pending?: boolean
		/** Assistant messages only: reasoning tokens as they streamed —
		 * live-only, absent from settled thread rows. */
		reasoning?: string
		/** Tool rows only: the persisted invocation envelope. */
		name?: string
		args?: string
		result?: string
		error?: string | null
	}

	interface Props {
		threadItems: ThreadItem[]
	}

	let { threadItems }: Props = $props()

	// A persisted tool row carries the envelope as content; the
	// fields ToolLine wants come out of it.
	function toolEntry(message: ThreadItem) {
		if (message.name !== undefined) return message
		try {
			const env = JSON.parse(message.content) as {
				name?: string
				args?: unknown
				result?: unknown
				error?: string | null
			}
			return {
				...message,
				name: env.name ?? '',
				args:
					typeof env.args === 'string'
						? env.args
						: (JSON.stringify(env.args) ?? ''),
				result:
					typeof env.result === 'string'
						? env.result
						: JSON.stringify(env.result),
				error: env.error ?? null
			}
		} catch {
			return {
				...message,
				name: message.content,
				args: '',
				result: undefined,
				error: null
			}
		}
	}
</script>

{#if threadItems.length > 0}
	{#each threadItems as message (message.id)}
		{#if message.role === 'user'}
			<div
				class="bg-overlay-dark/70 text-foreground/80 hover:bg-overlay-dark/90 hover:text-foreground ml-auto w-fit max-w-[75%] rounded-2xl rounded-br-[4px] px-5 py-3 text-[15px] leading-relaxed shadow-sm"
			>
				{message.content}
			</div>
		{:else if message.role === 'error'}
			<p
				class="bg-destructive/10 text-destructive mr-auto max-w-[75%] rounded-lg px-4 py-2 text-sm leading-relaxed"
			>
				{message.content}
			</p>
		{:else if message.role === 'tool'}
			{@const tool = toolEntry(message)}
			<ToolLine
				name={tool.name ?? ''}
				args={tool.args}
				result={tool.result}
				error={tool.error ?? null}
				pending={message.pending}
			/>
		{:else}
			<div
				class="text-foreground/90 mr-auto mb-4 w-full text-[15px] leading-relaxed"
			>
				{#if message.reasoning}
					<ThinkingLine
						reasoning={message.reasoning}
						thinking={!!message.pending && !message.content}
					/>
				{/if}
				<!-- biome-ignore lint/nursery/noSvelteAtHtmlTags: model output is sanitized with dompurify inside renderMarkdown -->
				{@html renderMarkdown(message.content)}
				{#if message.pending}
					<span
						class="bg-foreground/50 ml-0.5 inline-block size-[0.5em] animate-pulse rounded-[2px] align-baseline"
					></span>
				{/if}
			</div>
		{/if}
	{/each}
{/if}
