package apitypes

// Tool is one installed tool: catalog metadata plus its global
// availability switch. The id is the stable identifier agent
// preferences reference.
type Tool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
	Enabled     bool   `json:"enabled"`
}

// AgentTool is one installed tool as one agent sees it: GlobalEnabled
// is the global switch, AgentEnabled whether the agent holds a
// preference row -- presence is the whole grant.
type AgentTool struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	GlobalEnabled bool   `json:"globalEnabled"`
	AgentEnabled  bool   `json:"agentEnabled"`
}

// GetToolsResponse is GET /api/tools' body: the whole catalog.
type GetToolsResponse struct {
	Tools []Tool `json:"tools"`
}

// GetAgentToolsResponse is GET /api/agent/{id}/tools' body.
type GetAgentToolsResponse struct {
	Tools []AgentTool `json:"tools"`
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
