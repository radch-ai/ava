# Storage

S3-compatible storage for agent workspaces and conversation transcripts.

## Overview

```mermaid
flowchart TB
    subgraph Storage ["Storage Operations"]
        Put["Put(ctx, key, data)"]
        Get["Get(ctx, key) → []byte"]
        Append["Append(ctx, key, data)"]
        Delete["Delete(ctx, key)"]
    end

    subgraph Workspace ["Workspace Loader"]
        LoadFiles["Load(userID, agentID)"]
        SOUL["SOUL.md"]
        AGENTS["AGENTS.md"]
        USER["USER.md"]
        IDENTITY["IDENTITY.md"]
        TOOLS["TOOLS.md"]
        LoadFiles --> SOUL & AGENTS & USER & IDENTITY & TOOLS
    end

    subgraph Transcript ["Transcript Writer"]
        WriteTurn["WriteTurn(ctx, turn)"]
        JSONL["JSONL append"]
        WriteTurn --> JSONL
    end

    Storage --> S3["S3 Bucket"]
    Workspace --> Storage
    Transcript --> Storage

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Put,Get,Append,Delete,LoadFiles,WriteTurn internalFn
    class S3 localPkgs
```

## Structs

### Workspace

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Workspace` | `Soul` | `string` | Core agent personality |
| | `Agents` | `string` | Agent network config |
| | `User` | `string` | User-specific info |
| | `Identity` | `string` | Agent identity |
| | `Tools` | `string` | Tool guidance |
| | `Loaded` | `bool` | At least one file loaded |

### WorkspaceLoader

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `WorkspaceLoader` | `client` | `Client` | S3 client |

### Turn

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Turn` | `SessionID` | `string` | Session identifier |
| | `UserID` | `string` | User identifier |
| | `AgentID` | `string` | Agent identifier |
| | `TurnNumber` | `int` | Turn sequence number |
| | `Messages` | `[]agent.AgentMessage` | Conversation messages |
| | `Tools` | `[]ToolCall` | Tool calls in turn |
| | `Timestamp` | `int64` | Unix timestamp |

### ToolCall

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ToolCall` | `ID` | `string` | Tool call ID |
| | `Name` | `string` | Tool name |
| | `Args` | `map[string]any` | Tool arguments |
| | `Result` | `any` | Tool result |
| | `IsError` | `bool` | Error flag |

### TranscriptWriter

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `TranscriptWriter` | `client` | `Client` | S3 client |

## Interfaces

### Client

```go
type Client interface {
    Put(ctx context.Context, key string, data []byte) error
    Get(ctx context.Context, key string) ([]byte, error)
    Append(ctx context.Context, key string, data []byte) error
    Delete(ctx context.Context, key string) error
}
```

## Functions

### Path Helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `WorkspacePath` | `(userID, agentID, filename string) string` | S3 path for workspace file |
| `TranscriptPath` | `(userID, agentID, sessionID string) string` | S3 path for transcript |

### Workspace

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewWorkspaceLoader` | `(client Client) *WorkspaceLoader` | Creates loader |
| `Load` | `(ctx context.Context, userID, agentID string) (*Workspace, error)` | Loads workspace files |
| `FormatSystemPrompt` | `() string` | Formats as system prompt |

### Transcript

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewTranscriptWriter` | `(client Client) *TranscriptWriter` | Creates writer |
| `WriteTurn` | `(ctx context.Context, turn Turn) error` | Writes turn as JSONL |
| `TurnToJSONL` | `(turn Turn) string` | Converts to JSONL line |

## Error Handling

| Error Type | Handler | Behavior |
|------------|---------|----------|
| File not found | Returns empty string | Workspace files optional |
| S3 error | Returns error | Operation fails |

## Design Notes

### Missing Files as Empty

**Q: Why treat missing workspace files as empty instead of error?**

| Aspect | Description |
|--------|-------------|
| Context | Workspace file loading |
| Options considered | Error on missing, empty string, skip |
| Rationale | Not all agents need all files |

**Current approach is recommended because:**
- Agents can have minimal configuration
- New agents start with empty workspace
- No required file structure
- `Loaded` flag indicates if any files exist

### JSONL for Transcripts

**Q: Why use JSONL instead of JSON array?**

| Aspect | Description |
|--------|-------------|
| Context | Transcript format |
| Options considered | JSON array, JSONL, database |
| Rationale | Append-friendly, streamable, line-delimited |

**Current approach is recommended because:**
- Append operation is simple
- Can process while writing
- Each line is independent
- Standard format for logs/events

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestMockS3Client` | Mock client operations |
| `TestWorkspaceLoader` | File loading |
| `TestWorkspacePath` | Path generation |
| `TestTranscriptWriter` | Turn writing |
| `TestTurnToJSONL` | JSONL formatting |

**Running:**
```bash
make test ./internal/storage
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `internal/agent` | AgentMessage type |

**File Locations:**
- [`internal/storage/s3.go`](../../internal/storage/s3.go) - S3 client interface
- [`internal/storage/workspace.go`](../../internal/storage/workspace.go) - Workspace loader
- [`internal/storage/transcript.go`](../../internal/storage/transcript.go) - Transcript writer

## Related

- [Agent Tools](../agent/tools.md) - File tools use storage
- [Session](../session/README.md) - Session transcript integration
