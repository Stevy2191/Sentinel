# Network monitoring phase 2: ports and bandwidth

Part of the network monitoring roadmap
(`2026-09-28-network-monitoring-roadmap.md`), building on phase 1
(`2026-09-29-network-phase1-snmp-foundation-design.md`). Phase 2 turns the
devices phase 1 discovered into a usable switch monitor: per-port traffic,
errors and link state every minute, stored long-term in the generic metrics
model every later phase builds on; a port event log; warnings and alerts for
ports that matter; and a device page that draws the switch.

## Problem

Phase 1 knows a switch is reachable and what ports it has, but nothing about
what those ports are doing. There is no way to see how busy an uplink is, notice
a port throwing errors, learn that a camera dropped off, or look back at last
month's traffic. There is also nowhere to store any of it: the `metrics`
schema phase 0 created is empty.

Two smaller gaps from phase 1 are folded in: SNMP-reported identity is
sometimes wrong or missing (UniFi consoles), and a failed ifXTable walk blanks
port names for up to 15 minutes.

## Decisions

Taken with the user during design, 2026-09-30:

1. **The device page draws the switch**: a virtual faceplate, with each port
   coloured by state and filled from the bottom by how busy it is (mockup B,
   chosen over a colour-only faceplate and a labelled grid).
2. **Colours**: in use = green; problem = yellow; important port down = red;
   nothing plugged in = grey; disabled = dark. "Problem" is any of four
   conditions: errors/discards rising, flapping, slower link than usual,
   nearly full.
3. **Important ports alert on all four conditions plus link down.** Other
   ports log events and colour the faceplate but never alert.
4. **Device detail overrides** for vendor, model, location and device type.
   An override always wins over what SNMP reports; inventory never
   overwrites it. Serial number is not overridable.
5. **Storage is one generic model**: a series table plus a TimescaleDB samples
   hypertable, rates computed at poll time, 5-minute and hourly rollups
   (roadmap decision 5). Chosen over a dedicated port-stats table and over
   storing raw counters.
6. **Retention**: raw 1-minute samples for 365 days, rollups forever, both
   settings.
7. **Folded in**: the overrides (decision 4) and keeping stored ifX values
   when the ifXTable walk fails.

## Scope

In: sections 1–7 below.

Out, each to a later phase: PoE per port, LLDP neighbours and 10-second live
polling (phase 6); custom OIDs and MIB upload (phase 3); dashboards (phase 4);
metric reports (phase 5); UniFi controller data (phase 7); wireless client and
radio metrics (phase 7).

## 1. Collection

### Stats poll

A **stats poll** runs every 60 s per device, dispatched through the phase 1
worker pool beside the reachability poll. It runs only while the device's
status is `up`. Device start times are spread across the minute (a stable
per-device offset) so polls do not all fire together.

It reads, for every **collected** interface (below):

| Value | OID (IF-MIB) | Fallback |
|---|---|---|
| Octets in / out | ifHCInOctets / ifHCOutOctets | ifInOctets / ifOutOctets (32-bit) |
| Errors in / out | ifInErrors / ifOutErrors | |
| Discards in / out | ifInDiscards / ifOutDiscards | |
| Link state | ifOperStatus, ifAdminStatus | |
| Last change | ifLastChange | |
| Speed | ifHighSpeed | ifSpeed |

v1 devices have no 64-bit counters and no GETBULK: they use the 32-bit
fallbacks and GET requests (phase 1 decision 1). v2c/v3 devices use GET
requests batched by interface for the collected set, so a switch with 52
collected ports needs about 10 requests per poll.

Each device records how long its last stats poll took
(`devices.last_stats_duration_ms`), shown on the device page. It is also how the
first build task measures a real switch before the plan's worker defaults are
fixed.

### Which interfaces are collected

Each interface gets a **collect** flag. Its default comes from a pure
classifier:

- **Physical ports are collected**: `ifConnectorPresent` (ifXTable) is true, or,
  where the device does not report it, the ifType is an Ethernet type (6, 62,
  69, 117) and the name does not match a virtual pattern.
