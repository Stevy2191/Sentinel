-- 007_ntfy_auth_token.sql
-- Adds an optional auth token for ntfy channels (protected topics / self-hosted
-- servers that require "Authorization: Bearer <token>"). A separate migration is
-- used rather than editing 006 because that migration has already been applied
-- on existing databases; the runner tracks applied files by name and will not
-- re-run an edited one. IF NOT EXISTS keeps this safe on fresh installs too.

ALTER TABLE notification_configs
    ADD COLUMN IF NOT EXISTS ntfy_auth_token VARCHAR(255);
