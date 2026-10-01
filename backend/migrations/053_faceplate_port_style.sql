-- 053_faceplate_port_style.sql
-- The user can draw a device's main faceplate ports as all SFP or all RJ45
-- when automatic detection gets it wrong. NULL is Auto.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS faceplate_port_style TEXT
    CHECK (faceplate_port_style IN ('sfp', 'rj45'));
