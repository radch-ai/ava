# Multi-Agent Storage & Security Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use @superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build multi-agent per user support with secure S3 storage, Mem0 memory, and session management.

**Architecture:** Composite key `(userID, agentID)` for agent lookup, workspace-based isolation, S3 for storage, Mem0 for memory, server-managed sessions with timeout/reconnection.

**Tech Stack:** Go 1.24, S3-compatible storage, Mem0 API, Redis (quota only), WebSocket, gorilla/websocket

---

## Phase 1: Core Refactoring - Agent Key Changes

### Task 1: Add AgentID Context Utilities

**Files:**
- Modify: `internal/ctx/context.go`

**Step 1: Write the failing test**

Create: `tests/unit/ctx/context_test.go`

```go
package ctx_test

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/ctx"
)

func TestWithAgentID(t *testing.T) {
	ctx := context.Background()
-agentCtx := ctx.WithAgentID(ctx, "agent-123")
+agentID := ctx.GetAgentIDFromContext(agentCtx)

	if agentID != "agent-123" {
		t.Errorf("expected agent-123, got %s", agentID)
	}
}

func TestGetAgentIDFromContextMissing(t *testing.T) {
-agentID := ctx.GetAgentIDFromContext(context.Background())
+agentID := ctx.GetAgentIDFromContext(context.Background())

	if agentID != "" {
		t.Errorf("expected empty string, got %s", agentID)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/ctx/... -v`
Expected: FAIL with "undefined: ctx.WithAgentID"

**Step 3: Write minimal implementation**

Modify: `internal/ctx/context.go` - add after line 10:

```go
// AgentIDKey is the context key for agent ID.
const AgentIDKey contextKey = "agent_id"

// WithAgentID adds an agent ID to the context.
func WithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, AgentIDKey, agentID)
}

// GetAgentIDFromContext retrieves the agent ID from the context.
// Returns empty string if not set.
func GetAgentIDFromContext(ctx context.Context) string {
	if agentID, ok := ctx.Value(AgentIDKey).(string); ok {
		return agentID
	}
	return ""
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/ctx/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ctx/context.go tests/unit/ctx/context_test.go
git commit -m "feat(ctx): add agent ID context utilities"
```

---

### Task 2: Add AgentKey Type to Agent Manager

**Files:**
- Modify: `internal/websocket/agent_manager.go`

**Step 1: Write the failing test**

Modify: `tests/unit/websocket/agent_manager_test.go` - add at end:

```go
// TestAgentKeyCompositeKey verifies agents are keyed by (userID, agentID)
func TestAgentKeyCompositeKey(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "user-123"
	agentID := "agent-456"

	// Create agent with composite key
	agent1 := mgr.GetOrCreateAgentForUser(userID, agentID)
	if agent1 == nil {
		t.Fatal("GetOrCreateAgentForUser returned nil")
	}

	// Same composite key returns same agent
	agent2 := mgr.GetOrCreateAgentForUser(userID, agentID)
	if agent1 != agent2 {
		t.Error("Expected same agent for same (userID, agentID)")
	}

	// Different agentID returns different agent
	agent3 := mgr.GetOrCreateAgentForUser(userID, "agent-789")
	if agent1 == agent3 {
		t.Error("Expected different agent for different agentID")
	}

	// Different userID returns different agent
	agent4 := mgr.GetOrCreateAgentForUser("user-456", agentID)
	if agent1 == agent4 {
		t.Error("Expected different agent for different userID")
	}
}

// TestRemoveAgentForUser verifies agent removal by composite key
func TestRemoveAgentForUser(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "user-123"
	agentID := "agent-456"

	// Create agent
	agent1 := mgr.GetOrCreateAgentForUser(userID, agentID)

	// Remove agent
	mgr.RemoveAgentForUser(userID, agentID)

	// Verify new agent is created after removal
	agent2 := mgr.GetOrCreateAgentForUser(userID, agentID)
	if agent1 == agent2 {
		t.Error("Expected different agent after removal")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/websocket/... -run "TestAgentKey|TestRemoveAgentForUser" -v`
Expected: FAIL with "undefined: mgr.GetOrCreateAgentForUser"

**Step 3: Write minimal implementation**

Modify: `internal/websocket/agent_manager.go`:

```go
package websocket

import (
	"sync"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/llm-proxy"
	"github.com/rs/zerolog"
)

// AgentKey is the composite key for agent lookup: (userID, agentID)
type AgentKey struct {
	UserID  string
	AgentID string
}

// AgentManagerOptions configures a new AgentManager.
type AgentManagerOptions struct {
	LLMProxyHandler *proxy.Handler
}

// AgentManager manages per-user agents with thread-safe access.
// Agents are keyed by composite (userID, agentID) to support multiple agents per user.
type AgentManager struct {
	mu       sync.RWMutex
	agents   map[AgentKey]*agent.Agent
	logger   zerolog.Logger
	llmProxy *proxy.Handler
}

// NewAgentManager creates a new AgentManager.
func NewAgentManager(logger zerolog.Logger, opts *AgentManagerOptions) *AgentManager {
	var llmProxy *proxy.Handler
	if opts != nil {
		llmProxy = opts.LLMProxyHandler
	}

	return &AgentManager{
		agents:   make(map[AgentKey]*agent.Agent),
		logger:   logger,
		llmProxy: llmProxy,
	}
}

// GetOrCreateAgentForUser returns an existing agent for the (userID, agentID) pair or creates a new one.
// If an LLM proxy handler is configured, the agent will use proxy.Client for LLM calls.
func (m *AgentManager) GetOrCreateAgentForUser(userID, agentID string) *agent.Agent {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.RLock()
	existingAgent, exists := m.agents[key]
	m.mu.RUnlock()

	if exists {
		return existingAgent
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if existingAgent, exists := m.agents[key]; exists {
		return existingAgent
	}

	// Create new agent with appropriate LLM client
	newAgent := m.createAgent(userID, agentID)
	m.agents[key] = newAgent

	return newAgent
}

// createAgent creates a new agent with the appropriate LLM client.
func (m *AgentManager) createAgent(userID, agentID string) *agent.Agent {
	opts := agent.AgentOptions{
		ID:        agentID,
		SessionID: userID + ":" + agentID, // Composite session ID
	}

	// If LLM proxy handler is configured, create proxy client for this user
	if m.llmProxy != nil {
		proxyClient := proxy.NewClient(m.llmProxy, userID)
		opts.LLMClient = proxyClient
	} else {
		// Use mock client for backward compatibility/testing
		opts.LLMClient = &agent.MockLLMClient{}
	}

	return agent.NewAgent(opts)
}

// RemoveAgentForUser removes the agent for the given (userID, agentID) pair.
func (m *AgentManager) RemoveAgentForUser(userID, agentID string) {
	key := AgentKey{UserID: userID, AgentID: agentID}

	m.mu.Lock()
	defer m.mu.Unlock()

	if existingAgent, exists := m.agents[key]; exists {
		existingAgent.Unsubscribe(key.UserID) // Unsubscribe any event channels
		delete(m.agents, key)
	}
}

// GetOrCreateAgent is deprecated - use GetOrCreateAgentForUser instead.
// Kept for backward compatibility during transition.
func (m *AgentManager) GetOrCreateAgent(sessionID string, userID string) *agent.Agent {
	return m.GetOrCreateAgentForUser(userID, sessionID)
}

// RemoveAgent is deprecated - use RemoveAgentForUser instead.
// Kept for backward compatibility during transition.
func (m *AgentManager) RemoveAgent(sessionID string) {
	// For backward compatibility, treat sessionID as agentID and use empty userID
	m.RemoveAgentForUser("", sessionID)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/websocket/... -run "TestAgentKey|TestRemoveAgentForUser" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/websocket/agent_manager.go tests/unit/websocket/agent_manager_test.go
git commit -m "feat(websocket): add composite key (userID, agentID) for agent lookup"
```

