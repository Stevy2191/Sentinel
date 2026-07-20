-- 005_settings.sql
-- A simple key/value store for runtime-adjustable application settings that must
-- persist across restarts (e.g. whether new-user registration is open). The
-- initial value of each setting is seeded from the environment on first startup;
-- afterwards an admin can change it via the API and the stored value wins.

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
