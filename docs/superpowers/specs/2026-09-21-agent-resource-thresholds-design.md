# Configurable resource-usage alert thresholds for server agents

## Problem

A server agent can only alert on connectivity today: it went offline, or it
came back (`029_server_agents.sql` / `040_agent_notifications.sql`, driven by
`AgentService`'s offline sweep). Every metrics sample a server reports — CPU,
memory, disk usage — is stored and shown on its detail page, but nothing ever
evaluates those values against anything. A server can sit at 98% disk for
weeks and Sentinel will never say a word about it.

## Scope

- Three metrics get a configurable threshold: **CPU%**, **memory%**,
  **disk%** — each already a clean 0–100 value on `AgentMetric`. Network
  throughput and load average are excluded: network has no natural "100%"
  (a threshold there means an absolute rate, a different kind of setting),
  and load average only means something relative to core count. Either could
  be added later without disturbing this design, but neither is in scope now.
- **Per-server thresholds**, not one instance-wide default. A log server that
  normally runs at 85% disk needs a different number than one that should
  never pass 50%. New servers default to 90% for all three (adjustable or
  clearable before saving); existing servers get no threshold at all until an
  admin sets one — this must not make already-deployed servers suddenly start
  alerting on values they've always run at.
- **Alert on the first breach**, not after N consecutive samples. Unlike a
  flaky network ping, resource usage doesn't flap from one collection cycle
  to the next; requiring consecutive breaches would mostly just delay a real
  alert.
- One shared channel list per server (`agents.notify_channels`) covers both
  connectivity and threshold alerts — not a separate selector per alert type.
  This already exists and this feature reuses it as-is.

## Design

### Data model

Six new nullable/boolean columns on `agents` (migration
`042_agent_resource_thresholds.sql`):

```
cpu_threshold_percent     SMALLINT   -- NULL = disabled
memory_threshold_percent  SMALLINT   -- NULL = disabled
disk_threshold_percent    SMALLINT   -- NULL = disabled
cpu_alert_active          BOOLEAN NOT NULL DEFAULT false
memory_alert_active       BOOLEAN NOT NULL DEFAULT false
disk_alert_active         BOOLEAN NOT NULL DEFAULT false
```

Threshold values are checked (1–100) at the database level, same as every
other bounded column in this schema. No column-level `DEFAULT` on the
threshold values themselves: the 90% default for a *new* server lives in Go
(`models.DefaultThresholdPercent`), the same way `DefaultAgentInterval` and
`DefaultAgentRetries` already work — not a hidden default INSERT would
otherwise silently apply. Existing rows are added with these columns as
`NULL`/`false`, exactly what "not configured yet" and "no alert currently
open" should mean.

The `_alert_active` flags are the one-shot gate, mirroring `agents.status`
exactly: the flag *is* the record of "is there an open alert for this
metric right now", not a separate counter. A sweep or ingest cycle that
finds the flag already matching reality does nothing further.

### Evaluation

`AgentService.RecordMetrics` already runs inside one transaction per
incoming sample (insert the metric row, update `last_heartbeat`/`status`).
This adds, in the same transaction, for each of the three metrics with a
non-nil threshold:

```
breached := incomingValue >= threshold
switch {
case breached && !agent.XAlertActive:
    set XAlertActive = true;  record a "breach" transition
case !breached && agent.XAlertActive:
    set XAlertActive = false; record a "recovered" transition
default:
    no state change, no transition
}
```

Transitions are collected during the transaction and only acted on after
it commits — same reason `RecordMetrics` already waits until after commit to
fire the offline/online notification: never notify about a write that could
still roll back.

A metric that came back `nil` this cycle (every `AgentMetric` field is a
pointer for exactly this reason — a first-ever CPU sample, or any reading
that failed, is reported as absent rather than a false zero) is skipped for
that cycle, not treated as 0%: a missing reading must never look like a
sudden drop to 0% and silently flip an open alert to "recovered".

Editing a threshold — raising it, lowering it, or clearing it — is a config
edit, handled in `AgentService.Update`, not a metrics-driven transition: any
change to a metric's threshold value resets its `_alert_active` flag to
false, without firing a "recovered" notification, since nothing about the
server itself changed. The next incoming sample re-evaluates fresh against
whatever the threshold now is, and opens a new breach on its own if the
current value still warrants one.

A new `AgentService.SetThresholdChangeHook(fn func(context.Context,
AgentThresholdChange))` mirrors `SetStatusChangeHook` exactly, for the same
reason its own doc comment gives: `AgentService`'s job is agent state, not
notification delivery, and wiring delivery in directly would pull the whole
notification graph in behind it.

```go
type AgentThresholdChange struct {
    Agent     models.Agent
    Metric    string  // "cpu" | "memory" | "disk"
    Value     float64 // the sample that caused the transition
    Threshold int
    Breached  bool // true = just crossed over, false = just recovered
}
```

### Notification delivery

`cmd/sentinel/main.go` wires the new hook next to the existing one:

```go
agentService.SetThresholdChangeHook(func(ctx context.Context, change services.AgentThresholdChange) {
    notifyAgentThresholdChange(ctx, notificationManager, change)
})
```

`notifyAgentThresholdChange` (new, alongside the existing
`notifyAgentStatusChange`) builds a `notifications.NotificationMessage` the
same shape as the offline alert (`AgentID`, `MonitorName: agent.Name`,
`Channels: agent.NotifyChannels`), with:

- `Status: "warning"` on breach, `"recovered"` on recovery — **not** `"down"`.
  Every plugin's status handling today treats anything other than the exact
  string `"down"` as green/good (confirmed in email, Slack, Discord,
  Telegram, ntfy), so reusing `"down"` for "disk is at 95%" would have every
  channel announce the server is *offline*, which is false and exactly the
  kind of alert that trains people to ignore alerts. `"warning"` is a new,
  third status.
- `Message`: e.g. `"Disk usage is at 95%, at or above the 90% threshold"` /
  `"Disk usage is back under the 90% threshold (currently 82%)"`.

Adding `"warning"` touches five plugins' status-to-presentation logic (the
sixth, the generic webhook, just serializes `Status` as a JSON field and
needs no change):

