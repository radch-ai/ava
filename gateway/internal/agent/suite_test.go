package agent_test

// Shared harness for the lane's external tests: a real backend journal over
// an in-memory store, the subscription seam, and the frame readers over
// it. Turn mechanics are pinned through these; storage behavior itself is
// pinned in the backend's own tests.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/sqlstore"
	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

// journalFor opens a migrated in-memory store and builds the backend over it
// — the durable half every lane commits through.
func journalFor(t *testing.T) (*sql.DB, *backend.TurnService) {
	t.Helper()
	sqlDB, err := sqlstore.OpenSQLite(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	_, _, turns, err := backend.New(sqlDB, zerolog.Nop(), testKey, backend.DefaultToolLimits(), nil)
	require.NoError(t, err)
	return sqlDB, turns
}

// subscribe attaches a connection to the lane's feed — the subscription half
// of the synchronization hand-off, without its replay.
func subscribe(mgr *agent.UserAgentManager, agentID, connID string, conn *websocket.Conn) {
	agent.Subscribe(mgr, agentID, connID, conn)
}

// admitItem subscribes the connection (the hand-off's subscription half) and
// admits one prompt or command, returning its stable item id.
func admitItem(t *testing.T, mgr *agent.UserAgentManager, agentID, connID string, conn *websocket.Conn, content string, model *apitypes.ModelEntry, kind string) (string, error) {
	t.Helper()
	if conn != nil {
		subscribe(mgr, agentID, connID, conn)
	}
	return mgr.Submit(agentID, content, model, kind)
}

// readLifecycle drains frames until one committed lifecycle event satisfies
// the predicate and returns it.
func readLifecycle(t *testing.T, c *websocket.Conn, match func(apitypes.LifecycleEvent) bool) apitypes.LifecycleEvent {
	t.Helper()
	for {
		ev := readEvent(t, c)
		if ev.Type != wstypes.EventTypeLifecycle {
			continue
		}
		batch := eventPayload[wstypes.LifecycleBatchEvent](t, ev)
		for _, raw := range batch.Events {
			var lifecycle apitypes.LifecycleEvent
			require.NoError(t, json.Unmarshal(raw, &lifecycle))
			if match(lifecycle) {
				return lifecycle
			}
		}
	}
}

// readTerminal drains until the item's terminal lifecycle event.
func readTerminal(t *testing.T, c *websocket.Conn, itemID string) apitypes.LifecycleEvent {
	t.Helper()
	return readLifecycle(t, c, func(ev apitypes.LifecycleEvent) bool {
		return ev.Kind == apitypes.LifecycleItemFinished && ev.ItemID == itemID
	})
}

// threadRowsFor reads one agent's stored rows in display order — the durable
// half assertions compare against the wire feed.
type threadRow struct {
	ID        string
	Role      apitypes.ThreadRole
	Content   string
	CreatedAt int64
	TurnID    string
	ToolCalls string
	ThreadID  string
}

func storedRows(t *testing.T, sqlDB *sql.DB, agentID string) []threadRow {
	t.Helper()
	rows, err := sqlDB.Query(
		`SELECT id, role, content, created_at, turn_id, COALESCE(tool_calls, ''), thread_id
		 FROM thread_messages WHERE agent_id = ? ORDER BY created_at ASC, id ASC`, agentID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []threadRow
	for rows.Next() {
		var row threadRow
		require.NoError(t, rows.Scan(&row.ID, &row.Role, &row.Content, &row.CreatedAt,
			&row.TurnID, &row.ToolCalls, &row.ThreadID))
		out = append(out, row)
	}
	require.NoError(t, rows.Err())
	return out
}

// itemStates reads one agent's item bookkeeping by id.
func itemStates(t *testing.T, sqlDB *sql.DB, agentID string) map[string]string {
	t.Helper()
	rows, err := sqlDB.Query(`SELECT id, state FROM queue_items WHERE agent_id = ?`, agentID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	out := make(map[string]string)
	for rows.Next() {
		var id, state string
		require.NoError(t, rows.Scan(&id, &state))
		out[id] = state
	}
	require.NoError(t, rows.Err())
	return out
}

// activeThreadID reads the agent's stored pointer.
func activeThreadID(t *testing.T, sqlDB *sql.DB, agentID string) string {
	t.Helper()
	var id string
	require.NoError(t, sqlDB.QueryRow(
		`SELECT active_thread_id FROM agents WHERE id = ?`, agentID).Scan(&id))
	return id
}

// describe renders stored rows for failure messages.
func describe(rows []threadRow) string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, fmt.Sprintf("%s:%s", row.Role, row.Content))
	}
	return fmt.Sprint(out)
}

