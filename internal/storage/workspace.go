package storage

import (
	"context"
	"fmt"
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

// WorkspaceLoader loads workspace files from S3.
type WorkspaceLoader struct {
	client Client
}

// NewWorkspaceLoader creates a new WorkspaceLoader.
func NewWorkspaceLoader(client Client) *WorkspaceLoader {
	return &WorkspaceLoader{
		client: client,
	}
}

// Load loads all workspace files for an agent.
// Missing files are treated as empty strings.
func (l *WorkspaceLoader) Load(ctx context.Context, userID, agentID string) (*Workspace, error) {
	workspace := &Workspace{}

	// Try to load each file
	if soul, err := l.loadFile(ctx, userID, agentID, "SOUL.md"); err == nil {
		workspace.Soul = soul
		workspace.Loaded = true
	}

	if agents, err := l.loadFile(ctx, userID, agentID, "AGENTS.md"); err == nil {
		workspace.Agents = agents
		workspace.Loaded = true
	}

	if user, err := l.loadFile(ctx, userID, agentID, "USER.md"); err == nil {
		workspace.User = user
		workspace.Loaded = true
	}

	if identity, err := l.loadFile(ctx, userID, agentID, "IDENTITY.md"); err == nil {
		workspace.Identity = identity
		workspace.Loaded = true
	}

	if tools, err := l.loadFile(ctx, userID, agentID, "TOOLS.md"); err == nil {
		workspace.Tools = tools
		workspace.Loaded = true
	}

	return workspace, nil
}

// loadFile loads a single workspace file.
func (l *WorkspaceLoader) loadFile(ctx context.Context, userID, agentID, filename string) (string, error) {
	path := WorkspacePath(userID, agentID, filename)
	data, err := l.client.Get(ctx, path)
	if err != nil {
		return "", fmt.Errorf("file not found: %s", filename)
	}
	return string(data), nil
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
