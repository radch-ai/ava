<script lang="ts">
	import AppSidebar from '#components/AppSidebar.svelte'
	import * as Sidebar from '#components/ui/sidebar'
	import type { AgentIdentity } from '#lib/types/agent'

	let open = $state(true)
	let { agents }: { agents: AgentIdentity[] } = $props()
</script>

<Sidebar.Provider bind:open>
	<AppSidebar {agents} />
</Sidebar.Provider>
