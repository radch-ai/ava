package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestService_GetAgentConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		userID      string
		agentName   string
		setupStore  func() *MockStore
		wantConfig  *AgentConfiguration
		wantErr     bool
		errContains string
	}{
		{
			name:      "successful get",
			userID:    "123456789",
			agentName: "lyla",
			setupStore: func() *MockStore {
				return &MockStore{
					agentConfig: &AgentConfiguration{
						Name: "lyla",
						Icon: "star",
						Soul: SoulConfig{
							Personality: "friendly",
							Identity:    "assistant",
						},
					},
				}
			},
			wantConfig: &AgentConfiguration{
				Name: "lyla",
				Icon: "star",
				Soul: SoulConfig{
					Personality: "friendly",
					Identity:    "assistant",
				},
			},
			wantErr: false,
		},
		{
			name:      "agent not found",
			userID:    "123456789",
			agentName: "nonexistent",
			setupStore: func() *MockStore {
				return &MockStore{
					err: errors.New("agent not found"),
				}
			},
			wantErr:     true,
			errContains: "get agent configuration",
		},
		{
			name:      "empty user ID",
			userID:    "",
			agentName: "lyla",
			setupStore: func() *MockStore {
				return &MockStore{}
			},
			wantErr:     true,
			errContains: "user ID is required",
		},
		{
			name:      "empty agent name",
			userID:    "123456789",
			agentName: "",
			setupStore: func() *MockStore {
				return &MockStore{}
			},
			wantErr:     true,
			errContains: "agent name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setupStore()
			svc := NewService(store)

			config, err := svc.GetAgentConfiguration(context.Background(), tt.userID, tt.agentName)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantConfig, config)
		})
	}
}

func TestService_PatchAgentConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		userID      string
		agentName   string
		req         *PatchAgentRequest
		setupStore  func() *MockStore
		wantConfig  *AgentConfiguration
		wantErr     bool
		errContains string
	}{
		{
			name:      "successful patch",
			userID:    "123456789",
			agentName: "lyla",
			req: &PatchAgentRequest{
				Icon: ptr("star"),
				Soul: &SoulConfig{
					Personality: "updated",
					Identity:    "assistant",
				},
			},
			setupStore: func() *MockStore {
				return &MockStore{
					agentConfig: &AgentConfiguration{
						Name: "lyla",
						Icon: "star",
						Soul: SoulConfig{
							Personality: "updated",
							Identity:    "assistant",
						},
					},
				}
			},
			wantConfig: &AgentConfiguration{
				Name: "lyla",
				Icon: "star",
				Soul: SoulConfig{
					Personality: "updated",
					Identity:    "assistant",
				},
			},
			wantErr: false,
		},
		{
			name:      "store error",
			userID:    "123456789",
			agentName: "lyla",
			req:       &PatchAgentRequest{Icon: ptr("star")},
			setupStore: func() *MockStore {
				return &MockStore{
					err: errors.New("database error"),
				}
			},
			wantErr:     true,
			errContains: "patch agent configuration",
		},
		{
			name:        "empty user ID",
			userID:      "",
			agentName:   "lyla",
			req:         &PatchAgentRequest{Icon: ptr("star")},
			setupStore:  func() *MockStore { return &MockStore{} },
			wantErr:     true,
			errContains: "user ID is required",
		},
		{
			name:        "empty agent name",
			userID:      "123456789",
			agentName:   "",
			req:         &PatchAgentRequest{Icon: ptr("star")},
			setupStore:  func() *MockStore { return &MockStore{} },
			wantErr:     true,
			errContains: "agent name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setupStore()
			svc := NewService(store)

			config, err := svc.PatchAgentConfiguration(context.Background(), tt.userID, tt.agentName, tt.req)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantConfig, config)
		})
	}
}

func TestService_CreateAgent(t *testing.T) {
	tests := []struct {
		name        string
		userID      string
		req         *CreateAgentRequest
		setupStore  func() *MockStore
		wantResp    *CreateAgentResponse
		wantErr     bool
		errContains string
	}{
		{
			name:   "successful create",
			userID: "123456789",
			req: &CreateAgentRequest{
				Name: "newagent",
				Icon: "robot",
			},
			setupStore: func() *MockStore {
				return &MockStore{
					createResponse: &CreateAgentResponse{Name: "newagent"},
				}
			},
			wantResp: &CreateAgentResponse{Name: "newagent"},
			wantErr:  false,
		},
		{
			name:   "create with auto-suffixed name",
			userID: "123456789",
			req: &CreateAgentRequest{
				Name: "existing",
				Icon: "robot",
			},
			setupStore: func() *MockStore {
				return &MockStore{
					createResponse: &CreateAgentResponse{Name: "existing-2"},
				}
			},
			wantResp: &CreateAgentResponse{Name: "existing-2"},
			wantErr:  false,
		},
		{
			name:   "store error",
			userID: "123456789",
			req: &CreateAgentRequest{
				Name: "newagent",
				Icon: "robot",
			},
			setupStore: func() *MockStore {
				return &MockStore{
					err: errors.New("database error"),
				}
			},
			wantErr:     true,
			errContains: "create agent",
		},
		{
			name:        "empty user ID",
			userID:      "",
			req:         &CreateAgentRequest{Name: "newagent"},
			setupStore:  func() *MockStore { return &MockStore{} },
			wantErr:     true,
			errContains: "user ID is required",
		},
		{
			name:        "empty agent name",
			userID:      "123456789",
			req:         &CreateAgentRequest{Name: ""},
			setupStore:  func() *MockStore { return &MockStore{} },
			wantErr:     true,
			errContains: "agent name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setupStore()
			svc := NewService(store)

			resp, err := svc.CreateAgent(context.Background(), tt.userID, tt.req)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantResp, resp)
		})
	}
}

func TestNewService(t *testing.T) {
	store := &MockStore{}
	svc := NewService(store)

	assert.NotNil(t, svc)
	assert.Equal(t, store, svc.store)
}

func ptr(s string) *string {
	return &s
}
