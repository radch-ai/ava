# Multi-Agent Storage & Security Design - Decision Log

**Date:** 2026-02-17
**Topic:** Agent layer architecture with multi-agent per user, S3 storage, and security isolation

---

## Initial Context

**Starting Point:**
- Gateway has session-based agents (one agent per WebSocket connection)
- Agents keyed by `sessionID` only
- No persistence layer (in-memory only)
- Nodes register tools to agents
- Auth middleware protects WebSocket endpoints

**User's Reference:** OpenClaw agent architecture

---

## Questions Explored

### 1. What is the security model?

**Current State Assessment:**
- Auth middleware validates session cookies before WebSocket upgrade
- `userID` extracted and stored in request context
- Agent lookup uses `sessionID` as map key, but receives `userID` from context
- **Issue:** sessionID as key = one agent per session, can't have multiple agents per user

**Decision:** Refactor to `(userID, agentID)` composite key
- Enables multiple agents per user
- Agent lifecycle independent of WebSocket connection
- Nodes can correctly add tools to the right user's agents

### 2. How should tools be scoped?

**Options Considered:**
- Per-agent tools: Each agent has its own tool set. Best isolation.
- User-level tool pool: All agents share user's tools. Simpler, less isolation.
- Scoped tools: Configurable scope flags. Most flexible.

**Decision:** Deferred - to be determined based on implementation needs.

### 3. What storage architecture?

**User's Requirement:**
- Not in-memory from start
- Persistence built-in from day one
- Similar to OpenClaw: local storage replaced with cloud (S3-compatible)
- Industry standard approach

**Options Explored:**
- Object Storage (S3): Cheap, scalable, higher latency
- Block Storage (EBS): Fast, expensive, not shared
- Document DB (MongoDB): Queryable, flexible
- Vector DB: For semantic search
- Relational DB (PostgreSQL): ACID, structured

**Reference Research:**
- ChatGPT: Hot cache (Redis) + cold storage (S3-like) + vector DB for memory
- OpenClaw: JSONL transcripts + separate memory files
- Industry pattern: Hot/Warm/Cold layers

### 4. What about OpenClaw's dual-store architecture?

**Key Insight from OpenClaw:**

Two completely different stores:

| Store | Purpose | Content |
|-------|---------|---------|
| **Memory Files** | What the agent knows | Facts, preferences, durable knowledge |
| **Session Transcripts** | What actually happened | JSONL audit trail: user messages, tool calls, results |

When agent says "how did I do that?" → searches **transcripts**, not memory.

**Decision:** Adopt dual-store architecture
- Session transcripts (conversation history) = JSONL in S3
- Memory files = separate store for extracted knowledge
- Warm cache (Redis) for active session state

### 5. How to search S3 efficiently?

**Problem:** Can't scan all S3 files for every search.

**Solution:** Search index pattern
```
S3 (Cold) → Full JSONL files (source of truth)
              ↓
Search Index → Fast pointers (SQLite + vector)
              ↓
          Stream specific results from S3
```

**Decision:** S3 for storage + Search index for discovery

### 6. What search index service?

**Options Considered:**
- SQLite + sqlite-vec: Embedded, single file, does both keyword and vector
- PostgreSQL + pgvector: Extension approach
- Separate vector DB: Weaviate, Qdrant
- Managed cloud: Pinecone

**Decision:** SQLite + sqlite-vec
- Lightweight, can be separate service
- Does both keyword (FTS5) and semantic (vector) search
- Cheap, simple to operate

### 7. How does SQLite service work with S3?

**Architecture:**
```
SQLite (Index): Lightweight metadata + embeddings
     ↓ (returns S3 key + line number)
S3 (Storage): Full JSONL files
     ↓ (stream specific lines)
Result: Relevant conversation chunks
```

**What gets indexed:**
- Not just keywords - meaningful chunks from JSONL
- User messages (full content)
- Assistant responses (meaningful parts)
- Tool names + result summaries
- Error messages

