# UX Reorganization, Piece 2: One Monitoring List — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One Monitoring page at `/monitoring` listing uptime checks, servers and network devices in a section per type, with shared filters in the address and one "+ Add" menu; monitors and server agents gain an optional site.

**Architecture:** Backend: migration 060 adds a nullable `site_id` to `monitors` and `agents`; the monitor and agent handlers accept it (left out = unchanged, `null` = clear), check the caller may use that site, and label responses with `site_name`. Frontend: a pure view model (`utils/monitoringView.ts`) owns the filters ↔ address mapping, the shared status scale and per-section filtering; `pages/Monitoring.tsx` loads the three lists with their existing hooks and renders three section components that reuse today's tables. The old list pages are deleted at the end and their addresses redirect.

**Tech Stack:** Go 1.26, Gin, GORM, PostgreSQL 16 + TimescaleDB; React 18 + TypeScript + Vite + Tailwind + react-router-dom v6.

**Spec:** `docs/superpowers/specs/2026-10-07-ux-piece2-monitoring-list-design.md`

## Global Constraints

- Work on `feature/ux-piece2` in a worktree cut from `dev`.
- Migration file is `backend/migrations/060_monitor_agent_sites.sql`. Confirm first: `ls backend/migrations | tail -1` must print `059_network_tools.sql`; if not, use the next free number everywhere this plan says 060.
- No CHECK constraints on the new columns (a CHECK tying two columns breaks `ON DELETE SET NULL`).
- Handlers answer 400 with `respondError` (an `error` string). The refusal text for a site the caller may not use, or one that does not exist, is exactly `site not found`.
- Site on edit: **left out means unchanged, an explicit `null` clears it.** An unchanged site is always accepted, even when the editor cannot see it.
- A site is a label: it never changes who may see or edit a monitor or server.
- Frontend rules: colours from `src/utils/colors.ts` or the existing slate/Tailwind classes (no new hand-written hex, no `dark:` variants); component files export only components (react-refresh lint), helpers go in `src/utils/` or `src/hooks/`; no `window.confirm`/`alert`; guard async results so a late response cannot overwrite a newer one.
- Status scale (spec): monitor `!enabled` → paused, else in maintenance → maintenance, else online → up, offline → down, else pending. Server active → up, offline → down, pending → pending. Device status as is (`up`, `down`, `pending`, `paused`, `error`). Error and Pending are never Down.
- Address parameters exactly: `show` (`uptime`|`servers`|`devices`), `q`, `status` (`down`|`up`|`pending`|`paused`|`maintenance`|`error`), `site` (id or `none`), `group` (id or `ungrouped`), `type` (`http`|`dns`|`ping`|`tcp`|`webhook`), `tags` (comma-separated), `device_type` (`switch`|`router`|`access_point`|`nvr`|`ups`|`other`). Unknown values are ignored.
- Backend commands run from `backend/`: `go vet ./...`, `go test ./...`, and `./scripts/test-db.sh -run <Name> -v` for database tests (needs Docker). Without Go on the host, run the same `go` commands in `golang:1.26-alpine` with the backend mounted.
- Frontend gate (from the repo root): `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`. Never run npm as root.
- Throwaway frontend checks: a `check.ts` in a temp directory outside the repo (`CHECK=$(mktemp -d)`), run from the repo root with `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -v "$CHECK":/check -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx esbuild /check/check.ts --bundle --platform=node --outfile=/tmp/check.cjs --log-level=warning && node /tmp/check.cjs"`. The check imports modules by absolute path (`/app/src/utils/...`), so modules under check use only `import type` from `@/...` and relative paths (`./devices`) for value imports.
- Commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Editing a shared monitor whose site is not shared with the editor** must still save: the edit form always sends `site_id`, and an unchanged site must not be refused. Pinned by Task 2's `TestDBMonitorEditKeepsASiteTheEditorCannotSee`.
2. **A client that does not know about sites** (an old script, or any edit that omits the field) must never wipe a site, for monitors (`PUT`) or agents (`PATCH`). Pinned by Task 2's and Task 3's "kept" steps.
3. **More than 500 monitors** must all load, and a wrong page count from the server must not loop forever. Pinned by Task 5's `fetchAllPages` checks.
4. **Old links keep their filter**: `/uptime?type=http` lands on Monitoring with `show=uptime&type=http`; an unknown `status=` or `show=` value is ignored rather than emptying the page. Pinned by Task 5's `legacyMonitoringTarget` and `parseMonitoringParams` checks.
5. **Bulk upload cannot set a site** around the permission check (it builds monitors from raw JSON). Pinned by Task 2's `TestDBBulkCreateIgnoresSite`.

## File structure

Backend:
- Create `backend/migrations/060_monitor_agent_sites.sql`.
- Modify `backend/internal/models/monitor.go` (`SiteID`, `SiteName`, `SiteIDSet`), `backend/internal/models/agent.go` (`SiteID`, `SiteName`).
- Modify `backend/internal/services/site_service.go` (`NamesByID`), `backend/internal/services/monitor_service.go` (`applyMonitorUpdates`), `backend/internal/services/agent_service.go` (`AgentSettings.SetSite`/`SiteID`, `Update`).
- Create `backend/internal/api/site_labels.go` (`siteLabels`, `siteField`, `parseSiteField`, `requireAssignableSite`, `siteNameOf`, `labelMonitorSites`, `labelAgentSites`).
- Modify `backend/internal/api/monitor_handler.go`, `backend/internal/api/monitor_create_handler.go`, `backend/internal/api/agent_handler.go`, `backend/cmd/sentinel/main.go`.
- Tests: `backend/internal/services/monitor_agent_site_db_test.go`, `backend/internal/api/site_labels_test.go`, `backend/internal/api/monitor_site_db_test.go`, `backend/internal/api/agent_site_db_test.go`.

Frontend:
- Create `frontend/src/utils/siteChoices.ts`, `frontend/src/components/SiteSelect.tsx`.
- Create `frontend/src/utils/monitoringView.ts`, `frontend/src/utils/monitorPages.ts`.
- Create hooks `frontend/src/hooks/useAllMonitors.ts`, `useDebounced.ts`, `useDismissOnOutsideClick.ts`, `useRememberedToggles.ts`.
- Create `frontend/src/components/monitoring/{AddMenu,MonitoringToolbar,SummaryStrip,SectionShell,EmptyLine,GroupModal,UptimeSection,ServerRow,ServersSection,DevicesSection}.tsx`, `frontend/src/pages/Monitoring.tsx`, `frontend/src/components/MonitoringRedirect.tsx`.
- Modify `frontend/src/types/api.ts`, `frontend/src/hooks/useAgents.ts`, `frontend/src/components/{MonitorForm,CreateMonitorModal,MonitorTable,AgentSettingsFields,AddServerAgentModal,EditServerAgentModal}.tsx`, `frontend/src/pages/{MonitorWizard,MonitorDetail,ServerDetail,Overview,BulkUpload,NetworkDiscovery}.tsx`, `frontend/src/pages/network/{DeviceDetail,PortDetail}.tsx`, `frontend/src/utils/navigation.ts`, `frontend/src/App.tsx`.
- Delete `frontend/src/pages/UptimeMonitoring.tsx`, `frontend/src/pages/Monitors.tsx`, `frontend/src/pages/ServerMonitoring.tsx`, `frontend/src/pages/network/Devices.tsx`.
- Modify `docs/superpowers/STATUS.md`.

## Plan rulings

1. **An unchanged site is always accepted on edit.** The spec says a site may be set "to a site they can see"; keeping the site a monitor already has is not setting one, and refusing it would break editing any shared monitor whose site is not shared with the editor (the form always sends `site_id`).
2. **`site_name` appears on create, update, list and single-get responses** for monitors and agents. The agent status endpoint (`/agents/:id/status`) is left as it is.
3. **A failed site-name lookup is logged and leaves names empty** rather than failing the list: the name is a label, the rows are the content.
4. **Bulk create drops any `site_id`** in its rows (spec: bulk upload does not set a site).
5. **Typing in search replaces the current history entry**; every other filter change adds one, so Back steps through filter changes without stepping through keystrokes.
6. **Tags stay an any-of filter** (as today), shown as chips at the top of the Uptime section.
7. **Group collapse keeps its existing browser key** (`sentinel:uptimeCollapsedGroups`), so groups already collapsed stay collapsed; sections use a new key, `sentinel:monitoringCollapsed`.
8. **The old pages stay until Task 7**: Task 6 copies the pieces it needs (group modal, server row), and Task 7 deletes the old pages, so each task leaves a working app.
9. **The Group select appears only once a monitor group exists**: with no groups it could only offer "Ungrouped", which is every monitor.

---

### Task 1: Sites on monitors and agents (database and models)

**Files:**
- Create: `backend/migrations/060_monitor_agent_sites.sql`
- Modify: `backend/internal/models/monitor.go` (Monitor struct, after `GroupID`), `backend/internal/models/agent.go` (Agent struct, before `CreatedAt`), `backend/internal/services/site_service.go` (new method after `Get`)
- Test: `backend/internal/services/monitor_agent_site_db_test.go`

**Interfaces:**
- Produces: `models.Monitor.SiteID *uuid.UUID` (json `site_id`), `models.Monitor.SiteName *string` (json `site_name`, not stored), `models.Monitor.SiteIDSet bool` (json `-`, not stored); `models.Agent.SiteID *uuid.UUID`, `models.Agent.SiteName *string`; `func (s *SiteService) NamesByID(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)`.

- [ ] **Step 1: Write the failing test**

`backend/internal/services/monitor_agent_site_db_test.go`:

```go
package services

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// Deleting a site leaves its monitors and agents in place, with no site.
func TestDBMonitorAndAgentSitesClearWhenTheSiteGoes(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	site := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Annex')`, site)

	mon, err := NewMonitorService(db).CreateMonitor(ctx, &models.Monitor{
		Name: "core", Type: models.MonitorTypePing, URL: "10.0.0.1",
		IntervalSeconds: 60, TimeoutSeconds: 5, FailureThreshold: 2, SiteID: &site,
	})
	testdb.Must(t, err)
	agent := &models.Agent{Name: "fs-01", OSType: "linux", CheckInterval: 60, RetryAttempts: 3, SiteID: &site}
	testdb.Must(t, NewAgentService(db).Register(ctx, agent))

	var withSite int64
	testdb.Must(t, db.Raw(`SELECT
		(SELECT count(*) FROM monitors WHERE id = ? AND site_id = ?) +
		(SELECT count(*) FROM agents WHERE id = ? AND site_id = ?)`, mon.ID, site, agent.ID, site).Scan(&withSite).Error)
	if withSite != 2 {
		t.Fatalf("stored with a site: %d of 2", withSite)
	}

	if _, err := NewSiteService(db).Delete(ctx, site); err != nil {
		t.Fatalf("deleting the site: %v", err)
	}

	var cleared int64
	testdb.Must(t, db.Raw(`SELECT
		(SELECT count(*) FROM monitors WHERE id = ? AND site_id IS NULL) +
		(SELECT count(*) FROM agents WHERE id = ? AND site_id IS NULL)`, mon.ID, agent.ID).Scan(&cleared).Error)
	if cleared != 2 {
		t.Errorf("after the site was deleted, %d of 2 rows remain with no site", cleared)
	}
}

