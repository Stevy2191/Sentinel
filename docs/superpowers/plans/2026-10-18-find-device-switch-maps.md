# Find a Device and Switch Maps Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Find any device by name, IP or MAC, and give each switch a "Connected" map of what's plugged into it.

**Architecture:**
- A pure `l2` package parses SNMP MAC tables, LLDP and ARP, looks up makers, guesses kinds and places each MAC.
- An `L2Monitor`, run by the device poller every 5 minutes, stores observations in an `l2` schema (outside backups).
- An `EndpointService` builds device records with names (user names and cached DNS), filters them by site access, and serves search, per-switch lists and naming.
- The frontend adds a top-bar search and a "Connected" Map/List section on switch and router pages.

**Tech Stack:** Go (gosnmp, gorm, PostgreSQL), React + TypeScript.

**Spec:** `docs/superpowers/specs/2026-10-18-find-device-switch-maps-design.md`

## Global Constraints

- **The L2 poll:** every **5 minutes** per enabled device whose status is `up`, alongside the stats poll and never blocking it. Each table's walk is independent.
- **OIDs:**

  | Table | OID |
  |---|---|
  | `dot1qTpFdbPort` | `1.3.6.1.2.1.17.7.1.2.2.1.2` (index `vlan.mac6`) |
  | `dot1qTpFdbStatus` | `1.3.6.1.2.1.17.7.1.2.2.1.3`; keep learned (3) only |
  | `dot1dTpFdbPort` | `1.3.6.1.2.1.17.4.3.1.2` (index `mac6`; fallback when Q-BRIDGE is empty) |
  | `dot1dTpFdbStatus` | `1.3.6.1.2.1.17.4.3.1.3` |
  | `dot1dBasePortIfIndex` | `1.3.6.1.2.1.17.1.4.1.2` |
  | `lldpRemTable` | `1.0.8802.1.1.2.1.4.1.1` (index `timeMark.localPortNum.remIndex`), columns `.5` chassis ID, `.7` port ID, `.8` port descr, `.9` sys name, `.12` capabilities enabled |
  | `lldpLocPortId` | `1.0.8802.1.1.2.1.3.7.1.3` (local port number to port) |
  | `ipNetToPhysicalPhysAddress` | `1.3.6.1.2.1.4.35.1.4` (IPv4 rows only: index `ifIndex.1.4.a.b.c.d`) |
  | `ipNetToMediaPhysAddress` | `1.3.6.1.2.1.4.22.1.2` (fallback, index `ifIndex.a.b.c.d`) |

- **LLDP capability bits** (BITS, MSB first): other 0, repeater 1, bridge 2, wlanAccessPoint 3, router 4, telephone 5.
- **Port classes:**
  - **AP:** LLDP WLAN-AP, or a monitored `access_point` device.
  - **Link:** LLDP bridge or router, or a port link to another device, or the role `uplink`, or **more than 8 MACs**.
  - **Exception:** an LLDP neighbour with neither bridge nor router is always an edge port.
  - **Edge:** anything else.
- **Placement order:**
  1. edge: wired, the most recent if more than one
  2. AP: wireless through that AP
  3. only link ports: "behind", at the port with the fewest MACs
  4. ARP only: "seen by router, port unknown", with the router's site
- **A "behind" group** is a link port classed only by the 8-MAC rule, with no monitored device or LLDP bridge/router on its far end. It shows on the map as "*N* devices behind port *n*".
- **Device records:**
  - IPv4 only
  - names: the user's name, else the cached reverse-DNS name (24 h), else none
  - retention 30 days
  - the maker from the embedded IEEE OUI list
  - kinds: `camera`, `phone`, `printer`, `ap`, `device`
- **Kind makers** (matched on the OUI organisation name, case-insensitive):
  - **camera:** Hikvision, Dahua, Axis, Hanwha, Uniview
  - **phone:** Yealink, Polycom, Poly, Grandstream, Mitel, Avaya; or LLDP telephone
  - **printer:** Hewlett Packard, HP Inc, Brother, Lexmark, Canon, Seiko Epson, Xerox, Ricoh, Kyocera
