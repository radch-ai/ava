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
	sessions map[string]*Session
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
