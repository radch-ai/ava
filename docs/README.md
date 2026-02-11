# Documentation

This directory contains all project documentation, consolidated for easy navigation and maintenance. All documentation is centralized here, making docs/ the single source of truth for the project.

## Documentation Structure

### Agent Framework Documentation

Comprehensive documentation for the agent framework in [agent/](agent/):

- **[agent/README.md](agent/README.md)** - Agent framework overview
  - Quick start guide and basic usage
  - Architecture overview and design principles

- **[agent/types.md](agent/types.md)** - Core type definitions
  - Agent state and configuration types
  - Request and response structures
  - Event and message types

- **[agent/agent.md](agent/agent.md)** - Agent implementation
  - Agent creation and initialization
  - State management and lifecycle
  - Tool execution and response handling

- **[agent/events.md](agent/events.md)** - Event system
  - Event types and propagation
  - Event handlers and middleware
  - Event-driven communication patterns

- **[agent/loop.md](agent/loop.md)** - Agent execution loop
  - Prompt generation and message handling
  - Tool execution flow
  - Error handling and recovery

- **[agent/tool.go.md](agent/tool.go.md)** - Tool interface
  - Tool definition and execution
  - Tool input/output handling
  - Custom tool implementation

- **[agent/mock.md](agent/mock.md)** - Testing utilities
  - Mock agent for testing
  - Test helpers and fixtures

#### Tool Documentation

- **[agent/tools/README.md](agent/tools/README.md)** - Built-in tools overview
- **[agent/tools/example.md](agent/tools/example.md)** - Example tool implementations
- **[agent/tools/registry.md](agent/tools/registry.md)** - Tool registry and registration

### LLM Client Documentation

LLM client interfaces and provider implementations in [llm/](llm/):

- **[llm/README.md](llm/README.md)** - LLM client overview
  - Unified client interface design
  - Quick start guide

- **[llm/types.md](llm/types.md)** - Core type definitions
  - Client interface and common types
  - Request and response structures
  - Error handling types

- **[llm/factory.md](llm/factory.md)** - Client factory
  - Provider selection and configuration
  - Client instantiation patterns

- **[llm/tools.md](llm/tools.md)** - Tool integration
  - Function calling support
  - Tool schema definitions

- **[llm/openai.md](llm/openai.md)** - OpenAI client
  - OpenAI API integration
  - Streaming support

- **[llm/anthropic.md](llm/anthropic.md)** - Anthropic client
  - Claude API integration
  - Message and streaming support

- **[llm/google.md](llm/google.md)** - Google client
  - Gemini API integration
  - Vertex AI support

### Project Package Documentation

Project metadata and build information in [project/](project/):

- **[project/README.md](project/README.md)** - Project package overview
  - Project metadata constants
  - Version information management
  - Build-time configuration

- **[project/details.md](project/details.md)** - Project details reference
  - Name, Slug, Description constants
  - ModulePath and BinaryName
  - Version variable with build-time injection

### Server Documentation

HTTP server implementation with health checks and WebSocket integration in [server/](server/):

- **[server/README.md](server/README.md)** - Server overview
  - Architecture and design
  - Graceful shutdown behavior
  - Endpoint documentation

- **[server/server.md](server/server.md)** - Server implementation
  - Server struct and constructor
  - Start and Shutdown methods
  - Signal handling and lifecycle

### WebSocket Handler Documentation

WebSocket connection management and protocol in [websocket/](websocket/):

- **[websocket/README.md](websocket/README.md)** - WebSocket overview
  - Architecture and design
  - Quick start guide

- **[websocket/handler.md](websocket/handler.md)** - Handler implementation
  - Connection handling
  - Message routing and dispatch

- **[websocket/agent_manager.md](websocket/agent_manager.md)** - Agent management
  - Agent lifecycle over WebSocket
  - Connection pooling and multiplexing

