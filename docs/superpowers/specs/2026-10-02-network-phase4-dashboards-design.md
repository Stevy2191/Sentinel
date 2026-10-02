# Network monitoring phase 4: custom dashboards

Part of the network monitoring roadmap
(`2026-09-28-network-monitoring-roadmap.md`), building on phase 2
(`2026-09-30-network-phase2-ports-bandwidth-design.md`), UPS monitoring
(`2026-10-01-ups-monitoring-design.md`) and phase 3
(`2026-10-01-network-phase3-mibs-custom-metrics-design.md`). Phase 4 lets
users build dashboards: grids of widgets over any network metric, device,
site, monitor or server agent, viewed in the app, on a wall display, or
through a public link by people without a Sentinel login.

## Problem

Every view in Sentinel today is fixed: the Overview, the site, device and
port pages, the monitor pages. Nobody can put the things they care about on
one screen. The IT office has no wall display that shows the network, the
UPSes and the monitored services together, and there is no way to show
people outside IT a live view without giving them an account. The site power
view the user asked for on 2026-10-01 (every UPS in a site at a glance) was
deliberately left for this phase as a dashboard widget.

## Decisions

Taken with the user, 2026-10-02:

1. **Four audiences, one feature**: an always-on wall display in the IT
   office, techs troubleshooting (open a dashboard, click through to the
   device, port or incident), a personal overview per IT user, and read-only
   viewers outside IT.
2. **Non-IT viewers use a public link with no login**, like status pages.
   The same link is what a wall display opens, so a TV never drops off when
   a 24-hour login expires.
3. **Only admins create, regenerate or revoke public links.** Anything
   leaving the building goes through an admin.
4. **Dashboards cover network data and monitors**: devices, ports, UPSes,
   custom metrics, uptime monitors and server agents.
5. **One dashboard per screen.** No rotating playlists.
6. **Widgets get their data from the server** (approach 1 of three
   considered): one endpoint per widget resolves its saved settings,
   checks access and returns only that widget's data. Public links use the
   same code with sensitive fields trimmed, so a public link can never
   return data that is not on its dashboard.
7. **Drag-and-drop grid** with `react-grid-layout` (licence and React 18
   support confirmed during planning; if it fails that check, the plan picks
   an alternative before any code is written).
8. **A new site dashboard can start with standard widgets**, so a useful
   dashboard takes one click.

Rejected: widgets calling the existing APIs directly (approach 2: a second,
separately filtered data path for public links, and widgets would receive
whatever the APIs return, including IP addresses); fixed tiles with no
drag-and-drop (approach 3: not what the roadmap asks for).

## 1. Data model

One migration, `057_dashboards.sql` (the latest applied is 056). All
tables are in `public`, so configuration backups include them, public link
tokens included.

