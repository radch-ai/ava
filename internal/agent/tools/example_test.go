// Package tools provides example tools demonstrating the agent tool interface.
package tools

import (
	"context"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// TestExampleEchoTool tests that the echo tool returns the input text.
func TestExampleEchoTool(t *testing.T) {
	t.Run("returns the input text", func(t *testing.T) {
		tool := ExampleEchoTool()
		ctx := context.Background()

		args := map[string]any{"text": "hello world"}
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
		tool := ExampleEchoTool()
		ctx := context.Background()

		args := map[string]any{}
		_, err := tool.Execute(ctx, args)

		if err == nil {
			t.Error("expected error for missing text argument, got nil")
		}
	})
}

// TestExampleMathTool tests the math tool's operations.
func TestExampleMathTool(t *testing.T) {
	t.Run("adds two numbers", func(t *testing.T) {
		tool := ExampleMathTool()
		ctx := context.Background()

		args := map[string]any{"operation": "add", "a": 5.0, "b": 3.0}
		result, err := tool.Execute(ctx, args)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Content) == 0 {
			t.Fatal("expected content to have at least one block")
		}

		// Details should contain the numeric result
		got, ok := result.Details.(float64)
		if !ok {
			t.Fatalf("expected float64 details, got %T", result.Details)
		}

		if got != 8.0 {
			t.Errorf("expected 8.0, got %v", got)
		}
	})

	t.Run("subtracts two numbers", func(t *testing.T) {
		tool := ExampleMathTool()
		ctx := context.Background()

		args := map[string]any{"operation": "subtract", "a": 10.0, "b": 4.0}
		result, err := tool.Execute(ctx, args)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		got, ok := result.Details.(float64)
		if !ok {
			t.Fatalf("expected float64 details, got %T", result.Details)
		}

		if got != 6.0 {
			t.Errorf("expected 6.0, got %v", got)
		}
	})

	t.Run("multiplies two numbers", func(t *testing.T) {
		tool := ExampleMathTool()
		ctx := context.Background()

		args := map[string]any{"operation": "multiply", "a": 3.0, "b": 7.0}
		result, err := tool.Execute(ctx, args)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		got, ok := result.Details.(float64)
		if !ok {
			t.Fatalf("expected float64 details, got %T", result.Details)
		}

		if got != 21.0 {
			t.Errorf("expected 21.0, got %v", got)
		}
	})

	t.Run("divides two numbers", func(t *testing.T) {
		tool := ExampleMathTool()
		ctx := context.Background()

		args := map[string]any{"operation": "divide", "a": 20.0, "b": 4.0}
		result, err := tool.Execute(ctx, args)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		got, ok := result.Details.(float64)
		if !ok {
			t.Fatalf("expected float64 details, got %T", result.Details)
		}

		if got != 5.0 {
			t.Errorf("expected 5.0, got %v", got)
		}
	})

	t.Run("returns error on division by zero", func(t *testing.T) {
		tool := ExampleMathTool()
		ctx := context.Background()

		args := map[string]any{"operation": "divide", "a": 10.0, "b": 0.0}
		_, err := tool.Execute(ctx, args)

		if err == nil {
			t.Error("expected error for division by zero, got nil")
		}
	})

	t.Run("returns error for unknown operation", func(t *testing.T) {
		tool := ExampleMathTool()
		ctx := context.Background()

		args := map[string]any{"operation": "modulo", "a": 10.0, "b": 3.0}
		_, err := tool.Execute(ctx, args)

		if err == nil {
			t.Error("expected error for unknown operation, got nil")
		}
	})

	t.Run("returns error when arguments are missing", func(t *testing.T) {
		tool := ExampleMathTool()
		ctx := context.Background()

		args := map[string]any{"operation": "add"}
		_, err := tool.Execute(ctx, args)

		if err == nil {
			t.Error("expected error for missing arguments, got nil")
		}
	})
}

// TestExampleTimeTool tests the time tool.
func TestExampleTimeTool(t *testing.T) {
	t.Run("returns current time in RFC3339 format", func(t *testing.T) {
		tool := ExampleTimeTool()
		ctx := context.Background()

		result, err := tool.Execute(ctx, nil)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Content) == 0 {
			t.Fatal("expected content to have at least one block")
		}

		// Details should contain the time string
		timeStr, ok := result.Details.(string)
		if !ok {
			t.Fatalf("expected string details, got %T", result.Details)
		}

		// Verify it's a valid time string
		parsedTime, err := time.Parse(time.RFC3339, timeStr)
		if err != nil {
			t.Fatalf("failed to parse time string: %v", err)
		}

		// Check that the time is recent (within last minute)
		age := time.Since(parsedTime)
		if age < 0 || age > time.Minute {
			t.Errorf("returned time is not recent: %v (age: %v)", parsedTime, age)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tool := ExampleTimeTool()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := tool.Execute(ctx, nil)
		if err == nil {
			t.Error("expected error from cancelled context, got nil")
		}
	})
}

