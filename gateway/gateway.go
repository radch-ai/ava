// Package gateway is the public entry point of the ava gateway.
// Everything under gateway/internal is invisible outside this subtree; this
// package is the only importable surface.
//
// Build-out proceeds top-down; this surface accepts a WebSocket,
// confirms the connection, and distinguishes client message types.
package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/gateway/wstypes"
)

// Config is the gateway's runtime configuration. Fields arrive as the
// wiring phases need them.
type Config struct{}

// New builds the gateway WebSocket handler. The gateway is a single
// endpoint: the caller owns the path it is mounted on.
func New(cfg *Config, logger zerolog.Logger) (http.Handler, error) {
	return handleWS(logger), nil
}

// handleWS serves the gateway WebSocket endpoint.
func handleWS(logger zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			logger.Warn().Err(err).Msg("ws accept failed")
			return
		}
		defer func() { _ = conn.CloseNow() }()

		connID := uuid.New().String()
		logger = logger.With().Str("conn_id", connID).Logger()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// The connected event carries the connection ID.
		writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
		err = wsjson.Write(writeCtx, conn, wstypes.Event{
			Type: wstypes.EventTypeConnected,
			Data: wstypes.ConnectedEvent{ConnID: connID},
		})
		writeCancel()
		if err != nil {
			logger.Warn().Err(err).Msg("failed to send connected event")
			return
		}

		for {
			var raw json.RawMessage
			readCtx, readCancel := context.WithTimeout(ctx, 60*time.Second)
			err := wsjson.Read(readCtx, conn, &raw)
			readCancel()
			if err != nil {
				if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
					websocket.CloseStatus(err) == websocket.StatusGoingAway {
					logger.Info().Msg("client closed connection")
				} else {
					logger.Warn().Err(err).Msg("read error")
				}
				return
			}

			// The envelope head picks the arm; the payload rides in Data.
			var env wstypes.Envelope
			if err := json.Unmarshal(raw, &env); err != nil {
				logger.Warn().Err(err).Msg("unparseable message")
				continue
			}

			switch env.Type {
			case wstypes.EnvelopeTypePrompt:
				// Bare scaffold: the prompt arm completes with the
				// boundary-crossing commit.
			case wstypes.EnvelopeTypeStatus:
				writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
				err := wsjson.Write(writeCtx, conn, wstypes.Event{
					Type:    wstypes.EventTypeLaneStatus,
					AgentID: env.AgentID,
					Data:    wstypes.LaneStatus{ConnCount: 1},
				})
				writeCancel()
				if err != nil {
					logger.Warn().Err(err).Msg("failed to send lane_status event")
					return
				}
			case wstypes.EnvelopeTypeInterrupt:
				// Nothing runs behind the connection yet to interrupt.
			default:
				logger.Debug().Str("type", string(env.Type)).Msg("unknown message type")
			}
		}
	}
}