- **Link aggregates** (ifType 161) are collected.
- **Everything else is not**: loopback, CPU interfaces, bridges, VLAN and
  sub-interfaces, tunnels, radios and virtual Ethernet. Name patterns cover
  what devices report as ifType 6 anyway (`br*`, `vlan*`, `*.<n>`, `wlan*`,
  `ath*`, `ra*`, `veth*`, `docker*`, `tun*`, `lo`, `imq*`, `ifb*`).

A user's explicit choice overrides the default (`collect` is nullable: null
means "use the default"). `ifConnectorPresent` is added to the phase 1
inventory walk.

### Counter to rate

The poller keeps the last reading per interface in memory (counter values,
sysUpTime and the time read) and writes **rates**:

- Bits per second in and out: `Δoctets × 8 / Δt`.
- Errors and discards per minute.
- Utilisation in and out, as a percentage of the link speed. It is not
  written when the speed is 0 or unknown.
- Link speed, as a series (for "usual speed", section 3, and for charts).

Rules:

- **Wrap**: a 32-bit counter that went down is a wrap when adding 2³² gives a
  plausible rate (≤ link speed × 1.1, or ≤ 10 Gb/s when speed is unknown). A
  64-bit counter is never treated as wrapped.
- **Discontinuity**: the interval is skipped (nothing written) when sysUpTime
  went backwards, a counter went down without being a plausible wrap, the
  interface's ifIndex set changed, or Δt is outside 0.5×–3× the poll interval.
- **First reading** after a start, restart or skipped interval only sets the
  baseline.
- **Down ports** (oper status not up) write no rate samples. Charts show the
  gap as "down" rather than as zero traffic.

This logic is a pure function, table-tested (section 7).

### Keep ifX values when the ifXTable walk fails

When the inventory's ifXTable walk fails or returns nothing for an interface
that it answered before, `SaveInventory` keeps the stored name, alias and
high speed instead of overwriting them with empty values. The stats poll
treats a missing 64-bit counter on a device that previously had one as a
skipped interval, not a switch to 32-bit counters.

## 2. Storage

Migration `048_metrics.sql`, entirely in the `metrics` schema. All times are
`TIMESTAMPTZ`.

```sql
CREATE TABLE metrics.series (
    id           BIGSERIAL PRIMARY KEY,
    device_id    UUID NOT NULL,          -- no FK: see "Backups"
    metric       TEXT NOT NULL,          -- key from the metric catalogue
    instance     TEXT NOT NULL DEFAULT '', -- ifIndex for ports; '' for device-level
    interface_id UUID,                   -- no FK; set for port series
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (device_id, metric, instance)
);

CREATE TABLE metrics.samples (
    time      TIMESTAMPTZ NOT NULL,
    series_id BIGINT NOT NULL,
    value     DOUBLE PRECISION NOT NULL
);
-- hypertable on time, 1-day chunks; index (series_id, time DESC)
-- compression: segmentby series_id, orderby time DESC, after 2 days
-- retention: metrics_raw_retention_days (default 365)
```

**Metric catalogue**, a fixed list in Go, each with a unit and kind (phase 3
adds user-defined entries):

| Key | Unit |
|---|---|
| `if_in_bps`, `if_out_bps` | bits/s |
| `if_in_util_pct`, `if_out_util_pct` | % |
| `if_in_errors_pm`, `if_out_errors_pm` | per minute |
| `if_in_discards_pm`, `if_out_discards_pm` | per minute |
| `if_speed_bps` | bits/s |

**Rollups** are continuous aggregates:

- `metrics.samples_5m` over samples: per series and 5-minute bucket, the `min`,
  `max`, `sum` and `count`.
- `metrics.samples_1h`, hierarchical on `samples_5m`: `min` of mins, `max` of
  maxes, `sum` of sums, `sum` of counts. So an hourly average is
  `sum / count`, weighted correctly, never an average of averages.
- Both use real-time aggregation (`materialized_only = false`), so the newest
  minutes appear in charts.
- They are created `WITH NO DATA`. The migration runner
  (`database/migrate.go`) sends each file as one multi-statement query, which
  Postgres runs as one implicit transaction, and a continuous aggregate
  created with data cannot be built inside a transaction. Refresh policies
  fill them.
- The refresh windows (5m: last 3 hours; 1h: last 2 days) are far inside the
  raw retention, so dropping raw chunks never erases rollup rows.
