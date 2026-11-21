package agent_test

// Command-turn pins: a /new or /reset item executes as one durable
// thread switch — no model call, no user row, one two-event lifecycle
// batch — while queueing, promotion, failure, and stop behave like any
// other item.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/cloudwego/eino/components/tool"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// commandManager wires the lane to the real backend, with no switch
// override.
func commandManager(t *testing.T, llm *contextLLM, svc *backend.TurnService,
	hook func(ctx context.Context, agentID, itemID string) (*backend.ActivatedThread, *backend.JournalBatch, error)) *agent.UserAgentManager {
	t.Helper()
	return hookedManager(t, &agent.UserAgentManagerOptions{
		ResolveModel:   fixedResolver(llm.URL),
		GetAgentThread: svc.GetAgentThread,
	}, &journalHook{LifecycleJournal: svc, switchThread: hook})
}

// activeThreadRow reads the pointer straight from the row.
func activeThreadRow(t *testing.T, sqlDB *sql.DB, agentID string) string {
	t.Helper()
	return activeThreadID(t, sqlDB, agentID)
}

// readUntilCommandCompleted drains frames up to the thread-switch event of
// the command's committed batch.
func readUntilCommandCompleted(t *testing.T, c *websocket.Conn) apitypes.LifecycleEvent {
	t.Helper()
	for {
		ev := readEvent(t, c)
		if ev.Type != wstypes.EventTypeLifecycle {
			continue
		}
		for _, lifecycle := range lifecycleBatch(t, ev) {
			if lifecycle.Kind == apitypes.LifecycleThreadChanged {
				return lifecycle
			}
		}
	}
}

// awaitFirstChunk blocks until the running turn streams its first
// chunk, so the turn is genuinely in flight.
func awaitFirstChunk(t *testing.T, c *websocket.Conn) {
	t.Helper()
	for {
		ev := readEvent(t, c)
		if ev.Type == wstypes.EventTypeMessageChunk {
			return
		}
	}
}

// readTurnError drains frames up to a turn error's payload.
func readTurnError(t *testing.T, c *websocket.Conn) wstypes.ErrorEvent {
	t.Helper()
	return eventPayload[wstypes.ErrorEvent](t, readUntilError(t, c))
}

// A command is the whole turn: the thread switch commits with its two
// lifecycle events, the divider rides thread_changed, and nothing else
// runs.
func TestCommandCreatesThreadWithoutModel(t *testing.T) {
	for _, text := range []string{"/new", "/reset"} {
		t.Run(text, func(t *testing.T) {
			llm := newContextLLM(t)
			sqlDB, svc := contextBackend(t, "agent-cmd")
			mgr := commandManager(t, llm, svc, nil)

			c, sc := wsPair(t)
			itemID, err := admitItem(t, mgr, "agent-cmd", "conn-1", sc, text, nil,
				wstypes.QueueItemKindNewThread)
			require.NoError(t, err)

			// The started event names the pre-switch thread; the running
			// projection follows it with the kind and no user row: there
			// is nothing to render in the transcript.
			started := readLifecycle(t, c, func(ev apitypes.LifecycleEvent) bool {
				return ev.Kind == apitypes.LifecycleItemStarted
			})
			require.Equal(t, "agent-cmd-t1", started.ThreadID,
				"a command's start records the pre-switch thread")
			snapshot := queueStateUntil(t, c, func(p wstypes.QueueStateEvent) bool {
				return p.Active != nil
			})
			require.Equal(t, wstypes.QueueItemKindNewThread, snapshot.Active.Kind)
			require.Empty(t, snapshot.Active.UserMessageID)

			payload := readUntilCommandCompleted(t, c)
			require.Equal(t, itemID, payload.ItemID)
			require.NotEmpty(t, payload.ThreadID)
			require.Equal(t, "agent-cmd-t1", payload.PreviousThreadID)
			require.NotNil(t, payload.Divider)
			require.Equal(t, payload.ThreadID, payload.Divider.ID)
			require.Equal(t, "New session", payload.Divider.Content)
			require.NotEmpty(t, payload.Divider.CreatedAt)

			// The turn ends: the active item clears, nothing pends.
			final := queueStateUntil(t, c, func(p wstypes.QueueStateEvent) bool {
				return p.Active == nil
			})
			require.False(t, final.Paused)
			require.Empty(t, final.Items)

			require.Empty(t, llm.requests(), "a command never calls the model")
			require.Empty(t, storedRows(t, sqlDB, "agent-cmd"), "a command stores no message rows")
			require.Equal(t, payload.ThreadID, activeThreadRow(t, sqlDB, "agent-cmd"))

			// The display chain links the new thread to the old one.
			var previous sql.NullString
			require.NoError(t, sqlDB.QueryRow(
				`SELECT previous_thread_id FROM threads WHERE id = ?`, payload.ThreadID).Scan(&previous))
			require.True(t, previous.Valid)
			require.Equal(t, "agent-cmd-t1", previous.String)
		})
	}
}

