// Package agent provides type conversion helpers between agent types and SDK types.
package agent

import (
	"github.com/nevaria-ai/llm-proxy-sdk/pkg/types"
)

// MessageFromSDK converts SDK Message to AgentMessage.
func MessageFromSDK(msg *types.Message) AgentMessage {
	if msg == nil {
		return AgentMessage{}
	}

	// Convert content blocks
	content := make([]ContentBlock, len(msg.Content))
	for i, block := range msg.Content {
		switch b := block.(type) {
		case types.TextContent:
			content[i] = TextContent{Text: b.Text}
		case types.ImageContent:
			content[i] = ImageContent{Data: b.Data, MimeType: b.MediaType}
		case types.ToolCallContent:
			content[i] = ToolCallContent{ID: b.ID, Name: b.Name, Arguments: b.Arguments}
		}
	}

	// Convert Role from SDK to agent
	var agentRole Role
	switch msg.Role {
	case types.RoleUser:
		agentRole = RoleUser
	case types.RoleAssistant:
		agentRole = RoleAssistant
	case types.RoleToolResult:
		agentRole = RoleToolResult
	}

	return AgentMessage{
		Role:       agentRole,
		Content:    content,
		Timestamp:  msg.Timestamp,
		ToolCallID: msg.ToolCallID,
		ToolName:   msg.ToolName,
		IsError:    msg.IsError,
	}
}

// MessageToSDK converts AgentMessage to SDK Message.
func MessageToSDK(msg AgentMessage) *types.Message {
	// Convert content blocks
	content := make([]types.ContentBlock, len(msg.Content))
	for i, block := range msg.Content {
		switch b := block.(type) {
		case TextContent:
			content[i] = types.TextContent{Text: b.Text}
		case ImageContent:
			content[i] = types.ImageContent{Data: b.Data, MediaType: b.MimeType}
		case ToolCallContent:
			content[i] = types.ToolCallContent{ID: b.ID, Name: b.Name, Arguments: b.Arguments}
		}
	}

	// Convert Role from agent to SDK
	var sdkRole types.Role
	switch msg.Role {
	case RoleUser:
		sdkRole = types.RoleUser
	case RoleAssistant:
		sdkRole = types.RoleAssistant
	case RoleToolResult:
		sdkRole = types.RoleToolResult
	}

	return &types.Message{
		Role:       sdkRole,
		Content:    content,
		Timestamp:  msg.Timestamp,
		ToolCallID: msg.ToolCallID,
		ToolName:   msg.ToolName,
		IsError:    msg.IsError,
	}
}

// MessagesFromSDK converts slice of SDK Messages to AgentMessages.
func MessagesFromSDK(msgs []*types.Message) []AgentMessage {
	result := make([]AgentMessage, len(msgs))
	for i, msg := range msgs {
		result[i] = MessageFromSDK(msg)
	}
	return result
}

// MessagesToSDK converts slice of AgentMessages to SDK Messages.
func MessagesToSDK(msgs []AgentMessage) []*types.Message {
	result := make([]*types.Message, len(msgs))
	for i, msg := range msgs {
		result[i] = MessageToSDK(msg)
	}
	return result
}

// ToolsFromSDK converts SDK Tools to agent Tools.
func ToolsFromSDK(sdkTools []types.Tool) []Tool {
	result := make([]Tool, len(sdkTools))
	for i, t := range sdkTools {
		result[i] = Tool{
			Name:        t.Name,
			Description: t.Description,
			// Execute function is not in SDK Tool, so we can't convert it
		}
	}
	return result
}

// ToolsToSDK converts agent Tools to SDK Tools.
func ToolsToSDK(agentTools []Tool) []types.Tool {
	result := make([]types.Tool, len(agentTools))
	for i, t := range agentTools {
		result[i] = types.Tool{
			Name:        t.Name,
			Description: t.Description,
		}
	}
	return result
}
