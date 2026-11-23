package tools

import (
	"context"
	"fmt"
	"strings"

	duckduckgo "github.com/cloudwego/eino-ext/components/tool/duckduckgo"
	"github.com/cloudwego/eino/components/tool"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/mcp"
)

// Registry is the app's tool registration: catalog metadata for the
// backend's tools table plus the executables lanes attach. Built-ins are
// static; MCP tools are a live view over the client manager. One source,
// so the settings page and the runtime cannot disagree.
type Registry struct {
	catalog    []apitypes.Tool
	invokables map[string]tool.InvokableTool
	mcpManager *mcp.Manager
}

// Catalog is the registry's metadata; Enabled is the insertion
// default, not saved state.
func (r *Registry) Catalog() []apitypes.Tool { return r.catalog }

// MCP exposes the MCP client manager: boot discovery and the server
// routes dial through it.
func (r *Registry) MCP() *mcp.Manager { return r.mcpManager }

// Invokable returns the executable registered under a tool id.
// Built-in ids hit the static map; MCP catalog ids — "{slug}:{remote}",
// the colon never reaches a model — route to the owning server's cached
// client. An unknown or unsynced id is a miss, not a failure: callers
// treat it as an inert listing.
func (r *Registry) Invokable(id string) (tool.InvokableTool, bool) {
	if impl, ok := r.invokables[id]; ok {
		return impl, true
	}
	slug, remote, found := strings.Cut(id, ":")
	if !found {
		return nil, false
	}
	client, ok := r.mcpManager.Get(slug)
	if !ok {
		return nil, false
	}
	return client.InvokableByRemote(remote)
}

// New builds the built-in registry and its MCP client manager. A
// built-in tool that fails to construct is a boot error — never a
// silently missing entry. MCP servers connect later, best-effort.
func New() (*Registry, error) {
	ctx := context.Background()
	ddg, err := duckduckgo.NewTool(ctx, &duckduckgo.Config{}) //nolint:staticcheck // NewTextSearchTool not yet available
	if err != nil {
		return nil, fmt.Errorf("duckduckgo: %w", err)
	}
	info, err := ddg.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("duckduckgo info: %w", err)
	}
	return &Registry{
		catalog: []apitypes.Tool{{
			ID:          info.Name,
			Name:        "DuckDuckGo Search",
			Description: info.Desc,
			Enabled:     true,
		}},
		invokables: map[string]tool.InvokableTool{info.Name: ddg},
		mcpManager: mcp.NewManager(),
	}, nil
}
