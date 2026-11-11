package apitypes

// ActivityTrigger is what scheduled an activity row.
type ActivityTrigger string

const (
	ActivityTriggerHeartbeat ActivityTrigger = "heartbeat"
	ActivityTriggerCron      ActivityTrigger = "cron"
)

// ActivityStatus is an execution's outcome.
type ActivityStatus string

const (
	ActivityStatusRunning   ActivityStatus = "running"
	ActivityStatusCompleted ActivityStatus = "completed"
	ActivityStatusFailed    ActivityStatus = "failed"
)

// AgentActivityItem is one scheduler execution (heartbeat run, cron
// run); Message carries the response summary when there is one.
type AgentActivityItem struct {
	ID          string          `json:"id"`
	CreatedAt   string          `json:"createdAt"`
	TriggeredBy ActivityTrigger `json:"triggeredBy"`
	Action      string          `json:"action"`
	Status      ActivityStatus  `json:"status"`
	Message     *string         `json:"message,omitempty"`
}

// ActivityPage is one newest-first page of the feed. NextCursor names
// the page's oldest item and is null on the last page.
type ActivityPage struct {
	Items      []AgentActivityItem `json:"items"`
	NextCursor *string             `json:"nextCursor"`
}
