# Reporting Overhaul Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the flexible report-template/section system with exactly two fixed report types (Uptime, Incident), add a global default SLA target with a per-monitor override, and give the Uptime Report a real visual: a cumulative-uptime-vs-SLA line chart.

**Architecture:** `Report.TemplateID` (a foreign key into a `report_templates` table of section lists) becomes `Report.ReportType` (a plain `"uptime"`/`"incident"` string). `ReportAggregatorService.AggregateReportData` computes each monitor's effective SLA target (its own override, or a new instance-wide default setting) and clamps every monitor's window to when it actually existed (`MeasurableWindow`, promoted from `report_handler.go` so both files share one implementation), then computes a new cumulative-uptime time series for the graph. `PDFRendererService.RenderReportToPDF` drops its section-name dispatch for a two-way branch, each branch assembling a fixed layout from existing drawing helpers plus one new one (`drawPDFUptimeGraph`) built from `fpdf`'s own line/rect primitives. A large amount of now-dead code (the HTML report path, the timeline/availability/performance/period-comparison sections) is deleted as a direct consequence.

**Tech Stack:** Go (Gin, GORM, Postgres), `github.com/go-pdf/fpdf` v0.9.0, React + TypeScript.

**Spec:** `docs/superpowers/specs/2026-09-23-reporting-overhaul-design.md`

## Global Constraints

- Exactly two report types, `"uptime"` and `"incident"` — no template management UI, no section picker, ever again.
- Existing saved reports, schedules, generation history, and report_templates rows are wiped by the migration. No migration path onto the new schema — confirmed with the operator.
- SLA resolution: a monitor's own `SLATarget` override when set, otherwise the instance-wide `default_sla_target` setting (starts at 99.9%). `0` is the wire sentinel meaning "no override" for both create and update — the same convention `FailureThreshold` and the agent resource thresholds already use, needed because `SLATarget` is a `*float64` and JSON cannot otherwise distinguish "omitted" from "explicitly cleared".
- The Uptime Report's graph plots one aggregate line (cumulative uptime % from the report's start through each sample point, weighted by each monitor's own measurable time) against one flat SLA reference line (the effective SLA averaged across the monitors in scope) — no per-monitor lines.
- No new Go dependency and no image-embedding pipeline for the graph — `fpdf`'s native `Line`/`Rect`/`SetDrawColor` primitives only, matching why this package renders PDFs directly at all (see the package doc comment in `pdf_renderer.go`).
- `backend/internal/services/report_schedule.go` and the scheduler need **no changes** — `ReportSchedule` only ever stores a `ReportID`, never a template reference.
- Report scope (monitors/tags/groups/types) and period (rolling/calendar/custom) pickers carry over unchanged in both the frontend and backend.

---

### Task 1: Migration — drop report templates, add report_type, wipe report data

**Files:**
- Create: `backend/migrations/043_report_types_and_sla.sql`

**Interfaces:**
- Produces: a `reports.report_type` column (`text not null`, checked against `'uptime'`/`'incident'`), replacing `reports.template_id`. Drops `report_templates` entirely. Truncates `reports`, `report_generations`, `report_schedules`, `report_access`. Every later backend task assumes this column exists and `report_templates` does not.

- [ ] **Step 1: Write the migration**

Create `backend/migrations/043_report_types_and_sla.sql`:

```sql
-- Replaces the report template/section system with two fixed report types
-- (uptime, incident), and wipes existing saved reports so nothing carries a
-- stale template reference forward. Confirmed with the operator: a full wipe,
-- no migration path for existing report definitions, schedules, or history.
-- Generated PDF files already on disk for the wiped generations are removed
-- separately as a deploy step — a migration cannot touch the filesystem.

-- ---------------------------------------------------------------------------
-- Wipe existing report data. All four tables in one TRUNCATE so Postgres can
-- satisfy the foreign keys among them without CASCADE: report_generations,
-- report_access and report_schedules all reference reports.id.
-- ---------------------------------------------------------------------------
TRUNCATE TABLE report_generations, report_access, report_schedules, reports;

-- ---------------------------------------------------------------------------
-- Replace the template reference with a plain report type. The table is
-- empty from the truncate above, so adding a NOT NULL column needs no
-- backfill default.
-- ---------------------------------------------------------------------------
ALTER TABLE reports DROP COLUMN IF EXISTS template_id;
ALTER TABLE reports ADD COLUMN IF NOT EXISTS report_type VARCHAR(20) NOT NULL;

ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_report_type_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_report_type_check
    CHECK (report_type IN ('uptime', 'incident'));

-- report_generations never carried a template reference of its own (only
-- report_id, pdf_path, file_size, generated_by), so it needs no column change.

-- ---------------------------------------------------------------------------
-- Drop the template system. Safe now that reports.template_id (its only
-- referencing column) is gone.
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS report_templates;
```

- [ ] **Step 2: Verify the migration applies cleanly against a database with existing rows**

Run (spins up a scratch Postgres, applies every migration up to but not including the new one, inserts a report row referencing a template the old way, applies the new migration, then confirms the wipe, the new column, and its CHECK constraint):

```bash
docker run -d --name report-migration-check -e POSTGRES_DB=sentinel -e POSTGRES_USER=sentinel -e POSTGRES_PASSWORD=test -p 127.0.0.1:55433:5432 postgres:16-alpine
sleep 3
for f in $(ls backend/migrations/*.sql | sort -V | grep -v 043_report_types_and_sla); do
  echo "applying $f"
  PGPASSWORD=test psql -h 127.0.0.1 -p 55433 -U sentinel -d sentinel -f "$f" || break
done

PGPASSWORD=test psql -h 127.0.0.1 -p 55433 -U sentinel -d sentinel -c "
  INSERT INTO users (id, username, password_hash, is_admin)
    VALUES ('11111111-1111-1111-1111-111111111111', 'tester', 'x', true);
  INSERT INTO report_templates (id, name, is_default, sections_json)
    VALUES ('22222222-2222-2222-2222-222222222222', 'Standard Report', true, '[\"sla_compliance\"]');
  INSERT INTO reports (id, user_id, name, template_id, scope_type, scope_data, time_range_days, created_by)
    VALUES ('33333333-3333-3333-3333-333333333333', '11111111-1111-1111-1111-111111111111',
            'Existing report', '22222222-2222-2222-2222-222222222222', 'monitors', '{\"monitor_ids\":[]}',
            30, '11111111-1111-1111-1111-111111111111');
"

echo "applying 043_report_types_and_sla.sql"
PGPASSWORD=test psql -h 127.0.0.1 -p 55433 -U sentinel -d sentinel -f backend/migrations/043_report_types_and_sla.sql

echo "-- reports table is empty:"
PGPASSWORD=test psql -h 127.0.0.1 -p 55433 -U sentinel -d sentinel -c "SELECT count(*) FROM reports;"

echo "-- report_templates is gone:"
PGPASSWORD=test psql -h 127.0.0.1 -p 55433 -U sentinel -d sentinel -c "\dt report_templates"

echo "-- a bad report_type is rejected:"
PGPASSWORD=test psql -h 127.0.0.1 -p 55433 -U sentinel -d sentinel -c "
  INSERT INTO reports (id, user_id, name, report_type, scope_type, scope_data, time_range_days, created_by)
    VALUES (gen_random_uuid(), '11111111-1111-1111-1111-111111111111', 'New report', 'weekly_digest',
            'monitors', '{}', 30, '11111111-1111-1111-1111-111111111111');
" 2>&1 | grep -qi "violates check constraint" && echo "PASS: bad report_type rejected" || echo "FAIL: bad report_type was accepted"

echo "-- a good report_type is accepted:"
PGPASSWORD=test psql -h 127.0.0.1 -p 55433 -U sentinel -d sentinel -c "
  INSERT INTO reports (id, user_id, name, report_type, scope_type, scope_data, time_range_days, created_by)
    VALUES (gen_random_uuid(), '11111111-1111-1111-1111-111111111111', 'New report', 'uptime',
            'monitors', '{}', 30, '11111111-1111-1111-1111-111111111111');
  SELECT count(*) FROM reports;
"

docker rm -f report-migration-check
```

Expected: `reports` count is 0 right after the migration; `\dt report_templates` shows no matching relation; the bad `report_type` insert prints "PASS: bad report_type rejected"; the good insert succeeds and the final count is 1.

- [ ] **Step 3: Commit**

