# Network monitoring phase 0: TimescaleDB, Sites, and the Network section

Part of the network monitoring roadmap
(`2026-09-28-network-monitoring-roadmap.md`). Phase 0 builds nothing that
polls a device. It lays the ground every later phase stands on: the database
that will hold metrics, the backup behaviour that database requires, the
**Site** as the unit of organisation and sharing, and the **Network
Monitoring** section of the UI where all of it will live.

## Problem

- Later phases store long-term, full-resolution metrics for thousands of
  ports. Plain Postgres cannot hold that sensibly; the roadmap chose
  TimescaleDB (decision 4). Every install runs `postgres:16-alpine` today.
- Settings → Backups dumps the whole database as plain SQL
  (`pg_dump --clean --if-exists`, `backend/internal/services/backup_service.go:161`)
  and restores it by replaying it through `psql` over the running database
  (`:354`). That is not a supported way to back up or restore TimescaleDB
  hypertables, and a `--clean` dump of a database containing the extension
  would carry a `DROP EXTENSION timescaledb`, which on restore cascades to
  every hypertable.
- Nothing in Sentinel models a place. The network features are organised,
  shared and reported by site, so the site must exist before the devices
  that belong to it.
- The network features need more navigation (sites, devices, maps,
  dashboards, MIB library, credentials) than fits in the main sidebar.

## Scope

In:

- Switch the Postgres image to TimescaleDB (same Postgres 16) for every
  install, and enable the extension.
- Backups become **configuration only**: they exclude collected metrics.
- Sites, per-site sharing, and one access check that later phases reuse.
- The Network Monitoring section: its own sidebar nav, a Sites list and a
  site detail page, and an Overview card.

Out:

- Any SNMP, device, or metric table. The `metrics` schema is created empty.
- Assigning existing monitors or server agents to sites. Monitors keep
  `monitor_sharing` and groups untouched; linking them to sites can come
  later without redesign.
- Public read-only site pages (they arrive with maps, roadmap phase 6).
- Nav items for later phases. Each phase adds its own, so there are no dead
  links.

## Design

### 1. TimescaleDB

**Compose** (`docker-compose.yml`, the `postgres` service):

```yaml
  postgres:
    image: timescale/timescaledb:2.30.1-pg16
    command:
      - postgres
      - -c
      - shared_preload_libraries=timescaledb
      - -c
      - timescaledb.telemetry_level=off
```

- The tag is pinned to an exact TimescaleDB release (2.30.1, the newest
  pg16 build as of 2026-09-28), never `latest-pg16`, so a pull cannot change
  the database engine underneath an install. Upgrading it later is a
  deliberate change with its own `ALTER EXTENSION timescaledb UPDATE`.
- The image is Alpine-based Postgres 16, like `postgres:16-alpine`. The live
  database is PostgreSQL 16.14 on musl with UTF8 / `en_US.utf8`, so existing
  data directories open unchanged.
- `shared_preload_libraries` must be passed on the command line. The
  TimescaleDB image writes it into `postgresql.conf` only when it
  initialises a **new** data directory; every upgraded install has an
  existing one and would otherwise fail `CREATE EXTENSION`.
- Telemetry is off: Sentinel does not phone home, and neither should its
  database.
- Volume, credentials, port binding and healthcheck are unchanged.
  `pg_isready` works identically.

The backend image's `postgresql16-client` (`backend/Dockerfile:70`) stays;
the server is still Postgres 16.

**Migration `044_timescaledb.sql`:**

```sql
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE SCHEMA IF NOT EXISTS metrics;
```

No existing table changes. Idempotent, which matters for backups (section 2).

**Preflight.** `runMigrations` (`backend/cmd/sentinel/main.go:660`) applies
files with a bare `db.Exec`, so against plain Postgres 044 fails with a raw
SQL error. Before running migrations, the backend checks
`SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb'`. If the
check finds nothing, and 044 is not yet applied, startup fails with:

