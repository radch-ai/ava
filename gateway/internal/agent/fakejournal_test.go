package agent

// An in-memory lifecycle journal for unit tests that exercise queue and
// recorder mechanics without storage: item transitions, journal positions,
// and the drafts each commit carried are recorded so a test can assert
// what a transition actually wrote.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
)

// fakeWrite is one committed row batch, as a test observes it.
type fakeWrite struct {
	AgentID string
	ItemID  string
	Kind    string
	Drafts  []backend.ThreadMessageDraft
	Rows    []apitypes.ThreadMessage
}

type fakeItem struct {
	agentID       string
	id            string
	kind          string
	content       string
	state         string
	threadID      string
	userMessageID string
}

type fakeJournal struct {
	mu      sync.Mutex
	history string
	heads   map[string]int64
	events  map[string][]apitypes.LifecycleEvent
	items   map[string]*fakeItem
	writes  []fakeWrite

	// Failure injection, per operation.
	failAccept   error
	failStart    error
	failAppend   error
	failTool     error
	failFinish   error
	failSwitch   error
	failShake    error
	failSnapshot error
}

func newFakeJournal() *fakeJournal {
	return &fakeJournal{
		history: "hist",
		heads:   make(map[string]int64),
		events:  make(map[string][]apitypes.LifecycleEvent),
		items:   make(map[string]*fakeItem),
	}
}

func fakeKey(agentID, itemID string) string { return agentID + "/" + itemID }

// appendLocked records one event and advances the agent's head.
func (f *fakeJournal) appendLocked(agentID, itemID string, ev apitypes.LifecycleEvent) *backend.JournalBatch {
	f.heads[agentID]++
	ev.Pos = f.heads[agentID]
	ev.BatchEnd = true
	f.events[agentID] = append(f.events[agentID], ev)
	return &backend.JournalBatch{
		Cursor: backend.FormatCursor(f.history, ev.Pos),
		Events: []apitypes.LifecycleEvent{ev},
	}
}

func (f *fakeJournal) AcceptItem(_ context.Context, agentID, itemID, content, kind string, model *apitypes.ModelEntry) (*backend.JournalBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAccept != nil {
		return nil, f.failAccept
	}
	f.items[fakeKey(agentID, itemID)] = &fakeItem{
		agentID: agentID, id: itemID, kind: kind, content: content, state: "accepted",
	}
	return f.appendLocked(agentID, itemID, apitypes.LifecycleEvent{
		Kind: apitypes.LifecycleItemAccepted, ItemID: itemID, ItemKind: kind, Content: content,
	}), nil
}

func (f *fakeJournal) StartItem(_ context.Context, agentID, itemID string) (*backend.ItemStart, *backend.JournalBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failStart != nil {
		return nil, nil, f.failStart
	}
	item := f.items[fakeKey(agentID, itemID)]
	if item == nil || item.state != "accepted" {
		return nil, nil, errors.New("item is not accepted")
	}
	item.state = "started"
	item.threadID = "thread-" + agentID
	ev := apitypes.LifecycleEvent{
		Kind: apitypes.LifecycleItemStarted, ItemID: itemID, ItemKind: item.kind, ThreadID: item.threadID,
	}
	start := &backend.ItemStart{}
	if item.kind == "prompt" {
		item.userMessageID = "user-" + itemID
		start.UserMessageID = item.userMessageID
		row := apitypes.ThreadMessage{ID: item.userMessageID, Role: apitypes.ThreadRoleUser, Content: item.content}
		ev.UserMessageID = item.userMessageID
		ev.Rows = []apitypes.ThreadMessage{row}
	}
	return start, f.appendLocked(agentID, itemID, ev), nil
}

func (f *fakeJournal) RecordToolStarted(_ context.Context, agentID, itemID, invocationID, callID, name, args string) (*backend.JournalBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failTool != nil {
		return nil, f.failTool
	}
	return f.appendLocked(agentID, itemID, apitypes.LifecycleEvent{
		Kind: apitypes.LifecycleToolStarted, ItemID: itemID,
		InvocationID: invocationID, CallID: callID, ToolName: name, ToolArgs: args,
	}), nil
}

