package backend

// Admission policy types re-exported through backend's public surface: the
// binary binds them from the environment, and the gateway needs no access
// to backend/internal/*.

import (
	"context"

	"github.com/radch-ai/ava/backend/internal/tooladmission"
)

// ToolLimits is the invocation capacity budget: per MCP server and across
// the whole process, builtins included.
type ToolLimits = tooladmission.Limits

// DefaultToolLimits returns the approved starting policy.
func DefaultToolLimits() ToolLimits { return tooladmission.DefaultLimits() }

// WithToolCallID stamps the model's tool-call id onto an invocation
// context, so audit admission and outcome rows pair without either
// storing a payload.
func WithToolCallID(ctx context.Context, id string) context.Context {
	return tooladmission.WithCallID(ctx, id)
}

// NewPatternScrubber returns the pattern-only scrubber: the gateway's
// defense-in-depth fan-out boundary.
func NewPatternScrubber() *tooladmission.Scrubber {
	return tooladmission.NewPatternScrubber()
}
