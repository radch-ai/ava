package billing

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewWebhookHandler(t *testing.T) {
	tests := []struct {
		name          string
		secret        string
		ipWhitelist   []string
		expectedCount int
	}{
		{
			name:          "no IP whitelist",
			secret:        "test-secret",
			ipWhitelist:   nil,
			expectedCount: 0,
		},
		{
			name:          "single IP in whitelist",
			secret:        "test-secret",
			ipWhitelist:   []string{"192.168.1.1"},
			expectedCount: 1,
		},
		{
			name:          "multiple IPs in whitelist",
			secret:        "test-secret",
			ipWhitelist:   []string{"192.168.1.1", "10.0.0.1", "172.16.0.1"},
			expectedCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewWebhookHandler(nil, tt.secret, tt.ipWhitelist)

			if handler.webhookSecret != tt.secret {
				t.Errorf("expected secret %q, got %q", tt.secret, handler.webhookSecret)
			}
			if len(handler.ipWhitelist) != tt.expectedCount {
				t.Errorf("expected %d IPs in whitelist, got %d", tt.expectedCount, len(handler.ipWhitelist))
			}
			for _, ip := range tt.ipWhitelist {
				if !handler.ipWhitelist[ip] {
					t.Errorf("expected IP %q in whitelist", ip)
				}
			}
		})
	}
}

func TestWebhookHandler_SetLogger(t *testing.T) {
	handler := NewWebhookHandler(nil, "secret", nil)

	logger := zerolog.Nop()
	handler.SetLogger(logger)

	// Logger is set (no panic means success)
}

func TestWebhookHandler_UpdateIPWhitelist(t *testing.T) {
	handler := NewWebhookHandler(nil, "secret", []string{"192.168.1.1"})

	if len(handler.ipWhitelist) != 1 {
		t.Errorf("expected 1 IP, got %d", len(handler.ipWhitelist))
	}

	// Update whitelist
	handler.UpdateIPWhitelist([]string{"10.0.0.1", "10.0.0.2"})

	if len(handler.ipWhitelist) != 2 {
		t.Errorf("expected 2 IPs, got %d", len(handler.ipWhitelist))
	}
	if !handler.ipWhitelist["10.0.0.1"] || !handler.ipWhitelist["10.0.0.2"] {
		t.Error("expected new IPs in whitelist")
	}
	if handler.ipWhitelist["192.168.1.1"] {
		t.Error("expected old IP removed from whitelist")
	}
}

func TestProductIDToTier(t *testing.T) {
	tests := []struct {
		productID       string
		expectedTier    int
		expectedCredits float64
		wantErr         bool
	}{
		{"com.nevaria.basic", TierFree, CreditsBasic, false},
		{"com.nevaria.pro", TierPro, CreditsPro, false},
		{"com.nevaria.enterprise", TierEnterprise, CreditsEnterprise, false},
		{"com.nevaria.unknown", 0, 0, true},
		{"", 0, 0, true},
		{"invalid", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.productID, func(t *testing.T) {
			tier, credits, err := productIDToTier(tt.productID)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if tier != tt.expectedTier {
				t.Errorf("expected tier %d, got %d", tt.expectedTier, tier)
			}
			if credits != tt.expectedCredits {
				t.Errorf("expected credits %f, got %f", tt.expectedCredits, credits)
			}
		})
	}
}

func TestNetSplitHostPort(t *testing.T) {
	tests := []struct {
		input        string
		expectedHost string
		expectedPort string
		wantErr      bool
	}{
		{"192.168.1.1:8080", "192.168.1.1", "8080", false},
		{"[::1]:8080", "::1", "8080", false},
		{"[2001:db8::1]:8080", "2001:db8::1", "8080", false},
		{"10.0.0.1", "10.0.0.1", "", false},
		{"[invalid", "", "", true},
		{"localhost:3000", "localhost", "3000", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			host, port, err := netSplitHostPort(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if host != tt.expectedHost {
				t.Errorf("expected host %q, got %q", tt.expectedHost, host)
			}
			if port != tt.expectedPort {
				t.Errorf("expected port %q, got %q", tt.expectedPort, port)
			}
		})
	}
}

