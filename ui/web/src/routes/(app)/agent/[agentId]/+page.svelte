<script lang="ts">
	import { ArrowRight } from '@lucide/svelte'
	import { onMount } from 'svelte'
	import AgentShell from '#components/agent/AgentShell.svelte'
	import ModelSelector from '#components/ModelSelector.svelte'
	import { Button } from '#components/ui/button'
	import { APP_NAME, SUPERAGENT_AGENT_ID, SUPERAGENT_NAME } from '#lib/config'
	import { defaultModel } from '#lib/models'
	import {
		type ActivityFeed,
		getAgentActivityFeed,
		getAgentThread,
		mergePastThread
	} from '#lib/services/agent'
	import { resolve } from '$app/paths'
	import type { PageData } from './$types'
	import TextArea from './TextArea.svelte'

	let { data }: { data: PageData } = $props()

	// One page for every agent; the superagent id mounts the conversation
	// surface (tabs, thread, composer) on top of the shared shell.
	const isSuperagent = $derived(data.agent.id === SUPERAGENT_AGENT_ID)

	let activeTab = $state<'conversation' | 'activity'>('conversation')
	let thread = $state<PageData['thread'] | undefined>(undefined)
	let threadError = $state<string | null | undefined>(undefined)

	let isLoadingMore = $state(false)
	let loadMoreError = $state<string | null>(null)

	let activityFeed = $state<ActivityFeed | null>(null)
	let activityError = $state<string | null>(null)
	let isLoadingActivity = $state(false)

	$effect(() => {
		thread = data.thread
		threadError = data.threadError
		if (!isSuperagent) {
			activityFeed = data.activityFeed
			activityError = data.activityError
		}
	})

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
			const olderPage = await getAgentThread(
				data.agent.id,
				20,
				thread.nextCursor
			)
			thread = mergePastThread(thread, olderPage)
		} catch (e) {
			loadMoreError =
				e instanceof Error ? e.message : 'Failed to load more messages'
		} finally {
			isLoadingMore = false
		}
	}

	async function loadActivity() {
		if (activityFeed || activityError || isLoadingActivity) return

		isLoadingActivity = true
		activityError = null

		try {
			activityFeed = await getAgentActivityFeed(data.agent.id)
		} catch (e) {
			activityError = e instanceof Error ? e.message : 'Failed to load activity'
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

	function handleComposerSubmit(_text: string) {
		activeTab = 'conversation'
	}

	function handleActivityLoadMore(feed: ActivityFeed) {
		activityFeed = feed
		activityError = null
	}

	const ctaHref = $derived(
		`${resolve('/(app)/agent/[agentId]', { agentId: SUPERAGENT_AGENT_ID })}?prompt=${encodeURIComponent(`Use the ${data.agent.name} agent to...`)}`
	)
</script>

<AgentShell
	agent={data.agent}
	{activeTab}
	onTabChange={isSuperagent ? selectTab : undefined}
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
	{#if isSuperagent}
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
	{:else}
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
	{/if}
</AgentShell>
