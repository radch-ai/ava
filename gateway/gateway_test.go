package gateway_test

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/sqlstore"
	"github.com/radch-ai/ava/gateway"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// Frame plumbing: every read and write runs on its own deadline, so a
// stalled frame fails its step instead of the whole connection.

// readEvent reads the next server event.
func readEvent(t *testing.T, conn *coderws.Conn) wstypes.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ev wstypes.Event
	if err := wsjson.Read(ctx, conn, &ev); err != nil {
		t.Fatalf("reading event failed: %v", err)
	}
	return ev
}

// writeFrame writes one client frame.
func writeFrame(t *testing.T, conn *coderws.Conn, frame any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, frame); err != nil {
		t.Fatalf("writing frame failed: %v", err)
	}
}

// payload decodes one event's Data into T.
func payload[T any](t *testing.T, ev wstypes.Event) T {
	t.Helper()
	raw, err := json.Marshal(ev.Data)
	if err != nil {
		t.Fatalf("re-marshaling %s payload failed: %v", ev.Type, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding %s payload failed: %v", ev.Type, err)
	}
	return out
}

// syncAnswer sends one sync frame (cursor "" starts fresh) and returns
// the handshake answer that follows it.
func syncAnswer(t *testing.T, conn *coderws.Conn, agentID, cursor string) wstypes.Event {
	t.Helper()
	writeFrame(t, conn, map[string]any{
		"type": "sync", "agent_id": agentID,
		"data": map[string]any{"cursor": cursor},
	})
	for {
		ev := readEvent(t, conn)
		switch ev.Type {
		case wstypes.EventTypeSyncSnapshot, wstypes.EventTypeSyncBatch, wstypes.EventTypeSyncReady:
			return ev
		}
	}
}

// syncHandshake drives the client protocol from cursor until
// sync_ready: a snapshot or batch advances the checkpoint, the ready
// frame ends the handshake. The answer sequence is returned for
// ordering assertions.
func syncHandshake(t *testing.T, conn *coderws.Conn, agentID, cursor string) (wstypes.SyncReadyEvent, []wstypes.EventType) {
	t.Helper()
	var seq []wstypes.EventType
	for {
		ev := syncAnswer(t, conn, agentID, cursor)
		seq = append(seq, ev.Type)
		switch ev.Type {
		case wstypes.EventTypeSyncSnapshot:
			cursor = payload[wstypes.SyncSnapshotEvent](t, ev).Cursor
		case wstypes.EventTypeSyncBatch:
			cursor = payload[wstypes.LifecycleBatchEvent](t, ev).Cursor
		case wstypes.EventTypeSyncReady:
			return payload[wstypes.SyncReadyEvent](t, ev), seq
		}
	}
}

// syncConn synchronizes a fresh connection from scratch and returns
// the ready payload.
func syncConn(t *testing.T, conn *coderws.Conn, agentID string) wstypes.SyncReadyEvent {
	t.Helper()
	ready, _ := syncHandshake(t, conn, agentID, "")
	return ready
}

// lifecycleEvents decodes one lifecycle frame's committed events.
func lifecycleEvents(t *testing.T, ev wstypes.Event) []apitypes.LifecycleEvent {
	t.Helper()
	if ev.Type != wstypes.EventTypeLifecycle {
		t.Fatalf("expected a lifecycle event, got %v", ev.Type)
	}
	batch := payload[wstypes.LifecycleBatchEvent](t, ev)
	return decodeEvents(t, batch.Events)
}

// decodeEvents decodes one batch's raw lifecycle events.
func decodeEvents(t *testing.T, raws []json.RawMessage) []apitypes.LifecycleEvent {
	t.Helper()
	out := make([]apitypes.LifecycleEvent, 0, len(raws))
	for _, raw := range raws {
		var lc apitypes.LifecycleEvent
		if err := json.Unmarshal(raw, &lc); err != nil {
			t.Fatalf("decoding lifecycle event failed: %v", err)
		}
		out = append(out, lc)
	}
	return out
}

// readTerminal drains frames until the item's terminal lifecycle event.
func readTerminal(t *testing.T, conn *coderws.Conn, itemID string) apitypes.LifecycleEvent {
	t.Helper()
	for {
		ev := readEvent(t, conn)
		if ev.Type != wstypes.EventTypeLifecycle {
			continue
		}
		for _, lc := range lifecycleEvents(t, ev) {
			if lc.Kind == apitypes.LifecycleItemFinished && lc.ItemID == itemID {
				return lc
			}
		}
	}
}

// readAdmission answers the named admission reply, collecting the
// queue snapshots passed on the way: the pending-rows snapshot is
// published by the broadcast pump and can precede the direct reply,
// and the start snapshot that follows carries the item as active
// rather than pending — either may be the only one that ever names it.
// A rejected admission must change no queue state, so
// allowQueueState fails the read on any snapshot.
func readAdmission(t *testing.T, conn *coderws.Conn, requestID string, allowQueueState bool) (wstypes.QueueResultEvent, []wstypes.QueueStateEvent) {
	t.Helper()
	var snapshots []wstypes.QueueStateEvent
	for {
		ev := readEvent(t, conn)
		switch ev.Type {
		case wstypes.EventTypeQueueState:
			if !allowQueueState {
				t.Fatalf("rejected prompt %s changed queue state: %+v", requestID, ev.Data)
			}
			snapshots = append(snapshots, payload[wstypes.QueueStateEvent](t, ev))
		case wstypes.EventTypeQueueResult:
			res := payload[wstypes.QueueResultEvent](t, ev)
			if res.RequestID == requestID {
				return res, snapshots
			}
		}
	}
}

// sendPrompt submits one prompt on a synchronized connection and
// returns the admitted item's id. A nil model follows default routing.
func sendPrompt(t *testing.T, conn *coderws.Conn, agentID, requestID, content string, model map[string]string) string {
	t.Helper()
	data := map[string]any{"request_id": requestID, "content": content}
	if model != nil {
		data["model"] = model
	}
	writeFrame(t, conn, map[string]any{
		"type": "prompt", "agent_id": agentID, "data": data,
	})
	res, _ := readAdmission(t, conn, requestID, true)
	if !res.Accepted || res.ItemID == "" {
		t.Fatalf("prompt %s was not admitted: %+v", requestID, res)
	}
	return res.ItemID
}

// runTurn submits one prompt and waits for its committed answer.
func runTurn(t *testing.T, conn *coderws.Conn, agentID, requestID, content string) apitypes.LifecycleEvent {
	t.Helper()
	itemID := sendPrompt(t, conn, agentID, requestID, content, nil)
	terminal := readTerminal(t, conn, itemID)
	if terminal.Outcome != apitypes.ItemCompleted {
		t.Fatalf("turn %s did not complete: outcome %q, rows %+v", requestID, terminal.Outcome, terminal.Rows)
	}
	return terminal
}

// drainSilent asserts nothing arrives within the window — the "feed
// went quiet" check.
func drainSilent(t *testing.T, conn *coderws.Conn, wait time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	var ev wstypes.Event
	if err := wsjson.Read(ctx, conn, &ev); err == nil {
		t.Fatalf("unexpected event after the feed went quiet: %v", ev.Type)
	}
}

// dialServer serves handler over HTTP and dials it once, consuming the
// connected event.
func dialServer(t *testing.T, url string) (*coderws.Conn, context.Context) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(url, "http"), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	var connected map[string]any
	if err := wsjson.Read(ctx, conn, &connected); err != nil {
		t.Fatalf("reading connected event failed: %v", err)
	}
	if connected["type"] != "connected" {
		t.Fatalf("expected connected event, got %v", connected["type"])
	}
	data, ok := connected["data"].(map[string]any)
	if !ok {
		t.Fatalf("connected event data is not a map: %+v", connected["data"])
	}
	if _, exists := data["conn_id"]; !exists {
		t.Fatal("connected event should include conn_id")
	}
	return conn, ctx
}

