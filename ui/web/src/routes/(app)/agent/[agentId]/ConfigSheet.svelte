<script lang="ts">
	import { AgentConfigEditor } from '#components/agent-config'
	import * as Sheet from '#components/ui/sheet'
	import {
		SheetCloseGuard,
		type SheetCloseGuardExports,
		UnsavedChangesGuards
	} from '#components/unsaved-changes'
	import { SUPERAGENT_AGENT_ID } from '#lib/config'
	import { updateAgentConfiguration } from '#lib/services/agent'
	import {
		AGENT_FIELDS,
		type AgentConfiguration,
		getConfigurationField,
		setConfigurationField
	} from '#lib/types/agent'
	import type { Tool } from '#lib/types/tool'

	interface Props {
		isSheetOpened: boolean
		closeSheet: () => void
		configuration: AgentConfiguration
		/** All available tools for this user */
		availableTools: Tool[]
		/** Agent id for API calls */
		agentId: string
		/** Called after successful save with the new configuration */
		onSaved: (config: AgentConfiguration) => void
	}

	let {
		isSheetOpened = $bindable(),
		closeSheet,
		configuration,
		availableTools,
		agentId,
		onSaved
	}: Props = $props()

	// Track if there are unsaved changes (bound to editor)
	let hasChanges = $state(false)

	// Sheet-level close protection (escape / click-outside → dialog).
	// Level 4 of unsaved-changes protection — see SheetCloseGuard.
	let closeGuard = $state<SheetCloseGuardExports>()

	// Determine agent type from the reserved superagent id
	const isSuperagent = $derived(agentId === SUPERAGENT_AGENT_ID)

	// Guards see changes only while the sheet is open — hasChanges stays
	// stale true after the editor unmounts on close, so never feed it
	// through ungated.
	let guardDirty = $derived(isSheetOpened && hasChanges)

	// Single handler for sheet open/close from Sheet component
	// Cancel button calls handleCancel directly which closes immediately
	// This handler only intercepts escape/click-outside which are prevented when hasChanges
	function handleOpenChange(newOpen: boolean) {
		if (!newOpen && hasChanges) {
			// Don't close when there are unsaved changes (escape/click-outside)
			return
		}
		isSheetOpened = newOpen
		if (!newOpen) closeSheet()
	}

	async function handleSave(config: AgentConfiguration) {
		// Build partial update with only changed fields
		const fields = isSuperagent ? AGENT_FIELDS.superagent : AGENT_FIELDS.agent
		const changes: Partial<AgentConfiguration> = {}

		for (const field of fields) {
			const localValue = getConfigurationField(config, field)
			const originalValue = getConfigurationField(configuration, field)
			if (JSON.stringify(localValue) !== JSON.stringify(originalValue)) {
				setConfigurationField(changes as AgentConfiguration, field, localValue)
			}
		}

		try {
			const savedConfig = await updateAgentConfiguration(agentId, changes)
			onSaved(savedConfig)
			isSheetOpened = false
		} catch {
			// Keep sheet open on error so user can retry
		}
	}

	function handleCancel() {
		isSheetOpened = false
		closeSheet()
	}

	function handleCloseDiscard() {
		isSheetOpened = false
		closeSheet()
	}
</script>

<Sheet.Root open={isSheetOpened} onOpenChange={handleOpenChange}>
	<Sheet.Content
		class="bg-sidebar w-full max-w-2xl sm:max-w-2xl [&>button]:hidden"
		onEscapeKeydown={closeGuard?.onEscapeKeydown}
		onInteractOutside={closeGuard?.onInteractOutside}
	>
		<Sheet.Header>
			<Sheet.Title>Agent Configuration</Sheet.Title>
		</Sheet.Header>

		<!-- Kept separate so other consumers (e.g. /agent/new)can use the editor
		 			directly with their own save behavior (createAgent vs updateAgentConfiguration) -->
		<AgentConfigEditor
			{configuration}
			{availableTools}
			onSave={handleSave}
			onCancel={handleCancel}
			bind:hasChanges
			{isSuperagent}
		/>
	</Sheet.Content>
</Sheet.Root>

<SheetCloseGuard
	bind:this={closeGuard}
	hasChanges={guardDirty}
	onDiscard={handleCloseDiscard}
/>

<!-- Levels 2 + 3: in-app navigation + browser unload protection while the
     open sheet has unsaved changes. Gated on isSheetOpened so a closed
     sheet never blocks; onDiscard closes it as the navigation resumes. -->
<UnsavedChangesGuards hasChanges={guardDirty} onDiscard={closeSheet} />
