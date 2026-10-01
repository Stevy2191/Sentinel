# UPS monitoring: battery, load and power alerts

Builds on network phase 2 (`2026-09-30-network-phase2-ports-bandwidth-design.md`):
the per-device stats poll, the metrics store and port incidents. Devices whose
type is UPS (chosen or detected, since 052) get their battery, load and power
readings polled every minute, shown on the device page, kept as history, and
alerted on.

## Problem

A UPS shows up as a device that is "Up" and nothing more. The things that
matter about a UPS are whether it is running on battery, how much battery is
left and how loaded it is, and Sentinel reads none of them. The user's UPSes
(Tripp Lite SMART1500RM2UN, Eaton 9PX2000RT) both answer the standard UPS-MIB
(RFC 1628); the Tripp Lite card reports no interfaces at all, so today its
device page is empty.

## Decisions

Taken with the user, 2026-10-01:

1. **See it and be alerted** (option B): a Power panel with live readings and
   history charts, plus incidents and notifications.
2. **Three alerts**: on battery, low battery, high load.
3. **Defaults**: low battery below **25 %** charge (or when the UPS itself says
   low/depleted); high load at or above **80 %** held for **5 minutes**; on
   battery as soon as a poll sees it.
4. **Every UPS alerts**; there is no per-device alert switch.
5. **Thresholds** are instance-wide settings in Network settings, with an
   optional per-UPS override in Edit details.
6. **UPS alerts do not affect device availability**: a UPS on battery is up.

## What is read

Every stats poll of a device whose effective type is `ups` adds one SNMP GET
of these UPS-MIB scalars (`1.3.6.1.2.1.33.1` prefix). Line tables are read at
index 1 only (single-phase); three-phase UPSes show line 1.

| Reading | OID (under .33.1) | Scale | Metric key |
|---|---|---|---|
| Battery status | `.2.1.0` upsBatteryStatus | 1 unknown, 2 normal, 3 low, 4 depleted | — (condition input) |
| Seconds on battery | `.2.2.0` upsSecondsOnBattery | s | — (condition input) |
| Runtime left | `.2.3.0` upsEstimatedMinutesRemaining | min | `ups_runtime_min` |
| Charge | `.2.4.0` upsEstimatedChargeRemaining | % | `ups_charge_pct` |
| Battery temperature | `.2.7.0` upsBatteryTemperature | °C | `ups_battery_temp_c` |
| Input voltage | `.3.3.1.3.1` upsInputVoltage | V RMS | `ups_input_v` |
| Output source | `.4.1.0` upsOutputSource | 3 normal, 4 bypass, 5 battery, … | `ups_on_battery` (0/1) |
| Output voltage | `.4.4.1.2.1` upsOutputVoltage | V RMS | `ups_output_v` |
| Load | `.4.4.1.5.1` upsOutputPercentLoad | % | `ups_load_pct` |

A reading the agent does not answer (noSuchObject/noSuchInstance) is simply
absent: no sample is written and no condition is decided from it. The new
metric keys join `MetricCatalogue` so the existing metrics API serves them;
samples carry no interface (`interface_id` NULL) and instance `""`.

**On battery** is `upsOutputSource = 5`. When the agent does not report the
output source, `upsSecondsOnBattery > 0` decides instead; with neither, the
state is unknown.

When the GET fails outright (timeout), nothing is written and no condition
changes: a device that stops answering is already covered by the device-down
incident.

## Conditions

A pure package, `internal/upsmon`, holds the rules, mirroring `portmon`'s
Tracker: `Evaluate(prev State, r Readings, th Thresholds, now time.Time)
(State, []Change)`. It is fully unit-tested without a database.

| Condition | Starts | Clears |
|---|---|---|
| `ups_on_battery` | on battery (above) is true | on battery is false |
| `ups_low_battery` | battery status is low (3) or depleted (4), **or** charge < low-battery threshold | status is not low/depleted **and** charge ≥ threshold (a missing charge counts as ≥) |
| `ups_high_load` | load ≥ high-load threshold continuously for 5 minutes (the first over-threshold poll starts the clock; any poll below resets it) | load < threshold |

An unknown input (absent reading) leaves that condition as it was. Change
detail carries the values that triggered it (charge, runtime, load, threshold)
for the alert sentence.

After a restart the tracker is rebuilt from the device's open UPS incidents,
so an outage in progress is not re-alerted; the high-load 5-minute clock
starts again, which can only delay an alert, never duplicate one.

## Incidents and notifications

