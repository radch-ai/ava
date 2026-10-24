<script lang="ts">
	import { Button } from '$components/ui/button'
	import { ICON_CATEGORIES, Icon, type IconName } from '$lib/icons'

	interface Props {
		value: IconName
		onselect: (icon: IconName) => void
		id?: string
	}

	let { value, onselect, id }: Props = $props()
	let isOpen = $state(false)
	let dropdownElement: HTMLDivElement | null = $state(null)
	let triggerElement: HTMLButtonElement | null = $state(null)

	function toggle() {
		isOpen = !isOpen
	}

	function handleSelect(icon: IconName) {
		onselect(icon)
		isOpen = false
	}

	// Position the dropdown when it opens, ensuring it stays in viewport
	$effect(() => {
		if (isOpen && dropdownElement && triggerElement) {
			const rect = triggerElement.getBoundingClientRect()
			const dropdownWidth = 320 // w-80 = 320px
			const padding = 8

			// Calculate horizontal position - prefer left-aligned with trigger, but shift if needed
			let left = rect.left
			if (left + dropdownWidth > window.innerWidth - padding) {
				left = Math.max(padding, window.innerWidth - dropdownWidth - padding)
			}

			dropdownElement.style.top = `${rect.bottom + 8}px`
			dropdownElement.style.left = `${left}px`
		}
	})
</script>

<div class="relative">
	<Button
		bind:ref={triggerElement}
		{id}
		variant="outline"
		size="icon"
		aria-label="Select icon"
		onclick={toggle}
	>
		<Icon name={value} size={20} />
	</Button>

	{#if isOpen}
		<!-- Backdrop to close dropdown when clicking outside -->
		<button
			type="button"
			class="fixed inset-0 z-40 bg-transparent"
			aria-label="Close"
			onclick={toggle}
		></button>

		<!-- Dropdown panel - fixed positioning to avoid overflow clipping -->
		<div
			bind:this={dropdownElement}
			class="bg-popover fixed z-50 max-h-80 w-80 overflow-y-auto rounded-lg border p-3 shadow-lg"
		>
			{#each Object.entries(ICON_CATEGORIES) as [category, icons] (category)}
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
								<Icon name={icon} size={20} />
							</button>
						{/each}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
