# Site maps — design (network phase 6, piece C)

**Date:** 2026-10-19
**Status:** approved by the owner 2026-10-19 ("looks good"), with the second mockup (`assets/2026-10-19-site-maps/site-map-topology.html`, open it next to `drawings2.js` and `drawings.css`)
**Source:**
- The phase 6 roadmap (`2026-09-28-network-monitoring-roadmap.md`), which lists:
  - LLDP-suggested links
  - a map editor with draggable devices and links bound to ports
  - fast polling of bound ports, with a push channel to the browser
  - links coloured and sized by utilisation, with animated flow, that click through to the port
  - read-only sharing, a fullscreen wall display, and a dashboard widget
- Piece B (`2026-10-18-find-device-switch-maps-design.md`): "realistic site maps are piece C".

The owner asked to skip the questions, so everything not in those sources is **my call** and marked **(call)**.

## Purpose

A site map is a live picture of one site's **network structure**: its routers and firewalls, its switches, how they're cabled to each other, and what's flowing over each cable right now.

At home, that's Quantum-Gate (a UDM-SE) and QuantumLink (a USW-Pro-48-PoE) under it. At work, it's the router or firewall, the core switch, and the tree of distribution and access switches under it, EdgeSwitches and Cisco.

**Decided by the owner (2026-10-19, after the first mockup):** "I am looking for just a network structure topology on this site map. If the site has a router/firewall and the network switches and how they all connect. I don't need to see end points." So access points, NAS, NVR, UPS and other devices are not on the map. The second mockup, with 19 example switches, is the reference for the layout.

A glance at the map should answer three questions:
- What's down, and what does it cut off?
- Which link is busy?
- How is this site wired?

## Goals

- **It opens already drawn.** Devices are laid out and links found automatically from what Sentinel already collects: LLDP neighbours, port settings and MAC tables. Nobody has to draw the map before it's useful.
- **It can be edited.** You can drag devices, add a link by hand (bound to a port at one or both ends), and hide a device or a link.
- **It's live.**
  - Each link's colour shows how busy it is, its width shows its speed, and dots flow along it in the direction traffic goes.
  - Updates arrive about every 10 seconds, pushed to the browser.
- **It's realistic.** Devices Sentinel has a front-panel drawing for are drawn as their real front panel, and a link attaches to the actual port it's plugged into.
- **You can click through.** A link opens its two ports and their traffic, and a device opens its page.
- **It can be shared.**
  - A read-only public link.
  - A fullscreen wall display.
  - A dashboard widget.

## Non-goals

- **Endpoints of any kind,** the owner's decision. That covers PCs, phones, cameras and printers, but also access points, NAS, NVRs, UPSes and servers. The switch map from piece B shows what's plugged into a switch. The map's device panel says "82 end devices are plugged into it" in one line, with no chip on the map **(call)**.
- **The org map** of every site: that's piece D.
- **Floor plans or photos behind the map** **(call)**: later, if wanted.
- **More than one map per site** **(call)**: one map per site.
- **Wireless client counts and AP radio details**: these wait for the UniFi controller source (phase 7).
- **Keeping the fast samples as history.** The regular poll keeps writing port history as today.

## Anti-goals

- **A map that looks live when it isn't.** Stale traffic must look stale: grey links, and "Updated 2 min ago" or "Reconnecting…" shown plainly.
- **A guessed link drawn as if it were certain.** A link guessed from a MAC table is dashed and marked "?" until someone keeps it.
- **A map that needs an hour of drawing before it's any use.** It opens laid out.
- **Fast polling that hurts a device or slows the regular poll.** It reads only the linked ports, only while someone is watching, and within the poller's existing limits.

## Where it lives

- **On the site page** (`/network/sites/:id`): a **Map** card at the top of the right column **(call)**. It shows the map read-only at card size, with **Open map**.
- **The map page** (`/network/sites/:id/map`): the map fills the page. A toolbar holds:
  - **Realistic | Simple**, remembered per browser, as on the device page
  - **Edit**, for people who may edit
  - **Fullscreen**
  - **Share**, for admins
  - a live indicator ("Live" with a green dot, or "Updated 2 min ago" or "Reconnecting…")
