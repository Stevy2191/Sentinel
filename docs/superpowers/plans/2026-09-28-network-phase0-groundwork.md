# Network Monitoring Phase 0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move Sentinel onto TimescaleDB, make backups configuration-only, add Sites with per-site sharing, and add the Network Monitoring section of the UI with its own sidebar.

**Architecture:** The Postgres image becomes `timescale/timescaledb:2.30.1-pg16` with the extension preloaded on the command line; a preflight in `internal/database` refuses to start against plain Postgres with a readable message. Backups dump only the `public` schema. Sites live in `public` (`sites`, `site_sharing`); all permission rules go through one pure function, `resolveSiteAccess`, wrapped by `SiteService.SiteAccess`. Site HTTP handlers depend on a `siteStore` interface so they are tested without a database. The frontend `Layout` swaps its nav list for any route under `/network`.

**Tech Stack:** Go 1.26, Gin, GORM, PostgreSQL 16 + TimescaleDB 2.30.1; React + TypeScript + Vite + Tailwind; Docker Compose.

**Spec:** `docs/superpowers/specs/2026-09-28-network-phase0-groundwork-design.md`

## Global Constraints

- Branch: `feature/network-monitoring` (from `dev`). Never commit to `main` or `dev` directly.
- Database image: exactly `timescale/timescaledb:2.30.1-pg16`; never a floating tag.
- Postgres command flags: `shared_preload_libraries=timescaledb` and `timescaledb.telemetry_level=off`.
- Migrations: `044_timescaledb.sql`, `045_sites.sql` (the highest existing on this branch is `043_report_types_and_sla.sql`). Never edit an applied migration.
- Every new timestamp column is `TIMESTAMPTZ`.
- API routes live under `/api/v1` (not `/api`).
- A site the caller cannot see answers **404**, never 403.
- Admin-only routes use `RequireAdmin`, which aborts; a refused request must never run the handler.
- Share permissions are exactly `readonly` or `editable`.
- No foreign keys from any `metrics` table into `public` (a rule for phase 2; nothing in this plan creates one).
- Frontend follows the existing design system: slate/Tailwind, the `card`, `btn-primary`, `btn-secondary` classes, `rd-nav` for nav links. Tailwind classes must be literal strings, never assembled at runtime.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- The live stack (`/home/sysadmin/sentinel`, ports 3100/3001) is never touched by this plan. Verification happens on the sandbox (`/srv/docker/sentinel-dev`, ports 3200/3201, `IMAGE_TAG=dev`).

## Review Focus

1. **Site name with only whitespace or differing only in case** ("HQ" vs " hq "): must be rejected as empty or as a duplicate (409), never stored as a second site. Test: `TestNormalizeSiteInput` (Task 3) and the duplicate case in `TestSiteHandlers` (Task 5).
2. **Non-admin calling an admin site route with a well-formed body**: must get 403 with **no** store mutation or audit entry. Test: `TestSiteAdminRoutesDoNotRunForNonAdmins` (Task 5).
3. **Sharing with a user ID that does not exist**: must be a 400 "no such user", not a 500. Test: the `unknown user` case in `TestSiteHandlers` (Task 5).
4. **A share row carrying an unexpected permission string** (hand-edited DB, future value): must resolve to no access, not to anything higher. Test: `TestResolveSiteAccess` (Task 3).
5. **Restoring a backup taken before the TimescaleDB switch**: must restore cleanly and 044 must re-apply. Covered by checklist step 5 (Task 9). It cannot be unit tested without a database.

---

## File Structure

Backend:
- `backend/migrations/044_timescaledb.sql`: enable extension, create `metrics` schema.
- `backend/migrations/045_sites.sql`: `sites`, `site_sharing`.
- `backend/internal/database/timescale.go` + `_test.go`: preflight check and its message.
- `backend/internal/services/backup_service.go`: dump args extracted to `dumpArgs()`, adding `--schema=public`.
- `backend/internal/services/backup_args_test.go`: test for `dumpArgs`.
- `backend/internal/models/site.go` + `site_test.go`: `Site`, `SiteSharing`, `SiteInput`, `NormalizeSiteInput`.
- `backend/internal/models/audit.go`: site audit actions/resource.
- `backend/internal/services/site_access.go` + `site_access_test.go`: `SiteAccessLevel`, `resolveSiteAccess`.
- `backend/internal/services/site_service.go`: database operations.
- `backend/internal/api/site_handler.go` + `site_handler_test.go`: routes and handlers.
- `backend/cmd/sentinel/main.go`: preflight call, `SiteService`, route registration.

Deployment and docs:
- `docker-compose.yml`: postgres image and command.
- `repair.sh`: `--schema=public` on its safety dump.
- `GETTING_STARTED.md`, `README.md`: upgrade note and metrics-backup note.

Frontend:
- `frontend/src/hooks/useSites.ts`: types and API hooks.
- `frontend/src/components/Layout.tsx`: network nav switching.
- `frontend/src/App.tsx`: routes.
- `frontend/src/pages/network/Sites.tsx`: site list.
- `frontend/src/pages/network/SiteDetail.tsx`: site detail.
- `frontend/src/components/SiteFormModal.tsx`: create/edit form.
- `frontend/src/components/SiteSharingPanel.tsx`: sharing panel.
- `frontend/src/components/ShimmerStatCard.tsx`: `network` colour.
- `frontend/src/pages/Overview.tsx`: Network Monitoring card.
- `frontend/src/components/BackupRestore.tsx`: metrics note.

Frontend checks run in Docker (no Node on the host):

```bash
docker run --rm -v /home/sysadmin/sentinel/frontend:/app -w /app node:20-alpine sh -c "npx tsc --noEmit && npx eslint src"
```

---

### Task 1: TimescaleDB image, migration 044, and startup preflight

**Files:**
- Create: `backend/internal/database/timescale.go`
- Create: `backend/internal/database/timescale_test.go`
- Create: `backend/migrations/044_timescaledb.sql`
- Modify: `backend/cmd/sentinel/main.go:98-101` (call preflight before migrations)
- Modify: `docker-compose.yml` (postgres service)

**Interfaces:**
- Produces: `database.TimescaleMissingError(available bool) error` (nil when available) and `database.RequireTimescale(db *gorm.DB) error`.

Note on the spec: the spec says the preflight fails "if the check finds nothing, and 044 is not yet applied". If 044 has been applied the extension is installed, and an installed extension is always listed in `pg_available_extensions`; the only way it can be missing after 044 is an image rolled back to plain Postgres, where the same message is exactly right. The check is therefore simply "not available → fail".

- [ ] **Step 1: Write the failing test**

`backend/internal/database/timescale_test.go`:

```go
package database

import (
	"strings"
	"testing"
)

func TestTimescaleMissingError(t *testing.T) {
	if err := TimescaleMissingError(true); err != nil {
		t.Fatalf("available: got %v, want nil", err)
	}

	err := TimescaleMissingError(false)
	if err == nil {
		t.Fatal("unavailable: got nil, want an error")
	}
	// The message is what an operator sees when the backend refuses to start,
	// so it must say what is wrong and what to do, not just fail.
	for _, want := range []string{"TimescaleDB", "docker-compose.yml", "timescale/timescaledb", "docker compose up -d", "GETTING_STARTED.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/database/ -run TestTimescaleMissingError -v`
Expected: FAIL, `undefined: TimescaleMissingError`

- [ ] **Step 3: Write the implementation**

`backend/internal/database/timescale.go`:

```go
package database

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// errTimescaleMissing is what an install sees when its database has no
// TimescaleDB. Written for the person reading the startup log: most often
// someone who pulled a new backend but kept an old docker-compose.yml, whose
// postgres service is still plain postgres:16-alpine.
var errTimescaleMissing = errors.New(
	"Sentinel now requires the TimescaleDB extension. Update docker-compose.yml " +
		"from the current release (the postgres service uses the " +
		"timescale/timescaledb image) and run `docker compose up -d`. " +
		"See GETTING_STARTED.md.")

// TimescaleMissingError returns nil when the extension is available, and the
// explanation otherwise. Kept separate from the query so the message is tested
// without a database.
func TimescaleMissingError(available bool) error {
	if available {
		return nil
	}
	return errTimescaleMissing
}

// RequireTimescale checks, before any migration runs, that the server can load
// TimescaleDB. Without this, migration 044 fails with a bare SQL error that
// says nothing about the compose file.
func RequireTimescale(db *gorm.DB) error {
	var n int64
	if err := db.Raw(
		"SELECT count(*) FROM pg_available_extensions WHERE name = 'timescaledb'",
	).Scan(&n).Error; err != nil {
		return fmt.Errorf("checking for the TimescaleDB extension: %w", err)
	}
	return TimescaleMissingError(n > 0)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/database/ -v`
Expected: PASS

- [ ] **Step 5: Call the preflight before migrations**

In `backend/cmd/sentinel/main.go`, replace:

```go
	// 2. Migrations.
	if err := runMigrations(db, cfg.MigrationsDir); err != nil {
```

with:

```go
	// 2. Migrations. TimescaleDB first: migration 044 needs it, and without
	// this check an old compose file shows up as a bare SQL error.
	if err := database.RequireTimescale(db); err != nil {
		return err
	}
	if err := runMigrations(db, cfg.MigrationsDir); err != nil {
```

`database` is already imported in main.go (it calls `database.NewDB`).

- [ ] **Step 6: Add migration 044**

`backend/migrations/044_timescaledb.sql`:

```sql
-- 044_timescaledb.sql
-- TimescaleDB for network metrics (network monitoring roadmap, phase 0).
--
-- Nothing here touches an existing table. The metrics schema is created empty:
-- phase 2 adds its hypertables there. Keeping metrics out of public is what
-- lets backups dump public only (see BackupService.dumpArgs).
--
-- Idempotent on purpose: restoring a backup taken before this migration
-- rewrites schema_migrations without this row, so it runs again on the next
-- boot and must be harmless when everything already exists.
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE SCHEMA IF NOT EXISTS metrics;
```

- [ ] **Step 7: Switch the compose image**

In `docker-compose.yml`, in the `postgres` service, replace:

```yaml
    image: postgres:16-alpine
```

with:

```yaml
    # TimescaleDB is Postgres 16 plus the extension, on the same Alpine base as
    # postgres:16-alpine, so an existing data directory opens unchanged.
    # Pinned: a pull must never change the database engine underneath an
    # install. Upgrading is a deliberate edit plus ALTER EXTENSION ... UPDATE.
    image: timescale/timescaledb:2.30.1-pg16
    # The image only writes shared_preload_libraries into postgresql.conf when
    # it initialises a NEW data directory. Every upgraded install already has
    # one, so it is passed here or CREATE EXTENSION fails. Telemetry is off:
    # Sentinel does not phone home, and neither should its database.
    command:
      - postgres
      - -c
      - shared_preload_libraries=timescaledb
      - -c
      - timescaledb.telemetry_level=off
```

Leave volumes, environment, ports and healthcheck unchanged.

- [ ] **Step 8: Build and run all backend tests**

