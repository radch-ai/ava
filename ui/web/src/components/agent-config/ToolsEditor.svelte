<script lang="ts">
	import { SvelteMap } from 'svelte/reactivity'
	import ToolViewer from '#components/tools/ToolViewer.svelte'
	import type {
		ViewerEntry,
		ViewerServerEntry,
		ViewerToolEntry
	} from '#components/tools/viewer-types'
	import { groupTools } from '#components/tools/viewer-types'
	import {
		getAgentTools,
		toggleAgentMcp,
		toggleAgentTool
	} from '#lib/services/agent/config'
	import { getMcpServers } from '#lib/services/mcp'
	import { FIELD_CONFIGS } from '#lib/types/agent'
	import type { McpServer } from '#lib/types/mcp'
	import type { AgentTool } from '#lib/types/tool'
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

	// The tool list refetches on every panel open: global switches
	// change on the MCP & Tools page, so a cache would need
	// invalidation from there for a handful of rows.
	let tools = $state<AgentTool[]>([])
	let servers = $state<McpServer[]>([])
	let gates = new SvelteMap<string, boolean>()
	let isLoading = $state(false)
	let loadError = $state<string | null>(null)
	let fetchAgent: string | undefined

	function adoptListing(
		rows: AgentTool[],
		gateRows: { serverId: string; agentEnabled: boolean }[]
	): void {
		tools = rows
		saved = new SvelteMap(rows.map((row) => [row.id, row.agentEnabled]))
		gates = new SvelteMap(
			gateRows.map((gate) => [gate.serverId, gate.agentEnabled])
		)
		queue.prune((rowId) => rows.some((row) => row.id === rowId))
		serverQueue.prune((serverId) =>
			servers.some((server) => server.id === serverId)
		)
	}

	$effect(() => {
		const id = agentId
		fetchAgent = id
		tools = []
		servers = []
		gates = new SvelteMap()
		loadError = null
		if (!id) return
		isLoading = true
		Promise.all([
			getAgentTools(id),
			getMcpServers().catch(() => [] as McpServer[])
		])
			.then(([listing, serverRows]) => {
				if (fetchAgent !== id) return
				servers = serverRows
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

	// Saved is the persisted grant per row; the shared queue holds a
	// desired flip while its request is debounced or in flight.
	let saved = new SvelteMap<string, boolean>()
	let actionError = $state<string | null>(null)

	const queue = createToggleQueue<AgentTool>({
		apply: async (tool, enabled) => {
			if (!agentId) return tool.agentEnabled
			const id = agentId
			const row = await toggleAgentTool(id, tool.id, { enabled })
			if (fetchAgent === id) saved.set(tool.id, row.agentEnabled)
			return row.agentEnabled
		},
		persisted: (tool) => saved.get(tool.id) ?? tool.agentEnabled,
		rowFor: (rowId) => tools.find((row) => row.id === rowId),
		onError: (message) => (actionError = message)
	})

	// Server gates: the first enable approves the server's current
	// tools; later toggles keep every per-tool choice. The response is
	// the refreshed listing, so one round-trip re-renders everything.
	const serverQueue = createToggleQueue<McpServer>({
		apply: async (server, enabled) => {
			if (!agentId) return false
			const id = agentId
			const listing = await toggleAgentMcp(id, server.id, { enabled })
			if (fetchAgent !== id) return enabled
			adoptListing(listing.tools, listing.servers)
			const confirmed = listing.servers.find(
				(gate) => gate.serverId === server.id
			)
			return confirmed?.agentEnabled ?? enabled
		},
		persisted: (server) => gates.get(server.id) ?? false,
		rowFor: (serverId) => servers.find((server) => server.id === serverId),
		onError: (message) => (actionError = message)
	})

	// A pending target flushes when the panel closes instead of dying
	// with its timer.
	$effect(() => () => queue.teardown())
	$effect(() => () => serverQueue.teardown())

	function toolEntry(
		tool: AgentTool,
		dimmed: boolean,
		disabled: boolean
	): ViewerToolEntry {
		return {
			row: {
				id: tool.id,
				name: tool.name,
				description: tool.description,
				badges: tool.globalEnabled ? [] : ['Disabled globally']
			},
			state: {
				// A globally disabled tool renders off regardless of the
				// grant; the switch, not the grant, gates use.
				checked: tool.globalEnabled ? queue.displayed(tool) : false,
				disabled: disabled || queue.flushing.has(tool.id),
				dimmed
			},
			label: `Enable ${tool.name} for this agent`,
			ontoggle: () => queue.toggle(tool)
		}
	}

	// Same list as the MCP & Tools page, compact: builtins flat, then
	// servers with their tools nested. No manage buttons here — a
	// server's connection is edited globally, not per agent.
	const entries = $derived.by((): ViewerEntry[] => {
		const { builtins, byServer } = groupTools(tools)

		const rows: ViewerEntry[] = builtins.map((tool) => ({
			kind: 'tool',
			entry: toolEntry(tool, !tool.globalEnabled, !tool.globalEnabled)
		}))

		for (const server of servers) {
			const gateOn = serverQueue.displayed(server)
			// A server disabled globally is inert here too — the gate
			// renders off and inert (its saved state is untouched), so
			// the agent page can never show a dead server as active.
			const serverGloballyOn = server.enabled
			const entry: ViewerServerEntry = {
				row: {
					id: server.id,
					name: server.name,
					badges: serverGloballyOn ? [] : ['Disabled globally']
				},
				transport: server.transport,
				toolCount: server.toolCount,
				lastError: server.lastError,
				state: {
					checked: gateOn && serverGloballyOn,
					disabled: !serverGloballyOn || serverQueue.flushing.has(server.id),
					dimmed: !serverGloballyOn
				},
				label: `Enable ${server.name} for this agent`,
				ontoggle: () => serverQueue.toggle(server),
				tools: (byServer.get(server.id) ?? []).map((tool) =>
					toolEntry(
						tool,
						!gateOn || !tool.globalEnabled || !serverGloballyOn,
						!gateOn || !tool.globalEnabled || !serverGloballyOn
					)
				)
			}
			rows.push({ kind: 'server', entry })
		}
		return rows
	})
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
				<a
					href={resolve('/(app)/settings/tools')}
					class="text-primary shrink-0 text-sm underline-offset-4 hover:underline"
				>
					Manage MCP &amp; Tools
				</a>
			</div>
			{#if actionError}
				<p class="text-destructive text-sm">{actionError}</p>
			{/if}
			<ToolViewer {entries} compact />
		</section>
	{/if}
</div>
