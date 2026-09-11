-- Seed canonical instruments for funding arbitrage (BTC and ETH perps)
INSERT INTO instruments (canonical_symbol, base_asset, quote_asset, instrument_type, contract_type, price_tick, quantity_step, trading_enabled)
VALUES
  ('BTCUSDT', 'BTC', 'USDT', 'PERP', 'LINEAR', 0.01, 0.001, true),
  ('ETHUSDT', 'ETH', 'USDT', 'PERP', 'LINEAR', 0.01, 0.01, true)
ON CONFLICT (canonical_symbol) DO NOTHING;

-- Get IDs for the seeded instruments
DO $$
DECLARE
  btc_id UUID;
  eth_id UUID;
  binance_id UUID;
  extended_id UUID;
  variational_id UUID;
BEGIN
  SELECT id INTO btc_id FROM instruments WHERE canonical_symbol = 'BTCUSDT';
  SELECT id INTO eth_id FROM instruments WHERE canonical_symbol = 'ETHUSDT';
  SELECT id INTO binance_id FROM venues WHERE code = 'binance';
  SELECT id INTO extended_id FROM venues WHERE code = 'extended';
  SELECT id INTO variational_id FROM venues WHERE code = 'variational';

  -- Binance venue_instruments
  INSERT INTO venue_instruments (venue_id, instrument_id, venue_symbol, status)
  VALUES
    (binance_id, btc_id, 'BTCUSDT', 'ACTIVE'),
    (binance_id, eth_id, 'ETHUSDT', 'ACTIVE')
  ON CONFLICT DO NOTHING;

  -- Extended venue_instruments
  INSERT INTO venue_instruments (venue_id, instrument_id, venue_symbol, status)
  VALUES
    (extended_id, btc_id, 'BTC-USD', 'ACTIVE'),
    (extended_id, eth_id, 'ETH-USD', 'ACTIVE')
  ON CONFLICT DO NOTHING;

  -- Variational venue_instruments
  INSERT INTO venue_instruments (venue_id, instrument_id, venue_symbol, status)
  VALUES
    (variational_id, btc_id, 'BTCUSDT', 'ACTIVE'),
    (variational_id, eth_id, 'ETHUSDT', 'ACTIVE')
  ON CONFLICT DO NOTHING;
END $$;
