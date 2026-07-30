package user

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nevaria-ai/backend/internal/api/shared"
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

func TestService_GetLylaThread_FirstPage_WithNextCursor(t *testing.T) {
	now := time.Now()
	messages := make([]LylaThreadMessage, 11)
	for i := 0; i < 11; i++ {
		messages[i] = LylaThreadMessage{
			ID:        string(rune('a' + i)),
			Role:      "assistant",
			Content:   "message",
			CreatedAt: now.Add(-time.Duration(i) * time.Second),
		}
	}
	store := &MockStore{threadMessages: messages}
	svc := NewService(store)

	page, err := svc.GetLylaThread(context.Background(), "123", "10", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 10)
	require.NotNil(t, page.NextCursor)
}

func TestService_GetLylaThread_ExactLimit_NoNextCursor(t *testing.T) {
	now := time.Now()
	messages := make([]LylaThreadMessage, 10)
	for i := 0; i < 10; i++ {
		messages[i] = LylaThreadMessage{
			ID:        string(rune('a' + i)),
			Role:      "assistant",
			Content:   "message",
			CreatedAt: now.Add(-time.Duration(i) * time.Second),
		}
	}

	store := &MockStore{threadMessages: messages}
	svc := NewService(store)

	page, err := svc.GetLylaThread(context.Background(), "123", "10", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 10)
	assert.Nil(t, page.NextCursor)
}

func TestService_GetLylaThread_LastPage_NoNextCursor(t *testing.T) {
	now := time.Now()
	store := &MockStore{
		threadMessages: []LylaThreadMessage{
			{ID: "msg-1", Role: "assistant", Content: "only message", CreatedAt: now},
		},
	}
	svc := NewService(store)

	page, err := svc.GetLylaThread(context.Background(), "123", "10", "")
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Nil(t, page.NextCursor)
}

func TestService_GetLylaThread_EmptyThread_ReturnsEmptyItems(t *testing.T) {
	store := &MockStore{
		threadMessages: []LylaThreadMessage{},
	}
	svc := NewService(store)

	page, err := svc.GetLylaThread(context.Background(), "123", "10", "")
	require.NoError(t, err)
	assert.Empty(t, page.Items)
	assert.Nil(t, page.NextCursor)
}

func TestService_GetLylaThread_LowLimit_NormalizesTo20(t *testing.T) {
	now := time.Now()
	messages := make([]LylaThreadMessage, 25)
	for i := 0; i < 25; i++ {
		messages[i] = LylaThreadMessage{
			ID:        string(rune('a' + i)),
			Role:      "user",
			Content:   "message",
			CreatedAt: now.Add(-time.Duration(i) * time.Second),
		}
	}
	store := &MockStore{threadMessages: messages}
	svc := NewService(store)

	page, err := svc.GetLylaThread(context.Background(), "123", "0", "")
	require.NoError(t, err)
	assert.Len(t, page.Items, 20)
	assert.NotNil(t, page.NextCursor)

	page, err = svc.GetLylaThread(context.Background(), "123", "-5", "")
	require.NoError(t, err)
	assert.Len(t, page.Items, 20)
	assert.NotNil(t, page.NextCursor)
}

func TestService_GetLylaThread_InvalidCursor_ReturnsError(t *testing.T) {
	store := &MockStore{}
	svc := NewService(store)

	_, err := svc.GetLylaThread(context.Background(), "123", "10", "invalid-cursor")
	require.Error(t, err)
	assert.ErrorIs(t, err, shared.ErrInvalidCursor)
}

func TestService_GetLylaThread_DecodableEmptyCursor_ReturnsError(t *testing.T) {
	store := &CursorValidationStore{}
	svc := NewService(store)

	rawCursor := base64.URLEncoding.EncodeToString([]byte(`{}`))

	_, err := svc.GetLylaThread(context.Background(), "123", "10", rawCursor)
	require.Error(t, err)
	assert.ErrorIs(t, err, shared.ErrInvalidCursor)
	assert.False(t, store.called)
}

func TestService_GetLylaThread_DecodableBadUUIDCursor_ReturnsError(t *testing.T) {
	store := &CursorValidationStore{}
	svc := NewService(store)

	rawCursor, err := shared.EncodeCursor(time.Now(), "not-a-uuid")
	require.NoError(t, err)

	_, err = svc.GetLylaThread(context.Background(), "123", "10", rawCursor)
	require.Error(t, err)
	assert.ErrorIs(t, err, shared.ErrInvalidCursor)
	assert.False(t, store.called)
}

func TestService_GetLylaThread_CursorPage_AdvancesPastFirstPage(t *testing.T) {
	now := time.Now()
	messages := make([]LylaThreadMessage, 12)
	for i := 0; i < 12; i++ {
		messages[i] = LylaThreadMessage{
			ID:        fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1),
			Role:      "assistant",
			Content:   fmt.Sprintf("message-%d", i),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
	}

	store := &MockStore{threadMessages: messages}
	svc := NewService(store)

	firstPage, err := svc.GetLylaThread(context.Background(), "123", "5", "")
	require.NoError(t, err)
	require.Len(t, firstPage.Items, 5)
	require.NotNil(t, firstPage.NextCursor)

	secondPage, err := svc.GetLylaThread(context.Background(), "123", "5", *firstPage.NextCursor)
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 5)

	assert.Equal(t, "message-5", secondPage.Items[0].Content)
	assert.NotEqual(t, firstPage.Items[0].ID, secondPage.Items[0].ID)
	assert.True(t, secondPage.Items[0].CreatedAt.After(firstPage.Items[len(firstPage.Items)-1].CreatedAt))
}

type CursorValidationStore struct {
	called bool
}

func (s *CursorValidationStore) GetAgentsForUser(ctx context.Context, userID string) ([]AgentSummary, error) {
	return nil, nil
}

func (s *CursorValidationStore) GetLylaThreadMessages(ctx context.Context, userID string, params LylaThreadParams) ([]LylaThreadMessage, error) {
	s.called = true
	return []LylaThreadMessage{}, nil
}