// A command carries a selection like any prompt but never touches the
// model or the tools: an unusable selection cannot fail the switch.
func TestCommandBypassesModelResolution(t *testing.T) {
	sqlDB, svc := contextBackend(t, "agent-cmdx")
	opts := &agent.UserAgentManagerOptions{
		ResolveModel: func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
			return backend.ResolvedModel{}, errSink
		},
		Tools: func(ctx context.Context, agentID string) ([]tool.InvokableTool, error) {
			return nil, errSink
		},
		GetAgentThread: func(ctx context.Context, agentID string) ([]backend.AgentThreadMessage, error) {
			return nil, errSink
		},
	}
	mgr := hookedManager(t, opts, &journalHook{LifecycleJournal: svc})

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-cmdx", "conn-1", sc, "/reset",
		&apitypes.ModelEntry{ID: "missing", ProviderID: "missing"}, wstypes.QueueItemKindNewThread)
	require.NoError(t, err)

	payload := readUntilCommandCompleted(t, c)
	require.Equal(t, "agent-cmdx-t1", payload.PreviousThreadID)
	require.Empty(t, storedRows(t, sqlDB, "agent-cmdx"))
	require.Equal(t, payload.ThreadID, activeThreadRow(t, sqlDB, "agent-cmdx"))
}

// A command queued behind a running prompt waits its turn: the prompt
// finishes in the old thread, the switch commits, and the prompt
// behind the command sees only the new thread's context.
func TestCommandQueuedBehindPrompt(t *testing.T) {
	url, release := gatedStream(t)
	sqlDB, svc := contextBackend(t, "agent-cmdq")
	mgr := hookedManager(t, &agent.UserAgentManagerOptions{
		ResolveModel:   fixedResolver(url),
		GetAgentThread: svc.GetAgentThread,
	}, &journalHook{LifecycleJournal: svc})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-cmdq", "conn-1", sc, "prompt A")
	_, err := admitItem(t, mgr, "agent-cmdq", "conn-1", sc, "/new", nil, wstypes.QueueItemKindNewThread)
	require.NoError(t, err)
	_, err = admitItem(t, mgr, "agent-cmdq", "conn-1", sc, "prompt B", nil, "")
	require.NoError(t, err)

	// Release A: it settles, the command switches threads, B answers
	// in the new one.
	close(release)
	_, first := readUntilMessage(t, c)
	require.Equal(t, "par tial", first.Content)
	payload := readUntilCommandCompleted(t, c)
	require.Equal(t, "agent-cmdq-t1", payload.PreviousThreadID)
	_, second := readUntilMessage(t, c)
	require.Equal(t, "par tial", second.Content)

	require.Equal(t, payload.ThreadID, activeThreadRow(t, sqlDB, "agent-cmdq"))
	rows := storedRows(t, sqlDB, "agent-cmdq")
	var threads []string
	for _, row := range rows {
		threads = append(threads, row.ThreadID)
	}
	require.Equal(t, []string{
		"agent-cmdq-t1", "agent-cmdq-t1", payload.ThreadID, payload.ThreadID,
	}, threads, "A's rows stay in the old thread, B's land in the new one")
}

