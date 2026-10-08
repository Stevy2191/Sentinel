# UX piece 3: site profiles — design

Status: agreed with the owner on 2026-10-08. Piece 3 of the reorganization in
`2026-10-07-ux-reorganization-design.md` (pieces 1 and 2 are on `dev`).

## Purpose

Each site's page becomes the one-stop page for that location: its address,
networks, ISPs and circuits and notes, next to the devices, servers and uptime
checks there. Today a site holds a name, a description and a street address;
the subnet typed into "Scan subnet" is used once and thrown away, and the site
page does not show the site's servers or uptime checks even though piece 2 gave
them a site.

Decisions made with the owner:

- A profile holds **networks** (subnets and VLANs), **ISPs and circuits**, and
  **free-form notes**. Site contacts were offered and left out.
- **Admins and editable sharers** edit networks, circuits and notes. Renaming,
  addressing, deleting and sharing a site stay admin-only.
- A circuit may be **tied to the device port it plugs into**, and then shows
  that port's live usage against the circuit's speed.
- The page uses **two columns**: the site's facts on the left, what is there
  on the right. (Tabs and one long page were shown and turned down.)

## The site page (`/network/sites/:id`)

### Header

As today: name, description, address; Edit and Delete for admins. Below it a
summary line: "**N** watched · **N** down · Open in Monitoring", where watched
and down count the site's devices, servers and uptime checks on the shared
status scale from piece 2 (Error and Pending are not Down), and the link opens
`/monitoring?site=<id>`.

### Left column: the site's facts

1. **Circuits.** One card per circuit, ordered by provider then creation:
   provider, type and speed ("Fiber · 500/500 Mb"), circuit ID, support phone,
   account number, notes. When the circuit is tied to a port the card also
   shows (see "Circuit usage" below) the port's current in and out, bars against
   the circuit's download and upload speed, the port's status, and a link to
   the port's page.
2. **Networks.** A table — name, subnet, VLAN, gateway, note — sorted by subnet
   address (IPv4 before IPv6).
3. **Notes.** Plain text with line breaks kept; no formatting language.
4. **Sharing** (admins only), moved here from the bottom of today's page.

Each of the first three has, for people who may edit it, "+ Add" (circuits,
networks) or "Edit" (notes), and per-row Edit and Delete. Circuits and networks
are added and edited in a small form window; notes edit in place (a text box
with Save and Cancel). Deletes are confirmed on the page, never with a browser
dialog.

### Right column: what is there

1. **Dashboards** — the site's dashboard chips and "New site dashboard", as
   today.
2. **Devices** — today's device table and filters, with Scan subnet and Add
   device.
3. **Servers** — a compact list of the servers whose site is this site: status,
   name, CPU, and a link to the server's page.
4. **Uptime checks** — a compact list of the monitors whose site is this site
   that the viewer can see: status, name, response time, and a link to the
   monitor's page.
5. **Traffic**, **Busiest ports**, **Ports with problems** and **Recent port
   events** — as today, when the site has devices.

On a phone the left column stacks above the right.

### Empty states

- Circuits, networks, notes: editors see "No circuits yet" (or networks /
  notes) with the add action; read-only viewers see "No circuits recorded"
  (or networks / notes).
- Servers and Uptime checks: "None at this site" and, for anyone who can edit
  servers or monitors, a hint that the site is set in the server's or monitor's
  edit form.

## The sites list (`/network/sites`)

Each site card gains a status line: "**18** devices · **3** servers · **9**
checks · **1** down" (down on the shared scale, red when above zero), computed
from the same monitor, server and device lists the Monitoring page loads. The
line appears once those have loaded; a list that fails to load leaves its part
out rather than showing a zero.

## Data

Migration `061_site_profiles.sql` (confirm `ls backend/migrations | tail -1`
shows `060_monitor_agent_sites.sql` first):

- `sites.notes TEXT` (nullable).
- `site_networks`: `id UUID PK`, `site_id UUID NOT NULL REFERENCES sites(id)
  ON DELETE CASCADE`, `name VARCHAR(100) NOT NULL`, `cidr CIDR NOT NULL`,
  `vlan INTEGER` (nullable), `gateway INET` (nullable), `note VARCHAR(500)`,
  `created_at`/`updated_at TIMESTAMPTZ`; unique `(site_id, cidr)`; index on
  `site_id`.
- `site_circuits`: `id UUID PK`, `site_id UUID NOT NULL REFERENCES sites(id) ON
  DELETE CASCADE`, `provider VARCHAR(100) NOT NULL`, `circuit_ref
  VARCHAR(100)`, `kind VARCHAR(20) NOT NULL`, `download_mbps NUMERIC`,
  `upload_mbps NUMERIC`, `support_phone VARCHAR(50)`, `account_number
  VARCHAR(100)`, `notes VARCHAR(1000)`, `interface_id UUID REFERENCES
  device_interfaces(id) ON DELETE SET NULL`, `created_at`/`updated_at
  TIMESTAMPTZ`; index on `site_id`.
- No CHECK constraint ties two columns (it would break `ON DELETE SET NULL`).
  A CHECK on `kind` alone is fine, written `kind IS NOT NULL AND kind IN (...)`.

Deleting a site removes its networks, circuits and notes. Deleting a device or
port clears the circuit's port link; the circuit stays. Every table is in the
`public` schema, so backups include them with no change to the backup code.

## Validation

All refusals are 400 with a message naming the field.

