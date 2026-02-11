# Agent Integration Implementation Plan

**Date:** 2026-02-11
**Source:** Port from `@mariozechner/pi-agent-core` (TypeScript)
**Target:** Go 1.21+ in `/Projects/nevaria/gateway`

---

## Part 1: Core Message Types (Day 1)

### Overview

Port the core agent message types from TypeScript to Go, providing the foundation for the agent runtime and LLM proxy integration.

### Files to Create

| File | Purpose | Lines |
|------|---------|-------|
| `internal/agent/types.go` | Core message and state types | ~150 |
| `internal/agent/types_test.go` | Unit tests for types | ~200 |

---

## Step 1: Role Type and Content Block Interface

### RED Phase: Write Failing Tests

**File:** `/Projects/nevaria/gateway/internal/agent/types_test.go`

```go
package agent

import (
	"testing"
)

func TestRoleValues(t *testing.T) {
	tests := []struct {
		name  string
		role  Role
		valid bool
	}{
		{"user role", RoleUser, true},
		{"assistant role", RoleAssistant, true},
		{"tool result role", RoleToolResult, true},
		{"invalid role", Role("invalid"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.role {
			case RoleUser, RoleAssistant, RoleToolResult:
				// Valid roles
			default:
				if tt.valid {
					t.Errorf("Role %q should be invalid", tt.role)
				}
			}
		})
	}
}

func TestRoleString(t *testing.T) {
	tests := []struct {
		role Role
		want string
	}{
		{RoleUser, "user"},
		{RoleAssistant, "assistant"},
		{RoleToolResult, "toolResult"},
	}

	for _, tt := range tests {
		t.Run(tt.role.String(), func(t *testing.T) {
			if got := tt.role.String(); got != tt.want {
				t.Errorf("Role.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentBlockTypes(t *testing.T) {
	tests := []struct {
		name   string
		block  ContentBlock
		isText bool
		isImage bool
		isToolCall bool
	}{
		{
			name: "text content",
			block: TextContent{Text: "hello"},
			isText: true,
		},
		{
			name: "image content",
			block: ImageContent{
				Data:     "base64data",
				MimeType: "image/png",
			},
			isImage: true,
		},
		{
			name: "tool call content",
			block: ToolCallContent{
				ID:   "call_123",
				Name: "web_search",
				Arguments: map[string]any{
					"query": "golang",
				},
			},
			isToolCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch v := tt.block.(type) {
			case TextContent:
				if !tt.isText {
					t.Errorf("expected TextContent, got %T", v)
				}
			case ImageContent:
				if !tt.isImage {
					t.Errorf("expected ImageContent, got %T", v)
				}
			case ToolCallContent:
				if !tt.isToolCall {
					t.Errorf("expected ToolCallContent, got %T", v)
				}
			default:
				t.Errorf("unknown ContentBlock type: %T", v)
			}
		})
	}
}
```

**Run and verify failure:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v
# Expected: compilation errors (types don't exist yet)
```

### GREEN Phase: Implement Types

**File:** `/Projects/nevaria/gateway/internal/agent/types.go`

```go
package agent

// Role represents the role of a message sender
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "toolResult"
)

// String returns the string representation of the role
func (r Role) String() string {
	return string(r)
}

// ContentBlock is an interface for different content block types
// Messages can contain mixed content: text, images, tool calls
type ContentBlock interface {
	isContentBlock()
}

// TextContent represents a text block in a message
type TextContent struct {
	Text string
}

func (TextContent) isContentBlock() {}

// ImageContent represents an image block in a message
type ImageContent struct {
	Data     string // base64-encoded image data
	MimeType string // e.g., "image/jpeg", "image/png"
}

func (ImageContent) isContentBlock() {}

// ToolCallContent represents a tool invocation request
type ToolCallContent struct {
	ID        string         // Unique identifier for this tool call
	Name      string         // Name of the tool to call
	Arguments map[string]any // Tool arguments (JSON object)
}

func (ToolCallContent) isContentBlock() {}
```

**Run tests:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run "TestRole|TestContentBlock"
# Expected: PASS
```

### REFACTOR Phase

