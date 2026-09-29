# Network monitoring phase 1: SNMP foundation

Part of the network monitoring roadmap
(`2026-09-28-network-monitoring-roadmap.md`), building on phase 0
(`2026-09-28-network-phase0-groundwork-design.md`). Phase 1 makes Sentinel
talk SNMP: credential profiles, devices that belong to sites, discovery by
subnet scan, identity and interface inventory, reachability polling, and
down/recovery handled as **incidents**, the same as monitors.

It collects no traffic or error counters. Those, and everything built on
them, are phase 2.

## Problem

Sites exist (phase 0) but hold nothing. There is no way to tell Sentinel about
a switch, no SNMP code at all, and no way to learn that a switch at a remote
site has stopped answering. Incidents, the one place Sentinel records outages,
can only belong to a monitor.

## Decisions

Taken with the user during design, 2026-09-29:

1. **SNMP v1, v2c and v3.** Switches are mostly v2c; v3 must be available;
   v1 is needed for older point-to-point radios. v1 has no GETBULK and no
   64-bit counters, which the poller (here) and phase 2 must respect.
2. **An unreachable device opens an incident**, like a monitor, rather than
   only sending notifications as server agents do. Incidents gain a device
   subject. This is the larger option, chosen deliberately: outages of every
   kind in one place, with comments and root cause, reportable later.
3. **Devices are their own subsystem** (not a monitor type): own tables, own
   poller, permissions from sites.
4. **Adding devices**: manually, or by a per-site subnet scan.
5. **Every device belongs to exactly one site.** Admins manage credential
   profiles; users with editable access to a site add devices there by
   choosing a profile, and never see secrets.
6. **Polling defaults**: reachability every 60 s, down after 3 consecutive
   failures (as monitors), interface inventory every 15 min and on add.
7. **Built-in MIB files move to phase 3.** Phase 1 reads a dozen standard,
   fixed OIDs and needs no MIB parsing.
8. **Testing against an SNMP simulator and one real switch**, plus a real
   database test harness introduced now rather than in phase 2.

Done separately, before this phase: `GET /incidents` and `GET /incidents/:id`
now enforce monitor access (commit `ef520d5`, on `main` and `dev`). Phase 1
extends that filter to device incidents.

## Scope

In: sections 1–6 below.

Out, each to a later phase:

- Traffic, error and discard counters, bandwidth charts, port status change
  events, "important" ports (phase 2).
- Custom OIDs, MIB upload, built-in MIB files (phase 3).
- Device incidents in incident/uptime **reports**: reports stay monitor-only
  until the metric report type (phase 5).
- LLDP neighbours (phase 6). UniFi controller data (phase 7).

## Design

### 1. Incidents and notifications gain a device subject

**Migration `047_device_incidents.sql`** (after 046 creates `devices`):

```sql
ALTER TABLE incidents ALTER COLUMN monitor_id DROP NOT NULL;
ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS device_id UUID REFERENCES devices (id) ON DELETE CASCADE;
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_subject_check;
ALTER TABLE incidents
    ADD CONSTRAINT incidents_subject_check CHECK (num_nonnulls(monitor_id, device_id) = 1);
CREATE INDEX IF NOT EXISTS idx_incidents_device_start
    ON incidents (device_id, start_time DESC) WHERE device_id IS NOT NULL;

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS device_id UUID REFERENCES devices (id) ON DELETE CASCADE;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_subject_check;
ALTER TABLE notifications
    ADD CONSTRAINT notifications_subject_check
    CHECK (num_nonnulls(monitor_id, agent_id, device_id) = 1);
CREATE INDEX IF NOT EXISTS idx_notifications_device
    ON notifications (device_id, created_at DESC) WHERE device_id IS NOT NULL;
```

This follows migration 040, which made notifications "monitor or agent".
Deleting a device deletes its incidents and notifications, as deleting a
monitor does.

`models.Incident.MonitorID` becomes `*uuid.UUID`; `DeviceID *uuid.UUID` is
added. Every use of `MonitorID` is updated for the pointer.

