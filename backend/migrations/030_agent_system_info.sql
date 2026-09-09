-- ---------------------------------------------------------------------------
-- 030_agent_system_info
-- What a host reports about itself, for the server detail view.
--
-- Stored on the agent row rather than with each metrics sample: these change
-- when a machine is rebuilt or upgraded, not every collection cycle, so
-- recording them per sample would repeat the same values thousands of times a
-- day. The agent refreshes them on every heartbeat.
-- ---------------------------------------------------------------------------

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS kernel_version VARCHAR(255),
    ADD COLUMN IF NOT EXISTS architecture   VARCHAR(32),
    ADD COLUMN IF NOT EXISTS cpu_model      VARCHAR(255),
    ADD COLUMN IF NOT EXISTS cpu_cores      INTEGER,
    ADD COLUMN IF NOT EXISTS memory_total_mb BIGINT,
    -- The Go runtime the agent was built with. Useful when a fleet is part
    -- upgraded and one host behaves differently from the rest.
    ADD COLUMN IF NOT EXISTS go_version VARCHAR(32),
    -- Whether the agent found a Docker socket. Nullable rather than defaulting
    -- to false, so "has not reported yet" stays distinct from "no Docker".
    ADD COLUMN IF NOT EXISTS docker_available BOOLEAN;