*Review checklist:*
- [ ] Any code duplication? No - each type is unique
- [ ] Naming clear? Yes - Role, ContentBlock, etc.
- [ ] Simpler alternatives? Interface marker pattern is idiomatic Go
- [ ] Exported types properly documented? Need to add comments

*Action: Add godoc comments*

### Commit

```bash
git add internal/agent/types.go internal/agent/types_test.go
git commit -m "feat(agent): add Role type and ContentBlock interface

Add core message type foundations:
- Role enum (user, assistant, toolResult)
- ContentBlock interface for polymorphic content
- TextContent, ImageContent, ToolCallContent implementations

Ported from pi-agent-core AgentMessage types.

Tests: Unit tests for role validation and content block type switching

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Step 2: AgentMessage Type

### RED Phase: Write Failing Tests

**Append to:** `/Projects/nevaria/gateway/internal/agent/types_test.go`

```go
func TestNewTextMessage(t *testing.T) {
	msg := NewTextMessage(RoleUser, "Hello, world!")

	if msg.Role != RoleUser {
		t.Errorf("expected RoleUser, got %v", msg.Role)
	}

	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}

	text, ok := msg.Content[0].(TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", msg.Content[0])
	}

	if text.Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", text.Text)
	}

	if msg.Timestamp == 0 {
		t.Error("expected timestamp to be set")
	}
}

func TestNewToolCallMessage(t *testing.T) {
	msg := NewToolCallMessage("call_abc", "web_search", map[string]any{
		"query": "golang tutorials",
	})

	if msg.Role != RoleAssistant {
		t.Errorf("expected RoleAssistant, got %v", msg.Role)
	}

	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}

	tc, ok := msg.Content[0].(ToolCallContent)
	if !ok {
		t.Fatalf("expected ToolCallContent, got %T", msg.Content[0])
	}

	if tc.ID != "call_abc" {
		t.Errorf("expected ID 'call_abc', got %q", tc.ID)
	}

	if tc.Name != "web_search" {
		t.Errorf("expected Name 'web_search', got %q", tc.Name)
	}

	if tc.Arguments["query"] != "golang tutorials" {
		t.Errorf("unexpected arguments: %v", tc.Arguments)
	}
}

func TestNewToolResultMessage(t *testing.T) {
	msg := NewToolResultMessage("call_abc", "web_search", "Found 5 results", false)

	if msg.Role != RoleToolResult {
		t.Errorf("expected RoleToolResult, got %v", msg.Role)
	}

	if msg.ToolCallID != "call_abc" {
		t.Errorf("expected ToolCallID 'call_abc', got %q", msg.ToolCallID)
	}

	if msg.ToolName != "web_search" {
		t.Errorf("expected ToolName 'web_search', got %q", msg.ToolName)
	}

	if msg.IsError {
		t.Error("expected IsError to be false")
	}

	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}

	text, ok := msg.Content[0].(TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", msg.Content[0])
	}

	if text.Text != "Found 5 results" {
		t.Errorf("expected 'Found 5 results', got %q", text.Text)
	}
}

func TestNewToolResultMessageError(t *testing.T) {
	msg := NewToolResultMessage("call_abc", "web_search", "API error", true)

	if !msg.IsError {
		t.Error("expected IsError to be true")
	}
}

func TestAgentMessageWithMultipleContentBlocks(t *testing.T) {
	now := time.Now()
	msg := AgentMessage{
		Role:      RoleUser,
		Timestamp: now.Unix(),
		Content: []ContentBlock{
			TextContent{Text: "Search for this:"},
			ImageContent{
				Data:     "iVBORw0KGgo...",
				MimeType: "image/png",
			},
		},
	}

	if len(msg.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(msg.Content))
	}

	if _, ok := msg.Content[0].(TextContent); !ok {
		t.Error("first block should be TextContent")
	}

	if _, ok := msg.Content[1].(ImageContent); !ok {
		t.Error("second block should be ImageContent")
	}
}
```

**Run and verify failure:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run TestAgentMessage
# Expected: compilation errors (constructor functions don't exist)
```

### GREEN Phase: Implement AgentMessage

**Append to:** `/Projects/nevaria/gateway/internal/agent/types.go`