UPS incidents are device-level incidents with a condition: `device_id` set,
`interface_id` NULL, `condition` one of the three above. Migration 055:

- the `incidents_port_check` constraint is replaced so a row is either a
  device-down incident (`interface_id` and `condition` NULL), a port incident
  (as today), or a UPS incident (`interface_id` NULL, `device_id` NOT NULL,
  `condition IS NOT NULL AND condition IN (…three…)`);
- a unique partial index keeps one open incident per (device, condition):
  `ON incidents (device_id, condition) WHERE end_time IS NULL AND interface_id IS NULL AND condition IS NOT NULL`.

Every query that today means "device-down incident" by `interface_id IS
NULL` gains `AND condition IS NULL`: device availability
(`device_service.go`), and the active/close device incident lookups
(`incident_service.go`). A test proves a UPS incident neither lowers
availability nor is closed when the device comes back up.

`IncidentService` gains `OpenDeviceConditionIncident` /
`CloseDeviceConditionIncident` (idempotent per device and condition, like the
port pair) and `OpenDeviceConditionIncidents(deviceID)` for the restart
rebuild and reconciliation.

Alerts follow the port alert path: a condition starting opens the incident and
notifies; ending closes it and sends the recovery; an active condition with no
open incident is ensured each poll (retries a failed open). Reconciliation
closes, without notifying, any open UPS incident whose condition is not active
— including every one on a device that is no longer typed UPS or is deleted
(the cascade handles deletion).

Wording, with the device's display name:

- "EXP-BEB-SMART1500 is on battery (charge 96 %, 41 min left)" / "… is back on mains power"
- "EXP-BEB-SMART1500 battery is low (charge 22 %, 9 min left)" / "… battery has recovered (charge 30 %)"
- "EXP-BEB-SMART1500 load is high (86 %, threshold 80 %)" / "… load is back to normal (64 %)"

Incident titles read "EXP-BEB-SMART1500 · On battery" (· Low battery, · High
load), and the incident page names the source as "The UPS poll: On battery".

## Thresholds

Settings (key/value, seeded defaults, validated in `NetworkSettingsPatch`):

- `ups_low_battery_pct`, default 25, allowed 5–95
- `ups_high_load_pct`, default 80, allowed 10–100

Per-device overrides, migration 055: `devices.ups_low_battery_pct` and
`devices.ups_high_load_pct`, nullable INTEGER with the same CHECK ranges
(NULL = the instance setting). They are set through the existing
`PATCH /devices/:id/details` (`DeviceDetailsPatch`), audited with the other
details. The 5-minute high-load hold is a constant.

## Device page

For a UPS, a **Power** section sits above Traffic:

- **Status line**: a badge, *On mains* (green), *On battery* (amber) or
  *Unknown*, and the battery status when it is not normal.
- **Tiles**: Charge %, Runtime left, Load %, Input V, Output V, Battery °C.
  A tile whose reading the UPS does not report is left out; values come from
  the latest samples (same freshness rule as port figures).
- **Charts**: Charge and Load (%, one chart, two series) and Runtime (min),
  with the page's existing range picker, using the existing metrics API.
- If no UPS-MIB reading has ever arrived: "This UPS does not report standard
  UPS readings (UPS-MIB)." in place of tiles and charts.

The Traffic and Ports sections stay as they are (the port table already says
when a device reports no interfaces).

**Edit details** shows, only when the effective type is UPS, a "UPS alerts"
group: Low battery below (%) and High load at (%), each blank = "Use the
default (N %)".

**Network settings** gains a "UPS" group with the two defaults.

## Testing

- `upsmon`: every start/clear rule above, the 5-minute hold and its reset, the
  output-source fallback, absent readings, restoring from open incidents.
- `snmp`: parsing a GET response into `Readings` (scales, noSuchObject).
- DB: migration CHECK accepts the three UPS rows and rejects a bad condition;
  open/close idempotency; a UPS incident does not affect availability or the
  device-down open/close; reconciliation on type change; settings and
  per-device overrides round-trip.
- Monitor: a fake client driving on battery → back on mains produces one
  incident, one alert and one recovery; the GET failing changes nothing.
- Frontend: type check, lint and build.

## Out of scope

- Vendor MIBs (APC PowerNet, Tripp Lite, Eaton XUPS) — every UPS the user has
  answers UPS-MIB.
- Battery replacement date, self-test results, outlet groups, per-phase
  readings beyond line 1.
- Shutting anything down on low battery.
- A per-UPS alert on/off switch (decision 4).
