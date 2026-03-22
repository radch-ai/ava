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
		wantAgents  []AgentSummary
		wantErr     bool
		errContains string
	}{
		{
			name:   "single agent",
			userID: "user123",
			setupStore: func() *MockStore {
				return &MockStore{
					agents: []AgentSummary{{Name: "agent1", Icon: "robot"}},
				}
			},
			wantAgents: []AgentSummary{{Name: "agent1", Icon: "robot"}},
			wantErr:    false,
		},
		{
			name:   "multiple agents",
			userID: "user456",
			setupStore: func() *MockStore {
				return &MockStore{
					agents: []AgentSummary{
						{Name: "lyla", Icon: "star"},
						{Name: "worker-1", Icon: "gear"},
						{Name: "assistant", Icon: "robot"},
					},
				}
			},
			wantAgents: []AgentSummary{
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
					agents: []AgentSummary{},
				}
			},
			wantAgents: []AgentSummary{},
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
					agents: []AgentSummary{},
				}
			},
			wantAgents: []AgentSummary{},
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
