# Usage Tracking

Multi-layer usage tracking with graceful degradation.

## Architecture

```
                    ┌─────────────────┐
Agent Event ─────▶│ Usage Writer   │─────▶ PostgreSQL (primary)
                    └─────────────────┘       │
                                            │ (fallback)
                                            ▼
                                          Redis (secondary)
                                            │ (fallback)
                                            ▼
                                         Disk queue (tertiary)
```

## Layers

| Layer | Purpose | Behavior |
|--------|---------|----------|
| **PostgreSQL** | Primary storage | Writes async, persists all records |
| **Redis** | High-speed cache | Buffers before DB write |
| **Disk** | Last resort | JSONL files when DB unavailable |

## Event Structure

```go
type UsageEvent struct {
    Timestamp     time.Time
    UserID        string
    Model         string
    Provider      string
    TokensIn      int
    TokensOut     int
    Cost          float64
}
```

## Batching

Events are batched for efficiency:

1. **Memory buffer** - Accumulates events
2. **Redis flush** - Writes every 100 events or 10s
3. **DB persist** - Writes from Redis every 1000 events

## Service Pause

When primary storage fails:

1. Gateway enters "paused" state
2. Returns 503 with `Retry-After` header
3. Continues queueing to disk
4. Auto-resumes when storage recovers

## Implementation

Current: Memory mock with async simulation.
Planned: PostgreSQL + Redis + disk fallback.
