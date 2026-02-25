# Mem0

Mem0 API client for long-term memory storage and retrieval.

## Overview

```mermaid
flowchart TB
    StartAdd([Add]) --> Lock["m.mu.Lock()"]
    Lock --> NewUUID["uuid.New().String() → id"]
    NewUUID --> BuildMem["build Memory"]
    BuildMem --> StoreMem["m.memories[id] = memory"]
    StoreMem --> AppendUser["m.byUser[userID] = append(...)"]
    AppendUser --> CheckRun{runID?}
    CheckRun -->|yes| AppendRun["m.byRun[runID] = append(...)"]
    CheckRun -->|no| UnlockAdd["m.mu.Unlock()"]
    AppendRun --> UnlockAdd
    UnlockAdd --> DoneAdd(["return nil"])

    StartSearch([Search]) --> LockRead["m.mu.RLock()"]
    LockRead --> GetByUser["m.byUser[userID] → userMemories"]
    GetByUser --> CheckExists{exists?}
    CheckExists -->|no| UnlockSearch1["m.mu.RUnlock()"]
    UnlockSearch1 --> ReturnEmpty(["return []string, nil"])
    CheckExists -->|yes| LoopUser["for userMemories"]
    LoopUser --> CheckRunEmpty{mem.RunID?}
    CheckRunEmpty -->|set| SkipMem(["continue"])
    CheckRunEmpty -->|empty| CheckContains{strings.Contains?}
    CheckContains -->|yes| AppendResults["results = append(...)"]
    CheckContains -->|no| SkipMem
    AppendResults --> LoopUser
    LoopUser -->|done| UnlockSearch2["m.mu.RUnlock()"]
    UnlockSearch2 --> ReturnResults(["return results, nil"])

    StartSearchSession([SearchInSession]) --> LockRead3["m.mu.RLock()"]
    LockRead3 --> GetByRun["m.byRun[runID] → runMemories"]
    GetByRun --> CheckExists3{exists?}
    CheckExists3 -->|no| UnlockSearch3["m.mu.RUnlock()"]
    UnlockSearch3 --> ReturnEmpty3(["return []string, nil"])
    CheckExists3 -->|yes| LoopRun["for runMemories"]
    LoopRun --> CheckUserID{mem.UserID==userID?}
    CheckUserID -->|no| SkipRun(["continue"])
    CheckUserID -->|yes| CheckContains3{strings.Contains?}
    CheckContains3 -->|yes| AppendResults3["results = append(...)"]
    CheckContains3 -->|no| SkipRun
    AppendResults3 --> LoopRun
    LoopRun -->|done| UnlockSearch4["m.mu.RUnlock()"]
    UnlockSearch4 --> ReturnResults3(["return results, nil"])

    StartCreate([CreateSession]) --> NewUUID2["uuid.New().String() → runID"]
    NewUUID2 --> DoneCreate(["return runID, nil"])

    StartDelete([DeleteSession]) --> Lock4["m.mu.Lock()"]
    Lock4 --> GetByRun4["m.byRun[runID] → runMemories"]
    GetByRun4 --> CheckExists4{exists?}
    CheckExists4 -->|no| UnlockDelete["m.mu.Unlock()"]
    UnlockDelete --> DoneDelete(["return nil"])
    CheckExists4 -->|yes| LoopDelete["for runMemories"]
    LoopDelete --> DeleteMem["delete(m.memories, mem.ID)"]
    DeleteMem --> LoopDelete
    LoopDelete -->|done| DeleteRun["delete(m.byRun, runID)"]
    DeleteRun --> FilterUser["filter byUser[userID]"]
    FilterUser --> UnlockDelete2["m.mu.Unlock()"]
    UnlockDelete2 --> DoneDelete

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class StartAdd,DoneAdd,StartSearch,ReturnEmpty,ReturnResults,StartSearchSession,ReturnEmpty3,ReturnResults3,StartCreate,DoneCreate,StartDelete,DoneDelete,SkipMem,SkipRun mainFn
```

## Structs

