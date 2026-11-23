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
	McpToolDefinitionResponse,
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

/** Remove a server; its tools and their agent opt-outs cascade away. */
export async function deleteMcpServer(id: string): Promise<void> {
	await api.delete(`mcp/servers/${id}`)
}

/** Reconnect and re-discover one server's tools. */
export async function refreshMcpServer(
	id: string
): Promise<McpServerWithToolsResponse> {
	return api
		.post(`mcp/servers/${id}/refresh`)
		.json<McpServerWithToolsResponse>()
}

/**
 * One catalog tool's stored definition plus the authority behind it:
 * the endpoint, and whether the connection carries credentials or rides
 * plaintext.
 */
export async function getMcpToolDefinition(
	serverId: string,
	toolId: string
): Promise<McpToolDefinitionResponse> {
	return api
		.get(`mcp/servers/${serverId}/tools/${toolId}/definition`)
		.json<McpToolDefinitionResponse>()
}
