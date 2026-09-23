# Reporting overhaul: two fixed report types, SLA targets, and a graph

## Problem

Today a report is assembled from a flexible "template" — a pick-list of up to
8 sections (`sla_compliance`, `incident_summary`, `charts`, `custom`,
`executive_summary`, `timeline`, `availability_breakdown`, `performance`),
with 7 pre-built templates seeded across four migrations mixing them in
different combinations (`backend/internal/models/report.go:34-63`; templates
seeded in migrations 015, 026, 032, 037). This is more choice than useful:
picking a report means picking a template, which means understanding what
each of 8 section names actually renders.

Separately, `Monitor.SLATarget *float64` (`backend/internal/models/monitor.go:176`)
already exists in the schema and is already read by report generation
(`ReportMetrics.SLATarget`/`SLAMet`, `backend/internal/services/report_aggregator.go:53-55`)
and rendered in both the HTML and PDF paths — but there is no UI anywhere to
set it. And no report, in any of the 7 templates, contains an actual visual:
every section is fpdf text/tables (`backend/internal/services/pdf_sections.go`),
confirmed by grepping the whole backend for any charting capability — none
exists.

## Scope

- Replace the template/section system with exactly two fixed report types:
  **Uptime Report** and **Incident Report**. No template management UI, no
  section picker, ever.
- Add a system-wide default SLA target (a new setting) with an optional
  per-monitor override (the existing `Monitor.SLATarget` field, exposed in
  the monitor form for the first time).
- Add a genuine visual to the Uptime Report: a cumulative uptime-vs-SLA line
  chart, drawn with `fpdf`'s native primitives (no new dependency).
- Existing saved report definitions, their schedules, and past generation
  history are wiped as part of this change — explicitly confirmed with the
  user, no migration path needed.
- Report scheduling (`backend/internal/models/report_schedule.go`) is kept
  as-is. It turns out to need **no changes at all**: `ReportSchedule` only
  ever stores a `ReportID` (`:76`), never a template reference directly — the
  type it schedules is whatever the `Report` row it points at now has.
- Out of scope: the monitor scope picker (monitors/groups/tags/types) is
  already fully built and shared between both report UIs
  (`ReportBuilderWizard.tsx:218-263`) and needs no redesign — it carries over
  unchanged. The period picker (rolling/calendar/custom) also carries over
  unchanged.

## Design

### Report types replace templates

`ReportTemplate` (the model, its table, and all seeded rows) is removed
entirely. `Report` and `ReportGeneration` drop `TemplateID` in favor of a new
`ReportType string` column:

```go
const (
    ReportTypeUptime   = "uptime"
    ReportTypeIncident = "incident"
)
```

`Report.Validate()` checks `ReportType` is one of these two instead of
checking `TemplateID != uuid.Nil`. Everything else on `Report` (scope,
period, custom title/description) is untouched.

`PDFRendererService.RenderReportToPDF` (`backend/internal/services/pdf_renderer.go:64`)
changes signature from `(data *ReportData, sections []string, nameHint string)`
to `(data *ReportData, reportType string, nameHint string)`, and its
section-dispatch loop (`:75-94`) becomes a two-way branch:

```go
switch reportType {
case models.ReportTypeUptime:
    drawPDFUptimeReport(pdf, data) // SLA summary + graph + per-monitor table
case models.ReportTypeIncident:
    drawPDFIncidentReport(pdf, data) // incident list + summary stats
}
```

`drawPDFUptimeReport` and `drawPDFIncidentReport` are new functions in
`pdf_sections.go` that assemble the fixed layout for each type, reusing the
existing per-section drawing helpers where they already do the right thing
(e.g. `drawPDFSLASection`, `drawPDFIncidentSection` survive as building
blocks even though the section-name dispatch around them goes away) and
adding the new graph-drawing helper (below) for the Uptime Report.

The HTML report path (`backend/internal/services/report_html_generator.go`)
gets the same two-way branch, minus the graph — HTML rendering already has
no visual and adding one there is not part of this pass; the HTML path stays
text/table-only, matching its current fidelity level. It is not the primary
deliverable — the PDF is — so the same care described below (Testing) does
not need to extend to it.

