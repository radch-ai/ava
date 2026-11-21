package agent

// Publication-order pins: a durable mutation and the lifecycle frame
// carrying it share one publication section, so frames reach a
// subscriber in commit order — never in the order a write happened to
// finish, and never with a running snapshot ahead of its own start.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/wstypes"
)

// testWSPair returns the two ends of one accepted connection: frames
// written to the server end arrive on the client end in order.
func testWSPair(t *testing.T) (client, server *websocket.Conn) {
	t.Helper()
	serverCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		serverCh <- c
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, srv.URL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.CloseNow() })

	select {
	case server = <-serverCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server end never accepted")
	}
	return client, server
}

// readPublishedFrame decodes the next frame off a subscribed connection.
func readPublishedFrame(t *testing.T, c *websocket.Conn) wstypes.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var ev wstypes.Event
	require.NoError(t, wsjson.Read(ctx, c, &ev))
	return ev
}

// Concurrent mutations publish in commit order: positions are minted
// inside the boundary, so a subscriber always receives strictly
// increasing positions even when the writes finish out of order.
func TestCommitAndPublishKeepsCursorOrder(t *testing.T) {
	s := newAgentState("agent-order", context.Background(), &sync.WaitGroup{}, zerolog.Nop())
	client, server := testWSPair(t)
	s.conns.Add("conn-1", server)

	var pos atomic.Int64
	const writes = 40
	var wg sync.WaitGroup
	errs := make(chan error, writes)
	for i := range writes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.commitAndPublish(func() (*backend.JournalBatch, error) {
				// Uneven commit cost: without the boundary, a later
				// write would routinely overtake an earlier one.
				time.Sleep(time.Duration(i%4) * time.Millisecond)
				p := pos.Add(1)
				return &backend.JournalBatch{
					Cursor: backend.FormatCursor("history", p),
					Events: []apitypes.LifecycleEvent{{
						Kind:   apitypes.LifecycleItemAccepted,
						ItemID: "item",
						Pos:    p,
					}},
				}, nil
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var last int64
	for range writes {
		ev := readPublishedFrame(t, client)
		require.Equal(t, wstypes.EventTypeLifecycle, ev.Type)
		batch := eventPayloadLocal[wstypes.LifecycleBatchEvent](t, ev)
		require.Len(t, batch.Events, 1)
		var lifecycle apitypes.LifecycleEvent
		require.NoError(t, json.Unmarshal(batch.Events[0], &lifecycle))
		require.Greater(t, lifecycle.Pos, last, "published positions must follow commit order")
		last = lifecycle.Pos
	}
}

// Starting an item publishes its committed batch first and the running
// snapshot second: a client never observes an active item whose durable
// start it has not applied.
func TestStartItemPublishesBatchBeforeSnapshot(t *testing.T) {
	s := newAgentState("agent-start-order", context.Background(), &sync.WaitGroup{}, zerolog.Nop())
	client, server := testWSPair(t)
	s.conns.Add("conn-1", server)

	item, err := testAdmit(s, "A", nil, wstypes.QueueItemKindPrompt)
	require.NoError(t, err)
	// The admission snapshot is already out; the claim publishes nothing.
	require.Equal(t, wstypes.EventTypeQueueState, readPublishedFrame(t, client).Type)
	claim := s.claim()
	require.NotNil(t, claim)

	err = s.startItem(func() (*backend.ItemStart, *backend.JournalBatch, error) {
		return &backend.ItemStart{UserMessageID: "user-row-1"},
			&backend.JournalBatch{
				Cursor: backend.FormatCursor("history", 1),
				Events: []apitypes.LifecycleEvent{{Kind: apitypes.LifecycleItemStarted, ItemID: item.id, Pos: 1}},
			}, nil
	})
	require.NoError(t, err)

	first := readPublishedFrame(t, client)
	require.Equal(t, wstypes.EventTypeLifecycle, first.Type,
		"the committed start must precede the running projection, got %v", first.Type)

	second := readPublishedFrame(t, client)
	require.Equal(t, wstypes.EventTypeQueueState, second.Type)
	running := eventPayloadLocal[wstypes.QueueStateEvent](t, second)
	require.NotNil(t, running.Active)
	require.Equal(t, item.id, running.Active.ID)
	require.Equal(t, "user-row-1", running.Active.UserMessageID)
}

// eventPayloadLocal decodes an event's Data into the payload type.
func eventPayloadLocal[T any](t *testing.T, ev wstypes.Event) T {
	t.Helper()
	raw, err := json.Marshal(ev.Data)
	require.NoError(t, err)
	var p T
	require.NoError(t, json.Unmarshal(raw, &p))
	return p
}
