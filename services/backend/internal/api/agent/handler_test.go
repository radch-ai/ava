package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalctx "github.com/radch-ai/ava/services/backend/internal/ctx"
)

func TestHandler_GetAgentConfiguration(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		setupContext func(r *http.Request)
		setupService func() *Service
		body         string
		wantStatus   int
		wantResponse interface{}
		wantErrBody  string
	}{
		{
			name:   "successful get agent configuration",
			method: http.MethodGet,
			path:   "/user/agent/ava",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						agentConfig: &AgentConfiguration{
							Name: "ava",
							Icon: "star",
							Soul: SoulConfig{
								Personality: "friendly",
								Identity:    "assistant",
							},
							UserContext:  "user context data",
							ToolsEnabled: []string{"tool-1", "tool-2"},
						},
					},
				}
			},
			wantStatus: http.StatusOK,
			wantResponse: &AgentConfiguration{
				Name: "ava",
				Icon: "star",
				Soul: SoulConfig{
					Personality: "friendly",
					Identity:    "assistant",
				},
				UserContext:  "user context data",
				ToolsEnabled: []string{"tool-1", "tool-2"},
			},
		},
		{
			name:   "agent not found",
			method: http.MethodGet,
			path:   "/user/agent/nonexistent",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						err: assert.AnError,
					},
				}
			},
			wantStatus:  http.StatusNotFound,
			wantErrBody: "agent not found",
		},
		{
			name:   "user ID not in context",
			method: http.MethodGet,
			path:   "/user/agent/ava",
			setupContext: func(r *http.Request) {
			},
			setupService: func() *Service {
				return &Service{}
			},
			wantStatus:  http.StatusInternalServerError,
			wantErrBody: "internal error",
		},
		{
			name:   "empty agent name",
			method: http.MethodGet,
			path:   "/user/agent/",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{}
			},
			wantStatus:  http.StatusBadRequest,
			wantErrBody: "agent name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			handler := NewHandler(tt.setupService(), logger)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			tt.setupContext(req)

			w := httptest.NewRecorder()
			handler.GetAgentConfiguration(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantResponse != nil {
				var response AgentConfiguration
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tt.wantResponse, &response)
			}

			if tt.wantErrBody != "" {
				assert.Contains(t, w.Body.String(), tt.wantErrBody)
			}
		})
	}
}

func TestHandler_PatchAgentConfiguration(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		setupContext func(r *http.Request)
		setupService func() *Service
		body         string
		wantStatus   int
		wantResponse interface{}
		wantErrBody  string
	}{
		{
			name:   "successful patch",
			method: http.MethodPatch,
			path:   "/user/agent/ava",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						agentConfig: &AgentConfiguration{
							Name: "ava",
							Icon: "star",
							Soul: SoulConfig{
								Personality: "updated",
								Identity:    "assistant",
							},
						},
					},
				}
			},
			body:       `{"icon": "star", "soul": {"personality": "updated", "identity": "assistant"}}`,
			wantStatus: http.StatusOK,
			wantResponse: &AgentConfiguration{
				Name: "ava",
				Icon: "star",
				Soul: SoulConfig{
					Personality: "updated",
					Identity:    "assistant",
				},
			},
		},
		{
			name:   "invalid request body",
			method: http.MethodPatch,
			path:   "/user/agent/ava",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{}
			},
			body:        `{invalid json`,
			wantStatus:  http.StatusBadRequest,
			wantErrBody: "invalid request body",
		},
		{
			name:   "service error",
			method: http.MethodPatch,
			path:   "/user/agent/ava",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						err: assert.AnError,
					},
				}
			},
			body:        `{"icon": "star"}`,
			wantStatus:  http.StatusInternalServerError,
			wantErrBody: "failed to update configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			handler := NewHandler(tt.setupService(), logger)

			var bodyReader *strings.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			} else {
				bodyReader = strings.NewReader("")
			}

			req := httptest.NewRequest(tt.method, tt.path, bodyReader)
			tt.setupContext(req)

			w := httptest.NewRecorder()
			handler.PatchAgentConfiguration(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantResponse != nil {
				var response AgentConfiguration
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tt.wantResponse, &response)
			}

			if tt.wantErrBody != "" {
				assert.Contains(t, w.Body.String(), tt.wantErrBody)
			}
		})
	}
}