// testMasterKey mirrors the binary's key shape for backend.New.
var testMasterKey = []byte("0123456789abcdef0123456789abcdef")

// journalBackend opens a migrated in-memory store, builds the real
// backend over it, and seeds the named agents — the shape of
// turn_context_test.go's contextBackend. The store's foreign keys
// require the agent and thread rows before anything commits.
func journalBackend(t *testing.T, agentIDs ...string) (*sql.DB, *backend.TurnService) {
	t.Helper()
	sqlDB, err := sqlstore.OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("opening store failed: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	_, _, turns, err := backend.New(sqlDB, zerolog.Nop(), testMasterKey, backend.DefaultToolLimits(), nil)
	if err != nil {
		t.Fatalf("building backend failed: %v", err)
	}
	for _, agentID := range agentIDs {
		seedAgent(t, sqlDB, agentID)
	}
	return sqlDB, turns
}

// seedAgent inserts one agent with an active thread — the minimum the
// journal's foreign keys need. The system agents are seeded by
// migration already, so both inserts are idempotent and the agent's
// actual active thread is read back: it may be the migration's own.
func seedAgent(t *testing.T, sqlDB *sql.DB, agentID string) string {
	t.Helper()
	threadID := agentID + "-t1"
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatalf("beginning seed transaction failed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`INSERT INTO agents (id, name, icon, soul_personality, soul_identity, operating_instructions, tools_context, active_thread_id)
		 VALUES (?, ?, 'globe', '', '', '', '', ?) ON CONFLICT(id) DO NOTHING`, agentID, agentID, threadID); err != nil {
		t.Fatalf("seeding agent %s failed: %v", agentID, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO threads (id, agent_id) VALUES (?, ?) ON CONFLICT(id) DO NOTHING`, threadID, agentID); err != nil {
		t.Fatalf("seeding thread for %s failed: %v", agentID, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("committing seed failed: %v", err)
	}
	var active string
	if err := sqlDB.QueryRow(`SELECT active_thread_id FROM agents WHERE id = ?`, agentID).Scan(&active); err != nil {
		t.Fatalf("reading active thread for %s failed: %v", agentID, err)
	}
	return active
}

// activeThreadID reads the agent's stored thread pointer — the
// execution thread a started item resolves to.
func activeThreadID(t *testing.T, sqlDB *sql.DB, agentID string) string {
	t.Helper()
	var active string
	if err := sqlDB.QueryRow(`SELECT active_thread_id FROM agents WHERE id = ?`, agentID).Scan(&active); err != nil {
		t.Fatalf("reading active thread for %s failed: %v", agentID, err)
	}
	return active
}

// journalGateway is one gateway server over a real in-memory backend:
// admission, turns, and recovery all run through the durable journal
// the shipped protocol requires.
type journalGateway struct {
	url   string
	sqlDB *sql.DB
	turns *backend.TurnService
}

// dialJournalGateway builds a gateway over a fresh in-memory backend
// and serves it. cfg gets Journal injected unless it already carries
// one (tests may wrap the service); the named agents are seeded.
func dialJournalGateway(t *testing.T, cfg *gateway.Config, agentIDs ...string) *journalGateway {
	t.Helper()
	sqlDB, turns := journalBackend(t, agentIDs...)
	if cfg == nil {
		cfg = &gateway.Config{}
	}
	if cfg.Journal == nil {
		cfg.Journal = turns
	}
	handler, err := gateway.New(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &journalGateway{url: server.URL, sqlDB: sqlDB, turns: turns}
}

// dial opens one more client connection to the gateway.
func (g *journalGateway) dial(t *testing.T) *coderws.Conn {
	t.Helper()
	conn, _ := dialServer(t, g.url)
	return conn
}

// cursorAt renders the wire cursor for one journal position of the
// agent.
func cursorAt(t *testing.T, turns *backend.TurnService, agentID string, pos int64) string {
	t.Helper()
	historyID, _, _, err := turns.JournalStatus(context.Background(), agentID)
	if err != nil {
		t.Fatalf("journal status failed: %v", err)
	}
	return backend.FormatCursor(historyID, pos)
}

// headCursor renders the agent's committed head as the cursor the
// wire carries.
func headCursor(t *testing.T, turns *backend.TurnService, agentID string) string {
	t.Helper()
	_, head, _, err := turns.JournalStatus(context.Background(), agentID)
	if err != nil {
		t.Fatalf("journal status failed: %v", err)
	}
	return cursorAt(t, turns, agentID, head)
}

// historyOf reads the database's history identity for replay requests.
func historyOf(t *testing.T, turns *backend.TurnService, agentID string) string {
	t.Helper()
	historyID, _, _, err := turns.JournalStatus(context.Background(), agentID)
	if err != nil {
		t.Fatalf("journal status failed: %v", err)
	}
	return historyID
}

// journalEvents reads the retained events after pos straight from the
// journal, decoded — the committed history replay answers are compared
// against.
func journalEvents(t *testing.T, turns *backend.TurnService, agentID string, after int64) []apitypes.LifecycleEvent {
	t.Helper()
	page, err := turns.ReplayAfter(context.Background(), agentID, historyOf(t, turns, agentID), after, 1000)
	if err != nil {
		t.Fatalf("replaying journal after %d failed: %v", after, err)
	}
	events := make([]apitypes.LifecycleEvent, 0, len(page.Events))
	for _, raw := range page.Events {
		var ev apitypes.LifecycleEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("decoding replay event after %d: %v", after, err)
		}
		events = append(events, ev)
	}
	return events
}

// sseFrame renders one chat.completion.chunk SSE payload.
func sseFrame(delta string) string {
	payload := map[string]any{
		"id": "chatcmpl-test", "object": "chat.completion.chunk", "created": 0,
		"model": "test-model",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": nil,
			"delta": map[string]any{"role": "assistant", "content": delta},
		}},
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

// scriptedLLM serves one scripted SSE answer per request, in order,
// holding the last entry once the script runs out.
func scriptedLLM(t *testing.T, answers ...string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	next := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := next
		if i >= len(answers) {
			i = len(answers) - 1
		} else {
			next++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: " + sseFrame(answers[i]) + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(s.Close)
	return s
}

// fixedResolver resolves every selection to the given endpoint.
func fixedResolver(url string) func(context.Context, *apitypes.ModelEntry) (backend.ResolvedModel, error) {
	return func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{BaseURL: url, APIKey: "sk-test", Model: "test-model"}, nil
	}
}

// With no model resolver bound, the turn fails at message level. The
// failure is durable now: a terminal lifecycle event carries the error
// row, so every device — live or resynchronizing — sees the same fact.
func TestPromptWithoutResolverEmitsError(t *testing.T) {
	g := dialJournalGateway(t, &gateway.Config{GetAgentThread: emptyThread}, "_superagent")
	conn := g.dial(t)
	syncConn(t, conn, "_superagent")

	itemID := sendPrompt(t, conn, "_superagent", "r1", "hello", nil)

	terminal := readTerminal(t, conn, itemID)
	if terminal.Outcome != apitypes.ItemFailed {
		t.Fatalf("expected a failed outcome, got %q", terminal.Outcome)
	}
	if len(terminal.Rows) != 1 || terminal.Rows[0].Role != apitypes.ThreadRoleError {
		t.Fatalf("expected one error row on the terminal event, got %+v", terminal.Rows)
	}
	if terminal.Rows[0].Content != "no model resolver configured" {
		t.Fatalf("error row should name the missing resolver, got %q", terminal.Rows[0].Content)
	}
}

// The thread source is required, like the resolver: with no
// GetAgentThread bound there is no working context to run on, and the
// turn fails at message level with a durable error row.
func TestPromptWithoutThreadSourceFailsTurn(t *testing.T) {
	g := dialJournalGateway(t, &gateway.Config{}, "_superagent")
	conn := g.dial(t)
	syncConn(t, conn, "_superagent")

	itemID := sendPrompt(t, conn, "_superagent", "r1", "hello", nil)

	terminal := readTerminal(t, conn, itemID)
	if terminal.Outcome != apitypes.ItemFailed {
		t.Fatalf("expected a failed outcome, got %q", terminal.Outcome)
	}
	if len(terminal.Rows) != 1 || terminal.Rows[0].Content == "" {
		t.Fatalf("expected one error row on the terminal event, got %+v", terminal.Rows)
	}
	if !strings.Contains(terminal.Rows[0].Content, "no thread source configured") {
		t.Fatalf("error row should name the missing thread source, got %q", terminal.Rows[0].Content)
	}
}

func TestPromptResolverErrorReachesWire(t *testing.T) {
	resolve := func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{}, errors.New("read provider p-1: no rows in result set")
	}
	g := dialJournalGateway(t, &gateway.Config{ResolveModel: resolve, GetAgentThread: emptyThread}, "_superagent")
	conn := g.dial(t)
	syncConn(t, conn, "_superagent")

	itemID := sendPrompt(t, conn, "_superagent", "r1", "hello", nil)

	// A resolver failure — provider missing from the store — reaches
	// the submitting connection as the failed item's error row.
	terminal := readTerminal(t, conn, itemID)
	if terminal.Outcome != apitypes.ItemFailed || len(terminal.Rows) != 1 {
		t.Fatalf("expected a failed outcome with one error row, got %q %+v", terminal.Outcome, terminal.Rows)
	}
	if !strings.Contains(terminal.Rows[0].Content, "read provider p-1") {
		t.Fatalf("error row should carry the resolver failure, got %q", terminal.Rows[0].Content)
	}
}

// The sync handshake's ready frame reports the lane: an agent with no
// worker and no work is inactive, not reasoning, and counts only the
// connections registered before this one (the frame is captured before
// the synchronizing connection itself joins).
func TestSyncReadyReportsIdleLane(t *testing.T) {
	g := dialJournalGateway(t, &gateway.Config{GetAgentThread: emptyThread}, "_superagent")
	conn := g.dial(t)
	ready := syncConn(t, conn, "_superagent")

	if ready.Cursor != headCursor(t, g.turns, "_superagent") {
		t.Fatalf("ready cursor %q is not the committed head", ready.Cursor)
	}
	lane := ready.Lane
	if lane.Active || lane.Working || lane.Reasoning {
		t.Errorf("idle lane must be inactive: %+v", lane)
	}
	if lane.Model != "" {
		t.Errorf("idle lane should report no model, got %q", lane.Model)
	}
	if len(ready.Queue.Items) != 0 || ready.Queue.Active != nil || ready.Queue.Next != nil || ready.Queue.Paused {
		t.Errorf("ready queue should be empty: %+v", ready.Queue)
	}
}

// A sync frame binds the socket to one agent exactly like a prompt: a
// cross-agent frame afterwards is rejected, not a silent re-bind.
func TestSyncBindsAgentLikePrompt(t *testing.T) {
	g := dialJournalGateway(t, &gateway.Config{GetAgentThread: emptyThread}, "agent-a", "agent-b")
	conn := g.dial(t)
	if ready := syncConn(t, conn, "agent-a"); ready.Cursor == "" {
		t.Fatal("sync_ready should carry a cursor")
	}

	// A prompt for agent B on the same socket is dropped; then a prompt
	// for the bound agent is admitted. The first queue_result to arrive
	// must answer the bound prompt — the cross-agent frame is never
	// answered.
	writeFrame(t, conn, map[string]any{
		"type": "prompt", "agent_id": "agent-b",
		"data": map[string]any{"request_id": "r1", "content": "hi"},
	})
	writeFrame(t, conn, map[string]any{
		"type": "prompt", "agent_id": "agent-a",
		"data": map[string]any{"request_id": "r2", "content": "hi"},
	})
	for {
		ev := readEvent(t, conn)
		if ev.Type != wstypes.EventTypeQueueResult {
			continue
		}
		res := payload[wstypes.QueueResultEvent](t, ev)
		if res.RequestID == "r1" {
			t.Fatal("cross-agent prompt must not be answered")
		}
		if res.RequestID != "r2" {
			continue
		}
		if !res.Accepted {
			t.Fatalf("bound-agent prompt must be accepted, got %+v", res)
		}
		return
	}
}

// The stop frame binds the socket to one agent like every other frame:
// a connection joined to agent B must not stop agent A's running turn.
// The pin holds the model mid-stream, sends the cross-agent interrupt,
// and requires A's answer to settle normally.
func TestInterruptBindsAgentLikePrompt(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	finishStream := func() { releaseOnce.Do(func() { close(release) }) }
	defer finishStream()
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		close(started)
		<-release
		_, _ = w.Write([]byte("data: " + sseFrame("unstopped") + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer llm.Close()

	g := dialJournalGateway(t, &gateway.Config{
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
	}, "agent-a", "agent-b")
	// One gateway, two sockets: connA drives agent A's turn, connB is
	// bound to agent B — the cross-agent frame travels one manager.
	connA := g.dial(t)
	connB := g.dial(t)
	syncConn(t, connA, "agent-a")
	syncConn(t, connB, "agent-b")

	// Start agent A's turn on connA and wait for the model call.
	itemID := sendPrompt(t, connA, "agent-a", "r1", "hi", nil)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("model call never started")
	}

	// connB is bound to agent B: its interrupt for agent A is a
	// cross-agent frame and must be dropped, not forwarded.
	writeFrame(t, connB, map[string]string{"type": "interrupt", "agent_id": "agent-a"})
	finishStream()

	// A's turn settles normally: the terminal event is a completion
	// carrying the reply, not a stop and not a failure.
	terminal := readTerminal(t, connA, itemID)
	if terminal.Outcome != apitypes.ItemCompleted {
		t.Fatalf("turn must settle as completed, got %q (%+v)", terminal.Outcome, terminal.Rows)
	}
	if len(terminal.Rows) == 0 || terminal.Rows[len(terminal.Rows)-1].Content != "unstopped" {
		t.Fatalf("expected the unstopped reply, got %+v", terminal.Rows)
	}
}

// Prompt content is validated at admission: whitespace-only and
// over-length prompts are rejected through queue_result with no queue
// state change, and admitted content is trimmed.
func TestPromptContentValidated(t *testing.T) {
	g := dialJournalGateway(t, &gateway.Config{GetAgentThread: emptyThread}, "_superagent")
	conn := g.dial(t)
	syncConn(t, conn, "_superagent")

	write := func(requestID, content string) {
		t.Helper()
		writeFrame(t, conn, map[string]any{
			"type": "prompt", "agent_id": "_superagent",
			"data": map[string]any{"request_id": requestID, "content": content},
		})
	}

	write("ws", "   ")
	if res, _ := readAdmission(t, conn, "ws", false); res.Accepted {
		t.Fatalf("whitespace prompt must be rejected, got %+v", res)
	} else if res.Error == "" {
		t.Fatal("rejection must carry a reason")
	}

	write("long", strings.Repeat("x", 64<<10+1))
	if res, _ := readAdmission(t, conn, "long", false); res.Accepted {
		t.Fatalf("over-length prompt must be rejected, got %+v", res)
	}

	write("trim", "  hi  ")
	res, snapshots := readAdmission(t, conn, "trim", true)
	if !res.Accepted || res.ItemID == "" {
		t.Fatalf("trimmable prompt must be accepted, got %+v", res)
	}
	// The admitted item may surface in a snapshot on either side of the
	// reply or only as the active item once claimed; matching by the
	// trimmed content proves admission trimmed it.
	itemNamed := func(snapshot wstypes.QueueStateEvent) bool {
		if snapshot.Active != nil && snapshot.Active.Content == "hi" {
			return true
		}
		for _, item := range snapshot.Items {
			if item.Content == "hi" {
				return true
			}
		}
		return false
	}
	for {
		for _, snapshot := range snapshots {
			if itemNamed(snapshot) {
				return
			}
		}
		snapshots = nil
		ev := readEvent(t, conn)
		if ev.Type != wstypes.EventTypeQueueState {
			continue
		}
		snapshots = append(snapshots, payload[wstypes.QueueStateEvent](t, ev))
	}
}

// A device arriving mid-turn synchronizes into the live conversation:
// the snapshot baseline, then the ready frame with the queue's active
// item and the in-flight prefix, before the chunks still to come. The
// prefix's sequence is what the next chunk continues.
func TestSyncHandshakeMidTurnJoinsPrefix(t *testing.T) {
	release := make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: " + sseFrame("par ") + "\n\n"))
		flusher.Flush()
		<-release
		_, _ = w.Write([]byte("data: " + sseFrame("tial") + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer llm.Close()

	g := dialJournalGateway(t, &gateway.Config{
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
	}, "_superagent")
	conn1 := g.dial(t)
	syncConn(t, conn1, "_superagent")

	itemID := sendPrompt(t, conn1, "_superagent", "r1", "hello", nil)

	// conn1 confirms the first chunk is on the wire; the prefix is now
	// replayable state.
	var first wstypes.MessageChunkEvent
	for {
		ev := readEvent(t, conn1)
		if ev.Type != wstypes.EventTypeMessageChunk {
			continue
		}
		first = payload[wstypes.MessageChunkEvent](t, ev)
		break
	}
	if first.Delta != "par " || first.Seq != 1 || first.ID != itemID {
		t.Fatalf("unexpected first chunk: %+v", first)
	}

	// conn2 arrives mid-turn: the snapshot carries the committed user
	// row at the committed head.
	conn2 := g.dial(t)
	snapEv := syncAnswer(t, conn2, "_superagent", "")
	if snapEv.Type != wstypes.EventTypeSyncSnapshot {
		t.Fatalf("fresh cursor must answer sync_snapshot, got %v", snapEv.Type)
	}
	snap := payload[wstypes.SyncSnapshotEvent](t, snapEv)
	if snap.Cursor != headCursor(t, g.turns, "_superagent") {
		t.Fatalf("snapshot cursor %q is not the committed head", snap.Cursor)
	}
	var page apitypes.ThreadPage
	if err := json.Unmarshal(snap.Page, &page); err != nil {
		t.Fatalf("decoding snapshot page failed: %v", err)
	}
	if page.ThreadID != activeThreadID(t, g.sqlDB, "_superagent") {
		t.Fatalf("snapshot page thread = %q, want the agent's active thread", page.ThreadID)
	}
	if len(page.Items) != 1 || page.Items[0].Role != apitypes.ThreadRoleUser || page.Items[0].Content != "hello" {
		t.Fatalf("snapshot page should carry the committed user row, got %+v", page.Items)
	}

	// The caught-up step ends the handshake with the runtime snapshot:
	// the queue's active item names its execution thread and persisted
	// user row, and the prefix carries the streamed text and the
	// sequence it ends at.
	readyEv := syncAnswer(t, conn2, "_superagent", snap.Cursor)
	if readyEv.Type != wstypes.EventTypeSyncReady {
		t.Fatalf("caught-up cursor must answer sync_ready, got %v", readyEv.Type)
	}
	ready := payload[wstypes.SyncReadyEvent](t, readyEv)
	if ready.Queue.Active == nil {
		t.Fatal("ready queue must name the running item")
	}
	active := ready.Queue.Active
	if active.ID != itemID || active.Kind != wstypes.QueueItemKindPrompt {
		t.Fatalf("ready active item = %+v, want the running prompt %s", active, itemID)
	}
	if active.UserMessageID != page.Items[0].ID {
		t.Fatalf("ready active user row %q is not the row the snapshot showed (%q)", active.UserMessageID, page.Items[0].ID)
	}
	if len(ready.Queue.Items) != 0 || ready.Queue.Paused {
		t.Fatalf("ready queue should hold only the running item: %+v", ready.Queue)
	}
	if ready.Prefix == nil {
		t.Fatal("mid-turn ready must carry the in-flight prefix")
	}
	if ready.Prefix.ItemID != itemID || ready.Prefix.Text != "par " || ready.Prefix.LastSeq != 1 {
		t.Fatalf("ready prefix = %+v, want the streamed prefix of %s", ready.Prefix, itemID)
	}
	if !ready.Lane.Working {
		t.Fatalf("ready lane must report the running turn: %+v", ready.Lane)
	}

	close(release)

	// The live remainder follows the prefix: one more chunk continuing
	// the sequence, then the settled answer.
	for {
		ev := readEvent(t, conn2)
		if ev.Type != wstypes.EventTypeMessageChunk {
			continue
		}
		chunk := payload[wstypes.MessageChunkEvent](t, ev)
		if chunk.Delta != "tial" {
			t.Fatalf("expected the streamed remainder, got %q", chunk.Delta)
		}
		if chunk.Seq != 2 {
			t.Fatalf("the live chunk must continue the prefix's sequence, got %d", chunk.Seq)
		}
		break
	}
	terminal := readTerminal(t, conn2, itemID)
	if terminal.Outcome != apitypes.ItemCompleted || len(terminal.Rows) == 0 ||
		terminal.Rows[len(terminal.Rows)-1].Content != "par tial" {
		t.Fatalf("expected the settled answer %q, got %q %+v", "par tial", terminal.Outcome, terminal.Rows)
	}
}

// Server shutdown closes live connections with a GoingAway frame —
// the polite goodbye that tells clients not to reconnect instantly.
// The request context is cancelled mid-connection, exactly what a
// draining server does to its long-lived handlers.
func TestShutdownClosesConnectionsGoingAway(t *testing.T) {
	gw, err := gateway.New(&gateway.Config{GetAgentThread: emptyThread}, zerolog.Nop())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		time.AfterFunc(50*time.Millisecond, cancel)
		gw.ServeHTTP(w, r.WithContext(ctx))
	})
	server := httptest.NewServer(wrapped)
	defer server.Close()

	conn, ctx := dialServer(t, server.URL)

	// The handler's read loop unblocks on the cancelled context and
	// closes with GoingAway; the client sees the close frame.
	var event map[string]any
	err = wsjson.Read(ctx, conn, &event)
	if err == nil {
		t.Fatal("expected a close, got an event")
	}
	if got := coderws.CloseStatus(err); got != coderws.StatusGoingAway {
		t.Fatalf("expected GoingAway close, got status %v (%v)", got, err)
	}
}

// The live lane reports the submitted model while the turn runs: the
// working broadcast carries it.
func TestLiveLaneReportsModel(t *testing.T) {
	g := dialJournalGateway(t, &gateway.Config{GetAgentThread: emptyThread}, "_superagent")
	conn := g.dial(t)
	syncConn(t, conn, "_superagent")

	itemID := sendPrompt(t, conn, "_superagent", "r1", "hello", map[string]string{"id": "gpt-4o", "provider_id": "p-1"})
	if itemID == "" {
		t.Fatal("prompt must be admitted")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		ev := readEvent(t, conn)
		if ev.Type != wstypes.EventTypeLaneStatus {
			continue
		}
		lane := payload[wstypes.LaneStatus](t, ev)
		if lane.Active && lane.Working && lane.Model == "gpt-4o" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("live lane never reported the submitted model: %+v", lane)
		}
	}
}

// TestIdleConnectionSurvivesKeepalive proves the pre-keepalive failure
// mode is gone: a connection that sends nothing stays fully usable far
// past the old 60s read-idle drop, while the ping ticker runs against
// it the whole time (the client auto-pongs; a missed pong closes us).
func TestIdleConnectionSurvivesKeepalive(t *testing.T) {
	interval := 50 * time.Millisecond
	g := dialJournalGateway(t, &gateway.Config{
		PingInterval:   interval,
		GetAgentThread: emptyThread,
	}, "_superagent")
	conn := g.dial(t)
	syncConn(t, conn, "_superagent")

	// Idle for many ping cycles — hundreds of pings with no inbound
	// frames from this side.
	time.Sleep(300 * time.Millisecond)

	// Still fully usable: a mutation admits after the idle stretch.
	if itemID := sendPrompt(t, conn, "_superagent", "after-idle", "hi", nil); itemID == "" {
		t.Fatal("prompt after idle was not admitted")
	}
}

// TestDeadPeerReaped proves a peer that stops answering pings without a
// TCP close (half-open: sleep, NAT drop) is closed by the gateway
// within one ping cycle + pong wait.
func TestDeadPeerReaped(t *testing.T) {
	interval := 50 * time.Millisecond
	cfg := &gateway.Config{PingInterval: interval, GetAgentThread: emptyThread}
	handler, err := gateway.New(cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// Raw TCP + by-hand upgrade: a real client library would answer the
	// server's pings itself; this peer never reads a byte.
	nc, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("tcp dial failed: %v", err)
	}
	t.Cleanup(func() { _ = nc.Close() })

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	req := "GET /ws HTTP/1.1\r\n" +
		"Host: " + nc.RemoteAddr().String() + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := nc.Write([]byte(req)); err != nil {
		t.Fatalf("upgrade write failed: %v", err)
	}

	// Read the 101 handshake response, then consume the connected-event
	// frame the handler writes right after accept. From here on this
	// peer never reads a byte — no pongs.
	reader := bufio.NewReader(nc)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("handshake read failed: %v", err)
	}
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("expected 101 upgrade, got %q", statusLine)
	}

	// Server frames are unmasked: 2-byte header + ~66-byte payload.
	if err := nc.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
		t.Fatalf("setting drain deadline failed: %v", err)
	}
	if _, err := io.ReadFull(reader, make([]byte, 96)); err != nil {
		t.Fatalf("draining connected event failed: %v", err)
	}

	// The gateway must reap this silent peer within interval (first
	// ping) + pong wait (interval*10) + slack. The wire meanwhile
	// carries ping frames (opcode 0x9) — only a close frame (0x8) or
	// a FIN counts as reaped.
	reap := interval*12 + time.Second
	if err := nc.SetReadDeadline(time.Now().Add(reap)); err != nil {
		t.Fatalf("setting reap deadline failed: %v", err)
	}
	buf := make([]byte, 64)
	for {
		n, rerr := nc.Read(buf)
		if rerr != nil {
			if errors.Is(rerr, os.ErrDeadlineExceeded) {
				t.Fatal("dead peer not reaped in time")
			}
			break // EOF counts as reaped
		}
		if n == 0 {
			continue
		}
		if opcode := buf[0] & 0x0f; opcode == 0x8 {
			break // close frame: reaped
		}
	}
}

