-- Market data tables for funding arbitrage

-- Funding rates history
CREATE TABLE IF NOT EXISTS funding_rates (
    id BIGSERIAL PRIMARY KEY,
    venue_id UUID NOT NULL REFERENCES venues(id),
    instrument_id UUID NOT NULL REFERENCES instruments(id),
    observed_at TIMESTAMPTZ NOT NULL,
    funding_rate TEXT NOT NULL,
    interval_seconds INTEGER NOT NULL,
    next_funding_at TIMESTAMPTZ,
    premium_rate TEXT,
    mark_price TEXT,
    index_price TEXT,
    source_event_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_funding_rates_venue_instrument_observed 
    ON funding_rates(venue_id, instrument_id, observed_at DESC);

CREATE INDEX idx_funding_rates_observed 
    ON funding_rates(observed_at DESC);

-- Market trades
CREATE TABLE IF NOT EXISTS market_trades (
    id BIGSERIAL PRIMARY KEY,
    venue_id UUID NOT NULL REFERENCES venues(id),
    instrument_id UUID NOT NULL REFERENCES instruments(id),
    exchange_trade_id TEXT NOT NULL,
    exchange_timestamp TIMESTAMPTZ,
    receive_timestamp TIMESTAMPTZ NOT NULL,
    price TEXT NOT NULL,
    quantity TEXT NOT NULL,
    side TEXT NOT NULL,
    sequence_no BIGINT,
    raw_event_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_market_trades_venue_instrument_time 
    ON market_trades(venue_id, instrument_id, receive_timestamp DESC);

-- Market tickers
CREATE TABLE IF NOT EXISTS market_tickers (
    id BIGSERIAL PRIMARY KEY,
    venue_id UUID NOT NULL REFERENCES venues(id),
    instrument_id UUID NOT NULL REFERENCES instruments(id),
    exchange_timestamp TIMESTAMPTZ,
    receive_timestamp TIMESTAMPTZ NOT NULL,
    best_bid_price TEXT,
    best_bid_qty TEXT,
    best_ask_price TEXT,
    best_ask_qty TEXT,
    mark_price TEXT,
    index_price TEXT,
    sequence_no BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_market_tickers_venue_instrument_time 
    ON market_tickers(venue_id, instrument_id, receive_timestamp DESC);

-- Raw market events
CREATE TABLE IF NOT EXISTS raw_market_events (
    id BIGSERIAL PRIMARY KEY,
    venue_id UUID NOT NULL REFERENCES venues(id),
    venue_instrument_id UUID,
    event_type TEXT NOT NULL,
    exchange_timestamp TIMESTAMPTZ,
    receive_timestamp TIMESTAMPTZ NOT NULL,
    process_timestamp TIMESTAMPTZ,
    exchange_sequence BIGINT,
    connection_id UUID NOT NULL,
    payload JSONB NOT NULL,
    payload_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_raw_market_events_venue_type_time 
    ON raw_market_events(venue_id, event_type, receive_timestamp DESC);
