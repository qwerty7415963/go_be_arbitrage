-- Rollback initial schema

DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS config_versions;
DROP TABLE IF EXISTS strategy_instances;
DROP TABLE IF EXISTS strategy_types;
DROP TABLE IF EXISTS venue_instruments;
DROP TABLE IF EXISTS instruments;
DROP TABLE IF EXISTS venue_accounts;
DROP TABLE IF EXISTS venues;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
