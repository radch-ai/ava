package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/mcp"
	"github.com/radch-ai/ava/backend/internal/mcpserver"
	"github.com/radch-ai/ava/backend/internal/security"
	"github.com/radch-ai/ava/backend/internal/tooladmission"
)

// MaxAdvertisedToolsetBytes bounds the definitions advertised in one turn,
// across servers and built-ins. Exceeding it fails visibly so the
// operator can narrow the selection or raise the limit — never by silently
// hiding tools.
const MaxAdvertisedToolsetBytes = 128 << 10

// AgentTools answers a lane's per-turn tool set: the effective ids —
// global switches, per-agent opt-outs and MCP gates applied — matched
// against the registry's executables, each wrapped in the admission chain.
// An error fails the turn; an empty set is the honest no-tools turn.
func (s *TurnService) AgentTools(ctx context.Context, agentID string) ([]tool.InvokableTool, error) {
	rows, err := s.queries.ResolveAgentToolsDetailed(ctx, db.ResolveAgentToolsDetailedParams{
		AgentID:   agentID,
		AgentID_2: agentID,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve tools for %q: %w", agentID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	impls := make([]tool.InvokableTool, 0, len(rows))
	sizes := make(map[string]int, len(rows))
	total := 0
	// One credential read per server per turn: the scrubber's known values
	// and the call deadline both come from the row.
	configs := make(map[string]mcp.ServerConfig)
	for _, row := range rows {
		// A resolved row without an executable behind it is an
		// intersection miss, not a failure: the registry returns what it
		// can actually run.
		impl, ok := s.registry.Invokable(row.ID)
		if !ok {
			continue
		}
		info, err := impl.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("tool metadata for %q: %w", row.ID, err)
		}
		opts := tooladmission.Options{
			AgentID:   agentID,
			CatalogID: row.ID,
		}
		if serverID, isMCP := row.ServerID.(string); isMCP && serverID != "" {
			cfg, err := s.serverConfig(ctx, serverID, configs)
			if err != nil {
				return nil, err
			}
			opts.ServerID = serverID
			opts.KnownSecrets = cfg.KnownSecrets()
			opts.Timeout = tooladmission.ResolveTimeout(cfg.Timeout)
		}
		encoded, err := json.Marshal(info)
		if err != nil {
			return nil, fmt.Errorf("encode tool metadata for %q: %w", row.ID, err)
		}
		sizes[row.ID] = len(encoded)
		total += len(encoded)
		impls = append(impls, s.admitter.Wrap(impl, info, opts))
	}
	if total > MaxAdvertisedToolsetBytes {
		return nil, fmt.Errorf("toolset budget exceeded (%d KiB > %d KiB): %s",
			total>>10, MaxAdvertisedToolsetBytes>>10, largestTools(sizes))
	}
	if len(impls) == 0 {
		return nil, nil
	}
	return impls, nil
}

// serverConfig resolves (once per turn) one server's connection config:
// endpoint, approvals, credential values and the call deadline. Only
// successes are memoized — a resolution failure is re-attempted and
// surfaced per tool, never replayed from the cache.
func (s *TurnService) serverConfig(ctx context.Context, serverID string, cache map[string]mcp.ServerConfig) (mcp.ServerConfig, error) {
	if cached, ok := cache[serverID]; ok {
		return cached, nil
	}
	row, err := s.queries.GetMcpServer(ctx, serverID)
	if err != nil {
		return mcp.ServerConfig{}, fmt.Errorf("read mcp server %q: %w", serverID, err)
	}
	cfg, err := mcpserver.Config(ctx, row, s.vaultStore)
	if err != nil {
		return mcp.ServerConfig{}, fmt.Errorf("resolve mcp server %q: %w", serverID, err)
	}
	cache[serverID] = cfg
	return cfg, nil
}

// ScrubToolCopy returns a sanitized copy of tool-call content for
// persistence and emission: the union of the agent's MCP servers'
// known secrets plus the shared pattern battery, so a consumer copy never
// carries a credential the admission chain's own copy would not. It is a
// copy scrubber only — dispatched arguments are never rewritten.
//
// Resolution failure degrades to the pattern-only scrubber; it never
// fails the turn.
func (s *TurnService) ScrubToolCopy(ctx context.Context, agentID, content string) string {
	if content == "" {
		return content
	}
	values, err := s.agentKnownSecrets(ctx, agentID)
	if err != nil {
		values = nil
	}
	sanitized, _ := security.NewScrubber(values).ScrubString(content)
	return sanitized
}

// agentKnownSecrets lists the credential spellings behind every server the
// agent may call: the values that can legitimately appear in that agent's
// traffic, and therefore the ones a copy must not.
func (s *TurnService) agentKnownSecrets(ctx context.Context, agentID string) ([]string, error) {
	rows, err := s.queries.ResolveAgentToolsDetailed(ctx, db.ResolveAgentToolsDetailedParams{
		AgentID:   agentID,
		AgentID_2: agentID,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve tools for %q: %w", agentID, err)
	}
	cache := make(map[string]mcp.ServerConfig)
	seen := make(map[string]struct{}, len(rows))
	var values []string
	for _, row := range rows {
		serverID, isMCP := row.ServerID.(string)
		if !isMCP || serverID == "" {
			continue
		}
		if _, done := seen[serverID]; done {
			continue
		}
		seen[serverID] = struct{}{}
		cfg, err := s.serverConfig(ctx, serverID, cache)
		if err != nil {
			// One unreadable server does not blind every other server's
			// redaction: keep going with the values already collected.
			continue
		}
		values = append(values, cfg.KnownSecrets()...)
	}
	return values, nil
}

// largestTools names the biggest advertised definitions, so a budget
// failure points at what to narrow.
func largestTools(sizes map[string]int) string {
	type entry struct {
		id   string
		size int
	}
	entries := make([]entry, 0, len(sizes))
	for id, size := range sizes {
		entries = append(entries, entry{id: id, size: size})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].size != entries[j].size {
			return entries[i].size > entries[j].size
		}
		return entries[i].id < entries[j].id
	})
	limit := min(3, len(entries))
	parts := make([]string, 0, limit)
	for _, e := range entries[:limit] {
		parts = append(parts, fmt.Sprintf("%s (%d B)", e.id, e.size))
	}
	return strings.Join(parts, ", ")
}

