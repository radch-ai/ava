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

// AgentTool is one installed tool as one agent sees it: GlobalEnabled is
// the global switch, LocallyDisabled whether this agent opted out of it.
// Availability is the two switches plus the owning server's gate and
// health; only an explicit opt-out is agent-local state.
type AgentTool struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	GlobalEnabled   bool    `json:"globalEnabled"`
	LocallyDisabled bool    `json:"locallyDisabled"`
	Kind            string  `json:"kind"`
	ServerID        *string `json:"serverId,omitempty"`
}

// GetToolsResponse is GET /api/tools' body: the whole catalog.
type GetToolsResponse struct {
	Tools []Tool `json:"tools"`
}

// AgentMcpPreference is one agent's parent gate over one MCP server
// (agent_mcp_preferences). AgentEnabled=false means off, whether the row
// is absent or stored disabled: the gate decides only whether the agent
// uses that server's globally enabled tools. Name, ServerEnabled and
// CatalogState carry the facts the editor renders its server rows and
// global blockers from, so it explains availability without a second
// fetch. No endpoint, credential or approval facts are exposed here.
type AgentMcpPreference struct {
	ServerID      string `json:"serverId"`
	Name          string `json:"name"`
	AgentEnabled  bool   `json:"agentEnabled"`
	ServerEnabled bool   `json:"serverEnabled"`
	CatalogState  string `json:"catalogState"`
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
