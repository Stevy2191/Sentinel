# Tools and security S1 — Network tools: design

Status: design agreed with the owner on 2026-10-05, section by section.
Roadmap: `2026-10-04-tools-and-security-roadmap.md` (phase S1, first of the
tools and security track).

## Purpose

Admins and trusted staff troubleshoot reachability from inside Sentinel: pick
a host and a vantage point (the Sentinel server or any enabled server agent),
run ping, traceroute, a DNS lookup or a TCP port check, and watch the result
arrive live. "Can the file server reach the gateway, and where does it break?"
is the typical question.

S1 also builds the pieces the rest of the track reuses: the agent job
channel, Sentinel's first live push channel (SSE), and the guardrails
(permission, target allowlist, limits, audit, agent opt-in).

Success looks like this: a permitted user picks a target and a vantage point,
and results start appearing within a second or two, agents included; every
run is refused or recorded according to the guardrails; agents that have not
been updated keep working exactly as before.

## Decisions taken with the owner

| Question | Decision |
|---|---|
| Who may run tools | Admins always, plus non-admins an admin grants a "Network tools" permission to |
| Target allowlist out of the box | Empty: nothing runs until an admin adds subnets or hosts |
| Agent opt-in | Both a local flag in the agent's own config and an admin toggle in Sentinel |
| Results after a run | Kept as run history, pruned after a retention period (default 30 days) |
| Port scan reach | One host, at most 1,024 ports per run |
| Address family | IPv4 only in S1 (DNS lookups still return AAAA records) |
| Agent channel and streaming | Agents long-poll for jobs; results stream to the browser over SSE |

## What exists today (constraints)

- Agents only push: metrics every `CHECK_INTERVAL` (default 60 s) and a
  heartbeat every 5 minutes, both outbound POSTs with a per-agent bearer
  token. The agent never reads the server's reply. There is no server-to-agent
  channel.
- No SSE, WebSocket or long-poll exists in the backend or frontend. The
  `http.Server` has no `WriteTimeout`. nginx buffers `/api/` responses; Caddy
  flushes `text/event-stream` responses on its own.
- The ping monitor (`CheckService.ExecutePingCheck`) is IPv4-only, tied to a
  monitor, and falls back to a TCP connect. The DNS monitor only resolves
  A/AAAA through the system resolver. There is no traceroute code.
- The audit log (`audit_log`, `AuditService.Record`) exists, with an admin API
  and no screen.
- Authorization is `is_admin` (with `role` mirrored); there is no finer
  permission. A token-bucket `RateLimiter` exists (`ByUser`/`ByIP`); nothing
  caps concurrency.
- The backend container has `cap_add: NET_RAW`. The Linux agent runs as root
  under systemd; the Docker agent has `NET_RAW` by Docker's default; the
  Windows agent runs as LocalSystem.

## Architecture

### `internal/nettools` — the tools

A package with no database and no HTTP, compiled into both the Sentinel
binary and the agent binary (`cmd/agent` is in the same module).

- One interface: `Run(ctx, spec, emit)`. Each tool pushes typed events
  through `emit` as they happen and returns a summary at the end.
- `Validate(spec)` enforces every limit in this document (counts, ports,
  timeouts, record types). Sentinel calls it when a run is created; the agent
  calls it again before running a job, so a crafted request cannot exceed the
  limits at either end.
- A shared ICMP echo probe with a settable TTL underlies both ping and
  traceroute:
  - Linux and Docker: raw ICMP through `golang.org/x/net/icmp`, falling back
    to unprivileged ICMP (`udp4`) for ping only. Traceroute needs raw ICMP to
    read Time Exceeded messages.
  - Windows: the system ICMP API (`IcmpSendEcho2` from `iphlpapi.dll`, called
    without CGO), which reports TTL-exceeded replies with the hop address.
- DNS uses `golang.org/x/net/dns/dnsmessage` (already a dependency through
  `golang.org/x/net`), so no new library.
- Context cancellation and the run's deadline stop any tool immediately.

### `internal/stream` — SSE and the run hub

- An SSE writer helper: writes `id`/`event`/`data` frames, flushes each one,
  sends a keep-alive comment every 15 s, and sets `Content-Type:
  text/event-stream`, `Cache-Control: no-cache` and `X-Accel-Buffering: no`
  (so nginx passes the stream through without buffering).
