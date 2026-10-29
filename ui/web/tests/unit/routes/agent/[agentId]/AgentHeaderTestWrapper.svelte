<script lang="ts">
	import * as Tooltip from '#components/ui/tooltip'
	import type { Agent } from '#lib/types/agent'
	import AgentHeader from '#routes/agent/[agentId]/AgentHeader.svelte'

	let {
		agent,
		activeTab,
		onTabChange
	}: {
		agent: Agent
		activeTab?: 'conversation' | 'activity'
		onTabChange?: (tab: 'conversation' | 'activity') => void
	} = $props()
</script>

<Tooltip.Provider>
	<AgentHeader {agent} {activeTab} {onTabChange} />
</Tooltip.Provider>
