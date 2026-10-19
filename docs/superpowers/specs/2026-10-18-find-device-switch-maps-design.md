# Find a device, and switch maps — design

Status: written 2026-10-18 from a conversation with the owner. This is
**piece B** of four: A was back navigation (done, `56d8dbd`); C will be site
maps and D the org map, each designed separately later. Choices the owner
made are under "Decided by the owner". Everything else is Claude's call and
is marked **(call)**.

## Purpose

The owner's most common hunt is "which port is this PC, phone or camera
plugged into?". Today Sentinel can't answer it: it knows the switches' ports,
but not what's on them.

Piece B answers it two ways:
- **Find a device:** type a name, IP or MAC, and get the switch and port,
  or the AP for a wireless device.
- **Switch maps:** open a switch and see everything plugged into it, with
  each item's name, IP, MAC and maker.

The site map (C) and org map (D) will build on the data collected here.

### Decided by the owner

- **They usually search by name;** IP is the easy second.
- **The switch map** shows the devices plugged into that switch. It leaves
  out the uplink to the router, and links to other switches.
- **APs are shown** on the switch map. A device on wireless is shown as
  connected through its AP. Searching for one gives the AP and the AP's
  switch port.
- **Each device shows** its MAC address, IP address and maker.
- **Home gear and EdgeSwitches first; Cisco is a follow-up** (2026-10-18).
  Cisco Catalysts often keep their MAC table per VLAN, which needs
  per-VLAN SNMP reads, and may announce neighbours with CDP. That waits
  for a read-only walk of a work Cisco.

### Checked on the owner's gear (2026-10-18, read-only SNMP walks)

- **QuantumLink (USW-Pro-48-PoE):**
  - **MAC table:** only the newer VLAN-aware table (Q-BRIDGE
    `dot1qTpFdbPort`) is filled: 88 rows giving VLAN, MAC and bridge port.
    The old one (`dot1dTpFdbPort`) is empty.
  - **Bridge port to ifIndex** (`dot1dBasePortIfIndex`): 78 rows.
  - **LLDP:** 4 neighbours: the NAS, two APs (MBU7IW and KitchenAP), and
    Quantum-Gate.
- **Quantum-Gate (UDM-SE):**
  - **ARP:** `ipNetToMediaPhysAddress` has 104 IPv4 entries;
    `ipNetToPhysicalPhysAddress` has 285, including IPv6.
  - **No MAC table and no LLDP,** which is expected for a router.
- **Not checked yet:** the work switches (EdgeSwitch, Cisco). Cisco may run
  CDP rather than LLDP. See "Left open".

## Goals

- **Search:** a name, IP or MAC (or part of one) finds the device. Results
  show:
  - where it is: the switch and port, or the AP and that AP's switch and
    port
  - its VLAN, IP, MAC and maker
  - when it was last seen
- **A device that has gone quiet is still findable** at its last place,
  with the time it was last seen. That's often when someone goes looking.
- **Every monitored switch has a switch map:** its end devices and APs
  around it, plus a list view to sort and filter.
- **Names come from DNS where it knows them,** and from a name the user
  types otherwise.

## Non-goals

- Site maps, the org map, link drawing and LLDP-suggested links: pieces C
  and D. B only collects and stores LLDP neighbours, for its own use and
  for C.
- **SSID, signal strength or radio band** for wireless devices. SNMP
  doesn't give them; that waits for the UniFi controller (network phase 7).
- DHCP lease reading, and network access control or port security.
- **Alerts** when a device moves or disappears.
- **Real-time freshness.** Data is as of the last L2 poll, a few minutes old.
- **Finding devices on networks Sentinel doesn't poll,** for example where
  no router's ARP table covers that VLAN.
- **Cisco MAC tables and CDP** (follow-up "B2"): per-VLAN reads of the MAC
  table, SNMP context or community indexing, and CDP neighbours.
  - Until B2, a Cisco switch's L2 poll reads only what it offers the
    standard way: LLDP, if enabled, and ARP. It gets no Connected map when
    its standard MAC table is empty.
  - Devices behind it are placed wherever a UniFi or EdgeSwitch switch
    sees them.

## Anti-goals

- **Saying a device is plugged into a port when it's really behind another
  switch or an AP.** The placement rule below exists to prevent this.
- **Leaking across sites.** A user who can't see a site must not find its
  devices, MACs or names through search.

## How a device is found and placed

### What's collected

The **L2 poll** runs for every enabled device whose status is `up`. It runs
every 5 minutes **(call)**, independently of the 60-second stats poll, and
reads three things:

