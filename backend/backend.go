// Package backend is the public entry point of the ava backend.
// Everything under backend/internal is invisible outside this subtree; this
// package is the only importable surface.
package backend

import (
	"net/http"

	"github.com/rs/zerolog"
)

// Config is the backend's runtime configuration.
type Config struct {
	DBPath string
}

// New builds the backend HTTP handler. The backend owns its route paths
// under /api.
func New(cfg *Config, logger zerolog.Logger) (http.Handler, error) {
	mux := http.NewServeMux()
	return mux, nil
}
