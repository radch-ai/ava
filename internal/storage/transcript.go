package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// Turn represents a complete turn in the conversation.
type Turn struct {
	SessionID  string
	UserID     string
	AgentID    string
	TurnNumber int
	Messages   []agent.AgentMessage
	Tools      []ToolCall
	Timestamp  int64
}

// ToolCall represents a tool call within a turn.
type ToolCall struct {
	ID      string
	Name    string
	Args    map[string]any
	Result  any
	IsError bool
}

// TranscriptWriter writes conversation transcripts to S3.
type TranscriptWriter struct {
	client Client
}

// NewTranscriptWriter creates a new TranscriptWriter.
func NewTranscriptWriter(client Client) *TranscriptWriter {
	return &TranscriptWriter{
		client: client,
	}
}

// WriteTurn writes a single turn to the transcript as a JSONL line.
func (w *TranscriptWriter) WriteTurn(ctx context.Context, turn Turn) error {
	line := TurnToJSONL(turn)
	path := TranscriptPath(turn.UserID, turn.AgentID, turn.SessionID)
	return w.client.Append(ctx, path, []byte(line))
}

// TurnToJSONL converts a turn to a JSONL line.
func TurnToJSONL(turn Turn) string {
	// Convert messages to simplified format
	messages := make([]map[string]any, len(turn.Messages))
	for i, msg := range turn.Messages {
		messages[i] = map[string]any{
			"role":      string(msg.Role),
			"content":   extractTextContent(msg),
			"timestamp": msg.Timestamp,
		}
		if msg.ToolCallID != "" {
			messages[i]["tool_call_id"] = msg.ToolCallID
		}
		if msg.ToolName != "" {
			messages[i]["tool_name"] = msg.ToolName
		}
		if msg.IsError {
			messages[i]["is_error"] = true
		}
	}

	// Convert tools to simplified format
	tools := make([]map[string]any, len(turn.Tools))
	for i, tool := range turn.Tools {
		tools[i] = map[string]any{
			"id":      tool.ID,
			"name":    tool.Name,
			"args":    tool.Args,
			"result":  tool.Result,
			"isError": tool.IsError,
		}
	}

	data := map[string]any{
		"turn":       turn.TurnNumber,
		"session_id": turn.SessionID,
		"user_id":    turn.UserID,
		"agent_id":   turn.AgentID,
		"messages":   messages,
		"tools":      tools,
		"timestamp":  turn.Timestamp,
	}

	line, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprintf("{\"error\":\"marshal failed: %v\"}\n", err)
	}

	return string(line) + "\n"
}

// extractTextContent extracts text content from a message.
func extractTextContent(msg agent.AgentMessage) string {
	for _, block := range msg.Content {
		if tc, ok := block.(agent.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
