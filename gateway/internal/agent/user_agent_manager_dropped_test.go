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
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
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

// TestSubmitMsgRacingExitNeverReportsFalseSuccess drives the real
// loop against concurrent submits: the lane's idle timeout is one
// tick away from firing, so its exit races the senders the whole
// way. Every SubmitMsg that reports success must eventually run —
// the sink's user rows are the proof.
func TestSubmitMsgRacingExitNeverReportsFalseSuccess(t *testing.T) {
	var sinkMu sync.Mutex
	var persisted strings.Builder
	sink := func(_ context.Context, _ string, msgs []apitypes.ThreadMessageDraft) error {
		sinkMu.Lock()
		defer sinkMu.Unlock()
		for _, m := range msgs {
			persisted.WriteString(m.Content + "\n")
		}
		return nil
	}
	noModel := func(context.Context, *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		// The resolver failing is fine: the user row persists before
		// any resolution, so it still proves the turn ran.
		return backend.ResolvedModel{}, errors.New("no model in race test")
	}

	m := NewUserAgentManager(zerolog.Nop(), &UserAgentManagerOptions{
		ResolveModel: noModel,
		PersistTurn:  sink,
	})

	lane := &agentLane{
		msgCh: make(chan Message, defaultMsgQueueSize),
		done:  make(chan struct{}),
	}
	lane.once.Do(func() {}) // arm init: SubmitMsg must not rebuild the lane
	lane.agent = newUserAgent(context.WithoutCancel(context.Background()), UserAgentOptions{
		AgentID:     "agent-race",
		MsgCh:       lane.msgCh,
		IdleTimeout: time.Nanosecond, // the exit races the senders
		PersistTurn: sink,
		Logger:      zerolog.Nop(),
		OnExit: func() {
			close(lane.done)
			m.lanes.Delete("agent-race")
		},
	})
	m.lanes.Store("agent-race", lane)

	const racers = 64
	accepted := make([]bool, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			accepted[i] = m.SubmitMsg(context.Background(), "agent-race",
				"conn-1", nil, Message{ID: "m", Content: fmt.Sprintf("hi-%d", i)})
		}()
	}
	wg.Wait()

	// Every reported success must show up as a persisted prompt;
	// an orphaned buffer message never would.
	require.Eventually(t, func() bool {
		sinkMu.Lock()
		defer sinkMu.Unlock()
		text := persisted.String()
		for i := range racers {
			if accepted[i] && !strings.Contains(text, fmt.Sprintf("hi-%d", i)) {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond,
		"a submit reported success but its prompt never ran")
}
