# Network monitoring roadmap: SNMP, custom metrics, dashboards, reports, live maps

This is a roadmap, not a design. Each phase below gets its own design spec,
implementation plan and sandbox verification before it merges. What this
document fixes is the overall shape, the decisions every phase inherits, and
the order.

## Problem

Sentinel watches websites, services, certificates and servers, but not the
network those depend on. There is no way to see that a switch uplink went
down, that a port is saturated or throwing errors, how busy an access point
is, or how traffic moves between devices at a site.

The target is MSP-scale: many sites, well over a hundred devices, thousands of
ports, a mix of Ubiquiti EdgeSwitches, UniFi, and Cisco/Meraki hardware.

## Goals

- SNMP is the base. Any device that speaks standard SNMP is monitorable out
  of the box: reachability, interface inventory, port status, per-port and
  device-wide bandwidth, errors and discards.
- Vendor specifics come from **uploaded MIBs**, not from code. There are no
  per-vendor dashboards or integrations in the core; a user uploads a vendor
  MIB, picks the OIDs that matter, and they become ordinary metrics.
- **Dashboards are user-built.** Sentinel ships sensible default device and
  port pages, but anything beyond those is a dashboard of widgets a user
  assembles over any metric from any device or site.
- A **live topology map** per site: devices as nodes, links bound to chosen
  ports, bandwidth shown flowing along them in near real time.
- **Long-term full-resolution history** for bandwidth and other metrics.
- **Reports over any device and any metric**: every collected metric can be
  put in a scheduled, emailed PDF report, just as uptime is today.
- **Port changes are logged, not all alerted.** Every up/down is recorded;
  alerts fire only for ports marked important.

## Non-goals

- Vendor-specific dashboards (Meraki, Cisco or otherwise). Meraki switches
  are covered by SNMP like anything else.
- Full multi-tenancy (customers logging in to a walled-off instance). Sites
  are shared per user instead; see decision 3.
- Remote collectors. Sentinel polls everything centrally (decision 1). The
  poller is built behind an interface so collectors could be added later,
  but none are planned.
- Configuration management (pushing config to switches). Read-only
  monitoring only; SNMP write access is never used.

## Decisions every phase inherits

1. **Central polling.** The Sentinel backend polls every device itself, over
   the site-to-site VPN for remote sites. Consequences each phase must
   respect: UDP 161 must be reachable from the backend container; timeouts
   and retries are tuned for WAN links, not a LAN; polling uses a bounded
   worker pool and GETBULK walks so thousands of ports fit in one interval;
   targets go through the existing `netguard` private-network rules, since
   nearly every switch has a private address.

2. **Native Go poller on `gosnmp`**, inside the backend, next to the existing
   check scheduler, feeding the existing incidents and notification channels.
   No Telegraf, LibreNMS or other side system to run.

3. **Sites are first-class and shared per user.** Everyone internal sees every
   site; individual sites (and their maps and dashboards) can be shared with
   specific users, following the `monitor_sharing` pattern, or published
   read-only in the way status pages are.

4. **TimescaleDB for metric storage.** Keeping 1-minute data for months or
   years at thousands of ports is several million rows a day. Plain Postgres
   with rollups was the alternative; long-term full detail was chosen, so the
   database moves to the TimescaleDB image (same Postgres 16 major version)
   for hypertables, native compression and continuous aggregates.

5. **One generic metrics model.** Every collected value, whether built-in
   port stats, a custom OID from an uploaded MIB, or UniFi controller data,
   lands in the same shape: *device × metric × instance × time → value*
   (instance is e.g. an ifIndex, a radio, a client MAC). Dashboards, reports
   and maps query that one model and never need to know where a value came
   from. This is the decision that makes user-built dashboards and
   any-metric reports possible, so phase 2 must get it right before anything
   builds on it.

6. **Log everything, alert on the chosen.** Every port status change is an
   event, with rapid up/down cycles grouped into a single flap event. Alerts
   (incidents through existing channels) fire only for ports marked
   important, plus ports bound to a map link.

7. **Two polling speeds.** Ports shown on a live map are polled every ~10s
   and pushed to the browser; everything else every 60s.

8. **Timestamps are TIMESTAMPTZ** in every new table.

## Phases

### Phase 0 — Groundwork

- Move Postgres to the TimescaleDB image and enable the extension. This
  touches the live database, so the phase is: rehearse on the sandbox
  (`/srv/docker/sentinel-dev`), take a verified backup of live, then switch.
  Existing tables are unaffected; only new metric tables become hypertables.
- Add **Sites** (name, description, optional address) and per-site sharing.
  Existing monitors and agents may optionally be assigned to a site, but
  nothing requires it.

Depends on: nothing.

### Phase 1 — SNMP foundation

- **Credential profiles** (v2c community, v3 user/auth/priv), encrypted at
  rest with `cryptutil`, scoped to a site or global.
- **Devices**: add manually, or from a subnet scan that adds an SNMP probe to
  the existing Network Discovery. Vendor and model detected from
  `sysObjectID` / `sysDescr`.
- **Interface inventory** from IF-MIB (`ifTable` + `ifXTable`): name, alias,
  speed, type, admin/oper status. Refreshed periodically, so renamed or new
  ports appear.
- **Poller**: scheduler, bounded worker pool, per-device interval, backoff for
  unreachable devices.
- **Reachability** raises and resolves incidents like other monitor types.
- **Built-in MIBs** shipped with Sentinel: SNMPv2-SMI/TC/CONF/MIB, IF-MIB,
  IP-MIB, LLDP-MIB, ENTITY-MIB, HOST-RESOURCES-MIB and their dependencies.

