<script lang="ts">
	import { onMount } from 'svelte'
	import AgentShell from '#components/agent/AgentShell.svelte'
	import ModelSelector from '#components/ModelSelector.svelte'
	import { APP_NAME, SUPERAGENT_NAME } from '#lib/config'
	import { defaultModel } from '#lib/models'
	import { type ActivityFeed, getAgentActivityFeed } from '#lib/services/agent'
	import {
		getSuperagentThread,
		mergePastSuperagentThread
	} from '#lib/services/user'
	import { SUPERAGENT_AGENT } from '#lib/types/agent'
	import type { PageData } from './$types'
	import TextArea from './TextArea.svelte'

	let { data }: { data: PageData } = $props()

	let activeTab = $state<'conversation' | 'activity'>('conversation')
	let thread = $state<typeof data.thread | undefined>(undefined)
	let threadError = $state<typeof data.threadError | undefined>(undefined)

	$effect(() => {
		thread = data.thread
		threadError = data.threadError
	})
	let isLoadingMore = $state(false)
	let loadMoreError = $state<string | null>(null)

	let activityFeed = $state<ActivityFeed | null>(null)
	let activityError = $state<string | null>(null)
	let isLoadingActivity = $state(false)

	const MODEL_KEY = `${APP_NAME.toLowerCase()}-chat-model`
	let selectedModel = $state(defaultModel)

	onMount(() => {
		const stored = localStorage.getItem(MODEL_KEY)
		if (stored) {
			selectedModel = stored
		}
	})

	$effect(() => {
		if (typeof window === 'undefined') return
		void selectedModel
		localStorage.setItem(MODEL_KEY, selectedModel)
	})

	async function loadMore() {
		if (!thread?.nextCursor || isLoadingMore) return

		isLoadingMore = true
		loadMoreError = null

		try {
			const olderPage = await getSuperagentThread(20, thread.nextCursor)
			thread = mergePastSuperagentThread(thread, olderPage)
		} catch (error) {
			loadMoreError =
				error instanceof Error ? error.message : 'Failed to load more messages'
		} finally {
			isLoadingMore = false
		}
	}

	async function loadActivity() {
		if (activityFeed || activityError || isLoadingActivity) return

		isLoadingActivity = true
		activityError = null

		try {
			activityFeed = await getAgentActivityFeed(SUPERAGENT_NAME.toLowerCase())
		} catch (error) {
			activityError =
				error instanceof Error ? error.message : 'Failed to load activity'
		} finally {
			isLoadingActivity = false
		}
	}

	function selectTab(tab: 'conversation' | 'activity') {
		if (tab === 'activity') {
			loadActivity()
		}
		activeTab = tab
	}

	// biome-ignore lint/correctness/noUnusedFunctionParameters: used once the message-send API lands
	function handleComposerSubmit(text: string) {
		// TODO: send text as the agent prompt and refresh the thread
		// (message-send API not implemented yet)
		activeTab = 'conversation'
	}

	function handleActivityLoadMore(feed: ActivityFeed) {
		activityFeed = feed
	}
</script>

<AgentShell
	agent={SUPERAGENT_AGENT}
	{activeTab}
	onTabChange={selectTab}
	threadItems={thread?.items}
	{threadError}
	threadNextCursor={thread?.nextCursor}
	threadIsLoadingMore={isLoadingMore}
	threadLoadMoreError={loadMoreError}
	onThreadLoadMore={loadMore}
	{activityFeed}
	{activityError}
	{isLoadingActivity}
	onActivityLoadMore={handleActivityLoadMore}
>
	<div
		class="items-end max-md:flex max-md:flex-col md:grid md:grid-cols-[1fr_var(--content-max-width)_1fr]"
	>
		<div
			class="mr-2 flex items-center justify-end gap-1 max-md:my-2 md:mb-[2.5px] md:ml-4"
		>
			<span class="text-muted-foreground text-sm">Model:</span>
			<ModelSelector
				bind:value={selectedModel}
				class="!bg-overlay-dark/70 hover:!bg-overlay-light hover:!ring-ring hover:!ring-1"
			/>
		</div>
		<TextArea onsubmit={handleComposerSubmit} />
	</div>
</AgentShell>