func TestDBSiteNamesByID(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Annex'), (?, 'Courthouse')`, a, b)

	names, err := NewSiteService(db).NamesByID(ctx, []uuid.UUID{a, b, uuid.New()})
	testdb.Must(t, err)
	if len(names) != 2 || names[a] != "Annex" || names[b] != "Courthouse" {
		t.Errorf("names = %v", names)
	}

	none, err := NewSiteService(db).NamesByID(ctx, nil)
	testdb.Must(t, err)
	if len(none) != 0 {
		t.Errorf("no ids gave %v", none)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `./scripts/test-db.sh -run 'TestDBMonitorAndAgentSitesClearWhenTheSiteGoes|TestDBSiteNamesByID' -v`
Expected: build failure — `unknown field SiteID in struct literal of type models.Monitor` and `NewSiteService(db).NamesByID undefined`.

- [ ] **Step 3: Write the migration**

`backend/migrations/060_monitor_agent_sites.sql`:

```sql
-- 060_monitor_agent_sites.sql
-- UX piece 2 (spec 2026-10-07-ux-piece2-monitoring-list-design.md): uptime
-- monitors and server agents can carry a site, so the Monitoring list's site
-- filter covers them as it does devices. Optional, and a label only: it does
-- not change who can see anything. Deleting a site leaves its monitors and
-- agents in place with no site.

ALTER TABLE monitors ADD COLUMN IF NOT EXISTS site_id UUID REFERENCES sites(id) ON DELETE SET NULL;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS site_id UUID REFERENCES sites(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_monitors_site_id ON monitors (site_id);
CREATE INDEX IF NOT EXISTS idx_agents_site_id ON agents (site_id);
```

- [ ] **Step 4: Add the model fields**

In `backend/internal/models/monitor.go`, directly after the `GroupID` field:

```go
	// SiteID labels the monitor with a site, for filtering the Monitoring
	// list. Optional, and not a permission: who can see the monitor is decided
	// by ownership and sharing alone.
	SiteID *uuid.UUID `json:"site_id" gorm:"column:site_id;type:uuid"`
	// SiteName is the site's name, filled in by the API for responses. Not
	// stored.
	SiteName *string `json:"site_name" gorm:"-"`
	// SiteIDSet says an update carried site_id at all. A nil SiteID cannot
	// tell "left out" (keep the site) from "null" (clear it), so the handler
	// records which it was here. Never read from or written to JSON.
	SiteIDSet bool `json:"-" gorm:"-"`
```

In `backend/internal/models/agent.go`, directly before `CreatedAt time.Time`:

```go
	// SiteID labels the server with a site, for filtering the Monitoring
	// list. Optional, and not a permission.
	SiteID *uuid.UUID `json:"site_id" gorm:"column:site_id;type:uuid"`
	// SiteName is the site's name, filled in by the API for responses. Not
	// stored.
	SiteName *string `json:"site_name" gorm:"-"`
```

- [ ] **Step 5: Add `NamesByID`**

In `backend/internal/services/site_service.go`, after `Get`:

```go
// NamesByID returns the names of the given sites, keyed by id. Ids with no
// site are left out. It labels monitors and agents with their site, so it
// does not check access: anyone who can see a monitor sees its site's name.
func (s *SiteService) NamesByID(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	names := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	var rows []struct {
		ID   uuid.UUID
		Name string
	}
	if err := s.db.WithContext(ctx).Model(&models.Site{}).
		Select("id, name").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading site names: %w", err)
	}
	for _, r := range rows {
		names[r.ID] = r.Name
	}
	return names, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `./scripts/test-db.sh -run 'TestDBMonitorAndAgentSitesClearWhenTheSiteGoes|TestDBSiteNamesByID' -v`
Expected: both PASS. Then `go vet ./... && go test ./...` — PASS (no other code reads the new fields yet).

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/060_monitor_agent_sites.sql backend/internal/models/monitor.go backend/internal/models/agent.go backend/internal/services/site_service.go backend/internal/services/monitor_agent_site_db_test.go
git commit -m "feat(sites): optional site on monitors and server agents

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Monitor API takes and shows a site

**Files:**
- Create: `backend/internal/api/site_labels.go`, `backend/internal/api/site_labels_test.go`
- Modify: `backend/internal/api/monitor_handler.go` (imports; `CreateMonitorHandler`, `GetMonitorsHandler`, `GetMonitorHandler`, `UpdateMonitorHandler`, `RegisterMonitorRoutes`), `backend/internal/api/monitor_create_handler.go` (`BulkCreateMonitorsHandler` loop), `backend/internal/services/monitor_service.go` (`applyMonitorUpdates`), `backend/cmd/sentinel/main.go` (the `RegisterMonitorRoutes` call)
- Test: `backend/internal/api/monitor_site_db_test.go`

**Interfaces:**
- Consumes: Task 1's `Monitor.SiteID`, `Monitor.SiteName`, `Monitor.SiteIDSet`, `SiteService.NamesByID`; existing `SiteService.SiteAccess(ctx, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error)` and `services.ErrSiteNotFound`.
- Produces (Task 3 uses all of these): `type siteLabels interface { SiteAccess(...); NamesByID(...) }`; `type siteField struct { Set bool; ID *uuid.UUID }`; `func parseSiteField(raw json.RawMessage) (siteField, error)`; `func requireAssignableSite(c *gin.Context, sites siteLabels, siteID uuid.UUID) bool`; `func siteNameOf(names map[uuid.UUID]string, siteID *uuid.UUID) *string`; `func labelAgentSites(ctx context.Context, sites siteLabels, agents []*models.Agent)`; test helpers `siteTestGroup(t, db, caller) (*gin.Engine, *gin.RouterGroup, *services.AuthService)` and `dataOf[T any](t, w) T`; `RegisterMonitorRoutes(rg, monitorService, checkService, settingsService, sites siteLabels)`.

- [ ] **Step 1: Write the failing unit test for `parseSiteField`**

`backend/internal/api/site_labels_test.go`:

```go
package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// site_id in a request body has three meanings: left out (keep), null
// (clear) and an id (set). The test goes through a real decode, because what
// json.RawMessage receives for each of them is the point.
func TestParseSiteField(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		name    string
		body    string
		wantSet bool
		wantID  *uuid.UUID
		wantErr bool
	}{
		{"left out", `{"name":"x"}`, false, nil, false},
		{"null", `{"site_id":null}`, true, nil, false},
		{"an id", `{"site_id":"` + id.String() + `"}`, true, &id, false},
		{"not an id", `{"site_id":"annex"}`, false, nil, true},
		{"a number", `{"site_id":7}`, false, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body struct {
				SiteID json.RawMessage `json:"site_id"`
			}
			if err := json.Unmarshal([]byte(c.body), &body); err != nil {
				t.Fatal(err)
			}
			got, err := parseSiteField(body.SiteID)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want error %v", err, c.wantErr)
			}
			if got.Set != c.wantSet {
				t.Errorf("Set = %v, want %v", got.Set, c.wantSet)
			}
			if (got.ID == nil) != (c.wantID == nil) || (got.ID != nil && *got.ID != *c.wantID) {
				t.Errorf("ID = %v, want %v", got.ID, c.wantID)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/api -run TestParseSiteField`
Expected: build failure — `undefined: parseSiteField`.

- [ ] **Step 3: Write `site_labels.go`**

`backend/internal/api/site_labels.go`:

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// siteLabels is what the monitor and agent handlers need from sites: whether
// the caller may label something with a site, and the names to show.
type siteLabels interface {
	SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error)
	NamesByID(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// siteField is an optional site_id read from a request body. Set is false
// when the field was left out (keep the current site); Set with a nil ID is
// an explicit null (clear it).
type siteField struct {
	Set bool
	ID  *uuid.UUID
}

// parseSiteField reads site_id as captured by a json.RawMessage, which is nil
// when the field was left out and the literal null when it was sent as null.
func parseSiteField(raw json.RawMessage) (siteField, error) {
	if raw == nil {
		return siteField{}, nil
	}
	if string(bytes.TrimSpace(raw)) == "null" {
		return siteField{Set: true}, nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return siteField{}, errors.New("site_id must be a site id or null")
	}
	return siteField{Set: true, ID: &id}, nil
}

// requireAssignableSite answers 400 "site not found" unless the caller may
// label something with siteID: an admin any site that exists, anyone else a
// site shared with them. A missing site and someone else's get the same
// answer, so the response does not reveal which sites exist.
func requireAssignableSite(c *gin.Context, sites siteLabels, siteID uuid.UUID) bool {
	userID, _, isAdmin, _ := GetUserFromContext(c)
	level, err := sites.SiteAccess(c.Request.Context(), userID, isAdmin, siteID)
	if errors.Is(err, services.ErrSiteNotFound) || (err == nil && level == services.SiteAccessNone) {
		respondError(c, http.StatusBadRequest, "site not found")
		return false
	}
	if err != nil {
		respondInternal(c, "site access", err)
		return false
	}
	return true
}

// siteNameOf is the name to show for siteID, or nil.
func siteNameOf(names map[uuid.UUID]string, siteID *uuid.UUID) *string {
	if siteID == nil {
		return nil
	}
	if name, ok := names[*siteID]; ok {
		return &name
	}
	return nil
}

// siteNames looks up the names for ids. A failure is logged and gives no
// names: a site name is a label, and the list it labels is still worth
// showing without it.
func siteNames(ctx context.Context, sites siteLabels, ids []uuid.UUID) map[uuid.UUID]string {
	if len(ids) == 0 {
		return nil
	}
	names, err := sites.NamesByID(ctx, ids)
	if err != nil {
		log.Printf("[sites] labelling with site names: %v", err)
		return nil
	}
	return names
}

// labelMonitorSites fills SiteName on each monitor.
func labelMonitorSites(ctx context.Context, sites siteLabels, monitors []*models.Monitor) {
	ids := make([]uuid.UUID, 0, len(monitors))
	for _, m := range monitors {
		if m.SiteID != nil {
			ids = append(ids, *m.SiteID)
		}
	}
	names := siteNames(ctx, sites, ids)
	for _, m := range monitors {
		m.SiteName = siteNameOf(names, m.SiteID)
	}
}

// labelAgentSites fills SiteName on each agent.
func labelAgentSites(ctx context.Context, sites siteLabels, agents []*models.Agent) {
	ids := make([]uuid.UUID, 0, len(agents))
	for _, a := range agents {
		if a.SiteID != nil {
			ids = append(ids, *a.SiteID)
		}
	}
	names := siteNames(ctx, sites, ids)
	for _, a := range agents {
		a.SiteName = siteNameOf(names, a.SiteID)
	}
}
```

- [ ] **Step 4: Run the unit test to verify it passes**

Run: `go test ./internal/api -run TestParseSiteField -v`
Expected: PASS (5 subtests).

- [ ] **Step 5: Write the failing database tests**

`backend/internal/api/monitor_site_db_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// siteTestGroup is a router whose /api/v1 group acts as caller (read from the
// database), as the auth middleware would.
func siteTestGroup(t *testing.T, db *gorm.DB, caller uuid.UUID) (*gin.Engine, *gin.RouterGroup, *services.AuthService) {
	t.Helper()
	auth := services.NewAuthService(db, testJWTSecret)
	user, err := auth.GetUserByID(context.Background(), caller)
	testdb.Must(t, err)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", user.ID)
		c.Set("username", user.Username)
		c.Set("is_admin", user.IsAdmin)
		c.Next()
	})
	return r, v1, auth
}

// dataOf decodes the "data" member of a success response.
func dataOf[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var body struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding %s: %v", w.Body.String(), err)
	}
	return body.Data
}

func monitorSiteRouter(t *testing.T, db *gorm.DB, caller uuid.UUID) *gin.Engine {
	t.Helper()
	r, v1, _ := siteTestGroup(t, db, caller)
	monitors := services.NewMonitorService(db)
	checks := services.NewCheckService(db)
	RegisterMonitorRoutes(v1, monitors, checks, services.NewSettingsService(db), services.NewSiteService(db))
	RegisterMonitorCreationRoutes(v1, monitors, checks)
	return r
}

type monitorSiteView struct {
	ID       string  `json:"id"`
	SiteID   *string `json:"site_id"`
	SiteName *string `json:"site_name"`
}

func newSite(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, ?)`, id, name)
	return id
}

func pingMonitorBody(extra string) string {
	return `{"name":"core","type":"ping","url":"10.0.0.1","interval_seconds":60,"timeout_seconds":5` + extra + `}`
}

// An admin sets a site on create, an edit that leaves the field out keeps
// it, and an explicit null clears it. Every response carries the site name.
func TestDBMonitorSiteSetKeptAndCleared(t *testing.T) {
	db := testdb.Open(t)
	site := newSite(t, db, "Annex")
	r := monitorSiteRouter(t, db, testdb.NewUser(t, db, true))

	w := toolRequest(r, http.MethodPost, "/api/v1/monitors", pingMonitorBody(`,"site_id":"`+site.String()+`"`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := dataOf[monitorSiteView](t, w)
	if created.SiteID == nil || *created.SiteID != site.String() || created.SiteName == nil || *created.SiteName != "Annex" {
		t.Fatalf("created = %+v", created)
	}

	w = toolRequest(r, http.MethodPut, "/api/v1/monitors/"+created.ID, `{"name":"core-renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	w = toolRequest(r, http.MethodGet, "/api/v1/monitors/"+created.ID, "")
	if got := dataOf[monitorSiteView](t, w); got.SiteID == nil || *got.SiteID != site.String() || got.SiteName == nil || *got.SiteName != "Annex" {
		t.Errorf("after an edit without site_id: %+v, want the site kept", got)
	}

	w = toolRequest(r, http.MethodPut, "/api/v1/monitors/"+created.ID, `{"site_id":null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	if got := dataOf[monitorSiteView](t, w); got.SiteID != nil || got.SiteName != nil {
		t.Errorf("after site_id null: %+v, want no site", got)
	}
}

// A non-admin may only use a site shared with them; a missing site gets the
// same refusal.
func TestDBMonitorSiteRefusedWhenNotShared(t *testing.T) {
	db := testdb.Open(t)
	shared, other := newSite(t, db, "Annex"), newSite(t, db, "Courthouse")
	user := testdb.NewUser(t, db, false)
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, shared, user)
	r := monitorSiteRouter(t, db, user)

	for _, id := range []uuid.UUID{other, uuid.New()} {
		w := toolRequest(r, http.MethodPost, "/api/v1/monitors", pingMonitorBody(`,"site_id":"`+id.String()+`"`))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "site not found") {
			t.Errorf("site %s: %d %s, want 400 site not found", id, w.Code, w.Body.String())
		}
	}
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM monitors`).Scan(&n).Error)
	if n != 0 {
		t.Fatalf("%d monitors created by refused requests", n)
	}

	w := toolRequest(r, http.MethodPost, "/api/v1/monitors", pingMonitorBody(`,"site_id":"`+shared.String()+`"`))
	if w.Code != http.StatusCreated {
		t.Fatalf("shared site: %d %s", w.Code, w.Body.String())
	}
	created := dataOf[monitorSiteView](t, w)
	w = toolRequest(r, http.MethodPut, "/api/v1/monitors/"+created.ID, `{"site_id":"`+other.String()+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "site not found") {
		t.Errorf("edit to an unshared site: %d %s, want 400 site not found", w.Code, w.Body.String())
	}
}

// The edit form always sends site_id. A monitor labelled with a site the
// editor cannot see must still save when that site is sent back unchanged.
func TestDBMonitorEditKeepsASiteTheEditorCannotSee(t *testing.T) {
	db := testdb.Open(t)
	hidden := newSite(t, db, "Courthouse")
	user := testdb.NewUser(t, db, false)
	r := monitorSiteRouter(t, db, user)

	w := toolRequest(r, http.MethodPost, "/api/v1/monitors", pingMonitorBody(""))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := dataOf[monitorSiteView](t, w).ID
	testdb.Exec(t, db, `UPDATE monitors SET site_id = ? WHERE id = ?`, hidden, id)

	w = toolRequest(r, http.MethodPut, "/api/v1/monitors/"+id, `{"name":"core-renamed","site_id":"`+hidden.String()+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("saving with the unchanged site: %d %s", w.Code, w.Body.String())
	}
	if got := dataOf[monitorSiteView](t, w); got.SiteID == nil || *got.SiteID != hidden.String() {
		t.Errorf("site after save = %v, want it kept", got.SiteID)
	}
}

// The list carries each monitor's site name.
func TestDBMonitorListCarriesSiteName(t *testing.T) {
	db := testdb.Open(t)
	site := newSite(t, db, "Annex")
	r := monitorSiteRouter(t, db, testdb.NewUser(t, db, true))
	toolRequest(r, http.MethodPost, "/api/v1/monitors", pingMonitorBody(`,"site_id":"`+site.String()+`"`))
	toolRequest(r, http.MethodPost, "/api/v1/monitors", `{"name":"edge","type":"ping","url":"10.0.0.2","interval_seconds":60,"timeout_seconds":5}`)

	w := toolRequest(r, http.MethodGet, "/api/v1/monitors", "")
	list := dataOf[struct {
		Monitors []monitorSiteView `json:"monitors"`
	}](t, w)
	if len(list.Monitors) != 2 {
		t.Fatalf("listed %d monitors, want 2", len(list.Monitors))
	}
	for _, m := range list.Monitors {
		labelled := m.SiteName != nil && *m.SiteName == "Annex"
		if (m.SiteID != nil) != labelled {
			t.Errorf("monitor %+v: site id and name disagree", m)
		}
	}
}

// Bulk upload never sets a site, so it cannot be used to skip the check.
func TestDBBulkCreateIgnoresSite(t *testing.T) {
	db := testdb.Open(t)
	site := newSite(t, db, "Annex")
	r := monitorSiteRouter(t, db, testdb.NewUser(t, db, false))

	w := toolRequest(r, http.MethodPost, "/api/v1/monitors/bulk",
		`{"monitors":[{"name":"core","type":"ping","url":"10.0.0.1","interval_seconds":60,"timeout_seconds":5,"site_id":"`+site.String()+`"}]}`)
	if w.Code >= 300 {
		t.Fatalf("bulk: %d %s", w.Code, w.Body.String())
	}
	var total, labelled int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM monitors`).Scan(&total).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM monitors WHERE site_id IS NOT NULL`).Scan(&labelled).Error)
	if total != 1 || labelled != 0 {
		t.Errorf("monitors = %d, with a site = %d; want 1 and 0", total, labelled)
	}
}
```

- [ ] **Step 6: Run them to verify they fail**

Run: `./scripts/test-db.sh -run 'TestDBMonitorSite|TestDBMonitorEditKeeps|TestDBMonitorListCarries|TestDBBulkCreateIgnoresSite' -v`
Expected: build failure — `too many arguments in call to RegisterMonitorRoutes`.

- [ ] **Step 7: Apply the site on update**

In `backend/internal/services/monitor_service.go`, in `applyMonitorUpdates`, after the `NotifyChannels` block:

```go
	// Applied only when the request carried site_id: nil then means "clear",
	// where a request that left it out keeps the site it has.
	if updates.SiteIDSet {
		target.SiteID = updates.SiteID
	}
```

- [ ] **Step 8: Wire the monitor handlers**

In `backend/internal/api/monitor_handler.go`:

1. Add imports `"encoding/json"`, `"github.com/gin-gonic/gin/binding"`.
2. `RegisterMonitorRoutes` gains a last parameter `sites siteLabels`, and its route lines become:

```go
	monitors.POST("", CreateMonitorHandler(monitorService, settingsService, sites))
	monitors.GET("", GetMonitorsHandler(monitorService, sites))
	monitors.GET("/:id", GetMonitorHandler(monitorService, sites))
	monitors.PUT("/:id", UpdateMonitorHandler(monitorService, sites))
```

3. `CreateMonitorHandler(monitorService *services.MonitorService, settingsService *services.SettingsService, sites siteLabels)`: after the `monitor.Validate()` check and before setting `OwnerID`, add

```go
		// A site the caller cannot see is refused, the same as one that does
		// not exist.
		if monitor.SiteID != nil && !requireAssignableSite(c, sites, *monitor.SiteID) {
			return
		}
```

and just before `respondSuccess(c, http.StatusCreated, created)` add `labelMonitorSites(c.Request.Context(), sites, []*models.Monitor{created})`.

4. `GetMonitorsHandler(monitorService *services.MonitorService, sites siteLabels)`: after `pageItems` is cut and before `now := time.Now()`, add

```go
		labels := make([]*models.Monitor, len(pageItems))
		for i := range pageItems {
			labels[i] = &pageItems[i]
		}
		labelMonitorSites(c.Request.Context(), sites, labels)
```

5. `GetMonitorHandler(monitorService *services.MonitorService, sites siteLabels)`: after `monitor` is loaded without error, add `labelMonitorSites(c.Request.Context(), sites, []*models.Monitor{monitor})`.

6. `UpdateMonitorHandler(monitorService *services.MonitorService, sites siteLabels)`: replace the bind block and everything after it with

```go
		// Bound twice: once into the monitor, and once to see whether
		// site_id was sent at all, which the monitor's nil SiteID cannot say.
		var updates models.Monitor
		if err := c.ShouldBindBodyWith(&updates, binding.JSON); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		var raw struct {
			SiteID json.RawMessage `json:"site_id"`
		}
		if err := c.ShouldBindBodyWith(&raw, binding.JSON); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		site, err := parseSiteField(raw.SiteID)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		if site.Set && site.ID != nil {
			current, err := monitorService.GetMonitor(c.Request.Context(), id)
			if err != nil {
				respondError(c, classifyServiceError(err), err.Error())
				return
			}
			// Keeping the site a monitor already has is not choosing one: an
			// editor who cannot see that site must still be able to save.
			unchanged := current.SiteID != nil && *current.SiteID == *site.ID
			if !unchanged && !requireAssignableSite(c, sites, *site.ID) {
				return
			}
		}
		updates.SiteIDSet = site.Set
		updates.SiteID = site.ID

		updated, err := monitorService.UpdateMonitor(c.Request.Context(), id, &updates)
		if err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		labelMonitorSites(c.Request.Context(), sites, []*models.Monitor{updated})

		log.Printf("Monitor updated: %s (ID: %s)", updated.Name, updated.ID)
		respondSuccess(c, http.StatusOK, updated)
```

In `backend/internal/api/monitor_create_handler.go`, inside `BulkCreateMonitorsHandler`'s loop, directly after `monitor := req.Monitors[i]`:

```go
			// Bulk upload does not set a site (it sets no group either), and
			// it must not be a way around the site check either.
			monitor.SiteID = nil
```

In `backend/cmd/sentinel/main.go`: `api.RegisterMonitorRoutes(v1, monitorService, checkService, settingsService, siteService)`. `siteService` is declared earlier in `main` (line ~147); if it is declared after this call, move the call below it.

- [ ] **Step 9: Run the tests to verify they pass**

Run: `./scripts/test-db.sh -run 'TestDBMonitorSite|TestDBMonitorEditKeeps|TestDBMonitorListCarries|TestDBBulkCreateIgnoresSite' -v`
Expected: all five PASS. Then `go vet ./... && go test ./...` — PASS.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/api/site_labels.go backend/internal/api/site_labels_test.go backend/internal/api/monitor_site_db_test.go backend/internal/api/monitor_handler.go backend/internal/api/monitor_create_handler.go backend/internal/services/monitor_service.go backend/cmd/sentinel/main.go
git commit -m "feat(sites): monitors take a site and show its name

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Agent API takes and shows a site

**Files:**
- Modify: `backend/internal/api/agent_handler.go` (`createAgentRequest`, `CreateAgentHandler`, `ListAgentsHandler`, `GetAgentHandler`, `updateAgentRequest`, `UpdateAgentHandler`, `RegisterAgentRoutes`), `backend/internal/services/agent_service.go` (`AgentSettings`, `Update`), `backend/cmd/sentinel/main.go` (the `RegisterAgentRoutes` call)
- Test: `backend/internal/api/agent_site_db_test.go`

**Interfaces:**
- Consumes: Task 1's `Agent.SiteID`, `Agent.SiteName`; Task 2's `siteLabels`, `parseSiteField`, `requireAssignableSite`, `labelAgentSites`, `siteTestGroup`, `dataOf`, `newSite`.
- Produces: `RegisterAgentRoutes(rg, agents, settings, users adminChecker, sites siteLabels)`; `services.AgentSettings.SetSite bool`, `services.AgentSettings.SiteID *uuid.UUID`.

- [ ] **Step 1: Write the failing database tests**

`backend/internal/api/agent_site_db_test.go`:

```go
package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func agentSiteRouter(t *testing.T, db *gorm.DB, caller uuid.UUID) *gin.Engine {
	t.Helper()
	r, v1, auth := siteTestGroup(t, db, caller)
	RegisterAgentRoutes(v1, services.NewAgentService(db), services.NewSettingsService(db), auth, services.NewSiteService(db))
	return r
}

type agentSiteView struct {
	AgentID  string  `json:"agent_id"`
	SiteID   *string `json:"site_id"`
	SiteName *string `json:"site_name"`
}

// Set on create, kept by a PATCH that leaves site_id out, cleared by null;
// the list carries the site name.
func TestDBAgentSiteSetKeptAndCleared(t *testing.T) {
	db := testdb.Open(t)
	site := newSite(t, db, "Annex")
	r := agentSiteRouter(t, db, testdb.NewUser(t, db, true))

	w := toolRequest(r, http.MethodPost, "/api/v1/agents", `{"name":"fs-01","os_type":"linux","site_id":"`+site.String()+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := dataOf[struct {
		Agent agentSiteView `json:"agent"`
	}](t, w).Agent
	if created.SiteID == nil || *created.SiteID != site.String() || created.SiteName == nil || *created.SiteName != "Annex" {
		t.Fatalf("created = %+v", created)
	}

	w = toolRequest(r, http.MethodPatch, "/api/v1/agents/"+created.AgentID, `{"name":"fs-02"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	list := dataOf[[]agentSiteView](t, toolRequest(r, http.MethodGet, "/api/v1/agents", ""))
	if len(list) != 1 || list[0].SiteID == nil || *list[0].SiteID != site.String() || list[0].SiteName == nil || *list[0].SiteName != "Annex" {
		t.Errorf("after a PATCH without site_id: %+v, want the site kept and named", list)
	}

	w = toolRequest(r, http.MethodPatch, "/api/v1/agents/"+created.AgentID, `{"site_id":null}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	if got := dataOf[agentSiteView](t, w); got.SiteID != nil || got.SiteName != nil {
		t.Errorf("after site_id null: %+v, want no site", got)
	}
}

// A site that does not exist is refused on create and on edit.
func TestDBAgentUnknownSiteRefused(t *testing.T) {
	db := testdb.Open(t)
	r := agentSiteRouter(t, db, testdb.NewUser(t, db, true))
	missing := uuid.New().String()

	w := toolRequest(r, http.MethodPost, "/api/v1/agents", `{"name":"fs-01","os_type":"linux","site_id":"`+missing+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "site not found") {
		t.Fatalf("create: %d %s, want 400 site not found", w.Code, w.Body.String())
	}

	w = toolRequest(r, http.MethodPost, "/api/v1/agents", `{"name":"fs-01","os_type":"linux"}`)
	created := dataOf[struct {
		Agent agentSiteView `json:"agent"`
	}](t, w).Agent
	w = toolRequest(r, http.MethodPatch, "/api/v1/agents/"+created.AgentID, `{"site_id":"`+missing+`"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "site not found") {
		t.Errorf("edit: %d %s, want 400 site not found", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `./scripts/test-db.sh -run 'TestDBAgentSite|TestDBAgentUnknownSite' -v`
Expected: build failure — `too many arguments in call to RegisterAgentRoutes`.

- [ ] **Step 3: Carry the site through the agent service**

In `backend/internal/services/agent_service.go`, add to `AgentSettings` after `IPOverride`:

```go
	// SetSite says the update carries a site at all; SiteID nil with SetSite
	// clears it. Without SetSite the site is left as it is.
	SetSite bool
	SiteID  *uuid.UUID
```

and in `Update`, after the `NotifyChannels` block:

```go
	if settings.SetSite {
		if settings.SiteID == nil {
			updates["site_id"] = nil
		} else {
			updates["site_id"] = *settings.SiteID
		}
	}
```

- [ ] **Step 4: Wire the agent handlers**

In `backend/internal/api/agent_handler.go`:

1. Add `"encoding/json"` and `"github.com/google/uuid"` to the imports if they are not there.
2. `createAgentRequest` gains, after `IPAddressOverride`:

```go
	// SiteID labels the server with a site. Omitted means no site.
	SiteID *uuid.UUID `json:"site_id"`
```

3. `CreateAgentHandler(agents *services.AgentService, settings *services.SettingsService, sites siteLabels)`: after the threshold validation, add

```go
		if req.SiteID != nil && !requireAssignableSite(c, sites, *req.SiteID) {
			return
		}
```

set `SiteID: req.SiteID,` in the `&models.Agent{...}` literal, and just before the `respondSuccess(c, http.StatusCreated, ...)` call add `labelAgentSites(c.Request.Context(), sites, []*models.Agent{agent})`.

4. `ListAgentsHandler(agents *services.AgentService, sites siteLabels)`: replace the `HideToken` loop with

```go
		labels := make([]*models.Agent, len(list))
		for i := range list {
			list[i].HideToken()
			labels[i] = &list[i]
		}
		labelAgentSites(c.Request.Context(), sites, labels)
```

5. `GetAgentHandler(agents *services.AgentService, settings *services.SettingsService, sites siteLabels)`: after `agent` is loaded without error, add `labelAgentSites(c.Request.Context(), sites, []*models.Agent{agent})`.

6. `updateAgentRequest` gains, after `IPAddressOverride`:

```go
	// SiteID left out keeps the site, null clears it, an id sets it. Raw so
	// the handler can tell the first two apart.
	SiteID json.RawMessage `json:"site_id"`
```

7. `UpdateAgentHandler(agents *services.AgentService, sites siteLabels)`: after the threshold validation, add

```go
		site, err := parseSiteField(req.SiteID)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		unchanged := site.ID != nil && current.SiteID != nil && *current.SiteID == *site.ID
		if site.ID != nil && !unchanged && !requireAssignableSite(c, sites, *site.ID) {
			return
		}
```

set `SetSite: site.Set, SiteID: site.ID,` in the `services.AgentSettings{...}` literal, and after `updated.HideToken()` add `labelAgentSites(c.Request.Context(), sites, []*models.Agent{updated})`. (If `err` is already declared in that scope, use `=` instead of `:=` for `site, err`.)

8. `RegisterAgentRoutes(rg *gin.RouterGroup, agents *services.AgentService, settings *services.SettingsService, users adminChecker, sites siteLabels)`, passing `sites` to `ListAgentsHandler`, `CreateAgentHandler`, `GetAgentHandler` and `UpdateAgentHandler`.

In `backend/cmd/sentinel/main.go`: `api.RegisterAgentRoutes(v1, agentService, settingsService, authService, siteService)`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `./scripts/test-db.sh -run 'TestDBAgentSite|TestDBAgentUnknownSite' -v`
Expected: both PASS. Then `go vet ./... && go test ./...` and the full `./scripts/test-db.sh` — PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/agent_handler.go backend/internal/api/agent_site_db_test.go backend/internal/services/agent_service.go backend/cmd/sentinel/main.go
git commit -m "feat(sites): server agents take a site and show its name

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Site fields in the monitor and server forms

**Files:**
- Create: `frontend/src/utils/siteChoices.ts`, `frontend/src/components/SiteSelect.tsx`
- Modify: `frontend/src/types/api.ts` (`Monitor`, `MonitorInput`), `frontend/src/hooks/useAgents.ts` (`Agent`, `CreateAgentInput`), `frontend/src/components/MonitorForm.tsx`, `frontend/src/components/CreateMonitorModal.tsx`, `frontend/src/pages/MonitorWizard.tsx`, `frontend/src/pages/MonitorDetail.tsx` (the edit-mode `<MonitorForm>`), `frontend/src/components/MonitorTable.tsx`, `frontend/src/components/AgentSettingsFields.tsx`, `frontend/src/components/AddServerAgentModal.tsx`, `frontend/src/components/EditServerAgentModal.tsx`
- Test: throwaway `check.ts` + the frontend gate

**Interfaces:**
- Consumes: Tasks 2–3's API fields `site_id` / `site_name`.
- Produces: `Monitor.site_id?: string | null`, `Monitor.site_name?: string | null`, `MonitorInput.site_id?: string | null`, `Agent.site_id: string | null`, `Agent.site_name?: string | null`, `CreateAgentInput.site_id?: string | null`; `siteChoices(sites: SiteChoice[], currentId: string, currentName: string | null): SiteChoice[]`; `<SiteSelect id? value onChange currentName? className? style? />`.

- [ ] **Step 1: Write the failing check**

`$CHECK/check.ts`:

```ts
import { siteChoices } from '/app/src/utils/siteChoices.ts'

let failures = 0
function expect(name: string, got: unknown, want: unknown) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures++
    console.log(`FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`)
  }
}

const sites = [
  { id: 'b', name: 'Courthouse' },
  { id: 'a', name: 'Annex' },
]
expect('sorted', siteChoices(sites, '', null).map((s) => s.name), ['Annex', 'Courthouse'])
expect('current among them', siteChoices(sites, 'a', 'Annex').length, 2)
expect('current not shared', siteChoices(sites, 'z', 'Warehouse').map((s) => s.name), ['Annex', 'Courthouse', 'Warehouse'])
expect('current unnamed', siteChoices(sites, 'z', null).at(-1), { id: 'z', name: 'Current site' })
expect('input untouched', sites[0].name, 'Courthouse')

if (failures) {
  console.log(`${failures} check(s) failed`)
  process.exit(1)
}
console.log('siteChoices: all checks passed')
```

- [ ] **Step 2: Run it to verify it fails**

Run the throwaway check command (Global Constraints) with this `$CHECK`.
Expected: esbuild error — `Could not resolve "/app/src/utils/siteChoices.ts"`.

- [ ] **Step 3: Write `siteChoices.ts` and `SiteSelect.tsx`**

`frontend/src/utils/siteChoices.ts`:

```ts
// What a Site select offers. Free of `@/` imports so it can be checked alone.

export interface SiteChoice {
  id: string
  name: string
}

/** siteChoices is the user's sites A-Z, plus the current one when it is not
 *  among them (a monitor shared by someone whose site is not shared with
 *  you), so opening a form never silently drops a site. */
export function siteChoices(sites: SiteChoice[], currentId: string, currentName: string | null): SiteChoice[] {
  const list = [...sites].sort((a, b) => a.name.localeCompare(b.name))
  if (currentId && !list.some((s) => s.id === currentId)) {
    list.push({ id: currentId, name: currentName ?? 'Current site' })
  }
  return list
}
```

`frontend/src/components/SiteSelect.tsx`:

```tsx
import type { CSSProperties } from 'react'
import { useSites } from '@/hooks/useSites'
import { siteChoices } from '@/utils/siteChoices'

interface Props {
  id?: string
  /** A site id, or '' for no site. */
  value: string
  onChange: (siteId: string) => void
  /** The current site's name, offered when it is not among the user's sites. */
  currentName?: string | null
  className?: string
  style?: CSSProperties
}

/** A Site picker: "No site", then the sites this user can see. */
export default function SiteSelect({ id, value, onChange, currentName = null, className, style }: Props) {
  const { sites } = useSites()
  const choices = siteChoices(sites, value, currentName)
  return (
    <select id={id} className={className} style={style} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">No site</option>
      {choices.map((s) => (
        <option key={s.id} value={s.id}>
          {s.name}
        </option>
      ))}
    </select>
  )
}
```

- [ ] **Step 4: Run the check to verify it passes**

Run the throwaway check command.
Expected: `siteChoices: all checks passed`.

- [ ] **Step 5: Add the types**

In `frontend/src/types/api.ts`, in `Monitor` after `group_id`:

```ts
  /** The site this monitor is labelled with. A label only: it does not
   *  change who can see the monitor. */
  site_id?: string | null
  /** Read-only: the site's name, on list and detail responses. */
  site_name?: string | null
```

and in `MonitorInput` after `notify_channels`:

```ts
  /** null clears the site; left out keeps it. */
  site_id?: string | null
```

In `frontend/src/hooks/useAgents.ts`, in `Agent` after `tools_local`:

```ts
  /** The site this server is labelled with, or null. */
  site_id: string | null
  /** Read-only: the site's name, on list and single-agent responses. */
  site_name?: string | null
```

and in `CreateAgentInput` after `ip_address_override`:

```ts
  /** null clears the site (on edit); left out keeps it. */
  site_id?: string | null
```

- [ ] **Step 6: Monitor forms**

`frontend/src/components/MonitorForm.tsx`:
- `MonitorFormValues` gains `/** A site id, or '' for no site. */ site_id: string`; `emptyMonitorForm` gains `site_id: ''`; `monitorToForm` gains `site_id: m.site_id ?? '',`.
- In `monitorFormToInput`, before `return input`:

```ts
  // Always sent: null is how an edit clears the site.
  input.site_id = v.site_id || null
```

- `Props` gains `/** The current site's name, for a site the user cannot see. */ currentSiteName?: string | null`, destructured in `MonitorForm({ ..., currentSiteName })`.
- Import `SiteSelect from '@/components/SiteSelect'` and add, directly after the Tags `<Field>`:

```tsx
        <Field label="Site" help="Where this service is, for filtering the Monitoring list. It doesn't change who can see the monitor.">
          <SiteSelect
            value={values.site_id}
            onChange={(v) => set('site_id', v)}
            currentName={currentSiteName ?? null}
            className={inputCls}
          />
        </Field>
```

`frontend/src/pages/MonitorDetail.tsx`: the edit-mode `<MonitorForm initialValues={monitorToForm(monitor)} ...>` gains `currentSiteName={monitor.site_name ?? null}`.

`frontend/src/pages/MonitorWizard.tsx`: import `SiteSelect`; directly after the Group `<label>` block, add

```tsx
              <label className="block">
                <span className="vs-eyebrow mb-1 block">Site (optional)</span>
                <SiteSelect value={values.site_id} onChange={(v) => set('site_id', v)} className={inputCls} style={inputStyle} />
              </label>
```

`frontend/src/components/CreateMonitorModal.tsx`:
- `FormState` gains `/** A site id, or '' for no site. */ siteId: string`; the `blank` object gains `siteId: '',`.
- The `create({...})` payload gains `site_id: form.siteId || null,`.
- Import `SiteSelect`; directly after the Description `<div>` (the `cm-desc` field), add

```tsx
              <div>
                <label htmlFor="cm-site" className="mb-1 block text-sm font-medium text-white">
                  Site (Optional)
                </label>
                <SiteSelect
                  id="cm-site"
                  value={form.siteId}
                  onChange={(v) => set('siteId', v)}
                  className={`${field} cursor-pointer appearance-none`}
                />
                <p className="mt-1 text-xs text-slate-500">Where this service is, for filtering the Monitoring list</p>
              </div>
```

`frontend/src/components/MonitorTable.tsx`: add a header cell after "Service Type": `<th className="px-4 py-3 text-left text-xs font-medium text-slate-400">Site</th>`, and in `MonitorRow` a cell after the type cell: `<td className="px-4 py-3 text-sm text-slate-400">{monitor.site_name ?? '—'}</td>`.

- [ ] **Step 7: Server forms**

`frontend/src/components/AgentSettingsFields.tsx`:
- `AgentSettings` gains `/** A site id, or '' for no site. */ siteId: string`.
- Import `SiteSelect`; directly after the IP Address `<div>` block, add

```tsx
          <div>
            <label htmlFor="agent-site" className="mb-1 block text-sm font-medium text-white">
              Site
            </label>
            <SiteSelect
              id="agent-site"
              value={values.siteId}
              onChange={(v) => set('siteId', v)}
              className={`${field} cursor-pointer appearance-none`}
            />
            <p className="mt-1 text-xs text-slate-500">Where this host is, for filtering the Monitoring list (optional)</p>
          </div>
```

`frontend/src/components/AddServerAgentModal.tsx`: `BLANK` gains `siteId: '',`; the `create({...})` payload gains `site_id: values.siteId || null,`.

`frontend/src/components/EditServerAgentModal.tsx`: `settingsOf` gains `siteId: agent.site_id ?? '',`; `settingsChanged` gains `values.siteId !== initial.siteId ||` as its first line; the `update(agent.agent_id, {...})` payload gains `site_id: values.siteId || null,`.

- [ ] **Step 8: Run the frontend gate**

Run the frontend gate (Global Constraints).
Expected: no type, lint or build errors. If `tsc` names another `AgentSettings` literal, give it `siteId: ''`.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/utils/siteChoices.ts frontend/src/components/SiteSelect.tsx frontend/src/types/api.ts frontend/src/hooks/useAgents.ts frontend/src/components/MonitorForm.tsx frontend/src/components/CreateMonitorModal.tsx frontend/src/pages/MonitorWizard.tsx frontend/src/pages/MonitorDetail.tsx frontend/src/components/MonitorTable.tsx frontend/src/components/AgentSettingsFields.tsx frontend/src/components/AddServerAgentModal.tsx frontend/src/components/EditServerAgentModal.tsx
git commit -m "feat(sites): choose a site for monitors and servers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: The Monitoring view model

**Files:**
- Create: `frontend/src/utils/monitoringView.ts`, `frontend/src/utils/monitorPages.ts`
- Test: throwaway `check.ts` + the frontend gate

**Interfaces:**
- Consumes: Task 4's `Monitor.site_id`, `Agent.site_id`; existing `deviceType`, `vendorModel` from `utils/devices.ts`.
- Produces (Tasks 6–7 use these exact names): `type Section = 'uptime' | 'servers' | 'devices'`; `SECTIONS`; `SECTION_LABEL`; `SECTION_CHIP`; `type ViewStatus`; `VIEW_STATUS_LABEL`; `interface MonitoringFilters { show; q; status; site; group; type; tags; deviceType }`; `NO_FILTERS`; `parseMonitoringParams(params: URLSearchParams): MonitoringFilters`; `buildMonitoringParams(f): URLSearchParams`; `filtersNarrow(f): boolean`; `visibleSections(f): Section[]`; `sectionVisible(s: { loading: boolean; error: string | null; total: number; shown: number; narrowed: boolean; canAdd: boolean }): boolean`; `monitorState(m)`, `serverState(a)`, `deviceState(d): ViewStatus`; `filterMonitors`, `filterServers`, `filterDevicesView`; `summaryCounts(monitors, agents, devices): SummaryCounts`; `statusOptions(monitors, devices, selected): ViewStatus[]`; `siteFilterOptions(sites, rows): { id: string; name: string }[]`; `MONITOR_SORTS`, `type MonitorSortKey`, `sortMonitors(list, key, uptimeById)`; `monitoringPath(show?: Section): string`; `legacyMonitoringTarget(location: { search: string; hash: string }, show: Section): string`; and in `monitorPages.ts`: `interface ListPage<T>`, `fetchAllPages<T>(getPage, maxPages = 100): Promise<T[]>`.

- [ ] **Step 1: Write the failing check**

`$CHECK/check.ts`:

```ts
import {
  NO_FILTERS, buildMonitoringParams, deviceState, filterDevicesView, filterMonitors, filterServers,
  filtersNarrow, legacyMonitoringTarget, monitorState, monitoringPath, parseMonitoringParams,
  sectionVisible, serverState, siteFilterOptions, sortMonitors, statusOptions, summaryCounts, visibleSections,
} from '/app/src/utils/monitoringView.ts'
import { fetchAllPages } from '/app/src/utils/monitorPages.ts'

let failures = 0
function expect(name: string, got: unknown, want: unknown) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures++
    console.log(`FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`)
  }
}
// Fixtures: only the fields the view model reads.
/* eslint-disable @typescript-eslint/no-explicit-any */
const mon = (o: any): any => ({ id: 'm', name: 'm', url: '', type: 'http', enabled: true, current_status: 'online', is_in_maintenance: false, tags: null, group_id: null, site_id: null, last_response_time_ms: 0, ...o })
const srv = (o: any): any => ({ id: 's', name: 's', agent_id: 'a1', hostname: null, ip_address: null, status: 'active', site_id: null, ...o })
const dev = (o: any): any => ({ id: 'd', name: 'd', host: '10.0.0.1', status: 'up', site_id: 'x', site_name: 'X', vendor: '', model: '', effective_type: 'switch', ...o })
const P = (s: string) => parseMonitoringParams(new URLSearchParams(s))

// Address <-> filters
expect('empty', P(''), NO_FILTERS)
const full = P('show=uptime&q=web&status=down&site=s1&group=g1&type=http&tags=prod,api&device_type=router')
expect('full', full, { show: 'uptime', q: 'web', status: 'down', site: 's1', group: 'g1', type: 'http', tags: ['prod', 'api'], deviceType: 'router' })
expect('round trip', buildMonitoringParams(full).toString(), 'show=uptime&q=web&status=down&site=s1&group=g1&type=http&tags=prod%2Capi&device_type=router')
expect('unknown values ignored', P('show=bogus&status=broken&type=ftp&device_type=toaster'), NO_FILTERS)
expect('blank tags', P('tags=,,').tags, [])
expect('defaults build empty', buildMonitoringParams(NO_FILTERS).toString(), '')
expect('narrow: chips only', filtersNarrow({ ...NO_FILTERS, show: 'servers' }), false)
expect('narrow: search', filtersNarrow({ ...NO_FILTERS, q: 'x' }), true)

// Sections
expect('all sections', visibleSections(NO_FILTERS), ['uptime', 'servers', 'devices'])
expect('one section', visibleSections({ ...NO_FILTERS, show: 'devices' }), ['devices'])
expect('group hides others', visibleSections({ ...NO_FILTERS, group: 'g1' }), ['uptime'])
expect('group with servers chip', visibleSections({ ...NO_FILTERS, group: 'g1', show: 'servers' }), [])
const vis = { loading: false, error: null, total: 0, shown: 0, narrowed: false, canAdd: true }
expect('empty, can add', sectionVisible(vis), true)
expect('empty, cannot add', sectionVisible({ ...vis, canAdd: false }), false)
expect('filtered to nothing', sectionVisible({ ...vis, total: 3, narrowed: true }), false)
expect('loading', sectionVisible({ ...vis, loading: true, canAdd: false }), true)
expect('failed', sectionVisible({ ...vis, error: 'boom', canAdd: false }), true)
expect('has rows', sectionVisible({ ...vis, total: 3, shown: 1, narrowed: true }), true)

// One status scale
expect('paused beats offline', monitorState(mon({ enabled: false, current_status: 'offline' })), 'paused')
expect('maintenance beats offline', monitorState(mon({ is_in_maintenance: true, current_status: 'offline' })), 'maintenance')
expect('online', monitorState(mon({})), 'up')
expect('offline', monitorState(mon({ current_status: 'offline' })), 'down')
expect('unknown', monitorState(mon({ current_status: 'unknown' })), 'pending')
expect('server active', serverState(srv({})), 'up')
expect('server offline', serverState(srv({ status: 'offline' })), 'down')
expect('server pending', serverState(srv({ status: 'pending' })), 'pending')
expect('device error', deviceState(dev({ status: 'error' })), 'error')

// Filtering
const ms = [
  mon({ id: 'a', name: 'Web', url: 'https://www.example.gov', site_id: 's1', group_id: 'g1', tags: ['prod'] }),
  mon({ id: 'b', name: 'Tax', type: 'tcp', current_status: 'offline', tags: ['api'] }),
]
const ids = (rows: { id: string }[]) => rows.map((r) => r.id)
expect('search url', ids(filterMonitors(ms, { ...NO_FILTERS, q: 'EXAMPLE' })), ['a'])
expect('status down', ids(filterMonitors(ms, { ...NO_FILTERS, status: 'down' })), ['b'])
expect('no site', ids(filterMonitors(ms, { ...NO_FILTERS, site: 'none' })), ['b'])
expect('site', ids(filterMonitors(ms, { ...NO_FILTERS, site: 's1' })), ['a'])
expect('ungrouped', ids(filterMonitors(ms, { ...NO_FILTERS, group: 'ungrouped' })), ['b'])
expect('type', ids(filterMonitors(ms, { ...NO_FILTERS, type: 'tcp' })), ['b'])
expect('tags any of', ids(filterMonitors(ms, { ...NO_FILTERS, tags: ['prod', 'api'] })), ['a', 'b'])
const ss = [srv({ id: 'x', hostname: 'fs-01.local', ip_address: '10.1.1.5', site_id: 's1' }), srv({ id: 'y', status: 'offline' })]
expect('server search ip', ids(filterServers(ss, { ...NO_FILTERS, q: '10.1.1' })), ['x'])
expect('server search host', ids(filterServers(ss, { ...NO_FILTERS, q: 'FS-01' })), ['x'])
expect('server down', ids(filterServers(ss, { ...NO_FILTERS, status: 'down' })), ['y'])
expect('server site', ids(filterServers(ss, { ...NO_FILTERS, site: 's1' })), ['x'])
const ds = [dev({ id: 'p' }), dev({ id: 'q', status: 'error', effective_type: 'router', site_id: 's1', site_name: 'Annex' })]
expect('device type', ids(filterDevicesView(ds, { ...NO_FILTERS, deviceType: 'router' })), ['q'])
expect('device error', ids(filterDevicesView(ds, { ...NO_FILTERS, status: 'error' })), ['q'])
expect('device search site', ids(filterDevicesView(ds, { ...NO_FILTERS, q: 'annex' })), ['q'])

// Summary and choices
expect('summary', summaryCounts(
  [mon({}), mon({ current_status: 'offline' }), mon({ enabled: false })],
  [srv({ status: 'offline' }), srv({})],
  [dev({ status: 'error' }), dev({ status: 'down' }), dev({ status: 'paused' })],
), { watching: 8, down: 3, paused: 2 })
expect('statuses plain', statusOptions([mon({})], [dev({})], null), ['down', 'up', 'pending', 'paused'])
expect('statuses extra', statusOptions([mon({ is_in_maintenance: true })], [dev({ status: 'error' })], null), ['down', 'up', 'pending', 'paused', 'maintenance', 'error'])
expect('statuses keep selection', statusOptions([], [], 'error').at(-1), 'error')
expect('site options', siteFilterOptions(
  [{ id: 's2', name: 'courthouse' }, { id: 's1', name: 'Annex' }],
  [{ site_id: 's9', site_name: 'Warehouse' }, { site_id: 's1', site_name: 'Annex' }, { site_id: null }],
).map((s) => s.name), ['Annex', 'courthouse', 'Warehouse'])

// Sort
const toSort = [mon({ id: 'p', name: 'A', enabled: false }), mon({ id: 'u', name: 'B' }), mon({ id: 'd', name: 'C', current_status: 'offline' })]
expect('down first, paused last', ids(sortMonitors(toSort, 'down-first', new Map())), ['d', 'u', 'p'])
expect('lowest uptime', ids(sortMonitors(toSort, 'uptime', new Map([['u', 90], ['d', 99]]))), ['u', 'd', 'p'])
expect('sort leaves input', ids(toSort), ['p', 'u', 'd'])

// Addresses
expect('path all', monitoringPath(), '/monitoring')
expect('path one', monitoringPath('servers'), '/monitoring?show=servers')
expect('legacy keeps query', legacyMonitoringTarget({ search: '?type=http', hash: '' }, 'uptime'), '/monitoring?show=uptime&type=http')
expect('legacy replaces show', legacyMonitoringTarget({ search: '?show=devices&q=x', hash: '#top' }, 'servers'), '/monitoring?show=servers&q=x#top')

// Paging
async function paging() {
  const pages = [[1, 2], [3, 4], [5]]
  const calls: number[] = []
  const all = await fetchAllPages(async (page) => {
    calls.push(page)
    return { monitors: pages[page - 1] ?? [], pagination: { pages: 3 } }
  })
  expect('all pages', all, [1, 2, 3, 4, 5])
  expect('pages fetched', calls, [1, 2, 3])
  expect('no pages', await fetchAllPages(async () => ({ monitors: [], pagination: { pages: 0 } })), [])
  expect('missing count is one page', await fetchAllPages(async () => ({ monitors: [7], pagination: {} })), [7])
  let n = 0
  await fetchAllPages(async () => { n++; return { monitors: [n], pagination: { pages: 10_000 } } }, 5)
  expect('capped', n, 5)
}

paging().then(() => {
  if (failures) {
    console.log(`${failures} check(s) failed`)
    process.exit(1)
  }
  console.log('monitoringView: all checks passed')
})
```

- [ ] **Step 2: Run it to verify it fails**

Run the throwaway check command.
Expected: esbuild error — `Could not resolve "/app/src/utils/monitoringView.ts"`.

- [ ] **Step 3: Write `monitorPages.ts`**

`frontend/src/utils/monitorPages.ts`:

```ts
// Reading a paginated list in full. Free of `@/` imports so it can be checked
// alone.

/** One page of a list, as GET /monitors returns it. */
export interface ListPage<T> {
  monitors: T[]
  pagination: { pages?: number }
}

/** fetchAllPages reads page 1, 2, ... up to the last page the server reports,
 *  so a list longer than one page is never cut short. A missing page count
 *  means one page; maxPages bounds it whatever the server says, so a wrong
 *  count cannot loop forever. */
export async function fetchAllPages<T>(
  getPage: (page: number) => Promise<ListPage<T>>,
  maxPages = 100
): Promise<T[]> {
  const all: T[] = []
  for (let page = 1; page <= maxPages; page++) {
    const res = await getPage(page)
    all.push(...res.monitors)
    if (res.monitors.length === 0 || page >= (res.pagination.pages ?? 1)) break
  }
  return all
}
```

- [ ] **Step 4: Write `monitoringView.ts`**

`frontend/src/utils/monitoringView.ts`:

```ts
// The Monitoring page's model: the filters as they live in the address, the
// one status scale monitors, servers and devices share, and what each section
// shows. Only type imports from `@/` and relative value imports, so it can be
// checked on its own.
import type { Monitor } from '@/types'
import type { Agent } from '@/hooks/useAgents'
import type { Device, DeviceType } from '@/hooks/useDevices'
import { deviceType, vendorModel } from './devices'

export type Section = 'uptime' | 'servers' | 'devices'
export const SECTIONS: readonly Section[] = ['uptime', 'servers', 'devices']
export const SECTION_LABEL: Record<Section, string> = { uptime: 'Uptime checks', servers: 'Servers', devices: 'Devices' }
/** The type chips' labels. */
export const SECTION_CHIP: Record<Section, string> = { uptime: 'Uptime', servers: 'Servers', devices: 'Devices' }

export type ViewStatus = 'down' | 'up' | 'pending' | 'paused' | 'maintenance' | 'error'
export const VIEW_STATUS_LABEL: Record<ViewStatus, string> = {
  down: 'Down',
  up: 'Up',
  pending: 'Pending',
  paused: 'Paused',
  maintenance: 'Maintenance',
  error: 'Error',
}
const VIEW_STATUSES = Object.keys(VIEW_STATUS_LABEL) as ViewStatus[]
const MONITOR_TYPES = ['http', 'dns', 'ping', 'tcp', 'webhook'] as const
const DEVICE_TYPES: readonly DeviceType[] = ['switch', 'router', 'access_point', 'nvr', 'ups', 'other']

export interface MonitoringFilters {
  /** One section, or null for all of them (the type chips). */
  show: Section | null
  q: string
  status: ViewStatus | null
  /** A site id, 'none' for "no site", or null for every site. */
  site: string | null
  /** A monitor group id, 'ungrouped', or null. Uptime checks only. */
  group: string | null
  /** A monitor check type. Uptime checks only. */
  type: string | null
  /** Any of these tags. Uptime checks only. */
  tags: string[]
  /** Devices only. */
  deviceType: DeviceType | null
}

export const NO_FILTERS: MonitoringFilters = {
  show: null,
  q: '',
  status: null,
  site: null,
  group: null,
  type: null,
  tags: [],
  deviceType: null,
}

function oneOf<T extends string>(value: string | null, allowed: readonly T[]): T | null {
  return value !== null && (allowed as readonly string[]).includes(value) ? (value as T) : null
}

/** parseMonitoringParams reads the filters from the address. An unknown value
 *  is ignored, as if the parameter were absent. */
export function parseMonitoringParams(params: URLSearchParams): MonitoringFilters {
  const id = (key: string) => params.get(key)?.trim() || null
  return {
    show: oneOf(params.get('show'), SECTIONS),
    q: params.get('q') ?? '',
    status: oneOf(params.get('status'), VIEW_STATUSES),
    site: id('site'),
    group: id('group'),
    type: oneOf(params.get('type'), MONITOR_TYPES),
    tags: (params.get('tags') ?? '').split(',').map((t) => t.trim()).filter(Boolean),
    deviceType: oneOf(params.get('device_type'), DEVICE_TYPES),
  }
}

/** buildMonitoringParams writes the filters back, leaving defaults out. */
export function buildMonitoringParams(f: MonitoringFilters): URLSearchParams {
  const p = new URLSearchParams()
  if (f.show) p.set('show', f.show)
  if (f.q) p.set('q', f.q)
  if (f.status) p.set('status', f.status)
  if (f.site) p.set('site', f.site)
  if (f.group) p.set('group', f.group)
  if (f.type) p.set('type', f.type)
  if (f.tags.length) p.set('tags', f.tags.join(','))
  if (f.deviceType) p.set('device_type', f.deviceType)
  return p
}

/** filtersNarrow is whether anything besides the type chips narrows the rows. */
export function filtersNarrow(f: MonitoringFilters): boolean {
  return (
    f.q.trim() !== '' ||
    f.status !== null ||
    f.site !== null ||
    f.group !== null ||
    f.type !== null ||
    f.tags.length > 0 ||
    f.deviceType !== null
  )
}

/** visibleSections is which sections the chips allow, in page order. Servers
 *  and devices have no groups, so a group filter leaves uptime checks only. */
export function visibleSections(f: MonitoringFilters): Section[] {
  const shown = f.show ? [f.show] : [...SECTIONS]
  return f.group ? shown.filter((s) => s === 'uptime') : shown
}

/** sectionVisible decides whether an allowed section is drawn: always while
 *  loading or failed, when it has rows, and when it is genuinely empty (no
 *  filter narrows it) and this user could add the first one. */
export function sectionVisible(s: {
  loading: boolean
  error: string | null
  total: number
  shown: number
  narrowed: boolean
  canAdd: boolean
}): boolean {
  if (s.loading || s.error) return true
  if (s.shown > 0) return true
  return !s.narrowed && s.total === 0 && s.canAdd
}

/** monitorState places a monitor on the shared scale. Paused and maintenance
 *  win over the last check, as the Uptime page has always shown them. */
export function monitorState(m: Pick<Monitor, 'enabled' | 'is_in_maintenance' | 'current_status'>): ViewStatus {
  if (!m.enabled) return 'paused'
  if (m.is_in_maintenance) return 'maintenance'
  if (m.current_status === 'online') return 'up'
  if (m.current_status === 'offline') return 'down'
  return 'pending'
}

export function serverState(a: Pick<Agent, 'status'>): ViewStatus {
  if (a.status === 'active') return 'up'
  if (a.status === 'offline') return 'down'
  return 'pending'
}

export function deviceState(d: Pick<Device, 'status'>): ViewStatus {
  return d.status
}

function siteMatches(filter: string | null, siteId: string | null | undefined): boolean {
  if (filter === null) return true
  if (filter === 'none') return !siteId
  return siteId === filter
}

function textMatches(q: string, values: (string | null | undefined)[]): boolean {
  const needle = q.trim().toLowerCase()
  return !needle || values.some((v) => v?.toLowerCase().includes(needle))
}

export function filterMonitors(monitors: Monitor[], f: MonitoringFilters): Monitor[] {
  return monitors.filter(
    (m) =>
      textMatches(f.q, [m.name, m.url]) &&
      (f.status === null || monitorState(m) === f.status) &&
      siteMatches(f.site, m.site_id) &&
      (f.group === null || (f.group === 'ungrouped' ? !m.group_id : m.group_id === f.group)) &&
      (f.type === null || m.type === f.type) &&
      (f.tags.length === 0 || (m.tags ?? []).some((t) => f.tags.includes(t)))
  )
}

export function filterServers(agents: Agent[], f: MonitoringFilters): Agent[] {
  return agents.filter(
    (a) =>
      textMatches(f.q, [a.name, a.hostname, a.agent_id, a.ip_address]) &&
      (f.status === null || serverState(a) === f.status) &&
      siteMatches(f.site, a.site_id)
  )
}

export function filterDevicesView(devices: Device[], f: MonitoringFilters): Device[] {
  return devices.filter(
    (d) =>
      textMatches(f.q, [d.name, d.host, vendorModel(d), d.site_name]) &&
      (f.status === null || deviceState(d) === f.status) &&
      siteMatches(f.site, d.site_id) &&
      (f.deviceType === null || deviceType(d) === f.deviceType)
  )
}

export interface SummaryCounts {
  watching: number
  down: number
  paused: number
}

/** summaryCounts is the strip under the toolbar, over everything loaded.
 *  Error and Pending are not Down; servers cannot be paused. */
export function summaryCounts(monitors: Monitor[], agents: Agent[], devices: Device[]): SummaryCounts {
  let down = 0
  let paused = 0
  for (const m of monitors) {
    const s = monitorState(m)
    if (s === 'down') down++
    else if (s === 'paused') paused++
  }
  for (const a of agents) if (serverState(a) === 'down') down++
  for (const d of devices) {
    const s = deviceState(d)
    if (s === 'down') down++
    else if (s === 'paused') paused++
  }
  return { watching: monitors.length + agents.length + devices.length, down, paused }
}

/** statusOptions is the status select: the four everyday states, plus
 *  Maintenance and Error while something is in them (or one is selected). */
export function statusOptions(monitors: Monitor[], devices: Device[], selected: ViewStatus | null): ViewStatus[] {
  const out: ViewStatus[] = ['down', 'up', 'pending', 'paused']
  if (selected === 'maintenance' || monitors.some((m) => monitorState(m) === 'maintenance')) out.push('maintenance')
  if (selected === 'error' || devices.some((d) => d.status === 'error')) out.push('error')
  return out
}

/** siteFilterOptions is the site select: the user's sites plus any other site
 *  named on a row they can see, A-Z. */
export function siteFilterOptions(
  sites: { id: string; name: string }[],
  rows: { site_id?: string | null; site_name?: string | null }[]
): { id: string; name: string }[] {
  const byId = new Map(sites.map((s) => [s.id, s.name]))
  for (const r of rows) {
    if (r.site_id && !byId.has(r.site_id)) byId.set(r.site_id, r.site_name ?? 'Unknown site')
  }
  return [...byId].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))
}

