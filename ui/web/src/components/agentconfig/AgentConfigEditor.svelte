<script lang="ts">
	import { SvelteSet } from 'svelte/reactivity'
	import { Button } from '#components/ui/button'

	import {
		AGENT_FIELDS,
		type AgentConfiguration,
		type ConfigFieldName,
		cloneConfiguration,
		FIELD_CONFIGS,
		getConfigurationField,
		type HeartbeatConfig,
		type SuperagentConfiguration
	} from '#lib/types/agent'
	import type { Tool } from '#lib/types/tool'
	import ConfigTextField from './ConfigTextField.svelte'
	import CronJobEditor from './CronJobEditor.svelte'
	import HeartbeatEditor from './HeartbeatEditor.svelte'
	import NameIconEditor from './NameIconEditor.svelte'
	import SoulEditor from './SoulEditor.svelte'
	import ToolsEditor from './ToolsEditor.svelte'
	import UnsavedChangesDialog from './UnsavedChangesDialog.svelte'

	/**
	 * Compare soul configs
	 */
	function soulEquals(
		a: AgentConfiguration['soul'],
		b: AgentConfiguration['soul']
	): boolean {
		return JSON.stringify(a) === JSON.stringify(b)
	}

	/**
	 * Compare name field (ignoring icon)
	 */
	function nameEquals(a: AgentConfiguration, b: AgentConfiguration): boolean {
		return a.name === b.name
	}

	interface Props {
		configuration: AgentConfiguration
		/** All available tools */
		availableTools?: Tool[]
		/** Called when save is clicked with the configuration */
		onSave: (config: AgentConfiguration) => void | Promise<void>
		/** Called when cancel is confirmed (no changes or user discarded) */
		onCancel: () => void
		/** Label for the save button */
		saveLabel?: string
		/** Label for the cancel button */
		cancelLabel?: string
		/** Helper text shown above buttons */
		helperText?: string
		/** Bindable flag for tracking changes */
		hasChanges?: boolean
		/** Explicitly set agent type (overrides route detection) */
		isSuperagent?: boolean
	}

	let {
		configuration,
		availableTools = [],
		onSave,
		onCancel,
		saveLabel = 'Save',
		cancelLabel = 'Cancel',
		helperText = 'Please review and edit the options above, then click Save to apply your changes.',
		// bindable prop synced to parent via $effect
		hasChanges = $bindable(),
		isSuperagent: isSuperagentProp
	}: Props = $props()

	// Determine agent type from the isSuperagent prop
	const isSuperagent = $derived(isSuperagentProp ?? false)
	const fields = $derived(
		isSuperagent ? AGENT_FIELDS.superagent : AGENT_FIELDS.worker
	)

	// Local working copy of configuration
	let localConfiguration = $state<AgentConfiguration>()

	// Initialize local configuration when component mounts or configuration changes
	$effect(() => {
		if (!localConfiguration && configuration) {
			localConfiguration = cloneConfiguration(configuration)
		}
	})

	// Detect if there are unsaved changes by comparing to prop (internal derived)
	const hasLocalChanges = $derived.by(() => {
		if (!localConfiguration || !configuration) return false

		// Compare name (without icon - icon changes don't count)
		if (!nameEquals(localConfiguration, configuration)) return true

		// Compare soul
		if (!soulEquals(localConfiguration.soul, configuration.soul)) return true

		// Compare other fields
		for (const field of fields) {
			if (field === 'name' || field === 'soul') continue // Already compared above
			const localValue = getConfigurationField(localConfiguration, field)
			const originalValue = getConfigurationField(configuration, field)
			if (JSON.stringify(localValue) !== JSON.stringify(originalValue)) {
				return true
			}
		}
		return false
	})

	// Sync to bindable prop
	$effect(() => {
		hasChanges = hasLocalChanges
	})

	// Track which specific fields have unsaved changes
	let dirtyFields = $derived.by(() => {
		const dirty = new SvelteSet<ConfigFieldName>()
		if (!localConfiguration || !configuration) return dirty

		for (const field of fields) {
			if (field === 'name') {
				// Compare name without icon
				if (!nameEquals(localConfiguration, configuration)) {
					dirty.add(field)
				}
			} else if (field === 'soul') {
				if (!soulEquals(localConfiguration.soul, configuration.soul)) {
					dirty.add(field)
				}
			} else {
				const localValue = getConfigurationField(localConfiguration, field)
				const originalValue = getConfigurationField(configuration, field)
				if (JSON.stringify(localValue) !== JSON.stringify(originalValue)) {
					dirty.add(field)
				}
			}
		}
		return dirty
	})

	// Currently selected field
	let selectedField = $state<ConfigFieldName | undefined>()

	// Confirmation dialog state
	let showConfirmDialog = $state(false)

	// Initialize selected field when fields are available
	$effect(() => {
		if (!selectedField && fields.length > 0) {
			selectedField = fields[0]
		}
	})

	async function handleSave() {
		if (localConfiguration) {
			await onSave(localConfiguration)
		}
	}

	function handleCancel() {
		if (hasLocalChanges) {
			showConfirmDialog = true
		} else {
			onCancel()
		}
	}

	function handleKeepEditing() {
		showConfirmDialog = false
	}

	function handleDiscardChanges() {
		showConfirmDialog = false
		onCancel()
	}
