-- 004_maintenance_mode.sql
-- Adds maintenance-mode columns to monitors. During an active window, failed
-- checks are still recorded but do not open incidents or fire notifications.

ALTER TABLE monitors
    ADD COLUMN maintenance_mode_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN maintenance_start TIMESTAMPTZ,
    ADD COLUMN maintenance_end TIMESTAMPTZ;

-- Ensure the window is coherent when both bounds are set.
ALTER TABLE monitors
    ADD CONSTRAINT maintenance_dates_valid
    CHECK (maintenance_start IS NULL OR maintenance_end IS NULL OR maintenance_start < maintenance_end);

-- Speed up lookups of monitors currently in maintenance.
CREATE INDEX idx_monitors_maintenance
    ON monitors (id, maintenance_mode_enabled, maintenance_start, maintenance_end)
    WHERE maintenance_mode_enabled = true;
