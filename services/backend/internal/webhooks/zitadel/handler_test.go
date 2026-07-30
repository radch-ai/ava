package zitadel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testComputeSignature computes a signature for testing purposes.
func testComputeSignature(t time.Time, payload []byte, signingKey string) []byte {
	mac := hmac.New(sha256.New, []byte(signingKey))
	_, _ = fmt.Fprintf(mac, "%d", t.Unix())
	mac.Write([]byte("."))
	mac.Write(payload)
	return mac.Sum(nil)
}

const testSigningKey = "test-signing-key-12345"

func TestHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		body           interface{}
		wantStatus     int
		wantCreatedUID string // if set, verify CreateUser was called with this ID
		wantDeletedUID string // if set, verify DeleteUser was called with this ID
	}{
		{
			name:   "user.human.added success (v2 format)",
			method: http.MethodPost,
			body: WebhookRequest{
				AggregateID: "550e8400-e29b-41d4-a716-446655440000",
				EventType:   EventTypeUserHumanAdded,
			},
			wantStatus:     http.StatusOK,
			wantCreatedUID: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:   "user.removed success (v2 format)",
			method: http.MethodPost,
			body: WebhookRequest{
				AggregateID: "550e8400-e29b-41d4-a716-446655440000",
				EventType:   EventTypeUserRemoved,
			},
			wantStatus:     http.StatusOK,
			wantDeletedUID: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:   "unknown event type returns 200 OK",
			method: http.MethodPost,
			body: WebhookRequest{
				AggregateID: "550e8400-e29b-41d4-a716-446655440000",
				EventType:   "user.updated",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:   "empty event type returns 200 OK",
			method: http.MethodPost,
			body: WebhookRequest{
				AggregateID: "550e8400-e29b-41d4-a716-446655440000",
				EventType:   "",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "wrong method",
			method:     http.MethodGet,
			body:       WebhookRequest{EventType: EventTypeUserHumanAdded, AggregateID: "test"},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "invalid body (no signature) returns 401",
			method:     http.MethodPost,
			body:       "not json",
			wantStatus: http.StatusUnauthorized, // no signature = 401 before body parsing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{}
			service := NewService(mockStore, zerolog.Nop())
			handler := NewHandler(service, zerolog.Nop(), testSigningKey)

			var body bytes.Buffer
			if str, ok := tt.body.(string); ok {
				body.WriteString(str)
			} else {
				require.NoError(t, json.NewEncoder(&body).Encode(tt.body))
			}

			req := httptest.NewRequest(tt.method, "/", &body)

			// Add valid signature for POST requests with valid body
			if tt.method == http.MethodPost {
				if _, ok := tt.body.(string); !ok {
					now := time.Now()
					sig := testComputeSignature(now, body.Bytes(), testSigningKey)
					header := fmt.Sprintf("t=%d,v1=%s", now.Unix(), hex.EncodeToString(sig))
					req.Header.Set(HeaderSignature, header)
				}
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantCreatedUID != "" {
				assert.Equal(t, tt.wantCreatedUID, mockStore.createdUserID, "CreateUser should be called with correct user ID")
			}
			if tt.wantDeletedUID != "" {
				assert.Equal(t, tt.wantDeletedUID, mockStore.deletedUserID, "DeleteUser should be called with correct user ID")
			}
		})
	}
}

func TestHandler_ServiceError(t *testing.T) {
	mockStore := &MockStore{createErr: context.DeadlineExceeded}
	service := NewService(mockStore, zerolog.Nop())
	handler := NewHandler(service, zerolog.Nop(), testSigningKey)

	body := WebhookRequest{
		AggregateID: "550e8400-e29b-41d4-a716-446655440000",
		EventType:   EventTypeUserHumanAdded,
	}
	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))

	req := httptest.NewRequest(http.MethodPost, "/", &buf)

	// Add valid signature
	now := time.Now()
	sig := testComputeSignature(now, buf.Bytes(), testSigningKey)
	header := fmt.Sprintf("t=%d,v1=%s", now.Unix(), hex.EncodeToString(sig))
	req.Header.Set(HeaderSignature, header)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// MockStore implements Store for testing
