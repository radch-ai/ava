<script lang="ts">
	import type { Component } from 'svelte'
	import * as Tooltip from '#components/ui/tooltip'
	import type { ActivityFeed, AgentThread } from '#lib/services/agent'
	import type { Agent } from '#lib/types/agent'
	import AgentPage from '#routes/agent/[agentId]/+page.svelte'

	interface TestPageData {
		agent: Agent
		activityFeed: ActivityFeed
		activityError: string | null
		thread: AgentThread | null
		threadError: string | null
	}

	let { data }: { data: TestPageData } = $props()

	// page's real data prop is wider than TestPageData; cast for test rendering
	const TestAgentPage = AgentPage as unknown as Component<{
		data: TestPageData
	}>
</script>

<Tooltip.Provider>
	<TestAgentPage {data} />
</Tooltip.Provider>
