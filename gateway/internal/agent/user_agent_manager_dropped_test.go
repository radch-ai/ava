package agent

// The drop path: a submit landing on a lane whose agent already
// exited must return false and say so — the message would otherwise
// vanish into a channel nobody drains, or worse, report success
// while never running. The lane is staged in exactly the state
// OnExit occupies between close(done) and the map delete: dead, but
// still mapped.

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// lockedLogs is a race-safe zerolog sink.
type lockedLogs struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *lockedLogs) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *lockedLogs) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

func TestSubmitMsgWarnsOnExitedLane(t *testing.T) {
	logs := &lockedLogs{}
	m := NewUserAgentManager(zerolog.New(logs), &UserAgentManagerOptions{})

	dead := &agentLane{
		msgCh: make(chan Message, defaultMsgQueueSize),
		done:  make(chan struct{}),
	}
	m.lanes.Store("agent-drop", dead)
	dead.once.Do(func() {}) // arm init: SubmitMsg's once.Do must not rebuild the lane
	close(dead.done)

	// The conn is never reached: a dead lane is rejected before the
	// broadcaster sees it.
	ok := m.SubmitMsg(context.Background(), "agent-drop", "conn-1", nil,
		Message{ID: "m1", Content: "hi"})
	require.False(t, ok, "submit onto an exited lane must report the drop")
	require.Contains(t, logs.String(), "submit dropped",
		"a dropped submit must log its warning")
}
