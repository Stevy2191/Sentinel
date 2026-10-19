# Monitoring as one list — design

**Date:** 2026-10-19
**Status:** approved by the owner 2026-10-19, with the mockup
**Source:** the owner, 2026-10-19: "the monitoring page is hard to navigate with it broken up into monitoring, servers and devices. Monitoring should just be everything and then I can filter down myself. Can we just combine everything into one list and then make it to where I can just filter anything I want?" The owner skipped the questions, so every choice below not in that quote is **my call**, marked as such.

## Why

The Monitoring page today (`frontend/src/pages/Monitoring.tsx`) stacks three sections, Uptime checks, Servers and Devices. Each has its own table, columns, sort and header filters (check type and tags on uptime checks, device type on devices). Type chips (All / Uptime / Servers / Devices) pick which sections show.

Finding one thing means knowing which section it lives in, then scrolling past the others. The filters also only half-combine: some apply everywhere, some only inside one section's header. The owner wants **one list of everything Sentinel watches**, narrowed with filters they pick freely.

## Goals

- **One list:** every uptime check, server agent and network device as a row in a single table, with one set of columns, one sort and one set of filters.
- **Filter anything:** every filter works on the whole list and combines with every other. Filters that only make sense for one kind of row (check type, tags, monitor group) narrow the list to that kind, and say so.
- **Filters live in the address,** as today, so Back, bookmarks and links into the page keep working.
- **Nothing is lost:** every row action, the group features, the summary strip and the Add menu stay.

## Non-goals

- No backend change. The page already loads all three lists (`useAllMonitors`, `useAgents`, `useDevices`) and filters in the browser. That's fine at this scale: hundreds of rows, not tens of thousands.
- No saved views or named filter sets. That's a natural follow-up once the filters settle.
- No bulk actions (pause or delete several at once).
- The detail pages (monitor, server, device) don't change.

## Anti-goals

- **A row that needs a click to tell what it is.** The kind (uptime check, server, switch…) must be visible on the row itself.
- **A filter that silently does nothing** to some rows. If a filter only applies to uptime checks, choosing it shows only uptime checks, and the filter bar says so.
- **Breaking existing links.** `/monitoring?show=devices`, `/uptime`, `/servers`, `/network/devices` and the back links from detail pages must still land on a sensible view.

## The list

One table. **My call:** these columns, fitted to all three kinds:

| Column | Uptime check | Server | Device |
|---|---|---|---|
| **Name** (2nd line muted) | name; URL or host | name; hostname · OS | name; host · vendor model |
| **Kind** | "HTTP check", "Ping check"… | "Server" | "Switch", "Router", "Access point", "NVR", "UPS", "Other" |
| **Site** | site or — | site or — | site |
| **Status** | the shared status pill: Up, Down, Pending, Paused, Maintenance, Error |||
| **Health** | response time (ms) | CPU · memory · disk, coloured as today | system uptime ("up 12 d") |
| **Uptime** | 24 h uptime with today's strip | system uptime | 30-day availability bar |
| **Last seen** | last checked | last report | last seen |
| **Actions** (row menu) | today's monitor actions (pause/resume, group, edit, delete) | install instructions, unregister | open, edit (admins) |

- Clicking a row opens its detail page, as today.
- Each kind keeps a small icon in the Name cell, so the kinds are easy to tell apart when scanning.
- **Phone width:** rows become two-line cards: name, kind and status on top; site, health and last seen below.

## Filters

One filter bar above the list. Every filter applies to the whole list, and filters combine with AND. Within one filter, any chosen value matches (OR).

| Filter | Values | Applies to |
|---|---|---|
| **Search** | text in name, address/host, hostname, IP, vendor/model, site name | all |
| **Kind** (multi) | Uptime checks (with HTTP, DNS, Ping, TCP, Webhook under it), Servers, Devices (with Switch, Router, Access point, NVR, UPS, Other under it) | all |
| **Status** (multi) | Down, Up, Pending, Paused, Maintenance, Error | all |
| **Site** (multi) | each site, plus "No site" | all |
| **Monitor group** | each group, plus "Ungrouped" | uptime checks only |
| **Tags** (multi) | the monitors' tags | uptime checks only |

