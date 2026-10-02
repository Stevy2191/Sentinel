-- 056_custom_metrics.sql
-- Network phase 3: the MIB library, metric profiles and custom metrics, the
-- label on a metrics series, and metric-rule incidents.

CREATE TABLE IF NOT EXISTS mib_modules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    source      TEXT NOT NULL CHECK (source IS NOT NULL AND source IN ('builtin', 'upload')),
    file_name   TEXT NOT NULL DEFAULT '',
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    sha256      TEXT NOT NULL,
    content     TEXT NOT NULL,
    imports     TEXT[] NOT NULL DEFAULT '{}',
    missing     TEXT[] NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL CHECK (status IS NOT NULL AND status IN ('ready', 'waiting')),
    uploaded_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS mib_objects (
    id            BIGSERIAL PRIMARY KEY,
    module_id     UUID NOT NULL REFERENCES mib_modules(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    oid           TEXT NOT NULL,
    parent_oid    TEXT NOT NULL DEFAULT '',
    kind          TEXT NOT NULL,
    base_type     TEXT NOT NULL DEFAULT '',
    type_name     TEXT NOT NULL DEFAULT '',
    units         TEXT NOT NULL DEFAULT '',
    access        TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    enum          JSONB,
    index_columns TEXT[] NOT NULL DEFAULT '{}',
    UNIQUE (module_id, name)
);
CREATE INDEX IF NOT EXISTS idx_mib_objects_oid ON mib_objects (oid);
CREATE INDEX IF NOT EXISTS idx_mib_objects_parent ON mib_objects (parent_oid);
CREATE INDEX IF NOT EXISTS idx_mib_objects_lname ON mib_objects (lower(name));
CREATE INDEX IF NOT EXISTS idx_mib_objects_search ON mib_objects
    USING gin (to_tsvector('simple', name || ' ' || description));

CREATE TABLE IF NOT EXISTS metric_profiles (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  TEXT NOT NULL UNIQUE,
    description           TEXT NOT NULL DEFAULT '',
    match_prefixes        TEXT[] NOT NULL DEFAULT '{}',
    poll_interval_minutes INTEGER NOT NULL DEFAULT 1 CHECK (poll_interval_minutes IN (1, 5, 15)),
    builtin               BOOLEAN NOT NULL DEFAULT false,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS profile_metrics (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    profile_id        UUID NOT NULL REFERENCES metric_profiles(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    key               TEXT NOT NULL UNIQUE
        CHECK (key ~ '^[a-z][a-z0-9_]{2,62}$' AND key NOT LIKE 'if\_%' AND key NOT LIKE 'ups\_%'),
    source            TEXT NOT NULL CHECK (source IS NOT NULL AND source IN ('scalar', 'column', 'used_free_pct')),
    kind              TEXT NOT NULL CHECK (kind IS NOT NULL AND kind IN ('gauge', 'counter', 'status')),
    units             TEXT NOT NULL DEFAULT '',
    scale             DOUBLE PRECISION NOT NULL DEFAULT 1,
    oid               TEXT NOT NULL,
    oid2              TEXT NOT NULL DEFAULT '',
    precision_oid     TEXT NOT NULL DEFAULT '',
    filter_oid        TEXT NOT NULL DEFAULT '',
    filter_values     TEXT[] NOT NULL DEFAULT '{}',
    label_mode        TEXT NOT NULL DEFAULT 'index'
        CHECK (label_mode IS NOT NULL AND label_mode IN ('index', 'column', 'same_index', 'pointer')),
    label_oid         TEXT NOT NULL DEFAULT '',
    label_pointer_oid TEXT NOT NULL DEFAULT '',
    label_target_oid  TEXT NOT NULL DEFAULT '',
    ok_states         BIGINT[] NOT NULL DEFAULT '{}',
    state_names       JSONB,
    rule_kind         TEXT NOT NULL DEFAULT '' CHECK (rule_kind IN ('', 'above', 'below', 'not_ok')),
    rule_value        DOUBLE PRECISION,
    rule_hold_minutes INTEGER NOT NULL DEFAULT 0 CHECK (rule_hold_minutes BETWEEN 0 AND 1440),
    rule_enabled      BOOLEAN NOT NULL DEFAULT false,
    position          INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_profile_metrics_profile ON profile_metrics (profile_id, position);

CREATE TABLE IF NOT EXISTS device_profile_overrides (
    device_id  UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES metric_profiles(id) ON DELETE CASCADE,
    mode       TEXT NOT NULL CHECK (mode IS NOT NULL AND mode IN ('attach', 'detach')),
    PRIMARY KEY (device_id, profile_id)
);

CREATE TABLE IF NOT EXISTS device_profile_runs (
    device_id  UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    profile_id UUID NOT NULL REFERENCES metric_profiles(id) ON DELETE CASCADE,
    ran_at     TIMESTAMPTZ NOT NULL,
    ok         BOOLEAN NOT NULL,
    error      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, profile_id)
);

ALTER TABLE metrics.series ADD COLUMN IF NOT EXISTS label TEXT NOT NULL DEFAULT '';

-- Metric-rule incidents: device-level condition incidents with the metric
-- key and row they are about.
ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS metric_key      TEXT,
    ADD COLUMN IF NOT EXISTS metric_instance TEXT;
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_port_check;
ALTER TABLE incidents ADD CONSTRAINT incidents_port_check CHECK (
    (interface_id IS NULL AND condition IS NULL AND metric_key IS NULL AND metric_instance IS NULL)
    OR (interface_id IS NOT NULL AND device_id IS NOT NULL AND condition IS NOT NULL
        AND condition IN ('link_down', 'errors', 'flapping', 'slow_link', 'saturated')
        AND metric_key IS NULL AND metric_instance IS NULL)
    OR (interface_id IS NULL AND device_id IS NOT NULL AND condition IS NOT NULL
        AND condition IN ('ups_on_battery', 'ups_low_battery', 'ups_high_load')
        AND metric_key IS NULL AND metric_instance IS NULL)
    OR (interface_id IS NULL AND device_id IS NOT NULL AND condition IS NOT NULL AND condition = 'metric'
        AND metric_key IS NOT NULL AND metric_instance IS NOT NULL)
);
-- uq_incidents_open_device_condition (055) is one open per (device, condition);
-- metric incidents are one open per (device, key, instance) instead.
DROP INDEX IF EXISTS uq_incidents_open_device_condition;
CREATE UNIQUE INDEX IF NOT EXISTS uq_incidents_open_device_condition
    ON incidents (device_id, condition)
    WHERE end_time IS NULL AND interface_id IS NULL AND condition IS NOT NULL AND condition <> 'metric';
CREATE UNIQUE INDEX IF NOT EXISTS uq_incidents_open_metric
    ON incidents (device_id, metric_key, metric_instance)
    WHERE end_time IS NULL AND condition = 'metric';
