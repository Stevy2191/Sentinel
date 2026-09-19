-- Calendar periods for reports, alongside the existing rolling windows.
--
-- A report could only say "the last N days". That is the wrong shape for the
-- reports people actually ask for: "September" is a calendar month, not the 30
-- days ending whenever the report happened to run. Two consecutive monthly
-- reports built from rolling windows overlap and neither matches a month, which
-- makes them useless for comparing one period against another.
--
-- period_kind decides how the window is resolved:
--   rolling   - time_range_days back from now, the existing behaviour
--   calendar  - the period_unit containing now, shifted back by period_offset
--   custom    - the explicit period_start .. period_end range
ALTER TABLE reports
    ADD COLUMN IF NOT EXISTS period_kind VARCHAR(16) NOT NULL DEFAULT 'rolling';

-- Which calendar unit, when period_kind = 'calendar'.
ALTER TABLE reports
    ADD COLUMN IF NOT EXISTS period_unit VARCHAR(16);

-- How many units back: 0 is the one in progress ("this month"), 1 the one
-- before it ("last month"). A scheduled monthly report wants 1, so the report
-- that lands on the 1st covers the month that just finished rather than a month
-- that is one day old.
ALTER TABLE reports
    ADD COLUMN IF NOT EXISTS period_offset INTEGER NOT NULL DEFAULT 0;

-- Explicit bounds for period_kind = 'custom'.
ALTER TABLE reports
    ADD COLUMN IF NOT EXISTS period_start TIMESTAMP WITH TIME ZONE;
ALTER TABLE reports
    ADD COLUMN IF NOT EXISTS period_end TIMESTAMP WITH TIME ZONE;

ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_period_kind_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_period_kind_check
    CHECK (period_kind IN ('rolling', 'calendar', 'custom'));

ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_period_unit_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_period_unit_check
    CHECK (period_unit IS NULL OR period_unit IN ('week', 'month', 'quarter', 'year'));

-- Each kind needs its own fields present, so a half-configured period cannot be
-- stored and then silently resolve to something arbitrary at render time.
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_period_fields_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_period_fields_check
    CHECK (
        (period_kind = 'rolling'  AND time_range_days > 0)
     OR (period_kind = 'calendar' AND period_unit IS NOT NULL AND period_offset >= 0)
     OR (period_kind = 'custom'   AND period_start IS NOT NULL AND period_end IS NOT NULL
                                  AND period_start < period_end)
    );

ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_period_offset_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_period_offset_check
    CHECK (period_offset BETWEEN 0 AND 24);
