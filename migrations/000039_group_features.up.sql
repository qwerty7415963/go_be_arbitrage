ALTER TABLE trader_groups
    ADD COLUMN IF NOT EXISTS is_favorite BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_trader_groups_owner_fav
    ON trader_groups (user_id, is_favorite DESC, name ASC);

CREATE TABLE IF NOT EXISTS group_membership_events (
    id BIGSERIAL PRIMARY KEY,
    group_id UUID NOT NULL REFERENCES trader_groups(id) ON DELETE CASCADE,
    venue_id UUID NOT NULL REFERENCES venues(id),
    wallet_address TEXT NOT NULL CHECK (wallet_address = lower(wallet_address)),
    event_type TEXT NOT NULL CHECK (event_type IN ('added','removed')),
    actor_user_id UUID NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_group_membership_events_group
    ON group_membership_events (group_id, created_at DESC);

CREATE TABLE IF NOT EXISTS group_alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES trader_groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    type TEXT NOT NULL CHECK (type IN (
        'unrealized_pnl_above','unrealized_pnl_below','realized_pnl_reaches',
        'roi_reaches','position_opened','position_closed','position_increased',
        'position_reduced','exposure_above','long_short_shift',
        'wallet_inactive','wallet_stale','wallet_added','wallet_removed')),
    params JSONB NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    cooldown_seconds INT NOT NULL DEFAULT 300 CHECK (cooldown_seconds BETWEEN 0 AND 86400),
    notify_in_app BOOLEAN NOT NULL DEFAULT TRUE,
    notify_telegram BOOLEAN NOT NULL DEFAULT FALSE,
    last_condition_met BOOLEAN NOT NULL DEFAULT FALSE,
    last_value NUMERIC(30,12) NULL,
    last_triggered_at TIMESTAMPTZ NULL,
    last_evaluated_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (group_id, name)
);
CREATE INDEX IF NOT EXISTS idx_group_alerts_enabled ON group_alerts (enabled) WHERE enabled;

CREATE TABLE IF NOT EXISTS group_alert_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id UUID NOT NULL REFERENCES group_alerts(id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES trader_groups(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    dedup_key TEXT NOT NULL,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at TIMESTAMPTZ NULL,
    UNIQUE (dedup_key)
);
CREATE INDEX IF NOT EXISTS idx_group_alert_events_group
    ON group_alert_events (group_id, triggered_at DESC);
CREATE INDEX IF NOT EXISTS idx_group_alert_events_unread
    ON group_alert_events (group_id, triggered_at DESC) WHERE read_at IS NULL;