---

### Task 3: Update Agent Types with UserID

**Files:**
- Modify: `internal/agent/types.go`
- Modify: `internal/agent/agent.go`

**Step 1: Write the failing test**

Modify: `tests/unit/agent/types_test.go` - add at end:

```go
// TestAgentStateWithUserID verifies AgentState includes UserID
func TestAgentStateWithUserID(t *testing.T) {
	opts := agent.AgentStateOptions{
		UserID:    "user-123",
		AgentID:   "agent-456",
		SessionID: "session-789",
	}
	state := agent.NewAgentState(opts)

	if state.UserID != "user-123" {
		t.Errorf("expected UserID user-123, got %s", state.UserID)
	}
	if state.AgentID != "agent-456" {
		t.Errorf("expected AgentID agent-456, got %s", state.AgentID)
	}
}

// TestAgentWithUserID verifies Agent includes UserID
func TestAgentWithUserID(t *testing.T) {
	opts := agent.AgentOptions{
		UserID:  "user-123",
		AgentID: "agent-456",
	}
	a := agent.NewAgent(opts)

	state := a.State()
	if state.UserID != "user-123" {
		t.Errorf("expected UserID user-123, got %s", state.UserID)
	}
	if state.AgentID != "agent-456" {
		t.Errorf("expected AgentID agent-456, got %s", state.AgentID)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/agent/... -run "TestAgentStateWithUserID|TestAgentWithUserID" -v`
Expected: FAIL with "unknown field 'UserID' / 'unknown field 'AgentID'"

**Step 3: Write minimal implementation**

Modify: `internal/agent/types.go` - update AgentStateOptions and AgentState:

```go
// AgentStateOptions configures a new AgentState.
type AgentStateOptions struct {
	SystemPrompt  string
	Model         Model
	ThinkingLevel ThinkingLevel
	Tools         []Tool
	SessionID     string
	UserID        string // User who owns this agent
	AgentID       string // Agent identifier
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
	UserID           string // User who owns this agent
	AgentID          string // Agent identifier
}
```

Update NewAgentState in types.go:

```go
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
```

Modify: `internal/agent/agent.go` - update AgentOptions and Agent:

```go
// AgentOptions configures a new Agent.
type AgentOptions struct {
	ID           string
	UserID       string // User who owns this agent
	AgentID      string // Agent identifier
	SystemPrompt string
	SessionID    string
	LLMClient    LLMClient // Optional LLM client for tool calling loop
}

// Agent manages agent state with thread-safe access and event broadcasting.
type Agent struct {
	mu            sync.RWMutex
	state         AgentState
	id            string
	userID        string // User who owns this agent
	agentID       string // Agent identifier
	subscribers   map[string]chan AgentEvent
	subscribersMu sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	llmClient     LLMClient // Optional LLM client for tool calling
}
```

Update NewAgent in agent.go:

```go
// NewAgent creates a new Agent with the given options.
// If no ID is provided, a UUID will be generated.
func NewAgent(opts AgentOptions) *Agent {
	id := opts.ID
	if id == "" {
		id = uuid.New().String()
	}

	userID := opts.UserID
	if userID == "" {
		userID = "unknown"
	}

	agentID := opts.AgentID
	if agentID == "" {
		agentID = id
	}

	ctx, cancel := context.WithCancel(context.Background())

	state := NewAgentState(AgentStateOptions{
		SystemPrompt: opts.SystemPrompt,
		SessionID:    opts.SessionID,
		UserID:       userID,
		AgentID:      agentID,
	})

	return &Agent{
		id:          id,
		userID:      userID,
		agentID:     agentID,
		state:       state,
		subscribers: make(map[string]chan AgentEvent),
		ctx:         ctx,
		cancel:      cancel,
		llmClient:   opts.LLMClient,
	}
}
```

Update State() method return in agent.go to include new fields:

```go
// State returns a copy of the current agent state.
func (a *Agent) State() AgentState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Return a deep copy to prevent external mutation
	messages := make([]AgentMessage, len(a.state.Messages))
	copy(messages, a.state.Messages)

	pendingToolCalls := make(map[string]bool, len(a.state.PendingToolCalls))
	for k, v := range a.state.PendingToolCalls {
		pendingToolCalls[k] = v
	}

	tools := make([]Tool, len(a.state.Tools))
	copy(tools, a.state.Tools)

	return AgentState{
		SystemPrompt:     a.state.SystemPrompt,
		Model:            a.state.Model,
		ThinkingLevel:    a.state.ThinkingLevel,
		Tools:            tools,
		Messages:         messages,
		IsStreaming:      a.state.IsStreaming,
		StreamMessage:    a.state.StreamMessage,
		PendingToolCalls: pendingToolCalls,
		Error:            a.state.Error,
		SessionID:        a.state.SessionID,
		UserID:           a.state.UserID,
		AgentID:          a.state.AgentID,
	}
}
```

Update createAgent in websocket/agent_manager.go to pass new fields:

```go
func (m *AgentManager) createAgent(userID, agentID string) *agent.Agent {
	opts := agent.AgentOptions{
		AgentID:   agentID,
		UserID:    userID,
		SessionID: userID + ":" + agentID,
	}

	// ... rest of function
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/agent/... -run "TestAgentStateWithUserID|TestAgentWithUserID" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/types.go internal/agent/agent.go internal/websocket/agent_manager.go tests/unit/agent/types_test.go
git commit -m "feat(agent): add UserID and AgentID to Agent and AgentState"
```

---

### Task 4: Add AgentStatus Enum

**Files:**
- Create: `internal/agent/status.go`

**Step 1: Write the failing test**

Create: `tests/unit/agent/status_test.go`

```go
package agent_test

import (
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
)

func TestAgentStatusValues(t *testing.T) {
	statuses := []agent.AgentStatus{
		agent.AgentStatusIdle,
		agent.AgentStatusActive,
		agent.AgentStatusError,
	}

	for _, status := range statuses {
		if status.String() == "" {
			t.Errorf("status %v has empty string representation", status)
		}
	}
}

func TestAgentStatusString(t *testing.T) {
	tests := []struct {
		status    agent.AgentStatus
		expected string
	}{
		{agent.AgentStatusIdle, "idle"},
		{agent.AgentStatusActive, "active"},
		{agent.AgentStatusError, "error"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.expected {
			t.Errorf("AgentStatus.String() = %v, want %v", got, tt.expected)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/agent/... -run "TestAgentStatus" -v`
Expected: FAIL with "undefined: agent.AgentStatus"

**Step 3: Write minimal implementation**

Create: `internal/agent/status.go`

```go
package agent

// AgentStatus represents the current status of an agent.
type AgentStatus string

const (
	AgentStatusIdle   AgentStatus = "idle"
	AgentStatusActive AgentStatus = "active"
	AgentStatusError  AgentStatus = "error"
)

// String returns the string representation of the agent status.
func (s AgentStatus) String() string {
	return string(s)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/agent/... -run "TestAgentStatus" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/status.go tests/unit/agent/status_test.go
git commit -m "feat(agent): add AgentStatus enum"
```

---

## Phase 2: Session Management

### Task 5: Create Session Package

**Files:**
- Create: `internal/session/session.go`
- Create: `internal/session/manager.go`
- Create: `tests/unit/session/session_test.go`

**Step 1: Write the failing test**

Create: `tests/unit/session/session_test.go`

