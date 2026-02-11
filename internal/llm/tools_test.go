// Package llm provides LLM provider integration and tool format conversion.
package llm

import (
	"encoding/json"
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// Test helper to create a sample tool for testing
func createTestTool(name, description string) agent.Tool {
	return agent.Tool{
		Name:        name,
		Description: description,
		Execute:     nil, // Not needed for schema conversion tests
	}
}

// TestOpenAIToolFormat tests conversion to OpenAI function format
func TestOpenAIToolFormat(t *testing.T) {
	tests := []struct {
		name        string
		tool        agent.Tool
		wantType    string
		wantName    string
		wantDesc    string
		checkParams bool
	}{
		{
			name:        "simple tool without parameters",
			tool:        createTestTool("calculator", "Performs basic arithmetic operations"),
			wantType:    "function",
			wantName:    "calculator",
			wantDesc:    "Performs basic arithmetic operations",
			checkParams: true, // Should have empty parameters object
		},
		{
			name:        "tool with name and description",
			tool:        createTestTool("weather_fetcher", "Gets current weather for a location"),
			wantType:    "function",
			wantName:    "weather_fetcher",
			wantDesc:    "Gets current weather for a location",
			checkParams: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToOpenAITool(tt.tool)

			// Marshal and unmarshal to verify JSON structure
			data, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var gotMap map[string]interface{}
			if err := json.Unmarshal(data, &gotMap); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Check type field
			if gotType, ok := gotMap["type"].(string); !ok || gotType != tt.wantType {
				t.Errorf("type = %v, want %s", gotMap["type"], tt.wantType)
			}

			// Check function object
			function, ok := gotMap["function"].(map[string]interface{})
			if !ok {
				t.Fatal("function field is missing or not a map")
			}

			// Check name
			if name, ok := function["name"].(string); !ok || name != tt.wantName {
				t.Errorf("function.name = %v, want %s", function["name"], tt.wantName)
			}

			// Check description
			if desc, ok := function["description"].(string); !ok || desc != tt.wantDesc {
				t.Errorf("function.description = %v, want %s", function["description"], tt.wantDesc)
			}

			// Check parameters exist and are valid
			if tt.checkParams {
				params, ok := function["parameters"].(map[string]interface{})
				if !ok {
					t.Error("function.parameters field is missing or not a map")
				} else if paramsType, ok := params["type"].(string); !ok || paramsType != "object" {
					t.Errorf("parameters.type = %v, want object", params["type"])
				}
			}
		})
	}
}

// TestAnthropicToolFormat tests conversion to Anthropic tool format
func TestAnthropicToolFormat(t *testing.T) {
	tests := []struct {
		name        string
		tool        agent.Tool
		wantName    string
		wantDesc    string
		checkSchema bool
	}{
		{
			name:        "simple tool without parameters",
			tool:        createTestTool("search", "Searches the web"),
			wantName:    "search",
			wantDesc:    "Searches the web",
			checkSchema: true,
		},
		{
			name:        "tool with detailed description",
			tool:        createTestTool("file_reader", "Reads and returns file contents from the filesystem"),
			wantName:    "file_reader",
			wantDesc:    "Reads and returns file contents from the filesystem",
			checkSchema: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToAnthropicTool(tt.tool)

			// Marshal and unmarshal to verify JSON structure
			data, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var gotMap map[string]interface{}
			if err := json.Unmarshal(data, &gotMap); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Check name
			if name, ok := gotMap["name"].(string); !ok || name != tt.wantName {
				t.Errorf("name = %v, want %s", gotMap["name"], tt.wantName)
			}

			// Check description
			if desc, ok := gotMap["description"].(string); !ok || desc != tt.wantDesc {
				t.Errorf("description = %v, want %s", gotMap["description"], tt.wantDesc)
			}

			// Check input_schema exists
			if tt.checkSchema {
				schema, ok := gotMap["input_schema"].(map[string]interface{})
				if !ok {
					t.Error("input_schema field is missing or not a map")
				} else if schemaType, ok := schema["type"].(string); !ok || schemaType != "object" {
					t.Errorf("input_schema.type = %v, want object", schema["type"])
				}
			}
		})
	}
}

// TestGoogleToolFormat tests conversion to Google function declaration format
func TestGoogleToolFormat(t *testing.T) {
	tests := []struct {
		name        string
		tool        agent.Tool
		wantName    string
		wantDesc    string
		checkParams bool
	}{
		{
			name:        "simple tool",
			tool:        createTestTool("database_query", "Executes SQL queries"),
			wantName:    "database_query",
			wantDesc:    "Executes SQL queries",
			checkParams: true,
		},
		{
			name:        "tool with underscore name",
			tool:        createTestTool("api_caller", "Makes HTTP API calls"),
			wantName:    "api_caller",
			wantDesc:    "Makes HTTP API calls",
			checkParams: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToGoogleTool(tt.tool)

			// Marshal and unmarshal to verify JSON structure
			data, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var gotMap map[string]interface{}
			if err := json.Unmarshal(data, &gotMap); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			// Check name
			if name, ok := gotMap["name"].(string); !ok || name != tt.wantName {
				t.Errorf("name = %v, want %s", gotMap["name"], tt.wantName)
			}

			// Check description
			if desc, ok := gotMap["description"].(string); !ok || desc != tt.wantDesc {
				t.Errorf("description = %v, want %s", gotMap["description"], tt.wantDesc)
			}

			// Check parameters exist
			if tt.checkParams {
				params, ok := gotMap["parameters"].(map[string]interface{})
				if !ok {
					t.Error("parameters field is missing or not a map")
				} else if paramsType, ok := params["type"].(string); !ok || paramsType != "object" {
					t.Errorf("parameters.type = %v, want object", params["type"])
				}
			}
		})
	}
}

