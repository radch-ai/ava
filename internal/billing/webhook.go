// Package billing handles RevenueCat webhook events for subscription management.
//
// The package provides:
// - HMAC-SHA256 signature verification for webhook authenticity
// - Webhook event processing for subscription lifecycle events
// - Integration with PostgreSQL for user subscription updates
package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// VerifyWebhookSignature verifies a RevenueCat webhook signature.
// The signature is computed as HMAC-SHA256(payload, secret) and hex-encoded.
func VerifyWebhookSignature(payload []byte, signature, secret string) error {
	if signature == "" {
		return fmt.Errorf("signature is empty")
	}

	if secret == "" {
		return fmt.Errorf("secret is empty")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}
