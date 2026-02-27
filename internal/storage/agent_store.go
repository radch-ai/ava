package storage

import (
	"context"
	"fmt"
	"sync"
)

type AgentConfig struct {
	AgentID     string
	UserID      string
	Name        string
	SoulMd      string
	AgentsMd    string
	UserMd      string
	IdentityMd  string
	ToolsMd     string
	HeartbeatMd string
	Config      map[string]any
}

type AgentStore interface {
	Get(ctx context.Context, userID, agentID string) (*AgentConfig, error)
	Upsert(ctx context.Context, config *AgentConfig) error
	Delete(ctx context.Context, userID, agentID string) error
	ListByUser(ctx context.Context, userID string) ([]*AgentConfig, error)
}

type MockAgentStore struct {
	mu      sync.RWMutex
	configs map[string]*AgentConfig
}

func NewMockAgentStore() *MockAgentStore {
	return &MockAgentStore{
		configs: make(map[string]*AgentConfig),
	}
}

func (m *MockAgentStore) key(userID, agentID string) string {
	return fmt.Sprintf("%s:%s", userID, agentID)
}

func (m *MockAgentStore) Get(ctx context.Context, userID, agentID string) (*AgentConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	config, exists := m.configs[m.key(userID, agentID)]
	if !exists {
		return nil, fmt.Errorf("agent not found: %s/%s", userID, agentID)
	}

	return config, nil
}

func (m *MockAgentStore) Upsert(ctx context.Context, config *AgentConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.configs[m.key(config.UserID, config.AgentID)] = config
	return nil
}

func (m *MockAgentStore) Delete(ctx context.Context, userID, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.key(userID, agentID)
	if _, exists := m.configs[key]; !exists {
		return fmt.Errorf("agent not found: %s/%s", userID, agentID)
	}

	delete(m.configs, key)
	return nil
}

func (m *MockAgentStore) ListByUser(ctx context.Context, userID string) ([]*AgentConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*AgentConfig
	for key, config := range m.configs {
		if len(key) > len(userID)+1 && key[:len(userID)+1] == userID+":" {
			result = append(result, config)
		}
	}

	return result, nil
}