1. **The MAC table:**
   - `dot1qTpFdbPort` (`1.3.6.1.2.1.17.7.1.2.2.1.2`), indexed by VLAN
     then MAC, giving the bridge port
   - falling back to `dot1dTpFdbPort` (`1.3.6.1.2.1.17.4.3.1.2`) when the
     Q-BRIDGE table is empty
   - bridge ports mapped to ifIndex through `dot1dBasePortIfIndex`
     (`1.3.6.1.2.1.17.1.4.1.2`)

   Only entries learned on ports are kept (`dot1qTpFdbStatus` = learned(3)).
   Static entries and the switch's own MAC are skipped.
2. **LLDP neighbours** (LLDP-MIB `lldpRemTable`, `1.0.8802.1.1.2.1.4.1.1`):
   - the local port number (mapped to ifIndex through `lldpLocPortTable`,
     or the bridge port mapping)
   - the chassis ID
   - the system name (`.9`)
   - the port ID (`.7`) and port description (`.8`)
   - the enabled capabilities (`.12`): bridge, router, WLAN access point,
     telephone
3. **ARP:** `ipNetToPhysicalPhysAddress`, falling back to
   `ipNetToMediaPhysAddress`. Only IPv4 is kept **(call)**: IPv6 neighbour
   entries add noise and IPv6 search isn't asked for.

**Rules for the poll:**
- Each walk is independent; one table failing doesn't stop the others.
- A device that answers none of them is simply recorded as having no L2
  data.
- Walks use the device's own SNMP credential and timeouts, as the other
  polls do.

### Where a MAC is placed

Each port is classed in this order:
- **AP port:** the port's LLDP neighbour advertises the WLAN-AP
  capability, or is a monitored device of type `access_point`.
- **Link port:** the LLDP neighbour advertises bridge or router; or the
  port has a port link to another device, or the role `uplink`; or more
  than **8** MACs were learned on it in the last poll **(call)**.
  - **Exception:** a port whose LLDP neighbour advertises neither bridge
    nor router (a NAS or a server, say) is an edge port however many MACs
    it shows. A host running containers or VMs then stays on the map
    **(call)**.
  - **Unmonitored downstream:** a link port classed only by the 8-MAC
    rule, with no monitored device or LLDP switch on its far end, usually
    has an unmanaged desk switch on it. On the map it shows as a group,
    "*N* devices behind port *n*", which opens to list them. Search
    results for those devices highlight that group **(call)**.
- **Edge port:** anything else.

A MAC is then placed by the latest poll of every switch that saw it, using
the first of these that applies:
1. **On an edge port: wired**, on that switch and port. If two edge ports
   saw it, the most recent sighting wins.
2. **On an AP port: wireless,** through that AP, on that switch and port.
3. **Only on link ports:** shown as "behind *switch* port *n*", with the
   switch closest to the device. For this piece, that means the switch
   whose port saw the fewest MACs.
4. **On no switch, only in an ARP table:** for example a device plugged
   straight into the UDM-SE's own LAN ports, which report no MAC table. It
   shows as "seen by *router*, port unknown". Its site, for access, is the
   router's site. The router's Connected section lists these devices
   **(call)**.

### What each device record holds

- **MAC and maker:** the maker comes from the IEEE OUI list built into
  Sentinel, with nothing looked up online **(call)**.
- **IPv4 addresses:** every ARP entry for that MAC, from any device the
  viewer can see **(call)**. One router often holds the ARP table for
  several sites' VLANs, so the router doesn't have to be in the device's
  own site.
- **Name:**
  - the user's own name for it, if set
  - else its reverse-DNS name, using the Sentinel server's resolver,
    cached for 24 h
  - else none
- **Kind,** guessed for the icon **(call)**:
  - camera: a list of camera makers (Hikvision, Dahua, Axis, Ubiquiti
    Protect cameras)
  - phone: Yealink, Polycom/Poly, Grandstream, Cisco phones, or LLDP
    telephone capability
  - printer: HP, Brother, Lexmark, Canon, Epson, Xerox, Ricoh, Kyocera
  - AP: from LLDP
  - otherwise a generic computer/device

  The guess is a hint only; the maker is always shown alongside it.
- **Location:** switch, port, VLAN, and wired or through *AP*.
- **First seen and last seen.**

**Retention:** a MAC not seen for **30 days** is deleted **(call)**. Its
sightings older than that go with it.

## Storage

The next free migration number at build time (`063` is free now) holds five
tables, all with `TIMESTAMPTZ`:
- **`l2_sightings`:** `device_id`, `if_index`, `vlan`, `mac`, `first_seen`,
  `last_seen`. One row per MAC per port, updated each poll.
- **`l2_neighbors`:** `device_id`, `if_index`, `chassis_id`, `sys_name`,
  `port_id`, `port_descr`, `capabilities`, `last_seen`.
- **`l2_arp`:** `device_id` (the router), `ip`, `mac`, `last_seen`.
- **`endpoint_names`:** `mac` (unique), `name`, `updated_by`, `updated_at`.
  The user's names.
