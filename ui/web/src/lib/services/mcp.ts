/**
 * MCP server service — the /api/mcp/servers endpoints behind the MCP &
 * Tools settings page. Creates and connection-affecting edits verify
 * the server before saving; secret values are never returned.
 */

import type {
	CreateMcpServerRequest,
	GetMcpServersResponse,
	McpServer,
	McpServerWithToolsResponse,
	UpdateMcpServerRequest
} from '#lib/types/mcp'
import { api } from './api'

/** The installed servers with tool counts and diagnostics. */
export async function getMcpServers(): Promise<McpServer[]> {
	const { servers } = await api.get('mcp/servers').json<GetMcpServersResponse>()
	return servers
}

/** Install a server: verify-before-save, then discovery. */
export async function createMcpServer(
	input: CreateMcpServerRequest
): Promise<McpServerWithToolsResponse> {
	return api
		.post('mcp/servers', { json: input })
		.json<McpServerWithToolsResponse>()
}

/** Edit a server; absent fields keep their stored values. */
export async function updateMcpServer(
	id: string,
	patch: UpdateMcpServerRequest
): Promise<McpServerWithToolsResponse> {
	return api
		.patch(`mcp/servers/${id}`, { json: patch })
		.json<McpServerWithToolsResponse>()
}

/** Remove a server; its tools and their grants cascade away. */
export async function deleteMcpServer(id: string): Promise<void> {
	await api.delete(`mcp/servers/${id}`)
}

/** Flip one server's global switch; disabling closes its live client. */
export async function setMcpServerEnabled(
	id: string,
	enabled: boolean
): Promise<McpServerWithToolsResponse> {
	return api
		.patch(`mcp/servers/${id}`, { json: { enabled } })
		.json<McpServerWithToolsResponse>()
}

/** Reconnect and re-discover one server's tools. */
export async function refreshMcpServer(
	id: string
): Promise<McpServerWithToolsResponse> {
	return api
		.post(`mcp/servers/${id}/refresh`)
		.json<McpServerWithToolsResponse>()
}