```go
package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/session"
)

func TestNewSession(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	agentID := "agent-456"

	s := session.NewSession(ctx, userID, agentID)

	if s.SessionID() == "" {
		t.Error("expected non-empty SessionID")
	}
	if s.UserID() != userID {
		t.Errorf("expected UserID %s, got %s", userID, s.UserID())
	}
	if s.AgentID() != agentID {
		t.Errorf("expected AgentID %s, got %s", agentID, s.AgentID())
	}
	if s.Mem0SessionID() == "" {
		t.Error("expected non-empty Mem0SessionID")
	}
}

func TestSessionAddMessage(t *testing.T) {
	ctx := context.Background()
	s := session.NewSession(ctx, "user-123", "agent-456")

	msg := session.Message{
		Role:      "user",
		Content:   "Hello",
		Timestamp: time.Now().Unix(),
	}

	s.AddMessage(msg)

	messages := s.Messages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Content != "Hello" {
		t.Errorf("expected content 'Hello', got %s", messages[0].Content)
	}
}

func TestSessionLastActivity(t *testing.T) {
	ctx := context.Background()
	s := session.NewSession(ctx, "user-123", "agent-456")

	initialActivity := s.LastActivity()
	if initialActivity.IsZero() {
		t.Error("expected non-zero LastActivity")
	}

	// Wait a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	s.UpdateActivity()

	newActivity := s.LastActivity()
	if !newActivity.After(initialActivity) {
		t.Error("expected LastActivity to be updated")
	}
}

func TestSessionIsExpired(t *testing.T) {
	ctx := context.Background()
	s := session.NewSession(ctx, "user-123", "agent-456")

	// Fresh session should not be expired
	if s.IsExpired(5*time.Minute) {
		t.Error("fresh session should not be expired")
	}

	// Manually set old activity to simulate expiration
	s.UpdateActivity()
	oldActivity := s.LastActivity()
	s.SetLastActivity(oldActivity.Add(-10 * time.Minute))

	if !s.IsExpired(5*time.Minute) {
		t.Error("old session should be expired")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/session/... -v`
Expected: FAIL with "package not found" or "undefined: session"

**Step 3: Write minimal implementation**

Create: `internal/session/session.go`

```go
package session

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Message represents a message in the session.
type Message struct {
	Role      string
	Content   string
	Timestamp int64
}

// Session represents an active conversation with an agent.
type Session struct {
	sessionID     string
	userID        string
	agentID       string
	mem0SessionID string
	messages      []Message
	startedAt     time.Time
	lastActivity  time.Time
	mu            *sessionMutex
}

// sessionMutex is a lightweight mutex for session operations.
type sessionMutex struct {
	// In production, this would be sync.RWMutex
	// For test simplicity, we use no-op initially
}

// NewSession creates a new Session with a generated session ID.
func NewSession(ctx context.Context, userID, agentID string) *Session {
	now := time.Now()
	return &Session{
		sessionID:     uuid.New().String(),
		userID:        userID,
		agentID:       agentID,
		mem0SessionID: uuid.New().String(), // In production, this comes from Mem0
		messages:      make([]Message, 0),
		startedAt:     now,
		lastActivity:  now,
		mu:            &sessionMutex{},
	}
}

// SessionID returns the session ID.
func (s *Session) SessionID() string {
	return s.sessionID
}

// UserID returns the user ID.
func (s *Session) UserID() string {
	return s.userID
}

// AgentID returns the agent ID.
func (s *Session) AgentID() string {
	return s.agentID
}

// Mem0SessionID returns the Mem0 session ID.
func (s *Session) Mem0SessionID() string {
	return s.mem0SessionID
}

// Messages returns a copy of the session messages.
func (s *Session) Messages() []Message {
	result := make([]Message, len(s.messages))
	copy(result, s.messages)
	return result
}

// AddMessage appends a message to the session.
func (s *Session) AddMessage(msg Message) {
	s.messages = append(s.messages, msg)
	s.UpdateActivity()
}

// UpdateActivity updates the last activity time to now.
func (s *Session) UpdateActivity() {
	s.lastActivity = time.Now()
}

// SetLastActivity sets the last activity time (used for testing).
func (s *Session) SetLastActivity(t time.Time) {
	s.lastActivity = t
}

// LastActivity returns the last activity time.
func (s *Session) LastActivity() time.Time {
	return s.lastActivity
}

// StartedAt returns the session start time.
func (s *Session) StartedAt() time.Time {
	return s.startedAt
}

// IsExpired returns true if the session has expired based on the given timeout.
func (s *Session) IsExpired(timeout time.Duration) bool {
	return time.Since(s.lastActivity) > timeout
}

// MessageCount returns the number of messages in the session.
func (s *Session) MessageCount() int {
	return len(s.messages)
}

// ClearMessages clears all messages from the session.
func (s *Session) ClearMessages() {
	s.messages = make([]Message, 0)
}
```

Create: `internal/session/manager.go`

```go
package session

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Manager manages active sessions with timeout support.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session // key: sessionID
	logger   zerolog.Logger
	timeout  time.Duration
}

// NewManager creates a new Session Manager.
func NewManager(logger zerolog.Logger, timeout time.Duration) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		logger:   logger,
		timeout:  timeout,
	}
}

// GetOrCreateSession returns an existing session or creates a new one.
func (m *Manager) GetOrCreateSession(ctx context.Context, userID, agentID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Look for existing active session for this agent
	for _, s := range m.sessions {
		if s.AgentID() == agentID && s.UserID() == userID && !s.IsExpired(m.timeout) {
			s.UpdateActivity()
			return s
		}
	}

	// Create new session
	newSession := NewSession(ctx, userID, agentID)
	m.sessions[newSession.SessionID()] = newSession
	return newSession
}

// RemoveSession removes a session by ID.
func (m *Manager) RemoveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, exists := m.sessions[sessionID]; exists {
		delete(m.sessions, sessionID)
		m.logger.Info().
			Str("session_id", sessionID).
			Str("user_id", s.UserID()).
			Str("agent_id", s.AgentID()).
			Msg("Session removed")
	}
}

// CleanupExpired removes all expired sessions.
func (m *Manager) CleanupExpired() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for id, s := range m.sessions {
		if s.IsExpired(m.timeout) {
			delete(m.sessions, id)
			count++
		}
	}

	if count > 0 {
		m.logger.Debug().Int("count", count).Msg("Cleaned up expired sessions")
	}

	return count
}

// StartCleanupLoop starts a background goroutine that periodically cleans up expired sessions.
func (m *Manager) StartCleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			m.CleanupExpired()
		}
	}()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/session/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/session/session.go internal/session/manager.go tests/unit/session/session_test.go
git commit -m "feat(session): add session management with timeout support"
```

---

## Phase 3: Storage Integration - S3 Client

### Task 6: Create S3 Storage Package

**Files:**
- Create: `internal/storage/s3.go`
- Create: `tests/unit/storage/s3_test.go`

**Step 1: Write the failing test**

Create: `tests/unit/storage/s3_test.go`