- Rollups have no retention policy.

**Resolution by range**: raw samples for ranges up to 6 h, the 5-minute rollup
up to 7 days, hourly beyond. The 95th percentile (phase 5) is computed from
5-minute averages, the standard for bandwidth reporting.

**Settings** (the key/value settings pattern): `metrics_raw_retention_days`
(default 365, minimum 7). Changing it replaces the retention policy at
runtime. The rollups kept forever are not a setting.

**Backups**: nothing in `metrics` has a foreign key into `public`. A config
restore runs `DROP TABLE IF EXISTS public.devices` without `CASCADE`, which a
cross-schema FK would block, failing the restore halfway. Instead:

- `DeviceService.Delete` removes the device's series rows in the same
  operation.
- A nightly cleanup deletes samples whose series no longer exists, and series
  whose device no longer exists.
- Device IDs are UUIDs that a config restore preserves, so a device's graphs
  survive a restore.
- Backups remain configuration-only (phase 0).

**Writes**: one batch insert per device per poll. A failed batch is logged and
that minute is lost; it is not retried.

## 3. Port events and conditions

### Event log

Migration `049_port_monitoring.sql` adds `public.port_events` (small and
config-adjacent, like incidents, so it lives in `public` with FKs):

```
id, device_id FK CASCADE, interface_id FK CASCADE, if_index,
kind        -- link_up | link_down | flapping | speed_change | errors |
            -- saturated | admin_up | admin_down
started_at, ended_at NULL, -- ended_at for span events (flapping, errors, saturated)
detail JSONB               -- before/after speeds, flap count, peak values
```

Every collected port logs events, important or not. They are kept for the raw
retention period (the same nightly cleanup).

**Flap grouping**: 3 or more link transitions within 10 minutes open one
`flapping` event instead of more up/down events. It ends after 10 minutes with
no transition and records the transition count. A transition between two polls
is detected when `ifLastChange` moved even though the state is the same as last
poll (a down-and-up within one minute). That counts as two transitions.

### Conditions

A pure evaluator takes a port's recent readings (an in-memory window per
interface) and its thresholds, and returns which conditions are active:

| Condition | Starts | Ends |
|---|---|---|
| `errors` | errors + discards (in + out) ≥ threshold (default 10/min) in each of the last 5 polls | below threshold for 5 consecutive polls |
| `flapping` | as flap grouping above | 10 minutes without a transition |
| `slow_link` | link up at a speed below the port's usual speed | speed back to usual, or link down |
| `saturated` | in or out utilisation ≥ threshold (default 80%) averaged over the last 5 polls | average below threshold − 10 points (default 70%) |

**Usual speed** is recomputed nightly per port: the value of `if_speed_bps` seen
in the most 5-minute buckets over the last 7 days. It is only set once the port
has 24 hours of history; until then `slow_link` never fires. It is stored on
the interface (`usual_speed_bps`).

Active conditions persist on the interface (`conditions TEXT[]`,
`conditions_since JSONB`). After a restart, the evaluator rebuilds its windows
from new polls without ending or re-opening anything. A condition only ends on
evidence (the "Ends" column), never because a window is empty.

**When the device is not `up`**, port evaluation stops. Conditions, events and
incidents stay as they were until the device is back.

### Thresholds

The global defaults are settings: `port_error_threshold_per_min` (10),
`port_util_threshold_pct` (80) and `port_down_grace_seconds` (120). Each
interface has nullable overrides for the same three, where null means use the
default. The flap rule (3 in 10 minutes) is fixed.

## 4. Important ports, incidents and alerts

Interfaces gain `important BOOLEAN NOT NULL DEFAULT false`.

For important ports:

- **Link down** (oper down while admin up) for longer than the grace period
  opens an incident. The faceplate turns red immediately; only the incident
  waits for the grace. Link up closes it.
- **Each condition** (`errors`, `flapping`, `slow_link`, `saturated`) opens an
  incident when it starts and closes it when it ends.

**Incidents** gain `interface_id UUID NULL REFERENCES device_interfaces ON
DELETE CASCADE` and `condition VARCHAR(20) NULL`:

