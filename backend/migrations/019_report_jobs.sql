-- 019_report_jobs.sql
-- Moves PDF rendering off the request path.
--
-- Generation aggregates a time range, renders a document, and writes a file.
-- Doing that inside the HTTP handler ties up a connection for the duration and
-- puts an unbounded operation in front of the caller. A job row lets the API
-- accept the request immediately and a worker do the work.
--
-- generation_id is the result: it is set when the job succeeds and stays NULL
-- otherwise, so "did this produce anything" is one nullable column rather than
-- a status string that has to be trusted.

CREATE TABLE IF NOT EXISTS report_jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id     UUID NOT NULL REFERENCES reports (id) ON DELETE CASCADE,
    requested_by  UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status        VARCHAR(20) NOT NULL DEFAULT 'queued'
                      CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    -- Set on success. ON DELETE SET NULL so pruning generations does not take
    -- the job history with it.
    generation_id UUID REFERENCES report_generations (id) ON DELETE SET NULL,
    -- Operator-facing failure reason. The full error goes to the log.
    error         TEXT,
    attempts      INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ
);

-- The worker's claim query orders queued jobs by age, so this index covers the
-- only hot path. Partial, because finished jobs are never scanned for.
CREATE INDEX IF NOT EXISTS idx_report_jobs_queued
    ON report_jobs (created_at)
    WHERE status = 'queued';

CREATE INDEX IF NOT EXISTS idx_report_jobs_report_id ON report_jobs (report_id);
CREATE INDEX IF NOT EXISTS idx_report_jobs_status    ON report_jobs (status);