```sql
CREATE TABLE dashboards (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    description TEXT NOT NULL DEFAULT '',
    site_id     UUID REFERENCES sites (id) ON DELETE CASCADE,  -- NULL = personal
    owner_id    UUID REFERENCES users (id) ON DELETE CASCADE,  -- personal only
    created_by  UUID REFERENCES users (id) ON DELETE SET NULL, -- audit only
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A dashboard is either personal (an owner, no site) or a site's (a site,
    -- no owner). Both FKs in this CHECK cascade; neither is SET NULL.
    CHECK ((site_id IS NULL) = (owner_id IS NOT NULL))
);

CREATE TABLE dashboard_widgets (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES dashboards (id) ON DELETE CASCADE,
    type         TEXT NOT NULL,             -- validated against the Go registry
    title        TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 100),
    config       JSONB NOT NULL DEFAULT '{}',
    x INTEGER NOT NULL CHECK (x >= 0),
    y INTEGER NOT NULL CHECK (y >= 0),
    w INTEGER NOT NULL CHECK (w BETWEEN 1 AND 12),
    h INTEGER NOT NULL CHECK (h BETWEEN 1 AND 24),
    CHECK (x + w <= 12)
);
CREATE INDEX dashboard_widgets_dashboard ON dashboard_widgets (dashboard_id);

CREATE TABLE dashboard_sharing (               -- personal dashboards only
    dashboard_id UUID NOT NULL REFERENCES dashboards (id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    permission   TEXT NOT NULL CHECK (permission IN ('readonly', 'editable')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (dashboard_id, user_id)
);

CREATE TABLE dashboard_public_links (          -- at most one per dashboard
    dashboard_id UUID PRIMARY KEY REFERENCES dashboards (id) ON DELETE CASCADE,
    token        TEXT NOT NULL UNIQUE,         -- 32 random bytes, base64url
    created_by   UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- **Widget type** is checked in Go against the widget registry (section 3),
  not by a SQL CHECK, so adding a widget type needs no migration.
- **Widget ids are stable across saves.** A save sends the full widget list;
  widgets that carry an id keep it, new ones get one, and missing ones are
  deleted. Public widget URLs and cache keys therefore survive edits.
- **Limits:** 50 widgets per dashboard; a widget's `config` at most 16 KB.
- **Deletion:** deleting a user deletes their personal dashboards (and those
  dashboards' shares and links); deleting a site deletes its dashboards.
  Dashboards are deliberately *not* added to `siteContentTables`: that list
  stops a site delete from silently deleting monitored devices, and a
  dashboard is a view, not monitored content. The site delete confirmation
  says how many dashboards, and how many public links, go with it.
- **Deleted subjects:** a widget whose device, port, site or monitor no
  longer exists shows "Removed"; nothing cascades into widget configs.

## 2. Access rules

### Who may see or change a dashboard

`DashboardService.Access(ctx, viewer, dashboard)` returns one of `none`,
`view`, `edit` or `manage`, from one function, like `resolveSiteAccess`:

| Dashboard | view | edit | manage |
|---|---|---|---|
| Site | readonly access to the site | editable access to the site | admin |
| Personal | shared readonly | owner; shared editable | admin |

- **Create:** any signed-in user creates personal dashboards; a site
  dashboard needs editable access to the site.
- **Share** (personal dashboards only; a site dashboard follows its site's
  sharing): the owner and admins.
- **Published dashboards** (those with a public link): changing the name,
  description, widgets or layout, and deleting the dashboard, need
  `manage`. Others with `edit` see it read-only with "Published — ask an
  admin to change it". The owner can still change its sharing, which does
  not affect the public view.
- **Public links:** only admins create, regenerate or revoke them, behind
  `RequireAdmin` (which re-checks the database, not just the token claim).
- A dashboard the caller cannot see answers **404**, never 403.

### Who may see the data inside

A dashboard grants no data access of its own.

- Each widget declares its **subjects** (section 3). A central
  `filterSubjects(viewer, subjects)` sorts them into visible, hidden and
  removed using the existing rules: site access for sites, devices and
  ports; `CanUserViewMonitor` for monitors; admin-only for server agents.
  Widgets never check access themselves; they only resolve what they are
  handed.
- No visible subjects: the widget's state is `no_access` (some hidden) or
  `removed` (all gone). Otherwise `ok`, with `hidden` and `removed` counts
  the frame shows as "1 hidden".
- **Saving checks the editor**: every subject in every widget must be
  visible to the person saving, or the save is a 400. Nobody can use a
  dashboard to probe ids.
- **Viewers without `edit` never receive widget configs** (which contain
  subject ids), only each widget's type, title, position and display
  options.
- **Broad scopes** ("all open incidents I can see") resolve against the
  viewer, so two people see different rows on the same widget.

### Public links

- The public viewer has no user. A public request skips per-user subject
  checks, because an admin approved the dashboard, and resolves exactly the
  saved widgets.
- A link works only while the dashboard exists, the link row exists, and
  `created_by` is still an admin (checked on every request). Otherwise the
  public routes answer 404.
- **The public trim.** Public responses never contain: IP addresses,
  hostnames or ports of devices; monitor URLs, hostnames or targets;
  free-text status, error or detail strings (device `status_detail`,
  monitor check errors, incident root cause, notes and resolution notes),
  which routinely contain addresses; and any device, port, monitor,
  incident or agent UUID, so the public page cannot link into Sentinel.
  They keep names, port names and aliases, statuses, values, times and
  severities.
- A broad scope on a published dashboard resolves as the link creator's
  admin view, so it includes things added later. The Public link panel
  lists such widgets before the admin confirms.

## 3. Widgets

### The registry

Each widget type is one Go file under `internal/services/widgets/` that
implements:

```go
type Widget interface {
    Type() string
    // Validate normalises a config and rejects bad ones (400 naming the field).
    Validate(ctx context.Context, cfg json.RawMessage) (json.RawMessage, error)
    // Subjects lists what the config refers to, for access checks.
    Subjects(cfg json.RawMessage) Subjects
    // Resolve loads the data for the visible subjects only.
    Resolve(ctx context.Context, cfg json.RawMessage, in ResolveInput) (any, error)
    // Refresh is how soon the browser should ask again.
    Refresh(cfg json.RawMessage, rng string) time.Duration
}
```

`Subjects` holds site ids, device ids, ports (device id + ifIndex), monitor
ids, agent ids and a `Broad` flag. `ResolveInput` carries the visible
subjects, the effective range, and `Public bool`; with `Public` set the
widget builds its trimmed response. A registry maps type names to
implementations; adding a widget later is one file and one entry.

### The widget types

| Type | Shows | Config | Data from |
|---|---|---|---|
| `timeseries` | Line/area chart of up to 10 metrics | `source`: `metrics` (metric keys; devices + instances, or a site total) or `site_traffic` (site, `internet` or `east_west`); `range` | `MetricsStore.Query`; `PortService.SiteTraffic` |
| `stat` | One number: latest value or average over the range; amber/red at thresholds; optional sparkline | metric key; one device + instance or site total; `mode`; `warn`, `crit`, `direction` (above/below); `sparkline`; `range` | `MetricsStore.Query` |
| `port_grid` | A device's faceplate with live port status | device | `PortService` ports view |
| `top_n` | Busiest ports (average traffic or utilisation) or most errors | site or devices; `measure`: `traffic`, `utilisation`, `errors`; `n` 5–20; `range`; physical ports only | new `MetricsStore.TopN` |
| `event_log` | Recent port events and incident openings/closings, newest first | site or device; `limit` 10–50 | `PortService.Events`; incidents |
| `device_table` | A site's devices: status, name, type, vendor/model, last seen, 30-day availability, open incidents (the Address column is dropped in public views) | site; optional `types`, `statuses` filters | device list |
| `site_power` | Every UPS in a site: mains or battery, charge, runtime left, load, open UPS alerts | site | new site-wide UPS status |
| `device_health` | The device page's Health section | device | `ProfileService.DeviceHealth` |
| `open_incidents` | Open incidents, monitors and network | `scope`: site, devices, monitors, or `all` (broad) | incidents, with the new site filter |
| `monitors` | Monitors and server agents, up/down, as a list or as uptime bars | monitor ids (≤ 50), agent ids (≤ 50); `style`: `list` or `bars`; `window`: `24h` or `90d` | monitor status; the status-page uptime buckets |
| `label` | Plain text, e.g. "Main Campus" on a wall display | `text` (≤ 200 chars, no HTML or Markdown); `size` S/M/L | — |

- Per-widget limits mirror the metrics API: ≤ 10 metrics, ≤ 50 devices,
  ≤ 200 instances.
- Server agents in `bars` style show as status rows (they have no uptime
  history bars).
- The uptime bucket helpers, `computeDailyUptimeBuckets`
  (`api/status_page_handler.go`) and `computeHourlyUptimeBuckets`
  (`api/report_handler.go`), move into a service so status pages, reports
  and the `monitors` widget share one implementation.

### New backend pieces

- `GET /devices/:id/metrics` — the metrics and instances that exist for a
  device, from `metrics.series` joined with `MetricCatalogue` and phase 3
  custom keys and labels: `[{metric, label, unit, instances: [{instance,
  label}]}]`. Readonly site access. Used by the editor's pickers.
- `MetricsStore.TopN(scope, measure, from, to, n)` — reads the 5-minute and
  1-hour continuous aggregates (raw only for ranges ≤ 6h, matching
  `Query`), ranks physical ports, and returns device name, port name, alias
  and value.
- `PortService.SiteUPSStatus(siteID)` — every UPS in a site (chosen or
  detected type `ups`) with its `UPSStatusView` and device name and status.
- A `site_id` filter on the incident list, matching device, port, UPS and
  metric incidents of that site's devices. Monitors are not in sites, so a
  site filter never returns monitor incidents.

### Starter widgets for a new site dashboard

Optional when creating a site dashboard ("Start with the standard widgets"):
`timeseries` site internet traffic (w8 h4) and `site_power` (w4 h4);
`open_incidents` for the site (w6 h4) and `top_n` busiest by traffic over
24h (w6 h4); `device_table` for the site (w12 h5).

## 4. API

All under `/api/v1`. Logged-in routes are behind `AuthMiddleware`.

| Method | Path | Who |
|---|---|---|
| GET | `/dashboards?site_id=` | signed in; returns what the caller can see, grouped mine / shared / site |
| POST | `/dashboards` | personal: anyone; site: editable on the site. Body may set `starter: true` |
| GET | `/dashboards/:id` | `view`; configs only with `edit` |
| PUT | `/dashboards/:id` | `edit` (`manage` if published); body has `version` and the full widget list |
| DELETE | `/dashboards/:id` | `edit` (`manage` if published) |
| GET / PUT / DELETE | `/dashboards/:id/shares[/:userId]` | owner or admin; personal only |
| GET / POST / DELETE | `/dashboards/:id/public-link` | admin (`RequireAdmin`); POST creates or regenerates |
| GET | `/dashboards/:id/widgets/:wid/data?range=` | `view` |
| POST | `/dashboards/:id/widgets/preview` | `edit`; body is an unsaved widget; resolved as the editor |
| GET | `/devices/:id/metrics` | readonly on the device's site |
| GET | `/public/dashboards/:token` | anyone; layout, titles and display options, no ids |
| GET | `/public/dashboards/:token/widgets/:wid/data` | anyone; public trim |

- **Saving** runs in one transaction. A stale `version` is a 409 carrying
  the current version; nothing is written. Each widget is validated and its
  subjects checked against the editor; a failure is a 400 naming the
  widget's index and field.
- **Widget data response:**
  `{state: "ok"|"no_access"|"removed"|"no_data", data, hidden, removed,
  refresh_seconds, generated_at}`. Expected states are 200s; an unknown
  widget is a 404.
- **`range`** on the logged-in data route overrides the saved range of the
  time-based widgets (`timeseries`, `stat`, `top_n`) for that request only;
  other types ignore it. The public route takes no range.
- **Refresh:** 30 s for `port_grid`, `site_power`, `device_health`,
  `open_incidents`, `event_log`, `device_table` and `monitors`; for charts
  and stats, 60 s up to a 6h range, 5 min up to 7d, 15 min beyond; `label`
  never refreshes.
- **Public cache:** public data responses are cached in memory for 15 s,
  keyed by dashboard id, dashboard version and widget id, with concurrent
  identical requests collapsed into one query. Several TVs on one link cost
  one query per widget per 15 s. Logged-in responses are not cached, since
  each viewer's result can differ.
- **Rate limit:** the public routes get a per-IP limit of 600 requests a
  minute, sized for several TVs behind one address. Token guessing is
  infeasible regardless (256-bit tokens).
- **Errors:** real failures go through `respondInternal` (logged, generic
  message to the client), never raw error text.
- **Audit:** create, update, delete, share changes, and public link create,
  regenerate and revoke are recorded with the existing audit recorder.

## 5. Frontend

### Where dashboards live

- **Dashboards** is a main-sidebar item directly below Overview, since
  dashboards span monitors and network gear, with an Overview card
  (count of dashboards the user can see), following the one-card-per-section
  rule.
- A site's page lists its dashboards and has **New site dashboard**.
- Routes: `/dashboards` (Mine, Shared with me, Site dashboards),
  `/dashboards/:id` (view), `/dashboards/:id/edit` (edit), and
  `/public/dashboards/:token` outside the app shell, next to
  `/public/status/:slug`. The existing `/dashboard` → `/` redirect stays.

### Grid

- `react-grid-layout`, 12 columns, row height about 80 px.
- At phone width widgets stack in one column in saved order (by y, then x).
  Editing is desktop-only.

### View mode

- Header: name; the site (for a site dashboard); a range-override picker
  for time-based widgets (not saved); **Fullscreen**; **Share** (personal);
  **Public link** (admins).
- Ports, devices, incidents and monitors in widgets link to their pages.

### Edit mode

- Edits apply to a local draft; widgets move and resize by drag; **Save**
  sends the whole dashboard. **Cancel**, or navigating away with unsaved
  changes, confirms in-page (no `window.confirm`).
- **Add widget** lists the 11 types with a one-line description each;
  picking one opens its settings in a side panel with a live preview from
  the preview endpoint.
- Pickers go site → device → port or metric (from `/devices/:id/metrics`),
  or monitors and agents, offering only what the user can see.
- A 409 shows "Someone else saved this dashboard. Reload to see their
  changes; your edits will be lost." A 400 highlights the named widget and
  field.

### Fullscreen and the wall display

- Browser fullscreen with the app's chrome hidden, larger type, and a
  "last updated" clock.
- A widget whose data is older than three of its refresh periods is marked
  **stale**: a TV must never look healthy because it stopped updating.
- Asks for a screen wake lock where supported; after a lost connection
  keeps retrying with backoff (capped at 5 minutes); reloads itself every 6
  hours to pick up new versions and shed memory.
- The public page opens straight into this mode and has no way into the
  app.

### Components

- `components/dashboards/`: the grid, a `WidgetFrame` (title, loading,
  "No access", "Removed", "No data", errors, "N hidden", stale marker), one
  renderer and one settings form per widget type, the widget picker, the
  sharing panel and the public link panel. Pages under
  `pages/dashboards/`. Helpers in `src/utils/dashboards.ts` (component
  files export only components).
- Hooks: dashboards list, one dashboard, widget data (polls at
  `refresh_seconds`, aborts on unmount, ignores late responses after inputs
  change), and the public dashboard.
- Renderers reuse `AreaSeriesChart`, `Faceplate`, `HealthSection`, the UPS
  readings, `PortEventList`, `DeviceTable` and the status-page uptime bars.
  `AreaSeriesChart` gains a height prop (fixed at `h-64` today); clickable
  lists gain a prop to turn their links off for public views.
- Existing slate, dark-only design system; colours from `utils/colors.ts`.

## Testing

- **Unit (Go):** every widget type's `Validate` (good and bad configs) and
  `Subjects`; refresh periods; `DashboardService.Access` for every row of
  the access table.
- **DB tests** (`scripts/test-db.sh`):
  - CRUD, the version conflict, widget ids kept across saves, cascades on
    user and site deletion.
  - The access matrix: admin, owner, shared readonly, shared editable,
    site readonly, site editable and no access, against view, edit,
    delete, share and publish; published dashboards refusing non-admin
    edits.
  - Every widget's `Resolve` against seeded devices, series, UPS readings,
    incidents, monitors and agents; `no_access`, `removed` and the hidden
    count.
  - `TopN`, `SiteUPSStatus`, the incident site filter, and
    `/devices/:id/metrics`.
- **Public leak test:** seed a device with a known IP and hostname, a
  monitor with a known URL, a device `status_detail`, an incident root
  cause and notes; build a dashboard with one widget of every registered
  type; fetch the public layout and every widget's public data; assert none
  of those strings and none of the subject UUIDs appear. The test iterates
  the registry, so a new widget type fails it until it has seed config.
- **Public link lifecycle:** revoked, regenerated, creator demoted and
  dashboard deleted all give 404.
- **Handlers** (fakes): 404 vs 403, 409, 400 naming the widget, admin-only
  public link routes.
- **Simulator:** `timeseries` and `port_grid` end to end against snmpsim.
- **Frontend:** the repo's gate (typecheck, lint, build); there are no
  frontend unit tests.
- **Owner checks on the work install:** build a site dashboard from the
  starter widgets; leave it fullscreen for a day; open the public link in a
  private window, then revoke it; stop the backend and see the stale
  markers.

## Delivery

On `feature/network-phase4` from `dev`, in a git worktree. The plan builds
in this order: data model and access; the widget registry and each
widget's data; public links and the leak test; the grid and editor; the
widget renderers; fullscreen and the public page. A whole-branch review,
then a fast-forward into `dev`, deployment to the work install for the
owner checks, a phase 4 follow-ups doc, and an updated `STATUS.md`.

## Out of scope

- Rotating playlists across dashboards.
- Expiry dates on public links (revoke or regenerate only).
- Running a dashboard as a report (roadmap, Later).
- Per-widget colours, math across metrics, HTML or Markdown in labels.
- The live map widget (phase 6).