**Queries that need no change, by construction.** Every uptime, downtime,
count and active-incident query filters `monitor_id = ?`
(`IncidentService.GetIncidents`, `GetOverlappingIncidents`,
`GetActiveIncident`, `GetIncidentDuration`, `GetIncidentCount`, the report
aggregator). A device incident has `monitor_id` NULL, so none of them can see
it: a switch outage can never count against a monitor's uptime. This is
pinned by database tests (section 6), not assumed.

**Queries that change:**

- **Incident list and detail** (`ListIncidents`, `GetIncidentByID`) join
  `monitors` with an inner join, so device incidents would vanish. Both move
  to `LEFT JOIN monitors` and `LEFT JOIN devices` (+ `sites`), and the row
  gains `subject_type` (`monitor` | `device`), `subject_name`, `subject_target`
  (URL or host) and, for devices, `site_id` / `site_name`. The existing
  `monitor_name`, `monitor_url`, `monitor_type` fields stay for monitor rows
  so the current frontend keeps working unchanged until it is updated.
- **Access filter** (from `ef520d5`) becomes: admins see everything; members
  see monitor incidents for monitors they own or were shared (unchanged), and
  device incidents for sites shared with them:
  `(i.monitor_id IS NOT NULL AND (<monitor rule>)) OR (i.device_id IS NOT NULL
  AND d.site_id IN (SELECT site_id FROM site_sharing WHERE
  shared_with_user_id = ?))`.
- **Detail access** checks through the subject: `CanUserViewMonitor` for a
  monitor incident, `SiteAccess >= readonly` for a device incident; either
  failing is a 404.
- **Editing an incident and its comments** check through the subject:
  monitor edit rights (unchanged) or `SiteAccess >= editable` on the device's
  site.
- **Filters**: `?device_id=` restricts to one device; `?subject=monitor|device`
  restricts by kind. Search matches the subject's name.
- **Checks during an incident** (`ChecksDuringIncident`) apply to monitors
  only; a device incident's detail returns no checks.
- **Webhook payload and notification text** carry the device (see section 3).

**Opening and closing** a device incident: `IncidentService.OpenDeviceIncident`
(if none active) and `CloseDeviceIncident`, the device counterparts of the
existing monitor functions, used by the poller.

**Retention** is unchanged: it deletes by age, regardless of subject.

### 2. Data model

**Migration `046_snmp_devices.sql`:**

