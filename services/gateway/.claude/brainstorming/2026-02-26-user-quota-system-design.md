# User Quota System Architecture - Brainstorming

**Date:** 2026-02-26
**Topic:** Chunk-based quota reservation for multi-tenant HA agentic platform
**Status:** Brainstorming (pre-implementation)

---

## Original Specification (Preserved)

> This is a multi-tenant HA (high availability) agentic platform built in Go. Multiple instances run behind a load balancer. Users interact via WebSocket connections. Multiple AI agents can run concurrently for the same user within a session. Each user has a quota limit (variable per user) that resets every 4 hours.
>
> **Core Design Goal:** Minimize Redis calls per LLM turn while maintaining correct quota accounting across HA instances. In-memory operation is the default, Redis is only touched at chunk boundaries.

---

## Initial Context

### Current Architecture State
- **LLM calls:** Agents → `sdk_client.go` → `llmproxy.Handler` → Provider API
- **Quota handling:** Every LLM turn hits Redis (Reserve → Deduct pattern in SDK)
- **Multi-agent:** Supported via `(userID, agentID)` composite key
- **Redis client:** `go-redis/v9` with simple client (no cluster/sentinel needed - managed service)
- **Existing patterns:** Pipeline for batched ops, no Lua scripts yet

### Problem Being Solved
Current architecture: 1 Redis call per LLM turn
Target architecture: 1 Redis call per ~20 turns (95% reduction)

### Constraints
- **No cluster/sentinel concern:** Using managed Redis with HA already set up
- **Eino integration pending:** LLM layer will change, quota layer should be independent
- **Implementation timing:** Design now, implement after eino/LLM integration

---

## Questions Explored

### 1. Where does the quota system sit in the architecture?

**Options Considered:**

| Option | Position | Pros | Cons |
|--------|----------|------|------|
| A. SDK level | Inside llmproxy.Handler | Already has quota logic | Tied to SDK, doesn't work with eino |
| B. Agent level | Inside agent.Prompt() | Simple interception | Agent shouldn't know about quota |
| C. Middleware layer | Between agent and LLM | Clean separation, works with any LLM backend | New component to build |

**Decision: Option C - Middleware layer**

```
Agent.Prompt()
    ↓
┌─────────────────────────────────────┐
│      QUOTA MIDDLEWARE (NEW)         │
│  - Check/reserve chunk              │
│  - Track usage                      │
│  - Emit warnings                    │
└─────────────────────────────────────┘
    ↓
LLM Provider (eino or SDK, swappable)
```

**Rationale:**
- Agents remain quota-agnostic (single responsibility)
- Works with current SDK or future eino integration
- Single interception point for all LLM calls
- Can reject before any LLM cost is incurred
- Clean testing boundary

---

### 2. What is the unit of quota?

**Options Considered:**

| Unit | Storage | Operations | User-facing |
|------|---------|------------|-------------|
| Tokens | Integer | Simple | Confusing (model-dependent) |
| Dollars | Float | Complex (no atomic float ops) | Clear |
| Cents × 1000 | Integer | Atomic IncrBy/DecrBy | Clear enough |

**Decision: Cents × 1000 (integer)**

```go
// Storage: $1.50 stored as 1500
formatCents(dollars float64) int64 = int64(dollars * 1000)
centsToDollars(cents int64) float64 = float64(cents) / 1000
```

**Rationale:**
- Consistent with existing llm-proxy-sdk pattern
- Enables atomic Redis `IncrBy`/`DecrBy` operations
- No Lua scripts needed for simple arithmetic
- Different models have different costs per token - dollars abstracts this
- User sees "$X remaining" which is clearer than "Y tokens"

---

### 3. How do we size chunks dynamically?

**Problem:** Fixed chunk sizes either waste quota (too big) or cause frequent refills (too small).

**Solution: Dynamic chunk sizing based on actual usage**

**Initial Chunk (first prompt):**
```
1. Estimate cost of first prompt (eino provides, or character-based fallback)
2. Chunk size = estimate × 20
3. Request from Redis
```

**Subsequent Chunks:**
```
1. Use last turn's actual token usage (exact, from LLM response)
2. Chunk size = (last_actual_cost × 20) + overage_debt
```

