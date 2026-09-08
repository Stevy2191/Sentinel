-- Domain registration expiry, notification routing, and a resolver escape hatch.
--
-- The page is "SSL & Domain Management" but only ever tracked certificates. A
-- certificate and a registration are different clocks: a domain can have a
-- freshly renewed certificate and still lapse at the registrar in three weeks,
-- at which point the certificate stops mattering. Both live on the same row
-- because both answer "is this domain going to keep working", and an operator
-- wants them side by side.
ALTER TABLE ssl_certificates
    -- The registrable domain the registration belongs to. A certificate may be
    -- watched for sub.example.com, but registration is always example.com, so
    -- the two are stored separately rather than one being derived on read.
    ADD COLUMN IF NOT EXISTS registrable_domain VARCHAR(255),
    ADD COLUMN IF NOT EXISTS registrar VARCHAR(255),
    ADD COLUMN IF NOT EXISTS domain_expiry_date TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS domain_days_until_expiry INTEGER,
    ADD COLUMN IF NOT EXISTS domain_status VARCHAR(50) NOT NULL DEFAULT 'unknown'
        CHECK (domain_status IN ('unknown', 'valid', 'expiring_soon', 'expired')),
    ADD COLUMN IF NOT EXISTS registration_checked_at TIMESTAMPTZ,
    -- Why the registration lookup failed. Some registries do not publish RDAP,
    -- so an empty result is normal rather than a fault, and the row says which.
    ADD COLUMN IF NOT EXISTS registration_error TEXT,

    -- Which channels this domain's expiry alerts go to. NULL means every
    -- enabled channel, matching how monitors read the same field; an empty
    -- array means alert nowhere.
    ADD COLUMN IF NOT EXISTS notify_channels JSONB,

    -- Alert bookkeeping for the registration clock, kept separate from the
    -- certificate's so one going quiet does not silence the other.
    ADD COLUMN IF NOT EXISTS domain_last_notified_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS domain_last_notified_days INTEGER;

COMMENT ON COLUMN ssl_certificates.registrable_domain IS
    'The domain the registration belongs to, e.g. example.com for sub.example.com.';
COMMENT ON COLUMN ssl_certificates.notify_channels IS
    'Channel ids for expiry alerts. NULL = every enabled channel, [] = none.';
