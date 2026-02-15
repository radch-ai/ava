package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Errors returned by the auth client.
var (
	ErrSessionInvalid  = errors.New("session invalid")
	ErrSessionExpired  = errors.New("session expired")
	ErrAuthUnavailable = errors.New("auth service unavailable")
	ErrInvalidResponse = errors.New("invalid response from auth service")
	ErrContextCanceled = errors.New("context canceled")
)

const (
	defaultAuthURL    = "http://localhost:8081"
	defaultTimeout    = 2 * time.Second
	sessionCookieName = "session"
)

// SessionInfo contains validated session information.
type SessionInfo struct {
	UserID    string
	ExpiresAt time.Time
	// SetCookie is the cookie from auth service if session was extended (sliding expiration).
	// Gateway must forward this back to the browser using http.SetCookie().
	SetCookie *http.Cookie // nil if session wasn't extended
}

// Client is an HTTP client for the auth service.
type Client struct {
	baseURL string
	secret  string
	Client  *http.Client // Exported for testing
	logger  interface{}  // zerolog.Logger, but interface{} to avoid import
}

// NewAuthClient creates a new auth service client.
func NewAuthClient(baseURL, secret string, logger interface{}) *Client {
	if baseURL == "" {
		baseURL = defaultAuthURL
	}

	return &Client{
		baseURL: baseURL,
		secret:  secret,
		Client: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
}

// ValidateSession validates a session token with the auth service.
// The token should be in the format {sessionId}.{secret}.
func (c *Client) ValidateSession(ctx context.Context, sessionToken string) (*SessionInfo, error) {
	// Build request URL
	reqURL, err := url.Parse(c.baseURL + "/auth/session")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid auth URL: %w", ErrAuthUnavailable, err)
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to create request: %w", ErrAuthUnavailable, err)
	}

	// Add Authorization header with internal secret
	req.Header.Set("Authorization", "Bearer "+c.secret)
	req.Header.Set("Content-Type", "application/json")

	// Add session cookie
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: sessionToken,
	})

	// Execute request
	resp, err := c.Client.Do(req)
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

	// Handle response status
	switch resp.StatusCode {
	case http.StatusOK:
		// Parse successful response
		var result struct {
			Authenticated bool   `json:"authenticated"`
			UserID        string `json:"user_id"`
			ExpiresAt     int64  `json:"expires_at,omitempty"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("%w: failed to decode response: %w", ErrInvalidResponse, err)
		}
		if !result.Authenticated || result.UserID == "" {
			return nil, ErrSessionInvalid
		}

		// Capture session cookie for sliding expiration (auth service may extend session)
		// Gateway must forward this back to the browser
		var setCookie *http.Cookie
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "session" {
				setCookie = cookie
				break
			}
		}

		info := &SessionInfo{
			UserID: result.UserID,
		}
		if result.ExpiresAt > 0 {
			info.ExpiresAt = time.Unix(result.ExpiresAt, 0)
		}
		if setCookie != nil {
			info.SetCookie = setCookie
		}
		return info, nil

	case http.StatusUnauthorized:
		return nil, ErrSessionInvalid

	case http.StatusForbidden:
		return nil, fmt.Errorf("%w: invalid internal secret", ErrAuthUnavailable)

	case http.StatusServiceUnavailable:
		return nil, ErrAuthUnavailable

	default:
		return nil, fmt.Errorf("%w: unexpected status %d", ErrAuthUnavailable, resp.StatusCode)
	}
}
