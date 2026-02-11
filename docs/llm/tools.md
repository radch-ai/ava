# LLM Tools

This document describes the tool format conversion types and functions that enable unified function calling across different LLM providers.

## Overview

Each LLM provider uses a different format for function/tool calling:

- **OpenAI**: Uses a `function` type with a nested `function` object
- **Anthropic**: Uses a flat tool structure with `input_schema`
- **Google**: Uses `function_declarations` wrapped in a container

The tools package provides conversion functions to transform the internal `agent.Tool` format into each provider's native format.

## JSON Schema Types

### JSONSchema

```go
type JSONSchema struct {
    Type       string                 `json:"type"`
    Properties map[string]PropertySchema `json:"properties,omitempty"`
    Required   []string               `json:"required,omitempty"`
}
```

Represents a JSON Schema for tool parameters.

### PropertySchema

```go
type PropertySchema struct {
    Type        string   `json:"type"`
    Description string   `json:"description,omitempty"`
    Enum        []string `json:"enum,omitempty"`
}
```

Represents a single property in a JSON Schema.

## OpenAI Tool Format

### OpenAITool

```go
type OpenAITool struct {
    Type     string        `json:"type"`
    Function OpenAIFunction `json:"function"`
}
```

### OpenAIFunction

```go
type OpenAIFunction struct {
    Name        string    `json:"name"`
    Description string    `json:"description"`
    Parameters  *JSONSchema `json:"parameters"`
}
```

## Anthropic Tool Format

### AnthropicTool

```go
type AnthropicTool struct {
    Name        string    `json:"name"`
    Description string    `json:"description"`
    InputSchema *JSONSchema `json:"input_schema"`
}
```

## Google Tool Format

### GoogleTool

```go
type GoogleTool struct {
    Name        string    `json:"name"`
    Description string    `json:"description"`
    Parameters  *JSONSchema `json:"parameters"`
}
```

### GoogleTools

```go
type GoogleTools struct {
    FunctionDeclarations []GoogleTool `json:"function_declarations"`
}
```

Wraps multiple tools in Google's format.

## Conversion Functions

### ToOpenAITool

```go
func ToOpenAITool(tool agent.Tool) OpenAITool
```

Converts an agent `Tool` to OpenAI function format.

**Example:**
```go
tool := agent.Tool{
    Name: "get_weather",
    Description: "Get current weather for a location",
}

openaiTool := ToOpenAITool(tool)
// Returns: OpenAITool{Type: "function", Function: {...}}
```

### ToOpenAITools

```go
func ToOpenAITools(tools []agent.Tool) []OpenAITool
```

Converts multiple agent `Tool`s to OpenAI function format.

### ToAnthropicTool

```go
func ToAnthropicTool(tool agent.Tool) AnthropicTool
```

Converts an agent `Tool` to Anthropic tool format.

**Example:**
```go
tool := agent.Tool{
    Name: "search_database",
    Description: "Search the database",
}

anthropicTool := ToAnthropicTool(tool)
// Returns: AnthropicTool{Name: ..., InputSchema: {...}}
```

### ToAnthropicTools

```go
func ToAnthropicTools(tools []agent.Tool) []AnthropicTool
```

Converts multiple agent `Tool`s to Anthropic tool format.

### ToGoogleTool

```go
func ToGoogleTool(tool agent.Tool) GoogleTool
```

Converts an agent `Tool` to Google function declaration format.

**Example:**
```go
tool := agent.Tool{
    Name: "calculate",
    Description: "Perform a calculation",
}

googleTool := ToGoogleTool(tool)
// Returns: GoogleTool{Name: ..., Parameters: {...}}
```

### ToGoogleTools

```go
func ToGoogleTools(tools []agent.Tool) GoogleTools
```

Converts multiple agent `Tool`s to Google function declarations format.

**Example:**
```go
tools := []agent.Tool{
    {Name: "tool1", Description: "First tool"},
    {Name: "tool2", Description: "Second tool"},
}

googleTools := ToGoogleTools(tools)
// Returns: GoogleTools{FunctionDeclarations: [{tool1}, {tool2}]}
```

## Empty Object Schema

```go
func emptyObjectSchema() *JSONSchema
```

Returns a default empty object schema for tools without parameters. A new instance is returned each time to avoid shared mutable state.

**Note:** This is an internal function used by the conversion functions.

## Constants

```go
const openaiToolType = "function"
```

The constant type value for OpenAI function tools.

## Usage Example

```go
// Define tools
tools := []agent.Tool{
    {
        Name:        "get_weather",
        Description: "Get current weather for a location",
    },
    {
        Name:        "search",
        Description: "Search the web",
    },
}

// Convert for each provider
openaiTools := llm.ToOpenAITools(tools)
anthropicTools := llm.ToAnthropicTools(tools)
googleTools := llm.ToGoogleTools(tools)

// Use with client
events, err := client.Stream(ctx, messages, tools)
```

## Related

- [Factory](factory.md) - Client creation and Stream method
- [OpenAI Client](openai.md) - OpenAI tool calling implementation
- [Anthropic Client](anthropic.md) - Anthropic tool calling implementation
- [Google Client](google.md) - Google tool calling implementation
