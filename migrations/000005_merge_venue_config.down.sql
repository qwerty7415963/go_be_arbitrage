-- Restore exchange_configs table
CREATE TABLE IF NOT EXISTS exchange_configs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  venue_id        UUID NOT NULL REFERENCES venues(id) UNIQUE,
  exchange_name   TEXT NOT NULL,
  rest_base_url   TEXT NOT NULL,
  ws_url          TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'ACTIVE'
                  CHECK (status IN ('ACTIVE', 'DISABLED')),
  rate_limit_rpm  INT NOT NULL DEFAULT 1200,
  timeout_ms      INT NOT NULL DEFAULT 5000,
  created_by      UUID REFERENCES users(id),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Copy data back
INSERT INTO exchange_configs (venue_id, exchange_name, rest_base_url, ws_url, rate_limit_rpm, timeout_ms)
SELECT id, exchange_name, rest_base_url, ws_url, rate_limit_rpm, timeout_ms
FROM venues
WHERE exchange_name != '' AND rest_base_url != '';

-- Remove columns from venues
ALTER TABLE venues DROP COLUMN IF EXISTS exchange_name;
ALTER TABLE venues DROP COLUMN IF EXISTS rest_base_url;
ALTER TABLE venues DROP COLUMN IF EXISTS ws_url;
ALTER TABLE venues DROP COLUMN IF EXISTS rate_limit_rpm;
ALTER TABLE venues DROP COLUMN IF EXISTS timeout_ms;
