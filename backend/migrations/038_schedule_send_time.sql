-- When a scheduled report is actually sent.
--
-- The cadence was fixed: daily, weekly or monthly, always at 08:00 in whatever
-- zone the server process happened to run in — UTC in a container, so an
-- operator in Chicago got their "morning" report at 03:00. The hour is now
-- chosen, and interpreted in the instance's report timezone.
ALTER TABLE report_schedules
    ADD COLUMN IF NOT EXISTS send_hour INTEGER NOT NULL DEFAULT 8;
ALTER TABLE report_schedules
    ADD COLUMN IF NOT EXISTS send_minute INTEGER NOT NULL DEFAULT 0;

ALTER TABLE report_schedules DROP CONSTRAINT IF EXISTS report_schedules_send_time_check;
ALTER TABLE report_schedules
    ADD CONSTRAINT report_schedules_send_time_check
    CHECK (send_hour BETWEEN 0 AND 23 AND send_minute BETWEEN 0 AND 59);

-- Which day, for the cadences where there is a choice. NULL keeps the previous
-- behaviour: Monday for weekly, the 1st for monthly and quarterly.
ALTER TABLE report_schedules
    ADD COLUMN IF NOT EXISTS day_of_week INTEGER;
ALTER TABLE report_schedules
    ADD COLUMN IF NOT EXISTS day_of_month INTEGER;

ALTER TABLE report_schedules DROP CONSTRAINT IF EXISTS report_schedules_day_of_week_check;
ALTER TABLE report_schedules
    ADD CONSTRAINT report_schedules_day_of_week_check
    CHECK (day_of_week IS NULL OR day_of_week BETWEEN 0 AND 6);

-- Capped at 28 deliberately. A schedule set to the 31st would not fire in
-- February at all, and silently skipping a month is worse than not offering
-- the day.
ALTER TABLE report_schedules DROP CONSTRAINT IF EXISTS report_schedules_day_of_month_check;
ALTER TABLE report_schedules
    ADD CONSTRAINT report_schedules_day_of_month_check
    CHECK (day_of_month IS NULL OR day_of_month BETWEEN 1 AND 28);

-- Quarterly, so a report covering last quarter can be delivered when that
-- quarter ends rather than needing a hand-written cron expression.
ALTER TABLE report_schedules DROP CONSTRAINT IF EXISTS report_schedules_schedule_type_check;
ALTER TABLE report_schedules
    ADD CONSTRAINT report_schedules_schedule_type_check
    CHECK (schedule_type IN ('daily', 'weekly', 'monthly', 'quarterly', 'custom'));