- **Storage:**
  - schema `l2`: `sightings`, `neighbors`, `arp`, `dns_names`, no foreign keys (like `metrics`)
  - `public.endpoint_names`, which is backed up
  - the next free migration number (`063` now), `TIMESTAMPTZ` only
- **Access:**
  - Records are visible at site readonly or above.
  - A record's site is its placing switch's site, or the router's site for ARP-only records.
  - Names can be set by admins and editable sharers, and every change is audited.
- **API:**
  - `GET /network/endpoints/search?q=` returns up to 50 results.
  - `GET /devices/:id/endpoints`
  - `GET /network/endpoints/:mac` gives the record plus its last 20 sightings.
  - `PUT /network/endpoints/:mac/name`
- **Copy:**
  - search placeholder "Find a device by name, IP or MAC"
  - section title "Connected", with "Map | List"
  - location lines: "Core-SW port 14", "via KitchenAP, Core-SW port 47", "behind Core-SW port 12", "seen by Quantum-Gate, port unknown"
  - map overflow "+ N more · list view" after 24 items
- **Cisco is out of scope (B2):** no per-VLAN or context reads, and no CDP.
- **Frontend gate:** `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`
- **DB tests:** `cd backend && ./scripts/test-db.sh -run <Name> -v`

## Review Focus

1. **A MAC seen on two switches at once,** an edge port on one and an uplink on the other, must place on the edge port, never the uplink. Test in Task 2: `TestPlaceEdgeBeatsLink`.
2. **A NAS or server with more than 8 MACs on its port** (Docker or VMs) must stay an edge device when LLDP names it, not vanish into a "behind" group. Test in Task 2: `TestPlaceLLDPHostIsEdge`.
3. **A stranger searching an IP or MAC on a site they can't see** must get nothing, including through the forward-DNS fallback. Test in Task 4: `TestDBEndpointSearchSiteAccess`.
4. **A switch whose walk times out on one table** still stores the others, and the stats poll isn't delayed. Test in Task 3: `TestL2PollTableFailureKeepsOthers`.
5. **A MAC written in another notation** (`AA-BB-CC-DD-EE-FF`, `aabb.ccdd.eeff`, or partly typed `aa:bb:cc`) must match in search. Test in Task 1: `TestNormalizeMAC`, and in Task 4: `TestDBEndpointSearchMACForms`.

---

### Task 1: The `l2` package: parsers, makers, kinds

**Files:**
- Create: `backend/internal/l2/parse.go`, `backend/internal/l2/oui.go`, `backend/internal/l2/oui.csv.gz` (downloaded once from `https://standards-oui.ieee.org/oui/oui.csv`, gzipped and embedded), `backend/internal/l2/kind.go`
- Test: `backend/internal/l2/parse_test.go`, `backend/internal/l2/oui_test.go`

**Interfaces:**
- Produces:
  - `type FDBEntry struct { VLAN int; MAC string; BridgePort int }`
  - `func ParseQBridgeFDB(port, status []snmp.PDU) []FDBEntry`
  - `func ParseDot1dFDB(port, status []snmp.PDU) []FDBEntry`
  - `func ParseBasePorts(pdus []snmp.PDU) map[int]int` (bridge port to ifIndex)
  - `type Neighbor struct { LocalPort int; ChassisID, PortID, PortDescr, SysName string; Caps Caps }`, with `type Caps uint8` and methods `Bridge()`, `Router()`, `AP()`, `Phone()`
  - `func ParseLLDP(rows map[string][]snmp.PDU) []Neighbor`, keyed by column `"5"`, `"7"`, `"8"`, `"9"`, `"12"`
  - `type ARPEntry struct { IP string; MAC string }`
  - `func ParseARP(physical, media []snmp.PDU) []ARPEntry` (IPv4 only; physical preferred, media when physical is empty)
  - `func NormalizeMAC(s string) (string, bool)`, returning lower-case colon form. It accepts colon, dash, dot (Cisco) and bare hex.
  - `func MACPrefixForSearch(s string) string` (partial input to a lower-case colon prefix, or "")
  - `func Maker(mac string) string`
  - `func GuessKind(maker string, caps Caps) string`