- An in-memory hub keyed by run ID: subscribers get a buffered channel; a
  subscriber that falls behind is dropped rather than blocking the publisher.
- Network phase 6 reuses this package for its live maps.

### Tool-run service (backend `services`)

Owns the run lifecycle: create (guardrails, then start locally or queue for
an agent), record events, finish, cancel, time out, list history, prune.
The database is the record; the hub only carries live events.

### Agent job dispatcher

The queue is the `tool_runs` table (agent runs wait in `queued`). An
in-memory wake signal per agent (buffered channel of one) fires when a run is
queued for it. Because the database is the record, a missed signal costs at
most one long-poll cycle.

### Agent

A `jobs.go` loop in `cmd/agent` that starts only when the local
`ENABLE_TOOLS=true` is set. Details under "Agent job channel".

### Frontend

A Tools page, a run detail page, entry points on server, device and monitor
pages, a Settings tab, a per-user grant and a per-agent toggle. Details under
"Interface".

## Guardrails

### Permission

- New column `users.net_tools` (boolean, default false). Admins are always
  permitted, whatever the column says.
- `RequireNetTools` middleware re-reads the user from the database on every
  request (as `RequireAdmin` does) and aborts with 403 (`c.AbortWithStatusJSON`)
  when the user is neither an admin nor granted.
- Grant holders may: run tools, see every run (it is a team troubleshooting
  tool), and cancel their own runs.
- Only admins may: change the allowlist and other tool settings, grant or
  revoke the permission, toggle tools on an agent, and cancel other users'
  runs.

### Target allowlist

- Setting `net_tools_allowlist`: a JSON array of strings, empty by default.
  An empty list means no target is allowed.
- Entry forms: an IPv4 address, an IPv4 CIDR, an exact hostname, or a
  wildcard hostname `*.example.org` (matches any name ending in
  `.example.org`, not `example.org` itself). Hostnames compare
  case-insensitively, trailing dot ignored.
- On save, every entry is validated and each bad line reported (not a valid
  address, CIDR or hostname; or "too broad"). CIDRs broader than /8 are
  refused as too broad.
- Always blocked, whatever the list says: the cloud metadata ranges that
  `netguard` blocks, `0.0.0.0/8`, multicast (`224.0.0.0/4`) and
  `255.255.255.255`.
- Matching: Sentinel resolves the target once, when the run is created
  (IPv4 only). The run is allowed when the typed hostname matches a hostname
  entry, or when the resolved address falls inside an address or CIDR entry,
  and the address is not always-blocked. The run stores both the typed target
  and the resolved `target_ip`; the tool, on Sentinel or on an agent, probes
  `target_ip` only. What was checked is what gets probed; DNS cannot change it
  afterwards.
- DNS lookups: the name being looked up is not probed and is not checked. The
  vantage's own system resolver is always allowed. A named DNS server is a
  target and must pass the allowlist.

### Limits (fixed in S1)

| Limit | Value |
|---|---|
| Runs started per user | 30 per minute, burst 10 (`RateLimiter`, `ByUser`) |
| Runs started per target address | 20 per minute, across all users |
| Active runs per user | 3 |
| Active runs per target address | 3 |
| Active runs overall | 10 |
| Active runs per agent | 2 |

- The per-target limits key on the resolved `target_ip`; a DNS lookup through
  the system resolver has none and is limited per user only. The per-target
  rate limit is an in-memory token bucket checked in the service after
  resolution (the target is not known at middleware time).
- "Active" means `queued` or `running`. The active-run caps are checked
  inside the insert transaction under a transaction-level advisory lock, so
  concurrent creates cannot overshoot.
