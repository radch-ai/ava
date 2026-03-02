package server

import (
	"context"
	"errors"
	"fmt"

	zitadelresourceserver "github.com/zitadel/oidc/v3/pkg/client/rs"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	zitadelclient "github.com/zitadel/zitadel-go/v3/pkg/client"
)

// Auth errors.
var (
	ErrSessionInvalid      = errors.New("session invalid")
	ErrSessionExpired      = errors.New("session expired")
	ErrAuthUnavailable     = errors.New("auth service unavailable")
	ErrInvalidAuthResponse = errors.New("invalid response from auth service")
	ErrContextCanceled     = errors.New("context canceled")
)

// SessionInfo contains validated session information.
type SessionInfo struct {
	UserID string
}

// ValidateSession validates a session token using Zitadel introspection.
// Accepts both JWT and opaque tokens - Zitadel handles the distinction internally.
//
// Token Introspection vs Local JWT Validation:
//   - Detects revoked tokens (user logout, admin revocation)
//   - Works with opaque tokens, not just JWTs
//   - Single source of truth (Zitadel)
//   - No key rotation concerns
func ValidateSession(ctx context.Context, introspector *ZitadelIntrospector, sessionToken string) (*SessionInfo, error) {
	resp, err := zitadelresourceserver.Introspect[*oidc.IntrospectionResponse](ctx, introspector.resourceServer, sessionToken)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("%w: %w", ErrContextCanceled, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: request timeout", ErrAuthUnavailable)
		}
		return nil, fmt.Errorf("%w: introspection failed: %w", ErrAuthUnavailable, err)
	}

	if !resp.Active {
		return nil, ErrSessionInvalid
	}

	if resp.Subject == "" {
		return nil, ErrSessionInvalid
	}

	return &SessionInfo{
		UserID: resp.Subject,
	}, nil
}

// ZitadelIntrospector holds the Zitadel resource server client for token introspection.
type ZitadelIntrospector struct {
	resourceServer zitadelresourceserver.ResourceServer
}

// NewZitadelIntrospector creates a new introspector for Zitadel using JWT Profile.
//
// Machine Key JWT Authentication (JWT Profile):
//   - No shared secret that can be leaked like PAT or ClientSecrets
//   - Private key never transmitted
//   - Short-lived tokens with automatic renewal
//   - The machine key JWT authenticates the GATEWAY to Zitadel (NOT the user's JWT)
func NewZitadelIntrospector(ctx context.Context, domain, keyPath string) (*ZitadelIntrospector, error) {
	if domain == "" {
		return nil, errors.New("zitadel domain is required")
	}
	if keyPath == "" {
		return nil, errors.New("zitadel key path is required")
	}

	// Load the service account key file
	keyFile, err := zitadelclient.ConfigFromKeyFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load key file: %w", err)
	}

	// Create resource server client with JWT Profile
	resourceServer, err := zitadelresourceserver.NewResourceServerJWTProfile(
		ctx,
		domain,
		keyFile.ClientID,
		keyFile.KeyID,
		keyFile.Key,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource server client: %w", err)
	}

	return &ZitadelIntrospector{
		resourceServer: resourceServer,
	}, nil
}