- **Fullscreen** is the wall display: no app chrome, scaled to fit the screen, reconnecting by itself. It uses the dashboards' fullscreen display shell.
- **Dashboard widget** (`site_map`): pick a site, and it shows that site's map read-only, fitted to the widget.
- **Navigation:**
  - The map page's back link goes to the site.
  - Each switch's or router's device page gets "Show on site map".

## What's on the map

### Devices

- **Which devices:** the site's monitored **routers and switches**, with firewalls counted as routers.
  - A firewall or switch Sentinel has filed as Other can be put on the map by an editor ("Add a device to the map") **(call)**.
  - APs, NVRs, UPSes and other devices never appear on their own.
- **Realistic view:**
  - A device with a front-panel drawing is drawn as its front panel, scaled down, with live port lights as on its device page. Today that's the UDM-SE, the USW-Pro-48-PoE, the ES-16-XG, the EdgeSwitch family and the Catalyst 4500-X-16.
  - Every other device is a card showing its type icon, name, model and status.
- **Simple view:** every device is a card.
- **Status:**
  - Up devices look normal.
  - Down devices have a red outline and "Down 4 min".
  - Paused devices are greyed.
  - Error devices have an amber outline with the error as a tooltip.
- **Hiding:** editors can hide a device from the map, for example a lab switch. A hidden device is listed under Edit → "Hidden (1)", where it can be shown again.

### Links

Links come from four places. When several describe the same cable, they make one link, and its panel says where it came from.

1. **Drawn by hand** in edit mode.
2. **Port settings:** the phase 2 "Network link" neighbour on a port (`device_interfaces.neighbor_device_id` and `neighbor_if_index`).
3. **LLDP:** an entry in `l2.neighbors` whose neighbour is a monitored device at this site.
   - **Finding the neighbour device:** the neighbour's chassis ID matches one of the device's interface MACs. Failing that, the neighbour's system name matches the device's `sys_name` or `name`, ignoring case.
   - **Finding the neighbour's port,** in this order:
     1. if the neighbour's port ID or port description equals an interface's `ifName`, `ifDescr` or `ifAlias`, it's that port
     2. if it reads "Port N", it's the port numbered N on the faceplate. TS5410R81E reports QuantumLink's port as "Port 5", while QuantumLink names it "0/5".
     3. if the neighbour's port ID is one of that device's interface MACs, and only one physical port has that MAC, it's that port.
        - VLAN sub-interfaces and bridges sharing the MAC don't count. Quantum-Gate's eth10 shares its MAC with a dozen `eth10.N` sub-interfaces.
        - A device that gives every port the same MAC can't be matched this way. QuantumLink does that.
     4. otherwise the link attaches to the device itself, with no port
   - **Seen from both ends:** the two entries make one link, with both ports known.
   - **Only map devices count:** an LLDP neighbour that isn't a router or switch on the map is ignored.
   - **On the owner's data:**
     - QuantumLink's port 51 connects to Quantum-Gate's eth10 (port 11). That's the one link on the home map.
     - QuantumLink's LLDP also sees MBU7IW, TS5410R81E and KitchenAP on ports 4, 5 and 48. They're not switches, so they're not drawn.
     - TS5410R81E reporting QuantumLink's port as "Port 5" stays as test data for the "Port N" rule.
4. **MAC table guess:** one of a monitored device's interface MACs is learnt on a switch's edge port at this site in the last 24 hours, as piece B places MACs.
   - This is how a switch that doesn't send LLDP shows up, such as a basic managed switch with LLDP turned off. Only a link between two map devices is guessed; a guess never brings an AP or other device onto the map.
   - A guess is drawn **dashed with a "?"** until an editor clicks **Keep** (it then becomes a hand-drawn link) or **Hide**.
   - A guess is never drawn where LLDP or a port setting already links that device.

**Links that go away:** a link found by LLDP or a guess that hasn't been seen for 24 hours is drawn faded, with "not seen since …". After 7 days it's dropped **(call)**. Hand-drawn links stay until someone deletes them.

**Hiding:** a hidden found link stays hidden even when found again, until it's shown from Edit → "Hidden links".

### Live traffic