### Memory

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Memory` | `ID` | `string` | Memory identifier |
| | `UserID` | `string` | User identifier |
| | `RunID` | `string` | Session identifier (optional) |
| | `Content` | `string` | Memory content |

### MockClient

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `MockClient` | `mu` | `sync.RWMutex` | Thread-safe lock |
| | `memories` | `map[string]*Memory` | Memory storage |
| | `byUser` | `map[string][]*Memory` | User index |
| | `byRun` | `map[string][]*Memory` | Session index |

### APIClient

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `APIClient` | `baseURL` | `string` | API base URL |
| | `apiKey` | `string` | API key |

## Interfaces

### Client

```go
type Client interface {
    Add(ctx context.Context, userID, content, runID string) error
    Search(ctx context.Context, userID, query string) ([]string, error)
    SearchInSession(ctx context.Context, userID, runID, query string) ([]string, error)
    CreateSession(ctx context.Context, userID string) (string, error)
    DeleteSession(ctx context.Context, userID, runID string) error
}
```

## Functions

### NewMockClient

```mermaid
flowchart TB
    Start(["NewMockClient()"]) --> MakeMemories["make(map[string]*Memory) → memories"]
    MakeMemories --> MakeByUser["make(map[string][]*Memory) → byUser"]
    MakeByUser --> MakeByRun["make(map[string][]*Memory) → byRun"]

    subgraph SGCreateClient [" "]
        MakeByRun --> CreateClient["&MockClient{memories, byUser, byRun} → c"]
        CreateClient --> Done(["return c"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Done mainFn
    class CreateClient internalFn
```

### MockClient.Add

```mermaid
flowchart TB
    Start(["Add(ctx, userID, content, runID)"]) --> Lock["m.mu.Lock()"]
    Lock --> NewUUID["uuid.New().String() → id"]

    subgraph SGCreateMemory [" "]
        NewUUID --> CreateMemory["&Memory{id, userID, runID, content} → memory"]
        CreateMemory --> StoreMemory["m.memories[id] = memory"]
    end

    StoreMemory --> AppendUser["m.byUser[userID] = append(m.byUser[userID], memory)"]
    AppendUser --> CheckRunID{runID?}
    CheckRunID -->|yes| AppendRun["m.byRun[runID] = append(m.byRun[runID], memory)"]
    CheckRunID -->|no| Unlock
    AppendRun --> Unlock["m.mu.Unlock()"]
    Unlock --> Return(["return nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return mainFn
    class CreateMemory,StoreMemory internalFn
```

### MockClient.Search

```mermaid
flowchart TB
    Start(["Search"]) --> RLock["m.mu.RLock()"]
    RLock --> GetByUser["m.byUser[userID] → userMemories"]
    GetByUser --> CheckExists{exists?}
    CheckExists -->|no| RUnlock["m.mu.RUnlock()"]
    RUnlock --> ReturnEmpty(["return []string{}, nil"])
    CheckExists -->|yes| MakeResults["make([]string, 0) → results"]
    MakeResults --> ToLower["strings.ToLower(query) → queryLower"]
    ToLower --> LoopMemories["for userMemories"]

    subgraph SGLoopMemories1 [" "]
        LoopMemories --> GetMemory["mem := range"]
        GetMemory --> CheckRunID{mem.RunID?}
        CheckRunID -->|set| NextMemory["continue"]
        CheckRunID -->|empty| CheckContains{contains?}
        CheckContains -->|yes| AppendResults["results = append(results, mem.Content)"]
        CheckContains -->|no| NextMemory
        AppendResults --> NextMemory
        NextMemory --> LoopMemories
    end

    LoopMemories -->|done| RUnlock2["m.mu.RUnlock()"]
    RUnlock2 --> ReturnResults(["return results, nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,ReturnEmpty,ReturnResults mainFn
    class LoopMemories,GetMemory internalFn
```

### MockClient.SearchInSession

```mermaid
flowchart TB
    Start(["SearchInSession"]) --> RLock["m.mu.RLock()"]
    RLock --> GetByRun["m.byRun[runID] → runMemories"]
    GetByRun --> CheckExists{exists?}
    CheckExists -->|no| RUnlock["m.mu.RUnlock()"]
    RUnlock --> ReturnEmpty(["return []string{}, nil"])
    CheckExists -->|yes| MakeResults["make([]string, 0) → results"]
    MakeResults --> ToLower["strings.ToLower(query) → queryLower"]
    ToLower --> LoopMemories["for runMemories"]

    subgraph SGLoopMemories2 [" "]
        LoopMemories --> GetMemory["mem := range"]
        GetMemory --> CheckUserID{mem.UserID==userID?}
        CheckUserID -->|no| NextMemory["continue"]
        CheckUserID -->|yes| CheckContains{contains?}
        CheckContains -->|yes| AppendResults["results = append(results, mem.Content)"]
        CheckContains -->|no| NextMemory
        AppendResults --> NextMemory
        NextMemory --> LoopMemories
    end

    LoopMemories -->|done| RUnlock2["m.mu.RUnlock()"]
    RUnlock2 --> ReturnResults(["return results, nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,ReturnEmpty,ReturnResults mainFn
    class LoopMemories,GetMemory internalFn
```

### MockClient.CreateSession

```mermaid
flowchart TB
    Start(["CreateSession(ctx, userID)"]) --> NewUUID["uuid.New().String() → runID"]
    NewUUID --> Return(["return runID, nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### MockClient.DeleteSession

```mermaid
flowchart TB
    Start(["DeleteSession(ctx, userID, runID)"]) --> Lock["m.mu.Lock()"]
    Lock --> GetByRun["m.byRun[runID] → runMemories"]
    GetByRun --> CheckExists{exists?}
    CheckExists -->|no| Unlock["m.mu.Unlock()"]
    Unlock --> Return(["return nil"])
    CheckExists -->|yes| LoopMemories["for runMemories"]

    subgraph SGLoopMemories [" "]
        LoopMemories --> GetMemory["mem := range"]
        GetMemory --> DeleteMemory["delete(m.memories, mem.ID)"]
        DeleteMemory --> NextMemory
        NextMemory --> LoopMemories
    end

    LoopMemories -->|done| DeleteByRun["delete(m.byRun, runID)"]
    DeleteByRun --> MakeUpdated["make([]*Memory, 0) → updated"]
    MakeUpdated --> LoopUserMemories["for m.byUser[userID]"]

    subgraph SGLoopUserMemories [" "]
        LoopUserMemories --> GetUserMemory["mem := range"]
        GetUserMemory --> CheckRunID2{mem.RunID!=runID?}
        CheckRunID2 -->|yes| AppendUpdated["updated = append(updated, mem)"]
        CheckRunID2 -->|no| NextUserMemory["continue"]
        AppendUpdated --> NextUserMemory
        NextUserMemory --> LoopUserMemories
    end

    LoopUserMemories -->|done| SetByUser["m.byUser[userID] = updated"]
    SetByUser --> Unlock2["m.mu.Unlock()"]
    Unlock2 --> ReturnDone(["return nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return,ReturnDone mainFn
    class LoopMemories,LoopUserMemories internalFn
```

### NewAPIClient

```mermaid
flowchart TB
    Start(["NewAPIClient(baseURL, apiKey)"]) --> CreateClient["&APIClient{baseURL, apiKey} → c"]
    CreateClient --> Done(["return c"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Done mainFn
```

### APIClient.Add

```mermaid
flowchart TB
    Start(["Add(ctx, userID, content, runID)"]) --> Return(["return fmt.Errorf(not implemented)"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class Return errorFlow
```

### APIClient.Search

```mermaid
flowchart TB
    Start(["Search(ctx, userID, query)"]) --> Return(["return nil, fmt.Errorf(not implemented)"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class Return errorFlow
```

### APIClient.SearchInSession

```mermaid
flowchart TB
    Start(["SearchInSession(ctx, userID, runID, query)"]) --> Return(["return nil, fmt.Errorf(not implemented)"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class Return errorFlow
```

### APIClient.CreateSession

```mermaid
flowchart TB
    Start(["CreateSession(ctx, userID)"]) --> Return(["return '', fmt.Errorf(not implemented)"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class Return errorFlow
```

### APIClient.DeleteSession

```mermaid
flowchart TB
    Start(["DeleteSession(ctx, userID, runID)"]) --> Return(["return fmt.Errorf(not implemented)"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class Return errorFlow
```

## Design Notes

### Long-term vs Session Memory

**Q: What's the difference between runID="" and runID set?**

| Aspect | Description |
|--------|-------------|
| Context | Memory scoping |
| Options considered | Separate methods, runID flag, namespaces |
| Rationale | runID="" for long-term, runID set for session |

**Current approach is recommended because:**
- Simple API with one storage method
- Empty runID clearly indicates long-term
- Session-scoped memories auto-cleanup
- Search can filter by scope

### Mock Client for Testing

**Q: Why have mock client instead of real API for tests?**

| Aspect | Description |
|--------|-------------|
| Context | Test isolation |
| Options considered | Real API, mock, interface stub |
| Rationale | No external dependencies in unit tests |

**Current approach is recommended because:**
- Tests run without Mem0 service
- Deterministic behavior
- Fast execution
- Full control over test data

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestMockClient_Add` | Memory storage |
| `TestMockClient_Search` | Long-term search |
| `TestMockClient_SearchInSession` | Session search |
| `TestMockClient_CreateSession` | Session creation |
| `TestMockClient_DeleteSession` | Session deletion |

**Running:**
```bash
make test ./internal/mem0
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/google/uuid` | UUID generation |

**File Location:** [`internal/mem0/client.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/mem0/client.go)

## Related

- [Agent Tools](../agent/tools.md) - Mem0 tools use client
- [Session](../session/README.md) - Session ID for runID
