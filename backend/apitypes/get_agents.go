package apitypes

import "github.com/radch-ai/ava/backend/internal/db"

// GetAgentsResponse is GET /api/agents' body. Agents carries the
// sqlc rows directly: GetAgentsRow matches AgentIdentity
// field-for-field, but Go can't convert between the named slice
// types — the match is pinned by TestGetAgentsRowMatchesIdentity.
type GetAgentsResponse struct {
	Agents []db.GetAgentsRow `json:"agents" tstype:"AgentIdentity[]"`
}