func (f *fakeJournal) RecordToolFinished(_ context.Context, agentID, itemID, invocationID, result string, errText *string) (*backend.JournalBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failTool != nil {
		return nil, f.failTool
	}
	row := apitypes.ThreadMessage{ID: "tool-" + invocationID, Role: apitypes.ThreadRoleTool, Content: result}
	f.writes = append(f.writes, fakeWrite{AgentID: agentID, ItemID: itemID, Kind: apitypes.LifecycleToolFinished,
		Rows: []apitypes.ThreadMessage{row}})
	return f.appendLocked(agentID, itemID, apitypes.LifecycleEvent{
		Kind: apitypes.LifecycleToolFinished, ItemID: itemID, InvocationID: invocationID,
		ToolName: invocationID, ToolResult: result, ToolError: errText,
		Rows: []apitypes.ThreadMessage{row},
	}), nil
}

func (f *fakeJournal) AppendItemMessages(_ context.Context, agentID, itemID string, msgs []backend.ThreadMessageDraft) (*backend.JournalBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAppend != nil {
		return nil, f.failAppend
	}
	rows := make([]apitypes.ThreadMessage, 0, len(msgs))
	for _, m := range msgs {
		rows = append(rows, apitypes.ThreadMessage{ID: m.ID, Role: m.Role, Content: m.Content})
	}
	f.writes = append(f.writes, fakeWrite{AgentID: agentID, ItemID: itemID,
		Kind: apitypes.LifecycleMessageCommitted, Drafts: msgs, Rows: rows})
	return f.appendLocked(agentID, itemID, apitypes.LifecycleEvent{
		Kind: apitypes.LifecycleMessageCommitted, ItemID: itemID, Rows: rows,
	}), nil
}

func (f *fakeJournal) FinishItem(_ context.Context, agentID, itemID, outcome, reason string, msgs []backend.ThreadMessageDraft) (*backend.JournalBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failFinish != nil {
		return nil, f.failFinish
	}
	item := f.items[fakeKey(agentID, itemID)]
	if item == nil || item.state != "started" {
		return nil, errors.New("item is not started")
	}
	item.state = outcome
	rows := make([]apitypes.ThreadMessage, 0, len(msgs))
	for _, m := range msgs {
		rows = append(rows, apitypes.ThreadMessage{ID: m.ID, Role: m.Role, Content: m.Content})
	}
	f.writes = append(f.writes, fakeWrite{AgentID: agentID, ItemID: itemID,
		Kind: apitypes.LifecycleItemFinished, Drafts: msgs, Rows: rows})
	return f.appendLocked(agentID, itemID, apitypes.LifecycleEvent{
		Kind: apitypes.LifecycleItemFinished, ItemID: itemID, ItemKind: item.kind,
		ThreadID: item.threadID, Rows: rows, Outcome: outcome, Reason: reason,
	}), nil
}

func (f *fakeJournal) ExecuteThreadSwitch(_ context.Context, agentID, itemID string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSwitch != nil {
		return nil, nil, f.failSwitch
	}
	item := f.items[fakeKey(agentID, itemID)]
	if item == nil || item.state != "started" {
		return nil, nil, errors.New("item is not started")
	}
	item.state = apitypes.ItemCompleted
	previous := "thread-" + agentID
	f.heads[agentID]++
	changed := apitypes.LifecycleEvent{
		Kind: apitypes.LifecycleThreadChanged, ItemID: itemID, ItemKind: item.kind,
		ThreadID: "thread-new-" + itemID, PreviousThreadID: previous,
		Divider: &apitypes.ThreadMessage{ID: "thread-new-" + itemID, Role: apitypes.ThreadRoleDivider, Content: "New session"},
		Pos:     f.heads[agentID],
	}
	f.heads[agentID]++
	done := apitypes.LifecycleEvent{
		Kind: apitypes.LifecycleItemFinished, ItemID: itemID, ItemKind: item.kind,
		ThreadID: changed.ThreadID, Outcome: apitypes.ItemCompleted,
		Pos: f.heads[agentID], BatchEnd: true,
	}
	f.events[agentID] = append(f.events[agentID], changed, done)
	return &backend.ActivatedThread{ID: changed.ThreadID, PreviousID: previous},
		&backend.JournalBatch{Cursor: backend.FormatCursor(f.history, done.Pos),
			Events: []apitypes.LifecycleEvent{changed, done}}, nil
}

