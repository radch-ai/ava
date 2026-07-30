package user

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nevaria-ai/backend/internal/api/shared"
	internalctx "github.com/nevaria-ai/backend/internal/ctx"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_GetUserAgents(t *testing.T) {
	tests := []struct {
		name         string
		setupContext func(r *http.Request)
		setupService func() *Service
		wantStatus   int
		wantResponse *GetAgentsResponse
		wantErrBody  string
	}{
		{
			name: "successful response with agents",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user123")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						agents: []AgentSummary{
							{Name: "lyla", Icon: "star"},
							{Name: "worker", Icon: "gear"},
							{Name: "assistant", Icon: "robot"},
						},
					},
				}
			},
			wantStatus: http.StatusOK,
			wantResponse: &GetAgentsResponse{
				Agents: []AgentSummary{
					{Name: "lyla", Icon: "star"},
					{Name: "worker", Icon: "gear"},
					{Name: "assistant", Icon: "robot"},
				},
			},
		},
		{
			name: "successful response with no agents",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user456")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						agents: []AgentSummary{},
					},
				}
			},
			wantStatus: http.StatusOK,
			wantResponse: &GetAgentsResponse{
				Agents: []AgentSummary{},
			},
		},
		{
			name: "user ID not in context",
			setupContext: func(r *http.Request) {
				// Don't add user ID to context
			},
			setupService: func() *Service {
				return &Service{}
			},
			wantStatus:  http.StatusInternalServerError,
			wantErrBody: "internal error",
		},
		{
			name: "service returns error",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						err: assert.AnError,
					},
				}
			},
			wantStatus:  http.StatusInternalServerError,
			wantErrBody: "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			handler := NewHandler(tt.setupService(), logger)

			req := httptest.NewRequest(http.MethodGet, "/user/agents", nil)
			tt.setupContext(req)

			w := httptest.NewRecorder()
			handler.GetUserAgents(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantResponse != nil {
				var response GetAgentsResponse
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tt.wantResponse.Agents, response.Agents)
			}

			if tt.wantErrBody != "" {
				assert.Contains(t, w.Body.String(), tt.wantErrBody)
			}
		})
	}
}

func TestNewHandler(t *testing.T) {
	logger := zerolog.Nop()
	service := &Service{}

	handler := NewHandler(service, logger)

	assert.NotNil(t, handler)
	assert.Equal(t, service, handler.service)
}

func TestNewHandler_NilLogger(t *testing.T) {
	handler := NewHandler(nil, zerolog.Logger{})

	assert.NotNil(t, handler)
}

func TestHandler_GetLylaThread_Success(t *testing.T) {
	now := time.Now()
	messages := make([]LylaThreadMessage, 15)
	for i := 0; i < 15; i++ {
		messages[i] = LylaThreadMessage{
			ID:        string(rune('a' + i)),
			Role:      "assistant",
			Content:   "message",
			CreatedAt: now.Add(-time.Duration(i) * time.Second),
		}
	}

	tests := []struct {
		name         string
		setupContext func(r *http.Request)
		setupStore   func() *MockStore
		query        string
		wantStatus   int
		wantItems    int
		wantErrBody  string
	}{
		{
			name: "successful first page",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user123")
				*r = *r.WithContext(ctx)
			},
			setupStore: func() *MockStore {
				return &MockStore{threadMessages: messages}
			},
			query:      "?limit=10",
			wantStatus: http.StatusOK,
			wantItems:  10,
		},
		{
			name: "empty thread returns 200 with empty items",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user456")
				*r = *r.WithContext(ctx)
			},
			setupStore: func() *MockStore {
				return &MockStore{threadMessages: []LylaThreadMessage{}}
			},
			wantStatus: http.StatusOK,
			wantItems:  0,
		},
		{
			name: "missing user ID returns 500",
			setupContext: func(r *http.Request) {
			},
			setupStore:  func() *MockStore { return &MockStore{} },
			wantStatus:  http.StatusInternalServerError,
			wantErrBody: "internal error",
		},
		{
			name: "invalid cursor returns 400",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user789")
				*r = *r.WithContext(ctx)
			},
			setupStore:  func() *MockStore { return &MockStore{} },
			query:       "?cursor=invalid-cursor",
			wantStatus:  http.StatusBadRequest,
			wantErrBody: "invalid cursor",
		},
		{
			name: "decodable invalid cursor returns 400",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user789")
				*r = *r.WithContext(ctx)
			},
			setupStore:  func() *MockStore { return &MockStore{} },
			query:       "?cursor=" + base64.URLEncoding.EncodeToString([]byte(`{}`)),
			wantStatus:  http.StatusBadRequest,
			wantErrBody: "invalid cursor",
		},
		{
			name: "decodable bad uuid cursor returns 400",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user789")
				*r = *r.WithContext(ctx)
			},
			setupStore: func() *MockStore { return &MockStore{} },
			query: func() string {
				cursor, err := shared.EncodeCursor(time.Now(), "not-a-uuid")
				require.NoError(t, err)
				return "?cursor=" + cursor
			}(),
			wantStatus:  http.StatusBadRequest,
			wantErrBody: "invalid cursor",
		},
		{
			name: "limit=0 falls back to default",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user123")
				*r = *r.WithContext(ctx)
			},
			setupStore: func() *MockStore {
				return &MockStore{threadMessages: messages}
			},
			query:      "?limit=0",
			wantStatus: http.StatusOK,
			wantItems:  15,
		},
		{
			name: "limit=-5 falls back to default",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user123")
				*r = *r.WithContext(ctx)
			},
			setupStore: func() *MockStore {
				return &MockStore{threadMessages: messages}
			},
			query:      "?limit=-5",
			wantStatus: http.StatusOK,
			wantItems:  15,
		},
		{
			name: "limit=abc falls back to default",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "user123")
				*r = *r.WithContext(ctx)
			},
			setupStore: func() *MockStore {
				return &MockStore{threadMessages: messages}
			},
			query:      "?limit=abc",
			wantStatus: http.StatusOK,
			wantItems:  15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			store := tt.setupStore()
			service := NewService(store)
			handler := NewHandler(service, logger)

			req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread"+tt.query, nil)
			tt.setupContext(req)

			w := httptest.NewRecorder()
			handler.GetLylaThread(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantErrBody != "" {
				var response map[string]string
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tt.wantErrBody, response["error"])
			}

			if tt.wantItems >= 0 && tt.wantErrBody == "" {
				var response LylaThreadPage
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Len(t, response.Items, tt.wantItems)
			}
		})
	}
}

