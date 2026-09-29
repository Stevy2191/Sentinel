# Network Monitoring Phase 1 (SNMP Foundation) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Sentinel polls switches and APs over SNMP v1/v2c/v3: credential profiles, devices in sites, subnet scan, identity and interface inventory, reachability polling, and down/recovery as incidents shared with monitors.

**Architecture:** A new `internal/snmp` package wraps gosnmp behind a `Client` interface; all interpretation (system group, interfaces, entity, vendor) is pure. A `DevicePoller` service schedules reachability polls on a bounded worker pool, runs every outcome through a pure state machine, and applies its actions (open/close device incident, notify). Incidents and notifications gain a `device_id` subject beside `monitor_id`, enforced by CHECK constraints; every monitor uptime query filters `monitor_id = ?`, so device incidents cannot reach monitor numbers. A real-database test harness (`internal/testdb`, `make test-db`) pins the SQL; an snmpsim container exercises the real gosnmp client.

**Tech Stack:** Go 1.26, Gin, GORM (pgx), PostgreSQL 16 + TimescaleDB 2.30.1, `github.com/gosnmp/gosnmp`; React + TypeScript + Vite + Tailwind; Docker; snmpsim (Python) for the simulator.

**Spec:** `docs/superpowers/specs/2026-09-29-network-phase1-snmp-foundation-design.md`

## Global Constraints

- Branch: `feature/network-phase1` (from `dev`). The `/home/sysadmin/sentinel` working copy runs the **live** stack: switch it back to `main` at the end of every working session. Never deploy live from this plan.
- Migrations: `046_snmp_devices.sql`, `047_device_incidents.sql` (highest existing: `045_sites.sql`). Never edit an applied migration.
- Every timestamp column is `TIMESTAMPTZ`. Everything new lives in `public` and is a table (phase 0 backup rule: backups dump `--table='public.*'`).
- API routes under `/api/v1`. A device, credential or incident the caller cannot see answers **404**.
- SNMP versions exactly `1`, `2c`, `3`. Auth protocols `none, MD5, SHA, SHA224, SHA256, SHA384, SHA512`; privacy `none, DES, AES, AES192, AES256`.
- Polling defaults: interval 60 s, timeout 3000 ms, retries 1, down after **3** consecutive failures, inventory every 15 min. Worker pool: setting `snmp_poll_workers`, default 16.
- Scan: at most 1024 addresses (/22), per-host timeout 1 s, 0 retries, concurrency 64, one scan per site, jobs expire 1 h after finishing.
- `POST /devices/test`: 10 per minute per user.
- Secrets (`community`, `auth_password`, `priv_password`) are encrypted with `cryptutil.Encrypt`, never returned by an API, never written to the audit log. Model fields carry `json:"-"`.
- Device `notify_channels` follows the existing monitor/agent convention: `null` = every enabled channel, `[]` = none.
- Frontend follows the design system (slate/Tailwind, `card`, `btn-primary`, `btn-secondary`, `rd-nav`, `rd-select`); Tailwind classes are literal strings.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Frontend checks run in Docker (no Node on the host): `docker run --rm -v /home/sysadmin/sentinel/frontend:/app -w /app node:20-alpine sh -c "npx tsc --noEmit && npx eslint src --quiet"`.
- Plain `go test ./...` must stay database- and network-free. Integration tests skip unless `SENTINEL_TEST_DATABASE_URL` / `SENTINEL_TEST_SNMPSIM` are set.

## Spec corrections made by this plan

Both found while reading the code during planning; Task 2 updates the spec text.

1. **Device uniqueness includes the port**: `(site_id, lower(host), port)`, not `(site_id, lower(host))`. Two devices behind one NAT address on different ports are normal at a site, and the load test needs 30 simulated devices on one host.
2. **`notify_channels` keeps the existing convention** (`null` = all enabled channels, `[]` = none) instead of the spec's "none selected means none", because the shared `NotificationChannelPicker` already means `null` = all everywhere else.

## Review Focus

1. **A device incident must never change a monitor's numbers.** Uptime, downtime, counts and active-incident lookups filter `monitor_id = ?`; a regression that drops that filter would silently mix switch outages into monitor SLAs. Test: `TestDBDeviceIncidentsInvisibleToMonitorQueries` (Task 3).
2. **A member must see exactly their own incidents across both subjects**: monitor incidents by monitor sharing, device incidents by site sharing, and nothing from a site that is not shared, including through `?device_id=` of a foreign device. Test: `TestDBIncidentListAccess` (Task 3).
3. **Secrets never leave the server**: not in credential API responses, device responses, scan results, the audit log, or error messages. Test: `TestCredentialViewHasNoSecrets` (Task 6) and `TestDBCredentialSecretsEncrypted` (Task 6).
4. **A flapping device must not flood incidents**: one failure or two failures then success opens nothing; exactly the third consecutive failure opens one incident; further failures open no more. Test: `TestNextDeviceState` table (Task 5).
5. **Devices with no channels chosen**: `[]` sends nothing but still opens the incident; `null` sends to every enabled channel. Test: `TestPollerNotificationChannels` (Task 5).

---

## File Structure

Backend (new unless marked):
- `backend/internal/database/migrate.go` + `migrate_test.go`: `RunMigrations`, moved from `main.go`.
- `backend/internal/testdb/testdb.go`: per-test fresh database from `SENTINEL_TEST_DATABASE_URL`.
- `backend/scripts/test-db.sh`, `backend/Makefile` (modify): `make test-db`.
- `backend/migrations/046_snmp_devices.sql`, `047_device_incidents.sql`.
- `backend/internal/models/snmp.go` + `snmp_test.go`: `SNMPCredential`, `CredentialInput`, `NormalizeCredentialInput`, `Device`, `DeviceInput`, `NormalizeDeviceInput`, `DeviceInterface`.
- `backend/internal/models/monitor.go` (modify): `Incident.MonitorID` pointer, `Incident.DeviceID`, `Notification.DeviceID`.
- `backend/internal/snmp/client.go` (+`_test`): `Credential`, `Target`, `PDU`, `Client`, `GoSNMPClient`, `buildGoSNMP`.
- `backend/internal/snmp/parse.go` (+`_test`): OIDs, `ParseSystem`, `ParseInterfaces`, `ParseEntity`, `VendorFor`, `Identify`, `ReadInventory`.
- `backend/internal/snmp/simulator_test.go`: gosnmp against snmpsim (env-gated).
- `backend/internal/services/incident_service.go` (modify): device incidents, subject-aware list/detail.
- `backend/internal/services/incident_device_db_test.go`: DB tests for incidents.
- `backend/internal/services/device_state.go` (+`_test`): `NextDeviceState`.
- `backend/internal/services/device_poller.go` (+`_test`): scheduler, workers, inventory.
- `backend/internal/services/snmp_credential_service.go` (+`_db_test`).
- `backend/internal/services/device_service.go` (+`_db_test`).
- `backend/internal/services/scan_manager.go` (+`_test`).
- `backend/internal/services/site_service.go` (modify): `siteContentTables`.
- `backend/internal/notifications/plugins.go`, `webhook.go` (modify): `DeviceID`.
- `backend/internal/api/incident_handler.go`, `incident_comment_handler.go` (modify): subject access.
- `backend/internal/api/snmp_credential_handler.go`, `device_handler.go`, `scan_handler.go` (+ tests).
- `backend/internal/models/setting.go`, `audit.go` (modify).
- `backend/cmd/sentinel/main.go` (modify): wiring.
- `deploy/snmpsim/`: simulator Dockerfile, data, run script.

Frontend:
- `frontend/src/hooks/useDevices.ts`, `useSnmpCredentials.ts`, `useSiteScan.ts`.
- `frontend/src/pages/network/Credentials.tsx`, `Devices.tsx`, `DeviceDetail.tsx`.
- `frontend/src/components/network/DeviceFormModal.tsx`, `CredentialFormModal.tsx`, `DeviceTable.tsx`, `ScanModal.tsx`, `DeviceStatusBadge.tsx`.
- `frontend/src/pages/network/SiteDetail.tsx`, `components/Layout.tsx`, `App.tsx`, `pages/Incidents.tsx`, `pages/IncidentDetail.tsx`, `hooks/useIncidents.ts`, `hooks/useSites.ts`, `pages/Overview.tsx` (modify).

---

### Task 1: Database test harness

**Files:**
- Create: `backend/internal/database/migrate.go`, `backend/internal/database/migrate_test.go`
- Create: `backend/internal/testdb/testdb.go`
- Create: `backend/scripts/test-db.sh`
- Modify: `backend/Makefile`, `backend/cmd/sentinel/main.go` (remove `runMigrations`, call `database.RunMigrations`)

**Interfaces:**
- Produces: `database.RunMigrations(db *gorm.DB, dir string) error`; `testdb.Open(t *testing.T) *gorm.DB` (fresh migrated database per call, dropped at test end; `t.Skip` when `SENTINEL_TEST_DATABASE_URL` is unset); `testdb.MigrationsDir() string`.

- [ ] **Step 1: Move `runMigrations` without changing behaviour**

Create `backend/internal/database/migrate.go` with the body of `runMigrations` from `backend/cmd/sentinel/main.go` (the function starting `// runMigrations applies any *.sql files`), renamed and exported:

```go
package database

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"gorm.io/gorm"
)

// RunMigrations applies any *.sql files in dir that have not yet been recorded
// in the schema_migrations table, in filename order.
func RunMigrations(db *gorm.DB, dir string) error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename   TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`).Error; err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("listing migrations in %q: %w", dir, err)
	}
	sort.Strings(files)

	for _, path := range files {
		name := filepath.Base(path)

		var applied int64
		if err := db.Raw("SELECT count(*) FROM schema_migrations WHERE filename = ?", name).Scan(&applied).Error; err != nil {
			return fmt.Errorf("checking migration %q: %w", name, err)
		}
		if applied > 0 {
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading migration %q: %w", name, err)
		}
		log.Printf("applying migration %s", name)
		if err := db.Exec(string(content)).Error; err != nil {
			return fmt.Errorf("applying migration %q: %w", name, err)
		}
		if err := db.Exec("INSERT INTO schema_migrations (filename) VALUES (?)", name).Error; err != nil {
			return fmt.Errorf("recording migration %q: %w", name, err)
		}
	}
	return nil
}
```

Compare with the original before deleting it: `git show HEAD:backend/cmd/sentinel/main.go | awk '/^func runMigrations/,/^}/'`. The only differences allowed are the name and the `log.Printf` prefix (keep whatever the original printed; if it logs via a package-level logger, use `log.Printf` with the same text). Then delete `runMigrations` from `main.go` and replace its call with `database.RunMigrations(db, cfg.MigrationsDir)`. Remove any imports `main.go` no longer uses (`go build` names them).

- [ ] **Step 2: Build**

Run: `cd /home/sysadmin/sentinel/backend && go build ./... && go vet ./...`
Expected: no output.

- [ ] **Step 3: Write the test database helper**

`backend/internal/testdb/testdb.go`:

```go
// Package testdb gives integration tests a real, migrated PostgreSQL database.
//
// Tests that need one call Open(t). It skips the test unless
// SENTINEL_TEST_DATABASE_URL is set, so plain `go test ./...` stays
// database-free; `make test-db` starts a throwaway TimescaleDB and sets it.
//
// Every call creates a brand-new database and drops it when the test ends, so
// tests never see each other's rows and can run in parallel.
package testdb

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Stevy2191/Sentinel/backend/internal/database"
)

// EnvURL names the variable holding an admin connection string, e.g.
// postgres://sentinel:test@127.0.0.1:55432/sentinel?sslmode=disable
const EnvURL = "SENTINEL_TEST_DATABASE_URL"

// MigrationsDir is backend/migrations, located from this file so tests work
// from any package directory.
func MigrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

// Open returns a fresh database with every migration applied.
func Open(t *testing.T) *gorm.DB {
	t.Helper()
	raw := os.Getenv(EnvURL)
	if raw == "" {
		t.Skipf("%s not set; run `make test-db` for database tests", EnvURL)
	}

	admin, err := gorm.Open(postgres.Open(raw), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("connecting to %s: %v", EnvURL, err)
	}
	name := "t_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("creating test database: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %s: %v", EnvURL, err)
	}
	u.Path = "/" + name
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	if err := database.RunMigrations(db, MigrationsDir()); err != nil {
		t.Fatalf("migrating test database: %v", err)
	}
	return db
}

// Exec runs a statement and fails the test on error. For fixtures.
func Exec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// NewUser inserts a user and returns its id.
func NewUser(t *testing.T, db *gorm.DB, admin bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	name := "u" + strings.ReplaceAll(id.String()[:8], "-", "")
	role := "user"
	if admin {
		role = "admin"
	}
	Exec(t, db, `INSERT INTO users (id, username, email, password_hash, is_admin, role)
		VALUES (?, ?, ?, 'x', ?, ?)`, id, name, name+"@example.test", admin, role)
	return id
}

// Must fails the test on a non-nil error.
func Must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
```

`users` requires `username` (≤ 32 characters, case-insensitively unique) and `password_hash`; `role` exists since migration 011 and is kept in step with `is_admin` (memory: both must agree).

- [ ] **Step 4: Write the harness test**

`backend/internal/database/migrate_test.go`:

```go
package database_test

import (
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/database"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// Migrations apply to an empty database, and running them again is a no-op:
// the runner skips what schema_migrations records.
func TestDBMigrationsApplyAndAreIdempotent(t *testing.T) {
	db := testdb.Open(t) // already migrated once
	if err := database.RunMigrations(db, testdb.MigrationsDir()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var n int64
	testdb.Must(t, db.Raw("SELECT count(*) FROM schema_migrations").Scan(&n).Error)
	if n < 45 {
		t.Errorf("schema_migrations has %d rows, want at least 45", n)
	}
	var ext string
	testdb.Must(t, db.Raw("SELECT extname FROM pg_extension WHERE extname = 'timescaledb'").Scan(&ext).Error)
	if ext != "timescaledb" {
		t.Error("timescaledb extension missing from the test database")
	}
}
```

- [ ] **Step 5: Write the runner script and Make target**

`backend/scripts/test-db.sh`:

```bash
#!/usr/bin/env bash
# Runs the Go tests, including database integration tests, against a
# throwaway TimescaleDB container that is removed afterwards.
set -euo pipefail
cd "$(dirname "$0")/.."

name="sentinel-testdb-$$"
docker run -d --rm --name "$name" \
  -e POSTGRES_USER=sentinel -e POSTGRES_PASSWORD=test -e POSTGRES_DB=sentinel \
  -p 127.0.0.1::5432 \
  timescale/timescaledb:2.30.1-pg16 \
  postgres -c shared_preload_libraries=timescaledb -c timescaledb.telemetry_level=off \
  >/dev/null
trap 'docker stop "$name" >/dev/null 2>&1 || true' EXIT

for _ in $(seq 1 60); do
  if docker exec "$name" pg_isready -U sentinel -d sentinel >/dev/null 2>&1; then break; fi
  sleep 0.5
done
# pg_isready answers during the image's init restart; wait for a real query.
for _ in $(seq 1 60); do
  if docker exec "$name" psql -U sentinel -d sentinel -Atc 'select 1' >/dev/null 2>&1; then break; fi
  sleep 0.5
done

port="$(docker port "$name" 5432/tcp | head -1 | awk -F: '{print $NF}')"
export SENTINEL_TEST_DATABASE_URL="postgres://sentinel:test@127.0.0.1:${port}/sentinel?sslmode=disable"
go test "$@" ./...
```

Run: `chmod +x /home/sysadmin/sentinel/backend/scripts/test-db.sh`

In `backend/Makefile`, after the `test:` target, add:

```make
## test-db: Run the tests including database integration tests (needs Docker)
test-db:
	./scripts/test-db.sh
```

(The recipe line starts with a tab.)

- [ ] **Step 6: Run the harness both ways**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/database/ -run TestDB -v 2>&1 | tail -3`
Expected: `--- SKIP: TestDBMigrationsApplyAndAreIdempotent` (no env var).

Run: `cd /home/sysadmin/sentinel/backend && make test-db 2>&1 | tail -12`
Expected: every package `ok`; `internal/database` ran the DB test (confirm with `./scripts/test-db.sh -run TestDBMigrations -v 2>&1 | grep -E '^(---|ok)'` → `--- PASS`).

- [ ] **Step 7: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/internal/database/migrate.go backend/internal/database/migrate_test.go backend/internal/testdb/testdb.go \
  backend/scripts/test-db.sh backend/Makefile backend/cmd/sentinel/main.go
git commit -m "test(db): real-database test harness; migrations move to internal/database

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Devices schema and models

**Files:**
- Create: `backend/migrations/046_snmp_devices.sql`
- Create: `backend/internal/models/snmp.go`, `backend/internal/models/snmp_test.go`
- Create: `backend/internal/services/device_schema_db_test.go`
- Modify: `backend/internal/services/site_service.go` (`siteContentTables`)
- Modify: `docs/superpowers/specs/2026-09-29-network-phase1-snmp-foundation-design.md` (the two spec corrections)

**Interfaces:**
- Consumes: `cryptutil` is not used here (encryption happens in the service, Task 6).
- Produces (package `models`):
  - Constants `SNMPVersion1 = "1"`, `SNMPVersion2c = "2c"`, `SNMPVersion3 = "3"`; `SNMPProtoNone = "none"`.
  - `ValidAuthProtocols`, `ValidPrivProtocols map[string]bool`.
  - `type SNMPCredential struct { ID uuid.UUID; Name string; SiteID *uuid.UUID; Version string; Community string \`json:"-"\`; Username string; AuthProtocol string; AuthPassword string \`json:"-"\`; PrivProtocol string; PrivPassword string \`json:"-"\`; CreatedBy *uuid.UUID; CreatedAt, UpdatedAt time.Time }` (table `snmp_credentials`; secret fields hold ciphertext).
  - `type CredentialInput struct { Name string; SiteID *uuid.UUID; Version string; Community *string; Username string; AuthProtocol string; AuthPassword *string; PrivProtocol string; PrivPassword *string }` (JSON names snake_case).
  - `type CredentialSecrets struct { Community, AuthPassword, PrivPassword *string }` — plaintext values to store; nil = keep existing.
  - `NormalizeCredentialInput(in CredentialInput, existing *SNMPCredential) (CredentialInput, error)` — validates; for secrets, `nil`/empty keeps existing (valid only if existing has one).
  - Device status constants `DeviceStatusPending/Up/Down/Paused/Error` = `pending/up/down/paused/error`.
  - `type Device struct { ... }` (table `devices`, fields as in the migration, `NotifyChannels StringSlice`).
  - `type DeviceInput struct { SiteID uuid.UUID; CredentialID uuid.UUID; Name string; Host string; Port int; Enabled *bool; PollInterval, TimeoutMs, Retries int; NotifyChannels StringSlice }` and `NormalizeDeviceInput(in DeviceInput) (DeviceInput, error)` — applies defaults (port 161, 60, 3000, 1, enabled true) and range checks.
  - `type DeviceInterface struct { ... }` (table `device_interfaces`).

- [ ] **Step 1: Write the failing model tests**

`backend/internal/models/snmp_test.go`:

```go
package models

import (
	"strings"
	"testing"
)

func sp(s string) *string { return &s }

func TestNormalizeCredentialInput(t *testing.T) {
	cases := []struct {
		name     string
		in       CredentialInput
		existing *SNMPCredential
		wantErr  string // substring; "" = valid
	}{
		{"v2c with community", CredentialInput{Name: "Default", Version: "2c", Community: sp("public")}, nil, ""},
		{"v1 with community", CredentialInput{Name: "Radios", Version: "1", Community: sp("radios")}, nil, ""},
		{"v2c without community", CredentialInput{Name: "x", Version: "2c"}, nil, "community"},
		{"v2c blank community keeps existing", CredentialInput{Name: "x", Version: "2c", Community: sp("")},
			&SNMPCredential{Version: "2c", Community: "ciphertext"}, ""},
		{"v2c blank community with nothing stored", CredentialInput{Name: "x", Version: "2c", Community: sp("")},
			&SNMPCredential{Version: "3"}, "community"},
		{"unknown version", CredentialInput{Name: "x", Version: "4", Community: sp("c")}, nil, "version"},
		{"blank name", CredentialInput{Name: "  ", Version: "2c", Community: sp("c")}, nil, "name"},
		{"v3 noAuthNoPriv", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "none", PrivProtocol: "none"}, nil, ""},
		{"v3 without username", CredentialInput{Name: "x", Version: "3", AuthProtocol: "none", PrivProtocol: "none"}, nil, "username"},
		{"v3 authNoPriv", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA", AuthPassword: sp("longenough"), PrivProtocol: "none"}, nil, ""},
		{"v3 auth password too short", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA", AuthPassword: sp("short"), PrivProtocol: "none"}, nil, "8 characters"},
		{"v3 auth without password", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA256", PrivProtocol: "none"}, nil, "auth password"},
		{"v3 authPriv", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA", AuthPassword: sp("longenough"), PrivProtocol: "AES", PrivPassword: sp("alsolongenough")}, nil, ""},
		{"v3 priv without auth", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "none", PrivProtocol: "AES", PrivPassword: sp("alsolongenough")}, nil, "without authentication"},
		{"v3 unknown auth protocol", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA1", AuthPassword: sp("longenough"), PrivProtocol: "none"}, nil, "auth protocol"},
		{"v3 unknown priv protocol", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA", AuthPassword: sp("longenough"), PrivProtocol: "3DES", PrivPassword: sp("alsolongenough")}, nil, "privacy protocol"},
		{"v3 keeps stored auth password", CredentialInput{Name: "x", Version: "3", Username: "mon", AuthProtocol: "SHA", PrivProtocol: "none"},
			&SNMPCredential{Version: "3", AuthPassword: "ciphertext"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeCredentialInput(tc.in, tc.existing)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

// Fields that do not belong to the chosen version are cleared, so a profile
// switched from v3 to v2c does not keep a username it no longer uses.
func TestNormalizeCredentialInputClearsOtherVersionFields(t *testing.T) {
	out, err := NormalizeCredentialInput(CredentialInput{
		Name: " Default ", Version: "2c", Community: sp("public"),
		Username: "leftover", AuthProtocol: "SHA", AuthPassword: sp("longenough"), PrivProtocol: "AES", PrivPassword: sp("x"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "Default" || out.Username != "" || out.AuthProtocol != "none" || out.PrivProtocol != "none" ||
		out.AuthPassword != nil || out.PrivPassword != nil {
		t.Errorf("v2c profile kept v3 fields: %+v", out)
	}

	out, err = NormalizeCredentialInput(CredentialInput{
		Name: "v3", Version: "3", Username: "mon", AuthProtocol: "none", PrivProtocol: "none", Community: sp("leftover"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Community != nil {
		t.Error("v3 profile kept a community")
	}
}

func TestNormalizeDeviceInput(t *testing.T) {
	ok := DeviceInput{Name: " core-sw-1 ", Host: " 10.20.0.2 "}
	out, err := NormalizeDeviceInput(ok)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "core-sw-1" || out.Host != "10.20.0.2" || out.Port != 161 || out.PollInterval != 60 ||
		out.TimeoutMs != 3000 || out.Retries != 1 || out.Enabled == nil || !*out.Enabled {
		t.Errorf("defaults not applied: %+v", out)
	}

	// Name may be empty: the device is named from sysName after its first
	// inventory. Host may not.
	if _, err := NormalizeDeviceInput(DeviceInput{Host: "10.0.0.1"}); err != nil {
		t.Errorf("empty name should be allowed: %v", err)
	}
	for name, in := range map[string]DeviceInput{
		"blank host":    {Host: "  "},
		"host with space": {Host: "10.0.0.1 extra"},
		"port 0 given":  {Host: "h", Port: -1},
		"port too high": {Host: "h", Port: 70000},
		"interval low":  {Host: "h", PollInterval: 5},
		"interval high": {Host: "h", PollInterval: 4000},
		"timeout low":   {Host: "h", TimeoutMs: 100},
		"retries high":  {Host: "h", Retries: 9},
	} {
		if _, err := NormalizeDeviceInput(in); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/models/ -run 'Credential|DeviceInput' 2>&1 | head -3`
Expected: FAIL, `undefined: CredentialInput`.

- [ ] **Step 3: Write the models**

`backend/internal/models/snmp.go`:

```go
package models

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// SNMP versions a credential profile can use.
const (
	SNMPVersion1  = "1"
	SNMPVersion2c = "2c"
	SNMPVersion3  = "3"

	SNMPProtoNone = "none"
)

// ValidAuthProtocols and ValidPrivProtocols are the v3 protocols Sentinel
// supports, named as the schema's CHECK constraints name them.
var (
	ValidAuthProtocols = map[string]bool{
		SNMPProtoNone: true, "MD5": true, "SHA": true, "SHA224": true, "SHA256": true, "SHA384": true, "SHA512": true,
	}
	ValidPrivProtocols = map[string]bool{
		SNMPProtoNone: true, "DES": true, "AES": true, "AES192": true, "AES256": true,
	}
)

// minV3PasswordLen is RFC 3414's minimum for USM passphrases; shorter ones are
// rejected by most agents with an unhelpful error.
const minV3PasswordLen = 8

// SNMPCredential is a credential profile. Community and the v3 passwords hold
// ciphertext (cryptutil) and never leave the server: json:"-".
type SNMPCredential struct {
	ID           uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name         string     `json:"name" gorm:"column:name;not null"`
	SiteID       *uuid.UUID `json:"site_id" gorm:"column:site_id;type:uuid"`
	Version      string     `json:"version" gorm:"column:version;not null"`
	Community    string     `json:"-" gorm:"column:community"`
	Username     string     `json:"username" gorm:"column:username"`
	AuthProtocol string     `json:"auth_protocol" gorm:"column:auth_protocol;not null;default:none"`
	AuthPassword string     `json:"-" gorm:"column:auth_password"`
	PrivProtocol string     `json:"priv_protocol" gorm:"column:priv_protocol;not null;default:none"`
	PrivPassword string     `json:"-" gorm:"column:priv_password"`
	CreatedBy    *uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid"`
	CreatedAt    time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (SNMPCredential) TableName() string { return "snmp_credentials" }

// CredentialInput is a create or update request. Secrets are plaintext here;
// nil or "" means "keep what is stored" on update.
type CredentialInput struct {
	Name         string     `json:"name"`
	SiteID       *uuid.UUID `json:"site_id"`
	Version      string     `json:"version"`
	Community    *string    `json:"community"`
	Username     string     `json:"username"`
	AuthProtocol string     `json:"auth_protocol"`
	AuthPassword *string    `json:"auth_password"`
	PrivProtocol string     `json:"priv_protocol"`
	PrivPassword *string    `json:"priv_password"`
}

func blankSecret(s *string) bool { return s == nil || *s == "" }

// NormalizeCredentialInput validates a profile against its version and clears
// every field that does not belong to that version. existing is the stored
// profile on update (nil on create): a blank secret is valid when existing
// already holds one, and is returned as nil ("keep").
func NormalizeCredentialInput(in CredentialInput, existing *SNMPCredential) (CredentialInput, error) {
	out := CredentialInput{
		Name:         strings.TrimSpace(in.Name),
		SiteID:       in.SiteID,
		Version:      strings.TrimSpace(in.Version),
		AuthProtocol: SNMPProtoNone,
		PrivProtocol: SNMPProtoNone,
	}
	if out.Name == "" {
		return CredentialInput{}, errors.New("name is required")
	}
	if utf8.RuneCountInString(out.Name) > 255 {
		return CredentialInput{}, errors.New("name must be 255 characters or fewer")
	}
	has := func(stored string) bool { return existing != nil && stored != "" }

	switch out.Version {
	case SNMPVersion1, SNMPVersion2c:
		if blankSecret(in.Community) {
			if !has(existingField(existing, "community")) {
				return CredentialInput{}, errors.New("community is required for SNMP v1 and v2c")
			}
		} else {
			out.Community = in.Community
		}
	case SNMPVersion3:
		out.Username = strings.TrimSpace(in.Username)
		if out.Username == "" {
			return CredentialInput{}, errors.New("username is required for SNMP v3")
		}
		out.AuthProtocol = strings.TrimSpace(in.AuthProtocol)
		if out.AuthProtocol == "" {
			out.AuthProtocol = SNMPProtoNone
		}
		out.PrivProtocol = strings.TrimSpace(in.PrivProtocol)
		if out.PrivProtocol == "" {
			out.PrivProtocol = SNMPProtoNone
		}
		if !ValidAuthProtocols[out.AuthProtocol] {
			return CredentialInput{}, fmt.Errorf("unknown auth protocol %q", out.AuthProtocol)
		}
		if !ValidPrivProtocols[out.PrivProtocol] {
			return CredentialInput{}, fmt.Errorf("unknown privacy protocol %q", out.PrivProtocol)
		}
		if out.PrivProtocol != SNMPProtoNone && out.AuthProtocol == SNMPProtoNone {
			return CredentialInput{}, errors.New("SNMP v3 cannot use privacy without authentication")
		}
		if out.AuthProtocol != SNMPProtoNone {
			if blankSecret(in.AuthPassword) {
				if !has(existingField(existing, "auth")) {
					return CredentialInput{}, errors.New("an auth password is required for the chosen auth protocol")
				}
			} else if utf8.RuneCountInString(*in.AuthPassword) < minV3PasswordLen {
				return CredentialInput{}, errors.New("the auth password must be at least 8 characters")
			} else {
				out.AuthPassword = in.AuthPassword
			}
		}
		if out.PrivProtocol != SNMPProtoNone {
			if blankSecret(in.PrivPassword) {
				if !has(existingField(existing, "priv")) {
					return CredentialInput{}, errors.New("a privacy password is required for the chosen privacy protocol")
				}
			} else if utf8.RuneCountInString(*in.PrivPassword) < minV3PasswordLen {
				return CredentialInput{}, errors.New("the privacy password must be at least 8 characters")
			} else {
				out.PrivPassword = in.PrivPassword
			}
		}
	default:
		return CredentialInput{}, fmt.Errorf("version must be 1, 2c or 3, not %q", out.Version)
	}
	return out, nil
}

func existingField(c *SNMPCredential, which string) string {
	if c == nil {
		return ""
	}
	switch which {
	case "community":
		return c.Community
	case "auth":
		return c.AuthPassword
	default:
		return c.PrivPassword
	}
}

// Device statuses.
const (
	DeviceStatusPending = "pending"
	DeviceStatusUp      = "up"
	DeviceStatusDown    = "down"
	DeviceStatusPaused  = "paused"
	DeviceStatusError   = "error"
)

// Device is an SNMP-polled device. It belongs to exactly one site.
type Device struct {
	ID                  uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID              uuid.UUID   `json:"site_id" gorm:"column:site_id;type:uuid;not null"`
	CredentialID        uuid.UUID   `json:"credential_id" gorm:"column:credential_id;type:uuid;not null"`
	Name                string      `json:"name" gorm:"column:name;not null"`
	Host                string      `json:"host" gorm:"column:host;not null"`
	Port                int         `json:"port" gorm:"column:port;not null;default:161"`
	Enabled             bool        `json:"enabled" gorm:"column:enabled;not null;default:true"`
	PollInterval        int         `json:"poll_interval" gorm:"column:poll_interval;not null;default:60"`
	TimeoutMs           int         `json:"timeout_ms" gorm:"column:timeout_ms;not null;default:3000"`
	Retries             int         `json:"retries" gorm:"column:retries;not null;default:1"`
	NotifyChannels      StringSlice `json:"notify_channels" gorm:"column:notify_channels;type:jsonb"`
	Status              string      `json:"status" gorm:"column:status;not null;default:pending"`
	StatusDetail        string      `json:"status_detail" gorm:"column:status_detail"`
	ConsecutiveFailures int         `json:"consecutive_failures" gorm:"column:consecutive_failures;not null;default:0"`
	LastPolledAt        *time.Time  `json:"last_polled_at" gorm:"column:last_polled_at"`
	LastSeenAt          *time.Time  `json:"last_seen_at" gorm:"column:last_seen_at"`
	LastInventoryAt     *time.Time  `json:"last_inventory_at" gorm:"column:last_inventory_at"`
	SysName             string      `json:"sys_name" gorm:"column:sys_name"`
	SysDescr            string      `json:"sys_descr" gorm:"column:sys_descr"`
	SysObjectID         string      `json:"sys_object_id" gorm:"column:sys_object_id"`
	SysLocation         string      `json:"sys_location" gorm:"column:sys_location"`
	SysContact          string      `json:"sys_contact" gorm:"column:sys_contact"`
	SysUptimeSeconds    *int64      `json:"sys_uptime_seconds" gorm:"column:sys_uptime_seconds"`
	Vendor              string      `json:"vendor" gorm:"column:vendor"`
	Model               string      `json:"model" gorm:"column:model"`
	Serial              string      `json:"serial" gorm:"column:serial"`
	CreatedBy           *uuid.UUID  `json:"created_by" gorm:"column:created_by;type:uuid"`
	CreatedAt           time.Time   `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt           time.Time   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (Device) TableName() string { return "devices" }

// DeviceInput is a create or update request.
type DeviceInput struct {
	SiteID         uuid.UUID   `json:"site_id"`
	CredentialID   uuid.UUID   `json:"credential_id"`
	Name           string      `json:"name"`
	Host           string      `json:"host"`
	Port           int         `json:"port"`
	Enabled        *bool       `json:"enabled"`
	PollInterval   int         `json:"poll_interval"`
	TimeoutMs      int         `json:"timeout_ms"`
	Retries        int         `json:"retries"`
	NotifyChannels StringSlice `json:"notify_channels"`
}

// NormalizeDeviceInput trims, applies defaults for zero values and checks
// ranges (the same ranges as the schema's CHECK constraints). A zero value
// means "default"; a negative or out-of-range one is an error.
func NormalizeDeviceInput(in DeviceInput) (DeviceInput, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Host = strings.TrimSpace(in.Host)
	if out.Host == "" {
		return DeviceInput{}, errors.New("host is required")
	}
	if strings.ContainsAny(out.Host, " \t/") || utf8.RuneCountInString(out.Host) > 255 {
		return DeviceInput{}, errors.New("host must be an IP address or a DNS name")
	}
	if utf8.RuneCountInString(out.Name) > 255 {
		return DeviceInput{}, errors.New("name must be 255 characters or fewer")
	}
	def := func(v *int, d int) {
		if *v == 0 {
			*v = d
		}
	}
	def(&out.Port, 161)
	def(&out.PollInterval, 60)
	def(&out.TimeoutMs, 3000)
	if out.Retries == 0 && in.Retries == 0 {
		out.Retries = 1
	}
	if out.Enabled == nil {
		t := true
		out.Enabled = &t
	}
	switch {
	case out.Port < 1 || out.Port > 65535:
		return DeviceInput{}, errors.New("port must be between 1 and 65535")
	case out.PollInterval < 10 || out.PollInterval > 3600:
		return DeviceInput{}, errors.New("poll interval must be between 10 and 3600 seconds")
	case out.TimeoutMs < 200 || out.TimeoutMs > 30000:
		return DeviceInput{}, errors.New("timeout must be between 200 and 30000 ms")
	case out.Retries < 0 || out.Retries > 5:
		return DeviceInput{}, errors.New("retries must be between 0 and 5")
	}
	return out, nil
}

// DeviceInterface is one row of a device's interface table.
type DeviceInterface struct {
	ID                uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	DeviceID          uuid.UUID `json:"device_id" gorm:"column:device_id;type:uuid;not null"`
	IfIndex           int       `json:"if_index" gorm:"column:if_index;not null"`
	Name              string    `json:"name" gorm:"column:name"`
	Descr             string    `json:"descr" gorm:"column:descr"`
	Alias             string    `json:"alias" gorm:"column:alias"`
	IfType            int       `json:"if_type" gorm:"column:if_type"`
	SpeedBps          int64     `json:"speed_bps" gorm:"column:speed_bps"`
	MAC               string    `json:"mac" gorm:"column:mac"`
	AdminStatus       string    `json:"admin_status" gorm:"column:admin_status"`
	OperStatus        string    `json:"oper_status" gorm:"column:oper_status"`
	LastChangeSeconds int64     `json:"last_change_seconds" gorm:"column:last_change_seconds"`
	Present           bool      `json:"present" gorm:"column:present;not null;default:true"`
	UpdatedAt         time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (DeviceInterface) TableName() string { return "device_interfaces" }
```

Note on `Retries`: 0 is a legitimate value (no retries) but also the JSON zero value. `NormalizeDeviceInput` treats an explicit `0` in a request as the default of 1; a client wanting zero retries is not supported in phase 1 (the UI offers 1–5). This is deliberate: say so in the handler comment in Task 7.

- [ ] **Step 4: Run the model tests**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/models/ -run 'Credential|DeviceInput' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS for all three test functions.

- [ ] **Step 5: Write migration 046**

`backend/migrations/046_snmp_devices.sql`:

```sql
-- 046_snmp_devices.sql
-- SNMP credential profiles, devices and their interfaces (network monitoring
-- phase 1). All in public and all tables, per the phase 0 backup rule.

CREATE TABLE IF NOT EXISTS snmp_credentials (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              VARCHAR(255) NOT NULL,
    -- NULL = usable by every site; otherwise only by this site's devices.
    site_id           UUID REFERENCES sites (id) ON DELETE RESTRICT,
    version           VARCHAR(3) NOT NULL CHECK (version IN ('1', '2c', '3')),
    -- Secrets are ciphertext (cryptutil) and never returned by the API.
    community         TEXT,
    username          TEXT,
    auth_protocol     VARCHAR(10) NOT NULL DEFAULT 'none'
        CHECK (auth_protocol IN ('none','MD5','SHA','SHA224','SHA256','SHA384','SHA512')),
    auth_password     TEXT,
    priv_protocol     VARCHAR(10) NOT NULL DEFAULT 'none'
        CHECK (priv_protocol IN ('none','DES','AES','AES192','AES256')),
    priv_password     TEXT,
    created_by        UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_snmp_credentials_name ON snmp_credentials (lower(name));

CREATE TABLE IF NOT EXISTS devices (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id              UUID NOT NULL REFERENCES sites (id) ON DELETE RESTRICT,
    credential_id        UUID NOT NULL REFERENCES snmp_credentials (id) ON DELETE RESTRICT,
    name                 VARCHAR(255) NOT NULL,
    host                 VARCHAR(255) NOT NULL,
    port                 INTEGER NOT NULL DEFAULT 161 CHECK (port BETWEEN 1 AND 65535),
    enabled              BOOLEAN NOT NULL DEFAULT true,
    poll_interval        INTEGER NOT NULL DEFAULT 60 CHECK (poll_interval BETWEEN 10 AND 3600),
    timeout_ms           INTEGER NOT NULL DEFAULT 3000 CHECK (timeout_ms BETWEEN 200 AND 30000),
    retries              INTEGER NOT NULL DEFAULT 1 CHECK (retries BETWEEN 0 AND 5),
    -- null = every enabled channel, [] = none (the monitor/agent convention).
    notify_channels      JSONB,
    status               VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'up', 'down', 'paused', 'error')),
    status_detail        TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_polled_at       TIMESTAMPTZ,
    last_seen_at         TIMESTAMPTZ,
    last_inventory_at    TIMESTAMPTZ,
    sys_name             TEXT,
    sys_descr            TEXT,
    sys_object_id        TEXT,
    sys_location         TEXT,
    sys_contact          TEXT,
    sys_uptime_seconds   BIGINT,
    vendor               TEXT,
    model                TEXT,
    serial               TEXT,
    created_by           UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Per site, host and port: overlapping private ranges across sites are normal,
-- and two devices behind one NAT address on different ports are too.
CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_site_host_port ON devices (site_id, lower(host), port);
CREATE INDEX IF NOT EXISTS idx_devices_site ON devices (site_id);
CREATE INDEX IF NOT EXISTS idx_devices_due ON devices (last_polled_at) WHERE enabled;

CREATE TABLE IF NOT EXISTS device_interfaces (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id            UUID NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    if_index             INTEGER NOT NULL,
    name                 TEXT,
    descr                TEXT,
    alias                TEXT,
    if_type              INTEGER,
    speed_bps            BIGINT,
    mac                  TEXT,
    admin_status         VARCHAR(20),
    oper_status          VARCHAR(20),
    last_change_seconds  BIGINT,
    -- false once a walk no longer reports it; kept so later history is not
    -- orphaned. Deleted with its device.
    present              BOOLEAN NOT NULL DEFAULT true,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (device_id, if_index)
);
```

- [ ] **Step 6: Protect sites that hold devices or profiles**

In `backend/internal/services/site_service.go`, replace:

```go
var siteContentTables = []string{}
```

with:

```go
var siteContentTables = []string{"devices", "snmp_credentials"}
```

and update the comment above it: "Phase 1: devices and site-scoped credential profiles."

- [ ] **Step 7: Write the schema DB tests**

`backend/internal/services/device_schema_db_test.go`:

```go
package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

type seeded struct {
	SiteID, CredentialID, DeviceID uuid.UUID
}

// seedDevice inserts a site, a global v2c credential and a device.
func seedDevice(t *testing.T, db *gorm.DB, siteName, host string) seeded {
	t.Helper()
	s := seeded{SiteID: uuid.New(), CredentialID: uuid.New(), DeviceID: uuid.New()}
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, ?)`, s.SiteID, siteName)
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, ?, '2c', 'x')`,
		s.CredentialID, "cred-"+s.CredentialID.String()[:8])
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, ?, ?)`,
		s.DeviceID, s.SiteID, s.CredentialID, "dev-"+host, host)
	return s
}

