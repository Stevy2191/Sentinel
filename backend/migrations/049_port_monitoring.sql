-- 049_port_monitoring.sql
-- Port monitoring (network monitoring phase 2): per-interface collection and
-- alerting settings and live state, device overrides and type, the port event
-- log, and incidents/notifications that belong to a port.

ALTER TABLE device_interfaces
    ADD COLUMN IF NOT EXISTS connector_present       BOOLEAN,
    ADD COLUMN IF NOT EXISTS has_ifx                 BOOLEAN NOT NULL DEFAULT false,
    -- null = use collect_default (the classifier's answer, set by inventory)
    ADD COLUMN IF NOT EXISTS collect                 BOOLEAN,
    ADD COLUMN IF NOT EXISTS collect_default         BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS important               BOOLEAN NOT NULL DEFAULT false,
    -- null = the instance default setting
    ADD COLUMN IF NOT EXISTS util_threshold_pct      INTEGER CHECK (util_threshold_pct BETWEEN 10 AND 100),
    ADD COLUMN IF NOT EXISTS error_threshold_per_min INTEGER CHECK (error_threshold_per_min BETWEEN 1 AND 1000000),
    ADD COLUMN IF NOT EXISTS down_grace_seconds      INTEGER CHECK (down_grace_seconds BETWEEN 0 AND 86400),
    ADD COLUMN IF NOT EXISTS usual_speed_bps         BIGINT,
    ADD COLUMN IF NOT EXISTS conditions              JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS conditions_since        JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS oper_changed_at         TIMESTAMPTZ;

ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS vendor_override        TEXT,
    ADD COLUMN IF NOT EXISTS model_override         TEXT,
    ADD COLUMN IF NOT EXISTS location_override      TEXT,
    ADD COLUMN IF NOT EXISTS device_type            VARCHAR(20)
        CHECK (device_type IN ('switch', 'router', 'access_point', 'nvr', 'other')),
    ADD COLUMN IF NOT EXISTS device_type_detected   VARCHAR(20) NOT NULL DEFAULT 'other'
        CHECK (device_type_detected IN ('switch', 'router', 'access_point', 'nvr', 'other')),
    ADD COLUMN IF NOT EXISTS faceplate_rows         INTEGER CHECK (faceplate_rows IN (1, 2)),
    ADD COLUMN IF NOT EXISTS faceplate_sfp_ports    JSONB,
    ADD COLUMN IF NOT EXISTS last_stats_at          TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_stats_duration_ms INTEGER;

CREATE TABLE IF NOT EXISTS port_events (
    id           BIGSERIAL PRIMARY KEY,
    device_id    UUID NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    interface_id UUID NOT NULL REFERENCES device_interfaces (id) ON DELETE CASCADE,
    if_index     INTEGER NOT NULL,
    kind         VARCHAR(20) NOT NULL CHECK (kind IN ('link_up', 'link_down', 'flapping', 'speed_change',
                     'errors', 'saturated', 'slow_link', 'admin_up', 'admin_down')),
    started_at   TIMESTAMPTZ NOT NULL,
    -- set on span events (flapping, errors, saturated, slow_link) when they end
    ended_at     TIMESTAMPTZ,
    detail       JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_port_events_device ON port_events (device_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_port_events_interface ON port_events (interface_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_port_events_open ON port_events (interface_id, kind) WHERE ended_at IS NULL;

ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS interface_id UUID REFERENCES device_interfaces (id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS condition    VARCHAR(20);
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_port_check;
ALTER TABLE incidents ADD CONSTRAINT incidents_port_check CHECK (
    (interface_id IS NULL AND condition IS NULL)
    OR (interface_id IS NOT NULL AND device_id IS NOT NULL AND condition IS NOT NULL
        AND condition IN ('link_down', 'errors', 'flapping', 'slow_link', 'saturated'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_incidents_open_port
    ON incidents (interface_id, condition) WHERE end_time IS NULL AND interface_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_incidents_interface_start
    ON incidents (interface_id, start_time DESC) WHERE interface_id IS NOT NULL;

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS interface_id UUID REFERENCES device_interfaces (id) ON DELETE CASCADE;
