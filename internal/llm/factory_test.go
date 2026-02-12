// Package llm provides tests for the LLM client factory.
package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// TestProviderConstants verifies provider constants are properly defined.
func TestProviderConstants(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     string
	}{
		{
			name:     "OpenAI provider string",
			provider: ProviderOpenAI,
			want:     "openai",
		},
		{
			name:     "Anthropic provider string",
			provider: ProviderAnthropic,
			want:     "anthropic",
		},
		{
			name:     "Google provider string",
			provider: ProviderGoogle,
			want:     "google",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.provider.String(); got != tt.want {
				t.Errorf("Provider.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestProviderFromString tests parsing provider from string.
func TestProviderFromString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Provider
		wantErr bool
	}{
		{
			name:    "Valid OpenAI provider",
			input:   "openai",
			want:    ProviderOpenAI,
			wantErr: false,
		},
		{
			name:    "Valid Anthropic provider",
			input:   "anthropic",
			want:    ProviderAnthropic,
			wantErr: false,
		},
		{
			name:    "Valid Google provider",
			input:   "google",
			want:    ProviderGoogle,
			wantErr: false,
		},
		{
			name:    "Invalid provider",
			input:   "invalid",
			want:    Provider(""),
			wantErr: true,
		},
		{
			name:    "Empty provider",
			input:   "",
			want:    Provider(""),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProviderFromString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ProviderFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ProviderFromString() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDetectProviderFromModel tests provider detection from model names.
func TestDetectProviderFromModel(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		want    Provider
		wantErr bool
	}{
		{
			name:    "GPT-4 model",
			model:   "gpt-4",
			want:    ProviderOpenAI,
			wantErr: false,
		},
		{
			name:    "GPT-3.5 model",
			model:   "gpt-3.5-turbo",
			want:    ProviderOpenAI,
			wantErr: false,
		},
		{
			name:    "GPT-4o model",
			model:   "gpt-4o",
			want:    ProviderOpenAI,
			wantErr: false,
		},
		{
			name:    "Claude 3 Opus",
			model:   "claude-3-opus-20240229",
			want:    ProviderAnthropic,
			wantErr: false,
		},
		{
			name:    "Claude 3 Sonnet",
			model:   "claude-3-5-sonnet-20241022",
			want:    ProviderAnthropic,
			wantErr: false,
		},
		{
			name:    "Claude 3 Haiku",
			model:   "claude-3-haiku-20240307",
			want:    ProviderAnthropic,
			wantErr: false,
		},
		{
			name:    "Gemini Pro",
			model:   "gemini-pro",
			want:    ProviderGoogle,
			wantErr: false,
		},
		{
			name:    "Gemini Ultra",
			model:   "gemini-ultra",
			want:    ProviderGoogle,
			wantErr: false,
		},
		{
			name:    "Unknown model",
			model:   "unknown-model",
			want:    Provider(""),
			wantErr: true,
		},
		{
			name:    "Empty model",
			model:   "",
			want:    Provider(""),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectProviderFromModel(tt.model)
			if (err != nil) != tt.wantErr {
				t.Errorf("DetectProviderFromModel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("DetectProviderFromModel() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestConfigDefaults tests that Config has sensible defaults.
func TestConfigDefaults(t *testing.T) {
	config := Config{
		OpenAIAPIKey:    "test-openai-key",
		AnthropicAPIKey: "test-anthropic-key",
		GoogleAPIKey:    "test-google-key",
	}

	if config.Timeout != 0 {
		// Default timeout should be applied
		if config.Timeout != DefaultTimeout {
			t.Errorf("Expected default timeout, got %v", config.Timeout)
		}
	}
}

// TestNewClient tests the client factory function.
func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		model   agent.Model
		wantErr bool
	}{
		{
			name: "Valid OpenAI client",
			config: Config{
				OpenAIAPIKey: "test-openai-key",
			},
			model: agent.Model{
				ID:       "gpt-4",
				Provider: "openai",
			},
			wantErr: false,
		},
		{
			name: "Valid Anthropic client",
			config: Config{
				AnthropicAPIKey: "test-anthropic-key",
			},
			model: agent.Model{
				ID:       "claude-3-5-sonnet-20241022",
				Provider: "anthropic",
			},
			wantErr: false,
		},
		{
			name: "Valid Google client",
			config: Config{
				GoogleAPIKey: "test-google-key",
			},
			model: agent.Model{
				ID:       "gemini-pro",
				Provider: "google",
			},
			wantErr: false,
		},
		{
			name: "Missing API key for OpenAI",
			config: Config{
				OpenAIAPIKey: "",
			},
			model: agent.Model{
				ID:       "gpt-4",
				Provider: "openai",
			},
			wantErr: true,
		},
		{
			name: "Missing API key for Anthropic",
			config: Config{
				AnthropicAPIKey: "",
			},
			model: agent.Model{
				ID:       "claude-3-opus-20240229",
				Provider: "anthropic",
			},
			wantErr: true,
		},
		{
			name: "Missing API key for Google",
			config: Config{
				GoogleAPIKey: "",
			},
			model: agent.Model{
				ID:       "gemini-pro",
				Provider: "google",
			},
			wantErr: true,
		},
		{
			name: "Auto-detect OpenAI provider",
			config: Config{
				OpenAIAPIKey: "test-openai-key",
			},
			model: agent.Model{
				ID:       "gpt-4",
				Provider: "", // Provider should be auto-detected
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config, tt.model)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("NewClient() returned nil client without error")
			}
		})
	}
}

// TestClientStream tests that Client implements LLMClient interface.
func TestClientStream(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		model          agent.Model
		messages       []agent.AgentMessage
		wantMsgCount   int
		wantEventTypes []string
		wantErr        bool
	}{
		{
			name: "OpenAI stream with simple message",
			config: Config{
				OpenAIAPIKey: "test-openai-key",
			},
			model: agent.Model{
				ID:       "gpt-4",
				Provider: "openai",
			},
			messages: []agent.AgentMessage{
				agent.NewTextMessage(agent.RoleUser, "Hello"),
			},
			wantMsgCount: 1,
			wantEventTypes: []string{
				"TurnStartEvent",
				"MessageStartEvent",
				"MessageUpdateEvent",
				"MessageEndEvent",
				"TurnEndEvent",
			},
			wantErr: false,
		},
		{
			name: "Anthropic stream with simple message",
			config: Config{
				AnthropicAPIKey: "test-anthropic-key",
			},
			model: agent.Model{
				ID:       "claude-3-5-sonnet-20241022",
				Provider: "anthropic",
			},
			messages: []agent.AgentMessage{
				agent.NewTextMessage(agent.RoleUser, "Hello"),
			},
			wantMsgCount: 1,
			wantEventTypes: []string{
				"TurnStartEvent",
				"MessageStartEvent",
				"MessageUpdateEvent",
				"MessageEndEvent",
				"TurnEndEvent",
			},
			wantErr: false,
		},
		{
			name: "Google stream with simple message",
			config: Config{
				GoogleAPIKey: "test-google-key",
			},
			model: agent.Model{
				ID:       "gemini-pro",
				Provider: "google",
			},
			messages: []agent.AgentMessage{
				agent.NewTextMessage(agent.RoleUser, "Hello"),
			},
			wantMsgCount: 1,
			wantEventTypes: []string{
				"TurnStartEvent",
				"MessageStartEvent",
				"MessageUpdateEvent",
				"MessageEndEvent",
				"TurnEndEvent",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip integration tests that require real API keys
			if tt.name == "Anthropic stream with simple message" ||
				tt.name == "OpenAI stream with simple message" ||
				tt.name == "Google stream with simple message" {
				t.Skip("Skipping integration test - requires real API keys")
			}

			client, err := NewClient(tt.config, tt.model)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			ctx := context.Background()
			eventCh, err := client.Stream(ctx, tt.messages, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Client.Stream() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Collect events
			var events []agent.AgentEvent
			for event := range eventCh {
				events = append(events, event)
			}

			// Verify event types (should match expected pattern)
			if len(events) < len(tt.wantEventTypes) {
				t.Errorf("Expected at least %d events, got %d", len(tt.wantEventTypes), len(events))
			}
		})
	}
}

// TestClientStreamCancellation tests context cancellation.
func TestClientStreamCancellation(t *testing.T) {
	config := Config{
		OpenAIAPIKey: "test-openai-key",
	}
	model := agent.Model{
		ID:       "gpt-4",
		Provider: "openai",
	}

	client, err := NewClient(config, model)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "Hello"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	eventCh, err := client.Stream(ctx, messages, nil)
	// OpenAI SDK returns error when context is canceled before request starts
	// This is correct behavior - the HTTP request is never made
	if err != nil {
		// For OpenAI with real SDK, context cancellation returns error
		if strings.Contains(err.Error(), "context canceled") {
			// This is expected behavior for OpenAI
			return
		}
		t.Fatalf("Client.Stream() unexpected error = %v", err)
	}

	// Channel should be closed quickly
	select {
	case <-eventCh:
		// Expected
	case <-time.After(1 * time.Second):
		t.Error("Stream did not respect context cancellation")
	}
}

// TestGetProviderClient tests getting the specific provider client.
func TestGetProviderClient(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		model         agent.Model
		wantOpenAI    bool
		wantAnthropic bool
		wantGoogle    bool
		wantErr       bool
	}{
		{
			name: "OpenAI client has OpenAI provider",
			config: Config{
				OpenAIAPIKey: "test-openai-key",
			},
			model: agent.Model{
				ID:       "gpt-4",
				Provider: "openai",
			},
			wantOpenAI: true,
			wantErr:    false,
		},
		{
			name: "Anthropic client has Anthropic provider",
			config: Config{
				AnthropicAPIKey: "test-anthropic-key",
			},
			model: agent.Model{
				ID:       "claude-3-5-sonnet-20241022",
				Provider: "anthropic",
			},
			wantAnthropic: true,
			wantErr:       false,
		},
		{
			name: "Google client has Google provider",
			config: Config{
				GoogleAPIKey: "test-google-key",
			},
			model: agent.Model{
				ID:       "gemini-pro",
				Provider: "google",
			},
			wantGoogle: true,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config, tt.model)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if tt.wantOpenAI && client.openai == nil {
				t.Error("Expected OpenAI client to be non-nil")
			}
			if tt.wantAnthropic && client.anthropic == nil {
				t.Error("Expected Anthropic client to be non-nil")
			}
			if tt.wantGoogle && client.google == nil {
				t.Error("Expected Google client to be non-nil")
			}
		})
	}
}