> Sentinel now requires the TimescaleDB extension. Update docker-compose.yml
> from the current release (the postgres service uses the
> timescale/timescaledb image) and run `docker compose up -d`. See
> GETTING_STARTED.md.

Failing is correct: roadmap phases 2 onward cannot work without the
extension. The message is a pure function of the check result, so it is unit
tested.

**Scripts and docs.** `install.sh` has no Postgres image reference and needs
no change. `GETTING_STARTED.md` and `README.md` gain a short upgrade note:
pull the new compose file, then `docker compose up -d`; the backend migrates
itself.

### 2. Backups: configuration only

**Dump scope.** `BackupService.Create` adds `--table=public.*` to its
`pg_dump` arguments. Sentinel's own tables all live in `public`, so the dump
still contains every monitor, site, credential, report, incident and
setting. It no longer contains:

- the `metrics` schema: collected metrics are not backed up (roadmap
  decision; they are protected at the volume level, see below);
- TimescaleDB's internal `_timescaledb_*` schemas;
- extensions. With `--table`, `pg_dump` does not emit `CREATE EXTENSION` or,
  under `--clean`, `DROP EXTENSION`, so a restore can never drop
  `timescaledb` and cascade into the hypertables.

Restore is unchanged: the safety backup, then a `psql` replay with
`ON_ERROR_STOP` over the running database.

**Rule for phase 2 onward: no foreign keys from `metrics` into `public`.** A
restore executes `DROP TABLE IF EXISTS devices` (and so on) before
recreating it. A hypertable holding a foreign key to `devices` would make
that drop fail and abort the restore. Metric rows reference devices by ID;
rows for devices that no longer exist, which a restore of an older backup
can leave behind, are removed by a cleanup job, and retention expires them
regardless.

**Backups taken before this change** must still restore. They contain no
TimescaleDB objects, and `--clean` drops only what the dump contains, so the
extension and the `metrics` schema survive. Their `schema_migrations` table
lacks `044`, so the migration re-runs on the next boot, harmlessly, since it
is idempotent.

**Settings → Backups** gains one line: backups contain configuration and
history but not collected network metrics, with a link to the docs section
on protecting the database volume (a volume snapshot, or a full `pg_dump`
run by the host).

**`repair.sh`** takes its own safety dump before a full repair
(`repair.sh:271`), a plain full `pg_dump` that the script never restores. It
gets the same `--table='public.*'`, so it stays small once metrics exist.

**Why `--table`, not `--schema` (settled on the sandbox, 2026-09-29).** The
first version used `--schema=public`. Selecting the schema makes
`pg_dump --clean` emit `DROP SCHEMA IF EXISTS public`, which fails because the
`pgcrypto` and `timescaledb` extensions live in `public`. It fails only after
every table has been dropped, so `ON_ERROR_STOP` left an empty database;
reproduced twice on a scratch database. `--table=public.*` emits no schema or
extension statements, and restored cleanly into an empty and a populated
database with a hypertable in `metrics` surviving both. `pgcrypto` needs
nothing from the dump: no migration uses its functions.

**Rule: Sentinel keeps only tables (and their sequences) in `public`.**
`--table` dumps nothing else, so a function, view or custom type added to
`public` would silently be missing after a restore. At startup the backend
logs a warning naming any such object (`database.WarnNonTableObjects`);
extension-owned objects are excluded. A later phase that needs one must also
change the backup.

### 3. Sites

**Migration `045_sites.sql`:**

```sql
CREATE TABLE sites (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    address     TEXT,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_sites_name_lower ON sites (lower(name));

CREATE TABLE site_sharing (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id             UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    shared_with_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission          VARCHAR(50) NOT NULL DEFAULT 'readonly',
    shared_by_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (site_id, shared_with_user_id),
    CHECK (permission IN ('readonly', 'editable'))
);
CREATE INDEX idx_site_sharing_user_id ON site_sharing (shared_with_user_id);
```

