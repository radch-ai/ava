# LLM Proxy Usage

Usage tracking with buffered batch writes and Redis fallback.

### NewUsageWriter

```mermaid
flowchart TB
    Start(["NewUsageWriter(db, redis)"]) --> HCreate

    subgraph SGCreate [" "]
        HCreate["**Create UsageWriter with defaults**"]
        HCreate --> Return(["return &UsageWriter"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### Track

```mermaid
flowchart TB
    Start(["Track(event)"]) --> HSelect

    subgraph SGSelect [" "]
        HSelect["**select w.eventChan <- event**"]
        HSelect -->|success| Return(["return nil"])
        HSelect -->|full| HPushToRedis
    end

    HPushToRedis --> HTypeAssert

    subgraph SGTypeAssert [" "]
        HTypeAssert["**client, ok := w.redis.(*redis.Client)**"]
        HTypeAssert -->|!ok| E1["return ErrRedisNotConfigured"]
        HTypeAssert --> HMarshal
    end

    subgraph SGMarshal [" "]
        HMarshal["**json.Marshal(event) → data, err**"]
        HMarshal -->|error| E2["return &UsageError"]
        HMarshal --> HLPush
    end

    subgraph SGLPush [" "]
        HLPush["**client.LPush(ctx, key, data).Err() → err**"]
        HLPush -->|error| E3["return &UsageError"]
        HLPush --> Return
    end

    SelectCase["select w.eventChan <- event"]
    TypeAssertRedis["client, ok := w.redis.(*redis.Client)"]
    MarshalJSON["json.Marshal(event) → data, err"]
    LPushRedis["client.LPush(ctx, key, data).Err() → err"]

    click SelectCase href "https://pkg.go.dev/github.com/nevaria-ai/gateway/internal/llm-proxy/usage#UsageWriter.Track"
    click TypeAssertRedis href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client"
    click MarshalJSON href "https://pkg.go.dev/encoding/json#Marshal"
    click LPushRedis href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.LPush"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class SelectCase,TypeAssertRedis,MarshalJSON,LPushRedis localPkgs
    class E1,E2,E3 errorFlow
```

### pushToRedis

```mermaid
flowchart TB
    Start(["pushToRedis(event)"]) --> HCheckNil

    subgraph SGCheckNil [" "]
        HCheckNil["**w.redis == nil**"]
        HCheckNil -->|true| E1["return ErrRedisNotConfigured"]
        HCheckNil --> HTypeAssert
    end

    subgraph SGTypeAssert [" "]
        HTypeAssert["**client, ok := w.redis.(*redis.Client)**"]
        HTypeAssert -->|!ok| E2["return ErrRedisNotConfigured"]
        HTypeAssert --> HMarshal
    end

    subgraph SGMarshal [" "]
        HMarshal["**json.Marshal(event) → data, err**"]
        HMarshal -->|error| E3["return &UsageError"]
        HMarshal --> HLPush
    end

    subgraph SGLPush [" "]
        HLPush["**client.LPush(ctx, key, data).Err() → err**"]
        HLPush -->|error| E4["return &UsageError"]
        HLPush --> Return(["return nil"])
    end

    TypeAssertRedis["client, ok := w.redis.(*redis.Client)"]
    MarshalJSON["json.Marshal(event) → data, err"]
    LPushRedis["client.LPush(ctx, key, data).Err() → err"]

    click TypeAssertRedis href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client"
    click MarshalJSON href "https://pkg.go.dev/encoding/json#Marshal"
    click LPushRedis href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.LPush"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class TypeAssertRedis,MarshalJSON,LPushRedis localPkgs
    class E1,E2,E3,E4 errorFlow
