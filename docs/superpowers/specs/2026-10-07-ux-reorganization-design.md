# UX reorganization: design

Status: agreed with the owner on 2026-10-07. This document holds the plan for
the whole reorganization (four pieces) and the detailed design of piece 1.
Pieces 2–4 each get their own brainstorm, spec and plan when their turn
comes; only their intent is recorded here.

## Purpose

Sentinel started as uptime monitoring with a few extras and has grown piece by
piece: server agents, SSL and domains, SNMP network monitoring, dashboards,
status pages, reports and network tools. The owner wants it to feel like one
product: fewer top-level places, related things together, and setup out of
the way of everyday use.

What the owner said hurts today:

- Too many sidebar items (ten top-level entries, growing with each phase).
- Network Monitoring swaps the whole sidebar for its own menu; "← Sentinel"
  is the only way back.
- Settings are scattered: Settings (four tabs), Network's own settings page,
  the Users page, a second Notifications page, and setup screens
  (credentials, profiles, MIB library) inside Network's menu.

Who uses it: today only the owner, soon other staff. So everyday pages stay up
front, and setup moves into one admin area that other users mostly do not
see.

## The four pieces, in order

1. **Navigation and Settings** (this document). A single grouped sidebar,
   Network folded into it, Dashboards and Status Pages as one entry, and one
   Settings area with its own menu. Mostly frontend.
2. **Combined Monitoring list.** One "Monitoring" page listing everything
   Sentinel watches: uptime checks, network devices and server agents, with
   type chips, status, site/group filters and search, and one "+ Add" menu.
   Each row opens its existing detail page. It replaces the Uptime, Servers
   and Devices sidebar entries.
3. **Site profiles.** Each site becomes the one-stop page for its location:
   street address, network information (subnets, gateway, VLANs), ISPs and
   circuits (provider, circuit ID, support number), next to the devices,
   servers and monitors at that site.
4. **Dashboards and status pages merged.** Status pages become dashboards
   that can be published publicly, keeping everything status pages do today
   (readable public address `/public/status/<slug>`, logo, theme colour,
   publish switch, grouped monitors with 90-day uptime bars) through
   dashboard-level branding and a "Service status" widget. Existing status
   pages are converted automatically and keep their public addresses. The
   Status pages tab added in piece 1 then goes away.

Out of scope for all four: Reports vs the older Analytics page, and Overview
vs dashboards (the owner did not raise them).

## Piece 1: navigation and Settings

### Sidebar

One sidebar for the whole app; the separate Network menu and its
"← Sentinel" button go away. Order, groups and visibility:

| Group | Entry | Opens | Shown to |
|---|---|---|---|
| — | Overview | `/` | everyone |
| — | Dashboards | `/dashboards` | everyone |
| Monitor | Uptime | `/uptime` | everyone |
| Monitor | Servers | `/servers` | everyone |
| Monitor | Devices | `/network/devices` | everyone |
| Monitor | Sites | `/network/sites` | everyone |
| Monitor | SSL & Domains | `/ssl` | everyone |
| Respond | Incidents | `/incidents` | everyone |
| Respond | Reports | `/reports` | everyone |
| Tools | Network Tools | `/tools` | admins and grant holders (as today) |
| Tools | MIB Browser | `/network/mibs/browse` | everyone (as today) |

- Group labels are small uppercase headings, not links, and are always
  expanded.
- The footer keeps your name (to `/profile`), then **Settings**, then **Log
  out**. The Users button leaves the footer (Users moves into Settings).
