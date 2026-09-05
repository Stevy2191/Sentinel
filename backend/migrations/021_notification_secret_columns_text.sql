-- 015_notification_secret_columns_text.sql
-- Widen columns that now hold base64-encoded AES-GCM ciphertext (nonce + tag
-- + ciphertext) rather than raw plaintext, so a long original secret can't be
-- truncated by the old VARCHAR(255) limit. TEXT has no practical limit and no
-- performance cost in Postgres for values this size; webhook_url was already
-- TEXT and needs no change.
ALTER TABLE notification_configs
    ALTER COLUMN smtp_password TYPE TEXT,
    ALTER COLUMN telegram_bot_token TYPE TEXT,
    ALTER COLUMN ntfy_auth_token TYPE TEXT;
