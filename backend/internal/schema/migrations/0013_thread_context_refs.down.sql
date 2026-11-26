DROP INDEX IF EXISTS idx_thread_context_refs_message;
DROP TABLE IF EXISTS thread_context_refs;
ALTER TABLE thread_messages DROP COLUMN is_summary;
DROP INDEX IF EXISTS idx_thread_messages_agent_row;
