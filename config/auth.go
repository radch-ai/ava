package config

// AuthConfig contains zetidal API configuration for session validation.
type AuthConfig struct {
	// URL is the base URL of the zetidal API for session validation.
	URL string `env:"ZETIDAL_URL" envDefault:"http://localhost:8081"`
}