**Why 20x multiplier:**
- Covers context growth (later turns have more history, cost more tokens)
- 20 turns per chunk ≈ reasonable tradeoff
- Small enough that crash loss is acceptable
- Large enough to meaningfully reduce Redis calls

**Context growth example:**
```
Turn 1:  500 input tokens  → Chunk = $0.50
Turn 10: 1500 input tokens → Chunk = $1.50 (auto-adjusted at refill)
Turn 20: 3000 input tokens → Chunk = $3.00 (continues to adapt)
```

---

### 4. Lua vs Plain Redis Operations - When to Use Which?

**Key Insight:** The chunk grant is the ONLY operation requiring Lua. Everything else uses plain atomic Redis ops.

**Analysis:**

| Operation | Steps | Concurrency Risk | Solution |
|-----------|-------|------------------|----------|
| **Chunk Grant** | Read → Calculate → Deduct → Return | HIGH: Double-allocation | **Lua script** |
| Return unused | IncrBy | None (single op) | Plain Redis |
| Post overage | DecrBy | None (single op) | Plain Redis |
| Read remaining | Get | None (read-only) | Plain Redis |

**Why chunk grant needs Lua:**

```
Without Lua (TOCTOU race condition):
  Time    Agent A                    Agent B                    Redis
  ────    ────────                   ────────                   ─────
  T1      GET remaining → $5.00                                 $5.00
  T2                                 GET remaining → $5.00      $5.00
  T3      DECRBY $2.00                                           $3.00
  T4                                 DECRBY $2.00               $1.00
  Result: Both granted $2.00, but only $5.00 existed!
          Double-allocated $4.00 from $5.00 budget
```

```
With Lua (atomic):
  Time    Agent A                    Agent B                    Redis
  ────    ────────                   ────────                   ─────
  T1      EVAL chunk_grant($2)                                   $3.00
          → granted: $2, remaining: $3
  T2                                 EVAL chunk_grant($2)       $1.00
                                     → granted: $2, remaining: $1
  Result: Both correctly serialized, no double-allocation
```

**Lua Script for Chunk Grant:**

```lua
-- KEYS[1] = quota:{userID}:remaining
-- ARGV[1] = requested_chunk_amount
-- Returns: {granted_amount, global_remaining}

local remaining = tonumber(redis.call('GET', KEYS[1]) or '0')
local requested = tonumber(ARGV[1])

if remaining <= 0 then
    -- Quota exhausted, grant nothing
    return {0, remaining}
end

if remaining >= requested then
    -- Full grant available
    redis.call('DECRBY', KEYS[1], requested)
    return {requested, remaining - requested}
else
    -- Partial grant - give everything remaining
    local granted = remaining
    redis.call('SET', KEYS[1], 0)
    return {granted, 0}
end
```

**Go Integration with go-redis:**

```go
var chunkGrantScript = redis.NewScript(`
    local remaining = tonumber(redis.call('GET', KEYS[1]) or '0')
    local requested = tonumber(ARGV[1])

    if remaining <= 0 then
        return {0, remaining}
    end

    if remaining >= requested then
        redis.call('DECRBY', KEYS[1], requested)
        return {requested, remaining - requested}
    else
        local granted = remaining
        redis.call('SET', KEYS[1], 0)
        return {granted, 0}
    end
`)

func (q *QuotaManager) RequestChunk(ctx context.Context, userID string, requested int64) (*ChunkGrant, error) {
    key := fmt.Sprintf("quota:%s:remaining", userID)

    // Uses EVALSHA on subsequent calls (script registered once at startup)
    result, err := chunkGrantScript.Run(ctx, q.client, []string{key}, requested).Result()
    if err != nil {
        return nil, err
    }

    // Unpack the two return values
    values := result.([]interface{})
    return &ChunkGrant{
        GrantedAmount:   values[0].(int64),
        GlobalRemaining: values[1].(int64),
        Exhausted:       values[1].(int64) <= 0,
    }, nil
}
```

