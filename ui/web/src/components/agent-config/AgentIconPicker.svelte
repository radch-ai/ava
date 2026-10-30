<script lang="ts">
	import AgentIcon from '#components/agent-icons/index.svelte'
	import {
		AGENT_ICON_CATEGORIES,
		type AgentIconName
	} from '#components/agent-icons/index.ts'
	import { buttonVariants } from '#components/ui/button'
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuTrigger
	} from '#components/ui/dropdown-menu'

	interface Props {
		value: AgentIconName
		onselect: (icon: AgentIconName) => void
		id?: string
	}

	let { value, onselect, id }: Props = $props()

	// Close the menu after a selection.
	let isOpen = $state(false)

	function handleSelect(icon: AgentIconName) {
		onselect(icon)
		isOpen = false
	}
</script>

<DropdownMenu bind:open={isOpen}>
	<DropdownMenuTrigger
		class={buttonVariants({ variant: 'outline', size: 'icon' })}
		{id}
		aria-label={`Icon: ${value}. Change icon.`}
	>
		<AgentIcon name={value} size={20} />
	</DropdownMenuTrigger>
	<!-- DropdownMenuContent portals this panel to <body>: the sheet dialog's
	     contain: layout style makes the dialog the containing block for fixed
	     descendants, so the panel must live outside it to stay viewport-anchored. -->
	<DropdownMenuContent class="max-h-60 w-60 rounded-lg p-2">
		{#each Object.entries(
			AGENT_ICON_CATEGORIES
		) as [category, icons] (category)}
			<div class="mb-3">
				<h4
					class="text-muted-foreground mb-1.5 px-1 text-xs font-medium capitalize"
				>
					{category}
				</h4>
				<div class="grid grid-cols-6 gap-1">
					{#each icons as icon (icon)}
						<button
							type="button"
							aria-label={icon}
							title={icon}
							class="flex h-8 w-8 items-center justify-center rounded-md hover:bg-accent {value ===
							icon
								? 'bg-accent/50 ring-2 ring-primary'
								: ''}"
							onclick={() => handleSelect(icon)}
						>
							<AgentIcon name={icon} size={20} />
						</button>
					{/each}
				</div>
			</div>
		{/each}
	</DropdownMenuContent>
</DropdownMenu>
