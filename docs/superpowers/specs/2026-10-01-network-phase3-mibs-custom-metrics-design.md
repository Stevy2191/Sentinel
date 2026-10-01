# Network monitoring phase 3: MIB library and custom metrics

Part of the network monitoring roadmap
(`2026-09-28-network-monitoring-roadmap.md`), building on phase 2
(`2026-09-30-network-phase2-ports-bandwidth-design.md`) and UPS monitoring
(`2026-10-01-ups-monitoring-design.md`). Phase 3 makes vendor specifics a
matter of data, not code: an admin uploads a vendor's MIBs, explores them
against a live device, turns the values that matter into metrics grouped in
profiles, and those metrics are polled, charted and alerted on like the
built-in ones.

## Problem

Sentinel only collects what is hard-coded: interfaces, ports and UPS-MIB.
The user's first real need is Cisco switch health — CPU, memory,
temperatures, fans and power supplies on their 3850s, 4500-Xs and others —
and none of it is visible. Every new vendor reading today needs code. There
is also no MIB support at all: phase 1 moved built-in MIB files to this
phase (phase 1 spec, decision 7).

## Decisions

Taken with the user, 2026-10-01:

1. **First target: Cisco switch health** — CPU, memory, temperature, fans,
   power supplies.
2. **A ready-made "Cisco switch health" starter profile ships with
   Sentinel**, as data (MIBs plus a profile definition), editable and
   copyable. Other vendors' MIBs are uploaded by the user.
3. **Alert rules live in the profile** (one optional rule per metric),
   firing for every device the profile applies to. No per-device overrides
   in this phase.
4. **Approach A: parse once at upload, store the object tree.** A pure-Go
   parser (`github.com/sleepinggenius2/gosmi`, MIT) reads MIBs; the objects
   go into the database; polling uses stored numeric OIDs and never touches
   MIB files. Rejected: parsing on demand (slow browser, late errors) and
   net-snmp tools in the container (breaks roadmap decision 2, native Go).
5. **MIBs, metrics and profiles are admin-only and instance-wide**, like
   credential profiles. Seeing the resulting values follows site access.
6. **The site Power page** (roadmap "Later") is not part of this phase; the
   user wants it built as a phase 4 dashboard widget.

## 1. MIB library

**Upload.** An admin uploads one or more `.mib` / `.my` / `.txt` files or a
`.zip` of them, from a **MIB library** page under Network settings. Limits:
2 MB per file, 20 MB per upload, 500 files per zip; zip entries are read in
memory (no extraction to disk) and paths inside the zip are ignored except
the base name. Files are parsed as untrusted input; one that does not parse
is rejected with the parser's file, line and message, and nothing from that
upload is saved if any file fails to parse (all-or-nothing per upload).

