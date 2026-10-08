# UX piece 4: public dashboards — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A dashboard can be published at a readable address (`/public/status/<name>`) and/or its unguessable link, by admins only, branded with a logo and theme colour, and carry a Service status widget that does what status pages did. Status pages are removed.

**Architecture:** The dashboards package gains the address (name, switch, publisher) and branding columns, a `public` flag (address live or link present) that replaces "published" as the content lock, a slug resolver mirroring the token resolver, and a `service_status` widget. 90-day bars are counted per day in SQL. The public API serves both `/public/dashboards/:token` and `/public/status/:slug` through one pair of handlers. On the frontend, one public page renders both ways with a branded header. A Publishing panel holds the address and the link. The editor gains Appearance. The status page code, routes, tiles and tables go away (migration 062 refuses to drop a non-empty `status_pages`).

**Tech Stack:** Go 1.26, Gin, GORM (pgx), PostgreSQL 16 + TimescaleDB; React 18 + TypeScript + Vite + Tailwind.

**Spec:** `docs/superpowers/specs/2026-10-08-ux-piece4-public-dashboards-design.md`

## Global Constraints

- Work on branch `feature/ux-piece4-public-dashboards` in the worktree `.claude/worktrees/ux-piece4`, made from `dev`. Never push or merge; the owner asks for that separately.
- Migration file: `backend/migrations/062_public_dashboards.sql`. Confirm `ls backend/migrations | tail -1` prints `061_site_profiles.sql` before writing it. Never edit an applied migration.
- Address name rule: 3–50 characters, `^[a-zA-Z0-9-]+$`, unique across Sentinel, and compared as stored (case-sensitive).
- Exact wording:
  - "the address may only use letters, digits and hyphens (3–50)"
  - "that address is already used by another dashboard"
  - "choose an address first"
  - "this dashboard is published; ask an admin to change it" (the existing `ErrPublished`)
  - the migration guard: "N status pages still exist; delete or convert them before upgrading"
  - the banner: "All systems operational" and "N of M systems experiencing issues"
  - an empty public dashboard: "Nothing to show yet"
- Logo: an `http://` or `https://` address of at most 2048 characters, or empty. Theme colour: `#rgb` or `#rrggbb`, or empty.
- Public routes:
  - both route families are limited per IP to 600 per minute, burst 120;
  - every response sends `Cache-Control: no-store`;
  - widget data shares the 15 s cache (`ResolvePublic`);
  - responses carry no internal ids except widget ids;
  - an unknown, switched-off or orphaned address answers 404 with one message.
- Service status:
  - at most 20 groups and 100 monitors in total;
  - a group name is at most 100 characters;
  - a monitor appears at most once;
  - `show_banner` and `show_uptime` default to true;
  - the period is always 90 days, and the widget refreshes every 60 s.
- Only admins publish: the routes sit behind `RequireAdmin`, and the service checks `LevelManage`. Any Gin middleware that refuses must abort.
- New timestamps use `time.Now().UTC()`.
- Frontend rules:
  - colours come from `src/utils/colors.ts`; the only hex outside it is the user's stored theme colour, applied as an inline style;
  - component files export only components, and helpers live in `src/utils/`;
  - no `window.confirm`/`alert`;
  - overlays render through `createPortal(…, document.body)`;
  - guard async results so a late response cannot land after the inputs changed;
  - never write `*/` inside a CSS comment;
  - never run npm as root.
- Checks:
  - Backend: `cd backend && go vet ./... && go test ./...`, and `gofmt -l` on every changed Go file must print nothing. DB tests use `./scripts/test-db.sh -run <Name> -v`, which runs `go test <args> ./...` against a throwaway TimescaleDB.
  - Frontend gate (from the repo root): `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`.
  - Pure frontend helpers are checked with a throwaway `check.ts` kept outside the repo. It is bundled with the frontend's esbuild: `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -v "<check dir>":/check -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx esbuild /check/check.ts --bundle --platform=node --outfile=/tmp/check.cjs --log-level=warning && node /tmp/check.cjs"`.
- Every commit message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A visitor at phone width** opens `/public/status/<name>`. The branded header (logo, a long name, a description) wraps instead of truncating. The clock and the Fullscreen button sit below the name rather than pushing it off screen, and the widgets stack full width. Pinned by: Task 7's header uses `flex-wrap`, and owner check 3 (written in Task 9) is done at phone width too.
2. **A logo address that does not give a picture** (a 404, mixed content blocked on https, an HTML page): the header shows no broken-image icon, in the public view and in the editor preview. It recovers as soon as the address is corrected. Pinned by: `PublicHeader`'s `Logo` keyed by the address with an `onError` hide (Task 7), and `validLogoUrl` in Task 6's check.
3. **A stored short colour (`#1b9`)** in the editor: `<input type="color">` accepts only `#rrggbb`. The picker must show the same colour, and choosing one must fill the text field. Pinned by: `longHex` in Task 6's check, used by the editor in Task 8.
4. **An editor who is not an admin has the editor open when an admin publishes the dashboard.** Save answers 403 "this dashboard is published; ask an admin to change it". The editor shows it as a message and keeps the draft. Pinned by: Task 2's `TestDBLiveAddressLocksContent` (the backend side). The editor already shows a non-409 error's message.
5. **A public status page left open for hours while its address is renamed or switched off.** By the next 5-minute layout fetch, the page shows "This page is not available." instead of old data. Pinned by: Task 4's `TestPublicStatusRoutes` (404 on every request once the address is dead) and Task 7's `gone` handling, where a 404 on the layout is final.

## Plan rulings

These are the calls this plan makes where the spec is silent or a literal reading would misfire.

- **The `published` field becomes two fields.** `DashboardView.Published` (JSON `published`) is replaced by `HasLink` (`has_link`: the link exists) and `Public` (`public`: the address is live or the link exists). `Public` is the content lock. Every frontend reader moves to `public` in Task 8.
- **Appearance lives in the dashboard editor and is saved with the dashboard** (`PUT /dashboards/:id`). The logo and colour are content, so the existing lock covers them (an editor may change them unless the dashboard is public). `SaveInput.LogoURL` and `ThemeColor` are pointers: nil leaves them as they are.
- **Deleted monitors are dropped by the widget's `Validate`, on the server, on every save and preview.** This is what the spec means by "the editor drops them the next time the widget is saved". It does not depend on the editor's monitor list being complete.
- **A monitor listed twice is refused** with a 400, not silently dropped, because there is no right group to keep it in. The editor hides monitors that other groups already use.
- **"Experiencing issues" counts the shown monitors whose current status is not `online`**, following the spec's definition. A paused monitor shows the status "Paused" but is counted by its current status.
- **The 90-day bars come from per-day counts in SQL** (`CheckService.DailyCheckCounts`), so 100 monitors refreshing every 60 s never load every check row. `DailyUptimeBuckets` shares the bucket rule through `DailyBucketsFromCounts`.
- **The 404 message for a dead address is "This page is not available".** It is the same for unknown, switched off, and orphaned addresses.
- **Switching an address off clears its publisher, and switching it on records the admin who did it.** Renaming a live address re-records the publisher.
- **The two public route families get separate rate limiters.** Each allows 600 per minute per IP, with a burst of 120.
- **A group with no monitors left for the viewer is left out of the response.**
- **`/dashboards?new=1` goes away.** Only the Status pages tab used it.
- **The Overview tile subtitle reads "N public".**

---

### Task 1: Migration 062 and the dashboard columns

**Files:**
- Create: `backend/migrations/062_public_dashboards.sql`
- Create: `backend/internal/database/public_dashboards_migration_db_test.go`
- Create: `backend/internal/dashboards/address_schema_db_test.go`
- Create: `backend/internal/models/dashboard_json_test.go`
- Modify: `backend/internal/models/dashboard.go` (limits block, `Dashboard` struct)

**Interfaces:**
- Produces:
  - columns `dashboards.public_slug VARCHAR(50)` (unique), `public_slug_live BOOLEAN NOT NULL DEFAULT false`, `public_slug_by UUID` (FK users, ON DELETE SET NULL), `logo_url VARCHAR(2048) NOT NULL DEFAULT ''`, `theme_color VARCHAR(7) NOT NULL DEFAULT ''`;
  - `models.Dashboard` fields `PublicSlug *string` (json `public_slug`), `PublicSlugLive bool` (`public_slug_live`), `PublicSlugBy *uuid.UUID` (json `-`), `LogoURL string` (`logo_url`), `ThemeColor string` (`theme_color`);
  - constants `models.MinPublicSlugLen = 3`, `models.MaxPublicSlugLen = 50`, `models.MaxLogoURLLen = 2048`;
  - no `status_pages` or `status_page_monitors` tables.

- [ ] **Step 1: Confirm the migration number**

Run: `ls backend/migrations | tail -1`
Expected: `061_site_profiles.sql`. If it prints anything else, use the next number after it everywhere this task says 062.

- [ ] **Step 2: Write the failing tests**

`backend/internal/database/public_dashboards_migration_db_test.go`:

```go
package database_test

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/database"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// hasColumns reports whether table exists (column "") or has column.
func hasColumns(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	q := `SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ?`
	args := []any{table}
	if column != "" {
		q += ` AND column_name = ?`
		args = append(args, column)
	}
	var n int64
	testdb.Must(t, db.Raw(q, args...).Scan(&n).Error)
	return n > 0
}

// rollBack062 puts the database back as it was before 062, with status page
// tables like the ones 001 made, and forgets that 062 ran.
func rollBack062(t *testing.T, db *gorm.DB) {
	t.Helper()
	testdb.Exec(t, db, `ALTER TABLE dashboards DROP COLUMN public_slug, DROP COLUMN public_slug_live,
		DROP COLUMN public_slug_by, DROP COLUMN logo_url, DROP COLUMN theme_color`)
	testdb.Exec(t, db, `CREATE TABLE status_pages (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		slug VARCHAR(255) UNIQUE NOT NULL,
		name VARCHAR(255) NOT NULL)`)
	testdb.Exec(t, db, `CREATE TABLE status_page_monitors (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		status_page_id UUID NOT NULL REFERENCES status_pages (id) ON DELETE CASCADE,
		monitor_id UUID NOT NULL REFERENCES monitors (id) ON DELETE CASCADE)`)
	testdb.Exec(t, db, `DELETE FROM schema_migrations WHERE filename = '062_public_dashboards.sql'`)
}

// 062 refuses, changing nothing, while any status page exists.
func TestDBMigration062RefusesWhileStatusPagesExist(t *testing.T) {
	db := testdb.Open(t)
	rollBack062(t, db)
	testdb.Exec(t, db, `INSERT INTO status_pages (slug, name) VALUES ('main', 'Main'), ('ops', 'Ops')`)

	err := database.RunMigrations(db, testdb.MigrationsDir())
	if err == nil || !strings.Contains(err.Error(), "2 status pages still exist; delete or convert them before upgrading") {
		t.Fatalf("migrating with two status pages: err = %v, want the status page guard", err)
	}
	if !hasColumns(t, db, "status_pages", "") || !hasColumns(t, db, "status_page_monitors", "") {
		t.Error("the guard fired but the status page tables were dropped")
	}
	if hasColumns(t, db, "dashboards", "logo_url") {
		t.Error("the guard fired but the dashboard columns were added: the migration was not all-or-nothing")
	}
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM schema_migrations WHERE filename = '062_public_dashboards.sql'`).Scan(&n).Error)
	if n != 0 {
		t.Error("062 is recorded as applied although it was refused")
	}
}

// With no status pages, 062 drops their tables and adds the dashboard columns.
func TestDBMigration062DropsEmptyStatusPages(t *testing.T) {
	db := testdb.Open(t)
	rollBack062(t, db)
	testdb.Must(t, database.RunMigrations(db, testdb.MigrationsDir()))
	if hasColumns(t, db, "status_pages", "") || hasColumns(t, db, "status_page_monitors", "") {
		t.Error("the status page tables are still there")
	}
	for _, col := range []string{"public_slug", "public_slug_live", "public_slug_by", "logo_url", "theme_color"} {
		if !hasColumns(t, db, "dashboards", col) {
			t.Errorf("dashboards.%s is missing", col)
		}
	}
}
```

`backend/internal/dashboards/address_schema_db_test.go`:

```go
package dashboards

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// Addresses are unique as stored, and deleting the admin who published one
// keeps the address but clears its publisher (so it stops answering).
func TestDBPublicSlugUniqueAndPublisherCleared(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	publisher := testdb.NewUser(t, db, true)
	a := newDashboard(t, db, "A", nil, &owner)
	b := newDashboard(t, db, "B", nil, &owner)
	testdb.Exec(t, db, `UPDATE dashboards SET public_slug = 'main', public_slug_live = true, public_slug_by = ? WHERE id = ?`, publisher, a)
	if err := db.Exec(`UPDATE dashboards SET public_slug = 'main' WHERE id = ?`, b).Error; err == nil {
		t.Error("two dashboards took the address main")
	}
	if err := db.Exec(`UPDATE dashboards SET public_slug = 'Main' WHERE id = ?`, b).Error; err != nil {
		t.Errorf("addresses are compared as stored, but Main was refused next to main: %v", err)
	}

	testdb.Exec(t, db, `DELETE FROM users WHERE id = ?`, publisher)
	var row struct {
		PublicSlug     *string
		PublicSlugLive bool
		PublicSlugBy   *uuid.UUID
		LogoURL        string
		ThemeColor     string
	}
	testdb.Must(t, db.Raw(`SELECT public_slug, public_slug_live, public_slug_by, logo_url, theme_color FROM dashboards WHERE id = ?`, a).Scan(&row).Error)
	if row.PublicSlug == nil || *row.PublicSlug != "main" || !row.PublicSlugLive || row.PublicSlugBy != nil {
		t.Errorf("after deleting the publisher: %+v, want the address kept and its publisher cleared", row)
	}
	if row.LogoURL != "" || row.ThemeColor != "" {
		t.Errorf("a new dashboard has branding %q / %q, want none", row.LogoURL, row.ThemeColor)
	}
}
```

`backend/internal/models/dashboard_json_test.go`:

```go
package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Who published an address is never sent to the browser.
func TestDashboardJSONHidesPublisher(t *testing.T) {
	by := uuid.New()
	slug := "main"
	raw, err := json.Marshal(Dashboard{PublicSlug: &slug, PublicSlugLive: true, PublicSlugBy: &by, LogoURL: "https://x.test/l.png", ThemeColor: "#123"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, by.String()) || strings.Contains(s, "public_slug_by") {
		t.Errorf("dashboard JSON carries its publisher: %s", s)
	}
	for _, want := range []string{`"public_slug":"main"`, `"public_slug_live":true`, `"logo_url":"https://x.test/l.png"`, `"theme_color":"#123"`} {
		if !strings.Contains(s, want) {
			t.Errorf("dashboard JSON %s lacks %s", s, want)
		}
	}
}
```

- [ ] **Step 3: Run the tests to see them fail**

Run: `cd backend && go test ./internal/models/ -run TestDashboardJSONHidesPublisher`
Expected: FAIL to compile with `unknown field PublicSlug in struct literal`.

Run: `cd backend && ./scripts/test-db.sh -run 'TestDBMigration062|TestDBPublicSlugUniqueAndPublisherCleared' -v`
Expected: FAIL. The first exec fails with `column "public_slug" of relation "dashboards" does not exist`.

- [ ] **Step 4: Write the migration**

`backend/migrations/062_public_dashboards.sql`:

```sql
-- 062_public_dashboards.sql
-- UX piece 4 (spec 2026-10-08-ux-piece4-public-dashboards-design.md): a
-- dashboard can be published at a readable address (/public/status/<name>)
-- and carries a logo and a theme colour; status pages are removed (there are
-- none to convert).

-- Refuse, changing nothing, while any status page exists. This file runs as
-- one transaction, so the exception undoes everything in it.
DO $$
DECLARE
    n bigint;
BEGIN
    IF to_regclass('public.status_pages') IS NOT NULL THEN
        EXECUTE 'SELECT count(*) FROM status_pages' INTO n;
        IF n > 0 THEN
            RAISE EXCEPTION '% status pages still exist; delete or convert them before upgrading', n;
        END IF;
    END IF;
END $$;

-- The address name is unique as stored. public_slug_by is the admin who
-- switched the address on; deleting that user clears it, and the address then
-- answers "not available" until an admin publishes it again. No CHECK ties
-- the columns together, so ON DELETE SET NULL always succeeds.
ALTER TABLE dashboards ADD COLUMN IF NOT EXISTS public_slug VARCHAR(50);
ALTER TABLE dashboards ADD COLUMN IF NOT EXISTS public_slug_live BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE dashboards ADD COLUMN IF NOT EXISTS public_slug_by UUID REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE dashboards ADD COLUMN IF NOT EXISTS logo_url VARCHAR(2048) NOT NULL DEFAULT '';
ALTER TABLE dashboards ADD COLUMN IF NOT EXISTS theme_color VARCHAR(7) NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_dashboards_public_slug ON dashboards (public_slug);

DROP TABLE IF EXISTS status_page_monitors;
DROP TABLE IF EXISTS status_pages;
```

- [ ] **Step 5: Add the model fields**

In `backend/internal/models/dashboard.go`, below the `const (…)` block of dashboard limits, add:

```go
// Public address and branding limits (spec 2026-10-08 piece 4): the rules
// status pages followed.
const (
	MinPublicSlugLen = 3
	MaxPublicSlugLen = 50
	MaxLogoURLLen    = 2048
)
```

In the `Dashboard` struct, insert these fields after `CreatedBy`:

```go
	// PublicSlug is the readable public address (/public/status/<slug>), nil
	// for none. It answers only while PublicSlugLive is on and PublicSlugBy,
	// the admin who switched it on, is still an admin.
	PublicSlug     *string    `json:"public_slug" gorm:"column:public_slug"`
	PublicSlugLive bool       `json:"public_slug_live" gorm:"column:public_slug_live;not null"`
	PublicSlugBy   *uuid.UUID `json:"-" gorm:"column:public_slug_by;type:uuid"`
	// LogoURL and ThemeColor brand the public view ("" for none).
	LogoURL    string `json:"logo_url" gorm:"column:logo_url;not null"`
	ThemeColor string `json:"theme_color" gorm:"column:theme_color;not null"`
```

- [ ] **Step 6: Run the tests to see them pass**

Run: `cd backend && go vet ./... && go test ./internal/models/`
Expected: PASS.

Run: `cd backend && ./scripts/test-db.sh -run 'TestDBMigration|TestDBPublicSlugUniqueAndPublisherCleared|TestDBDashboard' -v`
Expected: PASS. This includes the existing `TestDBMigrationsApplyAndAreIdempotent` and the dashboard schema tests.

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/062_public_dashboards.sql backend/internal/models/dashboard.go \
  backend/internal/models/dashboard_json_test.go backend/internal/database/public_dashboards_migration_db_test.go \
  backend/internal/dashboards/address_schema_db_test.go
git commit -m "feat(dashboards): public address and branding columns; status page tables dropped (guarded)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: The public flag and the public address

**Files:**
- Create: `backend/internal/dashboards/address.go`
- Create: `backend/internal/dashboards/address_db_test.go`
- Modify: `backend/internal/dashboards/errors.go` (three errors)
- Modify: `backend/internal/dashboards/service.go` (`DashboardView`, `dashboardRow`, `dashboardSelect`, `view`, `contentError`)
- Modify: `backend/internal/dashboards/access.go` (`canChangeContent` doc)
- Modify: `backend/internal/dashboards/service_db_test.go:158-162`

**Interfaces:**
- Consumes (Task 1): the `models.Dashboard` address fields and `models.MinPublicSlugLen`/`MaxPublicSlugLen`.
- Produces:
  - `type AddressInput struct` with `Slug string` (json `slug`) and `Live bool` (json `live`);
  - `func (s *Service) SetPublicAddress(ctx context.Context, v Viewer, id uuid.UUID, in AddressInput) (*DashboardDetail, error)`, which returns ErrNotFound, ErrForbidden, ErrSlugRule, ErrSlugTaken or ErrSlugFirst;
  - `func (s *Service) ResolveSlug(ctx context.Context, slug string) (*models.Dashboard, error)` (ErrNotFound for any dead address);
  - `var ErrSlugRule, ErrSlugTaken, ErrSlugFirst error`;
  - `DashboardView.HasLink bool` (json `has_link`) and `DashboardView.Public bool` (json `public`), replacing `Published`.

- [ ] **Step 1: Write the failing tests**

`backend/internal/dashboards/address_db_test.go`:

```go
package dashboards

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBPublicAddressRules(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	admin := w.viewer(w.admin)

	for _, bad := range []string{"ab", strings.Repeat("a", 51), "has space", "slash/es", "dots.no", "ünï-x"} {
		if _, err := w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{Slug: bad, Live: true}); !errors.Is(err, ErrSlugRule) {
			t.Errorf("address %q err = %v, want ErrSlugRule", bad, err)
		}
	}
	if _, err := w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{Slug: "  ", Live: true}); !errors.Is(err, ErrSlugFirst) {
		t.Errorf("switching on with no name err = %v, want ErrSlugFirst", err)
	}

	d, err := w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{Slug: " hq-status ", Live: true})
	testdb.Must(t, err)
	if d.PublicSlug == nil || *d.PublicSlug != "hq-status" || !d.PublicSlugLive || !d.Public || d.HasLink {
		t.Errorf("after publishing: slug=%v live=%v public=%v has_link=%v; want hq-status, true, true, false", d.PublicSlug, d.PublicSlugLive, d.Public, d.HasLink)
	}
	var by *uuid.UUID
	testdb.Must(t, w.db.Raw(`SELECT public_slug_by FROM dashboards WHERE id = ?`, w.siteDash).Scan(&by).Error)
	if by == nil || *by != w.admin {
		t.Errorf("public_slug_by = %v, want the admin %s", by, w.admin)
	}

	if _, err := w.svc.SetPublicAddress(ctx, admin, w.personal, AddressInput{Slug: "hq-status"}); !errors.Is(err, ErrSlugTaken) {
		t.Errorf("a second dashboard taking hq-status err = %v, want ErrSlugTaken", err)
	}
	if _, err := w.svc.SetPublicAddress(ctx, admin, w.personal, AddressInput{Slug: "HQ-status"}); err != nil {
		t.Errorf("HQ-status next to hq-status: %v (names are compared as stored)", err)
	}
	if _, err := w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{Slug: "hq-status", Live: true}); err != nil {
		t.Errorf("re-saving a dashboard's own address: %v", err)
	}

	d, err = w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{Slug: "hq-status", Live: false})
	testdb.Must(t, err)
	if d.PublicSlug == nil || *d.PublicSlug != "hq-status" || d.PublicSlugLive || d.Public {
		t.Errorf("switched off: slug=%v live=%v public=%v, want the name kept and the dashboard not public", d.PublicSlug, d.PublicSlugLive, d.Public)
	}
	testdb.Must(t, w.db.Raw(`SELECT public_slug_by FROM dashboards WHERE id = ?`, w.siteDash).Scan(&by).Error)
	if by != nil {
		t.Errorf("switched off but public_slug_by = %v, want none", by)
	}

	d, err = w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{})
	testdb.Must(t, err)
	if d.PublicSlug != nil {
		t.Errorf("a cleared address is still %q", *d.PublicSlug)
	}
	if _, err := w.svc.SetPublicAddress(ctx, admin, w.personal, AddressInput{Slug: "hq-status"}); err != nil {
		t.Errorf("a cleared address could not be reused: %v", err)
	}
	// Deleting a dashboard frees its address.
	_, err = w.svc.Delete(ctx, admin, w.personal)
	testdb.Must(t, err)
	if _, err := w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{Slug: "hq-status", Live: true}); err != nil {
		t.Errorf("the address of a deleted dashboard could not be reused: %v", err)
	}
}