Run: `cd /home/sysadmin/sentinel/backend && go build ./... && go vet ./... && go test ./internal/...`
Expected: build succeeds, all packages `ok`.

- [ ] **Step 9: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/internal/database/timescale.go backend/internal/database/timescale_test.go \
  backend/migrations/044_timescaledb.sql backend/cmd/sentinel/main.go docker-compose.yml
git commit -m "feat(db): move to TimescaleDB, refusing to start without it

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Configuration-only backups

**Files:**
- Modify: `backend/internal/services/backup_service.go:150-168` (extract `dumpArgs`)
- Create: `backend/internal/services/backup_args_test.go`
- Modify: `repair.sh:271`
- Modify: `frontend/src/components/BackupRestore.tsx:111-116`
- Modify: `GETTING_STARTED.md`, `README.md`

**Interfaces:**
- Produces: `func (s *BackupService) dumpArgs() []string`.

- [ ] **Step 1: Write the failing test**

`backend/internal/services/backup_args_test.go`:

```go
package services

import (
	"slices"
	"testing"
)

// Backups are configuration only. Dumping just the public schema leaves out the
// metrics schema, TimescaleDB's own schemas, and every extension. The last one
// matters most: with --clean, a dump that included the timescaledb extension
// would begin its restore with DROP EXTENSION, which cascades to every
// hypertable.
func TestDumpArgsArePublicSchemaOnly(t *testing.T) {
	s := NewBackupService("/tmp/x", DBConfig{Host: "h", Port: "5432", User: "u", Name: "n"})
	args := s.dumpArgs()

	for _, want := range []string{"--schema=public", "--clean", "--if-exists", "--no-owner", "--no-privileges"} {
		if !slices.Contains(args, want) {
			t.Errorf("dump args %v missing %q", args, want)
		}
	}
	for _, pair := range [][2]string{{"--host", "h"}, {"--port", "5432"}, {"--username", "u"}, {"--dbname", "n"}} {
		i := slices.Index(args, pair[0])
		if i < 0 || i+1 >= len(args) || args[i+1] != pair[1] {
			t.Errorf("dump args %v: want %s %s", args, pair[0], pair[1])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/services/ -run TestDumpArgsArePublicSchemaOnly -v`
Expected: FAIL, `s.dumpArgs undefined`

- [ ] **Step 3: Extract `dumpArgs` and add the schema flag**

In `backend/internal/services/backup_service.go`, add above `Create`:

```go
// dumpArgs are the pg_dump arguments for a backup.
//
// --clean --if-exists makes the dump able to restore over a populated
// database: it drops each object before recreating it. That is what lets a
// restore run without dropping the database itself, which postgres refuses
// while anything is connected — and the application is always connected.
//
// --no-owner and --no-privileges keep the dump portable, so it can be
// restored into an install whose database role has a different name.
//
// --schema=public makes backups configuration only. Sentinel's own tables all
// live in public; collected network metrics live in the metrics schema and
// are deliberately left out (they are protected at the volume level). It also
// keeps extensions out of the dump: with --clean, including timescaledb would
// make a restore start with DROP EXTENSION, cascading to every hypertable.
func (s *BackupService) dumpArgs() []string {
	return []string{
		"--host", s.db.Host,
		"--port", s.db.Port,
		"--username", s.db.User,
		"--dbname", s.db.Name,
		"--schema=public",
		"--clean", "--if-exists",
		"--no-owner", "--no-privileges",
	}
}
```

In `Create`, replace the comment block and the `exec.CommandContext(runCtx, "pg_dump", ...)` call (from `// --clean --if-exists makes the dump` through the closing `)`) with:

```go
	cmd := exec.CommandContext(runCtx, "pg_dump", s.dumpArgs()...)
```

- [ ] **Step 4: Run tests**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/services/ -v -run 'TestDumpArgs' && go build ./...`
Expected: PASS, build succeeds.

- [ ] **Step 5: Same scope for `repair.sh`**

In `repair.sh`, replace:

```bash
  if $DC exec -T postgres pg_dump -U sentinel sentinel > "$file" 2>>"$LOG"; then
```

with:

```bash
  # public only: Sentinel's configuration. Collected network metrics live in
  # the metrics schema and would make this safety dump grow without bound.
  if $DC exec -T postgres pg_dump -U sentinel --schema=public sentinel > "$file" 2>>"$LOG"; then
```

Run: `bash -n /home/sysadmin/sentinel/repair.sh`
Expected: no output (syntax OK).

- [ ] **Step 6: Say it in the UI**

In `frontend/src/components/BackupRestore.tsx`, replace the paragraph:

```tsx
            <p className="text-xs text-slate-500">
              Stored in <code>{directory}</code>, readable only by the server. A backup contains
              password hashes, notification channel secrets and agent tokens — treat a downloaded
              file as you would those.
            </p>
```

with:

```tsx
            <p className="text-xs text-slate-500">
              Stored in <code>{directory}</code>, readable only by the server. A backup contains
              password hashes, notification channel secrets and agent tokens — treat a downloaded
              file as you would those.
            </p>
            {/* Said up front so nobody discovers it during a restore: metric
                history is left out on purpose, and has its own protection. */}
            <p className="text-xs text-slate-500">
              Backups hold configuration and history, not collected network metrics. To protect
              those, snapshot the database volume — see “Backing up network metrics” in
              GETTING_STARTED.md.
            </p>
```

- [ ] **Step 7: Document the upgrade and the metrics backup**

Append to `GETTING_STARTED.md`:

```markdown
## Upgrading to TimescaleDB

Sentinel's database now runs on TimescaleDB, which is PostgreSQL 16 with an
extension for time-series data. Existing installs keep their data: the new
image opens the same data directory.

1. Take a backup in **Settings → Backups**.
2. Pull the current `docker-compose.yml`. The `postgres` service uses
   `timescale/timescaledb:2.30.1-pg16` and passes
   `shared_preload_libraries=timescaledb` on its command line.
3. Run `docker compose up -d`. The backend enables the extension itself.

If the backend refuses to start with "Sentinel now requires the TimescaleDB
extension", the postgres service is still on the old image. Repeat step 2.

## Backing up network metrics

**Settings → Backups** holds configuration and history: monitors, sites,
credentials, reports, incidents and settings. It deliberately leaves out
collected network metrics, which can grow to many gigabytes.

To protect those too, back up the whole database from the host, for example:

    docker compose exec -T postgres pg_dump -U sentinel -Fc sentinel > sentinel-full.dump

or snapshot the Docker volume holding `/var/lib/postgresql/data`.
```

In `README.md`, find the installation or upgrade section (`grep -n -i 'upgrad\|install' README.md`) and add one line under it:

```markdown
> Upgrading an existing install? The database now runs on TimescaleDB. See
> "Upgrading to TimescaleDB" in GETTING_STARTED.md.
```

- [ ] **Step 8: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/internal/services/backup_service.go backend/internal/services/backup_args_test.go \
  repair.sh frontend/src/components/BackupRestore.tsx GETTING_STARTED.md README.md
git commit -m "feat(backup): back up configuration only, never extensions or metrics

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Site models, migration 045, and the access rules

**Files:**
- Create: `backend/migrations/045_sites.sql`
- Create: `backend/internal/models/site.go`
- Create: `backend/internal/models/site_test.go`
- Modify: `backend/internal/models/audit.go` (constants)
- Create: `backend/internal/services/site_access.go`
- Create: `backend/internal/services/site_access_test.go`

**Interfaces:**
- Consumes: `models.PermissionReadonly`, `models.PermissionEditable`, `models.ValidateSharePermission` (existing, `backend/internal/models/monitor_sharing.go`).
- Produces:
  - `models.Site{ID uuid.UUID; Name string; Description *string; Address *string; CreatedBy *uuid.UUID; CreatedAt, UpdatedAt time.Time}` with JSON names `id, name, description, address, created_by, created_at, updated_at`.
  - `models.SiteSharing{ID, SiteID, SharedWithUserID uuid.UUID; Permission string; SharedByUserID *uuid.UUID; CreatedAt, UpdatedAt time.Time}` with JSON names `id, site_id, shared_with_user_id, permission, shared_by_user_id, created_at, updated_at`.
  - `models.SiteInput{Name string; Description *string; Address *string}` (JSON `name, description, address`) and `models.NormalizeSiteInput(in SiteInput) (SiteInput, error)`.
  - `models.ActionSiteCreated/Updated/Deleted/Shared/Unshared`, `models.ResourceSite`.
  - `services.SiteAccessLevel` with `SiteAccessNone`, `SiteAccessReadonly`, `SiteAccessEditable`, `SiteAccessAdmin`, method `String() string` returning `none|readonly|editable|admin`.
  - `services.resolveSiteAccess(isAdmin bool, share *models.SiteSharing) SiteAccessLevel` (package-private).

- [ ] **Step 1: Write the failing model test**

`backend/internal/models/site_test.go`:

```go
package models

import (
	"strings"
	"testing"
)

func strp(s string) *string { return &s }