- [ ] **Step 1: Write the failing tests**
  - `TestParseQBridgeFDB`, using the row from the owner's walk (`.1.228.56.131.32.245.207 = 51`, status 3):
    - gives `{VLAN 1, MAC "e4:38:83:20:f5:cf", BridgePort 51}`
    - a status-1 (other) row is dropped
  - `TestParseDot1dFDB`: index `.0.17.34.51.68.85` gives MAC `00:11:22:33:44:55` with VLAN 0.
  - `TestParseBasePorts`: `.17.1.4.1.2.5 = 5` gives `{5: 5}`.
  - `TestParseLLDP`:
    - local port 47 with sys name `MBU7IW` and caps byte `0x10` (wlanAccessPoint) gives `AP()` true
    - `0x20` gives `Bridge()` true; `0x08` gives `Router()` true
  - `TestParseARP`:
    - a physical row `.13.1.4.10.1.10.60` with 6 bytes gives IP `10.1.10.60` and that MAC
    - an IPv6 row is dropped
    - with physical empty, the media rows are used
  - `TestNormalizeMAC`:
    - `AA-BB-CC-DD-EE-FF`, `aabb.ccdd.eeff` and `aabbccddeeff` all give `aa:bb:cc:dd:ee:ff`
    - `zz` gives false
    - `MACPrefixForSearch("aa:bb:c")` gives `"aa:bb:c"`, and `("AABB")` gives `"aa:bb"`
  - `TestMakerAndKind`:
    - `Maker` of a Hikvision OUI (`44:19:b6`) contains "Hikvision", and `GuessKind` gives `camera`
    - a Yealink OUI (`80:5e:c0`) gives `phone`
    - LLDP telephone caps give `phone`
    - unknown gives `device`

- [ ] **Step 2: Run them and see them fail**

  Run: `cd backend && go test ./internal/l2/`. Expected: build failure.

- [ ] **Step 3: Implement.**
  - Download the OUI CSV once into `oui.csv.gz`. It's a committed asset; there's no runtime fetch.
  - `oui.go` decompresses it on first use, behind `sync.Once`, into a map from 3-byte prefix to organisation name.

- [ ] **Step 4: Run them and see them pass.**

- [ ] **Step 5: Commit** `feat(network): parse MAC tables, LLDP and ARP; makers and kinds`

### Task 2: Placement

**Files:**
- Create: `backend/internal/l2/place.go`
- Test: `backend/internal/l2/place_test.go`

**Interfaces:**
- Consumes: Task 1 types.
- Produces:
  - `type PortKey struct { Device uuid.UUID; IfIndex int }`
  - `type PortInfo struct { Neighbor *Neighbor; LinkedTo bool; Uplink bool; MonitoredAP bool; MonitoredNeighbor bool }`
    - `LinkedTo`: the port has a port link to another device
    - `MonitoredNeighbor`: its LLDP chassis or sys name matches a monitored device
  - `type Sighting struct { Port PortKey; VLAN int; MAC string; LastSeen time.Time }`
  - `type PortClass int` with consts `PortEdge`, `PortAP`, `PortLink`, `PortBehind`
  - `func ClassifyPort(info PortInfo, macsOnPort int) PortClass`. `PortBehind` is a link classed only by the 8-MAC rule, with no monitored or LLDP bridge/router neighbour.
  - `type Placement struct { MAC string; Port *PortKey; VLAN int; Via string; How string; LastSeen time.Time }`
    - `How` is one of `"wired"`, `"wireless"`, `"behind"`, `"router"`
    - `Via` is the AP's name, from LLDP sys name or the monitored device's name
  - `func Place(sightings []Sighting, ports map[PortKey]PortInfo, apNames map[PortKey]string) map[string]Placement`
  - consts `MaxEdgeMACs = 8` and `RetentionDays = 30`

