package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// wrappedTool adapts one listed MCP tool for the registry: the model sees
// the composite name and each call runs under the per-call deadline.
type wrappedTool struct {
	id      string // server slug, prefixes errors
	base    tool.BaseTool
	model   string // ModelName(slug, remote)
	timeout time.Duration
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

// InvokableRun is the invocation boundary: per-call deadline here, errors
// carry the server slug so diagnostics stay explainable.
func (w *wrappedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	inv, ok := w.base.(tool.InvokableTool)
	if !ok {
		return "", fmt.Errorf("mcp server %s: tool %q is not invokable", w.id, w.model)
	}
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	out, err := inv.InvokableRun(ctx, argumentsInJSON, opts...)
	if err != nil {
		return "", fmt.Errorf("mcp server %s: %w", w.id, err)
	}
	return out, nil
}
