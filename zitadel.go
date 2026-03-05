// Package zitadelvalidate provides Zitadel token introspection for session validation.
//
// This package validates user sessions via Zitadel's OAuth token introspection
// endpoint. It uses Machine Key JWT Profile authentication to authenticate the
// introspection calls themselves (NOT the user's session token).
package zitadelvalidate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/zitadel/oidc/v3/pkg/client/rs"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/zitadel-go/v3/pkg/client"
)

// DefaultIntrospectionTimeout is the default timeout for introspection calls.
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
	log            zerolog.Logger
	timeout        time.Duration
}

// IntrospectorOption configures an Introspector.
type IntrospectorOption func(*Introspector)

// WithLogger sets a custom zerolog logger for the introspector.
func WithLogger(log zerolog.Logger) IntrospectorOption {
	return func(i *Introspector) {
		i.log = log
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
//   - The machine key JWT authenticates THIS SERVICE to Zitadel (NOT the user's session)
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

	i := &Introspector{
		resourceServer: resourceServer,
		log:            zerolog.Nop(),
		timeout:        DefaultIntrospectionTimeout,
	}

	for _, opt := range opts {
		opt(i)
	}

	return i, nil
}

// MustNewIntrospector is like NewIntrospector but calls log.Fatal on error.
// Useful for initialization in main() where failures should halt startup.
func MustNewIntrospector(ctx context.Context, domain, keyPath string, opts ...IntrospectorOption) *Introspector {
	i, err := NewIntrospector(ctx, domain, keyPath, opts...)
	if err != nil {
		log := zerolog.New(nil).With().Timestamp().Logger()
		log.Fatal().Err(err).Msg("failed to create introspector")
	}
	return i
}

// SessionInfo contains validated session information.
type SessionInfo struct {
	UserID string
	Expiry time.Time
}

// ValidateSession validates a user session token using Zitadel introspection.
//
// The sessionToken is the user's session cookie value, which may be either:
//   - A JWT (JSON Web Token)
//   - An opaque token (random string referencing server-side session)
//
// Zitadel handles the distinction internally — this package just passes it through.
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
			i.log.Debug().Dur("latency", latency).Msg("introspection canceled")
			return nil, fmt.Errorf("%w: %w", ErrContextCanceled, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			i.log.Warn().Dur("latency", latency).Dur("timeout", i.timeout).Msg("introspection timeout")
			return nil, fmt.Errorf("%w: %w", ErrAuthUnavailable, err)
		}
		i.log.Error().Err(err).Dur("latency", latency).Msg("introspection failed")
		return nil, fmt.Errorf("%w: introspection failed: %w", ErrAuthUnavailable, err)
	}

	if !resp.Active {
		i.log.Debug().Msg("inactive token")
		return nil, ErrSessionInvalid
	}

	// Check expiration explicitly — resp.Active alone isn't sufficient
	// due to potential clock skew or caching layers.
	if resp.Expiration != 0 {
		expiry := resp.Expiration.AsTime()
		if time.Now().After(expiry) {
			i.log.Debug().Time("expiry", expiry).Msg("token expired")
			return nil, ErrSessionExpired
		}
	}

	if resp.Subject == "" {
		i.log.Warn().Bool("active", resp.Active).Msg("active token missing subject")
		return nil, fmt.Errorf("%w: missing subject in active token", ErrInvalidAuthResponse)
	}

	i.log.Debug().Str("subject", resp.Subject).Dur("latency", latency).Msg("token validated")

	var expiry time.Time
	if resp.Expiration != 0 {
		expiry = resp.Expiration.AsTime()
	}

	return &SessionInfo{
		UserID: resp.Subject,
		Expiry: expiry,
	}, nil
}
