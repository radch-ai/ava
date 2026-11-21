// Package gateway is the public entry point of the ava gateway.
// Everything under gateway/internal is invisible outside this subtree; this
// package is the only importable surface.
//
// It implements the client wire contract: the connected greeting, the
// synchronization handshake that replays what a device missed, prompt
// routing into agent lanes, and interrupt.
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

	"github.com/radch-ai/ava/backend"
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
	// Journal is the durable conversation lifecycle feed — bound to the
	// backend TurnService by the binary at boot. It admits, settles, and
	// replays items; zero fails every admission closed.
	Journal agent.LifecycleJournal
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
}

// New builds the gateway. The gateway is a single WebSocket endpoint:
// the caller owns the path it is mounted on, and Drain is the
// shutdown half — call it after the HTTP server has drained.
func New(cfg *Config, logger zerolog.Logger) (*Gateway, error) {
	interval := pingInterval
	resolve := agent.ModelResolver(nil)
	var journal agent.LifecycleJournal
	var getConfig agent.AgentConfigSource
	var toolsFn agent.AgentToolsFunc
	var getThread agent.AgentThreadSource
	if cfg != nil {
		if cfg.PingInterval > 0 {
			interval = cfg.PingInterval
		}
		resolve = cfg.ResolveModel
		journal = cfg.Journal
		getConfig = cfg.GetAgentConfig
		toolsFn = cfg.Tools
		getThread = cfg.GetAgentThread
	}
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{
		ResolveModel:   resolve,
		Journal:        journal,
		GetAgentConfig: getConfig,
		Tools:          toolsFn,
		GetAgentThread: getThread,
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
		// or accept it. JSON escaping costs up to 6 bytes per content
		// byte, so the decoded bound needs its worst-case framing on
		// top. Over-length frames still reach the handler, whose
		// validation answers with queue_result.
		conn.SetReadLimit(6*maxPromptBytes + 8192)

		connID := uuid.New().String()
		logger = logger.With().Str("conn_id", connID).Logger()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// The first frame fixes which lane this connection belongs to;
		// leaving removes the connection from it.
		var joinedAgent string
		// synced gates every mutation: a peer that has not finished
		// recovery cannot submit or interrupt, because it does not yet
		// know the conversation it would act on.
		var synced bool
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

		// syncAgent advances one synchronization step for the bound
		// agent: a bounded replay batch after the client's cursor, the
		// authoritative baseline when that cursor cannot resume, or the
		// runtime snapshot plus a live subscription once the head is
		// reached. One frame per request keeps recovery bounded no
		// matter how far behind the device is. false means the
		// connection is gone.
		syncAgent := func(agentID string, msg wstypes.SyncMessage) bool {
			journal := mgr.Journal()
			if journal == nil {
				logger.Warn().Str("agent_id", agentID).Msg("sync without a lifecycle journal")
				return true
			}
			write := func(ev wstypes.Event) bool {
				writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				if err := wsjson.Write(writeCtx, conn, ev); err != nil {
					logger.Warn().Err(err).Msg("failed to send sync event")
					return false
				}
				return true
			}
			// failSync answers one failed synchronization step and closes
			// the connection: a peer never hangs half-synchronized on a
			// socket the server cannot advance.
			failSync := func(step string, err error) bool {
				logger.Warn().Err(err).Str("agent_id", agentID).Msg(step)
				ok := write(wstypes.Event{
					Type:    wstypes.EventTypeError,
					AgentID: agentID,
					Data:    wstypes.ErrorEvent{Message: "sync failed: " + err.Error()},
				})
				if ok {
					_ = conn.Close(websocket.StatusInternalError, "sync failed")
				}
				return false
			}
			// sendSnapshot answers the authoritative baseline: the active
			// thread's newest page at the committed head. A client applies
			// it, then continues from its cursor. A snapshot that cannot be
			// read is never silence: the peer learns the failure and the
			// connection closes, so it cannot hang half-synchronized.
			sendSnapshot := func() bool {
				snapshot, err := journal.SyncSnapshot(readCtx, agentID, snapshotPageLimit)
				if err != nil {
					return failSync("sync snapshot", err)
				}
				page, err := json.Marshal(snapshot.Page)
				if err != nil {
					return failSync("marshal sync page", err)
				}
				return write(wstypes.Event{
					Type:    wstypes.EventTypeSyncSnapshot,
					AgentID: agentID,
					Data: wstypes.SyncSnapshotEvent{
						Cursor: snapshot.Cursor,
						Page:   page,
					},
				})
			}
			// Shape first: an unparseable cursor recovers from a snapshot.
			historyID, pos, ok := backend.ParseCursor(msg.Cursor)
			if !ok {
				return sendSnapshot()
			}
			// Replay validates scope, coordinates, and events in one
			// read: a cursor from another database or projection never
			// resumes here, and its positions describe history this
			// database cannot serve.
			page, err := journal.ReplayAfter(readCtx, agentID, historyID, pos, replayBatchSize)
			if err != nil {
				if errors.Is(err, backend.ErrReplayExpired) || errors.Is(err, backend.ErrReplayAhead) ||
					errors.Is(err, backend.ErrReplayForeign) {
					return sendSnapshot()
				}
				return failSync("replay journal", err)
			}
			if len(page.Events) > 0 {
				return write(wstypes.Event{
					Type:    wstypes.EventTypeSyncBatch,
					AgentID: agentID,
					Data: wstypes.LifecycleBatchEvent{
						Cursor: backend.FormatCursor(historyID, page.LastPos),
						Events: page.Events,
					},
				})
			}

			// Caught up: the manager registers the connection under
			// the publication boundary, claiming readiness only while
			// the head still matches this position. Any mismatch means
			// the empty page that led here cannot advance the client,
			// so the connection closes without sync_ready and the
			// device recovers through a fresh sync instead of spinning
			// here.
			registered, head, err := mgr.HandOff(readCtx, agentID, connID, conn, pos)
			if err != nil {
				return failSync("sync hand-off", err)
			}
			if !registered {
				logger.Warn().Str("agent_id", agentID).Int64("pos", pos).Int64("head", head).
					Msg("replay stalled: empty page below head")
				return false
			}
			synced = true
			return true
		}

		// answer replies to one prompt or send_now on the requesting
		// connection; false means the reply itself failed, so the
		// handler ends — nothing else may ride a socket that cannot
		// answer its own admission frames.
		answer := func(agentID, requestID, itemID string, queueErr error) bool {
			if rerr := replyQueueResult(ctx, conn, agentID, requestID, itemID, queueErr); rerr != nil {
				logger.Warn().Err(rerr).Msg("failed to send queue_result event")
				return false
			}
			return true
		}
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
				if !synced {
					logger.Warn().Str("agent_id", env.AgentID).Msg("prompt before synchronization")
					if !answer(env.AgentID, msg.RequestID, "", errNotSynchronized) {
						return
					}
					continue
				}
				// Admission validates content before anything binds or
				// mutates: empty-after-trim and over-length prompts are
				// answered through queue_result and dropped.
				content := strings.TrimSpace(msg.Content)
				if reject := validatePromptContent(msg.Content, content); reject != nil {
					logger.Warn().Err(reject).Str("agent_id", env.AgentID).Msg("prompt rejected")
					if !answer(env.AgentID, msg.RequestID, "", reject) {
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
					if !answer(env.AgentID, msg.RequestID, "", reject) {
						return
					}
					continue
				}
				if !bindAgent(&joinedAgent, env.AgentID) {
					logger.Warn().Str("agent_id", env.AgentID).Msg("connection already bound to another agent")
					continue
				}
				// Admit into the agent's queue: the journal commits the
				// acceptance before the runtime queue exposes it, and
				// the accepted event rides the lifecycle feed. A nil
				// model follows the default routing.
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
				itemID, err := mgr.Submit(env.AgentID, content, entry, kind)
				if !answer(env.AgentID, msg.RequestID, itemID, err) {
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
				if !synced {
					logger.Warn().Str("agent_id", env.AgentID).Msg("send_now before synchronization")
					if !answer(env.AgentID, msg.RequestID, "", errNotSynchronized) {
						return
					}
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
				if !answer(env.AgentID, msg.RequestID, itemID, err) {
					return
				}
				if err != nil {
					logger.Warn().Err(err).Str("item_id", msg.ItemID).Msg("send_now rejected")
				}
			case wstypes.EnvelopeTypeSync:
				// One conversation per socket: the sync frame binds its
				// agent exactly like a prompt — cross-agent frames are
				// rejected, not a silent re-bind.
				var msg wstypes.SyncMessage
				if err := json.Unmarshal(env.Data, &msg); err != nil {
					logger.Warn().Err(err).Msg("malformed sync message")
					continue
				}
				if !bindAgent(&joinedAgent, env.AgentID) {
					logger.Warn().Str("agent_id", env.AgentID).Msg("connection already bound to another agent")
					continue
				}
				if !syncAgent(env.AgentID, msg) {
					return
				}
			case wstypes.EnvelopeTypeInterrupt:
				// A peer that has not synchronized does not know the turn
				// it would stop: its interrupt is ignored, not guessed at.
				if !synced {
					logger.Warn().Str("agent_id", env.AgentID).Msg("interrupt before synchronization")
					continue
				}
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
	if agentID == "" {
		return false
	}
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
	// errNotSynchronized answers a mutation from a peer that has not
	// finished recovery: it cannot act on a conversation it has not
	// yet seen.
	errNotSynchronized = errors.New("not synchronized; retry once the connection is live")
)

// Recovery bounds: one replay page and one snapshot page. A device that
// fell further behind asks again with its cursor, so neither is a
// promise about how much history a reconnect costs.
const (
	replayBatchSize   = 100
	snapshotPageLimit = 20
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
