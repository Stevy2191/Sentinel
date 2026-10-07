-- 059_network_tools.sql
-- Tools and security S1 (spec 2026-10-05-tools-s1-network-tools-design.md):
-- on-demand ping, traceroute, DNS lookup and TCP port checks, run from the
-- Sentinel server or an agent, kept as run history.

-- The "Network tools" grant. Admins are always permitted, whatever it says.
ALTER TABLE users ADD COLUMN IF NOT EXISTS net_tools BOOLEAN NOT NULL DEFAULT false;

-- The admin's switch for an agent, and the agent's own ENABLE_TOOLS flag as
-- its heartbeat reports it. tools_local is NULL until an agent reports the
-- flag at all: an agent too old to run tools never does.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS tools_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS tools_local BOOLEAN;

-- One run of one tool. Agent runs wait here as 'queued' until the agent
-- claims them. No CHECK ties vantage_kind to agent_id: deleting an agent
-- sets agent_id NULL on its runs, and such a CHECK would refuse that.
-- target_ip is canonical dotted IPv4 (only equality is needed); it is NULL
-- for a DNS lookup through the vantage's own resolver. agent_ref snapshots
-- the agent's readable id for links.
CREATE TABLE IF NOT EXISTS tool_runs (
    id           UUID PRIMARY KEY,
    tool         TEXT NOT NULL
        CHECK (tool IS NOT NULL AND tool IN ('ping', 'traceroute', 'dns', 'tcp')),
    status       TEXT NOT NULL
        CHECK (status IS NOT NULL AND status IN ('queued', 'running', 'done', 'failed', 'refused',
                                                 'cancelled', 'timed_out', 'interrupted')),
    user_id      UUID REFERENCES users (id) ON DELETE SET NULL,
    username     TEXT NOT NULL,
    vantage_kind TEXT NOT NULL
        CHECK (vantage_kind IS NOT NULL AND vantage_kind IN ('sentinel', 'agent')),
    agent_id     UUID REFERENCES agents (id) ON DELETE SET NULL,
    agent_ref    TEXT,
    vantage_name TEXT NOT NULL,
    target       TEXT NOT NULL,
    target_ip    TEXT,
    params       JSONB NOT NULL,
    summary      JSONB,
    error        TEXT,
    event_count  INTEGER NOT NULL DEFAULT 0 CHECK (event_count >= 0),
    created_at   TIMESTAMPTZ NOT NULL,
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    deadline     TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS tool_runs_created ON tool_runs (created_at DESC);
-- The active-run caps count these.
CREATE INDEX IF NOT EXISTS tool_runs_active_user ON tool_runs (user_id) WHERE status IN ('queued', 'running');
CREATE INDEX IF NOT EXISTS tool_runs_active_agent ON tool_runs (agent_id) WHERE status IN ('queued', 'running');
CREATE INDEX IF NOT EXISTS tool_runs_active_target ON tool_runs (target_ip) WHERE status IN ('queued', 'running');
-- An agent's job queue, oldest first.
CREATE INDEX IF NOT EXISTS tool_runs_queued ON tool_runs (agent_id, created_at) WHERE status = 'queued';
-- The daily prune.
CREATE INDEX IF NOT EXISTS tool_runs_finished ON tool_runs (finished_at) WHERE finished_at IS NOT NULL;

-- What a run reported, in order. A plain table: a run has at most 5,000.
CREATE TABLE IF NOT EXISTS tool_run_events (
    run_id UUID NOT NULL REFERENCES tool_runs (id) ON DELETE CASCADE,
    seq    INTEGER NOT NULL CHECK (seq > 0),
    at     TIMESTAMPTZ NOT NULL,
    type   TEXT NOT NULL,
    data   JSONB NOT NULL,
    PRIMARY KEY (run_id, seq)
);
