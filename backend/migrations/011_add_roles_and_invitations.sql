-- 011_add_roles_and_invitations.sql
-- Adds a role column (kept in sync with the existing is_admin flag) and an
-- invitations table for admin-driven onboarding.

ALTER TABLE users
    ADD COLUMN role VARCHAR(50) NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user'));

-- Backfill role from the existing is_admin flag.
UPDATE users SET role = CASE WHEN is_admin THEN 'admin' ELSE 'user' END;

CREATE TABLE invitations (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email              VARCHAR(255) NOT NULL UNIQUE,
    token              VARCHAR(255) NOT NULL UNIQUE,
    role               VARCHAR(50) NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    -- If the inviter is deleted, their pending invitations go with them.
    invited_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    accepted           BOOLEAN NOT NULL DEFAULT false,
    accepted_at        TIMESTAMPTZ,
    expires_at         TIMESTAMPTZ NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_invitations_token ON invitations(token);
CREATE INDEX idx_invitations_email ON invitations(email);
CREATE INDEX idx_users_role ON users(role);
