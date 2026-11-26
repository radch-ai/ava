<script lang="ts">
	import { ArrowRight } from '@lucide/svelte'
	import { untrack } from 'svelte'
	import ModelSelector from '#components/ModelSelector.svelte'
	import { Button } from '#components/ui/button'
	import { SUPERAGENT_AGENT_ID, SUPERAGENT_NAME } from '#lib/config'
	import {
		isModelSelection,
		type ModelSelection,
		modelsStore
	} from '#lib/models'
	import {
		type ActivityFeed,
		type AgentSocketState,
		createConversation,
		getAgentActivityFeed
	} from '#lib/services/agent'
	import { resolve } from '$app/paths'
	import type { PageData } from './$types'
	import AgentShell from './AgentShell.svelte'
	import QueueList from './QueueList.svelte'
	import TextArea from './TextArea.svelte'

	let { data }: { data: PageData } = $props()

	// One page for every agent; the superagent id mounts the conversation
	// surface (tabs, thread, composer) on top of the shared shell.
	const isSuperagent = $derived(data.agent.id === SUPERAGENT_AGENT_ID)

	let activeTab = $state<'conversation' | 'activity'>('conversation')

	let activityFeed = $state<ActivityFeed | null>(null)
	let activityError = $state<string | null>(null)
	let isLoadingActivity = $state(false)

	// The conversation store owns everything the gateway feeds: thread
	// segments, live tail, queue, lane, and the socket itself. One
	// store per agent — the swap below replaces it whole, so no
	// projection field is ever reset by hand.
	let conversation = $state.raw(
		untrack(() =>
			createConversation(data.agent.id, {
				thread: data.thread,
				threadError: data.threadError
			})
		)
	)
	let conversationAgentId = untrack(() => data.agent.id)

	// The id changes only on a real agent switch — never on a
	// same-agent reload, whose loaded pages and followed thread must
	// survive — and the effect swaps the store when it does. The
	// cleanup closes whichever store is current: the old lane's socket
	// dies before its replacement connects, and on unmount.
	const agentId = $derived(data.agent.id)
	$effect(() => {
		const nextId = agentId
		if (nextId !== conversationAgentId) {
			conversationAgentId = nextId
			conversation = createConversation(
				nextId,
				untrack(() => ({
					thread: data.thread,
					threadError: data.threadError
				}))
			)
		}
		return () => {
			conversation.dispose()
		}
	})

	// The activity feed's first page rides the load for other agents;
	// the same per-agent rule as the conversation: a mid-session
	// invalidation must not wipe a loaded feed.
	let activityAgentId = ''
	$effect(() => {
		if (activityAgentId === data.agent.id) return
		activityAgentId = data.agent.id
		if (!isSuperagent) {
			activityFeed = data.activityFeed
			activityError = data.activityError
		}
	})

	// The selected model rides each prompt frame; localStorage
	// keeps the browser's choice across reloads.
	const modelStorageKey = `ava.model.${data.agent.id}`
	let selectedModel = $state<ModelSelection>(readStoredModel())

	function readStoredModel(): ModelSelection {
		try {
			const raw = localStorage.getItem(modelStorageKey)
			const entry: unknown = raw ? JSON.parse(raw) : null
			return isModelSelection(entry) ? entry : null
		} catch {
			return null
		}
	}

	function handleModelChange(selection: ModelSelection) {
		selectedModel = selection
		try {
			if (selection) {
				localStorage.setItem(modelStorageKey, JSON.stringify(selection))
			} else {
				localStorage.removeItem(modelStorageKey)
			}
		} catch {
			// Storage unavailable: the selection lives for this session.
		}
	}

	$effect(() => {
		// A selection must name a catalog entry: providers change, and
		// a stored pair the selector cannot display would still ride
		// every prompt frame as a phantom. Once the catalog has
		// loaded, an unknown selection reverts to default routing and
		// is forgotten; a failed catalog load clears nothing.
		if (!modelsStore.loaded) return
		const selection = selectedModel
		if (selection === null) return
		const known = modelsStore.groups.some((g) =>
			g.models.some(
				(m) => m.id === selection.id && m.providerId === selection.providerId
			)
		)
		if (!known) handleModelChange(null)
	})

	// The shell's forced scroll: a submit always drops the view to
	// the newest content. A queued item claiming later must NOT —
	// the reader may have gone up to check something.
	let shell = $state<AgentShell>()

	// Busy is gateway truth — a turn in flight, or a claimed item — and
	// only while the lane is live: during recovery the strip's state is
	// stale, so the stop control stays hidden rather than acting on it.
	const composerWorking = $derived(
		conversation.socketState === 'live' &&
			((conversation.laneStatus?.working ?? false) ||
				conversation.queueActiveItem !== null)
	)

	// Auto-compaction in flight — the pre-prompt checker's or a
	// mid-loop rotation's summarization: the stream's pulse marker
	// shows it and send-now steps aside.
	const compacting = $derived(
		conversation.socketState === 'live' &&
			(conversation.laneStatus?.compacting ?? false)
	)

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

	async function handleComposerSubmit(text: string): Promise<void> {
		activeTab = 'conversation'
		// A submit always drops the view to the newest content —
		// before any ack or claim, wherever the reader was.
		shell?.scrollToBottom()
		const op = await conversation.sendPrompt(text, selectedModel)
		// Unconfirmed is not rejected: the ack never arrived, so the queue
		// snapshot decides. Only a real rejection keeps the draft.
		if (op.unconfirmed) {
			return
		}
		if (!op.accepted) {
			throw new Error(op.error || 'the gateway rejected the prompt')
		}
		// The user row appears when the queue snapshot shows the item
		// running — never at submit time.
	}

	function handleActivityLoadMore(feed: ActivityFeed) {
		activityFeed = feed
		activityError = null
	}

	const ctaHref = $derived(
		`${resolve('/(app)/agent/[agentId]', { agentId: SUPERAGENT_AGENT_ID })}?prompt=${encodeURIComponent(`Use the ${data.agent.name} agent to...`)}`
	)

	// The strip's label for each phase; the count comes from the queue
	// projection, not the lane broadcast, so a stale broadcast cannot
	// lower it.
	const laneStatusLabel = $derived(
		conversation.laneStatus
			? `lane ${conversation.laneStatus.active ? 'active' : 'idle'} · ${conversation.laneStatus.model || 'Default'} · queue ${conversation.queueLen()}`
			: ''
	)

	// The connection strip is one line of state: live reads as the lane
	// (or plain connected), every earlier phase as recovery.
	const STRIP_TEXT: Record<AgentSocketState, string> = {
		connecting: 'connecting…',
		syncing: 'syncing…',
		reconnecting: 'reconnecting…',
		live: 'connected',
		failed: 'connection failed'
	}

	const strip = $derived.by(() => {
		if (conversation.socketState === 'live') {
			return {
				tone: 'bg-emerald-500',
				text: conversation.laneStatus ? laneStatusLabel : STRIP_TEXT.live
			}
		}
		if (conversation.socketState === 'failed') {
			return { tone: 'bg-red-500', text: STRIP_TEXT.failed }
		}
		return { tone: 'bg-amber-500', text: STRIP_TEXT[conversation.socketState] }
	})