export const MONITOR_SORTS = [
  { key: 'down-first', label: 'Down first' },
  { key: 'name', label: 'Name (A–Z)' },
  { key: 'uptime', label: 'Lowest uptime' },
  { key: 'slowest', label: 'Slowest first' },
] as const
export type MonitorSortKey = (typeof MONITOR_SORTS)[number]['key']

// Sort weight for "Down first": states that need attention float up, and
// paused monitors sink below healthy ones - dormant is not urgent.
function urgency(m: Monitor): number {
  const s = monitorState(m)
  if (s === 'paused') return 4
  if (s === 'down') return 0
  if (s === 'maintenance') return 1
  if (s === 'up') return 3
  return 2
}

/** sortMonitors returns a sorted copy, ties broken by name. */
export function sortMonitors(list: Monitor[], key: MonitorSortKey, uptimeById: Map<string, number>): Monitor[] {
  const byName = (a: Monitor, b: Monitor) => a.name.localeCompare(b.name)
  return [...list].sort((a, b) => {
    switch (key) {
      case 'name':
        return byName(a, b)
      case 'uptime':
        return (uptimeById.get(a.id) ?? 100) - (uptimeById.get(b.id) ?? 100) || byName(a, b)
      case 'slowest':
        return b.last_response_time_ms - a.last_response_time_ms || byName(a, b)
      default:
        return urgency(a) - urgency(b) || byName(a, b)
    }
  })
}