### 8. What level of detail for indexing?

**Options:**
- Full content: ~500 bytes/line, most thorough
- Selective: ~100 bytes/line, smaller index
- Configurable: Different levels per event type

**Decision:** Full content indexing
- Storage is cheap (~$0.50/month per million lines)
- Better user experience (find specific things)
- Can optimize later

**Documented optimization strategies:**
1. Truncate large fields to N characters
2. Skip indexing for certain event types
3. Compress old index data

### 9. How does SQLite service enforce isolation?

**Options:**
- Single table, filter by `user_id` AND `agent_id`
- Separate tables per user
- Separate DB files per user

**Decision:** Single table with query-level filtering
```sql
SELECT s3_key, line_number, content
FROM transcripts
WHERE user_id = ?      -- enforced by service
  AND agent_id = ?     -- enforced by service
  AND content MATCH ?
```

**Isolation layers:**
1. Auth middleware validates user
2. Gateway includes user_id in requests
3. Service ALWAYS filters by both user_id AND agent_id
4. S3 paths organized by user/agent for additional protection

### 10. Workspace-based agents (OpenClaw model)

**Key Insight:** Each agent = isolated workspace folder containing personality/config files

**Workspace Structure:**
```
{user_id}/{agent_id}/
├── workspace/
│   ├── SOUL.md            (persona, tone, boundaries)
│   ├── AGENTS.md          (operating instructions, rules)
│   ├── USER.md            (who the user is)
│   ├── IDENTITY.md        (name, vibe, emoji)
│   ├── TOOLS.md           (tool guidance)
│   └── MEMORY.md          (originally in OpenClaw, now replaced by Mem0)
└── transcripts/
    └── session-*.jsonl
```

**Decision:** Workspace files stored in S3, loaded once per session
- Gateway is a router/executor, not state holder
- Agent becomes "idle" when not active (workspace files remain in S3)
- No in-memory agent state between sessions

### 11. Replacing OpenClaw's memory with Mem0 API

**OpenClaw's MEMORY.md** → Replaced by **Mem0 API**

**Two Memory Scopes in Mem0:**

| Scope | Purpose | Stored As |
|-------|---------|-----------|
| **Long-term** | Facts, preferences, project decisions | `mem0.add(..., user_id=X)` |
| **Session** | Temporary context, current topic | `mem0.add(..., user_id=X, run_id=Y)` |

**Memory Operations:**
- Auto-recall: `mem0.search(query, user_id)` - prepended to each message
- Explicit store: `mem0_search` and `mem0_store` tools available to LLM
- Session auto-capture: Every turn added to session scope
- Promotion: LLM can call `mem0_store` to promote to long-term

### 12. LLM caching strategy

**Key Insight:** Static workspace files should be cached at LLM provider level

**System Prompt (Cached):**
- Base instructions
- SOUL.md, AGENTS.md, USER.md, IDENTITY.md
- Workspace manifest
- Tool definitions

**Dynamic Content (per turn, not cached):**
- `<relevant-memories>` from Mem0 (long-term + session)
- Conversation history (recent turns from memory)
- Current user message

**Result:** ~85% token savings - system prompt cached after first call

### 13. Where to store workspace files? (Warm vs Cold)

**Initial Thought:** Warm storage (Redis/EFS) for frequent access

**Revised Decision:** S3 + LLM cache

**Why:**
- LLM provider caches system prompt after first call
- No need for separate warm layer
- S3 is source of truth, simplifies architecture
- Workspace files only loaded once per session

### 14. Transcript storage (write-only, for audit)

**Decision:** Transcripts stored to S3 for audit/billing/compliance only - NOT searchable

**What transcripts are used for:**
- Audit trails ("what exactly happened?")
- Billing verification
- Compliance requirements
- Future offline analysis

**What transcripts are NOT used for:**
- NOT searched by the gateway
- NOT accessible via tools
- NOT used for agent context