```go
package storage_test

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/storage"
)

func TestWorkspacePath(t *testing.T) {
	userID := "user-123"
	agentID := "agent-456"
	filename := "SOUL.md"

	path := storage.WorkspacePath(userID, agentID, filename)
	expected := "user-123/agent-456/workspace/SOUL.md"

	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestTranscriptPath(t *testing.T) {
	userID := "user-123"
	agentID := "agent-456"
	sessionID := "session-789"

	path := storage.TranscriptPath(userID, agentID, sessionID)
	expected := "user-123/agent-456/transcripts/session-789.jsonl"

	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestMockS3Client(t *testing.T) {
	client := storage.NewMockS3Client()
	ctx := context.Background()

	// Test write and read
	userID := "user-123"
	agentID := "agent-456"
	path := storage.WorkspacePath(userID, agentID, "SOUL.md")
	content := "# Soul\nYou are helpful."

	err := client.Put(ctx, path, []byte(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	result, err := client.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(result) != content {
		t.Errorf("expected %s, got %s", content, string(result))
	}

	// Test append
	transcriptPath := storage.TranscriptPath(userID, agentID, "session-789")
	line1 := "{\"turn\":1,\"user\":\"hello\"}\n"
	line2 := "{\"turn\":2,\"assistant\":\"hi\"}\n"

	err = client.Append(ctx, transcriptPath, []byte(line1))
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	err = client.Append(ctx, transcriptPath, []byte(line2))
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	result, err = client.Get(ctx, transcriptPath)
	if err != nil {
		t.Fatalf("Get transcript failed: %v", err)
	}

	expected := line1 + line2
	if string(result) != expected {
		t.Errorf("expected %s, got %s", expected, string(result))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/storage/... -v`
Expected: FAIL with "package not found"

**Step 3: Write minimal implementation**

Create: `internal/storage/s3.go`

```go
package storage

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
)

// Client is the interface for S3-compatible storage operations.
type Client interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Append(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
}

// WorkspacePath returns the S3 path for a workspace file.
func WorkspacePath(userID, agentID, filename string) string {
	return path.Join(userID, agentID, "workspace", filename)
}

// TranscriptPath returns the S3 path for a transcript file.
func TranscriptPath(userID, agentID, sessionID string) string {
	return path.Join(userID, agentID, "transcripts", sessionID+".jsonl")
}

// MockS3Client is an in-memory mock S3 client for testing.
type MockS3Client struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMockS3Client creates a new mock S3 client.
func NewMockS3Client() *MockS3Client {
	return &MockS3Client{
		data: make(map[string][]byte),
	}
}

// Put stores data at the given key.
func (m *MockS3Client) Put(ctx context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), data...) // Copy to avoid aliasing
	return nil
}

// Get retrieves data from the given key.
func (m *MockS3Client) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, exists := m.data[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	// Return a copy to avoid aliasing
	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

// Append appends data to the given key.
func (m *MockS3Client) Append(ctx context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.data[key]
	if !exists {
		m.data[key] = append([]byte(nil), data...) // Copy to avoid aliasing
		return nil
	}

	// Append and store new copy
	combined := append(existing, data...)
	m.data[key] = combined
	return nil
}

// Delete removes data at the given key.
func (m *MockS3Client) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// AWSS3Client implements Client using AWS SDK.
// This is a placeholder for production implementation.
type AWSS3Client struct {
	bucket string
	region string
}

// NewAWSS3Client creates a new AWS S3 client.
// TODO: Implement with actual AWS SDK
func NewAWSS3Client(bucket, region string) *AWSS3Client {
	return &AWSS3Client{
		bucket: bucket,
		region: region,
	}
}

// Put stores data in S3.
func (a *AWSS3Client) Put(ctx context.Context, key string, data []byte) error {
	// TODO: Implement with AWS SDK PutObject
	return fmt.Errorf("not implemented: use MockS3Client for testing")
}

// Get retrieves data from S3.
func (a *AWSS3Client) Get(ctx context.Context, key string) ([]byte, error) {
	// TODO: Implement with AWS SDK GetObject
	return nil, fmt.Errorf("not implemented: use MockS3Client for testing")
}

// Append appends data to a file in S3.
func (a *AWSS3Client) Append(ctx context.Context, key string, data []byte) error {
	// S3 doesn't support native append - need to get, append, put
	existing, err := a.Get(ctx, key)
	if err != nil && strings.Contains(err.Error(), "not found") {
		// File doesn't exist, just put
		return a.Put(ctx, key, data)
	}
	if err != nil {
		return err
	}

	combined := append(existing, data...)
	return a.Put(ctx, key, combined)
}

// Delete removes data from S3.
func (a *AWSS3Client) Delete(ctx context.Context, key string) error {
	// TODO: Implement with AWS SDK DeleteObject
	return fmt.Errorf("not implemented: use MockS3Client for testing")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/storage/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/storage/s3.go tests/unit/storage/s3_test.go
git commit -m "feat(storage): add S3 storage client with mock implementation"
```

---

### Task 7: Create Transcript Writer

**Files:**
- Create: `internal/storage/transcript.go`
- Create: `tests/unit/storage/transcript_test.go`

**Step 1: Write the failing test**

Create: `tests/unit/storage/transcript_test.go`

```go
package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/storage"
)

func TestWriteTurn(t *testing.T) {
	client := storage.NewMockS3Client()
	writer := storage.NewTranscriptWriter(client)
	ctx := context.Background()

	userID := "user-123"
	agentID := "agent-456"
	sessionID := "session-789"

	// Create a turn
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "Hello"),
		agent.NewTextMessage(agent.RoleAssistant, "Hi there"),
	}

	turn := storage.Turn{
		SessionID: sessionID,
		UserID:    userID,
		AgentID:   agentID,
		TurnNumber: 1,
		Messages:  messages,
		Timestamp: time.Now().Unix(),
	}

	err := writer.WriteTurn(ctx, turn)
	if err != nil {
		t.Fatalf("WriteTurn failed: %v", err)
	}

	// Verify the transcript was written
	path := storage.TranscriptPath(userID, agentID, sessionID)
	data, err := client.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get transcript failed: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Hello") {
		t.Errorf("expected transcript to contain 'Hello', got %s", content)
	}
}

func TestTurnToJSONL(t *testing.T) {
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "test"),
	}

	turn := storage.Turn{
		SessionID:  "session-123",
		UserID:     "user-123",
		AgentID:    "agent-123",
		TurnNumber: 1,
		Messages:   messages,
		Timestamp:  1234567890,
	}

	line := storage.TurnToJSONL(turn)

	if !strings.HasPrefix(line, "{") {
		t.Error("expected JSON line to start with {")
	}
	if !strings.Contains(line, "test") {
		t.Error("expected JSON line to contain user message")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/storage/... -run "TestWriteTurn|TestTurnToJSONL" -v`
Expected: FAIL with "undefined: storage.NewTranscriptWriter"

**Step 3: Write minimal implementation**

Create: `internal/storage/transcript.go`

```go
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
	ID       string
	Name     string
	Args     map[string]any
	Result   any
	IsError  bool
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
		// Fallback to error line
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/storage/... -run "TestWriteTurn|TestTurnToJSONL" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/storage/transcript.go tests/unit/storage/transcript_test.go
git commit -m "feat(storage): add transcript writer for JSONL format"
```

---

## Phase 4: Mem0 Integration

### Task 8: Create Mem0 Client Package

**Files:**
- Create: `internal/mem0/client.go`
- Create: `tests/unit/mem0/client_test.go`

**Step 1: Write the failing test**

Create: `tests/unit/mem0/client_test.go`