```sql
CREATE TABLE IF NOT EXISTS snmp_credentials (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              VARCHAR(255) NOT NULL,
    site_id           UUID REFERENCES sites (id) ON DELETE RESTRICT,  -- NULL = global
    version           VARCHAR(3) NOT NULL CHECK (version IN ('1', '2c', '3')),
    community         TEXT,          -- encrypted; v1/v2c
    username          TEXT,          -- v3
    auth_protocol     VARCHAR(10) NOT NULL DEFAULT 'none',
    auth_password     TEXT,          -- encrypted; v3
    priv_protocol     VARCHAR(10) NOT NULL DEFAULT 'none',
    priv_password     TEXT,          -- encrypted; v3
    created_by        UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (auth_protocol IN ('none','MD5','SHA','SHA224','SHA256','SHA384','SHA512')),
    CHECK (priv_protocol IN ('none','DES','AES','AES192','AES256'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_snmp_credentials_name ON snmp_credentials (lower(name));

CREATE TABLE IF NOT EXISTS devices (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id              UUID NOT NULL REFERENCES sites (id) ON DELETE RESTRICT,
    credential_id        UUID NOT NULL REFERENCES snmp_credentials (id) ON DELETE RESTRICT,
    name                 VARCHAR(255) NOT NULL,
    host                 VARCHAR(255) NOT NULL,
    port                 INTEGER NOT NULL DEFAULT 161 CHECK (port BETWEEN 1 AND 65535),
    enabled              BOOLEAN NOT NULL DEFAULT true,
    poll_interval        INTEGER NOT NULL DEFAULT 60 CHECK (poll_interval BETWEEN 10 AND 3600),
    timeout_ms           INTEGER NOT NULL DEFAULT 3000 CHECK (timeout_ms BETWEEN 200 AND 30000),
    retries              INTEGER NOT NULL DEFAULT 1 CHECK (retries BETWEEN 0 AND 5),
    notify_channels      JSONB,
    status               VARCHAR(20) NOT NULL DEFAULT 'pending',
    status_detail        TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_polled_at       TIMESTAMPTZ,
    last_seen_at         TIMESTAMPTZ,
    last_inventory_at    TIMESTAMPTZ,
    sys_name             TEXT,
    sys_descr            TEXT,
    sys_object_id        TEXT,
    sys_location         TEXT,
    sys_contact          TEXT,
    sys_uptime_seconds   BIGINT,
    vendor               TEXT,
    model                TEXT,
    serial               TEXT,
    created_by           UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (status IN ('pending', 'up', 'down', 'paused', 'error'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_site_host ON devices (site_id, lower(host));
CREATE INDEX IF NOT EXISTS idx_devices_site ON devices (site_id);

CREATE TABLE IF NOT EXISTS device_interfaces (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id     UUID NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    if_index      INTEGER NOT NULL,
    name          TEXT,     -- ifName, falling back to ifDescr
    descr         TEXT,     -- ifDescr
    alias         TEXT,     -- ifAlias: the port description set on the switch
    if_type       INTEGER,
    speed_bps     BIGINT,
    mac           TEXT,
    admin_status  VARCHAR(20),
    oper_status   VARCHAR(20),
    last_change_seconds BIGINT,  -- ifLastChange, as seconds of sysUpTime
    present       BOOLEAN NOT NULL DEFAULT true,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (device_id, if_index)
);
```

Notes:

- **Secrets** (`community`, `auth_password`, `priv_password`) are stored
  encrypted with `cryptutil.Encrypt` and never returned by any API; responses
  carry `has_community`, `has_auth_password`, `has_priv_password`. On update,
  an omitted or empty secret keeps the stored one.
- **Credential validation per version**: v1/v2c need `community`; v3 needs
  `username`; `auth_protocol` other than `none` needs `auth_password`
  (at least 8 characters, the RFC 3414 minimum); `priv_protocol` other than
  `none` needs `auth_protocol` other than `none` (no privacy without
  authentication). Fields that do not belong to the version are cleared.
- A **site-scoped** credential is offered only for that site's devices; a
  device's credential must be global or scoped to the device's site.
- **Host** is an IP address or a DNS name. Hosts are unique per site
  (case-insensitive), not globally: overlapping private ranges across sites
  are normal for an MSP.
- **`status`**: `pending` until the first poll; `up`; `down` (3 consecutive
  failures, incident open); `paused` (disabled); `error` (a configuration
  problem such as a target blocked by network policy, or an unresolvable
  host: no incident). `status_detail` carries the last error text.
- **Interfaces** are keyed by `(device_id, if_index)`. One not seen in the
  latest inventory walk is set `present = false`, never deleted, so phase 2's
  history for it is not orphaned; it goes with its device. `speed_bps` is
  `ifHighSpeed × 10⁶` when the device reports a non-zero ifHighSpeed, else
  `ifSpeed`. `admin_status` / `oper_status` are the IF-MIB names (`up`,
  `down`, `testing`, `unknown`, `dormant`, `notPresent`, `lowerLayerDown`).
- **Phase 0 hooks**: `devices` and `snmp_credentials` join
  `siteContentTables`, so a site cannot be deleted while it has devices or
  site-scoped credentials. Everything here lives in `public` and is a table,
  per the phase 0 backup rule.

