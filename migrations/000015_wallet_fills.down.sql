DELETE FROM venues WHERE code = 'hyperliquid'
  AND NOT EXISTS (SELECT 1 FROM wallet_fills WHERE venue_id = venues.id);

ALTER TABLE wallet_metric_snapshots DROP COLUMN IF EXISTS is_partial;

DROP TABLE IF EXISTS wallet_fills;
