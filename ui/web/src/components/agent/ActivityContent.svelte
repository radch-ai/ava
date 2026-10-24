<script lang="ts">
	import type { AgentActivityItem } from '$lib/services/agent'
	import { formatRelativeTime } from '$lib/utils/date'

	let { items }: { items: AgentActivityItem[] } = $props()
</script>

{#each items as activity (activity.id)}
	<div
		class="activity-card rounded-lg border-l-[3px] p-4 transition-all duration-200 {activity.status ===
		'failed'
			? 'border-l-destructive bg-destructive/[0.06] hover:bg-destructive/[0.1]'
			: activity.status === 'running'
				? 'border-l-amber-500 bg-amber-500/[0.06] hover:bg-amber-500/[0.1]'
				: 'border-l-primary bg-overlay-dark/70 hover:bg-overlay-dark/90'}"
	>
		<div class="flex items-start justify-between gap-3">
			<p class="text-muted-foreground text-sm">
				{activity.triggeredBy} <span class="mx-1 opacity-50">•</span>
				{formatRelativeTime(activity.createdAt)}
			</p>
			<div
				class="flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-semibold capitalize {activity.status ===
				'failed'
					? 'bg-destructive/15 text-destructive'
					: activity.status === 'running'
						? 'bg-amber-500/15 text-amber-500'
						: 'bg-overlay-light text-muted-foreground'}"
			>
				{#if activity.status === 'running'}
					<span class="relative flex size-2">
						<span
							class="absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-500 opacity-75"
						></span>
						<span
							class="relative inline-flex size-2 rounded-full bg-amber-500"
						></span>
					</span>
				{:else if activity.status === 'failed'}
					<span class="bg-destructive size-1.5 rounded-full"></span>
				{:else}
					<span class="bg-primary size-1.5 rounded-full"></span>
				{/if}
				{activity.status}
			</div>
		</div>
		<p class="text-foreground/90 mt-2 text-sm font-medium">
			{activity.action}
		</p>
		{#if activity.message}
			<p class="text-foreground/80 mt-1 text-sm whitespace-pre-wrap">
				{activity.message}
			</p>
		{/if}
	</div>
{/each}
