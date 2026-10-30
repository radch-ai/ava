// Package backend is the backend's public surface: internal/ is
// invisible outside this subtree, so this package and sqlstore are the
// importable entry points.
package backend

import (
	"database/sql"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend/internal/api"
)

// New builds the backend's HTTP handler over an already-open,
// already-migrated database. The route table lives in internal/api;
// the binary entrypoint mounts this handler under /api on its root mux.
func New(sqlDB *sql.DB, logger zerolog.Logger) (http.Handler, error) {
	return api.Handler(sqlDB, logger), nil
}
