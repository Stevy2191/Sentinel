-- 010_monitor_ownership_and_sharing.sql
-- Give monitors an owner and support granular per-user sharing.

ALTER TABLE monitors
    ADD COLUMN owner_id UUID REFERENCES users(id) ON DELETE CASCADE;

CREATE TABLE monitor_sharing (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    monitor_id          UUID NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    shared_with_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission          VARCHAR(50) NOT NULL DEFAULT 'readonly', -- 'readonly' | 'editable'
    shared_by_user_id   UUID NOT NULL REFERENCES users(id),      -- who shared it
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (monitor_id, shared_with_user_id)
);

CREATE INDEX idx_monitor_sharing_monitor_id ON monitor_sharing(monitor_id);
CREATE INDEX idx_monitor_sharing_user_id ON monitor_sharing(shared_with_user_id);
CREATE INDEX idx_monitors_owner_id ON monitors(owner_id);

-- Backfill: assign pre-existing monitors to the first (oldest) user so they stay
-- visible after ownership filtering. No-op on a fresh install (no users yet).
UPDATE monitors
SET owner_id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1)
WHERE owner_id IS NULL;
