<script lang="ts">
	import type { Snippet } from 'svelte'
	import * as Tooltip from '#components/ui/tooltip'
	import type { ActivityFeed } from '#lib/services/agent'
	import type { Agent } from '#lib/types/agent'
	import AgentShell from '#routes/agent/[agentId]/AgentShell.svelte'

	interface ThreadItem {
		id: string
		role: 'user' | 'assistant'
		content: string
	}

	let {
		agent,
		activeTab,
		onTabChange,
		threadItems,
		threadError,
		isLoadingThread,
		threadNextCursor,
		threadIsLoadingMore,
		threadLoadMoreError,
		onThreadLoadMore,
		activityFeed,
		activityError,
		isLoadingActivity,
		onActivityLoadMore,
		children
	}: {
		agent: Agent
		activeTab?: 'conversation' | 'activity'
		onTabChange?: (tab: 'conversation' | 'activity') => void
		threadItems?: ThreadItem[]
		threadError?: string | null
		isLoadingThread?: boolean
		threadNextCursor?: string | null
		threadIsLoadingMore?: boolean
		threadLoadMoreError?: string | null
		onThreadLoadMore?: () => void
		activityFeed?: ActivityFeed | null
		activityError?: string | null
		isLoadingActivity?: boolean
		onActivityLoadMore?: (feed: ActivityFeed) => void
		children?: Snippet
	} = $props()
</script>

<Tooltip.Provider>
	<AgentShell
		{agent}
		{activeTab}
		{onTabChange}
		{threadItems}
		{threadError}
		{isLoadingThread}
		{threadNextCursor}
		{threadIsLoadingMore}
		{threadLoadMoreError}
		{onThreadLoadMore}
		{activityFeed}
		{activityError}
		{isLoadingActivity}
		{onActivityLoadMore}
	>
		{#if children}
			{@render children()}
		{/if}
	</AgentShell>
</Tooltip.Provider>
