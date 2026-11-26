<script lang="ts">
	import { BrainCircuit, FoldVertical } from '@lucide/svelte'
	import { Separator } from '#components/ui/separator'
	import { threadRowKey } from '#lib/services/agent/thread'
	import type { ThreadItem } from '#lib/types/agent'
	import MarkdownBody from './MarkdownBody.svelte'
	import SummaryLine from './SummaryLine.svelte'
	import ToolLine from './ToolLine.svelte'

	interface Props {
		threadItems: ThreadItem[]
	}

	let { threadItems }: Props = $props()

	/** A tool row with the invocation's name resolved: a live row
	 * carries it already, a persisted one parses it from the
	 * envelope. */
	type ToolRow = ThreadItem & { name: string }

	// A persisted tool row carries the envelope as content; the fields
	// ToolLine wants come out of it. Committed rows are immutable, so
	// one parse per row serves every rebuild.
	const parsedTools = new WeakMap<ThreadItem, ToolRow>()

	function toolEntry(message: ThreadItem): ToolRow {
		if (isNamedToolRow(message)) return message
		const cached = parsedTools.get(message)
		if (cached) return cached
		let entry: ToolRow
		try {
			const env = JSON.parse(message.content) as {
				name?: string
				args?: unknown
				result?: unknown
				error?: string | null
			}
			entry = {
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
			entry = {
				...message,
				name: message.content,
				args: '',
				result: undefined,
				error: null
			}
		}
		parsedTools.set(message, entry)
		return entry
	}

	/** A live tool row carries its invocation name already; persisted
	 * ones recover theirs from the envelope parse. */
	function isNamedToolRow(row: ThreadItem): row is ToolRow {
		return row.name !== undefined
	}
</script>

{#if threadItems.length > 0}
	<!-- A divider's id is its thread id: the row key keeps the two id
	namespaces apart. -->
	{#each threadItems as message (threadRowKey(message))}
		{#if message.role === 'user'}
			<div
				class="bg-overlay-dark/70 text-foreground/90 hover:bg-overlay-dark/90 hover:text-foreground ml-auto w-fit max-w-[75%] rounded-2xl rounded-br-[4px] px-5 py-3 text-[15px] leading-relaxed shadow-sm"
			>
				{message.content}
			</div>
		{:else if message.role === 'divider'}
			<div class="my-6 flex w-full items-center gap-3">
				<Separator class="flex-1" />
				<span class="text-muted-foreground text-xs tracking-wide uppercase">
					{message.content}
				</span>
				<Separator class="flex-1" />
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
		{:else if message.role === 'assistant' && message.isSummary}
			<!-- A compaction successor's seeded summary: collapsed behind
		its boundary like a tool outcome, never a page of prose. -->
			<SummaryLine content={message.content} />
		{:else if message.role === 'compacting'}
			<!-- The lane's compacting signal, the same in-stream indicator
		the thinking pulse renders; it shows only while the signal is
		up. -->
			<div
				class="text-muted-foreground mr-auto mb-4 flex w-full items-center gap-2 py-1.5"
			>
				<FoldVertical class="size-3.5 shrink-0" />
				<span class="text-xs">Compacting</span>
				<span class="flex gap-0.5">
					{#each [0, 150, 300] as delay (delay)}
						<span
							class="bg-foreground/50 size-1 animate-pulse rounded-full"
							style="animation-delay: {delay}ms"
						></span>
					{/each}
				</span>
			</div>
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
			<MarkdownBody content={message.content} />
		{/if}
	{/each}
{/if}