**Dependencies.** A module whose IMPORTS name modules not loaded yet is saved
with status **waiting**, listing the missing modules ("Waiting for:
CISCO-SMI, CISCO-TC"). Every upload re-resolves every waiting module, so a
module becomes **ready** as soon as its dependencies arrive. Only ready
modules contribute objects to the browser and metric picker.

**Storage** (migration 056):

- `mib_modules`: `id`, `name` (unique), `source` (`builtin` | `upload`),
  `file_name`, `size_bytes`, `sha256`, `content` (the file text, so modules
  can be re-parsed after a parser upgrade), `imports` (text[]),
  `missing` (text[]), `status` (`ready` | `waiting`), `uploaded_by`,
  `created_at`, `updated_at` (TIMESTAMPTZ).
- `mib_objects`: `id`, `module_id` (FK, cascade), `name`, `oid` (dotted
  numeric text), `parent_oid`, `kind` (`node` | `scalar` | `table` | `row` |
  `column` | `notification`), `base_type` (e.g. `Integer32`, `Gauge32`,
  `Counter64`, `OctetString`), `enum` (jsonb: number → name), `units`,
  `access`, `description`, `index_columns` (text[], for rows). Unique
  `(module_id, name)`; indexed on `oid` and on `lower(name)`; full-text
  search over name and description.

When two modules define the same OID (versions of one MIB), the browser shows
the object from the most recently updated ready module.

**Built-in modules** are embedded in the binary and (re)loaded at startup,
replacing their rows only when the embedded file's hash changed:

- Standard: SNMPv2-SMI, SNMPv2-TC, SNMPv2-CONF, SNMPv2-MIB, IF-MIB,
  IANAifType-MIB, ENTITY-MIB, ENTITY-SENSOR-MIB, UPS-MIB (and any modules
  these import).
- Cisco health: CISCO-SMI, CISCO-TC, CISCO-PROCESS-MIB,
  CISCO-MEMORY-POOL-MIB, CISCO-ENHANCED-MEMPOOL-MIB, CISCO-ENVMON-MIB,
  CISCO-ENTITY-SENSOR-MIB, CISCO-ENTITY-FRU-CONTROL-MIB (and their imports).

LLDP-MIB and HOST-RESOURCES-MIB, listed in the roadmap's phase 1, are
deferred (LLDP comes with phase 6).

Built-ins cannot be deleted. An uploaded module with a built-in's name
replaces it (the upload wins until deleted, after which the built-in is
restored at the next startup).

**Licensing check (first plan task).** Before bundling the Cisco MIBs, read
the licence terms in Cisco's published MIB repository. If redistribution is
not permitted, ship only the standard MIBs and the profile; the profile's
metrics are defined by numeric OID and work without the Cisco MIBs, and the
UI tells the admin which Cisco files to upload for names and descriptions in
the browser. Everything else in this spec is unchanged either way.

**Delete.** An uploaded module can be deleted unless a ready module imports
it or a metric references one of its objects; the refusal names them.

**Parser concurrency.** `gosmi` keeps global state, so all parsing goes
through one package-level mutex. Parsing runs only at upload and startup.

## 2. OID browser and test-walk

**Browser** (MIB library → Browse):

- A tree by module or by OID path; each node shows name, OID, kind, type,
  units, description, and enumerations spelled out
  (`1 normal · 2 warning · 3 critical …`).
- Search by name, OID prefix or description words across ready modules.
- Tables show their columns and index columns.

**Test-walk.** From a scalar, column or table — or a raw OID typed by hand —
pick a device and press **Test on device**:

- Sentinel reads it live with the device's credential, timeout and retries
  (scalar: GET via `GetEach`; column/table: bulk walk).
- Results: one row per index with each column's raw value and, where the MIB
  defines one, its meaning (`1 → normal`). At most 500 rows ("first 500 of
  more"); 20 s overall limit.
- Allowed for editors and admins of the device's site (it sends SNMP to
  their equipment); readonly users and other sites get 404 as elsewhere.
- Nothing is written to the device and nothing is stored.

**Make a metric from this** opens the metric editor pre-filled from the
selection (OID, kind guessed from the type, units, enumeration).

## 3. Metrics and profiles

**Profile:** `id`, `name` (unique), `description`, `match_prefixes`
(sysObjectID prefixes, text[]), `poll_interval_minutes` (1, 5 or 15; default
1), `builtin` (bool, starter profiles), timestamps. A profile applies to a
device when the device's sysObjectID starts with any prefix (compared on
whole arcs: `1.3.6.1.4.1.9.1` matches `1.3.6.1.4.1.9.1.2066` but not
`1.3.6.1.4.1.9.12`), or when the device is attached by hand; a device can
be detached by hand from a profile that matches it
(`device_profile_overrides`: device, profile, `attach` | `detach`). A device
can use several profiles. **Copy** duplicates a profile with its metrics and
rules (keys get a suffix to stay unique).

**Metric** (belongs to one profile):