func TestDBPublicAddressIsAdminOnly(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	for _, user := range []uuid.UUID{w.siteRW, w.siteRO} {
		if _, err := w.svc.SetPublicAddress(ctx, w.viewer(user), w.siteDash, AddressInput{Slug: "hq-wall", Live: true}); !errors.Is(err, ErrForbidden) {
			t.Errorf("non-admin %s err = %v, want ErrForbidden", user, err)
		}
	}
	if _, err := w.svc.SetPublicAddress(ctx, w.viewer(w.stranger), w.siteDash, AddressInput{Slug: "hq-wall", Live: true}); !errors.Is(err, ErrNotFound) {
		t.Errorf("stranger err = %v, want ErrNotFound", err)
	}
	var n int64
	testdb.Must(t, w.db.Raw(`SELECT count(*) FROM dashboards WHERE public_slug IS NOT NULL`).Scan(&n).Error)
	if n != 0 {
		t.Errorf("%d dashboards got an address from non-admins", n)
	}
}

func TestDBResolveSlug(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	admin := w.viewer(w.admin)
	_, err := w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{Slug: "hq-status", Live: true})
	testdb.Must(t, err)

	d, err := w.svc.ResolveSlug(ctx, "hq-status")
	testdb.Must(t, err)
	if d.ID != w.siteDash || d.Name != "HQ overview" {
		t.Errorf("hq-status resolves to %s %q, want the HQ dashboard", d.ID, d.Name)
	}
	for _, slug := range []string{"HQ-STATUS", "nope-nope", "", "x' OR '1'='1"} {
		if _, err := w.svc.ResolveSlug(ctx, slug); !errors.Is(err, ErrNotFound) {
			t.Errorf("ResolveSlug(%q) err = %v, want ErrNotFound", slug, err)
		}
	}

	// Renaming moves the address at once.
	_, err = w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{Slug: "hq-wall", Live: true})
	testdb.Must(t, err)
	if _, err := w.svc.ResolveSlug(ctx, "hq-status"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the old address after a rename err = %v, want ErrNotFound", err)
	}
	if _, err := w.svc.ResolveSlug(ctx, "hq-wall"); err != nil {
		t.Errorf("the new address after a rename: %v", err)
	}

	_, err = w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{Slug: "hq-wall", Live: false})
	testdb.Must(t, err)
	if _, err := w.svc.ResolveSlug(ctx, "hq-wall"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a switched-off address err = %v, want ErrNotFound", err)
	}

	_, err = w.svc.SetPublicAddress(ctx, admin, w.siteDash, AddressInput{Slug: "hq-wall", Live: true})
	testdb.Must(t, err)
	testdb.Exec(t, w.db, `UPDATE users SET is_admin = false, role = 'user' WHERE id = ?`, w.admin)
	if _, err := w.svc.ResolveSlug(ctx, "hq-wall"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the address of a demoted admin err = %v, want ErrNotFound on the very next request", err)
	}
}

func TestDBResolveSlugPublisherDeleted(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	other := testdb.NewUser(t, w.db, true)
	_, err := w.svc.SetPublicAddress(ctx, Viewer{UserID: other, IsAdmin: true}, w.siteDash, AddressInput{Slug: "hq-status", Live: true})
	testdb.Must(t, err)
	testdb.Exec(t, w.db, `DELETE FROM users WHERE id = ?`, other)
	if _, err := w.svc.ResolveSlug(ctx, "hq-status"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the address of a deleted admin err = %v, want ErrNotFound", err)
	}
}

// A live address locks the content like a link does; switched off with no
// link, the editor may change it again.
func TestDBLiveAddressLocksContent(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	_, err := w.svc.SetPublicAddress(ctx, w.viewer(w.admin), w.siteDash, AddressInput{Slug: "hq-status", Live: true})
	testdb.Must(t, err)
	in := SaveInput{Version: 1, Name: "HQ overview"}
	if _, err := w.svc.Save(ctx, w.viewer(w.siteRW), w.siteDash, in); !errors.Is(err, ErrPublished) {
		t.Errorf("editor saving while the address is live err = %v, want ErrPublished", err)
	}
	if _, err := w.svc.Delete(ctx, w.viewer(w.siteRW), w.siteDash); !errors.Is(err, ErrPublished) {
		t.Errorf("editor deleting while the address is live err = %v, want ErrPublished", err)
	}
	d, err := w.svc.Get(ctx, w.viewer(w.siteRW), w.siteDash)
	testdb.Must(t, err)
	if !d.Public || d.HasLink || d.CanEdit {
		t.Errorf("public=%v has_link=%v can_edit=%v for an editor, want true/false/false", d.Public, d.HasLink, d.CanEdit)
	}
	if _, err := w.svc.Save(ctx, w.viewer(w.admin), w.siteDash, in); err != nil {
		t.Errorf("an admin saving a public dashboard: %v", err)
	}

	_, err = w.svc.SetPublicAddress(ctx, w.viewer(w.admin), w.siteDash, AddressInput{Slug: "hq-status", Live: false})
	testdb.Must(t, err)
	if _, err := w.svc.Save(ctx, w.viewer(w.siteRW), w.siteDash, SaveInput{Version: 2, Name: "HQ overview"}); err != nil {
		t.Errorf("editor saving once the address is off and there is no link: %v", err)
	}
}
```

In `backend/internal/dashboards/service_db_test.go`, replace the check after `publish(...)` in `TestDBDelete` (lines 158-162):

```go
	d, err := w.svc.Get(ctx, w.viewer(w.siteRW), w.siteDash)
	testdb.Must(t, err)
	if !d.HasLink || !d.Public || d.CanEdit {
		t.Errorf("has_link=%v public=%v can_edit=%v for an editor of a dashboard with a link, want true/true/false", d.HasLink, d.Public, d.CanEdit)
	}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `cd backend && go vet ./internal/dashboards/`
Expected: FAIL with `undefined: AddressInput`, `w.svc.SetPublicAddress undefined`, and `d.HasLink undefined`.

- [ ] **Step 3: Add the errors**

In `backend/internal/dashboards/errors.go`, inside the `var (…)` block after `ErrNoLink`:

```go
	// ErrSlugRule, ErrSlugTaken and ErrSlugFirst: a public address that
	// cannot be set, in the spec's wording.
	ErrSlugRule  = errors.New("the address may only use letters, digits and hyphens (3–50)")
	ErrSlugTaken = errors.New("that address is already used by another dashboard")
	ErrSlugFirst = errors.New("choose an address first")
```

- [ ] **Step 4: Replace "published" with "public" in the service**

In `backend/internal/dashboards/service.go`:

Replace the `Published bool \`json:"published"\`` field of `DashboardView` with:

```go
	// HasLink: the dashboard has an unguessable public link.
	HasLink bool `json:"has_link"`
	// Public: its address is live or it has a link. While it is public, only
	// admins change its content.
	Public bool `json:"public"`
```

Replace `Published bool \`gorm:"column:published"\`` in `dashboardRow` with:

```go
	HasLink         bool   `gorm:"column:has_link"`
	IsPublic        bool   `gorm:"column:is_public"`
```

Replace `dashboardSelect` with:

```go
const dashboardSelect = `d.*, COALESCE(st.name, '') AS site_name,
	(pl.dashboard_id IS NOT NULL) AS has_link,
	(pl.dashboard_id IS NOT NULL OR (d.public_slug_live AND d.public_slug IS NOT NULL)) AS is_public,
	COALESCE(sh.permission, '') AS share_permission,
	COALESCE(ss.permission, '') AS site_permission,
	(SELECT count(*) FROM dashboard_widgets w WHERE w.dashboard_id = d.id) AS widget_count`
```

In `view`, replace the two lines `Published: r.Published,` and `CanEdit: canChangeContent(level, r.Published),` with:

```go
		HasLink:     r.HasLink,
		Public:      r.IsPublic,
		CanEdit:     canChangeContent(level, r.IsPublic),
```

Replace `contentError` with:

```go
// contentError is why vw's content may not be changed: ErrPublished for an
// editor of a public dashboard, ErrForbidden for a viewer.
func contentError(vw *DashboardView) error {
	if vw.Public && vw.level >= LevelEdit {
		return ErrPublished
	}
	return ErrForbidden
}
```

In `backend/internal/dashboards/access.go`, replace `canChangeContent` and its comment with:

```go
// canChangeContent reports whether l may change a dashboard's name,
// description, branding, widgets or layout, or delete it. A public dashboard
// (live address or link) needs manage, so what is public stays what an admin
// approved.
func canChangeContent(l Level, public bool) bool {
	if public {
		return l >= LevelManage
	}
	return l >= LevelEdit
}
```

- [ ] **Step 5: Write the address code**

`backend/internal/dashboards/address.go`:

```go
package dashboards

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// slugPattern is the rule status page addresses followed: letters, digits
// and hyphens.
var slugPattern = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

// validSlug reports whether s may be a public address name. Names are
// compared as stored, so "Main" and "main" are different addresses.
func validSlug(s string) bool {
	return len(s) >= models.MinPublicSlugLen && len(s) <= models.MaxPublicSlugLen && slugPattern.MatchString(s)
}

// AddressInput sets a dashboard's public address: its name ("" clears it)
// and whether it answers.
type AddressInput struct {
	Slug string `json:"slug"`
	Live bool   `json:"live"`
}

// SetPublicAddress sets a dashboard's public address name and switch
// together; admins only. An empty name clears the address. Switching it on
// records v as its publisher, and the address answers only while that user is
// an admin; switching it off keeps the name reserved.
func (s *Service) SetPublicAddress(ctx context.Context, v Viewer, id uuid.UUID, in AddressInput) (*DashboardDetail, error) {
	if _, err := s.adminDashboard(ctx, v, id); err != nil {
		return nil, err
	}
	slug := strings.TrimSpace(in.Slug)
	updates := map[string]any{"public_slug": nil, "public_slug_live": false, "public_slug_by": nil, "updated_at": time.Now().UTC()}
	switch {
	case slug == "" && in.Live:
		return nil, ErrSlugFirst
	case slug == "":
		// Clearing: the values above.
	case !validSlug(slug):
		return nil, ErrSlugRule
	default:
		updates["public_slug"] = slug
		updates["public_slug_live"] = in.Live
		if in.Live {
			updates["public_slug_by"] = v.UserID
		}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if slug != "" {
			var n int64
			if err := tx.Model(&models.Dashboard{}).Where("public_slug = ? AND id <> ?", slug, id).Count(&n).Error; err != nil {
				return fmt.Errorf("checking public address: %w", err)
			}
			if n > 0 {
				return ErrSlugTaken
			}
		}
		return tx.Model(&models.Dashboard{}).Where("id = ?", id).Updates(updates).Error
	})
	switch {
	case err == nil:
	case errors.Is(err, ErrSlugTaken), strings.Contains(strings.ToLower(err.Error()), "duplicate key"):
		// The unique index settles a race between two admins choosing one name.
		return nil, ErrSlugTaken
	default:
		return nil, fmt.Errorf("setting public address: %w", err)
	}
	return s.Get(ctx, v, id)
}

// ResolveSlug returns the dashboard a public address opens. It answers
// ErrNotFound unless the address exists, is switched on, and the admin who
// switched it on is still an admin. It runs on every public request, so a
// rename, a switch-off or a demotion takes effect at once.
func (s *Service) ResolveSlug(ctx context.Context, slug string) (*models.Dashboard, error) {
	if !validSlug(slug) {
		return nil, ErrNotFound
	}
	var rows []models.Dashboard
	err := s.db.WithContext(ctx).Table("dashboards AS d").Select("d.*").
		Joins("JOIN users u ON u.id = d.public_slug_by").
		Where("d.public_slug = ? AND d.public_slug_live AND u.is_admin", slug).Limit(1).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("resolving public address: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}
```

- [ ] **Step 6: Run the tests to see them pass**

Run: `cd backend && go vet ./... && go test ./internal/dashboards/`
Expected: PASS (unit tests; DB tests skip).