// emptyThread is the thread seam for gateway-level tests that run
// turns without a store: no history rides the model call.
func emptyThread(context.Context, string) ([]backend.AgentThreadMessage, error) {
	return nil, nil
}

// Submission classification at the wire boundary: a recognized
// command with arguments is rejected and changes no queue state, an
// exact command admits as a command, and slash text that names no
// command is an ordinary prompt.
func TestCommandAdmission(t *testing.T) {
	g := dialJournalGateway(t, &gateway.Config{GetAgentThread: emptyThread}, "_superagent")
	conn := g.dial(t)
	syncConn(t, conn, "_superagent")

	write := func(requestID, content string) {
		t.Helper()
		writeFrame(t, conn, map[string]any{
			"type": "prompt", "agent_id": "_superagent",
			"data": map[string]any{"request_id": requestID, "content": content},
		})
	}
	// kindOf reports the kind of the item carrying content in one queue
	// snapshot: it is either still pending or already claimed.
	kindOf := func(snapshot wstypes.QueueStateEvent, content string) (string, bool) {
		if snapshot.Active != nil && snapshot.Active.Content == content {
			return snapshot.Active.Kind, true
		}
		for _, item := range snapshot.Items {
			if item.Content == content {
				return item.Kind, true
			}
		}
		return "", false
	}
	// admittedKind admits one prompt and reads its snapshot kind.
	// Every item carries one — there is no zero-value spelling.
	admittedKind := func(requestID, content string) string {
		t.Helper()
		res, snapshots := readAdmission(t, conn, requestID, true)
		if !res.Accepted {
			t.Fatalf("prompt %s must be accepted, got %+v", requestID, res)
		}
		for {
			for _, snapshot := range snapshots {
				kind, found := kindOf(snapshot, content)
				if !found {
					continue
				}
				if kind == "" {
					t.Fatalf("item %q carries no kind", content)
				}
				return kind
			}
			snapshots = nil
			ev := readEvent(t, conn)
			if ev.Type != wstypes.EventTypeQueueState {
				continue
			}
			snapshots = append(snapshots, payload[wstypes.QueueStateEvent](t, ev))
		}
	}

	// Arguments — after a space or a newline — reject the command: it
	// is a bare switch, never a switch with a prompt.
	for _, tc := range []struct{ requestID, content, want string }{
		{"args", "/new now", "/new takes no arguments; submit /new alone"},
		{"reset", "/reset extra", "/reset takes no arguments; submit /reset alone"},
		{"newline", "/new\nnow", "/new takes no arguments; submit /new alone"},
	} {
		write(tc.requestID, tc.content)
		res, _ := readAdmission(t, conn, tc.requestID, false)
		if res.Accepted {
			t.Fatalf("%q must be rejected, got %+v", tc.content, res)
		}
		if res.Error != tc.want {
			t.Fatalf("%q rejection reason = %q, want %q", tc.content, res.Error, tc.want)
		}
	}

	// /shake and /compact carry the same no-argument contract, spelled
	// with their own names in the rejection.
	write("shake-args", "/shake now")
	if res, _ := readAdmission(t, conn, "shake-args", false); res.Accepted ||
		res.Error != "/shake takes no arguments; submit /shake alone" {
		t.Fatalf("/shake now = %+v, want its own command-argument rejection", res)
	}
	write("compact-args", "/compact now")
	if res, _ := readAdmission(t, conn, "compact-args", false); res.Accepted ||
		res.Error != "/compact takes no arguments; submit /compact alone" {
		t.Fatalf("/compact now = %+v, want its own command-argument rejection", res)
	}

	// Only the exact spellings are commands: unknown slash text, a
	// case variant, a longer word, and embedded text are prompts.
	for requestID, content := range map[string]string{
		"near":     "/n",
		"case":     "/New",
		"longer":   "/newer",
		"embedded": "hi I want /new",
		"near-c":   "/compaction",
	} {
		write(requestID, content)
		if kind := admittedKind(requestID, content); kind != wstypes.QueueItemKindPrompt {
			t.Fatalf("%q kind = %q, want prompt", content, kind)
		}
	}

	// The exact spelling admits as a command item.
	write("new", "/new")
	if kind := admittedKind("new", "/new"); kind != wstypes.QueueItemKindNewThread {
		t.Fatalf("/new kind = %q, want new_thread", kind)
	}
	write("shake", "/shake")
	if kind := admittedKind("shake", "/shake"); kind != wstypes.QueueItemKindShake {
		t.Fatalf("/shake kind = %q, want shake", kind)
	}
	write("compact", "/compact")
	if kind := admittedKind("compact", "/compact"); kind != wstypes.QueueItemKindCompact {
		t.Fatalf("/compact kind = %q, want compact", kind)
	}
}