```bash
git add backend/migrations/043_report_types_and_sla.sql
git commit -m "$(cat <<'EOF'
feat(reports): migration to drop report templates and add report_type

Truncates reports/report_generations/report_schedules/report_access and
drops report_templates entirely, replacing reports.template_id with a
checked report_type column ('uptime'/'incident') - the schema half of
replacing the flexible template/section system with two fixed report types.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Report model — ReportType replaces ReportTemplate

**Files:**
- Modify: `backend/internal/models/report.go` (whole file)
- Modify: `backend/internal/models/report_test.go` (whole file)

**Interfaces:**
- Consumes: the `report_type` column from Task 1.
- Produces: `models.ReportTypeUptime = "uptime"`, `models.ReportTypeIncident = "incident"`, `models.ValidReportTypes map[string]bool`, `models.Report.ReportType string` (replacing `TemplateID uuid.UUID`). `models.ReportTemplate`, `models.SectionSLACompliance` and siblings, and `models.ValidReportSections` no longer exist — every later task that touched them must not reference them again.

- [ ] **Step 1: Write the failing test**

Replace `backend/internal/models/report_test.go` in full:

```go
package models

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestReportScopeValidate(t *testing.T) {
	id := uuid.New()

	cases := []struct {
		name      string
		scopeType string
		scope     ReportScope
		wantErr   bool
	}{
		{"monitors with ids", ScopeTypeMonitors, ReportScope{MonitorIDs: []uuid.UUID{id}}, false},
		{"monitors without ids", ScopeTypeMonitors, ReportScope{}, true},
		{"tags with tags", ScopeTypeTags, ReportScope{Tags: []string{"prod"}}, false},
		{"tags without tags", ScopeTypeTags, ReportScope{}, true},
		{"groups with ids", ScopeTypeGroups, ReportScope{GroupIDs: []uuid.UUID{id}}, false},
		{"groups without ids", ScopeTypeGroups, ReportScope{}, true},
		{"unknown scope type", "everything", ReportScope{}, true},
		// A scope carrying the wrong field for its type is empty as far as that
		// type is concerned, and must not silently produce an empty report.
		{"wrong field for type", ScopeTypeTags, ReportScope{MonitorIDs: []uuid.UUID{id}}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.scope.Validate(c.scopeType)
			if c.wantErr && err == nil {
				t.Error("expected an error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// The scope round-trips through JSONB, so Value and Scan must agree.
func TestReportScopeRoundTrip(t *testing.T) {
	original := ReportScope{Tags: []string{"prod", "eu-west"}}

	v, err := original.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	var restored ReportScope
	if err := restored.Scan(v); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(restored.Tags) != 2 || restored.Tags[0] != "prod" {
		t.Errorf("round trip lost data: %+v", restored)
	}

	t.Run("nil scans to an empty scope", func(t *testing.T) {
		var s ReportScope
		if err := s.Scan(nil); err != nil {
			t.Fatalf("Scan(nil): %v", err)
		}
		if len(s.Tags) != 0 || len(s.MonitorIDs) != 0 || len(s.GroupIDs) != 0 {
			t.Errorf("expected an empty scope, got %+v", s)
		}
	})

	t.Run("marshals to the documented shape", func(t *testing.T) {
		b, err := json.Marshal(ReportScope{Tags: []string{"prod"}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(b) != `{"tags":["prod"]}` {
			t.Errorf("got %s, want {\"tags\":[\"prod\"]}", b)
		}
	})
}

func TestReportValidate(t *testing.T) {
	valid := func() *Report {
		return &Report{
			Name:          "Monthly SLA",
			ReportType:    ReportTypeUptime,
			ScopeType:     ScopeTypeTags,
			ScopeData:     ReportScope{Tags: []string{"prod"}},
			TimeRangeDays: 30,
		}
	}

	if err := valid().Validate(); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}

	cases := map[string]func(*Report){
		"missing name":          func(r *Report) { r.Name = "" },
		"missing report type":   func(r *Report) { r.ReportType = "" },
		"unknown report type":   func(r *Report) { r.ReportType = "weekly_digest" },
		"bad scope type":        func(r *Report) { r.ScopeType = "everything" },
		"zero time range":       func(r *Report) { r.TimeRangeDays = 0 },
		"negative time range":   func(r *Report) { r.TimeRangeDays = -7 },
		"scope data mismatched": func(r *Report) { r.ScopeData = ReportScope{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := valid()
			mutate(r)
			if err := r.Validate(); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestReportValidateAcceptsIncidentType(t *testing.T) {
	r := &Report{
		Name:          "Incidents",
		ReportType:    ReportTypeIncident,
		ScopeType:     ScopeTypeMonitors,
		ScopeData:     ReportScope{MonitorIDs: []uuid.UUID{uuid.New()}},
		TimeRangeDays: 7,
	}
	if err := r.Validate(); err != nil {
		t.Errorf("incident report rejected: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/models/... -run TestReport -v`
Expected: FAIL to compile — `Report` has no field `ReportType`, `ReportTypeUptime`/`ReportTypeIncident` are undefined.

- [ ] **Step 3: Rewrite report.go**

Replace `backend/internal/models/report.go` in full:

```go
package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Report scope types: how a report decides which monitors it covers.
const (
	ScopeTypeMonitors = "monitors"
	ScopeTypeTags     = "tags"
	ScopeTypeGroups   = "groups"
	// ScopeTypeTypes covers every monitor of a given check type. Distinct from
	// tags: a tag is something a person applied, a type is what the monitor
	// inherently is, so "all DNS monitors" needs no upkeep as monitors come and
	// go.
	ScopeTypeTypes = "types"
)

// ValidScopeTypes lists the accepted scope_type values.
var ValidScopeTypes = map[string]bool{
	ScopeTypeMonitors: true,
	ScopeTypeTags:     true,
	ScopeTypeGroups:   true,
	ScopeTypeTypes:    true,
}

// Report types. Every report is exactly one of these - there is no template
// system to configure sections from.
const (
	// ReportTypeUptime renders uptime vs. SLA: the summary tiles, a
	// cumulative-uptime graph, and a per-monitor SLA table.
	ReportTypeUptime = "uptime"
	// ReportTypeIncident renders the full incident list for the scope, with
	// root cause and resolution detail.
	ReportTypeIncident = "incident"
)

// ValidReportTypes lists the accepted report_type values.
var ValidReportTypes = map[string]bool{
	ReportTypeUptime:   true,
	ReportTypeIncident: true,
}

// Report access types.
const (
	AccessTypeOwner  = "owner"
	AccessTypeViewer = "viewer"
)

// ReportScope is the JSONB payload on reports.scope_data. Exactly one field is
// populated, matching the row's scope_type.
//
// This is a concrete struct rather than a generic JSON container so the scope
// can be resolved without re-parsing untyped maps at every call site.
type ReportScope struct {
	MonitorIDs []uuid.UUID `json:"monitor_ids,omitempty"`
	Tags       []string    `json:"tags,omitempty"`
	GroupIDs   []uuid.UUID `json:"group_ids,omitempty"`
	// Types holds monitor check types: http, tcp, ping, dns.
	Types []string `json:"types,omitempty"`
}

// Value serializes the scope to JSON for storage.
func (s ReportScope) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Scan deserializes a JSONB value from the database into the scope.
func (s *ReportScope) Scan(value any) error {
	if value == nil {
		*s = ReportScope{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning ReportScope: %w", err)
	}
	return json.Unmarshal(data, s)
}

// Validate checks that the scope carries the field its type requires. An empty
// scope is rejected: a report covering nothing is a configuration mistake, and
// silently producing an empty report hides it.
func (s ReportScope) Validate(scopeType string) error {
	switch scopeType {
	case ScopeTypeMonitors:
		if len(s.MonitorIDs) == 0 {
			return errors.New("scope_data.monitor_ids is required when scope_type is \"monitors\"")
		}
	case ScopeTypeTags:
		if len(s.Tags) == 0 {
			return errors.New("scope_data.tags is required when scope_type is \"tags\"")
		}
	case ScopeTypeGroups:
		if len(s.GroupIDs) == 0 {
			return errors.New("scope_data.group_ids is required when scope_type is \"groups\"")
		}
	case ScopeTypeTypes:
		if len(s.Types) == 0 {
			return errors.New("scope_data.types is required when scope_type is \"types\"")
		}
		// Checked here rather than left to the query: an unknown type would
		// silently resolve to no monitors and produce an empty report that
		// looks like "nothing happened".
		for _, t := range s.Types {
			if !ReportableMonitorTypes[t] {
				return errors.New("unknown monitor type in scope_data.types: " + t)
			}
		}
	default:
		return errors.New("unknown scope_type: " + scopeType)
	}
	return nil
}

// Report is a saved report definition. Generating it produces a ReportGeneration.
type Report struct {
	ID            uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	UserID        uuid.UUID   `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	Name          string      `json:"name" gorm:"column:name;not null"`
	ReportType    string      `json:"report_type" gorm:"column:report_type;not null"`
	ScopeType     string      `json:"scope_type" gorm:"column:scope_type;not null"`
	ScopeData     ReportScope `json:"scope_data" gorm:"column:scope_data;type:jsonb;not null"`
	TimeRangeDays int         `json:"time_range_days" gorm:"column:time_range_days;not null"`
	// How the window is worked out: rolling (TimeRangeDays back from now),
	// calendar (a whole month/quarter/week), or custom (explicit bounds).
	// Empty means rolling, which is what every row written before periods
	// existed is.
	PeriodKind        string     `json:"period_kind" gorm:"column:period_kind;default:rolling"`
	PeriodUnit        string     `json:"period_unit" gorm:"column:period_unit"`
	PeriodOffset      int        `json:"period_offset" gorm:"column:period_offset;default:0"`
	PeriodStart       *time.Time `json:"period_start" gorm:"column:period_start"`
	PeriodEnd         *time.Time `json:"period_end" gorm:"column:period_end"`
	CustomTitle       *string    `json:"custom_title" gorm:"column:custom_title"`
	CustomDescription *string    `json:"custom_description" gorm:"column:custom_description"`
	CreatedAt         time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt         time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
	CreatedBy         uuid.UUID  `json:"created_by" gorm:"column:created_by;type:uuid;not null"`
}

// TableName tells GORM which table backs the Report model.
func (Report) TableName() string {
	return "reports"
}

// Validate checks the report definition before it is persisted.
func (r *Report) Validate() error {
	if r.Name == "" {
		return errors.New("report name is required")
	}
	if !ValidReportTypes[r.ReportType] {
		return errors.New("report_type must be one of: uptime, incident")
	}
	if !ValidScopeTypes[r.ScopeType] {
		return errors.New("scope_type must be one of: monitors, tags, groups")
	}
	if err := r.ValidatePeriod(); err != nil {
		return err
	}
	return r.ScopeData.Validate(r.ScopeType)
}

// ReportGeneration is one rendered PDF, kept as history so a shared link can
// resolve to the exact artifact that was produced.
type ReportGeneration struct {
	ID          uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	ReportID    uuid.UUID `json:"report_id" gorm:"column:report_id;type:uuid;not null"`
	GeneratedAt time.Time `json:"generated_at" gorm:"column:generated_at;autoCreateTime"`
	PDFPath     string    `json:"pdf_path" gorm:"column:pdf_path;not null"`
	FileSize    *int      `json:"file_size" gorm:"column:file_size"`
	GeneratedBy uuid.UUID `json:"generated_by" gorm:"column:generated_by;type:uuid;not null"`
}

// TableName tells GORM which table backs the ReportGeneration model.
func (ReportGeneration) TableName() string {
	return "report_generations"
}

// ReportAccess grants a user or a share token access to a report. UserID is nil
// for a public share token that is not tied to an account.
type ReportAccess struct {
	ID         uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	ReportID   uuid.UUID  `json:"report_id" gorm:"column:report_id;type:uuid;not null"`
	UserID     *uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid"`
	AccessType string     `json:"access_type" gorm:"column:access_type;not null"`
	ShareToken *string    `json:"share_token,omitempty" gorm:"column:share_token"`
	// ExpiresAt bounds a share link's lifetime. Nil means it never expires,
	// which is what links created before expiry existed remain.
	ExpiresAt *time.Time `json:"expires_at" gorm:"column:expires_at"`
	CreatedAt time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// IsExpired reports whether this grant has lapsed. A grant with no expiry never
// lapses.
func (ra *ReportAccess) IsExpired(now time.Time) bool {
	return ra.ExpiresAt != nil && !ra.ExpiresAt.After(now)
}

// TableName tells GORM which table backs the ReportAccess model.
func (ReportAccess) TableName() string {
	return "report_access"
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/models/... -run TestReport -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/models/report.go backend/internal/models/report_test.go
git commit -m "$(cat <<'EOF'
feat(reports): replace ReportTemplate with a fixed ReportType

Report.TemplateID becomes Report.ReportType ("uptime" | "incident"),
removing the ReportTemplate model, its section constants, and
ValidReportSections. Every consumer is updated in later tasks.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: SLA settings infrastructure

**Files:**
- Modify: `backend/internal/models/setting.go`
- Modify: `backend/internal/services/settings_service.go`

**Interfaces:**
- Produces: `models.SettingDefaultSLATarget = "default_sla_target"`, `models.DefaultSLATargetPercent = 99.9`, `models.MinSLATargetPercent = 0.0`, `models.MaxSLATargetPercent = 100.0`, `(*SettingsService).GetFloat(ctx, key string, fallback float64) float64`, `(*SettingsService).SetFloat(ctx, key string, value float64) error`, `(*SettingsService).DefaultSLATarget(ctx) float64`. Tasks 4, 5, and 7 consume `DefaultSLATarget`.

Note on testing: `GetInt`/`SetInt` and `IncidentRetentionDays` — the two closest existing accessors — have no unit tests of their own (confirmed: no `settings_service_test.go` exists in this repo, and none of `SettingsService`'s methods require a database in any test today). `GetFloat`/`SetFloat`/`DefaultSLATarget` mirror them exactly and are left at the same level of coverage rather than introducing new test-database infrastructure this codebase does not otherwise have. Correctness here is checked by compiling and by the higher-level `EffectiveSLATarget` unit test in Task 7, which is a pure function and does not need a database.

- [ ] **Step 1: Add the setting key and bounds to setting.go**

In `backend/internal/models/setting.go`, add to the `Setting keys` const block (after `SettingReportTimezone`):

```go
	// SettingDefaultSLATarget is the uptime percentage a monitor is held to
	// when it carries no override of its own.
	SettingDefaultSLATarget = "default_sla_target"
```

Add to the "Bounds and defaults" const block (after `MaxIncidentRetentionDays`):

```go

	// DefaultSLATargetPercent is the instance-wide SLA target until an admin
	// sets one, and the fallback a monitor's own override replaces.
	DefaultSLATargetPercent = 99.9
	MinSLATargetPercent     = 0.0
	MaxSLATargetPercent     = 100.0
```

- [ ] **Step 2: Add GetFloat/SetFloat to settings_service.go**

In `backend/internal/services/settings_service.go`, add immediately after `SeedInt` (before the `AppName` method):

```go

// GetFloat returns a float64 setting, falling back when absent or unparseable.
func (s *SettingsService) GetFloat(ctx context.Context, key string, fallback float64) float64 {
	raw, ok, err := s.getString(ctx, key)
	if err != nil {
		s.logger.Printf("[settings] %v; using default %g for %q", err, fallback, key)
		return fallback
	}
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		s.logger.Printf("[settings] value %q for %q is not a float; using default %g", raw, key, fallback)
		return fallback
	}
	return parsed
}

// SetFloat stores a float64 setting.
func (s *SettingsService) SetFloat(ctx context.Context, key string, value float64) error {
	return s.setString(ctx, key, strconv.FormatFloat(value, 'f', -1, 64))
}
```

- [ ] **Step 3: Add the DefaultSLATarget accessor**

In the same file, add immediately after `IncidentRetentionDays` (before the `RegistrationEnabled` comment):

```go

// DefaultSLATarget returns the instance-wide SLA target, clamped to a sane
// range the same way IncidentRetentionDays clamps its own stored value.
func (s *SettingsService) DefaultSLATarget(ctx context.Context) float64 {
	target := s.GetFloat(ctx, models.SettingDefaultSLATarget, models.DefaultSLATargetPercent)
	if target < models.MinSLATargetPercent || target > models.MaxSLATargetPercent {
		s.logger.Printf("[settings] stored %s=%g is out of range; using %g",
			models.SettingDefaultSLATarget, target, models.DefaultSLATargetPercent)
		return models.DefaultSLATargetPercent
	}
	return target
}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd backend && go build ./...`
Expected: no errors. `strconv` and `strings` are already imported by this file (used by `GetInt`/`GetBool`/`BaseURL`), so no import changes are needed.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/models/setting.go backend/internal/services/settings_service.go
git commit -m "$(cat <<'EOF'
feat(settings): add a default SLA target setting

GetFloat/SetFloat mirror the existing GetInt/SetInt pair, and
DefaultSLATarget clamps the stored value the same way
IncidentRetentionDays does. Not yet exposed over the API or used by
report generation - that follows in later tasks.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Monitor SLA target — validation and update handling

**Files:**
- Modify: `backend/internal/models/monitor.go:255-298` (`Validate`)
- Modify: `backend/internal/models/monitor_validate_test.go`
- Modify: `backend/internal/services/monitor_service.go:133-180` (`applyMonitorUpdates`)
- Create: `backend/internal/services/monitor_service_sla_test.go`
- Modify: `backend/internal/api/monitor_handler.go:156-190` (`CreateMonitorHandler`)

**Interfaces:**
- Consumes: `models.MinSLATargetPercent`/`MaxSLATargetPercent` (Task 3).
- Produces: `Monitor.SLATarget` (already existed) is now validated and is handled on update. `0` is the sentinel meaning "no override" on both the create and update paths — Task 7's `EffectiveSLATarget`/report aggregation assumes a monitor's stored `SLATarget` is either `nil` or a real 1-100 value, never a stray `0`.

`CreateMonitorHandler` already binds directly to `models.Monitor` (`c.ShouldBindJSON(&monitor)`), so `sla_target` is already accepted on create with no DTO change — only the zero-normalization below is new there.

- [ ] **Step 1: Write the failing validation test**

In `backend/internal/models/monitor_validate_test.go`, add at the end of the file:

```go

func f64ptr(v float64) *float64 { return &v }

func TestMonitorValidateSLATarget(t *testing.T) {
	base := func() *Monitor {
		return &Monitor{Name: "x", Type: MonitorTypeHTTP, URL: "https://example.com", IntervalSeconds: 60, TimeoutSeconds: 10}
	}

	cases := []struct {
		name    string
		target  *float64
		wantErr bool
	}{
		{"nil is valid (no override)", nil, false},
		{"zero is valid (treated as unset)", f64ptr(0), false},
		{"a realistic target is valid", f64ptr(99.9), false},
		{"the maximum is valid", f64ptr(100), false},
		{"negative is rejected", f64ptr(-1), true},
		{"over 100 is rejected", f64ptr(100.5), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := base()
			m.SLATarget = c.target
			err := m.Validate()
			if c.wantErr && err == nil {
				t.Error("expected an error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/models/... -run TestMonitorValidateSLATarget -v`
Expected: FAIL — negative and over-100 targets are currently accepted (no bounds check exists yet).

- [ ] **Step 3: Add the bounds check to Monitor.Validate**

In `backend/internal/models/monitor.go`, inside `Validate()`, immediately after the existing `FailureThreshold` check and before the trailing `return nil`:

```go
	// Zero is accepted as "not supplied" - normalized to nil by the handlers
	// before Validate ever sees it in practice (the same convention
	// FailureThreshold uses), since SLATarget is a *float64 and the wire
	// format cannot otherwise distinguish "omitted" from "explicitly
	// cleared". A non-zero value out of bounds is still rejected here.
	if m.SLATarget != nil && *m.SLATarget != 0 &&
		(*m.SLATarget < MinSLATargetPercent || *m.SLATarget > MaxSLATargetPercent) {
		return fmt.Errorf("sla_target must be between %.1f and %.1f, got %.2f",
			MinSLATargetPercent, MaxSLATargetPercent, *m.SLATarget)
	}
```

`MinSLATargetPercent`/`MaxSLATargetPercent` live in `setting.go`, same package — no import change needed.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/models/... -run TestMonitorValidateSLATarget -v`
Expected: PASS.

- [ ] **Step 5: Write the failing update-handling test**

Create `backend/internal/services/monitor_service_sla_test.go`:

```go
package services

import (
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestApplyMonitorUpdatesSLATarget(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }

	t.Run("nil update leaves the existing override alone", func(t *testing.T) {
		target := &models.Monitor{SLATarget: f64(99.5)}
		applyMonitorUpdates(target, &models.Monitor{})
		if target.SLATarget == nil || *target.SLATarget != 99.5 {
			t.Errorf("SLATarget = %v, want unchanged 99.5", target.SLATarget)
		}
	})

	t.Run("a real value sets the override", func(t *testing.T) {
		target := &models.Monitor{}
		applyMonitorUpdates(target, &models.Monitor{SLATarget: f64(99.9)})
		if target.SLATarget == nil || *target.SLATarget != 99.9 {
			t.Errorf("SLATarget = %v, want 99.9", target.SLATarget)
		}
	})

	t.Run("an explicit zero clears an existing override", func(t *testing.T) {
		target := &models.Monitor{SLATarget: f64(99.5)}
		applyMonitorUpdates(target, &models.Monitor{SLATarget: f64(0)})
		if target.SLATarget != nil {
			t.Errorf("SLATarget = %v, want nil (cleared)", target.SLATarget)
		}
	})
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `cd backend && go test ./internal/services/... -run TestApplyMonitorUpdatesSLATarget -v`
Expected: FAIL — `applyMonitorUpdates` does not touch `SLATarget` at all yet, so the "sets the override" and "clears an existing override" subtests fail.

- [ ] **Step 7: Add SLATarget handling to applyMonitorUpdates**

In `backend/internal/services/monitor_service.go`, inside `applyMonitorUpdates`, immediately after the `NotifyChannels` block and before `target.Enabled = updates.Enabled`:

```go
	// A pointer field cannot otherwise distinguish "the caller omitted this"
	// from "the caller wants it cleared" - both unmarshal to nil. 0 is the
	// sentinel for "clear the override, use the system default", the same
	// convention FailureThreshold and the agent resource thresholds use for
	// their own optional numeric fields.
	if updates.SLATarget != nil {
		if *updates.SLATarget == 0 {
			target.SLATarget = nil
		} else {
			target.SLATarget = updates.SLATarget
		}
	}
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `cd backend && go test ./internal/services/... -run TestApplyMonitorUpdatesSLATarget -v`
Expected: PASS.

- [ ] **Step 9: Normalize an explicit zero on create too**

In `backend/internal/api/monitor_handler.go`, inside `CreateMonitorHandler`, immediately after the interval-default block and before `monitor.Validate()`:

```go
		// The same 0-means-clear sentinel UpdateMonitor uses for this field, so
		// a client that always sends 0 for "no override" behaves identically
		// whether it is creating or editing.
		if monitor.SLATarget != nil && *monitor.SLATarget == 0 {
			monitor.SLATarget = nil
		}
```

- [ ] **Step 10: Run the full backend test suite**

Run: `cd backend && go build ./... && go test ./...`
Expected: builds and all tests pass.

- [ ] **Step 11: Commit**

```bash
git add backend/internal/models/monitor.go backend/internal/models/monitor_validate_test.go \
        backend/internal/services/monitor_service.go backend/internal/services/monitor_service_sla_test.go \
        backend/internal/api/monitor_handler.go
git commit -m "$(cat <<'EOF'
feat(monitors): validate and support editing the SLA target

Monitor.Validate now bounds-checks SLATarget (0 means unset), and
applyMonitorUpdates/CreateMonitorHandler both treat an explicit 0 as
"clear the override" - the only way to signal that through a *float64
field, which otherwise cannot distinguish omitted from explicitly
cleared.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Settings API — expose default_sla_target

**Files:**
- Modify: `backend/internal/api/settings_handler.go`
- Modify: `backend/internal/api/auth_handler.go`

**Interfaces:**
- Consumes: `SettingsService.DefaultSLATarget`/`SetFloat` (Task 3).
- Produces: `GET /api/v1/settings` and `PATCH /api/v1/settings/system` both carry `default_sla_target`. `GET /api/v1/auth/status` also carries it, the same way it already carries `default_check_interval` (public, needed by the monitor-create form, not a secret). Task 13 (frontend Settings page) and Task 14 (frontend monitor form's helper text) consume this field.

No existing test file covers this handler (`settings_handler_test.go` does not exist), matching the rest of `settings_handler.go` — this task is implementation-only, verified by a build and a manual curl.

- [ ] **Step 1: Add the field to GetSettingsHandler's response**

In `backend/internal/api/settings_handler.go`, inside `GetSettingsHandler`, replace:

```go
			"report_timezone":        settingsService.ReportTimezone(ctx),
		})
```

with:

```go
			"report_timezone":        settingsService.ReportTimezone(ctx),
			"default_sla_target":     settingsService.DefaultSLATarget(ctx),
		})
```

- [ ] **Step 2: Add the field to updateSystemRequest**

In the same file, replace:

```go
	// ReportTimezone is the IANA zone rendered reports are written in.
	ReportTimezone *string `json:"report_timezone"`
}
```

with:

```go
	// ReportTimezone is the IANA zone rendered reports are written in.
	ReportTimezone *string `json:"report_timezone"`
	// DefaultSLATarget is the uptime percentage a monitor is held to when it
	// carries no override of its own.
	DefaultSLATarget *float64 `json:"default_sla_target"`
}
```

- [ ] **Step 3: Handle it in UpdateSystemSettingsHandler**

In the same file, inside `UpdateSystemSettingsHandler`, add immediately after the `CheckRetentionDays` block and before the `SentinelExternalURL`/`SentinelInternalURL` loop:

```go
		if req.DefaultSLATarget != nil {
			n := *req.DefaultSLATarget
			if n < models.MinSLATargetPercent || n > models.MaxSLATargetPercent {
				respondError(c, http.StatusBadRequest,
					fmt.Sprintf("default_sla_target must be between %.1f and %.1f",
						models.MinSLATargetPercent, models.MaxSLATargetPercent))
				return
			}
			if err := settingsService.SetFloat(ctx, models.SettingDefaultSLATarget, n); err != nil {
				respondInternal(c, "UpdateSystemSettingsHandler", err)
				return
			}
		}
```

- [ ] **Step 4: Add it to the handler's response**

In the same function, replace:

```go
			"check_retention_days":   settingsService.CheckRetentionDays(ctx),
			"sentinel_external_url":  settingsService.GetString(ctx, models.SettingSentinelExternalURL, ""),
			"sentinel_internal_url":  settingsService.GetString(ctx, models.SettingSentinelInternalURL, ""),
			"message":                "System settings updated",
		})
```

with:

```go
			"check_retention_days":   settingsService.CheckRetentionDays(ctx),
			"sentinel_external_url":  settingsService.GetString(ctx, models.SettingSentinelExternalURL, ""),
			"sentinel_internal_url":  settingsService.GetString(ctx, models.SettingSentinelInternalURL, ""),
			"default_sla_target":     settingsService.DefaultSLATarget(ctx),
			"message":                "System settings updated",
		})
```

- [ ] **Step 5: Add it to the public auth-status response too**

The monitor-create form (Task 14) needs the current default to show in its
helper text, and any authenticated user may create a monitor while
`/settings` is admin-only — the same reason `default_check_interval`
already rides on this endpoint.

In `backend/internal/api/auth_handler.go`, inside `AuthStatusHandler`,
change:

```go
		respondSuccess(c, http.StatusOK, gin.H{
			"registration_enabled":   settingsService.RegistrationEnabled(ctx),
			"setup_required":         !hasUsers,
			"app_name":               settingsService.AppName(ctx),
			"default_check_interval": settingsService.DefaultCheckInterval(ctx, models.DefaultMonitorCheckInterval),
			// Readable by everyone, changeable only by an admin via /settings:
			// the UI formats timestamps with it so the screen and a rendered
			// report agree, and it is a display convention, not a secret.
			"report_timezone": settingsService.ReportTimezone(ctx),
		})
```

to:

```go
		respondSuccess(c, http.StatusOK, gin.H{
			"registration_enabled":   settingsService.RegistrationEnabled(ctx),
			"setup_required":         !hasUsers,
			"app_name":               settingsService.AppName(ctx),
			"default_check_interval": settingsService.DefaultCheckInterval(ctx, models.DefaultMonitorCheckInterval),
			// Readable by everyone, changeable only by an admin via /settings:
			// the UI formats timestamps with it so the screen and a rendered
			// report agree, and it is a display convention, not a secret.
			"report_timezone": settingsService.ReportTimezone(ctx),
			// Same reasoning as default_check_interval above: the monitor-create
			// form shows this in its SLA Target helper text, and it is a UI
			// default, not a secret.
			"default_sla_target": settingsService.DefaultSLATarget(ctx),
		})
```

- [ ] **Step 6: Verify it compiles and behaves**

Run: `cd backend && go build ./...`
Expected: no errors.

Then, against a running dev stack:

```bash
curl -s -b cookies.txt http://localhost:3001/api/v1/settings | grep default_sla_target
curl -s -b cookies.txt -X PATCH http://localhost:3001/api/v1/settings/system \
  -H 'Content-Type: application/json' -d '{"default_sla_target": 99.5}' | grep default_sla_target
curl -s -b cookies.txt -X PATCH http://localhost:3001/api/v1/settings/system \
  -H 'Content-Type: application/json' -d '{"default_sla_target": 150}'
curl -s http://localhost:3001/api/v1/auth/status | grep default_sla_target
```

Expected: the first two `/settings` calls show `"default_sla_target":99.5`
(or the current value on the first call); the third returns a 400 with a
message naming the 0-100 bound; the fourth (unauthenticated) also shows
the current value, confirming it rides on the public status endpoint.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/settings_handler.go backend/internal/api/auth_handler.go
git commit -m "$(cat <<'EOF'
feat(settings): expose default_sla_target over the settings API

GET /settings and PATCH /settings/system both carry the instance-wide
SLA target now, validated against the same 0-100 bound the monitor
form uses. It also rides on the public /auth/status response, the same
way default_check_interval already does, so the monitor-create form
can show the real current default in its helper text.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Report period cleanup — remove PreviousPeriod

**Files:**
- Modify: `backend/internal/models/report_period.go:157-194` (delete)
- Modify: `backend/internal/models/report_period_test.go:148-183` (delete)

**Interfaces:**
- Produces: nothing new. `Report.PreviousPeriod`/`Report.PreviousPeriodLabel` no longer exist — confirmed via grep to have exactly one caller, the period-comparison block in `report_aggregator.go`'s `AggregateReportData`, which Task 7 (the very next task) removes as dead code (the comparison-against-the-previous-period feature is not part of either new report type). This task runs immediately before Task 7, not earlier, specifically so that gap is one task wide: deleting these two functions breaks `report_aggregator.go`'s compile (its only caller) until Task 7 removes that caller, and no other task in between needs `report_aggregator.go`, `go build ./...`, or `go test ./...` to succeed.

- [ ] **Step 1: Delete the two PreviousPeriod tests**

In `backend/internal/models/report_period_test.go`, delete these two functions (the file's last 36 lines, from the comment through the closing brace of the second test):

```go
// The comparison for September is August, and August must be whole even when
// September is only partly over — comparing a full month against a partial one
// would make every in-progress report look like an improvement.
func TestPreviousPeriod_CalendarIsWholePrecedingUnit(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	r := &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitMonth} // September, in progress

	start, end := r.PreviousPeriod(now, loc)
	if got := start.In(loc).Format("2006-01-02"); got != "2026-08-01" {
		t.Errorf("previous start = %s, want 2026-08-01", got)
	}
	if got := end.In(loc).Format("2006-01-02"); got != "2026-09-01" {
		t.Errorf("previous end = %s, want 2026-09-01 (a whole month)", got)
	}
	if got := r.PreviousPeriodLabel(now, loc); got != "August 2026" {
		t.Errorf("label = %q, want August 2026", got)
	}
}

// A rolling window has no calendar predecessor, so it compares against the same
// span immediately before it.
func TestPreviousPeriod_RollingIsSameSpanBefore(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	r := &Report{TimeRangeDays: 7}

	curStart, _ := r.ResolvePeriod(now, nil)
	start, end := r.PreviousPeriod(now, nil)

	if !end.Equal(curStart) {
		t.Errorf("previous period ends at %v, want it to meet the current start %v", end, curStart)
	}
	if d := end.Sub(start).Hours(); d != 7*24 {
		t.Errorf("previous span = %v hours, want 168", d)
	}
}
```

Leave every earlier test in the file untouched.

- [ ] **Step 2: Run the test suite to verify it still compiles and passes**

Run: `cd backend && go test ./internal/models/... -run TestPreviousPeriod -v`
Expected: `testing: warning: no tests to run` (both tests are gone) with exit code 0 — not a failure.

- [ ] **Step 3: Delete PreviousPeriod and PreviousPeriodLabel**

In `backend/internal/models/report_period.go`, delete these two functions (the file's last 38 lines):

```go
// PreviousPeriod is the equivalent window immediately before this one, used to
// say whether things got better or worse.
//
// For a calendar period that is the preceding unit — August for September —
// which is the comparison people actually mean. For a rolling or custom window
// it is the same span ending where this one starts, since there is no calendar
// predecessor to name.
func (r *Report) PreviousPeriod(now time.Time, loc *time.Location) (start, end time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	if r.PeriodKind == PeriodCalendar {
		// Taken straight from calendarWindow rather than ResolvePeriod: the
		// latter clamps a period in progress to now, and the predecessor is
		// complete. Comparing a full month against a partial one is what makes
		// a comparison meaningless.
		return calendarWindow(now.In(loc), r.PeriodUnit, r.PeriodOffset+1, loc)
	}

	curStart, curEnd := r.ResolvePeriod(now, loc)
	span := curEnd.Sub(curStart)
	return curStart.Add(-span), curStart
}

// PreviousPeriodLabel names that window.
func (r *Report) PreviousPeriodLabel(now time.Time, loc *time.Location) string {
	if r.PeriodKind == PeriodCalendar {
		shifted := *r
		shifted.PeriodOffset = r.PeriodOffset + 1
		return shifted.PeriodLabel(now, loc)
	}
	start, end := r.PreviousPeriod(now, loc)
	if loc == nil {
		loc = time.UTC
	}
	return fmt.Sprintf("%s to %s",
		start.In(loc).Format("Jan 2"), end.In(loc).Format("Jan 2"))
}
```

The file's `import` block (`errors`, `fmt`, `time`) is unchanged — `fmt` is still used by `ValidatePeriod`, `PeriodLabel`, and `calendarWindow`'s neighbors.

- [ ] **Step 4: Confirm the only break this causes is the expected one**

Run: `cd backend && grep -rn "PreviousPeriod" --include=*.go .`
Expected: exactly two matches, both in `report_aggregator.go` (`report.PreviousPeriod(...)` and `report.PreviousPeriodLabel(...)`, inside `AggregateReportData`'s period-comparison block). That is the one caller Task 7 removes next — do not fix it here.

Run: `cd backend && go build ./...`
Expected: fails with exactly those two errors (`report.PreviousPeriod undefined`, `report.PreviousPeriodLabel undefined`, both in `report_aggregator.go`). Any other error means something in this task's own change is broken. `go build ./internal/models/...` on its own succeeds — the break is confined to `internal/services` and whatever imports it, resolved by Task 7 immediately following this one.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/models/report_period.go backend/internal/models/report_period_test.go
git commit -m "$(cat <<'EOF'
refactor(reports): remove PreviousPeriod dead code

The period-over-period comparison feature is not part of either of the
two new fixed report types. Its only caller (report_aggregator.go) is
removed in the next task; removing the dead function here first keeps
this change reviewable on its own. The repo-wide build is red between
this commit and the next one - expected, and confined to that one gap.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Report aggregator rewrite — shared MeasurableWindow, EffectiveSLATarget, uptime series, dead-code removal

**Files:**
- Modify: `backend/internal/services/report_aggregator.go` (whole file)
- Modify: `backend/internal/services/report_aggregator_test.go` (append)
- Modify: `backend/internal/api/report_handler.go:658-680,738` (remove local `measurableWindow`, use the shared one)
- Delete: `backend/internal/services/report_insights.go`
- Delete: `backend/internal/services/report_insights_test.go`

**Interfaces:**
- Consumes: `SettingsService.DefaultSLATarget` (Task 3).
- Produces: `services.MeasurableWindow(createdAt, start, end time.Time) (time.Time, bool)`, `services.EffectiveSLATarget(monitorOverride *float64, systemDefault float64) float64`, `services.UptimeSeriesPoint{Date, Uptime}`, `ReportData.UptimeSeries []UptimeSeriesPoint`, `ReportData.EffectiveSLA float64`. `ReportData.Timeline`/`Previous`/`Availability`/`Performance` no longer exist. Task 8 (PDF renderer) consumes `UptimeSeries` and `EffectiveSLA`; `api.GetSummaryReportHandler` consumes `MeasurableWindow`.

- [ ] **Step 1: Write the failing tests for the two new pure functions**

In `backend/internal/services/report_aggregator_test.go`, append at the end of the file:

```go

func TestMeasurableWindow(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

	t.Run("a monitor older than the window is unclamped", func(t *testing.T) {
		from, ok := MeasurableWindow(start.AddDate(0, -1, 0), start, end)
		if !ok || !from.Equal(start) {
			t.Errorf("from=%v ok=%v, want %v true", from, ok, start)
		}
	})

	t.Run("a monitor created mid-window is clamped to its creation", func(t *testing.T) {
		created := start.AddDate(0, 0, 3)
		from, ok := MeasurableWindow(created, start, end)
		if !ok || !from.Equal(created) {
			t.Errorf("from=%v ok=%v, want %v true", from, ok, created)
		}
	})

	t.Run("a monitor created after the window has nothing to measure", func(t *testing.T) {
		_, ok := MeasurableWindow(end.AddDate(0, 0, 1), start, end)
		if ok {
			t.Error("expected ok=false for a monitor created after the window closed")
		}
	})

	t.Run("a monitor created exactly at the window's end has nothing to measure", func(t *testing.T) {
		_, ok := MeasurableWindow(end, start, end)
		if ok {
			t.Error("expected ok=false when creation equals the window end")
		}
	})
}

func TestEffectiveSLATarget(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }

	if got := EffectiveSLATarget(nil, 99.9); got != 99.9 {
		t.Errorf("nil override: got %v, want the system default 99.9", got)
	}
	if got := EffectiveSLATarget(f64(95), 99.9); got != 95 {
		t.Errorf("override: got %v, want 95", got)
	}
	if got := EffectiveSLATarget(f64(0), 99.9); got != 0 {
		t.Errorf("EffectiveSLATarget takes the pointer at face value: got %v, want 0", got)
	}
	if got := EffectiveSLATarget(f64(100), 99.9); got != 100 {
		t.Errorf("boundary override: got %v, want 100", got)
	}
}

func TestComputeUptimeSeries(t *testing.T) {
	s := &ReportAggregatorService{}
	monitorID := uuid.New()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 10)

	// An outage on day 3 should show up as a dip that day and partially
	// recover as later, clean days join the cumulative average - not a flat
	// line, and not a permanent drop to the outage's instantaneous percentage.
	outageStart := start.AddDate(0, 0, 3)
	outageEnd := outageStart.Add(12 * time.Hour)

	byMonitor := []monitorIncidentsForTest{{
		id:        monitorID,
		from:      start,
		incidents: []incidentWindow{{start: outageStart, end: &outageEnd}},
	}}

	points := computeUptimeSeriesFromIncidents(s, byMonitor, start, end)
	if len(points) != uptimeSeriesPoints {
		t.Fatalf("got %d points, want %d", len(points), uptimeSeriesPoints)
	}

	dayIndex := func(daysFromStart int) int {
		// Sample i covers start+((i+1)/uptimeSeriesPoints)*window; find the
		// first sample whose date has passed the given day boundary.
		target := start.AddDate(0, 0, daysFromStart)
		for i, p := range points {
			if !p.Date.Before(target) {
				return i
			}
		}
		return len(points) - 1
	}

	beforeOutage := points[dayIndex(2)]
	if beforeOutage.Uptime != 100 {
		t.Errorf("before the outage, cumulative uptime = %v, want 100", beforeOutage.Uptime)
	}

	dayOfOutage := points[dayIndex(4)]
	if dayOfOutage.Uptime >= 100 || dayOfOutage.Uptime <= 0 {
		t.Errorf("the sample covering the outage = %v, want a dip strictly between 0 and 100", dayOfOutage.Uptime)
	}

	last := points[len(points)-1]
	if last.Uptime <= dayOfOutage.Uptime {
		t.Errorf("cumulative uptime should partially recover as clean days accumulate: day-of-outage=%v last=%v",
			dayOfOutage.Uptime, last.Uptime)
	}
	if last.Uptime >= 100 {
		t.Errorf("the outage must still be reflected at the end of the window, got %v", last.Uptime)
	}
}
```

This test drives `computeUptimeSeries`'s core arithmetic without a database by going through two small test-only seams: a `monitorIncidentsForTest`/`incidentWindow` pair of structs and a `computeUptimeSeriesFromIncidents` function that Step 3 factors the real `computeUptimeSeries` around (the DB query becomes a thin wrapper that builds this same shape and calls the shared arithmetic). `uuid` is already imported by this file.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/services/... -run 'TestMeasurableWindow|TestEffectiveSLATarget|TestComputeUptimeSeries' -v`
Expected: FAIL to compile — none of `MeasurableWindow`, `EffectiveSLATarget`, `uptimeSeriesPoints`, `monitorIncidentsForTest`, `incidentWindow`, `computeUptimeSeriesFromIncidents` exist yet.

- [ ] **Step 3: Rewrite report_aggregator.go**

Replace `backend/internal/services/report_aggregator.go` in full:

```go
// Package services - report_aggregator.go assembles the data a rendered report
// needs: which monitors are in scope, and each one's uptime, downtime, incident
// history, and SLA outcome over the report's time range.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// ReportAggregatorService computes report data from monitors, incidents, and
// the report's own scope definition.
type ReportAggregatorService struct {
	db *gorm.DB
	// settings supplies the report timezone and the default SLA target. Nil is
	// tolerated (keeping the service usable in tests): timezone falls back to
	// UTC, SLA target falls back to models.DefaultSLATargetPercent.
	settings *SettingsService
}

// NewReportAggregatorService returns a service bound to db.
func NewReportAggregatorService(db *gorm.DB, settings *SettingsService) *ReportAggregatorService {
	return &ReportAggregatorService{db: db, settings: settings}
}

// reportLocation is the configured report zone, or UTC.
func (s *ReportAggregatorService) reportLocation(ctx context.Context) *time.Location {
	if s.settings == nil {
		return time.UTC
	}
	return s.settings.ReportLocation(ctx)
}

// MeasurableWindow narrows a reporting window to the part of it a monitor
// actually existed for, and reports whether any of it is measurable at all.
//
// Uptime measured over the whole window regardless of the monitor's age made
// the same outage look smaller the further back the window reached: a monitor
// added yesterday reported ~100% over ninety days no matter how badly it
// behaved. Measuring from creation instead makes every window describe only
// the period the monitor was actually being watched.
//
// A monitor created at or after the window's end returns false: it has
// nothing to say about that period, and averaging it in as a perfect score
// would be inventing a result.
//
// Shared between this aggregator and GetSummaryReportHandler (api package),
// which applied this exact clamp first - api importing from services is the
// existing direction, so the definition lives here rather than there.
func MeasurableWindow(createdAt, start, end time.Time) (time.Time, bool) {
	from := start
	if createdAt.After(from) {
		from = createdAt
	}
	if !from.Before(end) {
		return time.Time{}, false
	}
	return from, true
}

// EffectiveSLATarget resolves the SLA target a monitor is held to: its own
// override when set, otherwise the instance-wide default.
func EffectiveSLATarget(monitorOverride *float64, systemDefault float64) float64 {
	if monitorOverride != nil {
		return *monitorOverride
	}
	return systemDefault
}

// ReportMetrics is one monitor's contribution to a report.
type ReportMetrics struct {
	MonitorID   uuid.UUID `json:"monitor_id"`
	MonitorName string    `json:"monitor_name"`
	Uptime      float64   `json:"uptime"` // percentage over the range
	// DowntimeMinutes is fractional on purpose. Truncating to whole minutes
	// made every outage shorter than 60s count as zero, so a report could show
	// incidents alongside 100% uptime.
	DowntimeMinutes float64 `json:"downtime_minutes"`
	IncidentCount   int     `json:"incident_count"`
	// SLATarget is always populated now - the monitor's own override, or the
	// instance-wide default when it has none. There is no longer a monitor
	// with "no SLA": every monitor is held to at least the system default.
	SLATarget *float64          `json:"sla_target"`
	SLAMet    bool              `json:"sla_met"`
	Incidents []IncidentSummary `json:"incidents"`
}

// IncidentSummary is one incident as it appears in a report.
type IncidentSummary struct {
	ID        uuid.UUID  `json:"id"`
	StartTime time.Time  `json:"start_time"`
	EndTime   *time.Time `json:"end_time"`
	// Duration is the incident's overlap with the report window, in minutes -
	// not its total length, which may extend beyond the window on either side.
	Duration        float64 `json:"duration_minutes"`
	Severity        string  `json:"severity"`
	Status          string  `json:"status"` // "ongoing" or "resolved"
	RootCause       string  `json:"root_cause"`
	ResolutionNotes string  `json:"resolution_notes"`
}

// UptimeSeriesPoint is one sample of the cumulative-uptime-to-date line: the
// uptime percentage across every monitor in scope, from the report's start
// through Date.
type UptimeSeriesPoint struct {
	Date   time.Time `json:"date"`
	Uptime float64   `json:"uptime"`
}

// ReportData is the fully aggregated payload handed to a renderer.
type ReportData struct {
	ReportName        string          `json:"report_name"`
	CustomTitle       *string         `json:"custom_title"`
	CustomDescription *string         `json:"custom_description"`
	TimeRangeStart    time.Time       `json:"time_range_start"`
	TimeRangeEnd      time.Time       `json:"time_range_end"`
	Metrics           []ReportMetrics `json:"metrics"`
	// Warnings names monitors that could not be aggregated. A report is a
	// compliance artifact, so a dropped monitor is surfaced rather than
	// silently omitted from the results.
	Warnings []string `json:"warnings,omitempty"`
	// UptimeSeries is the cumulative-uptime-to-date line the Uptime Report
	// graphs, aggregated across every monitor in scope.
	UptimeSeries []UptimeSeriesPoint `json:"uptime_series,omitempty"`
	// EffectiveSLA is the SLA target the graph's reference line is drawn at:
	// the effective target (override or system default) averaged across every
	// monitor in scope.
	EffectiveSLA float64 `json:"effective_sla"`
	// Location is the timezone every timestamp in the rendered report is
	// written in. Carried on the data rather than read by each renderer so a
	// PDF and its HTML equivalent cannot disagree about what time it was.
	// Renderers must use ReportLocation(), which falls back to UTC when unset.
	Location *time.Location `json:"-"`
}

// ReportLocation is the zone a report renders in, defaulting to UTC.
//
// Never falls back to time.Local: that is the server process's zone, which is
// whatever the container was given, which is not a deliberate choice by
// anyone.
func (d *ReportData) ReportLocation() *time.Location {
	if d == nil || d.Location == nil {
		return time.UTC
	}
	return d.Location
}

// AggregateReportData assembles everything a report needs. Monitors that fail
// to aggregate are recorded in Warnings rather than failing the whole report.
func (s *ReportAggregatorService) AggregateReportData(ctx context.Context, report *models.Report, requestedBy uuid.UUID) (*ReportData, error) {
	if report == nil {
		return nil, fmt.Errorf("report is nil")
	}
	if err := report.ValidatePeriod(); err != nil {
		return nil, fmt.Errorf("report period: %w", err)
	}

	// A report's scope can name monitors, groups, or tags the requester does
	// not own or have been shared. Resolving scope alone is not authorization -
	// it has to be intersected with what this specific user may see, the same
	// way ListAccessibleMonitors does for the monitors list itself.
	var requester models.User
	if err := s.db.WithContext(ctx).Select("is_admin").
		First(&requester, "id = ?", requestedBy).Error; err != nil {
		return nil, fmt.Errorf("resolving report requester: %w", err)
	}

	monitorIDs, err := s.getMonitorIDsForScope(ctx, report.ScopeType, report.ScopeData)
	if err != nil {
		return nil, err
	}

	loc := s.reportLocation(ctx)
	startTime, endTime := report.ResolvePeriod(time.Now(), loc)

	data := &ReportData{
		ReportName:        report.Name,
		CustomTitle:       report.CustomTitle,
		CustomDescription: report.CustomDescription,
		TimeRangeStart:    startTime,
		TimeRangeEnd:      endTime,
		Metrics:           []ReportMetrics{},
	}

	// An empty scope resolves to no monitors; return an empty report rather than
	// querying with an empty IN clause.
	if len(monitorIDs) == 0 {
		return data, nil
	}

	q := s.db.WithContext(ctx).Where("id IN ?", monitorIDs)
	if !requester.IsAdmin {
		// Same access rule as MonitorService.ListAccessibleMonitors: owned or
		// explicitly shared. A monitor named in scope that fails this check is
		// silently excluded rather than erroring, so a stale group/tag scope
		// degrades to "fewer monitors in the report" instead of failing it.
		q = q.Where(
			"owner_id = ? OR id IN (SELECT monitor_id FROM monitor_sharing WHERE shared_with_user_id = ?)",
			requestedBy, requestedBy,
		)
	}
	var monitors []models.Monitor
	if err := q.Find(&monitors).Error; err != nil {
		return nil, fmt.Errorf("loading monitors for report: %w", err)
	}

	systemDefaultSLA := models.DefaultSLATargetPercent
	if s.settings != nil {
		systemDefaultSLA = s.settings.DefaultSLATarget(ctx)
	}

	// A monitor that did not exist for any part of the window has nothing to
	// say about it, and is left out entirely rather than averaged in as a
	// perfect score it never earned - the same clamp GetSummaryReportHandler
	// already applied, now shared via MeasurableWindow.
	measurable := make([]models.Monitor, 0, len(monitors))
	windowStart := make(map[uuid.UUID]time.Time, len(monitors))
	var slaSum float64
	for i := range monitors {
		from, ok := MeasurableWindow(monitors[i].CreatedAt, startTime, endTime)
		if !ok {
			continue
		}
		measurable = append(measurable, monitors[i])
		windowStart[monitors[i].ID] = from
		slaSum += EffectiveSLATarget(monitors[i].SLATarget, systemDefaultSLA)
	}

	for i := range measurable {
		metrics, err := s.calculateMonitorMetrics(ctx, measurable[i],
			windowStart[measurable[i].ID], endTime, systemDefaultSLA)
		if err != nil {
			data.Warnings = append(data.Warnings,
				fmt.Sprintf("monitor %q (%s) omitted: %v", measurable[i].Name, measurable[i].ID, err))
			continue
		}
		data.Metrics = append(data.Metrics, metrics)
	}

	if len(measurable) > 0 {
		data.EffectiveSLA = round2(slaSum / float64(len(measurable)))
		series, err := s.computeUptimeSeries(ctx, measurable, windowStart, startTime, endTime)
		if err != nil {
			data.Warnings = append(data.Warnings, fmt.Sprintf("uptime graph unavailable: %v", err))
		} else {
			data.UptimeSeries = series
		}
	}

	return data, nil
}

// getMonitorIDsForScope resolves a report's scope to the monitor IDs it covers.
func (s *ReportAggregatorService) getMonitorIDsForScope(ctx context.Context, scopeType string, scope models.ReportScope) ([]uuid.UUID, error) {
	if err := scope.Validate(scopeType); err != nil {
		return nil, err
	}

	switch scopeType {
	case models.ScopeTypeMonitors:
		// Taken as given; a missing monitor simply contributes nothing.
		return scope.MonitorIDs, nil

	case models.ScopeTypeGroups:
		var ids []uuid.UUID
		if err := s.db.WithContext(ctx).Model(&models.Monitor{}).
			Where("group_id IN ?", scope.GroupIDs).
			Pluck("id", &ids).Error; err != nil {
			return nil, fmt.Errorf("resolving group scope: %w", err)
		}
		return ids, nil

	case models.ScopeTypeTags:
		// tags is a JSONB array on monitors; @> matches a monitor carrying the
		// tag. Any of the listed tags qualifies the monitor.
		var ids []uuid.UUID
		q := s.db.WithContext(ctx).Model(&models.Monitor{})
		conds := s.db.Session(&gorm.Session{NewDB: true})
		for i, tag := range scope.Tags {
			cond := "tags @> ?"
			arg := fmt.Sprintf("[%q]", tag)
			if i == 0 {
				conds = conds.Where(cond, arg)
			} else {
				conds = conds.Or(cond, arg)
			}
		}
		if err := q.Where(conds).Pluck("id", &ids).Error; err != nil {
			return nil, fmt.Errorf("resolving tag scope: %w", err)
		}
		return ids, nil

	case models.ScopeTypeTypes:
		// Resolved at generation time, not stored, so a report scoped to "all
		// DNS monitors" picks up monitors added since it was defined.
		var ids []uuid.UUID
		if err := s.db.WithContext(ctx).Model(&models.Monitor{}).
			Where("type IN ?", scope.Types).
			Pluck("id", &ids).Error; err != nil {
			return nil, fmt.Errorf("resolving type scope: %w", err)
		}
		return ids, nil

	default:
		return nil, fmt.Errorf("unknown scope_type %q", scopeType)
	}
}

// calculateMonitorMetrics computes one monitor's uptime, SLA outcome, and
// incident summary over [startTime, endTime]. systemDefaultSLA is the
// instance-wide default, used whenever the monitor carries no override of its
// own.
func (s *ReportAggregatorService) calculateMonitorMetrics(
	ctx context.Context,
	monitor models.Monitor,
	startTime, endTime time.Time,
	systemDefaultSLA float64,
) (ReportMetrics, error) {
	target := EffectiveSLATarget(monitor.SLATarget, systemDefaultSLA)
	metrics := ReportMetrics{
		MonitorID:   monitor.ID,
		MonitorName: monitor.Name,
		SLATarget:   &target,
		Incidents:   []IncidentSummary{},
	}

	// Select incidents that OVERLAP the window, not merely those that start
	// inside it. An incident that began before the range and is still open
	// contributes real downtime to it; filtering on start_time alone drops that
	// downtime and reports uptime that is too high.
	var incidents []models.Incident
	if err := s.db.WithContext(ctx).
		Where("monitor_id = ?", monitor.ID).
		Where("start_time <= ?", endTime).
		Where("end_time IS NULL OR end_time >= ?", startTime).
		Order("start_time ASC").
		Find(&incidents).Error; err != nil {
		return metrics, fmt.Errorf("loading incidents: %w", err)
	}

	summaries, totalDowntimeMinutes := summarizeIncidents(incidents, startTime, endTime)
	metrics.Incidents = summaries
	metrics.DowntimeMinutes = totalDowntimeMinutes
	metrics.IncidentCount = len(incidents)
	metrics.Uptime = uptimePercent(startTime, endTime, totalDowntimeMinutes)
	metrics.SLAMet = metrics.Uptime >= target

	return metrics, nil
}

// summarizeIncidents converts incidents to report summaries and totals the
// downtime they contribute to [startTime, endTime]. Each incident is clamped to
// the window at both ends, so downtime falling outside the reporting range is
// never counted - an incident that started before the window contributes only
// the part inside it, and one that ended after the window is cut at the end.
//
// Kept free of the database so the window arithmetic can be tested directly;
// it is the part of reporting most likely to be quietly wrong.
func summarizeIncidents(incidents []models.Incident, startTime, endTime time.Time) ([]IncidentSummary, float64) {
	summaries := make([]IncidentSummary, 0, len(incidents))
	total := 0.0

	for i := range incidents {
		incident := incidents[i]

		startT := incident.StartTime
		if startT.Before(startTime) {
			startT = startTime
		}
		// An open incident runs to the end of the window; a closed one ends when
		// it ended, unless that is past the window.
		endT := endTime
		if incident.EndTime != nil && incident.EndTime.Before(endTime) {
			endT = *incident.EndTime
		}

		// Kept fractional rather than truncated to whole minutes: a 40-second
		// outage is not zero downtime, and four of them are not zero either.
		durationMinutes := endT.Sub(startT).Minutes()
		if durationMinutes < 0 {
			durationMinutes = 0
		}
		total += durationMinutes

		summaries = append(summaries, IncidentSummary{
			ID:              incident.ID,
			StartTime:       incident.StartTime,
			EndTime:         incident.EndTime,
			Duration:        durationMinutes,
			Severity:        incident.Severity,
			Status:          incident.Status(),
			RootCause:       incident.RootCause,
			ResolutionNotes: incident.ResolutionNotes,
		})
	}

	return summaries, total
}

// uptimePercent is the share of the window not spent in downtime. Overlapping
// incidents can sum past the window length, so the result floors at zero rather
// than going negative.
func uptimePercent(startTime, endTime time.Time, downtimeMinutes float64) float64 {
	totalMinutes := endTime.Sub(startTime).Minutes()
	if totalMinutes <= 0 {
		return 0
	}
	up := totalMinutes - downtimeMinutes
	if up < 0 {
		up = 0
	}
	return up / totalMinutes * 100
}

// uptimeSeriesPoints is how many samples the cumulative-uptime graph plots.
// Enough to show shape over a typical reporting window without turning into
// noise, and small enough that the whole series costs one incident query per
// monitor rather than one per monitor per sample.
const uptimeSeriesPoints = 30

// monitorIncidentsForTest and incidentWindow let computeUptimeSeries's
// arithmetic be exercised without a database - see
// TestComputeUptimeSeries/computeUptimeSeriesFromIncidents.
type monitorIncidentsForTest struct {
	id        uuid.UUID
	from      time.Time
	incidents []incidentWindow
}

type incidentWindow struct {
	start time.Time
	end   *time.Time
}

// computeUptimeSeries samples cumulative uptime at evenly spaced points across
// [start, end]. windowStart carries each monitor's own measurable start (its
// creation time, when that is later than start), so a monitor added mid-period
// contributes only the minutes it actually existed for at every sample.
func (s *ReportAggregatorService) computeUptimeSeries(
	ctx context.Context,
	monitors []models.Monitor,
	windowStart map[uuid.UUID]time.Time,
	start, end time.Time,
) ([]UptimeSeriesPoint, error) {
	if len(monitors) == 0 || !end.After(start) {
		return nil, nil
	}

	// One incident query per monitor, covering the whole window - reused for
	// every sample point below instead of re-querying per point.
	byMonitor := make([]monitorIncidentsForTest, 0, len(monitors))
	for _, m := range monitors {
		from := windowStart[m.ID]
		var incidents []models.Incident
		if err := s.db.WithContext(ctx).
			Where("monitor_id = ?", m.ID).
			Where("start_time <= ?", end).
			Where("end_time IS NULL OR end_time >= ?", from).
			Order("start_time ASC").
			Find(&incidents).Error; err != nil {
			return nil, fmt.Errorf("loading incidents for uptime series: %w", err)
		}
		windows := make([]incidentWindow, 0, len(incidents))
		for _, inc := range incidents {
			windows = append(windows, incidentWindow{start: inc.StartTime, end: inc.EndTime})
		}
		byMonitor = append(byMonitor, monitorIncidentsForTest{id: m.ID, from: from, incidents: windows})
	}

	return computeUptimeSeriesFromIncidents(s, byMonitor, start, end), nil
}

// computeUptimeSeriesFromIncidents is computeUptimeSeries's arithmetic, free
// of the database so it can be unit tested directly against a synthetic set
// of incidents.
func computeUptimeSeriesFromIncidents(_ *ReportAggregatorService, byMonitor []monitorIncidentsForTest, start, end time.Time) []UptimeSeriesPoint {
	points := make([]UptimeSeriesPoint, 0, uptimeSeriesPoints)
	step := end.Sub(start) / time.Duration(uptimeSeriesPoints)
	for i := 1; i <= uptimeSeriesPoints; i++ {
		sampleEnd := start.Add(step * time.Duration(i))
		if i == uptimeSeriesPoints {
			sampleEnd = end
		}

		var totalMinutes, downMinutes float64
		for _, mi := range byMonitor {
			if !mi.from.Before(sampleEnd) {
				// Not measurable yet at this sample point.
				continue
			}
			asIncidents := make([]models.Incident, 0, len(mi.incidents))
			for _, w := range mi.incidents {
				asIncidents = append(asIncidents, models.Incident{StartTime: w.start, EndTime: w.end})
			}
			_, minutes := summarizeIncidents(asIncidents, mi.from, sampleEnd)
			totalMinutes += sampleEnd.Sub(mi.from).Minutes()
			downMinutes += minutes
		}

		points = append(points, UptimeSeriesPoint{
			Date:   sampleEnd,
			Uptime: aggregateUptimePercent(totalMinutes, downMinutes),
		})
	}
	return points
}

// aggregateUptimePercent is uptimePercent generalized to pre-summed
// total/down minutes across possibly many monitors, each contributing only
// its own measurable time, rather than a single [start,end] span.
func aggregateUptimePercent(totalMinutes, downMinutes float64) float64 {
	if totalMinutes <= 0 {
		return 100
	}
	up := totalMinutes - downMinutes
	if up < 0 {
		up = 0
	}
	return round2(up / totalMinutes * 100)
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/services/... -run 'TestMeasurableWindow|TestEffectiveSLATarget|TestComputeUptimeSeries|TestSummarizeIncidents|TestUptimePercent' -v`
Expected: PASS for all.

- [ ] **Step 5: Delete the now-dead report_insights.go and its test**

```bash
rm backend/internal/services/report_insights.go backend/internal/services/report_insights_test.go
```

- [ ] **Step 6: Point report_handler.go at the shared MeasurableWindow**

In `backend/internal/api/report_handler.go`, delete the local function (its doc comment plus body, immediately before `GetSummaryReportHandler`):

```go
// measurableWindow narrows a reporting window to the part of it a monitor
// actually existed for, and reports whether any of it is measurable at all.
//
// Uptime was previously divided by the whole window however young the monitor
// was, so the same outage looked smaller the further back the window reached: a
// monitor added yesterday reported ~100% over ninety days no matter how badly
// it behaved. Measuring from its creation instead makes every window describe
// the period the monitor was actually being watched.
//
// A monitor created after the window closed returns false. It has nothing to
// say about that period, and averaging it in as a perfect score would be
// inventing a result. Only reachable for a historical range, since a window
// ending now cannot precede an existing monitor.
func measurableWindow(createdAt, start, end time.Time) (time.Time, bool) {
	from := start
	if createdAt.After(from) {
		from = createdAt
	}
	if !from.Before(end) {
		return time.Time{}, false
	}
	return from, true
}

```

Then, inside `GetSummaryReportHandler`, change the one call site:

```go
			from, ok := measurableWindow(m.CreatedAt, start, end)
```

to:

```go
			from, ok := services.MeasurableWindow(m.CreatedAt, start, end)
```

`report_handler.go` already imports `"github.com/Stevy2191/Sentinel/backend/internal/services"` (used for `*services.MonitorService` etc. throughout the file), so no import change is needed.

- [ ] **Step 7: Run the full backend build and test suite**

Run: `cd backend && go build ./... && go test ./...`
Expected: builds and all tests pass. If `pdf_renderer.go`/`pdf_sections.go` fail to compile at this point (they still reference the now-removed `ReportData.Timeline`/`Previous`/`Availability`/`Performance` fields and old `RenderReportToPDF` call shape), that is expected and resolved in Task 8 — do not attempt to fix `pdf_*.go` files in this task.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/services/report_aggregator.go backend/internal/services/report_aggregator_test.go \
        backend/internal/api/report_handler.go
git rm backend/internal/services/report_insights.go backend/internal/services/report_insights_test.go
git commit -m "$(cat <<'EOF'
feat(reports): SLA-aware aggregation, shared MeasurableWindow, uptime series

report_aggregator.go now: clamps every monitor's window to when it
existed via MeasurableWindow (promoted here from report_handler.go so
both files share one implementation), resolves each monitor's
effective SLA target (its own override or the instance default), and
computes a 30-point cumulative-uptime time series for the Uptime
Report's graph. Removes the now-dead period-comparison, timeline,
availability, and performance aggregation this replaces, along with
report_insights.go which existed only to support it.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: PDF renderer rewrite — two fixed layouts and the uptime graph

**Files:**
- Delete: `backend/internal/services/pdf_sections.go`
- Modify: `backend/internal/services/pdf_renderer.go` (whole file)
- Modify: `backend/internal/services/pdf_renderer_test.go`
- Modify: `backend/internal/services/pdf_encoding_test.go`

**Interfaces:**
- Consumes: `models.ReportTypeUptime`/`ReportTypeIncident` (Task 2), `ReportData.UptimeSeries`/`EffectiveSLA` (Task 7).
- Produces: `(*PDFRendererService) RenderReportToPDF(data *ReportData, reportType string, nameHint string) (string, error)` — signature changed from `(data *ReportData, sections []string, nameHint string)`. Task 9 (`report_generator.go`) calls this with the new signature.

`pdf_sections.go` contains only `drawPDFExecutiveSummary`, `drawPDFTimeline`, `drawPDFAvailability`, `drawPDFPerformance`, and helpers used only by them (`drawTableHeader`, `formatMillis`, `signedDelta`, `signedCountDelta`, `signedDeltaMinutes`, `worstMonitor`, `perfectMonitors`, `joinCapped`) — confirmed via a whole-backend grep that none of these are used anywhere outside that file. It is deleted wholesale, not edited.

- [ ] **Step 1: Delete pdf_sections.go**

```bash
rm backend/internal/services/pdf_sections.go
```

- [ ] **Step 2: Write the failing tests**

Replace the two PDF-generation tests near the top of `backend/internal/services/pdf_renderer_test.go`. Change:

```go
// A generated report must actually be a readable PDF on disk, not merely a call
// that returned no error.
func TestRenderReportToPDFWritesAValidFile(t *testing.T) {
	dir := t.TempDir()
	r, err := NewPDFRendererService(dir)
	if err != nil {
		t.Fatalf("NewPDFRendererService: %v", err)
	}

	name, err := r.RenderReportToPDF(sampleReportData(), nil, "monthly")
	if err != nil {
		t.Fatalf("RenderReportToPDF: %v", err)
	}
	if filepath.Base(name) != name {
		t.Errorf("returned name %q should be a bare file name", name)
	}

	path := filepath.Join(dir, name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading generated PDF: %v", err)
	}
	if !strings.HasPrefix(string(content), "%PDF-") {
		t.Errorf("file does not start with the PDF magic bytes: %q", content[:min(8, len(content))])
	}
	if len(content) < 1000 {
		t.Errorf("PDF is suspiciously small (%d bytes) - sections may not have rendered", len(content))
	}

	size, err := r.GetPDFFileSize(name)
	if err != nil {
		t.Fatalf("GetPDFFileSize: %v", err)
	}
	if size != len(content) {
		t.Errorf("GetPDFFileSize = %d, want %d", size, len(content))
	}
}

// Each template section must change the output, or section selection is a lie.
func TestRenderReportToPDFHonoursSections(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewPDFRendererService(dir)
	data := sampleReportData()

	sizeOf := func(sections []string, hint string) int {
		name, err := r.RenderReportToPDF(data, sections, hint)
		if err != nil {
			t.Fatalf("render %v: %v", sections, err)
		}
		size, err := r.GetPDFFileSize(name)
		if err != nil {
			t.Fatalf("size: %v", err)
		}
		return size
	}

	slaOnly := sizeOf([]string{models.SectionSLACompliance}, "sla")
	everything := sizeOf([]string{
		models.SectionCharts, models.SectionSLACompliance,
		models.SectionIncidentSummary, models.SectionCustom,
	}, "all")

	if everything <= slaOnly {
		t.Errorf("a full report (%d bytes) should be larger than SLA-only (%d bytes); sections may be ignored",
			everything, slaOnly)
	}
}
```

to:

```go
// A generated report must actually be a readable PDF on disk, not merely a call
// that returned no error.
func TestRenderReportToPDFWritesAValidFile(t *testing.T) {
	dir := t.TempDir()
	r, err := NewPDFRendererService(dir)
	if err != nil {
		t.Fatalf("NewPDFRendererService: %v", err)
	}

	name, err := r.RenderReportToPDF(sampleReportData(), models.ReportTypeUptime, "monthly")
	if err != nil {
		t.Fatalf("RenderReportToPDF: %v", err)
	}
	if filepath.Base(name) != name {
		t.Errorf("returned name %q should be a bare file name", name)
	}

	path := filepath.Join(dir, name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading generated PDF: %v", err)
	}
	if !strings.HasPrefix(string(content), "%PDF-") {
		t.Errorf("file does not start with the PDF magic bytes: %q", content[:min(8, len(content))])
	}
	if len(content) < 1000 {
		t.Errorf("PDF is suspiciously small (%d bytes) - sections may not have rendered", len(content))
	}

	size, err := r.GetPDFFileSize(name)
	if err != nil {
		t.Fatalf("GetPDFFileSize: %v", err)
	}
	if size != len(content) {
		t.Errorf("GetPDFFileSize = %d, want %d", size, len(content))
	}
}

// Each report type must produce a genuinely different document, or the type
// selection is a lie.
func TestRenderReportToPDFHonoursReportType(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewPDFRendererService(dir)
	data := sampleReportData()

	render := func(reportType, hint string) string {
		name, err := r.RenderReportToPDF(data, reportType, hint)
		if err != nil {
			t.Fatalf("render %v: %v", reportType, err)
		}
		path, err := r.GetPDFPath(name)
		if err != nil {
			t.Fatalf("path: %v", err)
		}
		return pdfDrawnText(t, path)
	}

	uptime := render(models.ReportTypeUptime, "uptime")
	if !strings.Contains(uptime, "SLA Compliance") {
		t.Error("uptime report is missing the SLA Compliance section")
	}
	if strings.Contains(uptime, "Root cause") {
		t.Error("uptime report should not include incident detail")
	}

	incident := render(models.ReportTypeIncident, "incident")
	if !strings.Contains(incident, "Upstream provider outage") {
		t.Error("incident report is missing incident detail")
	}
	if strings.Contains(incident, "SLA Compliance") {
		t.Error("incident report should not include the SLA Compliance section")
	}
}
```

(`pdfDrawnText` is defined in `pdf_encoding_test.go`, same package, and is reused here as-is.)

Also update `sampleReportData()` in the same file so the fixture actually
exercises the graph — without this, `len(data.UptimeSeries) >= 2` in
`drawPDFUptimeReport` is always false and `drawPDFUptimeGraph`, the single
newest and highest-risk piece of drawing code in this task, is never called
by any test. Change:

```go
		Warnings: []string{"monitor \"legacy\" omitted: loading incidents: timeout"},
	}
}
```

to:

```go
		Warnings:     []string{"monitor \"legacy\" omitted: loading incidents: timeout"},
		EffectiveSLA: 99.5,
		UptimeSeries: []UptimeSeriesPoint{
			{Date: start, Uptime: 100.0},
			{Date: start.AddDate(0, 0, 10), Uptime: 99.8},
			{Date: start.AddDate(0, 0, 20), Uptime: 99.4},
			{Date: end, Uptime: 99.6},
		},
	}
}
```

And extend `TestRenderReportToPDFHonoursReportType`'s uptime-report
assertions to confirm the graph itself drew, not merely that the SLA table
beside it did — add these two checks right after the existing
`uptime := render(...)` call, before `incident := render(...)`:

```go
	if !strings.Contains(uptime, "Uptime vs. SLA target") {
		t.Error("uptime report is missing the uptime-vs-SLA graph")
	}
	if !strings.Contains(uptime, "SLA target 99.50%") {
		t.Error("uptime report graph is missing its SLA reference line label")
	}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/services/... -run TestRenderReportToPDF -v`
Expected: FAIL to compile — `RenderReportToPDF`'s second parameter is still `[]string`, and `models.SectionSLACompliance` etc. no longer exist (removed in Task 2).

- [ ] **Step 4: Rewrite pdf_renderer.go**

Replace `backend/internal/services/pdf_renderer.go` in full:

```go
// Package services - pdf_renderer.go renders aggregated report data to a PDF on
// disk.
//
// The original design shelled out to wkhtmltopdf. That is not viable here: the
// runtime image is Alpine, which has no wkhtmltopdf package at all, and the
// project was archived upstream in 2023 with open CVEs while parsing HTML we
// generate. Drawing the PDF directly keeps the image at ~20MB with no external
// binary and no subprocess to sandbox, at the cost of CSS fidelity - the layout
// here is code rather than a stylesheet.
package services

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// Page geometry and palette, kept close to the HTML report so the two renderings
// of the same data look like siblings.
const (
	pdfMarginLeft  = 15.0
	pdfMarginTop   = 15.0
	pdfMarginRight = 15.0
	pdfPageWidth   = 210.0 // A4 portrait, mm
	pdfContentW    = pdfPageWidth - pdfMarginLeft - pdfMarginRight
)

var (
	pdfInk     = [3]int{26, 26, 26}
	pdfMuted   = [3]int{102, 102, 102}
	pdfRule    = [3]int{229, 231, 235}
	pdfPanel   = [3]int{243, 244, 246}
	pdfAccent  = [3]int{59, 130, 246}
	pdfSuccess = [3]int{16, 185, 129}
	pdfWarning = [3]int{245, 158, 11}
	pdfDanger  = [3]int{239, 68, 68}
)

// PDFRendererService writes report PDFs into a single output directory.
type PDFRendererService struct {
	outputDir string
}

// NewPDFRendererService returns a renderer writing to outputDir, creating it if
// needed. An unusable directory is reported now rather than at the first
// generation attempt.
func NewPDFRendererService(outputDir string) (*PDFRendererService, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating report output directory %q: %w", outputDir, err)
	}
	return &PDFRendererService{outputDir: outputDir}, nil
}

// RenderReportToPDF draws data to a PDF and returns the generated file's base
// name (not its path - callers store the name and resolve it via GetPDFPath).
// reportType selects the fixed layout: models.ReportTypeUptime or
// models.ReportTypeIncident.
func (s *PDFRendererService) RenderReportToPDF(data *ReportData, reportType string, nameHint string) (string, error) {
	if data == nil {
		return "", fmt.Errorf("report data is nil")
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMarginLeft, pdfMarginTop, pdfMarginRight)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetTitle(reportTitle(data), true)
	pdf.AddPage()

	drawPDFHeader(pdf, data)
	switch reportType {
	case models.ReportTypeIncident:
		drawPDFIncidentReport(pdf, data)
	default:
		// Uptime is also the fallback for an unrecognized value, which
		// Report.Validate already prevents from ever being stored.
		drawPDFUptimeReport(pdf, data)
	}
	drawPDFWarnings(pdf, data)
	drawPDFFooter(pdf)

	filename := pdfFilename(nameHint)
	outputPath := filepath.Join(s.outputDir, filename)
	if err := pdf.OutputFileAndClose(outputPath); err != nil {
		return "", fmt.Errorf("writing report PDF: %w", err)
	}
	return filename, nil
}

// pdfFilename builds a collision-resistant, filesystem-safe file name.
func pdfFilename(nameHint string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, nameHint)
	if safe == "" {
		safe = "report"
	}
	if len(safe) > 60 {
		safe = safe[:60]
	}
	return fmt.Sprintf("%d_%s.pdf", time.Now().UnixNano(), safe)
}

// GetPDFPath resolves a stored file name to its path on disk.
//
// The name is treated as untrusted: only a bare base name is accepted, so a
// stored value containing a path separator or ".." cannot escape outputDir.
func (s *PDFRendererService) GetPDFPath(pdfFilename string) (string, error) {
	base := filepath.Base(pdfFilename)
	if base != pdfFilename || base == "." || base == ".." || base == "" {
		return "", fmt.Errorf("invalid report file name %q", pdfFilename)
	}
	return filepath.Join(s.outputDir, base), nil
}

// DeletePDF removes a generated report file. A file that is already gone is not
// an error: the database row is the record that matters, and a half-deleted
// report is worse than a missing file.
func (s *PDFRendererService) DeletePDF(pdfFilename string) error {
	path, err := s.GetPDFPath(pdfFilename)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// GetPDFFileSize returns the size of a generated report in bytes.
func (s *PDFRendererService) GetPDFFileSize(pdfFilename string) (int, error) {
	path, err := s.GetPDFPath(pdfFilename)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return int(info.Size()), nil
}

// ---- report-type layouts ---------------------------------------------------

// drawPDFUptimeReport assembles the fixed Uptime Report layout: the summary
// tiles, the cumulative-uptime-vs-SLA graph, and the per-monitor SLA table.
func drawPDFUptimeReport(pdf *fpdf.Fpdf, data *ReportData) {
	drawPDFSummary(pdf, data)
	if len(data.UptimeSeries) >= 2 {
		drawPDFUptimeGraph(pdf, data.UptimeSeries, data.EffectiveSLA, data.ReportLocation())
	}
	drawPDFSLASection(pdf, data)
}

// drawPDFIncidentReport assembles the fixed Incident Report layout: the
// summary tiles and the full incident detail list.
func drawPDFIncidentReport(pdf *fpdf.Fpdf, data *ReportData) {
	drawPDFSummary(pdf, data)
	drawPDFIncidentSection(pdf, data)
}

// ---- drawing helpers -------------------------------------------------------

func setColor(pdf *fpdf.Fpdf, c [3]int, fill bool) {
	if fill {
		pdf.SetFillColor(c[0], c[1], c[2])
		return
	}
	pdf.SetTextColor(c[0], c[1], c[2])
}

// setDrawColor sets the stroke color fpdf uses for Line and the "D"/"DF"
// rectangle styles - distinct from setColor's fill/text targets, neither of
// which affects a stroked line or border.
func setDrawColor(pdf *fpdf.Fpdf, c [3]int) {
	pdf.SetDrawColor(c[0], c[1], c[2])
}

// uptimeColor grades an uptime percentage the same way the HTML report does.
func uptimeColor(pct float64) [3]int {
	switch {
	case pct < 95:
		return pdfDanger
	case pct < 99:
		return pdfWarning
	default:
		return pdfSuccess
	}
}

func drawPDFHeader(pdf *fpdf.Fpdf, data *ReportData) {
	pdf.SetFont("Helvetica", "B", 22)
	setColor(pdf, pdfInk, false)
	pdf.MultiCell(pdfContentW, 9, pdfText(reportTitle(data)), "", "L", false)

	if data.CustomDescription != nil && *data.CustomDescription != "" {
		pdf.Ln(1)
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5, pdfText(*data.CustomDescription), "", "L", false)
	}

	pdf.Ln(2)
	pdf.SetFont("Helvetica", "", 9)
	setColor(pdf, pdfMuted, false)
	// Every timestamp in the document is written in the report's configured
	// zone. Without this they rendered in the server process's zone, which in a
	// container is UTC by default — a timezone nobody chose.
	loc := data.ReportLocation()
	pdf.MultiCell(pdfContentW, 5, pdfText(fmt.Sprintf(
		"Report period: %s to %s\nGenerated: %s",
		data.TimeRangeStart.In(loc).Format("January 02, 2006"),
		data.TimeRangeEnd.In(loc).Format("January 02, 2006"),
		time.Now().In(loc).Format("January 02, 2006 at 15:04 MST"),
	)), "", "L", false)

	pdf.Ln(2)
	setColor(pdf, pdfRule, true)
	pdf.Rect(pdfMarginLeft, pdf.GetY(), pdfContentW, 0.6, "F")
	pdf.Ln(5)
}

func drawSectionHeading(pdf *fpdf.Fpdf, title string) {
	pdf.Ln(3)
	y := pdf.GetY()
	setColor(pdf, pdfAccent, true)
	pdf.Rect(pdfMarginLeft, y, 1.4, 7, "F")
	pdf.SetX(pdfMarginLeft + 4)
	pdf.SetFont("Helvetica", "B", 14)
	setColor(pdf, pdfInk, false)
	pdf.CellFormat(pdfContentW-4, 7, pdfText(title), "", 1, "L", false, 0, "")
	pdf.Ln(2)
}

func drawPDFSummary(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "Summary")

	total := len(data.Metrics)
	healthy, incidents, avgUptime := 0, 0, 0.0
	for _, m := range data.Metrics {
		if m.Uptime >= 99.0 {
			healthy++
		}
		incidents += m.IncidentCount
		avgUptime += m.Uptime
	}
	if total > 0 {
		avgUptime /= float64(total)
	}

	tiles := []struct {
		label string
		value string
		color [3]int
	}{
		{"Services monitored", fmt.Sprintf("%d", total), pdfInk},
		{"Average uptime", formatUptimePercent(avgUptime), uptimeColor(avgUptime)},
		{"Total incidents", fmt.Sprintf("%d", incidents), pdfInk},
		{"Healthy services", fmt.Sprintf("%d", healthy), pdfSuccess},
	}

	const gap = 4.0
	w := (pdfContentW - gap*3) / 4
	y := pdf.GetY()
	for i, t := range tiles {
		x := pdfMarginLeft + float64(i)*(w+gap)
		setColor(pdf, pdfPanel, true)
		pdf.Rect(x, y, w, 20, "F")
		setColor(pdf, pdfAccent, true)
		pdf.Rect(x, y, 1.2, 20, "F")

		pdf.SetXY(x+4, y+3.5)
		pdf.SetFont("Helvetica", "", 7)
		setColor(pdf, pdfMuted, false)
		pdf.CellFormat(w-6, 4, pdfText(strings.ToUpper(t.label)), "", 0, "L", false, 0, "")

		pdf.SetXY(x+4, y+9.5)
		pdf.SetFont("Helvetica", "B", 15)
		setColor(pdf, t.color, false)
		pdf.CellFormat(w-6, 8, pdfText(t.value), "", 0, "L", false, 0, "")
	}
	pdf.SetY(y + 24)
}

// drawPDFUptimeGraph draws the cumulative-uptime-vs-SLA line chart: an axis
// box, a flat SLA reference bar, and the cumulative-uptime line connecting
// each sample point. Built from fpdf's own line/rect primitives - no image
// pipeline, matching why this package draws PDFs directly at all (see the
// package doc comment above).
func drawPDFUptimeGraph(pdf *fpdf.Fpdf, series []UptimeSeriesPoint, slaTarget float64, loc *time.Location) {
	if len(series) < 2 {
		return
	}
	drawSectionHeading(pdf, "Uptime vs. SLA target")

	const chartH = 55.0
	const axisLabelW = 14.0
	x0 := pdfMarginLeft + axisLabelW
	chartW := pdfContentW - axisLabelW
	y0 := pdf.GetY()

	// The floor is the lowest value actually on the chart rather than always
	// zero: uptime rarely drops below the high nineties, and a 0-100 axis would
	// draw every report as a flat line pinned to the top, hiding the one thing
	// an SLA chart exists to show.
	lo := slaTarget
	for _, p := range series {
		if p.Uptime < lo {
			lo = p.Uptime
		}
	}
	lo = math.Floor(lo) - 1
	if lo < 0 {
		lo = 0
	}
	hi := 100.0
	if hi <= lo {
		hi = lo + 1
	}
	yFor := func(pct float64) float64 {
		frac := (pct - lo) / (hi - lo)
		if frac < 0 {
			frac = 0
		} else if frac > 1 {
			frac = 1
		}
		return y0 + chartH*(1-frac)
	}

	pdf.SetFont("Helvetica", "", 7)
	for i := 0; i <= 4; i++ {
		frac := float64(i) / 4
		val := lo + (hi-lo)*frac
		y := y0 + chartH*(1-frac)
		setColor(pdf, pdfRule, true)
		pdf.Rect(x0, y, chartW, 0.15, "F")
		setColor(pdf, pdfMuted, false)
		pdf.SetXY(pdfMarginLeft, y-2)
		pdf.CellFormat(axisLabelW-1, 4, fmt.Sprintf("%.0f%%", val), "", 0, "R", false, 0, "")
	}
	setDrawColor(pdf, pdfRule)
	pdf.SetLineWidth(0.2)
	pdf.Rect(x0, y0, chartW, chartH, "D")

	// SLA reference line: a thin flat bar the full width of the chart.
	setColor(pdf, pdfWarning, true)
	pdf.Rect(x0, yFor(slaTarget)-0.25, chartW, 0.5, "F")
	pdf.SetXY(x0+2, yFor(slaTarget)-5)
	pdf.SetFont("Helvetica", "B", 7)
	setColor(pdf, pdfWarning, false)
	pdf.CellFormat(60, 4, pdfText(fmt.Sprintf("SLA target %.2f%%", slaTarget)), "", 0, "L", false, 0, "")

	// The cumulative-uptime line: one segment between each pair of consecutive
	// samples.
	setDrawColor(pdf, pdfAccent)
	pdf.SetLineWidth(0.6)
	n := len(series)
	for i := 0; i < n-1; i++ {
		x1 := x0 + chartW*float64(i)/float64(n-1)
		x2 := x0 + chartW*float64(i+1)/float64(n-1)
		pdf.Line(x1, yFor(series[i].Uptime), x2, yFor(series[i+1].Uptime))
	}
	pdf.SetLineWidth(0.2)

	// X-axis date labels: first, middle, and last sample only, so the axis
	// stays readable instead of crowding thirty overlapping labels.
	pdf.SetFont("Helvetica", "", 7)
	setColor(pdf, pdfMuted, false)
	label := func(i int, align string) {
		x := x0 + chartW*float64(i)/float64(n-1)
		pdf.SetXY(x-15, y0+chartH+1.5)
		pdf.CellFormat(30, 4, series[i].Date.In(loc).Format("Jan 2"), "", 0, align, false, 0, "")
	}
	label(0, "L")
	label(n-1, "R")
	if mid := (n - 1) / 2; mid > 0 && mid < n-1 {
		label(mid, "C")
	}

	pdf.SetY(y0 + chartH + 9)
}

func drawPDFSLASection(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "SLA Compliance")

	if len(data.Metrics) == 0 {
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5, "No monitors in scope for this report.", "", "L", false)
		return
	}

	widths := []float64{pdfContentW - 105, 35, 35, 35}
	headers := []string{"Service", "Uptime", "SLA target", "Status"}

	pdf.SetFont("Helvetica", "B", 9)
	setColor(pdf, pdfPanel, true)
	setColor(pdf, pdfInk, false)
	for i, h := range headers {
		pdf.CellFormat(widths[i], 8, pdfText(h), "", 0, "L", true, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Helvetica", "", 9)
	for _, m := range data.Metrics {
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(widths[0], 7, pdfText(truncate(m.MonitorName, 46)), "B", 0, "L", false, 0, "")
		setColor(pdf, uptimeColor(m.Uptime), false)
		pdf.CellFormat(widths[1], 7, formatUptimePercent(m.Uptime), "B", 0, "L", false, 0, "")
		setColor(pdf, pdfInk, false)
		target := 0.0
		if m.SLATarget != nil {
			target = *m.SLATarget
		}
		pdf.CellFormat(widths[2], 7, fmt.Sprintf("%.2f%%", target), "B", 0, "L", false, 0, "")

		status, color := "Missed", pdfDanger
		if m.SLAMet {
			status, color = "Met", pdfSuccess
		}
		setColor(pdf, color, false)
		pdf.CellFormat(widths[3], 7, pdfText(status), "B", 1, "L", false, 0, "")
	}
	pdf.Ln(2)
}

func drawPDFIncidentSection(pdf *fpdf.Fpdf, data *ReportData) {
	drawSectionHeading(pdf, "Incidents")

	total := 0
	for _, m := range data.Metrics {
		total += m.IncidentCount
	}
	if total == 0 {
		pdf.SetFont("Helvetica", "", 10)
		setColor(pdf, pdfMuted, false)
		pdf.MultiCell(pdfContentW, 5, "No incidents recorded during this period.", "", "L", false)
		return
	}

	pdf.SetFont("Helvetica", "B", 10)
	setColor(pdf, pdfInk, false)
	pdf.CellFormat(pdfContentW, 6, fmt.Sprintf("Total incidents: %d", total), "", 1, "L", false, 0, "")
	pdf.Ln(1)

	for _, m := range data.Metrics {
		if len(m.Incidents) == 0 {
			continue
		}
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "B", 11)
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(pdfContentW, 6,
			pdfText(fmt.Sprintf("%s (%d)", truncate(m.MonitorName, 60), len(m.Incidents))), "", 1, "L", false, 0, "")

		for _, inc := range m.Incidents {
			y := pdf.GetY()
			setColor(pdf, pdfDanger, true)
			pdf.Rect(pdfMarginLeft, y, 1.2, 6, "F")
			pdf.SetX(pdfMarginLeft + 4)

			pdf.SetFont("Helvetica", "B", 9)
			setColor(pdf, pdfInk, false)
			pdf.CellFormat(pdfContentW-44, 6, pdfText(inc.StartTime.In(data.ReportLocation()).Format("Jan 02, 2006 15:04")), "", 0, "L", false, 0, "")
			pdf.SetFont("Helvetica", "", 9)
			setColor(pdf, pdfMuted, false)
			pdf.CellFormat(40, 6, pdfText(fmt.Sprintf("%s  %s", formatMinutes(inc.Duration), inc.Status)), "", 1, "R", false, 0, "")

			for _, detail := range [][2]string{
				{"Root cause", inc.RootCause},
				{"Resolution", inc.ResolutionNotes},
			} {
				if detail[1] == "" {
					continue
				}
				pdf.SetX(pdfMarginLeft + 4)
				pdf.SetFont("Helvetica", "", 8)
				setColor(pdf, pdfMuted, false)
				pdf.MultiCell(pdfContentW-4, 4, pdfText(detail[0]+": "+detail[1]), "", "L", false)
			}
			pdf.Ln(1.5)
		}
	}
}

// drawPDFWarnings surfaces monitors the aggregator could not include. A report
// is a compliance artifact, so an omission is stated on its face rather than
// left to be noticed by its absence.
func drawPDFWarnings(pdf *fpdf.Fpdf, data *ReportData) {
	if len(data.Warnings) == 0 {
		return
	}
	drawSectionHeading(pdf, "Data warnings")
	pdf.SetFont("Helvetica", "", 9)
	setColor(pdf, pdfWarning, false)
	for _, w := range data.Warnings {
		pdf.MultiCell(pdfContentW, 4.5, pdfText("- "+w), "", "L", false)
	}
}

func drawPDFFooter(pdf *fpdf.Fpdf) {
	pdf.SetY(-15)
	pdf.SetFont("Helvetica", "I", 8)
	setColor(pdf, pdfMuted, false)
	pdf.CellFormat(pdfContentW, 6, "Generated by Sentinel", "", 0, "C", false, 0, "")
}

// ---- shared helpers --------------------------------------------------------

// reportTitle prefers the caller's custom title over the report's name.
func reportTitle(data *ReportData) string {
	if data.CustomTitle != nil && *data.CustomTitle != "" {
		return *data.CustomTitle
	}
	return data.ReportName
}

// formatMinutes renders a downtime duration compactly (e.g. "2h 15m").
//
// Sub-minute outages are shown in seconds rather than as "0m". A monitor on a
// 30-second interval produces exactly those, and rendering four of them as "0m"
// next to a 100% uptime figure is how a real outage came to look like nothing
// happened.
func formatMinutes(minutes float64) string {
	if minutes <= 0 {
		return "0s"
	}
	seconds := int(math.Round(minutes * 60))
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	total := int(math.Round(minutes))
	if total < 60 {
		return fmt.Sprintf("%dm", total)
	}
	h, m := total/60, total%60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// formatUptimePercent renders an uptime figure without ever rounding a real
// outage away.
//
// "%.2f" turns 99.9954% into "100.00%", which read as a contradiction beside a
// list of incidents. Anything short of a perfect record is rounded down, so
// 100% means no recorded downtime at all and nothing else does.
func formatUptimePercent(pct float64) string {
	if pct >= 100 {
		return "100.00%"
	}
	floored := math.Floor(pct*100) / 100
	if floored >= 100 {
		floored = 99.99
	}
	return fmt.Sprintf("%.2f%%", floored)
}

// truncate shortens s to max runes, marking that it was cut.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}
```

- [ ] **Step 5: Update the encoding probe test's helper and comment**

In `backend/internal/services/pdf_encoding_test.go`, change:

```go
	name, err := r.RenderReportToPDF(data, []string{models.SectionSLACompliance, models.SectionIncidentSummary}, "probe")
```

to:

```go
	name, err := r.RenderReportToPDF(data, models.ReportTypeUptime, "probe")
```

And in `TestPDF_AccentedMonitorNameSurvives`, change:

```go
		// An SLA target is required for the row to be drawn at all; without
		// one the section prints "no targets configured" and the name never
		// reaches the page.
		Metrics: []ReportMetrics{{
```

to:

```go
		Metrics: []ReportMetrics{{
```

(The row now draws regardless of `SLATarget`; the comment describing the old filtered behavior is stale.)

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/services/... -run 'TestRenderReportToPDF|TestPDF_|TestGetPDFPathRejectsTraversal|TestPDFFilenameIsSanitized|TestFormatMinutes|TestPDFText' -v`
Expected: PASS for all.

- [ ] **Step 7: Run the full backend build**

Run: `cd backend && go build ./...`
Expected: still fails at this point if Task 9 has not yet run (`report_generator.go` and `report_builder_handler.go` still call the old three-argument-with-sections shape indirectly via `template.Sections`, and `report_builder_handler.go` still references `models.ReportTemplate`). That is expected — Task 9 resolves it. If Tasks 7 and 8 are both done, running `go vet ./internal/services/...` alone should be clean for this package.

Run: `cd backend && go vet ./internal/services/...`
Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/services/pdf_renderer.go backend/internal/services/pdf_renderer_test.go \
        backend/internal/services/pdf_encoding_test.go
git rm backend/internal/services/pdf_sections.go
git commit -m "$(cat <<'EOF'
feat(reports): two fixed PDF layouts and the uptime-vs-SLA graph

RenderReportToPDF drops its section-name dispatch for a two-way branch
(uptime / incident), each assembling a fixed layout from the existing
drawing helpers. Adds drawPDFUptimeGraph, a cumulative-uptime-vs-SLA
line chart built from fpdf's own Line/Rect primitives - no new
dependency. Deletes pdf_sections.go (executive summary, timeline,
availability, performance), which existed only to support the section
system this replaces.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 9: Report builder handler contract, dead HTML report path, generator/job-queue cleanup

**Files:**
- Delete: `backend/internal/services/html_sections.go`
- Delete: `backend/internal/services/report_html_generator.go`
- Delete: `backend/internal/services/report_html_generator_test.go`
- Modify: `backend/internal/services/report_timezone_render_test.go`
- Modify: `backend/internal/services/report_generator.go`
- Modify: `backend/internal/services/report_job_queue.go`
- Modify: `backend/internal/api/report_builder_handler.go`

**Interfaces:**
- Consumes: `models.ReportTypeUptime`/`ReportTypeIncident`/`ValidReportTypes` (Task 2), the new `RenderReportToPDF` signature (Task 8).
- Produces: `POST /api/v1/reports/generate` now takes `report_type` instead of `template_id`; `GET /api/v1/reports` and its detail responses carry `report_type` instead of `template_name`; `GET /api/v1/report-templates` no longer exists. Task 10 (frontend types/hooks) and Tasks 11-12 (frontend components) consume this contract.

`report_html_generator.go`'s `GenerateHTMLReport` has zero live callers anywhere in the codebase today (confirmed by grep: `ReportBuilder.htmlGenerator` is constructed but `.GenerateHTMLReport(...)` is never called) - this is pre-existing dead code, unrelated to the report-type rework, that this task removes as a natural side effect of touching every other file that mentions `ReportTemplate`.

- [ ] **Step 1: Delete the dead HTML report path**

```bash
rm backend/internal/services/html_sections.go backend/internal/services/report_html_generator.go \
   backend/internal/services/report_html_generator_test.go
```

- [ ] **Step 2: Remove the one HTML-path test left over in report_timezone_render_test.go**

Replace `backend/internal/services/report_timezone_render_test.go` in full (dropping `TestHTMLReport_RendersInConfiguredZone` and its now-unused `firstLines` helper; `TestReportData_ReportLocationDefaultsToUTC` is unrelated to the HTML path and is unchanged):

```go
package services

import (
	"testing"
	"time"
)

// A report rendered with no zone configured must say UTC, never the server
// process's zone — the container's default is not a choice anyone made.
func TestReportData_ReportLocationDefaultsToUTC(t *testing.T) {
	var nilData *ReportData
	if nilData.ReportLocation() != time.UTC {
		t.Error("nil ReportData should report UTC")
	}
	if (&ReportData{}).ReportLocation() != time.UTC {
		t.Error("unset Location should report UTC")
	}
}
```

- [ ] **Step 3: Remove the template lookup from report_generator.go**

In `backend/internal/services/report_generator.go`, change:

```go
// GenerateAndSaveReport aggregates, renders, and records a report.
func (rg *ReportGenerator) GenerateAndSaveReport(ctx context.Context, report *models.Report, generatedBy uuid.UUID) (*GeneratedReport, error) {
	var template models.ReportTemplate
	if err := rg.db.WithContext(ctx).First(&template, "id = ?", report.TemplateID).Error; err != nil {
		return nil, fmt.Errorf("loading report template: %w", err)
	}

	data, err := rg.aggregator.AggregateReportData(ctx, report, generatedBy)
	if err != nil {
		return nil, fmt.Errorf("aggregating report data: %w", err)
	}

	// Stamped before rendering so both the PDF and anything else built from
	// this data describe the same clock.
	data.Location = rg.reportLocation(ctx)

	filename, err := rg.pdfRenderer.RenderReportToPDF(data, template.Sections, "report_"+report.ID.String()[:8])
	if err != nil {
		return nil, fmt.Errorf("rendering report PDF: %w", err)
	}
```

to:

```go
// GenerateAndSaveReport aggregates, renders, and records a report.
func (rg *ReportGenerator) GenerateAndSaveReport(ctx context.Context, report *models.Report, generatedBy uuid.UUID) (*GeneratedReport, error) {
	data, err := rg.aggregator.AggregateReportData(ctx, report, generatedBy)
	if err != nil {
		return nil, fmt.Errorf("aggregating report data: %w", err)
	}

	// Stamped before rendering so both the PDF and anything else built from
	// this data describe the same clock.
	data.Location = rg.reportLocation(ctx)

	filename, err := rg.pdfRenderer.RenderReportToPDF(data, report.ReportType, "report_"+report.ID.String()[:8])
	if err != nil {
		return nil, fmt.Errorf("rendering report PDF: %w", err)
	}
```

- [ ] **Step 4: Remove the now-unreachable classify case in report_job_queue.go**

In `backend/internal/services/report_job_queue.go`, change:

```go
	switch {
	case strings.Contains(msg, "loading report template"):
		return "report template could not be loaded"
	case strings.Contains(msg, "aggregating report data"):
```

to:

```go
	switch {
	case strings.Contains(msg, "aggregating report data"):
```

(`GenerateAndSaveReport` no longer produces a "loading report template" error, since Step 3 removed the lookup that wrapped it.)

- [ ] **Step 5: Rewrite report_builder_handler.go's report-type surface**

In `backend/internal/api/report_builder_handler.go`:

5a. Remove the unused `errors` import. Change:

```go
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)
```

to:

```go
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)
```

5b. Drop the `htmlGenerator` field. Change:

```go
// ReportBuilder holds the dependencies the report-builder endpoints need.
type ReportBuilder struct {
	db            *gorm.DB
	aggregator    *services.ReportAggregatorService
	pdfRenderer   *services.PDFRendererService
	htmlGenerator *services.HTMLReportGenerator
	// scheduler is used when deleting a report: the database cascades its
	// schedules away, but their cron jobs would otherwise keep firing against
	// rows that no longer exist.
	scheduler *services.ReportSchedulerService
	// jobs renders reports off the request path.
	jobs *services.ReportJobQueue
	// audit records who changed what.
	audit *services.AuditService
	// settings supplies the report timezone, which period labels are resolved
	// in so the list agrees with the rendered report.
	settings *services.SettingsService
}

// NewReportBuilder returns a handler set bound to its dependencies.
func NewReportBuilder(
	db *gorm.DB,
	aggregator *services.ReportAggregatorService,
	pdfRenderer *services.PDFRendererService,
	scheduler *services.ReportSchedulerService,
	settings *services.SettingsService,
) *ReportBuilder {
	return &ReportBuilder{
		db:            db,
		aggregator:    aggregator,
		pdfRenderer:   pdfRenderer,
		htmlGenerator: services.NewHTMLReportGenerator(),
		scheduler:     scheduler,
		settings:      settings,
	}
}
```

to:

```go
// ReportBuilder holds the dependencies the report-builder endpoints need.
type ReportBuilder struct {
	db          *gorm.DB
	aggregator  *services.ReportAggregatorService
	pdfRenderer *services.PDFRendererService
	// scheduler is used when deleting a report: the database cascades its
	// schedules away, but their cron jobs would otherwise keep firing against
	// rows that no longer exist.
	scheduler *services.ReportSchedulerService
	// jobs renders reports off the request path.
	jobs *services.ReportJobQueue
	// audit records who changed what.
	audit *services.AuditService
	// settings supplies the report timezone, which period labels are resolved
	// in so the list agrees with the rendered report.
	settings *services.SettingsService
}

// NewReportBuilder returns a handler set bound to its dependencies.
func NewReportBuilder(
	db *gorm.DB,
	aggregator *services.ReportAggregatorService,
	pdfRenderer *services.PDFRendererService,
	scheduler *services.ReportSchedulerService,
	settings *services.SettingsService,
) *ReportBuilder {
	return &ReportBuilder{
		db:          db,
		aggregator:  aggregator,
		pdfRenderer: pdfRenderer,
		scheduler:   scheduler,
		settings:    settings,
	}
}
```

5c. Change `GenerateReportRequest`. Change:

```go
// GenerateReportRequest creates a report definition and renders it immediately.
type GenerateReportRequest struct {
	Name       string             `json:"name" binding:"required"`
	TemplateID uuid.UUID          `json:"template_id" binding:"required"`
	ScopeType  string             `json:"scope_type" binding:"required,oneof=monitors tags groups types"`
	ScopeData  models.ReportScope `json:"scope_data" binding:"required"`
```

to:

```go
// GenerateReportRequest creates a report definition and renders it immediately.
type GenerateReportRequest struct {
	Name       string             `json:"name" binding:"required"`
	ReportType string             `json:"report_type" binding:"required,oneof=uptime incident"`
	ScopeType  string             `json:"scope_type" binding:"required,oneof=monitors tags groups types"`
	ScopeData  models.ReportScope `json:"scope_data" binding:"required"`
```

5d. Change `ReportResponse`. Change:

```go
// ReportResponse is a report definition plus its generation history.
type ReportResponse struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	TemplateName  string    `json:"template_name"`
	ScopeType     string    `json:"scope_type"`
	TimeRangeDays int       `json:"time_range_days"`
```

to:

```go
// ReportResponse is a report definition plus its generation history.
type ReportResponse struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	ReportType    string    `json:"report_type"`
	ScopeType     string    `json:"scope_type"`
	TimeRangeDays int       `json:"time_range_days"`
```

5e. Remove the template lookup from `GenerateReport` and use `ReportType` directly. Change:

```go
	userID, _, _, ok := GetUserFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var template models.ReportTemplate
	if err := h.db.WithContext(c.Request.Context()).
		First(&template, "id = ?", req.TemplateID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "report template not found")
			return
		}
		respondInternal(c, "loading report template", err)
		return
	}

	report := models.Report{
		ID:                uuid.New(),
		UserID:            userID,
		Name:              req.Name,
		TemplateID:        req.TemplateID,
		ScopeType:         req.ScopeType,
```

to:

```go
	userID, _, _, ok := GetUserFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	report := models.Report{
		ID:                uuid.New(),
		UserID:            userID,
		Name:              req.Name,
		ReportType:        req.ReportType,
		ScopeType:         req.ScopeType,
```

5f. Update the audit log summary. Change:

```go
	h.audit.Record(c.Request.Context(), actorFrom(c),
		models.ActionReportCreated, models.ResourceReport, &report.ID,
		models.AuditChanges{Summary: map[string]any{
			"name":            report.Name,
			"scope_type":      report.ScopeType,
			"time_range_days": report.TimeRangeDays,
			"template_id":     report.TemplateID,
		}})
```

to:

```go
	h.audit.Record(c.Request.Context(), actorFrom(c),
		models.ActionReportCreated, models.ResourceReport, &report.ID,
		models.AuditChanges{Summary: map[string]any{
			"name":            report.Name,
			"scope_type":      report.ScopeType,
			"time_range_days": report.TimeRangeDays,
			"report_type":     report.ReportType,
		}})
```

5g. Remove the template lookup from `buildReportResponse`. Change:

```go
func (h *ReportBuilder) buildReportResponse(ctx context.Context, report *models.Report, shareToken string) (ReportResponse, error) {
	var template models.ReportTemplate
	// A deleted template leaves the name blank rather than failing the listing.
	h.db.WithContext(ctx).First(&template, "id = ?", report.TemplateID)

	var generations []models.ReportGeneration
```

to:

```go
func (h *ReportBuilder) buildReportResponse(ctx context.Context, report *models.Report, shareToken string) (ReportResponse, error) {
	var generations []models.ReportGeneration
```

5h. Use `ReportType` in the built response. Change:

```go
	return ReportResponse{
		ID:            report.ID,
		Name:          report.Name,
		TemplateName:  template.Name,
		ScopeType:     report.ScopeType,
		TimeRangeDays: report.TimeRangeDays,
```

to:

```go
	return ReportResponse{
		ID:            report.ID,
		Name:          report.Name,
		ReportType:    report.ReportType,
		ScopeType:     report.ScopeType,
		TimeRangeDays: report.TimeRangeDays,
```

5i. Remove `ListTemplates` entirely. Delete:

```go
// ListTemplates handles GET /api/v1/report-templates. The report builder needs
// this to offer a template choice; without it the wizard has nothing to select.
func (h *ReportBuilder) ListTemplates(c *gin.Context) {
	var templates []models.ReportTemplate
	if err := h.db.WithContext(c.Request.Context()).
		Order("is_default DESC, name ASC").Find(&templates).Error; err != nil {
		respondInternal(c, "listing report templates", err)
		return
	}
	if templates == nil {
		templates = []models.ReportTemplate{}
	}
	respondSuccess(c, http.StatusOK, templates)
}

```

5j. Remove its route. Change:

```go
	// Sibling resources the builder UI needs. They sit outside the /reports
	// group so they do not collide with its ":id" wildcard.
	rg.GET("/report-templates", builder.ListTemplates)
	rg.GET("/monitor-tags", builder.ListMonitorTags)
```

to:

```go
	// A sibling resource the builder UI needs. It sits outside the /reports
	// group so it does not collide with its ":id" wildcard.
	rg.GET("/monitor-tags", builder.ListMonitorTags)
```

- [ ] **Step 6: Run the full backend build and test suite**

Run: `cd backend && go build ./... && go test ./...`
Expected: builds cleanly and every test passes. This is the first point since Task 2 where the whole backend is expected to compile — if it does not, check for a remaining reference to `ReportTemplate`, `TemplateID`, or the old `RenderReportToPDF([]string, ...)` shape:

```bash
grep -rn "ReportTemplate\|TemplateID\|template_id\|HTMLReportGenerator" --include=*.go backend/internal/
```

Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/services/report_generator.go backend/internal/services/report_job_queue.go \
        backend/internal/services/report_timezone_render_test.go backend/internal/api/report_builder_handler.go
git rm backend/internal/services/html_sections.go backend/internal/services/report_html_generator.go \
       backend/internal/services/report_html_generator_test.go
git commit -m "$(cat <<'EOF'
feat(reports): report_type replaces template_id across the builder API

POST /reports/generate now takes report_type instead of template_id;
GET /reports and its detail responses carry report_type instead of
template_name; GET /report-templates is removed. Also deletes the HTML
report path (report_html_generator.go, html_sections.go), which had
zero live callers before this change and existed only to render the
same templates the API surface above no longer has.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 10: Frontend types and hooks — ReportType replaces ReportTemplate

**Files:**
- Modify: `frontend/src/types/reports.ts`
- Modify: `frontend/src/hooks/useReportBuilder.ts`

**Interfaces:**
- Consumes: the backend contract from Task 9 (`report_type` field, no `/report-templates` endpoint).
- Produces: `export type ReportType = 'uptime' | 'incident'`, `export const REPORT_TYPE_LABEL: Record<ReportType, string>`. `ReportTemplate` and `useReportTemplates` no longer exist. Tasks 11, 12 consume `ReportType`/`REPORT_TYPE_LABEL`.

This is a types-and-data-layer-only task with no independent runtime behavior to test; frontend tests are the type checker (`tsc`) plus the manual verification performed once the components that use these types are updated in Tasks 11-12. There is no existing frontend unit test suite covering `useReportBuilder.ts` to extend.

- [ ] **Step 1: Update types/reports.ts**

In `frontend/src/types/reports.ts`, change:

```ts
export type ReportScopeType = 'monitors' | 'tags' | 'groups' | 'types'
export type ScheduleType = 'daily' | 'weekly' | 'monthly' | 'quarterly' | 'custom'

export interface ReportTemplate {
  id: string
  name: string
  is_default: boolean
  /** Section keys, in render order: sla_compliance, incident_summary, charts, custom. */
  sections: string[]
  created_at: string
}
```

to:

```ts
export type ReportScopeType = 'monitors' | 'tags' | 'groups' | 'types'
export type ScheduleType = 'daily' | 'weekly' | 'monthly' | 'quarterly' | 'custom'

/** The two fixed report types. No template system, no section picker. */
export type ReportType = 'uptime' | 'incident'

export const REPORT_TYPE_LABEL: Record<ReportType, string> = {
  uptime: 'Uptime Report',
  incident: 'Incident Report',
}
```

Then change:

```ts
export interface SavedReport {
  id: string
  name: string
  template_name: string
  scope_type: ReportScopeType
```

to:

```ts
export interface SavedReport {
  id: string
  name: string
  report_type: ReportType
  scope_type: ReportScopeType
```

Then change:

```ts
export interface CreateReportPayload {
  name: string
  template_id: string
  scope_type: ReportScopeType
```

to:

```ts
export interface CreateReportPayload {
  name: string
  report_type: ReportType
  scope_type: ReportScopeType
```

- [ ] **Step 2: Update useReportBuilder.ts**

In `frontend/src/hooks/useReportBuilder.ts`, change:

```ts
import type {
  CreateReportPayload,
  CreateSchedulePayload,
  GenerateReportResult,
  ReportJob,
  ReportSchedule,
  ReportTemplate,
  SavedReport,
  ShareLink,
  ShareReportResult,
} from '@/types/reports'
```

to:

```ts
import type {
  CreateReportPayload,
  CreateSchedulePayload,
  GenerateReportResult,
  ReportJob,
  ReportSchedule,
  SavedReport,
  ShareLink,
  ShareReportResult,
} from '@/types/reports'
```

Then delete `useReportTemplates` entirely:

```ts
/** useReportTemplates loads the templates the wizard offers. */
export function useReportTemplates() {
  const [templates, setTemplates] = useState<ReportTemplate[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const listTemplates = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const { data } = await api.get<ApiResponse<ReportTemplate[]>>('/report-templates')
      setTemplates(data.data ?? [])
    } catch (err) {
      setError(err as ApiError)
    } finally {
      setLoading(false)
    }
  }, [])

  return { templates, loading, error, listTemplates }
}

```

(Delete the whole block, including the trailing blank line, so `useReportSchedules` follows directly after `usePublicReport`... actually `useReportTemplates` sits between `useSavedReports`/`useReportJobs`/`getReportJob`/`waitForReportJob`/`useShareLinks` and `useReportSchedules` — remove only this block, leaving `useShareLinks` immediately followed by `useReportSchedules`.)

- [ ] **Step 3: Verify the frontend type-checks**

Run: `cd frontend && npx tsc --noEmit`
Expected: errors in `ReportBuilderWizard.tsx`, `GenerateReportModal.tsx`, `SavedReports.tsx`, and `SavedReportDetail.tsx` (they still reference `useReportTemplates`/`ReportTemplate`/`template_id`/`template_name`) — this is expected here and resolved in Tasks 11-12. Confirm the errors are confined to those four files and nothing else.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/types/reports.ts frontend/src/hooks/useReportBuilder.ts
git commit -m "$(cat <<'EOF'
feat(reports): ReportType replaces ReportTemplate in the frontend data layer

CreateReportPayload.template_id becomes report_type,
SavedReport.template_name becomes report_type, and useReportTemplates
is removed along with the /report-templates endpoint it called. Every
consumer is updated in the next two tasks.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 11: Frontend report wizards — Report Type step replaces Template step

**Files:**
- Modify: `frontend/src/components/ReportBuilderWizard.tsx` (whole file)
- Modify: `frontend/src/components/GenerateReportModal.tsx` (whole file)

**Interfaces:**
- Consumes: `ReportType`/`REPORT_TYPE_LABEL` (Task 10).
- Produces: no new exports; both components now send `report_type` instead of `template_id` when creating a report.

- [ ] **Step 1: Rewrite ReportBuilderWizard.tsx**

Replace `frontend/src/components/ReportBuilderWizard.tsx` in full:

```tsx
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, ChevronLeft, ChevronRight, Loader2 } from 'lucide-react'
import { useMonitors } from '@/hooks/useMonitors'
import { useMonitorGroups } from '@/hooks/useMonitorGroups'
import { useMonitorTags, useSavedReports, waitForReportJob } from '@/hooks/useReportBuilder'
import PeriodSelector, { DEFAULT_PERIOD, describePeriod } from '@/components/PeriodSelector'
import { REPORT_TYPE_LABEL } from '@/types/reports'
import type { ReportPeriod, ReportScopeType, ReportType } from '@/types/reports'

type WizardStep = 1 | 2 | 3 | 4

const STEPS = [
  { number: 1, title: 'Scope' },
  { number: 2, title: 'Period' },
  { number: 3, title: 'Report Type' },
  { number: 4, title: 'Details' },
] as const

// The check types a report may be scoped to, in the order the app shows them.
// Webhook is absent: it receives rather than checks, so it has no incidents.
const REPORTABLE_TYPES = ['http', 'dns', 'ping', 'tcp']

interface ReportBuilderWizardProps {
  onError?: (message: string) => void
}

/**
 * ReportBuilderWizard walks through defining a saved report: what it covers,
 * over what period, of which type, with optional title and description.
 * Generating it renders a PDF immediately.
 */
export default function ReportBuilderWizard({ onError }: ReportBuilderWizardProps) {
  const navigate = useNavigate()
  const { createReport } = useSavedReports()
  const { monitors, loading: monitorsLoading } = useMonitors()
  const { groups, loading: groupsLoading } = useMonitorGroups()
  const { tags, listTags, loading: tagsLoading } = useMonitorTags()

  const [step, setStep] = useState<WizardStep>(1)
  const [generating, setGenerating] = useState(false)
  // Rendering is queued, so the button reflects the job's actual state rather
  // than a generic spinner.
  const [progress, setProgress] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [scopeType, setScopeType] = useState<ReportScopeType>('monitors')
  const [selection, setSelection] = useState<string[]>([])
  const [period, setPeriod] = useState<ReportPeriod>(DEFAULT_PERIOD)
  const [reportType, setReportType] = useState<ReportType>('uptime')
  const [customTitle, setCustomTitle] = useState('')
  const [customDescription, setCustomDescription] = useState('')

  // Tags are fetched once, not on every step change.
  useEffect(() => {
    listTags()
  }, [listTags])

  // Options for the active scope tab. Tags are their own identity - there is no
  // separate id - so the value and the label are the same string.
  const options = useMemo(() => {
    if (scopeType === 'monitors') return monitors.map((m) => ({ id: m.id, name: m.name }))
    if (scopeType === 'groups') return groups.map((g) => ({ id: g.id, name: g.name }))
    if (scopeType === 'types') {
      // Only types that have monitors. Offering "PING" with no ping monitors
      // would build a report that is empty for a reason the reader cannot see.
      const counts = new Map<string, number>()
      for (const m of monitors) {
        if (REPORTABLE_TYPES.includes(m.type)) counts.set(m.type, (counts.get(m.type) ?? 0) + 1)
      }
      return REPORTABLE_TYPES.filter((t) => counts.has(t)).map((t) => ({
        id: t,
        name: `${t.toUpperCase()} (${counts.get(t)} monitor${counts.get(t) === 1 ? '' : 's'})`,
      }))
    }
    return tags.map((t) => ({ id: t, name: t }))
  }, [scopeType, monitors, groups, tags])

  const optionsLoading =
    (scopeType === 'monitors' && monitorsLoading) ||
    (scopeType === 'groups' && groupsLoading) ||
    (scopeType === 'tags' && tagsLoading) ||
    (scopeType === 'types' && monitorsLoading)

  const changeScopeType = (type: ReportScopeType) => {
    setScopeType(type)
    setSelection([])
  }

  const toggle = (id: string) =>
    setSelection((cur) => (cur.includes(id) ? cur.filter((s) => s !== id) : [...cur, id]))

  // Validation lives here so Next is disabled rather than failing on click.
  const stepError = useMemo(() => {
    if (step === 1) {
      if (!name.trim()) return 'Give the report a name'
      if (selection.length === 0) {
        return `Select at least one ${scopeType === 'types' ? 'monitor type' : scopeType.slice(0, -1)}`
      }
    }
    if (
      step === 2 &&
      period.period_kind === 'custom' &&
      (!period.period_start || !period.period_end)
    ) {
      return 'Choose both a start and an end date'
    }
    return null
  }, [step, name, selection, scopeType, period])

  const generate = async () => {
    setGenerating(true)
    try {
      const scopeData =
        scopeType === 'monitors'
          ? { monitor_ids: selection }
          : scopeType === 'tags'
            ? { tags: selection }
            : scopeType === 'types'
              ? { types: selection }
              : { group_ids: selection }

      const result = await createReport({
        name: name.trim(),
        report_type: reportType,
        scope_type: scopeType,
        scope_data: scopeData,
        ...period,
        custom_title: customTitle.trim() || undefined,
        custom_description: customDescription.trim() || undefined,
      })

      // The report exists now; its first PDF is still rendering. Wait for the
      // job so the detail page does not open on an empty generation list.
      setProgress('Queued…')
      try {
        await waitForReportJob(result.job_id, {
          onProgress: (job) =>
            setProgress(job.status === 'running' ? 'Rendering…' : 'Queued…'),
        })
      } catch (jobErr) {
        // The definition was saved even though the render failed, so send the
        // user to it rather than losing their work, and say what happened.
        onError?.(
          (jobErr as { message?: string }).message ??
            'The report was saved but its PDF could not be generated'
        )
      }
      navigate(`/reports/${result.id}`)
    } catch (err) {
      onError?.((err as { message?: string }).message ?? 'Could not generate the report')
    } finally {
      setGenerating(false)
      setProgress(null)
    }
  }

  return (
    <div className="mx-auto max-w-2xl space-y-5 pb-10">
      {/* Step indicator */}
      <div className="flex items-center">
        {STEPS.map((s, idx) => (
          <div key={s.number} className="flex flex-1 items-center">
            <div
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-sm font-semibold"
              style={{
                background: step >= s.number ? 'var(--vs-ecg)' : 'var(--vs-panel-2)',
                color: step >= s.number ? 'var(--vs-bg)' : 'var(--vs-text-dim)',
              }}
            >
              {step > s.number ? <Check className="h-4 w-4" /> : s.number}
            </div>
            <span
              className="ml-2 hidden text-sm sm:inline"
              style={{ color: step >= s.number ? 'var(--vs-text)' : 'var(--vs-text-dim)' }}
            >
              {s.title}
            </span>
            {idx < STEPS.length - 1 && (
              <div
                className="mx-3 h-px flex-1"
                style={{ background: step > s.number ? 'var(--vs-ecg)' : 'var(--vs-line)' }}
              />
            )}
          </div>
        ))}
      </div>

      {step === 1 && (
        <div className="rd-card space-y-5 p-5">
          <div>
            <label className="mb-1 block text-sm font-medium">Report name</label>
            <input
              className="rd-input w-full"
              placeholder="Weekly service report"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div>
            <span className="vs-eyebrow mb-2 block">Scope</span>
            <div className="mb-3 flex gap-1 border-b" style={{ borderColor: 'var(--vs-line)' }}>
              {(['monitors', 'types', 'groups', 'tags'] as const).map((type) => (
                <button
                  key={type}
                  type="button"
                  onClick={() => changeScopeType(type)}
                  className="px-3 py-2 text-sm font-medium capitalize"
                  style={{
                    color: scopeType === type ? 'var(--vs-ecg)' : 'var(--vs-text-dim)',
                    borderBottom:
                      scopeType === type ? '2px solid var(--vs-ecg)' : '2px solid transparent',
                  }}
                >
                  {type}
                </button>
              ))}
            </div>

            <div className="max-h-64 space-y-1 overflow-y-auto">
              {optionsLoading && (
                <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
                  Loading…
                </p>
              )}
              {!optionsLoading && options.length === 0 && (
                <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
                  No {scopeType === 'types' ? 'monitor types' : scopeType} available.
                  {scopeType === 'tags' && ' Tag a monitor first to scope a report by tag.'}
                  {scopeType === 'types' && ' Create a monitor first.'}
                </p>
              )}
              {!optionsLoading &&
                options.map((o) => (
                  <label
                    key={o.id}
                    className="flex cursor-pointer items-center gap-3 rounded px-2 py-2 text-sm hover:bg-white/5"
                  >
                    <input
                      type="checkbox"
                      className="h-4 w-4 rounded"
                      checked={selection.includes(o.id)}
                      onChange={() => toggle(o.id)}
                    />
                    <span>{o.name}</span>
                  </label>
                ))}
            </div>
            <p className="mt-2 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
              {selection.length} selected
            </p>
          </div>
        </div>
      )}

      {step === 2 && (
        <div className="rd-card space-y-5 p-5">
          <span className="vs-eyebrow block">Reporting period</span>
          {/* The same selector the quick dialog uses: the two paths must not
              offer different periods, or a report built one way cannot be
              reproduced the other. */}
          <PeriodSelector value={period} onChange={setPeriod} />
        </div>
      )}

      {step === 3 && (
        <div className="rd-card space-y-3 p-5">
          <span className="vs-eyebrow block">Report type</span>
          {(['uptime', 'incident'] as const).map((t) => (
            <button
              key={t}
              type="button"
              onClick={() => setReportType(t)}
              className="w-full rounded-md p-4 text-left"
              style={{
                border: `1px solid ${reportType === t ? 'var(--vs-ecg)' : 'var(--vs-line)'}`,
              }}
            >
              <p className="font-medium">{REPORT_TYPE_LABEL[t]}</p>
              <p className="mt-1 text-xs" style={{ color: 'var(--vs-text-dim)' }}>
                {t === 'uptime'
                  ? 'Uptime vs. SLA, with a cumulative-uptime graph and a per-monitor breakdown.'
                  : 'Every incident in scope, with root cause and resolution detail.'}
              </p>
            </button>
          ))}
        </div>
      )}

      {step === 4 && (
        <div className="rd-card space-y-4 p-5">
          <span className="vs-eyebrow block">Details (optional)</span>
          <div>
            <label className="mb-1 block text-sm font-medium">Title on the report</label>
            <input
              className="rd-input w-full"
              placeholder="Leave blank to use the report name"
              value={customTitle}
              onChange={(e) => setCustomTitle(e.target.value)}
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium">Description</label>
            <textarea
              className="rd-input h-24 w-full resize-none"
              placeholder="Context or notes for whoever reads this"
              value={customDescription}
              onChange={(e) => setCustomDescription(e.target.value)}
            />
          </div>

          <div
            className="rounded-md p-4 text-sm"
            style={{ background: 'var(--vs-panel-2)', color: 'var(--vs-text-dim)' }}
          >
            <p>
              <strong style={{ color: 'var(--vs-text)' }}>{name || 'Untitled'}</strong>
            </p>
            <p className="mt-1">
              {selection.length} {scopeType} · {describePeriod(period)} ·{' '}
              {REPORT_TYPE_LABEL[reportType]}
            </p>
          </div>
        </div>
      )}

      {stepError && (
        <p className="text-sm" style={{ color: 'var(--vs-amber)' }}>
          {stepError}
        </p>
      )}

      <div className="flex items-center justify-between gap-3">
        <button
          type="button"
          className="rd-btn rd-btn-secondary"
          onClick={() => setStep((s) => (s > 1 ? ((s - 1) as WizardStep) : s))}
          disabled={step === 1}
        >
          <ChevronLeft className="h-4 w-4" /> Back
        </button>

        {step < 4 ? (
          <button
            type="button"
            className="rd-btn rd-btn-primary"
            onClick={() => setStep((s) => (s + 1) as WizardStep)}
            disabled={stepError !== null}
          >
            Next <ChevronRight className="h-4 w-4" />
          </button>
        ) : (
          <button
            type="button"
            className="rd-btn rd-btn-primary"
            onClick={generate}
            disabled={generating || stepError !== null}
          >
            {generating ? (
              <span className="flex items-center gap-2">
                <Loader2 className="h-4 w-4 animate-spin" /> {progress ?? 'Saving…'}
              </span>
            ) : (
              'Generate report'
            )}
          </button>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Rewrite GenerateReportModal.tsx**

Replace `frontend/src/components/GenerateReportModal.tsx` in full:

```tsx
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { FileText, Loader2, Download, ExternalLink, Check, AlertTriangle } from 'lucide-react'
import {
  useSavedReports,
  useMonitorTags,
  waitForReportJob,
  downloadReportPDF,
} from '@/hooks/useReportBuilder'
import { useMonitors } from '@/hooks/useMonitors'
import { useMonitorGroups } from '@/hooks/useMonitorGroups'
import PeriodSelector, { DEFAULT_PERIOD, describePeriod } from '@/components/PeriodSelector'
import { REPORT_TYPE_LABEL } from '@/types/reports'
import type { ReportPeriod, ReportScopeType, ReportType } from '@/types/reports'

/** What a report covers, when the caller already knows — a monitor's own page. */
export interface FixedScope {
  scope_type: ReportScopeType
  ids: string[]
  /** How to describe it in the dialog, e.g. the monitor's name. */
  label: string
}

// Webhook is absent: it receives rather than checks, so it has no incidents.
const REPORTABLE_TYPES = ['http', 'dns', 'ping', 'tcp']

const SCOPE_TABS: { value: ReportScopeType; label: string }[] = [
  { value: 'monitors', label: 'Monitors' },
  { value: 'groups', label: 'Groups' },
  { value: 'tags', label: 'Tags' },
  { value: 'types', label: 'Types' },
]

type Phase =
  | { kind: 'form' }
  | { kind: 'working'; message: string }
  | { kind: 'done'; downloadURL: string | null; reportID: string }
  | { kind: 'error'; message: string; reportID?: string }

/**
 * Generates a report in one dialog.
 *
 * Most of the time the question is "last month, these services, that report",
 * and that fits in one screen. Given a fixedScope — a monitor's own page — the
 * scope picker is dropped entirely, since it is already answered.
 */
export default function GenerateReportModal({
  isOpen,
  onClose,
  fixedScope,
}: {
  isOpen: boolean
  onClose: () => void
  fixedScope?: FixedScope
}) {
  const navigate = useNavigate()
  const { createReport } = useSavedReports()
  const { monitors, loading: monitorsLoading } = useMonitors()
  const { groups, loading: groupsLoading } = useMonitorGroups()
  const { tags, listTags, loading: tagsLoading } = useMonitorTags()

  const [reportType, setReportType] = useState<ReportType>('uptime')
  const [period, setPeriod] = useState<ReportPeriod>(DEFAULT_PERIOD)
  const [scopeType, setScopeType] = useState<ReportScopeType>('monitors')
  const [selection, setSelection] = useState<string[]>([])
  const [phase, setPhase] = useState<Phase>({ kind: 'form' })

  useEffect(() => {
    if (!isOpen || fixedScope) return
    void listTags()
  }, [isOpen, fixedScope, listTags])

  // Reopening should start a fresh form rather than show the previous result.
  useEffect(() => {
    if (isOpen) {
      setPhase({ kind: 'form' })
      setSelection([])
    }
  }, [isOpen])

  // Options for the active scope tab. A tag is its own identity — there is no
  // separate id — so its value and label are the same string.
  const options = useMemo(() => {
    if (scopeType === 'monitors') return monitors.map((m) => ({ id: m.id, name: m.name }))
    if (scopeType === 'groups') return groups.map((g) => ({ id: g.id, name: g.name }))
    if (scopeType === 'types') {
      // Only types that have monitors. Offering PING with no ping monitors
      // builds a report that is empty for a reason the reader cannot see.
      const counts = new Map<string, number>()
      for (const m of monitors) {
        if (REPORTABLE_TYPES.includes(m.type)) counts.set(m.type, (counts.get(m.type) ?? 0) + 1)
      }
      return REPORTABLE_TYPES.filter((t) => counts.has(t)).map((t) => ({
        id: t,
        name: `${t.toUpperCase()} (${counts.get(t)})`,
      }))
    }
    return tags.map((t) => ({ id: t, name: t }))
  }, [scopeType, monitors, groups, tags])

  const optionsLoading =
    (scopeType === 'monitors' && monitorsLoading) ||
    (scopeType === 'groups' && groupsLoading) ||
    (scopeType === 'tags' && tagsLoading) ||
    (scopeType === 'types' && monitorsLoading)

  const effectiveScope: FixedScope | null = fixedScope
    ? fixedScope
    : selection.length > 0
      ? {
          scope_type: scopeType,
          ids: selection,
          label:
            selection.length === 1
              ? (options.find((o) => o.id === selection[0])?.name ?? selection[0])
              : `${selection.length} ${scopeType}`,
        }
      : null

  const periodValid =
    period.period_kind !== 'custom' || (!!period.period_start && !!period.period_end)
  const canGenerate = !!effectiveScope && periodValid

  if (!isOpen) return null

  const scopeData = (scope: FixedScope) => {
    switch (scope.scope_type) {
      case 'monitors':
        return { monitor_ids: scope.ids }
      case 'groups':
        return { group_ids: scope.ids }
      case 'tags':
        return { tags: scope.ids }
      default:
        return { types: scope.ids }
    }
  }

  const generate = async () => {
    if (!effectiveScope || !periodValid) return
    setPhase({ kind: 'working', message: 'Creating the report…' })
    let reportID: string | undefined
    try {
      const label = REPORT_TYPE_LABEL[reportType]
      const result = await createReport({
        name: `${effectiveScope.label} — ${label}`,
        report_type: reportType,
        scope_type: effectiveScope.scope_type,
        scope_data: scopeData(effectiveScope),
        ...period,
        custom_title: `${effectiveScope.label}: ${label}`,
        custom_description: describePeriod(period),
      })
      reportID = result.id

      setPhase({ kind: 'working', message: 'Queued…' })
      const job = await waitForReportJob(result.job_id, {
        onProgress: (j) =>
          setPhase({
            kind: 'working',
            message: j.status === 'running' ? 'Rendering…' : 'Queued…',
          }),
      })
      setPhase({ kind: 'done', downloadURL: job.download_url ?? null, reportID: result.id })
    } catch (err) {
      // A failed render still leaves the definition saved, so the report is
      // offered rather than lost — it can be re-run from its own page.
      setPhase({
        kind: 'error',
        message: (err as { message?: string }).message ?? 'Could not generate the report',
        reportID,
      })
    }
  }

  const download = async (url: string) => {
    const stamp = new Date().toISOString().slice(0, 10)
    await downloadReportPDF(url, `${effectiveScope?.label ?? 'report'}-${stamp}.pdf`)
  }

  const toggle = (id: string) =>
    setSelection((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]))

  const working = phase.kind === 'working'

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && !working && onClose()}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Generate a report"
        className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-xl border border-white/10 bg-slate-900/95 p-6"
      >
        <h3 className="flex items-center gap-2 text-lg font-semibold text-white">
          <FileText className="h-5 w-5" aria-hidden /> Generate Report
        </h3>
        {fixedScope && (
          <p className="mt-1 text-sm text-slate-400">
            Covering <span className="text-slate-200">{fixedScope.label}</span> only.
          </p>
        )}

        {phase.kind === 'form' && (
          <div className="mt-5 space-y-5">
            {!fixedScope && (
              <fieldset>
                <legend className="mb-2 text-sm font-medium text-white">What to cover</legend>
                <div className="mb-2 flex flex-wrap gap-1">
                  {SCOPE_TABS.map((t) => (
                    <button
                      key={t.value}
                      type="button"
                      onClick={() => {
                        setScopeType(t.value)
                        setSelection([])
                      }}
                      className={`rounded-lg px-3 py-1.5 text-sm transition ${
                        scopeType === t.value
                          ? 'bg-primary-500/15 text-white'
                          : 'text-slate-400 hover:text-white'
                      }`}
                    >
                      {t.label}
                    </button>
                  ))}
                </div>
                {optionsLoading ? (
                  <div className="flex items-center gap-2 text-sm text-slate-400">
                    <Loader2 className="h-4 w-4 animate-spin" /> Loading…
                  </div>
                ) : options.length === 0 ? (
                  <p className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-300">
                    No {scopeType} available.
                    {scopeType === 'tags' && ' Tag a monitor first to scope a report by tag.'}
                  </p>
                ) : (
                  <div className="max-h-40 space-y-1 overflow-y-auto rounded-lg border border-white/10 bg-slate-800/40 p-2">
                    {options.map((o) => (
                      <label
                        key={o.id}
                        className="flex cursor-pointer items-center gap-2 rounded px-2 py-1 text-sm text-slate-200 hover:bg-white/5"
                      >
                        <input
                          type="checkbox"
                          checked={selection.includes(o.id)}
                          onChange={() => toggle(o.id)}
                        />
                        {o.name}
                      </label>
                    ))}
                  </div>
                )}
              </fieldset>
            )}

            <fieldset>
              <legend className="mb-2 text-sm font-medium text-white">Period</legend>
              <PeriodSelector value={period} onChange={setPeriod} />
            </fieldset>

            <fieldset>
              <legend className="mb-2 text-sm font-medium text-white">Report type</legend>
              <div className="space-y-2">
                {(['uptime', 'incident'] as const).map((t) => (
                  <label
                    key={t}
                    className={`flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition ${
                      reportType === t
                        ? 'border-primary-500/60 bg-primary-500/10'
                        : 'border-white/10 bg-slate-800/40 hover:border-white/25'
                    }`}
                  >
                    <input
                      type="radio"
                      name="report-type"
                      className="mt-1"
                      checked={reportType === t}
                      onChange={() => setReportType(t)}
                    />
                    <span className="min-w-0">
                      <span className="block text-sm font-medium text-white">
                        {REPORT_TYPE_LABEL[t]}
                      </span>
                      <span className="block text-xs text-slate-400">
                        {t === 'uptime'
                          ? 'Uptime vs. SLA, with a cumulative-uptime graph and a per-monitor breakdown.'
                          : 'Every incident in scope, with root cause and resolution detail.'}
                      </span>
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>

            <p className="rounded-lg border border-white/10 bg-slate-800/40 p-3 text-xs text-slate-400">
              {canGenerate && effectiveScope ? (
                <>
                  <span className="text-slate-200">{REPORT_TYPE_LABEL[reportType]}</span> for{' '}
                  <span className="text-slate-200">{effectiveScope.label}</span>,{' '}
                  {describePeriod(period).toLowerCase()}. Saved under Reports, where it can be
                  shared or scheduled.
                </>
              ) : !effectiveScope ? (
                'Choose at least one thing to report on.'
              ) : (
                'Choose both dates for a custom period.'
              )}
            </p>

            <div className="flex justify-end gap-2">
              <button className="btn-secondary" onClick={onClose}>
                Cancel
              </button>
              <button className="btn-primary" disabled={!canGenerate} onClick={() => void generate()}>
                <FileText className="h-4 w-4" /> Generate
              </button>
            </div>
          </div>
        )}

        {phase.kind === 'working' && (
          <div className="mt-6 space-y-3">
            <div className="flex items-center gap-2 text-sm text-slate-300">
              <Loader2 className="h-4 w-4 animate-spin" /> {phase.message}
            </div>
            <p className="text-xs text-slate-500">
              Rendering happens on the server and keeps going if this is closed — the report appears
              under Reports either way.
            </p>
          </div>
        )}

        {phase.kind === 'done' && (
          <div className="mt-6 space-y-4">
            <div className="flex items-center gap-2 text-sm text-emerald-400">
              <Check className="h-4 w-4" /> Report ready.
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <button className="btn-secondary" onClick={onClose}>
                Close
              </button>
              <button
                className="btn-secondary"
                onClick={() => navigate(`/reports/${phase.reportID}`)}
              >
                <ExternalLink className="h-4 w-4" /> Open in Reports
              </button>
              {phase.downloadURL && (
                <button className="btn-primary" onClick={() => void download(phase.downloadURL!)}>
                  <Download className="h-4 w-4" /> Download PDF
                </button>
              )}
            </div>
          </div>
        )}

        {phase.kind === 'error' && (
          <div className="mt-6 space-y-4">
            <div className="flex items-start gap-2 rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-400">
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
              <span>{phase.message}</span>
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <button className="btn-secondary" onClick={() => setPhase({ kind: 'form' })}>
                Back
              </button>
              {phase.reportID && (
                <button
                  className="btn-secondary"
                  onClick={() => navigate(`/reports/${phase.reportID}`)}
                >
                  <ExternalLink className="h-4 w-4" /> Open in Reports
                </button>
              )}
              <button className="btn-primary" onClick={onClose}>
                Close
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Verify the frontend type-checks and builds**

Run: `cd frontend && npx tsc --noEmit && npm run build`
Expected: no errors from either of these two files. (`SavedReports.tsx`/`SavedReportDetail.tsx` still error until Task 12 — that is expected here.)

- [ ] **Step 4: Manually verify in the browser**

Start the dev stack, open the reports section, and confirm: the 4-step wizard's third step now shows two "Report Type" cards (Uptime Report / Incident Report) instead of a template list; the quick "Generate Report" dialog shows the same two options as radio choices; generating either one succeeds and the resulting report opens under Reports.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ReportBuilderWizard.tsx frontend/src/components/GenerateReportModal.tsx
git commit -m "$(cat <<'EOF'
feat(reports): Report Type step replaces the Template step

Both report-creation UIs (the 4-step wizard and the quick dialog) now
offer a fixed choice between Uptime Report and Incident Report instead
of a list of templates loaded from the server.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 12: Frontend saved-report pages — display report_type

**Files:**
- Modify: `frontend/src/pages/SavedReports.tsx`
- Modify: `frontend/src/pages/SavedReportDetail.tsx`

**Interfaces:**
- Consumes: `REPORT_TYPE_LABEL`, `SavedReport.report_type` (Task 10).

- [ ] **Step 1: Update SavedReports.tsx**

In `frontend/src/pages/SavedReports.tsx`, change:

```tsx
import type { SavedReport } from '@/types/reports'
```

to:

```tsx
import { REPORT_TYPE_LABEL, type SavedReport } from '@/types/reports'
```

Then change:

```tsx
                  <span>{report.template_name}</span>
```

to:

```tsx
                  <span>{REPORT_TYPE_LABEL[report.report_type]}</span>
```

- [ ] **Step 2: Update SavedReportDetail.tsx**

In `frontend/src/pages/SavedReportDetail.tsx`, change:

```tsx
import type { ReportSchedule } from '@/types/reports'
```

to:

```tsx
import { REPORT_TYPE_LABEL, type ReportSchedule } from '@/types/reports'
```

Then change:

```tsx
            {report.template_name} · {report.scope_type} · {report.time_range_days} day window
```

to:

```tsx
            {REPORT_TYPE_LABEL[report.report_type]} · {report.scope_type} · {report.time_range_days} day window
```

- [ ] **Step 3: Verify the frontend type-checks and builds**

Run: `cd frontend && npx tsc --noEmit && npm run build`
Expected: no errors anywhere in the project — this is the point at which every reference to the old template system is gone from the frontend.

Confirm with:

```bash
grep -rn "template_name\|template_id\|ReportTemplate\|useReportTemplates" frontend/src/
```

Expected: no output.

- [ ] **Step 4: Manually verify in the browser**

Open the Reports list and a saved report's detail page; confirm each shows "Uptime Report" or "Incident Report" where it used to show a template name.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/SavedReports.tsx frontend/src/pages/SavedReportDetail.tsx
git commit -m "$(cat <<'EOF'
feat(reports): show the report type on the saved-reports pages

Replaces the last two template_name references with
REPORT_TYPE_LABEL[report.report_type] - the frontend no longer
mentions the template system anywhere.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 13: Frontend Settings — Default SLA Target card

**Files:**
- Modify: `frontend/src/pages/Settings.tsx`

**Interfaces:**
- Consumes: `GET /settings`/`PATCH /settings/system`'s `default_sla_target` field (Task 5).

- [ ] **Step 1: Add the bounds constants**

In `frontend/src/pages/Settings.tsx`, change:

```ts
const MIN_INTERVAL = 10
const MAX_INTERVAL = 3600
const MIN_CHECK_RETENTION = 1
const MAX_CHECK_RETENTION = 3650
const MAX_APP_NAME = 40
```

to:

```ts
const MIN_INTERVAL = 10
const MAX_INTERVAL = 3600
const MIN_CHECK_RETENTION = 1
const MAX_CHECK_RETENTION = 3650
const MAX_APP_NAME = 40
const MIN_SLA_TARGET = 0
const MAX_SLA_TARGET = 100
```

- [ ] **Step 2: Add the field to SystemSettings**

Change:

```ts
interface SystemSettings {
  app_name: string
  base_url: string
  default_check_interval: number
  /** How long individual check results are kept. */
  check_retention_days: number
  /** Where agent install commands download from. Empty means derive it. */
  sentinel_external_url: string
  /** Where agents report metrics. Empty means use the external URL. */
  sentinel_internal_url: string
  /** IANA zone that reports are rendered in and the UI displays times in. */
  report_timezone: string
}
```

to:

```ts
interface SystemSettings {
  app_name: string
  base_url: string
  default_check_interval: number
  /** How long individual check results are kept. */
  check_retention_days: number
  /** Where agent install commands download from. Empty means derive it. */
  sentinel_external_url: string
  /** Where agents report metrics. Empty means use the external URL. */
  sentinel_internal_url: string
  /** IANA zone that reports are rendered in and the UI displays times in. */
  report_timezone: string
  /** Uptime percentage a monitor is held to when it has no override of its own. */
  default_sla_target: number
}
```

- [ ] **Step 3: Load and save the field**

Change:

```ts
        setSystem({
          app_name: r.data.data.app_name || DEFAULT_APP_NAME,
          base_url: r.data.data.base_url ?? '',
          default_check_interval: r.data.data.default_check_interval,
          check_retention_days: r.data.data.check_retention_days,
          sentinel_external_url: r.data.data.sentinel_external_url ?? '',
          sentinel_internal_url: r.data.data.sentinel_internal_url ?? '',
          report_timezone: r.data.data.report_timezone || 'UTC',
        })
```

to:

```ts
        setSystem({
          app_name: r.data.data.app_name || DEFAULT_APP_NAME,
          base_url: r.data.data.base_url ?? '',
          default_check_interval: r.data.data.default_check_interval,
          check_retention_days: r.data.data.check_retention_days,
          sentinel_external_url: r.data.data.sentinel_external_url ?? '',
          sentinel_internal_url: r.data.data.sentinel_internal_url ?? '',
          report_timezone: r.data.data.report_timezone || 'UTC',
          default_sla_target: r.data.data.default_sla_target,
        })
```

Change:

```ts
      await api.patch('/settings/system', {
        app_name: system.app_name.trim(),
        base_url: system.base_url.trim(),
        default_check_interval: system.default_check_interval,
        check_retention_days: system.check_retention_days,
        sentinel_external_url: system.sentinel_external_url.trim(),
        sentinel_internal_url: system.sentinel_internal_url.trim(),
        report_timezone: system.report_timezone,
      })
```

to:

```ts
      await api.patch('/settings/system', {
        app_name: system.app_name.trim(),
        base_url: system.base_url.trim(),
        default_check_interval: system.default_check_interval,
        check_retention_days: system.check_retention_days,
        sentinel_external_url: system.sentinel_external_url.trim(),
        sentinel_internal_url: system.sentinel_internal_url.trim(),
        report_timezone: system.report_timezone,
        default_sla_target: system.default_sla_target,
      })
```

- [ ] **Step 4: Add the validity check**

Change:

```ts
  const now = new Date()
  const intervalValid =
    Number.isFinite(system?.default_check_interval) &&
    (system?.default_check_interval ?? 0) >= MIN_INTERVAL &&
    (system?.default_check_interval ?? 0) <= MAX_INTERVAL
```

to:

```ts
  const now = new Date()
  const intervalValid =
    Number.isFinite(system?.default_check_interval) &&
    (system?.default_check_interval ?? 0) >= MIN_INTERVAL &&
    (system?.default_check_interval ?? 0) <= MAX_INTERVAL
  const slaValid =
    Number.isFinite(system?.default_sla_target) &&
    (system?.default_sla_target ?? -1) >= MIN_SLA_TARGET &&
    (system?.default_sla_target ?? -1) <= MAX_SLA_TARGET
```

- [ ] **Step 5: Add the SettingsCard**

Change:

```tsx
              <SettingsCard
                title="Default Check Interval"
                description="How often a newly created monitor checks, in seconds. Existing monitors keep their own interval."
              >
                <div className="flex items-center gap-2">
                  <input
                    type="number"
                    min={MIN_INTERVAL}
                    max={MAX_INTERVAL}
                    value={system.default_check_interval}
                    onChange={(e) =>
                      setSystem({ ...system, default_check_interval: Number(e.target.value) })
                    }
                    aria-label="Default check interval in seconds"
                    className="w-32"
                  />
                  <span className="text-sm text-slate-400">seconds</span>
                </div>
                {!intervalValid && (
                  <p className="text-xs text-red-400">
                    Must be between {MIN_INTERVAL} and {MAX_INTERVAL} seconds.
                  </p>
                )}
              </SettingsCard>


              <SettingsCard
                title="Sentinel URLs"
```

to:

```tsx
              <SettingsCard
                title="Default Check Interval"
                description="How often a newly created monitor checks, in seconds. Existing monitors keep their own interval."
              >
                <div className="flex items-center gap-2">
                  <input
                    type="number"
                    min={MIN_INTERVAL}
                    max={MAX_INTERVAL}
                    value={system.default_check_interval}
                    onChange={(e) =>
                      setSystem({ ...system, default_check_interval: Number(e.target.value) })
                    }
                    aria-label="Default check interval in seconds"
                    className="w-32"
                  />
                  <span className="text-sm text-slate-400">seconds</span>
                </div>
                {!intervalValid && (
                  <p className="text-xs text-red-400">
                    Must be between {MIN_INTERVAL} and {MAX_INTERVAL} seconds.
                  </p>
                )}
              </SettingsCard>

              <SettingsCard
                title="Default SLA Target"
                description="The uptime percentage a monitor is held to when it has no target of its own. Reports compare against this unless a monitor overrides it."
              >
                <div className="flex items-center gap-2">
                  <input
                    type="number"
                    min={MIN_SLA_TARGET}
                    max={MAX_SLA_TARGET}
                    step={0.1}
                    value={system.default_sla_target}
                    onChange={(e) =>
                      setSystem({ ...system, default_sla_target: Number(e.target.value) })
                    }
                    aria-label="Default SLA target percentage"
                    className="w-32"
                  />
                  <span className="text-sm text-slate-400">%</span>
                </div>
                {!slaValid && (
                  <p className="text-xs text-red-400">
                    Must be between {MIN_SLA_TARGET} and {MAX_SLA_TARGET}.
                  </p>
                )}
              </SettingsCard>

              <SettingsCard
                title="Sentinel URLs"
```

- [ ] **Step 6: Include slaValid in the save button's disabled condition**

Change:

```tsx
                  disabled={systemSaving || !nameValid || !intervalValid || !checkRetentionValid || !urlsValid}
```

to:

```tsx
                  disabled={systemSaving || !nameValid || !intervalValid || !checkRetentionValid || !urlsValid || !slaValid}
```

- [ ] **Step 7: Verify the frontend type-checks and builds**

Run: `cd frontend && npx tsc --noEmit && npm run build`
Expected: no errors.

- [ ] **Step 8: Manually verify in the browser**

Open Settings → System as an admin. Confirm a "Default SLA Target" card appears right after "Default Check Interval", pre-filled with the current value (99.9 on a fresh instance); typing 150 shows the red bounds message and disables Save; typing 99.5 and saving persists (reload the page and confirm it stuck).

- [ ] **Step 9: Commit**

```bash
git add frontend/src/pages/Settings.tsx
git commit -m "$(cat <<'EOF'
feat(settings): add a Default SLA Target card to System settings

Loads, edits, validates (0-100), and saves default_sla_target
alongside the other system-wide settings.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 14: Frontend Monitor form — SLA Target field

**Files:**
- Modify: `frontend/src/types/api.ts`
- Modify: `frontend/src/context/AppConfigContext.tsx`
- Modify: `frontend/src/components/CreateMonitorModal.tsx`
- Modify: `frontend/src/components/MonitorForm.tsx`

**Interfaces:**
- Consumes: `Monitor.sla_target`/`MonitorInput.sla_target` (this task adds them), the backend `0`-means-clear convention from Task 4, `GET /auth/status`'s `default_sla_target` field (Task 5).
- Produces: `useAppConfig().defaultSLATarget: number` — the same pattern `defaultCheckInterval` already establishes on this context, for the same reason: a value the monitor-create form needs that any authenticated user may reach, not only an admin.

Both components always send `sla_target` (never omit it), using `0` for "leave blank" — the same sentinel Task 4's `applyMonitorUpdates`/`CreateMonitorHandler` normalize to `nil`. This makes editing a monitor down to a blank field actually clear a previously-set override, not just leave it untouched (a bare `*float64` on the wire cannot otherwise distinguish "omitted" from "explicitly cleared").

- [ ] **Step 0: Add defaultSLATarget to AppConfigContext**

The spec's helper text ("Leave blank to use the system default (currently
X%)") needs the real current default, and `CreateMonitorModal.tsx`/
`MonitorForm.tsx` already get `defaultCheckInterval` from this same
context rather than fetching `/settings` themselves (admin-only, while any
authenticated user may create a monitor) — `default_sla_target` follows
the identical path, now that Task 5 put it on `/auth/status` too.

In `frontend/src/context/AppConfigContext.tsx`, change:

```ts
export const DEFAULT_APP_NAME = 'Sentinel'
/** Mirrors models.DefaultMonitorCheckInterval on the backend. */
export const DEFAULT_CHECK_INTERVAL = 60

interface AppConfig {
  /** The instance's display name, as set in Settings → System. */
  appName: string
  /** Whether self-registration is open. */
  registrationEnabled: boolean
  /** True when no accounts exist yet, so the first admin can still be created. */
  setupRequired: boolean
  /** The interval, in seconds, a newly created monitor starts with. */
  defaultCheckInterval: number
  /** The IANA zone reports are rendered in, and that the UI displays times in.
   *  Undefined until the config loads, when the browser's zone is used. */
  reportTimezone: string | undefined
  /** False until the first read completes. Callers that would otherwise flash
   *  a wrong answer — "registration disabled" before we know — wait on this. */
  loaded: boolean
  /** Re-reads the config — call after an admin changes it. */
  refresh: () => Promise<void>
}

const AppConfigContext = createContext<AppConfig>({
  appName: DEFAULT_APP_NAME,
  registrationEnabled: false,
  setupRequired: false,
  reportTimezone: undefined,
  defaultCheckInterval: DEFAULT_CHECK_INTERVAL,
  loaded: false,
  refresh: async () => {},
})
```

to:

```ts
export const DEFAULT_APP_NAME = 'Sentinel'
/** Mirrors models.DefaultMonitorCheckInterval on the backend. */
export const DEFAULT_CHECK_INTERVAL = 60
/** Mirrors models.DefaultSLATargetPercent on the backend. */
export const DEFAULT_SLA_TARGET = 99.9

interface AppConfig {
  /** The instance's display name, as set in Settings → System. */
  appName: string
  /** Whether self-registration is open. */
  registrationEnabled: boolean
  /** True when no accounts exist yet, so the first admin can still be created. */
  setupRequired: boolean
  /** The interval, in seconds, a newly created monitor starts with. */
  defaultCheckInterval: number
  /** The uptime percentage a monitor is held to when it has no override. */
  defaultSLATarget: number
  /** The IANA zone reports are rendered in, and that the UI displays times in.
   *  Undefined until the config loads, when the browser's zone is used. */
  reportTimezone: string | undefined
  /** False until the first read completes. Callers that would otherwise flash
   *  a wrong answer — "registration disabled" before we know — wait on this. */
  loaded: boolean
  /** Re-reads the config — call after an admin changes it. */
  refresh: () => Promise<void>
}

const AppConfigContext = createContext<AppConfig>({
  appName: DEFAULT_APP_NAME,
  registrationEnabled: false,
  setupRequired: false,
  reportTimezone: undefined,
  defaultCheckInterval: DEFAULT_CHECK_INTERVAL,
  defaultSLATarget: DEFAULT_SLA_TARGET,
  loaded: false,
  refresh: async () => {},
})
```

Then change:

```ts
  const [defaultCheckInterval, setDefaultCheckInterval] = useState(DEFAULT_CHECK_INTERVAL)
  const [reportTimezone, setReportTimezone] = useState<string | undefined>(undefined)
```

to:

```ts
  const [defaultCheckInterval, setDefaultCheckInterval] = useState(DEFAULT_CHECK_INTERVAL)
  const [defaultSLATarget, setDefaultSLATarget] = useState(DEFAULT_SLA_TARGET)
  const [reportTimezone, setReportTimezone] = useState<string | undefined>(undefined)
```

Then change:

```ts
      setDefaultCheckInterval(
        Number.isFinite(data.default_check_interval) && data.default_check_interval > 0
          ? data.default_check_interval
          : DEFAULT_CHECK_INTERVAL
      )
      // Applied to the formatters immediately, so every timestamp on screen
      // is in the same zone the server writes into reports.
      const tz = typeof data.report_timezone === 'string' ? data.report_timezone : undefined
      setReportTimezone(tz)
      setDisplayTimezone(tz)
    } catch {
      // A failed probe leaves the defaults in place: the app still renders,
      // just under its stock name with sign-up closed.
      setAppName(DEFAULT_APP_NAME)
      setRegistrationEnabled(false)
      setSetupRequired(false)
      setDefaultCheckInterval(DEFAULT_CHECK_INTERVAL)
    } finally {
```

to:

```ts
      setDefaultCheckInterval(
        Number.isFinite(data.default_check_interval) && data.default_check_interval > 0
          ? data.default_check_interval
          : DEFAULT_CHECK_INTERVAL
      )
      setDefaultSLATarget(
        Number.isFinite(data.default_sla_target) && data.default_sla_target > 0
          ? data.default_sla_target
          : DEFAULT_SLA_TARGET
      )
      // Applied to the formatters immediately, so every timestamp on screen
      // is in the same zone the server writes into reports.
      const tz = typeof data.report_timezone === 'string' ? data.report_timezone : undefined
      setReportTimezone(tz)
      setDisplayTimezone(tz)
    } catch {
      // A failed probe leaves the defaults in place: the app still renders,
      // just under its stock name with sign-up closed.
      setAppName(DEFAULT_APP_NAME)
      setRegistrationEnabled(false)
      setSetupRequired(false)
      setDefaultCheckInterval(DEFAULT_CHECK_INTERVAL)
      setDefaultSLATarget(DEFAULT_SLA_TARGET)
    } finally {
```

Finally, change the provider's value object and the two remaining defaults:

```ts
    <AppConfigContext.Provider value={{ appName, registrationEnabled, setupRequired, defaultCheckInterval, reportTimezone, loaded, refresh }}>
```

to:

```ts
    <AppConfigContext.Provider value={{ appName, registrationEnabled, setupRequired, defaultCheckInterval, defaultSLATarget, reportTimezone, loaded, refresh }}>
```

- [ ] **Step 0b: Verify it compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors from `AppConfigContext.tsx`. (Errors from `CreateMonitorModal.tsx`/`MonitorForm.tsx` about missing `sla_target` fields are expected until the remaining steps of this task land.)

- [ ] **Step 1: Add sla_target to the Monitor and MonitorInput types**

In `frontend/src/types/api.ts`, change:

```ts
export interface Monitor {
  id: string
  name: string
  description: string
  type: MonitorType
  url: string
  method: string
  headers: Record<string, string> | null
  body: string
  interval_seconds: number
  timeout_seconds: number
  retries: number
  /** Consecutive failed checks required before an incident opens. */
  failure_threshold: number
  current_status: MonitorStatus
```

to:

```ts
export interface Monitor {
  id: string
  name: string
  description: string
  type: MonitorType
  url: string
  method: string
  headers: Record<string, string> | null
  body: string
  interval_seconds: number
  timeout_seconds: number
  retries: number
  /** Consecutive failed checks required before an incident opens. */
  failure_threshold: number
  /** Uptime percentage this monitor is held to. Null uses the system default. */
  sla_target?: number | null
  current_status: MonitorStatus
```

Then change:

```ts
export interface MonitorInput {
  name: string
  description?: string
  type: MonitorType
  url: string
  method?: string
  headers?: Record<string, string>
  body?: string
  interval_seconds: number
  timeout_seconds: number
  retries?: number
  failure_threshold?: number
  enabled?: boolean
  tags?: string[]
  notify_channels?: string[] | null
  // Verify the TLS certificate on HTTPS checks. Omitted means verify, which is
  // also the column default — never send undefined meaning "off".
  ssl_verify?: boolean
}
```

to:

```ts
export interface MonitorInput {
  name: string
  description?: string
  type: MonitorType
  url: string
  method?: string
  headers?: Record<string, string>
  body?: string
  interval_seconds: number
  timeout_seconds: number
  retries?: number
  failure_threshold?: number
  enabled?: boolean
  tags?: string[]
  notify_channels?: string[] | null
  // Verify the TLS certificate on HTTPS checks. Omitted means verify, which is
  // also the column default — never send undefined meaning "off".
  ssl_verify?: boolean
  /** Uptime percentage this monitor is held to. Omit or 0 uses the system default. */
  sla_target?: number
}
```

- [ ] **Step 2: Add the field to CreateMonitorModal's FormState**

In `frontend/src/components/CreateMonitorModal.tsx`, change:

```ts
interface FormState {
  name: string
  type: TypeKey
  description: string
  target: string
  /** A preset in seconds, or 'custom' while the free-text field is in use. */
  interval: number | 'custom'
  customInterval: string
  timeout: number
  retryAttempts: number
  /** Ids of the channels this monitor alerts on. Empty means it alerts nowhere. */
  enableNotifications: boolean
  selectedNotifications: string[]
  enableSSLVerify: boolean
}

interface Errors {
  name?: string
  target?: string
  interval?: string
  timeout?: string
}
```

to:

```ts
interface FormState {
  name: string
  type: TypeKey
  description: string
  target: string
  /** A preset in seconds, or 'custom' while the free-text field is in use. */
  interval: number | 'custom'
  customInterval: string
  timeout: number
  retryAttempts: number
  /** Blank means no override; the instance default applies. */
  slaTarget: string
  /** Ids of the channels this monitor alerts on. Empty means it alerts nowhere. */
  enableNotifications: boolean
  selectedNotifications: string[]
  enableSSLVerify: boolean
}

interface Errors {
  name?: string
  target?: string
  interval?: string
  timeout?: string
  slaTarget?: string
}
```

- [ ] **Step 3: Seed the blank form and validate**

Change:

```ts
  const blank = useMemo<FormState>(
    () => ({
      name: '',
      type: 'http',
      description: '',
      target: '',
      // Starts at the instance default from Settings → System, so this dialog
      // agrees with the other create paths.
      interval: defaultCheckInterval,
      customInterval: '',
      timeout: 10,
      retryAttempts: 3,
      // Notifications are opt-in: a monitor is created silent unless the
      // switch is turned on and channels are chosen.
      enableNotifications: false,
      selectedNotifications: [],
      // Off by default. It is an opt-in check on the certificate, not on
      // whether the service is up, and turning it on for a host with a
      // self-signed or internal-CA certificate makes the monitor fail for a
      // reason unrelated to the thing being monitored.
      enableSSLVerify: false,
    }),
    [defaultCheckInterval]
  )
```

to:

```ts
  const blank = useMemo<FormState>(
    () => ({
      name: '',
      type: 'http',
      description: '',
      target: '',
      // Starts at the instance default from Settings → System, so this dialog
      // agrees with the other create paths.
      interval: defaultCheckInterval,
      customInterval: '',
      timeout: 10,
      retryAttempts: 3,
      slaTarget: '',
      // Notifications are opt-in: a monitor is created silent unless the
      // switch is turned on and channels are chosen.
      enableNotifications: false,
      selectedNotifications: [],
      // Off by default. It is an opt-in check on the certificate, not on
      // whether the service is up, and turning it on for a host with a
      // self-signed or internal-CA certificate makes the monitor fail for a
      // reason unrelated to the thing being monitored.
      enableSSLVerify: false,
    }),
    [defaultCheckInterval]
  )
```

Change the end of `validate()`:

```ts
    if (f.timeout < MIN_TIMEOUT || f.timeout > MAX_TIMEOUT) {
      e.timeout = `Must be between ${MIN_TIMEOUT} and ${MAX_TIMEOUT} seconds`
    } else if (interval !== null && f.timeout >= interval) {
      // The backend rejects this outright; catching it here explains why.
      e.timeout = 'Must be less than the check interval'
    }
    return e
  }
```

to:

```ts
    if (f.timeout < MIN_TIMEOUT || f.timeout > MAX_TIMEOUT) {
      e.timeout = `Must be between ${MIN_TIMEOUT} and ${MAX_TIMEOUT} seconds`
    } else if (interval !== null && f.timeout >= interval) {
      // The backend rejects this outright; catching it here explains why.
      e.timeout = 'Must be less than the check interval'
    }

    const slaRaw = f.slaTarget.trim()
    if (slaRaw) {
      const n = Number(slaRaw)
      if (!Number.isFinite(n) || n <= 0 || n > 100) {
        e.slaTarget = 'Must be a number greater than 0 and at most 100'
      }
    }
    return e
  }
```

- [ ] **Step 4: Send it on submit**

Change:

```ts
      const created = await create({
        name: form.name.trim(),
        description: form.description.trim(),
        type: form.type as MonitorType,
        url: form.target.trim(),
        interval_seconds: interval,
        timeout_seconds: form.timeout,
        retries: form.retryAttempts,
        // Sent only for HTTP, the one type it affects. Other types are left to
        // the column default rather than storing a value the form never showed.
        ...(form.type === 'http' ? { ssl_verify: form.enableSSLVerify } : {}),
        // The switch decides: off sends an empty set, which the API reads as
        // "alert nobody" rather than as "not specified".
        notify_channels: form.enableNotifications ? form.selectedNotifications : [],
      })
```

to:

```ts
      const created = await create({
        name: form.name.trim(),
        description: form.description.trim(),
        type: form.type as MonitorType,
        url: form.target.trim(),
        interval_seconds: interval,
        timeout_seconds: form.timeout,
        retries: form.retryAttempts,
        // Sent only for HTTP, the one type it affects. Other types are left to
        // the column default rather than storing a value the form never showed.
        ...(form.type === 'http' ? { ssl_verify: form.enableSSLVerify } : {}),
        // 0 is the sentinel for "no override, use the system default" - always
        // sent rather than omitted, matching the convention the backend uses
        // for editing (a bare pointer field cannot otherwise distinguish
        // "omitted" from "explicitly cleared").
        sla_target: form.slaTarget.trim() ? Number(form.slaTarget.trim()) : 0,
        // The switch decides: off sends an empty set, which the API reads as
        // "alert nobody" rather than as "not specified".
        notify_channels: form.enableNotifications ? form.selectedNotifications : [],
      })
```

- [ ] **Step 5: Add the field to the JSX**

First, pull `defaultSLATarget` alongside the `defaultCheckInterval` this
component already reads from the same context. Change:

```ts
  const { defaultCheckInterval } = useAppConfig()
```

to:

```ts
  const { defaultCheckInterval, defaultSLATarget } = useAppConfig()
```

Then change:

```tsx
              <div>
                <label htmlFor="cm-timeout" className="mb-1 block text-sm font-medium text-white">
                  Timeout (seconds)
                </label>
                <input
                  id="cm-timeout"
                  type="number"
                  min={MIN_TIMEOUT}
                  max={MAX_TIMEOUT}
                  value={form.timeout}
                  onChange={(e) => set('timeout', Number(e.target.value))}
                  aria-invalid={!!errors.timeout}
                  className={`${field} ${errors.timeout ? 'border-red-500/60' : ''}`}
                />
                <p className={`mt-1 text-xs ${errors.timeout ? 'text-red-400' : 'text-slate-500'}`}>
                  {errors.timeout ?? 'Maximum time to wait for a response'}
                </p>
              </div>

              {/* Only rendered for HTTP. It means nothing for DNS, PING or TCP,
```

to:

```tsx
              <div>
                <label htmlFor="cm-timeout" className="mb-1 block text-sm font-medium text-white">
                  Timeout (seconds)
                </label>
                <input
                  id="cm-timeout"
                  type="number"
                  min={MIN_TIMEOUT}
                  max={MAX_TIMEOUT}
                  value={form.timeout}
                  onChange={(e) => set('timeout', Number(e.target.value))}
                  aria-invalid={!!errors.timeout}
                  className={`${field} ${errors.timeout ? 'border-red-500/60' : ''}`}
                />
                <p className={`mt-1 text-xs ${errors.timeout ? 'text-red-400' : 'text-slate-500'}`}>
                  {errors.timeout ?? 'Maximum time to wait for a response'}
                </p>
              </div>

              <div>
                <label htmlFor="cm-sla" className="mb-1 block text-sm font-medium text-white">
                  SLA Target % (Optional)
                </label>
                <input
                  id="cm-sla"
                  type="text"
                  inputMode="decimal"
                  value={form.slaTarget}
                  onChange={(e) => set('slaTarget', e.target.value)}
                  placeholder="e.g., 99.9"
                  aria-invalid={!!errors.slaTarget}
                  className={`${field} ${errors.slaTarget ? 'border-red-500/60' : ''}`}
                />
                <p className={`mt-1 text-xs ${errors.slaTarget ? 'text-red-400' : 'text-slate-500'}`}>
                  {errors.slaTarget ?? `Leave blank to use the system default (currently ${defaultSLATarget}%)`}
                </p>
              </div>

              {/* Only rendered for HTTP. It means nothing for DNS, PING or TCP,
```

- [ ] **Step 6: Update MonitorForm.tsx's values, seed, and validation**

In `frontend/src/components/MonitorForm.tsx`, change:

```ts
export interface MonitorFormValues {
  name: string
  type: MonitorType
  url: string
  method: string
  headers: string
  body: string
  interval_seconds: number
  timeout_seconds: number
  retries: number
  failure_threshold: number
  tags: string
  /** null = all channels, [] = none, [...] = only those. */
  notify_channels: string[] | null
}

export const emptyMonitorForm: MonitorFormValues = {
  name: '',
  type: 'http',
  url: '',
  method: 'GET',
  headers: '',
  body: '',
  interval_seconds: 60,
  timeout_seconds: 10,
  retries: 3,
  failure_threshold: 2,
  tags: '',
  notify_channels: null,
}

/** Build form values from an existing monitor (for editing). */
export function monitorToForm(m: Monitor): MonitorFormValues {
  return {
    name: m.name,
    type: m.type,
    url: m.url,
    method: m.method || 'GET',
    headers: m.headers ? JSON.stringify(m.headers, null, 2) : '',
    body: m.body || '',
    interval_seconds: m.interval_seconds,
    timeout_seconds: m.timeout_seconds,
    retries: m.retries,
    failure_threshold: m.failure_threshold ?? 2,
    tags: (m.tags ?? []).join(', '),
    notify_channels: m.notify_channels ?? null,
  }
}
```

to:

```ts
export interface MonitorFormValues {
  name: string
  type: MonitorType
  url: string
  method: string
  headers: string
  body: string
  interval_seconds: number
  timeout_seconds: number
  retries: number
  failure_threshold: number
  tags: string
  /** Blank means no override; the instance default applies. */
  sla_target: string
  /** null = all channels, [] = none, [...] = only those. */
  notify_channels: string[] | null
}

export const emptyMonitorForm: MonitorFormValues = {
  name: '',
  type: 'http',
  url: '',
  method: 'GET',
  headers: '',
  body: '',
  interval_seconds: 60,
  timeout_seconds: 10,
  retries: 3,
  failure_threshold: 2,
  tags: '',
  sla_target: '',
  notify_channels: null,
}

/** Build form values from an existing monitor (for editing). */
export function monitorToForm(m: Monitor): MonitorFormValues {
  return {
    name: m.name,
    type: m.type,
    url: m.url,
    method: m.method || 'GET',
    headers: m.headers ? JSON.stringify(m.headers, null, 2) : '',
    body: m.body || '',
    interval_seconds: m.interval_seconds,
    timeout_seconds: m.timeout_seconds,
    retries: m.retries,
    failure_threshold: m.failure_threshold ?? 2,
    tags: (m.tags ?? []).join(', '),
    sla_target: m.sla_target != null ? String(m.sla_target) : '',
    notify_channels: m.notify_channels ?? null,
  }
}
```

Change the end of `validateMonitorForm`:

```ts
  if (v.type === 'http' && v.headers.trim()) {
    try {
      const parsed = JSON.parse(v.headers)
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
        e.headers = 'Headers must be a JSON object'
    } catch {
      e.headers = 'Headers must be valid JSON'
    }
  }
  return e
}
```

to:

```ts
  if (v.type === 'http' && v.headers.trim()) {
    try {
      const parsed = JSON.parse(v.headers)
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
        e.headers = 'Headers must be a JSON object'
    } catch {
      e.headers = 'Headers must be valid JSON'
    }
  }
  if (v.sla_target.trim()) {
    const n = Number(v.sla_target.trim())
    if (!Number.isFinite(n) || n <= 0 || n > 100) {
      e.sla_target = 'Must be a number greater than 0 and at most 100'
    }
  }
  return e
}
```

Change `monitorFormToInput`:

```ts
export function monitorFormToInput(v: MonitorFormValues): MonitorInput {
  const input: MonitorInput = {
    name: v.name.trim(),
    type: v.type,
    url: v.url.trim(),
    interval_seconds: v.interval_seconds,
    timeout_seconds: v.timeout_seconds,
    retries: v.retries,
    failure_threshold: v.failure_threshold,
    enabled: true,
  }
  if (v.type === 'http') {
    input.method = v.method
    if (v.body.trim()) input.body = v.body
    if (v.headers.trim()) input.headers = JSON.parse(v.headers) as Record<string, string>
  }
  const tags = v.tags.split(',').map((t) => t.trim()).filter(Boolean)
  if (tags.length) input.tags = tags
  input.notify_channels = v.notify_channels
  return input
}
```

to:

```ts
export function monitorFormToInput(v: MonitorFormValues): MonitorInput {
  const input: MonitorInput = {
    name: v.name.trim(),
    type: v.type,
    url: v.url.trim(),
    interval_seconds: v.interval_seconds,
    timeout_seconds: v.timeout_seconds,
    retries: v.retries,
    failure_threshold: v.failure_threshold,
    enabled: true,
  }
  if (v.type === 'http') {
    input.method = v.method
    if (v.body.trim()) input.body = v.body
    if (v.headers.trim()) input.headers = JSON.parse(v.headers) as Record<string, string>
  }
  const tags = v.tags.split(',').map((t) => t.trim()).filter(Boolean)
  if (tags.length) input.tags = tags
  input.notify_channels = v.notify_channels
  // 0 is the sentinel for "no override, use the system default" - always sent
  // rather than omitted, so editing a monitor down to a blank field actually
  // clears a previously-set override instead of leaving it untouched.
  input.sla_target = v.sla_target.trim() ? Number(v.sla_target.trim()) : 0
  return input
}
```

- [ ] **Step 7: Add the field to the JSX**

First, pull `defaultSLATarget` alongside the `defaultCheckInterval` this
component already reads from the same context. Change:

```ts
  const { defaultCheckInterval } = useAppConfig()
```

to:

```ts
  const { defaultCheckInterval, defaultSLATarget } = useAppConfig()
```

Then change:

```tsx
      <div className="card space-y-4 p-5">
        <Field label="Notifications" help="Where alerts for this monitor are sent">
          <NotificationChannelPicker
            value={values.notify_channels}
            onChange={(v) => set('notify_channels', v)}
          />
        </Field>
        <Field label="Tags" help="Comma-separated, e.g. prod, api">
          <input
            className={inputCls}
            value={values.tags}
            onChange={(e) => set('tags', e.target.value)}
            placeholder="prod, critical"
          />
        </Field>
      </div>
```

to:

```tsx
      <div className="card space-y-4 p-5">
        <Field label="Notifications" help="Where alerts for this monitor are sent">
          <NotificationChannelPicker
            value={values.notify_channels}
            onChange={(v) => set('notify_channels', v)}
          />
        </Field>
        <Field label="Tags" help="Comma-separated, e.g. prod, api">
          <input
            className={inputCls}
            value={values.tags}
            onChange={(e) => set('tags', e.target.value)}
            placeholder="prod, critical"
          />
        </Field>
        <Field
          label="SLA Target"
          help={`Percentage uptime this monitor is held to in reports. Leave blank to use the system default (currently ${defaultSLATarget}%).`}
          error={errors.sla_target}
        >
          <div className="flex items-center gap-2">
            <input
              type="text"
              inputMode="decimal"
              className={`${inputCls} w-32`}
              value={values.sla_target}
              onChange={(e) => set('sla_target', e.target.value)}
              placeholder="e.g. 99.9"
            />
            <span className="text-sm text-slate-400">%</span>
          </div>
        </Field>
      </div>
```

- [ ] **Step 8: Verify the frontend type-checks and builds**

Run: `cd frontend && npx tsc --noEmit && npm run build`
Expected: no errors.

- [ ] **Step 9: Manually verify in the browser**

Create a monitor via the quick "+ New" dialog with an SLA target of `99.9`; open it and confirm the value round-trips (via the full edit form at `/monitors/:id/edit` or equivalent). Edit an existing monitor with a set SLA target, clear the field, and save; reload and confirm the override is gone (the field shows blank again, meaning it now reads the system default). Try entering `150` and confirm the inline validation error appears and blocks submission in both forms.

- [ ] **Step 10: Commit**

```bash
git add frontend/src/types/api.ts frontend/src/components/CreateMonitorModal.tsx frontend/src/components/MonitorForm.tsx
git commit -m "$(cat <<'EOF'
feat(monitors): add an optional SLA Target field to both monitor forms

Both the quick create dialog and the full create/edit form gain an
optional "SLA Target %" field. Blank always sends 0 on the wire,
which the backend treats as "clear the override" - the only way a
*float64 field can distinguish "leave alone" from "explicitly reset"
on an edit.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Self-Review

**Spec coverage:**
- Two fixed report types replacing templates/sections → Tasks 1, 2, 8, 9, 10, 11, 12.
- Global default SLA target + per-monitor override → Tasks 3, 4, 5, 13, 14.
- Uptime computation unified on `measurableWindow` → Task 7.
- The graph (cumulative uptime % vs. SLA, fpdf native primitives, no new dependency) → Task 8.
- Full wipe of reports/schedules/history, no migration path → Task 1.
- Report scheduling needs no changes → confirmed unmodified throughout; no task touches `report_schedule.go`, `report_scheduler.go`, or `report_schedule_handler.go`.
- Scope and period pickers carry over unchanged → confirmed unmodified in Tasks 11, 12 (only the template/type step and display strings change).
- Dead code discovered during research (`pdf_sections.go`, `html_sections.go`, `report_insights.go` + test, `report_html_generator.go` + test, `PreviousPeriod`/`PreviousPeriodLabel`) → Tasks 6, 7, 8, 9.

**Placeholder scan:** every task's code blocks are complete, copy-paste-ready Go/TypeScript/SQL — no `TODO`, no "similar to Task N", no prose standing in for code.

**Type consistency check:**
- `services.MeasurableWindow` (Task 7) is the exact name `api.GetSummaryReportHandler` (same task) calls as `services.MeasurableWindow`.
- `services.EffectiveSLATarget` (Task 7) matches its two unit-test call sites and its use inside `calculateMonitorMetrics`/`AggregateReportData` in the same task.
- `ReportData.UptimeSeries`/`EffectiveSLA` (Task 7) are read by `drawPDFUptimeReport` (Task 8) under the same field names.
- `models.ReportTypeUptime`/`ReportTypeIncident` (Task 2) are the exact strings `RenderReportToPDF`'s switch (Task 8), `GenerateReportRequest.ReportType`'s `oneof` binding (Task 9), and the frontend's `ReportType`/`REPORT_TYPE_LABEL` (Task 10) all agree on (`"uptime"`/`"incident"`).
- `Report.ReportType` (Task 2) is what `report_generator.go` (Task 9) passes to `RenderReportToPDF` and what `buildReportResponse`/`GenerateReport` (Task 9) read and write — no lingering `TemplateID`.
- `MonitorInput.sla_target`/`Monitor.sla_target` (Task 14) match the backend's `sla_target` JSON tag on `Monitor.SLATarget` (already existed, validated in Task 4) — same key name on both sides of the wire.
- The `0`-means-clear sentinel is applied identically in three places: `CreateMonitorHandler` (Task 4, backend), `applyMonitorUpdates` (Task 4, backend), and both frontend forms (Task 14) — all three treat `0`/blank the same way.
