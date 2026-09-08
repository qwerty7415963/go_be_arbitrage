-- Add role to users table
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'
  CHECK (role IN ('admin', 'user'));

-- Exchange configs table
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

CREATE INDEX IF NOT EXISTS idx_exchange_configs_venue ON exchange_configs(venue_id);
CREATE INDEX IF NOT EXISTS idx_exchange_configs_status ON exchange_configs(status);