- A port incident has `device_id`, `interface_id` and `condition` set. A CHECK
  requires `interface_id` to be null unless `device_id` is set, and
  `condition` to be set exactly when `interface_id` is.
- A partial unique index on `(interface_id, condition) WHERE end_time IS NULL`
  allows one open incident per port per condition.
- Like device incidents, port incidents open and close automatically: a manual
  status change is rejected with 400. Comments are allowed.
- Access follows the device's site, as for device incidents.
- Un-marking a port as important closes its open port incidents with a note.

**Notifications** go through the device's `notify_channels`. Notifications gain
`interface_id` (nullable). Messages name the device, port and alias, and the
reading, e.g. "QuantumLink port 51 (Uplink To Quantum Gate) is nearly full: 82%
out". Recovery messages give the duration. `ViewPath` links to the port page.

**Device down suppresses port incidents**: while the device is not `up`, no
port incident opens, and open ones stay open.

## 5. Device overrides and device type

Devices gain:

- `vendor_override`, `model_override`, `location_override` (TEXT, nullable).
- `device_type` (nullable override) and `device_type_detected`, each one of
  `switch | router | access_point | nvr | other`.
- `faceplate_rows` (nullable: 1 or 2) and `faceplate_sfp_ports` (nullable
  `INTEGER[]` of port numbers).

Inventory writes only the SNMP fields and `device_type_detected`. It never
writes an override. The API returns the effective value (the override if set,
otherwise SNMP) and the reported value separately, so the UI can show "SNMP
says: UNVR4".

**Detected type** comes from a pure function of model and interfaces:

- USW*, US-*, EdgeSwitch, ES-* → switch.
- UDM*, UXG*, USG*, ER-* → router.
- U6*, U7*, UAP*, UAL*, UK* → access point.
- UNVR* → nvr.
- Otherwise: 8 or more collected physical ports → switch, else other.

Overrides are edited through a new `PATCH /network/devices/:id/details`
endpoint, rather than the phase 1 full-replace `PUT`. Only fields present in
the body change, and an explicit `null` clears an override. It needs editable
access to the device's site.

## 6. Pages and API

### API

All under the existing network API group, with access through the device's
site as in phase 1:

| Method + path | Purpose |
|---|---|
| `GET /network/devices/:id/ports` | collected ports with latest rates, utilisation, conditions, speed and usual speed, important/collect flags, and the faceplate layout |
| `PATCH /network/devices/:id/ports/:ifIndex` | `important`, `collect`, threshold overrides (editable access) |
| `GET /network/devices/:id/ports/:ifIndex` | one port: the above plus open incidents |
| `GET /network/devices/:id/events` / `GET /network/sites/:id/events` | port events, newest first, paged |
| `GET /network/metrics/query` | generic time-series query (below) |
| `GET /network/sites/:id/ports/summary` | busiest 5 ports and every port with an active condition or important-down |
| `PATCH /network/devices/:id/details` | overrides (section 5) |

**`/network/metrics/query`** is the generic read path phases 4–6 reuse:

- **Parameters:** `device_id`, `metric` (repeatable), optional `instance`
  (repeatable), `from`, `to`, optional `agg=sum` (sum across instances per
  bucket, for device and site totals).
- **Returns:** one series per metric/instance: points `(time, avg, min, max)`
  at the resolution picked by range (section 2), at most about 500 points per
  series.
- **Access:** checked per device, and devices the caller cannot see give 404.

### Faceplate layout

A pure function turns the collected physical ports (not aggregates) into rows
and blocks:

- **Port number** is parsed from the name: `0/12`, `1/0/12`, `Port 12`, `eth12`,
  `Slot: 0 Port: 12`, else ifIndex order.
- **SFP ports** are those whose descr or name contains `SFP` or a speed of
  10G or more (`10G`, `25G`, `40G`, `100G`), or that are listed in
  `faceplate_sfp_ports`. They form their own block on the right.
- **Rows**: 8 or fewer copper ports sit in one row; more go in two rows with
  odd numbers on top, in blocks of 12 ports. `faceplate_rows` overrides this.

The layout is returned by `GET …/ports`, so the frontend only draws.

### Pages

