<script lang="ts">
	import * as Select from '#components/ui/select'
	import { defaultModel, type ModelGroup, modelGroups } from '#lib/models'
	import { cn } from '#lib/utils'

	let {
		class: className,
		value = $bindable(defaultModel),
		modelGroups: providedGroups = modelGroups,
		showDefault = true
	}: {
		class?: string
		value?: string
		modelGroups?: ModelGroup[]
		showDefault?: boolean
	} = $props()

	// Flatten all models for finding the selected one
	const allModels = $derived(providedGroups.flatMap((g) => g.models))

	// Find the longest model name for minimum width (include defaultModel in calculation)
	const minWidth = $derived.by(() => {
		const longestName = allModels.reduce(
			(max, m) => Math.max(m.name.length, max),
			0
		)
		// Account for defaultModel display
		const maxLen = showDefault
			? Math.max(longestName, defaultModel.length)
			: longestName
		return `${maxLen + 2}ch`
	})

	let modelName = $derived.by(() => {
		if (value === defaultModel) return defaultModel
		return allModels.find((m) => m.id === value)?.name ?? 'Select model'
	})
</script>

<Select.Root type="single" bind:value>
	<Select.Trigger
		class={cn(
			'text-muted-foreground h-9 gap-1 border-0 px-2 text-sm',
			className
		)}
		style="min-width: {minWidth}"
	>
		{modelName}
	</Select.Trigger>
	<Select.Content class="opacity-80">
		{#if showDefault}
			<Select.Item value={defaultModel} label={defaultModel}
				>{defaultModel}</Select.Item
			>
		{/if}
		{#each providedGroups as group (group.provider)}
			<Select.Group>
				<Select.GroupHeading>{group.provider}</Select.GroupHeading>
				{#each group.models as model (model.id)}
					<Select.Item value={model.id} label={model.name}>
						{model.name}
					</Select.Item>
				{/each}
			</Select.Group>
		{/each}
	</Select.Content>
</Select.Root>
