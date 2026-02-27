// Package agent_test provides tests for example tools.
package agent_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// TestExampleEchoTool tests that the echo tool returns the input text.
func TestExampleEchoTool(t *testing.T) {
	t.Run("returns the input text", func(t *testing.T) {
		tool := agent.MockEchoTool("echo")
		ctx := context.Background()

		args := map[string]any{"input": "hello world"}
		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Content) == 0 {
			t.Fatal("expected content to have at least one block")
		}

		// The result should contain the echoed text
		got, ok := result.Content[0].(agent.TextContent)
		if !ok {
			t.Fatal("expected TextContent block")
		}

		if got.Text != "hello world" {
			t.Errorf("expected %q, got %q", "hello world", got.Text)
		}
	})

	t.Run("returns error when text argument is missing", func(t *testing.T) {
		tool := agent.MockEchoTool("echo")
		ctx := context.Background()

		args := map[string]any{}
		_, err := tool.Execute(ctx, args)

		if err == nil {
			t.Error("expected error for missing text argument, got nil")
		}
	})
}

// TestExampleTimeTool tests that the time tool returns current time.
func TestExampleTimeTool(t *testing.T) {
	t.Run("returns current time", func(t *testing.T) {
		tool := agent.MockTool(agent.MockToolConfig{
			Name: "time",
			ExecuteFunc: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
				now := time.Now().Format(time.RFC3339)
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: now}},
				}, nil
			},
		})

		ctx := context.Background()
		result, err := tool.Execute(ctx, nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Content) == 0 {
			t.Fatal("expected content to have at least one block")
		}

		_, ok := result.Content[0].(agent.TextContent)
		if !ok {
			t.Fatal("expected TextContent block")
		}
	})
}

// TestExampleWeatherTool tests that the weather tool returns mock weather data.
func TestExampleWeatherTool(t *testing.T) {
	t.Run("returns weather for location", func(t *testing.T) {
		tool := agent.MockTool(agent.MockToolConfig{
			Name: "weather",
			ExecuteFunc: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
				location, ok := args["location"].(string)
				if !ok {
					return nil, fmt.Errorf("missing location")
				}
				return &agent.ToolResult{
					Content: []agent.ContentBlock{
						agent.TextContent{Text: fmt.Sprintf("Weather in %s: Sunny, 72°F", location)},
					},
				}, nil
			},
		})

		ctx := context.Background()
		args := map[string]any{"location": "San Francisco"}
		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Content) == 0 {
			t.Fatal("expected content to have at least one block")
		}

		got, ok := result.Content[0].(agent.TextContent)
		if !ok {
			t.Fatal("expected TextContent block")
		}

		if !strings.Contains(got.Text, "San Francisco") {
			t.Errorf("expected weather info for San Francisco, got %q", got.Text)
		}
	})
}