// The handshake's full ordering: a fresh connection with no cursor
// receives the snapshot baseline at the committed head; a reconnecting
// cursor receives one batch per request — only what it missed, in
// position order, byte-equal with the journal's own replay — and the
// caught-up request ends in sync_ready at the head.
func TestSyncHandshakeSnapshotThenBatchesThenReady(t *testing.T) {
	llm := scriptedLLM(t, "answer-1", "answer-2", "answer-3")
	g := dialJournalGateway(t, &gateway.Config{
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
	}, "_superagent")
	conn1 := g.dial(t)
	syncConn(t, conn1, "_superagent")

	// Three committed turns; the checkpoint after the first is the
	// reconnecting client's stored cursor.
	runTurn(t, conn1, "_superagent", "t1", "one")
	midCursor := headCursor(t, g.turns, "_superagent")
	runTurn(t, conn1, "_superagent", "t2", "two")

	// A fresh device with no cursor: the snapshot names the committed
	// head and carries the newest display page.
	conn2 := g.dial(t)
	snapEv := syncAnswer(t, conn2, "_superagent", "")
	if snapEv.Type != wstypes.EventTypeSyncSnapshot {
		t.Fatalf("fresh cursor must answer sync_snapshot, got %v", snapEv.Type)
	}
	snap := payload[wstypes.SyncSnapshotEvent](t, snapEv)
	if snap.Cursor != headCursor(t, g.turns, "_superagent") {
		t.Fatalf("snapshot cursor %q is not the committed head", snap.Cursor)
	}
	var page apitypes.ThreadPage
	if err := json.Unmarshal(snap.Page, &page); err != nil {
		t.Fatalf("decoding snapshot page failed: %v", err)
	}
	if len(page.Items) != 4 {
		t.Fatalf("snapshot page should carry both turns' rows, got %+v", page.Items)
	}
	for i, want := range []struct {
		role    apitypes.ThreadRole
		content string
	}{
		{apitypes.ThreadRoleUser, "one"},
		{apitypes.ThreadRoleAssistant, "answer-1"},
		{apitypes.ThreadRoleUser, "two"},
		{apitypes.ThreadRoleAssistant, "answer-2"},
	} {
		if page.Items[i].Role != want.role || page.Items[i].Content != want.content {
			t.Fatalf("snapshot row %d = %s %q, want %s %q", i, page.Items[i].Role, page.Items[i].Content, want.role, want.content)
		}
	}

	// The reconnecting client at midCursor receives only what it
	// missed: turn two's batch, equal with the journal's replay.
	batchEv := syncAnswer(t, conn2, "_superagent", midCursor)
	if batchEv.Type != wstypes.EventTypeSyncBatch {
		t.Fatalf("stale cursor must answer sync_batch, got %v", batchEv.Type)
	}
	batch := payload[wstypes.LifecycleBatchEvent](t, batchEv)
	// One batch answers the whole retained remainder: the comparison
	// below is against every event after the cursor.
	replayed := decodeEvents(t, batch.Events)
	if !reflect.DeepEqual(replayed, journalEvents(t, g.turns, "_superagent", 3)) {
		t.Fatalf("replayed batch %+v is not the journal's committed history after position 3", replayed)
	}

	// Another turn commits while the device is between requests: the
	// next sync answers another batch, still only what is missing.
	runTurn(t, conn1, "_superagent", "t3", "three")
	batchEv2 := syncAnswer(t, conn2, "_superagent", batch.Cursor)
	if batchEv2.Type != wstypes.EventTypeSyncBatch {
		t.Fatalf("cursor behind the head must answer sync_batch, got %v", batchEv2.Type)
	}
	batch2 := payload[wstypes.LifecycleBatchEvent](t, batchEv2)
	replayed = append(replayed, decodeEvents(t, batch2.Events)...)

	// The concatenated batches equal the committed history in position
	// order — exactly what the client missed and nothing more.
	if !reflect.DeepEqual(replayed, journalEvents(t, g.turns, "_superagent", 3)) {
		t.Fatalf("concatenated replay %+v is not the committed history after position 3", replayed)
	}
	for i := 1; i < len(replayed); i++ {
		if replayed[i-1].Pos >= replayed[i].Pos {
			t.Fatalf("replayed positions are not strictly increasing: %d then %d", replayed[i-1].Pos, replayed[i].Pos)
		}
	}

	// Caught up: the next request ends the handshake at the head.
	readyEv := syncAnswer(t, conn2, "_superagent", batch2.Cursor)
	if readyEv.Type != wstypes.EventTypeSyncReady {
		t.Fatalf("caught-up cursor must answer sync_ready, got %v", readyEv.Type)
	}
	ready := payload[wstypes.SyncReadyEvent](t, readyEv)
	if ready.Cursor != headCursor(t, g.turns, "_superagent") {
		t.Fatalf("ready cursor %q is not the committed head %q", ready.Cursor, headCursor(t, g.turns, "_superagent"))
	}
}

