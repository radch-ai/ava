<script lang="ts">
	import { type Snippet, tick } from 'svelte'
	import { Button } from '#components/ui/button'
	import { Separator } from '#components/ui/separator'
	import { SUPERAGENT_AGENT_ID } from '#lib/config'
	import {
		type ActivityFeed,
		getAgentActivityFeed,
		mergePastFeed,
		threadRowKey
	} from '#lib/services/agent'
	import type { AgentIdentity, ThreadItem } from '#lib/types/agent'
	import ActivityContent from './ActivityContent.svelte'
	import AgentHeader from './AgentHeader.svelte'
	import ThreadContent from './ThreadContent.svelte'

	interface Props {
		agent: AgentIdentity
		activeTab?: 'conversation' | 'activity'
		onTabChange?: (tab: 'conversation' | 'activity') => void
		threadItems?: ThreadItem[]
		/** Live tail: turn messages assembled from gateway events. */
		liveItems?: ThreadItem[]
		threadError?: string | null
		isLoadingThread?: boolean
		threadNextCursor?: string | null
		threadIsLoadingMore?: boolean
		threadLoadMoreError?: string | null
		onThreadLoadMore?: () => void | Promise<void>
		/** Waiting queue rows rendered below the thread: changes resize
		 * the scroll region, so a reader near the bottom stays pinned
		 * instead of losing the newest rows under the fold. */
		queueRows?: number
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
		liveItems,
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
		queueRows,
		children
	}: Props = $props()

	const agentId = $derived(agent.id)
	const isConversation = $derived(
		agentId === SUPERAGENT_AGENT_ID && activeTab === 'conversation'
	)
	// A running turn's user row is live; the refetched stored copy must
	// not render beside it. Live wins until the turn settles — and with no
	// live rows there is nothing to filter, so the stored list is used
	// as-is.
	const conversationItems = $derived.by(() => {
		const live = liveItems ?? []
		const stored = threadItems ?? []
		if (live.length === 0) return stored
		const liveIds = new Set(live.map(threadRowKey))
		return [...stored.filter((m) => !liveIds.has(threadRowKey(m))), ...live]
	})

	const error = $derived(isConversation ? threadError : activityError)
	const isLoading = $derived(
		isConversation ? isLoadingThread : isLoadingActivity
	)

	let isLoadingMore = $state(false)
	let loadMoreError = $state<string | null>(null)
	let scrollRegionElement: HTMLDivElement | undefined = $state()

	// One scroll follower covers both growth sources: the live tail (a
	// streaming answer, a new tool row) and the queue rows rendered below
	// the thread. A reader near the bottom stays pinned to the newest
	// rows; a reader elsewhere keeps the position. Both dependencies feed
	// one animation frame, so a burst of chunks costs one scroll, not one
	// per chunk.
	let followFrame: number | undefined
	function scheduleFollow() {
		if (followFrame !== undefined) cancelAnimationFrame(followFrame)
		followFrame = requestAnimationFrame(() => {
			followFrame = undefined
			void tick().then(() => {
				const el = scrollRegionElement
				if (!el) return
				const nearBottom =
					el.scrollHeight - el.scrollTop - el.clientHeight < 120
				if (nearBottom) el.scrollTo({ top: el.scrollHeight })
			})
		})
	}

	$effect(() => {
		const live = liveItems ?? []
		// Length alone misses chunks that grow an existing bubble, so the
		// tail's total text is part of the dependency.
		void live.reduce((sum, message) => sum + message.content.length, 0)
		void queueRows
		scheduleFollow()
		return () => {
			if (followFrame !== undefined) cancelAnimationFrame(followFrame)
		}
	})

	// Forced scroll for the submit moment only: unconditional, unlike
	// the near-bottom followers above — a submit always shows the newest
	// content; a queued item claiming later never scrolls.
	export function scrollToBottom() {
		void tick().then(() =>
			scrollRegionElement?.scrollTo({ top: scrollRegionElement.scrollHeight })
		)
	}

	// A freshly opened thread starts at the newest message — once per
	// agent, so live turns and Load-more never yank the view.
	let bottomedAgent: string | undefined
	$effect(() => {
		if (threadItems?.length && bottomedAgent !== agentId) {
			bottomedAgent = agentId
			void tick().then(() =>
				scrollRegionElement?.scrollTo({ top: scrollRegionElement.scrollHeight })
			)
		}
	})

	// Load-more prepends older rows above the viewport; anchor the
	// scroll by the height they added — native anchoring fails across
	// the async fetch.
	async function handleLoadMore() {
		const el = scrollRegionElement
		const previousScrollTop = el?.scrollTop ?? 0
		const previousHeight = el?.scrollHeight ?? 0

		if (isConversation) {
			await onThreadLoadMore?.()
		} else {
			if (!activityFeed?.nextCursor || isLoadingMore) return

			isLoadingMore = true
			loadMoreError = null

			try {
				const olderPage = await getAgentActivityFeed(
					agentId,
					20,
					activityFeed.nextCursor
				)
				const mergedFeed = mergePastFeed(activityFeed, olderPage)
				onActivityLoadMore?.(mergedFeed)
			} catch (e) {
				loadMoreError = e instanceof Error ? e.message : 'Failed to load more'
				return
			} finally {
				isLoadingMore = false
			}
		}

		await tick()
		const newHeight = scrollRegionElement?.scrollHeight ?? 0
		scrollRegionElement?.scrollTo({
			top: previousScrollTop + (newHeight - previousHeight)
		})
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
	<div class="flex-1 overflow-y-auto" bind:this={scrollRegionElement}>
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
					{#if isConversation}
						{#if conversationItems.length === 0}
							<p class="text-muted-foreground text-center">
								No messages yet. Start a conversation!
							</p>
						{:else}
							<ThreadContent threadItems={conversationItems} />
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
	@reference "#app.css";

	.content-container {
		@apply mx-auto w-full max-md:px-6 md:max-w-[var(--content-max-width)];
	}
</style>
