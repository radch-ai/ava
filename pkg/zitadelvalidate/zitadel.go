// Package zitadelvalidate provides Zitadel token introspection for session validation.
//
// This package validates user sessions via Zitadel's OAuth token introspection
// endpoint. It uses Application Private Key JWT authentication to authenticate
// the introspection calls themselves (NOT the user's session token).
package zitadelvalidate

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
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

// ValidateDomain returns an error if raw is not a valid http or https URL.
func ValidateDomain(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid zitadel domain %q: must be a valid http or https URL", raw)
	}
	return nil
}

// Introspector holds the Zitadel resource server client for token introspection.
type Introspector struct {
	resourceServer rs.ResourceServer
	log            zerolog.Logger
	timeout        time.Duration
}

// NewIntrospector creates a new introspector for Zitadel using JWT Profile.
//
// Application Private Key JWT Authentication:
//   - No shared secret that can be leaked like PAT or ClientSecrets
//   - Private key never transmitted
//   - Short-lived tokens with automatic renewal
//   - The application key JWT authenticates THIS SERVICE to Zitadel (NOT the user's session)
//
// The keyPath should point to an application key file downloaded from Zitadel,
// which has the following structure:
//
//	{
//	    "type": "application",
//	    "keyId": "<YOUR_KEY_ID>",
//	    "key": "-----BEGIN RSA PRIVATE KEY-----...",
//	    "appId": "<YOUR_APP_ID>",
//	    "clientId": "<YOUR_CLIENT_ID>"
//	}
//
// Note: ctx controls the initial OIDC discovery fetch. Once the resource
// server is created it is long-lived and independent of any caller context.
func NewIntrospector(ctx context.Context, domain, keyPath string) (*Introspector, error) {
	if domain == "" {
		return nil, errors.New("zitadel domain is required")
	}
	if keyPath == "" {
		return nil, errors.New("zitadel key path is required")
	}

	// Load the application key file
	keyFile, err := client.ConfigFromKeyFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load key file: %w", err)
	}

	// ctx controls the OIDC discovery fetch; the resource server itself
	// is long-lived and independent of any caller context.
	resourceServer, err := rs.NewResourceServerJWTProfile(
		ctx,
		domain,
		keyFile.ClientID,
		keyFile.KeyID,
		keyFile.Key,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource server client: %w", err)
	}

	return &Introspector{
		resourceServer: resourceServer,
		log:            zerolog.Nop(),
		timeout:        DefaultIntrospectionTimeout,
	}, nil
}

// WithLogger sets a custom zerolog logger for the introspector.
// Returns the Introspector for method chaining.
func (i *Introspector) WithLogger(log zerolog.Logger) *Introspector {
	i.log = log
	return i
}

// WithTimeout sets a custom timeout for introspection calls.
// Returns the Introspector for method chaining.
func (i *Introspector) WithTimeout(timeout time.Duration) *Introspector {
	i.timeout = timeout
	return i
}

// MustNewIntrospector is like NewIntrospector but calls log.Fatal on error.
// Useful for initialization in main() where failures should halt startup.
func MustNewIntrospector(ctx context.Context, domain, keyPath string) *Introspector {
	i, err := NewIntrospector(ctx, domain, keyPath)
	if err != nil {
		log := zerolog.New(os.Stderr).With().Timestamp().Logger()
		log.Fatal().Err(err).Msg("failed to create introspector")
	}
	return i
}

// SessionInfo contains validated session information.
type SessionInfo struct {
	UserID string
	Expiry time.Time
}

// ValidateSession validates an OAuth access token using Zitadel introspection.
//
// The sessionToken is the OAuth access token issued to the user's client.
// It may be a JWT or an opaque token — Zitadel handles the distinction.
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

	// Defensive check — if resp.Expiration is set, verify locally as a
	// belt-and-suspenders measure. Zitadel's active=true should already
	// account for expiry, but this guards against any response caching.
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
