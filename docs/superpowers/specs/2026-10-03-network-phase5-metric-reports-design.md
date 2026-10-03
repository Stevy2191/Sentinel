# Network phase 5: metric reports

Status: approved in conversation 2026-10-03; this spec is the written form.
Roadmap: `2026-09-28-network-monitoring-roadmap.md`, Phase 5 and the "Report
query cost" risk.

## Purpose

Sentinel's reports cover monitors only: Uptime and Incident, the two fixed
types from the reporting overhaul (`2026-09-23-reporting-overhaul-design.md`).
Phase 5 adds a third fixed type, **Metrics**, that turns the network data
Sentinel already collects (port traffic, utilisation, errors, UPS readings,
profile and custom MIB metrics) into the same scheduled, emailed, shareable
PDF.

Its readers, all four of them confirmed by the owner:

- **Capacity planning.** Which links and devices run hot, and is it growing?
- **ISP / WAN bill checks.** The 95th percentile and total volume of each
  uplink, computed the way carriers bill.
- **Management summary.** A short headline page for non-IT readers.
- **Troubleshooting history.** A report on one device or port over a past
  window.

## Scope

In:

- A `metrics` report type, a network scope (ports, port roles at sites,
  devices, sites), up to 10 metrics with editable defaults, and a fixed PDF
  layout: headline page, combined charts, per-metric tables, small charts
  for the busiest rows.
- A comparison with the previous period on every figure.
- Exact 95th percentiles of 5-minute averages, computed at run time from the
  5-minute rollup (approach 1 in the brainstorm).
- The wizard reordered to Type → Scope → Metrics → Period, and a small
  endpoint that sizes a scope before the report is generated.

Out:

- Device-profile scopes (the roadmap's list); a device scope covers the need
  for now.
- Running a dashboard as a report (roadmap "Later").
- A per-day statistics table. If the measured performance test shows
  year-long site reports are too slow, a daily table can later serve
  averages and totals while the 95th stays exact.
- Changing the existing Uptime and Incident reports beyond the wizard order
  and the type-aware email summary.

## Decisions

| # | Decision | Why |
|---|---|---|
| 1 | A third fixed report type, no section picker | The overhaul's rule: the user picks data, not sections |
| 2 | Statistics computed at run time from `metrics.samples_5m` | The rollups are kept forever, so any period has data; exact billing percentiles; no new table |
| 3 | 95th = `percentile_cont(0.95)` over 5-minute averages | The carrier definition of burstable billing |
| 4 | Combined rows use phase 4's rule: sum additive units (`bps`, `per_min`), average the rest, per 5-minute bucket, then take statistics | One rule everywhere; a site's 95th is the 95th of its real combined traffic |
| 5 | Scope checked as the report owner at every run, through site access | The same model monitor reports use, with site access in place of monitor ownership |
| 6 | At most 500 rows; a larger scope keeps the 500 busiest | Keeps runs inside the 5-minute job limit; the measured test confirms the number |
| 7 | At most 10 metrics, first one ranks the rows | Keeps the PDF readable and the ranking explicit |
| 8 | Reuse `scope_type` / `scope_data`, not a new column | One scope model for every type; `Validate` pairs scope types with report types |

## Design

### 1. Data and scope

**Report type.** `models.ReportTypeMetrics = "metrics"` joins
`ValidReportTypes` (`backend/internal/models/report.go:45`). Migration
**058** drops and re-creates the `report_type` CHECK (from 043) and the
`scope_type` CHECK (last widened in 026) to add the new values. No existing
row changes.

**Scope types and data.** `ReportScope` (`models/report.go:61`) gains:

```go
PortIDs   []uuid.UUID `json:"port_ids,omitempty"`   // device_interfaces ids
SiteIDs   []uuid.UUID `json:"site_ids,omitempty"`
DeviceIDs []uuid.UUID `json:"device_ids,omitempty"`
Roles     []string    `json:"roles,omitempty"`      // models.ValidPortRoles
Metrics   []string    `json:"metrics,omitempty"`    // metric keys, 1-10, ordered
```

| `scope_type` | Requires | Resolves to |
|---|---|---|
| `ports` | `port_ids` (1-500) | those ports |
| `port_roles` | `site_ids` (≥1) and `roles` (≥1 of `wan`, `uplink`, `access`) | every port with one of those roles on a device in those sites, at run time |
| `devices` | `device_ids` (1-500) | those devices |
| `sites` | `site_ids` (≥1) | the site totals plus every port of every device in those sites, at run time |

`Report.Validate` enforces that `metrics` reports use only these four scope
types and `uptime`/`incident` reports only the monitor ones
(`monitors`/`tags`/`groups`/`types`), plus the counts above. Its error text
names the allowed values for the report type (and fixes the existing message
that omits `types`). The API binding tag `oneof=uptime incident`
(`api/report_builder_handler.go:98`) and the `scope_type` binding grow to
match.

**Metrics.** `scope_data.metrics` holds 1-10 keys, each one that exists in
the metric catalogue or among the profile/custom metrics. Metrics with unit
`enum` are refused ("text metrics cannot be reported"). A `bool` metric is
reported as the share and the length of time it was true (e.g. "on battery
2 h 13 m").

Defaults the editor pre-fills (section 4), by scope type:

- `ports`, `port_roles`: `if_in_bps`, `if_out_bps`, `if_in_util_pct`,
  `if_out_util_pct`.
- `devices`: in this order, the profile health metrics of the chosen
  devices, then the UPS readings (charge, load, runtime, on battery) if any
  chosen device is a UPS, capped at 10; if neither applies (plain switches
  without a profile), `if_in_bps` and `if_out_bps` as device totals.
- `sites`: `if_in_bps`, `if_out_bps` (site total and per port).

**Access.** At every run the scope is resolved as the report's owner (the
schedule owner for a scheduled run, the requester on demand), as
`report_aggregator.go:~177` does for monitors, using site access
(`services/site_access.go`). Ports and devices the owner cannot see, and
ones that no longer exist, are dropped; the PDF's warnings count them
("2 ports are no longer available to you") without naming them.

On create, a picked port, device or site that is hidden or missing fails
with one message for both cases: "A chosen port, device or site is not
available". The editor cannot be used to probe ids (the phase 4 rule).

**Cap.** After resolution at most 500 ports or devices (a device's
instance rows, such as its sensors, count as that one device). A larger
`port_roles` or `sites` scope keeps the 500 busiest by the ranking metric
and says so on the headline page; it does not fail.

### 2. The computation

All statistics read `metrics.samples_5m` (`bucket, series_id, vmin, vmax,
vsum, n`; `migrations/048_metrics.sql`), joined to `metrics.series` and
filtered by the resolved series and the period.

**Per-row statistics**, one row per port, per device, or per device
instance (e.g. a stack member's CPU, a temperature sensor):

| Figure | Definition |
|---|---|
| Average | `sum(vsum) / sum(n)` |
| Min | `min(vmin)` |
| Peak | `max(vmax)` (raw samples inside each bucket, so short bursts count) |
| 95th | `percentile_cont(0.95) WITHIN GROUP (ORDER BY vsum / n)` over the period's 5-minute buckets |
| Total | rates only: `bps` → bytes moved (Σ bucket average × 300 s ÷ 8); `per_min` → count (Σ bucket average × 5). Other units have no total |
| Coverage | buckets with data ÷ buckets in the period. Under 90% the row is flagged |

**Combined rows** (site totals, and device totals of port metrics) first
combine per 5-minute bucket with phase 4's rule: `bps` and `per_min` are
summed, every other unit averaged (the `setTotals` rule in
`internal/dashboards/totals.go`; the plan decides whether to share or mirror
it). Statistics are then taken over the combined series. Device totals of
`if_*` metrics count physical interfaces only.

**In/out pairs.** Metrics with an `_in_`/`_out_` counterpart (traffic, busy,
errors, discards) are paired when both are in the report. For traffic
(`bps`), the pair adds a **billable 95th** = max(95th in, 95th out).

**Previous period.** The same statistics for the period before: the previous
calendar unit for a calendar period, otherwise the same length immediately
before the report's start. Average, 95th and total show the change in
percent; a row with no data in the previous period shows "new"; a change
from zero shows "new" as well, never a division by zero.

**Ranking.** Rows are ranked by the 95th of the report's first metric (for a
traffic pair, the billable 95th). The ranking chooses the 500-row cap, the
busiest-5 list and the ~10 individually charted rows. Rows without data are
not ranked.

**Running hot.** When `if_in_util_pct` or `if_out_util_pct` is in the
report, ports whose 95th busy (either direction) is ≥ 80% are listed on the
headline page.

**Charts.** One series per metric for the whole scope (combined per the
rule above) and one per charted row, at about 200 points: from
`samples_5m` for periods up to 7 days and `samples_1h` beyond, matching
`PickResolution`'s split (`services/metrics_store.go:241`).

**Cost.** One statistics query per metric per period, grouped by series,
plus the chart queries. Each query runs with a statement timeout inside the
existing 5-minute job limit (`report_job_queue.go:30`,
`report_scheduler.go:22`). A run that hits it fails with "This report is too
large to build: narrow the scope or shorten the period". The job queue today
retries any failure up to `MaxJobAttempts` (3); this failure is marked
permanent so it is not retried.

### 3. The PDF

A4 portrait, `fpdf`, the shared header, warnings box and footer
(`services/pdf_renderer.go`). `RenderReportToPDF`'s type switch (`:77`)
gains `case models.ReportTypeMetrics: drawPDFMetricsReport(pdf, data)`.
`ReportData` gains a `Network *MetricsReportData` field holding everything
below; the monitor fields stay untouched.

**Page 1, the headline.**

- Title and scope in words: "WAN and uplink ports at HQ and Annex ·
  September 2026, compared with August 2026".
- One tile per metric for the scope's combined row: traffic shows the 95th
  and the total; percentages the average and the 95th; anything else the
  average. Each tile shows its change.
- **Running hot** (if any), **Busiest 5**.
- Notes: flagged coverage, the 500-row cut, unavailable subjects, skipped
  metrics.

**Charts**, two per page: one per metric for the whole scope with the
period's 95th as a reference line. `drawPDFUptimeGraph`
(`pdf_renderer.go:362`) is generalised into a line-chart helper (axes,
unit-aware tick labels, reference line) that both report types use.

**Tables**, one per metric or in/out pair, sorted busiest first, every row
(≤ 500), the header repeated on each page.

- Traffic pair: Port, 95th in, 95th out, Billable 95th, Peak, Total in,
  Total out, Change (on the billable 95th).
- Other pairs: Name, Average in/out, 95th in/out, Peak, Total (rates), Change.
- Single metrics: Name, Average, 95th, Peak, Total (rates), Change.
- Names: "device · port (alias)" or "device · instance label"; long names
  are truncated; flagged rows carry a short coverage note.

**Busiest ~10**, small charts: each charted row's first metric, with in and
out together for a pair.

**Units.** `bps` → Kbps/Mbps/Gbps; byte totals → MB/GB/TB; `per_min` totals
→ counts; `bool` → share and hours/minutes; `%`, `°C`, `V`, `min` as they
are; custom units printed verbatim.

**Elsewhere.**

- `SummaryLines` (`services/report_mailer.go:353`) becomes type-aware:
  Metrics reads "12 ports · billable 95th 640 Mbps (+18%) · 2.1 TB moved"
  (the figures that exist for the report's metrics).
- The HTML generator (`report_html_generator.go`) gets the headline and the
  tables without charts, matching its existing fidelity.

### 4. The editor and pages

**Wizard order** (`components/ReportBuilderWizard.tsx`): Type → Scope →
Metrics → Period. The Type step shows three cards (Uptime, Incident,
Metrics); `ReportType` and `REPORT_TYPE_LABEL` (`types/reports.ts`) gain
`metrics`. Uptime and Incident skip the Metrics step and keep their scope
tabs unchanged. `GenerateReportModal.tsx` follows the same order.

**Metrics scope step**, four tabs:

- Ports: pick a device, tick its ports, repeat across devices; chosen ports
  collect in a list ("device · port (alias)").
- Port roles: sites multi-select plus WAN / Uplink / Access checkboxes.
- Devices: devices multi-select.
- Sites: sites multi-select.

Pickers list only what the user can see (reusing the dashboard pickers where
they fit). Under the tabs a live line sizes the scope: "Covers 214 ports
right now" or "Covers 640 ports — the report will include the 500 busiest".
It comes from a new endpoint, `POST /reports/scope-size` (authenticated,
rate limited with the report routes), which resolves a metrics scope as the
caller and returns `{ports, devices, capped}`; hidden or missing subjects in
the request get the create error message.

**Metrics step.** Pre-filled with the scope's defaults; an ordered list of
up to 10, reordered with up/down buttons; the first is labelled "ranks the
rows". The choices are the metrics that exist for the scope's devices
(built-in, profile and custom, labelled), without `enum` ones. Changing the
scope type re-applies defaults only if the list was not edited; otherwise
metrics that no longer apply are removed with a note naming them.

**Period step.** Unchanged (`PeriodSelector.tsx`), plus a line naming the
comparison period ("Compared with August 2026").

**List and detail pages.** A "Metrics" label; the detail page describes the
scope in words ("WAN, Uplink ports at HQ, Annex · 4 metrics"). Schedules,
share links, history and downloads are unchanged. Definitions stay
immutable, as today.

### 5. Errors and limits

**Create** (field errors shown in the wizard):

- hidden or missing subjects: the one message above;
- metrics missing, more than 10, unknown, or `enum`;
- a scope type that does not match the report type;
- counts out of range (`port_ids`/`device_ids` > 500, empty `site_ids` or
  `roles`).

**Run**:

| Situation | Result |
|---|---|
| Some subjects unavailable | Dropped, counted in the warnings box |
| Every subject unavailable | A one-page PDF saying so; the run succeeds and the email goes out |
| A metric no longer exists | Skipped, with a warning naming it |
| A row has no data | "No data", not ranked |
| No row has data | Headline: "No data for this period" |
| Statement timeout | "This report is too large to build: narrow the scope or shorten the period"; not retried |

**Limits.** 500 rows after resolution, 10 metrics; the existing period rules
(rolling ≤ 365 days), the 5-per-minute generate limit and the 50-recipient
limit are unchanged. The previous period always has data to read (rollups
are never dropped).

## Testing

- **Unit:** unit formatting (bps, bytes, counts, bool durations); in/out
  pairing and the billable 95th; change and "new" (including from zero);
  `Validate`'s scope-type/report-type pairing and counts; row naming; the
  type-aware summary line.
- **DB (hand-computed expectations):** average, min, peak, 95th, total and
  coverage for one series; a site total that sums `bps` but averages `%`;
  device totals counting physical interfaces only; the previous period
  (calendar and rolling); `port_roles` resolved at run time (a role added
  after creation is included); the 500 cap keeping the busiest; a custom
  metric; hidden and deleted subjects dropped with a warning that names
  nothing; the create-time message identical for hidden and missing.
- **PDF:** a Metrics report renders; assertions on drawn text (headline
  tiles, "Billable 95th", "Running hot", the no-data page) using the
  existing `pdfDrawnText` pattern (`services/pdf_renderer_test.go`).
- **Measured performance:** generated 5-minute data (about 100 ports × 90
  days) through the real statistics queries; the test records the time and
  checks a budget, and the plan sets the 500-row cap from that number.
- **Migration:** 058 applies on a database that already holds reports of
  both existing types.
- **Simulator end to end:** a polled simulator device yields a non-empty
  Metrics PDF.
- **Frontend:** the gate (tsc, eslint, build); owner checks on the work
  install for the wizard and a real report from the switches.