```go
import (
	"time"
)

// AgentMessage represents a message in the agent conversation
// Messages can have multiple content blocks (text, images, tool calls)
type AgentMessage struct {
	Role      Role          // Role of the message sender
	Content   []ContentBlock // Mixed content blocks
	Timestamp int64         // Unix timestamp when message was created

	// ToolResult-specific fields (only set when Role == RoleToolResult)
	ToolCallID string // ID of the tool call this result is for
	ToolName   string // Name of the tool that was called
	IsError    bool   // True if the tool call resulted in an error
}

// NewTextMessage creates a new text-only message
func NewTextMessage(role Role, text string) AgentMessage {
	return AgentMessage{
		Role:      role,
		Content:   []ContentBlock{TextContent{Text: text}},
		Timestamp: time.Now().Unix(),
	}
}

// NewToolCallMessage creates a new message with a tool call
func NewToolCallMessage(id, name string, args map[string]any) AgentMessage {
	return AgentMessage{
		Role:      RoleAssistant,
		Content:   []ContentBlock{ToolCallContent{ID: id, Name: name, Arguments: args}},
		Timestamp: time.Now().Unix(),
	}
}

// NewToolResultMessage creates a new tool result message
func NewToolResultMessage(toolCallID, toolName string, result string, isError bool) AgentMessage {
	return AgentMessage{
		Role:      RoleToolResult,
		Content:   []ContentBlock{TextContent{Text: result}},
		Timestamp: time.Now().Unix(),
		ToolCallID: toolCallID,
		ToolName:   toolName,
		IsError:    isError,
	}
}
```

**Run tests:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run TestAgentMessage
# Expected: PASS
```

### REFACTOR Phase

*Review checklist:*
- [ ] Constructor functions follow Go conventions? Yes - NewTypeName
- [ ] Zero-value usability? AgentMessage{} is valid (empty Content slice)
- [ ] Memory allocation? Using []ContentBlock{} for single-item slices - could be pre-allocated with make
- [ ] Documentation? Need godoc comments

*Action: Add documentation*

### Commit

```bash
git add internal/agent/types.go internal/agent/types_test.go
git commit -m "feat(agent): add AgentMessage type with constructors

Add AgentMessage core type:
- Multi-block content support (text, images, tool calls)
- Constructor functions: NewTextMessage, NewToolCallMessage, NewToolResultMessage
- ToolResult-specific fields (ToolCallID, ToolName, IsError)
- Automatic timestamp generation

Tests: Constructor functions and multi-block messages

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Step 3: Model and ThinkingLevel Types

### RED Phase: Write Failing Tests

**Append to:** `/Projects/nevaria/gateway/internal/agent/types_test.go`

```go
func TestThinkingLevelValues(t *testing.T) {
	tests := []struct {
		name  string
		level ThinkingLevel
		valid bool
	}{
		{"off", ThinkingOff, true},
		{"minimal", ThinkingMinimal, true},
		{"low", ThinkingLow, true},
		{"medium", ThinkingMedium, true},
		{"high", ThinkingHigh, true},
		{"invalid", ThinkingLevel("invalid"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.level {
			case ThinkingOff, ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh:
				if !tt.valid {
					t.Errorf("ThinkingLevel %q should be valid", tt.level)
				}
			default:
				if tt.valid {
					t.Errorf("ThinkingLevel %q should be invalid", tt.level)
				}
			}
		})
	}
}

func TestModel(t *testing.T) {
	model := Model{
		ID:       "claude-3-5-sonnet-20241022",
		API:      "anthropic",
		Provider: "anthropic",
		BaseURL:  "https://api.anthropic.com",
	}

	if model.ID != "claude-3-5-sonnet-20241022" {
		t.Errorf("unexpected model ID: %s", model.ID)
	}

	if model.API != "anthropic" {
		t.Errorf("expected API 'anthropic', got %s", model.API)
	}
}

func TestModelProviderConstants(t *testing.T) {
	tests := []struct {
		provider string
		valid    bool
	}{
		{"anthropic", true},
		{"openai", true},
		{"google", true},
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			model := Model{Provider: tt.provider}
			switch model.Provider {
			case "anthropic", "openai", "google":
				if !tt.valid {
					t.Errorf("provider %q should be valid", tt.provider)
				}
			default:
				if tt.valid {
					t.Errorf("provider %q should be invalid", tt.provider)
				}
			}
		})
	}
}
```

