<script lang="ts">
	import { SvelteMap, SvelteSet } from 'svelte/reactivity'
	import ToolViewer from '#components/tools/ToolViewer.svelte'
	import type {
		ViewerStatus,
		ViewerToolEntry,
		ViewerToolInspect
	} from '#components/tools/viewer-types'
	import { buildViewerEntries } from '#components/tools/viewer-types'
	import * as AlertDialog from '#components/ui/alert-dialog'
	import { Button } from '#components/ui/button'
	import {
		deleteMcpServer,
		getMcpToolDefinition,
		refreshMcpServer,
		updateMcpServer
	} from '#lib/services/mcp'
	import { toggleTool } from '#lib/services/tools'
	import type {
		McpServer,
		McpServerWithToolsResponse,
		McpSkippedTool,
		McpToolDefinitionResponse
	} from '#lib/types/mcp'
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
			const { server: confirmed } = await updateMcpServer(server.id, {
				enabled
			})
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

	// Tools a discovery pass reported but could not represent: visible
	// reasons, never silent drops. Cleared on dismiss and replaced by the
	// next save or refresh.
	let skipped = $state<McpSkippedTool[] | null>(null)

	function onsaved(result?: McpServerWithToolsResponse): void {
		editing = null
		skipped = result?.skippedTools ?? null
		clearDefinitions()
		void invalidateAll()
	}

	// Definition inspection: this page owns every global fact, so the
	// agent editor stays switches and blocker labels. A re-listing
	// invalidates every cached definition — a shown definition must never
	// outlive the discovery pass that produced it.
	let openDefinitions = new SvelteMap<string, boolean>()
	let definitions = new SvelteMap<string, McpToolDefinitionResponse>()
	let loadingDefinitions = new SvelteSet<string>()
	let definitionError = $state<string | null>(null)
	// Bumped by every clear: a fetch that started before the clear
	// must not repopulate the cache when it resolves after it.
	let definitionGeneration = 0

	function clearDefinitions(): void {
		definitionGeneration++
		openDefinitions.clear()
		definitions.clear()
	}

	async function toggleDefinition(tool: Tool, serverId: string) {
		if (openDefinitions.get(tool.id)) {
			openDefinitions.delete(tool.id)
			return
		}
		openDefinitions.set(tool.id, true)
		if (definitions.has(tool.id)) return
		definitionError = null
		loadingDefinitions.add(tool.id)
		const generation = definitionGeneration
		try {
			const definition = await getMcpToolDefinition(serverId, tool.id)
			// A clear ran while this fetch was in flight: the result
			// belongs to a dead listing — drop it.
			if (generation !== definitionGeneration) return
			definitions.set(tool.id, definition)
		} catch (e) {
			// Same for a stale failure: it cannot reopen banners for
			// a listing that no longer exists.
			if (generation !== definitionGeneration) return
			openDefinitions.delete(tool.id)
			definitionError = errorMessage(e, 'Failed to load the definition')
		} finally {
			loadingDefinitions.delete(tool.id)
		}
	}

	function inspectFor(tool: Tool, serverId: string): ViewerToolInspect {
		const definition = definitions.get(tool.id)
		return {
			loading: loadingDefinitions.has(tool.id),
			json: openDefinitions.get(tool.id)
				? (definition?.definitionJson ?? null)
				: null,
			meta: definition
				? {
						endpoint: definition.endpoint ?? '',
						hasCredentials: definition.hasCredentials,
						allowPlaintext: definition.allowPlaintext,
						allowPrivateNetwork: definition.allowPrivateNetwork
					}
				: undefined,
			ontoggle: () => void toggleDefinition(tool, serverId)
		}
	}

	async function refresh(server: McpServer): Promise<void> {
		actionError = null
		// The new pass replaces the old one's banners: a failed
		// refresh must not leave the previous run's skips or
		// definition error on screen.
		skipped = null
		definitionError = null
		refreshing.add(server.id)
		try {
			const result = await refreshMcpServer(server.id)
			skipped = result.skippedTools ?? null
		} catch (e) {
			actionError = errorMessage(e, 'Failed to refresh server')
		} finally {
			refreshing.delete(server.id)
			// A failed refresh still writes diagnostics — and may have
			// paused the catalog — so the row is refetched either way.
			clearDefinitions()
			void invalidateAll()
		}
	}

	async function remove(server: McpServer): Promise<void> {
		actionError = null
		// The deleted server cannot leave its skips behind either.
		skipped = null
		try {
			await deleteMcpServer(server.id)
			deleting = null
			confirmDelete = false
			void invalidateAll()
		} catch (e) {
			actionError = errorMessage(e, 'Failed to delete server')
		}
	}

	// A server's catalog state is a policy fact the switch cannot
	// express: a paused catalog resolves nothing no matter how the switch
	// reads.
	function serverStatus(server: McpServer): ViewerStatus | undefined {
		if (server.catalogState === 'paused') {
			return {
				kind: 'paused',
				text: 'Catalog paused after a rejected refresh — refresh to retry'
			}
		}
		return undefined
	}

	function toolEntry(
		tool: Tool,
		serverId: string | undefined,
		dimmed = false
	): ViewerToolEntry {
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
			ontoggle: () => queue.toggle(tool),
			inspect: serverId ? inspectFor(tool, serverId) : undefined
		}
	}

	// One unified list: builtins flat, then every server (its tools
	// nested) in creation order. A server switch off dims and blocks its
	// tools; the manage buttons live on the server header. The structure
	// is the shared builder's; this page owns only its policy.
	// The builder keys nested tools by each surface's own id accessor;
	// this page hands the original McpServer rows straight through.
	const entries = $derived(
		buildViewerEntries(data.tools, data.servers, {
			serverIdOf: (server) => server.id,
			toolEntry: (tool, dimmed) =>
				toolEntry(tool, tool.serverId ?? undefined, dimmed),
			toolDimmed: (_tool, server) => {
				const on = serverQueue.displayed(server)
				return !on || server.catalogState === 'paused'
			},
			builtinDimmed: () => false,
			serverEntry: (server) => {
				const on = serverQueue.displayed(server)
				return {
					row: { id: server.id, name: server.name },
					toolCount: server.toolCount,
					lastError: server.lastError,
					status: serverStatus(server),
					state: {
						checked: on,
						disabled: serverQueue.flushing.has(server.id)
					},
					label: `Enable ${server.name} globally`,
					ontoggle: () => serverQueue.toggle(server),
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
			}
		})
	)
</script>

<svelte:head>
	<title>MCP &amp; Tools — ava</title>
</svelte:head>

<div class="mx-auto max-w-[var(--content-max-width)] space-y-6 px-6 py-8">
	<header class="space-y-3 border-b pb-4">
		<p class="text-muted-foreground text-sm">
			Tools your agents can use. Disabling a tool globally blocks access for
			every agent, whatever its own switch says. Enabling a tool makes it
			available to every agent using that server, except agents that disabled
			it.
		</p>
		<div class="flex items-center justify-between gap-4">
			<p class="text-muted-foreground text-xs">
				Built-in tools are enabled by default; a newly discovered MCP tool
				starts off. Each agent can opt out of an enabled tool in its Tools
				panel.
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
	{#if definitionError}
		<p class="bg-destructive/10 text-destructive rounded-lg p-4">
			{definitionError}
		</p>
	{/if}

	{#if skipped?.length}
		<div
			class="space-y-1 rounded-lg border border-amber-500/40 bg-amber-500/10 p-4 text-sm"
		>
			<div class="flex items-start justify-between gap-4">
				<p class="font-medium">
					{skipped.length}
					{skipped.length === 1 ? 'tool was' : 'tools were'}
					skipped
				</p>
				<Button
					variant="link"
					class="text-muted-foreground h-auto p-0 text-xs"
					aria-label="Dismiss skipped tools"
					onclick={() => (skipped = null)}
				>
					×
				</Button>
			</div>
			<ul class="text-muted-foreground space-y-0.5 text-xs">
				{#each skipped as tool (tool.name)}
					<li>{tool.name} — {tool.reason}</li>
				{/each}
			</ul>
		</div>
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
				Its tools, and every agent's selection of those tools, are removed with
				it. This cannot be undone.
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
