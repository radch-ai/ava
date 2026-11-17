<script lang="ts">
	import { ArrowRight } from '@lucide/svelte'
	import { onMount } from 'svelte'
	import ModelSelector from '#components/ModelSelector.svelte'
	import { Button } from '#components/ui/button'
	import { SUPERAGENT_AGENT_ID, SUPERAGENT_NAME } from '#lib/config'
	import type { ModelSelection } from '#lib/models'
	import {
		type ActivityFeed,
		type AgentSocket,
		type AgentSocketEvent,
		type AgentSocketState,
		createAgentSocket,
		createLiveTurns,
		getAgentActivityFeed,
		getAgentThread,
		type LaneStatus,
		mergePastThread
	} from '#lib/services/agent'
	import type { QueueActiveItem, QueueItem } from '#lib/types/gen/wstypes.gen'
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

	// The selected model rides each prompt frame; localStorage
	// keeps the browser's choice across reloads.
	const modelStorageKey = `ava.model.${data.agent.id}`
	let selectedModel = $state<ModelSelection>(readStoredModel())

	function readStoredModel(): ModelSelection {
		try {
			const raw = localStorage.getItem(modelStorageKey)
			const entry = raw ? (JSON.parse(raw) as ModelSelection) : null
			return entry &&
				typeof entry.id === 'string' &&
				typeof entry.providerId === 'string'
				? entry
				: null
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

	// One socket per conversation surface, opened eagerly on mount: a
	// page (re)loaded mid-turn asks for lane status on connect, which
	// joins the lane and replays the streamed prefix — a lazy,
	// first-submit socket would miss that window. Reconnects happen
	// inside the socket; state below tracks what the strip renders.
	let socket: AgentSocket | undefined
	let socketState = $state<AgentSocketState | 'idle'>('idle')
	let laneStatus = $state<LaneStatus | null>(null)
	const liveTurns = createLiveTurns()

	// The gateway-owned queue projection: full-replacement snapshots
	// only — the page never schedules or mutates the queue itself.
	let queueItems = $state<QueueItem[]>([])
	let queuePaused = $state(false)
	let queueActiveItem = $state<QueueActiveItem | null>(null)
	// The send-now reservation: claimed-but-not-started work, pinned
	// above the waiting list until its claim snapshot lands.
	let queueNextItem = $state<QueueItem | null>(null)
	// Item ids the gateway has claimed: a stale pre-claim snapshot
	// replayed on reconnect must not resurrect them as waiting.
	const executedItemIds = new Set<string>()
	let pendingSendNowId = $state<string | null>(null)
	// The shell's forced scroll: a submit always drops the view to
	// the newest content. A queued item claiming later must NOT —
	// the reader may have gone up to check something.
	let shell = $state<AgentShell>()

	// Only genuinely waiting work renders above the composer; the
	// running item lives in the transcript's user row.
	const waitingItems = $derived(
		queueItems.filter((item) => !executedItemIds.has(item.id))
	)
	// Busy is gateway truth: a turn in flight, or a claimed item —
	// the claim snapshot can beat the lane's working broadcast.
	const composerWorking = $derived(
		(laneStatus?.working ?? false) || queueActiveItem !== null
	)

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

	async function handleComposerSubmit(text: string): Promise<void> {
		activeTab = 'conversation'
		// A submit always drops the view to the newest content —
		// before any ack or claim, wherever the reader was.
		shell?.scrollToBottom()
		const op = await socket?.sendPrompt(text, selectedModel)
		if (!op) {
			throw new Error('not connected')
		}
		if (!op.accepted) {
			throw new Error(op.error || 'the gateway rejected the prompt')
		}
		// The user row appears when the queue snapshot shows the item
		// running — never at submit time.
	}

	function handleInterrupt() {
		socket?.sendInterrupt()
	}

	async function handleQueueSendNow(itemId: string) {
		if (pendingSendNowId) {
			return
		}
		pendingSendNowId = itemId
		try {
			// The current selection rides the frame: a paused queue
			// rebinds its failed-model entries to it.
			await socket?.sendNow(itemId, selectedModel)
		} catch {
			// Unconfirmed, not rejected: the next queue snapshot is
			// the truth; nothing to resubmit.
		} finally {
			pendingSendNowId = null
		}
	}

	// Connection state from the socket: live means the gateway greeted
	// us — on every (re)connection the lane status is re-requested so
	// a reconnect resyncs instead of trusting stale state.
	function handleSocketState(state: AgentSocketState) {
		socketState = state
		if (state !== 'live') {
			laneStatus = null
			return
		}
		socket?.requestStatus()
	}

	// Turn events fold into the live conversation tail; lane state
	// mirrors into the strip; queue snapshots replace the queue
	// projection and echo the running item's user row under its
	// server-minted id. A turn is over when its final message or its
	// stored failure arrives — the sink wrote before the event, so a
	// refetch replaces the newest window (keeping any loaded older
	// pages) and the live copies it covers are dropped, reconciling
	// by server ids so identical prompt texts never collide. Pages
	// arrive oldest→newest; anything already loaded that the fresh
	// window does not carry is strictly older, so the settled turn
	// lands at the bottom. Refetches serialize — an older response
	// can never overwrite a newer snapshot — and a stored error
	// whose refetch failed retries on the next terminal event
	// instead of lingering as a live copy forever.
	let reconcileChain: Promise<void> = Promise.resolve()
	const pendingErrorRows = new Set<string>()

	function handleSocketEvent(event: AgentSocketEvent) {
		liveTurns.applyEvent(event)
		if (event.type === 'queue_state') {
			queueItems = event.data.items
			queuePaused = event.data.paused
			queueActiveItem = event.data.active ?? null
			queueNextItem = event.data.next ?? null
			if (queueActiveItem) {
				executedItemIds.add(queueActiveItem.id)
			} else if (event.data.items.length === 0 && !event.data.paused) {
				executedItemIds.clear()
			}
			// The strip's queue count rides the snapshot too —
			// lane_status only fires on working transitions. Waiting
			// rows are pending plus the pinned reservation, the
			// server's own definition.
			if (laneStatus) {
				laneStatus = {
					...laneStatus,
					queue_len: event.data.items.length + (event.data.next ? 1 : 0)
				}
			}
			// The claim's user row mints under its server id; beginUser
			// is idempotent, so snapshot replays never duplicate it. No
			// scroll here: a claimed item must not yank a reader who
			// moved up while it waited — only submits scroll.
			const active = event.data.active
			if (active) {
				liveTurns.beginUser(active.user_message_id, active.content)
			}
		}
		if (event.type === 'lane_status') {
			// The status reply can trail a fresher queue snapshot; the
			// projection wins for the count, pending plus reservation.
			laneStatus = {
				...event.data,
				queue_len: queueItems.length + (queueNextItem ? 1 : 0)
			}
		}
		if (event.type === 'message') {
			queueReconcile(event.data.id)
		}
		if (event.type === 'error' && !event.data.tool && event.data.id) {
			queueReconcile(event.data.turn_id, event.data.id)
		}
	}

	function queueReconcile(turnId: string, errorRowId?: string) {
		if (errorRowId) pendingErrorRows.add(errorRowId)
		reconcileChain = reconcileChain.then(() => reconcileTurn(turnId))
	}

	async function reconcileTurn(id: string) {
		// The stored errors this pass may settle; the rest retry.
		const settlingErrors = [...pendingErrorRows]
		try {
			const fresh = await getAgentThread(data.agent.id, 20)
			const loaded = thread?.items ?? []
			const freshIds = new Set(fresh.items.map((m) => m.id))
			thread = {
				items: [...loaded.filter((m) => !freshIds.has(m.id)), ...fresh.items],
				nextCursor: fresh.nextCursor
			}
			liveTurns.settleTurn(
				id,
				fresh.items.filter((m) => m.role === 'user').map((m) => m.id),
				fresh.items.filter((m) => m.role === 'error').map((m) => m.id)
			)
			for (const errorId of settlingErrors) {
				if (freshIds.has(errorId)) pendingErrorRows.delete(errorId)
			}
		} catch {
			// Refetch failed: the live tail stays the truth until the
			// next terminal event retries the pending stored errors.
		}
	}

	onMount(() => {
		socket = createAgentSocket(
			data.agent.id,
			handleSocketEvent,
			handleSocketState
		)
		return () => socket?.close()
	})

	function handleActivityLoadMore(feed: ActivityFeed) {
		activityFeed = feed
		activityError = null
	}

	const ctaHref = $derived(
		`${resolve('/(app)/agent/[agentId]', { agentId: SUPERAGENT_AGENT_ID })}?prompt=${encodeURIComponent(`Use the ${data.agent.name} agent to...`)}`
	)

	const laneStatusLabel = $derived(
		laneStatus
			? `lane ${laneStatus.active ? 'active' : 'idle'} · ${laneStatus.model || 'Default'} · queue ${laneStatus.queue_len}`
			: ''
	)
</script>

<AgentShell
	bind:this={shell}
	agent={data.agent}
	{activeTab}
	onTabChange={isSuperagent ? selectTab : undefined}
	threadItems={thread?.items}
	liveItems={liveTurns.messages}
	{threadError}
	threadNextCursor={thread?.nextCursor}
	threadIsLoadingMore={isLoadingMore}
	threadLoadMoreError={loadMoreError}
	onThreadLoadMore={loadMore}
	{activityFeed}
	{activityError}
	{isLoadingActivity}
	onActivityLoadMore={handleActivityLoadMore}
	queueRows={waitingItems.length + (queueNextItem ? 1 : 0)}
>
	{#if isSuperagent}
		<div class="mx-auto w-full md:max-w-[calc(var(--content-max-width)+2rem)]">
			<!-- The queue renders only with waiting work: an empty queue
			hides the whole block, giving the thread its space back. -->
			{#if waitingItems.length > 0 || queueNextItem}
				<div class="mb-2">
					<QueueList
						items={waitingItems}
						next={queueNextItem}
						paused={queuePaused}
						pendingId={pendingSendNowId}
						onsendnow={handleQueueSendNow}
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
				oninterrupt={handleInterrupt}
			/>
		</div>

		{#if socketState !== 'idle'}
			<div
				class="text-muted-foreground mx-auto mt-1 flex w-fit items-center gap-2 text-xs"
			>
				{#if socketState === 'live'}
					<span class="size-1.5 rounded-full bg-emerald-500"></span>
					{#if laneStatus}
						<span>{laneStatusLabel}</span>
					{:else}
						<span>connected</span>
					{/if}
				{:else if socketState === 'reconnecting'}
					<span class="size-1.5 rounded-full bg-amber-500"></span>
					<span>reconnecting…</span>
				{:else}
					<span class="size-1.5 rounded-full bg-amber-500"></span>
					<span>connecting…</span>
				{/if}
			</div>
		{/if}
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
