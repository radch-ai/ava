package user

import "time"

// SoulConfig matches frontend SoulConfig interface
type SoulConfig struct {
	Name        string `json:"name"`
	Personality string `json:"personality"`
	Identity    string `json:"identity"`
}

// HeartbeatConfig matches frontend HeartbeatConfig interface
type HeartbeatConfig struct {
	Model    string `json:"model"`
	Enabled  bool   `json:"enabled"`
	Duration string `json:"duration"` // '30m' | '1hr' | '2hr'
	Content  string `json:"content"`
}

// CronJob matches frontend CronJob interface
type CronJob struct {
	ID             string `json:"id"`
	Time           string `json:"time"`
	Frequency      string `json:"frequency"` // 'daily' | 'weekly' | 'custom'
	CronExpression string `json:"cronExpression,omitempty"`
	Prompt         string `json:"prompt"`
}

// LylaConfiguration matches frontend LylaConfiguration interface
type LylaConfiguration struct {
	Soul                  SoulConfig       `json:"soul"`
	UserContext           string           `json:"userContext"`
	OperatingInstructions string           `json:"operatingInstructions"`
	ToolsContext          string           `json:"toolsContext"`
	ToolsEnabled          []string         `json:"toolsEnabled"`
	Heartbeat             *HeartbeatConfig `json:"heartbeat,omitempty"`
	CronJobs              []CronJob        `json:"cronJobs"`
}

// WorkerConfiguration matches frontend WorkerConfiguration interface
type WorkerConfiguration struct {
	Soul                  SoulConfig       `json:"soul"`
	OperatingInstructions string           `json:"operatingInstructions"`
	ToolsContext          string           `json:"toolsContext"`
	ToolsEnabled          []string         `json:"toolsEnabled"`
	Heartbeat             *HeartbeatConfig `json:"heartbeat,omitempty"`
}

// AgentConfiguration is the union type for all agent configs
// Using LylaConfiguration as base since it's the primary type
type AgentConfiguration struct {
	LylaConfiguration
}

// AgentType indicates the type of agent
type AgentType string

const (
	AgentTypeLyla   AgentType = "lyla"
	AgentTypeWorker AgentType = "worker"
)

// AgentRow represents a row in the agents table
type AgentRow struct {
	AgentID   string                 `json:"agentId"`
	UserID    string                 `json:"userId"`
	AgentType AgentType              `json:"agentType"`
	Config    map[string]interface{} `json:"config"`
	CreatedAt time.Time              `json:"createdAt"`
	UpdatedAt time.Time              `json:"updatedAt"`
}

// PatchAgentRequest is the request body for PATCH /agents/{agentId}
type PatchAgentRequest struct {
	Soul                  *SoulConfig      `json:"soul,omitempty"`
	UserContext           *string          `json:"userContext,omitempty"`
	OperatingInstructions *string          `json:"operatingInstructions,omitempty"`
	ToolsContext          *string          `json:"toolsContext,omitempty"`
	ToolsEnabled          []string         `json:"toolsEnabled,omitempty"`
	Heartbeat             *HeartbeatConfig `json:"heartbeat,omitempty"`
	CronJobs              []CronJob        `json:"cronJobs,omitempty"`
}
