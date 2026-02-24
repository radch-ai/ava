// Package billing provides RevenueCat webhook handler implementation for the gateway.
package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// Product tier credits allocation.
const (
	CreditsBasic      = 100.0
	CreditsPro        = 500.0
	CreditsEnterprise = 2000.0
)

// Database defines the interface for database operations needed by the webhook handler.
type Database interface {
	// QueryContext executes a query that returns rows.
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	// QueryRowContext executes a query that is expected to return at most one row.
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	// ExecContext executes a query without returning any rows.
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// WebhookHandler processes RevenueCat webhook events.
type WebhookHandler struct {
	db            Database
	webhookSecret string
	ipWhitelist   map[string]bool
	whitelistMu   sync.RWMutex
	rateLimiter   *rate.Limiter
	logger        zerolog.Logger
}

// WebhookEvent represents a RevenueCat webhook event.
type WebhookEvent struct {
	EventID string    `json:"event_id"`
	Type    string    `json:"type"`
	Data    EventData `json:"data"`
}

// EventData contains the event payload.
type EventData struct {
	ProductID    string       `json:"product_id"`
	CustomerInfo CustomerInfo `json:"customer_info"`
	Transaction  Transaction  `json:"transaction,omitempty"`
}

// CustomerInfo contains customer information.
type CustomerInfo struct {
	AppUserID string `json:"app_user_id"`
	Email     string `json:"email,omitempty"`
}

// Transaction contains transaction details for refunds.
type Transaction struct {
	Amount float64 `json:"amount,omitempty"`
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(db Database, webhookSecret string, ipWhitelist []string) *WebhookHandler {
	h := &WebhookHandler{
		db:            db,
		webhookSecret: webhookSecret,
		ipWhitelist:   make(map[string]bool),
		rateLimiter:   rate.NewLimiter(rate.Every(time.Second), 60), // 60 req/min
		logger:        zerolog.Nop(),
	}

	for _, ip := range ipWhitelist {
		h.ipWhitelist[ip] = true
	}

	return h
}

// SetLogger sets the logger for the handler.
func (h *WebhookHandler) SetLogger(logger zerolog.Logger) {
	h.logger = logger
}

// UpdateIPWhitelist updates the IP whitelist.
func (h *WebhookHandler) UpdateIPWhitelist(ips []string) {
	h.whitelistMu.Lock()
	defer h.whitelistMu.Unlock()

	h.ipWhitelist = make(map[string]bool)
	for _, ip := range ips {
		h.ipWhitelist[ip] = true
	}
}

// HandleWebhook processes incoming RevenueCat webhook requests.
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Only accept POST requests
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Check IP whitelist if configured
	if err := h.checkIPWhitelist(r.RemoteAddr); err != nil {
		h.logger.Warn().Err(err).Str("remote_addr", r.RemoteAddr).Msg("IP not in whitelist")
		h.writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Check rate limit
	if !h.rateLimiter.Allow() {
		h.logger.Warn().Msg("rate limit exceeded")
		h.writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	// Read payload
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to read body")
		h.writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Verify signature
	signature := r.Header.Get("X-RevenueCat-Signature")
	if err := VerifyWebhookSignature(payload, signature, h.webhookSecret); err != nil {
		h.logger.Error().Err(err).Msg("webhook signature verification failed")
		h.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse event
	var event WebhookEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		h.logger.Error().Err(err).Msg("failed to parse webhook event")
		h.writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	// Process event
	if err := h.processEvent(ctx, event); err != nil {
		h.logger.Error().Err(err).Str("event_id", event.EventID).Msg("failed to process webhook event")
		h.writeError(w, http.StatusInternalServerError, "processing failed")
		return
	}

	// Return success
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// processEvent processes a single webhook event.
func (h *WebhookHandler) processEvent(ctx context.Context, event WebhookEvent) error {
	// Check for duplicate processing
	if isProcessed, err := h.isWebhookProcessed(ctx, event.EventID); err != nil {
		return fmt.Errorf("check webhook processed: %w", err)
	} else if isProcessed {
		h.logger.Info().Str("event_id", event.EventID).Msg("webhook already processed, skipping")
		return nil
	}

	// Handle event based on type
	switch event.Type {
	case "INITIAL_PURCHASE":
		if err := h.handleInitialPurchase(ctx, event); err != nil {
			return fmt.Errorf("handle initial purchase: %w", err)
		}
	case "RENEWAL":
		if err := h.handleRenewal(ctx, event); err != nil {
			return fmt.Errorf("handle renewal: %w", err)
		}
	case "CANCELLATION":
		if err := h.handleCancellation(ctx, event); err != nil {
			return fmt.Errorf("handle cancellation: %w", err)
		}
	case "REFUND":
		if err := h.handleRefund(ctx, event); err != nil {
			return fmt.Errorf("handle refund: %w", err)
		}
	case "PRODUCT_CHANGE":
		if err := h.handleProductChange(ctx, event); err != nil {
			return fmt.Errorf("handle product change: %w", err)
		}
	case "NON_RENEWING_PURCHASE":
		// Handle non-renewing purchases as initial purchases
		if err := h.handleInitialPurchase(ctx, event); err != nil {
			return fmt.Errorf("handle non-renewing purchase: %w", err)
		}
	default:
		h.logger.Debug().Str("event_type", event.Type).Msg("unhandled event type")
		return nil
	}

	// Mark webhook as processed
	if err := h.markWebhookProcessed(ctx, event.EventID, event.Type); err != nil {
		return fmt.Errorf("mark webhook processed: %w", err)
	}

	return nil
}

// handleInitialPurchase processes initial subscription purchase events.
func (h *WebhookHandler) handleInitialPurchase(ctx context.Context, event WebhookEvent) error {
	tier, credits, err := productIDToTier(event.Data.ProductID)
	if err != nil {
		return fmt.Errorf("map product ID to tier: %w", err)
	}

	userID, err := h.getOrCreateUser(ctx, event.Data.CustomerInfo)
	if err != nil {
		return fmt.Errorf("get or create user: %w", err)
	}

	// Set billing period (30 days from now)
	now := time.Now().Unix()
	billingPeriodEnd := now + (30 * 24 * 60 * 60)

	if err := h.updateUserSubscription(ctx, userID, tier, "active", now, billingPeriodEnd, credits); err != nil {
		return fmt.Errorf("update user subscription: %w", err)
	}

	h.logger.Info().
		Str("user_id", userID).
		Int("tier", tier).
		Float64("credits", credits).
		Str("event_id", event.EventID).
		Msg("initial purchase processed")

	return nil
}

// handleRenewal processes subscription renewal events.
func (h *WebhookHandler) handleRenewal(ctx context.Context, event WebhookEvent) error {
	tier, credits, err := productIDToTier(event.Data.ProductID)
	if err != nil {
		return fmt.Errorf("map product ID to tier: %w", err)
	}

	userID, err := h.getUserByRevenueCatID(ctx, event.Data.CustomerInfo.AppUserID)
	if err != nil {
		return fmt.Errorf("get user by revenuecat ID: %w", err)
	}

	// Extend billing period by 30 days
	now := time.Now().Unix()
	billingPeriodEnd := now + (30 * 24 * 60 * 60)

	if err := h.updateUserSubscription(ctx, userID, tier, "active", now, billingPeriodEnd, credits); err != nil {
		return fmt.Errorf("update user subscription: %w", err)
	}

	h.logger.Info().
		Str("user_id", userID).
		Int("tier", tier).
		Float64("credits", credits).
		Str("event_id", event.EventID).
		Msg("renewal processed")

	return nil
}

// handleCancellation processes subscription cancellation events.
func (h *WebhookHandler) handleCancellation(ctx context.Context, event WebhookEvent) error {
	userID, err := h.getUserByRevenueCatID(ctx, event.Data.CustomerInfo.AppUserID)
	if err != nil {
		return fmt.Errorf("get user by revenuecat ID: %w", err)
	}

	// Get current subscription info to preserve period dates
	tier, _, periodStart, periodEnd, err := h.getUserSubscription(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user subscription: %w", err)
	}

	if err := h.updateSubscriptionStatus(ctx, userID, tier, "cancelled", periodStart, periodEnd); err != nil {
		return fmt.Errorf("update subscription status: %w", err)
	}

	h.logger.Info().
		Str("user_id", userID).
		Str("event_id", event.EventID).
		Msg("cancellation processed")

	return nil
}

// handleRefund processes purchase refund events.
func (h *WebhookHandler) handleRefund(ctx context.Context, event WebhookEvent) error {
	userID, err := h.getUserByRevenueCatID(ctx, event.Data.CustomerInfo.AppUserID)
	if err != nil {
		return fmt.Errorf("get user by revenuecat ID: %w", err)
	}

	// Get current subscription
	tier, _, periodStart, periodEnd, err := h.getUserSubscription(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user subscription: %w", err)
	}

	// Downgrade tier and reset credits
	newTier := max(0, tier-1)
	var newCredits float64
	switch newTier {
	case TierFree:
		newCredits = CreditsBasic
	case TierPro:
		newCredits = CreditsPro
	case TierEnterprise:
		newCredits = CreditsEnterprise
	}

	if err := h.updateSubscriptionStatus(ctx, userID, newTier, "active", periodStart, periodEnd); err != nil {
		return fmt.Errorf("update subscription status: %w", err)
	}

	if err := h.updateUserCredits(ctx, userID, newCredits); err != nil {
		return fmt.Errorf("update user credits: %w", err)
	}

	h.logger.Info().
		Str("user_id", userID).
		Int("old_tier", tier).
		Int("new_tier", newTier).
		Str("event_id", event.EventID).
		Msg("refund processed")

	return nil
}

// handleProductChange handles product upgrade/downgrade events.
func (h *WebhookHandler) handleProductChange(ctx context.Context, event WebhookEvent) error {
	tier, credits, err := productIDToTier(event.Data.ProductID)
	if err != nil {
		return fmt.Errorf("map product ID to tier: %w", err)
	}

	userID, err := h.getUserByRevenueCatID(ctx, event.Data.CustomerInfo.AppUserID)
	if err != nil {
		return fmt.Errorf("get user by revenuecat ID: %w", err)
	}

	// Keep current billing period
	_, _, periodStart, periodEnd, err := h.getUserSubscription(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user subscription: %w", err)
	}

	if err := h.updateUserSubscription(ctx, userID, tier, "active", periodStart, periodEnd, credits); err != nil {
		return fmt.Errorf("update user subscription: %w", err)
	}

	h.logger.Info().
		Str("user_id", userID).
		Int("tier", tier).
		Float64("credits", credits).
		Str("event_id", event.EventID).
		Msg("product change processed")

	return nil
}

// getOrCreateUser gets an existing user or creates a new one.
func (h *WebhookHandler) getOrCreateUser(ctx context.Context, customerInfo CustomerInfo) (string, error) {
	// First try to find user by email
	if customerInfo.Email != "" {
		userID, err := h.getUserByEmail(ctx, customerInfo.Email)
		if err == nil {
			// Link RevenueCat customer ID if not already set
			_ = h.linkRevenueCatCustomer(ctx, userID, customerInfo.AppUserID)
			return userID, nil
		}
	}

	// Try to find user by RevenueCat customer ID
	userID, err := h.getUserByRevenueCatID(ctx, customerInfo.AppUserID)
	if err == nil {
		return userID, nil
	}

	// User doesn't exist, create new user
	userID = customerInfo.AppUserID // Use RevenueCat customer ID as user ID
	if err := h.createUser(ctx, userID, customerInfo.Email); err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}

	return userID, nil
}

// checkIPWhitelist checks if the remote address is in the whitelist.
func (h *WebhookHandler) checkIPWhitelist(remoteAddr string) error {
	h.whitelistMu.RLock()
	defer h.whitelistMu.RUnlock()

	// If whitelist is empty, allow all IPs
	if len(h.ipWhitelist) == 0 {
		return nil
	}

	// Extract IP from remote address (format: "IP:port")
	ip, _, err := netSplitHostPort(remoteAddr)
	if err != nil {
		return err
	}

	if !h.ipWhitelist[ip] {
		return fmt.Errorf("IP not in whitelist: %s", ip)
	}

	return nil
}

// Database operations

// createUser creates a new user in the database.
func (h *WebhookHandler) createUser(ctx context.Context, userID, email string) error {
	const query = `
		INSERT INTO users (id, email, subscription_tier, subscription_status, credits_remaining, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			email = CASE WHEN $2 != '' THEN EXCLUDED.email ELSE users.email END,
			updated_at = $7
	`

	now := time.Now().Unix()
	_, err := h.db.ExecContext(ctx, query, userID, email, TierFree, "active", CreditsBasic, now, now)
	if err != nil {
		return fmt.Errorf("create user %q: %w", userID, err)
	}

	return nil
}

// getUserByEmail retrieves a user by email address.
func (h *WebhookHandler) getUserByEmail(ctx context.Context, email string) (string, error) {
	const query = `SELECT id FROM users WHERE email = $1`

	var userID string
	err := h.db.QueryRowContext(ctx, query, email).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("user not found with email %q", email)
		}
		return "", fmt.Errorf("get user by email %q: %w", email, err)
	}

	return userID, nil
}

// getUserByRevenueCatID retrieves a user by their RevenueCat customer ID.
func (h *WebhookHandler) getUserByRevenueCatID(ctx context.Context, customerID string) (string, error) {
	const query = `SELECT id FROM users WHERE revenuecat_customer_id = $1`

	var userID string
	err := h.db.QueryRowContext(ctx, query, customerID).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("user not found with revenuecat customer ID %q", customerID)
		}
		return "", fmt.Errorf("get user by revenuecat ID %q: %w", customerID, err)
	}

	return userID, nil
}