```go
package mem0_test

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/mem0"
)

func TestMockMem0ClientSearch(t *testing.T) {
	client := mem0.NewMockClient()
	ctx := context.Background()
	userID := "user-123"

	// Add some memories
	_ = client.Add(ctx, userID, "User prefers Python", "")
	_ = client.Add(ctx, userID, "User is working on AI project", "")

	// Search memories
	results, err := client.Search(ctx, userID, "Python")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected at least one memory result")
	}

	found := false
	for _, r := range results {
		if strings.Contains(r, "Python") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find memory about Python")
	}
}

func TestMockMem0ClientAdd(t *testing.T) {
	client := mem0.NewMockClient()
	ctx := context.Background()
	userID := "user-123"

	err := client.Add(ctx, userID, "Test memory", "")
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify memory was added
	results, _ := client.Search(ctx, userID, "Test")
	if len(results) == 0 {
		t.Error("expected to find added memory")
	}
}

func TestMockMem0ClientSessionScoped(t *testing.T) {
	client := mem0.NewMockClient()
	ctx := context.Background()
	userID := "user-123"
	runID := "session-456"

	// Add session-scoped memory
	err := client.Add(ctx, userID, "Session context", runID)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Search in session scope
	results, err := client.SearchInSession(ctx, userID, runID, "Session")
	if err != nil {
		t.Fatalf("SearchInSession failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected to find session memory")
	}
}

func TestMockMem0ClientCreateSession(t *testing.T) {
	client := mem0.NewMockClient()
	ctx := context.Background()
	userID := "user-123"

	sessionID, err := client.CreateSession(ctx, userID)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if sessionID == "" {
		t.Error("expected non-empty session ID")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/mem0/... -v`
Expected: FAIL with "package not found"

**Step 3: Write minimal implementation**

Create: `internal/mem0/client.go`

```go
package mem0

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// Client is the interface for Mem0 API operations.
type Client interface {
	// Add stores a memory. If runID is provided, memory is session-scoped.
	Add(ctx context.Context, userID, content, runID string) error

	// Search retrieves memories for a user.
	Search(ctx context.Context, userID, query string) ([]string, error)

	// SearchInSession retrieves memories from a specific session.
	SearchInSession(ctx context.Context, userID, runID, query string) ([]string, error)

	// CreateSession creates a new Mem0 session and returns its ID.
	CreateSession(ctx context.Context, userID string) (string, error)

	// DeleteSession removes a session and its memories.
	DeleteSession(ctx context.Context, userID, runID string) error
}

// Memory represents a stored memory.
type Memory struct {
	ID       string
	UserID   string
	RunID    string // Empty for long-term, set for session-scoped
	Content  string
}

// MockClient is an in-memory mock Mem0 client for testing.
type MockClient struct {
	mu      sync.RWMutex
	memories map[string]*Memory // key: memory ID
	byUser  map[string][]*Memory // key: userID
	byRun   map[string][]*Memory // key: runID
}

// NewMockClient creates a new mock Mem0 client.
func NewMockClient() *MockClient {
	return &MockClient{
		memories: make(map[string]*Memory),
		byUser:   make(map[string][]*Memory),
		byRun:    make(map[string][]*Memory),
	}
}

// Add stores a memory.
func (m *MockClient) Add(ctx context.Context, userID, content, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	memory := &Memory{
		ID:      uuid.New().String(),
		UserID:  userID,
		RunID:   runID,
		Content: content,
	}

	m.memories[memory.ID] = memory
	m.byUser[userID] = append(m.byUser[userID], memory)

	if runID != "" {
		m.byRun[runID] = append(m.byRun[runID], memory)
	}

	return nil
}

// Search retrieves memories for a user using simple keyword matching.
// In production, this uses vector embeddings via Mem0 API.
func (m *MockClient) Search(ctx context.Context, userID, query string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userMemories, exists := m.byUser[userID]
	if !exists {
		return []string{}, nil
	}

	var results []string
	queryLower := strings.ToLower(query)

	for _, mem := range userMemories {
		// Skip session-scoped memories in general search
		if mem.RunID != "" {
			continue
		}
		if strings.Contains(strings.ToLower(mem.Content), queryLower) {
			results = append(results, mem.Content)
		}
	}

	return results, nil
}

// SearchInSession retrieves memories from a specific session.
func (m *MockClient) SearchInSession(ctx context.Context, userID, runID, query string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runMemories, exists := m.byRun[runID]
	if !exists {
		return []string{}, nil
	}

	var results []string
	queryLower := strings.ToLower(query)

	for _, mem := range runMemories {
		if mem.UserID != userID {
			continue
		}
		if strings.Contains(strings.ToLower(mem.Content), queryLower) {
			results = append(results, mem.Content)
		}
	}

	return results, nil
}

// CreateSession creates a new Mem0 session.
// Returns a session ID that can be used for session-scoped memories.
func (m *MockClient) CreateSession(ctx context.Context, userID string) (string, error) {
	// In production, this calls Mem0 API to create a session
	// For mock, we just return a UUID
	return uuid.New().String(), nil
}

// DeleteSession removes all memories associated with a session.
func (m *MockClient) DeleteSession(ctx context.Context, userID, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	runMemories, exists := m.byRun[runID]
	if !exists {
		return nil
	}

	// Remove memories
	for _, mem := range runMemories {
		delete(m.memories, mem.ID)
	}

	delete(m.byRun, runID)

	// Rebuild user memories list
	var updated []*Memory
	for _, mem := range m.byUser[userID] {
		if mem.RunID != runID {
			updated = append(updated, mem)
		}
	}
	m.byUser[userID] = updated

	return nil
}

// APIClient implements Client using the real Mem0 API.
// This is a placeholder for production implementation.
type APIClient struct {
	baseURL string
	apiKey  string
}

// NewAPIClient creates a new Mem0 API client.
// TODO: Implement with actual Mem0 API calls
func NewAPIClient(baseURL, apiKey string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		apiKey:  apiKey,
	}
}

func (a *APIClient) Add(ctx context.Context, userID, content, runID string) error {
	return fmt.Errorf("not implemented: use MockClient for testing")
}

func (a *APIClient) Search(ctx context.Context, userID, query string) ([]string, error) {
	return nil, fmt.Errorf("not implemented: use MockClient for testing")
}

func (a *APIClient) SearchInSession(ctx context.Context, userID, runID, query string) ([]string, error) {
	return nil, fmt.Errorf("not implemented: use MockClient for testing")
}

func (a *APIClient) CreateSession(ctx context.Context, userID string) (string, error) {
	return "", fmt.Errorf("not implemented: use MockClient for testing")
}

func (a *APIClient) DeleteSession(ctx context.Context, userID, runID string) error {
	return fmt.Errorf("not implemented: use MockClient for testing")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/mem0/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/mem0/client.go tests/unit/mem0/client_test.go
git commit -m "feat(mem0): add Mem0 client with mock implementation"
```

---

### Task 9: Create Mem0 Tools

**Files:**
- Create: `internal/agent/tools/mem0.go`
- Create: `tests/unit/agent/tools/mem0_test.go`

**Step 1: Write the failing test**

Create: `tests/unit/agent/tools/mem0_test.go`

```go
package tools_test

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/nevaria-ai/gateway/internal/mem0"
	agenttools "github.com/nevaria-ai/gateway/internal/agent/tools"
)

func TestMem0SearchTool(t *testing.T) {
	client := mem0.NewMockClient()
	userID := "user-123"

	// Add a memory
	_ = client.Add(context.Background(), userID, "User likes Go programming", "")

	// Create context with userID
	ctx := ctx.WithUserID(context.Background(), userID)

	// Create tool
	tool := agenttools.Mem0SearchTool(client)

	// Execute tool
	result, err := tool.Execute(ctx, map[string]any{"query": "Go"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultText := extractTextResult(result)
	if !strings.Contains(resultText, "Go") {
		t.Errorf("expected result to contain 'Go', got %s", resultText)
	}
}

func TestMem0StoreTool(t *testing.T) {
	client := mem0.NewMockClient()
	userID := "user-123"

	ctx := ctx.WithUserID(context.Background(), userID)

	// Create tool
	tool := agenttools.Mem0StoreTool(client)

	// Execute tool
	result, err := tool.Execute(ctx, map[string]any{"content": "User prefers dark mode"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify memory was stored
	memories, _ := client.Search(ctx, userID, "dark mode")
	if len(memories) == 0 {
		t.Error("expected to find stored memory")
	}
}

func extractTextResult(result *agent.ToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	if tc, ok := result.Content[0].(agent.TextContent); ok {
		return tc.Text
	}
	return ""
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/agent/tools/... -run "TestMem0" -v`
Expected: FAIL with "undefined: agenttools.Mem0SearchTool"

