<script lang="ts">
	import { SvelteMap, SvelteSet } from 'svelte/reactivity'
	import { Button } from '#components/ui/button'
	import { Switch } from '#components/ui/switch'
	import { toggleTool } from '#lib/services/tools'
	import type { Tool } from '#lib/types/tool'
	import type { PageData } from './$types'

	let { data }: { data: PageData } = $props()

	// Saved holds each row's confirmed state — written only by a
	// successful save. Target holds a desired flip while its request
	// is debounced or in flight; the switch renders target when set,
	// so rapid clicking feels instant.
	let saved = new SvelteMap<string, boolean>()
	let target = new SvelteMap<string, boolean>()
	let flushing = new SvelteSet<string>()
	let saveError = $state<string | null>(null)
	// Bursts collapse into one request per row: the dispatch waits out
	// the debounce, and a click that lands mid-flight re-schedules
	// with the latest target instead of being dropped.
	const DEBOUNCE_MS = 250
	const timers = new Map<string, ReturnType<typeof setTimeout>>()

	// Timer teardown on unmount: a pending target flushes instead of
	// dying with its timer, so a click right before navigating away
	// still saves.
	$effect(() => {
		return () => {
			for (const timer of timers.values()) clearTimeout(timer)
			timers.clear()
			for (const [id, want] of target) {
				if (want === persisted(id)) continue
				void flush(id)
			}
		}
	})

	// persisted is the row's last confirmed state: the load's own
	// value until a save confirms a new one, so a burst that returns
	// to the server's state is a no-op at every stage.
	function persisted(id: string): boolean {
		const confirmed = saved.get(id)
		if (confirmed !== undefined) return confirmed
		return data.tools.find((tool) => tool.id === id)?.enabled ?? false
	}

	function displayed(tool: Tool): boolean {
		return target.get(tool.id) ?? persisted(tool.id)
	}

	function toggle(tool: Tool) {
		target.set(tool.id, !displayed(tool))
		if (!timers.has(tool.id)) {
			timers.set(
				tool.id,
				setTimeout(() => {
					timers.delete(tool.id)
					void flush(tool.id)
				}, DEBOUNCE_MS)
			)
		}
	}

	async function flush(id: string) {
		if (flushing.has(id)) return
		const want = target.get(id)
		if (want === undefined || want === persisted(id)) {
			target.delete(id)
			return
		}
		flushing.add(id)
		try {
			const tool = await toggleTool(id, { enabled: want })
			saved.set(id, tool.enabled)
			if (target.get(id) === tool.enabled) target.delete(id)
			saveError = null
		} catch (e) {
			// Back to the saved state; only the failure surfaces.
			target.delete(id)
			saveError = e instanceof Error ? e.message : 'Failed to update tool'
		} finally {
			flushing.delete(id)
			if (target.has(id)) {
				timers.set(
					id,
					setTimeout(() => {
						timers.delete(id)
						void flush(id)
					}, DEBOUNCE_MS)
				)
			}
		}
	}
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
				Adding, editing, and removing custom tools and MCP connections isn't
				available yet.
			</p>
			<Button disabled title="Adding tools isn't available yet"
				>Add tool</Button
			>
		</div>
	</header>

	{#if saveError}
		<p class="bg-destructive/10 text-destructive rounded-lg p-4">{saveError}</p>
	{/if}

	{#if data.toolsError}
		<p class="bg-destructive/10 text-destructive rounded-lg p-4">
			{data.toolsError}
		</p>
	{:else}
		<section class="divide-y rounded-lg border">
			{#each data.tools as tool (tool.id)}
				<div
					class="hover:bg-muted/40 flex items-center justify-between gap-4 p-4 transition-colors"
				>
					<div class="min-w-0 flex-1 space-y-1.5">
						<div class="flex items-center gap-2">
							<h3 class="font-semibold">{tool.name}</h3>
							{#if tool.builtin}
								<span
									class="bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs font-medium"
								>
									Built-in
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
						id="tool-{tool.id}"
						checked={displayed(tool)}
						disabled={flushing.has(tool.id)}
						onCheckedChange={() => toggle(tool)}
						aria-label="Enable {tool.name} globally"
					/>
				</div>
			{:else}
				<p class="text-muted-foreground p-4 text-sm">No tools available.</p>
			{/each}
		</section>
	{/if}
</div>
