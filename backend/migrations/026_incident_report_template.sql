-- Incident reporting: a report scoped to a monitor check type, and a template
-- that renders incidents alone.

-- 1. Allow scoping a report to a check type.
--
-- Distinct from tags: a tag is something a person applied and has to keep
-- applying, a type is what the monitor inherently is. "Every DNS monitor"
-- resolves at generation time, so a report defined once picks up monitors
-- added later without anyone remembering to tag them.
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_scope_type_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_scope_type_check
    CHECK (scope_type IN ('monitors', 'tags', 'groups', 'types'));

-- 2. An incident-only report template.
--
-- The only template was "Standard Report", which renders SLA compliance,
-- incidents and charts together. Asking "what went wrong with this group last
-- quarter" through it means reading past two sections that answer something
-- else. This one renders the incident section alone.
--
-- Seeded by name so re-running is a no-op, and is_default is left alone: the
-- standard report stays the default.
INSERT INTO report_templates (name, is_default, sections_json)
SELECT 'Incident Report', false, '["incident_summary"]'::jsonb
 WHERE NOT EXISTS (SELECT 1 FROM report_templates WHERE name = 'Incident Report');
