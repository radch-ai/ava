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
}

// NewSession creates a new Session with a generated session ID.
func NewSession(ctx context.Context, userID, agentID string) *Session {
	now := time.Now()
	return &Session{
		sessionID:     uuid.New().String(),
		userID:        userID,
		agentID:       agentID,
		mem0SessionID: uuid.New().String(),
		messages:      make([]Message, 0),
		startedAt:     now,
		lastActivity:  now,
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