// lazyJournal seeds an agent row the first time a test admits work for it, so
// lane tests name their agents freely; storage invariants are pinned in the
// backend's own tests.
type lazyJournal struct {
	*backend.TurnService
	sqlDB *sql.DB
	mu    sync.Mutex
	seen  map[string]struct{}
}

// newJournaledManager builds a manager over a real journal that seeds agents
// on first admission.
func newJournaledManager(t *testing.T, opts *agent.UserAgentManagerOptions) *agent.UserAgentManager {
	t.Helper()
	// journaledLane owns the one construction: this is the same manager
	// minus the store handle most tests never read.
	m, _ := journaledLane(t, opts)
	return m
}

func (j *lazyJournal) AcceptItem(ctx context.Context, agentID, itemID, content, kind string, model *apitypes.ModelEntry) (*backend.JournalBatch, error) {
	j.mu.Lock()
	if _, ok := j.seen[agentID]; !ok {
		if err := j.seedAgent(agentID); err != nil {
			j.mu.Unlock()
			return nil, err
		}
		j.seen[agentID] = struct{}{}
	}
	j.mu.Unlock()
	return j.TurnService.AcceptItem(ctx, agentID, itemID, content, kind, model)
}

func (j *lazyJournal) seedAgent(agentID string) error {
	threadID := agentID + "-t1"
	tx, err := j.sqlDB.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context, active_thread_id)
		 VALUES (?, ?, 'globe', '', '', '', '', ?) ON CONFLICT(id) DO NOTHING`,
		agentID, agentID, threadID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO threads (id, agent_id) VALUES (?, ?) ON CONFLICT(id) DO NOTHING`,
		threadID, agentID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// journalHook replaces selected journal operations for one test while every
// other call runs against the real backend — the seam failure injection and
// mid-operation parking need.
type journalHook struct {
	agent.LifecycleJournal
	switchThread   func(ctx context.Context, agentID, itemID string) (*backend.ActivatedThread, *backend.JournalBatch, error)
	shakeThread    func(ctx context.Context, agentID, itemID string) (int, *backend.JournalBatch, error)
	compactThread  func(ctx context.Context, agentID, itemID, summary string) (*backend.ActivatedThread, *backend.JournalBatch, error)
	appendMessages func(ctx context.Context, agentID, itemID string, msgs []backend.ThreadMessageDraft) (*backend.JournalBatch, error)
	finishItem     func(ctx context.Context, agentID, itemID, outcome, reason string, msgs []backend.ThreadMessageDraft) (*backend.JournalBatch, error)
	startItem      func(ctx context.Context, agentID, itemID string) (*backend.ItemStart, *backend.JournalBatch, error)
	toolFinished   func(ctx context.Context, agentID, itemID, invocationID, result string, errText *string) (*backend.JournalBatch, error)
	shakeAgent     func(ctx context.Context, agentID string) (int, error)
}

func (h *journalHook) StartItem(ctx context.Context, agentID, itemID string) (*backend.ItemStart, *backend.JournalBatch, error) {
	if h.startItem != nil {
		return h.startItem(ctx, agentID, itemID)
	}
	return h.LifecycleJournal.StartItem(ctx, agentID, itemID)
}

func (h *journalHook) AppendItemMessages(ctx context.Context, agentID, itemID string, msgs []backend.ThreadMessageDraft) (*backend.JournalBatch, error) {
	if h.appendMessages != nil {
		return h.appendMessages(ctx, agentID, itemID, msgs)
	}
	return h.LifecycleJournal.AppendItemMessages(ctx, agentID, itemID, msgs)
}

