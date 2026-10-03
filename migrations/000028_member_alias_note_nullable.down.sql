-- Rollback for 000028 (NULLs must be cleared first on downgrade).

UPDATE trader_group_members SET alias = '' WHERE alias IS NULL;
UPDATE trader_group_members SET note = '' WHERE note IS NULL;
ALTER TABLE trader_group_members
    ALTER COLUMN alias SET NOT NULL;
ALTER TABLE trader_group_members
    ALTER COLUMN note SET NOT NULL;
