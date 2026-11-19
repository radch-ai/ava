/**
 * MCP Types
 *
 * The wire contract lives in Go and is generated into
 * #lib/types/gen/apitypes.gen (gitignored) via `just typegen` —
 * regenerate, never edit.
 *
 * Secret values are write-only: requests accept env/headers maps, any
 * response lists key names only.
 */

export type {
	CreateMcpServerRequest,
	GetMcpServersResponse,
	McpDiscoveredTool,
	McpServer,
	McpServerWithToolsResponse,
	UpdateMcpServerRequest
} from '#lib/types/gen/apitypes.gen'
