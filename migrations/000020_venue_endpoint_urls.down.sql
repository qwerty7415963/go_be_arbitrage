-- Revert only rows still carrying exactly the values set by the up
-- migration (never touches manually customized rows).
UPDATE venues SET rest_base_url = '', ws_url = ''
WHERE code = 'hyperliquid'
  AND rest_base_url = 'https://api.hyperliquid.xyz'
  AND ws_url = 'wss://api.hyperliquid.xyz/ws';

UPDATE venues SET rest_base_url = '', ws_url = ''
WHERE code = 'extended'
  AND rest_base_url = 'https://api.starknet.extended.exchange/api/v1'
  AND ws_url = 'wss://api.starknet.extended.exchange/stream.extended.exchange/v1';

UPDATE venues SET rest_base_url = '', ws_url = ''
WHERE code = 'binance'
  AND rest_base_url = 'https://fapi.binance.com'
  AND ws_url = 'wss://fstream.binance.com/ws/';

UPDATE venues SET rest_base_url = ''
WHERE code = 'variational'
  AND rest_base_url = 'https://omni-client-api.prod.ap-northeast-1.variational.io';
