-- Backfill canonical REST/WS endpoints for venues (keeps manually
-- customized rows untouched: only empty values are filled).
-- URLs mirror the adapter constants in internal/exchange.
-- Variational has no public WS (trading API not live) → ws_url stays empty.

UPDATE venues
SET rest_base_url = 'https://api.hyperliquid.xyz',
    ws_url = 'wss://api.hyperliquid.xyz/ws'
WHERE code = 'hyperliquid'
  AND (rest_base_url = '' OR ws_url = '');

UPDATE venues
SET rest_base_url = 'https://api.starknet.extended.exchange/api/v1',
    ws_url = 'wss://api.starknet.extended.exchange/stream.extended.exchange/v1'
WHERE code = 'extended'
  AND (rest_base_url = '' OR ws_url = '');

UPDATE venues
SET rest_base_url = 'https://fapi.binance.com',
    ws_url = 'wss://fstream.binance.com/ws/'
WHERE code = 'binance'
  AND (rest_base_url = '' OR ws_url = '');

UPDATE venues
SET rest_base_url = 'https://omni-client-api.prod.ap-northeast-1.variational.io'
WHERE code = 'variational'
  AND rest_base_url = '';