- **Network:** name required, at most 100 characters; subnet required, IPv4 or
  IPv6 CIDR, stored as its network address (10.20.0.5/24 is saved as
  10.20.0.0/24); VLAN, when given, 1–4094; gateway, when given, an address of
  the same family inside the subnet ("10.30.0.1 is outside 10.20.0.0/24"); note
  at most 500 characters. A site cannot hold the same subnet twice ("Courthouse
  already has 10.20.0.0/24"); different sites may reuse one.
- **Circuit:** provider required, at most 100 characters; type one of `fiber`,
  `cable`, `dsl`, `fixed_wireless`, `cellular`, `copper`, `other` (shown as
  Fiber, Cable, DSL, Fixed wireless, Cellular, T1/copper, Other); download and
  upload speed, when given, greater than 0 and at most 100000 Mbps (decimals
  allowed); circuit ID and account number at most 100 characters; support phone
  at most 50 (free text); notes at most 1000. The port, when given, must be a
  port of a device at this same site.
- **Notes:** at most 10,000 characters; blank clears them.
- Text fields are trimmed; a blank optional field is stored as null.

## Who can do what

- **Read** (the profile, and the Servers and Uptime lists): anyone who can see
  the site. Account numbers are part of the profile and are visible to
  read-only sharers.
- **Change networks, circuits and notes:** admins and editable sharers. A
  read-only sharer is refused with 403 "you can view this site but not change
  it"; someone the site is not shared with gets 404, as today.
- **Name, description, address, delete, sharing:** admins only, unchanged.
- Each change to a network, circuit or the notes is written to the audit log
  (`site_network_created` / `_updated` / `_deleted`, `site_circuit_created` /
  `_updated` / `_deleted`, `site_notes_updated`, resource type `site`, resource
  id the site's id).

## API

- `GET /api/v1/sites/:id/profile` (read): `{ notes, networks: [...],
  circuits: [...] }`. Each circuit carries `port`: null, or `{ interface_id,
  device_id, device_name, if_index, name, oper_status, in_bps, out_bps }` with
  the port's current rates as the port pages show them (null when the port has
  no recent sample). A link whose device is no longer at this site is returned
  as `port: null`.
- `PUT /api/v1/sites/:id/notes` `{ notes }` (change).
- `POST /api/v1/sites/:id/networks`, `PUT` and `DELETE
  /api/v1/sites/:id/networks/:networkId` (change). A network id belonging to
  another site is 404.
- `POST /api/v1/sites/:id/circuits`, `PUT` and `DELETE
  /api/v1/sites/:id/circuits/:circuitId` (change), body including an optional
  `interface_id`. Same 404 rule.
- The existing site responses (`GET /sites`, `GET /sites/:id`) are unchanged:
  notes are read from `/profile`, not added to them, and `PUT /sites/:id`
  (admin) neither reads nor clears notes.

## Behaviour

- **Circuit usage.** In is counted as download and out as upload (traffic the
  site's device receives from the provider is download). With speeds set, each
  bar is the current rate as a share of that speed, capped at 100% for the bar
  while the number shows the real rate. Without a speed, only the numbers show.
  A port whose status is down shows "Port down" in red; a port with no current
  sample shows "No recent data". The profile is refreshed every 30 seconds while
  the page is open, and a late response never overwrites a newer one.
- **Choosing a circuit's port.** The circuit form offers the site's devices,
  then that device's ports (name and alias), or "Not tied to a port".
- **Scan subnet** offers the site's saved IPv4 networks of /22 or smaller as
  one-click choices that fill the subnet box; the box still takes any subnet.
- **Editing.** Each network and circuit saves on its own, so two people editing
  different rows do not overwrite each other; on the same row the last save
  wins.

## Out of scope

- Site contacts.
- Map views, geocoding, and validation of addresses.
- Alerting on a circuit's usage or on a circuit going down (port alerts already
  exist for the linked port).
- Showing a site's profile on dashboards or in reports.
- IPAM features (address allocation, overlap checks across sites).

## Testing

- **Backend (database tests):**
  - networks: subnet normalised to its network address; a duplicate subnet at
    the same site refused, the same subnet at another site accepted; gateway
    outside the subnet refused; VLAN out of range refused;
  - circuits: unknown type refused; a port at another site refused; deleting
    the port clears the link and keeps the circuit; the profile returns the
    linked port's device, status and current rates;
  - notes: saved and cleared;
  - permissions: a read-only sharer gets 403 on every change and can read the
    profile; a user without access gets 404; an editable sharer may change;
  - deleting a site removes its networks and circuits;
  - every change writes its audit entry;
  - a network or circuit id from another site is 404.
- **Restore test:** a backup taken with networks, circuits (one tied to a port)
  and notes restores with them intact (`TestDBRestoreWithMetrics` or a sibling,
  run through `./scripts/test-db.sh`).
- **Frontend:** pure helpers checked with small throwaway scripts — which saved
  networks Scan offers, the usage share and cap, the sites-list status line and
  the header summary counts — plus the usual typecheck, lint and build.

## Owner checks (on the dev stack)

1. A site's page shows two columns (one on a phone): circuits, networks, notes
   and sharing on the left; dashboards, devices, servers, uptime checks and the
   traffic and port lists on the right.
2. Add a network with a host address (10.20.0.5/24): it is saved as
   10.20.0.0/24. Adding it again is refused; a gateway outside it is refused.
3. Add a circuit tied to the firewall's WAN port: the card shows current
   in/out against its speed and links to the port; unplug or disable the port
   and the card says "Port down".
4. Edit the notes; reload; they are kept with their line breaks.
5. Scan subnet offers the saved networks.
6. Give a server and a monitor this site (from their edit forms): they appear
   in the site's Servers and Uptime checks lists.
7. The header's "watched · down" matches what Monitoring shows for the site,
   and "Open in Monitoring" lands on it filtered.
8. As an editable sharer, add and edit a circuit; as a read-only sharer, the
   add and edit actions are absent and the profile is readable.
9. The sites list shows each site's devices, servers, checks and down count.
