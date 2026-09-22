-- Configurable resource-usage alert thresholds for server agents.
--
-- An agent could only alert on connectivity until now (029/040): went
-- offline, came back. Every metrics sample it reports - CPU, memory, disk -
-- was stored and shown, but nothing ever evaluated it. A server could sit at
-- 98% disk for weeks and Sentinel would never say a word.
--
-- No column-level default on the threshold values: an existing server must
-- not suddenly start alerting on numbers it has always run at. NULL means
-- "not configured". A new server's 90% default lives in Go
-- (models.DefaultThresholdPercent), applied by the create-agent handler, the
-- same way DefaultAgentInterval already works.
ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS cpu_threshold_percent SMALLINT,
    ADD COLUMN IF NOT EXISTS memory_threshold_percent SMALLINT,
    ADD COLUMN IF NOT EXISTS disk_threshold_percent SMALLINT,
    ADD COLUMN IF NOT EXISTS cpu_alert_active BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS memory_alert_active BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS disk_alert_active BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_cpu_threshold_check;
ALTER TABLE agents
    ADD CONSTRAINT agents_cpu_threshold_check
    CHECK (cpu_threshold_percent IS NULL OR cpu_threshold_percent BETWEEN 1 AND 100);

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_memory_threshold_check;
ALTER TABLE agents
    ADD CONSTRAINT agents_memory_threshold_check
    CHECK (memory_threshold_percent IS NULL OR memory_threshold_percent BETWEEN 1 AND 100);

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_disk_threshold_check;
ALTER TABLE agents
    ADD CONSTRAINT agents_disk_threshold_check
    CHECK (disk_threshold_percent IS NULL OR disk_threshold_percent BETWEEN 1 AND 100);
