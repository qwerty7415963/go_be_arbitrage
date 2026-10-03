-- Allow clearing alias/note back to NULL (BE-2 member PATCH semantics:
-- present-but-empty clears; absent keeps). Existing '' values stay valid.

ALTER TABLE trader_group_members
    ALTER COLUMN alias DROP NOT NULL;
ALTER TABLE trader_group_members
    ALTER COLUMN note DROP NOT NULL;
