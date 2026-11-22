package mcp

// The in-house MCP tool invokable: eino-ext's mcpext.GetTools builds the
// same adapter but lists unboundedly, and the catalog admission happens
// in this package. Constructing the tool here keeps one listing pass and one
// place where a remote name, its definition and its connection agree.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// remoteTool is one listed MCP tool as an eino invokable. Its Info carries
// the remote name and the advertised schema; wrappedTool renames it. The
// dispatch target is resolved through the owner's manager at call time,
// so a refresh that replaces the connection never leaves a live tool
// holding a dead client — and a listing staged before adoption (the
// install-time verification pass) still dispatches on the owner.
type remoteTool struct {
	owner *Client
	name  string
	info  *schema.ToolInfo
}

var _ tool.InvokableTool = (*remoteTool)(nil)

// newRemoteTool builds the adapter for one raw listing entry. A schema
// that cannot become eino's JSON-schema form is a security-invalid
// generation: the tool would be advertised without its argument contract.
func newRemoteTool(owner *Client, remote rawTool, description string) (*remoteTool, error) {
	raw := remote.InputSchema
	if len(raw) == 0 {
		raw = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	js := &jsonschema.Schema{}
	if err := json.Unmarshal(raw, js); err != nil {
		return nil, fmt.Errorf("tool %q input schema: %w", remote.Name, err)
	}
	// The struct's own UnmarshalJSON re-decodes with plain
	// encoding/json, which flattens the any-typed fields (default, const,
	// enum, examples) to float64 — values past 2^53 would silently round
	// in the advertised schema. The raw tree decoded with UseNumber is
	// authoritative; walk both and restore the exact literals.
	rawDec := json.NewDecoder(bytes.NewReader(raw))
	rawDec.UseNumber()
	var tree any
	if err := rawDec.Decode(&tree); err == nil {
		if obj, ok := tree.(map[string]any); ok {
			repairSchemaNumbers(js, obj)
		}
	}
	return &remoteTool{
		owner: owner,
		name:  remote.Name,
		info: &schema.ToolInfo{
			Name:        remote.Name,
			Desc:        description,
			ParamsOneOf: schema.NewParamsOneOfByJSONSchema(js),
		},
	}, nil
}

// repairSchemaNumbers restores exact numeric literals (json.Number) into
// every any-typed schema field and recurses through the structural
// sub-schemas. A raw key without a decoded counterpart is left alone —
// the struct decoder already owns shape and validation.
func repairSchemaNumbers(js *jsonschema.Schema, raw map[string]any) {
	if js == nil {
		return
	}
	if v, ok := raw["default"]; ok {
		js.Default = v
	}
	if v, ok := raw["const"]; ok {
		js.Const = v
	}
	if v, ok := raw["enum"].([]any); ok {
		js.Enum = v
	}
	if v, ok := raw["examples"].([]any); ok {
		js.Examples = v
	}
	if props, ok := raw["properties"].(map[string]any); ok && js.Properties != nil {
		for pair := js.Properties.Oldest(); pair != nil; pair = pair.Next() {
			if rawChild, ok := props[pair.Key].(map[string]any); ok {
				repairSchemaNumbers(pair.Value, rawChild)
			}
		}
	}
	for _, sub := range []struct {
		key   string
		field **jsonschema.Schema
	}{
		{"items", &js.Items},
		{"additionalProperties", &js.AdditionalProperties},
		{"propertyNames", &js.PropertyNames},
		{"contains", &js.Contains},
		{"not", &js.Not},
		{"if", &js.If},
		{"then", &js.Then},
		{"else", &js.Else},
		{"contentSchema", &js.ContentSchema},
	} {
		if rawChild, ok := raw[sub.key].(map[string]any); ok {
			repairSchemaNumbers(*sub.field, rawChild)
		}
	}
	for _, list := range []struct {
		key   string
		field []*jsonschema.Schema
	}{
		{"allOf", js.AllOf},
		{"anyOf", js.AnyOf},
		{"oneOf", js.OneOf},
		{"prefixItems", js.PrefixItems},
	} {
		if arr, ok := raw[list.key].([]any); ok {
			for i, rawChild := range arr {
				if i >= len(list.field) {
					break
				}
				if m, ok := rawChild.(map[string]any); ok {
					repairSchemaNumbers(list.field[i], m)
				}
			}
		}
	}
	for _, dicts := range []struct {
		key   string
		field map[string]*jsonschema.Schema
	}{
		{"$defs", js.Definitions},
		{"dependentSchemas", js.DependentSchemas},
		{"patternProperties", js.PatternProperties},
	} {
		if m, ok := raw[dicts.key].(map[string]any); ok {
			for name, child := range dicts.field {
				if rawChild, ok := m[name].(map[string]any); ok {
					repairSchemaNumbers(child, rawChild)
				}
			}
		}
	}
}

// Info reports the advertised definition captured at listing time.
func (t *remoteTool) Info(context.Context) (*schema.ToolInfo, error) { return t.info, nil }

// InvokableRun dispatches one tools/call and returns the marshaled result
// — the shape eino's tool node hands to the model. The live connection is
// resolved per call and borrowed for the duration, so a concurrent refresh
// retires the old connection only after this call returns. Request headers
// are applied by the connection itself, so no per-call header copy exists
// here. Bounds, scrubbing and authorization are the admission chain's.
func (t *remoteTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	if argumentsInJSON == "" {
		argumentsInJSON = "{}"
	}
	live := t.owner
	if mgr := t.owner.mgr; mgr != nil {
		current, ok := mgr.Get(t.owner.id)
		if !ok {
			return "", fmt.Errorf("mcp server %s is not connected; retry after a refresh", t.owner.id)
		}
		live = current
	}
	if err := live.borrow(); err != nil {
		return "", fmt.Errorf("mcp server %s: %w", t.owner.id, err)
	}
	defer live.release()
	result, err := live.cli.CallTool(ctx, mcpsdk.CallToolRequest{
		Request: mcpsdk.Request{Method: "tools/call"},
		Params: mcpsdk.CallToolParams{
			Name:      t.name,
			Arguments: json.RawMessage(argumentsInJSON),
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to call mcp tool: %w", err)
	}
	// A native text result is delivered as text, so the admission
	// chain's plain-text scrub and 64 KiB ceiling apply to what the model
	// reads, not to a JSON envelope around it.
	text, isText := singleTextContent(result)
	if result.IsError {
		if isText {
			return "", fmt.Errorf("failed to call mcp tool, mcp server return error: %s", text)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal mcp tool result: %w", err)
		}
		return "", fmt.Errorf("failed to call mcp tool, mcp server return error: %s", encoded)
	}
	if isText {
		return text, nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal mcp tool result: %w", err)
	}
	return string(encoded), nil
}

// singleTextContent reports the result's text when it carries exactly one
// text block: the shape a plain MCP tool answer has.
func singleTextContent(result *mcpsdk.CallToolResult) (string, bool) {
	if len(result.Content) != 1 {
		return "", false
	}
	text, ok := result.Content[0].(mcpsdk.TextContent)
	if !ok {
		return "", false
	}
	return text.Text, true
}
