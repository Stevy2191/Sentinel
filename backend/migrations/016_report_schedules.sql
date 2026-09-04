-- 016_report_schedules.sql
-- Scheduled delivery: a report definition plus a cadence and a recipient list.
--
-- Deviations from the feature spec:
--   * TIMESTAMPTZ, not TIMESTAMP (see 002 - this schema was deliberately moved
--     off bare TIMESTAMP after timezone-offset bugs).
--   * schedule_type and the recipient list are constrained here as well as in
--     the model, so a row written outside the API cannot make the scheduler
--     misbehave.

CREATE TABLE IF NOT EXISTS report_schedules (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id          UUID NOT NULL REFERENCES reports (id) ON DELETE CASCADE,
    user_id            UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    schedule_type      VARCHAR(50) NOT NULL
                           CHECK (schedule_type IN ('daily', 'weekly', 'monthly', 'custom')),
    -- Only meaningful when schedule_type = 'custom'.
    cron_expression    VARCHAR(255),
    -- ["ops@example.com", ...] - must be a non-empty JSON array.
    email_recipients   JSONB NOT NULL
                           CHECK (jsonb_typeof(email_recipients) = 'array'
                                  AND jsonb_array_length(email_recipients) > 0),
    send_as_attachment BOOLEAN NOT NULL DEFAULT true,
    -- {"include_link": true, "include_summary": true}
    include_in_email   JSONB,
    last_run_at        TIMESTAMPTZ,
    next_run_at        TIMESTAMPTZ,
    is_active          BOOLEAN NOT NULL DEFAULT true,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- A custom cadence is meaningless without an expression to run it on.
    CONSTRAINT report_schedules_custom_needs_expression
        CHECK (schedule_type <> 'custom' OR cron_expression IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_report_schedules_report_id   ON report_schedules (report_id);
CREATE INDEX IF NOT EXISTS idx_report_schedules_next_run_at ON report_schedules (next_run_at);
CREATE INDEX IF NOT EXISTS idx_report_schedules_is_active   ON report_schedules (is_active);