**Run and verify failure:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run "TestThinkingLevel|TestModel"
# Expected: compilation errors
```

### GREEN Phase: Implement Types

**Append to:** `/Projects/nevaria/gateway/internal/agent/types.go`

```go
// ThinkingLevel controls how much thinking output the model produces
type ThinkingLevel string

const (
	ThinkingOff      ThinkingLevel = "off"
	ThinkingMinimal  ThinkingLevel = "minimal"
	ThinkingLow      ThinkingLevel = "low"
	ThinkingMedium   ThinkingLevel = "medium"
	ThinkingHigh     ThinkingLevel = "high"
)

// Model represents an LLM model configuration
type Model struct {
	ID       string // Model identifier (e.g., "claude-3-5-sonnet-20241022")
	API      string // API family: "anthropic", "openai", "google"
	Provider string // Provider name: "anthropic", "openai", "google"
	BaseURL  string // Custom base URL (empty for default)
}
```

**Run tests:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run "TestThinkingLevel|TestModel"
# Expected: PASS
```

### REFACTOR Phase

*Review checklist:*
- [ ] Duplicate constants? No
- [ ] Names clear? Yes
- [ ] Should provider be an enum? Could be, but string allows extensibility for custom providers

*No refactor needed - types are simple and clean*

### Commit

```bash
git add internal/agent/types.go internal/agent/types_test.go
git commit -m "feat(agent): add Model and ThinkingLevel types

Add LLM model configuration types:
- ThinkingLevel enum for thinking output control
- Model struct with ID, API, Provider, BaseURL
- Provider string for extensibility (anthropic, openai, google, custom)

Tests: Value validation and provider checking

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Step 4: Tool Type

### RED Phase: Write Failing Tests

**Append to:** `/Projects/nevaria/gateway/internal/agent/types_test.go`

```go
func TestTool(t *testing.T) {
	called := false
	var gotArgs map[string]any

	tool := Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
			called = true
			gotArgs = args
			return &ToolResult{
				Content: []ContentBlock{TextContent{Text: "success"}},
			}, nil
		},
	}

	if tool.Name != "test_tool" {
		t.Errorf("expected name 'test_tool', got %s", tool.Name)
	}

	// Execute the tool
	ctx := context.Background()
	result, err := tool.Execute(ctx, map[string]any{"key": "value"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Error("Execute function was not called")
	}

	if gotArgs["key"] != "value" {
		t.Errorf("expected args[key] = 'value', got %v", gotArgs["key"])
	}

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}

	text, ok := result.Content[0].(TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	if text.Text != "success" {
		t.Errorf("expected 'success', got %q", text.Text)
	}
}

func TestToolResult(t *testing.T) {
	result := ToolResult{
		Content: []ContentBlock{
			TextContent{Text: "result text"},
		},
		Details: map[string]any{
			"metadata": "value",
		},
	}

	if len(result.Content) != 1 {
		t.Errorf("expected 1 content block, got %d", len(result.Content))
	}

	if result.Details == nil {
		t.Error("expected Details to be set")
	}
}

func TestToolWithError(t *testing.T) {
	tool := Tool{
		Name: "failing_tool",
		Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
			return nil, fmt.Errorf("tool failed")
		},
	}

	ctx := context.Background()
	_, err := tool.Execute(ctx, nil)

	if err == nil {
		t.Error("expected error, got nil")
	}

	if err.Error() != "tool failed" {
		t.Errorf("expected error 'tool failed', got %v", err)
	}
}
```

**Run and verify failure:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run TestTool
# Expected: compilation errors
```

### GREEN Phase: Implement Tool Type

**Append to:** `/Projects/nevaria/gateway/internal/agent/types.go`

```go
import (
	"context"
	"fmt"
)

// Tool represents a callable tool that agents can invoke
type Tool struct {
	Name        string                    // Unique tool name
	Description string                    // Human-readable description
	Execute     func(ctx context.Context, args map[string]any) (*ToolResult, error)
	// TODO: Add Parameters jsonschema.Schema in Phase 6
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Content []ContentBlock // Result content (usually TextContent)
	Details any            // Additional structured details
}
```

