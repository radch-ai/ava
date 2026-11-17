package tools

import (
	"context"
	"fmt"

	duckduckgo "github.com/cloudwego/eino-ext/components/tool/duckduckgo"
	"github.com/cloudwego/eino/components/tool"

	"github.com/radch-ai/ava/backend/apitypes"
)

// Registry is the app's built-in tool registration: catalog metadata
// for the backend's tools table plus the executables lanes attach.
// One source, so the settings page and the runtime cannot disagree.
type Registry struct {
	catalog    []apitypes.Tool
	invokables map[string]tool.InvokableTool
}

// Catalog is the registry's metadata; Enabled is the insertion
// default, not saved state.
func (r *Registry) Catalog() []apitypes.Tool { return r.catalog }

// Invokable returns the executable registered under a tool id.
func (r *Registry) Invokable(id string) (tool.InvokableTool, bool) {
	impl, ok := r.invokables[id]
	return impl, ok
}

// New builds the built-in registry. A tool that fails to construct
// is a boot error — never a silently missing entry.
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
			Builtin:     true,
			Enabled:     true,
		}},
		invokables: map[string]tool.InvokableTool{info.Name: ddg},
	}, nil
}
