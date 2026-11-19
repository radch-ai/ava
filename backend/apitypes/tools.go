package apitypes

// Tool is one installed tool: catalog metadata plus its global
// availability switch. The id is the stable identifier agent
// preferences reference. Kind is "builtin" or "mcp"; ServerID names
// the owning MCP server for kind="mcp".
type Tool struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Builtin     bool    `json:"builtin"`
	Enabled     bool    `json:"enabled"`
	Kind        string  `json:"kind"`
	ServerID    *string `json:"serverId,omitempty"`
}

// AgentTool is one installed tool as one agent sees it: GlobalEnabled
// is the global switch, AgentEnabled whether the agent holds a
// preference row -- presence is the whole grant.
type AgentTool struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	GlobalEnabled bool    `json:"globalEnabled"`
	AgentEnabled  bool    `json:"agentEnabled"`
	Kind          string  `json:"kind"`
	ServerID      *string `json:"serverId,omitempty"`
}

// GetToolsResponse is GET /api/tools' body: the whole catalog.
type GetToolsResponse struct {
	Tools []Tool `json:"tools"`
}

// AgentMcpPreference is one agent's parent gate over one MCP server
// (agent_mcp_preferences). Absent from the list = never approved --
// distinct from a configured off, which round-trips as
// AgentEnabled=false.
type AgentMcpPreference struct {
	ServerID     string `json:"serverId"`
	AgentEnabled bool   `json:"agentEnabled"`
}

// GetAgentToolsResponse is GET /api/agent/{id}/tools' body: the
// catalog as the agent sees it plus the MCP gate state the editor's
// server switches render from.
type GetAgentToolsResponse struct {
	Tools   []AgentTool          `json:"tools"`
	Servers []AgentMcpPreference `json:"servers"`
}

// ToggleToolRequest is PATCH /api/tools/{id}'s body — the switch's
// target state. Enabled is a pointer so an absent field is a 400,
// not a hidden false.
type ToggleToolRequest struct {
	Enabled *bool `json:"enabled"`
}

// ToggleAgentToolRequest is PATCH /api/agent/{id}/tools/{toolId}'s
// body, same pointer rule.
type ToggleAgentToolRequest struct {
	Enabled *bool `json:"enabled"`
}

// ToggleAgentMcpRequest is PATCH /api/agent/{id}/mcp/{serverId}'s body,
// same pointer rule as the tool toggles.
type ToggleAgentMcpRequest struct {
	Enabled *bool `json:"enabled"`
}