// CancelServerTools cancels every in-flight call against one server: the
// revocation path the API triggers when a server is disabled or deleted.
// Cancellation is best-effort; the remote outcome stays unknown.
func (s *TurnService) CancelServerTools(serverID string) int {
	return s.admitter.Registry().CancelServer(serverID)
}

// CancelToolCalls cancels the in-flight calls for one catalog tool — the
// global-disable path.
func (s *TurnService) CancelToolCalls(serverID, toolID string) int {
	return s.admitter.Registry().CancelTool(serverID, toolID)
}

// CancelAgentServerTools cancels one agent's in-flight calls against one
// server: an agent-local withdrawal must not touch another agent.
// A serverID of "" matches builtins, which have no server row.
func (s *TurnService) CancelAgentServerTools(agentID, serverID string) int {
	return s.admitter.Registry().CancelAgentServer(agentID, serverID)
}

// CancelAgentToolCalls cancels one agent's in-flight calls for one
// catalog tool.
func (s *TurnService) CancelAgentToolCalls(agentID, serverID, toolID string) int {
	return s.admitter.Registry().CancelAgentTool(agentID, serverID, toolID)
}

// CloseTools tears down the MCP client manager and the audit retention
// ticker on shutdown.
func (s *TurnService) CloseTools() {
	if s.stopPrune != nil {
		s.stopPrune()
		s.stopPrune = nil
	}
	s.registry.MCP().CloseAll()
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
