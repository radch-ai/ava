<script lang="ts">
	import { ChevronDown, ChevronRight, Wrench } from '@lucide/svelte'

	// One tool invocation, live or persisted: a dimmed single line
	// (name + arg snippet, pulse while running) that expands in place
	// to the full args and result.
	interface Props {
		name: string
		args?: string
		result?: string
		error?: string | null
		pending?: boolean
	}

	let {
		name,
		args = '',
		result = undefined,
		error = null,
		pending = false
	}: Props = $props()

	let expanded = $state(false)

	const snippet = $derived(args.length > 64 ? `${args.slice(0, 64)}…` : args)
</script>

<div class="mr-auto w-full text-sm">
	<button
		type="button"
		class="group text-muted-foreground hover:bg-overlay-dark/50 hover:text-foreground flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors"
		aria-expanded={expanded}
		onclick={() => (expanded = !expanded)}
	>
		<span class="relative flex size-4 shrink-0 items-center justify-center">
			<Wrench class="size-3.5 group-hover:hidden" />
			<ChevronRight class="hidden size-4 group-hover:block" />
			{#if expanded}
				<ChevronDown class="absolute size-4" />
			{/if}
		</span>
		<span class="truncate font-mono text-xs">
			{name}
			{#if snippet}
				<span class="text-muted-foreground/70"> {snippet}</span>
			{/if}
		</span>
		{#if pending}
			<span
				class="bg-foreground/50 size-1.5 shrink-0 animate-pulse rounded-full"
			></span>
		{/if}
	</button>

	{#if expanded}
		<div class="space-y-2 pl-8">
			{#if args}
				<div>
					<p
						class="text-muted-foreground mb-1 text-[10px] tracking-wide uppercase"
					>
						Args
					</p>
					<pre
						class="bg-overlay-dark/70 max-h-40 overflow-y-auto rounded-md p-2 font-mono text-xs whitespace-pre-wrap"
					>{args}</pre>
				</div>
			{/if}
			{#if result !== undefined || error}
				<div>
					<p
						class="text-muted-foreground mb-1 text-[10px] tracking-wide uppercase"
					>
						Result
					</p>
					<pre
						class="max-h-48 overflow-y-auto rounded-md p-2 font-mono text-xs whitespace-pre-wrap {error
							? 'bg-destructive/10 text-destructive'
							: 'bg-overlay-dark/70'}"
					>{#if result}{result}{/if}{#if error}{error}{/if}</pre>
				</div>
			{/if}
		</div>
	{/if}
</div>
