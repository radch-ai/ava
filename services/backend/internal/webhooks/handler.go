package webhooks

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/services/backend/config"
	"github.com/radch-ai/ava/services/backend/internal/webhooks/zitadel"
)

// New creates the webhook router with all providers configured.
func New(cfg *config.Config, dbPool *pgxpool.Pool, logger zerolog.Logger) http.Handler {
	zitadelStore := zitadel.NewPGStore(dbPool)
	zitadelService := zitadel.NewService(zitadelStore, logger)
	zitadelHandler := zitadel.NewHandler(zitadelService, logger, cfg.ZitadelWebhookSigningKey)

	mux := http.NewServeMux()
	mux.Handle("/zitadel/", http.StripPrefix("/zitadel", zitadelHandler))

	return mux
}
