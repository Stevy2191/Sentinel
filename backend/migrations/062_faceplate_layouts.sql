-- 062_faceplate_layouts.sql
-- An admin's front-panel layout for a device model: ordered groups of ports
-- (range, SFP or RJ45, one or two rows), used for every device whose model
-- matches model_key. Groups are validated in Go (portmon.ValidateGroups).
CREATE TABLE IF NOT EXISTS faceplate_layouts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model       TEXT NOT NULL,          -- as the admin saved it, for display
    model_key   TEXT NOT NULL UNIQUE,   -- portmon.NormalizeModel(model)
    groups      JSONB NOT NULL,         -- [{first,last,kind,rows,numbering}]
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
