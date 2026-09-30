# Network Monitoring Phase 2 (Ports and Bandwidth) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every minute, Sentinel reads each switch port's traffic, errors and link state, stores rates long-term in a generic TimescaleDB metrics model, logs port events, raises incidents for important ports, and draws a live virtual faceplate on the device page.

**Architecture:** A pure `internal/portmon` package holds all port logic: counter→rate, interface classification, device type, faceplate layout and a per-port `Tracker` for events and conditions. The phase 1 `DevicePoller` hands each successful poll of an up device to a `PortMonitor`, which reads stats (`snmp.ReadStats`), computes rates, writes samples through `MetricsStore` (series + hypertable + 5m/1h continuous aggregates in the `metrics` schema), persists port state and events through `PortService`, and opens and closes port incidents through `IncidentService`. The API adds port, event, details, metrics-query and network-settings endpoints. The frontend reworks the device page around a faceplate and adds a port page.

**Tech Stack:** Go 1.26, Gin, GORM (pgx), PostgreSQL 16 + TimescaleDB 2.30.1 (TSL build), gosnmp; React + TypeScript + Vite + Tailwind + Recharts; Docker; snmpsim.

**Spec:** `docs/superpowers/specs/2026-09-30-network-phase2-ports-bandwidth-design.md`

## Global Constraints

- Branch `feature/network-phase2`, worktree `/home/sysadmin/sentinel-phase2`. Every path in this plan is relative to that worktree. `/home/sysadmin/sentinel` is the **live** checkout: never check out another branch there, never deploy live.
- Migrations: `048_metrics.sql`, `049_port_monitoring.sql` (highest existing: `047_device_incidents.sql`). Never edit an applied migration.
- Every new timestamp column is `TIMESTAMPTZ`.
- Metrics live in the `metrics` schema and never hold a foreign key into `public`. Everything else new is a table in `public`, because backups dump `--table='public.*'`.
- API routes are under `/api/v1`. A device, port, site or event the caller cannot see answers **404**. Reading needs readonly site access; changing needs editable access; network settings are admin-only.
- Metric keys are exactly: `if_in_bps`, `if_out_bps`, `if_in_util_pct`, `if_out_util_pct`, `if_in_errors_pm`, `if_out_errors_pm`, `if_in_discards_pm`, `if_out_discards_pm`, `if_speed_bps`.
- Defaults (settings):
  - `metrics_raw_retention_days` = 365 (7–3650);
  - `port_error_threshold_per_min` = 10 (1–1000000);
  - `port_util_threshold_pct` = 80 (10–100), clearing at 10 points below;
  - `port_down_grace_seconds` = 120 (0–86400).
- Fixed rules:
  - condition window = 5 polls;
  - flapping = 3 or more link transitions in 10 minutes, ending after 10 quiet minutes;
  - usual speed = the most common speed over 7 days, and only once there are 24 h of history.
- Device types are exactly `switch`, `router`, `access_point`, `nvr`, `other`. Port conditions are exactly `link_down`, `errors`, `flapping`, `slow_link`, `saturated`.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` (or the trailer naming the model that actually wrote the commit).
- Go checks: `cd backend && go vet ./... && go test ./...`, and `make test-db` for database tests. Plain `go test ./...` stays database- and network-free.
- Frontend checks run in Docker, since there is no Node on the host. Run as uid 1000 so `node_modules` stays owned by `sysadmin`:
  `docker run --rm --user 1000:1000 -e HOME=/tmp -v /home/sysadmin/sentinel-phase2/frontend:/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`
- The frontend follows the design system: slate/Tailwind, `card`, `btn-primary`, `btn-secondary`, the literal Tailwind class strings in `utils/colors.ts` style. Status colours are emerald (in use), yellow (problem), red (important down), slate (no link / disabled).

## Plan decisions (read before starting)

Made while planning against the code. Each is a narrow reading of the spec, not a change of intent:

1. **The stats poll rides the phase 1 poll job.** `PollOnce` calls `PortMonitor.PollStats` after a successful poll that leaves the device `up`. So stats run at the device's `poll_interval` (default 60 s), and the spreading comes from the poller's own `last_polled_at` scheduling rather than a separate per-device offset.
2. **Route paths use the existing groups:**
   - `/devices/:id/ports`, `/devices/:id/ports/:ifIndex`, `/devices/:id/events`, `/devices/:id/details`;
   - `/sites/:id/port-events`, `/sites/:id/ports/summary`;
   - `/network/metrics/query` and `/network/settings` in a new `/network` group.
   The spec's `/network/devices/...` spelling does not match phase 1's actual `/devices` group.
3. **JSONB, not arrays.** `conditions`, `conditions_since` and `faceplate_sfp_ports` are JSONB (the codebase's existing JSON column pattern) rather than `TEXT[]`/`INTEGER[]`.
4. **Deleted-series queue.** Deleting a device queues its series ids in `metrics.deleted_series`, and the nightly cleanup deletes those series' samples by id. A nightly anti-join over a year of samples would not scale. Rollup rows of deleted series stay behind; nothing can reach them.
5. **No flapping simulator port.** snmpsim cannot schedule link-state changes. The simulator gains increasing counters, and the flap path is covered by the `Tracker` table tests plus a scripted-SNMP database test of `PortMonitor` (Task 8).
6. **Poll timing is measured in Task 15.** Nothing exists to time before the stats poll does. Task 15 records `last_stats_duration_ms` on the user's real switch and decides whether `snmp_poll_workers` (default 16) needs changing.
7. **`slow_link` is also a span kind in `port_events`,** so the port page history shows it next to `errors`/`saturated`/`flapping`.
8. **Port incidents keep `subject_type = 'device'`.** Their `subject_name` reads "Device · 0/51 (Alias)", so the existing incident pages render them without changes. They also carry `port_if_index`.
9. **Network settings get a "Settings" entry in the Network sidebar** (admin-only), and the device list shows effective (overridden) vendor/model.

## Review Focus

1. **A port incident must never be mistaken for the device's own incident.** That means `OpenDeviceIncident`, `CloseDeviceIncident` and the 30-day availability all ignore rows with `interface_id`. Test: `TestDBPortIncidentsDoNotAffectDeviceIncident` (Task 6).
2. **A counter reset, reboot or implausible jump must never become a traffic spike.** Test: `TestComputeRates` (Task 2), `TestPortMonitorRebootWritesNoRates` (Task 8).
3. **A config restore must succeed with metrics present,** so nothing in `metrics` may depend on `public`. Tests: `TestDBMetricsHaveNoForeignKeys` (Task 1), `TestDBRestoreWithMetrics` (Task 10).
4. **A member must not read another site's metrics, ports or events,** including through `/network/metrics/query?device_id=<foreign>`. That answers 404. Test: `TestMetricsQueryAccess` (Task 9).
5. **A restart must neither end nor re-open an active condition or incident.** Tests: `TestTrackerRestoredDoesNotRestartOrEnd` (Task 4), `TestPortMonitorRestartKeepsIncident` (Task 8).

---

## File Structure

Backend (new unless marked):
- `backend/migrations/048_metrics.sql`: `metrics.series`, `metrics.samples` hypertable, `metrics.deleted_series`, `samples_5m`/`samples_1h` continuous aggregates, policies.
- `backend/migrations/049_port_monitoring.sql`: interface/device columns, `port_events`, incident/notification port columns.
- `backend/internal/models/snmp.go` (modify): device types, new `Device`/`DeviceInterface` fields, `PortEvent`, `PortPatch`, `DeviceDetailsPatch`.
- `backend/internal/models/jsontypes.go`: `ConditionSet`, `TimeMap`, `IntSlice`, `JSONMap`, `Opt[T]`.
- `backend/internal/models/monitor.go` (modify): `Incident.InterfaceID`, `Incident.Condition`, `Notification.InterfaceID`.
- `backend/internal/models/setting.go` (modify): network setting keys and bounds.
- `backend/internal/database/timescale.go` (modify): TSL licence check.
- `backend/scripts/test-db.sh` (modify): export the container name.
- `backend/internal/portmon/`: `rates.go`, `classify.go`, `devicetype.go`, `layout.go`, `usual.go`, `tracker.go` (+ tests, `fixtures_test.go`).
- `backend/internal/snmp/stats.go` (+ test); `parse.go` (modify): `ifConnectorPresent`, `HasIfX`.
- `backend/internal/services/metrics_catalog.go`, `metrics_store.go` (+ tests): series, samples, query, retention, cleanup, usual speed.
- `backend/internal/services/port_service.go` (+ tests): port store for the monitor, views for the API.
- `backend/internal/services/port_monitor.go` (+ tests): the stats poll.
- `backend/internal/services/network_settings.go`, `network_maintenance.go`.
- `backend/internal/services/incident_service.go`, `device_service.go`, `device_poller.go`, `settings_service.go` (modify).
- `backend/internal/notifications/plugins.go` (modify): port fields, `ViewPath`.
- `backend/internal/api/port_handler.go`, `network_handler.go` (+ tests); `device_handler.go`, `incident_handler.go` (modify).
- `backend/cmd/sentinel/main.go` (modify): wiring.
- `deploy/snmpsim/gen_data.py`, `data/*.snmprec` (modify): increasing counters.

Frontend:
- `frontend/src/hooks/useDevices.ts` (modify), `usePorts.ts`, `useMetrics.ts`, `useNetworkSettings.ts` (new).
- `frontend/src/utils/network.ts`: formatting, port colours.
- `frontend/src/components/network/Faceplate.tsx`, `TrafficChart.tsx`, `PortTable.tsx`, `PortEventList.tsx`, `EditDetailsModal.tsx`.
- `frontend/src/pages/network/DeviceDetail.tsx` (rework), `PortDetail.tsx`, `NetworkSettings.tsx` (new), `SiteDetail.tsx` (modify).
- `frontend/src/components/network/DeviceTable.tsx`, `components/Layout.tsx`, `App.tsx`, `hooks/useIncidents.ts`, `pages/IncidentDetail.tsx` (modify).

---

### Task 1: Metrics and port-monitoring schema

**Files:**
- Create: `backend/migrations/048_metrics.sql`, `backend/migrations/049_port_monitoring.sql`
- Create: `backend/internal/models/jsontypes.go`, `backend/internal/models/jsontypes_test.go`
- Modify: `backend/internal/models/snmp.go`, `backend/internal/models/monitor.go`
- Modify: `backend/internal/database/timescale.go`, `backend/internal/database/timescale_test.go`
- Modify: `backend/scripts/test-db.sh`
- Create: `backend/internal/services/metrics_schema_db_test.go`

**Interfaces:**
- Produces:
  - models: `DeviceTypeSwitch/Router/AccessPoint/NVR/Other`, `ValidDeviceTypes`; `PortEvent`; `PortEventKind*` constants; `PortCondition*` constants.
  - JSON types: `ConditionSet` ([]string, nil stored as `[]`), `TimeMap` (map[string]time.Time, nil stored as `{}`), `IntSlice`, `JSONMap`, `Opt[T]`.
  - New fields on `Device`, `DeviceInterface`, `Incident` and `Notification`, exactly as below.
  - `database.TimescaleStatus.License`.
  - The env var `SENTINEL_TEST_DB_CONTAINER`, set by `make test-db`.

- [ ] **Step 1: Write the failing schema tests**

Create `backend/internal/services/metrics_schema_db_test.go`:

```go
package services

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// The metrics hypertable exists, is compressed, and carries exactly the
// policies the spec names: one retention (365 days), one compression, and a
// refresh policy per continuous aggregate.
func TestDBMetricsSchema(t *testing.T) {
	db := testdb.Open(t)

	var compressed int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM timescaledb_information.hypertables
		WHERE hypertable_schema = 'metrics' AND hypertable_name = 'samples' AND compression_enabled`).Scan(&compressed).Error)
	if compressed != 1 {
		t.Fatalf("metrics.samples compressed hypertable: found %d", compressed)
	}

	var views []string
	testdb.Must(t, db.Raw(`SELECT view_name FROM timescaledb_information.continuous_aggregates
		WHERE view_schema = 'metrics' ORDER BY view_name`).Scan(&views).Error)
	if len(views) != 2 || views[0] != "samples_1h" || views[1] != "samples_5m" {
		t.Fatalf("continuous aggregates: %v", views)
	}

	var jobs []struct {
		ProcName string
		N        int
	}
	testdb.Must(t, db.Raw(`SELECT proc_name, count(*) AS n FROM timescaledb_information.jobs
		WHERE proc_name IN ('policy_retention', 'policy_compression', 'policy_refresh_continuous_aggregate')
		GROUP BY proc_name ORDER BY proc_name`).Scan(&jobs).Error)
	want := map[string]int{"policy_compression": 1, "policy_refresh_continuous_aggregate": 2, "policy_retention": 1}
	if len(jobs) != len(want) {
		t.Fatalf("policy jobs: %+v", jobs)
	}
	for _, j := range jobs {
		if want[j.ProcName] != j.N {
			t.Errorf("%s: %d jobs, want %d", j.ProcName, j.N, want[j.ProcName])
		}
	}

	var dropAfter string
	testdb.Must(t, db.Raw(`SELECT config->>'drop_after' FROM timescaledb_information.jobs
		WHERE proc_name = 'policy_retention'`).Scan(&dropAfter).Error)
	if dropAfter != "365 days" {
		t.Errorf("retention drop_after = %q, want 365 days", dropAfter)
	}
}

// A config restore drops and recreates public tables without CASCADE. A
// foreign key from the metrics schema into public would block that drop and
// fail the restore halfway, so there must be none.
func TestDBMetricsHaveNoForeignKeys(t *testing.T) {
	db := testdb.Open(t)
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM pg_constraint
		WHERE contype = 'f' AND connamespace = 'metrics'::regnamespace`).Scan(&n).Error)
	if n != 0 {
		t.Fatalf("metrics schema has %d foreign keys; it must have none", n)
	}
}

// Port incidents carry an interface and a condition together, only on a
// device incident, and at most one is open per port per condition.
func TestDBPortIncidentConstraints(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	ifID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name) VALUES (?, ?, 51, '0/51')`, ifID, s.DeviceID)
	start := time.Now().Add(-time.Hour)

	if err := db.Exec(`INSERT INTO incidents (device_id, interface_id, start_time) VALUES (?, ?, ?)`,
		s.DeviceID, ifID, start).Error; err == nil {
		t.Error("port incident without a condition was accepted")
	}
	if err := db.Exec(`INSERT INTO incidents (device_id, condition, start_time) VALUES (?, 'errors', ?)`,
		s.DeviceID, start).Error; err == nil {
		t.Error("condition without an interface was accepted")
	}
	if err := db.Exec(`INSERT INTO incidents (device_id, interface_id, condition, start_time) VALUES (?, ?, 'toaster', ?)`,
		s.DeviceID, ifID, start).Error; err == nil {
		t.Error("unknown condition was accepted")
	}
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, interface_id, condition, start_time) VALUES (?, ?, 'errors', ?)`,
		s.DeviceID, ifID, start)
	if err := db.Exec(`INSERT INTO incidents (device_id, interface_id, condition, start_time) VALUES (?, ?, 'errors', ?)`,
		s.DeviceID, ifID, start).Error; err == nil {
		t.Error("second open incident for the same port and condition was accepted")
	}
	// A different condition on the same port is its own incident.
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, interface_id, condition, start_time) VALUES (?, ?, 'saturated', ?)`,
		s.DeviceID, ifID, start)
	// Once closed, the same condition may open again.
	testdb.Exec(t, db, `UPDATE incidents SET end_time = now() WHERE interface_id = ? AND condition = 'errors'`, ifID)
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, interface_id, condition, start_time) VALUES (?, ?, 'errors', ?)`,
		s.DeviceID, ifID, start)
}

// New interface columns default so phase 1 rows and inserts keep working.
func TestDBInterfaceMonitoringDefaults(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name) VALUES (?, 1, '0/1')`, s.DeviceID)
	var row struct {
		Important       bool
		CollectDefault  bool
		Conditions      string
		ConditionsSince string
	}
	testdb.Must(t, db.Raw(`SELECT important, collect_default, conditions::text, conditions_since::text
		FROM device_interfaces WHERE device_id = ?`, s.DeviceID).Scan(&row).Error)
	if row.Important || row.CollectDefault || row.Conditions != "[]" || row.ConditionsSince != "{}" {
		t.Errorf("defaults: %+v", row)
	}
	var detected string
	testdb.Must(t, db.Raw(`SELECT device_type_detected FROM devices WHERE id = ?`, s.DeviceID).Scan(&detected).Error)
	if detected != "other" {
		t.Errorf("device_type_detected default = %q", detected)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd backend && make test-db 2>&1 | grep -E "TestDB(Metrics|PortIncident|InterfaceMonitoring)" | head`
Expected: FAIL. The `metrics.samples` hypertable is not found, and the column `interface_id`/`important` does not exist.

- [ ] **Step 3: Write migration 048**

Create `backend/migrations/048_metrics.sql`:

```sql
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
```

- [ ] **Step 4: Write migration 049**

Create `backend/migrations/049_port_monitoring.sql`:

```sql
-- 049_port_monitoring.sql
-- Port monitoring (network monitoring phase 2): per-interface collection and
-- alerting settings and live state, device overrides and type, the port event
-- log, and incidents/notifications that belong to a port.

ALTER TABLE device_interfaces
    ADD COLUMN IF NOT EXISTS connector_present       BOOLEAN,
    ADD COLUMN IF NOT EXISTS has_ifx                 BOOLEAN NOT NULL DEFAULT false,
    -- null = use collect_default (the classifier's answer, set by inventory)
    ADD COLUMN IF NOT EXISTS collect                 BOOLEAN,
    ADD COLUMN IF NOT EXISTS collect_default         BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS important               BOOLEAN NOT NULL DEFAULT false,
    -- null = the instance default setting
    ADD COLUMN IF NOT EXISTS util_threshold_pct      INTEGER CHECK (util_threshold_pct BETWEEN 10 AND 100),
    ADD COLUMN IF NOT EXISTS error_threshold_per_min INTEGER CHECK (error_threshold_per_min BETWEEN 1 AND 1000000),
    ADD COLUMN IF NOT EXISTS down_grace_seconds      INTEGER CHECK (down_grace_seconds BETWEEN 0 AND 86400),
    ADD COLUMN IF NOT EXISTS usual_speed_bps         BIGINT,
    ADD COLUMN IF NOT EXISTS conditions              JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS conditions_since        JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS oper_changed_at         TIMESTAMPTZ;

ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS vendor_override        TEXT,
    ADD COLUMN IF NOT EXISTS model_override         TEXT,
    ADD COLUMN IF NOT EXISTS location_override      TEXT,
    ADD COLUMN IF NOT EXISTS device_type            VARCHAR(20)
        CHECK (device_type IN ('switch', 'router', 'access_point', 'nvr', 'other')),
    ADD COLUMN IF NOT EXISTS device_type_detected   VARCHAR(20) NOT NULL DEFAULT 'other'
        CHECK (device_type_detected IN ('switch', 'router', 'access_point', 'nvr', 'other')),
    ADD COLUMN IF NOT EXISTS faceplate_rows         INTEGER CHECK (faceplate_rows IN (1, 2)),
    ADD COLUMN IF NOT EXISTS faceplate_sfp_ports    JSONB,
    ADD COLUMN IF NOT EXISTS last_stats_at          TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_stats_duration_ms INTEGER;

CREATE TABLE IF NOT EXISTS port_events (
    id           BIGSERIAL PRIMARY KEY,
    device_id    UUID NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    interface_id UUID NOT NULL REFERENCES device_interfaces (id) ON DELETE CASCADE,
    if_index     INTEGER NOT NULL,
    kind         VARCHAR(20) NOT NULL CHECK (kind IN ('link_up', 'link_down', 'flapping', 'speed_change',
                     'errors', 'saturated', 'slow_link', 'admin_up', 'admin_down')),
    started_at   TIMESTAMPTZ NOT NULL,
    -- set on span events (flapping, errors, saturated, slow_link) when they end
    ended_at     TIMESTAMPTZ,
    detail       JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_port_events_device ON port_events (device_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_port_events_interface ON port_events (interface_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_port_events_open ON port_events (interface_id, kind) WHERE ended_at IS NULL;

ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS interface_id UUID REFERENCES device_interfaces (id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS condition    VARCHAR(20);
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_port_check;
ALTER TABLE incidents ADD CONSTRAINT incidents_port_check CHECK (
    (interface_id IS NULL AND condition IS NULL)
    OR (interface_id IS NOT NULL AND device_id IS NOT NULL
        AND condition IN ('link_down', 'errors', 'flapping', 'slow_link', 'saturated'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_incidents_open_port
    ON incidents (interface_id, condition) WHERE end_time IS NULL AND interface_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_incidents_interface_start
    ON incidents (interface_id, start_time DESC) WHERE interface_id IS NOT NULL;

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS interface_id UUID REFERENCES device_interfaces (id) ON DELETE CASCADE;
```

- [ ] **Step 5: Export the test container's name**

In `backend/scripts/test-db.sh`, replace the last two lines:

```bash
export SENTINEL_TEST_DATABASE_URL="postgres://sentinel:test@127.0.0.1:${port}/sentinel?sslmode=disable"
go test "$@" ./...
```

with:

```bash
export SENTINEL_TEST_DATABASE_URL="postgres://sentinel:test@127.0.0.1:${port}/sentinel?sslmode=disable"
# Lets TestDBRestoreWithMetrics run pg_dump/psql inside the container (the
# host has no postgres client tools).
export SENTINEL_TEST_DB_CONTAINER="$name"
go test "$@" ./...
```

- [ ] **Step 6: Run the schema tests**

Run: `cd backend && make test-db 2>&1 | grep -E "^(---|ok|FAIL)" | grep -E "TestDB(Metrics|PortIncident|InterfaceMonitoring)|FAIL" | head`
Expected: every `TestDB(Metrics|PortIncident|InterfaceMonitoring)` test passes, and the phase 1 packages stay `ok`.

- [ ] **Step 7: Write the JSON column types with a failing test**

Create `backend/internal/models/jsontypes_test.go`:

```go
package models

import (
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"
)

// NOT NULL JSONB columns must never receive SQL NULL from a zero value.
func TestJSONTypesStoreEmptyNotNull(t *testing.T) {
	for name, v := range map[string]driver.Valuer{
		"ConditionSet": ConditionSet(nil),
		"TimeMap":      TimeMap(nil),
		"JSONMap":      JSONMap(nil),
	} {
		got, err := v.Value()
		if err != nil || got == nil {
			t.Errorf("%s(nil).Value() = %v, %v; want empty JSON", name, got, err)
		}
	}
	if got, _ := IntSlice(nil).Value(); got != nil {
		t.Errorf("IntSlice(nil) should store NULL (no override), got %v", got)
	}
}

func TestTimeMapRoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	raw, err := TimeMap{"errors": at}.Value()
	if err != nil {
		t.Fatal(err)
	}
	var back TimeMap
	if err := back.Scan(raw); err != nil || !back["errors"].Equal(at) {
		t.Fatalf("round trip: %v %v", back, err)
	}
}

// Opt distinguishes "absent" from "null" from a value.
func TestOptDecoding(t *testing.T) {
	var body struct {
		A Opt[int] `json:"a"`
		B Opt[int] `json:"b"`
		C Opt[int] `json:"c"`
	}
	if err := json.Unmarshal([]byte(`{"b": null, "c": 7}`), &body); err != nil {
		t.Fatal(err)
	}
	if body.A.Set {
		t.Error("absent field reported as set")
	}
	if !body.B.Set || body.B.Value != nil {
		t.Errorf("null field: %+v", body.B)
	}
	if !body.C.Set || body.C.Value == nil || *body.C.Value != 7 {
		t.Errorf("value field: %+v", body.C)
	}
}
```

Run: `cd backend && go test ./internal/models/ -run 'JSONTypes|TimeMap|Opt' 2>&1 | tail -3`
Expected: FAIL, with `undefined: ConditionSet`.

- [ ] **Step 8: Implement the JSON types**

Create `backend/internal/models/jsontypes.go`:

```go
package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// ConditionSet is a port's active conditions, stored as a JSON array. Unlike
// StringSlice (where NULL means "every channel"), nil is stored as [] so the
// NOT NULL column never receives NULL.
type ConditionSet []string

func (s ConditionSet) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	return json.Marshal(s)
}

func (s *ConditionSet) Scan(value any) error {
	if value == nil {
		*s = ConditionSet{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning ConditionSet: %w", err)
	}
	return json.Unmarshal(data, s)
}

// TimeMap maps a key (a condition) to a time, stored as a JSON object; nil is
// stored as {}.
type TimeMap map[string]time.Time

func (m TimeMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}

func (m *TimeMap) Scan(value any) error {
	if value == nil {
		*m = TimeMap{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning TimeMap: %w", err)
	}
	return json.Unmarshal(data, m)
}

// IntSlice is a nullable JSON array of integers: nil is SQL NULL (e.g. "no
// override").
type IntSlice []int

func (s IntSlice) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

func (s *IntSlice) Scan(value any) error {
	if value == nil {
		*s = nil
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning IntSlice: %w", err)
	}
	return json.Unmarshal(data, s)
}

// JSONMap is a free-form JSON object; nil is stored as {}.
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(value any) error {
	if value == nil {
		*m = JSONMap{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning JSONMap: %w", err)
	}
	return json.Unmarshal(data, m)
}

// Opt is a PATCH field: Set is false when the key was absent, and Value is nil
// when it was JSON null ("clear") and non-nil when it held a value.
type Opt[T any] struct {
	Set   bool
	Value *T
}

func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}
```

Run: `cd backend && go test ./internal/models/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 9: Add the model fields**

In `backend/internal/models/snmp.go`, add after the device status constants:

```go
// Device types. device_type is the user's override; device_type_detected is
// what inventory concluded (portmon.DetectDeviceType).
const (
	DeviceTypeSwitch      = "switch"
	DeviceTypeRouter      = "router"
	DeviceTypeAccessPoint = "access_point"
	DeviceTypeNVR         = "nvr"
	DeviceTypeOther       = "other"
)

var ValidDeviceTypes = map[string]bool{
	DeviceTypeSwitch: true, DeviceTypeRouter: true, DeviceTypeAccessPoint: true, DeviceTypeNVR: true, DeviceTypeOther: true,
}
```

In `type Device struct`, add after `Serial`:

```go
	// Overrides: when set, the UI shows these instead of what SNMP reported,
	// and inventory never writes them.
	VendorOverride   *string `json:"vendor_override" gorm:"column:vendor_override"`
	ModelOverride    *string `json:"model_override" gorm:"column:model_override"`
	LocationOverride *string `json:"location_override" gorm:"column:location_override"`
	// DeviceType is the user's override; DeviceTypeDetected is inventory's.
	DeviceType         *string `json:"device_type" gorm:"column:device_type"`
	DeviceTypeDetected string  `json:"device_type_detected" gorm:"column:device_type_detected;not null;default:other"`
	// Faceplate layout overrides (nil = automatic).
	FaceplateRows       *int       `json:"faceplate_rows" gorm:"column:faceplate_rows"`
	FaceplateSFPPorts   IntSlice   `json:"faceplate_sfp_ports" gorm:"column:faceplate_sfp_ports;type:jsonb"`
	LastStatsAt         *time.Time `json:"last_stats_at" gorm:"column:last_stats_at"`
	LastStatsDurationMs *int       `json:"last_stats_duration_ms" gorm:"column:last_stats_duration_ms"`
```

In `type DeviceInterface struct`, add after `Present`:

```go
	ConnectorPresent *bool `json:"connector_present" gorm:"column:connector_present"`
	// HasIfX: the ifXTable has answered for this interface at least once, so
	// its name, alias and high speed are not overwritten by an inventory whose
	// ifXTable walk failed.
	HasIfX bool `json:"-" gorm:"column:has_ifx;not null"`
	// Collect is the user's choice (nil = CollectDefault, the classifier's).
	Collect              *bool        `json:"collect" gorm:"column:collect"`
	CollectDefault       bool         `json:"collect_default" gorm:"column:collect_default;not null"`
	Important            bool         `json:"important" gorm:"column:important;not null"`
	UtilThresholdPct     *int         `json:"util_threshold_pct" gorm:"column:util_threshold_pct"`
	ErrorThresholdPerMin *int         `json:"error_threshold_per_min" gorm:"column:error_threshold_per_min"`
	DownGraceSeconds     *int         `json:"down_grace_seconds" gorm:"column:down_grace_seconds"`
	UsualSpeedBps        *int64       `json:"usual_speed_bps" gorm:"column:usual_speed_bps"`
	Conditions           ConditionSet `json:"conditions" gorm:"column:conditions;type:jsonb;not null"`
	ConditionsSince      TimeMap      `json:"conditions_since" gorm:"column:conditions_since;type:jsonb;not null"`
	OperChangedAt        *time.Time   `json:"oper_changed_at" gorm:"column:oper_changed_at"`
```

At the end of `snmp.go`, add:

```go
// CollectEffective is whether the stats poll reads this interface.
func (i DeviceInterface) CollectEffective() bool {
	if i.Collect != nil {
		return *i.Collect
	}
	return i.CollectDefault
}

// Port conditions (portmon.Condition values), as stored in
// device_interfaces.conditions and incidents.condition.
const (
	PortConditionLinkDown  = "link_down"
	PortConditionErrors    = "errors"
	PortConditionFlapping  = "flapping"
	PortConditionSlowLink  = "slow_link"
	PortConditionSaturated = "saturated"
)

// Port event kinds.
const (
	PortEventLinkUp      = "link_up"
	PortEventLinkDown    = "link_down"
	PortEventFlapping    = "flapping"
	PortEventSpeedChange = "speed_change"
	PortEventErrors      = "errors"
	PortEventSaturated   = "saturated"
	PortEventSlowLink    = "slow_link"
	PortEventAdminUp     = "admin_up"
	PortEventAdminDown   = "admin_down"
)

// PortEvent is one row of the port event log. Span events (flapping, errors,
// saturated, slow_link) get EndedAt when they end.
type PortEvent struct {
	ID          int64      `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	DeviceID    uuid.UUID  `json:"device_id" gorm:"column:device_id;type:uuid;not null"`
	InterfaceID uuid.UUID  `json:"interface_id" gorm:"column:interface_id;type:uuid;not null"`
	IfIndex     int        `json:"if_index" gorm:"column:if_index;not null"`
	Kind        string     `json:"kind" gorm:"column:kind;not null"`
	StartedAt   time.Time  `json:"started_at" gorm:"column:started_at;not null"`
	EndedAt     *time.Time `json:"ended_at" gorm:"column:ended_at"`
	Detail      JSONMap    `json:"detail" gorm:"column:detail;type:jsonb;not null"`
}

func (PortEvent) TableName() string { return "port_events" }
```

In `backend/internal/models/monitor.go`, add to `type Incident struct` after `DeviceID`:

```go
	// InterfaceID and Condition are set together on a port incident (a device
	// incident about one of the device's ports); incidents_port_check.
	InterfaceID *uuid.UUID `json:"interface_id" gorm:"column:interface_id;type:uuid"`
	Condition   *string    `json:"condition" gorm:"column:condition"`
```

and to `type Notification struct` after `DeviceID`:

```go
	// InterfaceID is set on a device notification about one port.
	InterfaceID *uuid.UUID `json:"interface_id" gorm:"column:interface_id;type:uuid"`
```

Run: `cd backend && go build ./... && go vet ./internal/models/`
Expected: no output.

- [ ] **Step 10: TimescaleDB licence preflight, test first**

Append to `backend/internal/database/timescale_test.go`:

```go
// Compression and continuous aggregates exist only in the Timescale-licensed
// build; the Apache-only image starts fine and then fails migration 048 with
// a bare "functionality not supported" error.
func TestTimescaleApacheOnly(t *testing.T) {
	err := TimescaleMissingError(TimescaleStatus{Available: true, Preloaded: true, License: "apache"})
	if err == nil {
		t.Fatal("apache-only build: got nil, want an error")
	}
	for _, want := range []string{"timescale/timescaledb", "docker-compose.yml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
	if err := TimescaleMissingError(TimescaleStatus{Available: true, Preloaded: true, License: "timescale"}); err != nil {
		t.Errorf("timescale licence: %v", err)
	}
}
```

Run: `cd backend && go test ./internal/database/ -run Timescale 2>&1 | tail -3`
Expected: FAIL, with `unknown field License`.

In `backend/internal/database/timescale.go`:

1. Add after `errTimescaleNotPreloaded`:

```go
// errTimescaleApacheOnly: the -oss image (Apache licence) lacks compression
// and continuous aggregates, which the metrics store needs.
var errTimescaleApacheOnly = errors.New(
	"Sentinel's database runs the Apache-only build of TimescaleDB, which lacks compression " +
		"and continuous aggregates. Use the timescale/timescaledb image from the current " +
		"docker-compose.yml (not an -oss tag) and run `docker compose up -d`. See GETTING_STARTED.md.")
```

2. Add a field to `TimescaleStatus`:

```go
	// License is timescaledb.license ("timescale" or "apache"); "" if unknown.
	License string
```

3. In `TimescaleMissingError`, add a case before `default`:

```go
	case st.License != "" && st.License != "timescale":
		return errTimescaleApacheOnly
```

4. In `RequireTimescale`, before `return TimescaleMissingError(st)`:

```go
	if st.Preloaded {
		if err := db.Raw("SELECT COALESCE(current_setting('timescaledb.license', true), '')").Scan(&st.License).Error; err != nil {
			return fmt.Errorf("checking the TimescaleDB licence: %w", err)
		}
	}
```

Run: `cd backend && go test ./internal/database/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 11: Full check and commit**

Run: `cd backend && go vet ./... && go test ./... 2>&1 | grep -v "^ok" | head; make test-db 2>&1 | grep -E "^(FAIL|---)" | head`
Expected: no FAIL lines.

```bash
git add backend/migrations/048_metrics.sql backend/migrations/049_port_monitoring.sql \
  backend/internal/models backend/internal/database backend/scripts/test-db.sh \
  backend/internal/services/metrics_schema_db_test.go
git commit -m "feat(metrics): metrics store and port monitoring schema

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 2: Counter-to-rate conversion (`portmon`)

**Files:**
- Create: `backend/internal/portmon/rates.go`, `backend/internal/portmon/rates_test.go`

**Interfaces:**
- Produces: `portmon.Reading`, `portmon.Rates`, `portmon.Skip` (+ `SkipNone`, `SkipFirst`, `SkipReboot`, `SkipReset`, `SkipInterval`, `SkipCounterType`), `func ComputeRates(prev *Reading, cur Reading, interval time.Duration) (Rates, Skip)`.

- [ ] **Step 1: Write the failing table test**

Create `backend/internal/portmon/rates_test.go`:

```go
package portmon

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func reading(at time.Time, uptime int64, in, out uint64) Reading {
	return Reading{At: at, UptimeSeconds: uptime, HC: true, InOctets: in, OutOctets: out, SpeedBps: 1_000_000_000}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6*math.Max(1, math.Abs(b)) }

func TestComputeRates(t *testing.T) {
	minute := time.Minute
	cases := []struct {
		name     string
		prev     *Reading
		cur      Reading
		wantSkip Skip
		check    func(t *testing.T, r Rates)
	}{
		{name: "first reading", prev: nil, cur: reading(t0, 100, 0, 0), wantSkip: SkipFirst},
		{
			name: "100 Mb/s in on a 1 Gb/s port",
			prev: ptr(reading(t0, 100, 0, 0)),
			cur:  reading(t0.Add(minute), 160, 750_000_000, 75_000_000),
			check: func(t *testing.T, r Rates) {
				if !near(r.InBps, 100e6) || !near(r.OutBps, 10e6) {
					t.Errorf("bps in %v out %v", r.InBps, r.OutBps)
				}
				if r.InUtilPct == nil || !near(*r.InUtilPct, 10) || !near(*r.OutUtilPct, 1) {
					t.Errorf("util %v %v", r.InUtilPct, r.OutUtilPct)
				}
			},
		},
		{
			name: "32-bit counter wrap is a rate, not a spike",
			prev: &Reading{At: t0, UptimeSeconds: 100, InOctets: 4_294_000_000, OutOctets: 10, SpeedBps: 1_000_000_000},
			cur:  Reading{At: t0.Add(minute), UptimeSeconds: 160, InOctets: 1_000_000, OutOctets: 10, SpeedBps: 1_000_000_000},
			check: func(t *testing.T, r Rates) {
				want := float64(1_000_000+(1<<32)-4_294_000_000) * 8 / 60
				if !near(r.InBps, want) {
					t.Errorf("wrapped in bps %v, want %v", r.InBps, want)
				}
			},
		},
		{
			name:     "32-bit drop too big to be a wrap on a 10 Mb/s link is a reset",
			prev:     &Reading{At: t0, UptimeSeconds: 100, InOctets: 4_000_000_000, SpeedBps: 10_000_000},
			cur:      Reading{At: t0.Add(minute), UptimeSeconds: 160, InOctets: 10, SpeedBps: 10_000_000},
			wantSkip: SkipReset,
		},
		{name: "64-bit counter going down is a reset", prev: ptr(reading(t0, 100, 5000, 0)), cur: reading(t0.Add(minute), 160, 10, 0), wantSkip: SkipReset},
		{name: "device restarted", prev: ptr(reading(t0, 100_000, 0, 0)), cur: reading(t0.Add(minute), 30, 10, 10), wantSkip: SkipReboot},
		{name: "interval far too short", prev: ptr(reading(t0, 100, 0, 0)), cur: reading(t0.Add(20*time.Second), 120, 1, 1), wantSkip: SkipInterval},
		{name: "interval far too long", prev: ptr(reading(t0, 100, 0, 0)), cur: reading(t0.Add(200*time.Second), 300, 1, 1), wantSkip: SkipInterval},
		{
			name:     "counter width changed",
			prev:     ptr(reading(t0, 100, 0, 0)),
			cur:      Reading{At: t0.Add(minute), UptimeSeconds: 160, HC: false, SpeedBps: 1_000_000_000},
			wantSkip: SkipCounterType,
		},
		{
			name:     "more traffic than the link can carry is discarded",
			prev:     ptr(reading(t0, 100, 0, 0)),
			cur:      reading(t0.Add(minute), 160, 1_000_000_000_000, 0),
			wantSkip: SkipReset,
		},
		{
			name: "speed unknown: rates without utilisation",
			prev: &Reading{At: t0, UptimeSeconds: 100, HC: true},
			cur:  Reading{At: t0.Add(minute), UptimeSeconds: 160, HC: true, InOctets: 7_500_000},
			check: func(t *testing.T, r Rates) {
				if !near(r.InBps, 1e6) || r.InUtilPct != nil {
					t.Errorf("in %v util %v", r.InBps, r.InUtilPct)
				}
			},
		},
		{
			name: "errors and discards per minute",
			prev: &Reading{At: t0, UptimeSeconds: 100, HC: true, SpeedBps: 1e9, InErrors: 5, OutErrors: 0, InDiscards: 100, OutDiscards: 0},
			cur:  Reading{At: t0.Add(2 * minute), UptimeSeconds: 220, HC: true, SpeedBps: 1e9, InErrors: 605, OutErrors: 20, InDiscards: 100, OutDiscards: 4},
			check: func(t *testing.T, r Rates) {
				if !near(r.InErrorsPM, 300) || !near(r.OutErrorsPM, 10) || r.InDiscardsPM != 0 || !near(r.OutDiscardsPM, 2) {
					t.Errorf("%+v", r)
				}
			},
		},
		{
			name:     "unknown uptime does not block a normal reading",
			prev:     &Reading{At: t0, UptimeSeconds: -1, HC: true, SpeedBps: 1e9},
			cur:      Reading{At: t0.Add(minute), UptimeSeconds: -1, HC: true, SpeedBps: 1e9, InOctets: 60},
			wantSkip: SkipNone,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, skip := ComputeRates(c.prev, c.cur, minute)
			if skip != c.wantSkip {
				t.Fatalf("skip = %q, want %q", skip, c.wantSkip)
			}
			if c.check != nil {
				c.check(t, r)
			}
		})
	}
}

func ptr(r Reading) *Reading { return &r }
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/portmon/ 2>&1 | tail -3`
Expected: FAIL (`no non-test Go files` or `undefined: Reading`).

- [ ] **Step 3: Implement**

Create `backend/internal/portmon/rates.go`:

```go
// Package portmon is the pure logic behind port monitoring: turning counter
// readings into rates, deciding which interfaces are ports, laying out the
// faceplate, and tracking each port's events and conditions. Nothing here
// touches the network or the database.
package portmon

import "time"

// Reading is one stats poll of one interface.
type Reading struct {
	At time.Time
	// UptimeSeconds is the device's sysUpTime when read; < 0 when unknown.
	UptimeSeconds int64
	// HC: the octet counters are the 64-bit ifHC* ones.
	HC                              bool
	InOctets, OutOctets             uint64
	InErrors, OutErrors             uint64
	InDiscards, OutDiscards         uint64
	SpeedBps                        int64
}

// Rates are derived from two consecutive readings.
type Rates struct {
	InBps, OutBps float64
	// Nil when the link speed is unknown (0).
	InUtilPct, OutUtilPct       *float64
	InErrorsPM, OutErrorsPM     float64
	InDiscardsPM, OutDiscardsPM float64
}

// Skip says why no rates were produced; SkipNone means they were.
type Skip string

const (
	SkipNone        Skip = ""
	SkipFirst       Skip = "first reading"
	SkipReboot      Skip = "device restarted"
	SkipReset       Skip = "counter went backwards or jumped implausibly"
	SkipInterval    Skip = "interval out of range"
	SkipCounterType Skip = "counter width changed"
)

const two32 = uint64(1) << 32

// unknownSpeedCeilingBps bounds a plausible rate when the link speed is not
// known: nothing Sentinel polls moves more than 10 Gb/s on one unknown port,
// and a counter reset read as a wrap would claim far more.
const unknownSpeedCeilingBps = 10e9

// ComputeRates turns two readings into rates. It refuses (returns a Skip)
// rather than guess whenever the pair cannot be trusted, because a single
// bogus terabit sample ruins a chart's scale and a report's 95th percentile:
// the device restarted (sysUpTime went backwards), the counters went down
// other than by a plausible 32-bit wrap, the delta is faster than the link
// can carry, the interval is outside 0.5x-3x the poll interval, or the counter
// width changed between readings.
func ComputeRates(prev *Reading, cur Reading, interval time.Duration) (Rates, Skip) {
	if prev == nil {
		return Rates{}, SkipFirst
	}
	dt := cur.At.Sub(prev.At)
	if dt <= 0 || (interval > 0 && (dt < interval/2 || dt > 3*interval)) {
		return Rates{}, SkipInterval
	}
	if prev.UptimeSeconds >= 0 && cur.UptimeSeconds >= 0 && cur.UptimeSeconds < prev.UptimeSeconds {
		return Rates{}, SkipReboot
	}
	if prev.HC != cur.HC {
		return Rates{}, SkipCounterType
	}
	secs := dt.Seconds()
	ceiling := unknownSpeedCeilingBps
	if cur.SpeedBps > 0 {
		ceiling = float64(cur.SpeedBps) * 1.1
	}
	maxOctets := ceiling / 8 * secs

	in, okIn := octetDelta(prev.InOctets, cur.InOctets, cur.HC, maxOctets)
	out, okOut := octetDelta(prev.OutOctets, cur.OutOctets, cur.HC, maxOctets)
	if !okIn || !okOut {
		return Rates{}, SkipReset
	}
	var deltas [4]uint64
	for i, p := range [4][2]uint64{
		{prev.InErrors, cur.InErrors}, {prev.OutErrors, cur.OutErrors},
		{prev.InDiscards, cur.InDiscards}, {prev.OutDiscards, cur.OutDiscards},
	} {
		d, ok := counter32Delta(p[0], p[1])
		if !ok {
			return Rates{}, SkipReset
		}
		deltas[i] = d
	}

	perMin := 60 / secs
	r := Rates{
		InBps:         float64(in) * 8 / secs,
		OutBps:        float64(out) * 8 / secs,
		InErrorsPM:    float64(deltas[0]) * perMin,
		OutErrorsPM:   float64(deltas[1]) * perMin,
		InDiscardsPM:  float64(deltas[2]) * perMin,
		OutDiscardsPM: float64(deltas[3]) * perMin,
	}
	if cur.SpeedBps > 0 {
		inPct := r.InBps / float64(cur.SpeedBps) * 100
		outPct := r.OutBps / float64(cur.SpeedBps) * 100
		r.InUtilPct, r.OutUtilPct = &inPct, &outPct
	}
	return r, SkipNone
}

// octetDelta is the octets moved between two readings. A 64-bit counter never
// wraps in practice, so going down means a reset; a 32-bit one going down is a
// wrap only when the wrapped delta is plausible for the link.
func octetDelta(prev, cur uint64, hc bool, maxOctets float64) (uint64, bool) {
	if cur >= prev {
		d := cur - prev
		return d, float64(d) <= maxOctets
	}
	if hc {
		return 0, false
	}
	d := cur + two32 - prev
	return d, float64(d) <= maxOctets
}

// counter32Delta handles the error and discard counters (always Counter32).
// A wrap is accepted when the wrapped delta is under half the counter's range;
// anything larger is a reset.
func counter32Delta(prev, cur uint64) (uint64, bool) {
	if cur >= prev {
		return cur - prev, true
	}
	d := cur + two32 - prev
	return d, d < two32/2
}
```

- [ ] **Step 4: Run the test**

Run: `cd backend && go test ./internal/portmon/ -run TestComputeRates -v 2>&1 | tail -20`
Expected: every subtest PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/portmon/rates.go backend/internal/portmon/rates_test.go
git commit -m "feat(portmon): counter-to-rate conversion that refuses resets and reboots

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Interface classifier, device type, faceplate layout, usual speed (`portmon`)

**Files:**
- Create: `backend/internal/portmon/classify.go`, `devicetype.go`, `layout.go`, `usual.go`
- Create: `backend/internal/portmon/fixtures_test.go`, `classify_test.go`, `layout_test.go`

**Interfaces:**
- Produces:
  - `portmon.IfInfo{Name, Descr string; Type int; ConnectorPresent *bool}`
  - `IsPhysical(IfInfo) bool`, `IsLAG(IfInfo) bool`, `DefaultCollect(IfInfo) bool`
  - `DetectDeviceType(model string, physicalPorts int) string` (returns the `models.DeviceType*` strings)
  - `PortNumber(name, descr string, ifIndex int) int`
  - `LayoutPort{IfIndex int; Name, Descr string}`, `FacePort{IfIndex, Number int}`, `FaceBlock{SFP bool; Top, Bottom []FacePort}`, `Faceplate{Rows int; Blocks []FaceBlock}` (JSON tags snake_case)
  - `Layout(ports []LayoutPort, rowsOverride *int, sfpOverride []int) Faceplate`
  - `UsualSpeed(counts map[int64]int, historyHours float64) int64`

- [ ] **Step 1: Write the fixtures (the user's real gear)**

Create `backend/internal/portmon/fixtures_test.go`:

```go
package portmon

import "fmt"

// Interface lists captured from the user's sandbox on 2026-09-30.

type fixtureIf struct {
	Index int
	Name  string
	Descr string
	Type  int
}

func info(f fixtureIf) IfInfo { return IfInfo{Name: f.Name, Descr: f.Descr, Type: f.Type} }

// QuantumLink: USW-Pro-48-PoE. Ports 1-48 copper, 49-52 SFP+, a CPU
// interface, and 26 link aggregates.
func uswPro48() []fixtureIf {
	var out []fixtureIf
	for i := 1; i <= 48; i++ {
		out = append(out, fixtureIf{i, fmt.Sprintf("0/%d", i), fmt.Sprintf("Slot: 0 Port: %d Gigabit - Level", i), 6})
	}
	for i := 49; i <= 52; i++ {
		out = append(out, fixtureIf{i, fmt.Sprintf("0/%d", i), fmt.Sprintf("Slot: 0 Port: %d 10G - Level", i), 6})
	}
	out = append(out, fixtureIf{65, "CPU Interface:  5/1", "CPU Interface for Slot: 5 Port: 1", 1})
	for i := 1; i <= 26; i++ {
		out = append(out, fixtureIf{65 + i, fmt.Sprintf("3/%d", i), fmt.Sprintf("Link Aggregate %d", i), 161})
	}
	return out
}

// Quantum-Gate: UDM-SE. eth0-7 LAN, eth8 2.5G WAN, eth9/eth10 SFP+; the rest
// is bridges, VLAN sub-interfaces, tunnels and the internal switch0.
func udmSE() []fixtureIf {
	out := []fixtureIf{
		{1, "lo", "lo", 24}, {2, "dummy0", "dummy0", 6},
		{3, "eth9", "Annapurna Labs Ltd. SFP+ 10G Ethernet Adapter", 6},
		{4, "eth10", "Annapurna Labs Ltd. SFP+ 10G Ethernet Adapter", 6},
		{5, "switch0", "Annapurna Labs Ltd. Gigabit Ethernet Adapter", 6},
		{6, "gre0", "gre0", 131}, {7, "gretap0", "gretap0", 6}, {8, "erspan0", "erspan0", 6},
		{9, "ip_vti0", "ip_vti0", 131}, {10, "ip6_vti0", "ip6_vti0", 131}, {11, "sit0", "sit0", 131},
		{12, "ip6tnl0", "ip6tnl0", 131},
		{13, "eth8", "Realtek Semiconductor Co., Ltd. RTL8125 2.5GbE Controller", 6},
		{14, "ifb0", "ifb0", 6}, {15, "ifb1", "ifb1", 6},
	}
	for i := 0; i <= 7; i++ {
		out = append(out, fixtureIf{16 + i, fmt.Sprintf("eth%d", i), fmt.Sprintf("eth%d", i), 6})
	}
	for i, v := range []int{10, 100, 20, 255, 30} {
		out = append(out, fixtureIf{24 + i, fmt.Sprintf("eth10.%d", v), fmt.Sprintf("eth10.%d", v), 6})
		out = append(out, fixtureIf{46 + i, fmt.Sprintf("switch0.%d", v), fmt.Sprintf("switch0.%d", v), 6})
		out = append(out, fixtureIf{58 + i, fmt.Sprintf("br%d", v), fmt.Sprintf("br%d", v), 6})
	}
	out = append(out, fixtureIf{70, "ifbeth6", "ifbeth6", 6}, fixtureIf{72, "wgsrv1", "wgsrv1", 1})
	return out
}

// KitchenAP: U7-Pro. Only eth0 is a physical port.
func u7Pro() []fixtureIf {
	return []fixtureIf{
		{1, "lo", "lo", 24}, {2, "miireg", "miireg", 1}, {3, "eth0", "eth0", 6},
		{4, "ip6tnl0", "ip6tnl0", 131}, {5, "sit0", "sit0", 131}, {6, "gre0", "gre0", 131},
		{7, "gretap0", "gretap0", 6}, {8, "erspan0", "erspan0", 6}, {9, "ip6gre0", "ip6gre0", 1},
		{10, "bond0", "bond0", 6}, {11, "teql0", "teql0", 1}, {12, "mld-wifi0", "mld-wifi0", 6},
		{13, "wifi0", "wifi0", 1}, {14, "soc0", "soc0", 1}, {15, "wifi1", "Device 17cb:1109", 1},
		{18, "wifi0ap0", "wifi0ap0", 6}, {23, "wifi1ap1", "wifi1ap1", 6},
		{31, "eth0.50", "eth0.50", 6}, {32, "wifi0ap0.50", "wifi0ap0.50", 6},
		{39, "br0", "br0", 6}, {40, "br0.10", "br0.10", 6}, {44, "pd99", "pd99", 1},
	}
}

// Overwatch: UNVR. Two physical ports, no faceplate (NVR).
func unvr() []fixtureIf {
	return []fixtureIf{
		{1, "lo", "lo", 24},
		{2, "enp0s1", "Annapurna Labs Ltd. Gigabit Ethernet Adapter", 6},
		{3, "enp0s2", "Annapurna Labs Ltd. SFP+ 10G Ethernet Adapter", 6},
	}
}
```

- [ ] **Step 2: Write the failing classifier and device-type tests**

Create `backend/internal/portmon/classify_test.go`:

```go
package portmon

import (
	"reflect"
	"sort"
	"testing"
)

func physicalIndexes(ifs []fixtureIf) []int {
	var out []int
	for _, f := range ifs {
		if IsPhysical(info(f)) {
			out = append(out, f.Index)
		}
	}
	sort.Ints(out)
	return out
}

func TestIsPhysicalOnRealGear(t *testing.T) {
	var usw []int
	for i := 1; i <= 52; i++ {
		usw = append(usw, i)
	}
	cases := map[string]struct {
		ifs  []fixtureIf
		want []int
	}{
		"USW-Pro-48": {uswPro48(), usw},
		"UDM-SE":     {udmSE(), []int{3, 4, 13, 16, 17, 18, 19, 20, 21, 22, 23}},
		"U7-Pro":     {u7Pro(), []int{3}},
		"UNVR":       {unvr(), []int{2, 3}},
	}
	for name, c := range cases {
		if got := physicalIndexes(c.ifs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s physical = %v, want %v", name, got, c.want)
		}
	}
}

func TestDefaultCollect(t *testing.T) {
	// Link aggregates are collected but are not physical ports.
	lag := IfInfo{Name: "3/1", Descr: "Link Aggregate 1", Type: 161}
	if !DefaultCollect(lag) || IsPhysical(lag) {
		t.Error("LAG: want collected, not physical")
	}
	// The CPU interface and virtual interfaces are not collected.
	for _, i := range []IfInfo{
		{Name: "CPU Interface:  5/1", Type: 1},
		{Name: "br0", Type: 6},
		{Name: "eth10.100", Type: 6},
		{Name: "switch0", Type: 6},
		{Name: "wifi0ap0", Type: 6},
	} {
		if DefaultCollect(i) {
			t.Errorf("%q collected by default", i.Name)
		}
	}
	// ifConnectorPresent decides for an Ethernet-typed interface with an
	// ordinary name, either way.
	yes, no := true, false
	if !IsPhysical(IfInfo{Name: "port7", Type: 1, ConnectorPresent: &yes}) {
		t.Error("connector present on a non-Ethernet type should still be physical")
	}
	if IsPhysical(IfInfo{Name: "eth3", Type: 6, ConnectorPresent: &no}) {
		t.Error("connector absent should not be physical")
	}
	// A virtual name wins even if the agent claims a connector.
	if IsPhysical(IfInfo{Name: "br0", Type: 6, ConnectorPresent: &yes}) {
		t.Error("bridge claiming a connector should not be physical")
	}
}

func TestDetectDeviceType(t *testing.T) {
	cases := []struct {
		model string
		ports int
		want  string
	}{
		{"USW-Pro-48-PoE", 52, "switch"},
		{"UBNT-US48PRO-POE", 52, "switch"}, // what the USW's ENTITY-MIB reports: falls back to port count
		{"ES-48-500W", 52, "switch"},
		{"EdgeSwitch 24", 26, "switch"},
		{"US-8-60W", 8, "switch"},
		{"UDM-SE", 11, "router"},
		{"UXG-Pro", 4, "router"},
		{"USG-3P", 3, "router"},
		{"ER-4", 4, "router"},
		{"U7-Pro", 1, "access_point"},
		{"U6-LR", 1, "access_point"},
		{"UAP-AC-Pro", 1, "access_point"},
		{"UNVR4", 2, "nvr"},
		{"WS-C2960X-24TS-L", 26, "switch"},
		{"", 2, "other"},
		{"Cambium ePMP", 2, "other"},
	}
	for _, c := range cases {
		if got := DetectDeviceType(c.model, c.ports); got != c.want {
			t.Errorf("DetectDeviceType(%q, %d) = %q, want %q", c.model, c.ports, got, c.want)
		}
	}
}

func TestUsualSpeed(t *testing.T) {
	counts := map[int64]int{1_000_000_000: 1900, 100_000_000: 116}
	if got := UsualSpeed(counts, 168); got != 1_000_000_000 {
		t.Errorf("usual = %d", got)
	}
	if got := UsualSpeed(counts, 23.9); got != 0 {
		t.Errorf("under 24 h of history must give 0, got %d", got)
	}
	// A tie picks the faster speed: an equally common slower speed is the
	// anomaly, not the norm.
	if got := UsualSpeed(map[int64]int{1e9: 5, 1e8: 5}, 48); got != 1e9 {
		t.Errorf("tie = %d", got)
	}
	if got := UsualSpeed(map[int64]int{}, 48); got != 0 {
		t.Errorf("empty = %d", got)
	}
}
```

- [ ] **Step 3: Write the failing layout test**

Create `backend/internal/portmon/layout_test.go`:

```go
package portmon

import (
	"reflect"
	"testing"
)

func numbers(ps []FacePort) []int {
	out := make([]int, len(ps))
	for i, p := range ps {
		out[i] = p.Number
	}
	return out
}

func TestPortNumber(t *testing.T) {
	cases := []struct {
		name, descr string
		idx, want   int
	}{
		{"0/12", "", 12, 12},
		{"1/0/12", "", 99, 12},
		{"Gi1/0/7", "", 10107, 7},
		{"Port 12", "", 3, 12},
		{"eth12", "", 40, 12},
		{"", "Slot: 0 Port: 12 Gigabit - Level", 5, 12},
		{"uplink", "", 7, 7},
	}
	for _, c := range cases {
		if got := PortNumber(c.name, c.descr, c.idx); got != c.want {
			t.Errorf("PortNumber(%q, %q, %d) = %d, want %d", c.name, c.descr, c.idx, got, c.want)
		}
	}
}

func uswLayoutPorts() []LayoutPort {
	var ps []LayoutPort
	for _, f := range uswPro48() {
		if IsPhysical(info(f)) {
			ps = append(ps, LayoutPort{IfIndex: f.Index, Name: f.Name, Descr: f.Descr})
		}
	}
	return ps
}

// The USW-Pro-48 draws like the real switch: four blocks of twelve with odd
// ports on top, then the four SFP+ ports as a 2x2 block.
func TestLayoutUSWPro48(t *testing.T) {
	fp := Layout(uswLayoutPorts(), nil, nil)
	if fp.Rows != 2 || len(fp.Blocks) != 5 {
		t.Fatalf("rows %d blocks %d", fp.Rows, len(fp.Blocks))
	}
	if got := numbers(fp.Blocks[0].Top); !reflect.DeepEqual(got, []int{1, 3, 5, 7, 9, 11}) {
		t.Errorf("block 1 top %v", got)
	}
	if got := numbers(fp.Blocks[3].Bottom); !reflect.DeepEqual(got, []int{38, 40, 42, 44, 46, 48}) {
		t.Errorf("block 4 bottom %v", got)
	}
	sfp := fp.Blocks[4]
	if !sfp.SFP || !reflect.DeepEqual(numbers(sfp.Top), []int{49, 51}) || !reflect.DeepEqual(numbers(sfp.Bottom), []int{50, 52}) {
		t.Errorf("sfp block %+v", sfp)
	}
}

func TestLayoutOverridesAndSmallDevices(t *testing.T) {
	one := 1
	fp := Layout(uswLayoutPorts(), &one, nil)
	if fp.Rows != 1 || len(fp.Blocks[0].Bottom) != 0 || len(fp.Blocks[0].Top) != 12 {
		t.Errorf("one-row override: %+v", fp.Blocks[0])
	}

	// Eight ports or fewer sit in one row.
	var eight []LayoutPort
	for i := 1; i <= 8; i++ {
		eight = append(eight, LayoutPort{IfIndex: i, Name: "Port " + string(rune('0'+i))})
	}
	if fp := Layout(eight, nil, nil); fp.Rows != 1 || len(fp.Blocks) != 1 || len(fp.Blocks[0].Top) != 8 {
		t.Errorf("8 ports: %+v", fp)
	}

	// The user can mark ports as SFP when the descriptions do not say so.
	fp = Layout(eight, nil, []int{7, 8})
	if len(fp.Blocks) != 2 || !fp.Blocks[1].SFP || len(fp.Blocks[0].Top) != 6 {
		t.Errorf("sfp override: %+v", fp)
	}

	// Empty input still yields well-formed JSON-able slices.
	if fp := Layout(nil, nil, nil); fp.Blocks == nil || len(fp.Blocks) != 0 {
		t.Errorf("empty layout: %+v", fp)
	}
}

// The UDM-SE: eth9/eth10 describe themselves as SFP+ and form their own block.
func TestLayoutUDMSE(t *testing.T) {
	var ps []LayoutPort
	for _, f := range udmSE() {
		if IsPhysical(info(f)) {
			ps = append(ps, LayoutPort{IfIndex: f.Index, Name: f.Name, Descr: f.Descr})
		}
	}
	fp := Layout(ps, nil, nil)
	last := fp.Blocks[len(fp.Blocks)-1]
	if !last.SFP || len(last.Top)+len(last.Bottom) != 2 {
		t.Errorf("UDM SFP block %+v", last)
	}
	copper := 0
	for _, b := range fp.Blocks[:len(fp.Blocks)-1] {
		copper += len(b.Top) + len(b.Bottom)
	}
	if copper != 9 {
		t.Errorf("UDM copper ports %d, want 9 (eth0-eth8)", copper)
	}
}
```

- [ ] **Step 4: Run them to verify they fail**

Run: `cd backend && go test ./internal/portmon/ 2>&1 | tail -3`
Expected: FAIL (`undefined: IsPhysical`).

- [ ] **Step 5: Implement the classifier**

Create `backend/internal/portmon/classify.go`:

```go
package portmon

import "regexp"

// IfInfo is what classification needs to know about an interface.
type IfInfo struct {
	Name  string
	Descr string
	Type  int // IANA ifType
	// ConnectorPresent is ifXTable's ifConnectorPresent; nil when the device
	// does not report it.
	ConnectorPresent *bool
}

const ifTypeLAG = 161

var ethernetTypes = map[int]bool{6: true, 62: true, 69: true, 117: true}

// virtualName matches what Linux-based devices (UniFi consoles and APs,
// EdgeOS) report as ifType 6 even though no cable can be plugged into it:
// loopback, bridges, VLANs, radios and their VAPs, tunnels, bonding and
// traffic-shaping devices, WireGuard, the UDM's internal switch0, and CPU
// interfaces.
var virtualName = regexp.MustCompile(`(?i)^(lo|br|vlan|wlan|wifi|ath|ra\d|rai\d|veth|docker|tun|tap|imq|ifb|gre|erspan|ip6tnl|ip6gre|ip_vti|ip6_vti|sit|teql|bond|mld-|soc\d|miireg|pd\d|dummy|wg|switch\d|cpu)`)

// subInterface matches VLAN sub-interfaces such as eth0.50.
var subInterface = regexp.MustCompile(`\.\d+$`)

// IsVirtualName reports whether an interface name is one of the virtual kinds.
func IsVirtualName(name string) bool {
	return virtualName.MatchString(name) || subInterface.MatchString(name)
}

// IsLAG reports a link aggregate.
func IsLAG(i IfInfo) bool { return i.Type == ifTypeLAG }

// IsPhysical reports whether an interface is a port a cable plugs into. A
// virtual name wins over anything the agent claims; otherwise
// ifConnectorPresent decides when reported, and an Ethernet ifType when not.
func IsPhysical(i IfInfo) bool {
	if IsLAG(i) || IsVirtualName(i.Name) {
		return false
	}
	if i.ConnectorPresent != nil {
		return *i.ConnectorPresent
	}
	return ethernetTypes[i.Type]
}

// DefaultCollect is whether the stats poll reads an interface unless the user
// says otherwise: physical ports and link aggregates.
func DefaultCollect(i IfInfo) bool { return IsLAG(i) || IsPhysical(i) }
```

Create `backend/internal/portmon/devicetype.go`:

```go
package portmon

import "strings"

// DetectDeviceType guesses switch/router/access_point/nvr/other from the model
// (UniFi and EdgeMax naming) and falls back to the number of physical ports:
// anything with 8 or more is drawn as a switch.
func DetectDeviceType(model string, physicalPorts int) string {
	m := strings.ToUpper(strings.TrimSpace(model))
	has := func(prefixes ...string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(m, p) {
				return true
			}
		}
		return false
	}
	switch {
	case has("USW", "US-", "EDGESWITCH", "ES-"):
		return "switch"
	case has("UDM", "UXG", "USG", "ER-"):
		return "router"
	case has("U6", "U7", "UAP", "UAL", "UK"):
		return "access_point"
	case has("UNVR"):
		return "nvr"
	case physicalPorts >= 8:
		return "switch"
	default:
		return "other"
	}
}
```

Create `backend/internal/portmon/usual.go`:

```go
package portmon

// UsualSpeed picks a port's usual link speed: the speed seen in the most
// 5-minute buckets (counts: speed -> buckets), ties going to the faster speed.
// Returns 0 (unknown, so slow_link never fires) under 24 hours of history.
func UsualSpeed(counts map[int64]int, historyHours float64) int64 {
	if historyHours < 24 {
		return 0
	}
	var best int64
	bestN := 0
	for speed, n := range counts {
		if speed <= 0 {
			continue
		}
		if n > bestN || (n == bestN && speed > best) {
			best, bestN = speed, n
		}
	}
	return best
}
```

- [ ] **Step 6: Implement the layout**

Create `backend/internal/portmon/layout.go`:

```go
package portmon

import (
	"regexp"
	"sort"
	"strconv"
)

// LayoutPort is a physical port to place on the faceplate.
type LayoutPort struct {
	IfIndex int
	Name    string
	Descr   string
}

// FacePort is one port's place: its ifIndex and the number printed by it.
type FacePort struct {
	IfIndex int `json:"if_index"`
	Number  int `json:"number"`
}

// FaceBlock is a group of ports drawn together. With two rows, Top holds the
// 1st, 3rd, 5th... port of the block and Bottom the 2nd, 4th...
type FaceBlock struct {
	SFP    bool       `json:"sfp"`
	Top    []FacePort `json:"top"`
	Bottom []FacePort `json:"bottom"`
}

// Faceplate is the whole front panel, left to right.
type Faceplate struct {
	Rows   int         `json:"rows"`
	Blocks []FaceBlock `json:"blocks"`
}

const (
	blockSize      = 12
	maxOneRowPorts = 8
)

var (
	trailingNumber = regexp.MustCompile(`(\d+)\s*$`)
	portWord       = regexp.MustCompile(`(?i)\bport:?\s*(\d+)`)
	sfpWord        = regexp.MustCompile(`(?i)sfp|\b(10|25|40|100)g\b`)
)

// PortNumber is the number printed next to a port: "Port N" in the name, else
// the trailing number of the name ("0/12", "Gi1/0/12", "eth12"), else "Port: N"
// in the description, else the ifIndex.
func PortNumber(name, descr string, ifIndex int) int {
	for _, m := range [][]string{portWord.FindStringSubmatch(name), trailingNumber.FindStringSubmatch(name), portWord.FindStringSubmatch(descr)} {
		if m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				return n
			}
		}
	}
	return ifIndex
}

// Layout places physical ports. Copper ports are ordered by number and split
// into blocks of 12; SFP ports (by description, or listed in sfpOverride by
// number) form a last block on the right. Eight copper ports or fewer sit in
// one row, more in two; rowsOverride (1 or 2) replaces that choice.
func Layout(ports []LayoutPort, rowsOverride *int, sfpOverride []int) Faceplate {
	sfpSet := map[int]bool{}
	for _, n := range sfpOverride {
		sfpSet[n] = true
	}
	copper, sfp := []FacePort{}, []FacePort{}
	for _, p := range ports {
		fp := FacePort{IfIndex: p.IfIndex, Number: PortNumber(p.Name, p.Descr, p.IfIndex)}
		if sfpSet[fp.Number] || sfpWord.MatchString(p.Name+" "+p.Descr) {
			sfp = append(sfp, fp)
		} else {
			copper = append(copper, fp)
		}
	}
	byNumber := func(ps []FacePort) {
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].Number != ps[j].Number {
				return ps[i].Number < ps[j].Number
			}
			return ps[i].IfIndex < ps[j].IfIndex
		})
	}
	byNumber(copper)
	byNumber(sfp)

	rows := 1
	if len(copper) > maxOneRowPorts {
		rows = 2
	}
	if rowsOverride != nil && (*rowsOverride == 1 || *rowsOverride == 2) {
		rows = *rowsOverride
	}

	blocks := []FaceBlock{}
	for i := 0; i < len(copper); i += blockSize {
		blocks = append(blocks, split(copper[i:min(i+blockSize, len(copper))], rows, false))
	}
	if len(sfp) > 0 {
		blocks = append(blocks, split(sfp, rows, true))
	}
	return Faceplate{Rows: rows, Blocks: blocks}
}

func split(ps []FacePort, rows int, sfp bool) FaceBlock {
	b := FaceBlock{SFP: sfp, Top: []FacePort{}, Bottom: []FacePort{}}
	if rows == 1 {
		b.Top = append(b.Top, ps...)
		return b
	}
	for i, p := range ps {
		if i%2 == 0 {
			b.Top = append(b.Top, p)
		} else {
			b.Bottom = append(b.Bottom, p)
		}
	}
	return b
}
```

- [ ] **Step 7: Run the tests**

Run: `cd backend && go test ./internal/portmon/ -v 2>&1 | grep -E "^(---|ok|FAIL)" | head -20`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/portmon
git commit -m "feat(portmon): classify ports, detect device type, lay out the faceplate

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Port events and conditions tracker (`portmon`)

**Files:**
- Create: `backend/internal/portmon/tracker.go`, `backend/internal/portmon/tracker_test.go`

**Interfaces:**
- Produces:
  - `portmon.Condition` (`LinkDown`, `Errors`, `Flapping`, `SlowLink`, `Saturated`); `WarningConditions`
  - `Thresholds{ErrorsPerMin, UtilPct float64; DownGrace time.Duration}`
  - `Observation{At; OperUp, AdminUp bool; SpeedBps int64; LastChangeSeconds int64; HaveRates bool; ErrorsPerMin, UtilPct float64}`
  - `Event{Kind string; At time.Time; Detail map[string]any}`, `Change{Condition; Started bool; At time.Time; Detail map[string]any}`, `Result{Events []Event; Changes []Change}`
  - `Snapshot{OperUp, AdminUp bool; SpeedBps, LastChangeSeconds int64; OperChangedAt time.Time; Active map[Condition]time.Time}`
  - `NewTracker(Snapshot) *Tracker`, `(*Tracker).Observe(Observation, Thresholds, usualSpeed int64) Result`, `(*Tracker).Active() map[Condition]time.Time`, `(*Tracker).OperChangedAt() time.Time`
- Event kinds (strings): `link_up`, `link_down`, `admin_up`, `admin_down`, `speed_change`. Condition names match `models.PortCondition*`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/portmon/tracker_test.go`:

```go
package portmon

import (
	"testing"
	"time"
)

var th = Thresholds{ErrorsPerMin: 10, UtilPct: 80, DownGrace: 2 * time.Minute}

func upObs(at time.Time) Observation {
	return Observation{At: at, OperUp: true, AdminUp: true, SpeedBps: 1e9, LastChangeSeconds: 500, HaveRates: true, UtilPct: 5}
}

func upTracker() *Tracker {
	return NewTracker(Snapshot{OperUp: true, AdminUp: true, SpeedBps: 1e9, LastChangeSeconds: 500})
}

func changes(r Result, c Condition, started bool) int {
	n := 0
	for _, ch := range r.Changes {
		if ch.Condition == c && ch.Started == started {
			n++
		}
	}
	return n
}

func events(r Result, kind string) int {
	n := 0
	for _, e := range r.Events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func TestTrackerErrorsNeedFivePollsBothWays(t *testing.T) {
	tr := upTracker()
	at := t0
	for i := 0; i < 4; i++ {
		o := upObs(at)
		o.ErrorsPerMin = 50
		if r := tr.Observe(o, th, 0); changes(r, Errors, true) != 0 {
			t.Fatalf("errors started after %d polls", i+1)
		}
		at = at.Add(time.Minute)
	}
	o := upObs(at)
	o.ErrorsPerMin = 50
	if r := tr.Observe(o, th, 0); changes(r, Errors, true) != 1 {
		t.Fatal("errors did not start on the 5th poll at threshold")
	}
	for i := 0; i < 4; i++ {
		at = at.Add(time.Minute)
		if r := tr.Observe(upObs(at), th, 0); changes(r, Errors, false) != 0 {
			t.Fatalf("errors ended after %d quiet polls", i+1)
		}
	}
	at = at.Add(time.Minute)
	if r := tr.Observe(upObs(at), th, 0); changes(r, Errors, false) != 1 {
		t.Fatal("errors did not end after 5 quiet polls")
	}
}

func TestTrackerSaturationHysteresis(t *testing.T) {
	tr := upTracker()
	at := t0
	obs := func(util float64) Result {
		at = at.Add(time.Minute)
		o := upObs(at)
		o.UtilPct = util
		return tr.Observe(o, th, 0)
	}
	for i := 0; i < 4; i++ {
		if r := obs(90); changes(r, Saturated, true) != 0 {
			t.Fatal("saturated before 5 polls")
		}
	}
	if r := obs(90); changes(r, Saturated, true) != 1 {
		t.Fatal("saturated did not start")
	}
	// Averages between 70 and 80 keep it on.
	for i := 0; i < 5; i++ {
		if r := obs(72); changes(r, Saturated, false) != 0 {
			t.Fatal("saturated ended above the 70% clear line")
		}
	}
	if r := obs(20); changes(r, Saturated, false) != 1 {
		t.Fatal("saturated did not end once the 5-poll average fell below 70%")
	}
}

func TestTrackerFlapping(t *testing.T) {
	tr := upTracker()
	down := func(at time.Time) Observation { o := upObs(at); o.OperUp = false; o.HaveRates = false; return o }
	r1 := tr.Observe(down(t0), th, 0)
	r2 := tr.Observe(upObs(t0.Add(time.Minute)), th, 0)
	if events(r1, "link_down") != 1 || events(r2, "link_up") != 1 {
		t.Fatal("first two transitions should log link events")
	}
	r3 := tr.Observe(down(t0.Add(2*time.Minute)), th, 0)
	if changes(r3, Flapping, true) != 1 {
		t.Fatal("third transition within 10 minutes should start flapping")
	}
	// Still bouncing: transitions while flapping are grouped into the flap,
	// not logged one by one.
	if r := tr.Observe(upObs(t0.Add(3*time.Minute)), th, 0); len(r.Events) != 0 {
		t.Fatalf("events while flapping: %+v", r.Events)
	}
	// 10 quiet minutes after the last transition end it, with the count.
	r := tr.Observe(upObs(t0.Add(13*time.Minute+time.Second)), th, 0)
	if changes(r, Flapping, false) != 1 {
		t.Fatal("flapping did not end after 10 quiet minutes")
	}
	for _, c := range r.Changes {
		if c.Condition == Flapping && c.Detail["transitions"] != 4 {
			t.Errorf("flap count %v, want 4", c.Detail["transitions"])
		}
	}
}

// A down-and-up between two polls is invisible in the status but moves
// ifLastChange: it counts as two transitions.
func TestTrackerBounceBetweenPolls(t *testing.T) {
	tr := upTracker()
	o := upObs(t0)
	o.LastChangeSeconds = 900
	r := tr.Observe(o, th, 0)
	if events(r, "link_down") != 1 || events(r, "link_up") != 1 {
		t.Fatalf("hidden bounce events: %+v", r.Events)
	}
	o = upObs(t0.Add(time.Minute))
	o.LastChangeSeconds = 960
	if r := tr.Observe(o, th, 0); changes(r, Flapping, true) != 1 {
		t.Fatal("two hidden bounces (4 transitions) should start flapping")
	}
	// Unknown last-change (e.g. the device restarted) is not a bounce.
	tr = upTracker()
	o = upObs(t0)
	o.LastChangeSeconds = -1
	if r := tr.Observe(o, th, 0); len(r.Events) != 0 {
		t.Fatalf("unknown last change produced events: %+v", r.Events)
	}
}

func TestTrackerLinkDownGrace(t *testing.T) {
	tr := upTracker()
	down := func(at time.Time) Observation { o := upObs(at); o.OperUp = false; o.HaveRates = false; return o }
	if r := tr.Observe(down(t0), th, 0); changes(r, LinkDown, true) != 0 {
		t.Fatal("link_down before the grace period")
	}
	if r := tr.Observe(down(t0.Add(time.Minute)), th, 0); changes(r, LinkDown, true) != 0 {
		t.Fatal("link_down at 1 minute")
	}
	if r := tr.Observe(down(t0.Add(2*time.Minute)), th, 0); changes(r, LinkDown, true) != 1 {
		t.Fatal("link_down not started at the grace period")
	}
	if r := tr.Observe(upObs(t0.Add(3*time.Minute)), th, 0); changes(r, LinkDown, false) != 1 {
		t.Fatal("link_down not ended on link up")
	}
	// An administratively disabled port is not "down".
	tr = upTracker()
	o := down(t0)
	o.AdminUp = false
	r := tr.Observe(o, th, 0)
	o.At = t0.Add(5 * time.Minute)
	r2 := tr.Observe(o, th, 0)
	if changes(r, LinkDown, true)+changes(r2, LinkDown, true) != 0 || events(r, "admin_down") != 1 {
		t.Fatal("admin-down port should log admin_down and never link_down")
	}
}

// A port first seen already down counts its grace from the first sighting.
func TestTrackerFreshPortSeenDown(t *testing.T) {
	tr := NewTracker(Snapshot{OperUp: false, AdminUp: true, LastChangeSeconds: -1})
	down := Observation{At: t0, AdminUp: true, LastChangeSeconds: 100, UtilPct: -1}
	tr.Observe(down, th, 0)
	down.At = t0.Add(2 * time.Minute)
	if r := tr.Observe(down, th, 0); changes(r, LinkDown, true) != 1 {
		t.Fatal("fresh down port never reached link_down")
	}
}

func TestTrackerSlowLinkAndSpeedChange(t *testing.T) {
	tr := upTracker()
	o := upObs(t0)
	o.SpeedBps = 100_000_000
	r := tr.Observe(o, th, 1e9)
	if changes(r, SlowLink, true) != 1 || events(r, "speed_change") != 1 {
		t.Fatalf("slow link: %+v", r)
	}
	if r := tr.Observe(upObs(t0.Add(time.Minute)), th, 1e9); changes(r, SlowLink, false) != 1 {
		t.Fatal("slow_link did not end at the usual speed")
	}
	// No usual speed yet (under 24 h of history): never slow.
	tr = upTracker()
	if r := tr.Observe(o, th, 0); changes(r, SlowLink, true) != 0 {
		t.Fatal("slow_link without a usual speed")
	}
}

func TestTrackerLinkDownEndsTrafficConditions(t *testing.T) {
	tr := NewTracker(Snapshot{OperUp: true, AdminUp: true, SpeedBps: 1e9, LastChangeSeconds: 500,
		Active: map[Condition]time.Time{Errors: t0, Saturated: t0}})
	o := upObs(t0.Add(time.Minute))
	o.OperUp, o.HaveRates = false, false
	r := tr.Observe(o, th, 0)
	if changes(r, Errors, false) != 1 || changes(r, Saturated, false) != 1 {
		t.Fatalf("link down should end traffic conditions: %+v", r.Changes)
	}
}

// Restored after a restart with errors active: one quiet poll neither ends it
// nor starts it again (Review Focus 5).
func TestTrackerRestoredDoesNotRestartOrEnd(t *testing.T) {
	since := t0.Add(-time.Hour)
	tr := NewTracker(Snapshot{OperUp: true, AdminUp: true, SpeedBps: 1e9, LastChangeSeconds: 500,
		Active: map[Condition]time.Time{Errors: since}})
	o := upObs(t0)
	o.ErrorsPerMin = 50
	r := tr.Observe(o, th, 0)
	if len(r.Changes) != 0 {
		t.Fatalf("restored tracker changed state on its first poll: %+v", r.Changes)
	}
	if got := tr.Active()[Errors]; !got.Equal(since) {
		t.Errorf("since = %v, want %v", got, since)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd backend && go test ./internal/portmon/ -run Tracker 2>&1 | tail -3`
Expected: FAIL (`undefined: Thresholds`).

- [ ] **Step 3: Implement the tracker**

Create `backend/internal/portmon/tracker.go`:

```go
package portmon

import (
	"math"
	"time"
)

// Condition is a port problem that has a start and an end.
type Condition string

const (
	LinkDown  Condition = "link_down"
	Errors    Condition = "errors"
	Flapping  Condition = "flapping"
	SlowLink  Condition = "slow_link"
	Saturated Condition = "saturated"
)

// WarningConditions turn a port yellow. LinkDown is shown separately: red for
// an important port, grey otherwise.
var WarningConditions = []Condition{Errors, Flapping, SlowLink, Saturated}

const (
	// Window is how many polls the errors and saturation rules look at.
	Window = 5
	// FlapWindow and FlapThreshold: this many link transitions within the
	// window is flapping; the window passing with none ends it.
	FlapWindow    = 10 * time.Minute
	FlapThreshold = 3
	// SaturationHysteresis: nearly-full ends this many points below the
	// threshold, so a port hovering at the line does not flap the condition.
	SaturationHysteresis = 10.0
)

// Thresholds are one port's limits (its overrides, else the defaults).
type Thresholds struct {
	ErrorsPerMin float64
	UtilPct      float64
	DownGrace    time.Duration
}

// Observation is one stats poll of one port.
type Observation struct {
	At      time.Time
	OperUp  bool
	AdminUp bool
	// SpeedBps is 0 when unknown.
	SpeedBps int64
	// LastChangeSeconds is ifLastChange in seconds of device uptime; < 0 when
	// unknown or not comparable (the device restarted).
	LastChangeSeconds int64
	// HaveRates: ErrorsPerMin and UtilPct come from a valid rate interval.
	HaveRates    bool
	ErrorsPerMin float64 // errors + discards, in + out
	UtilPct      float64 // max of in and out; < 0 when unknown
}

// Event is a point-in-time log entry: link_up, link_down, admin_up,
// admin_down, speed_change.
type Event struct {
	Kind   string
	At     time.Time
	Detail map[string]any
}

// Change is a condition starting or ending.
type Change struct {
	Condition Condition
	Started   bool
	At        time.Time
	Detail    map[string]any
}

// Result is what one observation produced.
type Result struct {
	Events  []Event
	Changes []Change
}

// Snapshot is what a tracker is restored from (the stored interface row).
type Snapshot struct {
	OperUp, AdminUp   bool
	SpeedBps          int64
	LastChangeSeconds int64
	OperChangedAt     time.Time
	Active            map[Condition]time.Time
}

// Tracker follows one port across polls. It is not safe for concurrent use;
// the poller never polls one device twice at once.
type Tracker struct {
	operUp, adminUp bool
	speed           int64
	lastChange      int64
	operChangedAt   time.Time
	transitions     []time.Time
	flapCount       int
	errWindow       []float64
	errBelow        int
	utilWindow      []float64
	active          map[Condition]time.Time
}

// NewTracker restores a tracker. Active conditions carry over with their
// start times; the rolling windows start empty, and since a condition only
// ends on evidence (5 quiet polls, a quiet flap window), a restart never ends
// or re-opens one.
func NewTracker(s Snapshot) *Tracker {
	active := make(map[Condition]time.Time, len(s.Active))
	for c, at := range s.Active {
		active[c] = at
	}
	return &Tracker{
		operUp: s.OperUp, adminUp: s.AdminUp, speed: s.SpeedBps,
		lastChange: s.LastChangeSeconds, operChangedAt: s.OperChangedAt, active: active,
	}
}

// Active returns a copy of the active conditions and when each started.
func (t *Tracker) Active() map[Condition]time.Time {
	out := make(map[Condition]time.Time, len(t.active))
	for c, at := range t.active {
		out[c] = at
	}
	return out
}

// OperChangedAt is when the link last went up or down.
func (t *Tracker) OperChangedAt() time.Time { return t.operChangedAt }

func (t *Tracker) isActive(c Condition) bool {
	_, ok := t.active[c]
	return ok
}

// Observe applies one poll and returns the events and condition changes it
// caused.
func (t *Tracker) Observe(o Observation, th Thresholds, usualSpeed int64) Result {
	var r Result
	event := func(kind string, detail map[string]any) {
		r.Events = append(r.Events, Event{Kind: kind, At: o.At, Detail: detail})
	}
	start := func(c Condition, detail map[string]any) {
		if t.isActive(c) {
			return
		}
		t.active[c] = o.At
		r.Changes = append(r.Changes, Change{Condition: c, Started: true, At: o.At, Detail: detail})
	}
	end := func(c Condition, detail map[string]any) {
		if !t.isActive(c) {
			return
		}
		delete(t.active, c)
		r.Changes = append(r.Changes, Change{Condition: c, Started: false, At: o.At, Detail: detail})
	}

	if o.AdminUp != t.adminUp {
		if o.AdminUp {
			event("admin_up", nil)
		} else {
			event("admin_down", nil)
		}
	}

	// Link transitions. While flapping, individual transitions are counted
	// into the flap, not logged one by one.
	flapping := t.isActive(Flapping)
	transitions := 0
	switch {
	case o.OperUp != t.operUp:
		transitions = 1
		t.operChangedAt = o.At
		if !flapping {
			if o.OperUp {
				event("link_up", map[string]any{"speed_bps": o.SpeedBps})
			} else {
				event("link_down", nil)
			}
		}
	case o.LastChangeSeconds >= 0 && t.lastChange >= 0 && o.LastChangeSeconds != t.lastChange:
		// Same state as last poll, but ifLastChange moved: it went down and
		// came back (or up and down) between polls.
		transitions = 2
		if !flapping {
			between := map[string]any{"between_polls": true}
			if o.OperUp {
				event("link_down", between)
				event("link_up", between)
			} else {
				event("link_up", between)
				event("link_down", between)
			}
		}
	}
	for i := 0; i < transitions; i++ {
		t.transitions = append(t.transitions, o.At)
	}
	if flapping {
		t.flapCount += transitions
	}
	cutoff := o.At.Add(-FlapWindow)
	kept := t.transitions[:0]
	for _, at := range t.transitions {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	t.transitions = kept
	switch {
	case !flapping && len(t.transitions) >= FlapThreshold:
		t.flapCount = len(t.transitions)
		start(Flapping, map[string]any{"transitions": t.flapCount})
	case flapping && len(t.transitions) == 0:
		end(Flapping, map[string]any{"transitions": t.flapCount})
		t.flapCount = 0
	}

	if o.OperUp && t.operUp && transitions == 0 && o.SpeedBps > 0 && t.speed > 0 && o.SpeedBps != t.speed {
		event("speed_change", map[string]any{"from_bps": t.speed, "to_bps": o.SpeedBps})
	}

	// Link down, after the grace period. A port first seen already down counts
	// from that first sighting.
	if !o.OperUp && t.operChangedAt.IsZero() {
		t.operChangedAt = o.At
	}
	switch {
	case !o.AdminUp, o.OperUp:
		end(LinkDown, nil)
	case o.At.Sub(t.operChangedAt) >= th.DownGrace:
		start(LinkDown, map[string]any{"down_since": t.operChangedAt})
	}

	if !o.OperUp {
		// No link, no traffic: traffic conditions end, windows reset.
		end(Errors, nil)
		end(Saturated, nil)
		end(SlowLink, nil)
		t.errWindow, t.utilWindow, t.errBelow = nil, nil, 0
	} else {
		if o.HaveRates {
			t.errWindow = push(t.errWindow, o.ErrorsPerMin)
			if t.isActive(Errors) {
				if o.ErrorsPerMin < th.ErrorsPerMin {
					t.errBelow++
				} else {
					t.errBelow = 0
				}
				if t.errBelow >= Window {
					end(Errors, map[string]any{"per_minute": round1(o.ErrorsPerMin)})
					t.errBelow = 0
				}
			} else if len(t.errWindow) == Window && allAtLeast(t.errWindow, th.ErrorsPerMin) {
				start(Errors, map[string]any{"per_minute": round1(maxOf(t.errWindow))})
				t.errBelow = 0
			}
			if o.UtilPct >= 0 {
				t.utilWindow = push(t.utilWindow, o.UtilPct)
				if len(t.utilWindow) == Window {
					avg := mean(t.utilWindow)
					if !t.isActive(Saturated) && avg >= th.UtilPct {
						start(Saturated, map[string]any{"util_pct": round1(avg)})
					} else if t.isActive(Saturated) && avg < th.UtilPct-SaturationHysteresis {
						end(Saturated, map[string]any{"util_pct": round1(avg)})
					}
				}
			}
		}
		if usualSpeed > 0 && o.SpeedBps > 0 {
			if o.SpeedBps < usualSpeed {
				start(SlowLink, map[string]any{"speed_bps": o.SpeedBps, "usual_speed_bps": usualSpeed})
			} else {
				end(SlowLink, map[string]any{"speed_bps": o.SpeedBps})
			}
		}
	}

	t.operUp, t.adminUp = o.OperUp, o.AdminUp
	if o.SpeedBps > 0 {
		t.speed = o.SpeedBps
	}
	t.lastChange = o.LastChangeSeconds
	return r
}

func push(w []float64, v float64) []float64 {
	w = append(w, v)
	if len(w) > Window {
		w = w[len(w)-Window:]
	}
	return w
}

func allAtLeast(w []float64, min float64) bool {
	for _, v := range w {
		if v < min {
			return false
		}
	}
	return true
}

func mean(w []float64) float64 {
	sum := 0.0
	for _, v := range w {
		sum += v
	}
	return sum / float64(len(w))
}

func maxOf(w []float64) float64 {
	m := math.Inf(-1)
	for _, v := range w {
		m = math.Max(m, v)
	}
	return m
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
```

- [ ] **Step 4: Run the tests**

Run: `cd backend && go test ./internal/portmon/ -v 2>&1 | grep -E "^(---|ok|FAIL)" | head -30`
Expected: all PASS.

Check `TestTrackerFlapping`: transitions happen at t0, +1, +2 and +3 minutes. The flap starts at +2 with 3 transitions, and +3 adds a 4th. By +13m1s all four have left the 10-minute window, so the flap ends with a count of 4. If the count comes out as 3, the `flapping` flag was read after `start()` on the same poll. It must be read before the switch, as written.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/portmon/tracker.go backend/internal/portmon/tracker_test.go
git commit -m "feat(portmon): per-port tracker for link events, flapping and conditions

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 5: Reading port stats; inventory keeps ifX values

**Files:**
- Create: `backend/internal/snmp/stats.go`, `backend/internal/snmp/stats_test.go`
- Modify: `backend/internal/snmp/parse.go`, `backend/internal/snmp/parse_test.go`
- Modify: `backend/internal/services/device_service.go` (`SaveInventory`)
- Create: `backend/internal/services/device_inventory_db_test.go`

**Interfaces:**
- Consumes:
  - `portmon.IfInfo`, `portmon.IsPhysical`, `portmon.DefaultCollect`, `portmon.DetectDeviceType` (Task 3)
  - `models.DeviceInterface` fields (Task 1)
- Produces:
  - `snmp.IfStats{Index int; HC, HaveOctets bool; InOctets, OutOctets, InErrors, OutErrors, InDiscards, OutDiscards uint64; HaveStatus, AdminUp, OperUp bool; LastChangeSeconds int64; SpeedBps int64}`
  - `snmp.StatsOIDs(index int, hc bool) []string`
  - `snmp.ParseStats(pdus []PDU, hc bool) map[int]IfStats`
  - `snmp.ReadStats(ctx, c Client, t Target, indexes []int, hc bool) (map[int]IfStats, error)`
  - `snmp.Interface.HasIfX`, `snmp.Interface.ConnectorPresent *bool`
  - `SaveInventory` now also sets `device_type_detected`, `collect_default`, `connector_present` and `has_ifx`, and keeps name/alias/speed when ifX is missing.

- [ ] **Step 1: Write the failing stats tests**

Create `backend/internal/snmp/stats_test.go`:

```go
package snmp

import (
	"context"
	"errors"
	"testing"
)

func TestStatsOIDs(t *testing.T) {
	hc := StatsOIDs(7, true)
	if len(hc) != 10 || hc[0] != "1.3.6.1.2.1.31.1.1.1.6.7" || hc[1] != "1.3.6.1.2.1.31.1.1.1.10.7" || hc[9] != "1.3.6.1.2.1.31.1.1.1.15.7" {
		t.Errorf("hc oids %v", hc)
	}
	lo := StatsOIDs(7, false)
	if lo[0] != "1.3.6.1.2.1.2.2.1.10.7" || lo[1] != "1.3.6.1.2.1.2.2.1.16.7" || lo[9] != "1.3.6.1.2.1.2.2.1.5.7" {
		t.Errorf("32-bit oids %v", lo)
	}
}

func TestParseStats(t *testing.T) {
	x, e := "1.3.6.1.2.1.31.1.1.1", "1.3.6.1.2.1.2.2.1"
	pdus := []PDU{
		{OID: x + ".6.1", Value: uint64(1_000_000)}, {OID: x + ".10.1", Value: uint64(2_000_000)},
		{OID: e + ".14.1", Value: uint64(3)}, {OID: e + ".20.1", Value: uint64(4)},
		{OID: e + ".13.1", Value: uint64(5)}, {OID: e + ".19.1", Value: uint64(6)},
		{OID: e + ".7.1", Value: int64(1)}, {OID: e + ".8.1", Value: int64(1)},
		{OID: e + ".9.1", Value: uint64(12345)}, {OID: x + ".15.1", Value: uint64(1000)},
		// Port 2: no 64-bit counters (NoSuchInstance), link down.
		{OID: x + ".6.2", Value: nil}, {OID: x + ".10.2", Value: nil},
		{OID: e + ".7.2", Value: int64(1)}, {OID: e + ".8.2", Value: int64(2)},
	}
	got := ParseStats(pdus, true)
	p1 := got[1]
	if !p1.HaveOctets || !p1.HC || p1.InOctets != 1_000_000 || p1.OutOctets != 2_000_000 ||
		p1.InErrors != 3 || p1.OutErrors != 4 || p1.InDiscards != 5 || p1.OutDiscards != 6 ||
		!p1.HaveStatus || !p1.AdminUp || !p1.OperUp || p1.LastChangeSeconds != 123 || p1.SpeedBps != 1_000_000_000 {
		t.Errorf("port 1: %+v", p1)
	}
	p2 := got[2]
	if p2.HaveOctets || !p2.HaveStatus || p2.OperUp || !p2.AdminUp || p2.LastChangeSeconds != -1 {
		t.Errorf("port 2: %+v", p2)
	}

	lo := ParseStats([]PDU{
		{OID: e + ".10.3", Value: uint64(10)}, {OID: e + ".16.3", Value: uint64(20)},
		{OID: e + ".5.3", Value: uint64(100_000_000)}, {OID: e + ".8.3", Value: int64(1)},
	}, false)
	if p3 := lo[3]; !p3.HaveOctets || p3.HC || p3.SpeedBps != 100_000_000 || !p3.OperUp {
		t.Errorf("32-bit port 3: %+v", p3)
	}
}

type countingClient struct {
	gets    int
	perGet  []int
	failOn  int // 1-based GET number that fails; 0 = none
}

func (c *countingClient) Get(_ context.Context, _ Target, oids []string) ([]PDU, error) {
	c.gets++
	c.perGet = append(c.perGet, len(oids))
	if c.gets == c.failOn {
		return nil, errors.New("timeout")
	}
	var out []PDU
	for _, o := range oids {
		out = append(out, PDU{OID: o, Value: uint64(1)})
	}
	return out, nil
}

func (c *countingClient) Walk(context.Context, Target, string) ([]PDU, error) { return nil, nil }

func TestReadStatsBatchesAndKeepsPartialResults(t *testing.T) {
	idx := make([]int, 12)
	for i := range idx {
		idx[i] = i + 1
	}
	c := &countingClient{}
	got, err := ReadStats(context.Background(), c, Target{Credential: Credential{Version: "2c"}}, idx, true)
	if err != nil || len(got) != 12 || c.gets != 3 || c.perGet[0] != 50 {
		t.Errorf("v2c: gets %d per %v len %d err %v", c.gets, c.perGet, len(got), err)
	}

	c = &countingClient{}
	if _, err := ReadStats(context.Background(), c, Target{Credential: Credential{Version: "1"}}, idx, false); err != nil || c.gets != 6 {
		t.Errorf("v1 should use smaller requests: %d gets, err %v", c.gets, err)
	}

	c = &countingClient{failOn: 2}
	got, err = ReadStats(context.Background(), c, Target{Credential: Credential{Version: "2c"}}, idx, true)
	if err == nil || len(got) != 7 {
		t.Errorf("partial: len %d err %v (want 7 ports and the error)", len(got), err)
	}
	for i := 6; i <= 10; i++ {
		if _, ok := got[i]; ok {
			t.Errorf("port %d came from the failed request", i)
		}
	}
}
```

Run: `cd backend && go test ./internal/snmp/ -run 'Stats' 2>&1 | tail -3`
Expected: FAIL (`undefined: StatsOIDs`).

- [ ] **Step 2: Implement stats reading**

Create `backend/internal/snmp/stats.go`:

```go
package snmp

import (
	"context"
	"fmt"
)

// IF-MIB columns the stats poll reads.
const (
	oidIfSpeed       = "1.3.6.1.2.1.2.2.1.5"
	oidIfAdminStatus = "1.3.6.1.2.1.2.2.1.7"
	oidIfOperStatus  = "1.3.6.1.2.1.2.2.1.8"
	oidIfLastChange  = "1.3.6.1.2.1.2.2.1.9"
	oidIfInOctets    = "1.3.6.1.2.1.2.2.1.10"
	oidIfInDiscards  = "1.3.6.1.2.1.2.2.1.13"
	oidIfInErrors    = "1.3.6.1.2.1.2.2.1.14"
	oidIfOutOctets   = "1.3.6.1.2.1.2.2.1.16"
	oidIfOutDiscards = "1.3.6.1.2.1.2.2.1.19"
	oidIfOutErrors   = "1.3.6.1.2.1.2.2.1.20"
	oidIfHCInOctets  = "1.3.6.1.2.1.31.1.1.1.6"
	oidIfHCOutOctets = "1.3.6.1.2.1.31.1.1.1.10"
	oidIfHighSpeed   = "1.3.6.1.2.1.31.1.1.1.15"
)

// IfStats is one interface's counters and state from one stats poll. A value
// the agent did not return leaves its Have* flag false (or LastChangeSeconds
// -1, SpeedBps 0).
type IfStats struct {
	Index                   int
	HC                      bool // octets are the 64-bit ifHC* counters
	HaveOctets              bool // both octet counters were returned
	InOctets, OutOctets     uint64
	InErrors, OutErrors     uint64
	InDiscards, OutDiscards uint64
	HaveStatus              bool // ifOperStatus was returned
	AdminUp, OperUp         bool
	LastChangeSeconds       int64
	SpeedBps                int64
}

// StatsOIDs are the ten OIDs read for one interface: octets (64-bit when hc),
// errors, discards, admin and oper status, last change, speed (ifHighSpeed
// when hc, else ifSpeed).
func StatsOIDs(index int, hc bool) []string {
	in, out, speed := oidIfInOctets, oidIfOutOctets, oidIfSpeed
	if hc {
		in, out, speed = oidIfHCInOctets, oidIfHCOutOctets, oidIfHighSpeed
	}
	cols := []string{in, out, oidIfInErrors, oidIfOutErrors, oidIfInDiscards, oidIfOutDiscards,
		oidIfAdminStatus, oidIfOperStatus, oidIfLastChange, speed}
	oids := make([]string, len(cols))
	for i, c := range cols {
		oids[i] = fmt.Sprintf("%s.%d", c, index)
	}
	return oids
}

// ParseStats reads the answers to StatsOIDs requests, by ifIndex.
func ParseStats(pdus []PDU, hc bool) map[int]IfStats {
	type acc struct {
		st      IfStats
		in, out bool
	}
	byIndex := map[int]*acc{}
	get := func(i int) *acc {
		if byIndex[i] == nil {
			byIndex[i] = &acc{st: IfStats{Index: i, HC: hc, LastChangeSeconds: -1}}
		}
		return byIndex[i]
	}
	for _, p := range pdus {
		n, ok := p.Number()
		if col, idx, found := splitColumn(p.OID, oidIfEntry); found {
			a := get(idx)
			if !ok {
				continue
			}
			switch col {
			case 5:
				if !hc {
					a.st.SpeedBps = int64(n)
				}
			case 7:
				a.st.AdminUp = n == 1
			case 8:
				a.st.OperUp, a.st.HaveStatus = n == 1, true
			case 9:
				a.st.LastChangeSeconds = int64(n / 100)
			case 10:
				if !hc {
					a.st.InOctets, a.in = n, true
				}
			case 13:
				a.st.InDiscards = n
			case 14:
				a.st.InErrors = n
			case 16:
				if !hc {
					a.st.OutOctets, a.out = n, true
				}
			case 19:
				a.st.OutDiscards = n
			case 20:
				a.st.OutErrors = n
			}
			continue
		}
		if col, idx, found := splitColumn(p.OID, oidIfXEntry); found {
			a := get(idx)
			if !ok {
				continue
			}
			switch col {
			case 6:
				if hc {
					a.st.InOctets, a.in = n, true
				}
			case 10:
				if hc {
					a.st.OutOctets, a.out = n, true
				}
			case 15:
				if hc {
					a.st.SpeedBps = int64(n) * 1_000_000
				}
			}
		}
	}
	out := make(map[int]IfStats, len(byIndex))
	for idx, a := range byIndex {
		a.st.HaveOctets = a.in && a.out
		out[idx] = a.st
	}
	return out
}

// interfacesPerGet keeps each request at 50 varbinds (gosnmp caps a request
// at 60), and at 20 for SNMPv1 agents, which are often small radios that
// answer a large request with tooBig.
func interfacesPerGet(version string) int {
	if version == "1" {
		return 2
	}
	return 5
}

// ReadStats reads the given interfaces in batched GETs. A failed batch does
// not stop the rest: the result holds every interface that answered, and the
// first error is returned alongside so the caller can log it.
func ReadStats(ctx context.Context, c Client, t Target, indexes []int, hc bool) (map[int]IfStats, error) {
	per := interfacesPerGet(t.Credential.Version)
	out := make(map[int]IfStats, len(indexes))
	var firstErr error
	for i := 0; i < len(indexes); i += per {
		batch := indexes[i:min(i+per, len(indexes))]
		oids := make([]string, 0, len(batch)*10)
		for _, idx := range batch {
			oids = append(oids, StatsOIDs(idx, hc)...)
		}
		pdus, err := c.Get(ctx, t, oids)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("reading interface counters: %w", err)
			}
			if ctx.Err() != nil {
				break
			}
			continue
		}
		for idx, st := range ParseStats(pdus, hc) {
			out[idx] = st
		}
	}
	return out, firstErr
}
```

Run: `cd backend && go test ./internal/snmp/ -run 'Stats' -v 2>&1 | grep -E "^(---|ok|FAIL)"`
Expected: PASS.

- [ ] **Step 3: Inventory reads ifConnectorPresent and notes ifX presence, test first**

Append to `backend/internal/snmp/parse_test.go`:

```go
// HasIfX marks interfaces the ifXTable answered for, and ifConnectorPresent
// (TruthValue: 1 true, 2 false) is carried when reported.
func TestParseInterfacesIfXPresence(t *testing.T) {
	e, x := "1.3.6.1.2.1.2.2.1", "1.3.6.1.2.1.31.1.1.1"
	got := ParseInterfaces([]PDU{
		{OID: e + ".2.1", Value: []byte("eth0")}, {OID: e + ".3.1", Value: int64(6)},
		{OID: x + ".1.1", Value: []byte("eth0")}, {OID: x + ".17.1", Value: int64(1)},
		{OID: e + ".2.2", Value: []byte("br0")}, {OID: e + ".3.2", Value: int64(6)},
		{OID: x + ".17.2", Value: int64(2)},
		{OID: e + ".2.3", Value: []byte("radio")}, {OID: e + ".3.3", Value: int64(6)},
	})
	if !got[0].HasIfX || got[0].ConnectorPresent == nil || !*got[0].ConnectorPresent {
		t.Errorf("if 1: %+v", got[0])
	}
	if !got[1].HasIfX || got[1].ConnectorPresent == nil || *got[1].ConnectorPresent {
		t.Errorf("if 2: %+v", got[1])
	}
	if got[2].HasIfX || got[2].ConnectorPresent != nil {
		t.Errorf("if 3 (no ifX): %+v", got[2])
	}
}
```

Run: `cd backend && go test ./internal/snmp/ -run IfXPresence 2>&1 | tail -3`
Expected: FAIL (`got[0].HasIfX undefined`).

In `backend/internal/snmp/parse.go`:

1. Change `ifXTableColumns` to include column 17:

```go
	ifXTableColumns = []int{1, 15, 17, 18}          // name, highSpeed, connectorPresent, alias
```

2. Add to `type Interface struct`:

```go
	// HasIfX: the ifXTable answered for this interface in this walk.
	HasIfX bool
	// ConnectorPresent is ifConnectorPresent; nil when not reported.
	ConnectorPresent *bool
```

3. In `ParseInterfaces`, in the `oidIfXEntry` branch, set `it.HasIfX = true` right after `it := get(idx)`, and add a case:

```go
			case 17:
				if n, ok := p.Number(); ok {
					present := n == 1
					it.ConnectorPresent = &present
				}
```

Run: `cd backend && go test ./internal/snmp/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 4: Write the failing SaveInventory database test**

Create `backend/internal/services/device_inventory_db_test.go`:

```go
package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func switchInventory(withIfX bool) snmp.Inventory {
	yes := true
	inv := snmp.Inventory{System: snmp.System{Name: "sw1"}, Vendor: "Ubiquiti (EdgeSwitch)", Model: "USW-Pro-48-PoE"}
	for i := 1; i <= 52; i++ {
		it := snmp.Interface{Index: i, Descr: fmt.Sprintf("Slot: 0 Port: %d Gigabit - Level", i), Type: 6,
			SpeedBps: 100_000_000, OperStatus: "up", AdminStatus: "up"}
		if withIfX {
			it.Name, it.Alias, it.SpeedBps, it.HasIfX, it.ConnectorPresent = fmt.Sprintf("0/%d", i), "desk", 1_000_000_000, true, &yes
		} else {
			it.Name = it.Descr
		}
		inv.Interfaces = append(inv.Interfaces, it)
	}
	inv.Interfaces = append(inv.Interfaces, snmp.Interface{Index: 65, Name: "CPU Interface:  5/1", Type: 1, HasIfX: withIfX})
	return inv
}

func TestDBSaveInventoryKeepsIfXValuesAndClassifies(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	svc := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	ctx := context.Background()

	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))
	// The next walk's ifXTable fails: names, aliases and speeds must survive.
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, switchInventory(false), time.Now()))

	var p1, cpu models.DeviceInterface
	testdb.Must(t, db.First(&p1, "device_id = ? AND if_index = 1", s.DeviceID).Error)
	testdb.Must(t, db.First(&cpu, "device_id = ? AND if_index = 65", s.DeviceID).Error)
	if p1.Name != "0/1" || p1.Alias != "desk" || p1.SpeedBps != 1_000_000_000 {
		t.Errorf("ifX values lost: name %q alias %q speed %d", p1.Name, p1.Alias, p1.SpeedBps)
	}
	if p1.Descr != "Slot: 0 Port: 1 Gigabit - Level" {
		t.Errorf("ifTable values should still update: descr %q", p1.Descr)
	}
	if !p1.CollectDefault || cpu.CollectDefault {
		t.Errorf("collect_default: port %v cpu %v", p1.CollectDefault, cpu.CollectDefault)
	}
	if p1.ConnectorPresent == nil || !*p1.ConnectorPresent {
		t.Errorf("connector_present lost on the ifX-less walk: %v", p1.ConnectorPresent)
	}

	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)
	if d.DeviceTypeDetected != models.DeviceTypeSwitch {
		t.Errorf("device_type_detected = %q", d.DeviceTypeDetected)
	}
}

// Overrides are never written by inventory.
func TestDBSaveInventoryLeavesOverrides(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `UPDATE devices SET model_override = 'UNVR (4-bay)', device_type = 'nvr' WHERE id = ?`, s.DeviceID)
	svc := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	testdb.Must(t, svc.SaveInventory(context.Background(), s.DeviceID, switchInventory(true), time.Now()))
	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)
	if d.ModelOverride == nil || *d.ModelOverride != "UNVR (4-bay)" || d.DeviceType == nil || *d.DeviceType != "nvr" || d.Model != "USW-Pro-48-PoE" {
		t.Errorf("overrides touched: %+v", d)
	}
}
```

Run: `cd backend && make test-db 2>&1 | grep -E "SaveInventory" | head`
Expected: FAIL. The name comes back `"Slot: 0 Port: 1 ..."` and `device_type_detected = "other"`.

- [ ] **Step 5: Update SaveInventory**

In `backend/internal/services/device_service.go`, add the import `"github.com/Stevy2191/Sentinel/backend/internal/portmon"` and replace the body of `SaveInventory` with:

```go
// SaveInventory stores identity and interfaces in one transaction.
// Interfaces are upserted by index; any not in this walk become absent. When
// this walk's ifXTable did not answer for an interface that it answered
// before, the stored name, alias and speed (which came from ifXTable) are
// kept rather than replaced by ifTable's fallbacks. Overrides (vendor_override
// and friends, device_type) are never written here.
func (s *DeviceService) SaveInventory(ctx context.Context, id uuid.UUID, inv snmp.Inventory, at time.Time) error {
	physical := 0
	for _, it := range inv.Interfaces {
		if portmon.IsPhysical(ifInfo(it)) {
			physical++
		}
	}
	detected := portmon.DetectDeviceType(inv.Model, physical)

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sys := inv.System
		if err := tx.Exec(`UPDATE devices SET
			sys_name = ?, sys_descr = ?, sys_object_id = ?, sys_location = ?, sys_contact = ?,
			vendor = ?, model = ?, serial = ?, device_type_detected = ?, last_inventory_at = ?,
			name = CASE WHEN name = host AND ? <> '' THEN ? ELSE name END,
			status_detail = CASE WHEN status_detail LIKE 'inventory:%' THEN '' ELSE status_detail END,
			updated_at = now()
			WHERE id = ?`,
			sys.Name, sys.Descr, sys.ObjectID, sys.Location, sys.Contact,
			inv.Vendor, inv.Model, inv.Serial, detected, at, sys.Name, sys.Name, id).Error; err != nil {
			return fmt.Errorf("saving device identity: %w", err)
		}

		// ifX-sourced columns keep their stored value when this walk has no
		// ifX for the interface but an earlier one did.
		keepIfX := func(col string) clause.Assignment {
			return clause.Assignment{Column: clause.Column{Name: col}, Value: gorm.Expr(
				"CASE WHEN device_interfaces.has_ifx AND NOT EXCLUDED.has_ifx THEN device_interfaces." + col +
					" ELSE EXCLUDED." + col + " END")}
		}
		set := clause.AssignmentColumns([]string{"descr", "if_type", "mac", "admin_status", "oper_status",
			"last_change_seconds", "present", "updated_at", "collect_default"})
		set = append(set,
			keepIfX("name"), keepIfX("alias"), keepIfX("speed_bps"),
			clause.Assignment{Column: clause.Column{Name: "has_ifx"}, Value: gorm.Expr("device_interfaces.has_ifx OR EXCLUDED.has_ifx")},
			clause.Assignment{Column: clause.Column{Name: "connector_present"},
				Value: gorm.Expr("COALESCE(EXCLUDED.connector_present, device_interfaces.connector_present)")},
		)

		seen := make([]int, 0, len(inv.Interfaces))
		for _, it := range inv.Interfaces {
			seen = append(seen, it.Index)
			row := models.DeviceInterface{
				DeviceID: id, IfIndex: it.Index, Name: it.Name, Descr: it.Descr, Alias: it.Alias, IfType: it.Type,
				SpeedBps: it.SpeedBps, MAC: it.MAC, AdminStatus: it.AdminStatus, OperStatus: it.OperStatus,
				LastChangeSeconds: it.LastChangeSeconds, Present: true, UpdatedAt: at,
				ConnectorPresent: it.ConnectorPresent, HasIfX: it.HasIfX,
				CollectDefault: portmon.DefaultCollect(ifInfo(it)),
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "device_id"}, {Name: "if_index"}},
				DoUpdates: set,
			}).Create(&row).Error; err != nil {
				return fmt.Errorf("saving interface %d: %w", it.Index, err)
			}
		}
		q := tx.Model(&models.DeviceInterface{}).Where("device_id = ? AND present", id)
		if len(seen) > 0 {
			q = q.Where("if_index NOT IN ?", seen)
		}
		if err := q.Updates(map[string]any{"present": false, "updated_at": at}).Error; err != nil {
			return fmt.Errorf("marking absent interfaces: %w", err)
		}
		return nil
	})
}

func ifInfo(it snmp.Interface) portmon.IfInfo {
	return portmon.IfInfo{Name: it.Name, Descr: it.Descr, Type: it.Type, ConnectorPresent: it.ConnectorPresent}
}
```

The `CollectDefault` classification uses this walk's name. On an ifX-less walk that name is ifTable's descr fallback, which classifies the same way for every fixture in Task 3, because virtual names are the same in ifDescr on Linux agents.

- [ ] **Step 6: Run the tests**

Run: `cd backend && go test ./... 2>&1 | grep -v "^ok" | head; make test-db 2>&1 | grep -E "^(---|FAIL)" | head`
Expected: no failures. That includes phase 1's `TestDBSaveInventory*` tests, if any exist.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/snmp backend/internal/services/device_service.go backend/internal/services/device_inventory_db_test.go
git commit -m "feat(snmp): read port counters; inventory keeps ifX values and classifies ports

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Port incidents and port notifications

**Files:**
- Modify: `backend/internal/services/incident_service.go`, `backend/internal/services/device_service.go` (availability SQL)
- Modify: `backend/internal/api/incident_handler.go`
- Modify: `backend/internal/notifications/plugins.go`, `backend/internal/notifications/plugins_test.go`
- Create: `backend/internal/services/incident_port_db_test.go`

**Interfaces:**
- Consumes: `models.Incident.InterfaceID/Condition`, `models.Notification.InterfaceID` (Task 1).
- Produces:
  - `(*IncidentService).OpenPortIncident(ctx, deviceID, interfaceID uuid.UUID, condition string, start time.Time, reason string) (*models.Incident, bool, error)`, idempotent
  - `(*IncidentService).ClosePortIncident(ctx, interfaceID uuid.UUID, condition string, end time.Time, note string) (*models.Incident, error)`, which returns nil, nil when none is open
  - `(*IncidentService).ClosePortIncidents(ctx, interfaceID uuid.UUID, end time.Time, note string) (int, error)`
  - `(*IncidentService).OpenPortIncidents(ctx, interfaceID uuid.UUID) ([]models.Incident, error)`
  - `IncidentWithMonitor.PortIfIndex *int` (`json:"port_if_index"`)
  - `notifications.NotificationMessage.InterfaceID *uuid.UUID` and `.PortIfIndex *int`; `ViewPath` returns `/network/devices/<id>/ports/<ifIndex>` for port alerts.

- [ ] **Step 1: Write the failing database tests**

Create `backend/internal/services/incident_port_db_test.go`:

```go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func seedPort(t *testing.T, db *gorm.DB, deviceID uuid.UUID, ifIndex int, name, alias string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name, alias) VALUES (?, ?, ?, ?, ?)`,
		id, deviceID, ifIndex, name, alias)
	return id
}

// Review Focus 1: a port incident is never the device's own incident.
func TestDBPortIncidentsDoNotAffectDeviceIncident(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `UPDATE devices SET created_at = now() - interval '2 days' WHERE id = ?`, s.DeviceID)
	port := seedPort(t, db, s.DeviceID, 51, "0/51", "Uplink")
	svc := NewIncidentService(db)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, opened, err := svc.OpenPortIncident(ctx, s.DeviceID, port, "errors", now.Add(-time.Hour), "errors rising"); err != nil || !opened {
		t.Fatalf("open port incident: opened %v err %v", opened, err)
	}
	dev, opened, err := svc.OpenDeviceIncident(ctx, s.DeviceID, now, "unreachable")
	if err != nil || !opened || dev.InterfaceID != nil {
		t.Fatalf("device incident should open beside a port incident: opened %v err %v inc %+v", opened, err, dev)
	}
	closed, err := svc.CloseDeviceIncident(ctx, s.DeviceID, now.Add(time.Minute), "")
	if err != nil || closed == nil || closed.ID != dev.ID {
		t.Fatalf("device close closed %+v, want the device incident", closed)
	}
	open, err := svc.OpenPortIncidents(ctx, port)
	if err != nil || len(open) != 1 {
		t.Fatalf("port incident should still be open: %v %v", open, err)
	}

	// Availability ignores port incidents: only the 1-minute device outage
	// counts against it.
	devices := NewDeviceService(db, NewSNMPCredentialService(db), svc)
	v, err := devices.Get(ctx, s.DeviceID)
	if err != nil || v.Availability30d == nil || *v.Availability30d < 99.9 {
		t.Fatalf("availability counted the port incident: %v %v", v.Availability30d, err)
	}
}

func TestDBPortIncidentOpenCloseIdempotent(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 3, "0/3", "")
	svc := NewIncidentService(db)
	ctx := context.Background()
	now := time.Now().UTC()

	a, opened, err := svc.OpenPortIncident(ctx, s.DeviceID, port, "link_down", now, "down")
	if err != nil || !opened {
		t.Fatal(err)
	}
	b, opened, err := svc.OpenPortIncident(ctx, s.DeviceID, port, "link_down", now.Add(time.Minute), "down")
	if err != nil || opened || b.ID != a.ID {
		t.Fatalf("second open: opened %v id %v want %v", opened, b.ID, a.ID)
	}
	if _, _, err := svc.OpenPortIncident(ctx, s.DeviceID, port, "saturated", now, "full"); err != nil {
		t.Fatal(err)
	}
	c, err := svc.ClosePortIncident(ctx, port, "link_down", now.Add(2*time.Minute), "")
	if err != nil || c == nil || c.ID != a.ID || c.EndTime == nil || c.DurationSeconds != 120 {
		t.Fatalf("close: %+v %v", c, err)
	}
	if c, err := svc.ClosePortIncident(ctx, port, "link_down", now.Add(3*time.Minute), ""); err != nil || c != nil {
		t.Fatalf("closing nothing: %+v %v", c, err)
	}
	n, err := svc.ClosePortIncidents(ctx, port, now.Add(4*time.Minute), "The port is no longer marked important.")
	if err != nil || n != 1 {
		t.Fatalf("close all: %d %v", n, err)
	}
}

func TestDBIncidentListNamesThePort(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 51, "0/51", "Uplink")
	svc := NewIncidentService(db)
	ctx := context.Background()
	if _, _, err := svc.OpenPortIncident(ctx, s.DeviceID, port, "saturated", time.Now().UTC(), "full"); err != nil {
		t.Fatal(err)
	}
	rows, _, err := svc.ListIncidents(ctx, IncidentListOptions{Viewer: &IncidentViewer{IsAdmin: true}})
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %v", rows, err)
	}
	r := rows[0]
	if r.SubjectType != "device" || r.SubjectName != "dev-10.0.0.2 · 0/51 (Uplink)" || r.PortIfIndex == nil || *r.PortIfIndex != 51 {
		t.Errorf("row: type %q name %q port %v", r.SubjectType, r.SubjectName, r.PortIfIndex)
	}
}
```

Run: `cd backend && make test-db 2>&1 | grep -E "PortIncident|NamesThePort" | head`
Expected: FAIL (`svc.OpenPortIncident undefined`).

- [ ] **Step 2: Scope the device-level queries, and add port incidents**

In `backend/internal/services/incident_service.go`:

1. In `closeDeviceIncident`, change the lookup to `db.Where("device_id = ? AND interface_id IS NULL AND end_time IS NULL", deviceID)`. In `activeDeviceIncident`, change the `Where` to `"device_id = ? AND interface_id IS NULL AND end_time IS NULL"`.

2. Replace the body of `closeDeviceIncident` after `active := rows[0]` so both paths share one helper:

```go
	active := rows[0]
	closed, err := s.closeIncidentRow(db, active, end, note)
	if err != nil {
		return nil, err
	}
	s.logger.Printf("[incident] closed id=%s device=%s duration=%ds", closed.ID, deviceID, closed.DurationSeconds)
	return closed, nil
}

// closeIncidentRow ends one open incident, appending note to its resolution
// notes when given, and returns it reloaded.
func (s *IncidentService) closeIncidentRow(db *gorm.DB, active models.Incident, end time.Time, note string) (*models.Incident, error) {
	if end.Before(active.StartTime) {
		end = active.StartTime
	}
	updates := map[string]any{
		"end_time":         end,
		"duration_seconds": int(end.Sub(active.StartTime).Seconds()),
		"updated_at":       time.Now(),
	}
	if note != "" {
		notes := note
		if active.ResolutionNotes != "" {
			notes = active.ResolutionNotes + "\n" + note
		}
		updates["resolution_notes"] = notes
	}
	if err := db.Model(&models.Incident{}).Where("id = ?", active.ID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("closing incident %s: %w", active.ID, err)
	}
	var closed models.Incident
	if err := db.First(&closed, "id = ?", active.ID).Error; err != nil {
		return nil, fmt.Errorf("reloading incident %s: %w", active.ID, err)
	}
	return &closed, nil
}
```

(`closeDeviceIncident` now keeps its lookup and ends with the call above. Delete the old inline update code.)

3. Append the port functions:

```go
// portIncidentType maps a port condition to the incident_type column.
func portIncidentType(condition string) string {
	if condition == models.PortConditionLinkDown {
		return models.IncidentTypeDown
	}
	return models.IncidentTypeError
}

// OpenPortIncident opens an incident for one condition on one port, unless one
// is already open for that port and condition, in which case that one is
// returned with opened=false. A concurrent open losing the race on the
// partial unique index is reported the same way.
func (s *IncidentService) OpenPortIncident(ctx context.Context, deviceID, interfaceID uuid.UUID, condition string, start time.Time, reason string) (*models.Incident, bool, error) {
	if active, err := s.activePortIncident(ctx, interfaceID, condition); err != nil || active != nil {
		return active, false, err
	}
	now := time.Now()
	cond := condition
	incident := &models.Incident{
		ID: uuid.New(), DeviceID: &deviceID, InterfaceID: &interfaceID, Condition: &cond,
		StartTime: start, Severity: defaultIncidentSeverity, IncidentType: portIncidentType(condition),
		RootCause: reason, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(incident).Error; err != nil {
		if isDuplicateKey(err) {
			active, err := s.activePortIncident(ctx, interfaceID, condition)
			return active, false, err
		}
		return nil, false, fmt.Errorf("creating %s incident for port %s: %w", condition, interfaceID, err)
	}
	s.logger.Printf("[incident] opened id=%s device=%s port=%s condition=%s", incident.ID, deviceID, interfaceID, condition)
	return incident, true, nil
}

// ClosePortIncident closes the open incident for one port and condition.
// Returns nil, nil when none is open.
func (s *IncidentService) ClosePortIncident(ctx context.Context, interfaceID uuid.UUID, condition string, end time.Time, note string) (*models.Incident, error) {
	active, err := s.activePortIncident(ctx, interfaceID, condition)
	if err != nil || active == nil {
		return nil, err
	}
	return s.closeIncidentRow(s.db.WithContext(ctx), *active, end, note)
}

// ClosePortIncidents closes every open incident on a port (it stopped being
// important) and returns how many it closed.
func (s *IncidentService) ClosePortIncidents(ctx context.Context, interfaceID uuid.UUID, end time.Time, note string) (int, error) {
	open, err := s.OpenPortIncidents(ctx, interfaceID)
	if err != nil {
		return 0, err
	}
	for _, inc := range open {
		if _, err := s.closeIncidentRow(s.db.WithContext(ctx), inc, end, note); err != nil {
			return 0, err
		}
	}
	return len(open), nil
}

// OpenPortIncidents lists a port's open incidents, newest first.
func (s *IncidentService) OpenPortIncidents(ctx context.Context, interfaceID uuid.UUID) ([]models.Incident, error) {
	var rows []models.Incident
	err := s.db.WithContext(ctx).Where("interface_id = ? AND end_time IS NULL", interfaceID).
		Order("start_time DESC").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing open incidents for port %s: %w", interfaceID, err)
	}
	return rows, nil
}

func (s *IncidentService) activePortIncident(ctx context.Context, interfaceID uuid.UUID, condition string) (*models.Incident, error) {
	var rows []models.Incident
	err := s.db.WithContext(ctx).
		Where("interface_id = ? AND condition = ? AND end_time IS NULL", interfaceID, condition).
		Limit(1).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("querying open %s incident for port %s: %w", condition, interfaceID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}
```

4. Name the port in the list and detail. Replace `incidentSubjectJoins` and `incidentSubjectSelect` with:

```go
const incidentSubjectJoins = `LEFT JOIN monitors AS m ON m.id = i.monitor_id
	LEFT JOIN devices AS d ON d.id = i.device_id
	LEFT JOIN sites AS st ON st.id = d.site_id
	LEFT JOIN device_interfaces AS di ON di.id = i.interface_id`

// A port incident keeps subject_type 'device' (it inherits the device's
// access and pages) and reads "Device · 0/51 (Alias)" as its subject.
const incidentSubjectSelect = `i.*,
	COALESCE(m.name, d.name) AS monitor_name,
	COALESCE(m.url, d.host) AS monitor_url,
	COALESCE(m.type, 'snmp') AS monitor_type,
	CASE WHEN i.device_id IS NOT NULL THEN 'device' ELSE 'monitor' END AS subject_type,
	CASE WHEN di.id IS NOT NULL THEN
		d.name || ' · ' || COALESCE(NULLIF(di.name, ''), di.if_index::text)
		|| CASE WHEN COALESCE(di.alias, '') <> '' THEN ' (' || di.alias || ')' ELSE '' END
	ELSE COALESCE(m.name, d.name) END AS subject_name,
	COALESCE(m.url, d.host) AS subject_target,
	d.site_id AS site_id,
	COALESCE(st.name, '') AS site_name,
	di.if_index AS port_if_index`
```

and add to `IncidentWithMonitor`:

```go
	// PortIfIndex is set for a port incident, for linking to the port page.
	PortIfIndex *int `json:"port_if_index" gorm:"column:port_if_index"`
```

5. In `backend/internal/services/device_service.go`'s `availabilitySQL`, change `LEFT JOIN incidents i ON i.device_id = d.id` to:

```sql
	LEFT JOIN incidents i ON i.device_id = d.id AND i.interface_id IS NULL
```

- [ ] **Step 3: Refuse manual status changes on port incidents with their own message**

In `backend/internal/api/incident_handler.go`, replace:

```go
		if req.Status != nil && incident.DeviceID != nil {
			respondError(c, http.StatusBadRequest, "device incidents open and close automatically; pause the device to close one")
```

with:

```go
		if req.Status != nil && incident.InterfaceID != nil {
			respondError(c, http.StatusBadRequest, "port incidents close when the problem clears, or when the port is no longer marked important")
			return
		}
		if req.Status != nil && incident.DeviceID != nil {
			respondError(c, http.StatusBadRequest, "device incidents open and close automatically; pause the device to close one")
```

(Keep the existing `return` and closing brace that follow.)

- [ ] **Step 4: Port fields on notifications, test first**

In `backend/internal/notifications/plugins_test.go`, add a case to the `TestViewPathAndHasReport` table:

```go
		{"port", NotificationMessage{MonitorID: monitorID, DeviceID: &deviceID, PortIfIndex: intPtr(51)}, fmt.Sprintf("/network/devices/%s/ports/51", deviceID), false},
```

and at the end of the file:

```go
func intPtr(v int) *int { return &v }
```

Run: `cd backend && go test ./internal/notifications/ -run ViewPath 2>&1 | tail -3`
Expected: FAIL (`unknown field PortIfIndex`).

In `backend/internal/notifications/plugins.go`, add to `NotificationMessage` after `DeviceID`:

```go
	// InterfaceID and PortIfIndex are set on a device alert about one port;
	// the link goes to the port's page.
	InterfaceID *uuid.UUID `json:"interface_id,omitempty"`
	PortIfIndex *int       `json:"port_if_index,omitempty"`
```

Change `ViewPath` to:

```go
func (m *NotificationMessage) ViewPath() string {
	if m.DeviceID != nil {
		if m.PortIfIndex != nil {
			return fmt.Sprintf("/network/devices/%s/ports/%d", *m.DeviceID, *m.PortIfIndex)
		}
		return fmt.Sprintf("/network/devices/%s", *m.DeviceID)
	}
	return fmt.Sprintf("/monitors/%s", m.MonitorID)
}
```

In `StoreNotificationRecord`, in the `case message.DeviceID != nil:` branch, add:

```go
		record.InterfaceID = message.InterfaceID
```

- [ ] **Step 5: Run everything**

Run: `cd backend && go vet ./... && go test ./... 2>&1 | grep -v "^ok" | head; make test-db 2>&1 | grep -E "^(---|FAIL)" | head`
Expected: no failures. Phase 1's device incident tests still pass, since they never set `interface_id`.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/services/incident_service.go backend/internal/services/device_service.go \
  backend/internal/services/incident_port_db_test.go backend/internal/api/incident_handler.go backend/internal/notifications
git commit -m "feat(incidents): port incidents, kept apart from the device's own

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Metrics store, network settings, device deletion

**Files:**
- Create: `backend/internal/services/metrics_catalog.go`, `backend/internal/services/metrics_store.go`
- Create: `backend/internal/services/metrics_store_test.go`, `backend/internal/services/metrics_store_db_test.go`
- Modify: `backend/internal/models/setting.go`, `backend/internal/services/settings_service.go`
- Modify: `backend/internal/services/device_service.go` (`Delete`)

**Interfaces:**
- Consumes: `portmon.Thresholds`, `portmon.UsualSpeed` (Tasks 3–4).
- Produces:
  - Metric key constants and catalogue: `MetricIfInBps`, `MetricIfOutBps`, `MetricIfInUtilPct`, `MetricIfOutUtilPct`, `MetricIfInErrorsPM`, `MetricIfOutErrorsPM`, `MetricIfInDiscardsPM`, `MetricIfOutDiscardsPM`, `MetricIfSpeedBps`; `MetricDef{Key, Unit, Label}`, `MetricCatalogue`, `KnownMetric(string) bool`.
  - `SamplePoint{Metric, Instance string; InterfaceID *uuid.UUID; Value float64}`.
  - `NewMetricsStore(db) *MetricsStore` with:
    - `Write(ctx, deviceID uuid.UUID, at time.Time, points []SamplePoint) error`
    - `Query(ctx, q MetricsQuery) (*MetricsResult, error)`
    - `LatestMany(ctx, deviceIDs []uuid.UUID, metrics []string, since time.Time) (map[uuid.UUID]map[string]map[string]float64, error)`
    - `ApplyRetention(ctx, days int) error`, `RetentionDays(ctx) (int, error)`
    - `Cleanup(ctx, eventRetentionDays int) (CleanupResult, error)`
    - `UpdateUsualSpeeds(ctx, now time.Time) (int, error)`
  - `MetricsQuery{DeviceIDs []uuid.UUID; Metrics, Instances []string; InterfaceIDs []uuid.UUID; From, To time.Time; Sum bool}`
  - `MetricsResult{Resolution string; StepSeconds int; Series []MetricSeries}`, `MetricSeries{DeviceID *uuid.UUID; Metric, Instance string; Points []MetricPoint}`, `MetricPoint{Time time.Time "t"; Avg, Min, Max float64}`.
  - `PickResolution(from, to time.Time) (source string, step time.Duration)`, with source one of `raw`, `5m`, `1h`.
  - `deleteDeviceSeries(tx *gorm.DB, deviceID uuid.UUID) error`.
  - Settings keys: `models.SettingMetricsRawRetentionDays`, `SettingPortErrorThresholdPerMin`, `SettingPortUtilThresholdPct`, `SettingPortDownGraceSeconds`, with defaults and bounds.
  - `(*SettingsService).MetricsRawRetentionDays(ctx) int` and `(*SettingsService).PortThresholds(ctx) portmon.Thresholds`.

- [ ] **Step 1: Settings keys**

In `backend/internal/models/setting.go`, add to the key constants:

```go
	// SettingMetricsRawRetentionDays is how long 1-minute network samples are
	// kept; the 5-minute and hourly rollups are kept forever.
	SettingMetricsRawRetentionDays = "metrics_raw_retention_days"
	// Default port thresholds; each port may override them.
	SettingPortErrorThresholdPerMin = "port_error_threshold_per_min"
	SettingPortUtilThresholdPct     = "port_util_threshold_pct"
	SettingPortDownGraceSeconds     = "port_down_grace_seconds"
```

and to the bounds constants:

```go
	DefaultMetricsRawRetentionDays = 365
	MinMetricsRawRetentionDays     = 7
	MaxMetricsRawRetentionDays     = 3650

	DefaultPortErrorThresholdPerMin = 10
	MinPortErrorThresholdPerMin     = 1
	MaxPortErrorThresholdPerMin     = 1_000_000
	DefaultPortUtilThresholdPct     = 80
	MinPortUtilThresholdPct         = 10
	MaxPortUtilThresholdPct         = 100
	DefaultPortDownGraceSeconds     = 120
	MinPortDownGraceSeconds         = 0
	MaxPortDownGraceSeconds         = 86400
```

In `backend/internal/services/settings_service.go` (import `portmon`), append:

```go
// intSetting reads an int setting, falling back to def when it is unset or
// outside [min, max].
func (s *SettingsService) intSetting(ctx context.Context, key string, def, min, max int) int {
	v := s.GetInt(ctx, key, def)
	if v < min || v > max {
		return def
	}
	return v
}

// MetricsRawRetentionDays is how many days of 1-minute network samples to keep.
func (s *SettingsService) MetricsRawRetentionDays(ctx context.Context) int {
	return s.intSetting(ctx, models.SettingMetricsRawRetentionDays, models.DefaultMetricsRawRetentionDays,
		models.MinMetricsRawRetentionDays, models.MaxMetricsRawRetentionDays)
}

// PortThresholds are the instance-wide port thresholds.
func (s *SettingsService) PortThresholds(ctx context.Context) portmon.Thresholds {
	return portmon.Thresholds{
		ErrorsPerMin: float64(s.intSetting(ctx, models.SettingPortErrorThresholdPerMin, models.DefaultPortErrorThresholdPerMin,
			models.MinPortErrorThresholdPerMin, models.MaxPortErrorThresholdPerMin)),
		UtilPct: float64(s.intSetting(ctx, models.SettingPortUtilThresholdPct, models.DefaultPortUtilThresholdPct,
			models.MinPortUtilThresholdPct, models.MaxPortUtilThresholdPct)),
		DownGrace: time.Duration(s.intSetting(ctx, models.SettingPortDownGraceSeconds, models.DefaultPortDownGraceSeconds,
			models.MinPortDownGraceSeconds, models.MaxPortDownGraceSeconds)) * time.Second,
	}
}
```

Run: `cd backend && go build ./...`
Expected: no output.

- [ ] **Step 2: Write the failing pure test**

Create `backend/internal/services/metrics_store_test.go`:

```go
package services

import (
	"testing"
	"time"
)

func TestPickResolution(t *testing.T) {
	end := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		span   time.Duration
		source string
		step   time.Duration
	}{
		{time.Hour, "raw", time.Minute},
		{6 * time.Hour, "raw", time.Minute},
		{24 * time.Hour, "5m", 5 * time.Minute},
		{7 * 24 * time.Hour, "5m", 25 * time.Minute},
		{30 * 24 * time.Hour, "1h", 2 * time.Hour},
		{90 * 24 * time.Hour, "1h", 5 * time.Hour},
		{365 * 24 * time.Hour, "1h", 18 * time.Hour},
	}
	for _, c := range cases {
		source, step := PickResolution(end.Add(-c.span), end)
		if source != c.source || step != c.step {
			t.Errorf("%v: %s/%v, want %s/%v", c.span, source, step, c.source, c.step)
		}
		if points := int(c.span / step); points > maxQueryPoints {
			t.Errorf("%v: %d points", c.span, points)
		}
	}
}

func TestMetricCatalogue(t *testing.T) {
	for _, k := range []string{"if_in_bps", "if_out_bps", "if_in_util_pct", "if_out_util_pct", "if_in_errors_pm",
		"if_out_errors_pm", "if_in_discards_pm", "if_out_discards_pm", "if_speed_bps"} {
		if !KnownMetric(k) {
			t.Errorf("%s missing from the catalogue", k)
		}
	}
	if KnownMetric("if_in_octets") || len(MetricCatalogue) != 9 {
		t.Error("catalogue must be exactly the spec's nine metrics")
	}
}
```

Run: `cd backend && go test ./internal/services/ -run 'PickResolution|MetricCatalogue' 2>&1 | tail -3`
Expected: FAIL (`undefined: PickResolution`).

- [ ] **Step 3: Implement the catalogue**

Create `backend/internal/services/metrics_catalog.go`:

```go
package services

// Built-in metric keys. Phase 3 adds user-defined metrics beside these; the
// tables never need to change for it.
const (
	MetricIfInBps         = "if_in_bps"
	MetricIfOutBps        = "if_out_bps"
	MetricIfInUtilPct     = "if_in_util_pct"
	MetricIfOutUtilPct    = "if_out_util_pct"
	MetricIfInErrorsPM    = "if_in_errors_pm"
	MetricIfOutErrorsPM   = "if_out_errors_pm"
	MetricIfInDiscardsPM  = "if_in_discards_pm"
	MetricIfOutDiscardsPM = "if_out_discards_pm"
	MetricIfSpeedBps      = "if_speed_bps"
)

// MetricDef describes one metric for the API and the UI.
type MetricDef struct {
	Key   string `json:"key"`
	Unit  string `json:"unit"`
	Label string `json:"label"`
}

var MetricCatalogue = []MetricDef{
	{MetricIfInBps, "bps", "Traffic in"},
	{MetricIfOutBps, "bps", "Traffic out"},
	{MetricIfInUtilPct, "%", "Busy in"},
	{MetricIfOutUtilPct, "%", "Busy out"},
	{MetricIfInErrorsPM, "per_min", "Errors in"},
	{MetricIfOutErrorsPM, "per_min", "Errors out"},
	{MetricIfInDiscardsPM, "per_min", "Discards in"},
	{MetricIfOutDiscardsPM, "per_min", "Discards out"},
	{MetricIfSpeedBps, "bps", "Link speed"},
}

var knownMetrics = func() map[string]bool {
	m := make(map[string]bool, len(MetricCatalogue))
	for _, d := range MetricCatalogue {
		m[d.Key] = true
	}
	return m
}()

// KnownMetric reports whether key is in the catalogue.
func KnownMetric(key string) bool { return knownMetrics[key] }
```

- [ ] **Step 4: Write the failing database tests**

Create `backend/internal/services/metrics_store_db_test.go`:

```go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func refreshRollups(t *testing.T, db *gorm.DB) {
	t.Helper()
	testdb.Exec(t, db, `CALL refresh_continuous_aggregate('metrics.samples_5m', NULL, NULL)`)
	testdb.Exec(t, db, `CALL refresh_continuous_aggregate('metrics.samples_1h', NULL, NULL)`)
}

func TestDBMetricsWriteAndQueryRaw(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	m := NewMetricsStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	for i := 0; i < 3; i++ {
		at := now.Add(time.Duration(i-3) * time.Minute)
		testdb.Must(t, m.Write(ctx, s.DeviceID, at, []SamplePoint{
			{Metric: MetricIfInBps, Instance: "1", Value: float64(100 * (i + 1))},
			{Metric: MetricIfInBps, Instance: "2", Value: 10},
		}))
	}
	res, err := m.Query(ctx, MetricsQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Metrics: []string{MetricIfInBps},
		From: now.Add(-time.Hour), To: now})
	if err != nil || res.Resolution != "raw" || len(res.Series) != 2 {
		t.Fatalf("query: %+v %v", res, err)
	}
	p := res.Series[0].Points
	if res.Series[0].Instance != "1" || len(p) != 3 || p[2].Avg != 300 {
		t.Errorf("instance 1 points: %+v", res.Series[0])
	}

	sum, err := m.Query(ctx, MetricsQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Metrics: []string{MetricIfInBps},
		From: now.Add(-time.Hour), To: now, Sum: true})
	if err != nil || len(sum.Series) != 1 || sum.Series[0].Points[2].Avg != 310 {
		t.Errorf("sum: %+v %v", sum, err)
	}

	if err := m.Write(ctx, s.DeviceID, now, []SamplePoint{{Metric: "bogus", Instance: "1", Value: 1}}); err == nil {
		t.Error("unknown metric accepted")
	}
}

// Rollups keep min/max/sum/count, and the hourly average is weighted, not an
// average of averages.
func TestDBMetricsRollups(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	m := NewMetricsStore(db)
	ctx := context.Background()
	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	// Bucket A (5 samples of 10) and bucket B (1 sample of 70).
	for i := 0; i < 5; i++ {
		testdb.Must(t, m.Write(ctx, s.DeviceID, base.Add(time.Duration(i)*time.Minute),
			[]SamplePoint{{Metric: MetricIfInUtilPct, Instance: "1", Value: 10}}))
	}
	testdb.Must(t, m.Write(ctx, s.DeviceID, base.Add(5*time.Minute), []SamplePoint{{Metric: MetricIfInUtilPct, Instance: "1", Value: 70}}))
	refreshRollups(t, db)

	var hour struct {
		Vmin, Vmax, Vsum float64
		N                int
	}
	testdb.Must(t, db.Raw(`SELECT vmin, vmax, vsum, n FROM metrics.samples_1h WHERE bucket = ?`, base).Scan(&hour).Error)
	if hour.Vmin != 10 || hour.Vmax != 70 || hour.Vsum != 120 || hour.N != 6 {
		t.Fatalf("hourly rollup %+v", hour)
	}

	res, err := m.Query(ctx, MetricsQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Metrics: []string{MetricIfInUtilPct},
		From: base.Add(-24 * time.Hour), To: base.Add(24 * time.Hour)})
	if err != nil || res.Resolution != "5m" {
		t.Fatalf("%+v %v", res, err)
	}
	if len(res.Series) != 1 || len(res.Series[0].Points) == 0 {
		t.Fatalf("no points from the 5-minute rollup: %+v", res)
	}
	// A 48 h range steps by 10 minutes: one step holds all six samples, so
	// avg = 120/6 = 20.
	if res.Series[0].Points[0].Avg != 20 {
		t.Errorf("weighted avg %v, want 20 (not (10+70)/2 = 40)", res.Series[0].Points[0].Avg)
	}
}

func TestDBMetricsRetentionSetting(t *testing.T) {
	db := testdb.Open(t)
	m := NewMetricsStore(db)
	ctx := context.Background()
	testdb.Must(t, m.ApplyRetention(ctx, 30))
	days, err := m.RetentionDays(ctx)
	if err != nil || days != 30 {
		t.Fatalf("retention %d %v", days, err)
	}
}

// Deleting a device removes its series at once and its samples at the next
// cleanup; the cleanup also catches series whose device vanished another way.
func TestDBDeviceDeleteAndCleanup(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	m := NewMetricsStore(db)
	ctx := context.Background()
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: 1}}))
	orphan := uuid.New()
	testdb.Must(t, m.Write(ctx, orphan, time.Now().UTC(), []SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: 1}}))

	devices := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	if _, err := devices.Delete(ctx, s.DeviceID); err != nil {
		t.Fatal(err)
	}
	var series, queued, samples int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.series WHERE device_id = ?`, s.DeviceID).Scan(&series).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.deleted_series`).Scan(&queued).Error)
	if series != 0 || queued != 1 {
		t.Fatalf("after delete: %d series, %d queued", series, queued)
	}

	res, err := m.Cleanup(ctx, 365)
	if err != nil || res.SeriesRemoved != 1 || res.SamplesRemoved != 2 {
		t.Fatalf("cleanup %+v %v", res, err)
	}
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.samples`).Scan(&samples).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.deleted_series`).Scan(&queued).Error)
	if samples != 0 || queued != 0 {
		t.Errorf("after cleanup: %d samples, %d queued", samples, queued)
	}
}

func TestDBUpdateUsualSpeeds(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	long := seedPort(t, db, s.DeviceID, 1, "0/1", "")
	short := seedPort(t, db, s.DeviceID, 2, "0/2", "")
	m := NewMetricsStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(5 * time.Minute)

	// Port 1: 30 hours at 1 Gb/s with an hour at 100 Mb/s. Port 2: 2 hours.
	for at := now.Add(-30 * time.Hour); at.Before(now); at = at.Add(5 * time.Minute) {
		speed := 1e9
		if at.After(now.Add(-2*time.Hour)) && at.Before(now.Add(-time.Hour)) {
			speed = 1e8
		}
		points := []SamplePoint{{Metric: MetricIfSpeedBps, Instance: "1", InterfaceID: &long, Value: speed}}
		if at.After(now.Add(-2 * time.Hour)) {
			points = append(points, SamplePoint{Metric: MetricIfSpeedBps, Instance: "2", InterfaceID: &short, Value: 1e9})
		}
		testdb.Must(t, m.Write(ctx, s.DeviceID, at, points))
	}
	refreshRollups(t, db)
	if _, err := m.UpdateUsualSpeeds(ctx, now); err != nil {
		t.Fatal(err)
	}
	var usual []struct {
		IfIndex       int
		UsualSpeedBps *int64
	}
	testdb.Must(t, db.Raw(`SELECT if_index, usual_speed_bps FROM device_interfaces WHERE device_id = ? ORDER BY if_index`, s.DeviceID).Scan(&usual).Error)
	if usual[0].UsualSpeedBps == nil || *usual[0].UsualSpeedBps != 1_000_000_000 {
		t.Errorf("port 1 usual %v", usual[0].UsualSpeedBps)
	}
	if usual[1].UsualSpeedBps != nil {
		t.Errorf("port 2 has 2 h of history, must stay unset: %v", *usual[1].UsualSpeedBps)
	}
}
```

Run: `cd backend && make test-db 2>&1 | grep -E "TestDB(Metrics|DeviceDelete|UpdateUsual)" | head`
Expected: FAIL (`undefined: NewMetricsStore`).

- [ ] **Step 5: Implement the metrics store**

Create `backend/internal/services/metrics_store.go`:

```go
package services

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

// SamplePoint is one value to write for a device.
type SamplePoint struct {
	Metric   string
	Instance string
	// InterfaceID links a port series to its interface row (no FK: see 048).
	InterfaceID *uuid.UUID
	Value       float64
}

type metricsSample struct {
	Time     time.Time `gorm:"column:time"`
	SeriesID int64     `gorm:"column:series_id"`
	Value    float64   `gorm:"column:value"`
}

type seriesKey struct {
	device           uuid.UUID
	metric, instance string
}

// MetricsStore reads and writes the generic metrics model (migration 048).
type MetricsStore struct {
	db  *gorm.DB
	mu  sync.Mutex
	ids map[seriesKey]int64
}

func NewMetricsStore(db *gorm.DB) *MetricsStore {
	return &MetricsStore{db: db, ids: map[seriesKey]int64{}}
}

func (m *MetricsStore) seriesID(ctx context.Context, deviceID uuid.UUID, p SamplePoint) (int64, error) {
	k := seriesKey{deviceID, p.Metric, p.Instance}
	m.mu.Lock()
	id, ok := m.ids[k]
	m.mu.Unlock()
	if ok {
		return id, nil
	}
	err := m.db.WithContext(ctx).Raw(`INSERT INTO metrics.series (device_id, metric, instance, interface_id)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (device_id, metric, instance)
		DO UPDATE SET interface_id = COALESCE(EXCLUDED.interface_id, metrics.series.interface_id)
		RETURNING id`, deviceID, p.Metric, p.Instance, p.InterfaceID).Scan(&id).Error
	if err != nil {
		return 0, fmt.Errorf("resolving series %s/%s: %w", p.Metric, p.Instance, err)
	}
	m.mu.Lock()
	m.ids[k] = id
	m.mu.Unlock()
	return id, nil
}

// Write stores one poll's points for a device, in one batch. Unknown metrics
// are an error; NaN and infinite values are dropped.
func (m *MetricsStore) Write(ctx context.Context, deviceID uuid.UUID, at time.Time, points []SamplePoint) error {
	rows := make([]metricsSample, 0, len(points))
	for _, p := range points {
		if !KnownMetric(p.Metric) {
			return fmt.Errorf("unknown metric %q", p.Metric)
		}
		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			continue
		}
		id, err := m.seriesID(ctx, deviceID, p)
		if err != nil {
			return err
		}
		rows = append(rows, metricsSample{Time: at, SeriesID: id, Value: p.Value})
	}
	if len(rows) == 0 {
		return nil
	}
	if err := m.db.WithContext(ctx).Table("metrics.samples").CreateInBatches(rows, 500).Error; err != nil {
		return fmt.Errorf("writing %d samples: %w", len(rows), err)
	}
	return nil
}

// deleteDeviceSeries removes a device's series and queues their ids for the
// nightly sample cleanup. Run inside the device deletion's transaction.
func deleteDeviceSeries(tx *gorm.DB, deviceID uuid.UUID) error {
	if err := tx.Exec(`INSERT INTO metrics.deleted_series (series_id)
		SELECT id FROM metrics.series WHERE device_id = ? ON CONFLICT DO NOTHING`, deviceID).Error; err != nil {
		return fmt.Errorf("queueing series for cleanup: %w", err)
	}
	if err := tx.Exec(`DELETE FROM metrics.series WHERE device_id = ?`, deviceID).Error; err != nil {
		return fmt.Errorf("deleting series: %w", err)
	}
	return nil
}

// ---- Query ------------------------------------------------------------------

// MetricsQuery selects series and a time range.
type MetricsQuery struct {
	DeviceIDs []uuid.UUID
	Metrics   []string
	// Instances restricts to these instances (ports' ifIndex); empty = all.
	Instances []string
	// InterfaceIDs restricts to series of these interfaces; empty = all.
	InterfaceIDs []uuid.UUID
	From, To     time.Time
	// Sum adds up every selected series per metric and step (device and site
	// totals). Min and Max then equal Avg: a peak of a sum cannot be derived
	// from per-series rollups.
	Sum bool
}

type MetricPoint struct {
	Time time.Time `json:"t"`
	Avg  float64   `json:"avg"`
	Min  float64   `json:"min"`
	Max  float64   `json:"max"`
}

type MetricSeries struct {
	DeviceID *uuid.UUID    `json:"device_id"`
	Metric   string        `json:"metric"`
	Instance string        `json:"instance"`
	Points   []MetricPoint `json:"points"`
}

type MetricsResult struct {
	Resolution  string         `json:"resolution"`
	StepSeconds int            `json:"step_seconds"`
	Series      []MetricSeries `json:"series"`
}

const maxQueryPoints = 500

// PickResolution chooses the source (raw up to 6 h, 5-minute rollup up to 7
// days, hourly beyond) and a step that is a multiple of the source's bucket
// and keeps a series to about 500 points.
func PickResolution(from, to time.Time) (string, time.Duration) {
	span := to.Sub(from)
	source, base := "1h", time.Hour
	switch {
	case span <= 6*time.Hour:
		source, base = "raw", time.Minute
	case span <= 7*24*time.Hour:
		source, base = "5m", 5*time.Minute
	}
	step := base
	if n := span / maxQueryPoints; n > step {
		step = ((n + base - 1) / base) * base
	}
	return source, step
}

// Query returns the selected series at the resolution PickResolution picks.
func (m *MetricsStore) Query(ctx context.Context, q MetricsQuery) (*MetricsResult, error) {
	source, step := PickResolution(q.From, q.To)
	res := &MetricsResult{Resolution: source, StepSeconds: int(step.Seconds()), Series: []MetricSeries{}}
	if len(q.DeviceIDs) == 0 || len(q.Metrics) == 0 {
		return res, nil
	}

	var inner string
	switch source {
	case "raw":
		inner = `SELECT s.device_id, s.metric, s.instance, time_bucket(make_interval(secs => ?), x.time) AS t,
			avg(x.value) AS avg, min(x.value) AS min, max(x.value) AS max
			FROM metrics.samples x JOIN metrics.series s ON s.id = x.series_id
			WHERE s.device_id IN ? AND s.metric IN ? AND x.time >= ? AND x.time < ?`
	default:
		table := "metrics.samples_5m"
		if source == "1h" {
			table = "metrics.samples_1h"
		}
		inner = `SELECT s.device_id, s.metric, s.instance, time_bucket(make_interval(secs => ?), r.bucket) AS t,
			COALESCE(sum(r.vsum) / NULLIF(sum(r.n), 0), 0) AS avg, min(r.vmin) AS min, max(r.vmax) AS max
			FROM ` + table + ` r JOIN metrics.series s ON s.id = r.series_id
			WHERE s.device_id IN ? AND s.metric IN ? AND r.bucket >= ? AND r.bucket < ?`
	}
	args := []any{step.Seconds(), q.DeviceIDs, q.Metrics, q.From, q.To}
	if len(q.Instances) > 0 {
		inner += " AND s.instance IN ?"
		args = append(args, q.Instances)
	}
	if len(q.InterfaceIDs) > 0 {
		inner += " AND s.interface_id IN ?"
		args = append(args, q.InterfaceIDs)
	}
	inner += " GROUP BY 1, 2, 3, 4"

	sql := inner + " ORDER BY 1, 2, 3, 4"
	if q.Sum {
		sql = `SELECT NULL::uuid AS device_id, metric, '' AS instance, t,
			sum(avg) AS avg, sum(avg) AS min, sum(avg) AS max
			FROM (` + inner + `) b GROUP BY metric, t ORDER BY metric, t`
	}

	var rows []struct {
		DeviceID *uuid.UUID
		Metric   string
		Instance string
		T        time.Time
		Avg      float64
		Min      float64
		Max      float64
	}
	if err := m.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("querying metrics: %w", err)
	}
	for _, r := range rows {
		n := len(res.Series)
		if n == 0 || res.Series[n-1].Metric != r.Metric || res.Series[n-1].Instance != r.Instance ||
			!sameDevice(res.Series[n-1].DeviceID, r.DeviceID) {
			res.Series = append(res.Series, MetricSeries{DeviceID: r.DeviceID, Metric: r.Metric, Instance: r.Instance})
			n++
		}
		res.Series[n-1].Points = append(res.Series[n-1].Points, MetricPoint{Time: r.T, Avg: r.Avg, Min: r.Min, Max: r.Max})
	}
	return res, nil
}

func sameDevice(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// LatestMany returns each series' newest value since `since`, as
// device -> metric -> instance -> value.
func (m *MetricsStore) LatestMany(ctx context.Context, deviceIDs []uuid.UUID, metrics []string, since time.Time) (map[uuid.UUID]map[string]map[string]float64, error) {
	out := map[uuid.UUID]map[string]map[string]float64{}
	if len(deviceIDs) == 0 || len(metrics) == 0 {
		return out, nil
	}
	var rows []struct {
		DeviceID uuid.UUID
		Metric   string
		Instance string
		Value    float64
	}
	err := m.db.WithContext(ctx).Raw(`SELECT DISTINCT ON (x.series_id) s.device_id, s.metric, s.instance, x.value
		FROM metrics.samples x JOIN metrics.series s ON s.id = x.series_id
		WHERE s.device_id IN ? AND s.metric IN ? AND x.time >= ?
		ORDER BY x.series_id, x.time DESC`, deviceIDs, metrics, since).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("reading latest values: %w", err)
	}
	for _, r := range rows {
		if out[r.DeviceID] == nil {
			out[r.DeviceID] = map[string]map[string]float64{}
		}
		if out[r.DeviceID][r.Metric] == nil {
			out[r.DeviceID][r.Metric] = map[string]float64{}
		}
		out[r.DeviceID][r.Metric][r.Instance] = r.Value
	}
	return out, nil
}

// ---- Retention and maintenance ---------------------------------------------

// ApplyRetention replaces the raw-samples retention policy.
func (m *MetricsStore) ApplyRetention(ctx context.Context, days int) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT remove_retention_policy('metrics.samples', if_exists => true)`).Error; err != nil {
			return fmt.Errorf("removing retention policy: %w", err)
		}
		if err := tx.Exec(`SELECT add_retention_policy('metrics.samples', make_interval(days => ?))`, days).Error; err != nil {
			return fmt.Errorf("adding retention policy: %w", err)
		}
		return nil
	})
}

// RetentionDays reads the current raw-samples retention policy.
func (m *MetricsStore) RetentionDays(ctx context.Context) (int, error) {
	var secs float64
	err := m.db.WithContext(ctx).Raw(`SELECT EXTRACT(EPOCH FROM (config->>'drop_after')::interval)
		FROM timescaledb_information.jobs
		WHERE proc_name = 'policy_retention' AND hypertable_schema = 'metrics' AND hypertable_name = 'samples'`).
		Scan(&secs).Error
	if err != nil {
		return 0, fmt.Errorf("reading retention policy: %w", err)
	}
	return int(secs / 86400), nil
}

// CleanupResult says what one nightly cleanup removed.
type CleanupResult struct {
	SeriesRemoved  int64
	SamplesRemoved int64
	EventsRemoved  int64
}

// Cleanup removes series whose device is gone, the samples of every queued
// series, and point events (and ended span events) older than
// eventRetentionDays.
func (m *MetricsStore) Cleanup(ctx context.Context, eventRetentionDays int) (CleanupResult, error) {
	var res CleanupResult
	db := m.db.WithContext(ctx)
	if err := db.Exec(`INSERT INTO metrics.deleted_series (series_id)
		SELECT s.id FROM metrics.series s WHERE NOT EXISTS (SELECT 1 FROM devices d WHERE d.id = s.device_id)
		ON CONFLICT DO NOTHING`).Error; err != nil {
		return res, fmt.Errorf("queueing orphan series: %w", err)
	}
	var queued []int64
	if err := db.Raw(`SELECT series_id FROM metrics.deleted_series ORDER BY series_id`).Scan(&queued).Error; err != nil {
		return res, fmt.Errorf("reading the deleted-series queue: %w", err)
	}
	if len(queued) > 0 {
		r := db.Exec(`DELETE FROM metrics.series WHERE id IN ?`, queued)
		if r.Error != nil {
			return res, fmt.Errorf("deleting orphan series: %w", r.Error)
		}
		res.SeriesRemoved = r.RowsAffected
		r = db.Exec(`DELETE FROM metrics.samples WHERE series_id IN ?`, queued)
		if r.Error != nil {
			return res, fmt.Errorf("deleting samples of removed series: %w", r.Error)
		}
		res.SamplesRemoved = r.RowsAffected
		if err := db.Exec(`DELETE FROM metrics.deleted_series WHERE series_id IN ?`, queued).Error; err != nil {
			return res, fmt.Errorf("clearing the deleted-series queue: %w", err)
		}
		m.mu.Lock()
		m.ids = map[seriesKey]int64{}
		m.mu.Unlock()
	}
	r := db.Exec(`DELETE FROM port_events WHERE started_at < now() - make_interval(days => ?)
		AND (ended_at IS NOT NULL OR kind IN ('link_up', 'link_down', 'speed_change', 'admin_up', 'admin_down'))`,
		eventRetentionDays)
	if r.Error != nil {
		return res, fmt.Errorf("deleting old port events: %w", r.Error)
	}
	res.EventsRemoved = r.RowsAffected
	return res, nil
}

// UpdateUsualSpeeds recomputes every port's usual speed from the last 7 days
// of if_speed_bps 5-minute buckets whose speed held steady (min = max).
// Returns how many ports got a usual speed.
func (m *MetricsStore) UpdateUsualSpeeds(ctx context.Context, now time.Time) (int, error) {
	var rows []struct {
		InterfaceID uuid.UUID
		Speed       float64
		N           int
		First       time.Time
	}
	err := m.db.WithContext(ctx).Raw(`SELECT s.interface_id, r.vmax AS speed, count(*) AS n, min(r.bucket) AS first
		FROM metrics.samples_5m r JOIN metrics.series s ON s.id = r.series_id
		WHERE s.metric = ? AND s.interface_id IS NOT NULL AND r.bucket >= ? AND r.vmin = r.vmax
		GROUP BY s.interface_id, r.vmax`, MetricIfSpeedBps, now.Add(-7*24*time.Hour)).Scan(&rows).Error
	if err != nil {
		return 0, fmt.Errorf("reading speed history: %w", err)
	}
	type hist struct {
		counts map[int64]int
		first  time.Time
	}
	byPort := map[uuid.UUID]*hist{}
	for _, r := range rows {
		h := byPort[r.InterfaceID]
		if h == nil {
			h = &hist{counts: map[int64]int{}, first: r.First}
			byPort[r.InterfaceID] = h
		}
		h.counts[int64(r.Speed)] += r.N
		if r.First.Before(h.first) {
			h.first = r.First
		}
	}
	updated := 0
	for id, h := range byPort {
		usual := portmon.UsualSpeed(h.counts, now.Sub(h.first).Hours())
		if usual <= 0 {
			continue
		}
		if err := m.db.WithContext(ctx).Exec(`UPDATE device_interfaces SET usual_speed_bps = ? WHERE id = ?`, usual, id).Error; err != nil {
			return updated, fmt.Errorf("saving usual speed: %w", err)
		}
		updated++
	}
	return updated, nil
}

// instanceKey is a port series' instance: its ifIndex.
func instanceKey(ifIndex int) string { return strconv.Itoa(ifIndex) }
```

- [ ] **Step 6: Device deletion removes series**

In `backend/internal/services/device_service.go`, replace the delete statement in `Delete`:

```go
	if err := s.db.WithContext(ctx).Delete(&models.Device{}, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("deleting device: %w", err)
	}
```

with:

```go
	// The device's metric series go in the same transaction (metrics has no
	// FKs to cascade them); their samples are removed by the nightly cleanup.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.Device{}, "id = ?", id).Error; err != nil {
			return fmt.Errorf("deleting device: %w", err)
		}
		return deleteDeviceSeries(tx, id)
	})
	if err != nil {
		return nil, err
	}
```

- [ ] **Step 7: Run the tests**

Run: `cd backend && go test ./internal/services/ -run 'PickResolution|MetricCatalogue' -v 2>&1 | grep -E "^(---|ok|FAIL)"; make test-db 2>&1 | grep -E "^(---|FAIL)" | head`
Expected: all PASS.

In `TestDBMetricsRollups`, a 48-hour span picks the 5m source with a step of `ceil(48h/500 / 5m) * 5m` = 10 minutes. The six samples fall into buckets at +0 and +5 of the same hour, and `time_bucket(10m)` puts both in the step at `base`. So `Points[0].Avg` = (5·10 + 70) / 6 = 20. If the step computes differently, fix the test's comment and the expected step, not the store. Record the correction as a ruling.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/services/metrics_catalog.go backend/internal/services/metrics_store.go \
  backend/internal/services/metrics_store_test.go backend/internal/services/metrics_store_db_test.go \
  backend/internal/services/device_service.go backend/internal/services/settings_service.go backend/internal/models/setting.go
git commit -m "feat(metrics): series and samples store with rollups, retention and cleanup

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 8: The port monitor (stats poll) and its store

**Files:**
- Create: `backend/internal/services/port_service.go` (store half; Task 9 adds the API half)
- Create: `backend/internal/services/port_monitor.go`, `backend/internal/services/port_monitor_test.go`, `backend/internal/services/port_monitor_db_test.go`
- Modify: `backend/internal/services/device_poller.go`, `backend/internal/services/device_poller_test.go`

**Interfaces:**
- Consumes:
  - `snmp.ReadStats`, `snmp.IfStats` (Task 5)
  - `portmon.ComputeRates`, `Reading`, `Tracker`, `Snapshot`, `Observation`, `Thresholds`, `PortNumber` (Tasks 2–4)
  - `MetricsStore.Write`, `SamplePoint`, the metric keys and `instanceKey` (Task 7)
  - `IncidentService.OpenPortIncident`/`ClosePortIncident` (Task 6)
  - `SettingsService.PortThresholds` (Task 7)
  - `notifications.NotificationMessage.InterfaceID`/`PortIfIndex` (Task 6)
  - `Notifier`, `displayName` and `humanDuration` from `device_poller.go`
- Produces:
  - `NewPortService(db, metrics *MetricsStore, incidents *IncidentService, settings *SettingsService) *PortService` with `CollectedInterfaces`, `SavePortState`, `RecordPortEvents`, `SaveStatsRun`, `SiteName`
  - `PortStateUpdate`, `PortEventEnd`
  - the interfaces `PortStore`, `PortIncidents`, `MetricsWriter`, `ThresholdSource`
  - `NewPortMonitor(store PortStore, metrics MetricsWriter, incidents PortIncidents, notifier Notifier, client snmp.Client, thresholds ThresholdSource) *PortMonitor`, with `PollStats(ctx, d models.Device, t snmp.Target, uptimeSeconds int64)` and `Forget(deviceID uuid.UUID)`
  - `PortStatsPoller` and `(*DevicePoller).SetPortStats(PortStatsPoller)`
  - `portLabel(models.DeviceInterface) string`

- [ ] **Step 1: Write the store half of PortService**

Create `backend/internal/services/port_service.go`:

```go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// PortService stores port state and events for the PortMonitor, and (Task 9)
// serves ports, events and summaries to the API.
type PortService struct {
	db        *gorm.DB
	metrics   *MetricsStore
	incidents *IncidentService
	settings  *SettingsService
}

func NewPortService(db *gorm.DB, metrics *MetricsStore, incidents *IncidentService, settings *SettingsService) *PortService {
	return &PortService{db: db, metrics: metrics, incidents: incidents, settings: settings}
}

// PortStateUpdate is what one stats poll writes back to an interface.
type PortStateUpdate struct {
	OperStatus, AdminStatus string
	SpeedBps                int64
	// LastChangeSeconds < 0 leaves the stored value.
	LastChangeSeconds int64
	// OperChangedAt, when set, records when the link last changed state.
	OperChangedAt   *time.Time
	Conditions      []string
	ConditionsSince map[string]time.Time
}

// PortEventEnd closes an open span event (flapping, errors, saturated,
// slow_link).
type PortEventEnd struct {
	InterfaceID uuid.UUID
	Kind        string
	At          time.Time
	Detail      map[string]any
}

// CollectedInterfaces are the present interfaces the stats poll reads.
func (s *PortService) CollectedInterfaces(ctx context.Context, deviceID uuid.UUID) ([]models.DeviceInterface, error) {
	var out []models.DeviceInterface
	err := s.db.WithContext(ctx).
		Where("device_id = ? AND present AND COALESCE(collect, collect_default)", deviceID).
		Order("if_index").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing collected interfaces: %w", err)
	}
	return out, nil
}

// SavePortState writes an interface's live state and active conditions.
func (s *PortService) SavePortState(ctx context.Context, interfaceID uuid.UUID, u PortStateUpdate) error {
	updates := map[string]any{
		"oper_status":      u.OperStatus,
		"admin_status":     u.AdminStatus,
		"speed_bps":        u.SpeedBps,
		"conditions":       models.ConditionSet(u.Conditions),
		"conditions_since": models.TimeMap(u.ConditionsSince),
		"updated_at":       time.Now(),
	}
	if u.LastChangeSeconds >= 0 {
		updates["last_change_seconds"] = u.LastChangeSeconds
	}
	if u.OperChangedAt != nil {
		updates["oper_changed_at"] = *u.OperChangedAt
	}
	if err := s.db.WithContext(ctx).Model(&models.DeviceInterface{}).Where("id = ?", interfaceID).Updates(updates).Error; err != nil {
		return fmt.Errorf("saving port state: %w", err)
	}
	return nil
}

// RecordPortEvents inserts new events (span events with no end) and ends
// open span events, in one transaction.
func (s *PortService) RecordPortEvents(ctx context.Context, starts []models.PortEvent, ends []PortEventEnd) error {
	if len(starts) == 0 && len(ends) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(starts) > 0 {
			if err := tx.Create(&starts).Error; err != nil {
				return fmt.Errorf("recording port events: %w", err)
			}
		}
		for _, e := range ends {
			detail, err := json.Marshal(e.Detail)
			if err != nil || e.Detail == nil {
				detail = []byte("{}")
			}
			if err := tx.Exec(`UPDATE port_events SET ended_at = ?, detail = detail || ?::jsonb
				WHERE interface_id = ? AND kind = ? AND ended_at IS NULL`,
				e.At, string(detail), e.InterfaceID, e.Kind).Error; err != nil {
				return fmt.Errorf("ending %s event: %w", e.Kind, err)
			}
		}
		return nil
	})
}

// SaveStatsRun records when a device's stats poll ran and how long it took.
func (s *PortService) SaveStatsRun(ctx context.Context, deviceID uuid.UUID, at time.Time, took time.Duration) error {
	return s.db.WithContext(ctx).Exec(`UPDATE devices SET last_stats_at = ?, last_stats_duration_ms = ? WHERE id = ?`,
		at, int(took.Milliseconds()), deviceID).Error
}

// SiteName names a site for alert text.
func (s *PortService) SiteName(ctx context.Context, siteID uuid.UUID) (string, error) {
	var name string
	err := s.db.WithContext(ctx).Raw("SELECT name FROM sites WHERE id = ?", siteID).Scan(&name).Error
	return name, err
}
```

Run: `cd backend && go build ./...`
Expected: no output.

- [ ] **Step 2: Write the failing unit tests for the monitor**

Create `backend/internal/services/port_monitor_test.go`:

```go
package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// pmSNMP answers GETs from a table of OID -> value; missing OIDs come back
// nil (noSuchInstance), as a v2c agent answers.
type pmSNMP struct {
	mu        sync.Mutex
	values    map[string]any
	requested []string
}

func newPMSNMP() *pmSNMP { return &pmSNMP{values: map[string]any{}} }

func (f *pmSNMP) Get(_ context.Context, _ snmp.Target, oids []string) ([]snmp.PDU, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]snmp.PDU, 0, len(oids))
	for _, o := range oids {
		f.requested = append(f.requested, o)
		out = append(out, snmp.PDU{OID: o, Value: f.values[o]})
	}
	return out, nil
}
func (f *pmSNMP) Walk(context.Context, snmp.Target, string) ([]snmp.PDU, error) { return nil, nil }

// port sets one interface's HC counters and state.
func (f *pmSNMP) port(idx int, in, out uint64, up bool, lastChangeTicks uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, x := "1.3.6.1.2.1.2.2.1", "1.3.6.1.2.1.31.1.1.1"
	oper := int64(1)
	if !up {
		oper = 2
	}
	set := func(base string, col int, v any) { f.values[fmt.Sprintf("%s.%d.%d", base, col, idx)] = v }
	set(x, 6, in)
	set(x, 10, out)
	set(x, 15, uint64(1000))
	set(e, 7, int64(1))
	set(e, 8, oper)
	set(e, 9, lastChangeTicks)
	for _, col := range []int{13, 14, 19, 20} {
		set(e, col, uint64(0))
	}
}

type pmStore struct {
	mu     sync.Mutex
	rows   []models.DeviceInterface
	starts []models.PortEvent
	ends   []PortEventEnd
	runs   int
}

func (s *pmStore) CollectedInterfaces(context.Context, uuid.UUID) ([]models.DeviceInterface, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.DeviceInterface(nil), s.rows...), nil
}
func (s *pmStore) SavePortState(_ context.Context, id uuid.UUID, u PortStateUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		if s.rows[i].ID == id {
			s.rows[i].OperStatus, s.rows[i].AdminStatus, s.rows[i].SpeedBps = u.OperStatus, u.AdminStatus, u.SpeedBps
			s.rows[i].Conditions, s.rows[i].ConditionsSince = u.Conditions, u.ConditionsSince
			if u.LastChangeSeconds >= 0 {
				s.rows[i].LastChangeSeconds = u.LastChangeSeconds
			}
			if u.OperChangedAt != nil {
				s.rows[i].OperChangedAt = u.OperChangedAt
			}
		}
	}
	return nil
}
func (s *pmStore) RecordPortEvents(_ context.Context, starts []models.PortEvent, ends []PortEventEnd) error {
	s.starts = append(s.starts, starts...)
	s.ends = append(s.ends, ends...)
	return nil
}
func (s *pmStore) SaveStatsRun(context.Context, uuid.UUID, time.Time, time.Duration) error {
	s.runs++
	return nil
}
func (s *pmStore) SiteName(context.Context, uuid.UUID) (string, error) { return "HQ", nil }

type pmMetrics struct{ writes [][]SamplePoint }

func (m *pmMetrics) Write(_ context.Context, _ uuid.UUID, _ time.Time, p []SamplePoint) error {
	m.writes = append(m.writes, p)
	return nil
}

func (m *pmMetrics) last(metric, instance string) (float64, bool) {
	if len(m.writes) == 0 {
		return 0, false
	}
	for _, p := range m.writes[len(m.writes)-1] {
		if p.Metric == metric && p.Instance == instance {
			return p.Value, true
		}
	}
	return 0, false
}

type pmIncidents struct {
	open           map[string]*models.Incident
	opens, closes  int
}

func newPMIncidents() *pmIncidents { return &pmIncidents{open: map[string]*models.Incident{}} }

func (f *pmIncidents) OpenPortIncident(_ context.Context, dev, port uuid.UUID, cond string, start time.Time, _ string) (*models.Incident, bool, error) {
	f.opens++
	k := port.String() + cond
	if inc, ok := f.open[k]; ok {
		return inc, false, nil
	}
	inc := &models.Incident{ID: uuid.New(), DeviceID: &dev, InterfaceID: &port, StartTime: start}
	f.open[k] = inc
	return inc, true, nil
}
func (f *pmIncidents) ClosePortIncident(_ context.Context, port uuid.UUID, cond string, end time.Time, _ string) (*models.Incident, error) {
	f.closes++
	k := port.String() + cond
	inc, ok := f.open[k]
	if !ok {
		return nil, nil
	}
	delete(f.open, k)
	inc.EndTime = &end
	inc.DurationSeconds = int(end.Sub(inc.StartTime).Seconds())
	return inc, nil
}

type pmThresholds struct{}

func (pmThresholds) PortThresholds(context.Context) portmon.Thresholds {
	return portmon.Thresholds{ErrorsPerMin: 10, UtilPct: 80, DownGrace: 2 * time.Minute}
}

type pmRig struct {
	m      *PortMonitor
	snmp   *pmSNMP
	store  *pmStore
	mets   *pmMetrics
	incs   *pmIncidents
	notif  *fakeNotifier
	dev    models.Device
	target snmp.Target
	clock  time.Time
}

func newPMRig(important bool) *pmRig {
	r := &pmRig{snmp: newPMSNMP(), store: &pmStore{}, mets: &pmMetrics{}, incs: newPMIncidents(), notif: &fakeNotifier{},
		clock: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	r.dev = models.Device{ID: uuid.New(), SiteID: uuid.New(), Name: "sw1", Host: "10.0.0.2", PollInterval: 60}
	r.target = snmp.Target{Credential: snmp.Credential{Version: "2c"}}
	r.store.rows = []models.DeviceInterface{{ID: uuid.New(), DeviceID: r.dev.ID, IfIndex: 1, Name: "0/1", Alias: "Uplink",
		OperStatus: "up", AdminStatus: "up", SpeedBps: 1e9, LastChangeSeconds: 10, Important: important, CollectDefault: true}}
	r.m = NewPortMonitor(r.store, r.mets, r.incs, r.notif, r.snmp, pmThresholds{})
	r.m.now = func() time.Time { return r.clock }
	return r
}

func (r *pmRig) poll(uptime int64) {
	r.m.PollStats(context.Background(), r.dev, r.target, uptime)
	r.clock = r.clock.Add(time.Minute)
}

func TestPortMonitorWritesRatesOnSecondPoll(t *testing.T) {
	r := newPMRig(false)
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	if _, ok := r.mets.last(MetricIfInBps, "1"); ok {
		t.Fatal("first poll wrote a rate")
	}
	if v, ok := r.mets.last(MetricIfSpeedBps, "1"); !ok || v != 1e9 {
		t.Fatalf("first poll speed %v %v", v, ok)
	}
	r.snmp.port(1, 750_000_000, 75_000_000, true, 1000)
	r.poll(160)
	if v, ok := r.mets.last(MetricIfInBps, "1"); !ok || v != 100e6 {
		t.Fatalf("in bps %v %v", v, ok)
	}
	if v, ok := r.mets.last(MetricIfOutUtilPct, "1"); !ok || v != 1 {
		t.Fatalf("out util %v %v", v, ok)
	}
	if r.store.runs != 2 {
		t.Errorf("stats runs recorded %d", r.store.runs)
	}
}

// Review Focus 2: a reboot between polls writes no rate at all.
func TestPortMonitorRebootWritesNoRates(t *testing.T) {
	r := newPMRig(false)
	r.snmp.port(1, 5_000_000_000, 5_000_000_000, true, 1000)
	r.poll(100_000)
	r.snmp.port(1, 1_000, 1_000, true, 20)
	r.poll(30)
	if _, ok := r.mets.last(MetricIfInBps, "1"); ok {
		t.Fatal("rate written across a reboot")
	}
	if len(r.store.starts) != 0 {
		t.Fatalf("reboot logged port events: %+v", r.store.starts)
	}
}

// A v2c device without ifHC counters is learned once; the next poll asks for
// the 32-bit counters.
func TestPortMonitorLearnsNoHC(t *testing.T) {
	r := newPMRig(false)
	e := "1.3.6.1.2.1.2.2.1"
	r.snmp.values[e+".7.1"], r.snmp.values[e+".8.1"] = int64(1), int64(1)
	r.snmp.values[e+".10.1"], r.snmp.values[e+".16.1"] = uint64(10), uint64(10)
	r.poll(100)
	if len(r.mets.writes) != 0 {
		t.Fatalf("points written while learning: %+v", r.mets.writes)
	}
	r.snmp.requested = nil
	r.poll(160)
	if !strings.Contains(strings.Join(r.snmp.requested, " "), e+".10.1") {
		t.Fatalf("second poll did not read 32-bit counters: %v", r.snmp.requested)
	}
}

func TestPortMonitorImportantPortAlertsAfterGrace(t *testing.T) {
	r := newPMRig(true)
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100) // t0 up
	r.snmp.port(1, 0, 0, false, 7000)
	r.poll(160) // t0+1m down
	r.poll(220) // t0+2m: 1 minute into the grace
	if r.incs.opens != 0 || len(r.notif.sent) != 0 {
		t.Fatalf("alerted inside the grace period: %d opens, %d sent", r.incs.opens, len(r.notif.sent))
	}
	r.poll(280) // t0+3m: grace over
	if len(r.incs.open) != 1 || len(r.notif.sent) != 1 {
		t.Fatalf("after grace: %d open, %d sent", len(r.incs.open), len(r.notif.sent))
	}
	m := r.notif.sent[0]
	if m.Status != "down" || m.PortIfIndex == nil || *m.PortIfIndex != 1 || m.InterfaceID == nil ||
		m.Message != "sw1 port 1 (Uplink) is down." || m.ViewPath() != fmt.Sprintf("/network/devices/%s/ports/1", r.dev.ID) {
		t.Errorf("message %+v", m)
	}
	r.poll(340) // still down: nothing new
	r.snmp.port(1, 0, 0, true, 9000)
	r.poll(400)
	if len(r.incs.open) != 0 || len(r.notif.sent) != 2 || r.notif.sent[1].Status != "recovered" {
		t.Fatalf("recovery: %d open, sent %+v", len(r.incs.open), r.notif.sent)
	}
}

func TestPortMonitorUnimportantPortLogsButNeverAlerts(t *testing.T) {
	r := newPMRig(false)
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	r.snmp.port(1, 0, 0, false, 7000)
	for i := 0; i < 5; i++ {
		r.poll(int64(160 + 60*i))
	}
	if r.incs.opens != 0 || len(r.notif.sent) != 0 {
		t.Fatalf("unimportant port alerted: %d opens", r.incs.opens)
	}
	if len(r.store.starts) != 1 || r.store.starts[0].Kind != models.PortEventLinkDown {
		t.Fatalf("events %+v", r.store.starts)
	}
	if r.store.rows[0].OperStatus != "down" || r.store.rows[0].OperChangedAt == nil {
		t.Errorf("state not saved: %+v", r.store.rows[0])
	}
}

// Review Focus 5: restarted with an active condition and its incident open,
// one quiet poll neither closes it nor alerts again.
func TestPortMonitorRestartKeepsIncident(t *testing.T) {
	r := newPMRig(true)
	since := r.clock.Add(-time.Hour)
	r.store.rows[0].Conditions = models.ConditionSet{"errors"}
	r.store.rows[0].ConditionsSince = models.TimeMap{"errors": since}
	port := r.store.rows[0].ID
	r.incs.open[port.String()+"errors"] = &models.Incident{ID: uuid.New(), StartTime: since}
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	if r.incs.closes != 0 || len(r.incs.open) != 1 || len(r.notif.sent) != 0 {
		t.Fatalf("restart: closes %d open %d sent %d", r.incs.closes, len(r.incs.open), len(r.notif.sent))
	}
}

// A condition already active when a port becomes important gets its incident
// on the next poll.
func TestPortMonitorOpensIncidentForActiveConditionOnNewlyImportantPort(t *testing.T) {
	r := newPMRig(true)
	r.store.rows[0].Conditions = models.ConditionSet{"saturated"}
	r.store.rows[0].ConditionsSince = models.TimeMap{"saturated": r.clock.Add(-10 * time.Minute)}
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	if len(r.incs.open) != 1 || len(r.notif.sent) != 1 || r.notif.sent[0].Status != "warning" {
		t.Fatalf("open %d sent %+v", len(r.incs.open), r.notif.sent)
	}
}

func TestPortMonitorNoChannelsStillOpensIncident(t *testing.T) {
	r := newPMRig(true)
	r.dev.NotifyChannels = models.StringSlice{}
	r.snmp.port(1, 0, 0, true, 1000)
	r.poll(100)
	r.snmp.port(1, 0, 0, false, 7000)
	for i := 0; i < 3; i++ {
		r.poll(int64(160 + 60*i))
	}
	if len(r.incs.open) != 1 || len(r.notif.sent) != 0 {
		t.Fatalf("open %d sent %d", len(r.incs.open), len(r.notif.sent))
	}
}
```

Run: `cd backend && go test ./internal/services/ -run PortMonitor 2>&1 | tail -3`
Expected: FAIL (`undefined: NewPortMonitor`).

- [ ] **Step 3: Implement the monitor**

Create `backend/internal/services/port_monitor.go`:

```go
package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// PortStore is the monitor's view of the database (PortService).
type PortStore interface {
	CollectedInterfaces(ctx context.Context, deviceID uuid.UUID) ([]models.DeviceInterface, error)
	SavePortState(ctx context.Context, interfaceID uuid.UUID, u PortStateUpdate) error
	RecordPortEvents(ctx context.Context, starts []models.PortEvent, ends []PortEventEnd) error
	SaveStatsRun(ctx context.Context, deviceID uuid.UUID, at time.Time, took time.Duration) error
	SiteName(ctx context.Context, siteID uuid.UUID) (string, error)
}

// PortIncidents opens and closes port incidents (IncidentService). Open is
// idempotent per port and condition.
type PortIncidents interface {
	OpenPortIncident(ctx context.Context, deviceID, interfaceID uuid.UUID, condition string, start time.Time, reason string) (*models.Incident, bool, error)
	ClosePortIncident(ctx context.Context, interfaceID uuid.UUID, condition string, end time.Time, note string) (*models.Incident, error)
}

// MetricsWriter stores samples (MetricsStore).
type MetricsWriter interface {
	Write(ctx context.Context, deviceID uuid.UUID, at time.Time, points []SamplePoint) error
}

// ThresholdSource gives the instance-wide port thresholds (SettingsService).
type ThresholdSource interface {
	PortThresholds(ctx context.Context) portmon.Thresholds
}

// PortMonitor runs each device's stats poll: counters to rates to samples,
// link state and conditions to events, and important ports' problems to
// incidents and alerts.
type PortMonitor struct {
	store      PortStore
	metrics    MetricsWriter
	incidents  PortIncidents
	notifier   Notifier
	client     snmp.Client
	thresholds ThresholdSource

	now    func() time.Time
	logger *log.Logger

	mu      sync.Mutex
	devices map[uuid.UUID]*deviceStats
}

// deviceStats is what the monitor remembers about a device between polls.
type deviceStats struct {
	// hc: nil until learned; false when the device has no ifHC counters.
	hc         *bool
	lastUptime int64
	ports      map[uuid.UUID]*portStats
}

type portStats struct {
	last    *portmon.Reading
	tracker *portmon.Tracker
}

func NewPortMonitor(store PortStore, metrics MetricsWriter, incidents PortIncidents, notifier Notifier, client snmp.Client, thresholds ThresholdSource) *PortMonitor {
	return &PortMonitor{
		store: store, metrics: metrics, incidents: incidents, notifier: notifier, client: client, thresholds: thresholds,
		now: time.Now, logger: log.Default(), devices: map[uuid.UUID]*deviceStats{},
	}
}

func (m *PortMonitor) state(deviceID uuid.UUID) *deviceStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	ds := m.devices[deviceID]
	if ds == nil {
		ds = &deviceStats{lastUptime: -1, ports: map[uuid.UUID]*portStats{}}
		m.devices[deviceID] = ds
	}
	return ds
}

// Forget drops what the monitor remembers about a device (it was deleted).
func (m *PortMonitor) Forget(deviceID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.devices, deviceID)
}

func (ds *deviceStats) port(row models.DeviceInterface) *portStats {
	ps := ds.ports[row.ID]
	if ps == nil {
		ps = &portStats{tracker: portmon.NewTracker(snapshotOf(row))}
		ds.ports[row.ID] = ps
	}
	return ps
}

// snapshotOf restores a tracker from the stored interface row.
func snapshotOf(row models.DeviceInterface) portmon.Snapshot {
	active := map[portmon.Condition]time.Time{}
	for _, c := range row.Conditions {
		at := row.ConditionsSince[c]
		if at.IsZero() {
			at = row.UpdatedAt
		}
		active[portmon.Condition(c)] = at
	}
	var changed time.Time
	if row.OperChangedAt != nil {
		changed = *row.OperChangedAt
	}
	return portmon.Snapshot{
		OperUp: row.OperStatus == "up", AdminUp: row.AdminStatus != "down", SpeedBps: row.SpeedBps,
		LastChangeSeconds: row.LastChangeSeconds, OperChangedAt: changed, Active: active,
	}
}

// portThresholds applies a port's overrides to the defaults.
func portThresholds(row models.DeviceInterface, def portmon.Thresholds) portmon.Thresholds {
	th := def
	if row.ErrorThresholdPerMin != nil {
		th.ErrorsPerMin = float64(*row.ErrorThresholdPerMin)
	}
	if row.UtilThresholdPct != nil {
		th.UtilPct = float64(*row.UtilThresholdPct)
	}
	if row.DownGraceSeconds != nil {
		th.DownGrace = time.Duration(*row.DownGraceSeconds) * time.Second
	}
	return th
}

// PollStats reads one device's collected interfaces and applies the results.
// uptimeSeconds is the device's sysUpTime from this poll (< 0 if unknown).
func (m *PortMonitor) PollStats(ctx context.Context, d models.Device, t snmp.Target, uptimeSeconds int64) {
	began := m.now()
	defer func() {
		if err := m.store.SaveStatsRun(ctx, d.ID, began, m.now().Sub(began)); err != nil {
			m.logger.Printf("[snmp] recording stats run for %s: %v", d.Host, err)
		}
	}()

	rows, err := m.store.CollectedInterfaces(ctx, d.ID)
	if err != nil {
		m.logger.Printf("[snmp] listing ports of %s: %v", d.Host, err)
		return
	}
	if len(rows) == 0 {
		return
	}
	ds := m.state(d.ID)
	hc := t.Credential.Version != models.SNMPVersion1 && (ds.hc == nil || *ds.hc)
	indexes := make([]int, len(rows))
	for i, r := range rows {
		indexes[i] = r.IfIndex
	}
	stats, err := snmp.ReadStats(ctx, m.client, t, indexes, hc)
	if err != nil {
		m.logger.Printf("[snmp] stats poll of %s: %v", d.Host, err)
	}
	if len(stats) == 0 {
		return
	}
	if hc && ds.hc == nil {
		learned := false
		for _, st := range stats {
			if st.HaveOctets {
				learned = true
				break
			}
		}
		ds.hc = &learned
		if !learned {
			return // no 64-bit counters: the next poll reads the 32-bit ones
		}
	}

	rebooted := uptimeSeconds >= 0 && ds.lastUptime >= 0 && uptimeSeconds < ds.lastUptime
	ds.lastUptime = uptimeSeconds
	now := m.now()
	def := m.thresholds.PortThresholds(ctx)
	interval := time.Duration(d.PollInterval) * time.Second
	var points []SamplePoint
	var starts []models.PortEvent
	var ends []PortEventEnd
	site := ""

	for _, row := range rows {
		st, ok := stats[row.IfIndex]
		if !ok || !st.HaveStatus {
			continue
		}
		ps := ds.port(row)
		speed := st.SpeedBps
		if speed <= 0 {
			speed = row.SpeedBps
		}

		var rates *portmon.Rates
		if st.HaveOctets {
			cur := portmon.Reading{
				At: now, UptimeSeconds: uptimeSeconds, HC: st.HC,
				InOctets: st.InOctets, OutOctets: st.OutOctets, InErrors: st.InErrors, OutErrors: st.OutErrors,
				InDiscards: st.InDiscards, OutDiscards: st.OutDiscards, SpeedBps: speed,
			}
			if st.OperUp {
				if r, skip := portmon.ComputeRates(ps.last, cur, interval); skip == portmon.SkipNone {
					rates = &r
				}
			}
			ps.last = &cur
		} else {
			ps.last = nil
		}
		points = append(points, portPoints(row, rates, st.OperUp, speed)...)

		obs := portmon.Observation{
			At: now, OperUp: st.OperUp, AdminUp: st.AdminUp, SpeedBps: speed,
			LastChangeSeconds: st.LastChangeSeconds, UtilPct: -1,
		}
		if rebooted {
			obs.LastChangeSeconds = -1
		}
		if rates != nil {
			obs.HaveRates = true
			obs.ErrorsPerMin = rates.InErrorsPM + rates.OutErrorsPM + rates.InDiscardsPM + rates.OutDiscardsPM
			if rates.InUtilPct != nil {
				obs.UtilPct = math.Max(*rates.InUtilPct, *rates.OutUtilPct)
			}
		}
		usual := int64(0)
		if row.UsualSpeedBps != nil {
			usual = *row.UsualSpeedBps
		}
		res := ps.tracker.Observe(obs, portThresholds(row, def), usual)

		if err := m.savePortState(ctx, row, st, speed, obs.LastChangeSeconds, ps.tracker); err != nil {
			m.logger.Printf("[snmp] saving port %d of %s: %v", row.IfIndex, d.Host, err)
		}
		s, e := portEvents(d.ID, row, res)
		starts, ends = append(starts, s...), append(ends, e...)
		if row.Important {
			m.alert(ctx, d, row, ps.tracker, res, &site)
		}
	}

	if err := m.store.RecordPortEvents(ctx, starts, ends); err != nil {
		m.logger.Printf("[snmp] recording port events of %s: %v", d.Host, err)
	}
	if err := m.metrics.Write(ctx, d.ID, now, points); err != nil {
		m.logger.Printf("[snmp] writing samples of %s: %v", d.Host, err)
	}
}

// portPoints are one port's samples for this poll: rates when there are any,
// and the link speed while the link is up.
func portPoints(row models.DeviceInterface, r *portmon.Rates, up bool, speed int64) []SamplePoint {
	id := row.ID
	inst := instanceKey(row.IfIndex)
	var out []SamplePoint
	add := func(metric string, v float64) {
		out = append(out, SamplePoint{Metric: metric, Instance: inst, InterfaceID: &id, Value: v})
	}
	if r != nil {
		add(MetricIfInBps, r.InBps)
		add(MetricIfOutBps, r.OutBps)
		if r.InUtilPct != nil {
			add(MetricIfInUtilPct, *r.InUtilPct)
			add(MetricIfOutUtilPct, *r.OutUtilPct)
		}
		add(MetricIfInErrorsPM, r.InErrorsPM)
		add(MetricIfOutErrorsPM, r.OutErrorsPM)
		add(MetricIfInDiscardsPM, r.InDiscardsPM)
		add(MetricIfOutDiscardsPM, r.OutDiscardsPM)
	}
	if up && speed > 0 {
		add(MetricIfSpeedBps, float64(speed))
	}
	return out
}

func (m *PortMonitor) savePortState(ctx context.Context, row models.DeviceInterface, st snmp.IfStats, speed, lastChange int64, tr *portmon.Tracker) error {
	oper, admin := "down", "down"
	if st.OperUp {
		oper = "up"
	}
	if st.AdminUp {
		admin = "up"
	}
	active := tr.Active()
	conds := make([]string, 0, len(active))
	since := make(map[string]time.Time, len(active))
	for c, at := range active {
		conds = append(conds, string(c))
		since[string(c)] = at
	}
	sort.Strings(conds)
	stored := append([]string(nil), row.Conditions...)
	sort.Strings(stored)
	operChanged := row.OperStatus != oper
	if !operChanged && row.AdminStatus == admin && row.SpeedBps == speed &&
		(lastChange < 0 || lastChange == row.LastChangeSeconds) && equalStrings(conds, stored) {
		return nil
	}
	u := PortStateUpdate{OperStatus: oper, AdminStatus: admin, SpeedBps: speed, LastChangeSeconds: lastChange,
		Conditions: conds, ConditionsSince: since}
	if at := tr.OperChangedAt(); operChanged && !at.IsZero() {
		u.OperChangedAt = &at
	}
	return m.store.SavePortState(ctx, row.ID, u)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// spanKinds are the conditions logged as span events; LinkDown is already
// logged by its link_down event.
var spanKinds = map[portmon.Condition]string{
	portmon.Flapping:  models.PortEventFlapping,
	portmon.Errors:    models.PortEventErrors,
	portmon.Saturated: models.PortEventSaturated,
	portmon.SlowLink:  models.PortEventSlowLink,
}

func portEvents(deviceID uuid.UUID, row models.DeviceInterface, res portmon.Result) ([]models.PortEvent, []PortEventEnd) {
	var starts []models.PortEvent
	var ends []PortEventEnd
	for _, e := range res.Events {
		starts = append(starts, models.PortEvent{DeviceID: deviceID, InterfaceID: row.ID, IfIndex: row.IfIndex,
			Kind: e.Kind, StartedAt: e.At, Detail: e.Detail})
	}
	for _, c := range res.Changes {
		kind, ok := spanKinds[c.Condition]
		if !ok {
			continue
		}
		if c.Started {
			starts = append(starts, models.PortEvent{DeviceID: deviceID, InterfaceID: row.ID, IfIndex: row.IfIndex,
				Kind: kind, StartedAt: c.At, Detail: c.Detail})
		} else {
			ends = append(ends, PortEventEnd{InterfaceID: row.ID, Kind: kind, At: c.At, Detail: c.Detail})
		}
	}
	return starts, ends
}

// alert turns an important port's condition changes into incidents and
// notifications. Conditions active without a change this poll are made sure
// of too (idempotently): that retries an open that failed to save, and gives
// a port just marked important an incident for what is already wrong.
func (m *PortMonitor) alert(ctx context.Context, d models.Device, row models.DeviceInterface, tr *portmon.Tracker, res portmon.Result, site *string) {
	handled := map[portmon.Condition]bool{}
	for _, c := range res.Changes {
		handled[c.Condition] = true
		cond := string(c.Condition)
		if c.Started {
			inc, opened, err := m.incidents.OpenPortIncident(ctx, d.ID, row.ID, cond, c.At.UTC(), portProblem(d, row, c))
			if err != nil {
				m.logger.Printf("[snmp] opening %s incident for %s port %d: %v", cond, d.Host, row.IfIndex, err)
				continue
			}
			if opened {
				m.notifyPort(ctx, d, row, c, inc, site)
			}
			continue
		}
		inc, err := m.incidents.ClosePortIncident(ctx, row.ID, cond, c.At.UTC(), "")
		if err != nil {
			m.logger.Printf("[snmp] closing %s incident for %s port %d: %v", cond, d.Host, row.IfIndex, err)
			continue
		}
		if inc != nil {
			m.notifyPort(ctx, d, row, c, inc, site)
		}
	}
	for c, since := range tr.Active() {
		if handled[c] {
			continue
		}
		ch := portmon.Change{Condition: c, Started: true, At: since}
		inc, opened, err := m.incidents.OpenPortIncident(ctx, d.ID, row.ID, string(c), since.UTC(), portProblem(d, row, ch))
		if err != nil {
			m.logger.Printf("[snmp] ensuring %s incident for %s port %d: %v", c, d.Host, row.IfIndex, err)
			continue
		}
		if opened {
			m.notifyPort(ctx, d, row, ch, inc, site)
		}
	}
}

// portLabel names a port for people: "port 51 (Uplink To Quantum Gate)".
func portLabel(row models.DeviceInterface) string {
	n := portmon.PortNumber(row.Name, row.Descr, row.IfIndex)
	if row.Alias != "" {
		return fmt.Sprintf("port %d (%s)", n, row.Alias)
	}
	return fmt.Sprintf("port %d", n)
}

var conditionNoun = map[portmon.Condition]string{
	portmon.LinkDown: "link down", portmon.Errors: "errors", portmon.Flapping: "flapping",
	portmon.SlowLink: "slow link", portmon.Saturated: "nearly full",
}

// portProblem is the alert sentence for a condition starting. Detail values
// come from the tracker; a retried open has none and gets the short form.
func portProblem(d models.Device, row models.DeviceInterface, c portmon.Change) string {
	who := displayName(d) + " " + portLabel(row)
	num := func(key string) (string, bool) {
		switch v := c.Detail[key].(type) {
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64), true
		case int:
			return strconv.Itoa(v), true
		case int64:
			return strconv.FormatInt(v, 10), true
		}
		return "", false
	}
	speed := func(key string) (string, bool) {
		if v, ok := c.Detail[key].(int64); ok {
			return formatBitsPerSecond(float64(v)), true
		}
		return "", false
	}
	switch c.Condition {
	case portmon.LinkDown:
		return who + " is down."
	case portmon.Errors:
		if n, ok := num("per_minute"); ok {
			return fmt.Sprintf("%s is logging errors: %s per minute.", who, n)
		}
		return who + " is logging errors."
	case portmon.Flapping:
		if n, ok := num("transitions"); ok {
			return fmt.Sprintf("%s is flapping: %s link changes in 10 minutes.", who, n)
		}
		return who + " is flapping."
	case portmon.SlowLink:
		now, ok1 := speed("speed_bps")
		usual, ok2 := speed("usual_speed_bps")
		if ok1 && ok2 {
			return fmt.Sprintf("%s linked at %s instead of its usual %s.", who, now, usual)
		}
		return who + " linked slower than usual."
	case portmon.Saturated:
		if n, ok := num("util_pct"); ok {
			return fmt.Sprintf("%s is nearly full: %s%% busy.", who, n)
		}
		return who + " is nearly full."
	}
	return who + " has a problem."
}

func formatBitsPerSecond(bps float64) string {
	switch {
	case bps >= 1e9:
		return strconv.FormatFloat(bps/1e9, 'f', -1, 64) + " Gb/s"
	case bps >= 1e6:
		return strconv.FormatFloat(bps/1e6, 'f', -1, 64) + " Mb/s"
	default:
		return strconv.FormatFloat(bps/1e3, 'f', -1, 64) + " kb/s"
	}
}

func (m *PortMonitor) notifyPort(ctx context.Context, d models.Device, row models.DeviceInterface, c portmon.Change, inc *models.Incident, site *string) {
	// Existing convention: nil = every enabled channel, [] = none.
	if d.NotifyChannels != nil && len(d.NotifyChannels) == 0 {
		return
	}
	if *site == "" {
		name, err := m.store.SiteName(ctx, d.SiteID)
		if err != nil || name == "" {
			name = "its site"
		}
		*site = name
	}
	id, ifID, ifIndex := d.ID, row.ID, row.IfIndex
	label := portLabel(row)
	msg := &notifications.NotificationMessage{
		DeviceID: &id, InterfaceID: &ifID, PortIfIndex: &ifIndex, SiteName: *site,
		MonitorName: displayName(d) + " " + label, MonitorURL: d.Host, Timestamp: c.At,
	}
	if d.NotifyChannels != nil {
		msg.Channels = []string(d.NotifyChannels)
	}
	if inc != nil {
		msg.IncidentID = &inc.ID
	}
	status := "warning"
	if c.Condition == portmon.LinkDown {
		status = "down"
	}
	if c.Started {
		msg.Status, msg.PreviousStatus = status, "up"
		msg.Message = portProblem(d, row, c)
	} else {
		dur := time.Duration(0)
		if inc != nil {
			dur = time.Duration(inc.DurationSeconds) * time.Second
		}
		msg.Status, msg.PreviousStatus, msg.DowntimeDuration = "recovered", status, dur
		msg.Message = fmt.Sprintf("%s %s: %s cleared after %s.", displayName(d), label, conditionNoun[c.Condition], humanDuration(dur))
	}
	if err := m.notifier.SendNotification(ctx, msg); err != nil {
		m.logger.Printf("[snmp] sending %s notification for %s %s: %v", msg.Status, d.Host, label, err)
	}
}
```

- [ ] **Step 4: Run the unit tests**

Run: `cd backend && go test ./internal/services/ -run PortMonitor -v 2>&1 | grep -E "^(---|ok|FAIL)"`
Expected: all PASS.

In `TestPortMonitorImportantPortAlertsAfterGrace` the port goes down at poll 2 (t0+1m). The grace is 2 minutes, so the incident opens at poll 4 (t0+3m), and the message expects "port 1": the fixture's name is `0/1`. If the recovery test fails because `ClosePortIncident` returned nil, check that `alert` passes the condition string `"link_down"` (`string(portmon.LinkDown)`) to both open and close.

- [ ] **Step 5: Hand successful polls to the monitor, test first**

Append to `backend/internal/services/device_poller_test.go`:

```go
type fakeStats struct {
	calls  int
	uptime int64
}

func (f *fakeStats) PollStats(_ context.Context, _ models.Device, _ snmp.Target, uptime int64) {
	f.calls++
	f.uptime = uptime
}

// The stats poll runs after a poll that leaves the device up, never after a
// failure, and gets this poll's sysUpTime.
func TestPollerRunsStatsOnlyWhenUp(t *testing.T) {
	p, _, _, _ := newTestPoller(&fakeSNMP{})
	stats := &fakeStats{}
	p.SetPortStats(stats)
	p.PollOnce(context.Background(), device(models.DeviceStatusUp, 0))
	if stats.calls != 1 || stats.uptime != 3600 {
		t.Fatalf("up: calls %d uptime %d", stats.calls, stats.uptime)
	}
	p.PollOnce(context.Background(), device(models.DeviceStatusPending, 0))
	if stats.calls != 2 {
		t.Fatalf("pending -> up should run stats: %d", stats.calls)
	}

	p, _, _, _ = newTestPoller(&fakeSNMP{getErr: errors.New("timeout")})
	stats = &fakeStats{}
	p.SetPortStats(stats)
	p.PollOnce(context.Background(), device(models.DeviceStatusUp, 0))
	if stats.calls != 0 {
		t.Fatal("stats ran after a failed poll")
	}
}
```

Run: `cd backend && go test ./internal/services/ -run RunsStats 2>&1 | tail -3`
Expected: FAIL (`p.SetPortStats undefined`).

In `backend/internal/services/device_poller.go`:

1. Add after the `Notifier` interface:

```go
// PortStatsPoller runs a device's stats poll (PortMonitor).
type PortStatsPoller interface {
	PollStats(ctx context.Context, d models.Device, t snmp.Target, uptimeSeconds int64)
}
```

2. Add a field to `DevicePoller`: `stats PortStatsPoller`, and this method:

```go
// SetPortStats makes every successful poll of an up device run its stats
// poll too.
func (p *DevicePoller) SetPortStats(s PortStatsPoller) { p.stats = s }
```

3. In `PollOnce`, directly after `p.apply(ctx, d, tr, detail, now)`:

```go
	if result == PollOK && tr.Status == models.DeviceStatusUp && p.stats != nil {
		up := int64(-1)
		if uptime != nil {
			up = *uptime
		}
		p.stats.PollStats(ctx, d, target, up)
	}
```

Run: `cd backend && go test ./internal/services/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 6: Database test for the whole path**

Create `backend/internal/services/port_monitor_db_test.go`:

```go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// The real store, metrics and incidents behind a scripted switch: samples,
// port state, events, an incident on the important port, and the stats run
// timing all land in the database.
func TestDBPortMonitorEndToEnd(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	ctx := context.Background()
	settings := NewSettingsService(db)
	incidents := NewIncidentService(db)
	devices := NewDeviceService(db, NewSNMPCredentialService(db), incidents)
	testdb.Must(t, devices.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))
	testdb.Exec(t, db, `UPDATE device_interfaces SET important = true WHERE device_id = ? AND if_index = 1`, s.DeviceID)

	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, incidents, settings)
	fake := newPMSNMP()
	notif := &fakeNotifier{}
	m := NewPortMonitor(ports, metrics, incidents, notif, fake, settings)
	clock := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Minute)
	m.now = func() time.Time { return clock }
	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)
	d.PollInterval = 60
	target := snmp.Target{Credential: snmp.Credential{Version: "2c"}}

	// Inventory stored last_change_seconds = 0; keep it there while up so the
	// first poll is not read as a bounce between polls.
	fake.port(1, 0, 0, true, 0)
	m.PollStats(ctx, d, target, 100)
	clock = clock.Add(time.Minute)
	fake.port(1, 750_000_000, 1_000, true, 0)
	m.PollStats(ctx, d, target, 160)
	fake.port(1, 750_000_000, 1_000, false, 9000)
	for i := 0; i < 4; i++ {
		clock = clock.Add(time.Minute)
		m.PollStats(ctx, d, target, int64(220+60*i))
	}

	var series int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.series WHERE device_id = ? AND metric = 'if_in_bps' AND instance = '1'`, s.DeviceID).Scan(&series).Error)
	if series != 1 {
		t.Errorf("if_in_bps series: %d", series)
	}
	var port models.DeviceInterface
	testdb.Must(t, db.First(&port, "device_id = ? AND if_index = 1", s.DeviceID).Error)
	if port.OperStatus != "down" || port.OperChangedAt == nil {
		t.Errorf("port state %q changed %v", port.OperStatus, port.OperChangedAt)
	}
	var events int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM port_events WHERE interface_id = ? AND kind = 'link_down'`, port.ID).Scan(&events).Error)
	if events != 1 {
		t.Errorf("link_down events: %d", events)
	}
	open, err := incidents.OpenPortIncidents(ctx, port.ID)
	if err != nil || len(open) != 1 || open[0].Condition == nil || *open[0].Condition != "link_down" {
		t.Errorf("port incidents: %+v %v", open, err)
	}
	var ms *int
	testdb.Must(t, db.Raw(`SELECT last_stats_duration_ms FROM devices WHERE id = ?`, s.DeviceID).Scan(&ms).Error)
	if ms == nil {
		t.Error("stats duration not recorded")
	}
}
```

Run: `cd backend && make test-db 2>&1 | grep -E "PortMonitorEndToEnd|^FAIL" | head`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/services/port_service.go backend/internal/services/port_monitor.go \
  backend/internal/services/port_monitor_test.go backend/internal/services/port_monitor_db_test.go \
  backend/internal/services/device_poller.go backend/internal/services/device_poller_test.go
git commit -m "feat(snmp): stats poll with rates, port events, and important-port incidents

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 9: Ports, events, device details, metrics query and settings API

**Files:**
- Modify: `backend/internal/models/snmp.go` (`PortPatch`, `DeviceDetailsPatch`), `backend/internal/models/snmp_test.go`
- Modify: `backend/internal/services/device_service.go` (`UpdateDetails`, effective fields), `backend/internal/services/port_service.go` (views)
- Create: `backend/internal/services/network_settings.go`, `backend/internal/services/port_views_db_test.go`
- Modify: `backend/internal/api/device_handler.go`, `backend/internal/api/device_handler_test.go`
- Create: `backend/internal/api/port_handler.go`, `backend/internal/api/network_handler.go`, `backend/internal/api/port_handler_test.go`

**Interfaces:**
- Consumes:
  - `portmon.Layout`, `PortNumber`, `IsPhysical`, `IfInfo` (Task 3)
  - `MetricsStore.Query`/`LatestMany`/`ApplyRetention`/`RetentionDays` (Task 7)
  - `IncidentService.OpenPortIncidents`/`ClosePortIncidents` (Task 6)
  - `SettingsService.PortThresholds`/`MetricsRawRetentionDays` (Task 7)
- Produces:
  - models: `PortPatch` (+ `Validate() error`, `Updates() map[string]any`) and `DeviceDetailsPatch` (+ `Updates() (map[string]any, error)`)
  - `DeviceView.EffectiveVendor/EffectiveModel/EffectiveLocation/EffectiveType` (`effective_*` JSON)
  - `(*DeviceService).UpdateDetails(ctx, id, models.DeviceDetailsPatch) (before, after *models.Device, error)`
  - `PortView`, `PortDefaults`, `DevicePortsView`, `PortDetailView`, `PortEventFilter`, `PortEventView`, `PortRef`, `SitePortSummary`, `ErrPortNotFound`
  - `(*PortService)`: `DevicePorts`, `Port`, `UpdatePort`, `Events`, `SiteSummary`, `PhysicalInterfaceIDs`
  - `NetworkSettings`, `NetworkSettingsPatch`, `NewNetworkSettingsService(settings, metrics)` with `Get`, `Update`, `EnsureRetention`
  - Routes:
    - `GET /devices/:id/ports`, `GET|PATCH /devices/:id/ports/:ifIndex`, `GET /devices/:id/events`, `PATCH /devices/:id/details`
    - `GET /sites/:id/port-events`, `GET /sites/:id/ports/summary`
    - `GET /network/metrics/query`, `GET|PATCH /network/settings`
  - `api.RegisterPortRoutes(rg, devices deviceStore, ports portStore, sites siteAccessChecker, audit auditRecorder)` and `api.RegisterNetworkRoutes(rg, devices deviceStore, ports portStore, metrics metricsQuerier, settings networkSettingsStore, sites siteAccessChecker, users adminChecker)`

- [ ] **Step 1: Patch types, test first**

Append to `backend/internal/models/snmp_test.go`:

```go
func TestPortPatch(t *testing.T) {
	var p PortPatch
	if err := json.Unmarshal([]byte(`{"important": true, "util_threshold_pct": null}`), &p); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	u := p.Updates()
	if u["important"] != true || u["util_threshold_pct"] != nil || len(u) != 3 { // + updated_at
		t.Errorf("updates %v", u)
	}
	if _, ok := u["collect"]; ok {
		t.Error("absent field included")
	}
	for _, body := range []string{`{"util_threshold_pct": 5}`, `{"error_threshold_per_min": 0}`,
		`{"down_grace_seconds": 90000}`, `{"important": null}`, `{}`} {
		var bad PortPatch
		_ = json.Unmarshal([]byte(body), &bad)
		if bad.Validate() == nil {
			t.Errorf("%s accepted", body)
		}
	}
}

func TestDeviceDetailsPatch(t *testing.T) {
	var p DeviceDetailsPatch
	if err := json.Unmarshal([]byte(`{"model_override": "  UNVR (4-bay) ", "vendor_override": "", "device_type": "nvr",
		"faceplate_sfp_ports": [52, 49, 49], "faceplate_rows": null}`), &p); err != nil {
		t.Fatal(err)
	}
	u, err := p.Updates()
	if err != nil {
		t.Fatal(err)
	}
	if u["model_override"] != "UNVR (4-bay)" || u["vendor_override"] != nil || u["device_type"] != "nvr" || u["faceplate_rows"] != nil {
		t.Errorf("updates %v", u)
	}
	if sfp, ok := u["faceplate_sfp_ports"].(IntSlice); !ok || len(sfp) != 2 || sfp[0] != 49 || sfp[1] != 52 {
		t.Errorf("sfp %v", u["faceplate_sfp_ports"])
	}
	for _, body := range []string{`{"device_type": "toaster"}`, `{"faceplate_rows": 3}`, `{"faceplate_sfp_ports": [0]}`, `{}`} {
		var bad DeviceDetailsPatch
		_ = json.Unmarshal([]byte(body), &bad)
		if _, err := bad.Updates(); err == nil {
			t.Errorf("%s accepted", body)
		}
	}
}
```

(Add `"encoding/json"` to that file's imports if it is not there.)

Run: `cd backend && go test ./internal/models/ -run 'PortPatch|DeviceDetailsPatch' 2>&1 | tail -3`
Expected: FAIL (`undefined: PortPatch`).

Append to `backend/internal/models/snmp.go` (adding `"sort"` to its imports):

```go
// PortPatch is PATCH /devices/:id/ports/:ifIndex. Absent fields are left
// alone; null clears an override back to the default (collect: back to the
// classifier's choice; thresholds: back to the instance defaults).
type PortPatch struct {
	Important            Opt[bool] `json:"important"`
	Collect              Opt[bool] `json:"collect"`
	UtilThresholdPct     Opt[int]  `json:"util_threshold_pct"`
	ErrorThresholdPerMin Opt[int]  `json:"error_threshold_per_min"`
	DownGraceSeconds     Opt[int]  `json:"down_grace_seconds"`
}

// Validate checks ranges (the same as the schema's CHECKs).
func (p PortPatch) Validate() error {
	if !p.Important.Set && !p.Collect.Set && !p.UtilThresholdPct.Set && !p.ErrorThresholdPerMin.Set && !p.DownGraceSeconds.Set {
		return errors.New("nothing to change")
	}
	if p.Important.Set && p.Important.Value == nil {
		return errors.New("important must be true or false")
	}
	inRange := func(o Opt[int], name string, min, max int) error {
		if o.Value != nil && (*o.Value < min || *o.Value > max) {
			return fmt.Errorf("%s must be between %d and %d", name, min, max)
		}
		return nil
	}
	if err := inRange(p.UtilThresholdPct, "the busy threshold", MinPortUtilThresholdPct, MaxPortUtilThresholdPct); err != nil {
		return err
	}
	if err := inRange(p.ErrorThresholdPerMin, "the error threshold", MinPortErrorThresholdPerMin, MaxPortErrorThresholdPerMin); err != nil {
		return err
	}
	return inRange(p.DownGraceSeconds, "the down grace period", MinPortDownGraceSeconds, MaxPortDownGraceSeconds)
}

// Updates is the column map for a validated patch.
func (p PortPatch) Updates() map[string]any {
	u := map[string]any{"updated_at": time.Now()}
	if p.Important.Set {
		u["important"] = *p.Important.Value
	}
	if p.Collect.Set {
		u["collect"] = optValue(p.Collect)
	}
	if p.UtilThresholdPct.Set {
		u["util_threshold_pct"] = optValue(p.UtilThresholdPct)
	}
	if p.ErrorThresholdPerMin.Set {
		u["error_threshold_per_min"] = optValue(p.ErrorThresholdPerMin)
	}
	if p.DownGraceSeconds.Set {
		u["down_grace_seconds"] = optValue(p.DownGraceSeconds)
	}
	return u
}

// optValue is the value to store for a set Opt: nil (SQL NULL) or the value.
func optValue[T any](o Opt[T]) any {
	if o.Value == nil {
		return nil
	}
	return *o.Value
}

// DeviceDetailsPatch is PATCH /devices/:id/details: the user's overrides. An
// empty string or null clears an override, so the SNMP value shows again.
type DeviceDetailsPatch struct {
	VendorOverride    Opt[string] `json:"vendor_override"`
	ModelOverride     Opt[string] `json:"model_override"`
	LocationOverride  Opt[string] `json:"location_override"`
	DeviceType        Opt[string] `json:"device_type"`
	FaceplateRows     Opt[int]    `json:"faceplate_rows"`
	FaceplateSFPPorts Opt[[]int]  `json:"faceplate_sfp_ports"`
}

// Updates validates the patch and returns its column map.
func (p DeviceDetailsPatch) Updates() (map[string]any, error) {
	u := map[string]any{}
	text := func(o Opt[string], col, label string) error {
		if !o.Set {
			return nil
		}
		if o.Value == nil || strings.TrimSpace(*o.Value) == "" {
			u[col] = nil
			return nil
		}
		v := strings.TrimSpace(*o.Value)
		if utf8.RuneCountInString(v) > 255 {
			return fmt.Errorf("%s must be 255 characters or fewer", label)
		}
		u[col] = v
		return nil
	}
	for _, f := range []struct {
		o          Opt[string]
		col, label string
	}{{p.VendorOverride, "vendor_override", "vendor"}, {p.ModelOverride, "model_override", "model"},
		{p.LocationOverride, "location_override", "location"}} {
		if err := text(f.o, f.col, f.label); err != nil {
			return nil, err
		}
	}
	if p.DeviceType.Set {
		switch {
		case p.DeviceType.Value == nil || *p.DeviceType.Value == "":
			u["device_type"] = nil
		case ValidDeviceTypes[*p.DeviceType.Value]:
			u["device_type"] = *p.DeviceType.Value
		default:
			return nil, fmt.Errorf("device type must be switch, router, access_point, nvr or other")
		}
	}
	if p.FaceplateRows.Set {
		if v := p.FaceplateRows.Value; v != nil && *v != 1 && *v != 2 {
			return nil, errors.New("faceplate rows must be 1 or 2")
		}
		u["faceplate_rows"] = optValue(p.FaceplateRows)
	}
	if p.FaceplateSFPPorts.Set {
		if p.FaceplateSFPPorts.Value == nil || len(*p.FaceplateSFPPorts.Value) == 0 {
			u["faceplate_sfp_ports"] = nil
		} else {
			seen := map[int]bool{}
			var ports IntSlice
			for _, n := range *p.FaceplateSFPPorts.Value {
				if n < 1 || n > 9999 {
					return nil, errors.New("SFP port numbers must be between 1 and 9999")
				}
				if !seen[n] {
					seen[n] = true
					ports = append(ports, n)
				}
			}
			if len(ports) > 64 {
				return nil, errors.New("at most 64 SFP ports")
			}
			sort.Ints(ports)
			u["faceplate_sfp_ports"] = ports
		}
	}
	if len(u) == 0 {
		return nil, errors.New("nothing to change")
	}
	return u, nil
}
```

Run: `cd backend && go test ./internal/models/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 2: Effective identity and UpdateDetails**

In `backend/internal/services/device_service.go`, add to `DeviceView`:

```go
	// Effective* are what the pages show: the user's override when set,
	// otherwise what SNMP reported (device type: what inventory detected).
	EffectiveVendor   string `json:"effective_vendor" gorm:"column:effective_vendor"`
	EffectiveModel    string `json:"effective_model" gorm:"column:effective_model"`
	EffectiveLocation string `json:"effective_location" gorm:"column:effective_location"`
	EffectiveType     string `json:"effective_type" gorm:"column:effective_type"`
```

Change the `Select` in `viewQuery` to:

```go
		Select("d.*, st.name AS site_name, c.name AS credential_name, " +
			"COALESCE(NULLIF(d.vendor_override, ''), d.vendor, '') AS effective_vendor, " +
			"COALESCE(NULLIF(d.model_override, ''), d.model, '') AS effective_model, " +
			"COALESCE(NULLIF(d.location_override, ''), d.sys_location, '') AS effective_location, " +
			"COALESCE(d.device_type, d.device_type_detected) AS effective_type, " + availabilitySQL).
```

Append:

```go
// UpdateDetails applies the user's overrides (vendor, model, location, device
// type, faceplate layout). Only fields present in the patch change.
func (s *DeviceService) UpdateDetails(ctx context.Context, id uuid.UUID, p models.DeviceDetailsPatch) (*models.Device, *models.Device, error) {
	before, err := s.getRaw(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	updates, err := p.Updates()
	if err != nil {
		return nil, nil, err
	}
	updates["updated_at"] = gorm.Expr("now()")
	if err := s.db.WithContext(ctx).Model(&models.Device{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, nil, fmt.Errorf("saving device details: %w", err)
	}
	after, err := s.getRaw(ctx, id)
	return before, after, err
}
```

- [ ] **Step 3: Port views, test first**

Create `backend/internal/services/port_views_db_test.go`:

```go
package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func portsFixture(t *testing.T) (*PortService, *DeviceService, *DeviceView, *MetricsStore) {
	t.Helper()
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	ctx := context.Background()
	incidents := NewIncidentService(db)
	devices := NewDeviceService(db, NewSNMPCredentialService(db), incidents)
	testdb.Must(t, devices.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))
	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, incidents, NewSettingsService(db))
	d, err := devices.Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	return ports, devices, d, metrics
}

func TestDBDevicePortsView(t *testing.T) {
	ports, _, d, metrics := portsFixture(t)
	ctx := context.Background()
	testdb.Must(t, metrics.Write(ctx, d.ID, time.Now().UTC(), []SamplePoint{
		{Metric: MetricIfInBps, Instance: "49", Value: 4e9},
		{Metric: MetricIfInUtilPct, Instance: "49", Value: 40},
		{Metric: MetricIfInErrorsPM, Instance: "49", Value: 3},
	}))
	v, err := ports.DevicePorts(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Ports) != 53 || v.Defaults.UtilThresholdPct != 80 {
		t.Fatalf("ports %d defaults %+v", len(v.Ports), v.Defaults)
	}
	var p49, cpu PortView
	for _, p := range v.Ports {
		switch p.IfIndex {
		case 49:
			p49 = p
		case 65:
			cpu = p
		}
	}
	if p49.Number != 49 || !p49.Physical || !p49.Collected || p49.InBps == nil || *p49.InBps != 4e9 ||
		p49.ErrorsPerMin == nil || *p49.ErrorsPerMin != 3 || p49.OutBps != nil {
		t.Errorf("port 49: %+v", p49)
	}
	if cpu.Physical || cpu.Collected {
		t.Errorf("cpu interface: %+v", cpu)
	}
	if v.Faceplate.Rows != 2 || len(v.Faceplate.Blocks) != 5 {
		t.Errorf("faceplate rows %d blocks %d", v.Faceplate.Rows, len(v.Faceplate.Blocks))
	}
}

func TestDBUpdatePortClosesIncidentsWhenUnmarked(t *testing.T) {
	ports, _, d, _ := portsFixture(t)
	ctx := context.Background()
	yes, no := true, false
	_, after, err := ports.UpdatePort(ctx, d.ID, 3, models.PortPatch{Important: models.Opt[bool]{Set: true, Value: &yes}})
	if err != nil || !after.Important {
		t.Fatalf("mark: %+v %v", after, err)
	}
	if _, _, err := ports.incidents.OpenPortIncident(ctx, d.ID, after.ID, "link_down", time.Now().UTC(), "down"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ports.UpdatePort(ctx, d.ID, 3, models.PortPatch{Important: models.Opt[bool]{Set: true, Value: &no}}); err != nil {
		t.Fatal(err)
	}
	open, _ := ports.incidents.OpenPortIncidents(ctx, after.ID)
	if len(open) != 0 {
		t.Fatalf("un-marking left %d incidents open", len(open))
	}
	if _, _, err := ports.UpdatePort(ctx, d.ID, 999, models.PortPatch{Important: models.Opt[bool]{Set: true, Value: &yes}}); !errors.Is(err, ErrPortNotFound) {
		t.Errorf("missing port: %v", err)
	}
}

func TestDBEventsAndSiteSummary(t *testing.T) {
	ports, _, d, metrics := portsFixture(t)
	ctx := context.Background()
	var p5 models.DeviceInterface
	testdb.Must(t, ports.db.First(&p5, "device_id = ? AND if_index = 5", d.ID).Error)
	testdb.Must(t, ports.RecordPortEvents(ctx, []models.PortEvent{
		{DeviceID: d.ID, InterfaceID: p5.ID, IfIndex: 5, Kind: "link_down", StartedAt: time.Now().Add(-time.Hour)},
		{DeviceID: d.ID, InterfaceID: p5.ID, IfIndex: 5, Kind: "errors", StartedAt: time.Now().Add(-time.Minute)},
	}, nil))
	testdb.Exec(t, ports.db, `UPDATE device_interfaces SET conditions = '["errors"]' WHERE id = ?`, p5.ID)
	testdb.Must(t, metrics.Write(ctx, d.ID, time.Now().UTC(), []SamplePoint{
		{Metric: MetricIfInUtilPct, Instance: "7", Value: 91}, {Metric: MetricIfOutUtilPct, Instance: "7", Value: 2},
		{Metric: MetricIfInUtilPct, Instance: "8", Value: 12},
	}))

	five := 5
	evs, total, err := ports.Events(ctx, PortEventFilter{DeviceID: &d.ID, IfIndex: &five})
	if err != nil || total != 2 || len(evs) != 2 || evs[0].Kind != "errors" || evs[0].PortNumber != 5 || evs[0].DeviceName == "" {
		t.Fatalf("events %+v total %d err %v", evs, total, err)
	}
	other := uuid.New()
	if evs, _, _ := ports.Events(ctx, PortEventFilter{SiteID: &other}); len(evs) != 0 {
		t.Errorf("another site's filter returned %d events", len(evs))
	}

	sum, err := ports.SiteSummary(ctx, d.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Busiest) == 0 || sum.Busiest[0].IfIndex != 7 || *sum.Busiest[0].UtilPct != 91 {
		t.Errorf("busiest %+v", sum.Busiest)
	}
	if len(sum.Problems) != 1 || sum.Problems[0].IfIndex != 5 {
		t.Errorf("problems %+v", sum.Problems)
	}
}

func TestDBUpdateDetailsAndEffectiveValues(t *testing.T) {
	_, devices, d, _ := portsFixture(t)
	ctx := context.Background()
	model := "UNVR (4-bay)"
	nvr := "nvr"
	_, _, err := devices.UpdateDetails(ctx, d.ID, models.DeviceDetailsPatch{
		ModelOverride: models.Opt[string]{Set: true, Value: &model},
		DeviceType:    models.Opt[string]{Set: true, Value: &nvr},
	})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := devices.Get(ctx, d.ID)
	if v.EffectiveModel != model || v.Model != "USW-Pro-48-PoE" || v.EffectiveType != "nvr" || v.EffectiveVendor != "Ubiquiti (EdgeSwitch)" {
		t.Errorf("effective %q/%q/%q, reported model %q", v.EffectiveVendor, v.EffectiveModel, v.EffectiveType, v.Model)
	}
	// Clearing brings the SNMP value back.
	if _, _, err := devices.UpdateDetails(ctx, d.ID, models.DeviceDetailsPatch{ModelOverride: models.Opt[string]{Set: true}}); err != nil {
		t.Fatal(err)
	}
	v, _ = devices.Get(ctx, d.ID)
	if v.EffectiveModel != "USW-Pro-48-PoE" {
		t.Errorf("after clearing: %q", v.EffectiveModel)
	}
}
```

Run: `cd backend && make test-db 2>&1 | grep -E "DevicePortsView|UpdatePort|EventsAndSite|UpdateDetails" | head`
Expected: FAIL (`ports.DevicePorts undefined`).

- [ ] **Step 4: Implement the views**

Append to `backend/internal/services/port_service.go` (adding imports `"errors"`, `"sort"`, and `"github.com/Stevy2191/Sentinel/backend/internal/portmon"`):

```go
var ErrPortNotFound = errors.New("port not found")

// PortView is one interface as the device and port pages show it: stored
// state plus its latest rates (nil when not collected or not fresh).
type PortView struct {
	models.DeviceInterface
	Number       int      `json:"number"`
	Collected    bool     `json:"collected"`
	Physical     bool     `json:"physical"`
	InBps        *float64 `json:"in_bps"`
	OutBps       *float64 `json:"out_bps"`
	InUtilPct    *float64 `json:"in_util_pct"`
	OutUtilPct   *float64 `json:"out_util_pct"`
	ErrorsPerMin *float64 `json:"errors_per_min"`
}

// PortDefaults are the instance thresholds a port uses when it has no
// overrides of its own.
type PortDefaults struct {
	ErrorThresholdPerMin int `json:"error_threshold_per_min"`
	UtilThresholdPct     int `json:"util_threshold_pct"`
	DownGraceSeconds     int `json:"down_grace_seconds"`
}

type DevicePortsView struct {
	Ports     []PortView        `json:"ports"`
	Faceplate portmon.Faceplate `json:"faceplate"`
	Defaults  PortDefaults      `json:"defaults"`
}

type PortDetailView struct {
	PortView
	Defaults      PortDefaults      `json:"defaults"`
	OpenIncidents []models.Incident `json:"open_incidents"`
}

var liveMetrics = []string{MetricIfInBps, MetricIfOutBps, MetricIfInUtilPct, MetricIfOutUtilPct,
	MetricIfInErrorsPM, MetricIfOutErrorsPM, MetricIfInDiscardsPM, MetricIfOutDiscardsPM}

// liveSince is how old a sample may be and still count as "now": three
// polls, and never less than five minutes.
func liveSince(now time.Time, pollInterval int) time.Time {
	w := 3 * time.Duration(pollInterval) * time.Second
	if w < 5*time.Minute {
		w = 5 * time.Minute
	}
	return now.Add(-w)
}

func (s *PortService) defaults(ctx context.Context) PortDefaults {
	th := s.settings.PortThresholds(ctx)
	return PortDefaults{ErrorThresholdPerMin: int(th.ErrorsPerMin), UtilThresholdPct: int(th.UtilPct),
		DownGraceSeconds: int(th.DownGrace.Seconds())}
}

func toPortView(row models.DeviceInterface, live map[string]map[string]float64) PortView {
	info := portmon.IfInfo{Name: row.Name, Descr: row.Descr, Type: row.IfType, ConnectorPresent: row.ConnectorPresent}
	v := PortView{DeviceInterface: row, Number: portmon.PortNumber(row.Name, row.Descr, row.IfIndex),
		Collected: row.CollectEffective(), Physical: portmon.IsPhysical(info)}
	inst := instanceKey(row.IfIndex)
	get := func(metric string) *float64 {
		if x, ok := live[metric][inst]; ok {
			return &x
		}
		return nil
	}
	v.InBps, v.OutBps = get(MetricIfInBps), get(MetricIfOutBps)
	v.InUtilPct, v.OutUtilPct = get(MetricIfInUtilPct), get(MetricIfOutUtilPct)
	sum, found := 0.0, false
	for _, k := range []string{MetricIfInErrorsPM, MetricIfOutErrorsPM, MetricIfInDiscardsPM, MetricIfOutDiscardsPM} {
		if p := get(k); p != nil {
			sum, found = sum+*p, true
		}
	}
	if found {
		v.ErrorsPerMin = &sum
	}
	return v
}

// DevicePorts lists a device's present interfaces with live figures, and the
// faceplate of its physical ports.
func (s *PortService) DevicePorts(ctx context.Context, d *DeviceView) (*DevicePortsView, error) {
	var rows []models.DeviceInterface
	if err := s.db.WithContext(ctx).Where("device_id = ? AND present", d.ID).Order("if_index").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing ports: %w", err)
	}
	latest, err := s.metrics.LatestMany(ctx, []uuid.UUID{d.ID}, liveMetrics, liveSince(time.Now(), d.PollInterval))
	if err != nil {
		return nil, err
	}
	out := &DevicePortsView{Ports: make([]PortView, 0, len(rows)), Defaults: s.defaults(ctx)}
	var layout []portmon.LayoutPort
	for _, r := range rows {
		v := toPortView(r, latest[d.ID])
		out.Ports = append(out.Ports, v)
		if v.Physical {
			layout = append(layout, portmon.LayoutPort{IfIndex: r.IfIndex, Name: r.Name, Descr: r.Descr})
		}
	}
	out.Faceplate = portmon.Layout(layout, d.FaceplateRows, d.FaceplateSFPPorts)
	return out, nil
}

// Port is one interface with live figures and its open incidents.
func (s *PortService) Port(ctx context.Context, d *DeviceView, ifIndex int) (*PortDetailView, error) {
	var rows []models.DeviceInterface
	if err := s.db.WithContext(ctx).Where("device_id = ? AND if_index = ?", d.ID, ifIndex).Limit(1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading port: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrPortNotFound
	}
	latest, err := s.metrics.LatestMany(ctx, []uuid.UUID{d.ID}, liveMetrics, liveSince(time.Now(), d.PollInterval))
	if err != nil {
		return nil, err
	}
	open, err := s.incidents.OpenPortIncidents(ctx, rows[0].ID)
	if err != nil {
		return nil, err
	}
	if open == nil {
		open = []models.Incident{}
	}
	return &PortDetailView{PortView: toPortView(rows[0], latest[d.ID]), Defaults: s.defaults(ctx), OpenIncidents: open}, nil
}

// UpdatePort applies a port patch. Un-marking a port as important closes its
// open port incidents.
func (s *PortService) UpdatePort(ctx context.Context, deviceID uuid.UUID, ifIndex int, p models.PortPatch) (*models.DeviceInterface, *models.DeviceInterface, error) {
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	var before models.DeviceInterface
	err := s.db.WithContext(ctx).Where("device_id = ? AND if_index = ?", deviceID, ifIndex).First(&before).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrPortNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("loading port: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&models.DeviceInterface{}).Where("id = ?", before.ID).Updates(p.Updates()).Error; err != nil {
		return nil, nil, fmt.Errorf("saving port: %w", err)
	}
	if before.Important && p.Important.Value != nil && !*p.Important.Value {
		if _, err := s.incidents.ClosePortIncidents(ctx, before.ID, time.Now().UTC(), "The port is no longer marked important."); err != nil {
			return nil, nil, err
		}
	}
	var after models.DeviceInterface
	if err := s.db.WithContext(ctx).First(&after, "id = ?", before.ID).Error; err != nil {
		return nil, nil, fmt.Errorf("reloading port: %w", err)
	}
	return &before, &after, nil
}

// PortEventFilter narrows the event log. Exactly one of DeviceID and SiteID
// is expected (the handlers set one).
type PortEventFilter struct {
	DeviceID *uuid.UUID
	SiteID   *uuid.UUID
	IfIndex  *int
	Page     int
	Limit    int
}

type PortEventView struct {
	models.PortEvent
	DeviceName string `json:"device_name" gorm:"column:device_name"`
	PortName   string `json:"port_name" gorm:"column:port_name"`
	PortAlias  string `json:"port_alias" gorm:"column:port_alias"`
	PortDescr  string `json:"-" gorm:"column:port_descr"`
	PortNumber int    `json:"port_number" gorm:"-"`
}

// Events returns a page of the port event log, newest first, and the total.
func (s *PortService) Events(ctx context.Context, f PortEventFilter) ([]PortEventView, int64, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 50
	}
	q := s.db.WithContext(ctx).Table("port_events AS e").
		Joins("JOIN devices d ON d.id = e.device_id").
		Joins("JOIN device_interfaces i ON i.id = e.interface_id")
	if f.DeviceID != nil {
		q = q.Where("e.device_id = ?", *f.DeviceID)
	}
	if f.SiteID != nil {
		q = q.Where("d.site_id = ?", *f.SiteID)
	}
	if f.IfIndex != nil {
		q = q.Where("e.if_index = ?", *f.IfIndex)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("counting port events: %w", err)
	}
	var rows []PortEventView
	err := q.Session(&gorm.Session{}).
		Select("e.*, d.name AS device_name, COALESCE(i.name, '') AS port_name, COALESCE(i.alias, '') AS port_alias, COALESCE(i.descr, '') AS port_descr").
		Order("e.started_at DESC, e.id DESC").Limit(f.Limit).Offset((f.Page - 1) * f.Limit).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("listing port events: %w", err)
	}
	for i := range rows {
		rows[i].PortNumber = portmon.PortNumber(rows[i].PortName, rows[i].PortDescr, rows[i].IfIndex)
	}
	if rows == nil {
		rows = []PortEventView{}
	}
	return rows, total, nil
}

// PortRef is a port in a site summary.
type PortRef struct {
	DeviceID   uuid.UUID `json:"device_id"`
	DeviceName string    `json:"device_name"`
	IfIndex    int       `json:"if_index"`
	Number     int       `json:"number"`
	Name       string    `json:"name"`
	Alias      string    `json:"alias"`
	OperStatus string    `json:"oper_status"`
	Important  bool      `json:"important"`
	Conditions []string  `json:"conditions"`
	InBps      *float64  `json:"in_bps"`
	OutBps     *float64  `json:"out_bps"`
	UtilPct    *float64  `json:"util_pct"`
}

type SitePortSummary struct {
	Busiest  []PortRef `json:"busiest"`
	Problems []PortRef `json:"problems"`
}

// SiteSummary lists a site's five busiest physical ports and every port with
// a problem: a warning condition, or an important port whose link is down.
func (s *PortService) SiteSummary(ctx context.Context, siteID uuid.UUID) (*SitePortSummary, error) {
	var rows []struct {
		models.DeviceInterface
		DeviceName   string `gorm:"column:device_name"`
		PollInterval int    `gorm:"column:poll_interval"`
	}
	err := s.db.WithContext(ctx).Raw(`SELECT i.*, d.name AS device_name, d.poll_interval
		FROM device_interfaces i JOIN devices d ON d.id = i.device_id
		WHERE d.site_id = ? AND i.present AND COALESCE(i.collect, i.collect_default)`, siteID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing site ports: %w", err)
	}
	ids, seen, maxPoll := []uuid.UUID{}, map[uuid.UUID]bool{}, 60
	for _, r := range rows {
		if !seen[r.DeviceID] {
			seen[r.DeviceID] = true
			ids = append(ids, r.DeviceID)
		}
		maxPoll = max(maxPoll, r.PollInterval)
	}
	latest, err := s.metrics.LatestMany(ctx, ids, []string{MetricIfInBps, MetricIfOutBps, MetricIfInUtilPct, MetricIfOutUtilPct},
		liveSince(time.Now(), maxPoll))
	if err != nil {
		return nil, err
	}
	out := &SitePortSummary{Busiest: []PortRef{}, Problems: []PortRef{}}
	var busy []PortRef
	for _, r := range rows {
		v := toPortView(r.DeviceInterface, latest[r.DeviceID])
		conds := []string{}
		for _, c := range r.Conditions {
			if c != models.PortConditionLinkDown {
				conds = append(conds, c)
			}
		}
		ref := PortRef{DeviceID: r.DeviceID, DeviceName: r.DeviceName, IfIndex: r.IfIndex, Number: v.Number,
			Name: r.Name, Alias: r.Alias, OperStatus: r.OperStatus, Important: r.Important, Conditions: conds,
			InBps: v.InBps, OutBps: v.OutBps}
		if v.InUtilPct != nil || v.OutUtilPct != nil {
			u := 0.0
			if v.InUtilPct != nil {
				u = *v.InUtilPct
			}
			if v.OutUtilPct != nil {
				u = max(u, *v.OutUtilPct)
			}
			ref.UtilPct = &u
		}
		importantDown := r.Important && r.OperStatus != "up" && r.AdminStatus != "down"
		if len(conds) > 0 || importantDown {
			out.Problems = append(out.Problems, ref)
		}
		if v.Physical && ref.UtilPct != nil {
			busy = append(busy, ref)
		}
	}
	sort.Slice(busy, func(i, j int) bool { return *busy[i].UtilPct > *busy[j].UtilPct })
	if len(busy) > 5 {
		busy = busy[:5]
	}
	out.Busiest = append(out.Busiest, busy...)
	sort.Slice(out.Problems, func(i, j int) bool {
		a, b := out.Problems[i], out.Problems[j]
		if a.DeviceName != b.DeviceName {
			return a.DeviceName < b.DeviceName
		}
		return a.Number < b.Number
	})
	return out, nil
}

// PhysicalInterfaceIDs returns the ids of the physical ports of the given
// devices, for totals that must not count a link aggregate and its members
// twice.
func (s *PortService) PhysicalInterfaceIDs(ctx context.Context, deviceIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(deviceIDs) == 0 {
		return nil, nil
	}
	var rows []models.DeviceInterface
	if err := s.db.WithContext(ctx).Where("device_id IN ? AND present", deviceIDs).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing physical ports: %w", err)
	}
	var ids []uuid.UUID
	for _, r := range rows {
		if portmon.IsPhysical(portmon.IfInfo{Name: r.Name, Descr: r.Descr, Type: r.IfType, ConnectorPresent: r.ConnectorPresent}) {
			ids = append(ids, r.ID)
		}
	}
	return ids, nil
}
```

Create `backend/internal/services/network_settings.go`:

```go
package services

import (
	"context"
	"fmt"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// NetworkSettings are the network section's instance-wide settings.
type NetworkSettings struct {
	MetricsRawRetentionDays  int `json:"metrics_raw_retention_days"`
	PortErrorThresholdPerMin int `json:"port_error_threshold_per_min"`
	PortUtilThresholdPct     int `json:"port_util_threshold_pct"`
	PortDownGraceSeconds     int `json:"port_down_grace_seconds"`
}

// NetworkSettingsPatch changes only the fields present.
type NetworkSettingsPatch struct {
	MetricsRawRetentionDays  *int `json:"metrics_raw_retention_days"`
	PortErrorThresholdPerMin *int `json:"port_error_threshold_per_min"`
	PortUtilThresholdPct     *int `json:"port_util_threshold_pct"`
	PortDownGraceSeconds     *int `json:"port_down_grace_seconds"`
}

type NetworkSettingsService struct {
	settings *SettingsService
	metrics  *MetricsStore
}

func NewNetworkSettingsService(settings *SettingsService, metrics *MetricsStore) *NetworkSettingsService {
	return &NetworkSettingsService{settings: settings, metrics: metrics}
}

func (s *NetworkSettingsService) Get(ctx context.Context) NetworkSettings {
	th := s.settings.PortThresholds(ctx)
	return NetworkSettings{
		MetricsRawRetentionDays:  s.settings.MetricsRawRetentionDays(ctx),
		PortErrorThresholdPerMin: int(th.ErrorsPerMin),
		PortUtilThresholdPct:     int(th.UtilPct),
		PortDownGraceSeconds:     int(th.DownGrace.Seconds()),
	}
}

// Update validates and saves the patch. A new retention replaces the
// TimescaleDB policy at once.
func (s *NetworkSettingsService) Update(ctx context.Context, p NetworkSettingsPatch) (NetworkSettings, error) {
	fields := []struct {
		v          *int
		key, label string
		min, max   int
	}{
		{p.MetricsRawRetentionDays, models.SettingMetricsRawRetentionDays, "Detailed history",
			models.MinMetricsRawRetentionDays, models.MaxMetricsRawRetentionDays},
		{p.PortErrorThresholdPerMin, models.SettingPortErrorThresholdPerMin, "The error threshold",
			models.MinPortErrorThresholdPerMin, models.MaxPortErrorThresholdPerMin},
		{p.PortUtilThresholdPct, models.SettingPortUtilThresholdPct, "The busy threshold",
			models.MinPortUtilThresholdPct, models.MaxPortUtilThresholdPct},
		{p.PortDownGraceSeconds, models.SettingPortDownGraceSeconds, "The down grace period",
			models.MinPortDownGraceSeconds, models.MaxPortDownGraceSeconds},
	}
	for _, f := range fields {
		if f.v != nil && (*f.v < f.min || *f.v > f.max) {
			return NetworkSettings{}, fmt.Errorf("%s must be between %d and %d", f.label, f.min, f.max)
		}
	}
	before := s.settings.MetricsRawRetentionDays(ctx)
	for _, f := range fields {
		if f.v != nil {
			if err := s.settings.SetInt(ctx, f.key, *f.v); err != nil {
				return NetworkSettings{}, fmt.Errorf("saving %s: %w", f.key, err)
			}
		}
	}
	if p.MetricsRawRetentionDays != nil && *p.MetricsRawRetentionDays != before {
		if err := s.metrics.ApplyRetention(ctx, *p.MetricsRawRetentionDays); err != nil {
			return NetworkSettings{}, err
		}
	}
	return s.Get(ctx), nil
}

// EnsureRetention makes the TimescaleDB policy match the setting (run at
// startup: a restored config may carry a different retention).
func (s *NetworkSettingsService) EnsureRetention(ctx context.Context) error {
	want := s.settings.MetricsRawRetentionDays(ctx)
	have, err := s.metrics.RetentionDays(ctx)
	if err != nil {
		return err
	}
	if have == want {
		return nil
	}
	return s.metrics.ApplyRetention(ctx, want)
}
```

Run: `cd backend && make test-db 2>&1 | grep -E "^(---|FAIL)" | head`
Expected: no failures.

- [ ] **Step 5: Handlers, test first**

Add to `backend/internal/api/device_handler_test.go`'s `fakeDevices`:

```go
func (f *fakeDevices) UpdateDetails(_ context.Context, id uuid.UUID, p models.DeviceDetailsPatch) (*models.Device, *models.Device, error) {
	if _, err := p.Updates(); err != nil {
		return nil, nil, err
	}
	f.mutations++
	return &models.Device{ID: id}, &models.Device{ID: id}, nil
}
```

Create `backend/internal/api/port_handler_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type fakePorts struct{ updates int }

func (f *fakePorts) DevicePorts(context.Context, *services.DeviceView) (*services.DevicePortsView, error) {
	return &services.DevicePortsView{Ports: []services.PortView{}}, nil
}
func (f *fakePorts) Port(_ context.Context, _ *services.DeviceView, ifIndex int) (*services.PortDetailView, error) {
	if ifIndex != 1 {
		return nil, services.ErrPortNotFound
	}
	return &services.PortDetailView{}, nil
}
func (f *fakePorts) UpdatePort(_ context.Context, _ uuid.UUID, _ int, p models.PortPatch) (*models.DeviceInterface, *models.DeviceInterface, error) {
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	f.updates++
	return &models.DeviceInterface{}, &models.DeviceInterface{}, nil
}
func (f *fakePorts) Events(context.Context, services.PortEventFilter) ([]services.PortEventView, int64, error) {
	return []services.PortEventView{}, 0, nil
}
func (f *fakePorts) SiteSummary(context.Context, uuid.UUID) (*services.SitePortSummary, error) {
	return &services.SitePortSummary{}, nil
}
func (f *fakePorts) PhysicalInterfaceIDs(context.Context, []uuid.UUID) ([]uuid.UUID, error) {
	return []uuid.UUID{uuid.New()}, nil
}

type fakeMetricsQuery struct{ last *services.MetricsQuery }

func (f *fakeMetricsQuery) Query(_ context.Context, q services.MetricsQuery) (*services.MetricsResult, error) {
	f.last = &q
	return &services.MetricsResult{Series: []services.MetricSeries{}}, nil
}

type fakeNetSettings struct{ patched int }

func (f *fakeNetSettings) Get(context.Context) services.NetworkSettings { return services.NetworkSettings{} }
func (f *fakeNetSettings) Update(context.Context, services.NetworkSettingsPatch) (services.NetworkSettings, error) {
	f.patched++
	return services.NetworkSettings{}, nil
}

type portRig struct {
	r       *gin.Engine
	devs    *fakeDevices
	ports   *fakePorts
	metrics *fakeMetricsQuery
	netSet  *fakeNetSettings
}

func newPortRig(dev services.DeviceView, levels fakeSiteLevels, isAdmin bool) *portRig {
	gin.SetMode(gin.TestMode)
	rig := &portRig{r: gin.New(), devs: &fakeDevices{device: dev}, ports: &fakePorts{}, metrics: &fakeMetricsQuery{}, netSet: &fakeNetSettings{}}
	rig.r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	g := rig.r.Group("/api/v1")
	RegisterDeviceRoutes(g, rig.devs, &fakeProber{}, levels, &fakeAudit{})
	RegisterPortRoutes(g, rig.devs, rig.ports, levels, &fakeAudit{})
	RegisterNetworkRoutes(g, rig.devs, rig.ports, rig.metrics, rig.netSet, levels, stubUsers{user: &models.User{IsAdmin: isAdmin}})
	return rig
}

func TestPortRoutesAccess(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	base := "/api/v1/devices/" + dev.ID.String()

	rig := newPortRig(dev, fakeSiteLevels{}, false)
	for _, p := range []string{base + "/ports", base + "/ports/1", base + "/events", "/api/v1/sites/" + site.String() + "/port-events",
		"/api/v1/sites/" + site.String() + "/ports/summary"} {
		if w := do(rig.r, http.MethodGet, p, nil); w.Code != http.StatusNotFound {
			t.Errorf("no access GET %s: %d, want 404", p, w.Code)
		}
	}

	rig = newPortRig(dev, fakeSiteLevels{site: services.SiteAccessReadonly}, false)
	if w := do(rig.r, http.MethodGet, base+"/ports", nil); w.Code != http.StatusOK {
		t.Errorf("readonly ports: %d", w.Code)
	}
	if w := do(rig.r, http.MethodGet, base+"/ports/7", nil); w.Code != http.StatusNotFound {
		t.Errorf("missing port: %d", w.Code)
	}
	if w := do(rig.r, http.MethodPatch, base+"/ports/1", map[string]any{"important": true}); w.Code != http.StatusForbidden || rig.ports.updates != 0 {
		t.Errorf("readonly patch: %d", w.Code)
	}
	if w := do(rig.r, http.MethodPatch, base+"/details", map[string]any{"model_override": "x"}); w.Code != http.StatusForbidden {
		t.Errorf("readonly details: %d", w.Code)
	}

	rig = newPortRig(dev, fakeSiteLevels{site: services.SiteAccessEditable}, false)
	if w := do(rig.r, http.MethodPatch, base+"/ports/1", map[string]any{"util_threshold_pct": 5}); w.Code != http.StatusBadRequest {
		t.Errorf("bad threshold: %d", w.Code)
	}
	if w := do(rig.r, http.MethodPatch, base+"/ports/1", map[string]any{"important": true}); w.Code != http.StatusOK || rig.ports.updates != 1 {
		t.Errorf("editable patch: %d", w.Code)
	}
	if w := do(rig.r, http.MethodPatch, base+"/details", map[string]any{"device_type": "toaster"}); w.Code != http.StatusBadRequest {
		t.Errorf("bad device type: %d", w.Code)
	}
	if w := do(rig.r, http.MethodPatch, base+"/details", map[string]any{"model_override": "UNVR"}); w.Code != http.StatusOK {
		t.Errorf("details: %d", w.Code)
	}
}

// Review Focus 4: a foreign device in a metrics query is a 404, and nothing
// is queried.
func TestMetricsQueryAccess(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	q := "/api/v1/network/metrics/query?metric=if_in_bps&range=24h&device_id=" + dev.ID.String()

	rig := newPortRig(dev, fakeSiteLevels{}, false)
	if w := do(rig.r, http.MethodGet, q, nil); w.Code != http.StatusNotFound || rig.metrics.last != nil {
		t.Errorf("foreign device: %d, queried %v", w.Code, rig.metrics.last != nil)
	}

	rig = newPortRig(dev, fakeSiteLevels{site: services.SiteAccessReadonly}, false)
	if w := do(rig.r, http.MethodGet, q, nil); w.Code != http.StatusOK || rig.metrics.last == nil || rig.metrics.last.DeviceIDs[0] != dev.ID {
		t.Fatalf("own device: %d %+v", w.Code, rig.metrics.last)
	}
	for _, bad := range []string{
		"/api/v1/network/metrics/query?metric=if_in_octets&range=24h&device_id=" + dev.ID.String(),
		"/api/v1/network/metrics/query?metric=if_in_bps&range=2h&device_id=" + dev.ID.String(),
		"/api/v1/network/metrics/query?metric=if_in_bps&range=24h",
		"/api/v1/network/metrics/query?metric=if_in_bps&range=24h&site_id=" + site.String(),
	} {
		if w := do(rig.r, http.MethodGet, bad, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, w.Code)
		}
	}
	w := do(rig.r, http.MethodGet, "/api/v1/network/metrics/query?metric=if_in_bps&range=24h&agg=sum&ports=physical&site_id="+site.String(), nil)
	if w.Code != http.StatusOK || !rig.metrics.last.Sum || len(rig.metrics.last.InterfaceIDs) != 1 {
		t.Errorf("site total: %d %+v", w.Code, rig.metrics.last)
	}
}

func TestNetworkSettingsAdminOnly(t *testing.T) {
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: uuid.New()}}
	rig := newPortRig(dev, fakeSiteLevels{}, false)
	if w := do(rig.r, http.MethodPatch, "/api/v1/network/settings", map[string]any{"port_util_threshold_pct": 70}); w.Code != http.StatusForbidden || rig.netSet.patched != 0 {
		t.Errorf("member patch: %d", w.Code)
	}
	rig = newPortRig(dev, fakeSiteLevels{}, true)
	if w := do(rig.r, http.MethodPatch, "/api/v1/network/settings", map[string]any{"port_util_threshold_pct": 70}); w.Code != http.StatusOK {
		t.Errorf("admin patch: %d", w.Code)
	}
}
```

Run: `cd backend && go test ./internal/api/ -run 'PortRoutes|MetricsQuery|NetworkSettings' 2>&1 | tail -3`
Expected: FAIL (`undefined: RegisterPortRoutes`).

- [ ] **Step 6: Implement the handlers**

In `backend/internal/api/device_handler.go`:

1. Add to `deviceStore`:

```go
	UpdateDetails(ctx context.Context, id uuid.UUID, p models.DeviceDetailsPatch) (*models.Device, *models.Device, error)
```

2. Register `g.PATCH("/:id/details", updateDeviceDetailsHandler(devices, sites, audit))` in `RegisterDeviceRoutes`, and append:

```go
func deviceDetailsAudit(d *models.Device) map[string]any {
	return map[string]any{"vendor_override": d.VendorOverride, "model_override": d.ModelOverride,
		"location_override": d.LocationOverride, "device_type": d.DeviceType,
		"faceplate_rows": d.FaceplateRows, "faceplate_sfp_ports": d.FaceplateSFPPorts}
}

// updateDeviceDetailsHandler handles PATCH /devices/:id/details: the user's
// overrides of what SNMP reported, which inventory never overwrites.
func updateDeviceDetailsHandler(devices deviceStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		var p models.DeviceDetailsPatch
		if err := c.ShouldBindJSON(&p); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		before, after, err := devices.UpdateDetails(c.Request.Context(), d.ID, p)
		if err != nil {
			respondDeviceError(c, "updateDeviceDetails", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceUpdated, models.ResourceDevice, &d.ID,
			models.AuditChanges{Before: deviceDetailsAudit(before), After: deviceDetailsAudit(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}
```

Create `backend/internal/api/port_handler.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type portStore interface {
	DevicePorts(ctx context.Context, d *services.DeviceView) (*services.DevicePortsView, error)
	Port(ctx context.Context, d *services.DeviceView, ifIndex int) (*services.PortDetailView, error)
	UpdatePort(ctx context.Context, deviceID uuid.UUID, ifIndex int, p models.PortPatch) (*models.DeviceInterface, *models.DeviceInterface, error)
	Events(ctx context.Context, f services.PortEventFilter) ([]services.PortEventView, int64, error)
	SiteSummary(ctx context.Context, siteID uuid.UUID) (*services.SitePortSummary, error)
	PhysicalInterfaceIDs(ctx context.Context, deviceIDs []uuid.UUID) ([]uuid.UUID, error)
}

// RegisterPortRoutes mounts the port, event and site summary routes. Access
// is the device's (or site's) SiteAccess: readonly to read, editable to
// change, and a device or site the caller cannot see is a 404.
func RegisterPortRoutes(rg *gin.RouterGroup, devices deviceStore, ports portStore, sites siteAccessChecker, audit auditRecorder) {
	rg.GET("/devices/:id/ports", devicePortsHandler(devices, ports, sites))
	rg.GET("/devices/:id/ports/:ifIndex", portHandler(devices, ports, sites))
	rg.PATCH("/devices/:id/ports/:ifIndex", updatePortHandler(devices, ports, sites, audit))
	rg.GET("/devices/:id/events", deviceEventsHandler(devices, ports, sites))
	rg.GET("/sites/:id/port-events", siteEventsHandler(ports, sites))
	rg.GET("/sites/:id/ports/summary", siteSummaryHandler(ports, sites))
}

func respondPortError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrPortNotFound):
		respondError(c, http.StatusNotFound, "port not found")
	case isInternal(err):
		respondInternal(c, op, err)
	default: // validation message from PortPatch
		respondError(c, http.StatusBadRequest, err.Error())
	}
}

func parseIfIndex(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("ifIndex"))
	if err != nil || n < 1 {
		respondError(c, http.StatusBadRequest, "invalid port")
		return 0, false
	}
	return n, true
}

func pageParams(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	return page, limit
}

func devicePortsHandler(devices deviceStore, ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		v, err := ports.DevicePorts(c.Request.Context(), d)
		if err != nil {
			respondInternal(c, "devicePorts", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	}
}

func portHandler(devices deviceStore, ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		idx, ok := parseIfIndex(c)
		if !ok {
			return
		}
		v, err := ports.Port(c.Request.Context(), d, idx)
		if err != nil {
			respondPortError(c, "port", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	}
}

func portAudit(i *models.DeviceInterface) map[string]any {
	return map[string]any{"if_index": i.IfIndex, "important": i.Important, "collect": i.Collect,
		"util_threshold_pct": i.UtilThresholdPct, "error_threshold_per_min": i.ErrorThresholdPerMin,
		"down_grace_seconds": i.DownGraceSeconds}
}

func updatePortHandler(devices deviceStore, ports portStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		idx, ok := parseIfIndex(c)
		if !ok {
			return
		}
		var p models.PortPatch
		if err := c.ShouldBindJSON(&p); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		before, after, err := ports.UpdatePort(c.Request.Context(), d.ID, idx, p)
		if err != nil {
			respondPortError(c, "updatePort", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceUpdated, models.ResourceDevice, &d.ID,
			models.AuditChanges{Before: portAudit(before), After: portAudit(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deviceEventsHandler(devices deviceStore, ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		f := services.PortEventFilter{DeviceID: &d.ID}
		if raw := c.Query("if_index"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				respondError(c, http.StatusBadRequest, "if_index must be a port number")
				return
			}
			f.IfIndex = &n
		}
		f.Page, f.Limit = pageParams(c)
		respondEvents(c, ports, f)
	}
}

func siteEventsHandler(ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, id, services.SiteAccessReadonly) {
			return
		}
		f := services.PortEventFilter{SiteID: &id}
		f.Page, f.Limit = pageParams(c)
		respondEvents(c, ports, f)
	}
}

func respondEvents(c *gin.Context, ports portStore, f services.PortEventFilter) {
	rows, total, err := ports.Events(c.Request.Context(), f)
	if err != nil {
		respondInternal(c, "portEvents", err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"events": rows, "total": total})
}

func siteSummaryHandler(ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, id, services.SiteAccessReadonly) {
			return
		}
		s, err := ports.SiteSummary(c.Request.Context(), id)
		if err != nil {
			respondInternal(c, "siteSummary", err)
			return
		}
		respondSuccess(c, http.StatusOK, s)
	}
}
```

Create `backend/internal/api/network_handler.go`:

```go
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type metricsQuerier interface {
	Query(ctx context.Context, q services.MetricsQuery) (*services.MetricsResult, error)
}

type networkSettingsStore interface {
	Get(ctx context.Context) services.NetworkSettings
	Update(ctx context.Context, p services.NetworkSettingsPatch) (services.NetworkSettings, error)
}

// RegisterNetworkRoutes mounts /network: the generic metrics query (any
// signed-in user, filtered by site access) and the network settings (admin).
func RegisterNetworkRoutes(rg *gin.RouterGroup, devices deviceStore, ports portStore, metrics metricsQuerier,
	settings networkSettingsStore, sites siteAccessChecker, users adminChecker) {
	g := rg.Group("/network")
	g.GET("/metrics/query", metricsQueryHandler(devices, ports, metrics, sites))
	admin := g.Group("", RequireAdmin(users))
	admin.GET("/settings", func(c *gin.Context) { respondSuccess(c, http.StatusOK, settings.Get(c.Request.Context())) })
	admin.PATCH("/settings", updateNetworkSettingsHandler(settings))
}

var queryRanges = map[string]time.Duration{
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour, "1y": 365 * 24 * time.Hour,
}

const (
	maxQueryMetrics   = 10
	maxQueryDevices   = 50
	maxQueryInstances = 200
	maxQuerySpan      = 400 * 24 * time.Hour
)

// canSeeSite reports whether the caller has at least readonly access,
// without writing a response.
func canSeeSite(c *gin.Context, sites siteAccessChecker, siteID uuid.UUID) (bool, error) {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		return false, nil
	}
	level, err := sites.SiteAccess(c.Request.Context(), userID, isAdmin, siteID)
	if errors.Is(err, services.ErrSiteNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return level != services.SiteAccessNone, nil
}

// metricsQueryHandler handles GET /network/metrics/query:
//
//	metric=... (1-10, repeatable), device_id=... (1-50, repeatable) or
//	site_id=... (with agg=sum), instance=... (optional, repeatable),
//	range=1h|6h|24h|7d|30d|90d|1y or from=&to= (RFC 3339), agg=sum,
//	ports=physical (sum only physical ports, not link aggregates).
func metricsQueryHandler(devices deviceStore, ports portStore, metrics metricsQuerier, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		names := c.QueryArray("metric")
		if len(names) == 0 || len(names) > maxQueryMetrics {
			respondError(c, http.StatusBadRequest, "choose 1 to 10 metrics")
			return
		}
		for _, n := range names {
			if !services.KnownMetric(n) {
				respondError(c, http.StatusBadRequest, fmt.Sprintf("unknown metric %q", n))
				return
			}
		}
		instances := c.QueryArray("instance")
		if len(instances) > maxQueryInstances {
			respondError(c, http.StatusBadRequest, "at most 200 instances")
			return
		}
		to := time.Now().UTC()
		var from time.Time
		if r := c.Query("range"); r != "" {
			span, ok := queryRanges[r]
			if !ok {
				respondError(c, http.StatusBadRequest, "range must be 1h, 6h, 24h, 7d, 30d, 90d or 1y")
				return
			}
			from = to.Add(-span)
		} else {
			var err1, err2 error
			from, err1 = time.Parse(time.RFC3339, c.Query("from"))
			to, err2 = time.Parse(time.RFC3339, c.Query("to"))
			if err1 != nil || err2 != nil || !from.Before(to) || to.Sub(from) > maxQuerySpan {
				respondError(c, http.StatusBadRequest, "give range, or from and to (RFC 3339, from before to, at most 400 days)")
				return
			}
		}
		agg := c.Query("agg")
		if agg != "" && agg != "sum" {
			respondError(c, http.StatusBadRequest, "agg must be sum")
			return
		}

		var ids []uuid.UUID
		deviceParams := c.QueryArray("device_id")
		if raw := c.Query("site_id"); raw != "" {
			if len(deviceParams) > 0 {
				respondError(c, http.StatusBadRequest, "use site_id or device_id, not both")
				return
			}
			if agg != "sum" {
				respondError(c, http.StatusBadRequest, "a site query needs agg=sum")
				return
			}
			siteID, err := uuid.Parse(raw)
			if err != nil {
				respondError(c, http.StatusBadRequest, "site_id must be a UUID")
				return
			}
			if !requireSiteLevel(c, sites, siteID, services.SiteAccessReadonly) {
				return
			}
			userID, _, isAdmin, _ := GetUserFromContext(c)
			list, err := devices.List(ctx, userID, isAdmin, services.DeviceFilter{SiteID: &siteID})
			if err != nil {
				respondInternal(c, "metricsQuery", err)
				return
			}
			for _, d := range list {
				ids = append(ids, d.ID)
			}
		} else {
			if len(deviceParams) == 0 || len(deviceParams) > maxQueryDevices {
				respondError(c, http.StatusBadRequest, "give 1 to 50 device_id values, or a site_id")
				return
			}
			for _, raw := range deviceParams {
				id, err := uuid.Parse(raw)
				if err != nil {
					respondError(c, http.StatusBadRequest, "device_id must be a UUID")
					return
				}
				d, err := devices.Get(ctx, id)
				if errors.Is(err, services.ErrDeviceNotFound) {
					respondError(c, http.StatusNotFound, "device not found")
					return
				}
				if err != nil {
					respondInternal(c, "metricsQuery", err)
					return
				}
				ok, err := canSeeSite(c, sites, d.SiteID)
				if err != nil {
					respondInternal(c, "metricsQuery", err)
					return
				}
				if !ok {
					respondError(c, http.StatusNotFound, "device not found")
					return
				}
				ids = append(ids, id)
			}
		}

		q := services.MetricsQuery{DeviceIDs: ids, Metrics: names, Instances: instances, From: from, To: to, Sum: agg == "sum"}
		if c.Query("ports") == "physical" && len(ids) > 0 {
			ifs, err := ports.PhysicalInterfaceIDs(ctx, ids)
			if err != nil {
				respondInternal(c, "metricsQuery", err)
				return
			}
			if len(ifs) == 0 {
				ifs = []uuid.UUID{uuid.Nil} // matches no series
			}
			q.InterfaceIDs = ifs
		}
		res, err := metrics.Query(ctx, q)
		if err != nil {
			respondInternal(c, "metricsQuery", err)
			return
		}
		respondSuccess(c, http.StatusOK, res)
	}
}

func updateNetworkSettingsHandler(settings networkSettingsStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var p services.NetworkSettingsPatch
		if err := c.ShouldBindJSON(&p); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		s, err := settings.Update(c.Request.Context(), p)
		if err != nil {
			if isInternal(err) {
				respondInternal(c, "updateNetworkSettings", err)
				return
			}
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, s)
	}
}
```

- [ ] **Step 7: Run everything**

Run: `cd backend && go vet ./... && go test ./... 2>&1 | grep -v "^ok" | head; make test-db 2>&1 | grep -E "^(---|FAIL)" | head`
Expected: no failures.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/models backend/internal/services backend/internal/api
git commit -m "feat(network): ports, events, device details, metrics query and settings API

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 10: Wiring, nightly maintenance, simulator counters, restore test

**Files:**
- Create: `backend/internal/services/network_maintenance.go`, `backend/internal/services/network_maintenance_db_test.go`
- Create: `backend/internal/services/backup_metrics_db_test.go`
- Modify: `backend/cmd/sentinel/main.go`
- Modify: `deploy/snmpsim/gen_data.py`, regenerate `deploy/snmpsim/data/*.snmprec`; modify `backend/internal/snmp/simulator_test.go`

**Interfaces:**
- Consumes: everything from Tasks 5–9.
- Produces: `NewNetworkMaintenance(metrics *MetricsStore, settings *SettingsService) *NetworkMaintenance` with `RunOnce(ctx) error` and `Start(ctx)`; a running backend with the stats poll, the new routes and nightly maintenance.

- [ ] **Step 1: Nightly maintenance, test first**

Create `backend/internal/services/network_maintenance_db_test.go`:

```go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBNetworkMaintenanceRunOnce(t *testing.T) {
	db := testdb.Open(t)
	m := NewMetricsStore(db)
	ctx := context.Background()
	testdb.Must(t, m.Write(ctx, uuid.New(), time.Now().UTC(), []SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: 1}}))
	if err := NewNetworkMaintenance(m, NewSettingsService(db)).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.samples`).Scan(&n).Error)
	if n != 0 {
		t.Errorf("orphan samples left: %d", n)
	}
}
```

Run: `cd backend && make test-db 2>&1 | grep -E "NetworkMaintenance" | head -3`
Expected: FAIL (`undefined: NewNetworkMaintenance`).

Create `backend/internal/services/network_maintenance.go`:

```go
package services

import (
	"context"
	"errors"
	"log"
	"time"
)

// NetworkMaintenanceHour is when the nightly network maintenance runs, local
// time: an hour after the incident purge (PurgeHour).
const NetworkMaintenanceHour = 3

// NetworkMaintenance is the nightly housekeeping of the metrics store:
// removing deleted devices' samples and old port events, and recomputing each
// port's usual speed.
type NetworkMaintenance struct {
	metrics  *MetricsStore
	settings *SettingsService
	now      func() time.Time
	logger   *log.Logger
}

func NewNetworkMaintenance(metrics *MetricsStore, settings *SettingsService) *NetworkMaintenance {
	return &NetworkMaintenance{metrics: metrics, settings: settings, now: time.Now, logger: log.Default()}
}

// RunOnce does one pass. Both halves run even if the first fails.
func (n *NetworkMaintenance) RunOnce(ctx context.Context) error {
	res, cleanErr := n.metrics.Cleanup(ctx, n.settings.MetricsRawRetentionDays(ctx))
	if cleanErr != nil {
		n.logger.Printf("[metrics] cleanup: %v", cleanErr)
	} else if res.SeriesRemoved+res.SamplesRemoved+res.EventsRemoved > 0 {
		n.logger.Printf("[metrics] cleanup removed %d series, %d samples, %d port events",
			res.SeriesRemoved, res.SamplesRemoved, res.EventsRemoved)
	}
	updated, speedErr := n.metrics.UpdateUsualSpeeds(ctx, n.now().UTC())
	if speedErr != nil {
		n.logger.Printf("[metrics] usual speeds: %v", speedErr)
	} else if updated > 0 {
		n.logger.Printf("[metrics] usual speed set for %d ports", updated)
	}
	return errors.Join(cleanErr, speedErr)
}

// Start runs a pass five minutes after startup, then nightly, until ctx ends.
func (n *NetworkMaintenance) Start(ctx context.Context) {
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_ = n.RunOnce(ctx)
			timer.Reset(time.Until(nextRunAt(time.Now(), NetworkMaintenanceHour)))
		}
	}
}
```

Run: `cd backend && make test-db 2>&1 | grep -E "NetworkMaintenance|^FAIL" | head -3`
Expected: PASS.

- [ ] **Step 2: Config restore with metrics present (Review Focus 3)**

Create `backend/internal/services/backup_metrics_db_test.go`:

```go
package services

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A config backup restored over a database whose metrics schema holds
// series and samples must succeed, and leave the metrics in place. Runs
// pg_dump and psql inside the test container with the backup's own
// arguments, since the host has no postgres client tools.
func TestDBRestoreWithMetrics(t *testing.T) {
	container := os.Getenv("SENTINEL_TEST_DB_CONTAINER")
	if container == "" {
		t.Skip("SENTINEL_TEST_DB_CONTAINER not set; run `make test-db`")
	}
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	ctx := context.Background()
	testdb.Must(t, NewMetricsStore(db).Write(ctx, s.DeviceID, time.Now().UTC(),
		[]SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: 5}}))

	var name string
	testdb.Must(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	b := &BackupService{db: DBConfig{Host: "127.0.0.1", Port: "5432", User: "sentinel", Name: name}}

	dump, err := exec.Command("docker", append([]string{"exec", "-e", "PGPASSWORD=test", container, "pg_dump"}, b.dumpArgs()...)...).Output()
	if err != nil {
		t.Fatalf("pg_dump: %v", err)
	}
	restore := exec.Command("docker", append([]string{"exec", "-i", "-e", "PGPASSWORD=test", container, "psql"}, b.restoreArgs()...)...)
	restore.Stdin = bytes.NewReader(dump)
	if out, err := restore.CombinedOutput(); err != nil {
		t.Fatalf("restore failed: %v\n%s", err, out)
	}

	var devices, series, samples int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM devices`).Scan(&devices).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.series WHERE device_id = ?`, s.DeviceID).Scan(&series).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.samples`).Scan(&samples).Error)
	if devices != 1 || series != 1 || samples != 1 {
		t.Errorf("after restore: %d devices, %d series, %d samples; want 1, 1, 1", devices, series, samples)
	}
}
```

Run: `cd backend && make test-db 2>&1 | grep -E "RestoreWithMetrics|^FAIL" | head -3`
Expected: PASS. A `SKIP` means `test-db.sh` did not export the container name (Task 1, Step 5).

- [ ] **Step 3: Wire it into main.go**

In `backend/cmd/sentinel/main.go`:

1. After `deviceService := services.NewDeviceService(...)`:

```go
	metricsStore := services.NewMetricsStore(db)
	portService := services.NewPortService(db, metricsStore, incidentService, settingsService)
	networkSettings := services.NewNetworkSettingsService(settingsService, metricsStore)
```

2. Next to the existing `SeedInt(settingsCtx, models.SettingIncidentRetentionDays, ...)` call, seed the four network settings, handling errors exactly as that call does:

```go
	for key, def := range map[string]int{
		models.SettingMetricsRawRetentionDays:  models.DefaultMetricsRawRetentionDays,
		models.SettingPortErrorThresholdPerMin: models.DefaultPortErrorThresholdPerMin,
		models.SettingPortUtilThresholdPct:     models.DefaultPortUtilThresholdPct,
		models.SettingPortDownGraceSeconds:     models.DefaultPortDownGraceSeconds,
	} {
		if _, err := settingsService.SeedInt(settingsCtx, key, def); err != nil {
			return fmt.Errorf("seeding %s: %w", key, err)
		}
	}
```

(If the neighbouring seed only logs a warning, log here too; match it.)

3. After `api.RegisterScanRoutes(...)`:

```go
	api.RegisterPortRoutes(v1, deviceService, portService, siteService, auditService)
	api.RegisterNetworkRoutes(v1, deviceService, portService, metricsStore, networkSettings, siteService, authService)
```

4. Replace:

```go
	devicePoller := services.NewDevicePoller(deviceService, snmpClient, incidentService, notificationManager, pollWorkers)
	go devicePoller.Start(loopCtx)
```

with:

```go
	devicePoller := services.NewDevicePoller(deviceService, snmpClient, incidentService, notificationManager, pollWorkers)
	devicePoller.SetPortStats(services.NewPortMonitor(portService, metricsStore, incidentService, notificationManager, snmpClient, settingsService))
	go devicePoller.Start(loopCtx)
	// A restored config may carry a different retention than the live policy.
	if err := networkSettings.EnsureRetention(context.Background()); err != nil {
		log.Printf("warning: could not apply the metrics retention setting: %v", err)
	}
	go services.NewNetworkMaintenance(metricsStore, settingsService).Start(loopCtx)
```

Run: `cd backend && go build ./... && go vet ./cmd/... && go test ./... 2>&1 | grep -v "^ok" | head`
Expected: no output.

- [ ] **Step 4: Simulator counters that increase**

In `deploy/snmpsim/gen_data.py`, inside `ports()`, extend the ifTable rows. After the `f"{t}.9.{i}|67|{1000 * i}",` line, add:

```python
            # Traffic: up ports move (i * 12.5 kB/s in, a quarter of that out),
            # via snmpsim's numeric variation module; errors/discards stay 0.
            *octets(f"{t}.10.{i}", 65, i * 12_500 if up == 1 and admin == 1 else 0),
            *octets(f"{t}.16.{i}", 65, i * 3_125 if up == 1 and admin == 1 else 0),
            f"{t}.13.{i}|65|0", f"{t}.14.{i}|65|0", f"{t}.19.{i}|65|0", f"{t}.20.{i}|65|0",
```

and in the `if xtable:` block add the 64-bit counters and ifConnectorPresent:

```python
                *octets(f"{x}.6.{i}", 70, i * 12_500 if up == 1 and admin == 1 else 0),
                *octets(f"{x}.10.{i}", 70, i * 3_125 if up == 1 and admin == 1 else 0),
                f"{x}.17.{i}|2|1",
```

Above `def ports(...)`, add:

```python
def octets(oid, tag, rate):
    """An octet counter growing at `rate` bytes/s (numeric variation module);
    a static 0 for a port with no traffic."""
    if rate == 0:
        return [f"{oid}|{tag}|0"]
    wrap = ",wrap=1,max=4294967295" if tag == 65 else ""
    return [f"{oid}|{tag}:numeric|rate={rate},cumulative=1,initial=0{wrap}"]
```

Regenerate and rebuild:

Run: `cd deploy/snmpsim && python3 gen_data.py && grep -c ':numeric' data/edgeswitch.snmprec && SNMPSIM_VERSION=1.2.2 ./run.sh`
Expected: `wrote [...]`, a count of 136 (34 up ports × 4 counters), and `sentinel-snmpsim running on UDP 1161-1190`.

Check that the counter moves:

Run: `docker run --rm --network host alpine sh -c "apk add -q net-snmp-tools >/dev/null && snmpget -v2c -c edgeswitch 127.0.0.1:1161 1.3.6.1.2.1.31.1.1.1.6.1 && sleep 3 && snmpget -v2c -c edgeswitch 127.0.0.1:1161 1.3.6.1.2.1.31.1.1.1.6.1"`
Expected: two `Counter64` values, the second about 37500 larger (3 s × 12500).

If the value does not grow, snmpsim's `numeric` module wants different options: check `docker exec sentinel-snmpsim sh -c 'python -c "import snmpsim, os; print(os.path.dirname(snmpsim.__file__))"'` and read `variation/numeric.py`. Adjust `octets()` to the options that make it grow, and ledger the change as a ruling.

- [ ] **Step 5: Simulator test for the stats read**

Append to `backend/internal/snmp/simulator_test.go`:

```go
// Counters read through ReadStats move between two polls, and the profile's
// down ports (every third) read as down.
func TestSimStatsCountersIncrease(t *testing.T) {
	tg := simTarget(t, Credential{Version: "2c", Community: "edgeswitch"})
	a, err := ReadStats(context.Background(), GoSNMPClient{}, tg, []int{1, 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	b, err := ReadStats(context.Background(), GoSNMPClient{}, tg, []int{1, 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !a[1].HaveOctets || !a[1].OperUp || b[1].InOctets <= a[1].InOctets || a[1].SpeedBps != 1_000_000_000 {
		t.Errorf("port 1: %+v then %+v", a[1], b[1])
	}
	if a[3].OperUp {
		t.Errorf("port 3 should be down: %+v", a[3])
	}
}
```

Run: `cd backend && SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 go test ./internal/snmp/ -run Sim -v 2>&1 | grep -E "^(---|ok|FAIL)"`
Expected: all simulator tests PASS, the phase 1 ones included.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/services/network_maintenance.go backend/internal/services/network_maintenance_db_test.go \
  backend/internal/services/backup_metrics_db_test.go backend/cmd/sentinel/main.go \
  deploy/snmpsim backend/internal/snmp/simulator_test.go
git commit -m "feat(network): wire the stats poll, routes and nightly maintenance; simulator traffic

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Frontend data layer

**Files:**
- Modify: `frontend/src/hooks/useDevices.ts`, `frontend/src/hooks/useIncidents.ts`
- Create: `frontend/src/hooks/usePorts.ts`, `frontend/src/hooks/useMetrics.ts`, `frontend/src/hooks/useNetworkSettings.ts`
- Create: `frontend/src/utils/network.ts`

**Interfaces:**
- Consumes: the API from Task 9.
- Produces:
  - Types: `DeviceType`, `DEVICE_TYPE_LABEL`, the new `Device`/`DeviceInterface` fields; `PortView`, `Faceplate`, `FaceBlock`, `FacePort`, `PortDefaults`, `DevicePorts`, `PortDetail`, `PortEvent`, `PortRef`, `SitePortSummary`, `PortPatch`, `DeviceDetailsPatch`.
  - Hooks: `useDevicePorts`, `usePort`, `usePortEvents`, `useSitePortSummary`, `usePortActions`; `MetricsRange`, `METRICS_RANGES`, `MetricsQuery`, `MetricsResult`, `useMetricsQuery`; `NetworkSettings`, `useNetworkSettings`.
  - Utils: `formatBps`, `formatPct`, `PortState`, `portState`, `PORT_STATE`, `WARNING_CONDITIONS`, `CONDITION_LABEL`, `EVENT_LABEL`, `portTitle`, `busiestUtil`.
  - `Incident.port_if_index`, `Incident.condition`.

- [ ] **Step 1: Extend the device types**

In `frontend/src/hooks/useDevices.ts`:

1. After `export type DeviceStatus = ...`:

```ts
export type DeviceType = 'switch' | 'router' | 'access_point' | 'nvr' | 'other'

export const DEVICE_TYPE_LABEL: Record<DeviceType, string> = {
  switch: 'Switch',
  router: 'Router',
  access_point: 'Access point',
  nvr: 'NVR',
  other: 'Other',
}
```

2. Add to `interface Device` (before `created_at`):

```ts
  vendor_override: string | null
  model_override: string | null
  location_override: string | null
  device_type: DeviceType | null
  device_type_detected: DeviceType
  faceplate_rows: number | null
  faceplate_sfp_ports: number[] | null
  last_stats_at: string | null
  last_stats_duration_ms: number | null
  /** The override when set, else what SNMP reported. */
  effective_vendor?: string
  effective_model?: string
  effective_location?: string
  effective_type?: DeviceType
```

3. Add to `interface DeviceInterface`:

```ts
  connector_present: boolean | null
  collect: boolean | null
  collect_default: boolean
  important: boolean
  util_threshold_pct: number | null
  error_threshold_per_min: number | null
  down_grace_seconds: number | null
  usual_speed_bps: number | null
  conditions: string[]
  conditions_since: Record<string, string>
  oper_changed_at: string | null
```

In `frontend/src/hooks/useIncidents.ts`, add to `interface Incident` after `device_id`:

```ts
  /** Set on a port incident: the port's ifIndex, for linking to its page. */
  port_if_index: number | null
  condition: string | null
```

- [ ] **Step 2: Formatting and port-state helpers**

Create `frontend/src/utils/network.ts`:

```ts
import type { PortView } from '@/hooks/usePorts'

/** "940 Mb/s", "1.2 Gb/s", "12 kb/s"; "—" when unknown. */
export function formatBps(bps: number | null | undefined): string {
  if (bps == null || Number.isNaN(bps)) return '—'
  if (bps >= 1e9) return `${+(bps / 1e9).toFixed(2)} Gb/s`
  if (bps >= 1e6) return `${+(bps / 1e6).toFixed(1)} Mb/s`
  if (bps >= 1e3) return `${+(bps / 1e3).toFixed(0)} kb/s`
  return `${Math.round(bps)} b/s`
}

export function formatPct(v: number | null | undefined): string {
  if (v == null) return '—'
  return v < 1 && v > 0 ? '<1%' : `${Math.round(v)}%`
}

export const WARNING_CONDITIONS = ['errors', 'flapping', 'slow_link', 'saturated']

export type PortState = 'critical' | 'warning' | 'up' | 'down' | 'disabled'

/** The faceplate colour rule: disabled, then important-and-down (red), then
 *  down (grey), then any warning condition (yellow), else in use (green). */
export function portState(p: { admin_status: string; oper_status: string; important: boolean; conditions: string[] }): PortState {
  if (p.admin_status === 'down') return 'disabled'
  if (p.oper_status !== 'up') return p.important ? 'critical' : 'down'
  if ((p.conditions ?? []).some((c) => WARNING_CONDITIONS.includes(c))) return 'warning'
  return 'up'
}

// Literal class strings (Tailwind only emits classes it finds spelled out).
export const PORT_STATE: Record<PortState, { label: string; fill: string; dot: string; badge: string }> = {
  critical: { label: 'Important port down', fill: 'bg-red-500', dot: 'bg-red-500', badge: 'border-red-500/30 bg-red-500/15 text-red-400' },
  warning: { label: 'Problem', fill: 'bg-yellow-500', dot: 'bg-yellow-500', badge: 'border-yellow-500/30 bg-yellow-500/15 text-yellow-400' },
  up: { label: 'In use', fill: 'bg-emerald-500', dot: 'bg-emerald-500', badge: 'border-emerald-500/30 bg-emerald-500/15 text-emerald-400' },
  down: { label: 'Nothing plugged in', fill: 'bg-slate-600', dot: 'bg-slate-500', badge: 'border-slate-500/30 bg-slate-500/15 text-slate-300' },
  disabled: { label: 'Disabled', fill: 'bg-slate-800 ring-1 ring-inset ring-slate-600', dot: 'bg-slate-700', badge: 'border-slate-600/40 bg-slate-800 text-slate-400' },
}

export const CONDITION_LABEL: Record<string, string> = {
  link_down: 'Link down',
  errors: 'Errors rising',
  flapping: 'Flapping',
  slow_link: 'Slower link than usual',
  saturated: 'Nearly full',
}

export const EVENT_LABEL: Record<string, string> = {
  link_up: 'Link up',
  link_down: 'Link down',
  flapping: 'Flapping',
  speed_change: 'Speed changed',
  errors: 'Errors rising',
  saturated: 'Nearly full',
  slow_link: 'Slower link',
  admin_up: 'Enabled',
  admin_down: 'Disabled',
}

/** "Port 51 · Uplink To Quantum Gate". */
export function portTitle(p: { number: number; alias?: string }): string {
  return p.alias ? `Port ${p.number} · ${p.alias}` : `Port ${p.number}`
}

/** The busier direction's utilisation, or null. */
export function busiestUtil(p: Pick<PortView, 'in_util_pct' | 'out_util_pct'>): number | null {
  if (p.in_util_pct == null && p.out_util_pct == null) return null
  return Math.max(p.in_util_pct ?? 0, p.out_util_pct ?? 0)
}
```

- [ ] **Step 3: Port hooks**

Create `frontend/src/hooks/usePorts.ts`:

```ts
import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { Device, DeviceInterface, DeviceType } from '@/hooks/useDevices'

export type PortCondition = 'link_down' | 'errors' | 'flapping' | 'slow_link' | 'saturated'

export interface PortView extends DeviceInterface {
  number: number
  collected: boolean
  physical: boolean
  in_bps: number | null
  out_bps: number | null
  in_util_pct: number | null
  out_util_pct: number | null
  errors_per_min: number | null
}

export interface FacePort {
  if_index: number
  number: number
}
export interface FaceBlock {
  sfp: boolean
  top: FacePort[]
  bottom: FacePort[]
}
export interface Faceplate {
  rows: number
  blocks: FaceBlock[]
}

export interface PortDefaults {
  error_threshold_per_min: number
  util_threshold_pct: number
  down_grace_seconds: number
}

export interface DevicePorts {
  ports: PortView[]
  faceplate: Faceplate
  defaults: PortDefaults
}

export interface PortIncident {
  id: string
  start_time: string
  condition: PortCondition | null
  root_cause: string
}

export interface PortDetail extends PortView {
  defaults: PortDefaults
  open_incidents: PortIncident[]
}

export interface PortEvent {
  id: number
  device_id: string
  interface_id: string
  if_index: number
  kind: string
  started_at: string
  ended_at: string | null
  detail: Record<string, unknown>
  device_name: string
  port_name: string
  port_alias: string
  port_number: number
}

export interface PortRef {
  device_id: string
  device_name: string
  if_index: number
  number: number
  name: string
  alias: string
  oper_status: string
  important: boolean
  conditions: PortCondition[]
  in_bps: number | null
  out_bps: number | null
  util_pct: number | null
}

export interface SitePortSummary {
  busiest: PortRef[]
  problems: PortRef[]
}

/** PATCH body: absent = unchanged, null = back to the default. */
export interface PortPatch {
  important?: boolean
  collect?: boolean | null
  util_threshold_pct?: number | null
  error_threshold_per_min?: number | null
  down_grace_seconds?: number | null
}

/** PATCH body: absent = unchanged, null or '' = back to what SNMP reports. */
export interface DeviceDetailsPatch {
  vendor_override?: string | null
  model_override?: string | null
  location_override?: string | null
  device_type?: DeviceType | null
  faceplate_rows?: number | null
  faceplate_sfp_ports?: number[] | null
}

const LIVE_MS = 60_000

/** GET url every refreshMs (0 = once). notFound covers 404, which the API
 *  also answers for things in a site the caller cannot see. */
function useResource<T>(url: string | null, params: Record<string, string | number> | undefined, refreshMs: number) {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [notFound, setNotFound] = useState(false)
  const paramsKey = JSON.stringify(params ?? {})

  const refetch = useCallback(async () => {
    if (!url) return
    try {
      const { data: res } = await api.get<ApiResponse<T>>(url, { params: JSON.parse(paramsKey) })
      setData(res.data)
      setError(null)
      setNotFound(false)
    } catch (err) {
      const e = err as ApiError
      if (e.status === 404) {
        setNotFound(true)
        setData(null)
      }
      setError(e.message || 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [url, paramsKey])

  useEffect(() => {
    setLoading(true)
    void refetch()
    if (!refreshMs) return
    const t = window.setInterval(() => void refetch(), refreshMs)
    return () => window.clearInterval(t)
  }, [refetch, refreshMs])

  return { data, loading, error, notFound, refetch }
}

/** A device's ports, faceplate and default thresholds, every minute. */
export function useDevicePorts(deviceId: string | undefined) {
  return useResource<DevicePorts>(deviceId ? `/devices/${deviceId}/ports` : null, undefined, LIVE_MS)
}

export function usePort(deviceId: string | undefined, ifIndex: string | undefined) {
  return useResource<PortDetail>(deviceId && ifIndex ? `/devices/${deviceId}/ports/${ifIndex}` : null, undefined, LIVE_MS)
}

/** Port events for a device (optionally one port) or a site, newest first. */
export function usePortEvents(scope: { deviceId?: string; siteId?: string; ifIndex?: number }, limit = 20) {
  const url = scope.deviceId ? `/devices/${scope.deviceId}/events` : scope.siteId ? `/sites/${scope.siteId}/port-events` : null
  const params: Record<string, number> = { limit }
  if (scope.ifIndex) params.if_index = scope.ifIndex
  const r = useResource<{ events: PortEvent[]; total: number }>(url, params, LIVE_MS)
  return { events: r.data?.events ?? [], total: r.data?.total ?? 0, loading: r.loading, refetch: r.refetch }
}

export function useSitePortSummary(siteId: string | undefined) {
  return useResource<SitePortSummary>(siteId ? `/sites/${siteId}/ports/summary` : null, undefined, LIVE_MS)
}

export function usePortActions() {
  const [busy, setBusy] = useState(false)
  const run = useCallback(async <T,>(fn: () => Promise<T>): Promise<T> => {
    setBusy(true)
    try {
      return await fn()
    } finally {
      setBusy(false)
    }
  }, [])
  const updatePort = useCallback(
    (deviceId: string, ifIndex: number, patch: PortPatch) =>
      run(async () => (await api.patch<ApiResponse<DeviceInterface>>(`/devices/${deviceId}/ports/${ifIndex}`, patch)).data.data),
    [run]
  )
  const updateDetails = useCallback(
    (deviceId: string, patch: DeviceDetailsPatch) =>
      run(async () => (await api.patch<ApiResponse<Device>>(`/devices/${deviceId}/details`, patch)).data.data),
    [run]
  )
  return { updatePort, updateDetails, busy }
}
```

- [ ] **Step 4: Metrics and settings hooks**

Create `frontend/src/hooks/useMetrics.ts`:

```ts
import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type MetricsRange = '1h' | '6h' | '24h' | '7d' | '30d' | '90d' | '1y'

export const METRICS_RANGES: { value: MetricsRange; label: string }[] = [
  { value: '1h', label: 'Last hour' },
  { value: '6h', label: 'Last 6 hours' },
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
  { value: '30d', label: 'Last 30 days' },
  { value: '90d', label: 'Last 90 days' },
  { value: '1y', label: 'Last year' },
]

export interface MetricPoint {
  t: string
  avg: number
  min: number
  max: number
}
export interface MetricSeries {
  device_id: string | null
  metric: string
  instance: string
  points: MetricPoint[]
}
export interface MetricsResult {
  resolution: 'raw' | '5m' | '1h'
  step_seconds: number
  series: MetricSeries[]
}

export interface MetricsQuery {
  deviceIds?: string[]
  siteId?: string
  metrics: string[]
  instances?: string[]
  range: MetricsRange
  /** Add up every selected series per metric (device and site totals). */
  sum?: boolean
  /** With sum: only physical ports, so a link aggregate is not counted twice. */
  physicalOnly?: boolean
}

/** Short ranges refresh every minute; long ones every five. */
function refreshFor(range: MetricsRange): number {
  return range === '1h' || range === '6h' || range === '24h' ? 60_000 : 300_000
}

export function useMetricsQuery(q: MetricsQuery | null) {
  const [result, setResult] = useState<MetricsResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const key = q ? JSON.stringify(q) : ''

  const refetch = useCallback(async () => {
    if (!key) return
    const query = JSON.parse(key) as MetricsQuery
    // Repeated keys (metric=a&metric=b) are what the API reads; axios's
    // default array format (metric[]=a) is not, so the params are built here.
    const params = new URLSearchParams()
    query.metrics.forEach((m) => params.append('metric', m))
    query.deviceIds?.forEach((d) => params.append('device_id', d))
    if (query.siteId) params.set('site_id', query.siteId)
    query.instances?.forEach((i) => params.append('instance', i))
    params.set('range', query.range)
    if (query.sum) params.set('agg', 'sum')
    if (query.physicalOnly) params.set('ports', 'physical')
    try {
      const { data } = await api.get<ApiResponse<MetricsResult>>('/network/metrics/query', { params })
      setResult(data.data)
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load the chart')
    } finally {
      setLoading(false)
    }
  }, [key])

  useEffect(() => {
    if (!key) return
    setLoading(true)
    void refetch()
    const range = (JSON.parse(key) as MetricsQuery).range
    const t = window.setInterval(() => void refetch(), refreshFor(range))
    return () => window.clearInterval(t)
  }, [key, refetch])

  return { result, loading, error }
}
```

Create `frontend/src/hooks/useNetworkSettings.ts`:

```ts
import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export interface NetworkSettings {
  metrics_raw_retention_days: number
  port_error_threshold_per_min: number
  port_util_threshold_pct: number
  port_down_grace_seconds: number
}

export function useNetworkSettings() {
  const [settings, setSettings] = useState<NetworkSettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const refetch = useCallback(async () => {
    try {
      const { data } = await api.get<ApiResponse<NetworkSettings>>('/network/settings')
      setSettings(data.data)
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load network settings')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  const save = useCallback(async (patch: Partial<NetworkSettings>) => {
    setSaving(true)
    try {
      const { data } = await api.patch<ApiResponse<NetworkSettings>>('/network/settings', patch)
      setSettings(data.data)
      return data.data
    } finally {
      setSaving(false)
    }
  }, [])

  return { settings, loading, error, saving, save }
}
```

- [ ] **Step 5: Type-check and lint**

Run the frontend check from Global Constraints.
Expected: exit 0, with no TypeScript or ESLint output.

`utils/network.ts` imports a type from `usePorts.ts`, and `usePorts.ts` imports types from `useDevices.ts`. These are type-only imports and cannot form a runtime cycle.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/hooks frontend/src/utils/network.ts
git commit -m "feat(network): frontend data layer for ports, metrics and settings

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 12: The faceplate and the reworked device page

**Files:**
- Create: `frontend/src/components/network/Faceplate.tsx`, `TrafficChart.tsx`, `PortTable.tsx`, `PortEventList.tsx`, `EditDetailsModal.tsx`
- Modify (rewrite): `frontend/src/pages/network/DeviceDetail.tsx`

**Interfaces:**
- Consumes: Task 11's hooks and utils; `formatSpeed`, `useDevice`, `useDeviceActions` (`useDevices.ts`); `useIncidents`, `formatDuration`, `DEFAULT_FILTERS` (`useIncidents.ts`); `useSite` (`useSites.ts`).
- Produces:
  - `<Faceplate layout ports title subtitle selected onSelect />` and `<FaceplateLegend />`
  - `<TrafficChart title query lines unit threshold? defaultRange? />` with `ChartLine{metric,label,colour}`
  - `<PortTable deviceId ports canEdit onChanged />`
  - `<PortEventList events showDevice? emptyText? />`
  - `<EditDetailsModal device onClose onSaved />`

This task has no unit-test runner (the frontend has none). Its gate is the type-check, lint and build, then the browser checks in Task 15.

- [ ] **Step 1: Faceplate**

Create `frontend/src/components/network/Faceplate.tsx`:

```tsx
import { useMemo } from 'react'
import type { FaceBlock, FacePort, Faceplate as Layout, PortView } from '@/hooks/usePorts'
import { busiestUtil, PORT_STATE, portState, portTitle, type PortState } from '@/utils/network'

// RJ45 outline with the latch notch facing away from the other row, as on
// the real switch: top-row notch at the bottom, bottom-row notch at the top.
const RJ45_TOP = 'polygon(0 0,100% 0,100% 70%,72% 70%,72% 100%,28% 100%,28% 70%,0 70%)'
const RJ45_BOTTOM = 'polygon(28% 0,72% 0,72% 30%,100% 30%,100% 100%,0 100%,0 30%,28% 30%)'

interface Props {
  layout: Layout
  ports: PortView[]
  title: string
  subtitle?: string
  selected: number | null
  onSelect: (ifIndex: number) => void
}

/** The virtual switch: every physical port coloured by state and filled from
 *  the bottom by how busy it is. */
export default function Faceplate({ layout, ports, title, subtitle, selected, onSelect }: Props) {
  const byIndex = useMemo(() => new Map(ports.map((p) => [p.if_index, p])), [ports])
  return (
    <div className="overflow-x-auto pb-1">
      <div className="inline-flex items-center gap-5 rounded-lg border border-slate-700/70 bg-gradient-to-b from-slate-900 to-slate-950 px-4 py-3 shadow-inner">
        <div className="w-28 shrink-0">
          <p className="flex items-center gap-1.5 truncate text-xs font-semibold text-slate-200">
            <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-400" aria-hidden />
            {title}
          </p>
          {subtitle && <p className="truncate text-[11px] text-slate-500">{subtitle}</p>}
        </div>
        {layout.blocks.map((b, i) => (
          <Block key={i} block={b} rows={layout.rows} byIndex={byIndex} selected={selected} onSelect={onSelect} />
        ))}
      </div>
    </div>
  )
}

function Block({
  block,
  rows,
  byIndex,
  selected,
  onSelect,
}: {
  block: FaceBlock
  rows: number
  byIndex: Map<number, PortView>
  selected: number | null
  onSelect: (ifIndex: number) => void
}) {
  const cols = Math.max(block.top.length, block.bottom.length)
  const cell = (fp: FacePort | undefined, flip: boolean) =>
    fp ? (
      <PortCell fp={fp} port={byIndex.get(fp.if_index)} sfp={block.sfp} flip={flip} selected={selected === fp.if_index} onSelect={onSelect} />
    ) : (
      <span className={`h-5 ${block.sfp ? 'w-8' : 'w-6'}`} />
    )
  return (
    <div className="flex gap-1">
      {Array.from({ length: cols }, (_, c) => (
        <div key={c} className="flex flex-col items-center gap-1">
          <Num fp={block.top[c]} />
          {cell(block.top[c], false)}
          {rows === 2 && cell(block.bottom[c], true)}
          {rows === 2 && <Num fp={block.bottom[c]} />}
        </div>
      ))}
    </div>
  )
}

function Num({ fp }: { fp?: FacePort }) {
  return <span className="h-3 text-[9.5px] leading-3 tabular-nums text-slate-500">{fp?.number ?? ''}</span>
}

function PortCell({
  fp,
  port,
  sfp,
  flip,
  selected,
  onSelect,
}: {
  fp: FacePort
  port: PortView | undefined
  sfp: boolean
  flip: boolean
  selected: boolean
  onSelect: (ifIndex: number) => void
}) {
  const state: PortState = port ? portState(port) : 'down'
  const util = port ? busiestUtil(port) : null
  const label = port ? `${portTitle(port)}: ${PORT_STATE[state].label}` : `Port ${fp.number}`
  return (
    <button
      type="button"
      onClick={() => onSelect(fp.if_index)}
      title={label}
      aria-label={label}
      aria-pressed={selected}
      className={`relative h-5 ${sfp ? 'w-8' : 'w-6'} overflow-hidden rounded-sm transition hover:brightness-125 focus:outline-none focus-visible:ring-2 focus-visible:ring-sky-400 ${PORT_STATE[state].fill} ${selected ? 'ring-2 ring-sky-400' : ''}`}
      style={sfp || selected ? undefined : { clipPath: flip ? RJ45_BOTTOM : RJ45_TOP }}
    >
      {util != null && util > 0 && (
        <span className="absolute inset-x-0 bottom-0 bg-white/55" style={{ height: `${Math.min(100, Math.max(8, util))}%` }} />
      )}
    </button>
  )
}

export function FaceplateLegend() {
  const order: PortState[] = ['up', 'warning', 'critical', 'down', 'disabled']
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-400">
      {order.map((s) => (
        <span key={s} className="flex items-center gap-1.5">
          <span className={`h-2.5 w-3.5 rounded-sm ${PORT_STATE[s].fill}`} aria-hidden />
          {PORT_STATE[s].label}
        </span>
      ))}
      <span className="flex items-center gap-1.5">
        <span className="relative h-2.5 w-3.5 overflow-hidden rounded-sm bg-emerald-500" aria-hidden>
          <span className="absolute inset-x-0 bottom-0 h-1/2 bg-white/55" />
        </span>
        Fill shows how busy
      </span>
    </div>
  )
}
```

- [ ] **Step 2: Chart**

Create `frontend/src/components/network/TrafficChart.tsx`:

```tsx
import { useId, useMemo, useState } from 'react'
import { Area, AreaChart, CartesianGrid, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { Loader2 } from 'lucide-react'
import { METRICS_RANGES, useMetricsQuery, type MetricsQuery, type MetricsRange } from '@/hooks/useMetrics'
import { formatBps } from '@/utils/network'

export interface ChartLine {
  metric: string
  label: string
  colour: string
}

type Unit = 'bps' | 'pct' | 'per_min'

interface Props {
  title: string
  query: Omit<MetricsQuery, 'range' | 'metrics'>
  lines: ChartLine[]
  unit: Unit
  /** Drawn as a dashed line, e.g. the nearly-full threshold. */
  threshold?: number
  defaultRange?: MetricsRange
}

function formatValue(v: number, unit: Unit): string {
  if (unit === 'bps') return formatBps(v)
  if (unit === 'pct') return `${Math.round(v)}%`
  return `${+v.toFixed(1)}/min`
}

const SHORT: MetricsRange[] = ['1h', '6h', '24h']

/** A metrics chart with its own range picker. Several instances of one
 *  metric in the result (a device's ports) are added together. */
export default function TrafficChart({ title, query, lines, unit, threshold, defaultRange = '24h' }: Props) {
  const gid = useId().replace(/:/g, '')
  const [range, setRange] = useState<MetricsRange>(defaultRange)
  const metrics = lines.map((l) => l.metric)
  // Keyed by value: callers pass a fresh object every render.
  const key = JSON.stringify({ ...query, metrics, range })
  const q = useMemo(() => JSON.parse(key) as MetricsQuery, [key])
  const { result, loading, error } = useMetricsQuery(q)

  const data = useMemo(() => {
    const rows = new Map<number, Record<string, number>>()
    for (const s of result?.series ?? []) {
      for (const p of s.points) {
        const t = new Date(p.t).getTime()
        const row = rows.get(t) ?? { t }
        row[s.metric] = (row[s.metric] ?? 0) + p.avg
        rows.set(t, row)
      }
    }
    return [...rows.values()].sort((a, b) => a.t - b.t)
  }, [result])

  const tick = (t: number) => {
    const d = new Date(t)
    return SHORT.includes(range)
      ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      : d.toLocaleDateString([], { month: 'short', day: 'numeric' })
  }

  return (
    <section>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-light text-white">{title}</h2>
        <select
          value={range}
          onChange={(e) => setRange(e.target.value as MetricsRange)}
          aria-label={`${title} time range`}
          className="cursor-pointer rounded-lg border border-white/10 bg-slate-800/50 px-3 py-1.5 text-sm text-white focus:border-white/30 focus:outline-none"
        >
          {METRICS_RANGES.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>
      </div>
      {loading && !result ? (
        <div className="flex items-center gap-2 rounded-lg border border-white/10 bg-slate-800/40 p-10 text-sm text-slate-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Loading…
        </div>
      ) : error ? (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-400">{error}</div>
      ) : data.length === 0 ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-10 text-center text-sm text-slate-400">
          No data in this range yet. Figures appear a minute or two after the first stats poll.
        </div>
      ) : (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-4">
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={data} margin={{ top: 5, right: 10, bottom: 0, left: 0 }}>
                <defs>
                  {lines.map((l) => (
                    <linearGradient key={l.metric} id={`${gid}-${l.metric}`} x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor={l.colour} stopOpacity={0.25} />
                      <stop offset="95%" stopColor={l.colour} stopOpacity={0} />
                    </linearGradient>
                  ))}
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#16303a" strokeOpacity={0.6} />
                <XAxis dataKey="t" type="number" scale="time" domain={['dataMin', 'dataMax']} tickFormatter={tick}
                  tick={{ fontSize: 10, fill: '#7A8A94' }} minTickGap={24} />
                <YAxis tickFormatter={(v: number) => formatValue(v, unit)} tick={{ fontSize: 10, fill: '#7A8A94' }} width={68} />
                <Tooltip
                  labelFormatter={(t: number) => new Date(t).toLocaleString()}
                  formatter={(v: number, name) => [formatValue(v, unit), lines.find((l) => l.metric === String(name))?.label ?? name]}
                  contentStyle={{ background: '#0f172a', border: '1px solid rgba(255,255,255,0.1)', borderRadius: 8, color: '#e2e8f0' }}
                />
                {threshold != null && <ReferenceLine y={threshold} stroke="#eab308" strokeDasharray="4 4" />}
                {lines.map((l) => (
                  <Area key={l.metric} type="monotone" dataKey={l.metric} stroke={l.colour} strokeWidth={2}
                    fill={`url(#${gid}-${l.metric})`} connectNulls={false} isAnimationActive={false} />
                ))}
              </AreaChart>
            </ResponsiveContainer>
          </div>
          <div className="mt-2 flex flex-wrap gap-4 text-xs text-slate-400">
            {lines.map((l) => (
              <span key={l.metric} className="flex items-center gap-1.5">
                <span className="h-0.5 w-4" style={{ background: l.colour }} aria-hidden /> {l.label}
              </span>
            ))}
            {result && result.step_seconds > 60 && (
              <span className="ml-auto text-slate-500">Averaged per {Math.round(result.step_seconds / 60)} minutes</span>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
```

- [ ] **Step 3: Port table, event list, details modal**

Create `frontend/src/components/network/PortTable.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Star } from 'lucide-react'
import { formatSpeed } from '@/hooks/useDevices'
import { usePortActions, type PortPatch, type PortView } from '@/hooks/usePorts'
import { busiestUtil, CONDITION_LABEL, formatBps, formatPct, PORT_STATE, portState, WARNING_CONDITIONS, type PortState } from '@/utils/network'
import type { ApiError } from '@/services/api'

type SortKey = 'number' | 'alias' | 'state' | 'speed' | 'in' | 'out' | 'busy' | 'errors'
const STATE_ORDER: Record<PortState, number> = { critical: 0, warning: 1, up: 2, down: 3, disabled: 4 }

function sortValue(p: PortView, key: SortKey): number | string {
  switch (key) {
    case 'number':
      return p.number
    case 'alias':
      return (p.alias || p.name).toLowerCase()
    case 'state':
      return STATE_ORDER[portState(p)]
    case 'speed':
      return p.oper_status === 'up' ? p.speed_bps : -1
    case 'in':
      return p.in_bps ?? -1
    case 'out':
      return p.out_bps ?? -1
    case 'busy':
      return busiestUtil(p) ?? -1
    case 'errors':
      return p.errors_per_min ?? -1
  }
}

interface Props {
  deviceId: string
  ports: PortView[]
  canEdit: boolean
  onChanged: () => void
}

/** Every port with its live figures; sortable, with the important star and
 *  the collect switch for editors. */
export default function PortTable({ deviceId, ports, canEdit, onChanged }: Props) {
  const { updatePort, busy } = usePortActions()
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'number', desc: false })
  const [showAll, setShowAll] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const rows = useMemo(() => {
    const list = ports.filter((p) => showAll || p.physical)
    return [...list].sort((a, b) => {
      const x = sortValue(a, sort.key)
      const y = sortValue(b, sort.key)
      const c = x < y ? -1 : x > y ? 1 : a.if_index - b.if_index
      return sort.desc ? -c : c
    })
  }, [ports, showAll, sort])

  const change = async (p: PortView, patch: PortPatch) => {
    setError(null)
    try {
      await updatePort(deviceId, p.if_index, patch)
      onChanged()
    } catch (err) {
      setError((err as ApiError).message || 'Could not update the port')
    }
  }

  const head = (key: SortKey, label: string, right = false) => (
    <th className={`px-3 py-2 font-medium ${right ? 'text-right' : ''}`}>
      <button
        type="button"
        className="hover:text-slate-200"
        onClick={() => setSort((s) => ({ key, desc: s.key === key ? !s.desc : key !== 'number' && key !== 'alias' }))}
      >
        {label}
        {sort.key === key ? (sort.desc ? ' ↓' : ' ↑') : ''}
      </button>
    </th>
  )

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-light text-white">Ports ({rows.length})</h2>
        <label className="flex items-center gap-2 text-sm text-slate-400">
          <input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} /> Show aggregates and virtual interfaces
        </label>
      </div>
      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}
      {rows.length === 0 ? (
        <p className="text-sm text-slate-500">No ports reported yet. The list arrives with the first inventory.</p>
      ) : (
        <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                  <th className="w-8 px-3 py-2">
                    <span className="sr-only">Important</span>
                  </th>
                  {head('number', 'Port')}
                  {head('alias', 'Name')}
                  {head('state', 'Status')}
                  {head('speed', 'Speed')}
                  {head('in', 'In', true)}
                  {head('out', 'Out', true)}
                  {head('busy', 'Busy', true)}
                  {head('errors', 'Errors/min', true)}
                  <th className="px-3 py-2 font-medium">Collect</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {rows.map((p) => {
                  const st = portState(p)
                  const warns = (p.conditions ?? []).filter((c) => WARNING_CONDITIONS.includes(c))
                  return (
                    <tr key={p.id} className={p.collected ? '' : 'opacity-60'}>
                      <td className="px-3 py-2">
                        {canEdit ? (
                          <button
                            type="button"
                            disabled={busy}
                            onClick={() => void change(p, { important: !p.important })}
                            aria-label={p.important ? 'Stop treating as important' : 'Mark as important'}
                            title={p.important ? 'Important: alerts are on' : 'Mark as important to get alerts'}
                          >
                            <Star className={`h-4 w-4 ${p.important ? 'fill-amber-400 text-amber-400' : 'text-slate-600 hover:text-slate-400'}`} />
                          </button>
                        ) : (
                          p.important && <Star className="h-4 w-4 fill-amber-400 text-amber-400" aria-label="Important" />
                        )}
                      </td>
                      <td className="px-3 py-2 font-medium tabular-nums">
                        <Link to={`/network/devices/${deviceId}/ports/${p.if_index}`} className="text-slate-200 hover:text-primary-400" title={p.name}>
                          {p.number}
                        </Link>
                      </td>
                      <td className="max-w-[16rem] truncate px-3 py-2 text-slate-300" title={p.alias || p.name}>
                        {p.alias || <span className="text-slate-500">{p.name}</span>}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2">
                        <span className={`inline-flex rounded-full border px-2 py-0.5 text-xs ${PORT_STATE[st].badge}`}>{PORT_STATE[st].label}</span>
                        {warns.length > 0 && <span className="ml-2 text-xs text-yellow-400">{warns.map((c) => CONDITION_LABEL[c]).join(', ')}</span>}
                      </td>
                      <td className="px-3 py-2 tabular-nums text-slate-400">{p.oper_status === 'up' ? formatSpeed(p.speed_bps) : '—'}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-slate-300">{formatBps(p.in_bps)}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-slate-300">{formatBps(p.out_bps)}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-slate-300">{formatPct(busiestUtil(p))}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-slate-300">
                        {p.errors_per_min == null ? '—' : +p.errors_per_min.toFixed(1)}
                      </td>
                      <td className="px-3 py-2">
                        {canEdit ? (
                          <input
                            type="checkbox"
                            checked={p.collected}
                            disabled={busy}
                            onChange={() => void change(p, { collect: !p.collected })}
                            aria-label={`Collect statistics for port ${p.number}`}
                          />
                        ) : (
                          <span className="text-slate-400">{p.collected ? 'Yes' : 'No'}</span>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </section>
  )
}
```

Create `frontend/src/components/network/PortEventList.tsx`:

```tsx
import { Link } from 'react-router-dom'
import type { PortEvent } from '@/hooks/usePorts'
import { formatDuration } from '@/hooks/useIncidents'
import { EVENT_LABEL, formatBps, portTitle } from '@/utils/network'

const DOT: Record<string, string> = {
  link_up: 'bg-emerald-500',
  link_down: 'bg-slate-500',
  flapping: 'bg-yellow-500',
  speed_change: 'bg-sky-500',
  errors: 'bg-yellow-500',
  saturated: 'bg-yellow-500',
  slow_link: 'bg-yellow-500',
  admin_up: 'bg-slate-400',
  admin_down: 'bg-slate-700',
}
const SPANS = new Set(['flapping', 'errors', 'saturated', 'slow_link'])

function describe(e: PortEvent): string {
  const d = e.detail ?? {}
  const num = (k: string) => (typeof d[k] === 'number' ? (d[k] as number) : null)
  switch (e.kind) {
    case 'speed_change':
      return `${formatBps(num('from_bps'))} → ${formatBps(num('to_bps'))}`
    case 'link_up':
      return d.between_polls ? 'went down and came back between polls' : num('speed_bps') ? `at ${formatBps(num('speed_bps'))}` : ''
    case 'link_down':
      return d.between_polls ? 'went down and came back between polls' : ''
    case 'flapping':
      return num('transitions') != null ? `${num('transitions')} link changes` : ''
    case 'errors':
      return num('per_minute') != null ? `${num('per_minute')} per minute` : ''
    case 'saturated':
      return num('util_pct') != null ? `${num('util_pct')}% busy` : ''
    case 'slow_link':
      return num('speed_bps') != null && num('usual_speed_bps') != null
        ? `${formatBps(num('speed_bps'))} instead of ${formatBps(num('usual_speed_bps'))}`
        : ''
    default:
      return ''
  }
}

export default function PortEventList({
  events,
  showDevice = false,
  emptyText = 'No port events yet.',
}: {
  events: PortEvent[]
  showDevice?: boolean
  emptyText?: string
}) {
  if (events.length === 0) return <p className="text-sm text-slate-500">{emptyText}</p>
  return (
    <ul className="card divide-y divide-white/10">
      {events.map((e) => {
        const span = SPANS.has(e.kind)
        const lasted =
          span && e.ended_at ? formatDuration((new Date(e.ended_at).getTime() - new Date(e.started_at).getTime()) / 1000) : null
        return (
          <li key={e.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
            <span className={`h-2 w-2 shrink-0 rounded-full ${DOT[e.kind] ?? 'bg-slate-500'}`} aria-hidden />
            <span className="w-44 shrink-0 tabular-nums text-slate-400">{new Date(e.started_at).toLocaleString()}</span>
            <span className="font-medium text-slate-200">{EVENT_LABEL[e.kind] ?? e.kind}</span>
            <Link to={`/network/devices/${e.device_id}/ports/${e.if_index}`} className="text-slate-300 hover:text-primary-400">
              {showDevice ? `${e.device_name} · ` : ''}
              {portTitle({ number: e.port_number, alias: e.port_alias })}
            </Link>
            <span className="text-slate-500">{describe(e)}</span>
            {span && <span className="ml-auto text-xs text-slate-500">{lasted ? `lasted ${lasted}` : 'ongoing'}</span>}
          </li>
        )
      })}
    </ul>
  )
}
```

Create `frontend/src/components/network/EditDetailsModal.tsx`:

```tsx
import { useState } from 'react'
import { X } from 'lucide-react'
import { DEVICE_TYPE_LABEL, type Device, type DeviceType } from '@/hooks/useDevices'
import { usePortActions } from '@/hooks/usePorts'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  device: Device
  onClose: () => void
  onSaved: () => void
}

/** The user's corrections to what SNMP reports. Inventory never overwrites
 *  them; clearing a field shows the SNMP value again. */
export default function EditDetailsModal({ device, onClose, onSaved }: Props) {
  const { updateDetails, busy } = usePortActions()
  const [vendor, setVendor] = useState(device.vendor_override ?? '')
  const [model, setModel] = useState(device.model_override ?? '')
  const [location, setLocation] = useState(device.location_override ?? '')
  const [type, setType] = useState<DeviceType | ''>(device.device_type ?? '')
  const [rows, setRows] = useState(device.faceplate_rows ? String(device.faceplate_rows) : '')
  const [sfp, setSfp] = useState((device.faceplate_sfp_ports ?? []).join(', '))
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    const ports = sfp.split(/[\s,]+/).filter(Boolean).map(Number)
    if (ports.some((n) => !Number.isInteger(n) || n < 1)) {
      setError('SFP ports are port numbers, e.g. 49, 50, 51, 52')
      return
    }
    try {
      await updateDetails(device.id, {
        vendor_override: vendor,
        model_override: model,
        location_override: location,
        device_type: type || null,
        faceplate_rows: rows ? Number(rows) : null,
        faceplate_sfp_ports: ports.length ? ports : null,
      })
      onSaved()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the details')
    }
  }

  const field = (label: string, value: string, set: (v: string) => void, reported: string) => (
    <label className="block space-y-1">
      <span className="text-sm text-slate-300">{label}</span>
      <input className={inputCls} value={value} onChange={(e) => set(e.target.value)} placeholder={reported || 'Not reported'} maxLength={255} />
      <span className="flex items-center justify-between gap-2 text-xs text-slate-500">
        <span className="truncate">SNMP says: {reported || 'nothing'}</span>
        {value && (
          <button type="button" className="shrink-0 text-primary-400 hover:underline" onClick={() => set('')}>
            Use SNMP value
          </button>
        )}
      </span>
    </label>
  )

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card max-h-[90vh] w-full max-w-md space-y-4 overflow-y-auto p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">Edit device details</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p className="text-sm text-slate-400">Your values are shown instead of what the device reports, and are never overwritten.</p>

        {field('Vendor', vendor, setVendor, device.vendor)}
        {field('Model', model, setModel, device.model)}
        {field('Location', location, setLocation, device.sys_location)}

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Device type</span>
          <select className={inputCls} value={type} onChange={(e) => setType(e.target.value as DeviceType | '')}>
            <option value="">Automatic ({DEVICE_TYPE_LABEL[device.device_type_detected] ?? 'Other'})</option>
            {(Object.keys(DEVICE_TYPE_LABEL) as DeviceType[]).map((t) => (
              <option key={t} value={t}>
                {DEVICE_TYPE_LABEL[t]}
              </option>
            ))}
          </select>
          <span className="text-xs text-slate-500">Switches and routers are drawn as a faceplate.</span>
        </label>

        <fieldset className="space-y-3 rounded-lg border border-white/10 p-3">
          <legend className="px-1 text-sm text-slate-300">Faceplate</legend>
          <label className="block space-y-1">
            <span className="text-xs text-slate-400">Rows</span>
            <select className={inputCls} value={rows} onChange={(e) => setRows(e.target.value)}>
              <option value="">Automatic</option>
              <option value="1">One row</option>
              <option value="2">Two rows</option>
            </select>
          </label>
          <label className="block space-y-1">
            <span className="text-xs text-slate-400">SFP ports</span>
            <input className={inputCls} value={sfp} onChange={(e) => setSfp(e.target.value)} placeholder="Detected automatically" />
            <span className="text-xs text-slate-500">Port numbers to draw as SFP, e.g. 49, 50, 51, 52.</span>
          </label>
        </fieldset>

        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy}>
            Save details
          </button>
        </div>
      </form>
    </div>
  )
}
```

- [ ] **Step 4: Rewrite the device page**

Replace `frontend/src/pages/network/DeviceDetail.tsx` with:

```tsx
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Pause, Pencil, Play, RefreshCw, SlidersHorizontal, Trash2 } from 'lucide-react'
import { useSite } from '@/hooks/useSites'
import { DEVICE_TYPE_LABEL, formatSpeed, useDevice, useDeviceActions, type Device } from '@/hooks/useDevices'
import { useIncidents, formatDuration, DEFAULT_FILTERS } from '@/hooks/useIncidents'
import { useDevicePorts, usePortEvents, type PortView } from '@/hooks/usePorts'
import DeviceStatusBadge from '@/components/network/DeviceStatusBadge'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import EditDetailsModal from '@/components/network/EditDetailsModal'
import Faceplate, { FaceplateLegend } from '@/components/network/Faceplate'
import TrafficChart from '@/components/network/TrafficChart'
import PortTable from '@/components/network/PortTable'
import PortEventList from '@/components/network/PortEventList'
import { busiestUtil, CONDITION_LABEL, formatBps, formatPct, PORT_STATE, portState, portTitle, WARNING_CONDITIONS } from '@/utils/network'
import type { ApiError } from '@/services/api'

/** Device types drawn as a faceplate; the rest get the port table only. */
const FACEPLATE_TYPES = new Set(['switch', 'router'])

const TRAFFIC_LINES = [
  { metric: 'if_in_bps', label: 'In', colour: '#22d3ee' },
  { metric: 'if_out_bps', label: 'Out', colour: '#a78bfa' },
]

function upSince(d: Device): string {
  if (d.sys_uptime_seconds == null || !d.last_seen_at) return '—'
  const since = new Date(new Date(d.last_seen_at).getTime() - d.sys_uptime_seconds * 1000)
  return `${since.toLocaleString()} (${formatDuration(d.sys_uptime_seconds)})`
}

function toInput(d: Device, enabled: boolean) {
  return {
    site_id: d.site_id, credential_id: d.credential_id, name: d.name, host: d.host, port: d.port, enabled,
    poll_interval: d.poll_interval, timeout_ms: d.timeout_ms, retries: d.retries, notify_channels: d.notify_channels,
  }
}

function PortQuickPanel({ deviceId, port }: { deviceId: string; port: PortView }) {
  const st = portState(port)
  const warns = (port.conditions ?? []).filter((c) => WARNING_CONDITIONS.includes(c))
  const facts: [string, string][] = [
    ['Link', port.oper_status === 'up' ? formatSpeed(port.speed_bps) : '—'],
    ['In', formatBps(port.in_bps)],
    ['Out', formatBps(port.out_bps)],
    ['Busy', formatPct(busiestUtil(port))],
    ['Errors/min', port.errors_per_min == null ? '—' : String(+port.errors_per_min.toFixed(1))],
  ]
  return (
    <div className="card flex flex-wrap items-start gap-x-8 gap-y-3 p-4">
      <div className="min-w-[12rem]">
        <p className="font-medium text-white">{portTitle(port)}</p>
        <div className="mt-1 flex flex-wrap items-center gap-2">
          <span className={`rounded-full border px-2 py-0.5 text-xs ${PORT_STATE[st].badge}`}>{PORT_STATE[st].label}</span>
          {port.important && <span className="rounded-full border border-sky-500/30 bg-sky-500/10 px-2 py-0.5 text-xs text-sky-300">Important</span>}
          {warns.map((c) => (
            <span key={c} className="text-xs text-yellow-400">
              {CONDITION_LABEL[c]}
            </span>
          ))}
        </div>
      </div>
      {facts.map(([k, v]) => (
        <div key={k}>
          <p className="text-[11px] uppercase tracking-widest text-slate-500">{k}</p>
          <p className="text-sm tabular-nums text-slate-200">{v}</p>
        </div>
      ))}
      <Link to={`/network/devices/${deviceId}/ports/${port.if_index}`} className="ml-auto self-center text-sm text-primary-400 hover:underline">
        Open port page →
      </Link>
    </div>
  )
}

export default function DeviceDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { device, loading, notFound, refetch } = useDevice(id)
  const { site } = useSite(device?.site_id)
  const { data: portsView, refetch: refetchPorts } = useDevicePorts(id)
  const { events } = usePortEvents({ deviceId: id }, 15)
  const { incidents } = useIncidents({ ...DEFAULT_FILTERS, limit: 10, deviceId: id })
  const { update, remove, refresh, busy } = useDeviceActions()
  const [editing, setEditing] = useState(false)
  const [editingDetails, setEditingDetails] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<number | null>(null)

  if (loading) return <p className="text-sm text-slate-400">Loading…</p>
  if (notFound || !device) {
    return (
      <div className="card p-8 text-center">
        <p className="text-slate-300">Device not found.</p>
        <Link to="/network/devices" className="mt-2 inline-block text-sm text-primary-400 hover:underline">
          Back to devices
        </Link>
      </div>
    )
  }

  const canEdit = site?.access === 'admin' || site?.access === 'editable'
  const act = async (fn: () => Promise<unknown>) => {
    setError(null)
    try {
      await fn()
      await refetch()
    } catch (err) {
      setError((err as ApiError).message || 'Something went wrong')
    }
  }
  const type = device.effective_type ?? device.device_type_detected ?? 'other'
  const ports = portsView?.ports ?? []
  const showFaceplate = FACEPLATE_TYPES.has(type) && (portsView?.faceplate.blocks.length ?? 0) > 0
  const selectedPort = ports.find((p) => p.if_index === selected) ?? null

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <Link to={`/network/sites/${device.site_id}`} className="text-sm text-slate-400 hover:text-slate-300">
            ← {device.site_name}
          </Link>
          <div className="mt-2 flex flex-wrap items-center gap-3">
            <h1 className="break-words text-4xl font-light text-white">{device.name}</h1>
            <DeviceStatusBadge status={device.status} />
          </div>
          <p className="mt-1 font-mono text-sm text-slate-400">
            {device.host}
            {device.port !== 161 && `:${device.port}`}
          </p>
          {device.status_detail && <p className="mt-1 text-sm text-amber-400">{device.status_detail}</p>}
        </div>
        {canEdit && (
          <div className="flex flex-wrap gap-2">
            <button className="btn-secondary flex items-center gap-2" disabled={busy || !device.enabled} onClick={() => void act(() => refresh(device.id))}>
              <RefreshCw className="h-4 w-4" /> Refresh now
            </button>
            <button className="btn-secondary flex items-center gap-2" onClick={() => setEditingDetails(true)}>
              <SlidersHorizontal className="h-4 w-4" /> Edit details
            </button>
            <button className="btn-secondary flex items-center gap-2" onClick={() => setEditing(true)}>
              <Pencil className="h-4 w-4" /> Edit
            </button>
            <button className="btn-secondary flex items-center gap-2" disabled={busy} onClick={() => void act(() => update(device.id, toInput(device, !device.enabled)))}>
              {device.enabled ? <><Pause className="h-4 w-4" /> Pause</> : <><Play className="h-4 w-4" /> Resume</>}
            </button>
            {confirmDelete ? (
              <>
                <span className="self-center text-xs text-slate-400">Its incident history and graphs are deleted too.</span>
                <button className="btn-secondary" onClick={() => setConfirmDelete(false)}>Cancel</button>
                <button className="btn bg-red-600 text-white hover:bg-red-700" disabled={busy}
                  onClick={() => void act(async () => { await remove(device.id); navigate(`/network/sites/${device.site_id}`) })}>
                  Delete device
                </button>
              </>
            ) : (
              <button className="btn-secondary flex items-center gap-2 text-red-400" onClick={() => setConfirmDelete(true)}>
                <Trash2 className="h-4 w-4" /> Delete
              </button>
            )}
          </div>
        )}
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      <dl className="card grid gap-x-6 gap-y-3 p-5 sm:grid-cols-2 lg:grid-cols-3">
        {[
          ['Vendor', device.effective_vendor || device.vendor || '—'],
          ['Model', device.effective_model || device.model || '—'],
          ['Type', DEVICE_TYPE_LABEL[type] ?? 'Other'],
          ['Serial', device.serial || '—'],
          ['Location', device.effective_location || device.sys_location || '—'],
          ['Contact', device.sys_contact || '—'],
          ['Up since', upSince(device)],
          ['Last seen', device.last_seen_at ? new Date(device.last_seen_at).toLocaleString() : 'never'],
          ['Credential profile', device.credential_name || '—'],
          ['30-day availability', device.availability_30d == null ? '—' : `${device.availability_30d.toFixed(2)}%`],
          ['Stats poll', device.last_stats_duration_ms == null ? 'not yet' : `${device.last_stats_duration_ms} ms`],
        ].map(([k, v]) => (
          <div key={k}>
            <dt className="text-xs uppercase tracking-widest text-slate-500">{k}</dt>
            <dd className="mt-0.5 break-words text-sm text-slate-200">{v}</dd>
          </div>
        ))}
        {device.sys_descr && (
          <div className="sm:col-span-2 lg:col-span-3">
            <dt className="text-xs uppercase tracking-widest text-slate-500">Description</dt>
            <dd className="mt-0.5 break-words text-sm text-slate-400">{device.sys_descr}</dd>
          </div>
        )}
      </dl>

      {showFaceplate && portsView && (
        <section className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="text-lg font-light text-white">Front panel</h2>
            <FaceplateLegend />
          </div>
          <Faceplate
            layout={portsView.faceplate}
            ports={ports}
            title={device.name}
            subtitle={device.effective_model || device.host}
            selected={selected}
            onSelect={(i) => setSelected((s) => (s === i ? null : i))}
          />
          {selectedPort && <PortQuickPanel deviceId={device.id} port={selectedPort} />}
        </section>
      )}

      <TrafficChart title="Traffic" query={{ deviceIds: [device.id], sum: true, physicalOnly: true }} lines={TRAFFIC_LINES} unit="bps" />

      <PortTable deviceId={device.id} ports={ports} canEdit={canEdit} onChanged={() => void refetchPorts()} />

      <section className="space-y-3">
        <h2 className="text-lg font-light text-white">Recent port events</h2>
        <PortEventList events={events} />
      </section>

      <section className="space-y-3">
        <h2 className="text-lg font-light text-white">Incidents</h2>
        {incidents.length === 0 ? (
          <p className="text-sm text-slate-500">No incidents recorded for this device.</p>
        ) : (
          <ul className="card divide-y divide-white/10">
            {incidents.map((inc) => (
              <li key={inc.id}>
                <Link to={`/incidents/${inc.id}`} className="flex flex-wrap items-center justify-between gap-2 p-3 text-sm hover:bg-white/5">
                  <span className={inc.status === 'ongoing' ? 'text-red-400' : 'text-slate-300'}>
                    {inc.status === 'ongoing' ? 'Ongoing' : 'Resolved'} · {new Date(inc.start_time).toLocaleString()} ·{' '}
                    {inc.port_if_index != null ? `${inc.subject_name}: ${CONDITION_LABEL[inc.condition ?? ''] ?? 'problem'}` : 'Device unreachable'}
                  </span>
                  <span className="text-slate-400">{formatDuration(inc.duration_seconds)}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </section>

      {editing && (
        <DeviceFormModal initial={device} onClose={() => setEditing(false)} onSaved={() => { setEditing(false); void refetch() }} />
      )}
      {editingDetails && (
        <EditDetailsModal device={device} onClose={() => setEditingDetails(false)} onSaved={() => { setEditingDetails(false); void refetch(); void refetchPorts() }} />
      )}
    </div>
  )
}
```

- [ ] **Step 5: Check and commit**

Run the frontend check from Global Constraints.
Expected: exit 0. `TrafficChart` memoises its query on the JSON `key` on purpose, because callers pass a fresh object every render. Do not add `query` to any dependency list.

```bash
git add frontend/src/components/network frontend/src/pages/network/DeviceDetail.tsx
git commit -m "feat(network): virtual faceplate, traffic chart and port table on the device page

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: The port page

**Files:**
- Create: `frontend/src/pages/network/PortDetail.tsx`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Consumes: `usePort`, `usePortActions`, `usePortEvents`, `TrafficChart`, `PortEventList`, and the Task 11 utils.
- Produces: the route `/network/devices/:id/ports/:ifIndex`.

- [ ] **Step 1: The page**

Create `frontend/src/pages/network/PortDetail.tsx`:

```tsx
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Star } from 'lucide-react'
import { formatSpeed, useDevice } from '@/hooks/useDevices'
import { useSite } from '@/hooks/useSites'
import { usePort, usePortActions, usePortEvents, type PortDetail as Port, type PortPatch } from '@/hooks/usePorts'
import TrafficChart from '@/components/network/TrafficChart'
import PortEventList from '@/components/network/PortEventList'
import { busiestUtil, CONDITION_LABEL, formatBps, formatPct, PORT_STATE, portState, portTitle, WARNING_CONDITIONS } from '@/utils/network'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-32 rounded-md border border-white/10 bg-slate-900/60 px-3 py-1.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

function ThresholdsForm({ port, deviceId, onSaved }: { port: Port; deviceId: string; onSaved: () => void }) {
  const { updatePort, busy } = usePortActions()
  const [util, setUtil] = useState(port.util_threshold_pct?.toString() ?? '')
  const [errs, setErrs] = useState(port.error_threshold_per_min?.toString() ?? '')
  const [grace, setGrace] = useState(port.down_grace_seconds?.toString() ?? '')
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const num = (s: string) => (s.trim() === '' ? null : Number(s))
  const d = port.defaults

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaved(false)
    setError(null)
    try {
      await updatePort(deviceId, port.if_index, {
        util_threshold_pct: num(util),
        error_threshold_per_min: num(errs),
        down_grace_seconds: num(grace),
      })
      setSaved(true)
      onSaved()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the thresholds')
    }
  }

  return (
    <form onSubmit={(e) => void save(e)} className="flex flex-wrap items-end gap-4">
      <label className="space-y-1">
        <span className="block text-xs text-slate-400">Nearly full at (%)</span>
        <input className={inputCls} type="number" min={10} max={100} value={util} onChange={(e) => setUtil(e.target.value)} placeholder={`Default ${d.util_threshold_pct}`} />
      </label>
      <label className="space-y-1">
        <span className="block text-xs text-slate-400">Errors per minute</span>
        <input className={inputCls} type="number" min={1} value={errs} onChange={(e) => setErrs(e.target.value)} placeholder={`Default ${d.error_threshold_per_min}`} />
      </label>
      <label className="space-y-1">
        <span className="block text-xs text-slate-400">Alert when down for (s)</span>
        <input className={inputCls} type="number" min={0} max={86400} value={grace} onChange={(e) => setGrace(e.target.value)} placeholder={`Default ${d.down_grace_seconds}`} />
      </label>
      <button className="btn-secondary" disabled={busy}>
        Save thresholds
      </button>
      {saved && <span className="text-sm text-emerald-400">Saved</span>}
      {error && <span className="text-sm text-red-400">{error}</span>}
      <p className="basis-full text-xs text-slate-500">Leave a field empty to use the default from Network settings.</p>
    </form>
  )
}

export default function PortDetail() {
  const { id, ifIndex } = useParams<{ id: string; ifIndex: string }>()
  const { device } = useDevice(id)
  const { site } = useSite(device?.site_id)
  const { data: port, loading, notFound, refetch } = usePort(id, ifIndex)
  const { events } = usePortEvents({ deviceId: id, ifIndex: ifIndex ? Number(ifIndex) : undefined }, 30)
  const { updatePort, busy } = usePortActions()
  const [error, setError] = useState<string | null>(null)

  if (loading && !port) return <p className="text-sm text-slate-400">Loading…</p>
  if (notFound || !port || !id) {
    return (
      <div className="card p-8 text-center">
        <p className="text-slate-300">Port not found.</p>
        <Link to={id ? `/network/devices/${id}` : '/network/devices'} className="mt-2 inline-block text-sm text-primary-400 hover:underline">
          Back to the device
        </Link>
      </div>
    )
  }

  const canEdit = site?.access === 'admin' || site?.access === 'editable'
  const st = portState(port)
  const warns = (port.conditions ?? []).filter((c) => WARNING_CONDITIONS.includes(c))
  const scope = { deviceIds: [id], instances: [String(port.if_index)] }
  const change = async (patch: PortPatch) => {
    setError(null)
    try {
      await updatePort(id, port.if_index, patch)
      await refetch()
    } catch (err) {
      setError((err as ApiError).message || 'Could not update the port')
    }
  }

  return (
    <div className="space-y-8">
      <div>
        <Link to={`/network/devices/${id}`} className="text-sm text-slate-400 hover:text-slate-300">
          ← {device?.name ?? 'Device'}
        </Link>
        <div className="mt-2 flex flex-wrap items-center gap-3">
          <h1 className="break-words text-4xl font-light text-white">{portTitle(port)}</h1>
          <span className={`rounded-full border px-2.5 py-0.5 text-xs ${PORT_STATE[st].badge}`}>{PORT_STATE[st].label}</span>
          {port.important && <span className="rounded-full border border-sky-500/30 bg-sky-500/10 px-2.5 py-0.5 text-xs text-sky-300">Important</span>}
        </div>
        {warns.length > 0 && <p className="mt-2 text-sm text-yellow-400">{warns.map((c) => CONDITION_LABEL[c]).join(' · ')}</p>}
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      <dl className="card grid gap-x-6 gap-y-3 p-5 sm:grid-cols-2 lg:grid-cols-4">
        {[
          ['Interface', port.name || '—'],
          ['Description', port.descr || '—'],
          ['Link', port.oper_status === 'up' ? formatSpeed(port.speed_bps) : 'Down'],
          ['Usual speed', port.usual_speed_bps ? formatSpeed(port.usual_speed_bps) : 'Learning (needs a day of history)'],
          ['In', formatBps(port.in_bps)],
          ['Out', formatBps(port.out_bps)],
          ['Busy', formatPct(busiestUtil(port))],
          ['Errors/min', port.errors_per_min == null ? '—' : String(+port.errors_per_min.toFixed(1))],
          ['Link last changed', port.oper_changed_at ? new Date(port.oper_changed_at).toLocaleString() : '—'],
          ['MAC', port.mac || '—'],
          ['Statistics', port.collected ? 'Collected every poll' : 'Not collected'],
        ].map(([k, v]) => (
          <div key={k}>
            <dt className="text-xs uppercase tracking-widest text-slate-500">{k}</dt>
            <dd className="mt-0.5 break-words text-sm text-slate-200">{v}</dd>
          </div>
        ))}
      </dl>

      {canEdit && (
        <section className="card space-y-4 p-5">
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
            <label className="flex items-center gap-2 text-sm text-slate-200">
              <input type="checkbox" checked={port.important} disabled={busy} onChange={() => void change({ important: !port.important })} />
              <Star className="h-4 w-4 text-amber-400" aria-hidden /> Important: alert on link down, errors, flapping, a slower link or nearly full
            </label>
            <label className="flex items-center gap-2 text-sm text-slate-300">
              <input type="checkbox" checked={port.collected} disabled={busy} onChange={() => void change({ collect: !port.collected })} /> Collect statistics
            </label>
            {port.collect !== null && (
              <button type="button" className="text-xs text-primary-400 hover:underline" onClick={() => void change({ collect: null })}>
                Back to automatic
              </button>
            )}
          </div>
          <ThresholdsForm
            key={`${port.util_threshold_pct}-${port.error_threshold_per_min}-${port.down_grace_seconds}`}
            port={port}
            deviceId={id}
            onSaved={() => void refetch()}
          />
        </section>
      )}

      <TrafficChart
        title="Traffic"
        query={scope}
        lines={[
          { metric: 'if_in_bps', label: 'In', colour: '#22d3ee' },
          { metric: 'if_out_bps', label: 'Out', colour: '#a78bfa' },
        ]}
        unit="bps"
      />
      <TrafficChart
        title="How busy"
        query={scope}
        lines={[
          { metric: 'if_in_util_pct', label: 'In', colour: '#22d3ee' },
          { metric: 'if_out_util_pct', label: 'Out', colour: '#a78bfa' },
        ]}
        unit="pct"
        threshold={port.util_threshold_pct ?? port.defaults.util_threshold_pct}
      />
      <TrafficChart
        title="Errors and discards"
        query={scope}
        lines={[
          { metric: 'if_in_errors_pm', label: 'Errors in', colour: '#f87171' },
          { metric: 'if_out_errors_pm', label: 'Errors out', colour: '#fb923c' },
          { metric: 'if_in_discards_pm', label: 'Discards in', colour: '#facc15' },
          { metric: 'if_out_discards_pm', label: 'Discards out', colour: '#94a3b8' },
        ]}
        unit="per_min"
      />

      {port.open_incidents.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-lg font-light text-white">Open incidents</h2>
          <ul className="card divide-y divide-white/10">
            {port.open_incidents.map((i) => (
              <li key={i.id}>
                <Link to={`/incidents/${i.id}`} className="block p-3 text-sm text-red-400 hover:bg-white/5">
                  {CONDITION_LABEL[i.condition ?? ''] ?? 'Incident'} since {new Date(i.start_time).toLocaleString()}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="space-y-3">
        <h2 className="text-lg font-light text-white">Events</h2>
        <PortEventList events={events} emptyText="Nothing has happened on this port yet." />
      </section>
    </div>
  )
}
```

Incident start times come from a `TIMESTAMP` column without a time zone, written in UTC (see memory `timestamp-timezone-gotcha`). If the displayed start is off by the local offset, append `'Z'` before parsing, the way `IncidentDetail.tsx` does. Check how that page parses `start_time` and do the same.

- [ ] **Step 2: Route**

In `frontend/src/App.tsx`, add beside the other network lazy imports:

```tsx
const PortDetail = lazy(() => import('@/pages/network/PortDetail'))
```

and after the `/network/devices/:id` route:

```tsx
              <Route path="/network/devices/:id/ports/:ifIndex" element={<PortDetail />} />
```

- [ ] **Step 3: Check and commit**

Run the frontend check from Global Constraints.
Expected: exit 0.

```bash
git add frontend/src/pages/network/PortDetail.tsx frontend/src/App.tsx
git commit -m "feat(network): port page with charts, thresholds and events

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: Site page, network settings, device list and incident links

**Files:**
- Modify: `frontend/src/pages/network/SiteDetail.tsx`, `frontend/src/components/network/DeviceTable.tsx`, `frontend/src/pages/IncidentDetail.tsx`, `frontend/src/components/Layout.tsx`, `frontend/src/App.tsx`
- Create: `frontend/src/pages/network/NetworkSettings.tsx`

**Interfaces:**
- Consumes: `useSitePortSummary`, `usePortEvents`, `useNetworkSettings`, `TrafficChart`, `PortEventList`, and the Task 11 utils.
- Produces: the route `/network/settings`.

- [ ] **Step 1: Site page additions**

In `frontend/src/pages/network/SiteDetail.tsx`:

1. Add imports:

```tsx
import { useSitePortSummary, usePortEvents, type PortRef } from '@/hooks/usePorts'
import TrafficChart from '@/components/network/TrafficChart'
import PortEventList from '@/components/network/PortEventList'
import { CONDITION_LABEL, formatPct, portTitle } from '@/utils/network'
```

2. Next to the other hooks at the top of `SiteDetail()`, before any early return:

```tsx
  const { data: summary } = useSitePortSummary(id)
  const { events } = usePortEvents({ siteId: id }, 15)
```

3. Add this component above `export default function SiteDetail()`:

```tsx
function PortRefList({ title, refs, empty, detail }: { title: string; refs: PortRef[]; empty: string; detail: (r: PortRef) => string }) {
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-light text-white">{title}</h2>
      {refs.length === 0 ? (
        <p className="text-sm text-slate-500">{empty}</p>
      ) : (
        <ul className="card divide-y divide-white/10">
          {refs.map((r) => (
            <li key={`${r.device_id}-${r.if_index}`}>
              <Link to={`/network/devices/${r.device_id}/ports/${r.if_index}`} className="flex items-center justify-between gap-3 p-3 text-sm hover:bg-white/5">
                <span className="min-w-0 truncate text-slate-200">
                  {r.device_name} · {portTitle(r)}
                </span>
                <span className="shrink-0 tabular-nums text-slate-400">{detail(r)}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
```

4. After the Devices `</section>` and before `{isAdmin && <SiteSharingPanel ... />}`:

```tsx
      {devices.length > 0 && (
        <>
          <TrafficChart
            title="Site traffic"
            query={{ siteId: site.id, sum: true, physicalOnly: true }}
            lines={[
              { metric: 'if_in_bps', label: 'In', colour: '#22d3ee' },
              { metric: 'if_out_bps', label: 'Out', colour: '#a78bfa' },
            ]}
            unit="bps"
          />
          <div className="grid gap-6 lg:grid-cols-2">
            <PortRefList title="Busiest ports" refs={summary?.busiest ?? []} empty="No traffic figures yet." detail={(r) => formatPct(r.util_pct)} />
            <PortRefList
              title="Ports with problems"
              refs={summary?.problems ?? []}
              empty="Nothing wrong right now."
              detail={(r) => (r.conditions.length ? r.conditions.map((c) => CONDITION_LABEL[c]).join(', ') : 'Link down')}
            />
          </div>
          <section className="space-y-3">
            <h2 className="text-lg font-light text-white">Recent port events</h2>
            <PortEventList events={events} showDevice />
          </section>
        </>
      )}
```

- [ ] **Step 2: Network settings page**

Create `frontend/src/pages/network/NetworkSettings.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { useAuthContext } from '@/context/AuthContext'
import { useNetworkSettings, type NetworkSettings as Settings } from '@/hooks/useNetworkSettings'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-32 rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-primary-500'

const FIELDS: { key: keyof Settings; label: string; help: string; min: number; max: number; unit: string }[] = [
  { key: 'metrics_raw_retention_days', label: 'Detailed history', help: 'How long 1-minute figures are kept. 5-minute and hourly figures are kept forever.', min: 7, max: 3650, unit: 'days' },
  { key: 'port_util_threshold_pct', label: 'Nearly full at', help: 'A port is flagged when either direction averages this over 5 minutes; it clears 10 points lower.', min: 10, max: 100, unit: '%' },
  { key: 'port_error_threshold_per_min', label: 'Errors rising at', help: 'Errors plus discards per minute, sustained for 5 minutes.', min: 1, max: 1000000, unit: 'per minute' },
  { key: 'port_down_grace_seconds', label: 'Alert when an important port is down for', help: 'Long enough that a device rebooting does not alert.', min: 0, max: 86400, unit: 'seconds' },
]

export default function NetworkSettings() {
  const { currentUser } = useAuthContext()
  const { settings, loading, error, saving, save } = useNetworkSettings()
  const [form, setForm] = useState<Record<string, string>>({})
  const [saved, setSaved] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  useEffect(() => {
    if (settings) setForm(Object.fromEntries(FIELDS.map((f) => [f.key, String(settings[f.key])])))
  }, [settings])

  if (!currentUser?.is_admin) {
    return <p className="text-sm text-slate-400">Only administrators change network settings.</p>
  }
  if (loading) return <p className="text-sm text-slate-400">Loading…</p>
  if (error || !settings) return <p className="text-sm text-red-400">{error ?? 'Could not load network settings'}</p>

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaved(false)
    setSaveError(null)
    try {
      await save(Object.fromEntries(FIELDS.map((f) => [f.key, Number(form[f.key])])) as Partial<Settings>)
      setSaved(true)
    } catch (err) {
      setSaveError((err as ApiError).message || 'Could not save')
    }
  }

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-4xl font-light text-white">Network settings</h1>
        <p className="mt-2 text-sm text-slate-400">Defaults for every port. Each port can override its thresholds on its own page.</p>
      </div>
      <form className="card max-w-2xl space-y-5 p-6" onSubmit={(e) => void submit(e)}>
        {FIELDS.map((f) => (
          <label key={f.key} className="block space-y-1">
            <span className="text-sm text-slate-200">{f.label}</span>
            <span className="flex items-center gap-2">
              <input
                className={inputCls}
                type="number"
                min={f.min}
                max={f.max}
                required
                value={form[f.key] ?? ''}
                onChange={(e) => setForm((s) => ({ ...s, [f.key]: e.target.value }))}
              />
              <span className="text-sm text-slate-400">{f.unit}</span>
            </span>
            <span className="block text-xs text-slate-500">{f.help}</span>
          </label>
        ))}
        {saveError && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{saveError}</div>}
        <div className="flex items-center gap-3">
          <button type="submit" className="btn-primary" disabled={saving}>
            Save
          </button>
          {saved && <span className="text-sm text-emerald-400">Saved</span>}
        </div>
      </form>
    </div>
  )
}
```

The admin check runs after the hooks, as on the Credentials page. A member's settings request answers 403, and the page shows the admin-only message.

- [ ] **Step 3: Nav, route, device list, incident link**

1. In `frontend/src/components/Layout.tsx`, add to `networkNav` after Credentials:

```tsx
  { to: '/network/settings', label: 'Settings', adminOnly: true },
```

2. In `frontend/src/App.tsx`, add the lazy import `const NetworkSettings = lazy(() => import('@/pages/network/NetworkSettings'))` and the route `<Route path="/network/settings" element={<NetworkSettings />} />` after `/network/credentials`.

3. In `frontend/src/components/network/DeviceTable.tsx`, change the vendor/model cell to show effective values:

```tsx
                <td className="px-4 py-3 text-slate-400">{[d.effective_vendor || d.vendor, d.effective_model || d.model].filter(Boolean).join(' · ') || '—'}</td>
```

4. In `frontend/src/pages/IncidentDetail.tsx`, make a port incident's subject link go to the port page:

```tsx
  const subjectLink = inc
    ? isDevice
      ? inc.port_if_index != null
        ? `/network/devices/${inc.device_id}/ports/${inc.port_if_index}`
        : `/network/devices/${inc.device_id}`
      : `/monitors/${inc.monitor_id}`
    : '/incidents'
```

and change `backLabel`'s device text to `inc?.port_if_index != null ? 'Back to Port' : 'Back to Device'`.

- [ ] **Step 4: Check and commit**

Run the frontend check from Global Constraints.
Expected: exit 0.

```bash
git add frontend/src
git commit -m "feat(network): site traffic and port summaries, network settings, port links

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 15: Sandbox verification with the user's gear

Run by the controller with the user (it needs their switch, their cables and a browser); not dispatched to an implementer.

**Files:** none (results go to the ledger; any bug found becomes a fix task with its own test).

- [ ] **Step 1: Full test run**

Run: `cd backend && go vet ./... && go test ./... 2>&1 | grep -v "^ok"; make test-db 2>&1 | grep -E "^(FAIL|---)"; SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 go test ./internal/snmp/ -run Sim 2>&1 | tail -1`
Expected: no failures; the simulator line is `ok`.

Then run the frontend check from Global Constraints.
Expected: exit 0.

- [ ] **Step 2: Deploy the branch to the sandbox**

```bash
git -C /srv/docker/sentinel-dev fetch /home/sysadmin/sentinel-phase2 feature/network-phase2
git -C /srv/docker/sentinel-dev checkout -B feature/network-phase2 FETCH_HEAD
cd /srv/docker/sentinel-dev && docker compose up -d --build
docker compose logs backend --since 3m 2>&1 | grep -E "migration|error|panic|snmp" | tail -20
```

Expected:
- `applying migration 048_metrics.sql`, then `049_port_monitoring.sql`;
- `[snmp] poller started`;
- no `panic`;
- no `error` lines from the metrics store.

If `sentinel-snmpsim` is not running, start it with `cd deploy/snmpsim && SNMPSIM_VERSION=1.2.2 ./run.sh` from the phase 2 worktree. That script also joins it to the sandbox network.

- [ ] **Step 3: Data is flowing, and poll timing (plan decision 6)**

After 3 minutes:

```bash
docker exec sentinel-dev-postgres psql -U sentinel -d sentinel -c "SELECT name, device_type_detected, last_stats_duration_ms FROM devices ORDER BY host"
docker exec sentinel-dev-postgres psql -U sentinel -d sentinel -c "SELECT count(*) AS series FROM metrics.series"
docker exec sentinel-dev-postgres psql -U sentinel -d sentinel -c "SELECT count(*) AS samples, max(time) AS newest FROM metrics.samples"
```

Expected:
- **Device types:** QuantumLink `switch`, Quantum-Gate `router`, the APs `access_point`, Overwatch `nvr`.
- **Timing:** a `last_stats_duration_ms` for every up device.
- **Data:** series in the hundreds, and samples within the last minute.

**Timing decision:** with W = `snmp_poll_workers` (16) and D = the slowest `last_stats_duration_ms`, the pool keeps up while (devices × D) / W stays under half the poll interval (30 000 ms). Record QuantumLink's duration and the verdict in the ledger. If it fails, raise the setting's default in a fix task. It will not fail at six devices; the number is recorded for the multi-site future.

- [ ] **Step 4: Browser checks with the user**

Ask the user to check each item and report back:

1. **Faceplate:**
   - QuantumLink's device page draws four blocks of twelve with odd ports on top, then 49–52 on the right as a 2×2.
   - Colours match reality: cameras and Prox hosts green, unplugged ports grey, disabled ports dark.
2. **Traffic:** within a few minutes the device traffic chart has a line, and busy ports (Uplink To Quantum Gate, UNAS Pro) show a fill.
3. **Important port:**
   - Mark a port important, using one whose device the user can unplug.
   - Unplug it: the port turns red within a minute.
   - After 2 minutes an incident appears on the Incidents page, named "QuantumLink · 0/N (alias)". There is no notification unless a channel is configured, which the user can add to test.
   - Plug it back in: the incident closes, and the event log shows link down/up.
4. **Overrides:**
   - Edit Overwatch's model to the user's choice and press Refresh now.
   - After the next inventory (on refresh) the override still shows, with "SNMP says: UNVR4" under it in Edit details.
5. **Device types:** the UDM-SE (router) shows a faceplate with eth9/eth10 as SFP. The APs and the UNVR show the port table only.
6. **Port page:** charts render, thresholds save, and "Back to automatic" on collect works.
7. **Site page:** site traffic, busiest ports and ports with problems render.
8. **Access:**
   - As the read-only user `sstevens`: there are no star or collect controls and no Edit details button, and PATCH attempts fail.
   - Network settings are not in their sidebar.
   - As admin: Network settings saves, and changing Detailed history to 400 days works.

Record each result in the ledger. Every failure becomes a fix task (test first) before the final review.

- [ ] **Step 5: Hand off to the final review**

When every check passes, the plan's tasks are complete. Next is the whole-branch final review (subagent-driven-development).