**Vendor detection** (`snmp.VendorFor(sysObjectID)`): the enterprise number
after `1.3.6.1.4.1.` is looked up in a built-in table: 9 Cisco, 11 HP,
674 Dell, 2636 Juniper, 4413 Ubiquiti (EdgeSwitch), 4526 Netgear,
6486 Alcatel-Lucent, 11863 TP-Link, 12356 Fortinet, 14823 Aruba,
14988 MikroTik, 17713 Cambium, 25053 Ruckus, 25461 Palo Alto, 29671 Meraki,
41112 Ubiquiti. Unknown numbers read `Unknown (enterprise N)`. **Model** and
**serial** come from ENTITY-MIB (`entPhysicalModelName`,
`entPhysicalSerialNum`) of the first entry whose `entPhysicalClass` is
chassis (3), falling back to the first entry with a non-empty model name.

### 3. Polling and discovery

**`internal/snmp`** wraps `github.com/gosnmp/gosnmp`:

```go
type Target struct {
    Host       string
    Port       uint16
    Credential Credential // decrypted, in memory only
    Timeout    time.Duration
    Retries    int
}

type Client interface {
    Get(ctx context.Context, t Target, oids []string) ([]PDU, error)
    Walk(ctx context.Context, t Target, rootOID string) ([]PDU, error)
}
```

`Walk` uses GETBULK for v2c/v3 and GETNEXT for v1. All interpretation is pure
and tested without a network: `ParseSystem`, `ParseInterfaces` (merging the
`ifTable` and `ifXTable` walks by index), `ParseEntity`, `VendorFor`.

OIDs read in phase 1: the system group (`sysDescr`, `sysObjectID`,
`sysUpTime`, `sysContact`, `sysName`, `sysLocation`), `ifTable` (`ifIndex`,
`ifDescr`, `ifType`, `ifSpeed`, `ifPhysAddress`, `ifAdminStatus`,
`ifOperStatus`, `ifLastChange`), `ifXTable` (`ifName`, `ifHighSpeed`,
`ifAlias`), and ENTITY-MIB (`entPhysicalClass`, `entPhysicalModelName`,
`entPhysicalSerialNum`). A device without ifXTable (v1, some radios) falls
back to `ifDescr` for the name and `ifSpeed` for speed; a device without
ENTITY-MIB has no model or serial.

**Reachability poller** (`services.DevicePoller`):

- A scheduler ticks every 5 s, selects enabled devices whose
  `last_polled_at + poll_interval <= now` (or never polled), and hands them to
  a bounded worker pool. A device already in flight is skipped.
- Pool size is the `snmp_poll_workers` setting, default 16, read at startup.
- A poll resolves the host, checks it with `netguard.IsBlocked` when private
  targets are disallowed, and sends one `GET sysUpTime.0`.
- The outcome goes through a **pure state machine**,
  `NextDeviceState(current, failures, result) → (state, failures, actions)`,
  where actions are `open_incident`, `close_incident`, `notify_down`,
  `notify_recovered`:
  - success: failures 0, status `up`; from `down` → close incident, notify
    recovered.
  - timeout / no response / auth failure: failures + 1; reaching 3 from any
    status but `down` → status `down`, open incident, notify down.
  - blocked by policy or unresolvable host: status `error`, detail set,
    failures unchanged, no incident.
  - disabled: status `paused`, no polling, no alerts. An open incident on a
    device that is paused is closed, with a note that monitoring was paused.
- After each poll the device's `last_polled_at`, and on success
  `last_seen_at` and `sys_uptime_seconds`, are stored.

**Inventory refresh**: on add, every 15 minutes, and on demand (`POST
/devices/:id/refresh`). Reads the system group, ENTITY-MIB and the interface
tables; updates the device's identity fields and upserts interfaces by index.
An inventory failure is logged in `status_detail` but never changes
up/down: reachability alone decides that. Inventory runs on the same worker
pool, after the reachability poll that finds it due.

**Subnet scan** (per site, editable access):

- Input: a CIDR (at most /22, 1024 addresses, as the existing discovery) and
  the credential profiles to try (default: all available to the site).
