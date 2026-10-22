# Site maps: bundles, calmer zoom, ports marked automatically — design

**Date:** 2026-10-19
**Status:** mockup approved by the owner 2026-10-19 ("Looks good"). This document writes up what the mockup showed, plus the parts it couldn't show (collection, storage, rules).
**Builds on:** `2026-10-19-site-maps-design.md` (piece C, on dev at 504c209).
**Reference images:** the owner's screenshot of a work site after the site maps update, and the approved mockup of the same site with this design (one with a cable down and its panel open). Removed on 2026-10-22: they showed the owner's work network, and this repository is public.

The mockup ran the real frontend with prototype changes, in a scratch copy (not committed). Its code is a guide, not the implementation: build each part test-first.

## Why

The owner opened a site map at work and said: "I think we need to do some work on the map when it comes to LAG groups and things like that." Then: "the movement of the map is super fast. It is hard to control when zooming in and trying to move around." Then they asked: "doesn't LLDP and CDP know which ports are plugged into which. Could it identify which ports are network links and WAN ports automatically and mark them?"

What the screenshot shows:
- **Each LAG member is drawn as its own cable:** two lines CORE-ES48-A → CORE-ES48-B (crossing each other), two CORE-ES48-A → core-es48-c, two LAB-ES48 → LAB-USW48. Each line is coloured against one member's speed with that member's share of the traffic, so a busy bundle looks calm. A failed member just goes grey, and nothing says the bundle is running at half capacity.
- **LAB-ES48 → LAB-USW48 goes the long way round:** out of the switch, all the way left, then back in from above. The switch below is directly underneath.
- **Busy uplinks look idle:** grey means under 1% of the link's speed, so a 10G link carrying up to 100 Mb/s draws grey while dots run along it.

## Goals

1. **Bundles:** cables between the same two switches are drawn as one bundle, a LAG or not.
2. **Straight drops:** the first switch under a distribution switch is linked straight down.
3. **Idle means idle:** grey only under 1 Mb/s.
4. **Pan and zoom you can control:** with a trackpad and with a mouse, and the map never jumps back to "fit" by itself.
5. **Ports marked automatically:** network links and WAN ports, from LLDP, CDP, LAG membership and the router's routes and addresses, with anything set by hand winning.
6. **The internet on the map:** an "Internet" tag on each router, with its WAN port's live traffic.

## Non-goals

- Per-VLAN MAC tables on Cisco (the rest of the deferred B2 piece). Only CDP is taken from B2.
- Configuring LAGs, or alerting on LAG state beyond what the map and port incidents already do. A member down still raises a port incident if that port is important, as today.
- Polling LAG interfaces' own counters: the bundle's traffic is its members' sum, which the fast poll already reads.
- New drawings (the Cisco drawings still wait on photos).
- STP state (a blocked redundant cable is not shown differently).

## 1. Bundles

**Which links bundle.** After `sitemap.Find` has merged and overridden links, links between the same two devices with a port at both ends and not a guess form one bundle, if there are two or more. Two cables to the same switch that aren't a LAG are bundled too, labelled "2 cables". Guesses and port-less ends never bundle. **(call)**

**What a bundle is in the API.** `MapLink` gains:
- `members []MapLink`: each cable, A/B oriented like the bundle (the same device first), sorted by A's port order.
- `lag *{a, b string}`: the LAG's name at each end (the aggregator's ifName, e.g. "lag 1", "3/1", "Po1"), when every member at that end belongs to the same aggregator (section 5). Null means "not a LAG".

The bundle's own fields:
- `key`: `bundle:` + its member keys joined with ",", in member order.
- `a`/`b`: the first member's ends.
- `speed_bps`: the sum of the members' speeds.
- `rate_end`: `b`.
- `sources`: the union of its members' sources.
- `stale`: true only when every member is.
- `manual_id`: null.

A single link is unchanged: no `members`, no `lag`.

**Traffic.** `MapTraffic.links` stays keyed by member key, so the fast poll is unchanged. The browser adds each bundle's members up, oriented to the bundle:
- `in_bps`/`out_bps`: the sums.
- `oper_up`: false only when every member is down.
- `at`: the newest member's.

