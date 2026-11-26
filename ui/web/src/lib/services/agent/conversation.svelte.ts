/**
 * Conversation state: the whole projection one agent's surface
 * renders — stored thread segments, the live tail, the queue, the
 * lane, and the socket that feeds them. A surface owns one store per
 * agent and replaces it whole on switch, so no projection field is
 * ever reset by hand.
 */

import type { ModelSelection } from '#lib/models'
import type { ThreadItem } from '#lib/types/agent'
import type {
	LaneStatus,
	QueueActiveItem,
	QueueItem,
	QueueStateEvent,
	SyncReadyEvent
} from '#lib/types/gen/wstypes.gen'
import { parseLifecycleEvent, type ValidatedLifecycleEvent } from './lifecycle'
import { createLiveTurns } from './live-turn.svelte'
import {
	type AgentThread,
	getAgentThread,
	parseAgentThread,
	threadRowKey
} from './thread'
import {
	type AgentSocketEvent,
	type AgentSocketState,
	createAgentSocket
} from './ws'

/** One thread's rows, oldest first; a divider row at the head names
 * the boundary the thread starts below, when it has one. */
interface ThreadSegment {
	threadId: string
	items: ThreadItem[]
}

/** The first authoritative page — the page load's — plus its error
 * banner when the load failed. */
export interface ConversationSeed {
	thread: AgentThread | null
	threadError?: string | null
}

/** Creates the conversation store for one agent, seeded from its
 * first authoritative page. */
