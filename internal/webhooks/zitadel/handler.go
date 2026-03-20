package zitadel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// HeaderSignature is the header name for Zitadel's webhook signature.
const HeaderSignature = "ZITADEL-Signature"

// SignatureTolerance is the max age of a signature before it's rejected.
const SignatureTolerance = 5 * time.Minute

// MaxBodySize is the maximum allowed request body size (1 MB).
const MaxBodySize = 1 << 20

// UserService defines the interface for user operations.
type UserService interface {
	CreateUser(ctx context.Context, userID string) error
	DeleteUser(ctx context.Context, userID string) error
}

// Handler handles Zitadel webhook HTTP requests.
type Handler struct {
	service    UserService
	logger     zerolog.Logger
	signingKey string // optional: if set, signature verification is required
}

// NewHandler creates a new Zitadel webhook handler.
// If signingKey is provided, incoming requests must have a valid ZITADEL-Signature header.
func NewHandler(service UserService, logger zerolog.Logger, signingKey string) *Handler {
	if logger.GetLevel() == zerolog.Disabled {
		logger = zerolog.Nop()
	}
	return &Handler{
		service:    service,
		logger:     logger,
		signingKey: signingKey,
	}
}

// ServeHTTP handles all Zitadel webhook requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Read the raw body for signature verification (limit to prevent DoS)
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, MaxBodySize))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	//nolint:errcheck // body close error is not critical after reading
	r.Body.Close()

	// Verify signature - always required
	signatureHeader := r.Header.Get(HeaderSignature)
	if signatureHeader == "" {
		h.logger.Warn().Msg("missing ZITADEL-Signature header")
		h.writeError(w, http.StatusUnauthorized, "missing signature")
		return
	}

	if err := h.verifySignature(bodyBytes, signatureHeader); err != nil {
		h.logger.Warn().Err(err).Msg("webhook signature verification failed")
		h.writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	var req WebhookRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.EventType {
	case EventTypeUserHumanAdded:
		h.handleUserCreated(w, r, req)
	case EventTypeUserRemoved:
		h.handleUserDeleted(w, r, req)
	default:
		// Return 200 OK for unknown event types to prevent Zitadel retries
		h.logger.Warn().Str("event_type", req.EventType).Msg("unknown event type, ignoring")
		w.WriteHeader(http.StatusOK)
	}
}

// signedHeader represents the parsed ZITADEL-Signature header.
type signedHeader struct {
	timestamp  time.Time
	signatures [][]byte
}

// verifySignature verifies the ZITADEL-Signature header.
// Format: t=<timestamp>,v1=<signature>
// Signature is computed over: timestamp.payload
func (h *Handler) verifySignature(body []byte, signatureHeader string) error {
	header, err := parseSignatureHeader(signatureHeader)
	if err != nil {
		return err
	}

	// Check timestamp is within tolerance (reject both stale and future timestamps)
	if time.Since(header.timestamp).Abs() > SignatureTolerance {
		return fmt.Errorf("signature timestamp outside tolerance window")
	}

	// Compute expected signature: HMAC-SHA256(timestamp + "." + payload)
	expectedSig := computeSignature(header.timestamp, body, h.signingKey)

	// Check if any of the provided signatures match
	for _, sig := range header.signatures {
		if hmac.Equal(expectedSig, sig) {
			return nil
		}
	}

	return fmt.Errorf("no valid signature found")
}

// computeSignature computes the HMAC-SHA256 signature for the given timestamp and payload.
func computeSignature(t time.Time, payload []byte, signingKey string) []byte {
	mac := hmac.New(sha256.New, []byte(signingKey))
	_, _ = fmt.Fprintf(mac, "%d", t.Unix())
	mac.Write([]byte("."))
	mac.Write(payload)
	return mac.Sum(nil)
}

// parseSignatureHeader parses the ZITADEL-Signature header.
// Format: t=<timestamp>,v1=<hex_signature>
func parseSignatureHeader(header string) (*signedHeader, error) {
	sh := &signedHeader{}
	if header == "" {
		return sh, fmt.Errorf("empty signature header")
	}

	pairs := strings.Split(header, ",")
	for _, pair := range pairs {
		parts := strings.Split(pair, "=")
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "t":
			timestamp, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				continue
			}
			sh.timestamp = time.Unix(timestamp, 0)
		case "v1":
			sig, err := hex.DecodeString(parts[1])
			if err != nil {
				continue
			}
			sh.signatures = append(sh.signatures, sig)
		}
	}

	if len(sh.signatures) == 0 {
		return sh, fmt.Errorf("no valid signature in header")
	}

	return sh, nil
}

func (h *Handler) handleUserCreated(w http.ResponseWriter, r *http.Request, req WebhookRequest) {
	// In Zitadel v2, the user ID is in AggregateID for user events
	userID := req.AggregateID
	if userID == "" {
		h.logger.Error().Msg("missing aggregateID in user.human.added event")
		h.writeError(w, http.StatusBadRequest, "missing aggregateID")
		return
	}

	if err := h.service.CreateUser(r.Context(), userID); err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("failed to create user")
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logger.Info().Str("user_id", userID).Msg("user created")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleUserDeleted(w http.ResponseWriter, r *http.Request, req WebhookRequest) {
	// In Zitadel v2, the user ID is in AggregateID for user events
	userID := req.AggregateID
	if userID == "" {
		h.logger.Error().Msg("missing aggregateID in user.removed event")
		h.writeError(w, http.StatusBadRequest, "missing aggregateID")
		return
	}

	if err := h.service.DeleteUser(r.Context(), userID); err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("failed to delete user")
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logger.Info().Str("user_id", userID).Msg("user deleted")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	//nolint:errcheck // encoding error response - nothing we can do if this fails
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
