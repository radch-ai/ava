package wstypes

import "encoding/json"

// Queue item kinds. QueueItemKindPrompt is an ordinary Eino turn;
// QueueItemKindNewThread is a /new or /reset command. Every item
// carries one of these — there is no zero-value spelling.
const (
	QueueItemKindPrompt    = "prompt"
	QueueItemKindNewThread = "new_thread"
)

// ModelRef is a prompt's model selection: the model id plus the
// provider row that owns it.
type ModelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
}

// PromptMessage is the payload of a prompt frame; the item ID is
// assigned server-side and returns via queue_result, which echoes
// RequestID. Model nil = default routing.
type PromptMessage struct {
	RequestID string    `json:"request_id,omitempty"`
	Content   string    `json:"content"`
	Model     *ModelRef `json:"model"`
}

// SendNowMessage is the payload of a send_now frame: promote one
// pending item to run next, interrupting the active turn. The item
// is addressed by its queue ID, never resubmitted as text. Model is
// the caller's current selection; a paused queue rebinds its failed-
// model entries to it, a healthy queue ignores it. Model nil =
// default routing.
type SendNowMessage struct {
	RequestID string    `json:"request_id,omitempty"`
	ItemID    string    `json:"item_id"`
	Model     *ModelRef `json:"model"`
}

// LaneStatus is the payload of a lane_status event. Model is the
// active lane's model id — display only. Working reports a turn in
// flight; the composer's stop control tracks it. Reasoning reports
// the model streaming reasoning tokens right now.
type LaneStatus struct {
	Active    bool   `json:"active"`
	Working   bool   `json:"working"`
	Reasoning bool   `json:"reasoning"`
	Model     string `json:"model"`
}

// QueueItem is one queued prompt. Kind is always set — a command or an
// ordinary prompt. The item's model selection is not projected: routing
// is the queue's business, and the transcript shows what actually ran.
type QueueItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Kind    string `json:"kind"`
}

// QueueActiveItem is the executing item. UserMessageID names its
// persisted user row — server-assigned and distinct from the stream ID,
// which stays the item ID. Commands persist no user row, so they omit
// it. Kind is always set, like the queued item's.
type QueueActiveItem struct {
	ID            string `json:"id"`
	Content       string `json:"content"`
	Kind          string `json:"kind"`
	UserMessageID string `json:"user_message_id,omitempty"`
}

// QueueResultEvent is the payload of a queue_result event: the reply
// to one prompt or send_now frame, correlated by RequestID. A
// rejection carries a user-facing reason and changes no queue state.
type QueueResultEvent struct {
	RequestID string `json:"request_id"`
	Accepted  bool   `json:"accepted"`
	ItemID    string `json:"item_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// QueueStateEvent is the payload of a queue_state event: the
// authoritative queue snapshot, always a full replacement. Items are
// pending work oldest-first, excluding the active item and the
// reserved-next one; Next is claimed-but-not-started work so a
// reconnect cannot mistake it for lost.
type QueueStateEvent struct {
	Items  []QueueItem      `json:"items"`
	Active *QueueActiveItem `json:"active,omitempty"`
	Next   *QueueItem       `json:"next,omitempty"`
	Paused bool             `json:"paused"`
}

// MessageChunkEvent is the payload of a message_chunk event: one
// streamed piece of the answer in flight. Seq is the stream's own
// increasing sequence — a client ignores a duplicate and reconnects on a
// gap rather than display incomplete text. Streaming is transient: the
// authoritative record is the committed row in the lifecycle feed.
type MessageChunkEvent struct {
	ID    string `json:"id"`
	Delta string `json:"delta"`
	Seq   int64  `json:"seq"`
}

// ErrorEvent is the payload of a transient error event: why a turn
// failed when nothing durable could carry the failure. Tool is set when a
// tool invocation failed the turn — its own row already shows the
// failure, so clients need not repeat it. Durable failures arrive as
// lifecycle events carrying their stored rows; this frame never does.
type ErrorEvent struct {
	TurnID  string `json:"turn_id,omitempty"`
	Message string `json:"message"`
	Tool    string `json:"tool,omitempty"`
}

// SyncMessage is the payload of a sync frame: continue from the cursor the
// client last applied, or omit it for a fresh baseline. A client that
// reconnects keeps its cursor and replays what it missed.
type SyncMessage struct {
	Cursor string `json:"cursor,omitempty"`
}

// SyncSnapshotEvent answers a sync request with the authoritative
// baseline — the active thread's newest display page — and the cursor a
// client checkpoints after applying it. Page is an apitypes.ThreadPage.
type SyncSnapshotEvent struct {
	Cursor string          `json:"cursor"`
	Page   json.RawMessage `json:"page"`
}

// SyncReadyEvent ends synchronization: the committed head and the runtime
// state a live client needs. Queue and Lane replace stale runtime state;
// Prefix is the in-flight turn's answer so far while its worker lives.
type SyncReadyEvent struct {
	Cursor string          `json:"cursor"`
	Queue  QueueStateEvent `json:"queue"`
	Lane   LaneStatus      `json:"lane"`
	Prefix *TurnPrefix     `json:"prefix,omitempty"`
}

// TurnPrefix is one in-flight turn's replayable state: the answer text
// streamed so far, the last chunk sequence it includes, and every
// invocation of the turn — pending ones included.
type TurnPrefix struct {
	ItemID      string                `json:"item_id"`
	Text        string                `json:"text"`
	LastSeq     int64                 `json:"last_seq"`
	Invocations []ToolInvocationState `json:"invocations,omitempty"`
}

// ToolInvocationState is one tool invocation of the in-flight turn, keyed
// by the server-assigned invocation id.
type ToolInvocationState struct {
	InvocationID string `json:"invocation_id"`
	Name         string `json:"name"`
	Args         string `json:"args"`
	Result       string `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	Pending      bool   `json:"pending"`
}

// LifecycleBatchEvent is one committed transaction's lifecycle events
// (backend/apitypes.LifecycleEvent) plus the cursor a client checkpoints
// after applying them — the payload of both the sync_batch replay answer
// and the live lifecycle feed.
type LifecycleBatchEvent struct {
	Cursor string            `json:"cursor"`
	Events []json.RawMessage `json:"events"`
}
