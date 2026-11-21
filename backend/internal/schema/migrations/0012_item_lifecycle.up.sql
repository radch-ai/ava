-- Durable conversation lifecycle: item bookkeeping, a per-agent ordered
-- event journal, and the identity coordinates replay depends on. The
-- message/thread tables stay the conversation's source of truth; this
-- schema records which admitted item produced which committed rows and
-- in what order, so a reconnecting device can be told what happened.
--
--   - sync_history is this database's lifecycle-history identity; a
--     recreated database (including :memory:) mints a different one, so
--     stale client cursors fail validation instead of replaying foreign
--     history.
--   - agent_sync_state carries each agent's journal head and replay
--     floor. Both survive pruning; a position is never reused.
--   - queue_items is bookkeeping from acceptance to one terminal
--     outcome — not an execution queue. Restart recovery settles
--     unresolved rows; it never runs them. idx_queue_items_unsettled
--     narrows that recovery to the rows it can act on instead of
--     scanning every terminal item.
--   - tool_invocations keys one executing call by a server-minted id;
--     the provider's call id is metadata, not a key.
--   - lifecycle_journal is the ordered feed. payload is the fully
--     hydrated wire event, so replay serves the committed bytes. outcome
--     is the terminal classification of an item_finished event, lifted
--     out of the payload so the snapshot's cancelled-notice read is an
--     index seek rather than a JSON scan.

CREATE TABLE IF NOT EXISTS sync_history (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    history_id TEXT NOT NULL
);
INSERT OR IGNORE INTO sync_history (id, history_id) VALUES (1, lower(hex(randomblob(16))));

CREATE TABLE IF NOT EXISTS agent_sync_state (
    agent_id  TEXT NOT NULL PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    head_pos  INTEGER NOT NULL DEFAULT 0,
    floor_pos INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS queue_items (
    id              TEXT NOT NULL,
    agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL CHECK (kind IN ('prompt', 'new_thread')),
    content         TEXT NOT NULL,
    model           TEXT,
    state           TEXT NOT NULL CHECK (state IN ('accepted', 'started', 'completed', 'failed', 'stopped', 'cancelled', 'interrupted')),
    -- thread_id names the execution thread once the item starts; NULL
    -- while it is only accepted. The composite FK keeps it inside this
    -- agent. No delete action is named: a thread is only ever removed
    -- with its agent, which cascades both rows in one statement, so the
    -- constraint holds at statement end.
    thread_id       TEXT,
    user_message_id TEXT,
    accepted_ms     INTEGER NOT NULL,
    started_ms      INTEGER,
    finished_ms     INTEGER,
    reason          TEXT,
    PRIMARY KEY (id),
    UNIQUE (agent_id, id),
    FOREIGN KEY (agent_id, thread_id) REFERENCES threads(agent_id, id)
);
CREATE INDEX IF NOT EXISTS idx_queue_items_unsettled ON queue_items (state, accepted_ms) WHERE state IN ('accepted', 'started');

CREATE TABLE IF NOT EXISTS tool_invocations (
    id         TEXT NOT NULL PRIMARY KEY,
    agent_id   TEXT NOT NULL,
    item_id    TEXT NOT NULL,
    call_id    TEXT NOT NULL,
    name       TEXT NOT NULL,
    args       TEXT NOT NULL,
    started_ms INTEGER NOT NULL,
    state      TEXT NOT NULL CHECK (state IN ('open', 'finished')),
    FOREIGN KEY (agent_id, item_id) REFERENCES queue_items(agent_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_tool_invocations_item ON tool_invocations (agent_id, item_id);

CREATE TABLE IF NOT EXISTS lifecycle_journal (
    agent_id   TEXT NOT NULL,
    pos        INTEGER NOT NULL,
    item_id    TEXT NOT NULL,
    kind       TEXT NOT NULL,
    batch_end  INTEGER NOT NULL DEFAULT 0,
    payload    TEXT NOT NULL,
    outcome    TEXT,
    created_ms INTEGER NOT NULL,
    PRIMARY KEY (agent_id, pos),
    FOREIGN KEY (agent_id, item_id) REFERENCES queue_items(agent_id, id) ON DELETE CASCADE
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_lifecycle_journal_item ON lifecycle_journal (agent_id, item_id);
CREATE INDEX IF NOT EXISTS idx_lifecycle_journal_cancelled ON lifecycle_journal (agent_id, pos) WHERE outcome = 'cancelled';