</script>

<div class="flex h-full flex-col">
	<!-- Top section: Field list + Content area (flex-1 to take remaining space) -->
	<div class="flex flex-1 overflow-hidden border-t">
		<!-- Left panel: Field list -->
		<div class="w-48 border-r py-2">
			{#each fields as field (field)}
				<button
					type="button"
					class="w-full px-4 py-2.5 text-left text-sm transition-colors {selectedField ===
					field
						? 'active-field'
						: 'hover-field'}"
					onclick={() => (selectedField = field)}
				>
					<span class="flex items-center gap-1">
						{FIELD_CONFIGS[field]?.label ?? field}
						{#if dirtyFields.has(field)}
							<span class="text-primary">*</span>
						{/if}
					</span>
				</button>
			{/each}
		</div>

		<!-- Right panel: Content area -->
		<div class="bg-background flex-1 overflow-y-auto p-4">
			{#if localConfiguration}
				{#if selectedField === 'name'}
					<NameIconEditor bind:config={localConfiguration} />
				{:else if selectedField === 'soul'}
					<SoulEditor bind:soul={localConfiguration.soul} />
				{:else if selectedField === 'heartbeat'}
					<HeartbeatEditor
						bind:heartbeat={localConfiguration.heartbeat as HeartbeatConfig}
					/>
				{:else if selectedField === 'cronJobs' && isSuperagent}
					{@const superagentConfig =
						localConfiguration as SuperagentConfiguration}
					<CronJobEditor bind:jobs={superagentConfig.cronJobs} />
				{:else if selectedField === 'userContext' && isSuperagent}
					{@const superagentConfig =
						localConfiguration as SuperagentConfiguration}
					<ConfigTextField
						value={superagentConfig.userContext}
						config={FIELD_CONFIGS.userContext}
						onchange={(v) => (superagentConfig.userContext = v)}
					/>
				{:else if selectedField === 'toolsEnabled'}
					<ToolsEditor
						tools={availableTools}
						bind:enabledToolIds={localConfiguration.toolsEnabled}
					/>
				{:else if selectedField === 'operatingInstructions'}
					<ConfigTextField
						value={localConfiguration.operatingInstructions}
						config={FIELD_CONFIGS.operatingInstructions}
						onchange={(v) => {
							if (localConfiguration)
								localConfiguration.operatingInstructions = v
						}}
					/>
				{:else if selectedField === 'toolsContext'}
					<ConfigTextField
						value={localConfiguration.toolsContext}
						config={FIELD_CONFIGS.toolsContext}
						onchange={(v) => {
							if (localConfiguration) localConfiguration.toolsContext = v
						}}
					/>
				{/if}
			{/if}
		</div>
	</div>
	<!-- Action buttons at bottom -->
	<div
		class="border-border/50 mr-1 mb-2 flex flex-col items-end gap-6 border-t"
	>
		<p class="text-muted-foreground text-xs">
			{helperText}
		</p>
		<div class="flex gap-4">
			<Button size="lg" class="sm:min-w-36" onclick={handleSave}
				>{saveLabel}</Button
			>
			<Button
				variant="outline"
				size="lg"
				class="sm:min-w-36"
				onclick={handleCancel}
			>
				{cancelLabel}
			</Button>
		</div>
	</div>
</div>

<!-- Confirmation dialog for unsaved changes -->
<UnsavedChangesDialog
	bind:open={showConfirmDialog}
	onDiscard={handleDiscardChanges}
	onKeepEditing={handleKeepEditing}
/>

<style>
	.active-field {
		background-color: var(--sidebar-active);
		color: var(--sidebar-active-foreground);
	}

	.hover-field:hover {
		background-color: var(--sidebar-accent);
		color: var(--sidebar-accent-foreground);
	}
</style>
