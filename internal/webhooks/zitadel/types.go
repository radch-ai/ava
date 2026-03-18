package zitadel

// WebhookRequest is the payload for Zitadel webhooks.
type WebhookRequest struct {
	EventType string `json:"event_type"`
	UserID    string `json:"user_id"`
}

// EventType constants for Zitadel webhook events.
const (
	EventTypeUserCreated = "user.created"
	EventTypeUserDeleted = "user.deleted"
)
