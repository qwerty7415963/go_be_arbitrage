-- Wire Hyperliquid into the funding-arbitrage pipeline: link its native
-- coin symbols to the canonical BTC/ETH instruments so HL shares
-- instrument_ids with the other venues (pair matching key).
-- Quote side follows the existing USDT-canonical convention (same as the
-- Binance/extended seeds); funding settlement currency is irrelevant to
-- the funding-rate comparison.

DO $$
DECLARE
  btc_id UUID;
  eth_id UUID;
  hyperliquid_id UUID;
BEGIN
  SELECT id INTO btc_id FROM instruments WHERE canonical_symbol = 'BTCUSDT';
  SELECT id INTO eth_id FROM instruments WHERE canonical_symbol = 'ETHUSDT';
  SELECT id INTO hyperliquid_id FROM venues WHERE code = 'hyperliquid';

  IF hyperliquid_id IS NOT NULL AND btc_id IS NOT NULL AND eth_id IS NOT NULL THEN
    INSERT INTO venue_instruments (venue_id, instrument_id, venue_symbol, status)
    VALUES
      (hyperliquid_id, btc_id, 'BTC', 'ACTIVE'),
      (hyperliquid_id, eth_id, 'ETH', 'ACTIVE')
    ON CONFLICT DO NOTHING;
  END IF;
END $$;