// linkRevenueCatCustomer links a RevenueCat customer ID to a user.
func (h *WebhookHandler) linkRevenueCatCustomer(ctx context.Context, userID, customerID string) error {
	const query = `UPDATE users SET revenuecat_customer_id = $1 WHERE id = $2 AND revenuecat_customer_id IS NULL`

	_, err := h.db.ExecContext(ctx, query, customerID, userID)
	if err != nil {
		return fmt.Errorf("link revenuecat customer for user %q: %w", userID, err)
	}

	return nil
}

// getUserSubscription retrieves a user's subscription information.
func (h *WebhookHandler) getUserSubscription(ctx context.Context, userID string) (tier int, status string, periodStart, periodEnd int64, err error) {
	const query = `
		SELECT
			COALESCE(subscription_tier, 0),
			COALESCE(subscription_status, 'active'),
			COALESCE(billing_period_start, 0),
			COALESCE(billing_period_end, 0)
		FROM users
		WHERE id = $1
	`

	err = h.db.QueryRowContext(ctx, query, userID).Scan(&tier, &status, &periodStart, &periodEnd)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, "", 0, 0, fmt.Errorf("user not found: %s", userID)
		}
		return 0, "", 0, 0, fmt.Errorf("get subscription for user %q: %w", userID, err)
	}

	return tier, status, periodStart, periodEnd, nil
}

