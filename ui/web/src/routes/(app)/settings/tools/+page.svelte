<script lang="ts">
	import { SvelteMap, SvelteSet } from 'svelte/reactivity'
	import ToolViewer from '#components/tools/ToolViewer.svelte'
	import type {
		ViewerEntry,
		ViewerServerEntry,
		ViewerToolEntry
	} from '#components/tools/viewer-types'
	import { groupTools } from '#components/tools/viewer-types'
	import * as AlertDialog from '#components/ui/alert-dialog'
	import { Button } from '#components/ui/button'
	import {
		deleteMcpServer,
		refreshMcpServer,
		setMcpServerEnabled
	} from '#lib/services/mcp'
	import { toggleTool } from '#lib/services/tools'
	import type { McpServer } from '#lib/types/mcp'
	import type { Tool } from '#lib/types/tool'
	import { errorMessage } from '#lib/utils/errors'
	import { createToggleQueue } from '#lib/utils/toggle-queue.svelte'
	import { invalidateAll } from '$app/navigation'
	import type { PageData } from './$types'
	import McpServerForm from './McpServerForm.svelte'

	let { data }: { data: PageData } = $props()

	let saved = new SvelteMap<string, boolean>()
	let saveError = $state<string | null>(null)

	// Tool switches: queued per row through the shared queue; the saved
	// map holds each row's confirmed value until fresh data wins.
	const queue = createToggleQueue<Tool>({
		apply: async (tool, enabled) => {
			const confirmed = await toggleTool(tool.id, { enabled })
			saved.set(tool.id, confirmed.enabled)
			return confirmed.enabled
		},
		persisted: (tool) => saved.get(tool.id) ?? tool.enabled,
		rowFor: (id) => data.tools.find((tool) => tool.id === id),
		onError: (message) => (saveError = message)
	})

	// Server switches: the global kill switch, one queue over servers.
	let savedServers = new SvelteMap<string, boolean>()
	let serverError = $state<string | null>(null)
	const serverQueue = createToggleQueue<McpServer>({
		apply: async (server, enabled) => {
			const { server: confirmed } = await setMcpServerEnabled(
				server.id,
				enabled
			)
			savedServers.set(server.id, confirmed.enabled)
			return confirmed.enabled
		},
		persisted: (server) => savedServers.get(server.id) ?? server.enabled,
		rowFor: (id) => data.servers.find((server) => server.id === id),
		onError: (message) => (serverError = message)
	})

	// A pending target flushes on unmount instead of dying with its
	// timer, so a click right before navigating away still saves.
	$effect(() => () => queue.teardown())
	$effect(() => () => serverQueue.teardown())

	// Fresh data prunes confirmed rows that no longer exist — a deleted
	// tool cannot leave a stale override behind. Queued targets for
	// vanished rows are dropped by the queue itself (no row resolves).
	$effect(() => {
		const liveTools = new Set(data.tools.map((tool) => tool.id))
		for (const id of [...saved.keys()]) if (!liveTools.has(id)) saved.delete(id)
		queue.prune((id) => liveTools.has(id))
		const liveServers = new Set(data.servers.map((server) => server.id))
		for (const id of [...savedServers.keys()])
			if (!liveServers.has(id)) savedServers.delete(id)
		serverQueue.prune((id) => liveServers.has(id))
	})

	// One server form open at a time: null = list only, 'new' = blank
	// form, otherwise the server id being edited.
	let editing = $state<'new' | string | null>(null)
	let actionError = $state<string | null>(null)
	let refreshing = new SvelteSet<string>()
	// Deleting holds the server awaiting confirmation; the dialog's
	// open state is separate so Cancel/overlay close both clear.
	let deleting = $state<McpServer | null>(null)
	let confirmDelete = $state(false)

	function onsaved(): void {
		editing = null
		void invalidateAll()
	}

	async function refresh(server: McpServer): Promise<void> {
		actionError = null
		refreshing.add(server.id)
		try {
			await refreshMcpServer(server.id)
			void invalidateAll()
		} catch (e) {
			actionError = errorMessage(e, 'Failed to refresh server')
		} finally {
			refreshing.delete(server.id)
		}
	}

	async function remove(server: McpServer): Promise<void> {
		actionError = null
		try {
			await deleteMcpServer(server.id)
			deleting = null
			confirmDelete = false
			void invalidateAll()
		} catch (e) {
			actionError = errorMessage(e, 'Failed to delete server')
		}
	}

	function toolEntry(tool: Tool, dimmed = false): ViewerToolEntry {
		return {
			row: {
				id: tool.id,
				name: tool.name,
				description: tool.description,
				badges: tool.kind === 'mcp' ? [] : ['Built-in']
			},
			state: {
				checked: queue.displayed(tool),
				// A server switched off blocks its tools' global switches.
				disabled: dimmed || queue.flushing.has(tool.id),
				dimmed
			},
			label: `Enable ${tool.name} globally`,
			ontoggle: () => queue.toggle(tool)
		}
	}

	// One unified list: builtins flat, then every server (its tools
	// nested) in creation order. A server switch off dims and blocks its
	// tools; the manage buttons live on the server header.
	const entries = $derived.by((): ViewerEntry[] => {
		const { builtins, byServer } = groupTools(data.tools)

		const rows: ViewerEntry[] = builtins.map((tool) => ({
			kind: 'tool',
			entry: toolEntry(tool)
		}))

		for (const server of data.servers) {
			const on = serverQueue.displayed(server)
			const entry: ViewerServerEntry = {
				row: { id: server.id, name: server.name },
				transport: server.transport,
				toolCount: server.toolCount,
				lastError: server.lastError,
				state: {
					checked: on,
					disabled: serverQueue.flushing.has(server.id)
				},
				label: `Enable ${server.name} globally`,
				ontoggle: () => serverQueue.toggle(server),
				tools: (byServer.get(server.id) ?? []).map((tool) =>
					toolEntry(tool, !on)
				),
				manage: {
					refreshing: refreshing.has(server.id),
					onrefresh: () => void refresh(server),
					onedit: () => (editing = server.id),
					ondelete: () => {
						deleting = server
						confirmDelete = true
					}
				}
			}
			rows.push({ kind: 'server', entry })
		}
		return rows
	})
