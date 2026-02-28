package agent

import (
	"sync"
)

type AgentConfig struct {
	AgentID      string
	UserID       string
	SystemPrompt string
	Model        Model
}

type AgentDB interface {
	GetAgentConfig(userID, agentID string) (*AgentConfig, error)
}

type AgentManager struct {
	agents map[string]*Agent
	db     AgentDB
	llm    LLMClient
	mu     sync.RWMutex
}

func NewAgentManager(db AgentDB, llm LLMClient, quota interface{}, mem0 interface{}) *AgentManager {
	return &AgentManager{
		agents: make(map[string]*Agent),
		db:     db,
		llm:    llm,
	}
}

func (m *AgentManager) GetAgent(userID, agentID string) *Agent {
	m.mu.RLock()
	if agent, exists := m.agents[agentID]; exists {
		m.mu.RUnlock()
		return agent
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if agent, exists := m.agents[agentID]; exists {
		return agent
	}

	config, err := m.db.GetAgentConfig(userID, agentID)
	if err != nil || config == nil {
		return nil
	}

	agent := NewAgent(AgentOptions{
		ID:           agentID,
		UserID:       userID,
		AgentID:      config.AgentID,
		SystemPrompt: config.SystemPrompt,
		LLMClient:    m.llm,
	})

	m.agents[agentID] = agent
	return agent
}

func (m *AgentManager) Evict(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.agents, agentID)
}