- A refusal returns 429 with code `limit` and a plain reason ("You already
  have 3 runs in progress").
- Hard deadlines per run: DNS 15 s, ping 2 min, traceroute 3 min, port scan
  5 min.

### Audit

New action constants in `models/audit.go` (snake case, like the existing
ones):

| Action | When | Resource |
|---|---|---|
| `tool_run_started` | a run is created | `tool_run`, run ID |
| `tool_run_finished` | a run reaches a final status | `tool_run`, run ID |
| `tool_run_refused` | refused for permission or allowlist reasons | `tool_run`, nil |
| `net_tools_settings_updated` | settings saved | `settings`, nil |
| `user_net_tools_changed` | grant given or removed | `user`, user ID |
| `agent_tools_changed` | agent toggle flipped | `agent`, agent ID |

- `tool_run_started` records the tool, the typed target and resolved address,
  the vantage (Sentinel or the agent's name and ID) and the parameters.
  `tool_run_finished` records the final status, duration and a one-line
  result. Rate-limit refusals (429) are not audited, so they cannot flood the
  log.
- Finishes written by the sweeper (timed out, interrupted, not picked up) are
  audited with the system as the actor.
- No audit screen in S1; the run history serves day-to-day use.

### Agent opt-in

- Two switches, both required:
  - Local: `ENABLE_TOOLS=true` in the agent's own config (an install option
    on Linux, Docker and Windows). Without it the agent never polls for jobs.
  - Sentinel: the admin toggle `agents.tools_enabled`.
- The heartbeat reports the local flag as `tools_local`, stored in
  `agents.tools_local`, so Sentinel can show both switches.
- Optional local `TOOLS_ALLOWED_TARGETS` (comma-separated IPv4 addresses and
  CIDRs): the agent refuses any job whose `target_ip` falls outside it. A
  compromised Sentinel cannot aim the agent anywhere else.
- The Sentinel server is a vantage too, switched by the setting
  `net_tools_server_enabled` (default on; nothing runs anyway until the
  allowlist has entries).

## Agent job channel

All agent endpoints use the agent's bearer token (`RequireAgentToken`) and
the existing own-agent check, and are mounted with the other agent ingest
routes.

### `GET /api/v1/agents/:agent_id/jobs/next`

- Answers immediately when a queued run exists for this agent. Otherwise it
  waits up to 25 s for the agent's wake signal (or for the agent to hang up)
  and then returns 204.
- Claim: one statement moves the oldest queued run for the agent to
  `running` (`UPDATE … WHERE id = (SELECT … FOR UPDATE SKIP LOCKED) RETURNING
  …`), so two polls can never take the same run.
- Response: run ID, tool, typed target, `target_ip`, parameters, deadline.
- If `agents.tools_enabled` is false: 403 with code `tools_disabled`; the agent
  backs off 60 s before polling again.
- Every poll stamps the agent's last-poll time in memory. An agent counts as
  ready when both switches are on and it polled within the last 60 s.

### `POST /api/v1/agents/:agent_id/jobs/:run_id/events`

- A batch of events, each with its sequence number; the agent sends a batch
  about every 250 ms while the tool runs.
- Stored with `ON CONFLICT (run_id, seq) DO NOTHING`, so a retried post never
  duplicates events.
- Rejected (409) unless the run belongs to this agent and is `running`.
- Caps: 64 KB per request body, 5,000 events per run. Reaching the event cap
  ends the run as `failed` ("too much output").
- The reply carries `cancel: true` once the run has been cancelled; the agent
  stops the tool.

### `POST /api/v1/agents/:agent_id/jobs/:run_id/finish`

- Final status (`done`, `failed` or `refused`), the summary and any error.
  Same ownership and state checks as events.

### Agent loop (`cmd/agent/jobs.go`)

- Starts only with `ENABLE_TOOLS=true`. Polls `jobs/next` with a 35 s client
  timeout.
- On a job: checks locally that the tool is known, `nettools.Validate`
  passes, `target_ip` is inside `TOOLS_ALLOWED_TARGETS` (when set) and the
  deadline has not passed. A failed check finishes the run as `refused` with
  the reason.
- Runs jobs in goroutines, at most 2 at a time, with the job's deadline on
  the context. Network errors back off from 5 s doubling to 60 s.
- The heartbeat gains `tools_local`. The agent version moves to 1.1.0.
- Agents that have not been updated never poll and never send
  `tools_local`; Sentinel shows them as "update the agent to use tools".

### Timeouts and sweeper

- A queued run not picked up within 30 s fails: "the agent didn't pick up the
  job (offline?)".
- A running run past its deadline plus 15 s becomes `timed_out`.
- At startup, runs left `queued` or `running` become `interrupted`. An agent
  still working on one gets 409 on its next post and stops.

### Runs on the Sentinel server

Run in a goroutine through the same recorder as agent runs (no HTTP), so they
store, stream, cancel and time out identically.

### Proxies

Agents usually reach Sentinel through Caddy or nginx; a 25 s hold is well
inside their default 60 s read timeouts, so no proxy change is needed.

## The tools

### Ping

| Parameter | Default | Range |
|---|---|---|
| Count | 5 | 1–100 |
| Interval | 1 s | 0.2–5 s |
| Probe timeout | 2 s | 0.5–5 s |
| Payload size | 56 bytes | 0–1,472 bytes |

- Events: `reply` (seq, RTT, TTL, from), `timeout` (seq), `error` (seq and
  message, e.g. "host unreachable from 10.0.0.1").
- Summary: sent, received, loss %, min, avg, max, jitter.
- If ICMP cannot be opened, the run fails and says so; no TCP fallback.

### Traceroute (MTR style)

| Parameter | Default | Range |
|---|---|---|
| Max hops | 30 | 1–30 |
| Rounds | 5 | 1–10 |
| Probe timeout | 1 s | 0.5–3 s |

- Each round sends one ICMP echo per TTL, in parallel. Replies are matched by
  the ICMP id and sequence quoted inside Time Exceeded messages. Hops beyond
  the destination are dropped.
- Events: `hop` (round, TTL, address or timeout, RTT, whether it is the
  destination), `round_done` (round), `hop_name` (TTL, address, name; a
  best-effort reverse lookup per new hop address, 1 s timeout).
- Summary per hop: addresses (several when the path splits), name, sent,
  loss %, last, avg, best, worst, stdev. Overall: destination reached, hop
  count.

### DNS lookup

- Record types: A, AAAA, CNAME, MX, NS, TXT, SOA, SRV, PTR, CAA. For PTR, an
  IPv4 address is turned into its `in-addr.arpa` name automatically.
- Server: the vantage's system resolver (default) or a named IPv4 server with
  an optional port (default 53), which must pass the allowlist. The system
  resolver's address comes from `/etc/resolv.conf` on Linux (Docker's
  embedded DNS inside the container) and from the adapters' DNS servers on
  Windows; the query always goes straight to that server.
