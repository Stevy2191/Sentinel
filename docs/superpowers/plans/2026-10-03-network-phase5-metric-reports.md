# Network Phase 5: Metric Reports Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a third fixed report type, Metrics, that turns Sentinel's network data into the same scheduled, emailed, shareable PDF as the Uptime and Incident reports, with exact 95th percentiles, totals and a comparison with the previous period.

**Architecture:** Statistics are computed at run time from the 5-minute rollup (`metrics.samples_5m`, kept forever) by two new `MetricsStore` methods. A new package `internal/netreport` resolves a network scope as the report's owner through site access, builds a `services.MetricsReportData` (rows, in/out pairs, ranking, previous period, charts), and plugs into the existing report pipeline behind a small interface on `ReportAggregatorService`. The PDF renderer gains a Metrics layout built on a line-chart helper generalised from the uptime graph. The wizard is reordered to Type → Scope → Metrics → Period, with a scope-preview endpoint that sizes the scope and lists the metrics available for it.

**Tech Stack:** Go 1.26, Gin, GORM (pgx), PostgreSQL 16 + TimescaleDB, go-pdf/fpdf; React 18 + TypeScript + Vite + Tailwind.

**Spec:** `docs/superpowers/specs/2026-10-03-network-phase5-metric-reports-design.md`

## Global Constraints

- Module path `github.com/Stevy2191/Sentinel/backend`; Go 1.26; no new Go or npm dependency (fpdf is already used).
- The migration is `backend/migrations/058_metric_reports.sql` (057 is the latest); never edit an applied migration; `col IN (...)` CHECKs add `col IS NOT NULL`.
- New report type value: `metrics`. New scope types: `ports`, `port_roles`, `devices`, `sites`. Port roles: `wan`, `uplink`, `access` (`models.ValidPortRoles`).
- Limits: at most 500 ports or devices per report after resolution (a device's instance rows count as that device); at most 10 metrics; a larger `port_roles`/`sites` scope keeps the 500 busiest and does not fail.
- Additive units are exactly `bps` and `per_min`; every other unit is averaged when rows combine. Metrics with unit `enum` cannot be reported; `bool` metrics report the share and length of time true.
- 95th = `percentile_cont(0.95)` over the period's 5-minute bucket averages (`vsum/n`).
- Coverage under 90% flags a row; running hot = 95th busy (either direction) ≥ 80%.
- Previous period: the previous calendar unit for a calendar period, otherwise the same length immediately before the start.
- Exact user-facing strings (copy verbatim):
  - `A chosen port, device or site is not available` (create and preview, for hidden and missing alike)
  - `This report is too large to build: narrow the scope or shorten the period`
  - `text metrics cannot be reported`
  - `No data for this period`
- Access: the scope is resolved as the report's owner (schedule owner for scheduled runs, requester on demand) through site access; unavailable subjects are counted in warnings, never named.
- Frontend: colours from `src/utils/colors.ts` or existing class maps (no hand-written hex, no `dark:`); component files export only components; no `window.confirm`/`alert`; guard async results against changed inputs.
- Backend checks: `go vet ./...`, `go test ./...`, and `./scripts/test-db.sh` (DB tests) from `backend/`. Frontend gate from the repo root: `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`.
- Commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. A rolling period that ends now: the newest 5-minute bucket is still filling (the rollups are real-time). Only complete buckets count, so the in-progress bucket never lowers coverage or skews the 95th. Pinned in Task 3.
2. A calendar period in the report's time zone across a DST change (November 2026 is 1 hour longer in America/Chicago: DST ends on 1 November): expected buckets come from the real duration, so coverage stays 100% for a complete month. Pinned in Task 3.
3. Ports with unknown speed have no busy-% data. They never appear under running hot, their busy columns show no data, and the ranking by traffic is unaffected. Pinned in Task 7.
4. The largest report (500 rows × 10 metrics, long and non-Latin names) renders without an fpdf panic and stays readable (names truncated, header repeated). Pinned in Task 12.
5. A device deleted after the period but before a scheduled run has its series cleaned up: its rows vanish, it is counted as unavailable, and the run never crashes. Pinned in Task 7.

## Plan-level deviations from the spec

1. The spec's "Elsewhere" bullet names `report_html_generator.go`; that file no longer exists (there is no HTML report path today), so nothing is added there.
2. `pdf_sections.go` no longer exists; the drawing code lives in `pdf_renderer.go`. New drawing goes in new files (`pdf_chart.go`, `pdf_units.go`, `pdf_metrics.go`).
3. The spec's `POST /reports/scope-size` becomes `POST /reports/scope-preview`. It returns the size and the capped flag the spec asks for, plus the metrics available for the scope and the defaults, because the Metrics step needs both and the defaults depend on backend knowledge (profiles, UPS devices).
4. The builder lives in a new package `internal/netreport` (as phase 4's `internal/dashboards`); the data types the renderer draws live in `services` so `services` never imports `netreport` (import cycle).

## Rulings settled while planning

The spec left these open; the plan decides them:

1. Profile metrics of kind `status` (fan or power-supply states) are refused like `enum`, with the same message ("text metrics cannot be reported").
2. Link speed (`if_speed_bps`) cannot be reported: "link speed cannot be reported". It is a setting, not a load.
3. A device-level metric on a `ports` or `port_roles` scope is refused at create ("<label> is a device metric: report it on devices or sites") and skipped at run time; the preview offers only port metrics for those scopes.
4. On a `sites` scope each table starts with "<site> (site total)" rows, which are never ranked, capped or listed among the busiest.
5. Scope labels read "N ports", "N devices", "WAN, Uplink ports at HQ, Annex" and "All of HQ".
6. A `bool` metric's `RowStats.Avg` is its share of time true (0-1); the PDF prints it as a percentage.
7. The job queue's general data-gathering failure reads "could not gather the data for this report" for every report type.
8. `scope-preview` has its own rate limit (60 a minute per user, burst 20), since the picker calls it about 400 ms after each scope change; generate keeps its 5 a minute.
9. The report list and detail responses gain `scope_data` (signed-in responses only) so the pages can describe a Metrics scope (Task 20).
10. Headline notes (CappedOut, Unavailable, Skipped, LowCoverage) are drawn by the PDF; they are not copied into `ReportData.Warnings`.
11. Two existing PDF bugs are fixed in Task 12: `pdfText` shared one fpdf translator buffer (concurrent reports garbled each other's text), and every report ended on a blank page holding only the footer.
12. Task 9's performance test runs only with `SENTINEL_PERF=1`; the 500 cap stands if "SeriesStats, 100 ports × 30 days" takes 3 s or less.

## File structure

Backend:
- `backend/migrations/058_metric_reports.sql` — widen the `report_type` and `scope_type` CHECKs.
- `backend/internal/models/report.go` — `ReportTypeMetrics`, scope-type constants, `ReportScope` fields, `Validate` pairing and counts.
- `backend/internal/models/report_period.go` — `PreviousPeriod`.
- `backend/internal/services/report_job_queue.go` — `ErrReportTooLarge` is permanent (not retried).
- `backend/internal/services/metrics_stats.go` — `SeriesStats`, `CombinedStats`, `CombinedSeries`.
- `backend/internal/services/report_metrics_data.go` — the `MetricsReportData` types (below) and the `NetworkReportBuilder` interface.
- `backend/internal/services/report_aggregator.go` — the Metrics branch.
- `backend/internal/netreport/` — `scope.go` (resolve and validate as the requester), `choices.go` (available metrics, defaults, `Preview`), `build.go` (`Build`), tests.
- `backend/internal/services/pdf_chart.go` — line-chart helper (uptime graph moves onto it).
- `backend/internal/services/pdf_units.go` — unit formatting.
- `backend/internal/services/pdf_metrics.go` — `drawPDFMetricsReport`.
- `backend/internal/services/report_mailer.go` — type-aware `SummaryLines`.
- `backend/internal/api/report_builder_handler.go` — binding tags, create-time validation, `scope_data` in list/detail responses.
- `backend/internal/api/report_scope_handler.go` — `POST /reports/scope-preview` and its rate limit.
- `backend/cmd/sentinel/main.go` — wiring.

Frontend:
- `frontend/src/types/reports.ts` — `metrics` type, network scope fields, preview types.
- `frontend/src/hooks/useScopePreview.ts` — debounced, guarded preview call.
- `frontend/src/hooks/useMetricsReportDraft.ts` — scope, preview, metrics and the reapply rule, shared by both builders.
- `frontend/src/utils/reportScope.ts` — `describeScope`, `comparisonLabel`, defaults handling.
- `frontend/src/components/ReportBuilderWizard.tsx`, `GenerateReportModal.tsx` — Type → Scope → Metrics → Period.
- `frontend/src/components/reports/MetricsScopePicker.tsx`, `PortPicker.tsx`, `ReportMetricsPicker.tsx`.
- `frontend/src/pages/SavedReports.tsx` / `SavedReportDetail.tsx` — labels and scope description.

Docs: `docs/superpowers/STATUS.md`, `docs/superpowers/plans/2026-10-03-network-phase5-followups.md`.

## Shared contracts (authoritative; every task uses these names)

### Models (Task 1)

```go
const ReportTypeMetrics = "metrics"

const (
	ScopeTypePorts     = "ports"
	ScopeTypePortRoles = "port_roles"
	ScopeTypeDevices   = "devices"
	ScopeTypeSites     = "sites"
)

const (
	MaxReportSubjects = 500
	MaxReportMetrics  = 10
)

// ReportScope gains (JSON names are the API contract):
PortIDs   []uuid.UUID `json:"port_ids,omitempty"`
SiteIDs   []uuid.UUID `json:"site_ids,omitempty"`
DeviceIDs []uuid.UUID `json:"device_ids,omitempty"`
Roles     []string    `json:"roles,omitempty"`
Metrics   []string    `json:"metrics,omitempty"`

// IsNetworkScope reports whether scopeType is one of the four network scopes.
func IsNetworkScope(scopeType string) bool

// PreviousPeriod returns the comparison window for [start, end) resolved in loc.
func (r *Report) PreviousPeriod(start, end time.Time, loc *time.Location) (time.Time, time.Time)
```

### Statistics (Tasks 3-4, `services/metrics_stats.go`)

```go
type StatsQuery struct {
	SeriesIDs []int64
	From, To  time.Time // [From, To); only complete 5-minute buckets inside count
}

// SeriesStat is one series' figures over a window, from metrics.samples_5m.
type SeriesStat struct {
	SeriesID  int64
	Avg       float64 // sum(vsum)/sum(n)
	Min       float64 // min(vmin)
	Peak      float64 // max(vmax)
	P95       float64 // percentile_cont(0.95) over the bucket averages
	BucketSum float64 // Σ of the 5-minute bucket averages (totals: bps → ×300/8 bytes, per_min → ×5)
	Buckets   int     // complete buckets with data
	Expected  int     // complete buckets in the window
}

func (m *MetricsStore) SeriesStats(ctx context.Context, q StatsQuery) ([]SeriesStat, error)

type CombinedQuery struct {
	SeriesIDs []int64
	From, To  time.Time
	Average   bool // false = sum per bucket (bps, per_min); true = average per bucket
}

// CombinedStats combines the series per bucket, then computes one SeriesStat (SeriesID 0).
func (m *MetricsStore) CombinedStats(ctx context.Context, q CombinedQuery) (SeriesStat, error)

// CombinedSeries returns about maxPoints chart points for the combined series,
// from samples_5m for windows up to 7 days and samples_1h beyond.
func (m *MetricsStore) CombinedSeries(ctx context.Context, q CombinedQuery, maxPoints int) ([]ChartPoint, error)
```

### Report data (Task 7 fills it, Tasks 10-13 draw it; `services/report_metrics_data.go`)

```go
// ErrReportTooLarge is returned when a statistics query hits its timeout; the job queue does not retry it.
var ErrReportTooLarge = errors.New("This report is too large to build: narrow the scope or shorten the period")

// NetworkReportBuilder builds the Metrics part of a report. netreport.Builder implements it.
type NetworkReportBuilder interface {
	Build(ctx context.Context, report *models.Report, requestedBy uuid.UUID, start, end time.Time, loc *time.Location) (*MetricsReportData, error)
}

type MetricsReportData struct {
	ScopeType   string    // the report's scope_type: what Unavailable and CappedOut count
	ScopeLabel  string    // "WAN, Uplink ports at HQ, Annex"
	PrevStart   time.Time
	PrevEnd     time.Time
	RankLabel   string    // label of the ranking metric, e.g. "Traffic"
	Tiles       []MetricsTile
	Tables      []MetricsTable // one per metric or in/out pair, in metric order
	Busiest     []string       // up to 5 row names, busiest first
	RunningHot  []HotPort
	Charts      []MetricsChart // one per table, whole scope combined
	RowCharts   []MetricsChart // up to 10 busiest rows, first table's metric
	Ports       int            // ports in the report
	Devices     int            // devices in the report
	Rows        int            // rows included (≤ 500 subjects)
	CappedOut   int            // subjects left out by the 500 cap
	Unavailable int            // subjects dropped as unavailable to the owner or deleted
	Skipped     []string       // metric keys no longer defined
	LowCoverage int            // rows under 90% coverage
	Empty       bool           // every subject unavailable: one-page PDF
	NoData      bool           // no row has any data in the period
}

type MetricsTile struct {
	Label  string   // "Traffic", "Busy", "CPU", ...
	Unit   string   // "bps", "%", ...
	Kind   string   // "traffic" | "percent" | "other"
	First  float64  // traffic: billable 95th; percent: average; other: average
	Second *float64 // traffic: total bytes; percent: 95th; other: nil
	Change *float64 // percent change of First vs the previous period; nil with New or no data
	New    bool     // no data (or zero) in the previous period
	NoData bool
}

type MetricsTable struct {
	Title    string // "Traffic", "Busy", "Errors", or the metric label
	Unit     string
	Paired   bool   // in/out columns
	Billable bool   // traffic pair: Billable 95th column
	HasTotal bool   // unit is bps or per_min
	Rows     []MetricsRow // busiest first
}

type MetricsRow struct {
	Name        string    // "core-sw1 · Gi1/0/1 (uplink to annex)" or "core-sw1 · Switch 1"
	In          *RowStats // single metric: In only
	Out         *RowStats
	Billable    *float64
	Change      *float64  // on the billable 95th (traffic), else on the average
	New         bool
	Coverage    float64   // 0-100
	LowCoverage bool
	NoData      bool
}

type RowStats struct {
	Avg, Min, Peak, P95 float64
	Total               *float64 // bytes for bps, count for per_min, nil otherwise
	TrueSeconds         *float64 // bool metrics: seconds true
}

type HotPort struct {
	Name          string
	P95In, P95Out float64 // percent
}

type MetricsChart struct {
	Title     string
	Unit      string
	Lines     []ChartLine // 1 line, or 2 for an in/out pair
	Reference *float64    // the 95th (billable for traffic)
}

type ChartLine struct {
	Label  string
	Points []ChartPoint
}

type ChartPoint struct {
	T time.Time
	V float64
}

// ReportData gains:
Network *MetricsReportData `json:"network,omitempty"`

func (s *ReportAggregatorService) SetNetworkBuilder(b NetworkReportBuilder)
```

### netreport (Tasks 5-7)

```go
package netreport

type Builder struct { /* db, metrics store, device/port services, catalog */ }

func NewBuilder(db *gorm.DB, metrics *services.MetricsStore) *Builder

// FieldError is a create/preview validation error the API returns as 400 {error: Message, field: Field}.
type FieldError struct{ Field, Message string }
func (e *FieldError) Error() string

const MsgNotAvailable = "A chosen port, device or site is not available"

// ValidateScope checks a network scope as requester: shape (counts, roles),
// every named subject visible (one FieldError for hidden and missing), and the
// metrics (known, not enum, 1..10).
func (b *Builder) ValidateScope(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) error

type MetricChoice struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Unit   string `json:"unit"`
	Source string `json:"source"` // "builtin" | "profile" | "custom"
}

type Preview struct {
	Ports    int            `json:"ports"`
	Devices  int            `json:"devices"`
	Capped   bool           `json:"capped"`
	Metrics  []MetricChoice `json:"metrics"`
	Defaults []string       `json:"defaults"`
}

// Preview sizes a scope as requester and lists its metrics; scope.Metrics is ignored.
func (b *Builder) Preview(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) (*Preview, error)

// Build implements services.NetworkReportBuilder.
func (b *Builder) Build(ctx context.Context, report *models.Report, requestedBy uuid.UUID, start, end time.Time, loc *time.Location) (*services.MetricsReportData, error)
```

### HTTP (Task 14) — what the frontend uses

- `POST /api/v1/reports/generate` — unchanged shape; accepts `report_type: "metrics"`, `scope_type` in the four network values, `scope_data` with `port_ids`, `site_ids`, `device_ids`, `roles`, `metrics`. A `netreport.FieldError` returns 400 `{ "success": false, "error": "<message>" }`.
- `POST /api/v1/reports/scope-preview` — authenticated; body `{ "scope_type": string, "scope_data": ReportScope }`; 200 `{ "success": true, "data": Preview }`; 400 `{ "error": "<message>" }`; rate limited like the other report routes.
- Existing endpoints the pickers use: sites list, devices list, and a device's ports (`GET /api/v1/devices/:id/ports`).

## Task list

Part A — data (Tasks 1-9):
1. Report type, network scopes and the previous period (migration 058, models).
2. Too-large reports are not retried (`ErrReportTooLarge`, job queue).
3. Per-series statistics from the 5-minute rollup (`SeriesStats`).
4. Combined statistics and chart series (`CombinedStats`, `CombinedSeries`).
5. Resolving and validating a network scope as the report owner (`netreport` scope).
6. Metric choices, defaults and the scope preview (`netreport` choices).
7a. Report figures: pairs, totals, change and ranking rules (pure, unit-tested).
7b. Building the Metrics report data (`netreport.Build`).
8. The Metrics branch in the report pipeline (aggregator, interface, wiring).
9. Measured performance of the statistics queries (sets the 500 cap on numbers).

Part B — rendering and API (Tasks 10-15):
10. A line-chart helper and unit formatting (the uptime graph moves onto it).
11. The Metrics PDF: headline page and charts.
12. The Metrics PDF: tables, busiest charts, empty and no-data pages.
13. A type-aware email summary line.
14. API: Metrics reports in generate, and the scope preview.
15. Simulator end to end, and the full backend suite.

Part C — frontend and docs (Tasks 16-21):
16. Types, the scope-preview hook and scope helpers.
17. The wizard order Type → Scope → Metrics → Period (both builders).
18. The network scope picker (four tabs, port picker, size line).
19. The Metrics step (ordered list, defaults, reapply rule).
20. Labels, scope descriptions and the comparison line on the report pages.
21. Docs: status and follow-ups.

---

### Task 1: Report type, network scopes and the previous period

**Files:**
- Create: `backend/migrations/058_metric_reports.sql`
- Modify: `backend/internal/models/report.go` (scope-type constants and `ValidScopeTypes`, report types, `ReportScope`, `ReportScope.Validate`, `Report.Validate`)
- Modify: `backend/internal/models/report_period.go` (new `PreviousPeriod` at the end)
- Test: `backend/internal/models/report_metrics_test.go` (new)
- Test: `backend/internal/database/report_metrics_migration_db_test.go` (new)

**Interfaces:**
- Consumes: `models.ValidPortRoles` (`models/snmp.go`), `models.ReportableMonitorTypes`, and the existing `Report.ResolvePeriod`.
- Produces:
  - `models.ReportTypeMetrics = "metrics"`.
  - `models.ScopeTypePorts`/`ScopeTypePortRoles`/`ScopeTypeDevices`/`ScopeTypeSites`.
  - `models.MaxReportSubjects = 500`, `models.MaxReportMetrics = 10`.
  - New `ReportScope` fields `PortIDs`, `SiteIDs`, `DeviceIDs`, `Roles`, `Metrics` (JSON `port_ids`, `site_ids`, `device_ids`, `roles`, `metrics`).
  - `func IsNetworkScope(scopeType string) bool`.
  - `func (s ReportScope) ValidateSubjects(scopeType string) error`.
  - `func (r *Report) PreviousPeriod(start, end time.Time, loc *time.Location) (time.Time, time.Time)`.
  - Error texts used by later tasks:
    - `report_type must be one of: uptime, incident, metrics`
    - `scope_type must be one of: ports, port_roles, devices, sites for a metrics report`
    - `scope_type must be one of: monitors, tags, groups, types for an uptime or incident report`
    - `scope_data.metrics is required for a metrics report`

- [ ] **Step 1: Write the failing unit tests**

Create `backend/internal/models/report_metrics_test.go`. The `chicago(t)` helper already exists in `report_period_test.go`, which is in the same package.

```go
package models

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newIDs(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

func TestReportScopeValidateNetwork(t *testing.T) {
	site := []uuid.UUID{uuid.New()}
	metrics := []string{"if_in_bps"}
	cases := []struct {
		name      string
		scopeType string
		scope     ReportScope
		wantErr   string // "" = valid; otherwise a part of the message
	}{
		{"ports", ScopeTypePorts, ReportScope{PortIDs: newIDs(1), Metrics: metrics}, ""},
		{"500 ports", ScopeTypePorts, ReportScope{PortIDs: newIDs(500), Metrics: metrics}, ""},
		{"501 ports", ScopeTypePorts, ReportScope{PortIDs: newIDs(501), Metrics: metrics}, "at most 500 ports"},
		{"no ports", ScopeTypePorts, ReportScope{Metrics: metrics}, "scope_data.port_ids is required"},
		{"devices", ScopeTypeDevices, ReportScope{DeviceIDs: newIDs(1), Metrics: metrics}, ""},
		{"501 devices", ScopeTypeDevices, ReportScope{DeviceIDs: newIDs(501), Metrics: metrics}, "at most 500 devices"},
		{"no devices", ScopeTypeDevices, ReportScope{Metrics: metrics}, "scope_data.device_ids is required"},
		{"port roles", ScopeTypePortRoles, ReportScope{SiteIDs: site, Roles: []string{"wan", "uplink"}, Metrics: metrics}, ""},
		{"roles without sites", ScopeTypePortRoles, ReportScope{Roles: []string{"wan"}, Metrics: metrics}, "scope_data.site_ids is required"},
		{"sites without roles", ScopeTypePortRoles, ReportScope{SiteIDs: site, Metrics: metrics}, "scope_data.roles is required"},
		{"an unknown role", ScopeTypePortRoles, ReportScope{SiteIDs: site, Roles: []string{"core"}, Metrics: metrics}, "allowed: wan, uplink, access"},
		{"sites", ScopeTypeSites, ReportScope{SiteIDs: site, Metrics: metrics}, ""},
		{"no sites", ScopeTypeSites, ReportScope{Metrics: metrics}, "scope_data.site_ids is required"},
		{"no metrics", ScopeTypeSites, ReportScope{SiteIDs: site}, "scope_data.metrics is required for a metrics report"},
		{"11 metrics", ScopeTypeSites, ReportScope{SiteIDs: site,
			Metrics: []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10", "a11"}}, "at most 10 metrics"},
		{"10 metrics", ScopeTypeSites, ReportScope{SiteIDs: site,
			Metrics: []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10"}}, ""},
		{"a metric twice", ScopeTypeSites, ReportScope{SiteIDs: site, Metrics: []string{"if_in_bps", "if_in_bps"}}, "twice"},
		{"an empty metric", ScopeTypeSites, ReportScope{SiteIDs: site, Metrics: []string{""}}, "an empty metric"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.scope.Validate(c.scopeType)
			switch {
			case c.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case c.wantErr != "" && err == nil:
				t.Errorf("accepted, want an error containing %q", c.wantErr)
			case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
				t.Errorf("error %q, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// The scope preview sizes a scope before any metric is chosen.
func TestReportScopeValidateSubjectsIgnoresMetrics(t *testing.T) {
	s := ReportScope{SiteIDs: newIDs(1)}
	if err := s.ValidateSubjects(ScopeTypeSites); err != nil {
		t.Errorf("ValidateSubjects without metrics: %v", err)
	}
	if err := s.Validate(ScopeTypeSites); err == nil {
		t.Error("Validate accepted a metrics scope with no metrics")
	}
	if err := (ReportScope{MonitorIDs: newIDs(1)}).ValidateSubjects(ScopeTypeMonitors); err == nil {
		t.Error("ValidateSubjects accepted a monitor scope type")
	}
}

func TestIsNetworkScope(t *testing.T) {
	for _, s := range []string{ScopeTypePorts, ScopeTypePortRoles, ScopeTypeDevices, ScopeTypeSites} {
		if !IsNetworkScope(s) || !ValidScopeTypes[s] {
			t.Errorf("%s: IsNetworkScope %v, ValidScopeTypes %v; want both true", s, IsNetworkScope(s), ValidScopeTypes[s])
		}
	}
	for _, s := range []string{ScopeTypeMonitors, ScopeTypeTags, ScopeTypeGroups, ScopeTypeTypes, "everything"} {
		if IsNetworkScope(s) {
			t.Errorf("%s is not a network scope", s)
		}
	}
}

// Metrics reports take only network scopes, uptime and incident reports only
// monitor scopes, and the message names what is allowed for the report type.
func TestReportValidatePairsScopeWithType(t *testing.T) {
	network := ReportScope{PortIDs: newIDs(1), Metrics: []string{"if_in_bps"}}
	cases := []struct {
		name, reportType, scopeType string
		scope                       ReportScope
		wantErr                     string
	}{
		{"metrics on ports", ReportTypeMetrics, ScopeTypePorts, network, ""},
		{"metrics on monitors", ReportTypeMetrics, ScopeTypeMonitors, ReportScope{MonitorIDs: newIDs(1)},
			"scope_type must be one of: ports, port_roles, devices, sites for a metrics report"},
		{"uptime on ports", ReportTypeUptime, ScopeTypePorts, network,
			"scope_type must be one of: monitors, tags, groups, types for an uptime or incident report"},
		{"incident on sites", ReportTypeIncident, ScopeTypeSites, ReportScope{SiteIDs: newIDs(1), Metrics: []string{"if_in_bps"}},
			"scope_type must be one of: monitors, tags, groups, types"},
		{"uptime on types", ReportTypeUptime, ScopeTypeTypes, ReportScope{Types: []string{"dns"}}, ""},
		{"unknown report type", "weekly", ScopeTypePorts, network, "report_type must be one of: uptime, incident, metrics"},
		{"metrics, scope too big", ReportTypeMetrics, ScopeTypePorts, ReportScope{PortIDs: newIDs(501), Metrics: []string{"if_in_bps"}},
			"at most 500 ports"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &Report{Name: "r", ReportType: c.reportType, ScopeType: c.scopeType, ScopeData: c.scope, TimeRangeDays: 30}
			err := r.Validate()
			switch {
			case c.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case c.wantErr != "" && err == nil:
				t.Errorf("accepted, want an error containing %q", c.wantErr)
			case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
				t.Errorf("error %q, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// The previous period is the previous calendar unit for a calendar report
// (whole, even when this one is still running), otherwise the same length
// immediately before the start.
func TestPreviousPeriod(t *testing.T) {
	loc := chicago(t)
	local := func(s string) time.Time {
		t.Helper()
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ptr := func(v time.Time) *time.Time { return &v }
	cases := []struct {
		name       string
		report     *Report
		now        string
		start, end string
	}{
		{"last month", &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitMonth, PeriodOffset: 1},
			"2026-12-02 12:00", "2026-10-01 00:00", "2026-11-01 00:00"},
		{"this month, still running", &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitMonth},
			"2026-10-03 12:00", "2026-09-01 00:00", "2026-10-01 00:00"},
		{"last quarter", &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitQuarter, PeriodOffset: 1},
			"2026-09-19 12:00", "2026-01-01 00:00", "2026-04-01 00:00"},
		{"last week", &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitWeek, PeriodOffset: 1},
			"2026-09-19 12:00", "2026-08-31 00:00", "2026-09-07 00:00"},
		{"last year", &Report{PeriodKind: PeriodCalendar, PeriodUnit: UnitYear, PeriodOffset: 1},
			"2026-09-19 12:00", "2024-01-01 00:00", "2025-01-01 00:00"},
		{"custom", &Report{PeriodKind: PeriodCustom, PeriodStart: ptr(local("2026-09-10 00:00")), PeriodEnd: ptr(local("2026-09-20 00:00"))},
			"2026-10-03 12:00", "2026-08-31 00:00", "2026-09-10 00:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end := c.report.ResolvePeriod(local(c.now), loc)
			ps, pe := c.report.PreviousPeriod(start, end, loc)
			if !ps.Equal(local(c.start)) || !pe.Equal(local(c.end)) {
				t.Errorf("previous = %s to %s, want %s to %s", ps.In(loc).Format("2006-01-02 15:04 MST"),
					pe.In(loc).Format("2006-01-02 15:04 MST"), c.start, c.end)
			}
		})
	}
}

// A rolling window that crosses the end of DST is 721 hours, and the window
// before it is 721 hours too, ending where it starts.
func TestPreviousPeriodRollingKeepsTheLength(t *testing.T) {
	loc := chicago(t)
	r := &Report{PeriodKind: PeriodRolling, TimeRangeDays: 30}
	start, end := r.ResolvePeriod(time.Date(2026, 11, 15, 18, 0, 0, 0, time.UTC), loc)
	if h := end.Sub(start).Hours(); h != 721 {
		t.Fatalf("30 days across the DST change span %v hours, want 721", h)
	}
	ps, pe := r.PreviousPeriod(start, end, loc)
	if !pe.Equal(start) || pe.Sub(ps) != end.Sub(start) {
		t.Errorf("previous = %v to %v, want the 721 hours ending at %v", ps, pe, start)
	}
	// nil is UTC.
	if ps2, pe2 := r.PreviousPeriod(start, end, nil); !ps2.Equal(ps) || !pe2.Equal(pe) {
		t.Errorf("nil location gave %v to %v, want %v to %v", ps2, pe2, ps, pe)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run (from `backend/`): `go test ./internal/models/ -run 'TestReportScopeValidateNetwork|TestReportScopeValidateSubjectsIgnoresMetrics|TestIsNetworkScope|TestReportValidatePairsScopeWithType|TestPreviousPeriod' -v`
Expected: the build fails with `unknown field PortIDs in struct literal`, `undefined: ScopeTypePorts`, and `r.PreviousPeriod undefined`.

- [ ] **Step 3: Write the migration and its failing DB test**

Create `backend/migrations/058_metric_reports.sql`. First check that 057 is still the latest: `ls backend/migrations | tail -1` must print `057_dashboards.sql`. If it does not, use the next free number and rename the file. Also change the filename in the DB test below.

```sql
-- 058_metric_reports.sql
-- Network phase 5: a third report type, metrics, which covers ports, port
-- roles at sites, devices or sites instead of monitors. Both CHECKs are
-- dropped and re-created with the new values (report_type from 043,
-- scope_type from 026). No existing row changes. Which scope types go with
-- which report type is Report.Validate's job: a CHECK tying the two columns
-- together would only repeat it.
ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_report_type_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_report_type_check
    CHECK (report_type IS NOT NULL AND report_type IN ('uptime', 'incident', 'metrics'));

ALTER TABLE reports DROP CONSTRAINT IF EXISTS reports_scope_type_check;
ALTER TABLE reports
    ADD CONSTRAINT reports_scope_type_check
    CHECK (scope_type IS NOT NULL AND scope_type IN ('monitors', 'tags', 'groups', 'types',
                                                     'ports', 'port_roles', 'devices', 'sites'));
```

Create `backend/internal/database/report_metrics_migration_db_test.go`:

```go
package database_test

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/database"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// insertReport adds a report row of the given type and scope.
func insertReport(db *gorm.DB, user uuid.UUID, reportType, scopeType, scopeData string) error {
	return db.Exec(`INSERT INTO reports (user_id, name, report_type, scope_type, scope_data, time_range_days, created_by)
		VALUES (?, ?, ?, ?, ?::jsonb, 7, ?)`, user, reportType+" report", reportType, scopeType, scopeData, user).Error
}

// 058 applies to a database that already holds uptime and incident reports,
// leaves them as they are, and from then on accepts metrics reports.
func TestDBMigration058KeepsReportsAndAcceptsMetrics(t *testing.T) {
	db := testdb.Open(t)
	// Put the reports table back as it was before 058, and forget that 058 ran.
	testdb.Exec(t, db, `ALTER TABLE reports DROP CONSTRAINT reports_report_type_check`)
	testdb.Exec(t, db, `ALTER TABLE reports ADD CONSTRAINT reports_report_type_check CHECK (report_type IN ('uptime', 'incident'))`)
	testdb.Exec(t, db, `ALTER TABLE reports DROP CONSTRAINT reports_scope_type_check`)
	testdb.Exec(t, db, `ALTER TABLE reports ADD CONSTRAINT reports_scope_type_check CHECK (scope_type IN ('monitors', 'tags', 'groups', 'types'))`)
	testdb.Exec(t, db, `DELETE FROM schema_migrations WHERE filename = '058_metric_reports.sql'`)

	user := testdb.NewUser(t, db, false)
	testdb.Must(t, insertReport(db, user, "uptime", "monitors", `{"monitor_ids":["`+uuid.NewString()+`"]}`))
	testdb.Must(t, insertReport(db, user, "incident", "types", `{"types":["dns"]}`))
	if err := insertReport(db, user, "metrics", "ports", `{"port_ids":["`+uuid.NewString()+`"],"metrics":["if_in_bps"]}`); err == nil {
		t.Fatal("a metrics report was accepted before 058: the rollback above did not take")
	}

	testdb.Must(t, database.RunMigrations(db, testdb.MigrationsDir()))

	var kept []struct {
		ReportType string
		ScopeType  string
	}
	testdb.Must(t, db.Raw(`SELECT report_type, scope_type FROM reports ORDER BY report_type`).Scan(&kept).Error)
	if len(kept) != 2 || kept[0].ReportType != "incident" || kept[0].ScopeType != "types" ||
		kept[1].ReportType != "uptime" || kept[1].ScopeType != "monitors" {
		t.Fatalf("reports after 058 = %+v, want the incident and uptime reports unchanged", kept)
	}
	for _, s := range []struct{ scopeType, data string }{
		{"ports", `{"port_ids":["` + uuid.NewString() + `"],"metrics":["if_in_bps"]}`},
		{"port_roles", `{"site_ids":["` + uuid.NewString() + `"],"roles":["wan"],"metrics":["if_in_bps"]}`},
		{"devices", `{"device_ids":["` + uuid.NewString() + `"],"metrics":["if_in_bps"]}`},
		{"sites", `{"site_ids":["` + uuid.NewString() + `"],"metrics":["if_in_bps"]}`},
	} {
		if err := insertReport(db, user, "metrics", s.scopeType, s.data); err != nil {
			t.Errorf("a metrics report scoped to %s was refused: %v", s.scopeType, err)
		}
	}
	if err := insertReport(db, user, "weekly", "monitors", `{}`); err == nil {
		t.Error("report_type 'weekly' was accepted")
	}
	if err := insertReport(db, user, "metrics", "everything", `{}`); err == nil {
		t.Error("scope_type 'everything' was accepted")
	}
}
```

- [ ] **Step 4: Implement the models**

In `backend/internal/models/report.go`, replace the `ValidScopeTypes` block, from `// ValidScopeTypes lists the accepted scope_type values.` through its closing `}`, with:

```go
// Network scope types, for metrics reports: what a report on network data
// covers.
const (
	// ScopeTypePorts covers the chosen ports (device_interfaces ids).
	ScopeTypePorts = "ports"
	// ScopeTypePortRoles covers every port with one of the chosen roles on a
	// device in the chosen sites, worked out again at every run.
	ScopeTypePortRoles = "port_roles"
	// ScopeTypeDevices covers the chosen devices.
	ScopeTypeDevices = "devices"
	// ScopeTypeSites covers the chosen sites: their totals and every port of
	// every device in them, worked out again at every run.
	ScopeTypeSites = "sites"
)

// Limits of a metrics report.
const (
	// MaxReportSubjects bounds the ports or devices one metrics report
	// covers after resolution; a larger port_roles or sites scope keeps the
	// busiest this many.
	MaxReportSubjects = 500
	// MaxReportMetrics bounds the metrics one metrics report lists.
	MaxReportMetrics = 10
)

// ValidScopeTypes lists the accepted scope_type values.
var ValidScopeTypes = map[string]bool{
	ScopeTypeMonitors:  true,
	ScopeTypeTags:      true,
	ScopeTypeGroups:    true,
	ScopeTypeTypes:     true,
	ScopeTypePorts:     true,
	ScopeTypePortRoles: true,
	ScopeTypeDevices:   true,
	ScopeTypeSites:     true,
}

// monitorScopeTypes are the scopes of uptime and incident reports.
var monitorScopeTypes = map[string]bool{
	ScopeTypeMonitors: true, ScopeTypeTags: true, ScopeTypeGroups: true, ScopeTypeTypes: true,
}

// networkScopeTypes are the scopes of metrics reports.
var networkScopeTypes = map[string]bool{
	ScopeTypePorts: true, ScopeTypePortRoles: true, ScopeTypeDevices: true, ScopeTypeSites: true,
}

// IsNetworkScope reports whether scopeType is one of the four network scopes.
func IsNetworkScope(scopeType string) bool { return networkScopeTypes[scopeType] }
```

In the report-type `const` block, after `ReportTypeIncident = "incident"`, add:

```go
	// ReportTypeMetrics renders network statistics (traffic, busy, errors,
	// UPS readings, profile and custom metrics) for ports, devices and sites,
	// compared with the period before.
	ReportTypeMetrics = "metrics"
```

and add `ReportTypeMetrics: true,` to `ValidReportTypes`.

Replace the `ReportScope` struct with the following. The comment changes too, because a network scope fills several fields:

```go
// ReportScope is the JSONB payload on reports.scope_data. A monitor scope
// fills the one field its scope_type names; a network scope fills its
// subject fields and Metrics.
//
// This is a concrete struct rather than a generic JSON container so the scope
// can be resolved without re-parsing untyped maps at every call site.
type ReportScope struct {
	MonitorIDs []uuid.UUID `json:"monitor_ids,omitempty"`
	Tags       []string    `json:"tags,omitempty"`
	GroupIDs   []uuid.UUID `json:"group_ids,omitempty"`
	// Types holds monitor check types: http, tcp, ping, dns.
	Types []string `json:"types,omitempty"`

	// PortIDs are device_interfaces ids (scope_type ports).
	PortIDs []uuid.UUID `json:"port_ids,omitempty"`
	// SiteIDs are the sites of a port_roles or sites scope.
	SiteIDs []uuid.UUID `json:"site_ids,omitempty"`
	// DeviceIDs are the devices of a devices scope.
	DeviceIDs []uuid.UUID `json:"device_ids,omitempty"`
	// Roles are port roles (models.ValidPortRoles) for port_roles.
	Roles []string `json:"roles,omitempty"`
	// Metrics are the metric keys a metrics report shows, 1 to 10, in
	// order; the first ranks the rows.
	Metrics []string `json:"metrics,omitempty"`
}
```

In `ReportScope.Validate`, insert this at the very top of the function body, before `switch scopeType {`:

```go
	if IsNetworkScope(scopeType) {
		if err := s.ValidateSubjects(scopeType); err != nil {
			return err
		}
		return s.validateMetrics()
	}
```

After the end of `ReportScope.Validate`, add:

```go
// ValidateSubjects checks the subject fields of a network scope: the ids and
// roles its type requires, within the limits. It leaves the metrics alone, so
// a scope can be sized before any metric is chosen. Whether each subject
// exists and is visible is netreport's job: it needs the database.
func (s ReportScope) ValidateSubjects(scopeType string) error {
	switch scopeType {
	case ScopeTypePorts:
		return checkSubjectCount("port_ids", len(s.PortIDs), scopeType, "ports")
	case ScopeTypeDevices:
		return checkSubjectCount("device_ids", len(s.DeviceIDs), scopeType, "devices")
	case ScopeTypePortRoles:
		if len(s.SiteIDs) == 0 {
			return errors.New(`scope_data.site_ids is required when scope_type is "port_roles"`)
		}
		if len(s.Roles) == 0 {
			return errors.New(`scope_data.roles is required when scope_type is "port_roles"`)
		}
		for _, r := range s.Roles {
			if !ValidPortRoles[r] {
				return fmt.Errorf("unknown port role in scope_data.roles: %q (allowed: wan, uplink, access)", r)
			}
		}
	case ScopeTypeSites:
		if len(s.SiteIDs) == 0 {
			return errors.New(`scope_data.site_ids is required when scope_type is "sites"`)
		}
	default:
		return errors.New("unknown network scope_type: " + scopeType)
	}
	return nil
}

// checkSubjectCount requires 1 to MaxReportSubjects ids in field.
func checkSubjectCount(field string, n int, scopeType, noun string) error {
	if n == 0 {
		return fmt.Errorf("scope_data.%s is required when scope_type is %q", field, scopeType)
	}
	if n > MaxReportSubjects {
		return fmt.Errorf("scope_data.%s can name at most %d %s", field, MaxReportSubjects, noun)
	}
	return nil
}

// validateMetrics requires 1 to MaxReportMetrics distinct, non-empty keys.
// Whether each key exists, and is one a report can show, is netreport's job.
func (s ReportScope) validateMetrics() error {
	if len(s.Metrics) == 0 {
		return errors.New("scope_data.metrics is required for a metrics report")
	}
	if len(s.Metrics) > MaxReportMetrics {
		return fmt.Errorf("scope_data.metrics can list at most %d metrics", MaxReportMetrics)
	}
	seen := make(map[string]bool, len(s.Metrics))
	for _, k := range s.Metrics {
		if k == "" {
			return errors.New("scope_data.metrics has an empty metric key")
		}
		if seen[k] {
			return fmt.Errorf("scope_data.metrics lists %s twice", k)
		}
		seen[k] = true
	}
	return nil
}
```

In `Report.Validate`, replace these two checks:

```go
	if !ValidReportTypes[r.ReportType] {
		return errors.New("report_type must be one of: uptime, incident")
	}
	if !ValidScopeTypes[r.ScopeType] {
		return errors.New("scope_type must be one of: monitors, tags, groups")
	}
```

with:

```go
	if !ValidReportTypes[r.ReportType] {
		return errors.New("report_type must be one of: uptime, incident, metrics")
	}
	// Each report type has its own scopes: a metrics report covers network
	// subjects, the others cover monitors. The message names what is allowed
	// for this report type.
	if r.ReportType == ReportTypeMetrics {
		if !networkScopeTypes[r.ScopeType] {
			return errors.New("scope_type must be one of: ports, port_roles, devices, sites for a metrics report")
		}
	} else if !monitorScopeTypes[r.ScopeType] {
		return errors.New("scope_type must be one of: monitors, tags, groups, types for an uptime or incident report")
	}
```

`report.go` already imports `errors` and `fmt`. `ValidPortRoles` is in `models/snmp.go`, which is in the same package.

At the end of `backend/internal/models/report_period.go`, add:

```go
// PreviousPeriod returns the window a report compares itself with: the
// previous calendar unit for a calendar period ("August" for "September",
// and whole even while this one is still running), otherwise the same length
// immediately before start. start and end are what ResolvePeriod returned.
// Calendar arithmetic runs in loc, so a month boundary stays at local midnight
// across a DST change. nil loc is UTC.
func (r *Report) PreviousPeriod(start, end time.Time, loc *time.Location) (time.Time, time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	start = start.In(loc)
	if r.PeriodKind == PeriodCalendar {
		switch r.PeriodUnit {
		case UnitWeek:
			return start.AddDate(0, 0, -7), start
		case UnitMonth:
			return start.AddDate(0, -1, 0), start
		case UnitQuarter:
			return start.AddDate(0, -3, 0), start
		case UnitYear:
			return start.AddDate(-1, 0, 0), start
		}
	}
	return start.Add(-end.Sub(start)), start
}
```

- [ ] **Step 5: Run the unit tests and the existing model tests**

Run: `go test ./internal/models/ -v -run 'Report|Period|NetworkScope'`
Expected: PASS. This includes the existing `TestReportScopeValidate`, `TestReportValidate` and `TestResolvePeriod_*`.

- [ ] **Step 6: Run the migration DB test**

Run: `./scripts/test-db.sh -run 'TestDBMigration058KeepsReportsAndAcceptsMetrics|TestDBMigrationsApplyAndAreIdempotent' -v`
Expected: PASS.

- [ ] **Step 7: Vet and commit**

Run: `go vet ./...`
Expected: no output.

```bash
git add backend/migrations/058_metric_reports.sql backend/internal/models/report.go backend/internal/models/report_period.go backend/internal/models/report_metrics_test.go backend/internal/database/report_metrics_migration_db_test.go
git commit -m "feat(reports): metrics report type, network scopes and the previous period

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Too-large reports are not retried

**Files:**
- Create: `backend/internal/services/report_metrics_data.go` (it starts with `ErrReportTooLarge`; Tasks 4, 7a and 8 add the rest of the contract types to it)
- Modify: `backend/internal/services/report_job_queue.go` (`classifyJobError`, new `permanentJobError`, `finishFailed`)
- Modify: `backend/internal/models/report_job.go` (the `MaxJobAttempts` comment)
- Test: `backend/internal/services/report_job_queue_test.go` (new, unit)
- Test: `backend/internal/services/report_job_queue_db_test.go` (new, DB)

**Interfaces:**
- Consumes: `models.ReportJob`, `models.MaxJobAttempts`, `models.JobFailed`, `models.JobQueued`, `models.JobRunning`.
- Produces:
  - `services.ErrReportTooLarge`, an `error` whose text is exactly `This report is too large to build: narrow the scope or shorten the period`.
  - The job queue fails a job permanently on its first attempt when `errors.Is(err, ErrReportTooLarge)`, and stores that exact text in `report_jobs.error`.
  - Any other failure keeps the existing retry behaviour. The "aggregating" category text becomes `could not gather the data for this report`, because the old wording ("monitor data") is wrong for a metrics report.

- [ ] **Step 1: Write the failing unit test**

Create `backend/internal/services/report_job_queue_test.go`:

```go
package services

import (
	"errors"
	"fmt"
	"testing"
)

// The too-large message reaches the user as written, and it is the one
// failure a retry cannot fix.
func TestClassifyJobErrorKeepsTooLarge(t *testing.T) {
	tooLarge := fmt.Errorf("aggregating report data: %w", ErrReportTooLarge)
	if got := classifyJobError(tooLarge); got != "This report is too large to build: narrow the scope or shorten the period" {
		t.Errorf("classifyJobError(too large) = %q", got)
	}
	if !permanentJobError(tooLarge) {
		t.Error("a too-large report must not be retried")
	}
	transient := errors.New("aggregating report data: connection reset by peer")
	if permanentJobError(transient) {
		t.Error("a dropped connection must be retried")
	}
	if got := classifyJobError(transient); got != "could not gather the data for this report" {
		t.Errorf("classifyJobError(transient) = %q", got)
	}
}
```

- [ ] **Step 2: Write the failing DB test**

Create `backend/internal/services/report_job_queue_db_test.go`:

```go
package services

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// runningJob inserts a report and a job for it that a worker has claimed
// attempts times.
func runningJob(t *testing.T, db *gorm.DB, attempts int) models.ReportJob {
	t.Helper()
	user := testdb.NewUser(t, db, false)
	reportID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO reports (id, user_id, name, report_type, scope_type, scope_data, time_range_days, created_by)
		VALUES (?, ?, 'r', 'uptime', 'monitors', '{"monitor_ids":[]}', 7, ?)`, reportID, user, user)
	job := models.ReportJob{ID: uuid.New(), ReportID: reportID, RequestedBy: user, Status: models.JobRunning, Attempts: attempts}
	testdb.Must(t, db.Create(&job).Error)
	return job
}

// A report too large to build fails at once with its own message; anything
// else is still put back in the queue while attempts remain.
func TestDBTooLargeReportIsNotRetried(t *testing.T) {
	db := testdb.Open(t)
	q := NewReportJobQueue(db, nil, 1)

	big := runningJob(t, db, 1)
	q.finishFailed(&big, fmt.Errorf("aggregating report data: %w", ErrReportTooLarge))
	var got models.ReportJob
	testdb.Must(t, db.First(&got, "id = ?", big.ID).Error)
	if got.Status != models.JobFailed || got.FinishedAt == nil || got.Error == nil ||
		*got.Error != "This report is too large to build: narrow the scope or shorten the period" {
		t.Errorf("too-large job = status %q, finished %v, error %v; want failed at once with the too-large message",
			got.Status, got.FinishedAt, got.Error)
	}

	blip := runningJob(t, db, 1)
	q.finishFailed(&blip, errors.New("aggregating report data: connection reset by peer"))
	testdb.Must(t, db.First(&got, "id = ?", blip.ID).Error)
	if got.Status != models.JobQueued || got.FinishedAt != nil {
		t.Errorf("transient failure = status %q, finished %v; want it queued again", got.Status, got.FinishedAt)
	}
}
```

- [ ] **Step 3: Run them and watch them fail**

Run: `go test ./internal/services/ -run TestClassifyJobErrorKeepsTooLarge -v`
Expected: the build fails with `undefined: ErrReportTooLarge` and `undefined: permanentJobError`.

- [ ] **Step 4: Implement**

Create `backend/internal/services/report_metrics_data.go`:

```go
// Package services - report_metrics_data.go holds what a Metrics report hands
// from its builder (internal/netreport) to the renderers.
package services

import "errors"

// ErrReportTooLarge is returned when a statistics query hits its timeout; the
// job queue does not retry it. Its text is shown to the user as it is.
var ErrReportTooLarge = errors.New("This report is too large to build: narrow the scope or shorten the period")
```

In `backend/internal/services/report_job_queue.go`, replace the `classifyJobError` function body's opening, from `func classifyJobError(err error) string {` through `msg := err.Error()`, with:

```go
func classifyJobError(err error) string {
	// Already user-facing and free of server detail: shown as it is.
	if errors.Is(err, ErrReportTooLarge) {
		return ErrReportTooLarge.Error()
	}
	msg := err.Error()
```

In the same function, replace `return "could not gather monitor data for this report"` with `return "could not gather the data for this report"`.

After `classifyJobError`, add:

```go
// permanentJobError reports a failure another attempt cannot fix: the same
// report over the same data hits the same statement timeout. Such a job
// fails at once instead of spending two more five-minute runs.
func permanentJobError(err error) bool {
	return errors.Is(err, ErrReportTooLarge)
}
```

In `finishFailed`, replace:

```go
	if job.Attempts < models.MaxJobAttempts {
```

with:

```go
	if job.Attempts < models.MaxJobAttempts && !permanentJobError(runErr) {
```

and in its `else` branch replace the log call with:

```go
		q.logger.Printf("[report-jobs] job %s failed permanently on attempt %d/%d: %v",
			job.ID, job.Attempts, models.MaxJobAttempts, runErr)
```

`errors` is already imported in `report_job_queue.go`.

In `backend/internal/models/report_job.go`, replace the comment above `const MaxJobAttempts = 3` with:

```go
// MaxJobAttempts bounds retries. A job that has failed this many times is left
// failed rather than cycling forever: the usual causes (a deleted monitor, an
// unwritable output directory) do not fix themselves. A failure no retry can
// fix (services.ErrReportTooLarge) fails on its first attempt.
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/services/ -run TestClassifyJobErrorKeepsTooLarge -v`
Expected: PASS.

Run: `./scripts/test-db.sh -run TestDBTooLargeReportIsNotRetried -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/services/report_metrics_data.go backend/internal/services/report_job_queue.go backend/internal/models/report_job.go backend/internal/services/report_job_queue_test.go backend/internal/services/report_job_queue_db_test.go
git commit -m "feat(reports): a report too large to build fails at once, with its own message

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Per-series statistics from the 5-minute rollup

**Files:**
- Create: `backend/internal/services/metrics_stats.go`
- Modify: `backend/internal/services/metrics_store.go` (two fields on `MetricsStore`)
- Test: `backend/internal/services/metrics_stats_test.go` (new, unit)
- Test: `backend/internal/services/metrics_stats_db_test.go` (new, DB)

**Interfaces:**
- Consumes:
  - `models.Int64Array` (`models/custom_metrics.go`). Its `Value()` renders `{1,2,3}`, which the queries bind as one `bigint[]` parameter. This avoids the 65535-parameter limit of an expanded `IN ?`.
  - `refreshRollups(t, db)` (`metrics_store_db_test.go`).
  - `Report.ResolvePeriod` and `models.PeriodCalendar`/`UnitMonth` (Task 1 and existing).
- Produces:
  - `services.StatsQuery{SeriesIDs []int64; From, To time.Time}`.
  - `services.SeriesStat{SeriesID int64; Avg, Min, Peak, P95, BucketSum float64; Buckets, Expected int}`.
  - `func (s SeriesStat) Coverage() float64`, 0-100.
  - `func (m *MetricsStore) SeriesStats(ctx context.Context, q StatsQuery) ([]SeriesStat, error)`. It returns one entry per series with data in the window, ordered by series id. Series with no data are absent.
  - For Task 4: `statsBucket`, `statsWindow(from, to, now time.Time, base time.Duration) (lo, hi time.Time, expected int)`, `(*MetricsStore).clock() time.Time`, `(*MetricsStore).withStatementTimeout(ctx, fn func(tx *gorm.DB) error) error`, and `statRow` with `stat(expected int) SeriesStat`.
  - For Tasks 4 and 9: the test helpers `statsSeries(t, db, metric) int64`, `insertSample(t, db, series, at, v)` and `fillSamples(t, db, series, from, to, every, v)`.
  - A statistics query that hits its statement timeout (2 minutes, or `m.statsTimeout` when set) returns an error for which `errors.Is(err, ErrReportTooLarge)` holds.

Note: the `services` test files define a package-level `min(a, b int) int` (`pdf_renderer_test.go`), which shadows the builtin in test builds of this package. Do not call `min` on floats in `services` code.

- [ ] **Step 1: Write the failing unit test**

Create `backend/internal/services/metrics_stats_test.go`:

```go
package services

import (
	"testing"
	"time"
)

// Only whole buckets inside the window that have ended by now count.
func TestStatsWindow(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	later := base.Add(24 * time.Hour)
	m := func(n int) time.Time { return base.Add(time.Duration(n) * time.Minute) }
	cases := []struct {
		name         string
		from, to     time.Time
		now          time.Time
		bucket       time.Duration
		lo, hi       time.Time
		wantExpected int
	}{
		{"partial buckets at both ends", m(-2), m(32), later, statsBucket, m(0), m(30), 6},
		{"a rolling window ending now", m(0), m(32), m(32), statsBucket, m(0), m(30), 6},
		{"now inside the window", m(0), m(30), m(17), statsBucket, m(0), m(15), 3},
		{"a window in the future", m(60), m(90), m(10), statsBucket, m(60), m(60), 0},
		{"hourly buckets", m(10), m(300), later, time.Hour, m(60), m(300), 4},
	}
	for _, c := range cases {
		lo, hi, n := statsWindow(c.from, c.to, c.now, c.bucket)
		if !lo.Equal(c.lo) || !hi.Equal(c.hi) || n != c.wantExpected {
			t.Errorf("%s: [%v, %v) %d buckets, want [%v, %v) %d", c.name, lo, hi, n, c.lo, c.hi, c.wantExpected)
		}
	}
}

func TestSeriesStatCoverage(t *testing.T) {
	if got := (SeriesStat{Buckets: 5, Expected: 6}).Coverage(); got < 83.33 || got > 83.34 {
		t.Errorf("5 of 6 buckets = %v%%, want 83.33", got)
	}
	if got := (SeriesStat{}).Coverage(); got != 0 {
		t.Errorf("an empty window = %v%%, want 0", got)
	}
}
```

- [ ] **Step 2: Write the failing DB tests**

Create `backend/internal/services/metrics_stats_db_test.go`:

```go
package services

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// statsSeries makes a series of metric for a device that need not exist: the
// metrics schema has no foreign keys.
func statsSeries(t *testing.T, db *gorm.DB, metric string) int64 {
	t.Helper()
	var id int64
	testdb.Must(t, db.Raw(`INSERT INTO metrics.series (device_id, metric, instance) VALUES (?, ?, '1') RETURNING id`,
		uuid.New(), metric).Scan(&id).Error)
	return id
}

// insertSample writes one raw sample.
func insertSample(t *testing.T, db *gorm.DB, series int64, at time.Time, v float64) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value) VALUES (?, ?, ?)`, at, series, v)
}

// fillSamples writes v every `every` from `from` up to, not including, `to`.
func fillSamples(t *testing.T, db *gorm.DB, series int64, from, to time.Time, every time.Duration, v float64) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT g, ?, ? FROM generate_series(?::timestamptz, ?::timestamptz - make_interval(secs => ?), make_interval(secs => ?)) g`,
		series, v, from, to, every.Seconds(), every.Seconds())
}

func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Every figure of one series, worked out by hand. Buckets (start + avg):
//
//	b0 10, 20      -> 15     b3 0, 100, 50 -> 50
//	b1 40          -> 40     b4 30         -> 30
//	b2 (no data)             b5 60, 80     -> 70
//
// Avg = (30+40+150+30+140) / 9 samples = 390/9; Min 0; Peak 100.
// 95th of [15 30 40 50 70]: position 0.95 x 4 = 3.8, so 50 + 0.8 x 20 = 66.
// BucketSum = 15+40+50+30+70 = 205; 5 of 6 buckets have data.
func TestDBSeriesStats(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	m := NewMetricsStore(db)
	t0 := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	m.now = func() time.Time { return t0.Add(2 * time.Hour) }
	id := statsSeries(t, db, MetricIfInBps)
	empty := statsSeries(t, db, MetricIfInBps)
	minute := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }
	for _, s := range []struct {
		n int
		v float64
	}{{1, 10}, {2, 20}, {6, 40}, {16, 0}, {17, 100}, {18, 50}, {21, 30}, {26, 60}, {27, 80},
		// Inside [From, To) but in buckets cut by its edges: not counted.
		{-1, 1000}, {31, 1000}} {
		insertSample(t, db, id, minute(s.n), s.v)
	}
	refreshRollups(t, db)

	got, err := m.SeriesStats(ctx, StatsQuery{SeriesIDs: []int64{id, empty}, From: minute(-2), To: minute(32)})
	testdb.Must(t, err)
	if len(got) != 1 || got[0].SeriesID != id {
		t.Fatalf("stats = %+v, want one entry, for the series with data", got)
	}
	s := got[0]
	if !closeTo(s.Avg, 390.0/9) || s.Min != 0 || s.Peak != 100 || !closeTo(s.P95, 66) || !closeTo(s.BucketSum, 205) ||
		s.Buckets != 5 || s.Expected != 6 {
		t.Errorf("stats = %+v, want avg 43.33, min 0, peak 100, p95 66, bucket sum 205, 5 of 6 buckets", s)
	}
	if none, err := m.SeriesStats(ctx, StatsQuery{From: minute(0), To: minute(30)}); err != nil || len(none) != 0 {
		t.Errorf("no series: %v, %v", none, err)
	}
}

// Review Focus 1: a rolling period that ends now. The rollups are real-time,
// so the bucket now falls in already has a row, from the samples so far. It
// must not count: not in the expected buckets (coverage stays 100%), not in
// the 95th or the peak.
func TestDBSeriesStatsLeavesOutTheBucketStillFilling(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	m := NewMetricsStore(db)
	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)
	now := base.Add(32 * time.Minute)
	m.now = func() time.Time { return now }
	id := statsSeries(t, db, MetricIfInUtilPct)
	for i := 0; i < 6; i++ {
		insertSample(t, db, id, base.Add(time.Duration(i)*statsBucket+time.Minute), 10)
	}
	insertSample(t, db, id, base.Add(30*time.Minute+30*time.Second), 1000) // the bucket still filling
	insertSample(t, db, id, base.Add(31*time.Minute), 1000)
	refreshRollups(t, db)

	got, err := m.SeriesStats(ctx, StatsQuery{SeriesIDs: []int64{id}, From: base, To: now})
	testdb.Must(t, err)
	if len(got) != 1 || got[0].Expected != 6 || got[0].Buckets != 6 || got[0].Coverage() != 100 ||
		got[0].P95 != 10 || got[0].Peak != 10 || got[0].Avg != 10 {
		t.Fatalf("stats = %+v, want 6 of 6 buckets, all 10: the filling bucket left out", got)
	}

	// Once that bucket has ended it counts.
	now = base.Add(35 * time.Minute)
	got, err = m.SeriesStats(ctx, StatsQuery{SeriesIDs: []int64{id}, From: base, To: now})
	testdb.Must(t, err)
	if len(got) != 1 || got[0].Expected != 7 || got[0].Buckets != 7 || got[0].Peak != 1000 {
		t.Errorf("stats after the bucket ended = %+v, want 7 of 7 buckets and its peak", got)
	}
}

// Review Focus 2: November 2026 in America/Chicago is 721 hours long (DST
// ends on 1 November). A complete month has 721 x 12 = 8652 buckets, all
// with data, so coverage is exactly 100%. Thirty days of 288 buckets (8640)
// would say 100.14%.
func TestDBSeriesStatsDSTMonthIsWhole(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	db := testdb.Open(t)
	ctx := context.Background()
	// The month is a fixed date; keep the retention policy away from it.
	testdb.Exec(t, db, `SELECT remove_retention_policy('metrics.samples', if_exists => true)`)
	r := &models.Report{PeriodKind: models.PeriodCalendar, PeriodUnit: models.UnitMonth, PeriodOffset: 1}
	now := time.Date(2026, 12, 2, 18, 0, 0, 0, time.UTC)
	start, end := r.ResolvePeriod(now, loc)
	m := NewMetricsStore(db)
	m.now = func() time.Time { return now }
	id := statsSeries(t, db, MetricIfInUtilPct)
	fillSamples(t, db, id, start, end, statsBucket, 42)
	refreshRollups(t, db)

	got, err := m.SeriesStats(ctx, StatsQuery{SeriesIDs: []int64{id}, From: start, To: end})
	testdb.Must(t, err)
	if len(got) != 1 || got[0].Expected != 8652 || got[0].Buckets != 8652 || got[0].Coverage() != 100 || got[0].Avg != 42 {
		t.Fatalf("November = %+v, want 8652 of 8652 buckets (100%%) at 42", got)
	}
}

// A statistics query that runs past its statement timeout is a report too
// large to build. The timeout is local to its transaction, so it never
// reaches later queries on the pool.
func TestDBStatsTimeoutIsTooLarge(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	m := NewMetricsStore(db)
	m.statsTimeout = 50 * time.Millisecond
	err := m.withStatementTimeout(ctx, func(tx *gorm.DB) error { return tx.Exec(`SELECT pg_sleep(1)`).Error })
	if !errors.Is(err, ErrReportTooLarge) {
		t.Fatalf("err = %v, want ErrReportTooLarge", err)
	}
	if err := m.withStatementTimeout(ctx, func(tx *gorm.DB) error { return tx.Exec(`SELECT 1`).Error }); err != nil {
		t.Errorf("a quick query under the timeout: %v", err)
	}
	if err := db.Exec(`SELECT pg_sleep(0.2)`).Error; err != nil {
		t.Errorf("the timeout leaked out of its transaction: %v", err)
	}
}
```

- [ ] **Step 3: Run them and watch them fail**

Run: `go test ./internal/services/ -run 'TestStatsWindow|TestSeriesStatCoverage' -v`
Expected: the build fails with `undefined: statsWindow`, `undefined: statsBucket` and `undefined: SeriesStat`.

- [ ] **Step 4: Add the two store fields**

In `backend/internal/services/metrics_store.go`, inside `type MetricsStore struct`, after the `retentionDays int` field, add:

```go

	// now is the clock the report statistics read (time.Now when nil); tests
	// pin it to put "the bucket still filling" where they need it.
	now func() time.Time
	// statsTimeout bounds each statistics query (statsStatementTimeout when 0).
	statsTimeout time.Duration
```

- [ ] **Step 5: Implement `metrics_stats.go`**

Create `backend/internal/services/metrics_stats.go`:

```go
// Package services - metrics_stats.go computes the Metrics report's figures
// from the 5-minute rollup (metrics.samples_5m, kept forever): per series,
// per combined group of series, and chart lines. Only whole 5-minute buckets
// that have ended count (see statsWindow).
package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

const (
	// statsBucket is the rollup every report statistic reads.
	statsBucket = 5 * time.Minute
	// statsStatementTimeout bounds one statistics or chart query. A report
	// makes a few dozen of them inside the job's 5-minute limit; one that
	// runs this long means the scope or the period is too big to report on.
	statsStatementTimeout = 2 * time.Minute
)

// StatsQuery selects series and a window for SeriesStats.
type StatsQuery struct {
	SeriesIDs []int64
	From, To  time.Time // [From, To); only complete 5-minute buckets inside count
}

// SeriesStat is one series' figures over a window, from metrics.samples_5m.
type SeriesStat struct {
	SeriesID  int64
	Avg       float64 // sum(vsum)/sum(n)
	Min       float64 // min(vmin)
	Peak      float64 // max(vmax)
	P95       float64 // percentile_cont(0.95) over the bucket averages
	BucketSum float64 // Σ of the 5-minute bucket averages (totals: bps → ×300/8 bytes, per_min → ×5)
	Buckets   int     // complete buckets with data
	Expected  int     // complete buckets in the window
}

// Coverage is the share of the window's complete buckets that have data,
// 0-100; 0 for an empty window.
func (s SeriesStat) Coverage() float64 {
	if s.Expected == 0 {
		return 0
	}
	return float64(s.Buckets) / float64(s.Expected) * 100
}

// statsWindow narrows [from, to) to the whole buckets of size base inside it
// that have ended by now, and counts them. The rollups are real-time
// (materialized_only = false), so the bucket now falls in already has a row
// built from the samples so far: counting it would lower coverage and put a
// partial average into the 95th. The count comes from the real duration, so
// a month with a DST change has its 23- or 25-hour day counted as it was.
func statsWindow(from, to, now time.Time, base time.Duration) (lo, hi time.Time, expected int) {
	lo = from.Truncate(base)
	if lo.Before(from) {
		lo = lo.Add(base)
	}
	if now.Before(to) {
		to = now
	}
	hi = to.Truncate(base)
	if !hi.After(lo) {
		return lo, lo, 0
	}
	return lo, hi, int(hi.Sub(lo) / base)
}

// clock is the store's idea of now: time.Now, or a fixed instant in tests.
func (m *MetricsStore) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// withStatementTimeout runs fn in a transaction whose statements may each
// run for at most the statistics timeout (SET LOCAL: it ends with the
// transaction and never reaches the pool). A statement cancelled by it is
// ErrReportTooLarge.
func (m *MetricsStore) withStatementTimeout(ctx context.Context, fn func(tx *gorm.DB) error) error {
	timeout := m.statsTimeout
	if timeout <= 0 {
		timeout = statsStatementTimeout
	}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('statement_timeout', ?, true)`,
			strconv.FormatInt(timeout.Milliseconds(), 10)).Error; err != nil {
			return fmt.Errorf("setting the statistics timeout: %w", err)
		}
		return fn(tx)
	})
	if isStatementTimeout(err) {
		return ErrReportTooLarge
	}
	return err
}

// isStatementTimeout reports Postgres' query_canceled (57014), which a
// statement timeout raises.
func isStatementTimeout(err error) bool {
	var coded interface{ SQLState() string }
	return errors.As(err, &coded) && coded.SQLState() == "57014"
}

// statRow is one row of a statistics query.
type statRow struct {
	ID        int64   `gorm:"column:id"`
	Avg       float64 `gorm:"column:avg"`
	Min       float64 `gorm:"column:min"`
	Peak      float64 `gorm:"column:peak"`
	P95       float64 `gorm:"column:p95"`
	BucketSum float64 `gorm:"column:bucket_sum"`
	Buckets   int     `gorm:"column:buckets"`
}

func (r statRow) stat(expected int) SeriesStat {
	return SeriesStat{SeriesID: r.ID, Avg: r.Avg, Min: r.Min, Peak: r.Peak, P95: r.P95,
		BucketSum: r.BucketSum, Buckets: r.Buckets, Expected: expected}
}

// SeriesStats returns each series' figures over the window's complete
// 5-minute buckets, in one query. A series with no data there is absent.
// The 95th is percentile_cont over the 5-minute averages, as carriers bill.
func (m *MetricsStore) SeriesStats(ctx context.Context, q StatsQuery) ([]SeriesStat, error) {
	lo, hi, expected := statsWindow(q.From, q.To, m.clock(), statsBucket)
	out := []SeriesStat{}
	if len(q.SeriesIDs) == 0 || expected == 0 {
		return out, nil
	}
	var rows []statRow
	err := m.withStatementTimeout(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT r.series_id AS id,
				sum(r.vsum) / sum(r.n) AS avg,
				min(r.vmin) AS min,
				max(r.vmax) AS peak,
				percentile_cont(0.95) WITHIN GROUP (ORDER BY r.vsum / r.n) AS p95,
				sum(r.vsum / r.n) AS bucket_sum,
				count(*) AS buckets
			FROM metrics.samples_5m r
			WHERE r.series_id = ANY(?::bigint[]) AND r.bucket >= ? AND r.bucket < ?
			GROUP BY r.series_id
			ORDER BY r.series_id`, models.Int64Array(q.SeriesIDs), lo, hi).Scan(&rows).Error
	})
	if err != nil {
		return nil, fmt.Errorf("reading series statistics: %w", err)
	}
	for _, r := range rows {
		out = append(out, r.stat(expected))
	}
	return out, nil
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/services/ -run 'TestStatsWindow|TestSeriesStatCoverage' -v`
Expected: PASS.

Run: `./scripts/test-db.sh -run 'TestDBSeriesStats|TestDBStatsTimeoutIsTooLarge' -v`
Expected: PASS for `TestDBSeriesStats`, `TestDBSeriesStatsLeavesOutTheBucketStillFilling`, `TestDBSeriesStatsDSTMonthIsWhole` and `TestDBStatsTimeoutIsTooLarge`. The DST test's samples fall in November 2026. When that month is still in the future, the real-time half of the rollup serves them, so the result is the same.

- [ ] **Step 7: Vet and commit**

Run: `go vet ./internal/services/`
Expected: no output.

```bash
git add backend/internal/services/metrics_stats.go backend/internal/services/metrics_store.go backend/internal/services/metrics_stats_test.go backend/internal/services/metrics_stats_db_test.go
git commit -m "feat(metrics): per-series report statistics from the 5-minute rollup

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Combined statistics and chart series

**Files:**
- Modify: `backend/internal/services/metrics_stats.go` (append `CombinedQuery`, `CombinedStats`, `GroupedStats`, `CombinedSeries`, `chartResolution`)
- Modify: `backend/internal/services/report_metrics_data.go` (add `ChartPoint`)
- Test: `backend/internal/services/metrics_stats_test.go` (add `TestChartResolution`)
- Test: `backend/internal/services/metrics_stats_db_test.go` (add `TestDBCombinedStats`, `TestDBCombinedSeries`)

**Interfaces:**
- Consumes (Task 3):
  - `statsBucket`, `statsWindow`, `(*MetricsStore).clock`, `withStatementTimeout`, `statRow.stat`.
  - The test helpers `statsSeries`, `insertSample`, `fillSamples`, `closeTo`.
  - The existing `PickResolution(from, to, rawSince time.Time) (string, time.Duration)` and `refreshRollups`.
- Produces:
  - `services.CombinedQuery{SeriesIDs []int64; From, To time.Time; Average bool}`.
  - `func (m *MetricsStore) CombinedStats(ctx context.Context, q CombinedQuery) (SeriesStat, error)`. It returns one `SeriesStat` with `SeriesID` 0. With no data, `Buckets` is 0 and `Expected` is still set.
  - `func (m *MetricsStore) GroupedStats(ctx context.Context, groups [][]int64, from, to time.Time, average bool) ([]SeriesStat, error)`. It returns one entry per group, in order, with `SeriesID` set to the group's index. An empty group or a group with no data has `Buckets` 0. A series listed twice in one group counts once.
  - `func (m *MetricsStore) CombinedSeries(ctx context.Context, q CombinedQuery, maxPoints int) ([]ChartPoint, error)`. The points are oldest first. An empty result is `[]ChartPoint{}`, not nil.
  - `services.ChartPoint{T time.Time; V float64}`.

How series combine (phase 4's rule): within each 5-minute bucket, the series' bucket averages are summed when `!Average` (bps, per_min) and averaged when `Average`. The figures are then taken over the combined series:
- `Avg` is its mean.
- `Min` and `Peak` are its lowest and highest 5-minute value. Raw peaks of different series do not line up, so a peak of a total cannot be read from them.
- `P95` is `percentile_cont(0.95)` over the combined values.
- `BucketSum` is the sum of the combined values.

- [ ] **Step 1: Write the failing unit test**

Append to `backend/internal/services/metrics_stats_test.go`:

```go
// Report charts read the 5-minute rollup up to 7 days and the hourly one
// beyond (PickResolution's split, never raw), at about maxPoints points.
func TestChartResolution(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		span      time.Duration
		maxPoints int
		table     string
		base      time.Duration
		step      time.Duration
	}{
		{2 * time.Hour, 12, "metrics.samples_5m", 5 * time.Minute, 10 * time.Minute},
		{time.Hour, 200, "metrics.samples_5m", 5 * time.Minute, 5 * time.Minute},
		{7 * 24 * time.Hour, 200, "metrics.samples_5m", 5 * time.Minute, 55 * time.Minute},        // 50.4 min, rounded up
		{7*24*time.Hour + time.Hour, 200, "metrics.samples_1h", time.Hour, time.Hour},             // 50.7 min < 1 h
		{30 * 24 * time.Hour, 200, "metrics.samples_1h", time.Hour, 4 * time.Hour},                // 3.6 h, rounded up
		{365 * 24 * time.Hour, 200, "metrics.samples_1h", time.Hour, 44 * time.Hour},              // 43.8 h, rounded up
	}
	for _, c := range cases {
		table, base, step := chartResolution(from, from.Add(c.span), c.maxPoints)
		if table != c.table || base != c.base || step != c.step {
			t.Errorf("%v at %d points: %s / %v / %v, want %s / %v / %v", c.span, c.maxPoints, table, base, step, c.table, c.base, c.step)
		}
	}
}
```

- [ ] **Step 2: Write the failing DB tests**

Append to `backend/internal/services/metrics_stats_db_test.go`:

```go
// Two series over four buckets, by hand (bucket averages):
//
//	        b0    b1        b2    b3
//	A       100   150,250   300   -
//	        100   200       300
//	B       50    -         100   40
//
// Summed: 150, 200, 400, 40. Avg 790/4 = 197.5, min 40, peak 400, bucket sum 790,
// 95th of [40 150 200 400] at 0.95 x 3 = 2.85: 200 + 0.85 x 200 = 370.
// Averaged: 75, 200, 200, 40. Avg 515/4 = 128.75, min 40, peak 200,
// bucket sum 515, 95th 200 + 0.85 x 0 = 200.
func TestDBCombinedStats(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	m := NewMetricsStore(db)
	t0 := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	m.now = func() time.Time { return t0.Add(time.Hour) }
	a, b := statsSeries(t, db, MetricIfInBps), statsSeries(t, db, MetricIfInBps)
	minute := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }
	for _, s := range []struct {
		series int64
		n      int
		v      float64
	}{{a, 1, 100}, {a, 6, 150}, {a, 7, 250}, {a, 11, 300}, {b, 1, 50}, {b, 11, 100}, {b, 16, 40}} {
		insertSample(t, db, s.series, minute(s.n), s.v)
	}
	refreshRollups(t, db)
	q := CombinedQuery{SeriesIDs: []int64{a, b}, From: t0, To: minute(20)}

	sum, err := m.CombinedStats(ctx, q)
	testdb.Must(t, err)
	if sum.SeriesID != 0 || !closeTo(sum.Avg, 197.5) || sum.Min != 40 || sum.Peak != 400 || !closeTo(sum.P95, 370) ||
		!closeTo(sum.BucketSum, 790) || sum.Buckets != 4 || sum.Expected != 4 {
		t.Errorf("summed = %+v, want avg 197.5, min 40, peak 400, p95 370, bucket sum 790, 4 of 4", sum)
	}
	q.Average = true
	avg, err := m.CombinedStats(ctx, q)
	testdb.Must(t, err)
	if !closeTo(avg.Avg, 128.75) || avg.Min != 40 || avg.Peak != 200 || !closeTo(avg.P95, 200) ||
		!closeTo(avg.BucketSum, 515) || avg.Buckets != 4 {
		t.Errorf("averaged = %+v, want avg 128.75, min 40, peak 200, p95 200, bucket sum 515", avg)
	}
	none, err := m.CombinedStats(ctx, CombinedQuery{From: t0, To: minute(20)})
	testdb.Must(t, err)
	if none.Buckets != 0 || none.Expected != 4 {
		t.Errorf("no series = %+v, want 0 of 4 buckets", none)
	}

	// Several groups in one query. [A] alone: 100, 200, 300, so avg 200, peak
	// 300, 95th 200 + 0.9 x 100 = 290. [B, B] counts B once: 50, 100, 40, so
	// avg 190/3, 95th of [40 50 100] = 50 + 0.9 x 50 = 95.
	groups, err := m.GroupedStats(ctx, [][]int64{{a}, {a, b}, {}, {b, b}}, t0, minute(20), false)
	testdb.Must(t, err)
	if len(groups) != 4 {
		t.Fatalf("got %d groups, want 4", len(groups))
	}
	g0, g1, g2, g3 := groups[0], groups[1], groups[2], groups[3]
	if g0.SeriesID != 0 || !closeTo(g0.Avg, 200) || g0.Min != 100 || g0.Peak != 300 || !closeTo(g0.P95, 290) ||
		!closeTo(g0.BucketSum, 600) || g0.Buckets != 3 || g0.Expected != 4 {
		t.Errorf("group [A] = %+v", g0)
	}
	if g1.SeriesID != 1 || !closeTo(g1.P95, 370) || !closeTo(g1.BucketSum, 790) || g1.Buckets != 4 {
		t.Errorf("group [A, B] = %+v, want the summed figures", g1)
	}
	if g2.SeriesID != 2 || g2.Buckets != 0 || g2.Expected != 4 {
		t.Errorf("empty group = %+v", g2)
	}
	if g3.SeriesID != 3 || !closeTo(g3.Avg, 190.0/3) || !closeTo(g3.P95, 95) || !closeTo(g3.BucketSum, 190) || g3.Buckets != 3 {
		t.Errorf("group [B, B] = %+v, want B counted once", g3)
	}
}

// A chart combines its series per bucket and keeps to about maxPoints
// points: 5-minute buckets for a short window, hourly ones for a long one.
func TestDBCombinedSeries(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	m := NewMetricsStore(db)
	c, d := statsSeries(t, db, MetricIfInBps), statsSeries(t, db, MetricIfInBps)
	t1 := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Hour)
	fillSamples(t, db, c, t1, t1.Add(2*time.Hour), time.Minute, 100)
	fillSamples(t, db, d, t1, t1.Add(2*time.Hour), time.Minute, 50)
	e, f := statsSeries(t, db, MetricIfInBps), statsSeries(t, db, MetricIfInBps)
	t2 := time.Now().UTC().Truncate(24 * time.Hour).Add(-12 * 24 * time.Hour)
	fillSamples(t, db, e, t2, t2.Add(10*24*time.Hour), statsBucket, 10)
	fillSamples(t, db, f, t2, t2.Add(10*24*time.Hour), statsBucket, 30)
	refreshRollups(t, db)

	for _, tc := range []struct {
		average bool
		want    float64
	}{{false, 150}, {true, 75}} {
		pts, err := m.CombinedSeries(ctx, CombinedQuery{SeriesIDs: []int64{c, d}, From: t1, To: t1.Add(2 * time.Hour),
			Average: tc.average}, 12)
		testdb.Must(t, err)
		if len(pts) != 12 || !pts[0].T.Equal(t1) || !pts[11].T.Equal(t1.Add(110*time.Minute)) {
			t.Fatalf("average=%v: %d points %v, want 12 ten-minute points from %v", tc.average, len(pts), pts, t1)
		}
		for _, p := range pts {
			if !closeTo(p.V, tc.want) {
				t.Errorf("average=%v: point %v = %v, want %v", tc.average, p.T, p.V, tc.want)
			}
		}
	}

	// Ten days: the hourly rollup, one point a day.
	pts, err := m.CombinedSeries(ctx, CombinedQuery{SeriesIDs: []int64{e, f}, From: t2, To: t2.Add(10 * 24 * time.Hour)}, 10)
	testdb.Must(t, err)
	if len(pts) != 10 || !pts[0].T.Equal(t2) || !closeTo(pts[0].V, 40) || !closeTo(pts[9].V, 40) {
		t.Errorf("ten days = %+v, want 10 daily points of 40 from %v", pts, t2)
	}

	if none, err := m.CombinedSeries(ctx, CombinedQuery{From: t1, To: t1.Add(time.Hour)}, 12); err != nil || none == nil || len(none) != 0 {
		t.Errorf("no series: %v, %v; want an empty, non-nil slice", none, err)
	}
}
```

- [ ] **Step 3: Run them and watch them fail**

Run: `go test ./internal/services/ -run TestChartResolution -v`
Expected: the build fails with `undefined: chartResolution`, `undefined: CombinedQuery` and `undefined: ChartPoint`.

- [ ] **Step 4: Add `ChartPoint`**

In `backend/internal/services/report_metrics_data.go`, replace `import "errors"` with:

```go
import (
	"errors"
	"time"
)
```

and append:

```go
// ChartPoint is one point of a report chart.
type ChartPoint struct {
	T time.Time
	V float64
}
```

- [ ] **Step 5: Implement**

Append to `backend/internal/services/metrics_stats.go`:

```go
// CombinedQuery selects series to combine into one series, per 5-minute
// bucket: added up (bps, per_min: four ports moving 10 Mbps move 40), or
// averaged (%, °C, V: four ports 50 % busy are 50 % busy).
type CombinedQuery struct {
	SeriesIDs []int64
	From, To  time.Time
	Average   bool // false = sum per bucket (bps, per_min); true = average per bucket
}

// combineFunc is the SQL aggregate that combines series in a bucket.
func combineFunc(average bool) string {
	if average {
		return "avg"
	}
	return "sum"
}

// CombinedStats combines the series per bucket, then computes one SeriesStat (SeriesID 0).
// Its figures are the combined series': Avg its mean, Min and Peak its lowest
// and highest 5-minute value (raw peaks of different series do not line up,
// so a peak of a total cannot be read from them), P95 its 95th percentile.
func (m *MetricsStore) CombinedStats(ctx context.Context, q CombinedQuery) (SeriesStat, error) {
	stats, err := m.GroupedStats(ctx, [][]int64{q.SeriesIDs}, q.From, q.To, q.Average)
	if err != nil {
		return SeriesStat{}, err
	}
	return stats[0], nil
}

// GroupedStats is CombinedStats for many groups in one query: one SeriesStat
// per group, in order, its SeriesID the group's index. A report reads every
// device or site total of a metric this way, one query per metric and
// period however many totals there are. An empty group, or one without data,
// has Buckets 0; a series listed twice in a group counts once.
func (m *MetricsStore) GroupedStats(ctx context.Context, groups [][]int64, from, to time.Time, average bool) ([]SeriesStat, error) {
	lo, hi, expected := statsWindow(from, to, m.clock(), statsBucket)
	out := make([]SeriesStat, len(groups))
	var ids, grp models.Int64Array
	for i, g := range groups {
		out[i] = SeriesStat{SeriesID: int64(i), Expected: expected}
		seen := make(map[int64]bool, len(g))
		for _, id := range g {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
				grp = append(grp, int64(i))
			}
		}
	}
	if len(ids) == 0 || expected == 0 {
		return out, nil
	}
	var rows []statRow
	err := m.withStatementTimeout(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`WITH member AS (SELECT * FROM unnest(?::bigint[], ?::bigint[]) AS u(series_id, grp)),
			b AS (SELECT member.grp, r.bucket, `+combineFunc(average)+`(r.vsum / r.n) AS v
				FROM metrics.samples_5m r JOIN member ON member.series_id = r.series_id
				WHERE r.series_id = ANY(?::bigint[]) AND r.bucket >= ? AND r.bucket < ?
				GROUP BY member.grp, r.bucket)
			SELECT grp AS id, avg(v) AS avg, min(v) AS min, max(v) AS peak,
				percentile_cont(0.95) WITHIN GROUP (ORDER BY v) AS p95,
				sum(v) AS bucket_sum, count(*) AS buckets
			FROM b GROUP BY grp ORDER BY grp`, ids, grp, ids, lo, hi).Scan(&rows).Error
	})
	if err != nil {
		return nil, fmt.Errorf("reading combined statistics: %w", err)
	}
	for _, r := range rows {
		out[r.ID] = r.stat(expected)
	}
	return out, nil
}

// chartResolution picks a report chart's rollup and step. The rollup follows
// PickResolution's split (the 5-minute rollup up to 7 days, hourly beyond);
// rawSince = to keeps it off the raw table, as a report reads whole buckets
// only. The step is a multiple of the rollup's bucket that keeps the chart to
// about maxPoints points.
func chartResolution(from, to time.Time, maxPoints int) (table string, base, step time.Duration) {
	table, base = "metrics.samples_5m", statsBucket
	if source, _ := PickResolution(from, to, to); source == "1h" {
		table, base = "metrics.samples_1h", time.Hour
	}
	if maxPoints < 1 {
		maxPoints = 1
	}
	step = base
	if n := to.Sub(from) / time.Duration(maxPoints); n > step {
		step = ((n + base - 1) / base) * base
	}
	return table, base, step
}

// CombinedSeries returns about maxPoints chart points for the combined series,
// from samples_5m for windows up to 7 days and samples_1h beyond.
// Series combine per rollup bucket as in CombinedStats; each point is the
// mean of the combined buckets in its step. Only whole buckets that have
// ended are read.
func (m *MetricsStore) CombinedSeries(ctx context.Context, q CombinedQuery, maxPoints int) ([]ChartPoint, error) {
	out := []ChartPoint{}
	table, base, step := chartResolution(q.From, q.To, maxPoints)
	lo, hi, n := statsWindow(q.From, q.To, m.clock(), base)
	if len(q.SeriesIDs) == 0 || n == 0 {
		return out, nil
	}
	var rows []struct {
		T time.Time `gorm:"column:t"`
		V float64   `gorm:"column:v"`
	}
	err := m.withStatementTimeout(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT time_bucket(make_interval(secs => ?), b.bucket) AS t, avg(b.v) AS v
			FROM (SELECT r.bucket, `+combineFunc(q.Average)+`(r.vsum / r.n) AS v
				FROM `+table+` r
				WHERE r.series_id = ANY(?::bigint[]) AND r.bucket >= ? AND r.bucket < ?
				GROUP BY r.bucket) b
			GROUP BY 1 ORDER BY 1`, step.Seconds(), models.Int64Array(q.SeriesIDs), lo, hi).Scan(&rows).Error
	})
	if err != nil {
		return nil, fmt.Errorf("reading chart series: %w", err)
	}
	for _, r := range rows {
		out = append(out, ChartPoint{T: r.T.UTC(), V: r.V})
	}
	return out, nil
}
```

`models.Int64Array` implements `driver.Valuer`, so GORM binds each one as a single parameter instead of expanding it. The `?::bigint[]` casts read its `{1,2,3}` text.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/services/ -run 'TestChartResolution|TestStatsWindow|TestPickResolution' -v`
Expected: PASS.

Run: `./scripts/test-db.sh -run 'TestDBCombinedStats|TestDBCombinedSeries|TestDBSeriesStats' -v`
Expected: PASS.

- [ ] **Step 7: Vet and commit**

Run: `go vet ./internal/services/`
Expected: no output.

```bash
git add backend/internal/services/metrics_stats.go backend/internal/services/report_metrics_data.go backend/internal/services/metrics_stats_test.go backend/internal/services/metrics_stats_db_test.go
git commit -m "feat(metrics): combined report statistics and chart series

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Resolving and validating a network scope as the report owner

**Files:**
- Create: `backend/internal/netreport/builder.go` (package doc, `Builder`, `NewBuilder`, `FieldError`, messages)
- Create: `backend/internal/netreport/scope.go` (resolving a scope as a user through site access)
- Create: `backend/internal/netreport/metrics.go` (describing and checking metric keys)
- Create: `backend/internal/netreport/rows.go` (series lookup and each metric's lines)
- Create: `backend/internal/netreport/validate.go` (`ValidateScope`)
- Test: `backend/internal/netreport/fixtures_db_test.go` (new; the fixtures Tasks 6-8 reuse)
- Test: `backend/internal/netreport/scope_db_test.go` (new)

**Interfaces:**
- Consumes:
  - From Task 1: `models.IsNetworkScope`, `models.ScopeType*`, `ReportScope.Validate`, `models.MaxReportSubjects`.
  - Existing: `services.SiteService.SiteAccess(ctx, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error)`, which returns `services.ErrSiteNotFound` for a missing site, `services.SiteAccessReadonly`, `services.NewSiteService(db)`, `services.MetricCatalogue`, `services.MetricIfSpeedBps`, `portmon.IsPhysical(portmon.IfInfo{Name, Descr, Type, ConnectorPresent})`, and `models.MetricKindStatus`.
- Produces (exported, per the contract):
  - `netreport.Builder`, `func NewBuilder(db *gorm.DB, metrics *services.MetricsStore) *Builder`.
  - `type FieldError struct{ Field, Message string }`, whose `Error()` returns `Message`.
  - `const MsgNotAvailable = "A chosen port, device or site is not available"`, `const MsgTextMetric = "text metrics cannot be reported"`.
  - `func (b *Builder) ValidateScope(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) error`. A hidden or missing subject gives exactly `&FieldError{Field: "scope_data", Message: MsgNotAvailable}`. Task 6's `Preview` returns the identical value.
- Produces (package-internal, used by Tasks 6-8):
  - `type port struct{ ID, DeviceID uuid.UUID; Name string; SpeedBps int64 }`.
  - `type device struct{ ID, SiteID uuid.UUID; Name string; Physical []uuid.UUID }`.
  - `type site struct{ ID uuid.UUID; Name string; Devices []uuid.UUID }`.
  - `type resolved struct{ scopeType string; ports []port; devices []device; sites []site; siteNames []string; unavailable int }`, with methods `deviceIDs() []uuid.UUID`, `portIDs() []uuid.UUID`, `physicalPorts() int` and `empty() bool`.
  - `func (b *Builder) resolve(ctx, user uuid.UUID, scopeType string, scope models.ReportScope) (*resolved, error)`.
  - `type metricInfo struct{ Key, Label, Unit, Source, Kind string; order int; profile string }`, with methods `port()`, `additive()`, `text()` and `reportable() bool`.
  - Source constants `sourceBuiltin = "builtin"`, `sourceProfile = "profile"`, `sourceCustom = "custom"`.
  - `func builtinInfo(key string) (metricInfo, bool)`.
  - `func (b *Builder) lookupMetrics(ctx, keys []string) (map[string]metricInfo, error)`.
  - `func checkMetrics(scopeType string, keys []string, infos map[string]metricInfo) error`.
  - `func deviceScope(scopeType string) bool`.
  - `type seriesIndex`, `func (b *Builder) loadSeries(ctx, res *resolved, metrics []string) (*seriesIndex, error)`.
  - `type row struct{ key, subject, name string; site bool; port *port; series []int64; total bool }`.
  - `func rowsFor(res *resolved, m metricInfo, idx *seriesIndex) []row`.
  - Line naming: a port is `"<device> · <port name> (<alias>)"`, a device total is `"<device>"`, a site total is `"<site> (site total)"`, and a device metric's instance is `"<device> · <series label or instance>"` (just `"<device>"` for an unnamed one).
  - Test fixtures (package `netreport`): `newSite`, `newDevice`, `shareSite`, `newPort`, `newVirtualPort`, `setRole`, `newSeries`, `portSeries`, `steady`, `sampleAt`, `refresh`, `newBuilder`, `newProfileMetric`.

How each scope resolves (always as the user, through `SiteService.SiteAccess`; readonly or better is visible):
- `ports`: the chosen ports. A port on a hidden site, or one that no longer exists, counts in `unavailable`.
- `devices`: the chosen devices, each with its present physical ports (for device totals). Hidden or missing devices count in `unavailable`.
- `port_roles`: the visible chosen sites (the hidden or missing ones count), then every present, collected port (`COALESCE(collect, collect_default)`) with one of the roles on a device in them, as the roles are now.
- `sites`: the visible chosen sites (the hidden or missing ones count), one total each, every present, collected port of their devices, and their devices (whose instances are the lines of device metrics such as CPU).

Lines per metric (`rowsFor`):
- Site totals come first. For a port metric, a site total combines the physical ports of the site's devices. For a device metric, it combines every instance of the site's devices.
- A port metric then gives each port its own line, except on a `devices` scope, where it gives each device its total over its physical ports.
- A device metric gives one line per instance of each device that has one.
- A port with no series still has its line, which shows "No data".

- [ ] **Step 1: Write the fixtures**

Create `backend/internal/netreport/fixtures_db_test.go`:

```go
package netreport

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func newSite(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, ?)`, id, name)
	return id
}

// newDevice inserts a device named name in siteID, with its own v2c credential.
func newDevice(t *testing.T, db *gorm.DB, siteID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	credID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, ?, '2c', 'x')`,
		credID, "cred-"+credID.String()[:8])
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, ?, ?)`,
		id, siteID, credID, name, name+".example.test")
	return id
}

func shareSite(t *testing.T, db *gorm.DB, siteID, userID uuid.UUID, permission string) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, ?)`,
		siteID, userID, permission)
}

// newPort inserts a present, collected, physical (ethernetCsmacd) port with
// the default role, access. speedBps 0 is an unknown speed.
func newPort(t *testing.T, db *gorm.DB, deviceID uuid.UUID, ifIndex int, name string, speedBps int64) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name, if_type, speed_bps, present, collect_default)
		VALUES (?, ?, ?, ?, 6, ?, true, true)`, id, deviceID, ifIndex, name, speedBps)
	return id
}

// newVirtualPort inserts a present interface that is not physical (ifType 53
// is a VLAN interface, 161 a port-channel) and that the user chose to collect.
func newVirtualPort(t *testing.T, db *gorm.DB, deviceID uuid.UUID, ifIndex int, name string, ifType int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name, if_type, present, collect_default, collect)
		VALUES (?, ?, ?, ?, ?, true, false, true)`, id, deviceID, ifIndex, name, ifType)
	return id
}

func setRole(t *testing.T, db *gorm.DB, portID uuid.UUID, role string) {
	t.Helper()
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = ? WHERE id = ?`, role, portID)
}

// newSeries inserts a series; portID links a port metric's series to its port.
func newSeries(t *testing.T, db *gorm.DB, deviceID uuid.UUID, metric, instance string, portID *uuid.UUID, label string) int64 {
	t.Helper()
	var id int64
	testdb.Must(t, db.Raw(`INSERT INTO metrics.series (device_id, metric, instance, interface_id, label)
		VALUES (?, ?, ?, ?, ?) RETURNING id`, deviceID, metric, instance, portID, label).Scan(&id).Error)
	return id
}

// portSeries inserts a port metric's series, as the port poll writes it.
func portSeries(t *testing.T, db *gorm.DB, deviceID, portID uuid.UUID, ifIndex int, metric string) int64 {
	t.Helper()
	return newSeries(t, db, deviceID, metric, strconv.Itoa(ifIndex), &portID, "")
}

// steady writes v one minute into each of n 5-minute buckets from start.
func steady(t *testing.T, db *gorm.DB, series int64, start time.Time, n int, v float64) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT ?::timestamptz + make_interval(mins => 5 * g + 1), ?, ? FROM generate_series(0, ? - 1) g`,
		start, series, v, n)
}

// sampleAt writes one raw sample.
func sampleAt(t *testing.T, db *gorm.DB, series int64, at time.Time, v float64) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value) VALUES (?, ?, ?)`, at, series, v)
}

// refresh materializes the rollups. They are real-time anyway, but reports
// on past periods read the materialized part, so the tests do too.
func refresh(t *testing.T, db *gorm.DB) {
	t.Helper()
	testdb.Exec(t, db, `CALL refresh_continuous_aggregate('metrics.samples_5m', NULL, NULL)`)
	testdb.Exec(t, db, `CALL refresh_continuous_aggregate('metrics.samples_1h', NULL, NULL)`)
}

func newBuilder(db *gorm.DB) *Builder { return NewBuilder(db, services.NewMetricsStore(db)) }

// newProfileMetric adds a metric to the profile named profile, creating the
// profile on first use.
func newProfileMetric(t *testing.T, db *gorm.DB, profile string, builtin bool, key, name, units, kind string, position int) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO metric_profiles (name, builtin) VALUES (?, ?) ON CONFLICT (name) DO NOTHING`, profile, builtin)
	testdb.Exec(t, db, `INSERT INTO profile_metrics (profile_id, name, key, source, kind, units, oid, position)
		SELECT id, ?, ?, 'scalar', ?, ?, '1.3.6.1.4.1.99999.1', ? FROM metric_profiles WHERE name = ?`,
		name, key, kind, units, position, profile)
}
```

- [ ] **Step 2: Write the failing tests**

Create `backend/internal/netreport/scope_db_test.go`:

```go
package netreport

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

const gigabit = int64(1_000_000_000)

func portIDsOf(ps []port) []uuid.UUID {
	out := make([]uuid.UUID, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}

func namesOf(rows []row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.name
	}
	return out
}

// The scope is resolved as its owner: a port on a site the owner cannot see
// and a port that no longer exists are counted, never shown.
func TestDBResolvePortsAsTheOwner(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	user := testdb.NewUser(t, db, false)
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	shareSite(t, db, hq, user, "readonly")
	sw := newDevice(t, db, hq, "core-sw1")
	far := newDevice(t, db, annex, "annex-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1/0/1", gigabit)
	testdb.Exec(t, db, `UPDATE device_interfaces SET alias = 'uplink to annex' WHERE id = ?`, gi1)
	hidden := newPort(t, db, far, 1, "Gi1/0/1", gigabit)
	b := newBuilder(db)

	res, err := b.resolve(ctx, user, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1, hidden, uuid.New()}})
	testdb.Must(t, err)
	if len(res.ports) != 1 || res.ports[0].ID != gi1 || res.ports[0].Name != "core-sw1 · Gi1/0/1 (uplink to annex)" ||
		res.ports[0].SpeedBps != gigabit || res.ports[0].DeviceID != sw {
		t.Errorf("ports = %+v, want only the visible one, named with its alias", res.ports)
	}
	if res.unavailable != 2 {
		t.Errorf("unavailable = %d, want 2 (one hidden, one missing)", res.unavailable)
	}

	admin := testdb.NewUser(t, db, true)
	res, err = b.resolve(ctx, admin, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1, hidden}})
	testdb.Must(t, err)
	if len(res.ports) != 2 || res.unavailable != 0 {
		t.Errorf("admin: %d ports, %d unavailable; want both ports and none unavailable", len(res.ports), res.unavailable)
	}
	if _, err := b.resolve(ctx, uuid.New(), models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1}}); err == nil {
		t.Error("a user that does not exist resolved a scope")
	}
}

// port_roles is expanded at each run: a role set after the report was made
// counts; ports the poll does not read, or that left the device, do not.
func TestDBResolvePortRolesAtRunTime(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	wan := newPort(t, db, sw, 1, "Gi1", gigabit)
	setRole(t, db, wan, models.PortRoleWAN)
	up := newPort(t, db, sw, 2, "Gi2", gigabit)
	setRole(t, db, up, models.PortRoleUplink)
	acc := newPort(t, db, sw, 3, "Gi3", gigabit) // access, the default
	off := newPort(t, db, sw, 4, "Gi4", gigabit)
	setRole(t, db, off, models.PortRoleWAN)
	testdb.Exec(t, db, `UPDATE device_interfaces SET collect = false WHERE id = ?`, off)
	gone := newPort(t, db, sw, 5, "Gi5", gigabit)
	setRole(t, db, gone, models.PortRoleWAN)
	testdb.Exec(t, db, `UPDATE device_interfaces SET present = false WHERE id = ?`, gone)
	b := newBuilder(db)
	scope := models.ReportScope{SiteIDs: []uuid.UUID{hq, uuid.New()}, Roles: []string{models.PortRoleWAN, models.PortRoleUplink}}

	res, err := b.resolve(ctx, admin, models.ScopeTypePortRoles, scope)
	testdb.Must(t, err)
	if !slices.Equal(portIDsOf(res.ports), []uuid.UUID{wan, up}) || res.unavailable != 1 || !slices.Equal(res.siteNames, []string{"HQ"}) {
		t.Fatalf("ports %v, unavailable %d, sites %v; want the WAN and uplink ports, the missing site counted",
			portIDsOf(res.ports), res.unavailable, res.siteNames)
	}

	setRole(t, db, acc, models.PortRoleWAN)
	res, err = b.resolve(ctx, admin, models.ScopeTypePortRoles, scope)
	testdb.Must(t, err)
	if !slices.Equal(portIDsOf(res.ports), []uuid.UUID{wan, up, acc}) {
		t.Errorf("after a role change: %v, want the newly WAN port included", portIDsOf(res.ports))
	}
}

// A device total adds up the device's physical ports only: not a VLAN
// interface, not a port-channel, not a port no longer present.
func TestDBDeviceTotalsCountPhysicalPortsOnly(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1/0/1", gigabit)
	vlan := newVirtualPort(t, db, sw, 100, "Vlan10", 53)
	po := newVirtualPort(t, db, sw, 200, "Port-channel1", 161)
	old := newPort(t, db, sw, 2, "Gi1/0/2", gigabit)
	testdb.Exec(t, db, `UPDATE device_interfaces SET present = false WHERE id = ?`, old)
	physical := portSeries(t, db, sw, gi1, 1, services.MetricIfInBps)
	portSeries(t, db, sw, vlan, 100, services.MetricIfInBps)
	portSeries(t, db, sw, po, 200, services.MetricIfInBps)
	portSeries(t, db, sw, old, 2, services.MetricIfInBps)
	b := newBuilder(db)

	res, err := b.resolve(ctx, admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{sw}})
	testdb.Must(t, err)
	if len(res.devices) != 1 || !slices.Equal(res.devices[0].Physical, []uuid.UUID{gi1}) || res.physicalPorts() != 1 {
		t.Fatalf("devices = %+v, want core-sw1 with Gi1/0/1 as its only physical port", res.devices)
	}
	idx, err := b.loadSeries(ctx, res, []string{services.MetricIfInBps})
	testdb.Must(t, err)
	m, _ := builtinInfo(services.MetricIfInBps)
	rows := rowsFor(res, m, idx)
	if len(rows) != 1 || rows[0].key != "d:"+sw.String() || rows[0].subject != "d:"+sw.String() || rows[0].name != "core-sw1" ||
		!rows[0].total || !slices.Equal(rows[0].series, []int64{physical}) {
		t.Errorf("lines = %+v, want one device total of the physical port's series", rows)
	}
}

// A sites scope: one total per site (in the order chosen), then each port;
// a device metric has a line per instance, and its site total adds them all.
func TestDBResolveSitesLines(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	sw1, sw2 := newDevice(t, db, hq, "core-sw1"), newDevice(t, db, annex, "annex-sw1")
	a1, a2, b1 := newPort(t, db, sw1, 1, "Gi1", gigabit), newPort(t, db, sw1, 2, "Gi2", gigabit), newPort(t, db, sw2, 1, "Gi1", gigabit)
	s1 := portSeries(t, db, sw1, a1, 1, services.MetricIfInBps)
	s2 := portSeries(t, db, sw1, a2, 2, services.MetricIfInBps)
	s3 := portSeries(t, db, sw2, b1, 1, services.MetricIfInBps)
	newProfileMetric(t, db, "Core health", true, "core_cpu", "CPU", "%", "gauge", 0)
	c1 := newSeries(t, db, sw1, "core_cpu", "1", nil, "Switch 1")
	c2 := newSeries(t, db, sw1, "core_cpu", "2", nil, "Switch 2")
	b := newBuilder(db)

	res, err := b.resolve(ctx, admin, models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{annex, hq}})
	testdb.Must(t, err)
	if !slices.Equal(res.siteNames, []string{"Annex", "HQ"}) || len(res.sites) != 2 || len(res.ports) != 3 || len(res.devices) != 2 {
		t.Fatalf("resolved = %+v", res)
	}
	keys := []string{services.MetricIfInBps, "core_cpu"}
	idx, err := b.loadSeries(ctx, res, keys)
	testdb.Must(t, err)
	infos, err := b.lookupMetrics(ctx, keys)
	testdb.Must(t, err)

	traffic := rowsFor(res, infos[services.MetricIfInBps], idx)
	if want := []string{"Annex (site total)", "HQ (site total)", "annex-sw1 · Gi1", "core-sw1 · Gi1", "core-sw1 · Gi2"}; !slices.Equal(namesOf(traffic), want) {
		t.Fatalf("traffic lines = %v, want %v", namesOf(traffic), want)
	}
	if !traffic[0].site || traffic[0].subject != "" || !slices.Equal(traffic[0].series, []int64{s3}) ||
		!slices.Equal(traffic[1].series, []int64{s1, s2}) || traffic[2].subject != "p:"+b1.String() ||
		traffic[2].port == nil || !slices.Equal(traffic[2].series, []int64{s3}) {
		t.Errorf("traffic lines = %+v", traffic)
	}

	cpu := rowsFor(res, infos["core_cpu"], idx)
	if want := []string{"Annex (site total)", "HQ (site total)", "core-sw1 · Switch 1", "core-sw1 · Switch 2"}; !slices.Equal(namesOf(cpu), want) {
		t.Fatalf("cpu lines = %v, want %v", namesOf(cpu), want)
	}
	if len(cpu[0].series) != 0 || !slices.Equal(cpu[1].series, []int64{c1, c2}) || cpu[2].subject != "d:"+sw1.String() ||
		cpu[2].total || !slices.Equal(cpu[2].series, []int64{c1}) {
		t.Errorf("cpu lines = %+v", cpu)
	}
}

// Hidden and missing subjects get the very same error, so the editor cannot
// be used to find out which ids exist.
func TestDBValidateScopeOneMessageForHiddenAndMissing(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	user := testdb.NewUser(t, db, false)
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	shareSite(t, db, hq, user, "readonly")
	sw, far := newDevice(t, db, hq, "core-sw1"), newDevice(t, db, annex, "annex-sw1")
	gi1, farPort := newPort(t, db, sw, 1, "Gi1", gigabit), newPort(t, db, far, 1, "Gi1", gigabit)
	b := newBuilder(db)
	m := []string{services.MetricIfInBps}
	wan := []string{models.PortRoleWAN}
	cases := []struct {
		name, scopeType string
		hidden, missing models.ReportScope
	}{
		{"ports", models.ScopeTypePorts,
			models.ReportScope{PortIDs: []uuid.UUID{gi1, farPort}, Metrics: m},
			models.ReportScope{PortIDs: []uuid.UUID{gi1, uuid.New()}, Metrics: m}},
		{"devices", models.ScopeTypeDevices,
			models.ReportScope{DeviceIDs: []uuid.UUID{far}, Metrics: m},
			models.ReportScope{DeviceIDs: []uuid.UUID{uuid.New()}, Metrics: m}},
		{"port roles", models.ScopeTypePortRoles,
			models.ReportScope{SiteIDs: []uuid.UUID{annex}, Roles: wan, Metrics: m},
			models.ReportScope{SiteIDs: []uuid.UUID{uuid.New()}, Roles: wan, Metrics: m}},
		{"sites", models.ScopeTypeSites,
			models.ReportScope{SiteIDs: []uuid.UUID{hq, annex}, Metrics: m},
			models.ReportScope{SiteIDs: []uuid.UUID{hq, uuid.New()}, Metrics: m}},
	}
	for _, c := range cases {
		var hidden, missing *FieldError
		if err := b.ValidateScope(ctx, user, c.scopeType, c.hidden); !errors.As(err, &hidden) {
			t.Fatalf("%s hidden: %v, want a FieldError", c.name, err)
		}
		if err := b.ValidateScope(ctx, user, c.scopeType, c.missing); !errors.As(err, &missing) {
			t.Fatalf("%s missing: %v, want a FieldError", c.name, err)
		}
		if *hidden != *missing || hidden.Field != "scope_data" || hidden.Message != MsgNotAvailable {
			t.Errorf("%s: hidden %+v, missing %+v; want both {scope_data, %q}", c.name, *hidden, *missing, MsgNotAvailable)
		}
	}
	if err := b.ValidateScope(ctx, user, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1}, Metrics: m}); err != nil {
		t.Errorf("a visible port: %v", err)
	}
}

// Metrics a report cannot show are refused with a message the user can act on.
func TestDBValidateScopeMetrics(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1", gigabit)
	newProfileMetric(t, db, "Core health", true, "core_cpu", "CPU", "%", "gauge", 0)
	newProfileMetric(t, db, "Core health", true, "core_fan", "Fan", "", "status", 1)
	b := newBuilder(db)
	ports := models.ReportScope{PortIDs: []uuid.UUID{gi1}}
	devices := models.ReportScope{DeviceIDs: []uuid.UUID{sw}}
	with := func(s models.ReportScope, metrics ...string) models.ReportScope { s.Metrics = metrics; return s }
	cases := []struct {
		name, scopeType string
		scope           models.ReportScope
		want            string // "" = valid
	}{
		{"traffic on ports", models.ScopeTypePorts, with(ports, services.MetricIfInBps, services.MetricIfOutBps), ""},
		{"cpu and on battery on devices", models.ScopeTypeDevices, with(devices, "core_cpu", services.MetricUPSOnBattery), ""},
		{"an unknown metric", models.ScopeTypePorts, with(ports, services.MetricIfInBps, "nope_metric"), "nope_metric is not a known metric"},
		{"an enum", models.ScopeTypeDevices, with(devices, services.MetricUPSBatteryStatus), MsgTextMetric},
		{"a status metric", models.ScopeTypeDevices, with(devices, "core_fan"), MsgTextMetric},
		{"link speed", models.ScopeTypeDevices, with(devices, services.MetricIfSpeedBps), "link speed cannot be reported"},
		{"a device metric on ports", models.ScopeTypePorts, with(ports, "core_cpu"), "CPU is a device metric: report it on devices or sites"},
		{"no metrics", models.ScopeTypePorts, ports, "scope_data.metrics is required for a metrics report"},
		{"a monitor scope", models.ScopeTypeMonitors, with(models.ReportScope{MonitorIDs: []uuid.UUID{uuid.New()}}, services.MetricIfInBps),
			"scope_type must be one of: ports, port_roles, devices, sites"},
	}
	for _, c := range cases {
		err := b.ValidateScope(ctx, admin, c.scopeType, c.scope)
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Message != c.want {
			t.Errorf("%s: %v, want a FieldError %q", c.name, err, c.want)
		}
	}
}
```

- [ ] **Step 3: Run them and watch them fail**

Run: `go vet ./internal/netreport/`
Expected: it fails, because the package has no non-test Go files yet and the tests refer to `undefined: Builder`, `undefined: port` and so on.

- [ ] **Step 4: Write `builder.go`**

Create `backend/internal/netreport/builder.go`:

```go
// Package netreport builds the Metrics report. It resolves a network scope
// (ports, port roles at sites, devices, sites) as the report's owner through
// site access, reads the statistics the PDF shows from the 5-minute rollup,
// and fills a services.MetricsReportData. It lives outside services, as
// dashboards does, so services never imports it: the report pipeline reaches
// it through services.NetworkReportBuilder.
package netreport

import (
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// User-facing messages, returned verbatim in the API's 400 responses.
const (
	// MsgNotAvailable answers a chosen port, device or site that is hidden
	// from the requester or no longer exists: one message for both, so the
	// editor cannot be used to probe ids.
	MsgNotAvailable = "A chosen port, device or site is not available"
	// MsgTextMetric refuses a metric whose values are codes, not amounts.
	MsgTextMetric = "text metrics cannot be reported"
)

// FieldError is a create/preview validation error the API returns as 400 {error: Message}.
type FieldError struct{ Field, Message string }

// Error is the message alone: it is what the user reads.
func (e *FieldError) Error() string { return e.Message }

// Builder resolves network scopes and builds Metrics reports.
type Builder struct {
	db      *gorm.DB
	metrics *services.MetricsStore
	sites   *services.SiteService
}

// NewBuilder returns a Builder reading db and the shared metrics store.
func NewBuilder(db *gorm.DB, metrics *services.MetricsStore) *Builder {
	return &Builder{db: db, metrics: metrics, sites: services.NewSiteService(db)}
}
```

- [ ] **Step 5: Write `scope.go`**

Create `backend/internal/netreport/scope.go`:

```go
package netreport

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// port is one port of a resolved scope: a line of its own in every port
// metric's table.
type port struct {
	ID       uuid.UUID
	DeviceID uuid.UUID
	// Name is "device · port (alias)".
	Name string
	// SpeedBps is the link speed, 0 when unknown (no busy % is recorded then).
	SpeedBps int64
}

// device is one device of a resolved scope.
type device struct {
	ID     uuid.UUID
	SiteID uuid.UUID
	Name   string
	// Physical lists its present physical ports. A device or site total of a
	// port metric adds up these only (phase 4's rule), so a VLAN interface or
	// a port-channel never counts the same traffic twice.
	Physical []uuid.UUID
}

// site is one site total of a sites scope.
type site struct {
	ID      uuid.UUID
	Name    string
	Devices []uuid.UUID
}

// resolved is a network scope as one user may see it now.
type resolved struct {
	scopeType string
	// ports: the ports scope's ports, port_roles' matches, a sites scope's
	// collected ports.
	ports []port
	// devices: the devices scope's devices; a sites scope's devices (their
	// instances are the lines of device metrics, their physical ports make
	// the site totals).
	devices []device
	// sites: a sites scope's sites, one total line each.
	sites []site
	// siteNames are the chosen sites the user may see, in the order chosen.
	siteNames []string
	// unavailable counts chosen ids that are hidden from the user or gone.
	unavailable int
}

// deviceIDs lists every device the scope reads series from, once each.
func (r *resolved) deviceIDs() []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	add := func(id uuid.UUID) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, p := range r.ports {
		add(p.DeviceID)
	}
	for _, d := range r.devices {
		add(d.ID)
	}
	return out
}

// portIDs lists the scope's ports.
func (r *resolved) portIDs() []uuid.UUID {
	out := make([]uuid.UUID, len(r.ports))
	for i, p := range r.ports {
		out[i] = p.ID
	}
	return out
}

// physicalPorts counts the physical ports of the scope's devices: what the
// device totals of a devices scope add up.
func (r *resolved) physicalPorts() int {
	n := 0
	for _, d := range r.devices {
		n += len(d.Physical)
	}
	return n
}

// empty reports a scope whose every chosen subject is unavailable.
func (r *resolved) empty() bool {
	return r.unavailable > 0 && len(r.ports) == 0 && len(r.devices) == 0 && len(r.siteNames) == 0
}

// gate answers "may the user see this site?" once per site, by the site
// access rules (services.SiteService.SiteAccess, readonly or better). A site
// that no longer exists is not visible.
type gate struct {
	sites   *services.SiteService
	user    uuid.UUID
	isAdmin bool
	seen    map[uuid.UUID]bool
}

// gateFor loads the user's admin flag. A user that no longer exists is an
// error, not an empty report: reports are deleted with their owner.
func (b *Builder) gateFor(ctx context.Context, user uuid.UUID) (*gate, error) {
	var u struct{ IsAdmin bool }
	res := b.db.WithContext(ctx).Raw(`SELECT is_admin FROM users WHERE id = ?`, user).Scan(&u)
	if res.Error != nil {
		return nil, fmt.Errorf("loading the report owner: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, errors.New("loading the report owner: no such user")
	}
	return &gate{sites: b.sites, user: user, isAdmin: u.IsAdmin, seen: map[uuid.UUID]bool{}}, nil
}

func (g *gate) visible(ctx context.Context, siteID uuid.UUID) (bool, error) {
	if v, ok := g.seen[siteID]; ok {
		return v, nil
	}
	level, err := g.sites.SiteAccess(ctx, g.user, g.isAdmin, siteID)
	if errors.Is(err, services.ErrSiteNotFound) {
		g.seen[siteID] = false
		return false, nil
	}
	if err != nil {
		return false, err
	}
	g.seen[siteID] = level >= services.SiteAccessReadonly
	return g.seen[siteID], nil
}

// resolve works out a network scope as user sees it now. Chosen ports,
// devices or sites that are hidden from the user, or that no longer exist,
// are counted in unavailable and otherwise left out. port_roles and sites
// are expanded from the ports and devices there are now, so a role set after
// the report was made is included.
func (b *Builder) resolve(ctx context.Context, user uuid.UUID, scopeType string, scope models.ReportScope) (*resolved, error) {
	g, err := b.gateFor(ctx, user)
	if err != nil {
		return nil, err
	}
	res := &resolved{scopeType: scopeType}
	switch scopeType {
	case models.ScopeTypePorts:
		err = b.resolvePorts(ctx, g, scope.PortIDs, res)
	case models.ScopeTypeDevices:
		err = b.resolveDevices(ctx, g, scope.DeviceIDs, res)
	case models.ScopeTypePortRoles:
		err = b.resolvePortRoles(ctx, g, scope.SiteIDs, scope.Roles, res)
	case models.ScopeTypeSites:
		err = b.resolveSites(ctx, g, scope.SiteIDs, res)
	default:
		err = fmt.Errorf("%q is not a network scope type", scopeType)
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ifaceRow is a device_interfaces row with its device, as the scope reads it.
type ifaceRow struct {
	ID               uuid.UUID `gorm:"column:id"`
	DeviceID         uuid.UUID `gorm:"column:device_id"`
	SiteID           uuid.UUID `gorm:"column:site_id"`
	Device           string    `gorm:"column:device"`
	IfIndex          int       `gorm:"column:if_index"`
	Name             string    `gorm:"column:name"`
	Descr            string    `gorm:"column:descr"`
	Alias            string    `gorm:"column:alias"`
	IfType           int       `gorm:"column:if_type"`
	SpeedBps         int64     `gorm:"column:speed_bps"`
	ConnectorPresent *bool     `gorm:"column:connector_present"`
	Present          bool      `gorm:"column:present"`
}

// ifaceSelect reads ifaceRows; the nullable text and number columns come back
// as zero values.
const ifaceSelect = `SELECT di.id, di.device_id, d.site_id, COALESCE(NULLIF(d.name, ''), d.host) AS device,
	di.if_index, COALESCE(di.name, '') AS name, COALESCE(di.descr, '') AS descr,
	COALESCE(di.alias, '') AS alias, COALESCE(di.if_type, 0) AS if_type,
	COALESCE(di.speed_bps, 0) AS speed_bps, di.connector_present, di.present
	FROM device_interfaces di JOIN devices d ON d.id = di.device_id`

// ifaceOrder lists ports by device name, then index.
const ifaceOrder = ` ORDER BY lower(COALESCE(NULLIF(d.name, ''), d.host)), d.id, di.if_index`

// collected keeps the ports the stats poll reads: only they have traffic.
const collected = ` AND di.present AND COALESCE(di.collect, di.collect_default)`

// physical reports a present port a cable plugs into (portmon's rule, as
// PortService.PhysicalInterfaceIDs uses it).
func (r ifaceRow) physical() bool {
	return r.Present && portmon.IsPhysical(portmon.IfInfo{Name: r.Name, Descr: r.Descr, Type: r.IfType, ConnectorPresent: r.ConnectorPresent})
}

// port names the row "device · port (alias)": the port's name, else its
// description, else its index, with the alias when it says something more.
func (r ifaceRow) port() port {
	name := r.Name
	if name == "" {
		name = r.Descr
	}
	if name == "" {
		name = "ifIndex " + strconv.Itoa(r.IfIndex)
	}
	if r.Alias != "" && r.Alias != name {
		name += " (" + r.Alias + ")"
	}
	return port{ID: r.ID, DeviceID: r.DeviceID, Name: r.Device + " · " + name, SpeedBps: r.SpeedBps}
}

func (b *Builder) resolvePorts(ctx context.Context, g *gate, ids []uuid.UUID, res *resolved) error {
	var rows []ifaceRow
	if err := b.db.WithContext(ctx).Raw(ifaceSelect+` WHERE di.id IN ?`+ifaceOrder, ids).Scan(&rows).Error; err != nil {
		return fmt.Errorf("loading the chosen ports: %w", err)
	}
	found := map[uuid.UUID]bool{}
	for _, r := range rows {
		ok, err := g.visible(ctx, r.SiteID)
		if err != nil {
			return err
		}
		if ok {
			found[r.ID] = true
			res.ports = append(res.ports, r.port())
		}
	}
	res.unavailable = countMissing(ids, found)
	return nil
}

// deviceRow is a devices row as the scope reads it.
type deviceRow struct {
	ID     uuid.UUID `gorm:"column:id"`
	SiteID uuid.UUID `gorm:"column:site_id"`
	Name   string    `gorm:"column:name"`
}

const deviceSelect = `SELECT id, site_id, COALESCE(NULLIF(name, ''), host) AS name FROM devices`

const deviceOrder = ` ORDER BY lower(COALESCE(NULLIF(name, ''), host)), id`

func (b *Builder) resolveDevices(ctx context.Context, g *gate, ids []uuid.UUID, res *resolved) error {
	var rows []deviceRow
	if err := b.db.WithContext(ctx).Raw(deviceSelect+` WHERE id IN ?`+deviceOrder, ids).Scan(&rows).Error; err != nil {
		return fmt.Errorf("loading the chosen devices: %w", err)
	}
	found := map[uuid.UUID]bool{}
	var visible []deviceRow
	for _, r := range rows {
		ok, err := g.visible(ctx, r.SiteID)
		if err != nil {
			return err
		}
		if ok {
			found[r.ID] = true
			visible = append(visible, r)
		}
	}
	res.unavailable = countMissing(ids, found)
	devices, err := b.withPhysical(ctx, visible)
	if err != nil {
		return err
	}
	res.devices = devices
	return nil
}

// withPhysical turns device rows into devices with their physical ports.
func (b *Builder) withPhysical(ctx context.Context, rows []deviceRow) ([]device, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	var ifs []ifaceRow
	if err := b.db.WithContext(ctx).Raw(ifaceSelect+` WHERE di.device_id IN ? AND di.present`+ifaceOrder, ids).Scan(&ifs).Error; err != nil {
		return nil, fmt.Errorf("loading the devices' ports: %w", err)
	}
	physical := map[uuid.UUID][]uuid.UUID{}
	for _, r := range ifs {
		if r.physical() {
			physical[r.DeviceID] = append(physical[r.DeviceID], r.ID)
		}
	}
	out := make([]device, len(rows))
	for i, r := range rows {
		out[i] = device{ID: r.ID, SiteID: r.SiteID, Name: r.Name, Physical: physical[r.ID]}
	}
	return out, nil
}

// siteRow is a sites row as the scope reads it.
type siteRow struct {
	ID   uuid.UUID `gorm:"column:id"`
	Name string    `gorm:"column:name"`
}

// visibleSites returns the chosen sites the user may see, in the order
// chosen, records their names, and counts the rest as unavailable.
func (b *Builder) visibleSites(ctx context.Context, g *gate, ids []uuid.UUID, res *resolved) ([]siteRow, error) {
	var rows []siteRow
	if err := b.db.WithContext(ctx).Raw(`SELECT id, name FROM sites WHERE id IN ?`, ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading the chosen sites: %w", err)
	}
	byID := make(map[uuid.UUID]siteRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	found := map[uuid.UUID]bool{}
	var out []siteRow
	for _, id := range ids {
		r, ok := byID[id]
		if !ok || found[id] {
			continue
		}
		vis, err := g.visible(ctx, id)
		if err != nil {
			return nil, err
		}
		if vis {
			found[id] = true
			out = append(out, r)
			res.siteNames = append(res.siteNames, r.Name)
		}
	}
	res.unavailable += countMissing(ids, found)
	return out, nil
}

func (b *Builder) resolvePortRoles(ctx context.Context, g *gate, siteIDs []uuid.UUID, roles []string, res *resolved) error {
	sites, err := b.visibleSites(ctx, g, siteIDs, res)
	if err != nil || len(sites) == 0 {
		return err
	}
	var rows []ifaceRow
	if err := b.db.WithContext(ctx).Raw(ifaceSelect+` WHERE d.site_id IN ? AND di.role IN ?`+collected+ifaceOrder,
		siteIDsOf(sites), roles).Scan(&rows).Error; err != nil {
		return fmt.Errorf("finding the ports with those roles: %w", err)
	}
	for _, r := range rows {
		res.ports = append(res.ports, r.port())
	}
	return nil
}

func (b *Builder) resolveSites(ctx context.Context, g *gate, ids []uuid.UUID, res *resolved) error {
	sites, err := b.visibleSites(ctx, g, ids, res)
	if err != nil || len(sites) == 0 {
		return err
	}
	visible := siteIDsOf(sites)
	var devs []deviceRow
	if err := b.db.WithContext(ctx).Raw(deviceSelect+` WHERE site_id IN ?`+deviceOrder, visible).Scan(&devs).Error; err != nil {
		return fmt.Errorf("loading the sites' devices: %w", err)
	}
	if res.devices, err = b.withPhysical(ctx, devs); err != nil {
		return err
	}
	for _, s := range sites {
		st := site{ID: s.ID, Name: s.Name}
		for _, d := range res.devices {
			if d.SiteID == s.ID {
				st.Devices = append(st.Devices, d.ID)
			}
		}
		res.sites = append(res.sites, st)
	}
	var rows []ifaceRow
	if err := b.db.WithContext(ctx).Raw(ifaceSelect+` WHERE d.site_id IN ?`+collected+ifaceOrder, visible).Scan(&rows).Error; err != nil {
		return fmt.Errorf("loading the sites' ports: %w", err)
	}
	for _, r := range rows {
		res.ports = append(res.ports, r.port())
	}
	return nil
}

func siteIDsOf(rows []siteRow) []uuid.UUID {
	out := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// countMissing counts the distinct ids not in found.
func countMissing(ids []uuid.UUID, found map[uuid.UUID]bool) int {
	n := 0
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if !found[id] && !seen[id] {
			n++
		}
		seen[id] = true
	}
	return n
}
```

- [ ] **Step 6: Write `metrics.go`**

Create `backend/internal/netreport/metrics.go`:

```go
package netreport

import (
	"context"
	"fmt"
	"strings"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Where a metric comes from, as the Metrics step labels it.
const (
	sourceBuiltin = "builtin" // Sentinel's own catalogue (ports, UPS)
	sourceProfile = "profile" // a metric of a built-in profile (Cisco switch health)
	sourceCustom  = "custom"  // a metric of a profile an admin made
)

// metricInfo is a metric as a report needs to know it.
type metricInfo struct {
	Key, Label, Unit string
	Source           string
	// Kind is a profile metric's kind (gauge, counter, status); "" built-in.
	Kind string
	// order and profile sort the choices: the catalogue's order for built-in
	// metrics; profile name (lower case), then position, for the others.
	order   int
	profile string
}

// port reports a built-in port metric, which has a series per port. Custom
// keys cannot start with if_ (profile_metrics' CHECK).
func (m metricInfo) port() bool { return strings.HasPrefix(m.Key, "if_") }

// additive reports a unit whose series add up when lines combine (bps,
// per_min); every other unit is averaged (phase 4's rule).
func (m metricInfo) additive() bool { return m.Unit == "bps" || m.Unit == "per_min" }

// text reports a metric whose values are codes, not amounts: an enum, or a
// profile's status metric (a fan's state). Its average means nothing.
func (m metricInfo) text() bool { return m.Unit == "enum" || m.Kind == models.MetricKindStatus }

// reportable reports whether a report can show m: not a text metric, and not
// link speed (bits per second that are not traffic, whose "bytes moved"
// would be nonsense).
func (m metricInfo) reportable() bool { return !m.text() && m.Key != services.MetricIfSpeedBps }

// builtinInfo describes a key of Sentinel's own catalogue.
func builtinInfo(key string) (metricInfo, bool) {
	for i, d := range services.MetricCatalogue {
		if d.Key == key {
			return metricInfo{Key: d.Key, Label: d.Label, Unit: d.Unit, Source: sourceBuiltin, order: i}, true
		}
	}
	return metricInfo{}, false
}

// lookupMetrics describes these keys: the built-in catalogue first, then
// profile_metrics, read directly (not through the in-memory key registry, so
// a report never depends on when that was last loaded). Unknown keys are
// absent.
func (b *Builder) lookupMetrics(ctx context.Context, keys []string) (map[string]metricInfo, error) {
	out := make(map[string]metricInfo, len(keys))
	var custom []string
	for _, k := range keys {
		if m, ok := builtinInfo(k); ok {
			out[k] = m
		} else {
			custom = append(custom, k)
		}
	}
	if len(custom) == 0 {
		return out, nil
	}
	var rows []struct {
		Key      string `gorm:"column:key"`
		Name     string `gorm:"column:name"`
		Units    string `gorm:"column:units"`
		Kind     string `gorm:"column:kind"`
		Builtin  bool   `gorm:"column:builtin"`
		Profile  string `gorm:"column:profile"`
		Position int    `gorm:"column:position"`
	}
	if err := b.db.WithContext(ctx).Raw(`SELECT pm.key, pm.name, pm.units, pm.kind, p.builtin, p.name AS profile, pm.position
		FROM profile_metrics pm JOIN metric_profiles p ON p.id = pm.profile_id
		WHERE pm.key IN ?`, custom).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("describing profile metrics: %w", err)
	}
	for _, r := range rows {
		src := sourceCustom
		if r.Builtin {
			src = sourceProfile
		}
		out[r.Key] = metricInfo{Key: r.Key, Label: r.Name, Unit: r.Units, Source: src, Kind: r.Kind,
			order: r.Position, profile: strings.ToLower(r.Profile)}
	}
	return out, nil
}

// checkMetrics refuses what a report cannot show: an unknown key, a text
// metric, link speed, or a device metric on a ports or port_roles scope
// (which has no devices to give it lines).
func checkMetrics(scopeType string, keys []string, infos map[string]metricInfo) error {
	for _, k := range keys {
		m, ok := infos[k]
		switch {
		case !ok:
			return &FieldError{Field: "scope_data.metrics", Message: k + " is not a known metric"}
		case m.text():
			return &FieldError{Field: "scope_data.metrics", Message: MsgTextMetric}
		case !m.reportable():
			return &FieldError{Field: "scope_data.metrics", Message: "link speed cannot be reported"}
		case !m.port() && !deviceScope(scopeType):
			return &FieldError{Field: "scope_data.metrics", Message: m.Label + " is a device metric: report it on devices or sites"}
		}
	}
	return nil
}

// deviceScope reports a scope whose lines include devices.
func deviceScope(scopeType string) bool {
	return scopeType == models.ScopeTypeDevices || scopeType == models.ScopeTypeSites
}
```

- [ ] **Step 7: Write `rows.go`**

Create `backend/internal/netreport/rows.go`:

```go
package netreport

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// seriesRow is one metrics.series row.
type seriesRow struct {
	ID          int64      `gorm:"column:id"`
	DeviceID    uuid.UUID  `gorm:"column:device_id"`
	Metric      string     `gorm:"column:metric"`
	Instance    string     `gorm:"column:instance"`
	InterfaceID *uuid.UUID `gorm:"column:interface_id"`
	Label       string     `gorm:"column:label"`
}

// seriesIndex finds a scope's series by port and by device.
type seriesIndex struct {
	byPort   map[uuid.UUID]map[string]int64
	byDevice map[uuid.UUID]map[string][]seriesRow
}

// loadSeries reads the scope's series of these metrics in one query. A
// device deleted since has none: DeviceService.Delete removes its series.
func (b *Builder) loadSeries(ctx context.Context, res *resolved, metrics []string) (*seriesIndex, error) {
	idx := &seriesIndex{byPort: map[uuid.UUID]map[string]int64{}, byDevice: map[uuid.UUID]map[string][]seriesRow{}}
	devices := res.deviceIDs()
	if len(devices) == 0 || len(metrics) == 0 {
		return idx, nil
	}
	var rows []seriesRow
	if err := b.db.WithContext(ctx).Raw(`SELECT id, device_id, metric, instance, interface_id, label
		FROM metrics.series WHERE device_id IN ? AND metric IN ?
		ORDER BY device_id, metric, length(instance), instance`, devices, metrics).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading the scope's series: %w", err)
	}
	for _, r := range rows {
		if r.InterfaceID != nil {
			if idx.byPort[*r.InterfaceID] == nil {
				idx.byPort[*r.InterfaceID] = map[string]int64{}
			}
			idx.byPort[*r.InterfaceID][r.Metric] = r.ID
		}
		if idx.byDevice[r.DeviceID] == nil {
			idx.byDevice[r.DeviceID] = map[string][]seriesRow{}
		}
		idx.byDevice[r.DeviceID][r.Metric] = append(idx.byDevice[r.DeviceID][r.Metric], r)
	}
	return idx, nil
}

// portSeries is the series of metric on each of these ports that has one.
func (x *seriesIndex) portSeries(ports []uuid.UUID, metric string) []int64 {
	var out []int64
	for _, p := range ports {
		if id, ok := x.byPort[p][metric]; ok {
			out = append(out, id)
		}
	}
	return out
}

// deviceSeries is every series of metric on these devices.
func (x *seriesIndex) deviceSeries(devices []uuid.UUID, metric string) []int64 {
	var out []int64
	for _, d := range devices {
		for _, s := range x.byDevice[d][metric] {
			out = append(out, s.ID)
		}
	}
	return out
}

// row is one line of a metric's table.
type row struct {
	// key names the line across a pair's two metrics: "p:<port>",
	// "d:<device>", "s:<site>" or "i:<device>:<instance>".
	key string
	// subject is what the 500 cap counts the line as: its port ("p:<id>")
	// or its device ("d:<id>"); "" for a site total, which is never cut.
	subject string
	name    string
	site    bool  // a site total: leads its table, never ranked
	port    *port // a port's line (running hot lists ports only)
	series  []int64
	// total: the line combines its series per bucket (a device or site total).
	total bool
}

// rowsFor lists one metric's lines in scope order. Site totals come first.
// Then a port metric has a line per port (on a devices scope, a total per
// device over its physical ports), and a device metric a line per instance
// of each device that has one (a stack member's CPU, a sensor). A port with
// no series still gets its line, which shows "No data".
func rowsFor(res *resolved, m metricInfo, idx *seriesIndex) []row {
	var out []row
	for _, s := range res.sites {
		r := row{key: "s:" + s.ID.String(), name: s.Name + " (site total)", site: true, total: true}
		if m.port() {
			for _, d := range res.devices {
				if d.SiteID == s.ID {
					r.series = append(r.series, idx.portSeries(d.Physical, m.Key)...)
				}
			}
		} else {
			r.series = idx.deviceSeries(s.Devices, m.Key)
		}
		out = append(out, r)
	}
	switch {
	case m.port() && res.scopeType == models.ScopeTypeDevices:
		for _, d := range res.devices {
			out = append(out, row{key: "d:" + d.ID.String(), subject: "d:" + d.ID.String(), name: d.Name, total: true,
				series: idx.portSeries(d.Physical, m.Key)})
		}
	case m.port():
		for i := range res.ports {
			p := &res.ports[i]
			r := row{key: "p:" + p.ID.String(), subject: "p:" + p.ID.String(), name: p.Name, port: p}
			if id, ok := idx.byPort[p.ID][m.Key]; ok {
				r.series = []int64{id}
			}
			out = append(out, r)
		}
	default:
		for _, d := range res.devices {
			for _, s := range idx.byDevice[d.ID][m.Key] {
				out = append(out, row{key: "i:" + d.ID.String() + ":" + s.Instance, subject: "d:" + d.ID.String(),
					name: instanceName(d.Name, s), series: []int64{s.ID}})
			}
		}
	}
	return out
}

// instanceName is "device · instance label"; just the device for a metric
// with one unnamed instance (a UPS reading).
func instanceName(deviceName string, s seriesRow) string {
	label := s.Label
	if label == "" {
		label = s.Instance
	}
	if label == "" {
		return deviceName
	}
	return deviceName + " · " + label
}
```

- [ ] **Step 8: Write `validate.go`**

Create `backend/internal/netreport/validate.go`:

```go
package netreport

import (
	"context"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// msgNetworkScopeTypes refuses a scope type a metrics report cannot take.
const msgNetworkScopeTypes = "scope_type must be one of: ports, port_roles, devices, sites"

// ValidateScope checks a network scope as requester: shape (counts, roles),
// every named subject visible (one FieldError for hidden and missing), and the
// metrics (known, not enum, 1..10).
func (b *Builder) ValidateScope(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) error {
	if !models.IsNetworkScope(scopeType) {
		return &FieldError{Field: "scope_type", Message: msgNetworkScopeTypes}
	}
	if err := scope.Validate(scopeType); err != nil {
		return &FieldError{Field: "scope_data", Message: err.Error()}
	}
	res, err := b.resolve(ctx, requester, scopeType, scope)
	if err != nil {
		return err
	}
	if res.unavailable > 0 {
		return notAvailable()
	}
	infos, err := b.lookupMetrics(ctx, scope.Metrics)
	if err != nil {
		return err
	}
	return checkMetrics(scopeType, scope.Metrics, infos)
}

// notAvailable is the one error for hidden and missing subjects, the same
// from ValidateScope and Preview.
func notAvailable() *FieldError {
	return &FieldError{Field: "scope_data", Message: MsgNotAvailable}
}
```

- [ ] **Step 9: Run the tests**

Run: `go vet ./internal/netreport/`
Expected: no output.

Run: `./scripts/test-db.sh -run 'TestDBResolve|TestDBDeviceTotalsCountPhysicalPortsOnly|TestDBValidateScope' -v`
Expected: PASS for `TestDBResolvePortsAsTheOwner`, `TestDBResolvePortRolesAtRunTime`, `TestDBDeviceTotalsCountPhysicalPortsOnly`, `TestDBResolveSitesLines`, `TestDBValidateScopeOneMessageForHiddenAndMissing` and `TestDBValidateScopeMetrics`.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/netreport/
git commit -m "feat(reports): resolve and validate a network scope as the report owner

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Metric choices, defaults and the scope preview

**Files:**
- Create: `backend/internal/netreport/choices.go`
- Test: `backend/internal/netreport/choices_db_test.go` (new)

**Interfaces:**
- Consumes (Task 5):
  - `resolve`, `resolved` with `deviceIDs()`, `portIDs()`, `physicalPorts()`.
  - `lookupMetrics`, `metricInfo` with `reportable()`, `port()`, `order` and `profile`.
  - `sourceBuiltin`, `deviceScope`, `notAvailable()`, `msgNetworkScopeTypes`.
  - The fixtures `newSite`, `newDevice`, `shareSite`, `newPort`, `newSeries`, `portSeries`, `setRole`, `newProfileMetric`, `newBuilder`, `gigabit`.
- Consumes (Task 1): `ReportScope.ValidateSubjects`, `models.MaxReportSubjects`, `models.MaxReportMetrics`.
- Produces (contract):
  - `netreport.MetricChoice{Key, Label, Unit, Source string}`, with JSON `key`, `label`, `unit`, `source`. `Source` is `"builtin"`, `"profile"` or `"custom"`.
  - `netreport.Preview{Ports, Devices int; Capped bool; Metrics []MetricChoice; Defaults []string}`, with JSON `ports`, `devices`, `capped`, `metrics`, `defaults`.
  - `func (b *Builder) Preview(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) (*Preview, error)`.

What Preview returns (as agreed with Part C):
- `Ports` and `Devices` are filled for every scope type:
  - `ports`/`port_roles`: the ports, and their distinct devices.
  - `devices`: the chosen devices, and their physical ports (what the device totals add up).
  - `sites`: the collected ports and the devices of the visible sites.
- `Capped` is true only when the subjects exceed 500. The subjects are ports for `ports`/`port_roles`/`sites` and devices for `devices`.
- `Metrics` lists the metrics with stored series:
  - on the scope's ports, for `ports`/`port_roles` (port metrics only);
  - on its devices, for `devices`/`sites`.
  - It always includes the scope's fixed defaults, and it never lists a text metric (`enum` unit or `status` kind) or link speed.
  - Order: built-in metrics in catalogue order, then profile metrics by profile name and position.
- `Defaults`:
  - `ports`/`port_roles`: `if_in_bps`, `if_out_bps`, `if_in_util_pct`, `if_out_util_pct`.
  - `sites`: `if_in_bps`, `if_out_bps`.
  - `devices`: the chosen devices' profile metrics in choice order, then the UPS readings `ups_charge_pct`, `ups_load_pct`, `ups_runtime_min`, `ups_on_battery` that any of them has, capped at 10. If there are none, `if_in_bps`, `if_out_bps` (device totals).
- Errors are always a `*FieldError`, which the API returns as 400 `{"error": message}`:
  - a scope type that is not a network one: `scope_type must be one of: ports, port_roles, devices, sites`;
  - a bad shape: the model's message, with `Field` `scope_data`;
  - a hidden or missing subject: exactly `&FieldError{Field: "scope_data", Message: MsgNotAvailable}`, identical to `ValidateScope`'s.
- `scope.Metrics` is ignored.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/netreport/choices_db_test.go`:

```go
package netreport

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func choiceKeys(cs []MetricChoice) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Key
	}
	return out
}

func choiceSource(cs []MetricChoice, key string) string {
	for _, c := range cs {
		if c.Key == key {
			return c.Source
		}
	}
	return ""
}

// The Metrics step offers what the scope's devices or ports have stored,
// without text metrics or link speed, and pre-fills the defaults of spec §1.
func TestDBPreviewChoicesAndDefaults(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw, ups, plain := newDevice(t, db, hq, "core-sw1"), newDevice(t, db, hq, "ups1"), newDevice(t, db, hq, "edge-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1", gigabit)
	for _, m := range []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct,
		services.MetricIfOutUtilPct, services.MetricIfInErrorsPM, services.MetricIfSpeedBps} {
		portSeries(t, db, sw, gi1, 1, m)
	}
	newProfileMetric(t, db, "Core health", true, "core_cpu", "CPU", "%", "gauge", 0)
	newProfileMetric(t, db, "Core health", true, "core_fan", "Fan", "", "status", 1)
	newProfileMetric(t, db, "Lab sensors", false, "lab_temp", "Room temperature", "°C", "gauge", 0)
	newSeries(t, db, sw, "core_cpu", "1", nil, "Switch 1")
	newSeries(t, db, sw, "core_fan", "1", nil, "Fan 1")
	newSeries(t, db, sw, "lab_temp", "1", nil, "Rack A")
	for _, m := range services.UPSMetrics {
		newSeries(t, db, ups, m, "", nil, "")
	}
	edge := newPort(t, db, plain, 1, "Gi1", gigabit)
	portSeries(t, db, plain, edge, 1, services.MetricIfInBps)
	portSeries(t, db, plain, edge, 1, services.MetricIfOutBps)
	b := newBuilder(db)

	// Devices with profiles and a UPS.
	p, err := b.Preview(ctx, admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{sw, ups}})
	testdb.Must(t, err)
	wantChoices := []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct, services.MetricIfOutUtilPct,
		services.MetricIfInErrorsPM, services.MetricUPSChargePct, services.MetricUPSRuntimeMin, services.MetricUPSLoadPct,
		services.MetricUPSInputV, services.MetricUPSOutputV, services.MetricUPSBatteryTempC, services.MetricUPSOnBattery,
		"core_cpu", "lab_temp"}
	if !slices.Equal(choiceKeys(p.Metrics), wantChoices) {
		t.Errorf("device choices = %v, want %v (no enum, no status metric, no link speed)", choiceKeys(p.Metrics), wantChoices)
	}
	if choiceSource(p.Metrics, "core_cpu") != "profile" || choiceSource(p.Metrics, "lab_temp") != "custom" ||
		choiceSource(p.Metrics, services.MetricIfInBps) != "builtin" {
		t.Errorf("sources = %+v", p.Metrics)
	}
	wantDefaults := []string{"core_cpu", "lab_temp", services.MetricUPSChargePct, services.MetricUPSLoadPct,
		services.MetricUPSRuntimeMin, services.MetricUPSOnBattery}
	if !slices.Equal(p.Defaults, wantDefaults) {
		t.Errorf("device defaults = %v, want %v", p.Defaults, wantDefaults)
	}
	if p.Ports != 1 || p.Devices != 2 || p.Capped {
		t.Errorf("devices size = %d ports, %d devices, capped %v; want 1, 2, false", p.Ports, p.Devices, p.Capped)
	}

	// A plain switch: traffic in and out, as device totals.
	p, err = b.Preview(ctx, admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{plain}})
	testdb.Must(t, err)
	if want := []string{services.MetricIfInBps, services.MetricIfOutBps}; !slices.Equal(p.Defaults, want) || !slices.Equal(choiceKeys(p.Metrics), want) {
		t.Errorf("plain switch: choices %v, defaults %v; want both %v", choiceKeys(p.Metrics), p.Defaults, want)
	}

	// Ports: port metrics only, and the four port defaults.
	p, err = b.Preview(ctx, admin, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1}})
	testdb.Must(t, err)
	if want := []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct, services.MetricIfOutUtilPct,
		services.MetricIfInErrorsPM}; !slices.Equal(choiceKeys(p.Metrics), want) {
		t.Errorf("port choices = %v, want %v", choiceKeys(p.Metrics), want)
	}
	if want := []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct, services.MetricIfOutUtilPct}; !slices.Equal(p.Defaults, want) {
		t.Errorf("port defaults = %v, want %v", p.Defaults, want)
	}
	if p.Ports != 1 || p.Devices != 1 {
		t.Errorf("ports size = %d ports, %d devices; want 1, 1", p.Ports, p.Devices)
	}

	// Sites: device metrics are offered too; traffic in and out by default.
	p, err = b.Preview(ctx, admin, models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{hq}})
	testdb.Must(t, err)
	if want := []string{services.MetricIfInBps, services.MetricIfOutBps}; !slices.Equal(p.Defaults, want) {
		t.Errorf("site defaults = %v, want %v", p.Defaults, want)
	}
	if !slices.Contains(choiceKeys(p.Metrics), "core_cpu") || slices.Contains(choiceKeys(p.Metrics), services.MetricUPSBatteryStatus) {
		t.Errorf("site choices = %v, want core_cpu and no enum", choiceKeys(p.Metrics))
	}
	if p.Ports != 2 || p.Devices != 3 || p.Capped {
		t.Errorf("site size = %d ports, %d devices, capped %v; want 2, 3, false", p.Ports, p.Devices, p.Capped)
	}

	// Port roles: every port is "access" until someone says otherwise.
	p, err = b.Preview(ctx, admin, models.ScopeTypePortRoles, models.ReportScope{SiteIDs: []uuid.UUID{hq}, Roles: []string{models.PortRoleAccess}})
	testdb.Must(t, err)
	if p.Ports != 2 || p.Devices != 2 {
		t.Errorf("access ports size = %d ports, %d devices; want 2, 2", p.Ports, p.Devices)
	}
}

// Capped only when the subjects exceed 500: ports for a port_roles scope,
// devices (not their ports) for a devices scope.
func TestDBPreviewCapsAbove500(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, if_type, present, collect_default, role)
		SELECT ?, g, 'Gi' || g, 6, true, true, 'wan' FROM generate_series(1, 501) g`, sw)
	b := newBuilder(db)
	wan := models.ReportScope{SiteIDs: []uuid.UUID{hq}, Roles: []string{models.PortRoleWAN}}

	p, err := b.Preview(ctx, admin, models.ScopeTypePortRoles, wan)
	testdb.Must(t, err)
	if p.Ports != 501 || p.Devices != 1 || !p.Capped {
		t.Errorf("501 WAN ports: %d ports, %d devices, capped %v; want 501, 1, true", p.Ports, p.Devices, p.Capped)
	}
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = 'access' WHERE device_id = ? AND if_index = 501`, sw)
	p, err = b.Preview(ctx, admin, models.ScopeTypePortRoles, wan)
	testdb.Must(t, err)
	if p.Ports != 500 || p.Capped {
		t.Errorf("500 WAN ports: %d ports, capped %v; want 500, false", p.Ports, p.Capped)
	}
	p, err = b.Preview(ctx, admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{sw}})
	testdb.Must(t, err)
	if p.Devices != 1 || p.Ports != 501 || p.Capped {
		t.Errorf("one device of 501 ports: %d devices, %d ports, capped %v; want 1, 501, false", p.Devices, p.Ports, p.Capped)
	}
}

// Preview fails like ValidateScope: one identical error for hidden and
// missing subjects, and a FieldError for a bad scope.
func TestDBPreviewErrorsMatchValidateScope(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	user := testdb.NewUser(t, db, false)
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	shareSite(t, db, hq, user, "readonly")
	far := newDevice(t, db, annex, "annex-sw1")
	b := newBuilder(db)

	_, hiddenErr := b.Preview(ctx, user, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{far}})
	_, missingErr := b.Preview(ctx, user, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{uuid.New()}})
	validateErr := b.ValidateScope(ctx, user, models.ScopeTypeDevices,
		models.ReportScope{DeviceIDs: []uuid.UUID{far}, Metrics: []string{services.MetricIfInBps}})
	var hidden, missing, validate *FieldError
	if !errors.As(hiddenErr, &hidden) || !errors.As(missingErr, &missing) || !errors.As(validateErr, &validate) {
		t.Fatalf("errors %v / %v / %v, want three FieldErrors", hiddenErr, missingErr, validateErr)
	}
	want := FieldError{Field: "scope_data", Message: MsgNotAvailable}
	if *hidden != want || *missing != want || *validate != want {
		t.Errorf("hidden %+v, missing %+v, validate %+v; want all %+v", *hidden, *missing, *validate, want)
	}

	var fe *FieldError
	if _, err := b.Preview(ctx, user, models.ScopeTypePortRoles, models.ReportScope{SiteIDs: []uuid.UUID{hq}}); !errors.As(err, &fe) ||
		fe.Message != `scope_data.roles is required when scope_type is "port_roles"` {
		t.Errorf("no roles: %v", err)
	}
	if _, err := b.Preview(ctx, user, models.ScopeTypeMonitors, models.ReportScope{}); !errors.As(err, &fe) ||
		fe.Message != "scope_type must be one of: ports, port_roles, devices, sites" {
		t.Errorf("a monitor scope: %v", err)
	}
	// Metrics are ignored: the preview sizes a scope before any is chosen.
	if _, err := b.Preview(ctx, user, models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{hq}, Metrics: []string{"nope_metric"}}); err != nil {
		t.Errorf("a preview with an unknown metric: %v", err)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go vet ./internal/netreport/`
Expected: it fails with `b.Preview undefined` and `undefined: MetricChoice`.

- [ ] **Step 3: Implement**

Create `backend/internal/netreport/choices.go`:

```go
package netreport

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// MetricChoice is one metric the Metrics step offers.
type MetricChoice struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Unit   string `json:"unit"`
	Source string `json:"source"` // "builtin" | "profile" | "custom"
}

// Preview is what the editor shows under a scope: its size now, and the
// metrics a report on it can show, with the defaults pre-filled.
type Preview struct {
	Ports    int            `json:"ports"`
	Devices  int            `json:"devices"`
	Capped   bool           `json:"capped"`
	Metrics  []MetricChoice `json:"metrics"`
	Defaults []string       `json:"defaults"`
}

// The editor's pre-filled metrics (spec §1).
var (
	portDefaults    = []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct, services.MetricIfOutUtilPct}
	trafficDefaults = []string{services.MetricIfInBps, services.MetricIfOutBps}
	upsReadings     = []string{services.MetricUPSChargePct, services.MetricUPSLoadPct, services.MetricUPSRuntimeMin, services.MetricUPSOnBattery}
)

// Preview sizes a scope as requester and lists its metrics; scope.Metrics is ignored.
// Ports and Devices are both filled for every scope type; Capped is set when
// the subjects (devices for a devices scope, ports otherwise) exceed
// models.MaxReportSubjects, so the report will keep the busiest of them.
func (b *Builder) Preview(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) (*Preview, error) {
	if !models.IsNetworkScope(scopeType) {
		return nil, &FieldError{Field: "scope_type", Message: msgNetworkScopeTypes}
	}
	if err := scope.ValidateSubjects(scopeType); err != nil {
		return nil, &FieldError{Field: "scope_data", Message: err.Error()}
	}
	res, err := b.resolve(ctx, requester, scopeType, scope)
	if err != nil {
		return nil, err
	}
	if res.unavailable > 0 {
		return nil, notAvailable()
	}
	choices, err := b.choices(ctx, res)
	if err != nil {
		return nil, err
	}
	p := &Preview{Ports: len(res.ports), Devices: len(res.deviceIDs()), Metrics: choices, Defaults: defaults(scopeType, choices)}
	subjects := p.Ports
	if scopeType == models.ScopeTypeDevices {
		p.Ports = res.physicalPorts()
		subjects = p.Devices
	}
	p.Capped = subjects > models.MaxReportSubjects
	return p, nil
}

// choices lists the metrics a report on this scope can show: those with
// stored series in it, plus its fixed defaults so the editor can always name
// them. Text metrics, link speed and (on ports and port roles) device
// metrics are left out.
func (b *Builder) choices(ctx context.Context, res *resolved) ([]MetricChoice, error) {
	keys, err := b.storedMetrics(ctx, res)
	if err != nil {
		return nil, err
	}
	keys = append(keys, fixedDefaults(res.scopeType)...)
	infos, err := b.lookupMetrics(ctx, keys)
	if err != nil {
		return nil, err
	}
	list := make([]metricInfo, 0, len(infos))
	for _, m := range infos {
		if m.reportable() && (m.port() || deviceScope(res.scopeType)) {
			list = append(list, m)
		}
	}
	sort.Slice(list, func(i, j int) bool { return lessMetric(list[i], list[j]) })
	out := make([]MetricChoice, len(list))
	for i, m := range list {
		out[i] = MetricChoice{Key: m.Key, Label: m.Label, Unit: m.Unit, Source: m.Source}
	}
	return out, nil
}

// storedMetrics lists the metrics with series in the scope: on its ports for
// a ports or port_roles scope, on its devices otherwise.
func (b *Builder) storedMetrics(ctx context.Context, res *resolved) ([]string, error) {
	var keys []string
	var err error
	q := b.db.WithContext(ctx)
	if deviceScope(res.scopeType) {
		if ids := res.deviceIDs(); len(ids) > 0 {
			err = q.Raw(`SELECT DISTINCT metric FROM metrics.series WHERE device_id IN ?`, ids).Scan(&keys).Error
		}
	} else if ids := res.portIDs(); len(ids) > 0 {
		err = q.Raw(`SELECT DISTINCT metric FROM metrics.series WHERE interface_id IN ?`, ids).Scan(&keys).Error
	}
	if err != nil {
		return nil, fmt.Errorf("listing the scope's metrics: %w", err)
	}
	return keys, nil
}

// lessMetric orders built-in metrics first, in catalogue order, then profile
// metrics by profile name and position.
func lessMetric(a, b metricInfo) bool {
	if (a.Source == sourceBuiltin) != (b.Source == sourceBuiltin) {
		return a.Source == sourceBuiltin
	}
	if a.profile != b.profile {
		return a.profile < b.profile
	}
	if a.order != b.order {
		return a.order < b.order
	}
	return a.Key < b.Key
}

// fixedDefaults are the defaults that do not depend on what the devices
// have: the port metrics for ports and port roles, traffic for sites (and
// the devices scope's fallback).
func fixedDefaults(scopeType string) []string {
	if scopeType == models.ScopeTypePorts || scopeType == models.ScopeTypePortRoles {
		return portDefaults
	}
	return trafficDefaults
}

// defaults is the editor's pre-filled list (spec §1). Devices get their
// profile metrics, then the UPS readings any of them has, at most 10; or
// traffic in and out (as device totals) when neither applies.
func defaults(scopeType string, choices []MetricChoice) []string {
	if scopeType != models.ScopeTypeDevices {
		return slices.Clone(fixedDefaults(scopeType))
	}
	var out []string
	for _, c := range choices {
		if c.Source != sourceBuiltin {
			out = append(out, c.Key)
		}
	}
	for _, k := range upsReadings {
		if slices.ContainsFunc(choices, func(c MetricChoice) bool { return c.Key == k }) {
			out = append(out, k)
		}
	}
	if len(out) > models.MaxReportMetrics {
		out = out[:models.MaxReportMetrics]
	}
	if len(out) == 0 {
		return slices.Clone(trafficDefaults)
	}
	return out
}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./internal/netreport/`
Expected: no output.

Run: `./scripts/test-db.sh -run 'TestDBPreview|TestDBValidateScope|TestDBResolve' -v`
Expected: PASS, including `TestDBPreviewChoicesAndDefaults`, `TestDBPreviewCapsAbove500` and `TestDBPreviewErrorsMatchValidateScope`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/netreport/choices.go backend/internal/netreport/choices_db_test.go
git commit -m "feat(reports): metric choices, defaults and the scope preview

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7a: Report figures — pairing, totals, change, ranking and tiles

Task 7 of the skeleton is split in two, so that each half fits one reviewer gate:
- 7a: the report data types and the pure figure rules, unit-tested with hand-computed numbers.
- 7b: `Build`, which reads the database and assembles the report, DB-tested.

**Files:**
- Modify: `backend/internal/services/report_metrics_data.go` (add the report data types)
- Create: `backend/internal/netreport/figures.go`
- Test: `backend/internal/netreport/figures_test.go` (new, unit)

**Interfaces:**
- Consumes:
  - From Task 3: `services.SeriesStat` and `Coverage()`.
  - From Task 4: `services.ChartPoint`.
  - From Task 5: `metricInfo`, its `additive()`, and `builtinInfo`.
- Produces (services, contract; the `ScopeType`, `Ports` and `Devices` fields are listed under Contract changes):
  - `MetricsReportData`, `MetricsTile`, `MetricsTable`, `MetricsRow`, `RowStats`, `HotPort`, `MetricsChart`, `ChartLine`.
- Produces (netreport, package-internal, for Task 7b):
  - Constants: `lowCoverage = 90.0`, `hotThreshold = 80.0`; tile kinds `kindTraffic = "traffic"`, `kindPercent = "percent"`, `kindOther = "other"`.
  - `type group struct{ title string; in metricInfo; out *metricInfo }`, with `metrics() []metricInfo`, `unit() string` and `billable() bool`.
  - `func groupMetrics(ms []metricInfo) []group`.
  - `func figures(st services.SeriesStat, unit string) *services.RowStats`.
  - `func busy(in, out *services.RowStats, unit string) (float64, bool)`, `func average(in, out *services.RowStats) (float64, bool)`, `func basis(in, out *services.RowStats, unit string) (float64, bool)`.
  - `func change(cur, prev float64, hasCur, hasPrev bool) (*float64, bool)`.
  - `func rankOf(g group, st [2]services.SeriesStat) (float64, bool)`.
  - `func lineFigures(g group, name string, cur, prev [2]services.SeriesStat) services.MetricsRow`.
  - `func tileFor(g group, cur, prev [2]services.SeriesStat) services.MetricsTile`.
  - `type candidate struct{ subject, name string; value float64; ok bool }`, `func keepBusiest(cands []candidate, limit int) (map[string]bool, int)`.
  - `func lessBusy(aOK bool, aValue float64, aName string, bOK bool, bValue float64, bName string) bool`.
  - `func isBusy(g group) bool`, `func hotPort(g group, name string, cur [2]services.SeriesStat) (services.HotPort, bool)`.
  - The test helpers `near(a, b float64) bool`, `info(t, key) metricInfo` and `full(avg, p95, bucketSum float64) services.SeriesStat`.

Index convention: in every `[2]services.SeriesStat`, index 0 is the in side or the single metric, and index 1 is the out side.

The rules (spec §2), which these functions own:
- Pairing:
  - `if_in_bps`/`if_out_bps` pair as "Traffic", the util metrics as "Busy", the errors as "Errors" and the discards as "Discards".
  - Two metrics pair only when both are in the report. The pair takes the place of whichever of the two comes first.
  - Any other metric is a single table titled with its label.
- Totals:
  - `bps` gives bytes: `BucketSum × 300 / 8`.
  - `per_min` gives a count: `BucketSum × 5`.
  - `bool` gives `TrueSeconds = BucketSum × 300`. A bool's `Avg` is the share of time true (0-1).
- Ranking figure (`busy`):
  - the 95th;
  - for a pair, the busier direction's 95th (for traffic this is the billable 95th, max(95th in, 95th out));
  - for a bool, the share of time true.
- Change basis: the busy figure for `bps`, otherwise the average (for a pair, the larger of the two).
- Change itself:
  - percent change;
  - "new" when the previous period had no data or was zero;
  - zero to zero is 0%;
  - no change without current data.
- Coverage: the worse side's share of buckets with data. A line with data but under 90% is flagged.
- Tiles (scope combined):
  - `traffic` (`bps`): First is the busy figure, Second the bytes in + out.
  - `percent` (`%`): First is the average, Second the 95th.
  - `other`: First is the average.
  - For a pair, the busier direction stands for both.
- Order: lines with data first, busiest first, ties by name. The cap keeps the first `limit` subjects in that order, with ties finally broken by subject id.
- Running hot: a 95th busy at or above 80% in either direction. A port with no busy data is never hot.

- [ ] **Step 1: Write the failing unit tests**

Create `backend/internal/netreport/figures_test.go`:

```go
package netreport

import (
	"math"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func info(t *testing.T, key string) metricInfo {
	t.Helper()
	m, ok := builtinInfo(key)
	if !ok {
		t.Fatalf("%s is not a built-in metric", key)
	}
	return m
}

// full is one side's statistics with all 12 of its buckets covered.
func full(avg, p95, bucketSum float64) services.SeriesStat {
	return services.SeriesStat{Avg: avg, P95: p95, BucketSum: bucketSum, Buckets: 12, Expected: 12}
}

func TestGroupMetrics(t *testing.T) {
	cpu := metricInfo{Key: "core_cpu", Label: "CPU", Unit: "%", Source: sourceProfile}
	got := groupMetrics([]metricInfo{info(t, services.MetricIfOutBps), cpu, info(t, services.MetricIfInBps), info(t, services.MetricIfInUtilPct)})
	if len(got) != 3 {
		t.Fatalf("got %d tables, want 3: %+v", len(got), got)
	}
	if got[0].title != "Traffic" || got[0].in.Key != services.MetricIfInBps || got[0].out == nil ||
		got[0].out.Key != services.MetricIfOutBps || !got[0].billable() || got[0].unit() != "bps" {
		t.Errorf("first table = %+v, want the traffic pair where the out side stood", got[0])
	}
	if got[1].title != "CPU" || got[1].out != nil {
		t.Errorf("second table = %+v, want CPU alone", got[1])
	}
	if got[2].title != "Busy in" || got[2].out != nil || got[2].billable() {
		t.Errorf("third table = %+v, want busy in alone (its out side is not in the report)", got[2])
	}
	errs := groupMetrics([]metricInfo{info(t, services.MetricIfInErrorsPM), info(t, services.MetricIfOutErrorsPM)})
	if len(errs) != 1 || errs[0].title != "Errors" || errs[0].billable() || len(errs[0].metrics()) != 2 {
		t.Errorf("errors = %+v, want one Errors pair without a billable column", errs)
	}
}

func TestFigures(t *testing.T) {
	// Task 3's series: bucket sum 205, so 205 x 300 / 8 = 7687.5 bytes.
	st := services.SeriesStat{Avg: 390.0 / 9, Min: 0, Peak: 100, P95: 66, BucketSum: 205, Buckets: 5, Expected: 6}
	bps := figures(st, "bps")
	if bps == nil || bps.Total == nil || !near(*bps.Total, 7687.5) || bps.TrueSeconds != nil || bps.P95 != 66 || bps.Peak != 100 || bps.Min != 0 {
		t.Errorf("bps = %+v, want 7687.5 bytes", bps)
	}
	// 12.4 errors a minute summed over buckets, x 5 minutes = 62 errors.
	if pm := figures(services.SeriesStat{BucketSum: 12.4, Buckets: 3}, "per_min"); pm == nil || pm.Total == nil || !near(*pm.Total, 62) {
		t.Errorf("per_min = %+v, want a total of 62", pm)
	}
	// "on battery 2 h 13 m": a bucket-share sum of 26.6 is 26.6 x 300 s = 7980 s.
	if b := figures(services.SeriesStat{Avg: 0.1, BucketSum: 26.6, Buckets: 50}, "bool"); b == nil || b.TrueSeconds == nil ||
		!near(*b.TrueSeconds, 7980) || b.Total != nil {
		t.Errorf("bool = %+v, want 7980 seconds true", b)
	}
	if pct := figures(st, "%"); pct == nil || pct.Total != nil || pct.TrueSeconds != nil {
		t.Errorf("%% = %+v, want no total", pct)
	}
	if none := figures(services.SeriesStat{Expected: 6}, "bps"); none != nil {
		t.Errorf("no data = %+v, want nil", none)
	}
}

func TestChange(t *testing.T) {
	ptr := func(v float64) *float64 { return &v }
	cases := []struct {
		name            string
		cur, prev       float64
		hasCur, hasPrev bool
		want            *float64
		isNew           bool
	}{
		{"up 30%", 130, 100, true, true, ptr(30), false},
		{"down 50%", 50, 100, true, true, ptr(-50), false},
		{"from zero", 10, 0, true, true, nil, true},
		{"zero to zero", 0, 0, true, true, ptr(0), false},
		{"no data before", 10, 0, true, false, nil, true},
		{"no data now", 0, 100, false, true, nil, false},
	}
	for _, c := range cases {
		got, isNew := change(c.cur, c.prev, c.hasCur, c.hasPrev)
		if isNew != c.isNew || (got == nil) != (c.want == nil) || (got != nil && !near(*got, *c.want)) {
			t.Errorf("%s: change %v, new %v; want %v, %v", c.name, got, isNew, c.want, c.isNew)
		}
	}
}

// A traffic pair's line, by hand: billable 95th = max(300, 500) = 500, up from
// max(200, 250) = 250, so +100%. Bytes 3000 x 300 / 8 = 112500 in and
// 5000 x 300 / 8 = 187500 out. Coverage is the worse side's: 11 of 12 = 91.67%.
func TestLineFiguresTrafficPair(t *testing.T) {
	out := info(t, services.MetricIfOutBps)
	g := group{title: "Traffic", in: info(t, services.MetricIfInBps), out: &out}
	cur := [2]services.SeriesStat{full(250, 300, 3000), {Avg: 450, P95: 500, BucketSum: 5000, Buckets: 11, Expected: 12}}
	prev := [2]services.SeriesStat{full(150, 200, 1800), full(200, 250, 2400)}
	r := lineFigures(g, "core-sw1 · Gi1", cur, prev)
	if r.Name != "core-sw1 · Gi1" || r.NoData || r.Billable == nil || *r.Billable != 500 || r.Change == nil || !near(*r.Change, 100) || r.New {
		t.Errorf("line = %+v, want billable 500, +100%%", r)
	}
	if r.In == nil || r.In.Total == nil || !near(*r.In.Total, 112500) || r.Out == nil || r.Out.Total == nil || !near(*r.Out.Total, 187500) {
		t.Errorf("totals = %+v / %+v, want 112500 and 187500 bytes", r.In, r.Out)
	}
	if !near(r.Coverage, 1100.0/12) || r.LowCoverage {
		t.Errorf("coverage = %v (low %v), want 91.67%%, not flagged", r.Coverage, r.LowCoverage)
	}
}

// A percentage alone: its change is on the average, and an average of 0
// before is "new", never a division by zero.
func TestLineFiguresFromZeroIsNew(t *testing.T) {
	g := group{title: "Busy in", in: info(t, services.MetricIfInUtilPct)}
	r := lineFigures(g, "x", [2]services.SeriesStat{full(40, 70, 480)}, [2]services.SeriesStat{full(0, 0, 0)})
	if r.Billable != nil || r.Change != nil || !r.New || r.In == nil || r.In.Avg != 40 || r.In.Total != nil || r.Out != nil {
		t.Errorf("line = %+v, want 40%% average, new", r)
	}
	if r.Coverage != 100 || r.LowCoverage {
		t.Errorf("coverage = %v, want 100", r.Coverage)
	}
	// 10 of 12 buckets is 83%: flagged. No data before: new.
	low := lineFigures(g, "x", [2]services.SeriesStat{{Avg: 40, P95: 70, Buckets: 10, Expected: 12}}, [2]services.SeriesStat{})
	if !low.LowCoverage || !low.New || low.Change != nil {
		t.Errorf("low = %+v, want flagged and new", low)
	}
}

func TestLineFiguresNoData(t *testing.T) {
	out := info(t, services.MetricIfOutUtilPct)
	g := group{title: "Busy", in: info(t, services.MetricIfInUtilPct), out: &out}
	r := lineFigures(g, "core-sw1 · Gi3", [2]services.SeriesStat{{Expected: 12}, {Expected: 12}},
		[2]services.SeriesStat{full(10, 10, 120), full(10, 10, 120)})
	if !r.NoData || r.In != nil || r.Out != nil || r.Change != nil || r.New || r.LowCoverage || r.Billable != nil {
		t.Errorf("line = %+v, want no data and nothing else", r)
	}
}

func TestTileFor(t *testing.T) {
	outBps := info(t, services.MetricIfOutBps)
	traffic := group{title: "Traffic", in: info(t, services.MetricIfInBps), out: &outBps}
	// 1300 bps in and 360 out in each of 12 buckets; 900 and 160 the period before.
	// Billable 95th 1300; bytes (15600 + 4320) x 300 / 8 = 747000; (1300 - 900) / 900 = +44.44%.
	tile := tileFor(traffic, [2]services.SeriesStat{full(1300, 1300, 15600), full(360, 360, 4320)},
		[2]services.SeriesStat{full(900, 900, 10800), full(160, 160, 1920)})
	if tile.Kind != "traffic" || tile.Label != "Traffic" || tile.Unit != "bps" || tile.First != 1300 || tile.Second == nil ||
		!near(*tile.Second, 747000) || tile.Change == nil || !near(*tile.Change, 400.0/9) || tile.New || tile.NoData {
		t.Errorf("traffic tile = %+v", tile)
	}

	outUtil := info(t, services.MetricIfOutUtilPct)
	busyPair := group{title: "Busy", in: info(t, services.MetricIfInUtilPct), out: &outUtil}
	tile = tileFor(busyPair, [2]services.SeriesStat{full(15, 15, 180), full(45, 45, 540)}, [2]services.SeriesStat{})
	if tile.Kind != "percent" || tile.First != 45 || tile.Second == nil || *tile.Second != 45 || tile.Change != nil || !tile.New {
		t.Errorf("busy tile = %+v, want average 45, 95th 45, new", tile)
	}

	temp := group{title: "Room temperature", in: metricInfo{Key: "lab_temp", Label: "Room temperature", Unit: "°C"}}
	tile = tileFor(temp, [2]services.SeriesStat{full(30, 31, 360)}, [2]services.SeriesStat{full(20, 21, 240)})
	if tile.Kind != "other" || tile.First != 30 || tile.Second != nil || tile.Change == nil || !near(*tile.Change, 50) {
		t.Errorf("temperature tile = %+v, want average 30, +50%%", tile)
	}

	if none := tileFor(traffic, [2]services.SeriesStat{{Expected: 12}, {Expected: 12}}, [2]services.SeriesStat{}); !none.NoData {
		t.Errorf("no data tile = %+v", none)
	}
}

// A bool metric ranks by its share of time true: its 95th is only 0 or 1.
func TestRankOfBool(t *testing.T) {
	g := group{title: "On battery", in: info(t, services.MetricUPSOnBattery)}
	if v, ok := rankOf(g, [2]services.SeriesStat{{Avg: 0.5625, P95: 1, Buckets: 12, Expected: 12}}); !ok || v != 0.5625 {
		t.Errorf("rank = %v, %v; want 0.5625", v, ok)
	}
	if _, ok := rankOf(g, [2]services.SeriesStat{{Expected: 12}}); ok {
		t.Error("a line without data was ranked")
	}
}

func TestKeepBusiest(t *testing.T) {
	cands := []candidate{{"p:a", "a", 10, true}, {"p:b", "b", 30, true}, {"p:c", "c", 0, false}, {"p:e", "e", 20, true}, {"p:d", "d", 20, true}}
	keep, cut := keepBusiest(cands, 3)
	if cut != 2 || len(keep) != 3 || !keep["p:b"] || !keep["p:d"] || !keep["p:e"] {
		t.Errorf("keep %v, cut %d; want b, d, e kept (d before e on the tie) and 2 cut", keep, cut)
	}
	if keep, cut := keepBusiest(cands, 500); cut != 0 || len(keep) != 5 {
		t.Errorf("under the cap: keep %v, cut %d", keep, cut)
	}
}

func TestHotPort(t *testing.T) {
	outUtil := info(t, services.MetricIfOutUtilPct)
	pair := group{title: "Busy", in: info(t, services.MetricIfInUtilPct), out: &outUtil}
	if !isBusy(pair) {
		t.Error("the busy pair is not recognised")
	}
	h, hot := hotPort(pair, "core-sw1 · Gi1", [2]services.SeriesStat{full(10, 10, 120), full(85, 85, 1020)})
	if !hot || h != (services.HotPort{Name: "core-sw1 · Gi1", P95In: 10, P95Out: 85}) {
		t.Errorf("hot = %v, %+v; want running hot at 85%% out", hot, h)
	}
	// A port of unknown speed has no busy data, so it is never hot.
	if _, hot := hotPort(pair, "x", [2]services.SeriesStat{{Expected: 12}, {Expected: 12}}); hot {
		t.Error("a port without busy data is running hot")
	}
	outOnly := group{title: "Busy out", in: info(t, services.MetricIfOutUtilPct)}
	if h, hot := hotPort(outOnly, "y", [2]services.SeriesStat{full(80, 80, 960)}); !hot || h.P95Out != 80 || h.P95In != 0 {
		t.Errorf("out only = %v, %+v; want hot at exactly 80%% out", hot, h)
	}
	inOnly := group{title: "Busy in", in: info(t, services.MetricIfInUtilPct)}
	if _, hot := hotPort(inOnly, "z", [2]services.SeriesStat{full(79.9, 79.9, 958.8)}); hot {
		t.Error("79.9% is running hot")
	}
	if isBusy(group{title: "Traffic in", in: info(t, services.MetricIfInBps)}) {
		t.Error("traffic is not the busy table")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/netreport/ -run 'TestGroupMetrics|TestFigures|TestChange|TestLineFigures|TestTileFor|TestRankOfBool|TestKeepBusiest|TestHotPort' -v`
Expected: the build fails with `undefined: groupMetrics`, `undefined: group` and `undefined: services.HotPort`.

- [ ] **Step 3: Add the report data types**

Append to `backend/internal/services/report_metrics_data.go`:

```go
// MetricsReportData is everything a Metrics report shows. netreport.Builder
// fills it; the PDF renderer and the email summary draw it. Notes for the
// headline (CappedOut, Unavailable, Skipped, LowCoverage) are fields only:
// nothing copies them into ReportData.Warnings.
type MetricsReportData struct {
	ScopeType   string // the report's scope_type: what Unavailable and CappedOut count
	ScopeLabel  string // "WAN, Uplink ports at HQ, Annex"
	PrevStart   time.Time
	PrevEnd     time.Time
	RankLabel   string         // label of the ranking metric, e.g. "Traffic"
	Ports       int            // ports in the report, after the cap
	Devices     int            // devices in the report, after the cap
	Tiles       []MetricsTile  // one per table, whole scope combined
	Tables      []MetricsTable // one per metric or in/out pair, in metric order
	Busiest     []string       // up to 5 row names, busiest first
	RunningHot  []HotPort
	Charts      []MetricsChart // one per table, whole scope combined
	RowCharts   []MetricsChart // up to 10 busiest rows, first table's metric
	Rows        int            // rows included (≤ 500 subjects)
	CappedOut   int            // subjects left out by the 500 cap
	Unavailable int            // subjects dropped as unavailable to the owner or deleted
	Skipped     []string       // metric keys no longer defined
	LowCoverage int            // rows under 90% coverage
	Empty       bool           // every subject unavailable: one-page PDF
	NoData      bool           // no row has any data in the period
}

// MetricsTile is one headline tile: a table's figures for the whole scope.
type MetricsTile struct {
	Label  string   // "Traffic", "Busy", "CPU", ...
	Unit   string   // "bps", "%", ...
	Kind   string   // "traffic" | "percent" | "other"
	First  float64  // traffic: billable 95th; percent: average; other: average
	Second *float64 // traffic: total bytes; percent: 95th; other: nil
	Change *float64 // percent change of First vs the previous period; nil with New or no data
	New    bool     // no data (or zero) in the previous period
	NoData bool
}

// MetricsTable is one table: a metric, or an in/out pair.
type MetricsTable struct {
	Title    string       // "Traffic", "Busy", "Errors", or the metric label
	Unit     string       // the metric's unit
	Paired   bool         // in/out columns
	Billable bool         // traffic pair: Billable 95th column
	HasTotal bool         // unit is bps or per_min
	Rows     []MetricsRow // busiest first; on a sites scope the site totals lead
}

// MetricsRow is one line of a table.
type MetricsRow struct {
	Name        string    // "core-sw1 · Gi1/0/1 (uplink to annex)" or "core-sw1 · Switch 1"
	In          *RowStats // single metric: In only; nil without data
	Out         *RowStats // nil without data, and for a single metric
	Billable    *float64  // traffic pair: max(95th in, 95th out)
	Change      *float64  // on the billable 95th (traffic), else on the average
	New         bool      // no data, or zero, in the previous period
	Coverage    float64   // 0-100
	LowCoverage bool      // has data, but for under 90% of the period
	NoData      bool
}

// RowStats is one side of a line over the period.
type RowStats struct {
	Avg, Min, Peak, P95 float64  // a bool metric's Avg is its share of time true (0-1)
	Total               *float64 // bytes for bps, count for per_min, nil otherwise
	TrueSeconds         *float64 // bool metrics: seconds true
}

// HotPort is a port whose 95th busy reached 80% in either direction.
type HotPort struct {
	Name          string
	P95In, P95Out float64 // percent; 0 where that direction has no data
}

// MetricsChart is one chart: a table for the whole scope, or one busy line.
type MetricsChart struct {
	Title     string
	Unit      string
	Lines     []ChartLine // 1 line, or 2 for an in/out pair
	Reference *float64    // the 95th (billable for traffic)
}

// ChartLine is one line of a chart.
type ChartLine struct {
	Label  string
	Points []ChartPoint
}
```

- [ ] **Step 4: Implement the figures**

Create `backend/internal/netreport/figures.go`:

```go
package netreport

import (
	"math"
	"slices"
	"sort"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

const (
	// lowCoverage: a line with data for less of the period than this (%) is flagged.
	lowCoverage = 90.0
	// hotThreshold: a port whose 95th busy reaches this (%) in either
	// direction is running hot.
	hotThreshold = 80.0
)

// Tile kinds (services.MetricsTile.Kind).
const (
	kindTraffic = "traffic"
	kindPercent = "percent"
	kindOther   = "other"
)

// pairs maps each in metric to its out side and the pair's title.
var pairs = map[string]struct{ out, title string }{
	services.MetricIfInBps:        {services.MetricIfOutBps, "Traffic"},
	services.MetricIfInUtilPct:    {services.MetricIfOutUtilPct, "Busy"},
	services.MetricIfInErrorsPM:   {services.MetricIfOutErrorsPM, "Errors"},
	services.MetricIfInDiscardsPM: {services.MetricIfOutDiscardsPM, "Discards"},
}

// group is one table of the report: an in/out pair, or a single metric.
type group struct {
	title string
	in    metricInfo  // the single metric, or the pair's in side
	out   *metricInfo // the pair's out side; nil for a single metric
}

func (g group) metrics() []metricInfo {
	if g.out == nil {
		return []metricInfo{g.in}
	}
	return []metricInfo{g.in, *g.out}
}

func (g group) unit() string { return g.in.Unit }

// billable reports the traffic pair, whose table has a Billable 95th column.
func (g group) billable() bool { return g.out != nil && g.in.Unit == "bps" }

// groupMetrics turns the report's metrics into its tables, in metric order:
// an in/out pair whose two sides are both listed becomes one table, standing
// where the first of the two stands; anything else is a table of its own.
func groupMetrics(ms []metricInfo) []group {
	listed := make(map[string]metricInfo, len(ms))
	for _, m := range ms {
		listed[m.Key] = m
	}
	inOf := map[string]string{}
	for in, p := range pairs {
		inOf[p.out] = in
	}
	done := map[string]bool{}
	var out []group
	for _, m := range ms {
		if done[m.Key] {
			continue
		}
		inKey, outKey := m.Key, ""
		if p, ok := pairs[m.Key]; ok {
			outKey = p.out
		} else if in, ok := inOf[m.Key]; ok {
			inKey, outKey = in, m.Key
		}
		inM, hasIn := listed[inKey]
		outM, hasOut := listed[outKey]
		if outKey != "" && hasIn && hasOut {
			o := outM
			out = append(out, group{title: pairs[inKey].title, in: inM, out: &o})
			done[inKey], done[outKey] = true, true
			continue
		}
		out = append(out, group{title: m.Label, in: m})
		done[m.Key] = true
	}
	return out
}

// figures turns one side's statistics into a line's numbers; nil without
// data. A rate gets its total: bytes for bps (Σ bucket average × 300 s ÷ 8),
// a count for per_min (Σ bucket average × 5 min). A bool metric gets the
// seconds it was true (Σ bucket share × 300 s); its Avg is the share.
func figures(st services.SeriesStat, unit string) *services.RowStats {
	if st.Buckets == 0 {
		return nil
	}
	rs := &services.RowStats{Avg: st.Avg, Min: st.Min, Peak: st.Peak, P95: st.P95}
	switch unit {
	case "bps":
		v := st.BucketSum * 300 / 8
		rs.Total = &v
	case "per_min":
		v := st.BucketSum * 5
		rs.Total = &v
	case "bool":
		v := st.BucketSum * 300
		rs.TrueSeconds = &v
	}
	return rs
}

// larger picks a figure from each side that has data and keeps the larger.
func larger(in, out *services.RowStats, pick func(*services.RowStats) float64) (float64, bool) {
	switch {
	case in == nil && out == nil:
		return 0, false
	case in == nil:
		return pick(out), true
	case out == nil:
		return pick(in), true
	}
	return math.Max(pick(in), pick(out)), true
}

// busy is the figure lines are ranked and sorted by: the 95th; for a pair the
// busier direction's (for traffic, the billable 95th = max(95th in, 95th
// out)); for a bool metric the share of time true, its 95th being only 0 or 1.
func busy(in, out *services.RowStats, unit string) (float64, bool) {
	if unit == "bool" {
		return larger(in, out, func(s *services.RowStats) float64 { return s.Avg })
	}
	return larger(in, out, func(s *services.RowStats) float64 { return s.P95 })
}

// average is a line's average; for a pair the busier direction's.
func average(in, out *services.RowStats) (float64, bool) {
	return larger(in, out, func(s *services.RowStats) float64 { return s.Avg })
}

// basis is the figure a line's change is worked out on: the busy figure (the
// billable 95th) for traffic, the average otherwise.
func basis(in, out *services.RowStats, unit string) (float64, bool) {
	if unit == "bps" {
		return busy(in, out, unit)
	}
	return average(in, out)
}

// change compares a figure with the previous period's: the change in
// percent, or isNew when the previous period had no data or was zero (never
// a division by zero). Zero to zero is no change. Without current data there
// is neither.
func change(cur, prev float64, hasCur, hasPrev bool) (*float64, bool) {
	switch {
	case !hasCur:
		return nil, false
	case !hasPrev:
		return nil, true
	case prev == 0 && cur == 0:
		v := 0.0
		return &v, false
	case prev == 0:
		return nil, true
	}
	v := (cur - prev) / prev * 100
	return &v, false
}

// sides is a line's figures in one period; out is nil for a single metric.
func sides(g group, st [2]services.SeriesStat) (in, out *services.RowStats) {
	in = figures(st[0], g.unit())
	if g.out != nil {
		out = figures(st[1], g.unit())
	}
	return in, out
}

// rankOf is a line's ranking figure in a period (see busy); false without data.
func rankOf(g group, st [2]services.SeriesStat) (float64, bool) {
	in, out := sides(g, st)
	return busy(in, out, g.unit())
}

// lineFigures is one table line's numbers from its statistics in the period
// (cur) and the one before (prev).
func lineFigures(g group, name string, cur, prev [2]services.SeriesStat) services.MetricsRow {
	unit := g.unit()
	in, out := sides(g, cur)
	r := services.MetricsRow{Name: name, In: in, Out: out, NoData: in == nil && out == nil}
	if r.NoData {
		return r
	}
	if g.billable() {
		v, _ := busy(in, out, unit)
		r.Billable = &v
	}
	pin, pout := sides(g, prev)
	c, _ := basis(in, out, unit)
	p, hasPrev := basis(pin, pout, unit)
	r.Change, r.New = change(c, p, true, hasPrev)
	r.Coverage = cur[0].Coverage()
	if g.out != nil {
		r.Coverage = math.Min(r.Coverage, cur[1].Coverage())
	}
	r.LowCoverage = r.Coverage < lowCoverage
	return r
}

func kindOf(unit string) string {
	switch unit {
	case "bps":
		return kindTraffic
	case "%":
		return kindPercent
	}
	return kindOther
}

func totalOf(s *services.RowStats) float64 {
	if s == nil || s.Total == nil {
		return 0
	}
	return *s.Total
}

// tileFor is a table's headline tile from the whole scope's combined
// statistics: traffic shows its 95th (billable for a pair) and the bytes
// moved; a percentage its average and 95th; anything else its average. For a
// pair the busier direction stands for both, except the bytes, which add up.
func tileFor(g group, cur, prev [2]services.SeriesStat) services.MetricsTile {
	unit := g.unit()
	t := services.MetricsTile{Label: g.title, Unit: unit, Kind: kindOf(unit)}
	in, out := sides(g, cur)
	if in == nil && out == nil {
		t.NoData = true
		return t
	}
	pin, pout := sides(g, prev)
	var p float64
	var hasPrev bool
	switch t.Kind {
	case kindTraffic:
		t.First, _ = busy(in, out, unit)
		total := totalOf(in) + totalOf(out)
		t.Second = &total
		p, hasPrev = busy(pin, pout, unit)
	case kindPercent:
		t.First, _ = average(in, out)
		p95, _ := busy(in, out, unit)
		t.Second = &p95
		p, hasPrev = average(pin, pout)
	default:
		t.First, _ = average(in, out)
		p, hasPrev = average(pin, pout)
	}
	t.Change, t.New = change(t.First, p, true, hasPrev)
	return t
}

// candidate is a subject (a port or a device) competing for a place in the
// report, by its busiest line in the first table.
type candidate struct {
	subject, name string
	value         float64
	ok            bool // false: no data there, so it ranks after every subject with data
}

// lessBusy orders busiest first: figures with data before those without,
// higher figures first, then by name.
func lessBusy(aOK bool, aValue float64, aName string, bOK bool, bValue float64, bName string) bool {
	if aOK != bOK {
		return aOK
	}
	if aOK && aValue != bValue {
		return aValue > bValue
	}
	return aName < bName
}

// keepBusiest orders the candidates busiest first (no data last, then by
// name and id so the order never wobbles) and keeps the first limit. It
// returns the kept subjects and how many were cut.
func keepBusiest(cands []candidate, limit int) (map[string]bool, int) {
	sorted := slices.Clone(cands)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if lessBusy(a.ok, a.value, a.name, b.ok, b.value, b.name) {
			return true
		}
		if lessBusy(b.ok, b.value, b.name, a.ok, a.value, a.name) {
			return false
		}
		return a.subject < b.subject
	})
	keep := make(map[string]bool, min(limit, len(sorted)))
	for i := 0; i < len(sorted) && i < limit; i++ {
		keep[sorted[i].subject] = true
	}
	return keep, max(0, len(sorted)-limit)
}

// isBusy reports the busy table: the in/out pair, or one direction alone.
func isBusy(g group) bool {
	return g.in.Key == services.MetricIfInUtilPct || g.in.Key == services.MetricIfOutUtilPct
}

// hotPort reports a port running hot: its 95th busy in either direction at
// or above 80%. g is the busy table. A port with no busy data (its speed is
// unknown, so no busy % is ever recorded) is never hot.
func hotPort(g group, name string, cur [2]services.SeriesStat) (services.HotPort, bool) {
	in, out := cur[0], cur[1]
	switch {
	case g.out == nil && g.in.Key == services.MetricIfOutUtilPct:
		in, out = services.SeriesStat{}, cur[0]
	case g.out == nil:
		out = services.SeriesStat{}
	}
	h := services.HotPort{Name: name}
	hot := false
	if in.Buckets > 0 {
		h.P95In = in.P95
		hot = hot || in.P95 >= hotThreshold
	}
	if out.Buckets > 0 {
		h.P95Out = out.P95
		hot = hot || out.P95 >= hotThreshold
	}
	return h, hot
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/netreport/ -run 'TestGroupMetrics|TestFigures|TestChange|TestLineFigures|TestTileFor|TestRankOfBool|TestKeepBusiest|TestHotPort' -v`
Expected: PASS.

Run: `go vet ./internal/netreport/ ./internal/services/`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/services/report_metrics_data.go backend/internal/netreport/figures.go backend/internal/netreport/figures_test.go
git commit -m "feat(reports): metrics report data and its figure rules

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7b: Building the Metrics report data (`netreport.Build`)

**Files:**
- Create: `backend/internal/netreport/build.go` (`Build`, the statistics reads, the cap, the scope label)
- Create: `backend/internal/netreport/assemble.go` (tables, tiles, charts, busiest, running hot)
- Test: `backend/internal/netreport/build_db_test.go` (new)

**Interfaces:**
- Consumes:
  - Task 1: `Report.PreviousPeriod`, `models.MaxReportSubjects`, and the `models.PortRole*` constants.
  - Task 2: `services.ErrReportTooLarge`.
  - Tasks 3-4: `MetricsStore.SeriesStats`, `GroupedStats`, `CombinedStats`, `CombinedSeries`, plus `services.StatsQuery` and `services.CombinedQuery`.
  - Task 5: `resolve`, `resolved` (with `empty()`), `lookupMetrics`, `metricInfo`, `deviceScope`, `loadSeries`, `row`, `rowsFor`, and the fixtures.
  - Task 7a: `group`, `groupMetrics`, `lineFigures`, `tileFor`, `rankOf`, `keepBusiest`, `candidate`, `lessBusy`, `isBusy`, `hotPort`, the data types, and the test helper `near`.
- Produces:
  - `func (b *Builder) Build(ctx context.Context, report *models.Report, requestedBy uuid.UUID, start, end time.Time, loc *time.Location) (*services.MetricsReportData, error)`. It implements `services.NetworkReportBuilder` (Task 8).
  - Errors: a statistics query that hits its statement timeout, or a context past its deadline, returns exactly `services.ErrReportTooLarge`.
  - A run never fails because subjects or metrics went away. Unavailable subjects are counted, and unknown metrics land in `Skipped`.
  - Nothing is written to `ReportData.Warnings`. The headline notes come from the fields.

How `Build` fills the report:
1. Resolve the scope as `requestedBy`.
   - Every chosen subject unavailable → `Empty`, nothing else.
2. Describe the metrics. Unknown ones, ones the report cannot show, and device metrics on a port scope are added to `Skipped`.
   - No metric left → `NoData`.
3. Pair the metrics into tables (`groupMetrics`). The first table's title is `RankLabel`.
4. Lay out each table's lines (`rowsFor`; a pair's sides are matched by line key), and the whole scope's series per side:
   - on a `sites` scope, the site totals' series;
   - otherwise every line's.
5. Read the first table for the period, for every line. Rank the subjects (ports or devices) by their busiest line there, keep the 500 busiest (`keepBusiest`), and drop the cut subjects' lines from every table. Site totals always stay.
6. Read the other tables for the period, and every table for the previous period, for the kept lines only. The scope's combined series (tiles, charts) always covers the whole resolved scope.
   - Each read is, per side: one `SeriesStats` for the single-series lines, one `GroupedStats` for the totals, and one `CombinedStats` for the scope.
7. Assemble:
   - Sort each table: site totals first, then busiest first, no data last.
   - Build the rows (`lineFigures`), the tiles (`tileFor`), and a chart per table with the scope's 95th as reference.
   - Running hot comes from the busy table's port lines.
   - `Busiest` (5) and `RowCharts` (10) are the first table's non-site lines with data, in order.
   - `LowCoverage` counts the distinct lines flagged in any table.
   - `NoData` is set when no line has data.
8. Fill the counts:
   - `Rows`: the subjects kept. `CappedOut`: the subjects cut.
   - `Ports`/`Devices` per scope type:
     - `ports`/`port_roles`: kept ports, and their devices.
     - `devices`: kept devices, and their physical ports.
     - `sites`: kept ports, and the devices in the visible sites.
   - `ScopeLabel`:
     - `ports`: `"<port name>"` for one port, else `"N ports"`.
     - `devices`: `"<device>"` for one device, else `"N devices"`.
     - `port_roles`: `"WAN, Uplink ports at HQ, Annex"`.
     - `sites`: `"All of HQ, Annex"`.
     - Only visible sites are named; none gives `"no available site"`.

- [ ] **Step 1: Write the failing DB tests**

Create `backend/internal/netreport/build_db_test.go`:

```go
package netreport

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// buildWindow is one whole hour two days ago: 12 complete buckets. With a
// rolling report the previous period is the hour before it.
func buildWindow() (time.Time, time.Time) {
	start := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Hour)
	return start, start.Add(time.Hour)
}

// metricsReport is a metrics report with a rolling period, so its previous
// period is the same length right before the start.
func metricsReport(owner uuid.UUID, scopeType string, scope models.ReportScope) *models.Report {
	return &models.Report{ID: uuid.New(), UserID: owner, CreatedBy: owner, Name: "Network", ReportType: models.ReportTypeMetrics,
		ScopeType: scopeType, ScopeData: scope, PeriodKind: models.PeriodRolling, TimeRangeDays: 1}
}

func rowNames(rows []services.MetricsRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

// Review Focus 3: a port of unknown speed has no busy data. It leads the
// traffic ranking on its traffic alone, its busy line says "no data", and it
// is never running hot. Every figure by hand (bps per bucket, 12 buckets):
//
//	       in    out   billable   busy in / out   the hour before
//	Gi1    100   300   300        10 / 85         100 / 150 -> +100%
//	Gi2    200    50   200        20 / 5          none      -> new
//	Gi3   1000    10   1000       (no speed)      800 / 10  -> +25%
func TestDBBuildPortsTrafficBusyAndRunningHot(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	gi1, gi2 := newPort(t, db, sw, 1, "Gi1", gigabit), newPort(t, db, sw, 2, "Gi2", gigabit)
	gi3 := newPort(t, db, sw, 3, "Gi3", 0) // speed unknown: no busy % is ever recorded
	start, end := buildWindow()
	prev := start.Add(-time.Hour)
	write := func(port uuid.UUID, ifIndex int, metric string, now float64, before ...float64) {
		s := portSeries(t, db, sw, port, ifIndex, metric)
		steady(t, db, s, start, 12, now)
		for _, v := range before {
			steady(t, db, s, prev, 12, v)
		}
	}
	write(gi1, 1, services.MetricIfInBps, 100, 100)
	write(gi1, 1, services.MetricIfOutBps, 300, 150)
	write(gi1, 1, services.MetricIfInUtilPct, 10)
	write(gi1, 1, services.MetricIfOutUtilPct, 85)
	write(gi2, 2, services.MetricIfInBps, 200)
	write(gi2, 2, services.MetricIfOutBps, 50)
	write(gi2, 2, services.MetricIfInUtilPct, 20)
	write(gi2, 2, services.MetricIfOutUtilPct, 5)
	write(gi3, 3, services.MetricIfInBps, 1000, 800)
	write(gi3, 3, services.MetricIfOutBps, 10, 10)
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1, gi2, gi3},
		Metrics: []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct, services.MetricIfOutUtilPct}})

	data, err := newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)

	if data.ScopeType != models.ScopeTypePorts || data.ScopeLabel != "3 ports" || data.RankLabel != "Traffic" ||
		!data.PrevStart.Equal(prev) || !data.PrevEnd.Equal(start) {
		t.Errorf("header = %q %q %q %v to %v", data.ScopeType, data.ScopeLabel, data.RankLabel, data.PrevStart, data.PrevEnd)
	}
	if data.Rows != 3 || data.Ports != 3 || data.Devices != 1 || data.CappedOut != 0 || data.Unavailable != 0 ||
		data.LowCoverage != 0 || data.NoData || data.Empty || len(data.Skipped) != 0 {
		t.Errorf("counts = rows %d, ports %d, devices %d, cut %d, unavailable %d, low %d, no data %v, empty %v, skipped %v",
			data.Rows, data.Ports, data.Devices, data.CappedOut, data.Unavailable, data.LowCoverage, data.NoData, data.Empty, data.Skipped)
	}
	if len(data.Tables) != 2 || len(data.Tiles) != 2 {
		t.Fatalf("%d tables, %d tiles; want Traffic and Busy", len(data.Tables), len(data.Tiles))
	}
	traffic, busyTable := data.Tables[0], data.Tables[1]
	if traffic.Title != "Traffic" || !traffic.Paired || !traffic.Billable || !traffic.HasTotal || traffic.Unit != "bps" ||
		busyTable.Title != "Busy" || !busyTable.Paired || busyTable.Billable || busyTable.HasTotal {
		t.Errorf("table headers = %+v / %+v", traffic, busyTable)
	}
	if want := []string{"core-sw1 · Gi3", "core-sw1 · Gi1", "core-sw1 · Gi2"}; !slices.Equal(rowNames(traffic.Rows), want) {
		t.Fatalf("traffic order = %v, want %v (Gi3 has no busy data; its traffic still ranks it first)", rowNames(traffic.Rows), want)
	}
	g3, g1, g2 := traffic.Rows[0], traffic.Rows[1], traffic.Rows[2]
	if *g3.Billable != 1000 || g3.Change == nil || !near(*g3.Change, 25) || g3.New || !near(*g3.In.Total, 1000*12*300/8) {
		t.Errorf("Gi3 = %+v, want billable 1000, +25%%", g3)
	}
	if *g1.Billable != 300 || g1.Change == nil || !near(*g1.Change, 100) || !near(*g1.In.Total, 45000) || !near(*g1.Out.Total, 135000) {
		t.Errorf("Gi1 = %+v, want billable 300, +100%%, 45000 bytes in and 135000 out", g1)
	}
	if *g2.Billable != 200 || g2.Change != nil || !g2.New || g2.Coverage != 100 {
		t.Errorf("Gi2 = %+v, want billable 200 and new", g2)
	}
	if want := []string{"core-sw1 · Gi1", "core-sw1 · Gi2", "core-sw1 · Gi3"}; !slices.Equal(rowNames(busyTable.Rows), want) {
		t.Errorf("busy order = %v, want %v", rowNames(busyTable.Rows), want)
	}
	if gi3Busy := busyTable.Rows[2]; !gi3Busy.NoData || gi3Busy.In != nil || gi3Busy.Out != nil {
		t.Errorf("Gi3 busy = %+v, want no data", gi3Busy)
	}
	if !slices.Equal(data.RunningHot, []services.HotPort{{Name: "core-sw1 · Gi1", P95In: 10, P95Out: 85}}) {
		t.Errorf("running hot = %+v, want Gi1 only", data.RunningHot)
	}
	if want := []string{"core-sw1 · Gi3", "core-sw1 · Gi1", "core-sw1 · Gi2"}; !slices.Equal(data.Busiest, want) {
		t.Errorf("busiest = %v, want %v", data.Busiest, want)
	}
	// The whole scope: 1300 bps in and 360 out; 900 and 160 the hour before.
	if tile := data.Tiles[0]; tile.Kind != "traffic" || tile.First != 1300 || tile.Second == nil || !near(*tile.Second, 747000) ||
		tile.Change == nil || !near(*tile.Change, 400.0/9) {
		t.Errorf("traffic tile = %+v, want 1300, 747000 bytes, +44.44%%", tile)
	}
	// Busy averages over the ports that have it: (10+20)/2 in, (85+5)/2 out.
	if tile := data.Tiles[1]; tile.Kind != "percent" || tile.First != 45 || tile.Second == nil || *tile.Second != 45 || !tile.New {
		t.Errorf("busy tile = %+v, want 45, 45, new", tile)
	}
	if len(data.Charts) != 2 || len(data.Charts[0].Lines) != 2 || data.Charts[0].Reference == nil ||
		*data.Charts[0].Reference != 1300 || len(data.Charts[0].Lines[0].Points) != 12 || data.Charts[0].Lines[0].Points[0].V != 1300 {
		t.Errorf("charts = %+v", data.Charts)
	}
	if len(data.RowCharts) != 3 || data.RowCharts[0].Title != "core-sw1 · Gi3" || data.RowCharts[0].Reference == nil ||
		*data.RowCharts[0].Reference != 1000 || len(data.RowCharts[0].Lines) != 2 || data.RowCharts[0].Lines[0].Label != "Traffic in" ||
		len(data.RowCharts[0].Lines[0].Points) != 12 || data.RowCharts[0].Lines[0].Points[0].V != 1000 {
		t.Errorf("row charts = %+v", data.RowCharts)
	}
}

// A site total sums traffic but averages busy %, over the physical ports
// only: a VLAN interface the user collects has its own line, and no part in
// the total.
func TestDBBuildSiteTotalsSumTrafficAndAverageBusy(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw1, sw2 := newDevice(t, db, hq, "sw1"), newDevice(t, db, hq, "sw2")
	g1, g2 := newPort(t, db, sw1, 1, "Gi1", gigabit), newPort(t, db, sw2, 1, "Gi1", gigabit)
	vlan := newVirtualPort(t, db, sw1, 100, "Vlan10", 53)
	start, end := buildWindow()
	for _, w := range []struct {
		device, port uuid.UUID
		ifIndex      int
		metric       string
		v            float64
	}{
		{sw1, g1, 1, services.MetricIfInBps, 100},
		{sw2, g2, 1, services.MetricIfInBps, 300},
		{sw1, vlan, 100, services.MetricIfInBps, 5000},
		{sw1, g1, 1, services.MetricIfInUtilPct, 40},
		{sw2, g2, 1, services.MetricIfInUtilPct, 60},
	} {
		steady(t, db, portSeries(t, db, w.device, w.port, w.ifIndex, w.metric), start, 12, w.v)
	}
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypeSites, models.ReportScope{SiteIDs: []uuid.UUID{hq},
		Metrics: []string{services.MetricIfInBps, services.MetricIfInUtilPct}})

	data, err := newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)

	traffic := data.Tables[0]
	if want := []string{"HQ (site total)", "sw1 · Vlan10", "sw2 · Gi1", "sw1 · Gi1"}; traffic.Title != "Traffic in" ||
		!slices.Equal(rowNames(traffic.Rows), want) {
		t.Fatalf("traffic = %q %v, want %v", traffic.Title, rowNames(traffic.Rows), want)
	}
	if site := traffic.Rows[0]; site.In == nil || site.In.P95 != 400 || site.In.Total == nil || !near(*site.In.Total, 400*12*300/8) {
		t.Errorf("site traffic = %+v, want 100 + 300 = 400 bps (not 5400: the VLAN is not physical)", site.In)
	}
	if tile := data.Tiles[0]; tile.Kind != "traffic" || tile.First != 400 {
		t.Errorf("traffic tile = %+v, want 400", tile)
	}
	busyTable := data.Tables[1]
	if want := []string{"HQ (site total)", "sw2 · Gi1", "sw1 · Gi1", "sw1 · Vlan10"}; !slices.Equal(rowNames(busyTable.Rows), want) {
		t.Fatalf("busy = %v, want %v", rowNames(busyTable.Rows), want)
	}
	if site := busyTable.Rows[0]; site.In == nil || site.In.Avg != 50 || site.In.P95 != 50 || site.In.Total != nil {
		t.Errorf("site busy = %+v, want (40 + 60) / 2 = 50, no total", site.In)
	}
	if tile := data.Tiles[1]; tile.Kind != "percent" || tile.First != 50 {
		t.Errorf("busy tile = %+v, want 50", tile)
	}
	if data.ScopeLabel != "All of HQ" || data.Rows != 3 || data.Ports != 3 || data.Devices != 2 || len(data.RunningHot) != 0 {
		t.Errorf("label %q, rows %d, ports %d, devices %d, hot %v", data.ScopeLabel, data.Rows, data.Ports, data.Devices, data.RunningHot)
	}
	if want := []string{"sw1 · Vlan10", "sw2 · Gi1", "sw1 · Gi1"}; !slices.Equal(data.Busiest, want) {
		t.Errorf("busiest = %v, want %v (site totals are not listed)", data.Busiest, want)
	}
}

// A port_roles scope of 502 ports keeps the 500 busiest by the first metric
// and says how many it left out; the headline still covers them all.
func TestDBBuildCapKeepsTheBusiest500(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	start, end := buildWindow()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, if_type, speed_bps, present, collect_default, role)
		SELECT ?, g, 'Gi' || g, 6, 1000000000, true, true, 'wan' FROM generate_series(1, 502) g`, sw)
	testdb.Exec(t, db, `INSERT INTO metrics.series (device_id, metric, instance, interface_id)
		SELECT device_id, 'if_in_bps', if_index::text, id FROM device_interfaces WHERE device_id = ?`, sw)
	// Port Gi<n> moves n bps in every bucket of the hour.
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT ?::timestamptz + make_interval(mins => 5 * b + 1), s.id, s.instance::int
		FROM metrics.series s, generate_series(0, 11) b WHERE s.device_id = ?`, start, sw)
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypePortRoles, models.ReportScope{SiteIDs: []uuid.UUID{hq},
		Roles: []string{models.PortRoleWAN}, Metrics: []string{services.MetricIfInBps}})

	data, err := newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)

	rows := data.Tables[0].Rows
	if data.Rows != 500 || data.CappedOut != 2 || data.Ports != 500 || data.Devices != 1 || len(rows) != 500 {
		t.Fatalf("rows %d, cut %d, ports %d, devices %d, lines %d; want 500 kept, 2 cut", data.Rows, data.CappedOut, data.Ports, data.Devices, len(rows))
	}
	if rows[0].Name != "core-sw1 · Gi502" || rows[499].Name != "core-sw1 · Gi3" {
		t.Errorf("lines run %q to %q, want Gi502 down to Gi3", rows[0].Name, rows[499].Name)
	}
	if want := []string{"core-sw1 · Gi502", "core-sw1 · Gi501", "core-sw1 · Gi500", "core-sw1 · Gi499", "core-sw1 · Gi498"}; !slices.Equal(data.Busiest, want) {
		t.Errorf("busiest = %v", data.Busiest)
	}
	// 1 + 2 + ... + 502 = 126253 bps: the cut ports still count in the headline.
	if data.Tiles[0].First != 126253 || len(data.RowCharts) != 10 || data.ScopeLabel != "WAN ports at HQ" {
		t.Errorf("tile %v, row charts %d, label %q", data.Tiles[0].First, len(data.RowCharts), data.ScopeLabel)
	}
}

// Review Focus 5: a device deleted after the period but before the run. Its
// series went with it, so its lines vanish; it is counted as unavailable and
// the run goes on, before and after the nightly cleanup removes its samples.
func TestDBBuildDeletedDeviceIsUnavailable(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw1, sw2 := newDevice(t, db, hq, "sw1"), newDevice(t, db, hq, "sw2")
	p1, p2 := newPort(t, db, sw1, 1, "Gi1", gigabit), newPort(t, db, sw2, 1, "Gi1", gigabit)
	start, end := buildWindow()
	steady(t, db, portSeries(t, db, sw1, p1, 1, services.MetricIfInBps), start, 12, 100)
	steady(t, db, portSeries(t, db, sw2, p2, 1, services.MetricIfInBps), start, 12, 200)
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{sw1, sw2},
		Metrics: []string{services.MetricIfInBps, services.MetricIfOutBps}})
	b := newBuilder(db)

	data, err := b.Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)
	if want := []string{"sw2", "sw1"}; !slices.Equal(rowNames(data.Tables[0].Rows), want) || data.Ports != 2 || data.Devices != 2 {
		t.Fatalf("before the delete: %v, %d ports, %d devices; want %v, 2, 2", rowNames(data.Tables[0].Rows), data.Ports, data.Devices, want)
	}

	devices := services.NewDeviceService(db, services.NewSNMPCredentialService(db), services.NewIncidentService(db))
	_, err = devices.Delete(ctx, sw2)
	testdb.Must(t, err)
	check := func(when string) {
		t.Helper()
		data, err := b.Build(ctx, report, admin, start, end, time.UTC)
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		rows := data.Tables[0].Rows
		if data.Unavailable != 1 || data.Rows != 1 || data.Devices != 1 || len(rows) != 1 || rows[0].Name != "sw1" ||
			rows[0].Billable == nil || *rows[0].Billable != 100 {
			t.Errorf("%s: unavailable %d, rows %d, lines %+v; want sw1 alone and one unavailable", when, data.Unavailable, data.Rows, rows)
		}
	}
	check("after the delete")
	_, err = services.NewMetricsStore(db).Cleanup(ctx, 365)
	testdb.Must(t, err)
	refresh(t, db)
	check("after the nightly cleanup")
}

// A calendar month compares with the month before. August's room
// temperature averages 30 °C and July's 20 °C: +50%. The UPS was on battery
// for 6 whole buckets and 3 of the 5 minutes of a seventh, then 5 buckets on
// mains: 6.6 x 300 s = 1980 s; 9 of its 16 samples were "on", a share of
// 0.5625. Each line has 12 of August's 8928 buckets: both are flagged.
func TestDBBuildCalendarPreviousCustomMetricAndBool(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	// The months below are fixed dates; keep the retention policy away from them.
	testdb.Exec(t, db, `SELECT remove_retention_policy('metrics.samples', if_exists => true)`)
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	ups := newDevice(t, db, hq, "ups1")
	newProfileMetric(t, db, "Lab sensors", false, "lab_temp", "Room temperature", "°C", "gauge", 0)
	temp := newSeries(t, db, ups, "lab_temp", "1", nil, "Rack A")
	batt := newSeries(t, db, ups, services.MetricUPSOnBattery, "", nil, "")
	start, end := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	steady(t, db, temp, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), 12, 30)
	steady(t, db, temp, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC), 12, 20)
	day := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	steady(t, db, batt, day, 6, 1)
	for i, v := range []float64{1, 1, 1, 0, 0} {
		sampleAt(t, db, batt, day.Add(30*time.Minute+time.Duration(i)*time.Minute), v)
	}
	steady(t, db, batt, day.Add(35*time.Minute), 5, 0)
	refresh(t, db)
	report := metricsReport(admin, models.ScopeTypeDevices, models.ReportScope{DeviceIDs: []uuid.UUID{ups},
		Metrics: []string{"lab_temp", services.MetricUPSOnBattery}})
	report.PeriodKind, report.PeriodUnit, report.PeriodOffset, report.TimeRangeDays = models.PeriodCalendar, models.UnitMonth, 1, 0

	data, err := newBuilder(db).Build(ctx, report, admin, start, end, time.UTC)
	testdb.Must(t, err)

	if !data.PrevStart.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) || !data.PrevEnd.Equal(start) {
		t.Errorf("previous = %v to %v, want July", data.PrevStart, data.PrevEnd)
	}
	if data.RankLabel != "Room temperature" || data.ScopeLabel != "ups1" || data.Rows != 1 || data.Devices != 1 ||
		data.Ports != 0 || data.LowCoverage != 2 {
		t.Errorf("rank %q, label %q, rows %d, devices %d, ports %d, low %d", data.RankLabel, data.ScopeLabel, data.Rows,
			data.Devices, data.Ports, data.LowCoverage)
	}
	temps := data.Tables[0]
	if temps.Title != "Room temperature" || temps.Unit != "°C" || temps.Paired || temps.HasTotal || len(temps.Rows) != 1 {
		t.Fatalf("temperature table = %+v", temps)
	}
	if r := temps.Rows[0]; r.Name != "ups1 · Rack A" || r.In == nil || r.In.Avg != 30 || r.Change == nil || !near(*r.Change, 50) ||
		!r.LowCoverage || !near(r.Coverage, 1200.0/8928) {
		t.Errorf("temperature = %+v, want 30 °C, +50%%, flagged at 0.13%%", r)
	}
	batts := data.Tables[1]
	if batts.Title != "On battery" || batts.Unit != "bool" || len(batts.Rows) != 1 {
		t.Fatalf("battery table = %+v", batts)
	}
	if r := batts.Rows[0]; r.Name != "ups1" || r.In == nil || r.In.TrueSeconds == nil || !near(*r.In.TrueSeconds, 1980) ||
		!near(r.In.Avg, 0.5625) || !r.New {
		t.Errorf("on battery = %+v, want 1980 s true, a share of 0.5625, new", r)
	}
	// The tile reads the scope's combined series: the mean of its bucket shares, 6.6 / 12 = 0.55.
	if tile := data.Tiles[1]; tile.Kind != "other" || !near(tile.First, 0.55) {
		t.Errorf("battery tile = %+v, want 0.55", tile)
	}
}

// Every chosen subject hidden: a one-page report that says so. A metric
// deleted since the report was made is skipped and named; with no data at
// all, the headline says so.
func TestDBBuildEmptySkippedAndNoData(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	user := testdb.NewUser(t, db, false)
	hq, annex := newSite(t, db, "HQ"), newSite(t, db, "Annex")
	shareSite(t, db, hq, user, "readonly")
	far := newDevice(t, db, annex, "annex-sw1")
	start, end := buildWindow()
	b := newBuilder(db)

	data, err := b.Build(ctx, metricsReport(user, models.ScopeTypeDevices,
		models.ReportScope{DeviceIDs: []uuid.UUID{far}, Metrics: []string{services.MetricIfInBps}}), user, start, end, time.UTC)
	testdb.Must(t, err)
	if !data.Empty || data.Unavailable != 1 || len(data.Tables) != 0 || data.ScopeType != models.ScopeTypeDevices {
		t.Errorf("hidden device: empty %v, unavailable %d, tables %d", data.Empty, data.Unavailable, len(data.Tables))
	}

	sw := newDevice(t, db, hq, "core-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1", gigabit)
	data, err = b.Build(ctx, metricsReport(user, models.ScopeTypePorts,
		models.ReportScope{PortIDs: []uuid.UUID{gi1}, Metrics: []string{services.MetricIfInBps, "gone_metric"}}), user, start, end, time.UTC)
	testdb.Must(t, err)
	if data.Empty || !data.NoData || !slices.Equal(data.Skipped, []string{"gone_metric"}) || len(data.Tables) != 1 ||
		len(data.Tables[0].Rows) != 1 || !data.Tables[0].Rows[0].NoData || !data.Tiles[0].NoData ||
		len(data.Busiest) != 0 || len(data.RowCharts) != 0 || data.Rows != 1 {
		t.Errorf("no data = %+v", data)
	}
}

// A build that runs out of time is a report too large to build, with the
// message the user reads; the job queue does not retry it (Task 2).
func TestDBBuildRunningOutOfTimeIsTooLarge(t *testing.T) {
	db := testdb.Open(t)
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1", gigabit)
	start, end := buildWindow()
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	report := metricsReport(admin, models.ScopeTypePorts, models.ReportScope{PortIDs: []uuid.UUID{gi1}, Metrics: []string{services.MetricIfInBps}})

	_, err := newBuilder(db).Build(expired, report, admin, start, end, time.UTC)
	if !errors.Is(err, services.ErrReportTooLarge) || err.Error() != "This report is too large to build: narrow the scope or shorten the period" {
		t.Errorf("err = %v, want ErrReportTooLarge", err)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go vet ./internal/netreport/`
Expected: it fails with `newBuilder(db).Build undefined (type *Builder has no field or method Build)`.

- [ ] **Step 3: Write `build.go`**

Create `backend/internal/netreport/build.go`:

```go
package netreport

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// line is one table line: its row, its series per side (0: in or the single
// metric, 1: out), and its statistics in the period and the one before.
type line struct {
	row
	sides     [2][]int64
	cur, prev [2]services.SeriesStat
}

// table is one group's lines, and the whole scope's series and statistics
// per side (its tile and its chart).
type table struct {
	group     group
	lines     []line
	scope     [2][]int64
	scopeCur  [2]services.SeriesStat
	scopePrev [2]services.SeriesStat
}

// Build implements services.NetworkReportBuilder. It resolves the report's
// scope as requestedBy, reads [start, end) and the period before it, and
// fills the report. A statistics query that runs out of time (its statement
// timeout, or the job's own deadline) fails the report with
// services.ErrReportTooLarge, which the job queue does not retry.
func (b *Builder) Build(ctx context.Context, report *models.Report, requestedBy uuid.UUID, start, end time.Time, loc *time.Location) (*services.MetricsReportData, error) {
	data, err := b.build(ctx, report, requestedBy, start, end, loc)
	if errors.Is(err, services.ErrReportTooLarge) || errors.Is(err, context.DeadlineExceeded) {
		return nil, services.ErrReportTooLarge
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (b *Builder) build(ctx context.Context, report *models.Report, user uuid.UUID, start, end time.Time, loc *time.Location) (*services.MetricsReportData, error) {
	prevStart, prevEnd := report.PreviousPeriod(start, end, loc)
	scope := report.ScopeData
	res, err := b.resolve(ctx, user, report.ScopeType, scope)
	if err != nil {
		return nil, err
	}
	data := &services.MetricsReportData{
		ScopeType: report.ScopeType, ScopeLabel: scopeLabel(report.ScopeType, scope, res),
		PrevStart: prevStart, PrevEnd: prevEnd, Unavailable: res.unavailable,
	}
	if res.empty() {
		data.Empty = true
		return data, nil
	}
	groups, skipped, err := b.groups(ctx, report.ScopeType, scope.Metrics)
	if err != nil {
		return nil, err
	}
	data.Skipped = skipped
	if len(groups) == 0 {
		data.NoData = true
		return data, nil
	}
	data.RankLabel = groups[0].title

	var keys []string
	for _, g := range groups {
		for _, m := range g.metrics() {
			keys = append(keys, m.Key)
		}
	}
	idx, err := b.loadSeries(ctx, res, keys)
	if err != nil {
		return nil, err
	}
	tables := make([]table, len(groups))
	for i, g := range groups {
		tables[i] = tableFor(res, g, idx)
	}
	// The first table ranks every subject, so it is read in full before the cap.
	if err := b.fill(ctx, &tables[0], start, end, false); err != nil {
		return nil, err
	}
	keep, cut := keepBusiest(candidates(tables), models.MaxReportSubjects)
	data.Rows, data.CappedOut = len(keep), cut
	countSubjects(data, res, keep)
	for i := range tables {
		tables[i].lines = keptLines(tables[i].lines, keep)
		if i > 0 {
			if err := b.fill(ctx, &tables[i], start, end, false); err != nil {
				return nil, err
			}
		}
		if err := b.fill(ctx, &tables[i], prevStart, prevEnd, true); err != nil {
			return nil, err
		}
	}
	if err := b.assemble(ctx, data, tables, start, end); err != nil {
		return nil, err
	}
	return data, nil
}

// groups describes the report's metrics and pairs them into tables. A metric
// that no longer exists, that a report cannot show, or that this scope has no
// lines for is skipped and named, rather than failing the run.
func (b *Builder) groups(ctx context.Context, scopeType string, keys []string) ([]group, []string, error) {
	infos, err := b.lookupMetrics(ctx, keys)
	if err != nil {
		return nil, nil, err
	}
	var ms []metricInfo
	var skipped []string
	for _, k := range keys {
		m, ok := infos[k]
		if !ok || !m.reportable() || (!m.port() && !deviceScope(scopeType)) {
			skipped = append(skipped, k)
			continue
		}
		ms = append(ms, m)
	}
	return groupMetrics(ms), skipped, nil
}

// tableFor lays out a group's lines (a pair's two metrics share their lines,
// matched by key) and the whole scope's series per side: the site totals' on
// a sites scope, otherwise every line's.
func tableFor(res *resolved, g group, idx *seriesIndex) table {
	t := table{group: g}
	at := map[string]int{}
	for side, m := range g.metrics() {
		for _, r := range rowsFor(res, m, idx) {
			i, ok := at[r.key]
			if !ok {
				i = len(t.lines)
				at[r.key] = i
				t.lines = append(t.lines, line{row: r})
			}
			t.lines[i].sides[side] = r.series
		}
		seen := map[int64]bool{}
		for _, l := range t.lines {
			if l.site != (len(res.sites) > 0) {
				continue
			}
			for _, id := range l.sides[side] {
				if !seen[id] {
					seen[id] = true
					t.scope[side] = append(t.scope[side], id)
				}
			}
		}
	}
	return t
}

// fill reads the statistics of a table's lines, and of the whole scope, over
// [from, to): per side, one query for the single-series lines, one for the
// totals, one for the scope.
func (b *Builder) fill(ctx context.Context, t *table, from, to time.Time, prev bool) error {
	for side, m := range t.group.metrics() {
		stats, err := b.lineStats(ctx, t.lines, side, !m.additive(), from, to)
		if err != nil {
			return err
		}
		scope, err := b.metrics.CombinedStats(ctx, services.CombinedQuery{SeriesIDs: t.scope[side], From: from, To: to, Average: !m.additive()})
		if err != nil {
			return err
		}
		for i := range t.lines {
			if prev {
				t.lines[i].prev[side] = stats[i]
			} else {
				t.lines[i].cur[side] = stats[i]
			}
		}
		if prev {
			t.scopePrev[side] = scope
		} else {
			t.scopeCur[side] = scope
		}
	}
	return nil
}

// lineStats reads one side's statistics for these lines: SeriesStats for the
// lines of one series, GroupedStats for the totals. A line without series,
// or without data, gets a zero SeriesStat (Buckets 0: no data).
func (b *Builder) lineStats(ctx context.Context, lines []line, side int, average bool, from, to time.Time) ([]services.SeriesStat, error) {
	out := make([]services.SeriesStat, len(lines))
	var singles []int64
	var groups [][]int64
	var grouped []int
	for i, l := range lines {
		ids := l.sides[side]
		switch {
		case l.total:
			groups = append(groups, ids)
			grouped = append(grouped, i)
		case len(ids) > 0:
			singles = append(singles, ids[0])
		}
	}
	if len(singles) > 0 {
		stats, err := b.metrics.SeriesStats(ctx, services.StatsQuery{SeriesIDs: singles, From: from, To: to})
		if err != nil {
			return nil, err
		}
		byID := make(map[int64]services.SeriesStat, len(stats))
		for _, s := range stats {
			byID[s.SeriesID] = s
		}
		for i, l := range lines {
			if !l.total && len(l.sides[side]) > 0 {
				out[i] = byID[l.sides[side][0]]
			}
		}
	}
	if len(groups) > 0 {
		stats, err := b.metrics.GroupedStats(ctx, groups, from, to, average)
		if err != nil {
			return nil, err
		}
		for j, s := range stats {
			out[grouped[j]] = s
		}
	}
	return out, nil
}

// candidates lists every subject with a line in any table, valued by its
// busiest line in the first table (the report's first metric). A subject
// with no line or no data there is unranked and goes last.
func candidates(tables []table) []candidate {
	var out []candidate
	at := map[string]int{}
	for _, t := range tables {
		for _, l := range t.lines {
			if l.subject == "" {
				continue
			}
			if _, ok := at[l.subject]; !ok {
				at[l.subject] = len(out)
				out = append(out, candidate{subject: l.subject, name: l.name})
			}
		}
	}
	first := tables[0]
	for _, l := range first.lines {
		if l.subject == "" {
			continue
		}
		v, ok := rankOf(first.group, l.cur)
		c := &out[at[l.subject]]
		if ok && (!c.ok || v > c.value) {
			c.value, c.ok = v, true
		}
	}
	return out
}

// keptLines drops the lines of subjects the cap cut; site totals stay.
func keptLines(lines []line, keep map[string]bool) []line {
	out := make([]line, 0, len(lines))
	for _, l := range lines {
		if l.site || keep[l.subject] {
			out = append(out, l)
		}
	}
	return out
}

// countSubjects fills the report's port and device counts from the subjects
// kept: ports and their devices; devices and their physical ports on a
// devices scope; the visible sites' devices on a sites scope.
func countSubjects(data *services.MetricsReportData, res *resolved, keep map[string]bool) {
	devices := map[uuid.UUID]bool{}
	for _, p := range res.ports {
		if keep["p:"+p.ID.String()] {
			data.Ports++
			devices[p.DeviceID] = true
		}
	}
	for _, d := range res.devices {
		if keep["d:"+d.ID.String()] {
			devices[d.ID] = true
			if res.scopeType == models.ScopeTypeDevices {
				data.Ports += len(d.Physical)
			}
		}
	}
	data.Devices = len(devices)
	if res.scopeType == models.ScopeTypeSites {
		data.Devices = len(res.devices)
	}
}

// roleNames are the port roles as the scope label writes them.
var roleNames = map[string]string{models.PortRoleWAN: "WAN", models.PortRoleUplink: "Uplink", models.PortRoleAccess: "Access"}

// scopeLabel names the scope in words, naming only what the owner can see:
// "WAN, Uplink ports at HQ, Annex", "All of HQ", "core-sw1 · Gi1/0/1",
// "12 ports", "3 devices".
func scopeLabel(scopeType string, scope models.ReportScope, res *resolved) string {
	sites := strings.Join(res.siteNames, ", ")
	if sites == "" {
		sites = "no available site"
	}
	switch scopeType {
	case models.ScopeTypePorts:
		if len(res.ports) == 1 {
			return res.ports[0].Name
		}
		return plural(len(res.ports), "port")
	case models.ScopeTypeDevices:
		if len(res.devices) == 1 {
			return res.devices[0].Name
		}
		return plural(len(res.devices), "device")
	case models.ScopeTypePortRoles:
		var roles []string
		for _, r := range []string{models.PortRoleWAN, models.PortRoleUplink, models.PortRoleAccess} {
			if slices.Contains(scope.Roles, r) {
				roles = append(roles, roleNames[r])
			}
		}
		return strings.Join(roles, ", ") + " ports at " + sites
	case models.ScopeTypeSites:
		return "All of " + sites
	}
	return ""
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
```

- [ ] **Step 4: Write `assemble.go`**

Create `backend/internal/netreport/assemble.go`:

```go
package netreport

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

const (
	// chartPoints is about how many points each chart has.
	chartPoints = 200
	// rowChartCount bounds the busiest lines charted one by one.
	rowChartCount = 10
	// busiestCount is how many names the headline's "Busiest" lists.
	busiestCount = 5
)

// assemble turns the filled tables into the report: tables, tiles and charts
// in metric order, running hot from the busy table, and the headline list
// and small charts from the first table's ranking.
func (b *Builder) assemble(ctx context.Context, data *services.MetricsReportData, tables []table, start, end time.Time) error {
	flagged := map[string]bool{}
	hasData := false
	for i := range tables {
		t := &tables[i]
		sortLines(t)
		mt := services.MetricsTable{Title: t.group.title, Unit: t.group.unit(), Paired: t.group.out != nil,
			Billable: t.group.billable(), HasTotal: t.group.in.additive(), Rows: []services.MetricsRow{}}
		for _, l := range t.lines {
			r := lineFigures(t.group, l.name, l.cur, l.prev)
			hasData = hasData || !r.NoData
			if r.LowCoverage {
				flagged[l.key] = true
			}
			mt.Rows = append(mt.Rows, r)
		}
		data.Tables = append(data.Tables, mt)
		data.Tiles = append(data.Tiles, tileFor(t.group, t.scopeCur, t.scopePrev))
		var ref *float64
		if v, ok := rankOf(t.group, t.scopeCur); ok {
			ref = &v
		}
		c, err := b.chart(ctx, t.group.title, t.group, t.scope, ref, start, end)
		if err != nil {
			return err
		}
		data.Charts = append(data.Charts, c)
		if isBusy(t.group) {
			for _, l := range t.lines {
				if l.port == nil {
					continue
				}
				if h, hot := hotPort(t.group, l.name, l.cur); hot {
					data.RunningHot = append(data.RunningHot, h)
				}
			}
		}
	}
	data.LowCoverage = len(flagged)
	data.NoData = !hasData
	sort.SliceStable(data.RunningHot, func(i, j int) bool {
		x, y := data.RunningHot[i], data.RunningHot[j]
		return lessBusy(true, math.Max(x.P95In, x.P95Out), x.Name, true, math.Max(y.P95In, y.P95Out), y.Name)
	})

	first := tables[0]
	for _, l := range first.lines {
		v, ok := rankOf(first.group, l.cur)
		if l.site || !ok {
			continue
		}
		if len(data.Busiest) < busiestCount {
			data.Busiest = append(data.Busiest, l.name)
		}
		if len(data.RowCharts) < rowChartCount {
			ref := v
			c, err := b.chart(ctx, l.name, first.group, l.sides, &ref, start, end)
			if err != nil {
				return err
			}
			data.RowCharts = append(data.RowCharts, c)
		}
	}
	return nil
}

// sortLines orders a table: site totals first, then busiest first by the
// ranking figure (the 95th, billable for traffic), lines without data last,
// ties by name.
func sortLines(t *table) {
	sort.SliceStable(t.lines, func(i, j int) bool {
		x, y := t.lines[i], t.lines[j]
		if x.site != y.site {
			return x.site
		}
		xv, xok := rankOf(t.group, x.cur)
		yv, yok := rankOf(t.group, y.cur)
		return lessBusy(xok, xv, x.name, yok, yv, y.name)
	})
}

// chart draws a group over [from, to): one line per side from these series,
// combined per bucket by the unit's rule, with ref as the reference line.
func (b *Builder) chart(ctx context.Context, title string, g group, sides [2][]int64, ref *float64, from, to time.Time) (services.MetricsChart, error) {
	c := services.MetricsChart{Title: title, Unit: g.unit(), Reference: ref}
	for side, m := range g.metrics() {
		pts, err := b.metrics.CombinedSeries(ctx, services.CombinedQuery{SeriesIDs: sides[side], From: from, To: to,
			Average: !m.additive()}, chartPoints)
		if err != nil {
			return c, err
		}
		c.Lines = append(c.Lines, services.ChartLine{Label: m.Label, Points: pts})
	}
	return c, nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go vet ./internal/netreport/`
Expected: no output.

Run: `./scripts/test-db.sh -run TestDBBuild -v`
Expected: PASS for `TestDBBuildPortsTrafficBusyAndRunningHot`, `TestDBBuildSiteTotalsSumTrafficAndAverageBusy`, `TestDBBuildCapKeepsTheBusiest500`, `TestDBBuildDeletedDeviceIsUnavailable`, `TestDBBuildCalendarPreviousCustomMetricAndBool`, `TestDBBuildEmptySkippedAndNoData` and `TestDBBuildRunningOutOfTimeIsTooLarge`.

Run: `go test ./internal/netreport/ -v`
Expected: the unit tests PASS, and the DB tests SKIP without a database.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/netreport/build.go backend/internal/netreport/assemble.go backend/internal/netreport/build_db_test.go
git commit -m "feat(reports): build the metrics report data from the rollups

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: The Metrics branch in the report pipeline

**Files:**
- Modify: `backend/internal/services/report_metrics_data.go` (add `NetworkReportBuilder`)
- Modify: `backend/internal/services/report_aggregator.go` (`network` field, `SetNetworkBuilder`, `ReportData.Network`, the branch in `AggregateReportData`, new `aggregateNetwork`)
- Modify: `backend/cmd/sentinel/main.go` (wire `netreport.NewBuilder`)
- Test: `backend/internal/services/report_aggregator_network_test.go` (new, unit)
- Test: `backend/internal/netreport/pipeline_db_test.go` (new, DB)

**Interfaces:**
- Consumes:
  - Task 7b: `(*netreport.Builder).Build`.
  - Task 1: `Report.ResolvePeriod` (existing) and `models.ReportTypeMetrics`.
  - The netreport fixtures (Task 5).
- Produces:
  - `services.NetworkReportBuilder` with `Build(ctx context.Context, report *models.Report, requestedBy uuid.UUID, start, end time.Time, loc *time.Location) (*MetricsReportData, error)`.
  - `func (s *ReportAggregatorService) SetNetworkBuilder(b NetworkReportBuilder)`.
  - `ReportData.Network *MetricsReportData` with JSON `network,omitempty`.
  - For a `metrics` report, `AggregateReportData`:
    - resolves the period in the report timezone and calls `Build`;
    - returns `ReportData` with `Network` set, `Metrics` empty and `Warnings` untouched;
    - skips the monitor path entirely;
    - passes errors through, so `errors.Is(err, ErrReportTooLarge)` still holds after the generator's `aggregating report data: %w` wrap.
  - In `main.go`, the variable `networkReports` (a `*netreport.Builder`), which Task 14 passes to `reportBuilder.SetNetworkScopes(networkReports)`.

- [ ] **Step 1: Write the failing unit tests**

Create `backend/internal/services/report_aggregator_network_test.go`:

```go
package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// fakeNetworkBuilder records what the aggregator asked it for.
type fakeNetworkBuilder struct {
	user       uuid.UUID
	start, end time.Time
	loc        *time.Location
	data       *MetricsReportData
	err        error
}

func (f *fakeNetworkBuilder) Build(_ context.Context, _ *models.Report, requestedBy uuid.UUID, start, end time.Time, loc *time.Location) (*MetricsReportData, error) {
	f.user, f.start, f.end, f.loc = requestedBy, start, end, loc
	return f.data, f.err
}

func networkReportFor(days int) *models.Report {
	return &models.Report{Name: "WAN", ReportType: models.ReportTypeMetrics, ScopeType: models.ScopeTypePortRoles,
		ScopeData:  models.ReportScope{SiteIDs: []uuid.UUID{uuid.New()}, Roles: []string{"wan"}, Metrics: []string{MetricIfInBps}},
		PeriodKind: models.PeriodRolling, TimeRangeDays: days}
}

// A metrics report goes to the network builder, with the period resolved in
// the report timezone, and never touches the monitor path: this service has
// no database here at all.
func TestAggregateMetricsReportUsesTheNetworkBuilder(t *testing.T) {
	fake := &fakeNetworkBuilder{data: &MetricsReportData{ScopeLabel: "WAN ports at HQ"}}
	s := NewReportAggregatorService(nil, nil)
	s.SetNetworkBuilder(fake)
	user := uuid.New()

	data, err := s.AggregateReportData(context.Background(), networkReportFor(7), user)
	if err != nil {
		t.Fatal(err)
	}
	if data.Network != fake.data || data.ReportName != "WAN" || len(data.Metrics) != 0 || len(data.Warnings) != 0 {
		t.Errorf("data = %+v, want the builder's network data and nothing else", data)
	}
	if fake.user != user || fake.loc != time.UTC || fake.end.Sub(fake.start) != 7*24*time.Hour {
		t.Errorf("builder got user %v, %v to %v in %v; want the requester, 7 days, UTC", fake.user, fake.start, fake.end, fake.loc)
	}
	if !data.TimeRangeStart.Equal(fake.start) || !data.TimeRangeEnd.Equal(fake.end) {
		t.Errorf("report window %v to %v, want the builder's %v to %v", data.TimeRangeStart, data.TimeRangeEnd, fake.start, fake.end)
	}
}

func TestAggregateMetricsReportPassesTooLargeThrough(t *testing.T) {
	s := NewReportAggregatorService(nil, nil)
	s.SetNetworkBuilder(&fakeNetworkBuilder{err: ErrReportTooLarge})
	if _, err := s.AggregateReportData(context.Background(), networkReportFor(7), uuid.New()); !errors.Is(err, ErrReportTooLarge) {
		t.Errorf("err = %v, want ErrReportTooLarge", err)
	}
}

func TestAggregateMetricsReportWithoutABuilder(t *testing.T) {
	if _, err := NewReportAggregatorService(nil, nil).AggregateReportData(context.Background(), networkReportFor(7), uuid.New()); err == nil {
		t.Error("a metrics report was aggregated with no network builder set")
	}
}
```

- [ ] **Step 2: Write the failing end-to-end DB test**

Create `backend/internal/netreport/pipeline_db_test.go`:

```go
package netreport

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A metrics report built end to end through the report pipeline's
// aggregator, as the job queue and the scheduler call it.
func TestDBMetricsReportThroughTheAggregator(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1/0/1", gigabit)
	from := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	steady(t, db, portSeries(t, db, sw, gi1, 1, services.MetricIfInBps), from, 12, 4000)
	steady(t, db, portSeries(t, db, sw, gi1, 1, services.MetricIfOutBps), from, 12, 1000)
	refresh(t, db)
	report := models.Report{ID: uuid.New(), UserID: admin, CreatedBy: admin, Name: "Uplinks", ReportType: models.ReportTypeMetrics,
		ScopeType: models.ScopeTypePorts,
		ScopeData: models.ReportScope{PortIDs: []uuid.UUID{gi1}, Metrics: []string{services.MetricIfInBps, services.MetricIfOutBps}},
		PeriodKind: models.PeriodRolling, TimeRangeDays: 1}
	testdb.Must(t, report.Validate())
	testdb.Must(t, db.Create(&report).Error)
	agg := services.NewReportAggregatorService(db, nil)
	agg.SetNetworkBuilder(newBuilder(db))

	data, err := agg.AggregateReportData(ctx, &report, admin)
	testdb.Must(t, err)
	if data.Network == nil || len(data.Metrics) != 0 || len(data.Warnings) != 0 || data.ReportName != "Uplinks" {
		t.Fatalf("data = %+v, want the network part only", data)
	}
	if d := data.TimeRangeEnd.Sub(data.TimeRangeStart); d != 24*time.Hour {
		t.Errorf("window = %v, want the rolling day", d)
	}
	n := data.Network
	if n.ScopeLabel != "core-sw1 · Gi1/0/1" || n.Ports != 1 || n.Devices != 1 || len(n.Tables) != 1 || len(n.Tables[0].Rows) != 1 {
		t.Fatalf("network = %+v", n)
	}
	// 4000 bps for 12 buckets: 4000 x 12 x 300 / 8 = 1,800,000 bytes, with data
	// in 12 of the day's 287 or 288 complete buckets (about 4.2%).
	row := n.Tables[0].Rows[0]
	if row.Billable == nil || *row.Billable != 4000 || row.In == nil || row.In.Total == nil || *row.In.Total != 1_800_000 ||
		!row.LowCoverage || row.Coverage < 4.1 || row.Coverage > 4.2 {
		t.Errorf("row = %+v, want billable 4000, 1800000 bytes in, flagged near 4.2%% coverage", row)
	}
}
```

- [ ] **Step 3: Run them and watch them fail**

Run: `go test ./internal/services/ -run TestAggregateMetricsReport -v`
Expected: the build fails with `s.SetNetworkBuilder undefined` and `data.Network undefined`.

- [ ] **Step 4: Add the interface**

In `backend/internal/services/report_metrics_data.go`, replace the import block with:

```go
import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)
```

and add, after `ErrReportTooLarge`:

```go
// NetworkReportBuilder builds the Metrics part of a report. netreport.Builder implements it.
// It lives behind an interface because netreport imports services.
type NetworkReportBuilder interface {
	Build(ctx context.Context, report *models.Report, requestedBy uuid.UUID, start, end time.Time, loc *time.Location) (*MetricsReportData, error)
}
```

- [ ] **Step 5: The aggregator branch**

In `backend/internal/services/report_aggregator.go`:

1. Add `"errors"` to the import block, after `"context"`.

2. In `type ReportAggregatorService struct`, after the `settings *SettingsService` field, add:

```go
	// network builds Metrics reports (netreport.Builder, set by main.go).
	// Nil leaves metrics reports unavailable; uptime and incident reports do
	// not use it.
	network NetworkReportBuilder
```

3. After `NewReportAggregatorService`, add:

```go
// SetNetworkBuilder wires in the Metrics report builder after construction:
// it lives in a package that imports this one.
func (s *ReportAggregatorService) SetNetworkBuilder(b NetworkReportBuilder) {
	s.network = b
}
```

4. In `type ReportData struct`, after the `EffectiveSLA float64` field and its comment, add:

```go
	// Network is a Metrics report's content; nil for uptime and incident
	// reports, whose fields above it leaves untouched.
	Network *MetricsReportData `json:"network,omitempty"`
```

5. In `AggregateReportData`, after the block

```go
	if err := report.ValidatePeriod(); err != nil {
		return nil, fmt.Errorf("report period: %w", err)
	}
```

insert:

```go
	// A Metrics report covers network subjects, not monitors: none of the
	// monitor path below applies to it.
	if report.ReportType == models.ReportTypeMetrics {
		return s.aggregateNetwork(ctx, report, requestedBy)
	}
```

6. After `AggregateReportData`, add:

```go
// aggregateNetwork assembles a Metrics report. The network builder does the
// work over the report's period, resolved in the report timezone, as the
// report's owner. The monitor fields stay empty, and its notes stay on
// data.Network (the PDF prints them on the headline page), not in Warnings.
func (s *ReportAggregatorService) aggregateNetwork(ctx context.Context, report *models.Report, requestedBy uuid.UUID) (*ReportData, error) {
	if s.network == nil {
		return nil, errors.New("metrics reports are not available: no network report builder is set")
	}
	loc := s.reportLocation(ctx)
	start, end := report.ResolvePeriod(time.Now(), loc)
	network, err := s.network.Build(ctx, report, requestedBy, start, end, loc)
	if err != nil {
		return nil, err
	}
	return &ReportData{
		ReportName:        report.Name,
		CustomTitle:       report.CustomTitle,
		CustomDescription: report.CustomDescription,
		TimeRangeStart:    start,
		TimeRangeEnd:      end,
		Metrics:           []ReportMetrics{},
		Network:           network,
	}, nil
}
```

- [ ] **Step 6: Wire it in `main.go`**

In `backend/cmd/sentinel/main.go`, add `"github.com/Stevy2191/Sentinel/backend/internal/netreport"` to the imports, after the `models` import.

Right after the line `deviceService.SetMetricsStore(metricsStore)`, add:

```go
	// Metrics reports are built by netreport. The report pipeline reaches it
	// through services.NetworkReportBuilder (services cannot import it), and
	// the API's scope checks use the same builder (Task 14).
	networkReports := netreport.NewBuilder(db, metricsStore)
	reportAggregator.SetNetworkBuilder(networkReports)
```

`reportAggregator` is created earlier in `run()` (`reportAggregator := services.NewReportAggregatorService(db, settingsService)`). It is the one instance that the API's report builder, the job queue's generator and the scheduler's generator all share, so all three paths get the branch.

- [ ] **Step 7: Run the tests and the build**

Run: `go build ./... && go vet ./...`
Expected: no output.

Run: `go test ./internal/services/ -run 'TestAggregateMetricsReport|TestSummarize|TestUptimePercent|TestComputeUptimeSeries' -v`
Expected: PASS.

Run: `./scripts/test-db.sh -run 'TestDBMetricsReportThroughTheAggregator|TestDBBuild' -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/services/report_metrics_data.go backend/internal/services/report_aggregator.go backend/cmd/sentinel/main.go backend/internal/services/report_aggregator_network_test.go backend/internal/netreport/pipeline_db_test.go
git commit -m "feat(reports): the metrics branch in the report pipeline

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Measured performance of the statistics queries

**Files:**
- Test: `backend/internal/services/metrics_stats_perf_db_test.go` (new, DB; opt-in)

**Interfaces:**
- Consumes:
  - Tasks 3-4: `SeriesStats`, `CombinedStats`, `GroupedStats`, `CombinedSeries`, `StatsQuery`, `CombinedQuery`.
  - Task 3's test helper `statsSeries`, and `refreshRollups`.
  - `models.Int64Array`.
- Produces: no code for later tasks. It produces measured numbers. The controller uses them to confirm `models.MaxReportSubjects = 500` or to lower it.

This test generates 100 ports × 90 days of 5-minute data (2.6 million rollup rows) and times the real statistics queries on it. Generating the data and refreshing the rollups take minutes, so the test **skips unless `SENTINEL_PERF=1` is set**, and the normal `go test ./...` and `./scripts/test-db.sh` runs stay fast. It writes one sample per 5-minute bucket, the fastest way to fill the rollup: the statistics read `metrics.samples_5m` only, so five samples per bucket would give the same number of rollup rows. It logs each timing on a `PERF` line, and fails only past a generous budget. Every budget is under the 2-minute statement timeout (Task 3).

- [ ] **Step 1: Write the test**

Create `backend/internal/services/metrics_stats_perf_db_test.go`:

```go
package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// TestDBPerfReportStats measures the Metrics report's statistics queries on
// generated 5-minute data: 100 ports' traffic for 90 days, 2.6 million rollup
// rows. It logs each timing as a PERF line and fails only past a generous
// budget; the numbers decide whether the 500-row cap
// (models.MaxReportSubjects) stands. Generating and rolling up the data takes
// minutes, so it runs only when asked:
//
//	SENTINEL_PERF=1 ./scripts/test-db.sh -run TestDBPerfReportStats -v -timeout 30m
func TestDBPerfReportStats(t *testing.T) {
	if os.Getenv("SENTINEL_PERF") != "1" {
		t.Skip("set SENTINEL_PERF=1 to measure the report statistics queries (slow)")
	}
	db := testdb.Open(t)
	ctx := context.Background()
	// Background jobs (compression, retention, the rollup refresh) would
	// compete with the timed queries.
	testdb.Exec(t, db, `SELECT alter_job(job_id, scheduled => false) FROM timescaledb_information.jobs WHERE job_id >= 1000`)

	const ports, days = 100, 90
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -days)
	month := end.AddDate(0, 0, -30)
	ids := make([]int64, ports)
	for i := range ids {
		ids[i] = statsSeries(t, db, MetricIfInBps)
	}
	began := time.Now()
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT g, s.id, 1e6 * (1.5 + sin(extract(epoch FROM g)::double precision / 3600 + s.id))
		FROM generate_series(?::timestamptz, ?::timestamptz - interval '5 minutes', interval '5 minutes') g,
		     unnest(?::bigint[]) AS s(id)`, start, end, models.Int64Array(ids))
	t.Logf("PERF generated %d samples in %s", ports*days*288, time.Since(began).Round(time.Second))
	began = time.Now()
	refreshRollups(t, db)
	testdb.Exec(t, db, `ANALYZE`)
	t.Logf("PERF refreshed the rollups in %s", time.Since(began).Round(time.Second))

	m := NewMetricsStore(db)
	timed := func(name string, budget time.Duration, run func() error) time.Duration {
		t.Helper()
		began := time.Now()
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		took := time.Since(began)
		t.Logf("PERF %-48s %9s (budget %s)", name, took.Round(time.Millisecond), budget)
		if took > budget {
			t.Errorf("%s took %s, over its %s budget", name, took, budget)
		}
		return took
	}
	// whole checks every series came back with every bucket.
	whole := func(st []SeriesStat, buckets int) error {
		if len(st) != ports {
			return fmt.Errorf("%d series came back, want %d", len(st), ports)
		}
		for _, s := range st {
			if s.Buckets != buckets || s.Expected != buckets {
				return fmt.Errorf("series %d: %d of %d buckets, want %d", s.SeriesID, s.Buckets, s.Expected, buckets)
			}
		}
		return nil
	}

	month30 := timed("SeriesStats, 100 ports x 30 days", 15*time.Second, func() error {
		st, err := m.SeriesStats(ctx, StatsQuery{SeriesIDs: ids, From: month, To: end})
		if err != nil {
			return err
		}
		return whole(st, 30*288)
	})
	timed("SeriesStats, 100 ports x 90 days", 45*time.Second, func() error {
		st, err := m.SeriesStats(ctx, StatsQuery{SeriesIDs: ids, From: start, To: end})
		if err != nil {
			return err
		}
		return whole(st, 90*288)
	})
	for _, w := range []struct {
		name   string
		from   time.Time
		want   int
		budget time.Duration
	}{
		{"CombinedStats, 100 ports summed x 30 days", month, 30 * 288, 15 * time.Second},
		{"CombinedStats, 100 ports summed x 90 days", start, 90 * 288, 45 * time.Second},
	} {
		timed(w.name, w.budget, func() error {
			st, err := m.CombinedStats(ctx, CombinedQuery{SeriesIDs: ids, From: w.from, To: end})
			if err == nil && st.Buckets != w.want {
				err = fmt.Errorf("%d of %d buckets, want %d", st.Buckets, st.Expected, w.want)
			}
			return err
		})
	}
	timed("GroupedStats, 10 totals of 10 ports x 30 days", 15*time.Second, func() error {
		groups := make([][]int64, 10)
		for i := range groups {
			groups[i] = ids[i*10 : i*10+10]
		}
		st, err := m.GroupedStats(ctx, groups, month, end, false)
		if err != nil {
			return err
		}
		for _, s := range st {
			if s.Buckets != 30*288 {
				return fmt.Errorf("group %d: %d buckets, want %d", s.SeriesID, s.Buckets, 30*288)
			}
		}
		return nil
	})
	timed("CombinedSeries, 100 ports x 90 days (hourly)", 15*time.Second, func() error {
		pts, err := m.CombinedSeries(ctx, CombinedQuery{SeriesIDs: ids, From: start, To: end}, 200)
		if err == nil && len(pts) == 0 {
			err = errors.New("no chart points")
		}
		return err
	})
	// A monthly report reads each metric's statistics for this month and the
	// last. At 500 ports that is about 2 x 5 = 10 times the 30-day figure per
	// metric, and 40 times it for four metrics.
	t.Logf("PERF estimate for a monthly 500-port report: 1 metric %s, 4 metrics %s (the job limit is 5 minutes)",
		(10 * month30).Round(time.Second), (40 * month30).Round(time.Second))
}
```

- [ ] **Step 2: Check that it skips by default**

Run: `./scripts/test-db.sh -run TestDBPerfReportStats -v`
Expected: `--- SKIP: TestDBPerfReportStats ... set SENTINEL_PERF=1 to measure the report statistics queries (slow)`.

- [ ] **Step 3: Measure**

Run: `SENTINEL_PERF=1 ./scripts/test-db.sh -run TestDBPerfReportStats -v -timeout 30m`
Expected: PASS, with `PERF` lines for the data generation, the rollup refresh, each of the six timed queries, and the 500-port estimate. If you run the tests through a Docker wrapper rather than this script, pass the variable into the container (`-e SENTINEL_PERF=1`), or the test skips.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/services/metrics_stats_perf_db_test.go
git commit -m "test(metrics): measure the report statistics queries on 90 days of 100 ports

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 5: Report the numbers**

Copy every `PERF` line into your task report, word for word, and name the machine it ran on. The controller sets the cap from them:
- If "SeriesStats, 100 ports x 30 days" takes **3 s or less**, a monthly 500-port report with four metrics stays near or under 2 minutes, and the cap of 500 stands.
- If it takes longer, say so. The controller will lower `models.MaxReportSubjects` (and the spec's "500" strings) in a follow-up change, or plan the per-day table the spec defers.

Do not change the cap in this task.


### Task 10: A line-chart helper and unit formatting

The uptime graph's drawing becomes a generic line-chart helper. Both report types use it: axes, unit-aware tick labels, one or more lines and an optional reference line. The uptime graph moves onto the helper and must render exactly as before. A golden file of its page content, recorded from the old code before the move, proves this. The unit formatting for Metrics reports (PDF and email) goes in its own file, with table-driven tests.

**Files:**
- Create: `backend/internal/services/pdf_chart.go`
- Create: `backend/internal/services/pdf_units.go`
- Create: `backend/internal/services/pdf_chart_test.go`
- Create: `backend/internal/services/pdf_units_test.go`
- Create (generated): `backend/internal/services/testdata/uptime_graph.golden`
- Modify: `backend/internal/services/pdf_renderer.go` (`drawPDFUptimeGraph`, from its `drawSectionHeading(pdf, "Uptime vs. SLA target")` line to the end of the function)

**Interfaces:**
- Consumes (existing, `services` package): `setColor(pdf, c [3]int, fill bool)`, `setDrawColor(pdf, c [3]int)`, `pdfText(s string) string`, `drawSectionHeading`, the palette (`pdfInk`, `pdfMuted`, `pdfRule`, `pdfPanel`, `pdfAccent`, `pdfSuccess`, `pdfWarning`, `pdfDanger`) and geometry (`pdfMarginLeft`, `pdfMarginTop`, `pdfMarginRight`, `pdfContentW`) from `pdf_renderer.go`. Test helpers `f64(v float64) *float64` and `sampleReportData() *ReportData` (`pdf_renderer_test.go`).
- Produces (unexported, used by Tasks 11-13):
  - `type chartLine struct { X, Y []float64; Color [3]int; Width float64 }`. X runs from 0 (the plot's left edge) to 1 (its right edge).
  - `type chartRef struct { Value float64; Label string; Color [3]int }`
  - `type chartXLabel struct { At float64; Text string; Align string }`
  - `type lineChart struct { Height, AxisLabelW, Lo, Hi, TopPad float64; Ticks int; TickLabel func(v float64) string; Ref *chartRef; Lines []chartLine; XLabels []chartXLabel; LabelsInside bool }`
  - `func drawLineChart(pdf *fpdf.Fpdf, c lineChart)`. It draws from the current Y and leaves Y at `start + Height + 9`. It does no page-fit check; callers do that.
  - `func niceCeil(v float64) float64`. It rounds up to 1, 2, 2.5 or 5 × 10ⁿ; `v ≤ 0` gives 1.
  - `func finite(v float64) bool`, `func trimZeros(s string) string`, `func formatSig(v float64) string`
  - `func formatBps(v float64) string`, `func formatBytes(v float64) string`, `func formatCount(v float64) string`, `func formatPercent(p float64) string`, `func formatDurationHM(seconds float64) string`
  - `func formatValue(v float64, unit string) string`. bps scales to Kbps/Mbps/Gbps, per_min prints as `N/min`, % and bool (as a share) print as percentages, anything else prints as `number + " " + unit`.
  - `func formatTotal(v float64, unit string) string`. bps gives bytes, per_min gives a count, anything else gives `""`.
  - `func formatChange(change *float64, isNew bool) string`. It gives `"+18%"`, `"-3.5%"`, `"0%"` or `"new"`, and `""` when there is no change.
  - Test helpers: `chartTestDoc() *fpdf.Fpdf`, `pageStreams(t *testing.T, pdf *fpdf.Fpdf) []byte`, and the `-update-golden` flag (`updateGolden`).

> Note for the implementer: `pdf_renderer_test.go` declares a package-level `func min(a, b int) int`, and in test builds it shadows Go's built-in `min` for the whole `services` package. Do not call `min` on floats in `services` code. Use `math.Min`.

- [ ] **Step 1: Write the golden test for the uptime graph, before touching the graph**

Create `backend/internal/services/pdf_chart_test.go`:

```go
package services

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite the golden files under testdata/")

// chartTestDoc is an uncompressed A4 page with fixed dates, so its bytes are
// the same on every run.
func chartTestDoc() *fpdf.Fpdf {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMarginLeft, pdfMarginTop, pdfMarginRight)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetCompression(false)
	fixed := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	pdf.SetCreationDate(fixed)
	pdf.SetModificationDate(fixed)
	pdf.AddPage()
	return pdf
}

// pageStreams returns the drawing operators of every page of an uncompressed document.
func pageStreams(t *testing.T, pdf *fpdf.Fpdf) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatal(err)
	}
	var out []byte
	for _, m := range regexp.MustCompile(`(?s)stream\r?\n(.*?)endstream`).FindAllSubmatch(buf.Bytes(), -1) {
		out = append(out, m[1]...)
	}
	return out
}

// The uptime graph moved onto the shared line-chart helper; its page must
// come out byte for byte as it did before the move.
func TestUptimeGraphRendersAsBefore(t *testing.T) {
	pdf := chartTestDoc()
	data := sampleReportData()
	drawPDFUptimeGraph(pdf, data.UptimeSeries, data.EffectiveSLA, time.UTC)
	got := pageStreams(t, pdf)

	golden := filepath.Join("testdata", "uptime_graph.golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading %s (run once with -update-golden on the code before the move): %v", golden, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("uptime graph drawing changed:\n got %d bytes\nwant %d bytes", len(got), len(want))
	}
}
```

- [ ] **Step 2: Record the golden file from the current code, then check it passes**

Run from `backend/`:

```bash
go test ./internal/services/ -run TestUptimeGraphRendersAsBefore -update-golden -v
go test ./internal/services/ -run TestUptimeGraphRendersAsBefore -v
```

Expected: both PASS, and `internal/services/testdata/uptime_graph.golden` exists, about 1.3 KB of PDF drawing operators that start `0 J` / `0 j` and contain `(Uptime vs. SLA target)Tj` and `(SLA target 99.50%)Tj`. This file is the old graph's exact output. Do not regenerate it after Step 7.

- [ ] **Step 3: Write the failing tests for the helper and the formatting**

In `backend/internal/services/pdf_chart_test.go`, replace the import block with:

```go
import (
	"bytes"
	"flag"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
)
```

and append:

```go
func TestDrawLineChartDrawsTicksLinesAndReference(t *testing.T) {
	pdf := chartTestDoc()
	y0 := pdf.GetY()
	drawLineChart(pdf, lineChart{
		Height: 40, AxisLabelW: 18, Lo: 0, Hi: 1e9, TopPad: 3, Ticks: 4,
		TickLabel: func(v float64) string { return formatValue(v, "bps") },
		Ref:       &chartRef{Value: 640e6, Label: "Billable 95th 640 Mbps", Color: pdfWarning},
		Lines: []chartLine{
			{X: []float64{0, 0.5, 1}, Y: []float64{100e6, 640e6, 200e6}, Color: pdfAccent, Width: 0.5},
			{X: []float64{0, 1}, Y: []float64{50e6, 300e6}, Color: pdfSuccess, Width: 0.5},
		},
		XLabels:      []chartXLabel{{At: 0, Text: "Sep 1", Align: "L"}, {At: 1, Text: "Oct 1", Align: "R"}},
		LabelsInside: true,
	})
	if got, want := pdf.GetY(), y0+40+9; got != want {
		t.Errorf("Y after the chart = %.2f, want %.2f (height + 9 mm of labels)", got, want)
	}
	content := string(pageStreams(t, pdf))
	for _, want := range []string{"(0 bps)Tj", "(250 Mbps)Tj", "(500 Mbps)Tj", "(750 Mbps)Tj", "(1 Gbps)Tj",
		"(Billable 95th 640 Mbps)Tj", "(Sep 1)Tj", "(Oct 1)Tj"} {
		if !strings.Contains(content, want) {
			t.Errorf("chart is missing %s", want)
		}
	}
	// Two segments for the three-point line, one for the two-point line.
	if got := strings.Count(content, " l S"); got != 3 {
		t.Errorf("drew %d line segments, want 3", got)
	}
}

func TestNiceCeil(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0, 1}, {-5, 1}, {0.37, 0.5}, {1, 1}, {7, 10}, {92.5, 100}, {130, 200}, {240, 250}, {640e6, 1e9}, {1.2e9, 2e9},
	}
	for _, c := range cases {
		if got := niceCeil(c.in); math.Abs(got-c.want) > 1e-9*c.want {
			t.Errorf("niceCeil(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
```

Create `backend/internal/services/pdf_units_test.go`:

```go
package services

import (
	"math"
	"testing"
)

func TestFormatValue(t *testing.T) {
	cases := []struct {
		v    float64
		unit string
		want string
	}{
		{0, "bps", "0 bps"},
		{950, "bps", "950 bps"},
		{12_345, "bps", "12.3 Kbps"},
		{640e6, "bps", "640 Mbps"},
		{1.25e9, "bps", "1.25 Gbps"},
		{999.6e6, "bps", "1 Gbps"}, // would round to "1000 Mbps"
		{2.5e12, "bps", "2500 Gbps"},
		{1.5, "per_min", "1.5/min"},
		{0.25, "per_min", "0.25/min"},
		{12.34, "%", "12.3%"},
		{40, "%", "40%"},
		{92.5, "%", "92.5%"},
		{0.0030787, "bool", "0.3%"},
		{1, "bool", "100%"},
		{31.5, "°C", "31.5 °C"},
		{230, "V", "230 V"},
		{42, "min", "42 min"},
		{1200, "rpm", "1200 rpm"},
		{3.14159, "", "3.14"},
		{math.NaN(), "bps", "-"},
		{math.Inf(1), "%", "-"},
	}
	for _, c := range cases {
		if got := formatValue(c.v, c.unit); got != c.want {
			t.Errorf("formatValue(%v, %q) = %q, want %q", c.v, c.unit, got, c.want)
		}
	}
}

func TestFormatTotal(t *testing.T) {
	cases := []struct {
		v    float64
		unit string
		want string
	}{
		{512, "bps", "512 B"},
		{1.5e6, "bps", "1.5 MB"},
		{2.1e12, "bps", "2.1 TB"},
		{3.25e15, "bps", "3.25 PB"},
		{0, "per_min", "0"},
		{999, "per_min", "999"},
		{1000, "per_min", "1,000"},
		{1234567.4, "per_min", "1,234,567"},
		{42, "%", ""},
		{42, "bool", ""},
	}
	for _, c := range cases {
		if got := formatTotal(c.v, c.unit); got != c.want {
			t.Errorf("formatTotal(%v, %q) = %q, want %q", c.v, c.unit, got, c.want)
		}
	}
}

func TestFormatDurationHM(t *testing.T) {
	cases := map[float64]string{
		0: "0 m", -5: "0 m", 29: "< 1 m", 30: "1 m", 2700: "45 m", 3600: "1 h", 7980: "2 h 13 m", 90000: "25 h",
	}
	for in, want := range cases {
		if got := formatDurationHM(in); got != want {
			t.Errorf("formatDurationHM(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatChange(t *testing.T) {
	cases := []struct {
		change *float64
		isNew  bool
		want   string
	}{
		{f64(18), false, "+18%"},
		{f64(-25), false, "-25%"},
		{f64(2), false, "+2%"},
		{f64(0.44), false, "+0.4%"},
		{f64(-3.46), false, "-3.5%"},
		{f64(9.96), false, "+10%"},
		{f64(0.04), false, "0%"},
		{f64(-0.04), false, "0%"},
		{f64(150), false, "+150%"},
		{nil, true, "new"},
		{f64(18), true, "new"},
		{nil, false, ""},
		{f64(math.Inf(1)), false, ""},
	}
	for _, c := range cases {
		if got := formatChange(c.change, c.isNew); got != c.want {
			t.Errorf("formatChange(%v, %v) = %q, want %q", c.change, c.isNew, got, c.want)
		}
	}
}
```

- [ ] **Step 4: Run them to see them fail**

Run: `go test ./internal/services/ -run 'TestDrawLineChart|TestNiceCeil|TestFormatValue|TestFormatTotal|TestFormatDurationHM|TestFormatChange' -v`
Expected: FAIL to build with `undefined: drawLineChart`, `undefined: lineChart`, `undefined: formatValue` and similar.

- [ ] **Step 5: Write the unit formatting**

Create `backend/internal/services/pdf_units.go`:

```go
package services

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Number formatting for Metrics reports, in the PDF and the email summary.
// Rates and sizes are decimal (1 Mbps = 1,000,000 bps, 1 GB = 10^9 bytes),
// the way carriers bill. Every function prints "-" for NaN or infinity
// rather than "NaN" on a customer's report.

var (
	bpsUnits  = []string{"bps", "Kbps", "Mbps", "Gbps"}
	byteUnits = []string{"B", "KB", "MB", "GB", "TB", "PB"}
)

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// trimZeros drops trailing zeros after a decimal point ("2.10" -> "2.1",
// "40.0" -> "40"), and a negative zero.
func trimZeros(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		return "0"
	}
	return s
}

// formatSig prints v with about three significant digits: 640, 12.5, 2.1, 0.25.
func formatSig(v float64) string {
	if !finite(v) {
		return "-"
	}
	prec := 2
	switch a := math.Abs(v); {
	case a >= 99.95:
		prec = 0
	case a >= 9.995:
		prec = 1
	}
	return trimZeros(strconv.FormatFloat(v, 'f', prec, 64))
}

// scaleDecimal steps v up through units in thousands. A value that would
// print as 1000 of one unit moves to the next (999.6 Mbps is "1 Gbps").
func scaleDecimal(v float64, units []string) string {
	if !finite(v) {
		return "-"
	}
	i := 0
	for i < len(units)-1 && math.Abs(v) >= 999.5 {
		v /= 1000
		i++
	}
	return formatSig(v) + " " + units[i]
}

// formatBps prints a rate: "950 bps", "12.3 Kbps", "640 Mbps", "1.25 Gbps".
func formatBps(v float64) string { return scaleDecimal(v, bpsUnits) }

// formatBytes prints a byte total: "512 KB", "1.5 MB", "2.1 TB".
func formatBytes(v float64) string { return scaleDecimal(v, byteUnits) }

// formatCount prints a whole count with thousands separators: "1,234,567".
func formatCount(v float64) string {
	if !finite(v) {
		return "-"
	}
	n := int64(math.Round(v))
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return sign + b.String()
}

// formatPercent prints a percentage with one decimal at most: "12.3%", "40%".
func formatPercent(p float64) string {
	if !finite(p) {
		return "-"
	}
	return trimZeros(strconv.FormatFloat(p, 'f', 1, 64)) + "%"
}

// formatDurationHM prints a length of time in hours and minutes: "2 h 13 m",
// "45 m", "1 h". Under half a minute is "< 1 m", nothing at all "0 m".
func formatDurationHM(seconds float64) string {
	if !finite(seconds) || seconds <= 0 {
		return "0 m"
	}
	mins := int64(math.Round(seconds / 60))
	h, m := mins/60, mins%60
	switch {
	case mins == 0:
		return "< 1 m"
	case h == 0:
		return fmt.Sprintf("%d m", m)
	case m == 0:
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d m", h, m)
}

// formatValue prints a statistic in its metric's unit. bps scales to
// Kbps/Mbps/Gbps; per_min is a rate per minute; bool is the share of time
// true (its average, 0-1); %, °C, V, min and custom units print verbatim
// after the number.
func formatValue(v float64, unit string) string {
	if !finite(v) {
		return "-"
	}
	switch unit {
	case "bps":
		return formatBps(v)
	case "per_min":
		return formatSig(v) + "/min"
	case "%":
		return formatPercent(v)
	case "bool":
		return formatPercent(v * 100)
	case "":
		return formatSig(v)
	}
	return formatSig(v) + " " + unit
}

// formatTotal prints a row's total: bytes for bps, a count for per_min.
// Other units have no total and print "".
func formatTotal(v float64, unit string) string {
	switch unit {
	case "bps":
		return formatBytes(v)
	case "per_min":
		return formatCount(v)
	}
	return ""
}

// formatChange prints a change on the previous period: "+18%", "-3.5%",
// "0%", or "new" when there was nothing to compare with. A missing change
// is "".
func formatChange(change *float64, isNew bool) string {
	if isNew {
		return "new"
	}
	if change == nil || !finite(*change) {
		return ""
	}
	prec := 0
	if math.Abs(*change) < 9.95 {
		prec = 1
	}
	s := trimZeros(strconv.FormatFloat(*change, 'f', prec, 64))
	if s == "0" {
		return "0%"
	}
	if !strings.HasPrefix(s, "-") {
		s = "+" + s
	}
	return s + "%"
}
```

- [ ] **Step 6: Write the line-chart helper**

Create `backend/internal/services/pdf_chart.go`:

```go
package services

import (
	"math"

	"github.com/go-pdf/fpdf"
)

// A line chart drawn from fpdf's own line and rectangle primitives, shared by
// the Uptime report's SLA graph and the Metrics report's charts. The caller
// decides the y range, the tick labels and where the points sit; this only
// draws.

// chartLine is one line: points at X (0 = the plot's left edge, 1 = its
// right edge) with values Y.
type chartLine struct {
	X, Y  []float64
	Color [3]int
	Width float64 // mm
}

// chartRef is a flat reference line across the plot, labelled above its
// left end.
type chartRef struct {
	Value float64
	Label string
	Color [3]int
}

// chartXLabel is an x-axis label at At (0-1 across the plot).
type chartXLabel struct {
	At    float64
	Text  string
	Align string // "L", "C" or "R"
}

// lineChart is everything drawLineChart needs.
type lineChart struct {
	Height     float64 // plot box height, mm
	AxisLabelW float64 // room left of the plot for the y-axis labels, mm
	Lo, Hi     float64 // y range; Hi must be above Lo
	// TopPad keeps a value at Hi off the box's top border, where it would
	// read as part of the frame rather than as data.
	TopPad    float64
	Ticks     int // gridlines from Lo to Hi: Ticks+1 of them
	TickLabel func(v float64) string
	Ref       *chartRef // drawn under the lines
	Lines     []chartLine
	XLabels   []chartXLabel
	// LabelsInside keeps the x labels within the plot: "L" starts at its
	// point and "R" ends at it. Off, every label sits in a 30 mm box centred
	// on its point, the uptime graph's original placement.
	LabelsInside bool
}

// drawLineChart draws c across the content width from the current Y, then
// moves Y below the x-axis labels (Height + 9 mm further down). It does not
// check for room on the page: callers do, because only they know what has to
// stay together with the chart (a heading, a legend).
func drawLineChart(pdf *fpdf.Fpdf, c lineChart) {
	x0 := pdfMarginLeft + c.AxisLabelW
	w := pdfContentW - c.AxisLabelW
	y0 := pdf.GetY()
	usableH := c.Height - c.TopPad
	yFor := func(v float64) float64 {
		frac := (v - c.Lo) / (c.Hi - c.Lo)
		if frac < 0 {
			frac = 0
		} else if frac > 1 {
			frac = 1
		}
		return y0 + c.TopPad + usableH*(1-frac)
	}

	pdf.SetFont("Helvetica", "", 7)
	for i := 0; i <= c.Ticks; i++ {
		frac := float64(i) / float64(c.Ticks)
		val := c.Lo + (c.Hi-c.Lo)*frac
		y := yFor(val)
		setColor(pdf, pdfRule, true)
		pdf.Rect(x0, y, w, 0.15, "F")
		setColor(pdf, pdfMuted, false)
		pdf.SetXY(pdfMarginLeft, y-2)
		pdf.CellFormat(c.AxisLabelW-1, 4, pdfText(c.TickLabel(val)), "", 0, "R", false, 0, "")
	}
	setDrawColor(pdf, pdfRule)
	pdf.SetLineWidth(0.2)
	pdf.Rect(x0, y0, w, c.Height, "D")

	if c.Ref != nil {
		ry := yFor(c.Ref.Value)
		setColor(pdf, c.Ref.Color, true)
		pdf.Rect(x0, ry-0.25, w, 0.5, "F")
		pdf.SetXY(x0+2, ry-5)
		pdf.SetFont("Helvetica", "B", 7)
		setColor(pdf, c.Ref.Color, false)
		pdf.CellFormat(60, 4, pdfText(c.Ref.Label), "", 0, "L", false, 0, "")
	}

	for _, l := range c.Lines {
		setDrawColor(pdf, l.Color)
		pdf.SetLineWidth(l.Width)
		for i := 0; i+1 < len(l.X) && i+1 < len(l.Y); i++ {
			pdf.Line(x0+w*l.X[i], yFor(l.Y[i]), x0+w*l.X[i+1], yFor(l.Y[i+1]))
		}
	}
	pdf.SetLineWidth(0.2)

	pdf.SetFont("Helvetica", "", 7)
	setColor(pdf, pdfMuted, false)
	for _, l := range c.XLabels {
		x := x0 + w*l.At
		left := x - 15
		if c.LabelsInside {
			switch l.Align {
			case "L":
				left = x
			case "R":
				left = x - 30
			}
		}
		pdf.SetXY(left, y0+c.Height+1.5)
		pdf.CellFormat(30, 4, pdfText(l.Text), "", 0, l.Align, false, 0, "")
	}

	pdf.SetY(y0 + c.Height + 9)
}

// niceCeil rounds v up to the next 1, 2, 2.5 or 5 times a power of ten, so
// axis ticks land on round numbers. Zero or less gives 1.
func niceCeil(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 1
	}
	base := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*base >= v {
			return m * base
		}
	}
	return 10 * base
}
```

The helper's drawing calls run in exactly the order the old graph used: gridlines and labels, box, reference bar and label, lines, x labels. With `LabelsInside` false, every x label sits in a 30 mm box centred on its point, as the old graph placed them. That keeps the uptime page byte-identical.

- [ ] **Step 7: Move the uptime graph onto the helper**

In `backend/internal/services/pdf_renderer.go`, `drawPDFUptimeGraph` keeps everything up to and including its page-fit guard and the `drawSectionHeading(pdf, "Uptime vs. SLA target")` line. Replace everything from that `drawSectionHeading` line to the function's closing brace with:

```go
	drawSectionHeading(pdf, "Uptime vs. SLA target")

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

	n := len(series)
	line := chartLine{X: make([]float64, n), Y: make([]float64, n), Color: pdfAccent, Width: 0.6}
	for i, p := range series {
		line.X[i] = float64(i) / float64(n-1)
		line.Y[i] = p.Uptime
	}
	// X-axis date labels: first, last, and middle sample only, so the axis
	// stays readable instead of crowding thirty overlapping labels.
	dateLabel := func(i int, align string) chartXLabel {
		return chartXLabel{At: float64(i) / float64(n-1), Text: series[i].Date.In(loc).Format("Jan 2"), Align: align}
	}
	labels := []chartXLabel{dateLabel(0, "L"), dateLabel(n-1, "R")}
	if mid := (n - 1) / 2; mid > 0 && mid < n-1 {
		labels = append(labels, dateLabel(mid, "C"))
	}

	drawLineChart(pdf, lineChart{
		Height:     chartH,
		AxisLabelW: 14,
		Lo:         lo,
		Hi:         hi,
		// A small top pad keeps a genuine 100% value's line visually separated
		// from the axis box's own top border. Without it, a flat 100% record
		// (the common "everything's fine" case) draws its line exactly on top
		// of the box's top edge - two things at the same coordinate read as
		// one, which looks like the graph never rendered at all.
		TopPad:    3,
		Ticks:     4,
		TickLabel: func(v float64) string { return fmt.Sprintf("%.0f%%", v) },
		Ref:       &chartRef{Value: slaTarget, Label: fmt.Sprintf("SLA target %.2f%%", slaTarget), Color: pdfWarning},
		Lines:     []chartLine{line},
		XLabels:   labels,
	})
}
```

`math` and `fmt` are still imported and used. `pdfText` is not needed here, because the helper converts tick labels, reference labels and x labels.

- [ ] **Step 8: Run the new tests, the golden test and the existing renderer tests**

Run: `go test ./internal/services/ -run 'TestUptimeGraphRendersAsBefore|TestDrawLineChart|TestNiceCeil|TestFormat|TestRenderReportToPDF|TestPDF' -v`
Expected: PASS for all, including `TestUptimeGraphRendersAsBefore` (byte-identical to the recorded old graph) and `TestRenderReportToPDFHonoursReportType` (still finds `SLA target 99.50%`).

Then: `go vet ./internal/services/`. Expected: no output.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/services/pdf_chart.go backend/internal/services/pdf_units.go \
  backend/internal/services/pdf_chart_test.go backend/internal/services/pdf_units_test.go \
  backend/internal/services/testdata/uptime_graph.golden backend/internal/services/pdf_renderer.go
git commit -m "feat(reports): a shared line-chart helper and unit formatting for metric reports

The uptime graph now draws through the helper, byte for byte as before
(pinned by a golden file of its page recorded from the old code).

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: The Metrics PDF: headline page and charts

`RenderReportToPDF` gains the Metrics layout. This task draws the headline page and the charts:

- **Headline page:** the scope and the period in words with the comparison period, one tile per metric, Running hot, the busiest rows, and notes on what was left out.
- **Charts:** one chart per metric for the whole scope, two to a page, with the period's 95th as a reference line.

Task 12 adds the tables, the busiest rows' small charts, and the empty and no-data pages.

**Files:**
- Create: `backend/internal/services/pdf_metrics.go`
- Create: `backend/internal/services/pdf_metrics_charts.go`
- Create: `backend/internal/services/pdf_metrics_test.go`
- Modify: `backend/internal/services/pdf_renderer.go` (`RenderReportToPDF`'s doc comment and its `switch reportType`)

**Interfaces:**
- Consumes:
  - Task 10: `lineChart`, `chartLine`, `chartRef`, `chartXLabel`, `drawLineChart`, `niceCeil`, `finite`, `formatValue`, `formatBytes`, `formatPercent`, `formatChange`; test helpers `chartTestDoc` and `pageStreams` (not used here).
  - Part A: `services.MetricsReportData`, `MetricsTile`, `MetricsTable`, `MetricsRow`, `RowStats`, `HotPort`, `MetricsChart`, `ChartLine`, `ChartPoint` (`report_metrics_data.go`), with `Ports` and `Devices` per Contract change 1. Also `ReportData.Network *MetricsReportData`, `models.ReportTypeMetrics` and `models.MaxReportSubjects`.
  - Existing: `drawSectionHeading`, `setColor`, `setDrawColor`, `pdfText`, `ReportData.ReportLocation()`; test helpers `pdfDrawnText(t, path) string` (`pdf_encoding_test.go`) and `f64` (`pdf_renderer_test.go`).
- Produces (Task 12 and Task 13 use these):
  - `func drawPDFMetricsReport(pdf *fpdf.Fpdf, data *ReportData)`. Task 12 replaces its body.
  - `func drawMetricsTitle(pdf *fpdf.Fpdf, data *ReportData)`. It draws the scope label and the period line.
  - `func drawMetricsHeadline(pdf *fpdf.Fpdf, data *ReportData)`
  - `func drawMetricsNotes(pdf *fpdf.Fpdf, n *MetricsReportData)` and `func metricsNotes(n *MetricsReportData) []string`
  - `func metricsSubjects(n *MetricsReportData) string`. It gives `"214 ports"`, `"3 devices"`, or `""`.
  - `func windowLabel(start, end time.Time, loc *time.Location) string`
  - `func drawMetricsCharts(pdf *fpdf.Fpdf, data *ReportData)` and `func drawMetricsChart(pdf *fpdf.Fpdf, data *ReportData, c MetricsChart, height float64)`
  - `func drawPDFNotice(pdf *fpdf.Fpdf, message string, edge [3]int)`. It draws a one-line panel 18 mm high.
  - `func fitText(pdf *fpdf.Fpdf, s string, w float64) string`. It converts with `pdfText` and cuts with "…" to fit `w` mm in the current font. Its result must never go through `pdfText` again.
  - `func countNoun(n int, one, many string) string`
  - Test helpers in `pdf_metrics_test.go`:
    - `sampleMetricsData() *ReportData` is September 2026 UTC, compared with August. It has:
      - 4 tiles: Traffic 640 Mbps / 2.1 TB / +18%; Busy 12.34% / 95th 40% / new; Errors 1.5/min / -25%; Battery charge with no data.
      - 3 tables:
        - Traffic pair: core-sw1 · Gi1/0/1 (uplink to annex), edge-sw2 · Gi0/48 at 72% coverage and new, and edge-sw3 · Gi0/1 with no data.
        - Busy pair.
        - Bool "On battery": ups-1, 7,980 s true.
      - 3 charts and 1 row chart.
      - `Ports: 3, CappedOut: 140, Unavailable: 2, Skipped: ["old_metric"], LowCoverage: 1`.
    - `renderMetrics(t, data) (text, path string)`
    - `hasLine(text, line string) bool`

The tile `Kind` values are the contract's string literals `"traffic"`, `"percent"` and `"other"`. If Part A defines constants for them, use the constants in place of the literals.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/services/pdf_metrics_test.go`:

```go
package services

import (
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// sampleMetricsData is a hand-built Metrics report for September 2026 (UTC),
// compared with August: three ports, a traffic pair, a busy pair and a UPS's
// on-battery metric.
func sampleMetricsData() *ReportData {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	core, edge2, edge3 := "core-sw1 · Gi1/0/1 (uplink to annex)", "edge-sw2 · Gi0/48", "edge-sw3 · Gi0/1"
	pts := func(vs ...float64) []ChartPoint {
		out := make([]ChartPoint, len(vs))
		for i, v := range vs {
			out[i] = ChartPoint{T: start.Add(time.Duration(i) * 10 * 24 * time.Hour), V: v}
		}
		return out
	}
	return &ReportData{
		ReportName:     "WAN monthly",
		TimeRangeStart: start,
		TimeRangeEnd:   end,
		Location:       time.UTC,
		Network: &MetricsReportData{
			ScopeLabel: "WAN, Uplink ports at HQ, Annex",
			PrevStart:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			PrevEnd:    start,
			RankLabel:  "Traffic",
			Tiles: []MetricsTile{
				{Label: "Traffic", Unit: "bps", Kind: "traffic", First: 640e6, Second: f64(2.1e12), Change: f64(18)},
				{Label: "Busy", Unit: "%", Kind: "percent", First: 12.34, Second: f64(40), New: true},
				{Label: "Errors", Unit: "per_min", Kind: "other", First: 1.5, Change: f64(-25)},
				{Label: "Battery charge", Unit: "%", Kind: "percent", NoData: true},
			},
			Tables: []MetricsTable{
				{Title: "Traffic", Unit: "bps", Paired: true, Billable: true, HasTotal: true, Rows: []MetricsRow{
					{Name: core, In: &RowStats{Avg: 200e6, Peak: 900e6, P95: 640e6, Total: f64(1.2e12)},
						Out:      &RowStats{Avg: 100e6, Peak: 500e6, P95: 300e6, Total: f64(0.9e12)},
						Billable: f64(640e6), Change: f64(18), Coverage: 100},
					{Name: edge2, In: &RowStats{Avg: 40e6, Peak: 150e6, P95: 120e6, Total: f64(9e10)},
						Out:      &RowStats{Avg: 20e6, Peak: 90e6, P95: 60e6, Total: f64(4e10)},
						Billable: f64(120e6), New: true, Coverage: 72, LowCoverage: true},
					{Name: edge3, NoData: true},
				}},
				{Title: "Busy", Unit: "%", Paired: true, Rows: []MetricsRow{
					{Name: core, In: &RowStats{Avg: 20, Peak: 99, P95: 92.5}, Out: &RowStats{Avg: 10, Peak: 50, P95: 41},
						Change: f64(5), Coverage: 100},
				}},
				{Title: "On battery", Unit: "bool", Rows: []MetricsRow{
					{Name: "ups-1", In: &RowStats{Avg: 0.0030787, Peak: 1, P95: 0, TrueSeconds: f64(7980)}, Coverage: 100},
				}},
			},
			Busiest:    []string{core, edge2},
			RunningHot: []HotPort{{Name: core, P95In: 92.5, P95Out: 41}},
			Charts: []MetricsChart{
				{Title: "Traffic", Unit: "bps", Reference: f64(640e6), Lines: []ChartLine{
					{Label: "In", Points: pts(100e6, 640e6, 300e6, 200e6)}, {Label: "Out", Points: pts(50e6, 300e6, 100e6, 80e6)}}},
				{Title: "Busy", Unit: "%", Reference: f64(92.5), Lines: []ChartLine{
					{Label: "In", Points: pts(10, 92.5, 30, 20)}, {Label: "Out", Points: pts(5, 41, 12, 9)}}},
				{Title: "On battery", Unit: "bool", Reference: f64(0), Lines: []ChartLine{
					{Label: "On battery", Points: pts(0, 0, 1, 0)}}},
			},
			RowCharts: []MetricsChart{
				{Title: core, Unit: "bps", Lines: []ChartLine{
					{Label: "In", Points: pts(100e6, 640e6, 300e6, 200e6)}, {Label: "Out", Points: pts(50e6, 300e6, 100e6, 80e6)}}},
			},
			Rows: 3, Ports: 3, CappedOut: 140, Unavailable: 2, Skipped: []string{"old_metric"}, LowCoverage: 1,
		},
	}
}

// hasLine reports whether text, as pdfDrawnText returns it, has a drawn
// string equal to line.
func hasLine(text, line string) bool {
	return strings.Contains("\n"+text+"\n", "\n"+line+"\n")
}

// renderMetrics renders data as a Metrics report and returns the drawn text
// and the file's path.
func renderMetrics(t *testing.T, data *ReportData) (string, string) {
	t.Helper()
	r, err := NewPDFRendererService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name, err := r.RenderReportToPDF(data, models.ReportTypeMetrics, "metrics")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	path, err := r.GetPDFPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return pdfDrawnText(t, path), path
}

func TestMetricsPDFHeadline(t *testing.T) {
	text, _ := renderMetrics(t, sampleMetricsData())
	for _, want := range []string{
		"WAN, Uplink ports at HQ, Annex",
		"September 2026, compared with August 2026",
		// Traffic tile: billable 95th, volume, change.
		"TRAFFIC", "640 Mbps", "billable 95th, 2.1 TB moved", "+18% vs August 2026",
		// Percent tile: average, 95th.
		"12.3%", "average, 95th 40%",
		// Other tile: average in its unit.
		"1.5/min", "-25% vs August 2026",
		"Running hot", "in 92.5%, out 41%",
		"Busiest 2 by Traffic", "edge-sw2",
		"1 row with data for less than 90% of the period, marked in the tables.",
		"Left out 140 ports beyond the 500-port limit; the report includes the 500 busiest.",
		"Left out 2 chosen ports, devices or sites that are no longer available to you.",
		"Left out metrics that no longer exist: old_metric.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("headline is missing %q", want)
		}
	}
	// The percent tile is new this period; the battery tile has no data.
	for _, want := range []string{"new", "No data"} {
		if !hasLine(text, want) {
			t.Errorf("headline is missing a tile reading %q", want)
		}
	}
}

func TestMetricsPDFChartsCarryTheReferenceLine(t *testing.T) {
	text, _ := renderMetrics(t, sampleMetricsData())
	for _, want := range []string{"Charts", "Billable 95th 640 Mbps", "95th 92.5%", "95th 0%", "1 Gbps", "Sep 1", "Oct 1"} {
		if !strings.Contains(text, want) {
			t.Errorf("charts are missing %q", want)
		}
	}
}

func TestWindowLabel(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	d := func(y int, m time.Month, day, h, mi int, loc *time.Location) time.Time {
		return time.Date(y, m, day, h, mi, 0, 0, loc)
	}
	cases := []struct {
		start, end time.Time
		loc        *time.Location
		want       string
	}{
		{d(2026, 9, 1, 0, 0, time.UTC), d(2026, 10, 1, 0, 0, time.UTC), time.UTC, "September 2026"},
		// November 2026 in Chicago is 721 hours long (DST ends on the 1st).
		{d(2026, 11, 1, 0, 0, chicago), d(2026, 12, 1, 0, 0, chicago), chicago, "November 2026"},
		{d(2026, 7, 1, 0, 0, time.UTC), d(2026, 10, 1, 0, 0, time.UTC), time.UTC, "Q3 2026"},
		{d(2026, 1, 1, 0, 0, time.UTC), d(2027, 1, 1, 0, 0, time.UTC), time.UTC, "2026"},
		{d(2026, 9, 7, 0, 0, time.UTC), d(2026, 9, 14, 0, 0, time.UTC), time.UTC, "Week of September 7, 2026"},
		{d(2026, 9, 3, 0, 0, time.UTC), d(2026, 10, 3, 0, 0, time.UTC), time.UTC, "September 3, 2026 to October 3, 2026"},
		{d(2026, 9, 3, 14, 5, time.UTC), d(2026, 10, 3, 14, 5, time.UTC), time.UTC, "Sep 3, 2026 14:05 to Oct 3, 2026 14:05"},
		// The same instants, read in Chicago.
		{d(2026, 9, 1, 5, 0, time.UTC), d(2026, 10, 1, 5, 0, time.UTC), chicago, "September 2026"},
	}
	for _, c := range cases {
		if got := windowLabel(c.start, c.end, c.loc); got != c.want {
			t.Errorf("windowLabel(%v, %v) = %q, want %q", c.start, c.end, got, c.want)
		}
	}
}

func TestFitText(t *testing.T) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "", 10)
	if got := fitText(pdf, "core-sw1", 50); got != "core-sw1" {
		t.Errorf("a name that fits changed: %q", got)
	}
	if got := fitText(pdf, "Café", 50); got != "Caf\xe9" {
		t.Errorf("fitText(Café) = %q, want it converted to cp1252", got)
	}
	long := strings.Repeat("very-long-device-name-", 10) + "END"
	got := fitText(pdf, long, 40)
	if !strings.HasSuffix(got, "\x85") || strings.Contains(got, "END") {
		t.Errorf("long name = %q, want it cut with an ellipsis", got)
	}
	if w := pdf.GetStringWidth(got); w > 40 {
		t.Errorf("cut name is %.1f mm wide, more than 40", w)
	}
	if more := fitText(pdf, long, 41); len(more) < len(got) {
		t.Errorf("a wider column kept less: %q vs %q", more, got)
	}
}
```

The expected strings are worked out by hand from the fixture:
- 640e6 bps prints as "640 Mbps" and 2.1e12 bytes as "2.1 TB".
- 12.34% prints as "12.3%".
- The traffic chart's highest value and its reference are both 640 Mbps, and `niceCeil` rounds that up to a top tick of "1 Gbps".
- The bool chart's reference of 0 prints as "95th 0%".
- `drawPDFHeader` writes "September 01" for the period, so a drawn "Sep 1" can only come from the chart's x axis.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/services/ -run 'TestMetricsPDF|TestWindowLabel|TestFitText' -v`
Expected: FAIL to build with `undefined: windowLabel` and `undefined: fitText`.

- [ ] **Step 3: Write the headline page**

Create `backend/internal/services/pdf_metrics.go`:

```go
package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// The Metrics report layout: a headline page (scope, tiles, running hot,
// busiest, notes), one chart per metric for the whole scope
// (pdf_metrics_charts.go), the tables and the busiest rows' small charts
// (pdf_metrics_tables.go).

const (
	metricsTileH    = 26.0
	metricsHotShown = 10 // running-hot rows listed on the headline page
)

// drawPDFMetricsReport assembles the fixed Metrics report layout.
func drawPDFMetricsReport(pdf *fpdf.Fpdf, data *ReportData) {
	if data.Network == nil {
		drawPDFNotice(pdf, "No network data was gathered for this report.", pdfWarning)
		return
	}
	drawMetricsHeadline(pdf, data)
	drawMetricsCharts(pdf, data)
}

// drawMetricsTitle writes the scope in words and the period with the one it
// is compared with.
func drawMetricsTitle(pdf *fpdf.Fpdf, data *ReportData) {
	n := data.Network
	if n.ScopeLabel != "" {
		pdf.SetFont("Helvetica", "B", 13)
		setColor(pdf, pdfInk, false)
		pdf.MultiCell(pdfContentW, 6, pdfText(n.ScopeLabel), "", "L", false)
	}
	pdf.SetFont("Helvetica", "", 10)
	setColor(pdf, pdfMuted, false)
	pdf.MultiCell(pdfContentW, 5, pdfText(metricsPeriodLine(data)), "", "L", false)
	pdf.Ln(3)
}

// metricsPeriodLine is "214 ports · September 2026, compared with August 2026".
func metricsPeriodLine(data *ReportData) string {
	n := data.Network
	loc := data.ReportLocation()
	line := windowLabel(data.TimeRangeStart, data.TimeRangeEnd, loc)
	if n.PrevEnd.After(n.PrevStart) {
		line += ", compared with " + windowLabel(n.PrevStart, n.PrevEnd, loc)
	}
	if subjects := metricsSubjects(n); subjects != "" {
		line = subjects + " · " + line
	}
	return line
}

// metricsSubjects counts what the report covers: "214 ports" or "3 devices".
func metricsSubjects(n *MetricsReportData) string {
	switch {
	case n.Ports > 0:
		return countNoun(n.Ports, "port", "ports")
	case n.Devices > 0:
		return countNoun(n.Devices, "device", "devices")
	}
	return ""
}

// windowLabel names a reporting window: "September 2026", "Q3 2026",
// "2026", "Week of September 7, 2026", or its two ends.
func windowLabel(start, end time.Time, loc *time.Location) string {
	s, e := start.In(loc), end.In(loc)
	midnight := func(t time.Time) bool {
		return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0
	}
	if midnight(s) {
		switch {
		case s.Day() == 1 && s.Month() == time.January && e.Equal(s.AddDate(1, 0, 0)):
			return s.Format("2006")
		case s.Day() == 1 && (s.Month()-1)%3 == 0 && e.Equal(s.AddDate(0, 3, 0)):
			return fmt.Sprintf("Q%d %d", (int(s.Month())-1)/3+1, s.Year())
		case s.Day() == 1 && e.Equal(s.AddDate(0, 1, 0)):
			return s.Format("January 2006")
		case e.Equal(s.AddDate(0, 0, 7)):
			return "Week of " + s.Format("January 2, 2006")
		}
		if midnight(e) {
			return s.Format("January 2, 2006") + " to " + e.Format("January 2, 2006")
		}
	}
	return s.Format("Jan 2, 2006 15:04") + " to " + e.Format("Jan 2, 2006 15:04")
}

// drawMetricsHeadline draws page one after the shared header.
func drawMetricsHeadline(pdf *fpdf.Fpdf, data *ReportData) {
	n := data.Network
	drawMetricsTitle(pdf, data)
	prev := windowLabel(n.PrevStart, n.PrevEnd, data.ReportLocation())
	drawMetricsTiles(pdf, n.Tiles, prev)
	drawMetricsRunningHot(pdf, n.RunningHot)
	drawMetricsBusiest(pdf, n)
	drawMetricsNotes(pdf, n)
}

// drawMetricsTiles lays the tiles out three to a row.
func drawMetricsTiles(pdf *fpdf.Fpdf, tiles []MetricsTile, prev string) {
	if len(tiles) == 0 {
		return
	}
	const perRow, gap = 3, 4.0
	w := (pdfContentW - gap*(perRow-1)) / perRow
	_, pageH := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	y := pdf.GetY()
	for i, t := range tiles {
		col := i % perRow
		if col == 0 && i > 0 {
			y += metricsTileH + gap
		}
		if col == 0 && y+metricsTileH > pageH-bottom {
			pdf.AddPage()
			y = pdf.GetY()
		}
		drawMetricsTile(pdf, pdfMarginLeft+float64(col)*(w+gap), y, w, t, prev)
	}
	pdf.SetY(y + metricsTileH + 4)
}

func drawMetricsTile(pdf *fpdf.Fpdf, x, y, w float64, t MetricsTile, prev string) {
	setColor(pdf, pdfPanel, true)
	pdf.Rect(x, y, w, metricsTileH, "F")
	setColor(pdf, pdfAccent, true)
	pdf.Rect(x, y, 1.2, metricsTileH, "F")
	textW := w - 6

	pdf.SetFont("Helvetica", "", 7)
	setColor(pdf, pdfMuted, false)
	pdf.SetXY(x+4, y+2.5)
	pdf.CellFormat(textW, 4, fitText(pdf, strings.ToUpper(t.Label), textW-2), "", 0, "L", false, 0, "")

	value, caption := tileFigures(t)
	pdf.SetFont("Helvetica", "B", 14)
	setColor(pdf, pdfInk, false)
	pdf.SetXY(x+4, y+7)
	pdf.CellFormat(textW, 7, fitText(pdf, value, textW-2), "", 0, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 7.5)
	setColor(pdf, pdfMuted, false)
	pdf.SetXY(x+4, y+15)
	pdf.CellFormat(textW, 4, fitText(pdf, caption, textW-2), "", 0, "L", false, 0, "")
	pdf.SetXY(x+4, y+19.5)
	pdf.CellFormat(textW, 4, fitText(pdf, tileChange(t, prev), textW-2), "", 0, "L", false, 0, "")
}

// tileFigures is a tile's big number and the line under it. Traffic shows
// the billable 95th and the volume moved; a percentage its average and 95th;
// anything else its average. Kind values are the contract's: "traffic",
// "percent", "other".
func tileFigures(t MetricsTile) (value, caption string) {
	if t.NoData {
		return "No data", ""
	}
	value = formatValue(t.First, t.Unit)
	switch t.Kind {
	case "traffic":
		caption = "billable 95th"
		if t.Second != nil {
			caption += ", " + formatBytes(*t.Second) + " moved"
		}
	case "percent":
		caption = "average"
		if t.Second != nil {
			caption += ", 95th " + formatValue(*t.Second, t.Unit)
		}
	default:
		caption = "average"
	}
	return value, caption
}

// tileChange is "+18% vs August 2026", "new", or nothing.
func tileChange(t MetricsTile, prev string) string {
	switch {
	case t.NoData:
		return ""
	case t.New:
		return "new"
	case t.Change != nil:
		return formatChange(t.Change, false) + " vs " + prev
	}
	return ""
}

func drawMetricsRunningHot(pdf *fpdf.Fpdf, hot []HotPort) {
	if len(hot) == 0 {
		return
	}
	drawSectionHeading(pdf, "Running hot")
	pdf.SetFont("Helvetica", "", 8.5)
	setColor(pdf, pdfMuted, false)
	pdf.CellFormat(pdfContentW, 5, "Ports whose 95th busy reached 80% or more in either direction.", "", 1, "L", false, 0, "")
	const figuresW = 45.0
	for i, h := range hot {
		if i == metricsHotShown {
			setColor(pdf, pdfMuted, false)
			pdf.CellFormat(pdfContentW, 5, fmt.Sprintf("and %d more: the Busy table lists every port.", len(hot)-i), "", 1, "L", false, 0, "")
			break
		}
		pdf.SetFont("Helvetica", "", 9)
		setColor(pdf, pdfInk, false)
		pdf.CellFormat(pdfContentW-figuresW, 5, fitText(pdf, h.Name, pdfContentW-figuresW-2), "", 0, "L", false, 0, "")
		setColor(pdf, pdfDanger, false)
		pdf.CellFormat(figuresW, 5, fmt.Sprintf("in %s, out %s", formatPercent(h.P95In), formatPercent(h.P95Out)), "", 1, "R", false, 0, "")
	}
}

func drawMetricsBusiest(pdf *fpdf.Fpdf, n *MetricsReportData) {
	if len(n.Busiest) == 0 {
		return
	}
	title := fmt.Sprintf("Busiest %d", len(n.Busiest))
	if n.RankLabel != "" {
		title += " by " + n.RankLabel
	}
	drawSectionHeading(pdf, title)
	pdf.SetFont("Helvetica", "", 9)
	setColor(pdf, pdfInk, false)
	for i, name := range n.Busiest {
		pdf.CellFormat(pdfContentW, 5, fitText(pdf, fmt.Sprintf("%d. %s", i+1, name), pdfContentW-2), "", 1, "L", false, 0, "")
	}
}

// metricsNotes says what the report left out or could not measure fully.
// Unavailable subjects are counted, never named.
func metricsNotes(n *MetricsReportData) []string {
	var notes []string
	if n.LowCoverage > 0 {
		notes = append(notes, fmt.Sprintf("%s with data for less than 90%% of the period, marked in the tables.",
			countNoun(n.LowCoverage, "row", "rows")))
	}
	if n.CappedOut > 0 {
		notes = append(notes, fmt.Sprintf("Left out %s beyond the %d-port limit; the report includes the %d busiest.",
			countNoun(n.CappedOut, "port", "ports"), models.MaxReportSubjects, models.MaxReportSubjects))
	}
	if n.Unavailable > 0 {
		verb := "are"
		if n.Unavailable == 1 {
			verb = "is"
		}
		notes = append(notes, fmt.Sprintf("Left out %s that %s no longer available to you.",
			countNoun(n.Unavailable, "chosen port, device or site", "chosen ports, devices or sites"), verb))
	}
	if len(n.Skipped) > 0 {
		notes = append(notes, "Left out metrics that no longer exist: "+strings.Join(n.Skipped, ", ")+".")
	}
	return notes
}

func drawMetricsNotes(pdf *fpdf.Fpdf, n *MetricsReportData) {
	notes := metricsNotes(n)
	if len(notes) == 0 {
		return
	}
	drawSectionHeading(pdf, "Notes")
	pdf.SetFont("Helvetica", "", 9)
	setColor(pdf, pdfWarning, false)
	for _, note := range notes {
		pdf.MultiCell(pdfContentW, 4.5, pdfText("- "+note), "", "L", false)
	}
}

// drawPDFNotice draws a one-line message in a panel with a coloured edge,
// on a new page when it would not fit.
func drawPDFNotice(pdf *fpdf.Fpdf, message string, edge [3]int) {
	const h = 18.0
	_, pageH := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	if pdf.GetY()+h > pageH-bottom {
		pdf.AddPage()
	}
	y := pdf.GetY()
	setColor(pdf, pdfPanel, true)
	pdf.Rect(pdfMarginLeft, y, pdfContentW, h, "F")
	setColor(pdf, edge, true)
	pdf.Rect(pdfMarginLeft, y, 1.4, h, "F")
	pdf.SetXY(pdfMarginLeft+6, y)
	pdf.SetFont("Helvetica", "", 10)
	setColor(pdf, pdfInk, false)
	pdf.CellFormat(pdfContentW-12, h, fitText(pdf, message, pdfContentW-14), "", 0, "L", false, 0, "")
	pdf.SetY(y + h + 4)
}

// fitText converts s for the page and, when it is wider than w mm in the
// current font, cuts it to the longest prefix that fits with an ellipsis.
// The result is already converted: draw it as it is, never through pdfText
// again (pdfText reads its input as UTF-8).
func fitText(pdf *fpdf.Fpdf, s string, w float64) string {
	if out := pdfText(s); pdf.GetStringWidth(out) <= w {
		return out
	}
	r := []rune(s)
	cut := func(k int) string { return pdfText(strings.TrimRight(string(r[:k]), " ") + "…") }
	lo, hi := 0, len(r)-1 // the answer keeps lo runes; hi is the most it could keep
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if pdf.GetStringWidth(cut(mid)) <= w {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return cut(lo)
}

// countNoun is "1 port" or "214 ports".
func countNoun(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
```

Most text goes through `fitText`, so a long name or label is cut to its cell rather than drawn over its neighbour. The few exceptions are the notes (drawn with `MultiCell`, which wraps) and fixed English strings that are known to fit.

- [ ] **Step 4: Write the charts**

Create `backend/internal/services/pdf_metrics_charts.go`:

```go
package services

import (
	"math"
	"time"

	"github.com/go-pdf/fpdf"
)

// The Metrics report's charts: one per metric for the whole scope, two to a
// page, with the period's 95th as a reference line. The busiest rows' small
// charts (pdf_metrics_tables.go) use the same drawing.

const (
	metricsChartH      = 95.0
	metricsLegendRoomW = 50.0 // kept free right of a chart title for the legend
)

// metricsLineColors colour a chart's lines: in, then out.
var metricsLineColors = [2][3]int{pdfAccent, pdfSuccess}

// drawMetricsCharts draws one chart per metric for the whole scope, two to
// a page.
func drawMetricsCharts(pdf *fpdf.Fpdf, data *ReportData) {
	charts := data.Network.Charts
	if len(charts) == 0 {
		return
	}
	pdf.AddPage()
	drawSectionHeading(pdf, "Charts")
	for i, c := range charts {
		if i > 0 && i%2 == 0 {
			pdf.AddPage()
		}
		drawMetricsChart(pdf, data, c, metricsChartH)
	}
}

// drawMetricsChart draws a title line with its legend, then the chart, on a
// new page when the two would not fit together.
func drawMetricsChart(pdf *fpdf.Fpdf, data *ReportData, c MetricsChart, height float64) {
	_, pageH := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	if pdf.GetY()+height+18 > pageH-bottom {
		pdf.AddPage()
	}
	y := pdf.GetY()
	pdf.SetFont("Helvetica", "B", 10)
	setColor(pdf, pdfInk, false)
	pdf.SetX(pdfMarginLeft)
	pdf.CellFormat(pdfContentW-metricsLegendRoomW, 6, fitText(pdf, c.Title, pdfContentW-metricsLegendRoomW-2), "", 0, "L", false, 0, "")
	drawChartLegend(pdf, c.Lines, y)
	pdf.SetY(y + 7)

	lc, ok := metricsLineChart(data, c, height)
	if !ok {
		drawPDFNotice(pdf, "Not enough data in this period to chart.", pdfMuted)
		return
	}
	drawLineChart(pdf, lc)
	pdf.Ln(2)
}

// drawChartLegend names a pair's lines at the right end of the title line.
func drawChartLegend(pdf *fpdf.Fpdf, lines []ChartLine, y float64) {
	if len(lines) < 2 {
		return
	}
	pdf.SetFont("Helvetica", "", 8)
	x := pdfMarginLeft + pdfContentW
	for i := len(lines) - 1; i >= 0; i-- {
		label := fitText(pdf, lines[i].Label, metricsLegendRoomW/2-8)
		w := pdf.GetStringWidth(label) + 2
		x -= w
		setColor(pdf, pdfMuted, false)
		pdf.SetXY(x, y+1)
		pdf.CellFormat(w, 4, label, "", 0, "L", false, 0, "")
		x -= 5
		setColor(pdf, metricsLineColors[i%2], true)
		pdf.Rect(x+0.5, y+2.6, 4, 1, "F")
		x -= 3
	}
}

// metricsLineChart places a chart's points across the report period and
// picks a y range from zero (or the lowest value, when negative) to a round
// number above the highest value and the reference line. ok is false when no
// line has two points to join.
func metricsLineChart(data *ReportData, c MetricsChart, height float64) (lineChart, bool) {
	start, end := data.TimeRangeStart, data.TimeRangeEnd
	span := end.Sub(start).Seconds()
	if span <= 0 {
		return lineChart{}, false
	}
	lc := lineChart{
		Height: height, AxisLabelW: 18, TopPad: 3, Ticks: 4, LabelsInside: true,
		TickLabel: func(v float64) string { return formatValue(v, c.Unit) },
		XLabels:   timeAxisLabels(start, end, data.ReportLocation()),
	}
	lo, hi, longest := 0.0, 0.0, 0
	for i, l := range c.Lines {
		line := chartLine{Color: metricsLineColors[i%2], Width: 0.5}
		for _, p := range l.Points {
			if !finite(p.V) {
				continue
			}
			x := math.Min(math.Max(p.T.Sub(start).Seconds()/span, 0), 1)
			line.X = append(line.X, x)
			line.Y = append(line.Y, p.V)
			lo, hi = math.Min(lo, p.V), math.Max(hi, p.V)
		}
		longest = max(longest, len(line.X))
		lc.Lines = append(lc.Lines, line)
	}
	if longest < 2 {
		return lineChart{}, false
	}
	if c.Reference != nil && finite(*c.Reference) {
		hi = math.Max(hi, *c.Reference)
		lc.Ref = &chartRef{Value: *c.Reference, Label: referenceLabel(c), Color: pdfWarning}
	}
	lc.Hi = niceCeil(hi)
	if lo < 0 {
		lc.Lo = -niceCeil(-lo)
	}
	return lc, true
}

// referenceLabel is "Billable 95th 640 Mbps" for a traffic pair, otherwise
// "95th 92.5%".
func referenceLabel(c MetricsChart) string {
	label := "95th "
	if c.Unit == "bps" && len(c.Lines) == 2 {
		label = "Billable 95th "
	}
	return label + formatValue(*c.Reference, c.Unit)
}

// timeAxisLabels marks the start, middle and end of the period, with times
// when it is two days or shorter.
func timeAxisLabels(start, end time.Time, loc *time.Location) []chartXLabel {
	layout := "Jan 2"
	if end.Sub(start) <= 48*time.Hour {
		layout = "Jan 2 15:04"
	}
	mid := start.Add(end.Sub(start) / 2)
	return []chartXLabel{
		{At: 0, Text: start.In(loc).Format(layout), Align: "L"},
		{At: 1, Text: end.In(loc).Format(layout), Align: "R"},
		{At: 0.5, Text: mid.In(loc).Format(layout), Align: "C"},
	}
}
```

The y axis starts at zero, or lower when a value is negative, and ends at `niceCeil` of the larger of the highest value and the reference. Points sit by time across the report period, so a gap in the data shows where it happened.

- [ ] **Step 5: Draw Metrics reports through the new layout**

In `backend/internal/services/pdf_renderer.go`, replace the two doc-comment lines

```go
// reportType selects the fixed layout: models.ReportTypeUptime or
// models.ReportTypeIncident.
```

with

```go
// reportType selects the fixed layout: models.ReportTypeUptime,
// models.ReportTypeIncident or models.ReportTypeMetrics.
```

and in `RenderReportToPDF`'s switch, after the `case models.ReportTypeIncident:` branch (`drawPDFIncidentReport(pdf, data)`), add:

```go
	case models.ReportTypeMetrics:
		drawPDFMetricsReport(pdf, data)
```

- [ ] **Step 6: Run the tests, then the whole renderer suite**

Run: `go test ./internal/services/ -run 'TestMetricsPDF|TestWindowLabel|TestFitText' -v`
Expected: PASS (4 tests).

Run: `go test ./internal/services/ -run 'TestRender|TestPDF|TestUptimeGraph|TestDrawLineChart|TestFormat' -v && go vet ./internal/services/`
Expected: PASS and no vet output. The uptime golden is untouched.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/services/pdf_metrics.go backend/internal/services/pdf_metrics_charts.go \
  backend/internal/services/pdf_metrics_test.go backend/internal/services/pdf_renderer.go
git commit -m "feat(reports): metrics PDF headline page and charts

Scope and comparison period in words, a tile per metric with its change,
running hot, the busiest rows and notes on what was left out; one chart per
metric with the period's 95th as a reference line, two to a page.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: The Metrics PDF: tables, busiest charts, empty and no-data pages

The Metrics layout is finished in this task:

- **Tables:** one per `MetricsTable`, busiest first, every row, with the header repeated on every page. Long names are cut to their column, and rows under 90% coverage carry a short note.
- **Small charts** for the busiest rows.
- **Empty report:** a one-page notice.
- **No data:** a "No data for this period" headline.

Two bugs that already exist block this and are fixed here:

- **The text translator is shared and races.** `pdfText` calls a translator that fpdf returns, and that translator writes into one buffer it keeps between calls. Reports render at the same time (the job queue's workers and the scheduler), so two renders mix each other's text. A Metrics report calls `pdfText` thousands of times, which makes this much more likely. It needs a lock.
- **Every report ends on a blank page.** `drawPDFFooter` draws below the page-break line with auto page break on, so every report today ends on a page holding nothing but "Generated by Sentinel". The spec's one-page empty report cannot exist until that is fixed.

This task pins Review Focus 4: a report of 500 rows × 10 tables, with long and non-Latin names, renders without an fpdf panic, cuts the names to fit and repeats the header.

**Files:**
- Create: `backend/internal/services/pdf_metrics_tables.go`
- Create: `backend/internal/services/pdf_metrics_tables_test.go`
- Modify: `backend/internal/services/pdf_encoding.go` (the translator `var` block and `pdfText`)
- Modify: `backend/internal/services/pdf_encoding_test.go` (imports, one new test)
- Modify: `backend/internal/services/pdf_metrics.go` (`drawPDFMetricsReport`'s body, and `drawMetricsHeadline`)
- Modify: `backend/internal/services/pdf_renderer.go` (`drawPDFFooter`)

**Interfaces:**
- Consumes:
  - Task 10: `formatValue`, `formatTotal`, `formatChange`, `formatDurationHM`.
  - Task 11: `drawMetricsTitle`, `drawMetricsHeadline`, `drawMetricsNotes`, `drawMetricsCharts`, `drawMetricsChart(pdf, data, c, height)`, `drawPDFNotice(pdf, message, edge)`, `fitText(pdf, s, w)`.
  - Task 11 test helpers: `sampleMetricsData()`, `renderMetrics(t, data) (text, path)` and `hasLine(text, line)` in `pdf_metrics_test.go`. The fixture's traffic table holds:
    - core-sw1 · Gi1/0/1 (uplink to annex): 95th in 640 Mbps, out 300 Mbps, peak 900 Mbps, totals 1.2 TB and 900 GB, +18%.
    - edge-sw2 · Gi0/48: 72% coverage, new.
    - edge-sw3 · Gi0/1: no data.

    Its Busy pair is 92.5% / 41% 95th with a 99% peak and +5%. Its bool table has ups-1 at a share of 0.0030787 (0.3%) and 7,980 s true.
  - Existing: `pdfDrawnText`, `f64`, `sampleReportData`, `drawSectionHeading`.
  - Part A: the `MetricsReportData` types, `models.ReportTypeUptime` and `models.ReportTypeMetrics`.
- Produces:
  - `const metricsEmptyMessage = "None of the ports, devices or sites in this report are available to you."`. Task 13's email summary reuses it.
  - `type metricsColumn struct { header string; value func(r MetricsRow) string }` and `func metricsColumns(t MetricsTable) []metricsColumn`
  - `func drawMetricsTables(pdf *fpdf.Fpdf, data *ReportData)`, `func drawMetricsTable(pdf *fpdf.Fpdf, t MetricsTable)`, `func drawMetricsRowCharts(pdf *fpdf.Fpdf, data *ReportData)`
  - `pdfText` is safe to call from several goroutines.
  - A report no longer ends on a blank page.
  - Test helpers `pdfPageCount(t, path) int` and `bigMetricsData() *ReportData`.

- [ ] **Step 1: Write the failing test for the translator race**

In `backend/internal/services/pdf_encoding_test.go`, add `"fmt"` and `"sync"` to the imports, so the block reads:

```go
import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)
```

and append:

```go
// Reports render concurrently (job workers and the scheduler), and fpdf's
// translator reuses one buffer between calls: unguarded, two goroutines mix
// each other's text.
func TestPDFTextIsSafeConcurrently(t *testing.T) {
	inputs := make([]string, 8)
	want := make([]string, len(inputs))
	for i := range inputs {
		inputs[i] = strings.Repeat(fmt.Sprintf("Café Müller %d · ", i), 20)
		want[i] = pdfText(inputs[i])
	}
	var wg sync.WaitGroup
	errs := make(chan string, len(inputs))
	for i := range inputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 2000; n++ {
				if got := pdfText(inputs[i]); got != want[i] {
					errs <- fmt.Sprintf("goroutine %d got text mixed with another's: %.40q", i, got)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/services/ -run TestPDFTextIsSafeConcurrently -count=3 -v`
Expected: FAIL, with errors such as `goroutine 5 got text mixed with another's: "Caf\xe9 M\xfcller 1 \xb7 Caf\xe9 M\xfcller 1 \xb7 Caf\xe9 M"`. Run with `-count=3` if one run happens to pass. It failed on every run while this plan was written.

- [ ] **Step 3: Serialise the translator**

In `backend/internal/services/pdf_encoding.go`, replace

```go
var (
	pdfTranslatorOnce sync.Once
	pdfTranslator     func(string) string
)
```

with

```go
//
// The translator fpdf returns writes into one buffer it keeps between calls,
// so it is not safe to call from two goroutines at once, and reports render
// concurrently (the job queue's workers and the scheduler). pdfTranslatorMu
// serialises the calls.
var (
	pdfTranslatorOnce sync.Once
	pdfTranslator     func(string) string
	pdfTranslatorMu   sync.Mutex
)
```

The new comment lines continue the comment block that already sits above the `var`. Then in `pdfText` replace

```go
	if tr := pdfTranslate(); tr != nil {
		return tr(s)
	}
```

with

```go
	if tr := pdfTranslate(); tr != nil {
		pdfTranslatorMu.Lock()
		defer pdfTranslatorMu.Unlock()
		return tr(s)
	}
```

The translator returns `buf.String()`, a copy, so the result stays safe to use once the lock is released.

- [ ] **Step 4: Run it to see it pass, and commit the fix**

Run: `go test ./internal/services/ -run 'TestPDFText|TestPDF_' -count=3 -v`
Expected: PASS.

```bash
git add backend/internal/services/pdf_encoding.go backend/internal/services/pdf_encoding_test.go
git commit -m "fix(reports): serialise the PDF text translator across concurrent renders

fpdf's translator reuses one buffer between calls; two reports rendering at
once mixed each other's text.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 5: Write the failing tests for the tables and the empty and no-data pages**

Create `backend/internal/services/pdf_metrics_tables_test.go`:

```go
package services

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// pdfPageCount counts the page objects in a rendered file.
func pdfPageCount(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(regexp.MustCompile(`/Type /Page\b`).FindAll(raw, -1))
}

func TestMetricsPDFTables(t *testing.T) {
	text, _ := renderMetrics(t, sampleMetricsData())
	// Traffic pair: the billing columns, in order, and the core port's figures.
	for _, want := range []string{"95th in", "95th out", "Billable 95th", "Peak", "Total in", "Total out", "Change",
		"640 Mbps", "300 Mbps", "900 Mbps", "1.2 TB", "900 GB", "+18%"} {
		if !hasLine(text, want) {
			t.Errorf("traffic table is missing %q", want)
		}
	}
	// The flagged row carries its coverage; the new row says so; the empty row says no data.
	for _, want := range []string{"72% data", "new", "No data"} {
		if !hasLine(text, want) {
			t.Errorf("traffic rows are missing %q", want)
		}
	}
	// Busy pair: averages and 95ths both ways, no totals.
	for _, want := range []string{"Avg in", "Avg out", "92.5%", "41%", "99%", "+5%"} {
		if !hasLine(text, want) {
			t.Errorf("busy table is missing %q", want)
		}
	}
	// A bool metric: share of time and how long.
	for _, want := range []string{"Share of time", "Time true", "0.3%", "2 h 13 m"} {
		if !hasLine(text, want) {
			t.Errorf("on-battery table is missing %q", want)
		}
	}
	// The busiest rows' small charts.
	if !hasLine(text, "Busiest by Traffic") {
		t.Error("missing the busiest rows' charts")
	}
}

func TestMetricsPDFRepeatsTheHeaderOnEveryPage(t *testing.T) {
	data := sampleMetricsData()
	traffic := &data.Network.Tables[0]
	traffic.Rows = nil
	for i := 0; i < 120; i++ {
		traffic.Rows = append(traffic.Rows, MetricsRow{
			Name: fmt.Sprintf("sw%d · Gi1/0/%d", i, i),
			In:   &RowStats{P95: 1e6, Total: f64(1e9)}, Out: &RowStats{P95: 1e6, Total: f64(1e9)}, Billable: f64(1e6), Coverage: 100,
		})
	}
	text, _ := renderMetrics(t, data)
	if n := strings.Count("\n"+text+"\n", "\nBillable 95th\n"); n < 3 {
		t.Errorf("the traffic header was drawn %d times for 120 rows, want it on each of at least 3 pages", n)
	}
}

func TestMetricsPDFNoData(t *testing.T) {
	data := sampleMetricsData()
	data.Network.NoData = true
	text, _ := renderMetrics(t, data)
	if !hasLine(text, "No data for this period") {
		t.Error("missing the no-data headline")
	}
	if !strings.Contains(text, "September 2026, compared with August 2026") {
		t.Error("the no-data page should still say which period it covers")
	}
	for _, absent := range []string{"Running hot", "Charts", "Billable 95th"} {
		if strings.Contains(text, absent) {
			t.Errorf("a report without data should not draw %q", absent)
		}
	}
}

func TestMetricsPDFEmptyIsOnePage(t *testing.T) {
	data := sampleMetricsData()
	data.Network.Empty = true
	text, path := renderMetrics(t, data)
	if !hasLine(text, metricsEmptyMessage) {
		t.Errorf("missing %q", metricsEmptyMessage)
	}
	if strings.Contains(text, "Billable 95th") || strings.Contains(text, "Running hot") {
		t.Error("an empty report should draw no tables or headline figures")
	}
	if n := pdfPageCount(t, path); n != 1 {
		t.Errorf("empty report has %d pages, want 1", n)
	}
}

// bigMetricsData is the largest report a run can produce: 500 rows in each
// of 10 tables, with long names and names in scripts the PDF fonts cannot
// draw, every table kind and 200-point charts.
func bigMetricsData() *ReportData {
	data := sampleMetricsData()
	n := data.Network
	start := data.TimeRangeStart
	long := strings.Repeat("very-long-device-name-", 8)
	name := func(i int) string {
		switch i % 4 {
		case 0:
			return fmt.Sprintf("%s%d · Gi1/0/%d (an alias that goes on and on and on)", long, i, i)
		case 1:
			return fmt.Sprintf("交换机-%d · 端口 %d", i, i)
		case 2:
			return fmt.Sprintf("Коммутатор-%d · порт %d", i, i)
		}
		return fmt.Sprintf("Café-Müller-%d · Ü%d 🚀 שלום", i, i)
	}
	kinds := []MetricsTable{
		{Title: "Traffic", Unit: "bps", Paired: true, Billable: true, HasTotal: true},
		{Title: "Busy", Unit: "%", Paired: true},
		{Title: "Errors", Unit: "per_min", Paired: true, HasTotal: true},
		{Title: "Temperature", Unit: "°C"},
		{Title: "On battery", Unit: "bool"},
		{Title: "Fan speed", Unit: "rpm"},
	}
	n.Tables, n.Charts, n.RowCharts, n.Tiles = nil, nil, nil, nil
	for ti := 0; ti < 10; ti++ {
		table := kinds[ti%len(kinds)]
		for i := 0; i < 500; i++ {
			v := float64(500-i) * 1.7e6
			s := &RowStats{Avg: v / 3, Min: 0, Peak: v * 1.5, P95: v, Total: f64(v * 2.6e6), TrueSeconds: f64(float64(i * 60))}
			row := MetricsRow{Name: name(i), In: s, Change: f64(float64(i%40) - 20), Coverage: 100}
			if table.Paired {
				row.Out = s
			}
			if table.Billable {
				row.Billable = f64(v)
			}
			switch {
			case i%11 == 0:
				row = MetricsRow{Name: name(i), NoData: true}
			case i%7 == 0:
				row.LowCoverage, row.Coverage = true, 61
			case i%13 == 0:
				row.Change, row.New = nil, true
			}
			table.Rows = append(table.Rows, row)
		}
		n.Tables = append(n.Tables, table)
		points := make([]ChartPoint, 200)
		for p := range points {
			points[p] = ChartPoint{T: start.Add(time.Duration(p) * 3 * time.Hour), V: float64(p%37) * 1e6}
		}
		chart := MetricsChart{Title: table.Title, Unit: table.Unit, Reference: f64(30e6),
			Lines: []ChartLine{{Label: "In", Points: points}, {Label: "Out", Points: points}}}
		n.Charts = append(n.Charts, chart)
		chart.Title = name(ti)
		n.RowCharts = append(n.RowCharts, chart)
		n.Tiles = append(n.Tiles, MetricsTile{Label: table.Title, Unit: table.Unit, Kind: "other", First: 42})
	}
	n.Busiest = []string{name(0), name(1), name(2), name(3), name(4)}
	n.RunningHot = nil
	for i := 0; i < 30; i++ {
		n.RunningHot = append(n.RunningHot, HotPort{Name: name(i), P95In: 95, P95Out: 81})
	}
	n.Rows, n.Ports = 500, 500
	return data
}

// Review Focus 4: the largest report renders without an fpdf panic, its long
// names cut to fit, its non-Latin names converted the way every report does
// (pdfText), and every table's header repeated on each page.
func TestMetricsPDFLargestReport(t *testing.T) {
	text, path := renderMetrics(t, bigMetricsData())
	info, err := os.Stat(path)
	if err != nil || info.Size() < 50_000 {
		t.Fatalf("PDF %v, %v: want a substantial file", info, err)
	}
	if pages := pdfPageCount(t, path); pages < 100 {
		t.Errorf("5,000 rows fit on %d pages; the tables did not render", pages)
	}
	if strings.Contains(text, strings.Repeat("very-long-device-name-", 8)) {
		t.Error("a long name was drawn whole instead of cut to its column")
	}
	if !strings.Contains(text, "very-long-device-name-") || !strings.Contains(text, "\x85") {
		t.Error("long names should be drawn cut with an ellipsis")
	}
	if !strings.Contains(text, "Caf\xe9-M\xfcller-3") {
		t.Error("accented names should survive as cp1252")
	}
	if strings.Contains(text, "交") || strings.Contains(text, "Ком") {
		t.Error("characters the fonts cannot draw reached the page as raw UTF-8")
	}
	if !hasLine(text, "and 20 more: the Busy table lists every port.") {
		t.Error("running hot should list 10 ports and count the rest")
	}
	if n := strings.Count("\n"+text+"\n", "\nBillable 95th\n"); n < 10 {
		t.Errorf("the traffic header was drawn %d times, want it on every page of the two traffic tables", n)
	}
}

// The footer is drawn below the page-break line; it must land on the last
// page rather than start an empty one.
func TestPDFFooterDoesNotAddAPage(t *testing.T) {
	r, _ := NewPDFRendererService(t.TempDir())
	name, err := r.RenderReportToPDF(sampleReportData(), models.ReportTypeUptime, "footer")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := r.GetPDFPath(name)
	if n := pdfPageCount(t, path); n != 1 {
		t.Errorf("the one-page uptime sample has %d pages", n)
	}
	if text := pdfDrawnText(t, path); !strings.Contains(text, "Generated by Sentinel") {
		t.Error("the footer is gone")
	}
}
```

Notes on the expectations:
- `pdfPageCount` counts `/Type /Page` objects. Those dictionaries are not compressed, and `\b` keeps `/Type /Pages` out of the count.
- The fixture's 120 rows at 5.5 mm each need at least three table pages, about 45 rows to a page.
- In the largest report, the CJK and Cyrillic names reach the page as fpdf's `.` placeholders, which is how every report already handles characters cp1252 cannot draw (`pdfText`). The check is that no raw UTF-8 bytes ever reach the page.
- Two of the ten tables are traffic pairs of 500 rows each, so "Billable 95th" heads at least 10 table pages. Every chart's reference label reads "95th …", never exactly "Billable 95th".

- [ ] **Step 6: Run them to see them fail**

Run: `go test ./internal/services/ -run 'TestMetricsPDF|TestPDFFooter' -v`
Expected: FAIL to build with `undefined: metricsEmptyMessage`.

- [ ] **Step 7: Write the tables and the busiest rows' charts**

Create `backend/internal/services/pdf_metrics_tables.go`:

```go
package services

import (
	"fmt"
	"math"

	"github.com/go-pdf/fpdf"
)

// The Metrics report's tables (one per metric or in/out pair, every row,
// busiest first) and the small charts of its busiest rows.

const (
	metricsRowH      = 5.5
	metricsHeaderH   = 7.0
	metricsMaxNumW   = 22.0 // widest a figure column gets
	metricsMinNameW  = 46.0 // narrowest the name column gets
	metricsRowChartH = 32.0
	// metricsEmptyMessage is the whole report when every subject is gone.
	metricsEmptyMessage = "None of the ports, devices or sites in this report are available to you."
)

// metricsColumn is one figure column of a metrics table.
type metricsColumn struct {
	header string
	value  func(r MetricsRow) string
}

// metricsColumns picks a table's figure columns. A traffic pair shows the
// billing view (95th each way, billable 95th, peak, volume each way); other
// pairs average and 95th each way, peak and, for rates, totals; a single
// metric its average, 95th, peak and total, except a bool, which shows the
// share and the length of time it was true.
func metricsColumns(t MetricsTable) []metricsColumn {
	u := t.Unit
	stat := func(pick func(*MetricsRow) *RowStats, f func(*RowStats) float64) func(MetricsRow) string {
		return func(r MetricsRow) string {
			s := pick(&r)
			if s == nil {
				return "-"
			}
			return formatValue(f(s), u)
		}
	}
	total := func(pick func(*MetricsRow) *RowStats) func(MetricsRow) string {
		return func(r MetricsRow) string {
			s := pick(&r)
			if s == nil || s.Total == nil {
				return "-"
			}
			return formatTotal(*s.Total, u)
		}
	}
	in := func(r *MetricsRow) *RowStats { return r.In }
	out := func(r *MetricsRow) *RowStats { return r.Out }
	avg := func(s *RowStats) float64 { return s.Avg }
	p95 := func(s *RowStats) float64 { return s.P95 }
	peakOf := func(s *RowStats) float64 { return s.Peak }
	peak := func(r MetricsRow) string {
		best, ok := math.Inf(-1), false
		for _, s := range []*RowStats{r.In, r.Out} {
			if s != nil {
				best, ok = math.Max(best, s.Peak), true
			}
		}
		if !ok {
			return "-"
		}
		return formatValue(best, u)
	}
	change := func(r MetricsRow) string {
		if c := formatChange(r.Change, r.New); c != "" {
			return c
		}
		return "-"
	}

	var cols []metricsColumn
	switch {
	case t.Paired && t.Billable:
		cols = []metricsColumn{
			{"95th in", stat(in, p95)}, {"95th out", stat(out, p95)},
			{"Billable 95th", func(r MetricsRow) string {
				if r.Billable == nil {
					return "-"
				}
				return formatValue(*r.Billable, u)
			}},
			{"Peak", peak}, {"Total in", total(in)}, {"Total out", total(out)},
		}
	case t.Paired:
		cols = []metricsColumn{
			{"Avg in", stat(in, avg)}, {"Avg out", stat(out, avg)},
			{"95th in", stat(in, p95)}, {"95th out", stat(out, p95)}, {"Peak", peak},
		}
		if t.HasTotal {
			cols = append(cols, metricsColumn{"Total in", total(in)}, metricsColumn{"Total out", total(out)})
		}
	case u == "bool":
		cols = []metricsColumn{
			{"Share of time", stat(in, avg)},
			{"Time true", func(r MetricsRow) string {
				if r.In == nil || r.In.TrueSeconds == nil {
					return "-"
				}
				return formatDurationHM(*r.In.TrueSeconds)
			}},
		}
	default:
		cols = []metricsColumn{{"Average", stat(in, avg)}, {"95th", stat(in, p95)}, {"Peak", stat(in, peakOf)}}
		if t.HasTotal {
			cols = append(cols, metricsColumn{"Total", total(in)})
		}
	}
	return append(cols, metricsColumn{"Change", change})
}

// drawMetricsTables draws every table, starting on a new page.
func drawMetricsTables(pdf *fpdf.Fpdf, data *ReportData) {
	tables := data.Network.Tables
	if len(tables) == 0 {
		return
	}
	pdf.AddPage()
	for _, t := range tables {
		drawMetricsTable(pdf, t)
	}
}

// drawMetricsTable draws one table, repeating its header on every page it
// runs onto.
func drawMetricsTable(pdf *fpdf.Fpdf, t MetricsTable) {
	cols := metricsColumns(t)
	numW := math.Min(metricsMaxNumW, (pdfContentW-metricsMinNameW)/float64(len(cols)))
	nameW := pdfContentW - numW*float64(len(cols))
	_, pageH := pdf.GetPageSize()
	_, _, _, bottom := pdf.GetMargins()
	limit := pageH - bottom

	// The heading (12 mm), the header and a first row stay together.
	if pdf.GetY()+12+metricsHeaderH+metricsRowH > limit {
		pdf.AddPage()
	}
	drawSectionHeading(pdf, t.Title)
	header := func() {
		pdf.SetFont("Helvetica", "B", 7)
		setColor(pdf, pdfPanel, true)
		setColor(pdf, pdfInk, false)
		pdf.SetX(pdfMarginLeft)
		pdf.CellFormat(nameW, metricsHeaderH, "Name", "", 0, "L", true, 0, "")
		for _, c := range cols {
			pdf.CellFormat(numW, metricsHeaderH, fitText(pdf, c.header, numW-2), "", 0, "R", true, 0, "")
		}
		pdf.Ln(-1)
	}
	header()
	setDrawColor(pdf, pdfRule)
	pdf.SetLineWidth(0.2)
	for _, r := range t.Rows {
		if pdf.GetY()+metricsRowH > limit {
			pdf.AddPage()
			header()
		}
		drawMetricsRow(pdf, r, cols, nameW, numW)
	}
	pdf.Ln(2)
}

// drawMetricsRow draws one row. A row under 90% coverage carries a short
// note after its name; a row without data says so across its figures.
func drawMetricsRow(pdf *fpdf.Fpdf, r MetricsRow, cols []metricsColumn, nameW, numW float64) {
	pdf.SetFont("Helvetica", "", 7)
	setColor(pdf, pdfInk, false)
	pdf.SetX(pdfMarginLeft)
	if r.LowCoverage {
		note := fmt.Sprintf("%.0f%% data", r.Coverage)
		noteW := pdf.GetStringWidth(note) + 3
		pdf.CellFormat(nameW-noteW, metricsRowH, fitText(pdf, r.Name, nameW-noteW-2), "B", 0, "L", false, 0, "")
		setColor(pdf, pdfWarning, false)
		pdf.CellFormat(noteW, metricsRowH, note, "B", 0, "L", false, 0, "")
		setColor(pdf, pdfInk, false)
	} else {
		pdf.CellFormat(nameW, metricsRowH, fitText(pdf, r.Name, nameW-2), "B", 0, "L", false, 0, "")
	}
	if r.NoData {
		setColor(pdf, pdfMuted, false)
		pdf.CellFormat(numW*float64(len(cols)), metricsRowH, "No data", "B", 1, "R", false, 0, "")
		return
	}
	for i, c := range cols {
		ln := 0
		if i == len(cols)-1 {
			ln = 1
		}
		pdf.CellFormat(numW, metricsRowH, fitText(pdf, c.value(r), numW-2), "B", ln, "R", false, 0, "")
	}
}

// drawMetricsRowCharts draws the busiest rows' small charts, starting on a
// new page.
func drawMetricsRowCharts(pdf *fpdf.Fpdf, data *ReportData) {
	n := data.Network
	if len(n.RowCharts) == 0 {
		return
	}
	pdf.AddPage()
	title := "Busiest rows"
	if n.RankLabel != "" {
		title = "Busiest by " + n.RankLabel
	}
	drawSectionHeading(pdf, title)
	for _, c := range n.RowCharts {
		drawMetricsChart(pdf, data, c, metricsRowChartH)
	}
}
```

Each row is checked against the page-break line before it is drawn, and a new page redraws the header first. fpdf's own auto page break never fires inside a table, so a header can never be left behind on the previous page.

- [ ] **Step 8: Draw the whole layout, the empty page and the no-data headline**

In `backend/internal/services/pdf_metrics.go`, replace the whole `drawPDFMetricsReport` function with:

```go
// drawPDFMetricsReport assembles the fixed Metrics report layout.
func drawPDFMetricsReport(pdf *fpdf.Fpdf, data *ReportData) {
	n := data.Network
	switch {
	case n == nil:
		drawPDFNotice(pdf, "No network data was gathered for this report.", pdfWarning)
		return
	case n.Empty:
		// Every subject is gone or hidden: one page that says so.
		drawMetricsTitle(pdf, data)
		drawPDFNotice(pdf, metricsEmptyMessage, pdfWarning)
		return
	}
	drawMetricsHeadline(pdf, data)
	if n.NoData {
		return
	}
	drawMetricsCharts(pdf, data)
	drawMetricsTables(pdf, data)
	drawMetricsRowCharts(pdf, data)
}
```

and in `drawMetricsHeadline`, directly after its `drawMetricsTitle(pdf, data)` line, insert:

```go
	if n.NoData {
		drawPDFNotice(pdf, "No data for this period", pdfMuted)
		drawMetricsNotes(pdf, n)
		return
	}
```

- [ ] **Step 9: Stop the footer starting a blank page**

In `backend/internal/services/pdf_renderer.go`, replace `drawPDFFooter` with:

```go
func drawPDFFooter(pdf *fpdf.Fpdf) {
	// The footer sits inside the bottom margin, below the page-break line.
	// With auto page break on, drawing there started a new page holding
	// nothing but the footer, so every report ended on a blank page.
	_, _, _, bottom := pdf.GetMargins()
	pdf.SetAutoPageBreak(false, bottom)
	defer pdf.SetAutoPageBreak(true, bottom)
	pdf.SetY(-15)
	pdf.SetFont("Helvetica", "I", 8)
	setColor(pdf, pdfMuted, false)
	pdf.CellFormat(pdfContentW, 6, "Generated by Sentinel", "", 0, "C", false, 0, "")
}
```

- [ ] **Step 10: Run the tests, then everything in the package**

Run: `go test ./internal/services/ -run 'TestMetricsPDF|TestPDFFooter|TestPDF|TestRender|TestUptimeGraph' -v`
Expected: PASS. `TestMetricsPDFLargestReport` takes about 0.2 s and renders around 119 pages.

Run: `go vet ./internal/services/ && go test ./internal/services/`
Expected: no vet output; `ok`.

- [ ] **Step 11: Commit**

```bash
git add backend/internal/services/pdf_metrics_tables.go backend/internal/services/pdf_metrics_tables_test.go \
  backend/internal/services/pdf_metrics.go backend/internal/services/pdf_renderer.go
git commit -m "feat(reports): metrics PDF tables, busiest-row charts, empty and no-data pages

Tables repeat their header on every page and cut long names to their column;
a 500-row x 10-table report with non-Latin names renders cleanly. The footer
no longer starts a blank last page on every report.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: A type-aware email summary line

`SummaryLines` builds the bullet list a scheduled email carries when the schedule asks for a summary. It becomes type-aware. A Metrics report gets one line built from the figures it has, for example "12 ports · billable 95th 640 Mbps (+18%) · 2.1 TB moved", and a second line when ports are running hot. Uptime and Incident reports keep their summary word for word.

The signature does not change. A report has network data (`data.Network != nil`) exactly when it is a Metrics report, because Part A's aggregator sets `Network` only on that branch. The one caller, `report_scheduler.go:251` (`email.Summary = SummaryLines(generated.Data)`), stays as it is.

**Files:**
- Modify: `backend/internal/services/report_mailer.go` (`SummaryLines`; two new functions after it)
- Modify: `backend/internal/services/report_mailer_test.go` (two new tests)

**Interfaces:**
- Consumes:
  - Task 10: `formatValue`, `formatBytes`, `formatChange`.
  - Task 11: `metricsSubjects(n *MetricsReportData) string` (`"12 ports"` / `"3 devices"` / `""`) and `countNoun(n int, one, many string) string`.
  - Task 12: `metricsEmptyMessage`.
  - Part A: `MetricsReportData` (with `Ports`/`Devices`, Contract change 1), `MetricsTile`, `HotPort`.
  - Test helper `f64` (`pdf_renderer_test.go`).
- Produces: `SummaryLines(data *ReportData) []string`, now type-aware; `func metricsSummaryLines(n *MetricsReportData) []string`; `func headlineFigures(tiles []MetricsTile) []string`.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/services/report_mailer_test.go`:

```go
func TestSummaryLinesMetrics(t *testing.T) {
	traffic := MetricsTile{Label: "Traffic", Unit: "bps", Kind: "traffic", First: 640e6, Second: f64(2.1e12), Change: f64(18)}
	cases := []struct {
		name string
		data MetricsReportData
		want []string
	}{
		{"traffic", MetricsReportData{Ports: 12, Tiles: []MetricsTile{traffic}},
			[]string{"12 ports · billable 95th 640 Mbps (+18%) · 2.1 TB moved"}},
		{"traffic first even when listed second", MetricsReportData{Ports: 1, Tiles: []MetricsTile{
			{Label: "Busy", Unit: "%", Kind: "percent", First: 12.34}, traffic}},
			[]string{"1 port · billable 95th 640 Mbps (+18%) · 2.1 TB moved"}},
		{"traffic new, no volume", MetricsReportData{Ports: 2, Tiles: []MetricsTile{
			{Label: "Traffic", Unit: "bps", Kind: "traffic", First: 1.25e9, New: true}}},
			[]string{"2 ports · billable 95th 1.25 Gbps (new)"}},
		{"no traffic: the first metric's average", MetricsReportData{Devices: 3, Tiles: []MetricsTile{
			{Label: "CPU", Unit: "%", Kind: "percent", NoData: true},
			{Label: "Memory used", Unit: "%", Kind: "percent", First: 23, Change: f64(-3.46)}}},
			[]string{"3 devices · Memory used average 23% (-3.5%)"}},
		{"running hot", MetricsReportData{Ports: 12, Tiles: []MetricsTile{traffic}, RunningHot: []HotPort{{}, {}}},
			[]string{"12 ports · billable 95th 640 Mbps (+18%) · 2.1 TB moved", "2 ports running hot (95th busy at or above 80%)"}},
		{"no data", MetricsReportData{Ports: 3, NoData: true}, []string{"3 ports · No data for this period"}},
		{"empty", MetricsReportData{Empty: true}, []string{metricsEmptyMessage}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := c.data
			got := SummaryLines(&ReportData{Network: &data})
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("got %q\nwant %q", got, c.want)
			}
		})
	}
}

// Uptime and Incident reports keep the monitor summary, word for word.
func TestSummaryLinesMonitorReportsUnchanged(t *testing.T) {
	got := SummaryLines(&ReportData{Metrics: []ReportMetrics{
		{MonitorName: "api", Uptime: 99, IncidentCount: 1, SLATarget: f64(99.5), SLAMet: false},
		{MonitorName: "web", Uptime: 97, IncidentCount: 0, SLATarget: f64(95), SLAMet: true},
	}})
	want := []string{"2 services, 98.00% average uptime, 1 incidents", "api missed its 99.50% SLA at 99.00%"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
```

All expected lines are worked out by hand:
- 640e6 bps prints as "640 Mbps", 2.1e12 bytes as "2.1 TB" and 1.25e9 bps as "1.25 Gbps".
- A change of -3.46 is under 10%, so it keeps one decimal: "-3.5%".
- In the monitor case, (99 + 97) / 2 = 98, which prints as "98.00%". Only `api` missed its SLA.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/services/ -run 'TestSummaryLines' -v`
Expected: `TestSummaryLinesMetrics` FAILS in every subtest. With no monitors in the data, each one gets `["No monitors were in scope for this report."]`. `TestSummaryLinesMonitorReportsUnchanged` and the existing `TestSummaryLines` PASS.

- [ ] **Step 3: Make the summary type-aware**

In `backend/internal/services/report_mailer.go`, replace the doc comment and the first lines of `SummaryLines`:

```go
// SummaryLines condenses report data into the bullet list an email can carry
// when the schedule asks for a summary in the body.
func SummaryLines(data *ReportData) []string {
	if data == nil || len(data.Metrics) == 0 {
```

with:

```go
// SummaryLines condenses report data into the bullet list an email can carry
// when the schedule asks for a summary in the body. A Metrics report (one
// with network data) gets its own summary; Uptime and Incident reports share
// the monitor summary below.
func SummaryLines(data *ReportData) []string {
	if data != nil && data.Network != nil {
		return metricsSummaryLines(data.Network)
	}
	if data == nil || len(data.Metrics) == 0 {
```

The rest of `SummaryLines` is unchanged. Directly after `SummaryLines`, before `// reportBaseURL returns the base URL`, add:

```go
// metricsSummaryLines condenses a Metrics report from the figures it has:
// "12 ports · billable 95th 640 Mbps (+18%) · 2.1 TB moved" when it carries
// traffic, otherwise the first metric's average ("3 devices · CPU average
// 23% (new)"), and a second line when ports are running hot.
func metricsSummaryLines(n *MetricsReportData) []string {
	if n.Empty {
		return []string{metricsEmptyMessage}
	}
	var parts []string
	if subjects := metricsSubjects(n); subjects != "" {
		parts = append(parts, subjects)
	}
	if n.NoData {
		return []string{strings.Join(append(parts, "No data for this period"), " · ")}
	}
	parts = append(parts, headlineFigures(n.Tiles)...)
	if len(parts) == 0 {
		return []string{"No data for this period"}
	}
	lines := []string{strings.Join(parts, " · ")}
	if len(n.RunningHot) > 0 {
		lines = append(lines, countNoun(len(n.RunningHot), "port", "ports")+" running hot (95th busy at or above 80%)")
	}
	return lines
}

// headlineFigures picks the summary's figures from the tiles: the first
// traffic tile's billable 95th and volume, or else the first tile with
// data's average. The change on the previous period follows in brackets.
func headlineFigures(tiles []MetricsTile) []string {
	change := func(t MetricsTile) string {
		if c := formatChange(t.Change, t.New); c != "" {
			return " (" + c + ")"
		}
		return ""
	}
	for _, t := range tiles {
		if t.Kind == "traffic" && !t.NoData {
			out := []string{"billable 95th " + formatValue(t.First, t.Unit) + change(t)}
			if t.Second != nil {
				out = append(out, formatBytes(*t.Second)+" moved")
			}
			return out
		}
	}
	for _, t := range tiles {
		if !t.NoData {
			return []string{t.Label + " average " + formatValue(t.First, t.Unit) + change(t)}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run them to see them pass**

Run: `go test ./internal/services/ -run 'TestSummaryLines|TestBuildMIME' -v && go vet ./internal/services/`
Expected: PASS, no vet output.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/report_mailer.go backend/internal/services/report_mailer_test.go
git commit -m "feat(reports): a metrics summary line in scheduled report emails

\"12 ports · billable 95th 640 Mbps (+18%) · 2.1 TB moved\", built from the
figures the report has; Uptime and Incident summaries are unchanged.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: API: Metrics reports in generate, and the scope preview

`POST /reports/generate` accepts Metrics reports. Before anything is saved, it checks the scope as the creator through `netreport.Builder.ValidateScope`. A `FieldError` answers 400 with its message, which is the same message for a hidden subject as for a missing one.

The new route `POST /reports/scope-preview` sizes a Metrics scope as the caller and lists its metrics and defaults for the wizard. It has its own rate-limit bucket, 60 a minute per user with a burst of 20 (Contract change 3). The wizard calls it about 400 ms after each scope change, so the generate route's 5 a minute would throttle normal use.

The scope code lives in a new file. `report_builder_handler.go` gets only four small edits and nothing in `ReportResponse` or `buildReportResponse`, because Part C's Task 20 adds `scope_data` there (Contract change 4).

**Files:**
- Create: `backend/internal/api/report_scope_handler.go`
- Create: `backend/internal/api/report_scope_handler_test.go`
- Create: `backend/internal/api/report_scope_handler_db_test.go`
- Modify: `backend/internal/api/report_builder_handler.go` (the `ReportBuilder` struct, `GenerateReportRequest`'s binding tags, `GenerateReport` after `report.Validate()`, `RegisterReportBuilderRoutes`)
- Modify: `backend/cmd/sentinel/main.go` (one line after `reportBuilder.SetAudit(auditService)`)

**Interfaces:**
- Consumes:
  - Part A (Tasks 1, 5, 6, 8):
    - `models.ReportTypeMetrics` and `models.ScopeTypePorts/PortRoles/Devices/Sites`.
    - `report.Validate()`, which pairs report types with scope types and checks the counts.
    - `netreport.NewBuilder(db *gorm.DB, metrics *services.MetricsStore) *netreport.Builder` and its methods:
      - `(*Builder).ValidateScope(ctx, requester uuid.UUID, scopeType string, scope models.ReportScope) error`
      - `(*Builder).Preview(ctx, requester uuid.UUID, scopeType string, scope models.ReportScope) (*netreport.Preview, error)`
    - `netreport.Preview{Ports, Devices int; Capped bool; Metrics []MetricChoice; Defaults []string}`, `netreport.MetricChoice{Key, Label, Unit, Source}`, `netreport.FieldError{Field, Message string}` and `netreport.MsgNotAvailable`.
    - In `main.go`, the `networkReports` variable Task 8 declares (`networkReports := netreport.NewBuilder(db, metricsStore)`, passed to `reportAggregator.SetNetworkBuilder`).
  - Existing api helpers: `respondError`, `respondSuccess`, `respondInternal`, `GetUserFromContext`, `NewRateLimiter(n, period, burst).Middleware(name, ByUser)`; test helper `do(r, method, path, body)` (`site_handler_test.go`).
  - Existing testdb helpers: `testdb.Open`, `testdb.NewUser`, `testdb.Exec`.
- Produces:
  - `type NetworkScopes interface { ValidateScope(...) error; Preview(...) (*netreport.Preview, error) }`
  - `func (h *ReportBuilder) SetNetworkScopes(n NetworkScopes)`
  - `func (h *ReportBuilder) ScopePreview(c *gin.Context)`
  - The HTTP contract:
    - `POST /api/v1/reports/scope-preview`. The body is `{scope_type, scope_data}`. It answers 200 `{success: true, data: Preview}`, or 400 `{success: false, error, field}`.
    - `POST /api/v1/reports/generate` accepts `report_type: "metrics"` and the four network scope types.
  - Test helpers: `fakeScopes`, `reportBuilderRouter(h, user)`, `reportScopeRouter(scopes, user)`, `errorText(t, body)`, `metricsReportBody(portID)`.

- [ ] **Step 1: Write the failing handler tests**

Create `backend/internal/api/report_scope_handler_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netreport"
)

// fakeScopes is a NetworkScopes that records its calls and answers with err
// (both methods) or preview.
type fakeScopes struct {
	err           error
	preview       *netreport.Preview
	validateCalls int
	previewCalls  int
	requester     uuid.UUID
	scopeType     string
	scope         models.ReportScope
}

func (f *fakeScopes) ValidateScope(_ context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) error {
	f.validateCalls++
	f.requester, f.scopeType, f.scope = requester, scopeType, scope
	return f.err
}

func (f *fakeScopes) Preview(_ context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) (*netreport.Preview, error) {
	f.previewCalls++
	f.requester, f.scopeType, f.scope = requester, scopeType, scope
	if f.err != nil {
		return nil, f.err
	}
	return f.preview, nil
}

// reportBuilderRouter mounts the report-builder routes on h for one
// signed-in, non-admin user.
func reportBuilderRouter(h *ReportBuilder, user uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", user)
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	RegisterReportBuilderRoutes(v1, h)
	return r
}

// reportScopeRouter is reportBuilderRouter with a fake scope checker and no
// database: these tests only reach paths that answer before one is needed.
func reportScopeRouter(scopes *fakeScopes, user uuid.UUID) *gin.Engine {
	h := &ReportBuilder{}
	h.SetNetworkScopes(scopes)
	return reportBuilderRouter(h, user)
}

// errorText decodes the error envelope's message.
func errorText(t *testing.T, body string) string {
	t.Helper()
	var env struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if env.Success {
		t.Errorf("body %q: success should be false", body)
	}
	return env.Error
}

func TestScopePreviewSizesTheScopeAsTheCaller(t *testing.T) {
	user, site := uuid.New(), uuid.New()
	scopes := &fakeScopes{preview: &netreport.Preview{Ports: 640, Devices: 12, Capped: true,
		Metrics:  []netreport.MetricChoice{{Key: "if_in_bps", Label: "Traffic in", Unit: "bps", Source: "builtin"}},
		Defaults: []string{"if_in_bps", "if_out_bps"}}}
	w := do(reportScopeRouter(scopes, user), http.MethodPost, "/api/v1/reports/scope-preview", map[string]any{
		"scope_type": "port_roles",
		"scope_data": map[string]any{"site_ids": []string{site.String()}, "roles": []string{"wan", "uplink"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Data netreport.Preview `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Ports != 640 || env.Data.Devices != 12 || !env.Data.Capped || len(env.Data.Metrics) != 1 || len(env.Data.Defaults) != 2 {
		t.Errorf("preview = %+v", env.Data)
	}
	if scopes.requester != user || scopes.scopeType != models.ScopeTypePortRoles ||
		len(scopes.scope.SiteIDs) != 1 || scopes.scope.SiteIDs[0] != site || len(scopes.scope.Roles) != 2 {
		t.Errorf("previewed %v %q %+v, want the caller's port_roles scope", scopes.requester, scopes.scopeType, scopes.scope)
	}
}

func TestScopePreviewAnswersFieldErrorsWithTheirMessage(t *testing.T) {
	scopes := &fakeScopes{err: &netreport.FieldError{Field: "port_ids", Message: netreport.MsgNotAvailable}}
	w := do(reportScopeRouter(scopes, uuid.New()), http.MethodPost, "/api/v1/reports/scope-preview", map[string]any{
		"scope_type": "ports", "scope_data": map[string]any{"port_ids": []string{uuid.NewString()}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if got := errorText(t, w.Body.String()); got != netreport.MsgNotAvailable {
		t.Errorf("error = %q, want %q", got, netreport.MsgNotAvailable)
	}
}

func TestScopePreviewHidesInternalErrors(t *testing.T) {
	scopes := &fakeScopes{err: errors.New(`pq: relation "secret_table" does not exist`)}
	w := do(reportScopeRouter(scopes, uuid.New()), http.MethodPost, "/api/v1/reports/scope-preview", map[string]any{
		"scope_type": "sites", "scope_data": map[string]any{"site_ids": []string{uuid.NewString()}},
	})
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "secret_table") {
		t.Errorf("status %d body %s: want a 500 without the database's text", w.Code, w.Body.String())
	}
}

func TestScopePreviewTakesOnlyNetworkScopes(t *testing.T) {
	scopes := &fakeScopes{preview: &netreport.Preview{}}
	r := reportScopeRouter(scopes, uuid.New())
	for _, body := range []map[string]any{
		{"scope_type": "monitors", "scope_data": map[string]any{"monitor_ids": []string{uuid.NewString()}}},
		{"scope_data": map[string]any{}},
	} {
		if w := do(r, http.MethodPost, "/api/v1/reports/scope-preview", body); w.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d, want 400", body, w.Code)
		}
	}
	if scopes.previewCalls != 0 {
		t.Errorf("an invalid body reached the scope checker %d times", scopes.previewCalls)
	}
}

// The editor previews about 400 ms after each change, so the preview has
// its own bucket: twenty at once, then it throttles without reaching the
// checker.
func TestScopePreviewIsRateLimited(t *testing.T) {
	scopes := &fakeScopes{preview: &netreport.Preview{}}
	r := reportScopeRouter(scopes, uuid.New())
	body := map[string]any{"scope_type": "sites", "scope_data": map[string]any{"site_ids": []string{uuid.NewString()}}}
	for i := 0; i < 20; i++ {
		if w := do(r, http.MethodPost, "/api/v1/reports/scope-preview", body); w.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i+1, w.Code)
		}
	}
	if w := do(r, http.MethodPost, "/api/v1/reports/scope-preview", body); w.Code != http.StatusTooManyRequests {
		t.Errorf("21st request: status %d, want 429", w.Code)
	}
	if scopes.previewCalls != 20 {
		t.Errorf("checker ran %d times, want 20 (the throttled request must not reach it)", scopes.previewCalls)
	}
}

// metricsReportBody is a valid Metrics report request on one port.
func metricsReportBody(portID uuid.UUID) map[string]any {
	return map[string]any{
		"name": "Uplink", "report_type": "metrics", "scope_type": "ports",
		"scope_data":      map[string]any{"port_ids": []string{portID.String()}, "metrics": []string{"if_in_bps", "if_out_bps"}},
		"time_range_days": 30,
	}
}

func TestGenerateMetricsReportChecksTheScopeAsTheCreator(t *testing.T) {
	user, port := uuid.New(), uuid.New()
	scopes := &fakeScopes{err: &netreport.FieldError{Field: "port_ids", Message: netreport.MsgNotAvailable}}
	w := do(reportScopeRouter(scopes, user), http.MethodPost, "/api/v1/reports/generate", metricsReportBody(port))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if got := errorText(t, w.Body.String()); got != netreport.MsgNotAvailable {
		t.Errorf("error = %q, want %q", got, netreport.MsgNotAvailable)
	}
	if scopes.validateCalls != 1 || scopes.requester != user || scopes.scopeType != models.ScopeTypePorts ||
		len(scopes.scope.PortIDs) != 1 || scopes.scope.PortIDs[0] != port || len(scopes.scope.Metrics) != 2 {
		t.Errorf("checked %d times as %v: %q %+v", scopes.validateCalls, scopes.requester, scopes.scopeType, scopes.scope)
	}
}

func TestGenerateRefusesAScopeTypeThatDoesNotMatchTheReportType(t *testing.T) {
	scopes := &fakeScopes{}
	r := reportScopeRouter(scopes, uuid.New())
	for _, body := range []map[string]any{
		{"name": "x", "report_type": "metrics", "scope_type": "monitors",
			"scope_data": map[string]any{"monitor_ids": []string{uuid.NewString()}, "metrics": []string{"if_in_bps"}}, "time_range_days": 30},
		{"name": "x", "report_type": "uptime", "scope_type": "ports",
			"scope_data": map[string]any{"port_ids": []string{uuid.NewString()}}, "time_range_days": 30},
	} {
		if w := do(r, http.MethodPost, "/api/v1/reports/generate", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s with %s: status %d, want 400", body["report_type"], body["scope_type"], w.Code)
		}
	}
	if scopes.validateCalls != 0 {
		t.Errorf("a mismatched report reached the scope checker %d times", scopes.validateCalls)
	}
}
```

The router's builder has no database, `jobs` or `audit`. Every case here answers before the handler would need one:
- the preview never touches the database;
- generate stops at binding, at `Validate`, or at the scope check.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/api/ -run 'TestScopePreview|TestGenerate' -v`
Expected: FAIL to build with `h.SetNetworkScopes undefined (type *ReportBuilder has no field or method SetNetworkScopes)`.

- [ ] **Step 3: Write the scope handlers**

Create `backend/internal/api/report_scope_handler.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netreport"
)

// This file holds the Metrics report's scope checks: the create-time check
// GenerateReport runs and POST /reports/scope-preview, which the editor
// calls as the user picks ports, devices and sites.

// NetworkScopes checks and sizes Metrics report scopes as a given user.
// netreport.Builder implements it; tests use a fake.
type NetworkScopes interface {
	ValidateScope(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) error
	Preview(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) (*netreport.Preview, error)
}

// SetNetworkScopes wires the Metrics scope checker in after construction.
func (h *ReportBuilder) SetNetworkScopes(n NetworkScopes) {
	h.scopes = n
}

// errScopesNotWired is a wiring mistake, reported as an internal error.
var errScopesNotWired = errors.New("network scope checker not wired")

// scopePreviewRequest is the body of POST /reports/scope-preview: a Metrics
// scope as the editor holds it. scope_data.metrics is ignored.
type scopePreviewRequest struct {
	ScopeType string             `json:"scope_type" binding:"required,oneof=ports port_roles devices sites"`
	ScopeData models.ReportScope `json:"scope_data"`
}

// checkNetworkScope runs a Metrics report's create-time scope check as its
// creator: every port, device and site it names must be one they can see,
// and a hidden one gets the same answer as a missing one. It writes the
// response and returns false when the request must stop. Other report types
// pass straight through.
func (h *ReportBuilder) checkNetworkScope(c *gin.Context, userID uuid.UUID, report *models.Report) bool {
	if report.ReportType != models.ReportTypeMetrics {
		return true
	}
	if h.scopes == nil {
		respondInternal(c, "checking report scope", errScopesNotWired)
		return false
	}
	if err := h.scopes.ValidateScope(c.Request.Context(), userID, report.ScopeType, report.ScopeData); err != nil {
		respondScopeError(c, "checking report scope", err)
		return false
	}
	return true
}

// ScopePreview handles POST /api/v1/reports/scope-preview. It sizes a
// Metrics scope as the caller ("Covers 214 ports right now") and lists the
// metrics it offers, with the defaults the editor pre-fills.
func (h *ReportBuilder) ScopePreview(c *gin.Context) {
	var req scopePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	userID, _, _, ok := GetUserFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.scopes == nil {
		respondInternal(c, "previewing report scope", errScopesNotWired)
		return
	}
	preview, err := h.scopes.Preview(c.Request.Context(), userID, req.ScopeType, req.ScopeData)
	if err != nil {
		respondScopeError(c, "previewing report scope", err)
		return
	}
	respondSuccess(c, http.StatusOK, preview)
}

// respondScopeError answers a scope problem the user can fix with 400 and
// its message, naming the field for the editor, and anything else as an
// internal error whose text stays in the server log.
func respondScopeError(c *gin.Context, op string, err error) {
	var fe *netreport.FieldError
	if errors.As(err, &fe) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": fe.Message, "field": fe.Field})
		return
	}
	respondInternal(c, op, err)
}
```

- [ ] **Step 4: Wire them into the report builder**

In `backend/internal/api/report_builder_handler.go`, make four edits.

(a) In the `ReportBuilder` struct, after the `settings *services.SettingsService` field and its comment, add:

```go
	// scopes checks and sizes Metrics report scopes as the caller
	// (report_scope_handler.go).
	scopes NetworkScopes
```

(b) In `GenerateReportRequest`, replace the two binding lines:

```go
	ReportType string             `json:"report_type" binding:"required,oneof=uptime incident"`
	ScopeType  string             `json:"scope_type" binding:"required,oneof=monitors tags groups types"`
```

with:

```go
	ReportType string             `json:"report_type" binding:"required,oneof=uptime incident metrics"`
	ScopeType  string             `json:"scope_type" binding:"required,oneof=monitors tags groups types ports port_roles devices sites"`
```

(c) In `GenerateReport`, directly after the `report.Validate()` block (the `if err := report.Validate(); err != nil { respondError(...); return }`) and before `access := models.ReportAccess{`, add:

```go
	if !h.checkNetworkScope(c, userID, &report) {
		return
	}
```

(d) In `RegisterReportBuilderRoutes`, directly after `reports.POST("/generate", generateLimit, builder.GenerateReport)`, add:

```go
	// The Metrics editor sizes its scope about 400 ms after each change, so
	// the preview has its own, looser bucket and never uses up the generate
	// allowance.
	previewLimit := NewRateLimiter(60, time.Minute, 20).Middleware("report-scope-preview", ByUser)
	reports.POST("/scope-preview", previewLimit, builder.ScopePreview)
```

`/reports/scope-preview` is a static path beside the existing `/:id/...` routes, the same way `/generate` and `/jobs/:job_id` already are, so gin's router accepts it. No imports change in this file.

- [ ] **Step 5: Run the handler tests**

Run: `go test ./internal/api/ -run 'TestScopePreview|TestGenerate' -v`
Expected: PASS (7 tests).

- [ ] **Step 6: Pin hidden-versus-missing at the HTTP level, with the real builder**

Create `backend/internal/api/report_scope_handler_db_test.go`:

```go
package api

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/netreport"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A port in a site the user cannot see and a port that does not exist must
// look the same over HTTP, byte for byte, in the preview and on create: the
// editor cannot be used to probe ids.
func TestDBScopeHiddenAndMissingLookAlike(t *testing.T) {
	db := testdb.Open(t)
	user := testdb.NewUser(t, db, false)
	site, cred, device, port := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Hidden')`, site)
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, 'cred', '2c', 'x')`, cred)
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, 'core-sw1', '10.0.0.2')`,
		device, site, cred)
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name, alias, if_type, present, oper_status, admin_status)
		VALUES (?, ?, 1, 'Gi1/0/1', 'uplink', 6, true, 'up', 'up')`, port, device)

	h := &ReportBuilder{db: db}
	h.SetNetworkScopes(netreport.NewBuilder(db, services.NewMetricsStore(db)))
	r := reportBuilderRouter(h, user)

	pairs := []struct {
		name           string
		hidden, absent map[string]any
		path           string
	}{
		{"preview ports",
			map[string]any{"scope_type": "ports", "scope_data": map[string]any{"port_ids": []string{port.String()}}},
			map[string]any{"scope_type": "ports", "scope_data": map[string]any{"port_ids": []string{uuid.NewString()}}},
			"/api/v1/reports/scope-preview"},
		{"preview sites",
			map[string]any{"scope_type": "sites", "scope_data": map[string]any{"site_ids": []string{site.String()}}},
			map[string]any{"scope_type": "sites", "scope_data": map[string]any{"site_ids": []string{uuid.NewString()}}},
			"/api/v1/reports/scope-preview"},
		{"preview devices",
			map[string]any{"scope_type": "devices", "scope_data": map[string]any{"device_ids": []string{device.String()}}},
			map[string]any{"scope_type": "devices", "scope_data": map[string]any{"device_ids": []string{uuid.NewString()}}},
			"/api/v1/reports/scope-preview"},
		{"create", metricsReportBody(port), metricsReportBody(uuid.New()), "/api/v1/reports/generate"},
	}
	for _, p := range pairs {
		hidden := do(r, http.MethodPost, p.path, p.hidden)
		absent := do(r, http.MethodPost, p.path, p.absent)
		if hidden.Code != http.StatusBadRequest || absent.Code != http.StatusBadRequest {
			t.Errorf("%s: statuses %d and %d, want 400 for both", p.name, hidden.Code, absent.Code)
			continue
		}
		if hidden.Body.String() != absent.Body.String() {
			t.Errorf("%s: hidden %s differs from missing %s", p.name, hidden.Body.String(), absent.Body.String())
		}
		if got := errorText(t, hidden.Body.String()); got != netreport.MsgNotAvailable {
			t.Errorf("%s: error %q, want %q", p.name, got, netreport.MsgNotAvailable)
		}
	}

	// Shared with the user, the same port is sized.
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, site, user)
	w := do(r, http.MethodPost, "/api/v1/reports/scope-preview", pairs[0].hidden)
	if w.Code != http.StatusOK {
		t.Fatalf("visible port: status %d: %s", w.Code, w.Body.String())
	}
}
```

Run: `./scripts/test-db.sh -run TestDBScopeHiddenAndMissingLookAlike -v`
Expected: PASS. Each of the four pairs answers 400 with byte-identical bodies of the form `{"error":"A chosen port, device or site is not available","field":"…","success":false}`. Once the site is shared, the port previews with 200.

If a pair's bodies differ, the fault is in Part A's `ValidateScope` or `Preview`: both must return the same `FieldError` (`Field` and `Message`) for hidden and missing. Fix it there, not here.

- [ ] **Step 7: Wire the builder in `main.go`**

In `backend/cmd/sentinel/main.go`, directly after `reportBuilder.SetAudit(auditService)`, add:

```go
	// Metrics reports: create-time scope checks and the editor's scope preview.
	reportBuilder.SetNetworkScopes(networkReports)
```

`networkReports` is the `*netreport.Builder` that Task 8 declares after `deviceService.SetMetricsStore(metricsStore)` and passes to `reportAggregator.SetNetworkBuilder`. If Task 8 named the variable differently, use that name. If it passed `netreport.NewBuilder(db, metricsStore)` inline, hoist the call into `networkReports := netreport.NewBuilder(db, metricsStore)` on the line before, and pass `networkReports` to both. One builder serves the aggregator and the API.

- [ ] **Step 8: Build, vet and run the package**

Run: `go build ./... && go vet ./... && go test ./internal/api/`
Expected: builds, no vet output, `ok`.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/api/report_scope_handler.go backend/internal/api/report_scope_handler_test.go \
  backend/internal/api/report_scope_handler_db_test.go backend/internal/api/report_builder_handler.go \
  backend/cmd/sentinel/main.go
git commit -m "feat(reports): metrics reports in generate, and POST /reports/scope-preview

Create checks a metrics scope as the creator (hidden and missing subjects
answer alike); the preview sizes a scope and lists its metrics for the
wizard, on its own rate-limit bucket.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 15: Simulator end to end, and the full backend suite

A real polled device must produce a real Metrics PDF through the same path a scheduled run takes (`ReportGenerator.GenerateAndSaveReport`). The test polls the simulated Cisco 2960X through the starter health profile, as `internal/dashboards/widget_sim_db_test.go` and `internal/services/profile_sim_db_test.go` do. It then generates a Metrics report on that device's CPU and checks four things:

- a non-empty PDF on disk;
- the statistics the simulator implies (Switch 1 at 23%, ranked above the second CPU row at 4%);
- the rows ranked in that order;
- a generation row recorded.

After that, the whole backend suite runs: vet, unit tests, and the full DB suite with the simulator.

**Files:**
- Create: `backend/internal/netreport/report_sim_db_test.go`

**Interfaces:**
- Consumes:
  - Part A: `NewBuilder(db, metrics)` (same package), `services.(*ReportAggregatorService).SetNetworkBuilder`, `models.ReportTypeMetrics` and `models.ScopeTypeDevices`, the 058 CHECKs accepting `metrics`/`devices`, and `ReportData.Network`.
  - Existing services: `NewMIBLibrary(db).SyncBuiltins`, `NewProfileService(db).SeedStarter/Load`, `NewMetricsStore`, `NewIncidentService`, `NewPortService(db, metrics, incidents, NewSettingsService(db))`, `NewProfileMonitor(profiles, metrics, incidents, notifier, snmp.GoSNMPClient{}, ports)` with `.PollProfiles(ctx, device, target, -1)`, `NewReportAggregatorService(db, nil)`, `NewPDFRendererService(dir)`, `NewReportGenerator(db, agg, renderer, nil)` with `.GenerateAndSaveReport(ctx, &report, userID) (*GeneratedReport, error)`.
  - `testdb.Open`, `testdb.NewUser`, `testdb.Exec`, `testdb.Must`.
- Produces: `TestDBSimMetricsReportEndToEnd`. It is gated on `SENTINEL_TEST_SNMPSIM` and on a database.

Facts the test relies on, all checked while this plan was written:
- The simulator's community `cisco` answers as a 2960X (`sys_object_id 1.3.6.1.4.1.9.1.1208`). The starter profile's `cisco_cpu_5min` reads instance `1` "Switch 1" = 23 and instance `2` "Row 2" = 4 (see `TestDBSimCiscoHealthEndToEnd`).
- The poll writes its samples at the current time, so they land in the 5-minute bucket that is still filling, and a report counts only complete buckets (Review Focus 1, Task 3). Copying the samples 15 minutes back puts the same readings in a complete bucket. A probe run showed `samples_5m` then holds Switch 1 = 23 and Row 2 = 4 in a bucket ending at least 10 minutes before now. The copy is an `INSERT … SELECT`, not an `UPDATE`, because an update that moved a row across the hypertable's 1-day chunk boundary would fail.

- [ ] **Step 1: Write the end-to-end test**

Create `backend/internal/netreport/report_sim_db_test.go`:

```go
package netreport

import (
	"context"
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

type simReportNotifier struct{}

func (simReportNotifier) SendNotification(context.Context, *notifications.NotificationMessage) error {
	return nil
}

// TestDBSimMetricsReportEndToEnd polls the simulated Cisco 2960X (community
// "cisco") through the starter health profile, then generates a Metrics
// report on that device's CPU through the same path a scheduled run takes
// (ReportGenerator.GenerateAndSaveReport): statistics from the 5-minute
// rollup, the busiest row first, a PDF on disk and a generation recorded.
func TestDBSimMetricsReportEndToEnd(t *testing.T) {
	addr := os.Getenv("SENTINEL_TEST_SNMPSIM")
	if addr == "" {
		t.Skip("SENTINEL_TEST_SNMPSIM not set; start deploy/snmpsim/run.sh")
	}
	host, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)

	lib := services.NewMIBLibrary(db)
	testdb.Must(t, lib.SyncBuiltins(ctx))
	profiles := services.NewProfileService(db)
	testdb.Must(t, profiles.SeedStarter(ctx))
	testdb.Must(t, profiles.Load(ctx))

	site, cred, deviceID := uuid.New(), uuid.New(), uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Sim')`, site)
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, 'sim', '2c', 'cisco')`, cred)
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host, sys_object_id)
		VALUES (?, ?, ?, 'sim-cisco', ?, '1.3.6.1.4.1.9.1.1208')`, deviceID, site, cred, host)
	var device models.Device
	testdb.Must(t, db.First(&device, "id = ?", deviceID).Error)

	metrics := services.NewMetricsStore(db)
	incidents := services.NewIncidentService(db)
	ports := services.NewPortService(db, metrics, incidents, services.NewSettingsService(db))
	mon := services.NewProfileMonitor(profiles, metrics, incidents, simReportNotifier{}, snmp.GoSNMPClient{}, ports)
	target := snmp.Target{Host: host, Port: uint16(port), Credential: snmp.Credential{Version: "2c", Community: "cisco"},
		Timeout: 2 * time.Second, Retries: 1}
	mon.PollProfiles(ctx, device, target, -1)

	// The poll wrote its samples now, into the 5-minute bucket still
	// filling, which a report never counts. Copy them 15 minutes back so one
	// complete bucket holds the simulator's readings.
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT time - INTERVAL '15 minutes', series_id, value FROM metrics.samples`)

	agg := services.NewReportAggregatorService(db, nil)
	agg.SetNetworkBuilder(NewBuilder(db, metrics))
	renderer, err := services.NewPDFRendererService(t.TempDir())
	testdb.Must(t, err)
	gen := services.NewReportGenerator(db, agg, renderer, nil)

	report := models.Report{
		ID: uuid.New(), UserID: admin, CreatedBy: admin, Name: "Sim CPU",
		ReportType: models.ReportTypeMetrics, ScopeType: models.ScopeTypeDevices,
		ScopeData:     models.ReportScope{DeviceIDs: []uuid.UUID{deviceID}, Metrics: []string{"cisco_cpu_5min"}},
		TimeRangeDays: 1, PeriodKind: models.PeriodRolling,
	}
	testdb.Must(t, report.Validate())
	testdb.Must(t, db.Create(&report).Error)

	out, err := gen.GenerateAndSaveReport(ctx, &report, admin)
	testdb.Must(t, err)

	raw, err := os.ReadFile(out.Path)
	testdb.Must(t, err)
	if !strings.HasPrefix(string(raw), "%PDF-") || len(raw) < 2000 {
		t.Errorf("PDF is %d bytes starting %q; want a real document", len(raw), raw[:min(8, len(raw))])
	}
	n := out.Data.Network
	if n == nil || n.Empty || n.NoData || len(n.Tables) == 0 {
		t.Fatalf("network data = %+v, want a table of the device's CPU", n)
	}
	rows := n.Tables[0].Rows
	// The simulator reports switch 1 at 23% and the second CPU row at 4%.
	if len(rows) != 2 || !strings.Contains(rows[0].Name, "Switch 1") || rows[0].In == nil || rows[0].In.Avg != 23 ||
		rows[1].In == nil || rows[1].In.Avg != 4 {
		t.Errorf("CPU rows = %+v, want Switch 1 at 23 ranked above the second row at 4", rows)
	}
	var generations int64
	testdb.Must(t, db.Model(&models.ReportGeneration{}).Where("report_id = ?", report.ID).Count(&generations).Error)
	if generations != 1 {
		t.Errorf("%d generations recorded, want 1", generations)
	}
}
```

The notifier type is named `simReportNotifier` so it does not collide with test helpers that Tasks 5-7 add to this package. Every fixture is inserted inline for the same reason.

- [ ] **Step 2: Start the simulator and run the test**

From the repository root:

```bash
cd deploy/snmpsim && SNMPSIM_VERSION=1.2.2 ./run.sh && cd ../..
```

Expected: `sentinel-snmpsim running on UDP 1161-1190`.

From `backend/`:

```bash
SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 ./scripts/test-db.sh -run TestDBSimMetricsReportEndToEnd -v
```

Expected: PASS. A log line about the simulator's Fan 2 incident (`metric=cisco_fan_envmon instance=2`) is normal. If it fails with `aggregating report data`, Task 8's Metrics branch is not reached. If the rows are empty or `NoData` is set, check Task 3's complete-bucket rule against the copied samples.

The test is written to pass on the first run against a correct Part A. There is no separate failing run: it exercises code that Tasks 1-14 already built.

- [ ] **Step 3: Run the whole backend suite**

From `backend/`:

```bash
go vet ./...
go test ./...
SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 ./scripts/test-db.sh
```

Expected: no vet output. Every package reports `ok` (or `no test files`) in both test runs. The DB run includes every `TestDB…` test from Tasks 1-15 and the existing simulator tests (`TestDBSimCiscoHealthEndToEnd`, `TestDBSimWidgetsEndToEnd`).

Then stop the simulator: `docker rm -f sentinel-snmpsim`.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/netreport/report_sim_db_test.go
git commit -m "test(reports): a polled simulator device yields a metrics report PDF

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 16: Types, the scope-preview hook and scope helpers

The frontend gains the `metrics` report type, the four network scopes, and the preview types. It also gains a debounced, guarded hook for `POST /reports/scope-preview` and one hook that holds the whole Metrics draft (scope, preview, ordered metrics) for both builders. The pure helpers live in `utils/reportScope.ts`: scope payloads, validation, `describeScope`, `comparisonLabel`, the size line, port names, and the spec's default-metrics re-apply rule. The frontend has no unit-test runner, so a throwaway assertion script, bundled with the esbuild that Vite already installs, checks the helpers. It is not committed.

**Files:**
- Modify: `frontend/src/types/reports.ts` (the head through `REPORT_TYPE_LABEL`, `SavedReport`, `ReportScopeData`, and new types appended)
- Create: `frontend/src/utils/reportScope.ts`
- Create: `frontend/src/hooks/useScopePreview.ts`
- Create: `frontend/src/hooks/useMetricsReportDraft.ts`
- Test: a throwaway `check.ts` in a temp directory (not committed), plus the frontend gate

**Interfaces:**
- Consumes (HTTP, Task 14): `POST /api/v1/reports/scope-preview`, body `{ scope_type, scope_data }`, 200 `{ success: true, data: Preview }`, 400 `{ error: "<message>" }`. Here `Preview` = `{ ports: number, devices: number, capped: boolean, metrics: {key, label, unit, source}[], defaults: string[] }`, and `source` is `"builtin" | "profile" | "custom"`. The `api` client (`@/services/api`) normalises errors to `ApiError { status, message }`.
- Consumes: `describePeriod(p: ReportPeriod): string` and `ReportPeriod` (`@/components/PeriodSelector`, `@/types/reports`); `PortRole = 'access' | 'uplink' | 'wan'` (`@/hooks/useDevices`).
- Produces (`@/types/reports`):
  - Scope types: `MonitorScopeType = 'monitors' | 'tags' | 'groups' | 'types'`, `NetworkScopeType = 'ports' | 'port_roles' | 'devices' | 'sites'`, `ReportScopeType = MonitorScopeType | NetworkScopeType`.
  - Report types: `ReportType = 'uptime' | 'incident' | 'metrics'`, `REPORT_TYPES: ReportType[]`, `REPORT_TYPE_LABEL` (adds `metrics: 'Metrics Report'`), `REPORT_TYPE_BLURB: Record<ReportType, string>`, `SCOPE_TYPE_LABEL: Record<ReportScopeType, string>`.
  - Limits: `MAX_REPORT_SUBJECTS = 500`, `MAX_REPORT_METRICS = 10`.
  - `ReportScopeData` gains `types?`, `port_ids?`, `site_ids?`, `device_ids?`, `roles?: PortRole[]` and `metrics?`. `SavedReport.scope_data?: ReportScopeData`.
  - Preview types: `MetricSource`, `MetricChoice { key, label, unit, source }`, `ScopePreview { ports, devices, capped, metrics, defaults }`.
- Produces (`@/utils/reportScope`):
  - Draft types: `ScopeChoice { id, name }` and `NetworkScopeDraft { scopeType, ports, sites, roles, devices }`.
  - Constants: `EMPTY_NETWORK_SCOPE`, `NETWORK_SCOPE_TABS`, `REPORT_ROLES`, `METRIC_SOURCE_LABEL`.
  - Scope payloads and checks: `monitorScopeData(t: MonitorScopeType, ids: string[]): ReportScopeData`, `networkScopeData(d: NetworkScopeDraft): ReportScopeData`, `networkScopeError(d: NetworkScopeDraft): string | null`.
  - Names: `ScopeNames { sites: Map, devices: Map }`, `scopeNames(sites, devices): ScopeNames`, `describeScope(scopeType, scope: ReportScopeData | undefined, names: ScopeNames): string`.
  - Labels: `previewSizeLine(scopeType: NetworkScopeType, p: ScopePreview): string`, `portChoiceName(deviceName, port): string`, `comparisonLabel(p: ReportPeriod, now?: Date): string`.
  - Metrics: `MetricsSelection { metrics, edited, dropped, choices }`, `EMPTY_METRICS_SELECTION`, `reapplyMetrics(cur: MetricsSelection, next: ScopePreview): MetricsSelection`, `moveItem<T>(list: T[], index: number, delta: -1 | 1): T[]`.
- Produces (`@/hooks/useScopePreview`):
  - `ScopePreviewState { data: ScopePreview | null; error: string | null; loading: boolean }`.
  - `useScopePreview(scopeType: NetworkScopeType | null, scopeData: ReportScopeData | null): ScopePreviewState`.
- Produces (`@/hooks/useMetricsReportDraft`): `useMetricsReportDraft(enabled: boolean)` returns:
  - `scope`, `setScope`, `scopeError`, `scopeData` (without metrics), `preview: ScopePreviewState`.
  - `scopeReady: boolean`.
  - `selection: MetricsSelection`, `setMetrics(keys: string[])`, `metricsError: string | null`.
  - `payloadData: ReportScopeData` (with metrics), `names: ScopeNames`, `reset()`.

- [ ] **Step 1: Run the frontend gate on the untouched tree**

This installs `frontend/node_modules` (the check script below uses its esbuild) and shows a green baseline. From the repo root (never run npm as root):

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output.

- [ ] **Step 2: Write the helper check (throwaway, outside the repo)**

```bash
export CHECK_DIR=$(mktemp -d)
cat > "$CHECK_DIR/check.ts" <<'EOF'
import assert from 'node:assert/strict'
import {
  comparisonLabel,
  describeScope,
  EMPTY_NETWORK_SCOPE,
  monitorScopeData,
  moveItem,
  networkScopeData,
  networkScopeError,
  portChoiceName,
  previewSizeLine,
  reapplyMetrics,
  scopeNames,
} from '@/utils/reportScope'
import type { MetricChoice, ScopePreview } from '@/types/reports'

const names = scopeNames(
  [{ id: 's1', name: 'HQ' }, { id: 's2', name: 'Annex' }, { id: 's3', name: 'Depot' }, { id: 's4', name: 'Yard' }],
  [{ id: 'd1', name: 'core-sw1' }],
)
const four = ['a', 'b', 'c', 'd']

// describeScope
assert.equal(
  describeScope('port_roles', { site_ids: ['s1', 's2'], roles: ['uplink', 'wan'], metrics: four }, names),
  'WAN, Uplink ports at HQ, Annex · 4 metrics',
)
assert.equal(describeScope('port_roles', { site_ids: ['x', 'y'], roles: ['wan'] }, names), 'WAN ports at 2 sites')
assert.equal(describeScope('ports', { port_ids: ['p1', 'p2', 'p3'], metrics: ['a'] }, names), '3 ports · 1 metric')
assert.equal(describeScope('devices', { device_ids: ['d1', 'gone'], metrics: ['a', 'b'] }, names), 'Devices: core-sw1 and 1 more · 2 metrics')
assert.equal(describeScope('devices', { device_ids: ['gone'] }, names), '1 device')
assert.equal(describeScope('sites', { site_ids: ['s1', 's2', 's3', 's4', 's5'] }, names), 'Sites: HQ, Annex, Depot and 2 more')
assert.equal(describeScope('monitors', { monitor_ids: ['m1'] }, names), 'monitors')
assert.equal(describeScope('ports', undefined, names), '0 ports')

// comparisonLabel, with today = Saturday 3 October 2026
const now = new Date(2026, 9, 3)
assert.equal(comparisonLabel({ period_kind: 'calendar', period_unit: 'month', period_offset: 1 }, now), 'Compared with August 2026')
assert.equal(comparisonLabel({ period_kind: 'calendar', period_unit: 'month', period_offset: 0 }, now), 'Compared with September 2026')
assert.equal(comparisonLabel({ period_kind: 'calendar', period_unit: 'quarter', period_offset: 1 }, now), 'Compared with Q2 2026')
assert.equal(comparisonLabel({ period_kind: 'calendar', period_unit: 'quarter', period_offset: 0 }, new Date(2026, 0, 15)), 'Compared with Q4 2025')
assert.equal(comparisonLabel({ period_kind: 'calendar', period_unit: 'year', period_offset: 0 }, now), 'Compared with 2025')
assert.equal(comparisonLabel({ period_kind: 'calendar', period_unit: 'week', period_offset: 1 }, now), 'Compared with Week of September 14, 2026')
assert.equal(comparisonLabel({ period_kind: 'rolling', time_range_days: 7 }, now), 'Compared with the previous 7 days')
assert.equal(comparisonLabel({ period_kind: 'rolling', time_range_days: 1 }, now), 'Compared with the previous 24 hours')
assert.equal(comparisonLabel({ period_kind: 'rolling', time_range_days: 45 }, now), 'Compared with the previous 45 days')
assert.equal(
  comparisonLabel({ period_kind: 'custom', period_start: '2026-09-01T00:00:00Z', period_end: '2026-09-15T23:59:59Z' }, now),
  'Compared with August 17, 2026 to August 31, 2026',
)
assert.equal(
  comparisonLabel({ period_kind: 'custom', period_start: '2026-09-01T00:00:00Z', period_end: '2026-09-01T23:59:59Z' }, now),
  'Compared with August 31, 2026',
)
assert.equal(comparisonLabel({ period_kind: 'custom', period_start: '2026-09-01T00:00:00Z' }, now), '')

// The default-metrics re-apply rule
const choice = (key: string, label: string): MetricChoice => ({ key, label, unit: 'bps', source: 'builtin' })
const preview = (metrics: MetricChoice[], defaults: string[]): ScopePreview => ({ ports: 1, devices: 1, capped: false, metrics, defaults })
const inOut = preview([choice('if_in_bps', 'Traffic in'), choice('if_out_bps', 'Traffic out')], ['if_in_bps', 'if_out_bps'])

assert.deepEqual(reapplyMetrics({ metrics: ['cpu_pct'], edited: false, dropped: ['old'], choices: [] }, inOut), {
  metrics: ['if_in_bps', 'if_out_bps'],
  edited: false,
  dropped: [],
  choices: inOut.metrics,
})
assert.deepEqual(
  reapplyMetrics({ metrics: ['cpu_pct', 'if_out_bps'], edited: true, dropped: [], choices: [choice('cpu_pct', 'CPU')] }, inOut),
  { metrics: ['if_out_bps'], edited: true, dropped: ['CPU'], choices: inOut.metrics },
)
assert.deepEqual(
  reapplyMetrics({ metrics: ['cpu_pct', 'mystery'], edited: true, dropped: [], choices: [choice('cpu_pct', 'CPU')] }, inOut),
  { metrics: ['if_in_bps', 'if_out_bps'], edited: false, dropped: ['CPU', 'mystery'], choices: inOut.metrics },
)
const twelve = Array.from({ length: 12 }, (_, i) => `m${i}`)
assert.equal(reapplyMetrics({ metrics: [], edited: false, dropped: [], choices: [] }, preview([], twelve)).metrics.length, 10)

// Scope payloads and validation
assert.deepEqual(monitorScopeData('types', ['http']), { types: ['http'] })
assert.deepEqual(monitorScopeData('groups', ['g1']), { group_ids: ['g1'] })
const draft = { ...EMPTY_NETWORK_SCOPE, sites: [{ id: 's1', name: 'HQ' }], roles: ['access' as const, 'wan' as const] }
assert.deepEqual(networkScopeData({ ...draft, scopeType: 'port_roles' }), { site_ids: ['s1'], roles: ['wan', 'access'] })
assert.deepEqual(networkScopeData({ ...draft, scopeType: 'sites' }), { site_ids: ['s1'] })
assert.equal(networkScopeError(EMPTY_NETWORK_SCOPE), 'Choose at least one port')
assert.equal(networkScopeError({ ...draft, scopeType: 'port_roles', roles: [] }), 'Choose at least one port role')
assert.equal(networkScopeError({ ...draft, scopeType: 'port_roles' }), null)
const many = Array.from({ length: 501 }, (_, i) => ({ id: `p${i}`, name: `p${i}` }))
assert.equal(networkScopeError({ ...draft, scopeType: 'ports', ports: many }), 'Choose at most 500 ports')

// Size line, port names, reordering
assert.equal(previewSizeLine('port_roles', { ...inOut, ports: 214 }), 'Covers 214 ports right now')
assert.equal(previewSizeLine('sites', { ...inOut, ports: 640, devices: 30, capped: true }), 'Covers 640 ports on 30 devices — the report will include the 500 busiest')
assert.equal(previewSizeLine('devices', { ...inOut, devices: 1 }), 'Covers 1 device right now')
assert.equal(portChoiceName('core-sw1', { name: 'Gi1/0/1', label: '1', number: 1, alias: 'uplink to annex' }), 'core-sw1 · Gi1/0/1 (uplink to annex)')
assert.equal(portChoiceName('sw2', { name: '', label: '1/4', number: 4, alias: '' }), 'sw2 · 1/4')
assert.deepEqual(moveItem(['a', 'b', 'c'], 2, -1), ['a', 'c', 'b'])
assert.deepEqual(moveItem(['a', 'b', 'c'], 0, -1), ['a', 'b', 'c'])

console.log('reportScope: all checks passed')
EOF
```

The expected values are hand-computed:
- Today is Saturday 3 October 2026. "Last month" is September, so it compares with August. "Last quarter" is Q3, so it compares with Q2. "Last week" started on Monday 21 September, so it compares with the week of 14 September.
- A custom 1-15 September is 15 days, so it compares with 17-31 August.
- `describeScope` lists at most three names, then "and N more". An unknown id is counted, never shown.

- [ ] **Step 3: Run it to see it fail**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app:ro -v "$CHECK_DIR":/check -w /app node:20-alpine sh -c "node_modules/.bin/esbuild /check/check.ts --bundle --platform=node --format=esm --jsx=automatic --tsconfig=/app/tsconfig.json --log-level=warning --outfile=/tmp/check.mjs && node /tmp/check.mjs"
```

Expected: FAIL — `Could not resolve "@/utils/reportScope"`.

- [ ] **Step 4: Add the types**

In `frontend/src/types/reports.ts`, replace everything from the top of the file through the closing `}` of `REPORT_TYPE_LABEL` (the file comment, `ReportScopeType`, `ScheduleType`, `ReportType`, `REPORT_TYPE_LABEL`) with:

```ts
// Types for the saved-report builder: definitions, generations, schedules, and
// the templates that shape them. These mirror the backend models in
// internal/models/report.go and report_schedule.go.

import type { PortRole } from '@/hooks/useDevices'

/** What an Uptime or Incident report covers. */
export type MonitorScopeType = 'monitors' | 'tags' | 'groups' | 'types'
/** What a Metrics report covers (models.ScopeTypePorts and its siblings). */
export type NetworkScopeType = 'ports' | 'port_roles' | 'devices' | 'sites'
export type ReportScopeType = MonitorScopeType | NetworkScopeType
export type ScheduleType = 'daily' | 'weekly' | 'monthly' | 'quarterly' | 'custom'

/** The three fixed report types. No template system, no section picker. */
export type ReportType = 'uptime' | 'incident' | 'metrics'

/** The types in the order the builders offer them. */
export const REPORT_TYPES: ReportType[] = ['uptime', 'incident', 'metrics']

export const REPORT_TYPE_LABEL: Record<ReportType, string> = {
  uptime: 'Uptime Report',
  incident: 'Incident Report',
  metrics: 'Metrics Report',
}

/** One line under each type's card, in both builders. */
export const REPORT_TYPE_BLURB: Record<ReportType, string> = {
  uptime: 'Uptime vs. SLA, with a cumulative-uptime graph and a per-monitor breakdown.',
  incident: 'Every incident in scope, with root cause and resolution detail.',
  metrics:
    'Traffic, busy %, errors and device health for ports, devices or sites: 95th percentiles, totals, and the change from the previous period.',
}

/** A scope type in words, where only the kind of scope is shown. */
export const SCOPE_TYPE_LABEL: Record<ReportScopeType, string> = {
  monitors: 'Monitors',
  tags: 'Tags',
  groups: 'Groups',
  types: 'Types',
  ports: 'Ports',
  port_roles: 'Port roles',
  devices: 'Devices',
  sites: 'Sites',
}

/** At most this many ports or devices in a Metrics report (models.MaxReportSubjects). */
export const MAX_REPORT_SUBJECTS = 500
/** At most this many metrics in a Metrics report (models.MaxReportMetrics). */
export const MAX_REPORT_METRICS = 10
```

In `interface SavedReport`, after `scope_type: ReportScopeType`, add:

```ts
  /** What the report covers. Absent on a share-link response. */
  scope_data?: ReportScopeData
```

Replace the whole `ReportScopeData` interface (and its one-line doc comment) with:

```ts
/**
 * Scope payload, matching scope_type: a monitor scope sets its one field; a
 * network scope sets its ids (and roles for port_roles) plus metrics.
 */
export interface ReportScopeData {
  monitor_ids?: string[]
  tags?: string[]
  group_ids?: string[]
  /** Monitor check types: http, dns, ping, tcp. */
  types?: string[]
  /** device_interfaces ids, 1-500. */
  port_ids?: string[]
  site_ids?: string[]
  /** 1-500. */
  device_ids?: string[]
  roles?: PortRole[]
  /** Metric keys, 1-10, in order: the first ranks the rows. */
  metrics?: string[]
}
```

(`types` was always sent for a Types scope; the interface simply never listed it.) Append at the end of the file:

```ts

/** Where a metric is defined: Sentinel's catalogue, a device profile, or a custom MIB metric. */
export type MetricSource = 'builtin' | 'profile' | 'custom'

/** One metric a Metrics report can include. Mirrors netreport.MetricChoice. */
export interface MetricChoice {
  key: string
  label: string
  unit: string
  source: MetricSource
}

/**
 * POST /reports/scope-preview: how big a network scope is right now and the
 * metrics it offers. Mirrors netreport.Preview.
 */
export interface ScopePreview {
  ports: number
  devices: number
  /** Over 500 ports or devices: the report keeps the 500 busiest. */
  capped: boolean
  metrics: MetricChoice[]
  /** The metrics a report on this scope starts with, in order. */
  defaults: string[]
}
```

- [ ] **Step 5: Write the helpers**

`frontend/src/utils/reportScope.ts`:

```ts
import { describePeriod } from '@/components/PeriodSelector'
import type { PortRole } from '@/hooks/useDevices'
import {
  MAX_REPORT_METRICS,
  MAX_REPORT_SUBJECTS,
  type MetricChoice,
  type MetricSource,
  type MonitorScopeType,
  type NetworkScopeType,
  type ReportPeriod,
  type ReportScopeData,
  type ReportScopeType,
  type ScopePreview,
} from '@/types/reports'

/** Something picked for a network scope, kept with its name so the builders
 *  can describe the scope without fetching names again. */
export interface ScopeChoice {
  id: string
  name: string
}

/**
 * The network scope being edited. Each tab keeps its own picks and only the
 * active tab's are sent; the Port roles and Sites tabs share the sites.
 */
export interface NetworkScopeDraft {
  scopeType: NetworkScopeType
  /** Named "device · port (alias)". */
  ports: ScopeChoice[]
  sites: ScopeChoice[]
  roles: PortRole[]
  devices: ScopeChoice[]
}

export const EMPTY_NETWORK_SCOPE: NetworkScopeDraft = {
  scopeType: 'ports',
  ports: [],
  sites: [],
  roles: [],
  devices: [],
}

export const NETWORK_SCOPE_TABS: { value: NetworkScopeType; label: string }[] = [
  { value: 'ports', label: 'Ports' },
  { value: 'port_roles', label: 'Port roles' },
  { value: 'devices', label: 'Devices' },
  { value: 'sites', label: 'Sites' },
]

/** Port roles in the order they are offered and named. */
export const REPORT_ROLES: { value: PortRole; label: string }[] = [
  { value: 'wan', label: 'WAN' },
  { value: 'uplink', label: 'Uplink' },
  { value: 'access', label: 'Access' },
]

export const METRIC_SOURCE_LABEL: Record<MetricSource, string> = {
  builtin: 'built-in',
  profile: 'profile',
  custom: 'custom',
}

function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? '' : 's'}`
}

/** scope_data for a monitor report: the one field its scope type uses. */
export function monitorScopeData(scopeType: MonitorScopeType, ids: string[]): ReportScopeData {
  switch (scopeType) {
    case 'monitors':
      return { monitor_ids: ids }
    case 'tags':
      return { tags: ids }
    case 'groups':
      return { group_ids: ids }
    case 'types':
      return { types: ids }
  }
}

/** scope_data for the draft's active tab, without the metrics. */
export function networkScopeData(d: NetworkScopeDraft): ReportScopeData {
  switch (d.scopeType) {
    case 'ports':
      return { port_ids: d.ports.map((p) => p.id) }
    case 'port_roles':
      return {
        site_ids: d.sites.map((s) => s.id),
        roles: REPORT_ROLES.map((r) => r.value).filter((r) => d.roles.includes(r)),
      }
    case 'devices':
      return { device_ids: d.devices.map((x) => x.id) }
    case 'sites':
      return { site_ids: d.sites.map((s) => s.id) }
  }
}

/** Why the draft's active tab cannot be sent yet, or null. */
export function networkScopeError(d: NetworkScopeDraft): string | null {
  switch (d.scopeType) {
    case 'ports':
      if (d.ports.length === 0) return 'Choose at least one port'
      return d.ports.length > MAX_REPORT_SUBJECTS ? `Choose at most ${MAX_REPORT_SUBJECTS} ports` : null
    case 'port_roles':
      if (d.sites.length === 0) return 'Choose at least one site'
      return d.roles.length === 0 ? 'Choose at least one port role' : null
    case 'devices':
      if (d.devices.length === 0) return 'Choose at least one device'
      return d.devices.length > MAX_REPORT_SUBJECTS ? `Choose at most ${MAX_REPORT_SUBJECTS} devices` : null
    case 'sites':
      return d.sites.length === 0 ? 'Choose at least one site' : null
  }
}

/** Names to describe a scope with. An id missing here (not loaded, hidden
 *  from this user, or deleted) is counted, never shown. */
export interface ScopeNames {
  sites: Map<string, string>
  devices: Map<string, string>
}

export function scopeNames(sites: { id: string; name: string }[], devices: { id: string; name: string }[]): ScopeNames {
  return {
    sites: new Map(sites.map((s) => [s.id, s.name])),
    devices: new Map(devices.map((d) => [d.id, d.name])),
  }
}

/** "HQ, Annex", "HQ, Annex, Depot and 2 more", or "5 sites" when no name is known. */
function nameList(ids: string[] | undefined, names: Map<string, string>, noun: string, prefix: string): string {
  const all = ids ?? []
  const known = all.map((id) => names.get(id)).filter((n): n is string => !!n)
  if (known.length === 0) return plural(all.length, noun)
  const shown = known.slice(0, 3)
  const more = all.length - shown.length
  return `${prefix}${shown.join(', ')}${more > 0 ? ` and ${more} more` : ''}`
}

/**
 * A network scope in words: "WAN, Uplink ports at HQ, Annex · 4 metrics".
 * Monitor scopes come back as their scope type, which is how the report pages
 * have always shown them.
 */
export function describeScope(scopeType: ReportScopeType, scope: ReportScopeData | undefined, names: ScopeNames): string {
  const s = scope ?? {}
  let what: string
  switch (scopeType) {
    case 'ports':
      what = plural(s.port_ids?.length ?? 0, 'port')
      break
    case 'port_roles': {
      const roles = REPORT_ROLES.filter((r) => s.roles?.includes(r.value)).map((r) => r.label)
      what = `${roles.join(', ')} ports at ${nameList(s.site_ids, names.sites, 'site', '')}`
      break
    }
    case 'devices':
      what = nameList(s.device_ids, names.devices, 'device', 'Devices: ')
      break
    case 'sites':
      what = nameList(s.site_ids, names.sites, 'site', 'Sites: ')
      break
    default:
      return scopeType
  }
  const n = s.metrics?.length ?? 0
  return n > 0 ? `${what} · ${plural(n, 'metric')}` : what
}

/** The scope picker's size line, from the preview. */
export function previewSizeLine(scopeType: NetworkScopeType, p: ScopePreview): string {
  const size =
    scopeType === 'devices'
      ? plural(p.devices, 'device')
      : scopeType === 'sites'
        ? `${plural(p.ports, 'port')} on ${plural(p.devices, 'device')}`
        : plural(p.ports, 'port')
  return p.capped
    ? `Covers ${size} — the report will include the ${MAX_REPORT_SUBJECTS} busiest`
    : `Covers ${size} right now`
}

/** "core-sw1 · Gi1/0/1 (uplink to annex)": how the report names a port row. */
export function portChoiceName(
  deviceName: string,
  p: { name: string; label: string; number: number; alias: string },
): string {
  const port = p.name || p.label || String(p.number)
  return `${deviceName} · ${port}${p.alias ? ` (${p.alias})` : ''}`
}

const DAY_MS = 86_400_000

function longDate(d: Date, utc: boolean): string {
  return d.toLocaleDateString('en-US', {
    month: 'long',
    day: 'numeric',
    year: 'numeric',
    ...(utc ? { timeZone: 'UTC' } : {}),
  })
}

/**
 * The calendar unit `offset` units before the one containing now, named as the
 * report names its period (models.PeriodLabel): "Week of September 14, 2026",
 * "August 2026", "Q2 2026", "2025". Worked out in the browser's zone; the
 * report's zone can differ, which matters only in a unit's first hours.
 */
function calendarName(unit: NonNullable<ReportPeriod['period_unit']>, offset: number, now: Date): string {
  const y = now.getFullYear()
  const m = now.getMonth()
  switch (unit) {
    case 'week': {
      const sinceMonday = (now.getDay() + 6) % 7
      return `Week of ${longDate(new Date(y, m, now.getDate() - sinceMonday - 7 * offset), false)}`
    }
    case 'month':
      return new Date(y, m - offset, 1).toLocaleDateString('en-US', { month: 'long', year: 'numeric' })
    case 'quarter': {
      const start = new Date(y, Math.floor(m / 3) * 3 - 3 * offset, 1)
      return `Q${Math.floor(start.getMonth() / 3) + 1} ${start.getFullYear()}`
    }
    case 'year':
      return String(y - offset)
  }
}

/**
 * The period a Metrics report compares with, in words: the previous calendar
 * unit for a calendar period, otherwise the same length just before the start
 * (models.Report.PreviousPeriod). Empty while a custom range is incomplete.
 */
export function comparisonLabel(p: ReportPeriod, now: Date = new Date()): string {
  if (p.period_kind === 'calendar') {
    return `Compared with ${calendarName(p.period_unit ?? 'month', (p.period_offset ?? 0) + 1, now)}`
  }
  if (p.period_kind === 'custom') {
    if (!p.period_start || !p.period_end) return ''
    const start = Date.parse(p.period_start)
    // The selector stores whole days (00:00:00 to 23:59:59), so this rounds to
    // the number of days chosen.
    const days = Math.max(1, Math.round((Date.parse(p.period_end) - start) / DAY_MS))
    const first = longDate(new Date(start - days * DAY_MS), true)
    const last = longDate(new Date(start - DAY_MS), true)
    return first === last ? `Compared with ${first}` : `Compared with ${first} to ${last}`
  }
  // Rolling: describePeriod names it "Last 7 days" or "Last 24 hours".
  return `Compared with the ${describePeriod(p).replace(/^Last /, 'previous ')}`
}

/** The Metrics step's list, the choices it was made from, and what a scope
 *  change removed. */
export interface MetricsSelection {
  /** Metric keys in order; the first ranks the rows. */
  metrics: string[]
  /** True once the user changed the list: defaults are no longer re-applied. */
  edited: boolean
  /** Labels of metrics removed because the scope no longer offers them. */
  dropped: string[]
  /** The metrics the scope offers, from the latest preview. */
  choices: MetricChoice[]
}

export const EMPTY_METRICS_SELECTION: MetricsSelection = { metrics: [], edited: false, dropped: [], choices: [] }

/**
 * Applies a new preview to the metrics list (spec section 4). An unedited list
 * becomes the scope's defaults. An edited list keeps what the scope still
 * offers, in order, and names what it lost; if it lost everything, the
 * defaults come back and the list counts as unedited again.
 */
export function reapplyMetrics(cur: MetricsSelection, next: ScopePreview): MetricsSelection {
  const defaults = next.defaults.slice(0, MAX_REPORT_METRICS)
  if (!cur.edited) return { metrics: defaults, edited: false, dropped: [], choices: next.metrics }
  const offered = new Set(next.metrics.map((m) => m.key))
  const kept = cur.metrics.filter((k) => offered.has(k))
  const dropped = cur.metrics
    .filter((k) => !offered.has(k))
    .map((k) => cur.choices.find((c) => c.key === k)?.label ?? k)
  if (kept.length === 0) return { metrics: defaults, edited: false, dropped, choices: next.metrics }
  return { metrics: kept, edited: true, dropped, choices: next.metrics }
}

/** The list with the item at index moved one place up (-1) or down (+1). */
export function moveItem<T>(list: T[], index: number, delta: -1 | 1): T[] {
  const to = index + delta
  if (index < 0 || index >= list.length || to < 0 || to >= list.length) return list
  const out = [...list]
  ;[out[index], out[to]] = [out[to], out[index]]
  return out
}
```

Two notes on `utils/reportPeriods.ts` and `describePeriod`:
- `utils/reportPeriods.ts` is unrelated: it holds the dashboard headline card's windows, and its `ReportPeriod` type is not the report one. Do not import from it here.
- `comparisonLabel` reuses `describePeriod` for rolling windows, so the two never name a window differently. Calendar units need date maths that `PeriodSelector` does not have, and they follow the backend's `PeriodLabel` formats.

- [ ] **Step 6: Run the check to see it pass**

Run the Step 3 command again.

Expected: `reportScope: all checks passed`.

- [ ] **Step 7: Write the two hooks**

`frontend/src/hooks/useScopePreview.ts`:

```ts
import { useEffect, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type { NetworkScopeType, ReportScopeData, ScopePreview } from '@/types/reports'

/** Long enough that ticking a run of checkboxes asks once, not per tick. */
const DEBOUNCE_MS = 400

export interface ScopePreviewState {
  /** The answer for the scope as it is now; null while asking or after an error. */
  data: ScopePreview | null
  /** The server's message: "A chosen port, device or site is not available", and the like. */
  error: string | null
  /** A complete scope is waiting for its answer (debounce or request). */
  loading: boolean
}

/**
 * Sizes a network scope and lists its metrics (POST /reports/scope-preview),
 * about 400 ms after it stops changing. Pass nulls while the scope is
 * incomplete: nothing is asked. Each answer is kept with the request it
 * answers, so a late answer for an earlier scope is never shown as the
 * current one.
 */
export function useScopePreview(scopeType: NetworkScopeType | null, scopeData: ReportScopeData | null): ScopePreviewState {
  // Keyed on the value, not the object, so a caller that rebuilds the scope
  // each render does not ask again.
  const key = scopeType && scopeData ? JSON.stringify({ scope_type: scopeType, scope_data: scopeData }) : ''
  const [answer, setAnswer] = useState<{ key: string; data: ScopePreview | null; error: string | null }>({
    key: '',
    data: null,
    error: null,
  })

  useEffect(() => {
    if (!key) return
    let cancelled = false
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      api
        .post<ApiResponse<ScopePreview>>('/reports/scope-preview', JSON.parse(key), { signal: controller.signal })
        .then(({ data }) => {
          if (!cancelled) setAnswer({ key, data: data.data, error: null })
        })
        .catch((err: ApiError) => {
          if (!cancelled) setAnswer({ key, data: null, error: err.message || 'Could not check this scope' })
        })
    }, DEBOUNCE_MS)
    return () => {
      cancelled = true
      controller.abort()
      window.clearTimeout(timer)
    }
  }, [key])

  const current = key !== '' && answer.key === key
  return {
    data: current ? answer.data : null,
    error: current ? answer.error : null,
    loading: key !== '' && !current,
  }
}
```

How the hook guards async results:
- The guard is the phase 4 pattern: a `cancelled` flag plus an `AbortController` in the effect cleanup.
- Each answer is also stored with the key it answers. Without the key, an old preview could show during the 400 ms debounce after the scope changed.

`frontend/src/hooks/useMetricsReportDraft.ts`:

```ts
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useScopePreview } from '@/hooks/useScopePreview'
import { MAX_REPORT_METRICS, type ReportScopeData } from '@/types/reports'
import {
  EMPTY_METRICS_SELECTION,
  EMPTY_NETWORK_SCOPE,
  networkScopeData,
  networkScopeError,
  reapplyMetrics,
  scopeNames,
  type MetricsSelection,
  type NetworkScopeDraft,
} from '@/utils/reportScope'

/**
 * The Metrics part of a report being built: the network scope, its live
 * preview, and the ordered metrics. The wizard and the quick dialog both use
 * it, so the two cannot drift. enabled is false while another report type is
 * chosen: nothing is fetched, and the picks are kept for coming back.
 */
export function useMetricsReportDraft(enabled: boolean) {
  const [scope, setScope] = useState<NetworkScopeDraft>(EMPTY_NETWORK_SCOPE)
  const [selection, setSelection] = useState<MetricsSelection>(EMPTY_METRICS_SELECTION)

  const scopeError = networkScopeError(scope)
  const scopeData = useMemo(() => networkScopeData(scope), [scope])
  const ready = enabled && scopeError === null
  const preview = useScopePreview(ready ? scope.scopeType : null, ready ? scopeData : null)

  // Every answer for a changed scope re-applies the defaults or, once the list
  // was edited, drops the metrics the scope no longer offers.
  const answer = preview.data
  useEffect(() => {
    if (answer) setSelection((cur) => reapplyMetrics(cur, answer))
  }, [answer])

  const setMetrics = useCallback((metrics: string[]) => {
    setSelection((cur) => ({ ...cur, metrics: metrics.slice(0, MAX_REPORT_METRICS), edited: true }))
  }, [])

  const reset = useCallback(() => {
    setScope(EMPTY_NETWORK_SCOPE)
    setSelection(EMPTY_METRICS_SELECTION)
  }, [])

  /** scope_data for the create call: the active tab's ids plus the metrics. */
  const payloadData = useMemo<ReportScopeData>(
    () => ({ ...scopeData, metrics: selection.metrics }),
    [scopeData, selection.metrics],
  )
  const names = useMemo(() => scopeNames(scope.sites, scope.devices), [scope.sites, scope.devices])

  return {
    scope,
    setScope,
    scopeError,
    scopeData,
    preview,
    /** The scope is complete, checked by the server, and its metrics are known. */
    scopeReady: scopeError === null && preview.data !== null,
    selection,
    setMetrics,
    metricsError: selection.metrics.length === 0 ? 'Choose at least one metric' : null,
    payloadData,
    names,
    reset,
  }
}
```

- [ ] **Step 8: Run the gate**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output. Nothing renders the new code yet, so there are no manual checks in this task. Delete the throwaway check with `rm -r "$CHECK_DIR"`.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/types/reports.ts frontend/src/utils/reportScope.ts frontend/src/hooks/useScopePreview.ts frontend/src/hooks/useMetricsReportDraft.ts
git commit -m "feat(reports): metrics report types, scope preview hook and scope helpers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 17: The wizard order Type → Scope → Metrics → Period (both builders)

Both builders are reordered so the report type comes first. The wizard's steps become Type → Scope → (Metrics) → Period → Details:
- Uptime and Incident skip the Metrics step and keep their monitor scope tabs and checklists exactly as they are, including the name field above the scope.
- The Type step shows three cards, each with its blurb.
- A Metrics report sends `report_type: 'metrics'`, the network `scope_type`, and `scope_data` holding the ids, roles and ordered metrics.

The quick dialog (`GenerateReportModal`) follows the same order on one screen. Given a fixed monitor scope (a monitor's page, its only caller today), it offers only Uptime and Incident.

In this task the Metrics scope and Metrics steps render a one-line placeholder. Tasks 18 and 19 replace those exact lines, and the gate stays green in between.

Both files are given whole: almost every block moves, and anchored edits would be longer than the files.

**Files:**
- Modify (rewrite): `frontend/src/components/ReportBuilderWizard.tsx`
- Modify (rewrite): `frontend/src/components/GenerateReportModal.tsx`
- Test: the frontend gate; manual checks below

**Interfaces:**
- Consumes (Task 16):
  - `useMetricsReportDraft(enabled)` → `{ scope, setScope, scopeError, scopeData, preview, scopeReady, selection, setMetrics, metricsError, payloadData, names, reset }`.
  - `describeScope` and `monitorScopeData` from `@/utils/reportScope`.
  - `REPORT_TYPES`, `REPORT_TYPE_LABEL`, `REPORT_TYPE_BLURB`, `MonitorScopeType`, `ReportScopeData`, `ReportScopeType` from `@/types/reports`.
  - `waitForReportJob(jobID, { timeoutMs?, onProgress? })` (`@/hooks/useReportBuilder`, existing).
- Produces:
  - The wizard renders `draft` (the hook's result) on the Scope and Metrics steps. Each step has one placeholder line, which Task 18 and Task 19 replace:
    - Scope step: `<p className="text-sm text-slate-400">Ports, port roles, devices and sites are chosen here.</p>`
    - Metrics step: `<p className="text-sm text-slate-400">{draft.selection.metrics.join(', ') || 'No metrics yet.'}</p>`
  - The dialog has the same two lines, indented four more spaces.
  - `FixedScope.scope_type` narrows to `MonitorScopeType`. The only caller, `MonitorDetail`, passes `'monitors'`.

- [ ] **Step 1: Rewrite the wizard**

Replace the whole of `frontend/src/components/ReportBuilderWizard.tsx` with:

```tsx
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, ChevronLeft, ChevronRight, Loader2 } from 'lucide-react'
import { useMonitors } from '@/hooks/useMonitors'
import { useMonitorGroups } from '@/hooks/useMonitorGroups'
import { useMonitorTags, useSavedReports, waitForReportJob } from '@/hooks/useReportBuilder'
import { useMetricsReportDraft } from '@/hooks/useMetricsReportDraft'
import PeriodSelector, { DEFAULT_PERIOD, describePeriod } from '@/components/PeriodSelector'
import { REPORT_TYPE_BLURB, REPORT_TYPE_LABEL, REPORT_TYPES } from '@/types/reports'
import type { MonitorScopeType, ReportPeriod, ReportType } from '@/types/reports'
import { describeScope, monitorScopeData } from '@/utils/reportScope'

type StepId = 'type' | 'scope' | 'metrics' | 'period' | 'details'

const STEP_TITLE: Record<StepId, string> = {
  type: 'Report Type',
  scope: 'Scope',
  metrics: 'Metrics',
  period: 'Period',
  details: 'Details',
}

// Uptime and Incident reports have no metrics to choose, so they skip that step.
const MONITOR_STEPS: StepId[] = ['type', 'scope', 'period', 'details']
const METRICS_STEPS: StepId[] = ['type', 'scope', 'metrics', 'period', 'details']

// A Metrics report can take up to the five-minute job limit to render.
const METRICS_WAIT_MS = 330_000

// The check types a report may be scoped to, in the order the app shows them.
// Webhook is absent: it receives rather than checks, so it has no incidents.
const REPORTABLE_TYPES = ['http', 'dns', 'ping', 'tcp']

interface ReportBuilderWizardProps {
  onError?: (message: string) => void
}

/**
 * ReportBuilderWizard walks through defining a saved report: its type, what it
 * covers, the metrics (Metrics reports only), the period, and an optional
 * title and description. Generating it renders a PDF immediately.
 */
export default function ReportBuilderWizard({ onError }: ReportBuilderWizardProps) {
  const navigate = useNavigate()
  const { createReport } = useSavedReports()
  const { monitors, loading: monitorsLoading } = useMonitors()
  const { groups, loading: groupsLoading } = useMonitorGroups()
  const { tags, listTags, loading: tagsLoading } = useMonitorTags()

  const [stepIndex, setStepIndex] = useState(0)
  const [generating, setGenerating] = useState(false)
  // Rendering is queued, so the button reflects the job's actual state rather
  // than a generic spinner.
  const [progress, setProgress] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [reportType, setReportType] = useState<ReportType>('uptime')
  const [scopeType, setScopeType] = useState<MonitorScopeType>('monitors')
  const [selection, setSelection] = useState<string[]>([])
  const [period, setPeriod] = useState<ReportPeriod>(DEFAULT_PERIOD)
  const [customTitle, setCustomTitle] = useState('')
  const [customDescription, setCustomDescription] = useState('')

  const isMetrics = reportType === 'metrics'
  const draft = useMetricsReportDraft(isMetrics)
  // The type is only chosen on the first step, which both lists share, so
  // switching it never strands the wizard on a step the other list lacks.
  const steps = isMetrics ? METRICS_STEPS : MONITOR_STEPS
  const step = steps[stepIndex]

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

  const changeScopeType = (type: MonitorScopeType) => {
    setScopeType(type)
    setSelection([])
  }

  const toggle = (id: string) =>
    setSelection((cur) => (cur.includes(id) ? cur.filter((s) => s !== id) : [...cur, id]))

  // Validation lives here so Next is disabled rather than failing on click.
  const stepError = useMemo(() => {
    if (step === 'scope') {
      if (!name.trim()) return 'Give the report a name'
      if (isMetrics) return draft.scopeError
      if (selection.length === 0) {
        return `Select at least one ${scopeType === 'types' ? 'monitor type' : scopeType.slice(0, -1)}`
      }
    }
    if (step === 'metrics') return draft.metricsError
    if (
      step === 'period' &&
      period.period_kind === 'custom' &&
      (!period.period_start || !period.period_end)
    ) {
      return 'Choose both a start and an end date'
    }
    return null
  }, [step, name, isMetrics, draft.scopeError, draft.metricsError, selection, scopeType, period])

  // A Metrics scope also waits for its preview, which checks every pick is
  // still available and lists the metrics the next step offers. The picker
  // shows its progress and any error itself.
  const waiting = step === 'scope' && isMetrics && !draft.scopeReady

  const generate = async () => {
    setGenerating(true)
    try {
      const result = await createReport({
        name: name.trim(),
        report_type: reportType,
        scope_type: isMetrics ? draft.scope.scopeType : scopeType,
        scope_data: isMetrics ? draft.payloadData : monitorScopeData(scopeType, selection),
        ...period,
        custom_title: customTitle.trim() || undefined,
        custom_description: customDescription.trim() || undefined,
      })

      // The report exists now; its first PDF is still rendering. Wait for the
      // job so the detail page does not open on an empty generation list.
      setProgress('Queued…')
      try {
        await waitForReportJob(result.job_id, {
          timeoutMs: isMetrics ? METRICS_WAIT_MS : undefined,
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
        {steps.map((id, idx) => (
          <div key={id} className="flex flex-1 items-center">
            <div
              className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-sm font-semibold"
              style={{
                background: stepIndex >= idx ? 'var(--vs-ecg)' : 'var(--vs-panel-2)',
                color: stepIndex >= idx ? 'var(--vs-bg)' : 'var(--vs-text-dim)',
              }}
            >
              {stepIndex > idx ? <Check className="h-4 w-4" /> : idx + 1}
            </div>
            <span
              className="ml-2 hidden text-sm sm:inline"
              style={{ color: stepIndex >= idx ? 'var(--vs-text)' : 'var(--vs-text-dim)' }}
            >
              {STEP_TITLE[id]}
            </span>
            {idx < steps.length - 1 && (
              <div
                className="mx-3 h-px flex-1"
                style={{ background: stepIndex > idx ? 'var(--vs-ecg)' : 'var(--vs-line)' }}
              />
            )}
          </div>
        ))}
      </div>

      {step === 'type' && (
        <div className="rd-card space-y-3 p-5">
          <span className="vs-eyebrow block">Report type</span>
          {REPORT_TYPES.map((t) => (
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
                {REPORT_TYPE_BLURB[t]}
              </p>
            </button>
          ))}
        </div>
      )}

      {step === 'scope' && (
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
            {isMetrics ? (
              <p className="text-sm text-slate-400">Ports, port roles, devices and sites are chosen here.</p>
            ) : (
              <>
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
              </>
            )}
          </div>
        </div>
      )}

      {step === 'metrics' && (
        <div className="rd-card space-y-3 p-5">
          <span className="vs-eyebrow block">Metrics</span>
          <p className="text-sm text-slate-400">{draft.selection.metrics.join(', ') || 'No metrics yet.'}</p>
        </div>
      )}

      {step === 'period' && (
        <div className="rd-card space-y-5 p-5">
          <span className="vs-eyebrow block">Reporting period</span>
          {/* The same selector the quick dialog uses: the two paths must not
              offer different periods, or a report built one way cannot be
              reproduced the other. */}
          <PeriodSelector value={period} onChange={setPeriod} />
        </div>
      )}

      {step === 'details' && (
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
              {isMetrics
                ? describeScope(draft.scope.scopeType, draft.payloadData, draft.names)
                : `${selection.length} ${scopeType}`}{' '}
              · {describePeriod(period)} · {REPORT_TYPE_LABEL[reportType]}
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
          onClick={() => setStepIndex((i) => Math.max(0, i - 1))}
          disabled={stepIndex === 0}
        >
          <ChevronLeft className="h-4 w-4" /> Back
        </button>

        {stepIndex < steps.length - 1 ? (
          <button
            type="button"
            className="rd-btn rd-btn-primary"
            onClick={() => setStepIndex((i) => i + 1)}
            disabled={stepError !== null || waiting}
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

What changed, for review:
- Steps are ids, and the list depends on the type. The type is chosen only on step 1, which both lists share, so switching it never strands the index.
- The monitor tabs and checklist are the old JSX unchanged, now in the `isMetrics ? … : <>…</>` else branch.
- On a Metrics scope, Next also waits for `draft.scopeReady`. The preview checks that every pick is still available and lists the metrics the next step needs.
- A Metrics render may take up to the 5-minute job limit, so the wait is 330 s instead of the default 120 s.
- The two inline `scopeData` ternaries became `monitorScopeData`, which gives the same mapping.

- [ ] **Step 2: Rewrite the quick dialog**

Replace the whole of `frontend/src/components/GenerateReportModal.tsx` with:

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
import { useMetricsReportDraft } from '@/hooks/useMetricsReportDraft'
import PeriodSelector, { DEFAULT_PERIOD, describePeriod } from '@/components/PeriodSelector'
import { REPORT_TYPE_BLURB, REPORT_TYPE_LABEL, REPORT_TYPES } from '@/types/reports'
import type { MonitorScopeType, ReportPeriod, ReportScopeData, ReportScopeType, ReportType } from '@/types/reports'
import { describeScope, monitorScopeData } from '@/utils/reportScope'

/** What a report covers, when the caller already knows — a monitor's own page. */
export interface FixedScope {
  scope_type: MonitorScopeType
  ids: string[]
  /** How to describe it in the dialog, e.g. the monitor's name. */
  label: string
}

// Webhook is absent: it receives rather than checks, so it has no incidents.
const REPORTABLE_TYPES = ['http', 'dns', 'ping', 'tcp']

// A Metrics report can take up to the five-minute job limit to render.
const METRICS_WAIT_MS = 330_000

const SCOPE_TABS: { value: MonitorScopeType; label: string }[] = [
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
 * Generates a report in one dialog, in the wizard's order: type, what to
 * cover, metrics (Metrics reports only), period.
 *
 * Most of the time the question is "last month, these services, that report",
 * and that fits in one screen. Given a fixedScope — a monitor's own page — the
 * scope picker is dropped entirely, since it is already answered, and so is
 * the Metrics type, which cannot cover a monitor.
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
  const [scopeType, setScopeType] = useState<MonitorScopeType>('monitors')
  const [selection, setSelection] = useState<string[]>([])
  const [phase, setPhase] = useState<Phase>({ kind: 'form' })

  const typeChoices = fixedScope ? REPORT_TYPES.filter((t) => t !== 'metrics') : REPORT_TYPES
  const isMetrics = reportType === 'metrics'
  const draft = useMetricsReportDraft(isOpen && isMetrics)
  const resetDraft = draft.reset

  useEffect(() => {
    if (!isOpen || fixedScope) return
    void listTags()
  }, [isOpen, fixedScope, listTags])

  // Reopening should start a fresh form rather than show the previous result.
  useEffect(() => {
    if (isOpen) {
      setPhase({ kind: 'form' })
      setSelection([])
      resetDraft()
    }
  }, [isOpen, resetDraft])

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

  // What the report covers, in words and as the API's scope fields; null
  // until something is chosen.
  const target: { label: string; scope_type: ReportScopeType; scope_data: ReportScopeData } | null = isMetrics
    ? draft.scopeError === null
      ? {
          label: describeScope(draft.scope.scopeType, draft.scopeData, draft.names),
          scope_type: draft.scope.scopeType,
          scope_data: draft.payloadData,
        }
      : null
    : effectiveScope
      ? {
          label: effectiveScope.label,
          scope_type: effectiveScope.scope_type,
          scope_data: monitorScopeData(effectiveScope.scope_type, effectiveScope.ids),
        }
      : null

  const periodValid =
    period.period_kind !== 'custom' || (!!period.period_start && !!period.period_end)
  // Why Generate is not available yet, or null when it is.
  const blocker = !target
    ? isMetrics
      ? draft.scopeError
      : 'Choose at least one thing to report on.'
    : !periodValid
      ? 'Choose both dates for a custom period.'
      : isMetrics && draft.preview.error
        ? draft.preview.error
        : isMetrics && !draft.scopeReady
          ? 'Checking the scope…'
          : isMetrics && draft.metricsError
            ? draft.metricsError
            : null
  const canGenerate = blocker === null

  if (!isOpen) return null

  const generate = async () => {
    if (!target || !canGenerate) return
    setPhase({ kind: 'working', message: 'Creating the report…' })
    let reportID: string | undefined
    try {
      const label = REPORT_TYPE_LABEL[reportType]
      const result = await createReport({
        name: `${target.label} — ${label}`,
        report_type: reportType,
        scope_type: target.scope_type,
        scope_data: target.scope_data,
        ...period,
        custom_title: `${target.label}: ${label}`,
        custom_description: describePeriod(period),
      })
      reportID = result.id

      setPhase({ kind: 'working', message: 'Queued…' })
      const job = await waitForReportJob(result.job_id, {
        timeoutMs: isMetrics ? METRICS_WAIT_MS : undefined,
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
    await downloadReportPDF(url, `${target?.label ?? 'report'}-${stamp}.pdf`)
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
            <fieldset>
              <legend className="mb-2 text-sm font-medium text-white">Report type</legend>
              <div className="space-y-2">
                {typeChoices.map((t) => (
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
                      <span className="block text-xs text-slate-400">{REPORT_TYPE_BLURB[t]}</span>
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>

            {!fixedScope && (
              <fieldset>
                <legend className="mb-2 text-sm font-medium text-white">What to cover</legend>
                {isMetrics ? (
                  <p className="text-sm text-slate-400">Ports, port roles, devices and sites are chosen here.</p>
                ) : (
                  <>
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
                  </>
                )}
              </fieldset>
            )}

            {isMetrics && (
              <fieldset>
                <legend className="mb-2 text-sm font-medium text-white">Metrics</legend>
                <p className="text-sm text-slate-400">{draft.selection.metrics.join(', ') || 'No metrics yet.'}</p>
              </fieldset>
            )}

            <fieldset>
              <legend className="mb-2 text-sm font-medium text-white">Period</legend>
              <PeriodSelector value={period} onChange={setPeriod} />
            </fieldset>

            <p className="rounded-lg border border-white/10 bg-slate-800/40 p-3 text-xs text-slate-400">
              {canGenerate && target ? (
                <>
                  <span className="text-slate-200">{REPORT_TYPE_LABEL[reportType]}</span> for{' '}
                  <span className="text-slate-200">{target.label}</span>,{' '}
                  {describePeriod(period).toLowerCase()}. Saved under Reports, where it can be
                  shared or scheduled.
                </>
              ) : (
                blocker
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

What changed, for review:
- Sections run type → what to cover → metrics → period.
- `target` replaces the monitor-only `effectiveScope` at the create call.
- `blocker` gives one reason why Generate is unavailable. The old two messages are unchanged for monitor reports.
- Reopening resets the Metrics draft. `resetDraft` is a stable `useCallback`, so it is a valid effect dependency.

- [ ] **Step 3: Run the gate**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output.

- [ ] **Step 4: Manual checks (sandbox, with the backend of Tasks 1-15)**

1. Reports → New report: the steps read 1 Report Type, 2 Scope, 3 Period, 4 Details, and Uptime is selected.
2. Choose Incident, then Next: the name field and the Monitors / Types / Groups / Tags tabs look and behave as before. Next is disabled with "Give the report a name", then "Select at least one monitor".
3. Generate an Uptime report end to end: it opens on its detail page with a PDF, exactly as before.
4. Back to step 1 and choose Metrics Report: the indicator shows five steps (Report Type, Scope, Metrics, Period, Details), and the Scope step shows the placeholder line. Next stays disabled ("Choose at least one port") until Task 18.
5. A monitor's page → Generate Report: the dialog shows Report type first, with Uptime and Incident only (no Metrics), then Period. The summary line reads as before, and generating works.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/ReportBuilderWizard.tsx frontend/src/components/GenerateReportModal.tsx
git commit -m "feat(reports): builders ask Type, Scope, Metrics, Period in that order

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 18: The network scope picker (four tabs, port picker, size line)

The Metrics scope step gets four tabs:
- Ports: pick a device, tick its ports, repeat across devices. The chosen list reads "device · port (alias)".
- Port roles: tick sites and WAN / Uplink / Access.
- Devices: tick devices.
- Sites: tick sites.

The lists come from `useSites` and `useDevices`, which the API already filters by site access.

The dashboard pickers in `components/dashboards/pickers` do not fit:
- `DevicePicker` caps a checklist at 50 (the widget limit) and returns only ids.
- `SitePicker` is a single select.

So this task adds one small `Checklist` (internal to `MetricsScopePicker`) and reuses the shared `inputCls` styling and `PORT_ROLE_HINT` wording.

Ports load through a new guarded hook. The existing `useDevicePorts` lets a late answer for the previous device overwrite the current one, which here would put one device's ports under another's name.

Under the tabs, the size line comes from the draft's preview:
- "Sizing the scope…"
- "Covers 214 ports right now"
- "Covers 640 ports — the report will include the 500 busiest" (amber)
- the server's error in red, for example "A chosen port, device or site is not available"

**Files:**
- Modify: `frontend/src/hooks/usePorts.ts` (add `usePortChoices` after `useDevicePorts`)
- Create: `frontend/src/components/reports/PortPicker.tsx`
- Create: `frontend/src/components/reports/MetricsScopePicker.tsx`
- Modify: `frontend/src/components/ReportBuilderWizard.tsx`, `frontend/src/components/GenerateReportModal.tsx` (each: one import, one placeholder line)
- Test: the frontend gate; manual checks below

**Interfaces:**
- Consumes (Task 16):
  - `NetworkScopeDraft`, `ScopeChoice`, `NETWORK_SCOPE_TABS`, `REPORT_ROLES`, `previewSizeLine`, `portChoiceName` (`@/utils/reportScope`).
  - `ScopePreviewState` (`@/hooks/useScopePreview`); `MAX_REPORT_SUBJECTS`, `NetworkScopeType` (`@/types/reports`).
  - The draft's `scope`, `setScope` and `preview`.
- Consumes (existing):
  - `useSites()` → `{ sites, loading, error }` and `useDevices()` → `{ devices, loading }`. `Device` has `id`, `name` and `site_name?`.
  - `PortView` (`@/hooks/usePorts`), with `id` (the device_interfaces id), `name`, `alias`, `label`, `physical` and `role`.
  - `GET /devices/:id/ports` → `{ ports: PortView[], … }`. `PORT_ROLE_HINT` (`@/utils/network`), `inputCls` (`@/utils/dashboards`), `colors` (`@/utils/colors`).
- Produces:
  - `usePortChoices(deviceId: string | undefined): { ports: PortView[] | null; error: string | null }`.
  - `PortPicker({ devices: Device[]; devicesLoading: boolean; value: ScopeChoice[]; onChange(ports: ScopeChoice[]) })`.
  - `MetricsScopePicker({ value: NetworkScopeDraft; onChange(next: NetworkScopeDraft); preview: ScopePreviewState })`.

- [ ] **Step 1: Put the picker where it will be used, and see it fail**

In `frontend/src/components/ReportBuilderWizard.tsx`, after the line `import PeriodSelector, { DEFAULT_PERIOD, describePeriod } from '@/components/PeriodSelector'`, add:

```tsx
import MetricsScopePicker from '@/components/reports/MetricsScopePicker'
```

and replace the placeholder line

```tsx
              <p className="text-sm text-slate-400">Ports, port roles, devices and sites are chosen here.</p>
```

with

```tsx
              <MetricsScopePicker value={draft.scope} onChange={draft.setScope} preview={draft.preview} />
```

In `frontend/src/components/GenerateReportModal.tsx`, add the same import after its `PeriodSelector` import. Then replace

```tsx
                  <p className="text-sm text-slate-400">Ports, port roles, devices and sites are chosen here.</p>
```

with

```tsx
                  <MetricsScopePicker value={draft.scope} onChange={draft.setScope} preview={draft.preview} />
```

Run the typecheck from the repo root (node_modules is in place from the last gate run):

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine npx tsc --noEmit
```

Expected: FAIL — `error TS2307: Cannot find module '@/components/reports/MetricsScopePicker'` in both files.

- [ ] **Step 2: Add the guarded port fetch**

In `frontend/src/hooks/usePorts.ts`, directly after the `useDevicePorts` function, add:

```ts
/**
 * A device's present ports for a picker, fetched once per device. An answer
 * for a device no longer chosen is dropped, so a slow response can never list
 * one device's ports under another's name. ports is null while loading, with
 * no device, or after an error.
 */
export function usePortChoices(deviceId: string | undefined) {
  const [answer, setAnswer] = useState<{ deviceId: string; ports: PortView[]; error: string | null } | null>(null)

  useEffect(() => {
    if (!deviceId) return
    let cancelled = false
    api
      .get<ApiResponse<DevicePorts>>(`/devices/${deviceId}/ports`)
      .then(({ data }) => {
        if (!cancelled) setAnswer({ deviceId, ports: data.data?.ports ?? [], error: null })
      })
      .catch((err: ApiError) => {
        if (!cancelled) setAnswer({ deviceId, ports: [], error: err.message || 'Could not load the ports' })
      })
    return () => {
      cancelled = true
    }
  }, [deviceId])

  const current = answer && answer.deviceId === deviceId ? answer : null
  return { ports: current && !current.error ? current.ports : null, error: current?.error ?? null }
}
```

(`useEffect`, `useState`, `api`, `ApiError`, `ApiResponse`, `DevicePorts` and `PortView` are already in scope in this file.)

- [ ] **Step 3: Write the port picker**

`frontend/src/components/reports/PortPicker.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { X } from 'lucide-react'
import type { Device, PortRole } from '@/hooks/useDevices'
import { usePortChoices, type PortView } from '@/hooks/usePorts'
import { MAX_REPORT_SUBJECTS } from '@/types/reports'
import { colors } from '@/utils/colors'
import { inputCls } from '@/utils/dashboards'
import { portChoiceName, type ScopeChoice } from '@/utils/reportScope'

interface Props {
  /** Devices the user can see, from the scope picker's useDevices. */
  devices: Device[]
  devicesLoading: boolean
  value: ScopeChoice[]
  onChange: (ports: ScopeChoice[]) => void
}

// Access is the default role, so only the other two are worth a tag.
const ROLE_TAG: Partial<Record<PortRole, string>> = { wan: 'WAN', uplink: 'Uplink' }

/**
 * Ports for a Metrics report: pick a device, tick its ports, then pick another
 * device and tick more. Everything ticked collects in one list, each port
 * named "device · port (alias)" the way the report names its rows.
 */
export default function PortPicker({ devices, devicesLoading, value, onChange }: Props) {
  const [deviceId, setDeviceId] = useState('')
  const [filter, setFilter] = useState('')
  const { ports, error } = usePortChoices(deviceId || undefined)
  const device = devices.find((d) => d.id === deviceId)
  const chosen = useMemo(() => new Set(value.map((p) => p.id)), [value])
  const full = value.length >= MAX_REPORT_SUBJECTS

  // Physical ports first, then VLANs, tunnels and the like; each group stays
  // in ifIndex order, which is how the API lists them.
  const shown = useMemo(() => {
    if (!ports) return []
    const q = filter.trim().toLowerCase()
    const matches = (p: PortView) => !q || [p.name, p.alias, p.label].some((s) => s.toLowerCase().includes(q))
    return [...ports.filter((p) => p.physical), ...ports.filter((p) => !p.physical)].filter(matches)
  }, [ports, filter])

  const choiceOf = (p: PortView): ScopeChoice => ({ id: p.id, name: portChoiceName(device?.name ?? 'Device', p) })
  const toggle = (p: PortView) => {
    if (chosen.has(p.id)) onChange(value.filter((v) => v.id !== p.id))
    else if (!full) onChange([...value, choiceOf(p)])
  }
  const tickAllShown = () => {
    const room = MAX_REPORT_SUBJECTS - value.length
    onChange([...value, ...shown.filter((p) => !chosen.has(p.id)).slice(0, room).map(choiceOf)])
  }

  return (
    <div className="space-y-3">
      <select
        className={inputCls}
        aria-label="Device"
        value={deviceId}
        onChange={(e) => {
          setDeviceId(e.target.value)
          setFilter('')
        }}
      >
        <option value="">{devicesLoading ? 'Loading…' : 'Choose a device to list its ports…'}</option>
        {devices.map((d) => (
          <option key={d.id} value={d.id}>
            {d.name}
            {d.site_name ? ` (${d.site_name})` : ''}
          </option>
        ))}
      </select>

      {deviceId && (
        <div className="space-y-2">
          <div className="flex gap-2">
            <input
              className={inputCls}
              placeholder="Filter by name or alias…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
            <button
              type="button"
              className="btn-secondary shrink-0"
              onClick={tickAllShown}
              disabled={full || shown.every((p) => chosen.has(p.id))}
            >
              Tick all shown
            </button>
          </div>
          <div className="max-h-56 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
            {error && <p className={`text-xs ${colors.error.text}`}>{error}</p>}
            {!error && ports === null && <p className="text-xs text-slate-500">Loading…</p>}
            {ports !== null && shown.length === 0 && (
              <p className="text-xs text-slate-500">
                {ports.length === 0 ? 'This device has no ports yet.' : 'No port matches.'}
              </p>
            )}
            {shown.map((p) => (
              <label
                key={p.id}
                className="flex cursor-pointer items-center gap-2 rounded px-1 py-1 text-sm text-slate-300 hover:bg-white/5"
              >
                <input
                  type="checkbox"
                  checked={chosen.has(p.id)}
                  disabled={full && !chosen.has(p.id)}
                  onChange={() => toggle(p)}
                />
                <span className="truncate">{p.name || `Port ${p.label}`}</span>
                {p.alias && <span className="truncate text-xs text-slate-500">{p.alias}</span>}
                {ROLE_TAG[p.role] && (
                  <span className="ml-auto shrink-0 rounded bg-white/5 px-1.5 text-xs text-slate-400">
                    {ROLE_TAG[p.role]}
                  </span>
                )}
              </label>
            ))}
          </div>
        </div>
      )}

      <div className="space-y-1">
        <p className="text-sm text-slate-300">Chosen ports ({value.length})</p>
        {value.length === 0 ? (
          <p className="text-xs text-slate-500">
            Pick a device and tick its ports; pick another device to add more.
          </p>
        ) : (
          <ul className="max-h-40 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
            {value.map((p) => (
              <li key={p.id} className="flex items-center gap-2 text-sm text-slate-300">
                <span className="min-w-0 flex-1 truncate">{p.name}</span>
                <button
                  type="button"
                  className="text-slate-500 hover:text-white"
                  aria-label={`Remove ${p.name}`}
                  onClick={() => onChange(value.filter((v) => v.id !== p.id))}
                >
                  <X className="h-4 w-4" />
                </button>
              </li>
            ))}
          </ul>
        )}
        {full && <p className={`text-xs ${colors.warning.text}`}>A report covers at most {MAX_REPORT_SUBJECTS} ports.</p>}
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Write the scope picker**

`frontend/src/components/reports/MetricsScopePicker.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useDevices, type PortRole } from '@/hooks/useDevices'
import type { ScopePreviewState } from '@/hooks/useScopePreview'
import { useSites } from '@/hooks/useSites'
import PortPicker from '@/components/reports/PortPicker'
import { MAX_REPORT_SUBJECTS, type NetworkScopeType } from '@/types/reports'
import { colors } from '@/utils/colors'
import { inputCls } from '@/utils/dashboards'
import { PORT_ROLE_HINT } from '@/utils/network'
import {
  NETWORK_SCOPE_TABS,
  REPORT_ROLES,
  previewSizeLine,
  type NetworkScopeDraft,
  type ScopeChoice,
} from '@/utils/reportScope'

interface Props {
  value: NetworkScopeDraft
  onChange: (next: NetworkScopeDraft) => void
  /** The live preview of value, from useMetricsReportDraft. */
  preview: ScopePreviewState
}

interface Item {
  id: string
  name: string
  /** Shown beside the name, e.g. a device's site. */
  hint?: string
}

/** Sites or devices to tick, with a filter once the list is long. The dashboard
 *  pickers cap at 50 (the widget limit) and do not hand back names, so the
 *  report tabs use this instead. */
function Checklist({
  items,
  value,
  onChange,
  max,
  empty,
  loading,
}: {
  items: Item[]
  value: ScopeChoice[]
  onChange: (next: ScopeChoice[]) => void
  max?: number
  empty: string
  loading: boolean
}) {
  const [filter, setFilter] = useState('')
  const chosen = useMemo(() => new Set(value.map((v) => v.id)), [value])
  const q = filter.trim().toLowerCase()
  const shown = q
    ? items.filter((i) => i.name.toLowerCase().includes(q) || (i.hint ?? '').toLowerCase().includes(q))
    : items
  const full = max !== undefined && value.length >= max
  const toggle = (i: Item) => {
    if (chosen.has(i.id)) onChange(value.filter((v) => v.id !== i.id))
    else if (!full) onChange([...value, { id: i.id, name: i.name }])
  }
  return (
    <div className="space-y-2">
      {items.length > 8 && (
        <input className={inputCls} placeholder="Filter…" value={filter} onChange={(e) => setFilter(e.target.value)} />
      )}
      <div className="max-h-56 space-y-1 overflow-auto rounded-md border border-white/10 p-2">
        {items.length === 0 && <p className="text-xs text-slate-500">{loading ? 'Loading…' : empty}</p>}
        {shown.map((i) => (
          <label
            key={i.id}
            className="flex cursor-pointer items-center gap-2 rounded px-1 py-1 text-sm text-slate-300 hover:bg-white/5"
          >
            <input
              type="checkbox"
              checked={chosen.has(i.id)}
              disabled={full && !chosen.has(i.id)}
              onChange={() => toggle(i)}
            />
            <span className="truncate">{i.name}</span>
            {i.hint && <span className="truncate text-xs text-slate-500">{i.hint}</span>}
          </label>
        ))}
      </div>
      <p className="text-xs text-slate-500">
        {value.length} selected{full && max !== undefined ? ` (at most ${max})` : ''}
      </p>
    </div>
  )
}

/** How big the scope is right now, or why it cannot be used. */
function SizeLine({ scopeType, preview }: { scopeType: NetworkScopeType; preview: ScopePreviewState }) {
  if (preview.error) {
    return (
      <p role="alert" className={`text-sm ${colors.error.text}`}>
        {preview.error}
      </p>
    )
  }
  if (preview.loading) {
    return (
      <p className="flex items-center gap-2 text-sm text-slate-400">
        <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden /> Sizing the scope…
      </p>
    )
  }
  if (!preview.data) return null
  const nothing = (scopeType === 'devices' ? preview.data.devices : preview.data.ports) === 0
  return (
    <p className={`text-sm ${preview.data.capped || nothing ? colors.warning.text : 'text-slate-300'}`}>
      {previewSizeLine(scopeType, preview.data)}
    </p>
  )
}

/**
 * What a Metrics report covers, in four tabs: chosen ports, port roles at
 * sites, devices, or whole sites. Lists hold only what the user can see (the
 * API filters by site access). Under the tabs, a live line sizes the scope.
 */
export default function MetricsScopePicker({ value, onChange, preview }: Props) {
  const { sites, loading: sitesLoading, error: sitesError } = useSites()
  const { devices, loading: devicesLoading } = useDevices()
  const siteItems = useMemo<Item[]>(() => sites.map((s) => ({ id: s.id, name: s.name })), [sites])
  const deviceItems = useMemo<Item[]>(
    () => devices.map((d) => ({ id: d.id, name: d.name, hint: d.site_name })),
    [devices],
  )
  const set = (patch: Partial<NetworkScopeDraft>) => onChange({ ...value, ...patch })
  const toggleRole = (role: PortRole) =>
    set({ roles: value.roles.includes(role) ? value.roles.filter((r) => r !== role) : [...value.roles, role] })

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-1">
        {NETWORK_SCOPE_TABS.map((t) => (
          <button
            key={t.value}
            type="button"
            aria-pressed={value.scopeType === t.value}
            onClick={() => set({ scopeType: t.value })}
            className={`rounded-lg px-3 py-1.5 text-sm transition ${
              value.scopeType === t.value ? 'bg-primary-500/15 text-white' : 'text-slate-400 hover:text-white'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {value.scopeType === 'ports' && (
        <PortPicker
          devices={devices}
          devicesLoading={devicesLoading}
          value={value.ports}
          onChange={(ports) => set({ ports })}
        />
      )}

      {value.scopeType === 'port_roles' && (
        <div className="space-y-3">
          <div className="space-y-1">
            <p className="text-sm text-slate-300">Ports with these roles</p>
            {REPORT_ROLES.map((r) => (
              <label key={r.value} className="flex items-start gap-2 text-sm text-slate-300">
                <input
                  type="checkbox"
                  className="mt-1"
                  checked={value.roles.includes(r.value)}
                  onChange={() => toggleRole(r.value)}
                />
                <span>
                  {r.label} <span className="text-xs text-slate-500">{PORT_ROLE_HINT[r.value]}</span>
                </span>
              </label>
            ))}
          </div>
          <div className="space-y-1">
            <p className="text-sm text-slate-300">At these sites</p>
            <Checklist
              items={siteItems}
              value={value.sites}
              onChange={(s) => set({ sites: s })}
              empty={sitesError ?? 'No sites.'}
              loading={sitesLoading}
            />
          </div>
          <p className="text-xs text-slate-500">
            Worked out each time the report runs, so a port given one of these roles later is included.
          </p>
        </div>
      )}

      {value.scopeType === 'devices' && (
        <Checklist
          items={deviceItems}
          value={value.devices}
          onChange={(d) => set({ devices: d })}
          max={MAX_REPORT_SUBJECTS}
          empty="No devices."
          loading={devicesLoading}
        />
      )}

      {value.scopeType === 'sites' && (
        <div className="space-y-1">
          <Checklist
            items={siteItems}
            value={value.sites}
            onChange={(s) => set({ sites: s })}
            empty={sitesError ?? 'No sites.'}
            loading={sitesLoading}
          />
          <p className="text-xs text-slate-500">
            Each site&apos;s totals, plus every port of every device there, worked out each time the report runs.
          </p>
        </div>
      )}

      <SizeLine scopeType={value.scopeType} preview={preview} />
    </div>
  )
}
```

- [ ] **Step 5: Run the gate**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output.

- [ ] **Step 6: Manual checks (sandbox, with the backend of Tasks 1-15)**

1. New report → Metrics Report → Next: four tabs, Ports first. Choose a switch: its physical ports are listed first, then the others, with WAN and Uplink tags. Filtering by an alias narrows the list.
2. Tick three ports, then choose a second device and tick two. "Chosen ports (5)" lists each one as "device · port (alias)". The X removes one.
3. With the browser's devtools on Slow 3G, switch between two devices quickly. The list never shows one device's ports while the other is chosen.
4. Network tab: ticking five boxes quickly sends one `scope-preview` request about 0.4 s after the last tick. While it is pending the line reads "Sizing the scope…", then "Covers 5 ports right now".
5. Port roles: tick WAN and a site, and the line gives the site's WAN port count. Untick every role, and the amber error reads "Choose at least one port role" and Next is disabled.
6. Devices: tick a switch and a UPS → "Covers 2 devices right now". Sites: tick a site → "Covers N ports on M devices right now".
7. If a site with more than 500 ports exists (or the backend cap is temporarily lowered in the sandbox), the line turns amber: "Covers 640 ports — the report will include the 500 busiest".
8. As a member with access to one site only: every tab lists only that site and its devices.
9. Pick a device, then remove its site's access for this user in another tab. Change the scope here: a red "A chosen port, device or site is not available" appears under the tabs, and Next is disabled.
10. Uptime and Incident scope tabs are unchanged.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/hooks/usePorts.ts frontend/src/components/reports/PortPicker.tsx frontend/src/components/reports/MetricsScopePicker.tsx frontend/src/components/ReportBuilderWizard.tsx frontend/src/components/GenerateReportModal.tsx
git commit -m "feat(reports): network scope picker with a live scope size

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 19: The Metrics step (ordered list, defaults, re-apply rule)

The Metrics step is an ordered list of up to 10 metrics, pre-filled with the scope's defaults:
- Up/down buttons reorder the list, and X removes an item.
- The first item is tagged "ranks the rows".
- New metrics are added from a select of what the scope offers, grouped Built-in / From device profiles / Custom (MIB). Each chosen row shows its unit and source.
- When the scope changes, the draft hook (Task 16) re-applies the spec section 4 rule. An unedited list takes the new defaults. An edited list loses what no longer applies, and an amber note names what was removed.

**Files:**
- Create: `frontend/src/components/reports/ReportMetricsPicker.tsx`
- Modify: `frontend/src/components/ReportBuilderWizard.tsx`, `frontend/src/components/GenerateReportModal.tsx` (each: one import, one placeholder line)
- Test: the frontend gate; manual checks below

**Interfaces:**
- Consumes (Task 16):
  - `MetricChoice`, `MetricSource` and `MAX_REPORT_METRICS` (`@/types/reports`); `METRIC_SOURCE_LABEL` and `moveItem` (`@/utils/reportScope`).
  - The draft's `selection: { metrics, edited, dropped, choices }` and `setMetrics(keys)`, which marks the list edited and keeps at most 10.
- Produces: `ReportMetricsPicker({ choices: MetricChoice[]; value: string[]; onChange(keys: string[]); dropped: string[] })`.

- [ ] **Step 1: Put the picker where it will be used, and see it fail**

In `frontend/src/components/ReportBuilderWizard.tsx`, after `import MetricsScopePicker from '@/components/reports/MetricsScopePicker'`, add:

```tsx
import ReportMetricsPicker from '@/components/reports/ReportMetricsPicker'
```

and replace

```tsx
          <p className="text-sm text-slate-400">{draft.selection.metrics.join(', ') || 'No metrics yet.'}</p>
```

with

```tsx
          <ReportMetricsPicker
            choices={draft.selection.choices}
            value={draft.selection.metrics}
            dropped={draft.selection.dropped}
            onChange={draft.setMetrics}
          />
```

In `frontend/src/components/GenerateReportModal.tsx`, add the same import after its `MetricsScopePicker` import. Then replace

```tsx
                <p className="text-sm text-slate-400">{draft.selection.metrics.join(', ') || 'No metrics yet.'}</p>
```

with

```tsx
                <ReportMetricsPicker
                  choices={draft.selection.choices}
                  value={draft.selection.metrics}
                  dropped={draft.selection.dropped}
                  onChange={draft.setMetrics}
                />
```

Run the typecheck from the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine npx tsc --noEmit
```

Expected: FAIL — `error TS2307: Cannot find module '@/components/reports/ReportMetricsPicker'` in both files.

- [ ] **Step 2: Write the metrics picker**

`frontend/src/components/reports/ReportMetricsPicker.tsx`:

```tsx
import { ChevronDown, ChevronUp, X } from 'lucide-react'
import { MAX_REPORT_METRICS, type MetricChoice, type MetricSource } from '@/types/reports'
import { colors } from '@/utils/colors'
import { inputCls } from '@/utils/dashboards'
import { METRIC_SOURCE_LABEL, moveItem } from '@/utils/reportScope'

interface Props {
  /** What the scope offers, from the latest preview. */
  choices: MetricChoice[]
  /** Chosen keys in order; the first ranks the rows. */
  value: string[]
  onChange: (keys: string[]) => void
  /** Labels of metrics a scope change removed, for the note. */
  dropped: string[]
}

const GROUPS: { source: MetricSource; label: string }[] = [
  { source: 'builtin', label: 'Built-in' },
  { source: 'profile', label: 'From device profiles' },
  { source: 'custom', label: 'Custom (MIB)' },
]

const iconBtn = 'rounded p-1 text-slate-400 hover:bg-white/5 hover:text-white disabled:opacity-30 disabled:hover:bg-transparent'

/**
 * The Metrics step: an ordered list of up to 10 metrics, pre-filled with the
 * scope's defaults. The first ranks the rows. Choices are what the scope's
 * devices offer, grouped by where each metric is defined.
 */
export default function ReportMetricsPicker({ choices, value, onChange, dropped }: Props) {
  const byKey = new Map(choices.map((c) => [c.key, c]))
  const remaining = choices.filter((c) => !value.includes(c.key))
  const full = value.length >= MAX_REPORT_METRICS
  const addLabel = full
    ? `Up to ${MAX_REPORT_METRICS} metrics`
    : choices.length === 0
      ? 'This scope has no metrics yet'
      : remaining.length === 0
        ? 'Every metric is chosen'
        : 'Add a metric…'

  return (
    <div className="space-y-3">
      {dropped.length > 0 && (
        <p className={`rounded-md border p-2 text-xs ${colors.warning.border} ${colors.warning.bg} ${colors.warning.text}`}>
          Removed because the scope no longer offers {dropped.length === 1 ? 'it' : 'them'}: {dropped.join(', ')}.
        </p>
      )}

      {value.length === 0 ? (
        <p className="text-sm text-slate-400">No metrics chosen.</p>
      ) : (
        <ol className="space-y-1">
          {value.map((key, i) => {
            const c = byKey.get(key)
            const label = c?.label ?? key
            return (
              <li
                key={key}
                className="flex items-center gap-2 rounded-md border border-white/10 bg-slate-800/40 px-3 py-2 text-sm"
              >
                <span className="w-5 shrink-0 text-right tabular-nums text-slate-500">{i + 1}.</span>
                <span className="min-w-0 flex-1">
                  <span className="text-white">{label}</span>
                  {c && (
                    <span className="ml-2 text-xs text-slate-500">
                      {[c.unit, METRIC_SOURCE_LABEL[c.source]].filter(Boolean).join(' · ')}
                    </span>
                  )}
                  {i === 0 && (
                    <span className="ml-2 rounded bg-primary-500/15 px-1.5 py-0.5 text-xs text-white">
                      ranks the rows
                    </span>
                  )}
                </span>
                <button
                  type="button"
                  className={iconBtn}
                  aria-label={`Move ${label} up`}
                  disabled={i === 0}
                  onClick={() => onChange(moveItem(value, i, -1))}
                >
                  <ChevronUp className="h-4 w-4" />
                </button>
                <button
                  type="button"
                  className={iconBtn}
                  aria-label={`Move ${label} down`}
                  disabled={i === value.length - 1}
                  onClick={() => onChange(moveItem(value, i, 1))}
                >
                  <ChevronDown className="h-4 w-4" />
                </button>
                <button
                  type="button"
                  className={iconBtn}
                  aria-label={`Remove ${label}`}
                  onClick={() => onChange(value.filter((k) => k !== key))}
                >
                  <X className="h-4 w-4" />
                </button>
              </li>
            )
          })}
        </ol>
      )}

      <select
        className={inputCls}
        aria-label="Add a metric"
        value=""
        disabled={full || remaining.length === 0}
        onChange={(e) => e.target.value && onChange([...value, e.target.value])}
      >
        <option value="">{addLabel}</option>
        {GROUPS.map((g) => {
          const items = remaining.filter((c) => c.source === g.source)
          return items.length === 0 ? null : (
            <optgroup key={g.source} label={g.label}>
              {items.map((c) => (
                <option key={c.key} value={c.key}>
                  {c.label}
                  {c.unit ? ` (${c.unit})` : ''}
                </option>
              ))}
            </optgroup>
          )
        })}
      </select>

      <p className="text-xs text-slate-500">
        The first metric ranks the rows: it decides the busiest and the order of every table. In and out of the
        same measure (traffic, busy, errors) are reported side by side.
      </p>
    </div>
  )
}
```

- [ ] **Step 3: Run the gate**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output.

- [ ] **Step 4: Manual checks (sandbox, with the backend of Tasks 1-15)**

1. Metrics Report → Ports tab → tick two ports → Next. The list is pre-filled with Traffic in, Traffic out, Busy in and Busy out, in that order, and the first is tagged "ranks the rows".
2. Move Busy in to the top with its up button, and the tag moves with it. Remove one with X. The Add select lists the rest under Built-in, plus any profile and custom metrics under their own groups.
3. Add metrics until there are 10: the select is disabled and reads "Up to 10 metrics".
4. Devices tab with a profiled switch, without editing: Next shows its profile health metrics. With a UPS ticked too, the UPS readings follow. With only a plain unprofiled switch, the list is Traffic in and Traffic out.
5. The re-apply rule, unedited: go Back, switch from Ports to Devices, then Next. The list is the device defaults and there is no note.
6. The re-apply rule, edited: on a Devices scope with a profiled switch, remove one metric, go Back, and change the scope to a different device without that profile's CPU metric. Next shows the CPU metric gone and an amber note "Removed because the scope no longer offers it: CPU". Every other choice keeps its order.
7. Generate the Metrics report end to end. It lands on its detail page with a PDF whose tables follow the chosen order.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/reports/ReportMetricsPicker.tsx frontend/src/components/ReportBuilderWizard.tsx frontend/src/components/GenerateReportModal.tsx
git commit -m "feat(reports): ordered metrics step with scope defaults and the re-apply rule

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 20: Labels, scope descriptions and the comparison line on the report pages

The report list and detail page show "Metrics Report" and describe a Metrics scope in words, for example "WAN, Uplink ports at HQ, Annex · 4 metrics". Site and device names come from the user's own visible lists, so a hidden or deleted one is counted, never named.

That needs `scope_data` on the list response, which does not carry it today, so Steps 1-4 add it to signed-in responses only. Share-link responses stay as they are: name, period and files.

The Period step names the comparison period for Metrics reports, for example "Compared with August 2026". The public share page names scope types in words, so it shows "Port roles", not "Port_roles". Schedules, share links, history and downloads are unchanged.

**Files:**
- Modify: `backend/internal/api/report_builder_handler.go` (`ReportResponse`, `buildReportResponse`)
- Test: `backend/internal/api/report_response_db_test.go` (create)
- Modify: `frontend/src/pages/SavedReports.tsx`, `frontend/src/pages/SavedReportDetail.tsx`, `frontend/src/pages/PublicReport.tsx`
- Modify: `frontend/src/components/ReportBuilderWizard.tsx`, `frontend/src/components/GenerateReportModal.tsx` (the comparison line)

**Interfaces:**
- Consumes:
  - `models.ReportScope` and the existing `buildReportResponse(ctx, report *models.Report, shareToken string) (ReportResponse, error)`.
  - `NewReportBuilder(db, aggregator, pdfRenderer, scheduler, settings)`. Nil settings resolve labels in UTC.
  - `testdb.Open` / `testdb.NewUser` / `testdb.Must`.
  - From Task 16: `describeScope`, `scopeNames`, `comparisonLabel` (`@/utils/reportScope`), `SCOPE_TYPE_LABEL` and `SavedReport.scope_data?`.
  - `useSites()` and `useDevices({ skip })`.
- Produces: ``ReportResponse.ScopeData *models.ReportScope `json:"scope_data,omitempty"` ``, present on signed-in responses and absent on share-link ones.

- [ ] **Step 1: Write the failing test**

`backend/internal/api/report_response_db_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// The report pages describe a Metrics scope in words from scope_data, so a
// signed-in response carries it; a share-link response is read by anyone with
// the link and must not.
func TestDBReportResponseCarriesScopeOnlyWhenSignedIn(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, false)
	monitor := uuid.New()
	report := models.Report{
		ID:            uuid.New(),
		UserID:        owner,
		CreatedBy:     owner,
		Name:          "Web",
		ReportType:    models.ReportTypeUptime,
		ScopeType:     models.ScopeTypeMonitors,
		ScopeData:     models.ReportScope{MonitorIDs: []uuid.UUID{monitor}},
		TimeRangeDays: 30,
		PeriodKind:    models.PeriodRolling,
	}
	testdb.Must(t, db.Create(&report).Error)
	h := NewReportBuilder(db, nil, nil, nil, nil)

	signedIn, err := h.buildReportResponse(context.Background(), &report, "")
	testdb.Must(t, err)
	if signedIn.ScopeData == nil || len(signedIn.ScopeData.MonitorIDs) != 1 || signedIn.ScopeData.MonitorIDs[0] != monitor {
		t.Errorf("signed-in scope_data = %+v, want the one monitor", signedIn.ScopeData)
	}

	shared, err := h.buildReportResponse(context.Background(), &report, "a-share-token")
	testdb.Must(t, err)
	body, err := json.Marshal(shared)
	testdb.Must(t, err)
	if shared.ScopeData != nil || strings.Contains(string(body), "scope_data") {
		t.Errorf("share-link response carries scope_data: %s", body)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

From `backend/`:

```bash
./scripts/test-db.sh -run TestDBReportResponseCarriesScopeOnlyWhenSignedIn -v
```

Expected: FAIL — the package does not compile: `signedIn.ScopeData undefined (type ReportResponse has no field or method ScopeData)`.

- [ ] **Step 3: Implement**

In `backend/internal/api/report_builder_handler.go`, in `type ReportResponse struct`, replace

```go
	ScopeType     string    `json:"scope_type"`
	TimeRangeDays int       `json:"time_range_days"`
```

with

```go
	ScopeType     string    `json:"scope_type"`
	// ScopeData is what the report covers, so the report pages can describe a
	// Metrics scope in words. Only signed-in responses carry it: a share link
	// is read by anyone holding it and stays limited to name, period and files.
	ScopeData     *models.ReportScope `json:"scope_data,omitempty"`
	TimeRangeDays int                 `json:"time_range_days"`
```

In `buildReportResponse`, directly before `return ReportResponse{`, add:

```go
	var scope *models.ReportScope
	if shareToken == "" {
		s := report.ScopeData
		scope = &s
	}

```

and in that struct literal, after `ScopeType:     report.ScopeType,`, add:

```go
		ScopeData:     scope,
```

Run `gofmt -l internal/api/` from `backend/` and expect no output.

- [ ] **Step 4: Run it to see it pass, and vet**

From `backend/`:

```bash
./scripts/test-db.sh -run TestDBReportResponseCarriesScopeOnlyWhenSignedIn -v
go vet ./...
```

Expected: `--- PASS: TestDBReportResponseCarriesScopeOnlyWhenSignedIn`; vet clean.

- [ ] **Step 5: The report list**

In `frontend/src/pages/SavedReports.tsx`:

Change the first import to `import { useEffect, useMemo, useState } from 'react'`. After the `@/hooks/useReportBuilder` import block, replace `import { REPORT_TYPE_LABEL, type SavedReport } from '@/types/reports'` with:

```tsx
import { useDevices } from '@/hooks/useDevices'
import { useSites } from '@/hooks/useSites'
import { REPORT_TYPE_LABEL, type SavedReport } from '@/types/reports'
import { describeScope, scopeNames } from '@/utils/reportScope'
```

After `const [confirmId, setConfirmId] = useState<string | null>(null)`, add:

```tsx

  // Names for Metrics scopes. Devices are only fetched when a Metrics report
  // is listed; a name the user cannot see is counted, never shown.
  const hasMetrics = reports.some((r) => r.report_type === 'metrics')
  const { sites } = useSites()
  const { devices } = useDevices({ skip: !hasMetrics })
  const names = useMemo(() => scopeNames(sites, devices), [sites, devices])
```

Replace the subtitle text `Generate, schedule, and share uptime reports.` with `Generate, schedule, and share uptime, incident and metrics reports.`.

Replace

```tsx
                  <span className="capitalize">{report.scope_type}</span>
```

with

```tsx
                  {report.report_type === 'metrics' ? (
                    <span>{describeScope(report.scope_type, report.scope_data, names)}</span>
                  ) : (
                    <span className="capitalize">{report.scope_type}</span>
                  )}
```

(`REPORT_TYPE_LABEL[report.report_type]` already prints "Metrics Report".)

- [ ] **Step 6: The detail page and the public page**

In `frontend/src/pages/SavedReportDetail.tsx`, replace `import { REPORT_TYPE_LABEL, type ReportSchedule } from '@/types/reports'` with:

```tsx
import { useDevices } from '@/hooks/useDevices'
import { useSites } from '@/hooks/useSites'
import { REPORT_TYPE_LABEL, type ReportSchedule } from '@/types/reports'
import { describeScope, scopeNames } from '@/utils/reportScope'
```

After `const report = useMemo(() => reports.find((r) => r.id === id), [reports, id])`, add:

```tsx
  // Names for a Metrics scope; a site or device the user cannot see is
  // counted, never shown.
  const isMetrics = report?.report_type === 'metrics'
  const { sites } = useSites()
  const { devices } = useDevices({ skip: !isMetrics })
  const names = useMemo(() => scopeNames(sites, devices), [sites, devices])
```

(These run before the page's early returns, so the hook order never changes.) Replace

```tsx
            {REPORT_TYPE_LABEL[report.report_type]} · {report.scope_type} · {report.time_range_days} day window
```

with

```tsx
            {REPORT_TYPE_LABEL[report.report_type]} ·{' '}
            {isMetrics ? describeScope(report.scope_type, report.scope_data, names) : report.scope_type} ·{' '}
            {report.time_range_days} day window
```

In `frontend/src/pages/PublicReport.tsx`, after the `@/hooks/useReportBuilder` import block, add `import { SCOPE_TYPE_LABEL } from '@/types/reports'`, and replace

```tsx
            <span className="capitalize">{report.scope_type}</span>
```

with

```tsx
            <span>{SCOPE_TYPE_LABEL[report.scope_type] ?? report.scope_type}</span>
```

- [ ] **Step 7: The comparison line on the Period step**

In `frontend/src/components/ReportBuilderWizard.tsx`, change `import { describeScope, monitorScopeData } from '@/utils/reportScope'` to `import { comparisonLabel, describeScope, monitorScopeData } from '@/utils/reportScope'`, and replace

```tsx
          <PeriodSelector value={period} onChange={setPeriod} />
        </div>
      )}
```

with

```tsx
          <PeriodSelector value={period} onChange={setPeriod} />
          {isMetrics && comparisonLabel(period) && (
            <p className="text-sm" style={{ color: 'var(--vs-text-dim)' }}>
              {comparisonLabel(period)}
            </p>
          )}
        </div>
      )}
```

In `frontend/src/components/GenerateReportModal.tsx`, make the same import change. Then replace

```tsx
              <PeriodSelector value={period} onChange={setPeriod} />
            </fieldset>
```

with

```tsx
              <PeriodSelector value={period} onChange={setPeriod} />
              {isMetrics && comparisonLabel(period) && (
                <p className="mt-2 text-xs text-slate-400">{comparisonLabel(period)}</p>
              )}
            </fieldset>
```

(`comparisonLabel` returns '' while a custom range lacks a date, so nothing renders then.)

- [ ] **Step 8: Run the gate**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output.

- [ ] **Step 9: Manual checks (sandbox, with the backend of Tasks 1-15)**

1. Reports list: a Metrics report on WAN and Uplink ports at two sites, with 4 metrics, reads "Metrics Report" and "WAN, Uplink ports at HQ, Annex · 4 metrics". Uptime and Incident rows read exactly as before.
2. Its detail page header reads "Metrics Report · WAN, Uplink ports at HQ, Annex · 4 metrics · 30 day window".
3. A devices report reads "Devices: core-sw1, ups-1 · 6 metrics". A ports report reads "12 ports · 4 metrics".
4. As a member without access to one of the sites: that site is counted ("and 1 more", or "2 sites"), never named.
5. Wizard Period step for a Metrics report:
   - Last month → "Compared with August 2026" (run in October 2026).
   - Last 7 days → "Compared with the previous 7 days".
   - Exact dates 1-15 September → "Compared with August 17, 2026 to August 31, 2026".
   - An Uptime report shows no such line.
6. Devtools, Network: `GET /reports` items carry `scope_data`. The share page's `GET /public/reports/share/:token` has no `scope_data`, and that page shows "Port roles" as the scope.
7. Add a daily schedule to a Metrics report and "Generate and send now": delivery, history and share links behave as for Uptime reports.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/api/report_builder_handler.go backend/internal/api/report_response_db_test.go frontend/src/pages/SavedReports.tsx frontend/src/pages/SavedReportDetail.tsx frontend/src/pages/PublicReport.tsx frontend/src/components/ReportBuilderWizard.tsx frontend/src/components/GenerateReportModal.tsx
git commit -m "feat(reports): metrics scopes in words on the report pages; comparison period line

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 21: Docs — status and follow-ups

**Files:**
- Modify: `docs/superpowers/STATUS.md`
- Create: `docs/superpowers/plans/2026-10-03-network-phase5-followups.md`

**Interfaces:**
- Consumes: the rulings recorded while executing this plan, and the findings of the final whole-branch review.
- Produces: nothing code consumes.

This task runs after the final whole-branch review and its fix wave, so the follow-ups doc can list what that review fixed and what it deferred.

- [ ] **Step 1: Update `STATUS.md`**

In the roadmap table, replace the phase 5 row with:

```markdown
| 5 | Metric reports: a Metrics report type (ports, port roles, devices, sites), exact 95th percentiles, totals, previous-period comparison, scheduled and shared like the other reports | Done (`feature/network-phase5`, awaiting merge to `dev`); follow-ups in `plans/2026-10-03-network-phase5-followups.md` |
```

Mark phase 7 (UniFi) as **Next** and phase 6 (live site maps) as "Not started; unblocked by phase 4". Under "To be checked by the owner", add above the phase 4 list:

```markdown
Phase 5 (after updating the work install from `dev`):
1. Create a Metrics report for the two WAN ports, period last month; the PDF shows 95th in/out, the billable 95th and the total moved, and the numbers look right against the ISP's portal.
2. Create a Metrics report for a whole site with a schedule; the email arrives with the "ports · billable 95th · moved" summary line.
3. A Port roles scope picks up a port given the Uplink role after the report was created (run it again).
4. Create a report for a scope larger than 500 ports (if one exists) or check the wizard's size line on the biggest site; the PDF says how many were left out.
5. The wizard: Type → Scope → Metrics → Period; Uptime and Incident reports still build exactly as before.
```

Keep the phase 4 and phase 3 lists that the owner has not confirmed yet.

- [ ] **Step 2: Write the follow-ups doc**

`docs/superpowers/plans/2026-10-03-network-phase5-followups.md`, in the shape of `2026-10-02-network-phase4-followups.md`:

- **Rulings:** the four plan-level deviations at the top of this plan, then every ruling taken while executing, one line each, tagged with its task number.
- **Fixed along the way (existing bugs, Task 12):** `pdfText` shared one fpdf translator buffer, so concurrently rendered reports garbled each other's text; every report ended on a blank page holding only "Generated by Sentinel".
- **Fixed in the final whole-branch review:** one line per fix.
- **Known limitations:** at least:
  - Statistics are computed at run time from the 5-minute rollup; a year-long report over the full 500 rows costs what Task 9 measured (state the numbers).
  - Report definitions cannot be edited after creation (unchanged from today).
  - Device-profile scopes and running a dashboard as a report are not built (roadmap "Later").
- **Deferred follow-ups:** every finding from the final review deliberately left for later, one line each, tagged with its task number. Include these existing issues found while planning:
  - The list page's `GenerateReportModal` never opens (`generateOpen` is never set), so its path is unreachable today; the wizard is the working path.
  - The report list shows "Nd window" even for calendar periods.
  - `comparisonLabel` names the comparison period in the browser's time zone, not the report time zone setting.
  - The shared `useResource` in `usePorts.ts` does not guard against late responses; phase 5 adds a guarded `usePortChoices` for the port picker only.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/STATUS.md docs/superpowers/plans/2026-10-03-network-phase5-followups.md
git commit -m "docs(network): record phase 5 status, rulings and follow-ups

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 4: Hand over**

Run the full checks one last time — from `backend/`: `go vet ./...`, `go test ./...`, `./scripts/test-db.sh`; from the repo root: the frontend gate — and report the output. Merging `feature/network-phase5` into `dev`, pushing, and updating the work install are the owner's call (CLAUDE.md: push and merge only when asked); use the finishing-a-development-branch skill to present the options.
