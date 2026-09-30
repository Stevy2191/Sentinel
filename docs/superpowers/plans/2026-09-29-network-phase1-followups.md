# Network phase 1 — rulings and deferred follow-ups

Record of the decisions made while executing
`2026-09-29-network-phase1-snmp-foundation.md` and the review findings that
were deliberately left for later. Merged to `dev` at 0acdc6f (2026-09-30).

## Rulings

Decisions taken during execution where the plan was wrong, ambiguous, or a
reviewer's finding was judged not to need a change.

### Plan defects fixed

- **test-db readiness** — `scripts/test-db.sh` waits for "ready to accept
  connections" twice in the container log instead of a `pg_isready` loop, which
  raced the image's temporary initdb server and failed every time. *Cost if
  wrong:* a log-based wait that breaks if the image's init sequence changes.
- **`default:true` on bools** — `Device.Enabled` and `DeviceInterface.Present`
  lost `default:true` from their gorm tags, because GORM drops a `false` on
  insert and paused-on-create would silently be enabled. The DB default stays
  for raw SQL. DB test pins it.
- **`resolveHost` collision** — renamed `resolveDeviceHost` (clashed with the
  existing `dns_resolve.go` helper).
- **ScanModal lint** — results memoised to satisfy `react-hooks/exhaustive-deps`.
- **snmpsim 1.2.2** — needs pysmi + cryptography; an empty v3 context maps to
  `self.snmprec`. Documented in `deploy/snmpsim/README.md`.
- **Attribution** — commit trailers name the model that actually wrote the
  commit.

### Poller behaviour

- A failed `OpenDeviceIncident` is retried on every poll while the device is
  down; the down notice is sent only when an incident was actually created.
  Recovery duration falls back to `now - LastSeenAt` when no incident exists.
- A poll that finishes after the device was paused is discarded
  (`ErrDeviceNotEnabled` from `SaveReachability`).
- Only a missing or undecryptable credential (`ErrCredentialUnusable`) marks
  the device blocked; other errors skip the poll without saving. *Cost if
  wrong:* an unwrapped credential error retries silently instead of showing
  "error".
- A blocked poll resets the failure count to 0 — "down" means 3 *consecutive*
  failures. *Cost if wrong:* a device leaving "error" needs 3 fresh failures to
  alert.
- The threshold-crossing down notice is gated on the incident being opened, so
  one crossing sends one alert.

### Incidents and access

- Device incidents open and close automatically: `PATCH status` on one returns
  400. Comments are still allowed. This also removes the double-open and
  "reopened while up stays open forever" cases. *Cost if wrong:* operators
  can't hand-close a device incident; pausing the device does it.
- On the Devices page, **Add** is admin-only; editors add devices from the site
  page, which is gated by site access.
- Scan modal keeps a local "added" set, clears the selection, and calls
  `onAdded` when anything was added (not only on zero failures).

### Findings where the code stands

- **SNMP v1 identity batch** — `Identify` fetches the six system OIDs in one
  GET, which is all-or-nothing on v1. MIB-II system is mandatory, and
  reachability only GETs `sysUpTime`, so there are no false down alerts. *Cost:*
  an odd v1 agent lacks identity/interfaces until a per-OID fallback exists.
- **v3 priv without auth** — rejected by `NormalizeCredentialInput`, the only
  writer. *Cost:* opaque timeouts for a hand-edited DB row.
- **`useDeviceInterfaces` doesn't reset on id/toggle change** — the only
  consumer never changes id within a mount. *Cost:* momentary stale rows.
- **`useDevice`/`useDeviceInterfaces` have no error state** — a first-load 500
  renders "Device not found"; the 30 s refresh self-heals. Follow-up: add an
  error state.
- **Hidden password sent after switching protocol to none** — goes only to
  Sentinel's own API, which discards it.
- **"Untick all profiles scans with all"** — not reachable; Scan is disabled
  with nothing chosen.

## Deferred follow-ups

### Worth doing next (user-visible)

- Transient ifXTable failure blanks port aliases/names and falls back to 32-bit
  speed for up to 15 min — keep stored values when the ifX walk fails (phase 2).
- Server-agent alerts in Discord/Slack/Telegram/email/ntfy link to
  `/monitors/<zero uuid>` (pre-existing; device alerts were fixed via
  `ViewPath`).
- Device `PUT` is full-replace: an omitted `enabled` resumes, an omitted
  `notify_channels` becomes null (all channels), and an empty name can't
  restore auto-naming. The frontend must send full objects.
