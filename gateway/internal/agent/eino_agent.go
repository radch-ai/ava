package agent

import (
	"context"
	"errors"

	openaicmp "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"

	"github.com/radch-ai/ava/backend"
)

// createChatModel connects an OpenAI-compatible chat model to the
// endpoint a turn was resolved to.
func createChatModel(ctx context.Context, cfg backend.ResolvedModel) (model.ToolCallingChatModel, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("base URL cannot be empty")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("API key cannot be empty")
	}
	if cfg.Model == "" {
		return nil, errors.New("model cannot be empty")
	}
	return openaicmp.NewChatModel(ctx, &openaicmp.ChatModelConfig{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
	})
}

// buildEinoAgent wraps a chat model in the agent runtime. The
// instruction is empty: a turn runs the submitted content alone.
func buildEinoAgent(ctx context.Context, agentID string, chatModel model.ToolCallingChatModel) (*adk.ChatModelAgent, error) {
	if agentID == "" {
		return nil, errors.New("agentID cannot be empty")
	}
	if chatModel == nil {
		return nil, errors.New("chatModel cannot be nil")
	}
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:  agentID,
		Model: chatModel,
	})
}

// buildEinoRunner creates a turn's runner. Streaming is off: a turn
// emits its assembled answer once.
func buildEinoRunner(ctx context.Context, agent *adk.ChatModelAgent) (*adk.Runner, error) {
	if agent == nil {
		return nil, errors.New("agent cannot be nil")
	}
	return adk.NewRunner(ctx, adk.RunnerConfig{
		Agent: agent,
	}), nil
}
