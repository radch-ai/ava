package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       interface{}
		wantStatus int
	}{
		{
			name:   "user.created success",
			method: http.MethodPost,
			body: WebhookRequest{
				EventType: EventTypeUserCreated,
				UserID:    "550e8400-e29b-41d4-a716-446655440000",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:   "user.deleted success",
			method: http.MethodPost,
			body: WebhookRequest{
				EventType: EventTypeUserDeleted,
				UserID:    "550e8400-e29b-41d4-a716-446655440000",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:   "unknown event type returns 200 OK",
			method: http.MethodPost,
			body: WebhookRequest{
				EventType: "user.updated",
				UserID:    "550e8400-e29b-41d4-a716-446655440000",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:   "empty event type returns 200 OK",
			method: http.MethodPost,
			body: WebhookRequest{
				EventType: "",
				UserID:    "550e8400-e29b-41d4-a716-446655440000",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong method",
			method:     http.MethodGet,
			body:       WebhookRequest{EventType: EventTypeUserCreated, UserID: "test"},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "invalid body",
			method:     http.MethodPost,
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{}
			service := NewService(mockStore, zerolog.Nop())
			handler := NewHandler(service, zerolog.Nop())

			var body bytes.Buffer
			if str, ok := tt.body.(string); ok {
				body.WriteString(str)
			} else {
				require.NoError(t, json.NewEncoder(&body).Encode(tt.body))
			}

			req := httptest.NewRequest(tt.method, "/", &body)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestHandler_ServiceError(t *testing.T) {
	mockStore := &MockStore{createErr: context.DeadlineExceeded}
	service := NewService(mockStore, zerolog.Nop())
	handler := NewHandler(service, zerolog.Nop())

	body := WebhookRequest{
		EventType: EventTypeUserCreated,
		UserID:    "550e8400-e29b-41d4-a716-446655440000",
	}
	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))

	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// MockStore implements Store for testing
type MockStore struct {
	createErr error
	deleteErr error
}

func (m *MockStore) CreateUser(ctx context.Context, userID string) error {
	return m.createErr
}

func (m *MockStore) DeleteUser(ctx context.Context, userID string) error {
	return m.deleteErr
}
