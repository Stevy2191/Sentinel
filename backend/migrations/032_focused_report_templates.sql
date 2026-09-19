-- Two more single-section report templates.
--
-- Reports can now be generated from a monitor's own page, where the question is
-- usually narrow: "how much downtime did this have last month" or "is it getting
-- slower". The existing choice was the standard report, which answers both plus
-- SLA compliance, or the incident report. These give the other two sections the
-- same treatment, so a generated report contains what was asked for and nothing
-- else.
--
-- Seeded by name so re-running is a no-op, and is_default is left alone: the
-- standard report stays the default.

INSERT INTO report_templates (name, is_default, sections_json)
SELECT 'Uptime & SLA Report', false, '["sla_compliance"]'::jsonb
 WHERE NOT EXISTS (SELECT 1 FROM report_templates WHERE name = 'Uptime & SLA Report');

INSERT INTO report_templates (name, is_default, sections_json)
SELECT 'Performance Report', false, '["charts"]'::jsonb
 WHERE NOT EXISTS (SELECT 1 FROM report_templates WHERE name = 'Performance Report');
