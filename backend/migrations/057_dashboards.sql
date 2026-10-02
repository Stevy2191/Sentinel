-- 057_dashboards.sql
-- Phase 4: user-built dashboards (spec 2026-10-02-network-phase4-dashboards-design.md).
-- A dashboard is personal (an owner, no site) or a site's (a site, no owner).

CREATE TABLE IF NOT EXISTS dashboards (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 500),
    site_id     UUID REFERENCES sites (id) ON DELETE CASCADE,
    owner_id    UUID REFERENCES users (id) ON DELETE CASCADE,
    created_by  UUID REFERENCES users (id) ON DELETE SET NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Both FKs in this CHECK cascade; neither is SET NULL, so a delete can
    -- never leave a row that violates it.
    CONSTRAINT dashboards_owner_xor_site CHECK ((site_id IS NULL) = (owner_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS dashboards_site ON dashboards (site_id) WHERE site_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS dashboards_owner ON dashboards (owner_id) WHERE owner_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS dashboard_widgets (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES dashboards (id) ON DELETE CASCADE,
    type         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 100),
    config       JSONB NOT NULL DEFAULT '{}',
    x INTEGER NOT NULL CHECK (x >= 0),
    y INTEGER NOT NULL CHECK (y >= 0),
    w INTEGER NOT NULL CHECK (w BETWEEN 1 AND 12),
    h INTEGER NOT NULL CHECK (h BETWEEN 1 AND 24),
    CONSTRAINT dashboard_widgets_in_grid CHECK (x + w <= 12)
);
CREATE INDEX IF NOT EXISTS dashboard_widgets_dashboard ON dashboard_widgets (dashboard_id);

CREATE TABLE IF NOT EXISTS dashboard_sharing (
    dashboard_id UUID NOT NULL REFERENCES dashboards (id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    permission   TEXT NOT NULL CHECK (permission IN ('readonly', 'editable')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (dashboard_id, user_id)
);
CREATE INDEX IF NOT EXISTS dashboard_sharing_user ON dashboard_sharing (user_id);

CREATE TABLE IF NOT EXISTS dashboard_public_links (
    dashboard_id UUID PRIMARY KEY REFERENCES dashboards (id) ON DELETE CASCADE,
    token        TEXT NOT NULL UNIQUE,
    created_by   UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