// A site holding devices cannot be deleted; nor can one holding its own
// credential profiles. Deleting a site must never silently delete them.
func TestDBSiteWithDevicesCannotBeDeleted(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	svc := NewSiteService(db)

	if _, err := svc.Delete(context.Background(), s.SiteID); !errors.Is(err, ErrSiteNotEmpty) {
		t.Fatalf("site with a device: got %v, want ErrSiteNotEmpty", err)
	}

	other := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Branch')`, other)
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (name, site_id, version, community) VALUES ('branch-only', ?, '2c', 'x')`, other)
	if _, err := svc.Delete(context.Background(), other); !errors.Is(err, ErrSiteNotEmpty) {
		t.Fatalf("site with a site-scoped credential: got %v, want ErrSiteNotEmpty", err)
	}
}

// The same address may appear in two sites, and twice in one site on
// different ports, but not twice on the same port.
func TestDBDeviceHostUniqueness(t *testing.T) {
	db := testdb.Open(t)
	a := seedDevice(t, db, "A", "10.0.0.2")
	b := seedDevice(t, db, "B", "10.0.0.2") // same host, other site: fine

	testdb.Exec(t, db, `INSERT INTO devices (site_id, credential_id, name, host, port) VALUES (?, ?, 'nat', '10.0.0.2', 1161)`,
		a.SiteID, a.CredentialID)
	err := db.Exec(`INSERT INTO devices (site_id, credential_id, name, host, port) VALUES (?, ?, 'dup', '10.0.0.2', 161)`,
		b.SiteID, b.CredentialID).Error
	if err == nil {
		t.Fatal("duplicate site+host+port was accepted")
	}
	err = db.Exec(`INSERT INTO devices (site_id, credential_id, name, host, port) VALUES (?, ?, 'case', '10.0.0.2', 1161)`,
		a.SiteID, a.CredentialID).Error
	if err == nil {
		t.Fatal("duplicate site+host+port (second port) was accepted")
	}
}

// Deleting a device removes its interfaces.
func TestDBDeviceDeleteCascadesInterfaces(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name) VALUES (?, 1, 'ge-0/0/1')`, s.DeviceID)
	testdb.Exec(t, db, `DELETE FROM devices WHERE id = ?`, s.DeviceID)
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM device_interfaces WHERE device_id = ?`, s.DeviceID).Scan(&n).Error)
	if n != 0 {
		t.Errorf("%d interfaces left after deleting their device", n)
	}
}
```

- [ ] **Step 8: Run the DB tests**

Run: `cd /home/sysadmin/sentinel/backend && ./scripts/test-db.sh -run 'TestDB(SiteWith|DeviceHost|DeviceDelete)' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: three `--- PASS`.

Then: `cd /home/sysadmin/sentinel/backend && go build ./... && go vet ./... && go test ./internal/... 2>&1 | grep -v 'no test files'`
Expected: all `ok`.

- [ ] **Step 9: Apply the two spec corrections**

In `docs/superpowers/specs/2026-09-29-network-phase1-snmp-foundation-design.md`:
- Replace `CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_site_host ON devices (site_id, lower(host));` with `CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_site_host_port ON devices (site_id, lower(host), port);` and the bullet beginning `- **Host** is an IP address or a DNS name. Hosts are unique per site` so it reads "Hosts are unique per site **and port** (case-insensitive), not globally: overlapping private ranges across sites are normal for an MSP, and so are two devices behind one NAT address on different ports."
- Replace "Channels come from the device's `notify_channels`; none selected means no notification (the incident still opens)." with "Channels come from the device's `notify_channels`, with the existing monitor/agent convention: `null` means every enabled channel, `[]` means none (the incident still opens)."

- [ ] **Step 10: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/migrations/046_snmp_devices.sql backend/internal/models/snmp.go backend/internal/models/snmp_test.go \
  backend/internal/services/device_schema_db_test.go backend/internal/services/site_service.go \
  docs/superpowers/specs/2026-09-29-network-phase1-snmp-foundation-design.md
git commit -m "feat(snmp): credential profiles, devices and interfaces schema

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 3: Incidents and notifications gain a device subject

**Files:**
- Create: `backend/migrations/047_device_incidents.sql`
- Modify: `backend/internal/models/monitor.go` (`Incident`, `Notification`)
- Modify: `backend/internal/services/incident_service.go`
- Create: `backend/internal/services/incident_device_db_test.go`
- Modify: `backend/internal/api/incident_handler.go`, `backend/internal/api/incident_comment_handler.go`, `backend/internal/api/incident_access_test.go`
- Modify: `backend/internal/notifications/plugins.go` (`NotificationMessage.DeviceID`, `StoreNotificationRecord`)
- Modify: `backend/internal/notifications/webhook.go` (device section and links)
- Modify: `backend/cmd/sentinel/main.go` (`RegisterIncidentRoutes` call)

**Interfaces:**
- Consumes: `services.SiteAccessLevel`, `SiteService.SiteAccess`, `services.ErrSiteNotFound` (phase 0).
- Produces:
  - `models.Incident.MonitorID *uuid.UUID`, `models.Incident.DeviceID *uuid.UUID`; `models.Notification.DeviceID *uuid.UUID`.
  - `IncidentService.OpenDeviceIncident(ctx, deviceID uuid.UUID, start time.Time, reason string) (*models.Incident, bool, error)` — returns the active incident and `false` if one is already open.
  - `IncidentService.CloseDeviceIncident(ctx, deviceID uuid.UUID, end time.Time, note string) (*models.Incident, error)` — `nil, nil` when none is open.
  - `IncidentListOptions.DeviceID *uuid.UUID`, `IncidentListOptions.Subject string` (`""`, `"monitor"`, `"device"`).
  - `IncidentWithMonitor` gains `SubjectType string \`json:"subject_type"\``, `SubjectName`, `SubjectTarget`, `SiteID *uuid.UUID \`json:"site_id"\``, `SiteName string \`json:"site_name"\``. For device rows `monitor_name`/`monitor_url`/`monitor_type` are filled from the device (`name`, `host`, `"snmp"`) so the current UI keeps rendering.
  - `notifications.NotificationMessage.DeviceID *uuid.UUID`, `SiteName string`.
  - `api.RegisterIncidentRoutes(rg, incidentService, monitorService, sites siteAccessChecker, db)`.
  - `api.siteAccessChecker` interface: `SiteAccess(ctx, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error)`.

Ruling recorded in this plan: the spec keeps `monitor_*` fields "for monitor rows"; this plan also fills them for device rows (from the device), a superset that lets the existing Incidents page render device incidents before Task 15 updates it.

- [ ] **Step 1: Write the failing DB tests**

`backend/internal/services/incident_device_db_test.go`:

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

func newMonitor(t *testing.T, db *gorm.DB, owner uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO monitors (id, name, type, url, owner_id) VALUES (?, ?, 'http', 'https://example.test', ?)`,
		id, name, owner)
	return id
}

func insertIncident(t *testing.T, db *gorm.DB, monitorID, deviceID *uuid.UUID, start time.Time, end *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO incidents (id, monitor_id, device_id, start_time, end_time, severity) VALUES (?, ?, ?, ?, ?, 'high')`,
		id, monitorID, deviceID, start, end)
	return id
}

// A switch outage must never count against a monitor. Every monitor query
// filters monitor_id = ?, so a device incident (monitor_id NULL) is invisible
// to them; this pins it against real SQL.
func TestDBDeviceIncidentsInvisibleToMonitorQueries(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	owner := testdb.NewUser(t, db, true)
	mon := newMonitor(t, db, owner, "web")
	dev := seedDevice(t, db, "HQ", "10.0.0.2")

	now := time.Now().UTC()
	start := now.Add(-2 * time.Hour)
	// The device is down for the whole window and still open; the monitor had
	// one closed 10-minute incident.
	insertIncident(t, db, nil, &dev.DeviceID, start.Add(-time.Hour), nil)
	end := start.Add(10 * time.Minute)
	insertIncident(t, db, &mon, nil, start, &end)

	svc := NewIncidentService(db)
	down, err := svc.GetIncidentDuration(ctx, mon, start.Add(-time.Minute), now)
	testdb.Must(t, err)
	if down != 10*time.Minute {
		t.Errorf("monitor downtime = %s, want 10m (device incident leaked in?)", down)
	}
	n, err := svc.GetIncidentCount(ctx, mon, start.Add(-3*time.Hour), now)
	testdb.Must(t, err)
	if n != 1 {
		t.Errorf("monitor incident count = %d, want 1", n)
	}
	active, err := svc.GetActiveIncident(ctx, mon)
	testdb.Must(t, err)
	if active != nil {
		t.Errorf("monitor has an active incident %s; only the device does", active.ID)
	}
	list, err := svc.GetOverlappingIncidents(ctx, mon, start.Add(-3*time.Hour), now)
	testdb.Must(t, err)
	if len(list) != 1 {
		t.Errorf("overlapping monitor incidents = %d, want 1", len(list))
	}
}

// Exactly one subject, enforced by the database for incidents and
// notifications alike.
func TestDBIncidentSubjectCheck(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, true)
	mon := newMonitor(t, db, owner, "web")
	dev := seedDevice(t, db, "HQ", "10.0.0.2")

	if err := db.Exec(`INSERT INTO incidents (monitor_id, device_id, start_time) VALUES (?, ?, now())`, mon, dev.DeviceID).Error; err == nil {
		t.Error("incident with both subjects was accepted")
	}
	if err := db.Exec(`INSERT INTO incidents (start_time) VALUES (now())`).Error; err == nil {
		t.Error("incident with no subject was accepted")
	}
	if err := db.Exec(`INSERT INTO notifications (monitor_id, device_id, channel, status) VALUES (?, ?, 'webhook', 'sent')`, mon, dev.DeviceID).Error; err == nil {
		t.Error("notification with two subjects was accepted")
	}
	testdb.Exec(t, db, `INSERT INTO notifications (device_id, channel, status) VALUES (?, 'webhook', 'sent')`, dev.DeviceID)
}

// Deleting a device deletes its incidents and notifications, as deleting a
// monitor does.
func TestDBDeviceDeleteCascadesIncidents(t *testing.T) {
	db := testdb.Open(t)
	dev := seedDevice(t, db, "HQ", "10.0.0.2")
	inc := insertIncident(t, db, nil, &dev.DeviceID, time.Now().UTC(), nil)
	testdb.Exec(t, db, `INSERT INTO notifications (device_id, incident_id, channel, status) VALUES (?, ?, 'webhook', 'sent')`, dev.DeviceID, inc)
	testdb.Exec(t, db, `DELETE FROM devices WHERE id = ?`, dev.DeviceID)
	var n int64
	testdb.Must(t, db.Raw(`SELECT (SELECT count(*) FROM incidents WHERE device_id = ?) + (SELECT count(*) FROM notifications WHERE device_id = ?)`,
		dev.DeviceID, dev.DeviceID).Scan(&n).Error)
	if n != 0 {
		t.Errorf("%d incident/notification rows survived their device", n)
	}
}

// Opening is idempotent while an incident is open; closing stamps the end and
// a note; closing when nothing is open is a no-op.
func TestDBOpenCloseDeviceIncident(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	dev := seedDevice(t, db, "HQ", "10.0.0.2")
	svc := NewIncidentService(db)

	start := time.Now().UTC().Add(-5 * time.Minute)
	first, opened, err := svc.OpenDeviceIncident(ctx, dev.DeviceID, start, "no SNMP response")
	testdb.Must(t, err)
	if !opened || first.DeviceID == nil || *first.DeviceID != dev.DeviceID || first.MonitorID != nil {
		t.Fatalf("first open: opened=%v incident=%+v", opened, first)
	}
	again, opened, err := svc.OpenDeviceIncident(ctx, dev.DeviceID, time.Now().UTC(), "still down")
	testdb.Must(t, err)
	if opened || again.ID != first.ID {
		t.Errorf("second open created a new incident (opened=%v, %s vs %s)", opened, again.ID, first.ID)
	}

	closed, err := svc.CloseDeviceIncident(ctx, dev.DeviceID, time.Now().UTC(), "Monitoring was paused.")
	testdb.Must(t, err)
	if closed == nil || closed.EndTime == nil || closed.DurationSeconds < 290 || closed.ResolutionNotes != "Monitoring was paused." {
		t.Errorf("close: %+v", closed)
	}
	none, err := svc.CloseDeviceIncident(ctx, dev.DeviceID, time.Now().UTC(), "")
	testdb.Must(t, err)
	if none != nil {
		t.Errorf("closing with nothing open returned %+v", none)
	}
}

// Members see exactly their own: monitor incidents by monitor ownership or
// sharing, device incidents by site sharing. Admins see everything.
func TestDBIncidentListAccess(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	alice := testdb.NewUser(t, db, false)
	bob := testdb.NewUser(t, db, false)

	aliceMon := newMonitor(t, db, alice, "alice-web")
	adminMon := newMonitor(t, db, admin, "admin-web")
	shared := seedDevice(t, db, "Shared", "10.0.0.2")
	private := seedDevice(t, db, "Private", "10.0.0.3")
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, shared.SiteID, alice)

	now := time.Now().UTC()
	want := map[string]uuid.UUID{
		"aliceMon": insertIncident(t, db, &aliceMon, nil, now, nil),
		"adminMon": insertIncident(t, db, &adminMon, nil, now, nil),
		"shared":   insertIncident(t, db, nil, &shared.DeviceID, now, nil),
		"private":  insertIncident(t, db, nil, &private.DeviceID, now, nil),
	}
	svc := NewIncidentService(db)
	list := func(v IncidentViewer, mod func(*IncidentListOptions)) map[uuid.UUID]IncidentWithMonitor {
		opts := IncidentListOptions{Viewer: &v}
		if mod != nil {
			mod(&opts)
		}
		rows, _, err := svc.ListIncidents(ctx, opts)
		testdb.Must(t, err)
		out := map[uuid.UUID]IncidentWithMonitor{}
		for _, r := range rows {
			out[r.ID] = r
		}
		return out
	}

	if got := list(IncidentViewer{UserID: admin, IsAdmin: true}, nil); len(got) != 4 {
		t.Errorf("admin sees %d incidents, want 4", len(got))
	}
	a := list(IncidentViewer{UserID: alice}, nil)
	if len(a) != 2 || a[want["aliceMon"]].ID == uuid.Nil || a[want["shared"]].ID == uuid.Nil {
		t.Errorf("alice sees %v, want exactly her monitor's and the shared site's", keys(a))
	}
	if row := a[want["shared"]]; row.SubjectType != "device" || row.SubjectName != "dev-10.0.0.2" ||
		row.SubjectTarget != "10.0.0.2" || row.SiteName != "Shared" || row.MonitorName != "dev-10.0.0.2" {
		t.Errorf("device row fields: %+v", row)
	}
	if row := a[want["aliceMon"]]; row.SubjectType != "monitor" || row.SubjectName != "alice-web" || row.SiteID != nil {
		t.Errorf("monitor row fields: %+v", row)
	}
	if got := list(IncidentViewer{UserID: alice}, func(o *IncidentListOptions) { o.DeviceID = &private.DeviceID }); len(got) != 0 {
		t.Errorf("alice filtered to a private device sees %d incidents, want 0", len(got))
	}
	if got := list(IncidentViewer{UserID: alice}, func(o *IncidentListOptions) { o.Subject = "device" }); len(got) != 1 {
		t.Errorf("alice subject=device sees %d, want 1", len(got))
	}
	if got := list(IncidentViewer{UserID: bob}, nil); len(got) != 0 {
		t.Errorf("bob sees %d incidents, want 0", len(got))
	}

	row, err := svc.GetIncidentByID(ctx, want["private"])
	testdb.Must(t, err)
	if row.SubjectType != "device" || row.SiteID == nil || *row.SiteID != private.SiteID {
		t.Errorf("GetIncidentByID device row: %+v", row)
	}
}

func keys(m map[uuid.UUID]IncidentWithMonitor) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./internal/services/ 2>&1 | head -3`
Expected: compile errors: `unknown field DeviceID`, `svc.OpenDeviceIncident undefined`.

- [ ] **Step 3: Migration 047**

`backend/migrations/047_device_incidents.sql`:

```sql
-- 047_device_incidents.sql
-- Incidents and notifications can belong to a network device as well as a
-- monitor (network monitoring phase 1). Mirrors 040, which made notifications
-- "monitor or agent".
--
-- Every existing uptime/downtime query filters monitor_id = ?, so a device
-- incident (monitor_id NULL) cannot reach a monitor's numbers.

ALTER TABLE incidents ALTER COLUMN monitor_id DROP NOT NULL;
ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS device_id UUID REFERENCES devices (id) ON DELETE CASCADE;
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_subject_check;
ALTER TABLE incidents
    ADD CONSTRAINT incidents_subject_check CHECK (num_nonnulls(monitor_id, device_id) = 1);
CREATE INDEX IF NOT EXISTS idx_incidents_device_start
    ON incidents (device_id, start_time DESC) WHERE device_id IS NOT NULL;

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS device_id UUID REFERENCES devices (id) ON DELETE CASCADE;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_subject_check;
ALTER TABLE notifications
    ADD CONSTRAINT notifications_subject_check
    CHECK (num_nonnulls(monitor_id, agent_id, device_id) = 1);
CREATE INDEX IF NOT EXISTS idx_notifications_device
    ON notifications (device_id, created_at DESC) WHERE device_id IS NOT NULL;
```

- [ ] **Step 4: Models**

In `backend/internal/models/monitor.go`, in `type Incident struct`, replace:

```go
	MonitorID uuid.UUID `json:"monitor_id" gorm:"column:monitor_id;type:uuid;not null"`
```

with:

```go
	// Exactly one of MonitorID and DeviceID is set (incidents_subject_check).
	MonitorID *uuid.UUID `json:"monitor_id" gorm:"column:monitor_id;type:uuid"`
	DeviceID  *uuid.UUID `json:"device_id" gorm:"column:device_id;type:uuid"`
```

In `type Notification struct`, after `AgentID`, add:

```go
	DeviceID   *uuid.UUID `json:"device_id" gorm:"column:device_id;type:uuid"`
```

and change its comment to "Exactly one of MonitorID, AgentID and DeviceID is set."

Run `cd /home/sysadmin/sentinel/backend && go build ./... 2>&1 | head -20` and fix each error in Step 5.

- [ ] **Step 5: IncidentService**

In `backend/internal/services/incident_service.go`:

1. In `CreateIncidentFromCheck`, change `MonitorID: monitorID,` to `MonitorID: &monitorID,`.
2. In `CloseIncident`, change the log line to `s.logger.Printf("[incident] closed id=%s duration=%ds", incident.ID, incident.DurationSeconds)`.
3. Replace the body of `ChecksDuringIncident` up to the query with a guard, so device incidents return no checks:

```go
func (s *IncidentService) ChecksDuringIncident(ctx context.Context, inc *models.Incident, limit int) ([]models.Check, error) {
	// Checks belong to monitors; a device incident has none.
	if inc.MonitorID == nil {
		return nil, nil
	}
	if limit < 1 || limit > 500 {
		limit = 200
	}
	end := time.Now()
	if inc.EndTime != nil {
		end = *inc.EndTime
	}
	var checks []models.Check
	err := s.db.WithContext(ctx).
		Where("monitor_id = ? AND timestamp >= ? AND timestamp <= ?", *inc.MonitorID, inc.StartTime, end).
		Order("timestamp ASC").
		Limit(limit).
		Find(&checks).Error
	if err != nil {
		return nil, fmt.Errorf("loading checks for incident %s: %w", inc.ID, err)
	}
	return checks, nil
}
```

4. Add after `CloseIncident`:

```go
// OpenDeviceIncident opens an incident for an unreachable device, unless one
// is already open, in which case that one is returned with opened=false.
func (s *IncidentService) OpenDeviceIncident(ctx context.Context, deviceID uuid.UUID, start time.Time, reason string) (*models.Incident, bool, error) {
	if active, err := s.activeDeviceIncident(ctx, deviceID); err != nil || active != nil {
		return active, false, err
	}
	now := time.Now()
	incident := &models.Incident{
		ID:           uuid.New(),
		DeviceID:     &deviceID,
		StartTime:    start,
		Severity:     defaultIncidentSeverity,
		IncidentType: models.IncidentTypeDown,
		RootCause:    reason,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.WithContext(ctx).Create(incident).Error; err != nil {
		return nil, false, fmt.Errorf("creating incident for device %s: %w", deviceID, err)
	}
	s.logger.Printf("[incident] opened id=%s device=%s", incident.ID, deviceID)
	return incident, true, nil
}

// CloseDeviceIncident closes a device's open incident, appending note to its
// resolution notes when given. Returns nil, nil when nothing is open.
func (s *IncidentService) CloseDeviceIncident(ctx context.Context, deviceID uuid.UUID, end time.Time, note string) (*models.Incident, error) {
	active, err := s.activeDeviceIncident(ctx, deviceID)
	if err != nil || active == nil {
		return nil, err
	}
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
	if err := s.db.WithContext(ctx).Model(&models.Incident{}).Where("id = ?", active.ID).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("closing incident %s: %w", active.ID, err)
	}
	var closed models.Incident
	if err := s.db.WithContext(ctx).First(&closed, "id = ?", active.ID).Error; err != nil {
		return nil, fmt.Errorf("reloading incident %s: %w", active.ID, err)
	}
	s.logger.Printf("[incident] closed id=%s device=%s duration=%ds", closed.ID, deviceID, closed.DurationSeconds)
	return &closed, nil
}

func (s *IncidentService) activeDeviceIncident(ctx context.Context, deviceID uuid.UUID) (*models.Incident, error) {
	var incident models.Incident
	err := s.db.WithContext(ctx).First(&incident, "device_id = ? AND end_time IS NULL", deviceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying active incident for device %s: %w", deviceID, err)
	}
	return &incident, nil
}
```

5. Extend `IncidentListOptions` (after `MonitorID`):

```go
	// DeviceID restricts to one device.
	DeviceID *uuid.UUID
	// Subject is "", "monitor" or "device".
	Subject string
```

and change `Search`'s comment to "matches the subject's name".

6. Replace `IncidentWithMonitor` with:

```go
// IncidentWithMonitor is a listed incident joined to its subject, so the
// table can show a name without a request per row. For device incidents the
// monitor_* fields carry the device's name and host (and type "snmp"), so
// views built before devices existed still render them.
type IncidentWithMonitor struct {
	models.Incident
	MonitorName   string     `json:"monitor_name" gorm:"column:monitor_name"`
	MonitorURL    string     `json:"monitor_url" gorm:"column:monitor_url"`
	MonitorType   string     `json:"monitor_type" gorm:"column:monitor_type"`
	SubjectType   string     `json:"subject_type" gorm:"column:subject_type"`
	SubjectName   string     `json:"subject_name" gorm:"column:subject_name"`
	SubjectTarget string     `json:"subject_target" gorm:"column:subject_target"`
	SiteID        *uuid.UUID `json:"site_id" gorm:"column:site_id"`
	SiteName      string     `json:"site_name" gorm:"column:site_name"`
}

// incidentSubjectJoins and incidentSubjectSelect are shared by the list and
// the detail so the two can never disagree about a row.
const incidentSubjectJoins = `LEFT JOIN monitors AS m ON m.id = i.monitor_id
	LEFT JOIN devices AS d ON d.id = i.device_id
	LEFT JOIN sites AS st ON st.id = d.site_id`

const incidentSubjectSelect = `i.*,
	COALESCE(m.name, d.name) AS monitor_name,
	COALESCE(m.url, d.host) AS monitor_url,
	COALESCE(m.type, 'snmp') AS monitor_type,
	CASE WHEN i.device_id IS NOT NULL THEN 'device' ELSE 'monitor' END AS subject_type,
	COALESCE(m.name, d.name) AS subject_name,
	COALESCE(m.url, d.host) AS subject_target,
	d.site_id AS site_id,
	COALESCE(st.name, '') AS site_name`
```

7. In `ListIncidents`, replace:

```go
	base := s.db.WithContext(ctx).
		Table("incidents AS i").
		Joins("JOIN monitors AS m ON m.id = i.monitor_id")
```

with:

```go
	base := s.db.WithContext(ctx).
		Table("incidents AS i").
		Joins(incidentSubjectJoins)
```

replace the `if !opts.Viewer.IsAdmin { ... }` block with:

```go
	if !opts.Viewer.IsAdmin {
		// Monitor incidents by monitor ownership or sharing (the monitor list's
		// rule); device incidents by site sharing (SiteAccess's rule).
		base = base.Where(
			`((i.monitor_id IS NOT NULL AND (m.owner_id = ? OR m.id IN
				(SELECT monitor_id FROM monitor_sharing WHERE shared_with_user_id = ?)))
			 OR (i.device_id IS NOT NULL AND d.site_id IN
				(SELECT site_id FROM site_sharing WHERE shared_with_user_id = ?)))`,
			opts.Viewer.UserID, opts.Viewer.UserID, opts.Viewer.UserID,
		)
	}
	if opts.DeviceID != nil {
		base = base.Where("i.device_id = ?", *opts.DeviceID)
	}
	switch opts.Subject {
	case "monitor":
		base = base.Where("i.monitor_id IS NOT NULL")
	case "device":
		base = base.Where("i.device_id IS NOT NULL")
	}
```

replace `base = base.Where("m.name ILIKE ?", "%"+q+"%")` with `base = base.Where("COALESCE(m.name, d.name) ILIKE ?", "%"+q+"%")`, and replace `Select("i.*, m.name AS monitor_name, m.url AS monitor_url, m.type AS monitor_type").` with `Select(incidentSubjectSelect).`.

8. In `GetIncidentByID`, replace the `Joins(...)` and `Select(...)` lines with `Joins(incidentSubjectJoins).` and `Select(incidentSubjectSelect).`.

- [ ] **Step 6: Notifications**

In `backend/internal/notifications/plugins.go`, in `NotificationMessage` after `AgentID`, add:

```go
	// DeviceID is set instead of MonitorID for a network device alert.
	// MonitorName/MonitorURL carry the device's name and host, and SiteName
	// its site, so plugins render it without knowing the difference.
	DeviceID *uuid.UUID `json:"device_id,omitempty"`
	SiteName string     `json:"site_name,omitempty"`
