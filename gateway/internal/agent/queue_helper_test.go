package agent

// Test-only admission: the queue's durable half is the backend journal,
// so these unit tests pass an accept callback that writes nothing.

import (
	"context"

	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
)

func testAdmit(s *agentState, content string, model *apitypes.ModelEntry, kind string) (*queuedItem, error) {
	return s.admit("item-"+uuid.NewString(), content, model, kind,
		func(context.Context) (*backend.JournalBatch, error) { return nil, nil })
}
