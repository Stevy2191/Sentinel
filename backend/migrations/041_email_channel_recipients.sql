-- A destination address for the email notification channel.
--
-- An email channel had no recipient of its own: NewEmailPluginFromConfig
-- always fell back to mailing the configured account itself
-- (smtp_from/smtp_user), silently. That was invisible as long as an install
-- held one email channel, but multiple channels are now supported and each
-- needs to say who it actually alerts - "IT team" and "Managers" cannot both
-- be "whoever the SMTP account belongs to".
--
-- Comma-separated, matching the existing SMTP_TO environment variable's
-- convention (see notifications.parseRecipients).
ALTER TABLE notification_configs
    ADD COLUMN IF NOT EXISTS smtp_to TEXT;
