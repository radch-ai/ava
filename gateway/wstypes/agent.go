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
// active lane's model id — display only.
type LaneStatus struct {
	Active    bool   `json:"active"`
	Model     string `json:"model"`
	QueueLen  int    `json:"queue_len"`
	ConnCount int    `json:"conn_count"`
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
