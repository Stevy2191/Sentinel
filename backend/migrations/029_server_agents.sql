-- ---------------------------------------------------------------------------
-- 029_server_agents
-- Server monitoring: an agent installed on a host reports system and container
-- metrics back to Sentinel.
--
-- Distinct from the monitors table, which probes a service from the outside.
-- An agent reports from inside a host it lives on, so it is identified by a
-- credential it holds rather than by a URL we reach.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    name VARCHAR(255) NOT NULL,

    -- The identifier the agent presents. Separate from the primary key: it
    -- travels in install commands, service files and log lines, where a bare
    -- UUID is hard to read and hard to type. Unique, since it is what an
    -- inbound request is looked up by.
    agent_id VARCHAR(64) NOT NULL UNIQUE,

    -- The shared secret the agent authenticates with. Read only by
    -- administrators and never returned in list responses, matching how
    -- notification channel secrets are handled.
    server_token VARCHAR(128) NOT NULL UNIQUE,

    os_type VARCHAR(50) NOT NULL DEFAULT 'linux'
        CHECK (os_type IN ('ubuntu','debian','centos','rhel','windows','linux')),

    check_interval INTEGER NOT NULL DEFAULT 60
        CHECK (check_interval BETWEEN 1 AND 3600),
    retry_attempts INTEGER NOT NULL DEFAULT 3
        CHECK (retry_attempts BETWEEN 1 AND 10),

    -- 'pending' until the agent first reports; there is no useful status
    -- before that, and calling an agent that has never connected "offline"
    -- reads as a fault rather than as work still to do.
    status VARCHAR(50) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','active','offline')),

    last_heartbeat TIMESTAMPTZ,
    ip_address     VARCHAR(45),
    hostname       VARCHAR(255),
    -- What the agent reports about itself on connect.
    os_version    VARCHAR(255),
    agent_version VARCHAR(50),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agents_status ON agents (status);

-- ---------------------------------------------------------------------------
-- agent_metrics
-- One row per collection cycle. This is the fastest-growing table in the
-- schema after checks, so it carries a retention window from the start rather
-- than acquiring one later.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS agent_metrics (
    id BIGSERIAL PRIMARY KEY,
    agent_id UUID NOT NULL REFERENCES agents (id) ON DELETE CASCADE,

    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    cpu_percent    DOUBLE PRECISION,
    memory_percent DOUBLE PRECISION,
    memory_used_mb BIGINT,
    memory_total_mb BIGINT,
    disk_percent   DOUBLE PRECISION,
    disk_used_gb   DOUBLE PRECISION,
    disk_total_gb  DOUBLE PRECISION,
    uptime_seconds BIGINT,

    load_average_1m  DOUBLE PRECISION,
    load_average_5m  DOUBLE PRECISION,
    load_average_15m DOUBLE PRECISION,

    -- Cumulative counters as the host reports them. Stored raw rather than as
    -- a rate so a gap between samples cannot silently distort a derived
    -- figure; the difference is taken when charting.
    network_in_bytes  BIGINT,
    network_out_bytes BIGINT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Covers the dashboard's only access pattern: one agent, newest first.
CREATE INDEX IF NOT EXISTS idx_agent_metrics_agent_time
    ON agent_metrics (agent_id, timestamp DESC);
-- Supports the retention purge, which sweeps by age across all agents.
CREATE INDEX IF NOT EXISTS idx_agent_metrics_timestamp
    ON agent_metrics (timestamp);

-- ---------------------------------------------------------------------------
-- agent_containers
-- Per-container metrics from one collection cycle, for hosts running Docker.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS agent_containers (
    id BIGSERIAL PRIMARY KEY,
    agent_id UUID NOT NULL REFERENCES agents (id) ON DELETE CASCADE,

    container_id   VARCHAR(255) NOT NULL,
    container_name VARCHAR(255),
    image          VARCHAR(255),
    status         VARCHAR(50),

    cpu_percent    DOUBLE PRECISION,
    memory_percent DOUBLE PRECISION,
    memory_used_mb BIGINT,

    timestamp  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_containers_agent_time
    ON agent_containers (agent_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_agent_containers_timestamp
    ON agent_containers (timestamp);
