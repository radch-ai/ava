-- Agents: identity (name, icon) and the five prompt-text fields,
-- one typed column each; user_context is the superagent's, NULL on
-- regular agents. Heartbeat and cron jobs live in their own
-- agent-keyed tables, tool opt-outs in agent_disabled_tools,
-- user-installed MCP servers in mcp_servers -- parent of the
-- catalog's kind='mcp' rows.
--   - ids derive from display names (lowercase-hyphen); "_"-prefixed
--     ids are system-reserved and underivable from names
--   - timestamps are BIGINT micros since epoch, stamped by the
--     database on insert
--   - active_thread_id names the thread the sink resolves when
--     writing and replay resolves when reading for the next
--     executing prompt; the deferred FK lets agent + initial thread
--     land in one transaction, agent first
CREATE TABLE IF NOT EXISTS agents (
    id                     TEXT NOT NULL PRIMARY KEY,
    name                   TEXT NOT NULL,
    icon                   TEXT NOT NULL,
    soul_personality       TEXT NOT NULL,
    soul_identity          TEXT NOT NULL,
    user_context           TEXT NULL,
    operating_instructions TEXT NOT NULL,
    tools_context          TEXT NOT NULL,
    created_at             BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000000 AS INTEGER)),
    active_thread_id       TEXT NOT NULL,
    FOREIGN KEY (id, active_thread_id) REFERENCES threads(agent_id, id)
        DEFERRABLE INITIALLY DEFERRED
);

-- One row per conversation boundary; the agent's active pointer
-- names one of these. created_at is DB-stamped epoch millis -- the
-- conversation-record convention (0009/0010), not agents' micros.
-- threads.agent_id is immediate: unknown agents fail at the thread
-- insert. The composite unique key is the FK parent for the pointer,
-- for thread_messages, and for the predecessor link, so a message or
-- a predecessor can never name another agent's thread.
--   - previous_thread_id names the thread this one replaced: the
--     display chain that turns /new into a divider. NULL on the
--     agent's initial thread.
--   - agent deletion still cascades in one statement: the cascade
--     follows the child FKs (agent -> threads, agent -> messages),
--     so parent and predecessor vanish together and the self-FK's
--     no-action default is never evaluated against a surviving row.
CREATE TABLE IF NOT EXISTS threads (
    id                  TEXT NOT NULL,
    agent_id            TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    created_at          BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000 AS INTEGER)),
    previous_thread_id  TEXT,
    -- Which of the two chain-growth ways this boundary is: a plain
    -- /new or a compaction successor, so a divider can label itself
    -- honestly instead of every rotation claiming "New session".
    kind              TEXT NOT NULL DEFAULT 'new'
        CHECK (kind IN ('new', 'compact')),
    PRIMARY KEY (id),
    UNIQUE (agent_id, id),
    FOREIGN KEY (agent_id, previous_thread_id) REFERENCES threads(agent_id, id)
);

-- User-installed MCP servers: the parent entity of kind='mcp' catalog
-- rows. Ids are slugs minted from the name at creation (agent
-- convention) and stay immutable while the name is editable. Secrets
-- never touch this table: header VALUES live sealed in the
-- runtime vault under mcp-server/{slug}; only key NAMES are recorded,
-- so the wire can render them redacted. A disabled server's tools
-- resolve to nothing at turn time (ResolveAgentToolsDetailed joins
-- this switch) -- same inertness as the global tool switch.
CREATE TABLE IF NOT EXISTS mcp_servers (
    id           TEXT NOT NULL PRIMARY KEY,
    name         TEXT NOT NULL,
    -- Streamable HTTP endpoint (http(s), validated in the API).
    url          TEXT NULL,
    -- JSON array of secret key names; values sealed in the vault.
    header_keys  TEXT NOT NULL DEFAULT '[]',
    -- Per-call deadline; NULL = global default.
    timeout_ms   INTEGER NULL,
    -- Server-level kill switch.
    enabled      INTEGER NOT NULL DEFAULT 1,
    -- Catalog admission state: 'paused' after a
    -- security-rejected discovery generation -- prior metadata stays
    -- readable, new calls are denied until a valid refresh succeeds.
    catalog_state TEXT NOT NULL DEFAULT 'active'
        CHECK (catalog_state IN ('active', 'paused')),
    -- Destination authority: administrator-approved
    -- exceptions for one server's canonical endpoint. Both default
    -- deny; a destination change resets them.
    allow_private_network INTEGER NOT NULL DEFAULT 0,
    allow_plaintext       INTEGER NOT NULL DEFAULT 0,
    -- Discovery diagnostics, so inert tools are explainable.
    last_error   TEXT NULL,
    created_at   BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000000 AS INTEGER))
);

