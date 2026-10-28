<script lang="ts">
	import { Bot } from '@lucide/svelte'
	import type { AgentIconName } from '#components/agent-icons/index.ts'
	import { AgentConfigEditor } from '#components/agentconfig'
	import UnsavedChangesDialog from '#components/agentconfig/UnsavedChangesDialog.svelte'
	import { createAgent } from '#lib/services/agent'
	import {
		createDefaultWorkerConfiguration,
		type WorkerConfiguration
	} from '#lib/types/agent'
	import { addAgent } from '#lib/user'
	import { type BeforeNavigate, beforeNavigate, goto } from '$app/navigation'
	import { resolve } from '$app/paths'
	import type { PathnameWithSearchOrHash } from '$app/types'
	import { isValidAgentName } from './agent-name'

	let configuration = $state<WorkerConfiguration>(
		createDefaultWorkerConfiguration()
	)
	let hasChanges = $state(false)

	// ============================================================
	// UNSAVED CHANGES NAVIGATION PROTECTION
	// ============================================================
	// This page implements three levels of navigation protection:
	//
	// 1. CANCEL BUTTON: AgentConfigEditor handles this internally,
	//    showing UnsavedChangesDialog when there are changes.
	//
	// 2. INTERNAL NAVIGATION (clicking links, back/forward):
	//    We intercept with beforeNavigate and show our dialog.
	//
	// 3. BROWSER REFRESH/CLOSE: Uses browser's native dialog via
	//    beforeunload event (not our custom dialog).

	// Flag to bypass protection when navigation is intentional
	let isNavigating = $state(false)

	// State for navigation confirmation dialog
	let showNavDialog = $state(false)
	let pendingNavigation: BeforeNavigate | null = $state(null)

	// Browser-level protection: Shows native browser dialog on refresh/close
	function handleBeforeUnload(e: BeforeUnloadEvent) {
		if (hasChanges && !isNavigating) {
			e.preventDefault()
		}
	}

	// SvelteKit-level protection: Intercepts internal app navigation
	// 'leave' type = browser refresh/close, handled by beforeunload above
	beforeNavigate((navigation) => {
		if (hasChanges && !isNavigating && navigation.type !== 'leave') {
			navigation.cancel()
			pendingNavigation = navigation
			showNavDialog = true
		}
	})

	function handleNavKeepEditing() {
		showNavDialog = false
		pendingNavigation = null
	}

	function handleNavDiscard() {
		showNavDialog = false
		if (pendingNavigation?.to?.url) {
			isNavigating = true
			const url = pendingNavigation.to.url
			goto(resolve((url.pathname + url.search) as PathnameWithSearchOrHash))
		}
		pendingNavigation = null
	}

	// ============================================================
	// PAGE ACTIONS
	// ============================================================

	function handleCancel() {
		isNavigating = true
		history.back()
	}

	let createError = $state<string | null>(null)

	async function handleSave(config: WorkerConfiguration) {
		const name = config.name.trim()
		if (!name) return

		if (!isValidAgentName(name)) {
			createError =
				'Invalid agent name: use letters, digits and single spaces only'
			return
		}
		createError = null

		try {
			// The server derives the id from the display name and rejects
			// invalid or duplicate names; the client never invents an id.
			const createdAgent = await createAgent({ ...config, name })
			addAgent({
				id: createdAgent.id,
				name: createdAgent.name,
				icon: (config.icon ?? 'bot') as AgentIconName
			})
			isNavigating = true
			await goto(
				resolve('/(app)/agent/[agentId]', { agentId: createdAgent.id })
			)
		} catch (error) {
			createError =
				error instanceof Error ? error.message : 'Failed to create agent'
		}
	}
</script>

<svelte:window on:beforeunload={handleBeforeUnload} />

<div class="mx-auto flex h-dvh min-h-[28rem] max-w-3xl flex-col px-4 py-8">
	<header class="mb-6 flex items-center gap-3">
		<Bot class="size-6" />
		<h1 class="text-2xl font-bold">New Agent</h1>
	</header>

	<!-- Configuration Editor (full screen) -->
	<div class="flex-1 overflow-auto rounded-lg border">
		{#if createError}
			<p
				role="alert"
				class="bg-destructive/10 text-destructive rounded-lg p-3 text-center text-sm"
			>
				{createError}
			</p>
		{/if}
		<AgentConfigEditor
			{configuration}
			onSave={handleSave}
			onCancel={handleCancel}
			saveLabel="Create Agent"
			cancelLabel="Cancel"
			helperText="Start by setting the agent name, then configure other options."
			isSuperagent={false}
			bind:hasChanges
		/>
	</div>
</div>

<!-- Navigation confirmation dialog (for internal app navigation) -->
<UnsavedChangesDialog
	bind:open={showNavDialog}
	onDiscard={handleNavDiscard}
	onKeepEditing={handleNavKeepEditing}
/>