</script>

<svelte:head>
	<title>MCP &amp; Tools — ava</title>
</svelte:head>

<div class="mx-auto max-w-[var(--content-max-width)] space-y-6 px-6 py-8">
	<header class="space-y-3 border-b pb-4">
		<p class="text-muted-foreground text-sm">
			Tools your agents can use. Disabling a tool globally blocks access for all
			agents without changing their preferences. Re-enabling it restores access
			for agents that have it enabled.
		</p>
		<div class="flex items-center justify-between gap-4">
			<p class="text-muted-foreground text-xs">
				Only built-in tools are granted to agents by default — enable others per
				agent in its Tools panel.
			</p>
			{#if editing !== 'new'}
				<Button onclick={() => (editing = 'new')}>Add MCP server</Button>
			{/if}
		</div>
	</header>

	{#if saveError}
		<p class="bg-destructive/10 text-destructive rounded-lg p-4">{saveError}</p>
	{/if}
	{#if serverError}
		<p class="bg-destructive/10 text-destructive rounded-lg p-4">
			{serverError}
		</p>
	{/if}
	{#if actionError}
		<p class="bg-destructive/10 text-destructive rounded-lg p-4">
			{actionError}
		</p>
	{/if}

	{#if editing === 'new'}
		<McpServerForm {onsaved} oncancel={() => (editing = null)} />
	{/if}

	{#if data.serversError}
		<p class="bg-destructive/10 text-destructive rounded-lg p-4">
			{data.serversError}
		</p>
	{:else}
		{#each data.servers as server (server.id)}
			{#if editing === server.id}
				<div class="rounded-lg border p-4">
					<McpServerForm {server} {onsaved} oncancel={() => (editing = null)} />
				</div>
			{/if}
		{/each}
	{/if}

	{#if data.toolsError}
		<p class="bg-destructive/10 text-destructive rounded-lg p-4">
			{data.toolsError}
		</p>
	{:else}
		<ToolViewer {entries} />
	{/if}
</div>

<AlertDialog.Root bind:open={confirmDelete}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete {deleting?.name}?</AlertDialog.Title>
			<AlertDialog.Description>
				Its tools and every agent's grants for them are removed with it. This
				cannot be undone.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel onclick={() => (deleting = null)}
				>Cancel</AlertDialog.Cancel
			>
			<AlertDialog.Action onclick={() => deleting && void remove(deleting)}>
				Delete server
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
