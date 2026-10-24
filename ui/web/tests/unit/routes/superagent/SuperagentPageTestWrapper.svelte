<script lang="ts">
	import * as Tooltip from '$components/ui/tooltip';
	import SuperagentPage from '$routes/+page.svelte';
	import type { SuperagentThread } from '$lib/services/user';
	import type { Agent } from '$lib/types/agent';
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	type AnyComponent = any;

	interface TestPageData {
		agents: Agent[];
		thread: SuperagentThread | null;
		threadError: string | null;
	}

	let { data }: { data: TestPageData } = $props();

	// Cast SuperagentPage to accept any data prop for testing purposes
	const TestSuperagentPage = SuperagentPage as unknown as AnyComponent;
</script>

<Tooltip.Provider>
	<TestSuperagentPage {data} />
</Tooltip.Provider>