export function createConversation(agentId: string, seed: ConversationSeed) {
	// The display is a chain of thread segments, oldest first. Order is
	// arrival order, never a timestamp comparison: a thread's created_at
	// is DB wall-clock with no strict-order guarantee, so it cannot say
	// which boundary came first.
	let segments = $state<ThreadSegment[]>([])
	// The oldest-loaded continuation. Seeded from the first page and
	// written by Load more only: a refresh must never move the reader's
	// pagination depth, nor turn an exhausted cursor back into one.
	let historyCursor = $state<string | null>(null)

	let threadError = $state<string | null | undefined>(undefined)
	let isLoadingMore = $state(false)
	let loadMoreError = $state<string | null>(null)
	// Snapshot generation: applySnapshot replaces the projection
	// wholesale, so an in-flight loadMore that started against an older
	// one must not merge its page or touch the cursor when it lands.
	let snapshotEpoch = 0

	// The socket exists from creation: connecting is the first state,
	// and a live one is only ever declared by sync_ready.
	let socketState = $state<AgentSocketState>('connecting')
	// The lane's last broadcast, verbatim; the strip derives its queue
	// count from the queue projection below.
	let laneStatus = $state<LaneStatus | null>(null)
	const liveTurns = createLiveTurns()

	// The gateway-owned queue projection: full-replacement snapshots
	// only — the surface never schedules or mutates the queue itself.
	let queueItems = $state<QueueItem[]>([])
	let queuePaused = $state(false)
	let queueActiveItem = $state<QueueActiveItem | null>(null)
	// The send-now reservation: claimed-but-not-started work, pinned
	// above the waiting list until its claim snapshot lands.
	let queueNextItem = $state<QueueItem | null>(null)
	let pendingSendNowId = $state<string | null>(null)

	/** Waiting rows: the pending list plus the pinned reservation. The
	 * wire does not project a count, so the projection is the count. */
	function queueLen() {
		return queueItems.length + (queueNextItem ? 1 : 0)
	}

	/** Applies one authoritative queue snapshot. The projection is a full
	 * replacement: the gateway's list, pause gate, running item, and
	 * reservation are the queue's whole truth. */
	function applyQueueState(q: QueueStateEvent) {
		queueItems = q.items
		queuePaused = q.paused
		queueActiveItem = q.active ?? null
		queueNextItem = q.next ?? null
	}

	/** Seeds the display from one authoritative page — the page load's or
	 * a synchronization snapshot's. Replaces every cached segment and
	 * restarts pagination from the active thread; older history stays
	 * loadable. */
	function applySnapshot(page: AgentThread | null | undefined) {
		snapshotEpoch++
		segments = page ? [{ threadId: page.threadId, items: page.items }] : []
		historyCursor = page?.nextCursor ?? null
		// A committed baseline answers the conversation: a failed page load
		// never leaves its banner over live content.
		threadError = null
	}

	applySnapshot(seed.thread)
	// The seed's banner answers the page load — after the snapshot,
	// so the clear above cannot wipe it.
	threadError = seed.threadError

	/** Places committed rows in the segment of the thread they belong to,
	 * replacing any stored row with the same key. A thread the display has
	 * not seen appends at the bottom — its boundary, if it has one,
	 * arrives as a thread_changed event, never as a guess. Structure-only
	 * rows — an empty assistant record that carries just the round's
	 * tool-call structure — never place: the server projection excludes
	 * them, and the live path must converge on the same visibility. */
	function placeStoredRows(threadId: string, rows: ThreadItem[]) {
		const visible = rows.filter(
			(m) => m.role !== 'assistant' || m.content !== ''
		)
		if (visible.length === 0) return
		const keyed = new Set(visible.map(threadRowKey))
		const target = segments.find((s) => s.threadId === threadId)
		if (target) {
			target.items = [
				...target.items.filter((m) => !keyed.has(threadRowKey(m))),
				...visible
			]
			segments = [...segments]
			return
		}
		segments = [...segments, { threadId, items: visible }]
	}

	/** Reduces one batch of committed events, live or replayed. Each event
	 * carries the server identity of every row it commits, so the display
	 * never infers a thread or a row id from whichever turn is followed. */
	function applyLifecycleEvents(raw: unknown[]) {
		for (const payload of raw) {
			const event = parseLifecycleEvent(payload)
			if (event) {
				handleLifecycleEvent(event)
			}
		}
	}

	function handleLifecycleEvent(event: ValidatedLifecycleEvent) {
		switch (event.kind) {
			case 'item_started': {
				// A prompt's user row is committed with its start: it belongs
				// to the stored display immediately, and the live tail holds
				// only the answer being streamed.
				placeStoredRows(event.threadId, event.rows ?? [])
				break
			}
			case 'tool_started': {
				liveTurns.beginTool({
					invocationId: event.invocationId,
					turnId: event.itemId,
					name: event.toolName ?? '',
					args: event.toolArgs ?? ''
				})
				break
			}
			case 'tool_finished': {
				if (event.rows?.length && event.threadId) {
					// The outcome row is committed: it enters the stored
					// display and the live invocation line retires — the
					// journal carries it now.
					placeStoredRows(event.threadId, event.rows)
					liveTurns.retireTool(event.invocationId)
					break
				}
				// Journal-less mode: the live line carries the outcome.
				liveTurns.finishTool(
					event.invocationId,
					event.toolResult ?? '',
					event.toolError ?? null
				)
				break
			}
			case 'message_committed': {
				// A mid-turn round's rows are committed: they enter the
				// stored display, and the round's streamed copy retires —
				// the next round's first chunk recreates the row.
				placeStoredRows(event.threadId, event.rows ?? [])
				liveTurns.settleTurn(event.itemId, [], [])
				break
			}
			case 'item_finished': {
				if (event.outcome === 'cancelled') {
					// A cancellation mints no transcript row, so the notice is
					// display-only: it names the submission that never ran,
					// lands in the newest displayed thread, and replays once
					// per item under its own stable id.
					const into = segments.at(-1)?.threadId
					if (into) {
						placeStoredRows(into, [
							{
								id: `cancelled-${event.itemId}`,
								role: 'error',
								content: `cancelled (${event.reason ?? 'unknown'}): ${event.content ?? ''}`
							}
						])
					}
					break
				}
				// The terminal rows are committed: they replace the item's
				// live copies, and the tail drops what the stored display now
				// carries. No refetch — the rows ride the event.
				const rows = event.rows ?? []
				if (event.threadId) {
					placeStoredRows(event.threadId, rows)
				}
				liveTurns.settleTurn(
					event.itemId,
					rows.filter((m) => m.role === 'user').map((m) => m.id),
					rows.filter((m) => m.role === 'error').map((m) => m.id)
				)
				break
			}
			case 'thread_changed': {
				// The durable switch committed: the boundary takes its place at
				// the bottom of the display, and the live tail's rows freeze
				// into the thread they ran in — above it. Commands are
				// serialized behind the turn that was running, so every live
				// row predates this boundary.
				// Nothing displayed yet: the first page supplies its own
				// boundary.
				if (segments.length === 0) return
				const last = segments[segments.length - 1]
				// A replayed event: the boundary is already in place.
				if (last.threadId === event.threadId) return
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
						threadId: event.threadId,
						items: [
							event.divider ?? {
								id: event.threadId,
								role: 'divider',
								content: 'New session'
							}
						]
					}
				]
				// An itemless rotation (the pre-prompt automation) seeds
				// the successor's summary on this event — no
				// item_finished ever carries it — so the stored rows
				// land with the boundary they opened.
				placeStoredRows(event.threadId, event.rows ?? [])
				break
			}
			case 'conversation_refresh': {
				// A committed mid-loop rotation: the authoritative page
				// replaces the cached projection and its pagination
				// cursor wholesale. Only sync_ready replaces the live
				// tail — the running turn's continuation must survive
				// the refresh below the replaced segments.
				applySnapshot(event.page)
				break
			}
		}
	}

	/** Applies the synchronization hand-off: the authoritative runtime
	 * state replaces the queue projection and the live tail wholesale, so
	 * nothing from a previous connection survives as pending work. */
	function handleSyncReady(data: SyncReadyEvent) {
		applyQueueState(data.queue)
		const active = queueActiveItem
		pendingSendNowId = null
		laneStatus = data.lane
		const prefix = data.prefix
		if (!prefix) {
			liveTurns.replaceWith(undefined)
		} else {
			liveTurns.replaceWith({
				itemId: prefix.item_id,
				userRow:
					active?.user_message_id && active.kind === 'prompt'
						? { id: active.user_message_id, content: active.content }
						: undefined,
				text: prefix.text,
				invocations: prefix.invocations ?? []
			})
		}
		// The hand-off rebuilt the tail wholesale, so the transient
		// markers it replaced away are gone with it — and the gateway
		// broadcasts lane_status on change only, so no frame follows
		// to restore them. The snapshot re-feeds the lane's own
		// signals: mid-reasoning or mid-compaction reconnects
		// rehydrate their pulse rows.
		liveTurns.applyEvent({ type: 'lane_status', data: data.lane })
	}

	// Connection state from the socket: the handshake declares `live`
	// only once the committed conversation has been replayed. Anything
	// earlier means the strip is stale, so the lane readout clears and
	// the composer blocks submissions until readiness.
	function handleSocketState(next: AgentSocketState) {
		socketState = next
		if (next !== 'live') {
			laneStatus = null
		}
	}

	function handleSocketEvent(event: AgentSocketEvent) {
		liveTurns.applyEvent(event)
		if (event.type === 'queue_state') {
			applyQueueState(event.data)
		}
		if (event.type === 'lane_status') {
			// A status frame can trail a fresher queue snapshot; the
			// label derives its count from the projection, so a stale
			// broadcast cannot lower it.
			laneStatus = event.data
		}
		if (event.type === 'sync_snapshot') {
			// The authoritative baseline: cached segments and pagination
			// are replaced, and replay continues from the cursor it
			// carries.
			applySnapshot(parseAgentThread(event.data.page))
		}
		if (event.type === 'sync_ready') {
			handleSyncReady(event.data)
		}
		if (event.type === 'lifecycle' || event.type === 'sync_batch') {
			// Replay and live delivery are one feed: a replayed batch is
			// reduced exactly like a live one, so a device that missed
			// frames converges on the same projection.
			applyLifecycleEvents(event.data.events ?? [])
		}
	}

	// One socket per store, opened eagerly at creation: a page
	// (re)loaded mid-turn asks for lane status on connect, which joins
	// the lane and replays the streamed prefix — a lazy, first-submit
	// socket would miss that window. Reconnects happen inside.
	const socket = createAgentSocket(
		agentId,
		handleSocketEvent,
		handleSocketState
	)

	/** Prepends the next older page when the cursor still names one. */
	async function loadMore() {
		const cursor = historyCursor
		if (cursor == null || isLoadingMore) return
		const epoch = snapshotEpoch

		isLoadingMore = true
		loadMoreError = null

		try {
			const olderPage = await getAgentThread(agentId, 20, cursor)
			if (epoch !== snapshotEpoch) {
				// A newer snapshot replaced the projection while this
				// page was in flight: its segments and cursor stand.
				return
			}
			// A thread owns exactly one segment: a page for a thread the
			// display already carries merges there (older rows above it,
			// the boundary row arriving with the page that reaches its
			// start); an unknown thread's page prepends as its own.
			const existing = segments.find((s) => s.threadId === olderPage.threadId)
			if (existing) {
				const known = new Set(existing.items.map(threadRowKey))
				const above = olderPage.items.filter((m) => !known.has(threadRowKey(m)))
				const merged = { ...existing, items: [...above, ...existing.items] }
				segments = segments.map((s) => (s === existing ? merged : s))
			} else {
				segments = [
					{ threadId: olderPage.threadId, items: olderPage.items },
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

	/** Promotes one pending item to run next; the pending mark keeps
	 * every queue action disabled while the request is in flight. */
	async function sendNow(itemId: string, selection: ModelSelection) {
		if (pendingSendNowId) {
			return
		}
		pendingSendNowId = itemId
		try {
			// The current selection rides the frame: a paused queue
			// rebinds its failed-model entries to it.
			await socket.sendNow(itemId, selection)
		} catch {
			// Unconfirmed, not rejected: the next queue snapshot is the
			// truth; nothing to resubmit.
		} finally {
			pendingSendNowId = null
		}
	}

	return {
		get threadItems(): ThreadItem[] {
			// The flat projection the shell renders: each segment's rows
			// in segment order, boundary dividers included.
			return segments.flatMap((segment) => segment.items)
		},
		get liveItems(): ThreadItem[] {
			return liveTurns.messages
		},
		get threadError() {
			return threadError
		},
		get historyCursor() {
			return historyCursor
		},
		get isLoadingMore() {
			return isLoadingMore
		},
		get loadMoreError() {
			return loadMoreError
		},
		get socketState() {
			return socketState
		},
		get laneStatus() {
			return laneStatus
		},
		get queueItems() {
			return queueItems
		},
		get queuePaused() {
			return queuePaused
		},
		get queueActiveItem() {
			return queueActiveItem
		},
		get queueNextItem() {
			return queueNextItem
		},
		get pendingSendNowId() {
			return pendingSendNowId
		},
		queueLen,
		loadMore,
		sendPrompt(content: string, selection: ModelSelection) {
			return socket.sendPrompt(content, selection)
		},
		sendNow,
		interrupt() {
			socket.sendInterrupt()
		},
		/** Terminal: closes the lane's socket. The projection needs no
		 * reset — the owning surface drops the whole store. */
		dispose() {
			socket.close()
		}
	}
}
