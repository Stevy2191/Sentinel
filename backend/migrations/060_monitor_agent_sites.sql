-- 060_monitor_agent_sites.sql
-- UX piece 2 (spec 2026-10-07-ux-piece2-monitoring-list-design.md): uptime
-- monitors and server agents can carry a site, so the Monitoring list's site
-- filter covers them as it does devices. Optional, and a label only: it does
-- not change who can see anything. Deleting a site leaves its monitors and
-- agents in place with no site.

ALTER TABLE monitors ADD COLUMN IF NOT EXISTS site_id UUID REFERENCES sites(id) ON DELETE SET NULL;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS site_id UUID REFERENCES sites(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_monitors_site_id ON monitors (site_id);
CREATE INDEX IF NOT EXISTS idx_agents_site_id ON agents (site_id);
