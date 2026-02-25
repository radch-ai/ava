# LLM Proxy Usage

Usage tracking with buffered batch writes and Redis fallback.

## Overview

```mermaid
flowchart TB
    Start(["Track(event)"]) --> HSelect

    subgraph SGSelect [" "]
        HSelect["**select w.eventChan <- event**"]
        HSelect -->|success| Done(["return nil"])
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
        HLPush --> Done
    end

    StartFlush(["flush()"]) --> HLock

    subgraph SGLock [" "]
        HLock["**w.bufferMu.Lock()**"]
        HLock --> HCheckBuffer
    end

    subgraph SGCheckBuffer [" "]
        HCheckBuffer["**len(w.buffer) == 0**"]
        HCheckBuffer -->|true| HUnlockEmpty
    end

    subgraph SGUnlockEmpty [" "]
        HUnlockEmpty["**w.bufferMu.Unlock()**"]
        HUnlockEmpty --> DoneFlush(["return nil"])
    end

    HCheckBuffer --> HCopyBuffer

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
        HUnlockFlush --> HDBInsert
    end

    HDBInsert --> HDBInsertCall

    subgraph SGDBInsertCall [" "]
        HDBInsertCall["**w.dbInsert(toFlush) → err**"]
        HDBInsertCall -->|error| HSetHealthyFalse
    end

    subgraph SGSetHealthyFalse [" "]
        HSetHealthyFalse["**w.dbHealthy.Store(false)**"]
        HSetHealthyFalse --> HRedisFallback
    end

    subgraph SGRedisFallback [" "]
        HRedisFallback["**for _, e := range toFlush: w.pushToRedis(e)**"]
        HRedisFallback --> DoneFlushError(["return err"])
    end

    HDBInsertCall -->|success| HSetHealthyTrue

    subgraph SGSetHealthyTrue [" "]
        HSetHealthyTrue["**w.dbHealthy.Store(true)**"]
        HSetHealthyTrue --> DoneFlushSuccess(["return nil"])
    end

    Select["select w.eventChan <- event"]
    TypeAssertRedis["client, ok := w.redis.(*redis.Client)"]
    MarshalJSON["json.Marshal(event) → data, err"]
    LPushRedis["client.LPush(ctx, key, data).Err() → err"]
    Prepare["tx.Prepare(sql) → stmt, err"]
    ExecStmt["stmt.Exec(...)"]
    Commit["tx.Commit()"]

    click Select href "https://pkg.go.dev/github.com/nevaria-ai/gateway/internal/llm-proxy/usage#UsageWriter.Track"
    click TypeAssertRedis href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client"
    click MarshalJSON href "https://pkg.go.dev/encoding/json#Marshal"
    click LPushRedis href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.LPush"
    click Prepare href "https://pkg.go.dev/database/sql#Tx.Prepare"
    click ExecStmt href "https://pkg.go.dev/database/sql#Stmt.Exec"
    click Commit href "https://pkg.go.dev/database/sql#Tx.Commit"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Done,StartFlush,DoneFlush,DoneFlushSuccess mainFn
    class Select,TypeAssertRedis,MarshalJSON,LPushRedis,Prepare,ExecStmt,Commit localPkgs
    class E1,E2,E3,DoneFlushError errorFlow
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

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewUsageWriter` | `(db, redis any) *UsageWriter` | Creates usage writer |
| `Track` | `(event UsageEvent) error` | Queues usage event |
| `pushToRedis` | `(event UsageEvent) error` | Pushes event to Redis |
| `flush` | `() error` | Flushes buffer to DB |
| `dbInsert` | `(events []UsageEvent) error` | Inserts events into DB |
| `Start` | `(ctx context.Context)` | Starts event processing |
| `TrackUsage` | `(ctx context.Context, event UsageEvent) error` | Convenience function |

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
