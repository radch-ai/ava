package agent

// AgentStatus represents the current status of an agent.
type AgentStatus string

const (
	AgentStatusIdle   AgentStatus = "idle"
	AgentStatusActive AgentStatus = "active"
	AgentStatusError  AgentStatus = "error"
)

// String returns the string representation of the agent status.
func (s AgentStatus) String() string {
	return string(s)
}