- [ ] **Step 1: Write the failing tests**
  - `TestClassifyPort`, a table:
    - an LLDP AP gives `PortAP`
    - a monitored AP gives `PortAP`
    - LLDP bridge gives `PortLink`
    - `LinkedTo` or `Uplink` gives `PortLink`
    - 9 MACs and no neighbour gives `PortBehind`
    - 9 MACs with an LLDP neighbour lacking bridge and router gives `PortEdge` (Review Focus 2)
    - 3 MACs and no neighbour gives `PortEdge`
  - `TestPlaceEdgeBeatsLink` (Review Focus 1): a MAC on switch A port 5 (edge) and switch B port 49 (link) gives wired, at A:5.
  - `TestPlaceWireless`: a MAC only on an AP port gives `How "wireless"` and `Via "KitchenAP"`.
  - `TestPlaceBehindFewest`: a MAC only on link ports with 40 and 12 MACs gives `behind`, at the 12-MAC port.
  - `TestPlaceMostRecentEdge`: two edge sightings give the later `LastSeen`.
  - `TestPlaceLLDPHostIsEdge` (Review Focus 2): 15 MACs on a port whose LLDP neighbour is `TS5410R81E` (no bridge or router caps) gives every MAC wired there.

- [ ] **Step 2: Run them and see them fail.**

- [ ] **Step 3: Implement** as specified. Router-only records (`How "router"`) are added later by the service from ARP; `Place` covers sightings only.

- [ ] **Step 4: Run them and see them pass.**

- [ ] **Step 5: Commit** `feat(network): place each MAC on its edge port, AP or link`

### Task 3: Storage and the L2 poll

**Files:**
- Create: `backend/migrations/063_l2.sql`, with the schema `l2` and four tables:
  - `l2.sightings (device_id UUID, if_index INT, vlan INT, mac TEXT, first_seen TIMESTAMPTZ, last_seen TIMESTAMPTZ, PRIMARY KEY (device_id, if_index, vlan, mac))`, plus an index on `mac`
  - `l2.neighbors (device_id, if_index, chassis_id, sys_name, port_id, port_descr, caps SMALLINT, last_seen)`, with primary key `(device_id, if_index, chassis_id)`
  - `l2.arp (device_id, ip TEXT, mac TEXT, last_seen)`, with primary key `(device_id, ip)` and an index on `mac`
  - `l2.dns_names (ip TEXT PRIMARY KEY, name TEXT, resolved_at TIMESTAMPTZ)`

  The same migration adds `public.endpoint_names (mac TEXT PRIMARY KEY, name TEXT NOT NULL, updated_by UUID, updated_at TIMESTAMPTZ NOT NULL DEFAULT now())`.
