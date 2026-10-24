<script lang="ts">
	import * as Tooltip from '$components/ui/tooltip';
	import AgentHeader from '$components/agent/AgentHeader.svelte';
	import type { Agent } from '$lib/types/agent';

	let {
		agent,
		activeTab,
		onTabChange
	}: {
		agent: Agent;
		activeTab?: 'conversation' | 'activity';
		onTabChange?: (tab: 'conversation' | 'activity') => void;
	} = $props();
</script>

<Tooltip.Provider>
	<AgentHeader {agent} {activeTab} {onTabChange} />
</Tooltip.Provider>
