# Gateway Implementation Plan

> **Workflow:** When a phase is complete:
> 1. Move details from brainstorming → `docs/` (proper project documentation)
> 2. Remove from this file
> 3. Keep only pending/in-progress work here
>
> **Phase 0: Foundation** → See [`../docs/foundation.md`](../docs/foundation.md)

## Phase 1: Core Gateway - MVP

**Status:** Pending

### Goals
- Build message protocol
- Implement connection management
- Add authentication layer
- Basic tool routing

### Message Protocol

- [ ] Define message JSON schema
- [ ] Message types: `tool_call`, `tool_response`, `auth`, `error`, `heartbeat`
- [ ] Message validation
- [ ] Error response format
- [ ] Version field for protocol evolution

### Connection Management

- [ ] Connection registry (active connections map)
- [ ] Device registration flow
- [ ] Capability discovery (what tools a device has)
- [ ] Heartbeat/ping-pong coordination

### Authentication

- [ ] OAuth 2.0 flow (Google, GitHub, Apple)
- [ ] JWT token generation/validation
- [ ] WebSocket auth handshake
- [ ] User session management

### Tool Routing

- [ ] Tool invocation protocol
- [ ] Cloud tool execution framework
- [ ] Relay service for device tools
- [ ] Response routing back to client
- [ ] Tool timeout handling
