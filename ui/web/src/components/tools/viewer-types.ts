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

export interface ViewerToolEntry {
	row: ViewerRow
	state: ToggleState
	label: string
	ontoggle: () => void
}

export interface ViewerManage {
	refreshing: boolean
	onrefresh: () => void
	onedit: () => void
	ondelete: () => void
}

export interface ViewerServerEntry {
	row: ViewerRow
	transport: string
	toolCount: number
	lastError?: string | null
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
