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
	"errors"
	"net/http"
	"strings"
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
	// to TurnService.ResolveModel by the binary at boot. Zero keeps the
	// lanes unresolved: turns fail closed.
	ResolveModel agent.ModelResolver
	// PersistTurn appends a turn's messages to the agent's thread —
	// bound to TurnService.AppendThreadMessages by the binary at boot.
	// Zero keeps turns ephemeral.
	PersistTurn agent.TurnSink
	// GetAgentConfig reads an agent's config for each turn's prompt —
	// bound to TurnService.GetAgentConfig by the binary at boot. Zero
	// keeps turns persona-less.
	GetAgentConfig agent.AgentConfigSource
	// Tools fetches an agent's effective tools for each turn — bound
	// to TurnService.AgentTools by the binary at boot. Zero keeps
	// turns tool-less.
	Tools agent.AgentToolsFunc
	// GetAgentThread reads the agent's active thread as the turn's
	// model input — bound to TurnService.GetAgentThread by the binary
	// at boot. Required: the working context is the committed user
	// row, so an unbound source fails the turn.
	GetAgentThread agent.AgentThreadSource
	// CreateThread runs /new and /reset — bound to
	// TurnService.CreateAndActivateThread by the binary at boot. Zero
	// fails commands closed.
	CreateThread agent.ThreadActivator
}

