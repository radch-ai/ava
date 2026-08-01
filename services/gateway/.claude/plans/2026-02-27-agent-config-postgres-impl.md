# Agent Config Postgres Storage Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate agent configuration from S3 to Postgres for transactional integrity and easier schema management.

**Architecture:** Add `agents` table to Postgres with markdown files as TEXT columns and structured config as JSONB. Create `AgentStore` interface for CRUD operations. Update `WorkspaceLoader` to read from Postgres. Keep transcripts in S3.

**Tech Stack:** Go, pgx/v5, existing billing migrations pattern

---

### Task 1: Add Agents Table Migration

**Files:**
- Modify: `internal/billing/migrations.go`

**Step 1: Add migration to migrations slice**

Add after the existing migrations in `internal/billing/migrations.go`:

```go
{
    Name: "create_agents_table",
    SQL: `
        CREATE TABLE IF NOT EXISTS agents (
            agent_id     TEXT PRIMARY KEY,
            user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
            name         TEXT,
            soul_md      TEXT,
            agents_md    TEXT,
            user_md      TEXT,
            identity_md  TEXT,
            tools_md     TEXT,
            heartbeat_md TEXT,
            config       JSONB,
            created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
            updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )
    `,
},
{
    Name: "create_agents_indexes",
    SQL: `
        CREATE INDEX IF NOT EXISTS idx_agents_user_id ON agents(user_id);
        CREATE INDEX IF NOT EXISTS idx_agents_updated_at ON agents(updated_at);
    `,
},
```

**Step 2: Add existence check to checkExistenceSQL**

Add cases to the switch statement:

```go
case "create_agents_table":
    return `SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'agents')`
case "create_agents_indexes":
    return `SELECT EXISTS (SELECT FROM pg_indexes WHERE indexname = 'idx_agents_user_id')`
```

**Step 3: Run unit tests**

Run: `make test-unit`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/billing/migrations.go
git commit -m "feat(storage): add agents table migration"
```

---

### Task 2: Create Agent Model and Store Interface

**Files:**
- Create: `internal/storage/agent_store.go`
- Create: `tests/unit/storage/agent_store_test.go`

**Step 1: Write the failing test**

Create `tests/unit/storage/agent_store_test.go`:

```go
package storage_test

import (
    "context"
    "testing"

    "github.com/nevaria-ai/gateway/internal/storage"
)

func TestAgentConfig_Get(t *testing.T) {
    ctx := context.Background()
    store := storage.NewMockAgentStore()

    _, err := store.Get(ctx, "user-1", "agent-1")
    if err == nil {
        t.Error("expected error for non-existent agent")
    }
}

func TestAgentConfig_UpsertAndGet(t *testing.T) {
    ctx := context.Background()
    store := storage.NewMockAgentStore()

    config := &storage.AgentConfig{
        AgentID:     "agent-1",
        UserID:      "user-1",
        Name:        "Test Agent",
        SoulMd:      "# Soul\nBe helpful.",
        AgentsMd:    "# Agents\nBe concise.",
        UserMd:      "# User\nCustom info.",
        IdentityMd:  "# Identity\nName: Bot",
        ToolsMd:     "# Tools\nUse carefully.",
        HeartbeatMd: "# Heartbeat\nStatus info.",
        Config:      map[string]any{"tool_whitelist": []string{"read_file"}},
    }

    err := store.Upsert(ctx, config)
    if err != nil {
        t.Fatalf("Upsert failed: %v", err)
    }

    got, err := store.Get(ctx, "user-1", "agent-1")
    if err != nil {
        t.Fatalf("Get failed: %v", err)
    }

    if got.AgentID != config.AgentID {
        t.Errorf("AgentID = %q, want %q", got.AgentID, config.AgentID)
    }
    if got.SoulMd != config.SoulMd {
        t.Errorf("SoulMd = %q, want %q", got.SoulMd, config.SoulMd)
    }
}

func TestAgentConfig_Delete(t *testing.T) {
    ctx := context.Background()
    store := storage.NewMockAgentStore()

    config := &storage.AgentConfig{
        AgentID: "agent-1",
        UserID:  "user-1",
    }
    _ = store.Upsert(ctx, config)

    err := store.Delete(ctx, "user-1", "agent-1")
    if err != nil {
        t.Fatalf("Delete failed: %v", err)
    }

    _, err = store.Get(ctx, "user-1", "agent-1")
    if err == nil {
        t.Error("expected error after delete")
    }
}

