-- Per-monitor TLS certificate verification for HTTP checks.
--
-- Until now the HTTP checker always verified, with no way to opt out. That is
-- the right default and stays the default here, but it makes a monitor pointed
-- at a host with a self-signed or internal-CA certificate permanently fail for
-- a reason unrelated to whether the service is actually up — a real situation
-- on the home-lab and internal networks this tool is aimed at.
--
-- NOT NULL DEFAULT TRUE means every existing monitor keeps verifying, and the
-- setting has to be turned off deliberately, per monitor.
ALTER TABLE monitors
    ADD COLUMN IF NOT EXISTS ssl_verify BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN monitors.ssl_verify IS
    'Verify the TLS certificate on HTTPS checks. Only consulted for type=http.';