// New builds the gateway. The gateway is a single WebSocket endpoint:
// the caller owns the path it is mounted on, and Drain is the
// shutdown half — call it after the HTTP server has drained.
func New(cfg *Config, logger zerolog.Logger) (*Gateway, error) {
	interval := pingInterval
	resolve := agent.ModelResolver(nil)
	var persist agent.TurnSink
	var getConfig agent.AgentConfigSource
	var toolsFn agent.AgentToolsFunc
	var getThread agent.AgentThreadSource
	var createThread agent.ThreadActivator
	if cfg != nil {
		if cfg.PingInterval > 0 {
			interval = cfg.PingInterval
		}
		resolve = cfg.ResolveModel
		persist = cfg.PersistTurn
		getConfig = cfg.GetAgentConfig
		toolsFn = cfg.Tools
		getThread = cfg.GetAgentThread
		createThread = cfg.CreateThread
	}
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{
		ResolveModel:   resolve,
		PersistTurn:    persist,
		GetAgentConfig: getConfig,
		Tools:          toolsFn,
		GetAgentThread: getThread,
		CreateThread:   createThread,
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

		// Inbound frames may carry a full prompt up to the admission
		// bound; the default 32 KiB read limit would close the socket
		// on a legitimate long prompt before admission could reject
		// or accept it. Over-length frames reach the handler, whose
		// validation answers with queue_result.
		conn.SetReadLimit(maxPromptBytes + 8192)

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
			mgr.RemoveConn(joinedAgent, connID)
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
				var msg wstypes.PromptMessage
				if err := json.Unmarshal(env.Data, &msg); err != nil {
					logger.Warn().Err(err).Msg("malformed prompt message")
					continue
				}
				// Admission validates content before anything binds or
				// mutates: empty-after-trim and over-length prompts are
				// answered through queue_result and dropped.
				content := strings.TrimSpace(msg.Content)
				if reject := validatePromptContent(msg.Content, content); reject != nil {
					logger.Warn().Err(reject).Str("agent_id", env.AgentID).Msg("prompt rejected")
					if rerr := replyQueueResult(ctx, conn, env.AgentID, msg.RequestID, "", reject); rerr != nil {
						logger.Warn().Err(rerr).Msg("failed to send queue_result event")
						return
					}
					continue
				}
				// A command is a queue item like any other, so it is
				// admitted behind existing work and reported the same
				// way; only its execution differs.
				kind, reject := classifySubmission(content)
				if reject != nil {
					logger.Warn().Err(reject).Str("agent_id", env.AgentID).Msg("command rejected")
					if rerr := replyQueueResult(ctx, conn, env.AgentID, msg.RequestID, "", reject); rerr != nil {
						logger.Warn().Err(rerr).Msg("failed to send queue_result event")
						return
					}
					continue
				}
				if !bindAgent(&joinedAgent, env.AgentID) {
					logger.Warn().Str("agent_id", env.AgentID).Msg("connection already bound to another agent")
					continue
				}
				// Enqueue into the agent's queue: first submit creates
				// the state, every submit subscribes this connection.
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
					Int("content_len", len(content)).
					Msg("prompt received over ws")
				itemID, err := mgr.Submit(env.AgentID, connID, conn, content, entry, kind)
				if rerr := replyQueueResult(ctx, conn, env.AgentID, msg.RequestID, itemID, err); rerr != nil {
					logger.Warn().Err(rerr).Msg("failed to send queue_result event")
					return
				}
				if err != nil {
					logger.Warn().Err(err).Str("agent_id", env.AgentID).Msg("queue admission rejected")
				}
			case wstypes.EnvelopeTypeSendNow:
				var msg wstypes.SendNowMessage
				if err := json.Unmarshal(env.Data, &msg); err != nil {
					logger.Warn().Err(err).Msg("malformed send_now message")
					continue
				}
				if !bindAgent(&joinedAgent, env.AgentID) {
					logger.Warn().Str("agent_id", env.AgentID).Msg("connection already bound to another agent")
					continue
				}
				var entry *apitypes.ModelEntry
				if msg.Model != nil {
					entry = &apitypes.ModelEntry{ID: msg.Model.ID, ProviderID: msg.Model.ProviderID}
				}
				itemID, err := mgr.SendNow(env.AgentID, msg.ItemID, entry)
				if rerr := replyQueueResult(ctx, conn, env.AgentID, msg.RequestID, itemID, err); rerr != nil {
					logger.Warn().Err(rerr).Msg("failed to send queue_result event")
					return
				}
				if err != nil {
					logger.Warn().Err(err).Str("item_id", msg.ItemID).Msg("send_now rejected")
				}
			case wstypes.EnvelopeTypeStatus:
				// One conversation per socket: the status frame binds its
				// agent exactly like a prompt — cross-agent frames are
				// rejected, not a silent re-bind. Reply first, then join:
				// a status request doubles as the lane join, so a page
				// (re)opened mid-turn receives the queue snapshot, the
				// streamed prefix, and everything after them. Joining an
				// agent without a worker still subscribes — pending work
				// survives the worker.
				if !bindAgent(&joinedAgent, env.AgentID) {
					logger.Warn().Str("agent_id", env.AgentID).Msg("connection already bound to another agent")
					continue
				}
				writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
				err := wsjson.Write(writeCtx, conn, wstypes.Event{
					Type:    wstypes.EventTypeLaneStatus,
					AgentID: env.AgentID,
					Data:    mgr.Status(env.AgentID),
				})
				writeCancel()
				if err != nil {
					logger.Warn().Err(err).Msg("failed to send lane_status event")
					return
				}
				mgr.Join(env.AgentID, connID, conn)
			case wstypes.EnvelopeTypeInterrupt:
				// The stop frame binds like every other frame: a socket
				// bound to one agent must not stop another agent's turn.
				if !bindAgent(&joinedAgent, env.AgentID) {
					logger.Warn().Str("agent_id", env.AgentID).Msg("connection already bound to another agent")
					continue
				}
				mgr.Interrupt(env.AgentID)
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

// bindAgent fixes the connection's agent on first use and rejects
// cross-agent frames afterwards: one conversation per socket.
func bindAgent(joined *string, agentID string) bool {
	if *joined == "" {
		*joined = agentID
		return true
	}
	return *joined == agentID
}

// maxPromptBytes bounds one prompt's raw content length.
const maxPromptBytes = 64 << 10

// Prompt admission rejections; user-facing text rides queue_result,
// and no queue state changes on rejection.
var (
	errPromptEmpty   = errors.New("prompt is empty")
	errPromptTooLong = errors.New("prompt is too long")
	// errCommandArguments rejects a recognized command carrying extra
	// text: /new is the whole submission or it is an ordinary prompt.
	errCommandArguments = errors.New("/new takes no arguments; submit /new alone")
)

// classifySubmission splits a trimmed submission into its queue kind:
// a recognized command, an argument rejection, or an ordinary prompt —
// including slash text that names no command.
func classifySubmission(trimmed string) (kind string, reject error) {
	if !strings.HasPrefix(trimmed, "/") {
		return wstypes.QueueItemKindPrompt, nil
	}
	cmd := trimmed
	if i := strings.IndexAny(cmd, " \t\n\r"); i >= 0 {
		cmd = cmd[:i]
	}
	switch cmd {
	case "/new", "/reset":
		if cmd != trimmed {
			// The kind is unused when reject != nil; the prompt
			// spelling keeps one encoding in the package.
			return wstypes.QueueItemKindPrompt, errCommandArguments
		}
		return wstypes.QueueItemKindNewThread, nil
	default:
		return wstypes.QueueItemKindPrompt, nil
	}
}

// validatePromptContent classifies raw and trimmed prompt content:
// raw length is bounded, trimmed emptiness rejected.
func validatePromptContent(raw, trimmed string) error {
	if len(raw) > maxPromptBytes {
		return errPromptTooLong
	}
	if trimmed == "" {
		return errPromptEmpty
	}
	return nil
}

// replyQueueResult answers one prompt or send_now frame on the
// requesting connection. Admission rejections carry a user-facing
// reason and change no queue state.
func replyQueueResult(ctx context.Context, conn *websocket.Conn, agentID, requestID, itemID string, queueErr error) error {
	ev := wstypes.QueueResultEvent{RequestID: requestID}
	if queueErr != nil {
		ev.Error = queueErr.Error()
	} else {
		ev.Accepted = true
		ev.ItemID = itemID
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return wsjson.Write(writeCtx, conn, wstypes.Event{
		Type:    wstypes.EventTypeQueueResult,
		AgentID: agentID,
		Data:    ev,
	})
}
