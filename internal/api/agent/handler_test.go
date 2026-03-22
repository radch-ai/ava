package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	internalctx "github.com/nevaria-ai/backend/internal/ctx"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			path:   "/user/agent/lyla",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						agentConfig: &AgentConfiguration{
							Name: "lyla",
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
				Name: "lyla",
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
			path:   "/user/agent/lyla",
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
			path:   "/user/agent/lyla",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			setupService: func() *Service {
				return &Service{
					store: &MockStore{
						agentConfig: &AgentConfiguration{
							Name: "lyla",
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
				Name: "lyla",
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
			path:   "/user/agent/lyla",
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
			path:   "/user/agent/lyla",
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

func TestHandler_ExtractUserAndAgent(t *testing.T) {
	logger := zerolog.Nop()
	handler := NewHandler(nil, logger)

	tests := []struct {
		name          string
		path          string
		setupContext  func(r *http.Request)
		wantUserID    string
		wantAgentName string
		wantStatus    int
		wantMessage   string
	}{
		{
			name: "simple name",
			path: "/user/agent/lyla",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			wantUserID:    "123456789",
			wantAgentName: "lyla",
		},
		{
			name: "name with trailing slash",
			path: "/user/agent/lyla/",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			wantUserID:    "123456789",
			wantAgentName: "lyla",
		},
		{
			name:        "missing user id",
			path:        "/user/agent/lyla",
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "internal error",
		},
		{
			name: "empty name",
			path: "/user/agent/",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			wantUserID:  "123456789",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "agent name is required",
		},
		{
			name: "name with hyphen",
			path: "/user/agent/my-agent-2",
			setupContext: func(r *http.Request) {
				ctx := internalctx.WithUserID(r.Context(), "123456789")
				*r = *r.WithContext(ctx)
			},
			wantUserID:    "123456789",
			wantAgentName: "my-agent-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.setupContext != nil {
				tt.setupContext(req)
			}

			userID, agentName, status, message := handler.extractUserAndAgent(req)

			assert.Equal(t, tt.wantUserID, userID)
			assert.Equal(t, tt.wantAgentName, agentName)
			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantMessage, message)
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

type MockStore struct {
	agentConfig    *AgentConfiguration
	createResponse *CreateAgentResponse
	err            error
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
	return m.createResponse, nil
}
