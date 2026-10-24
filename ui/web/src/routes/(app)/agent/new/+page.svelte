<script lang="ts">
	import type { BeforeNavigate } from '@sveltejs/kit'
	import { Bot } from 'lucide-svelte'
	import { beforeNavigate, goto } from '$app/navigation'
	import { resolve } from '$app/paths'
	import { AgentConfigEditor } from '$components/agentconfig'
	import UnsavedChangesDialog from '$components/agentconfig/UnsavedChangesDialog.svelte'
	import type { IconName } from '$lib/icons'
	import { createAgent } from '$lib/services/agent'
	import {
		createDefaultWorkerConfiguration,
		type WorkerConfiguration
	} from '$lib/types/agent'
	import { addAgent } from '$lib/user'

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
			// @ts-expect-error - Dynamic route not typed
			goto(resolve(url.pathname + url.search))
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

	async function handleSave(config: WorkerConfiguration) {
		const name = config.name.trim()
		if (!name) return

		try {
			// Generate agent name from display name (lowercase, replace spaces with dashes)
			const agentName = name
				.toLowerCase()
				.replace(/[^a-z0-9]+/g, '-')
				.replace(/^-+|-+$/g, '')

			const createdAgent = await createAgent(agentName, config)
			addAgent({
				name: createdAgent.name,
				icon: (config.icon ?? 'bot') as IconName
			})
			isNavigating = true
			await goto(resolve(`/agent/${createdAgent.name}`))
		} catch (error) {
			console.error('Failed to create agent:', error)
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
