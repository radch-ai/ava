package server

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/pkg/zitadelvalidate"
	"github.com/radch-ai/ava/services/gateway/internal/websocket"
)

// Server manages the HTTP server and WebSocket handlers.
type Server struct {
	httpServer *http.Server
	logger     zerolog.Logger
	wsHandler  *websocket.Handler
}

// New creates a new Server with auth middleware.
// The introspector is required for validating session tokens.
func New(addr string, logger zerolog.Logger, wsHandler *websocket.Handler, introspector *zitadelvalidate.Introspector) *Server {
	authMW := NewAuthMiddleware(introspector)

	mux := http.NewServeMux()

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

	// WebSocket endpoint with auth middleware
	wsHandlerFunc := http.HandlerFunc(wsHandler.HandleWebSocket)
	mux.Handle("/ws", authMW.Authenticate(wsHandlerFunc))

	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		logger:    logger,
		wsHandler: wsHandler,
	}
}

// Start starts the HTTP server and blocks until ctx is cancelled.
//
// Graceful Shutdown (30 second timeout):
//   - Allows in-flight requests to complete
//   - WebSocket connections can close cleanly
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

// WSHandler returns the WebSocket handler (for testing).
func (s *Server) WSHandler() *websocket.Handler {
	return s.wsHandler
}
