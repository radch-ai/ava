package storage

import (
	"context"
	"strings"
)

// Workspace represents an agent's workspace files.
type Workspace struct {
	Soul     string
	Agents   string
	User     string
	Identity string
	Tools    string
	Loaded   bool // true if at least one file was loaded
}

// WorkspaceLoader loads workspace files from AgentStore.
type WorkspaceLoader struct {
	agentStore AgentStore
}

// NewWorkspaceLoaderFromAgentStore creates a new WorkspaceLoader backed by AgentStore.
func NewWorkspaceLoaderFromAgentStore(store AgentStore) *WorkspaceLoader {
	return &WorkspaceLoader{
		agentStore: store,
	}
}

// Load loads all workspace files for an agent from AgentStore.
// Missing files are treated as empty strings.
func (l *WorkspaceLoader) Load(ctx context.Context, userID, agentID string) (*Workspace, error) {
	config, err := l.agentStore.Get(ctx, userID, agentID)
	if err != nil {
		return &Workspace{Loaded: false}, nil
	}

	workspace := &Workspace{
		Soul:     config.SoulMd,
		Agents:   config.AgentsMd,
		User:     config.UserMd,
		Identity: config.IdentityMd,
		Tools:    config.ToolsMd,
		Loaded:   true,
	}

	return workspace, nil
}

// FormatSystemPrompt formats the workspace content as a system prompt.
func (w *Workspace) FormatSystemPrompt() string {
	var prompt strings.Builder

	if w.Soul != "" {
		prompt.WriteString(w.Soul)
		prompt.WriteString("\n\n")
	}

	if w.Agents != "" {
		prompt.WriteString(w.Agents)
		prompt.WriteString("\n\n")
	}

	if w.User != "" {
		prompt.WriteString("## User Information\n\n")
		prompt.WriteString(w.User)
		prompt.WriteString("\n\n")
	}

	if w.Identity != "" {
		prompt.WriteString("## Identity\n\n")
		prompt.WriteString(w.Identity)
		prompt.WriteString("\n\n")
	}

	if w.Tools != "" {
		prompt.WriteString("## Tool Guidance\n\n")
		prompt.WriteString(w.Tools)
		prompt.WriteString("\n\n")
	}

	return prompt.String()
}