// A second connection that appears only after a turn committed still
// receives that turn's events from the journal alone — the
// commit-without-broadcast gap closes on the recovering side.
func TestSecondConnectionReceivesCommittedEventsFromJournal(t *testing.T) {
	llm := scriptedLLM(t, "answer-1")
	g := dialJournalGateway(t, &gateway.Config{
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
	}, "_superagent")
	conn1 := g.dial(t)
	syncConn(t, conn1, "_superagent")
	terminal := runTurn(t, conn1, "_superagent", "t1", "hello")
	itemID := terminal.ItemID

	// The device's stored cursor predates the turn, so recovery replays
	// the whole transaction history instead of a display snapshot.
	conn2 := g.dial(t)
	batchEv := syncAnswer(t, conn2, "_superagent", cursorAt(t, g.turns, "_superagent", 0))
	if batchEv.Type != wstypes.EventTypeSyncBatch {
		t.Fatalf("cursor behind the head must answer sync_batch, got %v", batchEv.Type)
	}
	batch := payload[wstypes.LifecycleBatchEvent](t, batchEv)
	events := decodeEvents(t, batch.Events)
	if !reflect.DeepEqual(events, journalEvents(t, g.turns, "_superagent", 0)) {
		t.Fatalf("replayed batch %+v is not the journal's committed history", events)
	}
	if len(events) != 3 {
		t.Fatalf("one completed turn journals three events, got %d", len(events))
	}
	wantKinds := []string{
		apitypes.LifecycleItemAccepted,
		apitypes.LifecycleItemStarted,
		apitypes.LifecycleItemFinished,
	}
	for i, kind := range wantKinds {
		if events[i].Kind != kind {
			t.Fatalf("event %d is %q, want %q", i, events[i].Kind, kind)
		}
		if events[i].ItemID != itemID {
			t.Fatalf("event %d names item %q, want %q", i, events[i].ItemID, itemID)
		}
	}
	if events[0].Content != "hello" || events[0].ItemKind != wstypes.QueueItemKindPrompt {
		t.Fatalf("acceptance event = %+v, want the submitted prompt", events[0])
	}
	if events[1].UserMessageID == "" || len(events[1].Rows) != 1 ||
		events[1].Rows[0].Role != apitypes.ThreadRoleUser || events[1].Rows[0].Content != "hello" {
		t.Fatalf("start event = %+v, want the committed user row", events[1])
	}
	if events[2].Outcome != apitypes.ItemCompleted || len(events[2].Rows) != 1 ||
		events[2].Rows[0].Content != "answer-1" {
		t.Fatalf("terminal event = %+v, want the committed answer", events[2])
	}

	// Recovery then reaches the live state at the committed head, and
	// the feed stays quiet — nothing else was missed.
	ready, _ := syncHandshake(t, conn2, "_superagent", batch.Cursor)
	if ready.Cursor != headCursor(t, g.turns, "_superagent") {
		t.Fatalf("ready cursor %q is not the committed head", ready.Cursor)
	}
	drainSilent(t, conn2, 150*time.Millisecond)
}

