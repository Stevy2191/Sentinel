-- 014_smtp_security.sql
-- Adds a selectable SMTP connection security mode to the email channel.
--
-- Until now EmailPlugin always opened a plaintext connection and *opportunistically*
-- upgraded with STARTTLS, so:
--   * implicit TLS (SMTPS, port 465) could never work - the server waits for a TLS
--     handshake that was never sent, and the connection just timed out; and
--   * a server that does not advertise STARTTLS silently stayed in cleartext, and
--     was then used to authenticate.
--
-- smtp_security is deliberately nullable: only the "email" row has any use for it,
-- and NULL resolves to 'starttls' at read time, so the column is never a source of
-- "unset" ambiguity.
--
--   'none'     -> plaintext (internal relays on port 25; no credentials sent)
--   'starttls' -> plaintext connect, then a REQUIRED upgrade (port 587)
--   'ssltls'   -> TLS handshake on connect, implicit TLS (port 465)

ALTER TABLE notification_configs
    ADD COLUMN IF NOT EXISTS smtp_security        VARCHAR(16),
    ADD COLUMN IF NOT EXISTS smtp_skip_tls_verify BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN notification_configs.smtp_security IS
    'SMTP connection security: none | starttls | ssltls (NULL = starttls)';
COMMENT ON COLUMN notification_configs.smtp_skip_tls_verify IS
    'Skip TLS certificate verification (self-signed internal mail servers); insecure';

-- Backfill from the port. A row on 465 was broken before this migration and starts
-- working; a row on 587 or 25 keeps behaving exactly as it does today.
UPDATE notification_configs
   SET smtp_security = CASE WHEN smtp_port = 465 THEN 'ssltls' ELSE 'starttls' END
 WHERE channel = 'email' AND smtp_security IS NULL;
