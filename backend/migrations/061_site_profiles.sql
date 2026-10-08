-- 061_site_profiles.sql
-- UX piece 3 (spec 2026-10-08-ux-piece3-site-profiles-design.md): each site
-- keeps its networks, its ISPs and circuits, and free-form notes.

ALTER TABLE sites ADD COLUMN IF NOT EXISTS notes TEXT;

-- One subnet at a site. The unique key also serves lookups by site.
CREATE TABLE IF NOT EXISTS site_networks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id    UUID NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    cidr       CIDR NOT NULL,
    vlan       INTEGER,
    gateway    INET,
    note       VARCHAR(500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (site_id, cidr)
);

-- One ISP circuit at a site, optionally tied to the device port it plugs
-- into. Deleting that port (or its device) clears the link, not the circuit.
CREATE TABLE IF NOT EXISTS site_circuits (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id        UUID NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    provider       VARCHAR(100) NOT NULL,
    circuit_ref    VARCHAR(100),
    kind           VARCHAR(20) NOT NULL DEFAULT 'other'
        CHECK (kind IS NOT NULL AND kind IN ('fiber', 'cable', 'dsl', 'fixed_wireless', 'cellular', 'copper', 'other')),
    download_mbps  DOUBLE PRECISION,
    upload_mbps    DOUBLE PRECISION,
    support_phone  VARCHAR(50),
    account_number VARCHAR(100),
    notes          VARCHAR(1000),
    interface_id   UUID REFERENCES device_interfaces (id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_site_circuits_site_id ON site_circuits (site_id);
CREATE INDEX IF NOT EXISTS idx_site_circuits_interface_id ON site_circuits (interface_id);
