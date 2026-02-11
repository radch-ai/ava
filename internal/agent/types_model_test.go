// Package agent provides core types and events for the agent runtime.
package agent

import (
	"context"
	"testing"
)

// TestThinkingLevelValues validates all thinking level constants.
func TestThinkingLevelValues(t *testing.T) {
	tests := []struct {
		name          string
		level         ThinkingLevel
		wantString    string
		wantIsOff     bool
		wantIsEnabled bool
	}{
		{
			name:          "off level",
			level:         ThinkingLevelOff,
			wantString:    "off",
			wantIsOff:     true,
			wantIsEnabled: false,
		},
		{
			name:          "minimal level",
			level:         ThinkingLevelMinimal,
			wantString:    "minimal",
			wantIsOff:     false,
			wantIsEnabled: true,
		},
		{
			name:          "low level",
			level:         ThinkingLevelLow,
			wantString:    "low",
			wantIsOff:     false,
			wantIsEnabled: true,
		},
		{
			name:          "medium level",
			level:         ThinkingLevelMedium,
			wantString:    "medium",
			wantIsOff:     false,
			wantIsEnabled: true,
		},
		{
			name:          "high level",
			level:         ThinkingLevelHigh,
			wantString:    "high",
			wantIsOff:     false,
			wantIsEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.level.String(); got != tt.wantString {
				t.Errorf("ThinkingLevel.String() = %v, want %v", got, tt.wantString)
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

// TestModel validates model configuration.
func TestModel(t *testing.T) {
	t.Run("creates model with all fields", func(t *testing.T) {
		model := Model{
			ID:       "claude-3-opus-20240229",
			API:      "anthropic",
			Provider: "Anthropic",
			BaseURL:  "https://api.anthropic.com",
		}

		if model.ID != "claude-3-opus-20240229" {
			t.Errorf("Model.ID = %v, want %v", model.ID, "claude-3-opus-20240229")
		}
		if model.API != "anthropic" {
			t.Errorf("Model.API = %v, want %v", model.API, "anthropic")
		}
		if model.Provider != "Anthropic" {
			t.Errorf("Model.Provider = %v, want %v", model.Provider, "Anthropic")
		}
		if model.BaseURL != "https://api.anthropic.com" {
			t.Errorf("Model.BaseURL = %v, want %v", model.BaseURL, "https://api.anthropic.com")
		}
	})

	t.Run("supports multiple API providers", func(t *testing.T) {
		models := []Model{
			{ID: "gpt-4", API: "openai", Provider: "OpenAI", BaseURL: "https://api.openai.com"},
			{ID: "gemini-pro", API: "google", Provider: "Google", BaseURL: "https://generativelanguage.googleapis.com"},
		}

		for _, m := range models {
			if m.ID == "" {
				t.Error("Model.ID should not be empty")
			}
			if m.API == "" {
				t.Error("Model.API should not be empty")
			}
		}
	})
}

// TestTool tests tool execution.
func TestTool(t *testing.T) {
	t.Run("executes tool successfully", func(t *testing.T) {
		result := &ToolResult{
			Content: []ContentBlock{
				TextContent{Text: "success"},
			},
		}

		tool := Tool{
			Name:        "test_tool",
			Description: "A test tool",
			Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
				return result, nil
			},
		}

		ctx := context.Background()
		got, err := tool.Execute(ctx, map[string]any{"key": "value"})

		if err != nil {
			t.Errorf("Tool.Execute() error = %v, want nil", err)
		}
		if got != result {
			t.Errorf("Tool.Execute() = %v, want %v", got, result)
		}
	})

	t.Run("receives context and arguments", func(t *testing.T) {
		var receivedCtx context.Context
		var receivedArgs map[string]any

		tool := Tool{
			Name:        "ctx_tool",
			Description: "Receives context",
			Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
				receivedCtx = ctx
				receivedArgs = args
				return &ToolResult{}, nil
			},
		}

		ctx := context.Background()
		args := map[string]any{"input": "test"}

		_, _ = tool.Execute(ctx, args)

		if receivedCtx != ctx {
			t.Error("Tool.Execute() did not receive the correct context")
		}
		if receivedArgs["input"] != "test" {
			t.Errorf("Tool.Execute() received args = %v, want %v", receivedArgs, args)
		}
	})
}

// TestToolResult validates result structure.
func TestToolResult(t *testing.T) {
	t.Run("creates result with content blocks", func(t *testing.T) {
		result := ToolResult{
			Content: []ContentBlock{
				TextContent{Text: "output"},
				ImageContent{Data: "binary", MimeType: "image/png"},
			},
			Details: map[string]any{"tokens": 100},
		}

		if len(result.Content) != 2 {
			t.Errorf("ToolResult.Content length = %v, want 2", len(result.Content))
		}
		text, ok := result.Content[0].(TextContent)
		if !ok {
			t.Fatal("expected TextContent")
		}
		if text.Text != "output" {
			t.Errorf("Content[0].Text = %v, want %v", text.Text, "output")
		}
		if result.Details == nil {
			t.Error("ToolResult.Details should not be nil")
		}
	})

	t.Run("supports empty content", func(t *testing.T) {
		result := ToolResult{}

		if len(result.Content) != 0 {
			t.Errorf("ToolResult.Content length = %v, want 0", len(result.Content))
		}
	})
}

// TestToolWithError tests error handling.
func TestToolWithError(t *testing.T) {
	t.Run("returns error from execution", func(t *testing.T) {
		expectedErr := &ToolExecutionError{Message: "tool failed"}

		tool := Tool{
			Name:        "failing_tool",
			Description: "Always fails",
			Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
				return nil, expectedErr
			},
		}

		_, err := tool.Execute(context.Background(), nil)

		if err != expectedErr {
			t.Errorf("Tool.Execute() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("error type contains tool name", func(t *testing.T) {
		toolErr := &ToolExecutionError{
			ToolName: "test_tool",
			Message:  "execution failed",
		}

		if toolErr.ToolName != "test_tool" {
			t.Errorf("ToolExecutionError.ToolName = %v, want %v", toolErr.ToolName, "test_tool")
		}
		if toolErr.Error() == "" {
			t.Error("ToolExecutionError.Error() should not be empty")
		}
	})
}
