# UX piece 4: dashboards and status pages merged — design

Status: agreed with the owner on 2026-10-08. Piece 4, the last piece of the
reorganization in `2026-10-07-ux-reorganization-design.md` (pieces 1–3 are on
`dev`).

## Purpose

Sentinel has two ways to show monitors to people: dashboards (any widgets,
shared inside Sentinel, optionally published by an admin at an unguessable
link) and status pages (a list of monitors in groups with 90-day uptime bars,
published at a readable address). The owner wants one thing: a dashboard that
can also be a public status page.

Decisions made with the owner:

- A dashboard can be reached from outside Sentinel by **a readable address**
  (`/public/status/<name>`, as status pages are today), by **the existing
  unguessable link** (`/public/dashboards/<token>`), by both, or by neither.
- **Only admins publish**, as dashboards already require: only an admin can
  give a dashboard a public address, switch it on or off, or create its link,
  and only an admin can change a dashboard while it is public. (Today any
  signed-in user can create, edit or delete any status page and add any
  monitor to it, including monitors they cannot see; this closes that.)
- A new **Service status** widget shows what a status page shows today.
- **No conversion:** there are no status pages in `dev` or `main`, so the old
  status page tables are removed, guarded so that nothing is ever dropped
  silently.

## Publishing and branding

### What a dashboard gains

- **A public address name** (`public_slug`): 3–50 letters, digits or hyphens
  (`^[a-zA-Z0-9-]+$`, the status page rule), unique across Sentinel. Optional.
- **An address switch** (`public_slug_live`): the address answers only while it
  is on. Turning it off keeps the name reserved, so the dashboard can be
  republished at the same address.
- **Who published the address** (`public_slug_by`, a user): recorded when an
  admin switches it on.
- **A logo** (`logo_url`): an `http://` or `https://` address of a picture, at
  most 2048 characters. Optional.
- **A theme colour** (`theme_color`): `#rgb` or `#rrggbb`. Optional.

The existing public link (`dashboard_public_links`) is unchanged.

### Who may do what

- Setting, changing or clearing the address name, switching it on or off, and
  creating or revoking the link: **admins only** (403 otherwise).
- Logo and theme colour: anyone who may edit the dashboard (they are shown only
  on the public view) — except while it is public, when they are content like
  everything else and only admins may change them.
- A dashboard is **public** while its address is live **or** it has a link.
  While public, only admins may change its content: an editor gets the existing
  refusal "this dashboard is published; ask an admin to change it". (Today this
  applies only to links; it now covers addresses too.)
- An address keeps answering only while its publisher is still an admin, the
  same rule links follow: if that user is demoted or deleted, the address shows
  "not available" until an admin publishes it again.

### The public view

`/public/status/<name>` (and, as today, `/public/dashboards/<token>`) shows:

- a header with the logo (hidden if the picture fails to load), the dashboard's
  name and description, accented with the theme colour;
- the dashboard's widgets in their layout;
- "Nothing to show yet" for a public dashboard with no widgets.

Public responses never carry internal ids, are rate-limited per address as
links are (600 per minute per IP, burst 120), carry `Cache-Control: no-store`,
and share the 15-second widget-data cache links use.

An address that is unknown, switched off, or whose publisher is no longer an
admin answers "not available" (404), with no difference between the cases.

### API

- `GET /api/v1/public/status/:slug` — the public layout (name, description,
  logo, theme colour, version, widgets with id, type, title, x, y, w, h; no
  config), no sign-in.
- `GET /api/v1/public/status/:slug/widgets/:wid/data` — a widget's public data,
  no sign-in.
- `PUT /api/v1/dashboards/:id/public-address` `{ slug, live }` — admin only;
  sets the name and switch together (an empty `slug` clears the address).
- Dashboard responses (`GET /dashboards`, `GET /dashboards/:id`) gain
  `public_slug`, `public_slug_live`, `logo_url`, `theme_color`, and a computed
  `public` (address live or link present). `PUT /dashboards/:id` accepts
  `logo_url` and `theme_color`; it never changes the address.

## The Service status widget

Widget type `service_status`.

