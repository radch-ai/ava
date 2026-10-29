<script lang="ts">
	import { Bot } from '@lucide/svelte'
	import { AgentConfigEditor } from '#components/agentconfig'
	import {
		UnsavedChangesGuards,
		type UnsavedChangesGuardsExports
	} from '#components/unsaved-changes'
	import { createAgent } from '#lib/services/agent'
	import {
		type AgentConfiguration,
		createDefaultAgentConfiguration
	} from '#lib/types/agent'
	import { goto, invalidate } from '$app/navigation'
	import { resolve } from '$app/paths'
	import { isValidAgentName } from './agent-name'

	let configuration = $state<AgentConfiguration>(
		createDefaultAgentConfiguration()
	)
	let hasChanges = $state(false)

	// Unsaved-changes protection — the editor confirms Cancel; navigation
	// and reload are guarded below (full level map in UnsavedChangesGuards).
	// Programmatic leaves (cancel, save-and-goto) run through bypassNavigation.
	let guard = $state<UnsavedChangesGuardsExports>()

	// ============================================================
	// PAGE ACTIONS
	// ============================================================

	function handleCancel() {
		void guard?.bypassNavigation(() => history.back())
	}

	let createError = $state<string | null>(null)

	async function handleSave(config: AgentConfiguration) {
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
			// Refresh the agents list so the sidebar shows the new agent.
			// Awaited so the target page's parent data already includes the
			// new agent when its load looks it up.
			await invalidate('app:agents')
			await guard?.bypassNavigation(() =>
				goto(resolve('/(app)/agent/[agentId]', { agentId: createdAgent.id }))
			)
		} catch (error) {
			createError =
				error instanceof Error ? error.message : 'Failed to create agent'
		}
	}
</script>

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

<UnsavedChangesGuards bind:this={guard} {hasChanges} />
