package config

import "time"

// AuthConfig contains authentication configuration.
type AuthConfig struct {
	// URL is the base URL of the zetidal API for session validation.
	URL string `env:"AUTH_SERVICE_URL" envDefault:"http://localhost:8081"`

	// Timeout is the timeout for auth requests.
	Timeout time.Duration `env:"AUTH_TIMEOUT" envDefault:"2s"`

	// InternalSecret is the gateway's own secret for protecting its /_internal/* endpoints.
	// Incoming requests to gateway's internal endpoints must present this secret.
	InternalSecret string `env:"INTERNAL_SECRET"`
}
