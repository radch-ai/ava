# Agent Config Storage: S3 to Postgres Migration

**Date:** 2026-02-27

## Overview

Move agent configuration from S3 to Postgres for transactional integrity and easier schema management. Transcripts remain in S3.

## Current State

| Storage | Contents |
|---------|----------|
| S3 (`{user_id}/{agent_id}/workspace/`) | SOUL.md, AGENTS.md, USER.md, IDENTITY.md, TOOLS.md |
| Postgres | users, usage_logs, processed_webhooks |

## Target State

| Storage | Contents |
|---------|----------|
| **Postgres** | Agent config + all markdown files |
| **S3** | Session transcripts only (`{user_id}/{agent_id}/transcripts/`) |

## Schema

```sql
CREATE TABLE IF NOT EXISTS agents (
    agent_id     TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT,
    
    -- markdown files
    soul_md      TEXT,
    agents_md    TEXT,
    user_md      TEXT,
    identity_md  TEXT,
    tools_md     TEXT,
    heartbeat_md TEXT,
    
    -- structured config
    config       JSONB,
    
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_agents_user_id ON agents(user_id);
CREATE INDEX IF NOT EXISTS idx_agents_updated_at ON agents(updated_at);
```

### config JSONB Structure

```json
{
  "tool_whitelist": ["read_file", "write_file"],
  "tool_env_vars": "<encrypted_blob>",
  "custom_settings": {}
}
```

- `tool_whitelist`: Array of allowed tool names for this agent
- `tool_env_vars`: Encrypted blob containing sensitive environment variables
- `custom_settings`: Agent-specific configuration

## Migration Path

1. Add `agents` table migration to `internal/billing/migrations.go`
2. Create `AgentStore` interface in `internal/storage/agent_store.go`
3. Implement Postgres-backed `AgentStore`
4. Update `WorkspaceLoader` to read from `AgentStore`
5. Keep S3 `Client` for transcripts only
6. Optional: one-time script to migrate existing S3 workspace files to Postgres

## Files Changed

| File | Change |
|------|--------|
| `internal/billing/migrations.go` | Add agents table migration |
| `internal/storage/agent_store.go` | New: AgentStore interface + Postgres impl |
| `internal/storage/workspace.go` | Update to use AgentStore |
| `internal/storage/s3.go` | Remove WorkspacePath (keep TranscriptPath) |

## Decisions

1. **Single agents table** - All config in one row per agent, not split into multiple tables
2. **JSONB for config** - Flexible for tool_whitelist, encrypted env vars, and future settings
3. **Transcripts stay in S3** - Large append-only data, not queried transactionally
4. **Encrypted tool_env_vars** - Stored in JSONB, encrypted at application layer