| Field | Meaning |
|---|---|
| `name`, `key` | Display name; key matches `^[a-z][a-z0-9_]{2,62}$`, unique across all metrics, and must not be a built-in metric key or start with `if_` or `ups_`. The key is the metric name in the history store. |
| `source` | `scalar` (one OID); `column` (a table column; one reading per row); or `used_free_pct` (two columns of one table, used and free: each row's value is used ÷ (used + free) × 100, for memory pools). Stored as numeric OIDs, plus the MIB object ids when chosen from the library. |
| `kind` | `gauge` (stored as read), `counter` (per-second rate; 32/64-bit wrap and device reboot handled as for ports), `status` (an enumerated state). |
| `units`, `scale` | Free-text units; a multiplier (default 1). |
| `precision_column` | Optional column OID in the same table whose value `p` means "divide by 10^p" (ENTITY-SENSOR style). |
| `filter` | Optional: keep only rows where column X's value is one of a list. |
| `label` | How rows are named: `index`; `column` (a column in the same table); `same_index` (a column in another table indexed the same way, e.g. `entPhysicalName`); `pointer` (column A of this table holds an index into table B, take B's column C). |
| `ok_states` | Status metrics: the values that count as OK, with names from the MIB enumeration (editable). |

Saving requires a preview: the editor runs the metric against a chosen device
(using the test-walk machinery and the same evaluation code as polling) and
shows the final rows — label, value after scale/precision/filter, state name
— and Save is enabled only after a preview returned at least one row.

**Cisco switch health starter profile** (`builtin`, matches
`1.3.6.1.4.1.9.1`, interval 1 minute):

| Metric | Source | Label | Rule |
|---|---|---|---|
| CPU busy (5 min) % | `cpmCPUTotal5minRev` | pointer `cpmCPUTotalPhysicalIndex` → `entPhysicalName` | above 90 for 10 min |
| Memory used % (enhanced) | `used_free_pct` of `cempMemPoolHCUsed` / `cempMemPoolHCFree` | `cempMemPoolName` | above 90 for 15 min |
| Memory used % (classic) | `used_free_pct` of `ciscoMemoryPoolUsed` / `ciscoMemoryPoolFree` | `ciscoMemoryPoolName` | above 90 for 15 min |
| Temperature °C (ENVMON) | `ciscoEnvMonTemperatureStatusValue` | `ciscoEnvMonTemperatureStatusDescr` | above 70 for 5 min |
| Temperature °C (sensors) | `entSensorValue`, filter `entSensorType` = celsius(8), precision `entSensorPrecision` | same_index `entPhysicalName` | above 70 for 5 min |
| Fan status (ENVMON) | `ciscoEnvMonFanState` | `ciscoEnvMonFanStatusDescr` | not OK (OK: normal) |
| Fan status (FRU) | `cefcFanTrayOperStatus` | same_index `entPhysicalName` | not OK (OK: up) |
| Power supply status (ENVMON) | `ciscoEnvMonSupplyState` | `ciscoEnvMonSupplyStatusDescr` | not OK (OK: normal) |
| Power supply status (FRU) | `cefcFRUPowerOperStatus` | same_index `entPhysicalName` | not OK (OK: on) |

Newer platforms answer the enhanced memory pool table and older ones the
classic one; a switch returns nothing for tables it lacks, so one profile
covers 2960, 3850, 4500-X and 9300.

Exact OIDs are taken from the bundled MIBs during implementation and pinned
in a test against the simulator's Cisco data.

## 4. Polling and storage

**When.** A **profile poll** runs after the port stats and UPS polls for each
device that is up, wired into `DevicePoller` the same way as `UPSPoller`. A
profile is due when `poll_interval_minutes` has passed since that device's
last run of it. One device's profile poll has a 30 s budget; work not done
in time waits for the next run.

**What is read.** For each due profile, Sentinel gathers the distinct OIDs
needed: value columns and scalars every run; filter, label (including
pointer targets) and precision columns are **cached per device and refreshed
with the 15-minute inventory** (or on the first run). Each column is
bulk-walked once per run however many metrics use it; scalars are fetched in
one `GetEach` request. A walk that fails (timeout) skips the metrics that
need it for this run; an empty result means "not supported here" and yields
nothing.

**Evaluation** is a pure package (`internal/custommetric`): given the walked
columns and a metric definition, return rows of (instance, label, value,
state). It applies filter, precision, scale, labels (all four modes) and, for
counters, rates against the previous reading (kept in memory per series, as
for ports; the first run after a restart produces no rate).

**Storage.** Values go to the existing metrics store under (device, metric
key, instance = row index, e.g. `1001` or `1.2`; `""` for scalars). Migration
056 adds `label` to `metrics.series`; the label is updated when it changes
without affecting the series identity. The metrics API's known-metric check
becomes built-ins plus custom keys (cached, reloaded when metrics change),
so charts and the query API work for custom metrics unchanged.

**Errors are visible.** The last run per (device, profile) — time, success,
and the error text when it failed — is stored in `device_profile_runs` and
shown on the device page.

**Deleting a metric** deletes its history (confirmation names how many
devices have data), using the phase 2 cleanup queue; deleting a profile does
the same for all its metrics. Built-in profiles cannot be deleted (copy and
edit instead), but their metrics and rules can be edited.

**Device page: Health section.** Shown when the device has profile metrics:
per metric, its rows with label, latest value (units) or state chip
(green OK / red problem), and a sparkline; clicking opens the full chart with
the range picker. The section also shows each profile's last run status.
Manual attach/detach of profiles lives in Edit details.

## 5. Alert rules and incidents

**Rule** (optional, one per metric):

- Numeric: `above` or `below` a value, held for N minutes (default 5) before
  starting; clears on the first poll back inside the limit.
- Status: `not_ok` — starts on the first poll that sees a non-OK state
  (optional hold N minutes); clears when the row is OK again.
- Evaluated per row: each (device, metric, instance) is its own condition.
- A row with no reading this run keeps its state.

Rule evaluation is a pure function (`internal/custommetric`), tested without
a database, with the hold clock kept in memory per row as for UPS high load.

**Incidents** reuse device-level condition incidents (UPS monitoring).
Migration 056 adds `metric_key` and `metric_instance` to `incidents`, adds
the condition value `metric` to the device-condition CHECK branch (requiring
both new columns when `condition = 'metric'`, and both NULL otherwise), and
a unique open index on `(device_id, metric_key, metric_instance)` for
`condition = 'metric'`.

- Subject: "core-3850 · Fan status (FRU): Switch 1 - Fan 2".
- Start messages: "core-3850 Switch 1 - Fan 2 is critical (was up)";
  "core-3850 CPU busy (5 min), Switch 1: 94 % (above 90 % for 10 min)".
  Recovery: "… is up again" / "… back to 41 %".
- Never counted as downtime; follow the device's notification channels;
  closed on pause (as UPS incidents).
- **Open incidents are the truth each poll** (the UPS-fix pattern): the
  active set before evaluation is read from open metric incidents; an open
  incident whose row is now inside its rule closes with a recovery notice
  (one was owed if an earlier close failed); one whose rule was removed or
  disabled, whose metric was deleted, or whose profile no longer applies to
  the device closes quietly with a note saying why.

**Permissions and audit.** MIB upload/delete and profile, metric and rule
create/update/delete are admin-only and audited with before/after.
Test-walk and metric preview need editor access to the device's site. The
Health section and custom metric charts follow normal site access.

## Testing

- Parser: every built-in module loads with no missing imports; a malformed
  file is rejected with its line; a waiting module becomes ready when its
  dependency is uploaded; zip upload; size limits; delete refusals.
- `custommetric`: filter, precision, scale, `used_free_pct` (including
  used + free = 0, which yields no value), each label mode (including a
  pointer whose target row is missing), counter rate and wrap, status
  mapping, and every rule start/clear/hold case.
- Simulator: `deploy/snmpsim/data/cisco.snmprec` gains CPU, ENVMON,
  ENTITY-SENSOR (mixed sensor types, precision 1), FRU fan/power and
  ENTITY-MIB name rows; an end-to-end test runs browse → test-walk → profile
  match → poll → history → rule → incident → recovery.
- DB: migration 056 constraints (metric incidents need key and instance;
  other conditions must leave them NULL), open-incident uniqueness,
  availability unaffected, open incidents as truth, metric delete queues its
  series.
- API: admin-only routes, test-walk site access, upload limits.
- Frontend: type check, lint and build.

## Out of scope

- General derived metrics (arbitrary expressions over several values);
  `used_free_pct` covers the memory case.
- Per-device rule overrides.
- SNMP traps; alerts come from polling only.
- LLDP-MIB and HOST-RESOURCES-MIB built-ins.
- Writing to devices (SNMP stays read-only).
- The site Power page (phase 4 widget).