- Each address is tried with each profile in order, stopping at the first
  that answers a `GET` of `sysName`, `sysDescr`, `sysObjectID`, with a 1 s
  timeout and no retries. Concurrency 64.
- It runs as an **in-memory background job**: `POST` returns a job id; `GET`
  returns progress (`done` / `total`) and results so far. Jobs expire an hour
  after they finish; a restart loses unfinished scans, which can be rerun.
  One scan per site at a time.
- Results: address, sysName, vendor, model (from sysDescr only; no ENTITY
  walk during a scan), the profile that answered, and whether the address is
  already a device in this site.
- `POST /sites/:id/devices` adds the selected results in one request, each
  named from its sysName, with default polling settings.

**Notifications** use the existing `NotificationManager`.
`NotificationMessage` gains `DeviceID *uuid.UUID`, like `AgentID`. Down text:
"*core-sw-1* at Warehouse (10.20.0.2) has stopped answering SNMP.
Last answered 14:02 UTC." Recovery: "*core-sw-1* at Warehouse is answering
again after 12 minutes." Channels come from the device's `notify_channels`;
none selected means no notification (the incident still opens). The webhook
payload carries `device_id`, `device_name`, `site_name` and `host`.

### 4. API

All under `/api/v1`, gated by `SiteAccess` (phase 0). A device on a site the
caller cannot see is a 404.

| Method | Path | Who |
|---|---|---|
| GET | `/snmp-credentials` | admin |
| POST / PUT / DELETE | `/snmp-credentials[/:id]` | admin; DELETE is 409 while devices use it |
| GET | `/sites/:id/credentials` | editable; id, name, version, scope only |
| GET | `/devices?site_id=&status=` | any user; filtered to sites they can see |
| POST | `/devices` | editable on the target site |
| GET | `/devices/:id` | readonly on its site |
| PUT | `/devices/:id` | editable on its site (and on the new site, if moved) |
| DELETE | `/devices/:id` | editable on its site |
| GET | `/devices/:id/interfaces?include_absent=` | readonly |
| POST | `/devices/:id/refresh` | editable |
| POST | `/devices/test` | editable on some site; rate limited |
| POST | `/sites/:id/scans` | editable |
| GET | `/sites/:id/scans/:job` | editable |
| POST | `/sites/:id/devices` | editable; bulk add from scan results |
| GET | `/incidents?device_id=&subject=` | existing route, extended |

- `POST /devices/test` takes `{site_id, host, port, credential_id}` and
  returns the system group, vendor and model, or the error. It sends SNMP to
  an address the caller chooses, so it is rate limited (10 per minute per
  user, `NewRateLimiter(...).Middleware("snmp-test", ByUser)`) and applies
  the same `netguard` rule as polling.
- Credential create/update/delete and device create/update/delete are
  written to the audit log (`models.ResourceSNMPCredential`,
  `models.ResourceDevice`). Secrets are never written to the audit log.

### 5. Screens

Following the design system; the Network sidebar gains **Devices** and, for
admins, **Credentials**.

- **Devices** (`/network/devices`): every device in the sites the user can
  see: status, name, site, host, vendor/model, last seen, 30-day
  availability (from device incidents). Filters: site, status, vendor;
  search. Add device.
- **Device** (`/network/devices/:id`): header (status, site, host,
  vendor/model/serial, location, contact, up since, last seen); actions
  (Refresh now, Edit, Pause/Resume, Delete, the last warning that incident
  history is deleted too); **Interfaces** table (name, alias, admin and oper
  status coloured, speed, type, MAC, last change; absent ports hidden, a
  toggle for admin-down); **Incidents** for the device.
- **Site** (`/network/sites/:id`): the phase 0 placeholder becomes the site's
  device table, with Add device and Scan subnet for editable users.
- **Add/Edit device**: site, host, port, credential (names only, filtered to
  the site), polling options, notification channels (the existing channel
  picker). **Test connection** reads the identity before saving and fills the
  name from sysName.
- **Scan subnet**: CIDR, profile checkboxes, progress, results with
  checkboxes, Add selected.
