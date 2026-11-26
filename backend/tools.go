package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

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
// against the registry's executables, each wrapped in the admission
// chain, with the internal read_tool_output always beside them. An
// error fails the turn; the bound seam never resolves to zero tools.
// TurnTools carries the whole resolution: the admitted invokables, each
// tool's metadata, and the scrubber for every observable copy of the
// turn's traffic (the agent's servers' known values plus the pattern
// battery). One resolution pass per turn — the gateway never re-reads
// metadata or credentials.
type TurnTools struct {
	Impls    []tool.InvokableTool
	Infos    []*schema.ToolInfo
	Scrubber *Scrubber
}

// Scrubber is the secret scrubber re-exported for the public surface:
// TurnTools.Scrubber's type, so the gateway can name it without
// reaching into the backend's internals.
type Scrubber = tooladmission.Scrubber

func (s *TurnService) AgentTools(ctx context.Context, agentID string) (TurnTools, error) {
	rows, err := s.queries.ResolveAgentToolsDetailed(ctx, agentID)
	if err != nil {
		return TurnTools{}, fmt.Errorf("resolve tools for %q: %w", agentID, err)
	}

	impls := make([]tool.InvokableTool, 0, len(rows)+1)
	infos := make([]*schema.ToolInfo, 0, len(rows)+1)
	// read_tool_output is internal, never catalog-managed: it rides
	// beside the effective ids so a shaken context always keeps its
	// restore path, even when the catalog resolves nothing.
	impls = append(impls, &readToolOutputTool{svc: s, agentID: agentID})
	infos = append(infos, readToolOutputInfo)

	sizes := make(map[string]int, len(rows))
	// The internal definition is advertised beside them: its fixed
	// cost rides the same budget the catalog rows are measured against.
	total := len(readToolOutputInfoJSON)
	// One credential read per server per turn: the scrubber's known
	// values and the call deadline both come from the row.
	configs := make(map[string]mcp.ServerConfig)
	forms := make(map[string][]string)
	var values []string
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
			return TurnTools{}, fmt.Errorf("tool metadata for %q: %w", row.ID, err)
		}
		opts := tooladmission.Options{
			AgentID:   agentID,
			CatalogID: row.ID,
		}
		if row.ServerID.Valid && row.ServerID.String != "" {
			serverID := row.ServerID.String
			cfg, err := s.serverConfig(ctx, serverID, configs)
			if err != nil {
				return TurnTools{}, err
			}
			opts.ServerID = serverID
			opts.Timeout = tooladmission.ResolveTimeout(cfg.Timeout)
			known, seen := forms[serverID]
			if !seen {
				known = cfg.KnownSecrets()
				forms[serverID] = known
				values = append(values, known...)
			}
			opts.KnownSecrets = known
		}
		encoded, err := json.Marshal(info)
		if err != nil {
			return TurnTools{}, fmt.Errorf("encode tool metadata for %q: %w", row.ID, err)
		}
		sizes[row.ID] = len(encoded)
		total += len(encoded)
		infos = append(infos, info)
		impls = append(impls, s.admitter.Wrap(impl, info, opts))
	}
	if total > MaxAdvertisedToolsetBytes {
		return TurnTools{}, fmt.Errorf("toolset budget exceeded (%d KiB > %d KiB): %s",
			total>>10, MaxAdvertisedToolsetBytes>>10, largestTools(sizes))
	}
	return TurnTools{Impls: impls, Infos: infos, Scrubber: security.NewScrubber(values)}, nil
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
		if err := queries.UpsertTool(ctx, db.UpsertToolParams{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
		}); err != nil {
			return fmt.Errorf("sync tool %q: %w", t.ID, err)
		}
	}
	return nil
}