</script>

<AgentShell
	bind:this={shell}
	agent={data.agent}
	{activeTab}
	onTabChange={isSuperagent ? selectTab : undefined}
	threadItems={conversation.threadItems}
	liveItems={conversation.liveItems}
	threadError={conversation.threadError}
	threadNextCursor={conversation.historyCursor}
	threadIsLoadingMore={conversation.isLoadingMore}
	threadLoadMoreError={conversation.loadMoreError}
	onThreadLoadMore={conversation.loadMore}
	{activityFeed}
	{activityError}
	{isLoadingActivity}
	onActivityLoadMore={handleActivityLoadMore}
	queueRows={conversation.queueLen()}
>
	{#if isSuperagent}
		<div class="mx-auto w-full md:max-w-[calc(var(--content-max-width)+2rem)]">
			<!-- The queue renders only with waiting work: an empty queue
			hides the whole block, giving the thread its space back. -->
			{#if conversation.queueItems.length > 0 || conversation.queueNextItem}
				<div class="mb-2">
					<QueueList
						items={conversation.queueItems}
						next={conversation.queueNextItem}
						paused={conversation.queuePaused}
						pendingId={conversation.pendingSendNowId}
						{compacting}
						onsendnow={(itemId) =>
							void conversation.sendNow(itemId, selectedModel)}
					/>
				</div>
			{/if}
			<div class="mr-3.25 mb-1 flex items-center justify-end gap-1">
				<span class="text-muted-foreground text-sm">Model:</span>
				<ModelSelector
					value={selectedModel}
					onChange={handleModelChange}
					class="!bg-overlay-dark/70 hover:!bg-overlay-light hover:!ring-ring hover:!ring-1"
				/>
			</div>
			<TextArea
				onsubmit={handleComposerSubmit}
				working={composerWorking}
				oninterrupt={() => conversation.interrupt()}
				blocked={conversation.socketState !== 'live'}
			/>
		</div>

		<div
			class="text-muted-foreground mx-auto mt-1 flex w-fit items-center gap-2 text-xs"
		>
			<span class="size-1.5 rounded-full {strip.tone}"></span>
			<span>{strip.text}</span>
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