- Transport: UDP, retried over TCP when the answer is truncated; 5 s timeout;
  recursion desired.
- Event: one `answer` event with the server that answered, response code,
  AA and TC flags, RTT, and the answer, authority and additional sections
  (name, type, TTL, data per record).

### TCP port check / quick scan

- Ports: one port, a list and ranges (`22,80,443,8000-8100`), or the
  `common` preset (about 100 well-known ports; the exact list is fixed in the
  plan). At most 1,024 ports per run; one host.
- Per-port timeout 1.5 s (range 0.5–5 s); 50 connects at a time.
- States: `open` (connected, with RTT), `closed` (refused), `filtered`
  (timed out or unreachable).
- Events: one `port` event per port as soon as its state is known, with a
  well-known service name when there is one ("22 ssh", "3389 rdp").
- Summary: counts by state and the list of open ports.
- No banner grabbing or version detection (nmap does that in S2).

## Data model

Migration `059_network_tools.sql` (re-check `ls backend/migrations | tail -1`
when implementing):

- `users.net_tools BOOLEAN NOT NULL DEFAULT false`.
- `agents.tools_enabled BOOLEAN NOT NULL DEFAULT false`,
  `agents.tools_local BOOLEAN NOT NULL DEFAULT false`.
- `tool_runs`:
  - `id UUID PRIMARY KEY`
  - `tool TEXT NOT NULL` — CHECK `tool IS NOT NULL AND tool IN ('ping',
    'traceroute', 'dns', 'tcp')`
  - `status TEXT NOT NULL` — CHECK over `queued, running, done, failed,
    refused, cancelled, timed_out, interrupted` (with `IS NOT NULL`)
  - `user_id UUID REFERENCES users ON DELETE SET NULL`, `username TEXT NOT
    NULL` (snapshot)
  - `vantage_kind TEXT NOT NULL` — CHECK `sentinel` or `agent`
  - `agent_id UUID REFERENCES agents ON DELETE SET NULL`, `vantage_name TEXT
    NOT NULL` (snapshot)
  - No CHECK ties `vantage_kind` to `agent_id`: it would break the
    `ON DELETE SET NULL` when an agent is deleted.
  - `target TEXT NOT NULL` (as typed), `target_ip INET` (NULL for a DNS lookup
    through the system resolver)
  - `params JSONB NOT NULL`, `summary JSONB`, `error TEXT`,
    `event_count INT NOT NULL DEFAULT 0`
  - `created_at TIMESTAMPTZ NOT NULL`, `started_at TIMESTAMPTZ`,
    `finished_at TIMESTAMPTZ`, `deadline TIMESTAMPTZ NOT NULL`
  - Indexes: `created_at DESC`; partial indexes on `(user_id)`, `(agent_id)`
    and `(target_ip)` where `status IN ('queued','running')` for the caps.