/** monitoringPath is the Monitoring page, optionally opened on one section. */
export function monitoringPath(show?: Section): string {
  return show ? `/monitoring?show=${show}` : '/monitoring'
}

/** legacyMonitoringTarget is where an old list address goes: Monitoring on
 *  `show`, keeping the old address's query (so /uptime?type=http keeps its
 *  filter) and hash. */
export function legacyMonitoringTarget(location: { search: string; hash: string }, show: Section): string {
  const old = new URLSearchParams(location.search)
  const next = new URLSearchParams({ show })
  old.forEach((value, key) => {
    if (key !== 'show') next.append(key, value)
  })
  return `/monitoring?${next.toString()}${location.hash}`
}
```

- [ ] **Step 5: Run the check to verify it passes**

Run the throwaway check command.
Expected: `monitoringView: all checks passed`.

- [ ] **Step 6: Run the frontend gate**

Expected: no errors (the modules are unused so far; `tsc` still type-checks them).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/utils/monitoringView.ts frontend/src/utils/monitorPages.ts
git commit -m "feat(monitoring): view model for the combined list

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: The Monitoring page

**Files:**
- Create: `frontend/src/hooks/{useAllMonitors,useDebounced,useDismissOnOutsideClick,useRememberedToggles}.ts`, `frontend/src/components/monitoring/{AddMenu,MonitoringToolbar,SummaryStrip,SectionShell,EmptyLine,GroupModal,UptimeSection,ServerRow,ServersSection,DevicesSection}.tsx`, `frontend/src/pages/Monitoring.tsx`
- Modify: `frontend/src/App.tsx` (add the `/monitoring` route)
- Test: the frontend gate, then a browser run on a dev server if one is available

**Interfaces:**
- Consumes: Task 5's whole view model; Task 4's `Monitor.site_name`, `Agent.site_name`; existing `useMonitorGroups()` (`groups`, `refetch`), `useAgents()` (`agents`, `loading`, `error: string | null`, `refetch`), `useAgentActions()` (`getWithToken`, `remove`, `busy`), `useAgentStatus(agentID, pollMs)`, `useDevices()` (`devices`, `loading`, `error: string | null`, `refetch`), `useSites()`, `useUsers()` (`usernameFor`), `useSummaryReport(start, end)` (`report`), `useToasts()`/`<Toaster>`, `<MonitorTable>`, `<GroupSection>`, `<DeviceTable devices showSite>`, `<CreateMonitorModal isOpen onClose onCreated push>`, `<AddServerAgentModal isOpen existing onClose onCreated push>`, `<DeviceFormModal siteId? onClose onSaved>`.
- Produces: route `/monitoring` → `pages/Monitoring.tsx`; `useAllMonitors(pollMs?)` → `{ monitors, loading, error: string | null, loadedAt: number, refetch }`; `useRememberedToggles(storageKey)` → `{ on: Record<string, boolean>, toggle(key) }`.

The old pages stay in place (ruling 8); this task copies what it needs from them.

- [ ] **Step 1: Hooks**

`frontend/src/hooks/useDebounced.ts`:

```ts
import { useEffect, useState } from 'react'

