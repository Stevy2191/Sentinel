# UX piece 2: deferred follow-ups

This records what was deliberately left for later while executing `2026-10-07-ux-piece2-monitoring-list.md` on `feature/ux-piece2`, and the rulings made at the final review.

## Fixed: `GET /monitor-groups` returned every grouped monitor to any signed-in user

`GET /monitor-groups` returned every grouped monitor — including `headers` and `body`, which can hold credentials — to any signed-in user, regardless of ownership or sharing. Fixed on `fix/monitor-group-access`: each group now lists, counts and averages only the monitors the caller can see (admins see all). The same fix makes `POST /monitors/:id/group` require edit access to the monitor; before, anyone signed in could regroup any monitor. Tests: `TestDBMonitorGroupsShowOnlyVisibleMonitors`, `TestDBMoveMonitorToGroupNeedsEditAccess`.

Still open, by design for now: monitor groups themselves are global, so any signed-in user can create, rename, reorder or delete a group (deleting one ungroups everyone's monitors in it).

## Rulings (declined to judge in this piece)

- `PUT /monitors/:id` always applies `enabled`, so a script that sends only `site_id` pauses the monitor. The edit form sends the whole monitor, so the UI is unaffected.
- Overview, Incidents and the report pickers still load only the first 50 monitors (`useMonitors`), so Overview's count can disagree with Monitoring above 50 monitors.
- Every monitor row fetches its own sparkline, so a large list makes many requests; a batched sparkline endpoint would fix it.
- The server detail page does not show the site.

## Deferred minors

Backend:

- `monitor_agent_site_db_test.go` sums two counts, so a failure does not say which row lost its site.
- A bad `site_id` on monitor create/update is rejected by the first bind into `models.Monitor` with the raw decoder message, so `parseSiteField`'s own error text is unreachable there.
- Monitor update reads the monitor twice (`GetMonitor`, then `UpdateMonitor`).
- A site deleted between the existence check and the save gives a 500 (FK) instead of a 400.
- Other test assertions are loose: the clear case is checked only on the PUT response, and the unchanged-hidden-site test does not check `site_name`.
- `CreateAgentHandler`: `labelAgentSites` sits under the comment that describes the token response.

Frontend:

- MonitorForm's Site help line is long; `SiteSelect` briefly shows "No site" while sites load; the Edit server modal passes no `currentName` (admins see every site anyway).
- Tags are not de-duplicated case-insensitively; the slowest and uptime sorts put never-checked monitors last (undocumented); an unchecked `as` cast on `VIEW_STATUSES`.
- Monitor-group load errors are dropped, and monitors whose group is not loaded render nowhere (pre-existing behaviour).
- Search text typed less than 300 ms before Clear filters or Back can be written into the new history entry.
- No late-response guard on the install instructions (two quick clicks).
- The Servers header shows offline twice (down badge and summary).
- Non-admins see Servers 0 and Devices 0 chips that lead to "Nothing matches".
- Every poll re-renders every monitor row (inline callbacks).
- The add-device modal can pre-fill a site from the filter that the user cannot add to.
- "Nothing is being watched yet" renders as a card below the strip, not beside + Add.
- The Refresh button spins only during the first load and while a manual refresh runs, not during background polls.
