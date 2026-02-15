package config

import "time"

// AuthConfig contains authentication service configuration.
type AuthConfig struct {
	// URL is the base URL of the auth service.
	URL string `env:"AUTH_SERVICE_URL" envDefault:"http://localhost:8081"`

	// Timeout is the timeout for auth service requests.
	Timeout time.Duration `env:"AUTH_TIMEOUT" envDefault:"2s"`

	// InternalSecret is the shared secret for service-to-service authentication.
	// This is required when calling auth service endpoints.
	Secret string `env:"INTERNAL_SECRET"`
}
