<script lang="ts">
	import type { PageData } from './$types';
	import { SUPERAGENT_NAME } from '$lib/config';
	import AgentShell from '$components/agent/AgentShell.svelte';
	import { ArrowRight } from 'lucide-svelte';
	import { Button } from '$components/ui/button';
	import { resolve } from '$app/paths';
	import type { ActivityFeed } from '$lib/services/agent';

	let { data }: { data: PageData } = $props();

	let activityFeed = $state<ActivityFeed | null>(null);
	let activityError = $state<string | null>(null);

	$effect(() => {
		activityFeed = data.activityFeed;
		activityError = data.activityError;
	});

	const ctaHref = $derived(
		`${resolve('/')}?prompt=${encodeURIComponent(`Use the ${data.agent.label} agent to...`)}`
	);

	function handleActivityLoadMore(feed: ActivityFeed) {
		activityFeed = feed;
		activityError = null;
	}
</script>

<AgentShell
	agent={data.agent}
	{activityFeed}
	{activityError}
	onActivityLoadMore={handleActivityLoadMore}
>
	<div class="mx-auto max-w-[var(--content-max-width)]">
		<Button
			href={ctaHref}
			class="w-full rounded-3xl bg-overlay-dark/70 py-6 text-foreground hover:bg-overlay-light"
			variant="outline"
		>
			Ask {SUPERAGENT_NAME} about this agent
			<ArrowRight class="ml-2 size-5" />
		</Button>
	</div>
</AgentShell>
