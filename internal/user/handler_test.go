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
						agentIDs: []string{"agent1", "agent2", "agent3"},
					},
				}
			},
			wantStatus: http.StatusOK,
			wantResponse: &AgentsResponse{
				AgentIDs: []string{"agent1", "agent2", "agent3"},
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
						agentIDs: []string{},
					},
				}
			},
			wantStatus: http.StatusOK,
			wantResponse: &AgentsResponse{
				AgentIDs: []string{},
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

			req := httptest.NewRequest(http.MethodGet, "/users/agents", nil)
			tt.setupContext(req)

			w := httptest.NewRecorder()
			handler.GetUserAgents(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantResponse != nil {
				var response AgentsResponse
				err := json.NewDecoder(w.Body).Decode(&response)
				require.NoError(t, err)
				assert.Equal(t, tt.wantResponse.AgentIDs, response.AgentIDs)
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
	agentIDs []string
	err      error
}

func (m *MockStoreHandler) GetAgentIDsByUserID(ctx context.Context, userID string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agentIDs, nil
}

func (m *MockStoreHandler) GetAgentConfiguration(ctx context.Context, agentID string) (*AgentConfiguration, error) {
	return nil, m.err
}

func (m *MockStoreHandler) PatchAgentConfiguration(ctx context.Context, agentID string, req *PatchAgentRequest) (*AgentConfiguration, error) {
	return nil, m.err
}

func TestHandler_GetAgentConfiguration_MethodNotAllowed(t *testing.T) {
	mockStore := &MockStoreHandler{agentIDs: []string{}}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/users/agents/test-id", nil)
	w := httptest.NewRecorder()

	handler.GetAgentConfiguration(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHandler_GetAgentConfiguration_MissingAgentID(t *testing.T) {
	mockStore := &MockStoreHandler{agentIDs: []string{}}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/users/agents/", nil)
	w := httptest.NewRecorder()

	handler.GetAgentConfiguration(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_PatchAgentConfiguration_InvalidBody(t *testing.T) {
	mockStore := &MockStoreHandler{agentIDs: []string{}}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPatch, "/users/agents/test-id", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_PatchAgentConfiguration_MissingAgentID(t *testing.T) {
	mockStore := &MockStoreHandler{agentIDs: []string{}}
	service := NewService(mockStore)
	handler := NewHandler(service, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPatch, "/users/agents/", nil)
	w := httptest.NewRecorder()

	handler.PatchAgentConfiguration(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
