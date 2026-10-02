# Network phase 3: rulings and deferred follow-ups

This records the decisions taken while executing `2026-10-01-network-phase3-mibs-custom-metrics.md`, plus the review findings deliberately left for later. Merged to `dev` on 2026-10-02.

## Rulings

- **No Cisco MIB files are bundled.** Cisco's MIB repository has no licence and the files say "All rights reserved". Only the 14 IETF/IANA modules are embedded; the Cisco switch health profile uses numeric OIDs, and the MIB library page links to the 8 Cisco files an admin can upload for names in the browser.
- **gosmi does not fail on missing imports**, so "waiting for" is computed by Sentinel from each module's IMPORTS; syntax errors come from `gosmi/parser.Parse`, which reports the line.
- **ENVMON `notPresent` counts as OK** for fans and power supplies (an empty redundant slot must not page).
- **Metric preview needs editor access** to the device's site (the spec), not readonly as the plan said.
- **A profile is due up to 15 s early**, so poll-clock jitter cannot push a 1-minute profile to every other minute.
- **The 20 MB upload limit is one budget per upload** across all files and zip entries (the plan's code reset it per zip).
- **A copied profile starts with no match prefixes** and says so on its page: following "copy the built-in and edit it" must not poll and alert twice on every matching device.
- **A row that disappears for good keeps its open incident** (spec: a missing row keeps its state). The Health section shows it as "No reading" with the problem flag; pausing the device closes it.
- **Modules whose OID assignments form a cycle are refused** (gosmi would hang on them).

## Fixed in the final whole-branch review

Stale series-id cache after deleting a metric/profile (data loss when a key returned); counter rates on device reboot and scale applied after the rate (sysUpTime now reaches the profile poll); copies keeping match prefixes; Health hiding open problems without a reading; tests for a failed incident open and a restart mid-incident; starter seeding after a rename; metric key registry after an in-app restore (unknown metric points are now skipped, not fatal to the batch); panic recovery around MIB parsing; zip paths in duplicate-module errors.

## Known limitations

- A row that vanishes for good (a removed fan module) keeps its alert open until the device is paused or the rule/profile changes.
- MIB browse/search endpoints are open to every signed-in user (MIB text is not sensitive); library changes are admin-only.
- The preview-before-save requirement is enforced in the UI only.

## Deferred follow-ups (minor review findings)

- (T1) `definitions` regexp counts DEFINITIONS ::= BEGIN inside -- comments (could misreport ErrSeveralModules)
- (T1) ParseError position regexp best-effort; other participle error shapes give Line 0
- (T1) a module gosmi fails to load is silently skipped (ready but no objects, no diagnostic)
- (T1) Build lets the later of two files declaring the same module win silently (Task 3's upload rejects duplicates and the DB keeps one row per name)
- (T2) no pure unit test for StringArray/Int64Array/EnumMap Scan/Value (reviewer verified against the real driver)
- (T3) no DB test for "upload replaces a built-in; SyncBuiltins keeps it; deleting it restores the built-in"
- (T3) Upload rebuilds even when every file was skipped as non-MIB
- (T3) List selects m.* (pulls content column) though Content is never serialised
- (T4) HasChildren EXISTS ignores module status (dead expand arrow if only waiting-module children)
- (T4) LIKE wildcards % and _ in search text are not escaped (over-match, not a security issue)
- (T4) no handler test for by-oid 404 or the 413 MaxBytes path
- (T6) TestDBMetricsQueryOldShortRangeUsesRollups (phase 2) flaked once in a full DB run — timing-dependent rollup test; watch in later full runs
- (T6) ValueOf's printable check decodes bytes as UTF-8, so binary that forms valid UTF-8 shows as text (plan-mandated code)
- (T6) no test for a multi-arc table index in test-walk
- (T6) ReadColumns drops individually refused scalars without an error entry (mirrors GetEach)
- (T7) CreateMetric position from a non-transactional count (concurrent creates may share a position)
- (T7) UpdateMetric persists Position from the PUT body (frontend must round-trip it)
- (T7) TestProfileCreateMetricValidation only checks a non-empty 400 body
- (T7) List counts matching devices per profile in Go, not SQL
- (T8) profileRouter test helper grew a previewer parameter (nil at 3 call sites)
- (T8) TestWalkDevice/PreviewDevice repeat a 3-line TargetFor block
- (T9) no pause test that a metric incident closes via CloseDeviceConditionIncidentsTx
- (T9) metric incident open/close/active trio mirrors the UPS and port trios (third copy of the pattern)
- (T10) 15 s due slack lets a 1-min profile run every poll on devices polled every 45–59 s (harmless; comment it)
- (T10) the 30 s budget bounds only SNMP reads; DB writes/notifications use the poll context (matches UPS/Port monitors)
- (T10) lastState replaced each poll, so a row absent one poll loses "(was X)"
- (T12) a failed tree expand (fetch error) looks the same as a node with no children
- (T13) Profiles/ProfileDetail lack the "Only administrators…" guard sibling admin pages show (backend still enforces)
- (T13) preview shows state as plain text, not a chip
- (T13) TestWalkPanel's kind= query param is unused (kind re-derived from the MIB object)
- (T14) EditDetailsModal profileBusy is one id, so rapid edits of two profiles re-enable the first select early (converges correctly)
- (T14) device page ignores useDeviceHealth's error (Health just hides), as with ports
- (T14) Health chip is amber for any not-OK row whose rule hasn't fired, as well as not-OK with no rule
