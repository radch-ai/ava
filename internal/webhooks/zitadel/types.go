package zitadel

// WebhookRequest is the payload for Zitadel Actions v2 webhooks.
// See: https://zitadel.com/docs/concepts/features/actions_v2
type WebhookRequest struct {
	// AggregateID is the ID of the aggregate (for user events, this is the user ID).
	AggregateID string `json:"aggregateID"`
	// AggregateType is the type of the aggregate (e.g., "user").
	AggregateType string `json:"aggregateType"`
	// ResourceOwner is the resource owner (organization) the aggregate belongs to.
	ResourceOwner string `json:"resourceOwner"`
	// InstanceID is the ID of the Zitadel instance.
	InstanceID string `json:"instanceID"`
	// Version is the version of the aggregate.
	Version string `json:"version"`
	// Sequence is the sequence number of the event.
	Sequence uint64 `json:"sequence"`
	// EventType is the type of the event (e.g., "user.human.added").
	EventType string `json:"eventType"`
	// CreatedAt is the timestamp when the event was created.
	CreatedAt string `json:"createdAt"`
	// UserID is the ID of the user who triggered the event (NOT the user being created/deleted).
	UserID string `json:"userID"`
	// Payload contains the event-specific data.
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// EventType constants for Zitadel Actions v2 webhook events.
const (
	// EventTypeUserHumanAdded is fired when a human user is created.
	EventTypeUserHumanAdded = "user.human.added"
	// EventTypeUserRemoved is fired when a user is deleted.
	EventTypeUserRemoved = "user.removed"
)
