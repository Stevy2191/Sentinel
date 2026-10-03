# Network phase 4: rulings and deferred follow-ups

This records the decisions taken while executing `2026-10-02-network-phase4-dashboards.md`, plus the review findings deliberately left for later. Done on `feature/network-phase4`, awaiting merge to `dev`.

## Rulings

Deviations from the spec, decided while planning:

- **The code lives in `internal/dashboards`**, not `internal/services/widgets/`: a `services` subpackage would make `services` import its own child (import cycle).
- **Site-wide UPS status is composed inside the `site_power` widget** from `DeviceService.List` and `PortService.UPSStatus`, instead of a new `PortService.SiteUPSStatus`. Same data, no new method.
- **`monitors` bars with the 90d window refresh every 15 minutes** (daily buckets change once a day); list and 24h bars keep 30 s.
- **`device_table` and `event_log` have their own compact renderers**, and `HealthSection` and `UPSPanel` each give up a presentational piece (`HealthRowTile`, `UPSReadingTiles`) so widgets avoid the charts that call logged-in APIs.
- **Public responses keep the logged-in JSON shapes** with sensitive fields blanked, so one renderer serves both views.
- **Leaving the editor with unsaved changes is confirmed in-page for Cancel only**; reload/close gets the browser warning; in-app navigation is not blocked (`<BrowserRouter>` cannot use `useBlocker`).

Decisions taken while executing:

