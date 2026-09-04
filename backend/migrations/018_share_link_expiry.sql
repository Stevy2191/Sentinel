-- 018_share_link_expiry.sql
-- Gives report share links an expiry, and makes them revocable.
--
-- Until now a share token was permanent: anyone who obtained the link kept
-- access forever, and the only way to withdraw it was to delete the row by
-- hand. expires_at bounds that, and a row can now be deleted through the API.
--
-- NULL means "never expires", which is what every existing token becomes. That
-- is deliberate: silently expiring links that are already in circulation would
-- break working access with no warning. New links get an expiry by default at
-- the API layer instead.

ALTER TABLE report_access
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

COMMENT ON COLUMN report_access.expires_at IS
    'When this share link stops working. NULL = never expires.';

-- Token lookups filter on expiry, so the partial index covers the hot path:
-- resolving a share token that has not yet lapsed.
CREATE INDEX IF NOT EXISTS idx_report_access_share_token_active
    ON report_access (share_token)
    WHERE share_token IS NOT NULL;