| Plugin | Change |
|---|---|
| `email.go` | `buildSubject`: `"warning"` → `"[WARNING] %s"`. `statusStyle`: `"warning"` → new `colorWarning = "#f59e0b"` (amber), label `"WARNING"`. |
| `slack.go` | Extend the existing `if m.Status == "down"` to also branch on `"warning"` → 🟡, `colorWarning` (the same constant, package-shared with email.go). |
| `discord.go` | Same shape, new `colorDiscordWarning = 0xF59E0B`. |
| `telegram.go` | Same shape, 🟡 for `"warning"`. |
| `ntfy.go` | `buildTitle`: `"warning"` → `"[WARNING] %s"`. `buildTags`: `"warning"` → `"warning"` tag (a standard emoji shortcode, ⚠️). Priority stays `"default"`, not `"high"` — a resource warning is real but a notch below a full outage. |

### Frontend

New "Alert Thresholds" section in `AgentSettingsFields.tsx` (shared by
create and edit), between "Collection" and "Notifications": three rows (CPU,
Memory, Disk), each a checkbox to enable plus a 1–100 number input, disabled
when unchecked. `AddServerAgentModal.tsx`'s blank state pre-fills all three
enabled at 90%; `EditServerAgentModal.tsx` seeds from the agent's actual
stored values (including "disabled" when null) and includes the fields in
its existing dirty-check and save payload, the same way every other setting
there already works.

Validation: 1–100 when enabled, matching the backend's check constraint —
same pattern as the existing interval/retries bounds.

## Testing

- `AgentService`: table-driven tests over the breach/no-change/recover state
  machine for each metric, verifying the hook fires exactly once per real
  transition and not on a repeated breach — the same shape as whatever
  covers `MarkStaleOffline`'s one-shot behavior today.
- Notification plugins: extend the existing fake-SMTP-style / unit tests to
  confirm `"warning"` renders with the new color/label and never falls into
  each plugin's default "green/good" branch.
- Migration applies cleanly against the existing `agents` table with data
  in it (the production table already has real rows).

## Release

This ships as a feature release. Once implemented and verified, tag and
push a new version (next: `v0.2.0`) the same way `v0.1.0` was cut, which
triggers the existing CI pipeline (versioned images + a GitHub Release).