func TestNormalizeSiteInput(t *testing.T) {
	t.Run("trims name and optional fields", func(t *testing.T) {
		got, err := NormalizeSiteInput(SiteInput{Name: "  HQ  ", Description: strp("  main office "), Address: strp("  ")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "HQ" {
			t.Errorf("name = %q, want %q", got.Name, "HQ")
		}
		if got.Description == nil || *got.Description != "main office" {
			t.Errorf("description = %v, want %q", got.Description, "main office")
		}
		// A blank optional field is stored as NULL, not as an empty string, so
		// "no address" has one representation.
		if got.Address != nil {
			t.Errorf("address = %q, want nil", *got.Address)
		}
	})

	t.Run("whitespace-only name is rejected", func(t *testing.T) {
		if _, err := NormalizeSiteInput(SiteInput{Name: "   "}); err == nil {
			t.Fatal("want error for blank name")
		}
	})

	t.Run("name longer than 255 characters is rejected", func(t *testing.T) {
		if _, err := NormalizeSiteInput(SiteInput{Name: strings.Repeat("a", 256)}); err == nil {
			t.Fatal("want error for 256-character name")
		}
	})

	t.Run("255 characters is allowed", func(t *testing.T) {
		if _, err := NormalizeSiteInput(SiteInput{Name: strings.Repeat("é", 255)}); err != nil {
			t.Fatalf("255 characters (multi-byte) should pass: %v", err)
		}
	})
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/models/ -run TestNormalizeSiteInput -v`
Expected: FAIL, `undefined: NormalizeSiteInput`

- [ ] **Step 3: Write the models**

`backend/internal/models/site.go`:

```go
package models

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Site is a place whose network Sentinel monitors: the unit that devices,
// maps, dashboards and network reports belong to, and the unit of sharing.
type Site struct {
	ID          uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name        string     `json:"name" gorm:"column:name;not null"`
	Description *string    `json:"description" gorm:"column:description"`
	Address     *string    `json:"address" gorm:"column:address"`
	CreatedBy   *uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid"`
	CreatedAt   time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table name.
func (Site) TableName() string { return "sites" }

// SiteSharing grants one user access to one site. Mirrors MonitorSharing;
// Permission is PermissionReadonly or PermissionEditable.
type SiteSharing struct {
	ID               uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID           uuid.UUID  `json:"site_id" gorm:"column:site_id;type:uuid;not null"`
	SharedWithUserID uuid.UUID  `json:"shared_with_user_id" gorm:"column:shared_with_user_id;type:uuid;not null"`
	Permission       string     `json:"permission" gorm:"column:permission;not null"`
	SharedByUserID   *uuid.UUID `json:"shared_by_user_id" gorm:"column:shared_by_user_id;type:uuid"`
	CreatedAt        time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt        time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table name.
func (SiteSharing) TableName() string { return "site_sharing" }

// SiteInput is what a create or update request supplies.
type SiteInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Address     *string `json:"address"`
}

// maxSiteNameLen matches the VARCHAR(255) column, counted in characters as
// Postgres counts them, not bytes.
const maxSiteNameLen = 255

// NormalizeSiteInput trims every field, turns blank optional fields into nil,
// and validates the name. Trimming happens here rather than in the database so
// " HQ" and "HQ" collide on the unique index instead of both being stored.
func NormalizeSiteInput(in SiteInput) (SiteInput, error) {
	out := SiteInput{Name: strings.TrimSpace(in.Name)}
	if out.Name == "" {
		return SiteInput{}, errors.New("name is required")
	}
	if utf8.RuneCountInString(out.Name) > maxSiteNameLen {
		return SiteInput{}, errors.New("name must be 255 characters or fewer")
	}
	out.Description = trimOptional(in.Description)
	out.Address = trimOptional(in.Address)
	return out, nil
}

func trimOptional(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}
```

- [ ] **Step 4: Run the model test**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/models/ -run TestNormalizeSiteInput -v`
Expected: PASS

- [ ] **Step 5: Add audit constants**

In `backend/internal/models/audit.go`, add to the "Audited actions" const block:

```go
	ActionSiteCreated  = "site_created"
	ActionSiteUpdated  = "site_updated"
	ActionSiteDeleted  = "site_deleted"
	ActionSiteShared   = "site_shared"
	ActionSiteUnshared = "site_unshared"
```

and to the "Audited resource types" block:

```go
	ResourceSite = "site"
```

- [ ] **Step 6: Write the failing access test**

`backend/internal/services/site_access_test.go`:

```go
package services

import (
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestResolveSiteAccess(t *testing.T) {
	share := func(p string) *models.SiteSharing { return &models.SiteSharing{Permission: p} }

	cases := []struct {
		name    string
		isAdmin bool
		share   *models.SiteSharing
		want    SiteAccessLevel
	}{
		{"admin without a share", true, nil, SiteAccessAdmin},
		// A share never lowers an admin: sharing a site with an admin must not
		// take away their ability to manage it.
		{"admin with a readonly share", true, share(models.PermissionReadonly), SiteAccessAdmin},
		{"readonly share", false, share(models.PermissionReadonly), SiteAccessReadonly},
		{"editable share", false, share(models.PermissionEditable), SiteAccessEditable},
		{"no share", false, nil, SiteAccessNone},
		// Anything unrecognised fails closed. A hand-edited row or a value
		// added by a future version must not be read as more access.
		{"unknown permission", false, share("owner"), SiteAccessNone},
		{"empty permission", false, share(""), SiteAccessNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveSiteAccess(tc.isAdmin, tc.share); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSiteAccessLevelString(t *testing.T) {
	for level, want := range map[SiteAccessLevel]string{
		SiteAccessNone: "none", SiteAccessReadonly: "readonly",
		SiteAccessEditable: "editable", SiteAccessAdmin: "admin",
	} {
		if got := level.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", level, got, want)
		}
	}
}
```

- [ ] **Step 7: Run to verify it fails**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/services/ -run 'TestResolveSiteAccess|TestSiteAccessLevelString' -v`
Expected: FAIL, `undefined: SiteAccessLevel`

- [ ] **Step 8: Write the access rules**

`backend/internal/services/site_access.go`:

```go
package services

import "github.com/Stevy2191/Sentinel/backend/internal/models"

// SiteAccessLevel is what a user may do in a site. Levels are ordered: each
// includes everything below it, so callers compare with >=.
type SiteAccessLevel int

const (
	// SiteAccessNone: the site does not exist as far as this user is concerned.
	SiteAccessNone SiteAccessLevel = iota
	// SiteAccessReadonly: see the site and everything in it.
	SiteAccessReadonly
	// SiteAccessEditable: also change what is inside the site (devices, maps,
	// dashboards, in later phases), but not the site itself.
	SiteAccessEditable
	// SiteAccessAdmin: also rename or delete the site and manage its sharing.
	SiteAccessAdmin
)

// String is the level's name as the API reports it.
func (l SiteAccessLevel) String() string {
	switch l {
	case SiteAccessReadonly:
		return "readonly"
	case SiteAccessEditable:
		return "editable"
	case SiteAccessAdmin:
		return "admin"
	default:
		return "none"
	}
}

// resolveSiteAccess holds every site permission rule, with no database, so
// the rules are tested directly. Admins manage every site; everyone else gets
// exactly what their share says, and anything unrecognised is no access.
func resolveSiteAccess(isAdmin bool, share *models.SiteSharing) SiteAccessLevel {
	if isAdmin {
		return SiteAccessAdmin
	}
	if share == nil {
		return SiteAccessNone
	}
	switch share.Permission {
	case models.PermissionReadonly:
		return SiteAccessReadonly
	case models.PermissionEditable:
		return SiteAccessEditable
	default:
		return SiteAccessNone
	}
}
```

- [ ] **Step 9: Run the access tests**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/services/ -run 'TestResolveSiteAccess|TestSiteAccessLevelString' -v`
Expected: PASS

- [ ] **Step 10: Add migration 045**

`backend/migrations/045_sites.sql`:

```sql
-- 045_sites.sql
-- Sites and per-site sharing (network monitoring roadmap, phase 0).
--
-- site_sharing mirrors monitor_sharing (010) with two deliberate differences:
-- shared_by_user_id is SET NULL rather than a blocking reference, so deleting
-- the admin who shared a site does not fail, and permission is constrained here
-- as well as in Go.

CREATE TABLE IF NOT EXISTS sites (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    address     TEXT,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Case-insensitive: "HQ" and "hq" would be indistinguishable in every site
-- picker later phases add.
CREATE UNIQUE INDEX IF NOT EXISTS idx_sites_name_lower ON sites (lower(name));

CREATE TABLE IF NOT EXISTS site_sharing (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id             UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    shared_with_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission          VARCHAR(50) NOT NULL DEFAULT 'readonly',
    shared_by_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (site_id, shared_with_user_id),
    CONSTRAINT site_sharing_permission_check CHECK (permission IN ('readonly', 'editable'))
);

CREATE INDEX IF NOT EXISTS idx_site_sharing_user_id ON site_sharing (shared_with_user_id);
```

- [ ] **Step 11: Run all backend tests and commit**

Run: `cd /home/sysadmin/sentinel/backend && go vet ./... && go test ./internal/...`
Expected: all `ok`.

```bash
cd /home/sysadmin/sentinel
git add backend/migrations/045_sites.sql backend/internal/models/site.go backend/internal/models/site_test.go \
  backend/internal/models/audit.go backend/internal/services/site_access.go backend/internal/services/site_access_test.go
git commit -m "feat(sites): site and sharing models with one set of access rules

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: SiteService (database operations)

**Files:**
- Create: `backend/internal/services/site_service.go`

**Interfaces:**
- Consumes: `models.Site`, `models.SiteSharing`, `models.SiteInput`, `resolveSiteAccess`, `SiteAccessLevel` (Task 3); `isDuplicateKey(err error) bool` (existing, `backend/internal/services/ssl_checker.go:680`).
- Produces (all used by Task 5 through the `siteStore` interface):
  - `var ErrSiteNotFound, ErrSiteNameTaken, ErrSiteNotEmpty, ErrSiteShareNotFound, ErrSiteShareUnknownUser error`
  - `type SiteShareView struct { models.SiteSharing; Username string \`json:"username"\`; Email string \`json:"email"\` }`
  - `func NewSiteService(db *gorm.DB) *SiteService`
  - `func (s *SiteService) List(ctx context.Context, userID uuid.UUID, isAdmin bool) ([]models.Site, error)`
  - `func (s *SiteService) Get(ctx context.Context, id uuid.UUID) (*models.Site, error)`
  - `func (s *SiteService) SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (SiteAccessLevel, error)`
  - `func (s *SiteService) Create(ctx context.Context, in models.SiteInput, createdBy uuid.UUID) (*models.Site, error)`
  - `func (s *SiteService) Update(ctx context.Context, id uuid.UUID, in models.SiteInput) (before, after *models.Site, err error)`
  - `func (s *SiteService) Delete(ctx context.Context, id uuid.UUID) (*models.Site, error)`
  - `func (s *SiteService) ListShares(ctx context.Context, siteID uuid.UUID) ([]SiteShareView, error)`
  - `func (s *SiteService) UpsertShare(ctx context.Context, siteID, userID, sharedBy uuid.UUID, permission string) (*models.SiteSharing, error)`
  - `func (s *SiteService) RemoveShare(ctx context.Context, siteID, userID uuid.UUID) error`

This service is database code. The project has no database test harness yet (the spec defers one to phase 2), so it is verified by the sandbox checklist in Task 9, and its callers are unit tested through a fake in Task 5. Keep all rules in `resolveSiteAccess` and `NormalizeSiteInput`; this file should only move rows.

- [ ] **Step 1: Write the service**

`backend/internal/services/site_service.go`:

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

var (
	ErrSiteNotFound         = errors.New("site not found")
	ErrSiteNameTaken        = errors.New("a site with this name already exists")
	ErrSiteNotEmpty         = errors.New("this site still contains items; remove them before deleting it")
	ErrSiteShareNotFound    = errors.New("this site is not shared with that user")
	ErrSiteShareUnknownUser = errors.New("no such user")
)

// siteContentTables lists the tables whose rows belong to a site, each with a
// site_id column. Delete refuses while any of them has a row for the site, so
// deleting a site is never a way to silently delete its devices.
//
// Empty in phase 0: nothing can belong to a site yet. Each later phase adds
// its own tables (devices in phase 1, dashboards in 4, maps in 6).
var siteContentTables = []string{}

// SiteShareView is a share with the recipient's name, for the sharing panel.
type SiteShareView struct {
	models.SiteSharing
	Username string `json:"username"`
	Email    string `json:"email"`
}

// SiteService stores sites and their sharing. Permission rules live in
// resolveSiteAccess, not here.
type SiteService struct {
	db *gorm.DB
}

func NewSiteService(db *gorm.DB) *SiteService {
	return &SiteService{db: db}
}

// List returns every site for an admin, and the shared ones for anyone else.
func (s *SiteService) List(ctx context.Context, userID uuid.UUID, isAdmin bool) ([]models.Site, error) {
	q := s.db.WithContext(ctx).Model(&models.Site{})
	if !isAdmin {
		q = q.Where("id IN (SELECT site_id FROM site_sharing WHERE shared_with_user_id = ?)", userID)
	}
	var sites []models.Site
	if err := q.Order("lower(name)").Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("listing sites: %w", err)
	}
	return sites, nil
}

// Get returns one site, or ErrSiteNotFound.
func (s *SiteService) Get(ctx context.Context, id uuid.UUID) (*models.Site, error) {
	var site models.Site
	err := s.db.WithContext(ctx).First(&site, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSiteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading site: %w", err)
	}
	return &site, nil
}

// SiteAccess is what userID may do in siteID. It returns ErrSiteNotFound for a
// site that does not exist, so callers can answer 404 for both "missing" and
// "not yours" without telling the two apart to the client.
func (s *SiteService) SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (SiteAccessLevel, error) {
	if _, err := s.Get(ctx, siteID); err != nil {
		return SiteAccessNone, err
	}
	if isAdmin {
		return resolveSiteAccess(true, nil), nil
	}
	var share models.SiteSharing
	err := s.db.WithContext(ctx).
		Where("site_id = ? AND shared_with_user_id = ?", siteID, userID).
		First(&share).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return resolveSiteAccess(false, nil), nil
	}
	if err != nil {
		return SiteAccessNone, fmt.Errorf("loading site share: %w", err)
	}
	return resolveSiteAccess(false, &share), nil
}

// Create stores a new site. in must already be normalized.
func (s *SiteService) Create(ctx context.Context, in models.SiteInput, createdBy uuid.UUID) (*models.Site, error) {
	site := models.Site{Name: in.Name, Description: in.Description, Address: in.Address, CreatedBy: &createdBy}
	if err := s.db.WithContext(ctx).Create(&site).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrSiteNameTaken
		}
		return nil, fmt.Errorf("creating site: %w", err)
	}
	return &site, nil
}

// Update replaces a site's editable fields. in must already be normalized.
// Returns the site before and after, for the audit log.
func (s *SiteService) Update(ctx context.Context, id uuid.UUID, in models.SiteInput) (*models.Site, *models.Site, error) {
	before, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	err = s.db.WithContext(ctx).Model(&models.Site{}).Where("id = ?", id).
		Updates(map[string]any{"name": in.Name, "description": in.Description, "address": in.Address, "updated_at": gorm.Expr("now()")}).Error
	if err != nil {
		if isDuplicateKey(err) {
			return nil, nil, ErrSiteNameTaken
		}
		return nil, nil, fmt.Errorf("updating site: %w", err)
	}
	after, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

// Delete removes an empty site and its shares. Returns the deleted site, for
// the audit log.
func (s *SiteService) Delete(ctx context.Context, id uuid.UUID) (*models.Site, error) {
	site, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, table := range siteContentTables {
		var n int64
		// table comes from the constant list above, never from input.
		if err := s.db.WithContext(ctx).Table(table).Where("site_id = ?", id).Count(&n).Error; err != nil {
			return nil, fmt.Errorf("checking %s for site contents: %w", table, err)
		}
		if n > 0 {
			return nil, ErrSiteNotEmpty
		}
	}
	// site_sharing rows go with it (ON DELETE CASCADE).
	if err := s.db.WithContext(ctx).Delete(&models.Site{}, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("deleting site: %w", err)
	}
	return site, nil
}

// ListShares returns everyone the site is shared with, oldest first.
func (s *SiteService) ListShares(ctx context.Context, siteID uuid.UUID) ([]SiteShareView, error) {
	if _, err := s.Get(ctx, siteID); err != nil {
		return nil, err
	}
	var out []SiteShareView
	err := s.db.WithContext(ctx).
		Table("site_sharing").
		Select("site_sharing.*, users.username, users.email").
		Joins("JOIN users ON users.id = site_sharing.shared_with_user_id").
		Where("site_sharing.site_id = ?", siteID).
		Order("site_sharing.created_at ASC").
		Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing site shares: %w", err)
	}
	return out, nil
}

// UpsertShare grants userID access to siteID, or changes the permission of an
// existing share. permission must already be validated.
func (s *SiteService) UpsertShare(ctx context.Context, siteID, userID, sharedBy uuid.UUID, permission string) (*models.SiteSharing, error) {
	if _, err := s.Get(ctx, siteID); err != nil {
		return nil, err
	}
	share := models.SiteSharing{SiteID: siteID, SharedWithUserID: userID, Permission: permission, SharedByUserID: &sharedBy}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "site_id"}, {Name: "shared_with_user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"permission":        permission,
			"shared_by_user_id": sharedBy,
			"updated_at":        gorm.Expr("now()"),
		}),
	}).Create(&share).Error
	if err != nil {
		if isForeignKeyViolation(err) {
			return nil, ErrSiteShareUnknownUser
		}
		return nil, fmt.Errorf("sharing site: %w", err)
	}
	// Reload: on conflict, share.ID is not the stored row's id.
	var stored models.SiteSharing
	if err := s.db.WithContext(ctx).
		Where("site_id = ? AND shared_with_user_id = ?", siteID, userID).
		First(&stored).Error; err != nil {
		return nil, fmt.Errorf("loading site share: %w", err)
	}
	return &stored, nil
}