func TestHandler_CreateAgent(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		setupContext func(r *http.Request)
		setupService func() *Service
		body         string
		wantStatus   int
		wantResponse interface{}
		wantErrBody  string
	}{
		{
			name:   "successful create",
			method: http.MethodPost,
			path:   "/user/agent/newagent",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						createResponse: &CreateAgentResponse{Name: "newagent"},
					},
				}
			},
			body:       `{"icon": "robot"}`,
			wantStatus: http.StatusCreated,
			wantResponse: &CreateAgentResponse{
				Name: "newagent",
			},
		},
		{
			name:   "create with auto-suffixed name",
			method: http.MethodPost,
			path:   "/user/agent/existing",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						createResponse: &CreateAgentResponse{Name: "existing-2"},
					},
				}
			},
			body:       `{"icon": "robot"}`,
			wantStatus: http.StatusCreated,
			wantResponse: &CreateAgentResponse{
				Name: "existing-2",
			},
		},
		{
			name:   "invalid request body",
			method: http.MethodPost,
			path:   "/user/agent/newagent",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{}
			},
			body:        `{invalid json`,
			wantStatus:  http.StatusBadRequest,
			wantErrBody: "invalid request body",
		},
		{
			name:   "service error",
			method: http.MethodPost,
			path:   "/user/agent/newagent",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						err: assert.AnError,
					},
				}
			},
			body:        `{"icon": "robot"}`,
			wantStatus:  http.StatusInternalServerError,
			wantErrBody: "failed to create agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			handler := NewHandler(tt.setupService(), logger)

			var bodyReader *strings.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			} else {
				bodyReader = strings.NewReader("")
			}

			req := httptest.NewRequest(tt.method, tt.path, bodyReader)
			tt.setupContext(req)

			w := httptest.NewRecorder()
			handler.CreateAgent(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantResponse != nil {
				var response CreateAgentResponse
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tt.wantResponse, &response)
			}

			if tt.wantErrBody != "" {
				assert.Contains(t, w.Body.String(), tt.wantErrBody)
			}
		})
	}
}

func TestHandler_AgentConfigurationHandler(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		wantStatus int
	}{
		{"POST routes to CreateAgent", http.MethodPost, http.StatusCreated},
		{"GET routes to GetAgentConfiguration", http.MethodGet, http.StatusOK},
		{"PATCH routes to PatchAgentConfiguration", http.MethodPatch, http.StatusOK},
		{"DELETE returns method not allowed", http.MethodDelete, http.StatusMethodNotAllowed},
		{"PUT returns method not allowed", http.MethodPut, http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := zerolog.Nop()
			mockStore := &MockStore{
				agentConfig:    &AgentConfiguration{Name: "test", Icon: "star"},
				createResponse: &CreateAgentResponse{Name: "test"},
			}
			handler := NewHandler(NewService(mockStore), logger)

			var bodyReader *strings.Reader
			if tt.method == http.MethodPost || tt.method == http.MethodPatch {
				bodyReader = strings.NewReader(`{}`)
			} else {
				bodyReader = strings.NewReader("")
			}

			req := httptest.NewRequest(tt.method, "/user/agent/test", bodyReader)
			ctx := internalctx.WithUserID(req.Context(), "123456789")
			*req = *req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.AgentConfigurationHandler(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestHandler_GetAgentConfiguration_PreservesSlashInAgentName(t *testing.T) {
	logger := zerolog.Nop()
	handler := NewHandler(&Service{
		store: &MockStore{
			agentConfig: &AgentConfiguration{Name: "foo/bar", Icon: "star"},
		},
	}, logger)

	req := httptest.NewRequest(http.MethodGet, "/user/agent/foo/bar", nil)
	req = req.WithContext(internalctx.WithUserID(req.Context(), "123456789"))

	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response AgentConfiguration
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "foo/bar", response.Name)
}

func TestHandler_GetAgentConfiguration_ActivitySegmentInNameStaysConfigRoute(t *testing.T) {
	logger := zerolog.Nop()
	handler := NewHandler(&Service{
		store: &MockStore{
			agentConfig: &AgentConfiguration{Name: "foo/activity/bar", Icon: "star"},
		},
	}, logger)

	req := httptest.NewRequest(http.MethodGet, "/user/agent/foo/activity/bar", nil)
	req = req.WithContext(internalctx.WithUserID(req.Context(), "123456789"))

	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response AgentConfiguration
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "foo/activity/bar", response.Name)
}

func TestHandler_ExtractUserAndAgent(t *testing.T) {
	handler := NewHandler(nil, zerolog.Nop())

	tests := []struct {
		name          string
		path          string
		withUserID    bool
		wantUserID    string
		wantAgentName string
		wantStatus    int
		wantMessage   string
	}{
		{
			name:          "config path preserves slash-containing name",
			path:          "/user/agent/foo/bar",
			withUserID:    true,
			wantUserID:    "123456789",
			wantAgentName: "foo/bar",
		},
		{
			name:          "activity path strips suffix and extracts name",
			path:          "/user/agent/foo/bar/activity",
			withUserID:    true,
			wantUserID:    "123456789",
			wantAgentName: "foo/bar",
		},
		{
			name:        "missing user id returns internal error",
			path:        "/user/agent/ava",
			withUserID:  false,
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "internal error",
		},
		{
			name:          "non-activity path returns agent name as-is",
			path:          "/user/agent/ava",
			withUserID:    true,
			wantUserID:    "123456789",
			wantAgentName: "ava",
		},
		{
			name:        "empty extracted name returns bad request",
			path:        "/user/agent/",
			withUserID:  true,
			wantUserID:  "123456789",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "agent name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.withUserID {
				req = req.WithContext(internalctx.WithUserID(req.Context(), "123456789"))
			}

			userID, agentName, status, message := handler.extractUserAndAgent(req)

			assert.Equal(t, tt.wantUserID, userID)
			assert.Equal(t, tt.wantAgentName, agentName)
			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantMessage, message)
		})
	}
}

