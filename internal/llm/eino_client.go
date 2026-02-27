package llm

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/nevaria-ai/gateway/internal/agent"
)

type EinoClient struct {
	modelsConfig *ModelsConfig
	selector     *ModelSelector
	factory      *ProviderFactory
	redisClient  interface{}
}

func NewEinoClient(modelsConfig *ModelsConfig, selector *ModelSelector, redisClient interface{}) *EinoClient {
	return &EinoClient{
		modelsConfig: modelsConfig,
		selector:     selector,
		factory:      NewProviderFactory(modelsConfig),
		redisClient:  redisClient,
	}
}

func (c *EinoClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	chatModel, err := c.selectModel(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to select model: %w", err)
	}

	einoMessages := ConvertAgentMessagesToEinoMessages(messages)

	result, err := chatModel.Generate(ctx, einoMessages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate: %w", err)
	}

	eventCh := make(chan agent.AgentEvent, 10)

	go func() {
		defer close(eventCh)

		agentMsg := EinoMessageToAgentMessage(result)
		eventCh <- agent.MessageStartEvent{
			Timestamp: agentMsg.Timestamp,
			Message:   agentMsg,
		}

		for _, content := range agentMsg.Content {
			if text, ok := content.(agent.TextContent); ok {
				eventCh <- agent.MessageUpdateEvent{
					Timestamp: agentMsg.Timestamp,
					Message:   agentMsg,
					Delta:     text.Text,
				}
			}
		}

		eventCh <- agent.MessageEndEvent{
			Timestamp: agentMsg.Timestamp,
			Message:   agentMsg,
		}
	}()

	return eventCh, nil
}

func (c *EinoClient) selectModel(ctx context.Context, messages []agent.AgentMessage) (model.ToolCallingChatModel, error) {
	return c.factory.CreateChatModel(ctx, "gpt-4o")
}