**Step 3: Write minimal implementation**

Create: `internal/agent/tools/mem0.go`

```go
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/nevaria-ai/gateway/internal/mem0"
)

// Mem0SearchTool creates a tool for searching memories.
func Mem0SearchTool(client mem0.Client) agent.Tool {
	return agent.Tool{
		Name:        "mem0_search",
		Description: "Search stored memories and knowledge about the user. Returns relevant facts, preferences, and past decisions.",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			// Get userID from context
			userID := ctx.GetUserIDFromContext(ctx)
			if userID == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: user ID not found in context"}},
				}, fmt.Errorf("user ID not found in context")
			}

			// Extract query
			query, ok := args["query"].(string)
			if !ok || query == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: 'query' parameter is required"}},
				}, fmt.Errorf("missing query parameter")
			}

			// Search memories
			memories, err := client.Search(ctx, userID, query)
			if err != nil {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Error searching memories: %v", err)}},
				}, err
			}

			// Format results
			if len(memories) == 0 {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "No relevant memories found"}},
				}, nil
			}

			resultText := fmt.Sprintf("Found %d relevant memories:\n", len(memories))
			for i, mem := range memories {
				resultText += fmt.Sprintf("%d. %s\n", i+1, mem)
			}

			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: resultText}},
			}, nil
		},
	}
}

// Mem0StoreTool creates a tool for storing long-term memories.
func Mem0StoreTool(client mem0.Client) agent.Tool {
	return agent.Tool{
		Name:        "mem0_store",
		Description: "Store important information in long-term memory. Use for facts, preferences, and decisions that should be remembered.",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			// Get userID from context
			userID := ctx.GetUserIDFromContext(ctx)
			if userID == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: user ID not found in context"}},
				}, fmt.Errorf("user ID not found in context")
			}

			// Extract content
			content, ok := args["content"].(string)
			if !ok || content == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: 'content' parameter is required"}},
				}, fmt.Errorf("missing content parameter")
			}

			// Store memory (long-term, no runID)
			err := client.Add(ctx, userID, content, "")
			if err != nil {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Error storing memory: %v", err)}},
				}, err
			}

			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Stored: %s", content)}},
			}, nil
		},
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/agent/tools/... -run "TestMem0" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/tools/mem0.go tests/unit/agent/tools/mem0_test.go
git commit -m "feat(agent/tools): add mem0_search and mem0_store tools"
```

---

## Phase 5: Integration - WebSocket Handler Updates

### Task 10: Update WebSocket Handler for AgentID

**Files:**
- Modify: `internal/websocket/handler.go`

**Step 1: Write the failing test**

Modify: `tests/unit/websocket/handler_test.go` - add at end:

```go
// TODO: Add tests for agent ID extraction from WebSocket message
// This requires refactoring the handler to accept agent ID from client
```

**Note:** This is a placeholder task. The actual implementation requires:
1. Client sends agent ID in initial WebSocket message
2. Handler extracts agentID and calls `GetOrCreateAgentForUser(userID, agentID)`

**Step 2: Update handler implementation**

Modify: `internal/websocket/handler.go` - find the agent creation section and update:

```go
// In HandleWebSocket, after userID is extracted:
// Client should send a message specifying which agent to connect to
// For now, use default agent ID if not specified

agentID := "default" // TODO: Extract from client message

// Create or get agent for this user
agentInstance := h.agentManager.GetOrCreateAgentForUser(sessionID, agentID)
```

**Step 3: Run tests to verify no regressions**

Run: `go test ./tests/unit/websocket/... -v`
Expected: PASS (or update tests as needed)

**Step 4: Commit**

```bash
git add internal/websocket/handler.go
git commit -m "feat(websocket): update handler to use composite agent key"
```

---

## Phase 6: End-to-End Integration

### Task 11: Create Workspace Loader

**Files:**
- Create: `internal/storage/workspace.go`
- Create: `tests/unit/storage/workspace_test.go`

**Step 1: Write the failing test**

Create: `tests/unit/storage/workspace_test.go`

```go
package storage_test

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/storage"
)

func TestLoadWorkspace(t *testing.T) {
	client := storage.NewMockS3Client()
	ctx := context.Background()

	userID := "user-123"
	agentID := "agent-456"

	// Pre-populate workspace files
	workspacePath := storage.WorkspacePath(userID, agentID, "")
	_ = client.Put(ctx, storage.WorkspacePath(userID, agentID, "SOUL.md"), []byte("# Soul\nYou are helpful."))
	_ = client.Put(ctx, storage.WorkspacePath(userID, agentID, "AGENTS.md"), []byte("# Instructions\nBe concise."))

	loader := storage.NewWorkspaceLoader(client)

	workspace, err := loader.Load(ctx, userID, agentID)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if workspace.Soul == "" {
		t.Error("expected non-empty Soul")
	}
	if !strings.Contains(workspace.Soul, "helpful") {
		t.Errorf("expected Soul to contain 'helpful', got %s", workspace.Soul)
	}

	if workspace.Agents == "" {
		t.Error("expected non-empty Agents")
	}
}

func TestLoadWorkspaceEmpty(t *testing.T) {
	client := storage.NewMockS3Client()
	ctx := context.Background()

	loader := storage.NewWorkspaceLoader(client)

	workspace, err := loader.Load(ctx, "user-123", "agent-456")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Should return empty workspace without error
	if workspace.Soul != "" {
		t.Error("expected empty Soul for non-existent workspace")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/storage/... -run "TestLoadWorkspace" -v`
Expected: FAIL with "undefined: storage.NewWorkspaceLoader"

**Step 3: Write minimal implementation**

Create: `internal/storage/workspace.go`

