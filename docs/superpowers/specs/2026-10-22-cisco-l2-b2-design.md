# Cisco L2 (B2): per-VLAN MAC tables, CDP fixes, Port-channels — design

**Date:** 2026-10-22
**Status:** approved by the owner 2026-10-21 ("Yes go with it"), after a read-only walk of three work Ciscos.
**Builds on:** `2026-10-18-find-device-switch-maps-design.md` ("Left open: Cisco") and `2026-10-19-site-map-bundles-design.md` (CDP, LAGs, automatic port roles), on dev at e5ba090.

## The walk (2026-10-21)

A one-off tool (not committed) walked LAB-C3850 (IOS-XE), BR-CORE-C9300 (C9300-48P, IOS-XE 17.12) and BR-ACC-2960X (WS-C2960X-48FPS-L, IOS 15.2) with the work read-only v2c profile, GET and GETBULK only, through Sentinel's own SNMP client and parsers. Nine seconds for all three. What it showed:

- **Q-BRIDGE is empty on all three.** The default context's dot1d table is VLAN 1 only: where VLAN 1 was also read in its own context, the rows were identical. The C9300 shows 15 MACs by default against 79 and 28 in two of its other VLANs; the 2960X 9 by default against 60, 26 and 23 in three others.
- **`community@vlan` works on all three.** A VLAN's three walks take 30–120 ms. Operational VLANs: C3850 3, C9300 13, 2960X 10.
- **Each VLAN's `dot1dBasePortIfIndex` lists only its own ports** (on the C3850, its busiest VLAN lists 3 of 18), and its numbering never disagrees with the default context. Every MAC mapped to an ifIndex.
- **MACs learned over a LAG are reported on the Port-channel** (on the C9300, 16 of one VLAN's 28 on Po1).
- **LLDP is not running** on any of them (`lldpLocalSystemData` is empty). CDP is the only neighbour source.
- **CDP parses correctly** on all 70 rows: domains stripped, every address type 1 (IPv4) with 4 bytes, no "(serial)" device IDs. Ports come in long form (`GigabitEthernet1/0/51`), which equals the far Cisco's ifDescr, so `sitemap.MatchNeighborPort` already finds the far port.
- **Name matching misses Ciscos named with their domain.** CDP sends `HQ-CORE-C9300`; Sentinel's name and sysName for it are `HQ-CORE-C9300.corp.example`, and the address CDP sends (an SVI) isn't the one Sentinel polls. So four CH 2960X uplinks to it are not marked today. The other direction only matched because those addresses happened to be the polled ones.
- **CDP capabilities (`cdpCacheCapabilities`, column 9) are not read.** Real values: phones 0x490 (host, phone, two-port MAC relay), switches and routers 0x29 (router, switch, IGMP), a 3750 0x28, UniFi 0x10 (host), MikroTik 0x01 (router). CDP rows are stored with caps 0, so placement classes any port with a CDP neighbour as an edge port.
- **CDP is heard through switches that don't speak it.** The C3850's only uplink, Te1/1/4, lists 15 CDP neighbours (14 phones and a UniFi); HQ-CORE-C9300's Te1/1/2 lists 9 and Te1/1/3 lists 6. Those uplinks class as edge ports today.
- **Every UniFi device sends the device ID `UBNT`.** Several on one port collapse into one stored row (the key is device, port, protocol, chassis ID).
- **Port-channels are ifType 53 (propVirtual), not 161.** So are SVIs (`Vl20`), `StackPort1` and `StackSub-*`, so ifType 53 alone doesn't mean a LAG.
- **`dot3adAggPortAttachedAggID` finds LACP members** (C9300 Te1/1/1–2 → Po1, 2960X Gi1/0/51–52 → Po1; CDP shows these are the two ends of one bundle) **but only LACP ones.** The C9300's Po10 over Te1/1/3–5 (static or PAgP, down) is only in `ifStackTable`, which is read only when dot3ad finds nothing.
- The C3850's hostname was never set: its sysName is `Switch.corp.example`.
- The 2960X took 5.2 s to answer the CDP table: fine in a 5-minute poll.

## Goals

1. **Per-VLAN MAC tables on Cisco**, so endpoints on every VLAN are found, not only VLAN 1's.
2. **CDP that matches and reads right:** names with or without the domain, a short port in the reason, capabilities, ports where CDP is heard through another switch, and UniFi devices told apart.
3. **Cisco Port-channels as LAGs:** named, members found for LACP and non-LACP channels, and the MACs learned on one placed as being beyond a link.
4. **The real port names** of the Cisco models still waiting for drawings, recorded.

## Non-goals

- No Cisco drawings: the owner sends photos first.
- Port-channels' stats collection stays as it is. `portmon.IsLAG` (ifType 161) also drives `DefaultCollect`, and inventory rewrites `collect_default`; counting Port-channels there would start polling them and could count site traffic twice. A follow-up if wanted.
- SNMPv3 `vlan-<id>` contexts are built from the docs; there is no v3 Cisco to test against.
- LLDP on the Ciscos: nothing to build (the owner could turn it on with `lldp run`).
- Tuning the 8-MAC threshold and the 5-minute interval.

## 1. Per-VLAN MAC tables

In `L2Monitor.writeSightings`:

1. Q-BRIDGE as today. Rows → use them, done.
2. Q-BRIDGE answered with no rows → walk CISCO-VTP-MIB `vtpVlanState` (1.3.6.1.4.1.9.9.46.1.3.1.1.2) and `vtpVlanType` (.3), index managementDomain.vlan. The VLANs read are those operational (state 1), ethernet (type 1) and not 1002–1005.
3. One or more VLANs → for each, in that VLAN's context, walk `dot1dTpFdbPort`, `dot1dTpFdbStatus` and `dot1dBasePortIfIndex`. Each entry's VLAN is that VLAN's ID; its bridge port maps through that VLAN's base table, else the default context's (the walk found their numbering always agrees), else as an ifIndex the device has (as `bridgeToIfIndex` does today), else it's skipped. The default context's dot1d walk is not made (it is VLAN 1, which is read in its own context).
4. No VLAN table (a non-Cisco, or the walk fails or is empty) → the default context's dot1d, as today.

**The context:** `snmp.Target.InVLAN(vlan)`. v1/v2c: the community becomes `<community>@<vlan>`. v3: the context name becomes `vlan-<vlan>` (a new `Target.Context`, passed to gosnmp's `ContextName`).

**Failures:** a VLAN whose port walk fails is skipped; its stored sightings are left alone, as with every other walk (rows are only upserted, never deleted, by the sightings write). A failed status walk keeps the entries (statuses are optional today). A failed base walk leaves the default context's base table to map through. When no VLAN's port walk gives rows (a community or v3 user without access to the VLAN contexts, which usually answers them empty rather than failing), the default context is read as today, so VLAN 1 is still found. A switch that refuses `community@vlan` times out on each VLAN, so after two failures with no VLAN read the rest wait for the next poll. (Both from the final review, 2026-10-22.)

**Old rows:** the dot1d rows stored so far have VLAN 0. They stop being refreshed and go with the 30-day purge.

## 2. CDP

**2a. Names with or without the domain.** `sitemap.MatchNeighborDevice` (site maps and port roles) tries, in order: chassis MAC (today); the name exactly, against sysName or name (today); then the name with everything from the first dot removed, on both sides, when exactly one device matches that way (a generic short name like `Switch` matching two devices matches none). A name that is an IP address is never shortened. Then the address (today). Placement's `world.monitored` gets the same shortened-name step, after its exact one.

**2b. The reason's port.** For CDP the far port is matched with `MatchNeighborPort` (as LLDP already is) and the reason shows its ifName: "Found by CDP: HQ-CORE-C9300.corp.example port Te1/1/6". When no far port matches, the port as sent: "… port GigabitEthernet1/0/25". LLDP reasons are unchanged (they show the faceplate label).

**2c. Capabilities.** `ParseCDP` reads column 9 (4 bytes, big-endian) and maps it onto the LLDP bits `l2.Caps` uses: router (0x01) → router; trans-bridge (0x02), source-route bridge (0x04), switch (0x08) → bridge; repeater (0x40) → repeater; phone (0x80) → telephone; host (0x10) → station. `writeCDP` walks column 9 too and stores the caps. So a CDP switch or router is a link, and a phone is a phone, in `ClassifyPort`.

**2d. Several CDP devices on one port.** A Cisco switch keeps the CDP it hears, so CDP rows from two or more different devices on one port nearly always mean a switch that doesn't speak CDP is in between. (The exception is a phone passing on CDP from a device on its PC port, as BR-CORE-C9300's Gi1/0/15 may be, an ATA and a phone: that port shows as behind, which still names the right port.) Placement classes such a port as behind (busy with nothing known beyond), never edge, unless an earlier rule makes it an AP or a link. The AP and link checks (a monitored AP, switch or router) look at every neighbour on the port, not only the latest.

**2e. IP device IDs.** A device ID that is an IP address keeps its whole name as the system name (today "10.99.1.1" becomes "10").

**2f. UniFi apart.** On one port, CDP rows with the same device ID but different addresses are stored apart: their chassis key becomes `<device ID>@<address>`. A device ID heard once on a port keeps the plain key. The system name stays `UBNT`.

## 3. Port-channels

**3a. What an aggregator is:** `portmon.IsAggregator`: ifType 161, or ifType 53 whose ifDescr starts `Port-channel`. Used by `writeLAG` (which ports are aggregators) and the port role service's `LagNames`. `IsLAG` is unchanged (see Non-goals).

**3b. Members:** dot3ad as today; then, when the device has an aggregator dot3ad names no member of, `ifStackStatus` is walked and its active rows (higher an aggregator, lower not) add members for those aggregators only. Today ifStack is only used when dot3ad names no member at all. `ifStackTable` is configuration, so a down channel's members are listed; down ports keep what they have, as today.

**3c. MACs on a Port-channel:** in placement, an aggregator one of whose members is a link (by the existing rules: a port link, the uplink role, a monitored neighbour, a bridge or router neighbour) is a link too, so the MACs learned on it are the far side's. Today it has no neighbour of its own and is classed behind when busy.

## 4. Drawing models

A comment in `backend/internal/portmon/drawings.go` records the real port names of the Cisco models still waiting for drawings (C9300-24P, WS-C2960CX-8PC-L, WS-C2960X-24PS-L, WS-C2960X-48FPS-L, WS-C2960X-48FPD-L), from the work instance's stored interfaces. No drawing until the owner sends photos.

## Testing

Test first throughout. Fixtures use made-up MACs, addresses and names in the shapes the walk showed (no work data in the repo).

- `l2`: VLAN list parsing (state, type, reserved VLANs); CDP capabilities mapping and the UBNT key; `ParseLAG` adding ifStack members only for aggregators dot3ad leaves empty.
- `snmp`: `InVLAN` for v2c and v3; `ContextName` reaches gosnmp.
- `portmon`: `IsAggregator` (Port-channel yes; `Vl20`, `StackPort1` no; 161 yes).
- `sitemap`: shortened-name matching, the ambiguity rule, IP names left alone.
- `l2.ClassifyPort`: shared-CDP ports are behind; a LAG with a link member is a link.
- DB: the L2 poll reads per-VLAN tables (a fake client answering by community/context), skips a failing VLAN and keeps its rows, falls back to the default context without a VLAN table; CDP caps and UniFi keys stored; Port-channel LAG members from dot3ad and ifStack; port roles from a CDP name without the domain, with the short port; placement of a shared-CDP port and of MACs on a Port-channel.
