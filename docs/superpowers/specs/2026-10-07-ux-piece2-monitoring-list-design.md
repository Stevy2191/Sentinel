# UX piece 2: one Monitoring list — design

Status: agreed with the owner on 2026-10-07. Piece 2 of the reorganization in
`2026-10-07-ux-reorganization-design.md` (piece 1, navigation and Settings, is
on `dev`).

## Purpose

Uptime checks, server agents and network devices each have their own list page
today (`/uptime`, `/servers`, `/network/devices`). The owner wants one place
that lists everything Sentinel watches, with one set of filters and one "+ Add"
menu, without losing anything the three pages do now.

Decisions made with the owner:

- **One page, a section per type** (Uptime checks, Servers, Devices), each
  keeping its own columns. A single mixed table with shared columns was shown
  and turned down.
- **Monitors and servers get an optional site**, so the site filter covers all
  three types and piece 3's site page can list them. A site on a monitor or
  server is a label, not a permission.

## The page

### Sidebar and address

- The page is **Monitoring** at `/monitoring`.
- The sidebar's Monitor group becomes: **Monitoring**, Sites, SSL & Domains.
  The Uptime, Servers and Devices entries go.
- Monitoring is the active entry on `/monitoring`, `/monitors`, `/monitors/*`,
  `/servers`, `/servers/*`, `/network/devices` and `/network/devices/*`
  (detail pages included).
- Detail pages (`/monitors/:id`, `/servers/:agentID`, `/network/devices/:id`
  and its ports) do not change, apart from their back links (below).

### Filters live in the address

Every filter is a query parameter, so a link or the Back button restores the
view. Absent means "all".

| Parameter | Values | Narrows |
|---|---|---|
| `show` | `uptime`, `servers`, `devices` | which sections appear (the type chips) |
| `q` | text | all sections (search) |
| `status` | `down`, `up`, `pending`, `paused`, `maintenance`, `error` | all sections |
| `site` | a site id, or `none` | all sections |
| `group` | a monitor group id, or `ungrouped` | Uptime only; Servers and Devices are hidden while it is set |
| `type` | `http`, `dns`, `ping`, `tcp`, `webhook` | Uptime only |
| `tags` | comma-separated tags | Uptime only |
| `device_type` | `switch`, `router`, `access_point`, `nvr`, `ups`, `other` | Devices only |

An unknown value is ignored, as if the parameter were absent.

### Header and toolbar

- Title "Monitoring" and one **+ Add** menu:
  - Uptime monitor (the create window), Monitor wizard, Bulk upload, Monitor
    group: everyone, as today.
  - Network discovery, Server agent, Network device: admins only, as today.
- Toolbar, shared by every section:
  - Search. It matches monitors on name and URL, servers on name, hostname,
    agent id and IP address, and devices on name, address, vendor/model and
    site (as today).
  - Type chips with counts: All, Uptime, Servers, Devices (single choice).
  - Status select. Down, Up, Pending and Paused are always offered;
    Maintenance and Error appear only while something is in that state.
  - Site select: All sites, No site, then every site the user can see, plus
    any other site named on a row they can see.
  - Group select: All groups, Ungrouped, then each monitor group.
- The Uptime section's header holds its own type and tags filters. The Devices
  section's header holds its device-type filter.

### Summary strip

One line under the toolbar, counting everything loaded (filters do not change
it): **Watching** N · **Down** N · **Paused** N.

- Down counts monitors that are down, servers that are offline and devices that
  are down. Error and Pending are not Down.
- Paused counts paused monitors and paused devices. Servers cannot be paused.
- Clicking Down or Paused sets that status filter.
- It replaces the Uptime page's three tiles and the Servers page's four. Average
  response time and 30-day incidents move to the Uptime section's header; the
  Servers header shows its count, offline and awaiting-first-report.

### Sections

Order: Uptime checks, Servers, Devices.

- Each header shows its name, count and how many are down, and collapses. The
  collapsed state is remembered in this browser.
- A section whose rows are all excluded by the filters is hidden.
- A section with nothing in it at all (no filters set) shows a one-line prompt
  with its add action. The Servers and Devices prompts are admin-only; a
  non-admin does not see an empty Servers or Devices section.
- **Uptime checks** keeps today's table: monitor-group subsections (with
  their edit/delete and remembered collapse), Status, Response time, 24-hour
  sparkline and uptime %, Last checked, the row menu (Open, Test, Pause/Resume,
  End maintenance, Edit, Delete, as permissions allow) and the sort choices
  (down first, name, lowest uptime, slowest). It gains a **Site** column.
- **Servers** keeps today's table: Status, CPU, Memory, Disk, Uptime, Last
  report, and the admin actions (install instructions, unregister). It gains a
  **Site** column.
- **Devices** keeps today's table: status, name, site, type, address,
  vendor/model, last seen, 30-day availability, sortable by any column.

Rows open their existing detail pages, as today.

### One status scale

| Section | Shown as | Filter value |
|---|---|---|
| Uptime | Up (online) | `up` |
| Uptime | Down (offline) | `down` |
| Uptime | Pending (unknown) | `pending` |
| Uptime | Paused (disabled) | `paused` |
| Uptime | Maintenance (in a maintenance window) | `maintenance` |
| Servers | Active | `up` |
| Servers | Offline | `down` |
| Servers | Awaiting first report | `pending` |
| Devices | Up / Down / Pending / Paused | `up` / `down` / `pending` / `paused` |
| Devices | Error (a setup problem, not an outage) | `error` |

A paused monitor counts as Paused even when its last status was down; a monitor
in maintenance counts as Maintenance, as the Uptime page decides today.

## Sites for monitors and servers