### SLA: global default + per-monitor override

New setting, following the existing key/value pattern in
`backend/internal/models/setting.go`:

```go
const SettingDefaultSLATarget = "default_sla_target"
const DefaultSLATargetPercent = 99.9
const MinSLATargetPercent = 0.0
const MaxSLATargetPercent = 100.0
```

`SettingsService` needs a `GetFloat`/`SetFloat` pair (it currently only has
`GetString`/`SetString` and `GetInt`/`SetInt`,
`backend/internal/services/settings_service.go:105-171`) — mirroring
`GetInt`/`SetInt` exactly, parsing/formatting as `float64` instead of `int`.
A `DefaultSLATarget(ctx) float64` accessor on top of that mirrors
`IncidentRetentionDays`'s shape (`:206-215`): read, validate against the
min/max bounds, fall back to `DefaultSLATargetPercent` and log if the stored
value is somehow out of range.

`GetSettingsHandler` (`backend/internal/api/settings_handler.go:25`) adds
`"default_sla_target": settingsService.DefaultSLATarget(ctx)` to its
response. `updateSystemRequest`/`UpdateSystemSettingsHandler` (`:44`, `:67`)
add a `DefaultSLATarget *float64` field, validated against the same bounds,
following the exact pattern `DefaultCheckInterval` already uses there
(`:111-123`).

Frontend: `Settings.tsx` gets a new field in its system-settings section,
next to the existing `default_check_interval` input (same component,
`Number.isFinite` validation pattern already used at `:332-333`).

Monitor form: the existing `CreateMonitorModal.tsx`/`MonitorForm.tsx` gets a
new optional "SLA Target %" field, following the existing sentinel-value
convention this codebase already uses elsewhere for an optional numeric
override (nil = use the default, a real value = override) — mirroring how
`IPAddressOverride` and the agent resource thresholds both already handle
"leave blank to use the default" for a per-item override of a system-wide
default. Helper text: "Leave blank to use the system default (currently
X%)."