func (f *fakeJournal) ShakeThreadContext(_ context.Context, agentID, itemID string) (int, *backend.JournalBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failShake != nil {
		return 0, nil, f.failShake
	}
	item := f.items[fakeKey(agentID, itemID)]
	if item == nil || item.state != "started" {
		return 0, nil, errors.New("item is not started")
	}
	item.state = apitypes.ItemCompleted
	f.writes = append(f.writes, fakeWrite{AgentID: agentID, ItemID: itemID, Kind: apitypes.LifecycleItemFinished})
	return 0, f.appendLocked(agentID, itemID, apitypes.LifecycleEvent{
		Kind: apitypes.LifecycleItemFinished, ItemID: itemID, ItemKind: item.kind,
		ThreadID: item.threadID, Outcome: apitypes.ItemCompleted,
	}), nil
}

func (f *fakeJournal) ShakeAgentContext(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (f *fakeJournal) RotateThreadCompacted(_ context.Context, agentID, tagItemID, summary string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item := f.items[fakeKey(agentID, tagItemID)]
	if item == nil || item.state != "accepted" {
		return nil, nil, errors.New("item is not accepted")
	}
	if summary == "" {
		return nil, nil, errors.New("empty summary")
	}
	return &backend.ActivatedThread{ID: tagItemID + "-successor", PreviousID: "thread-" + agentID},
		f.appendLocked(agentID, tagItemID, apitypes.LifecycleEvent{
			Kind: apitypes.LifecycleThreadChanged, ItemID: tagItemID, ItemKind: item.kind,
			ThreadID: tagItemID + "-successor", PreviousThreadID: "thread-" + agentID,
		}), nil
}

func (f *fakeJournal) ExecuteCompaction(_ context.Context, agentID, itemID, summary string) (*backend.ActivatedThread, *backend.JournalBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item := f.items[fakeKey(agentID, itemID)]
	if item == nil || item.state != "started" {
		return nil, nil, errors.New("item is not started")
	}
	if summary == "" {
		return nil, nil, errors.New("empty summary")
	}
	item.state = apitypes.ItemCompleted
	f.writes = append(f.writes, fakeWrite{AgentID: agentID, ItemID: itemID, Kind: apitypes.LifecycleItemFinished})
	return &backend.ActivatedThread{ID: itemID + "-successor"}, f.appendLocked(agentID, itemID, apitypes.LifecycleEvent{
		Kind: apitypes.LifecycleItemFinished, ItemID: itemID, ItemKind: item.kind,
		ThreadID: item.threadID, Outcome: apitypes.ItemCompleted,
	}), nil
}

func (f *fakeJournal) ReplayAfter(_ context.Context, agentID, historyID string, afterPos int64, limit int) (*backend.JournalPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if historyID != f.history {
		return nil, fmt.Errorf("replay journal: %w", backend.ErrReplayForeign)
	}
	if head := f.heads[agentID]; afterPos > head {
		return nil, fmt.Errorf("replay journal: %w", backend.ErrReplayAhead)
	}
	page := &backend.JournalPage{}
	for _, ev := range f.events[agentID] {
		if ev.Pos > afterPos && len(page.Events) < limit {
			raw, err := json.Marshal(ev)
			if err != nil {
				return nil, err
			}
			page.Events = append(page.Events, raw)
			page.LastPos = ev.Pos
		}
	}
	return page, nil
}

func (f *fakeJournal) JournalStatus(_ context.Context, agentID string) (string, int64, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.history, f.heads[agentID], 0, nil
}

func (f *fakeJournal) SyncSnapshot(_ context.Context, agentID string, _ int64) (*backend.SyncSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSnapshot != nil {
		return nil, f.failSnapshot
	}
	return &backend.SyncSnapshot{
		Cursor: backend.FormatCursor(f.history, f.heads[agentID]),
		Page:   &apitypes.ThreadPage{Items: []apitypes.ThreadMessage{}},
	}, nil
}

// writesFor returns the recorded row batches for one item.
func (f *fakeJournal) writesFor(itemID string) []fakeWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeWrite
	for _, w := range f.writes {
		if w.ItemID == itemID {
			out = append(out, w)
		}
	}
	return out
}