func TestHandler_GetLylaThread_CursorPage(t *testing.T) {
	now := time.Now()
	messages := make([]LylaThreadMessage, 25)
	for i := 0; i < 25; i++ {
		messages[i] = LylaThreadMessage{
			ID:        fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1),
			Role:      "assistant",
			Content:   fmt.Sprintf("message-%d", i),
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
	}

	logger := zerolog.Nop()
	store := &MockStore{threadMessages: messages}
	service := NewService(store)
	handler := NewHandler(service, logger)

	req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread?limit=10", nil)
	ctx := internalctx.WithUserID(req.Context(), "user123")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var firstPage LylaThreadPage
	err := json.NewDecoder(w.Body).Decode(&firstPage)
	require.NoError(t, err)
	require.Len(t, firstPage.Items, 10)
	require.NotNil(t, firstPage.NextCursor)

	cursorReq := httptest.NewRequest(http.MethodGet, "/user/lyla-thread?limit=10&cursor="+*firstPage.NextCursor, nil)
	cursorReq = cursorReq.WithContext(ctx)

	w2 := httptest.NewRecorder()
	handler.GetLylaThread(w2, cursorReq)

	require.Equal(t, http.StatusOK, w2.Code)

	var secondPage LylaThreadPage
	err = json.NewDecoder(w2.Body).Decode(&secondPage)
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 10)
	assert.Equal(t, "message-10", secondPage.Items[0].Content)
	assert.NotEqual(t, firstPage.Items[0].ID, secondPage.Items[0].ID)
	assert.True(t, secondPage.Items[0].CreatedAt.After(firstPage.Items[len(firstPage.Items)-1].CreatedAt))
}

func TestHandler_GetLylaThread_StoreError(t *testing.T) {
	logger := zerolog.Nop()
	store := &MockStore{err: errors.New("db error")}
	service := NewService(store)
	handler := NewHandler(service, logger)

	req := httptest.NewRequest(http.MethodGet, "/user/lyla-thread", nil)
	ctx := internalctx.WithUserID(req.Context(), "user123")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "internal error", response["error"])
}

func TestHandler_GetLylaThread_MethodNotAllowed(t *testing.T) {
	logger := zerolog.Nop()
	store := &MockStore{}
	service := NewService(store)
	handler := NewHandler(service, logger)

	req := httptest.NewRequest(http.MethodPost, "/user/lyla-thread", nil)
	ctx := internalctx.WithUserID(req.Context(), "user123")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetLylaThread(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "method not allowed", response["error"])
}

// MockStore implements Store for testing
type MockStore struct {
	agents         []AgentSummary
	threadMessages []LylaThreadMessage
	err            error
}

func (m *MockStore) GetAgentsForUser(ctx context.Context, userID string) ([]AgentSummary, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agents, nil
}

func (m *MockStore) GetLylaThreadMessages(ctx context.Context, userID string, params LylaThreadParams) ([]LylaThreadMessage, error) {
	if m.err != nil {
		return nil, m.err
	}
	start := 0
	if params.Cursor != nil {
		start = len(m.threadMessages)
		for i, message := range m.threadMessages {
			if message.ID == params.Cursor.ID && message.CreatedAt.Equal(params.Cursor.CreatedAt) {
				start = i + 1
				break
			}
		}
		if start == len(m.threadMessages) {
			return nil, shared.ErrInvalidCursor
		}
	}
	if start >= len(m.threadMessages) {
		return []LylaThreadMessage{}, nil
	}
	end := start + params.Limit + 1
	if end > len(m.threadMessages) {
		end = len(m.threadMessages)
	}
	return m.threadMessages[start:end], nil
}
