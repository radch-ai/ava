// Package llm provides LLM provider integration and tool format conversion.
package llm

import (
	"github.com/nevaria-ai/gateway/internal/agent"
)

// OpenAITool represents OpenAI's function calling format.
type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFunction `json:"function"`
}

// OpenAIFunction represents a function in OpenAI's format.
type OpenAIFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  *JSONSchema `json:"parameters"`
}

// AnthropicTool represents Anthropic's tool format.
type AnthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema *JSONSchema `json:"input_schema"`
}

// GoogleTool represents Google's function declaration format.
type GoogleTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  *JSONSchema `json:"parameters"`
}

// GoogleTools wraps multiple tools in Google's format.
type GoogleTools struct {
	FunctionDeclarations []GoogleTool `json:"function_declarations"`
}

// JSONSchema represents a JSON Schema for tool parameters.
type JSONSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties,omitempty"`
	Required   []string                  `json:"required,omitempty"`
}

// PropertySchema represents a single property in JSON Schema.
type PropertySchema struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// emptyObjectSchema returns the default empty object schema for tools without parameters.
// A new instance is returned each time to avoid shared mutable state.
func emptyObjectSchema() *JSONSchema {
	return &JSONSchema{
		Type:       "object",
		Properties: make(map[string]PropertySchema),
	}
}

// openaiToolType is the constant type value for OpenAI function tools.
const openaiToolType = "function"

// ToOpenAITool converts an agent Tool to OpenAI function format.
func ToOpenAITool(tool agent.Tool) OpenAITool {
	return OpenAITool{
		Type: openaiToolType,
		Function: OpenAIFunction{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  emptyObjectSchema(),
		},
	}
}

// ToOpenAITools converts multiple agent Tools to OpenAI function format.
func ToOpenAITools(tools []agent.Tool) []OpenAITool {
	result := make([]OpenAITool, len(tools))
	for i, tool := range tools {
		result[i] = ToOpenAITool(tool)
	}
	return result
}

// ToAnthropicTool converts an agent Tool to Anthropic tool format.
func ToAnthropicTool(tool agent.Tool) AnthropicTool {
	return AnthropicTool{
		Name:        tool.Name,
		Description: tool.Description,
		InputSchema: emptyObjectSchema(),
	}
}

// ToAnthropicTools converts multiple agent Tools to Anthropic tool format.
func ToAnthropicTools(tools []agent.Tool) []AnthropicTool {
	result := make([]AnthropicTool, len(tools))
	for i, tool := range tools {
		result[i] = ToAnthropicTool(tool)
	}
	return result
}

// ToGoogleTool converts an agent Tool to Google function declaration format.
func ToGoogleTool(tool agent.Tool) GoogleTool {
	return GoogleTool{
		Name:        tool.Name,
		Description: tool.Description,
		Parameters:  emptyObjectSchema(),
	}
}

// ToGoogleTools converts multiple agent Tools to Google function declarations format.
func ToGoogleTools(tools []agent.Tool) GoogleTools {
	declarations := make([]GoogleTool, len(tools))
	for i, tool := range tools {
		declarations[i] = ToGoogleTool(tool)
	}
	return GoogleTools{
		FunctionDeclarations: declarations,
	}
}
