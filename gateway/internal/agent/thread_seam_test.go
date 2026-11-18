package agent_test

import (
	"context"
	"sync"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
)

// emptyThread is the thread seam for manager tests that run turns
// without a store: no history rides the model call, so the scripted
// reply drives the assertions. Tests that assert model input wire a
// real store (turn_context_test.go) or memThread below.
func emptyThread(context.Context, string) ([]backend.AgentThreadMessage, error) {
	return nil, nil
}

// memThread pairs the sink and source seams over one slice, for tests
// that assert what the model received: the committed user row is the
// turn's prompt. Text rows replay; structure rows do not —
// structure-sensitive tests use the real store.
type memThread struct {
	mu   sync.Mutex
	rows []backend.ThreadMessageDraft
}

func (m *memThread) append(_ context.Context, _ string, msgs []backend.ThreadMessageDraft) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, msgs...)
	return nil
}

func (m *memThread) load(_ context.Context, _ string) ([]backend.AgentThreadMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]backend.AgentThreadMessage, 0, len(m.rows))
	for _, r := range m.rows {
		switch r.Role {
		case apitypes.ThreadRoleUser, apitypes.ThreadRoleAssistant:
			out = append(out, backend.AgentThreadMessage{Role: r.Role, Content: r.Content})
		}
	}
	return out, nil
}
