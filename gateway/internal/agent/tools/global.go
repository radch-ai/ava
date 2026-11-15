package tools

import (
	"context"

	duckduckgo "github.com/cloudwego/eino-ext/components/tool/duckduckgo"
	"github.com/cloudwego/eino/components/tool"
)

// GetGlobalTools returns the default tools available to all agents.
func GetGlobalTools() []tool.BaseTool {
	ctx := context.Background()
	ddgTool, err := duckduckgo.NewTool(ctx, &duckduckgo.Config{}) //nolint:staticcheck // NewTextSearchTool not yet available
	if err != nil {
		return []tool.BaseTool{}
	}
	return []tool.BaseTool{ddgTool}
}
