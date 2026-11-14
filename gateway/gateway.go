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
	// PersistTurn appends a turn's messages to the agent's thread —
	// bound to backend.AppendThreadMessages by the binary at boot.
	// Zero keeps turns ephemeral.
	PersistTurn agent.TurnSink
	// GetAgentConfig reads an agent's config for each turn's prompt —
	// bound to backend.GetAgentConfig by the binary at boot. Zero
	// keeps turns persona-less.
	GetAgentConfig agent.AgentConfigSource
}

// New builds the gateway. The gateway is a single WebSocket endpoint:
// the caller owns the path it is mounted on, and Drain is the
// shutdown half — call it after the HTTP server has drained.
func New(cfg *Config, logger zerolog.Logger) (*Gateway, error) {
	interval := pingInterval
	resolve := agent.ModelResolver(nil)
	var persist agent.TurnSink
	var getConfig agent.AgentConfigSource
	if cfg != nil {
		if cfg.PingInterval > 0 {
			interval = cfg.PingInterval
		}
		resolve = cfg.ResolveModel
		persist = cfg.PersistTurn
		getConfig = cfg.GetAgentConfig
	}
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{
		ResolveModel:   resolve,
		PersistTurn:    persist,
		GetAgentConfig: getConfig,
	})
	return &Gateway{
		handler: handleWS(interval, logger, mgr),
		mgr:     mgr,
	}, nil
}

// Gateway is the mounted WebSocket endpoint plus its drain half.
type Gateway struct {
	handler http.Handler
	mgr     *agent.UserAgentManager
}

// ServeHTTP serves the gateway WebSocket endpoint.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.handler.ServeHTTP(w, r)
}

// Drain interrupts every lane's running turn and waits, bounded by
// the timeout, for the interrupted turns to settle into their
// threads. Call it after the HTTP server has closed its
// connections.
func (g *Gateway) Drain(timeout time.Duration) {
	g.mgr.Drain(timeout)
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

		// Shutdown watcher: cancelling the request context is the
		// server draining. Close politely with GoingAway — the frame
		// must go out before anything lets a cancelled context touch
		// the connection, because coder/websocket aborts the
		// connection outright in that case. The close also unblocks
		// the read loop below.
		go func() {
			<-ctx.Done()
			_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
		}()

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

		// Reads run on a cancel-proof context: coder/websocket aborts
		// the whole connection when a read's context cancels, which
		// would race the shutdown watcher's polite GoingAway close.
		// The watcher's close — or the peer's own disconnect — is
		// what unblocks reads.
		readCtx := context.WithoutCancel(ctx)
		for {
			var raw json.RawMessage
			err := wsjson.Read(readCtx, conn, &raw)
			if err != nil {
				if ctx.Err() != nil {
					// Server drain: the shutdown watcher already
					// said goodbye with GoingAway.
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
				// Reply first, then join: a status request doubles
				// as the lane join, so a page (re)opened mid-turn
				// receives the streamed prefix and everything after
				// it, instead of a frozen working indicator. Joining
				// a lane-less agent is a no-op — the reply above
				// already reports it idle.
				writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
				err := wsjson.Write(writeCtx, conn, wstypes.Event{
					Type:    wstypes.EventTypeLaneStatus,
					AgentID: env.AgentID,
					Data:    mgr.Get(env.AgentID).LaneStatus(),
				})
				writeCancel()
				if err != nil {
					logger.Warn().Err(err).Msg("failed to send lane_status event")
					return
				}
				joinedAgent = env.AgentID
				mgr.Join(env.AgentID, connID, conn)
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
