/**
 * Tool Types
 *
 * The wire contract lives in Go and is generated into
 * #lib/types/gen/apitypes.gen (gitignored) via `just typegen` —
 * regenerate, never edit.
 *
 * Tools are installed globally (MCP & Tools): `enabled` is the global
 * switch. An agent inherits every enabled tool unless it holds an explicit
 * opt-out (`locallyDisabled`) for that catalog id.
 */

export type {
	AgentMcpPreference,
	AgentTool,
	GetAgentToolsResponse,
	GetToolsResponse,
	ToggleAgentMcpRequest,
	ToggleAgentToolRequest,
	ToggleToolRequest,
	Tool
} from '#lib/types/gen/apitypes.gen'