- **`dns_names`:** `ip` (unique), `name`, `resolved_at`. The reverse-DNS
  cache.

**Backups:**
- `endpoint_names` is configuration, so it is included in backups.
- The other four are observations that rebuild themselves within one poll.
  Excluding them keeps backups small **(call)**. The backup today dumps
  `public.*`, so the build must either exclude these tables explicitly or
  put them in their own schema, as metrics are.

**Placement is worked out when a device record is read,** from the latest
rows, not stored. It's cheap at this scale: 88 MACs on one switch, so
thousands at a site.

## API

All routes follow site access. A caller sees only the devices, MACs and
names on sites they can see, at readonly or above. A device's site is the
site of the switch that placed it, or, for a device only a router knows
(rule 4), the router's site.

| Route | Does |
|---|---|
| `GET /network/endpoints/search?q=` | Up to 50 results. Matches the user's name, DNS name, IP, MAC (any separators) or maker, case-insensitive and partial. When `q` looks like a hostname and nothing in Sentinel matches it, the server also does a forward DNS lookup and searches by the IPs it gets back **(call)**. |
| `GET /devices/:id/endpoints` | The switch map's data: that switch's AP ports and edge-port devices with their records, plus each AP's wireless clients, leaving out link ports. |
| `GET /network/endpoints/:mac` | One device's full record and its recent sighting history (the last 20 places and times). |
| `PUT /network/endpoints/:mac/name` | Sets or clears the user's name. Admins, and editable sharers of the site the device is placed on **(call)**. Audited. |

## Frontend

### Find a device

A search box in the top bar of every page, placeholder "Find a device by
name, IP or MAC" **(call)**:
- Results drop down as you type (debounced).
- Each result shows the name or MAC, IP, maker, the location line ("Core-SW
  port 14", or "via KitchenAP, Core-SW port 47") and when it was last seen.
- Choosing a result opens the switch's map with that device highlighted.
  The back link works as in piece A.

### The switch map

The switch map is a new section on the device page for switches and
routers, "Connected", with **Map | List** views **(call)**. It takes the
device page's place for this; there's no separate page.

- **Map:**
  - the switch in the centre, its end devices and APs around it, each
    labelled with port, name, IP and maker, with a kind icon
  - an AP shows its client count, and opens to list them
  - a device not seen in the last poll is drawn faded, with "last seen …"
  - more than about 24 items fold the rest into "+ N more · list view" **(call)**
- **List:** a table of port, name, IP, MAC, maker, VLAN, wired or through
  *AP*, and last seen. Sortable, with a filter box.
- **Clicking a device** opens a side panel with:
  - its full record and recent places
  - a name field (for those allowed to set it)
  - a link to its port
- **Clicking an AP** that Sentinel monitors opens that AP's device page.

The map keeps today's visual language (slate, and the design system's
colours) **(call)**; it's not a realistic drawing. Realistic site maps are
piece C.

## Testing

**Unit tests:**
- parsing each table: Q-BRIDGE and dot1d indexes, MACs in OIDs, LLDP
  capability bits, ARP with both MIBs
- the placement rule:
  - an edge port wins
  - an AP port gives wireless
  - only link ports gives "behind", at the switch with the fewest MACs
  - the 8-MAC threshold
  - port links and the uplink role count as link ports
- OUI lookup, MAC normalisation, and the kind guesses

**DB tests:**
- a poll writes and refreshes the rows
- retention
- site access: a stranger's search finds nothing, and a readonly sharer
  finds devices but can't name them
- naming is audited
- backups contain `endpoint_names` and not the observation tables

**Simulator:** extend `deploy/snmpsim` with a Q-BRIDGE MAC table, an LLDP
table and an ARP table, and run an end-to-end L2 poll against it.

**Frontend:** the gate, plus headless checks:
- search to a highlighted device
- an AP shows its wireless clients
- a faded last-seen device

**Owner checks on the sandbox:**
1. Search "KitchenAP", then a phone's IP. The phone shows "via KitchenAP,
   QuantumLink port …".
2. QuantumLink's Connected map shows the NAS, both APs and the wired
   devices, and no Quantum-Gate uplink.
3. Name a device, reload, and search for that name.
4. Unplug something, and after the next poll it's faded with "last seen".

## Left open

- **EdgeSwitches at work:** they're expected to fill the same Q-BRIDGE
  table as the UniFi switch, being the same firmware family. To be
  confirmed on the work instance after the build: their Connected maps fill
  in.
- **Cisco:** the follow-up B2, after a read-only walk of a work Cisco.
- **The 8-MAC threshold** and the 5-minute interval are first guesses.
  They may need tuning once real work data is seen.
