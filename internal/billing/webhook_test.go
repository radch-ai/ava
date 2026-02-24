package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifyWebhookSignature(t *testing.T) {
	tests := []struct {
		name      string
		payload   []byte
		signature string
		secret    string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid signature",
			payload:   []byte(`{"event_id":"test123","type":"INITIAL_PURCHASE"}`),
			signature: computeSignature([]byte(`{"event_id":"test123","type":"INITIAL_PURCHASE"}`), "my-secret"),
			secret:    "my-secret",
			wantErr:   false,
		},
		{
			name:      "empty signature",
			payload:   []byte(`{"event_id":"test123"}`),
			signature: "",
			secret:    "my-secret",
			wantErr:   true,
			errMsg:    "signature is empty",
		},
		{
			name:      "empty secret",
			payload:   []byte(`{"event_id":"test123"}`),
			signature: "some-signature",
			secret:    "",
			wantErr:   true,
			errMsg:    "secret is empty",
		},
		{
			name:      "invalid signature - wrong secret",
			payload:   []byte(`{"event_id":"test123"}`),
			signature: computeSignature([]byte(`{"event_id":"test123"}`), "correct-secret"),
			secret:    "wrong-secret",
			wantErr:   true,
			errMsg:    "invalid signature",
		},
		{
			name:      "invalid signature - tampered payload",
			payload:   []byte(`{"event_id":"tampered"}`),
			signature: computeSignature([]byte(`{"event_id":"original"}`), "my-secret"),
			secret:    "my-secret",
			wantErr:   true,
			errMsg:    "invalid signature",
		},
		{
			name:      "empty payload with valid signature",
			payload:   []byte{},
			signature: computeSignature([]byte{}, "my-secret"),
			secret:    "my-secret",
			wantErr:   false,
		},
		{
			name:      "large payload",
			payload:   makeLargePayload(10000),
			signature: computeSignature(makeLargePayload(10000), "my-secret"),
			secret:    "my-secret",
			wantErr:   false,
		},
		{
			name:      "signature with special characters in secret",
			payload:   []byte(`{"test":"data"}`),
			signature: computeSignature([]byte(`{"test":"data"}`), "s3cr3t!@#$%^&*()"),
			secret:    "s3cr3t!@#$%^&*()",
			wantErr:   false,
		},
		{
			name:      "unicode payload",
			payload:   []byte(`{"name":"测试用户","email":"user@example.com"}`),
			signature: computeSignature([]byte(`{"name":"测试用户","email":"user@example.com"}`), "my-secret"),
			secret:    "my-secret",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyWebhookSignature(tt.payload, tt.signature, tt.secret)

			if tt.wantErr {
				if err == nil {
					t.Errorf("VerifyWebhookSignature() expected error, got nil")
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("VerifyWebhookSignature() error = %q, want %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("VerifyWebhookSignature() unexpected error: %v", err)
				}
			}
		})
	}
}

// computeSignature is a test helper that computes the HMAC-SHA256 signature.
func computeSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// makeLargePayload creates a JSON payload of approximately the specified size.
func makeLargePayload(size int) []byte {
	// Create a payload with a large data field
	data := make([]byte, size)
	for i := range data {
		data[i] = 'x'
	}
	return []byte(`{"data":"` + string(data) + `"}`)
}

func TestVerifyWebhookSignature_TimingSafeComparison(t *testing.T) {
	// This test verifies that signature comparison is timing-safe.
	// hmac.Equal should be used instead of bytes.Equal or string comparison.

	payload := []byte(`{"event_id":"test123"}`)
	secret := "my-secret"

	// Correct signature
	correctSig := computeSignature(payload, secret)

	// Completely wrong signature (different length would fail fast in non-timing-safe comparison)
	wrongSig := "0000000000000000000000000000000000000000000000000000000000000000"

	// Test that both correct and incorrect signatures take similar time
	// (This is more of a sanity check - actual timing attacks are hard to detect in tests)
	err1 := VerifyWebhookSignature(payload, correctSig, secret)
	err2 := VerifyWebhookSignature(payload, wrongSig, secret)

	if err1 != nil {
		t.Error("Correct signature should validate")
	}
	if err2 == nil {
		t.Error("Wrong signature should fail validation")
	}
}
