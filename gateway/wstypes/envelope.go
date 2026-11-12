// Package wstypes is the WebSocket wire contract between the gateway
// and the web client. The UI generates TypeScript types from it via
// `just typegen` (tygo.yaml → ui/web/src/lib/types/gen/wstypes.gen.ts).
//
// Frames share one envelope shape per direction; payloads ride in Data.
package wstypes

// EventType tags server-to-client events.
type EventType string

const (
	EventTypeConnected EventType = "connected"
)

// EnvelopeType tags client frames.
type EnvelopeType string

const (
	EnvelopeTypePrompt EnvelopeType = "prompt"
)

// Event is the envelope for all server-to-client events.
type Event struct {
	Type    EventType `json:"type"`
	AgentID string    `json:"agent_id,omitempty"`
	Data    any       `json:"data,omitempty"`
}

// ConnectedEvent is the payload of a connected event.
type ConnectedEvent struct {
	ConnID string `json:"conn_id"`
}