func TestAgentConfig_ListByUser(t *testing.T) {
    ctx := context.Background()
    store := storage.NewMockAgentStore()

    _ = store.Upsert(ctx, &storage.AgentConfig{AgentID: "agent-1", UserID: "user-1", Name: "Agent 1"})
    _ = store.Upsert(ctx, &storage.AgentConfig{AgentID: "agent-2", UserID: "user-1", Name: "Agent 2"})
    _ = store.Upsert(ctx, &storage.AgentConfig{AgentID: "agent-3", UserID: "user-2", Name: "Agent 3"})

    agents, err := store.ListByUser(ctx, "user-1")
    if err != nil {
        t.Fatalf("ListByUser failed: %v", err)
    }

    if len(agents) != 2 {
        t.Errorf("ListByUser returned %d agents, want 2", len(agents))
    }
}
```

**Step 2: Run test to verify it fails**

Run: `make test-unit TEST=tests/unit/storage/agent_store_test.go`
Expected: FAIL - undefined storage.AgentConfig, AgentStore

**Step 3: Create AgentConfig model and interfaces**

Create `internal/storage/agent_store.go`:

```go
package storage

import (
    "context"
    "fmt"
    "sync"
)

type AgentConfig struct {
    AgentID     string
    UserID      string
    Name        string
    SoulMd      string
    AgentsMd    string
    UserMd      string
    IdentityMd  string
    ToolsMd     string
    HeartbeatMd string
    Config      map[string]any
}

type AgentStore interface {
    Get(ctx context.Context, userID, agentID string) (*AgentConfig, error)
    Upsert(ctx context.Context, config *AgentConfig) error
    Delete(ctx context.Context, userID, agentID string) error
    ListByUser(ctx context.Context, userID string) ([]*AgentConfig, error)
}

type MockAgentStore struct {
    mu   sync.RWMutex
    data map[string]*AgentConfig
}

func NewMockAgentStore() *MockAgentStore {
    return &MockAgentStore{
        data: make(map[string]*AgentConfig),
    }
}

func (m *MockAgentStore) key(userID, agentID string) string {
    return fmt.Sprintf("%s/%s", userID, agentID)
}

func (m *MockAgentStore) Get(ctx context.Context, userID, agentID string) (*AgentConfig, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    config, ok := m.data[m.key(userID, agentID)]
    if !ok {
        return nil, fmt.Errorf("agent not found: %s/%s", userID, agentID)
    }
    return config, nil
}

func (m *MockAgentStore) Upsert(ctx context.Context, config *AgentConfig) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.data[m.key(config.UserID, config.AgentID)] = config
    return nil
}

func (m *MockAgentStore) Delete(ctx context.Context, userID, agentID string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    delete(m.data, m.key(userID, agentID))
    return nil
}

