<script lang="ts">
	import { SvelteMap } from 'svelte/reactivity'
	import ToolViewer from '#components/tools/ToolViewer.svelte'
	import type {
		ViewerStatus,
		ViewerToolEntry
	} from '#components/tools/viewer-types'
	import {
		buildViewerEntries,
		groupTools
	} from '#components/tools/viewer-types'
	import { Button } from '#components/ui/button'
	import {
		getAgentTools,
		toggleAgentMcp,
		toggleAgentTool
	} from '#lib/services/agent/config'
	import { FIELD_CONFIGS } from '#lib/types/agent'
	import type { AgentMcpPreference, AgentTool } from '#lib/types/tool'
	import { errorMessage } from '#lib/utils/errors'
	import { createToggleQueue } from '#lib/utils/toggle-queue.svelte'
	import { resolve } from '$app/paths'
	import ConfigTextField from './ConfigTextField.svelte'

	// Undefined agentId = the agent is not created yet (the new-agent
	// page): the list has nothing to fetch, only the placeholder.
	interface Props {
		agentId?: string
		value: string
		onchange: (value: string) => void
	}

	let { agentId, value, onchange }: Props = $props()

	// The tool list refetches on every panel open: global switches and
	// server state change on the MCP & Tools page, so a cache would need
	// invalidation from there for a handful of rows.
	let tools = $state<AgentTool[]>([])
	// The listing's server rows carry everything this panel renders
	// about an owning server — the gate state rides the row — so no
	// second fetch or parallel gate map exists.
	let servers = $state<AgentMcpPreference[]>([])
	let isLoading = $state(false)
	let loadError = $state<string | null>(null)
	let fetchAgent: string | undefined

	// Where every global blocker is managed: one page owns global
	// switches, server state and definition inspection.
	const manageUrl = resolve('/(app)/settings/tools')

	function adoptListing(
		rows: AgentTool[],
		gateRows: AgentMcpPreference[]
	): void {
		tools = rows
		servers = gateRows
		// The switch means "available to this agent": inherited unless the
		// agent holds an explicit opt-out.
		saved.clear()
		for (const row of rows) {
			saved.set(row.id, !row.locallyDisabled)
		}
		queue.prune((rowId) => rows.some((row) => row.id === rowId))
		serverQueue.prune((serverId) =>
			gateRows.some((server) => server.serverId === serverId)
		)
	}

	$effect(() => {
		const id = agentId
		fetchAgent = id
		tools = []
		servers = []
		loadError = null
		if (!id) return
		isLoading = true
		getAgentTools(id)
			.then((listing) => {
				if (fetchAgent !== id) return
				adoptListing(listing.tools, listing.servers)
			})
			.catch((error: unknown) => {
				if (fetchAgent !== id) return
				loadError = errorMessage(error, 'Failed to load tools')
			})
			.finally(() => {
				if (fetchAgent === id) isLoading = false
			})
	})

	// Saved is the persisted availability per row; the shared queue holds
	// a desired flip while its request is debounced or in flight.
	let saved = new SvelteMap<string, boolean>()
	let actionError = $state<string | null>(null)

	const queue = createToggleQueue<AgentTool>({
		apply: async (tool, enabled) => {
			if (!agentId) return !tool.locallyDisabled
			const id = agentId
			const listing = await toggleAgentTool(id, tool.id, { enabled })
			const row = listing.tools.find((item) => item.id === tool.id) ?? tool
			if (fetchAgent === id) saved.set(tool.id, !row.locallyDisabled)
			return !row.locallyDisabled
		},
		persisted: (tool) => saved.get(tool.id) ?? !tool.locallyDisabled,
		rowFor: (rowId) => tools.find((row) => row.id === rowId),
		onError: (message) => (actionError = message)
	})

	// Server gates: the gate decides whether this agent uses the server's
	// globally enabled tools; it never seeds or revokes a per-tool choice.
	// The response is the refreshed listing, so one round-trip re-renders
	// everything. The queue keys rows by id, and a preference is
	// identified by its serverId, so the adapter is a pure view over
	// the stored listing — no second state to reset.
	const serverQueue = createToggleQueue<{
		id: string
		server: AgentMcpPreference
	}>({
		apply: async ({ server }, enabled) => {
			if (!agentId) return false
			const id = agentId
			const listing = await toggleAgentMcp(id, server.serverId, { enabled })
			if (fetchAgent !== id) return enabled
			adoptListing(listing.tools, listing.servers)
			const confirmed = listing.servers.find(
				(gate) => gate.serverId === server.serverId
			)
			return confirmed?.agentEnabled ?? enabled
		},
		persisted: ({ server }) => server.agentEnabled,
		rowFor: (serverId) =>
			servers
				.map((server) => ({ id: server.serverId, server }))
				.find((row) => row.id === serverId),
		onError: (message) => (actionError = message)
	})

	// A pending target flushes when the panel closes instead of dying
	// with its timer.
	$effect(() => () => queue.teardown())
	$effect(() => () => serverQueue.teardown())

	// A global blocker is not the agent's choice: it renders as a label
	// with the page that owns the switch.
	function serverStatus(server: AgentMcpPreference): ViewerStatus | undefined {
		if (!server.serverEnabled) {
			return {
				kind: 'server-disabled',
				text: 'Server disabled globally — Manage in MCP & Tools',
				manageUrl
			}
		}
		if (server.catalogState === 'paused') {
			return {
				kind: 'paused',
				text: 'Unavailable globally — Manage in MCP & Tools',
				manageUrl
			}
		}
		return undefined
	}

	function toolStatus(
		tool: AgentTool,
		server: AgentMcpPreference | undefined
	): ViewerStatus | undefined {
		// A globally off tool carries its own switch as the blocker, even
		// under a healthy server: the server row explains the server.
		if (!tool.globalEnabled) {
			return {
				kind: 'globally-disabled',
				text: 'Disabled globally — Manage in MCP & Tools',
				manageUrl
			}
		}
		if (server) return serverStatus(server)
		return undefined
	}

	function toolEntry(
		tool: AgentTool,
		server: AgentMcpPreference | undefined,
		dimmed: boolean
	): ViewerToolEntry {
		return {
			row: {
				id: tool.id,
				name: tool.name,
				description: tool.description,
				badges: tool.globalEnabled ? [] : ['Disabled globally']
			},
			state: {
				// A blocked tool renders off: the agent's own switch cannot
				// overcome a global one.
				checked: dimmed ? false : queue.displayed(tool),
				disabled: dimmed || queue.flushing.has(tool.id),
				dimmed
			},
			label: `Enable ${tool.name} for this agent`,
			ontoggle: () => queue.toggle(tool),
			status: toolStatus(tool, server)
		}
	}

	// Same list as the MCP & Tools page, compact, through the shared
	// builder: no manage buttons here — a server's connection is edited
	// globally, not per agent.
	// A server disabled globally, or a paused catalog, is inert here too:
	// the gate renders off and inert (its saved state is untouched), so
	// the agent page can never show a dead server as active.
	function serverGlobally(server: AgentMcpPreference): boolean {
		return server.serverEnabled && server.catalogState === 'active'
	}

	// Nested tool counts come from the shared grouping (the entries
	// builder re-groups internally for its own structure); owning-server
	// lookups key a server-id map, keyed by the preference's serverId.
	const grouped = $derived(groupTools(tools))
	const serverById = $derived(
		new Map(servers.map((row) => [row.serverId, row]))
	)
	function toolCountFor(server: AgentMcpPreference): number {
		return grouped.byServer.get(server.serverId)?.length ?? 0
	}

	const entries = $derived(
		buildViewerEntries(tools, servers, {
			serverIdOf: (server) => server.serverId,
			toolEntry: (tool, dimmed) =>
				toolEntry(tool, serverById.get(tool.serverId ?? ''), dimmed),
			toolDimmed: (tool, server) => {
				const gateOn = serverQueue.displayed({ id: server.serverId, server })
				return !gateOn || !tool.globalEnabled || !serverGlobally(server)
			},
			builtinDimmed: (tool) => !tool.globalEnabled,
			serverEntry: (server) => {
				const gateOn = serverQueue.displayed({ id: server.serverId, server })
				return {
					row: {
						id: server.serverId,
						name: server.name,
						badges: server.serverEnabled ? [] : ['Disabled globally']
					},
					toolCount: toolCountFor(server),
					status: serverStatus(server),
					state: {
						checked: gateOn && serverGlobally(server),
						disabled:
							!serverGlobally(server) ||
							serverQueue.flushing.has(server.serverId),
						dimmed: !serverGlobally(server)
					},
					label: `Enable ${server.name} for this agent`,
					ontoggle: () => serverQueue.toggle({ id: server.serverId, server })
				}
			}
		})
	)
</script>

<div class="space-y-6">
	<ConfigTextField {value} config={FIELD_CONFIGS.toolsContext} {onchange} />

	{#if agentId === undefined}
		<p class="text-muted-foreground text-sm">
			Create the agent first, then manage tools
		</p>
	{:else if isLoading}
		<p class="text-muted-foreground text-sm">Loading tools…</p>
	{:else if loadError}
		<p class="text-destructive text-sm">{loadError}</p>
	{:else}
		<section class="space-y-2">
			<div class="flex items-center justify-between gap-4">
				<p class="text-muted-foreground text-sm">
					Tool access saves immediately — it is not part of Save/Cancel.
				</p>
				<Button
					variant="link"
					href={manageUrl}
					class="h-auto shrink-0 p-0 text-sm"
				>
					Manage MCP &amp; Tools
				</Button>
			</div>
			{#if actionError}
				<p class="text-destructive text-sm">{actionError}</p>
			{/if}
			<ToolViewer {entries} compact />
		</section>
	{/if}
</div>
