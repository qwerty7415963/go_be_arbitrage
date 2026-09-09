-- Merge exchange_configs into venues

-- Add columns from exchange_configs to venues
ALTER TABLE venues ADD COLUMN IF NOT EXISTS exchange_name TEXT NOT NULL DEFAULT '';
ALTER TABLE venues ADD COLUMN IF NOT EXISTS rest_base_url TEXT NOT NULL DEFAULT '';
ALTER TABLE venues ADD COLUMN IF NOT EXISTS ws_url TEXT NOT NULL DEFAULT '';
ALTER TABLE venues ADD COLUMN IF NOT EXISTS rate_limit_rpm INT NOT NULL DEFAULT 1200;
ALTER TABLE venues ADD COLUMN IF NOT EXISTS timeout_ms INT NOT NULL DEFAULT 5000;

-- Copy data from exchange_configs to venues (if any)
UPDATE venues v
SET
  exchange_name = ec.exchange_name,
  rest_base_url = ec.rest_base_url,
  ws_url = ec.ws_url,
  rate_limit_rpm = ec.rate_limit_rpm,
  timeout_ms = ec.timeout_ms
FROM exchange_configs ec
WHERE v.id = ec.venue_id;

-- Drop exchange_configs table
DROP TABLE IF EXISTS exchange_configs;
