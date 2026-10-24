<script lang="ts">
	import { type Snippet, tick } from 'svelte'
	import { Button } from '$components/ui/button'
	import { Separator } from '$components/ui/separator'
	import { SUPERAGENT_NAME } from '$lib/config'
	import {
		type ActivityFeed,
		getAgentActivityFeed,
		mergePastFeed
	} from '$lib/services/agent'
	import type { Agent } from '$lib/types/agent'
	import ActivityContent from './ActivityContent.svelte'
	import AgentHeader from './AgentHeader.svelte'
	import ThreadContent from './ThreadContent.svelte'

	interface ThreadItem {
		id: string
		role: 'user' | 'assistant'
		content: string
	}

	interface Props {
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
	}: Props = $props()

	const agentName = $derived(agent.name)
	const isConversation = $derived(
		agentName === SUPERAGENT_NAME.toLowerCase() && activeTab === 'conversation'
	)
	const error = $derived(isConversation ? threadError : activityError)
	const isLoading = $derived(
		isConversation ? isLoadingThread : isLoadingActivity
	)

	let isLoadingMore = $state(false)
	let loadMoreError = $state<string | null>(null)
	let scrollRegionElement: HTMLDivElement | undefined = $state()

	async function handleLoadMore() {
		if (isConversation) {
			onThreadLoadMore?.()
			return
		}

		if (!activityFeed?.nextCursor || isLoadingMore) return

		isLoadingMore = true
		loadMoreError = null
		const previousHeight = scrollRegionElement?.scrollHeight ?? 0

		try {
			const olderPage = await getAgentActivityFeed(
				agentName,
				20,
				activityFeed.nextCursor
			)
			const mergedFeed = mergePastFeed(activityFeed, olderPage)
			onActivityLoadMore?.(mergedFeed)
			await tick()
			const newHeight = scrollRegionElement?.scrollHeight ?? 0
			scrollRegionElement?.scrollTo({ top: newHeight - previousHeight })
		} catch (e) {
			loadMoreError = e instanceof Error ? e.message : 'Failed to load more'
		} finally {
			isLoadingMore = false
		}
	}

	const nextCursor = $derived(
		isConversation ? threadNextCursor : activityFeed?.nextCursor
	)
	const currentIsLoadingMore = $derived(
		isConversation ? threadIsLoadingMore : isLoadingMore
	)
	const currentLoadMoreError = $derived(
		isConversation ? threadLoadMoreError : loadMoreError
	)
</script>

<div class="flex h-full flex-col">
	<!-- Header -->
	<header>
		<div class="content-container">
			<AgentHeader {agent} {activeTab} {onTabChange} />
		</div>
		<div class="px-8 max-md:px-6">
			<Separator />
		</div>
	</header>

	<!-- ContentPanel -->
	<div
		class="custom-scrollbar flex-1 overflow-y-auto"
		bind:this={scrollRegionElement}
	>
		<div class="content-container py-4">
			{#if error}
				<p
					class="bg-destructive/10 text-destructive rounded-lg p-4 text-center"
				>
					{error}
				</p>
			{:else if isLoading}
				<p class="text-muted-foreground text-center">Loading...</p>
			{:else}
				<!-- Load More Button -->
				{#if nextCursor !== null}
					<div class="mb-4 flex flex-col items-center gap-2">
						{#if currentLoadMoreError}
							<p
								class="bg-destructive/10 text-destructive rounded-lg p-2 text-center text-sm"
							>
								{currentLoadMoreError}. Try again.
							</p>
						{/if}
						<Button
							variant="outline"
							onclick={handleLoadMore}
							disabled={currentIsLoadingMore}
						>
							{currentIsLoadingMore ? 'Loading...' : 'Load more'}
						</Button>
					</div>
				{/if}
				<!-- Content -->
				<div class="flex flex-col gap-4">
					<!-- prettier-ignore -->
					{#if isConversation}
						{#if !threadItems?.length}
							<p class="text-muted-foreground text-center">
								No messages yet. Start a conversation!
							</p>
						{:else}
							<ThreadContent {threadItems} />
						{/if}
					{:else}
						{#if !activityFeed?.items?.length}
							<p class="text-muted-foreground text-center">No activity yet.</p>
						{:else}
							<ActivityContent items={activityFeed.items} />
						{/if}
					{/if}
				</div>
			{/if}
		</div>
	</div>

	<!-- Footer -->
	<footer class="max-md:px-6">
		{@render children?.()}
	</footer>
</div>

<style lang="postcss">
	@reference "../../app.css";

	.content-container {
		@apply mx-auto w-full max-md:px-6 md:max-w-[var(--content-max-width)];
	}

	.custom-scrollbar {
		scrollbar-color: var(--muted-foreground) transparent;
	}
</style>
