-- Record what kind of failure opened an incident, and why.
--
-- The table already existed and is what uptime percentages, MTTR and the
-- reports are computed from, so it is extended rather than replaced. Two things
-- were missing in practice: every incident looked identical, and root_cause was
-- never written, so an incident carried no explanation at all.
--
-- Status is deliberately NOT added as a column. It is exactly "end_time IS
-- NULL", and a stored copy is a second source of truth that can disagree with
-- the timestamps the durations are computed from.
ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS incident_type VARCHAR(50) NOT NULL DEFAULT 'down'
        CHECK (incident_type IN ('down', 'timeout', 'error'));

-- Listing incidents newest-first across all monitors is the common query on the
-- incidents page, and the retention purge scans the same column.
CREATE INDEX IF NOT EXISTS idx_incidents_start_time ON incidents (start_time DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_created_at ON incidents (created_at);

COMMENT ON COLUMN incidents.incident_type IS
    'How the check failed: timeout, error (a response that was not healthy), or down.';
COMMENT ON COLUMN incidents.root_cause IS
    'The failing check''s error message, captured when the incident opened.';
