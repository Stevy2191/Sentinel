-- Migration 002: Convert TIMESTAMP to TIMESTAMPTZ
-- Date: 2026-07-18
-- Purpose: Fix timezone handling for time-based calculations
--
-- Background: migration 001 declared all time columns as TIMESTAMP (without time
-- zone). With that type, a naive timestamp's meaning depends on the session's
-- timezone, so duration and downtime math becomes wrong when the app process or
-- database runs outside UTC. Converting to TIMESTAMPTZ stores an absolute
-- instant, making all time-based reporting timezone-independent.
--
-- Each conversion uses `USING <column> AT TIME ZONE 'UTC'` so that existing
-- naive values are interpreted as UTC (Sentinel writes timestamps in UTC),
-- rather than the session timezone. This keeps the conversion deterministic and
-- preserves the intended instants.
--
-- Wrapped in a transaction so the schema change is applied atomically.

BEGIN;

-- monitors
ALTER TABLE monitors ALTER COLUMN last_check_at TYPE TIMESTAMPTZ USING last_check_at AT TIME ZONE 'UTC';
ALTER TABLE monitors ALTER COLUMN created_at    TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';
ALTER TABLE monitors ALTER COLUMN updated_at    TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC';

-- checks
ALTER TABLE checks ALTER COLUMN timestamp TYPE TIMESTAMPTZ USING timestamp AT TIME ZONE 'UTC';

-- incidents
ALTER TABLE incidents ALTER COLUMN start_time TYPE TIMESTAMPTZ USING start_time AT TIME ZONE 'UTC';
ALTER TABLE incidents ALTER COLUMN end_time   TYPE TIMESTAMPTZ USING end_time AT TIME ZONE 'UTC';
ALTER TABLE incidents ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';
ALTER TABLE incidents ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC';

-- notifications
ALTER TABLE notifications ALTER COLUMN sent_at    TYPE TIMESTAMPTZ USING sent_at AT TIME ZONE 'UTC';
ALTER TABLE notifications ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';

-- status_pages
ALTER TABLE status_pages ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';
ALTER TABLE status_pages ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC';

-- status_page_monitors
ALTER TABLE status_page_monitors ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';

-- api_tokens
ALTER TABLE api_tokens ALTER COLUMN last_used_at TYPE TIMESTAMPTZ USING last_used_at AT TIME ZONE 'UTC';
ALTER TABLE api_tokens ALTER COLUMN expires_at   TYPE TIMESTAMPTZ USING expires_at AT TIME ZONE 'UTC';
ALTER TABLE api_tokens ALTER COLUMN created_at   TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';

COMMIT;

-- All TIMESTAMP columns converted to TIMESTAMPTZ for consistent UTC handling
-- Existing data is preserved; only column type changes
-- Ensures time-based reporting is timezone-independent

-- ---------------------------------------------------------------------------
-- Rollback (if needed): reverts each column back to TIMESTAMP, normalizing the
-- stored instant to a naive UTC value. Provided for reference; not executed.
-- ---------------------------------------------------------------------------
-- BEGIN;
-- ALTER TABLE monitors ALTER COLUMN last_check_at TYPE TIMESTAMP USING last_check_at AT TIME ZONE 'UTC';
-- ALTER TABLE monitors ALTER COLUMN created_at    TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC';
-- ALTER TABLE monitors ALTER COLUMN updated_at    TYPE TIMESTAMP USING updated_at AT TIME ZONE 'UTC';
-- ALTER TABLE checks ALTER COLUMN timestamp TYPE TIMESTAMP USING timestamp AT TIME ZONE 'UTC';
-- ALTER TABLE incidents ALTER COLUMN start_time TYPE TIMESTAMP USING start_time AT TIME ZONE 'UTC';
-- ALTER TABLE incidents ALTER COLUMN end_time   TYPE TIMESTAMP USING end_time AT TIME ZONE 'UTC';
-- ALTER TABLE incidents ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC';
-- ALTER TABLE incidents ALTER COLUMN updated_at TYPE TIMESTAMP USING updated_at AT TIME ZONE 'UTC';
-- ALTER TABLE notifications ALTER COLUMN sent_at    TYPE TIMESTAMP USING sent_at AT TIME ZONE 'UTC';
-- ALTER TABLE notifications ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC';
-- ALTER TABLE status_pages ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC';
-- ALTER TABLE status_pages ALTER COLUMN updated_at TYPE TIMESTAMP USING updated_at AT TIME ZONE 'UTC';
-- ALTER TABLE status_page_monitors ALTER COLUMN created_at TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC';
-- ALTER TABLE api_tokens ALTER COLUMN last_used_at TYPE TIMESTAMP USING last_used_at AT TIME ZONE 'UTC';
-- ALTER TABLE api_tokens ALTER COLUMN expires_at   TYPE TIMESTAMP USING expires_at AT TIME ZONE 'UTC';
-- ALTER TABLE api_tokens ALTER COLUMN created_at   TYPE TIMESTAMP USING created_at AT TIME ZONE 'UTC';
-- COMMIT;
