-- 001_initial_schema.sql
-- Sentinel initial database schema.
--
-- Creates the core tables for uptime monitoring: monitors and their check
-- history, incidents derived from downtime, notification delivery records,
-- public status pages, and API tokens for programmatic access.
--
-- Requires PostgreSQL 13+ for gen_random_uuid() (provided by pgcrypto, which is
-- built in from PostgreSQL 13 onward). The extension is created defensively
-- below in case an older server or a custom build does not expose it.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------------------------------------------------------------------------
-- monitors
-- One row per monitored endpoint. Holds the check configuration (type, URL,
-- schedule, timeouts) alongside a denormalized snapshot of the latest result
-- (current_status, last_check_at, last_response_time_ms) for fast dashboard
-- reads without scanning the checks history.
-- ---------------------------------------------------------------------------
CREATE TABLE monitors (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  VARCHAR(255) NOT NULL,
    description           TEXT,
    type                  VARCHAR(50) NOT NULL
                              CHECK (type IN ('http', 'tcp', 'ping', 'dns', 'webhook')),
    url                   VARCHAR(2048) NOT NULL,
    method                VARCHAR(10) DEFAULT 'GET',
    headers               JSONB,
    body                  TEXT,
    interval_seconds      INTEGER DEFAULT 60,
    timeout_seconds       INTEGER DEFAULT 10,
    retries               INTEGER DEFAULT 0,
    current_status        VARCHAR(50) DEFAULT 'unknown'
                              CHECK (current_status IN ('online', 'offline', 'unknown')),
    last_check_at         TIMESTAMP,
    last_response_time_ms INTEGER,
    enabled               BOOLEAN DEFAULT true,
    tags                  JSONB,
    created_at            TIMESTAMP DEFAULT NOW(),
    updated_at            TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_monitors_enabled ON monitors (enabled);

-- ---------------------------------------------------------------------------
-- checks
-- Append-only history of every check performed against a monitor. This is the
-- highest-volume table and the source of truth for uptime reporting and SLA
-- calculations. Indexed for time-ordered lookups per monitor and globally.
-- ---------------------------------------------------------------------------
CREATE TABLE checks (
    id               BIGSERIAL PRIMARY KEY,
    monitor_id       UUID NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    status           VARCHAR(50) NOT NULL
                         CHECK (status IN ('success', 'failed', 'timeout')),
    response_time_ms INTEGER,
    status_code      INTEGER,
    error_message    TEXT,
    timestamp        TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_checks_monitor_timestamp ON checks (monitor_id, timestamp DESC);
CREATE INDEX idx_checks_timestamp ON checks (timestamp DESC);
CREATE INDEX idx_checks_status ON checks (status);

-- ---------------------------------------------------------------------------
-- incidents
-- A period of downtime for a monitor, opened when it goes offline and closed
-- when it recovers. Stores duration and human-authored context (severity, root
-- cause, notes) for post-incident reporting.
-- ---------------------------------------------------------------------------
CREATE TABLE incidents (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    monitor_id       UUID NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    start_time       TIMESTAMP NOT NULL,
    end_time         TIMESTAMP,
    duration_seconds INTEGER,
    severity         VARCHAR(50)
                         CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    root_cause       TEXT,
    notes            TEXT,
    created_at       TIMESTAMP DEFAULT NOW(),
    updated_at       TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_incidents_monitor_start ON incidents (monitor_id, start_time DESC);

-- ---------------------------------------------------------------------------
-- notifications
-- Delivery record for each alert dispatched over a channel. Optionally linked
-- to the incident that triggered it; the link is nulled if that incident is
-- later deleted, preserving the delivery audit trail.
-- ---------------------------------------------------------------------------
CREATE TABLE notifications (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    monitor_id    UUID NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    incident_id   UUID REFERENCES incidents (id) ON DELETE SET NULL,
    channel       VARCHAR(50) NOT NULL
                      CHECK (channel IN ('email', 'slack', 'discord', 'ntfy', 'telegram', 'webhook')),
    status        VARCHAR(50) NOT NULL
                      CHECK (status IN ('pending', 'sent', 'failed')),
    error_message TEXT,
    sent_at       TIMESTAMP,
    created_at    TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_notifications_monitor ON notifications (monitor_id);

-- ---------------------------------------------------------------------------
-- status_pages
-- A public, shareable page addressed by a unique slug. Groups a set of
-- monitors (via status_page_monitors) into a branded uptime view.
-- ---------------------------------------------------------------------------
CREATE TABLE status_pages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        VARCHAR(255) UNIQUE NOT NULL,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    logo_url    VARCHAR(2048),
    theme_color VARCHAR(7),
    published   BOOLEAN DEFAULT true,
    created_at  TIMESTAMP DEFAULT NOW(),
    updated_at  TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_status_pages_slug ON status_pages (slug);

-- ---------------------------------------------------------------------------
-- status_page_monitors
-- Join table mapping monitors onto status pages, with optional grouping and
-- ordering for display. A monitor may appear on a page at most once.
-- ---------------------------------------------------------------------------
CREATE TABLE status_page_monitors (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status_page_id UUID NOT NULL REFERENCES status_pages (id) ON DELETE CASCADE,
    monitor_id     UUID NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    group_name     VARCHAR(255),
    position       INTEGER,
    created_at     TIMESTAMP DEFAULT NOW(),
    UNIQUE (status_page_id, monitor_id)
);

-- ---------------------------------------------------------------------------
-- api_tokens
-- Hashed API credentials for the REST API. Only the SHA-256 hash of a token is
-- stored; the plaintext is shown once at creation. Supports optional scopes and
-- expiry, and tracks last use.
-- ---------------------------------------------------------------------------
CREATE TABLE api_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   VARCHAR(64) UNIQUE NOT NULL,
    name         VARCHAR(255),
    scopes       JSONB,
    last_used_at TIMESTAMP,
    expires_at   TIMESTAMP,
    created_at   TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_api_tokens_token_hash ON api_tokens (token_hash);
