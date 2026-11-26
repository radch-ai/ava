<script lang="ts">
	import { ChevronDown, ChevronRight, ScrollText } from '@lucide/svelte'
	import * as Collapsible from '#components/ui/collapsible'

	// A compaction successor's seeded summary row: a tight single
	// line — icon, label — that expands in place to the summary's
	// text, the same shape a tool outcome renders in.
	interface Props {
		content: string
	}

	let { content }: Props = $props()

	let expanded = $state(false)
</script>

<Collapsible.Root
	bind:open={expanded}
	class="group hover:bg-overlay-dark/50 data-[state=open]:bg-overlay-dark/50 -mt-3 w-full rounded-md px-1.25 py-1 text-sm"
>
	<Collapsible.Trigger
		class="text-muted-foreground group-hover:text-foreground group-data-[state=open]:text-foreground flex w-full cursor-pointer items-center gap-1.5 py-0.5 text-left transition-colors"
	>
		<span class="flex size-4 shrink-0 items-center justify-center">
			<ScrollText
				class="size-3.5 {expanded ? 'hidden' : 'group-hover:hidden'}"
			/>
			<ChevronRight
				class="hidden size-4 {expanded ? '' : 'group-hover:block'}"
			/>
			<ChevronDown class="size-4 {expanded ? '' : 'hidden'}" />
		</span>
		<span class="truncate font-mono text-xs">Compacted summary</span>
	</Collapsible.Trigger>
	<Collapsible.Content class="border-t pt-1">
		<pre
			class="text-muted-foreground max-h-48 overflow-y-auto px-0.75 py-1 font-mono text-xs whitespace-pre-wrap"
		>{content}</pre>
	</Collapsible.Content>
</Collapsible.Root>
