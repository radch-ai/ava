package server

import (
	"context"
	"net/http"
	"time"

	zitadelvalidate "github.com/radch-ai/ava/pkg/zitadelvalidate"
	"github.com/rs/zerolog"
)

// Server manages the HTTP server and handlers.
type Server struct {
	httpServer *http.Server
	logger     zerolog.Logger
}

// New creates a new Server with all routes configured.
func New(
	logger zerolog.Logger,
	addr string,
	allowedOrigins []string,
	introspector *zitadelvalidate.Introspector,
	apiRouter http.Handler,
	webhookRouter http.Handler,
) *Server {
	mux := http.NewServeMux()

	// Create auth middleware
	authMiddleware := NewAuthMiddleware(introspector, logger)

	// Health check endpoint (unauthenticated)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			logger.Debug().Str("path", "/health").Str("method", r.Method).Msg("method not allowed")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Webhook routes (no auth)
	if webhookRouter != nil {
		logger.Debug().Msg("mounting webhook routes at /webhooks/")
		mux.Handle("/webhooks/", http.StripPrefix("/webhooks", webhookRouter))
	}

	// Protected API routes (CORS + authentication)
	corsMiddleware := newCORSMiddleware(allowedOrigins, logger)
	apiHandler := http.StripPrefix("/api", apiRouter)
	apiHandler = authMiddleware.Authenticate(apiHandler)
	// Mount protected routes under /api prefix
	logger.Debug().Msg("mounting protected API routes at /api/")
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
