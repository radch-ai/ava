-- System agents ("_"-prefixed ids) are protected twice over:
--   - undeletable: no code path may remove them
--   - identity-frozen: id, name, and icon match the clients'
--     constant, so only the config columns stay editable
-- Landing after the seed keeps every down file the exact inverse of
-- its up: the guards roll back before the seed does, so nothing has
-- to be lifted to unseed the row.

CREATE TRIGGER IF NOT EXISTS agents_no_delete_system
BEFORE DELETE ON agents
FOR EACH ROW
WHEN substr(OLD.id, 1, 1) = '_'
BEGIN
    SELECT RAISE(ABORT, 'system agents cannot be deleted');
END;

CREATE TRIGGER IF NOT EXISTS agents_no_update_system_identity
BEFORE UPDATE ON agents
FOR EACH ROW
WHEN substr(OLD.id, 1, 1) = '_'
     AND (NEW.id != OLD.id
          OR NEW.name != OLD.name
          OR NEW.icon != OLD.icon)
BEGIN
    SELECT RAISE(ABORT, 'system agents cannot change id, name, or icon');
END;
