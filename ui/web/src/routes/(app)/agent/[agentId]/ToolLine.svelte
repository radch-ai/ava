<script lang="ts">
	import {
		ChevronDown,
		ChevronRight,
		CircleAlert,
		Wrench
	} from '@lucide/svelte'
	import * as Collapsible from '#components/ui/collapsible'

	// One tool invocation, live or persisted: a tight single line —
	// icon, name, arg snippet, pulse while running — that expands in
	// place to one result box.
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

<Collapsible.Root
	bind:open={expanded}
	class="group hover:bg-overlay-dark/50 data-[state=open]:bg-overlay-dark/50 -mt-3 w-full rounded-md px-1.25 py-1 text-sm"
>
	<Collapsible.Trigger
		class="text-muted-foreground group-hover:text-foreground group-data-[state=open]:text-foreground flex w-full cursor-pointer items-center gap-1.5 py-0.5 text-left transition-colors"
	>
		<span class="flex size-4 shrink-0 items-center justify-center">
			{#if error}
				<CircleAlert
					class="text-destructive size-3.5 {expanded
						? 'hidden'
						: 'group-hover:hidden'}"
				/>
			{:else}
				<Wrench class="size-3.5 {expanded ? 'hidden' : 'group-hover:hidden'}" />
			{/if}
			<ChevronRight
				class="hidden size-4 {expanded ? '' : 'group-hover:block'}"
			/>
			<ChevronDown class="size-4 {expanded ? '' : 'hidden'}" />
		</span>
		<span class="truncate font-mono text-xs">
			{name}
			{#if snippet}
				<span
					class="text-muted-foreground/70 group-hover:text-foreground/70 group-data-[state=open]:text-foreground/70"
				>
					{snippet}</span
				>
			{/if}
		</span>
		{#if pending}
			<span
				class="bg-foreground/50 size-1.5 shrink-0 animate-pulse rounded-full"
			></span>
		{/if}
	</Collapsible.Trigger>
	<Collapsible.Content class="border-t pt-1">
		{#if result !== undefined || error}
			<pre
				class="max-h-48 overflow-y-auto px-0.75 py-1 font-mono text-xs whitespace-pre-wrap {error
					? 'text-destructive'
					: 'text-muted-foreground'}"
			>{#if result}{result}{/if}{#if error}{error}{/if}</pre>
		{/if}
	</Collapsible.Content>
</Collapsible.Root>
