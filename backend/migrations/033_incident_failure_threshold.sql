-- How many consecutive failed checks it takes to declare an incident.
--
-- Until now a single failed check flipped a monitor offline and opened an
-- incident on the spot. One dropped packet or one momentary 502 therefore
-- produced a real incident that closed again on the next cycle, which is what
-- makes an hour read as a "partial" outage when nothing was actually down.
--
-- This is distinct from "retries", which are attempts inside a single check:
-- retries decide whether one check fails, this decides how many failed checks
-- it takes to call it an outage.
--
-- Default 2 rather than 1: one bad cycle is noise, two in a row is a pattern.
-- Existing monitors adopt it, which is the point — they are the ones logging
-- blips today.
ALTER TABLE monitors
    ADD COLUMN IF NOT EXISTS failure_threshold INTEGER NOT NULL DEFAULT 2;

ALTER TABLE monitors DROP CONSTRAINT IF EXISTS monitors_failure_threshold_check;
ALTER TABLE monitors
    ADD CONSTRAINT monitors_failure_threshold_check
    CHECK (failure_threshold BETWEEN 1 AND 10);

-- The current run of consecutive failures, kept on the row so a restart does
-- not reset a streak mid-outage and so the decision costs no extra query.
ALTER TABLE monitors
    ADD COLUMN IF NOT EXISTS consecutive_failures INTEGER NOT NULL DEFAULT 0;

-- When the current streak began. An incident confirmed on the Nth failure
-- started at the first one, not at the moment it was confirmed, so downtime is
-- measured from when the service actually stopped answering.
ALTER TABLE monitors
    ADD COLUMN IF NOT EXISTS failure_streak_started_at TIMESTAMP WITH TIME ZONE;

-- A monitor already offline has, by definition, met its threshold. Seeding the
-- counter keeps those rows consistent with the new logic instead of requiring
-- a fresh streak before the next recovery can be recognised.
UPDATE monitors
   SET consecutive_failures = failure_threshold
 WHERE current_status = 'offline'
   AND consecutive_failures = 0;