Depends on: 0.

### Phase 2 — Ports and bandwidth

- The **generic metrics hypertable** (decision 5), with compression and
  retention policies and continuous aggregates for chart-friendly
  resolutions.
- **Counter-to-rate** conversion from 64-bit HC counters, correctly handling
  counter wraps and device reboots (sysUpTime going backwards).
- Per-port in/out bps, utilisation vs ifSpeed, errors, discards; device and
  site totals.
- **Port status events** with flap grouping; **important port** flag and
  alerting (decision 6).
- **Default pages**: site overview, device page with a port grid, port detail
  with charts.

Depends on: 1.

### Phase 3 — MIB library and custom metrics

- **Upload MIB files**; parse them (pure-Go SMI parser, e.g. `gosmi`),
  resolving IMPORTS against the built-in set and previously uploaded MIBs,
  and report missing dependencies clearly. Uploads are size-limited and
  parsed as untrusted input.
- **OID browser**: navigate the tree, read descriptions, and **test-walk** a
  live device to see real values before committing to anything.
- **Custom metric definitions**: scalar or table column, gauge or
  counter-to-rate, scale and unit, instance label from another column.
- **Device profiles** matched by `sysObjectID` prefix, so a metric defined
  once applies to every matching device automatically (e.g. every
  EdgeSwitch).
- Optional **thresholds** on custom metrics that raise alerts.

Depends on: 2.

### Phase 4 — Custom dashboards

- Drag-and-drop grid of widgets; dashboards are owned, shareable, and can be
  scoped to a site.
- Widget types: time series, stat/gauge, port status grid, top-N (busiest
  ports, most errors), event log, device table.
- Every widget picks its data from the generic model: any metric, any device
  or site, with a time range.

Depends on: 2. Most useful once 3 exists.

### Phase 5 — Metric reports

- A third fixed report type, **Metrics**, next to Uptime and Incident. It
  keeps the 2026-09-23 reporting overhaul's rule of fixed report types with
  no section picker: what the user picks is data (scope, metrics, period),
  not which sections to render.
- **Scope** extends the existing picker with sites, devices, device
  profiles and individual ports, alongside the monitor scopes.
- **Metrics**: any metric in the generic model, built-in or custom, so
  custom MIB metrics (phase 3) and UniFi data (phase 7) become reportable
  with no extra work.
- **Content**: a summary table per device/port (min, average, max, 95th
  percentile, and total volume for counters) and one chart per metric, with
  the 95th-percentile line. The chart generalises the `fpdf` uptime chart
  from the overhaul rather than adding a charting dependency.
- Aggregation reads the TimescaleDB continuous aggregates, so a year-long
  report over thousands of ports does not scan raw rows.
- Period picker, scheduling, email delivery and sharing carry over
  unchanged.

Depends on: 2. Covers custom metrics once 3 exists.

### Phase 6 — Live site maps

- **LLDP neighbor collection** to suggest links automatically.
- **Map editor**: devices as draggable nodes, links bound to a port at one or
  both ends.
- **Fast polling** for bound ports and an **SSE push channel** to the
  browser. This is Sentinel's first push mechanism; everything else polls.
- Links coloured and sized by utilisation, with animated flow in the
  direction of traffic; click through to the port.
- Read-only sharing and a fullscreen wall-display mode; a map can be
  embedded as a dashboard widget.

Depends on: 2 and 4.

### Phase 7 — UniFi controller source

- Optional per-site data source for the self-hosted UniFi Network controller
  API.
- UniFi devices matched to SNMP devices by MAC, so one device has one page.
- AP and radio metrics (channel, utilisation, client counts) and a wireless
  client list (who, which AP, band, signal, traffic), with client history,
  all written into the generic metrics model.

Depends on: 2.

### Later

Wanted, but not designed yet:

- Running a dashboard as a report (its widgets rendered into the PDF).
- A geographic multi-site view.
- **A site Power page** (asked for 2026-10-01). From a site, a Power option
  lists every UPS in that site at a glance: on mains or on battery, charge,
  runtime left, load, and any open UPS alerts — one screen to check during
  an outage instead of opening each UPS. It builds on UPS monitoring
  (`2026-10-01-ups-monitoring-design.md`, shipped to `dev`): the readings,
  alerts and Power panel styling exist; what is new is the site page and one
  request that returns every UPS in the site together.

## Order and milestones

Phases 0 → 1 → 2 are strictly sequential; after phase 2 Sentinel is a usable
switch monitor on its own. Phases 3, 4, 5 and 7 depend only on 2 and can be
reordered by priority; 6 needs 4. Each phase ships to the sandbox first and
merges to `dev` when verified there.

## Risks

- **Live database migration (phase 0).** Mitigated by a sandbox rehearsal
  and a verified backup before switching; the image change keeps the same
  Postgres major version so the data directory carries over.
- **Poll load across a VPN.** Thousands of ports every 60s plus 10s map
  polling. GETBULK, a bounded worker pool, and per-device backoff keep it in
  check; phase 1 should measure a real site before phase 2 fixes intervals.
- **MIB parsing (phase 3).** Real vendor MIBs are often sloppy. The parser
  must degrade gracefully and report what it could not resolve rather than
  reject the whole file.
- **Getting the metrics model wrong (phase 2).** Everything after builds on
  it. Its phase-2 design must be checked against dashboard, report, map and
  UniFi needs before it is built.
- **Report query cost (phase 5).** Percentiles and totals over long periods
  and many ports are expensive on raw data. The continuous aggregates
  designed in phase 2 must include what reports need (e.g. per-bucket max and
  a percentile-friendly form), or phase 5 will have to rebuild them.
