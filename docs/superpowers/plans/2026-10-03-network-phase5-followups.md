# Network phase 5: rulings and deferred follow-ups

This records the decisions taken while executing `2026-10-03-network-phase5-metric-reports.md`, plus the review findings deliberately left for later. Done on `feature/network-phase5`, awaiting merge to `dev`.

## Rulings

Deviations from the spec, decided while planning:

- **No HTML report change.** The spec's "Elsewhere" bullet names `report_html_generator.go`, which no longer exists (there is no HTML report path today).
- **New drawing code goes in new files** (`pdf_chart.go`, `pdf_units.go`, `pdf_metrics.go`): `pdf_sections.go` no longer exists, and the drawing code lives in `pdf_renderer.go`.
- **`POST /reports/scope-size` became `POST /reports/scope-preview`.** Besides the size and the capped flag it returns the metrics available for the scope and the defaults: the Metrics step needs both, and the defaults depend on backend knowledge (profiles, UPS devices).
- **The builder lives in `internal/netreport`** (like phase 4's `internal/dashboards`); the data types the renderer draws live in `services`, so `services` never imports `netreport` (import cycle).

Settled while planning (the spec left these open):

- Profile metrics of kind `status` (fan or power-supply states) are refused like `enum`, with "text metrics cannot be reported".
- Link speed (`if_speed_bps`) cannot be reported ("link speed cannot be reported"): it is a setting, not a load.
- A device metric on a `ports` or `port_roles` scope is refused at create ("<label> is a device metric: report it on devices or sites") and skipped at run time; the preview offers only port metrics there.
- On a `sites` scope each table starts with "<site> (site total)" rows, which are never ranked, capped or listed among the busiest.
- Scope labels read "N ports", "N devices", "WAN, Uplink ports at HQ, Annex" and "All of HQ".
- A `bool` metric's `RowStats.Avg` is its share of time true (0-1); the PDF prints it as a percentage.
- The job queue's general data-gathering failure reads "could not gather the data for this report" for every report type.
- `scope-preview` has its own rate limit (60 a minute per user, burst 20), since the picker calls it about 400 ms after each scope change; generate keeps its 5 a minute.
- The report list and detail responses gain `scope_data` (signed-in responses only) so the pages can describe a Metrics scope.
- The headline notes (CappedOut, Unavailable, Skipped, LowCoverage) are drawn by the PDF, not copied into `ReportData.Warnings`.
- Two existing PDF bugs are fixed in Task 12 (see "Fixed along the way").
- Task 9's performance test runs only with `SENTINEL_PERF=1`; the 500 cap stands if SeriesStats over 100 ports × 30 days takes 3 s or less.

Decisions taken while executing (P = the pre-flight scan of the plan, R = during the tasks):

- **R1-R3 (process).** Checks ran with the scratchpad helpers (go-docker, go-test-db [--sim], fe-gate) because Go is not installed on the host (same images and commands); commit trailers name the model that wrote the code (the phase 1 rule); each task ran focused tests, gofmt, vet, the no-database suite and the touched packages' DB tests, with the full DB suite at Tasks 8 and 15 and before the final review, to avoid hours of identical reruns.
- **R5 (process).** While the Sonnet weekly limit was spent, implementers ran on haiku when the brief carried the complete code and on opus for judgement-heavy tasks (5, 7b, 14, 17, 18, 20), with opus reviewers, so the work did not stall for two days; small fix-round diffs (tests, comments, one-liners) were verified by the controller instead of a re-review (the phase 4 R23 precedent).
- **P1.** (T2) The DB test resets `got` before the second `db.First`, because GORM keeps the stale primary key as a condition.
- **P2.** (T11/T13) The subject wording follows `ScopeType` (a devices scope says "N devices") while T7b keeps filling Ports with physical ports: the PDF and email must name what the report is about.
- **P3.** (T6/T7b/T11) The 500 cap counts ports and devices together and cuts devices first on sites, and the preview sizes a scope without its metrics, so a sites scope pushed over 500 only by device metrics is capped without the preview saying so; site totals still combine every port in the site. Rare, and it keeps the preview metric-independent.
- **P4.** (T15) The simulator test calls `refresh(t, db)` after copying the samples back, to avoid a flaky test.
- **P5.** (T7b/T11-T13) "Billable 95th" only for an in/out traffic pair; a bool chart has no 95th reference line; an empty scope reads "No available sites", not "All of no available site": wording defects in the plan.
- **P6a.** Bool metrics rank by share of time true, not by the 95th as spec §2 says: a 95th of 0/1 samples is meaningless.
- **P6b.** Each row and tile carries one change figure (on the 95th for traffic, on the average otherwise), not the three spec §2 lists, to fit A4 width.
- **P6c.** The PDF's prose scope label and the pages' `describeScope` ("Sites: …") may word a scope differently: the list page needs a type prefix, while the PDF reads as a sentence.
- **P7.** (T7b/T12/T15-T17) The underlying timeout is logged before mapping to `ErrReportTooLarge`, one shared reportable-on-scope predicate (`metricInfo.allowedOn`) replaces three copies, `METRICS_WAIT_MS` is defined once and tests check every error: rubric defects the reviews would raise anyway.
- **P8.** Implementers ignore slips in the plan text (dangling "Contract change N" references, a wrong RED line in T4, Review Focus naming Task 7 for 7b, small wording errors): none changes what is built.
- **R4.** (T3) A query cancelled by the job's own 5-minute deadline (also SQLSTATE 57014) reads as "too large to build": a report that cannot finish within the job limit is too large in practice, and that message gives the right advice.
- **R6.** (T4) A combined row counts a bucket as covered when any member reported: the plan defines it so, and per-member coverage on totals would need a second pass (known limitation).
- **R7.** (T4) Combined rows' Min and Peak are the min and max of the combined 5-minute values, since raw peaks at different minutes cannot be summed (known limitation).
- **R8.** (T4) Fix round: chart buckets align to the window start (`time_bucket` origin, plus an unaligned-start test), so a Chicago monthly chart's first point no longer falls before the period; `ErrReportTooLarge` is returned unwrapped as in SeriesStats; a file header.
- **R9.** (T5) `site_ids` had no upper bound (more than 65,535 ids reach pgx and return 500); deferred to the final fix wave, which capped it at 500, since only an absurd, rate-limited request triggers it and nothing leaks.
- **R10.** (T5) Fix round: resolve-level non-admin tests for sites, port_roles and devices with a hidden second site, plus an editable share, because this package is the report's access gate and only ports had such a test.
- **R11.** (T6) Custom (user-defined MIB) metrics on a device count as its "profile health metrics" for the devices defaults: the owner defined them because they matter, and spec §1 covers profile-attached custom metrics.
- **R12.** (T7a) A value that is zero in both periods shows 0%, not "new": 0 → 0 is no change, and labelling every quiet error counter "new" would be noise.
- **R13.** (T7b) Fix round: bool charts get no reference line (P5); a visible device with no series on a devices scope gets a "No data" line instead of vanishing (spec §5), so the counts agree with the label; empty ports and devices scopes read "No available ports" and "No available devices"; the hidden-site test asserts no row names that site.
- **R14.** (T9) The 500 cap stands: SeriesStats over 100 ports × 30 days took 387 ms against the 3 s threshold. The worst case (500 ports × 10 metrics × a year) can reach the 5-minute job limit and then fails with "too large" (known limitation).
- **R15.** (T9) Accepted that the first table and the scope totals read every candidate before the cap (about 50 s at 5,000 ports, 3.5 min at 20,000): a county estate is hundreds to low thousands of ports, and past the job limit the "too large" message advises narrowing.
- **R16.** (T11) Fix round: a sites subject falls through to "N ports" or "N devices" (P2); the sites cap note reads "Left out N ports or devices beyond the limit of 500 ports or devices; the report includes the 500 busiest."; the devices and single-traffic tests now pin what they claim; the shared fixture uses port_roles, which Tasks 12-13 reuse.
- **R17.** (T12) The first table column reads "Port" on ports and port_roles scopes (every row is a port) and "Name" otherwise: devices and sites tables mix site totals and devices, and spec §3's "Port" describes the bill-check table, which is a port scope.
- **R18.** (T12) Fix rounds: the footer restores the auto-page-break setting it found; the coverage note uses `formatPercent`, so 89.6% never prints as "90% data"; a NoData row has no coverage note; table-driven header tests. Rounds 2-3 replaced two vacuous Port/Name header tests with whole-line assertions shown failing when inverted.
- **R19.** (T13) Fix round: one caption helper serves the PDF tiles and the email so they never disagree on a billing figure (a single "Traffic in" is "95th", not "billable 95th"); the devices wording is pinned; the text and HTML parts declare Content-Transfer-Encoding: 8bit, as every Metrics summary carries "·".
- **R20.** (T16) Fix round: the brief's helper check was run and its line pasted; `portChoiceName` drops the alias when it equals the port name, as the backend does (no "Gi1/0/1 (Gi1/0/1)").
- **R21.** (T17) The wizard's job wait checks a mounted ref (no navigate, onError or progress update after the user leaves) and Back is disabled while generating: CLAUDE.md requires async results not to land after the user left, and the Metrics wait stretches to 330 s.
- **R22.** (T19) The list-page dialog shows the scope error, "Checking the scope…" or the preview error instead of the Metrics picker until the scope is ready, though that path is unreachable today: the spec's empty-state rule binds, and the fix protects the path once it is wired. One exhaustive source-label record serves both the select groups and the row label.
- **R23.** (final review) One fix wave took final Minors 1-5, the `report_metrics_data.go` comments, the stale Checklist filter and the UPS-only sites line: each is a line or two and user-visible. Minor 6 (running hot sees only the kept rows) became a known limitation with R14/R15.
- **R24.** (final review) The re-review's residuals were parked as follow-ups (below): the `formatSig` precision floor, two netreport comments and the site Checklists' missing 500 max. Cosmetic edge cases.

On the points the final reviewer declined to judge:

- Report-access viewers and share-link readers see PDFs generated as the owner: the existing sharing model. Accepted.
- Device and site totals count switched traffic on every physical port: the phase 4 rule (spec decision 4). Accepted.
- Heavy Metrics jobs can tie up report workers: bounded by the existing generate limit of 5 a minute. Accepted.
- Hourly chart lines are drawn against a 95th taken from 5-minute data: the spec's resolution split. Accepted.
- Content-Transfer-Encoding: 8bit now also goes on Uptime and Incident emails: correct.
- Ranking for metrics where low is bad, a notice for a too-large scheduled run and a global request-body cap: follow-ups (below).

## Fixed along the way (existing bugs)

- (T12) `pdfText` shared one fpdf translator buffer, so concurrently rendered reports garbled each other's text; a mutex now guards the translation.
- (T12) Every report ended on a blank page holding only "Generated by Sentinel"; the footer now turns the auto page break off while it draws, then restores it.
- (T13) The email's text and HTML parts now declare Content-Transfer-Encoding: 8bit, for Uptime and Incident emails too.

## Fixed in the final whole-branch review

- (T1) `site_ids` is capped at 500 and a role listed twice is refused (R9): a huge request now returns 400, not a 500 from pgx.
- (T7a) The change on a negative previous value divides by |prev|, so −5 → −7 reads −40%, not +40% (dBm-style custom metrics).
- (T10) Values between 0 and 0.01 no longer print as "0" (errors or discards per minute); they show two significant digits.
- (T7a/T11) Running hot prints "-" for a direction with no data, not "0%" (`HotPort` gained `HasIn` and `HasOut`).
- (T11) The tile change caption on rolling periods reads "vs the previous N days" instead of overflowing; calendar periods still name the unit ("vs August 2026").
- (T7a) The `report_metrics_data.go` comments say billable only for an in/out pair, and note that a percent pair's tile can mix directions.
- (T18) The scope picker's Checklist keeps its filter input while a filter is typed, so it can always be cleared.
- (T18) A sites scope with devices but no ports (UPS-only) no longer shows the amber empty line.

## Known limitations

- (R14) Statistics are computed at run time from the 5-minute rollup. Task 9 measured SeriesStats at 387 ms for 100 ports × 30 days (754 ms for 90 days); a monthly 500-port report takes about 4 s with one metric and 15-19 s with four. A year over 500 ports × 10 metrics (about 15 s per SeriesStats, × 2 periods × 10 metrics) reaches the 5-minute job limit and fails with "too large".
- (R15) The 500 cap does not bound the first table's read or the scope totals: a port_roles or sites scope of N ports costs about (N/100) × 387 ms plus 2 × metrics × (N/100) × 80 ms, roughly 50 s at 5,000 ports and 3.5 min at 20,000.
- Running hot sees only the rows the 500 cap kept, so on a capped scope ranked by traffic a slow but busy link can be cut.
- (P3) A sites scope pushed over 500 only by device metrics is capped without the wizard's size line saying so; the PDF's note does.
- (R6) A combined row counts a bucket as covered when any member reported, so a site or device total can dip during a partial outage without the under-90% flag.
- (R7) A combined row's Min and Peak are the min and max of the combined 5-minute values, so a single-series device total can show a lower peak than its own port row.
- Report definitions cannot be edited after creation (unchanged from today).
- Device-profile scopes and running a dashboard as a report are not built (roadmap "Later").

## Deferred follow-ups

- (final review) a too-large scheduled run fails with a log line only; the schedule owner is not told
- (final review) ranking takes the highest 95th even for metrics where low is bad (UPS charge, runtime), and there is no Min column; as the spec defines, an idea for later
- (final review) no global request-body cap on the API (pre-existing and app-wide, as phase 4 found for PUT /dashboards/:id); fix across the API
- (final re-review) a saved report that lists a role twice keeps running: there is no update route and Validate is not on the run path
- (R24) `formatSig` has no precision floor: float residue (e.g. 5.55e-17) can print as a ~20-digit decimal, mainly in the email
- (R24) "billable for traffic" comments remain in `netreport/assemble.go` (:85, :99) and `figures.go` (:142-143); comment-only
- (R24) the Sites and Port-roles site Checklists have no 500 max; the backend's 400 covers it
- (planning) the list page's `GenerateReportModal` never opens (`generateOpen` is never set), so its path is unreachable today; the wizard is the working path
- (planning) the report list shows "Nd window" even for calendar periods
- (planning) `comparisonLabel` names the comparison period in the browser's time zone, not the report time zone setting
- (planning) the shared `useResource` in `usePorts.ts` does not guard against late responses; phase 5 adds a guarded `usePortChoices` for the port picker only
- (T1) no PreviousPeriod case whose previous window itself spans DST (Dec → Nov); the site_ids and roles messages are literals while counts are formatted; `ReportScope.Validate` alone does not check type pairing (`Report.Validate` does)
- (T2) the DB test drives `finishFailed` directly, not a real render; scheduled runs log the raw wrapped error (no classifyJobError)
- (T3) the DST test's `remove_retention_policy` is unneeded (or a comment should say it keeps the test valid after Nov 2027); the log-before-mapping rule has no test
- (T4) `chartResolution` duplicates `PickResolution`'s step rounding and source-to-table mapping; chart test gaps (a partial-member bucket, the Min/Peak definitions, the unaligned test checks only two timestamps); `CombinedSeries` does not document the partial last step
- (T5) hidden and missing subjects differ by about 2 DB round trips (timing; matches the rest of the app); 2 queries per distinct site for non-admins; `ifaceRow.physical` repeats `PhysicalInterfaceIDs`' present filter
- (T6) the 10-metric cap on device defaults is untested; Preview repeats ValidateScope's guard sequence; error tests check Message, not Field; no Capped test on a sites scope; port_roles choices are not asserted port-only
- (T7a) keepBusiest's name and subject-id tie-breaks are untested; no test that a single bps metric has no billable column
- (T7b) no Build test drives a real statement timeout; no Build test of a sites scope with a device metric (instance lines, unranked device subjects in the cap)
- (T8) the missing-builder test asserts only err != nil and the fake builder ignores its report argument; an unwired builder is retried rather than failed (main.go always wires it)
- (T9) the estimate line counts SeriesStats only (about 20% under with CombinedStats; assumes linear, warm-cache cost); CombinedSeries is checked only for non-empty; the CombinedStats check skips Expected
- (T10) `drawLineChart` is not defended against Hi == Lo, Ticks == 0, a nil TickLabel or non-finite values (no caller reaches them); edge-case tests missing (nil Ref, empty, single-point or flat line, unit boundaries, NaN); `formatDurationHM` returns "0 m" for NaN against the file's "-" rule
- (T12) table test precision gaps: row order is unchecked, some figures also appear on tiles, one chart per RowCharts entry is not counted, and the NoData test does not check that row charts are absent
- (T13) the bare "No data for this period" fallback is untested; a single traffic metric does not name its direction in the email
- (T14) the `field` key is never asserted; no direct test that uptime and incident reports skip ValidateScope; binding-error 400s expose the Go struct name and carry no field; the rate-limit test is timing-sensitive
- (T15) the simulator port from Atoi is not range-checked (ParseUint 16); a poll that writes nothing fails later with a NoData message; only Avg is asserted (not P95, Peak or the second row's name); the refresh comment is inaccurate
- (T16) no retry after a preview error (a 429 or a blip sticks until the scope changes); the dropped-metrics note does not persist across answers; NETWORK_SCOPE_TABS repeats SCOPE_TYPE_LABEL; REPORT_ROLES wording differs from PORT_ROLE_LABEL (spec wording); `reportScope.ts` imports `describePeriod` from a component file
- (T17) the dialog's generated Metrics name can exceed `reports.name` VARCHAR(255) with three long names (unreachable today; monitor names share the risk); "Sites: HQ, Annex: Metrics Report" reads awkwardly; `waitForReportJob` keeps polling after the user leaves (results ignored, no cancel); the uptime/incident payload-equivalence check was not committed
- (T18) the Ports tab shows no reason when /devices fails to load, and load errors look like empty lists; no retry after a ports load error; filter inputs lack labels, groups lack fieldset/legend and "Sizing the scope…" lacks aria-live; the dialog shows a pending or failed check twice; a chosen device dropping out of the 30 s refresh names new picks "Device · …"; the Checklist's port-name fallback differs from `portChoiceName`
- (T19) raw unit tokens are shown (e.g. "per_min")
- (T20) report pages poll /devices every 30 s while a Metrics report is listed, and useSites runs on every Reports visit; `describeScope` prints "0 ports" without scope_data (unreachable with this backend; fall back to SCOPE_TYPE_LABEL); the tests use an uptime report, not a metrics scope; the public rule hangs on `shareToken == ""` (an explicit flag would be sturdier)
