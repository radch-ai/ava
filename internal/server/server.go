package server

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	zitadelvalidate "github.com/nevaria-ai/zitadel-validate"
	"github.com/rs/zerolog"

	"github.com/nevaria-ai/backend/internal/user"
)

// Server manages the HTTP server and handlers.
type Server struct {
	httpServer *http.Server
	logger     zerolog.Logger
}

// Handlers holds the HTTP handlers for the server.
type Handlers struct {
	UserHandler *user.Handler
}

// WebhookHandlers holds the webhook handlers.
type WebhookHandlers struct {
	Router http.Handler
}

// New creates a new Server with all routes configured.
func New(logger zerolog.Logger, addr string, allowedOrigins []string, dbPool *pgxpool.Pool, introspector *zitadelvalidate.Introspector, handlers *Handlers, webhookHandlers *WebhookHandlers) *Server {
	mux := http.NewServeMux()

	// Create auth middleware
	authMiddleware := NewAuthMiddleware(introspector)

	// Health check endpoint (unauthenticated)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Internal webhook routes (no auth, internal network only)
	if webhookHandlers != nil {
		mux.Handle("/internal/webhooks/", http.StripPrefix(
			"/internal/webhooks",
			webhookHandlers.Router,
		))
	}

	// Protected routes (require authentication)
	protectedMux := http.NewServeMux()

	// Agent endpoints: GET /user/agents (list), POST/GET/PATCH /user/agent/{name} (create/get/patch)
	protectedMux.Handle("/user/agents", http.HandlerFunc(handlers.UserHandler.GetUserAgents))
	protectedMux.Handle("/user/agent/", http.HandlerFunc(handlers.UserHandler.AgentConfigurationHandler))

	// Mount protected routes under /api prefix with CORS handling for browser clients
	corsMiddleware := newCORSMiddleware(allowedOrigins)
	apiHandler := http.StripPrefix("/api", protectedMux)
	apiHandler = authMiddleware.Authenticate(apiHandler)
	mux.Handle("/api/", corsMiddleware(apiHandler))

	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		logger: logger,
	}
}

// Start starts the HTTP server and blocks until ctx is cancelled.
//
// Graceful Shutdown (30 second timeout):
//   - Allows in-flight requests to complete
//   - Not so long as to delay deployments
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info().Str("addr", s.httpServer.Addr).Msg("server starting")

	// Channel to capture server startup errors
	errCh := make(chan error, 1)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		// Server failed to start
		return err
	case <-ctx.Done():
		s.logger.Info().Msg("shutdown signal received")
	}

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	s.logger.Info().Msg("server stopped")
	return nil
}

// HTTPServer returns the underlying HTTP server (for testing).
func (s *Server) HTTPServer() *http.Server {
	return s.httpServer
}
