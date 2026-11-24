/**
 * MCP Types
 *
 * The wire contract lives in Go and is generated into
 * #lib/types/gen/apitypes.gen (gitignored) via `just typegen` —
 * regenerate, never edit.
 *
 * Secret values are write-only: requests accept headers and env maps, any
 * response lists key names only. Two transports: "http" (streamable HTTP)
 * and "stdio" (a local process ava spawns with the operator's privileges).
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
