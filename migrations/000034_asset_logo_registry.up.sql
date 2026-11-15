CREATE TABLE IF NOT EXISTS assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    canonical_key TEXT NOT NULL UNIQUE,
    symbol TEXT NOT NULL,
    name TEXT,
    asset_class TEXT NOT NULL CHECK (asset_class IN ('crypto_native','crypto_token','index','commodity','equity','unknown')),
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS asset_aliases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    venue_id UUID REFERENCES venues(id) ON DELETE CASCADE,
    venue_symbol TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE NULLS NOT DISTINCT (venue_id, venue_symbol)
);

CREATE INDEX IF NOT EXISTS idx_asset_aliases_asset_id ON asset_aliases(asset_id);
CREATE INDEX IF NOT EXISTS idx_asset_aliases_venue ON asset_aliases(venue_id, venue_symbol);

CREATE TABLE IF NOT EXISTS asset_provider_refs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    ref JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (asset_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_asset_provider_refs_asset_id ON asset_provider_refs(asset_id);

CREATE TABLE IF NOT EXISTS asset_logos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'missing' CHECK (status IN ('found','missing','error','pending')),
    url TEXT,
    storage_path TEXT,
    content_type TEXT,
    etag TEXT,
    size_bytes BIGINT,
    fetched_at TIMESTAMPTZ,
    retry_after TIMESTAMPTZ,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (asset_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_asset_logos_status ON asset_logos(status);
CREATE INDEX IF NOT EXISTS idx_assets_class ON assets(asset_class);
