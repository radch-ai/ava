package apitypes

// HeartbeatDuration is the heartbeat cadence selector.
type HeartbeatDuration string

const (
	HeartbeatDuration30m HeartbeatDuration = "30m"
	HeartbeatDuration1hr HeartbeatDuration = "1hr"
	HeartbeatDuration2hr HeartbeatDuration = "2hr"
)

// HeartbeatConfig is the heartbeat configuration (stored in a
// separate heartbeat table).
type HeartbeatConfig struct {
	Model    string            `json:"model"`
	Enabled  bool              `json:"enabled"`
	Duration HeartbeatDuration `json:"duration"`
	Content  string            `json:"content"`
}
