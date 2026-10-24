<script lang="ts">
	import { ArrowRight } from '@lucide/svelte'
	import AgentShell from '#components/agent/AgentShell.svelte'
	import { Button } from '#components/ui/button'
	import { SUPERAGENT_NAME } from '#lib/config'
	import type { ActivityFeed } from '#lib/services/agent'
	import { resolve } from '$app/paths'
	import type { PageData } from './$types'

	let { data }: { data: PageData } = $props()

	let activityFeed = $state<ActivityFeed | null>(null)
	let activityError = $state<string | null>(null)

	$effect(() => {
		activityFeed = data.activityFeed
		activityError = data.activityError
	})

	const ctaHref = $derived(
		`${resolve('/(app)')}?prompt=${encodeURIComponent(`Use the ${data.agent.label} agent to...`)}`
	)

	function handleActivityLoadMore(feed: ActivityFeed) {
		activityFeed = feed
		activityError = null
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
			class="bg-overlay-dark/70 text-foreground hover:bg-overlay-light w-full rounded-3xl py-6"
			variant="outline"
		>
			Ask {SUPERAGENT_NAME} about this agent
			<ArrowRight class="ml-2 size-5" />
		</Button>
	</div>
</AgentShell>
