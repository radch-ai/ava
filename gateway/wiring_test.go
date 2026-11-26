package gateway

// The wiring pin: managerOptions must forward every Config seam. A
// dropped seam detaches a capability the binary bound at boot with no
// compile error — the agent-level suites inject their seams below this
// point, so nothing else catches it.

import (
	"context"
	"reflect"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/backend"
	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
)

func TestManagerOptionsForwardsEverySeam(t *testing.T) {
	var (
		resolve = func(context.Context, *apitypes.ModelEntry) (backend.ResolvedModel, error) {
			return backend.ResolvedModel{}, nil
		}
		cfgSrc                          = func(context.Context, string) (*apitypes.AgentConfig, error) { return nil, nil }
		tools                           = func(context.Context, string) (backend.TurnTools, error) { return backend.TurnTools{}, nil }
		thread                          = func(context.Context, string) ([]backend.AgentThreadMessage, error) { return nil, nil }
		noteWind                        = func(context.Context, string, string, int) error { return nil }
		journal  agent.LifecycleJournal = &backend.TurnService{}
	)
	cfg := &Config{
		ResolveModel:         resolve,
		Journal:              journal,
		GetAgentConfig:       cfgSrc,
		Tools:                tools,
		GetAgentThread:       thread,
		NoteDiscoveredWindow: noteWind,
	}
	opts := managerOptions(cfg)
	funcs := []struct {
		name string
		want any
		got  any
	}{
		{"ResolveModel", cfg.ResolveModel, opts.ResolveModel},
		{"GetAgentConfig", cfg.GetAgentConfig, opts.GetAgentConfig},
		{"Tools", cfg.Tools, opts.Tools},
		{"GetAgentThread", cfg.GetAgentThread, opts.GetAgentThread},
		{"NoteDiscoveredWindow", cfg.NoteDiscoveredWindow, opts.NoteDiscoveredWindow},
	}
	for _, f := range funcs {
		if reflect.ValueOf(f.want).Pointer() != reflect.ValueOf(f.got).Pointer() {
			t.Errorf("%s was not forwarded to the manager options", f.name)
		}
	}
	if opts.Journal != cfg.Journal {
		t.Error("Journal was not forwarded to the manager options")
	}

	// And New consumes the same literal: a gateway built from this
	// config wires the seam through, not just the struct.
	if _, err := New(cfg, zerolog.Nop()); err != nil {
		t.Fatalf("New: %v", err)
	}
}