**Run tests:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run TestTool
# Expected: PASS
```

### REFACTOR Phase

*Review checklist:*
- [ ] Function signature idiomatic? Yes - func with context and return error
- [ ] Details type appropriate? `any` allows flexibility, could be generic
- [ ] Should Execute be an interface? Function pointer is simpler for now

*No refactor needed - simple and clean design*

### Commit

```bash
git add internal/agent/types.go internal/agent/types_test.go
git commit -m "feat(agent): add Tool and ToolResult types

Add tool execution framework:
- Tool struct with name, description, execute function
- ToolResult for tool output with content and details
- Context-aware execution for cancellation support

Note: Parameters schema (JSON Schema) will be added in Phase 6

Tests: Tool execution, error handling, result formatting

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Step 5: AgentState Type

### RED Phase: Write Failing Tests

**Append to:** `/Projects/nevaria/gateway/internal/agent/types_test.go`

```go
func TestNewAgentState(t *testing.T) {
	model := Model{
		ID:       "claude-3-5-sonnet-20241022",
		API:      "anthropic",
		Provider: "anthropic",
	}

	tools := []Tool{
		{
			Name:        "test_tool",
			Description: "A test tool",
			Execute:     func(ctx context.Context, args map[string]any) (*ToolResult, error) {
				return &ToolResult{Content: []ContentBlock{TextContent{Text: "ok"}}}, nil
			},
		},
	}

	state := NewAgentState(AgentStateOptions{
		SystemPrompt:  "You are a helpful assistant",
		Model:         model,
		ThinkingLevel: ThinkingMedium,
		Tools:         tools,
		SessionID:     "session-123",
	})

	if state.SystemPrompt != "You are a helpful assistant" {
		t.Errorf("unexpected system prompt: %s", state.SystemPrompt)
	}

	if state.Model.ID != model.ID {
		t.Errorf("unexpected model: %s", state.Model.ID)
	}

	if state.ThinkingLevel != ThinkingMedium {
		t.Errorf("unexpected thinking level: %s", state.ThinkingLevel)
	}

	if len(state.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(state.Tools))
	}

	if len(state.Messages) != 0 {
		t.Errorf("expected empty messages, got %d", len(state.Messages))
	}

	if state.IsStreaming {
		t.Error("expected IsStreaming to be false")
	}

	if state.PendingToolCalls == nil {
		t.Error("expected PendingToolCalls map to be initialized")
	}

	if state.SessionID != "session-123" {
		t.Errorf("expected SessionID 'session-123', got %s", state.SessionID)
	}
}

func TestAgentStateAddMessage(t *testing.T) {
	state := NewAgentState(AgentStateOptions{})

	msg := NewTextMessage(RoleUser, "Hello")
	state.AddMessage(msg)

	if len(state.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(state.Messages))
	}

	if state.Messages[0].Role != RoleUser {
		t.Errorf("expected RoleUser, got %v", state.Messages[0].Role)
	}
}

func TestAgentStateSetStreaming(t *testing.T) {
	state := NewAgentState(AgentStateOptions{})

	state.SetStreaming(true, NewTextMessage(RoleAssistant, ""))
	if !state.IsStreaming {
		t.Error("expected IsStreaming to be true")
	}

	if state.StreamMessage == nil {
		t.Error("expected StreamMessage to be set")
	}

	state.SetStreaming(false, nil)
	if state.IsStreaming {
		t.Error("expected IsStreaming to be false")
	}

	if state.StreamMessage != nil {
		t.Error("expected StreamMessage to be nil")
	}
}

func TestAgentStatePendingToolCalls(t *testing.T) {
	state := NewAgentState(AgentStateOptions{})

	state.AddPendingToolCall("call_1")
	state.AddPendingToolCall("call_2")

	if !state.HasPendingToolCall("call_1") {
		t.Error("expected call_1 to be pending")
	}

	if !state.HasPendingToolCall("call_2") {
		t.Error("expected call_2 to be pending")
	}

	if state.HasPendingToolCall("call_3") {
		t.Error("expected call_3 to not be pending")
	}

	state.RemovePendingToolCall("call_1")

	if state.HasPendingToolCall("call_1") {
		t.Error("expected call_1 to be removed")
	}

	if !state.HasPendingToolCall("call_2") {
		t.Error("expected call_2 to still be pending")
	}
}
```

