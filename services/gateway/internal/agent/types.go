// Package agent provides core types for the agent runtime framework.
//
// # Core Types
//
//   - AgentMessage: A message with role, content blocks, and optional tool metadata
//   - ContentBlock: Interface for text, image, and tool call content blocks
//   - Tool: A callable function with name, description, and execute function
//   - ToolResult: Result of tool execution with content and optional details
//
// # Content Blocks
//
// Messages contain content blocks that can be:
//   - TextContent: Plain text
//   - ImageContent: Base64-encoded image data with MIME type
//   - ToolCallContent: Tool invocation request with ID, name, and arguments
//
// # Roles
//
// Messages have roles indicating the sender:
//   - RoleUser: From the human user
//   - RoleAssistant: From the AI agent
//   - RoleToolResult: Result from tool execution
//
// # Creating Messages
//
// Use helper functions to create messages:
//
//	userMsg := NewTextMessage(RoleUser, "Hello")
//	toolCallMsg := NewToolCallMessage("call-123", "calculator", map[string]any{"x": 1, "y": 2})
//	resultMsg := NewToolResultMessage("call-123", "calculator", "3", false)
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Role represents the role of a message sender.
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "toolResult"
)

// String returns the string representation of the role.
func (r Role) String() string {
	return string(r)
}

// ContentBlock is a sealed interface for content blocks in messages.
type ContentBlock interface {
	isContentBlock()
}

// TextContent represents a text block in a message.
type TextContent struct {
	Text string `json:"text"`
}

func (TextContent) isContentBlock() {}

// ImageContent represents an image block in a message with base64-encoded data.
type ImageContent struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

func (ImageContent) isContentBlock() {}

// ToolCallContent represents a tool invocation request from an assistant.
type ToolCallContent struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (ToolCallContent) isContentBlock() {}

