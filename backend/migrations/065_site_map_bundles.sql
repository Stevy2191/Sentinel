-- Site map bundles and ports marked automatically. A port's role is now set
-- by a person ('user') or by the rules that read LLDP, CDP, LAG membership
-- and a router's routes and addresses ('auto'); 'default' is neither, and
-- reads as an endpoint. role stays the column everything reads.

ALTER TABLE device_interfaces
    ADD COLUMN role_source  TEXT NOT NULL DEFAULT 'default'
        CHECK (role_source IN ('default', 'user', 'auto')),
    ADD COLUMN role_reason  TEXT NOT NULL DEFAULT '',
    -- The aggregator (a type-161 interface) this port is a member of.
    ADD COLUMN lag_if_index INT,
    -- What the last L2 poll found on a router's port, so the rules can be
    -- re-run without asking the device again.
    ADD COLUMN wan_evidence TEXT NOT NULL DEFAULT ''
        CHECK (wan_evidence IN ('', 'default_route', 'public_address'));

-- Until now only a person set roles, so every role other than the default is theirs.
UPDATE device_interfaces SET role_source = 'user' WHERE role <> 'access';

-- Neighbours now come from LLDP or CDP; the same neighbour may be heard by both.
ALTER TABLE l2.neighbors
    ADD COLUMN protocol TEXT NOT NULL DEFAULT 'lldp' CHECK (protocol IN ('lldp', 'cdp')),
    ADD COLUMN address  TEXT NOT NULL DEFAULT '';
ALTER TABLE l2.neighbors DROP CONSTRAINT neighbors_pkey,
    ADD PRIMARY KEY (device_id, if_index, protocol, chassis_id);