func TestWebhookHandler_HandleWebhook_MethodNotAllowed(t *testing.T) {
	handler := NewWebhookHandler(nil, "secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	w := httptest.NewRecorder()

	handler.HandleWebhook(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

func TestWebhookHandler_HandleWebhook_IPWhitelist(t *testing.T) {
	tests := []struct {
		name         string
		whitelist    []string
		remoteAddr   string
		expectStatus int
	}{
		{
			name:         "no whitelist allows all",
			whitelist:    nil,
			remoteAddr:   "192.168.1.100:12345",
			expectStatus: http.StatusUnauthorized, // Will fail on signature, not IP
		},
		{
			name:         "IP in whitelist passes",
			whitelist:    []string{"192.168.1.100"},
			remoteAddr:   "192.168.1.100:12345",
			expectStatus: http.StatusUnauthorized, // Will fail on signature, not IP
		},
		{
			name:         "IP not in whitelist blocked",
			whitelist:    []string{"192.168.1.1"},
			remoteAddr:   "192.168.1.100:12345",
			expectStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewWebhookHandler(nil, "secret", tt.whitelist)

			req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
			req.RemoteAddr = tt.remoteAddr
			w := httptest.NewRecorder()

			handler.HandleWebhook(w, req)

			if w.Code != tt.expectStatus {
				t.Errorf("expected status %d, got %d", tt.expectStatus, w.Code)
			}
		})
	}
}

func TestWebhookHandler_HandleWebhook_InvalidSignature(t *testing.T) {
	handler := NewWebhookHandler(nil, "secret", nil)

	payload := []byte(`{"event_id":"test123","type":"INITIAL_PURCHASE"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-RevenueCat-Signature", "invalid-signature")
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	handler.HandleWebhook(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestWebhookHandler_HandleWebhook_InvalidJSON(t *testing.T) {
	secret := "test-secret"
	handler := NewWebhookHandler(nil, secret, nil)

	payload := []byte(`{invalid json}`)
	signature := computeTestSignature(payload, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
	req.Header.Set("X-RevenueCat-Signature", signature)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	handler.HandleWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestWebhookHandler_HandleWebhook_EmptyBody(t *testing.T) {
	secret := "test-secret"
	handler := NewWebhookHandler(nil, secret, nil)

	req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
	req.Header.Set("X-RevenueCat-Signature", computeTestSignature([]byte{}, secret))
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	handler.HandleWebhook(w, req)

	// Empty body should fail JSON parsing
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestWebhookHandler_checkIPWhitelist(t *testing.T) {
	tests := []struct {
		name       string
		whitelist  []string
		remoteAddr string
		wantErr    bool
	}{
		{
			name:       "empty whitelist allows all",
			whitelist:  nil,
			remoteAddr: "192.168.1.100:12345",
			wantErr:    false,
		},
		{
			name:       "IP in whitelist",
			whitelist:  []string{"192.168.1.100"},
			remoteAddr: "192.168.1.100:12345",
			wantErr:    false,
		},
		{
			name:       "IP not in whitelist",
			whitelist:  []string{"192.168.1.1"},
			remoteAddr: "192.168.1.100:12345",
			wantErr:    true,
		},
		{
			name:       "IPv6 in whitelist",
			whitelist:  []string{"::1"},
			remoteAddr: "[::1]:12345",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewWebhookHandler(nil, "secret", tt.whitelist)

			err := handler.checkIPWhitelist(tt.remoteAddr)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebhookHandler_writeJSON(t *testing.T) {
	handler := NewWebhookHandler(nil, "secret", nil)

	w := httptest.NewRecorder()
	handler.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Errorf("failed to parse response: %v", err)
	}
	if response["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", response["status"])
	}
}

func TestWebhookHandler_writeError(t *testing.T) {
	handler := NewWebhookHandler(nil, "secret", nil)

	w := httptest.NewRecorder()
	handler.writeError(w, http.StatusBadRequest, "test error message")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Errorf("failed to parse response: %v", err)
	}
	if response["error"] != "test error message" {
		t.Errorf("expected error 'test error message', got %q", response["error"])
	}
}

func TestWebhookHandler_HandleWebhook_ReadBodyError(t *testing.T) {
	secret := "test-secret"
	handler := NewWebhookHandler(nil, secret, nil)

	// Create a request with a body that will error on read
	req := httptest.NewRequest(http.MethodPost, "/webhook", &errorReader{})
	req.Header.Set("X-RevenueCat-Signature", "some-signature")
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	handler.HandleWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestWebhookEvent_JSONRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		event WebhookEvent
	}{
		{
			name: "INITIAL_PURCHASE",
			event: WebhookEvent{
				EventID: "evt_123",
				Type:    "INITIAL_PURCHASE",
				Data: EventData{
					ProductID: "com.nevaria.pro",
					CustomerInfo: CustomerInfo{
						AppUserID: "user123",
						Email:     "test@example.com",
					},
				},
			},
		},
		{
			name: "REFUND with transaction",
			event: WebhookEvent{
				EventID: "evt_456",
				Type:    "REFUND",
				Data: EventData{
					ProductID: "com.nevaria.basic",
					CustomerInfo: CustomerInfo{
						AppUserID: "user456",
					},
					Transaction: Transaction{
						Amount: 9.99,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded WebhookEvent
			if err := json.Unmarshal(payload, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.EventID != tt.event.EventID {
				t.Errorf("expected EventID %q, got %q", tt.event.EventID, decoded.EventID)
			}
			if decoded.Type != tt.event.Type {
				t.Errorf("expected Type %q, got %q", tt.event.Type, decoded.Type)
			}
			if decoded.Data.ProductID != tt.event.Data.ProductID {
				t.Errorf("expected ProductID %q, got %q", tt.event.Data.ProductID, decoded.Data.ProductID)
			}
		})
	}
}

func TestCreditsConstants(t *testing.T) {
	// Verify credit constants are as expected
	if CreditsBasic != 100.0 {
		t.Errorf("expected CreditsBasic = 100.0, got %f", CreditsBasic)
	}
	if CreditsPro != 500.0 {
		t.Errorf("expected CreditsPro = 500.0, got %f", CreditsPro)
	}
	if CreditsEnterprise != 2000.0 {
		t.Errorf("expected CreditsEnterprise = 2000.0, got %f", CreditsEnterprise)
	}
}

func TestTierConstants(t *testing.T) {
	// Verify tier constants are as expected
	if TierFree != 0 {
		t.Errorf("expected TierFree = 0, got %d", TierFree)
	}
	if TierPro != 1 {
		t.Errorf("expected TierPro = 1, got %d", TierPro)
	}
	if TierEnterprise != 2 {
		t.Errorf("expected TierEnterprise = 2, got %d", TierEnterprise)
	}
}

// errorReader always returns an error on Read
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

// computeTestSignature computes HMAC-SHA256 signature for testing
func computeTestSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