```

In `StoreNotificationRecord`, replace:

```go
	if message.AgentID != nil {
		record.AgentID = message.AgentID
	} else {
```

with:

```go
	switch {
	case message.AgentID != nil:
		record.AgentID = message.AgentID
	case message.DeviceID != nil:
		record.DeviceID = message.DeviceID
	default:
```

and close the `switch` where the `else` block ended (the `monitorID := message.MonitorID` / `record.MonitorID = &monitorID` lines become the `default:` case).

In `backend/internal/notifications/webhook.go`, add a payload section and use device links:

```go
type webhookDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
	Site string `json:"site,omitempty"`
}
```

add `Device *webhookDevice \`json:"device,omitempty"\`` to `webhookPayload` after `Monitor`, and at the end of `buildPayload`, before `return payload`:

```go
	if m.DeviceID != nil {
		payload.Type = "sentinel_device_alert"
		payload.Device = &webhookDevice{ID: m.DeviceID.String(), Name: m.MonitorName, Host: m.MonitorURL, Site: m.SiteName}
		payload.Monitor = webhookMonitor{Name: m.MonitorName, URL: m.MonitorURL}
		payload.Links = webhookLinks{ViewInSentinel: fmt.Sprintf("%s/network/devices/%s", base, *m.DeviceID)}
	}
```

`webhookMonitor.ID` stays empty for devices (it is not a monitor), and `view_report` is empty (device reports come in phase 5). Add a test in `backend/internal/notifications/webhook_test.go` (create it if absent; if a test file for webhook exists, add to it):

```go
func TestWebhookPayloadForDevice(t *testing.T) {
	id := uuid.New()
	p := (&WebhookPlugin{}).buildPayload(&NotificationMessage{
		DeviceID: &id, MonitorName: "core-sw-1", MonitorURL: "10.20.0.2", SiteName: "Warehouse",
		Status: "down", Timestamp: time.Now(),
	})
	if p.Type != "sentinel_device_alert" || p.Device == nil || p.Device.ID != id.String() ||
		p.Device.Site != "Warehouse" || p.Monitor.ID != "" {
		t.Fatalf("payload: %+v", p)
	}
	if !strings.HasSuffix(p.Links.ViewInSentinel, "/network/devices/"+id.String()) {
		t.Errorf("link = %q", p.Links.ViewInSentinel)
	}
}
```

(imports: `strings`, `testing`, `time`, `github.com/google/uuid`.)

- [ ] **Step 7: Handler access through the subject**

In `backend/internal/api/incident_handler.go`, replace the `monitorViewChecker` interface with:

```go
// monitorAccessChecker answers whether a user may see or edit a monitor.
type monitorAccessChecker interface {
	CanUserViewMonitor(ctx context.Context, userID, monitorID uuid.UUID) (bool, error)
	CanUserEditMonitor(ctx context.Context, userID, monitorID uuid.UUID) (bool, error)
}

// siteAccessChecker is SiteService.SiteAccess.
type siteAccessChecker interface {
	SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error)
}

// incidentAccess is what the caller may do with an incident, decided by its
// subject: monitor sharing for a monitor incident, site sharing for a device
// incident.
type incidentAccess struct{ view, edit bool }

func incidentSubjectAccess(ctx context.Context, monitors monitorAccessChecker, sites siteAccessChecker,
	userID uuid.UUID, isAdmin bool, row *services.IncidentWithMonitor) (incidentAccess, error) {
	if isAdmin {
		return incidentAccess{view: true, edit: true}, nil
	}
	switch {
	case row.Incident.MonitorID != nil:
		view, err := monitors.CanUserViewMonitor(ctx, userID, *row.Incident.MonitorID)
		if err != nil || !view {
			return incidentAccess{}, err
		}
		edit, err := monitors.CanUserEditMonitor(ctx, userID, *row.Incident.MonitorID)
		return incidentAccess{view: true, edit: edit}, err
	case row.SiteID != nil:
		level, err := sites.SiteAccess(ctx, userID, false, *row.SiteID)
		if errors.Is(err, services.ErrSiteNotFound) {
			return incidentAccess{}, nil
		}
		if err != nil {
			return incidentAccess{}, err
		}
		return incidentAccess{view: level >= services.SiteAccessReadonly, edit: level >= services.SiteAccessEditable}, nil
	default:
		return incidentAccess{}, nil
	}
}

// authorizeIncident writes the response and returns false unless the caller
// may act on the incident at level ("view" or "edit"). Not being able to see
// it is a 404, exactly like an incident that does not exist; seeing but not
// editing is a 403.
func authorizeIncident(c *gin.Context, monitors monitorAccessChecker, sites siteAccessChecker,
	row *services.IncidentWithMonitor, level string) bool {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondAuthError(c, http.StatusUnauthorized, "authentication required")
		return false
	}
	access, err := incidentSubjectAccess(c.Request.Context(), monitors, sites, userID, isAdmin, row)
	if err != nil {
		respondInternal(c, "incident access", err)
		return false
	}
	if !access.view {
		respondError(c, http.StatusNotFound, "no such incident")
		return false
	}
	if level == "edit" && !access.edit {
		respondError(c, http.StatusForbidden, "you do not have permission to edit this incident")
		return false
	}
	return true
}
```

In `GetIncidentHandler`, change the signature to `func GetIncidentHandler(incidentService incidentDetailReader, monitors monitorAccessChecker, sites siteAccessChecker) gin.HandlerFunc` and replace the whole block from `// Checked before any detail is loaded` through the closing `}` of `if !isAdmin { ... }` with:

```go
		// Checked before any detail is loaded, so nothing about an incident
		// the caller cannot see is read, let alone returned.
		if !authorizeIncident(c, monitors, sites, row, "view") {
			return
		}
```

In `ListIncidentsHandler`, after the `monitor_id` parsing block, add:

```go
		if raw := c.Query("device_id"); raw != "" && raw != "null" {
			id, err := uuid.Parse(raw)
			if err != nil {
				respondError(c, http.StatusBadRequest, "device_id must be a UUID")
				return
			}
			opts.DeviceID = &id
		}
		switch subj := c.Query("subject"); subj {
		case "", "monitor", "device":
			opts.Subject = subj
		default:
			respondError(c, http.StatusBadRequest, "subject must be monitor or device")
			return
		}
```

In `UpdateIncidentHandler`, change the signature to add `sites siteAccessChecker` after `monitorService`, and replace the load-and-authorize block (from `var incident models.Incident` through the `authorizeMonitor(...)` check) with:

```go
		row, err := incidentService.GetIncidentByID(ctx, incidentID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(c, http.StatusNotFound, "incident not found")
				return
			}
			respondInternal(c, "loading incident", err)
			return
		}
		// An incident inherits its subject's permissions: editing needs edit
		// rights on the monitor, or editable access to the device's site.
		if !authorizeIncident(c, monitorService, sites, row, "edit") {
			return
		}
		incident := row.Incident
```

(`incident` is used further down for `EndTime`/`StartTime`; keep those uses.)

Change `RegisterIncidentRoutes` to:

```go
func RegisterIncidentRoutes(rg *gin.RouterGroup, incidentService *services.IncidentService, monitorService *services.MonitorService, sites siteAccessChecker, db *gorm.DB) {
	rg.GET("/incidents", ListIncidentsHandler(incidentService))
	rg.GET("/incidents/:id", GetIncidentHandler(incidentService, monitorService, sites))
	rg.PATCH("/incidents/:id", UpdateIncidentHandler(incidentService, monitorService, sites, db))
	rg.GET("/incidents/:id/comments", ListIncidentCommentsHandler(incidentService, monitorService, sites))
	rg.POST("/incidents/:id/comments", AddIncidentCommentHandler(incidentService, monitorService, sites))
	rg.PATCH("/incidents/:id/comments/:comment_id", UpdateIncidentCommentHandler(incidentService, monitorService, sites))
	rg.DELETE("/incidents/:id/comments/:comment_id", DeleteIncidentCommentHandler(incidentService, monitorService, sites))
}
```

If `UpdateIncidentHandler` no longer uses `db` for loading, keep the parameter only if it still uses it for the update; `go vet` reports an unused parameter only as a lint, so leave it.

In `backend/internal/api/incident_comment_handler.go`: add a `sites siteAccessChecker` parameter after `monitorService` to `loadIncidentForComment` and to each of the four comment handlers, pass it through, and in `loadIncidentForComment` replace `if !authorizeMonitor(c, monitorService, row.Incident.MonitorID, level) {` with `if !authorizeIncident(c, monitorService, sites, row, level) {`. Every call site of `loadIncidentForComment` already passes `"view"` or `"edit"`; confirm with `grep -n 'loadIncidentForComment(' backend/internal/api/incident_comment_handler.go`.

In `backend/cmd/sentinel/main.go`, change `api.RegisterIncidentRoutes(v1, incidentService, monitorService, db)` to `api.RegisterIncidentRoutes(v1, incidentService, monitorService, siteService, db)`. `siteService` is created before the routes (phase 0); if the incident routes are registered before `siteService := ...`, move that line up.

- [ ] **Step 8: Update the handler tests**

In `backend/internal/api/incident_access_test.go`, replace `fakeMonitorViews` and `incidentRouter` with:

```go
type fakeMonitorViews struct{ canView, canEdit bool }

func (f fakeMonitorViews) CanUserViewMonitor(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.canView, nil
}
func (f fakeMonitorViews) CanUserEditMonitor(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.canEdit, nil
}

type fakeSiteAccess struct{ level services.SiteAccessLevel }

func (f fakeSiteAccess) SiteAccess(context.Context, uuid.UUID, bool, uuid.UUID) (services.SiteAccessLevel, error) {
	return f.level, nil
}

func incidentRouter(inc *fakeIncidents, mon fakeMonitorViews, sites fakeSiteAccess, userID uuid.UUID, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	r.GET("/incidents", ListIncidentsHandler(inc))
	r.GET("/incidents/:id", GetIncidentHandler(inc, mon, sites))
	return r
}
```

Update every `incidentRouter(...)` call to pass `fakeSiteAccess{}` as the new third argument, change the test row to `Incident: models.Incident{ID: uuid.New(), MonitorID: ptr(uuid.New())}` with a helper `func ptr[T any](v T) *T { return &v }`, and add:

```go
// A device incident follows site sharing: readonly on the site shows it,
// no access is a 404 with nothing loaded.
func TestGetDeviceIncidentFollowsSiteAccess(t *testing.T) {
	site := uuid.New()
	row := &services.IncidentWithMonitor{
		Incident:    models.Incident{ID: uuid.New(), DeviceID: ptr(uuid.New())},
		SubjectType: "device", SiteID: &site,
	}
	path := "/incidents/" + row.Incident.ID.String()

	inc := &fakeIncidents{row: row}
	w := get(incidentRouter(inc, fakeMonitorViews{canView: true}, fakeSiteAccess{services.SiteAccessNone}, uuid.New(), false), path)
	if w.Code != http.StatusNotFound || inc.detailLoaded {
		t.Errorf("no site access: status %d, detail loaded %v; want 404, false", w.Code, inc.detailLoaded)
	}
	inc = &fakeIncidents{row: row}
	if w := get(incidentRouter(inc, fakeMonitorViews{}, fakeSiteAccess{services.SiteAccessReadonly}, uuid.New(), false), path); w.Code != http.StatusOK {
		t.Errorf("readonly site: status %d, want 200", w.Code)
	}
}
```

Also add to `TestListIncidentsPassesViewer`'s file a test that `?subject=bogus` is a 400 and `?device_id=<uuid>&subject=device` reaches `opts.DeviceID`/`opts.Subject`:

```go
func TestListIncidentsDeviceFilters(t *testing.T) {
	inc := &fakeIncidents{}
	r := incidentRouter(inc, fakeMonitorViews{}, fakeSiteAccess{}, uuid.New(), false)
	if w := get(r, "/incidents?subject=bogus"); w.Code != http.StatusBadRequest {
		t.Errorf("bogus subject: status %d, want 400", w.Code)
	}
	dev := uuid.New()
	if w := get(r, "/incidents?subject=device&device_id="+dev.String()); w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if inc.listOpts.Subject != "device" || inc.listOpts.DeviceID == nil || *inc.listOpts.DeviceID != dev {
		t.Errorf("opts = %+v", *inc.listOpts)
	}
}
```

- [ ] **Step 9: Run everything**

Run: `cd /home/sysadmin/sentinel/backend && go build ./... && go vet ./... && go test ./internal/... 2>&1 | grep -v 'no test files'`
Expected: all `ok`.

Run: `cd /home/sysadmin/sentinel/backend && ./scripts/test-db.sh -run 'TestDB' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: every `TestDB…` from Tasks 1–3 `--- PASS`.

Frontend still type-checks (the `monitor_id` field is still a string for monitor rows; the type is widened in Task 15): run the frontend check from Global Constraints. Expected: 0 errors.

- [ ] **Step 10: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/migrations/047_device_incidents.sql backend/internal/models/monitor.go \
  backend/internal/services/incident_service.go backend/internal/services/incident_device_db_test.go \
  backend/internal/api/incident_handler.go backend/internal/api/incident_comment_handler.go backend/internal/api/incident_access_test.go \
  backend/internal/notifications/plugins.go backend/internal/notifications/webhook.go backend/internal/notifications/webhook_test.go \
  backend/cmd/sentinel/main.go
git commit -m "feat(incidents): incidents and notifications can belong to a network device

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 4: The `internal/snmp` package

**Files:**
- Create: `backend/internal/snmp/client.go`, `backend/internal/snmp/client_test.go`
- Create: `backend/internal/snmp/parse.go`, `backend/internal/snmp/parse_test.go`
- Modify: `backend/go.mod`, `backend/go.sum` (add gosnmp)

**Interfaces:**
- Produces (package `snmp`):
  - `type Credential struct { Version, Community, Username, AuthProtocol, AuthPassword, PrivProtocol, PrivPassword string }` — plaintext, in memory only.
  - `type Target struct { Host string; Port uint16; Credential Credential; Timeout time.Duration; Retries int }`.
  - `type PDU struct { OID string; Value any }` — `OID` without a leading dot; `Value` is `[]byte` (octet strings), `int64` (INTEGER), `uint64` (Counter32/Gauge32/TimeTicks/Counter64/Uinteger32), `string` (OID or IpAddress values), or `nil` (noSuchObject/noSuchInstance/endOfMibView/NULL). Methods `Text() string`, `Number() (uint64, bool)`.
  - `type Client interface { Get(ctx context.Context, t Target, oids []string) ([]PDU, error); Walk(ctx context.Context, t Target, root string) ([]PDU, error) }`.
  - `type GoSNMPClient struct{}` implementing `Client`.
  - `type System struct { Descr, ObjectID, Contact, Name, Location string; UptimeSeconds int64 }`.
  - `type Interface struct { Index int; Name, Descr, Alias string; Type int; SpeedBps int64; MAC string; AdminStatus, OperStatus string; LastChangeSeconds int64 }`.
  - `type Inventory struct { System System; Vendor, Model, Serial string; Interfaces []Interface }`.
  - `SystemOIDs []string`, `OIDSysUpTime`, `OIDSysName`, `OIDSysDescr`, `OIDSysObjectID` constants.
  - `ParseSystem([]PDU) System`, `ParseInterfaces([]PDU) []Interface`, `ParseEntity([]PDU) (model, serial string)`, `VendorFor(sysObjectID string) string`.
  - `Identify(ctx, c Client, t Target) (System, error)`; `ReadInventory(ctx, c Client, t Target) (Inventory, error)`.
  - `Clean(s string) string` — strips NUL bytes and invalid UTF-8, trims space (Postgres TEXT rejects NUL; agents pad strings with it).

- [ ] **Step 1: Add the dependency**

Run: `cd /home/sysadmin/sentinel/backend && go get github.com/gosnmp/gosnmp@latest && grep gosnmp go.mod`
Expected: a `github.com/gosnmp/gosnmp vX.Y.Z` require line. Record the version in the commit message.

- [ ] **Step 2: Write the failing parser tests**

`backend/internal/snmp/parse_test.go`:

```go
package snmp

import (
	"reflect"
	"testing"
)

func pdu(oid string, v any) PDU { return PDU{OID: oid, Value: v} }

func TestParseSystem(t *testing.T) {
	got := ParseSystem([]PDU{
		pdu(OIDSysDescr, []byte("EdgeSwitch 48-Port 500W, 1.9.3.5\x00")),
		pdu(OIDSysObjectID, "1.3.6.1.4.1.4413"),
		pdu(OIDSysUpTime, uint64(123456)), // hundredths of a second
		pdu(OIDSysContact, []byte("noc@example.test")),
		pdu(OIDSysName, []byte("core-sw-1")),
		pdu(OIDSysLocation, []byte("MDF rack 2")),
	})
	want := System{Descr: "EdgeSwitch 48-Port 500W, 1.9.3.5", ObjectID: "1.3.6.1.4.1.4413", UptimeSeconds: 1234,
		Contact: "noc@example.test", Name: "core-sw-1", Location: "MDF rack 2"}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	// Missing values (noSuchObject) leave fields empty instead of failing.
	if got := ParseSystem([]PDU{pdu(OIDSysName, nil)}); got != (System{}) {
		t.Errorf("noSuchObject: got %+v", got)
	}
}

func TestParseInterfaces(t *testing.T) {
	const tbl, xtbl = "1.3.6.1.2.1.2.2.1", "1.3.6.1.2.1.31.1.1.1"
	got := ParseInterfaces([]PDU{
		// ifIndex 1: gigabit port with ifXTable data and a description.
		pdu(tbl+".1.1", int64(1)), pdu(tbl+".2.1", []byte("Slot: 0 Port: 1 Gigabit - Level")),
		pdu(tbl+".3.1", int64(6)), pdu(tbl+".5.1", uint64(1000000000)),
		pdu(tbl+".6.1", []byte{0x78, 0x8a, 0x20, 0x01, 0x02, 0x03}),
		pdu(tbl+".7.1", int64(1)), pdu(tbl+".8.1", int64(1)), pdu(tbl+".9.1", uint64(4500)),
		pdu(xtbl+".1.1", []byte("0/1")), pdu(xtbl+".15.1", uint64(1000)), pdu(xtbl+".18.1", []byte("Uplink to MDF")),
		// ifIndex 49: 10G port: ifSpeed saturates at 2^32-1, ifHighSpeed is right.
		pdu(tbl+".1.49", int64(49)), pdu(tbl+".2.49", []byte("Slot: 0 Port: 49 10G - Level")),
		pdu(tbl+".5.49", uint64(4294967295)), pdu(xtbl+".15.49", uint64(10000)),
		pdu(tbl+".7.49", int64(2)), pdu(tbl+".8.49", int64(7)),
		// ifIndex 3: a v1 device with no ifXTable: name falls back to ifDescr,
		// speed to ifSpeed.
		pdu(tbl+".1.3", int64(3)), pdu(tbl+".2.3", []byte("eth0")), pdu(tbl+".5.3", uint64(100000000)),
		pdu(tbl+".7.3", int64(1)), pdu(tbl+".8.3", int64(2)),
	})
	want := []Interface{
		{Index: 1, Name: "0/1", Descr: "Slot: 0 Port: 1 Gigabit - Level", Alias: "Uplink to MDF", Type: 6,
			SpeedBps: 1_000_000_000, MAC: "78:8a:20:01:02:03", AdminStatus: "up", OperStatus: "up", LastChangeSeconds: 45},
		{Index: 3, Name: "eth0", Descr: "eth0", SpeedBps: 100_000_000, AdminStatus: "up", OperStatus: "down"},
		{Index: 49, Name: "Slot: 0 Port: 49 10G - Level", Descr: "Slot: 0 Port: 49 10G - Level",
			SpeedBps: 10_000_000_000, AdminStatus: "down", OperStatus: "lowerLayerDown"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseEntity(t *testing.T) {
	const e = "1.3.6.1.2.1.47.1.1.1.1"
	// A stack: entry 1 is a module (class 9) listed before the chassis (3).
	model, serial := ParseEntity([]PDU{
		pdu(e+".5.1", int64(9)), pdu(e+".13.1", []byte("SFP-10G")), pdu(e+".11.1", []byte("MOD1")),
		pdu(e+".5.2", int64(3)), pdu(e+".13.2", []byte("ES-48-500W")), pdu(e+".11.2", []byte("F09FC2AABBCC")),
	})
	if model != "ES-48-500W" || serial != "F09FC2AABBCC" {
		t.Errorf("chassis: got %q %q", model, serial)
	}
	// No chassis entry: the first entry with a model name.
	model, serial = ParseEntity([]PDU{pdu(e+".5.7", int64(10)), pdu(e+".13.7", []byte("CRS326"))})
	if model != "CRS326" || serial != "" {
		t.Errorf("fallback: got %q %q", model, serial)
	}
	if m, s := ParseEntity(nil); m != "" || s != "" {
		t.Errorf("no ENTITY-MIB: got %q %q", m, s)
	}
}

func TestVendorFor(t *testing.T) {
	for oid, want := range map[string]string{
		"1.3.6.1.4.1.4413":              "Ubiquiti (EdgeSwitch)",
		"1.3.6.1.4.1.9.1.2066":          "Cisco",
		"1.3.6.1.4.1.41112.1.6":         "Ubiquiti",
		"1.3.6.1.4.1.29671.2.103":       "Cisco Meraki",
		"1.3.6.1.4.1.17713.21":          "Cambium Networks",
		".1.3.6.1.4.1.14988.1":          "MikroTik",
		"1.3.6.1.4.1.99999.1":           "Unknown (enterprise 99999)",
		"":                              "",
		"1.3.6.1.2.1.1":                 "",
	} {
		if got := VendorFor(oid); got != want {
			t.Errorf("VendorFor(%q) = %q, want %q", oid, got, want)
		}
	}
}

// Agents pad strings with NUL and some send Latin-1; Postgres TEXT rejects NUL
// bytes and invalid UTF-8, which would fail the whole inventory save.
func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"core\x00-sw\x00\x00": "core-sw",
		"  spaced  ":          "spaced",
		"caf\xe9":              "caf",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/snmp/ 2>&1 | head -3`
Expected: FAIL, `undefined: ParseSystem` (or "no non-test Go files").

- [ ] **Step 4: Write the parsers**

`backend/internal/snmp/parse.go`:

```go
package snmp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// System group (SNMPv2-MIB).
const (
	OIDSysDescr    = "1.3.6.1.2.1.1.1.0"
	OIDSysObjectID = "1.3.6.1.2.1.1.2.0"
	OIDSysUpTime   = "1.3.6.1.2.1.1.3.0"
	OIDSysContact  = "1.3.6.1.2.1.1.4.0"
	OIDSysName     = "1.3.6.1.2.1.1.5.0"
	OIDSysLocation = "1.3.6.1.2.1.1.6.0"
)

// SystemOIDs is the whole system group, in one GET.
var SystemOIDs = []string{OIDSysDescr, OIDSysObjectID, OIDSysUpTime, OIDSysContact, OIDSysName, OIDSysLocation}

const (
	oidIfEntry    = "1.3.6.1.2.1.2.2.1"      // IF-MIB ifTable
	oidIfXEntry   = "1.3.6.1.2.1.31.1.1.1"   // IF-MIB ifXTable
	oidEntPhysEnt = "1.3.6.1.2.1.47.1.1.1.1" // ENTITY-MIB entPhysicalTable
)

// ifTableColumns and ifXTableColumns are walked one column at a time, so the
// traffic counters in the same tables (phase 2) are not fetched here.
var (
	ifTableColumns  = []int{1, 2, 3, 5, 6, 7, 8, 9} // index, descr, type, speed, physAddress, admin, oper, lastChange
	ifXTableColumns = []int{1, 15, 18}              // name, highSpeed, alias
	entityColumns   = []int{5, 11, 13}              // class, serialNum, modelName
)

// System is the SNMPv2-MIB system group. Tagged for the Test connection and
// scan responses.
type System struct {
	Descr         string `json:"descr"`
	ObjectID      string `json:"object_id"`
	Contact       string `json:"contact"`
	Name          string `json:"name"`
	Location      string `json:"location"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// Interface is one IF-MIB interface, merged from ifTable and ifXTable.
type Interface struct {
	Index             int
	Name              string
	Descr             string
	Alias             string
	Type              int
	SpeedBps          int64
	MAC               string
	AdminStatus       string
	OperStatus        string
	LastChangeSeconds int64
}

// Inventory is everything a refresh learns about a device.
type Inventory struct {
	System     System
	Vendor     string
	Model      string
	Serial     string
	Interfaces []Interface
}

// Clean makes an agent's string safe to store: no NUL bytes (Postgres TEXT
// rejects them), valid UTF-8 only, surrounding space trimmed.
func Clean(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ToValidUTF8(s, "")
	return strings.TrimSpace(s)
}

// ParseSystem reads the system group. Missing values leave fields empty.
func ParseSystem(pdus []PDU) System {
	var s System
	for _, p := range pdus {
		switch p.OID {
		case OIDSysDescr:
			s.Descr = Clean(p.Text())
		case OIDSysObjectID:
			s.ObjectID = strings.TrimPrefix(p.Text(), ".")
		case OIDSysUpTime:
			if n, ok := p.Number(); ok {
				s.UptimeSeconds = int64(n / 100)
			}
		case OIDSysContact:
			s.Contact = Clean(p.Text())
		case OIDSysName:
			s.Name = Clean(p.Text())
		case OIDSysLocation:
			s.Location = Clean(p.Text())
		}
	}
	return s
}

var adminStatusNames = map[uint64]string{1: "up", 2: "down", 3: "testing"}
var operStatusNames = map[uint64]string{1: "up", 2: "down", 3: "testing", 4: "unknown", 5: "dormant", 6: "notPresent", 7: "lowerLayerDown"}

// splitColumn splits "<table>.<column>.<index>" into column and index.
func splitColumn(oid, table string) (column, index int, ok bool) {
	rest, found := strings.CutPrefix(oid, table+".")
	if !found {
		return 0, 0, false
	}
	colStr, idxStr, found := strings.Cut(rest, ".")
	if !found || strings.Contains(idxStr, ".") {
		return 0, 0, false
	}
	c, err1 := strconv.Atoi(colStr)
	i, err2 := strconv.Atoi(idxStr)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return c, i, true
}

func formatMAC(b []byte) string {
	if len(b) != 6 {
		return ""
	}
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02x", x)
	}
	return strings.Join(parts, ":")
}

// ParseInterfaces merges walked ifTable and ifXTable columns by ifIndex,
// sorted by index. Name is ifName, falling back to ifDescr; speed is
// ifHighSpeed (Mb/s) when non-zero, else ifSpeed (b/s).
func ParseInterfaces(pdus []PDU) []Interface {
	byIndex := map[int]*Interface{}
	highSpeed := map[int]uint64{}
	get := func(i int) *Interface {
		if byIndex[i] == nil {
			byIndex[i] = &Interface{Index: i}
		}
		return byIndex[i]
	}
	for _, p := range pdus {
		if col, idx, ok := splitColumn(p.OID, oidIfEntry); ok {
			it := get(idx)
			n, _ := p.Number()
			switch col {
			case 2:
				it.Descr = Clean(p.Text())
			case 3:
				it.Type = int(n)
			case 5:
				if it.SpeedBps == 0 {
					it.SpeedBps = int64(n)
				}
			case 6:
				if b, ok := p.Value.([]byte); ok {
					it.MAC = formatMAC(b)
				}
			case 7:
				it.AdminStatus = adminStatusNames[n]
			case 8:
				it.OperStatus = operStatusNames[n]
			case 9:
				it.LastChangeSeconds = int64(n / 100)
			}
			continue
		}
		if col, idx, ok := splitColumn(p.OID, oidIfXEntry); ok {
			it := get(idx)
			switch col {
			case 1:
				it.Name = Clean(p.Text())
			case 15:
				if n, ok := p.Number(); ok {
					highSpeed[idx] = n
				}
			case 18:
				it.Alias = Clean(p.Text())
			}
		}
	}
	out := make([]Interface, 0, len(byIndex))
	for idx, it := range byIndex {
		if hs := highSpeed[idx]; hs > 0 {
			it.SpeedBps = int64(hs) * 1_000_000
		}
		if it.Name == "" {
			it.Name = it.Descr
		}
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// ParseEntity picks the model and serial of the chassis (entPhysicalClass 3),
// or else of the first entry with a model name, from walked ENTITY-MIB columns.
func ParseEntity(pdus []PDU) (model, serial string) {
	type ent struct {
		class         uint64
		model, serial string
	}
	byIndex := map[int]*ent{}
	for _, p := range pdus {
		col, idx, ok := splitColumn(p.OID, oidEntPhysEnt)
		if !ok {
			continue
		}
		e := byIndex[idx]
		if e == nil {
			e = &ent{}
			byIndex[idx] = e
		}
		switch col {
		case 5:
			e.class, _ = p.Number()
		case 11:
			e.serial = Clean(p.Text())
		case 13:
			e.model = Clean(p.Text())
		}
	}
	idxs := make([]int, 0, len(byIndex))
	for i := range byIndex {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	for _, i := range idxs {
		if e := byIndex[i]; e.class == 3 && e.model != "" {
			return e.model, e.serial
		}
	}
	for _, i := range idxs {
		if e := byIndex[i]; e.model != "" {
			return e.model, e.serial
		}
	}
	return "", ""
}

// vendors maps IANA private enterprise numbers to names.
var vendors = map[int]string{
	9: "Cisco", 11: "HP", 674: "Dell", 2636: "Juniper", 4413: "Ubiquiti (EdgeSwitch)",
	4526: "Netgear", 6486: "Alcatel-Lucent", 11863: "TP-Link", 12356: "Fortinet",
	14823: "Aruba", 14988: "MikroTik", 17713: "Cambium Networks", 25053: "Ruckus",
	25461: "Palo Alto Networks", 29671: "Cisco Meraki", 41112: "Ubiquiti",
}

// VendorFor names the manufacturer behind a sysObjectID, or
// "Unknown (enterprise N)"; "" when the OID is not under enterprises.
func VendorFor(sysObjectID string) string {
	rest, ok := strings.CutPrefix(strings.TrimPrefix(sysObjectID, "."), "1.3.6.1.4.1.")
	if !ok {
		return ""
	}
	numStr, _, _ := strings.Cut(rest, ".")
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return ""
	}
	if v, ok := vendors[n]; ok {
		return v
	}
	return fmt.Sprintf("Unknown (enterprise %d)", n)
}

// Identify reads a device's system group.
func Identify(ctx context.Context, c Client, t Target) (System, error) {
	pdus, err := c.Get(ctx, t, SystemOIDs)
	if err != nil {
		return System{}, err
	}
	return ParseSystem(pdus), nil
}

// ReadInventory reads identity, model/serial and interfaces. Only a failure
// of the system group or ifTable is an error: ifXTable (absent on v1 and some
// radios) and ENTITY-MIB (absent on many small devices) are optional.
func ReadInventory(ctx context.Context, c Client, t Target) (Inventory, error) {
	sys, err := Identify(ctx, c, t)
	if err != nil {
		return Inventory{}, fmt.Errorf("reading system group: %w", err)
	}
	inv := Inventory{System: sys, Vendor: VendorFor(sys.ObjectID)}

	var ifPDUs []PDU
	for _, col := range ifTableColumns {
		p, err := c.Walk(ctx, t, fmt.Sprintf("%s.%d", oidIfEntry, col))
		if err != nil {
			return Inventory{}, fmt.Errorf("walking ifTable: %w", err)
		}
		ifPDUs = append(ifPDUs, p...)
	}
	for _, col := range ifXTableColumns {
		if p, err := c.Walk(ctx, t, fmt.Sprintf("%s.%d", oidIfXEntry, col)); err == nil {
			ifPDUs = append(ifPDUs, p...)
		}
	}
	inv.Interfaces = ParseInterfaces(ifPDUs)

	var entPDUs []PDU
	for _, col := range entityColumns {
		if p, err := c.Walk(ctx, t, fmt.Sprintf("%s.%d", oidEntPhysEnt, col)); err == nil {
			entPDUs = append(entPDUs, p...)
		}
	}
	inv.Model, inv.Serial = ParseEntity(entPDUs)
	return inv, nil
}
```

- [ ] **Step 5: Write the client and its test**

`backend/internal/snmp/client_test.go`:

```go
package snmp

import (
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func TestBuildGoSNMP(t *testing.T) {
	base := Target{Host: "10.0.0.2", Port: 161, Timeout: 3 * time.Second, Retries: 1}

	v2 := base
	v2.Credential = Credential{Version: "2c", Community: "public"}
	g, err := buildGoSNMP(v2)
	if err != nil || g.Version != gosnmp.Version2c || g.Community != "public" || g.Port != 161 || g.Retries != 1 {
		t.Fatalf("v2c: %+v %v", g, err)
	}

	v1 := base
	v1.Credential = Credential{Version: "1", Community: "radios"}
	if g, err := buildGoSNMP(v1); err != nil || g.Version != gosnmp.Version1 {
		t.Fatalf("v1: %+v %v", g, err)
	}

	cases := []struct {
		cred  Credential
		flags gosnmp.SnmpV3MsgFlags
		auth  gosnmp.SnmpV3AuthProtocol
		priv  gosnmp.SnmpV3PrivProtocol
	}{
		{Credential{Version: "3", Username: "mon", AuthProtocol: "none", PrivProtocol: "none"}, gosnmp.NoAuthNoPriv, gosnmp.NoAuth, gosnmp.NoPriv},
		{Credential{Version: "3", Username: "mon", AuthProtocol: "SHA256", AuthPassword: "longenough", PrivProtocol: "none"}, gosnmp.AuthNoPriv, gosnmp.SHA256, gosnmp.NoPriv},
		{Credential{Version: "3", Username: "mon", AuthProtocol: "MD5", AuthPassword: "longenough", PrivProtocol: "DES", PrivPassword: "longenough"}, gosnmp.AuthPriv, gosnmp.MD5, gosnmp.DES},
		{Credential{Version: "3", Username: "mon", AuthProtocol: "SHA512", AuthPassword: "longenough", PrivProtocol: "AES256", PrivPassword: "longenough"}, gosnmp.AuthPriv, gosnmp.SHA512, gosnmp.AES256},
	}
	for _, tc := range cases {
		tg := base
		tg.Credential = tc.cred
		g, err := buildGoSNMP(tg)
		if err != nil {
			t.Fatalf("%+v: %v", tc.cred, err)
		}
		usm := g.SecurityParameters.(*gosnmp.UsmSecurityParameters)
		if g.Version != gosnmp.Version3 || g.MsgFlags != tc.flags || usm.UserName != "mon" ||
			usm.AuthenticationProtocol != tc.auth || usm.PrivacyProtocol != tc.priv {
			t.Errorf("%+v: flags %v auth %v priv %v", tc.cred, g.MsgFlags, usm.AuthenticationProtocol, usm.PrivacyProtocol)
		}
	}

	bad := base
	bad.Credential = Credential{Version: "3", Username: "mon", AuthProtocol: "SHA1"}
	if _, err := buildGoSNMP(bad); err == nil {
		t.Error("unknown auth protocol accepted")
	}
	bad.Credential = Credential{Version: "4"}
	if _, err := buildGoSNMP(bad); err == nil {
		t.Error("unknown version accepted")
	}
}

func TestToPDU(t *testing.T) {
	for _, tc := range []struct {
		in   gosnmp.SnmpPDU
		want any
	}{
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("sw")}, []byte("sw")},
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.1.2.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.9"}, "1.3.6.1.4.1.9"},
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(500)}, uint64(500)},
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.2.2.1.8.1", Type: gosnmp.Integer, Value: 1}, int64(1)},
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.31.1.1.1.6.1", Type: gosnmp.Counter64, Value: uint64(1 << 40)}, uint64(1 << 40)},
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.1.9.0", Type: gosnmp.NoSuchObject, Value: nil}, nil},
	} {
		got := toPDU(tc.in)
		if got.OID[0] == '.' {
			t.Errorf("OID kept its leading dot: %q", got.OID)
		}
		switch want := tc.want.(type) {
		case []byte:
			if string(got.Value.([]byte)) != string(want) {
				t.Errorf("%s: %v", tc.in.Name, got.Value)
			}
		default:
			if got.Value != tc.want {
				t.Errorf("%s: got %#v want %#v", tc.in.Name, got.Value, tc.want)
			}
		}
	}
}
```

`backend/internal/snmp/client.go`:

```go
// Package snmp talks SNMP v1/v2c/v3 through gosnmp and interprets the
// answers. Everything that interprets is pure and tested without a network;
// the network lives behind Client.
package snmp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// Credential is a decrypted credential profile, held in memory only.
type Credential struct {
	Version      string // "1", "2c" or "3"
	Community    string
	Username     string
	AuthProtocol string
	AuthPassword string
	PrivProtocol string
	PrivPassword string
}

// Target is one device to talk to.
type Target struct {
	Host       string
	Port       uint16
	Credential Credential
	Timeout    time.Duration
	Retries    int
}

// PDU is one variable binding. OID has no leading dot. Value is []byte,
// int64, uint64, string (OID or IP address) or nil (no such object/instance,
// end of MIB view, NULL).
type PDU struct {
	OID   string
	Value any
}

// Text renders the value as a string.
func (p PDU) Text() string {
	switch v := p.Value.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	case int64:
		return fmt.Sprint(v)
	case uint64:
		return fmt.Sprint(v)
	default:
		return ""
	}
}

// Number returns a numeric value (negative INTEGERs are not numbers here).
func (p PDU) Number() (uint64, bool) {
	switch v := p.Value.(type) {
	case uint64:
		return v, true
	case int64:
		if v >= 0 {
			return uint64(v), true
		}
	}
	return 0, false
}

// Client is the network side. GoSNMPClient is the real one; tests use fakes.
type Client interface {
	Get(ctx context.Context, t Target, oids []string) ([]PDU, error)
	Walk(ctx context.Context, t Target, root string) ([]PDU, error)
}

var authProtocols = map[string]gosnmp.SnmpV3AuthProtocol{
	"none": gosnmp.NoAuth, "MD5": gosnmp.MD5, "SHA": gosnmp.SHA, "SHA224": gosnmp.SHA224,
	"SHA256": gosnmp.SHA256, "SHA384": gosnmp.SHA384, "SHA512": gosnmp.SHA512,
}

// AES192/AES256 map to gosnmp's Blumenthal key extension (RFC draft used by
// Net-SNMP). Some vendors (notably Cisco) use the Reeder extension instead
// (gosnmp.AES192C/AES256C); if a device answers snmpwalk with AES-192/256 but
// not Sentinel, that is why, and a "C" variant is the fix.
var privProtocols = map[string]gosnmp.SnmpV3PrivProtocol{
	"none": gosnmp.NoPriv, "DES": gosnmp.DES, "AES": gosnmp.AES, "AES192": gosnmp.AES192, "AES256": gosnmp.AES256,
}

func buildGoSNMP(t Target) (*gosnmp.GoSNMP, error) {
	g := &gosnmp.GoSNMP{
		Target:         t.Host,
		Port:           t.Port,
		Timeout:        t.Timeout,
		Retries:        t.Retries,
		MaxOids:        gosnmp.MaxOids,
		MaxRepetitions: 25,
	}
	c := t.Credential
	switch c.Version {
	case "1":
		g.Version, g.Community = gosnmp.Version1, c.Community
	case "2c":
		g.Version, g.Community = gosnmp.Version2c, c.Community
	case "3":
		auth, ok := authProtocols[c.AuthProtocol]
		if !ok {
			return nil, fmt.Errorf("unknown auth protocol %q", c.AuthProtocol)
		}
		priv, ok := privProtocols[c.PrivProtocol]
		if !ok {
			return nil, fmt.Errorf("unknown privacy protocol %q", c.PrivProtocol)
		}
		usm := &gosnmp.UsmSecurityParameters{UserName: c.Username, AuthenticationProtocol: auth, PrivacyProtocol: priv}
		flags := gosnmp.NoAuthNoPriv
		if auth != gosnmp.NoAuth {
			usm.AuthenticationPassphrase = c.AuthPassword
			flags = gosnmp.AuthNoPriv
		}
		if priv != gosnmp.NoPriv {
			usm.PrivacyPassphrase = c.PrivPassword
			flags = gosnmp.AuthPriv
		}
		g.Version = gosnmp.Version3
		g.SecurityModel = gosnmp.UserSecurityModel
		g.MsgFlags = flags
		g.SecurityParameters = usm
	default:
		return nil, fmt.Errorf("unknown SNMP version %q", c.Version)
	}
	return g, nil
}

func toPDU(v gosnmp.SnmpPDU) PDU {
	p := PDU{OID: strings.TrimPrefix(v.Name, ".")}
	switch v.Type {
	case gosnmp.OctetString, gosnmp.Opaque:
		if b, ok := v.Value.([]byte); ok {
			p.Value = b
		}
	case gosnmp.ObjectIdentifier:
		if s, ok := v.Value.(string); ok {
			p.Value = strings.TrimPrefix(s, ".")
		}
	case gosnmp.IPAddress:
		if s, ok := v.Value.(string); ok {
			p.Value = s
		}
	case gosnmp.Integer:
		p.Value = gosnmp.ToBigInt(v.Value).Int64()
	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks, gosnmp.Counter64, gosnmp.Uinteger32:
		p.Value = gosnmp.ToBigInt(v.Value).Uint64()
	default: // NoSuchObject, NoSuchInstance, EndOfMibView, Null
		p.Value = nil
	}
	return p
}

// GoSNMPClient is the real Client. It opens a UDP socket per call; a poll is
// one or a handful of requests, so pooling sockets is not worth its state.
type GoSNMPClient struct{}

func (GoSNMPClient) connect(ctx context.Context, t Target) (*gosnmp.GoSNMP, error) {
	g, err := buildGoSNMP(t)
	if err != nil {
		return nil, err
	}
	g.Context = ctx
	if err := g.Connect(); err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", t.Host, err)
	}
	return g, nil
}

// Get fetches the given OIDs in one request.
func (c GoSNMPClient) Get(ctx context.Context, t Target, oids []string) ([]PDU, error) {
	g, err := c.connect(ctx, t)
	if err != nil {
		return nil, err
	}
	defer g.Conn.Close()
	pkt, err := g.Get(oids)
	if err != nil {
		return nil, err
	}
	if pkt.Error != gosnmp.NoError {
		return nil, fmt.Errorf("agent returned %s", pkt.Error)
	}
	out := make([]PDU, 0, len(pkt.Variables))
	for _, v := range pkt.Variables {
		out = append(out, toPDU(v))
	}
	return out, nil
}

// Walk returns every binding under root: GETBULK on v2c/v3, GETNEXT on v1
// (which has no GETBULK).
func (c GoSNMPClient) Walk(ctx context.Context, t Target, root string) ([]PDU, error) {
	g, err := c.connect(ctx, t)
	if err != nil {
		return nil, err
	}
	defer g.Conn.Close()
	var vars []gosnmp.SnmpPDU
	if g.Version == gosnmp.Version1 {
		vars, err = g.WalkAll(root)
	} else {
		vars, err = g.BulkWalkAll(root)
	}
	if err != nil {
		return nil, err
	}
	out := make([]PDU, 0, len(vars))
	for _, v := range vars {
		out = append(out, toPDU(v))
	}
	return out, nil
}
```

If `go vet` reports that a gosnmp identifier does not exist in the fetched version (for example `gosnmp.Uinteger32` or `gosnmp.MaxOids`), check `go doc github.com/gosnmp/gosnmp <Name>` and use the name that version exports; do not drop the case.

- [ ] **Step 6: Run the package tests**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./internal/snmp/ && go test ./internal/snmp/ -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `TestParseSystem`, `TestParseInterfaces`, `TestParseEntity`, `TestVendorFor`, `TestClean`, `TestBuildGoSNMP`, `TestToPDU` all PASS.

- [ ] **Step 7: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/go.mod backend/go.sum backend/internal/snmp/
git commit -m "feat(snmp): gosnmp client for v1/v2c/v3 and pure MIB-II/ENTITY parsers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Device state machine and poller

**Files:**
- Create: `backend/internal/services/device_state.go`, `backend/internal/services/device_state_test.go`
- Create: `backend/internal/services/device_poller.go`, `backend/internal/services/device_poller_test.go`

**Interfaces:**
- Consumes: `snmp.Client`, `snmp.Target`, `snmp.Credential`, `snmp.OIDSysUpTime`, `snmp.ReadInventory`, `snmp.Inventory` (Task 4); `IncidentService.OpenDeviceIncident/CloseDeviceIncident` (Task 3); `notifications.NotificationMessage` with `DeviceID`, `SiteName` (Task 3); `models.Device`, status constants (Task 2); `netguard.IsBlocked`.
- Produces:
  - `type PollResult int` with `PollOK`, `PollFailed`, `PollBlocked`, `PollPaused`.
  - `type DeviceAction string` with `ActionOpenIncident`, `ActionCloseIncident`, `ActionNotifyDown`, `ActionNotifyRecovered`.
  - `const DeviceDownThreshold = 3`.
  - `type DeviceTransition struct { Status string; Failures int; Actions []DeviceAction }`.
  - `func NextDeviceState(status string, failures int, result PollResult) DeviceTransition`.
  - `type ReachabilityUpdate struct { Status string; Failures int; Detail string; PolledAt time.Time; SeenAt *time.Time; UptimeSeconds *int64 }`.
  - `type PollerStore interface { DueDevices(ctx, now time.Time, limit int) ([]models.Device, error); DeviceCredential(ctx, credentialID uuid.UUID) (snmp.Credential, error); SiteName(ctx, siteID uuid.UUID) (string, error); SaveReachability(ctx, deviceID uuid.UUID, u ReachabilityUpdate) error; SaveInventory(ctx, deviceID uuid.UUID, inv snmp.Inventory, at time.Time) error; SaveInventoryError(ctx, deviceID uuid.UUID, detail string, at time.Time) error }` — implemented by `DeviceService` in Task 7.
  - `type DeviceIncidents interface { OpenDeviceIncident(...); CloseDeviceIncident(...) }` and `type Notifier interface { SendNotification(ctx, *notifications.NotificationMessage) error }`.
  - `func NewDevicePoller(store PollerStore, client snmp.Client, incidents DeviceIncidents, notifier Notifier, workers int) *DevicePoller`; `(*DevicePoller).Start(ctx)`; `(*DevicePoller).PollOnce(ctx, d models.Device)`; `(*DevicePoller).Dispatch(ctx, jobs chan<- models.Device) int`.
  - `func TargetFor(d models.Device, host string, cred snmp.Credential) snmp.Target`.

- [ ] **Step 1: Write the failing state machine test**

`backend/internal/services/device_state_test.go`:

```go
package services

import (
	"reflect"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestNextDeviceState(t *testing.T) {
	type A = []DeviceAction
	cases := []struct {
		name     string
		status   string
		failures int
		result   PollResult
		want     DeviceTransition
	}{
		{"first success", models.DeviceStatusPending, 0, PollOK, DeviceTransition{models.DeviceStatusUp, 0, nil}},
		{"success stays up", models.DeviceStatusUp, 0, PollOK, DeviceTransition{models.DeviceStatusUp, 0, nil}},
		{"success after one failure", models.DeviceStatusUp, 1, PollOK, DeviceTransition{models.DeviceStatusUp, 0, nil}},
		{"one failure", models.DeviceStatusUp, 0, PollFailed, DeviceTransition{models.DeviceStatusUp, 1, nil}},
		{"two failures", models.DeviceStatusUp, 1, PollFailed, DeviceTransition{models.DeviceStatusUp, 2, nil}},
		{"third failure goes down", models.DeviceStatusUp, 2, PollFailed,
			DeviceTransition{models.DeviceStatusDown, 3, A{ActionOpenIncident, ActionNotifyDown}}},
		{"pending device never answering goes down", models.DeviceStatusPending, 2, PollFailed,
			DeviceTransition{models.DeviceStatusDown, 3, A{ActionOpenIncident, ActionNotifyDown}}},
		{"further failures while down do nothing", models.DeviceStatusDown, 3, PollFailed,
			DeviceTransition{models.DeviceStatusDown, 4, nil}},
		{"recovery", models.DeviceStatusDown, 7, PollOK,
			DeviceTransition{models.DeviceStatusUp, 0, A{ActionCloseIncident, ActionNotifyRecovered}}},
		{"blocked is a configuration error", models.DeviceStatusUp, 1, PollBlocked, DeviceTransition{models.DeviceStatusError, 1, nil}},
		{"blocked while down closes quietly", models.DeviceStatusDown, 3, PollBlocked,
			DeviceTransition{models.DeviceStatusError, 3, A{ActionCloseIncident}}},
		{"failing after an error starts counting as pending", models.DeviceStatusError, 0, PollFailed,
			DeviceTransition{models.DeviceStatusPending, 1, nil}},
		{"pause", models.DeviceStatusUp, 1, PollPaused, DeviceTransition{models.DeviceStatusPaused, 0, nil}},
		{"pause while down closes the incident", models.DeviceStatusDown, 4, PollPaused,
			DeviceTransition{models.DeviceStatusPaused, 0, A{ActionCloseIncident}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextDeviceState(tc.status, tc.failures, tc.result)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/services/ -run TestNextDeviceState 2>&1 | head -3`
Expected: FAIL, `undefined: DeviceAction`.

- [ ] **Step 3: Write the state machine**

`backend/internal/services/device_state.go`:

```go
package services

import "github.com/Stevy2191/Sentinel/backend/internal/models"

// PollResult is what one reachability poll found.
type PollResult int

const (
	// PollOK: the device answered.
	PollOK PollResult = iota
	// PollFailed: timeout, no answer, or an SNMP error (wrong community, bad
	// v3 credentials). Counts towards down.
	PollFailed
	// PollBlocked: a configuration problem (host does not resolve, address
	// blocked by network policy, credential unusable). Not an outage.
	PollBlocked
	// PollPaused: monitoring was switched off.
	PollPaused
)

// DeviceAction is a side effect a transition asks for.
type DeviceAction string

const (
	ActionOpenIncident    DeviceAction = "open_incident"
	ActionCloseIncident   DeviceAction = "close_incident"
	ActionNotifyDown      DeviceAction = "notify_down"
	ActionNotifyRecovered DeviceAction = "notify_recovered"
)

// DeviceDownThreshold is how many consecutive failed polls mark a device
// down, the same as monitors.
const DeviceDownThreshold = 3

// DeviceTransition is a device's next state and what must happen because of it.
type DeviceTransition struct {
	Status   string
	Failures int
	Actions  []DeviceAction
}

// NextDeviceState is every up/down rule, with no I/O, so each transition is
// tested directly. Only the third consecutive failure opens an incident, and
// only recovery from down notifies; a blocked or paused device is not an
// outage, so any open incident is closed without a recovery notice.
func NextDeviceState(status string, failures int, result PollResult) DeviceTransition {
	switch result {
	case PollOK:
		if status == models.DeviceStatusDown {
			return DeviceTransition{models.DeviceStatusUp, 0, []DeviceAction{ActionCloseIncident, ActionNotifyRecovered}}
		}
		return DeviceTransition{models.DeviceStatusUp, 0, nil}
	case PollFailed:
		n := failures + 1
		if status == models.DeviceStatusDown {
			return DeviceTransition{models.DeviceStatusDown, n, nil}
		}
		if n >= DeviceDownThreshold {
			return DeviceTransition{models.DeviceStatusDown, n, []DeviceAction{ActionOpenIncident, ActionNotifyDown}}
		}
		next := status
		if next == models.DeviceStatusError || next == models.DeviceStatusPaused {
			next = models.DeviceStatusPending
		}
		return DeviceTransition{next, n, nil}
	case PollBlocked:
		if status == models.DeviceStatusDown {
			return DeviceTransition{models.DeviceStatusError, failures, []DeviceAction{ActionCloseIncident}}
		}
		return DeviceTransition{models.DeviceStatusError, failures, nil}
	default: // PollPaused
		if status == models.DeviceStatusDown {
			return DeviceTransition{models.DeviceStatusPaused, 0, []DeviceAction{ActionCloseIncident}}
		}
		return DeviceTransition{models.DeviceStatusPaused, 0, nil}
	}
}
```

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/services/ -run TestNextDeviceState -v 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 4: Write the failing poller tests**

`backend/internal/services/device_poller_test.go`:

```go
package services

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type fakeSNMP struct {
	getErr  error
	walkErr error
	gets    int
	walks   int
}

func (f *fakeSNMP) Get(_ context.Context, _ snmp.Target, oids []string) ([]snmp.PDU, error) {
	f.gets++
	if f.getErr != nil {
		return nil, f.getErr
	}
	out := []snmp.PDU{}
	for _, o := range oids {
		switch o {
		case snmp.OIDSysUpTime:
			out = append(out, snmp.PDU{OID: o, Value: uint64(360000)})
		case snmp.OIDSysName:
			out = append(out, snmp.PDU{OID: o, Value: []byte("core-sw-1")})
		case snmp.OIDSysObjectID:
			out = append(out, snmp.PDU{OID: o, Value: "1.3.6.1.4.1.4413"})
		}
	}
	return out, nil
}

func (f *fakeSNMP) Walk(_ context.Context, _ snmp.Target, root string) ([]snmp.PDU, error) {
	f.walks++
	if f.walkErr != nil {
		return nil, f.walkErr
	}
	if root == "1.3.6.1.2.1.2.2.1.1" {
		return []snmp.PDU{{OID: root + ".1", Value: int64(1)}}, nil
	}
	return nil, nil
}

type fakeStore struct {
	mu        sync.Mutex
	due       []models.Device
	saved     []ReachabilityUpdate
	inventory []snmp.Inventory
	invErrors []string
}

func (s *fakeStore) DueDevices(context.Context, time.Time, int) ([]models.Device, error) { return s.due, nil }
func (s *fakeStore) DeviceCredential(context.Context, uuid.UUID) (snmp.Credential, error) {
	return snmp.Credential{Version: "2c", Community: "public"}, nil
}
func (s *fakeStore) SiteName(context.Context, uuid.UUID) (string, error) { return "Warehouse", nil }
func (s *fakeStore) SaveReachability(_ context.Context, _ uuid.UUID, u ReachabilityUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved = append(s.saved, u)
	return nil
}
func (s *fakeStore) SaveInventory(_ context.Context, _ uuid.UUID, inv snmp.Inventory, _ time.Time) error {
	s.inventory = append(s.inventory, inv)
	return nil
}
func (s *fakeStore) SaveInventoryError(_ context.Context, _ uuid.UUID, detail string, _ time.Time) error {
	s.invErrors = append(s.invErrors, detail)
	return nil
}

type fakeDeviceIncidents struct{ opened, closed int }

func (f *fakeDeviceIncidents) OpenDeviceIncident(_ context.Context, id uuid.UUID, start time.Time, _ string) (*models.Incident, bool, error) {
	f.opened++
	return &models.Incident{ID: uuid.New(), DeviceID: &id, StartTime: start}, true, nil
}
func (f *fakeDeviceIncidents) CloseDeviceIncident(_ context.Context, id uuid.UUID, end time.Time, _ string) (*models.Incident, error) {
	f.closed++
	return &models.Incident{ID: uuid.New(), DeviceID: &id, StartTime: end.Add(-12 * time.Minute), EndTime: &end, DurationSeconds: 720}, nil
}

type fakeNotifier struct{ sent []*notifications.NotificationMessage }

func (f *fakeNotifier) SendNotification(_ context.Context, m *notifications.NotificationMessage) error {
	f.sent = append(f.sent, m)
	return nil
}

func newTestPoller(client *fakeSNMP) (*DevicePoller, *fakeStore, *fakeDeviceIncidents, *fakeNotifier) {
	store, inc, notif := &fakeStore{}, &fakeDeviceIncidents{}, &fakeNotifier{}
	p := NewDevicePoller(store, client, inc, notif, 2)
	p.resolve = func(context.Context, string) (net.IP, error) { return net.ParseIP("10.20.0.2"), nil }
	p.blocked = func(net.IP) bool { return false }
	now := time.Date(2026, 9, 29, 14, 20, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	return p, store, inc, notif
}

func device(status string, failures int) models.Device {
	seen := time.Date(2026, 9, 29, 14, 2, 0, 0, time.UTC)
	inv := time.Date(2026, 9, 29, 14, 15, 0, 0, time.UTC) // fresh: no inventory due
	return models.Device{ID: uuid.New(), SiteID: uuid.New(), Name: "core-sw-1", Host: "10.20.0.2", Port: 161,
		Enabled: true, TimeoutMs: 3000, Retries: 1, Status: status, ConsecutiveFailures: failures,
		LastSeenAt: &seen, LastInventoryAt: &inv}
}

func TestPollerThirdFailureOpensIncidentAndNotifies(t *testing.T) {
	p, store, inc, notif := newTestPoller(&fakeSNMP{getErr: errors.New("request timeout")})
	p.PollOnce(context.Background(), device(models.DeviceStatusUp, 2))

	if len(store.saved) != 1 || store.saved[0].Status != models.DeviceStatusDown || store.saved[0].Failures != 3 ||
		store.saved[0].SeenAt != nil || store.saved[0].Detail != "request timeout" {
		t.Fatalf("saved %+v", store.saved)
	}
	if inc.opened != 1 || len(notif.sent) != 1 {
		t.Fatalf("opened %d incidents, sent %d notifications; want 1, 1", inc.opened, len(notif.sent))
	}
	m := notif.sent[0]
	if m.Status != "down" || m.DeviceID == nil || m.SiteName != "Warehouse" || m.MonitorURL != "10.20.0.2" ||
		m.IncidentID == nil || m.Message != "core-sw-1 at Warehouse (10.20.0.2) has stopped answering SNMP. Last answered 2026-09-29 14:02 UTC." {
		t.Errorf("message %+v", m)
	}
}

func TestPollerRecoveryClosesAndNotifies(t *testing.T) {
	p, store, inc, notif := newTestPoller(&fakeSNMP{})
	p.PollOnce(context.Background(), device(models.DeviceStatusDown, 5))
	if store.saved[0].Status != models.DeviceStatusUp || store.saved[0].SeenAt == nil ||
		store.saved[0].UptimeSeconds == nil || *store.saved[0].UptimeSeconds != 3600 {
		t.Fatalf("saved %+v", store.saved[0])
	}
	if inc.closed != 1 || len(notif.sent) != 1 || notif.sent[0].Status != "recovered" ||
		notif.sent[0].Message != "core-sw-1 at Warehouse is answering again after 12 minutes." {
		t.Fatalf("closed %d, sent %+v", inc.closed, notif.sent)
	}
}

// [] means "no channels": the incident still opens, nothing is sent. null
// means every enabled channel, passed through as nil.
func TestPollerNotificationChannels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		channels models.StringSlice
		wantSent bool
	}{
		{"none chosen", models.StringSlice{}, false},
		{"all channels", nil, true},
		{"one channel", models.StringSlice{"ops-slack"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, inc, notif := newTestPoller(&fakeSNMP{getErr: errors.New("timeout")})
			d := device(models.DeviceStatusUp, 2)
			d.NotifyChannels = tc.channels
			p.PollOnce(context.Background(), d)
			if inc.opened != 1 {
				t.Errorf("incident not opened")
			}
			if (len(notif.sent) == 1) != tc.wantSent {
				t.Fatalf("sent %d, want sent=%v", len(notif.sent), tc.wantSent)
			}
			if tc.wantSent && (len(notif.sent[0].Channels) != len(tc.channels) || (tc.channels == nil) != (notif.sent[0].Channels == nil)) {
				t.Errorf("channels %#v, want %#v", notif.sent[0].Channels, tc.channels)
			}
		})
	}
}

func TestPollerBlockedIsAnErrorNotAnOutage(t *testing.T) {
	client := &fakeSNMP{}
	p, store, inc, notif := newTestPoller(client)
	p.blocked = func(net.IP) bool { return true }
	p.PollOnce(context.Background(), device(models.DeviceStatusUp, 0))
	if store.saved[0].Status != models.DeviceStatusError || client.gets != 0 || inc.opened != 0 || len(notif.sent) != 0 {
		t.Fatalf("saved %+v, gets %d, opened %d", store.saved, client.gets, inc.opened)
	}
	if store.saved[0].Detail == "" {
		t.Error("blocked device has no explanation")
	}
}

func TestPollerInventoryOnlyWhenDue(t *testing.T) {
	client := &fakeSNMP{}
	p, store, _, _ := newTestPoller(client)
	p.PollOnce(context.Background(), device(models.DeviceStatusUp, 0)) // inventory 5 minutes old
	if len(store.inventory) != 0 || client.walks != 0 {
		t.Fatalf("fresh inventory was re-read")
	}
	d := device(models.DeviceStatusUp, 0)
	d.LastInventoryAt = nil
	p.PollOnce(context.Background(), d)
	if len(store.inventory) != 1 || store.inventory[0].System.Name != "core-sw-1" || len(store.inventory[0].Interfaces) != 1 {
		t.Fatalf("inventory %+v", store.inventory)
	}
}

// A failed inventory is recorded but never changes up/down.
func TestPollerInventoryFailureKeepsDeviceUp(t *testing.T) {
	p, store, inc, _ := newTestPoller(&fakeSNMP{walkErr: errors.New("walk timeout")})
	d := device(models.DeviceStatusUp, 0)
	d.LastInventoryAt = nil
	p.PollOnce(context.Background(), d)
	if store.saved[0].Status != models.DeviceStatusUp || len(store.invErrors) != 1 || inc.opened != 0 {
		t.Fatalf("saved %+v inv errors %v", store.saved, store.invErrors)
	}
}

// A device already being polled is not queued again, so a slow link cannot
// pile up duplicate polls.
func TestDispatchSkipsDevicesInFlight(t *testing.T) {
	p, store, _, _ := newTestPoller(&fakeSNMP{})
	a, b := device(models.DeviceStatusUp, 0), device(models.DeviceStatusUp, 0)
	store.due = []models.Device{a, b}
	p.inFlight.Store(a.ID, true)
	jobs := make(chan models.Device, 10)
	if n := p.Dispatch(context.Background(), jobs); n != 1 {
		t.Fatalf("dispatched %d, want 1", n)
	}
	if got := <-jobs; got.ID != b.ID {
		t.Errorf("dispatched %s, want %s", got.ID, b.ID)
	}
	if n := p.Dispatch(context.Background(), jobs); n != 0 {
		t.Errorf("second dispatch queued %d again", n)
	}
}
```

- [ ] **Step 5: Run to verify they fail**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./internal/services/ 2>&1 | head -3`
Expected: `undefined: NewDevicePoller`.

- [ ] **Step 6: Write the poller**

`backend/internal/services/device_poller.go`:

```go
package services

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netguard"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

const (
	pollerTick           = 5 * time.Second
	inventoryInterval    = 15 * time.Minute
	pollerDueBatch       = 1000
	defaultPollerWorkers = 16
)

// ReachabilityUpdate is what one poll writes back to a device.
type ReachabilityUpdate struct {
	Status        string
	Failures      int
	Detail        string
	PolledAt      time.Time
	SeenAt        *time.Time
	UptimeSeconds *int64
}

// PollerStore is the poller's view of the database (DeviceService).
type PollerStore interface {
	DueDevices(ctx context.Context, now time.Time, limit int) ([]models.Device, error)
	DeviceCredential(ctx context.Context, credentialID uuid.UUID) (snmp.Credential, error)
	SiteName(ctx context.Context, siteID uuid.UUID) (string, error)
	SaveReachability(ctx context.Context, deviceID uuid.UUID, u ReachabilityUpdate) error
	SaveInventory(ctx context.Context, deviceID uuid.UUID, inv snmp.Inventory, at time.Time) error
	SaveInventoryError(ctx context.Context, deviceID uuid.UUID, detail string, at time.Time) error
}

// DeviceIncidents opens and closes device incidents (IncidentService).
type DeviceIncidents interface {
	OpenDeviceIncident(ctx context.Context, deviceID uuid.UUID, start time.Time, reason string) (*models.Incident, bool, error)
	CloseDeviceIncident(ctx context.Context, deviceID uuid.UUID, end time.Time, note string) (*models.Incident, error)
}

// Notifier delivers alerts (NotificationManager).
type Notifier interface {
	SendNotification(ctx context.Context, m *notifications.NotificationMessage) error
}

// DevicePoller polls every enabled device for reachability on a bounded
// worker pool, and refreshes inventory when it is due.
type DevicePoller struct {
	store     PollerStore
	client    snmp.Client
	incidents DeviceIncidents
	notifier  Notifier
	workers   int

	// Replaceable in tests.
	resolve func(ctx context.Context, host string) (net.IP, error)
	blocked func(net.IP) bool
	now     func() time.Time

	inFlight sync.Map // device id -> true while queued or polling
	logger   *log.Logger
}

// NewDevicePoller builds a poller; workers <= 0 means the default (16).
func NewDevicePoller(store PollerStore, client snmp.Client, incidents DeviceIncidents, notifier Notifier, workers int) *DevicePoller {
	if workers <= 0 {
		workers = defaultPollerWorkers
	}
	return &DevicePoller{
		store: store, client: client, incidents: incidents, notifier: notifier, workers: workers,
		resolve: resolveHost,
		blocked: netguard.IsBlocked,
		now:     time.Now,
		logger:  log.Default(),
	}
}

// resolveHost returns the first IPv4 address for host (or host itself if it
// is an IP). Resolved on every poll, so a DHCP-reserved name that moves is
// followed.
func resolveHost(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}
	addrs, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if a.To4() != nil {
			return a, nil
		}
	}
	if len(addrs) > 0 {
		return addrs[0], nil
	}
	return nil, fmt.Errorf("%s has no addresses", host)
}

// TargetFor builds the SNMP target for a device at a resolved address.
func TargetFor(d models.Device, host string, cred snmp.Credential) snmp.Target {
	return snmp.Target{
		Host:       host,
		Port:       uint16(d.Port),
		Credential: cred,
		Timeout:    time.Duration(d.TimeoutMs) * time.Millisecond,
		Retries:    d.Retries,
	}
}

// Start runs the scheduler and workers until ctx is cancelled.
func (p *DevicePoller) Start(ctx context.Context) {
	jobs := make(chan models.Device, p.workers*4)
	for i := 0; i < p.workers; i++ {
		go func() {
			for d := range jobs {
				p.PollOnce(ctx, d)
				p.inFlight.Delete(d.ID)
			}
		}()
	}
	p.logger.Printf("[snmp] poller started with %d workers", p.workers)
	t := time.NewTicker(pollerTick)
	defer t.Stop()
	defer close(jobs)
	for {
		select {
		case <-ctx.Done():
			p.logger.Println("[snmp] poller stopped")
			return
		case <-t.C:
			p.Dispatch(ctx, jobs)
		}
	}
}

// Dispatch queues every due device not already in flight, without blocking:
// if the pool is saturated the rest wait for the next tick. Returns how many
// were queued.
func (p *DevicePoller) Dispatch(ctx context.Context, jobs chan<- models.Device) int {
	due, err := p.store.DueDevices(ctx, p.now(), pollerDueBatch)
	if err != nil {
		p.logger.Printf("[snmp] listing due devices: %v", err)
		return 0
	}
	queued := 0
	for _, d := range due {
		if _, busy := p.inFlight.LoadOrStore(d.ID, true); busy {
			continue
		}
		select {
		case jobs <- d:
			queued++
		default:
			p.inFlight.Delete(d.ID)
		}
	}
	return queued
}

// PollOnce polls one device, stores the outcome, applies the state machine's
// actions, and refreshes inventory when it is due.
func (p *DevicePoller) PollOnce(ctx context.Context, d models.Device) {
	now := p.now()
	result, detail := PollOK, ""
	var target snmp.Target
	var uptime *int64

	cred, err := p.store.DeviceCredential(ctx, d.CredentialID)
	if err != nil {
		result, detail = PollBlocked, "credential profile unavailable: "+err.Error()
	} else if ip, err := p.resolve(ctx, d.Host); err != nil {
		result, detail = PollBlocked, "cannot resolve host: "+err.Error()
	} else if p.blocked(ip) {
		result, detail = PollBlocked, fmt.Sprintf("%s is blocked by network policy (ALLOW_PRIVATE_NETWORK_TARGETS)", ip)
	} else {
		target = TargetFor(d, ip.String(), cred)
		pdus, err := p.client.Get(ctx, target, []string{snmp.OIDSysUpTime})
		if err != nil {
			result, detail = PollFailed, err.Error()
		} else if len(pdus) == 1 {
			if n, ok := pdus[0].Number(); ok {
				s := int64(n / 100)
				uptime = &s
			}
		}
	}

	tr := NextDeviceState(d.Status, d.ConsecutiveFailures, result)
	update := ReachabilityUpdate{Status: tr.Status, Failures: tr.Failures, Detail: detail, PolledAt: now, UptimeSeconds: uptime}
	if result == PollOK {
		update.SeenAt = &now
	}
	if err := p.store.SaveReachability(ctx, d.ID, update); err != nil {
		p.logger.Printf("[snmp] saving poll of %s: %v", d.Host, err)
		return
	}
	p.apply(ctx, d, tr.Actions, detail, now)

	if result == PollOK && (d.LastInventoryAt == nil || now.Sub(*d.LastInventoryAt) >= inventoryInterval) {
		inv, err := snmp.ReadInventory(ctx, p.client, target)
		if err != nil {
			_ = p.store.SaveInventoryError(ctx, d.ID, "inventory: "+err.Error(), now)
			return
		}
		if err := p.store.SaveInventory(ctx, d.ID, inv, now); err != nil {
			p.logger.Printf("[snmp] saving inventory of %s: %v", d.Host, err)
		}
	}
}

func displayName(d models.Device) string {
	if d.Name != "" {
		return d.Name
	}
	return d.Host
}

func (p *DevicePoller) apply(ctx context.Context, d models.Device, actions []DeviceAction, detail string, now time.Time) {
	var incident *models.Incident
	for _, a := range actions {
		var err error
		switch a {
		case ActionOpenIncident:
			incident, _, err = p.incidents.OpenDeviceIncident(ctx, d.ID, now, detail)
		case ActionCloseIncident:
			note := ""
			if len(actions) == 1 { // closed because blocked or paused, not recovered
				note = "Closed without recovery: " + detail
				if detail == "" {
					note = "Monitoring was paused."
				}
			}
			incident, err = p.incidents.CloseDeviceIncident(ctx, d.ID, now, note)
		case ActionNotifyDown, ActionNotifyRecovered:
			p.notify(ctx, d, a, incident, now)
		}
		if err != nil {
			p.logger.Printf("[snmp] %s for %s: %v", a, d.Host, err)
		}
	}
}

func (p *DevicePoller) notify(ctx context.Context, d models.Device, a DeviceAction, incident *models.Incident, now time.Time) {
	// Existing convention: nil = every enabled channel, [] = none.
	if d.NotifyChannels != nil && len(d.NotifyChannels) == 0 {
		return
	}
	site, err := p.store.SiteName(ctx, d.SiteID)
	if err != nil {
		site = "its site"
	}
	id := d.ID
	m := &notifications.NotificationMessage{
		DeviceID:    &id,
		SiteName:    site,
		MonitorName: displayName(d),
		MonitorURL:  d.Host,
		Timestamp:   now,
	}
	if d.NotifyChannels != nil {
		m.Channels = []string(d.NotifyChannels)
	}
	if incident != nil {
		m.IncidentID = &incident.ID
	}
	if a == ActionNotifyDown {
		last := "never"
		if d.LastSeenAt != nil {
			last = d.LastSeenAt.UTC().Format("2006-01-02 15:04 UTC")
		}
		m.Status, m.PreviousStatus = "down", d.Status
		m.Message = fmt.Sprintf("%s at %s (%s) has stopped answering SNMP. Last answered %s.", displayName(d), site, d.Host, last)
	} else {
		m.Status, m.PreviousStatus = "recovered", models.DeviceStatusDown
		dur := time.Duration(0)
		if incident != nil {
			dur = time.Duration(incident.DurationSeconds) * time.Second
			m.DowntimeDuration = dur
		}
		m.Message = fmt.Sprintf("%s at %s is answering again after %s.", displayName(d), site, humanDuration(dur))
	}
	if err := p.notifier.SendNotification(ctx, m); err != nil {
		p.logger.Printf("[snmp] sending %s notification for %s: %v", m.Status, d.Host, err)
	}
}

// humanDuration renders "12 minutes", "1 minute", "3 hours 5 minutes", "2 days 1 hour".
func humanDuration(d time.Duration) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	mins := int(d.Round(time.Minute) / time.Minute)
	switch {
	case mins < 1:
		return "less than a minute"
	case mins < 60:
		return plural(mins, "minute")
	case mins < 24*60:
		if m := mins % 60; m > 0 {
			return plural(mins/60, "hour") + " " + plural(m, "minute")
		}
		return plural(mins/60, "hour")
	default:
		if h := (mins / 60) % 24; h > 0 {
			return plural(mins/(24*60), "day") + " " + plural(h, "hour")
		}
		return plural(mins/(24*60), "day")
	}
}
```

Note: `apply` decides the close note from the action list: a close that is the only action came from `PollBlocked` (detail set) or `PollPaused` (detail empty); a close paired with `notify_recovered` is a real recovery and gets no note.

- [ ] **Step 7: Run the poller tests**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./internal/services/ && go test ./internal/services/ -run 'TestNextDeviceState|TestPoller|TestDispatch' -race -v 2>&1 | grep -E '^(--- |ok|FAIL|WARNING)'`
Expected: every test PASS, no `WARNING: DATA RACE`.

- [ ] **Step 8: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/internal/services/device_state.go backend/internal/services/device_state_test.go \
  backend/internal/services/device_poller.go backend/internal/services/device_poller_test.go
git commit -m "feat(snmp): reachability poller with a pure up/down state machine

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 6: Credential profiles — service and API

**Files:**
- Create: `backend/internal/services/snmp_credential_service.go`, `backend/internal/services/snmp_credential_db_test.go`, `backend/internal/services/snmp_credential_view_test.go`
- Create: `backend/internal/api/snmp_credential_handler.go`, `backend/internal/api/snmp_credential_handler_test.go`
- Modify: `backend/internal/models/audit.go`

**Interfaces:**
- Consumes: `models.SNMPCredential`, `models.CredentialInput`, `models.NormalizeCredentialInput` (Task 2); `cryptutil.Encrypt/Decrypt`; `snmp.Credential` (Task 4); `isDuplicateKey`, `isForeignKeyViolation`, `ErrSiteNotFound` (phase 0 services).
- Produces:
  - Errors `ErrCredentialNotFound`, `ErrCredentialNameTaken`, `ErrCredentialInUse`, `ErrCredentialSiteMismatch`.
  - `type CredentialView struct { ID uuid.UUID; Name string; SiteID *uuid.UUID; SiteName string; Version, Username, AuthProtocol, PrivProtocol string; HasCommunity, HasAuthPassword, HasPrivPassword bool; UsedBy int64; CreatedAt, UpdatedAt time.Time }` (snake_case JSON).
  - `type CredentialOption struct { ID uuid.UUID; Name, Version string; SiteID *uuid.UUID }`.
  - `func ViewOf(c models.SNMPCredential, usedBy int64, siteName string) CredentialView`.
  - `SNMPCredentialService` with `List(ctx) ([]CredentialView, error)`, `Create(ctx, in models.CredentialInput, by uuid.UUID) (CredentialView, error)`, `Update(ctx, id, in models.CredentialInput) (before, after CredentialView, err error)`, `Delete(ctx, id) (CredentialView, error)`, `ForSite(ctx, siteID) ([]CredentialOption, error)`, `UsableAt(ctx, credentialID, siteID uuid.UUID) (bool, error)`, `Decrypted(ctx, id) (snmp.Credential, error)`.
  - `models.ActionCredentialCreated/Updated/Deleted`, `models.ActionDeviceCreated/Updated/Deleted`, `models.ResourceSNMPCredential = "snmp_credential"`, `models.ResourceDevice = "device"`.
  - `api.RegisterSNMPCredentialRoutes(rg, creds credentialStore, sites siteAccessChecker, audit auditRecorder, users adminChecker)`.

- [ ] **Step 1: Audit constants**

In `backend/internal/models/audit.go`, add to the actions block:

```go
	ActionCredentialCreated = "snmp_credential_created"
	ActionCredentialUpdated = "snmp_credential_updated"
	ActionCredentialDeleted = "snmp_credential_deleted"
	ActionDeviceCreated     = "device_created"
	ActionDeviceUpdated     = "device_updated"
	ActionDeviceDeleted     = "device_deleted"
```

and to the resources block:

```go
	ResourceSNMPCredential = "snmp_credential"
	ResourceDevice         = "device"
```

(Align with `gofmt -w backend/internal/models/audit.go`.)

- [ ] **Step 2: Write the failing tests**

`backend/internal/services/snmp_credential_view_test.go`:

```go
package services

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// Neither the stored model nor the API view may ever carry a secret, in
// plaintext or ciphertext, under any key.
func TestCredentialViewHasNoSecrets(t *testing.T) {
	c := models.SNMPCredential{Name: "v3", Version: "3", Username: "mon",
		Community: "CIPHER-COMMUNITY", AuthProtocol: "SHA", AuthPassword: "CIPHER-AUTH",
		PrivProtocol: "AES", PrivPassword: "CIPHER-PRIV"}
	for name, v := range map[string]any{"model": c, "view": ViewOf(c, 2, "HQ")} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, bad := range []string{"CIPHER-", `"community"`, `"auth_password"`, `"priv_password"`} {
			if strings.Contains(s, bad) {
				t.Errorf("%s JSON contains %s: %s", name, bad, s)
			}
		}
	}
	v := ViewOf(c, 2, "HQ")
	if !v.HasCommunity || !v.HasAuthPassword || !v.HasPrivPassword || v.UsedBy != 2 || v.SiteName != "HQ" {
		t.Errorf("view flags: %+v", v)
	}
}
```

`backend/internal/services/snmp_credential_db_test.go`:

```go
package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func str(s string) *string { return &s }

func TestDBCredentialSecretsEncrypted(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewSNMPCredentialService(db)

	view, err := svc.Create(ctx, models.CredentialInput{Name: "core-v3", Version: "3", Username: "mon",
		AuthProtocol: "SHA256", AuthPassword: str("auth-secret-1"), PrivProtocol: "AES", PrivPassword: str("priv-secret-1")}, uuid.Nil)
	testdb.Must(t, err)

	var raw models.SNMPCredential
	testdb.Must(t, db.First(&raw, "id = ?", view.ID).Error)
	if raw.AuthPassword == "" || raw.AuthPassword == "auth-secret-1" || raw.PrivPassword == "priv-secret-1" {
		t.Fatalf("secrets stored in plaintext or missing: %+v", raw)
	}
	plain, err := svc.Decrypted(ctx, view.ID)
	testdb.Must(t, err)
	if plain.AuthPassword != "auth-secret-1" || plain.PrivPassword != "priv-secret-1" || plain.Username != "mon" || plain.Version != "3" {
		t.Errorf("decrypted %+v", plain)
	}

	// A blank secret on update keeps the stored one.
	_, after, err := svc.Update(ctx, view.ID, models.CredentialInput{Name: "core-v3", Version: "3", Username: "mon2",
		AuthProtocol: "SHA256", PrivProtocol: "AES"})
	testdb.Must(t, err)
	plain, _ = svc.Decrypted(ctx, view.ID)
	if plain.AuthPassword != "auth-secret-1" || after.Username != "mon2" || !after.HasPrivPassword {
		t.Errorf("update lost a secret: %+v / %+v", plain, after)
	}

	// Switching to v2c clears every v3 field, secrets included.
	_, after, err = svc.Update(ctx, view.ID, models.CredentialInput{Name: "core-v3", Version: "2c", Community: str("public")})
	testdb.Must(t, err)
	testdb.Must(t, db.First(&raw, "id = ?", view.ID).Error)
	if raw.AuthPassword != "" || raw.PrivPassword != "" || raw.Username != "" || !after.HasCommunity || after.HasAuthPassword {
		t.Errorf("v3 fields survived the switch to v2c: %+v", raw)
	}
}

func TestDBCredentialLifecycle(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewSNMPCredentialService(db)
	s := seedDevice(t, db, "HQ", "10.0.0.2") // its credential is in use by the device

	if _, err := svc.Delete(ctx, s.CredentialID); !errors.Is(err, ErrCredentialInUse) {
		t.Errorf("deleting a credential in use: %v, want ErrCredentialInUse", err)
	}
	if _, err := svc.Create(ctx, models.CredentialInput{Name: "CRED-" + s.CredentialID.String()[:8], Version: "2c", Community: str("c")}, uuid.Nil); !errors.Is(err, ErrCredentialNameTaken) {
		t.Errorf("duplicate name (case-insensitive): %v, want ErrCredentialNameTaken", err)
	}

	other := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Branch')`, other)
	branchOnly, err := svc.Create(ctx, models.CredentialInput{Name: "branch", SiteID: &other, Version: "1", Community: str("c")}, uuid.Nil)
	testdb.Must(t, err)

	opts, err := svc.ForSite(ctx, s.SiteID)
	testdb.Must(t, err)
	for _, o := range opts {
		if o.ID == branchOnly.ID {
			t.Error("HQ was offered Branch's site-scoped credential")
		}
	}
	if ok, _ := svc.UsableAt(ctx, branchOnly.ID, s.SiteID); ok {
		t.Error("UsableAt: branch-only credential usable at HQ")
	}
	if ok, _ := svc.UsableAt(ctx, s.CredentialID, other); !ok {
		t.Error("UsableAt: global credential not usable at Branch")
	}

	// Scoping a credential in use at HQ to Branch would strand HQ's device.
	if _, _, err := svc.Update(ctx, s.CredentialID, models.CredentialInput{Name: "x", SiteID: &other, Version: "2c"}); !errors.Is(err, ErrCredentialSiteMismatch) {
		t.Errorf("rescoping away from its devices: %v, want ErrCredentialSiteMismatch", err)
	}

	list, err := svc.List(ctx)
	testdb.Must(t, err)
	for _, v := range list {
		if v.ID == s.CredentialID && v.UsedBy != 1 {
			t.Errorf("used_by = %d, want 1", v.UsedBy)
		}
		if v.ID == branchOnly.ID && v.SiteName != "Branch" {
			t.Errorf("site_name = %q", v.SiteName)
		}
	}

	if _, err := svc.Delete(ctx, branchOnly.ID); err != nil {
		t.Errorf("deleting an unused credential: %v", err)
	}
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./internal/services/ 2>&1 | head -3`
Expected: `undefined: ViewOf` / `NewSNMPCredentialService`.

- [ ] **Step 4: Write the service**

`backend/internal/services/snmp_credential_service.go`:

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/cryptutil"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

var (
	ErrCredentialNotFound     = errors.New("credential profile not found")
	ErrCredentialNameTaken    = errors.New("a credential profile with this name already exists")
	ErrCredentialInUse        = errors.New("this credential profile is used by devices; move them to another profile first")
	ErrCredentialSiteMismatch = errors.New("devices in other sites use this profile, so it cannot be limited to one site")
)

// CredentialView is a profile as the API shows it: never a secret, only
// whether each is set.
type CredentialView struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	SiteID          *uuid.UUID `json:"site_id"`
	SiteName        string     `json:"site_name"`
	Version         string     `json:"version"`
	Username        string     `json:"username"`
	AuthProtocol    string     `json:"auth_protocol"`
	PrivProtocol    string     `json:"priv_protocol"`
	HasCommunity    bool       `json:"has_community"`
	HasAuthPassword bool       `json:"has_auth_password"`
	HasPrivPassword bool       `json:"has_priv_password"`
	UsedBy          int64      `json:"used_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// CredentialOption is what editors see when choosing a profile for a device.
type CredentialOption struct {
	ID      uuid.UUID  `json:"id"`
	Name    string     `json:"name"`
	Version string     `json:"version"`
	SiteID  *uuid.UUID `json:"site_id"`
}

// ViewOf builds the API view of a stored profile.
func ViewOf(c models.SNMPCredential, usedBy int64, siteName string) CredentialView {
	return CredentialView{
		ID: c.ID, Name: c.Name, SiteID: c.SiteID, SiteName: siteName, Version: c.Version,
		Username: c.Username, AuthProtocol: c.AuthProtocol, PrivProtocol: c.PrivProtocol,
		HasCommunity: c.Community != "", HasAuthPassword: c.AuthPassword != "", HasPrivPassword: c.PrivPassword != "",
		UsedBy: usedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// SNMPCredentialService stores credential profiles, encrypting secrets.
type SNMPCredentialService struct {
	db *gorm.DB
}

func NewSNMPCredentialService(db *gorm.DB) *SNMPCredentialService {
	return &SNMPCredentialService{db: db}
}

type credentialRow struct {
	models.SNMPCredential
	UsedBy   int64  `gorm:"column:used_by"`
	SiteName string `gorm:"column:site_name"`
}

func (s *SNMPCredentialService) query(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Table("snmp_credentials AS c").
		Select(`c.*, COALESCE(st.name, '') AS site_name,
			(SELECT count(*) FROM devices d WHERE d.credential_id = c.id) AS used_by`).
		Joins("LEFT JOIN sites st ON st.id = c.site_id")
}

// List returns every profile, by name.
func (s *SNMPCredentialService) List(ctx context.Context) ([]CredentialView, error) {
	var rows []credentialRow
	if err := s.query(ctx).Order("lower(c.name)").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing credential profiles: %w", err)
	}
	out := make([]CredentialView, 0, len(rows))
	for _, r := range rows {
		out = append(out, ViewOf(r.SNMPCredential, r.UsedBy, r.SiteName))
	}
	return out, nil
}

func (s *SNMPCredentialService) view(ctx context.Context, id uuid.UUID) (CredentialView, error) {
	var r credentialRow
	if err := s.query(ctx).Where("c.id = ?", id).Scan(&r).Error; err != nil {
		return CredentialView{}, fmt.Errorf("loading credential profile: %w", err)
	}
	if r.ID == uuid.Nil {
		return CredentialView{}, ErrCredentialNotFound
	}
	return ViewOf(r.SNMPCredential, r.UsedBy, r.SiteName), nil
}

func (s *SNMPCredentialService) get(ctx context.Context, id uuid.UUID) (*models.SNMPCredential, error) {
	var c models.SNMPCredential
	err := s.db.WithContext(ctx).First(&c, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading credential profile: %w", err)
	}
	return &c, nil
}

// seal encrypts a new secret; nil means "keep" and returns keep.
func seal(secret *string, keep string) (string, error) {
	if secret == nil {
		return keep, nil
	}
	return cryptutil.Encrypt(*secret)
}

// apply writes a normalized input onto c, encrypting new secrets and keeping
// stored ones only where the version still uses them.
func apply(c *models.SNMPCredential, in models.CredentialInput) error {
	var err error
	c.Name, c.SiteID, c.Version = in.Name, in.SiteID, in.Version
	c.Username, c.AuthProtocol, c.PrivProtocol = in.Username, in.AuthProtocol, in.PrivProtocol
	if in.Version == models.SNMPVersion3 {
		c.Community = ""
		if in.AuthProtocol == models.SNMPProtoNone {
			c.AuthPassword = ""
		} else if c.AuthPassword, err = seal(in.AuthPassword, c.AuthPassword); err != nil {
			return err
		}
		if in.PrivProtocol == models.SNMPProtoNone {
			c.PrivPassword = ""
		} else if c.PrivPassword, err = seal(in.PrivPassword, c.PrivPassword); err != nil {
			return err
		}
		return nil
	}
	c.AuthPassword, c.PrivPassword = "", ""
	c.Community, err = seal(in.Community, c.Community)
	return err
}

func mapCredentialWriteError(err error) error {
	switch {
	case isDuplicateKey(err):
		return ErrCredentialNameTaken
	case isForeignKeyViolation(err):
		return ErrSiteNotFound
	default:
		return fmt.Errorf("saving credential profile: %w", err)
	}
}

// Create stores a new profile.
func (s *SNMPCredentialService) Create(ctx context.Context, raw models.CredentialInput, by uuid.UUID) (CredentialView, error) {
	in, err := models.NormalizeCredentialInput(raw, nil)
	if err != nil {
		return CredentialView{}, err
	}
	c := models.SNMPCredential{}
	if by != uuid.Nil {
		c.CreatedBy = &by
	}
	if err := apply(&c, in); err != nil {
		return CredentialView{}, err
	}
	if err := s.db.WithContext(ctx).Create(&c).Error; err != nil {
		return CredentialView{}, mapCredentialWriteError(err)
	}
	return s.view(ctx, c.ID)
}

// Update changes a profile. Blank secrets keep the stored ones.
func (s *SNMPCredentialService) Update(ctx context.Context, id uuid.UUID, raw models.CredentialInput) (CredentialView, CredentialView, error) {
	before, err := s.view(ctx, id)
	if err != nil {
		return CredentialView{}, CredentialView{}, err
	}
	c, err := s.get(ctx, id)
	if err != nil {
		return CredentialView{}, CredentialView{}, err
	}
	in, err := models.NormalizeCredentialInput(raw, c)
	if err != nil {
		return CredentialView{}, CredentialView{}, err
	}
	if in.SiteID != nil {
		var elsewhere int64
		if err := s.db.WithContext(ctx).Model(&models.Device{}).
			Where("credential_id = ? AND site_id <> ?", id, *in.SiteID).Count(&elsewhere).Error; err != nil {
			return CredentialView{}, CredentialView{}, fmt.Errorf("checking devices using the profile: %w", err)
		}
		if elsewhere > 0 {
			return CredentialView{}, CredentialView{}, ErrCredentialSiteMismatch
		}
	}
	if err := apply(c, in); err != nil {
		return CredentialView{}, CredentialView{}, err
	}
	err = s.db.WithContext(ctx).Model(&models.SNMPCredential{}).Where("id = ?", id).Updates(map[string]any{
		"name": c.Name, "site_id": c.SiteID, "version": c.Version, "community": c.Community,
		"username": c.Username, "auth_protocol": c.AuthProtocol, "auth_password": c.AuthPassword,
		"priv_protocol": c.PrivProtocol, "priv_password": c.PrivPassword, "updated_at": gorm.Expr("now()"),
	}).Error
	if err != nil {
		return CredentialView{}, CredentialView{}, mapCredentialWriteError(err)
	}
	after, err := s.view(ctx, id)
	return before, after, err
}

// Delete removes an unused profile.
func (s *SNMPCredentialService) Delete(ctx context.Context, id uuid.UUID) (CredentialView, error) {
	v, err := s.view(ctx, id)
	if err != nil {
		return CredentialView{}, err
	}
	if v.UsedBy > 0 {
		return CredentialView{}, ErrCredentialInUse
	}
	if err := s.db.WithContext(ctx).Delete(&models.SNMPCredential{}, "id = ?", id).Error; err != nil {
		if isForeignKeyViolation(err) { // a device was added in between
			return CredentialView{}, ErrCredentialInUse
		}
		return CredentialView{}, fmt.Errorf("deleting credential profile: %w", err)
	}
	return v, nil
}

// ForSite lists the profiles a site's devices may use: global ones and the
// site's own.
func (s *SNMPCredentialService) ForSite(ctx context.Context, siteID uuid.UUID) ([]CredentialOption, error) {
	var out []CredentialOption
	err := s.db.WithContext(ctx).Model(&models.SNMPCredential{}).
		Select("id, name, version, site_id").
		Where("site_id IS NULL OR site_id = ?", siteID).
		Order("lower(name)").Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing credential profiles for site: %w", err)
	}
	return out, nil
}

// UsableAt reports whether a profile may be used by a device in siteID.
func (s *SNMPCredentialService) UsableAt(ctx context.Context, credentialID, siteID uuid.UUID) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&models.SNMPCredential{}).
		Where("id = ? AND (site_id IS NULL OR site_id = ?)", credentialID, siteID).Count(&n).Error
	return n > 0, err
}

