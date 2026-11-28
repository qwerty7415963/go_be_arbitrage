-- Security refresh family support (rotation + reuse detection)
ALTER TABLE refresh_tokens
  ADD COLUMN IF NOT EXISTS family_id UUID NOT NULL DEFAULT gen_random_uuid(),
  ADD COLUMN IF NOT EXISTS rotated_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS replaced_by_hash TEXT NULL;

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family ON refresh_tokens(family_id);
