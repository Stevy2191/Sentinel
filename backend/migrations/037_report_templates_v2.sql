-- Report types built on the richer sections.
--
-- "Performance Report" is corrected here. It was seeded against the "charts"
-- section, which despite the name has always drawn the summary tiles — so a
-- report asking for performance rendered four tiles and no performance data.
-- It now uses the real performance section.
UPDATE report_templates
   SET sections_json = '["performance"]'::jsonb
 WHERE name = 'Performance Report'
   AND sections_json = '["charts"]'::jsonb;

-- The report to reach for when the question is "how did last month go".
INSERT INTO report_templates (name, is_default, sections_json)
SELECT 'Full Review', false,
       '["executive_summary", "availability_breakdown", "timeline", "performance", "sla_compliance"]'::jsonb
 WHERE NOT EXISTS (SELECT 1 FROM report_templates WHERE name = 'Full Review');

-- Headline figures and their movement, for someone who wants one page.
INSERT INTO report_templates (name, is_default, sections_json)
SELECT 'Executive Summary', false,
       '["executive_summary", "availability_breakdown"]'::jsonb
 WHERE NOT EXISTS (SELECT 1 FROM report_templates WHERE name = 'Executive Summary');

-- What happened, in the order it happened.
INSERT INTO report_templates (name, is_default, sections_json)
SELECT 'Incident Timeline', false,
       '["executive_summary", "timeline"]'::jsonb
 WHERE NOT EXISTS (SELECT 1 FROM report_templates WHERE name = 'Incident Timeline');
