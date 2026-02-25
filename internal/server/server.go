package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/rs/zerolog"
)

// Version is set at build time via ldflags
var Version = "dev"

// A Server manages the HTTP server and WebSocket handlers.
type Server struct {
	httpServer     *http.Server
	logger         zerolog.Logger
	wsHandler      *websocket.Handler
	authMW         *AuthMiddleware // Authentication middleware
	internalRouter http.Handler    // Internal router mounted at /_internal/
	shutdownCh     chan struct{}   // Closed when shutdown is requested
}

// New creates a new Server with the given address, logger, WebSocket handler, auth middleware, and internal router.
// If authMW is nil, the WebSocket endpoint will not require authentication (for backward compatibility).
func New(addr string, logger zerolog.Logger, wsHandler *websocket.Handler, authMW *AuthMiddleware, internalRouter http.Handler) *Server {

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

	// WebSocket endpoint - wrap with auth middleware if provided
	wsHandlerFunc := http.HandlerFunc(wsHandler.HandleWebSocket)
	if authMW != nil {
		mux.Handle("/ws", authMW.Authenticate(wsHandlerFunc))
	} else {
		mux.Handle("/ws", wsHandlerFunc)
	}

	// Mount internal router at /_internal/
	if internalRouter != nil {
		mux.Handle("/_internal/", http.StripPrefix("/_internal/", internalRouter))
	}

	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		logger:         logger,
		wsHandler:      wsHandler,
		authMW:         authMW,
		internalRouter: internalRouter,
		shutdownCh:     make(chan struct{}),
	}
}

func (s *Server) Start() error {
	s.logger.Info().
		Str("service", "nevaria-gateway").
		Str("version", Version).
		Str("addr", s.httpServer.Addr).
		Msg("server starting")

	// Channel to capture server startup errors
	errCh := make(chan error, 1)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("server error: %w", err)
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
		return fmt.Errorf("shutdown failed: %w", err)
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
