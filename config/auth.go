package config

import "time"

// AuthConfig contains authentication configuration.
type AuthConfig struct {
	// URL is the base URL of the zetidal API for session validation.
	URL string `env:"AUTH_SERVICE_URL" envDefault:"http://localhost:8081"`

	// Timeout is the timeout for auth requests.
	Timeout time.Duration `env:"AUTH_TIMEOUT" envDefault:"2s"`
}
