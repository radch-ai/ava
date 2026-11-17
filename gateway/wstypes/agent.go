package wstypes

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
	QueueLen  int    `json:"queue_len"`
	ConnCount int    `json:"conn_count"`
}

// QueueItem is one queued prompt: the model selection captured at
// submission (nil = default routing) rides the item.
type QueueItem struct {
	ID      string    `json:"id"`
	Content string    `json:"content"`
	Model   *ModelRef `json:"model,omitempty"`
}

// QueueActiveItem is the executing item. UserMessageID names the
// started turn's persisted user row — distinct from the stream ID,
// which stays the item ID.
type QueueActiveItem struct {
	ID            string    `json:"id"`
	Content       string    `json:"content"`
	Model         *ModelRef `json:"model,omitempty"`
	UserMessageID string    `json:"user_message_id"`
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
// streamed piece of the answer in flight. The final message event
// with the same ID is the authoritative record.
type MessageChunkEvent struct {
	ID    string `json:"id"`
	Delta string `json:"delta"`
}

// ToolCallEvent is the payload of a tool_call event: the model
// invoked a tool. ID is the model's tool-call id; the matching
// tool_result carries the same one. TurnID names the queue item that
// issued the call — a late result keeps its turn, not whichever turn
// is active when it lands.
type ToolCallEvent struct {
	ID     string `json:"id"`
	TurnID string `json:"turn_id,omitempty"`
	Name   string `json:"name"`
	Args   string `json:"args"`
}

// ToolResultEvent is the payload of a tool_result event: one
// invocation completed. Error is set when the tool failed; result
// then carries whatever the tool managed to say. TurnID matches the
// originating tool_call's.
type ToolResultEvent struct {
	ID     string  `json:"id"`
	TurnID string  `json:"turn_id,omitempty"`
	Result string  `json:"result"`
	Error  *string `json:"error,omitempty"`
}

// MessageEvent is the payload of a message event: the assistant's
// completed answer to a turn.
type MessageEvent struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// ErrorEvent is the payload of an error event: why a turn failed.
// Tool is set when a tool invocation failed the turn — its row
// already shows the failure, so clients need not repeat it. ID names
// the stored error row when the failure persisted; without one the
// error is transient (stopped turns, tool failures, a failed write).
// TurnID names the failed turn's stream, like the message events'.
type ErrorEvent struct {
	ID      string `json:"id,omitempty"`
	TurnID  string `json:"turn_id,omitempty"`
	Message string `json:"message"`
	Tool    string `json:"tool,omitempty"`
}
