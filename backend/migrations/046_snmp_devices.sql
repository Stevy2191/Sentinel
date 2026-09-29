-- 046_snmp_devices.sql
-- SNMP credential profiles, devices and their interfaces (network monitoring
-- phase 1). All in public and all tables, per the phase 0 backup rule.

CREATE TABLE IF NOT EXISTS snmp_credentials (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              VARCHAR(255) NOT NULL,
    -- NULL = usable by every site; otherwise only by this site's devices.
    site_id           UUID REFERENCES sites (id) ON DELETE RESTRICT,
    version           VARCHAR(3) NOT NULL CHECK (version IN ('1', '2c', '3')),
    -- Secrets are ciphertext (cryptutil) and never returned by the API.
    community         TEXT,
    username          TEXT,
    auth_protocol     VARCHAR(10) NOT NULL DEFAULT 'none'
        CHECK (auth_protocol IN ('none','MD5','SHA','SHA224','SHA256','SHA384','SHA512')),
    auth_password     TEXT,
    priv_protocol     VARCHAR(10) NOT NULL DEFAULT 'none'
        CHECK (priv_protocol IN ('none','DES','AES','AES192','AES256')),
    priv_password     TEXT,
    created_by        UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_snmp_credentials_name ON snmp_credentials (lower(name));

CREATE TABLE IF NOT EXISTS devices (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id              UUID NOT NULL REFERENCES sites (id) ON DELETE RESTRICT,
    credential_id        UUID NOT NULL REFERENCES snmp_credentials (id) ON DELETE RESTRICT,
    name                 VARCHAR(255) NOT NULL,
    host                 VARCHAR(255) NOT NULL,
    port                 INTEGER NOT NULL DEFAULT 161 CHECK (port BETWEEN 1 AND 65535),
    enabled              BOOLEAN NOT NULL DEFAULT true,
    poll_interval        INTEGER NOT NULL DEFAULT 60 CHECK (poll_interval BETWEEN 10 AND 3600),
    timeout_ms           INTEGER NOT NULL DEFAULT 3000 CHECK (timeout_ms BETWEEN 200 AND 30000),
    retries              INTEGER NOT NULL DEFAULT 1 CHECK (retries BETWEEN 0 AND 5),
    -- null = every enabled channel, [] = none (the monitor/agent convention).
    notify_channels      JSONB,
    status               VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'up', 'down', 'paused', 'error')),
    status_detail        TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_polled_at       TIMESTAMPTZ,
    last_seen_at         TIMESTAMPTZ,
    last_inventory_at    TIMESTAMPTZ,
    sys_name             TEXT,
    sys_descr            TEXT,
    sys_object_id        TEXT,
    sys_location         TEXT,
    sys_contact          TEXT,
    sys_uptime_seconds   BIGINT,
    vendor               TEXT,
    model                TEXT,
    serial               TEXT,
    created_by           UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Per site, host and port: overlapping private ranges across sites are normal,
-- and two devices behind one NAT address on different ports are too.
CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_site_host_port ON devices (site_id, lower(host), port);
CREATE INDEX IF NOT EXISTS idx_devices_site ON devices (site_id);
CREATE INDEX IF NOT EXISTS idx_devices_due ON devices (last_polled_at) WHERE enabled;

CREATE TABLE IF NOT EXISTS device_interfaces (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id            UUID NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    if_index             INTEGER NOT NULL,
    name                 TEXT,
    descr                TEXT,
    alias                TEXT,
    if_type              INTEGER,
    speed_bps            BIGINT,
    mac                  TEXT,
    admin_status         VARCHAR(20),
    oper_status          VARCHAR(20),
    last_change_seconds  BIGINT,
    -- false once a walk no longer reports it; kept so later history is not
    -- orphaned. Deleted with its device.
    present              BOOLEAN NOT NULL DEFAULT true,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (device_id, if_index)
);
