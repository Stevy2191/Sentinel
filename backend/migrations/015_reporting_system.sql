-- 015_reporting_system.sql
-- Foundation for the reporting system: saved report definitions, the templates
-- that shape them, a generation history, and per-report sharing.
--
-- Deviations from the feature spec, and why:
--
--   * TIMESTAMPTZ, not TIMESTAMP. The spec's DDL says
--     "TIMESTAMP DEFAULT CURRENT_TIMESTAMP", but migration 002 exists solely to
--     convert this schema off bare TIMESTAMP after it produced timezone-offset
--     bugs. New tables must not reintroduce it.
--
--   * incidents.root_cause is NOT added here - 001_initial_schema.sql already
--     created it. The ADD below is left in with IF NOT EXISTS so this file still
--     documents the requirement, but it is a no-op on every existing database.
--
--   * incidents.notes already exists and holds much the same thing as the
--     requested resolution_notes. Both are kept: notes is in the API contract
--     today, and silently repurposing it would change existing behaviour.

-- ---------------------------------------------------------------------------
-- incident context
-- ---------------------------------------------------------------------------
ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS root_cause       TEXT,
    ADD COLUMN IF NOT EXISTS resolution_notes TEXT;

COMMENT ON COLUMN incidents.resolution_notes IS
    'How the incident was resolved. Distinct from notes (general context).';

-- ---------------------------------------------------------------------------
-- per-monitor SLA target
-- ---------------------------------------------------------------------------
ALTER TABLE monitors
    ADD COLUMN IF NOT EXISTS sla_target DECIMAL(5,2);

COMMENT ON COLUMN monitors.sla_target IS
    'Uptime percentage this monitor is held to, e.g. 99.90. NULL = no SLA.';

-- ---------------------------------------------------------------------------
-- report_templates
-- Which sections a report renders, in order.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS report_templates (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          VARCHAR(255) NOT NULL,
    is_default    BOOLEAN NOT NULL DEFAULT false,
    -- e.g. ["sla_compliance", "incident_summary", "charts", "custom"]
    sections_json JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- reports.template_id is NOT NULL, so at least one template must exist or no
-- report could ever be created. Seed the default that the UI starts from.
INSERT INTO report_templates (name, is_default, sections_json)
SELECT 'Standard Report', true,
       '["sla_compliance", "incident_summary", "charts"]'::jsonb
 WHERE NOT EXISTS (SELECT 1 FROM report_templates WHERE is_default);

-- ---------------------------------------------------------------------------
-- reports
-- A saved report definition. Generating one produces a report_generations row.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS reports (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name               VARCHAR(255) NOT NULL,
    template_id        UUID NOT NULL REFERENCES report_templates (id),
    -- scope_type: "monitors", "tags", or "groups"
    scope_type         VARCHAR(50) NOT NULL
                           CHECK (scope_type IN ('monitors', 'tags', 'groups')),
    -- scope_data: {"monitor_ids": [...]} | {"tags": [...]} | {"group_ids": [...]}
    scope_data         JSONB NOT NULL,
    time_range_days    INTEGER NOT NULL CHECK (time_range_days > 0),
    custom_title       VARCHAR(255),
    custom_description TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by         UUID NOT NULL REFERENCES users (id)
);

-- ---------------------------------------------------------------------------
-- report_generations
-- One rendered PDF. Kept as history so a shared link can resolve to the exact
-- artifact that was generated.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS report_generations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id    UUID NOT NULL REFERENCES reports (id) ON DELETE CASCADE,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    pdf_path     VARCHAR(512) NOT NULL,
    file_size    INTEGER,
    generated_by UUID NOT NULL REFERENCES users (id)
);

-- ---------------------------------------------------------------------------
-- report_access
-- Ownership and sharing. user_id is nullable so a row can represent a public
-- share token that is not tied to an account.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS report_access (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id   UUID NOT NULL REFERENCES reports (id) ON DELETE CASCADE,
    user_id     UUID REFERENCES users (id) ON DELETE CASCADE,
    access_type VARCHAR(50) NOT NULL
                    CHECK (access_type IN ('owner', 'viewer')),
    share_token VARCHAR(128) UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_reports_user_id              ON reports (user_id);
CREATE INDEX IF NOT EXISTS idx_report_generations_report_id ON report_generations (report_id);
CREATE INDEX IF NOT EXISTS idx_report_access_report_id      ON report_access (report_id);
CREATE INDEX IF NOT EXISTS idx_report_access_share_token    ON report_access (share_token);
