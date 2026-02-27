package llm

import (
	"context"
	"os"
	"testing"
)

func TestProviderFactory_createOpenAIModel(t *testing.T) {
	factory := &ProviderFactory{}

	tests := []struct {
		name    string
		config  *ModelConfig
		apiKey  string
		wantErr bool
	}{
		{
			name: "valid OpenAI model",
			config: &ModelConfig{
				Provider:  ProviderOpenAI,
				ModelID:   "gpt-4o",
				BaseURL:   "https://api.openai.com/v1",
				APIKeyEnv: "OPENAI_API_KEY",
			},
			apiKey:  "test-api-key",
			wantErr: false,
		},
		{
			name: "OpenAI with empty API key",
			config: &ModelConfig{
				Provider:  ProviderOpenAI,
				ModelID:   "gpt-4o",
				BaseURL:   "https://api.openai.com/v1",
				APIKeyEnv: "OPENAI_API_KEY",
			},
			apiKey:  "",
			wantErr: true,
		},
		{
			name: "OpenAI with custom base URL",
			config: &ModelConfig{
				Provider:  ProviderOpenAI,
				ModelID:   "gpt-4o",
				BaseURL:   "https://custom.api.com/v1",
				APIKeyEnv: "OPENAI_API_KEY",
			},
			apiKey:  "test-api-key",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatModel, err := factory.createOpenAIModel(context.Background(), tt.config, tt.apiKey)

			if tt.wantErr && err == nil {
				t.Error("Expected error but got nil")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !tt.wantErr && chatModel == nil {
				t.Error("Expected chatModel to not be nil")
			}
		})
	}
}

func TestProviderFactory_createAnthropicModel(t *testing.T) {
	factory := &ProviderFactory{}

	tests := []struct {
		name    string
		config  *ModelConfig
		apiKey  string
		wantErr bool
	}{
		{
			name: "valid Anthropic model",
			config: &ModelConfig{
				Provider:  ProviderAnthropic,
				ModelID:   "claude-3-5-sonnet-20241022",
				BaseURL:   "https://api.anthropic.com",
				APIKeyEnv: "ANTHROPIC_API_KEY",
			},
			apiKey:  "test-api-key",
			wantErr: false,
		},
		{
			name: "Anthropic with empty API key",
			config: &ModelConfig{
				Provider:  ProviderAnthropic,
				ModelID:   "claude-3-5-sonnet-20241022",
				BaseURL:   "https://api.anthropic.com",
				APIKeyEnv: "ANTHROPIC_API_KEY",
			},
			apiKey:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatModel, err := factory.createAnthropicModel(context.Background(), tt.config, tt.apiKey)

			if tt.wantErr && err == nil {
				t.Error("Expected error but got nil")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !tt.wantErr && chatModel == nil {
				t.Error("Expected chatModel to not be nil")
			}
		})
	}
}

func TestProviderFactory_createGeminiModel(t *testing.T) {
	if os.Getenv("GOOGLE_API_KEY") == "" {
		t.Skip("GOOGLE_API_KEY not set, skipping test")
	}

	factory := &ProviderFactory{}

	tests := []struct {
		name    string
		config  *ModelConfig
		apiKey  string
		wantErr bool
	}{
		{
			name: "valid Gemini model",
			config: &ModelConfig{
				Provider:  ProviderGoogle,
				ModelID:   "gemini-2.0-flash-exp",
				BaseURL:   "https://generativelanguage.googleapis.com/v1beta",
				APIKeyEnv: "GOOGLE_API_KEY",
			},
			apiKey:  os.Getenv("GOOGLE_API_KEY"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatModel, err := factory.createGeminiModel(context.Background(), tt.config, tt.apiKey)

			if tt.wantErr && err == nil {
				t.Error("Expected error but got nil")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !tt.wantErr && chatModel == nil {
				t.Error("Expected chatModel to not be nil")
			}
		})
	}
}

func TestProviderFactory_createGrokModel(t *testing.T) {
	factory := &ProviderFactory{}

	tests := []struct {
		name    string
		config  *ModelConfig
		apiKey  string
		wantErr bool
	}{
		{
			name: "valid Grok model",
			config: &ModelConfig{
				Provider:  ProviderGrok,
				ModelID:   "grok-2",
				BaseURL:   "https://api.x.ai/v1",
				APIKeyEnv: "XAI_API_KEY",
			},
			apiKey:  "test-api-key",
			wantErr: false,
		},
		{
			name: "Grok with empty API key",
			config: &ModelConfig{
				Provider:  ProviderGrok,
				ModelID:   "grok-2",
				BaseURL:   "https://api.x.ai/v1",
				APIKeyEnv: "XAI_API_KEY",
			},
			apiKey:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatModel, err := factory.createGrokModel(context.Background(), tt.config, tt.apiKey)

			if tt.wantErr && err == nil {
				t.Error("Expected error but got nil")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !tt.wantErr && chatModel == nil {
				t.Error("Expected chatModel to not be nil")
			}
		})
	}
}
