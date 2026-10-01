-- 050_port_role.sql
-- Port role (network monitoring phase 2 task 16): what each port connects to
-- (access/uplink/wan), for north-south / east-west site traffic. Inventory
-- never writes this column; it is only set through PATCH /devices/:id/ports/:ifIndex.

ALTER TABLE device_interfaces ADD COLUMN IF NOT EXISTS role VARCHAR(10) NOT NULL DEFAULT 'access'
    CHECK (role IN ('access', 'uplink', 'wan'));
