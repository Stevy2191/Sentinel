-- 055_ups_monitoring.sql
-- UPS monitoring: device-level condition incidents (a UPS on battery, low
-- battery, high load) and per-device thresholds.
--
-- A device-level condition incident has device_id and condition set and no
-- interface. The CHECK spells out "condition IS NOT NULL" because
-- "condition IN (...)" alone is NULL, not false, for a NULL condition.
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_port_check;
ALTER TABLE incidents ADD CONSTRAINT incidents_port_check CHECK (
    (interface_id IS NULL AND condition IS NULL)
    OR (interface_id IS NOT NULL AND device_id IS NOT NULL AND condition IS NOT NULL
        AND condition IN ('link_down', 'errors', 'flapping', 'slow_link', 'saturated'))
    OR (interface_id IS NULL AND device_id IS NOT NULL AND condition IS NOT NULL
        AND condition IN ('ups_on_battery', 'ups_low_battery', 'ups_high_load'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_incidents_open_device_condition
    ON incidents (device_id, condition)
    WHERE end_time IS NULL AND interface_id IS NULL AND condition IS NOT NULL;

-- NULL = the instance setting.
ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS ups_low_battery_pct INTEGER CHECK (ups_low_battery_pct BETWEEN 5 AND 95),
    ADD COLUMN IF NOT EXISTS ups_high_load_pct   INTEGER CHECK (ups_high_load_pct BETWEEN 10 AND 100);
