-- A comment thread on an incident.
--
-- Incidents already carried root_cause, notes and resolution_notes: three
-- single text fields that each overwrite on save. Investigating an outage is
-- not a single statement — observations arrive over time, from different
-- people, and "what did we know at 03:00" is exactly the question a postmortem
-- asks. One field cannot answer it, because every edit destroys the last one.
--
-- The existing fields are left alone. They are the summary; this is the record
-- of how the summary was arrived at.
CREATE TABLE IF NOT EXISTS incident_comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    -- The author. Kept when the account is deleted rather than removing the
    -- comment: the incident record should not develop holes because someone
    -- left. author_name preserves who wrote it once the user row is gone.
    user_id     UUID REFERENCES users (id) ON DELETE SET NULL,
    author_name VARCHAR(255) NOT NULL,
    body        TEXT NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),

    CONSTRAINT incident_comments_body_not_blank CHECK (btrim(body) <> '')
);

-- The thread is always read oldest-first for one incident.
CREATE INDEX IF NOT EXISTS idx_incident_comments_incident
    ON incident_comments (incident_id, created_at);