- The top bar keeps the refresh button; its Settings gear goes (it repeated
  the sidebar's Settings).
- The line under the app name that said "Uptime Monitor" or "Network
  Monitoring" (the old mode) is removed.
- **Active entry:** an entry is highlighted on its own pages and their
  children:
  - Overview: `/` only.
  - Dashboards: `/dashboards`, `/dashboards/*`, `/status-pages`,
    `/status-pages/*`.
  - Uptime: `/uptime`, `/monitors`, `/monitors/*`.
  - Servers: `/servers`, `/servers/*`.
  - Devices: `/network/devices`, `/network/devices/*`.
  - Sites: `/network/sites`, `/network/sites/*`.
  - SSL & Domains: `/ssl`.
  - Incidents: `/incidents`, `/incidents/*`.
  - Reports: `/reports`, `/reports/*`.
  - Network Tools: `/tools`, `/tools/*`.
  - MIB Browser: `/network/mibs/browse`.
  - Settings: `/settings`, `/settings/*`.
- The mobile drawer shows the same sidebar.

### Dashboards and Status pages: one entry

`/dashboards` and `/status-pages` share a tab bar at the top of the page:
**Dashboards** (`/dashboards`) and **Status pages** (`/status-pages`). Each
tab is the existing list, unchanged. A single **+ New** button offers "Dashboard"
(the existing new-dashboard flow) and "Status page" (`/status-pages/create`).
The editors, detail pages and public addresses do not change.

### Settings: one area with its own menu

`/settings` becomes a layout with a menu on the left (a select at the top of
the page on narrow screens) and the chosen section on the right. Each section
has its own address:

| Menu group | Section | Address | Content (from) | Shown to |
|---|---|---|---|---|
| Sentinel | General | `/settings/general` | Settings → System tab, minus backups | admins |
| Sentinel | Notifications → Channels | `/settings/notifications` | Settings → Notifications tab (channel setup) | admins |
| Sentinel | Notifications → History | `/settings/notifications/history` | the Notifications page (active channels, weekly counts, history with retry) | everyone |
| Sentinel | Users & access | `/settings/users` | the Users page (users, invitations, registration, network-tools grants) | admins |
| Sentinel | Backups | `/settings/backups` | the backup and restore card from Settings → System | admins |
| Network | SNMP credentials | `/settings/network/credentials` | Network → Credentials | admins |
| Network | Device profiles | `/settings/network/profiles`, `/settings/network/profiles/:id` | Network → Profiles and a profile's page | admins |
| Network | MIB library | `/settings/network/mibs` | Network → MIB library | admins |
| Network | Polling & thresholds | `/settings/network/polling` | Network → Settings | admins |
| Network | Network tools | `/settings/network-tools` | Settings → Network tools tab | admins |
| Help | About | `/settings/about` | Settings → About tab | everyone |

- `/settings` itself goes to `/settings/general` for admins and to
  `/settings/about` for everyone else. A section a user may not see sends
  them to `/settings/about`.
- Each section keeps its existing content and behaviour; only where it lives
  changes. A moved page drops its own big page title and uses the section's
  heading instead, so the layout reads as one area.
- Admin-only sections stay admin-only in the API as today; hiding them in the
  menu is for tidiness, not security.

### Old addresses redirect

Every address that moves keeps working through a redirect (replacing the
history entry), so bookmarks, alert-email links and in-app links all land in
the right place:

| Old address | Goes to |
|---|---|
| `/admin/users` | `/settings/users` |
| `/notifications` | `/settings/notifications/history` |
| `/network/credentials` | `/settings/network/credentials` |
| `/network/profiles` | `/settings/network/profiles` |
| `/network/profiles/:id` | `/settings/network/profiles/:id` |
| `/network/mibs` | `/settings/network/mibs` |
| `/network/settings` | `/settings/network/polling` |
| `/settings?tab=system` | `/settings/general` |
| `/settings?tab=notifications` | `/settings/notifications` |
| `/settings?tab=nettools` | `/settings/network-tools` |
| `/settings?tab=about` | `/settings/about` |

The existing redirects stay (`/dashboard`, `/server-monitoring`,
`/settings/security`, and `/network` → `/network/sites`). In-app links that point at a moved address are updated
to the new one; the redirects cover anything missed.

### What does not change

- No backend or API changes, no migration: every API route stays where it is.
- Sites, Devices, Servers, Uptime, monitor, device and port pages keep their
  addresses and content.
- Permission rules stay as they are (admins, grant holders for Network
  Tools).

### Errors and edge cases

- Unknown `/settings/<x>` → `/settings/about` (or `/settings/general` for
  admins), never a blank page.
- A non-admin following an old admin link (for example `/network/credentials`)
  lands on `/settings/about`, not on an empty admin page.
- The settings menu remembers nothing between visits; the address decides the
  section.

### Testing

There are no frontend unit tests; the frontend gate (type check, lint, build)
is the automated check. Owner checks on the dev stack:

1. The sidebar shows the groups and entries above; Network's pages no longer
   swap the menu.
2. Each sidebar entry highlights on its own pages and child pages (a monitor,
   a device's port, a server, a tool run, a status page's editor).
3. Dashboards shows the two tabs; "+ New" creates either a dashboard or a
   status page.
4. Every Settings section opens at its own address and works as before:
   change a General setting, add a notification channel, view notification
   history and retry a failed one, invite a user, run a backup, add an SNMP
   credential, open a device profile, upload a MIB, change a polling
   threshold, edit the network tools allowlist.
5. Every old address in the redirect table lands on its new place.
6. As a non-admin: the Settings menu shows only Notifications → History and
   About; an admin-only address sends you to About.
7. On a phone-width window the sidebar drawer and the Settings section
   select both work.