**All memory and search is handled by Mem0.**

**Two search types:**

| Search Type | Stored In | Accessed Via | When |
|-------------|-----------|--------------|------|
| **Memories** | Mem0 API | Auto-recalled (prepended) OR `mem0_search` tool | Every turn |
| **Transcripts** | S3 only | NOT searchable (admin/export only) | Never via tools |

### 15. Session history management

**Question:** Where to store conversation history during active session?

**Decision:** In-memory during session, S3 as source of truth

```go
type Session struct {
    UserID         string
    AgentID        string
    ConversationID string
    Messages       []Message  // Accumulates during session
    StartedAt      time.Time
}
```

**Memory Impact:**
- ~50 KB per 100-turn session
- Cleared on WebSocket disconnect
- S3 transcript is source of truth
- 10K concurrent users ≈ 800 MB RAM (manageable)

### 16. Batching S3 writes (avoiding excessive calls)

**Initial Concern:** Writing every tool call/result to S3 is excessive

**Solution:** Batch within a turn, write once per turn completion

**What gets written when:**

| Event | S3 Write |
|-------|----------|
| User message | ✅ Immediate |
| Tool call (intermediate) | ❌ In memory only |
| Tool result (intermediate) | ❌ In memory only |
| Assistant response (final) | ✅ Immediate (includes all tools) |

**S3 Format:** One complete turn per JSONL line
```jsonl
{"turn":1, "user":"Help me deploy", "assistant":"...", "tools":[{...},{...}]}
```

### 17. Redis buffer vs Direct S3 writes

**Question:** Should we buffer in Redis before writing to S3?

**Cost Analysis:**

| Approach | Cost (10K daily users) | Notes |
|----------|------------------------|-------|
| Direct S3 | ~$5/day + storage | Simple, durable, serverless |
| Redis buffer | ~$15-50/month + ~$1/day S3 | Complex, data loss risk |

**Decision:** Direct S3 writes
- S3 PUT requests are incredibly cheap ($5 per million)
- Redis is more expensive than S3 for storage
- S3 is serverless (no load on our servers)
- Simplicity wins

**Exception:** Redis for quota checks (still needed - high-frequency operations)

### 18. Why Redis for quota but not transcripts?

**Quota Requirements:**
- Checked EVERY turn, before LLM call
- High-frequency reads/writes
- Needs atomic operations (reserve, adjust)
- Database would get loaded under high concurrency

**Transcript Requirements:**
- Write once per turn completion
- S3 is serverless (scales automatically)
- No load on our servers

---

## Final Architecture Decisions (Updated)

### Agent Layer
- **Composite key:** `(userID, agentID)`
- **Multiple agents per user:** Supported from design
- **Agent lifecycle:** Independent of WebSocket connections

### Storage Architecture (Updated - Simplified)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Gateway Service                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    LLM Provider (Anthropic/OpenAI)                  │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  SYSTEM PROMPT CACHE (provider-side, 100% cached)           │   │   │
│  │  │  - Base instructions                                        │   │   │
│  │  │  - SOUL.md, AGENTS.md, USER.md, IDENTITY.md                 │   │   │
│  │  │  - Workspace manifest                                       │   │   │
│  │  │  - TOOL DEFINITIONS: mem0_search, mem0_store,               │   │   │
│  │  │                     read_agent_file, write_agent_file        │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  │                              ↓ (dynamic content per turn)            │   │
│  │  ┌─────────────────────────────────────────────────────────────┐   │   │
│  │  │  - Retrieved memories (Mem0)                                │   │   │
│  │  │  - Conversation history (in-memory during session)          │   │   │
│  │  │  - Current user message                                     │   │   │
│  │  └─────────────────────────────────────────────────────────────┘   │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                    ↕                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                     PARALLEL FETCH (on first request)                │   │
│  │  ┌────────────────────┐    ┌────────────────────┐                   │   │
│  │  │   Mem0 API         │    │   S3 Workspace     │                   │   │
│  │  │ - Long-term memory │    │ - SOUL.md          │                   │   │
│  │  │ - Session memory   │    │ - AGENTS.md        │                   │   │
│  │  └────────────────────┘    │ - USER.md, etc.    │                   │   │
│  │                            └────────────────────┘                   │   │
│  │                            (subsequent: workspace cached at LLM)      │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    Session (In-Memory)                              │   │
│  │  - Messages array (accumulates during session)                      │   │
│  │  - Cleared on WebSocket disconnect                                   │   │
│  │  - ~50 KB per 100-turn session                                      │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    Redis (for quota only)                           │   │
│  │  - High-frequency quota checks                                      │   │
│  │  - Atomic reserve/adjust operations                                 │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  NO SQLite search service - ALL search handled by Mem0                      │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
                                    ↕