// Decrypted returns a profile's secrets in plaintext, for polling only.
func (s *SNMPCredentialService) Decrypted(ctx context.Context, id uuid.UUID) (snmp.Credential, error) {
	c, err := s.get(ctx, id)
	if err != nil {
		return snmp.Credential{}, err
	}
	out := snmp.Credential{Version: c.Version, Username: c.Username, AuthProtocol: c.AuthProtocol, PrivProtocol: c.PrivProtocol}
	for _, f := range []struct {
		dst *string
		src string
	}{{&out.Community, c.Community}, {&out.AuthPassword, c.AuthPassword}, {&out.PrivPassword, c.PrivPassword}} {
		if *f.dst, err = cryptutil.Decrypt(f.src); err != nil {
			return snmp.Credential{}, fmt.Errorf("decrypting credential profile %q: %w", c.Name, err)
		}
	}
	return out, nil
}
```

- [ ] **Step 5: Run the service tests**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/services/ -run TestCredentialView -v 2>&1 | tail -2 && ./scripts/test-db.sh -run 'TestDBCredential' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `TestCredentialViewHasNoSecrets` PASS; `TestDBCredentialSecretsEncrypted` and `TestDBCredentialLifecycle` PASS.

- [ ] **Step 6: Write the failing handler tests**

`backend/internal/api/snmp_credential_handler_test.go`:

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

type fakeCreds struct{ mutations int }

func (f *fakeCreds) List(context.Context) ([]services.CredentialView, error) { return nil, nil }
func (f *fakeCreds) Create(context.Context, models.CredentialInput, uuid.UUID) (services.CredentialView, error) {
	f.mutations++
	return services.CredentialView{ID: uuid.New(), Name: "x"}, nil
}
func (f *fakeCreds) Update(context.Context, uuid.UUID, models.CredentialInput) (services.CredentialView, services.CredentialView, error) {
	f.mutations++
	return services.CredentialView{}, services.CredentialView{}, nil
}
func (f *fakeCreds) Delete(context.Context, uuid.UUID) (services.CredentialView, error) {
	f.mutations++
	return services.CredentialView{}, services.ErrCredentialInUse
}
func (f *fakeCreds) ForSite(context.Context, uuid.UUID) ([]services.CredentialOption, error) {
	return []services.CredentialOption{{ID: uuid.New(), Name: "Default", Version: "2c"}}, nil
}

func credRouter(creds *fakeCreds, siteLevel services.SiteAccessLevel, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	RegisterSNMPCredentialRoutes(r.Group("/api/v1"), creds, fakeSiteAccess{siteLevel}, &fakeAudit{},
		stubUsers{user: &models.User{IsAdmin: isAdmin}})
	return r
}

// Managing profiles is admin-only, even for someone with editable access to
// every site.
func TestCredentialRoutesAdminOnly(t *testing.T) {
	id := uuid.New().String()
	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/snmp-credentials"},
		{http.MethodPost, "/api/v1/snmp-credentials"},
		{http.MethodPut, "/api/v1/snmp-credentials/" + id},
		{http.MethodDelete, "/api/v1/snmp-credentials/" + id},
	} {
		creds := &fakeCreds{}
		w := do(credRouter(creds, services.SiteAccessEditable, false), rt.method, rt.path, map[string]any{"name": "x", "version": "2c", "community": "c"})
		if w.Code != http.StatusForbidden || creds.mutations != 0 {
			t.Errorf("%s %s: status %d, mutations %d; want 403, 0", rt.method, rt.path, w.Code, creds.mutations)
		}
	}
}

// Editors choose a profile by name for their site; readonly users cannot list
// them, and nothing secret is ever in the response.
func TestSiteCredentialOptions(t *testing.T) {
	path := "/api/v1/sites/" + uuid.New().String() + "/credentials"
	if w := do(credRouter(&fakeCreds{}, services.SiteAccessEditable, false), http.MethodGet, path, nil); w.Code != http.StatusOK {
		t.Errorf("editable: %d, want 200", w.Code)
	}
	if w := do(credRouter(&fakeCreds{}, services.SiteAccessReadonly, false), http.MethodGet, path, nil); w.Code != http.StatusForbidden {
		t.Errorf("readonly: %d, want 403", w.Code)
	}
	if w := do(credRouter(&fakeCreds{}, services.SiteAccessNone, false), http.MethodGet, path, nil); w.Code != http.StatusNotFound {
		t.Errorf("no access: %d, want 404", w.Code)
	}
}

func TestDeleteCredentialInUseIsConflict(t *testing.T) {
	w := do(credRouter(&fakeCreds{}, services.SiteAccessAdmin, true), http.MethodDelete, "/api/v1/snmp-credentials/"+uuid.New().String(), nil)
	if w.Code != http.StatusConflict {
		t.Errorf("status %d, want 409", w.Code)
	}
}
```