type MockStore struct {
	createErr     error
	deleteErr     error
	createdUserID string
	deletedUserID string
}

func (m *MockStore) CreateUser(ctx context.Context, userID string) error {
	m.createdUserID = userID
	return m.createErr
}

func (m *MockStore) DeleteUser(ctx context.Context, userID string) error {
	m.deletedUserID = userID
	return m.deleteErr
}

func TestHandler_SignatureVerification(t *testing.T) {
	tests := []struct {
		name           string
		body           WebhookRequest
		signature      string // if empty, compute valid signature; if "none", don't set header
		wantStatus     int
		wantCreatedUID string
	}{
		{
			name: "valid signature",
			body: WebhookRequest{
				AggregateID: "550e8400-e29b-41d4-a716-446655440000",
				EventType:   EventTypeUserHumanAdded,
			},
			wantStatus:     http.StatusOK,
			wantCreatedUID: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name: "invalid signature returns 401",
			body: WebhookRequest{
				AggregateID: "550e8400-e29b-41d4-a716-446655440000",
				EventType:   EventTypeUserHumanAdded,
			},
			signature:  "t=1234567890,v1=invalidhex",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "missing signature header returns 401",
			body: WebhookRequest{
				AggregateID: "550e8400-e29b-41d4-a716-446655440000",
				EventType:   EventTypeUserHumanAdded,
			},
			signature:  "none", // signal to not set header
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "expired timestamp returns 401",
			body: WebhookRequest{
				AggregateID: "550e8400-e29b-41d4-a716-446655440000",
				EventType:   EventTypeUserHumanAdded,
			},
			signature:  "expired", // signal to use expired timestamp
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "future timestamp returns 401",
			body: WebhookRequest{
				AggregateID: "550e8400-e29b-41d4-a716-446655440000",
				EventType:   EventTypeUserHumanAdded,
			},
			signature:  "future", // signal to use future timestamp
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := &MockStore{}
			service := NewService(mockStore, zerolog.Nop())
			handler := NewHandler(service, zerolog.Nop(), testSigningKey)

			var bodyBytes []byte
			bodyBytes, err := json.Marshal(tt.body)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bodyBytes))

			// Set signature header
			switch tt.signature {
			case "none":
				// Don't set header
			case "expired":
				// Use timestamp from 10 minutes ago (outside 5-minute tolerance)
				oldTime := time.Now().Add(-10 * time.Minute)
				sig := testComputeSignature(oldTime, bodyBytes, testSigningKey)
				header := fmt.Sprintf("t=%d,v1=%s", oldTime.Unix(), hex.EncodeToString(sig))
				req.Header.Set(HeaderSignature, header)
			case "future":
				// Use timestamp from 10 minutes in the future (outside 5-minute tolerance)
				futureTime := time.Now().Add(10 * time.Minute)
				sig := testComputeSignature(futureTime, bodyBytes, testSigningKey)
				header := fmt.Sprintf("t=%d,v1=%s", futureTime.Unix(), hex.EncodeToString(sig))
				req.Header.Set(HeaderSignature, header)
			case "":
				// Compute valid signature
				now := time.Now()
				sig := testComputeSignature(now, bodyBytes, testSigningKey)
				header := fmt.Sprintf("t=%d,v1=%s", now.Unix(), hex.EncodeToString(sig))
				req.Header.Set(HeaderSignature, header)
			default:
				// Use provided signature
				req.Header.Set(HeaderSignature, tt.signature)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantCreatedUID != "" {
				assert.Equal(t, tt.wantCreatedUID, mockStore.createdUserID, "CreateUser should be called with correct user ID")
			}
		})
	}
}