- **[websocket/protocol.md](websocket/protocol.md)** - Protocol specification
  - Message formats and types
  - Event streaming protocol
  - Error handling and status codes

### Configuration Documentation

Application configuration in [config/](config/):

- **[config/README.md](config/README.md)** - Configuration overview
  - Environment variable reference
  - Security considerations
  - Quick start guide

- **[config/config.md](config/config.md)** - Config types and functions
  - Config struct definition
  - Load function
  - Validate method

### High-Level Documentation

- **[foundation.md](foundation.md)** - Architecture and design patterns
  - High-level system architecture
  - Design decisions and patterns
  - Technology stack and rationale

- **[performance.md](performance.md)** - Benchmarks, scaling, and tuning
  - Memory optimization techniques
  - Horizontal and vertical scaling
  - Performance testing and monitoring

## Getting Started

1. **Start with architecture** - Read [foundation.md](foundation.md) for high-level understanding
2. **Configuration** - Review [config/README.md](config/README.md) for environment setup
3. **Explore agent framework** - Study [agent/README.md](agent/README.md) for core functionality
4. **Check LLM integration** - Review [llm/README.md](llm/README.md) for provider connections
5. **WebSocket implementation** - Study [websocket/README.md](websocket/README.md) for real-time communication
6. **Performance tuning** - Consult [performance.md](performance.md) for optimization

## Documentation Guidelines

- **Centralization**: All documentation lives in docs/ for easy discovery
- **Comprehensiveness**: Complete coverage of all features and APIs
- **Maintainability**: Documentation evolves with the codebase
- **Examples**: All code examples are tested and working
- **Cross-references**: Related topics are properly linked
- **Navigation**: Clear structure for easy information finding

## Quick Navigation

### Core Features
- [Agent Framework](agent/README.md) - AI agent creation and management
  - [Core Types](agent/types.md) - Type definitions
  - [Agent Implementation](agent/agent.md) - Agent logic
  - [Event System](agent/events.md) - Event handling
  - [Execution Loop](agent/loop.md) - Agent lifecycle
- [LLM Clients](llm/README.md) - Multi-provider LLM integration
  - [Client Types](llm/types.md) - Interface definitions
  - [Client Factory](llm/factory.md) - Provider selection
  - [OpenAI Client](llm/openai.md) - OpenAI integration
  - [Anthropic Client](llm/anthropic.md) - Claude integration
  - [Google Client](llm/google.md) - Gemini integration
- [Project Package](project/README.md) - Project metadata and build information
  - [Project Details](project/details.md) - Constants and version
- [Server](server/README.md) - HTTP server with health checks and WebSocket integration
  - [Server Implementation](server/server.md) - Server lifecycle and methods
- [WebSocket Handler](websocket/README.md) - Real-time communication
  - [Handler Implementation](websocket/handler.md) - Connection handling
  - [Agent Manager](websocket/agent_manager.md) - Agent lifecycle
  - [Protocol Specification](websocket/protocol.md) - Message formats
- [Configuration](config/README.md) - Application configuration
  - [Config Reference](config/config.md) - Types and functions

### Architecture
- [System Architecture](foundation.md) - Design patterns and decisions
- [Performance Guide](performance.md) - Optimization and scaling

## Development Workflow

### Creating New Documentation

When adding new features:
1. Create or update documentation in docs/
2. Include API references and examples
3. Add configuration options
4. Document error handling
5. Include performance considerations

### Updating Documentation

- Update documentation as part of code changes
- Ensure examples match current implementation
- Add new configuration options as they're introduced
- Update performance data as optimizations are made

### Contributing

- Follow the established documentation style
- Use consistent formatting and structure
- Include code examples for all APIs
- Document edge cases and error conditions
- Keep documentation up-to-date with code changes

## Related Resources

- [Repository](https://github.com/nevaria/gateway) - Source code and issue tracking
- [API Examples](examples/) - Working code examples
- [Contributing Guide](CONTRIBUTING.md) - How to contribute to the project
