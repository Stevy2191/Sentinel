-- 045_sites.sql
-- Sites and per-site sharing (network monitoring roadmap, phase 0).
--
-- site_sharing mirrors monitor_sharing (010) with two deliberate differences:
-- shared_by_user_id is SET NULL rather than a blocking reference, so deleting
-- the admin who shared a site does not fail, and permission is constrained here
-- as well as in Go.

CREATE TABLE IF NOT EXISTS sites (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    address     TEXT,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Case-insensitive: "HQ" and "hq" would be indistinguishable in every site
-- picker later phases add.
CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_name_lower ON sites (lower(name));

CREATE TABLE IF NOT EXISTS site_sharing (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id             UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    shared_with_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission          VARCHAR(50) NOT NULL DEFAULT 'readonly',
    shared_by_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (site_id, shared_with_user_id),
    CONSTRAINT site_sharing_permission_check CHECK (permission IN ('readonly', 'editable'))
);

CREATE INDEX IF NOT EXISTS idx_site_sharing_user_id ON site_sharing (shared_with_user_id);
