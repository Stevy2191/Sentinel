# Site map bundles, calmer zoom, ports marked automatically — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Draw LAGs and parallel cables as one bundle, fix the long-way route, idle colour and pan/zoom, and mark network-link and WAN ports automatically from LLDP, CDP, LAG membership and router routes/addresses.

**Architecture:**
- **Backend, collection:** the five-minute L2 poll also reads LAG membership, CDP, default routes and addresses, into new columns on `device_interfaces` and `l2.neighbors`.
- **Backend, roles:** a pure rules package decides automatic roles, and a small service writes them after each poll.
- **Backend, map:** the snapshot groups parallel links into bundles with members, and adds routers' WAN ports.
- **Frontend:** the browser sums member traffic, draws bundles and the Internet tag, frames role ports, and handles wheel and pinch without re-fitting.

**Tech Stack:** Go (gin, gorm, gosnmp, PostgreSQL), React 18 + TypeScript + Tailwind.

**Spec:** `docs/superpowers/specs/2026-10-19-site-map-bundles-design.md`

## Global Constraints

- Migration is `backend/migrations/065_site_map_bundles.sql`: run `ls backend/migrations | tail -1` first. TIMESTAMPTZ only. Never edit 064 or earlier.
- `role` stays the column every consumer reads. Automatic code never writes a port whose `role_source = 'user'`, and never writes `neighbor_device_id` / `neighbor_if_index`.
- Freshness window for evidence is 7 days, the same as `sitemap.dropAfter`.
- `IDLE_BPS = 1e6`. Zoom limits 0.15–2.5. Per-event factor clamp 0.8–1.25. Pinch factor `exp(-deltaY × 0.01)`, notch `exp(-deltaY × 0.001)`. Line delta = 16 px, page delta = viewport height.
- **Copy, word for word:**
  - Badge: "LAG · 2 × 10G", "2 cables · 2 × 1G", "LAG · 2 × 10G · 1 of 2 down".
  - Panel titles: "LAG" / "Cables".
  - Panel lines: "2 cables, not a LAG (each runs on its own)" and "1 of 2 cables down: running on 10 Gb of 20 Gb".
  - Legend: "idle (under 1 Mb/s)", "traffic, under half the busy threshold", "LAG or parallel cables, as one".
  - Reasons: "Found by LLDP: <device> port <label>", "Found by CDP: <device> <port>", "Member of <lag> to <device>", "Drawn on the site map", "The default route goes out this port", "Has a public address", "Named <ifName>". Popover: "Set by you".
- Frontend gate (must pass at the end of every frontend task): `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`
- **Backend tests:**
  - Unit: `cd backend && go vet ./... && go test ./...`
  - DB: `cd backend && ./scripts/test-db.sh` (with `-run` to narrow it)
- **TS checks:** `$W/check.sh`, where `W=.superpowers/sdd/2026-10-19-site-map-bundles`. It's a copy of the site-maps one: an esbuild bundle of `$S/smcheck/check.ts` run in node.
- **Harness:** `$S/lag-mock`, the mockup copy. Rsync the real `frontend/src` over it (keeping `harness-sitemap.tsx`), `vite build`, `vite preview` on 5181, and Playwright script `$S/pw/bundles.js`.
- Commit with the repo's `feat(network): …` / `fix(network): …` style, ending with the Co-Authored-By line.

## Review Focus

