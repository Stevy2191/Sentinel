-- 003_users.sql
-- Adds the users table backing authentication (username/password, JWT sessions,
-- and optional TOTP MFA). Timestamps use TIMESTAMPTZ (see migration 002).

CREATE TABLE users (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username         VARCHAR(32) NOT NULL,
    email            VARCHAR(255),
    password_hash    VARCHAR(255) NOT NULL,
    mfa_enabled      BOOLEAN NOT NULL DEFAULT false,
    mfa_secret       TEXT,
    mfa_backup_codes JSONB,
    is_admin         BOOLEAN NOT NULL DEFAULT true,
    last_login       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Case-insensitive unique usernames.
CREATE UNIQUE INDEX idx_users_username_lower ON users (LOWER(username));

-- Unique emails when provided (multiple users may omit an email).
CREATE UNIQUE INDEX idx_users_email ON users (email) WHERE email IS NOT NULL AND email <> '';
