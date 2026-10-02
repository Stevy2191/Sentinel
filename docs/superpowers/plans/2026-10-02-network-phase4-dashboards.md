# Network phase 4: custom dashboards — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Users build dashboards — drag-and-drop grids of widgets over network metrics, devices, sites, monitors and server agents — and view them in the app, fullscreen on a wall display, or through an admin-made public link with no login.

**Architecture:** A new Go package `internal/dashboards` owns the dashboard tables, the access rules, a widget registry (one file per widget type) and a resolver. Every widget's data comes from one endpoint that reads the widget's saved config, checks the viewer's access to each subject centrally, and calls the existing services; public links use the same code with sensitive fields blanked. The React side adds a `react-grid-layout` grid, an editor with live previews, one renderer and one settings form per widget type, and a display mode shared by fullscreen and the public page.

**Tech Stack:** Go 1.26, Gin, GORM (pgx), PostgreSQL 16 + TimescaleDB, `golang.org/x/sync/singleflight`; React 18 + TypeScript + Vite + Tailwind + Recharts, `react-grid-layout` 2.2.4.

**Spec:** `docs/superpowers/specs/2026-10-02-network-phase4-dashboards-design.md` (approved 2026-10-02). Read it before starting; this plan argues from it.

**Deviations from the spec (decided while planning; keep them):**
1. The code lives in `internal/dashboards`, not `internal/services/widgets/`. The dashboard service must call the widgets and the widgets call `services`; a `services/widgets` subpackage would make `services` import its own child — an import cycle. `dashboards` imports `services`; nothing in `services` imports `dashboards`.
2. The site-wide UPS status is composed inside the `site_power` widget from `DeviceService.List` and the existing `PortService.UPSStatus`, instead of a new `PortService.SiteUPSStatus`. Same data, no new service method.
3. `monitors` widgets in `bars` style with the `90d` window refresh every 15 minutes (daily buckets change once a day, and 90 days of checks per monitor every 30 s is wasteful). `list` and `24h` bars keep the spec's 30 s.
4. `device_table` and `event_log` get their own compact renderers: `DeviceTable` is a full-page sortable table, and the event log merges incidents with port events. `HealthSection` and `UPSPanel` each give up a presentational piece (`HealthRowTile`, `UPSReadingTiles`) so widgets reuse them without the charts those sections embed, which call logged-in APIs.
5. Public responses keep the same JSON shapes as logged-in ones, with sensitive fields blanked (subject ids omitted or zeroed, hosts and free text emptied), so one renderer serves both views.
6. Leaving the editor with unsaved changes is confirmed in-page only for **Cancel**; reloading or closing the tab gets the browser's own warning; a click on the sidebar leaves without asking. The app uses `<BrowserRouter>`, and React Router can only block in-app navigation from a data router (`useBlocker`). Moving the app to a data router is out of scope; Task 31 records it as a follow-up.

---

## Global Constraints

- Next migration is **`057_dashboards.sql`** (latest applied: `056_custom_metrics.sql`). Never edit an applied migration.
- Limits: **50** widgets per dashboard; widget `config` at most **16 KB**; dashboard name **1–100** chars (trimmed); description at most **500** chars; widget title at most **100** chars; label text at most **200** chars.
- Per widget: at most **10** metrics, **50** devices, **200** instances, **50** monitors, **50** agents. `top_n` `n` is **5–20**; `event_log` `limit` is **10–50**; `open_incidents` `limit` is **5–50** (default 20).
- Grid: **12** columns; `x >= 0`, `w` 1–12, `x + w <= 12`, `y >= 0`, `h` 1–24; row height **80 px**.
- Ranges: `1h`, `6h`, `24h`, `7d`, `30d`, `90d`, `1y` — the same set as `/network/metrics/query`.
- Refresh: **30 s** for `port_grid`, `site_power`, `device_health`, `open_incidents`, `event_log`, `device_table` and `monitors` (except `bars`+`90d`: **15 min**); charts and stats **60 s** up to a 6h range, **5 min** up to 7d, **15 min** beyond; `label` **never** (0).
- Public routes: per-IP limit **600 requests/minute**; public data cached **15 s**; tokens are **32 random bytes, base64url** (43 chars).
- A dashboard or widget the caller cannot see answers **404**, never 403. Real failures go through `respondInternal` — never raw `err.Error()` on a 500.
- The public trim: no IP addresses, hostnames or ports of devices; no monitor URLs or targets; no free-text status/error/detail strings (device `status_detail`, check errors, incident `root_cause`, `notes`, `resolution_notes`); no device, port, monitor, incident or agent UUIDs. Names, port names and aliases, statuses, values, times and severities stay.
- New tables use `TIMESTAMPTZ`. A CHECK of `col IN (...)` must also say `col IS NOT NULL` when the column is nullable.
- Gin middleware must abort (`c.Abort...`); `respondError` does not abort.
- Frontend: dark-only slate design system; Tailwind colours from `src/utils/colors.ts` (chart strokes from its `chartPalette`); component files export only components (helpers in `src/utils/`); no `window.confirm`/`alert`; guard async results so a late response cannot land after inputs changed.
- Commit after every task with a conventional message ending in the attribution trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Work on `feature/network-phase4` in a git worktree made from `dev`.

### Running the checks on this host

Go is not installed on the work machine; run it in the pinned image. Define these once per shell, **from the worktree's `backend/` directory**:

```bash
export GO_CACHE="$HOME/.cache/sentinel-go"; mkdir -p "$GO_CACHE"
go_docker() {
  docker run --rm ${GO_NET:+--network "$GO_NET"} -u "$(id -u):$(id -g)" \
    -v "$PWD:/src" -v "$GO_CACHE:/cache" -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod \
    -e HOME=/tmp -e GOFLAGS=-mod=mod -e SENTINEL_TEST_DATABASE_URL -e SENTINEL_TEST_SNMPSIM \
    -w /src golang:1.26-alpine go "$@"
}
# Database tests: a throwaway TimescaleDB on a private network, then go_docker on it.
db_up() {
  export GO_NET="sentinel-plan-$$"; docker network create "$GO_NET" >/dev/null
  docker run -d --rm --name "$GO_NET-db" --network "$GO_NET" -e POSTGRES_USER=sentinel \
    -e POSTGRES_PASSWORD=test -e POSTGRES_DB=sentinel timescale/timescaledb:2.30.1-pg16 \
    postgres -c shared_preload_libraries=timescaledb -c timescaledb.telemetry_level=off >/dev/null
  until [ "$(docker logs "$GO_NET-db" 2>&1 | grep -c 'ready to accept connections')" -ge 2 ]; do sleep 0.5; done
  export SENTINEL_TEST_DATABASE_URL="postgres://sentinel:test@$GO_NET-db:5432/sentinel?sslmode=disable"
}
db_down() { docker stop "$GO_NET-db" >/dev/null; docker network rm "$GO_NET" >/dev/null; unset GO_NET SENTINEL_TEST_DATABASE_URL; }
```

- Unit tests: `go_docker test ./internal/dashboards/...` (DB tests skip without `SENTINEL_TEST_DATABASE_URL`).
- DB tests: `db_up; go_docker test ./internal/dashboards/... -run TestDB -v; db_down`.
- Whole backend before each commit that touches Go: `go_docker vet ./... && go_docker test ./...` (with `db_up` for the full suite).
- On a machine **with** Go, the repo's own `./scripts/test-db.sh` does the same.
- Frontend gate (from the worktree root; never run npm as root):
  `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`

## Review Focus

Inputs the spec implies but no feature test would naturally exercise; each has a test in the named task.

1. **A device or monitor deleted after a widget was saved.** The widget must show "Removed", not a 500 or an empty chart (Task 9, `TestDBResolveRemovedSubject`).
2. **A custom metric deleted after a widget was saved** (its key no longer `KnownMetric`). Viewing must give `no_data`, not a 400 or 500; only saving re-validates (Task 15, `TestDBTimeseriesUnknownMetricIsNoData`).
3. **Valid JSON of the wrong shape in a widget config** (`"devices": "abc"`, a number where a list belongs) from a buggy or hostile client. Save must answer 400 naming the widget index and field, never 500 (Task 6, `TestDBSaveRejectsWrongShapeConfig`).
4. **The admin who made a public link is demoted or deleted.** The link must stop working at once — on the next request, not after a cache expiry (Task 8, `TestDBResolveTokenCreatorDemoted`; Task 10, `TestPublicWidgetDataChecksTokenEveryRequest`).
5. **A cached public response after the dashboard is edited.** A removed or changed widget must not keep serving its old data for 15 s (Task 9, `TestPublicCacheKeyedByVersion`).

---

## File Structure

**Backend — new**
- `backend/migrations/057_dashboards.sql` — the four tables.
- `backend/internal/models/dashboard.go` — `Dashboard`, `DashboardWidget`, `DashboardShare`, `DashboardPublicLink`, `RawJSON`, limits.
- `backend/internal/dashboards/` — the feature:
  - `access.go` — `Level`, `Viewer`, `resolveAccess`, `canChangeContent`, `canShare`.
  - `errors.go` — the package's error values and `WidgetError`.
  - `ranges.go` — range keys and spans.
  - `subjects.go` — `Subjects`, `Checker`, `DBChecker`, `Filter`, `Filtered`.
  - `registry.go` — `Widget` interface, `Registry`, `ResolveInput`, `ErrNoData`, `FieldError`, config decoding helpers.
  - `service.go` — `Service`: list, get, create, delete; views.
  - `save.go` — `Service.Save` and its validation.
  - `sharing.go` — personal dashboard sharing.
  - `links.go` — public links and token resolution.
  - `resolver.go` — `Resolver`, `Response`, states, the public cache.
  - `deps.go` — the data interfaces widgets use, `Deps`, `NewDefaultRegistry`.
  - `trim.go` — public-trim helpers shared by widgets.
  - `starter.go` — the standard widgets for a new site dashboard.
  - `widget_label.go`, `widget_timeseries.go`, `widget_stat.go`, `widget_port_grid.go`, `widget_device_health.go`, `widget_site_power.go`, `widget_top_n.go`, `widget_event_log.go`, `widget_device_table.go`, `widget_open_incidents.go`, `widget_monitors.go`.
  - Tests beside each file; DB fixtures in `fixtures_db_test.go`.
- `backend/internal/api/dashboard_handler.go`, `dashboard_public_handler.go` (+ tests).
- `backend/internal/api/device_metrics_handler.go` (+ test) — `GET /devices/:id/metrics`.
- `backend/internal/services/metrics_catalog_store.go` — `DeviceMetrics`, `Describe`, `InstanceLabels`.
- `backend/internal/services/metrics_topn.go` — `TopN`.
- `backend/internal/services/uptime_buckets.go` — the uptime bucket helpers, moved from `api`.

**Backend — modified**
- `backend/internal/models/audit.go` — dashboard actions and resource.
- `backend/internal/services/incident_service.go` — `SiteID`, `DeviceIDs`, `MonitorIDs` list options; `OpenCountsByDevice`.
- `backend/internal/services/monitor_sharing_service.go` — `MonitorsByIDs`.
- `backend/internal/api/status_page_handler.go`, `report_handler.go`, `hourly_buckets_test.go` — use the moved helpers.
- `backend/cmd/sentinel/main.go` — wiring.
- `backend/go.mod` — `golang.org/x/sync` becomes direct.

**Frontend — new**
- `frontend/src/types/dashboards.ts` — API and widget data types.
- `frontend/src/hooks/useDashboards.ts` — list, one, mutations, shares, public link, device metrics, preview.
- `frontend/src/hooks/useWidgetData.ts` — polling with backoff and staleness; public variant.
- `frontend/src/hooks/useDisplayMode.ts` — fullscreen, wake lock, 6-hour reload.
- `frontend/src/utils/dashboards.ts` — widget catalogue, layout helpers, chart rows, staleness, `inputCls`.
- `frontend/src/utils/health.ts`, `frontend/src/utils/ups.ts` — helpers moved out of component files.
- `frontend/src/components/dashboards/` — `DashboardGrid`, `WidgetFrame`, `WidgetBody` (fetches), `WidgetRenderer` (per-type switch), `WidgetSettings` (per-type form switch), `WidgetPreview`, `WidgetPicker`, `WidgetSettingsPanel`, `DashboardEditor`, `DisplayShell`, `NewDashboardModal`, `DashboardSharingPanel`, `PublicLinkPanel`, `RangeOverride`; `pickers/` (`SitePicker`, `DevicePicker`, `MetricPicker`, `InstancePicker`, `MonitorPicker`, `AgentPicker`, `RangeSelect`); `widgets/*` renderers; `settings/*` forms and `Field`.
- `frontend/src/components/network/HealthRowTile.tsx`, `UPSReadingTiles.tsx` — extracted presentational pieces.
- `frontend/src/pages/dashboards/Dashboards.tsx`, `DashboardPage.tsx`, `PublicDashboard.tsx`.

**Frontend — modified**
- `App.tsx` (routes), `components/Layout.tsx` (nav), `pages/Overview.tsx` + `components/ShimmerStatCard.tsx` (card), `pages/network/SiteDetail.tsx` (site dashboards, delete counts), `components/network/TrafficChart.tsx` (height prop), `components/network/HealthSection.tsx`, `components/network/UPSPanel.tsx` (use the extracted pieces), `utils/colors.ts` (`chartPalette`), `package.json` (`react-grid-layout`).

---
## Part A — Backend core

### Task 1: Schema, models and audit names

**Files:**
- Create: `backend/migrations/057_dashboards.sql`
- Create: `backend/internal/models/dashboard.go`
- Modify: `backend/internal/models/audit.go` (actions and resource constants)
- Create: `backend/internal/dashboards/doc.go`
- Create: `backend/internal/dashboards/fixtures_db_test.go`
- Test: `backend/internal/dashboards/schema_db_test.go`

**Interfaces:**
- Produces: tables `dashboards`, `dashboard_widgets`, `dashboard_sharing`, `dashboard_public_links`; `models.Dashboard`, `models.DashboardWidget`, `models.DashboardShare`, `models.DashboardPublicLink`, `models.RawJSON`; constants `models.MaxDashboardWidgets` (50), `models.MaxWidgetConfigBytes` (16384), `models.DashboardGridColumns` (12), `models.MaxWidgetHeight` (24), `models.MaxDashboardNameLen` (100), `models.MaxDashboardDescriptionLen` (500), `models.MaxWidgetTitleLen` (100); audit `models.ActionDashboardCreated|Updated|Deleted|Shared|Unshared|LinkCreated|LinkRevoked`, `models.ResourceDashboard`; test fixtures `newSite`, `newDevice`, `newMonitor`, `shareSite`, `shareMonitor`, `newAgent`, `newDashboard`, `newWidget`, `publish`.

- [ ] **Step 1: Write the migration**

`backend/migrations/057_dashboards.sql`:

```sql
-- 057_dashboards.sql
-- Phase 4: user-built dashboards (spec 2026-10-02-network-phase4-dashboards-design.md).
-- A dashboard is personal (an owner, no site) or a site's (a site, no owner).

CREATE TABLE IF NOT EXISTS dashboards (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 500),
    site_id     UUID REFERENCES sites (id) ON DELETE CASCADE,
    owner_id    UUID REFERENCES users (id) ON DELETE CASCADE,
    created_by  UUID REFERENCES users (id) ON DELETE SET NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Both FKs in this CHECK cascade; neither is SET NULL, so a delete can
    -- never leave a row that violates it.
    CONSTRAINT dashboards_owner_xor_site CHECK ((site_id IS NULL) = (owner_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS dashboards_site ON dashboards (site_id) WHERE site_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS dashboards_owner ON dashboards (owner_id) WHERE owner_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS dashboard_widgets (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES dashboards (id) ON DELETE CASCADE,
    type         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 100),
    config       JSONB NOT NULL DEFAULT '{}',
    x INTEGER NOT NULL CHECK (x >= 0),
    y INTEGER NOT NULL CHECK (y >= 0),
    w INTEGER NOT NULL CHECK (w BETWEEN 1 AND 12),
    h INTEGER NOT NULL CHECK (h BETWEEN 1 AND 24),
    CONSTRAINT dashboard_widgets_in_grid CHECK (x + w <= 12)
);
CREATE INDEX IF NOT EXISTS dashboard_widgets_dashboard ON dashboard_widgets (dashboard_id);

CREATE TABLE IF NOT EXISTS dashboard_sharing (
    dashboard_id UUID NOT NULL REFERENCES dashboards (id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    permission   TEXT NOT NULL CHECK (permission IN ('readonly', 'editable')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (dashboard_id, user_id)
);
CREATE INDEX IF NOT EXISTS dashboard_sharing_user ON dashboard_sharing (user_id);

CREATE TABLE IF NOT EXISTS dashboard_public_links (
    dashboard_id UUID PRIMARY KEY REFERENCES dashboards (id) ON DELETE CASCADE,
    token        TEXT NOT NULL UNIQUE,
    created_by   UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- [ ] **Step 2: Write the models**

`backend/internal/models/dashboard.go`:

```go
package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Dashboard limits (spec 2026-10-02 §1, §3).
const (
	MaxDashboardWidgets        = 50
	MaxWidgetConfigBytes       = 16 * 1024
	DashboardGridColumns       = 12
	MaxWidgetHeight            = 24
	MaxDashboardNameLen        = 100
	MaxDashboardDescriptionLen = 500
	MaxWidgetTitleLen          = 100
)

// Dashboard is a grid of widgets. Exactly one of SiteID and OwnerID is set
// (dashboards_owner_xor_site): a site's dashboard follows the site's sharing,
// a personal one belongs to its owner.
type Dashboard struct {
	ID          uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name        string     `json:"name" gorm:"column:name;not null"`
	Description string     `json:"description" gorm:"column:description;not null"`
	SiteID      *uuid.UUID `json:"site_id" gorm:"column:site_id;type:uuid"`
	OwnerID     *uuid.UUID `json:"owner_id" gorm:"column:owner_id;type:uuid"`
	CreatedBy   *uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid"`
	// Version goes up by one on every save; a save carrying an older version
	// is refused, so two editors cannot overwrite each other. No gorm default:
	// GORM would omit an explicit value equal to the zero value.
	Version   int       `json:"version" gorm:"column:version;not null"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table name.
func (Dashboard) TableName() string { return "dashboards" }

// DashboardWidget is one widget: its type, its config (shaped by the type's
// own code) and its place on the 12-column grid.
type DashboardWidget struct {
	ID          uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	DashboardID uuid.UUID `json:"dashboard_id" gorm:"column:dashboard_id;type:uuid;not null"`
	Type        string    `json:"type" gorm:"column:type;not null"`
	Title       string    `json:"title" gorm:"column:title;not null"`
	Config      RawJSON   `json:"config" gorm:"column:config;type:jsonb;not null"`
	X           int       `json:"x" gorm:"column:x;not null"`
	Y           int       `json:"y" gorm:"column:y;not null"`
	W           int       `json:"w" gorm:"column:w;not null"`
	H           int       `json:"h" gorm:"column:h;not null"`
}

// TableName pins the table name.
func (DashboardWidget) TableName() string { return "dashboard_widgets" }

// DashboardShare grants one user access to one personal dashboard.
// Permission is PermissionReadonly or PermissionEditable.
type DashboardShare struct {
	DashboardID uuid.UUID `json:"dashboard_id" gorm:"column:dashboard_id;type:uuid;primaryKey"`
	UserID      uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid;primaryKey"`
	Permission  string    `json:"permission" gorm:"column:permission;not null"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// TableName pins the table name.
func (DashboardShare) TableName() string { return "dashboard_sharing" }

// DashboardPublicLink is a dashboard's one public link. Deleting the row
// revokes it; regenerating replaces the token.
type DashboardPublicLink struct {
	DashboardID uuid.UUID `json:"dashboard_id" gorm:"column:dashboard_id;type:uuid;primaryKey"`
	Token       string    `json:"token" gorm:"column:token;not null"`
	CreatedBy   uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid;not null"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// TableName pins the table name.
func (DashboardPublicLink) TableName() string { return "dashboard_public_links" }

// RawJSON is a JSONB column kept as raw bytes: a widget's config, whose shape
// only the widget type's own code knows. Empty marshals as {}.
type RawJSON json.RawMessage

// Value writes the bytes as JSON text.
func (r RawJSON) Value() (driver.Value, error) {
	if len(r) == 0 {
		return "{}", nil
	}
	return string(r), nil
}

// Scan copies the column's bytes (the driver may reuse its buffer).
func (r *RawJSON) Scan(value any) error {
	if value == nil {
		*r = RawJSON("{}")
		return nil
	}
	b, err := asBytes(value)
	if err != nil {
		return err
	}
	*r = append(RawJSON(nil), b...)
	return nil
}

// MarshalJSON emits the stored JSON as is.
func (r RawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("{}"), nil
	}
	return []byte(r), nil
}

// UnmarshalJSON keeps the raw bytes.
func (r *RawJSON) UnmarshalJSON(b []byte) error {
	*r = append(RawJSON(nil), b...)
	return nil
}
```

- [ ] **Step 3: Add the audit names**

In `backend/internal/models/audit.go`, add to the `// Audited actions.` const block, after `ActionMetricDeleted`:

```go
	ActionDashboardCreated     = "dashboard_created"
	ActionDashboardUpdated     = "dashboard_updated"
	ActionDashboardDeleted     = "dashboard_deleted"
	ActionDashboardShared      = "dashboard_shared"
	ActionDashboardUnshared    = "dashboard_unshared"
	ActionDashboardLinkCreated = "dashboard_link_created"
	ActionDashboardLinkRevoked = "dashboard_link_revoked"
```

and to the `// Audited resource types.` block, after `ResourceCustomMetric`:

```go
	ResourceDashboard = "dashboard"
```

- [ ] **Step 4: Create the package and its DB fixtures**

`backend/internal/dashboards/doc.go`:

```go
// Package dashboards stores user-built dashboards, decides who may see and
// change them, and resolves each widget's data. It sits above services: it
// calls them, and nothing in services imports it.
//
// Spec: docs/superpowers/specs/2026-10-02-network-phase4-dashboards-design.md
package dashboards
```

`backend/internal/dashboards/fixtures_db_test.go`:

```go
package dashboards

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func newSite(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, ?)`, id, name)
	return id
}

// newDevice inserts a device in siteID, with its own global v2c credential.
func newDevice(t *testing.T, db *gorm.DB, siteID uuid.UUID, name, host string) uuid.UUID {
	t.Helper()
	credID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, ?, '2c', 'x')`,
		credID, "cred-"+credID.String()[:8])
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, ?, ?)`,
		id, siteID, credID, name, host)
	return id
}

func newMonitor(t *testing.T, db *gorm.DB, owner uuid.UUID, name, url string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO monitors (id, name, type, url, owner_id) VALUES (?, ?, 'http', ?, ?)`,
		id, name, url, owner)
	return id
}

func shareSite(t *testing.T, db *gorm.DB, siteID, userID uuid.UUID, permission string) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, ?)`,
		siteID, userID, permission)
}

func shareMonitor(t *testing.T, db *gorm.DB, monitorID, userID, by uuid.UUID) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO monitor_sharing (monitor_id, shared_with_user_id, permission, shared_by_user_id)
		VALUES (?, ?, 'readonly', ?)`, monitorID, userID, by)
}

func newAgent(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO agents (id, name, agent_id, server_token) VALUES (?, ?, ?, ?)`,
		id, name, "a-"+id.String()[:8], "tok-"+id.String())
	return id
}

// newDashboard inserts a dashboard: personal when owner is set, a site's when
// site is set (exactly one must be).
func newDashboard(t *testing.T, db *gorm.DB, name string, site, owner *uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO dashboards (id, name, site_id, owner_id, version) VALUES (?, ?, ?, ?, 1)`,
		id, name, site, owner)
	return id
}

func newWidget(t *testing.T, db *gorm.DB, dashboardID uuid.UUID, typ, config string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO dashboard_widgets (id, dashboard_id, type, config, x, y, w, h)
		VALUES (?, ?, ?, ?::jsonb, 0, 0, 4, 2)`, id, dashboardID, typ, config)
	return id
}

// publish gives a dashboard a public link made by creator; returns the token.
func publish(t *testing.T, db *gorm.DB, dashboardID, creator uuid.UUID) string {
	t.Helper()
	token := "tok" + uuid.NewString()
	testdb.Exec(t, db, `INSERT INTO dashboard_public_links (dashboard_id, token, created_by) VALUES (?, ?, ?)`,
		dashboardID, token, creator)
	return token
}

func ptr[T any](v T) *T { return &v }
```

- [ ] **Step 5: Write the failing schema tests**

`backend/internal/dashboards/schema_db_test.go`:

```go
package dashboards

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBDashboardIsPersonalOrSiteNeverBoth(t *testing.T) {
	db := testdb.Open(t)
	user := testdb.NewUser(t, db, false)
	site := newSite(t, db, "HQ")
	for name, args := range map[string][]any{
		"neither": {uuid.New(), "x", nil, nil},
		"both":    {uuid.New(), "x", site, user},
	} {
		err := db.Exec(`INSERT INTO dashboards (id, name, site_id, owner_id) VALUES (?, ?, ?, ?)`, args...).Error
		if err == nil {
			t.Errorf("%s: insert succeeded, want the owner/site CHECK to refuse it", name)
		}
	}
	newDashboard(t, db, "mine", nil, &user)
	newDashboard(t, db, "site's", &site, nil)
}

func TestDBDashboardCascades(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	admin := testdb.NewUser(t, db, true)
	other := testdb.NewUser(t, db, false)
	site := newSite(t, db, "HQ")
	personal := newDashboard(t, db, "mine", nil, &owner)
	siteDash := newDashboard(t, db, "site's", &site, nil)
	newWidget(t, db, personal, "label", `{"text":"a"}`)
	newWidget(t, db, siteDash, "label", `{"text":"b"}`)
	testdb.Exec(t, db, `INSERT INTO dashboard_sharing (dashboard_id, user_id, permission) VALUES (?, ?, 'readonly')`, personal, other)
	publish(t, db, siteDash, admin)

	testdb.Exec(t, db, `DELETE FROM users WHERE id = ?`, owner)
	testdb.Exec(t, db, `DELETE FROM sites WHERE id = ?`, site)

	for _, table := range []string{"dashboards", "dashboard_widgets", "dashboard_sharing", "dashboard_public_links"} {
		var n int64
		testdb.Must(t, db.Table(table).Count(&n).Error)
		if n != 0 {
			t.Errorf("%s has %d rows after deleting the owner and the site, want 0", table, n)
		}
	}
}

func TestDBWidgetMustFitTheGrid(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	d := newDashboard(t, db, "mine", nil, &owner)
	err := db.Exec(`INSERT INTO dashboard_widgets (dashboard_id, type, x, y, w, h) VALUES (?, 'label', 10, 0, 4, 2)`, d).Error
	if err == nil {
		t.Fatal("x+w = 14 accepted, want the grid CHECK to refuse it")
	}
}

func TestDBWidgetConfigRoundTrips(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	d := newDashboard(t, db, "mine", nil, &owner)
	w := models.DashboardWidget{DashboardID: d, Type: "label", Config: models.RawJSON(`{"text":"Main Campus","size":"l"}`), W: 4, H: 1}
	testdb.Must(t, db.Create(&w).Error)
	var got models.DashboardWidget
	testdb.Must(t, db.First(&got, "id = ?", w.ID).Error)
	if string(got.Config) != `{"size": "l", "text": "Main Campus"}` {
		t.Fatalf("config = %s, want the jsonb normal form of what was saved", got.Config)
	}
}
```

- [ ] **Step 6: Run the tests**

Run: `db_up; go_docker test ./internal/dashboards/ -run TestDB -v; db_down`
Expected: all four PASS once the migration and models exist (they FAIL with "relation \"dashboards\" does not exist" if Step 1 is missing).

- [ ] **Step 7: Vet and commit**

```bash
go_docker vet ./...
git add backend/migrations/057_dashboards.sql backend/internal/models/dashboard.go backend/internal/models/audit.go backend/internal/dashboards
git commit -m "feat(dashboards): schema, models and audit names

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Dashboard access rules

**Files:**
- Create: `backend/internal/dashboards/access.go`
- Test: `backend/internal/dashboards/access_test.go`

**Interfaces:**
- Consumes: `models.Dashboard`, `models.PermissionReadonly|Editable`, `services.SiteAccessLevel` and its constants.
- Produces: `type Level int` with `LevelNone`, `LevelView`, `LevelEdit`, `LevelManage` and `String()` (`"none"|"view"|"edit"|"manage"`); `type Viewer struct{ UserID uuid.UUID; IsAdmin bool; Public bool }`; `var PublicViewer = Viewer{Public: true}`; `resolveAccess(v Viewer, d *models.Dashboard, siteLevel services.SiteAccessLevel, share string) Level`; `sitePermissionLevel(p string) services.SiteAccessLevel`; `canChangeContent(l Level, published bool) bool`; `canShare(v Viewer, d *models.Dashboard) bool`.

- [ ] **Step 1: Write the failing tests**

`backend/internal/dashboards/access_test.go`:

```go
package dashboards

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

func TestResolveAccess(t *testing.T) {
	owner, someone := uuid.New(), uuid.New()
	site := uuid.New()
	personal := &models.Dashboard{OwnerID: &owner}
	siteDash := &models.Dashboard{SiteID: &site}
	cases := []struct {
		name  string
		v     Viewer
		d     *models.Dashboard
		site  services.SiteAccessLevel
		share string
		want  Level
	}{
		{"admin, personal", Viewer{UserID: someone, IsAdmin: true}, personal, 0, "", LevelManage},
		{"admin, site", Viewer{UserID: someone, IsAdmin: true}, siteDash, 0, "", LevelManage},
		{"public never", PublicViewer, siteDash, services.SiteAccessEditable, "", LevelNone},
		{"owner", Viewer{UserID: owner}, personal, 0, "", LevelEdit},
		{"shared readonly", Viewer{UserID: someone}, personal, 0, models.PermissionReadonly, LevelView},
		{"shared editable", Viewer{UserID: someone}, personal, 0, models.PermissionEditable, LevelEdit},
		{"stranger, personal", Viewer{UserID: someone}, personal, 0, "", LevelNone},
		{"unknown share", Viewer{UserID: someone}, personal, 0, "owner", LevelNone},
		{"site readonly", Viewer{UserID: someone}, siteDash, services.SiteAccessReadonly, "", LevelView},
		{"site editable", Viewer{UserID: someone}, siteDash, services.SiteAccessEditable, "", LevelEdit},
		{"site none", Viewer{UserID: someone}, siteDash, services.SiteAccessNone, "", LevelNone},
		{"site share ignored on personal", Viewer{UserID: someone}, personal, services.SiteAccessEditable, "", LevelNone},
		{"personal share ignored on site", Viewer{UserID: someone}, siteDash, services.SiteAccessNone, models.PermissionEditable, LevelNone},
	}
	for _, c := range cases {
		if got := resolveAccess(c.v, c.d, c.site, c.share); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCanChangeContent(t *testing.T) {
	cases := []struct {
		l         Level
		published bool
		want      bool
	}{
		{LevelView, false, false}, {LevelEdit, false, true}, {LevelManage, false, true},
		{LevelEdit, true, false}, {LevelManage, true, true},
	}
	for _, c := range cases {
		if got := canChangeContent(c.l, c.published); got != c.want {
			t.Errorf("canChangeContent(%v, published=%v) = %v, want %v", c.l, c.published, got, c.want)
		}
	}
}

func TestCanShare(t *testing.T) {
	owner, someone, site := uuid.New(), uuid.New(), uuid.New()
	personal := &models.Dashboard{OwnerID: &owner}
	siteDash := &models.Dashboard{SiteID: &site}
	if !canShare(Viewer{UserID: owner}, personal) {
		t.Error("the owner cannot share their dashboard")
	}
	if canShare(Viewer{UserID: someone}, personal) {
		t.Error("a stranger can share someone's dashboard")
	}
	if !canShare(Viewer{UserID: someone, IsAdmin: true}, personal) {
		t.Error("an admin cannot share a personal dashboard")
	}
	if canShare(PublicViewer, personal) {
		t.Error("a public viewer can share")
	}
	if canShare(Viewer{UserID: someone}, siteDash) {
		t.Error("a member can share a site dashboard (it follows the site's sharing)")
	}
}

func TestLevelString(t *testing.T) {
	for l, want := range map[Level]string{LevelNone: "none", LevelView: "view", LevelEdit: "edit", LevelManage: "manage"} {
		if l.String() != want {
			t.Errorf("Level(%d).String() = %q, want %q", l, l.String(), want)
		}
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go_docker test ./internal/dashboards/ -run 'TestResolveAccess|TestCanChangeContent|TestCanShare|TestLevelString'`
Expected: FAIL — `undefined: resolveAccess` (and the other names).

- [ ] **Step 3: Implement**

`backend/internal/dashboards/access.go`:

```go
package dashboards

import (
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Level is what a viewer may do with a dashboard. Levels are ordered: each
// includes everything below it, so callers compare with >=.
type Level int

const (
	// LevelNone: the dashboard does not exist as far as this viewer knows.
	LevelNone Level = iota
	// LevelView: see the dashboard and load its widgets.
	LevelView
	// LevelEdit: also change it (unless it is published) and see its configs.
	LevelEdit
	// LevelManage: an admin: everything, published or not.
	LevelManage
)

// String is the level's name as the API reports it.
func (l Level) String() string {
	switch l {
	case LevelView:
		return "view"
	case LevelEdit:
		return "edit"
	case LevelManage:
		return "manage"
	default:
		return "none"
	}
}

// Viewer is who is asking. A Public viewer is a public-link request: it has
// no user, and the logged-in routes never grant it anything.
type Viewer struct {
	UserID  uuid.UUID
	IsAdmin bool
	Public  bool
}

// PublicViewer is the viewer of every public-link request.
var PublicViewer = Viewer{Public: true}

// resolveAccess holds every dashboard permission rule, with no database, so
// the rules are tested directly. siteLevel is the viewer's access to a site
// dashboard's site and is ignored for a personal one; share is the viewer's
// share on a personal dashboard ("" for none) and is ignored for a site's.
func resolveAccess(v Viewer, d *models.Dashboard, siteLevel services.SiteAccessLevel, share string) Level {
	if v.Public {
		return LevelNone
	}
	if v.IsAdmin {
		return LevelManage
	}
	if d.SiteID != nil {
		switch {
		case siteLevel >= services.SiteAccessEditable:
			return LevelEdit
		case siteLevel >= services.SiteAccessReadonly:
			return LevelView
		}
		return LevelNone
	}
	if d.OwnerID != nil && *d.OwnerID == v.UserID {
		return LevelEdit
	}
	switch share {
	case models.PermissionEditable:
		return LevelEdit
	case models.PermissionReadonly:
		return LevelView
	}
	return LevelNone
}

// sitePermissionLevel turns a site_sharing permission into the level
// resolveSiteAccess would give it ("" or anything unknown is none).
func sitePermissionLevel(p string) services.SiteAccessLevel {
	switch p {
	case models.PermissionReadonly:
		return services.SiteAccessReadonly
	case models.PermissionEditable:
		return services.SiteAccessEditable
	}
	return services.SiteAccessNone
}

// canChangeContent reports whether l may change a dashboard's name,
// description, widgets or layout, or delete it. A published dashboard needs
// manage, so what is public stays what an admin approved.
func canChangeContent(l Level, published bool) bool {
	if published {
		return l >= LevelManage
	}
	return l >= LevelEdit
}

// canShare reports whether v may change who a dashboard is shared with: the
// owner of a personal dashboard, or an admin. A site dashboard follows its
// site's sharing, which only admins change.
func canShare(v Viewer, d *models.Dashboard) bool {
	if v.Public {
		return false
	}
	if v.IsAdmin {
		return true
	}
	return d.SiteID == nil && d.OwnerID != nil && *d.OwnerID == v.UserID
}
```

- [ ] **Step 4: Run to see them pass**

Run: `go_docker test ./internal/dashboards/ -run 'TestResolveAccess|TestCanChangeContent|TestCanShare|TestLevelString' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/dashboards/access.go backend/internal/dashboards/access_test.go
git commit -m "feat(dashboards): one function for every dashboard permission rule

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Subjects and the central access filter

**Files:**
- Create: `backend/internal/dashboards/subjects.go`
- Test: `backend/internal/dashboards/subjects_db_test.go`

**Interfaces:**
- Consumes: `Viewer`, `services.SiteService.SiteAccess`, `services.MonitorService.CanUserViewMonitor`, `services.ErrSiteNotFound`.
- Produces:
  - `type Subjects struct{ Sites, Devices, Monitors, Agents []uuid.UUID; Broad bool }`
  - states `StateOK = "ok"`, `StateNoAccess = "no_access"`, `StateRemoved = "removed"`, `StateNoData = "no_data"`
  - `type Filtered struct{ Visible Subjects; Hidden, Removed int }` with `State() string`
  - `type SiteLeveler interface{ SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error) }`
  - `type MonitorViewer interface{ CanUserViewMonitor(ctx context.Context, userID, monitorID uuid.UUID) (bool, error) }`
  - `type Checker interface` (5 methods below), `type DBChecker`, `NewDBChecker(db *gorm.DB, sites SiteLeveler, monitors MonitorViewer) *DBChecker`
  - `Filter(ctx context.Context, c Checker, v Viewer, s Subjects) (Filtered, error)`

Rules (spec §2): sites and devices by site access (a device follows its site); monitors by `CanUserViewMonitor`; server agents admin-only. Admins and public viewers see everything that exists. Missing subjects count as removed.

- [ ] **Step 1: Write the failing tests**

`backend/internal/dashboards/subjects_db_test.go`:

```go
package dashboards

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

type subjectWorld struct {
	checker                              *DBChecker
	admin, member                        uuid.UUID
	sharedSite, otherSite                uuid.UUID
	sharedDev, otherDev                  uuid.UUID
	ownMonitor, sharedMonitor, otherMon  uuid.UUID
	agent                                uuid.UUID
}

func newSubjectWorld(t *testing.T) subjectWorld {
	t.Helper()
	db := testdb.Open(t)
	w := subjectWorld{admin: testdb.NewUser(t, db, true), member: testdb.NewUser(t, db, false)}
	w.sharedSite, w.otherSite = newSite(t, db, "Shared"), newSite(t, db, "Other")
	shareSite(t, db, w.sharedSite, w.member, models.PermissionReadonly)
	w.sharedDev = newDevice(t, db, w.sharedSite, "core", "10.0.0.1")
	w.otherDev = newDevice(t, db, w.otherSite, "edge", "10.9.0.1")
	w.ownMonitor = newMonitor(t, db, w.member, "mine", "https://mine.test")
	w.sharedMonitor = newMonitor(t, db, w.admin, "shared", "https://shared.test")
	shareMonitor(t, db, w.sharedMonitor, w.member, w.admin)
	w.otherMon = newMonitor(t, db, w.admin, "other", "https://other.test")
	w.agent = newAgent(t, db, "fileserver")
	w.checker = NewDBChecker(db, services.NewSiteService(db), services.NewMonitorService(db))
	return w
}

func (w subjectWorld) all() Subjects {
	return Subjects{
		Sites:    []uuid.UUID{w.sharedSite, w.otherSite},
		Devices:  []uuid.UUID{w.sharedDev, w.otherDev, uuid.New()},
		Monitors: []uuid.UUID{w.ownMonitor, w.sharedMonitor, w.otherMon, uuid.New()},
		Agents:   []uuid.UUID{w.agent},
	}
}

func TestDBFilterMember(t *testing.T) {
	w := newSubjectWorld(t)
	f, err := Filter(context.Background(), w.checker, Viewer{UserID: w.member}, w.all())
	testdb.Must(t, err)
	if len(f.Visible.Sites) != 1 || f.Visible.Sites[0] != w.sharedSite {
		t.Errorf("visible sites = %v, want only the shared site", f.Visible.Sites)
	}
	if len(f.Visible.Devices) != 1 || f.Visible.Devices[0] != w.sharedDev {
		t.Errorf("visible devices = %v, want only the device in the shared site", f.Visible.Devices)
	}
	if len(f.Visible.Monitors) != 2 {
		t.Errorf("visible monitors = %v, want own and shared", f.Visible.Monitors)
	}
	if len(f.Visible.Agents) != 0 {
		t.Errorf("a member sees agents %v; agents are admin-only", f.Visible.Agents)
	}
	// Hidden: other site, other device, other monitor, the agent. Removed: one device, one monitor.
	if f.Hidden != 4 || f.Removed != 2 {
		t.Errorf("hidden=%d removed=%d, want 4 and 2", f.Hidden, f.Removed)
	}
	if f.State() != "" {
		t.Errorf("State() = %q with visible subjects, want \"\" (resolve)", f.State())
	}
}

func TestDBFilterAdminAndPublicSeeEverythingThatExists(t *testing.T) {
	w := newSubjectWorld(t)
	for name, v := range map[string]Viewer{"admin": {UserID: w.admin, IsAdmin: true}, "public": PublicViewer} {
		f, err := Filter(context.Background(), w.checker, v, w.all())
		testdb.Must(t, err)
		if f.Hidden != 0 || f.Removed != 2 {
			t.Errorf("%s: hidden=%d removed=%d, want 0 and 2", name, f.Hidden, f.Removed)
		}
		if len(f.Visible.Devices) != 2 || len(f.Visible.Monitors) != 3 || len(f.Visible.Agents) != 1 || len(f.Visible.Sites) != 2 {
			t.Errorf("%s: visible = %+v, want every existing subject", name, f.Visible)
		}
	}
}

func TestDBFilterStates(t *testing.T) {
	w := newSubjectWorld(t)
	ctx := context.Background()
	member := Viewer{UserID: w.member}

	f, err := Filter(ctx, w.checker, member, Subjects{Devices: []uuid.UUID{w.otherDev}})
	testdb.Must(t, err)
	if f.State() != StateNoAccess {
		t.Errorf("only a hidden device: State() = %q, want no_access", f.State())
	}
	f, err = Filter(ctx, w.checker, member, Subjects{Devices: []uuid.UUID{uuid.New()}})
	testdb.Must(t, err)
	if f.State() != StateRemoved {
		t.Errorf("only a missing device: State() = %q, want removed", f.State())
	}
	f, err = Filter(ctx, w.checker, member, Subjects{})
	testdb.Must(t, err)
	if f.State() != "" {
		t.Errorf("no subjects (a label): State() = %q, want \"\"", f.State())
	}
	f, err = Filter(ctx, w.checker, member, Subjects{Broad: true})
	testdb.Must(t, err)
	if f.State() != "" || !f.Visible.Broad {
		t.Errorf("broad scope: State() = %q, Broad = %v; want \"\" and true", f.State(), f.Visible.Broad)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `db_up; go_docker test ./internal/dashboards/ -run TestDBFilter -v; db_down`
Expected: FAIL — `undefined: NewDBChecker`.

- [ ] **Step 3: Implement**

`backend/internal/dashboards/subjects.go`:

```go
package dashboards

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Widget data states (spec §4). Every state is a 200 response.
const (
	StateOK       = "ok"
	StateNoAccess = "no_access"
	StateRemoved  = "removed"
	StateNoData   = "no_data"
)

// Subjects is what a widget's config refers to, for access checks. A port is
// listed as its device: a port's access is its device's.
type Subjects struct {
	Sites    []uuid.UUID
	Devices  []uuid.UUID
	Monitors []uuid.UUID
	Agents   []uuid.UUID
	// Broad: the widget also shows things chosen by the viewer's own access
	// ("all open incidents I can see"), so its rows differ between viewers.
	Broad bool
}

func (s Subjects) count() int {
	return len(s.Sites) + len(s.Devices) + len(s.Monitors) + len(s.Agents)
}

// Filtered is a widget's subjects sorted by what one viewer may see.
type Filtered struct {
	Visible Subjects
	Hidden  int
	Removed int
	total   int
}

// State is the response state when none of the widget's subjects remain
// visible, or "" when the widget should resolve. A widget with no subjects
// (a label) or a broad scope always resolves.
func (f Filtered) State() string {
	if f.total == 0 || f.Visible.count() > 0 {
		return ""
	}
	if f.Hidden > 0 {
		return StateNoAccess
	}
	return StateRemoved
}

// SiteLeveler is services.SiteService.SiteAccess.
type SiteLeveler interface {
	SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error)
}

// MonitorViewer is services.MonitorService.CanUserViewMonitor.
type MonitorViewer interface {
	CanUserViewMonitor(ctx context.Context, userID, monitorID uuid.UUID) (bool, error)
}

// Checker answers existence and visibility questions by the existing rules.
// Taken as an interface so the resolver is tested without a database.
type Checker interface {
	// DeviceSites maps each existing device to its site; missing ones are absent.
	DeviceSites(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
	// SiteLevel is the viewer's access to a site; services.ErrSiteNotFound
	// when it does not exist.
	SiteLevel(ctx context.Context, v Viewer, siteID uuid.UUID) (services.SiteAccessLevel, error)
	ExistingMonitors(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)
	MonitorVisible(ctx context.Context, v Viewer, id uuid.UUID) (bool, error)
	ExistingAgents(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)
}

// DBChecker is the Checker over the real tables and services.
type DBChecker struct {
	db       *gorm.DB
	sites    SiteLeveler
	monitors MonitorViewer
}

// NewDBChecker builds the production Checker.
func NewDBChecker(db *gorm.DB, sites SiteLeveler, monitors MonitorViewer) *DBChecker {
	return &DBChecker{db: db, sites: sites, monitors: monitors}
}

// DeviceSites implements Checker.
func (c *DBChecker) DeviceSites(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	var rows []struct {
		ID     uuid.UUID
		SiteID uuid.UUID
	}
	if err := c.db.WithContext(ctx).Table("devices").Select("id, site_id").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading device sites: %w", err)
	}
	out := make(map[uuid.UUID]uuid.UUID, len(rows))
	for _, r := range rows {
		out[r.ID] = r.SiteID
	}
	return out, nil
}

// SiteLevel implements Checker. A public viewer is answered as an admin: an
// admin published the dashboard.
func (c *DBChecker) SiteLevel(ctx context.Context, v Viewer, siteID uuid.UUID) (services.SiteAccessLevel, error) {
	return c.sites.SiteAccess(ctx, v.UserID, v.IsAdmin || v.Public, siteID)
}

// ExistingMonitors implements Checker.
func (c *DBChecker) ExistingMonitors(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	return c.existing(ctx, "monitors", ids)
}

// MonitorVisible implements Checker, by the monitor list's own rule.
func (c *DBChecker) MonitorVisible(ctx context.Context, v Viewer, id uuid.UUID) (bool, error) {
	return c.monitors.CanUserViewMonitor(ctx, v.UserID, id)
}

// ExistingAgents implements Checker.
func (c *DBChecker) ExistingAgents(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	return c.existing(ctx, "agents", ids)
}

// existing reports which ids exist in table (a constant, never input).
func (c *DBChecker) existing(ctx context.Context, table string, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	var found []uuid.UUID
	if err := c.db.WithContext(ctx).Table(table).Where("id IN ?", ids).Pluck("id", &found).Error; err != nil {
		return nil, fmt.Errorf("checking %s: %w", table, err)
	}
	out := make(map[uuid.UUID]bool, len(found))
	for _, id := range found {
		out[id] = true
	}
	return out, nil
}

const (
	subjectVisible = iota + 1
	subjectHidden
	subjectRemoved
)

// Filter sorts s into what v may see, what v may not, and what no longer
// exists. Admins and public viewers see everything that exists. This is the
// only place widget subjects are access-checked; widgets resolve only what
// Filter hands them.
func Filter(ctx context.Context, c Checker, v Viewer, s Subjects) (Filtered, error) {
	out := Filtered{total: s.count()}
	out.Visible.Broad = s.Broad
	everything := v.IsAdmin || v.Public

	siteSeen := map[uuid.UUID]int{}
	siteState := func(id uuid.UUID) (int, error) {
		if st, ok := siteSeen[id]; ok {
			return st, nil
		}
		level, err := c.SiteLevel(ctx, v, id)
		st := subjectVisible
		switch {
		case errors.Is(err, services.ErrSiteNotFound):
			st = subjectRemoved
		case err != nil:
			return 0, err
		case level < services.SiteAccessReadonly:
			st = subjectHidden
		}
		siteSeen[id] = st
		return st, nil
	}
	tally := func(st int) bool {
		switch st {
		case subjectHidden:
			out.Hidden++
		case subjectRemoved:
			out.Removed++
		}
		return st == subjectVisible
	}

	for _, id := range s.Sites {
		st, err := siteState(id)
		if err != nil {
			return out, err
		}
		if tally(st) {
			out.Visible.Sites = append(out.Visible.Sites, id)
		}
	}
	if len(s.Devices) > 0 {
		sites, err := c.DeviceSites(ctx, s.Devices)
		if err != nil {
			return out, err
		}
		for _, id := range s.Devices {
			siteID, ok := sites[id]
			if !ok {
				out.Removed++
				continue
			}
			st, err := siteState(siteID)
			if err != nil {
				return out, err
			}
			if tally(st) {
				out.Visible.Devices = append(out.Visible.Devices, id)
			}
		}
	}
	if len(s.Monitors) > 0 {
		exist, err := c.ExistingMonitors(ctx, s.Monitors)
		if err != nil {
			return out, err
		}
		for _, id := range s.Monitors {
			if !exist[id] {
				out.Removed++
				continue
			}
			ok := everything
			if !ok {
				if ok, err = c.MonitorVisible(ctx, v, id); err != nil {
					return out, err
				}
			}
			if ok {
				out.Visible.Monitors = append(out.Visible.Monitors, id)
			} else {
				out.Hidden++
			}
		}
	}
	if len(s.Agents) > 0 {
		exist, err := c.ExistingAgents(ctx, s.Agents)
		if err != nil {
			return out, err
		}
		for _, id := range s.Agents {
			switch {
			case !exist[id]:
				out.Removed++
			case everything:
				out.Visible.Agents = append(out.Visible.Agents, id)
			default:
				out.Hidden++
			}
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run to see them pass**

Run: `db_up; go_docker test ./internal/dashboards/ -run TestDBFilter -v; db_down`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/dashboards/subjects.go backend/internal/dashboards/subjects_db_test.go
git commit -m "feat(dashboards): check every widget subject in one place

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Widget registry, ranges and the label widget

**Files:**
- Create: `backend/internal/dashboards/registry.go`
- Create: `backend/internal/dashboards/ranges.go`
- Create: `backend/internal/dashboards/widget_label.go`
- Test: `backend/internal/dashboards/registry_test.go`, `backend/internal/dashboards/widget_label_test.go`

**Interfaces:**
- Consumes: `Subjects`, `Viewer`.
- Produces:
  - `type Widget interface { Type() string; Validate(ctx context.Context, cfg json.RawMessage) (json.RawMessage, error); Subjects(cfg json.RawMessage) Subjects; Resolve(ctx context.Context, cfg json.RawMessage, in ResolveInput) (any, error); Refresh(cfg json.RawMessage, override string) time.Duration }`
  - `type ResolveInput struct{ Visible Subjects; Viewer Viewer; Override string; Now time.Time }`
  - `var ErrNoData`; `type FieldError struct{ Field, Msg string }`; `fieldErr(field, msg string) error`; `decodeConfig(raw json.RawMessage, dst any) error`
  - `type Registry`; `NewRegistry(ws ...Widget) *Registry`; `(*Registry).Get(t string) (Widget, bool)`; `(*Registry).Types() []string`
  - `ValidRange(r string) bool`; `rangeSpan(r string) time.Duration`; `effectiveRange(saved, override string) string`; `chartRefresh(r string) time.Duration`; `const statusRefresh = 30 * time.Second`; `validateRange(r string) (string, error)`
  - helpers `dedupe(ids []uuid.UUID) []uuid.UUID`, `checkCount(field string, n, lo, hi int) error`
  - `type labelWidget struct{}`; `type LabelData struct{ Text, Size string }`

- [ ] **Step 1: Write the failing tests**

`backend/internal/dashboards/registry_test.go`:

```go
package dashboards

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRegistryKeepsOrderAndRefusesDuplicates(t *testing.T) {
	r := NewRegistry(labelWidget{})
	if got := r.Types(); len(got) != 1 || got[0] != "label" {
		t.Fatalf("Types() = %v, want [label]", got)
	}
	if _, ok := r.Get("nope"); ok {
		t.Error("Get(nope) found a widget")
	}
	defer func() {
		if recover() == nil {
			t.Error("registering a type twice did not panic")
		}
	}()
	NewRegistry(labelWidget{}, labelWidget{})
}

func TestDecodeConfigNamesTheField(t *testing.T) {
	var c struct {
		Devices []uuid.UUID `json:"devices"`
		N       int         `json:"n"`
	}
	for raw, field := range map[string]string{
		`{"devices": "abc"}`:   "devices",
		`{"n": "five"}`:        "n",
		`{"devices": ["abc"]}`: "config",
		`{"devices": [`:        "config",
	} {
		err := decodeConfig(json.RawMessage(raw), &c)
		var fe *FieldError
		if !errors.As(err, &fe) {
			t.Errorf("%s: err = %v, want a FieldError", raw, err)
			continue
		}
		if fe.Field != field {
			t.Errorf("%s: field = %q, want %q", raw, fe.Field, field)
		}
	}
}

func TestRanges(t *testing.T) {
	for _, r := range []string{"1h", "6h", "24h", "7d", "30d", "90d", "1y"} {
		if !ValidRange(r) {
			t.Errorf("ValidRange(%q) = false", r)
		}
	}
	if ValidRange("2h") || ValidRange("") {
		t.Error("ValidRange accepts 2h or empty")
	}
	if effectiveRange("24h", "") != "24h" || effectiveRange("24h", "7d") != "7d" {
		t.Error("effectiveRange does not prefer the override")
	}
	for r, want := range map[string]time.Duration{"1h": time.Minute, "6h": time.Minute, "24h": 5 * time.Minute,
		"7d": 5 * time.Minute, "30d": 15 * time.Minute, "1y": 15 * time.Minute} {
		if got := chartRefresh(r); got != want {
			t.Errorf("chartRefresh(%s) = %v, want %v", r, got, want)
		}
	}
}

func TestDedupeAndCount(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	if got := dedupe([]uuid.UUID{a, b, a, uuid.Nil}); len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("dedupe = %v, want [a b] in order without nil", got)
	}
	if checkCount("devices", 3, 1, 50) != nil || checkCount("devices", 0, 1, 50) == nil || checkCount("devices", 51, 1, 50) == nil {
		t.Error("checkCount bounds are wrong")
	}
}
```

`backend/internal/dashboards/widget_label_test.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestLabelValidate(t *testing.T) {
	ctx := context.Background()
	w := labelWidget{}
	got, err := w.Validate(ctx, json.RawMessage(`{"text":"  Main Campus  "}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"text":"Main Campus","size":"m"}` {
		t.Errorf("normalised = %s, want trimmed text and default size m", got)
	}
	for raw, field := range map[string]string{
		`{"text":""}`:                                      "text",
		`{"text":"` + strings.Repeat("x", 201) + `"}`:      "text",
		`{"text":"a\u0007b"}`:                              "text",
		`{"text":"ok","size":"xl"}`:                        "size",
		`{"text":5}`:                                       "text",
	} {
		_, err := w.Validate(ctx, json.RawMessage(raw))
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: err = %v, want a FieldError on %q", raw, err, field)
		}
	}
}

func TestLabelResolveAndRefresh(t *testing.T) {
	w := labelWidget{}
	data, err := w.Resolve(context.Background(), json.RawMessage(`{"text":"HQ","size":"l"}`), ResolveInput{})
	if err != nil {
		t.Fatal(err)
	}
	if d := data.(LabelData); d.Text != "HQ" || d.Size != "l" {
		t.Errorf("data = %+v", d)
	}
	if w.Refresh(nil, "") != 0 {
		t.Error("a label refreshes; it should never need to")
	}
	if s := w.Subjects(nil); s.count() != 0 || s.Broad {
		t.Errorf("a label has subjects %+v", s)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go_docker test ./internal/dashboards/ -run 'TestRegistry|TestDecodeConfig|TestRanges|TestDedupe|TestLabel'`
Expected: FAIL — `undefined: NewRegistry`, `undefined: labelWidget`.

- [ ] **Step 3: Implement the registry**

`backend/internal/dashboards/registry.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Widget is one widget type. Resolve and Subjects only ever see a config
// Validate has normalised (the saved form), so both may assume its shape; a
// config that no longer decodes resolves to zero values, never a panic.
type Widget interface {
	// Type is the name stored in dashboard_widgets.type.
	Type() string
	// Validate normalises a config and rejects a bad one with a *FieldError
	// (the API answers 400 naming the field). Any other error is a 500.
	Validate(ctx context.Context, cfg json.RawMessage) (json.RawMessage, error)
	// Subjects lists what the config refers to, for access checks.
	Subjects(cfg json.RawMessage) Subjects
	// Resolve loads the data for the visible subjects only. ErrNoData
	// becomes the no_data state.
	Resolve(ctx context.Context, cfg json.RawMessage, in ResolveInput) (any, error)
	// Refresh is how soon the browser should ask again; 0 means never.
	Refresh(cfg json.RawMessage, override string) time.Duration
}

// ResolveInput is everything a widget needs beyond its config.
type ResolveInput struct {
	// Visible is the config's subjects that the viewer may see.
	Visible Subjects
	Viewer  Viewer
	// Override is the dashboard's range picker ("" for none); time-based
	// widgets use it instead of their saved range.
	Override string
	Now      time.Time
}

// ErrNoData is returned by Resolve when there is nothing to show yet.
var ErrNoData = errors.New("no data")

// FieldError is a config problem the editor can show next to a field.
type FieldError struct {
	Field string
	Msg   string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Msg }

func fieldErr(field, msg string) error { return &FieldError{Field: field, Msg: msg} }

// decodeConfig unmarshals a config. A value of the wrong type is a
// FieldError naming the field; anything else that fails is a FieldError on
// "config", so a hostile or buggy client gets a 400, never a 500.
func decodeConfig(raw json.RawMessage, dst any) error {
	err := json.Unmarshal(raw, dst)
	if err == nil {
		return nil
	}
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) && te.Field != "" {
		return fieldErr(te.Field, "has the wrong type")
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return fieldErr("config", "is not valid JSON")
	}
	return fieldErr("config", "has an invalid value")
}

// Registry maps widget type names to their implementations.
type Registry struct {
	byType map[string]Widget
	order  []string
}

// NewRegistry registers ws in order. Registering a type twice is a
// programming error and panics at startup.
func NewRegistry(ws ...Widget) *Registry {
	r := &Registry{byType: make(map[string]Widget, len(ws))}
	for _, w := range ws {
		if _, dup := r.byType[w.Type()]; dup {
			panic(fmt.Sprintf("dashboards: widget type %q registered twice", w.Type()))
		}
		r.byType[w.Type()] = w
		r.order = append(r.order, w.Type())
	}
	return r
}

// Get returns the widget registered as t.
func (r *Registry) Get(t string) (Widget, bool) {
	w, ok := r.byType[t]
	return w, ok
}

// Types lists the registered type names in registration order.
func (r *Registry) Types() []string { return append([]string(nil), r.order...) }

// dedupe drops nil and repeated ids, keeping first-seen order.
func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// checkCount is a FieldError unless lo <= n <= hi.
func checkCount(field string, n, lo, hi int) error {
	if n < lo || n > hi {
		if lo == 0 {
			return fieldErr(field, fmt.Sprintf("allows at most %d", hi))
		}
		return fieldErr(field, fmt.Sprintf("needs %d to %d", lo, hi))
	}
	return nil
}
```

`backend/internal/dashboards/ranges.go`:

```go
package dashboards

import "time"

// rangeSpans are the ranges a time-based widget may show: the same set the
// metrics query endpoint accepts.
var rangeSpans = map[string]time.Duration{
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour, "1y": 365 * 24 * time.Hour,
}

// statusRefresh is how often live-status widgets refresh.
const statusRefresh = 30 * time.Second

// ValidRange reports whether r is a known range key.
func ValidRange(r string) bool {
	_, ok := rangeSpans[r]
	return ok
}

// validateRange defaults an empty range to 24h and rejects unknown ones.
func validateRange(r string) (string, error) {
	if r == "" {
		return "24h", nil
	}
	if !ValidRange(r) {
		return "", fieldErr("range", "must be 1h, 6h, 24h, 7d, 30d, 90d or 1y")
	}
	return r, nil
}

// rangeSpan is r's length; an unknown key reads as 24h.
func rangeSpan(r string) time.Duration {
	if d, ok := rangeSpans[r]; ok {
		return d
	}
	return 24 * time.Hour
}

// effectiveRange is the dashboard override when set, else the saved range.
func effectiveRange(saved, override string) string {
	if override != "" {
		return override
	}
	return saved
}

// chartRefresh is the refresh period of a chart or stat showing range r:
// 60 s up to 6h, 5 min up to 7d, 15 min beyond (spec §4).
func chartRefresh(r string) time.Duration {
	switch span := rangeSpan(r); {
	case span <= 6*time.Hour:
		return time.Minute
	case span <= 7*24*time.Hour:
		return 5 * time.Minute
	}
	return 15 * time.Minute
}
```

- [ ] **Step 4: Implement the label widget**

`backend/internal/dashboards/widget_label.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxLabelText = 200

type labelConfig struct {
	Text string `json:"text"`
	Size string `json:"size"`
}

// LabelData is a label's response: its text and size (s, m or l).
type LabelData struct {
	Text string `json:"text"`
	Size string `json:"size"`
}

// labelWidget is plain text, e.g. a heading on a wall display. Its text is
// rendered as text, never as HTML or Markdown.
type labelWidget struct{}

func (labelWidget) Type() string { return "label" }

func (labelWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c labelConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	c.Text = strings.TrimSpace(c.Text)
	switch {
	case c.Text == "":
		return nil, fieldErr("text", "is required")
	case utf8.RuneCountInString(c.Text) > maxLabelText:
		return nil, fieldErr("text", "must be 200 characters or fewer")
	case strings.ContainsFunc(c.Text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }):
		return nil, fieldErr("text", "must be plain text")
	}
	switch c.Size {
	case "":
		c.Size = "m"
	case "s", "m", "l":
	default:
		return nil, fieldErr("size", "must be s, m or l")
	}
	return json.Marshal(c)
}

func (labelWidget) Subjects(json.RawMessage) Subjects { return Subjects{} }

func (labelWidget) Resolve(_ context.Context, raw json.RawMessage, _ ResolveInput) (any, error) {
	var c labelConfig
	_ = json.Unmarshal(raw, &c)
	return LabelData{Text: c.Text, Size: c.Size}, nil
}

func (labelWidget) Refresh(json.RawMessage, string) time.Duration { return 0 }
```

- [ ] **Step 5: Run to see them pass**

Run: `go_docker test ./internal/dashboards/ -run 'TestRegistry|TestDecodeConfig|TestRanges|TestDedupe|TestLabel' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/dashboards/registry.go backend/internal/dashboards/ranges.go backend/internal/dashboards/widget_label.go backend/internal/dashboards/registry_test.go backend/internal/dashboards/widget_label_test.go
git commit -m "feat(dashboards): widget registry, ranges and the label widget

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 5: Dashboard service — list, get, create, delete

**Files:**
- Create: `backend/internal/dashboards/errors.go`
- Create: `backend/internal/dashboards/service.go`
- Test: `backend/internal/dashboards/service_db_test.go`

**Interfaces:**
- Consumes: `resolveAccess`, `sitePermissionLevel`, `canChangeContent`, `canShare`, `Registry`, `Checker`, `SiteLeveler`, `models.Dashboard`, `models.DashboardWidget`.
- Produces:
  - errors `ErrNotFound`, `ErrForbidden`, `ErrPublished`, `ErrInvalid`, `ErrSiteNotFound`, `ErrWidgetNotFound`, `ErrShareSiteDashboard`, `ErrShareOwner`, `ErrUnknownUser`, `ErrNoLink`; `type VersionConflictError struct{ Current int }`; `type WidgetError struct{ Index int; Field, Msg string }`; `invalid(msg string) error`
  - `type Service`; `NewService(db *gorm.DB, sites SiteLeveler, registry *Registry, checker Checker) *Service`
  - `type DashboardView struct{ models.Dashboard; SiteName string; Access string; Published bool; CanEdit bool; CanShare bool; WidgetCount int }` (JSON: `site_name`, `access`, `published`, `can_edit`, `can_share`, `widget_count`) with unexported `level Level`
  - `type WidgetView struct{ ID uuid.UUID; Type, Title string; X, Y, W, H int; Config models.RawJSON }` (JSON `config` omitted when empty)
  - `type DashboardDetail struct{ DashboardView; Widgets []WidgetView }`
  - `type CreateInput struct{ Name, Description string; SiteID *uuid.UUID }`
  - methods `List(ctx, v Viewer, siteID *uuid.UUID) ([]DashboardView, error)`, `Get(ctx, v Viewer, id uuid.UUID) (*DashboardDetail, error)`, `Create(ctx, v Viewer, in CreateInput) (*DashboardDetail, error)`, `Delete(ctx, v Viewer, id uuid.UUID) (*models.Dashboard, error)`; internal `load(ctx, v, id) (*DashboardView, error)`, `widgets(ctx, id) ([]models.DashboardWidget, error)`, `normalizeNames(name, desc string) (string, string, error)`, `contentError(vw *DashboardView) error`

- [ ] **Step 1: Write the failing tests**

`backend/internal/dashboards/service_db_test.go`:

```go
package dashboards

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// accessWorld has one user per role against one personal and one site dashboard.
type accessWorld struct {
	db                                           *gorm.DB
	svc                                          *Service
	admin, owner, readonly, editable, siteRO     uuid.UUID
	siteRW, stranger                             uuid.UUID
	site, personal, siteDash                     uuid.UUID
}

func newAccessWorld(t *testing.T) accessWorld {
	t.Helper()
	db := testdb.Open(t)
	w := accessWorld{db: db}
	w.admin = testdb.NewUser(t, db, true)
	for _, id := range []*uuid.UUID{&w.owner, &w.readonly, &w.editable, &w.siteRO, &w.siteRW, &w.stranger} {
		*id = testdb.NewUser(t, db, false)
	}
	w.site = newSite(t, db, "HQ")
	shareSite(t, db, w.site, w.siteRO, models.PermissionReadonly)
	shareSite(t, db, w.site, w.siteRW, models.PermissionEditable)
	w.personal = newDashboard(t, db, "Mine", nil, &w.owner)
	testdb.Exec(t, db, `INSERT INTO dashboard_sharing (dashboard_id, user_id, permission) VALUES (?, ?, 'readonly'), (?, ?, 'editable')`,
		w.personal, w.readonly, w.personal, w.editable)
	w.siteDash = newDashboard(t, db, "HQ overview", &w.site, nil)
	newWidget(t, db, w.siteDash, "label", `{"text":"HQ","size":"m"}`)
	sites := services.NewSiteService(db)
	w.svc = NewService(db, sites, NewRegistry(labelWidget{}), NewDBChecker(db, sites, services.NewMonitorService(db)))
	return w
}

func (w accessWorld) viewer(id uuid.UUID) Viewer { return Viewer{UserID: id, IsAdmin: id == w.admin} }

func TestDBListShowsWhatEachRoleMaySee(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	want := map[uuid.UUID][]uuid.UUID{
		w.admin:    {w.siteDash, w.personal},
		w.owner:    {w.personal},
		w.readonly: {w.personal},
		w.editable: {w.personal},
		w.siteRO:   {w.siteDash},
		w.siteRW:   {w.siteDash},
		w.stranger: {},
	}
	for user, ids := range want {
		got, err := w.svc.List(ctx, w.viewer(user), nil)
		testdb.Must(t, err)
		if len(got) != len(ids) {
			t.Errorf("user %s sees %d dashboards, want %d", user, len(got), len(ids))
			continue
		}
		seen := map[uuid.UUID]bool{}
		for _, d := range got {
			seen[d.ID] = true
		}
		for _, id := range ids {
			if !seen[id] {
				t.Errorf("user %s does not see dashboard %s", user, id)
			}
		}
	}
	if _, err := w.svc.List(ctx, PublicViewer, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("public List err = %v, want ErrNotFound", err)
	}
}

func TestDBGetAccessAndConfigs(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	cases := []struct {
		user       uuid.UUID
		id         uuid.UUID
		access     string
		withConfig bool
	}{
		{w.admin, w.siteDash, "manage", true},
		{w.siteRW, w.siteDash, "edit", true},
		{w.siteRO, w.siteDash, "view", false},
		{w.owner, w.personal, "edit", true},
		{w.readonly, w.personal, "view", false},
	}
	for _, c := range cases {
		d, err := w.svc.Get(ctx, w.viewer(c.user), c.id)
		testdb.Must(t, err)
		if d.Access != c.access {
			t.Errorf("access = %s, want %s", d.Access, c.access)
		}
		if c.id == w.siteDash {
			if len(d.Widgets) != 1 {
				t.Fatalf("widgets = %d, want 1", len(d.Widgets))
			}
			if hasConfig := len(d.Widgets[0].Config) > 0; hasConfig != c.withConfig {
				t.Errorf("%s: config sent = %v, want %v (configs need edit)", c.access, hasConfig, c.withConfig)
			}
		}
	}
	for _, user := range []uuid.UUID{w.stranger, w.siteRO} {
		if _, err := w.svc.Get(ctx, w.viewer(user), w.personal); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get by a user without access err = %v, want ErrNotFound (404, not 403)", err)
		}
	}
}

func TestDBCreate(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	d, err := w.svc.Create(ctx, w.viewer(w.stranger), CreateInput{Name: "  My view  "})
	testdb.Must(t, err)
	if d.Name != "My view" || d.OwnerID == nil || *d.OwnerID != w.stranger || d.SiteID != nil || d.Version != 1 {
		t.Errorf("personal create = %+v", d.Dashboard)
	}
	if _, err := w.svc.Create(ctx, w.viewer(w.siteRO), CreateInput{Name: "x", SiteID: &w.site}); !errors.Is(err, ErrForbidden) {
		t.Errorf("readonly site create err = %v, want ErrForbidden", err)
	}
	if _, err := w.svc.Create(ctx, w.viewer(w.stranger), CreateInput{Name: "x", SiteID: &w.site}); !errors.Is(err, ErrSiteNotFound) {
		t.Errorf("create on an unshared site err = %v, want ErrSiteNotFound", err)
	}
	sd, err := w.svc.Create(ctx, w.viewer(w.siteRW), CreateInput{Name: "Closet", SiteID: &w.site})
	testdb.Must(t, err)
	if sd.SiteID == nil || sd.OwnerID != nil || sd.CreatedBy == nil || *sd.CreatedBy != w.siteRW {
		t.Errorf("site create = %+v", sd.Dashboard)
	}
	for _, name := range []string{"", "   ", string(make([]rune, 101))} {
		if _, err := w.svc.Create(ctx, w.viewer(w.owner), CreateInput{Name: name}); !errors.Is(err, ErrInvalid) {
			t.Errorf("name %q err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestDBDelete(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	if _, err := w.svc.Delete(ctx, w.viewer(w.readonly), w.personal); !errors.Is(err, ErrForbidden) {
		t.Errorf("readonly delete err = %v, want ErrForbidden", err)
	}
	if _, err := w.svc.Delete(ctx, w.viewer(w.stranger), w.personal); !errors.Is(err, ErrNotFound) {
		t.Errorf("stranger delete err = %v, want ErrNotFound", err)
	}
	publish(t, w.db, w.siteDash, w.admin)
	if _, err := w.svc.Delete(ctx, w.viewer(w.siteRW), w.siteDash); !errors.Is(err, ErrPublished) {
		t.Errorf("editor deleting a published dashboard err = %v, want ErrPublished", err)
	}
	d, err := w.svc.Get(ctx, w.viewer(w.siteRW), w.siteDash)
	testdb.Must(t, err)
	if !d.Published || d.CanEdit {
		t.Errorf("published=%v can_edit=%v for an editor of a published dashboard, want true/false", d.Published, d.CanEdit)
	}
	if _, err := w.svc.Delete(ctx, w.viewer(w.admin), w.siteDash); err != nil {
		t.Errorf("admin delete of a published dashboard: %v", err)
	}
	if _, err := w.svc.Delete(ctx, w.viewer(w.editable), w.personal); err != nil {
		t.Errorf("shared-editable delete: %v", err)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `db_up; go_docker test ./internal/dashboards/ -run 'TestDBList|TestDBGet|TestDBCreate|TestDBDelete' -v; db_down`
Expected: FAIL — `undefined: NewService`.

- [ ] **Step 3: Write the errors**

`backend/internal/dashboards/errors.go`:

```go
package dashboards

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound covers "missing" and "not yours": the API answers 404 for both.
	ErrNotFound = errors.New("dashboard not found")
	// ErrForbidden: the caller can see the dashboard but not do this to it.
	ErrForbidden = errors.New("you need edit access to do that")
	// ErrPublished: an editor tried to change a published dashboard.
	ErrPublished = errors.New("this dashboard is published; ask an admin to change it")
	// ErrInvalid wraps a validation message for the dashboard itself.
	ErrInvalid = errors.New("invalid dashboard")
	// ErrSiteNotFound: a site dashboard for a site the caller cannot see.
	ErrSiteNotFound = errors.New("site not found")
	// ErrWidgetNotFound: no such widget on this dashboard.
	ErrWidgetNotFound = errors.New("widget not found")
	// ErrShareSiteDashboard: site dashboards follow their site's sharing.
	ErrShareSiteDashboard = errors.New("a site dashboard is shared through its site")
	// ErrShareOwner: sharing a dashboard with its own owner.
	ErrShareOwner = errors.New("the owner already has this dashboard")
	// ErrUnknownUser: sharing with a user who does not exist.
	ErrUnknownUser = errors.New("no such user")
	// ErrNoLink: revoking a link that does not exist.
	ErrNoLink = errors.New("this dashboard has no public link")
)

// invalid wraps msg as an ErrInvalid.
func invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalid, msg) }

// VersionConflictError: a save carried an older version than the stored one.
type VersionConflictError struct{ Current int }

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("someone else saved this dashboard (it is now at version %d)", e.Current)
}

// WidgetError is a save rejected because of one widget; Index is its position
// in the request (0-based), Field the config field or "type"/"title"/"position".
type WidgetError struct {
	Index int
	Field string
	Msg   string
}

func (e *WidgetError) Error() string {
	return fmt.Sprintf("widget %d: %s %s", e.Index+1, e.Field, e.Msg)
}
```

- [ ] **Step 4: Write the service**

`backend/internal/dashboards/service.go`:

```go
package dashboards

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Service stores dashboards and decides who may do what with them.
type Service struct {
	db       *gorm.DB
	sites    SiteLeveler
	registry *Registry
	checker  Checker
}

// NewService builds the dashboard service.
func NewService(db *gorm.DB, sites SiteLeveler, registry *Registry, checker Checker) *Service {
	return &Service{db: db, sites: sites, registry: registry, checker: checker}
}

// DashboardView is a dashboard as one caller sees it, with what the caller
// may do, so the page knows which controls to show without a second request.
type DashboardView struct {
	models.Dashboard
	SiteName    string `json:"site_name"`
	Access      string `json:"access"`
	Published   bool   `json:"published"`
	CanEdit     bool   `json:"can_edit"`
	CanShare    bool   `json:"can_share"`
	WidgetCount int    `json:"widget_count"`
	level       Level
}

// WidgetView is a widget as one caller sees it. Config is sent only to
// callers with edit access: it holds subject ids.
type WidgetView struct {
	ID     uuid.UUID      `json:"id"`
	Type   string         `json:"type"`
	Title  string         `json:"title"`
	X      int            `json:"x"`
	Y      int            `json:"y"`
	W      int            `json:"w"`
	H      int            `json:"h"`
	Config models.RawJSON `json:"config,omitempty"`
}

// DashboardDetail is a dashboard and its widgets.
type DashboardDetail struct {
	DashboardView
	Widgets []WidgetView `json:"widgets"`
}

// CreateInput is what a create request supplies. SiteID set: a site
// dashboard; nil: a personal one owned by the caller.
type CreateInput struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	SiteID      *uuid.UUID `json:"site_id"`
}

// dashboardRow is one dashboard joined to what decides the caller's access.
type dashboardRow struct {
	models.Dashboard
	SiteName        string `gorm:"column:site_name"`
	Published       bool   `gorm:"column:published"`
	SharePermission string `gorm:"column:share_permission"`
	SitePermission  string `gorm:"column:site_permission"`
	WidgetCount     int    `gorm:"column:widget_count"`
}

const dashboardSelect = `d.*, COALESCE(st.name, '') AS site_name,
	(pl.dashboard_id IS NOT NULL) AS published,
	COALESCE(sh.permission, '') AS share_permission,
	COALESCE(ss.permission, '') AS site_permission,
	(SELECT count(*) FROM dashboard_widgets w WHERE w.dashboard_id = d.id) AS widget_count`

// query selects dashboards with the caller's share and site share joined in.
func (s *Service) query(ctx context.Context, v Viewer) *gorm.DB {
	return s.db.WithContext(ctx).Table("dashboards AS d").Select(dashboardSelect).
		Joins("LEFT JOIN sites st ON st.id = d.site_id").
		Joins("LEFT JOIN dashboard_public_links pl ON pl.dashboard_id = d.id").
		Joins("LEFT JOIN dashboard_sharing sh ON sh.dashboard_id = d.id AND sh.user_id = ?", v.UserID).
		Joins("LEFT JOIN site_sharing ss ON ss.site_id = d.site_id AND ss.shared_with_user_id = ?", v.UserID)
}

func (r dashboardRow) view(v Viewer) DashboardView {
	level := resolveAccess(v, &r.Dashboard, sitePermissionLevel(r.SitePermission), r.SharePermission)
	return DashboardView{
		Dashboard:   r.Dashboard,
		SiteName:    r.SiteName,
		Access:      level.String(),
		Published:   r.Published,
		CanEdit:     canChangeContent(level, r.Published),
		CanShare:    canShare(v, &r.Dashboard),
		WidgetCount: r.WidgetCount,
		level:       level,
	}
}

// List returns the dashboards v can see, optionally only one site's.
func (s *Service) List(ctx context.Context, v Viewer, siteID *uuid.UUID) ([]DashboardView, error) {
	if v.Public {
		return nil, ErrNotFound
	}
	q := s.query(ctx, v)
	if !v.IsAdmin {
		q = q.Where("(d.owner_id = ? OR sh.user_id IS NOT NULL OR ss.id IS NOT NULL)", v.UserID)
	}
	if siteID != nil {
		q = q.Where("d.site_id = ?", *siteID)
	}
	var rows []dashboardRow
	if err := q.Order("lower(d.name), d.id").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing dashboards: %w", err)
	}
	out := make([]DashboardView, 0, len(rows))
	for _, r := range rows {
		if vw := r.view(v); vw.level >= LevelView {
			out = append(out, vw)
		}
	}
	return out, nil
}

// load is one dashboard as v sees it; ErrNotFound when missing or not v's.
func (s *Service) load(ctx context.Context, v Viewer, id uuid.UUID) (*DashboardView, error) {
	var rows []dashboardRow
	if err := s.query(ctx, v).Where("d.id = ?", id).Limit(1).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading dashboard: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	vw := rows[0].view(v)
	if vw.level < LevelView {
		return nil, ErrNotFound
	}
	return &vw, nil
}

// widgets returns a dashboard's widgets top to bottom, left to right.
func (s *Service) widgets(ctx context.Context, id uuid.UUID) ([]models.DashboardWidget, error) {
	var ws []models.DashboardWidget
	if err := s.db.WithContext(ctx).Where("dashboard_id = ?", id).Order("y, x, id").Find(&ws).Error; err != nil {
		return nil, fmt.Errorf("loading widgets: %w", err)
	}
	return ws, nil
}

// Get returns a dashboard and its widgets as v sees them.
func (s *Service) Get(ctx context.Context, v Viewer, id uuid.UUID) (*DashboardDetail, error) {
	vw, err := s.load(ctx, v, id)
	if err != nil {
		return nil, err
	}
	ws, err := s.widgets(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &DashboardDetail{DashboardView: *vw, Widgets: make([]WidgetView, 0, len(ws))}
	for _, w := range ws {
		wv := WidgetView{ID: w.ID, Type: w.Type, Title: w.Title, X: w.X, Y: w.Y, W: w.W, H: w.H}
		if vw.level >= LevelEdit {
			wv.Config = w.Config
		}
		out.Widgets = append(out.Widgets, wv)
	}
	return out, nil
}

// normalizeNames trims and checks a dashboard's name and description.
func normalizeNames(name, desc string) (string, string, error) {
	name, desc = strings.TrimSpace(name), strings.TrimSpace(desc)
	if name == "" {
		return "", "", invalid("name is required")
	}
	if utf8.RuneCountInString(name) > models.MaxDashboardNameLen {
		return "", "", invalid("name must be 100 characters or fewer")
	}
	if utf8.RuneCountInString(desc) > models.MaxDashboardDescriptionLen {
		return "", "", invalid("description must be 500 characters or fewer")
	}
	return name, desc, nil
}

// Create stores a new, empty dashboard: personal unless in.SiteID is set, in
// which case the caller needs editable access to the site.
func (s *Service) Create(ctx context.Context, v Viewer, in CreateInput) (*DashboardDetail, error) {
	if v.Public {
		return nil, ErrNotFound
	}
	name, desc, err := normalizeNames(in.Name, in.Description)
	if err != nil {
		return nil, err
	}
	creator := v.UserID
	d := models.Dashboard{Name: name, Description: desc, Version: 1, CreatedBy: &creator}
	if in.SiteID != nil {
		level, err := s.sites.SiteAccess(ctx, v.UserID, v.IsAdmin, *in.SiteID)
		if errors.Is(err, services.ErrSiteNotFound) || (err == nil && level == services.SiteAccessNone) {
			return nil, ErrSiteNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("checking site access: %w", err)
		}
		if level < services.SiteAccessEditable {
			return nil, ErrForbidden
		}
		siteID := *in.SiteID
		d.SiteID = &siteID
	} else {
		d.OwnerID = &creator
	}
	if err := s.db.WithContext(ctx).Create(&d).Error; err != nil {
		return nil, fmt.Errorf("creating dashboard: %w", err)
	}
	return s.Get(ctx, v, d.ID)
}

// contentError is why vw's content may not be changed: ErrPublished for an
// editor of a published dashboard, ErrForbidden for a viewer.
func contentError(vw *DashboardView) error {
	if vw.Published && vw.level >= LevelEdit {
		return ErrPublished
	}
	return ErrForbidden
}

// Delete removes a dashboard, its widgets, shares and public link.
func (s *Service) Delete(ctx context.Context, v Viewer, id uuid.UUID) (*models.Dashboard, error) {
	vw, err := s.load(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if !vw.CanEdit {
		return nil, contentError(vw)
	}
	if err := s.db.WithContext(ctx).Delete(&models.Dashboard{}, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("deleting dashboard: %w", err)
	}
	return &vw.Dashboard, nil
}
```

- [ ] **Step 5: Run to see them pass**

Run: `db_up; go_docker test ./internal/dashboards/ -run 'TestDBList|TestDBGet|TestDBCreate|TestDBDelete' -v; db_down`
Expected: PASS (4 tests).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/dashboards/errors.go backend/internal/dashboards/service.go backend/internal/dashboards/service_db_test.go
git commit -m "feat(dashboards): list, get, create and delete with per-role access

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Saving a dashboard

**Files:**
- Create: `backend/internal/dashboards/save.go`
- Test: `backend/internal/dashboards/save_db_test.go`

**Interfaces:**
- Consumes: `Service.load`, `normalizeNames`, `contentError`, `Registry.Get`, `Widget.Validate`, `Widget.Subjects`, `Filter`, `FieldError`.
- Produces: `type WidgetInput struct{ ID *uuid.UUID; Type, Title string; Config json.RawMessage; X, Y, W, H int }` (JSON `id`, `type`, `title`, `config`, `x`, `y`, `w`, `h`); `type SaveInput struct{ Version int; Name, Description string; Widgets []WidgetInput }`; `(*Service).Save(ctx, v Viewer, id uuid.UUID, in SaveInput) (*DashboardDetail, error)`; `(*Service).validateWidget(ctx, v Viewer, i int, in WidgetInput) (models.DashboardWidget, error)`.

Rules (spec §1, §2, §4): one transaction; the dashboard row is locked; a stale `version` is a `*VersionConflictError` and nothing is written; widgets with an id keep it, new ones get one, missing ones are deleted; every widget is validated by its type and its subjects checked against the editor; the version goes up by one.

- [ ] **Step 1: Write the failing tests**

`backend/internal/dashboards/save_db_test.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func labelInput(id *uuid.UUID, text string, x, y int) WidgetInput {
	return WidgetInput{ID: id, Type: "label", Config: json.RawMessage(`{"text":"` + text + `"}`), X: x, Y: y, W: 4, H: 1}
}

func TestDBSaveKeepsIdsAndBumpsVersion(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	owner := w.viewer(w.owner)
	d, err := w.svc.Save(ctx, owner, w.personal, SaveInput{Version: 1, Name: "Mine", Widgets: []WidgetInput{
		labelInput(nil, "first", 0, 0), labelInput(nil, "second", 4, 0),
	}})
	testdb.Must(t, err)
	if d.Version != 2 || len(d.Widgets) != 2 {
		t.Fatalf("after save: version %d, %d widgets; want 2 and 2", d.Version, len(d.Widgets))
	}
	keep := d.Widgets[0].ID
	d, err = w.svc.Save(ctx, owner, w.personal, SaveInput{Version: 2, Name: "Renamed", Widgets: []WidgetInput{
		labelInput(&keep, "first, moved", 0, 3),
	}})
	testdb.Must(t, err)
	if d.Name != "Renamed" || d.Version != 3 || len(d.Widgets) != 1 || d.Widgets[0].ID != keep || d.Widgets[0].Y != 3 {
		t.Fatalf("second save = %+v, want the kept widget moved and the other deleted", d)
	}
	if string(d.Widgets[0].Config) != `{"size": "m", "text": "first, moved"}` {
		t.Errorf("config = %s, want the normalised config", d.Widgets[0].Config)
	}
}

func TestDBSaveVersionConflictWritesNothing(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	owner := w.viewer(w.owner)
	_, err := w.svc.Save(ctx, owner, w.personal, SaveInput{Version: 1, Name: "A", Widgets: []WidgetInput{labelInput(nil, "a", 0, 0)}})
	testdb.Must(t, err)
	_, err = w.svc.Save(ctx, owner, w.personal, SaveInput{Version: 1, Name: "B"})
	var vc *VersionConflictError
	if !errors.As(err, &vc) || vc.Current != 2 {
		t.Fatalf("stale save err = %v, want a VersionConflictError at 2", err)
	}
	d, err := w.svc.Get(ctx, owner, w.personal)
	testdb.Must(t, err)
	if d.Name != "A" || len(d.Widgets) != 1 {
		t.Errorf("a refused save changed the dashboard: %+v", d)
	}
}

func TestDBSaveRejectsWrongShapeConfig(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	owner := w.viewer(w.owner)
	cases := []struct {
		widget WidgetInput
		field  string
	}{
		{WidgetInput{Type: "nope", W: 1, H: 1}, "type"},
		{WidgetInput{Type: "label", Config: json.RawMessage(`{"text": 5}`), W: 1, H: 1}, "text"},
		{WidgetInput{Type: "label", Config: json.RawMessage(`{"text": ["a"]}`), W: 1, H: 1}, "text"},
		{WidgetInput{Type: "label", Config: json.RawMessage(`[1,2]`), W: 1, H: 1}, "config"},
		{WidgetInput{Type: "label", Config: json.RawMessage(`{"text":"ok"}`), X: 10, W: 4, H: 1}, "position"},
		{WidgetInput{Type: "label", Config: json.RawMessage(`{"text":"ok"}`), W: 1, H: 25}, "position"},
	}
	for _, c := range cases {
		_, err := w.svc.Save(ctx, owner, w.personal, SaveInput{Version: 1, Name: "Mine", Widgets: []WidgetInput{labelInput(nil, "ok", 0, 0), c.widget}})
		var we *WidgetError
		if !errors.As(err, &we) || we.Index != 1 || we.Field != c.field {
			t.Errorf("%+v: err = %v, want a WidgetError on widget index 1, field %q", c.widget, err, c.field)
		}
	}
}

func TestDBSaveRefusesForeignOrRepeatedWidgetIds(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	foreign := newWidget(t, w.db, w.siteDash, "label", `{"text":"x"}`)
	_, err := w.svc.Save(ctx, w.viewer(w.owner), w.personal, SaveInput{Version: 1, Name: "Mine", Widgets: []WidgetInput{labelInput(&foreign, "steal", 0, 0)}})
	var we *WidgetError
	if !errors.As(err, &we) || we.Field != "id" {
		t.Errorf("saving another dashboard's widget id err = %v, want a WidgetError on id", err)
	}
}

func TestDBSaveAccess(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	in := SaveInput{Version: 1, Name: "x"}
	if _, err := w.svc.Save(ctx, w.viewer(w.readonly), w.personal, in); !errors.Is(err, ErrForbidden) {
		t.Errorf("readonly save err = %v, want ErrForbidden", err)
	}
	if _, err := w.svc.Save(ctx, w.viewer(w.stranger), w.personal, in); !errors.Is(err, ErrNotFound) {
		t.Errorf("stranger save err = %v, want ErrNotFound", err)
	}
	publish(t, w.db, w.siteDash, w.admin)
	if _, err := w.svc.Save(ctx, w.viewer(w.siteRW), w.siteDash, in); !errors.Is(err, ErrPublished) {
		t.Errorf("editor saving a published dashboard err = %v, want ErrPublished", err)
	}
	if _, err := w.svc.Save(ctx, w.viewer(w.admin), w.siteDash, in); err != nil {
		t.Errorf("admin saving a published dashboard: %v", err)
	}
	many := make([]WidgetInput, 51)
	for i := range many {
		many[i] = labelInput(nil, "x", 0, i)
	}
	if _, err := w.svc.Save(ctx, w.viewer(w.owner), w.personal, SaveInput{Version: 1, Name: "x", Widgets: many}); !errors.Is(err, ErrInvalid) {
		t.Errorf("51 widgets err = %v, want ErrInvalid", err)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `db_up; go_docker test ./internal/dashboards/ -run TestDBSave -v; db_down`
Expected: FAIL — `w.svc.Save undefined`.

- [ ] **Step 3: Implement**

`backend/internal/dashboards/save.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// WidgetInput is one widget in a save. ID is nil for a widget added since the
// editor loaded the dashboard.
type WidgetInput struct {
	ID     *uuid.UUID      `json:"id"`
	Type   string          `json:"type"`
	Title  string          `json:"title"`
	Config json.RawMessage `json:"config"`
	X      int             `json:"x"`
	Y      int             `json:"y"`
	W      int             `json:"w"`
	H      int             `json:"h"`
}

// SaveInput is a whole dashboard as the editor has it. Version is the
// version the editor started from.
type SaveInput struct {
	Version     int           `json:"version"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Widgets     []WidgetInput `json:"widgets"`
}

// Save replaces a dashboard's name, description and widgets in one
// transaction. A stale Version is a *VersionConflictError and writes nothing.
func (s *Service) Save(ctx context.Context, v Viewer, id uuid.UUID, in SaveInput) (*DashboardDetail, error) {
	vw, err := s.load(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if !vw.CanEdit {
		return nil, contentError(vw)
	}
	name, desc, err := normalizeNames(in.Name, in.Description)
	if err != nil {
		return nil, err
	}
	if len(in.Widgets) > models.MaxDashboardWidgets {
		return nil, invalid(fmt.Sprintf("a dashboard holds at most %d widgets", models.MaxDashboardWidgets))
	}
	clean := make([]models.DashboardWidget, len(in.Widgets))
	for i, w := range in.Widgets {
		if clean[i], err = s.validateWidget(ctx, v, i, w); err != nil {
			return nil, err
		}
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.Dashboard
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("locking dashboard: %w", err)
		}
		if current.Version != in.Version {
			return &VersionConflictError{Current: current.Version}
		}
		var existing []uuid.UUID
		if err := tx.Model(&models.DashboardWidget{}).Where("dashboard_id = ?", id).Pluck("id", &existing).Error; err != nil {
			return fmt.Errorf("loading widget ids: %w", err)
		}
		known := make(map[uuid.UUID]bool, len(existing))
		for _, e := range existing {
			known[e] = true
		}
		keep := make([]uuid.UUID, 0, len(clean))
		kept := map[uuid.UUID]bool{}
		for i := range clean {
			wid := clean[i].ID
			if wid == uuid.Nil {
				continue
			}
			if !known[wid] {
				return &WidgetError{Index: i, Field: "id", Msg: "is not a widget of this dashboard"}
			}
			if kept[wid] {
				return &WidgetError{Index: i, Field: "id", Msg: "appears twice"}
			}
			kept[wid] = true
			keep = append(keep, wid)
		}
		del := tx.Where("dashboard_id = ?", id)
		if len(keep) > 0 {
			del = del.Where("id NOT IN ?", keep)
		}
		if err := del.Delete(&models.DashboardWidget{}).Error; err != nil {
			return fmt.Errorf("removing widgets: %w", err)
		}
		for i := range clean {
			w := &clean[i]
			w.DashboardID = id
			if w.ID == uuid.Nil {
				w.ID = uuid.New()
				if err := tx.Create(w).Error; err != nil {
					return fmt.Errorf("adding widget: %w", err)
				}
				continue
			}
			err := tx.Model(&models.DashboardWidget{}).Where("id = ?", w.ID).Updates(map[string]any{
				"type": w.Type, "title": w.Title, "config": w.Config, "x": w.X, "y": w.Y, "w": w.W, "h": w.H,
			}).Error
			if err != nil {
				return fmt.Errorf("updating widget: %w", err)
			}
		}
		return tx.Model(&models.Dashboard{}).Where("id = ?", id).Updates(map[string]any{
			"name": name, "description": desc, "version": current.Version + 1, "updated_at": time.Now().UTC(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, v, id)
}

// validateWidget checks one widget and returns it ready to store: its type
// exists, its title and position fit, its config is valid for its type, and
// every subject it uses is one the editor may see.
func (s *Service) validateWidget(ctx context.Context, v Viewer, i int, in WidgetInput) (models.DashboardWidget, error) {
	fail := func(field, msg string) (models.DashboardWidget, error) {
		return models.DashboardWidget{}, &WidgetError{Index: i, Field: field, Msg: msg}
	}
	wd, ok := s.registry.Get(in.Type)
	if !ok {
		return fail("type", fmt.Sprintf("%q is not a widget type", in.Type))
	}
	title := strings.TrimSpace(in.Title)
	if utf8.RuneCountInString(title) > models.MaxWidgetTitleLen {
		return fail("title", "must be 100 characters or fewer")
	}
	cols, maxH := models.DashboardGridColumns, models.MaxWidgetHeight
	if in.X < 0 || in.Y < 0 || in.W < 1 || in.W > cols || in.X+in.W > cols || in.H < 1 || in.H > maxH {
		return fail("position", "must fit the 12-column grid (width 1 to 12, height 1 to 24)")
	}
	raw := in.Config
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	if len(raw) > models.MaxWidgetConfigBytes {
		return fail("config", "is larger than 16 KB")
	}
	cfg, err := wd.Validate(ctx, raw)
	if err != nil {
		var fe *FieldError
		if errors.As(err, &fe) {
			return fail(fe.Field, fe.Msg)
		}
		return models.DashboardWidget{}, fmt.Errorf("validating %s widget: %w", in.Type, err)
	}
	f, err := Filter(ctx, s.checker, v, wd.Subjects(cfg))
	if err != nil {
		return models.DashboardWidget{}, fmt.Errorf("checking widget subjects: %w", err)
	}
	if f.Hidden > 0 {
		return fail("config", "uses a site, device, monitor or server you cannot see")
	}
	if f.Removed > 0 {
		return fail("config", "uses a site, device, monitor or server that no longer exists")
	}
	out := models.DashboardWidget{Type: in.Type, Title: title, Config: models.RawJSON(cfg), X: in.X, Y: in.Y, W: in.W, H: in.H}
	if in.ID != nil {
		out.ID = *in.ID
	}
	return out, nil
}
```

Note on the `[1,2]` case: `json.Unmarshal` of an array into a struct is an `UnmarshalTypeError` with an empty `Field`, which `decodeConfig` reports on `config` — that is why the test expects `config`.

- [ ] **Step 4: Run to see them pass**

Run: `db_up; go_docker test ./internal/dashboards/ -run TestDBSave -v; db_down`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/dashboards/save.go backend/internal/dashboards/save_db_test.go
git commit -m "feat(dashboards): save in one transaction with version conflicts and per-widget validation

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Sharing personal dashboards

**Files:**
- Create: `backend/internal/dashboards/sharing.go`
- Test: `backend/internal/dashboards/sharing_db_test.go`

**Interfaces:**
- Consumes: `Service.load`, `canShare`, `models.DashboardShare`.
- Produces: `type ShareView struct{ UserID uuid.UUID; Username, Email, Permission string; CreatedAt time.Time }` (JSON `user_id`, `username`, `email`, `permission`, `created_at`); `(*Service).ListShares(ctx, v Viewer, id uuid.UUID) ([]ShareView, error)`; `(*Service).UpsertShare(ctx, v Viewer, id, userID uuid.UUID, permission string) error`; `(*Service).RemoveShare(ctx, v Viewer, id, userID uuid.UUID) error`.

- [ ] **Step 1: Write the failing tests**

`backend/internal/dashboards/sharing_db_test.go`:

```go
package dashboards

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBSharing(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	owner := w.viewer(w.owner)

	testdb.Must(t, w.svc.UpsertShare(ctx, owner, w.personal, w.stranger, models.PermissionReadonly))
	if _, err := w.svc.Get(ctx, w.viewer(w.stranger), w.personal); err != nil {
		t.Fatalf("after sharing, Get by the new viewer: %v", err)
	}
	testdb.Must(t, w.svc.UpsertShare(ctx, owner, w.personal, w.stranger, models.PermissionEditable))
	shares, err := w.svc.ListShares(ctx, owner, w.personal)
	testdb.Must(t, err)
	found := false
	for _, s := range shares {
		if s.UserID == w.stranger {
			found = s.Permission == models.PermissionEditable && s.Username != ""
		}
	}
	if !found {
		t.Errorf("shares = %+v, want the stranger with editable and a username", shares)
	}
	testdb.Must(t, w.svc.RemoveShare(ctx, owner, w.personal, w.stranger))
	if _, err := w.svc.Get(ctx, w.viewer(w.stranger), w.personal); !errors.Is(err, ErrNotFound) {
		t.Errorf("after unsharing, Get err = %v, want ErrNotFound", err)
	}
}

func TestDBSharingRules(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	owner := w.viewer(w.owner)
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"editable sharer is not the owner", w.svc.UpsertShare(ctx, w.viewer(w.editable), w.personal, w.stranger, "readonly"), ErrForbidden},
		{"stranger", w.svc.UpsertShare(ctx, w.viewer(w.stranger), w.personal, w.stranger, "readonly"), ErrNotFound},
		{"site dashboard", w.svc.UpsertShare(ctx, w.viewer(w.admin), w.siteDash, w.stranger, "readonly"), ErrShareSiteDashboard},
		{"owner", w.svc.UpsertShare(ctx, owner, w.personal, w.owner, "readonly"), ErrShareOwner},
		{"unknown user", w.svc.UpsertShare(ctx, owner, w.personal, uuid.New(), "readonly"), ErrUnknownUser},
		{"bad permission", w.svc.UpsertShare(ctx, owner, w.personal, w.stranger, "admin"), ErrInvalid},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, c.err, c.want)
		}
	}
	if err := w.svc.UpsertShare(ctx, w.viewer(w.admin), w.personal, w.stranger, "readonly"); err != nil {
		t.Errorf("admin sharing a personal dashboard: %v", err)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `db_up; go_docker test ./internal/dashboards/ -run TestDBSharing -v; db_down`
Expected: FAIL — `w.svc.UpsertShare undefined`.

- [ ] **Step 3: Implement**

`backend/internal/dashboards/sharing.go`:

```go
package dashboards

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// ShareView is one share with the recipient's name, for the sharing panel.
type ShareView struct {
	UserID     uuid.UUID `json:"user_id" gorm:"column:user_id"`
	Username   string    `json:"username" gorm:"column:username"`
	Email      string    `json:"email" gorm:"column:email"`
	Permission string    `json:"permission" gorm:"column:permission"`
	CreatedAt  time.Time `json:"created_at" gorm:"column:created_at"`
}

// shareable loads a personal dashboard v may share.
func (s *Service) shareable(ctx context.Context, v Viewer, id uuid.UUID) (*DashboardView, error) {
	vw, err := s.load(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if vw.SiteID != nil {
		return nil, ErrShareSiteDashboard
	}
	if !canShare(v, &vw.Dashboard) {
		return nil, ErrForbidden
	}
	return vw, nil
}

// ListShares returns who a personal dashboard is shared with, oldest first.
func (s *Service) ListShares(ctx context.Context, v Viewer, id uuid.UUID) ([]ShareView, error) {
	if _, err := s.shareable(ctx, v, id); err != nil {
		return nil, err
	}
	var out []ShareView
	err := s.db.WithContext(ctx).Table("dashboard_sharing AS sh").
		Select("sh.user_id, u.username, u.email, sh.permission, sh.created_at").
		Joins("JOIN users u ON u.id = sh.user_id").
		Where("sh.dashboard_id = ?", id).Order("sh.created_at").Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing dashboard shares: %w", err)
	}
	return out, nil
}

// UpsertShare shares a personal dashboard with userID, or changes the
// permission of an existing share.
func (s *Service) UpsertShare(ctx context.Context, v Viewer, id, userID uuid.UUID, permission string) error {
	vw, err := s.shareable(ctx, v, id)
	if err != nil {
		return err
	}
	if permission != models.PermissionReadonly && permission != models.PermissionEditable {
		return invalid("permission must be readonly or editable")
	}
	if vw.OwnerID != nil && *vw.OwnerID == userID {
		return ErrShareOwner
	}
	var n int64
	if err := s.db.WithContext(ctx).Table("users").Where("id = ?", userID).Count(&n).Error; err != nil {
		return fmt.Errorf("checking user: %w", err)
	}
	if n == 0 {
		return ErrUnknownUser
	}
	share := models.DashboardShare{DashboardID: id, UserID: userID, Permission: permission}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dashboard_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"permission"}),
	}).Create(&share).Error
	if err != nil {
		return fmt.Errorf("sharing dashboard: %w", err)
	}
	return nil
}

// RemoveShare stops sharing a personal dashboard with userID. Removing a
// share that does not exist is not an error.
func (s *Service) RemoveShare(ctx context.Context, v Viewer, id, userID uuid.UUID) error {
	if _, err := s.shareable(ctx, v, id); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&models.DashboardShare{}, "dashboard_id = ? AND user_id = ?", id, userID).Error; err != nil {
		return fmt.Errorf("unsharing dashboard: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run to see them pass**

Run: `db_up; go_docker test ./internal/dashboards/ -run TestDBSharing -v; db_down`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/dashboards/sharing.go backend/internal/dashboards/sharing_db_test.go
git commit -m "feat(dashboards): share personal dashboards with other users

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Public links

**Files:**
- Create: `backend/internal/dashboards/links.go`
- Test: `backend/internal/dashboards/links_db_test.go`

**Interfaces:**
- Consumes: `Service.load`, `Service.widgets`, `Registry`, `models.DashboardPublicLink`.
- Produces:
  - `type BroadWidget struct{ ID uuid.UUID; Type, Title string }` (JSON `id`, `type`, `title`)
  - `type LinkView struct{ Link *models.DashboardPublicLink; Broad []BroadWidget }` (JSON `link` — null when none — and `broad_widgets`)
  - `(*Service).PublicLink(ctx, v Viewer, id uuid.UUID) (*LinkView, error)`
  - `(*Service).CreatePublicLink(ctx, v Viewer, id uuid.UUID) (*models.DashboardPublicLink, error)` — creates or regenerates
  - `(*Service).RevokePublicLink(ctx, v Viewer, id uuid.UUID) error` — `ErrNoLink` when none
  - `(*Service).ResolveToken(ctx, token string) (*models.Dashboard, error)` — `ErrNotFound` unless the link exists and its creator is still an admin
  - `type PublicWidget struct{ ID uuid.UUID; Type, Title string; X, Y, W, H int }`, `type PublicDashboard struct{ Name, Description string; Version int; Widgets []PublicWidget }`
  - `(*Service).PublicLayout(ctx, d *models.Dashboard) (*PublicDashboard, error)`
  - `(*Service).PublicWidget(ctx, d *models.Dashboard, widgetID uuid.UUID) (*models.DashboardWidget, error)` and `(*Service).Widget(ctx, v Viewer, dashboardID, widgetID uuid.UUID) (*models.DashboardWidget, *DashboardView, error)`
  - `newToken() (string, error)`

The admin check lives in the route (`RequireAdmin`); these methods also refuse a non-admin viewer so a wiring mistake cannot publish anything.

- [ ] **Step 1: Write the failing tests**

`backend/internal/dashboards/links_db_test.go`:

```go
package dashboards

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBPublicLinkLifecycle(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	admin := w.viewer(w.admin)

	link, err := w.svc.CreatePublicLink(ctx, admin, w.siteDash)
	testdb.Must(t, err)
	if len(link.Token) != 43 {
		t.Errorf("token %q has %d chars, want 43 (32 bytes base64url)", link.Token, len(link.Token))
	}
	d, err := w.svc.ResolveToken(ctx, link.Token)
	testdb.Must(t, err)
	if d.ID != w.siteDash {
		t.Errorf("token resolves to %s, want %s", d.ID, w.siteDash)
	}

	again, err := w.svc.CreatePublicLink(ctx, admin, w.siteDash)
	testdb.Must(t, err)
	if again.Token == link.Token {
		t.Error("regenerating kept the old token")
	}
	if _, err := w.svc.ResolveToken(ctx, link.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("old token after regenerating err = %v, want ErrNotFound", err)
	}

	testdb.Must(t, w.svc.RevokePublicLink(ctx, admin, w.siteDash))
	if _, err := w.svc.ResolveToken(ctx, again.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked token err = %v, want ErrNotFound", err)
	}
	if err := w.svc.RevokePublicLink(ctx, admin, w.siteDash); !errors.Is(err, ErrNoLink) {
		t.Errorf("revoking twice err = %v, want ErrNoLink", err)
	}
}

func TestDBResolveTokenCreatorDemoted(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	link, err := w.svc.CreatePublicLink(ctx, w.viewer(w.admin), w.siteDash)
	testdb.Must(t, err)
	testdb.Exec(t, w.db, `UPDATE users SET is_admin = false, role = 'user' WHERE id = ?`, w.admin)
	if _, err := w.svc.ResolveToken(ctx, link.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("token of a demoted admin err = %v, want ErrNotFound on the very next request", err)
	}
}

func TestDBPublicLinkRefusesNonAdmins(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	if _, err := w.svc.CreatePublicLink(ctx, w.viewer(w.siteRW), w.siteDash); !errors.Is(err, ErrForbidden) {
		t.Errorf("editor creating a link err = %v, want ErrForbidden", err)
	}
	for _, token := range []string{"", "short", "x' OR '1'='1"} {
		if _, err := w.svc.ResolveToken(ctx, token); !errors.Is(err, ErrNotFound) {
			t.Errorf("ResolveToken(%q) err = %v, want ErrNotFound", token, err)
		}
	}
}

func TestDBPublicLayoutHasNoConfigs(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	d, err := w.svc.Get(ctx, w.viewer(w.admin), w.siteDash)
	testdb.Must(t, err)
	layout, err := w.svc.PublicLayout(ctx, &d.Dashboard)
	testdb.Must(t, err)
	if layout.Name != "HQ overview" || len(layout.Widgets) != 1 || layout.Widgets[0].Type != "label" {
		t.Errorf("layout = %+v", layout)
	}
	if _, err := w.svc.PublicWidget(ctx, &d.Dashboard, uuid.New()); !errors.Is(err, ErrWidgetNotFound) {
		t.Errorf("unknown widget err = %v, want ErrWidgetNotFound", err)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `db_up; go_docker test ./internal/dashboards/ -run 'TestDBPublic|TestDBResolveToken' -v; db_down`
Expected: FAIL — `w.svc.CreatePublicLink undefined`.

- [ ] **Step 3: Implement**

`backend/internal/dashboards/links.go`:

```go
package dashboards

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// tokenLen is the length of a base64url-encoded 32-byte token.
const tokenLen = 43

// newToken returns 32 random bytes, base64url without padding (43 chars).
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// BroadWidget is a widget whose rows follow the link creator's own access, so
// a public link to it shows things added later too.
type BroadWidget struct {
	ID    uuid.UUID `json:"id"`
	Type  string    `json:"type"`
	Title string    `json:"title"`
}

// LinkView is what the Public link panel shows: the link (nil when there is
// none) and the widgets with a broad scope.
type LinkView struct {
	Link  *models.DashboardPublicLink `json:"link"`
	Broad []BroadWidget               `json:"broad_widgets"`
}

// adminDashboard loads a dashboard for an admin-only operation.
func (s *Service) adminDashboard(ctx context.Context, v Viewer, id uuid.UUID) (*DashboardView, error) {
	vw, err := s.load(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if vw.level < LevelManage {
		return nil, ErrForbidden
	}
	return vw, nil
}

// PublicLink returns a dashboard's link, if any, and its broad widgets.
func (s *Service) PublicLink(ctx context.Context, v Viewer, id uuid.UUID) (*LinkView, error) {
	if _, err := s.adminDashboard(ctx, v, id); err != nil {
		return nil, err
	}
	out := &LinkView{Broad: []BroadWidget{}}
	var link models.DashboardPublicLink
	err := s.db.WithContext(ctx).First(&link, "dashboard_id = ?", id).Error
	switch {
	case err == nil:
		out.Link = &link
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, fmt.Errorf("loading public link: %w", err)
	}
	ws, err := s.widgets(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, w := range ws {
		if wd, ok := s.registry.Get(w.Type); ok && wd.Subjects([]byte(w.Config)).Broad {
			out.Broad = append(out.Broad, BroadWidget{ID: w.ID, Type: w.Type, Title: w.Title})
		}
	}
	return out, nil
}

// CreatePublicLink gives a dashboard a public link, replacing any existing
// one (the old token stops working at once).
func (s *Service) CreatePublicLink(ctx context.Context, v Viewer, id uuid.UUID) (*models.DashboardPublicLink, error) {
	if _, err := s.adminDashboard(ctx, v, id); err != nil {
		return nil, err
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	link := models.DashboardPublicLink{DashboardID: id, Token: token, CreatedBy: v.UserID, CreatedAt: time.Now().UTC()}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dashboard_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"token", "created_by", "created_at"}),
	}).Create(&link).Error
	if err != nil {
		return nil, fmt.Errorf("creating public link: %w", err)
	}
	return &link, nil
}

// RevokePublicLink deletes a dashboard's public link.
func (s *Service) RevokePublicLink(ctx context.Context, v Viewer, id uuid.UUID) error {
	if _, err := s.adminDashboard(ctx, v, id); err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Delete(&models.DashboardPublicLink{}, "dashboard_id = ?", id)
	if res.Error != nil {
		return fmt.Errorf("revoking public link: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNoLink
	}
	return nil
}

// ResolveToken returns the dashboard a public token opens. It answers
// ErrNotFound unless the link exists and the admin who made it is still an
// admin; this runs on every public request, so a demotion takes effect at once.
func (s *Service) ResolveToken(ctx context.Context, token string) (*models.Dashboard, error) {
	if len(token) != tokenLen {
		return nil, ErrNotFound
	}
	var rows []models.Dashboard
	err := s.db.WithContext(ctx).Table("dashboard_public_links AS pl").Select("d.*").
		Joins("JOIN dashboards d ON d.id = pl.dashboard_id").
		Joins("JOIN users u ON u.id = pl.created_by").
		Where("pl.token = ? AND u.is_admin", token).Limit(1).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("resolving public token: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// PublicWidget is a widget as the public layout shows it: no config.
type PublicWidget struct {
	ID    uuid.UUID `json:"id"`
	Type  string    `json:"type"`
	Title string    `json:"title"`
	X     int       `json:"x"`
	Y     int       `json:"y"`
	W     int       `json:"w"`
	H     int       `json:"h"`
}

// PublicDashboard is the layout a public link serves.
type PublicDashboard struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Version     int            `json:"version"`
	Widgets     []PublicWidget `json:"widgets"`
}

// PublicLayout returns d's name, layout and titles, without configs or ids
// of anything but the widgets themselves.
func (s *Service) PublicLayout(ctx context.Context, d *models.Dashboard) (*PublicDashboard, error) {
	ws, err := s.widgets(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	out := &PublicDashboard{Name: d.Name, Description: d.Description, Version: d.Version, Widgets: make([]PublicWidget, 0, len(ws))}
	for _, w := range ws {
		out.Widgets = append(out.Widgets, PublicWidget{ID: w.ID, Type: w.Type, Title: w.Title, X: w.X, Y: w.Y, W: w.W, H: w.H})
	}
	return out, nil
}

// PublicWidget returns one widget of d.
func (s *Service) PublicWidget(ctx context.Context, d *models.Dashboard, widgetID uuid.UUID) (*models.DashboardWidget, error) {
	var w models.DashboardWidget
	err := s.db.WithContext(ctx).First(&w, "id = ? AND dashboard_id = ?", widgetID, d.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWidgetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading widget: %w", err)
	}
	return &w, nil
}

// Widget returns one widget of a dashboard v can see, and the dashboard.
func (s *Service) Widget(ctx context.Context, v Viewer, dashboardID, widgetID uuid.UUID) (*models.DashboardWidget, *DashboardView, error) {
	vw, err := s.load(ctx, v, dashboardID)
	if err != nil {
		return nil, nil, err
	}
	w, err := s.PublicWidget(ctx, &vw.Dashboard, widgetID)
	if err != nil {
		return nil, nil, err
	}
	return w, vw, nil
}
```

- [ ] **Step 4: Run to see them pass**

Run: `db_up; go_docker test ./internal/dashboards/ -run 'TestDBPublic|TestDBResolveToken' -v; db_down`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/dashboards/links.go backend/internal/dashboards/links_db_test.go
git commit -m "feat(dashboards): admin-only public links that die with the creator's admin rights

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: The resolver and the public cache

**Files:**
- Create: `backend/internal/dashboards/resolver.go`
- Modify: `backend/go.mod`, `backend/go.sum` (`golang.org/x/sync` becomes a direct dependency)
- Test: `backend/internal/dashboards/resolver_test.go`, `backend/internal/dashboards/resolver_db_test.go`

**Interfaces:**
- Consumes: `Registry`, `Checker`, `Filter`, `ErrNoData`, `models.DashboardWidget`, `models.Dashboard`, `ValidRange`.
- Produces:
  - `type Response struct{ State string; Data any; Hidden, Removed int; RefreshSeconds int; GeneratedAt time.Time }` (JSON `state`, `data` (omitempty), `hidden`, `removed`, `refresh_seconds`, `generated_at`)
  - `type Resolver`; `NewResolver(registry *Registry, checker Checker) *Resolver`
  - `(*Resolver).Resolve(ctx, v Viewer, w *models.DashboardWidget, override string) (*Response, error)`
  - `(*Resolver).ResolvePublic(ctx, d *models.Dashboard, w *models.DashboardWidget) (*Response, error)` — cached 15 s by (dashboard id, version, widget id)
  - `(*Resolver).Preview(ctx, v Viewer, typ string, cfg json.RawMessage, override string) (*Response, error)` — validates first; a bad config is a `*FieldError`
  - `const publicCacheTTL = 15 * time.Second`

- [ ] **Step 1: Write the failing unit tests (fake checker and widget)**

`backend/internal/dashboards/resolver_test.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// openChecker: every subject exists and is visible.
type openChecker struct{}

func (openChecker) DeviceSites(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	out := map[uuid.UUID]uuid.UUID{}
	for _, id := range ids {
		out[id] = uuid.Nil
	}
	return out, nil
}
func (openChecker) SiteLevel(context.Context, Viewer, uuid.UUID) (services.SiteAccessLevel, error) {
	return services.SiteAccessAdmin, nil
}
func (openChecker) ExistingMonitors(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}
func (openChecker) MonitorVisible(context.Context, Viewer, uuid.UUID) (bool, error) { return true, nil }
func (openChecker) ExistingAgents(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	return openChecker{}.ExistingMonitors(ctx, ids)
}

// countingWidget counts Resolve calls and returns what it was told to.
type countingWidget struct {
	calls *atomic.Int32
	err   error
	devs  []uuid.UUID
}

func (countingWidget) Type() string { return "counting" }
func (countingWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if string(raw) == `{"bad":true}` {
		return nil, fieldErr("bad", "is not allowed")
	}
	return raw, nil
}
func (w countingWidget) Subjects(json.RawMessage) Subjects { return Subjects{Devices: w.devs} }
func (w countingWidget) Resolve(_ context.Context, _ json.RawMessage, in ResolveInput) (any, error) {
	w.calls.Add(1)
	if w.err != nil {
		return nil, w.err
	}
	return map[string]any{"override": in.Override, "public": in.Viewer.Public}, nil
}
func (countingWidget) Refresh(_ json.RawMessage, override string) time.Duration {
	if override == "7d" {
		return 5 * time.Minute
	}
	return time.Minute
}

func newCountingResolver(err error) (*Resolver, *atomic.Int32) {
	calls := &atomic.Int32{}
	return NewResolver(NewRegistry(countingWidget{calls: calls, err: err}), openChecker{}), calls
}

func TestResolveStatesAndRefresh(t *testing.T) {
	ctx := context.Background()
	r, _ := newCountingResolver(nil)
	w := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}
	resp, err := r.Resolve(ctx, Viewer{UserID: uuid.New()}, w, "7d")
	if err != nil {
		t.Fatal(err)
	}
	if resp.State != StateOK || resp.RefreshSeconds != 300 || resp.Data.(map[string]any)["override"] != "7d" {
		t.Errorf("resp = %+v", resp)
	}

	r, _ = newCountingResolver(ErrNoData)
	resp, err = r.Resolve(ctx, Viewer{}, w, "")
	if err != nil || resp.State != StateNoData || resp.Data != nil {
		t.Errorf("ErrNoData: resp = %+v err = %v, want state no_data", resp, err)
	}

	boom := errors.New("boom")
	r, _ = newCountingResolver(boom)
	if _, err := r.Resolve(ctx, Viewer{}, w, ""); !errors.Is(err, boom) {
		t.Errorf("a real failure err = %v, want it returned (500)", err)
	}

	unknown := &models.DashboardWidget{ID: uuid.New(), Type: "gone", Config: models.RawJSON(`{}`)}
	resp, err = r.Resolve(ctx, Viewer{}, unknown, "")
	if err != nil || resp.State != StateNoData {
		t.Errorf("a widget whose type was removed: resp = %+v err = %v, want no_data", resp, err)
	}
}

func TestPublicCacheKeyedByVersion(t *testing.T) {
	ctx := context.Background()
	r, calls := newCountingResolver(nil)
	d := &models.Dashboard{ID: uuid.New(), Version: 4}
	w := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}
	for i := 0; i < 3; i++ {
		resp, err := r.ResolvePublic(ctx, d, w)
		if err != nil || resp.Data.(map[string]any)["public"] != true {
			t.Fatalf("public resolve = %+v, %v", resp, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("three public requests resolved %d times, want 1 (cached)", calls.Load())
	}
	d.Version = 5 // the dashboard was edited
	if _, err := r.ResolvePublic(ctx, d, w); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("after an edit the cache answered with old data (%d resolves, want 2)", calls.Load())
	}
}

func TestPublicCacheExpires(t *testing.T) {
	ctx := context.Background()
	r, calls := newCountingResolver(nil)
	now := time.Now()
	r.now = func() time.Time { return now }
	d := &models.Dashboard{ID: uuid.New(), Version: 1}
	w := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}
	_, _ = r.ResolvePublic(ctx, d, w)
	now = now.Add(publicCacheTTL + time.Second)
	_, _ = r.ResolvePublic(ctx, d, w)
	if calls.Load() != 2 {
		t.Errorf("after the TTL: %d resolves, want 2", calls.Load())
	}
}

func TestPreviewValidatesFirst(t *testing.T) {
	r, calls := newCountingResolver(nil)
	_, err := r.Preview(context.Background(), Viewer{}, "counting", json.RawMessage(`{"bad":true}`), "")
	var fe *FieldError
	if !errors.As(err, &fe) || calls.Load() != 0 {
		t.Errorf("bad preview err = %v, calls = %d; want a FieldError and no resolve", err, calls.Load())
	}
	if _, err := r.Preview(context.Background(), Viewer{}, "nope", json.RawMessage(`{}`), ""); err == nil {
		t.Error("previewing an unknown type succeeded")
	}
}
```

- [ ] **Step 2: Write the failing DB test (Review Focus 1)**

`backend/internal/dashboards/resolver_db_test.go`:

```go
package dashboards

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBResolveRemovedSubject(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	member := testdb.NewUser(t, db, false)
	site := newSite(t, db, "HQ")
	shareSite(t, db, site, member, models.PermissionReadonly)
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	sites := services.NewSiteService(db)
	calls := &atomic.Int32{}
	r := NewResolver(NewRegistry(countingWidget{calls: calls, devs: []uuid.UUID{dev}}),
		NewDBChecker(db, sites, services.NewMonitorService(db)))
	w := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}

	resp, err := r.Resolve(ctx, Viewer{UserID: member}, w, "")
	testdb.Must(t, err)
	if resp.State != StateOK {
		t.Fatalf("before the delete: state %s, want ok", resp.State)
	}
	testdb.Exec(t, db, `DELETE FROM devices WHERE id = ?`, dev)
	resp, err = r.Resolve(ctx, Viewer{UserID: member}, w, "")
	testdb.Must(t, err)
	if resp.State != StateRemoved || resp.Removed != 1 || calls.Load() != 1 {
		t.Errorf("after the delete: %+v (resolves %d); want removed, 1 removed, and no second resolve", resp, calls.Load())
	}
}

func TestDBResolveNoAccess(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	member := testdb.NewUser(t, db, false)
	site := newSite(t, db, "Elsewhere")
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	sites := services.NewSiteService(db)
	r := NewResolver(NewRegistry(countingWidget{calls: &atomic.Int32{}, devs: []uuid.UUID{dev}}),
		NewDBChecker(db, sites, services.NewMonitorService(db)))
	w := &models.DashboardWidget{ID: uuid.New(), Type: "counting", Config: models.RawJSON(`{}`)}
	resp, err := r.Resolve(ctx, Viewer{UserID: member}, w, "")
	testdb.Must(t, err)
	if resp.State != StateNoAccess || resp.Hidden != 1 || resp.Data != nil {
		t.Errorf("resp = %+v, want no_access with hidden 1 and no data", resp)
	}
}
```

- [ ] **Step 3: Run to see them fail**

Run: `go_docker test ./internal/dashboards/ -run 'TestResolve|TestPublicCache|TestPreview'`
Expected: FAIL — `undefined: NewResolver`.

- [ ] **Step 4: Implement**

`backend/internal/dashboards/resolver.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// publicCacheTTL is how long a public widget response is reused, so several
// TVs on one link cost one query per widget per 15 s (spec §4).
const publicCacheTTL = 15 * time.Second

// Response is one widget's data, as the data routes return it. Expected
// states (no_access, removed, no_data) are 200s.
type Response struct {
	State          string    `json:"state"`
	Data           any       `json:"data,omitempty"`
	Hidden         int       `json:"hidden"`
	Removed        int       `json:"removed"`
	RefreshSeconds int       `json:"refresh_seconds"`
	GeneratedAt    time.Time `json:"generated_at"`
}

type cacheKey struct {
	dashboard uuid.UUID
	version   int
	widget    uuid.UUID
}

type cacheEntry struct {
	resp    *Response
	expires time.Time
}

// Resolver turns a saved widget into its data for one viewer.
type Resolver struct {
	registry *Registry
	checker  Checker
	now      func() time.Time

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
	group singleflight.Group
}

// NewResolver builds a resolver over registry, checking subjects with checker.
func NewResolver(registry *Registry, checker Checker) *Resolver {
	return &Resolver{registry: registry, checker: checker, now: time.Now, cache: map[cacheKey]cacheEntry{}}
}

// Resolve loads w's data as v sees it. override is a range key from the
// dashboard's range picker ("" for none); the caller validates it.
func (r *Resolver) Resolve(ctx context.Context, v Viewer, w *models.DashboardWidget, override string) (*Response, error) {
	return r.resolve(ctx, v, w.Type, json.RawMessage(w.Config), override)
}

// Preview resolves an unsaved widget for its editor, after validating it.
// A bad config is a *FieldError; an unknown type is an error too.
func (r *Resolver) Preview(ctx context.Context, v Viewer, typ string, cfg json.RawMessage, override string) (*Response, error) {
	wd, ok := r.registry.Get(typ)
	if !ok {
		return nil, fieldErr("type", fmt.Sprintf("%q is not a widget type", typ))
	}
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}
	clean, err := wd.Validate(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return r.resolve(ctx, v, typ, clean, override)
}

// ResolvePublic loads w's data for d's public link, with the public trim,
// reusing a response for publicCacheTTL. The key includes d's version, so an
// edit is never answered from the cache; concurrent identical requests share
// one resolve. ResolveToken runs before this on every request, so a revoked
// link or demoted creator is refused even while an entry is cached.
func (r *Resolver) ResolvePublic(ctx context.Context, d *models.Dashboard, w *models.DashboardWidget) (*Response, error) {
	key := cacheKey{dashboard: d.ID, version: d.Version, widget: w.ID}
	now := r.now()
	r.mu.Lock()
	if e, ok := r.cache[key]; ok && now.Before(e.expires) {
		r.mu.Unlock()
		return e.resp, nil
	}
	r.mu.Unlock()

	res, err, _ := r.group.Do(fmt.Sprintf("%s/%d/%s", key.dashboard, key.version, key.widget), func() (any, error) {
		resp, err := r.resolve(ctx, PublicViewer, w.Type, json.RawMessage(w.Config), "")
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.sweepLocked(now)
		r.cache[key] = cacheEntry{resp: resp, expires: now.Add(publicCacheTTL)}
		r.mu.Unlock()
		return resp, nil
	})
	if err != nil {
		return nil, err
	}
	return res.(*Response), nil
}

// sweepLocked drops expired entries; called with mu held.
func (r *Resolver) sweepLocked(now time.Time) {
	for k, e := range r.cache {
		if !now.Before(e.expires) {
			delete(r.cache, k)
		}
	}
}

func (r *Resolver) resolve(ctx context.Context, v Viewer, typ string, cfg json.RawMessage, override string) (*Response, error) {
	now := r.now()
	resp := &Response{GeneratedAt: now.UTC()}
	wd, ok := r.registry.Get(typ)
	if !ok {
		// A type removed in a later release: show "No data", not an error.
		resp.State = StateNoData
		return resp, nil
	}
	resp.RefreshSeconds = int(wd.Refresh(cfg, override).Seconds())
	f, err := Filter(ctx, r.checker, v, wd.Subjects(cfg))
	if err != nil {
		return nil, err
	}
	resp.Hidden, resp.Removed = f.Hidden, f.Removed
	if st := f.State(); st != "" {
		resp.State = st
		return resp, nil
	}
	data, err := wd.Resolve(ctx, cfg, ResolveInput{Visible: f.Visible, Viewer: v, Override: override, Now: now})
	if errors.Is(err, ErrNoData) {
		resp.State = StateNoData
		return resp, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolving %s widget: %w", typ, err)
	}
	resp.State = StateOK
	resp.Data = data
	return resp, nil
}
```

Then make the dependency direct:

Run: `go_docker mod tidy`
Expected: `backend/go.mod` lists `golang.org/x/sync` without `// indirect`.

- [ ] **Step 5: Run to see them pass**

Run: `go_docker test ./internal/dashboards/ -run 'TestResolve|TestPublicCache|TestPreview' -v`
Then: `db_up; go_docker test ./internal/dashboards/ -run TestDBResolve -v; db_down`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/dashboards/resolver.go backend/internal/dashboards/resolver_test.go backend/internal/dashboards/resolver_db_test.go backend/go.mod backend/go.sum
git commit -m "feat(dashboards): resolve widget data per viewer, with a short public cache

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 10: Dashboard HTTP routes, public routes and wiring

**Files:**
- Create: `backend/internal/dashboards/deps.go` (starts with the label widget only; later tasks add fields and widgets)
- Create: `backend/internal/api/dashboard_handler.go`
- Create: `backend/internal/api/dashboard_public_handler.go`
- Modify: `backend/cmd/sentinel/main.go`
- Test: `backend/internal/api/dashboard_handler_test.go`

**Interfaces:**
- Consumes: every `dashboards.Service` method from Tasks 5–8, `dashboards.Resolver` (Task 9), `dashboards.ValidRange`, `RequireAdmin`, `NewRateLimiter`, `ByIP`, `respondError`, `respondSuccess`, `respondInternal`, `respondAuthError`, `GetUserFromContext`, `actorFrom`, `auditRecorder`, `adminChecker`.
- Produces:
  - `dashboards.Deps` (empty struct for now) and `dashboards.NewDefaultRegistry(d Deps) *Registry`
  - `api.RegisterDashboardRoutes(rg *gin.RouterGroup, store dashboardStore, resolver widgetResolver, audit auditRecorder, users adminChecker)`
  - `api.RegisterPublicDashboardRoutes(router *gin.Engine, store dashboardStore, resolver widgetResolver)`
  - Routes (all under `/api/v1`):
    - `GET /dashboards?site_id=`, `POST /dashboards`, `GET|PUT|DELETE /dashboards/:id`
    - `GET /dashboards/:id/shares`, `PUT|DELETE /dashboards/:id/shares/:user_id` (PUT body `{"permission":"readonly|editable"}`)
    - `GET /dashboards/:id/widgets/:wid/data?range=`, `POST /dashboards/:id/widgets/preview` (body `{"type","config","range"}`)
    - `GET|POST|DELETE /dashboards/:id/public-link` (admin)
    - `GET /public/dashboards/:token`, `GET /public/dashboards/:token/widgets/:wid/data` (no login, 600/min/IP)
  - Error JSON: 409 adds `"current_version"`; a widget 400 adds `"widget_index"` and `"field"`; a preview 400 adds `"field"`.

- [ ] **Step 1: Start the default registry**

`backend/internal/dashboards/deps.go`:

```go
package dashboards

// Deps is what the widgets read their data from. Each widget takes only the
// parts it needs, as interfaces, so widgets are tested without the real
// services. main.go fills it in.
type Deps struct{}

// NewDefaultRegistry registers every widget type Sentinel ships, in the order
// the editor lists them.
func NewDefaultRegistry(d Deps) *Registry {
	return NewRegistry(
		labelWidget{},
	)
}
```

- [ ] **Step 2: Write the failing handler tests**

`backend/internal/api/dashboard_handler_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/dashboards"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// fakeDashboards is an in-memory dashboardStore. err, when set, is what every
// call returns; mutations counts calls that would change data.
type fakeDashboards struct {
	err         error
	tokenErr    error
	mutations   int
	lastViewer  dashboards.Viewer
	dashboardID uuid.UUID
}

func (f *fakeDashboards) detail() *dashboards.DashboardDetail {
	return &dashboards.DashboardDetail{DashboardView: dashboards.DashboardView{
		Dashboard: models.Dashboard{ID: f.dashboardID, Name: "HQ", Version: 1}, Access: "edit", CanEdit: true}}
}
func (f *fakeDashboards) List(_ context.Context, v dashboards.Viewer, _ *uuid.UUID) ([]dashboards.DashboardView, error) {
	f.lastViewer = v
	return []dashboards.DashboardView{f.detail().DashboardView}, f.err
}
func (f *fakeDashboards) Get(_ context.Context, v dashboards.Viewer, _ uuid.UUID) (*dashboards.DashboardDetail, error) {
	f.lastViewer = v
	if f.err != nil {
		return nil, f.err
	}
	return f.detail(), nil
}
func (f *fakeDashboards) Create(context.Context, dashboards.Viewer, dashboards.CreateInput) (*dashboards.DashboardDetail, error) {
	f.mutations++
	if f.err != nil {
		return nil, f.err
	}
	return f.detail(), nil
}
func (f *fakeDashboards) Save(context.Context, dashboards.Viewer, uuid.UUID, dashboards.SaveInput) (*dashboards.DashboardDetail, error) {
	f.mutations++
	if f.err != nil {
		return nil, f.err
	}
	return f.detail(), nil
}
func (f *fakeDashboards) Delete(context.Context, dashboards.Viewer, uuid.UUID) (*models.Dashboard, error) {
	f.mutations++
	if f.err != nil {
		return nil, f.err
	}
	return &models.Dashboard{ID: f.dashboardID, Name: "HQ"}, nil
}
func (f *fakeDashboards) ListShares(context.Context, dashboards.Viewer, uuid.UUID) ([]dashboards.ShareView, error) {
	return nil, f.err
}
func (f *fakeDashboards) UpsertShare(context.Context, dashboards.Viewer, uuid.UUID, uuid.UUID, string) error {
	f.mutations++
	return f.err
}
func (f *fakeDashboards) RemoveShare(context.Context, dashboards.Viewer, uuid.UUID, uuid.UUID) error {
	f.mutations++
	return f.err
}
func (f *fakeDashboards) PublicLink(context.Context, dashboards.Viewer, uuid.UUID) (*dashboards.LinkView, error) {
	return &dashboards.LinkView{}, f.err
}
func (f *fakeDashboards) CreatePublicLink(context.Context, dashboards.Viewer, uuid.UUID) (*models.DashboardPublicLink, error) {
	f.mutations++
	return &models.DashboardPublicLink{Token: strings.Repeat("t", 43)}, f.err
}
func (f *fakeDashboards) RevokePublicLink(context.Context, dashboards.Viewer, uuid.UUID) error {
	f.mutations++
	return f.err
}
func (f *fakeDashboards) Widget(_ context.Context, v dashboards.Viewer, _, wid uuid.UUID) (*models.DashboardWidget, *dashboards.DashboardView, error) {
	f.lastViewer = v
	if f.err != nil {
		return nil, nil, f.err
	}
	return &models.DashboardWidget{ID: wid, Type: "label"}, &f.detail().DashboardView, nil
}
func (f *fakeDashboards) ResolveToken(context.Context, string) (*models.Dashboard, error) {
	if f.tokenErr != nil {
		return nil, f.tokenErr
	}
	return &models.Dashboard{ID: f.dashboardID, Version: 1}, nil
}
func (f *fakeDashboards) PublicLayout(context.Context, *models.Dashboard) (*dashboards.PublicDashboard, error) {
	return &dashboards.PublicDashboard{Name: "HQ"}, nil
}
func (f *fakeDashboards) PublicWidget(_ context.Context, _ *models.Dashboard, wid uuid.UUID) (*models.DashboardWidget, error) {
	return &models.DashboardWidget{ID: wid, Type: "label"}, nil
}

type fakeWidgetResolver struct {
	calls         int
	lastOverride  string
	publicCalls   int
	previewErr    error
}

func (r *fakeWidgetResolver) Resolve(_ context.Context, _ dashboards.Viewer, _ *models.DashboardWidget, override string) (*dashboards.Response, error) {
	r.calls++
	r.lastOverride = override
	return &dashboards.Response{State: dashboards.StateOK}, nil
}
func (r *fakeWidgetResolver) ResolvePublic(context.Context, *models.Dashboard, *models.DashboardWidget) (*dashboards.Response, error) {
	r.publicCalls++
	return &dashboards.Response{State: dashboards.StateOK}, nil
}
func (r *fakeWidgetResolver) Preview(context.Context, dashboards.Viewer, string, json.RawMessage, string) (*dashboards.Response, error) {
	if r.previewErr != nil {
		return nil, r.previewErr
	}
	return &dashboards.Response{State: dashboards.StateOK}, nil
}

func dashboardRouter(store *fakeDashboards, res *fakeWidgetResolver, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterPublicDashboardRoutes(r, store, res)
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	RegisterDashboardRoutes(v1, store, res, &fakeAudit{}, stubUsers{user: &models.User{IsAdmin: isAdmin}})
	return r
}

func TestDashboardErrorStatuses(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		err    error
		status int
		extra  string
	}{
		{dashboards.ErrNotFound, http.StatusNotFound, ""},
		{dashboards.ErrPublished, http.StatusForbidden, ""},
		{dashboards.ErrForbidden, http.StatusForbidden, ""},
		{&dashboards.VersionConflictError{Current: 7}, http.StatusConflict, `"current_version":7`},
		{&dashboards.WidgetError{Index: 2, Field: "metrics", Msg: "needs 1 to 10"}, http.StatusBadRequest, `"widget_index":2`},
		{fmt.Errorf("%w: name is required", dashboards.ErrInvalid), http.StatusBadRequest, ""},
	}
	for _, c := range cases {
		store := &fakeDashboards{err: c.err, dashboardID: id}
		w := do(dashboardRouter(store, &fakeWidgetResolver{}, false), http.MethodPut, "/api/v1/dashboards/"+id.String(), map[string]any{"version": 1, "name": "x"})
		if w.Code != c.status {
			t.Errorf("%v: status %d, want %d", c.err, w.Code, c.status)
		}
		if c.extra != "" && !strings.Contains(w.Body.String(), c.extra) {
			t.Errorf("%v: body %s lacks %s", c.err, w.Body.String(), c.extra)
		}
	}
}

func TestDashboardInternalErrorsAreGeneric(t *testing.T) {
	store := &fakeDashboards{err: errors.New(`pq: relation "secret_table" does not exist`)}
	w := do(dashboardRouter(store, &fakeWidgetResolver{}, false), http.MethodGet, "/api/v1/dashboards", nil)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "secret_table") {
		t.Errorf("status %d body %s: want a 500 without the database's text", w.Code, w.Body.String())
	}
}

func TestDashboardPublicLinkRoutesAreAdminOnly(t *testing.T) {
	id := uuid.New().String()
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		store := &fakeDashboards{}
		w := do(dashboardRouter(store, &fakeWidgetResolver{}, false), method, "/api/v1/dashboards/"+id+"/public-link", nil)
		if w.Code != http.StatusForbidden || store.mutations != 0 {
			t.Errorf("%s by a member: status %d, mutations %d; want 403 and none", method, w.Code, store.mutations)
		}
	}
	store := &fakeDashboards{}
	w := do(dashboardRouter(store, &fakeWidgetResolver{}, true), http.MethodPost, "/api/v1/dashboards/"+id+"/public-link", nil)
	if w.Code != http.StatusOK || store.mutations != 1 {
		t.Errorf("POST by an admin: status %d, mutations %d", w.Code, store.mutations)
	}
}

func TestWidgetDataRange(t *testing.T) {
	id, wid := uuid.New().String(), uuid.New().String()
	res := &fakeWidgetResolver{}
	r := dashboardRouter(&fakeDashboards{}, res, false)
	if w := do(r, http.MethodGet, "/api/v1/dashboards/"+id+"/widgets/"+wid+"/data?range=2h", nil); w.Code != http.StatusBadRequest {
		t.Errorf("range=2h: status %d, want 400", w.Code)
	}
	if w := do(r, http.MethodGet, "/api/v1/dashboards/"+id+"/widgets/"+wid+"/data?range=7d", nil); w.Code != http.StatusOK || res.lastOverride != "7d" {
		t.Errorf("range=7d: status %d, override %q", w.Code, res.lastOverride)
	}
	if w := do(r, http.MethodGet, "/api/v1/dashboards/"+id+"/widgets/not-a-uuid/data", nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad widget id: status %d, want 400", w.Code)
	}
}

func TestPreviewFieldErrorIs400(t *testing.T) {
	res := &fakeWidgetResolver{previewErr: &dashboards.FieldError{Field: "metrics", Msg: "needs 1 to 10"}}
	w := do(dashboardRouter(&fakeDashboards{}, res, false), http.MethodPost, "/api/v1/dashboards/"+uuid.NewString()+"/widgets/preview",
		map[string]any{"type": "timeseries", "config": map[string]any{}})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"field":"metrics"`) {
		t.Errorf("status %d body %s, want 400 naming the field", w.Code, w.Body.String())
	}
}

func TestPublicWidgetDataChecksTokenEveryRequest(t *testing.T) {
	store := &fakeDashboards{}
	res := &fakeWidgetResolver{}
	r := dashboardRouter(store, res, false)
	path := "/api/v1/public/dashboards/" + strings.Repeat("t", 43) + "/widgets/" + uuid.NewString() + "/data"
	if w := do(r, http.MethodGet, path, nil); w.Code != http.StatusOK {
		t.Fatalf("first request: status %d", w.Code)
	}
	store.tokenErr = dashboards.ErrNotFound // revoked, or the creator was demoted
	w := do(r, http.MethodGet, path, nil)
	if w.Code != http.StatusNotFound || res.publicCalls != 1 {
		t.Errorf("after revoking: status %d, resolves %d; want 404 without resolving", w.Code, res.publicCalls)
	}
	if !strings.Contains(w.Body.String(), "no longer available") {
		t.Errorf("body %s, want the link-unavailable message", w.Body.String())
	}
}
```

- [ ] **Step 3: Run to see them fail**

Run: `go_docker test ./internal/api/ -run 'TestDashboard|TestWidgetData|TestPreviewFieldError|TestPublicWidgetData'`
Expected: FAIL — `undefined: RegisterDashboardRoutes`.

- [ ] **Step 4: Write the logged-in handlers**

`backend/internal/api/dashboard_handler.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/dashboards"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// dashboardStore is what the dashboard handlers need from dashboards.Service.
type dashboardStore interface {
	List(ctx context.Context, v dashboards.Viewer, siteID *uuid.UUID) ([]dashboards.DashboardView, error)
	Get(ctx context.Context, v dashboards.Viewer, id uuid.UUID) (*dashboards.DashboardDetail, error)
	Create(ctx context.Context, v dashboards.Viewer, in dashboards.CreateInput) (*dashboards.DashboardDetail, error)
	Save(ctx context.Context, v dashboards.Viewer, id uuid.UUID, in dashboards.SaveInput) (*dashboards.DashboardDetail, error)
	Delete(ctx context.Context, v dashboards.Viewer, id uuid.UUID) (*models.Dashboard, error)
	ListShares(ctx context.Context, v dashboards.Viewer, id uuid.UUID) ([]dashboards.ShareView, error)
	UpsertShare(ctx context.Context, v dashboards.Viewer, id, userID uuid.UUID, permission string) error
	RemoveShare(ctx context.Context, v dashboards.Viewer, id, userID uuid.UUID) error
	PublicLink(ctx context.Context, v dashboards.Viewer, id uuid.UUID) (*dashboards.LinkView, error)
	CreatePublicLink(ctx context.Context, v dashboards.Viewer, id uuid.UUID) (*models.DashboardPublicLink, error)
	RevokePublicLink(ctx context.Context, v dashboards.Viewer, id uuid.UUID) error
	Widget(ctx context.Context, v dashboards.Viewer, dashboardID, widgetID uuid.UUID) (*models.DashboardWidget, *dashboards.DashboardView, error)
	ResolveToken(ctx context.Context, token string) (*models.Dashboard, error)
	PublicLayout(ctx context.Context, d *models.Dashboard) (*dashboards.PublicDashboard, error)
	PublicWidget(ctx context.Context, d *models.Dashboard, widgetID uuid.UUID) (*models.DashboardWidget, error)
}

// widgetResolver is what the data routes need from dashboards.Resolver.
type widgetResolver interface {
	Resolve(ctx context.Context, v dashboards.Viewer, w *models.DashboardWidget, override string) (*dashboards.Response, error)
	ResolvePublic(ctx context.Context, d *models.Dashboard, w *models.DashboardWidget) (*dashboards.Response, error)
	Preview(ctx context.Context, v dashboards.Viewer, typ string, cfg json.RawMessage, override string) (*dashboards.Response, error)
}

// RegisterDashboardRoutes mounts /dashboards. Reading and editing are open to
// signed-in users and decided per dashboard by the service; public links are
// admin-only and re-checked against the database by RequireAdmin.
func RegisterDashboardRoutes(rg *gin.RouterGroup, store dashboardStore, resolver widgetResolver, audit auditRecorder, users adminChecker) {
	g := rg.Group("/dashboards")
	g.GET("", listDashboardsHandler(store))
	g.POST("", createDashboardHandler(store, audit))
	g.GET("/:id", getDashboardHandler(store))
	g.PUT("/:id", saveDashboardHandler(store, audit))
	g.DELETE("/:id", deleteDashboardHandler(store, audit))
	g.GET("/:id/shares", listDashboardSharesHandler(store))
	g.PUT("/:id/shares/:user_id", shareDashboardHandler(store, audit))
	g.DELETE("/:id/shares/:user_id", unshareDashboardHandler(store, audit))
	g.GET("/:id/widgets/:wid/data", widgetDataHandler(store, resolver))
	g.POST("/:id/widgets/preview", widgetPreviewHandler(store, resolver))

	admin := g.Group("", RequireAdmin(users))
	admin.GET("/:id/public-link", getPublicLinkHandler(store))
	admin.POST("/:id/public-link", createPublicLinkHandler(store, audit))
	admin.DELETE("/:id/public-link", revokePublicLinkHandler(store, audit))
}

// dashboardViewer is the signed-in caller as a dashboards.Viewer.
func dashboardViewer(c *gin.Context) (dashboards.Viewer, bool) {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondAuthError(c, http.StatusUnauthorized, "authentication required")
		return dashboards.Viewer{}, false
	}
	return dashboards.Viewer{UserID: userID, IsAdmin: isAdmin}, true
}

// parseUUIDParam reads a UUID path parameter, answering 400 when it is not one.
func parseUUIDParam(c *gin.Context, name, what string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid "+what+" id")
		return uuid.Nil, false
	}
	return id, true
}

// respondDashboardError maps dashboards errors to responses. 404 covers
// "missing" and "not yours" alike.
func respondDashboardError(c *gin.Context, op string, err error) {
	var vc *dashboards.VersionConflictError
	var we *dashboards.WidgetError
	var fe *dashboards.FieldError
	switch {
	case errors.Is(err, dashboards.ErrNotFound), errors.Is(err, dashboards.ErrSiteNotFound),
		errors.Is(err, dashboards.ErrWidgetNotFound), errors.Is(err, dashboards.ErrNoLink):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, dashboards.ErrForbidden), errors.Is(err, dashboards.ErrPublished):
		respondError(c, http.StatusForbidden, err.Error())
	case errors.As(err, &vc):
		c.JSON(http.StatusConflict, gin.H{"success": false, "error": vc.Error(), "current_version": vc.Current})
	case errors.As(err, &we):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": we.Error(), "widget_index": we.Index, "field": we.Field})
	case errors.As(err, &fe):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": fe.Error(), "field": fe.Field})
	case errors.Is(err, dashboards.ErrInvalid), errors.Is(err, dashboards.ErrShareSiteDashboard),
		errors.Is(err, dashboards.ErrShareOwner), errors.Is(err, dashboards.ErrUnknownUser):
		respondError(c, http.StatusBadRequest, err.Error())
	default:
		respondInternal(c, op, err)
	}
}

func dashboardSummary(d *models.Dashboard) map[string]any {
	return map[string]any{"name": d.Name, "site_id": d.SiteID, "owner_id": d.OwnerID, "version": d.Version}
}

func listDashboardsHandler(store dashboardStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		var siteID *uuid.UUID
		if raw := c.Query("site_id"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				respondError(c, http.StatusBadRequest, "site_id must be a UUID")
				return
			}
			siteID = &id
		}
		list, err := store.List(c.Request.Context(), v, siteID)
		if err != nil {
			respondDashboardError(c, "listDashboards", err)
			return
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func createDashboardHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		var in dashboards.CreateInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		d, err := store.Create(c.Request.Context(), v, in)
		if err != nil {
			respondDashboardError(c, "createDashboard", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardCreated, models.ResourceDashboard,
			&d.ID, models.AuditChanges{Summary: dashboardSummary(&d.Dashboard)})
		respondSuccess(c, http.StatusCreated, d)
	}
}

func getDashboardHandler(store dashboardStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		d, err := store.Get(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "getDashboard", err)
			return
		}
		respondSuccess(c, http.StatusOK, d)
	}
}

func saveDashboardHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		var in dashboards.SaveInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		d, err := store.Save(c.Request.Context(), v, id, in)
		if err != nil {
			respondDashboardError(c, "saveDashboard", err)
			return
		}
		after := dashboardSummary(&d.Dashboard)
		after["widget_count"] = len(d.Widgets)
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardUpdated, models.ResourceDashboard,
			&id, models.AuditChanges{After: after})
		respondSuccess(c, http.StatusOK, d)
	}
}

func deleteDashboardHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		d, err := store.Delete(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "deleteDashboard", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardDeleted, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: dashboardSummary(d)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func listDashboardSharesHandler(store dashboardStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		shares, err := store.ListShares(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "listDashboardShares", err)
			return
		}
		if shares == nil {
			shares = []dashboards.ShareView{}
		}
		respondSuccess(c, http.StatusOK, shares)
	}
}

func shareDashboardHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		userID, ok := parseUUIDParam(c, "user_id", "user")
		if !ok {
			return
		}
		var body struct {
			Permission string `json:"permission"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := store.UpsertShare(c.Request.Context(), v, id, userID, body.Permission); err != nil {
			respondDashboardError(c, "shareDashboard", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardShared, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: map[string]any{"user_id": userID, "permission": body.Permission}})
		respondSuccess(c, http.StatusOK, gin.H{"shared": true})
	}
}

func unshareDashboardHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		userID, ok := parseUUIDParam(c, "user_id", "user")
		if !ok {
			return
		}
		if err := store.RemoveShare(c.Request.Context(), v, id, userID); err != nil {
			respondDashboardError(c, "unshareDashboard", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardUnshared, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: map[string]any{"user_id": userID}})
		respondSuccess(c, http.StatusOK, gin.H{"unshared": true})
	}
}

// rangeOverride reads ?range=, answering 400 for an unknown key.
func rangeOverride(c *gin.Context) (string, bool) {
	r := c.Query("range")
	if r != "" && !dashboards.ValidRange(r) {
		respondError(c, http.StatusBadRequest, "range must be 1h, 6h, 24h, 7d, 30d, 90d or 1y")
		return "", false
	}
	return r, true
}

func widgetDataHandler(store dashboardStore, resolver widgetResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		wid, ok := parseUUIDParam(c, "wid", "widget")
		if !ok {
			return
		}
		override, ok := rangeOverride(c)
		if !ok {
			return
		}
		w, _, err := store.Widget(c.Request.Context(), v, id, wid)
		if err != nil {
			respondDashboardError(c, "widgetData", err)
			return
		}
		resp, err := resolver.Resolve(c.Request.Context(), v, w, override)
		if err != nil {
			respondDashboardError(c, "widgetData", err)
			return
		}
		respondSuccess(c, http.StatusOK, resp)
	}
}

type previewRequest struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
	Range  string          `json:"range"`
}

// widgetPreviewHandler resolves an unsaved widget for its editor. It needs
// edit access to the dashboard, and resolves as the editor, so a preview can
// never show what the editor could not see anyway.
func widgetPreviewHandler(store dashboardStore, resolver widgetResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		var req previewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Range != "" && !dashboards.ValidRange(req.Range) {
			respondError(c, http.StatusBadRequest, "range must be 1h, 6h, 24h, 7d, 30d, 90d or 1y")
			return
		}
		d, err := store.Get(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "widgetPreview", err)
			return
		}
		if d.Access != dashboards.LevelEdit.String() && d.Access != dashboards.LevelManage.String() {
			respondDashboardError(c, "widgetPreview", dashboards.ErrForbidden)
			return
		}
		resp, err := resolver.Preview(c.Request.Context(), v, req.Type, req.Config, req.Range)
		if err != nil {
			respondDashboardError(c, "widgetPreview", err)
			return
		}
		respondSuccess(c, http.StatusOK, resp)
	}
}

func getPublicLinkHandler(store dashboardStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		link, err := store.PublicLink(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "getPublicLink", err)
			return
		}
		respondSuccess(c, http.StatusOK, link)
	}
}

func createPublicLinkHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		link, err := store.CreatePublicLink(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "createPublicLink", err)
			return
		}
		// The token itself is never written to the audit log.
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardLinkCreated, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: map[string]any{"dashboard_id": id}})
		respondSuccess(c, http.StatusOK, link)
	}
}

func revokePublicLinkHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "dashboard")
		if !ok {
			return
		}
		if err := store.RevokePublicLink(c.Request.Context(), v, id); err != nil {
			respondDashboardError(c, "revokePublicLink", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardLinkRevoked, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: map[string]any{"dashboard_id": id}})
		respondSuccess(c, http.StatusOK, gin.H{"revoked": true})
	}
}
```

- [ ] **Step 5: Write the public handlers**

`backend/internal/api/dashboard_public_handler.go`:

```go
package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/dashboards"
)

// linkGone is what a dead public link shows, so a TV says why it is blank.
const linkGone = "This dashboard link is no longer available"

// RegisterPublicDashboardRoutes mounts the public-link routes: no login, a
// per-IP limit sized for several TVs behind one office address (spec §4).
// Every request resolves the token again, so a revoked link, a regenerated
// one, or a demoted creator stops working on the next request.
func RegisterPublicDashboardRoutes(router *gin.Engine, store dashboardStore, resolver widgetResolver) {
	limiter := NewRateLimiter(600, time.Minute, 120)
	g := router.Group("/api/v1/public/dashboards", limiter.Middleware("public-dashboard", ByIP))
	g.GET("/:token", publicDashboardHandler(store))
	g.GET("/:token/widgets/:wid/data", publicWidgetDataHandler(store, resolver))
}

func publicDashboardHandler(store dashboardStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		d, err := store.ResolveToken(c.Request.Context(), c.Param("token"))
		if errors.Is(err, dashboards.ErrNotFound) {
			respondError(c, http.StatusNotFound, linkGone)
			return
		}
		if err != nil {
			respondInternal(c, "publicDashboard", err)
			return
		}
		layout, err := store.PublicLayout(c.Request.Context(), d)
		if err != nil {
			respondInternal(c, "publicDashboard", err)
			return
		}
		respondSuccess(c, http.StatusOK, layout)
	}
}

func publicWidgetDataHandler(store dashboardStore, resolver widgetResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		wid, err := uuid.Parse(c.Param("wid"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid widget id")
			return
		}
		d, err := store.ResolveToken(c.Request.Context(), c.Param("token"))
		if errors.Is(err, dashboards.ErrNotFound) {
			respondError(c, http.StatusNotFound, linkGone)
			return
		}
		if err != nil {
			respondInternal(c, "publicWidgetData", err)
			return
		}
		w, err := store.PublicWidget(c.Request.Context(), d, wid)
		if errors.Is(err, dashboards.ErrWidgetNotFound) {
			respondError(c, http.StatusNotFound, "widget not found")
			return
		}
		if err != nil {
			respondInternal(c, "publicWidgetData", err)
			return
		}
		resp, err := resolver.ResolvePublic(c.Request.Context(), d, w)
		if err != nil {
			respondInternal(c, "publicWidgetData", err)
			return
		}
		respondSuccess(c, http.StatusOK, resp)
	}
}
```

- [ ] **Step 6: Wire it up in `main.go`**

In `backend/cmd/sentinel/main.go`, add the import `"github.com/Stevy2191/Sentinel/backend/internal/dashboards"`. Immediately before `router := gin.New()`, add:

```go
	// Dashboards (phase 4). The registry is shared by the service (to
	// validate saves) and the resolver (to load widget data).
	dashboardRegistry := dashboards.NewDefaultRegistry(dashboards.Deps{})
	dashboardChecker := dashboards.NewDBChecker(db, siteService, monitorService)
	dashboardService := dashboards.NewService(db, siteService, dashboardRegistry, dashboardChecker)
	dashboardResolver := dashboards.NewResolver(dashboardRegistry, dashboardChecker)
```

After `api.RegisterPublicReportRoutes(router, reportBuilder)`, add:

```go
	// Public dashboard links: no login, like status pages and shared reports.
	api.RegisterPublicDashboardRoutes(router, dashboardService, dashboardResolver)
```

After `api.RegisterDeviceHealthRoute(...)`, add:

```go
	api.RegisterDashboardRoutes(v1, dashboardService, dashboardResolver, auditService, authService)
```

- [ ] **Step 7: Run the tests and the build**

Run: `go_docker test ./internal/api/ -run 'TestDashboard|TestWidgetData|TestPreviewFieldError|TestPublicWidgetData' -v`
Expected: PASS (6 tests).
Run: `go_docker build ./... && go_docker vet ./...`
Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/dashboards/deps.go backend/internal/api/dashboard_handler.go backend/internal/api/dashboard_public_handler.go backend/internal/api/dashboard_handler_test.go backend/cmd/sentinel/main.go
git commit -m "feat(dashboards): HTTP routes, public link routes and wiring

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Which metrics a device has, and their labels

**Files:**
- Create: `backend/internal/services/metrics_catalog_store.go`
- Create: `backend/internal/api/device_metrics_handler.go`
- Modify: `backend/cmd/sentinel/main.go` (register the route)
- Test: `backend/internal/services/metrics_catalog_store_db_test.go`, `backend/internal/api/device_metrics_handler_test.go`

**Interfaces:**
- Consumes: `MetricsStore`, `MetricCatalogue`, `MetricDef`, tables `metrics.series`, `device_interfaces`, `profile_metrics`; api `loadDevice`, `deviceStore`, `siteAccessChecker`.
- Produces:
  - `type InstanceKey struct{ DeviceID uuid.UUID; Metric, Instance string }`
  - `type MetricInstance struct{ Instance, Label string }` (JSON `instance`, `label`)
  - `type DeviceMetric struct{ Metric, Label, Unit string; Instances []MetricInstance }` (JSON `metric`, `label`, `unit`, `instances`)
  - `(*MetricsStore).Describe(ctx, keys []string) (map[string]MetricDef, error)`
  - `(*MetricsStore).InstanceLabels(ctx, deviceIDs []uuid.UUID, metrics []string) (map[InstanceKey]string, error)`
  - `(*MetricsStore).DeviceMetrics(ctx, deviceID uuid.UUID) ([]DeviceMetric, error)`
  - `api.RegisterDeviceMetricsRoute(rg *gin.RouterGroup, metrics deviceMetricsLister, devices deviceStore, sites siteAccessChecker)` → `GET /devices/:id/metrics` (readonly site access)

- [ ] **Step 1: Write the failing DB test**

`backend/internal/services/metrics_catalog_store_db_test.go`:

```go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBDeviceMetricsAndLabels(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 5, "Gi1/0/5", "uplink")
	testdb.Exec(t, db, `INSERT INTO metric_profiles (id, name) VALUES (gen_random_uuid(), 'Test')`)
	testdb.Exec(t, db, `INSERT INTO profile_metrics (profile_id, name, key, source, kind, units, oid)
		SELECT id, 'Fan speed', 'fan_rpm', 'column', 'gauge', 'rpm', '1.3.6.1.4.1.9.9.13.1.4.1.3' FROM metric_profiles WHERE name = 'Test'`)
	SetCustomMetricKeys([]string{"fan_rpm"})
	t.Cleanup(func() { SetCustomMetricKeys(nil) })

	m := NewMetricsStore(db)
	at := time.Now().UTC().Add(-time.Minute)
	testdb.Must(t, m.Write(ctx, s.DeviceID, at, []SamplePoint{
		{Metric: MetricIfInBps, Instance: "5", InterfaceID: &port, Value: 1000},
		{Metric: MetricUPSChargePct, Instance: "", Value: 97},
		{Metric: "fan_rpm", Instance: "1", Label: "Fan 1", Value: 5200},
	}))

	got, err := m.DeviceMetrics(ctx, s.DeviceID)
	testdb.Must(t, err)
	byKey := map[string]DeviceMetric{}
	for _, d := range got {
		byKey[d.Metric] = d
	}
	if d := byKey[MetricIfInBps]; d.Label != "Traffic in" || d.Unit != "bps" || len(d.Instances) != 1 || d.Instances[0].Label != "Gi1/0/5" {
		t.Errorf("if_in_bps = %+v, want the catalogue label and the port's name", d)
	}
	if d := byKey["fan_rpm"]; d.Label != "Fan speed" || d.Unit != "rpm" || d.Instances[0].Label != "Fan 1" {
		t.Errorf("fan_rpm = %+v, want the profile metric's name, units and the row label", d)
	}
	if d := byKey[MetricUPSChargePct]; d.Unit != "%" || d.Instances[0].Instance != "" {
		t.Errorf("ups_charge_pct = %+v", d)
	}

	defs, err := m.Describe(ctx, []string{MetricIfInBps, "fan_rpm", "nope"})
	testdb.Must(t, err)
	if len(defs) != 2 || defs["fan_rpm"].Label != "Fan speed" {
		t.Errorf("Describe = %+v, want two known keys", defs)
	}
	labels, err := m.InstanceLabels(ctx, []uuid.UUID{s.DeviceID}, []string{MetricIfInBps, "fan_rpm"})
	testdb.Must(t, err)
	if labels[InstanceKey{DeviceID: s.DeviceID, Metric: MetricIfInBps, Instance: "5"}] != "Gi1/0/5" {
		t.Errorf("InstanceLabels = %+v", labels)
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `db_up; go_docker test ./internal/services/ -run TestDBDeviceMetricsAndLabels -v; db_down`
Expected: FAIL — `m.DeviceMetrics undefined`.

- [ ] **Step 3: Implement**

`backend/internal/services/metrics_catalog_store.go`:

```go
package services

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

// InstanceKey names one series of one device.
type InstanceKey struct {
	DeviceID uuid.UUID
	Metric   string
	Instance string
}

// MetricInstance is one instance of a metric with its human name.
type MetricInstance struct {
	Instance string `json:"instance"`
	Label    string `json:"label"`
}

// DeviceMetric is one metric a device has stored series for, for the
// dashboard editor's pickers.
type DeviceMetric struct {
	Metric    string           `json:"metric"`
	Label     string           `json:"label"`
	Unit      string           `json:"unit"`
	Instances []MetricInstance `json:"instances"`
}

// instanceLabelSQL names a series for people: a custom row's label, else the
// port's name, else the raw instance.
const instanceLabelSQL = `COALESCE(NULLIF(s.label, ''), NULLIF(di.name, ''), s.instance)`

func builtinDef(key string) (MetricDef, bool) {
	for _, d := range MetricCatalogue {
		if d.Key == key {
			return d, true
		}
	}
	return MetricDef{}, false
}

// Describe returns the label and unit of each known key: the built-in
// catalogue, then custom metrics' names and units. Unknown keys are absent.
func (m *MetricsStore) Describe(ctx context.Context, keys []string) (map[string]MetricDef, error) {
	out := make(map[string]MetricDef, len(keys))
	var custom []string
	for _, k := range keys {
		if d, ok := builtinDef(k); ok {
			out[k] = d
		} else {
			custom = append(custom, k)
		}
	}
	if len(custom) == 0 {
		return out, nil
	}
	var rows []struct {
		Key   string
		Name  string
		Units string
	}
	if err := m.db.WithContext(ctx).Table("profile_metrics").Select("key, name, units").
		Where("key IN ?", custom).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("describing custom metrics: %w", err)
	}
	for _, r := range rows {
		out[r.Key] = MetricDef{Key: r.Key, Unit: r.Units, Label: r.Name}
	}
	return out, nil
}

// InstanceLabels returns the human name of every stored series of these
// devices and metrics.
func (m *MetricsStore) InstanceLabels(ctx context.Context, deviceIDs []uuid.UUID, metrics []string) (map[InstanceKey]string, error) {
	out := map[InstanceKey]string{}
	if len(deviceIDs) == 0 || len(metrics) == 0 {
		return out, nil
	}
	var rows []struct {
		DeviceID uuid.UUID
		Metric   string
		Instance string
		Label    string
	}
	err := m.db.WithContext(ctx).Table("metrics.series AS s").
		Select("s.device_id, s.metric, s.instance, "+instanceLabelSQL+" AS label").
		Joins("LEFT JOIN device_interfaces di ON di.id = s.interface_id").
		Where("s.device_id IN ? AND s.metric IN ?", deviceIDs, metrics).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("loading series labels: %w", err)
	}
	for _, r := range rows {
		out[InstanceKey{DeviceID: r.DeviceID, Metric: r.Metric, Instance: r.Instance}] = r.Label
	}
	return out, nil
}

// DeviceMetrics lists every known metric with stored series for a device,
// each with its instances in natural order (port 2 before port 10), sorted by
// label. A metric whose key is no longer known (a deleted custom metric) is
// left out: a widget could not be saved with it.
func (m *MetricsStore) DeviceMetrics(ctx context.Context, deviceID uuid.UUID) ([]DeviceMetric, error) {
	var rows []struct {
		Metric   string
		Instance string
		Label    string
	}
	err := m.db.WithContext(ctx).Table("metrics.series AS s").
		Select("s.metric, s.instance, "+instanceLabelSQL+" AS label").
		Joins("LEFT JOIN device_interfaces di ON di.id = s.interface_id").
		Where("s.device_id = ?", deviceID).
		Order("s.metric, length(s.instance), s.instance").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing device metrics: %w", err)
	}
	var keys []string
	seen := map[string]bool{}
	for _, r := range rows {
		if !seen[r.Metric] && KnownMetric(r.Metric) {
			seen[r.Metric] = true
			keys = append(keys, r.Metric)
		}
	}
	defs, err := m.Describe(ctx, keys)
	if err != nil {
		return nil, err
	}
	byKey := map[string]*DeviceMetric{}
	out := make([]DeviceMetric, 0, len(keys))
	for _, k := range keys {
		def, ok := defs[k]
		if !ok {
			continue
		}
		out = append(out, DeviceMetric{Metric: k, Label: def.Label, Unit: def.Unit, Instances: []MetricInstance{}})
	}
	for i := range out {
		byKey[out[i].Metric] = &out[i]
	}
	for _, r := range rows {
		if d := byKey[r.Metric]; d != nil {
			d.Instances = append(d.Instances, MetricInstance{Instance: r.Instance, Label: r.Label})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}
```

- [ ] **Step 4: Run to see it pass**

Run: `db_up; go_docker test ./internal/services/ -run TestDBDeviceMetricsAndLabels -v; db_down`
Expected: PASS.

- [ ] **Step 5: Write the route test, then the route**

`backend/internal/api/device_metrics_handler_test.go`:

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

type fakeDeviceMetrics struct{ called bool }

func (f *fakeDeviceMetrics) DeviceMetrics(context.Context, uuid.UUID) ([]services.DeviceMetric, error) {
	f.called = true
	return []services.DeviceMetric{{Metric: "if_in_bps", Label: "Traffic in", Unit: "bps"}}, nil
}

func TestDeviceMetricsNeedsSiteAccess(t *testing.T) {
	site := uuid.New()
	devs := &fakeDevices{device: services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}}
	for level, want := range map[services.SiteAccessLevel]int{services.SiteAccessNone: http.StatusNotFound, services.SiteAccessReadonly: http.StatusOK} {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set("user_id", uuid.New()); c.Set("username", "u"); c.Set("is_admin", false); c.Next() })
		lister := &fakeDeviceMetrics{}
		RegisterDeviceMetricsRoute(r.Group("/api/v1"), lister, devs, fakeSiteLevels{site: level})
		w := do(r, http.MethodGet, "/api/v1/devices/"+devs.device.ID.String()+"/metrics", nil)
		if w.Code != want || lister.called != (want == http.StatusOK) {
			t.Errorf("level %v: status %d, listed %v; want %d", level, w.Code, lister.called, want)
		}
	}
}
```

`backend/internal/api/device_metrics_handler.go`:

```go
package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// deviceMetricsLister is services.MetricsStore.DeviceMetrics.
type deviceMetricsLister interface {
	DeviceMetrics(ctx context.Context, deviceID uuid.UUID) ([]services.DeviceMetric, error)
}

// RegisterDeviceMetricsRoute mounts GET /devices/:id/metrics: the metrics and
// instances a device has, for the dashboard editor. Readonly site access.
func RegisterDeviceMetricsRoute(rg *gin.RouterGroup, metrics deviceMetricsLister, devices deviceStore, sites siteAccessChecker) {
	rg.GET("/devices/:id/metrics", func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		list, err := metrics.DeviceMetrics(c.Request.Context(), d.ID)
		if err != nil {
			respondInternal(c, "deviceMetrics", err)
			return
		}
		respondSuccess(c, http.StatusOK, list)
	})
}
```

In `main.go`, after `api.RegisterDeviceHealthRoute(...)`:

```go
	api.RegisterDeviceMetricsRoute(v1, metricsStore, deviceService, siteService)
```

- [ ] **Step 6: Run and commit**

Run: `go_docker test ./internal/api/ -run TestDeviceMetricsNeedsSiteAccess -v && go_docker build ./...`
Expected: PASS, no build output.

```bash
git add backend/internal/services/metrics_catalog_store.go backend/internal/services/metrics_catalog_store_db_test.go backend/internal/api/device_metrics_handler.go backend/internal/api/device_metrics_handler_test.go backend/cmd/sentinel/main.go
git commit -m "feat(metrics): list a device's metrics with labels and units for the editor

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Top-N ports from the rollups

**Files:**
- Create: `backend/internal/services/metrics_topn.go`
- Test: `backend/internal/services/metrics_topn_db_test.go`

**Interfaces:**
- Consumes: `MetricsStore`, `PickResolution`, `rawRetentionDays`, metric keys `MetricIfInBps`, `MetricIfOutBps`, `MetricIfInUtilPct`, `MetricIfOutUtilPct`, `MetricIfInErrorsPM`, `MetricIfOutErrorsPM`.
- Produces: constants `TopNTraffic = "traffic"`, `TopNUtilisation = "utilisation"`, `TopNErrors = "errors"`; `type TopNQuery struct{ DeviceIDs, InterfaceIDs []uuid.UUID; Measure string; From, To time.Time; N int }`; `type TopNRow struct{ DeviceID uuid.UUID; DeviceName string; IfIndex int; PortName, Alias string; Value float64 }` (JSON `device_id`, `device_name`, `if_index`, `port_name`, `alias`, `value`); `(*MetricsStore).TopN(ctx, q TopNQuery) ([]TopNRow, error)`.

Measures: `traffic` = average in + average out (bps); `utilisation` = the busier direction's average (%); `errors` = average in + out errors per minute, ports with none left out.

- [ ] **Step 1: Write the failing test**

`backend/internal/services/metrics_topn_db_test.go`:

```go
package services

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBTopN(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	m := NewMetricsStore(db)
	now := time.Now().UTC()
	ports := map[int]uuid.UUID{}
	for ifIndex, bps := range map[int]float64{1: 100, 2: 5000, 3: 900} {
		ports[ifIndex] = seedPort(t, db, s.DeviceID, ifIndex, "port"+strconv.Itoa(ifIndex), "")
		id := ports[ifIndex]
		for _, ago := range []time.Duration{40 * time.Minute, 20 * time.Minute} {
			testdb.Must(t, m.Write(ctx, s.DeviceID, now.Add(-ago), []SamplePoint{
				{Metric: MetricIfInBps, Instance: strconv.Itoa(ifIndex), InterfaceID: &id, Value: bps},
				{Metric: MetricIfOutBps, Instance: strconv.Itoa(ifIndex), InterfaceID: &id, Value: bps / 2},
				{Metric: MetricIfInErrorsPM, Instance: strconv.Itoa(ifIndex), InterfaceID: &id, Value: map[int]float64{1: 0, 2: 0, 3: 4}[ifIndex]},
			}))
		}
	}
	refreshRollups(t, db)

	for _, span := range []time.Duration{time.Hour, 24 * time.Hour} { // raw, then the 5-minute rollup
		rows, err := m.TopN(ctx, TopNQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Measure: TopNTraffic, From: now.Add(-span), To: now, N: 2})
		testdb.Must(t, err)
		if len(rows) != 2 || rows[0].IfIndex != 2 || rows[1].IfIndex != 3 || rows[0].Value != 7500 {
			t.Errorf("span %v: traffic top 2 = %+v, want port 2 (7500 bps) then port 3", span, rows)
		}
		if rows[0].DeviceName != "dev-10.0.0.2" || rows[0].PortName != "port2" {
			t.Errorf("span %v: row = %+v, want device and port names joined in", span, rows[0])
		}
	}
	rows, err := m.TopN(ctx, TopNQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Measure: TopNErrors, From: now.Add(-time.Hour), To: now, N: 10})
	testdb.Must(t, err)
	if len(rows) != 1 || rows[0].IfIndex != 3 {
		t.Errorf("errors = %+v, want only port 3 (ports with no errors are left out)", rows)
	}
	rows, err = m.TopN(ctx, TopNQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, InterfaceIDs: []uuid.UUID{ports[1]}, Measure: TopNTraffic, From: now.Add(-time.Hour), To: now, N: 10})
	testdb.Must(t, err)
	if len(rows) != 1 || rows[0].IfIndex != 1 {
		t.Errorf("restricted to port 1 = %+v", rows)
	}
	if _, err := m.TopN(ctx, TopNQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Measure: "nope", From: now.Add(-time.Hour), To: now, N: 5}); err == nil {
		t.Error("an unknown measure was accepted")
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `db_up; go_docker test ./internal/services/ -run TestDBTopN -v; db_down`
Expected: FAIL — `m.TopN undefined`.

- [ ] **Step 3: Implement**

`backend/internal/services/metrics_topn.go`:

```go
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Top-N measures.
const (
	TopNTraffic     = "traffic"
	TopNUtilisation = "utilisation"
	TopNErrors      = "errors"
)

// TopNQuery ranks ports of DeviceIDs over [From, To).
type TopNQuery struct {
	DeviceIDs []uuid.UUID
	// InterfaceIDs restricts to these ports (physical ones, say); empty = all.
	InterfaceIDs []uuid.UUID
	Measure      string
	From, To     time.Time
	N            int
}

// TopNRow is one ranked port.
type TopNRow struct {
	DeviceID   uuid.UUID `json:"device_id" gorm:"column:device_id"`
	DeviceName string    `json:"device_name" gorm:"column:device_name"`
	IfIndex    int       `json:"if_index" gorm:"column:if_index"`
	PortName   string    `json:"port_name" gorm:"column:port_name"`
	Alias      string    `json:"alias" gorm:"column:alias"`
	Value      float64   `json:"value" gorm:"column:value"`
}

// topNMeasure is a measure's metrics and how their per-series averages combine.
func topNMeasure(measure string) (metrics []string, combine string, having string, err error) {
	switch measure {
	case TopNTraffic:
		return []string{MetricIfInBps, MetricIfOutBps}, "sum", "", nil
	case TopNUtilisation:
		return []string{MetricIfInUtilPct, MetricIfOutUtilPct}, "max", "", nil
	case TopNErrors:
		return []string{MetricIfInErrorsPM, MetricIfOutErrorsPM}, "sum", " HAVING sum(x.avg) > 0", nil
	}
	return nil, "", "", fmt.Errorf("unknown top-N measure %q", measure)
}

// TopN ranks ports by measure over the window, reading the same source
// PickResolution would (raw up to 6 h, then the 5-minute and hourly rollups).
func (m *MetricsStore) TopN(ctx context.Context, q TopNQuery) ([]TopNRow, error) {
	metrics, combine, having, err := topNMeasure(q.Measure)
	if err != nil {
		return nil, err
	}
	if len(q.DeviceIDs) == 0 || q.N <= 0 {
		return []TopNRow{}, nil
	}
	rawSince := time.Now().UTC().Add(-time.Duration(m.rawRetentionDays(ctx)) * 24 * time.Hour)
	source, _ := PickResolution(q.From, q.To, rawSince)

	var inner string
	switch source {
	case "raw":
		inner = `SELECT s.device_id, s.interface_id, s.metric, avg(x.value) AS avg
			FROM metrics.samples x JOIN metrics.series s ON s.id = x.series_id
			WHERE s.device_id IN ? AND s.metric IN ? AND x.time >= ? AND x.time < ?`
	default:
		table := "metrics.samples_5m"
		if source == "1h" {
			table = "metrics.samples_1h"
		}
		inner = `SELECT s.device_id, s.interface_id, s.metric, COALESCE(sum(r.vsum) / NULLIF(sum(r.n), 0), 0) AS avg
			FROM ` + table + ` r JOIN metrics.series s ON s.id = r.series_id
			WHERE s.device_id IN ? AND s.metric IN ? AND r.bucket >= ? AND r.bucket < ?`
	}
	args := []any{q.DeviceIDs, metrics, q.From, q.To}
	if len(q.InterfaceIDs) > 0 {
		inner += " AND s.interface_id IN ?"
		args = append(args, q.InterfaceIDs)
	}
	inner += " AND s.interface_id IS NOT NULL GROUP BY s.device_id, s.interface_id, s.metric"

	sql := `SELECT x.device_id, d.name AS device_name, di.if_index, di.name AS port_name, di.alias,
			` + combine + `(x.avg) AS value
		FROM (` + inner + `) x
		JOIN devices d ON d.id = x.device_id
		JOIN device_interfaces di ON di.id = x.interface_id
		GROUP BY x.device_id, d.name, di.if_index, di.name, di.alias` + having + `
		ORDER BY value DESC, d.name, di.if_index
		LIMIT ?`
	args = append(args, q.N)

	var rows []TopNRow
	if err := m.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("ranking ports: %w", err)
	}
	if rows == nil {
		rows = []TopNRow{}
	}
	return rows, nil
}
```

- [ ] **Step 4: Run to see it pass**

Run: `db_up; go_docker test ./internal/services/ -run TestDBTopN -v; db_down`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/metrics_topn.go backend/internal/services/metrics_topn_db_test.go
git commit -m "feat(metrics): rank ports by traffic, utilisation or errors from the rollups

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: Incidents by site and subjects; monitors by id

**Files:**
- Modify: `backend/internal/services/incident_service.go` (`IncidentListOptions`, `ListIncidents`, new `OpenCountsByDevice`)
- Modify: `backend/internal/services/monitor_service.go` (new `MonitorsByIDs`, after `GetMonitor`)
- Test: `backend/internal/services/incident_dashboard_db_test.go`

**Interfaces:**
- Produces: `IncidentListOptions.SiteID *uuid.UUID`, `IncidentListOptions.DeviceIDs []uuid.UUID`, `IncidentListOptions.MonitorIDs []uuid.UUID`; `(*IncidentService).OpenCountsByDevice(ctx, deviceIDs []uuid.UUID) (map[uuid.UUID]int, error)`; `(*MonitorService).MonitorsByIDs(ctx, ids []uuid.UUID) ([]models.Monitor, error)`.

- [ ] **Step 1: Write the failing test**

`backend/internal/services/incident_dashboard_db_test.go`:

```go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBIncidentListBySiteAndSubjects(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	a := seedDevice(t, db, "A", "10.0.0.1")
	b := seedDevice(t, db, "B", "10.0.0.2")
	mon := newMonitor(t, db, admin, "web")
	start := time.Now().UTC().Add(-time.Hour)
	inA := insertIncident(t, db, nil, &a.DeviceID, start, nil)
	inB := insertIncident(t, db, nil, &b.DeviceID, start, nil)
	inM := insertIncident(t, db, &mon, nil, start, nil)
	svc := NewIncidentService(db)
	viewer := &IncidentViewer{UserID: admin, IsAdmin: true}

	ids := func(opts IncidentListOptions) map[uuid.UUID]bool {
		opts.Viewer = viewer
		rows, _, err := svc.ListIncidents(ctx, opts)
		testdb.Must(t, err)
		out := map[uuid.UUID]bool{}
		for _, r := range rows {
			out[r.ID] = true
		}
		return out
	}
	if got := ids(IncidentListOptions{SiteID: &a.SiteID}); len(got) != 1 || !got[inA] {
		t.Errorf("site A = %v, want only its device's incident", got)
	}
	if got := ids(IncidentListOptions{DeviceIDs: []uuid.UUID{b.DeviceID}, MonitorIDs: []uuid.UUID{mon}}); len(got) != 2 || !got[inB] || !got[inM] {
		t.Errorf("device B or the monitor = %v", got)
	}
	if got := ids(IncidentListOptions{MonitorIDs: []uuid.UUID{mon}}); len(got) != 1 || !got[inM] {
		t.Errorf("monitor only = %v", got)
	}

	counts, err := svc.OpenCountsByDevice(ctx, []uuid.UUID{a.DeviceID, b.DeviceID, uuid.New()})
	testdb.Must(t, err)
	if counts[a.DeviceID] != 1 || counts[b.DeviceID] != 1 || len(counts) != 2 {
		t.Errorf("open counts = %v", counts)
	}
}

func TestDBMonitorsByIDs(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	m1, m2 := newMonitor(t, db, owner, "one"), newMonitor(t, db, owner, "two")
	got, err := NewMonitorService(db).MonitorsByIDs(context.Background(), []uuid.UUID{m1, m2, uuid.New()})
	testdb.Must(t, err)
	if len(got) != 2 {
		t.Errorf("got %d monitors, want the 2 that exist", len(got))
	}
	if none, err := NewMonitorService(db).MonitorsByIDs(context.Background(), nil); err != nil || len(none) != 0 {
		t.Errorf("no ids: %v, %v", none, err)
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `db_up; go_docker test ./internal/services/ -run 'TestDBIncidentListBySiteAndSubjects|TestDBMonitorsByIDs' -v; db_down`
Expected: FAIL — `unknown field SiteID in struct literal`.

- [ ] **Step 3: Implement**

In `IncidentListOptions` (`incident_service.go`), after the `DeviceID` field:

```go
	// SiteID restricts to incidents of devices in one site. Monitors are not
	// in sites, so a site filter never returns monitor incidents.
	SiteID *uuid.UUID
	// DeviceIDs and MonitorIDs restrict to these subjects; with both set, an
	// incident of either kind matches.
	DeviceIDs  []uuid.UUID
	MonitorIDs []uuid.UUID
```

In `ListIncidents`, after the `if opts.DeviceID != nil { ... }` block:

```go
	if opts.SiteID != nil {
		base = base.Where("d.site_id = ?", *opts.SiteID)
	}
	switch {
	case len(opts.DeviceIDs) > 0 && len(opts.MonitorIDs) > 0:
		base = base.Where("(i.device_id IN ? OR i.monitor_id IN ?)", opts.DeviceIDs, opts.MonitorIDs)
	case len(opts.DeviceIDs) > 0:
		base = base.Where("i.device_id IN ?", opts.DeviceIDs)
	case len(opts.MonitorIDs) > 0:
		base = base.Where("i.monitor_id IN ?", opts.MonitorIDs)
	}
```

At the end of `incident_service.go`:

```go
// OpenCountsByDevice counts each device's open incidents of every kind
// (down, port, UPS, metric). Devices with none are absent.
func (s *IncidentService) OpenCountsByDevice(ctx context.Context, deviceIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(deviceIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		DeviceID uuid.UUID
		N        int
	}
	err := s.db.WithContext(ctx).Table("incidents").Select("device_id, count(*) AS n").
		Where("end_time IS NULL AND device_id IN ?", deviceIDs).Group("device_id").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("counting open incidents: %w", err)
	}
	for _, r := range rows {
		out[r.DeviceID] = r.N
	}
	return out, nil
}
```

In `monitor_service.go`, after `GetMonitor`:

```go
// MonitorsByIDs returns the monitors with these ids that exist, in no
// particular order. Unlike GetMonitor it does not log each lookup: a
// dashboard asks for up to 50 of them every 30 seconds.
func (s *MonitorService) MonitorsByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Monitor, error) {
	if len(ids) == 0 {
		return []models.Monitor{}, nil
	}
	var out []models.Monitor
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("loading monitors: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Run to see them pass, and the existing incident tests**

Run: `db_up; go_docker test ./internal/services/ -run 'TestDBIncident|TestDBMonitorsByIDs|TestDBOpen' -v; go_docker test ./internal/api/ -run Incident; db_down`
Expected: PASS, including the existing incident access tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/incident_service.go backend/internal/services/monitor_service.go backend/internal/services/incident_dashboard_db_test.go
git commit -m "feat(incidents): list by site and by chosen subjects; count open incidents per device

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: Move the uptime bucket helpers into services

A pure move: the status page's 90-day buckets and the hourly buckets (with the span helpers only they use) go to `services`, so status pages, reports and the `monitors` widget share one implementation (spec §3). Behaviour and JSON are unchanged; the existing api tests prove it.

**Files:**
- Create: `backend/internal/services/uptime_buckets.go`
- Modify: `backend/internal/api/report_handler.go` (delete `span`, `latest`, `earliest`, `rfc3339OrNil`, `interval`, `incidentIntervals`, `maintenanceIntervals`, `spanInHour`, `computeHourlyUptimeBuckets`; call the service)
- Modify: `backend/internal/api/status_page_handler.go` (delete `computeDailyUptimeBuckets`; call the service)
- Modify: `backend/internal/api/hourly_buckets_test.go`, `backend/internal/api/daily_buckets_test.go` (call the service)

**Interfaces:**
- Produces: `services.UptimeDailyDays = 90`; `services.DailyUptimeBuckets(checks []models.Check, now time.Time) []map[string]any`; `services.HourlyUptimeBuckets(checks []models.Check, incidents []models.Incident, maintenanceWindows []models.MaintenanceHistory, now, createdAt time.Time) []map[string]any`.

- [ ] **Step 1: Confirm nothing else uses the helpers being moved**

Run: `grep -n 'latest(\|earliest(\|rfc3339OrNil(\|spanInHour(\|incidentIntervals(\|maintenanceIntervals(\|interval{\|span{' backend/internal/api/*.go | grep -v _test`
Expected: every hit is inside the block from `type span struct` to the end of `spanInHour`, or inside `computeHourlyUptimeBuckets`. If anything else uses one of them, stop and keep that helper in `api` as well.

- [ ] **Step 2: Create the service file**

`backend/internal/services/uptime_buckets.go`:

```go
package services

import (
	"math"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// UptimeDailyDays is how many days DailyUptimeBuckets covers.
const UptimeDailyDays = 90

func roundUptime(f float64) float64 { return math.Round(f*100) / 100 }

// DailyUptimeBuckets buckets checks into the 90 UTC calendar days ending at
// now, each summarized purely by pass/fail counts - no incident- or
// maintenance-derived spans, since the public status page never surfaces
// exact downtime clock times. Used by the public status page and the
// dashboard monitors widget; the authenticated endpoints have their own,
// hourly, view (HourlyUptimeBuckets).
func DailyUptimeBuckets(checks []models.Check, now time.Time) []map[string]any {
	type bucket struct{ total, failed int }
	buckets := make(map[time.Time]*bucket)
	truncDay := func(t time.Time) time.Time {
		t = t.UTC()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	for _, ch := range checks {
		k := truncDay(ch.Timestamp)
		b := buckets[k]
		if b == nil {
			b = &bucket{}
			buckets[k] = b
		}
		b.total++
		if ch.Status != "success" {
			b.failed++
		}
	}

	daily := make([]map[string]any, 0, UptimeDailyDays)
	curDay := truncDay(now)
	for i := UptimeDailyDays - 1; i >= 0; i-- {
		k := curDay.AddDate(0, 0, -i)
		b := buckets[k]
		status := "nodata"
		uptime := 0.0
		if b != nil && b.total > 0 {
			uptime = roundUptime(float64(b.total-b.failed) / float64(b.total) * 100)
			switch {
			case b.failed == 0:
				status = "up"
			case b.failed == b.total:
				status = "down"
			default:
				status = "partial"
			}
		}
		daily = append(daily, map[string]any{
			"date":   k.Format("2006-01-02"),
			"uptime": uptime,
			"status": status,
		})
	}
	return daily
}

// uptimeSpan is a stretch of time inside a single hourly bucket, together
// with how many seconds of that bucket it covers. A zero start means "nothing here".
type uptimeSpan struct {
	start, end time.Time
	seconds    int
}

// latestTime/earliestTime are the time equivalents of max/min.
func latestTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earliestTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// rfc3339OrNil renders a timestamp for JSON, or nil when it is unset, so the
// client can distinguish "no downtime" from "downtime at the epoch".
func rfc3339OrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// uptimeInterval is a period of interest — an outage or a maintenance window
// — with a nil end meaning it is still running.
type uptimeInterval struct {
	start time.Time
	end   *time.Time
}

func incidentIntervals(incidents []models.Incident) []uptimeInterval {
	out := make([]uptimeInterval, 0, len(incidents))
	for i := range incidents {
		out = append(out, uptimeInterval{start: incidents[i].StartTime, end: incidents[i].EndTime})
	}
	return out
}

func maintenanceIntervals(windows []models.MaintenanceHistory) []uptimeInterval {
	out := make([]uptimeInterval, 0, len(windows))
	for i := range windows {
		end := windows[i].EndTime
		out = append(out, uptimeInterval{start: windows[i].StartTime, end: &end})
	}
	return out
}

// spanInHour clips every interval to [hourStart, hourEnd] and returns the
// outer bounds of the clipped pieces plus their total duration. Reporting the
// outer bounds (rather than each piece) keeps the client's tooltip to a
// single "from — to" while the second count stays exact.
func spanInHour(intervals []uptimeInterval, hourStart, hourEnd, now time.Time) uptimeSpan {
	var out uptimeSpan
	for _, iv := range intervals {
		segStart := latestTime(iv.start.UTC(), hourStart)
		// An open interval is still running, so it covers the hour up to now.
		segEnd := now
		if iv.end != nil {
			segEnd = iv.end.UTC()
		}
		segEnd = earliestTime(segEnd, hourEnd)

		if !segEnd.After(segStart) {
			continue
		}
		out.seconds += int(segEnd.Sub(segStart).Seconds())
		if out.start.IsZero() || segStart.Before(out.start) {
			out.start = segStart
		}
		if segEnd.After(out.end) {
			out.end = segEnd
		}
	}
	return out
}

// HourlyUptimeBuckets buckets checks into the 24 hours ending at now, each
// annotated with two views of the hour: "uptime"/"status" summarize the
// checks recorded in it (what a sparkline draws), while the down/maintenance
// spans give the actual clock time derived from incidents and recorded
// maintenance history (what a detailed 24-hour health bar draws). Shared by
// the authenticated uptime-history endpoint, the public status page and the
// dashboard monitors widget, so all draw the same 24-hour strip.
//
// checks may span a wider window than 24 hours - only checks that fall in
// the last 24 hours affect the result. incidents and maintenanceWindows
// should already be scoped to that same 24-hour window.
func HourlyUptimeBuckets(
	checks []models.Check,
	incidents []models.Incident,
	maintenanceWindows []models.MaintenanceHistory,
	now time.Time,
	createdAt time.Time,
) []map[string]any {
	type bucket struct {
		total, failed int
		// First/last failing check in the hour: the fallback source for a
		// downtime span when no incident was recorded (e.g. failures during a
		// maintenance window, where incidents are suppressed).
		firstFail, lastFail time.Time
	}
	buckets := make(map[time.Time]*bucket)
	truncHour := func(t time.Time) time.Time {
		t = t.UTC()
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	}
	for _, ch := range checks {
		k := truncHour(ch.Timestamp)
		b := buckets[k]
		if b == nil {
			b = &bucket{}
			buckets[k] = b
		}
		b.total++
		if ch.Status != "success" {
			b.failed++
			ts := ch.Timestamp.UTC()
			if b.firstFail.IsZero() || ts.Before(b.firstFail) {
				b.firstFail = ts
			}
			if ts.After(b.lastFail) {
				b.lastFail = ts
			}
		}
	}

	downIntervals := incidentIntervals(incidents)
	maintIntervals := maintenanceIntervals(maintenanceWindows)

	hourly := make([]map[string]any, 0, 24)
	curHour := truncHour(now)
	for i := 23; i >= 0; i-- {
		k := curHour.Add(time.Duration(-i) * time.Hour)
		b := buckets[k]
		status := "nodata"
		uptime := 0.0
		if b != nil && b.total > 0 {
			uptime = roundUptime(float64(b.total-b.failed) / float64(b.total) * 100)
			switch {
			case b.failed == 0:
				status = "up"
			case b.failed == b.total:
				status = "down"
			default:
				status = "partial"
			}
		}

		// The hour is only observable up to "now" — never attribute downtime
		// to the part of the current hour that hasn't happened yet.
		hourEnd := earliestTime(k.Add(time.Hour), now.UTC())

		downSpan := spanInHour(downIntervals, k, hourEnd, now.UTC())
		if downSpan.seconds == 0 && b != nil && !b.firstFail.IsZero() {
			// No incident on record, but checks failed here: report the span
			// the failures cover so the hour still reads as degraded.
			downSpan = uptimeSpan{start: b.firstFail, end: b.lastFail, seconds: 0}
		}
		maintSpan := spanInHour(maintIntervals, k, hourEnd, now.UTC())

		hourly = append(hourly, map[string]any{
			"hour":                k.Hour(),
			"uptime":              uptime,
			"status":              status,
			"bucket_start":        k.Format(time.RFC3339),
			"down_seconds":        downSpan.seconds,
			"maintenance_seconds": maintSpan.seconds,
			"down_start":          rfc3339OrNil(downSpan.start),
			"down_end":            rfc3339OrNil(downSpan.end),
			"maintenance_start":   rfc3339OrNil(maintSpan.start),
			"maintenance_end":     rfc3339OrNil(maintSpan.end),
			// An hour entirely before the monitor existed has nothing to
			// report, as opposed to an hour that was simply quiet.
			"observed": !k.Add(time.Hour).Before(createdAt.UTC()),
		})
	}
	return hourly
}
```

Before saving, open `report_handler.go` and compare the moved bodies line by line with the originals (`computeHourlyUptimeBuckets` and the helpers from `type span struct` to the end of `spanInHour`). Only these may differ: the renames above (`span`→`uptimeSpan`, `interval`→`uptimeInterval`, `latest`/`earliest`→`latestTime`/`earliestTime`, `round2`→`roundUptime`, `gin.H`→`map[string]any`, `interface{}`→`any`). If the original contains anything this copy lacks, copy it across.

- [ ] **Step 3: Point the api at the service and delete the originals**

In `backend/internal/api/report_handler.go`: delete `type span`, `latest`, `earliest`, `rfc3339OrNil`, `type interval`, `incidentIntervals`, `maintenanceIntervals`, `spanInHour` and `computeHourlyUptimeBuckets`, and replace

```go
		hourly := computeHourlyUptimeBuckets(checks, incidents, maintenanceWindows, now, monitor.CreatedAt)
```

with

```go
		hourly := services.HourlyUptimeBuckets(checks, incidents, maintenanceWindows, now, monitor.CreatedAt)
```

Keep `round2` — other report code uses it.

In `backend/internal/api/status_page_handler.go`: delete `computeDailyUptimeBuckets` (keep `publicStatusDays`, now `const publicStatusDays = services.UptimeDailyDays`), and replace

```go
			daily := make([]gin.H, 0, publicStatusDays)
			if checks, err := checkService.GetChecksInRange(ctx, m.ID, windowStart, now, 0, 0); err != nil {
				log.Printf("[statuspage] daily buckets failed for monitor %s: %v", m.ID, err)
			} else {
				daily = computeDailyUptimeBuckets(checks, now)
			}
```

with

```go
			daily := make([]map[string]any, 0, publicStatusDays)
			if checks, err := checkService.GetChecksInRange(ctx, m.ID, windowStart, now, 0, 0); err != nil {
				log.Printf("[statuspage] daily buckets failed for monitor %s: %v", m.ID, err)
			} else {
				daily = services.DailyUptimeBuckets(checks, now)
			}
```

In `hourly_buckets_test.go` and `daily_buckets_test.go`, replace `computeHourlyUptimeBuckets(` with `services.HourlyUptimeBuckets(` and `computeDailyUptimeBuckets(` with `services.DailyUptimeBuckets(`, add the `services` import, and update the comments that name the old functions.

- [ ] **Step 4: Run the whole api and services suites**

Run: `go_docker vet ./... && go_docker test ./internal/api/ ./internal/services/`
Expected: PASS — the moved functions behave exactly as before.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/uptime_buckets.go backend/internal/api/report_handler.go backend/internal/api/status_page_handler.go backend/internal/api/hourly_buckets_test.go backend/internal/api/daily_buckets_test.go
git commit -m "refactor(uptime): move the hourly and daily bucket helpers into services

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
## Part B — Widgets

Every widget follows the same shape: a config struct, `Validate` that normalises it (defaults filled, ids deduped, limits enforced, `FieldError`s on bad input), `Subjects`, `Resolve` that reads only `in.Visible` and builds a response struct, and `Refresh`. Response structs carry subject ids only as `*uuid.UUID` with `omitempty`, set only when `!in.Viewer.Public`, and never carry hosts, URLs or free text — so the public trim is the default, not an afterthought.

### Task 15: Widget data dependencies; timeseries and stat widgets

**Files:**
- Modify: `backend/internal/dashboards/deps.go` (the data interfaces, `Deps` fields, register the two widgets)
- Create: `backend/internal/dashboards/widget_timeseries.go`, `backend/internal/dashboards/widget_stat.go`
- Modify: `backend/internal/dashboards/fixtures_db_test.go` (add `newPort`, `realDeps`, `resolveWidget`, `writePortBps`)
- Modify: `backend/cmd/sentinel/main.go` (fill `Deps`)
- Test: `backend/internal/dashboards/widget_timeseries_db_test.go`, `backend/internal/dashboards/widget_stat_test.go`, `backend/internal/dashboards/widget_stat_db_test.go`

**Interfaces:**
- Consumes: `services.MetricsStore.Query|Describe|InstanceLabels|TopN`, `services.PortService.SiteTraffic|PhysicalInterfaceIDs|DevicePorts|Events|UPSStatus`, `services.DeviceService.Get|List`, `services.KnownMetric`.
- Produces:
  - interfaces `MetricsReader`, `PortReader`, `DeviceReader`, `IncidentReader`, `MonitorReader`, `CheckReader`, `AgentReader`; `type HealthFunc func(ctx context.Context, d *services.DeviceView) (*services.DeviceHealthView, error)`; `Deps{Metrics, Ports, Devices, Health, Incidents, Monitors, Checks, Agents}`
  - `timeseriesWidget{metrics MetricsReader; ports PortReader; devices DeviceReader}`; `TimeseriesData{Range, Resolution string; StepSeconds int; Lines []SeriesLine}`; `SeriesLine{Key, Label, Metric, Unit string; Points []services.MetricPoint}`
  - `statWidget{...same deps}`; `StatData{Label, Unit string; Value float64; Level, Mode, Range string; Spark []services.MetricPoint}`; `statLevel(v float64, warn, crit *float64, direction string) string`
  - shared helpers `validateMetricKeys(keys []string, lo, hi int) ([]string, error)`, `deviceNames(ctx, d DeviceReader, ids []uuid.UUID) (map[uuid.UUID]string, error)`, `siteDeviceIDs(ctx, d DeviceReader, siteID uuid.UUID) ([]uuid.UUID, error)`, `allPortMetrics(keys []string) bool`, `const maxMetrics = 10`, `maxDevices = 50`, `maxInstances = 200`

- [ ] **Step 1: Define the data interfaces**

Replace `backend/internal/dashboards/deps.go` with:

```go
package dashboards

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Per-widget limits (spec §3), matching the metrics query endpoint.
const (
	maxMetrics   = 10
	maxDevices   = 50
	maxInstances = 200
	maxMonitors  = 50
	maxAgents    = 50
)

// MetricsReader is the part of services.MetricsStore widgets use.
type MetricsReader interface {
	Query(ctx context.Context, q services.MetricsQuery) (*services.MetricsResult, error)
	TopN(ctx context.Context, q services.TopNQuery) ([]services.TopNRow, error)
	Describe(ctx context.Context, keys []string) (map[string]services.MetricDef, error)
	InstanceLabels(ctx context.Context, deviceIDs []uuid.UUID, metrics []string) (map[services.InstanceKey]string, error)
}

// PortReader is the part of services.PortService widgets use.
type PortReader interface {
	DevicePorts(ctx context.Context, d *services.DeviceView) (*services.DevicePortsView, error)
	Events(ctx context.Context, f services.PortEventFilter) ([]services.PortEventView, int64, error)
	SiteTraffic(ctx context.Context, siteID uuid.UUID, from, to time.Time) (*services.SiteTraffic, error)
	UPSStatus(ctx context.Context, d *services.DeviceView) (*services.UPSStatusView, error)
	PhysicalInterfaceIDs(ctx context.Context, deviceIDs []uuid.UUID) ([]uuid.UUID, error)
}

// DeviceReader is the part of services.DeviceService widgets use.
type DeviceReader interface {
	Get(ctx context.Context, id uuid.UUID) (*services.DeviceView, error)
	List(ctx context.Context, userID uuid.UUID, isAdmin bool, f services.DeviceFilter) ([]services.DeviceView, error)
}

// HealthFunc is a device's Health section (ProfileService.DeviceHealth with
// its metrics store and incident service bound).
type HealthFunc func(ctx context.Context, d *services.DeviceView) (*services.DeviceHealthView, error)

// IncidentReader is the part of services.IncidentService widgets use.
type IncidentReader interface {
	ListIncidents(ctx context.Context, opts services.IncidentListOptions) ([]services.IncidentWithMonitor, int64, error)
	OpenCountsByDevice(ctx context.Context, deviceIDs []uuid.UUID) (map[uuid.UUID]int, error)
	GetOverlappingIncidents(ctx context.Context, monitorID uuid.UUID, start, end time.Time) ([]models.Incident, error)
}

// MonitorReader is the part of services.MonitorService widgets use.
type MonitorReader interface {
	MonitorsByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Monitor, error)
	GetMaintenanceHistory(ctx context.Context, monitorID uuid.UUID, start, end time.Time) ([]models.MaintenanceHistory, error)
}

// CheckReader is the part of services.CheckService widgets use.
type CheckReader interface {
	GetChecksInRange(ctx context.Context, monitorID uuid.UUID, start, end time.Time, limit, offset int) ([]models.Check, error)
}

// AgentReader is the part of services.AgentService widgets use.
type AgentReader interface {
	List(ctx context.Context) ([]models.Agent, error)
}

// Deps is what the widgets read their data from. Each widget takes only the
// parts it needs, so widgets are tested against the real services in DB
// tests and nothing else. main.go fills it in.
type Deps struct {
	Metrics   MetricsReader
	Ports     PortReader
	Devices   DeviceReader
	Health    HealthFunc
	Incidents IncidentReader
	Monitors  MonitorReader
	Checks    CheckReader
	Agents    AgentReader
}

// NewDefaultRegistry registers every widget type Sentinel ships, in the order
// the editor lists them.
func NewDefaultRegistry(d Deps) *Registry {
	return NewRegistry(
		labelWidget{},
		timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices},
		statWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices},
	)
}

// deviceNames maps each id to its device's name; missing devices are absent.
func deviceNames(ctx context.Context, d DeviceReader, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		v, err := d.Get(ctx, id)
		if err != nil {
			continue // deleted between the access check and now: its line is dropped
		}
		out[id] = v.Name
	}
	return out, nil
}

// siteDeviceIDs lists a site's devices. The site was access-checked already.
func siteDeviceIDs(ctx context.Context, d DeviceReader, siteID uuid.UUID) ([]uuid.UUID, error) {
	list, err := d.List(ctx, uuid.Nil, true, services.DeviceFilter{SiteID: &siteID})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(list))
	for i, v := range list {
		ids[i] = v.ID
	}
	return ids, nil
}

// allPortMetrics reports whether every key is a built-in port metric.
func allPortMetrics(keys []string) bool {
	for _, k := range keys {
		if len(k) < 3 || k[:3] != "if_" {
			return false
		}
	}
	return len(keys) > 0
}

// validateMetricKeys dedupes keys and checks there are lo to hi known ones.
func validateMetricKeys(keys []string, lo, hi int) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" || seen[k] {
			continue
		}
		if !services.KnownMetric(k) {
			return nil, fieldErr("metrics", "include "+k+", which is not a known metric")
		}
		seen[k] = true
		out = append(out, k)
	}
	if err := checkCount("metrics", len(out), lo, hi); err != nil {
		return nil, err
	}
	return out, nil
}

// knownOnly drops keys that are no longer known (a custom metric deleted
// after the widget was saved). Viewing never re-validates; it just shows less.
func knownOnly(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if services.KnownMetric(k) {
			out = append(out, k)
		}
	}
	return out
}
```

- [ ] **Step 2: Add the widget test fixtures**

Append to `backend/internal/dashboards/fixtures_db_test.go` (and add the imports `context`, `encoding/json`, `strconv`, `time`, and `github.com/Stevy2191/Sentinel/backend/internal/services`):

```go
// newPort inserts a present, physical (ethernetCsmacd) port.
func newPort(t *testing.T, db *gorm.DB, deviceID uuid.UUID, ifIndex int, name, alias string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name, alias, if_type, present, oper_status, admin_status)
		VALUES (?, ?, ?, ?, ?, 6, true, 'up', 'up')`, id, deviceID, ifIndex, name, alias)
	return id
}

// realDeps wires the widgets to the real services on db.
func realDeps(t *testing.T, db *gorm.DB) Deps {
	t.Helper()
	incidents := services.NewIncidentService(db)
	metrics := services.NewMetricsStore(db)
	ports := services.NewPortService(db, metrics, incidents, services.NewSettingsService(db))
	devices := services.NewDeviceService(db, services.NewSNMPCredentialService(db), incidents)
	devices.SetMetricsStore(metrics)
	profiles := services.NewProfileService(db)
	return Deps{
		Metrics: metrics, Ports: ports, Devices: devices,
		Health: func(ctx context.Context, d *services.DeviceView) (*services.DeviceHealthView, error) {
			return profiles.DeviceHealth(ctx, d, metrics, incidents)
		},
		Incidents: incidents, Monitors: services.NewMonitorService(db),
		Checks: services.NewCheckService(db), Agents: services.NewAgentService(db),
	}
}

// writePortBps writes in/out traffic for a port at at.
func writePortBps(t *testing.T, d Deps, deviceID, portID uuid.UUID, ifIndex int, at time.Time, in, out float64) {
	t.Helper()
	m := d.Metrics.(*services.MetricsStore)
	inst := strconv.Itoa(ifIndex)
	testdb.Must(t, m.Write(context.Background(), deviceID, at, []services.SamplePoint{
		{Metric: services.MetricIfInBps, Instance: inst, InterfaceID: &portID, Value: in},
		{Metric: services.MetricIfOutBps, Instance: inst, InterfaceID: &portID, Value: out},
	}))
}

// resolveWidget validates cfg with w, then resolves it with in (Now defaults
// to now).
func resolveWidget(t *testing.T, w Widget, cfg string, in ResolveInput) (any, error) {
	t.Helper()
	clean, err := w.Validate(context.Background(), json.RawMessage(cfg))
	if err != nil {
		t.Fatalf("validating %s: %v", cfg, err)
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	return w.Resolve(context.Background(), clean, in)
}
```

- [ ] **Step 3: Write the failing tests**

`backend/internal/dashboards/widget_stat_test.go`:

```go
package dashboards

import "testing"

func TestStatLevel(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		v          float64
		warn, crit *float64
		dir        string
		want       string
	}{
		{50, f(80), f(90), "above", "ok"},
		{85, f(80), f(90), "above", "warn"},
		{95, f(80), f(90), "above", "crit"},
		{90, f(80), f(90), "above", "crit"},
		{30, f(25), f(10), "below", "ok"},
		{20, f(25), f(10), "below", "warn"},
		{5, f(25), f(10), "below", "crit"},
		{99, nil, nil, "above", "ok"},
		{95, nil, f(90), "above", "crit"},
	}
	for _, c := range cases {
		if got := statLevel(c.v, c.warn, c.crit, c.dir); got != c.want {
			t.Errorf("statLevel(%v, %v, %v, %s) = %s, want %s", c.v, c.warn, c.crit, c.dir, got, c.want)
		}
	}
}
```

`backend/internal/dashboards/widget_timeseries_db_test.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBTimeseries(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	site := newSite(t, db, "HQ")
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	port := newPort(t, db, dev, 1, "Gi1/0/1", "uplink")
	now := time.Now().UTC()
	writePortBps(t, d, dev, port, 1, now.Add(-30*time.Minute), 1000, 500)
	writePortBps(t, d, dev, port, 1, now.Add(-10*time.Minute), 3000, 700)
	visible := ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Now: now}

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"metrics":["if_in_bps"],"devices":["%s"],"instances":["1"],"range":"1h"}`, dev), visible)
	testdb.Must(t, err)
	ts := data.(TimeseriesData)
	if len(ts.Lines) != 1 || ts.Lines[0].Label != "Gi1/0/1 · Traffic in" || ts.Lines[0].Unit != "bps" || len(ts.Lines[0].Points) != 2 {
		t.Errorf("one device, one port: %+v", ts)
	}

	data, err = resolveWidget(t, w, fmt.Sprintf(`{"metrics":["if_in_bps","if_out_bps"],"devices":["%s"],"range":"1h"}`, dev), visible)
	testdb.Must(t, err)
	if ts = data.(TimeseriesData); len(ts.Lines) != 2 || ts.Lines[0].Label != "Traffic in" {
		t.Errorf("no instances (device total per metric): %+v", ts.Lines)
	}

	data, err = resolveWidget(t, w, fmt.Sprintf(`{"metrics":["if_in_bps"],"site_id":"%s","range":"1h"}`, site),
		ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now})
	testdb.Must(t, err)
	if ts = data.(TimeseriesData); len(ts.Lines) != 1 || ts.Lines[0].Label != "Site total · Traffic in" {
		t.Errorf("site total: %+v", ts.Lines)
	}

	_, err = resolveWidget(t, w, fmt.Sprintf(`{"source":"site_traffic","site_id":"%s","view":"internet"}`, site),
		ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("internet traffic with no WAN port: err = %v, want ErrNoData", err)
	}
}

func TestDBTimeseriesUnknownMetricIsNoData(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	// Saved while fan_rpm existed; the custom metric has since been deleted,
	// so the saved config is resolved without validating it again.
	cfg := json.RawMessage(fmt.Sprintf(`{"source":"metrics","metrics":["fan_rpm"],"devices":["%s"],"range":"1h"}`, dev))
	_, err := w.Resolve(context.Background(), cfg, ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Now: time.Now()})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("err = %v, want ErrNoData (not a 400 or a 500)", err)
	}
}

func TestTimeseriesValidate(t *testing.T) {
	w := timeseriesWidget{}
	dev, site := uuid.New(), uuid.New()
	for raw, field := range map[string]string{
		`{"metrics":[],"devices":["` + dev.String() + `"]}`:                                         "metrics",
		`{"metrics":["nope"],"devices":["` + dev.String() + `"]}`:                                   "metrics",
		`{"metrics":["if_in_bps"]}`:                                                                 "devices",
		`{"metrics":["if_in_bps"],"devices":["` + dev.String() + `"],"site_id":"` + site.String() + `"}`: "devices",
		`{"metrics":["if_in_bps"],"devices":["` + dev.String() + `"],"range":"2h"}`:                 "range",
		`{"source":"site_traffic"}`:                                                                 "site_id",
		`{"source":"site_traffic","site_id":"` + site.String() + `","view":"sideways"}`:             "view",
		`{"source":"weather"}`:                                                                      "source",
	} {
		_, err := w.Validate(context.Background(), json.RawMessage(raw))
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: err = %v, want a FieldError on %q", raw, err, field)
		}
	}
	got, err := w.Validate(context.Background(), json.RawMessage(`{"metrics":["if_in_bps","if_in_bps"],"devices":["`+dev.String()+`","`+dev.String()+`"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var c timeseriesConfig
	_ = json.Unmarshal(got, &c)
	if c.Source != "metrics" || c.Range != "24h" || len(c.Metrics) != 1 || len(c.Devices) != 1 {
		t.Errorf("normalised = %s, want defaults filled and duplicates dropped", got)
	}
}
```

`backend/internal/dashboards/widget_stat_db_test.go`:

```go
package dashboards

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBStat(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := statWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	port := newPort(t, db, dev, 1, "Gi1/0/1", "")
	now := time.Now().UTC()
	in := ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Now: now}

	_, err := resolveWidget(t, w, fmt.Sprintf(`{"metric":"if_in_bps","device_id":"%s","instance":"1","range":"1h"}`, dev), in)
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("no samples: err = %v, want ErrNoData", err)
	}
	writePortBps(t, d, dev, port, 1, now.Add(-30*time.Minute), 1000, 0)
	writePortBps(t, d, dev, port, 1, now.Add(-5*time.Minute), 3000, 0)

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"metric":"if_in_bps","device_id":"%s","instance":"1","range":"1h","warn":2000,"crit":5000,"sparkline":true}`, dev), in)
	testdb.Must(t, err)
	s := data.(StatData)
	if s.Value != 3000 || s.Level != "warn" || s.Unit != "bps" || len(s.Spark) != 2 || s.Mode != "latest" {
		t.Errorf("latest = %+v, want 3000 bps, warn, two sparkline points", s)
	}
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"metric":"if_in_bps","device_id":"%s","instance":"1","range":"1h","mode":"average"}`, dev), in)
	testdb.Must(t, err)
	if s = data.(StatData); s.Value != 2000 || s.Spark != nil {
		t.Errorf("average = %+v, want 2000 and no sparkline", s)
	}
}
```

- [ ] **Step 4: Run to see them fail**

Run: `go_docker test ./internal/dashboards/ -run 'TestStatLevel|TestTimeseriesValidate'`
Expected: FAIL — `undefined: statLevel`, `undefined: timeseriesWidget`.

- [ ] **Step 5: Implement the timeseries widget**

`backend/internal/dashboards/widget_timeseries.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

const (
	sourceMetrics     = "metrics"
	sourceSiteTraffic = "site_traffic"
)

type timeseriesConfig struct {
	Source    string      `json:"source"`
	Metrics   []string    `json:"metrics,omitempty"`
	Devices   []uuid.UUID `json:"devices,omitempty"`
	Instances []string    `json:"instances,omitempty"`
	// SiteID: with source metrics, a site total; with site_traffic, the site.
	SiteID *uuid.UUID `json:"site_id,omitempty"`
	// View is site_traffic's chart: internet (north-south) or east_west.
	View  string `json:"view,omitempty"`
	Range string `json:"range"`
}

// SeriesLine is one drawn line. Key is unique within one response.
type SeriesLine struct {
	Key    string                 `json:"key"`
	Label  string                 `json:"label"`
	Metric string                 `json:"metric"`
	Unit   string                 `json:"unit"`
	Points []services.MetricPoint `json:"points"`
}

// TimeseriesData is a chart's response. It never carries subject ids.
type TimeseriesData struct {
	Range       string       `json:"range"`
	Resolution  string       `json:"resolution"`
	StepSeconds int          `json:"step_seconds"`
	Lines       []SeriesLine `json:"lines"`
}

type timeseriesWidget struct {
	metrics MetricsReader
	ports   PortReader
	devices DeviceReader
}

func (timeseriesWidget) Type() string { return "timeseries" }

func (timeseriesWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c timeseriesConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	var err error
	if c.Range, err = validateRange(c.Range); err != nil {
		return nil, err
	}
	switch c.Source {
	case "", sourceMetrics:
		c.Source, c.View = sourceMetrics, ""
		if c.Metrics, err = validateMetricKeys(c.Metrics, 1, maxMetrics); err != nil {
			return nil, err
		}
		c.Devices = dedupe(c.Devices)
		switch {
		case c.SiteID != nil && len(c.Devices) > 0:
			return nil, fieldErr("devices", "choose devices or a site total, not both")
		case c.SiteID == nil:
			if err := checkCount("devices", len(c.Devices), 1, maxDevices); err != nil {
				return nil, err
			}
		}
		if err := checkCount("instances", len(c.Instances), 0, maxInstances); err != nil {
			return nil, err
		}
		if c.SiteID != nil {
			c.Instances = nil
		}
	case sourceSiteTraffic:
		if c.SiteID == nil {
			return nil, fieldErr("site_id", "is required")
		}
		switch c.View {
		case "":
			c.View = "internet"
		case "internet", "east_west":
		default:
			return nil, fieldErr("view", "must be internet or east_west")
		}
		c.Metrics, c.Devices, c.Instances = nil, nil, nil
	default:
		return nil, fieldErr("source", "must be metrics or site_traffic")
	}
	return json.Marshal(c)
}

func (timeseriesWidget) Subjects(raw json.RawMessage) Subjects {
	var c timeseriesConfig
	_ = json.Unmarshal(raw, &c)
	if c.SiteID != nil {
		return Subjects{Sites: []uuid.UUID{*c.SiteID}}
	}
	return Subjects{Devices: c.Devices}
}

func (timeseriesWidget) Refresh(raw json.RawMessage, override string) time.Duration {
	var c timeseriesConfig
	_ = json.Unmarshal(raw, &c)
	return chartRefresh(effectiveRange(c.Range, override))
}

func (w timeseriesWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c timeseriesConfig
	_ = json.Unmarshal(raw, &c)
	rng := effectiveRange(c.Range, in.Override)
	to := in.Now.UTC()
	from := to.Add(-rangeSpan(rng))
	if c.Source == sourceSiteTraffic {
		return w.siteTraffic(ctx, c, rng, from, to)
	}

	keys := knownOnly(c.Metrics)
	if len(keys) == 0 {
		return nil, ErrNoData
	}
	q := services.MetricsQuery{Metrics: keys, From: from, To: to}
	siteTotal := c.SiteID != nil
	if siteTotal {
		ids, err := siteDeviceIDs(ctx, w.devices, *c.SiteID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, ErrNoData
		}
		q.DeviceIDs, q.Sum = ids, true
		if allPortMetrics(keys) {
			ifs, err := w.ports.PhysicalInterfaceIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			if len(ifs) == 0 {
				return nil, ErrNoData
			}
			q.InterfaceIDs = ifs
		}
	} else {
		q.DeviceIDs, q.Instances = in.Visible.Devices, c.Instances
	}
	res, err := w.metrics.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defs, err := w.metrics.Describe(ctx, keys)
	if err != nil {
		return nil, err
	}
	series := res.Series
	if !siteTotal && len(c.Instances) == 0 {
		series = sumPerDeviceMetric(series)
	}
	names := map[uuid.UUID]string{}
	labels := map[services.InstanceKey]string{}
	if !siteTotal {
		if names, err = deviceNames(ctx, w.devices, q.DeviceIDs); err != nil {
			return nil, err
		}
		if labels, err = w.metrics.InstanceLabels(ctx, q.DeviceIDs, keys); err != nil {
			return nil, err
		}
	}
	data := TimeseriesData{Range: rng, Resolution: res.Resolution, StepSeconds: res.StepSeconds, Lines: []SeriesLine{}}
	for _, s := range series {
		def := defs[s.Metric]
		metricLabel := def.Label
		if metricLabel == "" {
			metricLabel = s.Metric
		}
		var parts []string
		switch {
		case siteTotal:
			parts = append(parts, "Site total")
		default:
			if len(q.DeviceIDs) > 1 && s.DeviceID != nil {
				parts = append(parts, names[*s.DeviceID])
			}
			if s.Instance != "" && s.DeviceID != nil {
				label := labels[services.InstanceKey{DeviceID: *s.DeviceID, Metric: s.Metric, Instance: s.Instance}]
				if label == "" {
					label = s.Instance
				}
				parts = append(parts, label)
			}
		}
		parts = append(parts, metricLabel)
		data.Lines = append(data.Lines, SeriesLine{
			Key: fmt.Sprintf("s%d", len(data.Lines)), Label: strings.Join(parts, " · "),
			Metric: s.Metric, Unit: def.Unit, Points: s.Points,
		})
	}
	if noPoints(data.Lines) {
		return nil, ErrNoData
	}
	return data, nil
}

// siteTraffic is the site traffic chart: internet in/out, or east-west.
func (w timeseriesWidget) siteTraffic(ctx context.Context, c timeseriesConfig, rng string, from, to time.Time) (any, error) {
	st, err := w.ports.SiteTraffic(ctx, *c.SiteID, from, to)
	if err != nil {
		return nil, err
	}
	data := TimeseriesData{Range: rng, Resolution: st.Resolution, StepSeconds: st.StepSeconds, Lines: []SeriesLine{}}
	if c.View == "east_west" {
		pts := make([]services.MetricPoint, len(st.EastWest))
		for i, p := range st.EastWest {
			pts[i] = services.MetricPoint{Time: p.Time, Avg: p.Bps, Min: p.Bps, Max: p.Bps}
		}
		data.Lines = append(data.Lines, SeriesLine{Key: "s0", Label: "Inside the site", Metric: "east_west_bps", Unit: "bps", Points: pts})
	} else {
		if !st.WANConfigured {
			return nil, ErrNoData
		}
		down := make([]services.MetricPoint, len(st.NorthSouth))
		up := make([]services.MetricPoint, len(st.NorthSouth))
		for i, p := range st.NorthSouth {
			down[i] = services.MetricPoint{Time: p.Time, Avg: p.InBps, Min: p.InBps, Max: p.InBps}
			up[i] = services.MetricPoint{Time: p.Time, Avg: p.OutBps, Min: p.OutBps, Max: p.OutBps}
		}
		data.Lines = append(data.Lines,
			SeriesLine{Key: "s0", Label: "Download", Metric: "internet_in_bps", Unit: "bps", Points: down},
			SeriesLine{Key: "s1", Label: "Upload", Metric: "internet_out_bps", Unit: "bps", Points: up})
	}
	if noPoints(data.Lines) {
		return nil, ErrNoData
	}
	return data, nil
}

// sumPerDeviceMetric adds up each device's instances of a metric (a
// device's ports) into one line per device and metric, as the device page's
// traffic chart does. Min and Max then equal Avg.
func sumPerDeviceMetric(in []services.MetricSeries) []services.MetricSeries {
	type key struct {
		device uuid.UUID
		metric string
	}
	sums := map[key]map[time.Time]float64{}
	var order []key
	for _, s := range in {
		if s.DeviceID == nil {
			continue
		}
		k := key{*s.DeviceID, s.Metric}
		if sums[k] == nil {
			sums[k] = map[time.Time]float64{}
			order = append(order, k)
		}
		for _, p := range s.Points {
			sums[k][p.Time] += p.Avg
		}
	}
	out := make([]services.MetricSeries, 0, len(order))
	for _, k := range order {
		times := make([]time.Time, 0, len(sums[k]))
		for t := range sums[k] {
			times = append(times, t)
		}
		sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
		pts := make([]services.MetricPoint, len(times))
		for i, t := range times {
			v := sums[k][t]
			pts[i] = services.MetricPoint{Time: t, Avg: v, Min: v, Max: v}
		}
		dev := k.device
		out = append(out, services.MetricSeries{DeviceID: &dev, Metric: k.metric, Points: pts})
	}
	return out
}

func noPoints(lines []SeriesLine) bool {
	for _, l := range lines {
		if len(l.Points) > 0 {
			return false
		}
	}
	return true
}
```

- [ ] **Step 6: Implement the stat widget**

`backend/internal/dashboards/widget_stat.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type statConfig struct {
	Metric    string     `json:"metric"`
	DeviceID  *uuid.UUID `json:"device_id,omitempty"`
	Instance  string     `json:"instance,omitempty"`
	SiteID    *uuid.UUID `json:"site_id,omitempty"`
	Mode      string     `json:"mode"`
	Warn      *float64   `json:"warn,omitempty"`
	Crit      *float64   `json:"crit,omitempty"`
	Direction string     `json:"direction"`
	Sparkline bool       `json:"sparkline"`
	Range     string     `json:"range"`
}

// StatData is one number and how it compares with its thresholds.
type StatData struct {
	Label string                 `json:"label"`
	Unit  string                 `json:"unit"`
	Value float64                `json:"value"`
	Level string                 `json:"level"`
	Mode  string                 `json:"mode"`
	Range string                 `json:"range"`
	Spark []services.MetricPoint `json:"spark,omitempty"`
}

type statWidget struct {
	metrics MetricsReader
	ports   PortReader
	devices DeviceReader
}

func (statWidget) Type() string { return "stat" }

// statLevel is ok, warn or crit: crossing a threshold in direction counts
// from the threshold itself.
func statLevel(v float64, warn, crit *float64, direction string) string {
	past := func(t *float64) bool {
		if t == nil {
			return false
		}
		if direction == "below" {
			return v <= *t
		}
		return v >= *t
	}
	switch {
	case past(crit):
		return "crit"
	case past(warn):
		return "warn"
	}
	return "ok"
}

func (statWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c statConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	if c.Metric == "" {
		return nil, fieldErr("metric", "is required")
	}
	if !services.KnownMetric(c.Metric) {
		return nil, fieldErr("metric", c.Metric+" is not a known metric")
	}
	switch {
	case c.DeviceID != nil && c.SiteID != nil:
		return nil, fieldErr("device_id", "choose a device or a site total, not both")
	case c.DeviceID == nil && c.SiteID == nil:
		return nil, fieldErr("device_id", "choose a device or a site")
	}
	if c.SiteID != nil {
		c.Instance = ""
	}
	switch c.Mode {
	case "":
		c.Mode = "latest"
	case "latest", "average":
	default:
		return nil, fieldErr("mode", "must be latest or average")
	}
	switch c.Direction {
	case "":
		c.Direction = "above"
	case "above", "below":
	default:
		return nil, fieldErr("direction", "must be above or below")
	}
	if c.Warn != nil && c.Crit != nil {
		if (c.Direction == "above" && *c.Warn > *c.Crit) || (c.Direction == "below" && *c.Warn < *c.Crit) {
			return nil, fieldErr("warn", "must come before the critical threshold")
		}
	}
	var err error
	if c.Range, err = validateRange(c.Range); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

func (statWidget) Subjects(raw json.RawMessage) Subjects {
	var c statConfig
	_ = json.Unmarshal(raw, &c)
	switch {
	case c.SiteID != nil:
		return Subjects{Sites: []uuid.UUID{*c.SiteID}}
	case c.DeviceID != nil:
		return Subjects{Devices: []uuid.UUID{*c.DeviceID}}
	}
	return Subjects{}
}

func (statWidget) Refresh(raw json.RawMessage, override string) time.Duration {
	var c statConfig
	_ = json.Unmarshal(raw, &c)
	return chartRefresh(effectiveRange(c.Range, override))
}

func (w statWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c statConfig
	_ = json.Unmarshal(raw, &c)
	if !services.KnownMetric(c.Metric) {
		return nil, ErrNoData
	}
	rng := effectiveRange(c.Range, in.Override)
	to := in.Now.UTC()
	q := services.MetricsQuery{Metrics: []string{c.Metric}, From: to.Add(-rangeSpan(rng)), To: to, Sum: true}
	var parts []string
	if c.SiteID != nil {
		ids, err := siteDeviceIDs(ctx, w.devices, *c.SiteID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, ErrNoData
		}
		q.DeviceIDs = ids
		if allPortMetrics(q.Metrics) {
			ifs, err := w.ports.PhysicalInterfaceIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			if len(ifs) == 0 {
				return nil, ErrNoData
			}
			q.InterfaceIDs = ifs
		}
		parts = append(parts, "Site total")
	} else {
		if len(in.Visible.Devices) == 0 {
			return nil, ErrNoData
		}
		q.DeviceIDs = in.Visible.Devices[:1]
		if c.Instance != "" {
			q.Instances = []string{c.Instance}
			labels, err := w.metrics.InstanceLabels(ctx, q.DeviceIDs, q.Metrics)
			if err != nil {
				return nil, err
			}
			if l := labels[services.InstanceKey{DeviceID: q.DeviceIDs[0], Metric: c.Metric, Instance: c.Instance}]; l != "" {
				parts = append(parts, l)
			}
		}
	}
	res, err := w.metrics.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(res.Series) == 0 || len(res.Series[0].Points) == 0 {
		return nil, ErrNoData
	}
	pts := res.Series[0].Points
	value := pts[len(pts)-1].Avg
	if c.Mode == "average" {
		var sum float64
		for _, p := range pts {
			sum += p.Avg
		}
		value = sum / float64(len(pts))
	}
	defs, err := w.metrics.Describe(ctx, q.Metrics)
	if err != nil {
		return nil, err
	}
	def := defs[c.Metric]
	label := def.Label
	if label == "" {
		label = c.Metric
	}
	out := StatData{Label: strings.Join(append(parts, label), " · "), Unit: def.Unit, Value: value,
		Level: statLevel(value, c.Warn, c.Crit, c.Direction), Mode: c.Mode, Range: rng}
	if c.Sparkline {
		out.Spark = pts
	}
	return out, nil
}
```

- [ ] **Step 7: Fill `Deps` in `main.go`**

Replace the `dashboards.NewDefaultRegistry(dashboards.Deps{})` line from Task 10 with:

```go
	dashboardRegistry := dashboards.NewDefaultRegistry(dashboards.Deps{
		Metrics: metricsStore,
		Ports:   portService,
		Devices: deviceService,
		Health: func(ctx context.Context, d *services.DeviceView) (*services.DeviceHealthView, error) {
			return profileService.DeviceHealth(ctx, d, metricsStore, incidentService)
		},
		Incidents: incidentService,
		Monitors:  monitorService,
		Checks:    checkService,
		Agents:    agentService,
	})
```

If any of these variables is constructed after the dashboard block, move the dashboard block below the last of them (it must stay before `router := gin.New()`).

- [ ] **Step 8: Run to see them pass**

Run: `go_docker test ./internal/dashboards/ -run 'TestStatLevel|TestTimeseriesValidate' -v`
Then: `db_up; go_docker test ./internal/dashboards/ -run 'TestDBTimeseries|TestDBStat' -v; db_down`
Then: `go_docker build ./...`
Expected: PASS; no build output.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/dashboards backend/cmd/sentinel/main.go
git commit -m "feat(dashboards): timeseries and stat widgets over the metrics store

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 16: Port grid, device health and site power widgets

**Files:**
- Create: `backend/internal/dashboards/trim.go`
- Create: `backend/internal/dashboards/widget_port_grid.go`, `widget_device_health.go`, `widget_site_power.go`
- Modify: `backend/internal/dashboards/deps.go` (register them)
- Test: `backend/internal/dashboards/widget_devices_db_test.go`

**Interfaces:**
- Consumes: `DeviceReader.Get|List`, `PortReader.DevicePorts|UPSStatus`, `HealthFunc`, `models.DeviceTypeUPS`, `portmon.UnitFaceplate`.
- Produces:
  - `trimPort(p *services.PortView)`; `idFor(v Viewer, id uuid.UUID) *uuid.UUID` (nil for a public viewer)
  - `portGridWidget{devices DeviceReader; ports PortReader}` → `PortGridData{DeviceID *uuid.UUID; DeviceName, Model string; Ports []services.PortView; Faceplates []portmon.UnitFaceplate}`
  - `deviceHealthWidget{devices DeviceReader; health HealthFunc}` → `DeviceHealthData{DeviceID *uuid.UUID; DeviceName string; Metrics []services.HealthMetric}`
  - `sitePowerWidget{devices DeviceReader; ports PortReader}` → `SitePowerData{UPSes []UPSCard}`, `UPSCard{DeviceID *uuid.UUID; Name, Status string; Readings map[string]float64; Conditions []string; LowBatteryPct, HighLoadPct int}`
  - config structs `deviceConfig{DeviceID uuid.UUID}` (shared by port_grid and device_health), `siteConfig{SiteID uuid.UUID}`; helpers `validateDeviceConfig`, `validateSiteConfig`

- [ ] **Step 1: Write the failing test**

`backend/internal/dashboards/widget_devices_db_test.go`:

```go
package dashboards

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBPortGridTrimsForPublic(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := portGridWidget{devices: d.Devices, ports: d.Ports}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	port := newPort(t, db, dev, 1, "Gi1/0/1", "uplink")
	testdb.Exec(t, db, `UPDATE device_interfaces SET mac = 'aa:bb:cc:dd:ee:ff' WHERE id = ?`, port)
	cfg := fmt.Sprintf(`{"device_id":"%s"}`, dev)

	data, err := resolveWidget(t, w, cfg, ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}})
	testdb.Must(t, err)
	pg := data.(PortGridData)
	if pg.DeviceID == nil || *pg.DeviceID != dev || len(pg.Ports) != 1 || pg.Ports[0].MAC != "aa:bb:cc:dd:ee:ff" || pg.DeviceName != "core" {
		t.Errorf("logged in = %+v", pg)
	}
	data, err = resolveWidget(t, w, cfg, ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}, Viewer: PublicViewer})
	testdb.Must(t, err)
	pg = data.(PortGridData)
	if pg.DeviceID != nil || pg.Ports[0].ID != uuid.Nil || pg.Ports[0].DeviceID != uuid.Nil || pg.Ports[0].MAC != "" {
		t.Errorf("public = %+v, want ids and MAC blanked", pg)
	}
	if pg.Ports[0].Alias != "uplink" || pg.Ports[0].Name != "Gi1/0/1" {
		t.Errorf("public port lost its name or alias: %+v", pg.Ports[0])
	}
}

func TestDBDeviceHealthNoDataWithoutProfiles(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := deviceHealthWidget{devices: d.Devices, health: d.Health}
	dev := newDevice(t, db, newSite(t, db, "HQ"), "core", "10.0.0.1")
	_, err := resolveWidget(t, w, fmt.Sprintf(`{"device_id":"%s"}`, dev), ResolveInput{Visible: Subjects{Devices: []uuid.UUID{dev}}})
	if !errors.Is(err, ErrNoData) {
		t.Errorf("err = %v, want ErrNoData for a device with no health metrics", err)
	}
}

func TestDBSitePower(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := sitePowerWidget{devices: d.Devices, ports: d.Ports}
	site := newSite(t, db, "HQ")
	ups := newDevice(t, db, site, "ups-1", "10.0.0.50")
	newDevice(t, db, site, "core", "10.0.0.1")
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'ups' WHERE id = ?`, ups)
	m := d.Metrics.(*services.MetricsStore)
	testdb.Must(t, m.Write(context.Background(), ups, time.Now().UTC().Add(-time.Minute), []services.SamplePoint{
		{Metric: services.MetricUPSChargePct, Value: 97}, {Metric: services.MetricUPSOnBattery, Value: 0},
	}))
	in := ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}}
	data, err := resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s"}`, site), in)
	testdb.Must(t, err)
	sp := data.(SitePowerData)
	if len(sp.UPSes) != 1 || sp.UPSes[0].Name != "ups-1" || sp.UPSes[0].Readings[services.MetricUPSChargePct] != 97 || sp.UPSes[0].DeviceID == nil {
		t.Errorf("site power = %+v, want the one UPS with its charge", sp)
	}
	in.Viewer = PublicViewer
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s"}`, site), in)
	testdb.Must(t, err)
	if data.(SitePowerData).UPSes[0].DeviceID != nil {
		t.Error("public site power carries the device id")
	}
	empty := newSite(t, db, "Closet")
	if _, err := resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s"}`, empty), ResolveInput{Visible: Subjects{Sites: []uuid.UUID{empty}}}); !errors.Is(err, ErrNoData) {
		t.Errorf("a site without UPSes: err = %v, want ErrNoData", err)
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `go_docker vet ./internal/dashboards/`
Expected: FAIL — `undefined: portGridWidget`.

- [ ] **Step 3: Implement**

`backend/internal/dashboards/trim.go`:

```go
package dashboards

import (
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// idFor is id for a logged-in viewer and nil for a public one: public
// responses never carry subject ids, so the public page cannot link into
// Sentinel or learn what exists.
func idFor(v Viewer, id uuid.UUID) *uuid.UUID {
	if v.Public {
		return nil
	}
	return &id
}

// trimPort blanks what a public view must not see on a port: its ids, its
// hardware address and the id of the device at its other end. Names,
// aliases, status and traffic stay.
func trimPort(p *services.PortView) {
	p.ID = uuid.Nil
	p.DeviceID = uuid.Nil
	p.MAC = ""
	p.NeighborDeviceID = nil
}
```

`backend/internal/dashboards/widget_port_grid.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// deviceConfig is the config of widgets about one device.
type deviceConfig struct {
	DeviceID uuid.UUID `json:"device_id"`
}

func validateDeviceConfig(raw json.RawMessage) (json.RawMessage, error) {
	var c deviceConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	if c.DeviceID == uuid.Nil {
		return nil, fieldErr("device_id", "is required")
	}
	return json.Marshal(c)
}

func deviceSubjects(raw json.RawMessage) Subjects {
	var c deviceConfig
	_ = json.Unmarshal(raw, &c)
	if c.DeviceID == uuid.Nil {
		return Subjects{}
	}
	return Subjects{Devices: []uuid.UUID{c.DeviceID}}
}

// PortGridData is a device's faceplate with live port status.
type PortGridData struct {
	DeviceID   *uuid.UUID              `json:"device_id,omitempty"`
	DeviceName string                  `json:"device_name"`
	Model      string                  `json:"model"`
	Ports      []services.PortView     `json:"ports"`
	Faceplates []portmon.UnitFaceplate `json:"faceplates"`
}

type portGridWidget struct {
	devices DeviceReader
	ports   PortReader
}

func (portGridWidget) Type() string { return "port_grid" }

func (portGridWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	return validateDeviceConfig(raw)
}

func (portGridWidget) Subjects(raw json.RawMessage) Subjects { return deviceSubjects(raw) }

func (portGridWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

func (w portGridWidget) Resolve(ctx context.Context, _ json.RawMessage, in ResolveInput) (any, error) {
	if len(in.Visible.Devices) == 0 {
		return nil, ErrNoData
	}
	d, err := w.devices.Get(ctx, in.Visible.Devices[0])
	if err != nil {
		return nil, ErrNoData
	}
	view, err := w.ports.DevicePorts(ctx, d)
	if err != nil {
		return nil, err
	}
	if len(view.Ports) == 0 {
		return nil, ErrNoData
	}
	if in.Viewer.Public {
		for i := range view.Ports {
			trimPort(&view.Ports[i])
		}
	}
	return PortGridData{DeviceID: idFor(in.Viewer, d.ID), DeviceName: d.Name, Model: d.EffectiveModel,
		Ports: view.Ports, Faceplates: view.Faceplates}, nil
}
```

`backend/internal/dashboards/widget_device_health.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// DeviceHealthData is a device's Health section: its live custom metrics.
// The profiles it lists on the device page are left out (they carry ids).
type DeviceHealthData struct {
	DeviceID   *uuid.UUID              `json:"device_id,omitempty"`
	DeviceName string                  `json:"device_name"`
	Metrics    []services.HealthMetric `json:"metrics"`
}

type deviceHealthWidget struct {
	devices DeviceReader
	health  HealthFunc
}

func (deviceHealthWidget) Type() string { return "device_health" }

func (deviceHealthWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	return validateDeviceConfig(raw)
}

func (deviceHealthWidget) Subjects(raw json.RawMessage) Subjects { return deviceSubjects(raw) }

func (deviceHealthWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

func (w deviceHealthWidget) Resolve(ctx context.Context, _ json.RawMessage, in ResolveInput) (any, error) {
	if len(in.Visible.Devices) == 0 {
		return nil, ErrNoData
	}
	d, err := w.devices.Get(ctx, in.Visible.Devices[0])
	if err != nil {
		return nil, ErrNoData
	}
	h, err := w.health(ctx, d)
	if err != nil {
		return nil, err
	}
	if h == nil || len(h.Metrics) == 0 {
		return nil, ErrNoData
	}
	return DeviceHealthData{DeviceID: idFor(in.Viewer, d.ID), DeviceName: d.Name, Metrics: h.Metrics}, nil
}
```

`backend/internal/dashboards/widget_site_power.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// siteConfig is the config of widgets about one site.
type siteConfig struct {
	SiteID uuid.UUID `json:"site_id"`
}

func validateSiteConfig(raw json.RawMessage) (json.RawMessage, error) {
	var c siteConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	if c.SiteID == uuid.Nil {
		return nil, fieldErr("site_id", "is required")
	}
	return json.Marshal(c)
}

func siteSubjects(raw json.RawMessage) Subjects {
	var c siteConfig
	_ = json.Unmarshal(raw, &c)
	if c.SiteID == uuid.Nil {
		return Subjects{}
	}
	return Subjects{Sites: []uuid.UUID{c.SiteID}}
}

// UPSCard is one UPS: its reachability, latest readings and open conditions.
type UPSCard struct {
	DeviceID      *uuid.UUID         `json:"device_id,omitempty"`
	Name          string             `json:"name"`
	Status        string             `json:"status"`
	Readings      map[string]float64 `json:"readings"`
	Conditions    []string           `json:"conditions"`
	LowBatteryPct int                `json:"low_battery_pct"`
	HighLoadPct   int                `json:"high_load_pct"`
}

// SitePowerData is every UPS in a site.
type SitePowerData struct {
	UPSes []UPSCard `json:"upses"`
}

type sitePowerWidget struct {
	devices DeviceReader
	ports   PortReader
}

func (sitePowerWidget) Type() string { return "site_power" }

func (sitePowerWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	return validateSiteConfig(raw)
}

func (sitePowerWidget) Subjects(raw json.RawMessage) Subjects { return siteSubjects(raw) }

func (sitePowerWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

// Resolve lists the site's UPSes (chosen or detected type ups) and reads each
// one's status with the same code as the device page's Power panel.
func (w sitePowerWidget) Resolve(ctx context.Context, _ json.RawMessage, in ResolveInput) (any, error) {
	if len(in.Visible.Sites) == 0 {
		return nil, ErrNoData
	}
	site := in.Visible.Sites[0]
	list, err := w.devices.List(ctx, uuid.Nil, true, services.DeviceFilter{SiteID: &site})
	if err != nil {
		return nil, err
	}
	out := SitePowerData{UPSes: []UPSCard{}}
	for i := range list {
		d := &list[i]
		if d.EffectiveType != models.DeviceTypeUPS {
			continue
		}
		st, err := w.ports.UPSStatus(ctx, d)
		if err != nil {
			return nil, err
		}
		conds := st.Conditions
		if conds == nil {
			conds = []string{}
		}
		out.UPSes = append(out.UPSes, UPSCard{DeviceID: idFor(in.Viewer, d.ID), Name: d.Name, Status: d.Status,
			Readings: st.Readings, Conditions: conds, LowBatteryPct: st.LowBatteryPct, HighLoadPct: st.HighLoadPct})
	}
	if len(out.UPSes) == 0 {
		return nil, ErrNoData
	}
	return out, nil
}
```

In `deps.go`, add to `NewDefaultRegistry` after `statWidget{...}`:

```go
		portGridWidget{devices: d.Devices, ports: d.Ports},
		deviceHealthWidget{devices: d.Devices, health: d.Health},
		sitePowerWidget{devices: d.Devices, ports: d.Ports},
```

- [ ] **Step 4: Run to see them pass**

Run: `db_up; go_docker test ./internal/dashboards/ -run 'TestDBPortGrid|TestDBDeviceHealth|TestDBSitePower' -v; db_down`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/dashboards
git commit -m "feat(dashboards): port grid, device health and site power widgets

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 17: Top-N, event log, device table and open incidents widgets

**Files:**
- Create: `backend/internal/dashboards/widget_top_n.go`, `widget_event_log.go`, `widget_device_table.go`, `widget_open_incidents.go`
- Modify: `backend/internal/dashboards/deps.go` (register them)
- Test: `backend/internal/dashboards/widget_lists_db_test.go`

**Interfaces:**
- Consumes: `MetricsReader.TopN`, `PortReader.PhysicalInterfaceIDs|Events`, `DeviceReader.List`, `IncidentReader.ListIncidents|OpenCountsByDevice`, `services.TopN*` constants, `models.ValidDeviceTypes`.
- Produces:
  - `topNWidget` → `TopNData{Measure, Unit, Range string; Items []TopNItem}`, `TopNItem{DeviceID *uuid.UUID; DeviceName string; IfIndex int; PortName, Alias string; Value float64}`
  - `eventLogWidget` → `EventLogData{Items []EventItem}`, `EventItem{Time time.Time; Kind string; DeviceID *uuid.UUID; DeviceName string; IfIndex *int; PortLabel, PortName, Subject, Condition, Severity string; IncidentID *uuid.UUID; EndedAt *time.Time}`; kinds: the port event kinds, `incident_opened`, `incident_closed`
  - `deviceTableWidget` → `DeviceTableData{Devices []DeviceRow}`, `DeviceRow{DeviceID *uuid.UUID; Name, Host, Type, Status, VendorModel string; LastSeenAt *time.Time; Availability30d *float64; OpenIncidents int}` (`host` omitted when empty)
  - `openIncidentsWidget` → `OpenIncidentsData{Incidents []IncidentRow; Total int64}`, `IncidentRow{IncidentID *uuid.UUID; SubjectType, SubjectName, SiteName, Condition, Severity string; StartTime time.Time; DurationSeconds int; DeviceID, MonitorID *uuid.UUID; PortIfIndex *int}`
  - `incidentViewer(v Viewer) *services.IncidentViewer` (a public viewer reads as an admin)

Empty lists are `ok` with an empty slice — "No open incidents" is news, not missing data. Only `top_n` with no ranked ports is `no_data`.

- [ ] **Step 1: Write the failing test**

`backend/internal/dashboards/widget_lists_db_test.go`:

```go
package dashboards

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBTopNWidget(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := topNWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices}
	site := newSite(t, db, "HQ")
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	p1, p2 := newPort(t, db, dev, 1, "Gi1/0/1", "printer"), newPort(t, db, dev, 2, "Gi1/0/2", "uplink")
	now := time.Now().UTC()
	writePortBps(t, d, dev, p1, 1, now.Add(-20*time.Minute), 100, 100)
	writePortBps(t, d, dev, p2, 2, now.Add(-20*time.Minute), 9000, 1000)
	in := ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now}
	data, err := resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s","range":"1h","n":5}`, site), in)
	testdb.Must(t, err)
	tn := data.(TopNData)
	if tn.Unit != "bps" || len(tn.Items) != 2 || tn.Items[0].Alias != "uplink" || tn.Items[0].Value != 10000 || tn.Items[0].DeviceID == nil {
		t.Errorf("top-N = %+v", tn)
	}
	in.Viewer = PublicViewer
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s","range":"1h","n":5}`, site), in)
	testdb.Must(t, err)
	if data.(TopNData).Items[0].DeviceID != nil {
		t.Error("public top-N carries device ids")
	}
}

func TestDBEventLogMergesIncidentsAndPortEvents(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := eventLogWidget{ports: d.Ports, incidents: d.Incidents}
	site := newSite(t, db, "HQ")
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	port := newPort(t, db, dev, 3, "Gi1/0/3", "")
	now := time.Now().UTC()
	testdb.Exec(t, db, `INSERT INTO port_events (device_id, interface_id, if_index, kind, started_at, detail) VALUES (?, ?, 3, 'link_down', ?, '{}')`,
		dev, port, now.Add(-30*time.Minute))
	end := now.Add(-5 * time.Minute)
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, start_time, end_time, severity, root_cause) VALUES (?, ?, ?, 'high', 'cannot reach 10.0.0.1')`,
		dev, now.Add(-20*time.Minute), end)

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"site_id":"%s","limit":10}`, site), ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Now: now})
	testdb.Must(t, err)
	items := data.(EventLogData).Items
	kinds := []string{}
	for _, it := range items {
		kinds = append(kinds, it.Kind)
	}
	if fmt.Sprint(kinds) != "[incident_closed incident_opened link_down]" {
		t.Errorf("kinds newest first = %v", kinds)
	}
	raw, _ := json.Marshal(data)
	if containsAny(string(raw), "cannot reach", "10.0.0.1") {
		t.Errorf("event log leaks free text or the host: %s", raw)
	}
}

func TestDBDeviceTableAndOpenIncidents(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	member := testdb.NewUser(t, db, false)
	site := newSite(t, db, "HQ")
	shareSite(t, db, site, member, "readonly")
	dev := newDevice(t, db, site, "core", "10.0.0.1")
	newDevice(t, db, site, "ap-1", "10.0.0.20")
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'access_point' WHERE name = 'ap-1'`)
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, start_time, severity) VALUES (?, ?, 'high')`, dev, time.Now().UTC().Add(-time.Hour))
	other := newDevice(t, db, newSite(t, db, "Other"), "edge", "10.9.0.1")
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, start_time, severity) VALUES (?, ?, 'low')`, other, time.Now().UTC().Add(-time.Hour))

	table := deviceTableWidget{devices: d.Devices, incidents: d.Incidents}
	data, err := resolveWidget(t, table, fmt.Sprintf(`{"site_id":"%s","types":["switch","other"]}`, site),
		ResolveInput{Visible: Subjects{Sites: []uuid.UUID{site}}, Viewer: PublicViewer})
	testdb.Must(t, err)
	rows := data.(DeviceTableData).Devices
	if len(rows) != 1 || rows[0].Name != "core" || rows[0].OpenIncidents != 1 || rows[0].Host != "" || rows[0].DeviceID != nil {
		t.Errorf("public device table filtered to switch/other = %+v", rows)
	}

	open := openIncidentsWidget{incidents: d.Incidents}
	data, err = resolveWidget(t, open, `{"scope":"all"}`, ResolveInput{Visible: Subjects{Broad: true}, Viewer: Viewer{UserID: member}})
	testdb.Must(t, err)
	if inc := data.(OpenIncidentsData); len(inc.Incidents) != 1 || inc.Incidents[0].SubjectName != "core" {
		t.Errorf("member's all-scope incidents = %+v, want only the shared site's", inc)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run to see it fail**

Run: `go_docker vet ./internal/dashboards/`
Expected: FAIL — `undefined: topNWidget`.

- [ ] **Step 3: Implement top-N**

`backend/internal/dashboards/widget_top_n.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type topNConfig struct {
	SiteID  *uuid.UUID  `json:"site_id,omitempty"`
	Devices []uuid.UUID `json:"devices,omitempty"`
	Measure string      `json:"measure"`
	N       int         `json:"n"`
	Range   string      `json:"range"`
}

// TopNItem is one ranked port.
type TopNItem struct {
	DeviceID   *uuid.UUID `json:"device_id,omitempty"`
	DeviceName string     `json:"device_name"`
	IfIndex    int        `json:"if_index"`
	PortName   string     `json:"port_name"`
	Alias      string     `json:"alias"`
	Value      float64    `json:"value"`
}

// TopNData is the ranking and the unit of its values.
type TopNData struct {
	Measure string     `json:"measure"`
	Unit    string     `json:"unit"`
	Range   string     `json:"range"`
	Items   []TopNItem `json:"items"`
}

var topNUnits = map[string]string{services.TopNTraffic: "bps", services.TopNUtilisation: "%", services.TopNErrors: "per_min"}

type topNWidget struct {
	metrics MetricsReader
	ports   PortReader
	devices DeviceReader
}

func (topNWidget) Type() string { return "top_n" }

func (topNWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c topNConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	c.Devices = dedupe(c.Devices)
	switch {
	case c.SiteID != nil && len(c.Devices) > 0:
		return nil, fieldErr("devices", "choose devices or a site, not both")
	case c.SiteID == nil:
		if err := checkCount("devices", len(c.Devices), 1, maxDevices); err != nil {
			return nil, err
		}
	}
	if c.Measure == "" {
		c.Measure = services.TopNTraffic
	}
	if _, ok := topNUnits[c.Measure]; !ok {
		return nil, fieldErr("measure", "must be traffic, utilisation or errors")
	}
	if c.N == 0 {
		c.N = 10
	}
	if err := checkCount("n", c.N, 5, 20); err != nil {
		return nil, err
	}
	var err error
	if c.Range, err = validateRange(c.Range); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

func (topNWidget) Subjects(raw json.RawMessage) Subjects {
	var c topNConfig
	_ = json.Unmarshal(raw, &c)
	if c.SiteID != nil {
		return Subjects{Sites: []uuid.UUID{*c.SiteID}}
	}
	return Subjects{Devices: c.Devices}
}

func (topNWidget) Refresh(raw json.RawMessage, override string) time.Duration {
	var c topNConfig
	_ = json.Unmarshal(raw, &c)
	return chartRefresh(effectiveRange(c.Range, override))
}

func (w topNWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c topNConfig
	_ = json.Unmarshal(raw, &c)
	ids := in.Visible.Devices
	if c.SiteID != nil {
		var err error
		if ids, err = siteDeviceIDs(ctx, w.devices, *c.SiteID); err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		return nil, ErrNoData
	}
	ifs, err := w.ports.PhysicalInterfaceIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(ifs) == 0 {
		return nil, ErrNoData
	}
	rng := effectiveRange(c.Range, in.Override)
	to := in.Now.UTC()
	rows, err := w.metrics.TopN(ctx, services.TopNQuery{DeviceIDs: ids, InterfaceIDs: ifs, Measure: c.Measure,
		From: to.Add(-rangeSpan(rng)), To: to, N: c.N})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNoData
	}
	out := TopNData{Measure: c.Measure, Unit: topNUnits[c.Measure], Range: rng, Items: make([]TopNItem, len(rows))}
	for i, r := range rows {
		out.Items[i] = TopNItem{DeviceID: idFor(in.Viewer, r.DeviceID), DeviceName: r.DeviceName, IfIndex: r.IfIndex,
			PortName: r.PortName, Alias: r.Alias, Value: r.Value}
	}
	return out, nil
}
```

- [ ] **Step 4: Implement the event log**

`backend/internal/dashboards/widget_event_log.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type eventLogConfig struct {
	SiteID   *uuid.UUID `json:"site_id,omitempty"`
	DeviceID *uuid.UUID `json:"device_id,omitempty"`
	Limit    int        `json:"limit"`
}

// EventItem is one line of the log: a port event, or an incident opening or
// closing. No free text (root causes, notes) is ever included.
type EventItem struct {
	Time       time.Time  `json:"time"`
	Kind       string     `json:"kind"`
	DeviceID   *uuid.UUID `json:"device_id,omitempty"`
	DeviceName string     `json:"device_name"`
	IfIndex    *int       `json:"if_index,omitempty"`
	PortLabel  string     `json:"port_label"`
	PortName   string     `json:"port_name"`
	Subject    string     `json:"subject"`
	Condition  string     `json:"condition"`
	Severity   string     `json:"severity"`
	IncidentID *uuid.UUID `json:"incident_id,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

// EventLogData is the newest events first.
type EventLogData struct {
	Items []EventItem `json:"items"`
}

type eventLogWidget struct {
	ports     PortReader
	incidents IncidentReader
}

func (eventLogWidget) Type() string { return "event_log" }

func (eventLogWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c eventLogConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	switch {
	case c.SiteID != nil && c.DeviceID != nil:
		return nil, fieldErr("device_id", "choose a site or a device, not both")
	case c.SiteID == nil && c.DeviceID == nil:
		return nil, fieldErr("site_id", "choose a site or a device")
	}
	if c.Limit == 0 {
		c.Limit = 20
	}
	if err := checkCount("limit", c.Limit, 10, 50); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

func (eventLogWidget) Subjects(raw json.RawMessage) Subjects {
	var c eventLogConfig
	_ = json.Unmarshal(raw, &c)
	switch {
	case c.SiteID != nil:
		return Subjects{Sites: []uuid.UUID{*c.SiteID}}
	case c.DeviceID != nil:
		return Subjects{Devices: []uuid.UUID{*c.DeviceID}}
	}
	return Subjects{}
}

func (eventLogWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

func (w eventLogWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c eventLogConfig
	_ = json.Unmarshal(raw, &c)
	filter := services.PortEventFilter{SiteID: c.SiteID, DeviceID: c.DeviceID, Page: 1, Limit: c.Limit}
	opts := services.IncidentListOptions{
		// The site or device was access-checked; every incident of its
		// devices is visible to whoever can see it.
		Viewer: &services.IncidentViewer{IsAdmin: true},
		SiteID: c.SiteID, DeviceID: c.DeviceID, Page: 1, Limit: c.Limit, Subject: "device", Desc: true,
	}
	events, _, err := w.ports.Events(ctx, filter)
	if err != nil {
		return nil, err
	}
	incs, _, err := w.incidents.ListIncidents(ctx, opts)
	if err != nil {
		return nil, err
	}
	items := make([]EventItem, 0, len(events)+2*len(incs))
	for _, e := range events {
		ifIndex := e.IfIndex
		items = append(items, EventItem{Time: e.StartedAt, Kind: e.Kind, DeviceID: idFor(in.Viewer, e.DeviceID),
			DeviceName: e.DeviceName, IfIndex: &ifIndex, PortLabel: e.PortLabel, PortName: e.PortName, EndedAt: e.EndedAt})
	}
	for _, inc := range incs {
		base := EventItem{Subject: inc.SubjectName, Severity: inc.Severity, IncidentID: idFor(in.Viewer, inc.ID),
			IfIndex: inc.PortIfIndex}
		if inc.DeviceID != nil {
			base.DeviceID = idFor(in.Viewer, *inc.DeviceID)
		}
		if inc.Condition != nil {
			base.Condition = *inc.Condition
		}
		opened := base
		opened.Time, opened.Kind = inc.StartTime, "incident_opened"
		items = append(items, opened)
		if inc.EndTime != nil {
			closed := base
			closed.Time, closed.Kind = *inc.EndTime, "incident_closed"
			items = append(items, closed)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Time.After(items[j].Time) })
	if len(items) > c.Limit {
		items = items[:c.Limit]
	}
	return EventLogData{Items: items}, nil
}
```

- [ ] **Step 5: Implement the device table**

`backend/internal/dashboards/widget_device_table.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

var deviceStatuses = map[string]bool{"pending": true, "up": true, "down": true, "paused": true, "error": true}

type deviceTableConfig struct {
	SiteID   uuid.UUID `json:"site_id"`
	Types    []string  `json:"types,omitempty"`
	Statuses []string  `json:"statuses,omitempty"`
}

// DeviceRow is one device. Host is set only for logged-in viewers.
type DeviceRow struct {
	DeviceID        *uuid.UUID `json:"device_id,omitempty"`
	Name            string     `json:"name"`
	Host            string     `json:"host,omitempty"`
	Type            string     `json:"type"`
	Status          string     `json:"status"`
	VendorModel     string     `json:"vendor_model"`
	LastSeenAt      *time.Time `json:"last_seen_at"`
	Availability30d *float64   `json:"availability_30d"`
	OpenIncidents   int        `json:"open_incidents"`
}

// DeviceTableData is a site's devices, sorted as the devices list sorts them.
type DeviceTableData struct {
	Devices []DeviceRow `json:"devices"`
}

type deviceTableWidget struct {
	devices   DeviceReader
	incidents IncidentReader
}

func (deviceTableWidget) Type() string { return "device_table" }

func (deviceTableWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c deviceTableConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	if c.SiteID == uuid.Nil {
		return nil, fieldErr("site_id", "is required")
	}
	for _, t := range c.Types {
		if !models.ValidDeviceTypes[t] {
			return nil, fieldErr("types", "include "+t+", which is not a device type")
		}
	}
	for _, s := range c.Statuses {
		if !deviceStatuses[s] {
			return nil, fieldErr("statuses", "include "+s+", which is not a device status")
		}
	}
	return json.Marshal(c)
}

func (deviceTableWidget) Subjects(raw json.RawMessage) Subjects {
	var c deviceTableConfig
	_ = json.Unmarshal(raw, &c)
	if c.SiteID == uuid.Nil {
		return Subjects{}
	}
	return Subjects{Sites: []uuid.UUID{c.SiteID}}
}

func (deviceTableWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

func (w deviceTableWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c deviceTableConfig
	_ = json.Unmarshal(raw, &c)
	if len(in.Visible.Sites) == 0 {
		return nil, ErrNoData
	}
	site := in.Visible.Sites[0]
	list, err := w.devices.List(ctx, uuid.Nil, true, services.DeviceFilter{SiteID: &site})
	if err != nil {
		return nil, err
	}
	keep := func(set []string, v string) bool {
		if len(set) == 0 {
			return true
		}
		for _, s := range set {
			if s == v {
				return true
			}
		}
		return false
	}
	var rows []DeviceRow
	var ids []uuid.UUID
	for _, d := range list {
		if !keep(c.Types, d.EffectiveType) || !keep(c.Statuses, d.Status) {
			continue
		}
		row := DeviceRow{DeviceID: idFor(in.Viewer, d.ID), Name: d.Name, Type: d.EffectiveType, Status: d.Status,
			VendorModel:     strings.TrimSpace(d.EffectiveVendor + " " + d.EffectiveModel),
			LastSeenAt:      d.LastSeenAt, Availability30d: d.Availability30d}
		if !in.Viewer.Public {
			row.Host = d.Host
		}
		rows = append(rows, row)
		ids = append(ids, d.ID)
	}
	counts, err := w.incidents.OpenCountsByDevice(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].OpenIncidents = counts[ids[i]]
	}
	if rows == nil {
		rows = []DeviceRow{}
	}
	return DeviceTableData{Devices: rows}, nil
}
```

- [ ] **Step 6: Implement open incidents**

`backend/internal/dashboards/widget_open_incidents.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type openIncidentsConfig struct {
	Scope    string      `json:"scope"`
	SiteID   *uuid.UUID  `json:"site_id,omitempty"`
	Devices  []uuid.UUID `json:"devices,omitempty"`
	Monitors []uuid.UUID `json:"monitors,omitempty"`
	Limit    int         `json:"limit"`
}

// IncidentRow is one open incident: who, what and since when. No root
// cause, notes, URLs or hosts.
type IncidentRow struct {
	IncidentID      *uuid.UUID `json:"incident_id,omitempty"`
	SubjectType     string     `json:"subject_type"`
	SubjectName     string     `json:"subject_name"`
	SiteName        string     `json:"site_name"`
	Condition       string     `json:"condition"`
	Severity        string     `json:"severity"`
	StartTime       time.Time  `json:"start_time"`
	DurationSeconds int        `json:"duration_seconds"`
	DeviceID        *uuid.UUID `json:"device_id,omitempty"`
	MonitorID       *uuid.UUID `json:"monitor_id,omitempty"`
	PortIfIndex     *int       `json:"port_if_index,omitempty"`
}

// OpenIncidentsData is the newest open incidents and how many there are.
type OpenIncidentsData struct {
	Incidents []IncidentRow `json:"incidents"`
	Total     int64         `json:"total"`
}

type openIncidentsWidget struct {
	incidents IncidentReader
}

// incidentViewer is v for the incident list; a public viewer reads as an
// admin, since an admin published the dashboard.
func incidentViewer(v Viewer) *services.IncidentViewer {
	if v.Public {
		return &services.IncidentViewer{IsAdmin: true}
	}
	return &services.IncidentViewer{UserID: v.UserID, IsAdmin: v.IsAdmin}
}

func (openIncidentsWidget) Type() string { return "open_incidents" }

func (openIncidentsWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c openIncidentsConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	c.Devices, c.Monitors = dedupe(c.Devices), dedupe(c.Monitors)
	switch c.Scope {
	case "site":
		if c.SiteID == nil {
			return nil, fieldErr("site_id", "is required")
		}
		c.Devices, c.Monitors = nil, nil
	case "devices":
		if err := checkCount("devices", len(c.Devices), 1, maxDevices); err != nil {
			return nil, err
		}
		c.SiteID, c.Monitors = nil, nil
	case "monitors":
		if err := checkCount("monitors", len(c.Monitors), 1, maxMonitors); err != nil {
			return nil, err
		}
		c.SiteID, c.Devices = nil, nil
	case "all":
		c.SiteID, c.Devices, c.Monitors = nil, nil, nil
	default:
		return nil, fieldErr("scope", "must be site, devices, monitors or all")
	}
	if c.Limit == 0 {
		c.Limit = 20
	}
	if err := checkCount("limit", c.Limit, 5, 50); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

func (openIncidentsWidget) Subjects(raw json.RawMessage) Subjects {
	var c openIncidentsConfig
	_ = json.Unmarshal(raw, &c)
	switch c.Scope {
	case "site":
		if c.SiteID != nil {
			return Subjects{Sites: []uuid.UUID{*c.SiteID}}
		}
	case "devices":
		return Subjects{Devices: c.Devices}
	case "monitors":
		return Subjects{Monitors: c.Monitors}
	case "all":
		return Subjects{Broad: true}
	}
	return Subjects{}
}

func (openIncidentsWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

func (w openIncidentsWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c openIncidentsConfig
	_ = json.Unmarshal(raw, &c)
	opts := services.IncidentListOptions{Viewer: incidentViewer(in.Viewer), Status: models.IncidentStatusOngoing,
		Page: 1, Limit: c.Limit, Desc: true}
	switch c.Scope {
	case "site":
		opts.SiteID = c.SiteID
	case "devices":
		opts.DeviceIDs = in.Visible.Devices
	case "monitors":
		opts.MonitorIDs = in.Visible.Monitors
	}
	rows, total, err := w.incidents.ListIncidents(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := OpenIncidentsData{Incidents: make([]IncidentRow, 0, len(rows)), Total: total}
	for _, r := range rows {
		row := IncidentRow{IncidentID: idFor(in.Viewer, r.ID), SubjectType: r.SubjectType, SubjectName: r.SubjectName,
			SiteName: r.SiteName, Severity: r.Severity, StartTime: r.StartTime,
			DurationSeconds: int(in.Now.Sub(r.StartTime).Seconds()), PortIfIndex: r.PortIfIndex}
		if r.Condition != nil {
			row.Condition = *r.Condition
		}
		if r.DeviceID != nil {
			row.DeviceID = idFor(in.Viewer, *r.DeviceID)
		}
		if r.MonitorID != nil {
			row.MonitorID = idFor(in.Viewer, *r.MonitorID)
		}
		out.Incidents = append(out.Incidents, row)
	}
	return out, nil
}
```

In `deps.go`, add to `NewDefaultRegistry` after `sitePowerWidget{...}`:

```go
		topNWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices},
		eventLogWidget{ports: d.Ports, incidents: d.Incidents},
		deviceTableWidget{devices: d.Devices, incidents: d.Incidents},
		openIncidentsWidget{incidents: d.Incidents},
```

- [ ] **Step 7: Run to see them pass**

Run: `db_up; go_docker test ./internal/dashboards/ -run 'TestDBTopNWidget|TestDBEventLog|TestDBDeviceTable' -v; db_down`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/dashboards
git commit -m "feat(dashboards): top-N, event log, device table and open incidents widgets

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 18: The monitors widget

**Files:**
- Create: `backend/internal/dashboards/widget_monitors.go`
- Modify: `backend/internal/dashboards/deps.go` (register it)
- Test: `backend/internal/dashboards/widget_monitors_db_test.go`

**Interfaces:**
- Consumes: `MonitorReader.MonitorsByIDs|GetMaintenanceHistory`, `CheckReader.GetChecksInRange`, `IncidentReader.GetOverlappingIncidents`, `AgentReader.List`, `services.HourlyUptimeBuckets`, `services.DailyUptimeBuckets`.
- Produces: `monitorsWidget{monitors MonitorReader; checks CheckReader; incidents IncidentReader; agents AgentReader}` → `MonitorsData{Style, Window string; Monitors []MonitorRow; Agents []AgentRow}`; `MonitorRow{MonitorID *uuid.UUID; Name, Type, Status string; ResponseTimeMs int; LastCheckAt *time.Time; Buckets []UptimeBucket}`; `AgentRow{AgentID *uuid.UUID; Name, Status string; LastHeartbeat *time.Time}`; `UptimeBucket{Label, Status string; Uptime float64}`.

Status: a disabled monitor reads `paused`; otherwise its `current_status`. Rows keep the config's order. Refresh: 15 min for `bars` + `90d`, else 30 s (plan deviation 3).

- [ ] **Step 1: Write the failing test**

`backend/internal/dashboards/widget_monitors_db_test.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBMonitorsWidget(t *testing.T) {
	db := testdb.Open(t)
	d := realDeps(t, db)
	w := monitorsWidget{monitors: d.Monitors, checks: d.Checks, incidents: d.Incidents, agents: d.Agents}
	owner := testdb.NewUser(t, db, true)
	m1 := newMonitor(t, db, owner, "Website", "https://intranet.secret.test")
	m2 := newMonitor(t, db, owner, "Mail", "https://mail.secret.test")
	testdb.Exec(t, db, `UPDATE monitors SET enabled = false WHERE id = ?`, m2)
	agent := newAgent(t, db, "fileserver")
	now := time.Now().UTC()
	testdb.Exec(t, db, `INSERT INTO checks (monitor_id, status, response_time_ms, timestamp) VALUES (?, 'success', 40, ?), (?, 'failed', 0, ?)`,
		m1, now.Add(-2*time.Hour), m1, now.Add(-90*time.Minute))
	in := ResolveInput{Visible: Subjects{Monitors: []uuid.UUID{m2, m1}, Agents: []uuid.UUID{agent}}, Now: now}

	data, err := resolveWidget(t, w, fmt.Sprintf(`{"monitors":["%s","%s"],"agents":["%s"],"style":"bars","window":"24h"}`, m2, m1, agent), in)
	testdb.Must(t, err)
	md := data.(MonitorsData)
	if len(md.Monitors) != 2 || md.Monitors[0].Name != "Mail" || md.Monitors[0].Status != "paused" || len(md.Monitors[1].Buckets) != 24 {
		t.Errorf("monitors = %+v, want config order, paused for the disabled one, 24 hourly buckets", md.Monitors)
	}
	if len(md.Agents) != 1 || md.Agents[0].Name != "fileserver" {
		t.Errorf("agents = %+v", md.Agents)
	}
	in.Viewer = PublicViewer
	data, err = resolveWidget(t, w, fmt.Sprintf(`{"monitors":["%s"],"style":"bars","window":"90d"}`, m1), in)
	testdb.Must(t, err)
	raw, _ := json.Marshal(data)
	if containsAny(string(raw), "secret.test", m1.String()) {
		t.Errorf("public monitors data leaks the URL or id: %s", raw)
	}
	if b := data.(MonitorsData).Monitors[0].Buckets; len(b) != 90 {
		t.Errorf("90-day window has %d buckets, want 90", len(b))
	}
	clean, _ := w.Validate(context.Background(), json.RawMessage(fmt.Sprintf(`{"monitors":["%s"],"style":"bars","window":"90d"}`, m1)))
	if w.Refresh(clean, "") != 15*time.Minute {
		t.Error("90-day bars should refresh every 15 minutes")
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `go_docker vet ./internal/dashboards/`
Expected: FAIL — `undefined: monitorsWidget`.

- [ ] **Step 3: Implement**

`backend/internal/dashboards/widget_monitors.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type monitorsConfig struct {
	Monitors []uuid.UUID `json:"monitors,omitempty"`
	Agents   []uuid.UUID `json:"agents,omitempty"`
	Style    string      `json:"style"`
	Window   string      `json:"window"`
}

// UptimeBucket is one bar: an hour (24h) or a day (90d).
type UptimeBucket struct {
	Label  string  `json:"label"`
	Status string  `json:"status"`
	Uptime float64 `json:"uptime"`
}

// MonitorRow is one monitor. Its URL and target are never included.
type MonitorRow struct {
	MonitorID      *uuid.UUID     `json:"monitor_id,omitempty"`
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Status         string         `json:"status"`
	ResponseTimeMs int            `json:"response_time_ms"`
	LastCheckAt    *time.Time     `json:"last_check_at"`
	Buckets        []UptimeBucket `json:"buckets,omitempty"`
}

// AgentRow is one server agent: its name and whether it is reporting.
type AgentRow struct {
	AgentID       *uuid.UUID `json:"agent_id,omitempty"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	LastHeartbeat *time.Time `json:"last_heartbeat"`
}

// MonitorsData is the chosen monitors and agents, in config order.
type MonitorsData struct {
	Style    string       `json:"style"`
	Window   string       `json:"window"`
	Monitors []MonitorRow `json:"monitors"`
	Agents   []AgentRow   `json:"agents"`
}

type monitorsWidget struct {
	monitors  MonitorReader
	checks    CheckReader
	incidents IncidentReader
	agents    AgentReader
}

func (monitorsWidget) Type() string { return "monitors" }

func (monitorsWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c monitorsConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	c.Monitors, c.Agents = dedupe(c.Monitors), dedupe(c.Agents)
	if len(c.Monitors)+len(c.Agents) == 0 {
		return nil, fieldErr("monitors", "choose at least one monitor or server")
	}
	if err := checkCount("monitors", len(c.Monitors), 0, maxMonitors); err != nil {
		return nil, err
	}
	if err := checkCount("agents", len(c.Agents), 0, maxAgents); err != nil {
		return nil, err
	}
	switch c.Style {
	case "":
		c.Style = "list"
	case "list", "bars":
	default:
		return nil, fieldErr("style", "must be list or bars")
	}
	switch c.Window {
	case "":
		c.Window = "24h"
	case "24h", "90d":
	default:
		return nil, fieldErr("window", "must be 24h or 90d")
	}
	return json.Marshal(c)
}

func (monitorsWidget) Subjects(raw json.RawMessage) Subjects {
	var c monitorsConfig
	_ = json.Unmarshal(raw, &c)
	return Subjects{Monitors: c.Monitors, Agents: c.Agents}
}

func (monitorsWidget) Refresh(raw json.RawMessage, _ string) time.Duration {
	var c monitorsConfig
	_ = json.Unmarshal(raw, &c)
	if c.Style == "bars" && c.Window == "90d" {
		return 15 * time.Minute
	}
	return statusRefresh
}

func (w monitorsWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c monitorsConfig
	_ = json.Unmarshal(raw, &c)
	out := MonitorsData{Style: c.Style, Window: c.Window, Monitors: []MonitorRow{}, Agents: []AgentRow{}}

	if len(in.Visible.Monitors) > 0 {
		list, err := w.monitors.MonitorsByIDs(ctx, in.Visible.Monitors)
		if err != nil {
			return nil, err
		}
		byID := make(map[uuid.UUID]*models.Monitor, len(list))
		for i := range list {
			byID[list[i].ID] = &list[i]
		}
		for _, id := range in.Visible.Monitors {
			m := byID[id]
			if m == nil {
				continue
			}
			row := MonitorRow{MonitorID: idFor(in.Viewer, m.ID), Name: m.Name, Type: m.Type, Status: m.CurrentStatus,
				ResponseTimeMs: m.LastResponseTimeMs, LastCheckAt: m.LastCheckAt}
			if !m.Enabled {
				row.Status = "paused"
			}
			if c.Style == "bars" {
				b, err := w.buckets(ctx, m, c.Window, in.Now.UTC())
				if err != nil {
					return nil, err
				}
				row.Buckets = b
			}
			out.Monitors = append(out.Monitors, row)
		}
	}

	if len(in.Visible.Agents) > 0 {
		all, err := w.agents.List(ctx)
		if err != nil {
			return nil, err
		}
		byID := make(map[uuid.UUID]models.Agent, len(all))
		for _, a := range all {
			byID[a.ID] = a
		}
		for _, id := range in.Visible.Agents {
			a, ok := byID[id]
			if !ok {
				continue
			}
			out.Agents = append(out.Agents, AgentRow{AgentID: idFor(in.Viewer, a.ID), Name: a.Name, Status: a.Status, LastHeartbeat: a.LastHeartbeat})
		}
	}
	return out, nil
}

// buckets is a monitor's uptime bars, from the same helpers the status page
// and the monitor page use.
func (w monitorsWidget) buckets(ctx context.Context, m *models.Monitor, window string, now time.Time) ([]UptimeBucket, error) {
	if window == "90d" {
		checks, err := w.checks.GetChecksInRange(ctx, m.ID, now.AddDate(0, 0, -services.UptimeDailyDays), now, 0, 0)
		if err != nil {
			return nil, err
		}
		return toBuckets(services.DailyUptimeBuckets(checks, now), "date"), nil
	}
	start := now.Add(-24 * time.Hour)
	checks, err := w.checks.GetChecksInRange(ctx, m.ID, start, now, 0, 0)
	if err != nil {
		return nil, err
	}
	incidents, err := w.incidents.GetOverlappingIncidents(ctx, m.ID, start, now)
	if err != nil {
		return nil, err
	}
	maint, err := w.monitors.GetMaintenanceHistory(ctx, m.ID, start, now)
	if err != nil {
		return nil, err
	}
	return toBuckets(services.HourlyUptimeBuckets(checks, incidents, maint, now, m.CreatedAt), "bucket_start"), nil
}

func toBuckets(entries []map[string]any, labelKey string) []UptimeBucket {
	out := make([]UptimeBucket, len(entries))
	for i, e := range entries {
		label, _ := e[labelKey].(string)
		status, _ := e["status"].(string)
		uptime, _ := e["uptime"].(float64)
		out[i] = UptimeBucket{Label: label, Status: status, Uptime: uptime}
	}
	return out
}
```

In `deps.go`, add to `NewDefaultRegistry` after `openIncidentsWidget{...}`:

```go
		monitorsWidget{monitors: d.Monitors, checks: d.Checks, incidents: d.Incidents, agents: d.Agents},
```

- [ ] **Step 4: Run to see it pass**

Run: `db_up; go_docker test ./internal/dashboards/ -run TestDBMonitorsWidget -v; db_down`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/dashboards
git commit -m "feat(dashboards): monitors widget with status lists and uptime bars

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 19: Standard site widgets and the public leak test

**Files:**
- Create: `backend/internal/dashboards/starter.go`
- Modify: `backend/internal/dashboards/service.go` (`CreateInput.Starter`; `Create` inserts the standard widgets)
- Test: `backend/internal/dashboards/starter_db_test.go`, `backend/internal/dashboards/public_leak_db_test.go`

**Interfaces:**
- Consumes: `Service.validateWidget`, `NewDefaultRegistry`, `NewResolver`, every widget type.
- Produces: `starterWidgets(siteID uuid.UUID) []WidgetInput`; `CreateInput.Starter bool` (JSON `starter`).

- [ ] **Step 1: Write the failing tests**

`backend/internal/dashboards/starter_db_test.go`:

```go
package dashboards

import (
	"context"
	"errors"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBCreateWithStandardWidgets(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	site := newSite(t, db, "HQ")
	sites := services.NewSiteService(db)
	checker := NewDBChecker(db, sites, services.NewMonitorService(db))
	svc := NewService(db, sites, NewDefaultRegistry(realDeps(t, db)), checker)

	d, err := svc.Create(ctx, Viewer{UserID: admin, IsAdmin: true}, CreateInput{Name: "HQ", SiteID: &site, Starter: true})
	testdb.Must(t, err)
	types := map[string]bool{}
	for _, w := range d.Widgets {
		types[w.Type] = true
	}
	for _, want := range []string{"timeseries", "site_power", "open_incidents", "top_n", "device_table"} {
		if !types[want] {
			t.Errorf("standard widgets lack %s: %+v", want, d.Widgets)
		}
	}
	if _, err := svc.Create(ctx, Viewer{UserID: admin, IsAdmin: true}, CreateInput{Name: "Mine", Starter: true}); !errors.Is(err, ErrInvalid) {
		t.Errorf("standard widgets on a personal dashboard: err = %v, want ErrInvalid", err)
	}
}
```

`backend/internal/dashboards/public_leak_db_test.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// TestDBPublicLinkLeaksNothing builds a dashboard with one widget of every
// registered type over subjects full of sensitive strings, publishes it, and
// checks that no public response contains any of them. A new widget type
// fails here until it is given a config below.
func TestDBPublicLinkLeaksNothing(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	deps := realDeps(t, db)
	registry := NewDefaultRegistry(deps)
	sites := services.NewSiteService(db)
	checker := NewDBChecker(db, sites, services.NewMonitorService(db))
	svc := NewService(db, sites, registry, checker)
	resolver := NewResolver(registry, checker)

	admin := testdb.NewUser(t, db, true)
	site := newSite(t, db, "HQ")
	dev := newDevice(t, db, site, "core", "10.77.0.9")
	testdb.Exec(t, db, `UPDATE devices SET status = 'down', status_detail = 'dial udp 10.77.0.9:161: i/o timeout' WHERE id = ?`, dev)
	port := newPort(t, db, dev, 1, "Gi1/0/1", "uplink")
	testdb.Exec(t, db, `UPDATE device_interfaces SET mac = 'aa:bb:cc:dd:ee:ff' WHERE id = ?`, port)
	ups := newDevice(t, db, site, "ups-1", "10.77.0.50")
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'ups' WHERE id = ?`, ups)
	mon := newMonitor(t, db, admin, "Intranet", "https://intranet.secret.test/health")
	agent := newAgent(t, db, "fileserver")
	now := time.Now().UTC()
	writePortBps(t, deps, dev, port, 1, now.Add(-10*time.Minute), 5000, 100)
	m := deps.Metrics.(*services.MetricsStore)
	testdb.Must(t, m.Write(ctx, ups, now.Add(-time.Minute), []services.SamplePoint{{Metric: services.MetricUPSChargePct, Value: 90}}))
	incident := uuid.New()
	testdb.Exec(t, db, `INSERT INTO incidents (id, device_id, start_time, severity, root_cause, notes) VALUES (?, ?, ?, 'high', 'cannot reach 10.77.0.9', 'call Bob at x123')`,
		incident, dev, now.Add(-time.Hour))
	testdb.Exec(t, db, `INSERT INTO port_events (device_id, interface_id, if_index, kind, started_at, detail) VALUES (?, ?, 1, 'link_down', ?, '{}')`,
		dev, port, now.Add(-30*time.Minute))

	configs := map[string]string{
		"label":          `{"text":"HQ"}`,
		"timeseries":     fmt.Sprintf(`{"metrics":["if_in_bps"],"devices":["%s"],"range":"1h"}`, dev),
		"stat":           fmt.Sprintf(`{"metric":"if_in_bps","device_id":"%s","range":"1h"}`, dev),
		"port_grid":      fmt.Sprintf(`{"device_id":"%s"}`, dev),
		"device_health":  fmt.Sprintf(`{"device_id":"%s"}`, dev),
		"site_power":     fmt.Sprintf(`{"site_id":"%s"}`, site),
		"top_n":          fmt.Sprintf(`{"site_id":"%s","measure":"traffic","n":5,"range":"1h"}`, site),
		"event_log":      fmt.Sprintf(`{"site_id":"%s","limit":10}`, site),
		"device_table":   fmt.Sprintf(`{"site_id":"%s"}`, site),
		"open_incidents": `{"scope":"all","limit":10}`,
		"monitors":       fmt.Sprintf(`{"monitors":["%s"],"agents":["%s"],"style":"bars","window":"24h"}`, mon, agent),
	}
	dash := newDashboard(t, db, "Wall", &site, nil)
	for _, typ := range registry.Types() {
		cfg, ok := configs[typ]
		if !ok {
			t.Fatalf("widget type %q has no config in this test; add one so the public trim is checked for it", typ)
		}
		wd, _ := registry.Get(typ)
		clean, err := wd.Validate(ctx, json.RawMessage(cfg))
		testdb.Must(t, err)
		newWidget(t, db, dash, typ, string(clean))
	}
	publish(t, db, dash, admin)

	secrets := []string{"10.77.0.9", "10.77.0.50", "i/o timeout", "intranet.secret.test", "cannot reach", "call Bob",
		"aa:bb:cc:dd:ee:ff", site.String(), dev.String(), ups.String(), port.String(), mon.String(), agent.String(), incident.String()}

	d, err := svc.Get(ctx, Viewer{UserID: admin, IsAdmin: true}, dash)
	testdb.Must(t, err)
	layout, err := svc.PublicLayout(ctx, &d.Dashboard)
	testdb.Must(t, err)
	check := func(what string, v any) {
		raw, err := json.Marshal(v)
		testdb.Must(t, err)
		for _, s := range secrets {
			if strings.Contains(string(raw), s) {
				t.Errorf("%s leaks %q: %s", what, s, raw)
			}
		}
	}
	check("public layout", layout)
	for _, pw := range layout.Widgets {
		w, err := svc.PublicWidget(ctx, &d.Dashboard, pw.ID)
		testdb.Must(t, err)
		resp, err := resolver.ResolvePublic(ctx, &d.Dashboard, w)
		testdb.Must(t, err)
		check(pw.Type+" widget", resp)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `db_up; go_docker test ./internal/dashboards/ -run 'TestDBCreateWithStandardWidgets|TestDBPublicLinkLeaksNothing' -v; db_down`
Expected: `TestDBCreateWithStandardWidgets` FAILS — `unknown field Starter`. `TestDBPublicLinkLeaksNothing` should PASS already if the widgets from Tasks 15–18 trim correctly; if it fails, the message names the widget and the leaked string — fix that widget, not the test.

- [ ] **Step 3: Implement the standard widgets**

`backend/internal/dashboards/starter.go`:

```go
package dashboards

import (
	"encoding/json"

	"github.com/google/uuid"
)

// starterWidgets are the standard widgets a new site dashboard can start
// with (spec §3): internet traffic and power on top, open incidents and the
// busiest ports below, then the site's devices.
func starterWidgets(siteID uuid.UUID) []WidgetInput {
	cfg := func(v any) json.RawMessage {
		b, _ := json.Marshal(v)
		return b
	}
	return []WidgetInput{
		{Type: "timeseries", Title: "Internet traffic", X: 0, Y: 0, W: 8, H: 4,
			Config: cfg(map[string]any{"source": sourceSiteTraffic, "site_id": siteID, "view": "internet", "range": "24h"})},
		{Type: "site_power", Title: "Power", X: 8, Y: 0, W: 4, H: 4,
			Config: cfg(map[string]any{"site_id": siteID})},
		{Type: "open_incidents", Title: "Open incidents", X: 0, Y: 4, W: 6, H: 4,
			Config: cfg(map[string]any{"scope": "site", "site_id": siteID, "limit": 20})},
		{Type: "top_n", Title: "Busiest ports", X: 6, Y: 4, W: 6, H: 4,
			Config: cfg(map[string]any{"site_id": siteID, "measure": "traffic", "n": 10, "range": "24h"})},
		{Type: "device_table", Title: "Devices", X: 0, Y: 8, W: 12, H: 5,
			Config: cfg(map[string]any{"site_id": siteID})},
	}
}
```

In `service.go`, add the field to `CreateInput`:

```go
	// Starter fills a new site dashboard with the standard widgets.
	Starter bool `json:"starter"`
```

and replace the end of `Create`, from `if err := s.db.WithContext(ctx).Create(&d).Error; err != nil {` to `return s.Get(ctx, v, d.ID)`, with:

```go
	var starter []models.DashboardWidget
	if in.Starter {
		if d.SiteID == nil {
			return nil, invalid("the standard widgets are for site dashboards")
		}
		for i, w := range starterWidgets(*d.SiteID) {
			clean, err := s.validateWidget(ctx, v, i, w)
			if err != nil {
				return nil, fmt.Errorf("standard widget %d: %w", i, err)
			}
			starter = append(starter, clean)
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&d).Error; err != nil {
			return fmt.Errorf("creating dashboard: %w", err)
		}
		for i := range starter {
			starter[i].ID = uuid.New()
			starter[i].DashboardID = d.ID
			if err := tx.Create(&starter[i]).Error; err != nil {
				return fmt.Errorf("adding standard widget: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, v, d.ID)
```

(`Create` already declares `err` from `normalizeNames`; keep that declaration.)

- [ ] **Step 4: Run the whole package**

Run: `db_up; go_docker test ./internal/dashboards/... -v; db_down`
Expected: every test PASSES, including `TestDBPublicLinkLeaksNothing` and `TestDBCreateWithStandardWidgets`.

- [ ] **Step 5: Add the simulator end-to-end test (spec, Testing)**

`backend/internal/dashboards/widget_sim_db_test.go` — the port grid from a real inventory of the simulated EdgeSwitch, and a time series and device health from a real Cisco health profile poll (gauges, so no counter deltas are needed):

```go
package dashboards

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

type simNotifier struct{}

func (simNotifier) SendNotification(context.Context, *notifications.NotificationMessage) error { return nil }

func TestDBSimWidgetsEndToEnd(t *testing.T) {
	addr := os.Getenv("SENTINEL_TEST_SNMPSIM")
	if addr == "" {
		t.Skip("SENTINEL_TEST_SNMPSIM not set; start deploy/snmpsim/run.sh")
	}
	host, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)
	target := func(community string) snmp.Target {
		return snmp.Target{Host: host, Port: uint16(port), Credential: snmp.Credential{Version: "2c", Community: community}, Timeout: 2 * time.Second, Retries: 1}
	}
	db := testdb.Open(t)
	ctx := context.Background()
	d := realDeps(t, db)
	site := newSite(t, db, "Sim")

	edge := newDevice(t, db, site, "sim-edge", host)
	inv, err := snmp.ReadInventory(ctx, snmp.GoSNMPClient{}, target("edgeswitch"))
	testdb.Must(t, err)
	testdb.Must(t, d.Devices.(*services.DeviceService).SaveInventory(ctx, edge, inv, time.Now()))
	data, err := resolveWidget(t, portGridWidget{devices: d.Devices, ports: d.Ports}, fmt.Sprintf(`{"device_id":"%s"}`, edge),
		ResolveInput{Visible: Subjects{Devices: []uuid.UUID{edge}}})
	testdb.Must(t, err)
	if pg := data.(PortGridData); len(pg.Ports) == 0 || len(pg.Faceplates) == 0 || pg.Ports[0].Name == "" {
		t.Errorf("port grid from the simulated EdgeSwitch = %d ports, %d faceplates", len(pg.Ports), len(pg.Faceplates))
	}

	lib := services.NewMIBLibrary(db)
	testdb.Must(t, lib.SyncBuiltins(ctx))
	profiles := services.NewProfileService(db)
	testdb.Must(t, profiles.SeedStarter(ctx))
	testdb.Must(t, profiles.Load(ctx))
	cisco := newDevice(t, db, site, "sim-cisco", host)
	testdb.Exec(t, db, `UPDATE devices SET sys_object_id = '1.3.6.1.4.1.9.1.1208' WHERE id = ?`, cisco)
	var dev models.Device
	testdb.Must(t, db.First(&dev, "id = ?", cisco).Error)
	mon := services.NewProfileMonitor(profiles, d.Metrics.(*services.MetricsStore), services.NewIncidentService(db), simNotifier{},
		snmp.GoSNMPClient{}, d.Ports.(*services.PortService))
	mon.PollProfiles(ctx, dev, target("cisco"), -1)

	in := ResolveInput{Visible: Subjects{Devices: []uuid.UUID{cisco}}}
	data, err = resolveWidget(t, timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices},
		fmt.Sprintf(`{"metrics":["cisco_cpu_5min"],"devices":["%s"],"instances":["1"],"range":"1h"}`, cisco), in)
	testdb.Must(t, err)
	ts := data.(TimeseriesData)
	if len(ts.Lines) != 1 || len(ts.Lines[0].Points) == 0 || ts.Lines[0].Points[len(ts.Lines[0].Points)-1].Avg != 23 || !strings.Contains(ts.Lines[0].Label, "Switch 1") {
		t.Errorf("CPU of switch 1 = %+v, want the simulator's 23 labelled Switch 1", ts.Lines)
	}
	data, err = resolveWidget(t, deviceHealthWidget{devices: d.Devices, health: d.Health}, fmt.Sprintf(`{"device_id":"%s"}`, cisco), in)
	testdb.Must(t, err)
	if len(data.(DeviceHealthData).Metrics) == 0 {
		t.Error("device health from the simulated Cisco has no metrics")
	}
}
```

Run it with the simulator on the same Docker network as the test container:

```bash
db_up
(cd ../deploy/snmpsim && SNMPSIM_VERSION=1.2.2 ./run.sh) && docker network connect "$GO_NET" sentinel-snmpsim
export SENTINEL_TEST_SNMPSIM=sentinel-snmpsim:1161
go_docker test ./internal/dashboards/ -run TestDBSimWidgetsEndToEnd -v
docker rm -f sentinel-snmpsim; unset SENTINEL_TEST_SNMPSIM; db_down
```

Expected: PASS. Without `SENTINEL_TEST_SNMPSIM` it skips, so the normal suite does not need the simulator.

- [ ] **Step 6: Run the whole backend**

Run: `db_up; go_docker vet ./... && go_docker test ./...; db_down`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/dashboards
git commit -m "feat(dashboards): standard site widgets and a leak test over every widget type

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
## Part C — Frontend

The frontend has no unit tests; every task's check is the gate command (typecheck, lint, build) plus a look at the running page. To look at a page, run the dev stack the usual way for this repo, or deploy the branch's images to a sandbox — never to the work install before the whole-branch review.

### Task 20: Grid library, types, hooks and helpers

**Files:**
- Modify: `frontend/package.json`, `frontend/package-lock.json` (add `react-grid-layout@^2.2.4`)
- Modify: `frontend/src/services/api.ts` (`ApiError.details`)
- Modify: `frontend/src/utils/colors.ts` (`chartPalette`)
- Create: `frontend/src/types/dashboards.ts`
- Create: `frontend/src/hooks/useDashboards.ts`, `frontend/src/hooks/useWidgetData.ts`
- Create: `frontend/src/utils/dashboards.ts`

**Interfaces:**
- Produces (TypeScript):
  - types: `DashboardAccess`, `WidgetType`, `WidgetState`, `Dashboard`, `DashboardWidget`, `DashboardDetail`, `WidgetResponse<T>`, `PublicWidget`, `PublicDashboard`, `DashboardShare`, `PublicLinkInfo`, `DeviceMetric`, and the widget data types `LabelData`, `SeriesLine`, `TimeseriesData`, `StatData`, `PortGridData`, `DeviceHealthData`, `UPSCard`, `SitePowerData`, `TopNItem`, `TopNData`, `EventItem`, `EventLogData`, `DeviceRow`, `DeviceTableData`, `IncidentRow`, `OpenIncidentsData`, `UptimeBucket`, `MonitorRow`, `AgentRow`, `MonitorsData`
  - hooks: `useDashboards(siteId?)` → `{ dashboards, loading, error, refetch }`; `useDashboard(id)` → `{ dashboard, setDashboard, loading, notFound, error, refetch }`; `useDashboardActions()` → `{ create, save, remove, listShares, share, unshare, getPublicLink, createPublicLink, revokePublicLink, preview }`; `useDeviceMetrics(deviceId)` → `{ metrics, loading }`; `useWidgetData<T>(source: WidgetSource | null)` → `{ response, error, stale, lastSuccess }`
  - `type WidgetSource = { kind: 'dashboard'; dashboardId: string; widgetId: string; override: MetricsRange | '' } | { kind: 'public'; token: string; widgetId: string }`
  - utils: `GRID_COLS`, `ROW_HEIGHT`, `GRID_MARGIN`, `WIDGET_TYPES`, `widgetInfo(t)`, `stackOrder(items)`, `isStale(lastSuccess, refreshSeconds, now)`, `chartRows(lines)`, `unitOf(unit)`, `formatMetric(v, unit)`, `defaultConfig(type, siteId)`, `nextY(items)`, `inputCls`, `type DraftWidget`, `widgetSummary(w)`, `isTimeBased(type)`
  - `chartPalette: string[]` in `utils/colors.ts`
  - `ApiError.details?: Record<string, unknown>`

- [ ] **Step 1: Add the grid library**

From the worktree root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine \
  npm install --no-audit --no-fund react-grid-layout@^2.2.4
```

Expected: `package.json` lists `"react-grid-layout": "^2.2.4"` under dependencies and `package-lock.json` changes. Version 2 ships its own types; do **not** add `@types/react-grid-layout`.

- [ ] **Step 2: Let callers read extra error fields**

In `frontend/src/services/api.ts`, extend `ApiError`:

```ts
export interface ApiError {
  status: number
  message: string
  code?: string
  /** The whole error body, for endpoints that add fields to it (a dashboard
   *  save adds widget_index and field; a conflict adds current_version). */
  details?: Record<string, unknown>
}
```

and in the response interceptor replace `const apiError: ApiError = { status, message, code }` with:

```ts
    const details = error.response?.data as unknown as Record<string, unknown> | undefined
    const apiError: ApiError = { status, message, code, details }
```

- [ ] **Step 3: Add the chart palette**

Append to `frontend/src/utils/colors.ts`:

```ts
// Chart line colours, in the order dashboard lines take them. Hex, not
// Tailwind classes, because Recharts draws SVG strokes.
export const chartPalette = [
  '#22d3ee', '#a78bfa', '#34d399', '#60a5fa', '#f472b6',
  '#fbbf24', '#f87171', '#2dd4bf', '#c084fc', '#a3e635',
]
```

- [ ] **Step 4: Write the types**

`frontend/src/types/dashboards.ts`:

```ts
import type { MetricPoint, MetricsRange } from '@/hooks/useMetrics'
import type { PortView, UnitFaceplate } from '@/hooks/usePorts'
import type { HealthMetric } from '@/hooks/useDeviceHealth'
import type { DeviceStatus, DeviceType } from '@/hooks/useDevices'

export type DashboardAccess = 'none' | 'view' | 'edit' | 'manage'

export type WidgetType =
  | 'label' | 'timeseries' | 'stat' | 'port_grid' | 'device_health' | 'site_power'
  | 'top_n' | 'event_log' | 'device_table' | 'open_incidents' | 'monitors'

export type WidgetState = 'ok' | 'no_access' | 'removed' | 'no_data'

export interface Dashboard {
  id: string
  name: string
  description: string
  site_id: string | null
  owner_id: string | null
  created_by: string | null
  version: number
  created_at: string
  updated_at: string
  site_name: string
  access: DashboardAccess
  published: boolean
  /** May change the name, widgets and layout now (published needs an admin). */
  can_edit: boolean
  can_share: boolean
  widget_count: number
}

export interface DashboardWidget {
  id: string
  type: WidgetType
  title: string
  x: number
  y: number
  w: number
  h: number
  /** Only sent to callers who can edit the dashboard. */
  config?: Record<string, unknown>
}

export interface DashboardDetail extends Dashboard {
  widgets: DashboardWidget[]
}

export interface WidgetResponse<T = unknown> {
  state: WidgetState
  data?: T
  hidden: number
  removed: number
  refresh_seconds: number
  generated_at: string
}

export interface PublicWidget {
  id: string
  type: WidgetType
  title: string
  x: number
  y: number
  w: number
  h: number
}

export interface PublicDashboard {
  name: string
  description: string
  version: number
  widgets: PublicWidget[]
}

export interface DashboardShare {
  user_id: string
  username: string
  email: string
  permission: 'readonly' | 'editable'
  created_at: string
}

export interface PublicLink {
  dashboard_id: string
  token: string
  created_by: string
  created_at: string
}

export interface PublicLinkInfo {
  link: PublicLink | null
  broad_widgets: { id: string; type: WidgetType; title: string }[]
}

export interface DeviceMetric {
  metric: string
  label: string
  unit: string
  instances: { instance: string; label: string }[]
}

// ---- Widget data (mirrors backend/internal/dashboards/widget_*.go) ----

export interface LabelData {
  text: string
  size: 's' | 'm' | 'l'
}

export interface SeriesLine {
  key: string
  label: string
  metric: string
  unit: string
  points: MetricPoint[]
}

export interface TimeseriesData {
  range: MetricsRange
  resolution: string
  step_seconds: number
  lines: SeriesLine[]
}

export interface StatData {
  label: string
  unit: string
  value: number
  level: 'ok' | 'warn' | 'crit'
  mode: 'latest' | 'average'
  range: MetricsRange
  spark?: MetricPoint[]
}

export interface PortGridData {
  device_id?: string
  device_name: string
  model: string
  ports: PortView[]
  faceplates: UnitFaceplate[]
}

export interface DeviceHealthData {
  device_id?: string
  device_name: string
  metrics: HealthMetric[]
}

export interface UPSCard {
  device_id?: string
  name: string
  status: DeviceStatus
  readings: Record<string, number>
  conditions: string[]
  low_battery_pct: number
  high_load_pct: number
}

export interface SitePowerData {
  upses: UPSCard[]
}

export interface TopNItem {
  device_id?: string
  device_name: string
  if_index: number
  port_name: string
  alias: string
  value: number
}

export interface TopNData {
  measure: 'traffic' | 'utilisation' | 'errors'
  unit: string
  range: MetricsRange
  items: TopNItem[]
}

export interface EventItem {
  time: string
  kind: string
  device_id?: string
  device_name: string
  if_index?: number
  port_label: string
  port_name: string
  subject: string
  condition: string
  severity: string
  incident_id?: string
  ended_at?: string
}

export interface EventLogData {
  items: EventItem[]
}

export interface DeviceRow {
  device_id?: string
  name: string
  host?: string
  type: DeviceType
  status: DeviceStatus
  vendor_model: string
  last_seen_at: string | null
  availability_30d: number | null
  open_incidents: number
}

export interface DeviceTableData {
  devices: DeviceRow[]
}

export interface IncidentRow {
  incident_id?: string
  subject_type: 'monitor' | 'device'
  subject_name: string
  site_name: string
  condition: string
  severity: string
  start_time: string
  duration_seconds: number
  device_id?: string
  monitor_id?: string
  port_if_index?: number
}

export interface OpenIncidentsData {
  incidents: IncidentRow[]
  total: number
}

export interface UptimeBucket {
  label: string
  status: 'up' | 'down' | 'partial' | 'nodata'
  uptime: number
}

export interface MonitorRow {
  monitor_id?: string
  name: string
  type: string
  status: string
  response_time_ms: number
  last_check_at: string | null
  buckets?: UptimeBucket[]
}

export interface AgentRow {
  agent_id?: string
  name: string
  status: string
  last_heartbeat: string | null
}

export interface MonitorsData {
  style: 'list' | 'bars'
  window: '24h' | '90d'
  monitors: MonitorRow[]
  agents: AgentRow[]
}
```

- [ ] **Step 5: Write the dashboard hooks**

`frontend/src/hooks/useDashboards.ts`:

```ts
import { useCallback, useEffect, useMemo, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { MetricsRange } from '@/hooks/useMetrics'
import type {
  Dashboard, DashboardDetail, DashboardShare, DeviceMetric, PublicLink, PublicLinkInfo, WidgetResponse, WidgetType,
} from '@/types/dashboards'

export interface SaveWidget {
  id?: string
  type: WidgetType
  title: string
  config: Record<string, unknown>
  x: number
  y: number
  w: number
  h: number
}

export interface SaveDashboardInput {
  version: number
  name: string
  description: string
  widgets: SaveWidget[]
}

export interface CreateDashboardInput {
  name: string
  description?: string
  site_id?: string | null
  starter?: boolean
}

/** Dashboards the signed-in user can see, optionally one site's. A late
 *  response for an earlier siteId is dropped. */
export function useDashboards(siteId?: string) {
  const [dashboards, setDashboards] = useState<Dashboard[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)
  const refetch = useCallback(() => setVersion((v) => v + 1), [])

  useEffect(() => {
    let cancelled = false
    api
      .get<ApiResponse<Dashboard[]>>('/dashboards', { params: siteId ? { site_id: siteId } : {} })
      .then(({ data }) => {
        if (cancelled) return
        setDashboards(data.data ?? [])
        setError(null)
      })
      .catch((err: ApiError) => {
        if (!cancelled) setError(err.message || 'Failed to load dashboards')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [siteId, version])

  return { dashboards, loading, error, refetch }
}

/** One dashboard. notFound covers "missing" and "not yours": the API answers
 *  404 for both on purpose. */
export function useDashboard(id: string | undefined) {
  const [dashboard, setDashboard] = useState<DashboardDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)
  const refetch = useCallback(() => setVersion((v) => v + 1), [])

  useEffect(() => {
    if (!id) return
    let cancelled = false
    api
      .get<ApiResponse<DashboardDetail>>(`/dashboards/${id}`)
      .then(({ data }) => {
        if (cancelled) return
        setDashboard(data.data)
        setNotFound(false)
        setError(null)
      })
      .catch((err: ApiError) => {
        if (cancelled) return
        if (err.status === 404 || err.status === 400) setNotFound(true)
        else setError(err.message || 'Failed to load the dashboard')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [id, version])

  return { dashboard, setDashboard, loading, notFound, error, refetch }
}

/** Dashboard mutations and the admin/editor calls that are not lists. */
export function useDashboardActions() {
  return useMemo(
    () => ({
      create: async (input: CreateDashboardInput) =>
        (await api.post<ApiResponse<DashboardDetail>>('/dashboards', input)).data.data,
      save: async (id: string, input: SaveDashboardInput) =>
        (await api.put<ApiResponse<DashboardDetail>>(`/dashboards/${id}`, input)).data.data,
      remove: async (id: string) => {
        await api.delete(`/dashboards/${id}`)
      },
      listShares: async (id: string) =>
        (await api.get<ApiResponse<DashboardShare[]>>(`/dashboards/${id}/shares`)).data.data ?? [],
      share: async (id: string, userId: string, permission: 'readonly' | 'editable') => {
        await api.put(`/dashboards/${id}/shares/${userId}`, { permission })
      },
      unshare: async (id: string, userId: string) => {
        await api.delete(`/dashboards/${id}/shares/${userId}`)
      },
      getPublicLink: async (id: string) =>
        (await api.get<ApiResponse<PublicLinkInfo>>(`/dashboards/${id}/public-link`)).data.data,
      createPublicLink: async (id: string) =>
        (await api.post<ApiResponse<PublicLink>>(`/dashboards/${id}/public-link`)).data.data,
      revokePublicLink: async (id: string) => {
        await api.delete(`/dashboards/${id}/public-link`)
      },
      preview: async (id: string, body: { type: WidgetType; config: Record<string, unknown>; range?: MetricsRange | '' }) =>
        (await api.post<ApiResponse<WidgetResponse>>(`/dashboards/${id}/widgets/preview`, body)).data.data,
    }),
    [],
  )
}

/** The metrics and instances a device has, for the editor's pickers. */
export function useDeviceMetrics(deviceId: string | undefined) {
  const [metrics, setMetrics] = useState<DeviceMetric[]>([])
  const [loading, setLoading] = useState(false)
  useEffect(() => {
    if (!deviceId) {
      setMetrics([])
      return
    }
    let cancelled = false
    setLoading(true)
    api
      .get<ApiResponse<DeviceMetric[]>>(`/devices/${deviceId}/metrics`)
      .then(({ data }) => {
        if (!cancelled) setMetrics(data.data ?? [])
      })
      .catch(() => {
        if (!cancelled) setMetrics([])
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [deviceId])
  return { metrics, loading }
}
```

- [ ] **Step 6: Write the widget data hook**

`frontend/src/hooks/useWidgetData.ts`:

```ts
import { useEffect, useState } from 'react'
import axios from 'axios'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { MetricsRange } from '@/hooks/useMetrics'
import type { WidgetResponse } from '@/types/dashboards'
import { isStale } from '@/utils/dashboards'

export type WidgetSource =
  | { kind: 'dashboard'; dashboardId: string; widgetId: string; override: MetricsRange | '' }
  | { kind: 'public'; token: string; widgetId: string }

const MAX_BACKOFF_MS = 5 * 60 * 1000

async function fetchWidget<T>(src: WidgetSource, signal: AbortSignal): Promise<WidgetResponse<T>> {
  if (src.kind === 'public') {
    // A bare axios call: the public page has no session and must not trigger
    // the app's auth handling.
    const res = await axios.get<ApiResponse<WidgetResponse<T>>>(
      `/api/v1/public/dashboards/${src.token}/widgets/${src.widgetId}/data`,
      { signal },
    )
    return res.data.data
  }
  const res = await api.get<ApiResponse<WidgetResponse<T>>>(
    `/dashboards/${src.dashboardId}/widgets/${src.widgetId}/data`,
    { params: src.override ? { range: src.override } : {}, signal },
  )
  return res.data.data
}

/** One widget's data, refreshed when the server says to. A failed request is
 *  retried with backoff (5 s doubling, capped at 5 minutes) and the last good
 *  data stays on screen, marked stale once it is three refresh periods old. */
export function useWidgetData<T>(source: WidgetSource | null) {
  const [response, setResponse] = useState<WidgetResponse<T> | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [lastSuccess, setLastSuccess] = useState<number | null>(null)
  const [now, setNow] = useState(() => Date.now())
  // The effect keys on the source's value, not its identity, so a parent that
  // rebuilds the object each render does not refetch.
  const key = source ? JSON.stringify(source) : ''

  useEffect(() => {
    if (!key) return
    const src = JSON.parse(key) as WidgetSource
    let cancelled = false
    let timer: number | undefined
    let failures = 0
    const controller = new AbortController()
    const load = async () => {
      try {
        const resp = await fetchWidget<T>(src, controller.signal)
        if (cancelled) return
        failures = 0
        setResponse(resp)
        setError(null)
        setLastSuccess(Date.now())
        if (resp.refresh_seconds > 0) timer = window.setTimeout(() => void load(), resp.refresh_seconds * 1000)
      } catch (err) {
        if (cancelled) return
        failures++
        const msg = axios.isAxiosError(err)
          ? ((err.response?.data as { error?: string } | undefined)?.error ?? err.message)
          : (err as ApiError).message
        setError(msg || 'Could not load this widget')
        timer = window.setTimeout(() => void load(), Math.min(MAX_BACKOFF_MS, 5000 * 2 ** Math.min(failures - 1, 6)))
      }
    }
    void load()
    return () => {
      cancelled = true
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [key])

  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 15000)
    return () => window.clearInterval(t)
  }, [])

  return { response, error, lastSuccess, stale: isStale(lastSuccess, response?.refresh_seconds ?? 0, now) }
}
```

- [ ] **Step 7: Write the helpers**

`frontend/src/utils/dashboards.ts`:

```ts
import type { SeriesLine, WidgetType } from '@/types/dashboards'
import type { Unit } from '@/components/network/TrafficChart'
import { formatBps } from '@/utils/network'

export const GRID_COLS = 12
export const ROW_HEIGHT = 80
export const GRID_MARGIN: [number, number] = [12, 12]

export const inputCls =
  'w-full rounded-md border border-white/10 bg-slate-900/60 px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-primary-500'

export interface WidgetTypeInfo {
  type: WidgetType
  label: string
  description: string
  w: number
  h: number
  minW: number
  minH: number
  timeBased: boolean
}

/** The widget types, in the order the picker lists them. */
export const WIDGET_TYPES: WidgetTypeInfo[] = [
  { type: 'label', label: 'Label', description: 'A heading or a note, e.g. "Main Campus".', w: 4, h: 1, minW: 2, minH: 1, timeBased: false },
  { type: 'timeseries', label: 'Time series', description: "A chart of up to 10 metrics, or a site's internet traffic.", w: 6, h: 4, minW: 3, minH: 3, timeBased: true },
  { type: 'stat', label: 'Stat', description: 'One number, amber or red past its thresholds.', w: 3, h: 2, minW: 2, minH: 2, timeBased: true },
  { type: 'port_grid', label: 'Port grid', description: "A device's faceplate with live port status.", w: 8, h: 3, minW: 4, minH: 2, timeBased: false },
  { type: 'device_health', label: 'Device health', description: 'CPU, temperatures, fans and power supplies.', w: 6, h: 4, minW: 3, minH: 2, timeBased: false },
  { type: 'site_power', label: 'Site power', description: 'Every UPS in a site at a glance.', w: 4, h: 4, minW: 3, minH: 2, timeBased: false },
  { type: 'top_n', label: 'Top ports', description: 'The busiest ports, or those with the most errors.', w: 6, h: 4, minW: 3, minH: 3, timeBased: true },
  { type: 'event_log', label: 'Event log', description: 'Recent port events and incidents.', w: 6, h: 4, minW: 3, minH: 3, timeBased: false },
  { type: 'device_table', label: 'Device table', description: "A site's devices and how they are doing.", w: 12, h: 5, minW: 6, minH: 3, timeBased: false },
  { type: 'open_incidents', label: 'Open incidents', description: 'What is down or degraded right now.', w: 6, h: 4, minW: 3, minH: 2, timeBased: false },
  { type: 'monitors', label: 'Monitors', description: 'Websites, services and servers, up or down.', w: 6, h: 4, minW: 3, minH: 2, timeBased: false },
]

const UNKNOWN: WidgetTypeInfo = { type: 'label', label: 'Widget', description: '', w: 4, h: 2, minW: 1, minH: 1, timeBased: false }

export function widgetInfo(t: WidgetType): WidgetTypeInfo {
  return WIDGET_TYPES.find((w) => w.type === t) ?? UNKNOWN
}

export function isTimeBased(t: WidgetType): boolean {
  return widgetInfo(t).timeBased
}

/** A widget being edited. key is its id, or "new-N" until it is saved. */
export interface DraftWidget {
  key: string
  id?: string
  type: WidgetType
  title: string
  config: Record<string, unknown>
  x: number
  y: number
  w: number
  h: number
}

/** Top to bottom, left to right: the phone-width order. */
export function stackOrder<T extends { x: number; y: number }>(items: T[]): T[] {
  return [...items].sort((a, b) => a.y - b.y || a.x - b.x)
}

/** The first free row below every widget. */
export function nextY(items: { y: number; h: number }[]): number {
  return items.reduce((max, w) => Math.max(max, w.y + w.h), 0)
}

/** Older than three refresh periods: the screen must not look healthy just
 *  because it stopped updating. */
export function isStale(lastSuccess: number | null, refreshSeconds: number, now: number): boolean {
  if (lastSuccess == null || refreshSeconds <= 0) return false
  return now - lastSuccess > 3 * refreshSeconds * 1000
}

/** Lines merged into chart rows: { t: epoch ms, [line key]: value }. */
export function chartRows(lines: SeriesLine[]): Record<string, number>[] {
  const byTime = new Map<number, Record<string, number>>()
  for (const l of lines) {
    for (const p of l.points) {
      const t = new Date(p.t).getTime()
      const row = byTime.get(t) ?? { t }
      row[l.key] = p.avg
      byTime.set(t, row)
    }
  }
  return [...byTime.values()].sort((a, b) => a.t - b.t)
}

/** A metric unit as AreaSeriesChart names it. */
export function unitOf(unit: string): Unit {
  switch (unit) {
    case 'bps':
      return 'bps'
    case '%':
      return 'pct'
    case 'per_min':
      return 'per_min'
    case 'min':
      return 'min'
    default:
      return 'custom'
  }
}

/** A value in its unit, the way charts label it. */
export function formatMetric(v: number, unit: string): string {
  switch (unit) {
    case 'bps':
      return formatBps(v)
    case '%':
      return `${Math.round(v)}%`
    case 'per_min':
      return `${+v.toFixed(1)}/min`
    case 'min':
      return `${Math.round(v)} min`
    default:
      return `${+v.toFixed(1)} ${unit}`.trim()
  }
}

/** A new widget's starting config; siteId is the dashboard's site, if any. */
export function defaultConfig(type: WidgetType, siteId: string | null): Record<string, unknown> {
  switch (type) {
    case 'label':
      return { text: 'New label', size: 'm' }
    case 'timeseries':
      return siteId
        ? { source: 'site_traffic', site_id: siteId, view: 'internet', range: '24h' }
        : { source: 'metrics', metrics: [], devices: [], range: '24h' }
    case 'stat':
      return { mode: 'latest', direction: 'above', range: '24h' }
    case 'top_n':
      return siteId ? { site_id: siteId, measure: 'traffic', n: 10, range: '24h' } : { devices: [], measure: 'traffic', n: 10, range: '24h' }
    case 'event_log':
      return siteId ? { site_id: siteId, limit: 20 } : { limit: 20 }
    case 'open_incidents':
      return siteId ? { scope: 'site', site_id: siteId, limit: 20 } : { scope: 'all', limit: 20 }
    case 'monitors':
      return { monitors: [], agents: [], style: 'list', window: '24h' }
    case 'site_power':
    case 'device_table':
      return siteId ? { site_id: siteId } : {}
    default:
      return {}
  }
}

/** A one-line description of a draft widget's config, for the editor grid. */
export function widgetSummary(w: DraftWidget): string {
  const c = w.config
  const count = (k: string) => (Array.isArray(c[k]) ? (c[k] as unknown[]).length : 0)
  switch (w.type) {
    case 'label':
      return String(c.text ?? '')
    case 'timeseries':
      return c.source === 'site_traffic' ? `Site ${c.view === 'east_west' ? 'inside traffic' : 'internet traffic'}` : `${count('metrics')} metric(s) · ${c.site_id ? 'site total' : `${count('devices')} device(s)`}`
    case 'monitors':
      return `${count('monitors')} monitor(s), ${count('agents')} server(s) · ${c.style === 'bars' ? 'uptime bars' : 'list'}`
    case 'open_incidents':
      return `Scope: ${String(c.scope ?? 'all')}`
    default:
      return widgetInfo(w.type).description
  }
}
```

- [ ] **Step 8: Run the gate**

Run the frontend gate command from Global Constraints.
Expected: typecheck, lint and build pass (nothing uses the new files yet; they must still compile).

- [ ] **Step 9: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/src/services/api.ts frontend/src/utils/colors.ts frontend/src/types/dashboards.ts frontend/src/hooks/useDashboards.ts frontend/src/hooks/useWidgetData.ts frontend/src/utils/dashboards.ts
git commit -m "feat(dashboards): grid library, API types, data hooks and helpers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 21: Navigation, the dashboards list and creating a dashboard

**Files:**
- Modify: `frontend/src/components/Layout.tsx` (nav entry)
- Modify: `frontend/src/App.tsx` (lazy page and `/dashboards` route)
- Modify: `frontend/src/components/ShimmerStatCard.tsx` (`dashboards` colour)
- Modify: `frontend/src/pages/Overview.tsx` (Dashboards card, first, matching the sidebar)
- Create: `frontend/src/pages/dashboards/Dashboards.tsx`
- Create: `frontend/src/components/dashboards/NewDashboardModal.tsx`

**Interfaces:**
- Consumes: `useDashboards`, `useDashboardActions().create`, `useSites`, `useAuthContext`.
- Produces: route `/dashboards`; `NewDashboardModal({ siteId?: string | null; onClose; onCreated(d: DashboardDetail) })`.

- [ ] **Step 1: Add the nav entry**

In `frontend/src/components/Layout.tsx`, change the `nav` array's head to:

```ts
const nav = [
  { to: '/', label: 'Overview', end: true },
  // Dashboards cover everything below, so they sit with the Overview.
  { to: '/dashboards', label: 'Dashboards' },
  // What is being monitored.
```

- [ ] **Step 2: Add the Overview card**

In `frontend/src/components/ShimmerStatCard.tsx`, add `'dashboards'` to the `colorType` union and to `colorMap`:

```ts
  dashboards: {
    hoverBorder: 'hover:border-teal-500/50',
    bg: 'from-teal-600/15',
    text: 'text-teal-400',
    subtle: 'text-teal-400/70',
    border: 'border-teal-500/30',
    glow: 'bg-teal-500/10',
    glowHover: 'group-hover:bg-teal-500/20',
  },
```

In `frontend/src/pages/Overview.tsx`: import `useDashboards` from `@/hooks/useDashboards`; call `const { dashboards } = useDashboards()` with the other hooks; add `'dashboards'` to the `useCardShimmer([...])` list right after `'operational'`; make this the first entry of `sectionCards`:

```ts
      {
        key: 'dashboards',
        title: 'Dashboards',
        to: '/dashboards',
        colorType: 'dashboards' as const,
        value: String(dashboards.length),
        subtitle:
          dashboards.length === 0
            ? 'build one'
            : `${dashboards.filter((d) => d.published).length} with a public link`,
      },
```

and add `dashboards` to that `useMemo`'s dependency list.

- [ ] **Step 3: Write the new-dashboard modal**

`frontend/src/components/dashboards/NewDashboardModal.tsx`:

```tsx
import { useState } from 'react'
import { X } from 'lucide-react'
import { useSites } from '@/hooks/useSites'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { DashboardDetail } from '@/types/dashboards'
import { inputCls } from '@/utils/dashboards'

interface Props {
  /** Preselects a site dashboard for this site (from the site's page). */
  siteId?: string | null
  onClose: () => void
  onCreated: (d: DashboardDetail) => void
}

export default function NewDashboardModal({ siteId = null, onClose, onCreated }: Props) {
  const { sites } = useSites()
  const actions = useDashboardActions()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [kind, setKind] = useState<'personal' | 'site'>(siteId ? 'site' : 'personal')
  const [site, setSite] = useState(siteId ?? '')
  const [starter, setStarter] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const forSite = kind === 'site' && site !== ''
      const d = await actions.create({ name, description, site_id: forSite ? site : null, starter: forSite && starter })
      onCreated(d)
    } catch (err) {
      setError((err as ApiError).message || 'Could not create the dashboard')
      setBusy(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <form className="card w-full max-w-md space-y-4 p-6" onClick={(e) => e.stopPropagation()} onSubmit={(e) => void submit(e)}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">New dashboard</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Name</span>
          <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} maxLength={100} required autoFocus />
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-slate-300">Description</span>
          <textarea className={inputCls} rows={2} value={description} onChange={(e) => setDescription(e.target.value)} maxLength={500} />
        </label>
        <fieldset className="space-y-2">
          <legend className="text-sm text-slate-300">Belongs to</legend>
          <label className="flex items-center gap-2 text-sm">
            <input type="radio" checked={kind === 'personal'} onChange={() => setKind('personal')} /> Me — share it with people later
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input type="radio" checked={kind === 'site'} onChange={() => setKind('site')} /> A site — everyone with access to the site sees it
          </label>
          {kind === 'site' && (
            <>
              <select className={inputCls} value={site} onChange={(e) => setSite(e.target.value)} required>
                <option value="">Choose a site…</option>
                {sites.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
              <label className="flex items-center gap-2 text-sm text-slate-300">
                <input type="checkbox" checked={starter} onChange={(e) => setStarter(e.target.checked)} />
                Start with the standard widgets (traffic, power, incidents, busiest ports, devices)
              </label>
            </>
          )}
        </fieldset>
        {error && <p className="text-sm text-red-400">{error}</p>}
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn-primary" disabled={busy || name.trim() === ''}>
            {busy ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </div>
  )
}
```

- [ ] **Step 4: Write the list page**

`frontend/src/pages/dashboards/Dashboards.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Globe2, LayoutDashboard, Loader2, Plus } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { useDashboards } from '@/hooks/useDashboards'
import NewDashboardModal from '@/components/dashboards/NewDashboardModal'
import type { Dashboard } from '@/types/dashboards'

function DashboardList({ title, items, empty }: { title: string; items: Dashboard[]; empty: string }) {
  return (
    <section className="space-y-2">
      <h2 className="text-sm font-semibold uppercase tracking-widest text-slate-500">{title}</h2>
      {items.length === 0 ? (
        <p className="text-sm text-slate-500">{empty}</p>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {items.map((d) => (
            <li key={d.id}>
              <Link to={`/dashboards/${d.id}`} className="card block p-4 transition hover:bg-white/5">
                <p className="flex items-center gap-2 font-medium text-slate-100">
                  <LayoutDashboard className="h-4 w-4 text-teal-400" /> {d.name}
                  {d.published && (
                    <span className="ml-auto inline-flex items-center gap-1 rounded-full border border-teal-500/30 bg-teal-500/10 px-2 py-0.5 text-xs text-teal-300">
                      <Globe2 className="h-3 w-3" /> Public
                    </span>
                  )}
                </p>
                <p className="mt-1 text-sm text-slate-400">
                  {d.site_id ? d.site_name : 'Personal'} · {d.widget_count} widget{d.widget_count === 1 ? '' : 's'}
                </p>
                {d.description && <p className="mt-1 line-clamp-2 text-sm text-slate-500">{d.description}</p>}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

export default function Dashboards() {
  const navigate = useNavigate()
  const { currentUser } = useAuthContext()
  const { dashboards, loading, error } = useDashboards()
  const [creating, setCreating] = useState(false)

  const groups = useMemo(() => {
    const me = currentUser?.user_id
    return {
      mine: dashboards.filter((d) => d.owner_id === me),
      shared: dashboards.filter((d) => d.owner_id && d.owner_id !== me),
      site: dashboards.filter((d) => d.site_id),
    }
  }, [dashboards, currentUser])

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-4xl font-light text-white">Dashboards</h1>
          <p className="mt-2 text-slate-400">Your own views, site views for everyone at a site, and screens for the wall.</p>
        </div>
        <button className="btn-primary flex items-center gap-2" onClick={() => setCreating(true)}>
          <Plus className="h-4 w-4" /> New dashboard
        </button>
      </div>
      {error && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error}</div>}
      {loading ? (
        <Loader2 className="h-6 w-6 animate-spin text-slate-500" />
      ) : (
        <>
          <DashboardList title="Mine" items={groups.mine} empty="You have no dashboards of your own yet." />
          <DashboardList
            title={currentUser?.is_admin ? "Other people's" : 'Shared with me'}
            items={groups.shared}
            empty="Nothing here yet."
          />
          <DashboardList title="Site dashboards" items={groups.site} empty="No site has a dashboard yet." />
        </>
      )}
      {creating && (
        <NewDashboardModal onClose={() => setCreating(false)} onCreated={(d) => navigate(`/dashboards/${d.id}/edit`)} />
      )}
    </div>
  )
}
```

- [ ] **Step 5: Add the route**

In `frontend/src/App.tsx`, add with the other lazy pages:

```ts
const Dashboards = lazy(() => import('@/pages/dashboards/Dashboards'))
```

and inside the authenticated layout routes, after `<Route path="/" element={<Overview />} />`:

```tsx
              <Route path="/dashboards" element={<Dashboards />} />
```

- [ ] **Step 6: Run the gate and look at it**

Run the frontend gate. Then open `/dashboards`: the sidebar shows Dashboards under Overview; the Overview shows a Dashboards card first; "New dashboard" creates a personal dashboard (it navigates to `/dashboards/:id/edit`, which 404s in the router until Task 22 — expected).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/Layout.tsx frontend/src/App.tsx frontend/src/components/ShimmerStatCard.tsx frontend/src/pages/Overview.tsx frontend/src/pages/dashboards/Dashboards.tsx frontend/src/components/dashboards/NewDashboardModal.tsx
git commit -m "feat(dashboards): sidebar entry, Overview card, list page and new-dashboard dialog

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 22: Viewing a dashboard

**Files:**
- Create: `frontend/src/components/dashboards/DashboardGrid.tsx`
- Create: `frontend/src/components/dashboards/WidgetFrame.tsx`
- Create: `frontend/src/components/dashboards/WidgetRenderer.tsx`
- Create: `frontend/src/components/dashboards/WidgetBody.tsx`
- Create: `frontend/src/components/dashboards/RangeOverride.tsx`
- Create: `frontend/src/components/dashboards/widgets/LabelWidget.tsx`
- Create: `frontend/src/pages/dashboards/DashboardPage.tsx`
- Modify: `frontend/src/App.tsx` (routes `/dashboards/:id`, `/dashboards/:id/edit`)

**Interfaces:**
- Consumes: `useDashboard`, `useWidgetData`, `widgetInfo`, `stackOrder`, `GRID_*`, `ROW_HEIGHT`, `react-grid-layout`'s `GridLayout`, `useContainerWidth`, `Layout`.
- Produces:
  - `DashboardGrid<T extends GridItem>({ items, editing, render, onLayoutChange?, rowHeight? })`, where `GridItem = { id: string; type: WidgetType; x: number; y: number; w: number; h: number }`
  - `WidgetFrame({ title, typeLabel, state?, hidden?, stale?, loading?, error?, editing?, selected?, display?, onEdit?, onRemove?, children? })`
  - `WidgetRenderer({ type, data, linkable })` — the per-type switch Tasks 24–27 extend
  - `WidgetBody({ widget: { id, type, title }, source: WidgetSource, linkable, display? })`
  - `RangeOverride({ value, onChange })`
  - `DashboardPage({ mode: 'view' | 'edit' })` (edit mode is Task 23)

- [ ] **Step 1: Write the grid**

`frontend/src/components/dashboards/DashboardGrid.tsx`:

```tsx
import { useMemo, type ReactNode } from 'react'
import { GridLayout, useContainerWidth, type Layout } from 'react-grid-layout'
import 'react-grid-layout/css/styles.css'
import type { WidgetType } from '@/types/dashboards'
import { GRID_COLS, GRID_MARGIN, ROW_HEIGHT, stackOrder, widgetInfo } from '@/utils/dashboards'

interface GridItem {
  id: string
  type: WidgetType
  x: number
  y: number
  w: number
  h: number
}

interface Props<T extends GridItem> {
  items: T[]
  editing: boolean
  render: (item: T) => ReactNode
  onLayoutChange?: (layout: Layout) => void
  rowHeight?: number
}

/** The 12-column widget grid. Dragging and resizing only while editing; at
 *  phone width the widgets stack in their saved order instead. */
export default function DashboardGrid<T extends GridItem>({ items, editing, render, onLayoutChange, rowHeight = ROW_HEIGHT }: Props<T>) {
  const { width, mounted, containerRef } = useContainerWidth()
  const layout = useMemo<Layout>(
    () =>
      items.map((it) => {
        const info = widgetInfo(it.type)
        return { i: it.id, x: it.x, y: it.y, w: it.w, h: it.h, minW: info.minW, minH: info.minH }
      }),
    [items],
  )

  if (mounted && width < 640) {
    return (
      <div ref={containerRef} className="space-y-3">
        {stackOrder(items).map((it) => (
          <div key={it.id} style={{ height: it.h * rowHeight }}>
            {render(it)}
          </div>
        ))}
      </div>
    )
  }
  return (
    <div ref={containerRef}>
      {mounted && (
        <GridLayout
          width={width}
          layout={layout}
          gridConfig={{ cols: GRID_COLS, rowHeight, margin: GRID_MARGIN }}
          dragConfig={{ enabled: editing, handle: '.widget-drag-handle', cancel: '.widget-no-drag' }}
          resizeConfig={{ enabled: editing }}
          onLayoutChange={onLayoutChange}
        >
          {items.map((it) => (
            <div key={it.id}>{render(it)}</div>
          ))}
        </GridLayout>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Write the frame**

`frontend/src/components/dashboards/WidgetFrame.tsx`:

```tsx
import type { ReactNode } from 'react'
import { GripVertical, Loader2, Settings2, Trash2 } from 'lucide-react'
import type { WidgetState } from '@/types/dashboards'

interface Props {
  title: string
  typeLabel: string
  state?: WidgetState
  hidden?: number
  stale?: boolean
  loading?: boolean
  error?: string | null
  editing?: boolean
  selected?: boolean
  /** Fullscreen and public: larger type for reading across a room. */
  display?: boolean
  onEdit?: () => void
  onRemove?: () => void
  children?: ReactNode
}

const STATE_TEXT: Record<Exclude<WidgetState, 'ok'>, string> = {
  no_access: "You don't have access to what this widget shows.",
  removed: 'What this widget showed has been removed.',
  no_data: 'No data yet.',
}

/** Every widget's frame: its title, the stale marker, and the states every
 *  widget shares, so renderers only ever draw real data. */
export default function WidgetFrame({ title, typeLabel, state, hidden = 0, stale, loading, error, editing, selected, display, onEdit, onRemove, children }: Props) {
  let body: ReactNode = children
  if (loading) body = <Loader2 className="h-5 w-5 animate-spin text-slate-500" />
  else if (error) body = <p className="text-sm text-red-400">{error}</p>
  else if (state && state !== 'ok') body = <p className="text-sm text-slate-500">{STATE_TEXT[state]}</p>

  return (
    <div className={`card flex h-full flex-col overflow-hidden ${selected ? 'ring-2 ring-primary-500' : ''}`}>
      <div className={`flex items-center gap-2 border-b border-white/10 px-3 py-2 ${editing ? 'widget-drag-handle cursor-move' : ''}`}>
        {editing && <GripVertical className="h-4 w-4 shrink-0 text-slate-500" aria-hidden />}
        <h3 className={`min-w-0 flex-1 truncate font-medium text-slate-200 ${display ? 'text-lg' : 'text-sm'}`}>{title || typeLabel}</h3>
        {stale && (
          <span className="rounded-full border border-amber-500/30 bg-amber-500/15 px-2 py-0.5 text-xs text-amber-300" title="This data has not been refreshed for a while">
            Stale
          </span>
        )}
        {hidden > 0 && <span className="text-xs text-slate-500">{hidden} hidden</span>}
        {editing && (
          <>
            <button type="button" className="widget-no-drag text-slate-400 hover:text-white" onClick={onEdit} aria-label="Widget settings">
              <Settings2 className="h-4 w-4" />
            </button>
            <button type="button" className="widget-no-drag text-slate-400 hover:text-red-400" onClick={onRemove} aria-label="Remove widget">
              <Trash2 className="h-4 w-4" />
            </button>
          </>
        )}
      </div>
      <div className={`min-h-0 flex-1 overflow-auto p-3 ${display ? 'text-base' : 'text-sm'}`}>{body}</div>
    </div>
  )
}
```

- [ ] **Step 3: Write the label renderer and the renderer switch**

`frontend/src/components/dashboards/widgets/LabelWidget.tsx`:

```tsx
import type { LabelData } from '@/types/dashboards'

const SIZE = { s: 'text-lg', m: 'text-2xl', l: 'text-4xl' } as const

export default function LabelWidget({ data }: { data: LabelData }) {
  return (
    <div className="flex h-full items-center">
      <p className={`whitespace-pre-line font-light text-white ${SIZE[data.size] ?? SIZE.m}`}>{data.text}</p>
    </div>
  )
}
```

`frontend/src/components/dashboards/WidgetRenderer.tsx`:

```tsx
import type { LabelData, WidgetType } from '@/types/dashboards'
import LabelWidget from '@/components/dashboards/widgets/LabelWidget'

interface Props {
  type: WidgetType
  data: unknown
  /** Logged-in views link into Sentinel; public views never do. */
  linkable: boolean
}

/** Draws a widget's data by its type. Each type's renderer only ever sees
 *  data in the ok state; WidgetFrame handles the rest. */
export default function WidgetRenderer({ type, data }: Props) {
  switch (type) {
    case 'label':
      return <LabelWidget data={data as LabelData} />
    default:
      return <p className="text-sm text-slate-500">This version of Sentinel cannot show this widget.</p>
  }
}
```

(Tasks 24–27 add a `case` per type and start using `linkable`.)

- [ ] **Step 4: Write the fetching body and the range override**

`frontend/src/components/dashboards/WidgetBody.tsx`:

```tsx
import { useWidgetData, type WidgetSource } from '@/hooks/useWidgetData'
import type { WidgetType } from '@/types/dashboards'
import { widgetInfo } from '@/utils/dashboards'
import WidgetFrame from '@/components/dashboards/WidgetFrame'
import WidgetRenderer from '@/components/dashboards/WidgetRenderer'

interface Props {
  widget: { id: string; type: WidgetType; title: string }
  source: WidgetSource
  linkable: boolean
  display?: boolean
}

/** One widget on a dashboard: loads its data and draws it in its frame. */
export default function WidgetBody({ widget, source, linkable, display }: Props) {
  const { response, error, stale } = useWidgetData<unknown>(source)
  return (
    <WidgetFrame
      title={widget.title}
      typeLabel={widget.type ? widgetInfo(widget.type).label : 'Widget'}
      state={response?.state}
      hidden={response?.hidden}
      stale={stale}
      loading={!response && !error}
      error={response ? null : error}
      display={display}
    >
      {response?.state === 'ok' && <WidgetRenderer type={widget.type} data={response.data} linkable={linkable} />}
    </WidgetFrame>
  )
}
```

`frontend/src/components/dashboards/RangeOverride.tsx`:

```tsx
import { METRICS_RANGES, type MetricsRange } from '@/hooks/useMetrics'

/** The dashboard-wide range picker: overrides every time-based widget's
 *  saved range until the page is left. Never saved. */
export default function RangeOverride({ value, onChange }: { value: MetricsRange | ''; onChange: (v: MetricsRange | '') => void }) {
  return (
    <select
      className="rounded-md border border-white/10 bg-slate-900/60 px-2 py-1.5 text-sm text-slate-200"
      value={value}
      onChange={(e) => onChange(e.target.value as MetricsRange | '')}
      aria-label="Time range"
    >
      <option value="">Saved ranges</option>
      {METRICS_RANGES.map((r) => (
        <option key={r.value} value={r.value}>
          {r.label}
        </option>
      ))}
    </select>
  )
}
```

- [ ] **Step 5: Write the page (view mode)**

`frontend/src/pages/dashboards/DashboardPage.tsx`:

```tsx
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ArrowLeft, Loader2, Pencil } from 'lucide-react'
import { useDashboard } from '@/hooks/useDashboards'
import type { MetricsRange } from '@/hooks/useMetrics'
import DashboardGrid from '@/components/dashboards/DashboardGrid'
import WidgetBody from '@/components/dashboards/WidgetBody'
import RangeOverride from '@/components/dashboards/RangeOverride'
import { isTimeBased } from '@/utils/dashboards'

export default function DashboardPage({ mode }: { mode: 'view' | 'edit' }) {
  const { id } = useParams()
  const { dashboard, loading, notFound, error } = useDashboard(id)
  const [override, setOverride] = useState<MetricsRange | ''>('')

  if (loading) return <Loader2 className="h-6 w-6 animate-spin text-slate-500" />
  if (notFound || !dashboard) {
    return (
      <div className="space-y-3">
        <p className="text-slate-400">{error ?? 'Dashboard not found.'}</p>
        <Link to="/dashboards" className="text-primary-400 hover:underline">
          Back to dashboards
        </Link>
      </div>
    )
  }
  if (mode === 'edit') {
    // Task 23 replaces this branch with the editor.
    return <p className="text-slate-400">The editor is not built yet.</p>
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Link to="/dashboards" className="inline-flex items-center gap-1 text-sm text-slate-400 hover:text-slate-200">
            <ArrowLeft className="h-4 w-4" /> Dashboards
          </Link>
          <h1 className="mt-1 break-words text-3xl font-light text-white">{dashboard.name}</h1>
          <p className="text-sm text-slate-400">
            {dashboard.site_id ? (
              <Link to={`/network/sites/${dashboard.site_id}`} className="hover:text-slate-200">
                {dashboard.site_name}
              </Link>
            ) : (
              'Personal'
            )}
            {dashboard.description && ` · ${dashboard.description}`}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {dashboard.widgets.some((w) => isTimeBased(w.type)) && <RangeOverride value={override} onChange={setOverride} />}
          {dashboard.can_edit && (
            <Link to={`/dashboards/${dashboard.id}/edit`} className="btn-secondary flex items-center gap-2">
              <Pencil className="h-4 w-4" /> Edit
            </Link>
          )}
        </div>
      </div>
      {dashboard.published && !dashboard.can_edit && dashboard.access === 'edit' && (
        <p className="rounded-lg border border-teal-500/30 bg-teal-500/10 p-3 text-sm text-teal-200">
          This dashboard has a public link, so only an admin can change it.
        </p>
      )}
      {dashboard.widgets.length === 0 ? (
        <p className="text-slate-500">No widgets yet.{dashboard.can_edit && ' Choose Edit to add some.'}</p>
      ) : (
        <DashboardGrid
          items={dashboard.widgets}
          editing={false}
          render={(w) => (
            <WidgetBody widget={w} linkable source={{ kind: 'dashboard', dashboardId: dashboard.id, widgetId: w.id, override }} />
          )}
        />
      )}
    </div>
  )
}
```

In `frontend/src/App.tsx`, add the lazy page and two routes after `/dashboards`:

```ts
const DashboardPage = lazy(() => import('@/pages/dashboards/DashboardPage'))
```

```tsx
              <Route path="/dashboards/:id" element={<DashboardPage mode="view" />} />
              <Route path="/dashboards/:id/edit" element={<DashboardPage mode="edit" />} />
```

- [ ] **Step 6: Run the gate and look at it**

Run the frontend gate. Open a site dashboard created with the standard widgets (create it through the API or the dialog): the page shows the grid; the standard widgets show "This version of Sentinel cannot show this widget." until Tasks 24–27; a personal dashboard with a label widget saved through the API shows the label. Narrow the window below 640 px: widgets stack.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/dashboards frontend/src/pages/dashboards/DashboardPage.tsx frontend/src/App.tsx
git commit -m "feat(dashboards): view a dashboard: grid, widget frame and range override

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 23: Editing a dashboard

**Files:**
- Create: `frontend/src/components/dashboards/DashboardEditor.tsx`
- Create: `frontend/src/components/dashboards/WidgetPicker.tsx`
- Create: `frontend/src/components/dashboards/WidgetSettingsPanel.tsx`
- Create: `frontend/src/components/dashboards/WidgetSettings.tsx` (the per-type form switch Tasks 24–27 extend)
- Create: `frontend/src/components/dashboards/WidgetPreview.tsx`
- Create: `frontend/src/components/dashboards/pickers/SitePicker.tsx`, `DevicePicker.tsx`, `MetricPicker.tsx`, `InstancePicker.tsx`, `MonitorPicker.tsx`, `AgentPicker.tsx`, `RangeSelect.tsx`
- Create: `frontend/src/components/dashboards/settings/LabelSettings.tsx`
- Modify: `frontend/src/pages/dashboards/DashboardPage.tsx` (edit branch)

**Interfaces:**
- Consumes: `DraftWidget`, `defaultConfig`, `nextY`, `widgetInfo`, `widgetSummary`, `useDashboardActions().save|preview`, `WidgetRenderer`, `WidgetFrame`, `DashboardGrid`, `useSites`, `useDevices`, `useDeviceMetrics`, `useMonitors`, `useAgents`, `METRICS_RANGES`.
- Produces:
  - `DashboardEditor({ dashboard: DashboardDetail; onSaved(d: DashboardDetail); onCancel() })`
  - `WidgetSettings({ type, config, siteId, onChange(config) })`
  - settings form contract, used by every `settings/*Settings.tsx`: `({ config: Record<string, unknown>; siteId: string | null; onChange: (config: Record<string, unknown>) => void })`
  - pickers: `SitePicker({ value: string | null; onChange(id: string | null); allowNone?: boolean })`, `DevicePicker({ siteId?: string | null; value: string[]; onChange(ids: string[]); single?: boolean })`, `MetricPicker({ deviceId?: string; value: string[]; onChange(keys: string[]); single?: boolean; portOnly?: boolean })`, `InstancePicker({ deviceId?: string; metric?: string; value: string[]; onChange(v: string[]); single?: boolean })`, `MonitorPicker({ value: string[]; onChange(ids: string[]) })`, `AgentPicker({ value: string[]; onChange(ids: string[]) })`, `RangeSelect({ value: string; onChange(v: string) })`
  - helpers used by the forms: `str(c, k)`, `strs(c, k)`, `num(c, k, def)` from `utils/dashboards.ts` (add them in Step 1)

Browser limitation: the app uses `<BrowserRouter>`, so React Router cannot block in-app navigation (`useBlocker` needs a data router). The editor confirms in-page on **Cancel**, warns through `beforeunload` on reload or tab close, and accepts that a sidebar click while editing leaves without asking. Record this in the follow-ups doc (Task 31); moving the app to a data router is out of scope here.

- [ ] **Step 1: Add the config readers to `utils/dashboards.ts`**

```ts
/** Reads a string field of a widget config. */
export function str(c: Record<string, unknown>, k: string): string {
  return typeof c[k] === 'string' ? (c[k] as string) : ''
}

/** Reads a string-list field of a widget config. */
export function strs(c: Record<string, unknown>, k: string): string[] {
  return Array.isArray(c[k]) ? (c[k] as unknown[]).filter((v): v is string => typeof v === 'string') : []
}

/** Reads a number field of a widget config, or def. */
export function num(c: Record<string, unknown>, k: string, def: number): number {
  return typeof c[k] === 'number' ? (c[k] as number) : def
}
```

- [ ] **Step 2: Write the pickers**

`frontend/src/components/dashboards/pickers/SitePicker.tsx`:

```tsx
import { useSites } from '@/hooks/useSites'
import { inputCls } from '@/utils/dashboards'

export default function SitePicker({ value, onChange, allowNone = false }: { value: string | null; onChange: (id: string | null) => void; allowNone?: boolean }) {
  const { sites } = useSites()
  return (
    <select className={inputCls} value={value ?? ''} onChange={(e) => onChange(e.target.value || null)}>
      <option value="">{allowNone ? 'Any site' : 'Choose a site…'}</option>
      {sites.map((s) => (
        <option key={s.id} value={s.id}>
          {s.name}
        </option>
      ))}
    </select>
  )
}
```

`frontend/src/components/dashboards/pickers/DevicePicker.tsx`:

```tsx
import { useDevices } from '@/hooks/useDevices'
import { inputCls } from '@/utils/dashboards'

interface Props {
  siteId?: string | null
  value: string[]
  onChange: (ids: string[]) => void
  single?: boolean
}

/** Devices the user can see, optionally one site's; a select for one, a
 *  checklist for several (at most 50, the widget limit). */
export default function DevicePicker({ siteId, value, onChange, single }: Props) {
  const { devices, loading } = useDevices(siteId ? { siteId } : {})
  if (single) {
    return (
      <select className={inputCls} value={value[0] ?? ''} onChange={(e) => onChange(e.target.value ? [e.target.value] : [])}>
        <option value="">{loading ? 'Loading…' : 'Choose a device…'}</option>
        {devices.map((d) => (
          <option key={d.id} value={d.id}>
            {d.name} {siteId ? '' : `(${d.site_name})`}
          </option>
        ))}
      </select>
    )
  }
  const toggle = (id: string) =>
    onChange(value.includes(id) ? value.filter((v) => v !== id) : value.length < 50 ? [...value, id] : value)
  return (
    <div className="max-h-48 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      {devices.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : 'No devices.'}</p>}
      {devices.map((d) => (
        <label key={d.id} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(d.id)} onChange={() => toggle(d.id)} />
          {d.name} <span className="text-slate-500">{d.site_name}</span>
        </label>
      ))}
    </div>
  )
}
```

`frontend/src/components/dashboards/pickers/MetricPicker.tsx`:

```tsx
import { useDeviceMetrics } from '@/hooks/useDashboards'
import { inputCls } from '@/utils/dashboards'

interface Props {
  /** Metrics are listed from this device (the first one chosen). */
  deviceId?: string
  value: string[]
  onChange: (keys: string[]) => void
  single?: boolean
  /** Only port metrics (if_*), for site totals. */
  portOnly?: boolean
}

const PORT_METRICS = [
  { metric: 'if_in_bps', label: 'Traffic in' },
  { metric: 'if_out_bps', label: 'Traffic out' },
  { metric: 'if_in_util_pct', label: 'Busy in' },
  { metric: 'if_out_util_pct', label: 'Busy out' },
  { metric: 'if_in_errors_pm', label: 'Errors in' },
  { metric: 'if_out_errors_pm', label: 'Errors out' },
]

export default function MetricPicker({ deviceId, value, onChange, single, portOnly }: Props) {
  const { metrics, loading } = useDeviceMetrics(portOnly ? undefined : deviceId)
  const options = portOnly ? PORT_METRICS : metrics.map((m) => ({ metric: m.metric, label: `${m.label}${m.unit ? ` (${m.unit})` : ''}` }))
  if (!portOnly && !deviceId) return <p className="text-xs text-slate-500">Choose a device first.</p>
  if (single) {
    return (
      <select className={inputCls} value={value[0] ?? ''} onChange={(e) => onChange(e.target.value ? [e.target.value] : [])}>
        <option value="">{loading ? 'Loading…' : 'Choose a metric…'}</option>
        {options.map((o) => (
          <option key={o.metric} value={o.metric}>
            {o.label}
          </option>
        ))}
      </select>
    )
  }
  const toggle = (k: string) =>
    onChange(value.includes(k) ? value.filter((v) => v !== k) : value.length < 10 ? [...value, k] : value)
  return (
    <div className="max-h-48 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      {options.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : 'This device has no metrics yet.'}</p>}
      {options.map((o) => (
        <label key={o.metric} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(o.metric)} onChange={() => toggle(o.metric)} /> {o.label}
        </label>
      ))}
    </div>
  )
}
```

`frontend/src/components/dashboards/pickers/InstancePicker.tsx`:

```tsx
import { useDeviceMetrics } from '@/hooks/useDashboards'
import { inputCls } from '@/utils/dashboards'

interface Props {
  deviceId?: string
  metric?: string
  value: string[]
  onChange: (v: string[]) => void
  single?: boolean
}

/** The instances (ports, sensor rows) of one metric on one device. Choosing
 *  none means the device's total. */
export default function InstancePicker({ deviceId, metric, value, onChange, single }: Props) {
  const { metrics } = useDeviceMetrics(deviceId)
  const instances = metrics.find((m) => m.metric === metric)?.instances ?? []
  if (!deviceId || !metric || instances.length <= 1) return null
  if (single) {
    return (
      <select className={inputCls} value={value[0] ?? ''} onChange={(e) => onChange(e.target.value ? [e.target.value] : [])}>
        <option value="">Device total</option>
        {instances.map((i) => (
          <option key={i.instance} value={i.instance}>
            {i.label}
          </option>
        ))}
      </select>
    )
  }
  const toggle = (k: string) => onChange(value.includes(k) ? value.filter((v) => v !== k) : [...value, k])
  return (
    <div className="max-h-40 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      <p className="text-xs text-slate-500">None chosen: the device total.</p>
      {instances.map((i) => (
        <label key={i.instance} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(i.instance)} onChange={() => toggle(i.instance)} /> {i.label}
        </label>
      ))}
    </div>
  )
}
```

`frontend/src/components/dashboards/pickers/MonitorPicker.tsx`:

```tsx
import { useMonitors } from '@/hooks/useMonitors'

export default function MonitorPicker({ value, onChange }: { value: string[]; onChange: (ids: string[]) => void }) {
  const { monitors, loading } = useMonitors({ page: 1, limit: 500 })
  const toggle = (id: string) =>
    onChange(value.includes(id) ? value.filter((v) => v !== id) : value.length < 50 ? [...value, id] : value)
  return (
    <div className="max-h-48 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      {monitors.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : 'No monitors.'}</p>}
      {monitors.map((m) => (
        <label key={m.id} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(m.id)} onChange={() => toggle(m.id)} /> {m.name}
        </label>
      ))}
    </div>
  )
}
```

`frontend/src/components/dashboards/pickers/AgentPicker.tsx`:

```tsx
import { useAgents } from '@/hooks/useAgents'

/** Server agents. Admin-only, like the agents themselves; members never see
 *  this picker. */
export default function AgentPicker({ value, onChange }: { value: string[]; onChange: (ids: string[]) => void }) {
  const { agents, loading } = useAgents(0)
  const toggle = (id: string) =>
    onChange(value.includes(id) ? value.filter((v) => v !== id) : value.length < 50 ? [...value, id] : value)
  return (
    <div className="max-h-40 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      {agents.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : 'No servers.'}</p>}
      {agents.map((a) => (
        <label key={a.id} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(a.id)} onChange={() => toggle(a.id)} /> {a.name}
        </label>
      ))}
    </div>
  )
}
```

`useAgents(0)` loads once and does not poll (`pollMs <= 0` skips the interval).

`frontend/src/components/dashboards/pickers/RangeSelect.tsx`:

```tsx
import { METRICS_RANGES } from '@/hooks/useMetrics'
import { inputCls } from '@/utils/dashboards'

export default function RangeSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <select className={inputCls} value={value || '24h'} onChange={(e) => onChange(e.target.value)}>
      {METRICS_RANGES.map((r) => (
        <option key={r.value} value={r.value}>
          {r.label}
        </option>
      ))}
    </select>
  )
}
```

- [ ] **Step 3: Write the label settings and the settings switch**

`frontend/src/components/dashboards/settings/LabelSettings.tsx`:

```tsx
import { inputCls, str } from '@/utils/dashboards'

interface Props {
  config: Record<string, unknown>
  siteId: string | null
  onChange: (config: Record<string, unknown>) => void
}

export default function LabelSettings({ config, onChange }: Props) {
  return (
    <div className="space-y-3">
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Text</span>
        <textarea className={inputCls} rows={2} maxLength={200} value={str(config, 'text')} onChange={(e) => onChange({ ...config, text: e.target.value })} />
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Size</span>
        <select className={inputCls} value={str(config, 'size') || 'm'} onChange={(e) => onChange({ ...config, size: e.target.value })}>
          <option value="s">Small</option>
          <option value="m">Medium</option>
          <option value="l">Large</option>
        </select>
      </label>
    </div>
  )
}
```

`frontend/src/components/dashboards/WidgetSettings.tsx`:

```tsx
import type { WidgetType } from '@/types/dashboards'
import LabelSettings from '@/components/dashboards/settings/LabelSettings'

interface Props {
  type: WidgetType
  config: Record<string, unknown>
  siteId: string | null
  onChange: (config: Record<string, unknown>) => void
}

/** The settings form for a widget's type. */
export default function WidgetSettings({ type, config, siteId, onChange }: Props) {
  const props = { config, siteId, onChange }
  switch (type) {
    case 'label':
      return <LabelSettings {...props} />
    default:
      return <p className="text-sm text-slate-500">This widget has no settings form yet.</p>
  }
}
```

(Tasks 24–27 add a `case` per type.)

- [ ] **Step 4: Write the preview, picker and panel**

`frontend/src/components/dashboards/WidgetPreview.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { WidgetResponse, WidgetType } from '@/types/dashboards'
import { widgetInfo } from '@/utils/dashboards'
import WidgetFrame from '@/components/dashboards/WidgetFrame'
import WidgetRenderer from '@/components/dashboards/WidgetRenderer'

interface Props {
  dashboardId: string
  type: WidgetType
  title: string
  config: Record<string, unknown>
}

/** A live preview of an unsaved widget, 600 ms after the last change. The
 *  server resolves it as the editor, so it shows only what the editor may see. */
export default function WidgetPreview({ dashboardId, type, title, config }: Props) {
  const { preview } = useDashboardActions()
  const [result, setResult] = useState<{ response?: WidgetResponse; error?: string } | null>(null)
  const key = JSON.stringify({ type, config })

  useEffect(() => {
    let cancelled = false
    const body = JSON.parse(key) as { type: WidgetType; config: Record<string, unknown> }
    const timer = window.setTimeout(() => {
      preview(dashboardId, body)
        .then((response) => {
          if (!cancelled) setResult({ response })
        })
        .catch((err: ApiError) => {
          if (!cancelled) setResult({ error: err.message || 'Preview failed' })
        })
    }, 600)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [preview, dashboardId, key])

  return (
    <div className="h-64">
      <WidgetFrame
        title={title}
        typeLabel={widgetInfo(type).label}
        state={result?.response?.state}
        hidden={result?.response?.hidden}
        loading={!result}
        error={result?.error}
      >
        {result?.response?.state === 'ok' && <WidgetRenderer type={type} data={result.response.data} linkable={false} />}
      </WidgetFrame>
    </div>
  )
}
```

`frontend/src/components/dashboards/WidgetPicker.tsx`:

```tsx
import { X } from 'lucide-react'
import type { WidgetType } from '@/types/dashboards'
import { WIDGET_TYPES } from '@/utils/dashboards'

export default function WidgetPicker({ onPick, onClose }: { onPick: (t: WidgetType) => void; onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card w-full max-w-2xl space-y-4 p-6" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">Add a widget</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <ul className="grid gap-2 sm:grid-cols-2">
          {WIDGET_TYPES.map((w) => (
            <li key={w.type}>
              <button type="button" className="w-full rounded-lg border border-white/10 bg-slate-800/40 p-3 text-left transition hover:bg-slate-800/70" onClick={() => onPick(w.type)}>
                <p className="font-medium text-slate-100">{w.label}</p>
                <p className="text-sm text-slate-400">{w.description}</p>
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
```

`frontend/src/components/dashboards/WidgetSettingsPanel.tsx`:

```tsx
import { X } from 'lucide-react'
import type { DraftWidget } from '@/utils/dashboards'
import { inputCls, widgetInfo } from '@/utils/dashboards'
import WidgetSettings from '@/components/dashboards/WidgetSettings'
import WidgetPreview from '@/components/dashboards/WidgetPreview'

interface Props {
  dashboardId: string
  siteId: string | null
  widget: DraftWidget
  error?: string
  onChange: (patch: Partial<DraftWidget>) => void
  onClose: () => void
}

/** The side panel for one widget: its title, its type's settings and a live preview. */
export default function WidgetSettingsPanel({ dashboardId, siteId, widget, error, onChange, onClose }: Props) {
  return (
    <aside className="fixed inset-y-0 right-0 z-40 flex w-full max-w-md flex-col gap-4 overflow-y-auto border-l border-white/10 bg-slate-950 p-5 shadow-2xl">
      <div className="flex items-start justify-between">
        <h2 className="text-lg font-semibold">{widgetInfo(widget.type).label}</h2>
        <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close settings">
          <X className="h-5 w-5" />
        </button>
      </div>
      {error && <p className="rounded-md border border-red-500/30 bg-red-500/10 p-2 text-sm text-red-300">{error}</p>}
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Title</span>
        <input className={inputCls} maxLength={100} value={widget.title} placeholder={widgetInfo(widget.type).label} onChange={(e) => onChange({ title: e.target.value })} />
      </label>
      <WidgetSettings type={widget.type} config={widget.config} siteId={siteId} onChange={(config) => onChange({ config })} />
      <div className="space-y-1">
        <p className="text-xs uppercase tracking-widest text-slate-500">Preview</p>
        <WidgetPreview dashboardId={dashboardId} type={widget.type} title={widget.title} config={widget.config} />
      </div>
    </aside>
  )
}
```

- [ ] **Step 5: Write the editor**

`frontend/src/components/dashboards/DashboardEditor.tsx`:

```tsx
import { useEffect, useMemo, useRef, useState } from 'react'
import { Plus, Save } from 'lucide-react'
import type { Layout } from 'react-grid-layout'
import { useDashboardActions, type SaveDashboardInput } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { DashboardDetail, WidgetType } from '@/types/dashboards'
import { defaultConfig, inputCls, nextY, widgetInfo, widgetSummary, type DraftWidget } from '@/utils/dashboards'
import DashboardGrid from '@/components/dashboards/DashboardGrid'
import WidgetFrame from '@/components/dashboards/WidgetFrame'
import WidgetPicker from '@/components/dashboards/WidgetPicker'
import WidgetSettingsPanel from '@/components/dashboards/WidgetSettingsPanel'

interface Props {
  dashboard: DashboardDetail
  onSaved: (d: DashboardDetail) => void
  onCancel: () => void
}

function toDraft(d: DashboardDetail): DraftWidget[] {
  return d.widgets.map((w) => ({ key: w.id, id: w.id, type: w.type, title: w.title, config: w.config ?? {}, x: w.x, y: w.y, w: w.w, h: w.h }))
}

/** Edits a local draft of the whole dashboard; nothing is saved until Save. */
export default function DashboardEditor({ dashboard, onSaved, onCancel }: Props) {
  const { save } = useDashboardActions()
  const [name, setName] = useState(dashboard.name)
  const [description, setDescription] = useState(dashboard.description)
  const [widgets, setWidgets] = useState<DraftWidget[]>(() => toDraft(dashboard))
  const [selected, setSelected] = useState<string | null>(null)
  const [picking, setPicking] = useState(false)
  const [confirmCancel, setConfirmCancel] = useState(false)
  const [saving, setSaving] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [error, setError] = useState<{ key?: string; message: string } | null>(null)
  const counter = useRef(0)

  const initial = useMemo(() => JSON.stringify({ n: dashboard.name, d: dashboard.description, w: toDraft(dashboard) }), [dashboard])
  const dirty = JSON.stringify({ n: name, d: description, w: widgets }) !== initial

  useEffect(() => {
    if (!dirty) return
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault()
    }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  const onLayoutChange = (layout: Layout) =>
    setWidgets((ws) =>
      ws.map((w) => {
        const l = layout.find((i) => i.i === w.key)
        return l && (l.x !== w.x || l.y !== w.y || l.w !== w.w || l.h !== w.h) ? { ...w, x: l.x, y: l.y, w: l.w, h: l.h } : w
      }),
    )

  const addWidget = (type: WidgetType) => {
    const info = widgetInfo(type)
    const key = `new-${counter.current++}`
    setWidgets((ws) => [...ws, { key, type, title: '', config: defaultConfig(type, dashboard.site_id), x: 0, y: nextY(ws), w: info.w, h: info.h }])
    setSelected(key)
    setPicking(false)
  }

  const patch = (key: string, p: Partial<DraftWidget>) => setWidgets((ws) => ws.map((w) => (w.key === key ? { ...w, ...p } : w)))

  const submit = async () => {
    setSaving(true)
    setError(null)
    const input: SaveDashboardInput = {
      version: dashboard.version,
      name,
      description,
      widgets: widgets.map((w) => ({ id: w.id, type: w.type, title: w.title, config: w.config, x: w.x, y: w.y, w: w.w, h: w.h })),
    }
    try {
      onSaved(await save(dashboard.id, input))
    } catch (err) {
      const e = err as ApiError
      if (e.status === 409) {
        setConflict(true)
      } else {
        const index = e.details?.widget_index
        const w = typeof index === 'number' ? widgets[index] : undefined
        if (w) setSelected(w.key)
        setError({ key: w?.key, message: e.message || 'Could not save' })
      }
    } finally {
      setSaving(false)
    }
  }

  const current = widgets.find((w) => w.key === selected)
  const gridItems = widgets.map((w) => ({ ...w, id: w.key }))

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <label className="min-w-64 flex-1 space-y-1">
          <span className="text-xs uppercase tracking-widest text-slate-500">Name</span>
          <input className={inputCls} value={name} maxLength={100} onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="min-w-64 flex-1 space-y-1">
          <span className="text-xs uppercase tracking-widest text-slate-500">Description</span>
          <input className={inputCls} value={description} maxLength={500} onChange={(e) => setDescription(e.target.value)} />
        </label>
        <button className="btn-secondary flex items-center gap-2" onClick={() => setPicking(true)} disabled={widgets.length >= 50}>
          <Plus className="h-4 w-4" /> Add widget
        </button>
        {confirmCancel ? (
          <>
            <span className="text-sm text-amber-300">Discard your changes?</span>
            <button className="btn-secondary" onClick={() => setConfirmCancel(false)}>
              Keep editing
            </button>
            <button className="btn bg-red-600 text-white hover:bg-red-700" onClick={onCancel}>
              Discard
            </button>
          </>
        ) : (
          <button className="btn-secondary" onClick={() => (dirty ? setConfirmCancel(true) : onCancel())}>
            Cancel
          </button>
        )}
        <button className="btn-primary flex items-center gap-2" onClick={() => void submit()} disabled={saving || conflict || name.trim() === ''}>
          <Save className="h-4 w-4" /> {saving ? 'Saving…' : 'Save'}
        </button>
      </div>

      {conflict && (
        <div className="flex flex-wrap items-center gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-200">
          Someone else saved this dashboard. Reload to see their changes; your edits will be lost.
          <button className="btn-secondary" onClick={() => window.location.reload()}>
            Reload
          </button>
        </div>
      )}
      {error && !error.key && <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">{error.message}</div>}
      <p className="text-xs text-slate-500 lg:hidden">Editing works best on a wide screen.</p>

      {widgets.length === 0 ? (
        <p className="text-slate-500">No widgets yet. Choose Add widget.</p>
      ) : (
        <DashboardGrid
          items={gridItems}
          editing
          onLayoutChange={onLayoutChange}
          render={(w) => (
            <WidgetFrame
              title={w.title}
              typeLabel={widgetInfo(w.type).label}
              editing
              selected={w.key === selected}
              onEdit={() => setSelected(w.key)}
              onRemove={() => {
                setWidgets((ws) => ws.filter((x) => x.key !== w.key))
                if (selected === w.key) setSelected(null)
              }}
            >
              <p className="text-sm text-slate-400">{widgetSummary(w)}</p>
              {error?.key === w.key && <p className="mt-1 text-sm text-red-400">{error.message}</p>}
            </WidgetFrame>
          )}
        />
      )}

      {picking && <WidgetPicker onPick={addWidget} onClose={() => setPicking(false)} />}
      {current && (
        <WidgetSettingsPanel
          dashboardId={dashboard.id}
          siteId={dashboard.site_id}
          widget={current}
          error={error?.key === current.key ? error.message : undefined}
          onChange={(p) => patch(current.key, p)}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  )
}
```

- [ ] **Step 6: Use the editor in the page**

In `DashboardPage.tsx`, import `DashboardEditor` and `useNavigate`, take `setDashboard` from `useDashboard(id)`, and replace the edit branch with:

```tsx
  if (mode === 'edit') {
    if (!dashboard.can_edit) {
      return (
        <p className="text-slate-400">
          {dashboard.published ? 'This dashboard has a public link, so only an admin can change it.' : 'You can view this dashboard but not change it.'}
        </p>
      )
    }
    return (
      <DashboardEditor
        dashboard={dashboard}
        onSaved={(d) => {
          setDashboard(d)
          navigate(`/dashboards/${d.id}`)
        }}
        onCancel={() => navigate(`/dashboards/${dashboard.id}`)}
      />
    )
  }
```

- [ ] **Step 7: Run the gate and try it**

Run the frontend gate. Then: create a personal dashboard, add a Label, change its text (the preview updates after a pause), drag and resize it, Save — the view shows the label where you put it. Edit again, change something, Cancel → "Discard your changes?". Open the same dashboard's editor in two tabs, save in one, then save in the other → the conflict banner. Set a label's text to 201 characters through the API to see a 400 highlight the widget (or trust Task 6's tests and skip).

- [ ] **Step 8: Commit**

```bash
git add frontend/src/components/dashboards frontend/src/pages/dashboards/DashboardPage.tsx frontend/src/utils/dashboards.ts
git commit -m "feat(dashboards): editor with drag and resize, widget picker, settings and live preview

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 24: Time series and stat widgets in the UI

**Files:**
- Modify: `frontend/src/components/network/TrafficChart.tsx` (`AreaSeriesChart` gains `bare`)
- Modify: `frontend/src/utils/dashboards.ts` (`SettingsProps`)
- Create: `frontend/src/components/dashboards/settings/Field.tsx`
- Create: `frontend/src/components/dashboards/widgets/TimeseriesWidget.tsx`, `StatWidget.tsx`
- Create: `frontend/src/components/dashboards/settings/TimeseriesSettings.tsx`, `StatSettings.tsx`
- Modify: `frontend/src/components/dashboards/WidgetRenderer.tsx`, `WidgetSettings.tsx` (two cases each)

**Interfaces:**
- Consumes: `AreaSeriesChart`, `chartRows`, `unitOf`, `formatMetric`, `chartPalette`, `colors`, pickers from Task 23, `str`/`strs`/`num`.
- Produces: `AreaSeriesChartProps.bare?: boolean`; `interface SettingsProps { config: Record<string, unknown>; siteId: string | null; onChange: (config: Record<string, unknown>) => void }` in `utils/dashboards.ts`; `Field({ label, hint?, children })`.

- [ ] **Step 1: Let the chart fill a widget**

In `frontend/src/components/network/TrafficChart.tsx`, add to `AreaSeriesChartProps`:

```ts
  /** Fill the parent instead of drawing a fixed-height card: dashboard
   *  widgets, whose frame is already the card. */
  bare?: boolean
```

and in `AreaSeriesChart`, take `bare = false` from the props and replace the two opening divs:

```tsx
    <div className={bare ? 'flex h-full flex-col' : 'rounded-lg border border-white/10 bg-slate-800/40 p-4'}>
      <div className={bare ? 'min-h-0 flex-1' : 'h-64'}>
```

Every existing caller omits `bare`, so device, port and site pages are unchanged.

- [ ] **Step 2: Add the settings props type and the field wrapper**

Append to `frontend/src/utils/dashboards.ts`:

```ts
/** What every widget settings form takes. siteId is the dashboard's site. */
export interface SettingsProps {
  config: Record<string, unknown>
  siteId: string | null
  onChange: (config: Record<string, unknown>) => void
}
```

`frontend/src/components/dashboards/settings/Field.tsx`:

```tsx
import type { ReactNode } from 'react'

export default function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="space-y-1">
      <p className="text-sm text-slate-300">{label}</p>
      {children}
      {hint && <p className="text-xs text-slate-500">{hint}</p>}
    </div>
  )
}
```

- [ ] **Step 3: Write the renderers**

`frontend/src/components/dashboards/widgets/TimeseriesWidget.tsx`:

```tsx
import { useMemo } from 'react'
import { AreaSeriesChart } from '@/components/network/TrafficChart'
import type { TimeseriesData } from '@/types/dashboards'
import { chartPalette } from '@/utils/colors'
import { chartRows, unitOf } from '@/utils/dashboards'

const SHORT = new Set(['1h', '6h', '24h'])

export default function TimeseriesWidget({ data }: { data: TimeseriesData }) {
  const rows = useMemo(() => chartRows(data.lines), [data.lines])
  const lines = useMemo(
    () => data.lines.map((l, i) => ({ metric: l.key, label: l.label, colour: chartPalette[i % chartPalette.length] })),
    [data.lines],
  )
  const unit = data.lines[0]?.unit ?? ''
  return (
    <AreaSeriesChart bare data={rows} lines={lines} unit={unitOf(unit)} unitLabel={unit} step={data.step_seconds} shortTicks={SHORT.has(data.range)} />
  )
}
```

`frontend/src/components/dashboards/widgets/StatWidget.tsx`:

```tsx
import { useMemo } from 'react'
import { Area, AreaChart, ResponsiveContainer } from 'recharts'
import type { StatData } from '@/types/dashboards'
import { chartPalette, colors } from '@/utils/colors'
import { formatMetric } from '@/utils/dashboards'

const LEVEL = { ok: colors.operational.text, warn: colors.warning.text, crit: colors.error.text } as const

export default function StatWidget({ data }: { data: StatData }) {
  const spark = useMemo(() => (data.spark ?? []).map((p) => ({ t: new Date(p.t).getTime(), v: p.avg })), [data.spark])
  return (
    <div className="flex h-full flex-col justify-between gap-2">
      <p className="text-xs text-slate-400">
        {data.label}
        {data.mode === 'average' && ` · average over ${data.range}`}
      </p>
      <p className={`text-4xl font-light tabular-nums ${LEVEL[data.level] ?? LEVEL.ok}`}>{formatMetric(data.value, data.unit)}</p>
      {spark.length > 1 && (
        <div className="h-10">
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={spark} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
              <Area type="monotone" dataKey="v" stroke={chartPalette[0]} fill={chartPalette[0]} fillOpacity={0.15} isAnimationActive={false} />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Write the settings forms**

`frontend/src/components/dashboards/settings/TimeseriesSettings.tsx`:

```tsx
import { inputCls, str, strs, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'
import MetricPicker from '@/components/dashboards/pickers/MetricPicker'
import InstancePicker from '@/components/dashboards/pickers/InstancePicker'
import RangeSelect from '@/components/dashboards/pickers/RangeSelect'

export default function TimeseriesSettings({ config, siteId, onChange }: SettingsProps) {
  const source = str(config, 'source') || 'metrics'
  const range = str(config, 'range') || '24h'
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const devices = strs(config, 'devices')
  const metrics = strs(config, 'metrics')
  const siteTotal = source === 'metrics' && 'site_id' in config && config.site_id !== undefined

  return (
    <div className="space-y-3">
      <Field label="Show">
        <select
          className={inputCls}
          value={source}
          onChange={(e) =>
            onChange(
              e.target.value === 'site_traffic'
                ? { source: 'site_traffic', site_id: siteId, view: 'internet', range }
                : { source: 'metrics', metrics: [], devices: [], range },
            )
          }
        >
          <option value="metrics">Metrics from devices</option>
          <option value="site_traffic">A site's traffic</option>
        </select>
      </Field>

      {source === 'site_traffic' ? (
        <>
          <Field label="Site">
            <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
          </Field>
          <Field label="Traffic" hint="Internet traffic needs a port marked WAN at the site.">
            <select className={inputCls} value={str(config, 'view') || 'internet'} onChange={(e) => set({ view: e.target.value })}>
              <option value="internet">Internet (download and upload)</option>
              <option value="east_west">Inside the site</option>
            </select>
          </Field>
        </>
      ) : (
        <>
          <label className="flex items-center gap-2 text-sm text-slate-300">
            <input
              type="checkbox"
              checked={siteTotal}
              onChange={(e) =>
                e.target.checked
                  ? set({ site_id: siteId, devices: [], instances: [], metrics: metrics.filter((m) => m.startsWith('if_')) })
                  : set({ site_id: undefined })
              }
            />
            A whole site's total
          </label>
          {siteTotal ? (
            <Field label="Site">
              <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
            </Field>
          ) : (
            <Field label="Devices" hint="Up to 50.">
              <DevicePicker siteId={siteId} value={devices} onChange={(ids) => set({ devices: ids, instances: [] })} />
            </Field>
          )}
          <Field label="Metrics" hint={siteTotal ? 'Site totals add up the ports of every device.' : 'Up to 10, listed from the first device.'}>
            <MetricPicker deviceId={devices[0]} portOnly={siteTotal} value={metrics} onChange={(m) => set({ metrics: m, instances: [] })} />
          </Field>
          {!siteTotal && devices.length === 1 && metrics.length === 1 && (
            <Field label="Ports or rows">
              <InstancePicker deviceId={devices[0]} metric={metrics[0]} value={strs(config, 'instances')} onChange={(v) => set({ instances: v })} />
            </Field>
          )}
        </>
      )}
      <Field label="Time range">
        <RangeSelect value={range} onChange={(r) => set({ range: r })} />
      </Field>
    </div>
  )
}
```

`frontend/src/components/dashboards/settings/StatSettings.tsx`:

```tsx
import { inputCls, str, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'
import MetricPicker from '@/components/dashboards/pickers/MetricPicker'
import InstancePicker from '@/components/dashboards/pickers/InstancePicker'
import RangeSelect from '@/components/dashboards/pickers/RangeSelect'

function numberOrUndefined(v: string): number | undefined {
  return v === '' || Number.isNaN(Number(v)) ? undefined : Number(v)
}

export default function StatSettings({ config, siteId, onChange }: SettingsProps) {
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const siteTotal = 'site_id' in config && config.site_id !== undefined
  const device = str(config, 'device_id')
  const metric = str(config, 'metric')

  return (
    <div className="space-y-3">
      <Field label="Of">
        <select
          className={inputCls}
          value={siteTotal ? 'site' : 'device'}
          onChange={(e) => (e.target.value === 'site' ? set({ site_id: siteId, device_id: undefined, instance: undefined }) : set({ site_id: undefined }))}
        >
          <option value="device">One device</option>
          <option value="site">A whole site's total</option>
        </select>
      </Field>
      {siteTotal ? (
        <Field label="Site">
          <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
        </Field>
      ) : (
        <Field label="Device">
          <DevicePicker single siteId={siteId} value={device ? [device] : []} onChange={(ids) => set({ device_id: ids[0], instance: undefined })} />
        </Field>
      )}
      <Field label="Metric">
        <MetricPicker single deviceId={device || undefined} portOnly={siteTotal} value={metric ? [metric] : []} onChange={(m) => set({ metric: m[0], instance: undefined })} />
      </Field>
      {!siteTotal && (
        <InstancePicker single deviceId={device || undefined} metric={metric || undefined} value={str(config, 'instance') ? [str(config, 'instance')] : []} onChange={(v) => set({ instance: v[0] })} />
      )}
      <Field label="Show">
        <select className={inputCls} value={str(config, 'mode') || 'latest'} onChange={(e) => set({ mode: e.target.value })}>
          <option value="latest">The latest value</option>
          <option value="average">The average over the range</option>
        </select>
      </Field>
      <div className="grid grid-cols-3 gap-2">
        <Field label="Warning at">
          <input className={inputCls} type="number" value={config.warn === undefined ? '' : String(config.warn)} onChange={(e) => set({ warn: numberOrUndefined(e.target.value) })} />
        </Field>
        <Field label="Critical at">
          <input className={inputCls} type="number" value={config.crit === undefined ? '' : String(config.crit)} onChange={(e) => set({ crit: numberOrUndefined(e.target.value) })} />
        </Field>
        <Field label="When">
          <select className={inputCls} value={str(config, 'direction') || 'above'} onChange={(e) => set({ direction: e.target.value })}>
            <option value="above">Above</option>
            <option value="below">Below</option>
          </select>
        </Field>
      </div>
      <label className="flex items-center gap-2 text-sm text-slate-300">
        <input type="checkbox" checked={config.sparkline === true} onChange={(e) => set({ sparkline: e.target.checked })} /> Show a sparkline
      </label>
      <Field label="Time range">
        <RangeSelect value={str(config, 'range')} onChange={(r) => set({ range: r })} />
      </Field>
    </div>
  )
}
```

- [ ] **Step 5: Register both**

In `WidgetRenderer.tsx`, import `TimeseriesWidget`, `StatWidget` and their data types, and add:

```tsx
    case 'timeseries':
      return <TimeseriesWidget data={data as TimeseriesData} />
    case 'stat':
      return <StatWidget data={data as StatData} />
```

In `WidgetSettings.tsx`, import both forms and add:

```tsx
    case 'timeseries':
      return <TimeseriesSettings {...props} />
    case 'stat':
      return <StatSettings {...props} />
```

- [ ] **Step 6: Run the gate and try it**

Run the frontend gate. On a dashboard: add a Time series of a switch's traffic in and out, a site total, and a site's internet traffic; add a Stat with warning and critical thresholds. Check the device, port and site pages still draw their charts as before (the `bare` change must not affect them).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/network/TrafficChart.tsx frontend/src/utils/dashboards.ts frontend/src/components/dashboards
git commit -m "feat(dashboards): time series and stat widgets in the UI

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 25: Port grid, device health and site power widgets in the UI

**Files:**
- Create: `frontend/src/utils/health.ts`, `frontend/src/utils/ups.ts`
- Create: `frontend/src/components/network/HealthRowTile.tsx`, `UPSReadingTiles.tsx`, `UPSSourceBadge.tsx`
- Modify: `frontend/src/components/network/HealthSection.tsx`, `UPSPanel.tsx` (use the extracted pieces)
- Create: `frontend/src/components/dashboards/widgets/PortGridWidget.tsx`, `DeviceHealthWidget.tsx`, `SitePowerWidget.tsx`
- Create: `frontend/src/components/dashboards/settings/DeviceSettings.tsx`, `SiteSettings.tsx`
- Modify: `WidgetRenderer.tsx`, `WidgetSettings.tsx`

**Interfaces:**
- Produces: `CHIP_TONE`, `chipTone(row)`, `rowValueText(row, units)` in `utils/health.ts`; `HealthRowTile({ row, units, open?, onToggle? })` (no `onToggle`: a numeric row is not clickable); `BATTERY_STATUS`, `upsTiles(readings): { key, label, value }[]` in `utils/ups.ts`; `UPSReadingTiles({ readings, compact?, only? })`; `UPSSourceBadge({ onBattery })`; `DeviceSettings` (one device), `SiteSettings` (one site).

- [ ] **Step 1: Move the health tile out of `HealthSection.tsx`**

`frontend/src/utils/health.ts`:

```ts
import type { HealthRow } from '@/hooks/useDeviceHealth'

export const CHIP_TONE = {
  ok: 'border-emerald-500/30 bg-emerald-500/15 text-emerald-300',
  problem: 'border-red-500/30 bg-red-500/15 text-red-300',
  warn: 'border-amber-500/30 bg-amber-500/15 text-amber-300',
} as const

/** green when the row's state is OK, red while an incident is open for it,
 *  else amber (a state outside the OK list, but with no rule raising it). */
export function chipTone(row: HealthRow): keyof typeof CHIP_TONE {
  if (row.problem) return 'problem'
  if (row.ok) return 'ok'
  return 'warn'
}

export function rowValueText(row: HealthRow, units: string): string {
  return `${+row.value.toFixed(1)} ${units}`.trim()
}
```

`frontend/src/components/network/HealthRowTile.tsx` — `RowTile` from `HealthSection.tsx`, moved, with the toggle made optional:

```tsx
import type { HealthRow } from '@/hooks/useDeviceHealth'
import { CHIP_TONE, chipTone, rowValueText } from '@/utils/health'

interface Props {
  row: HealthRow
  units: string
  open?: boolean
  /** Without it a numeric row is a plain tile (dashboards); with it, a
   *  button that opens the row's history chart (the device page). */
  onToggle?: () => void
}

/** One row's tile: a status row shows a coloured chip; a numeric row shows
 *  its value. A red ring marks a row an incident is open for; a row with an
 *  open incident but no live sample says "No reading". */
export default function HealthRowTile({ row, units, open = false, onToggle }: Props) {
  const ring = row.problem ? 'ring-2 ring-red-500/60' : ''
  if (row.no_reading) {
    return (
      <div className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 ${ring}`}>
        <p className="truncate text-xs text-slate-400">{row.label}</p>
        <p className="mt-1 text-sm text-red-300">No reading</p>
      </div>
    )
  }
  if (row.state !== undefined) {
    return (
      <div className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 ${ring}`}>
        <p className="truncate text-xs text-slate-400">{row.label}</p>
        <span className={`mt-1 inline-flex rounded-full border px-2 py-0.5 text-xs font-medium ${CHIP_TONE[chipTone(row)]}`}>{row.state}</span>
      </div>
    )
  }
  const body = (
    <>
      <p className="truncate text-xs text-slate-400">{row.label}</p>
      <p className="mt-1 text-sm tabular-nums text-white">{rowValueText(row, units)}</p>
    </>
  )
  if (!onToggle) return <div className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 ${ring}`}>{body}</div>
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={open}
      className={`rounded-lg border border-white/10 bg-slate-800/40 p-2 text-left transition hover:bg-slate-800/60 ${ring}`}
    >
      {body}
    </button>
  )
}
```

In `HealthSection.tsx`: delete `CHIP_TONE`, `chipTone`, `rowValueText` and `RowTile`; import `HealthRowTile` from `@/components/network/HealthRowTile`; replace every `<RowTile` with `<HealthRowTile` (the props are the same); remove imports left unused.

- [ ] **Step 2: Move the UPS pieces out of `UPSPanel.tsx`**

`frontend/src/utils/ups.ts`:

```ts
export const BATTERY_STATUS: Record<number, string> = { 1: 'Battery status unknown', 3: 'Battery low', 4: 'Battery depleted' }

/** A UPS's readings as tiles, in display order; missing readings are left out. */
export function upsTiles(r: Record<string, number>): { key: string; label: string; value: string }[] {
  const out: { key: string; label: string; value: string }[] = []
  const add = (key: string, label: string, fmt: (n: number) => string) => {
    if (r[key] !== undefined) out.push({ key, label, value: fmt(r[key]) })
  }
  add('ups_charge_pct', 'Charge', (n) => `${Math.round(n)}%`)
  add('ups_runtime_min', 'Runtime left', (n) => `${Math.round(n)} min`)
  add('ups_load_pct', 'Load', (n) => `${Math.round(n)}%`)
  add('ups_input_v', 'Input', (n) => `${Math.round(n)} V`)
  add('ups_output_v', 'Output', (n) => `${Math.round(n)} V`)
  add('ups_battery_temp_c', 'Battery temperature', (n) => `${Math.round(n)} °C`)
  return out
}
```

`frontend/src/components/network/UPSSourceBadge.tsx`:

```tsx
import { BatteryWarning, Plug } from 'lucide-react'

/** On mains, on battery, or unknown, from the ups_on_battery reading. */
export default function UPSSourceBadge({ onBattery }: { onBattery: number | undefined }) {
  if (onBattery === 1) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/30 bg-amber-500/15 px-2.5 py-0.5 text-xs font-medium text-amber-300">
        <BatteryWarning className="h-3.5 w-3.5" /> On battery
      </span>
    )
  }
  if (onBattery === 0) {
    return (
      <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/15 px-2.5 py-0.5 text-xs font-medium text-emerald-300">
        <Plug className="h-3.5 w-3.5" /> On mains
      </span>
    )
  }
  return <span className="rounded-full border border-slate-500/30 bg-slate-500/15 px-2.5 py-0.5 text-xs text-slate-300">Source unknown</span>
}
```

`frontend/src/components/network/UPSReadingTiles.tsx`:

```tsx
import { BatteryCharging } from 'lucide-react'
import { upsTiles } from '@/utils/ups'

interface Props {
  readings: Record<string, number>
  /** Smaller tiles in two columns, for dashboard widgets. */
  compact?: boolean
  /** Only these reading keys (e.g. charge, runtime, load). */
  only?: string[]
}

export default function UPSReadingTiles({ readings, compact = false, only }: Props) {
  const tiles = upsTiles(readings).filter((t) => !only || only.includes(t.key))
  return (
    <dl className={compact ? 'grid grid-cols-3 gap-2' : 'grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6'}>
      {tiles.map((t) => (
        <div key={t.key} className={`rounded-lg border border-white/10 bg-slate-800/40 ${compact ? 'p-2' : 'p-3'}`}>
          <dt className="flex items-center gap-1 text-xs uppercase tracking-widest text-slate-500">
            {t.key === 'ups_charge_pct' && <BatteryCharging className="h-3.5 w-3.5" />}
            {t.label}
          </dt>
          <dd className={`mt-1 font-light tabular-nums text-white ${compact ? 'text-base' : 'text-xl'}`}>{t.value}</dd>
        </div>
      ))}
    </dl>
  )
}
```

In `UPSPanel.tsx`: delete `BATTERY_STATUS`, the `tiles` array and its `add` helper; import `BATTERY_STATUS` from `@/utils/ups`, `UPSSourceBadge` and `UPSReadingTiles`; replace the three-way source badge with `{hasData && <UPSSourceBadge onBattery={onBattery} />}` and the `<dl>…</dl>` block with `<UPSReadingTiles readings={r} />`. Drop the now-unused lucide imports.

- [ ] **Step 3: Write the three renderers**

`frontend/src/components/dashboards/widgets/PortGridWidget.tsx`:

```tsx
import { useNavigate } from 'react-router-dom'
import Faceplate from '@/components/network/Faceplate'
import type { PortGridData } from '@/types/dashboards'

export default function PortGridWidget({ data, linkable }: { data: PortGridData; linkable: boolean }) {
  const navigate = useNavigate()
  return (
    <Faceplate
      faceplates={data.faceplates}
      ports={data.ports}
      title={data.device_name}
      subtitle={data.model || undefined}
      selected={null}
      onSelect={(ifIndex) => {
        if (linkable && data.device_id) navigate(`/network/devices/${data.device_id}/ports/${ifIndex}`)
      }}
    />
  )
}
```

`frontend/src/components/dashboards/widgets/DeviceHealthWidget.tsx`:

```tsx
import { Link } from 'react-router-dom'
import HealthRowTile from '@/components/network/HealthRowTile'
import type { DeviceHealthData } from '@/types/dashboards'

export default function DeviceHealthWidget({ data, linkable }: { data: DeviceHealthData; linkable: boolean }) {
  return (
    <div className="space-y-3">
      {data.metrics.map((m) => (
        <div key={m.key} className="space-y-1">
          <p className="text-xs text-slate-400">
            {m.name}
            {m.rule && ` · alert when ${m.rule}`}
          </p>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(7rem,1fr))] gap-2">
            {m.rows.map((r) => (
              <HealthRowTile key={r.instance} row={r} units={m.units} />
            ))}
          </div>
        </div>
      ))}
      {linkable && data.device_id && (
        <Link to={`/network/devices/${data.device_id}`} className="text-xs text-primary-400 hover:underline">
          Open {data.device_name}
        </Link>
      )}
    </div>
  )
}
```

`frontend/src/components/dashboards/widgets/SitePowerWidget.tsx`:

```tsx
import { Link } from 'react-router-dom'
import UPSReadingTiles from '@/components/network/UPSReadingTiles'
import UPSSourceBadge from '@/components/network/UPSSourceBadge'
import type { SitePowerData } from '@/types/dashboards'
import { CONDITION_LABEL } from '@/utils/network'

const KEY_READINGS = ['ups_charge_pct', 'ups_runtime_min', 'ups_load_pct']

export default function SitePowerWidget({ data, linkable }: { data: SitePowerData; linkable: boolean }) {
  return (
    <ul className="space-y-3">
      {data.upses.map((u, i) => (
        <li key={u.device_id ?? `${u.name}-${i}`} className="space-y-2 rounded-lg border border-white/10 p-2">
          <div className="flex flex-wrap items-center gap-2">
            {linkable && u.device_id ? (
              <Link to={`/network/devices/${u.device_id}`} className="font-medium text-slate-200 hover:text-primary-400">
                {u.name}
              </Link>
            ) : (
              <span className="font-medium text-slate-200">{u.name}</span>
            )}
            {u.status === 'down' ? (
              <span className="rounded-full border border-red-500/30 bg-red-500/15 px-2 py-0.5 text-xs text-red-300">Not answering</span>
            ) : (
              <UPSSourceBadge onBattery={u.readings.ups_on_battery} />
            )}
            {u.conditions
              .filter((c) => c !== 'ups_on_battery')
              .map((c) => (
                <span key={c} className="rounded-full border border-red-500/30 bg-red-500/15 px-2 py-0.5 text-xs text-red-300">
                  {CONDITION_LABEL[c] ?? c}
                </span>
              ))}
          </div>
          <UPSReadingTiles readings={u.readings} compact only={KEY_READINGS} />
        </li>
      ))}
    </ul>
  )
}
```

- [ ] **Step 4: Write the two settings forms**

`frontend/src/components/dashboards/settings/DeviceSettings.tsx`:

```tsx
import { str, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'

/** One device: the port grid and device health widgets. */
export default function DeviceSettings({ config, siteId, onChange }: SettingsProps) {
  const device = str(config, 'device_id')
  return (
    <Field label="Device">
      <DevicePicker single siteId={siteId} value={device ? [device] : []} onChange={(ids) => onChange({ ...config, device_id: ids[0] })} />
    </Field>
  )
}
```

`frontend/src/components/dashboards/settings/SiteSettings.tsx`:

```tsx
import { str, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'

/** One site: the site power widget. */
export default function SiteSettings({ config, onChange }: SettingsProps) {
  return (
    <Field label="Site">
      <SitePicker value={str(config, 'site_id') || null} onChange={(id) => onChange({ ...config, site_id: id })} />
    </Field>
  )
}
```

- [ ] **Step 5: Register them**

`WidgetRenderer.tsx`:

```tsx
    case 'port_grid':
      return <PortGridWidget data={data as PortGridData} linkable={linkable} />
    case 'device_health':
      return <DeviceHealthWidget data={data as DeviceHealthData} linkable={linkable} />
    case 'site_power':
      return <SitePowerWidget data={data as SitePowerData} linkable={linkable} />
```

(take `linkable` from the props now: `export default function WidgetRenderer({ type, data, linkable }: Props)`).

`WidgetSettings.tsx`:

```tsx
    case 'port_grid':
    case 'device_health':
      return <DeviceSettings {...props} />
    case 'site_power':
      return <SiteSettings {...props} />
```

- [ ] **Step 6: Run the gate and try it**

Run the frontend gate. Check the device page's Health section and Power panel look exactly as before (the extraction must not change them), then add a Port grid, a Device health and a Site power widget to a dashboard.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/utils/health.ts frontend/src/utils/ups.ts frontend/src/components/network frontend/src/components/dashboards
git commit -m "feat(dashboards): port grid, device health and site power widgets in the UI

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 26: Top ports, event log, device table and open incidents in the UI

**Files:**
- Create: `frontend/src/components/dashboards/widgets/TopNWidget.tsx`, `EventLogWidget.tsx`, `DeviceTableWidget.tsx`, `OpenIncidentsWidget.tsx`
- Create: `frontend/src/components/dashboards/settings/TopNSettings.tsx`, `EventLogSettings.tsx`, `DeviceTableSettings.tsx`, `OpenIncidentsSettings.tsx`
- Modify: `WidgetRenderer.tsx`, `WidgetSettings.tsx`

**Interfaces:**
- Consumes: `formatMetric`, `EVENT_LABEL`, `CONDITION_LABEL`, `timeAgo` (`utils/network`), `formatDuration` (`hooks/useIncidents`), `DeviceStatusBadge`, `DEVICE_TYPE_LABEL`, pickers.

- [ ] **Step 1: Write the renderers**

`frontend/src/components/dashboards/widgets/TopNWidget.tsx`:

```tsx
import { Link } from 'react-router-dom'
import type { TopNData } from '@/types/dashboards'
import { formatMetric } from '@/utils/dashboards'

export default function TopNWidget({ data, linkable }: { data: TopNData; linkable: boolean }) {
  const max = Math.max(1, ...data.items.map((i) => i.value))
  return (
    <ol className="space-y-2">
      {data.items.map((it, i) => {
        const label = `${it.device_name} · ${it.port_name}${it.alias ? ` (${it.alias})` : ''}`
        return (
          <li key={`${it.device_name}-${it.if_index}-${i}`} className="space-y-0.5">
            <div className="flex items-baseline justify-between gap-2">
              {linkable && it.device_id ? (
                <Link to={`/network/devices/${it.device_id}/ports/${it.if_index}`} className="truncate text-slate-200 hover:text-primary-400">
                  {label}
                </Link>
              ) : (
                <span className="truncate text-slate-200">{label}</span>
              )}
              <span className="shrink-0 tabular-nums text-slate-300">{formatMetric(it.value, data.unit)}</span>
            </div>
            <div className="h-1.5 rounded bg-slate-800">
              <div className="h-1.5 rounded bg-cyan-500/70" style={{ width: `${(it.value / max) * 100}%` }} />
            </div>
          </li>
        )
      })}
    </ol>
  )
}
```

`frontend/src/components/dashboards/widgets/EventLogWidget.tsx`:

```tsx
import { Link } from 'react-router-dom'
import type { EventItem, EventLogData } from '@/types/dashboards'
import { CONDITION_LABEL, EVENT_LABEL } from '@/utils/network'

const KIND_LABEL: Record<string, string> = { ...EVENT_LABEL, incident_opened: 'Incident opened', incident_closed: 'Incident closed' }
const DOT: Record<string, string> = {
  incident_opened: 'bg-red-400',
  incident_closed: 'bg-emerald-400',
  link_down: 'bg-red-400',
  link_up: 'bg-emerald-400',
}

function what(e: EventItem): string {
  if (e.kind.startsWith('incident_')) {
    return e.condition && CONDITION_LABEL[e.condition] && !e.subject.includes(CONDITION_LABEL[e.condition])
      ? `${e.subject} · ${CONDITION_LABEL[e.condition]}`
      : e.subject
  }
  return `${e.device_name} · ${e.port_label || e.port_name}`
}

export default function EventLogWidget({ data, linkable }: { data: EventLogData; linkable: boolean }) {
  if (data.items.length === 0) return <p className="text-slate-500">No events yet.</p>
  return (
    <ul className="divide-y divide-white/5">
      {data.items.map((e, i) => {
        const href = !linkable
          ? null
          : e.incident_id
            ? `/incidents/${e.incident_id}`
            : e.device_id && e.if_index != null
              ? `/network/devices/${e.device_id}/ports/${e.if_index}`
              : null
        return (
          <li key={`${e.time}-${e.kind}-${i}`} className="flex items-center gap-2 py-1.5">
            <span className={`h-2 w-2 shrink-0 rounded-full ${DOT[e.kind] ?? 'bg-slate-500'}`} aria-hidden />
            <span className="w-24 shrink-0 tabular-nums text-xs text-slate-500">
              {new Date(e.time).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
            </span>
            <span className="shrink-0 text-slate-300">{KIND_LABEL[e.kind] ?? e.kind}</span>
            {href ? (
              <Link to={href} className="truncate text-slate-200 hover:text-primary-400">
                {what(e)}
              </Link>
            ) : (
              <span className="truncate text-slate-200">{what(e)}</span>
            )}
          </li>
        )
      })}
    </ul>
  )
}
```

`frontend/src/components/dashboards/widgets/DeviceTableWidget.tsx`:

```tsx
import { Link } from 'react-router-dom'
import DeviceStatusBadge from '@/components/network/DeviceStatusBadge'
import { DEVICE_TYPE_LABEL } from '@/hooks/useDevices'
import type { DeviceTableData } from '@/types/dashboards'
import { timeAgo } from '@/utils/network'

export default function DeviceTableWidget({ data, linkable }: { data: DeviceTableData; linkable: boolean }) {
  if (data.devices.length === 0) return <p className="text-slate-500">No devices.</p>
  const showHost = data.devices.some((d) => d.host)
  return (
    <table className="w-full text-left">
      <thead className="text-xs uppercase tracking-widest text-slate-500">
        <tr>
          <th className="py-1 pr-3 font-medium">Status</th>
          <th className="py-1 pr-3 font-medium">Name</th>
          <th className="py-1 pr-3 font-medium">Type</th>
          {showHost && <th className="py-1 pr-3 font-medium">Address</th>}
          <th className="py-1 pr-3 font-medium">Vendor / model</th>
          <th className="py-1 pr-3 font-medium">Last seen</th>
          <th className="py-1 pr-3 text-right font-medium">30 days</th>
          <th className="py-1 text-right font-medium">Open</th>
        </tr>
      </thead>
      <tbody className="divide-y divide-white/5">
        {data.devices.map((d, i) => (
          <tr key={d.device_id ?? `${d.name}-${i}`}>
            <td className="py-1.5 pr-3">
              <DeviceStatusBadge status={d.status} />
            </td>
            <td className="py-1.5 pr-3 font-medium text-slate-200">
              {linkable && d.device_id ? (
                <Link to={`/network/devices/${d.device_id}`} className="hover:text-primary-400">
                  {d.name}
                </Link>
              ) : (
                d.name
              )}
            </td>
            <td className="py-1.5 pr-3 text-slate-400">{DEVICE_TYPE_LABEL[d.type] ?? d.type}</td>
            {showHost && <td className="py-1.5 pr-3 font-mono text-xs text-slate-400">{d.host}</td>}
            <td className="py-1.5 pr-3 text-slate-400">{d.vendor_model || '—'}</td>
            <td className="py-1.5 pr-3 text-slate-400">{d.last_seen_at ? timeAgo(d.last_seen_at) : '—'}</td>
            <td className="py-1.5 pr-3 text-right tabular-nums text-slate-300">
              {d.availability_30d == null ? '—' : `${d.availability_30d.toFixed(2)}%`}
            </td>
            <td className={`py-1.5 text-right tabular-nums ${d.open_incidents > 0 ? 'text-red-300' : 'text-slate-500'}`}>{d.open_incidents}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
```

`frontend/src/components/dashboards/widgets/OpenIncidentsWidget.tsx`:

```tsx
import { Link } from 'react-router-dom'
import { formatDuration } from '@/hooks/useIncidents'
import type { OpenIncidentsData } from '@/types/dashboards'

const SEVERITY: Record<string, string> = {
  critical: 'border-red-500/40 bg-red-500/20 text-red-200',
  high: 'border-red-500/30 bg-red-500/15 text-red-300',
  medium: 'border-amber-500/30 bg-amber-500/15 text-amber-300',
  low: 'border-slate-500/30 bg-slate-500/15 text-slate-300',
}

export default function OpenIncidentsWidget({ data, linkable }: { data: OpenIncidentsData; linkable: boolean }) {
  if (data.incidents.length === 0) return <p className="text-emerald-300">No open incidents.</p>
  return (
    <div className="space-y-2">
      <ul className="divide-y divide-white/5">
        {data.incidents.map((inc, i) => (
          <li key={inc.incident_id ?? `${inc.subject_name}-${i}`} className="flex items-center gap-2 py-1.5">
            <span className={`shrink-0 rounded-full border px-2 py-0.5 text-xs ${SEVERITY[inc.severity] ?? SEVERITY.low}`}>{inc.severity}</span>
            {linkable && inc.incident_id ? (
              <Link to={`/incidents/${inc.incident_id}`} className="min-w-0 flex-1 truncate text-slate-200 hover:text-primary-400">
                {inc.subject_name}
              </Link>
            ) : (
              <span className="min-w-0 flex-1 truncate text-slate-200">{inc.subject_name}</span>
            )}
            {inc.site_name && <span className="hidden shrink-0 text-xs text-slate-500 sm:inline">{inc.site_name}</span>}
            <span className="shrink-0 tabular-nums text-xs text-slate-400">{formatDuration(inc.duration_seconds)}</span>
          </li>
        ))}
      </ul>
      {data.total > data.incidents.length && <p className="text-xs text-slate-500">and {data.total - data.incidents.length} more</p>}
    </div>
  )
}
```

- [ ] **Step 2: Write the settings forms**

`frontend/src/components/dashboards/settings/TopNSettings.tsx`:

```tsx
import { inputCls, num, str, strs, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'
import RangeSelect from '@/components/dashboards/pickers/RangeSelect'

export default function TopNSettings({ config, siteId, onChange }: SettingsProps) {
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const bySite = 'site_id' in config && config.site_id !== undefined
  return (
    <div className="space-y-3">
      <Field label="Ports of">
        <select className={inputCls} value={bySite ? 'site' : 'devices'} onChange={(e) => (e.target.value === 'site' ? set({ site_id: siteId, devices: [] }) : set({ site_id: undefined, devices: [] }))}>
          <option value="site">A site</option>
          <option value="devices">Chosen devices</option>
        </select>
      </Field>
      {bySite ? (
        <Field label="Site">
          <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
        </Field>
      ) : (
        <Field label="Devices">
          <DevicePicker siteId={siteId} value={strs(config, 'devices')} onChange={(ids) => set({ devices: ids })} />
        </Field>
      )}
      <Field label="Rank by">
        <select className={inputCls} value={str(config, 'measure') || 'traffic'} onChange={(e) => set({ measure: e.target.value })}>
          <option value="traffic">Traffic (average in + out)</option>
          <option value="utilisation">How busy (busier direction)</option>
          <option value="errors">Errors per minute</option>
        </select>
      </Field>
      <Field label="How many" hint="5 to 20.">
        <input className={inputCls} type="number" min={5} max={20} value={num(config, 'n', 10)} onChange={(e) => set({ n: Number(e.target.value) })} />
      </Field>
      <Field label="Time range">
        <RangeSelect value={str(config, 'range')} onChange={(r) => set({ range: r })} />
      </Field>
    </div>
  )
}
```

`frontend/src/components/dashboards/settings/EventLogSettings.tsx`:

```tsx
import { inputCls, num, str, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'

export default function EventLogSettings({ config, siteId, onChange }: SettingsProps) {
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const forDevice = 'device_id' in config && config.device_id !== undefined
  const device = str(config, 'device_id')
  return (
    <div className="space-y-3">
      <Field label="Events of">
        <select className={inputCls} value={forDevice ? 'device' : 'site'} onChange={(e) => (e.target.value === 'device' ? set({ device_id: '', site_id: undefined }) : set({ site_id: siteId, device_id: undefined }))}>
          <option value="site">A site</option>
          <option value="device">One device</option>
        </select>
      </Field>
      {forDevice ? (
        <Field label="Device">
          <DevicePicker single siteId={siteId} value={device ? [device] : []} onChange={(ids) => set({ device_id: ids[0] })} />
        </Field>
      ) : (
        <Field label="Site">
          <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
        </Field>
      )}
      <Field label="How many" hint="10 to 50.">
        <input className={inputCls} type="number" min={10} max={50} value={num(config, 'limit', 20)} onChange={(e) => set({ limit: Number(e.target.value) })} />
      </Field>
    </div>
  )
}
```

`frontend/src/components/dashboards/settings/DeviceTableSettings.tsx`:

```tsx
import { DEVICE_TYPE_LABEL, type DeviceType } from '@/hooks/useDevices'
import { str, strs, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'

const STATUSES = ['up', 'down', 'pending', 'paused', 'error']

export default function DeviceTableSettings({ config, onChange }: SettingsProps) {
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const toggle = (key: 'types' | 'statuses', v: string) => {
    const cur = strs(config, key)
    set({ [key]: cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v] })
  }
  return (
    <div className="space-y-3">
      <Field label="Site">
        <SitePicker value={str(config, 'site_id') || null} onChange={(id) => set({ site_id: id })} />
      </Field>
      <Field label="Only these types" hint="None ticked: every type.">
        <div className="flex flex-wrap gap-3">
          {(Object.keys(DEVICE_TYPE_LABEL) as DeviceType[]).map((t) => (
            <label key={t} className="flex items-center gap-1.5 text-sm text-slate-300">
              <input type="checkbox" checked={strs(config, 'types').includes(t)} onChange={() => toggle('types', t)} /> {DEVICE_TYPE_LABEL[t]}
            </label>
          ))}
        </div>
      </Field>
      <Field label="Only these statuses" hint="None ticked: every status.">
        <div className="flex flex-wrap gap-3">
          {STATUSES.map((s) => (
            <label key={s} className="flex items-center gap-1.5 text-sm capitalize text-slate-300">
              <input type="checkbox" checked={strs(config, 'statuses').includes(s)} onChange={() => toggle('statuses', s)} /> {s}
            </label>
          ))}
        </div>
      </Field>
    </div>
  )
}
```

`frontend/src/components/dashboards/settings/OpenIncidentsSettings.tsx`:

```tsx
import { inputCls, num, str, strs, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import SitePicker from '@/components/dashboards/pickers/SitePicker'
import DevicePicker from '@/components/dashboards/pickers/DevicePicker'
import MonitorPicker from '@/components/dashboards/pickers/MonitorPicker'

export default function OpenIncidentsSettings({ config, siteId, onChange }: SettingsProps) {
  const scope = str(config, 'scope') || 'all'
  const limit = num(config, 'limit', 20)
  const setScope = (s: string) => {
    if (s === 'site') onChange({ scope: s, site_id: siteId, limit })
    else if (s === 'devices') onChange({ scope: s, devices: [], limit })
    else if (s === 'monitors') onChange({ scope: s, monitors: [], limit })
    else onChange({ scope: 'all', limit })
  }
  return (
    <div className="space-y-3">
      <Field label="Incidents of" hint={scope === 'all' ? 'Everything the viewer can see. On a public link, everything — including what is added later.' : undefined}>
        <select className={inputCls} value={scope} onChange={(e) => setScope(e.target.value)}>
          <option value="all">Everything I can see</option>
          <option value="site">A site's devices</option>
          <option value="devices">Chosen devices</option>
          <option value="monitors">Chosen monitors</option>
        </select>
      </Field>
      {scope === 'site' && (
        <Field label="Site">
          <SitePicker value={str(config, 'site_id') || null} onChange={(id) => onChange({ ...config, site_id: id })} />
        </Field>
      )}
      {scope === 'devices' && (
        <Field label="Devices">
          <DevicePicker siteId={siteId} value={strs(config, 'devices')} onChange={(ids) => onChange({ ...config, devices: ids })} />
        </Field>
      )}
      {scope === 'monitors' && (
        <Field label="Monitors">
          <MonitorPicker value={strs(config, 'monitors')} onChange={(ids) => onChange({ ...config, monitors: ids })} />
        </Field>
      )}
      <Field label="Show at most" hint="5 to 50.">
        <input className={inputCls} type="number" min={5} max={50} value={limit} onChange={(e) => onChange({ ...config, limit: Number(e.target.value) })} />
      </Field>
    </div>
  )
}
```

- [ ] **Step 3: Register them**

`WidgetRenderer.tsx`:

```tsx
    case 'top_n':
      return <TopNWidget data={data as TopNData} linkable={linkable} />
    case 'event_log':
      return <EventLogWidget data={data as EventLogData} linkable={linkable} />
    case 'device_table':
      return <DeviceTableWidget data={data as DeviceTableData} linkable={linkable} />
    case 'open_incidents':
      return <OpenIncidentsWidget data={data as OpenIncidentsData} linkable={linkable} />
```

`WidgetSettings.tsx`:

```tsx
    case 'top_n':
      return <TopNSettings {...props} />
    case 'event_log':
      return <EventLogSettings {...props} />
    case 'device_table':
      return <DeviceTableSettings {...props} />
    case 'open_incidents':
      return <OpenIncidentsSettings {...props} />
```

- [ ] **Step 4: Run the gate and try it**

Run the frontend gate. A site dashboard created with the standard widgets now shows real Open incidents, Top ports and Devices; add an Event log.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/dashboards
git commit -m "feat(dashboards): top ports, event log, device table and open incidents in the UI

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 27: The monitors widget in the UI

**Files:**
- Create: `frontend/src/components/dashboards/widgets/MonitorsWidget.tsx`
- Create: `frontend/src/components/dashboards/settings/MonitorsSettings.tsx`
- Modify: `WidgetRenderer.tsx`, `WidgetSettings.tsx`

- [ ] **Step 1: Write the renderer**

`frontend/src/components/dashboards/widgets/MonitorsWidget.tsx`:

```tsx
import { Link } from 'react-router-dom'
import type { MonitorsData, UptimeBucket } from '@/types/dashboards'
import { timeAgo } from '@/utils/network'

const DOT: Record<string, string> = { online: 'bg-emerald-400', offline: 'bg-red-400', paused: 'bg-slate-500', unknown: 'bg-slate-500', pending: 'bg-slate-500' }
const BAR: Record<UptimeBucket['status'], string> = {
  up: 'bg-emerald-500/80',
  partial: 'bg-amber-400/80',
  down: 'bg-red-500/80',
  nodata: 'bg-slate-700',
}

function Bars({ buckets }: { buckets: UptimeBucket[] }) {
  return (
    <div className="flex h-4 flex-1 gap-px" role="img" aria-label="Uptime history">
      {buckets.map((b, i) => (
        <span key={`${b.label}-${i}`} className={`flex-1 rounded-sm ${BAR[b.status] ?? BAR.nodata}`} title={`${b.label}: ${b.status === 'nodata' ? 'no data' : `${b.uptime}%`}`} />
      ))}
    </div>
  )
}

export default function MonitorsWidget({ data, linkable }: { data: MonitorsData; linkable: boolean }) {
  return (
    <ul className="space-y-1.5">
      {data.monitors.map((m, i) => (
        <li key={m.monitor_id ?? `m-${i}`} className="flex items-center gap-2">
          <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${DOT[m.status] ?? DOT.unknown}`} aria-label={m.status} />
          {linkable && m.monitor_id ? (
            <Link to={`/monitors/${m.monitor_id}`} className="w-40 shrink-0 truncate text-slate-200 hover:text-primary-400">
              {m.name}
            </Link>
          ) : (
            <span className="w-40 shrink-0 truncate text-slate-200">{m.name}</span>
          )}
          {data.style === 'bars' && m.buckets ? (
            <Bars buckets={m.buckets} />
          ) : (
            <span className="flex-1 text-right text-xs text-slate-500">
              {m.status === 'paused' ? 'paused' : `${m.response_time_ms} ms · ${m.last_check_at ? timeAgo(m.last_check_at) : 'never checked'}`}
            </span>
          )}
        </li>
      ))}
      {data.agents.map((a, i) => (
        <li key={a.agent_id ?? `a-${i}`} className="flex items-center gap-2">
          <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${DOT[a.status] ?? DOT.unknown}`} aria-label={a.status} />
          <span className="w-40 shrink-0 truncate text-slate-200">{a.name}</span>
          <span className="flex-1 text-right text-xs text-slate-500">
            {a.status} · {a.last_heartbeat ? timeAgo(a.last_heartbeat) : 'never reported'}
          </span>
        </li>
      ))}
    </ul>
  )
}
```

Server agents are not linked: their page is `/servers/:agentID` with the agent's readable id, which this widget does not carry.

- [ ] **Step 2: Write the settings**

`frontend/src/components/dashboards/settings/MonitorsSettings.tsx`:

```tsx
import { useAuthContext } from '@/context/AuthContext'
import { inputCls, str, strs, type SettingsProps } from '@/utils/dashboards'
import Field from '@/components/dashboards/settings/Field'
import MonitorPicker from '@/components/dashboards/pickers/MonitorPicker'
import AgentPicker from '@/components/dashboards/pickers/AgentPicker'

export default function MonitorsSettings({ config, onChange }: SettingsProps) {
  const { currentUser } = useAuthContext()
  const set = (patch: Record<string, unknown>) => onChange({ ...config, ...patch })
  const style = str(config, 'style') || 'list'
  return (
    <div className="space-y-3">
      <Field label="Monitors" hint="Up to 50.">
        <MonitorPicker value={strs(config, 'monitors')} onChange={(ids) => set({ monitors: ids })} />
      </Field>
      {currentUser?.is_admin && (
        <Field label="Servers" hint="Server agents are visible to admins only.">
          <AgentPicker value={strs(config, 'agents')} onChange={(ids) => set({ agents: ids })} />
        </Field>
      )}
      <Field label="Show as">
        <select className={inputCls} value={style} onChange={(e) => set({ style: e.target.value })}>
          <option value="list">A status list</option>
          <option value="bars">Uptime bars</option>
        </select>
      </Field>
      {style === 'bars' && (
        <Field label="Bars cover">
          <select className={inputCls} value={str(config, 'window') || '24h'} onChange={(e) => set({ window: e.target.value })}>
            <option value="24h">The last 24 hours (one bar an hour)</option>
            <option value="90d">The last 90 days (one bar a day)</option>
          </select>
        </Field>
      )}
    </div>
  )
}
```

- [ ] **Step 3: Register it**

`WidgetRenderer.tsx`: `case 'monitors': return <MonitorsWidget data={data as MonitorsData} linkable={linkable} />`
`WidgetSettings.tsx`: `case 'monitors': return <MonitorsSettings {...props} />`

Every type in `WidgetType` now has a renderer and a form. Keep both `default` branches: they are the safety net for a type a newer server knows and this frontend does not.

- [ ] **Step 4: Run the gate and try it**

Run the frontend gate. Add a Monitors widget as a list and as 24-hour and 90-day bars.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/dashboards
git commit -m "feat(dashboards): monitors widget with status lists and uptime bars in the UI

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---
### Task 28: Sharing and public links in the UI

**Files:**
- Create: `frontend/src/components/dashboards/DashboardSharingPanel.tsx`
- Create: `frontend/src/components/dashboards/PublicLinkPanel.tsx`
- Modify: `frontend/src/pages/dashboards/DashboardPage.tsx` (Share and Public link buttons)

**Interfaces:**
- Consumes: `useDashboardActions().listShares|share|unshare|getPublicLink|createPublicLink|revokePublicLink`, `useUsers`, `useAuthContext`, `useDashboard().refetch`.
- Produces: `DashboardSharingPanel({ dashboard: Dashboard; onClose })`, `PublicLinkPanel({ dashboardId: string; onClose; onChanged })`.

- [ ] **Step 1: Write the sharing panel**

`frontend/src/components/dashboards/DashboardSharingPanel.tsx`:

```tsx
import { useEffect, useMemo, useState } from 'react'
import { Trash2, X } from 'lucide-react'
import { useUsers } from '@/hooks/useUsers'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { Dashboard, DashboardShare } from '@/types/dashboards'
import { inputCls } from '@/utils/dashboards'

/** Who a personal dashboard is shared with. A site dashboard follows its site's sharing instead. */
export default function DashboardSharingPanel({ dashboard, onClose }: { dashboard: Dashboard; onClose: () => void }) {
  const actions = useDashboardActions()
  const { users } = useUsers()
  const [shares, setShares] = useState<DashboardShare[] | null>(null)
  const [userId, setUserId] = useState('')
  const [permission, setPermission] = useState<'readonly' | 'editable'>('readonly')
  const [error, setError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)

  useEffect(() => {
    let cancelled = false
    actions
      .listShares(dashboard.id)
      .then((s) => {
        if (!cancelled) setShares(s)
      })
      .catch((err: ApiError) => {
        if (!cancelled) setError(err.message || 'Could not load the shares')
      })
    return () => {
      cancelled = true
    }
  }, [actions, dashboard.id, version])

  const available = useMemo(
    () => users.filter((u) => u.id !== dashboard.owner_id && !shares?.some((s) => s.user_id === u.id)),
    [users, shares, dashboard.owner_id],
  )

  const run = async (fn: () => Promise<void>) => {
    setError(null)
    try {
      await fn()
      setVersion((v) => v + 1)
    } catch (err) {
      setError((err as ApiError).message || 'That did not work')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card w-full max-w-lg space-y-4 p-6" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">Share “{dashboard.name}”</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p className="text-sm text-slate-400">People see only the widgets whose devices and monitors they can already see.</p>
        <div className="flex gap-2">
          <select className={inputCls} value={userId} onChange={(e) => setUserId(e.target.value)}>
            <option value="">Choose a person…</option>
            {available.map((u) => (
              <option key={u.id} value={u.id}>
                {u.username}
              </option>
            ))}
          </select>
          <select className={`${inputCls} w-36`} value={permission} onChange={(e) => setPermission(e.target.value as 'readonly' | 'editable')}>
            <option value="readonly">Can view</option>
            <option value="editable">Can edit</option>
          </select>
          <button className="btn-primary" disabled={!userId} onClick={() => void run(async () => { await actions.share(dashboard.id, userId, permission); setUserId('') })}>
            Share
          </button>
        </div>
        {error && <p className="text-sm text-red-400">{error}</p>}
        <ul className="divide-y divide-white/10">
          {(shares ?? []).map((s) => (
            <li key={s.user_id} className="flex items-center justify-between gap-2 py-2 text-sm">
              <span className="text-slate-200">{s.username}</span>
              <span className="flex items-center gap-3">
                <select
                  className="rounded-md border border-white/10 bg-slate-900/60 px-2 py-1 text-xs"
                  value={s.permission}
                  onChange={(e) => void run(() => actions.share(dashboard.id, s.user_id, e.target.value as 'readonly' | 'editable'))}
                >
                  <option value="readonly">Can view</option>
                  <option value="editable">Can edit</option>
                </select>
                <button className="text-slate-400 hover:text-red-400" aria-label={`Stop sharing with ${s.username}`} onClick={() => void run(() => actions.unshare(dashboard.id, s.user_id))}>
                  <Trash2 className="h-4 w-4" />
                </button>
              </span>
            </li>
          ))}
          {shares?.length === 0 && <li className="py-2 text-sm text-slate-500">Not shared with anyone yet.</li>}
        </ul>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Write the public link panel**

`frontend/src/components/dashboards/PublicLinkPanel.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { Copy, X } from 'lucide-react'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { PublicLinkInfo } from '@/types/dashboards'
import { inputCls, widgetInfo } from '@/utils/dashboards'

interface Props {
  dashboardId: string
  onClose: () => void
  /** The dashboard's published state changed: refetch it. */
  onChanged: () => void
}

/** Admin only: the dashboard's public link, for outsiders and wall displays. */
export default function PublicLinkPanel({ dashboardId, onClose, onChanged }: Props) {
  const actions = useDashboardActions()
  const [info, setInfo] = useState<PublicLinkInfo | null>(null)
  const [confirm, setConfirm] = useState<'regenerate' | 'revoke' | null>(null)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)

  useEffect(() => {
    let cancelled = false
    actions
      .getPublicLink(dashboardId)
      .then((i) => {
        if (!cancelled) setInfo(i)
      })
      .catch((err: ApiError) => {
        if (!cancelled) setError(err.message || 'Could not load the link')
      })
    return () => {
      cancelled = true
    }
  }, [actions, dashboardId, version])

  const url = info?.link ? `${window.location.origin}/public/dashboards/${info.link.token}` : null

  const run = async (fn: () => Promise<unknown>) => {
    setError(null)
    setConfirm(null)
    setCopied(false)
    try {
      await fn()
      setVersion((v) => v + 1)
      onChanged()
    } catch (err) {
      setError((err as ApiError).message || 'That did not work')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card w-full max-w-lg space-y-4 p-6" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">Public link</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p className="text-sm text-slate-400">
          Anyone with the link sees this dashboard without signing in, so a TV can show it for days. IP addresses, hostnames,
          monitor URLs, error details and notes are left out, and nothing on it links into Sentinel. While it has a link, only
          admins can change or delete the dashboard.
        </p>
        {info && info.broad_widgets.length > 0 && (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-200">
            These widgets show everything rather than chosen items, so the link will also show things added later:
            <ul className="mt-1 list-disc pl-5">
              {info.broad_widgets.map((w) => (
                <li key={w.id}>{w.title || widgetInfo(w.type).label}</li>
              ))}
            </ul>
          </div>
        )}
        {error && <p className="text-sm text-red-400">{error}</p>}
        {!info ? null : url ? (
          <>
            <div className="flex gap-2">
              <input className={inputCls} readOnly value={url} onFocus={(e) => e.target.select()} />
              <button
                className="btn-secondary flex items-center gap-1"
                onClick={() => void navigator.clipboard.writeText(url).then(() => setCopied(true))}
              >
                <Copy className="h-4 w-4" /> {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
            {confirm ? (
              <div className="flex flex-wrap items-center gap-2 text-sm">
                <span className="text-amber-300">
                  {confirm === 'revoke' ? 'Turn the link off? Screens using it go blank.' : 'Make a new link? The current one stops working at once.'}
                </span>
                <button className="btn-secondary" onClick={() => setConfirm(null)}>
                  Keep it
                </button>
                <button
                  className="btn bg-red-600 text-white hover:bg-red-700"
                  onClick={() => void run(() => (confirm === 'revoke' ? actions.revokePublicLink(dashboardId) : actions.createPublicLink(dashboardId)))}
                >
                  {confirm === 'revoke' ? 'Turn off' : 'Make a new link'}
                </button>
              </div>
            ) : (
              <div className="flex gap-2">
                <button className="btn-secondary" onClick={() => setConfirm('regenerate')}>
                  New link
                </button>
                <button className="btn-secondary text-red-400" onClick={() => setConfirm('revoke')}>
                  Turn off
                </button>
              </div>
            )}
          </>
        ) : (
          <button className="btn-primary" onClick={() => void run(() => actions.createPublicLink(dashboardId))}>
            Create a public link
          </button>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Add the buttons to the page**

In `DashboardPage.tsx`: import `useAuthContext`, `DashboardSharingPanel`, `PublicLinkPanel`, and `Globe2`, `Share2` from lucide; take `refetch` from `useDashboard(id)`; add state:

```tsx
  const { currentUser } = useAuthContext()
  const [sharing, setSharing] = useState(false)
  const [linking, setLinking] = useState(false)
```

In the header's button group (view mode), before the Edit link:

```tsx
          {dashboard.can_share && !dashboard.site_id && (
            <button className="btn-secondary flex items-center gap-2" onClick={() => setSharing(true)}>
              <Share2 className="h-4 w-4" /> Share
            </button>
          )}
          {currentUser?.is_admin && (
            <button className="btn-secondary flex items-center gap-2" onClick={() => setLinking(true)}>
              <Globe2 className="h-4 w-4" /> {dashboard.published ? 'Public link' : 'Publish'}
            </button>
          )}
```

and at the end of the view-mode JSX:

```tsx
      {sharing && <DashboardSharingPanel dashboard={dashboard} onClose={() => setSharing(false)} />}
      {linking && <PublicLinkPanel dashboardId={dashboard.id} onClose={() => setLinking(false)} onChanged={refetch} />}
```

All hooks (`useAuthContext`, the two `useState`s) go above the page's early returns.

- [ ] **Step 4: Run the gate and try it**

Run the frontend gate. As the owner of a personal dashboard, share it read-only with a member and check that member sees it under "Shared with me" without an Edit button. As an admin, create a public link, copy it, regenerate it (the old URL stops working), and turn it off.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/dashboards frontend/src/pages/dashboards/DashboardPage.tsx
git commit -m "feat(dashboards): sharing and admin public links in the UI

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 29: Fullscreen display mode and the public page

**Files:**
- Create: `frontend/src/hooks/useDisplayMode.ts`
- Create: `frontend/src/components/dashboards/DisplayShell.tsx`
- Create: `frontend/src/pages/dashboards/PublicDashboard.tsx`
- Modify: `frontend/src/components/dashboards/WidgetBody.tsx` (`onUpdated`)
- Modify: `frontend/src/pages/dashboards/DashboardPage.tsx` (Fullscreen)
- Modify: `frontend/src/App.tsx` (public route)

**Interfaces:**
- Produces: `useDisplayMode(enabled: boolean)` → `{ fullscreen, enter(), exit() }` (while enabled: a screen wake lock where supported, re-requested when the tab becomes visible, and a page reload every 6 hours); `DisplayShell({ title, lastUpdated, fullscreen, onFullscreen, onExit?, children })`; `WidgetBody` prop `onUpdated?: (t: number) => void`; route `/public/dashboards/:token`.

- [ ] **Step 1: Write the display hook**

`frontend/src/hooks/useDisplayMode.ts`:

```ts
import { useCallback, useEffect, useState } from 'react'

const RELOAD_EVERY_MS = 6 * 60 * 60 * 1000

interface WakeLockLike {
  release(): Promise<void>
}
type NavigatorWithWakeLock = Navigator & { wakeLock?: { request(type: 'screen'): Promise<WakeLockLike> } }

/** What an unattended screen needs. While enabled it keeps the screen awake
 *  (where the browser supports it) and reloads the page every 6 hours, to
 *  pick up new versions of Sentinel and free memory. */
export function useDisplayMode(enabled: boolean) {
  const [fullscreen, setFullscreen] = useState(() => document.fullscreenElement != null)

  useEffect(() => {
    const onChange = () => setFullscreen(document.fullscreenElement != null)
    document.addEventListener('fullscreenchange', onChange)
    return () => document.removeEventListener('fullscreenchange', onChange)
  }, [])

  useEffect(() => {
    if (!enabled) return
    let lock: WakeLockLike | null = null
    const request = async () => {
      try {
        lock = (await (navigator as NavigatorWithWakeLock).wakeLock?.request('screen')) ?? null
      } catch {
        lock = null // refused (battery saver, unsupported): the page still works
      }
    }
    void request()
    // A wake lock is dropped whenever the tab is hidden; take it again on return.
    const onVisible = () => {
      if (document.visibilityState === 'visible') void request()
    }
    document.addEventListener('visibilitychange', onVisible)
    const reload = window.setTimeout(() => window.location.reload(), RELOAD_EVERY_MS)
    return () => {
      document.removeEventListener('visibilitychange', onVisible)
      window.clearTimeout(reload)
      void lock?.release()
    }
  }, [enabled])

  const enter = useCallback(async () => {
    try {
      await document.documentElement.requestFullscreen()
    } catch {
      // The browser refused (no user gesture, or a kiosk policy): the display
      // shell still covers the app, just not the browser's own chrome.
    }
  }, [])

  const exit = useCallback(async () => {
    if (document.fullscreenElement) await document.exitFullscreen()
  }, [])

  return { fullscreen, enter, exit }
}
```

- [ ] **Step 2: Write the display shell**

`frontend/src/components/dashboards/DisplayShell.tsx`:

```tsx
import { useEffect, useState, type ReactNode } from 'react'
import { Maximize2, X } from 'lucide-react'

interface Props {
  title: string
  /** When the newest widget data arrived (epoch ms). */
  lastUpdated: number | null
  fullscreen: boolean
  onFullscreen: () => void
  onExit?: () => void
  children: ReactNode
}

function clock(t: number): string {
  return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

/** The wall-display frame: covers the app's chrome, shows the dashboard's
 *  name, the time and when data last arrived. */
export default function DisplayShell({ title, lastUpdated, fullscreen, onFullscreen, onExit, children }: Props) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 30000)
    return () => window.clearInterval(t)
  }, [])
  return (
    <div className="fixed inset-0 z-50 overflow-y-auto bg-slate-950 p-4">
      <header className="mb-4 flex items-center gap-4">
        <h1 className="min-w-0 flex-1 truncate text-3xl font-light text-white">{title}</h1>
        <span className="tabular-nums text-slate-400">
          {lastUpdated ? `Updated ${clock(lastUpdated)}` : 'Loading…'} · {clock(now)}
        </span>
        {!fullscreen && (
          <button className="btn-secondary flex items-center gap-2" onClick={onFullscreen}>
            <Maximize2 className="h-4 w-4" /> Fullscreen
          </button>
        )}
        {onExit && (
          <button className="btn-secondary flex items-center gap-2" onClick={onExit} aria-label="Leave the wall display">
            <X className="h-4 w-4" /> Exit
          </button>
        )}
      </header>
      {children}
    </div>
  )
}
```

- [ ] **Step 3: Report updates from each widget**

In `WidgetBody.tsx`, add `onUpdated?: (t: number) => void` to the props, take `lastSuccess` from `useWidgetData`, and add (with `useEffect` imported from react):

```tsx
  useEffect(() => {
    if (lastSuccess != null) onUpdated?.(lastSuccess)
  }, [lastSuccess, onUpdated])
```

- [ ] **Step 4: Add Fullscreen to the dashboard page**

In `DashboardPage.tsx`: import `useCallback`, `useDisplayMode`, `DisplayShell` and `Maximize2`; add above the early returns:

```tsx
  const [display, setDisplay] = useState(false)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)
  const { fullscreen, enter, exit } = useDisplayMode(display)
  const onUpdated = useCallback((t: number) => setLastUpdated((prev) => (prev != null && prev > t ? prev : t)), [])
```

add a header button in view mode:

```tsx
          <button
            className="btn-secondary flex items-center gap-2"
            title="For a screen that stays up for days, use a public link: a signed-in session ends after 24 hours"
            onClick={() => {
              setDisplay(true)
              void enter()
            }}
          >
            <Maximize2 className="h-4 w-4" /> Fullscreen
          </button>
```

and, just before the view-mode `return`, the display branch:

```tsx
  if (display) {
    return (
      <DisplayShell
        title={dashboard.name}
        lastUpdated={lastUpdated}
        fullscreen={fullscreen}
        onFullscreen={() => void enter()}
        onExit={() => {
          void exit()
          setDisplay(false)
        }}
      >
        <DashboardGrid
          items={dashboard.widgets}
          editing={false}
          render={(w) => (
            <WidgetBody widget={w} display onUpdated={onUpdated} linkable source={{ kind: 'dashboard', dashboardId: dashboard.id, widgetId: w.id, override }} />
          )}
        />
      </DisplayShell>
    )
  }
```

- [ ] **Step 5: Write the public page**

`frontend/src/pages/dashboards/PublicDashboard.tsx`:

```tsx
import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import axios from 'axios'
import { Loader2 } from 'lucide-react'
import type { ApiResponse } from '@/types'
import type { PublicDashboard as PublicLayout } from '@/types/dashboards'
import { useDisplayMode } from '@/hooks/useDisplayMode'
import DisplayShell from '@/components/dashboards/DisplayShell'
import DashboardGrid from '@/components/dashboards/DashboardGrid'
import WidgetBody from '@/components/dashboards/WidgetBody'

const LAYOUT_EVERY_MS = 5 * 60 * 1000

/** A dashboard's public link: no login, straight into display mode. The
 *  layout is fetched again every 5 minutes so an admin's edits reach the TV. */
export default function PublicDashboardPage() {
  const { token } = useParams()
  const [layout, setLayout] = useState<PublicLayout | null>(null)
  const [gone, setGone] = useState(false)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)
  const { fullscreen, enter } = useDisplayMode(true)
  const onUpdated = useCallback((t: number) => setLastUpdated((prev) => (prev != null && prev > t ? prev : t)), [])

  useEffect(() => {
    if (!token) return
    let cancelled = false
    const load = () =>
      axios
        .get<ApiResponse<PublicLayout>>(`/api/v1/public/dashboards/${token}`)
        .then((res) => {
          if (cancelled) return
          setLayout(res.data.data)
          setGone(false)
        })
        .catch((err: unknown) => {
          // A 404 is final (revoked, regenerated, or its admin was demoted);
          // anything else is a blip, and the next attempt may succeed.
          if (!cancelled && axios.isAxiosError(err) && err.response?.status === 404) setGone(true)
        })
    void load()
    const t = window.setInterval(() => void load(), LAYOUT_EVERY_MS)
    return () => {
      cancelled = true
      window.clearInterval(t)
    }
  }, [token])

  if (gone) {
    return <div className="flex min-h-screen items-center justify-center bg-slate-950 p-8 text-center text-2xl font-light text-slate-300">This dashboard link is no longer available.</div>
  }
  if (!layout || !token) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-950">
        <Loader2 className="h-8 w-8 animate-spin text-slate-500" />
      </div>
    )
  }
  return (
    <DisplayShell title={layout.name} lastUpdated={lastUpdated} fullscreen={fullscreen} onFullscreen={() => void enter()}>
      <DashboardGrid
        items={layout.widgets}
        editing={false}
        render={(w) => <WidgetBody widget={w} display onUpdated={onUpdated} linkable={false} source={{ kind: 'public', token, widgetId: w.id }} />}
      />
    </DisplayShell>
  )
}
```

In `App.tsx`, add the lazy page and, next to `/public/status/:slug` (outside `RequireAuth`), the route:

```ts
const PublicDashboardPage = lazy(() => import('@/pages/dashboards/PublicDashboard'))
```

```tsx
            <Route path="/public/dashboards/:token" element={<PublicDashboardPage />} />
```

- [ ] **Step 6: Run the gate and try it**

Run the frontend gate. Then:
- Open a dashboard → Fullscreen: the app's menus disappear, the time and "Updated" show, Exit returns.
- Open a public link in a private window: it loads without a login, shows no links, and goes fullscreen from its button.
- Stop the backend (`docker compose stop backend` on the sandbox) for longer than three refresh periods: widgets keep their last data and show **Stale**; start it again and the markers clear without a reload.
- Revoke the link: within a refresh the page reads "This dashboard link is no longer available."

- [ ] **Step 7: Commit**

```bash
git add frontend/src/hooks/useDisplayMode.ts frontend/src/components/dashboards frontend/src/pages/dashboards frontend/src/App.tsx
git commit -m "feat(dashboards): fullscreen wall display and the public link page

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 30: Dashboards on the site page

**Files:**
- Modify: `frontend/src/pages/network/SiteDetail.tsx`

**Interfaces:**
- Consumes: `useDashboards(siteId)`, `NewDashboardModal({ siteId })`, the page's existing `canEdit`, `isAdmin`, `confirmDelete`.

- [ ] **Step 1: List the site's dashboards and offer a new one**

In `SiteDetail.tsx`: import `useDashboards` from `@/hooks/useDashboards`, `NewDashboardModal` from `@/components/dashboards/NewDashboardModal`, and `LayoutDashboard` from lucide; with the other hooks (above the early returns) add:

```tsx
  const { dashboards } = useDashboards(id)
  const [newDashboard, setNewDashboard] = useState(false)
```

Add this section directly after the header block (before the Devices section):

```tsx
      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-lg font-light text-white">Dashboards</h2>
          {canEdit && (
            <button className="btn-secondary flex items-center gap-2" onClick={() => setNewDashboard(true)}>
              <Plus className="h-4 w-4" /> New site dashboard
            </button>
          )}
        </div>
        {dashboards.length === 0 ? (
          <p className="text-sm text-slate-500">No dashboards for this site yet.</p>
        ) : (
          <ul className="flex flex-wrap gap-2">
            {dashboards.map((d) => (
              <li key={d.id}>
                <Link to={`/dashboards/${d.id}`} className="card inline-flex items-center gap-2 px-3 py-2 text-sm hover:bg-white/5">
                  <LayoutDashboard className="h-4 w-4 text-teal-400" /> {d.name}
                  {d.published && <span className="text-xs text-teal-300">public</span>}
                </Link>
              </li>
            ))}
          </ul>
        )}
      </section>
```

and with the other modals at the end:

```tsx
      {newDashboard && (
        <NewDashboardModal siteId={site.id} onClose={() => setNewDashboard(false)} onCreated={(d) => navigate(`/dashboards/${d.id}/edit`)} />
      )}
```

- [ ] **Step 2: Say what deleting the site takes with it**

In the delete confirmation (the `confirmDelete ? (...)` branch), add before the Cancel button:

```tsx
                {dashboards.length > 0 && (
                  <span className="self-center text-sm text-amber-300">
                    Its {dashboards.length} dashboard{dashboards.length === 1 ? '' : 's'}
                    {dashboards.some((d) => d.published) && ` (${dashboards.filter((d) => d.published).length} with a public link)`} will be deleted too.
                  </span>
                )}
```

Only admins can delete sites, and admins see every site dashboard, so the count is complete.

- [ ] **Step 3: Run the gate and try it**

Run the frontend gate. On a site page: the Dashboards section lists the site's dashboards; "New site dashboard" opens the dialog with the site chosen and the standard widgets ticked; Delete shows the count.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/network/SiteDetail.tsx
git commit -m "feat(dashboards): site dashboards on the site page and in the delete warning

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 31: Docs — status and follow-ups

**Files:**
- Modify: `docs/superpowers/STATUS.md`
- Create: `docs/superpowers/plans/2026-10-02-network-phase4-followups.md`

- [ ] **Step 1: Update `STATUS.md`**

In the roadmap table, set the phase 4 row to:

```markdown
| 4 | Custom dashboards: grid editor, 11 widget types (incl. site power), sharing, fullscreen wall display, admin public links | Done (`dev`, YYYY-MM-DD — the merge date); follow-ups in `plans/2026-10-02-network-phase4-followups.md` |
```

and mark the next phase as **Next** — ask the owner which of 5 (metric reports) and 7 (UniFi) comes first; phase 6 (maps) is now unblocked too. Replace the "To be checked by the owner on real equipment" list with phase 4's checks, keeping any phase 3 checks the owner has not confirmed yet:

```markdown
Phase 4 (after updating the work install from `dev`):
1. Create a site dashboard with the standard widgets; every widget shows data within a minute.
2. Leave it fullscreen on a screen for a day; it is still updating the next morning (no Stale markers).
3. As an admin, create a public link, open it in a private window, then turn the link off; the page says the link is no longer available.
4. Stop the backend for a few minutes; the widgets show Stale, then recover on their own.
```

- [ ] **Step 2: Write the follow-ups doc**

`docs/superpowers/plans/2026-10-02-network-phase4-followups.md`, in the shape of the phase 3 one: **Rulings** (the six deviations at the top of this plan, plus every decision taken while executing), **Fixed in the final whole-branch review**, **Known limitations**, **Deferred follow-ups**. Known limitations to record at least:

- In-app navigation away from the editor is not blocked (the app uses `<BrowserRouter>`; `useBlocker` needs a data router). Cancel confirms in-page and reload/close warns.
- A logged-in fullscreen display drops off when its 24-hour session expires; the public link is the way to run a TV for days.
- A time series with lines in different units draws one axis in the first line's unit.
- `useDashboards` on the Overview loads every visible dashboard just to count them.

Then add every finding from the final review that was deliberately left for later, one line each, tagged with its task number like the phase 3 doc.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/STATUS.md docs/superpowers/plans/2026-10-02-network-phase4-followups.md
git commit -m "docs(network): record phase 4 status, rulings and follow-ups

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 4: Hand over**

Run the full checks one last time — `db_up; go_docker vet ./... && go_docker test ./...; db_down` from `backend/`, and the frontend gate — and report the output. Merging `feature/network-phase4` into `dev`, pushing, and updating the work install are the owner's call (CLAUDE.md: push and merge only when asked); use the finishing-a-development-branch skill to present the options.