- **My call:** Kind, Status, Site and Tags are multi-select. Today each is single-select; the owner asked to "filter anything I want", and "Down or Pending at these two sites" is the case single-select can't do.
- **Kind is one tree:** choosing "Uptime checks" means every check type; choosing "HTTP" alone narrows to HTTP checks. It replaces today's type chips, the check-type select and the device-type select.
- **Uptime-only filters:** choosing a monitor group or a tag narrows the list to uptime checks, and the bar shows it, e.g. "Group: Web servers · uptime checks only".
- **Active filters show as removable pills** under the bar, with **Clear all**. The summary strip (Watching / Down / Paused) stays, and clicking Down still filters to Down.
- **Counts:** each Kind value shows how many rows it has ("Switch 3"), counted over everything else that's filtered, so the next click's result is predictable.

## Sort and grouping

- **Sort:** click a column heading, click again to reverse. The default is **Down first** (needs-attention states up, Paused at the bottom, then name), as uptime checks sort today, now applied to every row. **My call:** status, then name, is the sensible "down first" for servers and devices too.
- **Group by:** a select with **None** (default: one flat list), **Site**, **Kind** and **Monitor group**. Grouping gives collapsible headings with a count and down count. Under **Monitor group**, servers and devices go in an "Other" heading. Each group keeps its uptime figure and edit button, as today.
- **My call:** the default is None, because the owner asked for one list. Group by is remembered in this browser, like the collapsed sections today.

## Addresses and old links

- **New parameters:**
  - `kind` (comma-separated: `uptime`, `servers`, `devices` or the leaf values `http`, `ping`, `switch`, `ups`…)
  - `status`, `site` and `tags` (comma-separated)
  - `group`, `q`, `sort`, `by`
- **Old addresses keep working:** `show=uptime|servers|devices` reads as `kind=` that value. Old `type=` (check type) and `device_type=` read as the matching leaf kind. `/uptime`, `/servers` and `/network/devices` redirect as today.
- **Back links:** `monitoringPath('devices')` and the other callers of `monitoringPath` keep their meaning (they now open with `kind=` set). The back links from detail pages return to the list exactly as it was left, which is already how navigation history works.

## What goes away

- The three section shells, their collapse toggles and their header controls.
- The type chips. The **Kind** filter replaces them.
- The per-section header stats ("Avg 142 ms · 3 incidents (30 days)"). **My call:** drop them. The Overview page already shows these, and a mixed list has no single average. If the owner misses them, they can go in the summary strip.

## Empty and error states

- **Nothing watched yet:** the same message as today, pointing to Add.
- **Nothing matches the filters:** "Nothing matches these filters" with Clear all, as today.
- **One source failed to load** (say the device list): the list shows the rest, and a line above it says "Devices couldn't be loaded — Retry". It never shows a quietly shorter list.

## Testing

- `monitoringView.ts` keeps its pure, separately testable shape. Unit tests cover the cases that matter:
  - parsing old addresses (`show=`, `type=`, `device_type=`) into the new filters
  - Kind tree matching (parent versus leaf)
  - any-of within a filter and all-of across filters
  - uptime-only filters narrowing to uptime checks
  - the Down-first sort across kinds
  - the counts shown on each Kind value
- Headless checks in the harness, with a mocked API:
  - a mixed list
  - Down at two sites
  - HTTP checks only
  - Group by site
  - a phone-width view
  - an old `?show=devices` link
- **The frontend gate** passes, and the change is pushed to `dev` and the sandbox, as usual.

## Left to the builder

- The exact component split. A shared row component per kind feeding one table is the obvious shape, reusing today's `MonitorRowActions`, the server row's metrics hook and the device row cells.
- The multi-select control's look, within the design system (`rd-select` style, slate, the 8 accents in `utils/colors.ts`).
- Debounce and history behaviour for search, kept as today: typing replaces the address entry, other filter changes push one.
