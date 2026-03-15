package user

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_GetAgentIDs(t *testing.T) {
	tests := []struct {
		name        string
		userID      string
		setupStore  func() *MockStore
		wantIDs     []string
		wantErr     bool
		errContains string
	}{
		{
			name:   "single agent",
			userID: "user123",
			setupStore: func() *MockStore {
				return &MockStore{
					agentIDs: []string{"agent1"},
				}
			},
			wantIDs: []string{"agent1"},
			wantErr: false,
		},
		{
			name:   "multiple agents",
			userID: "user456",
			setupStore: func() *MockStore {
				return &MockStore{
					agentIDs: []string{"agent1", "agent2", "agent3"},
				}
			},
			wantIDs: []string{"agent1", "agent2", "agent3"},
			wantErr: false,
		},
		{
			name:   "no agents",
			userID: "user789",
			setupStore: func() *MockStore {
				return &MockStore{
					agentIDs: []string{},
				}
			},
			wantIDs: []string{},
			wantErr: false,
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
			errContains: "get agent IDs",
		},
		{
			name:   "empty user ID",
			userID: "",
			setupStore: func() *MockStore {
				return &MockStore{
					agentIDs: []string{},
				}
			},
			wantIDs: []string{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setupStore()
			svc := NewService(store)

			agentIDs, err := svc.GetAgentIDs(context.Background(), tt.userID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantIDs, agentIDs)
		})
	}
}

func TestService_GetAgentConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		agentID  string
		mockResp *AgentConfiguration
		mockErr  error
		wantErr  bool
	}{
		{
			name:    "success",
			agentID: "test-agent",
			mockResp: &AgentConfiguration{
				LylaConfiguration: LylaConfiguration{
					Soul: SoulConfig{Name: "Test"},
				},
			},
			wantErr: false,
		},
		{
			name:    "empty agent ID",
			agentID: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{
				agentConfig: tt.mockResp,
				err:         tt.mockErr,
			}

			svc := NewService(mockStore)
			got, err := svc.GetAgentConfiguration(context.Background(), tt.agentID)

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
		name     string
		agentID  string
		req      *PatchAgentRequest
		mockResp *AgentConfiguration
		mockErr  error
		wantErr  bool
	}{
		{
			name:     "success",
			agentID:  "test-agent",
			req:      &PatchAgentRequest{UserContext: ptr("updated")},
			mockResp: &AgentConfiguration{},
			wantErr:  false,
		},
		{
			name:    "empty agent ID",
			agentID: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{
				agentConfig: tt.mockResp,
				err:         tt.mockErr,
			}

			svc := NewService(mockStore)
			got, err := svc.PatchAgentConfiguration(context.Background(), tt.agentID, tt.req)

			if tt.wantErr {
				require.Error(t, err)
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
	agentIDs    []string
	agentConfig *AgentConfiguration
	err         error
}

func (m *MockStore) GetAgentIDsByUserID(ctx context.Context, userID string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agentIDs, nil
}

func (m *MockStore) GetAgentConfiguration(ctx context.Context, agentID string) (*AgentConfiguration, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agentConfig, nil
}

func (m *MockStore) PatchAgentConfiguration(ctx context.Context, agentID string, req *PatchAgentRequest) (*AgentConfiguration, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agentConfig, nil
}
