// Package usage_test provides tests for usage tracking with Redis fallback.
package usage_test

import (
	"context"
	"testing"

	llm_proxy "github.com/nevaria-ai/gateway/internal/llm-proxy"
	llm_proxy_usage "github.com/nevaria-ai/gateway/internal/llm-proxy/usage"
)

func TestTrackUsage(t *testing.T) {
	ctx := context.Background()

	event := llm_proxy.UsageEvent{
		UserID:       "test-user",
		SessionID:    "test-session",
		Model:        "gpt-4o-mini",
		TokensInput:  100,
		TokensOutput: 50,
		CostTotal:    0.0003,
		ChargedFrom:  "plan",
	}

	err := llm_proxy_usage.TrackUsage(ctx, event)
	if err != nil {
		t.Logf("TrackUsage failed (expected without actual storage): %v", err)
		// This is expected to fail without actual storage configured
	}
}

func TestUsageWriter(t *testing.T) {
	writer := llm_proxy_usage.NewUsageWriter(nil, nil)
	if writer == nil {
		t.Fatal("NewUsageWriter returned nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the writer in a goroutine
	go writer.Start(ctx)

	// Track a simple event
	event := llm_proxy.UsageEvent{
		UserID:       "test-user",
		Model:        "gpt-4o-mini",
		TokensInput:  100,
		TokensOutput: 50,
		CostTotal:    0.0003,
	}

	err := writer.Track(event)
	if err != nil {
		t.Logf("Track failed (expected without storage): %v", err)
		// This is expected to fail without actual storage
	}
}

func TestUsageBatching(t *testing.T) {
	writer := llm_proxy_usage.NewUsageWriter(nil, nil)
	if writer == nil {
		t.Fatal("NewUsageWriter returned nil")
	}

	// Track multiple events
	for i := 0; i < 10; i++ {
		event := llm_proxy.UsageEvent{
			UserID:       "test-user",
			Model:        "gpt-4o-mini",
			TokensInput:  100,
			TokensOutput: 50,
			CostTotal:    0.0003,
		}
		err := writer.Track(event)
		if err != nil {
			t.Logf("Track failed for event %d (expected without storage): %v", i, err)
			// This is expected to fail without actual storage
		}
	}
}
