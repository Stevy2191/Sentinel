-- 017_report_schedule_cron_entry.sql
-- Records the in-process cron entry a schedule is currently registered under,
-- for observability: it makes "is this schedule actually running?" answerable
-- from the database rather than only from the logs.
--
-- Important caveat, and why the scheduler clears this column on startup:
-- cron.EntryID is a counter local to one cron runner instance. It restarts at 1
-- on every process start, so a value persisted by a previous process is not
-- merely stale - after a restart the same number refers to a DIFFERENT
-- schedule's job. Treating a stored id as authoritative would remove the wrong
-- job. ReportSchedulerService.Start therefore nulls every row before
-- re-registering, so a non-null value always belongs to the running process.

ALTER TABLE report_schedules
    ADD COLUMN IF NOT EXISTS cron_entry_id INTEGER;

COMMENT ON COLUMN report_schedules.cron_entry_id IS
    'Cron entry id within the CURRENT process; NULL when not registered. Not stable across restarts.';

CREATE INDEX IF NOT EXISTS idx_report_schedules_cron_entry_id
    ON report_schedules (cron_entry_id);
