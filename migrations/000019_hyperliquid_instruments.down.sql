DELETE FROM venue_instruments vi
USING venues v
WHERE vi.venue_id = v.id
  AND v.code = 'hyperliquid'
  AND vi.venue_symbol IN ('BTC', 'ETH');
