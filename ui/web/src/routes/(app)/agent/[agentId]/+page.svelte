<script lang="ts">
	import { ArrowRight } from '@lucide/svelte'
	import { onMount } from 'svelte'
	import ModelSelector from '#components/ModelSelector.svelte'
	import { Button } from '#components/ui/button'
	import { SUPERAGENT_AGENT_ID, SUPERAGENT_NAME } from '#lib/config'
	import type { ModelSelection } from '#lib/models'
	import { modelsStore } from '#lib/models'
	import {
		type ActivityFeed,
		type AgentSocket,
		type AgentSocketEvent,
		type AgentSocketState,
		type AgentThread,
		createAgentSocket,
		createLiveTurns,
		getAgentActivityFeed,
		getAgentThread,
		type LaneStatus,
		threadRowKey
	} from '#lib/services/agent'
	import type { ThreadItem } from '#lib/types/agent'
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
	let threadError = $state<string | null | undefined>(undefined)

	/** The boundary label of a segment; undefined when the segment
	 * starts the display. */
	interface ThreadSegment {
		threadId: string
		items: ThreadItem[]
		dividerContent: string | undefined
	}

	// The display is a chain of thread segments, oldest first. Order is
	// arrival order, never a timestamp comparison: a thread's created_at
	// is DB wall-clock with no strict-order guarantee, so it cannot say
	// which boundary came first.
	let segments = $state<ThreadSegment[]>([])
	// The oldest-loaded continuation. Seeded from the first page and
	// written by Load more only: a refresh must never move the reader's
	// pagination depth, nor turn an exhausted cursor back into one.
	let historyCursor = $state<string | null | undefined>(undefined)
	// The thread the surface follows: refreshes pin to it, so a turn
	// that ran in a thread the reader has since left stays out.
	let activeThreadId = $state<string | undefined>(undefined)
	// previous thread id -> the thread that replaced it, from
	// command_completed: a delayed refresh for a thread the display has
	// not seen yet slots in above its successor.
	const successorThread = new Map<string, string>()

	// The flat projection the shell renders: each segment's boundary
	// row above its own rows.
	const threadItems = $derived(
		segments.flatMap((segment): ThreadItem[] =>
			segment.dividerContent === undefined
				? segment.items
				: [
						{
							id: segment.threadId,
							role: 'divider',
							content: segment.dividerContent
						},
						...segment.items
					]
		)
	)
	const threadNextCursor = $derived(historyCursor ?? null)

	/** Splits a page's own head boundary off its rows: a page that
	 * exhausts a thread carries its divider at the head, and segment
	 * order already says where that boundary sits. */
	function readPage(page: AgentThread): {
		rows: ThreadItem[]
		divider: string | undefined
	} {
		const head = page.items[0]
		if (head?.role === 'divider' && head.id === page.threadId) {
			return { rows: page.items.slice(1), divider: head.content }
		}
		return { rows: page.items, divider: undefined }
	}

	let isLoadingMore = $state(false)
	let loadMoreError = $state<string | null>(null)

	let activityFeed = $state<ActivityFeed | null>(null)
	let activityError = $state<string | null>(null)
	let isLoadingActivity = $state(false)

	// The id whose load last seeded the surface: a load invalidation
	// mid-session must not wipe loaded pages or the followed thread,
	// so only an agent switch (or an empty surface) re-seeds state.
	let loadedAgentId: string | undefined
	let seeded = false

	$effect(() => {
		if (loadedAgentId === data.agent.id && seeded) return
		loadedAgentId = data.agent.id
		seeded = true
		const page = data.thread
		if (page) {
			const { rows, divider } = readPage(page)
			segments = [
				{ threadId: page.threadId, items: rows, dividerContent: divider }
			]
		} else {
			segments = []
		}
		historyCursor = page?.nextCursor ?? null
		activeThreadId = page?.threadId
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
		const cursor = historyCursor
		if (cursor == null || isLoadingMore) return

		isLoadingMore = true
		loadMoreError = null

		try {
			const olderPage = await getAgentThread(data.agent.id, 20, cursor)
			const { rows, divider } = readPage(olderPage)
			// A thread owns exactly one segment: a page for a thread the
			// display already carries merges there (older rows above it,
			// the boundary arriving with the page that reaches its
			// start); an unknown thread's page prepends as its own.
			const existing = segments.find((s) => s.threadId === olderPage.threadId)
			if (existing) {
				const known = new Set(existing.items.map(threadRowKey))
				const above = rows.filter((m) => !known.has(threadRowKey(m)))
				const merged = {
					...existing,
					items: [...above, ...existing.items],
					dividerContent: divider ?? existing.dividerContent
				}
				segments = segments.map((s) => (s === existing ? merged : s))
			} else {
				segments = [
					{
						threadId: olderPage.threadId,
						items: rows,
						dividerContent: divider
					},
					...segments
				]
			}
			historyCursor = olderPage.nextCursor
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
	// refetch of the thread the turn ran in replaces that segment's
	// window (keeping its loaded older rows) and the frozen copies it
	// covers are dropped, reconciling by server ids so identical
	// prompt texts never collide. Refetches serialize — an older
	// response can never overwrite a newer snapshot — and a stored
	// error whose refetch failed retries on the next terminal event
	// instead of lingering as a live copy forever.
	let reconcileChain: Promise<void> = Promise.resolve()
	const pendingErrorRows = new Set<string>()
	// Reconciles already scheduled per turn: a message event and the
	// queue transition that follows it are one settlement, not two.
	const settledItemIds = new Set<string>()
	// The queue item currently claimed, tracked to detect the
	// transition that ends it, and the thread each item claimed in —
	// /new can switch the live thread while an older turn still runs,
	// and that turn's refresh must read the thread it ran in.
	let claimedItemId: string | null = null
	// The claimed item's kind: a command's artifact is its completion
	// event, so its transition schedules no refresh.
	let claimedItemKind: string | null = null
	const itemThreads = new Map<string, string | undefined>()

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
			// A command mints no user row: its work is the thread
			// switch, which arrives as command_completed.
			const active = event.data.active
			if (active && active.kind === 'prompt') {
				itemThreads.set(active.id, activeThreadId ?? segments.at(-1)?.threadId)
				if (active.user_message_id) {
					liveTurns.beginUser(active.user_message_id, active.content)
				}
			}
			// A turn that ends without a message or error — a quiet
			// stop — still needs its reconcile: the transition away
			// from the claimed item is the only signal. A command has
			// no stored rows: its artifact is the completion event.
			const previous = claimedItemId
			const previousKind = claimedItemKind
			claimedItemId = active?.id ?? null
			claimedItemKind = active?.kind ?? null
			if (previous && previous !== claimedItemId && previousKind === 'prompt') {
				queueReconcile(previous)
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
		if (event.type === 'command_completed') {
			// The durable switch committed: the boundary takes its place
			// at the bottom of the display, and the live tail's rows
			// freeze into the thread they ran in — above it. Commands are
			// serialized behind the turn that was running, so every live
			// row predates this boundary, and no chunk can arrive for a
			// frozen row afterwards.
			activeThreadId = event.data.thread_id
			successorThread.set(event.data.previous_thread_id, event.data.thread_id)
			// Nothing displayed yet: the first page supplies its own
			// boundary.
			if (segments.length === 0) return
			const last = segments[segments.length - 1]
			// A replayed event: the boundary is already in place.
			if (last.threadId === event.data.thread_id) return
			// Freeze the tail into the thread its rows belong to. A row
			// the segment already carries by key stays as it is: the
			// stored copy and the live copy are the same row, and the
			// settle refresh reconciles the content.
			const known = new Set(last.items.map(threadRowKey))
			const frozen = liveTurns.takeRows().filter((m) => {
				const key = threadRowKey(m)
				if (known.has(key)) return false
				known.add(key)
				return true
			})
			segments = [
				...segments.slice(0, -1),
				{ ...last, items: [...last.items, ...frozen] },
				{
					threadId: event.data.thread_id,
					items: [],
					dividerContent: event.data.divider.content
				}
			]
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
		if (settledItemIds.has(turnId)) {
			return
		}
		settledItemIds.add(turnId)
		// The thread is captured when the reconcile is scheduled: a
		// later /new switches the live thread, and this turn's rows
		// still belong to the one it ran in.
		const turnThread =
			itemThreads.get(turnId) ?? activeThreadId ?? segments.at(-1)?.threadId
		reconcileChain = reconcileChain.then(() =>
			reconcileTurn(turnId, turnThread)
		)
	}

	async function reconcileTurn(id: string, turnThread?: string) {
		// The stored errors this pass may settle; the rest retry.
		const settlingErrors = [...pendingErrorRows]
		try {
			const fresh = await getAgentThread(
				data.agent.id,
				20,
				turnThread ? `${turnThread}:` : undefined
			)
			const { rows, divider } = readPage(fresh)
			const freshKeys = new Set(fresh.items.map(threadRowKey))
			const target = segments.find((s) => s.threadId === fresh.threadId)
			if (target) {
				// Loaded older rows stay; this turn's frozen live copies
				// go — the answer keys on the turn id and tool rows on the
				// turn that ran them, while the stored rows carry ids in
				// their own namespace — and the fresh copies replace their
				// stale versions by key.
				target.items = [
					...target.items.filter(
						(m) =>
							m.id !== id && m.turnId !== id && !freshKeys.has(threadRowKey(m))
					),
					...rows
				]
				// The page that reaches a thread's start proves it has a
				// predecessor: its boundary belongs above the segment.
				target.dividerContent = target.dividerContent ?? divider
				segments = [...segments]
			} else {
				// A thread the display has not seen: slot it above its
				// successor when a command event named one, else at the
				// bottom — it is the newest thread the turn ran in.
				const segment: ThreadSegment = {
					threadId: fresh.threadId,
					items: rows,
					dividerContent: divider ?? 'New session'
				}
				const at = segments.findIndex(
					(s) => s.threadId === successorThread.get(fresh.threadId)
				)
				segments =
					at >= 0
						? [...segments.slice(0, at), segment, ...segments.slice(at)]
						: [...segments, segment]
			}
			liveTurns.settleTurn(
				id,
				fresh.items.filter((m) => m.role === 'user').map((m) => m.id),
				fresh.items.filter((m) => m.role === 'error').map((m) => m.id)
			)
			for (const errorId of settlingErrors) {
				if (freshKeys.has(`msg:${errorId}`)) pendingErrorRows.delete(errorId)
			}
		} catch {
			// Refetch failed: displayed content stays the truth, and the
			// next terminal event retries the pending stored errors.
		} finally {
			// Retire this turn's bookkeeping: neither the settled set nor
			// the thread map may grow for the life of the page.
			settledItemIds.delete(id)
			itemThreads.delete(id)
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
	{threadItems}
	liveItems={liveTurns.messages}
	{threadError}
	{threadNextCursor}
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