(`do`, `fakeAudit` and `stubUsers` exist in this package from phase 0; `fakeSiteAccess` from Task 3.)

- [ ] **Step 7: Write the handlers**

`backend/internal/api/snmp_credential_handler.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// credentialStore is what the handlers need from SNMPCredentialService.
type credentialStore interface {
	List(ctx context.Context) ([]services.CredentialView, error)
	Create(ctx context.Context, in models.CredentialInput, by uuid.UUID) (services.CredentialView, error)
	Update(ctx context.Context, id uuid.UUID, in models.CredentialInput) (services.CredentialView, services.CredentialView, error)
	Delete(ctx context.Context, id uuid.UUID) (services.CredentialView, error)
	ForSite(ctx context.Context, siteID uuid.UUID) ([]services.CredentialOption, error)
}

// RegisterSNMPCredentialRoutes mounts credential profile management (admin)
// and the per-site option list (editors of that site).
func RegisterSNMPCredentialRoutes(rg *gin.RouterGroup, creds credentialStore, sites siteAccessChecker, audit auditRecorder, users adminChecker) {
	admin := rg.Group("/snmp-credentials", RequireAdmin(users))
	admin.GET("", listCredentialsHandler(creds))
	admin.POST("", createCredentialHandler(creds, audit))
	admin.PUT("/:id", updateCredentialHandler(creds, audit))
	admin.DELETE("/:id", deleteCredentialHandler(creds, audit))

	rg.GET("/sites/:id/credentials", siteCredentialOptionsHandler(creds, sites))
}

func respondCredentialError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrCredentialNotFound):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrCredentialNameTaken), errors.Is(err, services.ErrCredentialInUse),
		errors.Is(err, services.ErrCredentialSiteMismatch):
		respondError(c, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrSiteNotFound):
		respondError(c, http.StatusBadRequest, "no such site")
	default:
		// Validation errors from NormalizeCredentialInput are plain errors
		// with a user-facing message and never contain a secret.
		if isInternal(err) {
			respondInternal(c, op, err)
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
	}
}

// isInternal separates wrapped database/crypto failures (which carry "saving",
// "loading", "decrypting", ... prefixes from the service) from validation
// messages.
func isInternal(err error) bool {
	var wrapped interface{ Unwrap() error }
	return errors.As(err, &wrapped)
}

func credentialAudit(v services.CredentialView) map[string]any {
	// Never a secret: the view has none.
	return map[string]any{"name": v.Name, "version": v.Version, "site_id": v.SiteID}
}

func listCredentialsHandler(creds credentialStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := creds.List(c.Request.Context())
		if err != nil {
			respondInternal(c, "listCredentials", err)
			return
		}
		if list == nil {
			list = []services.CredentialView{}
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func createCredentialHandler(creds credentialStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in models.CredentialInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		userID, _, _, _ := GetUserFromContext(c)
		v, err := creds.Create(c.Request.Context(), in, userID)
		if err != nil {
			respondCredentialError(c, "createCredential", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionCredentialCreated, models.ResourceSNMPCredential,
			&v.ID, models.AuditChanges{Summary: credentialAudit(v)})
		respondSuccess(c, http.StatusCreated, v)
	}
}

func updateCredentialHandler(creds credentialStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid credential id")
			return
		}
		var in models.CredentialInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		before, after, err := creds.Update(c.Request.Context(), id, in)
		if err != nil {
			respondCredentialError(c, "updateCredential", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionCredentialUpdated, models.ResourceSNMPCredential,
			&id, models.AuditChanges{Before: credentialAudit(before), After: credentialAudit(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteCredentialHandler(creds credentialStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid credential id")
			return
		}
		v, err := creds.Delete(c.Request.Context(), id)
		if err != nil {
			respondCredentialError(c, "deleteCredential", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionCredentialDeleted, models.ResourceSNMPCredential,
			&id, models.AuditChanges{Summary: credentialAudit(v)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

// siteCredentialOptionsHandler lists profile names usable at a site, for
// people who can add devices there.
func siteCredentialOptionsHandler(creds credentialStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		siteID, ok := parseSiteID(c)
		if !ok {
			return
		}
		if !requireSiteLevel(c, sites, siteID, services.SiteAccessEditable) {
			return
		}
		opts, err := creds.ForSite(c.Request.Context(), siteID)
		if err != nil {
			respondInternal(c, "siteCredentialOptions", err)
			return
		}
		if opts == nil {
			opts = []services.CredentialOption{}
		}
		respondSuccess(c, http.StatusOK, opts)
	}
}

// requireSiteLevel writes the response and returns false unless the caller has
// at least want on the site: no access is a 404, too little a 403.
func requireSiteLevel(c *gin.Context, sites siteAccessChecker, siteID uuid.UUID, want services.SiteAccessLevel) bool {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondAuthError(c, http.StatusUnauthorized, "authentication required")
		return false
	}
	level, err := sites.SiteAccess(c.Request.Context(), userID, isAdmin, siteID)
	if errors.Is(err, services.ErrSiteNotFound) || (err == nil && level == services.SiteAccessNone) {
		respondError(c, http.StatusNotFound, "site not found")
		return false
	}
	if err != nil {
		respondInternal(c, "site access", err)
		return false
	}
	if level < want {
		respondError(c, http.StatusForbidden, "you need edit access to this site")
		return false
	}
	return true
}
```

About `isInternal`: every service error that is not a validation message is created with `fmt.Errorf("...: %w", err)` and so unwraps; validation errors from `NormalizeCredentialInput` are `errors.New`/`fmt.Errorf` without `%w` and do not. Before relying on it, confirm with `grep -n 'fmt.Errorf' backend/internal/models/snmp.go`: none of them may use `%w`.

- [ ] **Step 8: Run the handler tests**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./internal/api/ && go test ./internal/api/ -run 'Credential' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: PASS for the three tests.

- [ ] **Step 9: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/internal/models/audit.go backend/internal/services/snmp_credential_service.go \
  backend/internal/services/snmp_credential_db_test.go backend/internal/services/snmp_credential_view_test.go \
  backend/internal/api/snmp_credential_handler.go backend/internal/api/snmp_credential_handler_test.go
git commit -m "feat(snmp): credential profiles with encrypted, write-only secrets

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 7: Devices — service, prober and API

**Files:**
- Create: `backend/internal/services/device_service.go`, `backend/internal/services/device_service_db_test.go`
- Create: `backend/internal/services/prober.go`, `backend/internal/services/prober_test.go`
- Create: `backend/internal/api/device_handler.go`, `backend/internal/api/device_handler_test.go`

**Interfaces:**
- Consumes: Tasks 2–6 (`models.Device`, `NormalizeDeviceInput`, `IncidentService.CloseDeviceIncident`, `NextDeviceState`, `PollerStore`, `ReachabilityUpdate`, `SNMPCredentialService.UsableAt/Decrypted`, `snmp.*`), `requireSiteLevel`, `siteAccessChecker`, `auditRecorder` (api).
- Produces:
  - Errors `ErrDeviceNotFound`, `ErrDeviceHostTaken`, `ErrCredentialNotUsable`, `ErrTargetBlocked`.
  - `type DeviceView struct { models.Device; SiteName string \`json:"site_name"\`; CredentialName string \`json:"credential_name"\`; Availability30d *float64 \`json:"availability_30d"\` }`.
  - `type DeviceFilter struct { SiteID *uuid.UUID; Status string }`.
  - `DeviceService` (`NewDeviceService(db, creds *SNMPCredentialService, incidents *IncidentService)`) with `List(ctx, userID uuid.UUID, isAdmin bool, f DeviceFilter) ([]DeviceView, error)`, `Get(ctx, id) (*DeviceView, error)`, `Create(ctx, in models.DeviceInput, by uuid.UUID) (*models.Device, error)`, `CreateMany(ctx, siteID uuid.UUID, in []models.DeviceInput, by uuid.UUID) ([]models.Device, []BulkAddError)`, `Update(ctx, id, in models.DeviceInput) (before, after *models.Device, err error)`, `Delete(ctx, id) (*models.Device, error)`, `Interfaces(ctx, id, includeAbsent bool) ([]models.DeviceInterface, error)`, `RequestRefresh(ctx, id) error`, `HostsInSite(ctx, siteID) (map[string]bool, error)`, plus the `PollerStore` methods. `var _ PollerStore = (*DeviceService)(nil)`.
  - `type BulkAddError struct { Host string \`json:"host"\`; Error string \`json:"error"\` }`.
  - `type Prober struct` with `NewProber(creds *SNMPCredentialService, client snmp.Client) *Prober` and `Identify(ctx, host string, port int, credentialID uuid.UUID, timeout time.Duration, retries int) (snmp.System, error)`; `IdentifyWith(ctx, host string, port int, cred snmp.Credential, timeout time.Duration, retries int) (snmp.System, error)`.
  - `api.RegisterDeviceRoutes(rg, devices deviceStore, prober deviceProber, sites siteAccessChecker, audit auditRecorder)`.

Device naming: a device created without a name is named after its host; the first inventory renames it to `sysName` if it still carries the host as its name. A name the user typed is never overwritten.

- [ ] **Step 1: Write the failing DB tests**

`backend/internal/services/device_service_db_test.go`:

```go
package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func deviceSvc(db *gorm.DB) *DeviceService {
	return NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
}

func TestDBDeviceListFollowsSiteSharing(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	alice := testdb.NewUser(t, db, false)
	shared := seedDevice(t, db, "Shared", "10.0.0.2")
	seedDevice(t, db, "Private", "10.0.0.3")
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, shared.SiteID, alice)
	svc := deviceSvc(db)

	all, err := svc.List(ctx, uuid.Nil, true, DeviceFilter{})
	testdb.Must(t, err)
	mine, err := svc.List(ctx, alice, false, DeviceFilter{})
	testdb.Must(t, err)
	if len(all) != 2 || len(mine) != 1 || mine[0].ID != shared.DeviceID || mine[0].SiteName != "Shared" {
		t.Fatalf("admin %d, alice %+v", len(all), mine)
	}
	down, err := svc.List(ctx, uuid.Nil, true, DeviceFilter{Status: models.DeviceStatusDown})
	testdb.Must(t, err)
	if len(down) != 0 {
		t.Errorf("status filter: %d down, want 0", len(down))
	}
}

func TestDBDeviceCreateChecksCredentialScope(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	hq := seedDevice(t, db, "HQ", "10.0.0.2")
	branch := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Branch')`, branch)
	branchCred := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, site_id, version, community) VALUES (?, 'branch', ?, '2c', 'x')`, branchCred, branch)
	svc := deviceSvc(db)

	if _, err := svc.Create(ctx, models.DeviceInput{SiteID: hq.SiteID, CredentialID: branchCred, Host: "10.0.0.9"}, uuid.Nil); !errors.Is(err, ErrCredentialNotUsable) {
		t.Errorf("branch credential at HQ: %v, want ErrCredentialNotUsable", err)
	}
	d, err := svc.Create(ctx, models.DeviceInput{SiteID: hq.SiteID, CredentialID: hq.CredentialID, Host: "10.0.0.9"}, uuid.Nil)
	testdb.Must(t, err)
	if d.Name != "10.0.0.9" || d.Status != models.DeviceStatusPending || d.Port != 161 {
		t.Errorf("created %+v", d)
	}
	if _, err := svc.Create(ctx, models.DeviceInput{SiteID: hq.SiteID, CredentialID: hq.CredentialID, Host: "10.0.0.9"}, uuid.Nil); !errors.Is(err, ErrDeviceHostTaken) {
		t.Errorf("duplicate host: %v, want ErrDeviceHostTaken", err)
	}
}

func TestDBSaveInventory(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `UPDATE devices SET name = host WHERE id = ?`, s.DeviceID) // unnamed
	svc := deviceSvc(db)
	at := time.Now().UTC()

	inv := snmp.Inventory{
		System: snmp.System{Name: "core-sw-1", Descr: "EdgeSwitch", ObjectID: "1.3.6.1.4.1.4413", Location: "MDF"},
		Vendor: "Ubiquiti (EdgeSwitch)", Model: "ES-48-500W", Serial: "F09F",
		Interfaces: []snmp.Interface{{Index: 1, Name: "0/1", AdminStatus: "up", OperStatus: "up", SpeedBps: 1e9},
			{Index: 2, Name: "0/2", AdminStatus: "up", OperStatus: "down"}},
	}
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, inv, at))
	inv.Interfaces = inv.Interfaces[:1] // port 2 disappears (module removed)
	inv.Interfaces[0].Alias = "Uplink"
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, inv, at))

	d, err := svc.Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	if d.Name != "core-sw-1" || d.Model != "ES-48-500W" || d.Vendor != "Ubiquiti (EdgeSwitch)" || d.SysLocation != "MDF" || d.LastInventoryAt == nil {
		t.Errorf("device after inventory: %+v", d.Device)
	}
	present, err := svc.Interfaces(ctx, s.DeviceID, false)
	testdb.Must(t, err)
	all, err := svc.Interfaces(ctx, s.DeviceID, true)
	testdb.Must(t, err)
	if len(present) != 1 || present[0].Alias != "Uplink" || len(all) != 2 {
		t.Errorf("interfaces: present %+v, all %d", present, len(all))
	}

	// A name the user typed survives inventory.
	testdb.Exec(t, db, `UPDATE devices SET name = 'Core switch' WHERE id = ?`, s.DeviceID)
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, inv, at))
	d, _ = svc.Get(ctx, s.DeviceID)
	if d.Name != "Core switch" {
		t.Errorf("user's name overwritten: %q", d.Name)
	}
}

func TestDBDueDevices(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	never := seedDevice(t, db, "A", "10.0.0.1")
	due := seedDevice(t, db, "B", "10.0.0.2")
	fresh := seedDevice(t, db, "C", "10.0.0.3")
	off := seedDevice(t, db, "D", "10.0.0.4")
	now := time.Now().UTC()
	testdb.Exec(t, db, `UPDATE devices SET last_polled_at = ? WHERE id = ?`, now.Add(-61*time.Second), due.DeviceID)
	testdb.Exec(t, db, `UPDATE devices SET last_polled_at = ? WHERE id = ?`, now.Add(-30*time.Second), fresh.DeviceID)
	testdb.Exec(t, db, `UPDATE devices SET enabled = false WHERE id = ?`, off.DeviceID)

	got, err := deviceSvc(db).DueDevices(ctx, now, 100)
	testdb.Must(t, err)
	ids := map[uuid.UUID]bool{}
	for _, d := range got {
		ids[d.ID] = true
	}
	if !ids[never.DeviceID] || !ids[due.DeviceID] || ids[fresh.DeviceID] || ids[off.DeviceID] || len(ids) != 2 {
		t.Errorf("due = %v", ids)
	}
}

// Pausing a device that is down closes its incident, noting why.
func TestDBPauseClosesIncident(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `UPDATE devices SET status = 'down', consecutive_failures = 4 WHERE id = ?`, s.DeviceID)
	svc := deviceSvc(db)
	_, _, err := NewIncidentService(db).OpenDeviceIncident(ctx, s.DeviceID, time.Now().UTC().Add(-time.Hour), "timeout")
	testdb.Must(t, err)

	off := false
	_, after, err := svc.Update(ctx, s.DeviceID, models.DeviceInput{SiteID: s.SiteID, CredentialID: s.CredentialID, Host: "10.0.0.2", Enabled: &off})
	testdb.Must(t, err)
	if after.Status != models.DeviceStatusPaused || after.ConsecutiveFailures != 0 {
		t.Errorf("after pause: %+v", after)
	}
	var notes string
	testdb.Must(t, db.Raw(`SELECT resolution_notes FROM incidents WHERE device_id = ? AND end_time IS NOT NULL`, s.DeviceID).Scan(&notes).Error)
	if notes != "Monitoring was paused." {
		t.Errorf("incident not closed with a note: %q", notes)
	}

	on := true
	_, after, err = svc.Update(ctx, s.DeviceID, models.DeviceInput{SiteID: s.SiteID, CredentialID: s.CredentialID, Host: "10.0.0.2", Enabled: &on})
	testdb.Must(t, err)
	if after.Status != models.DeviceStatusPending || after.LastPolledAt != nil {
		t.Errorf("after resume: %+v", after)
	}
}

// Availability is measured over the time the device has existed, capped at
// 30 days: a device 10 days old with one day of outage is at 90%.
func TestDBDeviceAvailability(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	now := time.Now().UTC()
	testdb.Exec(t, db, `UPDATE devices SET created_at = ? WHERE id = ?`, now.Add(-10*24*time.Hour), s.DeviceID)
	start, end := now.Add(-5*24*time.Hour), now.Add(-4*24*time.Hour)
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, start_time, end_time, severity) VALUES (?, ?, ?, 'high')`, s.DeviceID, start, end)

	d, err := deviceSvc(db).Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	if d.Availability30d == nil || *d.Availability30d < 89.9 || *d.Availability30d > 90.1 {
		t.Errorf("availability = %v, want ~90", d.Availability30d)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./internal/services/ 2>&1 | head -3`
Expected: `undefined: NewDeviceService`.

- [ ] **Step 3: Write the service**

`backend/internal/services/device_service.go`:

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

var (
	ErrDeviceNotFound      = errors.New("device not found")
	ErrDeviceHostTaken     = errors.New("this site already has a device at that address and port")
	ErrCredentialNotUsable = errors.New("that credential profile cannot be used in this site")
)

// DeviceView is a device with the names and figures its pages show.
type DeviceView struct {
	models.Device
	SiteName        string   `json:"site_name" gorm:"column:site_name"`
	CredentialName  string   `json:"credential_name" gorm:"column:credential_name"`
	Availability30d *float64 `json:"availability_30d" gorm:"column:availability_30d"`
}

// DeviceFilter narrows the device list.
type DeviceFilter struct {
	SiteID *uuid.UUID
	Status string
}

// BulkAddError explains why one address of a bulk add was not added.
type BulkAddError struct {
	Host  string `json:"host"`
	Error string `json:"error"`
}

// DeviceService stores devices and is the poller's store.
type DeviceService struct {
	db        *gorm.DB
	creds     *SNMPCredentialService
	incidents *IncidentService
}

var _ PollerStore = (*DeviceService)(nil)

func NewDeviceService(db *gorm.DB, creds *SNMPCredentialService, incidents *IncidentService) *DeviceService {
	return &DeviceService{db: db, creds: creds, incidents: incidents}
}

// availabilitySQL is the share of the last 30 days (or of the device's life,
// if shorter) not covered by its incidents. NULL for a device younger than a
// minute, where a percentage would mean nothing.
const availabilitySQL = `(
	SELECT CASE WHEN win.secs < 60 THEN NULL ELSE
		GREATEST(0, 100 - 100 * COALESCE(SUM(EXTRACT(EPOCH FROM (
			LEAST(COALESCE(i.end_time, now()), now()) - GREATEST(i.start_time, win.since)))), 0) / win.secs)
	END
	FROM (SELECT GREATEST(d.created_at, now() - interval '30 days') AS since,
	             EXTRACT(EPOCH FROM (now() - GREATEST(d.created_at, now() - interval '30 days'))) AS secs) win
	LEFT JOIN incidents i ON i.device_id = d.id
		AND COALESCE(i.end_time, now()) > win.since
	GROUP BY win.secs
) AS availability_30d`

func (s *DeviceService) viewQuery(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Table("devices AS d").
		Select("d.*, st.name AS site_name, c.name AS credential_name, " + availabilitySQL).
		Joins("JOIN sites st ON st.id = d.site_id").
		Joins("JOIN snmp_credentials c ON c.id = d.credential_id")
}

// List returns the devices in sites the caller can see.
func (s *DeviceService) List(ctx context.Context, userID uuid.UUID, isAdmin bool, f DeviceFilter) ([]DeviceView, error) {
	q := s.viewQuery(ctx)
	if !isAdmin {
		q = q.Where("d.site_id IN (SELECT site_id FROM site_sharing WHERE shared_with_user_id = ?)", userID)
	}
	if f.SiteID != nil {
		q = q.Where("d.site_id = ?", *f.SiteID)
	}
	if f.Status != "" {
		q = q.Where("d.status = ?", f.Status)
	}
	var out []DeviceView
	if err := q.Order("lower(st.name), lower(d.name)").Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("listing devices: %w", err)
	}
	return out, nil
}

// Get returns one device, or ErrDeviceNotFound.
func (s *DeviceService) Get(ctx context.Context, id uuid.UUID) (*DeviceView, error) {
	var v DeviceView
	if err := s.viewQuery(ctx).Where("d.id = ?", id).Scan(&v).Error; err != nil {
		return nil, fmt.Errorf("loading device: %w", err)
	}
	if v.ID == uuid.Nil {
		return nil, ErrDeviceNotFound
	}
	return &v, nil
}

func (s *DeviceService) getRaw(ctx context.Context, id uuid.UUID) (*models.Device, error) {
	var d models.Device
	err := s.db.WithContext(ctx).First(&d, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading device: %w", err)
	}
	return &d, nil
}

func mapDeviceWriteError(err error) error {
	switch {
	case isDuplicateKey(err):
		return ErrDeviceHostTaken
	case isForeignKeyViolation(err):
		return ErrSiteNotFound
	default:
		return fmt.Errorf("saving device: %w", err)
	}
}

func (s *DeviceService) checkCredential(ctx context.Context, credentialID, siteID uuid.UUID) error {
	ok, err := s.creds.UsableAt(ctx, credentialID, siteID)
	if err != nil {
		return fmt.Errorf("checking credential profile: %w", err)
	}
	if !ok {
		return ErrCredentialNotUsable
	}
	return nil
}

// Create adds a device. An empty name becomes the host until the first
// inventory names it from sysName.
func (s *DeviceService) Create(ctx context.Context, raw models.DeviceInput, by uuid.UUID) (*models.Device, error) {
	in, err := models.NormalizeDeviceInput(raw)
	if err != nil {
		return nil, err
	}
	if err := s.checkCredential(ctx, in.CredentialID, in.SiteID); err != nil {
		return nil, err
	}
	d := models.Device{
		SiteID: in.SiteID, CredentialID: in.CredentialID, Name: in.Name, Host: in.Host, Port: in.Port,
		Enabled: *in.Enabled, PollInterval: in.PollInterval, TimeoutMs: in.TimeoutMs, Retries: in.Retries,
		NotifyChannels: in.NotifyChannels, Status: models.DeviceStatusPending,
	}
	if d.Name == "" {
		d.Name = d.Host
	}
	if !d.Enabled {
		d.Status = models.DeviceStatusPaused
	}
	if by != uuid.Nil {
		d.CreatedBy = &by
	}
	if err := s.db.WithContext(ctx).Create(&d).Error; err != nil {
		return nil, mapDeviceWriteError(err)
	}
	return &d, nil
}

// CreateMany adds several devices to one site, reporting each failure
// instead of stopping at the first.
func (s *DeviceService) CreateMany(ctx context.Context, siteID uuid.UUID, in []models.DeviceInput, by uuid.UUID) ([]models.Device, []BulkAddError) {
	var added []models.Device
	var failed []BulkAddError
	for _, one := range in {
		one.SiteID = siteID
		d, err := s.Create(ctx, one, by)
		if err != nil {
			failed = append(failed, BulkAddError{Host: one.Host, Error: err.Error()})
			continue
		}
		added = append(added, *d)
	}
	return added, failed
}

// Update changes a device. Disabling pauses it (closing any open incident);
// enabling a paused device starts it again from pending.
func (s *DeviceService) Update(ctx context.Context, id uuid.UUID, raw models.DeviceInput) (*models.Device, *models.Device, error) {
	before, err := s.getRaw(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	in, err := models.NormalizeDeviceInput(raw)
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkCredential(ctx, in.CredentialID, in.SiteID); err != nil {
		return nil, nil, err
	}
	updates := map[string]any{
		"site_id": in.SiteID, "credential_id": in.CredentialID, "host": in.Host, "port": in.Port,
		"enabled": *in.Enabled, "poll_interval": in.PollInterval, "timeout_ms": in.TimeoutMs,
		"retries": in.Retries, "notify_channels": in.NotifyChannels, "updated_at": gorm.Expr("now()"),
	}
	if in.Name != "" {
		updates["name"] = in.Name
	}
	switch {
	case before.Enabled && !*in.Enabled:
		tr := NextDeviceState(before.Status, before.ConsecutiveFailures, PollPaused)
		updates["status"], updates["consecutive_failures"], updates["status_detail"] = tr.Status, tr.Failures, ""
		for _, a := range tr.Actions {
			if a == ActionCloseIncident {
				if _, err := s.incidents.CloseDeviceIncident(ctx, id, time.Now().UTC(), "Monitoring was paused."); err != nil {
					return nil, nil, err
				}
			}
		}
	case !before.Enabled && *in.Enabled:
		updates["status"], updates["consecutive_failures"], updates["last_polled_at"] = models.DeviceStatusPending, 0, nil
	case in.Host != before.Host || in.Port != before.Port || in.CredentialID != before.CredentialID:
		// A different target: poll and re-inventory it now.
		updates["last_polled_at"], updates["last_inventory_at"] = nil, nil
	}
	if err := s.db.WithContext(ctx).Model(&models.Device{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, nil, mapDeviceWriteError(err)
	}
	after, err := s.getRaw(ctx, id)
	return before, after, err
}

// Delete removes a device; its interfaces, incidents and notifications go
// with it (ON DELETE CASCADE).
func (s *DeviceService) Delete(ctx context.Context, id uuid.UUID) (*models.Device, error) {
	d, err := s.getRaw(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Delete(&models.Device{}, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("deleting device: %w", err)
	}
	return d, nil
}

// Interfaces lists a device's interfaces by index.
func (s *DeviceService) Interfaces(ctx context.Context, id uuid.UUID, includeAbsent bool) ([]models.DeviceInterface, error) {
	q := s.db.WithContext(ctx).Where("device_id = ?", id)
	if !includeAbsent {
		q = q.Where("present")
	}
	var out []models.DeviceInterface
	if err := q.Order("if_index").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("listing interfaces: %w", err)
	}
	return out, nil
}

// RequestRefresh makes the poller poll and re-inventory the device on its
// next tick.
func (s *DeviceService) RequestRefresh(ctx context.Context, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Model(&models.Device{}).Where("id = ? AND enabled", id).
		Updates(map[string]any{"last_polled_at": nil, "last_inventory_at": nil})
	if res.Error != nil {
		return fmt.Errorf("requesting refresh: %w", res.Error)
	}
	return nil
}

// HostsInSite returns "host:port" for every device in a site, for marking scan
// results that are already added.
func (s *DeviceService) HostsInSite(ctx context.Context, siteID uuid.UUID) (map[string]bool, error) {
	var rows []struct {
		Host string
		Port int
	}
	if err := s.db.WithContext(ctx).Model(&models.Device{}).Select("host, port").
		Where("site_id = ?", siteID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing site hosts: %w", err)
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[fmt.Sprintf("%s:%d", r.Host, r.Port)] = true
	}
	return out, nil
}

// ---- PollerStore ------------------------------------------------------------

// DueDevices returns enabled devices never polled or whose interval has
// passed, longest-waiting first.
func (s *DeviceService) DueDevices(ctx context.Context, now time.Time, limit int) ([]models.Device, error) {
	var out []models.Device
	err := s.db.WithContext(ctx).
		Where("enabled AND (last_polled_at IS NULL OR last_polled_at + make_interval(secs => poll_interval) <= ?)", now).
		Order("last_polled_at ASC NULLS FIRST").Limit(limit).Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing due devices: %w", err)
	}
	return out, nil
}

func (s *DeviceService) DeviceCredential(ctx context.Context, credentialID uuid.UUID) (snmp.Credential, error) {
	return s.creds.Decrypted(ctx, credentialID)
}

func (s *DeviceService) SiteName(ctx context.Context, siteID uuid.UUID) (string, error) {
	var name string
	err := s.db.WithContext(ctx).Raw("SELECT name FROM sites WHERE id = ?", siteID).Scan(&name).Error
	return name, err
}

// SaveReachability stores a poll's outcome. "AND enabled" keeps a poll that
// finished after the device was paused from overwriting the pause.
func (s *DeviceService) SaveReachability(ctx context.Context, id uuid.UUID, u ReachabilityUpdate) error {
	return s.db.WithContext(ctx).Exec(`UPDATE devices SET
		status = ?, consecutive_failures = ?, status_detail = ?, last_polled_at = ?,
		last_seen_at = COALESCE(?, last_seen_at),
		sys_uptime_seconds = COALESCE(?, sys_uptime_seconds),
		updated_at = now()
		WHERE id = ? AND enabled`,
		u.Status, u.Failures, u.Detail, u.PolledAt, u.SeenAt, u.UptimeSeconds, id).Error
}

// SaveInventory stores identity and interfaces in one transaction.
// Interfaces are upserted by index; any not in this walk become absent.
func (s *DeviceService) SaveInventory(ctx context.Context, id uuid.UUID, inv snmp.Inventory, at time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sys := inv.System
		if err := tx.Exec(`UPDATE devices SET
			sys_name = ?, sys_descr = ?, sys_object_id = ?, sys_location = ?, sys_contact = ?,
			vendor = ?, model = ?, serial = ?, last_inventory_at = ?,
			name = CASE WHEN name = host AND ? <> '' THEN ? ELSE name END,
			status_detail = CASE WHEN status_detail LIKE 'inventory:%' THEN '' ELSE status_detail END,
			updated_at = now()
			WHERE id = ?`,
			sys.Name, sys.Descr, sys.ObjectID, sys.Location, sys.Contact,
			inv.Vendor, inv.Model, inv.Serial, at, sys.Name, sys.Name, id).Error; err != nil {
			return fmt.Errorf("saving device identity: %w", err)
		}
		seen := make([]int, 0, len(inv.Interfaces))
		for _, it := range inv.Interfaces {
			seen = append(seen, it.Index)
			row := models.DeviceInterface{
				DeviceID: id, IfIndex: it.Index, Name: it.Name, Descr: it.Descr, Alias: it.Alias, IfType: it.Type,
				SpeedBps: it.SpeedBps, MAC: it.MAC, AdminStatus: it.AdminStatus, OperStatus: it.OperStatus,
				LastChangeSeconds: it.LastChangeSeconds, Present: true, UpdatedAt: at,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "device_id"}, {Name: "if_index"}},
				DoUpdates: clause.AssignmentColumns([]string{"name", "descr", "alias", "if_type", "speed_bps", "mac",
					"admin_status", "oper_status", "last_change_seconds", "present", "updated_at"}),
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

// SaveInventoryError records a failed inventory and stamps the attempt, so it
// is retried at the next interval rather than on every poll.
func (s *DeviceService) SaveInventoryError(ctx context.Context, id uuid.UUID, detail string, at time.Time) error {
	return s.db.WithContext(ctx).Exec(
		`UPDATE devices SET status_detail = ?, last_inventory_at = ?, updated_at = now() WHERE id = ?`,
		detail, at, id).Error
}
```

- [ ] **Step 4: Run the DB tests**

Run: `cd /home/sysadmin/sentinel/backend && ./scripts/test-db.sh -run 'TestDB(DeviceList|DeviceCreate|SaveInventory|DueDevices|PauseCloses|DeviceAvailability)' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: six `--- PASS`. If `TestDBDeviceAvailability` is off by the width of a timestamp rounding, do not widen the tolerance beyond ±0.1: find the cause (TIMESTAMP vs TIMESTAMPTZ on `devices.created_at` is the usual one; it must be TIMESTAMPTZ).

- [ ] **Step 5: The prober (test connection and scans)**

`backend/internal/services/prober_test.go`:

```go
package services

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

func TestProberRefusesBlockedTargets(t *testing.T) {
	client := &fakeSNMP{}
	p := &Prober{client: client,
		resolve: func(context.Context, string) (net.IP, error) { return net.ParseIP("169.254.169.254"), nil },
		blocked: func(net.IP) bool { return true }}
	_, err := p.IdentifyWith(context.Background(), "metadata", 161, snmp.Credential{Version: "2c"}, time.Second, 0)
	if !errors.Is(err, ErrTargetBlocked) || client.gets != 0 {
		t.Fatalf("err %v, gets %d; want ErrTargetBlocked and no SNMP sent", err, client.gets)
	}

	p.blocked = func(net.IP) bool { return false }
	sys, err := p.IdentifyWith(context.Background(), "10.0.0.2", 161, snmp.Credential{Version: "2c"}, time.Second, 0)
	if err != nil || sys.Name != "core-sw-1" {
		t.Fatalf("allowed target: %+v %v", sys, err)
	}
}
```

`backend/internal/services/prober.go`:

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/netguard"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// ErrTargetBlocked is returned for an address the network policy forbids.
var ErrTargetBlocked = errors.New("that address is blocked by network policy")

// Prober reads a device's identity on demand: Test connection and subnet
// scans. It applies the same network policy as the poller.
type Prober struct {
	creds   *SNMPCredentialService
	client  snmp.Client
	resolve func(ctx context.Context, host string) (net.IP, error)
	blocked func(net.IP) bool
}

func NewProber(creds *SNMPCredentialService, client snmp.Client) *Prober {
	return &Prober{creds: creds, client: client, resolve: resolveHost, blocked: netguard.IsBlocked}
}

// Identify reads the system group using a stored credential profile.
func (p *Prober) Identify(ctx context.Context, host string, port int, credentialID uuid.UUID, timeout time.Duration, retries int) (snmp.System, error) {
	cred, err := p.creds.Decrypted(ctx, credentialID)
	if err != nil {
		return snmp.System{}, err
	}
	return p.IdentifyWith(ctx, host, port, cred, timeout, retries)
}

// IdentifyWith reads the system group with an already-decrypted credential.
func (p *Prober) IdentifyWith(ctx context.Context, host string, port int, cred snmp.Credential, timeout time.Duration, retries int) (snmp.System, error) {
	ip, err := p.resolve(ctx, host)
	if err != nil {
		return snmp.System{}, fmt.Errorf("cannot resolve %s: %v", host, err)
	}
	if p.blocked(ip) {
		return snmp.System{}, ErrTargetBlocked
	}
	return snmp.Identify(ctx, p.client, snmp.Target{
		Host: ip.String(), Port: uint16(port), Credential: cred, Timeout: timeout, Retries: retries,
	})
}
```

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/services/ -run TestProber -v 2>&1 | tail -2`
Expected: PASS.

- [ ] **Step 6: Write the failing handler tests**

`backend/internal/api/device_handler_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type fakeDevices struct {
	device    services.DeviceView
	mutations int
}

func (f *fakeDevices) List(context.Context, uuid.UUID, bool, services.DeviceFilter) ([]services.DeviceView, error) {
	return []services.DeviceView{f.device}, nil
}
func (f *fakeDevices) Get(context.Context, uuid.UUID) (*services.DeviceView, error) { d := f.device; return &d, nil }
func (f *fakeDevices) Create(_ context.Context, in models.DeviceInput, _ uuid.UUID) (*models.Device, error) {
	f.mutations++
	return &models.Device{ID: uuid.New(), SiteID: in.SiteID, Host: in.Host}, nil
}
func (f *fakeDevices) Update(_ context.Context, id uuid.UUID, in models.DeviceInput) (*models.Device, *models.Device, error) {
	f.mutations++
	return &models.Device{ID: id}, &models.Device{ID: id, SiteID: in.SiteID}, nil
}
func (f *fakeDevices) Delete(_ context.Context, id uuid.UUID) (*models.Device, error) {
	f.mutations++
	return &models.Device{ID: id}, nil
}
func (f *fakeDevices) Interfaces(context.Context, uuid.UUID, bool) ([]models.DeviceInterface, error) {
	return nil, nil
}
func (f *fakeDevices) RequestRefresh(context.Context, uuid.UUID) error { f.mutations++; return nil }

type fakeProber struct {
	calls    int
	unusable bool
}

func (f *fakeProber) Identify(context.Context, string, int, uuid.UUID, time.Duration, int) (snmp.System, error) {
	f.calls++
	return snmp.System{Name: "core-sw-1", ObjectID: "1.3.6.1.4.1.4413"}, nil
}
func (f *fakeProber) UsableAt(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return !f.unusable, nil }

// fakeSiteLevels grants a level per site id; unknown sites get none.
type fakeSiteLevels map[uuid.UUID]services.SiteAccessLevel

func (f fakeSiteLevels) SiteAccess(_ context.Context, _ uuid.UUID, _ bool, id uuid.UUID) (services.SiteAccessLevel, error) {
	return f[id], nil
}

func deviceRouter(devs *fakeDevices, prober *fakeProber, levels fakeSiteLevels) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	uid := uuid.New() // one caller per router, so ByUser rate limiting sees one user
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uid)
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	RegisterDeviceRoutes(r.Group("/api/v1"), devs, prober, levels, &fakeAudit{})
	return r
}

