# Site maps (phase 6 piece C) — follow-ups

Spec `specs/2026-10-19-site-maps-design.md`, plan `plans/2026-10-19-site-maps.md`. On `dev` since b33bfdc. The calls made while building it, and the final review's minor findings left for later.

## Deferred minors (final review)

- Placements was inserted inside SetName's doc comment (endpoint_service.go)
- a hidden link reappears when its ends change (exact-key match, not fits())
- adding the same hand-drawn link twice / double Keep makes two rows, so Delete seems not to work
- carried-forward rates and the stored-rate fallback are stamped "now", so old figures can look live
- no stored-rate fallback after the first tick for links whose device isn't fast-polled yet
- the poller's per-site/per-port maps are never pruned
- the busy claim can't trigger; no coordination with the regular poller's in-flight set; ~14 GETs not "one request"
- a public stream sends one extra map frame after a revoke before its recheck ends it
- no cap on concurrent public streams
- folding an access switch leaves empty rows in its column
- the widget blocks page scrolling on phones (touch-none)
- a failed drag save leaves the dropped position on screen until the next map
- "Show on site map" doesn't centre on the device, and shows nothing special for a hidden one
- guesses also use "behind" placements and show for 7 days, where the spec says edge ports seen in 24 h

## Calls made while building

- **Task 1:** added site_map_positions.has_position — a row can exist only to mark a device hidden or added, and reset must keep those flags while clearing the place — cost if wrong: one column
- **Task 4:** one ErrSiteMapInvalid for every bad edit (plan named ErrSiteMapBadLink and used it for a device add too) — clearer for the API's 400 — cost if wrong: a rename
- **Task 4:** Connected counts come from one Placements(nil) call per snapshot (all placed MACs), not ForDevice per switch — ForDevice reloads the whole L2 world per call, 20x for a 20-switch map — cost if wrong: count differs slightly from the Connected section (it counts wired+wireless+behind MACs on that switch, minus map devices' own MACs)
- **Task 4:** NewSiteMapService takes no PortService (the plan listed it); the public ports route uses PortService in the handler — cost if wrong: none
- **Task 4:** one guess per unlinked switch (its most recent placement), so a switch with several interface MACs can't sprout several guesses — cost if wrong: a missed second cable to a non-LLDP switch
- **Task 4:** added OnMap(site, device) for the public ports route's check — cost if wrong: none
- **Task 5:** BoundPort gains InterfaceID (and the map's port rows their id) — Latest's stored-rate fallback needs interface ids for LivePorts — cost if wrong: none
- **Task 5:** exported Tick(ctx) beside Run, so tests drive ticks with a fake clock — cost if wrong: none
- **Task 5:** PortMonitor keeps learnedHC under its mutex (the poll writes ds.hc unlocked); SNMPv1 devices are fast-polled on 32-bit counters without waiting, as the regular poll never learns hc for them — cost if wrong: none
- **Task 5:** the simulator test is a plain test (no database), link_poller_sim_test.go, on ports 1 and 2 (the sim has 3 and 48 down) — cost if wrong: none
- **Task 6:** the stream tests use fakes in site_map_handler_test.go (no database), not site_map_stream_db_test.go — access and revocation are decided by the store and site checker, which the fakes stand in for; the real ones are covered by Task 4's DB tests — cost if wrong: none
- **Task 6:** the handler is an exported SiteMapHandler (NewSiteMapHandler, PublishTraffic, Changed) so main wires the hub once for both route sets — cost if wrong: none
- **Task 6:** exported dashboards.TrimPortForPublic for the public ports route, and its own "This map link is no longer available" message — cost if wrong: none
- **Task 7:** devicePoller and portMonitor are built before the dashboard registry in main.go (configured and started where they were), because the widget needs the fast poller — cost if wrong: none
- **Task 7:** LinkPoller.SetPublish, since the stream handler is built after the poller — cost if wrong: none
- **Task 7:** widget test is a plain test with a fake reader (widget_site_map_test.go), not a DB test; the widget blanks hidden/candidate lists and CanEdit — cost if wrong: none
- **Task 8:** tooltip end naming — a switch end reads "port N", a router's or other device's its ifName ("eth10"), which yields the spec's exact example text — cost if wrong: one helper
- **Task 8:** tooltip is told from the rate end (the spec's example puts QuantumLink first): ↓ = arriving at the rate-end device, ↑ = leaving; plan's test figures (in 38/out 412) corrected to in 412/out 38 to fit — cost if wrong: arrow meaning
- **Task 8:** layout returns width too (more than 3 distribution columns widen the map), routePath gains a 'chain' kind for daisy chains, Placed gains `below` for "▾ N below", and speedText/endText helpers — cost if wrong: none
- **Task 9:** LiveState adds 'connecting' before the first frame; useSiteMap returns no at in LiveState (the traffic carries its own time) — cost if wrong: none
- **Task 7:** public maps show stand-in ids (uuid.NewSHA1(site, device)), not real ones — the dashboards' leak guard treats device/site ids as secrets on public links; PublicTraffic rekeys traffic, PublicDevice resolves a stand-in for the ports route (OnMap removed) — cost if wrong: none (guard test now covers site_map, asserting state ok)
- **Task 10:** link ends attach by a new data-if-index on each drawn port (PortHit), not the aria-label text — exact for stacks and module ports — cost if wrong: none
- **Task 10:** unitPorts moved from RealisticFaceplate into drawings/kit.ts and shared — cost if wrong: none
- **Task 10:** an unknown link speed draws dashed along its length ("8 5"), not only 14 units at each end — simpler, same signal — cost if wrong: a styling tweak
- **Task 10:** link tooltips are native SVG <title>s — accessible, no tooltip component — cost if wrong: look of the tooltip
- **Task 10:** Keep/Hide/Change ports/Delete show for can_edit users whether or not Edit mode is on (as the mockup); dragging needs Edit — cost if wrong: one condition
- **Task 10:** the fast poll's device statuses override the snapshot's in the canvas (fresher) — cost if wrong: none
- **Task 11:** the map page, its wall display and the public map share one SiteMapView (toolbar, edit bar, side panel, legend); the public page is always the wall-display frame — cost if wrong: none
- **Task 11:** the site card hides itself when the site has no routers or switches, and (being live) counts as watching the map while the site page is open — cost if wrong: a little extra fast polling while a site page is open
- **Task 11:** view and folds are remembered via hooks/useSiteMapPrefs.ts (sentinel:map-view, sentinel:map-folds keyed by site id or public:<token>) — cost if wrong: none
- **Task 13:** fold chips only on distribution and access switches — folding at the router or core folds the whole map (spec said "a switch with others below it") — cost if wrong: one condition
- **Task 13:** the site page's card and the device page's "Show on site map" are checked by the gate (tsc) and on the sandbox, not in the harness (SiteDetail/DeviceDetail need dozens of mocked endpoints); SiteMapCard itself is checked in the harness — cost if wrong: a broken placement found on the sandbox
- **Final:** crossed cabling when neither port matches — the data can't settle it; links stay device-level — cost if wrong: a pair drawn crossed
- **Final:** hand-drawn/hidden rows go stale if a device renumbers its ifIndex — inherent in the spec's data model — cost if wrong: an editor redraws a link after a firmware change
- **Final:** 32-bit counters on 10G ports can wrap more than once in 10 s — ComputeRates rejects implausible deltas, so the reading is skipped — cost if wrong: a gap on such ports
- **Final:** a switch retyped to an AP keeps its position row — same site, harmless, used again if retyped back — cost if wrong: one stale row
- **Final:** the public map shows models, uptime, port names and link sources — not on the spec's leave-out list — cost if wrong: the owner may want them hidden too
- **Final:** the public ports route exposes what public dashboards' trimPort already allows (aliases, far-end names, conditions) — same policy — cost if wrong: one shared policy change
- **Final:** layout() is about O(n²) — fine at site sizes — cost if wrong: slow maps past a few hundred switches
- **Final:** the flow animation jumps slightly at each loop — cosmetic — cost if wrong: a visual tick
- **Final:** no CSRF tokens on the edit routes — they follow the app's existing cookie-auth pattern — cost if wrong: same as the rest of the app
