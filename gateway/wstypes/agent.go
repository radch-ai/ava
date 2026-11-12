package wstypes

// PromptMessage is the payload of a prompt frame; message IDs are
// assigned server-side.
type PromptMessage struct {
	Content string `json:"content"`
	Model   string `json:"model"`
}

// LaneStatus is the payload of a lane_status event.
type LaneStatus struct {
	Active    bool   `json:"active"`
	Model     string `json:"model"`
	QueueLen  int    `json:"queue_len"`
	ConnCount int    `json:"conn_count"`
}
