package llm

import (
	"encoding/json"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/nevaria-ai/gateway/internal/agent"
)

func ConvertAgentMessagesToEinoMessages(msgs []agent.AgentMessage) []*schema.Message {
	result := make([]*schema.Message, len(msgs))
	for i, msg := range msgs {
		result[i] = AgentMessageToEinoMessage(msg)
	}
	return result
}

func AgentMessageToEinoMessage(msg agent.AgentMessage) *schema.Message {
	result := &schema.Message{
		Role:       roleToEino(msg.Role),
		ToolCallID: msg.ToolCallID,
		ToolName:   msg.ToolName,
		Content:    "",
	}

	for _, block := range msg.Content {
		switch content := block.(type) {
		case agent.TextContent:
			result.Content += content.Text
		case agent.ToolCallContent:
			if result.ToolCalls == nil {
				result.ToolCalls = []schema.ToolCall{}
			}
			result.ToolCalls = append(result.ToolCalls, schema.ToolCall{
				ID: content.ID,
				Function: schema.FunctionCall{
					Name:      content.Name,
					Arguments: argsToString(content.Arguments),
				},
			})
		case agent.ImageContent:
			if result.UserInputMultiContent == nil {
				result.UserInputMultiContent = []schema.MessageInputPart{}
			}
			result.UserInputMultiContent = append(result.UserInputMultiContent, schema.MessageInputPart{
				Type: schema.ChatMessagePartTypeImageURL,
				Image: &schema.MessageInputImage{
					MessagePartCommon: schema.MessagePartCommon{
						Base64Data: &content.Data,
						MIMEType:   content.MimeType,
					},
				},
			})
		}
	}

	return result
}

func EinoMessageToAgentMessage(msg *schema.Message) agent.AgentMessage {
	result := agent.AgentMessage{
		Role:       roleFromEino(msg.Role),
		ToolCallID: msg.ToolCallID,
		ToolName:   msg.ToolName,
		Content:    []agent.ContentBlock{},
		Timestamp:  time.Now().Unix(),
	}

	switch msg.Role {
	case schema.Assistant:
		if len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				args := argsFromString(toolCall.Function.Arguments)
				result.Content = append(result.Content, agent.ToolCallContent{
					ID:        toolCall.ID,
					Name:      toolCall.Function.Name,
					Arguments: args,
				})
			}
		} else if msg.Content != "" {
			result.Content = append(result.Content, agent.TextContent{Text: msg.Content})
		}
	case schema.Tool:
		if msg.Content != "" {
			result.Content = append(result.Content, agent.TextContent{Text: msg.Content})
		}
	case schema.User:
		if len(msg.UserInputMultiContent) > 0 {
			for _, part := range msg.UserInputMultiContent {
				if part.Type == schema.ChatMessagePartTypeText && part.Text != "" {
					result.Content = append(result.Content, agent.TextContent{Text: part.Text})
				} else if part.Type == schema.ChatMessagePartTypeImageURL && part.Image != nil {
					result.Content = append(result.Content, agent.ImageContent{
						Data:     stringOrEmpty(part.Image.Base64Data),
						MimeType: part.Image.MIMEType,
					})
				}
			}
		} else if msg.Content != "" {
			result.Content = append(result.Content, agent.TextContent{Text: msg.Content})
		}
	case schema.System:
		if msg.Content != "" {
			result.Content = append(result.Content, agent.TextContent{Text: msg.Content})
		}
	}

	return result
}

func roleFromEino(role schema.RoleType) agent.Role {
	switch role {
	case schema.User:
		return agent.RoleUser
	case schema.Assistant:
		return agent.RoleAssistant
	case schema.Tool:
		return agent.RoleToolResult
	case schema.System:
		return agent.RoleUser
	default:
		return agent.RoleUser
	}
}

func roleToEino(role agent.Role) schema.RoleType {
	switch role {
	case agent.RoleUser:
		return schema.User
	case agent.RoleAssistant:
		return schema.Assistant
	case agent.RoleToolResult:
		return schema.Tool
	default:
		return schema.User
	}
}

func argsToString(args map[string]any) string {
	if args == nil {
		return ""
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func argsFromString(s string) map[string]any {
	if s == "" {
		return nil
	}
	var result map[string]any
	err := json.Unmarshal([]byte(s), &result)
	if err != nil {
		return nil
	}
	return result
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
