<script lang="ts">
	import { ChevronRight, Pencil, RefreshCw, Trash2 } from '@lucide/svelte'
	import { Button } from '#components/ui/button'
	import {
		Collapsible,
		CollapsibleContent,
		CollapsibleTrigger
	} from '#components/ui/collapsible'
	import { Switch } from '#components/ui/switch'
	import { cn } from '#lib/utils.js'

	import type {
		ViewerEntry,
		ViewerStatus,
		ViewerToolEntry
	} from './viewer-types'

	/**
	 * Shared tool list: builtins as flat switch rows, MCP servers as
	 * collapsible parents whose nested tools carry their own switches.
	 * Both surfaces render this component — the MCP & Tools page (global
	 * switches, manage buttons) and the per-agent editor (compact,
	 * per-agent opt-outs) — so grouping and collapse behavior cannot
	 * drift.
	 */

	interface Props {
		entries: ViewerEntry[]
		/** Agent-editor size variant: same DOM, tighter spacing. */
		compact?: boolean
	}

	let { entries, compact = false }: Props = $props()

	/** The authority line beside a definition: where a call would go. */
	function authorityLine(tool: ViewerToolEntry): string {
		const meta = tool.inspect?.meta
		if (!meta) return ''
		const parts = [
			meta.endpoint,
			`credentials: ${meta.hasCredentials ? 'yes' : 'no'}`,
			`plaintext: ${meta.allowPlaintext ? 'yes' : 'no'}`,
			`private network: ${meta.allowPrivateNetwork ? 'yes' : 'no'}`
		]
		return parts.filter((part) => part !== '').join(' · ')
	}
</script>