func TestHandler_GetAgentConfiguration_TrailingActivitySegmentRoutesToActivity(t *testing.T) {
	logger := zerolog.Nop()
	handler := NewHandler(&Service{
		store: &MockStore{
			activityItems: []ActivityItem{},
		},
	}, logger)

	req := httptest.NewRequest(http.MethodGet, "/user/agent/foo/bar/activity", nil)
	req = req.WithContext(internalctx.WithUserID(req.Context(), "123456789"))

	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)

	// Now routes to GetActivity since path ends with /activity
	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandler_GetAgentConfiguration_ActivityExtraPathStaysConfigRoute(t *testing.T) {
	logger := zerolog.Nop()
	handler := NewHandler(&Service{
		store: &MockStore{
			agentConfig: &AgentConfiguration{Name: "foo/activity/extra", Icon: "star"},
		},
	}, logger)

	req := httptest.NewRequest(http.MethodGet, "/user/agent/foo/activity/extra", nil)
	req = req.WithContext(internalctx.WithUserID(req.Context(), "123456789"))

	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response AgentConfiguration
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "foo/activity/extra", response.Name)
}

func TestHandler_GetAgentConfiguration_ActivityTrailingSlashStaysConfigRoute(t *testing.T) {
	logger := zerolog.Nop()
	handler := NewHandler(&Service{
		store: &MockStore{
			agentConfig: &AgentConfiguration{Name: "foo/activity", Icon: "star"},
		},
	}, logger)

	req := httptest.NewRequest(http.MethodGet, "/user/agent/foo/activity/", nil)
	req = req.WithContext(internalctx.WithUserID(req.Context(), "123456789"))

	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response AgentConfiguration
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "foo/activity", response.Name)
}