func TestDeviceAccess(t *testing.T) {
	siteA, siteB := uuid.New(), uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: siteA}}
	path := "/api/v1/devices/" + dev.ID.String()
	body := map[string]any{"site_id": siteA, "credential_id": uuid.New(), "host": "10.0.0.2"}

	// No access to the device's site: 404 for every route, nothing changes.
	devs := &fakeDevices{device: dev}
	r := deviceRouter(devs, &fakeProber{}, fakeSiteLevels{})
	for _, rt := range []struct{ m, p string }{
		{http.MethodGet, path}, {http.MethodPut, path}, {http.MethodDelete, path},
		{http.MethodGet, path + "/interfaces"}, {http.MethodPost, path + "/refresh"},
	} {
		if w := do(r, rt.m, rt.p, body); w.Code != http.StatusNotFound {
			t.Errorf("no access %s %s: %d, want 404", rt.m, rt.p, w.Code)
		}
	}

	// Readonly: can read, cannot change.
	r = deviceRouter(devs, &fakeProber{}, fakeSiteLevels{siteA: services.SiteAccessReadonly})
	if w := do(r, http.MethodGet, path, nil); w.Code != http.StatusOK {
		t.Errorf("readonly get: %d", w.Code)
	}
	for _, rt := range []struct{ m, p string }{{http.MethodPut, path}, {http.MethodDelete, path}, {http.MethodPost, path + "/refresh"}, {http.MethodPost, "/api/v1/devices"}} {
		if w := do(r, rt.m, rt.p, body); w.Code != http.StatusForbidden {
			t.Errorf("readonly %s %s: %d, want 403", rt.m, rt.p, w.Code)
		}
	}
	if devs.mutations != 0 {
		t.Fatalf("refused requests changed data: %d", devs.mutations)
	}

	// Editable on A but not B: cannot move a device from A to B.
	r = deviceRouter(devs, &fakeProber{}, fakeSiteLevels{siteA: services.SiteAccessEditable, siteB: services.SiteAccessReadonly})
	moved := map[string]any{"site_id": siteB, "credential_id": uuid.New(), "host": "10.0.0.2"}
	if w := do(r, http.MethodPut, path, moved); w.Code != http.StatusForbidden || devs.mutations != 0 {
		t.Errorf("move to readonly site: %d, mutations %d", w.Code, devs.mutations)
	}
	if w := do(r, http.MethodPost, "/api/v1/devices", body); w.Code != http.StatusCreated {
		t.Errorf("editable create: %d, want 201", w.Code)
	}
}

// Test connection sends SNMP to an address of the caller's choosing, so it is
// rate limited per user: the 11th request in a minute is refused.
func TestDeviceTestIsRateLimited(t *testing.T) {
	site := uuid.New()
	prober := &fakeProber{}
	r := deviceRouter(&fakeDevices{}, prober, fakeSiteLevels{site: services.SiteAccessEditable})
	body := map[string]any{"site_id": site, "credential_id": uuid.New(), "host": "10.0.0.2"}
	codes := map[int]int{}
	for i := 0; i < 11; i++ {
		codes[do(r, http.MethodPost, "/api/v1/devices/test", body).Code]++
	}
	if codes[http.StatusOK] != 10 || codes[http.StatusTooManyRequests] != 1 || prober.calls != 10 {
		t.Errorf("codes %v, probes %d", codes, prober.calls)
	}
}

// Testing with another site's credential profile is refused before any SNMP
// is sent.
func TestDeviceTestChecksCredentialScope(t *testing.T) {
	site := uuid.New()
	prober := &fakeProber{unusable: true}
	r := deviceRouter(&fakeDevices{}, prober, fakeSiteLevels{site: services.SiteAccessEditable})
	w := do(r, http.MethodPost, "/api/v1/devices/test", map[string]any{"site_id": site, "credential_id": uuid.New(), "host": "10.0.0.2"})
	if w.Code != http.StatusBadRequest || prober.calls != 0 {
		t.Errorf("status %d, probes %d; want 400, 0", w.Code, prober.calls)
	}
}
```

- [ ] **Step 7: Write the device handlers**

`backend/internal/api/device_handler.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type deviceStore interface {
	List(ctx context.Context, userID uuid.UUID, isAdmin bool, f services.DeviceFilter) ([]services.DeviceView, error)
	Get(ctx context.Context, id uuid.UUID) (*services.DeviceView, error)
	Create(ctx context.Context, in models.DeviceInput, by uuid.UUID) (*models.Device, error)
	Update(ctx context.Context, id uuid.UUID, in models.DeviceInput) (*models.Device, *models.Device, error)
	Delete(ctx context.Context, id uuid.UUID) (*models.Device, error)
	Interfaces(ctx context.Context, id uuid.UUID, includeAbsent bool) ([]models.DeviceInterface, error)
	RequestRefresh(ctx context.Context, id uuid.UUID) error
}

type deviceProber interface {
	Identify(ctx context.Context, host string, port int, credentialID uuid.UUID, timeout time.Duration, retries int) (snmp.System, error)
	UsableAt(ctx context.Context, credentialID, siteID uuid.UUID) (bool, error)
}

// RegisterDeviceRoutes mounts /devices. Every route is gated on the device's
// site through SiteAccess: readonly to read, editable to change.
func RegisterDeviceRoutes(rg *gin.RouterGroup, devices deviceStore, prober deviceProber, sites siteAccessChecker, audit auditRecorder) {
	g := rg.Group("/devices")
	g.GET("", listDevicesHandler(devices))
	g.POST("", createDeviceHandler(devices, sites, audit))
	// 10 per minute per user: it sends SNMP to an address the caller chooses.
	g.POST("/test", NewRateLimiter(10, time.Minute, 10).Middleware("snmp-test", ByUser), testDeviceHandler(prober, sites))
	g.GET("/:id", getDeviceHandler(devices, sites))
	g.PUT("/:id", updateDeviceHandler(devices, sites, audit))
	g.DELETE("/:id", deleteDeviceHandler(devices, sites, audit))
	g.GET("/:id/interfaces", deviceInterfacesHandler(devices, sites))
	g.POST("/:id/refresh", refreshDeviceHandler(devices, sites))
}

func respondDeviceError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrDeviceNotFound):
		respondError(c, http.StatusNotFound, "device not found")
	case errors.Is(err, services.ErrDeviceHostTaken):
		respondError(c, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrCredentialNotUsable), errors.Is(err, services.ErrSiteNotFound),
		errors.Is(err, services.ErrCredentialNotFound):
		respondError(c, http.StatusBadRequest, err.Error())
	case isInternal(err):
		respondInternal(c, op, err)
	default: // validation message from NormalizeDeviceInput
		respondError(c, http.StatusBadRequest, err.Error())
	}
}

// loadDevice resolves :id and checks the caller has at least want on its
// site. A device on a site the caller cannot see is a 404.
func loadDevice(c *gin.Context, devices deviceStore, sites siteAccessChecker, want services.SiteAccessLevel) (*services.DeviceView, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid device id")
		return nil, false
	}
	d, err := devices.Get(c.Request.Context(), id)
	if err != nil {
		respondDeviceError(c, "loadDevice", err)
		return nil, false
	}
	if !requireSiteLevel(c, sites, d.SiteID, want) {
		return nil, false
	}
	return d, true
}

func deviceAudit(d *models.Device) map[string]any {
	return map[string]any{"name": d.Name, "host": d.Host, "port": d.Port, "site_id": d.SiteID, "enabled": d.Enabled}
}

func listDevicesHandler(devices deviceStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, isAdmin, _ := GetUserFromContext(c)
		f := services.DeviceFilter{Status: c.Query("status")}
		if raw := c.Query("site_id"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				respondError(c, http.StatusBadRequest, "site_id must be a UUID")
				return
			}
			f.SiteID = &id
		}
		list, err := devices.List(c.Request.Context(), userID, isAdmin, f)
		if err != nil {
			respondInternal(c, "listDevices", err)
			return
		}
		if list == nil {
			list = []services.DeviceView{}
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func getDeviceHandler(devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		respondSuccess(c, http.StatusOK, d)
	}
}

func createDeviceHandler(devices deviceStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in models.DeviceInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if !requireSiteLevel(c, sites, in.SiteID, services.SiteAccessEditable) {
			return
		}
		userID, _, _, _ := GetUserFromContext(c)
		d, err := devices.Create(c.Request.Context(), in, userID)
		if err != nil {
			respondDeviceError(c, "createDevice", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceCreated, models.ResourceDevice, &d.ID,
			models.AuditChanges{Summary: deviceAudit(d)})
		respondSuccess(c, http.StatusCreated, d)
	}
}