/** useDebounced returns a value that only updates after `ms` of no changes. */
export function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(value), ms)
    return () => window.clearTimeout(t)
  }, [value, ms])
  return debounced
}
```

`frontend/src/hooks/useDismissOnOutsideClick.ts`:

```ts
import { useEffect, useRef } from 'react'

/** Close a popover when the pointer goes down anywhere outside it, or on Escape. */
export function useDismissOnOutsideClick(open: boolean, close: () => void) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) close()
    }
    const onEsc = (e: KeyboardEvent) => e.key === 'Escape' && close()
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onEsc)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onEsc)
    }
  }, [open, close])
  return ref
}
```

`frontend/src/hooks/useRememberedToggles.ts`:

```ts
import { useCallback, useState } from 'react'

function load(key: string): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as Record<string, boolean>) : {}
  } catch {
    return {}
  }
}

/** useRememberedToggles is a set of on/off switches (collapsed sections or
 *  groups) kept in this browser under storageKey, so they survive leaving the
 *  page and coming back. */
export function useRememberedToggles(storageKey: string) {
  const [on, setOn] = useState<Record<string, boolean>>(() => load(storageKey))
  const toggle = useCallback(
    (key: string) =>
      setOn((cur) => {
        const next = { ...cur, [key]: !cur[key] }
        try {
          localStorage.setItem(storageKey, JSON.stringify(next))
        } catch {
          // Private browsing or a full quota: the toggle still works for this
          // visit, it just will not be remembered.
        }
        return next
      }),
    [storageKey]
  )
  return { on, toggle }
}
```

`frontend/src/hooks/useAllMonitors.ts`:

```ts
import { useCallback, useEffect, useRef, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse, Monitor, PaginatedMonitors } from '@/types'
import { fetchAllPages } from '@/utils/monitorPages'

