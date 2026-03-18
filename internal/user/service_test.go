package user

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_GetAgents(t *testing.T) {
	tests := []struct {
		name        string
		userID      string
		setupStore  func() *MockStore
		wantAgents  []Agent
		wantErr     bool
		errContains string
	}{
		{
			name:   "single agent",
			userID: "user123",
			setupStore: func() *MockStore {
				return &MockStore{
					agents: []Agent{{Name: "agent1", Icon: "robot"}},
				}
			},
			wantAgents: []Agent{{Name: "agent1", Icon: "robot"}},
			wantErr:    false,
		},
		{
			name:   "multiple agents",
			userID: "user456",
			setupStore: func() *MockStore {
				return &MockStore{
					agents: []Agent{
						{Name: "lyla", Icon: "star"},
						{Name: "worker-1", Icon: "gear"},
						{Name: "assistant", Icon: "robot"},
					},
				}
			},
			wantAgents: []Agent{
				{Name: "lyla", Icon: "star"},
				{Name: "worker-1", Icon: "gear"},
				{Name: "assistant", Icon: "robot"},
			},
			wantErr: false,
		},
		{
			name:   "no agents",
			userID: "user789",
			setupStore: func() *MockStore {
				return &MockStore{
					agents: []Agent{},
				}
			},
			wantAgents: []Agent{},
			wantErr:    false,
		},
		{
			name:   "store error",
			userID: "user-error",
			setupStore: func() *MockStore {
				return &MockStore{
					err: errors.New("database error"),
				}
			},
			wantErr:     true,
			errContains: "database error",
		},
		{
			name:   "empty user ID",
			userID: "",
			setupStore: func() *MockStore {
				return &MockStore{
					agents: []Agent{},
				}
			},
			wantAgents: []Agent{},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setupStore()
			svc := NewService(store)

			agents, err := svc.GetAgents(context.Background(), tt.userID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAgents, agents)
		})
	}
}

func TestService_GetAgentConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		userID    string
		agentName string
		mockResp  *AgentConfiguration
		mockErr   error
		wantErr   bool
	}{
		{
			name:      "success",
			userID:    "user123",
			agentName: "lyla",
			mockResp: &AgentConfiguration{
				Name: "lyla",
				Icon: "robot",
				Soul: SoulConfig{Personality: "friendly"},
			},
			wantErr: false,
		},
		{
			name:      "empty agent name",
			userID:    "user123",
			agentName: "",
			wantErr:   true,
		},
		{
			name:      "empty user ID",
			userID:    "",
			agentName: "lyla",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{
				agentConfig: tt.mockResp,
				err:         tt.mockErr,
			}

			svc := NewService(mockStore)
			got, err := svc.GetAgentConfiguration(context.Background(), tt.userID, tt.agentName)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.mockResp, got)
		})
	}
}

func TestService_PatchAgentConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		userID    string
		agentName string
		req       *PatchAgentRequest
		mockResp  *AgentConfiguration
		mockErr   error
		wantErr   bool
	}{
		{
			name:      "success",
			userID:    "user123",
			agentName: "lyla",
			req:       &PatchAgentRequest{UserContext: ptr("updated")},
			mockResp:  &AgentConfiguration{},
			wantErr:   false,
		},
		{
			name:      "empty agent name",
			userID:    "user123",
			agentName: "",
			wantErr:   true,
		},
		{
			name:      "empty user ID",
			userID:    "",
			agentName: "lyla",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{
				agentConfig: tt.mockResp,
				err:         tt.mockErr,
			}

			svc := NewService(mockStore)
			got, err := svc.PatchAgentConfiguration(context.Background(), tt.userID, tt.agentName, tt.req)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.mockResp, got)
		})
	}
}

func TestService_CreateAgent(t *testing.T) {
	tests := []struct {
		name        string
		userID      string
		req         *CreateAgentRequest
		mockResp    *CreateAgentResponse
		mockErr     error
		wantErr     bool
		errContains string
	}{
		{
			name:     "success",
			userID:   "user123",
			req:      &CreateAgentRequest{Name: "lyla"},
			mockResp: &CreateAgentResponse{Name: "lyla"},
			wantErr:  false,
		},
		{
			name:     "duplicate name gets suffix",
			userID:   "user123",
			req:      &CreateAgentRequest{Name: "lyla"},
			mockResp: &CreateAgentResponse{Name: "lyla-2"},
			wantErr:  false,
		},
		{
			name:        "empty user ID",
			userID:      "",
			req:         &CreateAgentRequest{Name: "lyla"},
			wantErr:     true,
			errContains: "user ID is required",
		},
		{
			name:        "empty agent name",
			userID:      "user123",
			req:         &CreateAgentRequest{Name: ""},
			wantErr:     true,
			errContains: "agent name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{
				createResp: tt.mockResp,
				err:        tt.mockErr,
			}

			svc := NewService(mockStore)
			got, err := svc.CreateAgent(context.Background(), tt.userID, tt.req)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.mockResp, got)
		})
	}
}

func ptr(s string) *string { return &s }

// MockStore implements Store for testing
type MockStore struct {
	agents      []Agent
	agentConfig *AgentConfiguration
	createResp  *CreateAgentResponse
	err         error
}

func (m *MockStore) GetAgentsForUser(ctx context.Context, userID string) ([]Agent, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agents, nil
}

func (m *MockStore) GetAgentConfiguration(ctx context.Context, userID, agentName string) (*AgentConfiguration, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agentConfig, nil
}

func (m *MockStore) PatchAgentConfiguration(ctx context.Context, userID, agentName string, req *PatchAgentRequest) (*AgentConfiguration, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agentConfig, nil
}

func (m *MockStore) CreateAgent(ctx context.Context, userID string, req *CreateAgentRequest) (*CreateAgentResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.createResp, nil
}