**Why `redis.NewScript()` is clean:**
- Script text sent once, SHA computed and cached
- Subsequent calls use `EVALSHA` (just the hash, not full script)
- No performance penalty after first call
- Fits naturally as a method on QuotaManager

---

### 5. What does Redis return on chunk grant?

**Decision:** Return two values atomically via Lua script

```go
type ChunkGrant struct {
    GrantedAmount   int64  // What was actually granted (may be partial)
    GlobalRemaining int64  // Remaining in Redis after this grant
    Exhausted       bool   // True if global_remaining <= 0
}
```

**Grant scenarios:**

| Scenario | Requested | Available | Granted | Remaining | Action |
|----------|-----------|-----------|---------|-----------|--------|
| Normal | $2.00 | $5.00 | $2.00 | $3.00 | Continue |
| Partial | $2.00 | $1.50 | $1.50 | $0.00 | Continue on partial |
| Exhausted | $2.00 | $0.00 | $0.00 | $0.00 | **Reject** |
| Negative (debt) | $2.00 | -$0.50 | $0.00 | -$0.50 | **Reject** |

**Key principle:** Never reject just because chunk is smaller than requested. Let session continue on whatever is left.

---

### 6. What happens when chunk is exhausted mid-turn?

**Scenario:** Chunk has $0.05 remaining, turn costs $0.10

**Decision:** Let the turn complete, track negative balance

```
Before LLM call:
  Chunk balance: $0.05 (still positive, proceed)

After LLM call:
  Actual cost: $0.10
  Chunk balance: $0.05 - $0.10 = -$0.05 (negative)
  Overage debt: $0.05 (stored for next refill)

Next chunk request:
  Requested = (last_actual × 20) + overage_debt
  Requested = ($0.10 × 20) + $0.05 = $2.05
```

**Rationale:**
- Prevents abrupt mid-stream failures (bad UX)
- User gets complete response
- Honest accounting via negative tracking
- Debt automatically paid at next chunk request

---

### 7. What about negative quota posting to Redis?

**Scenario:** Session's final turn costs more than all remaining quota

**Decision:** Post overage to Redis. Redis goes negative.

```
Before: quota:{userID}:remaining = $0.10
Final turn costs: $0.15
After: quota:{userID}:remaining = -$0.05
```

**Impact:**
- User starts next 4-hour period with $0.05 debt
- Period reset adds limit → debt is paid automatically
- Honest accounting - no tokens silently absorbed