- `tool_run_events`: `run_id UUID REFERENCES tool_runs ON DELETE CASCADE`,
  `seq INT`, `at TIMESTAMPTZ NOT NULL`, `type TEXT NOT NULL`, `data JSONB NOT
  NULL`, `PRIMARY KEY (run_id, seq)`. A plain table; volume is small.
- Settings keys: `net_tools_allowlist` (JSON array, default `[]`),
  `net_tools_server_enabled` (default true), `net_tools_retention_days`
  (default 30, range 1–365).
- A daily prune deletes finished runs older than the retention period; their
  events go with them. Audit entries are kept.
- Backups: both tables are in `public`, so dumps include them unchanged.

## Streaming

`GET /api/v1/tools/runs/:id/events` (session cookie, `RequireNetTools`),
read in the browser with `EventSource`.

- SSE event types: `event` (a tool event, `id` = its seq), `status` (status
  changes), `end` (final status and summary). The server closes the stream
  after `end`; a request for a run that has already finished replays and
  ends at once.
- Reconnects: `EventSource` resends `Last-Event-ID`; the server replays
  events after it.
- No gaps or duplicates: subscribe to the hub first, replay stored events
  after `Last-Event-ID` from the database, then forward live events, skipping
  any seq already sent.
- A subscriber that falls behind is dropped; its browser reconnects and
  replays from the database.
- The recorder (shared by Sentinel and agent runs) writes events in batches
  about every 250 ms: insert the batch, bump `event_count`, publish to the
  hub.

## API

User side (all under `AuthMiddleware` and `RequireNetTools`):

| Method | Path | Purpose |
|---|---|---|
| GET | `/tools/vantages` | Sentinel plus every agent, each with `ready` and, when not ready, the reason: tools off in Sentinel, not enabled on the server, offline, agent too old |
| POST | `/tools/runs` | Create a run: `{tool, vantage: {kind, agent_id}, target, params}` |
| GET | `/tools/runs` | History; filters `tool`, `user_id`, `agent_id`, `target`, `status`, `limit`, `offset`; `X-Total-Count` header |
| GET | `/tools/runs/:id` | One run with all its events |
| GET | `/tools/runs/:id/events` | SSE stream (above) |
| POST | `/tools/runs/:id/cancel` | Cancel (own run, or any run for admins) |

`POST /tools/runs` returns 201 with the run, or an error with a `code`:

| Status | Code | Meaning |
|---|---|---|
| 403 | `forbidden` | not an admin and not granted |
| 422 | `invalid_params` | `Validate` refused the parameters |
| 422 | `target_not_allowed` | outside the allowlist, or always blocked |
| 422 | `resolve_failed` | the target does not resolve from Sentinel |
| 422 | `ipv6_unsupported` | the target resolves only to IPv6 |
| 409 | `vantage_not_ready` | the chosen agent or the Sentinel server cannot run tools now |
| 429 | `limit` | a rate limit or active-run cap |

Admin side:

- `GET /settings/net-tools` and `PUT /settings/net-tools` (`{allowlist,
  server_enabled, retention_days}`); a PUT with bad allowlist entries returns
  422 listing each bad entry and saves nothing.
- `PATCH /users/:id/net-tools` (`{enabled}`).
- The existing `PATCH /agents/:agent_id` accepts `tools_enabled`.
- `GET /me` includes `net_tools`, so the frontend knows whether to show the
  tools.

## Interface

- **Navigation:** a top-level "Network Tools" entry (`/tools`), visible to
  admins and grant holders only.
