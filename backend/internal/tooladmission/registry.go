package tooladmission

// Cancellation registry: revocation cancels the request contexts
// of calls already admitted. Cancellation is not rollback — the remote
// action may already have happened — so the outcome reports an unknown
// remote result and nothing is retried.

import (
	"context"
	"sync"
)

// CancelRegistry maps live invocations to their cancel functions.
type CancelRegistry struct {
	mu    sync.Mutex
	next  uint64
	calls map[uint64]entry
}

// entry is one registered invocation. agentID scopes agent-local
// withdrawal to that agent's calls.
type entry struct {
	agentID  string
	serverID string
	toolID   string
	cancel   context.CancelFunc
}

// NewCancelRegistry returns an empty registry.
func NewCancelRegistry() *CancelRegistry {
	return &CancelRegistry{calls: make(map[uint64]entry)}
}

// Register records one in-flight call and returns its unregister hook.
// Unregistering an unknown handle is a no-op, so a double defer is safe.
func (r *CancelRegistry) Register(agentID, serverID, toolID string, cancel context.CancelFunc) (unregister func()) {
	r.mu.Lock()
	r.next++
	handle := r.next
	r.calls[handle] = entry{agentID: agentID, serverID: serverID, toolID: toolID, cancel: cancel}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.calls, handle)
		r.mu.Unlock()
	}
}

// CancelServer cancels every live call against one server and reports how
// many it cancelled.
func (r *CancelRegistry) CancelServer(serverID string) int {
	return r.cancelWhere(func(e entry) bool { return e.serverID == serverID })
}

// CancelTool cancels the live calls for one catalog tool.
func (r *CancelRegistry) CancelTool(serverID, toolID string) int {
	return r.cancelWhere(func(e entry) bool {
		return e.serverID == serverID && e.toolID == toolID
	})
}

// CancelAgentServer cancels one agent's live calls against one server:
// an agent-local withdrawal must not touch another agent's work. A
// serverID of "" matches builtins, which have no server.
func (r *CancelRegistry) CancelAgentServer(agentID, serverID string) int {
	return r.cancelWhere(func(e entry) bool {
		return e.agentID == agentID && e.serverID == serverID
	})
}

// CancelAgentTool cancels one agent's live calls for one catalog tool.
func (r *CancelRegistry) CancelAgentTool(agentID, serverID, toolID string) int {
	return r.cancelWhere(func(e entry) bool {
		return e.agentID == agentID && e.serverID == serverID && e.toolID == toolID
	})
}

// cancelWhere cancels the matching calls outside the registry lock: a
// cancel function may synchronously run cleanup that re-enters here.
func (r *CancelRegistry) cancelWhere(match func(entry) bool) int {
	r.mu.Lock()
	matched := make([]entry, 0, len(r.calls))
	for _, e := range r.calls {
		if match(e) {
			matched = append(matched, e)
		}
	}
	r.mu.Unlock()
	for _, e := range matched {
		e.cancel()
	}
	return len(matched)
}

// Len reports how many calls are currently registered (diagnostics).
func (r *CancelRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}