**Effective SLA resolution**, one small helper used everywhere an effective
target is needed (report generation, and the graph's reference line):

```go
func EffectiveSLATarget(monitorOverride *float64, systemDefault float64) float64 {
    if monitorOverride != nil {
        return *monitorOverride
    }
    return systemDefault
}
```

### Uptime computation: unify on `measurableWindow`

`report_aggregator.go`'s per-monitor loop computes `ReportMetrics.Uptime`
via the same `incidentService.GetDowntimePercentage`/`GetIncidentDuration`/
`GetIncidentCount` calls `GetSummaryReportHandler` uses
(`backend/internal/api/report_handler.go:744-756`) — but without first
clamping the window to the monitor's `CreatedAt` the way that handler does
via `measurableWindow` (`:671-680`). A monitor added partway through a report
period is currently scored over the full requested period rather than just
the time it existed, the same bug the Overview page fix (`42af4d9`) already
corrected in the newer code path.

The aggregator's loop is changed to call `measurableWindow(monitor.CreatedAt,
start, end)` first (moving the function to a shared location both files can
call — `backend/internal/services/` alongside the aggregator itself, since
`api` importing from `services` is the existing direction and `services`
cannot import `api`) and skip a monitor entirely when it returns `false`,
exactly matching the handler's existing behavior, rather than maintaining
two slightly different implementations of the same clamp.

### The graph: cumulative uptime % vs. SLA, one line for the whole scope

New computation, new to this codebase (no existing time-bucketed uptime
series exists anywhere — grepped for anything resembling a
"day-by-day"/"time series" uptime function, found none):

For a report covering `[start, end]` with `N` monitors in scope, the graph
plots one series: at each sample point `t` between `start` and `end` (a
day-by-day or otherwise evenly-spaced set of points — exact spacing is a
plan-time decision balancing chart resolution against how many incident
queries it costs to compute), the **cumulative uptime percentage from
`start` to `t`**, aggregated across every monitor in scope, each weighted by
its own measurable time (a monitor added mid-period contributes only the
minutes it has actually existed for, consistent with `measurableWindow`).

```go
// UptimeSeriesPoint is one sample of the cumulative-uptime-to-date line.
type UptimeSeriesPoint struct {
    Date   time.Time `json:"date"`
    Uptime float64   `json:"uptime"` // cumulative % from the report's start through Date
}
```

The reference line is the **effective SLA averaged across the monitors in
scope** (flat, one value, spanning the full width of the chart) — noted in
the spec per the user's own framing that a scope mixing monitors with
different overrides is a rare edge case, not a case this needs to handle
with per-monitor nuance.

`drawPDFUptimeReport` renders this as a line chart using `fpdf`'s native
`Line`/`SetDrawColor`/`Text` primitives: an axis box, the SLA line drawn flat
across it, the cumulative-uptime line drawn as connected segments between
sample points, with axis labels (dates along the bottom, percentage along
the side). No new Go dependency, no image-embedding pipeline — consistent
with why this codebase draws PDFs directly already (see the package doc
comment at the top of `pdf_renderer.go`: no external binary, no subprocess).

### What gets wiped

A new migration (043, the next number after 042):

- Drops `report_templates`.
- Drops `template_id` from `reports`, adds `report_type` (`text not null`,
  checked against `'uptime'`/`'incident'`). `report_generations` does not
  carry `TemplateID` today (`backend/internal/models/report.go:217-224` — it
  only has `ReportID`, `PDFPath`, `FileSize`, `GeneratedBy`), so it needs no
  column change, only its rows removed below.
- Truncates `reports`, `report_generations`, `report_schedules`, and
  `report_access` — explicitly confirmed with the user as a full wipe, no
  migration of existing rows onto the new `report_type` values.
- Existing generated PDF files on disk (under the report output directory)
  are deleted at the same time the DB rows are, so an orphaned file can never
  be reached through a dangling `report_generations` row that no longer
  exists — the migration itself can't touch the filesystem, so this is a
  step in the deploy/rollout process (documented in the plan), not the SQL
  file.

### Frontend

`ReportBuilderWizard.tsx`'s "Template" step is replaced with a "Report Type"
step: two large selectable cards (Uptime Report / Incident Report) instead of
a list of named templates. `GenerateReportModal.tsx` gets the equivalent
simplification. The Scope and Period steps are unchanged (per Scope, above).

`SavedReportDetail.tsx`/wherever a generated report's on-screen preview lives
gets whatever minimal change is needed to reflect `report_type` instead of a
template name — exact scope of this determined at plan-writing time by
reading that file, not guessed here.

## Testing

- `EffectiveSLATarget`: table-driven unit test (nil override → default,
  non-nil override → override value, boundary values at 0/100).
- `SettingsService.GetFloat`/`SetFloat`: mirrors whatever test coverage
  `GetInt`/`SetInt` already has, if any — checked at plan-writing time.
- `measurableWindow`'s move to a shared location: existing behavior is
  preserved exactly; a test confirms both the report-handler summary
  endpoint and the aggregator now produce identical uptime figures for a
  monitor created partway through a report window, where before this change
  they would have differed.
- The cumulative-uptime series computation: unit tests against a synthetic
  set of incidents (an outage on day 3 of a 10-day window should produce a
  dip at day 3 that partially recovers by day 10, not a flat line and not a
  permanent drop to the outage's instantaneous percentage).
- PDF generation: existing report-generation tests (if any — checked at
  plan-writing time) updated for the new two-type dispatch; a smoke test
  that both report types generate a non-empty PDF without error for a
  representative scope.
- Migration 043 is tested against a database with existing rows in every
  table it touches (the established convention in this codebase's migration
  tests), confirming the truncate + column changes apply cleanly.

## Release

Ships as a feature release once implemented and verified. Per the user's
own plan, this is what bumps the version to `v0.4.0` — not something this
plan tags itself; the user will do that once satisfied, the same way every
prior feature in this project has been released.