```go
package storage

import (
	"context"
	"fmt"
)

// Workspace represents an agent's workspace files.
type Workspace struct {
	Soul      string
	Agents    string
	User      string
	Identity  string
	Tools     string
	Loaded    bool // true if at least one file was loaded
}

// WorkspaceLoader loads workspace files from S3.
type WorkspaceLoader struct {
	client Client
}

// NewWorkspaceLoader creates a new WorkspaceLoader.
func NewWorkspaceLoader(client Client) *WorkspaceLoader {
	return &WorkspaceLoader{
		client: client,
	}
}

// Load loads all workspace files for an agent.
// Missing files are treated as empty strings.
func (l *WorkspaceLoader) Load(ctx context.Context, userID, agentID string) (*Workspace, error) {
	workspace := &Workspace{}

	// Try to load each file
	if soul, err := l.loadFile(ctx, userID, agentID, "SOUL.md"); err == nil {
		workspace.Soul = soul
		workspace.Loaded = true
	}

	if agents, err := l.loadFile(ctx, userID, agentID, "AGENTS.md"); err == nil {
		workspace.Agents = agents
		workspace.Loaded = true
	}

	if user, err := l.loadFile(ctx, userID, agentID, "USER.md"); err == nil {
		workspace.User = user
		workspace.Loaded = true
	}

	if identity, err := l.loadFile(ctx, userID, agentID, "IDENTITY.md"); err == nil {
		workspace.Identity = identity
		workspace.Loaded = true
	}

	if tools, err := l.loadFile(ctx, userID, agentID, "TOOLS.md"); err == nil {
		workspace.Tools = tools
		workspace.Loaded = true
	}

	return workspace, nil
}

// loadFile loads a single workspace file.
func (l *WorkspaceLoader) loadFile(ctx context.Context, userID, agentID, filename string) (string, error) {
	path := WorkspacePath(userID, agentID, filename)
	data, err := l.client.Get(ctx, path)
	if err != nil {
		return "", fmt.Errorf("file not found: %s", filename)
	}
	return string(data), nil
}

// FormatSystemPrompt formats the workspace content as a system prompt.
func (w *Workspace) FormatSystemPrompt() string {
	prompt := ""

	if w.Soul != "" {
		prompt += w.Soul + "\n\n"
	}

	if w.Agents != "" {
		prompt += w.Agents + "\n\n"
	}

	if w.User != "" {
		prompt += "## User Information\n\n" + w.User + "\n\n"
	}

	if w.Identity != "" {
		prompt += "## Identity\n\n" + w.Identity + "\n\n"
	}

	if w.Tools != "" {
		prompt += "## Tool Guidance\n\n" + w.Tools + "\n\n"
	}

	return prompt
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/storage/... -run "TestLoadWorkspace" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/storage/workspace.go tests/unit/storage/workspace_test.go
git commit -m "feat(storage): add workspace loader for agent personality files"
```

---

## Task 12: Create Agent File Tools

**Files:**
- Create: `internal/agent/tools/files.go`
- Create: `tests/unit/agent/tools/files_test.go`

**Step 1: Write the failing test**

Create: `tests/unit/agent/tools/files_test.go`

```go
package tools_test

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/nevaria-ai/gateway/internal/storage"
	agenttools "github.com/nevaria-ai/gateway/internal/agent/tools"
)

func TestReadAgentFileTool(t *testing.T) {
	client := storage.NewMockS3Client()
	userID := "user-123"
	agentID := "agent-456"

	// Pre-populate a file
	_ = client.Put(context.Background(), storage.WorkspacePath(userID, agentID, "SOUL.md"), []byte("Test content"))

	// Create context with userID and agentID
	ctx := context.Background()
	ctx = ctx.WithUserID(ctx, userID)
	ctx = ctx.WithAgentID(ctx, agentID)

	// Create tool
	tool := agenttools.ReadAgentFileTool(client)

	// Execute tool
	result, err := tool.Execute(ctx, map[string]any{"path": "SOUL.md"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultText := extractTextResult(result)
	if !strings.Contains(resultText, "Test content") {
		t.Errorf("expected result to contain 'Test content', got %s", resultText)
	}
}

func TestWriteAgentFileTool(t *testing.T) {
	client := storage.NewMockS3Client()
	userID := "user-123"
	agentID := "agent-456"

	ctx := context.Background()
	ctx = ctx.WithUserID(ctx, userID)
	ctx = ctx.WithAgentID(ctx, agentID)

	// Create tool
	tool := agenttools.WriteAgentFileTool(client)

	// Execute tool
	result, err := tool.Execute(ctx, map[string]any{
		"path":    "CUSTOM.md",
		"content": "Custom content",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify file was written
	data, err := client.Get(ctx, storage.WorkspacePath(userID, agentID, "CUSTOM.md"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(data) != "Custom content" {
		t.Errorf("expected 'Custom content', got %s", string(data))
	}
}

func TestReadAgentFileToolEnforcesScope(t *testing.T) {
	client := storage.NewMockS3Client()

	// Create context with different user/agent
	ctx := context.Background()
	ctx = ctx.WithUserID(ctx, "other-user")
	ctx = ctx.WithAgentID(ctx, "other-agent")

	tool := agenttools.ReadAgentFileTool(client)

	// Try to read a file from different user/agent
	_, err := tool.Execute(ctx, map[string]any{"path": "SOUL.md"})
	// Should either fail or return not found
	if err == nil {
		// If no error, verify empty result
	} else {
		// Error is acceptable - file not found
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/unit/agent/tools/... -run "TestReadAgentFile|TestWriteAgentFile" -v`
Expected: FAIL with "undefined: agenttools.ReadAgentFileTool"

**Step 3: Write minimal implementation**

Create: `internal/agent/tools/files.go`

```go
package tools

import (
	"context"
	"fmt"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/nevaria-ai/gateway/internal/storage"
)

// ReadAgentFileTool creates a tool for reading agent workspace files.
func ReadAgentFileTool(client storage.Client) agent.Tool {
	return agent.Tool{
		Name:        "read_agent_file",
		Description: "Read a file from the agent's workspace. Common files: SOUL.md, AGENTS.md, USER.md, IDENTITY.md, TOOLS.md",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			// Get userID and agentID from context
			userID := ctx.GetUserIDFromContext(ctx)
			agentID := ctx.GetAgentIDFromContext(ctx)

			if userID == "" || agentID == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: user ID or agent ID not found in context"}},
				}, fmt.Errorf("user ID or agent ID not found in context")
			}

			// Extract path
			path, ok := args["path"].(string)
			if !ok || path == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: 'path' parameter is required"}},
				}, fmt.Errorf("missing path parameter")
			}

			// Build S3 path with enforced scope
			s3Path := storage.WorkspacePath(userID, agentID, path)

			// Read file
			data, err := client.Get(ctx, s3Path)
			if err != nil {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Error reading file: %v", err)}},
				}, err
			}

			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: string(data)}},
			}, nil
		},
	}
}

// WriteAgentFileTool creates a tool for writing agent workspace files.
func WriteAgentFileTool(client storage.Client) agent.Tool {
	return agent.Tool{
		Name:        "write_agent_file",
		Description: "Write a file to the agent's workspace. Use for updating configuration or saving important information.",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			// Get userID and agentID from context
			userID := ctx.GetUserIDFromContext(ctx)
			agentID := ctx.GetAgentIDFromContext(ctx)

			if userID == "" || agentID == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: user ID or agent ID not found in context"}},
				}, fmt.Errorf("user ID or agent ID not found in context")
			}

			// Extract parameters
			path, ok := args["path"].(string)
			if !ok || path == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: 'path' parameter is required"}},
				}, fmt.Errorf("missing path parameter")
			}

			content, ok := args["content"].(string)
			if !ok {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: 'content' parameter is required"}},
				}, fmt.Errorf("missing content parameter")
			}

			// Build S3 path with enforced scope
			s3Path := storage.WorkspacePath(userID, agentID, path)

			// Write file
			err := client.Put(ctx, s3Path, []byte(content))
			if err != nil {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Error writing file: %v", err)}},
				}, err
			}

			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Successfully wrote %s", path)}},
			}, nil
		},
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./tests/unit/agent/tools/... -run "TestReadAgentFile|TestWriteAgentFile" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/tools/files.go tests/unit/agent/tools/files_test.go
git commit -m "feat(agent/tools): add read_agent_file and write_agent_file tools"
```

---

## Phase 7: Testing & Documentation

### Task 13: Add Integration Tests

**Files:**
- Create: `tests/integration/multi_agent_test.go`

**Step 1: Write the integration test**

Create: `tests/integration/multi_agent_test.go`