- **R1.** Checks run with the scratchpad helpers (go-docker, go-test-db, fe-gate) because shell functions do not persist between subagent commands; same images and commands.
- **R2.** Task 1 omits the unused `ptr` fixture helper (YAGNI).
- **R3.** Commit trailers name the model that wrote the code, not the plan's literal Opus line (the phase 1 rule).
- **R4.** Per task: focused tests, vet, the no-database suite and the touched packages' DB tests; the full DB suite (about 6 min) runs at Tasks 14 and 19 and before the final review, to avoid hours of identical reruns.
- **R5.** (T3) A broad scope always resolves, even with only hidden explicit subjects, as spec section 2/3 promises.
- **R6.** `go fmt` runs from Task 6 on; earlier gofmt-only changes went in a separate `style(dashboards): gofmt` commit.
- **R7.** (T6) Hidden and removed subjects give one identical save error so dashboards cannot be used to probe ids (spec section 2).
- **R8.** (T6/T9) `Resolver.Preview` refuses a config with hidden or removed subjects with the same FieldError as save; otherwise preview is the same id oracle.
- **R9.** (T9) The `DoChan` panic path is fixed now (recover in the flight closure): it was reachable from the unauthenticated public route.
- **R10.** (T10) Non-admin dashboard routes trust the JWT `is_admin` claim (a demoted admin keeps visibility up to 24 h), as monitors and devices do; public links re-check the DB.
- **R11.** (T15) Site-total and site_traffic return `ErrNoData` unless the site is in `in.Visible.Sites`; the widget must not rely on the resolver alone.
- **R12.** (T17) top_n (site), event_log and open_incidents take subjects from `in.Visible` and return `ErrNoData` when empty; an empty filter would list every incident, and a public viewer reads as admin.
- **R13.** (T16) Public port_grid keeps `Descr` and `NeighborDeviceName`: names stay per spec, and the device ports page already shows both.
- **R14.** (T16) Added a trimPort unit test (the only guard on a privacy guarantee) and made `devices.Get` map only `ErrDeviceNotFound` to `ErrNoData`.
- **R15.** (T19) The public leak test links the port to the UPS as an uplink so the neighbor-id trim is exercised end to end.
- **R16.** (T19) The leak test asserts widget count equals registered types, has a positive control (device name present) and seeds a failed check with an error message, so a vacuous pass is impossible.
- **R17.** (T20) The package-lock churn is accepted (node:20-alpine npm drops `libc` fields; only react-grid-layout and its 4 deps were added; both Dockerfiles use the same npm).
- **R18.** (T20) Widget components treat every array in widget data as possibly null (`?? []`): Go nil slices marshal as null.
- **R19.** (T20) Hook state resets on input change (useDashboard, useDashboards, useDeviceMetrics) so A is never rendered under B's URL.
- **R20.** Browser checks deferred to one pass after the final review (no browser in the session); closed by R28.
- **R21.** (T23) Interim state accepted: the picker offers every type while only label has a form until Tasks 24-27; all forms land before any merge.
- **R22.** (T23) Preview keyed by widget, the 400 highlight clears on edit, and the grid sets `maxH` 24.
- **R23.** (T26) The one-line `key={widget.key}` fix was verified by the controller, not re-reviewed.
- **R24.** (T28) A busy flag disables the public-link buttons during an action and hides the old URL while regenerating, so a dead link is not copied to a wall screen.
- **R25.** Task 31 runs after the final review and fix wave; STATUS says "awaiting merge to `dev`" rather than a merge date, since merging is the owner's call.
- **R26.** The final fix wave took I1-I3 and M1-M6, M8-M10, T12 and T24 (all user-visible or one line); I1 sums only additive units, averages the rest, and counts physical interfaces only for device port totals. #7, #11, #12 and the follow-up rows were left.
- **R27.** The health-check timeout (AbortController plus 10 s timer, as Vite's target predates `AbortSignal.timeout`) was fixed and controller-verified; the per-metric interface filter and the mixed-unit release note go to this doc.
- **R28.** No browser tool was available, so the visual and browser checks became owner checks (STATUS items 5-7).

## Fixed in the final whole-branch review

- (T15) Device and site totals summed non-additive metrics (%, °C, V); now only additive units (bps, per_min) are summed, the rest averaged, and device totals of port metrics count physical interfaces only. The stat form clears a non-port metric when switching to a site total (T24).
- (T27) Healthy agents (`active`) showed a grey dot; agents now have their own colour map.
- (T29) The in-app fullscreen display ended at the 6-hour reload; display mode now lives in the URL (`?display=1`).
- The 6-hour reload first checks `/api/health` (aborting after 10 s) and retries if it fails.
- A kiosk/F11 hint by the Fullscreen button and on the public page.
- 60 s timeout on the public axios calls.
- (T17) The event log missed the closing of an old incident; it orders by `COALESCE(end_time, start_time)`.
- (T24) Mixed units on one chart: Validate rejects them and the picker filters by unit.
- The monitors-widget bars spammed the log through `GetChecksInRange`; a non-logging variant is used.
- The audit log now distinguishes a regenerated link from a new one.
- (T22) The range override goes to time-based widgets only.
- (T23) `beforeunload` sets `returnValue` for older browsers.
- (T12) The TopN ORDER BY tie-break is total.

## Known limitations

- In-app navigation away from the editor is not blocked (the app uses `<BrowserRouter>`; `useBlocker` needs a data router). Cancel confirms in-page and reload/close warns.
- A logged-in fullscreen display drops off when its 24-hour session expires; the public link is the way to run a TV for days.
- Browser fullscreen cannot survive the 6-hour reload (the Fullscreen API needs a user gesture); use kiosk mode or F11 for a TV.
- `useDashboards` on the Overview loads every visible dashboard just to count them.
- Release note: a timeseries widget saved before the mixed-unit rule that mixes units still renders, but the dashboard's next save is refused on that widget ("choose metrics with the same unit") until one metric is unticked.

## Deferred follow-ups

- (final review, #7) a dead link still looks live after its creator is demoted: `PublicLink` and `published` still report it, so admins see a URL that 404s; fix: return `active` in LinkView
- (final review, #11) the logged-in view never refetches its layout; widgets another editor adds or deletes appear only at the 6-hour reload (the public page polls every 5 min)
- (final review, #12) `/devices/:id/metrics` inherits the pre-existing device-route id oracle (`loadDevice` answers "device not found" vs "site not found"); fix every device route together
- (final review) a device chart mixing a port metric with a same-unit profile metric (e.g. Busy in % + CPU %) skips the physical-interface filter, so the port metric averages over Vlan and Port-channel interfaces (totals.go:31); filter interfaces per metric
- (final review) the metrics API's `agg=sum` (api/network_handler.go:175) still adds up any unit; no frontend caller uses it today
- (final review) PUT /dashboards/:id has no body-size cap, like the other JSON endpoints; fix across the API
- (final review) no backup/restore test with the new tables; no backup code changed, so a restore smoke test on the work install is enough
- (final review) several TVs reloading together can hit the 120-request burst and recover by backoff
- (T1) schema tests assert only err != nil; no per-column CHECK tests
- (T2) no test pinning canShare for admin on a site dashboard (shareable refuses first); access_test gaps (LevelNone, public+admin viewer)
- (T3) Filter double-counts duplicate ids (validators dedupe); no fake-Checker unit tests; missing-site and mixed hidden+removed untested
- (T4) no wrong-shape or boundary config tests (behaviour is correct); labels allow bidi/zero-width characters (rendered as text, no XSS)
- (T5) over-length name test uses NULs; untested: description >500, list site filter, public viewer, cascade; Delete checks access and deletes in two statements
- (T6) the publish-vs-save re-check runs outside the row lock (ms window); the 16 KB limit is checked on input, not the normalised config; one write per widget
- (T7) sharing negative tests missing
- (T8) the injection test input is too short to reach SQL; no base64url charset check before the DB; PublicLink/Revoke non-admin tests missing; add a "never serialize" doc comment on PublicWidget
- (T9) no cache re-check inside the flight; the shared *Response must stay read-only; Filter errors unwrapped; the R8 test covers devices only; O(n) cache sweep
- (T10) handler test gaps (share errors, readonly preview 403, 404 kinds); wid parsed before token
- (T11) DeviceMetrics test gaps; two passes over the series
- (T12) TopN test gaps (utilisation max, 1h source, rollup errors, direction)
- (T13) no non-admin narrowing test for the new incident filters
- (T15) deviceNames swallows Get errors (leading " · "); time.Time map key; deviceNames N+1 (up to 50); no instance-length cap (bounded by 16 KB)
- (T16) device_health positive/public and port_grid empty-path tests; one failing UPSStatus fails the whole site_power card list
- (T17) empty-Visible guard tests cover only open_incidents devices; device_table `List(uuid.Nil, true)` trust comment
- (T18) the public monitors test reuses Visible; the deleted-subject skip, list style and Validate rejections are untested; 90d bars load every check for 90 days per monitor (15 min refresh)
- (T19) starter widgets are validated before the transaction; the starter test checks presence only; the sim test discards Atoi errors
- (T20) a 15 s interval runs per widget; permanent 404/403 responses are retried forever with backoff; a device-metrics failure shows as "no metrics yet"; one render of old state on an input change
- (T21) the new-dashboard modal navigates after a mid-request close; the Overview count is not refreshed
- (T22) lastSuccess carries across a source change
- (T23) Reload after a 409 prompts beforeunload twice; DashboardEditor has no key; possible spurious dirty if the grid compacts a gapped layout (owner check 5)
- (T24) InstancePicker has no 200 cap; the stat InstancePicker is unlabelled
- (T26) blank Address cell for an empty host; EventLogWidget hand-formats dates; an empty number input becomes 0 (the backend defaults it)
- (T27) status colour maps are local per widget (how the agent-dot bug slipped in); no paused label in bars style; no client hint for "at least one monitor or server"; an empty list renders an empty `<ul>`; the picker cap is a literal
- (T28) "Copied" never resets; a load failure has no retry button
- (T29) visibilitychange can overwrite a held wake lock (hidden tabs auto-release); lastUpdated is not reset when re-entering display