`site_sharing` mirrors `monitor_sharing` (migration 010), with two
deliberate differences: `shared_by_user_id` is `SET NULL` rather than a
blocking reference, so deleting the admin who shared a site does not fail,
and `permission` is constrained in the database, not only in Go. Site names
are unique case-insensitively: two sites called "HQ" and "hq" would be
indistinguishable in every picker that later phases add.

**Models:** `models.Site` and `models.SiteSharing` in
`backend/internal/models/site.go`, following `monitor_sharing.go`.

**The access check.** Every later phase (devices, dashboards, maps, metric
reports) asks one question: what may this user do in this site? It is
answered in one place:

```go
type SiteAccessLevel int

const (
    SiteAccessNone     SiteAccessLevel = iota
    SiteAccessReadonly // see the site and everything in it
    SiteAccessEditable // also change what is inside it (later phases)
    SiteAccessAdmin    // also rename, delete, and manage sharing
)

// resolveSiteAccess is pure: the rules, with no database.
func resolveSiteAccess(isAdmin bool, share *models.SiteSharing) SiteAccessLevel

// SiteAccess loads the user's share for the site and resolves it.
func (s *SiteService) SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (SiteAccessLevel, error)
```

Admins are `SiteAccessAdmin` for every site. Other users get their share's
level, or `SiteAccessNone`. Editable users cannot rename, delete or reshare a
site: only admins can, matching "admins manage, sharing grants access".

`SiteService` (`backend/internal/services/site_service.go`) also provides
List (filtered by access), Get, Create, Update, Delete, and share
list/add/update/remove.

**API** (`backend/internal/api/site_handler.go`), under the authenticated
group:

| Method | Path | Who |
|---|---|---|
| GET | `/api/sites` | any user; admins get all, others their shared sites |
| GET | `/api/sites/:id` | readonly or above; response includes the caller's access level |
| POST | `/api/sites` | admin |
| PUT | `/api/sites/:id` | admin |
| DELETE | `/api/sites/:id` | admin |
| GET | `/api/sites/:id/shares` | admin |
| POST | `/api/sites/:id/shares` | admin; body `{user_id, permission}`, upserts |
| DELETE | `/api/sites/:id/shares/:userID` | admin |

- Admin routes use `RequireAdmin`, which must `Abort()` before returning so
  the handler never runs for a non-admin.
- A site the caller cannot see is a **404**, not a 403, so site names and IDs
  are not confirmed to people without access.
- A duplicate name is a 409 with a clear message.
- **Delete** refuses with a 409 while the site contains anything. In phase 0
  nothing can belong to a site, so the check exists but always passes; each
  later phase adds its own tables to it. Deleting a site is never a way to
  silently delete its devices.
- Create, update, delete, share and unshare are written to the audit log
  through `AuditService.Record`.

### 4. The Network Monitoring section

**Sidebar switching.** `Layout.tsx` today has one `nav` array rendered by
`SidebarBody`, which is shared by the fixed 224px desktop sidebar and the
mobile drawer. It gains a second array, `networkNav`, and picks between them
by route: any path under `/network` renders `networkNav`, everything else the
existing `nav`. `SidebarBody`, width, user footer and drawer are shared, so
both navs look and behave identically.

Inside the network section:

- the wordmark subtitle reads **Network Monitoring** instead of "Uptime
  Monitor";
- the first item is **← Sentinel**, which returns to Overview;
- in phase 0 the only other item is **Sites**. Later phases add Devices and
  Credentials (1), MIB Library (3), Dashboards (4) and Maps (6). Admin-only
  items are hidden from non-admins, as Users is today.

The main `nav` gains **Network Monitoring** (`/network`), after Server
Monitoring, in the "what is being monitored" group.

**Routes** (`App.tsx`, lazy-loaded like the others):

- `/network` redirects to `/network/sites` until a network overview page
  exists.