// The API's largest page.
const PAGE_SIZE = 500

/** useAllMonitors is every monitor the user can see, paged through in full
 *  and refreshed every pollMs. loadedAt changes on each successful load, so
 *  rows can refresh what they fetch themselves. */
export function useAllMonitors(pollMs = 30_000) {
  const [monitors, setMonitors] = useState<Monitor[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [loadedAt, setLoadedAt] = useState(0)
  // Each load takes a number; only the newest may write, so a slow earlier
  // load cannot overwrite a newer one.
  const latest = useRef(0)

  const refetch = useCallback(async () => {
    const mine = ++latest.current
    try {
      const all = await fetchAllPages(async (page) => {
        const { data } = await api.get<ApiResponse<PaginatedMonitors>>('/monitors', {
          params: { page, limit: PAGE_SIZE },
        })
        return data.data
      })
      if (mine !== latest.current) return
      setMonitors(all)
      setError(null)
      setLoadedAt(Date.now())
    } catch (err) {
      if (mine !== latest.current) return
      setError((err as ApiError).message || 'Failed to load monitors')
    } finally {
      if (mine === latest.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refetch()
    const t = window.setInterval(() => void refetch(), pollMs)
    return () => window.clearInterval(t)
  }, [refetch, pollMs])

  return { monitors, loading, error, loadedAt, refetch }
}
```

- [ ] **Step 2: Small shared pieces**

`frontend/src/components/monitoring/EmptyLine.tsx`:

```tsx
/** A one-line empty state with an optional action. */
export default function EmptyLine({ text, action, onAction }: { text: string; action?: string; onAction?: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-lg border border-white/10 bg-slate-800/40 px-4 py-3 text-sm text-slate-400">
      <span>{text}</span>
      {action && onAction && (
        <button className="text-primary-400 hover:underline" onClick={onAction}>
          {action}
        </button>
      )}
    </div>
  )
}
```

`frontend/src/components/monitoring/SectionShell.tsx`:

```tsx
import type { ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'

interface Props {
  title: string
  /** Rows after filtering. */
  count: number
  down: number
  /** Figures for the header, e.g. average response time. */
  summary?: ReactNode
  /** The section's own filters. */
  controls?: ReactNode
  collapsed: boolean
  onToggle: () => void
  /** True until the first load finishes. */
  loading: boolean
  error: string | null
  onRetry: () => void
  children: ReactNode
}

/** One Monitoring section: a collapsible header with its count, how many are
 *  down and its own figures and filters, then its rows. Collapsed sections do
 *  not render their rows, so rows that fetch their own data stay quiet. */
export default function SectionShell(p: Props) {
  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <button className="flex items-center gap-2 text-left" onClick={p.onToggle} aria-expanded={!p.collapsed}>
          <ChevronDown className={`h-4 w-4 text-slate-400 transition-transform ${p.collapsed ? '-rotate-90' : ''}`} />
          <h2 className="text-xl font-light text-white">{p.title}</h2>
          <span className="text-sm tabular-nums text-slate-400">{p.loading ? '…' : p.count}</span>
          {p.down > 0 && (
            <span className="rounded-full border border-red-500/30 bg-red-500/10 px-2 py-0.5 text-xs font-medium text-red-400">
              {p.down} down
            </span>
          )}
        </button>
        {!p.collapsed && (p.summary || p.controls) && (
          <div className="flex flex-wrap items-center gap-3 text-sm text-slate-400">
            {p.summary}
            {p.controls}
          </div>
        )}
      </div>
      {!p.collapsed && (
        <>
          {p.error && (
            <div className="flex items-center justify-between rounded-lg border border-red-500/30 bg-red-500/10 p-3">
              <span className="text-sm text-red-400">{p.error}</span>
              <button className="btn-secondary" onClick={p.onRetry}>
                Retry
              </button>
            </div>
          )}
          {p.loading ? (
            <div className="space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="h-12 animate-pulse rounded-lg border border-white/10 bg-slate-800/40" />
              ))}
            </div>
          ) : (
            p.children
          )}
        </>
      )}
    </section>
  )
}
```

`frontend/src/components/monitoring/GroupModal.tsx`: copy `GroupModal` from `frontend/src/pages/UptimeMonitoring.tsx` (lines 285–394, the whole function) unchanged except `function GroupModal(` becomes `export default function GroupModal(`. Above it put these imports and the colour constant it uses (copied from the same file's line 38):

```tsx
import { useState } from 'react'
import { Trash2 } from 'lucide-react'
import ColorPicker from '@/components/ColorPicker'
import { useCreateMonitorGroup, useDeleteMonitorGroup, useUpdateMonitorGroup } from '@/hooks/useMonitorGroups'
import type { MonitorGroup } from '@/types'

// A group's default colour, stored with the group (not page styling).
const DEFAULT_GROUP_COLOR = '#10b981'
```

`frontend/src/components/monitoring/ServerRow.tsx`: copy from `frontend/src/pages/ServerMonitoring.tsx` the `STATUS_STYLE` constant and the helpers `usageClass`, `pct`, `relative`, `uptime` and `UsageBar` (lines 18–73), and `AgentRow` (lines 291–379), with these imports:

```tsx
import { ChevronRight, Terminal, Trash2 } from 'lucide-react'
import { useAgentStatus, type Agent } from '@/hooks/useAgents'
```

Change `function AgentRow(` to `export default function AgentRow(`, and add a Site cell directly after the Server cell (the first `<td>`):

```tsx
      <td className="px-4 py-3 text-sm text-slate-400">{agent.site_name ?? '—'}</td>
```

- [ ] **Step 3: Header, toolbar and summary**

`frontend/src/components/monitoring/AddMenu.tsx`:

```tsx
import { useCallback, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ChevronDown, FolderPlus, Globe, Plus, Radar, Router, Server, Upload, Wand2 } from 'lucide-react'
import { useDismissOnOutsideClick } from '@/hooks/useDismissOnOutsideClick'

interface Props {
  isAdmin: boolean
  onMonitor: () => void
  onGroup: () => void
  onServer: () => void
  onDevice: () => void
}

/** The page's one "+ Add" menu: every create action of the old Uptime,
 *  Servers and Devices pages, the admin-only ones for admins only. */
export default function AddMenu({ isAdmin, onMonitor, onGroup, onServer, onDevice }: Props) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const ref = useDismissOnOutsideClick(open, useCallback(() => setOpen(false), []))

  const items = [
    { key: 'monitor', label: 'Uptime monitor', icon: Globe, onClick: onMonitor },
    { key: 'wizard', label: 'Monitor wizard', icon: Wand2, onClick: () => navigate('/monitors/new/wizard') },
    { key: 'bulk', label: 'Bulk upload', icon: Upload, onClick: () => navigate('/monitors/bulk') },
    { key: 'group', label: 'Monitor group', icon: FolderPlus, onClick: onGroup },
    ...(isAdmin
      ? [
          { key: 'discover', label: 'Network discovery', icon: Radar, onClick: () => navigate('/monitors/discover') },
          { key: 'server', label: 'Server agent', icon: Server, onClick: onServer },
          { key: 'device', label: 'Network device', icon: Router, onClick: onDevice },
        ]
      : []),
  ]

  return (
    <div ref={ref} className="relative shrink-0">
      <button className="rd-btn rd-btn-primary" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <Plus className="h-4 w-4" /> Add
        <ChevronDown className={`h-3.5 w-3.5 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div role="menu" className="absolute right-0 z-40 mt-2 w-56 overflow-hidden rounded-lg border border-white/10 bg-slate-900 py-1 shadow-xl">
          {items.map((item) => (
            <button
              key={item.key}
              role="menuitem"
              onClick={() => {
                setOpen(false)
                item.onClick()
              }}
              className="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-white transition-colors hover:bg-white/5"
            >
              <item.icon className="h-4 w-4 shrink-0 text-cyan-400" />
              <span className="flex-1">{item.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
```

`frontend/src/components/monitoring/MonitoringToolbar.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { Search, X } from 'lucide-react'
import { useDebounced } from '@/hooks/useDebounced'
import {
  SECTIONS,
  SECTION_CHIP,
  VIEW_STATUS_LABEL,
  type MonitoringFilters,
  type Section,
  type ViewStatus,
} from '@/utils/monitoringView'

interface Props {
  filters: MonitoringFilters
  /** replace: change the address without a new history entry (typing). */
  onChange: (next: MonitoringFilters, replace?: boolean) => void
  counts: Record<Section, number>
  statuses: ViewStatus[]
  sites: { id: string; name: string }[]
  groups: { id: string; name: string }[]
}

const chipCls = (on: boolean) =>
  `rounded-full px-3 py-1 text-xs font-medium transition ${
    on ? 'bg-primary-600 text-white' : 'bg-slate-800 text-slate-300 hover:bg-slate-700'
  }`

/** The filters every section shares: search, status, site, group and the
 *  type chips. */
export default function MonitoringToolbar({ filters, onChange, counts, statuses, sites, groups }: Props) {
  const [text, setText] = useState(filters.q)
  const debounced = useDebounced(text, 300)

  // Follow the address when it changes from outside (Back, Clear filters).
  useEffect(() => {
    setText(filters.q)
  }, [filters.q])

  useEffect(() => {
    if (debounced !== filters.q) onChange({ ...filters, q: debounced }, true)
    // Runs on the debounced text only: filters and onChange change on every
    // address change, and re-running then would undo Back.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debounced])

  const total = counts.uptime + counts.servers + counts.devices

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2.5">
        <div className="relative min-w-[200px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Search by name or address"
            aria-label="Search by name or address"
            className="rd-input py-2 pl-9 pr-8"
          />
          {text && (
            <button
              onClick={() => setText('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-slate-500 transition hover:text-white"
              aria-label="Clear search"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
        <select
          className="rd-select"
          aria-label="Filter by status"
          value={filters.status ?? ''}
          onChange={(e) => onChange({ ...filters, status: (e.target.value || null) as ViewStatus | null })}
        >
          <option value="">All statuses</option>
          {statuses.map((s) => (
            <option key={s} value={s}>
              {VIEW_STATUS_LABEL[s]}
            </option>
          ))}
        </select>
        <select
          className="rd-select"
          aria-label="Filter by site"
          value={filters.site ?? ''}
          onChange={(e) => onChange({ ...filters, site: e.target.value || null })}
        >
          <option value="">All sites</option>
          <option value="none">No site</option>
          {sites.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
        {groups.length > 0 && (
          <select
            className="rd-select"
            aria-label="Filter by group"
            value={filters.group ?? ''}
            onChange={(e) => onChange({ ...filters, group: e.target.value || null })}
          >
            <option value="">All groups</option>
            <option value="ungrouped">Ungrouped</option>
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        )}
      </div>
      <div className="flex flex-wrap gap-1.5" role="group" aria-label="Show">
        <button className={chipCls(filters.show === null)} aria-pressed={filters.show === null} onClick={() => onChange({ ...filters, show: null })}>
          All {total}
        </button>
        {SECTIONS.map((s) => (
          <button key={s} className={chipCls(filters.show === s)} aria-pressed={filters.show === s} onClick={() => onChange({ ...filters, show: s })}>
            {SECTION_CHIP[s]} {counts[s]}
          </button>
        ))}
      </div>
    </div>
  )
}
```

`frontend/src/components/monitoring/SummaryStrip.tsx`:

```tsx
import type { SummaryCounts } from '@/utils/monitoringView'

interface Props {
  counts: SummaryCounts
  /** Labels of the sections that failed to load. */
  missing: string[]
  onPick: (status: 'down' | 'paused') => void
}

/** Watching · Down · Paused across everything loaded. Down and Paused set
 *  that status filter. */
export default function SummaryStrip({ counts, missing, onPick }: Props) {
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-1 rounded-lg border border-white/10 bg-slate-800/40 px-4 py-2.5 text-sm">
      <span className="text-slate-400">
        Watching <span className="font-semibold tabular-nums text-white">{counts.watching}</span>
      </span>
      <button className="text-slate-400 transition hover:text-white" onClick={() => onPick('down')}>
        Down{' '}
        <span className={`font-semibold tabular-nums ${counts.down > 0 ? 'text-red-400' : 'text-white'}`}>{counts.down}</span>
      </button>
      <button className="text-slate-400 transition hover:text-white" onClick={() => onPick('paused')}>
        Paused <span className="font-semibold tabular-nums text-white">{counts.paused}</span>
      </button>
      {missing.length > 0 && <span className="text-amber-300">{missing.join(', ')} couldn&rsquo;t load</span>}
    </div>
  )
}
```

- [ ] **Step 4: The three sections**

`frontend/src/components/monitoring/UptimeSection.tsx`:

```tsx
import { useMemo, useState } from 'react'
import GroupSection from '@/components/GroupSection'
import MonitorTable from '@/components/MonitorTable'
import SectionShell from '@/components/monitoring/SectionShell'
import EmptyLine from '@/components/monitoring/EmptyLine'
import { useRememberedToggles } from '@/hooks/useRememberedToggles'
import { MONITOR_SORTS, monitorState, sortMonitors, type MonitorSortKey, type MonitoringFilters } from '@/utils/monitoringView'
import type { Monitor, MonitorGroup } from '@/types'

interface Props {
  /** Rows after every filter. */
  monitors: Monitor[]
  /** Every loaded monitor: the check-type and tag choices, the average. */
  all: Monitor[]
  groups: MonitorGroup[]
  filters: MonitoringFilters
  onFilters: (next: MonitoringFilters) => void
  /** Whether any filter narrows the rows (empty groups are then hidden). */
  narrowed: boolean
  uptimeById: Map<string, number>
  incidents30d: number | null
  usernameFor: (id: string | null | undefined) => string | undefined
  refreshKey: number
  collapsed: boolean
  onToggle: () => void
  loading: boolean
  error: string | null
  onRetry: () => void
  onChanged: () => void
  onAdd: () => void
  onEditGroup: (group: MonitorGroup) => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

const chipCls = (on: boolean) =>
  `rounded-full px-2.5 py-1 text-xs font-medium transition ${
    on ? 'bg-primary-600 text-white' : 'bg-slate-800 text-slate-300 hover:bg-slate-700'
  }`

export default function UptimeSection(p: Props) {
  const [sort, setSort] = useState<MonitorSortKey>('down-first')
  // The same key the Uptime page used, so collapsed groups stay collapsed.
  const groupToggles = useRememberedToggles('sentinel:uptimeCollapsedGroups')

  const sorted = useMemo(() => sortMonitors(p.monitors, sort, p.uptimeById), [p.monitors, sort, p.uptimeById])
  const types = useMemo(() => [...new Set(p.all.map((m) => m.type))].sort(), [p.all])
  const tags = useMemo(() => [...new Set(p.all.flatMap((m) => m.tags ?? []))].sort(), [p.all])
  const avgResponse = useMemo(() => {
    const timed = p.all.filter((m) => m.enabled && m.last_response_time_ms > 0)
    return timed.length ? Math.round(timed.reduce((sum, m) => sum + m.last_response_time_ms, 0) / timed.length) : 0
  }, [p.all])
  const down = p.monitors.filter((m) => monitorState(m) === 'down').length

  const ungrouped = sorted.filter((m) => !m.group_id)
  const byGroup = useMemo(() => {
    const map = new Map<string, Monitor[]>()
    for (const m of sorted) {
      if (!m.group_id) continue
      const list = map.get(m.group_id) ?? []
      list.push(m)
      map.set(m.group_id, list)
    }
    return map
  }, [sorted])

  const toggleTag = (t: string) =>
    p.onFilters({ ...p.filters, tags: p.filters.tags.includes(t) ? p.filters.tags.filter((x) => x !== t) : [...p.filters.tags, t] })

  const table = (rows: Monitor[]) => (
    <MonitorTable
      monitors={rows}
      uptimeById={p.uptimeById}
      usernameFor={p.usernameFor}
      onChanged={p.onChanged}
      push={p.push}
      refreshKey={p.refreshKey}
    />
  )

  return (
    <SectionShell
      title="Uptime checks"
      count={p.monitors.length}
      down={down}
      collapsed={p.collapsed}
      onToggle={p.onToggle}
      loading={p.loading}
      error={p.error}
      onRetry={p.onRetry}
      summary={
        <span>
          {avgResponse > 0 ? `Avg ${avgResponse} ms` : 'No response times yet'} · {p.incidents30d ?? 0} incidents (30 days)
        </span>
      }
      controls={
        <>
          <select
            className="rd-select"
            aria-label="Filter by check type"
            value={p.filters.type ?? ''}
            onChange={(e) => p.onFilters({ ...p.filters, type: e.target.value || null })}
          >
            <option value="">All check types</option>
            {types.map((t) => (
              <option key={t} value={t}>
                {t.toUpperCase()}
              </option>
            ))}
          </select>
          <select className="rd-select" aria-label="Sort monitors" value={sort} onChange={(e) => setSort(e.target.value as MonitorSortKey)}>
            {MONITOR_SORTS.map((s) => (
              <option key={s.key} value={s.key}>
                {s.label}
              </option>
            ))}
          </select>
        </>
      }
    >
      {p.all.length === 0 ? (
        <EmptyLine text="No uptime checks yet." action="Add an uptime monitor" onAction={p.onAdd} />
      ) : (
        <div className="space-y-5">
          {tags.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="vs-eyebrow mr-1">Tags</span>
              {tags.map((t) => (
                <button key={t} className={chipCls(p.filters.tags.includes(t))} aria-pressed={p.filters.tags.includes(t)} onClick={() => toggleTag(t)}>
                  {t}
                </button>
              ))}
            </div>
          )}
          {p.groups.map((g) => {
            const members = byGroup.get(g.id) ?? []
            if (p.narrowed && members.length === 0) return null
            return (
              <GroupSection
                key={g.id}
                title={g.name}
                color={g.color}
                uptime={g.group_uptime}
                count={members.length}
                expanded={!groupToggles.on[g.id]}
                onToggle={() => groupToggles.toggle(g.id)}
                onEdit={() => p.onEditGroup(g)}
              >
                {members.length === 0 ? (
                  <p className="px-2 py-1 text-sm text-slate-400">
                    No monitors in this group yet &mdash; assign one from a row&rsquo;s Group dropdown.
                  </p>
                ) : (
                  table(members)
                )}
              </GroupSection>
            )
          })}
          {ungrouped.length > 0 &&
            (p.groups.length > 0 ? (
              <GroupSection
                title="Ungrouped"
                color={null}
                uptime={null}
                count={ungrouped.length}
                expanded={!groupToggles.on.__ungrouped}
                onToggle={() => groupToggles.toggle('__ungrouped')}
              >
                {table(ungrouped)}
              </GroupSection>
            ) : (
              table(ungrouped)
            ))}
        </div>
      )}
    </SectionShell>
  )
}
```

`frontend/src/components/monitoring/ServersSection.tsx`:

```tsx
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import SectionShell from '@/components/monitoring/SectionShell'
import EmptyLine from '@/components/monitoring/EmptyLine'
import AgentRow from '@/components/monitoring/ServerRow'
import { useAgentActions, type Agent, type CreatedAgent } from '@/hooks/useAgents'
import type { ApiError } from '@/services/api'

interface Props {
  /** Rows after every filter. */
  agents: Agent[]
  /** How many servers there are at all. */
  total: number
  isAdmin: boolean
  collapsed: boolean
  onToggle: () => void
  loading: boolean
  error: string | null
  onRetry: () => void
  onChanged: () => void
  onAdd: () => void
  /** Opens the install instructions for an existing server. */
  onInstructions: (agent: CreatedAgent) => void
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

export default function ServersSection(p: Props) {
  const navigate = useNavigate()
  const { getWithToken, remove, busy } = useAgentActions()
  const [confirmDelete, setConfirmDelete] = useState<Agent | null>(null)
  const offline = p.agents.filter((a) => a.status === 'offline').length
  const pending = p.agents.filter((a) => a.status === 'pending').length

  const showInstructions = async (agent: Agent) => {
    try {
      // Re-fetched rather than held in memory: the token is not in the list
      // payload, and the install command is meaningless without it.
      p.onInstructions(await getWithToken(agent.agent_id))
    } catch (err) {
      p.push((err as ApiError).message || 'Could not load install instructions', 'error')
    }
  }

  const doDelete = async () => {
    if (!confirmDelete) return
    try {
      await remove(confirmDelete.agent_id)
      p.push(`${confirmDelete.name} unregistered`, 'success')
      setConfirmDelete(null)
      p.onChanged()
    } catch (err) {
      p.push((err as ApiError).message || 'Could not unregister the agent', 'error')
    }
  }

  return (
    <SectionShell
      title="Servers"
      count={p.agents.length}
      down={offline}
      collapsed={p.collapsed}
      onToggle={p.onToggle}
      loading={p.loading}
      error={p.error}
      onRetry={p.onRetry}
      summary={
        <span>
          {offline} offline · {pending} awaiting first report
        </span>
      }
    >
      {p.total === 0 ? (
        <EmptyLine text="No servers are being monitored yet." action="Add a server agent" onAction={p.onAdd} />
      ) : (
        <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40 backdrop-blur-sm">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10 bg-slate-800/20 text-xs font-medium text-slate-400">
                  <th className="px-4 py-3 text-left">Server</th>
                  <th className="px-4 py-3 text-left">Site</th>
                  <th className="px-4 py-3 text-left">Status</th>
                  <th className="px-4 py-3 text-left">CPU</th>
                  <th className="px-4 py-3 text-left">Memory</th>
                  <th className="px-4 py-3 text-left">Disk</th>
                  <th className="px-4 py-3 text-left">Uptime</th>
                  <th className="px-4 py-3 text-left">Last Report</th>
                  <th className="px-4 py-3 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {p.agents.map((a, i) => (
                  <AgentRow
                    key={a.id}
                    agent={a}
                    striped={i % 2 === 1}
                    onOpen={() => navigate(`/servers/${a.agent_id}`)}
                    isAdmin={p.isAdmin}
                    onInstructions={() => void showInstructions(a)}
                    onDelete={() => setConfirmDelete(a)}
                  />
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {confirmDelete && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
          onMouseDown={(e) => e.target === e.currentTarget && setConfirmDelete(null)}
        >
          <div role="dialog" aria-modal="true" className="w-full max-w-md rounded-xl border border-white/10 bg-slate-900/95 p-6">
            <h3 className="text-lg font-semibold text-white">Unregister {confirmDelete.name}?</h3>
            <p className="mt-2 text-sm text-slate-400">
              Its metric history is deleted with it. The agent on that host will keep running and start failing to report
              until it is stopped and removed.
            </p>
            <div className="mt-6 flex justify-end gap-2">
              <button className="btn-secondary" onClick={() => setConfirmDelete(null)} disabled={busy}>
                Cancel
              </button>
              <button
                className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-red-500 disabled:opacity-50"
                onClick={() => void doDelete()}
                disabled={busy}
              >
                {busy ? 'Removing…' : 'Unregister'}
              </button>
            </div>
          </div>
        </div>
      )}
    </SectionShell>
  )
}
```

`frontend/src/components/monitoring/DevicesSection.tsx`:

```tsx
import SectionShell from '@/components/monitoring/SectionShell'
import EmptyLine from '@/components/monitoring/EmptyLine'
import DeviceTable from '@/components/network/DeviceTable'
import { DEVICE_TYPE_LABEL, type Device, type DeviceType } from '@/hooks/useDevices'
import type { MonitoringFilters } from '@/utils/monitoringView'

interface Props {
  /** Rows after every filter. */
  devices: Device[]
  /** How many devices there are at all. */
  total: number
  isAdmin: boolean
  filters: MonitoringFilters
  onFilters: (next: MonitoringFilters) => void
  collapsed: boolean
  onToggle: () => void
  loading: boolean
  error: string | null
  onRetry: () => void
  onAdd: () => void
}

export default function DevicesSection(p: Props) {
  const down = p.devices.filter((d) => d.status === 'down').length
  return (
    <SectionShell
      title="Devices"
      count={p.devices.length}
      down={down}
      collapsed={p.collapsed}
      onToggle={p.onToggle}
      loading={p.loading}
      error={p.error}
      onRetry={p.onRetry}
      controls={
        <select
          className="rd-select"
          aria-label="Filter by device type"
          value={p.filters.deviceType ?? ''}
          onChange={(e) => p.onFilters({ ...p.filters, deviceType: (e.target.value || null) as DeviceType | null })}
        >
          <option value="">All device types</option>
          {(Object.keys(DEVICE_TYPE_LABEL) as DeviceType[]).map((t) => (
            <option key={t} value={t}>
              {DEVICE_TYPE_LABEL[t]}
            </option>
          ))}
        </select>
      }
    >
      {p.total === 0 ? (
        <EmptyLine
          text={p.isAdmin ? "No devices yet. Add one, or scan a subnet from a site's page." : 'No devices yet.'}
          action={p.isAdmin ? 'Add a device' : undefined}
          onAction={p.onAdd}
        />
      ) : (
        <DeviceTable devices={p.devices} showSite />
      )}
    </SectionShell>
  )
}
```

- [ ] **Step 5: The page and its route**

`frontend/src/pages/Monitoring.tsx`:

```tsx
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useAuthContext } from '@/context/AuthContext'
import { useAllMonitors } from '@/hooks/useAllMonitors'
import { useMonitorGroups } from '@/hooks/useMonitorGroups'
import { useSummaryReport } from '@/hooks/useReports'
import { useUsers } from '@/hooks/useUsers'
import { useAgents, type CreatedAgent } from '@/hooks/useAgents'
import { useDevices } from '@/hooks/useDevices'
import { useSites } from '@/hooks/useSites'
import { useRememberedToggles } from '@/hooks/useRememberedToggles'
import { useToasts, Toaster } from '@/components/Toast'
import CreateMonitorModal from '@/components/CreateMonitorModal'
import AddServerAgentModal from '@/components/AddServerAgentModal'
import DeviceFormModal from '@/components/network/DeviceFormModal'
import AddMenu from '@/components/monitoring/AddMenu'
import GroupModal from '@/components/monitoring/GroupModal'
import MonitoringToolbar from '@/components/monitoring/MonitoringToolbar'
import SummaryStrip from '@/components/monitoring/SummaryStrip'
import UptimeSection from '@/components/monitoring/UptimeSection'
import ServersSection from '@/components/monitoring/ServersSection'
import DevicesSection from '@/components/monitoring/DevicesSection'
import {
  NO_FILTERS,
  SECTION_LABEL,
  buildMonitoringParams,
  filterDevicesView,
  filterMonitors,
  filterServers,
  filtersNarrow,
  parseMonitoringParams,
  sectionVisible,
  siteFilterOptions,
  statusOptions,
  summaryCounts,
  visibleSections,
  type MonitoringFilters,
  type Section,
} from '@/utils/monitoringView'
import type { MonitorGroup } from '@/types'

const GROUPS_REFRESH_MS = 30_000

export default function Monitoring() {
  const { currentUser } = useAuthContext()
  const isAdmin = !!currentUser?.is_admin
  const [searchParams, setSearchParams] = useSearchParams()
  const filters = useMemo(() => parseMonitoringParams(searchParams), [searchParams])
  const setFilters = useCallback(
    (next: MonitoringFilters, replace = false) => setSearchParams(buildMonitoringParams(next), { replace }),
    [setSearchParams]
  )

  const { monitors, loading: monitorsLoading, error: monitorsError, loadedAt, refetch: refetchMonitorList } = useAllMonitors()
  const { groups, refetch: refetchGroups } = useMonitorGroups()
  const { agents, loading: agentsLoading, error: agentsError, refetch: refetchAgents } = useAgents()
  const { devices, loading: devicesLoading, error: devicesError, refetch: refetchDevices } = useDevices()
  const { sites } = useSites()
  const { usernameFor } = useUsers()
  const { toasts, push } = useToasts()
  const sectionToggles = useRememberedToggles('sentinel:monitoringCollapsed')

  useEffect(() => {
    const t = window.setInterval(() => void refetchGroups(), GROUPS_REFRESH_MS)
    return () => window.clearInterval(t)
  }, [refetchGroups])

  const refetchMonitors = useCallback(async () => {
    await Promise.all([refetchMonitorList(), refetchGroups()])
  }, [refetchMonitorList, refetchGroups])

  // 24-hour uptime per monitor (each row's figure until its own loads, and
  // the "lowest uptime" sort) and 30-day incidents, fixed at mount.
  const windows = useMemo(() => {
    const end = new Date()
    return {
      day: new Date(end.getTime() - 24 * 3600e3).toISOString(),
      month: new Date(end.getTime() - 30 * 24 * 3600e3).toISOString(),
      end: end.toISOString(),
    }
  }, [])
  const { report: daySummary } = useSummaryReport(windows.day, windows.end)
  const { report: monthSummary } = useSummaryReport(windows.month, windows.end)
  const uptimeById = useMemo(
    () => new Map((daySummary?.monitors ?? []).map((r) => [r.monitor_id, r.uptime_percent] as const)),
    [daySummary]
  )

  const shownMonitors = useMemo(() => filterMonitors(monitors, filters), [monitors, filters])
  const shownAgents = useMemo(() => filterServers(agents, filters), [agents, filters])
  const shownDevices = useMemo(() => filterDevicesView(devices, filters), [devices, filters])
  const counts = useMemo(() => summaryCounts(monitors, agents, devices), [monitors, agents, devices])
  const siteOptions = useMemo(() => siteFilterOptions(sites, [...monitors, ...agents]), [sites, monitors, agents])
  const narrowed = filtersNarrow(filters)

  const [createOpen, setCreateOpen] = useState(false)
  const [groupModal, setGroupModal] = useState<{ mode: 'create' | 'edit'; group?: MonitorGroup } | null>(null)
  const [agentModal, setAgentModal] = useState<{ existing: CreatedAgent | null } | null>(null)
  const [addingDevice, setAddingDevice] = useState(false)

  const state: Record<Section, { loading: boolean; error: string | null; total: number; shown: number; canAdd: boolean }> = {
    uptime: { loading: monitorsLoading, error: monitorsError, total: monitors.length, shown: shownMonitors.length, canAdd: true },
    servers: { loading: agentsLoading, error: agentsError, total: agents.length, shown: shownAgents.length, canAdd: isAdmin },
    devices: { loading: devicesLoading, error: devicesError, total: devices.length, shown: shownDevices.length, canAdd: isAdmin },
  }
  const order = visibleSections(filters).filter((s) => sectionVisible({ ...state[s], narrowed }))
  const missing = (Object.keys(state) as Section[]).filter((s) => state[s].error).map((s) => SECTION_LABEL[s])
  const allLoaded = !monitorsLoading && !agentsLoading && !devicesLoading
  const nothingAtAll = allLoaded && missing.length === 0 && monitors.length + agents.length + devices.length === 0
  const nothingMatches = allLoaded && !nothingAtAll && order.length === 0

  return (
    <div className="space-y-6">
      <Toaster toasts={toasts} />

      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-4xl font-light text-white">Monitoring</h1>
          <p className="mt-2 text-sm text-slate-400">Uptime checks, servers and network devices in one place</p>
        </div>
        <AddMenu
          isAdmin={isAdmin}
          onMonitor={() => setCreateOpen(true)}
          onGroup={() => setGroupModal({ mode: 'create' })}
          onServer={() => setAgentModal({ existing: null })}
          onDevice={() => setAddingDevice(true)}
        />
      </div>

      <MonitoringToolbar
        filters={filters}
        onChange={setFilters}
        counts={{ uptime: monitors.length, servers: agents.length, devices: devices.length }}
        statuses={statusOptions(monitors, devices, filters.status)}
        sites={siteOptions}
        groups={groups}
      />

      <SummaryStrip counts={counts} missing={missing} onPick={(status) => setFilters({ ...filters, status })} />

      {nothingAtAll ? (
        <div className="rounded-lg border border-white/10 bg-slate-800/40 p-10 text-center text-sm text-slate-300">
          Nothing is being watched yet. Use <span className="font-medium text-white">Add</span> to start with an uptime monitor
          {isAdmin ? ', a server agent or a network device' : ''}.
        </div>
      ) : nothingMatches ? (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-white/10 bg-slate-800/40 p-8 text-center text-sm text-slate-400">
          Nothing matches these filters.
          <button className="btn-secondary" onClick={() => setFilters(NO_FILTERS)}>
            Clear filters
          </button>
        </div>
      ) : (
        <div className="space-y-8">
          {order.map((s) =>
            s === 'uptime' ? (
              <UptimeSection
                key="uptime"
                monitors={shownMonitors}
                all={monitors}
                groups={groups}
                filters={filters}
                onFilters={setFilters}
                narrowed={narrowed}
                uptimeById={uptimeById}
                incidents30d={monthSummary?.aggregate.total_incidents ?? null}
                usernameFor={usernameFor}
                refreshKey={loadedAt}
                collapsed={!!sectionToggles.on.uptime}
                onToggle={() => sectionToggles.toggle('uptime')}
                loading={monitorsLoading}
                error={monitorsError}
                onRetry={() => void refetchMonitors()}
                onChanged={() => void refetchMonitors()}
                onAdd={() => setCreateOpen(true)}
                onEditGroup={(group) => setGroupModal({ mode: 'edit', group })}
                push={push}
              />
            ) : s === 'servers' ? (
              <ServersSection
                key="servers"
                agents={shownAgents}
                total={agents.length}
                isAdmin={isAdmin}
                collapsed={!!sectionToggles.on.servers}
                onToggle={() => sectionToggles.toggle('servers')}
                loading={agentsLoading}
                error={agentsError}
                onRetry={() => void refetchAgents()}
                onChanged={() => void refetchAgents()}
                onAdd={() => setAgentModal({ existing: null })}
                onInstructions={(existing) => setAgentModal({ existing })}
                push={push}
              />
            ) : (
              <DevicesSection
                key="devices"
                devices={shownDevices}
                total={devices.length}
                isAdmin={isAdmin}
                filters={filters}
                onFilters={setFilters}
                collapsed={!!sectionToggles.on.devices}
                onToggle={() => sectionToggles.toggle('devices')}
                loading={devicesLoading}
                error={devicesError}
                onRetry={() => void refetchDevices()}
                onAdd={() => setAddingDevice(true)}
              />
            )
          )}
        </div>
      )}

      <CreateMonitorModal isOpen={createOpen} onClose={() => setCreateOpen(false)} onCreated={() => void refetchMonitors()} push={push} />
      {groupModal && (
        <GroupModal
          mode={groupModal.mode}
          group={groupModal.group}
          onClose={() => setGroupModal(null)}
          onSaved={() => void refetchMonitors()}
          push={push}
        />
      )}
      <AddServerAgentModal
        isOpen={agentModal !== null}
        existing={agentModal?.existing ?? null}
        onClose={() => setAgentModal(null)}
        onCreated={() => void refetchAgents()}
        push={push}
      />
      {addingDevice && (
        <DeviceFormModal
          siteId={filters.site && filters.site !== 'none' ? filters.site : undefined}
          onClose={() => setAddingDevice(false)}
          onSaved={() => {
            setAddingDevice(false)
            void refetchDevices()
          }}
        />
      )}
    </div>
  )
}
```

In `frontend/src/App.tsx`, add `const Monitoring = lazy(() => import('@/pages/Monitoring'))` beside the other page imports and, inside the authenticated layout next to the `/uptime` route, `<Route path="/monitoring" element={<Monitoring />} />`.

- [ ] **Step 6: Run the frontend gate**

Expected: no errors. Typical fixes: if `useUsers().usernameFor` or `useAgents().error` has a different type than the props above, adapt the prop type to the hook's (do not change the hook).

- [ ] **Step 7: Check it in a browser**

If a dev server or the dev stack is available, open `/monitoring` and confirm: the three sections render with today's columns plus Site; each collapses and stays collapsed after a reload; the type chips, status, site and group selects and search change the address and the rows; Back restores the previous view; "Down" in the strip filters to down rows. If no browser is available, say so in the task report — the owner checks in Task 7 cover it.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/hooks/useAllMonitors.ts frontend/src/hooks/useDebounced.ts frontend/src/hooks/useDismissOnOutsideClick.ts frontend/src/hooks/useRememberedToggles.ts frontend/src/components/monitoring frontend/src/pages/Monitoring.tsx frontend/src/App.tsx
git commit -m "feat(monitoring): one page for uptime checks, servers and devices

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Cut over to the Monitoring page

**Files:**
- Create: `frontend/src/components/MonitoringRedirect.tsx`
- Modify: `frontend/src/utils/navigation.ts` (the Monitor group), `frontend/src/App.tsx`, `frontend/src/pages/Overview.tsx`, `frontend/src/pages/MonitorDetail.tsx`, `frontend/src/pages/ServerDetail.tsx`, `frontend/src/pages/network/DeviceDetail.tsx`, `frontend/src/pages/network/PortDetail.tsx`, `frontend/src/pages/MonitorWizard.tsx`, `frontend/src/pages/BulkUpload.tsx`, `frontend/src/pages/NetworkDiscovery.tsx`, `docs/superpowers/STATUS.md`
- Delete: `frontend/src/pages/UptimeMonitoring.tsx`, `frontend/src/pages/Monitors.tsx`, `frontend/src/pages/ServerMonitoring.tsx`, `frontend/src/pages/network/Devices.tsx`
- Test: throwaway `check.ts` + the frontend gate

**Interfaces:**
- Consumes: Task 5's `monitoringPath`, `legacyMonitoringTarget`, `type Section`; Task 6's `/monitoring` route; piece 1's `NAV` and `isNavActive`.
- Produces: the sidebar Monitor group `Monitoring`, `Sites`, `SSL & Domains`; redirects for `/uptime`, `/monitors`, `/servers`, `/server-monitoring`, `/network/devices`.

- [ ] **Step 1: Write the failing check**

`$CHECK/check.ts`:

```ts
import { NAV, isNavActive } from '/app/src/utils/navigation.ts'

let failures = 0
function expect(name: string, got: unknown, want: unknown) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures++
    console.log(`FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`)
  }
}

const monitor = NAV.find((g) => g.label === 'Monitor')
expect('monitor group', monitor?.items.map((i) => i.label), ['Monitoring', 'Sites', 'SSL & Domains'])
const entry = monitor!.items[0]
expect('opens', entry.to, '/monitoring')
for (const path of ['/monitoring', '/monitors/abc', '/monitors/new/wizard', '/servers/srv-1', '/network/devices/7', '/network/devices/7/ports/3']) {
  expect(`active on ${path}`, isNavActive(path, entry), true)
}
for (const path of ['/network/sites', '/network/sites/1', '/ssl', '/monitoringx']) {
  expect(`not active on ${path}`, isNavActive(path, entry), false)
}
const labels = NAV.flatMap((g) => g.items.map((i) => i.label))
expect('old entries gone', ['Uptime', 'Servers', 'Devices'].filter((l) => labels.includes(l)), [])

if (failures) {
  console.log(`${failures} check(s) failed`)
  process.exit(1)
}
console.log('navigation: all checks passed')
```

- [ ] **Step 2: Run it to verify it fails**

Run the throwaway check command.
Expected: `FAIL monitor group: got ["Uptime","Servers","Devices","Sites","SSL & Domains"] ...` and further failures, exit 1.

- [ ] **Step 3: Change the sidebar**

In `frontend/src/utils/navigation.ts`, replace the Uptime, Servers and Devices entries of the Monitor group with one entry:

```ts
      // One list for everything Sentinel watches; detail pages keep their
      // own addresses, so they are matched here too.
      { to: '/monitoring', label: 'Monitoring', match: ['/monitoring', '/monitors', '/servers', '/network/devices'] },
```

Update `isNavActive`'s comment example: "so /monitoringX does not light up Monitoring".

- [ ] **Step 4: Run the check to verify it passes**

Run the throwaway check command.
Expected: `navigation: all checks passed`.

- [ ] **Step 5: Redirects and deleted pages**

`frontend/src/components/MonitoringRedirect.tsx`:

```tsx
import { Navigate, useLocation } from 'react-router-dom'
import { legacyMonitoringTarget, type Section } from '@/utils/monitoringView'

/** MonitoringRedirect sends an old list address (/uptime, /servers,
 *  /network/devices) to the Monitoring page on that section, keeping its
 *  query and hash, so old bookmarks and links keep working. */
export default function MonitoringRedirect({ show }: { show: Section }) {
  const location = useLocation()
  return <Navigate to={legacyMonitoringTarget(location, show)} replace />
}
```

In `frontend/src/App.tsx`:
- Remove the lazy imports of `UptimeMonitoring`, `Monitors`, `ServerMonitoring` and `Devices`; import `MonitoringRedirect from '@/components/MonitoringRedirect'`.
- Replace these routes:

```tsx
              <Route path="/uptime" element={<MonitoringRedirect show="uptime" />} />
              <Route path="/monitors" element={<MonitoringRedirect show="uptime" />} />
              <Route path="/servers" element={<MonitoringRedirect show="servers" />} />
              <Route path="/server-monitoring" element={<MonitoringRedirect show="servers" />} />
              <Route path="/network/devices" element={<MonitoringRedirect show="devices" />} />
```

(the `/servers/:agentID`, `/network/devices/:id` and `/network/devices/:id/ports/:ifIndex` routes stay). Update the comment above `/server-monitoring` to say the old list addresses open the Monitoring page.

Delete the four old pages:

```bash
git rm frontend/src/pages/UptimeMonitoring.tsx frontend/src/pages/Monitors.tsx frontend/src/pages/ServerMonitoring.tsx frontend/src/pages/network/Devices.tsx
```

- [ ] **Step 6: Move in-app links to the new addresses**

Each file imports `monitoringPath` from `'@/utils/monitoringView'`.

- `pages/Overview.tsx`: the uptime card `to: '/uptime'` → `to: monitoringPath('uptime')`; the servers card `to: '/servers'` → `to: monitoringPath('servers')`.
- `pages/MonitorDetail.tsx`: every `navigate('/uptime')` (four) → `navigate(monitoringPath('uptime'))`; both `to="/uptime"` → `to={monitoringPath('uptime')}`; "Back to Monitors" → "Back to Monitoring"; breadcrumb "Uptime Monitoring" → "Monitoring".
- `pages/ServerDetail.tsx`: `navigate('/servers')` → `navigate(monitoringPath('servers'))`; every `to="/servers"` (three) → `to={monitoringPath('servers')}`; both "Back to Servers" → "Back to Monitoring"; breadcrumb "Server Monitoring" → "Monitoring".
- `pages/network/DeviceDetail.tsx`: `to="/network/devices"` → `to={monitoringPath('devices')}`; "Back to devices" → "Back to Monitoring".
- `pages/network/PortDetail.tsx`: the fallback `'/network/devices'` → `monitoringPath('devices')`.
- `pages/MonitorWizard.tsx`, `pages/BulkUpload.tsx`, `pages/NetworkDiscovery.tsx`: every `navigate('/uptime')` (two each) → `navigate(monitoringPath('uptime'))`.

Then confirm nothing still links the old list addresses:

Run: `grep -rnE "'/uptime'|\"/uptime\"|'/servers'|\"/servers\"|'/network/devices'|\"/network/devices\"" frontend/src --include=*.tsx --include=*.ts`
Expected: only the redirect routes in `App.tsx`.

- [ ] **Step 7: Run the frontend gate**

Expected: no errors (an unused-import error means a deleted page's component was still imported somewhere — remove that import).

- [ ] **Step 8: Record it in STATUS.md**

In `docs/superpowers/STATUS.md`, set the UX reorganization table's piece 2 row's state to `Done (\`feature/ux-piece2\`, awaiting merge to \`dev\`); plan \`plans/2026-10-07-ux-piece2-monitoring-list.md\``, and under "To be checked by the owner" add a section "UX reorganization piece 2 (on the dev stack)" with the nine owner checks copied from the spec's "Owner checks (on the dev stack)".

- [ ] **Step 9: Commit**

```bash
git add -A frontend/src docs/superpowers/STATUS.md
git commit -m "feat(monitoring): Monitoring replaces the Uptime, Servers and Devices pages

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 10: Whole-branch verification**

Run from `backend/`: `go vet ./...`, `go test ./...`, `./scripts/test-db.sh`. Run the frontend gate.
Expected: all PASS.