- **Device page** (reworked):
  - A header with the effective identity and an **Edit details** modal: each
    field shows the SNMP value, with **Use SNMP value** to clear the override.
  - The **faceplate** for switches and routers (mockup B):
    - each port coloured by precedence: red (important and down), yellow
      (any condition), green (up), grey (down), dark (admin down);
    - filled by the higher of in/out utilisation;
    - refreshed every 60 s;
    - clicking a port opens a detail panel linking to the port page.
  - The device traffic chart (sum of in/out bps, ranges 1h/6h/24h/7d/30d/90d/1y).
  - A sortable port table (number, name, status, speed, in, out, % busy,
    errors, important ★, collect).
  - Recent port events.
  - Access points, NVRs and "other" get the port table without a faceplate.
- **Port page** (new, `/network/devices/:id/ports/:ifIndex`):
  - status, alias, current and usual speed;
  - the important toggle and threshold overrides, showing defaults when unset;
  - charts: traffic in/out, % busy with the threshold line, errors and
    discards;
  - events and open incidents.
- **Site page** (added to): a site traffic chart, the busiest 5 ports, ports
  with problems, and recent events.

Charts use Recharts (already a dependency) and the existing design system.
View-only users see everything and get no toggles or edit controls.

## 7. Failures, testing, rollout

### Failure handling

- **A stats poll that fails or partially answers** skips that interval for the
  affected interfaces and keeps the counter baselines. It never changes device
  status, which stays the reachability poll's job.
- **Startup preflight** (phase 0) is extended: TimescaleDB must be the full
  (TSL) build (`timescaledb.license = 'timescale'`), since compression and
  continuous aggregates are unavailable in the Apache-only build. Otherwise
  the backend refuses to start with a clear message.
- **Migration 048 is idempotent** (`IF NOT EXISTS`, `if_not_exists => true` on
  policies), matching 044's restore-safety rule.

### Testing

- **Pure, table-driven**:
  - counter→rate: 32- and 64-bit wrap, implausible wrap, sysUpTime backwards,
    first reading, Δt out of range, speed 0;
  - the condition evaluator: every start/end row above, hysteresis, the
    grace period, flap counting including the `ifLastChange` case;
  - the interface classifier and detected device type, against the real
    interface lists of QuantumLink, the EdgeSwitch simulator, the UDM-SE and a
    U7 (captured as fixtures);
  - faceplate layout;
  - usual speed.
- **Real database** (`internal/testdb`, TimescaleDB image):
  - migrations create the hypertable, compression, retention and refresh
    policies;
  - rollup rows carry the correct min/max/sum/count;
  - the retention setting replaces the policy;
  - device delete removes its series, and the nightly cleanup removes
    orphans;
  - **a config backup restore succeeds with the metrics tables populated**;
  - the port incident constraints (one open per port per condition, the
    CHECKs);
  - query-endpoint access filtering.
- **Simulator**:
  - snmpsim's EdgeSwitch profile gains counters that increase between reads,
    and one port that flaps on a schedule;
  - an end-to-end test polls it twice and asserts rates and a flap event.
- **Frontend**: lint and build, then browser checks in the sandbox.

### Rollout

The sandbox first, then `dev` after the user verifies it there. The sandbox
checklist uses the user's own devices:

- the faceplate matches the physical USW-Pro-48;
- mark a port important and unplug its device: red immediately, incident and
  alert after the grace period, recovery on replug;
- the uplink's traffic chart moves;
- edit the UNVR's model, and it survives the next inventory;
- the stats poll duration is recorded.

Live remains blocked on phase 0 (the TimescaleDB image) and ships only when the
user asks.

## Risks

- **Rollup shape is load-bearing for phase 5.** Reports need 95th percentiles
  and volumes over long ranges. min/max/sum/count per 5-minute bucket supports
  both (p95 over 5-minute averages; volume = sum of rate × bucket length).
  Anything else phase 5 needs would mean adding a rollup, not changing one.
- **Classifier misses.** Devices that report virtual interfaces as physical
  Ethernet without `ifConnectorPresent` will have extra ports collected. The
  per-interface collect toggle is the escape hatch; the fixtures cover the
  user's real gear.
- **Poll load over VPN.** Unmeasured until the first task. The stats duration
  field and the spread start offsets make it visible and bounded.
