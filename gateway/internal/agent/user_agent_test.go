package agent_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
)

func TestUserAgent_SubmitAdmitsAndRunsWorker(t *testing.T) {
	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel: func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
				return backend.ResolvedModel{}, errors.New("no model in start test")
			},
		})

	id, err := mgr.Submit("agent-start-test", "test message", &apitypes.ModelEntry{ID: "gpt-4o"}, "")
	require.NoError(t, err)
	require.NotEmpty(t, id, "admission returns the item's stable id")

	// The worker exists to run the queue.
	require.NotNil(t, agent.Worker(mgr, "agent-start-test"))
}

func TestUserAgent_ConcurrentSubmit(t *testing.T) {
	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{
			GetAgentThread: emptyThread,
			ResolveModel: func(ctx context.Context, entry *apitypes.ModelEntry) (backend.ResolvedModel, error) {
				return backend.ResolvedModel{}, errors.New("no model in concurrent test")
			},
		})

	const numGoroutines = 10
	const numMessagesPerGoroutine = 5

	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range numMessagesPerGoroutine {
				_, _ = mgr.Submit("agent-concurrent-test", "concurrent message", nil, "")
			}
		}()
	}
	wg.Wait()
}

func TestUserAgent_StatusReportsQueue(t *testing.T) {
	mgr := newJournaledManager(t,
		&agent.UserAgentManagerOptions{GetAgentThread: emptyThread})

	_, err := mgr.Submit("agent-status-test", "test", &apitypes.ModelEntry{ID: "gpt-4o"}, "")
	require.NoError(t, err)

	require.True(t, agent.LaneStatus(mgr, "agent-status-test").Active)

	// The worker claims asynchronously: whatever raced, the queue
	// ends up empty once the item runs.
	require.Eventually(t, func() bool {
		return agent.QueueLen(mgr, "agent-status-test") == 0
	}, 5*time.Second, 10*time.Millisecond, "the submitted item never left the queue")
}