-- The installed-tool catalog with its global switch. Built-ins ship
-- with the app and default ON; entries are synced from the gateway's
-- registry at boot, which never overwrites a saved switch.
CREATE TABLE IF NOT EXISTS tools (
    id          TEXT NOT NULL PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- Global availability: the outer permission gate.
    enabled     INTEGER NOT NULL DEFAULT 1,
    -- Provenance: shipped built-in vs MCP-discovered; a tool dies
    -- with its owning server (delete cascades rows and opt-outs).
    kind      TEXT NOT NULL DEFAULT 'builtin',
    server_id TEXT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    -- The advertised definition exactly as discovered (bounded,
    -- sanitized): the UI's definition source. Never executed.
    definition_json   TEXT NOT NULL DEFAULT '',
    created_at  BIGINT NOT NULL
        DEFAULT (CAST((julianday('now') - 2440587.5) * 86400000000 AS INTEGER))
);

-- Server-owned rows by parent.
CREATE INDEX IF NOT EXISTS idx_tools_server_id ON tools (server_id);

-- row presence = this agent explicitly disabled this tool; absence = inherit
CREATE TABLE IF NOT EXISTS agent_disabled_tools (
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    tool_id  TEXT NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, tool_id)
);

-- Per-agent MCP server gate: the parent switch over a server's tools.
-- absent = off; enabled = agent uses this server's globally enabled tools.
CREATE TABLE IF NOT EXISTS agent_mcp_preferences (
    agent_id  TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    server_id TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    enabled   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (agent_id, server_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_mcp_preferences_server
    ON agent_mcp_preferences (server_id);

-- One definition of the effective-tool predicate: (tool x agent) rows
-- with the folded gate/opt-out columns. Both consumers bind the agent
-- in the outer WHERE; SQLite inlines views into the caller's plan.
CREATE VIEW IF NOT EXISTS agent_effective_tools AS
SELECT t.id, t.server_id,
       t.enabled AS tool_enabled,
       CAST(COALESCE(s.enabled, 1) AS INTEGER) AS server_enabled,
       CAST(COALESCE(s.catalog_state, 'active') AS TEXT) AS catalog_state,
       CAST(CASE WHEN t.server_id IS NULL THEN 1 ELSE COALESCE(ap.enabled, 0) END AS INTEGER) AS gate_on,
       EXISTS(SELECT 1 FROM agent_disabled_tools d
              WHERE d.agent_id = a.id AND d.tool_id = t.id) AS locally_disabled,
       a.id AS agent_id
FROM tools t
CROSS JOIN agents a
LEFT JOIN mcp_servers s ON s.id = t.server_id
LEFT JOIN agent_mcp_preferences ap ON ap.server_id = t.server_id AND ap.agent_id = a.id;

-- Durable, payload-free security audit. Columns are validated
-- identifiers, timestamps, decisions, static reason codes
-- and bounded counts -- there is deliberately no payload column, so
-- arguments, results, credentials, URLs and schemas cannot be stored
-- here even by mistake. 'admitted' rows are the pre-dispatch record
-- (protected until terminal); 'terminal' rows are outcomes and
-- point-in-time decisions.
CREATE TABLE IF NOT EXISTS tool_audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at BIGINT NOT NULL,
    event TEXT NOT NULL CHECK (event IN (
        'admission', 'outcome', 'approval_change',
        'plaintext_exception', 'denial', 'redaction')),
    agent_id   TEXT,
    server_id  TEXT,
    tool_id    TEXT,
    tool_call_id TEXT,
    decision    TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    count       INTEGER,
    state TEXT NOT NULL CHECK (state IN ('admitted', 'terminal'))
);

CREATE INDEX IF NOT EXISTS idx_tool_audit_occurred ON tool_audit_events (occurred_at);
CREATE INDEX IF NOT EXISTS idx_tool_audit_agent ON tool_audit_events (agent_id, occurred_at);
