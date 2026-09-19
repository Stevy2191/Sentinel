-- A calendar or custom period carries no day count, so the original
-- "time_range_days > 0" column check rejected every one of them.
--
-- The rule it encoded is not dropped, only narrowed: reports_period_fields_check
-- (migration 034) already requires time_range_days > 0 for rolling reports,
-- which is the only kind the value means anything for.
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_time_range_days_check;

ALTER TABLE reports
    ADD CONSTRAINT reports_time_range_days_check
    CHECK (time_range_days >= 0 AND time_range_days <= 365);
