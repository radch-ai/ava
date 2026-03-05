// Package zitadelvalidate provides Zitadel token introspection for session validation.
//
// This package can be used across services to validate user sessions via Zitadel's
// OAuth token introspection endpoint. It supports both JWT and opaque tokens.
package zitadelvalidate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/rs"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/zitadel-go/v3/pkg/client"
)

// Default timeout for introspection calls.
const DefaultIntrospectionTimeout = 5 * time.Second

// Auth errors.
var (
	ErrSessionInvalid      = errors.New("session invalid")
	ErrSessionExpired      = errors.New("session expired")
	ErrAuthUnavailable     = errors.New("auth service unavailable")
	ErrInvalidAuthResponse = errors.New("invalid response from auth service")
	ErrContextCanceled     = errors.New("context canceled")
)

// Introspector holds the Zitadel resource server client for token introspection.
type Introspector struct {
	resourceServer rs.ResourceServer
	logger         *slog.Logger
	timeout        time.Duration
}

// IntrospectorOption configures an Introspector.
type IntrospectorOption func(*Introspector)

// WithLogger sets a custom logger for the introspector.
func WithLogger(logger *slog.Logger) IntrospectorOption {
	return func(i *Introspector) {
		i.logger = logger
	}
}

// WithTimeout sets a custom timeout for introspection calls.
func WithTimeout(timeout time.Duration) IntrospectorOption {
	return func(i *Introspector) {
		i.timeout = timeout
	}
}

// NewIntrospector creates a new introspector for Zitadel using JWT Profile.
//
// Machine Key JWT Authentication (JWT Profile):
//   - No shared secret that can be leaked like PAT or ClientSecrets
//   - Private key never transmitted
//   - Short-lived tokens with automatic renewal
//   - The machine key JWT authenticates the GATEWAY to Zitadel (NOT the user's JWT)
//
// Note: The ctx parameter is only used for the initial setup. The returned
// Introspector is long-lived and safe to use across requests.
func NewIntrospector(ctx context.Context, domain, keyPath string, opts ...IntrospectorOption) (*Introspector, error) {
	if domain == "" {
		return nil, errors.New("zitadel domain is required")
	}
	if keyPath == "" {
		return nil, errors.New("zitadel key path is required")
	}

	// Load the service account key file
	keyFile, err := client.ConfigFromKeyFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load key file: %w", err)
	}

	// Use background context — resource server is long-lived and should not
	// be tied to any request-scoped context passed by the caller.
	resourceServer, err := rs.NewResourceServerJWTProfile(
		context.Background(),
		domain,
		keyFile.ClientID,
		keyFile.KeyID,
		keyFile.Key,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource server client: %w", err)
	}

	introspector := &Introspector{
		resourceServer: resourceServer,
		logger:         slog.Default(),
		timeout:        DefaultIntrospectionTimeout,
	}

	for _, opt := range opts {
		opt(introspector)
	}

	return introspector, nil
}

// MustNewIntrospector is like NewIntrospector but panics on error.
// Useful for initialization in main() where failures should halt startup.
func MustNewIntrospector(ctx context.Context, domain, keyPath string, opts ...IntrospectorOption) *Introspector {
	i, err := NewIntrospector(ctx, domain, keyPath, opts...)
	if err != nil {
		panic(fmt.Sprintf("zitadelvalidate: %v", err))
	}
	return i
}

// SessionInfo contains validated session information.
type SessionInfo struct {
	UserID string
	Expiry time.Time
}

// ValidateSession validates a session token using Zitadel introspection.
// Accepts both JWT and opaque tokens - Zitadel handles the distinction internally.
//
// Token Introspection vs Local JWT Validation:
//   - Detects revoked tokens (user logout, admin revocation)
//   - Works with opaque tokens, not just JWTs
//   - Single source of truth (Zitadel)
//   - No key rotation concerns
func (i *Introspector) ValidateSession(ctx context.Context, sessionToken string) (*SessionInfo, error) {
	if sessionToken == "" {
		return nil, ErrSessionInvalid
	}

	// Apply explicit timeout to prevent blocking on slow Zitadel responses
	introspectCtx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	start := time.Now()
	resp, err := rs.Introspect[*oidc.IntrospectionResponse](introspectCtx, i.resourceServer, sessionToken)
	latency := time.Since(start)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			i.logger.Debug("introspection canceled", "latency", latency)
			return nil, fmt.Errorf("%w: %w", ErrContextCanceled, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			i.logger.Warn("introspection timeout",
				"latency", latency,
				"timeout", i.timeout,
			)
			return nil, fmt.Errorf("%w: %w", ErrAuthUnavailable, err)
		}
		i.logger.Error("introspection failed",
			"error", err,
			"latency", latency,
		)
		return nil, fmt.Errorf("%w: introspection failed: %w", ErrAuthUnavailable, err)
	}

	if !resp.Active {
		i.logger.Debug("inactive token")
		return nil, ErrSessionInvalid
	}

	// Check expiration explicitly — resp.Active alone isn't sufficient
	// due to potential clock skew or caching layers.
	if resp.Expiration != 0 {
		expiry := resp.Expiration.AsTime()
		if time.Now().After(expiry) {
			i.logger.Debug("token expired", "expiry", expiry)
			return nil, ErrSessionExpired
		}
	}

	if resp.Subject == "" {
		i.logger.Warn("active token missing subject", "active", resp.Active)
		return nil, fmt.Errorf("%w: missing subject in active token", ErrInvalidAuthResponse)
	}

	i.logger.Debug("token validated",
		"subject", resp.Subject,
		"latency", latency,
	)

	var expiry time.Time
	if resp.Expiration != 0 {
		expiry = resp.Expiration.AsTime()
	}

	return &SessionInfo{
		UserID: resp.Subject,
		Expiry: expiry,
	}, nil
}
