// Package wstypes is the WebSocket wire contract between the gateway
// and the web client. The UI generates TypeScript types from it via
// `just typegen` (tygo.yaml → ui/web/src/lib/types/gen/wstypes.gen.ts).
//
// Frames share one envelope shape per direction; payloads ride in Data.
// Conversation facts travel as lifecycle events (backend/apitypes), so
// this package names the envelopes and the sync handshake only — durable
// payloads stay json.RawMessage and are validated against the generated
// apitypes by the client.
package wstypes

import "encoding/json"

// EventType tags server-to-client events.
type EventType string

const (
	EventTypeConnected EventType = "connected"
	// EventTypeLaneStatus reports the lane's live state; it is runtime
	// truth, never evidence a conversation outcome was recorded.
	EventTypeLaneStatus EventType = "lane_status"
	// EventTypeSyncSnapshot answers a sync request with the authoritative
	// baseline and its cursor.
	EventTypeSyncSnapshot EventType = "sync_snapshot"
	// EventTypeSyncBatch answers a sync request with retained lifecycle
	// events after the client's cursor.
	EventTypeSyncBatch EventType = "sync_batch"
	// EventTypeSyncReady ends synchronization: the committed head, the
	// runtime snapshot, and the live subscription are established.
	EventTypeSyncReady    EventType = "sync_ready"
	EventTypeLifecycle    EventType = "lifecycle"
	EventTypeMessageChunk EventType = "message_chunk"
	EventTypeError        EventType = "error"
	EventTypeQueueResult  EventType = "queue_result"
	EventTypeQueueState   EventType = "queue_state"
)

// EnvelopeType tags client frames.
type EnvelopeType string

const (
	EnvelopeTypePrompt    EnvelopeType = "prompt"
	EnvelopeTypeSendNow   EnvelopeType = "send_now"
	EnvelopeTypeInterrupt EnvelopeType = "interrupt"
	// EnvelopeTypeSync starts (or continues) conversation
	// synchronization for the frame's agent.
	EnvelopeTypeSync EnvelopeType = "sync"
)

// Envelope is the head of every client frame. Data stays raw JSON:
// each frame decodes its own payload once, straight from the bytes.
type Envelope struct {
	Type    EnvelopeType    `json:"type"`
	AgentID string          `json:"agent_id"`
	Data    json.RawMessage `json:"data,omitempty"`
}

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
