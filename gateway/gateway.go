// Package gateway is the public entry point of the ava gateway.
// Everything under gateway/internal is invisible outside this subtree; this
// package is the only importable surface.
//
// It implements the client wire contract: connected event, prompt
// routing into agent lanes, lane_status reply.
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

	"github.com/radch-ai/ava/gateway/internal/agent"
	"github.com/radch-ai/ava/gateway/wstypes"
)

const (
	// pingInterval is how often the gateway proves each connection is
	// alive. 30s sits below common NAT/proxy idle timeouts so half-open
	// TCP never goes unnoticed; browser clients answer pings in-band.
	pingInterval = 30 * time.Second
	// pongTimeout caps one ping: no pong in time means the peer is gone.
	// Tests with shrunken intervals scale the wait down from this bound.
	pongTimeout = 10 * time.Second
)

// Config is the gateway's runtime configuration.
type Config struct {
	// PingInterval overrides pingInterval. Zero keeps the default; the
	// field exists for tests to shrink the wait.
	PingInterval time.Duration
}

// New builds the gateway WebSocket handler. The gateway is a single
// endpoint: the caller owns the path it is mounted on.
func New(cfg *Config, logger zerolog.Logger) (http.Handler, error) {
	interval := pingInterval
	if cfg != nil && cfg.PingInterval > 0 {
		interval = cfg.PingInterval
	}
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})
	return handleWS(interval, logger, mgr), nil
}

// handleWS serves the gateway WebSocket endpoint.
//
// Lifecycle for a self-hosted, single-user gateway: reads have no
// deadline (quiet devices are the normal state); a ping ticker proves
// liveness and reaps half-open peers; cancellation of the request
// context (client gone or server draining) ends the handler.
func handleWS(interval time.Duration, logger zerolog.Logger, mgr *agent.UserAgentManager) http.HandlerFunc {
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

		go keepalive(ctx, conn, interval, logger)

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
			err := wsjson.Read(ctx, conn, &raw)
			if err != nil {
				if ctx.Err() != nil {
					// Context cancelled: client vanished or the server is
					// draining. coder/websocket already tore the connection
					// down (the peer sees EOF); cancelling this context also
					// ends any agent lane bound to it.
					logger.Info().Msg("closing for shutdown")
					return
				}
				if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
					websocket.CloseStatus(err) == websocket.StatusGoingAway {
					logger.Info().Msg("connection closed")
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
				payload, err := json.Marshal(env.Data)
				if err != nil {
					logger.Warn().Err(err).Msg("malformed prompt message")
					continue
				}
				var msg wstypes.PromptMessage
				if err := json.Unmarshal(payload, &msg); err != nil {
					logger.Warn().Err(err).Msg("malformed prompt message")
					continue
				}
				// Route into the lane: first submit creates it, every
				// submit adds this connection and queues the message.
				logger.Info().
					Str("agent_id", env.AgentID).
					Str("model", msg.Model).
					Int("content_len", len(msg.Content)).
					Msg("prompt received over ws")
				mgr.SubmitMsg(ctx, env.AgentID, connID, conn, agent.Message{
					ID:      uuid.New().String(),
					Content: msg.Content,
					Model:   msg.Model,
				})
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
				// No interruptible work runs on the lanes.
			default:
				logger.Debug().Str("type", string(env.Type)).Msg("unknown message type")
			}
		}
	}
}

// keepalive pings the peer on a ticker so half-open connections (sleep,
// NAT drop) are reaped instead of lingering. A pong only reaches the
// ping while the read loop is running — coder/websocket delivers
// control frames through reads — so this only works together with it.
func keepalive(ctx context.Context, conn *websocket.Conn, interval time.Duration, logger zerolog.Logger) {
	pongWait := interval * 10
	if pongWait > pongTimeout {
		pongWait = pongTimeout
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, pongWait)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				logger.Info().Err(err).Msg("keepalive failed, closing")
				_ = conn.Close(websocket.StatusGoingAway, "keepalive failed")
				return
			}
		}
	}
}
