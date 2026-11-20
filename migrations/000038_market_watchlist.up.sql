CREATE TABLE IF NOT EXISTS watched_markets (
    venue_id    UUID NOT NULL REFERENCES venues(id) ON DELETE CASCADE,
    market      TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue_id, market)
);

CREATE INDEX IF NOT EXISTS idx_watched_markets_venue
    ON watched_markets (venue_id, enabled, sort_order);
