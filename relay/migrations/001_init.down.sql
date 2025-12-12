-- 001_init.down.sql
-- Rollback initial database schema

DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS usage_daily;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS plans;
DROP TABLE IF EXISTS tenants;