```

### flush

```mermaid
flowchart TB
    Start(["flush()"]) --> HLock

    subgraph SGLock [" "]
        HLock["**w.bufferMu.Lock()**"]
        HLock --> HCheckBuffer
    end

    subgraph SGCheckBuffer [" "]
        HCheckBuffer["**len(w.buffer) == 0**"]
        HCheckBuffer -->|true| HUnlockEmpty
        HCheckBuffer -->|false| HCopyBuffer
    end

    subgraph SGUnlockEmpty [" "]
        HUnlockEmpty["**w.bufferMu.Unlock()**"]
        HUnlockEmpty --> Return(["return nil"])
    end

    subgraph SGCopyBuffer [" "]
        HCopyBuffer["**toFlush := make([]UsageEvent, len(w.buffer))**"]
        HCopyBuffer --> HClearBuffer
    end

    subgraph SGClearBuffer [" "]
        HClearBuffer["**w.buffer = w.buffer[:0]**"]
        HClearBuffer --> HUnlockFlush
    end

    subgraph SGUnlockFlush [" "]
        HUnlockFlush["**w.bufferMu.Unlock()**"]
        HUnlockFlush --> HDBInsertCall
    end

    HDBInsertCall --> HDBInsert

    subgraph SGDBInsert [" "]
        HDBInsert["**w.dbInsert(toFlush) → err**"]
        HDBInsert -->|error| HSetHealthyFalse
        HDBInsert -->|success| HSetHealthyTrue
    end

    subgraph SGSetHealthyFalse [" "]
        HSetHealthyFalse["**w.dbHealthy.Store(false)**"]
        HSetHealthyFalse --> HRedisFallback
    end

    subgraph SGRedisFallback [" "]
        HRedisFallback["**for _, e := range toFlush: w.pushToRedis(e)**"]
        HRedisFallback --> ReturnError(["return err"])
    end

    subgraph SGSetHealthyTrue [" "]
        HSetHealthyTrue["**w.dbHealthy.Store(true)**"]
        HSetHealthyTrue --> Return(["return nil"])
    end

    Lock["w.bufferMu.Lock()"]
    Unlock["w.bufferMu.Unlock()"]
    StoreFalse["w.dbHealthy.Store(false)"]
    StoreTrue["w.dbHealthy.Store(true)"]

    click Lock href "https://pkg.go.dev/sync#Mutex.Lock"
    click Unlock href "https://pkg.go.dev/sync#Mutex.Unlock"
    click StoreFalse href "https://pkg.go.dev/sync/atomic#Bool.Store"
    click StoreTrue href "https://pkg.go.dev/sync/atomic#Bool.Store"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return,ReturnError mainFn
    class Lock,Unlock,StoreFalse,StoreTrue localPkgs
```

### dbInsert

```mermaid
flowchart TB
    Start(["dbInsert(events)"]) --> HTypeAssert

    subgraph SGTypeAssert [" "]
        HTypeAssert["**db, ok := w.db.(*sql.DB)**"]
        HTypeAssert -->|!ok| E1["return error"]
        HTypeAssert --> HBegin
    end

    subgraph SGBegin [" "]
        HBegin["**tx, err := db.Begin()**"]
        HBegin -->|error| E2["return err"]
        HBegin --> HPrepare
    end

    subgraph SGPrepare [" "]
        HPrepare["**stmt, err := tx.Prepare(sql) → stmt, err**"]
        HPrepare -->|error| E3["return err"]
        HPrepare --> HIterate
    end

    subgraph SGIterate [" "]
        HIterate["**for _, event := range events**"]
        HIterate --> HExec
    end

    subgraph SGExec [" "]
        HExec["**stmt.Exec(event fields...) → _, err**"]
        HExec -->|error| HIterate
        HExec -->|success| HIterate
        HIterate --> HCommit
    end

    subgraph SGCommit [" "]
        HCommit["**tx.Commit() → err**"]
        HCommit -->|error| E4["return err"]
        HCommit --> Return(["return nil"])
    end

    TypeAssertDB["db, ok := w.db.(*sql.DB)"]
    BeginTx["tx, err := db.Begin()"]
    PrepareStmt["tx.Prepare(sql) → stmt, err"]
    ExecStmt["stmt.Exec(event fields...)"]
    CommitTx["tx.Commit()"]

    click TypeAssertDB href "https://pkg.go.dev/database/sql"
    click BeginTx href "https://pkg.go.dev/database/sql#DB.Begin"
    click PrepareStmt href "https://pkg.go.dev/database/sql#Tx.Prepare"
    click ExecStmt href "https://pkg.go.dev/database/sql#Stmt.Exec"
    click CommitTx href "https://pkg.go.dev/database/sql#Tx.Commit"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class TypeAssertDB,BeginTx,PrepareStmt,ExecStmt,CommitTx localPkgs
    class E1,E2,E3,E4 errorFlow