- **Tools page:**
  - Tool tabs: Ping, Traceroute, DNS, Ports.
  - Form: target (with a known-hosts picker of devices, monitors and agents,
    as a convenience; the allowlist still applies), "Run from" (Sentinel and
    every agent; agents that are not ready are disabled with the reason
    shown), a collapsed Options section per tool with the defaults filled in,
    Run, and Cancel while running.
  - Empty allowlist: a banner, "No targets are allowed yet. An admin adds
    subnets and hosts in Settings → Network tools" (with a link for admins).
  - Status line: "Waiting for file-server to pick up…", "Running — round 3 of
    5", "Done", or the failure or refusal reason in plain language.
  - Live results: ping (reply list, a live RTT chart, tiles for loss, min,
    avg, max, jitter); traceroute (an MTR table updating each round:
    hop, host and address, loss %, sent, last, avg, best, worst, stdev);
    DNS (header line, then answer, authority and additional tables with TTLs);
    ports (progress bar, open ports first with service names, closed and
    filtered counts that expand to the list).
  - History below: time, user, tool, target → address, from, status and a
    one-line result ("0% loss, 2.1 ms avg", "reached in 9 hops", "3 open");
    filters for tool, mine or all, and status.
- **Run detail** (`/tools/runs/:id`): the same result panel built from the
  stored events, live while the run is going; "Run again" fills the form.
- **Entry points:** ServerDetail gets a "Network tools" button (opens the
  page with that agent as the vantage); DeviceDetail and MonitorDetail get
  "Ping / Trace" (target filled in). Shown only to permitted users.
- **Admin:**
  - Settings gets a "Network tools" tab: the allowlist editor (one entry per
    line, errors shown against the bad lines), "Allow runs from the Sentinel
    server", retention days.
  - AdminUsers gets a "Network tools" checkbox per user; for admins it is
    checked and disabled ("Admins always can").
  - The agent edit form gets "Allow network tools", with the agent's own
    state beside it: "Enabled on the server: no — add `ENABLE_TOOLS=true` to
    the agent config" or "Agent 1.0.0 — update to 1.1.0 to use tools".
  - The add-agent install instructions gain "Enable network tools on this
    server", which adds `ENABLE_TOOLS=true` to the generated command.
- House rules: slate theme and `src/utils/colors.ts`; confirmations in the
  page; helpers in `src/utils/` or hooks, not component files. Switching tool
  or run closes the open `EventSource`, so a late event cannot land on the
  wrong view.

## Errors

