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

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewMockClient` | `() *MockClient` | Creates mock client |
| `NewAPIClient` | `(baseURL, apiKey string) *APIClient` | Creates API client |
| `Add` | `(ctx, userID, content, runID) error` | Stores memory |
| `Search` | `(ctx, userID, query) ([]string, error)` | Searches long-term memory |
| `SearchInSession` | `(ctx, userID, runID, query) ([]string, error)` | Searches session memory |
| `CreateSession` | `(ctx, userID) (string, error)` | Creates session |
| `DeleteSession` | `(ctx, userID, runID) error` | Deletes session memories |

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
