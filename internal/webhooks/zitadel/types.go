package zitadel

// WebhookRequest is the payload for Zitadel Actions v2 webhooks.
// See: https://zitadel.com/docs/concepts/features/actions_v2
type WebhookRequest struct {
	// AggregateID is the ID of the aggregate (for user events, this is the user ID).
	AggregateID string `json:"aggregateID"`
	// EventType is the type of the event (e.g., "user.human.added").
	EventType string `json:"event_type"`
}

// EventType constants for Zitadel Actions v2 webhook events.
const (
	// EventTypeUserHumanAdded is fired when a human user is created.
	EventTypeUserHumanAdded = "user.human.added"
	// EventTypeUserRemoved is fired when a user is deleted.
	EventTypeUserRemoved = "user.removed"
)