┌─────────────────────────────────────────────────────────────────────────────┐
│                              COLD STORAGE (S3)                              │
│  s3://bucket/                                                               │
│    ├── {user_id}/{agent_id}/                                               │
│    │   ├── workspace/              ← Fetched once, cached by LLM            │
│    │   │   ├── SOUL.md                                                     │
│    │   │   ├── AGENTS.md                                                   │
│    │   │   ├── USER.md                                                     │
│    │   │   └── IDENTITY.md                                                 │
│    │   │                                                                   │
│    │   └── transcripts/            ← Write-only for audit/billing           │
│    │       └── session-{timestamp}.jsonl (NOT searchable)                  │
│                                                                              │
│  NO MEMORY.md or memory folder — replaced by Mem0 API                        │
│  NO SQLite index — transcripts are write-only                              │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                         Mem0 API (External)                                 │
│  - Handles ALL memory and search:                                          │
│    - Long-term memories (facts, preferences, decisions)                    │
│    - Session memories (temporary context)                                  │
│    - Semantic search (auto-recalled + tools)                               │
│  - Auto-recalled each turn and prepended to user message                    │
│  - Also accessible via mem0_search and mem0_store tools                     │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Request Flow Per Turn

```
1. User sends message via WebSocket
   ↓
2. PARALLEL FETCH (~200ms total):
   ├─ Mem0.search(query) → relevant memories
   └─ S3: Get workspace files (first time only, subsequent calls cached at LLM)
   ↓
3. Build LLM messages:
   ├─ System prompt (cached by LLM provider after first call)
   │  ├─ Base instructions
   │  ├─ Workspace files (SOUL, AGENTS, USER, IDENTITY)
   │  ├─ Workspace manifest
   │  └─ Tool definitions
   └─ User message with:
      ├─ <relevant-memories> from Mem0
      ├─ Recent conversation history (from in-memory session)
      └─ Current user message
   ↓
4. LLM call (streaming response)
   ↓
5. Stream assistant response to WebSocket
   ↓
6. ASYNC (non-blocking, fire-and-forget):
   ├─ Append complete turn to S3 (user + assistant + tools)
   ├─ mem0.add() to session scope
   └─ Update SQLite search index
   ↓
7. In-memory session updated with new messages
```

### Data Model (Updated)

**S3 Structure:**
```
s3://bucket/
  └── {user_id}/
      └── {agent_id}/
          ├── workspace/              ← Agent's personality/config
          │   ├── SOUL.md            (persona, tone, boundaries)
          │   ├── AGENTS.md          (operating instructions, rules)
          │   ├── USER.md            (who the user is)
          │   ├── IDENTITY.md        (name, vibe, emoji)
          │   └── TOOLS.md           (tool guidance)
          │
          └── transcripts/            ← Full conversation history
              ├── session-{timestamp}.jsonl
              └── session-{timestamp}.jsonl

Note: NO memory folder - replaced by Mem0 API
```