```

### Start

```mermaid
flowchart TB
    Start(["Start(ctx)"]) --> HLoop

    subgraph SGLoop [" "]
        HLoop["**for**"]
        HLoop --> RecvChan
    end

    RecvChan["**event := <-w.eventChan**"] --> HAppend

    subgraph SGAppend [" "]
        HAppend["**w.buffer = append(w.buffer, event)**"]
        HAppend --> HCheckLen
    end

    subgraph SGCheckLen [" "]
        HCheckLen["**len(w.buffer) >= w.batchSize**"]
        HCheckLen -->|true| HFlushBatch
        HCheckLen -->|false| RecvTicker
    end

    subgraph SGFlushBatch [" "]
        HFlushBatch["**w.flush()**"]
        HFlushBatch --> RecvTicker
    end

    RecvTicker["**<-w.ticker.C**"] --> HFlushTicker

    subgraph SGFlushTicker [" "]
        HFlushTicker["**w.flush()**"]
        HFlushTicker --> RecvDone
    end

    RecvDone["**<-ctx.Done()**"] --> HFinalFlush

    subgraph SGFinalFlush [" "]
        HFinalFlush["**w.flush()**"]
        HFinalFlush --> HStop
    end

    subgraph SGStop [" "]
        HStop["**w.ticker.Stop()**"]
        HStop --> Return(["return"])
    end

    StopTicker["w.ticker.Stop()"]

    click StopTicker href "https://pkg.go.dev/time#Ticker.Stop"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Return mainFn
    class StopTicker localPkgs
```

### TrackUsage

```mermaid
flowchart TB
    Start(["TrackUsage(ctx, event)"]) --> HLog

    subgraph SGLog [" "]
        HLog["**Log usage event**"]
        HLog --> Return(["return nil"])
    end

    LogInfo["log.Info().Str(...).Msg('Usage tracked')"]

    click LogInfo href "https://github.com/rs/zerolog"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Return mainFn
    class LogInfo localPkgs
```

## Structs

### UsageWriter

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `UsageWriter` | `eventChan` | `chan UsageEvent` | Event channel |
| | `buffer` | `[]UsageEvent` | Event buffer |
| | `bufferMu` | `sync.Mutex` | Buffer lock |
| | `ticker` | `*time.Ticker` | Flush ticker |
| | `db` | `any` | Database connection |
| | `redis` | `any` | Redis client |
| | `dbHealthy` | `atomic.Bool` | DB health flag |
| | `batchSize` | `int` | Batch size |

### UsageError

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `UsageError` | `Op` | `string` | Operation name |
| | `Err` | `error` | Underlying error |

### FallbackError

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `FallbackError` | `Message` | `string` | Error message |

## Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `defaultBatchSize` | `int` | `1000` | Default batch size |
| `defaultFlushInterval` | `time.Duration` | `30s` | Default flush interval |
| `defaultMaxBufferSize` | `int` | `10000` | Default max buffer size |

## Database Schema

### usage_events table

| Column | Type | Description |
|--------|------|-------------|
| `user_id` | `TEXT` | User identifier |
| `session_id` | `TEXT` | Session identifier |
| `model` | `TEXT` | Model name |
| `provider` | `TEXT` | Provider name |
| `tokens_input` | `INTEGER` | Input tokens |
| `tokens_output` | `INTEGER` | Output tokens |
| `cost_total` | `NUMERIC` | Total cost |
| `charged_from` | `TEXT` | "plan" or "addon" |
| `timestamp` | `BIGINT` | Unix timestamp |

## Redis Keys

| Key Pattern | Description |
|-------------|-------------|
| `usage:{userID}` | List of usage events (JSON) |

## Processing Flow

1. **Track**: Event sent to channel or directly to Redis if channel full
2. **Background goroutine**:
   - Receives events from channel
   - Appends to buffer
   - Flushes when buffer reaches batch size or on ticker interval
3. **Flush**:
   - Attempts DB insert
   - On DB failure, falls back to Redis
   - Updates dbHealthy flag accordingly

## Batch Write Behavior

| Trigger | Action |
|---------|--------|
| Buffer reaches batch size | Immediate flush |
| Ticker interval | Periodic flush |
| Context cancelled | Final flush before exit |

## Fallback Behavior

| Condition | Behavior |
|-----------|----------|
| Channel not full | Send to event channel |
| Channel full | Push directly to Redis |
| DB insert successful | Mark DB healthy |
| DB insert failed | Mark DB unhealthy, flush to Redis |
| Redis not configured | Return error |

## Errors

| Error | Description |
|-------|-------------|
| `ErrRedisNotConfigured` | Redis client not configured |
| `ErrRedisUnavailable` | Redis unavailable |

## Related

- [LLM Proxy README](README.md) - Main proxy documentation
