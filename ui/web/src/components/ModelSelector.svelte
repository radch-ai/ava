<script lang="ts">
	import * as Select from '#components/ui/select'
	import { defaultModel, type ModelGroup, modelsStore } from '#lib/models'
	import { cn } from '#lib/utils'

	let {
		class: className,
		value = $bindable(defaultModel),
		modelGroups: providedGroups,
		showDefault = true
	}: {
		class?: string
		value?: string
		modelGroups?: ModelGroup[]
		showDefault?: boolean
	} = $props()

	// Catalog from the store unless the caller supplies groups; the
	// store loads once per session, shared by every selector.
	const groups = $derived(providedGroups ?? modelsStore.groups)
	$effect(() => {
		void modelsStore.ensureLoaded()
	})

	// Flatten all models for finding the selected one
	const allModels = $derived(groups.flatMap((g) => g.models))

	// Find the longest model id for minimum width (include
	// defaultModel in calculation)
	const minWidth = $derived.by(() => {
		const longestId = allModels.reduce((max, m) => Math.max(m.length, max), 0)
		// Account for defaultModel display
		const maxLen = showDefault
			? Math.max(longestId, defaultModel.length)
			: longestId
		return `${maxLen + 2}ch`
	})

	let modelName = $derived.by(() => {
		if (value === defaultModel) return defaultModel
		return allModels.includes(value) ? value : 'Select model'
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
		{#each groups as group (group.provider)}
			<Select.Group>
				<Select.GroupHeading>{group.provider}</Select.GroupHeading>
				{#each group.models as model (model)}
					<Select.Item value={model} label={model}>
						{model}
					</Select.Item>
				{/each}
			</Select.Group>
		{/each}
		{#if modelsStore.error}
			<div class="text-destructive px-2 py-1.5 text-xs">
				{modelsStore.error}
			</div>
		{/if}
	</Select.Content>
</Select.Root>
