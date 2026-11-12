package wstypes

// LaneStatus is the payload of a lane_status event.
type LaneStatus struct {
	Active    bool   `json:"active"`
	Model     string `json:"model"`
	QueueLen  int    `json:"queue_len"`
	ConnCount int    `json:"conn_count"`
}