// Mutations before sync_ready are rejected: a peer that has not
// finished recovery cannot act on a conversation it has not yet seen.
// Prompts and send_now answers carry the not-synchronized rejection;
// an interrupt is dropped; after the handshake the same mutations are
// admitted.
func TestMutationsBeforeSyncRejected(t *testing.T) {
	g := dialJournalGateway(t, &gateway.Config{GetAgentThread: emptyThread}, "_superagent")
	conn := g.dial(t)

	writeFrame(t, conn, map[string]any{
		"type": "prompt", "agent_id": "_superagent",
		"data": map[string]any{"request_id": "r1", "content": "hi"},
	})
	res, _ := readAdmission(t, conn, "r1", false)
	if res.Accepted {
		t.Fatalf("prompt before sync_ready must be rejected, got %+v", res)
	}
	if res.Error != "not synchronized; retry once the connection is live" {
		t.Fatalf("rejection reason = %q, want the not-synchronized message", res.Error)
	}

	writeFrame(t, conn, map[string]any{
		"type": "send_now", "agent_id": "_superagent",
		"data": map[string]any{"request_id": "r2", "item_id": "item-x"},
	})
	res, _ = readAdmission(t, conn, "r2", false)
	if res.Accepted || res.Error != "not synchronized; retry once the connection is live" {
		t.Fatalf("send_now before sync_ready must be rejected, got %+v", res)
	}

	// The interrupt is dropped entirely — no reply, no effect.
	writeFrame(t, conn, map[string]string{"type": "interrupt", "agent_id": "_superagent"})

	// The connection remains usable: the handshake completes and the
	// same mutation is admitted afterwards. The turn fails for the
	// missing resolver — a failure, not a stop, proving the dropped
	// interrupt never armed.
	syncConn(t, conn, "_superagent")
	itemID := sendPrompt(t, conn, "_superagent", "r3", "hi", nil)
	terminal := readTerminal(t, conn, itemID)
	if terminal.Outcome != apitypes.ItemFailed {
		t.Fatalf("post-sync turn outcome = %q, want failed (the pre-sync interrupt must not stop it)", terminal.Outcome)
	}
}

