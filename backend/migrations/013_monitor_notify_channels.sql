-- 013_monitor_notify_channels.sql
-- Lets a monitor choose which notification channels it alerts on.
--
-- Until now SendNotification fanned every alert out to every enabled channel,
-- so a monitor could not opt out or pick a subset. The column is deliberately
-- three-valued:
--
--   NULL              -> every enabled channel (the behaviour before this
--                        migration, so existing monitors are unaffected)
--   '[]'              -> no notifications for this monitor
--   '["slack","ntfy"]'-> only those channels, if they are enabled globally
--
-- A JSONB array matches how tags are already stored on this table.

ALTER TABLE monitors
    ADD COLUMN IF NOT EXISTS notify_channels JSONB;

COMMENT ON COLUMN monitors.notify_channels IS
    'Channels to alert on: NULL = all enabled, [] = none, ["slack"] = subset';
