<script lang="ts">
	import { Command as CommandPrimitive } from 'bits-ui';
	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		value = $bindable(''),
		class: className,
		...restProps
	}: CommandPrimitive.RootProps = $props();
</script>

<CommandPrimitive.Root
	bind:ref
	bind:value
	data-slot="command"
	class={cn(
		'bg-popover text-popover-foreground flex size-full flex-col overflow-hidden rounded-xl! p-1',
		className
	)}
	{...restProps}
/>