// An unusable cursor — malformed, from a foreign history, or ahead of
// the committed head — answers the authoritative snapshot instead of a
// replay, never disclosing this database's positions.
func TestSyncUnusableCursorAnswersSnapshot(t *testing.T) {
	llm := scriptedLLM(t, "answer-1")
	g := dialJournalGateway(t, &gateway.Config{
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
	}, "_superagent")
	conn1 := g.dial(t)
	syncConn(t, conn1, "_superagent")
	runTurn(t, conn1, "_superagent", "t1", "hello")
	head := headCursor(t, g.turns, "_superagent")

	conn2 := g.dial(t)
	ask := func(cursor string) wstypes.SyncSnapshotEvent {
		t.Helper()
		writeFrame(t, conn2, map[string]any{
			"type": "sync", "agent_id": "_superagent",
			"data": map[string]any{"cursor": cursor},
		})
		ev := readEvent(t, conn2)
		if ev.Type != wstypes.EventTypeSyncSnapshot {
			t.Fatalf("cursor %q must answer sync_snapshot, got %v", cursor, ev.Type)
		}
		return payload[wstypes.SyncSnapshotEvent](t, ev)
	}
	for _, tc := range []struct {
		name   string
		cursor string
	}{
		{"malformed", "not-a-cursor"},
		{"foreign history", backend.FormatCursor("0f3b1e68-0000-4000-8000-000000000000", 1)},
		{"ahead of head", cursorAt(t, g.turns, "_superagent", 1<<20)},
	} {
		snap := ask(tc.cursor)
		if snap.Cursor != head {
			t.Fatalf("%s: snapshot cursor %q is not the committed head %q", tc.name, snap.Cursor, head)
		}
	}
}

