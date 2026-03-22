package user

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// MockStore implements Store for testing
type MockStore struct {
	agents []AgentSummary
	err    error
}

func (m *MockStore) GetAgentsForUser(ctx context.Context, userID string) ([]AgentSummary, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agents, nil
}
