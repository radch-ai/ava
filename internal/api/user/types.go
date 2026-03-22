// Package user handles user agent list types.
package user

// AgentSummary represents a minimal agent in the list response.
type AgentSummary struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// GetAgentsResponse is the response for GET /user/agents.
type GetAgentsResponse struct {
	Agents []AgentSummary `json:"agents"`
}
