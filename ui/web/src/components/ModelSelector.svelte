<script lang="ts">
	import { RefreshCw } from '@lucide/svelte'
	import * as Select from '#components/ui/select'
	import {
		type ModelEntry,
		type ModelGroup,
		type ModelSelection,
		modelsStore
	} from '#lib/models'
	import { cn } from '#lib/utils'

	let {
		class: className,
		value = $bindable(null),
		modelGroups: providedGroups,
		showDefault = true,
		onChange
	}: {
		class?: string
		value?: ModelSelection
		modelGroups?: ModelGroup[]
		showDefault?: boolean
		/** For callers that derive the value from two fields (e.g.
		 * heartbeat config) instead of binding a writable. */
		onChange?: (selection: ModelSelection) => void
	} = $props()

	// Catalog from the store unless the caller supplies groups; the
	// store loads once per session, shared by every selector.
	const groups = $derived(providedGroups ?? modelsStore.groups)
	$effect(() => {
		void modelsStore.ensureLoaded()
	})

	// The widget binds string keys; entries serialize to them and
	// back (the same id can appear under two provider rows).
	const NULL_KEY = 'null' // JSON.stringify(null)
	const keyOf = (entry: ModelEntry) => JSON.stringify(entry)
	const selectValue = $derived(value === null ? NULL_KEY : keyOf(value))

	const allEntries = $derived(groups.flatMap((g) => g.models))

	// Longest label for minimum width (include the default option)
	const minWidth = $derived.by(() => {
		let longest = allEntries.reduce((max, m) => Math.max(m.id.length, max), 0)
		if (showDefault) longest = Math.max(longest, 'Default'.length)
		return `${longest + 2}ch`
	})

	function setFromKey(key: string) {
		let next: ModelSelection = null
		if (key !== NULL_KEY) {
			try {
				const parsed = JSON.parse(key) as ModelEntry
				if (
					parsed &&
					typeof parsed.id === 'string' &&
					typeof parsed.providerId === 'string'
				) {
					next = { id: parsed.id, providerId: parsed.providerId }
				}
			} catch {
				// Malformed key: leave the selection untouched.
				return
			}
		}
		value = next
		onChange?.(next)
	}

	let modelName = $derived.by(() => {
		if (value === null) return 'Default'
		const selected: ModelEntry = value
		const known = allEntries.find(
			(m) => m.id === selected.id && m.providerId === selected.providerId
		)
		return known ? known.id : 'Select model'
	})
</script>

<Select.Root type="single" value={selectValue} onValueChange={setFromKey}>
	<Select.Trigger
		class={cn(
			'text-muted-foreground h-9 gap-1 border-0 px-2 text-sm',
			className
		)}
		style="min-width: {minWidth}"
	>
		{modelName}
	</Select.Trigger>
	<Select.Content>
		{#if !providedGroups}
			<!-- Catalog header: refresh the shared store without closing
				the dropdown. -->
			<div class="flex justify-end border-b p-1">
				<button
					type="button"
					aria-label="Refresh model list"
					title="Refresh model list"
					class="text-muted-foreground hover:text-foreground hover:bg-accent rounded-sm p-0.5"
					onclick={(e) => {
						e.stopPropagation()
						void modelsStore.refresh()
					}}
				>
					<RefreshCw
						class={cn('size-3', modelsStore.refreshing && 'animate-spin')}
					/>
				</button>
			</div>
		{/if}
		{#if showDefault}
			<Select.Item value={NULL_KEY} label="Default">Default</Select.Item>
		{/if}
		{#each groups as group (group.providerName)}
			<Select.Group>
				<Select.GroupHeading>{group.providerName}</Select.GroupHeading>
				{#each group.models as model (keyOf(model))}
					<Select.Item value={keyOf(model)} label={model.id}>
						{model.id}
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
