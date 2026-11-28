ALTER TABLE refresh_tokens
  DROP COLUMN IF EXISTS family_id,
  DROP COLUMN IF EXISTS rotated_at,
  DROP COLUMN IF EXISTS replaced_by_hash;

DROP INDEX IF EXISTS idx_refresh_tokens_family;