Run: `cd backend && ./scripts/test-db.sh -run 'TestDBPublicAddress|TestDBResolveSlug|TestDBLiveAddressLocksContent|TestDBDelete|TestDBSave|TestDBList|TestDBGet|TestDBPublicLink' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/dashboards/address.go backend/internal/dashboards/address_db_test.go \
  backend/internal/dashboards/errors.go backend/internal/dashboards/service.go \
  backend/internal/dashboards/access.go backend/internal/dashboards/service_db_test.go
git commit -m "feat(dashboards): public address (admins only); public = live address or link

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Logo and theme colour

**Files:**
- Create: `backend/internal/dashboards/branding.go`
- Create: `backend/internal/dashboards/branding_test.go`
- Create: `backend/internal/dashboards/branding_db_test.go`
- Modify: `backend/internal/dashboards/save.go` (`SaveInput`, `Save`)
- Modify: `backend/internal/dashboards/links.go` (`PublicDashboard`, `PublicLayout`)

**Interfaces:**
- Consumes (Task 1): `models.Dashboard.LogoURL`, `ThemeColor`, `models.MaxLogoURLLen`.
- Produces:
  - `SaveInput.LogoURL *string` (json `logo_url`) and `SaveInput.ThemeColor *string` (json `theme_color`), where nil leaves the value unchanged;
  - `*FieldError` with Field `logo_url` or `theme_color` for bad values;
  - `PublicDashboard.LogoURL` (json `logo_url`) and `PublicDashboard.ThemeColor` (json `theme_color`), filled by `PublicLayout` for both the link and the address.

- [ ] **Step 1: Write the failing tests**

`backend/internal/dashboards/branding_test.go`:

```go
package dashboards

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeLogo(t *testing.T) {
	for in, want := range map[string]string{
		"":                               "",
		"   ":                            "",
		"https://example.org/logo.png":   "https://example.org/logo.png",
		"http://intranet/logo.svg":       "http://intranet/logo.svg",
		" https://x.test/a.png ":         "https://x.test/a.png",
		"HTTPS://X.TEST/A.PNG":           "HTTPS://X.TEST/A.PNG",
	} {
		got, err := normalizeLogo(in)
		if err != nil || got != want {
			t.Errorf("normalizeLogo(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"javascript:alert(1)", "data:image/png;base64,AAAA", "ftp://x.test/a.png",
		"//x.test/a.png", "logo.png", "https://", "https://x.test/" + strings.Repeat("a", 2048)} {
		_, err := normalizeLogo(bad)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "logo_url" {
			t.Errorf("normalizeLogo(%.40q) err = %v, want a FieldError on logo_url", bad, err)
		}
	}
}

func TestNormalizeThemeColor(t *testing.T) {
	for in, want := range map[string]string{"": "", "#1b9": "#1b9", "#10B981": "#10B981", " #10b981 ": "#10b981"} {
		got, err := normalizeThemeColor(in)
		if err != nil || got != want {
			t.Errorf("normalizeThemeColor(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"10b981", "#12", "#12345", "#1234567", "red", "#ggg"} {
		_, err := normalizeThemeColor(bad)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "theme_color" {
			t.Errorf("normalizeThemeColor(%q) err = %v, want a FieldError on theme_color", bad, err)
		}
	}
}
```

`backend/internal/dashboards/branding_db_test.go`:

```go
package dashboards

import (
	"context"
	"errors"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBSaveBranding(t *testing.T) {
	w := newAccessWorld(t)
	ctx := context.Background()
	editor := w.viewer(w.siteRW)
	logo, color := " https://example.org/logo.png ", "#1b9"

	d, err := w.svc.Save(ctx, editor, w.siteDash, SaveInput{Version: 1, Name: "HQ overview", LogoURL: &logo, ThemeColor: &color})
	testdb.Must(t, err)
	if d.LogoURL != "https://example.org/logo.png" || d.ThemeColor != "#1b9" {
		t.Errorf("branding after save = %q / %q", d.LogoURL, d.ThemeColor)
	}
	d, err = w.svc.Save(ctx, editor, w.siteDash, SaveInput{Version: 2, Name: "HQ overview"})
	testdb.Must(t, err)
	if d.LogoURL != "https://example.org/logo.png" || d.ThemeColor != "#1b9" {
		t.Errorf("a save without branding changed it to %q / %q; nil must leave it as it is", d.LogoURL, d.ThemeColor)
	}

	for field, in := range map[string]SaveInput{
		"logo_url":    {Version: 3, Name: "x", LogoURL: strp("javascript:alert(1)")},
		"theme_color": {Version: 3, Name: "x", ThemeColor: strp("red")},
	} {
		_, err := w.svc.Save(ctx, editor, w.siteDash, in)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("bad %s err = %v, want a FieldError on %s", field, err, field)
		}
	}
	got, err := w.svc.Get(ctx, editor, w.siteDash)
	testdb.Must(t, err)
	if got.Version != 3 || got.Name != "HQ overview" {
		t.Errorf("a refused save wrote: version %d name %q", got.Version, got.Name)
	}

	layout, err := w.svc.PublicLayout(ctx, &got.Dashboard)
	testdb.Must(t, err)
	if layout.LogoURL != "https://example.org/logo.png" || layout.ThemeColor != "#1b9" {
		t.Errorf("public layout branding = %q / %q", layout.LogoURL, layout.ThemeColor)
	}

	empty := ""
	d, err = w.svc.Save(ctx, editor, w.siteDash, SaveInput{Version: 3, Name: "HQ overview", LogoURL: &empty, ThemeColor: &empty})
	testdb.Must(t, err)
	if d.LogoURL != "" || d.ThemeColor != "" {
		t.Errorf("clearing left %q / %q", d.LogoURL, d.ThemeColor)
	}

	// While public, branding is content: an editor may not change it.
	publish(t, w.db, w.siteDash, w.admin)
	if _, err := w.svc.Save(ctx, editor, w.siteDash, SaveInput{Version: 4, Name: "HQ overview", LogoURL: &logo}); !errors.Is(err, ErrPublished) {
		t.Errorf("editor changing the logo of a public dashboard err = %v, want ErrPublished", err)
	}
}

func strp(s string) *string { return &s }
```

Before adding `strp`, check that the dashboards package does not already have a helper by that name: `grep -rn "func strp" backend/internal/dashboards/`. If it does, drop the line above and use the existing one.

- [ ] **Step 2: Run the tests to see them fail**

Run: `cd backend && go test ./internal/dashboards/ -run 'TestNormalize'`
Expected: FAIL to compile with `undefined: normalizeLogo`, `undefined: normalizeThemeColor`, and `unknown field LogoURL in struct literal of type SaveInput`.

- [ ] **Step 3: Write the branding rules**

`backend/internal/dashboards/branding.go`:

```go
package dashboards

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// hexColor is a theme colour: #rgb or #rrggbb.
var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// normalizeLogo trims a logo address: "" for none, else an http(s) address
// with a host. Anything else (javascript:, data:, a bare file name) is a
// FieldError, so the public page only ever loads a picture from a web server.
func normalizeLogo(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(s) > models.MaxLogoURLLen {
		return "", fieldErr("logo_url", "must be an http:// or https:// address of at most 2048 characters")
	}
	return s, nil
}

// normalizeThemeColor trims a theme colour: "" for none, else #rgb or #rrggbb.
func normalizeThemeColor(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || hexColor.MatchString(s) {
		return s, nil
	}
	return "", fieldErr("theme_color", "must be a colour like #10b981 or #1b9")
}
```

- [ ] **Step 4: Save the branding**

In `backend/internal/dashboards/save.go`, add to `SaveInput` after `Description`:

```go
	// LogoURL and ThemeColor brand the public view; nil leaves them as they
	// are. They are content: a public dashboard's need an admin to change.
	LogoURL    *string `json:"logo_url"`
	ThemeColor *string `json:"theme_color"`
```

In `Save`, directly after the `normalizeNames` call and its error check, add:

```go
	updates := map[string]any{"name": name, "description": desc}
	if in.LogoURL != nil {
		logo, err := normalizeLogo(*in.LogoURL)
		if err != nil {
			return nil, err
		}
		updates["logo_url"] = logo
	}
	if in.ThemeColor != nil {
		color, err := normalizeThemeColor(*in.ThemeColor)
		if err != nil {
			return nil, err
		}
		updates["theme_color"] = color
	}
```

Then replace the transaction's last statement (the `Updates(map[string]any{"name": name, …})` return) with:

```go
		updates["version"] = current.Version + 1
		updates["updated_at"] = time.Now().UTC()
		return tx.Model(&models.Dashboard{}).Where("id = ?", id).Updates(updates).Error
```

- [ ] **Step 5: Serve the branding publicly**

In `backend/internal/dashboards/links.go`, replace `PublicDashboard` with:

```go
// PublicDashboard is the layout a public link or address serves.
type PublicDashboard struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	LogoURL     string         `json:"logo_url"`
	ThemeColor  string         `json:"theme_color"`
	Version     int            `json:"version"`
	Widgets     []PublicWidget `json:"widgets"`
}
```

In `PublicLayout`, replace the `out := &PublicDashboard{…}` line with:

```go
	out := &PublicDashboard{Name: d.Name, Description: d.Description, LogoURL: d.LogoURL, ThemeColor: d.ThemeColor,
		Version: d.Version, Widgets: make([]PublicWidget, 0, len(ws))}
```

and update its doc comment's first line to: `// PublicLayout returns d's name, branding, layout and titles, without`.

- [ ] **Step 6: Run the tests to see them pass**

Run: `cd backend && go vet ./... && go test ./internal/dashboards/`
Expected: PASS.

Run: `cd backend && ./scripts/test-db.sh -run 'TestDBSave|TestDBPublicLayout|TestDBPublicLinkLeaksNothing' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/dashboards/branding.go backend/internal/dashboards/branding_test.go \
  backend/internal/dashboards/branding_db_test.go backend/internal/dashboards/save.go backend/internal/dashboards/links.go
git commit -m "feat(dashboards): logo and theme colour, saved as content and served publicly

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: API routes; the status page backend removed

**Files:**
- Modify: `backend/internal/api/dashboard_handler.go` (store interface, admin route, error mapping, handler)
- Modify: `backend/internal/api/dashboard_public_handler.go` (whole file)
- Modify: `backend/internal/api/dashboard_handler_test.go` (fake fields and methods)
- Create: `backend/internal/api/dashboard_address_handler_test.go`
- Modify: `backend/internal/models/audit.go` (one action)
- Modify: `backend/cmd/sentinel/main.go:123,361-368,393`
- Modify: `backend/internal/services/monitor_service.go:215-217` (comment)
- Delete: `backend/internal/api/status_page_handler.go`, `backend/internal/services/status_page_service.go`, `backend/internal/models/status_page.go`

**Interfaces:**
- Consumes (Tasks 2, 3): `SetPublicAddress`, `ResolveSlug`, `AddressInput`, `ErrSlugRule`, `ErrSlugTaken`, `ErrSlugFirst`, and the `PublicDashboard` branding.
- Produces:
  - `PUT /api/v1/dashboards/:id/public-address` with body `{slug, live}`: admin only; returns the dashboard detail; audited as `dashboard_address_set`;
  - `GET /api/v1/public/status/:key` and `GET /api/v1/public/status/:key/widgets/:wid/data`: no sign-in, rate limited, `no-store`, and a 404 with `"This page is not available"` for a dead address;
  - the token routes are unchanged in behaviour; their path parameter is renamed to `:key`;
  - `models.ActionDashboardAddressSet = "dashboard_address_set"`;
  - the `/api/v1/status-pages…` routes are gone.

- [ ] **Step 1: Extend the fake store**

In `backend/internal/api/dashboard_handler_test.go`, add two fields to `fakeDashboards` (after `hasLink bool`):

```go
	// slugErr, when set, is what ResolveSlug returns; lastAddress is the
	// last SetPublicAddress body.
	slugErr     error
	lastAddress dashboards.AddressInput
```

and, after its `PublicWidget` method, add:

```go
func (f *fakeDashboards) SetPublicAddress(_ context.Context, _ dashboards.Viewer, _ uuid.UUID, in dashboards.AddressInput) (*dashboards.DashboardDetail, error) {
	f.mutations++
	f.lastAddress = in
	if f.err != nil {
		return nil, f.err
	}
	return f.detail(), nil
}
func (f *fakeDashboards) ResolveSlug(context.Context, string) (*models.Dashboard, error) {
	if f.slugErr != nil {
		return nil, f.slugErr
	}
	return &models.Dashboard{ID: f.dashboardID, Version: 1}, nil
}
```

- [ ] **Step 2: Write the failing tests**

`backend/internal/api/dashboard_address_handler_test.go`:

```go
package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/dashboards"
)

func TestPublicAddressRouteIsAdminOnly(t *testing.T) {
	path := "/api/v1/dashboards/" + uuid.NewString() + "/public-address"
	body := map[string]any{"slug": "hq-status", "live": true}
	store := &fakeDashboards{}
	w := do(dashboardRouter(store, &fakeWidgetResolver{}, false), http.MethodPut, path, body)
	if w.Code != http.StatusForbidden || store.mutations != 0 {
		t.Errorf("PUT by a member: status %d, mutations %d; want 403 and the handler not run", w.Code, store.mutations)
	}

	store, audit := &fakeDashboards{}, &fakeAudit{}
	w = do(dashboardRouterAudited(store, &fakeWidgetResolver{}, true, audit), http.MethodPut, path, body)
	if w.Code != http.StatusOK || store.lastAddress.Slug != "hq-status" || !store.lastAddress.Live {
		t.Errorf("PUT by an admin: status %d, body passed %+v", w.Code, store.lastAddress)
	}
	if len(audit.changes) != 1 {
		t.Errorf("audit entries = %d, want 1", len(audit.changes))
	}
}

func TestPublicAddressErrorsKeepTheirWording(t *testing.T) {
	path := "/api/v1/dashboards/" + uuid.NewString() + "/public-address"
	for _, e := range []error{dashboards.ErrSlugRule, dashboards.ErrSlugTaken, dashboards.ErrSlugFirst} {
		w := do(dashboardRouter(&fakeDashboards{err: e}, &fakeWidgetResolver{}, true), http.MethodPut, path, map[string]any{"slug": "x"})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), e.Error()) {
			t.Errorf("%v: status %d body %s, want 400 with the message", e, w.Code, w.Body.String())
		}
	}
}

func TestPublicStatusRoutes(t *testing.T) {
	store, res := &fakeDashboards{}, &fakeWidgetResolver{}
	r := dashboardRouter(store, res, false)
	layout := "/api/v1/public/status/hq-status"
	data := layout + "/widgets/" + uuid.NewString() + "/data"

	w := do(r, http.MethodGet, layout, nil)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("layout: status %d, Cache-Control %q", w.Code, w.Header().Get("Cache-Control"))
	}
	if w := do(r, http.MethodGet, data, nil); w.Code != http.StatusOK || res.publicCalls != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("widget data: status %d, resolves %d", w.Code, res.publicCalls)
	}

	store.slugErr = dashboards.ErrNotFound // switched off, unknown, or its publisher demoted
	for _, p := range []string{layout, data} {
		w := do(r, http.MethodGet, p, nil)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "This page is not available") {
			t.Errorf("%s after the address died: status %d body %s", p, w.Code, w.Body.String())
		}
	}
	if res.publicCalls != 1 {
		t.Errorf("resolved widget data %d times; a dead address must not resolve", res.publicCalls)
	}
	if w := do(r, http.MethodGet, layout+"/widgets/not-a-uuid/data", nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad widget id: status %d, want 400", w.Code)
	}
}

func TestPublicStatusIsRateLimited(t *testing.T) {
	r := dashboardRouter(&fakeDashboards{}, &fakeWidgetResolver{}, false)
	limited := 0
	for i := 0; i < 200; i++ {
		if w := do(r, http.MethodGet, "/api/v1/public/status/hq-status", nil); w.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Error("200 quick requests from one address were never limited (burst 120)")
	}
}
```

- [ ] **Step 3: Run the tests to see them fail**

Run: `cd backend && go test ./internal/api/ -run 'TestPublicAddress|TestPublicStatus'`
Expected: FAIL. The route `PUT /public-address` answers 404, and `/api/v1/public/status/...` answers 404.

- [ ] **Step 4: Add the admin route**

In `backend/internal/models/audit.go`, after `ActionDashboardLinkRevoked`:

```go
	ActionDashboardAddressSet  = "dashboard_address_set"
```

In `backend/internal/api/dashboard_handler.go`:

Add to the `dashboardStore` interface, after `RevokePublicLink`:

```go
	SetPublicAddress(ctx context.Context, v dashboards.Viewer, id uuid.UUID, in dashboards.AddressInput) (*dashboards.DashboardDetail, error)
	ResolveSlug(ctx context.Context, slug string) (*models.Dashboard, error)
```

Add to the admin group in `RegisterDashboardRoutes`, after the `admin.DELETE("/:id/public-link", …)` line:

```go
	admin.PUT("/:id/public-address", setPublicAddressHandler(store, audit))
```

and change that function's doc comment's last clause to `public links and addresses are admin-only and re-checked against the database by RequireAdmin.`

In `respondDashboardError`, extend the last 400 case to:

```go
	case errors.Is(err, dashboards.ErrInvalid), errors.Is(err, dashboards.ErrShareSiteDashboard),
		errors.Is(err, dashboards.ErrShareOwner), errors.Is(err, dashboards.ErrUnknownUser),
		errors.Is(err, dashboards.ErrSlugRule), errors.Is(err, dashboards.ErrSlugTaken), errors.Is(err, dashboards.ErrSlugFirst):
		respondError(c, http.StatusBadRequest, err.Error())
```

Append the handler at the end of the file:

```go
func setPublicAddressHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		var in dashboards.AddressInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		d, err := store.SetPublicAddress(c.Request.Context(), v, id, in)
		if err != nil {
			respondDashboardError(c, "setPublicAddress", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardAddressSet, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: map[string]any{"public_slug": d.PublicSlug, "live": d.PublicSlugLive}})
		respondSuccess(c, http.StatusOK, d)
	}
}
```

- [ ] **Step 5: Serve both public route families**

Replace `backend/internal/api/dashboard_public_handler.go` with:

```go
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/dashboards"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// linkGone is what a dead public link shows, so a TV says why it is blank.
const linkGone = "This dashboard link is no longer available"

// addressGone is what an unknown, switched-off or orphaned public address
// shows: one message for all three, so the answer says nothing about which.
const addressGone = "This page is not available"

// publicResolve finds the dashboard a public link token or address opens,
// or answers dashboards.ErrNotFound.
type publicResolve func(ctx context.Context, key string) (*models.Dashboard, error)

