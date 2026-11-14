package wstypes

// ModelRef is a prompt's model selection: the model id plus the
// provider row that owns it.
type ModelRef struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
}

// PromptMessage is the payload of a prompt frame; message IDs are
// assigned server-side. Model nil = default routing.
type PromptMessage struct {
	Content string    `json:"content"`
	Model   *ModelRef `json:"model"`
}

// LaneStatus is the payload of a lane_status event. Model is the
// active lane's model id — display only. Working reports a turn in
// flight; the composer's stop control tracks it.
type LaneStatus struct {
	Active    bool   `json:"active"`
	Working   bool   `json:"working"`
	Model     string `json:"model"`
	QueueLen  int    `json:"queue_len"`
	ConnCount int    `json:"conn_count"`
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
// tool_result carries the same one.
type ToolCallEvent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// ToolResultEvent is the payload of a tool_result event: one
// invocation completed. Error is set when the tool failed; result
// then carries whatever the tool managed to say.
type ToolResultEvent struct {
	ID     string  `json:"id"`
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
type ErrorEvent struct {
	Message string `json:"message"`
}
