# WebSocket

WebSocket connection handler with multi-agent routing, connection management, and agent lifecycle.

## Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `defaultBufferSize` | `int` | `8192` | 8KB read/write buffer per connection |
| `MaxMessageSize` | `int` | `65536` | 64KB max message size |
| `readTimeout` | `time.Duration` | `60s` | Read deadline timeout |
| `writeTimeout` | `time.Duration` | `10s` | Write deadline timeout |
| `pingInterval` | `time.Duration` | `30s` | Ping frame interval |

## Variables

| Variable | Type | Description |
|----------|------|-------------|
| `upgrader` | `websocket.Upgrader` | Package-level upgrader with 8KB buffers |
| `wsMsgPool` | `sync.Pool` | Pool for wsMessage structs |

## Structs

### Handler

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Handler` | `logger` | `zerolog.Logger` | Structured logger |
| | `upgrader` | `websocket.Upgrader` | HTTP to WebSocket upgrader |
| | `connSem` | `chan struct{}` | Semaphore for connection limiting |
| | `maxConns` | `int` | Maximum connections (0 = unlimited) |
| | `activeConns` | `int32` | Active connection count (atomic) |
| | `agentManager` | `*AgentManager` | Agent lifecycle manager |

### wsMessage

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `wsMessage` | `messageType` | `int` | WebSocket message type (text/binary/ping) |
| | `data` | `[]byte` | Message payload |

### wsEvent

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `wsEvent` | `Type` | `string` | Event type (connected, agent_start, etc.) |
| | `AgentID` | `string` | Agent identifier (optional) |
| | `Data` | `any` | Event payload |

## Files

| File | Purpose |
|------|---------|
| [`handler.md`](handler.md) | WebSocket connection handler with message routing |
| [`conn_wrapper.md`](conn_wrapper.md) | WebSocket connection wrapper for agent streaming |
| [`agent_manager.md`](agent_manager.md) | Agent lifecycle management with thread-safe access |

## Design Notes

### Single Writer Pattern

**Q: Why use a single writer goroutine for WebSocket writes?**

| Aspect | Description |
|--------|-------------|
| Context | gorilla/websocket is not thread-safe for concurrent writes |
| Options considered | Mutex protection, channel serialization, lock-free queue |
| Rationale | Channel serialization is cleaner and handles deadline management |

**Current approach is recommended because:**
- WebSocket protocol requires serialized writes
- Writer goroutine manages write deadlines centrally
- Naturally handles ping/pong frame interleaving
- Simplifies error handling and connection cleanup

### sync.Pool for Messages

**Q: Why use sync.Pool for wsMessage structs?**

| Aspect | Description |
|--------|-------------|
| Context | High-frequency message allocation in busy connections |
| Options considered | Stack allocation, sync.Pool, custom allocator |
| Rationale | Reduces GC pressure in high-traffic scenarios |

**Current approach is recommended because:**
- Messages are short-lived and frequently allocated
- Pool reuses memory across all connections
- Standard Go pattern for high-throughput systems
- Automatic GC of unused pool items

### Agents Persist Beyond Connection

**Q: Why do agents persist after WebSocket disconnects?**

| Aspect | Description |
|--------|-------------|
| Context | Mobile clients may lose connection frequently |
| Options considered | Destroy on disconnect, persist indefinitely, timed cleanup |
| Rationale | Users can reconnect and continue conversation seamlessly |

**Current approach is recommended because:**
- Better UX - conversation state preserved
- Workspace and memory retained between sessions
- Supports mobile app backgrounding scenarios
- Agents have internal idle timeout for automatic cleanup

### Composite Key for Multi-Agent

**Q: Why use (userID, agentID) composite key for agent storage?**

| Aspect | Description |
|--------|-------------|
| Context | Multiple agents per user, complete isolation required |
| Options considered | Single agentID key, composite key, hierarchical map |
| Rationale | Prevents agent ID collisions between users |

**Current approach is recommended because:**
- Clear ownership boundaries
- No agentID coordination needed between users
- Efficient single map lookup
- Natural multi-tenancy support

## Test Coverage

### Unit Tests (`tests/unit/websocket/`)

| Test Suite | Coverage |
|------------|----------|
| `TestWebSocketUpgrade` | HTTP to WebSocket upgrade |
| `TestWebSocketMessageLogging` | Message send/receive |
| `TestWebSocketOriginValidation` | Origin header validation |
| `TestWebSocketConnectionLimit` | Connection semaphore |
| `TestWebSocketBinaryMessage` | Binary message echo |
| `TestWebSocketPingPong` | Keepalive mechanism |
| `TestWebSocketCloseOnClientDisconnect` | Cleanup on disconnect |
| `TestActiveConns` | Active connection counter |
| `TestWebSocketMessageSizeLimit` | 64KB limit enforcement |
| `TestWebSocketMessageRequiresAgentID` | agent_id validation |
| `TestWebSocketMultipleAgentsPerConnection` | Multi-agent routing |
| `TestAgentManagerCreation` | AgentManager initialization |
| `TestGetOrCreateAgentForUser` | Agent creation on demand |
| `TestRemoveAgentForUser` | Agent cleanup |
| `TestGetAgent` | Agent retrieval with adapter |
| `TestAgentKeyCompositeKey` | Composite key behavior |

**Running:**
```bash
make test-unit TEST_PATH=./tests/unit/websocket/...
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/gorilla/websocket` | WebSocket protocol implementation |
| `github.com/google/uuid` | UUID generation for connection IDs |
| `github.com/rs/zerolog` | Structured logging |
| `internal/agent` | ADKAgent runtime |
| `internal/ctx` | Context utilities (userID extraction) |

**File Location:** [`internal/websocket/`](../../internal/websocket/)
