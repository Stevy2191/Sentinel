-- 052_device_type_ups.sql
-- UPS becomes a device type: chosen by the user (device_type) or detected by
-- inventory from the vendor's enterprise number (device_type_detected).
-- 049 declared both CHECKs inline, so Postgres named them
-- devices_device_type_check and devices_device_type_detected_check.
ALTER TABLE devices DROP CONSTRAINT IF EXISTS devices_device_type_check;
ALTER TABLE devices ADD CONSTRAINT devices_device_type_check
    CHECK (device_type IN ('switch', 'router', 'access_point', 'nvr', 'ups', 'other'));
ALTER TABLE devices DROP CONSTRAINT IF EXISTS devices_device_type_detected_check;
ALTER TABLE devices ADD CONSTRAINT devices_device_type_detected_check
    CHECK (device_type_detected IN ('switch', 'router', 'access_point', 'nvr', 'ups', 'other'));