// racingJournal injects one journal commit between the client's last
// replay batch and the hand-off — exactly the window the ready gap
// must close. Everything else delegates to the real service.
type racingJournal struct {
	*backend.TurnService
	agentID string
	// atPos arms the race; -1 keeps it disarmed (journal positions are
	// never negative).
	atPos int64
	fired bool
}

func (j *racingJournal) ReplayAfter(ctx context.Context, agentID, historyID string, afterPos int64, limit int) (*backend.JournalPage, error) {
	if agentID == j.agentID && !j.fired && afterPos == j.atPos {
		j.fired = true
		// An event commits after the client's last batch but before
		// the hand-off could claim readiness at that position.
		if _, err := j.AcceptItem(ctx, agentID, "race-item", "raced", wstypes.QueueItemKindPrompt, nil); err != nil {
			return nil, err
		}
	}
	return j.TurnService.ReplayAfter(ctx, agentID, historyID, afterPos, limit)
}

// Gap-free hand-off: an event committed between the reconnecting
// client's last batch and its readiness claim is delivered as another
// batch — never skipped by a ready frame that would leave the client
// behind the head. The race is driven deterministically by committing
// inside the replay step the hand-off follows.
func TestSyncHandOffClosesCommitGap(t *testing.T) {
	llm := scriptedLLM(t, "answer-1", "answer-2", "answer-3")
	_, turns := journalBackend(t, "_superagent")
	race := &racingJournal{TurnService: turns, agentID: "_superagent", atPos: -1}
	g := dialJournalGateway(t, &gateway.Config{
		Journal:        race,
		GetAgentThread: emptyThread,
		ResolveModel:   fixedResolver(llm.URL),
	})

	conn1 := g.dial(t)
	syncConn(t, conn1, "_superagent")
	runTurn(t, conn1, "_superagent", "t1", "one")
	runTurn(t, conn1, "_superagent", "t2", "two")
	runTurn(t, conn1, "_superagent", "t3", "three")

	// The client reconnects from zero; the next caught-up probe races a
	// fresh commit.
	_, head, _, err := turns.JournalStatus(context.Background(), "_superagent")
	if err != nil {
		t.Fatalf("journal status failed: %v", err)
	}
	race.atPos = head

	conn2 := g.dial(t)
	batchEv := syncAnswer(t, conn2, "_superagent", cursorAt(t, turns, "_superagent", 0))
	if batchEv.Type != wstypes.EventTypeSyncBatch {
		t.Fatalf("cursor behind the head must answer sync_batch, got %v", batchEv.Type)
	}
	batch := payload[wstypes.LifecycleBatchEvent](t, batchEv)
	replayed := decodeEvents(t, batch.Events)

	// The caught-up step would hand off — but an event just committed,
	// so the answer is another batch carrying it, not a ready frame.
	racedEv := syncAnswer(t, conn2, "_superagent", batch.Cursor)
	if racedEv.Type != wstypes.EventTypeSyncBatch {
		t.Fatalf("a commit racing the hand-off must answer sync_batch, got %v", racedEv.Type)
	}
	raced := payload[wstypes.LifecycleBatchEvent](t, racedEv)
	racedEvents := decodeEvents(t, raced.Events)
	if len(racedEvents) != 1 || racedEvents[0].Kind != apitypes.LifecycleItemAccepted || racedEvents[0].ItemID != "race-item" {
		t.Fatalf("the racing batch should carry exactly the raced acceptance, got %+v", racedEvents)
	}
	replayed = append(replayed, racedEvents...)

	// Nothing was lost: the concatenated batches are the committed
	// history in position order.
	if !reflect.DeepEqual(replayed, journalEvents(t, turns, "_superagent", 0)) {
		t.Fatalf("concatenated replay %+v is not the committed history", replayed)
	}

	// The next step is genuinely caught up and ends at the head.
	readyEv := syncAnswer(t, conn2, "_superagent", raced.Cursor)
	if readyEv.Type != wstypes.EventTypeSyncReady {
		t.Fatalf("caught-up cursor must answer sync_ready, got %v", readyEv.Type)
	}
	ready := payload[wstypes.SyncReadyEvent](t, readyEv)
	if ready.Cursor != headCursor(t, turns, "_superagent") {
		t.Fatalf("ready cursor %q is not the committed head %q", ready.Cursor, headCursor(t, turns, "_superagent"))
	}
}

// failingReplay wraps the real journal with a ReplayAfter that always
// errors — an unexpected store failure, not a classified cursor.
type failingReplay struct {
	*backend.TurnService
	err error
}

func (j *failingReplay) ReplayAfter(context.Context, string, string, int64, int) (*backend.JournalPage, error) {
	return nil, j.err
}

// An unexpected replay failure is answered and the connection closes:
// never a socket left open in 'syncing', silently stuck.
func TestSyncReplayFailureAnswersAndCloses(t *testing.T) {
	_, turns := journalBackend(t, "_superagent")
	history, _, _, err := turns.JournalStatus(context.Background(), "_superagent")
	if err != nil {
		t.Fatalf("journal status: %v", err)
	}
	g := dialJournalGateway(t, &gateway.Config{
		Journal:        &failingReplay{TurnService: turns, err: errors.New("disk on fire")},
		GetAgentThread: emptyThread,
	})
	conn := g.dial(t)
	writeFrame(t, conn, map[string]any{
		"type": "sync", "agent_id": "_superagent",
		"data": map[string]any{"cursor": backend.FormatCursor(history, 0)},
	})
	ev := readEvent(t, conn)
	if ev.Type != wstypes.EventTypeError {
		t.Fatalf("replay failure must answer an error frame, got %v", ev.Type)
	}
	if got := payload[wstypes.ErrorEvent](t, ev).Message; got != "sync failed: disk on fire" {
		t.Fatalf("error message = %q, want the failure surfaced", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var dead wstypes.Event
	if err := wsjson.Read(ctx, conn, &dead); err == nil {
		t.Fatal("the connection must close after a sync failure")
	}
}
