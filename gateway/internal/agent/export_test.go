package agent

// Test-only observation surfaces: manager-owned queue state — the wire does
// not project connection or waiting-work counts, so the UI derives them from
// the queue snapshot — and the compaction projection helpers the agent_test
// suite pins as plain units. Nothing here adds a production surface.

import (
	"github.com/coder/websocket"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// ConnCount reports one agent's subscribed connections.
func ConnCount(m *UserAgentManager, agentID string) int {
	return m.state(agentID).conns.Count()
}

// Worker returns the agent's live worker or nil; workers are optional
// surfaces over the durable queue state.
func Worker(m *UserAgentManager, agentID string) *UserAgent {
	s, ok := m.peek(agentID)
	if !ok {
		return nil
	}
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	return s.worker
}

// LaneStatus snapshots the agent onto the wire even without a worker —
// a queue can hold pending work while idle.
func LaneStatus(m *UserAgentManager, agentID string) wstypes.LaneStatus {
	s, ok := m.peek(agentID)
	if !ok {
		return wstypes.LaneStatus{}
	}
	s.workerMu.Lock()
	w := s.worker
	s.workerMu.Unlock()
	return w.LaneStatus()
}

// QueueLen reports one agent's waiting work: the FIFO plus the reserved
// slot, excluding the active item.
func QueueLen(m *UserAgentManager, agentID string) int {
	s := m.state(agentID)
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending) + s.nextSlotCount()
}

// Subscribe attaches a connection to one agent's feed without replay —
// the test harness's attach route. Production paths synchronize through
// HandOff, whose single registration carries the ready frame.
func Subscribe(m *UserAgentManager, agentID, connID string, conn *websocket.Conn) {
	m.state(agentID).conns.Add(connID, conn)
}

// ReduceForSummary exposes the summarization reduction so the
// agent_test suite can pin its projection rules as a plain unit.
func ReduceForSummary(records []backend.AgentThreadMessage) []backend.AgentThreadMessage {
	return reduceForSummary(records)
}

// OmittedToolNote exposes the neutral omission marker for the same
// unit pins.
func OmittedToolNote(size int) string {
	return omittedToolNote(size)
}
