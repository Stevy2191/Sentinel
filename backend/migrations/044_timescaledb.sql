-- 044_timescaledb.sql
-- TimescaleDB for network metrics (network monitoring roadmap, phase 0).
--
-- Nothing here touches an existing table. The metrics schema is created empty:
-- phase 2 adds its hypertables there. Keeping metrics out of public is what
-- lets backups dump public only (see BackupService.dumpArgs).
--
-- Idempotent on purpose: restoring a backup taken before this migration
-- rewrites schema_migrations without this row, so it runs again on the next
-- boot and must be harmless when everything already exists.
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE SCHEMA IF NOT EXISTS metrics;
