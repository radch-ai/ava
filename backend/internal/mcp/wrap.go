package mcp

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// wrappedTool adapts one listed MCP tool for the registry: the model sees
// the composite name and each call carries the catalog identity the
// admission chain re-authorizes against — never reverse-parsed from the
// model name.
type wrappedTool struct {
	id     string // server slug, prefixes errors
	remote string // the server's own tool name
	base   tool.BaseTool
	model  string // ModelName(slug, remote)
}

var _ tool.InvokableTool = (*wrappedTool)(nil)

// Info copies the underlying metadata (shared pointers stay shared —
// read-only) and swaps in the composite name.
func (w *wrappedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := w.base.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcp server %s: tool info: %w", w.id, err)
	}
	out := *info
	out.Name = w.model
	return &out, nil
}

// InvokableRun dispatches one call. The deadline, authorization, budgets
// and output bounds are the admission chain's (backend/internal/tooladmission
// wraps every invokable); errors carry the server slug so diagnostics stay
// explainable.
func (w *wrappedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	inv, ok := w.base.(tool.InvokableTool)
	if !ok {
		return "", fmt.Errorf("mcp server %s: tool %q is not invokable", w.id, w.model)
	}
	out, err := inv.InvokableRun(ctx, argumentsInJSON, opts...)
	if err != nil {
		return "", fmt.Errorf("mcp server %s: %w", w.id, err)
	}
	return out, nil
}
