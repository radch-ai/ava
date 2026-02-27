// Package tools provides agent tools for file operations.
package tools

import (
	"context"
	"fmt"
	"path"

	"github.com/nevaria-ai/gateway/internal/agent"
	ctxpkg "github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/nevaria-ai/gateway/internal/storage"
)

// ReadAgentFileTool creates a tool for reading agent workspace files.
func ReadAgentFileTool(client storage.Client) agent.Tool {
	return agent.Tool{
		Name:        "read_agent_file",
		Description: "Read a file from the agent's workspace. Common files: SOUL.md, AGENTS.md, USER.md, IDENTITY.md, TOOLS.md",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			// Get userID and agentID from context
			userID := ctxpkg.GetUserIDFromContext(ctx)
			agentID := ctxpkg.GetAgentIDFromContext(ctx)

			if userID == "" || agentID == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: user ID or agent ID not found in context"}},
				}, fmt.Errorf("user ID or agent ID not found in context")
			}

			// Extract path
			filePath, ok := args["path"].(string)
			if !ok || filePath == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: 'path' parameter is required"}},
				}, fmt.Errorf("missing path parameter")
			}

			// Build S3 path with enforced scope
			s3Path := path.Join(userID, agentID, "workspace", filePath)

			// Read file
			data, err := client.Get(ctx, s3Path)
			if err != nil {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Error reading file: %v", err)}},
				}, err
			}

			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: string(data)}},
			}, nil
		},
	}
}

// WriteAgentFileTool creates a tool for writing agent workspace files.
func WriteAgentFileTool(client storage.Client) agent.Tool {
	return agent.Tool{
		Name:        "write_agent_file",
		Description: "Write a file to the agent's workspace. Use for updating configuration or saving important information.",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			// Get userID and agentID from context
			userID := ctxpkg.GetUserIDFromContext(ctx)
			agentID := ctxpkg.GetAgentIDFromContext(ctx)

			if userID == "" || agentID == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: user ID or agent ID not found in context"}},
				}, fmt.Errorf("user ID or agent ID not found in context")
			}

			// Extract parameters
			filePath, ok := args["path"].(string)
			if !ok || filePath == "" {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: 'path' parameter is required"}},
				}, fmt.Errorf("missing path parameter")
			}

			content, ok := args["content"].(string)
			if !ok {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "Error: 'content' parameter is required"}},
				}, fmt.Errorf("missing content parameter")
			}

			// Build S3 path with enforced scope
			s3Path := path.Join(userID, agentID, "workspace", filePath)

			// Write file
			err := client.Put(ctx, s3Path, []byte(content))
			if err != nil {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Error writing file: %v", err)}},
				}, err
			}

			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("Successfully wrote %s", filePath)}},
			}, nil
		},
	}
}
