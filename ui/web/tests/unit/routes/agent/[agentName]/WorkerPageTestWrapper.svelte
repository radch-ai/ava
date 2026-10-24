<script lang="ts">
	import * as Tooltip from '#components/ui/tooltip'
	import type { ActivityFeed } from '#lib/services/agent'
	import type { Agent } from '#lib/types/agent'
	import WorkerPage from '#routes/agent/[agentName]/+page.svelte'

	interface TestPageData {
		agentsError: string | null
		agent: Agent
		activityFeed: ActivityFeed
		activityError: string | null
	}

	let { data }: { data: TestPageData } = $props()
</script>

<Tooltip.Provider>
	<WorkerPage {data} />
</Tooltip.Provider>
