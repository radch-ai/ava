<script lang="ts">
	import { SvelteMap, SvelteSet } from 'svelte/reactivity'
	import { Switch } from '#components/ui/switch'
	import { getAgentTools, toggleAgentTool } from '#lib/services/agent/config'
	import { FIELD_CONFIGS } from '#lib/types/agent'
	import type { AgentTool } from '#lib/types/tool'
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
	let isLoading = $state(false)
	let loadError = $state<string | null>(null)
	let fetchAgent: string | undefined

	$effect(() => {
		const id = agentId
		fetchAgent = id
		tools = []
		loadError = null
		if (!id) return
		isLoading = true
		getAgentTools(id)
			.then((rows) => {
				if (fetchAgent !== id) return
				tools = rows
				saved = new SvelteMap(rows.map((row) => [row.id, row.agentEnabled]))
			})
			.catch((error: unknown) => {
				if (fetchAgent !== id) return
				loadError =
					error instanceof Error ? error.message : 'Failed to load tools'
			})
			.finally(() => {
				if (fetchAgent === id) isLoading = false
			})
	})

	// Saved is the persisted grant per row; target holds a desired
	// flip while its request is debounced or in flight. The switch
	// renders target when set, so rapid clicking feels instant.
	let saved = new SvelteMap<string, boolean>()
	let target = new SvelteMap<string, boolean>()
	let flushing = new SvelteSet<string>()
	let actionError = $state<string | null>(null)
	// Bursts collapse into one request per row: the dispatch waits out
	// the debounce, and a click that lands mid-flight re-schedules
	// with the latest target instead of being dropped.
	const DEBOUNCE_MS = 250
	const timers = new SvelteMap<string, ReturnType<typeof setTimeout>>()

	// Timer teardown on unmount: a pending target flushes instead of
	// dying with its timer, so a click right before the panel closes
	// still saves.
	$effect(() => {
		return () => {
			for (const timer of timers.values()) clearTimeout(timer)
			timers.clear()
			for (const [id, want] of target) {
				if (want === saved.get(id)) continue
				const tool = tools.find((row) => row.id === id)
				if (tool) void flush(tool)
			}
		}
	})

	function displayed(tool: AgentTool): boolean {
		return target.get(tool.id) ?? saved.get(tool.id) ?? tool.agentEnabled
	}

	function toggle(tool: AgentTool) {
		target.set(tool.id, !displayed(tool))
		if (!timers.has(tool.id)) {
			timers.set(
				tool.id,
				setTimeout(() => {
					timers.delete(tool.id)
					void flush(tool)
				}, DEBOUNCE_MS)
			)
		}
	}

	async function flush(tool: AgentTool) {
		if (flushing.has(tool.id) || !agentId) return
		const want = target.get(tool.id)
		if (want === undefined || want === saved.get(tool.id)) {
			target.delete(tool.id)
			return
		}
		const id = agentId
		flushing.add(tool.id)
		try {
			const row = await toggleAgentTool(id, tool.id, { enabled: want })
			if (fetchAgent === id) {
				saved.set(tool.id, row.agentEnabled)
				if (target.get(tool.id) === row.agentEnabled) target.delete(tool.id)
				actionError = null
			}
		} catch (e) {
			// Back to the saved state; only the failure surfaces.
			if (fetchAgent === id) {
				target.delete(tool.id)
				actionError = e instanceof Error ? e.message : 'Failed to update tool'
			}
		} finally {
			flushing.delete(tool.id)
			if (target.has(tool.id)) {
				timers.set(
					tool.id,
					setTimeout(() => {
						timers.delete(tool.id)
						void flush(tool)
					}, DEBOUNCE_MS)
				)
			}
		}
	}
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
			<div class="divide-y rounded-lg border">
				{#each tools as tool (tool.id)}
					<div
						class="hover:bg-muted/40 flex items-center justify-between gap-4 p-4 transition-colors {tool.globalEnabled
							? ''
							: 'opacity-60'}"
					>
						<div
							class="min-w-0 flex-1 space-y-1.5 {tool.globalEnabled
								? ''
								: 'blur-[0.6px]'}"
						>
							<div class="flex items-center gap-2">
								<h4 class="text-sm font-semibold">{tool.name}</h4>
								{#if !tool.globalEnabled}
									<span
										class="bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs font-medium"
									>
										Disabled globally
									</span>
								{/if}
							</div>
							{#if tool.description}
								<p class="text-muted-foreground truncate text-sm">
									{tool.description}
								</p>
							{/if}
						</div>
						<Switch
							id="agent-tool-{tool.id}"
							checked={tool.globalEnabled ? displayed(tool) : false}
							disabled={!tool.globalEnabled || flushing.has(tool.id)}
							onCheckedChange={() => toggle(tool)}
							aria-label="Enable {tool.name} for this agent"
						/>
					</div>
				{:else}
					<p class="text-muted-foreground p-4 text-sm">No tools available.</p>
				{/each}
			</div>
		</section>
	{/if}
</div>