- Target errors are refused at create time with the codes above and a plain
  message ("fileserver.local doesn't resolve from Sentinel — use its IP
  address").
- ICMP unavailable at the vantage: the run fails with "ICMP isn't available
  here (needs root or NET_RAW)".
- Agent offline: a queued run fails after 30 s. Agent vanishes mid-run: the
  run times out at deadline plus 15 s.
- Cancel: a Sentinel run stops at once; an agent sees `cancel: true` on its
  next events post (within about 250 ms).
- Sentinel restart: runs in progress become `interrupted`.
- Saving the allowlist never affects runs already in progress.

## Testing

- **`nettools` unit tests:** `Validate` limits; port-list parsing; allowlist
  parsing and matching (address, CIDR, hostname, wildcard, too broad, always
  blocked); DNS against an in-test UDP and TCP server (truncation retry, PTR
  conversion, sections and TTLs); port states through an injected dialer;
  ping and traceroute logic through a fake probe (round assembly, hop
  statistics, split paths, destination detection) plus parsing of recorded
  ICMP Time Exceeded and Echo Reply packets; real loopback ping and traceroute
  tests that skip when ICMP is not permitted. The Windows probe gets a
  cross-compiled `go vet` (`GOOS=windows`).
- **DB tests:** the migration; caps under concurrent creates (exactly the cap
  succeeds); two pollers and one run (exactly one claims it); duplicate event
  seqs; the sweepers (not picked up, timed out, interrupted at startup);
  retention prune cascading to events; audit entries written.
- **API tests:** `RequireNetTools` aborts and the guarded handler does not
  run; grant holders cannot change settings, grants or agent toggles, or
  cancel others' runs; an agent cannot read or write another agent's run;
  body and event caps; long-poll wake and 204 timeout (wait shortened in
  tests); SSE replay from `Last-Event-ID`, live follow, close on `end`, and
  the `X-Accel-Buffering: no` header.
- **Agent tests** (against an `httptest` server): no polling without
  `ENABLE_TOOLS`; refusal outside `TOOLS_ALLOWED_TARGETS`; cancel honoured;
  back-off after errors.
- **Frontend:** the project gate (type check, lint, build).
- **Sandbox check on the dev stack:** a real ping and traceroute from the
  backend container and from a Linux agent (a Windows agent if one is to
  hand); confirm SSE events arrive one by one through Caddy rather than all
  at the end.

## Rollout

- No `docker-compose.yml` change (the backend already has `NET_RAW`) and no
  proxy configuration change (the backend sends `X-Accel-Buffering: no`).
- The agent binaries (1.1.0) ship in the backend image as usual. Existing
  agents carry on unchanged until updated and enabled.
- Enabling an existing agent: re-run the install script with the tools
  option, or add `ENABLE_TOOLS=true` to the agent's config
  (`/etc/sentinel-agent` env file on Linux, the service environment on
  Windows, `-e ENABLE_TOOLS=true` for the Docker agent) and restart it; then
  flip the toggle in Sentinel.

## Out of scope for S1

IPv6 targets; subnet sweeps; banner and version detection; nmap (S2); an
audit-log screen; tunable limits; UDP probes and UDP traceroute; scheduled or
recurring tool runs; alerts raised from tool runs; moving the existing
monitors onto `nettools`.

## Risks

- **Windows ICMP.** `IcmpSendEcho2` through `syscall` without CGO is the
  least familiar piece; it is isolated behind the probe interface and checked
  by a cross-compile, with a real check on a Windows agent in the sandbox.
- **Raw ICMP on agent hosts.** Hardened hosts may block ICMP Time Exceeded
  inbound; traceroute then shows timeouts past the first hop, which is the
  honest result, and the run says so in its summary.
- **Long-poll and proxies.** A proxy with a read timeout under 25 s between
  agent and Sentinel would cut polls short; the agent treats that as an
  empty poll and re-polls, so it degrades to slower pickup rather than
  failing.

## Amendments made while planning (2026-10-05)

Found while reading the code for the implementation plan; they override the
text above where the two differ.

1. **Old agents are detected by a missing flag, not by version.** CI stamps
   the agent binary with the image's version, which is the branch name
   (`dev`, `main`) for branch builds, so no version comparison is possible.
   `agents.tools_local` is therefore nullable: NULL means the agent has never
   reported the flag (too old to run tools), false means a current agent with
   `ENABLE_TOOLS` off, true means enabled. The agent form says "This agent's
   version can't run tools — update it" instead of naming versions.
2. **`target_ip` is TEXT** (canonical dotted IPv4), not INET: only equality
   is needed, and TEXT avoids driver-specific INET scanning.
3. **`tool_runs.agent_ref TEXT`** snapshots the agent's readable id
   (`agent_…`), which the API returns as `agent_id` for links and "Run
   again"; `tool_runs.agent_id` stays the UUID foreign key.
4. **The agent toggle has its own endpoint**, `PUT /agents/:agent_id/tools`
   (`{enabled}`), instead of a new field on `PATCH /agents/:agent_id`, so the
   change is audited without touching the general agent update.
5. **`GET /tools/vantages` also returns `allowlist_empty`**, because grant
   holders cannot read the admin settings but the Tools page needs to show
   the empty-allowlist banner.
6. **The Linux agent's config file is `/etc/sentinel/agent.conf`.**
7. **Changed by the final whole-branch review** (see the follow-ups doc,
   "Fixed in the final whole-branch review"): an agent answered
   `tools_disabled` backs off 15 s, not 60 s; a poll answered
   `tools_disabled` does not count towards readiness (only polls with tools
   on are stamped); and the agent bounds a job by its own clock (claim time
   + the tool's deadline) instead of checking Sentinel's absolute deadline,
   so clock skew between the two cannot refuse or shorten runs.
8. **The allowlist fences port checks only** (owner's decision after S1
   reached dev, 2026-10-07). Ping, traceroute and DNS lookups, including a
   named DNS server, may contact any address that is not always blocked; the
   always-blocked addresses are refused for every tool. The empty-allowlist
   banner shows on the Ports tab only. See the follow-ups doc, "Changed after
   S1 reached dev".
