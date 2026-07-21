-- 009_user_theme_preferences.sql
-- Persist each user's theme so it syncs across their devices. Existing rows are
-- backfilled with the defaults by the column DEFAULT clauses.

ALTER TABLE users
    ADD COLUMN theme_primary_color VARCHAR(7)  NOT NULL DEFAULT '#10b981',
    ADD COLUMN theme_accent_color  VARCHAR(7)  NOT NULL DEFAULT '#f59e0b',
    ADD COLUMN theme_mode          VARCHAR(20) NOT NULL DEFAULT 'auto';
