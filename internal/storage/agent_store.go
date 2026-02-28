package storage

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

type PostgresAgentStore struct {
	pool *pgxpool.Pool
}

func NewPostgresAgentStore(pool *pgxpool.Pool) *PostgresAgentStore {
	return &PostgresAgentStore{pool: pool}
}

func (s *PostgresAgentStore) Get(ctx context.Context, userID, agentID string) (*AgentConfig, error) {
	query := `
		SELECT agent_id, user_id, name, soul_md, agents_md, user_md, identity_md, tools_md, heartbeat_md, config
		FROM agents
		WHERE user_id = $1 AND agent_id = $2
	`

	var config AgentConfig
	err := s.pool.QueryRow(ctx, query, userID, agentID).Scan(
		&config.AgentID,
		&config.UserID,
		&config.Name,
		&config.SoulMd,
		&config.AgentsMd,
		&config.UserMd,
		&config.IdentityMd,
		&config.ToolsMd,
		&config.HeartbeatMd,
		&config.Config,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("agent not found: %s/%s: %w", userID, agentID, err)
		}
		return nil, fmt.Errorf("failed to get agent %s/%s: %w", userID, agentID, err)
	}

	return &config, nil
}

func (s *PostgresAgentStore) Upsert(ctx context.Context, config *AgentConfig) error {
	query := `
		INSERT INTO agents (agent_id, user_id, name, soul_md, agents_md, user_md, identity_md, tools_md, heartbeat_md, config)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (agent_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			name = EXCLUDED.name,
			soul_md = EXCLUDED.soul_md,
			agents_md = EXCLUDED.agents_md,
			user_md = EXCLUDED.user_md,
			identity_md = EXCLUDED.identity_md,
			tools_md = EXCLUDED.tools_md,
			heartbeat_md = EXCLUDED.heartbeat_md,
			config = EXCLUDED.config,
			updated_at = NOW()
	`

	_, err := s.pool.Exec(ctx, query,
		config.AgentID,
		config.UserID,
		config.Name,
		config.SoulMd,
		config.AgentsMd,
		config.UserMd,
		config.IdentityMd,
		config.ToolsMd,
		config.HeartbeatMd,
		config.Config,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert agent %s: %w", config.AgentID, err)
	}

	return nil
}

func (s *PostgresAgentStore) Delete(ctx context.Context, userID, agentID string) error {
	query := `DELETE FROM agents WHERE user_id = $1 AND agent_id = $2`

	result, err := s.pool.Exec(ctx, query, userID, agentID)
	if err != nil {
		return fmt.Errorf("failed to delete agent %s/%s: %w", userID, agentID, err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("agent not found: %s/%s", userID, agentID)
	}

	return nil
}

func (s *PostgresAgentStore) ListByUser(ctx context.Context, userID string) ([]*AgentConfig, error) {
	query := `
		SELECT agent_id, user_id, name, soul_md, agents_md, user_md, identity_md, tools_md, heartbeat_md, config
		FROM agents
		WHERE user_id = $1
	`

	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents for user %s: %w", userID, err)
	}
	defer rows.Close()

	var configs []*AgentConfig
	for rows.Next() {
		var config AgentConfig
		err := rows.Scan(
			&config.AgentID,
			&config.UserID,
			&config.Name,
			&config.SoulMd,
			&config.AgentsMd,
			&config.UserMd,
			&config.IdentityMd,
			&config.ToolsMd,
			&config.HeartbeatMd,
			&config.Config,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		configs = append(configs, &config)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agents: %w", err)
	}

	return configs, nil
}
