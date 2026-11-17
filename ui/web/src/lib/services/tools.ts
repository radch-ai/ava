/**
 * Tool catalog service — the /api/tools endpoints behind the MCP &
 * Tools settings page.
 */

import type { GetToolsResponse, ToggleToolRequest, Tool } from '#lib/types/tool'
import { api } from './api'

/** The installed catalog: metadata plus each tool's global switch. */
export async function getTools(): Promise<Tool[]> {
	const { tools } = await api.get('tools').json<GetToolsResponse>()
	return tools
}

/** Flip one tool's global switch; answers the saved record. */
export async function toggleTool(
	id: string,
	body: ToggleToolRequest
): Promise<Tool> {
	return api.patch(`tools/${id}`, { json: body }).json<Tool>()
}
