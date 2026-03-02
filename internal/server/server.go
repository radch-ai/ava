package server

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/rs/zerolog"
)

// Server manages the HTTP server and WebSocket handlers.
type Server struct {
	httpServer *http.Server
	logger     zerolog.Logger
	wsHandler  *websocket.Handler
	shutdownCh chan struct{} // Closed when shutdown is requested
}

// New creates a new Server with auth middleware.
// The introspector is required for validating session tokens.
func New(addr string, logger zerolog.Logger, wsHandler *websocket.Handler, introspector *ZitadelIntrospector) *Server {
	authMW := NewAuthMiddleware(introspector)

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET and HEAD
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
		logger:     logger,
		wsHandler:  wsHandler,
		shutdownCh: make(chan struct{}),
	}
}

// Start starts the HTTP server and handles graceful shutdown.
//
// Graceful Shutdown (30 second timeout):
//   - Allows in-flight requests to complete
//   - WebSocket connections can close cleanly
//   - Not so long as to delay deployments
func (s *Server) Start() error {
	s.logger.Info().
		Str("service", "nevaria-gateway").
		Str("addr", s.httpServer.Addr).
		Msg("server starting")

	// Channel to capture server startup errors
	errCh := make(chan error, 1)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		// Server failed to start
		s.logger.Error().Err(err).Msg("server failed to start")
		return err
	case sig := <-sigChan:
		s.logger.Info().Str("signal", sig.String()).Msg("shutdown signal received")
	case <-s.shutdownCh:
		s.logger.Info().Msg("shutdown requested")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger.Error().Err(err).Msg("shutdown error")
		return err
	}

	s.logger.Info().Msg("server stopped gracefully")
	return nil
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	// Signal to Start() that shutdown is requested
	close(s.shutdownCh)
	return s.httpServer.Shutdown(ctx)
}

// HTTPServer returns the underlying HTTP server (for testing purposes).
func (s *Server) HTTPServer() *http.Server {
	return s.httpServer
}

// WSHandler returns the WebSocket handler (for testing purposes).
func (s *Server) WSHandler() *websocket.Handler {
	return s.wsHandler
}