**S3 Transcript Format (one turn per line):**
```jsonl
{"turn":1,"user":"Help me deploy","assistant":"Let me check the pods...","tools":[{"name":"list_pods","result":{"pods":["redis-abc","nginx-xyz"]}}],"timestamp":1737123456}
{"turn":2,"user":"What about redis?","assistant":"Redis is having connection issues...","tools":[{"name":"get_logs","result":{"logs":"Error: connection refused"}}],"timestamp":1737123459}
```

**SQLite Schema (transcript search index):**
```sql
CREATE TABLE transcripts (
    id INTEGER PRIMARY KEY,
    user_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    s3_key TEXT NOT NULL,
    turn_number INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    content TEXT NOT NULL,
    embedding BLOB,
    UNIQUE(s3_key, turn_number)
);

CREATE VIRTUAL TABLE transcripts_fts USING fts5(content);
```

**In-Memory Session:**
```go
type Session struct {
    UserID         string
    AgentID        string
    ConversationID string
    Messages       []Message    // Accumulates, ~50KB per 100 turns
    Mem0SessionID  string       // For session-scoped memories
    StartedAt      time.Time
    LastActivity   time.Time
}

type Message struct {
    Role      string  // "user" or "assistant"
    Content   string
    Tools     []ToolCall    // Accumulated during turn
    Timestamp int64
}
```

### Security Isolation

1. **Auth layer:** Session validation via auth service
2. **Context propagation:** user_id in request context
3. **Query enforcement:** Service filters by user_id AND agent_id
4. **S3 path isolation:** Organized by user/agent
5. **Mem0 scoping:** user_id + optional run_id for isolation

### Tools Available to LLM

| Tool | Purpose | Storage Backend |
|------|---------|-----------------|
| `mem0_search(query)` | Search memories | Mem0 API |
| `mem0_store(content)` | Store long-term memory | Mem0 API |
| `read_agent_file(path)` | Read workspace file | S3 |
| `write_agent_file(path, content)` | Write workspace file | S3 |

**Removed:**
- `search_agent_transcript` - transcripts are write-only for audit
- No transcript search tools

---

## Open Questions / Deferred

1. ~~Tool scoping~~ → **Resolved: Agent-scoped with client handling bulk ops**
2. ~~Vector embedding provider~~ → **Resolved: Not needed - Mem0 handles all search**
3. ~~Index build strategy~~ → **Resolved: No search index - transcripts write-only**
4. **Session timeout:** When to mark session as "finished"

---

## Key Decisions Summary

| Decision | Rationale |
|----------|-----------|
| **Workspace-based agents** | Each agent = isolated folder with personality files |
| **Mem0 for ALL memory/search** | Replaces OpenClaw's MEMORY.md, handles semantic search |
| **LLM cache for workspace** | Static files cached at provider, no warm layer needed |
| **Direct S3 writes** | Cheaper and simpler than Redis buffer |
| **Redis for quota only** | High-frequency operations need low latency |
| **In-memory session history** | Simple, fast; S3 transcript is source of truth |
| **Batch tool calls in turns** | One S3 write per turn completion, not per tool call |
| **Transcripts write-only** | S3 for audit/billing only; Mem0 handles all search |
| **NO SQLite search service** | Removed - unnecessary complexity |
| **NO transcript search tools** | All search via Mem0 |

---

## Status

**✅ Design formalized** - See `.claude/plans/2026-02-17-multi-agent-storage-security.md`

This brainstorming document preserves the decision-making process, alternatives considered, and rationale. The formal design document contains the implementation-ready specification.

## Next Steps

1. ✅ Write formal design document → **DONE**
2. Create detailed implementation plan (break down into files/functions)
3. Implement agent refactoring (sessionID → userID+agentID)
4. Implement Mem0 integration
5. Implement workspace file loading from S3
6. Implement S3 transcript storage (write-only, async)
7. Regenerate docs/ from codebase after implementation

**Removed from scope:**
- SQLite search service implementation
- Transcript search indexing
- Vector embeddings for transcripts
