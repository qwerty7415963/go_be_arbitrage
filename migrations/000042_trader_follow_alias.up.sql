-- User-level wallet nickname attached to a follow. Group aliases remain
-- contextual overrides inside a group.
ALTER TABLE trader_follows
    ADD COLUMN IF NOT EXISTS alias TEXT NULL;