- Retries can't be set to 0 (spec allows 0–5).
- Changing a device's target in the same PUT as resume doesn't reset
  `last_inventory_at`; a target change keeps the old status, failures and open
  incident.
- Refresh on a paused device answers 202 "queued" but does nothing.
- Scanning a private range while private targets are blocked returns silently
  empty — add a note to the job.
- No settings UI or seed for `snmp_poll_workers` (edit the DB row).
- Notification history: view-only members see a Retry button that 403s.
- `useDevice`/`useDeviceInterfaces` error state (see rulings).

### Correctness edge cases

- Pause/poll race can open an incident on a just-paused device (millisecond
  window).
- An incident opened by the retry path starts at the retry poll, not when the
  device first crossed to down.
- Threshold crossing that finds an incident already open suppresses the down
  alert.
- Recovery message says "less than a minute" when `LastSeenAt` is nil and there
  is no incident.
- `?subject=monitor&device_id=X` returns nothing instead of 400.
- Credential `Update` isn't transactional (site-mismatch count vs a concurrent
  device create); a nonexistent `site_id` on update with devices → 409 instead
  of 400.
- Every device FK violation maps to `ErrSiteNotFound` (a credential FK race is
  reported as a site); credential `created_by` FK violation likewise.
- `isInternal` classifies errors by `Unwrap` presence — a future unwrapped DB
  error would surface as 400 with raw Postgres text. Prefer a typed
  `ValidationError`.
- `ReadInventory` swallows ifXTable/ENTITY walk errors by design, so a transient
  failure looks like an unsupported table.
- `closeDeviceIncident` logs before the tx commits; retyping the literal old
  host as a custom name is dropped.
- History/retry trust the JWT `is_admin` claim (project-wide stale-claim
  follow-up); retry leaks monitor-lookup error text.
- sysDescr parsing: bare "Ubiquiti UniFi" and double-space edge cases.
- Credential profile names are unique globally, even for site-scoped profiles.

### Robustness and performance

- Poller shutdown drains queued jobs with a cancelled ctx (log noise); `Start`
  has no WaitGroup; `PollOnce` has no recover.
- `SaveInventoryError`'s error is discarded.
- Notification history resolves device names via `DeviceService.List` (4 calls
  per page) — use an id-list lookup.
- Scan uses a fixed 64-worker pool even for tiny scans; `credential_ids` not
  deduped.
- `useSiteScan` poll requests can overlap (>1 s); `start()` isn't guarded
  against a double call.
- `snmp.Get/Walk` defer `g.Conn.Close()` instead of `g.Close()`.
- `uint16(d.Port)` relies on the 1–65535 validation (currently satisfied).
- Monitor `GetActiveIncident` still logs "record not found".

### UI polish

- ScanModal: brief "No profiles available" flash while options load; success and
  failure messages share a style; closing mid-scan can't resume watching the
  job.
- Credential modal: no focus trap/Escape; icon buttons use `title` not
  `aria-label` (existing convention); redundant version check on
  `has_community`.
- Clearing a number input submits 0.
- Long unwrapped icon+name line in the Incidents device cell; Overview subtitle
  can briefly read "· — sites".
- Secret rotation isn't visible in the audit log (add `secrets_changed`).

### Tests and docs

- Handler tests: readonly-user 403 on device incident PATCH/comment;
  `ErrSiteNotFound`→404; scan `SiteAccessNone`→404; `ErrNotificationNotRetryable`
  →400; device PUT permitted move and 409; `ErrCredentialDecryptFailed`
  classification; credential options body, explicit `""` secret, v3 auth-only
  clears priv, 400/500 classification.
- Poller tests: close notes, failed `SaveReachability` skips actions, full queue
  releases inFlight, Start shutdown, `humanDuration` hours/days; fakes unlocked.
- Availability with an incident before the window / an open incident;
  simultaneous name+host change.
- Notification history: negative monitor case (foreign/shared rows),
  status-filter sub-case; handler test that the viewer reaches the device
  namer; test-send test via real route registration.
- `TestToPDU` lacks an IPAddress case; snmp_test "port 0 given" passes -1.
- `testdb.NewUser` `ReplaceAll` on `id[:8]` is a no-op.
- Stale comments: "monitor" meaning subject (incident_comment_handler.go,
  Get/ListIncidentsHandler, GetIncidentByID, models.Incident); history /
  ListNotifications / NotificationViewer; device_handler.go retries comment;
  test-db.sh should note its dependency on the image's init restart;
  `deploy/snmpsim/README.md` says four profiles (there are five) and lacks a v1
  radio check.
- Close note chosen by `len(actions)==1` — pass the PollResult into apply.
- Unused `useSendTestNotification` hook.
