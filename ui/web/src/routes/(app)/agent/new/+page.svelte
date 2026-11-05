<script lang="ts">
	import { Bot } from '@lucide/svelte'
	import { AgentConfigEditor } from '#components/agent-config'
	import {
		UnsavedChangesGuards,
		type UnsavedChangesGuardsExports
	} from '#components/unsaved-changes'
	import { createAgent, updateHeartbeat } from '#lib/services/agent'
	import {
		type AgentConfig,
		type AgentIdentity,
		createDefaultAgentConfig,
		createDefaultHeartbeatConfig
	} from '#lib/types/agent'
	import { goto, invalidate } from '$app/navigation'
	import { resolve } from '$app/paths'
	import { isValidAgentName } from './agent-name'

	let configuration = $state(createDefaultAgentConfig())
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

	async function handleSave(
		config: AgentConfig & Pick<AgentIdentity, 'name' | 'icon'>
	) {
		const name = config.name.trim()
		if (!name) return

		if (!isValidAgentName(name)) {
			throw new Error(
				'Invalid agent name: use letters, digits and single spaces only'
			)
		}

		// The server derives the id from the display name and rejects
		// invalid or duplicate names; the client never invents an id.
		const createdAgent = await createAgent({ ...config, name })
		// The heartbeat row is written by its own endpoint; skip the
		// call when the editor still holds the disabled default.
		if (
			JSON.stringify(config.heartbeat) !==
			JSON.stringify(createDefaultHeartbeatConfig())
		) {
			await updateHeartbeat(createdAgent.id, config.heartbeat)
		}
		// Refresh the agents list so the sidebar shows the new agent.
		// Awaited so the target page's parent data already includes the
		// new agent when its load looks it up.
		await invalidate('app:agents')
		await guard?.bypassNavigation(() =>
			goto(resolve('/(app)/agent/[agentId]', { agentId: createdAgent.id }))
		)
	}
</script>

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

<UnsavedChangesGuards bind:this={guard} {hasChanges} />
