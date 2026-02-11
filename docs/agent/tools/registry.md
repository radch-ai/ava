# Tool Registry

The tool registry provides thread-safe operations for registering and retrieving tools by name.

## Registry Type

```go
type Registry struct {
    mu    sync.RWMutex
    tools map[string]agent.Tool
}
```

### Fields

| Field | Description |
|-------|-------------|
| `mu` | Read-write mutex for thread-safe access |
| `tools` | Map of tool name to Tool |

## Creating a Registry

### NewRegistry

Creates a new tool registry, optionally initialized with tools.

```go
func NewRegistry(tools ...agent.Tool) *Registry
```

If tools are provided, they will be registered in the new registry using their names as keys.

#### Example

```go
// Empty registry
registry := NewRegistry()

// Registry with initial tools
registry := NewRegistry(
    ExampleEchoTool(),
    ExampleMathTool(),
)
```

## Registering Tools

### RegisterTool

Registers a tool in the registry.

```go
func RegisterTool(r *Registry, name string, tool agent.Tool) bool
```

### Return Value

- `true`: Tool was registered successfully
- `false`: A tool with that name already exists (the existing tool is preserved)

### Behavior

- Uses write lock for thread safety
- Preserves existing tool if name is already registered
- Does not modify the tool name (uses provided name parameter)

#### Example

```go
registry := NewRegistry()

success := RegisterTool(registry, "my_tool", agent.Tool{
    Name:        "my_tool",
    Description: "My custom tool",
    Execute:     myExecuteFunc,
})

if !success {
    log.Printf("Tool 'my_tool' already registered")
}
```

#### Duplicate Prevention

```go
registry := NewRegistry()

// First registration succeeds
success1 := RegisterTool(registry, "echo", ExampleEchoTool())
fmt.Println(success1) // true

// Second registration fails
success2 := RegisterTool(registry, "echo", ExampleEchoTool())
fmt.Println(success2) // false

// Original tool is preserved
tool := GetTool(registry, "echo")
// tool is the first one registered
```

## Retrieving Tools

### GetTool

Retrieves a tool by name from the registry.

```go
func GetTool(r *Registry, name string) *agent.Tool
```

### Return Value

- Pointer to the tool if found
- `nil` if tool is not found

### Behavior

- Uses read lock for thread safety (allows concurrent reads)
- Returns a pointer to the tool in the registry

#### Example

```go
registry := NewRegistry(ExampleEchoTool(), ExampleMathTool())

tool := GetTool(registry, "echo")
if tool == nil {
    log.Printf("Tool 'echo' not found")
    return
}

fmt.Printf("Found tool: %s - %s\n", tool.Name, tool.Description)
```

#### Not Found Handling

```go
tool := GetTool(registry, "unknown")
if tool == nil {
    log.Printf("Tool not found, registering...")
    RegisterTool(registry, "unknown", newTool)
}
```

## Default Tools

### DefaultTools

Returns a slice of all example tools.

```go
func DefaultTools() []agent.Tool
```

### Returned Tools

| Name | Description |
|------|-------------|
| `echo` | Echoes input text back |
| `math` | Performs arithmetic operations |
| `time` | Returns current time |
| `delay` | Simulates long-running operations |

#### Example

```go
tools := DefaultTools()

for _, tool := range tools {
    fmt.Printf("Tool: %s - %s\n", tool.Name, tool.Description)
}
// Output:
// Tool: echo - Echoes input text back to the caller
// Tool: math - Performs simple math operations: add, subtract, multiply, divide
// Tool: time - Returns current time in RFC3339 format
// Tool: delay - Waits for specified duration in milliseconds
```

## Complete Example

```go
func setupRegistry() *Registry {
    // Create registry with default tools
    registry := NewRegistry()

    // Register default tools
    for _, tool := range DefaultTools() {
        name := tool.Name
        if !RegisterTool(registry, name, tool) {
            log.Printf("Warning: tool %s already registered", name)
        }
    }

    // Add custom tool
    customTool := agent.Tool{
        Name:        "greet",
        Description: "Greets the user by name",
        Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
            name, ok := args["name"].(string)
            if !ok || name == "" {
                name = "World"
            }
            return &agent.ToolResult{
                Content: []agent.ContentBlock{agent.TextContent{Text: "Hello, " + name + "!"}},
            }, nil
        },
    }
    RegisterTool(registry, "greet", customTool)

    return registry
}

func useRegistry(registry *Registry, toolName string, args map[string]any) {
    // Get tool
    tool := GetTool(registry, toolName)
    if tool == nil {
        log.Printf("Tool '%s' not found\n", toolName)
        return
    }

    // Execute tool
    result, err := tool.Execute(context.Background(), args)
    if err != nil {
        log.Printf("Tool execution failed: %v\n", err)
        return
    }

    // Print result
    if len(result.Content) > 0 {
        if tc, ok := result.Content[0].(agent.TextContent); ok {
            fmt.Printf("Result: %s\n", tc.Text)
        }
    }
}
```

## Thread Safety

The registry provides thread-safe operations:

- **Read operations** (`GetTool`): Use read locks, allowing concurrent access
- **Write operations** (`RegisterTool`): Use write locks, blocking all access

This allows multiple goroutines to read tools simultaneously while safely registering new tools.

```go
// Concurrent reads (safe)
var wg sync.WaitGroup
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(name string) {
        defer wg.Done()
        tool := GetTool(registry, name)
        _ = tool
    }("echo")
}
wg.Wait()

// Concurrent writes (safe but serialized)
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(i int) {
        defer wg.Done()
        name := fmt.Sprintf("tool_%d", i)
        RegisterTool(registry, name, agent.Tool{Name: name})
    }(i)
}
wg.Wait()
```

## See Also

- [../types.md](../types.md) - Tool type definition
- [example.md](example.md) - Example tool implementations
- [README.md](README.md) - Tools subpackage overview
