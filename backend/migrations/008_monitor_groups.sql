-- 008_monitor_groups.sql
-- Monitor groups let the dashboard organize monitors into collapsible sections
-- with a per-group uptime roll-up. Numbered 008 (007 adds ntfy_auth_token).

CREATE TABLE monitor_groups (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    color       VARCHAR(7),            -- hex color for the group badge, e.g. #10b981
    position    INTEGER NOT NULL DEFAULT 0, -- ordering on the dashboard
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Assign monitors to a group; deleting a group ungroups its monitors.
ALTER TABLE monitors
    ADD COLUMN group_id UUID REFERENCES monitor_groups(id) ON DELETE SET NULL;

CREATE INDEX idx_monitors_group_id ON monitors(group_id);
