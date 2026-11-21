package agent

// The admission race: a submit landing on a queue whose worker is on
// its way out must never strand an accepted item. The manager-owned
// state outlives the worker — its exit hook respawns one when work
// arrived during the exit — so every accepted prompt eventually
// executes, proved by the sink's user rows.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
)

func TestSubmitRacingWorkerExitNeverStrandsAcceptedItems(t *testing.T) {
	// One plain SSE answer per request: every turn settles, so the
	// only way a prompt goes missing is a stranded admission.
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"m\",\"choices\":[{\"index\":0,\"finish_reason\":null,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer llm.Close()
	resolver := func(context.Context, *apitypes.ModelEntry) (backend.ResolvedModel, error) {
		return backend.ResolvedModel{BaseURL: llm.URL, APIKey: "sk-test", Model: "test-model"}, nil
	}

	journal := newFakeJournal()

	// No idle timer exists anymore: the worker exits whenever the
	// queue has nothing runnable, so its exit races the senders the
	// whole way through on its own.
	m := NewUserAgentManager(zerolog.Nop(), &UserAgentManagerOptions{
		ResolveModel: resolver,
		Journal:      journal,
		GetAgentThread: func(context.Context, string) ([]backend.AgentThreadMessage, error) {
			return nil, nil
		},
	})

	s := m.state("agent-race")
	s.workerMu.Lock()
	s.worker = m.newWorker(s)
	s.workerMu.Unlock()

	const racers = 64
	accepted := make([]bool, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Submit("agent-race", fmt.Sprintf("hi-%d", i), nil, "")
			accepted[i] = err == nil
		}()
	}
	wg.Wait()

	// Every reported success must show up as a started item — its user
	// row committed; an item stranded in a dead worker's wake never
	// would.
	require.Eventually(t, func() bool {
		journal.mu.Lock()
		defer journal.mu.Unlock()
		for i := range racers {
			if !accepted[i] {
				continue
			}
			found := false
			for _, item := range journal.items {
				if item.content == fmt.Sprintf("hi-%d", i) && item.state != "accepted" {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}, 10*time.Second, 10*time.Millisecond,
		"a submit reported success but its prompt never ran")
}