- **Which port's figures:** each link takes its figures from one bound port. That's the switch end when there's a choice, then whichever end has a known speed.
- **Colour:** by the busier direction's utilisation against that port's **busy threshold**. That's the existing per-port setting, 80% by default.
  - Below 1%: slate
  - From 1% up to half the busy threshold: green
  - From half the threshold up to the threshold: amber
  - At or above the threshold: red
- **Width:** by link speed, in steps: 100 Mb, 1 Gb, 2.5 Gb, 10 Gb, 25 Gb and above. An unknown speed draws at 1 Gb width, dashed at the ends.
- **Flow:**
  - Dots move along the link in each direction. Their speed follows that direction's rate on a log scale, and an idle direction has no dots.
  - When the browser asks for reduced motion, there is no animation; a small arrow per direction shows which way is busier.
- **Problems:**
  - A port that's down draws a red line with no flow.
  - A device that's down greys its links and draws them dashed.
  - With no sample for three updates (30 s), the link turns grey with "no data".
- **Hover:** a link's tooltip reads "QuantumLink port 51 ↔ Quantum-Gate eth10 · ↓ 412 Mb/s ↑ 38 Mb/s · 1 Gb".

### Clicking

- **A link** opens a side panel with:
  - both ends, as device and port, each linking to its port page
  - speed and the current in and out, with utilisation
  - the last hour from the stored port history, as a sparkline
  - where the link came from
  - for editors: **Hide**; **Keep** on a guess; **Change ports** and **Delete** on a hand-drawn link
- **A device** opens a side panel with:
  - status, model, address and uptime
  - its links
  - **Open device**

## Editing

- **Who:** admins, and people the site is shared with as editable, the same people who edit the site's profile.
- **Edit mode:**
  - Drag devices. Positions snap to an 8-unit grid **(call)**.
  - **Add link:** click a device, pick its port (from a list, or by clicking the port on a realistic drawing), then click the other device and optionally pick its port.
  - Hide or show devices and links.
  - **Reset layout** goes back to the automatic layout, after a confirmation on the page.
- **Saving:** each change saves as it's made, with no Save button **(call)**. Other people watching the map see it move.
- **Phones:** view-only, with pan and zoom **(call)**.

## Automatic layout

It's a tree, laid out the way the second mockup draws it:
- **Rows:**
  - Routers at the top.
  - Under them, the core: the switch or switches the router plugs into.
  - Then the distribution switches, the core's switch neighbours, in one row.
- **Columns:** each distribution switch heads a column. Its access switches stack under it, one per row, in the order of the distribution switch's ports.
- **Deeper switches:** a switch hanging off an access switch (a daisy chain) goes straight below its parent in the same column.
- **Tier labels:** the rows are labelled Router, Core, Distribution and Access in faint text at the left. The labels come from position in the tree, not from a setting **(call)**.
- **Unlinked switches** sit in a row at the bottom, under "Not linked".
- **Small sites:** with no distribution layer (a router and one or two switches), the tree is just as many rows as it needs.
- **How links are drawn:**
  - Links between rows are curves.
  - A link from a distribution switch down to its access switches runs down a lane to the left of the column, then across and into the port from above, with the lanes of one column side by side. This keeps 20 switches readable.
- **Folding:** a switch with others below it shows "▾ 5 below". Clicking it folds that branch into one line, such as "5 switches folded · 1 down". The folding is remembered per browser **(call)**.
- **Scale:** realistic drawings are scaled by tier. Routers and core are the largest, then distribution, then access. Fit, the mouse wheel and the zoom buttons move between the overview and a close look at one branch.
- **After an edit:** once an editor moves anything, every position is saved. A device added later goes into its place in the tree, or the bottom row.

## Live updates and fast polling

- **The stream:**
  - The map page opens `GET /api/v1/network/sites/:id/map/stream`, a server-sent event stream.
  - The first event is the whole map: devices, links, statuses and rates.
  - After that, an update about every 10 seconds carries the linked ports' rates and the devices' statuses, plus a "map changed" event when someone edits it.
  - It uses the same framing and helpers as the network tools' run stream, which already passes through the frontend's proxy.
