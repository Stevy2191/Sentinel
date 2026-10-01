-- 051_port_links.sql
-- Port links (network monitoring phase 2 task 17): the device (and
-- optionally the port) on the other end of a "Network link" port, and which
-- stack member a port belongs to (0 when the device is not a stack).
-- Inventory writes stack_unit; it never writes neighbor_device_id,
-- neighbor_if_index or role.

ALTER TABLE device_interfaces
    ADD COLUMN IF NOT EXISTS neighbor_device_id UUID REFERENCES devices (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS neighbor_if_index  INTEGER,
    -- stack member (1, 2, ...) when the device is a stack of more than one
    -- switch; 0 otherwise. Set by inventory.
    ADD COLUMN IF NOT EXISTS stack_unit         INTEGER NOT NULL DEFAULT 0;

ALTER TABLE device_interfaces DROP CONSTRAINT IF EXISTS device_interfaces_neighbor_check;
-- Deviation from the design doc: the doc's version of this CHECK also
-- required neighbor_if_index IS NULL whenever neighbor_device_id IS NULL.
-- That breaks ON DELETE SET NULL: deleting a neighbor device nulls only
-- neighbor_device_id (the FK column), leaving a non-null neighbor_if_index
-- behind, so the UPDATE Postgres runs for the cascade would itself violate
-- the CHECK and the DELETE would fail outright (verified against Postgres
-- 16). The only invariant that actually matters is "a port with a neighbor
-- device is role uplink"; neighbor_if_index's own validity (it cannot be
-- given without a neighbor device) is enforced in PortPatch.Validate, not
-- here, since a stale if_index left behind by a cascade is harmless.
ALTER TABLE device_interfaces ADD CONSTRAINT device_interfaces_neighbor_check CHECK (
    neighbor_device_id IS NULL OR role = 'uplink');
