/**
 * MCP Types
 *
 * The wire contract lives in Go and is generated into
 * #lib/types/gen/apitypes.gen (gitignored) via `just typegen` —
 * regenerate, never edit.
 *
 * Secret values are write-only: requests accept a headers map, any
 * response lists key names only. Servers connect over streamable HTTP
 * only: there is no command/argv/env field to send.
 */

export type {
	CreateMcpServerRequest,
	GetMcpServersResponse,
	McpDiscoveredTool,
	McpServer,
	McpServerWithToolsResponse,
	McpSkippedTool,
	McpToolDefinitionResponse,
	UpdateMcpServerRequest
} from '#lib/types/gen/apitypes.gen'
