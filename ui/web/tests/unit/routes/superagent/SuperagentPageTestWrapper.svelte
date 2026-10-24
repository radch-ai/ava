<script lang="ts">
	import type { Component } from 'svelte'
	import * as Tooltip from '$components/ui/tooltip'
	import type { SuperagentThread } from '$lib/services/user'
	import type { Agent } from '$lib/types/agent'
	import SuperagentPage from '$routes/+page.svelte'

	interface TestPageData {
		agents: Agent[]
		thread: SuperagentThread | null
		threadError: string | null
	}

	let { data }: { data: TestPageData } = $props()

	// page's real data prop is wider than TestPageData; cast for test rendering
	const TestSuperagentPage = SuperagentPage as unknown as Component<{
		data: TestPageData
	}>
</script>

<Tooltip.Provider>
	<TestSuperagentPage {data} />
</Tooltip.Provider>