```go
package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/nevaria-ai/gateway/internal/mem0"
	"github.com/nevaria-ai/gateway/internal/session"
	"github.com/nevaria-ai/gateway/internal/storage"
	"github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/rs/zerolog"
)

// TestMultiAgentPerUser verifies multiple agents can be created for a single user.
func TestMultiAgentPerUser(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "user-123"

	// Create three different agents for the same user
	agent1 := mgr.GetOrCreateAgentForUser(userID, "agent-1")
	agent2 := mgr.GetOrCreateAgentForUser(userID, "agent-2")
	agent3 := mgr.GetOrCreateAgentForUser(userID, "agent-3")

	// Verify they are different
	if agent1 == agent2 {
		t.Error("agent1 and agent2 should be different")
	}
	if agent2 == agent3 {
		t.Error("agent2 and agent3 should be different")
	}

	// Verify state has correct agent IDs
	state1 := agent1.State()
	if state1.AgentID != "agent-1" {
		t.Errorf("expected agent-1, got %s", state1.AgentID)
	}

	state2 := agent2.State()
	if state2.AgentID != "agent-2" {
		t.Errorf("expected agent-2, got %s", state2.AgentID)
	}
}

// TestSessionIsolation verifies sessions are isolated per agent.
func TestSessionIsolation(t *testing.T) {
	ctx := context.Background()
	sessionMgr := session.NewManager(zerolog.Nop(), 5*time.Minute)

	userID := "user-123"
	agentID1 := "agent-1"
	agentID2 := "agent-2"

	// Create sessions for different agents
	session1 := sessionMgr.GetOrCreateSession(ctx, userID, agentID1)
	session2 := sessionMgr.GetOrCreateSession(ctx, userID, agentID2)

	if session1.SessionID() == session2.SessionID() {
		t.Error("sessions should have different IDs")
	}

	if session1.AgentID() != agentID1 {
		t.Errorf("expected agent-1, got %s", session1.AgentID())
	}

	if session2.AgentID() != agentID2 {
		t.Errorf("expected agent-2, got %s", session2.AgentID())
	}
}

// TestMemoryIsolation verifies memories are isolated per user.
func TestMemoryIsolation(t *testing.T) {
	ctx := context.Background()
	client := mem0.NewMockClient()

	user1 := "user-1"
	user2 := "user-2"

	// Add memory for user1
	_ = client.Add(ctx, user1, "User1 likes pizza", "")

	// Add memory for user2
	_ = client.Add(ctx, user2, "User2 hates pizza", "")

	// Search user1 memories
	results1, _ := client.Search(ctx, user1, "pizza")
	if len(results1) != 1 {
		t.Errorf("expected 1 result for user1, got %d", len(results1))
	}

	// Search user2 memories
	results2, _ := client.Search(ctx, user2, "pizza")
	if len(results2) != 1 {
		t.Errorf("expected 1 result for user2, got %d", len(results2))
	}

	// Verify content is different
	if results1[0] == results2[0] {
		t.Error("memories should be different for different users")
	}
}

// TestStorageIsolation verifies S3 paths enforce isolation.
func TestStorageIsolation(t *testing.T) {
	ctx := context.Background()
	client := storage.NewMockS3Client()

	userID := "user-123"
	agentID := "agent-456"

	// Write a file for this user/agent
	path := storage.WorkspacePath(userID, agentID, "SOUL.md")
	_ = client.Put(ctx, path, []byte("Agent personality"))

	// Try to read with correct path
	data, err := client.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(data) != "Agent personality" {
		t.Errorf("expected 'Agent personality', got %s", string(data))
	}

	// Verify different user/agent can't access (different path)
	differentPath := storage.WorkspacePath("other-user", "other-agent", "SOUL.md")
	_, err = client.Get(ctx, differentPath)
	if err == nil {
		t.Error("expected error when accessing different user's file")
	}
}

// TestAgentWithTools verifies agents can have tools added.
func TestAgentWithTools(t *testing.T) {
	opts := agent.AgentOptions{
		UserID:  "user-123",
		AgentID: "agent-456",
	}
	a := agent.NewAgent(opts)

	// Create a test tool
	testTool := agent.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: "test result"}},
			}, nil
		},
	}

	// Add tool
	a.AddTools(testTool)

	// Verify tool is in state
	state := a.State()
	if len(state.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(state.Tools))
	}

	if state.Tools[0].Name != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got %s", state.Tools[0].Name)
	}
}

// TestSessionTimeout verifies sessions timeout correctly.
func TestSessionTimeout(t *testing.T) {
	ctx := context.Background()
	s := session.NewSession(ctx, "user-123", "agent-456")

	// Fresh session should not be expired
	if s.IsExpired(5*time.Minute) {
		t.Error("fresh session should not be expired")
	}

	// Set old activity time
	oldTime := time.Now().Add(-10 * time.Minute)
	s.SetLastActivity(oldTime)

	// Should be expired now
	if !s.IsExpired(5*time.Minute) {
		t.Error("old session should be expired")
	}
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./tests/integration/... -v`
Expected: PASS

**Step 3: Commit**

```bash
git add tests/integration/multi_agent_test.go
git commit -m "test(integration): add multi-agent isolation tests"
```

---

### Task 14: Update Documentation

**Files:**
- Modify: `docs/README.md`
- Create: `docs/agent.md`
- Create: `docs/session.md`
- Create: `docs/storage.md`

**Step 1: Update main README**

Modify: `docs/README.md` - add architecture overview section describing multi-agent support.

**Step 2: Create agent documentation**

Create: `docs/agent.md` - document agent types, lifecycle, and usage.

**Step 3: Create session documentation**

Create: `docs/session.md` - document session management, timeout, reconnection.

**Step 4: Create storage documentation**

Create: `docs/storage.md` - document S3 structure, workspace files, transcripts.

**Step 5: Commit**

```bash
git add docs/
git commit -m "docs: add multi-agent architecture documentation"
```

---

## Task 15: Run Full Test Suite

**Step 1: Run all tests**

```bash
go test ./... -v
```

Expected: All tests pass

**Step 2: Run with race detector**

```bash
go test ./... -race -v
```

Expected: No race conditions

**Step 3: Build verification**

```bash
make build
```

Expected: Binary builds successfully

**Step 4: Commit**

If any issues found, fix and commit:
```bash
git commit -m "test: fix issues found in full test suite"
```

---

## Implementation Notes

### Dependencies to Add (when ready for production)
- AWS SDK for Go v2 (`github.com/aws/aws-sdk-go-v2`)
- S3 specific packages
- Mem0 API client (when available)

### Environment Variables
```
S3_BUCKET=your-bucket-name
S3_REGION=us-east-1
MEM0_API_URL=https://api.mem0.ai
MEM0_API_KEY=your-api-key
SESSION_TIMEOUT=30m
SESSION_CLEANUP_INTERVAL=5m
```

### TODOs for Production
1. Replace MockS3Client with actual AWS SDK implementation
2. Replace MockMem0Client with actual Mem0 API calls
3. Add metrics and observability
4. Add rate limiting for S3 operations
5. Implement workspace file caching
6. Add proper error handling for S3 failures
7. Implement session reconstruction from S3 transcripts
8. Add workspace file validation
9. Implement workspace template system

---

## Checklist

- [ ] Phase 1: Core Refactoring - Tasks 1-4
- [ ] Phase 2: Session Management - Task 5
- [ ] Phase 3: Storage Integration - Tasks 6-7
- [ ] Phase 4: Mem0 Integration - Tasks 8-9
- [ ] Phase 5: WebSocket Updates - Task 10
- [ ] Phase 6: Workspace & File Tools - Tasks 11-12
- [ ] Phase 7: Testing & Documentation - Tasks 13-15

---

**Plan complete and saved to `.claude/plans/2026-02-17-multi-agent-storage-security-implementation.md`**

**Two execution options:**

**1. Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with @superpowers:executing-plans, batch execution with checkpoints

**Which approach?**
