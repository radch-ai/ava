package backend

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
)

// AgentTools answers a lane's per-turn tool set: the effective ids —
// global switches and per-agent grants applied — matched against the
// registry's executables, ready for the lane's capture wrapper. An
// error fails the turn; an empty set is the honest no-tools turn.
func (s *TurnService) AgentTools(ctx context.Context, agentID string) ([]tool.InvokableTool, error) {
	ids, err := s.queries.ResolveAgentTools(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("resolve tools for %q: %w", agentID, err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	impls := make([]tool.InvokableTool, 0, len(ids))
	for _, id := range ids {
		// A granted row without an executable behind it is an
		// intersection miss, not a failure: the fetch returns what
		// the registry can actually run.
		if impl, ok := s.registry.Invokable(id); ok {
			impls = append(impls, impl)
		}
	}
	if len(impls) == 0 {
		return nil, nil
	}
	return impls, nil
}

// syncToolCatalog upserts the registered built-ins at boot: missing
// entries are created with their defaults, code-owned metadata is
// refreshed, and saved global switches are never overwritten.
func syncToolCatalog(queries *db.Queries, builtinTools []apitypes.Tool) error {
	ctx := context.Background()
	for _, t := range builtinTools {
		builtin := int64(0)
		if t.Builtin {
			builtin = 1
		}
		if err := queries.UpsertTool(ctx, db.UpsertToolParams{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
			Builtin:     builtin,
		}); err != nil {
			return fmt.Errorf("sync tool %q: %w", t.ID, err)
		}
	}
	return nil
}
