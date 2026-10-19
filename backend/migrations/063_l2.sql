-- What switches and routers know about the endpoints behind them, from the
-- L2 poll: MAC address tables, LLDP neighbours and ARP tables, plus a cache
-- of reverse-DNS names. Kept in their own schema so config backups (which
-- dump public.* only) leave out this regenerable data. No foreign keys:
-- DeviceService.Delete removes a device's rows in its own transaction, and
-- the nightly maintenance drops rows not seen for 30 days.
CREATE SCHEMA IF NOT EXISTS l2;

CREATE TABLE l2.sightings (
    device_id  UUID        NOT NULL,
    if_index   INT         NOT NULL,
    vlan       INT         NOT NULL DEFAULT 0,
    mac        TEXT        NOT NULL,
    first_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, if_index, vlan, mac)
);
CREATE INDEX l2_sightings_mac ON l2.sightings (mac);

CREATE TABLE l2.neighbors (
    device_id  UUID        NOT NULL,
    if_index   INT         NOT NULL,
    chassis_id TEXT        NOT NULL,
    sys_name   TEXT        NOT NULL DEFAULT '',
    port_id    TEXT        NOT NULL DEFAULT '',
    port_descr TEXT        NOT NULL DEFAULT '',
    caps       SMALLINT    NOT NULL DEFAULT 0,
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, if_index, chassis_id)
);

CREATE TABLE l2.arp (
    device_id UUID        NOT NULL,
    ip        TEXT        NOT NULL,
    mac       TEXT        NOT NULL,
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, ip)
);
CREATE INDEX l2_arp_mac ON l2.arp (mac);

CREATE TABLE l2.dns_names (
    ip          TEXT PRIMARY KEY,
    name        TEXT        NOT NULL DEFAULT '',
    resolved_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Names users give endpoints ("Front desk phone"). Configuration, so it
-- lives in public and is backed up.
CREATE TABLE endpoint_names (
    mac        TEXT PRIMARY KEY,
    name       TEXT        NOT NULL,
    updated_by UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
