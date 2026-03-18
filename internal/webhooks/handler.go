package webhooks

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/nevaria-ai/backend/internal/webhooks/zitadel"
)

// Handlers manages all webhook handlers.
type Handlers struct {
	router         http.Handler
	zitadelHandler *zitadel.Handler
}

// New creates a new Handlers with all webhook providers configured.
func New(dbPool *pgxpool.Pool, logger zerolog.Logger) *Handlers {
	zitadelStore := zitadel.NewPGStore(dbPool)
	zitadelService := zitadel.NewService(zitadelStore, logger)
	zitadelHandler := zitadel.NewHandler(zitadelService, logger)

	mux := http.NewServeMux()
	mux.Handle("/zitadel/", http.StripPrefix("/zitadel", zitadelHandler))

	return &Handlers{
		router:         mux,
		zitadelHandler: zitadelHandler,
	}
}

// Router returns the HTTP handler for all webhooks.
func (h *Handlers) Router() http.Handler {
	return h.router
}
