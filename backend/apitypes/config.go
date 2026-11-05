package apitypes

// AgentConfig is the agent configuration. UserContext is
// superagent-only; heartbeat starts disabled with empty fields (the
// superagent is seeded enabled); CronJobs is empty on regular agents.
type AgentConfig struct {
	// Soul is the agent personality and identity texts.
	Soul SoulConfig `json:"soul"`
	// UserContext is information about the user and their
	// preferences — superagent only.
	UserContext *string `json:"userContext,omitempty"`
	// OperatingInstructions are operating rules, priorities, and
	// behavioral guidelines.
	OperatingInstructions string `json:"operatingInstructions"`
	// ToolsContext is notes about local tools, conventions, and
	// integrations.
	ToolsContext string `json:"toolsContext"`
	// ToolsEnabled is the array of tool IDs this agent can use.
	ToolsEnabled []string `json:"toolsEnabled"`
	// Heartbeat config; disabled with empty fields until saved.
	Heartbeat HeartbeatConfig `json:"heartbeat"`
	// CronJobs is the agent's cron jobs — empty on regular agents.
	CronJobs []CronJob `json:"cronJobs"`
}
