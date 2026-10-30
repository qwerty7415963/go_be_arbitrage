DROP INDEX IF EXISTS idx_telegram_outbox_pending;
DROP INDEX IF EXISTS uq_telegram_outbox_dedup;
DROP TABLE IF EXISTS telegram_outbox;
ALTER TABLE trader_groups DROP COLUMN IF EXISTS notify_enabled;
DROP INDEX IF EXISTS idx_telegram_link_codes_user;
DROP TABLE IF EXISTS telegram_link_codes;
DROP TABLE IF EXISTS telegram_bindings;