// contentBlockWrapper is used for JSON marshaling of ContentBlock.
type contentBlockWrapper struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	Data      string         `json:"data,omitempty"`
	MimeType  string         `json:"mime_type,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// MarshalJSON implements json.Marshaler for TextContent.
func (c TextContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(contentBlockWrapper{
		Type: "text",
		Text: c.Text,
	})
}

// UnmarshalJSON implements json.Unmarshaler for TextContent.
func (c *TextContent) UnmarshalJSON(data []byte) error {
	var wrapper contentBlockWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	c.Text = wrapper.Text
	return nil
}

// MarshalJSON implements json.Marshaler for ImageContent.
func (c ImageContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(contentBlockWrapper{
		Type:     "image",
		Data:     c.Data,
		MimeType: c.MimeType,
	})
}

// UnmarshalJSON implements json.Unmarshaler for ImageContent.
func (c *ImageContent) UnmarshalJSON(data []byte) error {
	var wrapper contentBlockWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	c.Data = wrapper.Data
	c.MimeType = wrapper.MimeType
	return nil
}

// MarshalJSON implements json.Marshaler for ToolCallContent.
func (c ToolCallContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(contentBlockWrapper{
		Type:      "tool_call",
		ID:        c.ID,
		Name:      c.Name,
		Arguments: c.Arguments,
	})
}

// UnmarshalJSON implements json.Unmarshaler for ToolCallContent.
func (c *ToolCallContent) UnmarshalJSON(data []byte) error {
	var wrapper contentBlockWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	c.ID = wrapper.ID
	c.Name = wrapper.Name
	c.Arguments = wrapper.Arguments
	return nil
}

// AgentMessage represents a message in the agent conversation.
type AgentMessage struct {
	Role      Role           `json:"role"`
	Content   []ContentBlock `json:"content"`
	Timestamp int64          `json:"timestamp"`

	// ToolResult-specific fields
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
}

// MarshalJSON implements json.Marshaler for AgentMessage.
func (m AgentMessage) MarshalJSON() ([]byte, error) {
	type Alias AgentMessage
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(&m),
	})
}

// UnmarshalJSON implements json.Unmarshaler for AgentMessage.
func (m *AgentMessage) UnmarshalJSON(data []byte) error {
	type Alias AgentMessage
	aux := &struct {
		Content []json.RawMessage `json:"content"`
		*Alias
	}{
		Alias: (*Alias)(m),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	m.Content = make([]ContentBlock, len(aux.Content))
	for i, raw := range aux.Content {
		var wrapper contentBlockWrapper
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return err
		}

		switch wrapper.Type {
		case "text":
			var tc TextContent
			if err := json.Unmarshal(raw, &tc); err != nil {
				return err
			}
			m.Content[i] = tc
		case "image":
			var ic ImageContent
			if err := json.Unmarshal(raw, &ic); err != nil {
				return err
			}
			m.Content[i] = ic
		case "tool_call":
			var tcc ToolCallContent
			if err := json.Unmarshal(raw, &tcc); err != nil {
				return err
			}
			m.Content[i] = tcc
		default:
			return fmt.Errorf("unknown content block type: %s", wrapper.Type)
		}
	}

	return nil
}

// NewTextMessage creates a new text-only message.
func NewTextMessage(role Role, text string) AgentMessage {
	return AgentMessage{
		Role:      role,
		Content:   []ContentBlock{TextContent{Text: text}},
		Timestamp: time.Now().Unix(),
	}
}

// NewToolCallMessage creates a new message with a tool call from an assistant.
func NewToolCallMessage(id, name string, args map[string]any) AgentMessage {
	return AgentMessage{
		Role:      RoleAssistant,
		Content:   []ContentBlock{ToolCallContent{ID: id, Name: name, Arguments: args}},
		Timestamp: time.Now().Unix(),
	}
}

// NewToolResultMessage creates a new tool result message.
func NewToolResultMessage(toolCallID, toolName, result string, isError bool) AgentMessage {
	return AgentMessage{
		Role:       RoleToolResult,
		Content:    []ContentBlock{TextContent{Text: result}},
		Timestamp:  time.Now().Unix(),
		ToolCallID: toolCallID,
		ToolName:   toolName,
		IsError:    isError,
	}
}

// ThinkingLevel represents the thinking/reasoning intensity level.
type ThinkingLevel string

const (
	ThinkingLevelOff     ThinkingLevel = "off"
	ThinkingLevelMinimal ThinkingLevel = "minimal"
	ThinkingLevelLow     ThinkingLevel = "low"
	ThinkingLevelMedium  ThinkingLevel = "medium"
	ThinkingLevelHigh    ThinkingLevel = "high"
)

// String returns the string representation of the thinking level.
func (tl ThinkingLevel) String() string {
	return string(tl)
}

// IsOff returns true if thinking is disabled.
func (tl ThinkingLevel) IsOff() bool {
	return tl == ThinkingLevelOff
}

// IsEnabled returns true if thinking is enabled (any non-off level).
func (tl ThinkingLevel) IsEnabled() bool {
	return tl != ThinkingLevelOff
}

// Model represents an LLM model configuration.
type Model struct {
	ID       string
	API      string
	Provider string
	BaseURL  string
}

// Tool represents a callable tool function.
type Tool struct {
	Name        string
	Description string
	Execute     func(ctx context.Context, args map[string]any) (*ToolResult, error)
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	Content []ContentBlock
	Details any
}

// ToolExecutionError represents an error that occurred during tool execution.
type ToolExecutionError struct {
	ToolName string
	Message  string
}

// Error returns the error message.
func (e *ToolExecutionError) Error() string {
	if e.ToolName != "" {
		return "tool " + e.ToolName + ": " + e.Message
	}
	return e.Message
}

// AgentStateOptions configures a new AgentState.
type AgentStateOptions struct {
	SystemPrompt  string
	Model         Model
	ThinkingLevel ThinkingLevel
	Tools         []Tool
	SessionID     string
	UserID        string
	AgentID       string
}

// AgentState tracks the current state of an agent session.
type AgentState struct {
	SystemPrompt     string
	Model            Model
	ThinkingLevel    ThinkingLevel
	Tools            []Tool
	Messages         []AgentMessage
	IsStreaming      bool
	StreamMessage    *AgentMessage
	PendingToolCalls map[string]bool
	Error            string
	SessionID        string
	UserID           string
	AgentID          string
}

// NewAgentState creates a new AgentState with the given options.
func NewAgentState(opts AgentStateOptions) AgentState {
	return AgentState{
		SystemPrompt:     opts.SystemPrompt,
		Model:            opts.Model,
		ThinkingLevel:    opts.ThinkingLevel,
		Tools:            opts.Tools,
		SessionID:        opts.SessionID,
		UserID:           opts.UserID,
		AgentID:          opts.AgentID,
		Messages:         []AgentMessage{},
		PendingToolCalls: make(map[string]bool),
	}
}

// AddMessage appends a message to the conversation history.
func (s *AgentState) AddMessage(msg AgentMessage) {
	s.Messages = append(s.Messages, msg)
}

// SetStreaming updates the streaming state and optionally the current stream message.
func (s *AgentState) SetStreaming(streaming bool, msg *AgentMessage) {
	s.IsStreaming = streaming
	s.StreamMessage = msg
}

// AddPendingToolCall marks a tool call as pending.
func (s *AgentState) AddPendingToolCall(id string) {
	s.PendingToolCalls[id] = true
}

// RemovePendingToolCall removes a tool call from the pending set.
func (s *AgentState) RemovePendingToolCall(id string) {
	delete(s.PendingToolCalls, id)
}

// HasPendingToolCall returns true if the tool call is still pending.
func (s *AgentState) HasPendingToolCall(id string) bool {
	return s.PendingToolCalls[id]
}

// Agent is a legacy type kept for node service compatibility.
// Node tool migration is pending - this stub allows compilation.
type Agent struct {
	ID      string
	userID  string
	agentID string
	tools   []Tool
}

func (a *Agent) AddTools(tools ...Tool) error {
	a.tools = append(a.tools, tools...)
	return nil
}

func (a *Agent) RemoveTools(prefix string) error {
	filtered := make([]Tool, 0)
	for _, t := range a.tools {
		if !strings.HasPrefix(t.Name, prefix) {
			filtered = append(filtered, t)
		}
	}
	a.tools = filtered
	return nil
}

func (a *Agent) State() *AgentState {
	return &AgentState{
		UserID:  a.userID,
		AgentID: a.agentID,
		Tools:   a.tools,
	}
}

// AgentOptions is a legacy type for node test compatibility.
type AgentOptions struct {
	SystemPrompt string
	SessionID    string
	UserID       string
	AgentID      string
}

// NewAgent creates a legacy Agent for node test compatibility.
func NewAgent(opts AgentOptions) *Agent {
	return &Agent{
		ID:      opts.SessionID,
		userID:  opts.UserID,
		agentID: opts.AgentID,
		tools:   make([]Tool, 0),
	}
}