**Why accept this:**
- Simpler than trying to prevent it
- Debt is bounded (one turn's worth, typically <$0.50)
- Rare edge case (user exhausting quota on final turn)
- Automatic resolution at period reset

---

### 8. What about crash loss?

**Scenario:** Instance crashes with in-memory chunk of $1.50

**Decision:** Accept the loss. No TTL, heartbeat, or reconciler.

**Loss calculation:**
```
Typical chunk size: ~$1-2 (20 turns worth)
User with $10/4hr period
Maximum loss per crash: ~10-20% of one user's period quota
Crash frequency: Rare (HA setup, managed infrastructure)
```

**Rejected alternatives:**

| Alternative | Complexity | Value |
|-------------|------------|-------|
| TTL on chunk | High (need reconciler) | Low (recovers ~$1) |
| Heartbeat | High (network chatter) | Low |
| Reserve in Redis | High (locks quota) | Medium |
| Accept loss | **Low** | N/A |

**Rationale:**
- Chunk sizes are small (% of period limit)
- Crashes are rare events
- Complexity of recovery mechanism > value of recovered quota
- User impact is minimal (one chunk worth)

---

### 9. When do we return unused chunk to Redis?

**Decision:** Return unused remainder on session end or idle timeout (5 minutes)

**Session end triggers:**
1. LLM signals completion (agent task finished)
2. WebSocket disconnect (user closed browser/tab)
3. Idle timeout (5 minutes of no activity)

**Implementation:**

```go
func (s *QuotaSession) ReturnChunk(ctx context.Context, client *redis.Client) error {
    if s.ChunkBalance > 0 {
        key := fmt.Sprintf("quota:%s:remaining", s.UserID)
        err := client.IncrBy(ctx, key, s.ChunkBalance).Err()
        if err != nil {
            return fmt.Errorf("failed to return chunk: %w", err)
        }
        s.ChunkBalance = 0
    }
    return nil
}
```

**Why this matters:**
- Minimizes crash loss (only active sessions at crash time lose quota)
- Normal session endings = full budget recovery
- 5-minute timeout catches abandoned sessions quickly

---

### 10. How do we handle multi-agent concurrency?

**Scenario:** User has 3 agents running simultaneously

**Decision:** Each agent holds its own independent chunk. Redis serializes grants atomically via Lua.

```
Timeline:
─────────────────────────────────────────────────────────────────
Redis remaining: $5.00

Agent A: RequestChunk($1.00)
  → Lua executes atomically
  → Granted: $1.00, Remaining: $4.00
  → Agent A local balance: $1.00

Agent B: RequestChunk($1.00)
  → Lua executes atomically
  → Granted: $1.00, Remaining: $3.00
  → Agent B local balance: $1.00

Agent C: RequestChunk($1.00)
  → Lua executes atomically
  → Granted: $1.00, Remaining: $2.00
  → Agent C local balance: $1.00

... 15 turns later, agents need refills ...

Agent A: RequestChunk($1.00)
  → Granted: $1.00, Remaining: $1.00

Agent B: RequestChunk($1.00)
  → Granted: $1.00, Remaining: $0.00

Agent C: RequestChunk($1.00)
  → Granted: $0, Remaining: $0.00
  → EXHAUSTED → Agent C rejected
─────────────────────────────────────────────────────────────────
```

**Why this works:**
- No coordination needed between agents
- Lua script serializes all grants atomically
- Agents naturally reject themselves when grant returns 0
- No broadcast or invalidation mechanism needed

---

### 11. How do we warn users about low quota?

**Decision:** WebSocket event at chunk grant time

**Trigger:** Global remaining ≤ 7% of user's period limit

**Warning event:**
```json
{
  "type": "quota_warning",
  "data": {
    "remaining_cents": 70,
    "limit_cents": 1000,
    "percent_remaining": 7,
    "message": "You have approximately 7% of your quota remaining"
  }
}
```

**Exhaustion event:**
```json
{
  "type": "quota_exhausted",
  "data": {
    "message": "Your 4-hour quota is exhausted. It will reset at 14:00 UTC.",
    "resets_at": 1738190400
  }
}
```

**When to check:**
- Every chunk grant returns `global_remaining`
- Compare against user's limit (fetched at session start)
- Only send to human-facing sessions (not agent-to-agent calls)
- Warning only sent once per session (track `warning_sent` flag)

---

### 12. What about token estimation for first chunk?

**Challenge:** First prompt has no "last actual cost" to base chunk size on

**Options:**

| Approach | Accuracy | Complexity |
|----------|----------|------------|
| Eino built-in | High | Depends on eino availability |
| Character-based (len/4) | Low | Simple |
| Model lookup table | Medium | Requires maintenance |

**Decision:** Eino for first prompt, actual usage for subsequent

```go
func (q *QuotaManager) estimateFirstChunk(prompt string, model string) int64 {
    // Eino provides token estimation
    estimatedTokens := eino.EstimateTokens(prompt, model)

    // Calculate cost from model config
    costPerMillionInput := getModelCost(model).InputCostPerMillion
    estimatedCost := float64(estimatedTokens) * costPerMillionInput / 1_000_000

    // 20x multiplier for chunk
    return formatCents(estimatedCost * 20)
}

func (q *QuotaManager) calculateNextChunk(lastActualCost int64, overageDebt int64) int64 {
    return (lastActualCost * 20) + overageDebt
}
```

**Fallback if eino not available:**
```go
// Rough estimate: 1 token ≈ 4 characters for English
func fallbackEstimate(prompt string) int {
    return len(prompt) / 4
}
```

---

### 13. What is the complete session lifecycle?

**Session Start:**
```
1. WebSocket connects for (userID, agentID)
2. Create QuotaSession struct
3. Fetch user's period limit from Redis: GET quota:{userID}:period_limit
4. Fetch current remaining: GET quota:{userID}:remaining
5. Estimate first chunk size
6. RequestChunk() via Lua script
7. If granted == 0 → reject immediately, send quota_exhausted event
8. Store granted amount as ChunkBalance
```

**Per-Turn Flow:**
```
1. User sends message
2. BeforeLLM():
   a. Check if ChunkBalance > 0 OR (ChunkBalance < 0 AND overage not yet handled)
   b. If balance <= 0 AND no pending overage:
      - Calculate next chunk = (LastActualCost × 20) + OverageDebt
      - RequestChunk() via Lua
      - If granted == 0 → reject, send quota_exhausted event
      - Store new ChunkBalance
   c. Proceed with LLM call
3. LLM call executes (streaming response)
4. AfterLLM():
   a. Extract actual token usage from response
   b. Calculate actual cost (model × tokens)
   c. Deduct from ChunkBalance (may go negative)
   d. If now negative, set OverageDebt = abs(balance)
   e. Store LastActualCost for next chunk calculation
5. Return response to agent
```

**Session End:**
```
1. Trigger: LLM done OR WebSocket close OR 5-minute idle timeout
2. If ChunkBalance > 0:
   - ReturnChunk(): INCRBY quota:{userID}:remaining ChunkBalance
3. Clear session from memory
```

---

### 14. What data structures do we need?

**Redis Keys:**
```
quota:{userID}:remaining       # Integer cents × 1000 (can be negative)
quota:{userID}:period_start    # Unix timestamp of period start
quota:{userID}:period_limit    # User's limit for this period (cents × 1000)
```

**In-Memory Session:**
```go
type QuotaSession struct {
    UserID          string
    AgentID         string
    PeriodLimit     int64       // User's period limit (cents)
    ChunkBalance    int64       // Current chunk (can be negative)
    OverageDebt     int64       // Debt to add to next chunk request
    LastActualCost  int64       // Last turn's actual cost (for next chunk)
    PeriodStart     time.Time   // When current period began
    LastActivity    time.Time   // For idle timeout detection
    WarningSent     bool        // Track if 7% warning was sent
}
```

**Chunk Grant Result:**
```go
type ChunkGrant struct {
    GrantedAmount   int64  // What was actually granted
    GlobalRemaining int64  // Remaining in Redis after grant
    Exhausted       bool   // True if quota fully exhausted
}
```

**Quota Middleware:**
```go
type QuotaMiddleware struct {
    client       *redis.Client
    sessions     map[SessionKey]*QuotaSession  // Key: (userID, agentID)
    mu           sync.RWMutex
    modelConfigs map[string]ModelConfig        // Loaded at startup
    grantScript  *redis.Script                 // Lua script for chunk grant
}
```

---

### 15. How does period reset work?

**Note:** Period reset implementation is a separate concern (handled by external service/job)

**What quota system needs:**
- Read `period_start` to know current period
- Read `period_limit` to calculate warning threshold (7%)
- Read/write `remaining` for all quota operations

**Period boundary detection:**
```go
func (q *QuotaMiddleware) checkPeriodBoundary(session *QuotaSession) bool {
    now := time.Now()
    periodEnd := session.PeriodStart.Add(4 * time.Hour)

    if now.After(periodEnd) {
        // New period has started, refresh from Redis
        newLimit, _ := q.client.Get(ctx, quotaKey(session.UserID, "period_limit")).Int64()
        newRemaining, _ := q.client.Get(ctx, quotaKey(session.UserID, "remaining")).Int64()
        newStart, _ := q.client.Get(ctx, quotaKey(session.UserID, "period_start")).Int64()

        session.PeriodLimit = newLimit
        session.ChunkBalance = 0  // Old chunk is stale
        session.PeriodStart = time.Unix(newStart, 0)

        return true  // Period changed
    }
    return false
}
```

---

### 16. How does cost calculation work?

**Reuse from llm-proxy-sdk:**

```go
// Model config loaded from config/models.yaml
type ModelConfig struct {
    Name                 string
    Provider             string
    InputCostPerMillion  float64
    OutputCostPerMillion float64
    MaxTokens            int
}

func CalculateCost(cfg *ModelConfig, inputTokens, outputTokens int) float64 {
    inputCost := float64(inputTokens) * cfg.InputCostPerMillion / 1_000_000
    outputCost := float64(outputTokens) * cfg.OutputCostPerMillion / 1_000_000
    return inputCost + outputCost
}
```

**Integration point:**
- Model configs already exist in `config/models.yaml`
- Load at middleware startup
- Pass model name with each LLM call
- Calculate cost from tokens + model config after response

---

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Gateway Instance                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────┐     ┌───────────────────────────────────────────────────┐ │
│  │  WebSocket   │     │              QuotaMiddleware                       │ │
│  │  Handler     │────▶│  ┌─────────────────────────────────────────────┐  │ │
│  └──────────────┘     │  │  Sessions Map: (userID, agentID) → Session  │  │ │
│                       │  │                                             │  │ │
│  ┌──────────────┐     │  │  Per-Session State:                         │  │ │
│  │  Agent       │     │  │  - ChunkBalance (can go negative)           │  │ │
│  │  Manager     │────▶│  │  - OverageDebt (for next refill)            │  │ │
│  └──────────────┘     │  │  - LastActualCost (for chunk sizing)        │  │ │
│                       │  │  - PeriodLimit (for 7% warning)             │  │ │
│  ┌──────────────┐     │  │  - WarningSent (dedup warning events)       │  │ │
│  │  Multiple    │     │  └─────────────────────────────────────────────┘  │ │
│  │  Agents      │────▶│                                                    │ │
│  │  (per user)  │     │  Methods:                                          │ │
│  └──────────────┘     │  - BeforeLLM(userID, AgentID, Model, Prompt)      │ │
│                       │    → Check chunk, refill via Lua if needed         │ │
│                       │    → Return: proceed or reject                     │ │
│                       │                                                     │ │
│                       │  - AfterLLM(userID, AgentID, Tokens)               │ │
│                       │    → Calculate cost, deduct from chunk             │ │
│                       │    → Track overage if negative                     │ │
│                       │                                                     │ │
│                       │  - ReturnChunk(userID, AgentID)                    │ │
│                       │    → INCRBY unused balance to Redis                │ │
│                       │    → Called on session end/timeout                 │ │
│                       │                                                     │ │
│                       │  - CheckIdle() [background goroutine]              │ │
│                       │    → 5-minute timeout → ReturnChunk()              │ │
│                       └───────────────────────────────────────────────────┘ │
│                                              │                               │
│                                              │ Lua (chunk grant)             │
│                                              │ Plain ops (return, read)      │
│                                              ▼                               │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │                    LLM Provider (eino or SDK)                        │  │
│  │                    - Swappable, quota-agnostic                       │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
                                       │
                                       │ Redis Protocol
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                     Redis (Master Ledger) - Managed HA Service              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  quota:{userID}:remaining     # Integer cents × 1000 (CAN BE NEGATIVE)      │
│  quota:{userID}:period_start  # Unix timestamp                              │
│  quota:{userID}:period_limit  # User's 4-hour limit                         │
│                                                                              │
│  Operations:                                                                 │
│  ─────────────────────────────────────────────────────────────────────────  │
│  Chunk Grant:    EVALSHA chunk_grant.lua (Lua - atomic)                     │
│  Return Unused:  INCRBY quota:{userID}:remaining {amount}                   │
│  Read Limit:     GET quota:{userID}:period_limit                            │
│  Read Period:    GET quota:{userID}:period_start                            │
│                                                                              │
│  Note: No cluster/sentinel config needed - managed service handles HA       │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Redis Operations Summary

| Event | Operation | Type | Frequency |
|-------|-----------|------|-----------|
| Session start | `GET period_limit`, `GET remaining` | Plain | Once |
| Chunk grant | `EVALSHA chunk_grant.lua` | **Lua** | Once per ~20 turns |
| Return unused | `INCRBY remaining {balance}` | Plain | Once per session |
| Period check | `GET period_start` | Plain | Once per session |

**Before this design:** 1 Redis call per turn
**After this design:** 1 Redis call per ~20 turns
**Reduction:** ~95%

---

## Edge Cases & Scenarios

### Scenario 1: User rapidly creates 10 agents
```
User has $10 limit, creates 10 agents simultaneously
Each requests $1 chunk (10% of limit)
After 10 grants: Redis remaining = $0
11th agent (if any): Grant = 0, rejected
All 10 agents operate independently, self-reject when chunks exhausted
```

### Scenario 2: One agent hogs all quota
```
Agent A has $10 chunk, makes expensive calls
Agent B requests $2 chunk → gets it (if remaining)
Agent A exhausts chunk, requests refill → may get 0 if B took remaining
Agent A rejected, even though it was the "hog"
Solution: This is correct behavior - first-come-first-served at chunk level
```

### Scenario 3: Instance crash mid-session
```
Session has $1.50 chunk balance
Instance crashes
$1.50 is lost (not returned to Redis)
User's next session: Remaining = $8.50 (assuming $10 limit)
Accepted loss - no recovery mechanism
```

### Scenario 4: Period reset during active session
```
Session started at T=0, period ends at T=4hr
At T=4hr01min, session still has $2 chunk
Session makes next LLM call:
  - checkPeriodBoundary() detects new period
  - Refreshes from Redis: new limit, new remaining, new period_start
  - Old $2 chunk is stale, discarded
  - Request new chunk from fresh remaining
```

### Scenario 5: WebSocket reconnect
```
User closes browser, reopens 2 minutes later
Old session: idle timeout not yet hit (5 min), still in memory
New WebSocket: same (userID, agentID)
Agent manager: Returns existing agent/session
Session continues with same chunk
If 5 min passed: session cleaned up, new session with fresh chunk request
```

### Scenario 6: Negative balance across period boundary
```
End of period: remaining = -$0.50 (debt from last turn)
Period reset: remaining set to period_limit ($10)
Effective start: $10 - $0.50 = $9.50 (debt paid automatically)
```

---

## Key Decisions Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Architecture position** | Middleware between agent and LLM | Clean separation, swappable backend |
| **Quota unit** | Cents × 1000 (integer) | Atomic IncrBy/DecrBy, no float issues |
| **Chunk sizing** | Dynamic (last_actual × 20) + overage | Adapts to context growth |
| **Lua usage** | Chunk grant ONLY | Atomic read-calculate-deduct-return |
| **Plain ops** | Return, read, overage post | Already atomic, no Lua needed |
| **Chunk exhaustion mid-turn** | Let complete, track negative | No mid-stream failures |
| **Negative quota** | Allowed, debt to next period | Honest accounting |
| **Crash loss** | Accepted | Simplicity > perfection |
| **Unused return** | On session end / 5-min timeout | Minimize loss |
| **Multi-agent** | Per-agent chunks, Lua serializes | No coordination needed |
| **Warnings** | WebSocket event at ≤7% | Clean separation from LLM response |
| **Token estimation** | Eino for first, actual for rest | Accurate after first turn |
| **Redis setup** | Simple client, no cluster/sentinel | Managed service handles HA |

---

## Implementation Phases (Future)

### Phase 1: Core Quota Manager
- Redis key management
- Lua script for chunk grant
- Session state management
- Idle timeout goroutine

### Phase 2: Middleware Integration
- QuotaMiddleware struct
- BeforeLLM / AfterLLM hooks
- WebSocket event emission
- Cost calculation integration

### Phase 3: Eino Integration (After LLM Layer)
- Replace SDK client with eino
- Token estimation from eino
- Unified agent runtime

---

## Related Documents

- `.claude/brainstorming/llm-proxy-design.md` - Original proxy design
- `.claude/brainstorming/2026-02-17-multi-agent-storage-security-design.md` - Multi-agent architecture
- `llm-proxy-sdk/pkg/quota/quota.go` - Current quota implementation (reference patterns)
- `llm-proxy-sdk/pkg/models/costs.go` - Cost calculation (reusable)
- `config/models.yaml` - Model cost configurations

---

## Status

**🧠 Brainstorming complete** - Comprehensive design documented

**Next steps:**
1. ✅ Capture all decisions and rationale
2. ✅ Save to `.claude/brainstorming/2026-02-26-user-quota-system-design.md`
3. Wait for LLM/eino integration
4. Create formal implementation plan when ready to build
