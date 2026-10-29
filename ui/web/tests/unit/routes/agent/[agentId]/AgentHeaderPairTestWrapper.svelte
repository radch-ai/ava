<script lang="ts">
	import * as Tooltip from '#components/ui/tooltip'
	import type { Agent } from '#lib/types/agent'
	import AgentHeader from '#routes/agent/[agentId]/AgentHeader.svelte'

	let {
		agents,
		activeTab,
		onTabChange
	}: {
		agents: Agent[]
		activeTab?: 'conversation' | 'activity'
		onTabChange?: (tab: 'conversation' | 'activity') => void
	} = $props()
</script>

<Tooltip.Provider delayDuration={0}>
	{#each agents as agent (agent.name)}
		<AgentHeader {agent} {activeTab} {onTabChange} />
	{/each}
</Tooltip.Provider>
