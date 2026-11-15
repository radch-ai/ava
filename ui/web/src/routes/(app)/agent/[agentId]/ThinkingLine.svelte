<script lang="ts">
	import { BrainCircuit, ChevronDown, ChevronRight } from '@lucide/svelte'

	// The model's reasoning tokens for one turn: a dimmed disclosure
	// above the answer. Open with a pulse while only reasoning
	// streams, auto-collapse when the answer starts, and stay
	// user-togglable afterwards. Live-only — settled turns drop it.
	interface Props {
		reasoning: string
		/** True while the turn still streams reasoning only. */
		thinking: boolean
	}

	let { reasoning, thinking }: Props = $props()

	let userToggled = $state<boolean | null>(null)
	const expanded = $derived(userToggled ?? thinking)
</script>

<div class="mr-auto mb-1 w-full text-sm">
	<button
		type="button"
		class="group text-muted-foreground hover:bg-overlay-dark/50 hover:text-foreground flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors"
		aria-expanded={expanded}
		onclick={() => (userToggled = !expanded)}
	>
		<span class="relative flex size-4 shrink-0 items-center justify-center">
			<BrainCircuit class="size-3.5 group-hover:hidden" />
			<ChevronRight class="hidden size-4 group-hover:block" />
			{#if expanded}
				<ChevronDown class="absolute size-4" />
			{/if}
		</span>
		<span class="text-xs">Thinking</span>
		{#if thinking}
			<span
				class="bg-foreground/50 size-1.5 shrink-0 animate-pulse rounded-full"
			></span>
		{/if}
	</button>

	{#if expanded}
		<pre
			class="text-muted-foreground bg-overlay-dark/40 max-h-48 overflow-y-auto rounded-md px-2 py-2 font-mono text-xs whitespace-pre-wrap"
		>{reasoning}</pre>
	{/if}
</div>
