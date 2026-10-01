# Network phase 2: rulings and deferred follow-ups

This records the decisions taken while executing `2026-09-30-network-phase2-ports-bandwidth.md`, plus the review findings deliberately left for later. Merged to `dev` on 2026-10-01.

## Additions after sandbox testing (user-approved)

- **Task 16: port roles** (access, uplink, wan; labelled Endpoint, Network link, Internet (WAN)).
  - `GET /sites/:id/traffic` gives north-south traffic (the WAN ports) and east-west traffic: Σ access in/out on switches and routers, minus WAN, averaged over both directions and clamped at 0.
  - The faceplate hover shows live figures, and port-table rows are clickable.
- **Task 17: neighbour links** (`neighbor_device_id` / `neighbor_if_index`), with an automatic reverse link.
  - Stack members are stored in `stack_unit` by inventory, and each member gets its own faceplate, labelled "Switch N".
  - Other changes in this task:
    - the themed portal hover box: Download/Upload by role, Received/Sent on links;
    - the port table scrolls past 24 ports.

## Rulings

### Plan defects fixed
- **049 port-incident CHECK:** `condition IN (...)` let NULL through. Fixed with `condition IS NOT NULL`.
- **051 neighbour CHECK** (as briefed): it broke device deletion, because `ON DELETE SET NULL` only nulls the device column. It was narrowed to `neighbor_device_id IS NULL OR role = 'uplink'`, and a stale index now self-heals on the next patch.
- **Flapping after a restart:** a restored flap ended on the first quiet poll. It now needs a 10-minute quiet window that it observes itself, and the anchor is one-shot on the first poll (two fix rounds).
- **Short windows on old data:** a ≤6 h window older than raw retention returned nothing. It now reads the 5-minute rollup.
- **Nightly cleanup:** the queue is processed in batches of 1000 (Postgres caps bind parameters at 65 535).
- **Retention:** `ApplyRetention` rejects values outside 7–3650 days. A retention change applies the TimescaleDB policy before saving the setting, so a failed change can be retried.
- **Span events:** they keep their start figures at the top level of `detail`; end figures go under `detail.end`.
- **Port incidents close when a port disappears** from inventory or stops being collected. Stopping collection also clears the port's conditions and ends its open spans, in one transaction.
- **The simulator's `numeric` counters** dropped `cumulative=1`, which compounds; growth is now linear.

### Decisions
- **portmon has no dependencies:** it returns device types as plain strings, and the DB CHECK pins them.
- **East-west excludes endpoint devices:** it counts only access ports on switches and routers, so AP and NVR ports are never double-counted.
- **Links stay within a site:**
  - moving a device to another site clears its links in both directions;
  - neighbours are resolved within the same site only;
  - the reverse link never overwrites a WAN port.
- **Stacks:** in a stacked device, unit-0 physical ports (e.g. a management port) stay off the faceplate.
- **Poll timing,** measured on the user's gear: 434 ms on the USW-Pro-48, 522 ms worst case. The default of 16 workers is unchanged.

### Final-review fixes
- Port trackers are pruned when a port stops being collected, and state and span writes are guarded on collection.
- Open port incidents are reconciled each poll: an incident is closed when its port is no longer important, its condition is no longer active, or the port is no longer collected. These closes send no alert.
- Port incident pages name the condition ("The port's stats poll: …") and show "View port".
- Stacked ports read "switch N port M" in alert text.
- Deleting a device drops its cached series ids.
- East-west drops the newest bucket, which is still partial.

### Parked
- **Incident reconciliation** does not run on a poll that has no collected ports, or before a device's HC capability is learned. Changing a port's settings already closes its incidents directly, so this is only a gap if those closes fail.

## Deferred follow-ups

### Worth doing next
- Starring a port that isn't collected promises alerts that can never fire. Either turn collection on with the star, or show a hint.
- Lowering raw retention has no confirmation and no audit entry, and an in-app restore does not re-run `EnsureRetention`.
- Very large sites can exceed the bind-parameter limit in `SiteTraffic` and in `ports=physical` queries. Use `= ANY(?)`.
- Juniper virtual chassis numbering (`ge-0/…`, `ge-1/…`) is not detected as a stack.
- Network settings changes are not audited. Existing settings handlers aren't either.

### Edge cases
- If a link goes down and back up between two polls while the port was already down, `oper_changed_at` is not refreshed.
- On non-HC polls, the 32-bit `ifSpeed` value 4294967295 ("too high to represent") is not treated as unknown. This only affects SNMPv1 links faster than 4.29G.
- Inventory writes full ifOperStatus names, and does so without `oper_changed_at`. This pulls against the stats poll's up/down values, and the time a port was first seen down is not saved.
- HC counter capability is learned once per device and kept until the process restarts.
- Neighbour checks run before the transaction, so they can race with a neighbour being deleted or moved.
- `PATCH {role:access, neighbor_if_index:N}` without a `neighbor_device_id` key returns 400. Only direct API calls can send this; the UI always sends both.
- There is a short window between a device delete committing and its series cache being dropped.
- If two `ApplyRetention` calls succeed at the same time, the retention cache can end up with the older value.
- `Write` validates metric names one point at a time, so valid points before a bad one have already created their series.
- The series-id cache keeps `interface_id` until the process restarts.
- `RetentionDays` returns 0 when no policy exists.
- `SlowLink` has no hysteresis. Link speeds come in fixed steps, so it shouldn't chatter.
- `NetworkMaintenance.RunOnce` has no timeout. The sibling purge loop uses 30 minutes.

### Tests and polish
- **Missing tests:**
  - `LatestMany`, the 1h source, `Sum` on rollups, and the `Instances`/`InterfaceIDs` filters;
  - the ensure loop over several polls, and a complete active-condition incident going through reconciliation's keep path;
  - the out-of-range CHECKs on thresholds and device type.
- **UI:**
  - one shared `busy` flag disables every port row during a single update; PortDetail and ThresholdsForm use separate flags;
  - the `PORT_STATE` badge lookup is repeated inline;
  - editors see the role twice on the port page.
- **Other:**
  - notification timestamps are in local time, while incident times are UTC;
  - the settings seed loop iterates a map, so which error surfaces first is nondeterministic.

## Live upgrade notes (from the final review)
- **TimescaleDB image:** live still runs `main` without TimescaleDB. Promoting `dev` brings phase 0's image switch, and the backend now also refuses the `-oss` (Apache-only) image.
- **Backups:** restores are version-checked, so take a fresh backup right after upgrading. Older ones cannot be restored.
- **Background jobs:** the reused data volume never ran `timescaledb-tune`. An hour after the upgrade, check that the compression, retention and two refresh jobs show successes in `timescaledb_information.job_stats`. If they don't, add worker settings to the compose `command`.