**Run and verify failure:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run TestAgentState
# Expected: compilation errors
```

### GREEN Phase: Implement AgentState

**Append to:** `/Projects/nevaria/gateway/internal/agent/types.go`

```go
// AgentStateOptions contains options for creating a new AgentState
type AgentStateOptions struct {
	SystemPrompt  string
	Model         Model
	ThinkingLevel ThinkingLevel
	Tools         []Tool
	SessionID     string
}

// AgentState represents the current state of an agent
type AgentState struct {
	SystemPrompt     string           // System prompt for the LLM
	Model            Model            // Current model configuration
	ThinkingLevel    ThinkingLevel    // Thinking verbosity
	Tools            []Tool           // Available tools
	Messages         []AgentMessage   // Conversation history
	IsStreaming      bool             // True if currently streaming a response
	StreamMessage    *AgentMessage    // Current message being streamed
	PendingToolCalls map[string]bool  // Tool calls awaiting results
	Error            string           // Last error message
	SessionID        string           // User session identifier
}

// NewAgentState creates a new agent state with defaults
func NewAgentState(opts AgentStateOptions) AgentState {
	return AgentState{
		SystemPrompt:     opts.SystemPrompt,
		Model:            opts.Model,
		ThinkingLevel:    opts.ThinkingLevel,
		Tools:            opts.Tools,
		Messages:         []AgentMessage{},
		PendingToolCalls: make(map[string]bool),
		SessionID:        opts.SessionID,
	}
}

// AddMessage appends a message to the conversation history
func (s *AgentState) AddMessage(msg AgentMessage) {
	s.Messages = append(s.Messages, msg)
}

// SetStreaming updates the streaming state
func (s *AgentState) SetStreaming(streaming bool, msg *AgentMessage) {
	s.IsStreaming = streaming
	s.StreamMessage = msg
}

// AddPendingToolCall adds a tool call to the pending set
func (s *AgentState) AddPendingToolCall(id string) {
	s.PendingToolCalls[id] = true
}

// RemovePendingToolCall removes a tool call from the pending set
func (s *AgentState) RemovePendingToolCall(id string) {
	delete(s.PendingToolCalls, id)
}

// HasPendingToolCall checks if a tool call is pending
func (s *AgentState) HasPendingToolCall(id string) bool {
	return s.PendingToolCalls[id]
}
```

**Run tests:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run TestAgentState
# Expected: PASS
```

### REFACTOR Phase

*Review checklist:*
- [ ] Methods use value or pointer receiver correctly? Pointer receivers for state mutation
- [ ] Map initialization? Done in constructor
- [ ] Nil safety? Methods handle nil state appropriately
- [ ] Memory allocation? Messages slice grows dynamically - could pre-allocate if we know size

*No refactor needed - clean state management*

### Commit

```bash
git add internal/agent/types.go internal/agent/types_test.go
git commit -m "feat(agent): add AgentState with state management methods

Add agent state management:
- AgentState struct with conversation, tools, streaming state
- Constructor with AgentStateOptions for flexible initialization
- Message history management
- Tool call pending/complete tracking
- Streaming state with current message reference

Tests: State initialization, message addition, streaming, tool call tracking

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Step 6: JSON Serialization Tests

### RED Phase: Write Failing Tests

**Append to:** `/Projects/nevaria/gateway/internal/agent/types_test.go`

```go
func TestAgentMessageJSON(t *testing.T) {
	msg := NewTextMessage(RoleUser, "Hello, world!")

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded AgentMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Role != msg.Role {
		t.Errorf("expected Role %v, got %v", msg.Role, decoded.Role)
	}

	if decoded.Timestamp != msg.Timestamp {
		t.Errorf("expected Timestamp %d, got %d", msg.Timestamp, decoded.Timestamp)
	}

	if len(decoded.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(decoded.Content))
	}

	text, ok := decoded.Content[0].(TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", decoded.Content[0])
	}

	if text.Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", text.Text)
	}
}