The bundle has `members_down` (how many members' ports are down) and a live speed (the sum of the members that are up), which utilisation is measured against.

**Hide and keep.** Hiding a bundle hides each member (one override row per member key). The hidden list in edit mode shows the bundle as one entry; showing it shows every member. **(call)** Manual links between the same two ports bundle like any other.

**Drawing** (as the mockup):
- **The line:** a bundle is two rails with a dark core, `linkWidth(sum of speeds) + 4` wide, coloured by the summed traffic.
- **At each end:** a bar runs across the member ports, just outside the faceplate (box edge ± 7 map units), with a 2-unit stub from each member port to the bar. The bundle's line starts and ends at the bar's middle. A down member's stub is red and dashed. When members sit one above the other (49 over 50), the bar has no width and the line just leaves from between them. That's expected.
- **The badge** at the middle: "LAG · 2 × 10G", or "2 cables · 2 × 1G". When a member is down, the badge reads "LAG · 2 × 10G · 1 of 2 down" with an amber pill, and the whole bundle is amber whatever its traffic.
- **Every member down:** drawn as a down port is today.
- **Simple view:** the same rails and badge, ends on the cards' slots (one slot per bundle, not per cable).
- **Flow dots:** along the line only, not the stubs.
- **Legend:** adds "LAG or parallel cables, as one".
- **Toolbar count:** a bundle counts as one link. Write "6 links".

**Panel.** Clicking a bundle opens a panel titled "LAG" (or "Cables"):
- the two devices
- "LAG: lag 2 on CORE-ES48-A · lag 1 on core-es48-c", or "2 cables, not a LAG (each runs on its own)"
- when a member is down: "1 of 2 cables down: running on 10 Gb of 20 Gb", in amber
- a table with a row per member: each end's port (linking to its port page when signed in), and its traffic each way, or "down" in red
- then Speed, the traffic each way, and Busy threshold, as for a link
- no last-hour chart (it would be one member's)
- Hide, as for a link.

**Layout.** A bundle is one neighbour (today's `children()` de-duplication already handles that), and one lane in a lane route.

**Public map.** Members get stand-in keys and ids like links. LAG names are shown, like port names.

## 2. Straight drops

In `MapCanvas`'s route choice: a link from a distribution switch to an access switch in the same column takes the **lane** only when that access switch is not the first one under it (`row > 0`). The first one is linked with the **chain** route, straight down. Lanes exist so links reach lower rows without crossing the switches in between, and the first row has none.

## 3. Idle colour

`linkColour` gets the busier direction's rate:
- **idle (grey):** under 1 Mb/s (`IDLE_BPS = 1e6`) either way
- **ok (green):** any traffic under half the busy threshold
- **warn / bad:** as today

The legend reads "idle (under 1 Mb/s)" and "traffic, under half the busy threshold".

## 4. Pan and zoom

Three causes, all fixed:
1. **Trackpads.** Every wheel event zoomed 12%, and a trackpad sends dozens a second. New wheel handling:
   - **Pinch** (`ctrlKey`, which also covers Ctrl + wheel): zoom by `exp(-deltaY × 0.01)`.
   - **Mouse wheel:** a notch is `deltaMode` lines or pages, or a whole-number `deltaY` of 50 or more with no `deltaX`. Zoom by `exp(-deltaY × 0.001)`, about 10% a notch.
   - **Everything else** (two-finger scroll): pan by `deltaX`/`deltaY`.
   - **Limits:** each event's factor is clamped to 0.8–1.25, and zoom to 0.15–2.5 (was 0.1–3). Pinch on a touch screen gets the same zoom limits.
   - Line deltas count as 16 px, page deltas as the viewport height.
2. **Re-sent maps.** The server re-sends the map every 5 minutes and after every edit, and each copy re-fit the camera, because `fit` depended on the bounds object. Fit only when the bounds' values change (a key of the four numbers), when the view (realistic/simple) changes, and on the Fit button.
3. **Opening a panel.** It re-fit the map (`fitKey` included whether a side panel was open) and threw away the zoom. It no longer does. The viewport narrows and the camera stays.

Resizing the window still fits, since the ResizeObserver calls `fit` as today. **(call)** Keep it: a wall display that changes size should re-frame.

Tests: unit-test the wheel decision (pinch, notch, pan) as a pure function in `utils/siteMap.ts`, e.g. `wheelAction(e) → {kind:'pan',dx,dy} | {kind:'zoom',factor}`. A harness test checks that the camera survives a re-sent identical map and opening a panel.

## 5. What's collected (the five-minute L2 poll)

`L2Monitor.PollL2` already walks MAC, LLDP and ARP tables every 5 minutes. It also reads the following, each walk on its own (one failing leaves the rest), each with a short timeout. Checked on the owner's home gear on 2026-10-19 with a read-only walk.

**LAG membership** (switches and routers): IEEE8023-LAG-MIB `dot3adAggPortAttachedAggID` (`1.2.840.10006.300.43.1.2.1.1.13`), indexed by member ifIndex, valued with the aggregator's ifIndex (0 when not attached).
- QuantumLink (USW-Pro, the same FASTPATH family as the work EdgeSwitches) answers with 52 rows (no LAGs set up there, so all 0), and the aggregators are its type-161 interfaces 66–91.
- IF-MIB `ifStackTable` returned nothing there.
- Fall back to `ifStackStatus` (`1.3.6.1.2.1.31.1.2.1.3`, index higher.lower, where the higher layer is a type-161 interface) when the dot3ad walk is empty. Cisco answers both.
- Stored per port: `device_interfaces.lag_if_index INT NULL`, rewritten for every port on a successful walk (0 → NULL); a failed walk leaves it.

**CDP** (`cdpCacheTable`, `1.3.6.1.4.1.9.9.23.1.2.1.1`, index ifIndex.deviceIndex), any device that answers:
- `.6` cdpCacheDeviceId: stored as chassis ID, and sys name after stripping a domain and a "(serial)" suffix
- `.7` cdpCacheDevicePort: port ID
- `.8` cdpCachePlatform: port descr
- `.4` cdpCacheAddress (4 bytes when `.3` cdpCacheAddressType is 1): the address

Rows go into `l2.neighbors` with `protocol = 'cdp'`. Matching to a monitored device goes by MAC, then sys name (as LLDP today), then **address equals the device's host**. Port matching is today's `MatchNeighborPort` ("GigabitEthernet1/0/1" matches ifName or ifDescr).

Be honest in the copy: CDP only sees neighbours that send CDP (other Cisco gear). The C3850 ↔ USW link is found only if the C3850 runs LLDP (`lldp run`, off by default on Cisco) or by today's MAC-table guess.

**Default route** (routers only):
- `ipCidrRouteIfIndex` under `1.3.6.1.2.1.4.24.4.1.5.0.0.0.0`: walk that subtree only, which gives the default routes' rows.
- Else `ipRouteIfIndex.0.0.0.0` (`1.3.6.1.2.1.4.21.1.2.0.0.0.0`, a GET).
- Quantum-Gate (UDM-SE) exposes only connected routes: no default route over SNMP. FortiGates usually do. Not walked: `inetCidrRouteTable` (it timed out on the UDM-SE).

**Addresses** (routers only): `ipAdEntIfIndex` (`1.3.6.1.2.1.4.20.1.2`).
- Quantum-Gate: 108.204.170.x on ifIndex 13 = eth8, its WAN port, which the owner already marked WAN by hand.
- Its other addresses are 10.1.x.1 on VLAN interfaces, 127.0.0.1, and 192.168.8.1 on a virtual interface.

## 6. Ports marked automatically

**Storage** (migration 065, the next free number; `ls` to confirm):
- `device_interfaces.role_source TEXT NOT NULL DEFAULT 'default'`, CHECK in (`default`, `user`, `auto`). Back-fill: `user` where `role <> 'access'`. Today only a person sets roles, so every non-access role is theirs.
- `device_interfaces.role_reason TEXT NOT NULL DEFAULT ''`: why an automatic role was given, in words.
- `device_interfaces.lag_if_index INT NULL` (section 5).
- `device_interfaces.wan_evidence TEXT NOT NULL DEFAULT ''`, CHECK in (`''`, `default_route`, `public_address`): what the last L2 poll found on this port, so the rules can be re-run without SNMP. The name rule needs nothing stored. A failed walk leaves the previous value; a successful one rewrites every port of the device.
- `l2.neighbors.protocol TEXT NOT NULL DEFAULT 'lldp'`, CHECK in (`lldp`, `cdp`).
- `l2.neighbors.address TEXT NOT NULL DEFAULT ''`.

`role` stays the role everything reads (reports, endpoint placement, traffic labels, the map), so no consumer changes. SaveInventory keeps omitting `role`, and also omits the new columns.

**The rules**, run for a device at the end of each L2 poll, from what's fresh (seen in the last 7 days, the same window the map drops links after):
- **Network link (`uplink`):**
  - the port has an LLDP or CDP neighbour that matches a monitored **router or switch**, or
  - it's a LAG member whose aggregator has such a member, or
  - it's an end of a manual map link.

  APs, NAS and other devices don't make a port a network link. They're endpoints, as the owner decided for the map. **(call)**
- **WAN (`wan`)**, routers only, in this order, first rule that finds a port wins:
  1. The physical port the default route goes out of.
  2. A physical port, up, with an IPv4 address that is not private (10/8, 172.16/12, 192.168/16), loopback, link-local or 0.0.0.0. CGNAT (100.64/10) counts as WAN: an ISP can hand that out. A VLAN or virtual interface never counts.
  3. A physical port, up, named `wan`, `wan1`, `wan2`, … (FortiGate), by ifName.

  More than one port can be WAN (dual WAN), within the winning rule.
- **Writing:**
  - Only ports with `role_source` `default` or `auto` are touched.
  - A port with evidence gets `role`, `role_source = 'auto'` and `role_reason`.
  - An `auto` port with no evidence left goes back to `access` / `default` / `''`.
  - WAN beats network link on the same port.
  - Never touch `neighbor_device_id` / `neighbor_if_index` (the person's "Network link to …" setting).
  - No audit row: automatic changes aren't a person's edit. Logged at debug only. **(call)**
- **Reasons**, word for word:
  - "Found by LLDP: CORE-ES48-A port 49"
  - "Found by CDP: LAB-C3850 Gi1/0/1"
  - "Member of lag 1 to CORE-ES48-B"
  - "Drawn on the site map"
  - "The default route goes out this port"
  - "Has a public address"
  - "Named wan1"

**Editing** (PortDetail's "Connects to"):
- **A new first option, "Automatic".**
  - Its hint shows what was found ("Found: Network link · Found by LLDP: …", or "Nothing found: Endpoint").
  - Choosing it sends `role: null`, which sets `role_source = 'default'` and re-runs the rules for that device right away (from stored evidence, no SNMP).
- **Choosing any role sends it as today** and sets `role_source = 'user'`, including Endpoint: a person can say "this is not a network link" and it sticks.
- The PortPatch comment changes ("null = back to automatic").
- The port's role also gains `role_source` and `role_reason` in the ports API.

**Showing it:**
- **Realistic drawings:** a port with role `uplink` gets a solid 2 px sky frame (`#38bdf8`), and `wan` a violet one (`#a78bfa`) with a small "WAN" under it. A frame is never the yellow/red alert pulse, so the two can't be confused.
- **Simple faceplate (`PortCell`):** the same two frames.
- **Port popover:** a line under the state, "Network link to CORE-ES48-A · port 49" (or "Internet (WAN)"), and under it, smaller, the reason, or "Set by you".
- **Port table "Connects to" column:** the role, with "· auto" when automatic.

**What it improves elsewhere** (no code needed, worth a line in STATUS.md):
- WAN ports' traffic reads Download/Upload the right way round.
- Reports group ports by role.
- Endpoint placement skips network-link ports.

## 7. The internet on the map

Each router on the map with WAN ports gets a violet "Internet" tag beside it, one per WAN port: a globe, "Internet", the port name, and live ↓/↑ traffic. Its tooltip is the reason it's WAN.
- **Router drawn as a card:** the tag sits to the card's left, joined by a short violet line.
- **Router drawn realistically:** the tag sits to the left of the drawing, and a violet line runs from it to the WAN port, like a link stub.

The fast poll also reads the map routers' WAN ports: `BoundPorts` includes them, with a link key of `wan:<deviceId>:<ifIndex>`. Traffic arrives in `MapTraffic.links` under that key. `MapDevice` gains `wan []{if_index, name, speed_bps, reason}`.

On the public map, the tag shows "Internet" and the traffic, without the port name or reason. **(call)**

## Testing

- **Go:**
  - Bundling in the snapshot: two members become one bundle; LAG names only when every member at an end shares an aggregator; guesses never bundle; hiding a bundle hides each member; public stand-ins.
  - Parsers: dot3ad and ifStack, CDP rows (device ID cleaning, address bytes), default route and addresses.
  - The rules: each WAN rule's order; user wins; auto clears; WAN over uplink; APs don't count.
  - Migration back-fill.
  - The L2 poll writing the new columns: a DB test with the simulator.
  - The leak guard covering the WAN tag.
- **TS (check script):** the bundle aggregation (sums, orientation, members down, live speed), `linkColour` with a rate, `wheelAction`, and the route choice (row 0 is a chain).
- **Playwright harness (the work-site fixture from the mockup):** bundle badge and amber state, bundle panel, straight drop, camera survives a re-sent map and a panel opening, port frames and popover line, Internet tag on card and on a UDM-SE drawing.
- **The owner's home lab is the real check.** After deploy, eth8 should come out WAN by the public-address rule and eth10 / port 51 network links by LLDP. They're marked by hand already, so those stay `user`. Check with a dry run of the rules against the sandbox DB, and report what it would have set.

## Left to the builder

- Component and file names, and whether the bundle aggregation lives in `utils/siteMap.ts` (it should: pure and checkable).
- Exact badge sizing and positions, within what the mockup shows.
- How the rules runner is shaped (a function over a device's ports, neighbours, LAG rows and addresses, returning the role changes, is the testable shape).
