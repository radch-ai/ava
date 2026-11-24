import type { McpToolDefinitionResponse } from '#lib/types/mcp'

/**
 * Shared tool-viewer types. The MCP & Tools page and the per-agent
 * editor both build these entries from their own data and hand them to
 * `ToolViewer.svelte`, which owns layout, grouping and collapse only.
 */

export interface ViewerRow {
	id: string
	name: string
	description?: string
	badges?: string[]
}

/** One switch: render value, busy/blocked state, and whether it dims. */
export interface ToggleState {
	checked: boolean
	disabled: boolean
	dimmed?: boolean
}

/** One status line: what the row is waiting for, not why it was toggled. */
export interface ViewerStatus {
	/** "paused" (catalog rejected), "globally-disabled" (a built-in's
	 *  global switch), "server-disabled" (the owning server's global
	 *  switch). The last two are managed elsewhere. */
	kind: 'paused' | 'globally-disabled' | 'server-disabled'
	text: string
	/** Where the blocker is managed; renders the text as a link. */
	manageUrl?: string
}

/**
 * The authority behind one MCP tool, shown beside its definition: the
 * wire response's own fields, so the two cannot drift.
 */
export type ViewerToolAuthority = Pick<
	McpToolDefinitionResponse,
	| 'endpoint'
	| 'command'
	| 'hasCredentials'
	| 'allowPlaintext'
	| 'allowPrivateNetwork'
>

/**
 * The stored definition of one MCP tool, fetched on demand: what the
 * tool actually advertises, which the row's name and description cannot
 * convey.
 */
export interface ViewerToolInspect {
	loading: boolean
	/** The sanitized definition JSON, or null while nothing is loaded. */
	json: string | null
	meta?: ViewerToolAuthority
	ontoggle: () => void
}

export interface ViewerToolEntry {
	row: ViewerRow
	state: ToggleState
	label: string
	ontoggle: () => void
	status?: ViewerStatus
	/** Present on MCP tools: the definition/authority disclosure. */
	inspect?: ViewerToolInspect
}

export interface ViewerManage {
	refreshing: boolean
	onrefresh: () => void
	onedit: () => void
	ondelete: () => void
}

export interface ViewerServerEntry {
	row: ViewerRow
	toolCount: number
	/** "http" | "stdio"; absent where the surface has no transport fact. */
	transport?: string
	lastError?: string | null
	/** Present when the server needs attention beyond its switch. */
	status?: ViewerStatus
	state: ToggleState
	label: string
	ontoggle: () => void
	tools: ViewerToolEntry[]
	/** Present only where server management belongs (the page). */
	manage?: ViewerManage
}

export type ViewerEntry =
	| { kind: 'tool'; entry: ViewerToolEntry }
	| { kind: 'server'; entry: ViewerServerEntry }

/**
 * Split a flat catalog into builtins and per-server buckets, preserving
 * order — the grouping both tool surfaces render (the page and the
 * per-agent editor otherwise duplicate this fold).
 */
export function groupTools<
	T extends { kind?: string; serverId?: string | null }
>(tools: T[]): { builtins: T[]; byServer: Map<string, T[]> } {
	const builtins: T[] = []
	const byServer = new Map<string, T[]>()
	for (const tool of tools) {
		if (tool.kind === 'mcp' && tool.serverId) {
			const list = byServer.get(tool.serverId) ?? []
			list.push(tool)
			byServer.set(tool.serverId, list)
		} else {
			builtins.push(tool)
		}
	}
	return { builtins, byServer }
}

/**
 * Per-surface policy for the shared entries builder: each surface keeps
 * its own badges, switch rules and statuses, and the builder owns the
 * one structure — builtins flat, then servers with their nested tools.
 */
export interface ViewerEntryHooks<
	TTool extends { kind?: string; serverId?: string | null },
	TServer
> {
	/** One tool's entry under the surface's dimming rule. */
	toolEntry: (tool: TTool, dimmed: boolean) => ViewerToolEntry
	/** A nested tool's dimming under its owning server. */
	toolDimmed: (tool: TTool, server: TServer) => boolean
	/** A builtin's dimming (false on the page; global-off in the editor). */
	builtinDimmed: (tool: TTool) => boolean
	/** One server's entry without its nested tools. */
	serverEntry: (server: TServer) => Omit<ViewerServerEntry, 'tools'>
	/** The id a server's nested tools key on — each surface names its own
	 * field (McpServer.id, AgentMcpPreference.serverId), so the builder
	 * never forces a fabricated copy. */
	serverIdOf: (server: TServer) => string
}

/**
 * Build the viewer's entries: builtins flat, then every server in the
 * listing's order with its tools nested. The hooks carry each surface's
 * policy; the structure is shared once.
 */
export function buildViewerEntries<
	TTool extends { kind?: string; serverId?: string | null },
	TServer
>(
	tools: TTool[],
	servers: TServer[],
	hooks: ViewerEntryHooks<TTool, TServer>
): ViewerEntry[] {
	const { builtins, byServer } = groupTools(tools)
	const rows: ViewerEntry[] = builtins.map((tool) => ({
		kind: 'tool' as const,
		entry: hooks.toolEntry(tool, hooks.builtinDimmed(tool))
	}))
	for (const server of servers) {
		const entry: ViewerServerEntry = {
			...hooks.serverEntry(server),
			tools: (byServer.get(hooks.serverIdOf(server)) ?? []).map((tool) =>
				hooks.toolEntry(tool, hooks.toolDimmed(tool, server))
			)
		}
		rows.push({ kind: 'server', entry })
	}
	return rows
}
