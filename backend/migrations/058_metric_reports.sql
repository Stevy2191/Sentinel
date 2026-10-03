-- 058_metric_reports.sql
-- Network phase 5: a third report type, metrics, which covers ports, port
-- roles at sites, devices or sites instead of monitors. Both CHECKs are
-- dropped and re-created with the new values (report_type from 043,
-- scope_type from 026). No existing row changes. Which scope types go with
-- which report type is Report.Validate's job: a CHECK tying the two columns
-- together would only repeat it.
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_report_type_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_report_type_check
    CHECK (report_type IS NOT NULL AND report_type IN ('uptime', 'incident', 'metrics'));

ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_scope_type_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_scope_type_check
    CHECK (scope_type IS NOT NULL AND scope_type IN ('monitors', 'tags', 'groups', 'types',
                                                     'ports', 'port_roles', 'devices', 'sites'));
