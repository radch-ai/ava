<script lang="ts">
	import AppSidebar from '#components/AppSidebar.svelte'
	import * as Sidebar from '#components/ui/sidebar'
	import type { Agent } from '#lib/types/agent'

	let open = $state(true)
	let { agents }: { agents: Agent[] } = $props()
</script>

<Sidebar.Provider bind:open>
	<AppSidebar {agents} />
</Sidebar.Provider>
