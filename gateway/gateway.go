// Package gateway is the public entry point of the ava gateway.
// Everything under gateway/internal is invisible outside this subtree; this
// package is the only importable surface.
//
// It implements the client wire contract: connected event, prompt
// routing into agent lanes, turn message and error events,
// lane_status reply, interrupt.
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

	"github.com/radch-ai/ava/backend/apitypes"
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
	// ResolveModel maps a selection to a callable endpoint — bound
	// to backend.ResolveModel by the binary at boot. Zero keeps the
	// lanes unresolved: turns fail closed.
	ResolveModel agent.ModelResolver
}

// New builds the gateway WebSocket handler. The gateway is a single
// endpoint: the caller owns the path it is mounted on.
func New(cfg *Config, logger zerolog.Logger) (http.Handler, error) {
	interval := pingInterval
	resolve := agent.ModelResolver(nil)
	if cfg != nil {
		if cfg.PingInterval > 0 {
			interval = cfg.PingInterval
		}
		resolve = cfg.ResolveModel
	}
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{
		ResolveModel: resolve,
	})
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

		// The first prompt fixes which lane this connection belongs to;
		// leaving removes the connection from it.
		var joinedAgent string
		defer func() {
			if joinedAgent == "" {
				return
			}
			if ag := mgr.Get(joinedAgent); ag != nil {
				ag.RemoveConn(connID)
			}
		}()

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
				if joinedAgent == "" {
					joinedAgent = env.AgentID
				}
				// Route into the lane: first submit creates it, every
				// submit adds this connection and queues the message.
				// A nil model follows the default routing.
				var entry *apitypes.ModelEntry
				modelID, providerID := "", ""
				if msg.Model != nil {
					entry = &apitypes.ModelEntry{ID: msg.Model.ID, ProviderID: msg.Model.ProviderID}
					modelID, providerID = entry.ID, entry.ProviderID
				}
				logger.Info().
					Str("agent_id", env.AgentID).
					Str("model", modelID).
					Str("provider_id", providerID).
					Int("content_len", len(msg.Content)).
					Msg("prompt received over ws")
				mgr.SubmitMsg(ctx, env.AgentID, connID, conn, agent.Message{
					ID:      uuid.New().String(),
					Content: msg.Content,
					Model:   entry,
				})
			case wstypes.EnvelopeTypeStatus:
				writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
				err := wsjson.Write(writeCtx, conn, wstypes.Event{
					Type:    wstypes.EventTypeLaneStatus,
					AgentID: env.AgentID,
					Data:    laneStatus(mgr.Get(env.AgentID)),
				})
				writeCancel()
				if err != nil {
					logger.Warn().Err(err).Msg("failed to send lane_status event")
					return
				}
			case wstypes.EnvelopeTypeInterrupt:
				if ag := mgr.Get(env.AgentID); ag != nil {
					ag.Interrupt()
				}
			default:
				logger.Debug().Str("type", string(env.Type)).Msg("unknown message type")
			}
		}
	}
}

// laneStatus maps a lane's status onto the wire type; a missing lane
// reports an inactive, empty status.
func laneStatus(ag *agent.UserAgent) wstypes.LaneStatus {
	if ag == nil {
		return wstypes.LaneStatus{}
	}
	s := ag.GetStatus()
	active, _ := s["active"].(bool)
	model, _ := s["model"].(string)
	queueLen, _ := s["queue_len"].(int)
	connCount, _ := s["conn_count"].(int)
	return wstypes.LaneStatus{
		Active:    active,
		Model:     model,
		QueueLen:  queueLen,
		ConnCount: connCount,
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
