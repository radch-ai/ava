# internal/llm-proxy/usage

Usage tracking with Redis fallback.

## Description

The usage package tracks LLM token usage with a two-tier system: primary database insert with Redis queue fallback.

## Fallback Flow

```mermaid
flowchart TB
    Start["Track event"] --> Channel["Send to buffered event channel"]
    Channel --> HasSpace{Channel has space}
    HasSpace -->|Yes| Enqueued["Event queued"]
    HasSpace -->|No - Channel full| RedisLPUSH["Try Redis LPUSH"]
    RedisLPUSH --> Done["Done"]
```

## Buffered Write Flow

```mermaid
flowchart TB
    Goroutine["Start goroutine"] --> Receive["Receive event"]
    Goroutine --> Ticker["Ticker - 30s"]
    Goroutine --> Cancel["Context cancelled"]
    Receive --> AddBuffer["Add to buffer"]
    Ticker --> FlushBuffer["Flush buffer"]
    Cancel --> FinalFlush["Final flush & exit"]
    AddBuffer --> BatchCheck{"Buffer >= batch_size"}
    BatchCheck -->|Yes| ImmediateFlush["Flush immediately"]
    FlushBuffer --> DBInsert["dbInsert"]
    ImmediateFlush --> DBInsert
    FinalFlush --> DBInsert
    DBInsert --> DBSuccess{Success?}
    DBSuccess -->|YES| Done["Done"]
    DBSuccess -->|NO| RedisFallback["Redis fallback"]
```

## Types and Functions

| Name | Kind | Location | Description |
|------|------|----------|-------------|
| UsageWriter | struct | [writer.go#L25](../internal/llm-proxy/usage/writer.go#L25) | Buffered usage writer |
| NewUsageWriter | func | [writer.go#L37](../internal/llm-proxy/usage/writer.go#L37) | Creates usage writer |
| Track | func | [writer.go#L50](../internal/llm-proxy/usage/writer.go#L50) | Queues usage event |
| pushToRedis | func | [writer.go#L61](../internal/llm-proxy/usage/writer.go#L61) | LPUSH operation |
| flush | func | [writer.go#L90](../internal/llm-proxy/usage/writer.go#L90) | Writes buffer to DB |
| dbInsert | func | [writer.go#L119](../internal/llm-proxy/usage/writer.go#L119) | Batch insert |
| Start | func | [writer.go#L181](../internal/llm-proxy/usage/writer.go#L181) | Starts processing loop |
| UsageError | struct | [writer.go#L225](../internal/llm-proxy/usage/writer.go#L225) | Usage error type |
| FallbackError | struct | [writer.go#L242](../internal/llm-proxy/usage/writer.go#L242) | Fallback error type |

## UsageEvent Fields

| Field | Type | Description |
|-------|------|-------------|
| `UserID` | string | User identifier |
| `SessionID` | string | Session identifier |
| `Model` | string | Model used |
| `Provider` | string | LLM provider |
| `TokensInput` | int | Input tokens used |
| `TokensOutput` | int | Output tokens used |
| `CostTotal` | float64 | Total cost |
| `ChargedFrom` | string | "plan" or "addon" |
| `Timestamp` | int64 | Unix timestamp |

## Fallback Tiers

1. **Database** - PostgreSQL batch insert (primary)
2. **Redis** - LPUSH to `usage:{userID}` key (fallback)

## Configuration

| Constant | Value | Description |
|----------|-------|-------------|
| `defaultBatchSize` | 1000 | Events per batch |
| `defaultFlushInterval` | 30s | Time between flushes |
| `defaultMaxBufferSize` | 10000 | Max buffered events |

## Database Schema

```sql
INSERT INTO usage_events (
    user_id, session_id, model, provider,
    tokens_input, tokens_output, cost_total,
    charged_from, timestamp
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
```
