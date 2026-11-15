<script lang="ts">
	import { BrainCircuit } from '@lucide/svelte'
	import { renderMarkdown } from '#lib/services/agent/markdown'
	import ToolLine from './ToolLine.svelte'

	interface ThreadItem {
		id: string
		role: 'user' | 'assistant' | 'tool' | 'error'
		content: string
		pending?: boolean
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
				class="bg-overlay-dark/70 text-foreground/90 hover:bg-overlay-dark/90 hover:text-foreground ml-auto w-fit max-w-[75%] rounded-2xl rounded-br-[4px] px-5 py-3 text-[15px] leading-relaxed shadow-sm"
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
		{:else if message.role === 'assistant' && !message.content}
			<!-- The lane's reasoning signal stands in for an answer row
		that does not exist yet; it renders only while the signal is
		up. -->
			<div
				class="text-muted-foreground mr-auto mb-4 flex w-full items-center gap-2 py-1.5"
			>
				<BrainCircuit class="size-3.5 shrink-0" />
				<span class="text-xs">Thinking</span>
				<span class="flex gap-0.5">
					{#each [0, 150, 300] as delay (delay)}
						<span
							class="bg-foreground/50 size-1 animate-pulse rounded-full"
							style="animation-delay: {delay}ms"
						></span>
					{/each}
				</span>
			</div>
		{:else}
			<div class="text-foreground mb-4 w-full text-[15px] leading-relaxed">
				<!-- biome-ignore lint/nursery/noSvelteAtHtmlTags: model output is sanitized with dompurify inside renderMarkdown -->
				{@html renderMarkdown(message.content)}
			</div>
		{/if}
	{/each}
{/if}
