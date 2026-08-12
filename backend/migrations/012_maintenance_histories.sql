-- 012_maintenance_histories.sql
-- Records every maintenance window a monitor has had, not just the one
-- currently configured on the monitors row. Without this, a window that has
-- been cleared or overwritten leaves no trace, so reporting cannot say a
-- service was under maintenance during some past hour — it can only describe
-- the window that happens to be set right now.
--
-- Columns are TIMESTAMPTZ, matching every other time column after migration
-- 002. A bare TIMESTAMP would be read back as UTC regardless of what was
-- written, shifting each instant by the writer's offset.

CREATE TABLE IF NOT EXISTS maintenance_histories (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    monitor_id UUID        NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    start_time TIMESTAMPTZ NOT NULL,
    end_time   TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT maintenance_history_dates_valid CHECK (start_time < end_time)
);

-- Reporting reads windows for one monitor overlapping a time range.
CREATE INDEX IF NOT EXISTS idx_maintenance_histories_monitor_time
    ON maintenance_histories (monitor_id, start_time, end_time);

-- Carry over the window each monitor currently has configured, so windows that
-- pre-date this table are not lost from history the moment they are cleared.
INSERT INTO maintenance_histories (monitor_id, start_time, end_time)
SELECT id, maintenance_start, maintenance_end
FROM monitors
WHERE maintenance_mode_enabled = true
  AND maintenance_start IS NOT NULL
  AND maintenance_end IS NOT NULL
  AND maintenance_start < maintenance_end;
