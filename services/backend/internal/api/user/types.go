// Package user handles user agent list types.
package user

import "time"

// AgentSummary represents a minimal agent in the list response.
type AgentSummary struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// GetAgentsResponse is the response for GET /user/agents.
type GetAgentsResponse struct {
	Agents []AgentSummary `json:"agents"`
}

// ThreadMessage represents a single message in the thread.
type ThreadMessage struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

// ThreadPage represents a paginated response of thread messages.
type ThreadPage struct {
	Items      []ThreadMessage `json:"items"`
	NextCursor *string         `json:"nextCursor"`
}