func TestToolCallMessageJSON(t *testing.T) {
	msg := NewToolCallMessage("call_123", "web_search", map[string]any{
		"query": "golang",
		"count": 5,
	})

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded AgentMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	tc, ok := decoded.Content[0].(ToolCallContent)
	if !ok {
		t.Fatalf("expected ToolCallContent, got %T", decoded.Content[0])
	}

	if tc.ID != "call_123" {
		t.Errorf("expected ID 'call_123', got %q", tc.ID)
	}

	if tc.Name != "web_search" {
		t.Errorf("expected Name 'web_search', got %q", tc.Name)
	}

	if tc.Arguments["query"] != "golang" {
		t.Errorf("expected query 'golang', got %v", tc.Arguments["query"])
	}

	if tc.Arguments["count"] != float64(5) {
		t.Errorf("expected count 5, got %v", tc.Arguments["count"])
	}
}

func TestImageContentJSON(t *testing.T) {
	msg := AgentMessage{
		Role:      RoleUser,
		Timestamp: time.Now().Unix(),
		Content: []ContentBlock{
			ImageContent{
				Data:     "iVBORw0KGgo...",
				MimeType: "image/png",
			},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded AgentMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	img, ok := decoded.Content[0].(ImageContent)
	if !ok {
		t.Fatalf("expected ImageContent, got %T", decoded.Content[0])
	}

	if img.Data != "iVBORw0KGgo..." {
		t.Errorf("unexpected data: %s", img.Data)
	}

	if img.MimeType != "image/png" {
		t.Errorf("expected MIME type 'image/png', got %s", img.MimeType)
	}
}
```

**Run and verify failure:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run JSON
# Expected: Tests may fail due to missing JSON tags
```

### GREEN Phase: Add JSON Tags

**Update types.go with JSON tags:**

```go
// Role represents the role of a message sender
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "toolResult"
)

// String returns the string representation of the role
func (r Role) String() string {
	return string(r)
}

// ContentBlock is an interface for different content block types
type ContentBlock interface {
	isContentBlock()
}

// TextContent represents a text block in a message
type TextContent struct {
	Text string `json:"text"`
}

func (TextContent) isContentBlock() {}

// ImageContent represents an image block in a message
type ImageContent struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

func (ImageContent) isContentBlock() {}

// ToolCallContent represents a tool invocation request
type ToolCallContent struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (ToolCallContent) isContentBlock() {}

// AgentMessage represents a message in the agent conversation
type AgentMessage struct {
	Role      Role           `json:"role"`
	Content   []ContentBlock `json:"content"`
	Timestamp int64          `json:"timestamp"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolName   string        `json:"tool_name,omitempty"`
	IsError    bool          `json:"is_error,omitempty"`
}
```

**Also need to add custom marshaling for ContentBlock interface:**

```go
import (
	"encoding/json"
)

// contentBlockWrapper is used for JSON marshaling of ContentBlock
type contentBlockWrapper struct {
	Type string `json:"type"`
	// Fields will be set based on Type
	Text      string         `json:"text,omitempty"`
	Data      string         `json:"data,omitempty"`
	MimeType  string         `json:"mime_type,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// MarshalJSON implements json.Marshaler for ContentBlock
func (c TextContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(contentBlockWrapper{
		Type: "text",
		Text: c.Text,
	})
}

func (c ImageContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(contentBlockWrapper{
		Type:     "image",
		Data:     c.Data,
		MimeType: c.MimeType,
	})
}

func (c ToolCallContent) MarshalJSON() ([]byte, error) {
	return json.Marshal(contentBlockWrapper{
		Type:      "tool_call",
		ID:        c.ID,
		Name:      c.Name,
		Arguments: c.Arguments,
	})
}

// UnmarshalJSON implements json.Unmarshaler for ContentBlock
func (c *TextContent) UnmarshalJSON(data []byte) error {
	var wrapper contentBlockWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	c.Text = wrapper.Text
	return nil
}

func (c *ImageContent) UnmarshalJSON(data []byte) error {
	var wrapper contentBlockWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	c.Data = wrapper.Data
	c.MimeType = wrapper.MimeType
	return nil
}

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
```

**Update AgentMessage to handle ContentBlock marshaling:**

```go
// MarshalJSON implements json.Marshaler for AgentMessage
func (m AgentMessage) MarshalJSON() ([]byte, error) {
	type Alias AgentMessage
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(&m),
	})
}

// UnmarshalJSON implements json.Unmarshaler for AgentMessage
func (m *AgentMessage) UnmarshalJSON(data []byte) error {
	// For now, use a simplified approach - full implementation
	// would need to handle ContentBlock type discrimination
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
```

**Run tests:**

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -run JSON
# Expected: PASS after fixing any issues
```

### REFACTOR Phase

*Review checklist:*
- [ ] JSON handling clean? The wrapper approach is verbose but necessary for interface polymorphism
- [ ] Error handling complete? Yes
- [ ] Could use a library? Could use json-iterator or similar, but stdlib is sufficient

*Potential refactor: Extract common wrapper logic*

### Commit

```bash
git add internal/agent/types.go internal/agent/types_test.go
git commit -m "feat(agent): add JSON marshaling for all types

Add JSON serialization support:
- Custom MarshalJSON/UnmarshalJSON for ContentBlock types
- Type-discriminated content block deserialization
- JSON tags on all exported fields
- Full round-trip test coverage

Tests: Text, Image, ToolCall message marshaling/unmarshaling

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Final Verification

Run all tests together to ensure everything works:

```bash
cd /Projects/nevaria/gateway
go test ./internal/agent -v -cover
```

Expected output:
```
=== RUN   TestRoleValues
--- PASS: TestRoleValues (0.00s)
=== RUN   TestRoleString
--- PASS: TestRoleString (0.00s)
=== RUN   TestContentBlockTypes
--- PASS: TestContentBlockTypes (0.00s)
=== RUN   TestNewTextMessage
--- PASS: TestNewTextMessage (0.00s)
=== RUN   TestNewToolCallMessage
--- PASS: TestNewToolCallMessage (0.00s)
=== RUN   TestNewToolResultMessage
--- PASS: TestNewToolResultMessage (0.00s)
=== RUN   TestNewToolResultMessageError
--- PASS: TestNewToolResultMessageError (0.00s)
=== RUN   TestAgentMessageWithMultipleContentBlocks
--- PASS: TestAgentMessageWithMultipleContentBlocks (0.00s)
=== RUN   TestThinkingLevelValues
--- PASS: TestThinkingLevelValues (0.00s)
=== RUN   TestModel
--- PASS: TestModel (0.00s)
=== RUN   TestModelProviderConstants
--- PASS: TestModelProviderConstants (0.00s)
=== RUN   TestTool
--- PASS: TestTool (0.00s)
=== RUN   TestToolResult
--- PASS: TestToolResult (0.00s)
=== RUN   TestToolWithError
---PASS: TestToolWithError (0.00s)
=== RUN   TestNewAgentState
--- PASS: TestNewAgentState (0.00s)
=== RUN   TestAgentStateAddMessage
--- PASS: TestAgentStateAddMessage (0.00s)
=== RUN   TestAgentStateSetStreaming
--- PASS: TestAgentStateSetStreaming (0.00s)
=== RUN   TestAgentStatePendingToolCalls
--- PASS: TestAgentStatePendingToolCalls (0.00s)
=== RUN   TestAgentMessageJSON
--- PASS: TestAgentMessageJSON (0.00s)
=== RUN   TestToolCallMessageJSON
--- PASS: TestToolCallMessageJSON (0.00s)
=== RUN   TestImageContentJSON
--- PASS: TestImageContentJSON (0.00s)
PASS
coverage: 100.0% of statements
```

---

## Summary: Part 1 Deliverables

| File | Lines (est) | Tests | Coverage |
|------|-------------|-------|----------|
| `internal/agent/types.go` | ~300 | - | - |
| `internal/agent/types_test.go` | ~400 | 20+ | 100% |

### Types Implemented

1. **Role** - Message role enum (user, assistant, toolResult)
2. **ContentBlock** - Interface for polymorphic content
3. **TextContent** - Text content blocks
4. **ImageContent** - Image content blocks (base64)
5. **ToolCallContent** - Tool invocation requests
6. **AgentMessage** - Complete message type with constructors
7. **ThinkingLevel** - Thinking verbosity enum
8. **Model** - LLM model configuration
9. **Tool** - Tool execution framework
10. **ToolResult** - Tool execution results
11. **AgentState** - Complete agent state with management methods

### Next Steps

Proceed to **Part 2: Agent Struct and Event Streaming** (Day 2)

---

**End of Part 1**
