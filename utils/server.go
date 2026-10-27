package utils

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// Serve runs the app handler on addr until ctx is cancelled, then drains
// in-flight requests. Server-level concerns stay here: timeouts sized for a
// self-hosted single-user deployment, graceful drain, and the /healthz
// liveness probe. The caller's handler owns every other path.
func Serve(ctx context.Context, h http.Handler, addr string, logger zerolog.Logger) error {
	mux := http.NewServeMux()

	// Health check endpoint (always unauthenticated)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			logger.Debug().Str("path", "/healthz").Str("method", r.Method).Msg("method not allowed")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Every other path belongs to the app handler
	mux.Handle("/", h)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	logger.Info().Str("addr", addr).Msg("listening")

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		logger.Info().Msg("shutdown signal received")
	case err := <-serveErr:
		return err
	}

	// Graceful drain: let in-flight requests finish, but not so long as to
	// stall a redeploy.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	logger.Info().Msg("server stopped")
	return nil
}
