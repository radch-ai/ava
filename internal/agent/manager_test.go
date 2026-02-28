package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockAgentDB struct {
	mock.Mock
}

func (m *MockAgentDB) GetAgentConfig(userID, agentID string) (*AgentConfig, error) {
	args := m.Called(userID, agentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*AgentConfig), args.Error(1)
}

func TestAgentManager_GetAgent_ReturnsCached(t *testing.T) {
	mockDB := new(MockAgentDB)
	mockLLM := &MockLLMClient{}

	mgr := NewAgentManager(mockDB, mockLLM, nil, nil)

	config := &AgentConfig{
		AgentID:      "agent-1",
		UserID:       "user-1",
		SystemPrompt: "You are helpful",
	}
	mockDB.On("GetAgentConfig", "user-1", "agent-1").Return(config, nil).Once()

	agent1 := mgr.GetAgent("user-1", "agent-1")
	assert.NotNil(t, agent1)

	agent2 := mgr.GetAgent("user-1", "agent-1")
	assert.NotNil(t, agent2)
	assert.Equal(t, agent1, agent2, "should return same cached agent")

	mockDB.AssertExpectations(t)
}

func TestAgentManager_GetAgent_LoadsFromDB(t *testing.T) {
	mockDB := new(MockAgentDB)
	mockLLM := &MockLLMClient{}

	mgr := NewAgentManager(mockDB, mockLLM, nil, nil)

	config := &AgentConfig{
		AgentID:      "agent-2",
		UserID:       "user-2",
		SystemPrompt: "You are an expert",
	}
	mockDB.On("GetAgentConfig", "user-2", "agent-2").Return(config, nil).Once()

	agent := mgr.GetAgent("user-2", "agent-2")

	assert.NotNil(t, agent)
	state := agent.State()
	assert.Equal(t, "You are an expert", state.SystemPrompt)
	assert.Equal(t, "user-2", state.UserID)
	assert.Equal(t, "agent-2", state.AgentID)

	mockDB.AssertExpectations(t)
}

func TestAgentManager_GetAgent_ReturnsNilIfNotInDB(t *testing.T) {
	mockDB := new(MockAgentDB)
	mockLLM := &MockLLMClient{}

	mgr := NewAgentManager(mockDB, mockLLM, nil, nil)

	mockDB.On("GetAgentConfig", "user-3", "agent-3").Return(nil, nil).Once()

	agent := mgr.GetAgent("user-3", "agent-3")

	assert.Nil(t, agent)

	mockDB.AssertExpectations(t)
}

func TestAgentManager_Evict_RemovesFromCache(t *testing.T) {
	mockDB := new(MockAgentDB)
	mockLLM := &MockLLMClient{}

	mgr := NewAgentManager(mockDB, mockLLM, nil, nil)

	config := &AgentConfig{
		AgentID:      "agent-4",
		UserID:       "user-4",
		SystemPrompt: "Test",
	}
	mockDB.On("GetAgentConfig", "user-4", "agent-4").Return(config, nil)

	agent1 := mgr.GetAgent("user-4", "agent-4")
	assert.NotNil(t, agent1)

	mgr.Evict("agent-4")

	agent2 := mgr.GetAgent("user-4", "agent-4")
	assert.NotNil(t, agent2)
	assert.NotEqual(t, agent1, agent2, "should create new agent after eviction")

	mockDB.AssertNumberOfCalls(t, "GetAgentConfig", 2)
}

func TestAgentManager_Evict_HandlesNonExistent(t *testing.T) {
	mockDB := new(MockAgentDB)
	mockLLM := &MockLLMClient{}

	mgr := NewAgentManager(mockDB, mockLLM, nil, nil)

	assert.NotPanics(t, func() {
		mgr.Evict("non-existent-agent")
	})
}

func TestAgentManager_GetAgent_ConcurrentAccess(t *testing.T) {
	mockDB := new(MockAgentDB)
	mockLLM := &MockLLMClient{}

	mgr := NewAgentManager(mockDB, mockLLM, nil, nil)

	config := &AgentConfig{
		AgentID:      "agent-concurrent",
		UserID:       "user-concurrent",
		SystemPrompt: "Concurrent test",
	}
	mockDB.On("GetAgentConfig", "user-concurrent", "agent-concurrent").Return(config, nil).Once()

	var agents [3]*Agent
	done := make(chan bool)

	for i := 0; i < 3; i++ {
		go func(idx int) {
			agents[idx] = mgr.GetAgent("user-concurrent", "agent-concurrent")
			done <- true
		}(i)
	}

	for i := 0; i < 3; i++ {
		<-done
	}

	assert.NotNil(t, agents[0])
	assert.Equal(t, agents[0], agents[1], "concurrent calls should return same agent")
	assert.Equal(t, agents[0], agents[2], "concurrent calls should return same agent")

	mockDB.AssertExpectations(t)
}

func TestAgentManager_GetAgent_MultipleAgents(t *testing.T) {
	mockDB := new(MockAgentDB)
	mockLLM := &MockLLMClient{}

	mgr := NewAgentManager(mockDB, mockLLM, nil, nil)

	config1 := &AgentConfig{
		AgentID:      "agent-a",
		UserID:       "user-1",
		SystemPrompt: "Agent A",
	}
	config2 := &AgentConfig{
		AgentID:      "agent-b",
		UserID:       "user-1",
		SystemPrompt: "Agent B",
	}
	mockDB.On("GetAgentConfig", "user-1", "agent-a").Return(config1, nil).Once()
	mockDB.On("GetAgentConfig", "user-1", "agent-b").Return(config2, nil).Once()

	agentA := mgr.GetAgent("user-1", "agent-a")
	agentB := mgr.GetAgent("user-1", "agent-b")

	assert.NotNil(t, agentA)
	assert.NotNil(t, agentB)
	assert.NotEqual(t, agentA, agentB, "different agents should be different instances")

	mockDB.AssertExpectations(t)
}
