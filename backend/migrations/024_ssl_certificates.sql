-- SSL certificate expiry monitoring.
--
-- Separate from monitors on purpose. A monitor answers "is this service
-- responding right now" on a seconds-to-minutes cadence; a certificate answers
-- "will this stop working in N days" and moves once a day at most. Folding
-- certificates into monitors would put a daily, date-based check into a table
-- whose uptime percentages, incidents and response times all assume a fast
-- reachability probe.
CREATE TABLE IF NOT EXISTS ssl_certificates (
    -- UUID, not SERIAL: every other table here uses one, and a second identity
    -- scheme in one place is a trap for anything that handles ids generically.
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    domain VARCHAR(255) NOT NULL,
    -- Read from the certificate, so null until the first successful check.
    issuer      VARCHAR(255),
    expiry_date TIMESTAMPTZ,
    -- Derived from expiry_date at check time and stored so the list can be
    -- sorted and filtered by it without recomputing per row.
    days_until_expiry INTEGER,

    -- unknown until the first check completes, or if it keeps failing.
    status VARCHAR(50) NOT NULL DEFAULT 'unknown'
        CHECK (status IN ('unknown', 'valid', 'expiring_soon', 'expired')),

    -- Fixed at one day. Stored rather than hard-coded so the value the UI shows
    -- comes from the same place the job reads, but the API refuses to change it.
    check_interval INTEGER NOT NULL DEFAULT 86400,
    -- How many days before expiry to raise the alert.
    expiry_notification_days INTEGER NOT NULL DEFAULT 7
        CHECK (expiry_notification_days BETWEEN 1 AND 365),

    last_checked TIMESTAMPTZ,
    -- Why the last check failed: unreachable host, handshake failure, no
    -- certificate. Kept so a red row can explain itself instead of just
    -- reading "unknown".
    last_error TEXT,

    -- Alert bookkeeping. Without these the daily job would re-alert on every
    -- pass for the whole window before expiry.
    last_notified_at   TIMESTAMPTZ,
    last_notified_days INTEGER,

    enabled BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per domain. Case-insensitive because DNS is: adding Example.com when
-- example.com is already watched is a duplicate, not a second certificate.
CREATE UNIQUE INDEX IF NOT EXISTS idx_ssl_certificates_domain
    ON ssl_certificates (lower(domain));

-- The job scans for enabled rows that are due.
CREATE INDEX IF NOT EXISTS idx_ssl_certificates_due
    ON ssl_certificates (enabled, last_checked);

COMMENT ON COLUMN ssl_certificates.check_interval IS
    'Seconds between checks. Fixed at 86400 (one day); the API rejects changes.';
COMMENT ON COLUMN ssl_certificates.last_notified_days IS
    'days_until_expiry at the last alert, so the daily job alerts on change rather than every pass.';