- Create: `backend/internal/services/l2_monitor.go`
- Modify: `backend/internal/services/device_poller.go` (an `L2Poller` hook), `backend/internal/services/device_service.go` (`Delete` removes the device's `l2` rows in the same transaction), `backend/internal/services/network_maintenance.go` (30-day retention), `backend/cmd/sentinel/main.go`
- Modify: `deploy/snmpsim/gen_data.py` and the regenerated `deploy/snmpsim/data/edgeswitch.snmprec`, adding a Q-BRIDGE table (3 MACs on 0/3, 12 on 0/48), an LLDP neighbour on 0/48 with bridge caps, and 2 ARP rows
- Test: `backend/internal/services/l2_monitor_test.go`, `backend/internal/services/l2_db_test.go`, `backend/internal/services/l2_sim_db_test.go`

**Interfaces:**
- Consumes: Task 1 parsers.
- Produces:
  - `type L2Poller interface { PollL2(ctx context.Context, d models.Device, t snmp.Target) }` and `func (p *DevicePoller) SetL2(l L2Poller)`. The poller calls it after an OK poll that leaves the device `up`. The monitor itself skips any device polled less than 5 minutes ago, keeping the time in memory.
  - `func NewL2Monitor(db *gorm.DB, client snmp.Client) *L2Monitor`, with `PollL2` writing:
    - sightings and neighbours, mapping bridge ports through `dot1dBasePortIfIndex`
    - ARP
  - **LLDP local port numbers** are mapped to an ifIndex in this order:
    1. through `dot1dBasePortIfIndex`, when the number is a bridge port there
    2. else as an ifIndex the device has
    3. else the neighbour is stored with `if_index` 0 and is unused for placement

    QuantumLink's walk gave 34, 47, 78 and 1100026; the test pins all three paths.
  - Each table is upserted with `last_seen = now`, so rows not seen this poll keep their old `last_seen`.
  - `func (m *L2Monitor) Purge(ctx, before time.Time) (int64, error)` deletes `l2` rows older than that time. `NetworkMaintenance` calls it with `now - 30d`.

- [ ] **Step 1: Write the failing tests**
  - `TestL2PollWritesTables` (fake client, DB):
    - a poll with 2 FDB rows, 1 LLDP neighbour and 2 ARP rows writes 2 sightings, 1 neighbour and 2 ARP rows
    - a second poll within 5 minutes walks nothing
    - after the clock passes 5 minutes, a poll updates `last_seen`
  - `TestL2PollTableFailureKeepsOthers` (Review Focus 4): the LLDP walk errors, and the FDB and ARP rows are still written.
  - `TestL2LLDPPortMapping`:
    - local port 78, found in the base-port map as 78 → 52, gives `if_index` 52
    - local port 34, not in the map but an ifIndex the device has, gives 34
    - local port 1100026 gives 0
  - `TestDBL2DeviceDeleteRemovesRows` and `TestDBL2Purge`: a row 31 days old goes, one 29 days old stays.
  - `TestDBBackupExcludesL2`: the backup's `dumpArgs` table list covers `public.endpoint_names` and not the `l2` schema. Assert on the args and on a real dump in the test container, as `TestDBRestoreWithMetrics` does.
  - `TestDBL2Simulator`, run with `SENTINEL_TEST_SNMPSIM`: polling the simulated EdgeSwitch writes 15 sightings and 1 neighbour.

- [ ] **Step 2: Run them and see them fail.**

- [ ] **Step 3: Implement and wire `main.go`** (`devicePoller.SetL2(l2Monitor)`, and pass the monitor to `NetworkMaintenance`).

- [ ] **Step 4: Run** `go test ./...` and `./scripts/test-db.sh -run 'L2|Backup'`. Expected: they pass.

- [ ] **Step 5: Commit** `feat(network): the L2 poll stores MAC tables, LLDP and ARP`

### Task 4: `EndpointService`: records, search, names, DNS

**Files:**
- Create: `backend/internal/services/endpoint_service.go`, `backend/internal/services/endpoint_dns.go`
- Test: `backend/internal/services/endpoint_db_test.go`

**Interfaces:**
- Consumes: Tasks 2 and 3. `SiteService.SiteAccess` for filtering.
- Produces:
  - `type Endpoint struct`, with fields:
    - `MAC`, `Maker`, `Kind string`
    - `IPs []string`
    - `Name string`, `NameSource string` (`"user"`, `"dns"` or `""`)
    - `How string`, `Via string`
    - `DeviceID *uuid.UUID`, `DeviceName string`, `IfIndex *int`, `PortLabel string`, `VLAN int`
    - `SiteID uuid.UUID`
    - `LastSeen time.Time`
    - `Behind *BehindGroup`, where `BehindGroup{DeviceID uuid.UUID; IfIndex int; Count int}`

    The JSON is snake_case.
  - `type Resolver interface { LookupAddr(ctx, ip string) ([]string, error); LookupHost(ctx, host string) ([]string, error) }`. Defaults to `net.DefaultResolver`, with 2 s timeouts.
  - `func NewEndpointService(db *gorm.DB, sites *SiteService, resolver Resolver) *EndpointService`
  - `func (s) Search(ctx, viewer Viewer, q string) ([]Endpoint, error)`, where `Viewer{UserID uuid.UUID; IsAdmin bool}`
    - matches the name, DNS name, IP, MAC (via `NormalizeMAC` or `MACPrefixForSearch`) and maker, case-insensitive and partial
    - limit 50
    - if nothing matches and `q` looks like a hostname, it calls `LookupHost` and searches by the IPs it gets back
  - `func (s) ForDevice(ctx, viewer, deviceID uuid.UUID) (*ConnectedView, error)`, returning `ConnectedView{Endpoints []Endpoint; APs []APGroup; Behind []BehindGroup}`:
    - the switch's edge-port devices
    - its AP ports, each with its wireless clients
    - its "behind" groups
    - for a router, its ARP-only records
  - `func (s) Get(ctx, viewer, mac string) (*Endpoint, []SightingView, error)`, with the last 20 sightings
  - `func (s) SetName(ctx, viewer, mac, name string) (before, after string, err error)`. It needs editable access to the record's site; an empty name clears it. Errors are `ErrEndpointNotFound` and `ErrEndpointForbidden`.
  - Reverse DNS: for any IP without a cached name newer than 24 h, `Search` and `ForDevice` start a background lookup, bounded to 8 at a time. Its result is stored in `l2.dns_names` and appears on the next read; reads never block on DNS.

- [ ] **Step 1: Write the failing tests** (DB, with a fake resolver)
  - `TestDBEndpointRecord`:
    - 1 edge sighting, 1 AP sighting and 1 ARP row for the MAC give an IP, a maker and wired
    - the wireless client shows `Via` with the AP's name
    - an ARP-only MAC gives `How "router"`, located at the router's site
  - `TestDBEndpointSearchSiteAccess` (Review Focus 3):
    - a user with no share on the site gets 0 results by IP, by MAC, and by a name whose fake `LookupHost` returns that IP
    - a readonly sharer gets 1
  - `TestDBEndpointSearchMACForms` (Review Focus 5): `AA-BB-CC-DD-EE-FF`, `aabb.ccdd.eeff` and `aa:bb:cc` all find it.
  - `TestDBEndpointNames`:
    - a user's name beats DNS
    - a readonly sharer setting a name gets `ErrEndpointForbidden`
    - after a set, `Search` finds the new name
  - `TestDBEndpointDNSCache`:
    - the first read returns no name and calls the fake resolver once
    - after the background lookup finishes, a read returns the name with `NameSource "dns"`
    - a second read within 24 h doesn't call the resolver
  - `TestDBConnectedView`:
    - a switch with an edge port (3 MACs), an AP port (2 clients) and a 12-MAC port with no neighbour gives 3 endpoints, 1 AP group with 2 clients, and 1 behind group with count 12
    - an LLDP-bridge link port gives nothing

- [ ] **Step 2: Run them and see them fail.**

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Run them and see them pass.**

- [ ] **Step 5: Commit** `feat(network): device records, search, names and DNS names`

### Task 5: Routes and wiring

**Files:**
- Create: `backend/internal/api/endpoint_handler.go`
- Modify: `backend/internal/models/audit.go` (`ActionEndpointNamed = "endpoint_named"`, `ResourceEndpoint = "endpoint"`), `backend/cmd/sentinel/main.go`
- Test: `backend/internal/api/endpoint_handler_test.go` (fakes), `backend/internal/api/endpoint_db_test.go`

**Interfaces:**
- `func RegisterEndpointRoutes(rg *gin.RouterGroup, endpoints endpointStore, devices deviceStore, sites siteAccessChecker, audit auditRecorder)`, with the four routes from Global Constraints.
- `GET /devices/:id/endpoints` uses `loadDevice(..., SiteAccessReadonly)`. The others filter inside the service.
- The PUT body is `{name}`. Audit records before and after.

- [ ] **Step 1: Write the failing tests**
  - `TestEndpointRoutes` (fakes):
    - search with an empty `q` gives 400
    - a MAC that doesn't normalise gives 400
    - a name over 64 characters gives 400
  - `TestDBEndpointRoutes`:
    - a stranger gets 0 results, and 404 for `/network/endpoints/:mac`
    - a readonly sharer's PUT gets 403
    - an editable sharer's PUT gets 200, with an audit entry
    - `/devices/:id/endpoints` for an unknown device gives 404

- [ ] **Step 2: Run them and see them fail.**

- [ ] **Step 3: Implement and wire.**

- [ ] **Step 4: Run** `go vet ./... && go test ./...`, then the full `./scripts/test-db.sh`.

- [ ] **Step 5: Commit** `feat(network): endpoint search, connected view and naming routes`

### Task 6: Frontend

**Files:**
- Create: `frontend/src/hooks/useEndpoints.ts`, `frontend/src/components/network/FindDevice.tsx`, `frontend/src/components/network/ConnectedSection.tsx`, `frontend/src/components/network/ConnectedMap.tsx`, `frontend/src/components/network/EndpointPanel.tsx`
- Modify: `frontend/src/components/Layout.tsx` (top bar), `frontend/src/pages/network/DeviceDetail.tsx` (a Connected section for `switch` and `router`, and `?endpoint=<mac>` to highlight)

**Interfaces:**
- Consumes: the Task 5 JSON.
- Produces:
  - `useEndpointSearch(q)`, debounced 250 ms
  - `useConnected(deviceId)`, refreshed every 60 s
  - `useEndpoint(mac)`
  - `setEndpointName(mac, name)`

- [ ] **Step 1: The top-bar `FindDevice`**
  - beside the greeting, placeholder as in Global Constraints
  - a dropdown of up to 8 results, each with the name or MAC, IP, maker, the location line and "last seen" (relative)
  - Enter or click navigates to `/network/devices/<switch>?endpoint=<mac>`. For a "behind" result, `?endpoint=<mac>` highlights the group.
  - keyboard navigable: ↑ ↓, Enter, Esc

- [ ] **Step 2: `ConnectedSection` with Map | List.**
  - **The map** (SVG, the design system's colours):
    - the switch in the centre, and around it the end devices, AP nodes with client counts (they expand into their clients) and "behind" groups
    - each labelled with its port, name or IP, and maker, with a kind icon (lucide: `Camera`, `Phone`, `Printer`, `Wifi`, `Monitor`, `Network` for behind)
    - not seen in the last poll: drawn at 50% opacity with "last seen …"
    - more than 24 items fold into "+ N more · list view"
  - **The list:** a sortable table (port, name, IP, MAC, maker, VLAN, how, last seen) with a filter box.
  - **Clicking a device** opens `EndpointPanel`: the full record, its last 20 sightings, a name field (shown only for editable access), and a link to its port.

- [ ] **Step 3: Run the gate.**

- [ ] **Step 4: Headless checks** in the harness, with a mocked API:
  - search, then a highlighted device on the map
  - an AP expanding to its 2 clients
  - a faded "last seen" device
  - a "behind" group of 12

- [ ] **Step 5: Commit** `feat(network): find a device in the top bar, and Connected maps`

### Task 7: Review, status, push, sandbox

- [ ] **Step 1:** One fresh reviewer (the most capable model) reviews the branch against the spec and this Review Focus. Fix findings test-first.
- [ ] **Step 2:** In `STATUS.md`, add piece B, its owner checks from the spec, and B2 (Cisco) as pending. Commit.
- [ ] **Step 3:** Fast-forward `dev` and push (the owner pushes to `dev` routinely), then update the sandbox:

  ```bash
  cd /srv/docker/sentinel-dev
  git pull --ff-only
  docker compose up -d --build
  ```

  Expected: migration 063 is applied and the backend is healthy.
- [ ] **Step 4:** After about 6 minutes, check that `l2.sightings` has QuantumLink's rows and `l2.arp` has Quantum-Gate's, with a read-only `psql` count.
