package tools

import (
	"context"
	"fmt"

	"github.com/nevaria-ai/gateway/internal/agent"
	ctxpkg "github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/nevaria-ai/gateway/internal/mem0"
)

// Mem0SearchTool creates a tool for searching memories.
func Mem0SearchTool(client mem0.Client) agent.Tool {
	return agent.Tool{
		Name:        "mem0_search",
		Description: "Search stored memories and knowledge about the user. Returns relevant facts, preferences, and past decisions.",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			// Get userID from context
			userID := ctxpkg.GetUserIDFromContext(ctx)
			if userID == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: user ID not found in context"}},
				}, fmt.Errorf("user ID not found in context")
			}

			// Extract query
			query, ok := args["query"].(string)
			if !ok || query == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: 'query' parameter is required"}},
				}, fmt.Errorf("missing query parameter")
			}

			// Search memories
			memories, err := client.Search(ctx, userID, query)
			if err != nil {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Error searching memories: %v", err)}},
				}, err
			}

			// Format results
			if len(memories) == 0 {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "No relevant memories found"}},
				}, nil
			}

			resultText := fmt.Sprintf("Found %d relevant memories:\n", len(memories))
			for i, mem := range memories {
				resultText += fmt.Sprintf("%d. %s\n", i+1, mem)
			}

			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: resultText}},
			}, nil
		},
	}
}

// Mem0StoreTool creates a tool for storing long-term memories.
func Mem0StoreTool(client mem0.Client) agent.Tool {
	return agent.Tool{
		Name:        "mem0_store",
		Description: "Store important information in long-term memory. Use for facts, preferences, and decisions that should be remembered.",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			// Get userID from context
			userID := ctxpkg.GetUserIDFromContext(ctx)
			if userID == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: user ID not found in context"}},
				}, fmt.Errorf("user ID not found in context")
			}

			// Extract content
			content, ok := args["content"].(string)
			if !ok || content == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: 'content' parameter is required"}},
				}, fmt.Errorf("missing content parameter")
			}

			// Store memory (long-term, no runID)
			err := client.Add(ctx, userID, content, "")
			if err != nil {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Error storing memory: %v", err)}},
				}, err
			}

			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Stored: %s", content)}},
			}, nil
		},
	}
}