// RegisterPublicDashboardRoutes mounts the public dashboard routes: an
// unguessable link (/public/dashboards/:key) and a readable address
// (/public/status/:key). Neither needs a login; each has a per-IP limit sized
// for several TVs behind one office address (spec §4). Every request resolves
// its key again, so a revoked link, an address switched off or renamed, or a
// demoted publisher stops working on the next request.
func RegisterPublicDashboardRoutes(router *gin.Engine, store dashboardStore, resolver widgetResolver) {
	links := NewRateLimiter(600, time.Minute, 120)
	g := router.Group("/api/v1/public/dashboards", links.Middleware("public-dashboard", ByIP))
	g.GET("/:key", publicDashboardHandler(store, store.ResolveToken, linkGone))
	g.GET("/:key/widgets/:wid/data", publicWidgetDataHandler(store, resolver, store.ResolveToken, linkGone))

	addresses := NewRateLimiter(600, time.Minute, 120)
	s := router.Group("/api/v1/public/status", addresses.Middleware("public-status", ByIP))
	s.GET("/:key", publicDashboardHandler(store, store.ResolveSlug, addressGone))
	s.GET("/:key/widgets/:wid/data", publicWidgetDataHandler(store, resolver, store.ResolveSlug, addressGone))
}

func publicDashboardHandler(store dashboardStore, resolve publicResolve, gone string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		d, err := resolve(c.Request.Context(), c.Param("key"))
		if errors.Is(err, dashboards.ErrNotFound) {
			respondError(c, http.StatusNotFound, gone)
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

func publicWidgetDataHandler(store dashboardStore, resolver widgetResolver, resolve publicResolve, gone string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		wid, err := uuid.Parse(c.Param("wid"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid widget id")
			return
		}
		d, err := resolve(c.Request.Context(), c.Param("key"))
		if errors.Is(err, dashboards.ErrNotFound) {
			respondError(c, http.StatusNotFound, gone)
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

- [ ] **Step 6: Remove the status page backend**

```bash
git rm backend/internal/api/status_page_handler.go backend/internal/services/status_page_service.go backend/internal/models/status_page.go
```

In `backend/cmd/sentinel/main.go`:
- Delete the line `statusPageService := services.NewStatusPageService(db)`.
- Replace the lines:

  ```go
  	// Public auth endpoints (register/login/mfa-verify) + public status pages.
  	api.RegisterAuthRoutes(router, authService, settingsService)
  	api.RegisterPublicStatusRoutes(router, statusPageService, incidentService, checkService)
  	// Share-token report access: public by design, so outside the authenticated
  	// v1 group (same split as the public status pages above).
  	api.RegisterPublicReportRoutes(router, reportBuilder)
  	// Public dashboard links: no login, like status pages and shared reports.
  	api.RegisterPublicDashboardRoutes(router, dashboardService, dashboardResolver)
  ```

  with:

  ```go
  	// Public auth endpoints (register/login/mfa-verify).
  	api.RegisterAuthRoutes(router, authService, settingsService)
  	// Share-token report access: public by design, so outside the authenticated
  	// v1 group (same split as the public dashboards below).
  	api.RegisterPublicReportRoutes(router, reportBuilder)
  	// Public dashboards, by link or by readable address: no login, like shared reports.
  	api.RegisterPublicDashboardRoutes(router, dashboardService, dashboardResolver)
  ```

- Delete the line `api.RegisterStatusPageRoutes(v1, statusPageService, incidentService)`.

In `backend/internal/services/monitor_service.go`, replace the `DeleteMonitor` comment's first two lines with:

```go
// DeleteMonitor hard-deletes a monitor by ID. Related checks, incidents and
// notifications are removed by the database via
```

so that the comment now reads "…removed by the database via ON DELETE CASCADE foreign keys.".

Then check that nothing still refers to the removed code:

Run: `grep -rn "StatusPage\|status_page\|statusPage" backend --include=*.go | grep -v public_dashboards_migration_db_test.go`
Expected: no output. Task 1's migration test names the old tables on purpose.

- [ ] **Step 7: Run the tests to see them pass**

Run: `cd backend && go vet ./... && go build ./... && go test ./internal/api/ ./internal/dashboards/ ./internal/models/`
Expected: PASS. `go build` proves main.go compiles without the status page service.

Run: `cd backend && ./scripts/test-db.sh`
Expected: the full suite PASSES, DB tests included.

- [ ] **Step 8: Commit**

```bash
git add -A backend/internal/api backend/internal/models/audit.go backend/cmd/sentinel/main.go backend/internal/services/monitor_service.go
git commit -m "feat(api): public-address route and /public/status/:slug for dashboards; status page backend removed

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: The Service status widget (backend)

**Files:**
- Modify: `backend/internal/services/uptime_buckets.go` (`DayCount`, `DailyBucketsFromCounts`; `DailyUptimeBuckets` delegates)
- Modify: `backend/internal/services/check_service.go` (`DailyCheckCounts`)
- Create: `backend/internal/services/daily_check_counts_db_test.go`
- Create: `backend/internal/dashboards/widget_service_status.go`
- Create: `backend/internal/dashboards/widget_service_status_db_test.go`
- Modify: `backend/internal/dashboards/deps.go` (limits, `IncidentReader`, `CheckReader`, registry)
- Modify: `backend/internal/dashboards/public_leak_db_test.go` (a `service_status` config)

**Interfaces:**
- Consumes: `services.IncidentService.GetDowntimePercentage(ctx, monitorID uuid.UUID, start, end time.Time) (float64, error)` (existing); `MonitorReader.MonitorsByIDs`; `idFor`, `toBuckets`, `decodeConfig`, `fieldErr`, `checkCount` (existing in the package).
- Produces:
  - `services.DayCount{Day time.Time; Total, Failed int}`;
  - `services.DailyBucketsFromCounts(counts []DayCount, now time.Time) []map[string]any`;
  - `(*services.CheckService).DailyCheckCounts(ctx, monitorIDs []uuid.UUID, start, end time.Time) (map[uuid.UUID][]services.DayCount, error)`;
  - widget type `service_status` with config `{groups:[{name, monitors:[uuid]}], show_banner, show_uptime}`;
  - data `ServiceStatusData{show_banner, show_uptime, total, down, groups:[{name, monitors:[{monitor_id?, name, status, uptime_90d?, days:[{label,status,uptime}]}]}]}`, refreshed every 60 s.

- [ ] **Step 1: Write the failing services test**

`backend/internal/services/daily_check_counts_db_test.go`:

```go
package services

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// DailyCheckCounts counts per UTC day in SQL, and its counts make the same
// bars as DailyUptimeBuckets over the raw checks.
func TestDBDailyCheckCounts(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	owner := testdb.NewUser(t, db, true)
	a := newMonitor(t, db, owner, "web")
	b := newMonitor(t, db, owner, "mail")
	quiet := newMonitor(t, db, owner, "dns")
	now := time.Now().UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	d3, d10 := day.AddDate(0, 0, -3), day.AddDate(0, 0, -10)
	testdb.Exec(t, db, `INSERT INTO checks (monitor_id, status, timestamp) VALUES
		(?, 'success', ?), (?, 'failed', ?), (?, 'timeout', ?), (?, 'success', ?), (?, 'success', ?), (?, 'success', ?)`,
		a, d3.Add(time.Hour), a, d3.Add(2*time.Hour), a, d3.Add(23*time.Hour+59*time.Minute), a, d10.Add(time.Hour),
		b, d3.Add(time.Hour), a, day.AddDate(0, 0, -100))
	start := now.AddDate(0, 0, -UptimeDailyDays)
	s := NewCheckService(db)

	counts, err := s.DailyCheckCounts(ctx, []uuid.UUID{a, b, quiet}, start, now)
	testdb.Must(t, err)
	if c, ok := counts[quiet]; ok {
		t.Errorf("a monitor with no checks has counts %v", c)
	}
	got := map[string][2]int{}
	for _, c := range counts[a] {
		got[c.Day.Format("2006-01-02")] = [2]int{c.Total, c.Failed}
	}
	want := map[string][2]int{d3.Format("2006-01-02"): {3, 2}, d10.Format("2006-01-02"): {1, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("daily counts = %v, want %v (a timeout counts as failed; the check 100 days ago is outside)", got, want)
	}
	if len(counts[b]) != 1 || counts[b][0].Total != 1 || counts[b][0].Failed != 0 {
		t.Errorf("mail counts = %v, want one day with one passing check", counts[b])
	}

	checks, err := s.ChecksInRange(ctx, a, start, now, 0, 0)
	testdb.Must(t, err)
	if !reflect.DeepEqual(DailyBucketsFromCounts(counts[a], now), DailyUptimeBuckets(checks, now)) {
		t.Error("bars from the counts differ from bars from the checks")
	}

	none, err := s.DailyCheckCounts(ctx, nil, start, now)
	testdb.Must(t, err)
	if len(none) != 0 {
		t.Errorf("no monitors gave %v", none)
	}
}
```

(The package's DB tests already have `newMonitor(t, db, owner, name) uuid.UUID` in `incident_device_db_test.go`.)

- [ ] **Step 2: Run it to see it fail**

Run: `cd backend && go vet ./internal/services/`
Expected: FAIL with `s.DailyCheckCounts undefined` and `undefined: DailyBucketsFromCounts`.

- [ ] **Step 3: Count checks per day**

In `backend/internal/services/uptime_buckets.go`, replace `DailyUptimeBuckets` (its doc comment and body) with:

```go
// DayCount is one UTC day of a monitor's checks: how many ran and how many
// did not succeed.
type DayCount struct {
	Day    time.Time
	Total  int
	Failed int
}

// utcDay is t's UTC calendar day, at midnight.
func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// DailyUptimeBuckets buckets checks into the 90 UTC calendar days ending at
// now, each summarized purely by pass/fail counts - no incident- or
// maintenance-derived spans, since a public view never surfaces exact
// downtime clock times. Used by the dashboard monitors widget; the
// authenticated endpoints have their own, hourly, view (HourlyUptimeBuckets).
func DailyUptimeBuckets(checks []models.Check, now time.Time) []map[string]any {
	byDay := map[time.Time]*DayCount{}
	for _, ch := range checks {
		k := utcDay(ch.Timestamp)
		c := byDay[k]
		if c == nil {
			c = &DayCount{Day: k}
			byDay[k] = c
		}
		c.Total++
		if ch.Status != "success" {
			c.Failed++
		}
	}
	counts := make([]DayCount, 0, len(byDay))
	for _, c := range byDay {
		counts = append(counts, *c)
	}
	return DailyBucketsFromCounts(counts, now)
}

// DailyBucketsFromCounts is DailyUptimeBuckets for checks already counted per
// UTC day (CheckService.DailyCheckCounts): the 90 days ending at now, oldest
// first, a day with no checks as "nodata".
func DailyBucketsFromCounts(counts []DayCount, now time.Time) []map[string]any {
	byDay := make(map[time.Time]DayCount, len(counts))
	for _, c := range counts {
		byDay[utcDay(c.Day)] = c
	}
	daily := make([]map[string]any, 0, UptimeDailyDays)
	curDay := utcDay(now)
	for i := UptimeDailyDays - 1; i >= 0; i-- {
		k := curDay.AddDate(0, 0, -i)
		c := byDay[k]
		status := "nodata"
		uptime := 0.0
		if c.Total > 0 {
			uptime = roundUptime(float64(c.Total-c.Failed) / float64(c.Total) * 100)
			switch {
			case c.Failed == 0:
				status = "up"
			case c.Failed == c.Total:
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
```

In `backend/internal/services/check_service.go`, after `ChecksInRange`, add:

```go
// DailyCheckCounts counts each monitor's checks, and those that did not
// succeed, per UTC day in [start, end], in one query: 90-day bars for many
// monitors without loading every check. A monitor with no checks is absent.
func (s *CheckService) DailyCheckCounts(ctx context.Context, monitorIDs []uuid.UUID, start, end time.Time) (map[uuid.UUID][]DayCount, error) {
	out := make(map[uuid.UUID][]DayCount, len(monitorIDs))
	if len(monitorIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		MonitorID uuid.UUID `gorm:"column:monitor_id"`
		Day       time.Time `gorm:"column:day"`
		Total     int       `gorm:"column:total"`
		Failed    int       `gorm:"column:failed"`
	}
	err := s.db.WithContext(ctx).Raw(`SELECT c.monitor_id,
			date_trunc('day', c.timestamp AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS day,
			count(*) AS total,
			count(*) FILTER (WHERE c.status <> 'success') AS failed
		FROM checks c
		WHERE c.monitor_id IN ? AND c.timestamp >= ? AND c.timestamp <= ?
		GROUP BY 1, 2`, monitorIDs, start, end).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("counting daily checks: %w", err)
	}
	for _, r := range rows {
		out[r.MonitorID] = append(out[r.MonitorID], DayCount{Day: r.Day.UTC(), Total: r.Total, Failed: r.Failed})
	}
	return out, nil
}
```

(`check_service.go` already imports `context`, `fmt`, `time` and `github.com/google/uuid`; add any that `go vet` reports missing.)

- [ ] **Step 4: Run the services tests to see them pass**

Run: `cd backend && go vet ./internal/services/ && go test ./internal/api/ -run Daily`
Expected: PASS. `api/daily_buckets_test.go` keeps `DailyUptimeBuckets` honest.

Run: `cd backend && ./scripts/test-db.sh -run 'TestDBDailyCheckCounts|TestDBChecksInRange|TestDBMonitorsWidget' -v`
Expected: PASS.

- [ ] **Step 5: Write the failing widget tests**

`backend/internal/dashboards/widget_service_status_db_test.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func newServiceStatus(d Deps) serviceStatusWidget {
	return serviceStatusWidget{monitors: d.Monitors, checks: d.Checks, incidents: d.Incidents}
}

func TestDBServiceStatusValidate(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	w := newServiceStatus(realDeps(t, db))
	owner := testdb.NewUser(t, db, true)
	a := newMonitor(t, db, owner, "Website", "https://a.test")
	b := newMonitor(t, db, owner, "Mail", "https://b.test")
	gone := uuid.New() // never existed: as good as deleted since the last save

	clean, err := w.Validate(ctx, json.RawMessage(fmt.Sprintf(
		`{"groups":[{"name":"  Public  ","monitors":["%s","%s"]},{"name":"","monitors":["%s"]}],"extra":1}`, b, gone, a)))
	testdb.Must(t, err)
	want := fmt.Sprintf(`{"groups":[{"name":"Public","monitors":["%s"]},{"name":"","monitors":["%s"]}],"show_banner":true,"show_uptime":true}`, b, a)
	if string(clean) != want {
		t.Errorf("clean config =\n  %s\nwant\n  %s\n(names trimmed, deleted monitors and unknown fields dropped, switches default on)", clean, want)
	}
	off, err := w.Validate(ctx, json.RawMessage(fmt.Sprintf(`{"groups":[{"monitors":["%s"]}],"show_banner":false,"show_uptime":false}`, a)))
	testdb.Must(t, err)
	if !strings.Contains(string(off), `"show_banner":false,"show_uptime":false`) {
		t.Errorf("switched-off config = %s", off)
	}

	empties := make([]string, 21)
	for i := range empties {
		empties[i] = fmt.Sprintf(`{"name":"g%d","monitors":[]}`, i)
	}
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = `"` + uuid.NewString() + `"`
	}
	for name, cfg := range map[string]string{
		"21 groups":            `{"groups":[` + strings.Join(empties, ",") + `]}`,
		"a 101-character name": fmt.Sprintf(`{"groups":[{"name":"%s","monitors":["%s"]}]}`, strings.Repeat("n", 101), a),
		"a monitor twice":      fmt.Sprintf(`{"groups":[{"monitors":["%s"]},{"monitors":["%s"]}]}`, a, a),
		"no monitors":          `{"groups":[{"name":"empty","monitors":[]}]}`,
		"no groups":            `{}`,
		"only deleted ones":    fmt.Sprintf(`{"groups":[{"monitors":["%s"]}]}`, gone),
		"101 monitors":         `{"groups":[{"monitors":[` + strings.Join(tooMany, ",") + `]}]}`,
		"groups of the wrong type": `{"groups":"all"}`,
	} {
		_, err := w.Validate(ctx, json.RawMessage(cfg))
		var fe *FieldError
		if !errors.As(err, &fe) {
			t.Errorf("%s: err = %v, want a FieldError", name, err)
		}
	}
	if s := w.Subjects(clean); len(s.Monitors) != 2 || s.Monitors[0] != b || s.Monitors[1] != a || s.Broad {
		t.Errorf("subjects = %+v, want Mail then Website and not broad", s)
	}
	if w.Refresh(clean, "") != time.Minute {
		t.Errorf("refresh = %v, want 1m", w.Refresh(clean, ""))
	}
}

func TestDBServiceStatusResolve(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	deps := realDeps(t, db)
	w := newServiceStatus(deps)
	sites := services.NewSiteService(db)
	r := NewResolver(NewRegistry(w), NewDBChecker(db, sites, services.NewMonitorService(db)))
	admin := testdb.NewUser(t, db, true)
	member := testdb.NewUser(t, db, false)
	web := newMonitor(t, db, admin, "Website", "https://intranet.secret.test")
	mail := newMonitor(t, db, admin, "Mail", "https://mail.secret.test")
	dns := newMonitor(t, db, admin, "DNS", "https://dns.secret.test")
	testdb.Exec(t, db, `UPDATE monitors SET current_status = 'online' WHERE id IN (?, ?)`, web, dns)
	testdb.Exec(t, db, `UPDATE monitors SET current_status = 'offline' WHERE id = ?`, mail)
	shareMonitor(t, db, web, member, admin)
	now := time.Now().UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	d3, d5 := day.AddDate(0, 0, -3), day.AddDate(0, 0, -5)
	testdb.Exec(t, db, `INSERT INTO checks (monitor_id, status, response_time_ms, timestamp) VALUES (?, 'success', 40, ?), (?, 'failed', 0, ?), (?, 'success', 30, ?)`,
		web, d3.Add(time.Hour), web, d3.Add(2*time.Hour), web, d5.Add(time.Hour))
	clean, err := w.Validate(ctx, json.RawMessage(fmt.Sprintf(
		`{"groups":[{"name":"Public","monitors":["%s","%s"]},{"name":"","monitors":["%s"]}]}`, mail, web, dns)))
	testdb.Must(t, err)
	widget := &models.DashboardWidget{ID: uuid.New(), Type: "service_status", Config: models.RawJSON(clean)}
	resolve := func(v Viewer) (*Response, ServiceStatusData) {
		t.Helper()
		resp, err := r.Resolve(ctx, v, widget, "")
		testdb.Must(t, err)
		sd, _ := resp.Data.(ServiceStatusData)
		return resp, sd
	}

	resp, sd := resolve(Viewer{UserID: admin, IsAdmin: true})
	if resp.State != StateOK || resp.RefreshSeconds != 60 {
		t.Errorf("state %q refresh %d, want ok and 60", resp.State, resp.RefreshSeconds)
	}
	if sd.Total != 3 || sd.Down != 1 || !sd.ShowBanner || !sd.ShowUptime {
		t.Errorf("total %d down %d banner %v uptime %v, want 3, 1, true, true", sd.Total, sd.Down, sd.ShowBanner, sd.ShowUptime)
	}
	if len(sd.Groups) != 2 || sd.Groups[0].Name != "Public" || len(sd.Groups[0].Monitors) != 2 ||
		sd.Groups[0].Monitors[0].Name != "Mail" || sd.Groups[0].Monitors[1].Name != "Website" ||
		sd.Groups[1].Name != "" || len(sd.Groups[1].Monitors) != 1 || sd.Groups[1].Monitors[0].Name != "DNS" {
		t.Fatalf("groups = %+v, want Public (Mail, Website) then an unnamed group (DNS), in the order set", sd.Groups)
	}
	site := sd.Groups[0].Monitors[1]
	if len(site.Days) != 90 || site.Days[86].Status != "partial" || site.Days[86].Uptime != 50 ||
		site.Days[84].Status != "up" || site.Days[89].Status != "nodata" {
		t.Errorf("Website days: %d of them; 3 days ago %+v, 5 days ago %+v, today %+v", len(site.Days), site.Days[86], site.Days[84], site.Days[89])
	}
	if site.Uptime90d == nil || *site.Uptime90d != 100 {
		t.Errorf("Website uptime = %v, want 100 (no incidents)", site.Uptime90d)
	}
	if m := sd.Groups[0].Monitors[0]; m.Status != "offline" || m.Uptime90d == nil || *m.Uptime90d != 99.99 {
		t.Errorf("Mail = %+v, want offline at 99.99%% (an outage now never reads 100)", m)
	}
	if site.MonitorID == nil || *site.MonitorID != web {
		t.Errorf("a signed-in row carries monitor id %v, want %s", site.MonitorID, web)
	}

	// A member sees only what is shared with them, and the banner counts only that.
	resp, sd = resolve(Viewer{UserID: member})
	if resp.Hidden != 2 || sd.Total != 1 || sd.Down != 0 || len(sd.Groups) != 1 || sd.Groups[0].Monitors[0].Name != "Website" {
		t.Errorf("member: hidden %d total %d down %d groups %+v; want 2 hidden and only Website", resp.Hidden, sd.Total, sd.Down, sd.Groups)
	}

	// The public view shows every monitor, with no ids or URLs.
	resp, sd = resolve(PublicViewer)
	raw, err := json.Marshal(resp)
	testdb.Must(t, err)
	if sd.Total != 3 || containsAny(string(raw), "secret.test", web.String(), mail.String(), dns.String()) {
		t.Errorf("public: total %d, payload %s; want all three monitors and no ids or URLs", sd.Total, raw)
	}

	// A monitor deleted after the save is skipped.
	testdb.Exec(t, db, `DELETE FROM monitors WHERE id = ?`, dns)
	resp, sd = resolve(Viewer{UserID: admin, IsAdmin: true})
	if resp.Removed != 1 || sd.Total != 2 || len(sd.Groups) != 1 {
		t.Errorf("after deleting DNS: removed %d total %d groups %d; want 1, 2, 1", resp.Removed, sd.Total, len(sd.Groups))
	}

	// With the uptime switched off there are no uptime numbers.
	off, err := w.Validate(ctx, json.RawMessage(fmt.Sprintf(`{"groups":[{"monitors":["%s"]}],"show_banner":false,"show_uptime":false}`, web)))
	testdb.Must(t, err)
	widget.Config = models.RawJSON(off)
	_, sd = resolve(Viewer{UserID: admin, IsAdmin: true})
	if sd.ShowBanner || sd.ShowUptime || len(sd.Groups) != 1 || sd.Groups[0].Monitors[0].Uptime90d != nil {
		t.Errorf("switched off: %+v, want no banner and no uptime numbers", sd)
	}
}
```

In `backend/internal/dashboards/public_leak_db_test.go`, add to the `configs` map after `"monitors"`:

```go
		"service_status": fmt.Sprintf(`{"groups":[{"name":"Public","monitors":["%s"]}]}`, mon),
```

- [ ] **Step 6: Run them to see them fail**

Run: `cd backend && go vet ./internal/dashboards/`
Expected: FAIL with `undefined: serviceStatusWidget` and `undefined: ServiceStatusData`.

- [ ] **Step 7: Write the widget**

In `backend/internal/dashboards/deps.go`:
- Add to the limits `const (…)` block:

  ```go
  	maxServiceGroups    = 20
  	maxServiceMonitors  = 100
  	maxServiceGroupName = 100
  ```

- Add to `IncidentReader`:

  ```go
  	GetDowntimePercentage(ctx context.Context, monitorID uuid.UUID, start, end time.Time) (float64, error)
  ```

- Add to `CheckReader`:

  ```go
  	DailyCheckCounts(ctx context.Context, monitorIDs []uuid.UUID, start, end time.Time) (map[uuid.UUID][]services.DayCount, error)
  ```

- Register the widget last in `NewDefaultRegistry`:

  ```go
  		serviceStatusWidget{monitors: d.Monitors, checks: d.Checks, incidents: d.Incidents},
  ```

`backend/internal/dashboards/widget_service_status.go`:

```go
package dashboards

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// serviceStatusRefresh is how often a Service status widget asks again.
const serviceStatusRefresh = time.Minute

type serviceGroupConfig struct {
	Name     string      `json:"name"`
	Monitors []uuid.UUID `json:"monitors"`
}

type serviceStatusConfig struct {
	Groups     []serviceGroupConfig `json:"groups"`
	ShowBanner *bool                `json:"show_banner"`
	ShowUptime *bool                `json:"show_uptime"`
}

// ServiceMonitor is one monitor on a Service status widget. Its URL and
// target are never included; its id only for signed-in viewers.
type ServiceMonitor struct {
	MonitorID *uuid.UUID     `json:"monitor_id,omitempty"`
	Name      string         `json:"name"`
	Status    string         `json:"status"`
	Uptime90d *float64       `json:"uptime_90d,omitempty"`
	Days      []UptimeBucket `json:"days"`
}

// ServiceGroup is one group of monitors, in the order the editor set.
type ServiceGroup struct {
	Name     string           `json:"name"`
	Monitors []ServiceMonitor `json:"monitors"`
}

// ServiceStatusData is what a status page showed: an overall banner and
// the monitors in groups, each with 90 daily bars. Total and Down count only
// the monitors shown, so the banner never counts what the viewer cannot see.
type ServiceStatusData struct {
	ShowBanner bool           `json:"show_banner"`
	ShowUptime bool           `json:"show_uptime"`
	Total      int            `json:"total"`
	Down       int            `json:"down"`
	Groups     []ServiceGroup `json:"groups"`
}

type serviceStatusWidget struct {
	monitors  MonitorReader
	checks    CheckReader
	incidents IncidentReader
}

func (serviceStatusWidget) Type() string { return "service_status" }

// isOn reads a switch that defaults to on.
func isOn(b *bool) bool { return b == nil || *b }

// Validate checks the limits, trims group names, and drops monitors that no
// longer exist, so the first save after a monitor is deleted keeps the rest
// (the spec's "the editor drops them the next time the widget is saved").
func (w serviceStatusWidget) Validate(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c serviceStatusConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	if err := checkCount("groups", len(c.Groups), 0, maxServiceGroups); err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]bool{}
	var all []uuid.UUID
	for i := range c.Groups {
		g := &c.Groups[i]
		g.Name = strings.TrimSpace(g.Name)
		if utf8.RuneCountInString(g.Name) > maxServiceGroupName {
			return nil, fieldErr("groups", "need names of 100 characters or fewer")
		}
		ids := make([]uuid.UUID, 0, len(g.Monitors))
		for _, id := range g.Monitors {
			if id == uuid.Nil {
				continue
			}
			if seen[id] {
				return nil, fieldErr("groups", "may show a monitor only once")
			}
			seen[id] = true
			ids = append(ids, id)
		}
		g.Monitors = ids
		all = append(all, ids...)
	}
	if len(all) > maxServiceMonitors {
		return nil, fieldErr("groups", "allow at most 100 monitors in all")
	}
	if len(all) == 0 {
		return nil, fieldErr("groups", "need at least one monitor")
	}
	existing, err := w.monitors.MonitorsByIDs(ctx, all)
	if err != nil {
		return nil, err
	}
	alive := make(map[uuid.UUID]bool, len(existing))
	for _, m := range existing {
		alive[m.ID] = true
	}
	kept := 0
	for i := range c.Groups {
		ids := make([]uuid.UUID, 0, len(c.Groups[i].Monitors))
		for _, id := range c.Groups[i].Monitors {
			if alive[id] {
				ids = append(ids, id)
			}
		}
		c.Groups[i].Monitors = ids
		kept += len(ids)
	}
	if kept == 0 {
		return nil, fieldErr("groups", "need at least one monitor")
	}
	banner, uptime := isOn(c.ShowBanner), isOn(c.ShowUptime)
	c.ShowBanner, c.ShowUptime = &banner, &uptime
	return json.Marshal(c)
}

func (serviceStatusWidget) Subjects(raw json.RawMessage) Subjects {
	var c serviceStatusConfig
	_ = json.Unmarshal(raw, &c)
	var ids []uuid.UUID
	for _, g := range c.Groups {
		ids = append(ids, g.Monitors...)
	}
	return Subjects{Monitors: ids}
}

func (serviceStatusWidget) Refresh(json.RawMessage, string) time.Duration { return serviceStatusRefresh }

func (w serviceStatusWidget) Resolve(ctx context.Context, raw json.RawMessage, in ResolveInput) (any, error) {
	var c serviceStatusConfig
	_ = json.Unmarshal(raw, &c)
	out := ServiceStatusData{ShowBanner: isOn(c.ShowBanner), ShowUptime: isOn(c.ShowUptime), Groups: []ServiceGroup{}}
	if len(in.Visible.Monitors) == 0 {
		return out, nil
	}
	list, err := w.monitors.MonitorsByIDs(ctx, in.Visible.Monitors)
	if err != nil {
		return nil, err
	}
	// Only visible monitors are loaded, so a hidden one, or one deleted
	// since the access check, is simply absent below.
	byID := make(map[uuid.UUID]*models.Monitor, len(list))
	for i := range list {
		byID[list[i].ID] = &list[i]
	}
	now := in.Now.UTC()
	start := now.AddDate(0, 0, -services.UptimeDailyDays)
	counts, err := w.checks.DailyCheckCounts(ctx, in.Visible.Monitors, start, now)
	if err != nil {
		return nil, err
	}
	for _, g := range c.Groups {
		sg := ServiceGroup{Name: g.Name, Monitors: []ServiceMonitor{}}
		for _, id := range g.Monitors {
			m := byID[id]
			if m == nil {
				continue
			}
			row := ServiceMonitor{MonitorID: idFor(in.Viewer, m.ID), Name: m.Name, Status: m.CurrentStatus,
				Days: toBuckets(services.DailyBucketsFromCounts(counts[m.ID], now), "date")}
			if !m.Enabled {
				row.Status = "paused"
			}
			if out.ShowUptime {
				down, err := w.incidents.GetDowntimePercentage(ctx, m.ID, start, now)
				if err != nil {
					return nil, err
				}
				u := math.Round((100-down)*100) / 100
				if m.CurrentStatus == models.StatusOffline && u >= 100 {
					u = 99.99 // an outage right now never reads as 100%
				}
				row.Uptime90d = &u
			}
			out.Total++
			// The spec's "up": the monitor's current status is online.
			if m.CurrentStatus != models.StatusOnline {
				out.Down++
			}
			sg.Monitors = append(sg.Monitors, row)
		}
		if len(sg.Monitors) > 0 {
			out.Groups = append(out.Groups, sg)
		}
	}
	return out, nil
}
```

- [ ] **Step 8: Run the tests to see them pass**

Run: `cd backend && go vet ./... && go test ./internal/dashboards/ ./internal/services/ ./internal/api/`
Expected: PASS.

Run: `cd backend && ./scripts/test-db.sh -run 'TestDBServiceStatus|TestDBPublicLinkLeaksNothing|TestDBMonitorsWidget|TestDBDailyCheckCounts' -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/services/uptime_buckets.go backend/internal/services/check_service.go \
  backend/internal/services/daily_check_counts_db_test.go backend/internal/dashboards/deps.go \
  backend/internal/dashboards/widget_service_status.go backend/internal/dashboards/widget_service_status_db_test.go \
  backend/internal/dashboards/public_leak_db_test.go
git commit -m "feat(dashboards): Service status widget (groups, banner, 90-day bars and uptime)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Frontend helpers and the Service status widget

**Files:**
- Create: `frontend/src/utils/publicDashboards.ts`
- Create: `frontend/src/components/dashboards/widgets/ServiceStatusWidget.tsx`
- Create: `frontend/src/components/dashboards/settings/ServiceStatusSettings.tsx`
- Modify: `frontend/src/types/dashboards.ts` (`WidgetType`, Service status data types)
- Modify: `frontend/src/utils/dashboards.ts` (`WIDGET_TYPES`, `defaultConfig`, `widgetSummary`)
- Modify: `frontend/src/components/dashboards/pickers/MonitorPicker.tsx` (`exclude`, `max`)
- Modify: `frontend/src/components/dashboards/WidgetRenderer.tsx`, `frontend/src/components/dashboards/WidgetSettings.tsx`

**Interfaces:**
- Consumes (Task 5): the `service_status` config and data shapes.
- Produces (`@/utils/publicDashboards`; no imports, so a throwaway check can bundle it):
  - `type PublicVia = 'link' | 'address'`;
  - `publicApiPath(via, key): string` and `publicAddressUrl(origin, slug): string`;
  - `validPublicSlug(s): boolean`, `SLUG_RULE` and `SLUG_FIRST` (the spec's messages);
  - `validThemeColor(c): boolean` (empty is valid), `longHex(c): string | null` and `validLogoUrl(u): boolean` (empty is valid);
  - `bannerText(total, down): string`, `serviceStatusLabel(status): string` and `dayLabel(iso): string`;
  - `moveItem<T>(list, index, delta: -1 | 1): T[]`;
  - `MAX_SERVICE_GROUPS = 20`, `MAX_SERVICE_MONITORS = 100`;
  - `interface ServiceGroupConfig { name: string; monitors: string[] }` and `serviceConfig(c): { groups; showBanner; showUptime }`;
  - types `ServiceMonitor`, `ServiceGroup`, `ServiceStatusData`, and `WidgetType` gains `'service_status'`.

- [ ] **Step 1: Write the failing helper check**

Make a scratch directory outside the repo (e.g. `$TMPDIR/piece4-check`) holding `check.ts`:

```ts
import {
  MAX_SERVICE_GROUPS, MAX_SERVICE_MONITORS, SLUG_FIRST, SLUG_RULE, bannerText, dayLabel, longHex, moveItem,
  publicAddressUrl, publicApiPath, serviceConfig, serviceStatusLabel, validLogoUrl, validPublicSlug, validThemeColor,
} from '/app/src/utils/publicDashboards.ts'

let failures = 0
function expect(name: string, got: unknown, want: unknown) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures++
    console.log(`FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`)
  }
}

expect('banner ok', bannerText(3, 0), 'All systems operational')
expect('banner issues', bannerText(3, 1), '1 of 3 systems experiencing issues')
expect('banner all down', bannerText(2, 2), '2 of 2 systems experiencing issues')

expect('slug short', validPublicSlug('ab'), false)
expect('slug min', validPublicSlug('abc'), true)
expect('slug max', validPublicSlug('a'.repeat(50)), true)
expect('slug long', validPublicSlug('a'.repeat(51)), false)
expect('slug case kept', validPublicSlug('Test-Status'), true)
expect('slug space', validPublicSlug('has space'), false)
expect('slug unicode', validPublicSlug('ünï-x'), false)
expect('slug empty', validPublicSlug(''), false)
expect('slug messages', [SLUG_RULE, SLUG_FIRST], ['the address may only use letters, digits and hyphens (3–50)', 'choose an address first'])

const abc = ['a', 'b', 'c']
expect('move down', moveItem(abc, 0, 1), ['b', 'a', 'c'])
expect('move up', moveItem(abc, 2, -1), ['a', 'c', 'b'])
expect('move past end', moveItem(abc, 2, 1), ['a', 'b', 'c'])
expect('move before start', moveItem(abc, 0, -1), ['a', 'b', 'c'])
expect('move is a copy', moveItem(abc, 0, 1) !== abc && abc.join('') === 'abc', true)

expect('hex short', longHex('#1b9'), '#11bb99')
expect('hex long', longHex('#10B981'), '#10b981')
expect('hex spaces', longHex(' #10b981 '), '#10b981')
expect('hex bad', longHex('red'), null)
expect('hex empty', longHex(''), null)
expect('colour empty ok', validThemeColor(''), true)
expect('colour ok', validThemeColor('#1b9'), true)
expect('colour bad', validThemeColor('10b981'), false)

expect('logo empty ok', validLogoUrl(''), true)
expect('logo https', validLogoUrl('https://x.test/a.png'), true)
expect('logo http', validLogoUrl('http://intranet/logo.svg'), true)
expect('logo javascript', validLogoUrl('javascript:alert(1)'), false)
expect('logo ftp', validLogoUrl('ftp://x.test/a.png'), false)
expect('logo no host', validLogoUrl('https://'), false)
expect('logo too long', validLogoUrl('https://x.test/' + 'a'.repeat(2048)), false)

expect('day', dayLabel('2026-06-27'), 'Jun 27')
expect('day first', dayLabel('2026-01-01'), 'Jan 1')
expect('day bad', dayLabel('bad'), 'bad')
expect('day bad month', dayLabel('2026-13-01'), '2026-13-01')

expect('status online', serviceStatusLabel('online'), 'Operational')
expect('status offline', serviceStatusLabel('offline'), 'Down')
expect('status paused', serviceStatusLabel('paused'), 'Paused')
expect('status other', serviceStatusLabel('pending'), 'Unknown')

expect('api link', publicApiPath('link', 'abc_-1'), '/api/v1/public/dashboards/abc_-1')
expect('api address', publicApiPath('address', 'hq-status'), '/api/v1/public/status/hq-status')
expect('address url', publicAddressUrl('https://s.test', 'hq-status'), 'https://s.test/public/status/hq-status')

expect('config empty', serviceConfig({}), { groups: [], showBanner: true, showUptime: true })
expect(
  'config messy',
  serviceConfig({ groups: [{ name: 1, monitors: ['a', 2] }, null], show_banner: false }),
  { groups: [{ name: '', monitors: ['a'] }, { name: '', monitors: [] }], showBanner: false, showUptime: true },
)
expect('limits', [MAX_SERVICE_GROUPS, MAX_SERVICE_MONITORS], [20, 100])

if (failures) {
  console.log(`${failures} check(s) failed`)
  process.exit(1)
}
console.log('publicDashboards: all checks passed')
```

- [ ] **Step 2: Run it to see it fail**

Run (repo root): `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -v "$TMPDIR/piece4-check":/check -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx esbuild /check/check.ts --bundle --platform=node --outfile=/tmp/check.cjs --log-level=warning && node /tmp/check.cjs"`
Expected: FAIL. esbuild reports `Could not resolve "/app/src/utils/publicDashboards.ts"`.

- [ ] **Step 3: Write the helpers**

`frontend/src/utils/publicDashboards.ts`:

```ts
// Pure helpers for public dashboards and the Service status widget. No
// imports, so a throwaway script can check them on its own.

/** How a public dashboard is reached: its unguessable link or its readable address. */
export type PublicVia = 'link' | 'address'

/** The API path a public dashboard's layout is read from; its widgets' data is under it. */
export function publicApiPath(via: PublicVia, key: string): string {
  return `/api/v1/public/${via === 'link' ? 'dashboards' : 'status'}/${encodeURIComponent(key)}`
}

/** The page a public address name opens. */
export function publicAddressUrl(origin: string, slug: string): string {
  return `${origin}/public/status/${encodeURIComponent(slug)}`
}

const SLUG_RE = /^[a-zA-Z0-9-]+$/

/** The public address rule (the backend's validSlug): 3–50 letters, digits or hyphens. */
export function validPublicSlug(slug: string): boolean {
  return slug.length >= 3 && slug.length <= 50 && SLUG_RE.test(slug)
}

export const SLUG_RULE = 'the address may only use letters, digits and hyphens (3–50)'
export const SLUG_FIRST = 'choose an address first'

const HEX_RE = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/

/** A theme colour is empty (none) or #rgb / #rrggbb. */
export function validThemeColor(c: string): boolean {
  const t = c.trim()
  return t === '' || HEX_RE.test(t)
}

/** c as lower-case #rrggbb, for <input type="color">, which takes only the
 *  long form; null when c is not a colour. */
export function longHex(c: string): string | null {
  const t = c.trim()
  if (!HEX_RE.test(t)) return null
  const long = t.length === 7 ? t : `#${t.slice(1).split('').map((ch) => ch + ch).join('')}`
  return long.toLowerCase()
}

/** A logo address is empty (none) or an http(s) address with a host, at most 2048 characters. */
export function validLogoUrl(u: string): boolean {
  const t = u.trim()
  return t === '' || (t.length <= 2048 && /^https?:\/\/[^\s/?#]+\S*$/i.test(t))
}

/** The Service status banner, in the status page's words. */
export function bannerText(total: number, down: number): string {
  return down === 0 ? 'All systems operational' : `${down} of ${total} systems experiencing issues`
}

/** A monitor's status as a public page words it. */
export function serviceStatusLabel(status: string): string {
  switch (status) {
    case 'online':
      return 'Operational'
    case 'offline':
      return 'Down'
    case 'paused':
      return 'Paused'
    default:
      return 'Unknown'
  }
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/** "Jun 27" for a bar's "2026-06-27"; anything else as given. */
export function dayLabel(iso: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso)
  const month = m ? MONTHS[Number(m[2]) - 1] : undefined
  return m && month ? `${month} ${Number(m[3])}` : iso
}

/** A copy of list with item index moved one place up (-1) or down (1);
 *  unchanged when it is already at that end. */
export function moveItem<T>(list: T[], index: number, delta: -1 | 1): T[] {
  const out = list.slice()
  const to = index + delta
  if (index < 0 || index >= list.length || to < 0 || to >= list.length) return out
  ;[out[index], out[to]] = [out[to], out[index]]
  return out
}

export const MAX_SERVICE_GROUPS = 20
export const MAX_SERVICE_MONITORS = 100

export interface ServiceGroupConfig {
  name: string
  monitors: string[]
}

/** A Service status widget's config, read defensively (a draft may be anything). */
export function serviceConfig(c: Record<string, unknown>): { groups: ServiceGroupConfig[]; showBanner: boolean; showUptime: boolean } {
  const raw: unknown[] = Array.isArray(c.groups) ? c.groups : []
  const groups = raw.map((g) => {
    const o = (g && typeof g === 'object' ? g : {}) as Record<string, unknown>
    return {
      name: typeof o.name === 'string' ? o.name : '',
      monitors: Array.isArray(o.monitors) ? o.monitors.filter((v): v is string => typeof v === 'string') : [],
    }
  })
  return { groups, showBanner: c.show_banner !== false, showUptime: c.show_uptime !== false }
}
```

- [ ] **Step 4: Run the check to see it pass**

Run: the command from Step 2.
Expected: `publicDashboards: all checks passed`.

- [ ] **Step 5: Add the types and the widget registration**

In `frontend/src/types/dashboards.ts`:
- Change `WidgetType`'s last line to `| 'top_n' | 'event_log' | 'device_table' | 'open_incidents' | 'monitors' | 'service_status'`.
- Append:

```ts
export interface ServiceMonitor {
  monitor_id?: string
  name: string
  status: string
  /** Absent when the widget's uptime is switched off. */
  uptime_90d?: number
  days: UptimeBucket[]
}

export interface ServiceGroup {
  name: string
  monitors: ServiceMonitor[]
}

export interface ServiceStatusData {
  show_banner: boolean
  show_uptime: boolean
  total: number
  down: number
  groups: ServiceGroup[]
}
```

In `frontend/src/utils/dashboards.ts`:
- Add after the other imports: `import { serviceConfig } from '@/utils/publicDashboards'`.
- Append to `WIDGET_TYPES` after the `monitors` entry:

  ```ts
    { type: 'service_status', label: 'Service status', description: 'Monitors in groups with 90-day uptime bars, like a public status page.', w: 6, h: 6, minW: 4, minH: 3, timeBased: false },
  ```

- In `defaultConfig`, before `case 'site_power':`:

  ```ts
      case 'service_status':
        return { groups: [{ name: '', monitors: [] }], show_banner: true, show_uptime: true }
  ```

- In `widgetSummary`, before `case 'open_incidents':`:

  ```ts
      case 'service_status': {
        const groups = serviceConfig(c).groups
        return `${groups.reduce((n, g) => n + g.monitors.length, 0)} monitor(s) in ${groups.length} group(s)`
      }
  ```

- [ ] **Step 6: Let the monitor picker leave some out**

Replace `frontend/src/components/dashboards/pickers/MonitorPicker.tsx` with:

```tsx
import { useMonitors } from '@/hooks/useMonitors'

interface Props {
  value: string[]
  onChange: (ids: string[]) => void
  /** Monitors to leave out of the list, e.g. ones another group already shows. */
  exclude?: string[]
  /** How many may be chosen (50 unless set). */
  max?: number
}

export default function MonitorPicker({ value, onChange, exclude = [], max = 50 }: Props) {
  const { monitors, loading } = useMonitors({ page: 1, limit: 500 })
  const shown = monitors.filter((m) => !exclude.includes(m.id))
  const toggle = (id: string) =>
    onChange(value.includes(id) ? value.filter((v) => v !== id) : value.length < max ? [...value, id] : value)
  return (
    <div className="max-h-48 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
      {shown.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : 'No monitors.'}</p>}
      {shown.map((m) => (
        <label key={m.id} className="flex items-center gap-2 text-sm text-slate-300">
          <input type="checkbox" checked={value.includes(m.id)} onChange={() => toggle(m.id)} /> {m.name}
        </label>
      ))}
    </div>
  )
}
```

- [ ] **Step 7: Draw the widget**

`frontend/src/components/dashboards/widgets/ServiceStatusWidget.tsx`:

```tsx
import { Link } from 'react-router-dom'
import type { ServiceStatusData } from '@/types/dashboards'
import { Sparkline, uptimeColor } from '@/components/UptimeSparkline'
import { colors } from '@/utils/colors'
import { bannerText, dayLabel, serviceStatusLabel } from '@/utils/publicDashboards'

const DOT: Record<string, string> = { online: 'bg-emerald-400', offline: 'bg-red-400' }

/** What a status page showed: the banner, then the monitors in their groups,
 *  each with its status, 90-day uptime and one bar a day. */
export default function ServiceStatusWidget({ data, linkable }: { data: ServiceStatusData; linkable: boolean }) {
  const tone = data.down === 0 ? colors.operational : colors.error
  return (
    <div className="space-y-4">
      {data.show_banner && (
        <div className={`rounded-lg border p-3 text-center font-medium ${tone.bg} ${tone.border} ${tone.text}`}>
          {bannerText(data.total, data.down)}
        </div>
      )}
      {(data.groups ?? []).map((g, gi) => (
        <section key={`${gi}-${g.name}`} className="space-y-2">
          {g.name && <h4 className="font-semibold text-slate-200">{g.name}</h4>}
          <ul className="divide-y divide-white/5 rounded-lg border border-white/10">
            {g.monitors.map((m, mi) => (
              <li key={m.monitor_id ?? `${gi}-${mi}`} className="space-y-2 p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex min-w-0 items-baseline gap-2">
                    {linkable && m.monitor_id ? (
                      <Link to={`/monitors/${m.monitor_id}`} className="truncate font-medium text-slate-100 hover:text-primary-400">
                        {m.name}
                      </Link>
                    ) : (
                      <span className="truncate font-medium text-slate-100">{m.name}</span>
                    )}
                    {data.show_uptime && m.uptime_90d != null && (
                      <span className={`whitespace-nowrap text-xs tabular-nums ${uptimeColor(m.uptime_90d)}`}>{m.uptime_90d.toFixed(2)}%</span>
                    )}
                  </div>
                  <span className="flex shrink-0 items-center gap-1.5 text-xs text-slate-300">
                    <span className={`h-2 w-2 rounded-full ${DOT[m.status] ?? 'bg-slate-500'}`} aria-hidden />
                    {serviceStatusLabel(m.status)}
                  </span>
                </div>
                <Sparkline data={m.days.map((d) => ({ label: dayLabel(d.label), uptime: d.uptime, status: d.status }))} className="h-6 w-full" />
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}
```

- [ ] **Step 8: Write the widget's settings**

`frontend/src/components/dashboards/settings/ServiceStatusSettings.tsx`:

```tsx
import { useMemo } from 'react'
import { ArrowDown, ArrowUp, Plus, Trash2, X } from 'lucide-react'
import { useMonitors } from '@/hooks/useMonitors'
import { inputCls, type SettingsProps } from '@/utils/dashboards'
import { MAX_SERVICE_GROUPS, MAX_SERVICE_MONITORS, moveItem, serviceConfig, type ServiceGroupConfig } from '@/utils/publicDashboards'
import MonitorPicker from '@/components/dashboards/pickers/MonitorPicker'

const iconBtn = 'text-slate-400 hover:text-white disabled:opacity-30'

/** Groups of monitors, each with a name, in order; a monitor sits in one group at most. */
export default function ServiceStatusSettings({ config, onChange }: SettingsProps) {
  const { groups, showBanner, showUptime } = serviceConfig(config)
  const { monitors } = useMonitors({ page: 1, limit: 500 })
  const names = useMemo(() => new Map(monitors.map((m) => [m.id, m.name] as const)), [monitors])
  const total = groups.reduce((n, g) => n + g.monitors.length, 0)
  const setGroups = (next: ServiceGroupConfig[]) => onChange({ ...config, groups: next })
  const setGroup = (i: number, g: ServiceGroupConfig) => setGroups(groups.map((x, j) => (j === i ? g : x)))

  return (
    <div className="space-y-4">
      {groups.map((g, i) => {
        const others = groups.flatMap((x, j) => (j === i ? [] : x.monitors))
        return (
          <div key={i} className="space-y-2 rounded-md border border-white/10 p-3">
            <div className="flex items-center gap-2">
              <input
                className={inputCls}
                value={g.name}
                maxLength={100}
                placeholder="Group name (optional)"
                aria-label={`Group ${i + 1} name`}
                onChange={(e) => setGroup(i, { ...g, name: e.target.value })}
              />
              <button type="button" className={iconBtn} disabled={i === 0} onClick={() => setGroups(moveItem(groups, i, -1))} aria-label="Move group up">
                <ArrowUp className="h-4 w-4" />
              </button>
              <button type="button" className={iconBtn} disabled={i === groups.length - 1} onClick={() => setGroups(moveItem(groups, i, 1))} aria-label="Move group down">
                <ArrowDown className="h-4 w-4" />
              </button>
              <button type="button" className="text-slate-400 hover:text-red-400" onClick={() => setGroups(groups.filter((_, j) => j !== i))} aria-label="Remove group">
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
            {g.monitors.length > 0 && (
              <ol className="space-y-1">
                {g.monitors.map((id, k) => (
                  <li key={id} className="flex items-center gap-2 text-sm text-slate-300">
                    <span className="min-w-0 flex-1 truncate">{names.get(id) ?? 'Unknown monitor (deleted, or not visible to you)'}</span>
                    <button type="button" className={iconBtn} disabled={k === 0} onClick={() => setGroup(i, { ...g, monitors: moveItem(g.monitors, k, -1) })} aria-label="Move monitor up">
                      <ArrowUp className="h-3.5 w-3.5" />
                    </button>
                    <button type="button" className={iconBtn} disabled={k === g.monitors.length - 1} onClick={() => setGroup(i, { ...g, monitors: moveItem(g.monitors, k, 1) })} aria-label="Move monitor down">
                      <ArrowDown className="h-3.5 w-3.5" />
                    </button>
                    <button type="button" className="text-slate-400 hover:text-red-400" onClick={() => setGroup(i, { ...g, monitors: g.monitors.filter((m) => m !== id) })} aria-label="Remove monitor">
                      <X className="h-3.5 w-3.5" />
                    </button>
                  </li>
                ))}
              </ol>
            )}
            <MonitorPicker
              value={g.monitors}
              exclude={others}
              max={MAX_SERVICE_MONITORS - others.length}
              onChange={(ids) => setGroup(i, { ...g, monitors: ids })}
            />
          </div>
        )
      })}
      <button
        type="button"
        className="btn-secondary flex items-center gap-2"
        disabled={groups.length >= MAX_SERVICE_GROUPS}
        onClick={() => setGroups([...groups, { name: '', monitors: [] }])}
      >
        <Plus className="h-4 w-4" /> Add a group
      </button>
      <p className="text-xs text-slate-500">
        {total} of {MAX_SERVICE_MONITORS} monitors, in up to {MAX_SERVICE_GROUPS} groups. A group without a name shows its monitors without a heading.
      </p>
      <label className="flex items-center gap-2 text-sm text-slate-300">
        <input type="checkbox" checked={showBanner} onChange={(e) => onChange({ ...config, show_banner: e.target.checked })} />
        Show the overall banner ("All systems operational")
      </label>
      <label className="flex items-center gap-2 text-sm text-slate-300">
        <input type="checkbox" checked={showUptime} onChange={(e) => onChange({ ...config, show_uptime: e.target.checked })} />
        Show each monitor's 90-day uptime
      </label>
    </div>
  )
}
```

In `frontend/src/components/dashboards/WidgetRenderer.tsx`:
- Add `ServiceStatusData` to the `@/types/dashboards` type import.
- Add `import ServiceStatusWidget from '@/components/dashboards/widgets/ServiceStatusWidget'`.
- Add before `default:`:

  ```tsx
      case 'service_status':
        return <ServiceStatusWidget data={data as ServiceStatusData} linkable={linkable} />
  ```

In `frontend/src/components/dashboards/WidgetSettings.tsx`:
- Add `import ServiceStatusSettings from '@/components/dashboards/settings/ServiceStatusSettings'`.
- Add before `default:`:

  ```tsx
      case 'service_status':
        return <ServiceStatusSettings {...props} />
  ```

- [ ] **Step 9: Run the frontend gate**

Run (repo root): the frontend gate command from Global Constraints.
Expected: it finishes with no output from tsc, eslint or vite, and exit code 0.

- [ ] **Step 10: Commit**

```bash
git add frontend/src/utils/publicDashboards.ts frontend/src/utils/dashboards.ts frontend/src/types/dashboards.ts \
  frontend/src/components/dashboards/widgets/ServiceStatusWidget.tsx frontend/src/components/dashboards/settings/ServiceStatusSettings.tsx \
  frontend/src/components/dashboards/pickers/MonitorPicker.tsx frontend/src/components/dashboards/WidgetRenderer.tsx \
  frontend/src/components/dashboards/WidgetSettings.tsx
git commit -m "feat(dashboards-ui): Service status widget and the public dashboard helpers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: The public view, by link or by address, with branding

**Files:**
- Create: `frontend/src/components/dashboards/PublicHeader.tsx`
- Modify: `frontend/src/utils/colors.ts` (`DEFAULT_THEME_COLOR`)
- Modify: `frontend/src/components/dashboards/DisplayShell.tsx` (`branding` prop, wrapping header)
- Modify: `frontend/src/hooks/useWidgetData.ts` (public source)
- Modify: `frontend/src/pages/dashboards/PublicDashboard.tsx` (whole file)
- Modify: `frontend/src/types/dashboards.ts` (`PublicDashboard` branding)
- Modify: `frontend/src/App.tsx` (the two public routes; drop the `PublicStatus` import)

**Interfaces:**
- Consumes (Tasks 3, 4, 6): the API's `GET /api/v1/public/{dashboards|status}/:key[/widgets/:wid/data]` with `logo_url` and `theme_color` in the layout; `publicApiPath`, `longHex` and `PublicVia`.
- Produces:
  - `<PublicHeader name description logoUrl themeColor />`;
  - `DisplayShell`'s optional `branding?: { description: string; logoUrl: string; themeColor: string }`;
  - `WidgetSource`'s public variant `{ kind: 'public'; via: PublicVia; key: string; widgetId: string }`;
  - `<PublicDashboardPage via="link" | "address" />`, routed at `/public/dashboards/:token` and `/public/status/:slug`;
  - `DEFAULT_THEME_COLOR` in `@/utils/colors`.

- [ ] **Step 1: Add the default theme colour**

Append to `frontend/src/utils/colors.ts`:

```ts
// The accent of a public dashboard with no theme colour of its own. Hex, like
// chartPalette, because it is applied as an inline style next to the user's
// own stored colour.
export const DEFAULT_THEME_COLOR = '#10b981'
```

- [ ] **Step 2: Write the public header**

`frontend/src/components/dashboards/PublicHeader.tsx`:

```tsx
import { useState } from 'react'
import { DEFAULT_THEME_COLOR } from '@/utils/colors'
import { longHex } from '@/utils/publicDashboards'

function Logo({ src }: { src: string }) {
  const [broken, setBroken] = useState(false)
  if (broken) return null
  return <img src={src} alt="" className="h-10 w-10 shrink-0 rounded object-contain" onError={() => setBroken(true)} />
}

interface Props {
  name: string
  description: string
  logoUrl: string
  themeColor: string
}

/** The top of a public dashboard: its logo (left out when the picture does not
 *  load), its name and description, and a bar in its theme colour. */
export default function PublicHeader({ name, description, logoUrl, themeColor }: Props) {
  const color = longHex(themeColor) ?? DEFAULT_THEME_COLOR
  return (
    <div className="min-w-0 flex-1">
      <div className="flex items-center gap-3">
        {/* Keyed by the address, so a corrected address gets a fresh try. */}
        {logoUrl && <Logo key={logoUrl} src={logoUrl} />}
        <h1 className="min-w-0 break-words text-3xl font-light text-white">{name}</h1>
      </div>
      {description && <p className="mt-1 break-words text-slate-400">{description}</p>}
      <span className="mt-2 block h-1 w-16 rounded-full" style={{ backgroundColor: color }} aria-hidden />
    </div>
  )
}
```

- [ ] **Step 3: Let the display shell carry the branding**

In `frontend/src/components/dashboards/DisplayShell.tsx`:
- Add `import PublicHeader from '@/components/dashboards/PublicHeader'`.
- Add to `Props` after `title: string`:

  ```ts
    /** A public view's branding; the signed-in wall display has none. */
    branding?: { description: string; logoUrl: string; themeColor: string }
  ```

- Add `branding` to the destructured props.
- Replace `<header className="mb-4 flex items-center gap-4">` and the `<h1 …>{title}</h1>` line after it with:

  ```tsx
      <header className="mb-4 flex flex-wrap items-center gap-4">
        {branding ? (
          <PublicHeader name={title} description={branding.description} logoUrl={branding.logoUrl} themeColor={branding.themeColor} />
        ) : (
          <h1 className="min-w-0 flex-1 truncate text-3xl font-light text-white">{title}</h1>
        )}
  ```

- [ ] **Step 4: Fetch public widget data by link or address**

In `frontend/src/hooks/useWidgetData.ts`:
- Add `import { publicApiPath, type PublicVia } from '@/utils/publicDashboards'`.
- Replace the public variant of `WidgetSource` with `| { kind: 'public'; via: PublicVia; key: string; widgetId: string }`.
- Replace the URL in the public branch of `fetchWidget` with `` `${publicApiPath(src.via, src.key)}/widgets/${src.widgetId}/data` ``.

In `frontend/src/types/dashboards.ts`, add to `PublicDashboard` after `description: string`:

```ts
  logo_url: string
  theme_color: string
```

- [ ] **Step 5: Rewrite the public page**

Replace `frontend/src/pages/dashboards/PublicDashboard.tsx` with:

```tsx
import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import axios from 'axios'
import { Loader2 } from 'lucide-react'
import type { ApiResponse } from '@/types'
import type { PublicDashboard as PublicLayout } from '@/types/dashboards'
import { useDisplayMode } from '@/hooks/useDisplayMode'
import { publicApiPath, type PublicVia } from '@/utils/publicDashboards'
import DisplayShell from '@/components/dashboards/DisplayShell'
import DashboardGrid from '@/components/dashboards/DashboardGrid'
import WidgetBody from '@/components/dashboards/WidgetBody'

const LAYOUT_EVERY_MS = 5 * 60 * 1000

const GONE: Record<PublicVia, string> = {
  link: 'This dashboard link is no longer available.',
  address: 'This page is not available.',
}

/** A public dashboard, by its unguessable link (/public/dashboards/:token) or
 *  its readable address (/public/status/:slug): no login, its branding on top.
 *  The layout is fetched again every 5 minutes, so an admin's edits reach the
 *  screen and an address switched off shows "not available". */
export default function PublicDashboardPage({ via }: { via: PublicVia }) {
  const params = useParams()
  const key = (via === 'link' ? params.token : params.slug) ?? ''
  const [layout, setLayout] = useState<PublicLayout | null>(null)
  const [layoutAt, setLayoutAt] = useState<number | null>(null)
  const [gone, setGone] = useState(false)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)
  const { fullscreen, enter } = useDisplayMode(true)
  const onUpdated = useCallback((t: number) => setLastUpdated((prev) => (prev != null && prev > t ? prev : t)), [])

  // Reset only when the address changes; the 5-minute reload keeps the page on screen.
  useEffect(() => {
    setLayout(null)
    setGone(false)
  }, [via, key])

  useEffect(() => {
    if (!key) return
    let cancelled = false
    const load = () =>
      axios
        .get<ApiResponse<PublicLayout>>(publicApiPath(via, key), { timeout: 60000 })
        .then((res) => {
          if (cancelled) return
          setLayout(res.data.data)
          setLayoutAt(Date.now())
          setGone(false)
        })
        .catch((err: unknown) => {
          // A 404 is final (revoked, switched off, renamed, or its admin was
          // demoted); anything else is a blip, and the next attempt may succeed.
          if (!cancelled && axios.isAxiosError(err) && err.response?.status === 404) setGone(true)
        })
    void load()
    const t = window.setInterval(() => void load(), LAYOUT_EVERY_MS)
    return () => {
      cancelled = true
      window.clearInterval(t)
    }
  }, [via, key])

  if (gone || !key) {
    return <div className="flex min-h-screen items-center justify-center bg-slate-950 p-8 text-center text-2xl font-light text-slate-300">{GONE[via]}</div>
  }
  if (!layout) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-950">
        <Loader2 className="h-8 w-8 animate-spin text-slate-500" />
      </div>
    )
  }
  const empty = layout.widgets.length === 0
  return (
    <DisplayShell
      title={layout.name}
      branding={{ description: layout.description, logoUrl: layout.logo_url, themeColor: layout.theme_color }}
      lastUpdated={empty ? layoutAt : lastUpdated}
      fullscreen={fullscreen}
      onFullscreen={() => void enter()}
    >
      {empty ? (
        <p className="py-16 text-center text-xl font-light text-slate-400">Nothing to show yet.</p>
      ) : (
        <DashboardGrid
          items={layout.widgets}
          editing={false}
          render={(w) => <WidgetBody widget={w} display onUpdated={onUpdated} linkable={false} source={{ kind: 'public', via, key, widgetId: w.id }} />}
        />
      )}
    </DisplayShell>
  )
}
```

- [ ] **Step 6: Route both public addresses to it**

In `frontend/src/App.tsx`:
- Delete the line `const PublicStatus = lazy(() => import('@/pages/PublicStatus'))`.
- Replace the two public routes:

  ```tsx
              <Route path="/public/status/:slug" element={<PublicStatus />} />
              <Route path="/public/dashboards/:token" element={<PublicDashboardPage />} />
  ```

  with:

  ```tsx
              {/* A dashboard published at its readable address or its unguessable link. */}
              <Route path="/public/status/:slug" element={<PublicDashboardPage via="address" />} />
              <Route path="/public/dashboards/:token" element={<PublicDashboardPage via="link" />} />
  ```

(`pages/PublicStatus.tsx` is now unused; Task 9 deletes it.)

- [ ] **Step 7: Run the frontend gate**

Run (repo root): the frontend gate command from Global Constraints.
Expected: exit code 0, with no tsc, eslint or vite errors.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/components/dashboards/PublicHeader.tsx frontend/src/utils/colors.ts \
  frontend/src/components/dashboards/DisplayShell.tsx frontend/src/hooks/useWidgetData.ts \
  frontend/src/pages/dashboards/PublicDashboard.tsx frontend/src/types/dashboards.ts frontend/src/App.tsx
git commit -m "feat(dashboards-ui): public view at /public/status/:slug and the link, with logo, description and colour

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Publishing panel, Appearance, and "public" across the app

**Files:**
- Create: `frontend/src/components/dashboards/CopyField.tsx`
- Create: `frontend/src/components/dashboards/PublicAddressSection.tsx`
- Create: `frontend/src/components/dashboards/PublicLinkSection.tsx`
- Create: `frontend/src/components/dashboards/PublishingPanel.tsx`
- Delete: `frontend/src/components/dashboards/PublicLinkPanel.tsx`
- Modify: `frontend/src/types/dashboards.ts` (`Dashboard`)
- Modify: `frontend/src/hooks/useDashboards.ts` (`SaveDashboardInput`, `setPublicAddress`)
- Modify: `frontend/src/components/dashboards/DashboardEditor.tsx` (Appearance)
- Modify: `frontend/src/pages/dashboards/DashboardPage.tsx`
- Modify: `frontend/src/pages/dashboards/Dashboards.tsx:23`
- Modify: `frontend/src/pages/Overview.tsx:158`
- Modify: `frontend/src/pages/network/SiteDetail.tsx:193,214`

**Interfaces:**
- Consumes:
  - Task 2's dashboard fields `has_link`, `public`, `public_slug`, `public_slug_live`;
  - Task 3's `logo_url` and `theme_color`, plus `logo_url`/`theme_color` on `PUT /dashboards/:id`;
  - Task 4's `PUT /dashboards/:id/public-address`;
  - Task 6's `validPublicSlug`, `publicAddressUrl`, `SLUG_RULE`, `SLUG_FIRST`, `validLogoUrl`, `validThemeColor` and `longHex`;
  - Task 7's `PublicHeader` and `DEFAULT_THEME_COLOR`.
- Produces:
  - `useDashboardActions().setPublicAddress(id, { slug, live }): Promise<DashboardDetail>`;
  - `SaveDashboardInput.logo_url?` and `theme_color?`;
  - `Dashboard` loses `published`.

- [ ] **Step 1: Change the types and the hook**

In `frontend/src/types/dashboards.ts`, replace `published: boolean` in `Dashboard` with:

```ts
  /** Has an unguessable public link. */
  has_link: boolean
  /** Its address is live or it has a link: only an admin may change it. */
  public: boolean
  public_slug: string | null
  public_slug_live: boolean
  logo_url: string
  theme_color: string
```

In `frontend/src/hooks/useDashboards.ts`:
- Add to `SaveDashboardInput` after `description: string`:

  ```ts
    logo_url?: string
    theme_color?: string
  ```

- Add to `useDashboardActions` after `revokePublicLink`:

  ```ts
        setPublicAddress: async (id: string, body: { slug: string; live: boolean }) =>
          (await api.put<ApiResponse<DashboardDetail>>(`/dashboards/${id}/public-address`, body)).data.data,
  ```

Run (repo root): the frontend gate.
Expected: FAIL. tsc reports `Property 'published' does not exist on type 'Dashboard'` in DashboardPage.tsx, Dashboards.tsx, Overview.tsx and SiteDetail.tsx. The next steps fix each of these.

- [ ] **Step 2: Write the copy field and the two sections**

`frontend/src/components/dashboards/CopyField.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import { Copy } from 'lucide-react'
import { inputCls } from '@/utils/dashboards'

/** A read-only address with a Copy button. navigator.clipboard exists only in
 *  a secure context; without it, or when the write is refused, the address is
 *  selected so it can be copied by hand. */
export default function CopyField({ value }: { value: string }) {
  const [copiedValue, setCopiedValue] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const copy = async () => {
    try {
      if (!navigator.clipboard) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(value)
      if (mounted.current) setCopiedValue(value)
    } catch {
      inputRef.current?.select()
    }
  }
  return (
    <div className="flex gap-2">
      <input ref={inputRef} className={inputCls} readOnly value={value} onFocus={(e) => e.target.select()} />
      <button type="button" className="btn-secondary flex items-center gap-1" onClick={() => void copy()}>
        <Copy className="h-4 w-4" /> {copiedValue === value ? 'Copied' : 'Copy'}
      </button>
    </div>
  )
}
```

`frontend/src/components/dashboards/PublicAddressSection.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { DashboardDetail } from '@/types/dashboards'
import { inputCls } from '@/utils/dashboards'
import { SLUG_FIRST, SLUG_RULE, publicAddressUrl, validPublicSlug } from '@/utils/publicDashboards'
import CopyField from '@/components/dashboards/CopyField'

interface Props {
  dashboard: DashboardDetail
  onSaved: (d: DashboardDetail) => void
}

/** The readable address, /public/status/<name>: a status page people can type or bookmark. */
export default function PublicAddressSection({ dashboard, onSaved }: Props) {
  const { setPublicAddress } = useDashboardActions()
  const [name, setName] = useState(dashboard.public_slug ?? '')
  const [live, setLive] = useState(dashboard.public_slug_live)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const slug = name.trim()
  const saved = dashboard.public_slug ?? ''
  const dirty = slug !== saved || live !== dashboard.public_slug_live
  const problem = slug === '' ? (live ? SLUG_FIRST : null) : validPublicSlug(slug) ? null : SLUG_RULE
  const shown = error ?? (dirty ? problem : null)

  const save = async () => {
    if (problem) {
      setError(problem)
      return
    }
    setError(null)
    setBusy(true)
    try {
      const d = await setPublicAddress(dashboard.id, { slug, live })
      if (!mounted.current) return
      setName(d.public_slug ?? '')
      setLive(d.public_slug_live)
      onSaved(d)
    } catch (err) {
      if (mounted.current) setError((err as ApiError).message || 'Could not save the address')
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  return (
    <section className="space-y-3">
      <div>
        <h3 className="font-medium text-slate-100">Public address</h3>
        <p className="text-sm text-slate-400">A readable address, for a status page people can type or bookmark. Clear the name and save to remove it.</p>
      </div>
      <label className="block space-y-1">
        <span className="text-sm text-slate-300">Name</span>
        <input
          className={inputCls}
          value={name}
          maxLength={50}
          placeholder="e.g. it-status"
          onChange={(e) => {
            setName(e.target.value)
            setError(null)
          }}
        />
        <span className="block break-all text-xs text-slate-500">
          {slug ? publicAddressUrl(window.location.origin, slug) : 'Letters, digits and hyphens, 3 to 50.'}
        </span>
      </label>
      <label className="flex items-center gap-2 text-sm text-slate-300">
        <input
          type="checkbox"
          checked={live}
          onChange={(e) => {
            setLive(e.target.checked)
            setError(null)
          }}
        />
        On: the address shows this dashboard. Off keeps the name for later.
      </label>
      {shown && <p className="text-sm text-red-400">{shown}</p>}
      <button type="button" className="btn-primary" disabled={busy || !dirty} onClick={() => void save()}>
        {busy ? 'Saving…' : 'Save address'}
      </button>
      {!dirty && dashboard.public_slug && dashboard.public_slug_live && <CopyField value={publicAddressUrl(window.location.origin, dashboard.public_slug)} />}
      {!dirty && dashboard.public_slug && !dashboard.public_slug_live && (
        <p className="text-sm text-slate-500">The address is off: it shows "not available" until you switch it on.</p>
      )}
    </section>
  )
}
```

`frontend/src/components/dashboards/PublicLinkSection.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { PublicLinkInfo } from '@/types/dashboards'
import CopyField from '@/components/dashboards/CopyField'

interface Props {
  dashboardId: string
  /** The link as last loaded; null while loading. */
  info: PublicLinkInfo | null
  /** The link was created, replaced or revoked. */
  onChanged: () => void
}

/** The unguessable link: for a TV or wall display that stays up for days. */
export default function PublicLinkSection({ dashboardId, info, onChanged }: Props) {
  const actions = useDashboardActions()
  const [confirm, setConfirm] = useState<'regenerate' | 'revoke' | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const url = info?.link ? `${window.location.origin}/public/dashboards/${info.link.token}` : null

  const run = async (fn: () => Promise<unknown>) => {
    setError(null)
    setConfirm(null)
    setBusy(true)
    try {
      await fn()
      onChanged()
    } catch (err) {
      if (mounted.current) setError((err as ApiError).message || 'That did not work')
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  return (
    <section className="space-y-3">
      <div>
        <h3 className="font-medium text-slate-100">Unguessable link</h3>
        <p className="text-sm text-slate-400">A long random address, for a TV that shows this dashboard for days.</p>
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      {busy ? (
        <p className="text-sm text-slate-400">Updating…</p>
      ) : !info ? (
        <p className="text-sm text-slate-500">Loading…</p>
      ) : url ? (
        <>
          <CopyField value={url} />
          {confirm ? (
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="text-amber-300">
                {confirm === 'revoke' ? 'Turn the link off? Screens using it go blank.' : 'Make a new link? The current one stops working at once.'}
              </span>
              <button type="button" className="btn-secondary" onClick={() => setConfirm(null)}>
                Keep it
              </button>
              <button
                type="button"
                className="btn bg-red-600 text-white hover:bg-red-700"
                onClick={() => void run(() => (confirm === 'revoke' ? actions.revokePublicLink(dashboardId) : actions.createPublicLink(dashboardId)))}
              >
                {confirm === 'revoke' ? 'Turn off' : 'Make a new link'}
              </button>
            </div>
          ) : (
            <div className="flex gap-2">
              <button type="button" className="btn-secondary" onClick={() => setConfirm('regenerate')}>
                New link
              </button>
              <button type="button" className="btn-secondary text-red-400" onClick={() => setConfirm('revoke')}>
                Turn off
              </button>
            </div>
          )}
        </>
      ) : (
        <button type="button" className="btn-primary" onClick={() => void run(() => actions.createPublicLink(dashboardId))}>
          Create a public link
        </button>
      )}
    </section>
  )
}
```

- [ ] **Step 3: Write the Publishing panel**

`frontend/src/components/dashboards/PublishingPanel.tsx`:

```tsx
import { useCallback, useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { useDashboardActions } from '@/hooks/useDashboards'
import type { ApiError } from '@/services/api'
import type { DashboardDetail, PublicLinkInfo } from '@/types/dashboards'
import { widgetInfo } from '@/utils/dashboards'
import PublicAddressSection from '@/components/dashboards/PublicAddressSection'
import PublicLinkSection from '@/components/dashboards/PublicLinkSection'

interface Props {
  dashboard: DashboardDetail
  onClose: () => void
  /** The address was saved: the dashboard as it is now. */
  onSaved: (d: DashboardDetail) => void
  /** The link was created, replaced or revoked: refetch the dashboard. */
  onLinkChanged: () => void
}

/** Admin only: the two ways to show a dashboard outside Sentinel, its
 *  readable address and its unguessable link. Rendered in a portal, because a
 *  card's backdrop blur would trap a fixed overlay inside the card. */
export default function PublishingPanel({ dashboard, onClose, onSaved, onLinkChanged }: Props) {
  const actions = useDashboardActions()
  const [info, setInfo] = useState<PublicLinkInfo | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)

  useEffect(() => {
    let cancelled = false
    // Drop the old link until the reload brings the new state, so a dead URL can't be copied.
    setInfo(null)
    actions
      .getPublicLink(dashboard.id)
      .then((i) => {
        if (!cancelled) setInfo(i)
      })
      .catch((err: ApiError) => {
        if (!cancelled) setError(err.message || 'Could not load the link')
      })
    return () => {
      cancelled = true
    }
  }, [actions, dashboard.id, version])

  const linkChanged = useCallback(() => {
    setVersion((v) => v + 1)
    onLinkChanged()
  }, [onLinkChanged])

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="card max-h-[90vh] w-full max-w-lg space-y-5 overflow-y-auto p-6" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between">
          <h2 className="text-lg font-semibold">Publishing</h2>
          <button type="button" className="text-slate-400 hover:text-slate-300" onClick={onClose} aria-label="Close">
            <X className="h-5 w-5" />
          </button>
        </div>
        <p className="text-sm text-slate-400">
          Anyone can see a public dashboard without signing in. IP addresses, hostnames, monitor URLs, error details and notes
          are left out, and nothing on it links into Sentinel. While it is public, only admins can change or delete it.
        </p>
        {info && info.broad_widgets.length > 0 && (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-200">
            These widgets show everything rather than chosen items, so the public view will also show things added later:
            <ul className="mt-1 list-disc pl-5">
              {info.broad_widgets.map((w) => (
                <li key={w.id}>{w.title || widgetInfo(w.type).label}</li>
              ))}
            </ul>
          </div>
        )}
        {error && <p className="text-sm text-red-400">{error}</p>}
        <PublicAddressSection dashboard={dashboard} onSaved={onSaved} />
        <div className="border-t border-white/10" />
        <PublicLinkSection dashboardId={dashboard.id} info={info} onChanged={linkChanged} />
      </div>
    </div>,
    document.body,
  )
}
```

Then: `git rm frontend/src/components/dashboards/PublicLinkPanel.tsx`.

- [ ] **Step 4: Use it on the dashboard page**

In `frontend/src/pages/dashboards/DashboardPage.tsx`:
- Replace `import PublicLinkPanel from '@/components/dashboards/PublicLinkPanel'` with `import PublishingPanel from '@/components/dashboards/PublishingPanel'`.
- Replace `const [linking, setLinking] = useState(false)` with `const [publishing, setPublishing] = useState(false)`.
- In the edit-mode refusal, replace `dashboard.published ? 'This dashboard has a public link, so only an admin can change it.' :` with `dashboard.public ? 'This dashboard is public, so only an admin can change it.' :`.
- Directly after the `<p className="text-sm text-slate-400">…</p>` subtitle (the one showing site name or "Personal" and the description), still inside `<div className="min-w-0">`, add:

  ```tsx
            {dashboard.public && (
              <p className="mt-2 flex flex-wrap gap-2 text-xs">
                {dashboard.public_slug_live && dashboard.public_slug && (
                  <a
                    href={`/public/status/${encodeURIComponent(dashboard.public_slug)}`}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 rounded-full border border-teal-500/30 bg-teal-500/10 px-2 py-0.5 text-teal-300 hover:bg-teal-500/20"
                  >
                    <Globe2 className="h-3 w-3" /> Public at /public/status/{dashboard.public_slug}
                  </a>
                )}
                {dashboard.has_link && (
                  <span className="inline-flex items-center gap-1 rounded-full border border-teal-500/30 bg-teal-500/10 px-2 py-0.5 text-teal-300">
                    <Globe2 className="h-3 w-3" /> Public link on
                  </span>
                )}
              </p>
            )}
  ```

- Replace the admin button with:

  ```tsx
            {currentUser?.is_admin && (
              <button className="btn-secondary flex items-center gap-2" onClick={() => setPublishing(true)}>
                <Globe2 className="h-4 w-4" /> {dashboard.public ? 'Publishing' : 'Publish'}
              </button>
            )}
  ```

- Replace the notice block `{dashboard.published && !dashboard.can_edit && dashboard.access === 'edit' && (…)}` with:

  ```tsx
        {dashboard.public && !dashboard.can_edit && dashboard.access === 'edit' && (
          <p className="rounded-lg border border-teal-500/30 bg-teal-500/10 p-3 text-sm text-teal-200">
            This dashboard is public, so only an admin can change it.
          </p>
        )}
  ```

- Replace the `{linking && <PublicLinkPanel … />}` line with:

  ```tsx
        {publishing && (
          <PublishingPanel dashboard={dashboard} onClose={() => setPublishing(false)} onSaved={setDashboard} onLinkChanged={refetch} />
        )}
  ```

- [ ] **Step 5: Add Appearance to the editor**

In `frontend/src/components/dashboards/DashboardEditor.tsx`:
- Add imports:

  ```ts
  import { DEFAULT_THEME_COLOR } from '@/utils/colors'
  import { longHex, validLogoUrl, validThemeColor } from '@/utils/publicDashboards'
  import PublicHeader from '@/components/dashboards/PublicHeader'
  ```

- After `const [description, setDescription] = useState(dashboard.description)` add:

  ```ts
    const [logoUrl, setLogoUrl] = useState(dashboard.logo_url)
    const [themeColor, setThemeColor] = useState(dashboard.theme_color)
  ```

- Replace the `initial` and `dirty` lines with:

  ```ts
    const initial = useMemo(
      () => JSON.stringify({ n: dashboard.name, d: dashboard.description, l: dashboard.logo_url, c: dashboard.theme_color, w: toDraft(dashboard) }),
      [dashboard],
    )
    const dirty = JSON.stringify({ n: name, d: description, l: logoUrl, c: themeColor, w: widgets }) !== initial
    const logoOk = validLogoUrl(logoUrl)
    const colorOk = validThemeColor(themeColor)
  ```

- In `submit`, add to the `input` object after `description,`:

  ```ts
        logo_url: logoUrl.trim(),
        theme_color: themeColor.trim(),
  ```

- Change the Save button's `disabled` to `disabled={saving || conflict || name.trim() === '' || !logoOk || !colorOk}`.
- Directly after the closing `</div>` of the top row (the one holding Name, Description and the buttons), add:

  ```tsx
        <div className="space-y-3 rounded-lg border border-white/10 p-3">
          <p className="text-xs uppercase tracking-widest text-slate-500">Appearance on public pages</p>
          <div className="flex flex-wrap items-start gap-3">
            <label className="min-w-64 flex-1 space-y-1">
              <span className="text-xs text-slate-400">Logo address</span>
              <input className={inputCls} value={logoUrl} maxLength={2048} placeholder="https://example.org/logo.png" onChange={(e) => setLogoUrl(e.target.value)} />
              {!logoOk && <span className="block text-xs text-red-400">Use an http:// or https:// address of a picture.</span>}
            </label>
            <label className="space-y-1">
              <span className="text-xs text-slate-400">Theme colour</span>
              <span className="flex items-center gap-2">
                <input
                  type="color"
                  className="h-9 w-12 cursor-pointer rounded border border-white/10 bg-transparent"
                  value={longHex(themeColor) ?? DEFAULT_THEME_COLOR}
                  onChange={(e) => setThemeColor(e.target.value)}
                  aria-label="Pick a theme colour"
                />
                <input className={`${inputCls} w-28`} value={themeColor} maxLength={7} placeholder={DEFAULT_THEME_COLOR} onChange={(e) => setThemeColor(e.target.value)} />
              </span>
              {!colorOk && <span className="block text-xs text-red-400">Use #rgb or #rrggbb.</span>}
            </label>
          </div>
          <div className="rounded-md bg-slate-950 p-3">
            <PublicHeader name={name.trim() || 'Dashboard'} description={description} logoUrl={logoOk ? logoUrl.trim() : ''} themeColor={themeColor} />
          </div>
        </div>
  ```

- [ ] **Step 6: Read "public" everywhere else**

- `frontend/src/pages/dashboards/Dashboards.tsx`: replace `{d.published && (` with `{d.public && (`.
- `frontend/src/pages/Overview.tsx`: replace `` : `${dashboards.filter((d) => d.published).length} with a public link`, `` with `` : `${dashboards.filter((d) => d.public).length} public`, ``.
- `frontend/src/pages/network/SiteDetail.tsx`:
  - replace `{d.published && <span className="text-xs text-teal-300">public</span>}` with `{d.public && <span className="text-xs text-teal-300">public</span>}`;
  - replace `` dashboards.some((d) => d.published) ? ` (${dashboards.filter((d) => d.published).length} with a public link)` : '' `` with `` dashboards.some((d) => d.public) ? ` (${dashboards.filter((d) => d.public).length} public)` : '' ``.

Then check that nothing reads the old field:

Run: `grep -rn "\.published\b" frontend/src --include=*.ts --include=*.tsx | grep -v "StatusPage\|statusPage\|PublicStatus\|types/api.ts"`
Expected: no output. Lines left in status page files are removed in Task 9.

- [ ] **Step 7: Run the frontend gate**

Run (repo root): the frontend gate command from Global Constraints.
Expected: exit code 0.

- [ ] **Step 8: Commit**

```bash
git add -A frontend/src/components/dashboards frontend/src/types/dashboards.ts frontend/src/hooks/useDashboards.ts \
  frontend/src/pages/dashboards frontend/src/pages/Overview.tsx frontend/src/pages/network/SiteDetail.tsx
git commit -m "feat(dashboards-ui): Publishing panel (address and link), Appearance in the editor, Public badges

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Remove status pages from the app; status notes

**Files:**
- Delete: `frontend/src/pages/StatusPages.tsx`, `frontend/src/pages/PublicStatus.tsx`, `frontend/src/components/StatusPageForm.tsx`, `frontend/src/components/MonitorList.tsx`, `frontend/src/hooks/useStatusPages.ts`, `frontend/src/hooks/usePublicStatus.ts`
- Modify: `frontend/src/components/DashboardsHeader.tsx` (whole file)
- Modify: `frontend/src/pages/dashboards/Dashboards.tsx` (header use; no `?new=1`)
- Modify: `frontend/src/pages/Overview.tsx` (Status Pages tile)
- Modify: `frontend/src/components/ShimmerStatCard.tsx` (`statusPages` colour)
- Modify: `frontend/src/utils/navigation.ts:28-30`
- Modify: `frontend/src/utils/validators.ts:34-40`
- Modify: `frontend/src/types/api.ts` (the status page types)
- Modify: `frontend/src/App.tsx` (`StatusPages` import and routes)
- Modify: `docs/superpowers/STATUS.md`

**Interfaces:**
- Consumes (Task 8): `Dashboards.tsx` already reads `d.public`.
- Produces:
  - no status page code in the frontend;
  - `/status-pages/*` redirects to `/dashboards`;
  - `DashboardsHeader({ onNewDashboard })`.

- [ ] **Step 1: Delete the status page files**

```bash
git rm frontend/src/pages/StatusPages.tsx frontend/src/pages/PublicStatus.tsx frontend/src/components/StatusPageForm.tsx \
  frontend/src/components/MonitorList.tsx frontend/src/hooks/useStatusPages.ts frontend/src/hooks/usePublicStatus.ts
```

Run (repo root): the frontend gate.
Expected: FAIL. tsc reports that it cannot find module `@/pages/StatusPages` (App.tsx) or `@/hooks/useStatusPages` (Overview.tsx). The steps below fix both.

- [ ] **Step 2: One list, one button**

Replace `frontend/src/components/DashboardsHeader.tsx` with:

```tsx
import { Plus } from 'lucide-react'

/** DashboardsHeader is the top of the Dashboards list. It is one list since
 *  status pages became dashboards that can be published (piece 4). */
export default function DashboardsHeader({ onNewDashboard }: { onNewDashboard: () => void }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="text-4xl font-light text-white">Dashboards</h1>
        <p className="mt-2 text-slate-400">Your own views, site views, screens for the wall, and public status pages.</p>
      </div>
      <button className="btn-primary flex items-center gap-2" onClick={onNewDashboard}>
        <Plus className="h-4 w-4" /> New dashboard
      </button>
    </div>
  )
}
```

In `frontend/src/pages/dashboards/Dashboards.tsx`:
- Change the router import to `import { Link, useNavigate } from 'react-router-dom'`.
- Replace the lines from `const [searchParams, setSearchParams] = useSearchParams()` through the end of `closeCreate` with:

  ```ts
    const [creating, setCreating] = useState(false)
  ```

- Replace `<DashboardsHeader active="dashboards" onNewDashboard={() => setCreating(true)} />` with `<DashboardsHeader onNewDashboard={() => setCreating(true)} />`.
- Replace `<NewDashboardModal onClose={closeCreate} …` with `<NewDashboardModal onClose={() => setCreating(false)} …`.

- [ ] **Step 3: Remove the Overview tile, the nav match and the old routes**

In `frontend/src/pages/Overview.tsx`:
- delete the line `import { useStatusPages } from '@/hooks/useStatusPages'`;
- delete the line `const { pages: statusPages } = useStatusPages()`;
- delete `'statusPages',` from the `useCardShimmer([...])` list;
- delete the whole card object whose `key` is `'statusPages'` (from `{` through `},`);
- delete `statusPages,` from the `sectionCards` dependency list.

In `frontend/src/components/ShimmerStatCard.tsx`:
- remove `| 'statusPages'` from the `colorType` union;
- delete the `statusPages: { … },` style entry.

In `frontend/src/utils/navigation.ts`, replace:

```ts
      // Status pages share the entry: piece 4 of the reorganization turns them
      // into dashboards that can be published.
      { to: '/dashboards', label: 'Dashboards', match: ['/dashboards', '/status-pages'] },
```

with:

```ts
      { to: '/dashboards', label: 'Dashboards', match: ['/dashboards'] },
```

In `frontend/src/utils/validators.ts`, delete `const SLUG_RE = /^[a-zA-Z0-9-]+$/` and the `validateStatusPageSlug` function with its comment. The address rule now lives in `@/utils/publicDashboards`.

In `frontend/src/types/api.ts`, delete everything from the line `// ---- Status pages ----…` up to, but not including, the line `// ---- Notifications (endpoints to be created server-side) ----…`:

```bash
awk '/^\/\/ ---- Status pages/{skip=1} /^\/\/ ---- Notifications/{skip=0} !skip' frontend/src/types/api.ts > frontend/src/types/api.ts.new \
  && mv frontend/src/types/api.ts.new frontend/src/types/api.ts
```

In `frontend/src/App.tsx`:
- delete the line `const StatusPages = lazy(() => import('@/pages/StatusPages'))`;
- replace the four `/status-pages…` routes with:

  ```tsx
                {/* Status pages became dashboards that can be published (piece 4). */}
                <Route path="/status-pages/*" element={<Navigate to="/dashboards" replace />} />
  ```

Then confirm nothing is left:

Run: `grep -rn "status-pages\|StatusPage\|statusPage\|status_page\|PublicStatus\|usePublicStatus\|validateStatusPageSlug" frontend/src`
Expected: only the redirect route line in `App.tsx`.

- [ ] **Step 4: Run the frontend gate**

Run (repo root): the frontend gate command from Global Constraints.
Expected: exit code 0.

- [ ] **Step 5: Update STATUS.md**

In `docs/superpowers/STATUS.md`:
- Change the "Updated" line to `Updated 2026-10-08.`
- Replace the piece 4 row's State cell (`Spec agreed: … plan next`) with ``Built on `feature/ux-piece4-public-dashboards`; plan `plans/2026-10-08-ux-piece4-public-dashboards.md` ``.
- In "### UX reorganization piece 1 (on the dev stack)", change item 3 to: `3. Dashboards is one list (piece 4 removed the Status pages tab); "+ New dashboard" creates a dashboard.`
- In the same list, change item 2's last example from `a status page's editor` to `a dashboard's editor`.
- After the piece 2 owner checks, add:

```markdown
### UX reorganization piece 4 (on the dev stack)

1. Dashboards is one list; there is no Status pages tab; `/status-pages` opens Dashboards.
2. Add a Service status widget with two groups and a few monitors; reorder them; the banner reads "All systems operational", or "N of M systems experiencing issues" when one is down.
3. As an admin, give the dashboard the address `test-status`, switch it on, and open `/public/status/test-status` in a private window (also at phone width): the header shows the logo, name, description and colour, and the widget shows the monitors.
4. Switch it off: the address shows "not available". Switch it on again at the same name.
5. The unguessable link still works alongside the address.
6. As a non-admin who can edit the dashboard, publishing controls are absent, and while it is public the content cannot be changed.
7. The Overview Dashboards tile counts the public dashboard; the Status Pages tile is gone.
8. Take a backup before deploying to production (the migration removes the status page tables, and refuses to run if any status page exists).
```

- [ ] **Step 6: Run everything**

Run: `cd backend && go vet ./... && go test ./...`
Expected: PASS.

Run: `cd backend && ./scripts/test-db.sh`
Expected: PASS (the full DB suite).

Run (repo root): the frontend gate.
Expected: exit code 0.

- [ ] **Step 7: Commit**

```bash
git add -A frontend/src docs/superpowers/STATUS.md
git commit -m "feat(ux): status pages removed from the app; Dashboards is one list; old addresses redirect

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