// TestGoogleToolsFormat tests conversion of multiple tools to Google format
func TestGoogleToolsFormat(t *testing.T) {
	tools := []agent.Tool{
		createTestTool("tool1", "First tool"),
		createTestTool("tool2", "Second tool"),
		createTestTool("tool3", "Third tool"),
	}

	got := ToGoogleTools(tools)

	// Marshal and unmarshal to verify JSON structure
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var gotMap map[string]interface{}
	if err := json.Unmarshal(data, &gotMap); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Check function_declarations array
	declarations, ok := gotMap["function_declarations"].([]interface{})
	if !ok {
		t.Fatal("function_declarations field is missing or not an array")
	}

	// Check count
	if len(declarations) != 3 {
		t.Errorf("len(function_declarations) = %d, want 3", len(declarations))
	}

	// Check each declaration has required fields
	for i, decl := range declarations {
		declMap, ok := decl.(map[string]interface{})
		if !ok {
			t.Errorf("declaration %d is not a map", i)
			continue
		}

		if _, hasName := declMap["name"]; !hasName {
			t.Errorf("declaration %d missing name field", i)
		}
		if _, hasDesc := declMap["description"]; !hasDesc {
			t.Errorf("declaration %d missing description field", i)
		}
		if _, hasParams := declMap["parameters"]; !hasParams {
			t.Errorf("declaration %d missing parameters field", i)
		}
	}
}

// TestEmptyParameters tests that tools without parameters generate valid empty parameter schemas
func TestEmptyParameters(t *testing.T) {
	tool := createTestTool("simple_tool", "A simple tool with no parameters")

	t.Run("OpenAI", func(t *testing.T) {
		openaiTool := ToOpenAITool(tool)
		data, _ := json.Marshal(openaiTool)

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to parse: %v", err)
		}

		function := parsed["function"].(map[string]interface{})
		params := function["parameters"].(map[string]interface{})

		if params["type"] != "object" {
			t.Errorf("parameters.type = %v, want object", params["type"])
		}
	})

	t.Run("Anthropic", func(t *testing.T) {
		anthropicTool := ToAnthropicTool(tool)
		data, _ := json.Marshal(anthropicTool)

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to parse: %v", err)
		}

		schema := parsed["input_schema"].(map[string]interface{})
		if schema["type"] != "object" {
			t.Errorf("input_schema.type = %v, want object", schema["type"])
		}
	})

	t.Run("Google", func(t *testing.T) {
		googleTool := ToGoogleTool(tool)
		data, _ := json.Marshal(googleTool)

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to parse: %v", err)
		}

		params := parsed["parameters"].(map[string]interface{})
		if params["type"] != "object" {
			t.Errorf("parameters.type = %v, want object", params["type"])
		}
	})
}

// TestJSONMarshalUnmarshal tests that converted tools can be marshaled and unmarshaled correctly
func TestJSONMarshalUnmarshal(t *testing.T) {
	tool := createTestTool("test_tool", "A test tool for JSON marshaling")

	t.Run("OpenAI", func(t *testing.T) {
		openaiTool := ToOpenAITool(tool)
		data, err := json.Marshal(openaiTool)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		var unmarshaled OpenAITool
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if unmarshaled.Function.Name != "test_tool" {
			t.Errorf("Name = %s, want test_tool", unmarshaled.Function.Name)
		}
		if unmarshaled.Function.Description != "A test tool for JSON marshaling" {
			t.Errorf("Description = %s, want 'A test tool for JSON marshaling'", unmarshaled.Function.Description)
		}
	})

	t.Run("Anthropic", func(t *testing.T) {
		anthropicTool := ToAnthropicTool(tool)
		data, err := json.Marshal(anthropicTool)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		var unmarshaled AnthropicTool
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if unmarshaled.Name != "test_tool" {
			t.Errorf("Name = %s, want test_tool", unmarshaled.Name)
		}
		if unmarshaled.Description != "A test tool for JSON marshaling" {
			t.Errorf("Description = %s, want 'A test tool for JSON marshaling'", unmarshaled.Description)
		}
	})

	t.Run("Google", func(t *testing.T) {
		googleTool := ToGoogleTool(tool)
		data, err := json.Marshal(googleTool)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		var unmarshaled GoogleTool
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if unmarshaled.Name != "test_tool" {
			t.Errorf("Name = %s, want test_tool", unmarshaled.Name)
		}
		if unmarshaled.Description != "A test tool for JSON marshaling" {
			t.Errorf("Description = %s, want 'A test tool for JSON marshaling'", unmarshaled.Description)
		}
	})
}
