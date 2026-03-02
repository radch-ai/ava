package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Auth errors.
var (
	ErrSessionInvalid      = errors.New("session invalid")
	ErrSessionExpired      = errors.New("session expired")
	ErrAuthUnavailable     = errors.New("auth service unavailable")
	ErrInvalidAuthResponse = errors.New("invalid response from auth service")
	ErrContextCanceled     = errors.New("context canceled")
)

const (
	defaultAuthURL    = "http://localhost:8081"
	sessionCookieName = "session"
)

// SessionInfo contains validated session information.
type SessionInfo struct {
	UserID    string
	ExpiresAt int64 // Unix timestamp, 0 if not provided
}

// AuthClient is an HTTP client for the auth service.
type AuthClient struct {
	baseURL string
	client  *http.Client
}

// NewAuthClient creates a new auth service client.
func NewAuthClient(baseURL string) *AuthClient {
	if baseURL == "" {
		baseURL = defaultAuthURL
	}
	return &AuthClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// ValidateSession validates a session token with the auth service.
func (c *AuthClient) ValidateSession(ctx context.Context, sessionToken string) (*SessionInfo, error) {
	reqURL, err := url.Parse(c.baseURL + "/auth/session")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid auth URL: %w", ErrAuthUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create request: %w", ErrAuthUnavailable, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: sessionToken,
	})

	resp, err := c.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("%w: %w", ErrContextCanceled, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: request timeout", ErrAuthUnavailable)
		}
		return nil, fmt.Errorf("%w: %w", ErrAuthUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		var result struct {
			Authenticated bool   `json:"authenticated"`
			UserID        string `json:"user_id"`
			ExpiresAt     int64  `json:"expires_at,omitempty"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("%w: failed to decode response: %w", ErrInvalidAuthResponse, err)
		}
		if !result.Authenticated || result.UserID == "" {
			return nil, ErrSessionInvalid
		}
		return &SessionInfo{
			UserID:    result.UserID,
			ExpiresAt: result.ExpiresAt,
		}, nil

	case http.StatusUnauthorized:
		return nil, ErrSessionInvalid

	case http.StatusServiceUnavailable:
		return nil, ErrAuthUnavailable

	default:
		return nil, fmt.Errorf("%w: unexpected status %d", ErrAuthUnavailable, resp.StatusCode)
	}
}
