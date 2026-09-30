-- 048_metrics.sql
-- The generic metrics store (network monitoring phase 2): one row per measured
-- thing in metrics.series, one row per reading in the metrics.samples
-- hypertable, and 5-minute / hourly continuous aggregates for charts and
-- reports.
--
-- Nothing here has a foreign key into public. A config restore drops and
-- recreates public tables without CASCADE; a cross-schema FK would block that
-- and fail the restore halfway. Device deletion cleans up instead
-- (DeviceService.Delete queues the device's series in metrics.deleted_series
-- and the nightly NetworkMaintenance removes their samples).
--
-- The continuous aggregates are created WITH NO DATA: RunMigrations sends this
-- file as one multi-statement query, which Postgres runs as one implicit
-- transaction, and a continuous aggregate cannot be materialized inside one.
-- The refresh policies fill them.

CREATE TABLE IF NOT EXISTS metrics.series (
    id           BIGSERIAL PRIMARY KEY,
    device_id    UUID NOT NULL,
    metric       TEXT NOT NULL,
    instance     TEXT NOT NULL DEFAULT '',
    interface_id UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (device_id, metric, instance)
);
CREATE INDEX IF NOT EXISTS idx_series_interface ON metrics.series (interface_id) WHERE interface_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS metrics.samples (
    time      TIMESTAMPTZ NOT NULL,
    series_id BIGINT NOT NULL,
    value     DOUBLE PRECISION NOT NULL
);
SELECT create_hypertable('metrics.samples', by_range('time', INTERVAL '1 day'), if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS idx_samples_series_time ON metrics.samples (series_id, time DESC);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM timescaledb_information.hypertables
                   WHERE hypertable_schema = 'metrics' AND hypertable_name = 'samples' AND compression_enabled) THEN
        ALTER TABLE metrics.samples SET (
            timescaledb.compress,
            timescaledb.compress_segmentby = 'series_id',
            timescaledb.compress_orderby = 'time DESC'
        );
    END IF;
END $$;
SELECT add_compression_policy('metrics.samples', INTERVAL '2 days', if_not_exists => TRUE);
SELECT add_retention_policy('metrics.samples', INTERVAL '365 days', if_not_exists => TRUE);

-- Series whose samples the nightly cleanup still has to delete.
CREATE TABLE IF NOT EXISTS metrics.deleted_series (
    series_id  BIGINT PRIMARY KEY,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- min/max/sum/count rather than avg: an hourly average must be
-- sum(vsum)/sum(n), never an average of 5-minute averages.
CREATE MATERIALIZED VIEW IF NOT EXISTS metrics.samples_5m
WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
SELECT time_bucket(INTERVAL '5 minutes', time) AS bucket,
       series_id,
       min(value) AS vmin,
       max(value) AS vmax,
       sum(value) AS vsum,
       count(*)   AS n
FROM metrics.samples
GROUP BY bucket, series_id
WITH NO DATA;
SELECT add_continuous_aggregate_policy('metrics.samples_5m',
    start_offset => INTERVAL '3 hours', end_offset => INTERVAL '5 minutes',
    schedule_interval => INTERVAL '5 minutes', if_not_exists => TRUE);

CREATE MATERIALIZED VIEW IF NOT EXISTS metrics.samples_1h
WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
SELECT time_bucket(INTERVAL '1 hour', bucket) AS bucket,
       series_id,
       min(vmin) AS vmin,
       max(vmax) AS vmax,
       sum(vsum) AS vsum,
       sum(n)    AS n
FROM metrics.samples_5m
GROUP BY 1, series_id
WITH NO DATA;
SELECT add_continuous_aggregate_policy('metrics.samples_1h',
    start_offset => INTERVAL '2 days', end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour', if_not_exists => TRUE);