// TestExampleDelayTool tests the delay tool.
func TestExampleDelayTool(t *testing.T) {
	t.Run("waits for the specified duration", func(t *testing.T) {
		tool := ExampleDelayTool()
		ctx := context.Background()

		start := time.Now()
		args := map[string]any{"duration_ms": 50}
		result, err := tool.Execute(ctx, args)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Content) == 0 {
			t.Fatal("expected content to have at least one block")
		}

		// Should have waited at least 50ms
		if elapsed < 50*time.Millisecond {
			t.Errorf("expected delay of at least 50ms, got %v", elapsed)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tool := ExampleDelayTool()
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel after a short delay
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		args := map[string]any{"duration_ms": 1000}
		start := time.Now()
		_, err := tool.Execute(ctx, args)
		elapsed := time.Since(start)

		if err == nil {
			t.Error("expected error from cancelled context, got nil")
		}

		// Should have been cancelled well before 1000ms
		if elapsed > 200*time.Millisecond {
			t.Errorf("expected quick cancellation, got %v", elapsed)
		}
	})

	t.Run("returns error when duration_ms is missing", func(t *testing.T) {
		tool := ExampleDelayTool()
		ctx := context.Background()

		args := map[string]any{}
		_, err := tool.Execute(ctx, args)

		if err == nil {
			t.Error("expected error for missing duration_ms, got nil")
		}
	})

	t.Run("returns error for negative duration", func(t *testing.T) {
		tool := ExampleDelayTool()
		ctx := context.Background()

		args := map[string]any{"duration_ms": -100}
		_, err := tool.Execute(ctx, args)

		if err == nil {
			t.Error("expected error for negative duration, got nil")
		}
	})
}

// TestRegistry tests the tool registry.
func TestRegistry(t *testing.T) {
	t.Run("DefaultTools returns example tools", func(t *testing.T) {
		tools := DefaultTools()

		if len(tools) == 0 {
			t.Fatal("expected at least one default tool")
		}

		// Check for expected tool names
		toolNames := make(map[string]bool)
		for _, tool := range tools {
			toolNames[tool.Name] = true
		}

		expectedTools := []string{"echo", "math", "time", "delay"}
		for _, name := range expectedTools {
			if !toolNames[name] {
				t.Errorf("expected default tool %q not found", name)
			}
		}
	})

	t.Run("RegisterTool adds a tool to the registry", func(t *testing.T) {
		registry := NewRegistry()

		customTool := agent.Tool{
			Name:        "custom",
			Description: "A custom tool",
			Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "custom result"}},
					Details: "custom",
				}, nil
			},
		}

		registered := RegisterTool(registry, "custom", customTool)
		if !registered {
			t.Error("expected RegisterTool to return true")
		}

		// Verify the tool was registered
		retrieved := GetTool(registry, "custom")
		if retrieved == nil {
			t.Fatal("expected to retrieve registered tool")
		}

		if retrieved.Name != "custom" {
			t.Errorf("expected tool name %q, got %q", "custom", retrieved.Name)
		}
	})

	t.Run("RegisterTool returns false for duplicate names", func(t *testing.T) {
		registry := NewRegistry()

		tool1 := agent.Tool{
			Name:        "duplicate",
			Description: "First tool",
			Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
				return &agent.ToolResult{}, nil
			},
		}

		tool2 := agent.Tool{
			Name:        "duplicate",
			Description: "Second tool",
			Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
				return &agent.ToolResult{}, nil
			},
		}

		RegisterTool(registry, "duplicate", tool1)
		duplicateRegistered := RegisterTool(registry, "duplicate", tool2)

		if duplicateRegistered {
			t.Error("expected RegisterTool to return false for duplicate name")
		}

		// Verify the original tool is still there
		retrieved := GetTool(registry, "duplicate")
		if retrieved.Description != "First tool" {
			t.Error("expected original tool to remain registered")
		}
	})

	t.Run("GetTool returns nil for unknown tool", func(t *testing.T) {
		registry := NewRegistry()

		retrieved := GetTool(registry, "unknown")
		if retrieved != nil {
			t.Error("expected nil for unknown tool")
		}
	})

	t.Run("registry can be initialized with default tools", func(t *testing.T) {
		registry := NewRegistry(DefaultTools()...)

		// Should be able to get all default tools
		defaultToolNames := []string{"echo", "math", "time", "delay"}
		for _, name := range defaultToolNames {
			retrieved := GetTool(registry, name)
			if retrieved == nil {
				t.Errorf("expected default tool %q to be registered", name)
			}
		}
	})
}