- `/network/sites`: site cards (name, description, address). Admins see Add,
  Edit and Delete.
- `/network/sites/:id`: the site's details, an empty state ("No devices yet.
  SNMP devices arrive in a later update."), and, for admins, a **Sharing**
  panel: pick a user, readonly or editable, remove. It reuses the monitor
  sharing UI's components where they fit.

All of it follows the existing design system (slate/Tailwind, accents from
`utils/colors.ts`).

**Overview.** Overview shows one card per sidebar section, in sidebar order
(commit `fdb10b3`). It gains a **Network Monitoring** card after Server
Monitoring, leading with the number of sites the user can see and opening
`/network`.

**Reports** stay in the main Reports section. The metric report type
(roadmap phase 5) will be a third type there, and the network nav may link to
it.

## Testing

The backend's tests are pure Go unit tests with no database, the frontend
has none, and CI builds images only. Phase 0 matches that and verifies
database behaviour on the sandbox. A database test harness (TimescaleDB in a
container) is deferred to phase 2, where the metrics code needs it.

**Unit tests:**

- `resolveSiteAccess`: admin gets admin with or without a share; readonly
  and editable shares map to their levels; no share gives none; an unknown
  permission string gives none rather than anything higher.
- Site handlers, in the style of `auth_middleware_test.go`: a non-admin
  calling each admin route gets 403 **and the handler body does not run**;
  an inaccessible site is 404; a duplicate name is 409.
- The TimescaleDB preflight message.
- `BackupService` dump arguments include `--table=public.*` and never select a
  schema; the non-table-objects warning names what it finds.

**Sandbox checklist** (`/srv/docker/sentinel-dev`, `IMAGE_TAG=dev`):

1. Switch to the TimescaleDB image. The existing data opens; 044 and 045
   apply; `timescaledb` is in `pg_extension`; `SHOW
   timescaledb.telemetry_level` is `off`.
2. Every existing page loads. Overview shows the Network Monitoring card.
3. As an admin, create, edit and share a site. As a second, non-admin user:
   only shared sites are listed; no manage controls; an unshared site's URL
   is a 404; admin API calls are 403 with no change made.
4. Backups: create a throwaway hypertable in `metrics` with a row in it.
   Take a backup; confirm the file contains no `timescaledb`, `EXTENSION`,
   `_timescaledb` or `metrics.` content. Restore it; the app works and the
   throwaway hypertable and its row survive. Confirm the two open questions
   from section 2 (`public` schema handling, `pgcrypto`).
5. Restore a backup taken **before** the switch; 044 re-applies on the next
   boot and the app works.
6. Rollback drill: drop the throwaway table and the extension, revert the
   image to `postgres:16-alpine`, confirm the database opens and the app
   works; switch forward again.
7. Break the preflight on purpose (plain Postgres with 044 unapplied, on a
   scratch database) and confirm the startup error is the friendly one.

## Rollout

1. Build on `feature/network-monitoring`; deploy to the sandbox; run the
   checklist.
2. Merge to `dev` once it passes.
3. Live, at a time the user chooses: take a backup in the app **and** copy
   the Postgres volume; switch the image; run checklist steps 1–3 against
   live.

Rollback on live is the drill from step 6. It stays that simple until
phase 2 creates real hypertables, which is why phase 0 rehearses it now.

## Risks

- **Image switch on existing data.** Mitigated by identical Postgres major
  version and libc, a volume copy before the switch, and the sandbox
  rehearsal of both directions.
- **Other installs upgrading.** Anyone who pulls a new backend but keeps an
  old compose file gets the preflight's explanation rather than a raw SQL
  error.
- **Timescale License.** Compression and continuous aggregates, which later
  phases use, are under the Timescale License in the `timescale/timescaledb`
  image. It permits use inside a self-hosted application like Sentinel; it
  prohibits offering TimescaleDB itself as a hosted database service, which
  Sentinel does not do.
