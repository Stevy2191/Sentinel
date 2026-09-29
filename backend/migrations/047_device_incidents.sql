-- 047_device_incidents.sql
-- Incidents and notifications can belong to a network device as well as a
-- monitor (network monitoring phase 1). Mirrors 040, which made notifications
-- "monitor or agent".
--
-- Every existing uptime/downtime query filters monitor_id = ?, so a device
-- incident (monitor_id NULL) cannot reach a monitor's numbers.

ALTER TABLE incidents ALTER COLUMN monitor_id DROP NOT NULL;
ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS device_id UUID REFERENCES devices (id) ON DELETE CASCADE;
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_subject_check;
ALTER TABLE incidents
    ADD CONSTRAINT incidents_subject_check CHECK (num_nonnulls(monitor_id, device_id) = 1);
CREATE INDEX IF NOT EXISTS idx_incidents_device_start
    ON incidents (device_id, start_time DESC) WHERE device_id IS NOT NULL;

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS device_id UUID REFERENCES devices (id) ON DELETE CASCADE;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_subject_check;
ALTER TABLE notifications
    ADD CONSTRAINT notifications_subject_check
    CHECK (num_nonnulls(monitor_id, agent_id, device_id) = 1);
CREATE INDEX IF NOT EXISTS idx_notifications_device
    ON notifications (device_id, created_at DESC) WHERE device_id IS NOT NULL;
