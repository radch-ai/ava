package user

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

func TestHandler_GetUserAgents(t *testing.T) {
	tests := []struct {
		name         string
		setupContext func(r *http.Request)
		setupService func() *Service
		wantStatus   int
		wantResponse *AgentsResponse
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
					store: &MockStoreHandler{
						agents: []Agent{
							{Name: "lyla", Icon: "star"},
							{Name: "worker", Icon: "gear"},
							{Name: "assistant", Icon: "robot"},
						},
					},
				}
			},
			wantStatus: http.StatusOK,
			wantResponse: &AgentsResponse{
				Agents: []Agent{
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
					store: &MockStoreHandler{
						agents: []Agent{},
					},
				}
			},
			wantStatus: http.StatusOK,
			wantResponse: &AgentsResponse{
				Agents: []Agent{},
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
					store: &MockStoreHandler{
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
				var response AgentsResponse
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

func TestHandler_writeJSON(t *testing.T) {
	logger := zerolog.Nop()
	handler := NewHandler(nil, logger)

	w := httptest.NewRecorder()
	data := map[string]string{"status": "ok"}
	handler.writeJSON(w, http.StatusOK, data)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
}

func TestHandler_writeError(t *testing.T) {
	logger := zerolog.Nop()
	handler := NewHandler(nil, logger)

	w := httptest.NewRecorder()
	handler.writeError(w, http.StatusBadRequest, "bad request")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]string
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "bad request", response["error"])
}

func TestHandler_writeJSON_EncodingError(t *testing.T) {
	logger := zerolog.Nop()
	handler := NewHandler(nil, logger)

	w := httptest.NewRecorder()
	// Create a value that can't be marshaled to JSON
	badData := make(chan int)

	handler.writeJSON(w, http.StatusOK, badData)

	// The function should not crash, but the response body will be empty
	// because json.NewEncoder.Encode will fail silently
}

func TestNewHandler(t *testing.T) {
	logger := zerolog.Nop()
	service := &Service{}

	handler := NewHandler(service, logger)

	assert.NotNil(t, handler)
	assert.Equal(t, service, handler.service)
}

func TestNewHandler_NilLogger(t *testing.T) {
	// Test that nil logger is replaced with Nop()
	handler := NewHandler(nil, zerolog.Logger{})

	assert.NotNil(t, handler)
}

// MockStoreHandler implements Store for handler tests
type MockStoreHandler struct {
	agents      []Agent
	agentConfig *AgentConfiguration
	createResp  *CreateAgentResponse
	err         error
}

func (m *MockStoreHandler) GetAgentsForUser(ctx context.Context, userID string) ([]Agent, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agents, nil
}

func (m *MockStoreHandler) GetAgentConfiguration(ctx context.Context, userID, agentName string) (*AgentConfiguration, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agentConfig, nil
}

func (m *MockStoreHandler) PatchAgentConfiguration(ctx context.Context, userID, agentName string, req *PatchAgentRequest) (*AgentConfiguration, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agentConfig, nil
}

func (m *MockStoreHandler) CreateAgent(ctx context.Context, userID string, req *CreateAgentRequest) (*CreateAgentResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.createResp, nil
}

func TestHandler_GetAgentConfiguration_MissingAgentName(t *testing.T) {
	mockStore := &MockStoreHandler{agents: []Agent{}}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	ctx := internalctx.WithUserID(context.Background(), "user123")
	req := httptest.NewRequest(http.MethodGet, "/user/agent/", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.GetAgentConfiguration(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_GetAgentConfiguration_Success(t *testing.T) {
	mockStore := &MockStoreHandler{
		agentConfig: &AgentConfiguration{
			Name: "lyla",
			Icon: "robot",
		},
	}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	ctx := internalctx.WithUserID(context.Background(), "user123")
	req := httptest.NewRequest(http.MethodGet, "/user/agent/lyla", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.GetAgentConfiguration(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response AgentConfiguration
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "lyla", response.Name)
	assert.Equal(t, "robot", response.Icon)
}

func TestHandler_PatchAgentConfiguration_InvalidBody(t *testing.T) {
	mockStore := &MockStoreHandler{agents: []Agent{}}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	ctx := internalctx.WithUserID(context.Background(), "user123")
	req := httptest.NewRequest(http.MethodPatch, "/user/agent/lyla", strings.NewReader("invalid json")).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_PatchAgentConfiguration_MissingAgentName(t *testing.T) {
	mockStore := &MockStoreHandler{agents: []Agent{}}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	ctx := internalctx.WithUserID(context.Background(), "user123")
	req := httptest.NewRequest(http.MethodPatch, "/user/agent/", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_CreateAgent_Success(t *testing.T) {
	mockStore := &MockStoreHandler{
		createResp: &CreateAgentResponse{Name: "lyla"},
	}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	// POST to /user/agent/{name} - name comes from URL
	body := `{"icon": "robot"}`
	ctx := internalctx.WithUserID(context.Background(), "user123")
	req := httptest.NewRequest(http.MethodPost, "/user/agent/lyla", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateAgent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var response CreateAgentResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "lyla", response.Name)
}

func TestHandler_CreateAgent_DuplicateName(t *testing.T) {
	mockStore := &MockStoreHandler{
		createResp: &CreateAgentResponse{Name: "lyla-2"},
	}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	// POST to /user/agent/{name} - name comes from URL
	body := `{}`
	ctx := internalctx.WithUserID(context.Background(), "user123")
	req := httptest.NewRequest(http.MethodPost, "/user/agent/lyla", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateAgent(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var response CreateAgentResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "lyla-2", response.Name)
}

func TestHandler_CreateAgent_MissingName(t *testing.T) {
	mockStore := &MockStoreHandler{}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	// POST to /user/agent/ (no name in URL)
	body := `{}`
	ctx := internalctx.WithUserID(context.Background(), "user123")
	req := httptest.NewRequest(http.MethodPost, "/user/agent/", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateAgent(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_CreateAgent_InvalidBody(t *testing.T) {
	mockStore := &MockStoreHandler{}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	ctx := internalctx.WithUserID(context.Background(), "user123")
	req := httptest.NewRequest(http.MethodPost, "/user/agent/lyla", strings.NewReader("invalid")).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.CreateAgent(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_AgentConfigurationHandler_Routing(t *testing.T) {
	mockStore := &MockStoreHandler{
		agentConfig: &AgentConfiguration{Name: "lyla"},
		createResp:  &CreateAgentResponse{Name: "lyla"},
	}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	ctx := internalctx.WithUserID(context.Background(), "user123")

	// Test POST routing (create)
	req := httptest.NewRequest(http.MethodPost, "/user/agent/lyla", strings.NewReader(`{}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Test GET routing
	req = httptest.NewRequest(http.MethodGet, "/user/agent/lyla", nil).WithContext(ctx)
	w = httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Test PATCH routing
	req = httptest.NewRequest(http.MethodPatch, "/user/agent/lyla", strings.NewReader(`{}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Test method not allowed
	req = httptest.NewRequest(http.MethodDelete, "/user/agent/lyla", nil).WithContext(ctx)
	w = httptest.NewRecorder()
	handler.AgentConfigurationHandler(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
