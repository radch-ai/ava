<script lang="ts">
	import { SendHorizontal } from '@lucide/svelte'
	import { Button } from '#components/ui/button'
	import type { QueueItem } from '#lib/types/gen/wstypes.gen'

	let {
		items,
		next = null,
		paused = false,
		/** The item whose send-immediately result is pending; all
		 * actions disable while one is in flight. */
		pendingId = null,
		onsendnow
	}: {
		items: QueueItem[]
		/** The send-now reservation: pinned above the waiting items,
		 * action-less — its turn comes next regardless. */
		next?: QueueItem | null
		paused?: boolean
		pendingId?: string | null
		onsendnow: (itemId: string) => void
	} = $props()

	const actionsDisabled = $derived(pendingId !== null)
</script>

<!-- The gateway-owned queue, oldest first: waiting prompts and
	the paused label live here — never in the conversation tail.
	The page renders this only with waiting work: the server arms
	the pause solely while items wait, so a paused-but-empty queue
	cannot occur. -->
<ul class="w-full list-none space-y-1.5" aria-label="Pending prompts">
	{#if next}
		<li class="bg-primary/10 flex items-center gap-2 rounded-2xl px-3 py-1.5">
			<span class="text-primary shrink-0 text-xs font-medium">Next</span>
			<span
				class="text-muted-foreground min-w-0 flex-1 truncate text-sm"
				class:font-mono={next.kind === 'new_thread'}
			>
				{next.content}
			</span>
		</li>
	{/if}
	{#each items as item (item.id)}
		<li
			class="bg-overlay-dark/50 flex items-center gap-2 rounded-2xl px-3 py-1.5"
		>
			<span
				class="text-muted-foreground min-w-0 flex-1 truncate text-sm"
				class:font-mono={item.kind === 'new_thread'}
			>
				{item.content}
			</span>
			<Button
				size="sm"
				variant="ghost"
				class="h-7 shrink-0 cursor-pointer px-2 text-xs"
				disabled={actionsDisabled}
				onclick={() => onsendnow(item.id)}
			>
				<SendHorizontal class="size-3.5" />
				Send immediately
			</Button>
		</li>
	{/each}
	{#if paused}
		<p class="text-destructive/80 px-3 text-xs">
			Paused — choose Send immediately to continue.
		</p>
	{/if}
</ul>
