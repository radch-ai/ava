// Package backend is the public entry point of the ava backend.
// Everything under backend/internal is invisible outside this subtree; this
// package and the sqlstore adapter are its importable surfaces.
package backend

import (
	"database/sql"
	"net/http"

	"github.com/rs/zerolog"
)

// Backend holds the backend's long-lived components. Its handler is the only
// public surface; the database handle will back the /api endpoints as they
// land.
type Backend struct {
	sqlDB   *sql.DB
	handler http.Handler
}

// New builds the backend handler over an already-open database — the caller
// wires the engine, so this package never sees a driver. The backend owns
// its route paths under /api.
func New(sqlDB *sql.DB, logger zerolog.Logger) (http.Handler, error) {
	b := &Backend{
		sqlDB:   sqlDB,
		handler: http.NewServeMux(), // routes arrive with the endpoints
	}
	return b.handler, nil
}
