-- Replaces the report template/section system with two fixed report types
-- (uptime, incident), and wipes existing saved reports so nothing carries a
-- stale template reference forward. Confirmed with the operator: a full wipe,
-- no migration path for existing report definitions, schedules, or history.
-- Generated PDF files already on disk for the wiped generations are removed
-- separately as a deploy step — a migration cannot touch the filesystem.

-- ---------------------------------------------------------------------------
-- Wipe existing report data. All five tables in one TRUNCATE so Postgres can
-- satisfy the foreign keys among them without CASCADE: report_jobs.report_id
-- references reports.id (019_report_jobs.sql), and report_generations,
-- report_access and report_schedules all reference reports.id directly.
-- ---------------------------------------------------------------------------
TRUNCATE TABLE report_jobs, report_generations, report_access, report_schedules, reports;

-- ---------------------------------------------------------------------------
-- Replace the template reference with a plain report type. The table is
-- empty from the truncate above, so adding a NOT NULL column needs no
-- backfill default.
-- ---------------------------------------------------------------------------
ALTER TABLE reports DROP COLUMN IF EXISTS template_id;
ALTER TABLE reports ADD COLUMN IF NOT EXISTS report_type VARCHAR(20) NOT NULL;

ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_report_type_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_report_type_check
    CHECK (report_type IN ('uptime', 'incident'));

-- report_generations never carried a template reference of its own (only
-- report_id, pdf_path, file_size, generated_by), so it needs no column change.

-- ---------------------------------------------------------------------------
-- Drop the template system. Safe now that reports.template_id (its only
-- referencing column) is gone.
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS report_templates;
