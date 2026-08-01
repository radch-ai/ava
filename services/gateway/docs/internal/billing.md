# Billing

RevenueCat webhook handling, quota management, and database migrations for subscription billing.

## Overview

<!-- Overview replaced by individual function flowcharts below -->

## Structs

### QuotaManager

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `QuotaManager` | `db` | `*pgxpool.Pool` | Database connection pool |

### Migration

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Migration` | `Name` | `string` | Migration name |
| | `SQL` | `string` | SQL to execute |

### MigrationResult

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `MigrationResult` | `MigrationName` | `string` | Migration name |
| | `AlreadyExists` | `bool` | Already applied |
| | `Error` | `error` | Error if any |

## Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `TierFree` | `int` | `0` | Free tier |
| `TierPro` | `int` | `1` | Pro tier |
| `TierEnterprise` | `int` | `2` | Enterprise tier |

## Database Schema

```mermaid
erDiagram
    users ||--o{ usage_logs : "generates"
    users {
        TEXT id PK
        TEXT email
        INTEGER tier
        NUMERIC credits_remaining
        TEXT subscription_tier
        TEXT subscription_status
        INTEGER billing_period_start
        INTEGER billing_period_end
        TEXT revenuecat_customer_id
        INTEGER created_at
        INTEGER updated_at
    }
    usage_logs {
        BIGSERIAL id PK
        TEXT user_id FK
        TEXT agent_id
        INTEGER tokens_used
        NUMERIC credit_cost
        TIMESTAMPTZ timestamp
    }
    processed_webhooks {
        TEXT event_id PK
        INTEGER processed_at
        TEXT event_type
    }
```

### Relationships

| Relationship | Type | Description |
|--------------|------|-------------|
| `users` → `usage_logs` | 1:N | Usage tracking per user |

### Cascade Behavior

| Table | On Delete | Behavior |
|-------|-----------|----------|
| `users` | CASCADE | Delete all usage logs for user |

### Key Constraints

| Table | Constraints |
|-------|-------------|
| `users` | CHECK (tier IN (0, 1, 2)) |

## Functions

### VerifyWebhookSignature

```mermaid
flowchart TB
    Start([payload, signature, secret]) --> HVerify["**VerifyWebhookSignature(payload, signature, secret) → err**"]
    HVerify --> CheckSig["signature == \"\""]
    CheckSig -->|true| ESigEmpty(["return error"])
    CheckSig -->|false| CheckSecret["secret == \"\""]
    CheckSecret -->|true| ESecretEmpty(["return error"])
    CheckSecret -->|false| NewMAC["hmac.New(sha256.New, []byte(secret)) → mac"]
    NewMAC --> Write["mac.Write(payload)"]
    Write --> Expected["expectedSig ← hex.EncodeToString(mac.Sum(nil))"]
    Expected --> Equal["hmac.Equal([]byte(signature), []byte(expectedSig))"]
    Equal -->|false| EInvalid(["return error"])
    Equal -->|true| OK(["return nil"])

    click NewMAC href "https://pkg.go.dev/crypto/hmac#New"
    click Write href "https://pkg.go.dev/hash#Hash.Write"
    click Expected href "https://pkg.go.dev/encoding/hex#EncodeToString"
    click Equal href "https://pkg.go.dev/crypto/hmac#Equal"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,OK mainFn
    class HVerify internalFn
    class NewMAC,Write,Expected,Equal localPkgs
    class ESigEmpty,ESecretEmpty,EInvalid errorFlow
```

### NewQuotaManager

```mermaid
flowchart TB
    Start([db *pgxpool.Pool]) --> HNewQM["**NewQuotaManager(db) → *QuotaManager**"]
    HNewQM --> CreateQM["qm ← &QuotaManager"]
    CreateQM --> SetDB["qm.db ← db"]
    SetDB --> Return["return qm"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return mainFn
    class HNewQM internalFn
```

### CheckQuota

```mermaid
flowchart TB
    Start([qm *QuotaManager, ctx, userID, creditsNeeded]) --> HCheckQuota["**(qm *QuotaManager).CheckQuota(...) → err**"]
    HCheckQuota --> EStub(["return fmt.Errorf(\"CheckQuota not yet implemented (Wave 4)\")"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,EStub mainFn
    class HCheckQuota internalFn
    class EStub errorFlow
```

### ReserveCredit

```mermaid
flowchart TB
    Start([qm *QuotaManager, ctx, userID, amount]) --> HReserve["**(qm *QuotaManager).ReserveCredit(...) → err**"]
    HReserve --> EStub(["return fmt.Errorf(\"ReserveCredit not yet implemented (Wave 4)\")"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,EStub mainFn
    class HReserve internalFn
    class EStub errorFlow
```

### DeductCredit

```mermaid
flowchart TB
    Start([qm *QuotaManager, ctx, userID, agentID, tokensUsed, creditCost]) --> HDeduct["**(qm *QuotaManager).DeductCredit(...) → err**"]
    HDeduct --> EStub(["return fmt.Errorf(\"DeductCredit not yet implemented (Wave 4)\")"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,EStub mainFn
    class HDeduct internalFn
    class EStub errorFlow
```

### RunMigrations

```mermaid
flowchart TB
    Start([ctx, dbURL]) --> HRunMigs["**RunMigrations(ctx, dbURL) → err**"]
    HRunMigs --> CheckURL["dbURL == \"\""]
    CheckURL -->|true| EURL(["return error"])
    CheckURL -->|false| ParseConfig["pgxpool.ParseConfig(dbURL) → config, err"]
    ParseConfig -->|error| EParse(["return error"])
    ParseConfig --> SetMaxConns["config.MaxConns ← 5"]
    SetMaxConns --> SetMinConns["config.MinConns ← 1"]
    SetMinConns --> HTimeout["ctxTimeout, cancel ← context.WithTimeout(ctx, 30*time.Second)"]
    HTimeout --> NewPool["pgxpool.NewWithConfig(ctxTimeout, config) → pool, err"]
    NewPool -->|error| EPool(["return error"])
    NewPool --> Ping["pool.Ping(ctxTimeout) → err"]
    Ping -->|error| EPing(["return error"])
    Ping --> Loop["for migration ← range migrations"]
    Loop --> HRunMig["**runMigration(ctx, pool, migration) → err**"]
    HRunMig -->|error| EMig(["return error"])
    HRunMig --> Loop
    Loop -->|done| ClosePool["pool.Close()"]
    ClosePool --> ReturnOK(["return nil"])

    click ParseConfig href "https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool#ParseConfig"
    click NewPool href "https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool#NewWithConfig"
    click Ping href "https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool#Pool.Ping"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnOK mainFn
    class HRunMigs,HRunMig internalFn
    class ParseConfig,NewPool,Ping localPkgs
    class EURL,EParse,EPool,EPing,EMig errorFlow
```

### RunMigrationsWithPool

```mermaid
flowchart TB
    Start([ctx, pool *pgxpool.Pool]) --> HRunWithPool["**RunMigrationsWithPool(ctx, pool) → err**"]
    HRunWithPool --> CheckNil["pool == nil"]
    CheckNil -->|true| EPoolNil(["return error"])
    CheckNil -->|false| Loop["for migration ← range migrations"]
    Loop --> HRunMig["**runMigration(ctx, pool, migration) → err**"]
    HRunMig -->|error| EMig(["return error"])
    HRunMig --> Loop
    Loop -->|done| ReturnOK(["return nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnOK mainFn
    class HRunWithPool,HRunMig internalFn
    class EPoolNil,EMig errorFlow
```

### CheckMigrations

```mermaid
flowchart TB
    Start([ctx, dbURL]) --> HCheckMigs["**CheckMigrations(ctx, dbURL) → results, err**"]
    HCheckMigs --> CheckURL["dbURL == \"\""]
    CheckURL -->|true| EURL(["return nil, error"])
    CheckURL -->|false| ParseConfig["pgxpool.ParseConfig(dbURL) → config, err"]
    ParseConfig -->|error| EParse(["return nil, error"])
    ParseConfig --> SetMaxConns["config.MaxConns ← 2"]
    SetMaxConns --> HTimeout["ctxTimeout, cancel ← context.WithTimeout(ctx, 10*time.Second)"]
    HTimeout --> NewPool["pgxpool.NewWithConfig(ctxTimeout, config) → pool, err"]
    NewPool -->|error| EPool(["return nil, error"])
    NewPool --> MakeResults["results ← make([]MigrationResult, len(migrations))"]
    MakeResults --> Loop["for i, migration ← range migrations"]
    Loop --> SetResult["results[i] ← MigrationResult{MigrationName: migration.Name}"]
    SetResult --> HCheckSQL["**checkExistenceSQL(migration) → checkSQL**"]
    HCheckSQL --> QueryRow["pool.QueryRow(ctx, checkSQL) → row"]
    QueryRow --> Scan["row.Scan(&exists) → err"]
    Scan --> CheckErr["err != nil && err != pgx.ErrNoRows"]
    CheckErr -->|true| SetError["results[i].Error ← err"]
    SetError --> Loop
    CheckErr -->|false| SetExists["results[i].AlreadyExists ← exists"]
    SetExists --> Loop
    Loop -->|done| ClosePool["pool.Close()"]
    ClosePool --> ReturnResults(["return results, nil"])

    click ParseConfig href "https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool#ParseConfig"
    click NewPool href "https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool#NewWithConfig"
    click QueryRow href "https://pkg.go.dev/github.com/jackc/pgx/v5#Conn.QueryRow"
    click Scan href "https://pkg.go.dev/github.com/jackc/pgx/v5#Row.Scan"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnResults mainFn
    class HCheckMigs,HCheckSQL internalFn
    class ParseConfig,NewPool,QueryRow,Scan localPkgs
    class EURL,EParse,EPool errorFlow
```

### runMigration

```mermaid
flowchart TB
    Start([ctx, pool, migration]) --> HRunMig["**runMigration(ctx, pool, migration) → err**"]
    HRunMig --> BeginTx["pool.Begin(ctx) → tx, err"]
    BeginTx -->|error| EBegin(["return error"])
    BeginTx --> ExecTx["tx.Exec(ctx, migration.SQL) → _, err"]
    ExecTx -->|error| RollbackDefer["defer tx.Rollback(ctx)"]
    RollbackDefer --> EExec(["return error"])
    ExecTx --> CommitTx["tx.Commit(ctx) → err"]
    CommitTx -->|error| ECommit(["return error"])
    CommitTx --> ReturnOK(["return nil"])

    click BeginTx href "https://pkg.go.dev/github.com/jackc/pgx/v5#TxPool.Begin"
    click ExecTx href "https://pkg.go.dev/github.com/jackc/pgx/v5#Tx.Exec"
    click CommitTx href "https://pkg.go.dev/github.com/jackc/pgx/v5#Tx.Commit"
    click RollbackDefer href "https://pkg.go.dev/github.com/jackc/pgx/v5#Tx.Rollback"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnOK mainFn
    class HRunMig internalFn
    class BeginTx,ExecTx,CommitTx localPkgs
    class EBegin,EExec,ECommit errorFlow
```

### checkExistenceSQL

```mermaid
flowchart TB
    Start([migration]) --> HCheckSQL["**checkExistenceSQL(migration) → checkSQL**"]
    HCheckSQL --> SwitchName["migration.Name"]
    SwitchName -->|"create_users_table"| UsersSQL["return \"SELECT EXISTS...\""]
    SwitchName -->|"create_usage_logs_table"| LogsSQL["return \"SELECT EXISTS...\""]
    SwitchName -->|"create_processed_webhooks_table"| WebhooksSQL["return \"SELECT EXISTS...\""]
    SwitchName -->|"create_users_indexes"| UsersIdxSQL["return \"SELECT EXISTS...\""]
    SwitchName -->|"create_usage_logs_indexes"| LogsIdxSQL["return \"SELECT EXISTS...\""]
    SwitchName -->|"create_processed_webhooks_indexes"| WebhooksIdxSQL["return \"SELECT EXISTS...\""]
    SwitchName -->|"create_usage_logs_foreign_key"| FKSQL["return \"SELECT EXISTS...\""]
    SwitchName -->|default| FalseSQL["return \"SELECT false\""]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start mainFn
    class HCheckSQL internalFn
```

## Design Notes

### HMAC-SHA256 Verification

**Q: Why HMAC-SHA256 for webhook signatures?**

| Aspect | Description |
|--------|-------------|
| Context | Webhook authenticity |
| Options considered | Plain hash, HMAC, asymmetric |
| Rationale | RevenueCat standard, prevents tampering |

**Current approach is recommended because:**
- Industry standard for webhooks
- Prevents replay attacks with timestamp
- Shared secret is simple to manage
- Supported by all major payment providers

### Stub Implementation

**Q: Why are quota methods stub implementations?**

| Aspect | Description |
|--------|-------------|
| Context | Wave 4 implementation |
| Options considered | Full implementation, stub, skip |
| Rationale | Incremental delivery with clear TODOs |

**Current approach is recommended because:**
- Clear interface definition
- Explicit Wave 4 markers
- Tests can verify structure
- Implementation can be completed later

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestVerifyWebhookSignature` | HMAC verification |
| `TestVerifyWebhookSignature_Invalid` | Invalid signature rejection |
| `TestMigrations` | Migration execution |
| `TestCheckMigrations` | Status checking |

**Running:**
```bash
make test ./internal/billing
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/jackc/pgx/v5` | PostgreSQL driver |
| `github.com/jackc/pgx/v5/pgxpool` | Connection pooling |

**File Locations:**
- [`internal/billing/webhook.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/billing/webhook.go) - Webhook handling
- [`internal/billing/quota.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/billing/quota.go) - Quota management
- [`internal/billing/migrations.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/billing/migrations.go) - Database migrations

## Related

- [LLM Proxy Quota](../llm-proxy/quota.md) - Quota checking integration
- [Config Plans](../../config/plans.md) - Plan definitions
