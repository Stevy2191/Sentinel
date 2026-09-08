-- ---------------------------------------------------------------------------
-- 028_ssl_certificate_details
-- Records the rest of what a handshake already tells us about a certificate,
-- so the detail view can show it without re-reading the certificate on open.
--
-- Every column is nullable: rows created before this migration have never had
-- these values, and a check that fails leaves them alone rather than blanking
-- what the last successful read found.
-- ---------------------------------------------------------------------------

ALTER TABLE ssl_certificates
    -- The subject the certificate was issued to, and the issuer's own common
    -- name. Issuer already holds the issuing organisation, which is the more
    -- useful label; the CN is kept alongside because it is what openssl prints
    -- and what people compare against.
    ADD COLUMN IF NOT EXISTS subject_common_name VARCHAR(255),
    ADD COLUMN IF NOT EXISTS issuer_common_name  VARCHAR(255),

    -- Stored as text, not a number. A serial is a big integer up to 20 octets
    -- and does not fit a bigint; JSON consumers that parse it as a number lose
    -- precision and render it in scientific notation.
    ADD COLUMN IF NOT EXISTS serial_number VARCHAR(128),

    -- Signature algorithm, plus the public key type and size, e.g.
    -- "ECDSA-SHA256" and "ECDSA 256-bit".
    ADD COLUMN IF NOT EXISTS signature_algorithm VARCHAR(64),
    ADD COLUMN IF NOT EXISTS public_key_algorithm VARCHAR(64),

    -- Every name the certificate is valid for.
    ADD COLUMN IF NOT EXISTS subject_alternative_names JSONB,

    -- The start of the validity window. The end is already stored as
    -- expiry_date, which the status and alerting are derived from.
    ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ,

    -- The address the check actually connected to. Worth recording: where an
    -- internal resolver answers for a public domain, this is what shows the
    -- check reached the wrong host.
    ADD COLUMN IF NOT EXISTS resolved_ip VARCHAR(45);
