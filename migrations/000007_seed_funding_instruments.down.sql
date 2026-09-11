DELETE FROM venue_instruments WHERE venue_symbol IN ('BTCUSDT', 'ETHUSDT', 'BTC-USD', 'ETH-USD');
DELETE FROM instruments WHERE canonical_symbol IN ('BTCUSDT', 'ETHUSDT');