// RemoveShare revokes userID's access to siteID.
func (s *SiteService) RemoveShare(ctx context.Context, siteID, userID uuid.UUID) error {
	res := s.db.WithContext(ctx).
		Where("site_id = ? AND shared_with_user_id = ?", siteID, userID).
		Delete(&models.SiteSharing{})
	if res.Error != nil {
		return fmt.Errorf("removing site share: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrSiteShareNotFound
	}
	return nil
}

// isForeignKeyViolation reports whether err is a foreign-key violation, which
// for a share means the user id does not exist (the site was checked first).
func isForeignKeyViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "foreign key") || strings.Contains(msg, "23503")
}
```

- [ ] **Step 2: Build and vet**

Run: `cd /home/sysadmin/sentinel/backend && go build ./... && go vet ./internal/services/`
Expected: no output. If `gorm.io/gorm/clause` is reported missing, it is part of the `gorm.io/gorm` module already required in `go.mod`; run `go mod tidy` only if the build asks for it.

- [ ] **Step 3: Run the services tests**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/services/`
Expected: `ok`

- [ ] **Step 4: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/internal/services/site_service.go
git commit -m "feat(sites): SiteService for sites and their sharing

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Site API handlers and routes

**Files:**
- Create: `backend/internal/api/site_handler.go`
- Create: `backend/internal/api/site_handler_test.go`
- Modify: `backend/cmd/sentinel/main.go` (construct `SiteService`, register routes after `RegisterBackupRoutes`, line ~317)

**Interfaces:**
- Consumes: everything `SiteService` produces (Task 4), `models.NormalizeSiteInput`, `models.ValidateSharePermission`, `models.Action*`/`models.ResourceSite` (Task 3), and existing `RequireAdmin(users adminChecker)`, `GetUserFromContext`, `respondError`, `respondSuccess`, `respondInternal`, `actorFrom`, `auditRecorder` (all in package `api`).
- Produces: `func RegisterSiteRoutes(rg *gin.RouterGroup, sites siteStore, audit auditRecorder, users adminChecker)`; JSON: `GET /sites` → `data: Site[]`; `GET /sites/:id` → `data: Site & {access: "readonly"|"editable"|"admin"}`; `POST /sites` → 201 `data: Site`; `PUT /sites/:id` → `data: Site`; `DELETE /sites/:id` → `data: {deleted: true}`; `GET /sites/:id/shares` → `data: SiteShareView[]`; `POST /sites/:id/shares` body `{user_id, permission}` → `data: SiteSharing`; `DELETE /sites/:id/shares/:user_id` → `data: {removed: true}`.

- [ ] **Step 1: Write the failing tests**

