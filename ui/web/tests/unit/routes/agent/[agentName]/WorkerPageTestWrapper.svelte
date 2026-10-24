<script lang="ts">
	import * as Tooltip from '$components/ui/tooltip';
	import WorkerPage from '$routes/agent/[agentName]/+page.svelte';
	import type { Agent } from '$lib/types/agent';
	import type { ActivityFeed } from '$lib/services/agent';

	interface TestPageData {
		agentsError: string | null;
		agent: Agent;
		activityFeed: ActivityFeed;
		activityError: string | null;
	}

	let { data }: { data: TestPageData } = $props();
</script>

<Tooltip.Provider>
	<WorkerPage {data} />
</Tooltip.Provider>