func (h *journalHook) FinishItem(ctx context.Context, agentID, itemID, outcome, reason string, msgs []backend.ThreadMessageDraft) (*backend.JournalBatch, error) {
	if h.finishItem != nil {
		return h.finishItem(ctx, agentID, itemID, outcome, reason, msgs)
	}
	return h.LifecycleJournal.FinishItem(ctx, agentID, itemID, outcome, reason, msgs)
}

func (h *journalHook) RecordToolFinished(ctx context.Context, agentID, itemID, invocationID, result string, errText *string) (*backend.JournalBatch, error) {
	if h.toolFinished != nil {
		return h.toolFinished(ctx, agentID, itemID, invocationID, result, errText)
	}
	return h.LifecycleJournal.RecordToolFinished(ctx, agentID, itemID, invocationID, result, errText)
}

func (h *journalHook) ShakeThreadContext(ctx context.Context, agentID, itemID string) (int, *backend.JournalBatch, error) {
	if h.shakeThread != nil {
		return h.shakeThread(ctx, agentID, itemID)
	}
	return h.LifecycleJournal.ShakeThreadContext(ctx, agentID, itemID)
}

func (h *journalHook) ExecuteThreadSwitch(ctx context.Context, agentID, itemID string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
	if h.switchThread != nil {
		return h.switchThread(ctx, agentID, itemID)
	}
	return h.LifecycleJournal.ExecuteThreadSwitch(ctx, agentID, itemID)
}

func (h *journalHook) ExecuteCompaction(ctx context.Context, agentID, itemID, summary string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
	if h.compactThread != nil {
		return h.compactThread(ctx, agentID, itemID, summary)
	}
	return h.LifecycleJournal.ExecuteCompaction(ctx, agentID, itemID, summary)
}

func (h *journalHook) ShakeAgentContext(ctx context.Context, agentID string) (int, error) {
	if h.shakeAgent != nil {
		return h.shakeAgent(ctx, agentID)
	}
	return h.LifecycleJournal.ShakeAgentContext(ctx, agentID)
}

// RotateThreadCompacted needs no hook: no test parks or fails it — the
// checker's rotation paths exercise through the real journal.
func (h *journalHook) RotateThreadCompacted(ctx context.Context, agentID, tagItemID, summary string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
	return h.LifecycleJournal.RotateThreadCompacted(ctx, agentID, tagItemID, summary)
}

// hookedManager builds a manager whose lane commits through hook.
func hookedManager(t *testing.T, opts *agent.UserAgentManagerOptions, hook *journalHook) *agent.UserAgentManager {
	t.Helper()
	if opts == nil {
		opts = &agent.UserAgentManagerOptions{}
	}
	opts.Journal = hook
	return agent.NewUserAgentManager(zerolog.Nop(), opts)
}

// gatedStream serves a model that flushes "par ", parks until released, then
// streams "tial" and finishes — a turn that is genuinely mid-flight.
func gatedStream(t *testing.T) (url string, release chan struct{}) {
	t.Helper()
	release = make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseChunk("par ")))
		flusher.Flush()
		<-release
		_, _ = w.Write([]byte(sseChunk("tial")))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(llm.Close)
	return llm.URL, release
}

// journaledLane builds a manager over a real journal (agents seeded on
// first admission) and returns the store its rows land in, for tests
// that assert the durable half alongside the wire feed.
func journaledLane(t *testing.T, opts *agent.UserAgentManagerOptions) (*agent.UserAgentManager, *sql.DB) {
	t.Helper()
	sqlDB, turns := journalFor(t)
	if opts == nil {
		opts = &agent.UserAgentManagerOptions{}
	}
	opts.Journal = &lazyJournal{TurnService: turns, sqlDB: sqlDB, seen: make(map[string]struct{})}
	return agent.NewUserAgentManager(zerolog.Nop(), opts), sqlDB
}
