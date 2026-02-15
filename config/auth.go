package config

import "time"

// AuthConfig contains authentication service configuration.
type AuthConfig struct {
	// URL is the base URL of the auth service.
	URL string `env:"AUTH_SERVICE_URL" envDefault:"http://localhost:8081"`

	// Timeout is the timeout for auth service requests.
	Timeout time.Duration `env:"AUTH_TIMEOUT" envDefault:"2s"`

	// AuthInternalSecret is the secret for calling auth service endpoints.
	// This is sent as Bearer token when calling /auth/session on the auth service.
	AuthInternalSecret string `env:"AUTH_INTERNAL_SECRET"`

	// InternalSecret is the gateway's own secret for protecting its /_internal/* endpoints.
	// Incoming requests to gateway's internal endpoints must present this secret.
	InternalSecret string `env:"INTERNAL_SECRET"`
}