- **Shows:**
  - a banner: "All systems operational" when every monitor shown is up, else
    "N of M systems experiencing issues" (the status page wording; up means the
    monitor's current status is online);
  - the monitors in named groups, in the order set;
  - per monitor: name, current status, 90 daily bars, 90-day uptime %;
  - a group whose name is empty shows its monitors without a heading.
- **Config:** `{ groups: [{ name: string, monitors: [monitor id] }],
  show_banner: bool, show_uptime: bool }`. At most 20 groups and 100 monitors in
  total; a group name at most 100 characters; a monitor appears at most once.
  `show_banner` and `show_uptime` default to true. The period is always 90 days.
- **Editor:** add, rename, remove and move groups up and down; add monitors to a
  group with the existing monitor picker; move monitors up and down within a
  group; remove them. Switches for the banner and the uptime %.
- **Who sees which monitors:**
  - inside Sentinel, a viewer sees only monitors they may see (the resolver's
    usual subject filtering), and the banner counts only those;
  - on a public view, every monitor on the widget is shown (only admins publish,
    so an admin chose them).
- **Deleted monitors** are skipped when the widget is read; the editor drops
  them the next time the widget is saved.
- **Refresh:** every 60 seconds; the public view shares the 15-second cache.
- It is an ordinary widget: it can sit on any dashboard with any other widgets.

## What goes away

- **Migration `062_public_dashboards.sql`** (confirm `ls backend/migrations |
  tail -1` shows `061_site_profiles.sql` first):
  - adds the dashboard columns above (`public_slug` unique, `public_slug_by`
    referencing users `ON DELETE SET NULL`, no CHECK tying two columns);
  - **guard:** if `status_pages` holds any row, the migration raises "N status
    pages still exist; delete or convert them before upgrading" and changes
    nothing;
  - otherwise drops `status_page_monitors` and `status_pages`.
- **Backend:** the status page model, service, admin routes
  (`/api/v1/status-pages...`) and the old public route's handler. The path
  `/api/v1/public/status/:slug` is reused by the dashboard public view.
- **Frontend:**
  - the Status pages tab: Dashboards becomes one list (the piece 1 header loses
    its tabs; "+ New dashboard" stays), each dashboard with a **Public** badge
    when it has a live address or a link;
  - the status page pages, form, monitor list, hooks and types;
  - the old public status page; `/public/status/:slug` renders the public
    dashboard view instead;
  - the Overview Status Pages tile; the Dashboards tile counts dashboards that
    are public (address or link);
  - the sidebar's Dashboards entry stops matching `/status-pages`.
- **Old in-app addresses** (`/status-pages`, `/status-pages/create`,
  `/status-pages/:slug/detail`, `/status-pages/:slug/edit`) open `/dashboards`.
- **Backups:** the new columns are in `public`, so dumps include them. A backup
  taken before this upgrade restores only on the version before it (the existing
  schema-version rule), so take a backup right before deploying, especially
  before promoting to production.

## The dashboard page

- **Publishing panel** (admins; replaces the link panel and holds both):
  - *Public address:* the name field, the `/public/status/<name>` it makes, a
    live switch, and a copy button;
  - *Unguessable link:* as today (create, copy, revoke).
- **Appearance** (anyone who may edit): logo address and theme colour, with a
  small preview of the public header.
- The Dashboards list shows the **Public** badge; a dashboard's own page shows
  which public ways are on.

## Edge cases

- A name that breaks the rules: 400 "the address may only use letters, digits
  and hyphens (3–50)". A name another dashboard uses: 400 "that address is
  already used by another dashboard". Names are compared as stored (the status
  page rule).
- Switching the address on with no name: 400 "choose an address first".
- Renaming a live address moves it immediately; the old address shows "not
  available".
- A logo that is not an `http(s)` address, or longer than 2048: 400 naming the
  field. A colour that is not `#rgb`/`#rrggbb`: 400 naming the field.
- Deleting a dashboard frees its address.

## Out of scope

- Uploading a logo (it remains an address, as on status pages).
- Incident or maintenance announcements on public pages (status pages never had
  them).
- Custom domains.
- Converting status pages (none exist).

## Testing

- **Backend (database tests):**
  - migration guard: with a `status_pages` row present the migration's guard
    raises and leaves the tables; with none it drops them;
  - address: name rules, uniqueness, clearing, admin-only (403 for an editor and
    a viewer), live switch, `public_slug_by` recorded;
  - public address: serves the layout and widget data with no internal ids;
    answers 404 when switched off, unknown, or when the publisher is demoted;
    rate limit applies;
  - published content edits: an editor is refused while the address is live,
    allowed when it is off and there is no link;
  - logo and colour validation;
  - Service status widget: config validation (limits, duplicates, unknown
    fields), group and monitor order, banner counts, 90 daily buckets,
    in-app viewers see only their monitors, deleted monitors skipped, public
    payload without ids.
- **Frontend:** pure helpers checked with small throwaway scripts — the banner
  text, the address-name rule, and group/monitor reordering — plus the usual
  typecheck, lint and build.

## Owner checks (on the dev stack)

1. Dashboards is one list; there is no Status pages tab; `/status-pages` opens
   Dashboards.
2. Add a Service status widget with two groups and a few monitors; reorder
   them; the banner reads "All systems operational", or "N of M systems
   experiencing issues" when one is down.
3. As an admin, give the dashboard the address `test-status`, switch it on, and
   open `/public/status/test-status` in a private window: the header shows the
   logo, name, description and colour, and the widget shows the monitors.
4. Switch it off: the address shows "not available". Switch it on again at the
   same name.
5. The unguessable link still works alongside the address.
6. As a non-admin who can edit the dashboard, publishing controls are absent,
   and while it is public the content cannot be changed.
7. The Overview Dashboards tile counts the public dashboard; the Status Pages
   tile is gone.
8. Take a backup before deploying to production (the migration removes the
   status page tables).
