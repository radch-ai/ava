// Package usage provides usage tracking with Redis fallback.
package usage

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/llm-proxy"
)

func TestTrackUsage(t *testing.T) {
	ctx := context.Background()

	event := proxy.UsageEvent{
		UserID:       "test-user",
		SessionID:    "test-session",
		Model:        "gpt-4o-mini",
		TokensInput:  100,
		TokensOutput: 50,
		CostTotal:    0.0003,
		ChargedFrom:  "plan",
	}

	err := TrackUsage(ctx, event)
	if err != nil {
		t.Fatalf("TrackUsage failed: %v", err)
	}
}

func TestUsageWriter(t *testing.T) {
	writer := NewUsageWriter(nil, nil)
	if writer == nil {
		t.Fatal("NewUsageWriter returned nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the writer in a goroutine
	go writer.Start(ctx)

	// Track a simple event
	event := proxy.UsageEvent{
		UserID:       "test-user",
		Model:        "gpt-4o-mini",
		TokensInput:  100,
		TokensOutput: 50,
		CostTotal:    0.0003,
	}

	err := writer.Track(event)
	if err != nil {
		t.Fatalf("Track failed: %v", err)
	}
}

func TestUsageBatching(t *testing.T) {
	writer := NewUsageWriter(nil, nil)
	if writer == nil {
		t.Fatal("NewUsageWriter returned nil")
	}

	// Track multiple events
	for i := 0; i < 10; i++ {
		event := proxy.UsageEvent{
			UserID:       "test-user",
			Model:        "gpt-4o-mini",
			TokensInput:  100,
			TokensOutput: 50,
			CostTotal:    0.0003,
		}
		err := writer.Track(event)
		if err != nil {
			t.Fatalf("Track failed for event %d: %v", i, err)
		}
	}
}