// Send-immediately promotes a command like any item: the running
// prompt settles as a partial answer, the switch commits, and the
// prompt behind it runs in the new thread.
func TestCommandSendNowPromotion(t *testing.T) {
	url, release := gatedStream(t)
	sqlDB, svc := contextBackend(t, "agent-cmdsn")
	mgr := hookedManager(t, &agent.UserAgentManagerOptions{
		ResolveModel:   fixedResolver(url),
		GetAgentThread: svc.GetAgentThread,
	}, &journalHook{LifecycleJournal: svc})

	c, sc := wsPair(t)
	submit(t, mgr, "agent-cmdsn", "conn-1", sc, "prompt A")
	// Promotion only interrupts a running turn: wait until A streams.
	awaitFirstChunk(t, c)
	cmdID, err := admitItem(t, mgr, "agent-cmdsn", "conn-1", sc, "/new", nil, wstypes.QueueItemKindNewThread)
	require.NoError(t, err)
	_, err = admitItem(t, mgr, "agent-cmdsn", "conn-1", sc, "prompt B", nil, "")
	require.NoError(t, err)

	_, err = mgr.SendNow("agent-cmdsn", cmdID, nil)
	require.NoError(t, err)

	// A settles without the gated tail: the promotion cancels its
	// read, so releasing the model now can only feed later requests.
	_, first := readUntilSettled(t, c)
	require.Equal(t, "par ", first.Content, "A settles with what streamed before the promotion")
	require.Equal(t, apitypes.ItemStopped, first.Event.Outcome)
	close(release)
	payload := readUntilCommandCompleted(t, c)
	_, second := readUntilMessage(t, c)
	require.NotEmpty(t, second.Content)
	require.Equal(t, payload.ThreadID, activeThreadRow(t, sqlDB, "agent-cmdsn"))
}

// A failed switch is a message-level failure: the error persists in the
// unchanged thread, the pause gate holds the work behind it, and
// send-immediately resumes.
func TestCommandFailurePausesAndReports(t *testing.T) {
	llm := newContextLLM(t, sseChunk("answer B"))
	sqlDB, svc := contextBackend(t, "agent-cmdf")
	failing := func(context.Context, string, string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
		return nil, nil, errSink
	}
	mgr := commandManager(t, llm, svc, failing)

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-cmdf", "conn-1", sc, "/new", nil, wstypes.QueueItemKindNewThread)
	require.NoError(t, err)
	bID, err := admitItem(t, mgr, "agent-cmdf", "conn-1", sc, "prompt B", nil, "")
	require.NoError(t, err)

	failure := readTurnError(t, c)
	require.Equal(t, errSink.Error(), failure.Message)
	paused := queueStateUntil(t, c, func(p wstypes.QueueStateEvent) bool {
		return p.Paused
	})
	require.Len(t, paused.Items, 1)
	require.Equal(t, bID, paused.Items[0].ID)

	// The failure is durable in the thread that never switched.
	rows := storedRows(t, sqlDB, "agent-cmdf")
	require.Len(t, rows, 1)
	require.Equal(t, apitypes.ThreadRoleError, rows[0].Role)
	require.Equal(t, failure.Message, rows[0].Content)
	require.Equal(t, "agent-cmdf-t1", rows[0].ThreadID)

	// The gate holds the queue until the user decides; the promoted
	// prompt answers normally.
	_, err = mgr.SendNow("agent-cmdf", bID, nil)
	require.NoError(t, err)
	_, answer := readUntilMessage(t, c)
	require.Equal(t, "answer B", answer.Content)
	require.Equal(t, "agent-cmdf-t1", activeThreadRow(t, sqlDB, "agent-cmdf"))
}

// A command whose backend op fails emits the backend's own wrap
// once: the lane never prefixes the message a second time.
func TestCommandFailureShowsBackendWrapOnce(t *testing.T) {
	llm := newContextLLM(t)
	sqlDB, svc := contextBackend(t, "agent-cmdw")
	wrapped := func(context.Context, string, string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
		return nil, nil, fmt.Errorf("create thread: %w", errSink)
	}
	mgr := commandManager(t, llm, svc, wrapped)

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-cmdw", "conn-1", sc, "/new", nil, wstypes.QueueItemKindNewThread)
	require.NoError(t, err)

	failure := readTurnError(t, c)
	require.Equal(t, "create thread: sink unavailable", failure.Message)
	require.Equal(t, "agent-cmdw-t1", activeThreadRow(t, sqlDB, "agent-cmdw"))
}

