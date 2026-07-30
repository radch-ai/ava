package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/radch-ai/ava/services/backend/internal/api/shared"
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

func TestService_GetActivity_AgentWithNoActivityReturnsEmptyItems(t *testing.T) {
	store := &MockStore{activityItems: []ActivityItem{}}
	svc := NewService(store)

	page, err := svc.GetActivity(context.Background(), "123456789", "lyla", "10", "")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Empty(t, page.Items)
	assert.Nil(t, page.NextCursor)
}

func TestService_GetActivity_NextCursorCalculatedForMultiPageResults(t *testing.T) {
	now := time.Now().UTC()
	items := []ActivityItem{
		{ID: "00000000-0000-0000-0000-000000000001", CreatedAt: now, Message: "1"},
		{ID: "00000000-0000-0000-0000-000000000002", CreatedAt: now.Add(-time.Minute), Message: "2"},
		{ID: "00000000-0000-0000-0000-000000000003", CreatedAt: now.Add(-2 * time.Minute), Message: "3"},
	}
	store := &MockStore{activityItems: items}
	svc := NewService(store)

	page, err := svc.GetActivity(context.Background(), "123456789", "lyla", "2", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)

	decoded, err := shared.DecodeCursor(*page.NextCursor)
	require.NoError(t, err)
	assert.Equal(t, items[1].ID, decoded.ID)
	assert.True(t, items[1].CreatedAt.Equal(decoded.CreatedAt))
}

func TestService_GetActivity_LowNumericLimitsNormalizeTo20(t *testing.T) {
	items := make([]ActivityItem, 21)
	base := time.Now().UTC()
	for i := range items {
		items[i] = ActivityItem{
			ID:        fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1),
			CreatedAt: base.Add(-time.Duration(i) * time.Minute),
		}
	}
	store := &MockStore{activityItems: items}
	svc := NewService(store)

	page, err := svc.GetActivity(context.Background(), "123456789", "lyla", "0", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 20)
	require.NotNil(t, store.lastActivityParams)
	assert.Equal(t, 20, store.lastActivityParams.Limit)

	page, err = svc.GetActivity(context.Background(), "123456789", "lyla", "-5", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 20)
	require.NotNil(t, store.lastActivityParams)
	assert.Equal(t, 20, store.lastActivityParams.Limit)
}

func TestService_GetActivity_MalformedCursorReturnsInvalidCursor(t *testing.T) {
	store := &MockStore{}
	svc := NewService(store)

	_, err := svc.GetActivity(context.Background(), "123456789", "lyla", "10", "invalid-cursor")
	require.Error(t, err)
	assert.ErrorIs(t, err, shared.ErrInvalidCursor)
}

func TestService_GetActivity_DecodableMalformedCursorReturnsInvalidCursor(t *testing.T) {
	store := &MockStore{}
	svc := NewService(store)

	rawCursor := base64.URLEncoding.EncodeToString([]byte(`{}`))

	_, err := svc.GetActivity(context.Background(), "123456789", "lyla", "10", rawCursor)
	require.Error(t, err)
	assert.ErrorIs(t, err, shared.ErrInvalidCursor)
}

func ptr(s string) *string {
	return &s
}