{#snippet statusLine(status: ViewerStatus)}
	<p
		class="text-xs {status.kind === 'paused'
			? 'text-amber-600'
			: 'text-muted-foreground'}"
	>
		{#if status.manageUrl}
			<a
				href={status.manageUrl}
				class="text-primary underline-offset-4 hover:underline"
			>
				{status.text}
			</a>
		{:else}
			{status.text}
		{/if}
	</p>
{/snippet}

{#snippet definitionPanel(tool: ViewerToolEntry)}
	{#if tool.inspect}
		{@const inspect = tool.inspect}
		<div class="space-y-1.5">
			<button
				type="button"
				class="text-primary text-xs underline-offset-4 hover:underline"
				aria-expanded={inspect.json !== null}
				onclick={() => inspect.ontoggle()}
			>
				{inspect.loading
					? `Loading definition of ${tool.row.name}…`
					: inspect.json === null
						? `Show definition of ${tool.row.name}`
						: `Hide definition of ${tool.row.name}`}
			</button>
			{#if inspect.json !== null}
				{#if inspect.meta}
					<p class="text-muted-foreground font-mono text-xs break-all">
						{authorityLine(tool)}
					</p>
				{/if}
				<pre
					class="bg-muted/40 max-h-64 overflow-auto rounded-md p-2 font-mono text-xs break-all whitespace-pre-wrap"
				>{inspect.json}</pre>
			{/if}
		</div>
	{/if}
{/snippet}

<div class="divide-y rounded-lg border">
	{#each entries as item (item.kind === 'tool'
		? item.entry.row.id
		: item.entry.row.id)}
		{#if item.kind === 'tool'}
			{@const tool = item.entry}
			<div
				class="hover:bg-muted/40 flex items-center justify-between gap-4 transition-colors {compact
					? 'px-3 py-2.5'
					: 'p-4'} {tool.state.dimmed ? 'opacity-60' : ''}"
			>
				<div class="min-w-0 flex-1 space-y-1.5">
					<div class="flex flex-wrap items-center gap-2">
						<h3 class="text-sm font-semibold">{tool.row.name}</h3>
						{#each tool.row.badges ?? [] as badge (badge)}
							<span
								class="bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs font-medium"
							>
								{badge}
							</span>
						{/each}
					</div>
					{#if tool.row.description}
						<p class="text-muted-foreground truncate text-sm">
							{tool.row.description}
						</p>
					{/if}
					{#if tool.status}
						{@render statusLine(tool.status)}
					{/if}
					{@render definitionPanel(tool)}
				</div>
				<Switch
					id="tool-{tool.row.id}"
					aria-label={tool.label}
					checked={tool.state.checked}
					disabled={tool.state.disabled}
					onCheckedChange={() => tool.ontoggle()}
				/>
			</div>
		{:else}
			{@const server = item.entry}
			<Collapsible>
				<div
					class="hover:bg-muted/40 flex items-center justify-between gap-4 transition-colors {compact
						? 'px-3 py-2.5'
						: 'p-4'}"
				>
					<div class="min-w-0 flex-1 space-y-1.5">
						<div class="flex flex-wrap items-center gap-2">
							<CollapsibleTrigger
								class="group flex items-center gap-2 text-left"
							>
								{#if server.state.checked}
									<ChevronRight
										class="text-muted-foreground size-4 transition-transform group-data-[state=open]:rotate-90"
									/>
								{/if}
								<h3 class="text-sm font-semibold">{server.row.name}</h3>
							</CollapsibleTrigger>
							<span
								class="bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs font-medium"
							>
								MCP
							</span>
							<span class="text-muted-foreground text-xs">
								{server.toolCount}
								{server.toolCount === 1 ? 'tool' : 'tools'}
							</span>
							{#each server.row.badges ?? [] as badge (badge)}
								<span
									class="bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs font-medium"
								>
									{badge}
								</span>
							{/each}
						</div>
						{#if server.status}
							{@render statusLine(server.status)}
						{/if}
						{#if server.lastError}
							<p
								class="text-destructive truncate text-xs"
								title={server.lastError}
							>
								{server.lastError}
							</p>
						{/if}
					</div>
					<div class="flex shrink-0 items-center gap-1">
						{#if server.manage}
							<Button
								variant="ghost"
								size="icon"
								title="Refresh tools"
								disabled={server.manage.refreshing}
								onclick={() => server.manage?.onrefresh()}
							>
								<RefreshCw
									class={server.manage.refreshing ? 'animate-spin' : ''}
								/>
							</Button>
							<Button
								variant="ghost"
								size="icon"
								title="Edit server"
								onclick={() => server.manage?.onedit()}
							>
								<Pencil />
							</Button>
							<Button
								variant="ghost"
								size="icon"
								title="Delete server"
								onclick={() => server.manage?.ondelete()}
							>
								<Trash2 class="text-destructive" />
							</Button>
						{/if}
						<Switch
							id="server-{server.row.id}"
							aria-label={server.label}
							checked={server.state.checked}
							disabled={server.state.disabled}
							onCheckedChange={() => server.ontoggle()}
						/>
					</div>
				</div>
				{#if server.state.checked}
					<CollapsibleContent>
						<div class="bg-muted/20 divide-y border-t">
							{#each server.tools as tool (tool.row.id)}
								<div
									class={cn(
										'hover:bg-muted/40 flex items-center justify-between gap-4 transition-colors',
										compact ? 'py-2 pr-3 pl-8' : 'py-3 pr-4 pl-10',
										tool.state.dimmed && 'opacity-60'
									)}
								>
									<div class="min-w-0 flex-1 space-y-1">
										<div class="flex flex-wrap items-center gap-2">
											<h4 class="text-sm font-medium">{tool.row.name}</h4>
											{#each tool.row.badges ?? [] as badge (badge)}
												<span
													class="bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs font-medium"
												>
													{badge}
												</span>
											{/each}
										</div>
										{#if tool.row.description}
											<p class="text-muted-foreground truncate text-xs">
												{tool.row.description}
											</p>
										{/if}
										{#if tool.status}
											{@render statusLine(tool.status)}
										{/if}
										{@render definitionPanel(tool)}
									</div>
									<Switch
										id="nested-tool-{tool.row.id}"
										aria-label={tool.label}
										checked={tool.state.checked}
										disabled={tool.state.disabled}
										onCheckedChange={() => tool.ontoggle()}
									/>
								</div>
							{:else}
								<p class="text-muted-foreground px-8 py-3 text-sm">
									No tools discovered yet — refresh this server.
								</p>
							{/each}
						</div>
					</CollapsibleContent>
				{/if}
			</Collapsible>
		{/if}
	{:else}
		<p class="text-muted-foreground p-4 text-sm">No tools available.</p>
	{/each}
</div>