func updateDeviceHandler(devices deviceStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		var in models.DeviceInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		// Retries of 0 in a request means "default" (1); see NormalizeDeviceInput.
		if in.SiteID == uuid.Nil {
			in.SiteID = d.SiteID
		}
		if in.SiteID != d.SiteID && !requireSiteLevel(c, sites, in.SiteID, services.SiteAccessEditable) {
			return
		}
		before, after, err := devices.Update(c.Request.Context(), d.ID, in)
		if err != nil {
			respondDeviceError(c, "updateDevice", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceUpdated, models.ResourceDevice, &d.ID,
			models.AuditChanges{Before: deviceAudit(before), After: deviceAudit(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteDeviceHandler(devices deviceStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		deleted, err := devices.Delete(c.Request.Context(), d.ID)
		if err != nil {
			respondDeviceError(c, "deleteDevice", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceDeleted, models.ResourceDevice, &d.ID,
			models.AuditChanges{Summary: deviceAudit(deleted)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func deviceInterfacesHandler(devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		list, err := devices.Interfaces(c.Request.Context(), d.ID, c.Query("include_absent") == "true")
		if err != nil {
			respondInternal(c, "deviceInterfaces", err)
			return
		}
		if list == nil {
			list = []models.DeviceInterface{}
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func refreshDeviceHandler(devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		if err := devices.RequestRefresh(c.Request.Context(), d.ID); err != nil {
			respondInternal(c, "refreshDevice", err)
			return
		}
		respondSuccess(c, http.StatusAccepted, gin.H{"queued": true})
	}
}

type testDeviceRequest struct {
	SiteID       uuid.UUID `json:"site_id"`
	CredentialID uuid.UUID `json:"credential_id"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
}

// testDeviceHandler reads a device's identity before it is saved.
func testDeviceHandler(prober deviceProber, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req testDeviceRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if !requireSiteLevel(c, sites, req.SiteID, services.SiteAccessEditable) {
			return
		}
		in, err := models.NormalizeDeviceInput(models.DeviceInput{Host: req.Host, Port: req.Port})
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		usable, err := prober.UsableAt(c.Request.Context(), req.CredentialID, req.SiteID)
		if err != nil {
			respondInternal(c, "testDevice", err)
			return
		}
		if !usable {
			respondError(c, http.StatusBadRequest, services.ErrCredentialNotUsable.Error())
			return
		}
		sys, err := prober.Identify(c.Request.Context(), in.Host, in.Port, req.CredentialID, 3*time.Second, 0)
		if err != nil {
			// Reported as a result, not a server error: "no answer" is the
			// answer the user asked for.
			respondSuccess(c, http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"ok": true, "system": sys, "vendor": snmp.VendorFor(sys.ObjectID)})
	}
}
```

Add to `backend/internal/services/prober.go`:

```go
// UsableAt reports whether a credential profile may be used in a site, so Test
// connection cannot borrow another site's profile.
func (p *Prober) UsableAt(ctx context.Context, credentialID, siteID uuid.UUID) (bool, error) {
	return p.creds.UsableAt(ctx, credentialID, siteID)
}
```

- [ ] **Step 8: Run the handler tests**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./... && go test ./internal/api/ -run 'TestDevice' -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `TestDeviceAccess`, `TestDeviceTestIsRateLimited` and `TestDeviceTestChecksCredentialScope` PASS.

- [ ] **Step 9: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/internal/services/device_service.go backend/internal/services/device_service_db_test.go \
  backend/internal/services/prober.go backend/internal/services/prober_test.go \
  backend/internal/api/device_handler.go backend/internal/api/device_handler_test.go
git commit -m "feat(snmp): devices API, inventory storage and test connection

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 8: Subnet scans

**Files:**
- Create: `backend/internal/services/scan_manager.go`, `backend/internal/services/scan_manager_test.go`
- Create: `backend/internal/api/scan_handler.go`, `backend/internal/api/scan_handler_test.go`

**Interfaces:**
- Consumes: `Prober.IdentifyWith`, `ErrTargetBlocked` (Task 7); `enumerateHosts`, `maxDiscoveryHosts` (existing, `discovery_service.go`); `snmp.VendorFor`; `SNMPCredentialService.ForSite/Decrypted`; `DeviceService.HostsInSite/CreateMany`.
- Produces:
  - Errors `ErrScanRunning`, `ErrScanTooLarge`, `ErrScanNotFound`, `ErrScanNoCredentials`.
  - `type ScanCredential struct { ID uuid.UUID; Name string; Cred snmp.Credential }`.
  - `type ScanResult struct { Host string; Port int; Name, Descr, ObjectID, Vendor string; CredentialID uuid.UUID; CredentialName string; AlreadyAdded bool }` (snake_case JSON).
  - `type ScanJob struct { ID, SiteID uuid.UUID; CIDR string; Total, Done int; Running bool; Results []ScanResult; StartedAt time.Time; FinishedAt *time.Time }`.
  - `NewScanManager(id scanIdentifier) *ScanManager`; `Start(siteID uuid.UUID, cidr string, creds []ScanCredential, existing map[string]bool) (ScanJob, error)`; `Get(siteID, jobID uuid.UUID) (ScanJob, error)`.
  - `api.RegisterScanRoutes(rg, scans scanRunner, creds scanCredentials, devices scanDevices, sites siteAccessChecker)`.

- [ ] **Step 1: Write the failing scan tests**

`backend/internal/services/scan_manager_test.go`:

```go
package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// fakeIdentifier answers for (host, community) pairs; everything else times out.
type fakeIdentifier struct {
	mu      sync.Mutex
	answers map[string]string // host -> community that works
	blocked map[string]bool
	tries   map[string]int
}

func (f *fakeIdentifier) IdentifyWith(_ context.Context, host string, _ int, cred snmp.Credential, _ time.Duration, _ int) (snmp.System, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tries == nil {
		f.tries = map[string]int{}
	}
	f.tries[host]++
	if f.blocked[host] {
		return snmp.System{}, ErrTargetBlocked
	}
	if f.answers[host] == cred.Community {
		return snmp.System{Name: "sw-" + host, Descr: "EdgeSwitch", ObjectID: "1.3.6.1.4.1.4413"}, nil
	}
	return snmp.System{}, errors.New("request timeout")
}

func waitDone(t *testing.T, m *ScanManager, site, job uuid.UUID) ScanJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err := m.Get(site, job)
		if err != nil {
			t.Fatal(err)
		}
		if !j.Running {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scan did not finish")
	return ScanJob{}
}

func TestScanFindsDevicesWithFirstWorkingProfile(t *testing.T) {
	id := &fakeIdentifier{answers: map[string]string{"10.0.0.2": "public", "10.0.0.3": "private"}}
	m := NewScanManager(id)
	site := uuid.New()
	pub := ScanCredential{ID: uuid.New(), Name: "Public", Cred: snmp.Credential{Version: "2c", Community: "public"}}
	priv := ScanCredential{ID: uuid.New(), Name: "Private", Cred: snmp.Credential{Version: "2c", Community: "private"}}

	job, err := m.Start(site, "10.0.0.0/29", []ScanCredential{pub, priv}, map[string]bool{"10.0.0.2:161": true})
	if err != nil {
		t.Fatal(err)
	}
	done := waitDone(t, m, site, job.ID)
	if done.Total != 6 || done.Done != 6 || len(done.Results) != 2 || done.FinishedAt == nil {
		t.Fatalf("job %+v", done)
	}
	byHost := map[string]ScanResult{}
	for _, r := range done.Results {
		byHost[r.Host] = r
	}
	if r := byHost["10.0.0.2"]; r.CredentialID != pub.ID || !r.AlreadyAdded || r.Vendor != "Ubiquiti (EdgeSwitch)" || r.Name != "sw-10.0.0.2" {
		t.Errorf("10.0.0.2: %+v", r)
	}
	if r := byHost["10.0.0.3"]; r.CredentialID != priv.ID || r.AlreadyAdded {
		t.Errorf("10.0.0.3: %+v", r)
	}
	// .2 answered the first profile, so the second was never tried.
	if id.tries["10.0.0.2"] != 1 || id.tries["10.0.0.4"] != 2 {
		t.Errorf("tries %v", id.tries)
	}
}

func TestScanStopsTryingBlockedAddresses(t *testing.T) {
	id := &fakeIdentifier{blocked: map[string]bool{"10.0.0.1": true}}
	m := NewScanManager(id)
	site := uuid.New()
	creds := []ScanCredential{{ID: uuid.New(), Cred: snmp.Credential{Community: "a"}}, {ID: uuid.New(), Cred: snmp.Credential{Community: "b"}}}
	job, _ := m.Start(site, "10.0.0.1/32", creds, nil)
	waitDone(t, m, site, job.ID)
	if id.tries["10.0.0.1"] != 1 {
		t.Errorf("blocked address tried %d times, want 1", id.tries["10.0.0.1"])
	}
}

func TestScanLimits(t *testing.T) {
	m := NewScanManager(&fakeIdentifier{})
	site := uuid.New()
	creds := []ScanCredential{{ID: uuid.New()}}
	if _, err := m.Start(site, "10.0.0.0/21", creds, nil); !errors.Is(err, ErrScanTooLarge) {
		t.Errorf("/21: %v, want ErrScanTooLarge", err)
	}
	if _, err := m.Start(site, "not-a-cidr", creds, nil); err == nil {
		t.Error("bad CIDR accepted")
	}
	if _, err := m.Start(site, "10.0.0.0/30", nil, nil); !errors.Is(err, ErrScanNoCredentials) {
		t.Errorf("no credentials: %v", err)
	}

	// One scan per site at a time.
	slow := &fakeIdentifier{}
	m = NewScanManager(slow)
	m.perHost = 0
	m.hold = make(chan struct{})
	if _, err := m.Start(site, "10.0.0.0/30", creds, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(site, "10.0.0.0/30", creds, nil); !errors.Is(err, ErrScanRunning) {
		t.Errorf("second scan: %v, want ErrScanRunning", err)
	}
	if _, err := m.Start(uuid.New(), "10.0.0.0/30", creds, nil); err != nil {
		t.Errorf("another site's scan was blocked: %v", err)
	}
	close(m.hold)
}

func TestScanJobsExpireAndStaySiteScoped(t *testing.T) {
	m := NewScanManager(&fakeIdentifier{})
	site := uuid.New()
	job, _ := m.Start(site, "10.0.0.1/32", []ScanCredential{{ID: uuid.New()}}, nil)
	waitDone(t, m, site, job.ID)

	if _, err := m.Get(uuid.New(), job.ID); !errors.Is(err, ErrScanNotFound) {
		t.Errorf("another site read the job: %v", err)
	}
	later := time.Now().Add(61 * time.Minute)
	m.now = func() time.Time { return later }
	if _, err := m.Get(site, job.ID); !errors.Is(err, ErrScanNotFound) {
		t.Errorf("job still there after an hour: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./internal/services/ 2>&1 | head -2`
Expected: `undefined: NewScanManager`.

- [ ] **Step 3: Write the scan manager**

`backend/internal/services/scan_manager.go`:

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

var (
	ErrScanRunning       = errors.New("a scan is already running for this site")
	ErrScanTooLarge      = fmt.Errorf("subnet too large to scan (at most %d addresses, a /22)", maxDiscoveryHosts)
	ErrScanNotFound      = errors.New("scan not found (scans are kept for an hour after they finish)")
	ErrScanNoCredentials = errors.New("choose at least one credential profile to scan with")
)

const (
	scanConcurrency = 64
	scanPerHost     = time.Second
	scanJobTTL      = time.Hour
	scanMaxDuration = 15 * time.Minute
)

// ScanCredential is a decrypted profile to try, with its identity.
type ScanCredential struct {
	ID   uuid.UUID
	Name string
	Cred snmp.Credential
}

// ScanResult is one address that answered.
type ScanResult struct {
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	Name           string    `json:"name"`
	Descr          string    `json:"descr"`
	ObjectID       string    `json:"object_id"`
	Vendor         string    `json:"vendor"`
	CredentialID   uuid.UUID `json:"credential_id"`
	CredentialName string    `json:"credential_name"`
	AlreadyAdded   bool      `json:"already_added"`
}

// ScanJob is a scan's progress and results so far.
type ScanJob struct {
	ID         uuid.UUID    `json:"id"`
	SiteID     uuid.UUID    `json:"site_id"`
	CIDR       string       `json:"cidr"`
	Total      int          `json:"total"`
	Done       int          `json:"done"`
	Running    bool         `json:"running"`
	Results    []ScanResult `json:"results"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at"`
}

type scanIdentifier interface {
	IdentifyWith(ctx context.Context, host string, port int, cred snmp.Credential, timeout time.Duration, retries int) (snmp.System, error)
}

// ScanManager runs subnet scans as in-memory background jobs: one per site at
// a time, kept for an hour after finishing. A restart loses them.
type ScanManager struct {
	mu      sync.Mutex
	jobs    map[uuid.UUID]*ScanJob
	running map[uuid.UUID]uuid.UUID // site -> job
	id      scanIdentifier
	now     func() time.Time
	perHost time.Duration
	hold    chan struct{} // tests: blocks workers until closed
}

func NewScanManager(id scanIdentifier) *ScanManager {
	return &ScanManager{jobs: map[uuid.UUID]*ScanJob{}, running: map[uuid.UUID]uuid.UUID{},
		id: id, now: time.Now, perHost: scanPerHost}
}

// Start begins a scan and returns its initial state.
func (m *ScanManager) Start(siteID uuid.UUID, cidr string, creds []ScanCredential, existing map[string]bool) (ScanJob, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return ScanJob{}, fmt.Errorf("%q is not a subnet in CIDR form, e.g. 10.20.0.0/24", cidr)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return ScanJob{}, errors.New("only IPv4 subnets can be scanned")
	}
	if 1<<uint(bits-ones) > maxDiscoveryHosts {
		return ScanJob{}, ErrScanTooLarge
	}
	if len(creds) == 0 {
		return ScanJob{}, ErrScanNoCredentials
	}
	hosts, err := enumerateHosts(ipnet)
	if err != nil {
		return ScanJob{}, err
	}

	m.mu.Lock()
	m.purgeLocked()
	if _, busy := m.running[siteID]; busy {
		m.mu.Unlock()
		return ScanJob{}, ErrScanRunning
	}
	job := &ScanJob{ID: uuid.New(), SiteID: siteID, CIDR: ipnet.String(), Total: len(hosts), Running: true,
		Results: []ScanResult{}, StartedAt: m.now()}
	m.jobs[job.ID] = job
	m.running[siteID] = job.ID
	snapshot := *job
	m.mu.Unlock()

	// Not the request's context: the scan outlives the request that started it.
	go m.run(job, hosts, creds, existing)
	return snapshot, nil
}

func (m *ScanManager) run(job *ScanJob, hosts []string, creds []ScanCredential, existing map[string]bool) {
	ctx, cancel := context.WithTimeout(context.Background(), scanMaxDuration)
	defer cancel()
	work := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < scanConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for host := range work {
				if m.hold != nil {
					<-m.hold
				}
				result, found := m.probe(ctx, host, creds, existing)
				m.mu.Lock()
				job.Done++
				if found {
					job.Results = append(job.Results, result)
				}
				m.mu.Unlock()
			}
		}()
	}
	for _, h := range hosts {
		work <- h
	}
	close(work)
	wg.Wait()

	m.mu.Lock()
	now := m.now()
	job.Running, job.FinishedAt = false, &now
	delete(m.running, job.SiteID)
	m.mu.Unlock()
}

// probe tries each profile in order and stops at the first that answers. A
// blocked address is not tried again with other profiles.
func (m *ScanManager) probe(ctx context.Context, host string, creds []ScanCredential, existing map[string]bool) (ScanResult, bool) {
	for _, c := range creds {
		sys, err := m.id.IdentifyWith(ctx, host, 161, c.Cred, m.perHost, 0)
		if errors.Is(err, ErrTargetBlocked) {
			return ScanResult{}, false
		}
		if err != nil {
			continue
		}
		return ScanResult{
			Host: host, Port: 161, Name: sys.Name, Descr: sys.Descr, ObjectID: sys.ObjectID,
			Vendor: snmp.VendorFor(sys.ObjectID), CredentialID: c.ID, CredentialName: c.Name,
			AlreadyAdded: existing[fmt.Sprintf("%s:%d", host, 161)],
		}, true
	}
	return ScanResult{}, false
}

// Get returns a copy of a site's scan.
func (m *ScanManager) Get(siteID, jobID uuid.UUID) (ScanJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeLocked()
	job, ok := m.jobs[jobID]
	if !ok || job.SiteID != siteID {
		return ScanJob{}, ErrScanNotFound
	}
	out := *job
	out.Results = append([]ScanResult(nil), job.Results...)
	return out, nil
}

func (m *ScanManager) purgeLocked() {
	now := m.now()
	for id, j := range m.jobs {
		if j.FinishedAt != nil && now.Sub(*j.FinishedAt) > scanJobTTL {
			delete(m.jobs, id)
		}
	}
}
```

- [ ] **Step 4: Run the scan tests**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/services/ -run TestScan -race -v 2>&1 | grep -E '^(--- |ok|FAIL|WARNING)'`
Expected: four PASS, no data race.

- [ ] **Step 5: Write the failing handler test**

`backend/internal/api/scan_handler_test.go`:

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
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type fakeScans struct {
	startErr error
	started  int
}

func (f *fakeScans) Start(site uuid.UUID, cidr string, creds []services.ScanCredential, _ map[string]bool) (services.ScanJob, error) {
	if f.startErr != nil {
		return services.ScanJob{}, f.startErr
	}
	f.started++
	return services.ScanJob{ID: uuid.New(), SiteID: site, CIDR: cidr, Running: true}, nil
}
func (f *fakeScans) Get(uuid.UUID, uuid.UUID) (services.ScanJob, error) {
	return services.ScanJob{}, services.ErrScanNotFound
}

type fakeScanCreds struct{ ids []uuid.UUID }

func (f fakeScanCreds) ForSite(context.Context, uuid.UUID) ([]services.CredentialOption, error) {
	out := []services.CredentialOption{}
	for _, id := range f.ids {
		out = append(out, services.CredentialOption{ID: id, Name: "p"})
	}
	return out, nil
}
func (f fakeScanCreds) Decrypted(context.Context, uuid.UUID) (snmp.Credential, error) {
	return snmp.Credential{Version: "2c", Community: "public"}, nil
}

type fakeScanDevices struct{ added int }

func (f *fakeScanDevices) HostsInSite(context.Context, uuid.UUID) (map[string]bool, error) { return nil, nil }
func (f *fakeScanDevices) CreateMany(_ context.Context, _ uuid.UUID, in []models.DeviceInput, _ uuid.UUID) ([]models.Device, []services.BulkAddError) {
	f.added += len(in)
	return make([]models.Device, len(in)), nil
}

func scanRouter(scans *fakeScans, devs *fakeScanDevices, creds fakeScanCreds, level services.SiteAccessLevel) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	RegisterScanRoutes(r.Group("/api/v1"), scans, creds, devs, fakeSiteAccess{level}, &fakeAudit{})
	return r
}

func TestScanRoutes(t *testing.T) {
	site := uuid.New().String()
	cred := uuid.New()
	start := "/api/v1/sites/" + site + "/scans"
	add := "/api/v1/sites/" + site + "/devices"

	// Readonly users can neither scan nor bulk-add.
	scans, devs := &fakeScans{}, &fakeScanDevices{}
	r := scanRouter(scans, devs, fakeScanCreds{ids: []uuid.UUID{cred}}, services.SiteAccessReadonly)
	if w := do(r, http.MethodPost, start, map[string]any{"cidr": "10.0.0.0/24"}); w.Code != http.StatusForbidden {
		t.Errorf("readonly scan: %d", w.Code)
	}
	if w := do(r, http.MethodPost, add, map[string]any{"devices": []map[string]any{{"host": "10.0.0.2", "credential_id": cred}}}); w.Code != http.StatusForbidden || devs.added != 0 {
		t.Errorf("readonly add: %d, added %d", w.Code, devs.added)
	}

	// Editable: default is every profile available to the site; a profile
	// from elsewhere is refused.
	r = scanRouter(scans, devs, fakeScanCreds{ids: []uuid.UUID{cred}}, services.SiteAccessEditable)
	if w := do(r, http.MethodPost, start, map[string]any{"cidr": "10.0.0.0/24"}); w.Code != http.StatusAccepted || scans.started != 1 {
		t.Errorf("editable scan: %d, started %d", w.Code, scans.started)
	}
	if w := do(r, http.MethodPost, start, map[string]any{"cidr": "10.0.0.0/24", "credential_ids": []uuid.UUID{uuid.New()}}); w.Code != http.StatusBadRequest {
		t.Errorf("foreign profile: %d, want 400", w.Code)
	}
	if w := do(r, http.MethodGet, start+"/"+uuid.New().String(), nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown job: %d, want 404", w.Code)
	}
	if w := do(r, http.MethodPost, add, map[string]any{"devices": []map[string]any{{"host": "10.0.0.2", "credential_id": cred}}}); w.Code != http.StatusOK || devs.added != 1 {
		t.Errorf("bulk add: %d, added %d", w.Code, devs.added)
	}

	busy := &fakeScans{startErr: services.ErrScanRunning}
	r = scanRouter(busy, devs, fakeScanCreds{ids: []uuid.UUID{cred}}, services.SiteAccessEditable)
	if w := do(r, http.MethodPost, start, map[string]any{"cidr": "10.0.0.0/24"}); w.Code != http.StatusConflict {
		t.Errorf("scan running: %d, want 409", w.Code)
	}
}
```

- [ ] **Step 6: Write the handlers**

`backend/internal/api/scan_handler.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type scanRunner interface {
	Start(siteID uuid.UUID, cidr string, creds []services.ScanCredential, existing map[string]bool) (services.ScanJob, error)
	Get(siteID, jobID uuid.UUID) (services.ScanJob, error)
}

type scanCredentials interface {
	ForSite(ctx context.Context, siteID uuid.UUID) ([]services.CredentialOption, error)
	Decrypted(ctx context.Context, id uuid.UUID) (snmp.Credential, error)
}

type scanDevices interface {
	HostsInSite(ctx context.Context, siteID uuid.UUID) (map[string]bool, error)
	CreateMany(ctx context.Context, siteID uuid.UUID, in []models.DeviceInput, by uuid.UUID) ([]models.Device, []services.BulkAddError)
}

// RegisterScanRoutes mounts subnet scans and bulk add; both need editable
// access to the site.
func RegisterScanRoutes(rg *gin.RouterGroup, scans scanRunner, creds scanCredentials, devices scanDevices, sites siteAccessChecker, audit auditRecorder) {
	rg.POST("/sites/:id/scans", startScanHandler(scans, creds, devices, sites))
	rg.GET("/sites/:id/scans/:job", getScanHandler(scans, sites))
	rg.POST("/sites/:id/devices", bulkAddDevicesHandler(devices, sites, audit))
}

type startScanRequest struct {
	CIDR          string      `json:"cidr"`
	CredentialIDs []uuid.UUID `json:"credential_ids"`
}

func startScanHandler(scans scanRunner, creds scanCredentials, devices scanDevices, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		siteID, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, siteID, services.SiteAccessEditable) {
			return
		}
		var req startScanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		ctx := c.Request.Context()
		options, err := creds.ForSite(ctx, siteID)
		if err != nil {
			respondInternal(c, "startScan", err)
			return
		}
		allowed := map[uuid.UUID]services.CredentialOption{}
		for _, o := range options {
			allowed[o.ID] = o
		}
		ids := req.CredentialIDs
		if len(ids) == 0 {
			for _, o := range options {
				ids = append(ids, o.ID)
			}
		}
		var scanCreds []services.ScanCredential
		for _, id := range ids {
			o, ok := allowed[id]
			if !ok {
				respondError(c, http.StatusBadRequest, services.ErrCredentialNotUsable.Error())
				return
			}
			cred, err := creds.Decrypted(ctx, id)
			if err != nil {
				respondInternal(c, "startScan", err)
				return
			}
			scanCreds = append(scanCreds, services.ScanCredential{ID: id, Name: o.Name, Cred: cred})
		}
		existing, err := devices.HostsInSite(ctx, siteID)
		if err != nil {
			respondInternal(c, "startScan", err)
			return
		}
		job, err := scans.Start(siteID, req.CIDR, scanCreds, existing)
		switch {
		case errors.Is(err, services.ErrScanRunning):
			respondError(c, http.StatusConflict, err.Error())
		case err != nil:
			respondError(c, http.StatusBadRequest, err.Error())
		default:
			respondSuccess(c, http.StatusAccepted, job)
		}
	}
}

func getScanHandler(scans scanRunner, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		siteID, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, siteID, services.SiteAccessEditable) {
			return
		}
		jobID, err := uuid.Parse(c.Param("job"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid scan id")
			return
		}
		job, err := scans.Get(siteID, jobID)
		if err != nil {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, job)
	}
}

type bulkAddRequest struct {
	Devices []models.DeviceInput `json:"devices"`
}

func bulkAddDevicesHandler(devices scanDevices, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		siteID, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, siteID, services.SiteAccessEditable) {
			return
		}
		var req bulkAddRequest
		if err := c.ShouldBindJSON(&req); err != nil || len(req.Devices) == 0 {
			respondError(c, http.StatusBadRequest, "send at least one device")
			return
		}
		if len(req.Devices) > maxBulkAdd {
			respondError(c, http.StatusBadRequest, "at most 1024 devices per request")
			return
		}
		userID, _, _, _ := GetUserFromContext(c)
		added, failed := devices.CreateMany(c.Request.Context(), siteID, req.Devices, userID)
		// One entry for the batch: a scan can add hundreds of devices at once.
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceCreated, models.ResourceDevice, nil,
			models.AuditChanges{Summary: map[string]any{"site_id": siteID, "added": len(added), "failed": len(failed)}})
		if added == nil {
			added = []models.Device{}
		}
		if failed == nil {
			failed = []services.BulkAddError{}
		}
		respondSuccess(c, http.StatusOK, gin.H{"added": added, "failed": failed})
	}
}

// maxBulkAdd matches the largest scan (a /22).
const maxBulkAdd = 1024
```

- [ ] **Step 7: Run and commit**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./... && go test ./internal/api/ -run TestScanRoutes -v 2>&1 | tail -2 && go test ./internal/... 2>&1 | grep -v 'no test files'`
Expected: PASS; all packages `ok`.

```bash
cd /home/sysadmin/sentinel
git add backend/internal/services/scan_manager.go backend/internal/services/scan_manager_test.go \
  backend/internal/api/scan_handler.go backend/internal/api/scan_handler_test.go
git commit -m "feat(snmp): per-site subnet scans as background jobs, with bulk add

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Wire it into the backend

**Files:**
- Modify: `backend/cmd/sentinel/main.go`
- Modify: `backend/internal/models/setting.go`

**Interfaces:**
- Consumes: everything above.
- Produces: a running poller and the routes of Tasks 6–8; setting `models.SettingSNMPPollWorkers = "snmp_poll_workers"`.

- [ ] **Step 1: Setting key**

In `backend/internal/models/setting.go`, with the other `Setting…` constants:

```go
	// SettingSNMPPollWorkers sizes the SNMP poller's worker pool (default 16).
	// Read at startup; a change takes effect on restart.
	SettingSNMPPollWorkers = "snmp_poll_workers"
```

- [ ] **Step 2: Services and routes**

In `backend/cmd/sentinel/main.go`, after `siteService := services.NewSiteService(db)`:

```go
	snmpCredentialService := services.NewSNMPCredentialService(db)
	deviceService := services.NewDeviceService(db, snmpCredentialService, incidentService)
	snmpClient := snmp.GoSNMPClient{}
	prober := services.NewProber(snmpCredentialService, snmpClient)
	scanManager := services.NewScanManager(prober)
```

After `api.RegisterSiteRoutes(...)`:

```go
	api.RegisterSNMPCredentialRoutes(v1, snmpCredentialService, siteService, auditService, authService)
	api.RegisterDeviceRoutes(v1, deviceService, prober, siteService, auditService)
	api.RegisterScanRoutes(v1, scanManager, snmpCredentialService, deviceService, siteService, auditService)
```

Beside the other background loops (`go agentService.StartOfflineSweep(loopCtx)`):

```go
	pollWorkers := settingsService.GetInt(context.Background(), models.SettingSNMPPollWorkers, 16)
	devicePoller := services.NewDevicePoller(deviceService, snmpClient, incidentService, notificationManager, pollWorkers)
	go devicePoller.Start(loopCtx)
```

Add the import `"github.com/Stevy2191/Sentinel/backend/internal/snmp"`.

Check the route prefixes do not collide: `GET /sites/:id/credentials`, `/sites/:id/scans…` and `POST /sites/:id/devices` join phase 0's `/sites/:id` and `/sites/:id/shares` on the same tree; gin panics at startup on a conflict, so Step 3 proves it.

- [ ] **Step 3: Build and start it**

Run: `cd /home/sysadmin/sentinel/backend && go build ./... && go vet ./... && go test ./internal/... 2>&1 | grep -v 'no test files'`
Expected: all `ok`.

Start the real server against a throwaway database to prove the router and poller start (a gin route conflict panics here, not in unit tests):

```bash
cd /home/sysadmin/sentinel/backend
name=sentinel-smoke-$$
docker run -d --rm --name $name -e POSTGRES_USER=sentinel -e POSTGRES_PASSWORD=test -e POSTGRES_DB=sentinel \
  -p 127.0.0.1:55499:5432 timescale/timescaledb:2.30.1-pg16 \
  postgres -c shared_preload_libraries=timescaledb -c timescaledb.telemetry_level=off >/dev/null
sleep 8
DB_HOST=127.0.0.1 DB_PORT=55499 DB_USER=sentinel DB_PASSWORD=test DB_NAME=sentinel PORT=38999 \
  MIGRATIONS_DIR=./migrations timeout 15 go run ./cmd/sentinel 2>&1 | grep -E 'poller started|panic|fatal|listening' | head
docker stop $name >/dev/null
```

Expected: a `[snmp] poller started with 16 workers` line and no `panic`. (`PORT`, `MIGRATIONS_DIR` and `DB_*` are the names `main.go` and `database.NewDB` read.)

- [ ] **Step 4: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/cmd/sentinel/main.go backend/internal/models/setting.go
git commit -m "feat(snmp): start the device poller and mount the network API

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: SNMP simulator and end-to-end client tests

**Files:**
- Create: `deploy/snmpsim/Dockerfile`, `deploy/snmpsim/gen_data.py`, `deploy/snmpsim/run.sh`, `deploy/snmpsim/README.md`
- Create: `deploy/snmpsim/data/` (generated `.snmprec` files, committed)
- Create: `backend/internal/snmp/simulator_test.go`

**Interfaces:**
- Produces: a container `sentinel-snmpsim` answering on UDP `127.0.0.1:1161–1190` (host) and on the sandbox network as `sentinel-snmpsim:1161–1190`. Communities select the simulated device: `edgeswitch` (52 ports, ENTITY-MIB), `cisco` (26 ports), `radio` (v1-style: no ifXTable, no ENTITY-MIB). v3 users: `sentinel-auth` (SHA, `authpass123`) and `sentinel-priv` (SHA/AES, `authpass123`/`privpass123`), both answering with the EdgeSwitch data. Env `SENTINEL_TEST_SNMPSIM=127.0.0.1:1161` enables the Go tests.

- [ ] **Step 1: Generate the simulated devices**

`deploy/snmpsim/gen_data.py`:

```python
#!/usr/bin/env python3
"""Writes the simulator's .snmprec files: OID|TAG|VALUE lines, OID-sorted.

Tags: 2 INTEGER, 4 OCTET STRING, 4x hex OCTET STRING, 6 OID, 65 Counter32,
66 Gauge32, 67 TimeTicks, 70 Counter64.
"""
import os

OUT = os.path.join(os.path.dirname(__file__), "data")


def oid_key(line):
    return tuple(int(x) for x in line.split("|", 1)[0].split("."))


def system(descr, objid, name, location, uptime_ticks):
    return [
        f"1.3.6.1.2.1.1.1.0|4|{descr}",
        f"1.3.6.1.2.1.1.2.0|6|{objid}",
        f"1.3.6.1.2.1.1.3.0|67|{uptime_ticks}",
        "1.3.6.1.2.1.1.4.0|4|noc@example.test",
        f"1.3.6.1.2.1.1.5.0|4|{name}",
        f"1.3.6.1.2.1.1.6.0|4|{location}",
    ]


def ports(n, name_fmt, descr_fmt, speed_bps, xtable=True, mac_base=0x788A20000000):
    rows = []
    for i in range(1, n + 1):
        up = 1 if i % 3 else 2  # every third port is down
        admin = 2 if i == n else 1  # last port administratively down
        t = "1.3.6.1.2.1.2.2.1"
        rows += [
            f"{t}.1.{i}|2|{i}",
            f"{t}.2.{i}|4|{descr_fmt.format(i=i)}",
            f"{t}.3.{i}|2|6",
            f"{t}.5.{i}|66|{min(speed_bps, 4294967295)}",
            f"{t}.6.{i}|4x|{mac_base + i:012x}",
            f"{t}.7.{i}|2|{admin}",
            f"{t}.8.{i}|2|{up if admin == 1 else 2}",
            f"{t}.9.{i}|67|{1000 * i}",
        ]
        if xtable:
            x = "1.3.6.1.2.1.31.1.1.1"
            rows += [
                f"{x}.1.{i}|4|{name_fmt.format(i=i)}",
                f"{x}.15.{i}|66|{speed_bps // 1_000_000}",
                f"{x}.18.{i}|4|{'Uplink to MDF' if i == 1 else ''}",
            ]
    return rows


def entity(model, serial):
    e = "1.3.6.1.2.1.47.1.1.1.1"
    return [f"{e}.5.1|2|3", f"{e}.11.1|4|{serial}", f"{e}.13.1|4|{model}"]


def write(name, lines):
    os.makedirs(OUT, exist_ok=True)
    with open(os.path.join(OUT, name + ".snmprec"), "w") as f:
        f.write("\n".join(sorted(lines, key=oid_key)) + "\n")


edgeswitch = (system("EdgeSwitch 48-Port 500W, 1.9.3.5372984", "1.3.6.1.4.1.4413", "sim-edgeswitch", "Simulator rack", 123456789)
              + ports(52, "0/{i}", "Slot: 0 Port: {i} Gigabit - Level", 1_000_000_000)
              + entity("ES-48-500W", "SIMSERIAL001"))
cisco = (system("Cisco IOS Software, C2960X Software (C2960X-UNIVERSALK9-M), Version 15.2(7)E8", "1.3.6.1.4.1.9.1.1208",
                "sim-cisco", "Simulator rack", 98765432)
         + ports(26, "Gi1/0/{i}", "GigabitEthernet1/0/{i}", 1_000_000_000, mac_base=0x00AABB000000)
         + entity("WS-C2960X-24TS-L", "SIMSERIAL002"))
radio = (system("Cambium ePMP 1000", "1.3.6.1.4.1.17713.21", "sim-radio", "Tower 3", 5555555)
         + ports(2, "", "eth{i}", 100_000_000, xtable=False, mac_base=0x000456000000))

write("edgeswitch", edgeswitch)
write("public", edgeswitch)  # v3 with an empty context name reads this one
write("cisco", cisco)
write("radio", radio)
print("wrote", sorted(os.listdir(OUT)))
```

Run: `cd /home/sysadmin/sentinel/deploy/snmpsim && python3 gen_data.py && head -3 data/edgeswitch.snmprec && wc -l data/*.snmprec`
Expected: four files; `edgeswitch.snmprec` starts with `1.3.6.1.2.1.1.1.0|4|EdgeSwitch…`.

- [ ] **Step 2: The simulator container**

`deploy/snmpsim/Dockerfile`:

```dockerfile
# SNMP simulator for development and tests only. Never part of a release.
FROM python:3.12-alpine
# snmpsim is maintained by LeXtudio; pinned to the version current when this
# was written (look it up with `pip index versions snmpsim` and pin exactly).
ARG SNMPSIM_VERSION
RUN pip install --no-cache-dir "snmpsim==${SNMPSIM_VERSION}"
RUN adduser -D snmpsim
COPY data /usr/local/snmpsim/data
USER snmpsim
ENTRYPOINT ["snmpsim-command-responder", "--data-dir=/usr/local/snmpsim/data"]
```

`deploy/snmpsim/run.sh`:

```bash
#!/usr/bin/env bash
# Builds and starts the simulator: UDP 1161-1190 on the host's loopback, and
# on the sandbox's compose network as sentinel-snmpsim (for the backend).
set -euo pipefail
cd "$(dirname "$0")"
: "${SNMPSIM_VERSION:?set SNMPSIM_VERSION to the pinned snmpsim version}"
docker build -q --build-arg SNMPSIM_VERSION="$SNMPSIM_VERSION" -t sentinel-snmpsim:dev . >/dev/null
docker rm -f sentinel-snmpsim >/dev/null 2>&1 || true

endpoints=()
ports=()
for p in $(seq 1161 1190); do
  endpoints+=("--agent-udpv4-endpoint=0.0.0.0:$p")
  ports+=(-p "127.0.0.1:$p:$p/udp")
done

docker run -d --name sentinel-snmpsim "${ports[@]}" sentinel-snmpsim:dev \
  "${endpoints[@]}" \
  --v3-user=sentinel-auth --v3-auth-key=authpass123 --v3-auth-proto=SHA \
  --v3-user=sentinel-priv --v3-auth-key=authpass123 --v3-auth-proto=SHA \
    --v3-priv-key=privpass123 --v3-priv-proto=AES \
  >/dev/null

if docker network inspect sentinel-dev_default >/dev/null 2>&1; then
  docker network connect sentinel-dev_default sentinel-snmpsim
fi
echo "sentinel-snmpsim running on UDP 1161-1190"
```

Run:

```bash
cd /home/sysadmin/sentinel/deploy/snmpsim && chmod +x run.sh
docker run --rm python:3.12-alpine pip index versions snmpsim 2>/dev/null | head -2
```

Pin the newest listed version: set it as the default of `ARG SNMPSIM_VERSION=<version>` in the Dockerfile, and export it when running (`SNMPSIM_VERSION=<version> ./run.sh`).

Verify from the host (install `snmp` tools only if absent; otherwise use a throwaway container: `docker run --rm --network host alpine sh -c "apk add -q net-snmp-tools && snmpget -v2c -c edgeswitch 127.0.0.1:1161 1.3.6.1.2.1.1.5.0"`):
Expected: `STRING: "sim-edgeswitch"`. Also `snmpget -v3 -l authPriv -u sentinel-priv -a SHA -A authpass123 -x AES -X privpass123 127.0.0.1:1161 1.3.6.1.2.1.1.5.0` → `sim-edgeswitch`. If v3 answers `No Such Object` instead, the empty context did not map to `public.snmprec`: read `snmpsim-command-responder --help` for the context/community mapping of this version and adjust the data file name, recording the rule in the README.

`deploy/snmpsim/README.md`: one short paragraph: what it is (development/test only), how to start it (`SNMPSIM_VERSION=<pinned> ./run.sh`), the communities and v3 users above, how to regenerate data (`python3 gen_data.py`), and that `SENTINEL_TEST_SNMPSIM=127.0.0.1:1161` enables `go test ./internal/snmp/ -run TestSim`.

- [ ] **Step 3: End-to-end client tests**

`backend/internal/snmp/simulator_test.go`:

```go
package snmp

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

// simTarget returns a target on the simulator, skipping unless
// SENTINEL_TEST_SNMPSIM (host:port) is set. See deploy/snmpsim.
func simTarget(t *testing.T, cred Credential) Target {
	t.Helper()
	addr := os.Getenv("SENTINEL_TEST_SNMPSIM")
	if addr == "" {
		t.Skip("SENTINEL_TEST_SNMPSIM not set; start deploy/snmpsim/run.sh")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	return Target{Host: host, Port: uint16(port), Credential: cred, Timeout: 2 * time.Second, Retries: 0}
}

func TestSimV2cInventory(t *testing.T) {
	inv, err := ReadInventory(context.Background(), GoSNMPClient{}, simTarget(t, Credential{Version: "2c", Community: "edgeswitch"}))
	if err != nil {
		t.Fatal(err)
	}
	if inv.System.Name != "sim-edgeswitch" || inv.Vendor != "Ubiquiti (EdgeSwitch)" || inv.Model != "ES-48-500W" ||
		len(inv.Interfaces) != 52 || inv.Interfaces[0].Alias != "Uplink to MDF" || inv.Interfaces[0].SpeedBps != 1_000_000_000 {
		t.Errorf("inventory: %+v (interfaces %d)", inv.System, len(inv.Interfaces))
	}
}

// v1 has no GETBULK: the walk must use GETNEXT, and a device without ifXTable
// still lists its ports.
func TestSimV1WalkWithoutBulk(t *testing.T) {
	inv, err := ReadInventory(context.Background(), GoSNMPClient{}, simTarget(t, Credential{Version: "1", Community: "radio"}))
	if err != nil {
		t.Fatal(err)
	}
	if inv.Vendor != "Cambium Networks" || len(inv.Interfaces) != 2 || inv.Interfaces[0].Name != "eth1" || inv.Model != "" {
		t.Errorf("v1 radio: %+v %+v", inv.System, inv.Interfaces)
	}
}

func TestSimV3(t *testing.T) {
	for _, cred := range []Credential{
		{Version: "3", Username: "sentinel-auth", AuthProtocol: "SHA", AuthPassword: "authpass123", PrivProtocol: "none"},
		{Version: "3", Username: "sentinel-priv", AuthProtocol: "SHA", AuthPassword: "authpass123", PrivProtocol: "AES", PrivPassword: "privpass123"},
	} {
		sys, err := Identify(context.Background(), GoSNMPClient{}, simTarget(t, cred))
		if err != nil || sys.Name != "sim-edgeswitch" {
			t.Errorf("%s: %+v %v", cred.Username, sys, err)
		}
	}
	bad := Credential{Version: "3", Username: "sentinel-priv", AuthProtocol: "SHA", AuthPassword: "wrongpass99", PrivProtocol: "AES", PrivPassword: "privpass123"}
	if _, err := Identify(context.Background(), GoSNMPClient{}, simTarget(t, bad)); err == nil {
		t.Error("wrong v3 password answered")
	}
}

// A wrong community and a closed port both fail, and within the timeout.
func TestSimFailures(t *testing.T) {
	tg := simTarget(t, Credential{Version: "2c", Community: "no-such-community"})
	start := time.Now()
	if _, err := Identify(context.Background(), GoSNMPClient{}, tg); err == nil {
		t.Error("wrong community answered")
	}
	tg.Port = 1199 // nothing listens there
	if _, err := Identify(context.Background(), GoSNMPClient{}, tg); err == nil {
		t.Error("closed port answered")
	}
	if time.Since(start) > 6*time.Second {
		t.Errorf("failures took %s; timeouts not honoured", time.Since(start))
	}
}
```

Run: `cd /home/sysadmin/sentinel/backend && SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 go test ./internal/snmp/ -run TestSim -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: four PASS. Without the variable they SKIP.

If `TestSimV1WalkWithoutBulk` reports ports from another file, snmpsim served `radio` data under v1 differently than expected: check `snmpget -v1 -c radio 127.0.0.1:1161 1.3.6.1.2.1.1.5.0` returns `sim-radio`.

- [ ] **Step 4: Commit**

```bash
cd /home/sysadmin/sentinel
git add deploy/snmpsim backend/internal/snmp/simulator_test.go
git commit -m "test(snmp): snmpsim simulator and end-to-end v1/v2c/v3 client tests

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 11: Frontend data layer, navigation and routes

**Files:**
- Create: `frontend/src/hooks/useSnmpCredentials.ts`, `frontend/src/hooks/useDevices.ts`, `frontend/src/hooks/useSiteScan.ts`
- Modify: `frontend/src/components/Layout.tsx` (`networkNav`), `frontend/src/App.tsx` (routes), `frontend/src/hooks/useIncidents.ts` (types and filters)
- Create (placeholders completed in Tasks 12–13): `frontend/src/pages/network/Credentials.tsx`, `Devices.tsx`, `DeviceDetail.tsx`

**Interfaces:**
- Consumes: the API of Tasks 3 and 6–8.
- Produces (`@/hooks/useSnmpCredentials`): `SnmpVersion`, `AUTH_PROTOCOLS`, `PRIV_PROTOCOLS`, `CredentialView`, `CredentialInput`, `CredentialOption`, `useSnmpCredentials()` → `{ credentials, loading, error, refetch, create, update, remove }`, `useSiteCredentialOptions(siteId, enabled)` → `{ options, loading }`.
- Produces (`@/hooks/useDevices`): `DeviceStatus`, `Device`, `DeviceInput`, `DeviceInterface`, `TestResult`, `useDevices({ siteId?, status? })` → `{ devices, loading, error, refetch }`, `useDevice(id)` → `{ device, loading, notFound, refetch }`, `useDeviceInterfaces(id, includeAbsent)` → `{ interfaces, loading }`, `useDeviceActions()` → `{ create, update, remove, refresh, test, busy }`, `useDeviceSummary()` → `{ value, subtitle, hasDevices }`.
- Produces (`@/hooks/useSiteScan`): `ScanJob`, `ScanResult`, `useSiteScan(siteId)` → `{ job, start, busy, error, addDevices }`.
- `Incident` gains `device_id`, `subject_type`, `subject_name`, `subject_target`, `site_id`, `site_name`; `monitor_id` becomes `string | null`; `IncidentFilters` gains `subject?: 'monitor' | 'device'`, `deviceId?: string`.

- [ ] **Step 1: Credential hooks**

`frontend/src/hooks/useSnmpCredentials.ts`:

```ts
import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type SnmpVersion = '1' | '2c' | '3'
export const AUTH_PROTOCOLS = ['none', 'MD5', 'SHA', 'SHA224', 'SHA256', 'SHA384', 'SHA512'] as const
export const PRIV_PROTOCOLS = ['none', 'DES', 'AES', 'AES192', 'AES256'] as const

/** A profile as the API shows it: secrets are never sent, only whether set. */
export interface CredentialView {
  id: string
  name: string
  site_id: string | null
  site_name: string
  version: SnmpVersion
  username: string
  auth_protocol: string
  priv_protocol: string
  has_community: boolean
  has_auth_password: boolean
  has_priv_password: boolean
  used_by: number
  created_at: string
  updated_at: string
}

/** Secrets left empty on update keep the stored value. */
export interface CredentialInput {
  name: string
  site_id: string | null
  version: SnmpVersion
  community?: string
  username?: string
  auth_protocol?: string
  auth_password?: string
  priv_protocol?: string
  priv_password?: string
}

export interface CredentialOption {
  id: string
  name: string
  version: SnmpVersion
  site_id: string | null
}

/** Every profile (admin only; the API enforces it). */
export function useSnmpCredentials() {
  const [credentials, setCredentials] = useState<CredentialView[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<CredentialView[]>>('/snmp-credentials')
      setCredentials(data.data ?? [])
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load credential profiles')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  const create = useCallback(
    async (input: CredentialInput) => {
      const { data } = await api.post<ApiResponse<CredentialView>>('/snmp-credentials', input)
      await refetch()
      return data.data
    },
    [refetch]
  )
  const update = useCallback(
    async (id: string, input: CredentialInput) => {
      const { data } = await api.put<ApiResponse<CredentialView>>(`/snmp-credentials/${id}`, input)
      await refetch()
      return data.data
    },
    [refetch]
  )
  const remove = useCallback(
    async (id: string) => {
      await api.delete(`/snmp-credentials/${id}`)
      await refetch()
    },
    [refetch]
  )

  return { credentials, loading, error, refetch, create, update, remove }
}

/** Profiles usable in a site, by name (editors of that site). */
export function useSiteCredentialOptions(siteId: string | undefined, enabled: boolean) {
  const [options, setOptions] = useState<CredentialOption[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!siteId || !enabled) {
      setOptions([])
      return
    }
    let active = true
    setLoading(true)
    api
      .get<ApiResponse<CredentialOption[]>>(`/sites/${siteId}/credentials`)
      .then((res) => active && setOptions(res.data.data ?? []))
      .catch(() => active && setOptions([]))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [siteId, enabled])

  return { options, loading }
}
```

- [ ] **Step 2: Device hooks**

`frontend/src/hooks/useDevices.ts`:

```ts
import { useCallback, useEffect, useMemo, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type DeviceStatus = 'pending' | 'up' | 'down' | 'paused' | 'error'

export interface Device {
  id: string
  site_id: string
  credential_id: string
  name: string
  host: string
  port: number
  enabled: boolean
  poll_interval: number
  timeout_ms: number
  retries: number
  notify_channels: string[] | null
  status: DeviceStatus
  status_detail: string
  consecutive_failures: number
  last_polled_at: string | null
  last_seen_at: string | null
  last_inventory_at: string | null
  sys_name: string
  sys_descr: string
  sys_object_id: string
  sys_location: string
  sys_contact: string
  sys_uptime_seconds: number | null
  vendor: string
  model: string
  serial: string
  created_at: string
  updated_at: string
  site_name?: string
  credential_name?: string
  availability_30d?: number | null
}

export interface DeviceInput {
  site_id: string
  credential_id: string
  name: string
  host: string
  port: number
  enabled: boolean
  poll_interval: number
  timeout_ms: number
  retries: number
  notify_channels: string[] | null
}

export interface DeviceInterface {
  id: string
  if_index: number
  name: string
  descr: string
  alias: string
  if_type: number
  speed_bps: number
  mac: string
  admin_status: string
  oper_status: string
  last_change_seconds: number
  present: boolean
}

export interface TestResult {
  ok: boolean
  error?: string
  vendor?: string
  system?: { name: string; descr: string; object_id: string; location: string; contact: string; uptime_seconds: number }
}

const REFRESH_MS = 30_000

/** Devices in the sites the user can see, refreshed every 30 s. */
export function useDevices(filter: { siteId?: string; status?: DeviceStatus } = {}) {
  const [devices, setDevices] = useState<Device[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const { siteId, status } = filter

  const refetch = useCallback(async () => {
    try {
      const { data } = await api.get<ApiResponse<Device[]>>('/devices', {
        params: { ...(siteId ? { site_id: siteId } : {}), ...(status ? { status } : {}) },
      })
      setDevices(data.data ?? [])
      setError(null)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load devices')
    } finally {
      setLoading(false)
    }
  }, [siteId, status])

  useEffect(() => {
    void refetch()
    const t = window.setInterval(() => void refetch(), REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetch])

  return { devices, loading, error, refetch }
}

/** One device. notFound covers "missing" and "not in a site you can see". */
export function useDevice(id: string | undefined) {
  const [device, setDevice] = useState<Device | null>(null)
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)

  const refetch = useCallback(async () => {
    if (!id) return
    try {
      const { data } = await api.get<ApiResponse<Device>>(`/devices/${id}`)
      setDevice(data.data)
      setNotFound(false)
    } catch (err) {
      const e = err as ApiError
      if (e.status === 404 || e.status === 400) {
        setDevice(null)
        setNotFound(true)
      }
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    setLoading(true)
    setDevice(null)
    setNotFound(false)
    void refetch()
    const t = window.setInterval(() => void refetch(), REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetch])

  return { device, loading, notFound, refetch }
}

export function useDeviceInterfaces(id: string | undefined, includeAbsent: boolean) {
  const [interfaces, setInterfaces] = useState<DeviceInterface[]>([])
  const [loading, setLoading] = useState(true)

  const refetch = useCallback(async () => {
    if (!id) return
    try {
      const { data } = await api.get<ApiResponse<DeviceInterface[]>>(`/devices/${id}/interfaces`, {
        params: includeAbsent ? { include_absent: 'true' } : {},
      })
      setInterfaces(data.data ?? [])
    } finally {
      setLoading(false)
    }
  }, [id, includeAbsent])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { interfaces, loading, refetch }
}

export function useDeviceActions() {
  const [busy, setBusy] = useState(false)
  const run = useCallback(async <T,>(fn: () => Promise<T>): Promise<T> => {
    setBusy(true)
    try {
      return await fn()
    } finally {
      setBusy(false)
    }
  }, [])

  const create = useCallback(
    (input: DeviceInput) => run(async () => (await api.post<ApiResponse<Device>>('/devices', input)).data.data),
    [run]
  )
  const update = useCallback(
    (id: string, input: DeviceInput) =>
      run(async () => (await api.put<ApiResponse<Device>>(`/devices/${id}`, input)).data.data),
    [run]
  )
  const remove = useCallback((id: string) => run(async () => void (await api.delete(`/devices/${id}`))), [run])
  const refresh = useCallback(
    (id: string) => run(async () => void (await api.post(`/devices/${id}/refresh`))),
    [run]
  )
  const test = useCallback(
    (input: { site_id: string; credential_id: string; host: string; port: number }) =>
      run(async () => (await api.post<ApiResponse<TestResult>>('/devices/test', input)).data.data),
    [run]
  )

  return { create, update, remove, refresh, test, busy }
}

/** The Overview card: device count, with what needs attention. */
export function useDeviceSummary(): { value: string; subtitle: string; hasDevices: boolean } {
  const { devices, loading, error } = useDevices()
  return useMemo(() => {
    if (loading) return { value: '—', subtitle: 'loading', hasDevices: false }
    if (error || devices.length === 0) return { value: '0', subtitle: '', hasDevices: false }
    const down = devices.filter((d) => d.status === 'down').length
    const erroring = devices.filter((d) => d.status === 'error').length
    let subtitle = 'all up'
    if (down > 0) subtitle = `${down} down`
    else if (erroring > 0) subtitle = `${erroring} need attention`
    else if (devices.some((d) => d.status === 'pending')) subtitle = 'first polls pending'
    return { value: String(devices.length), subtitle, hasDevices: true }
  }, [devices, loading, error])
}

/** "1 Gb/s", "10 Gb/s", "100 Mb/s". */
export function formatSpeed(bps: number): string {
  if (!bps) return '—'
  if (bps >= 1e9) return `${+(bps / 1e9).toFixed(1)} Gb/s`
  if (bps >= 1e6) return `${+(bps / 1e6).toFixed(1)} Mb/s`
  return `${+(bps / 1e3).toFixed(1)} kb/s`
}
```

- [ ] **Step 3: Scan hook**

`frontend/src/hooks/useSiteScan.ts`:

```ts
import { useCallback, useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export interface ScanResult {
  host: string
  port: number
  name: string
  descr: string
  object_id: string
  vendor: string
  credential_id: string
  credential_name: string
  already_added: boolean
}

export interface ScanJob {
  id: string
  site_id: string
  cidr: string
  total: number
  done: number
  running: boolean
  results: ScanResult[]
  started_at: string
  finished_at: string | null
}

/** Starts a scan and polls it every second until it finishes. */
export function useSiteScan(siteId: string) {
  const [job, setJob] = useState<ScanJob | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const start = useCallback(
    async (cidr: string, credentialIds: string[]) => {
      setBusy(true)
      setError(null)
      try {
        const { data } = await api.post<ApiResponse<ScanJob>>(`/sites/${siteId}/scans`, {
          cidr,
          credential_ids: credentialIds,
        })
        setJob(data.data)
      } catch (err) {
        setError((err as ApiError).message || 'Could not start the scan')
      } finally {
        setBusy(false)
      }
    },
    [siteId]
  )

  useEffect(() => {
    if (!job?.running) return
    const t = window.setInterval(async () => {
      try {
        const { data } = await api.get<ApiResponse<ScanJob>>(`/sites/${siteId}/scans/${job.id}`)
        setJob(data.data)
      } catch (err) {
        setError((err as ApiError).message || 'Lost track of the scan')
        setJob((j) => (j ? { ...j, running: false } : j))
      }
    }, 1000)
    return () => window.clearInterval(t)
  }, [job?.id, job?.running, siteId])

  const addDevices = useCallback(
    async (rows: ScanResult[]) => {
      const { data } = await api.post<ApiResponse<{ added: unknown[]; failed: { host: string; error: string }[] }>>(
        `/sites/${siteId}/devices`,
        { devices: rows.map((r) => ({ host: r.host, port: r.port, credential_id: r.credential_id, name: r.name })) }
      )
      return data.data
    },
    [siteId]
  )

  return { job, start, busy, error, addDevices }
}
```

- [ ] **Step 4: Navigation, routes and incident types**

In `frontend/src/components/Layout.tsx`, replace the `networkNav` array with:

```ts
const networkNav: { to: string; label: string; end?: boolean; adminOnly?: boolean }[] = [
  { to: '/network/sites', label: 'Sites' },
  { to: '/network/devices', label: 'Devices' },
  { to: '/network/credentials', label: 'Credentials', adminOnly: true },
]
```

Create placeholders so routes compile (Tasks 12–13 replace them):

`frontend/src/pages/network/Credentials.tsx`, `Devices.tsx`, `DeviceDetail.tsx` each:

```tsx
export default function Placeholder() {
  return <h1 className="text-4xl font-light text-white">Coming up</h1>
}
```

In `frontend/src/App.tsx`, after the phase 0 `SiteDetail` lazy import:

```ts
const Devices = lazy(() => import('@/pages/network/Devices'))
const DeviceDetail = lazy(() => import('@/pages/network/DeviceDetail'))
const Credentials = lazy(() => import('@/pages/network/Credentials'))
```

and after the `/network/sites/:id` route:

```tsx
              <Route path="/network/devices" element={<Devices />} />
              <Route path="/network/devices/:id" element={<DeviceDetail />} />
              <Route path="/network/credentials" element={<Credentials />} />
```

In `frontend/src/hooks/useIncidents.ts`, change the `Incident` interface's first fields to:

```ts
export interface Incident {
  id: string
  /** Set for monitor incidents; null for network device incidents. */
  monitor_id: string | null
  device_id: string | null
  subject_type: 'monitor' | 'device'
  subject_name: string
  subject_target: string
  site_id: string | null
  site_name: string
  monitor_name: string
  monitor_url: string
  monitor_type: string
```

(the rest of the interface unchanged), add to `IncidentFilters`:

```ts
  subject?: 'monitor' | 'device'
  deviceId?: string
```

and in `useIncidents`, destructure `subject, deviceId` with the others, add them to the params:

```ts
          ...(subject ? { subject } : {}),
          ...(deviceId ? { device_id: deviceId } : {}),
```

and to the `useCallback` dependency list.

- [ ] **Step 5: Typecheck**

Run the frontend check from Global Constraints.
Expected: errors only where `inc.monitor_id` is now `string | null` (`IncidentDetail.tsx` links, and any other `monitor_id` string use). Fix each by guarding: in `IncidentDetail.tsx` those lines are rewritten in Task 15, so for now wrap them as `inc.monitor_id ?? ''`. Re-run until 0 errors.

- [ ] **Step 6: Commit**

```bash
cd /home/sysadmin/sentinel
git add frontend/src/hooks/useSnmpCredentials.ts frontend/src/hooks/useDevices.ts frontend/src/hooks/useSiteScan.ts \
  frontend/src/hooks/useIncidents.ts frontend/src/components/Layout.tsx frontend/src/App.tsx frontend/src/pages/network/ \
  frontend/src/pages/IncidentDetail.tsx
git commit -m "feat(network): device, credential and scan hooks; nav and routes

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Credentials page

**Files:**
- Create: `frontend/src/components/network/CredentialFormModal.tsx`
- Modify: `frontend/src/pages/network/Credentials.tsx`

**Interfaces:**
- Consumes: `useSnmpCredentials`, `CredentialView`, `CredentialInput`, `AUTH_PROTOCOLS`, `PRIV_PROTOCOLS` (Task 11); `useSites` (phase 0); `useAuthContext`.

- [ ] **Step 1: The form**

`frontend/src/components/network/CredentialFormModal.tsx`:

```tsx
import { useState } from 'react'
import { X } from 'lucide-react'
import { useSites } from '@/hooks/useSites'
import {
  AUTH_PROTOCOLS,
  PRIV_PROTOCOLS,
  type CredentialInput,
  type CredentialView,
  type SnmpVersion,
} from '@/hooks/useSnmpCredentials'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  initial?: CredentialView
  onClose: () => void
  onSave: (input: CredentialInput) => Promise<unknown>
}

/** Secret fields are write-only: they start blank, and blank keeps what is stored. */
function SecretField({ label, isSet, value, onChange }: { label: string; isSet: boolean; value: string; onChange: (v: string) => void }) {
  return (
    <label className="block space-y-1">
      <span className="text-sm text-slate-300">
        {label} {isSet && <span className="text-xs text-emerald-400">(set — leave blank to keep)</span>}
      </span>
      <input
        type="password"
        autoComplete="new-password"
        className={inputCls}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={isSet ? '••••••••' : ''}
      />
    </label>
  )
}

export default function CredentialFormModal({ initial, onClose, onSave }: Props) {
  const { sites } = useSites()
  const [name, setName] = useState(initial?.name ?? '')
  const [siteId, setSiteId] = useState<string>(initial?.site_id ?? '')
  const [version, setVersion] = useState<SnmpVersion>(initial?.version ?? '2c')
  const [community, setCommunity] = useState('')
  const [username, setUsername] = useState(initial?.username ?? '')
  const [authProto, setAuthProto] = useState(initial?.auth_protocol ?? 'SHA')
  const [authPass, setAuthPass] = useState('')
  const [privProto, setPrivProto] = useState(initial?.priv_protocol ?? 'AES')
  const [privPass, setPrivPass] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setSaving(true)
    const input: CredentialInput = { name, site_id: siteId || null, version }
    if (version === '3') {
      Object.assign(input, {
        username,
        auth_protocol: authProto,
        priv_protocol: authProto === 'none' ? 'none' : privProto,
        ...(authPass ? { auth_password: authPass } : {}),
        ...(privPass ? { priv_password: privPass } : {}),
      })
    } else if (community) {
      input.community = community
    }
    try {
      await onSave(input)
      onClose()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the profile')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card max-h-[90vh] w-full max-w-md space-y-4 overflow-y-auto p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{initial ? 'Edit credential profile' : 'Add credential profile'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Name</span>
          <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} required maxLength={255} autoFocus />
        </label>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Available to</span>
          <select className={inputCls} value={siteId} onChange={(e) => setSiteId(e.target.value)}>
            <option value="">Every site</option>
            {sites.map((s) => (
              <option key={s.id} value={s.id}>
                Only {s.name}
              </option>
            ))}
          </select>
        </label>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">SNMP version</span>
          <select className={inputCls} value={version} onChange={(e) => setVersion(e.target.value as SnmpVersion)}>
            <option value="2c">v2c</option>
            <option value="3">v3</option>
            <option value="1">v1 (older devices)</option>
          </select>
        </label>

        {version !== '3' ? (
          <SecretField label="Community" isSet={!!initial?.has_community && initial.version !== '3'} value={community} onChange={setCommunity} />
        ) : (
          <>
            <label className="block space-y-1">
              <span className="text-sm text-slate-300">Username</span>
              <input className={inputCls} value={username} onChange={(e) => setUsername(e.target.value)} required />
            </label>
            <label className="block space-y-1">
              <span className="text-sm text-slate-300">Authentication</span>
              <select className={inputCls} value={authProto} onChange={(e) => setAuthProto(e.target.value)}>
                {AUTH_PROTOCOLS.map((p) => (
                  <option key={p} value={p}>
                    {p === 'none' ? 'None' : p}
                  </option>
                ))}
              </select>
            </label>
            {authProto !== 'none' && (
              <>
                <SecretField label="Auth password (8+ characters)" isSet={!!initial?.has_auth_password} value={authPass} onChange={setAuthPass} />
                <label className="block space-y-1">
                  <span className="text-sm text-slate-300">Privacy</span>
                  <select className={inputCls} value={privProto} onChange={(e) => setPrivProto(e.target.value)}>
                    {PRIV_PROTOCOLS.map((p) => (
                      <option key={p} value={p}>
                        {p === 'none' ? 'None' : p}
                      </option>
                    ))}
                  </select>
                </label>
                {privProto !== 'none' && (
                  <SecretField label="Privacy password (8+ characters)" isSet={!!initial?.has_priv_password} value={privPass} onChange={setPrivPass} />
                )}
              </>
            )}
          </>
        )}

        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={saving || !name.trim()}>
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </form>
    </div>
  )
}
```

- [ ] **Step 2: The page**

Replace `frontend/src/pages/network/Credentials.tsx`:

```tsx
import { useState } from 'react'
import { KeyRound, Pencil, Plus, Trash2 } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useSnmpCredentials, type CredentialView } from '@/hooks/useSnmpCredentials'
import CredentialFormModal from '@/components/network/CredentialFormModal'
import type { ApiError } from '@/services/api'

function describe(c: CredentialView): string {
  if (c.version !== '3') return `v${c.version} community`
  if (c.auth_protocol === 'none') return `v3 ${c.username}, no auth`
  return `v3 ${c.username}, ${c.auth_protocol}${c.priv_protocol !== 'none' ? ` + ${c.priv_protocol}` : ''}`
}

export default function Credentials() {
  const { currentUser } = useAuthContext()
  const { credentials, loading, error, create, update, remove } = useSnmpCredentials()
  const [editing, setEditing] = useState<CredentialView | 'new' | null>(null)
  const [confirm, setConfirm] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  if (!currentUser?.is_admin) {
    return <p className="text-sm text-slate-400">Only administrators manage credential profiles.</p>
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-4xl font-light text-white">Credentials</h1>
          <p className="mt-2 text-sm text-slate-400">
            SNMP credential profiles. Secrets are stored encrypted and never shown again after saving.
          </p>
        </div>
        <button className="btn-primary flex items-center gap-2" onClick={() => setEditing('new')}>
          <Plus className="h-4 w-4" /> Add profile
        </button>
      </div>

      {(error || actionError) && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error ?? actionError}</div>
      )}

      {loading ? (
        <p className="text-sm text-slate-400">Loading…</p>
      ) : credentials.length === 0 ? (
        <div className="card p-8 text-center">
          <KeyRound className="mx-auto h-8 w-8 text-slate-500" />
          <p className="mt-2 text-slate-300">No credential profiles yet.</p>
          <p className="mt-1 text-sm text-slate-500">Add one before adding devices; most switches use a v2c community.</p>
        </div>
      ) : (
        <div className="card divide-y divide-white/10">
          {credentials.map((c) => (
            <div key={c.id} className="flex flex-wrap items-center gap-4 p-4">
              <div className="min-w-0 flex-1">
                <div className="font-medium text-white">{c.name}</div>
                <div className="text-xs text-slate-400">
                  {describe(c)} · {c.site_id ? `only ${c.site_name}` : 'every site'} · used by {c.used_by}{' '}
                  device{c.used_by === 1 ? '' : 's'}
                </div>
              </div>
              {confirm === c.id ? (
                <div className="flex items-center gap-2">
                  <span className="text-xs text-slate-400">Delete {c.name}?</span>
                  <button className="btn-secondary !py-1" onClick={() => setConfirm(null)}>
                    Cancel
                  </button>
                  <button
                    className="btn bg-red-600 !py-1 text-white hover:bg-red-700"
                    onClick={async () => {
                      setActionError(null)
                      try {
                        await remove(c.id)
                      } catch (err) {
                        setActionError((err as ApiError).message)
                      }
                      setConfirm(null)
                    }}
                  >
                    Delete
                  </button>
                </div>
              ) : (
                <div className="flex gap-2">
                  <button className="btn-secondary !px-2 !py-1" title={`Edit ${c.name}`} onClick={() => setEditing(c)}>
                    <Pencil className="h-4 w-4" />
                  </button>
                  <button
                    className="btn-secondary !px-2 !py-1 text-red-400 disabled:opacity-40"
                    title={c.used_by > 0 ? 'In use by devices' : `Delete ${c.name}`}
                    disabled={c.used_by > 0}
                    onClick={() => setConfirm(c.id)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {editing && (
        <CredentialFormModal
          initial={editing === 'new' ? undefined : editing}
          onClose={() => setEditing(null)}
          onSave={(input) => (editing === 'new' ? create(input) : update(editing.id, input))}
        />
      )}
    </div>
  )
}
```

- [ ] **Step 3: Typecheck and commit**

Run the frontend check. Expected: 0 errors.

```bash
cd /home/sysadmin/sentinel
git add frontend/src/components/network/CredentialFormModal.tsx frontend/src/pages/network/Credentials.tsx
git commit -m "feat(network): credential profiles page with write-only secrets

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: Devices list, device page and device form

**Files:**
- Create: `frontend/src/components/network/DeviceStatusBadge.tsx`, `DeviceTable.tsx`, `DeviceFormModal.tsx`
- Modify: `frontend/src/pages/network/Devices.tsx`, `frontend/src/pages/network/DeviceDetail.tsx`

**Interfaces:**
- Consumes: `useDevices`, `useDevice`, `useDeviceInterfaces`, `useDeviceActions`, `formatSpeed`, `Device`, `DeviceInput` (Task 11); `useSiteCredentialOptions` (Task 11); `useSites`, `useSite` (phase 0); `useIncidents`, `formatDuration` (existing, extended in Task 11); `NotificationChannelPicker` (existing: props `value: string[] | null`, `onChange`).
- Produces: `<DeviceStatusBadge status detail? />`, `<DeviceTable devices showSite />`, `<DeviceFormModal initial? siteId? onClose onSaved />`.

- [ ] **Step 1: Status badge and table**

`frontend/src/components/network/DeviceStatusBadge.tsx`:

```tsx
import type { DeviceStatus } from '@/hooks/useDevices'

const STYLE: Record<DeviceStatus, string> = {
  up: 'border-emerald-500/30 bg-emerald-500/15 text-emerald-400',
  down: 'border-red-500/30 bg-red-500/15 text-red-400',
  pending: 'border-slate-500/30 bg-slate-500/15 text-slate-300',
  paused: 'border-slate-500/30 bg-slate-500/10 text-slate-400',
  error: 'border-amber-500/30 bg-amber-500/15 text-amber-400',
}
const LABEL: Record<DeviceStatus, string> = { up: 'Up', down: 'Down', pending: 'Pending', paused: 'Paused', error: 'Error' }

export default function DeviceStatusBadge({ status, detail }: { status: DeviceStatus; detail?: string }) {
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium ${STYLE[status]}`}
      title={detail || undefined}
    >
      {LABEL[status]}
    </span>
  )
}
```

`frontend/src/components/network/DeviceTable.tsx`:

```tsx
import { useNavigate } from 'react-router-dom'
import type { Device } from '@/hooks/useDevices'
import DeviceStatusBadge from './DeviceStatusBadge'

function ago(iso: string | null): string {
  if (!iso) return 'never'
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 90) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return new Date(iso).toLocaleDateString()
}

export default function DeviceTable({ devices, showSite }: { devices: Device[]; showSite: boolean }) {
  const navigate = useNavigate()
  return (
    <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-white/10 text-left text-xs text-slate-400">
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Name</th>
              {showSite && <th className="px-4 py-3 font-medium">Site</th>}
              <th className="px-4 py-3 font-medium">Address</th>
              <th className="px-4 py-3 font-medium">Vendor / model</th>
              <th className="px-4 py-3 font-medium">Last seen</th>
              <th className="px-4 py-3 text-right font-medium">30-day availability</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {devices.map((d) => (
              <tr key={d.id} className="cursor-pointer transition hover:bg-white/5" onClick={() => navigate(`/network/devices/${d.id}`)}>
                <td className="px-4 py-3">
                  <DeviceStatusBadge status={d.status} detail={d.status_detail} />
                </td>
                <td className="px-4 py-3 font-medium text-slate-200">{d.name}</td>
                {showSite && <td className="px-4 py-3 text-slate-400">{d.site_name}</td>}
                <td className="px-4 py-3 font-mono text-xs text-slate-400">
                  {d.host}
                  {d.port !== 161 && `:${d.port}`}
                </td>
                <td className="px-4 py-3 text-slate-400">{[d.vendor, d.model].filter(Boolean).join(' · ') || '—'}</td>
                <td className="px-4 py-3 text-slate-400">{ago(d.last_seen_at)}</td>
                <td className="px-4 py-3 text-right tabular-nums text-slate-300">
                  {d.availability_30d == null ? '—' : `${d.availability_30d.toFixed(2)}%`}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Device form with Test connection**

`frontend/src/components/network/DeviceFormModal.tsx`:

```tsx
import { useState } from 'react'
import { CheckCircle2, X, XCircle } from 'lucide-react'
import { useSites } from '@/hooks/useSites'
import { useSiteCredentialOptions } from '@/hooks/useSnmpCredentials'
import { useDeviceActions, type Device, type DeviceInput, type TestResult } from '@/hooks/useDevices'
import NotificationChannelPicker from '@/components/NotificationChannelPicker'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  initial?: Device
  /** Preselected site when adding from a site page. */
  siteId?: string
  onClose: () => void
  onSaved: (d: Device) => void
}

export default function DeviceFormModal({ initial, siteId, onClose, onSaved }: Props) {
  const { sites } = useSites()
  const { create, update, test, busy } = useDeviceActions()
  const [form, setForm] = useState<DeviceInput>({
    site_id: initial?.site_id ?? siteId ?? '',
    credential_id: initial?.credential_id ?? '',
    name: initial?.name ?? '',
    host: initial?.host ?? '',
    port: initial?.port ?? 161,
    enabled: initial?.enabled ?? true,
    poll_interval: initial?.poll_interval ?? 60,
    timeout_ms: initial?.timeout_ms ?? 3000,
    retries: initial?.retries ?? 1,
    notify_channels: initial?.notify_channels ?? null,
  })
  const { options } = useSiteCredentialOptions(form.site_id || undefined, !!form.site_id)
  const [result, setResult] = useState<TestResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const set = <K extends keyof DeviceInput>(k: K, v: DeviceInput[K]) => setForm((f) => ({ ...f, [k]: v }))

  const runTest = async () => {
    setResult(null)
    setError(null)
    try {
      const r = await test({ site_id: form.site_id, credential_id: form.credential_id, host: form.host, port: form.port })
      setResult(r)
      if (r.ok && r.system?.name && !form.name) set('name', r.system.name)
    } catch (err) {
      setError((err as ApiError).message || 'Test failed')
    }
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    try {
      onSaved(initial ? await update(initial.id, form) : await create(form))
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the device')
    }
  }

  const ready = form.site_id && form.credential_id && form.host.trim()

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card max-h-[90vh] w-full max-w-lg space-y-4 overflow-y-auto p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{initial ? 'Edit device' : 'Add device'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Site</span>
          <select className={inputCls} value={form.site_id} onChange={(e) => { set('site_id', e.target.value); set('credential_id', '') }} required>
            <option value="">Choose a site…</option>
            {sites.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </label>

        <div className="grid grid-cols-[1fr_7rem] gap-3">
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Address</span>
            <input className={inputCls} value={form.host} onChange={(e) => set('host', e.target.value)} placeholder="10.20.0.2 or core-sw-1.example" required />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-slate-300">Port</span>
            <input type="number" min={1} max={65535} className={inputCls} value={form.port} onChange={(e) => set('port', Number(e.target.value))} />
          </label>
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Credential profile</span>
          <select className={inputCls} value={form.credential_id} onChange={(e) => set('credential_id', e.target.value)} required disabled={!form.site_id}>
            <option value="">{form.site_id ? 'Choose a profile…' : 'Choose a site first'}</option>
            {options.map((o) => (
              <option key={o.id} value={o.id}>
                {o.name} (v{o.version})
              </option>
            ))}
          </select>
        </label>

        <div className="flex items-center gap-3">
          <button type="button" className="btn-secondary" disabled={!ready || busy} onClick={() => void runTest()}>
            {busy ? 'Testing…' : 'Test connection'}
          </button>
          {result &&
            (result.ok ? (
              <span className="flex items-center gap-1.5 text-sm text-emerald-400">
                <CheckCircle2 className="h-4 w-4" /> {result.system?.name || 'Answered'}
                {result.vendor && <span className="text-slate-400">· {result.vendor}</span>}
              </span>
            ) : (
              <span className="flex items-center gap-1.5 text-sm text-red-400">
                <XCircle className="h-4 w-4" /> {result.error}
              </span>
            ))}
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">
            Name <span className="text-slate-500">(optional — taken from the device if blank)</span>
          </span>
          <input className={inputCls} value={form.name} onChange={(e) => set('name', e.target.value)} maxLength={255} />
        </label>

        <details className="rounded-md border border-white/10 p-3">
          <summary className="cursor-pointer text-sm text-slate-300">Polling</summary>
          <div className="mt-3 grid grid-cols-3 gap-3">
            <label className="block space-y-1">
              <span className="text-xs text-slate-400">Every (s)</span>
              <input type="number" min={10} max={3600} className={inputCls} value={form.poll_interval} onChange={(e) => set('poll_interval', Number(e.target.value))} />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-slate-400">Timeout (ms)</span>
              <input type="number" min={200} max={30000} className={inputCls} value={form.timeout_ms} onChange={(e) => set('timeout_ms', Number(e.target.value))} />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-slate-400">Retries</span>
              <input type="number" min={1} max={5} className={inputCls} value={form.retries} onChange={(e) => set('retries', Number(e.target.value))} />
            </label>
          </div>
        </details>

        <div className="space-y-1">
          <span className="text-sm text-slate-300">Alert channels</span>
          <NotificationChannelPicker value={form.notify_channels} onChange={(v) => set('notify_channels', v)} />
        </div>

        {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || !ready}>
            {initial ? 'Save' : 'Add device'}
          </button>
        </div>
      </form>
    </div>
  )
}
```

The site select is limited server-side: saving to a site where the user lacks edit access returns 403, shown as the error. The list shows every site the user can see; that is acceptable in phase 1 (readonly users do not see the Add button at all).

- [ ] **Step 3: Devices page**

Replace `frontend/src/pages/network/Devices.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { Plus, Router, Search } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useSites } from '@/hooks/useSites'
import { useDevices, type DeviceStatus } from '@/hooks/useDevices'
import DeviceTable from '@/components/network/DeviceTable'
import DeviceFormModal from '@/components/network/DeviceFormModal'

export default function Devices() {
  const { currentUser } = useAuthContext()
  const { sites } = useSites()
  const [siteId, setSiteId] = useState('')
  const [status, setStatus] = useState<DeviceStatus | ''>('')
  const [search, setSearch] = useState('')
  const [adding, setAdding] = useState(false)
  const { devices, loading, error, refetch } = useDevices({ siteId: siteId || undefined, status: status || undefined })

  const shown = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return devices
    return devices.filter((d) => [d.name, d.host, d.vendor, d.model, d.site_name].some((v) => v?.toLowerCase().includes(q)))
  }, [devices, search])

  // Admins can add anywhere; others need an editable site, which the form
  // enforces through the API. Offer the button to anyone who can see a site.
  const canAdd = !!currentUser?.is_admin || sites.length > 0

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-4xl font-light text-white">Devices</h1>
          <p className="mt-2 text-sm text-slate-400">Every SNMP device in the sites you can see.</p>
        </div>
        {canAdd && (
          <button className="btn-primary flex items-center gap-2" onClick={() => setAdding(true)}>
            <Plus className="h-4 w-4" /> Add device
          </button>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2.5">
        <div className="relative min-w-[220px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search name, address, vendor…"
            className="w-full rounded-lg border border-white/10 bg-slate-900/60 py-2 pl-9 pr-3 text-sm text-white placeholder-slate-500"
          />
        </div>
        <select className="rd-select" value={siteId} onChange={(e) => setSiteId(e.target.value)} aria-label="Filter by site">
          <option value="">All sites</option>
          {sites.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
        <select className="rd-select" value={status} onChange={(e) => setStatus(e.target.value as DeviceStatus | '')} aria-label="Filter by status">
          <option value="">All statuses</option>
          <option value="down">Down</option>
          <option value="up">Up</option>
          <option value="error">Error</option>
          <option value="pending">Pending</option>
          <option value="paused">Paused</option>
        </select>
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      {loading ? (
        <p className="text-sm text-slate-400">Loading devices…</p>
      ) : shown.length === 0 ? (
        <div className="card p-8 text-center">
          <Router className="mx-auto h-8 w-8 text-slate-500" />
          <p className="mt-2 text-slate-300">{devices.length === 0 ? 'No devices yet.' : 'No devices match these filters.'}</p>
          {devices.length === 0 && <p className="mt-1 text-sm text-slate-500">Add one, or scan a subnet from a site's page.</p>}
        </div>
      ) : (
        <DeviceTable devices={shown} showSite />
      )}

      {adding && (
        <DeviceFormModal
          siteId={siteId || undefined}
          onClose={() => setAdding(false)}
          onSaved={() => {
            setAdding(false)
            void refetch()
          }}
        />
      )}
    </div>
  )
}
```

- [ ] **Step 4: Device page**

Replace `frontend/src/pages/network/DeviceDetail.tsx`:

```tsx
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Pause, Pencil, Play, RefreshCw, Trash2 } from 'lucide-react'
import { useSite } from '@/hooks/useSites'
import { formatSpeed, useDevice, useDeviceActions, useDeviceInterfaces, type Device } from '@/hooks/useDevices'
import { useIncidents, formatDuration, DEFAULT_FILTERS } from '@/hooks/useIncidents'
import DeviceStatusBadge from '@/components/network/DeviceStatusBadge'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import type { ApiError } from '@/services/api'

const PORT_TONE: Record<string, string> = {
  up: 'text-emerald-400',
  down: 'text-red-400',
  lowerLayerDown: 'text-red-400',
  dormant: 'text-slate-400',
  notPresent: 'text-slate-500',
}

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

export default function DeviceDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { device, loading, notFound, refetch } = useDevice(id)
  const { site } = useSite(device?.site_id)
  const [showAbsent, setShowAbsent] = useState(false)
  const [hideAdminDown, setHideAdminDown] = useState(false)
  const { interfaces } = useDeviceInterfaces(id, showAbsent)
  const { incidents } = useIncidents({ ...DEFAULT_FILTERS, limit: 10, deviceId: id })
  const { update, remove, refresh, busy } = useDeviceActions()
  const [editing, setEditing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<string | null>(null)

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
  const ports = interfaces.filter((i) => !hideAdminDown || i.admin_status !== 'down')

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
            <button className="btn-secondary flex items-center gap-2" onClick={() => setEditing(true)}>
              <Pencil className="h-4 w-4" /> Edit
            </button>
            <button className="btn-secondary flex items-center gap-2" disabled={busy}
              onClick={() => void act(() => update(device.id, toInput(device, !device.enabled)))}>
              {device.enabled ? <><Pause className="h-4 w-4" /> Pause</> : <><Play className="h-4 w-4" /> Resume</>}
            </button>
            {confirmDelete ? (
              <>
                <span className="self-center text-xs text-slate-400">Its incident history is deleted too.</span>
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
          ['Vendor', device.vendor || '—'],
          ['Model', device.model || '—'],
          ['Serial', device.serial || '—'],
          ['Location', device.sys_location || '—'],
          ['Contact', device.sys_contact || '—'],
          ['Up since', upSince(device)],
          ['Last seen', device.last_seen_at ? new Date(device.last_seen_at).toLocaleString() : 'never'],
          ['Credential profile', device.credential_name || '—'],
          ['30-day availability', device.availability_30d == null ? '—' : `${device.availability_30d.toFixed(2)}%`],
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

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-lg font-light text-white">Interfaces ({ports.length})</h2>
          <div className="flex gap-4 text-sm text-slate-400">
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={hideAdminDown} onChange={(e) => setHideAdminDown(e.target.checked)} /> Hide disabled ports
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={showAbsent} onChange={(e) => setShowAbsent(e.target.checked)} /> Show removed ports
            </label>
          </div>
        </div>
        {ports.length === 0 ? (
          <p className="text-sm text-slate-500">{device.last_inventory_at ? 'No interfaces reported.' : 'The interface list arrives with the first inventory.'}</p>
        ) : (
          <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                    <th className="px-4 py-2 font-medium">Port</th>
                    <th className="px-4 py-2 font-medium">Description</th>
                    <th className="px-4 py-2 font-medium">Admin</th>
                    <th className="px-4 py-2 font-medium">Link</th>
                    <th className="px-4 py-2 font-medium">Speed</th>
                    <th className="px-4 py-2 font-medium">MAC</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {ports.map((p) => (
                    <tr key={p.id} className={p.present ? '' : 'opacity-50'}>
                      <td className="px-4 py-2 font-medium text-slate-200" title={p.descr}>{p.name}</td>
                      <td className="px-4 py-2 text-slate-400">{p.alias || '—'}</td>
                      <td className={`px-4 py-2 ${p.admin_status === 'down' ? 'text-slate-500' : 'text-slate-300'}`}>{p.admin_status || '—'}</td>
                      <td className={`px-4 py-2 ${PORT_TONE[p.oper_status] ?? 'text-slate-400'}`}>{p.present ? p.oper_status || '—' : 'removed'}</td>
                      <td className="px-4 py-2 tabular-nums text-slate-400">{formatSpeed(p.speed_bps)}</td>
                      <td className="px-4 py-2 font-mono text-xs text-slate-500">{p.mac || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
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
                    {inc.status === 'ongoing' ? 'Ongoing' : 'Resolved'} · {new Date(inc.start_time).toLocaleString()}
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
    </div>
  )
}
```

`useSite` returns the caller's `access` for the site (phase 0), which decides whether the edit controls show; the API enforces the same rule.

- [ ] **Step 5: Typecheck and commit**

Run the frontend check. Expected: 0 errors (if `Router` is not exported by the installed `lucide-react`, use `Network` instead).

```bash
cd /home/sysadmin/sentinel
git add frontend/src/components/network/DeviceStatusBadge.tsx frontend/src/components/network/DeviceTable.tsx \
  frontend/src/components/network/DeviceFormModal.tsx frontend/src/pages/network/Devices.tsx frontend/src/pages/network/DeviceDetail.tsx
git commit -m "feat(network): devices list, device page with interfaces, and device form

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: Site page devices and subnet scan

**Files:**
- Create: `frontend/src/components/network/ScanModal.tsx`
- Modify: `frontend/src/pages/network/SiteDetail.tsx`

**Interfaces:**
- Consumes: `useSiteScan`, `ScanResult` (Task 11); `useSiteCredentialOptions` (Task 11); `useDevices`, `DeviceTable`, `DeviceFormModal` (Tasks 11, 13).

- [ ] **Step 1: Scan modal**

`frontend/src/components/network/ScanModal.tsx`:

```tsx
import { useEffect, useMemo, useState } from 'react'
import { Loader2, X } from 'lucide-react'
import { useSiteScan, type ScanResult } from '@/hooks/useSiteScan'
import { useSiteCredentialOptions } from '@/hooks/useSnmpCredentials'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

export default function ScanModal({ siteId, onClose, onAdded }: { siteId: string; onClose: () => void; onAdded: () => void }) {
  const { options } = useSiteCredentialOptions(siteId, true)
  const { job, start, busy, error, addDevices } = useSiteScan(siteId)
  const [cidr, setCidr] = useState('')
  const [chosen, setChosen] = useState<string[]>([])
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [message, setMessage] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)

  // Every profile available to the site is tried by default.
  useEffect(() => setChosen(options.map((o) => o.id)), [options])

  const results = job?.results ?? []
  const addable = useMemo(() => results.filter((r) => !r.already_added), [results])
  const key = (r: ScanResult) => `${r.host}:${r.port}`

  const add = async () => {
    setAdding(true)
    setMessage(null)
    try {
      const rows = addable.filter((r) => picked.has(key(r)))
      const res = await addDevices(rows)
      setMessage(`Added ${res.added.length}${res.failed.length ? `; ${res.failed.length} failed: ${res.failed.map((f) => `${f.host} (${f.error})`).join(', ')}` : ''}.`)
      if (res.failed.length === 0) onAdded()
    } catch (err) {
      setMessage((err as ApiError).message || 'Could not add the devices')
    } finally {
      setAdding(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card flex max-h-[90vh] w-full max-w-3xl flex-col gap-4 p-6" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between">
          <div>
            <h2 className="text-lg font-semibold">Scan subnet</h2>
            <p className="text-sm text-slate-400">Tries each chosen profile on every address, stopping at the first that answers.</p>
          </div>
          <button className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        {!job && (
          <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); void start(cidr.trim(), chosen) }}>
            <label className="block space-y-1">
              <span className="text-sm text-slate-300">Subnet</span>
              <input className={inputCls} value={cidr} onChange={(e) => setCidr(e.target.value)} placeholder="10.20.0.0/24 (a /22 at most)" required autoFocus />
            </label>
            <fieldset className="space-y-1">
              <legend className="text-sm text-slate-300">Credential profiles to try</legend>
              {options.length === 0 && <p className="text-sm text-slate-500">No profiles are available to this site; an admin can add one under Credentials.</p>}
              {options.map((o) => (
                <label key={o.id} className="flex items-center gap-2 text-sm text-slate-300">
                  <input type="checkbox" checked={chosen.includes(o.id)}
                    onChange={(e) => setChosen((c) => (e.target.checked ? [...c, o.id] : c.filter((x) => x !== o.id)))} />
                  {o.name} <span className="text-slate-500">(v{o.version})</span>
                </label>
              ))}
            </fieldset>
            {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}
            <div className="flex justify-end">
              <button className="btn-primary" disabled={busy || chosen.length === 0 || !cidr.trim()}>
                {busy ? 'Starting…' : 'Scan'}
              </button>
            </div>
          </form>
        )}

        {job && (
          <>
            <div className="space-y-1">
              <div className="flex items-center justify-between text-sm text-slate-300">
                <span className="flex items-center gap-2">
                  {job.running && <Loader2 className="h-4 w-4 animate-spin" />}
                  {job.cidr}: {job.done} of {job.total} addresses checked, {results.length} answered
                </span>
              </div>
              <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/10">
                <div className="h-full rounded-full bg-emerald-500 transition-all" style={{ width: `${job.total ? (100 * job.done) / job.total : 0}%` }} />
              </div>
            </div>
            {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

            <div className="min-h-0 flex-1 overflow-y-auto rounded-lg border border-white/10">
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-slate-900">
                  <tr className="text-left text-xs text-slate-400">
                    <th className="w-8 px-3 py-2">
                      <input type="checkbox" aria-label="Select all"
                        checked={addable.length > 0 && addable.every((r) => picked.has(key(r)))}
                        onChange={(e) => setPicked(e.target.checked ? new Set(addable.map(key)) : new Set())} />
                    </th>
                    <th className="px-3 py-2 font-medium">Address</th>
                    <th className="px-3 py-2 font-medium">Name</th>
                    <th className="px-3 py-2 font-medium">Vendor</th>
                    <th className="px-3 py-2 font-medium">Profile</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {[...results].sort((a, b) => a.host.localeCompare(b.host, undefined, { numeric: true })).map((r) => (
                    <tr key={key(r)} className={r.already_added ? 'opacity-50' : ''}>
                      <td className="px-3 py-2">
                        <input type="checkbox" disabled={r.already_added} checked={picked.has(key(r))}
                          onChange={(e) => setPicked((p) => { const n = new Set(p); if (e.target.checked) n.add(key(r)); else n.delete(key(r)); return n })} />
                      </td>
                      <td className="px-3 py-2 font-mono text-xs text-slate-300">{r.host}</td>
                      <td className="px-3 py-2 text-slate-200" title={r.descr}>{r.name || '—'}{r.already_added && <span className="ml-2 text-xs text-slate-500">already added</span>}</td>
                      <td className="px-3 py-2 text-slate-400">{r.vendor || '—'}</td>
                      <td className="px-3 py-2 text-slate-400">{r.credential_name}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {message && <p className="text-sm text-slate-300">{message}</p>}
            <div className="flex justify-end gap-2">
              <button className="btn-secondary" onClick={onClose}>Close</button>
              <button className="btn-primary" disabled={adding || picked.size === 0} onClick={() => void add()}>
                {adding ? 'Adding…' : `Add selected (${picked.size})`}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Site page**

In `frontend/src/pages/network/SiteDetail.tsx` (phase 0), add imports:

```tsx
import { Plus, Radar } from 'lucide-react'
import { useDevices } from '@/hooks/useDevices'
import DeviceTable from '@/components/network/DeviceTable'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import ScanModal from '@/components/network/ScanModal'
```

(merge with the existing `lucide-react` import line rather than adding a second one), add state beside the others:

```tsx
  const { devices, refetch: refetchDevices } = useDevices({ siteId: id })
  const [addingDevice, setAddingDevice] = useState(false)
  const [scanning, setScanning] = useState(false)
```

compute after `const isAdmin = site.access === 'admin'`:

```tsx
  const canEdit = isAdmin || site.access === 'editable'
```

replace the phase 0 placeholder block:

```tsx
      <div className="card p-8 text-center">
        <p className="text-slate-300">No devices yet.</p>
        <p className="mt-1 text-sm text-slate-500">SNMP devices arrive in a later update.</p>
      </div>
```

with:

```tsx
      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-lg font-light text-white">Devices ({devices.length})</h2>
          {canEdit && (
            <div className="flex gap-2">
              <button className="btn-secondary flex items-center gap-2" onClick={() => setScanning(true)}>
                <Radar className="h-4 w-4" /> Scan subnet
              </button>
              <button className="btn-primary flex items-center gap-2" onClick={() => setAddingDevice(true)}>
                <Plus className="h-4 w-4" /> Add device
              </button>
            </div>
          )}
        </div>
        {devices.length === 0 ? (
          <div className="card p-8 text-center">
            <p className="text-slate-300">No devices yet.</p>
            {canEdit && <p className="mt-1 text-sm text-slate-500">Add a device by address, or scan a subnet to find them.</p>}
          </div>
        ) : (
          <DeviceTable devices={devices} showSite={false} />
        )}
      </section>
```

and before the closing `</div>` of the page, beside the other modals:

```tsx
      {addingDevice && (
        <DeviceFormModal siteId={site.id} onClose={() => setAddingDevice(false)} onSaved={() => { setAddingDevice(false); void refetchDevices() }} />
      )}
      {scanning && <ScanModal siteId={site.id} onClose={() => setScanning(false)} onAdded={() => void refetchDevices()} />}
```

The phase 0 `useSite` hook is called with `id`; `useDevices` needs to be called unconditionally before any early `return` (React hook rules): place it next to `useSite(id)` at the top of the component, not after the `loading`/`notFound` returns.

- [ ] **Step 3: Typecheck and commit**

Run the frontend check. Expected: 0 errors (use `Search` in place of `Radar` if the installed `lucide-react` lacks it).

```bash
cd /home/sysadmin/sentinel
git add frontend/src/components/network/ScanModal.tsx frontend/src/pages/network/SiteDetail.tsx
git commit -m "feat(network): site devices and subnet scan

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 15: Incidents pages and Overview

**Files:**
- Modify: `frontend/src/pages/Incidents.tsx`, `frontend/src/pages/IncidentDetail.tsx`, `frontend/src/pages/Overview.tsx`

**Interfaces:**
- Consumes: `Incident` subject fields and `IncidentFilters.subject` (Task 11); `useDeviceSummary` (Task 11); `useSiteSummary` (phase 0).

- [ ] **Step 1: Incidents list**

In `frontend/src/pages/Incidents.tsx`:

1. Add `Router` to the `lucide-react` import (or `Network`, as in Task 13).
2. After the monitor `<select>`, add a subject filter:

```tsx
        <select
          className="rd-select"
          value={filters.subject ?? ''}
          onChange={(e) => set('subject', (e.target.value || undefined) as IncidentFilters['subject'])}
          aria-label="Filter by kind"
        >
          <option value="">Monitors and devices</option>
          <option value="monitor">Monitors</option>
          <option value="device">Network devices</option>
        </select>
```

3. Change the search placeholder and aria-label from "Search by monitor name" to "Search by name".
4. Change the table header `Monitor` to `Subject`.
5. Replace the first cell:

```tsx
                      <td className="px-4 py-3">
                        <div className="font-medium text-slate-200">{inc.monitor_name}</div>
                        <div className="truncate text-xs text-slate-500">{inc.monitor_url}</div>
                      </td>
```

with:

```tsx
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-1.5 font-medium text-slate-200">
                          {inc.subject_type === 'device' && <Router className="h-3.5 w-3.5 shrink-0 text-indigo-400" aria-label="Network device" />}
                          {inc.subject_name || inc.monitor_name}
                        </div>
                        <div className="truncate text-xs text-slate-500">
                          {inc.subject_type === 'device' ? `${inc.site_name} · ${inc.subject_target}` : inc.monitor_url}
                        </div>
                      </td>
```

6. The empty-state text "An incident is opened when a monitor goes down…" becomes "An incident is opened when a monitor or network device goes down and closed when it recovers." Include `filters.subject` in the two "any filter set" conditions (`filters.search || filters.status !== 'all' || filters.monitorId || filters.subject`).
7. Subtitle "View all service incidents" becomes "Outages of monitors and network devices you can see".

- [ ] **Step 2: Incident detail**

In `frontend/src/pages/IncidentDetail.tsx`, replace the `backTo`/`backLabel` block:

```tsx
  const from = (location.state as { from?: string } | null)?.from
  const backTo = from ?? (inc ? `/monitors/${inc.monitor_id}` : '/incidents')
  const backLabel = from === '/incidents' ? 'Back to Incidents' : 'Back to Monitor'
```

with:

```tsx
  const from = (location.state as { from?: string } | null)?.from
  const isDevice = inc?.subject_type === 'device'
  // The subject's own page: the monitor, or the network device.
  const subjectLink = inc ? (isDevice ? `/network/devices/${inc.device_id}` : `/monitors/${inc.monitor_id}`) : '/incidents'
  const backTo = from ?? subjectLink
  const backLabel = from === '/incidents' ? 'Back to Incidents' : isDevice ? 'Back to Device' : 'Back to Monitor'
```

Replace both `` `/monitors/${inc.monitor_id ?? ''}` `` links (the breadcrumb and the "View monitor" link; Task 11 made them `?? ''`) with `{subjectLink}`, and the link text `View monitor` with `{isDevice ? 'View device' : 'View monitor'}`. Under the `<h1>`, replace `{inc.monitor_url}` in the subtitle `<p>` with `{isDevice ? `${inc.site_name} · ${inc.subject_target}` : inc.monitor_url}` (and its `title`).

Hide what does not apply to devices: wrap the "Checks during the incident" `<div>` in the `<dl>` and the whole "Check timeline" `<section>` in `{!isDevice && ( … )}`; for devices, add to the `<dl>` instead:

```tsx
            {isDevice && (
              <div>
                <dt className="text-xs uppercase tracking-widest text-slate-500">Detected by</dt>
                <dd className="mt-0.5 text-sm text-slate-200">3 consecutive SNMP polls without an answer</dd>
              </div>
            )}
```

The save error "You do not have permission to edit this monitor" becomes "You do not have permission to edit this incident".

- [ ] **Step 3: Overview card**

In `frontend/src/pages/Overview.tsx`, import `useDeviceSummary` from `@/hooks/useDevices`, call `const deviceSummary = useDeviceSummary()` beside `siteSummary`, and change the `network` card's value and subtitle so devices lead once there are any:

```ts
        value: deviceSummary.hasDevices ? deviceSummary.value : siteSummary.value,
        subtitle: deviceSummary.hasDevices
          ? `${deviceSummary.subtitle} · ${siteSummary.value} site${siteSummary.value === '1' ? '' : 's'}`
          : siteSummary.subtitle,
```

Add `deviceSummary` to the `useMemo` dependencies.

- [ ] **Step 4: Typecheck and commit**

Run the frontend check. Expected: 0 errors.

```bash
cd /home/sysadmin/sentinel
git add frontend/src/pages/Incidents.tsx frontend/src/pages/IncidentDetail.tsx frontend/src/pages/Overview.tsx
git commit -m "feat(incidents): show network device incidents; Overview counts devices

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 16: Sandbox verification

No code unless a step fails; a failure is fixed in the task that owns the code, committed, and the checklist restarted from step 1.

**Files:** `deploy/snmpsim/data/realswitch.snmprec` (recorded, scrubbed) only.

- [ ] **Step 1: Full test suite with both harnesses**

```bash
cd /home/sysadmin/sentinel/deploy/snmpsim && SNMPSIM_VERSION=<pinned> ./run.sh
cd /home/sysadmin/sentinel/backend && SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 ./scripts/test-db.sh 2>&1 | tail -12
```

Expected: every package `ok`, with the `TestDB…` and `TestSim…` tests run (not skipped): confirm with `SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 ./scripts/test-db.sh -v 2>&1 | grep -cE -- '--- SKIP'` → `0`.

- [ ] **Step 2: Deploy to the sandbox**

Deploy the branch without pushing (the sandbox is a separate clone):

```bash
cd /srv/docker/sentinel-dev
git fetch -q /home/sysadmin/sentinel feature/network-phase1:feature/network-phase1 && git switch -q feature/network-phase1
docker compose up -d --build && docker compose ps
docker compose logs backend --since 3m | grep -E 'applying migration|poller started|panic|fatal'
```

Expected: 046 and 047 applied, `[snmp] poller started with 16 workers`, no panic. `docker network connect sentinel-dev_default sentinel-snmpsim` if `run.sh` ran before the sandbox network existed.

- [ ] **Step 3: Record the real switch (with the user)**

Ask the user for the real switch's IP and read-only v2c community. From the sandbox host:

```bash
docker run --rm --network host -v /home/sysadmin/sentinel/deploy/snmpsim/data:/out sentinel-snmpsim:dev \
  sh -c "snmpsim-record-commands --agent-udpv4-endpoint=<IP>:161 --protocol-version=2c --community=<community> --output-file=/out/realswitch.snmprec"
```

(The recorder's command name varies by snmpsim version; `ls $(python -c 'import sys;print(sys.prefix)')/bin | grep -i rec` inside the image finds it.)

Scrub before committing: replace every serial number (`1.3.6.1.2.1.47.1.1.1.1.11.*`) value with `SCRUBBED`, every MAC (`1.3.6.1.2.1.2.2.1.6.*`) with a synthetic `02005e0000NN`, and `sysContact`/`sysLocation` with neutral text. Confirm with `grep -iE '<community>|<real serial>' data/realswitch.snmprec` → nothing. Rebuild the simulator (`./run.sh`) so `realswitch` becomes a community.

- [ ] **Step 4: Checklist with the user** (sandbox UI at http://localhost:3200)

1. Credentials: add a v2c profile for the real switch and one named `sim-edgeswitch` with community `edgeswitch`. The list says "set" for secrets; editing shows them blank.
2. Add the real switch with **Test connection**: name, vendor and model fill in; after saving, within a minute it is Up and its interface list matches the switch's ports, including port descriptions set on the switch.
3. Add a device `sentinel-snmpsim` port 1161 with the `sim-edgeswitch` profile: 52 ports, model `ES-48-500W`.
4. Scan the real switch's subnet: it is found and marked "already added".
5. Down and recovery: `docker stop sentinel-snmpsim`; within about 3 minutes the simulated device is Down, an incident is open on the Incidents page (with the switch icon and site name), and a notification arrives on the chosen channel. `docker start sentinel-snmpsim`: within a minute it is Up, the incident is resolved, a recovery notification arrives ("…answering again after N minutes").
6. Pause while down: stop the simulator again until Down, then Pause the device: the incident closes with "Monitoring was paused."; Resume and start the simulator: it returns to Up.
7. Access (second, non-admin user with readonly on the site): sees the site's devices and their incidents; no Add/Edit/Scan buttons; `/network/credentials` shows the admin-only message; a device in another site is "Device not found"; `curl` as that user: `POST /api/v1/devices` → 403, `GET /api/v1/snmp-credentials` → 403.
8. Load: add 30 devices `sentinel-snmpsim:1161`–`1190` (bulk: scan cannot target ports, so use `POST /api/v1/sites/:id/devices` with 30 entries via curl as admin). For five minutes, `docker compose logs backend | grep '\[snmp\]'` shows no "saving poll" errors, and in the database `SELECT max(now() - last_polled_at) FROM devices WHERE enabled` stays under 70 s. Record the number.

- [ ] **Step 5: Commit the recording and report**

```bash
cd /home/sysadmin/sentinel
git add deploy/snmpsim/data/realswitch.snmprec
git commit -m "test(snmp): scrubbed recording of a real switch for the simulator

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git switch main
```

Report every checklist result (including the load number from step 8) to the user. Merging to `dev`, pushing, and anything on the live stack are the user's decisions.