`backend/internal/api/site_handler_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// fakeSites is an in-memory siteStore. mutations counts every call that would
// change data, so tests can prove a refused request changed nothing.
type fakeSites struct {
	access    services.SiteAccessLevel
	accessErr error
	createErr error
	deleteErr error
	shareErr  error
	mutations int
}

func (f *fakeSites) List(context.Context, uuid.UUID, bool) ([]models.Site, error) {
	return []models.Site{{ID: uuid.New(), Name: "HQ"}}, nil
}
func (f *fakeSites) Get(_ context.Context, id uuid.UUID) (*models.Site, error) {
	return &models.Site{ID: id, Name: "HQ"}, nil
}
func (f *fakeSites) SiteAccess(context.Context, uuid.UUID, bool, uuid.UUID) (services.SiteAccessLevel, error) {
	return f.access, f.accessErr
}
func (f *fakeSites) Create(_ context.Context, in models.SiteInput, _ uuid.UUID) (*models.Site, error) {
	f.mutations++
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &models.Site{ID: uuid.New(), Name: in.Name}, nil
}
func (f *fakeSites) Update(_ context.Context, id uuid.UUID, in models.SiteInput) (*models.Site, *models.Site, error) {
	f.mutations++
	return &models.Site{ID: id, Name: "old"}, &models.Site{ID: id, Name: in.Name}, nil
}
func (f *fakeSites) Delete(_ context.Context, id uuid.UUID) (*models.Site, error) {
	f.mutations++
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &models.Site{ID: id, Name: "HQ"}, nil
}
func (f *fakeSites) ListShares(context.Context, uuid.UUID) ([]services.SiteShareView, error) {
	return nil, nil
}
func (f *fakeSites) UpsertShare(_ context.Context, siteID, userID, _ uuid.UUID, p string) (*models.SiteSharing, error) {
	f.mutations++
	if f.shareErr != nil {
		return nil, f.shareErr
	}
	return &models.SiteSharing{SiteID: siteID, SharedWithUserID: userID, Permission: p}, nil
}
func (f *fakeSites) RemoveShare(context.Context, uuid.UUID, uuid.UUID) error {
	f.mutations++
	return nil
}

type fakeAudit struct{ entries []string }

func (a *fakeAudit) Record(_ context.Context, _ services.Actor, action, _ string, _ *uuid.UUID, _ models.AuditChanges) {
	a.entries = append(a.entries, action)
}

// siteRouter mounts the site routes as a signed-in user with the given admin
// claim. stubUsers (auth_middleware_test.go) confirms the claim.
func siteRouter(store *fakeSites, audit *fakeAudit, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	RegisterSiteRoutes(r.Group("/api/v1"), store, audit, stubUsers{user: &models.User{IsAdmin: isAdmin}})
	return r
}

func do(r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// A non-admin must be refused by every admin route, with a well-formed body,
// and nothing may change: no store mutation and no audit entry.
func TestSiteAdminRoutesDoNotRunForNonAdmins(t *testing.T) {
	id := uuid.New().String()
	routes := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/sites", map[string]any{"name": "HQ"}},
		{http.MethodPut, "/api/v1/sites/" + id, map[string]any{"name": "HQ"}},
		{http.MethodDelete, "/api/v1/sites/" + id, nil},
		{http.MethodGet, "/api/v1/sites/" + id + "/shares", nil},
		{http.MethodPost, "/api/v1/sites/" + id + "/shares", map[string]any{"user_id": uuid.New(), "permission": "readonly"}},
		{http.MethodDelete, "/api/v1/sites/" + id + "/shares/" + uuid.New().String(), nil},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			store := &fakeSites{access: services.SiteAccessEditable}
			audit := &fakeAudit{}
			w := do(siteRouter(store, audit, false), rt.method, rt.path, rt.body)
			if w.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", w.Code)
			}
			if store.mutations != 0 || len(audit.entries) != 0 {
				t.Errorf("refused request changed data: %d mutations, audit %v", store.mutations, audit.entries)
			}
		})
	}
}

func TestSiteHandlers(t *testing.T) {
	id := uuid.New().String()

	t.Run("site without access is 404, not 403", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{access: services.SiteAccessNone}, &fakeAudit{}, false), http.MethodGet, "/api/v1/sites/"+id, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("missing site is 404", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{accessErr: services.ErrSiteNotFound}, &fakeAudit{}, false), http.MethodGet, "/api/v1/sites/"+id, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("shared site reports the caller's access", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{access: services.SiteAccessReadonly}, &fakeAudit{}, false), http.MethodGet, "/api/v1/sites/"+id, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var body struct {
			Data struct {
				Access string `json:"access"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Data.Access != "readonly" {
			t.Errorf("access = %q, want readonly", body.Data.Access)
		}
	})

	t.Run("malformed id is 400", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{}, &fakeAudit{}, true), http.MethodGet, "/api/v1/sites/not-a-uuid", nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("blank name is 400 and never reaches the store", func(t *testing.T) {
		store := &fakeSites{}
		w := do(siteRouter(store, &fakeAudit{}, true), http.MethodPost, "/api/v1/sites", map[string]any{"name": "   "})
		if w.Code != http.StatusBadRequest || store.mutations != 0 {
			t.Errorf("status = %d, mutations = %d; want 400 and 0", w.Code, store.mutations)
		}
	})

	t.Run("duplicate name is 409", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{createErr: services.ErrSiteNameTaken}, &fakeAudit{}, true), http.MethodPost, "/api/v1/sites", map[string]any{"name": "hq"})
		if w.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409", w.Code)
		}
	})

	t.Run("admin create is 201 and audited", func(t *testing.T) {
		audit := &fakeAudit{}
		w := do(siteRouter(&fakeSites{}, audit, true), http.MethodPost, "/api/v1/sites", map[string]any{"name": "HQ"})
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", w.Code)
		}
		if len(audit.entries) != 1 || audit.entries[0] != models.ActionSiteCreated {
			t.Errorf("audit = %v, want [%s]", audit.entries, models.ActionSiteCreated)
		}
	})

	t.Run("deleting a non-empty site is 409", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{deleteErr: services.ErrSiteNotEmpty}, &fakeAudit{}, true), http.MethodDelete, "/api/v1/sites/"+id, nil)
		if w.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409", w.Code)
		}
	})

	t.Run("invalid share permission is 400", func(t *testing.T) {
		store := &fakeSites{}
		w := do(siteRouter(store, &fakeAudit{}, true), http.MethodPost, "/api/v1/sites/"+id+"/shares",
			map[string]any{"user_id": uuid.New(), "permission": "owner"})
		if w.Code != http.StatusBadRequest || store.mutations != 0 {
			t.Errorf("status = %d, mutations = %d; want 400 and 0", w.Code, store.mutations)
		}
	})

	t.Run("unknown user is 400", func(t *testing.T) {
		w := do(siteRouter(&fakeSites{shareErr: services.ErrSiteShareUnknownUser}, &fakeAudit{}, true), http.MethodPost,
			"/api/v1/sites/"+id+"/shares", map[string]any{"user_id": uuid.New(), "permission": "readonly"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/api/ -run 'TestSite' -v`
Expected: FAIL, `undefined: RegisterSiteRoutes`

- [ ] **Step 3: Write the handlers**

`backend/internal/api/site_handler.go`:

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

// siteStore is what the site handlers need from SiteService. An interface so
// the handlers are tested without a database.
type siteStore interface {
	List(ctx context.Context, userID uuid.UUID, isAdmin bool) ([]models.Site, error)
	Get(ctx context.Context, id uuid.UUID) (*models.Site, error)
	SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error)
	Create(ctx context.Context, in models.SiteInput, createdBy uuid.UUID) (*models.Site, error)
	Update(ctx context.Context, id uuid.UUID, in models.SiteInput) (*models.Site, *models.Site, error)
	Delete(ctx context.Context, id uuid.UUID) (*models.Site, error)
	ListShares(ctx context.Context, siteID uuid.UUID) ([]services.SiteShareView, error)
	UpsertShare(ctx context.Context, siteID, userID, sharedBy uuid.UUID, permission string) (*models.SiteSharing, error)
	RemoveShare(ctx context.Context, siteID, userID uuid.UUID) error
}

// siteWithAccess is a site plus what the caller may do with it, so the page
// knows which controls to show without a second request.
type siteWithAccess struct {
	models.Site
	Access string `json:"access"`
}

type shareSiteRequest struct {
	UserID     uuid.UUID `json:"user_id"`
	Permission string    `json:"permission"`
}

// RegisterSiteRoutes mounts /sites. Reading is open to any signed-in user and
// filtered by access; everything that changes a site or its sharing is
// admin-only.
func RegisterSiteRoutes(rg *gin.RouterGroup, sites siteStore, audit auditRecorder, users adminChecker) {
	g := rg.Group("/sites")
	g.GET("", listSitesHandler(sites))
	g.GET("/:id", getSiteHandler(sites))

	admin := g.Group("", RequireAdmin(users))
	admin.POST("", createSiteHandler(sites, audit))
	admin.PUT("/:id", updateSiteHandler(sites, audit))
	admin.DELETE("/:id", deleteSiteHandler(sites, audit))
	admin.GET("/:id/shares", listSiteSharesHandler(sites))
	admin.POST("/:id/shares", shareSiteHandler(sites, audit))
	admin.DELETE("/:id/shares/:user_id", unshareSiteHandler(sites, audit))
}

// parseSiteID reads :id, answering 400 for anything that is not a UUID.
func parseSiteID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid site id")
		return uuid.Nil, false
	}
	return id, true
}

// respondSiteError maps SiteService errors to responses.
func respondSiteError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrSiteNotFound):
		respondError(c, http.StatusNotFound, "site not found")
	case errors.Is(err, services.ErrSiteNameTaken), errors.Is(err, services.ErrSiteNotEmpty):
		respondError(c, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrSiteShareNotFound):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrSiteShareUnknownUser):
		respondError(c, http.StatusBadRequest, err.Error())
	default:
		respondInternal(c, op, err)
	}
}

func siteSummary(s *models.Site) map[string]any {
	return map[string]any{"name": s.Name, "description": s.Description, "address": s.Address}
}

func listSitesHandler(sites siteStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, isAdmin, _ := GetUserFromContext(c)
		list, err := sites.List(c.Request.Context(), userID, isAdmin)
		if err != nil {
			respondSiteError(c, "listSites", err)
			return
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func getSiteHandler(sites siteStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		userID, _, isAdmin, _ := GetUserFromContext(c)
		level, err := sites.SiteAccess(c.Request.Context(), userID, isAdmin, id)
		if err != nil {
			respondSiteError(c, "getSite", err)
			return
		}
		// "Not yours" answers exactly like "does not exist", so a site's
		// existence is never confirmed to someone without access.
		if level == services.SiteAccessNone {
			respondError(c, http.StatusNotFound, "site not found")
			return
		}
		site, err := sites.Get(c.Request.Context(), id)
		if err != nil {
			respondSiteError(c, "getSite", err)
			return
		}
		respondSuccess(c, http.StatusOK, siteWithAccess{Site: *site, Access: level.String()})
	}
}

// bindSiteInput reads and normalizes a create/update body, answering 400 on
// any problem.
func bindSiteInput(c *gin.Context) (models.SiteInput, bool) {
	var raw models.SiteInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return models.SiteInput{}, false
	}
	in, err := models.NormalizeSiteInput(raw)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return models.SiteInput{}, false
	}
	return in, true
}

func createSiteHandler(sites siteStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		in, ok := bindSiteInput(c)
		if !ok {
			return
		}
		userID, _, _, _ := GetUserFromContext(c)
		site, err := sites.Create(c.Request.Context(), in, userID)
		if err != nil {
			respondSiteError(c, "createSite", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteCreated, models.ResourceSite,
			&site.ID, models.AuditChanges{Summary: siteSummary(site)})
		respondSuccess(c, http.StatusCreated, site)
	}
}

func updateSiteHandler(sites siteStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		in, ok := bindSiteInput(c)
		if !ok {
			return
		}
		before, after, err := sites.Update(c.Request.Context(), id, in)
		if err != nil {
			respondSiteError(c, "updateSite", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteUpdated, models.ResourceSite,
			&id, models.AuditChanges{Before: siteSummary(before), After: siteSummary(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteSiteHandler(sites siteStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		site, err := sites.Delete(c.Request.Context(), id)
		if err != nil {
			respondSiteError(c, "deleteSite", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteDeleted, models.ResourceSite,
			&id, models.AuditChanges{Summary: siteSummary(site)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func listSiteSharesHandler(sites siteStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		shares, err := sites.ListShares(c.Request.Context(), id)
		if err != nil {
			respondSiteError(c, "listSiteShares", err)
			return
		}
		if shares == nil {
			shares = []services.SiteShareView{}
		}
		respondSuccess(c, http.StatusOK, shares)
	}
}

func shareSiteHandler(sites siteStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		var req shareSiteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.UserID == uuid.Nil {
			respondError(c, http.StatusBadRequest, "user_id is required")
			return
		}
		if req.Permission == "" {
			req.Permission = models.PermissionReadonly
		}
		if err := models.ValidateSharePermission(req.Permission); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		sharedBy, _, _, _ := GetUserFromContext(c)
		share, err := sites.UpsertShare(c.Request.Context(), id, req.UserID, sharedBy, req.Permission)
		if err != nil {
			respondSiteError(c, "shareSite", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteShared, models.ResourceSite,
			&id, models.AuditChanges{Summary: map[string]any{"user_id": req.UserID, "permission": req.Permission}})
		respondSuccess(c, http.StatusOK, share)
	}
}

func unshareSiteHandler(sites siteStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		userID, err := uuid.Parse(c.Param("user_id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid user_id: must be a UUID")
			return
		}
		if err := sites.RemoveShare(c.Request.Context(), id, userID); err != nil {
			respondSiteError(c, "unshareSite", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteUnshared, models.ResourceSite,
			&id, models.AuditChanges{Summary: map[string]any{"user_id": userID}})
		respondSuccess(c, http.StatusOK, gin.H{"removed": true})
	}
}
```

- [ ] **Step 4: Run the handler tests**

Run: `cd /home/sysadmin/sentinel/backend && go test ./internal/api/ -run 'TestSite' -v`
Expected: PASS. If gin reports a route conflict between `GET /sites/:id` and the admin group's routes, it is a real bug in the registration; do not work around it by renaming paths. Both groups share the `/sites` prefix, so the combined tree must be conflict-free.

- [ ] **Step 5: Wire into main.go**

In `backend/cmd/sentinel/main.go`, after `auditService := services.NewAuditService(db)` (line ~130), add:

```go
	siteService := services.NewSiteService(db)
```

and after `api.RegisterBackupRoutes(v1, backupService, auditService, authService)` (line ~317), add:

```go
	api.RegisterSiteRoutes(v1, siteService, auditService, authService)
```

- [ ] **Step 6: Build and run everything**

Run: `cd /home/sysadmin/sentinel/backend && go build ./... && go vet ./... && go test ./internal/...`
Expected: all `ok`.

- [ ] **Step 7: Commit**

```bash
cd /home/sysadmin/sentinel
git add backend/internal/api/site_handler.go backend/internal/api/site_handler_test.go backend/cmd/sentinel/main.go
git commit -m "feat(sites): site API, admin-managed, 404 for sites you cannot see

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Frontend site hooks, network nav, and routes

**Files:**
- Create: `frontend/src/hooks/useSites.ts`
- Modify: `frontend/src/components/Layout.tsx`
- Modify: `frontend/src/App.tsx`
- Create: `frontend/src/pages/network/Sites.tsx` (placeholder completed in Task 7)
- Create: `frontend/src/pages/network/SiteDetail.tsx` (placeholder completed in Task 7)

**Interfaces:**
- Consumes: the API from Task 5.
- Produces (`@/hooks/useSites`):
  - `type SitePermission = 'readonly' | 'editable'`
  - `type SiteAccess = 'none' | 'readonly' | 'editable' | 'admin'`
  - `interface Site { id: string; name: string; description: string | null; address: string | null; created_by: string | null; created_at: string; updated_at: string }`
  - `interface SiteDetail extends Site { access: SiteAccess }`
  - `interface SiteInput { name: string; description: string | null; address: string | null }`
  - `interface SiteShare { id: string; site_id: string; shared_with_user_id: string; username: string; email: string; permission: SitePermission; created_at: string }`
  - `useSites(): { sites: Site[]; loading: boolean; error: string | null; refetch: () => Promise<void> }`
  - `useSite(id: string | undefined): { site: SiteDetail | null; loading: boolean; notFound: boolean; refetch: () => Promise<void> }`
  - `useSiteActions(): { create(input: SiteInput): Promise<Site>; update(id: string, input: SiteInput): Promise<Site>; remove(id: string): Promise<void>; busy: boolean }`
  - `useSiteShares(siteId: string | undefined, enabled: boolean): { shares: SiteShare[]; loading: boolean; refetch: () => Promise<void>; share(userId: string, p: SitePermission): Promise<void>; unshare(userId: string): Promise<void> }`
  - `useSiteSummary(): { value: string; subtitle: string }`

- [ ] **Step 1: Write the hooks**

`frontend/src/hooks/useSites.ts`:

```ts
import { useCallback, useEffect, useMemo, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'

export type SitePermission = 'readonly' | 'editable'
export type SiteAccess = 'none' | 'readonly' | 'editable' | 'admin'

export interface Site {
  id: string
  name: string
  description: string | null
  address: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

/** GET /sites/:id adds what the caller may do, so the page knows which
 *  controls to show. */
export interface SiteDetail extends Site {
  access: SiteAccess
}

export interface SiteInput {
  name: string
  description: string | null
  address: string | null
}

export interface SiteShare {
  id: string
  site_id: string
  shared_with_user_id: string
  username: string
  email: string
  permission: SitePermission
  created_at: string
}

/** Sites the signed-in user can see: all of them for an admin. */
export function useSites() {
  const [sites, setSites] = useState<Site[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<Site[]>>('/sites')
      setSites(data.data ?? [])
    } catch (err) {
      setError((err as ApiError).message || 'Failed to load sites')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { sites, loading, error, refetch }
}

/** One site. notFound covers both "missing" and "not shared with you": the
 *  API answers 404 for both on purpose. */
export function useSite(id: string | undefined) {
  const [site, setSite] = useState<SiteDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)

  const refetch = useCallback(async () => {
    if (!id) return
    try {
      const { data } = await api.get<ApiResponse<SiteDetail>>(`/sites/${id}`)
      setSite(data.data)
      setNotFound(false)
    } catch (err) {
      const e = err as ApiError
      if (e.status === 404 || e.status === 400) setNotFound(true)
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    void refetch()
  }, [refetch])

  return { site, loading, notFound, refetch }
}

/** Create, edit and delete (admin only; the API enforces it). */
export function useSiteActions() {
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
    (input: SiteInput) =>
      run(async () => (await api.post<ApiResponse<Site>>('/sites', input)).data.data),
    [run]
  )
  const update = useCallback(
    (id: string, input: SiteInput) =>
      run(async () => (await api.put<ApiResponse<Site>>(`/sites/${id}`, input)).data.data),
    [run]
  )
  const remove = useCallback(
    (id: string) =>
      run(async () => {
        await api.delete(`/sites/${id}`)
      }),
    [run]
  )

  return { create, update, remove, busy }
}

/** Who a site is shared with (admin only). enabled=false skips the request
 *  for non-admins, who would only get a 403. */
export function useSiteShares(siteId: string | undefined, enabled: boolean) {
  const [shares, setShares] = useState<SiteShare[]>([])
  const [loading, setLoading] = useState(false)

  const refetch = useCallback(async () => {
    if (!siteId || !enabled) return
    setLoading(true)
    try {
      const { data } = await api.get<ApiResponse<SiteShare[]>>(`/sites/${siteId}/shares`)
      setShares(data.data ?? [])
    } finally {
      setLoading(false)
    }
  }, [siteId, enabled])

  useEffect(() => {
    void refetch()
  }, [refetch])

  const share = useCallback(
    async (userId: string, permission: SitePermission) => {
      await api.post(`/sites/${siteId}/shares`, { user_id: userId, permission })
      await refetch()
    },
    [siteId, refetch]
  )
  const unshare = useCallback(
    async (userId: string) => {
      await api.delete(`/sites/${siteId}/shares/${userId}`)
      await refetch()
    },
    [siteId, refetch]
  )

  return { shares, loading, refetch, share, unshare }
}

/** The Overview card: how many sites the user can see. */
export function useSiteSummary(): { value: string; subtitle: string } {
  const { sites, loading, error } = useSites()
  return useMemo(() => {
    if (loading) return { value: '—', subtitle: 'loading' }
    if (error) return { value: '—', subtitle: 'unavailable' }
    // A bare zero says nothing about why; the card says what to do instead.
    if (sites.length === 0) return { value: '0', subtitle: 'add a site' }
    return { value: String(sites.length), subtitle: sites.length === 1 ? 'site' : 'sites' }
  }, [sites, loading, error])
}
```

- [ ] **Step 2: Switch the sidebar by route**

In `frontend/src/components/Layout.tsx`:

1. Change the router import to include `useLocation`:

```ts
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
```

2. Replace the `nav` array with:

```ts
// Order matters: the four things Sentinel watches sit together, then what it
// publishes, then what it reports after the fact. Incidents was splitting the
// monitoring group in half.
const nav = [
  { to: '/', label: 'Overview', end: true },
  // What is being monitored.
  { to: '/uptime', label: 'Uptime Monitoring' },
  { to: '/ssl', label: 'SSL & Domains' },
  { to: '/servers', label: 'Server Monitoring' },
  { to: '/network', label: 'Network Monitoring' },
  // What comes out of it.
  { to: '/status-pages', label: 'Status Pages' },
  { to: '/incidents', label: 'Incidents' },
  { to: '/reports', label: 'Reports' },
]

// Network Monitoring has its own nav: it will hold sites, devices, maps,
// dashboards, MIBs and credentials, far more than fits in the main list. Each
// roadmap phase adds its own entries here, so there is never a dead link.
// adminOnly entries are hidden from members, as Users is.
const networkNav: { to: string; label: string; end?: boolean; adminOnly?: boolean }[] = [
  { to: '/network/sites', label: 'Sites' },
]

/** Whether a path belongs to the Network Monitoring section. */
function inNetworkSection(pathname: string): boolean {
  return pathname === '/network' || pathname.startsWith('/network/')
}
```

3. In `SidebarBody`, after `const role = ...`, add:

```ts
  const { pathname } = useLocation()
  const network = inNetworkSection(pathname)
```

4. Replace the wordmark subtitle line:

```tsx
        <div className="mt-2 text-xs text-slate-400">Uptime Monitor</div>
```

with:

```tsx
        <div className="mt-2 text-xs text-slate-400">{network ? 'Network Monitoring' : 'Uptime Monitor'}</div>
```

5. Replace the `<nav>` block with:

```tsx
      {/* Nav */}
      <nav className="flex-1 space-y-1 overflow-y-auto p-4">
        {network ? (
          <>
            {/* The way back out. A plain button rather than a NavLink so it is
                never shown as the active page. */}
            <button className="rd-nav w-full text-left" onClick={() => go('/')}>
              ← Sentinel
            </button>
            <div className="my-2 border-t border-white/10" />
            {networkNav
              .filter((item) => !item.adminOnly || currentUser?.is_admin)
              .map((item) => (
                <NavLink key={item.to} to={item.to} end={item.end} onClick={onNavigate} className={navClass}>
                  {item.label}
                </NavLink>
              ))}
          </>
        ) : (
          nav.map((item) => (
            <NavLink key={item.to} to={item.to} end={item.end} onClick={onNavigate} className={navClass}>
              {item.label}
            </NavLink>
          ))
        )}
      </nav>
```

`.rd-nav` is a plain class selector (`frontend/src/index.css:132`), so it styles the `<button>` the same as the links.

- [ ] **Step 3: Placeholder pages so routes compile**

`frontend/src/pages/network/Sites.tsx`:

```tsx
export default function Sites() {
  return <h1 className="text-4xl font-light text-white">Sites</h1>
}
```

`frontend/src/pages/network/SiteDetail.tsx`:

```tsx
export default function SiteDetail() {
  return <h1 className="text-4xl font-light text-white">Site</h1>
}
```

- [ ] **Step 4: Add routes**

In `frontend/src/App.tsx`, after `const ServerDetail = lazy(...)`, add:

```ts
const Sites = lazy(() => import('@/pages/network/Sites'))
const SiteDetail = lazy(() => import('@/pages/network/SiteDetail'))
```

After the `/server-monitoring` redirect route, add:

```tsx
              {/* Network Monitoring. /network has no page of its own until a
                  network overview exists, so it opens the site list. */}
              <Route path="/network" element={<Navigate to="/network/sites" replace />} />
              <Route path="/network/sites" element={<Sites />} />
              <Route path="/network/sites/:id" element={<SiteDetail />} />
```

- [ ] **Step 5: Typecheck and lint**

Run: `docker run --rm -v /home/sysadmin/sentinel/frontend:/app -w /app node:20-alpine sh -c "npx tsc --noEmit && npx eslint src"`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
cd /home/sysadmin/sentinel
git add frontend/src/hooks/useSites.ts frontend/src/components/Layout.tsx frontend/src/App.tsx frontend/src/pages/network/
git commit -m "feat(network): Network Monitoring section with its own sidebar

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Sites list, site detail, form and sharing panel

**Files:**
- Create: `frontend/src/components/SiteFormModal.tsx`
- Create: `frontend/src/components/SiteSharingPanel.tsx`
- Modify: `frontend/src/pages/network/Sites.tsx` (replace placeholder)
- Modify: `frontend/src/pages/network/SiteDetail.tsx` (replace placeholder)

**Interfaces:**
- Consumes: `useSites`, `useSite`, `useSiteActions`, `useSiteShares`, `Site`, `SiteInput`, `SitePermission` (Task 6); `useUsers` (`@/hooks/useUsers`, returns `{ users: {id, username, email}[] }`); `useAuthContext` (`currentUser.is_admin`, `currentUser.user_id`). Errors are shown inline, as `BackupRestore.tsx` does, so no toast hook is needed.
- Produces: `SiteFormModal` props `{ initial?: Site; onClose(): void; onSaved(site: Site): void }`; `SiteSharingPanel` props `{ siteId: string }`.

- [ ] **Step 1: Site form modal**

`frontend/src/components/SiteFormModal.tsx`:

```tsx
import { useState } from 'react'
import { X } from 'lucide-react'
import { useSiteActions, type Site } from '@/hooks/useSites'
import type { ApiError } from '@/services/api'

const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

interface Props {
  /** The site being edited; omitted when creating. */
  initial?: Site
  onClose: () => void
  onSaved: (site: Site) => void
}

export default function SiteFormModal({ initial, onClose, onSaved }: Props) {
  const { create, update, busy } = useSiteActions()
  const [name, setName] = useState(initial?.name ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [address, setAddress] = useState(initial?.address ?? '')
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    // Blank optional fields are sent as null; the server trims and does the same.
    const input = {
      name,
      description: description.trim() || null,
      address: address.trim() || null,
    }
    try {
      const saved = initial ? await update(initial.id, input) : await create(input)
      onSaved(saved)
    } catch (err) {
      setError((err as ApiError).message || 'Failed to save site')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form
        className="card w-full max-w-md space-y-4 p-6"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void submit(e)}
      >
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">{initial ? 'Edit site' : 'Add site'}</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>

        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Name</span>
          <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} maxLength={255} required autoFocus />
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Description <span className="text-slate-500">(optional)</span></span>
          <textarea className={inputCls} rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Address <span className="text-slate-500">(optional)</span></span>
          <input className={inputCls} value={address} onChange={(e) => setAddress(e.target.value)} />
        </label>

        {error && (
          <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>
        )}

        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || !name.trim()}>
            {busy ? 'Saving…' : initial ? 'Save' : 'Add site'}
          </button>
        </div>
      </form>
    </div>
  )
}
```

- [ ] **Step 2: Sharing panel**

`frontend/src/components/SiteSharingPanel.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { Loader2, Share2, Trash2 } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useUsers } from '@/hooks/useUsers'
import { useSiteShares, type SitePermission } from '@/hooks/useSites'
import type { ApiError } from '@/services/api'

const selectCls =
  'rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-primary-500'

function permLabel(p: SitePermission) {
  return p === 'editable' ? 'Can edit' : 'Read-only'
}

/** Admin-only: who else can see this site. Inline on the site page rather than
 *  a modal, since sharing is part of what a site is. */
export default function SiteSharingPanel({ siteId }: { siteId: string }) {
  const { currentUser } = useAuthContext()
  const { users } = useUsers()
  const { shares, loading, share, unshare } = useSiteShares(siteId, true)
  const [userId, setUserId] = useState('')
  const [permission, setPermission] = useState<SitePermission>('readonly')
  const [busyUser, setBusyUser] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Not yourself (an admin already sees every site) and not anyone already listed.
  const available = useMemo(() => {
    const shared = new Set(shares.map((s) => s.shared_with_user_id))
    return users.filter((u) => u.id !== currentUser?.user_id && !shared.has(u.id))
  }, [users, shares, currentUser])

  const attempt = async (who: string, fn: () => Promise<void>) => {
    setBusyUser(who)
    setError(null)
    try {
      await fn()
    } catch (err) {
      setError((err as ApiError).message || 'Something went wrong')
    } finally {
      setBusyUser(null)
    }
  }

  return (
    <div className="card space-y-4 p-6">
      <div>
        <h2 className="flex items-center gap-2 text-lg font-semibold">
          <Share2 className="h-5 w-5 text-primary-400" /> Sharing
        </h2>
        <p className="text-sm text-slate-400">
          Admins see every site. Share it with anyone else who needs it. Read-only lets them view it; can edit also lets
          them change what is in it, but not rename, delete or reshare it.
        </p>
      </div>

      <div className="flex flex-wrap gap-2">
        <select className={`${selectCls} min-w-[12rem] flex-1`} value={userId} onChange={(e) => setUserId(e.target.value)}>
          <option value="">Select a user…</option>
          {available.map((u) => (
            <option key={u.id} value={u.id}>
              {u.username}
              {u.email ? ` (${u.email})` : ''}
            </option>
          ))}
        </select>
        <select className={selectCls} value={permission} onChange={(e) => setPermission(e.target.value as SitePermission)}>
          <option value="readonly">{permLabel('readonly')}</option>
          <option value="editable">{permLabel('editable')}</option>
        </select>
        <button
          className="btn-primary"
          disabled={!userId || busyUser !== null}
          onClick={() =>
            void attempt(userId, async () => {
              await share(userId, permission)
              setUserId('')
              setPermission('readonly')
            })
          }
        >
          Share
        </button>
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      {loading ? (
        <div className="flex justify-center py-4 text-slate-400">
          <Loader2 className="h-5 w-5 animate-spin" />
        </div>
      ) : shares.length === 0 ? (
        <p className="text-sm text-slate-400">Not shared with anyone yet.</p>
      ) : (
        <div className="divide-y divide-white/10">
          {shares.map((s) => (
            <div key={s.shared_with_user_id} className="flex flex-wrap items-center gap-3 py-3">
              <div className="min-w-0 flex-1">
                <div className="truncate font-medium text-white">{s.username}</div>
                {s.email && <div className="truncate text-xs text-slate-400">{s.email}</div>}
              </div>
              <select
                className={selectCls}
                value={s.permission}
                disabled={busyUser === s.shared_with_user_id}
                aria-label={`Permission for ${s.username}`}
                onChange={(e) =>
                  void attempt(s.shared_with_user_id, () => share(s.shared_with_user_id, e.target.value as SitePermission))
                }
              >
                <option value="readonly">{permLabel('readonly')}</option>
                <option value="editable">{permLabel('editable')}</option>
              </select>
              <button
                className="btn-secondary !px-2 !py-1 text-red-400"
                title={`Remove ${s.username}`}
                disabled={busyUser === s.shared_with_user_id}
                onClick={() => void attempt(s.shared_with_user_id, () => unshare(s.shared_with_user_id))}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 3: Sites list page**

Replace `frontend/src/pages/network/Sites.tsx` with:

```tsx
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { MapPin, Plus } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useSites } from '@/hooks/useSites'
import SiteFormModal from '@/components/SiteFormModal'

export default function Sites() {
  const navigate = useNavigate()
  const { currentUser } = useAuthContext()
  const isAdmin = !!currentUser?.is_admin
  const { sites, loading, error, refetch } = useSites()
  const [adding, setAdding] = useState(false)

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-4xl font-light text-white">Sites</h1>
          <p className="mt-2 text-sm text-slate-400">
            Each site groups the network devices, maps and dashboards for one place.
          </p>
        </div>
        {isAdmin && (
          <button className="btn-primary flex items-center gap-2" onClick={() => setAdding(true)}>
            <Plus className="h-4 w-4" /> Add site
          </button>
        )}
      </div>

      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}

      {loading ? (
        <p className="text-sm text-slate-400">Loading sites…</p>
      ) : sites.length === 0 ? (
        <div className="card p-8 text-center">
          <p className="text-slate-300">No sites yet.</p>
          <p className="mt-1 text-sm text-slate-500">
            {isAdmin ? 'Add a site to start organising network monitoring.' : 'No sites have been shared with you.'}
          </p>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {sites.map((s) => (
            <button
              key={s.id}
              className="card p-6 text-left transition hover:border-white/20"
              onClick={() => navigate(`/network/sites/${s.id}`)}
            >
              <div className="truncate text-lg font-medium text-white" title={s.name}>
                {s.name}
              </div>
              {s.description && <p className="mt-1 line-clamp-2 text-sm text-slate-400">{s.description}</p>}
              {s.address && (
                <p className="mt-3 flex items-center gap-1.5 truncate text-xs text-slate-500">
                  <MapPin className="h-3.5 w-3.5 shrink-0" /> {s.address}
                </p>
              )}
            </button>
          ))}
        </div>
      )}

      {adding && (
        <SiteFormModal
          onClose={() => setAdding(false)}
          onSaved={(site) => {
            setAdding(false)
            void refetch()
            navigate(`/network/sites/${site.id}`)
          }}
        />
      )}
    </div>
  )
}
```

- [ ] **Step 4: Site detail page**

Replace `frontend/src/pages/network/SiteDetail.tsx` with:

```tsx
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { MapPin, Pencil, Trash2 } from 'lucide-react'
import { useSite, useSiteActions } from '@/hooks/useSites'
import SiteFormModal from '@/components/SiteFormModal'
import SiteSharingPanel from '@/components/SiteSharingPanel'
import type { ApiError } from '@/services/api'

export default function SiteDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { site, loading, notFound, refetch } = useSite(id)
  const { remove, busy } = useSiteActions()
  const [editing, setEditing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (loading) return <p className="text-sm text-slate-400">Loading…</p>

  // Missing and not-shared look the same on purpose; the API does not say which.
  if (notFound || !site) {
    return (
      <div className="card p-8 text-center">
        <p className="text-slate-300">Site not found.</p>
        <Link to="/network/sites" className="mt-2 inline-block text-sm text-primary-400 hover:underline">
          Back to sites
        </Link>
      </div>
    )
  }

  const isAdmin = site.access === 'admin'

  const handleDelete = async () => {
    setError(null)
    try {
      await remove(site.id)
      navigate('/network/sites')
    } catch (err) {
      setError((err as ApiError).message || 'Failed to delete site')
      setConfirmDelete(false)
    }
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <Link to="/network/sites" className="text-sm text-slate-400 hover:text-slate-300">
            ← Sites
          </Link>
          <h1 className="mt-2 break-words text-4xl font-light text-white">{site.name}</h1>
          {site.description && <p className="mt-2 text-slate-400">{site.description}</p>}
          {site.address && (
            <p className="mt-2 flex items-center gap-1.5 text-sm text-slate-500">
              <MapPin className="h-4 w-4 shrink-0" /> {site.address}
            </p>
          )}
        </div>
        {isAdmin && (
          <div className="flex gap-2">
            <button className="btn-secondary flex items-center gap-2" onClick={() => setEditing(true)}>
              <Pencil className="h-4 w-4" /> Edit
            </button>
            {confirmDelete ? (
              <>
                <button className="btn-secondary" onClick={() => setConfirmDelete(false)}>
                  Cancel
                </button>
                <button className="btn bg-red-600 text-white hover:bg-red-700" disabled={busy} onClick={() => void handleDelete()}>
                  Delete site
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

      <div className="card p-8 text-center">
        <p className="text-slate-300">No devices yet.</p>
        <p className="mt-1 text-sm text-slate-500">SNMP devices arrive in a later update.</p>
      </div>

      {isAdmin && <SiteSharingPanel siteId={site.id} />}

      {editing && (
        <SiteFormModal
          initial={site}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false)
            void refetch()
          }}
        />
      )}
    </div>
  )
}
```

- [ ] **Step 5: Typecheck and lint**

Run: `docker run --rm -v /home/sysadmin/sentinel/frontend:/app -w /app node:20-alpine sh -c "npx tsc --noEmit && npx eslint src"`
Expected: no errors. If `btn` (used for the red delete button) is not a defined class, check `grep -n '\.btn\b\|\.btn ' frontend/src/index.css`; `ShareModal.tsx` uses `className="btn bg-error-600 ..."`, so it should exist.

- [ ] **Step 6: Commit**

```bash
cd /home/sysadmin/sentinel
git add frontend/src/components/SiteFormModal.tsx frontend/src/components/SiteSharingPanel.tsx frontend/src/pages/network/
git commit -m "feat(network): site list and detail pages with admin sharing

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Overview card

**Files:**
- Modify: `frontend/src/components/ShimmerStatCard.tsx` (add `network` colour)
- Modify: `frontend/src/pages/Overview.tsx`

**Interfaces:**
- Consumes: `useSiteSummary()` (Task 6).

- [ ] **Step 1: Add the colour**

In `frontend/src/components/ShimmerStatCard.tsx`, extend the `colorType` union:

```ts
  colorType: 'monitoring' | 'responseTime' | 'incidents' | 'agents' | 'ssl' | 'statusPages' | 'reports' | 'network'
```

and add to `colorMap` (indigo is unused by any other card):

```ts
  network: {
    hoverBorder: 'hover:border-indigo-500/50',
    bg: 'from-indigo-600/15',
    text: 'text-indigo-400',
    subtle: 'text-indigo-400/70',
    border: 'border-indigo-500/30',
    glow: 'bg-indigo-500/10',
    glowHover: 'group-hover:bg-indigo-500/20',
  },
```

Match the exact set of keys the other entries in `colorMap` have. If they have more keys than the seven above, copy an existing entry (for example `statusPages`) and change every `cyan` to `indigo`.

- [ ] **Step 2: Add the card**

In `frontend/src/pages/Overview.tsx`:

1. Import: `import { useSiteSummary } from '@/hooks/useSites'`
2. In the component, after `const sslSummary = useSSLSummary()`: `const siteSummary = useSiteSummary()`
3. Add `'network'` to the `useCardShimmer([...])` list, after `'agents'`.
4. In `sectionCards`, after the `agents` entry, add:

```ts
      {
        key: 'network',
        title: 'Network Monitoring',
        to: '/network',
        colorType: 'network' as const,
        value: siteSummary.value,
        subtitle: siteSummary.subtitle,
      },
```

5. Add `siteSummary` to the `useMemo` dependency array.
6. Seven cards no longer fit `xl:grid-cols-6` (one would sit alone on a second row). Replace the grid classes:

```tsx
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4 2xl:grid-cols-7">
```

- [ ] **Step 3: Typecheck and lint**

Run: `docker run --rm -v /home/sysadmin/sentinel/frontend:/app -w /app node:20-alpine sh -c "npx tsc --noEmit && npx eslint src"`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
cd /home/sysadmin/sentinel
git add frontend/src/components/ShimmerStatCard.tsx frontend/src/pages/Overview.tsx
git commit -m "feat(overview): Network Monitoring card, in sidebar order

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Sandbox verification

No code unless a step fails. If one does, fix it in the task that owns the code, commit, and re-run from step 1.

**Files:** none in the repo. The sandbox is `/srv/docker/sentinel-dev`, a separate clone on `dev`.

- [ ] **Step 1: Point the sandbox at the feature branch**

```bash
cd /home/sysadmin/sentinel && git push -u origin feature/network-monitoring
cd /srv/docker/sentinel-dev && git fetch origin && git switch -c feature/network-monitoring --track origin/feature/network-monitoring
```

Pushing is outward-facing: confirm with the user before running the `git push`. If they would rather not push, use `git -C /srv/docker/sentinel-dev fetch /home/sysadmin/sentinel feature/network-monitoring:feature/network-monitoring && git -C /srv/docker/sentinel-dev switch feature/network-monitoring`.

- [ ] **Step 2: Take a pre-switch backup (for checklist step 5)**

```bash
curl -fsS -c /tmp/claude-sbx-cookie -H 'Content-Type: application/json' \
  -d '{"username":"<sandbox admin>","password":"<password>"}' http://localhost:3201/api/v1/auth/login
```

Logging in needs the sandbox admin's credentials; ask the user, or ask them to click **Create backup** in Settings → Backups at http://localhost:3200. Record the backup id.

- [ ] **Step 3: Switch and rebuild**

```bash
cd /srv/docker/sentinel-dev && docker compose up -d --build && docker compose ps
```

Expected: all services healthy.

- [ ] **Step 4: Checklist 1, database**

```bash
cd /srv/docker/sentinel-dev
docker compose exec -T postgres psql -U sentinel -d sentinel -Atc \
  "select extname, extversion from pg_extension where extname='timescaledb';
   show timescaledb.telemetry_level;
   select filename from schema_migrations where filename in ('044_timescaledb.sql','045_sites.sql') order by 1;
   select nspname from pg_namespace where nspname='metrics';"
docker compose logs backend --since 5m | grep -iE 'migration|error|panic'
```

Expected: `timescaledb|2.30.1`, `off`, both migration files, `metrics`; logs show 044 and 045 applied and no errors.

- [ ] **Step 5: Checklist 2 and 3, UI and access (with the user)**

Ask the user to check in the browser at http://localhost:3200:
- every main page loads; Overview shows the Network Monitoring card in position 5; at wide, laptop and phone widths the grid has no lone orphan card;
- the sidebar switches inside `/network`, **← Sentinel** returns to Overview, and the mobile drawer shows the same;
- as admin: create a site, try " site one " then "SITE ONE" (the second must be refused), edit it, share it read-only with a second non-admin user;
- as that user: only the shared site is listed; no Add/Edit/Delete/Sharing; an unshared site's URL shows "Site not found".

Then verify the API side directly as the non-admin (cookie from their login):

```bash
curl -s -b <member-cookie> -X DELETE http://localhost:3201/api/v1/sites/<shared-site-id> ; echo
curl -s -b <member-cookie> http://localhost:3201/api/v1/sites/<unshared-site-id> ; echo
```

Expected: 403 body for the delete (and the site still exists), 404 body for the unshared site.

- [ ] **Step 6: Checklist 4, backup round trip**

```bash
cd /srv/docker/sentinel-dev
docker compose exec -T postgres psql -U sentinel -d sentinel -c \
  "CREATE TABLE metrics.phase0_probe (t TIMESTAMPTZ NOT NULL, v DOUBLE PRECISION);
   SELECT create_hypertable('metrics.phase0_probe', 't');
   INSERT INTO metrics.phase0_probe VALUES (now(), 42);"
```

Create a backup in the UI, then inspect it:

```bash
docker compose exec -T backend sh -c 'ls -t /var/lib/sentinel/backups | head -1'
docker compose exec -T backend sh -c 'zcat /var/lib/sentinel/backups/<file> | grep -ciE "timescaledb|EXTENSION|_timescaledb|metrics\." ; zcat /var/lib/sentinel/backups/<file> | grep -nE "SCHEMA public|pgcrypto" | head'
```

Expected: the count is `0`. Note how the dump handles `SCHEMA public` (open question from the spec) and whether any `pgcrypto` reference appears.

Restore that backup in the UI, then:

```bash
docker compose exec -T postgres psql -U sentinel -d sentinel -Atc \
  "select count(*) from metrics.phase0_probe; select extname from pg_extension where extname='timescaledb'; select count(*) from sites;"
```

Expected: `1`, `timescaledb`, and the number of sites created in step 5. The app still loads and login works. If the restore failed on `SCHEMA public`, stop and report it: the fix changes Task 2 and needs the user's decision.

`pgcrypto`: Sentinel uses `gen_random_uuid()`, which is built into Postgres 13+, so nothing should depend on the extension being in the dump. Confirm with `grep -rn 'crypt(\|digest(\|pgp_' backend/migrations backend/internal | grep -v _test | head`. Expected: no SQL use of pgcrypto functions.

- [ ] **Step 7: Checklist 5, pre-switch backup**

Restore the backup from step 2 in the UI, then restart the backend (`docker compose restart backend`) and check:

```bash
docker compose logs backend --since 2m | grep -iE 'applying migration|error'
docker compose exec -T postgres psql -U sentinel -d sentinel -Atc "select count(*) from metrics.phase0_probe"
```

Expected: 044 and 045 applied again with no error; the probe hypertable still has its row; the app works (sites are gone, as they post-date that backup).

- [ ] **Step 8: Checklist 6, rollback drill**

```bash
cd /srv/docker/sentinel-dev
docker compose exec -T postgres psql -U sentinel -d sentinel -c "DROP TABLE metrics.phase0_probe; DROP EXTENSION timescaledb;"
docker compose stop backend
```

Temporarily point the sandbox at plain Postgres without editing the tracked file: add to the gitignored `docker-compose.override.yml` under `services.postgres`:

```yaml
    image: postgres:16-alpine
    command: ["postgres"]
```

```bash
docker compose up -d postgres && docker compose exec -T postgres psql -U sentinel -d sentinel -Atc "select count(*) from sites"
```

Expected: the database opens and answers. Then check checklist 7 on this same setup:

```bash
docker compose up -d backend; sleep 15; docker compose logs backend --since 1m | grep -i timescale
```

Expected: the backend refuses to start with "Sentinel now requires the TimescaleDB extension…".

Remove the two override lines, then `docker compose up -d` and confirm 044 re-applies and everything is healthy.

- [ ] **Step 9: Clean up and report**

```bash
docker compose -f /srv/docker/sentinel-dev/docker-compose.yml -f /srv/docker/sentinel-dev/docker-compose.override.yml ps
git -C /srv/docker/sentinel-dev status --short
```

Expected: all healthy; override file back to its original contents; no tracked changes in the sandbox checkout.

Report the checklist results to the user, including how `SCHEMA public` behaved. Merging to `dev` and switching live are the user's decisions; do not do either without being asked.
