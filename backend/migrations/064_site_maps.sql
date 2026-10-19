-- Site maps (network phase 6, piece C): where each router and switch sits on
-- its site's map, the links people drew or hid, and the map's public link.
-- Links Sentinel finds (port settings, LLDP, MAC tables) are worked out when
-- the map is read and are not stored; site_map_links holds only hand-drawn
-- links and "hidden" overrides on found ones. Configuration, so it lives in
-- public and is backed up.

CREATE TABLE site_map_positions (
    site_id    UUID             NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    device_id  UUID             NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    x          DOUBLE PRECISION NOT NULL DEFAULT 0,
    y          DOUBLE PRECISION NOT NULL DEFAULT 0,
    -- has_position: x and y were saved by an editor; false means the map
    -- lays the device out itself (a row may exist only for hidden/added).
    has_position BOOLEAN        NOT NULL DEFAULT false,
    hidden     BOOLEAN          NOT NULL DEFAULT false,
    -- added: a device of another type (a firewall filed as Other) that an
    -- editor put on the map. Routers and switches are on it anyway.
    added      BOOLEAN          NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ      NOT NULL DEFAULT now(),
    PRIMARY KEY (site_id, device_id)
);

CREATE TABLE site_map_links (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id     UUID        NOT NULL REFERENCES sites (id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL CHECK (kind IN ('manual', 'hidden')),
    a_device_id UUID        NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    a_if_index  INT,
    b_device_id UUID        NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    b_if_index  INT,
    created_by  UUID        REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (a_device_id <> b_device_id)
);
CREATE INDEX site_map_links_site ON site_map_links (site_id);

CREATE TABLE site_map_public_links (
    site_id    UUID        PRIMARY KEY REFERENCES sites (id) ON DELETE CASCADE,
    token      TEXT        NOT NULL UNIQUE,
    created_by UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
