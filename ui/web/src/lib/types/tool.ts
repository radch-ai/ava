/**
 * Tool Types
 *
 * The wire contract lives in Go and is generated into
 * #lib/types/gen/apitypes.gen (gitignored) via `just typegen` —
 * regenerate, never edit.
 *
 * Tools are installed globally (MCP & Tools); agents hold only a
 * per-agent preference referencing the catalog id.
 */

export type {
	AgentTool,
	GetAgentToolsResponse,
	GetToolsResponse,
	ToggleAgentToolRequest,
	ToggleToolRequest,
	Tool
} from '#lib/types/gen/apitypes.gen'
