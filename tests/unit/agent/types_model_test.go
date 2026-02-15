// Package agent_test provides tests for the agent runtime.
package agent_test

import (
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// TestThinkingLevelValues validates all thinking level constants.
func TestThinkingLevelValues(t *testing.T) {
	tests := []struct {
		name          string
		level         agent.ThinkingLevel
		wantString    string
		wantIsOff     bool
		wantIsEnabled bool
	}{
		{
			name:          "off level",
			level:         agent.ThinkingLevelOff,
			wantString:    "off",
			wantIsOff:     true,
			wantIsEnabled: false,
		},
		{
			name:          "minimal level",
			level:         agent.ThinkingLevelMinimal,
			wantString:    "minimal",
			wantIsOff:     false,
			wantIsEnabled: true,
		},
		{
			name:          "low level",
			level:         agent.ThinkingLevelLow,
			wantString:    "low",
			wantIsOff:     false,
			wantIsEnabled: true,
		},
		{
			name:          "medium level",
			level:         agent.ThinkingLevelMedium,
			wantString:    "medium",
			wantIsOff:     false,
			wantIsEnabled: true,
		},
		{
			name:          "high level",
			level:         agent.ThinkingLevelHigh,
			wantString:    "high",
			wantIsOff:     false,
			wantIsEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.level.String(); got != tt.wantString {
				t.Errorf("ThinkingLevel.String() = %q, want %q", got, tt.wantString)
			}

			if got := tt.level.IsOff(); got != tt.wantIsOff {
				t.Errorf("ThinkingLevel.IsOff() = %v, want %v", got, tt.wantIsOff)
			}

			if got := tt.level.IsEnabled(); got != tt.wantIsEnabled {
				t.Errorf("ThinkingLevel.IsEnabled() = %v, want %v", got, tt.wantIsEnabled)
			}
		})
	}
}