### Database

Migration `060_monitor_agent_sites.sql` adds a nullable `site_id UUID
REFERENCES sites(id) ON DELETE SET NULL` to `monitors` and to `agents`, each
with an index. No CHECK constraints. Deleting a site leaves its monitors and
servers in place with no site.

### API

- Monitor and agent responses gain `site_id` (UUID or null) and `site_name`
  (string or null, read-only).
- Create accepts `site_id`. Edit (`PUT /monitors/:id`, `PATCH
  /agents/:agent_id`) treats the field the same way the other optional fields
  do: **left out means unchanged, an explicit `null` clears it.** An existing
  caller that does not know about sites never wipes one.
- **Who may set which site:**
  - A monitor's site may be set by anyone allowed to edit that monitor, to a
    site they can see (admins: any site; others: sites shared with them, the
    same list `GET /sites` returns them). Any other id is refused with 400
    "site not found", the same answer as for an id that does not exist.
  - Server agents are created and edited by admins only, so any site is
    accepted.
- The site is a label: it does not change who can see or edit a monitor or
  server, and `site_sharing` still governs devices only. Anyone who can see a
  monitor sees its site name.

### Forms

- Monitors: a **Site** select (No site, then the user's sites) in the create
  window, the monitor wizard (next to Group) and the monitor edit form.
- Servers: a **Site** select in the Add and Edit server agent forms.
- Bulk upload and network discovery do not set a site (they do not set a
  group today either); it is set afterwards.

## Data loading and refresh

- Each section loads on its own and keeps today's refresh: monitors and groups
  every 30 s, the server list every 15 s with each row's status every 20 s,
  devices every 30 s. Sites load once for the Site select.
- Monitors load page by page until all are in (the API caps a page at 500).
  Today the Uptime page loads only the first 50 and silently drops the rest.
- The per-monitor 24-hour sparkline is still fetched per row. Rows inside a
  collapsed section or group are not rendered, so they do not fetch. If a list
  of hundreds proves slow, a batched sparkline endpoint is the follow-up (not
  in this piece).
- Filtering, searching and sorting happen in the browser over the loaded rows,
  as on the three pages today.

## Errors and edge cases

- A section whose data fails to load shows an error line with **Retry**; the
  other sections still work, and the summary strip names what is missing
  ("Servers couldn't load").
- While a section loads it shows placeholder rows; its count appears when it
  arrives.
- Nothing added at all: one message ("Nothing is being watched yet") beside the
  + Add menu.
- Filters that match nothing: "Nothing matches these filters" and a **Clear
  filters** button.
- A late response must not overwrite newer data (the existing hooks already
  guard this; new code follows suit).
- Who sees what does not change: monitors the user owns or that are shared with
  them (admins all), every server for any signed-in user, devices only at sites
  the user can see.

## Old addresses and in-app links

- Redirects (replace, so Back does not bounce), keeping any other query
  parameters:
  - `/uptime` and `/monitors` → `/monitoring?show=uptime` (`/uptime?type=http`
    keeps `type=http`).
  - `/servers` and `/server-monitoring` → `/monitoring?show=servers`.
  - `/network/devices` → `/monitoring?show=devices`.
- The old list pages go: `UptimeMonitoring`, `ServerMonitoring`,
  `network/Devices`, and the older `Monitors` page at `/monitors`, which
  nothing in the app links to any more.
- In-app links move to the new addresses: the Overview tiles for uptime and
  servers, the back links and breadcrumbs on the monitor, server, device and
  port pages, and the "done" and "cancel" buttons of the monitor wizard, bulk
  upload and network discovery.

## Out of scope

- Listing a site's monitors and servers on its page (piece 3).
- The site name in alert messages, and report scopes by site.
- A batched sparkline endpoint.
- Any change to detail pages beyond their back links.

## Testing

- **Backend (database tests):**
  - Migration: both columns exist; deleting a site sets them to null.
  - Monitor create and edit with a site; edit without the field keeps it;
    explicit null clears it.
  - A non-admin is refused a site not shared with them (400 "site not found");
    an admin may use any site.
  - Agent create and edit with a site, same keep/clear rules.
  - Monitor and agent lists include `site_name`.
- **Frontend:** the logic lives in `src/utils/` as pure functions, each
  checked by a small throwaway script (the project has no frontend test
  runner), plus the usual typecheck, lint and build:
  - address ↔ filters (parse, build, unknown values ignored),
  - the status scale and the summary counts,
  - filtering and search per section, including `group` hiding Servers and
    Devices,
  - the old-address redirects, keeping query parameters,
  - the active sidebar entry for every path above.

## Owner checks (on the dev stack)

1. The sidebar shows Monitoring, Sites, SSL & Domains under Monitor; Monitoring
   stays highlighted on a monitor, a server, a device and a port.
2. `/monitoring` shows Uptime checks, Servers and Devices; each collapses and
   stays collapsed after a reload.
3. All monitors appear (more than 50 if you have them).
4. The type chips, status, site and group filters, and search narrow the
   sections; picking a group hides Servers and Devices; the address changes
   with each filter and Back restores the previous view.
5. "Down" in the summary strip filters to everything down.
6. Give a monitor and a server a site; both show it, and the site filter
   finds them with that site's devices. Delete a test site: its monitor and
   server stay, with no site.
7. As a non-admin, the Site select in a monitor's form lists only your sites;
   an empty Servers or Devices section is not shown.
8. `/uptime`, `/uptime?type=http`, `/servers` and `/network/devices` land on
   the right filtered view; the old bookmarks for monitor, server and device
   pages still work.
9. + Add offers every create action from the three old pages, with the
   admin-only ones hidden from non-admins.
