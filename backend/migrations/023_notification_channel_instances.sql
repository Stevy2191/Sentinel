-- Allow more than one configured channel of the same type.
--
-- Until now notification_configs held at most one row per channel type, so an
-- install could have exactly one Slack webhook, one ntfy topic and so on. That
-- is the wrong shape for real use: separate topics for separate teams, a
-- staging webhook alongside production, two Discord servers. The row already
-- had a UUID primary key; only a unique index stood in the way.
--
-- Identity moves from the channel type to that id. A name is added because a
-- list of four rows all saying "Ntfy" is unusable.

-- 1. Every channel gets a human name. Backfilled from the type so existing rows
--    are immediately readable, then made NOT NULL.
ALTER TABLE notification_configs
    ADD COLUMN IF NOT EXISTS name VARCHAR(80);

UPDATE notification_configs
SET name = initcap(channel)
WHERE name IS NULL OR btrim(name) = '';

ALTER TABLE notification_configs
    ALTER COLUMN name SET NOT NULL;

-- 2. The one-per-type constraint is what this migration exists to remove.
--    Lookups by type are still common (loading every enabled Slack instance),
--    so the index is replaced rather than dropped.
DROP INDEX IF EXISTS idx_notification_config_channel;
CREATE INDEX IF NOT EXISTS idx_notification_config_channel ON notification_configs (channel);

-- 3. Delivery history records which channel *type* sent a notification, which
--    stops being enough the moment two instances share a type. The id is
--    nullable because history outlives the channel that produced it, and is
--    SET NULL rather than CASCADE for the same reason: deleting a channel must
--    not erase the record of what it already sent.
ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS channel_id UUID
        REFERENCES notification_configs (id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_notifications_channel_id ON notifications (channel_id);

COMMENT ON COLUMN notification_configs.name IS
    'Operator-facing label. Unique per install by convention, not enforced.';
COMMENT ON COLUMN notifications.channel_id IS
    'Which configured channel sent this. NULL for history whose channel was deleted, or for a send that predates instance identity.';
