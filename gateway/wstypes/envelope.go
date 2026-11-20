// Package wstypes is the WebSocket wire contract between the gateway
// and the web client. The UI generates TypeScript types from it via
// `just typegen` (tygo.yaml → ui/web/src/lib/types/gen/wstypes.gen.ts).
//
// Frames share one envelope shape per direction; payloads ride in Data.
package wstypes

import "encoding/json"

// EventType tags server-to-client events.
type EventType string

const (
	EventTypeConnected    EventType = "connected"
	EventTypeLaneStatus   EventType = "lane_status"
	EventTypeMessage      EventType = "message"
	EventTypeMessageChunk EventType = "message_chunk"
	EventTypeToolCall     EventType = "tool_call"
	EventTypeToolResult   EventType = "tool_result"
	EventTypeError        EventType = "error"
	EventTypeQueueResult  EventType = "queue_result"
	EventTypeQueueState   EventType = "queue_state"

	// EventTypeCommandCompleted reports a durable thread switch: the
	// /new (or /reset) command committed.
	EventTypeCommandCompleted EventType = "command_completed"
)

// EnvelopeType tags client frames.
type EnvelopeType string

const (
	EnvelopeTypePrompt    EnvelopeType = "prompt"
	EnvelopeTypeSendNow   EnvelopeType = "send_now"
	EnvelopeTypeStatus    EnvelopeType = "status"
	EnvelopeTypeInterrupt EnvelopeType = "interrupt"
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
