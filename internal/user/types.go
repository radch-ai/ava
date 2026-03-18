// Package user handles agent configuration for users.
//
// Architecture: Handler → Service → Store (PostgreSQL)
//   - Handler: HTTP routing and request/response handling
//   - Service: Business logic and validation
//   - Store: Database operations via sqlc-generated code
//
// Agent Identity:
//   - Primary key: (user_id, name) composite key
//   - Duplicate names auto-suffixed: "agent" → "agent-2" → "agent-3"
//   - Name is stored as column, not in JSONB config
package user

// =============================================================================
// Config Types (stored in agents.config JSONB)
// =============================================================================

// SoulConfig defines the agent's personality and identity.
type SoulConfig struct {
	Personality string `json:"personality"`
	Identity    string `json:"identity"`
}

// HeartbeatConfig defines periodic check-in behavior for the agent.
type HeartbeatConfig struct {
	Model    string `json:"model"`    // LLM model to use
	Enabled  bool   `json:"enabled"`  // Whether heartbeat is active
	Duration string `json:"duration"` // Interval: "30m" | "1hr" | "2hr"
	Content  string `json:"content"`  // Prompt content for heartbeat
}

// CronJob defines a scheduled task for the agent.
type CronJob struct {
	ID             string `json:"id"`                       // UUID (server-generated)
	Time           string `json:"time"`                     // Execution time
	Frequency      string `json:"frequency"`                // "daily" | "weekly" | "custom"
	CronExpression string `json:"cronExpression,omitempty"` // Custom cron expression
	Prompt         string `json:"prompt"`                   // Task prompt
}

// =============================================================================
// Agent Configuration (full agent state)
// =============================================================================

// AgentConfiguration is the complete configuration for an agent.
// This is what GET/PATCH endpoints return/accept.
type AgentConfiguration struct {
	Name                  string           `json:"name"`                  // From agents.name column
	Icon                  string           `json:"icon"`                  // From agents.icon column
	Soul                  SoulConfig       `json:"soul"`                  // From config JSONB
	UserContext           string           `json:"userContext"`           // From config JSONB
	OperatingInstructions string           `json:"operatingInstructions"` // From config JSONB
	ToolsContext          string           `json:"toolsContext"`          // From config JSONB
	ToolsEnabled          []string         `json:"toolsEnabled"`          // From agent_tools junction
	Heartbeat             *HeartbeatConfig `json:"heartbeat,omitempty"`   // From heartbeat table
	CronJobs              []CronJob        `json:"cronJobs"`              // From cron_jobs table
}

// =============================================================================
// Request/Response Types
// =============================================================================

// PatchAgentRequest updates an existing agent. All fields are optional.
// Note: ToolsEnabled and CronJobs use "full replace" semantics (send entire list).
type PatchAgentRequest struct {
	Icon                  *string          `json:"icon,omitempty"`
	Soul                  *SoulConfig      `json:"soul,omitempty"`
	UserContext           *string          `json:"userContext,omitempty"`
	OperatingInstructions *string          `json:"operatingInstructions,omitempty"`
	ToolsContext          *string          `json:"toolsContext,omitempty"`
	ToolsEnabled          []string         `json:"toolsEnabled,omitempty"` // Full replace
	Heartbeat             *HeartbeatConfig `json:"heartbeat,omitempty"`
	CronJobs              []CronJob        `json:"cronJobs,omitempty"` // Full replace
}

// CreateAgentRequest creates a new agent. Name is set from URL path, not request body.
type CreateAgentRequest struct {
	Name                  string           `json:"-"` // Set from URL path, not request body
	Icon                  string           `json:"icon,omitempty"`
	Soul                  *SoulConfig      `json:"soul,omitempty"`
	UserContext           string           `json:"userContext,omitempty"`
	OperatingInstructions string           `json:"operatingInstructions,omitempty"`
	ToolsContext          string           `json:"toolsContext,omitempty"`
	ToolsEnabled          []string         `json:"toolsEnabled,omitempty"`
	Heartbeat             *HeartbeatConfig `json:"heartbeat,omitempty"`
	CronJobs              []CronJob        `json:"cronJobs,omitempty"`
}

// CreateAgentResponse returns the created agent's name.
// Name may differ from request if auto-suffixed due to duplicate.
type CreateAgentResponse struct {
	Name string `json:"name"`
}

// =============================================================================
// Agent List Types
// =============================================================================

// Agent represents a minimal agent for listing.
type Agent struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// AgentsResponse is the response for GET /user/agents.
type AgentsResponse struct {
	Agents []Agent `json:"agents"`
}
