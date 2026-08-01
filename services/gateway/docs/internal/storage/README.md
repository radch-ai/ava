# Storage

S3-compatible storage for agent workspaces and conversation transcripts.

## Overview

```mermaid
flowchart TB
    StartLoad([Load]) --> NewWorkspace["workspace = &Workspace{}"]
    NewWorkspace --> LoadSoul["l.loadFile(SOUL.md) → soul, err"]
    LoadSoul -->|error| LoadAgents["l.loadFile(AGENTS.md)"]
    LoadSoul -->|ok| SetSoul["workspace.Soul = soul"]
    SetSoul --> LoadAgents
    LoadAgents -->|error| LoadUser["l.loadFile(USER.md)"]
    LoadAgents -->|ok| SetAgents["workspace.Agents = agents"]
    SetAgents --> LoadUser
    LoadUser -->|error| LoadIdentity["l.loadFile(IDENTITY.md)"]
    LoadUser -->|ok| SetUser["workspace.User = user"]
    SetUser --> LoadIdentity
    LoadIdentity -->|error| LoadTools["l.loadFile(TOOLS.md)"]
    LoadIdentity -->|ok| SetIdentity["workspace.Identity = identity"]
    SetIdentity --> LoadTools
    LoadTools -->|error| DoneLoad(["return workspace"])
    LoadTools -->|ok| SetTools["workspace.Tools = tools"]
    SetTools --> DoneLoad

    StartLoadFile([loadFile]) --> BuildPath["WorkspacePath(userID, agentID, filename) → path"]
    BuildPath --> GetFile["l.client.Get(ctx, path) → data, err"]
    GetFile -->|error| ErrFile(["return empty, error"])
    GetFile -->|ok| ToString["string(data)"]
    ToString --> DoneFile(["return content, nil"])

    StartWriteTurn([WriteTurn]) --> TurnToJSONL["TurnToJSONL(turn) → line"]
    TurnToJSONL --> BuildPath2["TranscriptPath(userID, agentID, sessionID) → path"]
    BuildPath2 --> Append["w.client.Append(ctx, path, []byte(line))"]
    Append --> DoneWrite(["return err"])

    StartTurnToJSONL([TurnToJSONL]) --> BuildMessages["build messages"]
    BuildMessages --> BuildTools["build tools"]
    BuildTools --> Marshal["json.Marshal(data) → line, err"]
    Marshal -->|error| ErrMarshal(["return error JSONL"])
    Marshal -->|ok| AddNewline["line + newline"]
    AddNewline --> DoneJSONL(["return string"])

    click LoadSoul href "#Storage"
    click LoadAgents href "#Storage"
    click LoadUser href "#Storage"
    click LoadIdentity href "#Storage"
    click LoadTools href "#Storage"
    click GetFile href "#Storage"
    click Append href "#Storage"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class StartLoad,DoneLoad,StartLoadFile,DoneFile,StartWriteTurn,DoneWrite,StartTurnToJSONL,DoneJSONL mainFn
    class LoadSoul,LoadAgents,LoadUser,LoadIdentity,LoadTools,GetFile,Append localPkgs
    class ErrFile,ErrMarshal errorFlow
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

### WorkspacePath

```mermaid
flowchart TB
    Start(["WorkspacePath(userID, agentID, filename)"]) --> PathJoin["path.Join(userID, agentID, workspace, filename) → path"]
    PathJoin --> Return(["return path"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### TranscriptPath

```mermaid
flowchart TB
    Start(["TranscriptPath(userID, agentID, sessionID)"]) --> PathJoin["path.Join(userID, agentID, transcripts, sessionID + .jsonl) → path"]
    PathJoin --> Return(["return path"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### NewWorkspaceLoader

```mermaid
flowchart TB
    Start(["NewWorkspaceLoader(client)"]) --> CreateLoader["&WorkspaceLoader{client} → l"]
    CreateLoader --> Done(["return l"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Done mainFn
```

### Load

```mermaid
flowchart TB
    Start(["Load(ctx, userID, agentID)"]) --> CreateWorkspace["&Workspace{} → workspace"]
    CreateWorkspace --> LoadSoul["l.loadFile(ctx, userID, agentID, SOUL.md) → soul, err"]

    subgraph SGCheckSoul [" "]
        LoadSoul --> CheckSoulErr{err?}
        CheckSoulErr -->|no| SetSoul["workspace.Soul = soul"]
        SetSoul --> SetLoaded1["workspace.Loaded = true"]
        CheckSoulErr -->|yes| LoadAgents
        SetLoaded1 --> LoadAgents
    end

    LoadAgents --> LoadAgentsFile["l.loadFile(ctx, userID, agentID, AGENTS.md) → agents, err"]

    subgraph SGCheckAgents [" "]
        LoadAgentsFile --> CheckAgentsErr{err?}
        CheckAgentsErr -->|no| SetAgents["workspace.Agents = agents"]
        SetAgents --> SetLoaded2["workspace.Loaded = true"]
        CheckAgentsErr -->|yes| LoadUser
        SetLoaded2 --> LoadUser
    end

    LoadUser --> LoadUserFile["l.loadFile(ctx, userID, agentID, USER.md) → user, err"]

    subgraph SGCheckUser [" "]
        LoadUserFile --> CheckUserErr{err?}
        CheckUserErr -->|no| SetUser["workspace.User = user"]
        SetUser --> SetLoaded3["workspace.Loaded = true"]
        CheckUserErr -->|yes| LoadIdentity
        SetLoaded3 --> LoadIdentity
    end

    LoadIdentity --> LoadIdentityFile["l.loadFile(ctx, userID, agentID, IDENTITY.md) → identity, err"]

    subgraph SGCheckIdentity [" "]
        LoadIdentityFile --> CheckIdentityErr{err?}
        CheckIdentityErr -->|no| SetIdentity["workspace.Identity = identity"]
        SetIdentity --> SetLoaded4["workspace.Loaded = true"]
        CheckIdentityErr -->|yes| LoadTools
        SetLoaded4 --> LoadTools
    end

    LoadTools --> LoadToolsFile["l.loadFile(ctx, userID, agentID, TOOLS.md) → tools, err"]

    subgraph SGCheckTools [" "]
        LoadToolsFile --> CheckToolsErr{err?}
        CheckToolsErr -->|no| SetTools["workspace.Tools = tools"]
        SetTools --> SetLoaded5["workspace.Loaded = true"]
        CheckToolsErr -->|yes| Return
        SetLoaded5 --> Return
    end

    Return(["return workspace, nil"])

    click LoadSoul href "#loadFile"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Return mainFn
    class LoadSoul,LoadAgentsFile,LoadUserFile,LoadIdentityFile,LoadToolsFile localPkgs
```

### loadFile

```mermaid
flowchart TB
    Start(["loadFile(ctx, userID, agentID, filename)"]) --> WorkspacePathCall["WorkspacePath(userID, agentID, filename) → path"]
    WorkspacePathCall --> GetFile["l.client.Get(ctx, path) → data, err"]
    GetFile --> CheckErr{err?}
    CheckErr -->|yes| ReturnErr(["return '', fmt.Errorf(file not found)"])
    CheckErr -->|no| ToString["string(data) → content"]
    ToString --> Return(["return content, nil"])

    click WorkspacePathCall href "#WorkspacePath"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class WorkspacePathCall localPkgs
    class ReturnErr errorFlow
```

### FormatSystemPrompt

```mermaid
flowchart TB
    Start(["FormatSystemPrompt()"]) --> MakeBuilder["strings.Builder{} → prompt"]
    MakeBuilder --> CheckSoul{w.Soul?}
    CheckSoul -->|yes| WriteSoul["prompt.WriteString(w.Soul + \n\n)"]
    CheckSoul -->|no| CheckAgents
    WriteSoul --> CheckAgents{w.Agents?}
    CheckAgents -->|yes| WriteAgents["prompt.WriteString(w.Agents + \n\n)"]
    CheckAgents -->|no| CheckUser
    WriteAgents --> CheckUser{w.User?}

    subgraph SGWriteUser [" "]
        CheckUser -->|yes| WriteUserHeader["prompt.WriteString(## User Information\n\n)"]
        WriteUserHeader --> WriteUser["prompt.WriteString(w.User + \n\n)"]
        CheckUser -->|no| CheckIdentity
        WriteUser --> CheckIdentity
    end

    CheckIdentity -->|yes| WriteIdentityHeader["prompt.WriteString(## Identity\n\n)"]
    CheckIdentity -->|no| CheckTools{w.Tools?}
    WriteIdentityHeader --> WriteIdentity["prompt.WriteString(w.Identity + \n\n)"]
    WriteIdentity --> CheckTools

    CheckTools -->|yes| WriteToolsHeader["prompt.WriteString(## Tool Guidance\n\n)"]
    CheckTools -->|no| Return
    WriteToolsHeader --> WriteTools["prompt.WriteString(w.Tools + \n\n)"]
    WriteTools --> Return

    Return(["return prompt.String()"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### NewTranscriptWriter

```mermaid
flowchart TB
    Start(["NewTranscriptWriter(client)"]) --> CreateWriter["&TranscriptWriter{client} → w"]
    CreateWriter --> Done(["return w"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Done mainFn
```

### WriteTurn

```mermaid
flowchart TB
    Start(["WriteTurn(ctx, turn)"]) --> TurnToJSONLCall["TurnToJSONL(turn) → line"]
    TurnToJSONLCall --> TranscriptPathCall["TranscriptPath(turn.UserID, turn.AgentID, turn.SessionID) → path"]
    TranscriptPathCall --> Append["w.client.Append(ctx, path, []byte(line)) → err"]
    Append --> Return(["return err"])

    click TurnToJSONLCall href "#TurnToJSONL"
    click TranscriptPathCall href "#TranscriptPath"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Return mainFn
    class TurnToJSONLCall,TranscriptPathCall localPkgs
```

### TurnToJSONL

```mermaid
flowchart TB
    Start(["TurnToJSONL(turn)"]) --> MakeMessages["make([]map[string]any, len(turn.Messages)) → messages"]
    MakeMessages --> LoopMessages["for turn.Messages"]

    subgraph SGLoopMessages [" "]
        LoopMessages --> GetMessage["msg := range"]
        GetMessage --> MakeMsgMap["map[string]any{role, content, timestamp} → msgMap"]
        MakeMsgMap --> CheckToolCallID{msg.ToolCallID?}
        CheckToolCallID -->|yes| SetToolCallID["msgMap[tool_call_id] = msg.ToolCallID"]
        CheckToolCallID -->|no| CheckToolName
        SetToolCallID --> CheckToolName{msg.ToolName?}
        CheckToolName -->|yes| SetToolName["msgMap[tool_name] = msg.ToolName"]
        CheckToolName -->|no| CheckIsError
        SetToolName --> CheckIsError{msg.IsError?}
        CheckIsError -->|yes| SetIsError["msgMap[is_error] = true"]
        CheckIsError -->|no| SetMessage
        SetIsError --> SetMessage["messages[i] = msgMap"]
        CheckIsError -->|no| SetMessage
        SetMessage --> NextMessage["i++"]
        NextMessage --> LoopMessages
    end

    LoopMessages -->|done| MakeTools["make([]map[string]any, len(turn.Tools)) → tools"]
    MakeTools --> LoopTools["for turn.Tools"]

    subgraph SGLoopTools [" "]
        LoopTools --> GetTool["tool := range"]
        GetTool --> MakeToolMap["map[string]any{id, name, args, result, isError} → toolMap"]
        MakeToolMap --> SetTool["tools[i] = toolMap"]
        SetTool --> NextTool["i++"]
        NextTool --> LoopTools
    end

    LoopTools -->|done| MakeData["map[string]any{turn, session_id, user_id, agent_id, messages, tools, timestamp} → data"]
    MakeData --> Marshal["json.Marshal(data) → line, err"]
    Marshal --> CheckErr{err?}
    CheckErr -->|yes| ReturnErr(["return fmt.Errorf(marshal failed)"])
    CheckErr -->|no| AddNewline["string(line) + \n → line"]
    AddNewline --> Return(["return line"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return,ReturnErr mainFn
    class LoopMessages,LoopTools internalFn
    class ReturnErr errorFlow
```

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
- [`internal/storage/s3.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/storage/s3.go) - S3 client interface
- [`internal/storage/workspace.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/storage/workspace.go) - Workspace loader
- [`internal/storage/transcript.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/storage/transcript.go) - Transcript writer

## Related

- [Agent Tools](../agent/tools.md) - File tools use storage
- [Session](../session/README.md) - Session transcript integration
