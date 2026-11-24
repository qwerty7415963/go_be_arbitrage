DROP INDEX IF EXISTS idx_group_alert_events_unread;
DROP INDEX IF EXISTS idx_group_alert_events_group;
DROP TABLE IF EXISTS group_alert_events;
DROP INDEX IF EXISTS idx_group_alerts_enabled;
DROP TABLE IF EXISTS group_alerts;
DROP INDEX IF EXISTS idx_group_membership_events_group;
DROP TABLE IF EXISTS group_membership_events;
DROP INDEX IF EXISTS idx_trader_groups_owner_fav;
ALTER TABLE trader_groups DROP COLUMN IF EXISTS is_favorite;
