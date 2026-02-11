// Package tools provides example tools demonstrating the agent tool interface.
//
// The tools in this package demonstrate:
// - Argument parsing from map[string]any
// - Error handling and validation
// - Context cancellation support
// - Different return types (text, numeric results)
//
// These tools can be used as templates for creating custom tools.
package tools

import (
	"sync"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// Registry manages tool registration and lookup.
//
// The Registry provides thread-safe operations for registering and retrieving
// tools by name. It prevents duplicate tool names and supports initialization
// with an existing set of tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]agent.Tool
}

// NewRegistry creates a new tool registry, optionally initialized with tools.
//
// If tools are provided, they will be registered in the new registry.
func NewRegistry(tools ...agent.Tool) *Registry {
	r := &Registry{
		tools: make(map[string]agent.Tool),
	}
	for _, tool := range tools {
		r.tools[tool.Name] = tool
	}
	return r
}

// RegisterTool registers a tool in the registry.
//
// Returns true if the tool was registered successfully.
// Returns false if a tool with that name already exists (the existing tool is preserved).
func RegisterTool(r *Registry, name string, tool agent.Tool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[name]; exists {
		return false
	}
	r.tools[name] = tool
	return true
}

// GetTool retrieves a tool by name from the registry.
//
// Returns nil if the tool is not found.
func GetTool(r *Registry, name string) *agent.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if tool, exists := r.tools[name]; exists {
		return &tool
	}
	return nil
}

// DefaultTools returns a slice of default example tools.
//
// The returned slice contains all example tools: echo, math, time, and delay.
func DefaultTools() []agent.Tool {
	return []agent.Tool{
		ExampleEchoTool(),
		ExampleMathTool(),
		ExampleTimeTool(),
		ExampleDelayTool(),
	}
}