func (m *MockAgentStore) ListByUser(ctx context.Context, userID string) ([]*AgentConfig, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    var result []*AgentConfig
    for key, config := range m.data {
        if len(key) > len(userID) && key[:len(userID)] == userID {
            result = append(result, config)
        }
    }
    return result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `make test-unit TEST=tests/unit/storage/agent_store_test.go`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/storage/agent_store.go tests/unit/storage/agent_store_test.go
git commit -m "feat(storage): add AgentConfig model and MockAgentStore"
```

---

### Task 3: Create PostgresAgentStore

**Files:**
- Modify: `internal/storage/agent_store.go`
- Modify: `tests/unit/storage/agent_store_test.go`

**Step 1: Write the failing test**

Add to `tests/unit/storage/agent_store_test.go`:

```go
func TestPostgresAgentStore_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    
    ctx := context.Background()
    pool := setupTestDB(t)
    defer pool.Close()

    store := storage.NewPostgresAgentStore(pool)

    config := &storage.AgentConfig{
        AgentID:     "agent-pg-1",
        UserID:      "user-pg-1",
        Name:        "Postgres Agent",
        SoulMd:      "# Soul",
        AgentsMd:    "# Agents",
        Config:      map[string]any{"tool_whitelist": []string{"read_file"}},
    }

    err := store.Upsert(ctx, config)
    if err != nil {
        t.Fatalf("Upsert failed: %v", err)
    }

    got, err := store.Get(ctx, "user-pg-1", "agent-pg-1")
    if err != nil {
        t.Fatalf("Get failed: %v", err)
    }

    if got.Name != config.Name {
        t.Errorf("Name = %q, want %q", got.Name, config.Name)
    }

    err = store.Delete(ctx, "user-pg-1", "agent-pg-1")
    if err != nil {
        t.Fatalf("Delete failed: %v", err)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `make test-integration`
Expected: FAIL - undefined PostgresAgentStore

**Step 3: Add PostgresAgentStore implementation**

Add to `internal/storage/agent_store.go`:

```go
import (
    "github.com/jackc/pgx/v5/pgxpool"
)

type PostgresAgentStore struct {
    db *pgxpool.Pool
}

func NewPostgresAgentStore(db *pgxpool.Pool) *PostgresAgentStore {
    return &PostgresAgentStore{db: db}
}

func (p *PostgresAgentStore) Get(ctx context.Context, userID, agentID string) (*AgentConfig, error) {
    query := `
        SELECT agent_id, user_id, name, soul_md, agents_md, user_md, 
               identity_md, tools_md, heartbeat_md, config
        FROM agents WHERE user_id = $1 AND agent_id = $2
    `
    
    var config AgentConfig
    err := p.db.QueryRow(ctx, query, userID, agentID).Scan(
        &config.AgentID, &config.UserID, &config.Name,
        &config.SoulMd, &config.AgentsMd, &config.UserMd,
        &config.IdentityMd, &config.ToolsMd, &config.HeartbeatMd,
        &config.Config,
    )
    if err != nil {
        return nil, fmt.Errorf("get agent: %w", err)
    }
    return &config, nil
}

func (p *PostgresAgentStore) Upsert(ctx context.Context, config *AgentConfig) error {
    query := `
        INSERT INTO agents (agent_id, user_id, name, soul_md, agents_md, 
                           user_md, identity_md, tools_md, heartbeat_md, config,
                           created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
        ON CONFLICT (agent_id) DO UPDATE SET
            name = EXCLUDED.name,
            soul_md = EXCLUDED.soul_md,
            agents_md = EXCLUDED.agents_md,
            user_md = EXCLUDED.user_md,
            identity_md = EXCLUDED.identity_md,
            tools_md = EXCLUDED.tools_md,
            heartbeat_md = EXCLUDED.heartbeat_md,
            config = EXCLUDED.config,
            updated_at = NOW()
    `
    _, err := p.db.Exec(ctx, query,
        config.AgentID, config.UserID, config.Name,
        config.SoulMd, config.AgentsMd, config.UserMd,
        config.IdentityMd, config.ToolsMd, config.HeartbeatMd,
        config.Config,
    )
    return err
}

func (p *PostgresAgentStore) Delete(ctx context.Context, userID, agentID string) error {
    _, err := p.db.Exec(ctx, "DELETE FROM agents WHERE user_id = $1 AND agent_id = $2", userID, agentID)
    return err
}

func (p *PostgresAgentStore) ListByUser(ctx context.Context, userID string) ([]*AgentConfig, error) {
    query := `
        SELECT agent_id, user_id, name, soul_md, agents_md, user_md,
               identity_md, tools_md, heartbeat_md, config
        FROM agents WHERE user_id = $1
    `
    
    rows, err := p.db.Query(ctx, query, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var agents []*AgentConfig
    for rows.Next() {
        var config AgentConfig
        err := rows.Scan(
            &config.AgentID, &config.UserID, &config.Name,
            &config.SoulMd, &config.AgentsMd, &config.UserMd,
            &config.IdentityMd, &config.ToolsMd, &config.HeartbeatMd,
            &config.Config,
        )
        if err != nil {
            return nil, err
        }
        agents = append(agents, &config)
    }
    return agents, nil
}
```

**Step 4: Run tests**

Run: `make test-unit`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/storage/agent_store.go tests/unit/storage/agent_store_test.go
git commit -m "feat(storage): add PostgresAgentStore implementation"
```

---

### Task 4: Update WorkspaceLoader to Use AgentStore

**Files:**
- Modify: `internal/storage/workspace.go`
- Modify: `tests/unit/storage/workspace_test.go`

**Step 1: Write the failing test**

Update `tests/unit/storage/workspace_test.go` to test loading from AgentStore:

```go
func TestWorkspaceLoader_FromAgentStore(t *testing.T) {
    ctx := context.Background()
    agentStore := storage.NewMockAgentStore()
    
    config := &storage.AgentConfig{
        AgentID:    "agent-1",
        UserID:     "user-1",
        SoulMd:     "# Soul\nYou are helpful.",
        AgentsMd:   "# Instructions\nBe concise.",
        UserMd:     "# User\nCustom info.",
        IdentityMd: "# Identity\nBot",
        ToolsMd:    "# Tools\nUse carefully.",
    }
    _ = agentStore.Upsert(ctx, config)

    loader := storage.NewWorkspaceLoaderFromAgentStore(agentStore)
    workspace, err := loader.Load(ctx, "user-1", "agent-1")
    if err != nil {
        t.Fatalf("Load failed: %v", err)
    }

    if workspace.Soul != config.SoulMd {
        t.Errorf("Soul = %q, want %q", workspace.Soul, config.SoulMd)
    }
    if workspace.Agents != config.AgentsMd {
        t.Errorf("Agents = %q, want %q", workspace.Agents, config.AgentsMd)
    }
    if !workspace.Loaded {
        t.Error("Expected Loaded = true")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `make test-unit TEST=tests/unit/storage/workspace_test.go`
Expected: FAIL - undefined NewWorkspaceLoaderFromAgentStore

**Step 3: Update WorkspaceLoader**

Modify `internal/storage/workspace.go`:

```go
type WorkspaceLoader struct {
    client      Client
    agentStore  AgentStore
}

func NewWorkspaceLoader(client Client) *WorkspaceLoader {
    return &WorkspaceLoader{client: client}
}

func NewWorkspaceLoaderFromAgentStore(store AgentStore) *WorkspaceLoader {
    return &WorkspaceLoader{agentStore: store}
}

func (l *WorkspaceLoader) Load(ctx context.Context, userID, agentID string) (*Workspace, error) {
    if l.agentStore != nil {
        return l.loadFromStore(ctx, userID, agentID)
    }
    return l.loadFromS3(ctx, userID, agentID)
}

func (l *WorkspaceLoader) loadFromStore(ctx context.Context, userID, agentID string) (*Workspace, error) {
    config, err := l.agentStore.Get(ctx, userID, agentID)
    if err != nil {
        return &Workspace{}, nil
    }

    workspace := &Workspace{
        Soul:     config.SoulMd,
        Agents:   config.AgentsMd,
        User:     config.UserMd,
        Identity: config.IdentityMd,
        Tools:    config.ToolsMd,
    }

    if config.SoulMd != "" || config.AgentsMd != "" || config.UserMd != "" {
        workspace.Loaded = true
    }

    return workspace, nil
}

func (l *WorkspaceLoader) loadFromS3(ctx context.Context, userID, agentID string) (*Workspace, error) {
    // ... existing implementation ...
}
```

**Step 4: Run tests**

Run: `make test-unit`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/storage/workspace.go tests/unit/storage/workspace_test.go
git commit -m "feat(storage): add WorkspaceLoader from AgentStore"
```

---

### Task 5: Clean Up S3 Workspace Functions

**Files:**
- Modify: `internal/storage/s3.go`
- Modify: `internal/storage/workspace.go`
- Modify: `tests/unit/storage/s3_test.go`

**Step 1: Remove WorkspacePath from s3.go**

Remove the `WorkspacePath` function from `internal/storage/s3.go`. Keep `TranscriptPath`.

**Step 2: Update workspace.go**

Remove the `loadFile` method and S3-related code from the AgentStore path.

**Step 3: Update tests**

Remove workspace-related tests from `tests/unit/storage/s3_test.go`.

**Step 4: Run tests**

Run: `make test-unit`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/storage/s3.go internal/storage/workspace.go tests/unit/storage/s3_test.go
git commit -m "refactor(storage): remove S3 workspace functions, keep transcripts only"
```

---

## Summary

| Task | Description |
|------|-------------|
| 1 | Add agents table migration |
| 2 | Create AgentConfig model and MockAgentStore |
| 3 | Create PostgresAgentStore implementation |
| 4 | Update WorkspaceLoader to use AgentStore |
| 5 | Clean up S3 workspace functions |

## Dependencies

- Task 1 must complete before Task 3 (migration needed for PostgresAgentStore)
- Task 2 must complete before Task 3 (interface needed)
- Task 2 and 3 must complete before Task 4 (AgentStore needed by WorkspaceLoader)