// A stop that lands before the commit rolls the transaction back: nothing
// switches, nothing is reported, and the item still settles — as a stop.
func TestCommandCancelBeforeCommitIsQuietStop(t *testing.T) {
	llm := newContextLLM(t)
	sqlDB, svc := contextBackend(t, "agent-cmdc")
	entered := make(chan struct{})
	blocked := func(ctx context.Context, _, _ string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
		close(entered)
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	mgr := commandManager(t, llm, svc, blocked)

	c, sc := wsPair(t)
	itemID, err := admitItem(t, mgr, "agent-cmdc", "conn-1", sc, "/new", nil, wstypes.QueueItemKindNewThread)
	require.NoError(t, err)
	<-entered
	mgr.Interrupt("agent-cmdc")

	final := queueStateUntil(t, c, func(p wstypes.QueueStateEvent) bool {
		return p.Active == nil && len(p.Items) == 0 && !p.Paused
	})
	require.False(t, final.Paused, "a stop is not a failure")
	require.Equal(t, "agent-cmdc-t1", activeThreadRow(t, sqlDB, "agent-cmdc"))
	require.Empty(t, storedRows(t, sqlDB, "agent-cmdc"), "a stopped command stores nothing")
	require.Equal(t, apitypes.ItemStopped, itemStates(t, sqlDB, "agent-cmdc")[itemID],
		"the stopped item still reaches a terminal outcome")
}

// A stop that lands after the commit still reports it: the switch is
// durable, so the transcript must show it.
func TestCommandAfterCommitStopStillReports(t *testing.T) {
	llm := newContextLLM(t)
	sqlDB, svc := contextBackend(t, "agent-cmda")
	committed := make(chan struct{})
	commitThenPark := func(ctx context.Context, agentID, itemID string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
		res, batch, err := svc.ExecuteThreadSwitch(ctx, agentID, itemID)
		if err != nil {
			return nil, nil, err
		}
		close(committed)
		<-ctx.Done()
		return res, batch, nil
	}
	mgr := commandManager(t, llm, svc, commitThenPark)

	c, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-cmda", "conn-1", sc, "/new", nil, wstypes.QueueItemKindNewThread)
	require.NoError(t, err)
	<-committed
	mgr.Interrupt("agent-cmda")

	payload := readUntilCommandCompleted(t, c)
	require.Equal(t, "agent-cmda-t1", payload.PreviousThreadID)
	require.Equal(t, payload.ThreadID, activeThreadRow(t, sqlDB, "agent-cmda"))
}

// Without a journal there is no durable half: admission is rejected and no
// item is invented.
func TestCommandWithoutJournalRejectsAdmission(t *testing.T) {
	mgr := agent.NewUserAgentManager(zerolog.Nop(), &agent.UserAgentManagerOptions{
		GetAgentThread: emptyThread,
	})

	itemID, err := mgr.Submit("agent-cmdm", "/new", nil, wstypes.QueueItemKindNewThread)
	require.Error(t, err)
	require.Empty(t, itemID)
}

// Draining admits nothing new, but a command already claimed still
// completes its switch: shutdown must not strand a committed intent.
func TestCommandDrainingAdmitsNothing(t *testing.T) {
	llm := newContextLLM(t)
	sqlDB, svc := contextBackend(t, "agent-cmdd")
	entered := make(chan struct{})
	release := make(chan struct{})
	parked := func(_ context.Context, agentID, itemID string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
		close(entered)
		<-release
		// The queued intent still commits: the drain's interrupt ends
		// the turn, not the durability of a claimed switch.
		return svc.ExecuteThreadSwitch(context.Background(), agentID, itemID)
	}
	mgr := commandManager(t, llm, svc, parked)

	_, sc := wsPair(t)
	_, err := admitItem(t, mgr, "agent-cmdd", "conn-1", sc, "/new", nil, wstypes.QueueItemKindNewThread)
	require.NoError(t, err)
	<-entered

	drained := make(chan struct{})
	go func() {
		mgr.Drain(time.Second)
		close(drained)
	}()
	// The switch holds the publication boundary while it is parked, so
	// the drain's own interrupt cannot settle it: the test releases it.
	close(release)
	<-drained

	// A drained queue admits nothing — the gate never reopens, so no
	// submission mints an item after shutdown began.
	itemID, err := mgr.Submit("agent-cmdd", "/new", nil, wstypes.QueueItemKindNewThread)
	require.Error(t, err, "a draining queue admits nothing")
	require.Empty(t, itemID)

	// The claimed switch committed anyway: the pointer moved.
	require.NotEqual(t, "agent-cmdd-t1", activeThreadRow(t, sqlDB, "agent-cmdd"),
		"the claimed command must not be stranded by the drain")
	_ = sqlDB
}