- **Credentials** (`/network/credentials`, admin): list (name, version,
  scope, used by N devices); form showing only the current version's fields;
  secret fields write-only ("set" / blank to keep).
- **Incidents page**: device rows show a switch icon and the device and site
  name, linking to the device; the type filter gains "Network device".
- **Overview card**: Network Monitoring leads with the device count and a
  subtitle of "N down" or "all up"; with no devices yet it keeps showing the
  site count.

### 6. Testing

**Database test harness** (new, pulled forward from phase 2):

- `runMigrations` moves from `cmd/sentinel/main.go` to
  `internal/database.RunMigrations` (no behaviour change) so tests can build
  a real schema.
- Integration tests run only when `SENTINEL_TEST_DATABASE_URL` is set, and
  skip otherwise, so plain `go test ./...` stays database-free.
- `make test-db` starts a throwaway `timescale/timescaledb:2.30.1-pg16`
  container (with the same preload flags as compose) on a random port, runs
  the integration tests against it, and removes it.
- Pinned against real SQL:
  - monitor uptime, downtime, counts and active-incident lookups ignore
    device incidents;
  - the database rejects an incident or notification with both or neither
    subject;
  - incident list/detail filter device incidents by site sharing and monitor
    incidents by monitor sharing (a member sees exactly their own);
  - deleting a device deletes its incidents, notifications and interfaces;
  - a site with devices or site-scoped credentials cannot be deleted;
  - credential secrets are stored encrypted, and no credential API response
    contains a secret.

**Unit tests** (no network, no database): the parsers against recorded
responses (system, interfaces including ifHighSpeed vs ifSpeed and the
no-ifXTable fallback, entity, vendor); `NextDeviceState` across every
transition; credential validation per version and the mapping to gosnmp
protocol constants; the scheduler (due selection, no double polling) and scan
jobs (first profile that answers, expiry, one per site) with a fake client;
handlers with fakes (readonly cannot add devices, editable cannot manage
credentials, 404 for devices on other sites, `/devices/test` rate limited).

**SNMP simulator** (`snmpsim`, a sandbox-only container): serves an
EdgeSwitch (52 ports), a Cisco, a v1 point-to-point radio, and v3 with
auth-only and auth+priv, each on its own port. Integration tests gated on
`SENTINEL_TEST_SNMPSIM` drive the real gosnmp client against it: v1 walk
without bulk, v2c bulk walk, v3 with each protocol, wrong community, timeout.
The user's real switch is recorded once (`snmprec`) and replayed as one of the
simulated devices, with serial numbers and MAC addresses scrubbed before the
recording is committed.

**Sandbox checklist** (with the user):

1. Add the real switch with Test connection: name, vendor, model and port
   list (including port descriptions) match the switch.
2. Scan its subnet: it is found and marked as already added.
3. Stop the simulated switch: after 3 failed polls it is down, an incident is
   open, a notification arrives. Start it: the incident closes, a recovery
   notification arrives.
4. A non-admin with read-only access to a site sees its devices but cannot add
   one; cannot see another site's devices or incidents; cannot open
   Credentials.
5. Load: 30 simulated devices × 52 ports at default settings; every 60 s
   cycle completes on time, with poll timings logged. This is the evidence for
   the roadmap's poll-load risk before phase 2 adds counters.

## Risks

- **Changing incidents** touches code that computes customer-visible numbers.
  Mitigated by the "no change by construction" property (`monitor_id = ?`)
  and the database tests that pin it, plus keeping the existing monitor
  fields in the list response so the frontend does not have to change in
  lockstep.
- **Poll load over VPN.** Measured in checklist step 5 before phase 2.
- **SNMP from the server to arbitrary addresses.** Polling and the test
  endpoint are limited to editable users, respect `netguard`, and the test
  endpoint is rate limited.
- **gosnmp v3 and old devices.** Some old radios only speak v1 or DES/MD5;
  both are supported and covered by the simulator.