// updateUserSubscription updates a user's subscription information including credits.
func (h *WebhookHandler) updateUserSubscription(ctx context.Context, userID string, tier int, status string, periodStart, periodEnd int64, credits float64) error {
	const query = `
		UPDATE users
		SET subscription_tier = $1,
		    subscription_status = $2,
		    billing_period_start = $3,
		    billing_period_end = $4,
		    credits_remaining = credits_remaining + $5,
		    updated_at = $6
		WHERE id = $7
	`

	now := time.Now().Unix()
	result, err := h.db.ExecContext(ctx, query, tier, status, periodStart, periodEnd, credits, now, userID)
	if err != nil {
		return fmt.Errorf("update subscription for user %q: %w", userID, err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}

	return nil
}

// updateSubscriptionStatus updates only the subscription status and tier.
func (h *WebhookHandler) updateSubscriptionStatus(ctx context.Context, userID string, tier int, status string, periodStart, periodEnd int64) error {
	const query = `
		UPDATE users
		SET subscription_tier = $1,
		    subscription_status = $2,
		    billing_period_start = $3,
		    billing_period_end = $4,
		    updated_at = $5
		WHERE id = $6
	`

	now := time.Now().Unix()
	result, err := h.db.ExecContext(ctx, query, tier, status, periodStart, periodEnd, now, userID)
	if err != nil {
		return fmt.Errorf("update subscription status for user %q: %w", userID, err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}

	return nil
}

// updateUserCredits updates a user's credit balance.
func (h *WebhookHandler) updateUserCredits(ctx context.Context, userID string, credits float64) error {
	const query = `
		UPDATE users
		SET credits_remaining = $1,
		    updated_at = $2
		WHERE id = $3
	`

	now := time.Now().Unix()
	result, err := h.db.ExecContext(ctx, query, credits, now, userID)
	if err != nil {
		return fmt.Errorf("update credits for user %q: %w", userID, err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}

	return nil
}

// isWebhookProcessed checks if a webhook has already been processed.
func (h *WebhookHandler) isWebhookProcessed(ctx context.Context, eventID string) (bool, error) {
	const query = `SELECT 1 FROM processed_webhooks WHERE event_id = $1`

	var dummy int
	err := h.db.QueryRowContext(ctx, query, eventID).Scan(&dummy)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("check if webhook %q processed: %w", eventID, err)
	}

	return true, nil
}

// markWebhookProcessed marks a webhook event as processed.
func (h *WebhookHandler) markWebhookProcessed(ctx context.Context, eventID, eventType string) error {
	const query = `
		INSERT INTO processed_webhooks (event_id, event_type, processed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (event_id) DO NOTHING
	`

	_, err := h.db.ExecContext(ctx, query, eventID, eventType, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("mark webhook %q as processed: %w", eventID, err)
	}

	return nil
}

// Helper functions

// productIDToTier maps a RevenueCat product ID to subscription tier and credits.
func productIDToTier(productID string) (tier int, credits float64, err error) {
	switch productID {
	case "com.nevaria.basic":
		return TierFree, CreditsBasic, nil
	case "com.nevaria.pro":
		return TierPro, CreditsPro, nil
	case "com.nevaria.enterprise":
		return TierEnterprise, CreditsEnterprise, nil
	default:
		return 0, 0, fmt.Errorf("unknown product ID: %s", productID)
	}
}

// netSplitHostPort splits a network address into host and port.
// This is a simplified version that handles the common cases.
func netSplitHostPort(hostPort string) (host string, port string, err error) {
	// Handle IPv6 addresses
	if strings.HasPrefix(hostPort, "[") {
		end := strings.Index(hostPort, "]")
		if end < 0 {
			return "", "", fmt.Errorf("invalid IPv6 address")
		}
		host = hostPort[1:end]
		if end+1 < len(hostPort) && hostPort[end+1] == ':' {
			port = hostPort[end+2:]
		}
		return host, port, nil
	}

	// Handle IPv4 addresses
	lastColon := strings.LastIndex(hostPort, ":")
	if lastColon < 0 {
		return hostPort, "", nil
	}
	return hostPort[:lastColon], hostPort[lastColon+1:], nil
}

// Response helpers

// writeJSON writes a JSON response with the given status code.
func (h *WebhookHandler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes an error response.
func (h *WebhookHandler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]string{"error": message})
}