- **Fast polling:**
  - While anyone is watching a site's map, the linked ports on that site's devices are read every 10 seconds **(call)**. Watching means an open stream, a public map, or a dashboard widget that asked in the last 60 seconds.
  - Each device gets one SNMP request for just those ports' 64-bit octet counters and up/down state.
  - Rates come from the same calculation the regular poll uses (`portmon.ComputeRates`).
- **Limits:**
  - At most 64 ports per device per fast poll **(call)**.
  - One fast poll per device at a time, within the poller's existing SNMP concurrency.
  - A device that's down, paused, or backing off after errors isn't fast-polled.
- **Stopping:** fast polling for a site stops 60 seconds after its last viewer leaves.
- **Reconnecting:**
  - The browser reconnects by itself, and the toolbar shows "Reconnecting…".
  - After 30 seconds without updates, the traffic turns grey.
  - When it reconnects, the full map arrives again.

## Sharing

- **Who sees it:** anyone who can see the site sees its map. Devices the viewer can't see aren't drawn, though today site access covers every device at the site.
- **Public link:**
  - Admins can create a read-only public link, as for dashboards: `/public/maps/:token`, with its own stream under `/api/v1/public/maps/:token/stream`, rate-limited by address.
  - It shows device names, statuses and traffic. It leaves out IP addresses, MACs, serial numbers and the connected counts **(call)**.
  - Admins can turn the link off, which ends any open public streams.
- **Dashboard widget:** it refreshes through the dashboard's existing widget-data request every 10 seconds, including on public dashboards, so it needs no new kind of access **(call)**.

## Data

Migration `064_site_maps.sql`, in `public` so it's backed up:

| Table | Holds |
|---|---|
| `site_map_positions` | `site_id`, `device_id`, `x`, `y`, `hidden`. Primary key (`site_id`, `device_id`). Foreign keys cascade on delete. |
| `site_map_links` | Hand-drawn links and overrides on found links: `id`, `site_id`, `a_device_id`, `a_if_index` (nullable), `b_device_id`, `b_if_index` (nullable), `kind` (`manual` or `hidden`), `created_by`, `created_at`. "Keep" on a guess writes a `manual` row. |
| `site_map_public_links` | `site_id` (primary key), `token` (unique), `created_by`, `created_at`. |

- Found links are worked out when the map is read. They're not stored.
- Positions are in map units, so the layout doesn't depend on screen size.
- **Audit:** "site map edited", "site map public link created" and "site map public link removed".
- **Moves and deletes:** when a device moves to another site, its rows on the old site's map are removed. A deleted device's rows go by cascade.

## Testing

- **Go unit tests:**
  - **Link finding,** on the owner's real data:
    - QuantumLink ports 4, 5, 48 and 51
    - TS5410R81E ↔ QuantumLink "Port 5", making one link with both ends
    - the chassis-ID and system-name fallbacks
    - a guess between two switches, and no guess ever bringing a non-switch onto the map
    - combining links from several sources, with hidden overrides applied
  - **Layout:**
    - the rows, the columns per distribution switch and daisy chains
    - small sites with no distribution layer
    - the bottom row for unlinked switches
    - folding
- **Database tests:**
  - positions, links and overrides, including cascades and site moves
  - access for admins, editable sharers and read-only sharers
  - public links
- **Fast poller,** against the SNMP simulator:
  - rates, and viewers starting and stopping it
  - stopping after 60 seconds idle
  - skipping down and backing-off devices
  - the 64-port cap
- **Stream:**
  - the full map first, then updates
  - a signed-in user versus a public token
  - turning the public link off ends the stream
- **Headless browser checks** against a mocked API:
  - the realistic and simple views
  - link colours and dashes, including a guess and stale data
  - dragging saves a position
  - adding and hiding a link
  - reduced motion
  - phone view, fullscreen, and the widget
- **The frontend gate passes,** and the work is pushed to `dev` and the sandbox.
- **Mockups come first,** drawn with the owner's real devices, with any simulated problem labelled.

## Left to the builder

- The SVG composition of the map, and pan and zoom (pointer events, no new library preferred).
- How port anchor points are taken from the realistic drawings: they already place each port, so expose those positions rather than hand-listing them.
- The exact colours and the flow animation, within the design system.
- How the stream hub is shared between viewers of the same site.
