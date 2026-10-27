// Package gateway is the public entry point of the ava gateway.
// Everything under gateway/internal is invisible outside this subtree; this
// package is the only importable surface.
package gateway

import (
	"net/http"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

// Config is the gateway's runtime configuration.
type Config struct {
	DBPath string
}

// New builds the gateway WebSocket handler. The gateway is a single endpoint:
// the caller owns the path it is mounted on.
func New(cfg *Config, logger zerolog.Logger) (http.Handler, error) {
	return handleWS(logger), nil
}

// handleWS is the WebSocket placeholder: accepts a connection, greets with a
// hello frame, then closes. Real-time agent traffic lands here later.
func handleWS(logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			logger.Warn().Err(err).Msg("ws accept failed")
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"hello"}`))
	}
}
