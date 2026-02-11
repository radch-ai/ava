// Package tools provides example tools demonstrating the agent tool interface.
//
// The tools in this package demonstrate:
// - Argument parsing from map[string]any
// - Error handling and validation
// - Context cancellation support
// - Different return types (text, numeric results)
//
// These tools can be used as templates for creating custom tools.
package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// ExampleEchoTool returns a tool that echoes input text back.
//
// The tool requires a "text" argument and returns the same text.
// Useful for testing the tool execution pipeline.
func ExampleEchoTool() agent.Tool {
	return agent.Tool{
		Name:        "echo",
		Description: "Echoes the input text back to the caller",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			text, ok := args["text"].(string)
			if !ok || text == "" {
				return nil, fmt.Errorf("missing or invalid 'text' argument")
			}
			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: text}},
				Details: text,
			}, nil
		},
	}
}

// ExampleMathTool returns a tool that performs simple math operations.
//
// Supported operations:
//   - add: returns a + b
//   - subtract: returns a - b
//   - multiply: returns a * b
//   - divide: returns a / b
//
// Arguments:
//   - operation (string): the operation to perform
//   - a (float64): the first operand
//   - b (float64): the second operand
func ExampleMathTool() agent.Tool {
	return agent.Tool{
		Name:        "math",
		Description: "Performs simple math operations: add, subtract, multiply, divide",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			operation, ok := args["operation"].(string)
			if !ok {
				return nil, fmt.Errorf("missing 'operation' argument")
			}

			a, okA := args["a"].(float64)
			if !okA {
				return nil, fmt.Errorf("missing or invalid 'a' argument")
			}

			b, okB := args["b"].(float64)
			if !okB {
				return nil, fmt.Errorf("missing or invalid 'b' argument")
			}

			var result float64
			var err error

			switch operation {
			case "add":
				result = a + b
			case "subtract":
				result = a - b
			case "multiply":
				result = a * b
			case "divide":
				if b == 0 {
					err = fmt.Errorf("division by zero")
				} else {
					result = a / b
				}
			default:
				err = fmt.Errorf("unknown operation: %s", operation)
			}

			if err != nil {
				return nil, err
			}

			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("%.2f", result)}},
				Details: result,
			}, nil
		},
	}
}

// ExampleTimeTool returns a tool that returns the current time.
//
// The time is returned in RFC3339 format as both content and details.
// This tool demonstrates context cancellation support.
func ExampleTimeTool() agent.Tool {
	return agent.Tool{
		Name:        "time",
		Description: "Returns the current time in RFC3339 format",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			now := time.Now().Format(time.RFC3339)
			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: now}},
				Details: now,
			}, nil
		},
	}
}

// ExampleDelayTool returns a tool that simulates long-running operations.
//
// The tool waits for the specified duration before returning.
// It demonstrates proper context cancellation handling for long-running tools.
//
// Arguments:
//   - duration_ms (int or float64): milliseconds to wait (must be non-negative)
func ExampleDelayTool() agent.Tool {
	return agent.Tool{
		Name:        "delay",
		Description: "Waits for the specified duration in milliseconds",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			durationMsFloat, ok := args["duration_ms"].(float64)
			if !ok {
				// Also accept int values for convenience
				durationMsInt, okInt := args["duration_ms"].(int)
				if !okInt {
					return nil, fmt.Errorf("missing or invalid 'duration_ms' argument")
				}
				durationMsFloat = float64(durationMsInt)
			}

			if durationMsFloat < 0 {
				return nil, fmt.Errorf("duration_ms must be non-negative")
			}

			duration := time.Duration(durationMsFloat) * time.Millisecond
			timer := time.NewTimer(duration)
			defer timer.Stop()

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
				result := fmt.Sprintf("waited %d ms", int(durationMsFloat))
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: result}},
					Details: durationMsFloat,
				}, nil
			}
		},
	}
}
