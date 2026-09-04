-- 020_audit_log.sql
-- Records who changed what, and when, for reports, schedules and share links.
--
-- Two decisions worth stating, because both are easy to get backwards:
--
-- user_id is ON DELETE SET NULL, not CASCADE. An audit trail that disappears
-- when the account is deleted is not an audit trail - removing a user would
-- erase the record of everything they did. username is denormalised alongside
-- it so the entry still names an actor after the account is gone.
--
-- There is no foreign key on resource_id. Entries outlive the rows they
-- describe: "report_deleted" is precisely the case where the report no longer
-- exists, and a constraint would make that record impossible to keep.

CREATE TABLE IF NOT EXISTS audit_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID REFERENCES users (id) ON DELETE SET NULL,
    -- Snapshot of the actor's name at the time of the action.
    username      VARCHAR(64),
    -- e.g. report_created, schedule_updated, share_link_revoked
    action        VARCHAR(64) NOT NULL,
    resource_type VARCHAR(32) NOT NULL,
    resource_id   UUID,
    -- Free-form detail: {"before": {...}, "after": {...}} for updates, or a
    -- flat summary for creates and deletes. Never contains secrets.
    changes       JSONB,
    ip_address    VARCHAR(64),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_created_at    ON audit_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_user_id       ON audit_log (user_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_resource      ON audit_log (resource_type, resource_id);
