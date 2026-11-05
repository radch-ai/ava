package apitypes

// CreateAgentRequest is POST /api/agent's body: identity and config
// fields at one level; only the column fields persist — extras are
// ignored.
type CreateAgentRequest struct {
	Name                  string     `json:"name"`
	Icon                  string     `json:"icon"`
	Soul                  SoulConfig `json:"soul"`
	UserContext           *string    `json:"userContext,omitempty"`
	OperatingInstructions string     `json:"operatingInstructions"`
	ToolsContext          string     `json:"toolsContext"`
}

// CreateAgentResponse is POST /api/agent's 201 body: just the id —
// the UI navigates from it alone.
type CreateAgentResponse struct {
	ID string `json:"id"`
}
