-- Notification settings for server agents.
--
-- Agents were already flipped to offline by the sweep, and the comment on that
-- code anticipated this: the transition was persisted so something could alert
-- on it later. Nothing ever did, so a server going silent was visible only to
-- whoever happened to open the page.
ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS notify_channels JSONB;

-- Delivery history has to be able to describe an agent alert.
--
-- notifications.monitor_id was NOT NULL with a foreign key to monitors, so
-- recording an agent's alert would have violated it. The column becomes
-- nullable and gains an agent_id beside it, with a check that exactly one is
-- set — a row belongs to a monitor or to an agent, never both and never
-- neither.
ALTER TABLE notifications
    ALTER COLUMN monitor_id DROP NOT NULL;

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS agent_id UUID REFERENCES agents (id) ON DELETE CASCADE;

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_subject_check;
ALTER TABLE notifications
    ADD CONSTRAINT notifications_subject_check
    CHECK (num_nonnulls(monitor_id, agent_id) = 1);

CREATE INDEX IF NOT EXISTS idx_notifications_agent
    ON notifications (agent_id, created_at DESC)
 WHERE agent_id IS NOT NULL;
