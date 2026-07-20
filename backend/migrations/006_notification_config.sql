-- 006_notification_config.sql
-- Persists per-channel notification settings so channels can be configured from
-- the admin UI (instead of only environment variables). One row per channel.
-- Secrets (SMTP password, Telegram bot token, webhook URLs) live here; the API
-- layer strips them from list responses.

CREATE TABLE notification_configs (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel  VARCHAR(50) NOT NULL,
    -- channel: "email", "slack", "discord", "telegram", "ntfy", "webhook"
    -- Uniqueness enforced by idx_notification_config_channel below.

    enabled  BOOLEAN NOT NULL DEFAULT false,

    -- Email/SMTP
    smtp_host     VARCHAR(255),
    smtp_port     INTEGER,
    smtp_user     VARCHAR(255),
    smtp_password VARCHAR(255),
    smtp_from     VARCHAR(255),

    -- Slack/Discord/Webhook/Ntfy (generic URL storage)
    webhook_url TEXT,

    -- Telegram specific
    telegram_bot_token VARCHAR(255),
    telegram_chat_id   VARCHAR(255),

    -- Ntfy specific
    ntfy_url   VARCHAR(255),
    ntfy_topic VARCHAR(255),

    -- Generic webhook headers (JSONB for flexibility)
    custom_headers JSONB,

    -- Connection test status
    last_test_at      TIMESTAMPTZ,
    last_test_success BOOLEAN,
    last_test_error   TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The UNIQUE on channel already creates an index; keep an explicitly named one
-- to match the spec and document intent (only one config row per channel).
CREATE UNIQUE INDEX idx_notification_config_channel ON notification_configs (channel);