1. **A complete LLDP walk deletes every neighbour row not seen:** with CDP rows in the same table, the LLDP delete must be scoped to `protocol = 'lldp'` (and CDP's to `'cdp'`), or each walk wipes the other's rows. Test: Task 3 `TestL2PollKeepsCDPWhenLLDPRewrites`.
2. **A person's choice is overwritten by the rules:** a port the user set to Endpoint that LLDP sees a switch on must stay Endpoint. Test: Task 4 `TestDecideUserWins` and Task 5 `TestDBPortRolesSkipUserPorts`.
3. **A far port pointed back by a "Network link to…" patch must become `role_source = 'user'`**, and one cleared by losing that link must go back to `default`. Otherwise the rules either fight the person or never take the port back. Test: Task 6 `TestDBPortPatchFarPortSources`.
4. **Hiding a bundle hides only one member:** the next snapshot shows a single cable. Test: Task 7 `TestDBSiteMapHideBundleHidesMembers`.
5. **The public map or widget leaks real ids through bundle members or WAN keys.** Test: Task 7 `TestPublicSnapshotAliasesBundleMembers`, Task 8 `TestPublicTrafficAliasesWANKeys`, and the leak guard with a bundle and a WAN router in Task 8.

---

### Task 1: Migration 065 and model fields

**Files:**
- Create: `backend/migrations/065_site_map_bundles.sql`
- Modify: `backend/internal/models/snmp.go` (DeviceInterface)
- Test: `backend/internal/services/port_roles_db_test.go` (new)

**Interfaces:**
- Produces:
  - Columns `device_interfaces.role_source` (`default|user|auto`), `role_reason`, `lag_if_index`, `wan_evidence` (`''|default_route|public_address`), and `l2.neighbors.protocol` (`lldp|cdp`), `address`.
  - Primary key `l2.neighbors (device_id, if_index, protocol, chassis_id)`.
  - Go: `models.PortRoleSourceDefault/User/Auto`, `models.WANEvidenceDefaultRoute/PublicAddress`, and DeviceInterface fields `RoleSource string json:"role_source"`, `RoleReason string json:"role_reason"`, `LagIfIndex *int json:"lag_if_index"`, `WANEvidence string json:"-"`, with gorm `default:` tags so inserts take the defaults.

- [ ] **Step 1:** Write `TestDBPortRoleColumns`:
  - A new port's role_source is `default`, role_reason `''`, wan_evidence `''`, lag_if_index NULL.
  - `UPDATE … SET role_source = 'bogus'` fails, and so does wan_evidence `'x'`.
  - An `l2.neighbors` insert of the same (device, if_index, chassis_id) with protocol `lldp` and `cdp` both succeed.

  Write `TestDBSaveInventoryKeepsRoleSource`: set role_source `auto`, role_reason `x`, lag_if_index 66 and wan_evidence `public_address`, then run `SaveInventory` for that device. All four are unchanged.

  The test DB is migrated before tests run, so the back-fill (`user` where `role <> 'access'`) is checked on the sandbox after deploy instead: Quantum-Gate eth8 and eth10 and QuantumLink 51 must read `user` (Task 12).
- [ ] **Step 2:** Run `./scripts/test-db.sh -run 'PortRoleColumns|SaveInventoryKeepsRoleSource'`. Expected: FAIL (column does not exist).
- [ ] **Step 3:** Write the migration:
  - The four columns with their CHECKs, NOT NULL and defaults as the spec says.
  - Back-fill: `UPDATE device_interfaces SET role_source = 'user' WHERE role <> 'access'`.
  - `l2.neighbors`: `protocol` (CHECK `IN ('lldp','cdp')`, `IS NOT NULL` is implied by NOT NULL), and `address`.
  - Replace the primary key: `ALTER TABLE l2.neighbors DROP CONSTRAINT neighbors_pkey, ADD PRIMARY KEY (device_id, if_index, protocol, chassis_id)`. Check the constraint's name with `\d l2.neighbors` on the sandbox DB first.

  Add the model fields and constants. SaveInventory's DoUpdates list already omits them; confirm no `Select("*")` path writes them.
- [ ] **Step 4:** Rerun the step-2 command. Expected: PASS. Also run `go test ./...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(network): port role sources, LAG and WAN evidence columns (065)`.

### Task 2: Parsers for LAG, CDP, routes and addresses

**Files:**
- Create: `backend/internal/l2/lag.go`, `backend/internal/l2/cdp.go`, `backend/internal/l2/routes.go`
- Test: `backend/internal/l2/lag_test.go`, `cdp_test.go`, `routes_test.go`

**Interfaces:**
- Produces (all in package `l2`):
  - `OIDDot3adAttachedAgg = "1.2.840.10006.300.43.1.2.1.1.13"` and `OIDIfStackStatus = "1.3.6.1.2.1.31.1.2.1.3"`
  - `OIDCDPCache = "1.3.6.1.4.1.9.9.23.1.2.1.1"`
  - `OIDCidrRouteIfIndexDefault = "1.3.6.1.2.1.4.24.4.1.5.0.0.0.0"` and `OIDRouteIfIndex = "1.3.6.1.2.1.4.21.1.2"`
  - `OIDIPAdEntIfIndex = "1.3.6.1.2.1.4.20.1.2"`
  - `func ParseLAG(dot3ad, stack []snmp.PDU, isLAG func(ifIndex int) bool) map[int]int`: member ifIndex → aggregator ifIndex. dot3ad rows valued 0 are dropped. When dot3ad gives no non-zero row, use ifStack: index `higher.lower`, status 1 (active) only, where `isLAG(higher)` and `lower` is not a LAG.
  - `type CDPNeighbor struct { IfIndex int; DeviceID, SysName, Port, Platform, Address string }`
  - `func ParseCDP(cols map[string][]snmp.PDU) []CDPNeighbor`: the columns are keyed "3", "4", "6", "7", "8", the index is ifIndex.deviceIndex. SysName is DeviceID with everything from the first "." or "(" cut. Address is dotted IPv4 when type (col 3) is 1 and the value is 4 bytes, else "".
  - `func ParseDefaultRoutes(cidr, route []snmp.PDU) []int`: ifIndexes, de-duplicated and sorted. cidr is the subtree walk (every row is a default route). route is the whole ipRouteIfIndex table; only rows whose index is exactly `0.0.0.0` count. route is used only when cidr is empty. ifIndex 0 is dropped.
  - `func ParseAddresses(pdus []snmp.PDU) map[int][]string`: ifIndex → dotted IPv4s, sorted.
  - `func IsPublicIPv4(ip string) bool`: false for 10/8, 172.16/12, 192.168/16, 127/8, 169.254/16, 0.0.0.0/8, 224/4 and above, and anything unparsable. True for 100.64/10 and the rest.

- [ ] **Step 1:** Write the tests, using the home walk shapes recorded in the spec:
  - `TestParseLAGDot3ad`: rows `.1 = 0`, `.49 = 66`, `.50 = 66`. Want `{49:66, 50:66}`.
  - `TestParseLAGFallsBackToIfStack`: dot3ad all 0; stack `.66.49 = 1`, `.66.50 = 1`, `.66.51 = 2`, `.0.66 = 1`. isLAG(66) is true. Want `{49:66, 50:66}`.
  - `TestParseCDP`:
    - Index `10101.3`. DeviceId "LAB-C3850.corp.example(FOC1234)" → SysName "LAB-C3850". Port "GigabitEthernet1/0/1". Type 1 with bytes `{10,20,0,5}` → "10.20.0.5". IfIndex 10101.
    - A second row with type 20 has Address "".
  - `TestParseDefaultRoutesCidr`: `...0.0.0.0.0.0.0.0.0.10.0.0.1 = 13` → `[13]`.
  - `TestParseDefaultRoutesOldTable`: cidr empty, route rows from the UDM-SE (`.10.1.1.0 = 58`, `.108.204.168.0 = 13`) → `[]`. Adding `.0.0.0.0 = 13` → `[13]`.
  - `TestParseAddresses`: the UDM-SE rows (`.108.204.170.224 = 13`, `.10.99.1.1 = 58`, `.127.0.0.1 = 1`) → `{13:["108.204.170.224"], 58:["10.99.1.1"], 1:["127.0.0.1"]}`.
  - `TestIsPublicIPv4` table: 108.204.170.224 true; 100.64.3.4 true; 10.99.1.1, 172.20.0.1, 192.168.8.1, 127.0.0.1, 169.254.1.1, 0.0.0.0, 224.0.0.1, "x" false.
- [ ] **Step 2:** Run `go test ./internal/l2/ -run 'LAG|CDP|Routes|Addresses|Public'`. Expected: FAIL (undefined).
- [ ] **Step 3:** Implement. Reuse `index(oid, root)` from parse.go, and `lldpText` for octet strings.
- [ ] **Step 4:** Rerun. Expected: PASS.
- [ ] **Step 5:** Commit `feat(network): parse LAG membership, CDP, default routes and addresses`.

### Task 3: The L2 poll collects them

**Files:**
- Modify: `backend/internal/services/l2_monitor.go`
- Test: `backend/internal/services/l2_monitor_test.go`

**Interfaces:**
- Consumes: Task 2's parsers; Task 1's columns.
- Produces:
  - `func (m *L2Monitor) SetAfterPoll(f func(ctx context.Context, deviceID uuid.UUID))`, called once at the end of each PollL2 that ran.
  - A device is a router when `COALESCE(device_type, device_type_detected) = 'router'`. Use the model's existing effective-type helper if it has one.

- [ ] **Step 1:** Write the tests (fakeL2SNMP, testdb):
  - `TestL2PollWritesLAGMembers`: ports 49, 50 and 66 (if_type 161), dot3ad `.49=66 .50=66`. Then lag_if_index is 66 on 49 and 50, and NULL on 66. A second poll whose dot3ad rows are all 0 sets them NULL. A third poll where the dot3ad walk errors leaves them as they were.
  - `TestL2PollWritesCDP`: a CDP row on ifIndex 3 becomes an `l2.neighbors` row with `protocol='cdp'`, `chassis_id` the device ID, sys_name cleaned, port_id the port, port_descr the platform, and `address` set.
  - `TestL2PollKeepsCDPWhenLLDPRewrites` (Review Focus 1): after a poll with one LLDP and one CDP row, a poll 6 minutes later with the same CDP row and no LLDP rows. Exactly the CDP row remains. The mirror case: CDP gone, LLDP kept.
  - `TestL2PollWANEvidenceRouterOnly`: a router device with ports 13 and 58, addresses `13→108.204.170.224`, `58→10.99.1.1`, and no default route. Then wan_evidence is `public_address` on 13 and `''` on 58. A cidr default route on 58 makes it `default_route` on 58, and 13 keeps `public_address`. The same walks on a switch write nothing.
  - `TestL2PollCallsAfterPoll`: the hook runs once with the device ID, and not on a skipped (within 5 min) poll.
- [ ] **Step 2:** Run `./scripts/test-db.sh -run 'L2Poll'`. Expected: the new tests FAIL, and the old ones still PASS.
- [ ] **Step 3:** Implement:
  - **`writeLAG`:** dot3ad walk, plus an ifStack walk only when dot3ad has no non-zero row. `isLAG` comes from the device's ports with if_type 161. On success, one UPDATE sets lag_if_index for the members and NULL for the rest of the device.
  - **`writeCDP`:** columns 3, 4, 6, 7, 8 under OIDCDPCache. Any column failing returns without writing. Upsert keys gain `protocol`. Then delete this device's `protocol='cdp'` rows not seen. An empty but successful walk (a non-Cisco device answers nothing) also deletes the old CDP rows.
  - **LLDP:** `writeNeighbors` sets `protocol='lldp'`, and its delete adds `AND protocol = 'lldp'`.
  - **`writeWAN`:** routers only. Walk cidr default, plus route when cidr is empty, plus addresses. Ports in default routes get `default_route`. Other ports with an `IsPublicIPv4` address get `public_address`. The rest get `''`. All of it only if the address walk succeeded (a route-walk failure counts as no routes).
  - Then call the hook.
- [ ] **Step 4:** Rerun. Expected: PASS. Also run `go test ./...`.
- [ ] **Step 5:** Commit `feat(network): the L2 poll reads LAG members, CDP and routers' WAN evidence`.

### Task 4: The rules (pure)

**Files:**
- Create: `backend/internal/portroles/rules.go`
- Test: `backend/internal/portroles/rules_test.go`

**Interfaces:**
- Produces (package `portroles`):

```go
type Port struct {
    IfIndex    int
    Name       string // ifName
    Label      string // faceplate label, for reasons
    Physical   bool
    Up         bool   // oper_status up
    Role       string
    RoleSource string
    RoleReason string
    LagIfIndex int    // 0 none
    WANEvidence string
}
type Peer struct{ IfIndex int; Protocol string; DeviceName string; PortText string; LastSeen time.Time } // a matched router/switch neighbour
type Input struct {
    IsRouter bool
    Ports    []Port
    Peers    []Peer            // already matched to monitored routers/switches
    LagNames map[int]string    // aggregator ifIndex → its ifName
    LagPeer  map[int]string    // aggregator ifIndex → device name reached through any member
    Manual   map[int]bool      // ifIndexes at a manual map link end
    Now      time.Time
}
type Change struct{ IfIndex int; Role, Source, Reason string }
func Decide(in Input) []Change
```

  `Decide` returns only the ports whose (role, source, reason) differ from now.

- [ ] **Step 1:** Write the tests, each a small Input:
  - `TestDecideLLDPPeer`: a port with a fresh LLDP peer "CORE-ES48-A" port "49" → `uplink/auto/"Found by LLDP: CORE-ES48-A port 49"`.
  - `TestDecideCDPPeer`: → `"Found by CDP: LAB-C3850 Gi1/0/1"`.
  - `TestDecideStalePeerIgnored`: a peer last seen 8 days ago gives no change on a default port. An auto uplink with only that peer goes back to `access/default/""`.
  - `TestDecideLAGMember`: port 50 has LagIfIndex 66, LagNames{66:"lag 1"}, LagPeer{66:"CORE-ES48-B"}, and no peer of its own → `"Member of lag 1 to CORE-ES48-B"`. A peer on the port itself wins over the LAG reason.
  - `TestDecideManual` → `"Drawn on the site map"`.
  - `TestDecideWANOrder` (router):
    - port 13 with public_address and port 58 with default_route → only 58 is WAN, "The default route goes out this port".
    - With no default_route anywhere, 13 is WAN, "Has a public address".
    - With neither, an up physical port named "wan1" → "Named wan1".
    - A down "wan2" is not.
    - A non-physical port with public_address is not.
  - `TestDecideWANNotOnSwitch`: the same Input with `IsRouter: false` gives no WAN.
  - `TestDecideWANBeatsUplink`: a router port with an LLDP peer and default_route → WAN.
  - `TestDecideUserWins` (Review Focus 2): RoleSource user, Role access, fresh peer → no change.
  - `TestDecideNoChurn`: an auto uplink with the same reason → no Change.
- [ ] **Step 2:** Run `go test ./internal/portroles/`. Expected: FAIL (undefined).
- [ ] **Step 3:** Implement `Decide`, with the order from the spec. Name rule: `^wan\d*$`, case-insensitive, on Name. Peer reason uses PortText as given: the caller passes the label for LLDP ("49") and the raw port for CDP.
- [ ] **Step 4:** Rerun. Expected: PASS.
- [ ] **Step 5:** Commit `feat(network): rules for marking network-link and WAN ports`.

### Task 5: Applying the rules

**Files:**
- Create: `backend/internal/services/port_roles.go`
- Modify: `backend/cmd/sentinel/main.go` (wire `l2Monitor.SetAfterPoll(portRoles.Apply)`)
- Test: `backend/internal/services/port_roles_db_test.go`

**Interfaces:**
- Consumes: `portroles.Decide`; `sitemap.MatchNeighborDevice` (extended in Task 7 for address; Task 5 calls it with what exists, and Task 7 adds the address field without changing the signature); `NumberingFor(...).Label` for labels.
- Produces:
  - `type PortRoleService struct`
  - `NewPortRoleService(db *gorm.DB) *PortRoleService`
  - `func (s *PortRoleService) Apply(ctx context.Context, deviceID uuid.UUID)`: logs errors, returns nothing (it's a hook).
  - `func (s *PortRoleService) ApplyErr(ctx context.Context, deviceID uuid.UUID) error`: for the port patch.

- [ ] **Step 1:** Write the tests:
  - `TestDBPortRolesFromLLDP`: two switches A and B (type switch) at a site. B's sys_name is "B". An l2.neighbors row on A port 49 → chassis anything, sys_name "B", port_id "0/49". B has port 49 named "0/49". Apply(A) → A port 49 is `uplink/auto/"Found by LLDP: B port 49"`.
  - `TestDBPortRolesSkipUserPorts` (Review Focus 2): the same with A port 49 already `access/user` → unchanged.
  - `TestDBPortRolesAPIsDontCount`: the neighbour matches a device of type access_point → no change.
  - `TestDBPortRolesWAN`: a router with wan_evidence public_address on 13 → `wan/auto/"Has a public address"`.
  - `TestDBPortRolesClear`: an auto uplink whose neighbour row is deleted → back to `access/default/""` on Apply.
- [ ] **Step 2:** Run `./scripts/test-db.sh -run PortRoles`. Expected: FAIL.
- [ ] **Step 3:** Implement Apply:
  - Load the device and its present ports.
  - Load its `l2.neighbors` rows, and match each against the routers and switches anywhere in the instance (not only its site; a link can cross sites), using MatchNeighborDevice.
  - Load the manual map link ends at this device's site (`site_map_links` kind manual).
  - Build LagNames, and LagPeer (any member's matched peer name).
  - Call Decide, and write each Change in one transaction with `WHERE role_source <> 'user'`, setting `updated_at`.
- [ ] **Step 4:** Rerun. Expected: PASS. Run `go vet ./... && go test ./...`.
- [ ] **Step 5:** Commit `feat(network): mark ports automatically after each L2 poll`.

### Task 6: Port patch — Automatic, and who set it

**Files:**
- Modify: `backend/internal/models/snmp.go` (`PortPatch.Validate`, comment), `backend/internal/services/port_service.go`
- Test: `backend/internal/services/port_service_db_test.go` (existing file; add tests)

**Interfaces:**
- Consumes: `PortRoleService.ApplyErr` (inject via `PortService.SetRoles(r *PortRoleService)`; nil means don't re-run).
- Produces: `role: null` in PATCH = back to automatic. The ports API JSON carries `role_source` and `role_reason` (model fields from Task 1).

- [ ] **Step 1:** Write the tests:
  - `TestDBPortPatchRoleSetsUser`: PATCH `{role:"access"}` → role_source user.
  - `TestDBPortPatchRoleNullIsAutomatic`: a port `uplink/user` with a fresh matched LLDP peer; PATCH `{role:null}` → role_source auto, role uplink, reason set.
  - `TestDBPortPatchFarPortSources` (Review Focus 3): A port 1 PATCH `{neighbor_device_id:B, neighbor_if_index:2}`. B port 2 is `uplink/user`, and A port 1 `uplink/user`. Then PATCH A port 1 `{neighbor_device_id:null}` → B port 2 `access/default`.
  - Validate: `{role:null}` is accepted; `{role:"bogus"}` still errors "role must be access, uplink or wan".
- [ ] **Step 2:** Run `./scripts/test-db.sh -run PortPatch`. Expected: the new ones FAIL.
- [ ] **Step 3:** Implement. Validate allows `Role.Set && Value == nil`. In UpdatePort:
  - **`role: null`:** inside the transaction, set `role='access', role_source='default', role_reason=''` and clear the neighbour columns (the same as access). After the commit, call `ApplyErr` for the device, and return the port re-read after it.
  - **A role given, or a neighbour implying uplink:** `role_source='user', role_reason=''`.
  - **Far port pointed back:** add `role_source: user, role_reason: ''`.
  - **Far port cleared:** `role_source: default, role_reason: ''`.

  `p.Updates()` must not turn a nil role into a NULL write. Handle Role before it.
- [ ] **Step 4:** Rerun. Expected: PASS. Run the full DB suite `./scripts/test-db.sh` (port service is heavily tested).
- [ ] **Step 5:** Commit `feat(network): a port can go back to automatic; hand-set roles are remembered as yours`.

### Task 7: The map — CDP links and bundles

**Files:**
- Modify: `backend/internal/sitemap/links.go`, `ports.go` (Neighbor `Protocol`, `Address`; Device `Host`; `SourceCDP = "cdp"`; address match)
- Create: `backend/internal/services/site_map_bundles.go`
- Modify: `backend/internal/services/site_map_service.go` (load lag_if_index, protocol, address, host; bundle in `build`; BoundPorts over members), `site_map_edits.go` (SetLinkHidden accepts bundle keys), `site_map_public.go` (alias members and bundle keys)
- Test: `backend/internal/sitemap/links_test.go`, `backend/internal/services/site_map_bundles_test.go` (pure), `site_map_db_test.go` (existing; add)

**Interfaces:**
- Produces:
  - `MapLink` gains `Members []MapLink json:"members,omitempty"` and `Lag *MapLag json:"lag,omitempty"`, with `type MapLag struct{ A, B string }` (json `a`, `b`).
  - `func bundleLinks(links []MapLink, lagOf func(e MapEnd) (name string, agg int)) []MapLink`
  - `func isBundleKey(k string) bool`, `func bundleMembers(k string) []string`
  - A neighbour from CDP gives `SourceCDP`. The frontend's sourceText adds "Found by CDP" (Task 10).

- [ ] **Step 1:** Write the tests:
  - `TestMatchNeighborByAddress` (sitemap): a neighbour with no MAC or name match and Address "10.20.0.5" matches the device with Host "10.20.0.5".
  - `TestFindCDPSource`: a CDP neighbour yields a link with Sources `[cdp]`.
  - `TestBundleTwoMembers` (pure):
    - Two links A:49–B:49 and B:50–A:50 (the second written B-first) → one bundle. Key `bundle:<k49>,<k50>`. Members oriented A-first with the second's rate_end flipped. Speed is the sum, rate_end b, Sources the union.
    - `TestBundleLagNames`: lagOf gives ("lag 1",66) for both A ends, and ("lag 1",70) / ("lag 2",71) for the B ends → Lag `{A:"lag 1", B:""}`.
    - A single link, a guess, and a port-less end stay as they are.
    - Order follows A's port order.
  - `TestDBSiteMapBundleSnapshot`: a site with two LLDP cables between two switches → one link with two members. BoundPorts returns both member ports with member keys.
  - `TestDBSiteMapHideBundleHidesMembers` (Review Focus 4): SetLinkHidden(bundle key, true) → two hidden override rows. The snapshot's Links has none, and HiddenLinks has one bundle. SetLinkHidden(bundle key, false) shows both.
  - `TestPublicSnapshotAliasesBundleMembers` (Review Focus 5): no real device ID appears anywhere in the JSON of a public snapshot with a bundle (marshal it and search for each real UUID).
- [ ] **Step 2:** Run `go test ./internal/sitemap/ ./internal/services/ -run 'Bundle|Address|CDP'` and `./scripts/test-db.sh -run 'SiteMap'`. Expected: the new ones FAIL.
- [ ] **Step 3:** Implement:
  - **Matching:** add `Address` to Neighbor and `Host` to Device. MatchNeighborDevice tries MAC, then name, then `Address != "" && Address == Host` (Host trimmed, case-insensitive).
  - **Load:** the service loads protocol and address, and Find sets the source by protocol.
  - **Building:** `build` runs bundleLinks on Links and on HiddenLinks separately. lagOf reads `w.ports[end].LagIfIndex` and the aggregator's Name.
  - **Hiding:** SetLinkHidden with a bundle key runs the single-key path for each member in one transaction.
  - **Public:** aliasSnapshot aliases each member like a link, and rebuilds the bundle key from the aliased member keys.
- [ ] **Step 4:** Rerun. Expected: PASS. Then `go vet ./... && go test ./...`.
- [ ] **Step 5:** Commit `feat(network): site map bundles LAGs and parallel cables; CDP neighbours draw links`.

### Task 8: The map — routers' WAN ports

**Files:**
- Modify: `backend/internal/services/site_map_service.go` (MapDevice.WAN, BoundPorts), `site_map_public.go` (PublicTraffic and aliasSnapshot for WAN), `backend/internal/dashboards/public_leak_db_test.go`
- Test: `site_map_db_test.go`, `site_map_public_test.go` (or the existing public test file)

**Interfaces:**
- Produces:
  - `MapDevice.WAN []MapWAN json:"wan,omitempty"`, with `type MapWAN struct{ IfIndex int json:"if_index"; Name string json:"name"; SpeedBps int64 json:"speed_bps"; Reason string json:"reason" }`. On a public map, IfIndex stays (stand-in device ids make it harmless), and Name and Reason are "".
  - `func wanKey(device uuid.UUID, ifIndex int) string` returns `"wan:<device>:<ifIndex>"`.

- [ ] **Step 1:** Write the tests:
  - `TestDBSiteMapRouterWAN`: a router with port 13 role wan (auto, with reason) → MapDevice.WAN `[{13,"eth8",speed,reason}]`, and BoundPorts includes `{port 13, LinkKey wan:<id>:13}`.
  - `TestPublicTrafficAliasesWANKeys`: a traffic map with key `wan:<real>:13` comes out as `wan:<publicID>:13`, and no real id is left.
  - Extend the leak guard's site fixture with a WAN port on its router and a second cable between its two switches. It must still find no real id.
- [ ] **Step 2:** Run `./scripts/test-db.sh -run 'SiteMapRouterWAN|PublicLeak'` and `go test ./internal/services/ -run PublicTraffic`. Expected: FAIL.
- [ ] **Step 3:** Implement: load the role per port in mapPortRow; mapDevice fills WAN for router-type devices from `role='wan'` ports; BoundPorts appends WAN ports of visible routers; PublicTraffic maps `wan:` keys; aliasSnapshot blanks Name and Reason.
- [ ] **Step 4:** Rerun. Expected: PASS. Then the full DB suite.
- [ ] **Step 5:** Commit `feat(network): routers' WAN ports on the site map, with live traffic`.

### Task 9: Frontend model — bundles, idle, wheel, routes

**Files:**
- Modify: `frontend/src/types/siteMap.ts`, `frontend/src/utils/siteMap.ts`
- Test: `$S/smcheck/check.ts` (extend), run with `$W/check.sh`

**Interfaces:**
- Produces:
  - **Types:**
    - `MapLink.members?: MapLink[]`
    - `MapLink.lag?: { a: string; b: string } | null`
    - `MapDevice.wan?: { if_index: number; name: string; speed_bps: number; reason: string }[]`
    - `LinkSource` adds `'cdp'`
  - `IDLE_BPS`
  - `linkColour(u, thresholdPct, busiestBps?)`
  - `directional(l, r, speedBps = l.speed_bps)`
  - `bundleState(l: MapLink, rates: Record<string, LinkRate> | undefined): { rate: LinkRate; down: number; liveSpeed: number }`, using orientation from each member's own rate_end.
  - `wanKey(deviceId, ifIndex)`
  - `wheelAction(e: { deltaX: number; deltaY: number; deltaMode: number; ctrlKey: boolean }, pageHeight: number): { kind: 'pan'; dx: number; dy: number } | { kind: 'zoom'; factor: number }`
  - `routeKind(upTier: Tier, loTier: Tier, child: boolean, loRow: number): 'curve' | 'lane' | 'chain'`
  - `badgeText(l: MapLink, down: number): string`

- [ ] **Step 1:** Extend check.ts:
  - `bundleState`: two members 1G each, rates `{in 120e6, out 31e6}` and `{in 108e6, out 27e6}` (rate_end b) → `{in 228e6, out 58e6, oper_up true}`, down 0, liveSpeed 2e9. Make one member `oper_up false` → down 1, liveSpeed 1e9. Make both down → oper_up false.
  - `linkColour(0.04, 80, 4e6)` is 'ok'. `linkColour(0.04, 80, 5e5)` is 'idle'. `linkColour(50, 80, 5e9)` is 'warn'.
  - `wheelAction`:
    - `{deltaX:0, deltaY:-100, deltaMode:0, ctrlKey:false}` → zoom with factor `exp(0.1)` rounded to 4 places.
    - `{0, 3.5, 0, false}` → pan `{dx:0, dy:3.5}`.
    - `{0, -2, 0, true}` → zoom `exp(0.02)`.
    - `{0, 3, 1, false}` (lines) → zoom `exp(-0.048)`.
    - `{0, -1000, 0, true}` → factor 1.25.
  - `routeKind('dist','access',true,0)` is 'chain', with row 1 'lane'; `('access','access',true,0)` is 'chain'; `('core','dist',true,0)` is 'curve'.
  - `badgeText`: a LAG with two 10G members and 0 down → "LAG · 2 × 10G". Not a LAG, 1G → "2 cables · 2 × 1G". 1 down → "LAG · 2 × 10G · 1 of 2 down".
- [ ] **Step 2:** Run `$W/check.sh`. Expected: FAIL (import errors).
- [ ] **Step 3:** Implement in `utils/siteMap.ts` (it stays type-only on `@/`).
- [ ] **Step 4:** Run `$W/check.sh` → ALL OK. Run the frontend gate → pass.
- [ ] **Step 5:** Commit `feat(network): site map model — bundle traffic, idle under 1 Mb/s, wheel and route choices`.

### Task 10: Frontend map — drawing bundles, panel, camera, Internet tag

**Files:**
- Modify: `frontend/src/components/network/sitemap/MapCanvas.tsx`, `MapLinks.tsx`, `LinkPanel.tsx`, `SiteMapView.tsx`, `MapNode.tsx`, `EditBar.tsx`
- Test: `$S/pw/bundles.js` against the harness (`$S/lag-mock` with the work-site fixture from the mockup, its `bundleLinks` frontend shim removed: the fixture now sends `members`/`lag` the way the API does)

**Interfaces:**
- Consumes: Task 9's functions.

- [ ] **Step 1:** Update the harness fixture to the API shape (bundles with members, `wan` on the FortiGate, a UDM-SE router variant at `?router=udm`). Write `$S/pw/bundles.js`, with these checks:
  - The CORE-ES48-A ↔ core-es48-c badge text is "LAG · 2 × 10G".
  - After "Simulate one cable down": the badge text ends "1 of 2 down", and the bundle path's stroke is `#f59e0b`.
  - Clicking the bundle opens the panel with heading "LAG", the "1 of 2 cables down: running on 10 Gb of 20 Gb" line, and two member rows.
  - The LAB-ES48 → LAB-USW48 path has no point left of the LAB-ES48 box (straight drop).
  - Camera: zoom with the mouse wheel; re-emit an identical `map` event; the canvas transform is unchanged. Open a panel; unchanged.
  - Two-finger scroll (`wheel` with deltaY 3.5 ×30) changes translate, not scale.
  - The FortiGate card has an "Internet" tag with the ↓/↑ text. With `?router=udm`, a violet path runs from the tag to `[data-if-index="13"]`.
  - The toolbar says "6 links".
  - The legend has "LAG or parallel cables, as one" and "idle (under 1 Mb/s)".
- [ ] **Step 2:** Rsync the current `frontend/src` into the harness, build, preview, and run `bundles.js`. Expected: FAIL on the bundle checks.
- [ ] **Step 3:** Implement as the spec's sections 1–4 and 7 say:
  - **MapCanvas:**
    - Bar/stub geometry for `members`, using `routeKind`.
    - `wheelAction` in the native wheel listener.
    - Fit keyed on the bounds' values.
    - `fitKey` from SiteMapView is the view only.
    - The WAN stub geometry from the tag to the WAN port (or the card's left edge).
  - **MapLinks:** rails and core, forks, down stubs, the badge, and amber when down > 0. It reads `bundleState` per bundle.
  - **LinkPanel:** the bundle body.
  - **SiteMapView:** the legend.
  - **MapNode:** the Internet tag, using `traffic.links[wanKey]`.
  - **EditBar:** the hidden bundle shows as one entry, named "A ↔ B (2 cables)".
  - LinkPanel's `sourceText` handles 'cdp' ("Found by CDP").
- [ ] **Step 4:** Rerun `bundles.js` → all PASS. Also rerun `$S/pw/sitemap.js` against the harness (the piece-C checks; update only checks this spec changed: idle colour and fit behaviour, noted as rulings). Then the frontend gate.
- [ ] **Step 5:** Commit `feat(network): site map draws bundles, keeps your zoom, and shows the internet`.

### Task 11: Frontend ports — frames, popover, Automatic

**Files:**
- Modify: `frontend/src/hooks/usePorts.ts` (PortView `role_source`, `role_reason`; PortPatch `role?: PortRole | null`), `components/network/drawings/parts.tsx`, `drawings/drawings.css`, `components/network/Faceplate.tsx` (PortCell, PortPopover), `pages/network/PortDetail.tsx`, `components/network/PortTable.tsx`, `utils/network.ts`
- Test: `$S/pw/bundles.js` (extend), plus a check in check.ts for the new `roleHint` helper

**Interfaces:**
- Produces: `roleLine(p: PortView): { text: string; sub: string } | null` in `utils/network.ts`: null for access. Text is "Network link to X · port N" (or "Network link" with no neighbour name) or "Internet (WAN)". Sub is the reason, or "Set by you" when role_source is user.

- [ ] **Step 1:** Write the checks:
  - check.ts `roleLine` cases: uplink auto with reason; wan user → sub "Set by you"; access → null.
  - In the harness:
    - A63's port 49 button has class `rf-role-uplink`, and its computed box-shadow contains `56, 189, 248`.
    - Hovering it shows "Network link to CORE-ES48-B · port 49" and the reason.
    - The FortiGate isn't drawn, so a UDM-SE fixture port 13 has `rf-role-wan`.
    - The PortDetail route `/network/devices/a/ports/49` shows a "Connects to" select whose first option is "Automatic". Choosing it sends PATCH `{role:null}`. Add PortDetail to the harness routes, with its adapter answers.
- [ ] **Step 2:** Run both → FAIL.
- [ ] **Step 3:** Implement:
  - **Frames:** in PortHit and PortCell, with the exact colours from Global Constraints. The "WAN" text sits under the wan frame.
  - **Popover:** the line from `roleLine`.
  - **PortDetail:** the select gets the Automatic option, shown selected when role_source isn't user. Its hint is "Found: <label> · <reason>", or "Nothing found: Endpoint".
  - **PortTable:** "· auto" after the role when role_source is auto.
- [ ] **Step 4:** Rerun → PASS. Then the frontend gate.
- [ ] **Step 5:** Commit `feat(network): ports found to be network links or WAN are marked, and can go back to automatic`.

### Task 12: Docs and the home-lab dry run

**Files:**
- Modify: `STATUS.md` (a "Site map bundles" section: what changed, the owner checks, the role side effects), `~/.claude/projects/-home-sysadmin-sentinel/memory/migration-numbering.md` (dev at 065)
- Create: nothing in the repo for the dry run (a scratchpad tool, never committed)

- [ ] **Step 1:** After deploying to the sandbox, check the back-fill: `SELECT name, role, role_source FROM device_interfaces WHERE role <> 'access'` must show `user` for Quantum-Gate eth8 and eth10 and QuantumLink 0/51. Then build a throwaway `cmd/zzroles`. It loads the sandbox DB and runs the Task 5 loading plus `Decide` for Quantum-Gate and QuantumLink, printing the would-be changes without writing them. Delete the directory before any commit, the same as zzwalk. Expected: none for eth8, eth10 and port 51 (they're user ports). Record any other change it would make.
- [ ] **Step 2:** Write the STATUS.md section and the memory update.
- [ ] **Step 3:** Commit `docs(status): site map bundles and automatic port roles`.
