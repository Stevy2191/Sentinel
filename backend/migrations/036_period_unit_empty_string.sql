-- A rolling report has no calendar unit, and Go writes a string column's zero
-- value as '' rather than NULL — which the unit check rejected, so no rolling
-- report could be saved once migration 034 landed.
--
-- '' is accepted as "not applicable", and the composite check is tightened in
-- exchange: a calendar report must name a real unit, where before it only had
-- to be non-null and '' would have slipped through.
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_period_unit_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_period_unit_check
    CHECK (period_unit IS NULL OR period_unit IN ('', 'week', 'month', 'quarter', 'year'));

ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_period_fields_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_period_fields_check
    CHECK (
        (period_kind = 'rolling'  AND time_range_days > 0)
     OR (period_kind = 'calendar' AND period_unit IN ('week', 'month', 'quarter', 'year')
                                  AND period_offset >= 0)
     OR (period_kind = 'custom'   AND period_start IS NOT NULL AND period_end IS NOT NULL
                                  AND period_start < period_end)
    );