func TestHandler_AgentConfigurationHandler_ActivitySuffixRoutesToActivity(t *testing.T) {
	handler := NewHandler(NewService(&MockStore{activityItems: []ActivityItem{}}), zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/user/agent/foo/activity", nil)
	req = req.WithContext(internalctx.WithUserID(req.Context(), "123456789"))

	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)

	// Now routes to GetActivity since path ends with /activity
	require.Equal(t, http.StatusOK, w.Code)
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

func TestHandler_GetActivity_PaginatedSuccess(t *testing.T) {
	now := time.Now().UTC()
	items := make([]ActivityItem, 25)
	for i := 0; i < 25; i++ {
		items[i] = ActivityItem{
			ID:          fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1),
			TriggeredBy: "user",
			Action:      "run",
			Status:      "completed",
			Message:     fmt.Sprintf("item-%d", i),
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
		}
	}

	handler := NewHandler(NewService(&MockStore{activityItems: items}), zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/user/agent/ava/activity?limit=10", nil)
	req = req.WithContext(internalctx.WithUserID(req.Context(), "123456789"))

	w := httptest.NewRecorder()
	handler.GetActivity(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var page ActivityPage
	err := json.NewDecoder(w.Body).Decode(&page)
	require.NoError(t, err)
	require.Len(t, page.Items, 10)
	require.NotNil(t, page.NextCursor)

	secondReq := httptest.NewRequest(http.MethodGet, "/user/agent/ava/activity?limit=10&cursor="+*page.NextCursor, nil)
	secondReq = secondReq.WithContext(internalctx.WithUserID(secondReq.Context(), "123456789"))
	secondW := httptest.NewRecorder()
	handler.GetActivity(secondW, secondReq)

	require.Equal(t, http.StatusOK, secondW.Code)
	var secondPage ActivityPage
	err = json.NewDecoder(secondW.Body).Decode(&secondPage)
	require.NoError(t, err)
	require.Len(t, secondPage.Items, 10)
	assert.Equal(t, "item-10", secondPage.Items[0].Message)
}

func TestHandler_GetActivity_ErrorsAndDefaults(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		store       *MockStore
		wantStatus  int
		wantErrBody string
		wantItems   int
	}{
		{
			name:        "invalid cursor returns bad request",
			method:      http.MethodGet,
			path:        "/user/agent/ava/activity?cursor=invalid-cursor",
			store:       &MockStore{},
			wantStatus:  http.StatusBadRequest,
			wantErrBody: "invalid cursor",
		},
		{
			name:       "agent with no activity returns empty items",
			method:     http.MethodGet,
			path:       "/user/agent/ava/activity",
			store:      &MockStore{activityItems: []ActivityItem{}},
			wantStatus: http.StatusOK,
			wantItems:  0,
		},
		{
			name:       "low invalid limits fall back to default behavior",
			method:     http.MethodGet,
			path:       "/user/agent/ava/activity?limit=abc",
			store:      &MockStore{activityItems: makeActivityItems(15)},
			wantStatus: http.StatusOK,
			wantItems:  15,
		},
		{
			name:        "POST method not allowed",
			method:      http.MethodPost,
			path:        "/user/agent/ava/activity",
			store:       &MockStore{},
			wantStatus:  http.StatusMethodNotAllowed,
			wantErrBody: "method not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(NewService(tt.store), zerolog.Nop())
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.path, nil)
			req = req.WithContext(internalctx.WithUserID(req.Context(), "123456789"))

			w := httptest.NewRecorder()
			handler.GetActivity(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantErrBody != "" {
				assert.Equal(t, tt.wantErrBody, decodeErrorBody(t, w.Body.String()))
				return
			}

			var page ActivityPage
			err := json.NewDecoder(w.Body).Decode(&page)
			require.NoError(t, err)
			assert.Len(t, page.Items, tt.wantItems)
		})
	}
}

func TestHandler_NonExactActivityVariantsStillMatchActivity(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "missing name variant returns bad request", path: "/user/agent//activity", wantStatus: http.StatusBadRequest},
		{name: "double slash variant routes to activity", path: "/user/agent/foo//activity", wantStatus: http.StatusOK},
		{name: "trailing slash variant routes to activity", path: "/user/agent/foo/activity/", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(&Service{
				store: &MockStore{activityItems: []ActivityItem{}},
			}, zerolog.Nop())
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req = req.WithContext(internalctx.WithUserID(req.Context(), "123456789"))

			w := httptest.NewRecorder()
			handler.AgentConfigurationHandler(w, req)

			require.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func makeActivityItems(count int) []ActivityItem {
	items := make([]ActivityItem, count)
	base := time.Now().UTC()
	for i := 0; i < count; i++ {
		items[i] = ActivityItem{
			ID:          fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1),
			TriggeredBy: "user",
			Action:      "run",
			Status:      "completed",
			Message:     fmt.Sprintf("item-%d", i),
			CreatedAt:   base.Add(time.Duration(i) * time.Minute),
		}
	}
	return items
}

func decodeErrorBody(t *testing.T, body string) string {
	t.Helper()
	var response map[string]string
	err := json.NewDecoder(strings.NewReader(body)).Decode(&response)
	require.NoError(t, err)
	return response["error"]
}

type MockStore struct {
	agentConfig        *AgentConfiguration
	activityItems      []ActivityItem
	lastActivityParams *ActivityParams
	createResponse     *CreateAgentResponse
	err                error
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

func (m *MockStore) GetAgentActivityRuns(ctx context.Context, userID, agentName string, params ActivityParams) ([]ActivityItem, error) {
	m.lastActivityParams = &params
	if m.err != nil {
		return nil, m.err
	}
	start := 0
	if params.Cursor != nil {
		start = len(m.activityItems)
		for i, item := range m.activityItems {
			if item.CreatedAt.After(params.Cursor.CreatedAt) || (item.CreatedAt.Equal(params.Cursor.CreatedAt) && item.ID > params.Cursor.ID) {
				start = i
				break
			}
		}
	}
	end := start + params.Limit + 1
	if end > len(m.activityItems) {
		end = len(m.activityItems)
	}
	if start > len(m.activityItems) {
		start = len(m.activityItems)
	}
	return m.activityItems[start:end], nil
}

func (m *MockStore) CreateAgent(ctx context.Context, userID string, req *CreateAgentRequest) (*CreateAgentResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.createResponse, nil
}
