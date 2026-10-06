# Tools and Security S1: Network Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On-demand ping, MTR-style traceroute, DNS lookup and TCP port check/quick scan, run from the Sentinel server or any enabled server agent, with results streamed live to the browser, kept as run history, and guarded by a per-user grant, a target allowlist, limits, audit entries and a two-switch agent opt-in.

**Architecture:** The tools live in a new dependency-light package `internal/nettools` that both the Sentinel binary and the agent compile in; each tool reports typed events through a callback and returns a summary. A new package `internal/toolruns` owns the run lifecycle (guardrails, local execution, the agent job queue in `tool_runs`, event recording, sweeping, pruning) and publishes live events through `internal/stream` (an SSE writer and an in-memory hub). Agents with `ENABLE_TOOLS=true` long-poll `GET /agents/:agent_id/jobs/next`, run the job with `nettools`, and post events back in batches; browsers read `GET /tools/runs/:id/events` with `EventSource`.

**Tech Stack:** Go 1.26, Gin, GORM (pgx), PostgreSQL 16 + TimescaleDB, `golang.org/x/net` (icmp, ipv4, dns/dnsmessage), `golang.org/x/sys/windows`, `golang.org/x/time/rate`; React 18 + TypeScript + Vite + Tailwind + Recharts.

**Spec:** `docs/superpowers/specs/2026-10-05-tools-s1-network-tools-design.md` (including its "Amendments made while planning" section, which overrides the body where they differ).

## Global Constraints

- Module path `github.com/Stevy2191/Sentinel/backend`; Go 1.26. **No new Go module and no new npm package**: `golang.org/x/net`, `golang.org/x/sys` and `golang.org/x/time` are already in `go.mod`.
- `internal/nettools` imports only the standard library and `golang.org/x/net/...`, `golang.org/x/sys/...` — never gorm, gin, `internal/models`, `internal/services` or any other Sentinel package — because the agent binary compiles it in.
- IPv4 only for probe targets. DNS lookups still return AAAA records.
- The migration is `backend/migrations/059_network_tools.sql` (058 is the latest; re-check with `ls backend/migrations | tail -1`). Never edit an applied migration. `col IN (...)` CHECKs also say `col IS NOT NULL`. No CHECK ties `vantage_kind` to `agent_id` (it would break `ON DELETE SET NULL`).
- Timestamps: `TIMESTAMPTZ` columns; `time.Now().UTC()` in Go.
- Gin middleware must abort (`c.Abort…`); `respondError` never in middleware. Tests of a guard assert the guarded handler did NOT run.
- Limits, exactly: per user 30 runs/minute burst 10, ≤3 active; per target address 20 runs/minute, ≤3 active; ≤10 active overall; ≤2 active per agent; ≤5,000 events per run; ≤64 KB per agent post; ≤1,024 ports per run.
- Deadlines, exactly: dns 15 s, ping 2 min, traceroute 3 min, tcp 5 min. A queued run not picked up in 30 s fails; a running run past deadline + 15 s times out; an agent is "ready" if it polled in the last 60 s; long-poll wait 25 s; recorder flush every 250 ms; SSE keep-alive every 15 s.
- Settings keys: `net_tools_allowlist` (JSON array of strings, default `[]`), `net_tools_server_enabled` (default true), `net_tools_retention_days` (default 30, range 1–365).
- Exact user-facing strings (copy verbatim):
  - `ICMP isn't available here (needs root or NET_RAW)`
  - `the agent didn't pick up the job (offline?)`
  - `too much output`
  - `No targets are allowed yet. An admin adds subnets and hosts in Settings → Network tools`
  - `Admins always can`
  - `This agent's version can't run tools — update it`
- Error shape for tool endpoints: `{"success": false, "error": {"code": "<code>", "message": "<text>"}}` via `respondToolError` (which aborts). Codes: `forbidden`, `invalid_params`, `target_not_allowed`, `resolve_failed`, `ipv6_unsupported`, `vantage_not_ready`, `limit`, `not_found`, `conflict`, `invalid_allowlist`, `tools_disabled`.
- Frontend: colours from `src/utils/colors.ts` or existing class maps (no hand-written hex, no `dark:`); component files export only components (helpers in `src/utils/` or hooks); no `window.confirm`/`alert`; guard async results against changed inputs; switching tool or run closes the open `EventSource`.
- Backend checks (from `backend/`, Go is run through the project's Docker helper if not installed locally): `go vet ./...`, `go test ./...`, `./scripts/test-db.sh` (DB tests; `./scripts/test-db.sh -run <Name> -v` for one). Windows cross-check: `GOOS=windows go vet ./internal/nettools/... ./cmd/agent/...`. Frontend gate from the repo root: `docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"`.
- DB test functions are named `TestDB…` and start with `db := testdb.Open(t)` (package `internal/testdb`; it skips without `SENTINEL_TEST_DATABASE_URL`).
- Commits end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **An agent vanishes after claiming a run.** The run must end `timed_out` at deadline + 15 s, an `end` frame must reach any open stream, and the browser must stop showing "Running". Pinned in Task 10 (sweeper) and Task 11 (stream ends when the sweeper finishes the run).
2. **A second tab, or a reconnect, mid-run.** The stream replays stored events after `Last-Event-ID`, then follows live events, with no gap and no duplicate even when events land between the replay query and the first live message. Pinned in Task 11.
3. **A hostname allowlist entry whose name resolves to an always-blocked address** (metadata, multicast, `0.0.0.0/8`, broadcast) — the run is refused with `target_not_allowed`, whatever the list says. Pinned in Task 1 (`Allows`) and Task 8 (`Create`).
4. **Ping parameters that cannot finish inside the 2-minute deadline** (e.g. count 100 × interval 5 s) are refused by `Normalize` with `invalid_params`, rather than timing out half-way. Pinned in Task 1.
5. **A user cancels while an agent is mid-run.** The run is `cancelled` at once; the agent's next events post gets `cancel: true` and stops; its late finish never turns `cancelled` back into `done`. Pinned in Task 10 (service) and Task 13 (agent loop).

---

## Rulings settled while planning

The spec left these open or the code forced them; the plan decides them:

1. **Ping must fit its deadline.** `Normalize` refuses ping when `count × interval_ms + timeout_ms > 115000` ("count × interval is too long: at most 115 seconds"), so a run never outlives the 2-minute deadline.
2. **Traceroute hop rows.** Hops beyond the destination are dropped once the destination TTL is known. When the destination is not reached, the hop rows run from TTL 1 to `min(max_hops, last TTL that ever answered + 1)` (at least 1), so the table shows where the path stops without 30 rows of timeouts.
3. **Traceroute rounds** start at least 1 s apart; a round ends when every probe of the round has answered or timed out.
4. **`round_done` carries the cumulative hop statistics** (`[]HopStats`), so the browser renders the MTR table directly and never re-implements the maths.
5. **Jitter** is the mean absolute difference between consecutive reply RTTs (nil with fewer than 2 replies). Stdev is the population standard deviation.
6. **TCP scans emit a `start` event** with the number of ports, for the progress bar.
7. **DNS** sends no EDNS0; a truncated UDP answer is retried over TCP. The OPT pseudo-record never appears in the sections.
8. **Cancel wins.** Cancelling marks the run `cancelled` at once. Any later finish (local goroutine or agent) only fills `summary` if it is still NULL and never changes the status. An agent posting events for a `cancelled` run gets `200 {"cancel": true}` (not 409).
9. **`agents.tools_local` is nullable** (spec amendment 1): the heartbeat field is `tools_local` (`*bool`); absent means NULL.
10. **`tool_runs.target_ip` is TEXT and `tool_runs.agent_ref` snapshots the readable agent id** (spec amendments 2–3). The run's JSON field `agent_id` is `agent_ref`; the UUID column is not serialised.
11. **The per-user rate limit** is the existing `api.NewRateLimiter(30, time.Minute, 10).Middleware("tool-runs", ByUser)`, so its 429 carries the plain string error, not the code `limit`; the frontend treats any 429 as a limit.
12. **`RequireNetTools`** answers 403 with code `forbidden` in the coded shape, and stores the DB-fresh admin flag in the context key `net_tools_is_admin` for the handlers.
13. **Shutdown.** The HTTP server gets a `BaseContext` that is cancelled just before `server.Shutdown`, so open SSE streams and long-polls end at once instead of holding shutdown for its whole timeout.
14. **Audit actor for sweeper finishes** is `services.Actor{Username: "system"}`.
15. **The tool-run package is `internal/toolruns`** (it imports `services`; `services` never imports it), as phase 4 did with `internal/dashboards`.
16. **`GET /tools/runs/:id` returns `{run, events}`; `GET /tools/runs` returns the run list with an `X-Total-Count` header; `GET /tools/vantages` returns `{vantages, allowlist_empty}`** (spec amendment 5).
17. **Hop names** are looked up once per distinct hop address, in the background, with a 1 s timeout; a failed lookup emits nothing.
18. **The Tools page URL** carries the form: `/tools?tool=<tool>&target=<target>&from=sentinel|agent:<agent_id>`; "Run again" adds `&params=<URL-encoded JSON>`. Entry points use the same contract.

## File structure

Backend — new:
- `backend/internal/nettools/nettools.go` — `Tool`, `Spec`, `Params`, `Event`, `Emitter`, event and summary types, `Deadline`.
- `backend/internal/nettools/params.go` — limits, `ParamError`, `Normalize`.
- `backend/internal/nettools/ports.go` — `ParsePorts`, `CommonPorts`, `ServiceName`.
- `backend/internal/nettools/allowlist.go` — `Allowlist`, `ParseAllowlist`, `EntryError`, `AlwaysBlocked`, `ParseAddressList`, `InNets`.
- `backend/internal/nettools/icmp.go` — `Prober`, `EchoReply`, `ReplyKind`, `ErrNoReply`, `ErrICMPUnavailable`, `parseICMPv4`.
- `backend/internal/nettools/icmp_unix.go` (`//go:build !windows`) — raw ICMP prober with unprivileged fallback.
- `backend/internal/nettools/icmp_windows.go` — `IcmpSendEcho` prober.
- `backend/internal/nettools/ping.go`, `traceroute.go` — the two ICMP tools.
- `backend/internal/nettools/dns.go`, `dns_system_unix.go`, `dns_system_windows.go` — DNS lookup and the system resolver address.
- `backend/internal/nettools/tcp.go`, `tcp_refused_unix.go`, `tcp_refused_windows.go` — port scan.
- `backend/internal/nettools/runner.go` — `Runner` and `Run` dispatch.
- `backend/internal/stream/sse.go`, `hub.go` — SSE writer and hub.
- `backend/internal/toolruns/` — `settings.go`, `service.go` (types, `New`, `Create`, `Vantages`), `limits.go`, `polls.go`, `recorder.go`, `local.go`, `query.go` (`Get`, `Events`, `List`, `Cancel`), `dispatch.go` (`NextJob`, `AgentEvents`, `AgentFinish`), `sweeper.go`.
- `backend/internal/models/tool_run.go` — `ToolRun`, `ToolRunEvent`, status and vantage constants.
- `backend/internal/api/net_tools_access.go` — `RequireNetTools`, `respondToolError`, `requesterFrom`.
- `backend/internal/api/tool_runs_handler.go` — user routes and the SSE handler.
- `backend/internal/api/agent_jobs_handler.go` — agent job routes.
- `backend/internal/api/net_tools_admin_handler.go` — settings, grant, agent toggle.
- `backend/migrations/059_network_tools.sql`.
- `backend/cmd/agent/jobs.go` — the agent's job loop.

Backend — modified:
- `backend/internal/models/user.go` (`NetTools`), `agent.go` (`ToolsEnabled`, `ToolsLocal`), `audit.go` (actions), `setting.go` (keys).
- `backend/internal/services/agent_service.go` (`AgentSystemInfo.ToolsLocal`, `Heartbeat`, `SetToolsEnabled`), `auth_service.go` (`SetNetTools`).
- `backend/internal/api/agent_handler.go` (heartbeat `tools_local`), `auth_handler.go` (`/me` gains `net_tools`), `agent_install_scripts.go` (`ENABLE_TOOLS`, `TOOLS_ALLOWED_TARGETS`).
- `backend/cmd/sentinel/main.go` (wiring, `BaseContext`), `backend/cmd/agent/main.go` (config, heartbeat, start the job loop), `backend/Dockerfile` (`AGENT_VERSION` default `1.1.0`).

Frontend — new:
- `frontend/src/types/netTools.ts`, `frontend/src/hooks/useNetTools.ts`, `frontend/src/hooks/useRunStream.ts`, `frontend/src/utils/netTools.ts`.
- `frontend/src/pages/NetworkTools.tsx`, `frontend/src/pages/ToolRunDetail.tsx`.
- `frontend/src/components/netTools/` — `ToolForm.tsx`, `VantageSelect.tsx`, `TargetInput.tsx`, `RunStatusLine.tsx`, `RunResult.tsx`, `PingResult.tsx`, `TracerouteResult.tsx`, `DNSResult.tsx`, `PortsResult.tsx`, `RunHistory.tsx`.
- `frontend/src/components/settings/NetToolsSettings.tsx`.

Frontend — modified: `services/api.ts` (export the base URL), `context/AuthContext.tsx` (`net_tools`), `App.tsx`, `components/Layout.tsx`, `pages/Settings.tsx`, `pages/AdminUsers.tsx`, the agent edit form (`components/EditServerAgentModal.tsx` / `AgentSettingsFields.tsx`), `components/AddServerAgentModal.tsx`, `pages/ServerDetail.tsx`, `pages/network/DeviceDetail.tsx`, `pages/MonitorDetail.tsx`, `types/index.ts` (agent and user fields).

Docs: `docs/superpowers/STATUS.md`, `docs/superpowers/plans/2026-10-05-tools-s1-followups.md`.

## Shared contract

Every task implements or consumes these exact names. A task's **Interfaces** block points here; where a task's text and this section differ, this section wins.

### `internal/nettools` (Go)

```go
package nettools

type Tool string

const (
	ToolPing       Tool = "ping"
	ToolTraceroute Tool = "traceroute"
	ToolDNS        Tool = "dns"
	ToolTCP        Tool = "tcp"
)

// Spec is one run: the tool, what it targets and its parameters.
type Spec struct {
	Tool Tool `json:"tool"`
	// Target is what the user typed: a host name or IPv4 address; for dns,
	// the name (or IPv4 address, for PTR) to look up.
	Target string `json:"target"`
	// TargetIP is the IPv4 address the tool contacts: the probed host for
	// ping, traceroute and tcp; the named DNS server for dns (empty when the
	// lookup goes through the system resolver). Set by the server after the
	// allowlist check; tools never resolve Target themselves.
	TargetIP string `json:"target_ip,omitempty"`
	Params   Params `json:"params"`
}

// Params holds every tool's parameters. Normalize fills the defaults and
// clears the fields the tool does not use.
type Params struct {
	Count      int    `json:"count,omitempty"`       // ping
	IntervalMS int    `json:"interval_ms,omitempty"` // ping
	Size       *int   `json:"size,omitempty"`        // ping payload bytes; 0 is valid
	MaxHops    int    `json:"max_hops,omitempty"`    // traceroute
	Rounds     int    `json:"rounds,omitempty"`      // traceroute
	TimeoutMS  int    `json:"timeout_ms,omitempty"`  // ping, traceroute, tcp (per probe)
	RecordType string `json:"record_type,omitempty"` // dns
	Server     string `json:"server,omitempty"`      // dns: "" = system resolver, else "a.b.c.d" or "a.b.c.d:port"
	Ports      string `json:"ports,omitempty"`       // tcp: "22,80,443,8000-8100" or "common"
}

// Limits and defaults (params.go).
const (
	PingCountDefault, PingCountMin, PingCountMax          = 5, 1, 100
	PingIntervalDefaultMS, PingIntervalMinMS, PingIntervalMaxMS = 1000, 200, 5000
	PingTimeoutDefaultMS, PingTimeoutMinMS, PingTimeoutMaxMS    = 2000, 500, 5000
	PingSizeDefault, PingSizeMax                          = 56, 1472
	PingMaxTotalMS                                        = 115000 // count × interval + timeout
	TraceMaxHopsDefault, TraceMaxHopsMin, TraceMaxHopsMax = 30, 1, 30
	TraceRoundsDefault, TraceRoundsMin, TraceRoundsMax    = 5, 1, 10
	TraceTimeoutDefaultMS, TraceTimeoutMinMS, TraceTimeoutMaxMS = 1000, 500, 3000
	TCPTimeoutDefaultMS, TCPTimeoutMinMS, TCPTimeoutMaxMS = 1500, 500, 5000
	TCPConcurrency                                        = 50
	MaxPorts                                              = 1024
	MaxTargetLength                                       = 253
	DNSTimeout                                            = 5 * time.Second // typed time.Duration in code
)

// DNSRecordTypes in display order; RecordType default "A".
var DNSRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "NS", "TXT", "SOA", "SRV", "PTR", "CAA"}

// ParamError is a parameter Normalize refused.
type ParamError struct {
	Field   string
	Message string
}

func (e *ParamError) Error() string // Field + ": " + Message

// Normalize checks a spec against the limits and returns it with defaults
// filled in and unused parameters cleared. It does not resolve anything or
// consult the allowlist. Errors are *ParamError.
func Normalize(s Spec) (Spec, error)

// Deadline is the tool's hard limit: dns 15s, ping 2m, traceroute 3m, tcp 5m.
func Deadline(t Tool) time.Duration

// ---- ports.go
var CommonPorts []int                       // the "common" preset, ascending, ~100 ports
func ParsePorts(spec string) ([]int, error) // "common" or list/ranges; ascending, de-duplicated, 1..65535, ≤ MaxPorts
func ServiceName(port int) string           // "ssh", "rdp", … or ""

// ---- allowlist.go
type EntryError struct {
	Entry   string `json:"entry"`
	Message string `json:"message"`
}
type Allowlist struct{ /* unexported */ }
// ParseAllowlist accepts IPv4 addresses, IPv4 CIDRs (no broader than /8),
// exact host names and "*.domain" wildcards. Blank entries are ignored.
// Every bad entry is reported; the returned list holds the good ones.
func ParseAllowlist(entries []string) (*Allowlist, []EntryError)
func (a *Allowlist) Empty() bool
// Allows reports whether a run may contact ip for the typed target: the
// target matches a host-name entry, or ip lies in an address/CIDR entry —
// and ip is never AlwaysBlocked. ip may be nil only for a pure host-name match.
func (a *Allowlist) Allows(target string, ip net.IP) bool
// AlwaysBlocked: 169.254.169.254/32, 169.254.170.2/32, 100.100.100.200/32,
// 0.0.0.0/8, 224.0.0.0/4 and 255.255.255.255/32; nil and non-IPv4 too.
func AlwaysBlocked(ip net.IP) bool
// ParseAddressList parses the agent's TOOLS_ALLOWED_TARGETS: comma-separated
// IPv4 addresses and CIDRs ("" → nil, nil).
func ParseAddressList(s string) ([]*net.IPNet, error)
func InNets(ip net.IP, nets []*net.IPNet) bool

// ---- events (nettools.go)
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}
type Emitter func(Event)

const (
	EventReply     = "reply"      // ping: PingReply
	EventTimeout   = "timeout"    // ping: PingTimeout
	EventError     = "error"      // ping: PingError
	EventHop       = "hop"        // traceroute: HopProbe
	EventRoundDone = "round_done" // traceroute: RoundDone
	EventHopName   = "hop_name"   // traceroute: HopName
	EventAnswer    = "answer"     // dns: DNSAnswer
	EventStart     = "start"      // tcp: ScanStart
	EventPort      = "port"       // tcp: PortResult
)

type PingReply struct {
	Seq   int     `json:"seq"`
	RTTMS float64 `json:"rtt_ms"`
	TTL   int     `json:"ttl"`
	From  string  `json:"from"`
}
type PingTimeout struct {
	Seq int `json:"seq"`
}
type PingError struct {
	Seq     int    `json:"seq"`
	Message string `json:"message"`
}
type PingSummary struct {
	Sent     int      `json:"sent"`
	Received int      `json:"received"`
	LossPct  float64  `json:"loss_pct"`
	MinMS    *float64 `json:"min_ms"`
	AvgMS    *float64 `json:"avg_ms"`
	MaxMS    *float64 `json:"max_ms"`
	JitterMS *float64 `json:"jitter_ms"`
}

type HopProbe struct {
	Round   int      `json:"round"`
	TTL     int      `json:"ttl"`
	Addr    string   `json:"addr,omitempty"` // empty on timeout
	RTTMS   *float64 `json:"rtt_ms,omitempty"`
	Reached bool     `json:"reached"`
}
type HopStats struct {
	TTL      int      `json:"ttl"`
	Addrs    []string `json:"addrs"`
	Name     string   `json:"name,omitempty"`
	Sent     int      `json:"sent"`
	Received int      `json:"received"`
	LossPct  float64  `json:"loss_pct"`
	LastMS   *float64 `json:"last_ms"`
	AvgMS    *float64 `json:"avg_ms"`
	BestMS   *float64 `json:"best_ms"`
	WorstMS  *float64 `json:"worst_ms"`
	StdevMS  *float64 `json:"stdev_ms"`
}
type RoundDone struct {
	Round int        `json:"round"`
	Hops  []HopStats `json:"hops"` // cumulative after this round
}
type HopName struct {
	TTL  int    `json:"ttl"`
	Addr string `json:"addr"`
	Name string `json:"name"`
}
type TraceSummary struct {
	Reached  bool       `json:"reached"`
	HopCount int        `json:"hop_count"`
	Hops     []HopStats `json:"hops"`
}

type DNSRecord struct {
	Name string `json:"name"`
	Type string `json:"type"`
	TTL  uint32 `json:"ttl"`
	Data string `json:"data"`
}
type DNSAnswer struct {
	Server        string      `json:"server"` // ip:port asked
	RCode         string      `json:"rcode"`  // "NOERROR", "NXDOMAIN", …
	Authoritative bool        `json:"authoritative"`
	Truncated     bool        `json:"truncated"` // the UDP answer was truncated
	TCP           bool        `json:"tcp"`       // the answer shown came over TCP
	RTTMS         float64     `json:"rtt_ms"`
	Answer        []DNSRecord `json:"answer"`
	Authority     []DNSRecord `json:"authority"`
	Additional    []DNSRecord `json:"additional"`
}
type DNSSummary struct {
	Server      string  `json:"server"`
	RCode       string  `json:"rcode"`
	AnswerCount int     `json:"answer_count"`
	RTTMS       float64 `json:"rtt_ms"`
}

const (
	PortOpen     = "open"
	PortClosed   = "closed"
	PortFiltered = "filtered"
)
type ScanStart struct {
	Total int `json:"total"`
}
type PortResult struct {
	Port    int      `json:"port"`
	State   string   `json:"state"`
	RTTMS   *float64 `json:"rtt_ms,omitempty"`
	Service string   `json:"service,omitempty"`
}
type TCPSummary struct {
	Total     int   `json:"total"`
	Open      int   `json:"open"`
	Closed    int   `json:"closed"`
	Filtered  int   `json:"filtered"`
	OpenPorts []int `json:"open_ports"`
}

// ---- icmp.go
type ReplyKind int

const (
	ReplyEcho ReplyKind = iota + 1
	ReplyTimeExceeded
	ReplyUnreachable
)

type EchoReply struct {
	Kind ReplyKind
	From net.IP
	RTT  time.Duration
	TTL  int // the reply's IP TTL, 0 when unknown
	Code int // ICMP code (unreachable)
}

var (
	ErrNoReply         = errors.New("no reply")
	ErrICMPUnavailable = errors.New("ICMP isn't available here (needs root or NET_RAW)")
)

// Prober sends ICMP echo requests. Safe for concurrent use.
type Prober interface {
	// Echo sends one echo request to dst with the given IP TTL and payload
	// size, and waits up to timeout for the matching reply: an echo reply, or
	// a time-exceeded/unreachable message quoting this request.
	// ErrNoReply when nothing matching arrives in time.
	Echo(ctx context.Context, dst net.IP, ttl, size int, timeout time.Duration) (EchoReply, error)
	// CanTrace reports whether time-exceeded messages are visible (raw ICMP
	// or the Windows ICMP API; not the unprivileged socket).
	CanTrace() bool
	Close() error
}

// NewProber opens the platform prober; ErrICMPUnavailable if it cannot.
func NewProber() (Prober, error)

// ---- dns_system_*.go
// SystemResolver returns "ip:port" of the host's first IPv4 DNS server.
func SystemResolver() (string, error)

// ---- runner.go
// Runner runs tools. Nil fields use the system's ICMP, dialer and resolvers.
type Runner struct {
	NewProber func() (Prober, error)
	Dial      func(ctx context.Context, network, addr string) (net.Conn, error)
	SystemDNS func() (string, error)
	LookupPTR func(ctx context.Context, addr string) (string, error)
}

// Run executes a normalized spec, calling emit for each event as it
// happens, and returns the tool's summary (PingSummary, TraceSummary,
// DNSSummary or TCPSummary). When ctx ends first it returns the summary of
// what completed and ctx.Err().
func (r *Runner) Run(ctx context.Context, s Spec, emit Emitter) (any, error)
```

### `internal/stream` (Go)

```go
package stream

const KeepAlive = 15 * time.Second

// Message is one SSE frame. Data is already-encoded JSON.
type Message struct {
	ID    string // "" = no id line
	Event string // "" = default "message"
	Data  []byte
}

type Writer struct{ /* unexported */ }

// NewWriter sets Content-Type: text/event-stream, Cache-Control: no-cache,
// Connection: keep-alive and X-Accel-Buffering: no, writes 200 and flushes.
// Error when w is not an http.Flusher.
func NewWriter(w http.ResponseWriter) (*Writer, error)
func (w *Writer) Send(m Message) error     // id/event/data lines + blank line, flushed
func (w *Writer) Comment(text string) error // ": text\n\n", flushed

type Hub struct{ /* unexported */ }
func NewHub(buffer int) *Hub
func (h *Hub) Subscribe(key string) *Subscription
// Publish never blocks: a subscriber whose buffer is full is dropped (its
// channel closed, Dropped() true).
func (h *Hub) Publish(key string, m Message)

type Subscription struct{ /* unexported */ }
func (s *Subscription) C() <-chan Message // closed when dropped or closed
func (s *Subscription) Dropped() bool
func (s *Subscription) Close()            // idempotent
```

### `internal/models` (Go)

```go
// tool_run.go
const (
	ToolRunQueued      = "queued"
	ToolRunRunning     = "running"
	ToolRunDone        = "done"
	ToolRunFailed      = "failed"
	ToolRunRefused     = "refused"
	ToolRunCancelled   = "cancelled"
	ToolRunTimedOut    = "timed_out"
	ToolRunInterrupted = "interrupted"

	VantageSentinel = "sentinel"
	VantageAgent    = "agent"
)

// ToolRunActive reports queued or running.
func ToolRunActive(status string) bool

type ToolRun struct {
	ID          uuid.UUID  `json:"id" gorm:"column:id;type:uuid;primaryKey"`
	Tool        string     `json:"tool" gorm:"column:tool"`
	Status      string     `json:"status" gorm:"column:status"`
	UserID      *uuid.UUID `json:"user_id" gorm:"column:user_id"`
	Username    string     `json:"username" gorm:"column:username"`
	VantageKind string     `json:"vantage_kind" gorm:"column:vantage_kind"`
	AgentUUID   *uuid.UUID `json:"-" gorm:"column:agent_id"`
	AgentRef    *string    `json:"agent_id" gorm:"column:agent_ref"`
	VantageName string     `json:"vantage_name" gorm:"column:vantage_name"`
	Target      string     `json:"target" gorm:"column:target"`
	TargetIP    *string    `json:"target_ip" gorm:"column:target_ip"`
	Params      RawJSON    `json:"params" gorm:"column:params;type:jsonb"`
	Summary     *RawJSON   `json:"summary" gorm:"column:summary;type:jsonb"`
	Error       *string    `json:"error" gorm:"column:error"`
	EventCount  int        `json:"event_count" gorm:"column:event_count"`
	CreatedAt   time.Time  `json:"created_at" gorm:"column:created_at"`
	StartedAt   *time.Time `json:"started_at" gorm:"column:started_at"`
	FinishedAt  *time.Time `json:"finished_at" gorm:"column:finished_at"`
	Deadline    time.Time  `json:"deadline" gorm:"column:deadline"`
}
func (ToolRun) TableName() string { return "tool_runs" }

type ToolRunEvent struct {
	RunID uuid.UUID `json:"-" gorm:"column:run_id;type:uuid;primaryKey"`
	Seq   int       `json:"seq" gorm:"column:seq;primaryKey"`
	At    time.Time `json:"at" gorm:"column:at"`
	Type  string    `json:"type" gorm:"column:type"`
	Data  RawJSON   `json:"data" gorm:"column:data;type:jsonb"`
}
func (ToolRunEvent) TableName() string { return "tool_run_events" }

// user.go: User gains
NetTools bool `json:"net_tools" gorm:"column:net_tools"`

// agent.go: Agent gains
ToolsEnabled bool  `json:"tools_enabled" gorm:"column:tools_enabled"`
ToolsLocal   *bool `json:"tools_local" gorm:"column:tools_local"` // NULL = never reported (too old)

// audit.go: actions and resources
ActionToolRunStarted           = "tool_run_started"
ActionToolRunFinished          = "tool_run_finished"
ActionToolRunRefused           = "tool_run_refused"
ActionNetToolsSettingsUpdated  = "net_tools_settings_updated"
ActionUserNetToolsChanged      = "user_net_tools_changed"
ActionAgentToolsChanged        = "agent_tools_changed"
ResourceToolRun                = "tool_run"
// "settings", "user" and "agent" resource types: reuse existing constants
// when present, else add ResourceSettings = "settings", ResourceUser = "user",
// ResourceAgent = "agent".

// setting.go
SettingNetToolsAllowlist     = "net_tools_allowlist"
SettingNetToolsServerEnabled = "net_tools_server_enabled"
SettingNetToolsRetentionDays = "net_tools_retention_days"
```

### `internal/services` additions (Go)

```go
// AgentSystemInfo gains:
ToolsLocal *bool // nil → stored as NULL
// AgentService:
func (s *AgentService) SetToolsEnabled(ctx context.Context, agentID string, enabled bool) (*models.Agent, error) // ErrAgentNotFound
// AuthService:
func (s *AuthService) SetNetTools(ctx context.Context, userID uuid.UUID, enabled bool) (*models.User, error)
```

### `internal/toolruns` (Go)

```go
package toolruns

const (
	UserRatePerMinute   = 30
	UserRateBurst       = 10
	TargetRatePerMinute = 20
	MaxActivePerUser    = 3
	MaxActivePerTarget  = 3
	MaxActiveOverall    = 10
	MaxActivePerAgent   = 2
	MaxEventsPerRun     = 5000
	MaxAgentPostBytes   = 64 << 10
	PickupTimeout       = 30 * time.Second
	OverdueGrace        = 15 * time.Second
	ReadyWindow         = 60 * time.Second
	LongPollWait        = 25 * time.Second
	FlushEvery          = 250 * time.Millisecond
	SweepEvery          = 5 * time.Second
	DefaultRetentionDays, MinRetentionDays, MaxRetentionDays = 30, 1, 365
)

// ---- settings.go
type Settings struct {
	Allowlist     []string `json:"allowlist"`
	ServerEnabled bool     `json:"server_enabled"`
	RetentionDays int      `json:"retention_days"`
}
func LoadSettings(ctx context.Context, ss *services.SettingsService) Settings
// SaveSettings validates and stores s (entries trimmed, blanks dropped,
// de-duplicated, host names lower-cased). Bad entries → *SettingsError and
// nothing is saved; retention outside 1–365 → *SettingsError with no Entries.
func SaveSettings(ctx context.Context, ss *services.SettingsService, s Settings) (Settings, error)
type SettingsError struct {
	Message string
	Entries []nettools.EntryError
}
func (e *SettingsError) Error() string

// ---- service.go
type AuditRecorder interface {
	Record(ctx context.Context, actor services.Actor, action, resourceType string, resourceID *uuid.UUID, changes models.AuditChanges)
}
type Deps struct {
	DB       *gorm.DB
	Settings *services.SettingsService
	Audit    AuditRecorder
	Hub      *stream.Hub
	Runner   *nettools.Runner                                      // nil → &nettools.Runner{}
	Resolve  func(ctx context.Context, host string) ([]net.IP, error) // nil → net.DefaultResolver.LookupIP(ctx, "ip", host)
	Now      func() time.Time                                       // nil → time.Now().UTC()
}
type Service struct{ /* unexported */ }
func New(d Deps) *Service

type Requester struct {
	UserID   uuid.UUID
	Username string
	IsAdmin  bool
	IP       string
}
type Vantage struct {
	Kind    string `json:"kind"`               // models.VantageSentinel | models.VantageAgent
	AgentID string `json:"agent_id,omitempty"` // readable agent id
}
type CreateRequest struct {
	Tool    nettools.Tool   `json:"tool"`
	Vantage Vantage         `json:"vantage"`
	Target  string          `json:"target"`
	Params  nettools.Params `json:"params"`
}

// Refusal is a failure the user sees; the API writes it with respondToolError.
type Refusal struct {
	Status  int
	Code    string
	Message string
}
func (r *Refusal) Error() string { return r.Message }

const (
	CodeForbidden        = "forbidden"
	CodeInvalidParams    = "invalid_params"
	CodeTargetNotAllowed = "target_not_allowed"
	CodeResolveFailed    = "resolve_failed"
	CodeIPv6Unsupported  = "ipv6_unsupported"
	CodeVantageNotReady  = "vantage_not_ready"
	CodeLimit            = "limit"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
)

// Create validates, applies the guardrails, inserts the run and starts it
// (locally) or queues it (agent). User-facing failures are *Refusal.
func (s *Service) Create(ctx context.Context, who Requester, req CreateRequest) (*models.ToolRun, error)

type VantageView struct {
	Kind    string `json:"kind"`
	AgentID string `json:"agent_id,omitempty"`
	Name    string `json:"name"`
	Ready   bool   `json:"ready"`
	Reason  string `json:"reason,omitempty"`
}
const (
	ReasonServerDisabled     = "server_disabled"
	ReasonAgentTooOld        = "agent_too_old"
	ReasonToolsOff           = "tools_off"
	ReasonNotEnabledOnServer = "not_enabled_on_server"
	ReasonOffline            = "offline"
)
// Vantages: Sentinel first, then agents by name. Agent reason precedence:
// too old (tools_local NULL) → tools off (tools_enabled false) → not enabled
// on the server (tools_local false) → offline (no poll within ReadyWindow).
func (s *Service) Vantages(ctx context.Context) ([]VantageView, error)
func (s *Service) AllowlistEmpty(ctx context.Context) bool

// ---- query.go
var ErrRunNotFound = errors.New("tool run not found")
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*models.ToolRun, error)
func (s *Service) Events(ctx context.Context, id uuid.UUID, afterSeq int) ([]models.ToolRunEvent, error) // seq ascending
type ListFilter struct {
	Tool    string
	UserID  *uuid.UUID
	AgentID string // readable agent id (agent_ref)
	Target  string // exact match on target or target_ip
	Status  string
	Limit   int // default 50, max 200
	Offset  int
}
func (s *Service) List(ctx context.Context, f ListFilter) ([]models.ToolRun, int64, error) // newest first
// Cancel: 404 not_found; 403 forbidden (not the owner and not admin);
// 409 conflict (already finished). Marks cancelled at once (ruling 8).
func (s *Service) Cancel(ctx context.Context, who Requester, id uuid.UUID) (*models.ToolRun, error)

// ---- dispatch.go (agent side)
type Job struct {
	RunID    uuid.UUID     `json:"run_id"`
	Spec     nettools.Spec `json:"spec"`
	Deadline time.Time     `json:"deadline"`
}
var (
	ErrToolsDisabled = errors.New("tools are off for this agent")
	ErrRunNotActive  = errors.New("that run is not running on this agent")
	ErrTooMuchOutput = errors.New("too much output")
)
// NextJob claims the oldest queued run for agent, waiting up to wait for one
// to be queued. (nil, nil) when none arrived. ErrToolsDisabled when the
// agent's tools_enabled is false. Every call records a poll for readiness.
func (s *Service) NextJob(ctx context.Context, agent *models.Agent, wait time.Duration) (*Job, error)
type AgentEvent struct {
	Seq  int             `json:"seq"`
	At   time.Time       `json:"at"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}
// AgentEvents stores a batch (duplicates by seq ignored) and publishes it.
// cancel=true when the run is cancelled. ErrRunNotActive when the run is
// not this agent's or is final for another reason. ErrTooMuchOutput when the
// batch would pass MaxEventsPerRun (the run is then finished as failed).
func (s *Service) AgentEvents(ctx context.Context, agent *models.Agent, runID uuid.UUID, events []AgentEvent) (cancel bool, err error)
type AgentFinish struct {
	Status  string          `json:"status"` // done | failed | refused
	Summary json.RawMessage `json:"summary"`
	Error   string          `json:"error"`
}
func (s *Service) AgentFinish(ctx context.Context, agent *models.Agent, runID uuid.UUID, f AgentFinish) error

// ---- sweeper.go
func (s *Service) InterruptStale(ctx context.Context) (int64, error) // startup: active → interrupted
func (s *Service) Sweep(ctx context.Context) error                   // pickup timeouts and overdue runs
func (s *Service) Prune(ctx context.Context) (int64, error)          // finished runs older than retention
// Start: InterruptStale once, then Sweep every SweepEvery and Prune once a
// minute after start and every 24 h, until ctx ends.
func (s *Service) Start(ctx context.Context)
```

**Hub keys and frames.** The hub key is the run id string. Frames:
- `event` — `ID` = seq, `Data` = the `models.ToolRunEvent` JSON (`{"seq","at","type","data"}`).
- `status` — `Data` = `{"status":"running"}` (published when a run starts running).
- `end` — `Data` = the final `models.ToolRun` JSON. Published once, when a run reaches a final status.

### `internal/api` (Go)

```go
// net_tools_access.go
const netToolsAdminKey = "net_tools_is_admin"
func respondToolError(c *gin.Context, status int, code, message string) // aborts; coded shape
func RequireNetTools(users adminChecker) gin.HandlerFunc               // admin or user.NetTools, re-read from DB
func requesterFrom(c *gin.Context) toolruns.Requester                  // uses netToolsAdminKey
func writeRefusal(c *gin.Context, err error) bool                     // *toolruns.Refusal → respondToolError; true if handled

// tool_runs_handler.go — all behind AuthMiddleware (v1) and RequireNetTools
func RegisterToolRoutes(rg *gin.RouterGroup, runs *toolruns.Service, hub *stream.Hub, users adminChecker, audit auditRecorder)
//   GET  /tools/vantages            → {vantages, allowlist_empty}
//   POST /tools/runs                → 201 ToolRun (rate limit 30/min burst 10 ByUser)
//   GET  /tools/runs                → []ToolRun + X-Total-Count; query tool, user_id, agent_id, target, status, mine=1, limit, offset
//   GET  /tools/runs/:id            → {run, events}
//   GET  /tools/runs/:id/events     → SSE
//   POST /tools/runs/:id/cancel     → ToolRun

// agent_jobs_handler.go — RequireAgentToken + requireOwnAgent
func RegisterAgentJobRoutes(router *gin.Engine, agents *services.AgentService, runs *toolruns.Service)
//   GET  /api/v1/agents/:agent_id/jobs/next           → 200 Job | 204 | 403 tools_disabled
//   POST /api/v1/agents/:agent_id/jobs/:run_id/events → {"cancel": bool} | 409 conflict | 413
//   POST /api/v1/agents/:agent_id/jobs/:run_id/finish → {"ok": true} | 409 conflict

// net_tools_admin_handler.go — mounted on the admin (RequireAdmin) group
func RegisterNetToolsAdminRoutes(admin *gin.RouterGroup, settings *services.SettingsService, auth *services.AuthService, agents *services.AgentService, audit auditRecorder)
//   GET   /settings/net-tools       → Settings
//   PUT   /settings/net-tools       → Settings | 422 {error:{code:"invalid_allowlist"}, entries:[{entry,message}]}
//   PATCH /users/:id/net-tools      {enabled} → {id, net_tools}
//   PUT   /agents/:agent_id/tools   {enabled} → Agent (token hidden)
```

Agent wire format: events post body `{"events": [AgentEvent…]}`; finish body `AgentFinish`; the heartbeat body gains `"tools_local": true|false`.

### Frontend (TypeScript)

```ts
// types/netTools.ts
export type NetTool = 'ping' | 'traceroute' | 'dns' | 'tcp'
export type ToolRunStatus = 'queued' | 'running' | 'done' | 'failed' | 'refused' | 'cancelled' | 'timed_out' | 'interrupted'
export type VantageKind = 'sentinel' | 'agent'
export type VantageReason = 'server_disabled' | 'agent_too_old' | 'tools_off' | 'not_enabled_on_server' | 'offline'
export interface ToolParams {
  count?: number; interval_ms?: number; size?: number
  max_hops?: number; rounds?: number; timeout_ms?: number
  record_type?: string; server?: string; ports?: string
}
export interface Vantage { kind: VantageKind; agent_id?: string }
export interface VantageView { kind: VantageKind; agent_id?: string; name: string; ready: boolean; reason?: VantageReason }
export interface VantagesResponse { vantages: VantageView[]; allowlist_empty: boolean }
export interface CreateToolRunRequest { tool: NetTool; vantage: Vantage; target: string; params: ToolParams }
export interface ToolRun {
  id: string; tool: NetTool; status: ToolRunStatus
  user_id: string | null; username: string
  vantage_kind: VantageKind; agent_id: string | null; vantage_name: string
  target: string; target_ip: string | null
  params: ToolParams
  summary: PingSummary | TraceSummary | DNSSummary | TCPSummary | null
  error: string | null; event_count: number
  created_at: string; started_at: string | null; finished_at: string | null; deadline: string
}
export interface ToolRunEvent { seq: number; at: string; type: string; data: unknown }
export interface ToolRunDetail { run: ToolRun; events: ToolRunEvent[] }
export interface PingReply { seq: number; rtt_ms: number; ttl: number; from: string }
export interface PingTimeout { seq: number }
export interface PingError { seq: number; message: string }
export interface PingSummary { sent: number; received: number; loss_pct: number; min_ms: number | null; avg_ms: number | null; max_ms: number | null; jitter_ms: number | null }
export interface HopStats { ttl: number; addrs: string[]; name?: string; sent: number; received: number; loss_pct: number; last_ms: number | null; avg_ms: number | null; best_ms: number | null; worst_ms: number | null; stdev_ms: number | null }
export interface RoundDone { round: number; hops: HopStats[] }
export interface HopName { ttl: number; addr: string; name: string }
export interface TraceSummary { reached: boolean; hop_count: number; hops: HopStats[] }
export interface DNSRecord { name: string; type: string; ttl: number; data: string }
export interface DNSAnswer { server: string; rcode: string; authoritative: boolean; truncated: boolean; tcp: boolean; rtt_ms: number; answer: DNSRecord[]; authority: DNSRecord[]; additional: DNSRecord[] }
export interface DNSSummary { server: string; rcode: string; answer_count: number; rtt_ms: number }
export type PortState = 'open' | 'closed' | 'filtered'
export interface ScanStart { total: number }
export interface PortResult { port: number; state: PortState; rtt_ms?: number; service?: string }
export interface TCPSummary { total: number; open: number; closed: number; filtered: number; open_ports: number[] }
export interface NetToolsSettings { allowlist: string[]; server_enabled: boolean; retention_days: number }
export interface AllowlistEntryError { entry: string; message: string }
export interface ToolRunFilter { tool?: NetTool; mine?: boolean; status?: ToolRunStatus; agent_id?: string; target?: string; limit?: number; offset?: number }

// services/api.ts gains
export const apiBaseURL: string // the same value axios uses ('/api/v1' by default)

// hooks/useNetTools.ts
export function useVantages(): { data: VantagesResponse | null; loading: boolean; error: string | null; reload: () => void }
export function useToolRuns(filter: ToolRunFilter): { runs: ToolRun[]; total: number; loading: boolean; error: string | null; reload: () => void }
export function useToolRun(id: string | undefined): { detail: ToolRunDetail | null; loading: boolean; error: string | null; reload: () => void }
export function createToolRun(req: CreateToolRunRequest): Promise<ToolRun>
export function cancelToolRun(id: string): Promise<ToolRun>
export function useNetToolsSettings(): { settings: NetToolsSettings | null; loading: boolean; error: string | null; save: (s: NetToolsSettings) => Promise<NetToolsSettings>; reload: () => void }
export function setUserNetTools(userId: string, enabled: boolean): Promise<void>
export function setAgentTools(agentId: string, enabled: boolean): Promise<void>

// hooks/useRunStream.ts — opens `${apiBaseURL}/tools/runs/${id}/events` with
// EventSource (withCredentials); merges events by seq; closes on `end`, on
// id change and on unmount.
export function useRunStream(runId: string | null): { events: ToolRunEvent[]; status: ToolRunStatus | null; run: ToolRun | null; error: string | null }

// utils/netTools.ts
export function canUseNetTools(user: { is_admin: boolean; net_tools?: boolean } | null | undefined): boolean
export function runResultLine(run: ToolRun): string // "0% loss, 2.1 ms avg" | "reached in 9 hops" | "3 open" | "NOERROR, 2 answers" | status text
export function statusLabel(status: ToolRunStatus): string
export function vantageReasonText(reason: VantageReason): string
export const TOOL_LABEL: Record<NetTool, string> // Ping, Traceroute, DNS, Ports
export function parseToolsQuery(search: URLSearchParams): { tool?: NetTool; target?: string; vantage?: Vantage; params?: ToolParams }
export function toolsQuery(v: { tool?: NetTool; target?: string; vantage?: Vantage; params?: ToolParams }): string // "?tool=…", '' when nothing set
export function isFinalStatus(s: ToolRunStatus): boolean

// context/AuthContext.tsx: CurrentUser gains `net_tools?: boolean`
```

### Contract changes made while writing the tasks

These were found while the tasks were written against the real code; the tasks already use them.

- **`nettools.Runner.Run` summaries:** a nil summary for any error other than the context's (and for `dns` also when the context ends); ping, traceroute and tcp return a typed partial summary when the context ends. Summaries are values, not pointers. `emit` is called from one goroutine per run. Consumers (Tasks 9, 13) treat a nil summary as "no summary".
- **Traceroute `hop` events** can, in round 1, include TTLs past the destination; the browser builds the MTR table only from `round_done` (and the final summary).
- **`Normalize` canonicalises** `Params.Server` (`ip` or `ip:port`) and sets `Ports` to `"common"` when empty. For a named DNS server, `Create` sets `TargetIP` to the server's address; the Runner uses it with the port from `Params.Server`.
- **`ParamError.Field`** is the JSON name (`count`, `interval_ms`, `timeout_ms`, `size`, `max_hops`, `rounds`, `record_type`, `server`, `ports`, `tool`, `target`).
- **`internal/toolruns`** also exports `(*Service).Subscribe(runID uuid.UUID) *stream.Subscription` and `ErrBadFinishStatus`; `service.go` is split into `service.go`, `create.go` and `vantages.go`. The batch insert `insertEvents` is defined in Task 9 and reused by Task 10. An agent's finish error text is clipped to 500 bytes. `AgentEvents` accepts an empty batch (the agent polls for a cancel every 250 ms).
- **`internal/stream`** also exports `ErrNoFlush` (returned by `NewWriter`).
- **`SetNetTools`** on an unknown user wraps `gorm.ErrRecordNotFound`.
- **`GET /api/v1/users`** sends `net_tools` to admin callers (Task 6); the Users page reads it there.
- **API:** the agent job routes answer in the standard `{"success": true, "data": …}` envelope (`data` is a `Job`, `{"cancel": bool}` or `{"ok": true}`); a too-large agent post or too much output answers 413 with code `limit`; internal errors use the existing `respondInternal`. On connecting to an active run, the SSE stream sends a `status` frame with the current status after the replay. `RegisterToolRoutes` takes the hub (to subscribe before replaying) and the audit recorder (to audit a `RequireNetTools` refusal of `POST /tools/runs` as `tool_run_refused`).
- **Frontend:** `Agent` gains `tools_enabled`/`tools_local` in `hooks/useAgents.ts` and `ManagedUser` gains `net_tools?` in `hooks/useUserManagement.ts` (`types/index.ts` is a barrel). `types/netTools.ts` adds `HopProbe`. The panel view models live in `utils/netToolViews.ts`; `utils/colors.ts` gains `chartChrome`; `utils/netTools.ts` exports extra helpers (form defaults and limits, `paramError`, `cleanParams`, `vantageKey`/`vantageFromKey`, `monitorHost`, `knownHosts`, `runTargetText`, `runStatusText`, status colour maps, `agentToolsStateText`, `allowlistErrors`, `lineErrors`, `formatMs`, `formatLoss`). `/tools/runs/:id` is routed in Task 17.

---

## Tasks

### Task 1: `nettools` foundations — specs, limits, ports, allowlist

This task creates the `internal/nettools` package with everything the server and the agent check before a run: the run types, the limits and `Normalize`, the port-list parser with the "common" preset, and the target allowlist. It has no network code yet.

Writer's decisions, all pinned by the tests:

- `Normalize` checks in this order: unknown tool (`Field: "tool"`), then the target (trimmed, non-empty, at most 253 characters, no whitespace; `Field: "target"`), then the tool's parameters. Zero means "use the default"; a negative value is out of range. `Field` is the parameter's JSON name.
- The ping total rule (ruling 1) uses `count × interval_ms + timeout_ms > 115000`, so exactly 115,000 ms is allowed.
- DNS: `Server` is written back in canonical form (`"192.0.2.53"` or `"192.0.2.53:5353"`); `"a.b.c.d:"`, IPv6 and IPv4-mapped IPv6 are refused. A PTR of a name is allowed as typed.
- TCP: `""` and `"common"` in any case become `"common"`; any other list is kept as typed (trimmed) and validated by `ParsePorts`. `ParsePorts` skips empty items (`"22,,80,"`) and refuses an empty list ("no ports given").
- Allowlist host names: letters, digits and hyphens; labels of 1–63 characters that do not start or end with a hyphen; at most 253 characters; and a last label that is not all digits, so `10.0.0.256` is refused rather than taken for a host name.
- `AlwaysBlocked` is checked before any match, so a listed host name that resolves to a metadata, multicast, `0.0.0.0/8` or broadcast address is refused (Review Focus 3). See Contract change 1 for the extra metadata address.

**Files:**
- Create: `backend/internal/nettools/nettools.go`
- Create: `backend/internal/nettools/params.go`
- Create: `backend/internal/nettools/ports.go`
- Create: `backend/internal/nettools/allowlist.go`
- Test: `backend/internal/nettools/params_test.go`
- Test: `backend/internal/nettools/ports_test.go`
- Test: `backend/internal/nettools/allowlist_test.go`

**Interfaces:**
- Consumes: the standard library only.
- Produces (shared contract, `internal/nettools`):
  - `nettools.go`: `Tool` and `ToolPing`/`ToolTraceroute`/`ToolDNS`/`ToolTCP`; `Spec`; `Params`; `Event`; `Emitter`; the `Event…` type constants; `PingReply`, `PingTimeout`, `PingError`, `PingSummary`, `HopProbe`, `HopStats`, `RoundDone`, `HopName`, `TraceSummary`, `DNSRecord`, `DNSAnswer`, `DNSSummary`, `PortOpen`/`PortClosed`/`PortFiltered`, `ScanStart`, `PortResult`, `TCPSummary`; `func Deadline(t Tool) time.Duration` (0 for an unknown tool).
  - `params.go`: every limit constant from the contract (including `TCPConcurrency`, `MaxPorts`, `MaxTargetLength`, `DNSTimeout`); `var DNSRecordTypes []string`; `type ParamError struct{ Field, Message string }`; `func Normalize(s Spec) (Spec, error)`.
  - `ports.go`: `var CommonPorts []int` (100 ports); `func ParsePorts(spec string) ([]int, error)`; `func ServiceName(port int) string`.
  - `allowlist.go`: `type EntryError struct{ Entry, Message string }` (JSON `entry`, `message`); `type Allowlist`; `func ParseAllowlist(entries []string) (*Allowlist, []EntryError)`; `func (a *Allowlist) Empty() bool`; `func (a *Allowlist) Allows(target string, ip net.IP) bool`; `func AlwaysBlocked(ip net.IP) bool`; `func ParseAddressList(s string) ([]*net.IPNet, error)`; `func InNets(ip net.IP, nets []*net.IPNet) bool`.
  - Bad allowlist entry messages, verbatim: `not a valid IPv4 address, CIDR or host name`, `too broad: use /8 or narrower`, `IPv6 is not supported yet`.
  - Test helpers later tasks use: `intp(n int) *int` and `paramField(err error) string` (`params_test.go`).

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/nettools/params_test.go`:

```go
package nettools

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func intp(n int) *int { return &n }

// paramField returns the Field of a *ParamError, or "" for nil / other errors.
func paramField(err error) string {
	var pe *ParamError
	if errors.As(err, &pe) {
		return pe.Field
	}
	return ""
}

func TestNormalizeDefaults(t *testing.T) {
	cases := []struct {
		tool Tool
		want Params
	}{
		{ToolPing, Params{Count: 5, IntervalMS: 1000, TimeoutMS: 2000, Size: intp(56)}},
		{ToolTraceroute, Params{MaxHops: 30, Rounds: 5, TimeoutMS: 1000}},
		{ToolDNS, Params{RecordType: "A"}},
		{ToolTCP, Params{Ports: "common", TimeoutMS: 1500}},
	}
	for _, c := range cases {
		got, err := Normalize(Spec{Tool: c.tool, Target: "  192.0.2.10 "})
		if err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		if got.Target != "192.0.2.10" {
			t.Errorf("%s: target %q, want it trimmed", c.tool, got.Target)
		}
		if !reflect.DeepEqual(got.Params, c.want) {
			t.Errorf("%s: params %+v, want %+v", c.tool, got.Params, c.want)
		}
	}
}

// Every bound: min-1 and max+1 refused with the field named, min and max kept.
func TestNormalizeBounds(t *testing.T) {
	type tc struct {
		name  string
		spec  Spec
		field string // "" = accepted
	}
	ping := func(p Params) Spec { return Spec{Tool: ToolPing, Target: "h", Params: p} }
	trace := func(p Params) Spec { return Spec{Tool: ToolTraceroute, Target: "h", Params: p} }
	tcp := func(p Params) Spec { return Spec{Tool: ToolTCP, Target: "h", Params: p} }
	cases := []tc{
		{"ping count -1", ping(Params{Count: -1}), "count"},
		{"ping count 1", ping(Params{Count: 1}), ""},
		{"ping count 100", ping(Params{Count: 100}), ""},
		{"ping count 101", ping(Params{Count: 101}), "count"},
		{"ping interval 199", ping(Params{IntervalMS: 199}), "interval_ms"},
		{"ping interval 200", ping(Params{IntervalMS: 200}), ""},
		{"ping interval 5000", ping(Params{Count: 1, IntervalMS: 5000}), ""},
		{"ping interval 5001", ping(Params{Count: 1, IntervalMS: 5001}), "interval_ms"},
		{"ping timeout 499", ping(Params{TimeoutMS: 499}), "timeout_ms"},
		{"ping timeout 500", ping(Params{TimeoutMS: 500}), ""},
		{"ping timeout 5000", ping(Params{TimeoutMS: 5000}), ""},
		{"ping timeout 5001", ping(Params{TimeoutMS: 5001}), "timeout_ms"},
		{"ping size -1", ping(Params{Size: intp(-1)}), "size"},
		{"ping size 0", ping(Params{Size: intp(0)}), ""},
		{"ping size 1472", ping(Params{Size: intp(1472)}), ""},
		{"ping size 1473", ping(Params{Size: intp(1473)}), "size"},
		{"trace hops 0 is the default", trace(Params{MaxHops: 0}), ""},
		{"trace hops -1", trace(Params{MaxHops: -1}), "max_hops"},
		{"trace hops 1", trace(Params{MaxHops: 1}), ""},
		{"trace hops 30", trace(Params{MaxHops: 30}), ""},
		{"trace hops 31", trace(Params{MaxHops: 31}), "max_hops"},
		{"trace rounds -1", trace(Params{Rounds: -1}), "rounds"},
		{"trace rounds 1", trace(Params{Rounds: 1}), ""},
		{"trace rounds 10", trace(Params{Rounds: 10}), ""},
		{"trace rounds 11", trace(Params{Rounds: 11}), "rounds"},
		{"trace timeout 499", trace(Params{TimeoutMS: 499}), "timeout_ms"},
		{"trace timeout 500", trace(Params{TimeoutMS: 500}), ""},
		{"trace timeout 3000", trace(Params{TimeoutMS: 3000}), ""},
		{"trace timeout 3001", trace(Params{TimeoutMS: 3001}), "timeout_ms"},
		{"tcp timeout 499", tcp(Params{TimeoutMS: 499}), "timeout_ms"},
		{"tcp timeout 500", tcp(Params{TimeoutMS: 500}), ""},
		{"tcp timeout 5000", tcp(Params{TimeoutMS: 5000}), ""},
		{"tcp timeout 5001", tcp(Params{TimeoutMS: 5001}), "timeout_ms"},
		{"tcp ports 1-1024", tcp(Params{Ports: "1-1024"}), ""},
		{"tcp ports 1-1025", tcp(Params{Ports: "1-1025"}), "ports"},
		{"tcp ports bad", tcp(Params{Ports: "ssh"}), "ports"},
		{"unknown tool", Spec{Tool: "nmap", Target: "h"}, "tool"},
		{"empty target", Spec{Tool: ToolPing, Target: "   "}, "target"},
		{"target with a space", Spec{Tool: ToolPing, Target: "a b"}, "target"},
		{"target of 253", Spec{Tool: ToolPing, Target: string(make253())}, ""},
		{"target of 254", Spec{Tool: ToolPing, Target: string(make253()) + "a"}, "target"},
	}
	for _, c := range cases {
		_, err := Normalize(c.spec)
		if got := paramField(err); got != c.field {
			t.Errorf("%s: refused field %q (err %v), want %q", c.name, got, err, c.field)
		}
		if err != nil && paramField(err) == "" {
			t.Errorf("%s: error %v is not a *ParamError", c.name, err)
		}
	}
}

func make253() []byte {
	b := make([]byte, 253)
	for i := range b {
		b[i] = 'a'
	}
	return b
}

// Review Focus 4: a ping must fit its 2-minute deadline.
func TestNormalizePingTotal(t *testing.T) {
	cases := []struct {
		count, interval, timeout int
		ok                       bool
	}{
		{100, 1000, 2000, true},  // 102,000 ms
		{100, 5000, 2000, false}, // 502,000 ms
		{23, 5000, 2000, false},  // 117,000 ms
		{22, 5000, 2000, true},   // 112,000 ms
		{22, 5000, 5000, true},   // 115,000 ms: exactly the limit
	}
	for _, c := range cases {
		_, err := Normalize(Spec{Tool: ToolPing, Target: "h", Params: Params{Count: c.count, IntervalMS: c.interval, TimeoutMS: c.timeout}})
		if (err == nil) != c.ok {
			t.Errorf("%d × %d + %d: err %v, want ok=%v", c.count, c.interval, c.timeout, err, c.ok)
		}
	}
	_, err := Normalize(Spec{Tool: ToolPing, Target: "h", Params: Params{Count: 23, IntervalMS: 5000}})
	var pe *ParamError
	if !errors.As(err, &pe) || pe.Field != "count" || pe.Message != "count × interval is too long: at most 115 seconds" {
		t.Errorf("23 × 5000: %v, want count: count × interval is too long: at most 115 seconds", err)
	}
}

func TestNormalizeClearsOtherToolsFields(t *testing.T) {
	all := Params{Count: 3, IntervalMS: 300, Size: intp(10), MaxHops: 4, Rounds: 2, TimeoutMS: 900,
		RecordType: "mx", Server: "192.0.2.53", Ports: "22"}
	cases := []struct {
		tool Tool
		want Params
	}{
		{ToolPing, Params{Count: 3, IntervalMS: 300, Size: intp(10), TimeoutMS: 900}},
		{ToolTraceroute, Params{MaxHops: 4, Rounds: 2, TimeoutMS: 900}},
		{ToolDNS, Params{RecordType: "MX", Server: "192.0.2.53"}},
		{ToolTCP, Params{Ports: "22", TimeoutMS: 900}},
	}
	for _, c := range cases {
		got, err := Normalize(Spec{Tool: c.tool, Target: "example.org", Params: all})
		if err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		if !reflect.DeepEqual(got.Params, c.want) {
			t.Errorf("%s: %+v, want %+v", c.tool, got.Params, c.want)
		}
	}
}

func TestNormalizeDNS(t *testing.T) {
	cases := []struct {
		recordType, server string
		wantType, wantSrv  string
		field              string
	}{
		{"", "", "A", "", ""},
		{" aaaa ", "", "AAAA", "", ""},
		{"caa", "", "CAA", "", ""},
		{"AXFR", "", "", "", "record_type"},
		{"A", "192.0.2.53", "A", "192.0.2.53", ""},
		{"A", " 192.0.2.53:5353 ", "A", "192.0.2.53:5353", ""},
		{"A", "192.0.2.53:0", "", "", "server"},
		{"A", "192.0.2.53:65536", "", "", "server"},
		{"A", "192.0.2.53:", "", "", "server"},
		{"A", "dns.example.org", "", "", "server"},
		{"A", "2001:db8::53", "", "", "server"},
		{"A", "[2001:db8::53]:53", "", "", "server"},
		{"A", "::ffff:192.0.2.53", "", "", "server"},
	}
	for _, c := range cases {
		got, err := Normalize(Spec{Tool: ToolDNS, Target: "example.org", Params: Params{RecordType: c.recordType, Server: c.server}})
		if f := paramField(err); f != c.field {
			t.Errorf("%q %q: field %q (%v), want %q", c.recordType, c.server, f, err, c.field)
			continue
		}
		if err == nil && (got.Params.RecordType != c.wantType || got.Params.Server != c.wantSrv) {
			t.Errorf("%q %q: got %q %q, want %q %q", c.recordType, c.server, got.Params.RecordType, got.Params.Server, c.wantType, c.wantSrv)
		}
	}
	_, err := Normalize(Spec{Tool: ToolDNS, Target: "x", Params: Params{Server: "nope"}})
	var pe *ParamError
	if !errors.As(err, &pe) || pe.Message != "server must be an IPv4 address, optionally with :port" {
		t.Errorf("bad server message: %v", err)
	}
	// PTR of a name is looked up as typed.
	if _, err := Normalize(Spec{Tool: ToolDNS, Target: "10.in-addr.arpa", Params: Params{RecordType: "PTR"}}); err != nil {
		t.Errorf("PTR of a name: %v", err)
	}
}

func TestNormalizeTCPPorts(t *testing.T) {
	for in, want := range map[string]string{"": "common", " COMMON ": "common", " 22, 80 ": "22, 80"} {
		got, err := Normalize(Spec{Tool: ToolTCP, Target: "h", Params: Params{Ports: in}})
		if err != nil || got.Params.Ports != want {
			t.Errorf("ports %q: %q, %v; want %q", in, got.Params.Ports, err, want)
		}
	}
}

func TestDeadline(t *testing.T) {
	want := map[Tool]time.Duration{ToolDNS: 15 * time.Second, ToolPing: 2 * time.Minute,
		ToolTraceroute: 3 * time.Minute, ToolTCP: 5 * time.Minute, "nmap": 0}
	for tool, d := range want {
		if got := Deadline(tool); got != d {
			t.Errorf("Deadline(%s) = %v, want %v", tool, got, d)
		}
	}
}
```

Create `backend/internal/nettools/ports_test.go`:

```go
package nettools

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestParsePorts(t *testing.T) {
	cases := []struct {
		in      string
		want    []int
		wantErr string // substring; "" = no error
	}{
		{"22", []int{22}, ""},
		{"80,80,22", []int{22, 80}, ""},
		{" 443 , 22 , 8000-8003 ", []int{22, 443, 8000, 8001, 8002, 8003}, ""},
		{"22,,80,", []int{22, 80}, ""},
		{"5-5", []int{5}, ""},
		{"1-1024", nil, ""}, // checked by length below
		{"1-1025", nil, "at most 1024 ports"},
		{"1-1000,2000-2025", nil, "at most 1024 ports"},
		{"1-65535", nil, "at most 1024 ports"},
		{"0", nil, `"0" is not a port number (1-65535)`},
		{"65536", nil, `"65536" is not a port number (1-65535)`},
		{"10-5", nil, `"10-5": the range starts above its end`},
		{"ssh", nil, "is not a port number"},
		{"22-", nil, "is not a port number"},
		{"", nil, "no ports given"},
		{" , ", nil, "no ports given"},
	}
	for _, c := range cases {
		got, err := ParsePorts(c.in)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("ParsePorts(%q) error %v, want %q", c.in, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePorts(%q): %v", c.in, err)
			continue
		}
		if c.want != nil && !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParsePorts(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	if got, _ := ParsePorts("1-1024"); len(got) != 1024 || got[0] != 1 || got[1023] != 1024 {
		t.Errorf("1-1024: %d ports", len(got))
	}
}

func TestCommonPorts(t *testing.T) {
	if len(CommonPorts) != 100 {
		t.Errorf("%d common ports, want 100", len(CommonPorts))
	}
	if !sort.IntsAreSorted(CommonPorts) {
		t.Error("CommonPorts is not ascending")
	}
	for i := 1; i < len(CommonPorts); i++ {
		if CommonPorts[i] == CommonPorts[i-1] {
			t.Errorf("port %d listed twice", CommonPorts[i])
		}
	}
	got, err := ParsePorts(" Common ")
	if err != nil || !reflect.DeepEqual(got, CommonPorts) {
		t.Errorf("ParsePorts(common) = %d ports, %v", len(got), err)
	}
	got[0] = 9999 // the preset is a copy
	if CommonPorts[0] != 7 {
		t.Error("ParsePorts returned the preset itself")
	}
}

func TestServiceName(t *testing.T) {
	for port, want := range map[int]string{22: "ssh", 3389: "rdp", 443: "https", 5985: "winrm", 27017: "mongodb", 12345: ""} {
		if got := ServiceName(port); got != want {
			t.Errorf("ServiceName(%d) = %q, want %q", port, got, want)
		}
	}
}
```

Create `backend/internal/nettools/allowlist_test.go`:

```go
package nettools

import (
	"net"
	"reflect"
	"testing"
)

func TestParseAllowlistErrors(t *testing.T) {
	a, bad := ParseAllowlist([]string{
		"10.0.0.0/8",      // ok: /8 is the broadest allowed
		"10.0.0.0/7",      // too broad
		"0.0.0.0/0",       // too broad
		"192.0.2.10",      // ok
		" ",               // blank: ignored
		"fileserver",      // ok: exact name
		"*.Example.ORG.",  // ok: wildcard
		"*",               // bad
		"*.org",           // bad: the wildcard needs a dotted domain
		"bad_host.local",  // bad: underscore
		"-lead.example",   // bad: label starts with a hyphen
		"10.0.0.256",      // bad: not an address, and not a name
		"10.0.0.0/33",     // bad CIDR
		"2001:db8::1",     // IPv6
		"2001:db8::/32",   // IPv6
		"fe80::1%eth0:80", // bad
	})
	want := []EntryError{
		{"10.0.0.0/7", "too broad: use /8 or narrower"},
		{"0.0.0.0/0", "too broad: use /8 or narrower"},
		{"*", "not a valid IPv4 address, CIDR or host name"},
		{"*.org", "not a valid IPv4 address, CIDR or host name"},
		{"bad_host.local", "not a valid IPv4 address, CIDR or host name"},
		{"-lead.example", "not a valid IPv4 address, CIDR or host name"},
		{"10.0.0.256", "not a valid IPv4 address, CIDR or host name"},
		{"10.0.0.0/33", "not a valid IPv4 address, CIDR or host name"},
		{"2001:db8::1", "IPv6 is not supported yet"},
		{"2001:db8::/32", "IPv6 is not supported yet"},
		{"fe80::1%eth0:80", "not a valid IPv4 address, CIDR or host name"},
	}
	if !reflect.DeepEqual(bad, want) {
		t.Errorf("errors:\n got %v\nwant %v", bad, want)
	}
	if a.Empty() {
		t.Fatal("the good entries were dropped")
	}
	if !a.Allows("10.200.0.1", net.ParseIP("10.200.0.1")) || !a.Allows("192.0.2.10", net.ParseIP("192.0.2.10")) {
		t.Error("the good address entries do not match")
	}
	if !a.Allows("FileServer.", nil) || !a.Allows("www.example.org", nil) {
		t.Error("the good name entries do not match")
	}
}

func TestAllowlistLongNames(t *testing.T) {
	label63 := "a123456789b123456789c123456789d123456789e123456789f123456789abc"
	if _, bad := ParseAllowlist([]string{label63 + ".example"}); len(bad) != 0 {
		t.Errorf("a 63-character label: %v", bad)
	}
	if _, bad := ParseAllowlist([]string{label63 + "x.example"}); len(bad) != 1 {
		t.Errorf("a 64-character label was accepted")
	}
}

func TestAllowlistAllows(t *testing.T) {
	a, bad := ParseAllowlist([]string{"10.1.0.0/16", "192.0.2.10", "nas.lab", "*.corp.example", "*.metadata.test"})
	if len(bad) != 0 {
		t.Fatal(bad)
	}
	ip := net.ParseIP
	cases := []struct {
		name   string
		target string
		ip     net.IP
		want   bool
	}{
		{"inside the CIDR", "10.1.2.3", ip("10.1.2.3"), true},
		{"outside the CIDR", "10.2.0.1", ip("10.2.0.1"), false},
		{"single address", "192.0.2.10", ip("192.0.2.10"), true},
		{"neighbour of the single address", "192.0.2.11", ip("192.0.2.11"), false},
		{"name typed, address elsewhere", "nas.lab", ip("172.16.5.5"), true},
		{"name in another case, trailing dot", "NAS.Lab.", ip("172.16.5.5"), true},
		{"name not listed, address in the CIDR", "printer.lab", ip("10.1.9.9"), true},
		{"name not listed, address not either", "printer.lab", ip("172.16.5.6"), false},
		{"wildcard, one level", "db.corp.example", ip("172.16.0.1"), true},
		{"wildcard, two levels", "a.b.corp.example", ip("172.16.0.1"), true},
		{"wildcard does not match the bare domain", "corp.example", ip("172.16.0.1"), false},
		{"wildcard does not match a lookalike", "xcorp.example", ip("172.16.0.1"), false},
		{"pure name match without an address", "nas.lab", nil, true},
		{"no name match and no address", "other.lab", nil, false},
		{"IPv6 address", "nas.lab", ip("2001:db8::1"), false},
		// Review Focus 3: a listed name that resolves to an always-blocked address.
		{"name entry, metadata address", "x.metadata.test", ip("169.254.169.254"), false},
		{"name entry, ECS metadata", "nas.lab", ip("169.254.170.2"), false},
		{"name entry, Alibaba metadata", "nas.lab", ip("100.100.100.200"), false},
		{"name entry, multicast", "nas.lab", ip("239.1.2.3"), false},
		{"name entry, this network", "nas.lab", ip("0.1.2.3"), false},
		{"name entry, broadcast", "nas.lab", ip("255.255.255.255"), false},
	}
	for _, c := range cases {
		if got := a.Allows(c.target, c.ip); got != c.want {
			t.Errorf("%s: Allows(%q, %v) = %v, want %v", c.name, c.target, c.ip, got, c.want)
		}
	}
}

func TestAllowlistBlockedEvenWhenListed(t *testing.T) {
	a, bad := ParseAllowlist([]string{"169.254.0.0/16", "224.0.0.0/8", "0.0.0.0/8"})
	if len(bad) != 0 {
		t.Fatal(bad)
	}
	for _, s := range []string{"169.254.169.254", "224.0.0.1", "0.0.0.0"} {
		if a.Allows(s, net.ParseIP(s)) {
			t.Errorf("%s allowed", s)
		}
	}
	if !a.Allows("169.254.1.1", net.ParseIP("169.254.1.1")) {
		t.Error("an ordinary link-local address in a listed CIDR was refused")
	}
}

func TestAllowlistEmpty(t *testing.T) {
	for _, entries := range [][]string{nil, {}, {"", "  "}, {"*"}} {
		a, _ := ParseAllowlist(entries)
		if !a.Empty() {
			t.Errorf("%q: not empty", entries)
		}
		if a.Allows("10.0.0.1", net.ParseIP("10.0.0.1")) || a.Allows("fileserver", nil) {
			t.Errorf("%q: an empty list allowed something", entries)
		}
	}
	var none *Allowlist
	if !none.Empty() || none.Allows("x", net.ParseIP("10.0.0.1")) {
		t.Error("a nil list allowed something")
	}
}

func TestAlwaysBlocked(t *testing.T) {
	cases := map[string]bool{
		"169.254.169.254": true, "169.254.170.2": true, "100.100.100.200": true,
		"0.0.0.0": true, "0.255.255.255": true, "224.0.0.1": true, "239.255.255.255": true,
		"255.255.255.255": true, "2001:db8::1": true,
		"169.254.169.253": false, "1.0.0.0": false, "223.255.255.255": false, "240.0.0.1": false,
		"10.0.0.1": false, "127.0.0.1": false, "255.255.255.254": false,
	}
	for s, want := range cases {
		if got := AlwaysBlocked(net.ParseIP(s)); got != want {
			t.Errorf("AlwaysBlocked(%s) = %v, want %v", s, got, want)
		}
	}
	if !AlwaysBlocked(nil) {
		t.Error("AlwaysBlocked(nil) = false")
	}
}

func TestParseAddressList(t *testing.T) {
	nets, err := ParseAddressList(" 10.0.0.0/24, 192.0.2.7 ,,")
	if err != nil || len(nets) != 2 {
		t.Fatalf("got %v, %v", nets, err)
	}
	for s, want := range map[string]bool{"10.0.0.255": true, "10.0.1.0": false, "192.0.2.7": true, "192.0.2.8": false} {
		if got := InNets(net.ParseIP(s), nets); got != want {
			t.Errorf("InNets(%s) = %v, want %v", s, got, want)
		}
	}
	if nets, err := ParseAddressList(""); nets != nil || err != nil {
		t.Errorf(`"" = %v, %v; want nil, nil`, nets, err)
	}
	for _, s := range []string{"10.0.0.0/33", "fileserver", "2001:db8::/32", "10.0.0.1,nope"} {
		if _, err := ParseAddressList(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
	if InNets(net.ParseIP("2001:db8::1"), nets) || InNets(nil, nets) {
		t.Error("InNets matched a non-IPv4 address")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run (from `backend/`): `go test ./internal/nettools/ -v`
Expected: the build fails with `undefined: ParseAllowlist`, `undefined: EntryError`, `undefined: Normalize` and similar.

- [ ] **Step 3: Write the types**

Create `backend/internal/nettools/nettools.go`:

```go
// Package nettools runs the network troubleshooting tools: ping, MTR-style
// traceroute, DNS lookup and TCP port check. It is compiled into both the
// Sentinel server and the agent, so it imports only the standard library and
// golang.org/x/net and golang.org/x/sys: no database, no HTTP framework and no
// other Sentinel package.
package nettools

import "time"

// Tool names one of the network tools.
type Tool string

const (
	ToolPing       Tool = "ping"
	ToolTraceroute Tool = "traceroute"
	ToolDNS        Tool = "dns"
	ToolTCP        Tool = "tcp"
)

// Spec is one run: the tool, what it targets and its parameters.
type Spec struct {
	Tool Tool `json:"tool"`
	// Target is what the user typed: a host name or IPv4 address; for dns,
	// the name (or IPv4 address, for PTR) to look up.
	Target string `json:"target"`
	// TargetIP is the IPv4 address the tool contacts: the probed host for
	// ping, traceroute and tcp; the named DNS server for dns (empty when the
	// lookup goes through the system resolver). Set by the server after the
	// allowlist check; tools never resolve Target themselves.
	TargetIP string `json:"target_ip,omitempty"`
	Params   Params `json:"params"`
}

// Params holds every tool's parameters. Normalize fills the defaults and
// clears the fields the tool does not use.
type Params struct {
	Count      int    `json:"count,omitempty"`       // ping
	IntervalMS int    `json:"interval_ms,omitempty"` // ping
	Size       *int   `json:"size,omitempty"`        // ping payload bytes; 0 is valid
	MaxHops    int    `json:"max_hops,omitempty"`    // traceroute
	Rounds     int    `json:"rounds,omitempty"`      // traceroute
	TimeoutMS  int    `json:"timeout_ms,omitempty"`  // ping, traceroute, tcp (per probe)
	RecordType string `json:"record_type,omitempty"` // dns
	Server     string `json:"server,omitempty"`      // dns: "" = system resolver, else "a.b.c.d" or "a.b.c.d:port"
	Ports      string `json:"ports,omitempty"`       // tcp: "22,80,443,8000-8100" or "common"
}

// Event is one result a tool reports while it runs. Data is one of the event
// types below, chosen by Type.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Emitter receives a tool's events. A tool calls it from one goroutine at a
// time, in the order the results became known.
type Emitter func(Event)

// Event types.
const (
	EventReply     = "reply"      // ping: PingReply
	EventTimeout   = "timeout"    // ping: PingTimeout
	EventError     = "error"      // ping: PingError
	EventHop       = "hop"        // traceroute: HopProbe
	EventRoundDone = "round_done" // traceroute: RoundDone
	EventHopName   = "hop_name"   // traceroute: HopName
	EventAnswer    = "answer"     // dns: DNSAnswer
	EventStart     = "start"      // tcp: ScanStart
	EventPort      = "port"       // tcp: PortResult
)

// PingReply is an echo reply to probe Seq.
type PingReply struct {
	Seq   int     `json:"seq"`
	RTTMS float64 `json:"rtt_ms"`
	TTL   int     `json:"ttl"`
	From  string  `json:"from"`
}

// PingTimeout is a probe that got no reply in time.
type PingTimeout struct {
	Seq int `json:"seq"`
}

// PingError is a probe that failed, e.g. "host unreachable from 10.0.0.1".
type PingError struct {
	Seq     int    `json:"seq"`
	Message string `json:"message"`
}

// PingSummary sums up a ping run. The RTT figures are nil without replies;
// JitterMS is nil with fewer than two.
type PingSummary struct {
	Sent     int      `json:"sent"`
	Received int      `json:"received"`
	LossPct  float64  `json:"loss_pct"`
	MinMS    *float64 `json:"min_ms"`
	AvgMS    *float64 `json:"avg_ms"`
	MaxMS    *float64 `json:"max_ms"`
	JitterMS *float64 `json:"jitter_ms"`
}

// HopProbe is one traceroute probe: what answered the probe sent with TTL in
// Round. Addr is empty and RTTMS nil when nothing answered.
type HopProbe struct {
	Round   int      `json:"round"`
	TTL     int      `json:"ttl"`
	Addr    string   `json:"addr,omitempty"`
	RTTMS   *float64 `json:"rtt_ms,omitempty"`
	Reached bool     `json:"reached"`
}

// HopStats is one row of the MTR table: every probe sent with one TTL.
type HopStats struct {
	TTL      int      `json:"ttl"`
	Addrs    []string `json:"addrs"`
	Name     string   `json:"name,omitempty"`
	Sent     int      `json:"sent"`
	Received int      `json:"received"`
	LossPct  float64  `json:"loss_pct"`
	LastMS   *float64 `json:"last_ms"`
	AvgMS    *float64 `json:"avg_ms"`
	BestMS   *float64 `json:"best_ms"`
	WorstMS  *float64 `json:"worst_ms"`
	StdevMS  *float64 `json:"stdev_ms"`
}

// RoundDone closes a traceroute round with the table so far.
type RoundDone struct {
	Round int        `json:"round"`
	Hops  []HopStats `json:"hops"` // cumulative after this round
}

// HopName is the reverse-DNS name of a hop address.
type HopName struct {
	TTL  int    `json:"ttl"`
	Addr string `json:"addr"`
	Name string `json:"name"`
}

// TraceSummary sums up a traceroute run.
type TraceSummary struct {
	Reached  bool       `json:"reached"`
	HopCount int        `json:"hop_count"`
	Hops     []HopStats `json:"hops"`
}

// DNSRecord is one resource record, with its data written out as text.
type DNSRecord struct {
	Name string `json:"name"`
	Type string `json:"type"`
	TTL  uint32 `json:"ttl"`
	Data string `json:"data"`
}

// DNSAnswer is the reply to a DNS lookup.
type DNSAnswer struct {
	Server        string      `json:"server"` // ip:port asked
	RCode         string      `json:"rcode"`  // "NOERROR", "NXDOMAIN", …
	Authoritative bool        `json:"authoritative"`
	Truncated     bool        `json:"truncated"` // the UDP answer was truncated
	TCP           bool        `json:"tcp"`       // the answer shown came over TCP
	RTTMS         float64     `json:"rtt_ms"`
	Answer        []DNSRecord `json:"answer"`
	Authority     []DNSRecord `json:"authority"`
	Additional    []DNSRecord `json:"additional"`
}

// DNSSummary sums up a DNS lookup.
type DNSSummary struct {
	Server      string  `json:"server"`
	RCode       string  `json:"rcode"`
	AnswerCount int     `json:"answer_count"`
	RTTMS       float64 `json:"rtt_ms"`
}

// TCP port states.
const (
	PortOpen     = "open"
	PortClosed   = "closed"
	PortFiltered = "filtered"
)

// ScanStart opens a port scan with the number of ports it will try.
type ScanStart struct {
	Total int `json:"total"`
}

// PortResult is the state of one port. RTTMS is set for open ports.
type PortResult struct {
	Port    int      `json:"port"`
	State   string   `json:"state"`
	RTTMS   *float64 `json:"rtt_ms,omitempty"`
	Service string   `json:"service,omitempty"`
}

// TCPSummary sums up a port scan.
type TCPSummary struct {
	Total     int   `json:"total"`
	Open      int   `json:"open"`
	Closed    int   `json:"closed"`
	Filtered  int   `json:"filtered"`
	OpenPorts []int `json:"open_ports"`
}

// Deadline is the tool's hard limit: dns 15s, ping 2m, traceroute 3m, tcp 5m.
// It is 0 for an unknown tool.
func Deadline(t Tool) time.Duration {
	switch t {
	case ToolDNS:
		return 15 * time.Second
	case ToolPing:
		return 2 * time.Minute
	case ToolTraceroute:
		return 3 * time.Minute
	case ToolTCP:
		return 5 * time.Minute
	}
	return 0
}
```

- [ ] **Step 4: Write the limits and `Normalize`**

Create `backend/internal/nettools/params.go`:

```go
package nettools

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Limits and defaults. Normalize enforces every one of them.
const (
	PingCountDefault, PingCountMin, PingCountMax                = 5, 1, 100
	PingIntervalDefaultMS, PingIntervalMinMS, PingIntervalMaxMS = 1000, 200, 5000
	PingTimeoutDefaultMS, PingTimeoutMinMS, PingTimeoutMaxMS    = 2000, 500, 5000
	PingSizeDefault, PingSizeMax                                = 56, 1472
	PingMaxTotalMS                                              = 115000 // count × interval + timeout
	TraceMaxHopsDefault, TraceMaxHopsMin, TraceMaxHopsMax       = 30, 1, 30
	TraceRoundsDefault, TraceRoundsMin, TraceRoundsMax          = 5, 1, 10
	TraceTimeoutDefaultMS, TraceTimeoutMinMS, TraceTimeoutMaxMS = 1000, 500, 3000
	TCPTimeoutDefaultMS, TCPTimeoutMinMS, TCPTimeoutMaxMS       = 1500, 500, 5000
	TCPConcurrency                                              = 50
	MaxPorts                                                    = 1024
	MaxTargetLength                                             = 253
	DNSTimeout                                                  = 5 * time.Second
)

// DNSRecordTypes in display order; RecordType default "A".
var DNSRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "NS", "TXT", "SOA", "SRV", "PTR", "CAA"}

// ParamError is a parameter Normalize refused. Field is the parameter's JSON
// name ("count", "server", …), or "tool" / "target".
type ParamError struct {
	Field   string
	Message string
}

func (e *ParamError) Error() string { return e.Field + ": " + e.Message }

// Normalize checks a spec against the limits and returns it with defaults
// filled in and unused parameters cleared. It does not resolve anything or
// consult the allowlist. Errors are *ParamError.
func Normalize(s Spec) (Spec, error) {
	s.Target = strings.TrimSpace(s.Target)
	s.TargetIP = strings.TrimSpace(s.TargetIP)
	var norm func(Params) (Params, error)
	switch s.Tool {
	case ToolPing:
		norm = normalizePing
	case ToolTraceroute:
		norm = normalizeTraceroute
	case ToolDNS:
		norm = normalizeDNS
	case ToolTCP:
		norm = normalizeTCP
	default:
		return s, &ParamError{Field: "tool", Message: fmt.Sprintf("unknown tool %q", s.Tool)}
	}
	if err := checkTarget(s.Target); err != nil {
		return s, err
	}
	p, err := norm(s.Params)
	if err != nil {
		return s, err
	}
	s.Params = p
	return s, nil
}

func checkTarget(t string) error {
	switch {
	case t == "":
		return &ParamError{Field: "target", Message: "target is required"}
	case len(t) > MaxTargetLength:
		return &ParamError{Field: "target", Message: fmt.Sprintf("target is too long: at most %d characters", MaxTargetLength)}
	case strings.IndexFunc(t, unicode.IsSpace) >= 0:
		return &ParamError{Field: "target", Message: "target must not contain spaces"}
	}
	return nil
}

// intParam returns v, or def when v is 0, and checks it lies in [lo, hi].
func intParam(field string, v, def, lo, hi int) (int, error) {
	if v == 0 {
		v = def
	}
	if v < lo || v > hi {
		return 0, &ParamError{Field: field, Message: fmt.Sprintf("%s must be between %d and %d", field, lo, hi)}
	}
	return v, nil
}

func normalizePing(in Params) (Params, error) {
	var out Params
	var err error
	if out.Count, err = intParam("count", in.Count, PingCountDefault, PingCountMin, PingCountMax); err != nil {
		return out, err
	}
	if out.IntervalMS, err = intParam("interval_ms", in.IntervalMS, PingIntervalDefaultMS, PingIntervalMinMS, PingIntervalMaxMS); err != nil {
		return out, err
	}
	if out.TimeoutMS, err = intParam("timeout_ms", in.TimeoutMS, PingTimeoutDefaultMS, PingTimeoutMinMS, PingTimeoutMaxMS); err != nil {
		return out, err
	}
	size := PingSizeDefault
	if in.Size != nil {
		size = *in.Size
	}
	if size < 0 || size > PingSizeMax {
		return out, &ParamError{Field: "size", Message: fmt.Sprintf("size must be between 0 and %d", PingSizeMax)}
	}
	out.Size = &size
	if out.Count*out.IntervalMS+out.TimeoutMS > PingMaxTotalMS {
		return out, &ParamError{Field: "count", Message: "count × interval is too long: at most 115 seconds"}
	}
	return out, nil
}

func normalizeTraceroute(in Params) (Params, error) {
	var out Params
	var err error
	if out.MaxHops, err = intParam("max_hops", in.MaxHops, TraceMaxHopsDefault, TraceMaxHopsMin, TraceMaxHopsMax); err != nil {
		return out, err
	}
	if out.Rounds, err = intParam("rounds", in.Rounds, TraceRoundsDefault, TraceRoundsMin, TraceRoundsMax); err != nil {
		return out, err
	}
	if out.TimeoutMS, err = intParam("timeout_ms", in.TimeoutMS, TraceTimeoutDefaultMS, TraceTimeoutMinMS, TraceTimeoutMaxMS); err != nil {
		return out, err
	}
	return out, nil
}

func normalizeDNS(in Params) (Params, error) {
	var out Params
	out.RecordType = strings.ToUpper(strings.TrimSpace(in.RecordType))
	if out.RecordType == "" {
		out.RecordType = "A"
	}
	known := false
	for _, t := range DNSRecordTypes {
		if t == out.RecordType {
			known = true
		}
	}
	if !known {
		return out, &ParamError{Field: "record_type", Message: "record_type must be one of " + strings.Join(DNSRecordTypes, ", ")}
	}
	server, err := normalizeServer(in.Server)
	if err != nil {
		return out, err
	}
	out.Server = server
	return out, nil
}

// normalizeServer accepts "", "a.b.c.d" or "a.b.c.d:port" and writes the
// address in canonical form.
func normalizeServer(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	bad := &ParamError{Field: "server", Message: "server must be an IPv4 address, optionally with :port"}
	host, port, hasPort := s, "", false
	if h, p, err := net.SplitHostPort(s); err == nil {
		host, port, hasPort = h, p, true
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || strings.Contains(host, ":") {
		return "", bad
	}
	if !hasPort {
		return ip.To4().String(), nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", bad
	}
	return net.JoinHostPort(ip.To4().String(), strconv.Itoa(n)), nil
}

func normalizeTCP(in Params) (Params, error) {
	var out Params
	var err error
	out.Ports = strings.TrimSpace(in.Ports)
	if out.Ports == "" || strings.EqualFold(out.Ports, "common") {
		out.Ports = "common"
	}
	if _, perr := ParsePorts(out.Ports); perr != nil {
		return out, &ParamError{Field: "ports", Message: perr.Error()}
	}
	if out.TimeoutMS, err = intParam("timeout_ms", in.TimeoutMS, TCPTimeoutDefaultMS, TCPTimeoutMinMS, TCPTimeoutMaxMS); err != nil {
		return out, err
	}
	return out, nil
}
```

- [ ] **Step 5: Write the port parser**

Create `backend/internal/nettools/ports.go`:

```go
package nettools

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CommonPorts is the "common" preset: the 100 TCP ports most often found
// open (nmap's top ports), ascending.
var CommonPorts = []int{
	7, 9, 13, 21, 22, 23, 25, 26, 37, 53,
	79, 80, 81, 88, 106, 110, 111, 113, 119, 135,
	139, 143, 144, 179, 199, 389, 427, 443, 444, 445,
	465, 513, 514, 515, 543, 544, 548, 554, 587, 631,
	646, 873, 990, 993, 995, 1025, 1026, 1027, 1028, 1029,
	1110, 1433, 1720, 1723, 1755, 1900, 2000, 2001, 2049, 2121,
	2717, 3000, 3128, 3306, 3389, 3986, 4899, 5000, 5009, 5051,
	5060, 5101, 5190, 5357, 5432, 5631, 5666, 5800, 5900, 6000,
	6001, 6646, 7070, 8000, 8008, 8009, 8080, 8081, 8443, 8888,
	9100, 9999, 10000, 32768, 49152, 49153, 49154, 49155, 49156, 49157,
}

// serviceNames are the well-known services shown beside a port.
var serviceNames = map[int]string{
	7: "echo", 9: "discard", 13: "daytime", 21: "ftp", 22: "ssh", 23: "telnet",
	25: "smtp", 37: "time", 53: "dns", 79: "finger", 80: "http", 88: "kerberos",
	110: "pop3", 111: "rpcbind", 113: "ident", 119: "nntp", 123: "ntp",
	135: "msrpc", 139: "netbios-ssn", 143: "imap", 161: "snmp", 179: "bgp",
	389: "ldap", 443: "https", 445: "smb", 465: "smtps", 514: "syslog",
	515: "printer", 548: "afp", 554: "rtsp", 587: "submission", 631: "ipp",
	636: "ldaps", 873: "rsync", 990: "ftps", 993: "imaps", 995: "pop3s",
	1433: "mssql", 1723: "pptp", 1900: "upnp", 2049: "nfs", 3000: "http-alt",
	3128: "squid", 3306: "mysql", 3389: "rdp", 5060: "sip", 5432: "postgresql",
	5900: "vnc", 5985: "winrm", 5986: "winrm-https", 6379: "redis",
	8000: "http-alt", 8080: "http-proxy", 8443: "https-alt", 9100: "jetdirect",
	9200: "elasticsearch", 27017: "mongodb",
}

// ServiceName is the well-known service on port ("ssh", "rdp", …) or "".
func ServiceName(port int) string { return serviceNames[port] }

var errTooManyPorts = fmt.Errorf("at most %d ports", MaxPorts)

// ParsePorts reads "common" or a comma-separated list of ports and ranges
// ("22,80,443,8000-8100"; spaces allowed). The result is ascending and
// de-duplicated; every port is 1–65535 and there are at most MaxPorts.
func ParsePorts(spec string) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if strings.EqualFold(spec, "common") {
		return append([]int(nil), CommonPorts...), nil
	}
	seen := map[int]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		loText, hiText, isRange := strings.Cut(part, "-")
		lo, err := parsePort(loText)
		if err != nil {
			return nil, err
		}
		hi := lo
		if isRange {
			if hi, err = parsePort(hiText); err != nil {
				return nil, err
			}
			if lo > hi {
				return nil, fmt.Errorf("%q: the range starts above its end", part)
			}
		}
		for p := lo; p <= hi; p++ {
			seen[p] = true
			if len(seen) > MaxPorts {
				return nil, errTooManyPorts
			}
		}
	}
	if len(seen) == 0 {
		return nil, errors.New("no ports given")
	}
	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports, nil
}

func parsePort(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%q is not a port number (1-65535)", s)
	}
	return n, nil
}
```

- [ ] **Step 6: Write the allowlist**

Create `backend/internal/nettools/allowlist.go`:

```go
package nettools

import (
	"fmt"
	"net"
	"strings"
)

// Messages for bad allowlist entries.
const (
	msgBadEntry = "not a valid IPv4 address, CIDR or host name"
	msgTooBroad = "too broad: use /8 or narrower"
	msgIPv6     = "IPv6 is not supported yet"
)

// EntryError is one allowlist entry ParseAllowlist refused.
type EntryError struct {
	Entry   string `json:"entry"`
	Message string `json:"message"`
}

// Allowlist is the set of targets the tools may contact.
type Allowlist struct {
	nets     []*net.IPNet
	hosts    map[string]bool // exact names, lower case, no trailing dot
	suffixes []string        // ".example.org" for "*.example.org"
}

// ParseAllowlist accepts IPv4 addresses, IPv4 CIDRs (no broader than /8),
// exact host names and "*.domain" wildcards. Blank entries are ignored.
// Every bad entry is reported; the returned list holds the good ones.
func ParseAllowlist(entries []string) (*Allowlist, []EntryError) {
	a := &Allowlist{hosts: map[string]bool{}}
	var bad []EntryError
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		if msg := a.add(e); msg != "" {
			bad = append(bad, EntryError{Entry: e, Message: msg})
		}
	}
	return a, bad
}

// add parses one trimmed entry into a and returns "" or why it is refused.
func (a *Allowlist) add(e string) string {
	if strings.Contains(e, ":") {
		if net.ParseIP(e) != nil {
			return msgIPv6
		}
		if _, _, err := net.ParseCIDR(e); err == nil {
			return msgIPv6
		}
		return msgBadEntry
	}
	if strings.Contains(e, "/") {
		ip, n, err := net.ParseCIDR(e)
		if err != nil || ip.To4() == nil {
			return msgBadEntry
		}
		if ones, _ := n.Mask.Size(); ones < 8 {
			return msgTooBroad
		}
		a.nets = append(a.nets, n)
		return ""
	}
	if ip := net.ParseIP(e); ip != nil {
		a.nets = append(a.nets, &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)})
		return ""
	}
	name := normalizeHost(e)
	if rest, ok := strings.CutPrefix(name, "*."); ok {
		if !strings.Contains(rest, ".") || !validHostName(rest) {
			return msgBadEntry
		}
		a.suffixes = append(a.suffixes, "."+rest)
		return ""
	}
	if !validHostName(name) {
		return msgBadEntry
	}
	a.hosts[name] = true
	return ""
}

// normalizeHost lower-cases a host name and drops one trailing dot.
func normalizeHost(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// validHostName: letters, digits and hyphens in dot-separated labels of
// 1–63 characters, no label starting or ending with a hyphen, at most 253
// characters, and a last label that is not all digits (so "10.0.0.256" is
// not taken for a name).
func validHostName(s string) bool {
	if s == "" || len(s) > MaxTargetLength {
		return false
	}
	labels := strings.Split(s, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	last := labels[len(labels)-1]
	return strings.Trim(last, "0123456789") != ""
}

// Empty reports whether the list allows nothing.
func (a *Allowlist) Empty() bool {
	return a == nil || len(a.nets) == 0 && len(a.hosts) == 0 && len(a.suffixes) == 0
}

// Allows reports whether a run may contact ip for the typed target: the
// target matches a host-name entry, or ip lies in an address/CIDR entry —
// and ip is never AlwaysBlocked. ip may be nil only for a pure host-name match.
func (a *Allowlist) Allows(target string, ip net.IP) bool {
	if a == nil {
		return false
	}
	if ip != nil && AlwaysBlocked(ip) {
		return false
	}
	if a.matchesHost(target) {
		return true
	}
	return ip != nil && InNets(ip, a.nets)
}

func (a *Allowlist) matchesHost(target string) bool {
	t := normalizeHost(target)
	if t == "" {
		return false
	}
	if a.hosts[t] {
		return true
	}
	for _, suffix := range a.suffixes {
		if len(t) > len(suffix) && strings.HasSuffix(t, suffix) {
			return true
		}
	}
	return false
}

// alwaysBlockedNets: cloud metadata addresses (the ones netguard blocks),
// "this network", multicast and the limited broadcast address.
var alwaysBlockedNets = mustCIDRs(
	"169.254.169.254/32", // AWS, GCP, Azure, DigitalOcean, Oracle Cloud metadata
	"169.254.170.2/32",   // AWS ECS task metadata
	"100.100.100.200/32", // Alibaba Cloud metadata
	"0.0.0.0/8",
	"224.0.0.0/4",
	"255.255.255.255/32",
)

func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("nettools: bad CIDR %q: %v", c, err))
		}
		out = append(out, n)
	}
	return out
}

// AlwaysBlocked reports whether ip may never be contacted, whatever the
// allowlist says: the cloud metadata addresses, 0.0.0.0/8, multicast
// (224.0.0.0/4) and 255.255.255.255. A nil or non-IPv4 address is blocked
// too, since the tools reach IPv4 only.
func AlwaysBlocked(ip net.IP) bool {
	if ip.To4() == nil {
		return true
	}
	return InNets(ip, alwaysBlockedNets)
}

// ParseAddressList parses the agent's TOOLS_ALLOWED_TARGETS: comma-separated
// IPv4 addresses and CIDRs ("" → nil, nil).
func ParseAddressList(s string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, part := range strings.Split(s, ",") {
		e := strings.TrimSpace(part)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			ip, n, err := net.ParseCIDR(e)
			if err != nil || ip.To4() == nil || strings.Contains(e, ":") {
				return nil, fmt.Errorf("%q is not an IPv4 address or CIDR", e)
			}
			nets = append(nets, n)
			continue
		}
		ip := net.ParseIP(e)
		if ip == nil || ip.To4() == nil || strings.Contains(e, ":") {
			return nil, fmt.Errorf("%q is not an IPv4 address or CIDR", e)
		}
		nets = append(nets, &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)})
	}
	return nets, nil
}

// InNets reports whether the IPv4 address ip lies in one of nets.
func InNets(ip net.IP, nets []*net.IPNet) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip4) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 7: Run the tests**

Run (from `backend/`): `go test ./internal/nettools/ -v`
Expected: PASS for `TestNormalizeDefaults`, `TestNormalizeBounds`, `TestNormalizePingTotal`, `TestNormalizeClearsOtherToolsFields`, `TestNormalizeDNS`, `TestNormalizeTCPPorts`, `TestDeadline`, `TestParsePorts`, `TestCommonPorts`, `TestServiceName`, `TestParseAllowlistErrors`, `TestAllowlistLongNames`, `TestAllowlistAllows`, `TestAllowlistBlockedEvenWhenListed`, `TestAllowlistEmpty`, `TestAlwaysBlocked`, `TestParseAddressList`.

Run: `gofmt -l ./internal/nettools && go vet ./internal/nettools/`
Expected: no output.

- [ ] **Step 8: Commit**

From the repository root:

```bash
git add backend/internal/nettools/nettools.go backend/internal/nettools/params.go backend/internal/nettools/ports.go backend/internal/nettools/allowlist.go backend/internal/nettools/params_test.go backend/internal/nettools/ports_test.go backend/internal/nettools/allowlist_test.go
git commit -m "feat(tools): nettools specs, limits, port lists and the target allowlist

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 2: ICMP probers

This task adds the shared ICMP echo probe that ping and traceroute use: the `Prober` interface, a parser for the ICMP messages that answer our probes, the Linux/Docker prober and the Windows prober.

How the probers work:

- **Linux and Docker (`icmp_unix.go`, every OS but Windows).** `NewProber` opens a raw socket (`icmp.ListenPacket("ip4:icmp", "0.0.0.0")`, needs root or `NET_RAW`; `CanTrace` true). If that fails it opens the unprivileged datagram socket (`"udp4"`, allowed by `net.ipv4.ping_group_range`; `CanTrace` false). If both fail it returns `ErrICMPUnavailable` itself, unwrapped, because its text is the user-facing message. One reader goroutine parses every incoming message and hands it to the probe waiting for its key: `(id, seq)` on the raw socket, where the prober's id is random; `seq` alone on the datagram socket, because the kernel rewrites the echo id there. Sequence numbers come from one `uint16` counter per prober. Writes are serialised by a mutex because each one does `SetTTL(ttl)` and then `WriteTo`. The reply's TTL comes from the `IP_RECVTTL` control message when the socket allows it (0 otherwise). RTT runs from just before the write to the moment the reader received the reply.
- **Windows (`icmp_windows.go`).** It calls `IcmpCreateFile`, `IcmpSendEcho` and `IcmpCloseHandle` from `iphlpapi.dll` without CGO. The DLL is loaded with `windows.NewLazySystemDLL`, the `golang.org/x/sys/windows` equivalent of `syscall.NewLazyDLL` that loads from System32 only; the agent's `collect_windows.go` loads `kernel32.dll` the same way. Each probe opens its own handle, so concurrent traceroute probes never share one. The blocking call runs in a goroutine and `Echo` returns as soon as `ctx` ends. The struct layouts are written out with `uintptr` for the C pointers, which gives the right layout on 32- and 64-bit; `icmp_windows_test.go` pins the 64-bit offsets. The RTT is the measured time around the call: the API's `RoundTripTime` is in whole milliseconds and reads 0 on a LAN.
- `parseICMPv4` reads the ICMP message without its IP header. An echo reply (type 0) carries the id and sequence in its own header. Time exceeded (11) and destination unreachable (3) quote the IPv4 header (its IHL gives the length, options included) and then our echo request's first 8 bytes. Anything else is not ours.

The real-socket tests skip cleanly when this user cannot send ICMP. In a container running as a non-root user they usually run over the unprivileged socket, because Docker sets `ping_group_range` to all groups. The Windows prober is checked by a cross-compiled `go vet`.

**Files:**
- Create: `backend/internal/nettools/icmp.go`
- Create: `backend/internal/nettools/icmp_unix.go`
- Create: `backend/internal/nettools/icmp_windows.go`
- Test: `backend/internal/nettools/icmp_test.go`
- Test: `backend/internal/nettools/icmp_unix_test.go`
- Test: `backend/internal/nettools/icmp_windows_test.go`

**Interfaces:**
- Consumes: `golang.org/x/net/icmp` (`ListenPacket`, `(*PacketConn).IPv4PacketConn/WriteTo/Close`, `Message.Marshal`, `Echo`), `golang.org/x/net/ipv4` (`ICMPTypeEcho`, `(*PacketConn).SetTTL/SetControlMessage/ReadFrom`, `FlagTTL`), `golang.org/x/sys/windows` (`NewLazySystemDLL`, `Handle`, `InvalidHandle`). All of these are checked against x/net v0.57.0 and x/sys v0.47.0.
- Produces (shared contract):
  - `type ReplyKind int` with `ReplyEcho`, `ReplyTimeExceeded`, `ReplyUnreachable`.
  - `type EchoReply struct{ Kind ReplyKind; From net.IP; RTT time.Duration; TTL int; Code int }`.
  - `var ErrNoReply`, `var ErrICMPUnavailable` (text `ICMP isn't available here (needs root or NET_RAW)`).
  - `type Prober interface { Echo(ctx, dst net.IP, ttl, size int, timeout time.Duration) (EchoReply, error); CanTrace() bool; Close() error }`.
  - `func NewProber() (Prober, error)`, on every OS.
  - Unexported, for tests: `parseICMPv4(b []byte) (parsedICMP, bool)`, `type parsedICMP struct{ kind ReplyKind; id, seq, code int }`, `echoRequest(id, seq, size int) ([]byte, error)`.
  - Test helpers for Task 3 (`icmp_unix_test.go`, build tag `!windows`): `openProber(t) Prober`, which skips when ICMP is unavailable and closes the prober at cleanup; `skipIfDenied(t, err)`; `var loopback net.IP`.
  - Real probers refuse a probe whose context has already ended, before sending anything.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/nettools/icmp_test.go`:

```go
package nettools

import (
	"testing"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// quotedIPv4 is the start of the datagram an ICMP error quotes: an IPv4
// header of ihl 32-bit words carrying proto, then the first bytes of its
// payload.
func quotedIPv4(ihl int, proto byte, payload []byte) []byte {
	h := make([]byte, ihl*4)
	h[0] = 0x40 | byte(ihl)
	h[8] = 1 // TTL left when it expired
	h[9] = proto
	copy(h[12:16], []byte{10, 0, 0, 5})
	copy(h[16:20], []byte{192, 0, 2, 10})
	return append(h, payload...)
}

// icmpErrorMsg is an ICMP error of type typ and code quoting q.
func icmpErrorMsg(typ, code byte, q []byte) []byte {
	return append([]byte{typ, code, 0, 0, 0, 0, 0, 0}, q...)
}

// Our echo request: id 0x1234, seq 7.
var quotedEcho = []byte{8, 0, 0xf7, 0xc3, 0x12, 0x34, 0x00, 0x07}

func TestParseICMPv4(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		ok   bool
		want parsedICMP
	}{
		{"echo reply", []byte{0, 0, 0xab, 0xcd, 0x12, 0x34, 0x00, 0x07, 'h', 'i'}, true,
			parsedICMP{kind: ReplyEcho, id: 0x1234, seq: 7}},
		{"time exceeded, plain header", icmpErrorMsg(11, 0, quotedIPv4(5, 1, quotedEcho)), true,
			parsedICMP{kind: ReplyTimeExceeded, id: 0x1234, seq: 7}},
		{"time exceeded, header with options", icmpErrorMsg(11, 0, quotedIPv4(6, 1, quotedEcho)), true,
			parsedICMP{kind: ReplyTimeExceeded, id: 0x1234, seq: 7}},
		{"host unreachable", icmpErrorMsg(3, 1, quotedIPv4(5, 1, quotedEcho)), true,
			parsedICMP{kind: ReplyUnreachable, id: 0x1234, seq: 7, code: 1}},
		{"an echo request is not a reply", quotedEcho, false, parsedICMP{}},
		{"time exceeded for a UDP datagram", icmpErrorMsg(11, 0, quotedIPv4(5, 17, quotedEcho)), false, parsedICMP{}},
		{"time exceeded quoting an echo reply", icmpErrorMsg(11, 0, quotedIPv4(5, 1, []byte{0, 0, 0, 0, 0x12, 0x34, 0, 7})), false, parsedICMP{}},
		{"quoted request cut short", icmpErrorMsg(11, 0, quotedIPv4(5, 1, quotedEcho[:4])), false, parsedICMP{}},
		{"quoted header cut short", icmpErrorMsg(11, 0, quotedIPv4(5, 1, nil)[:12]), false, parsedICMP{}},
		{"quoted IHL below 5", icmpErrorMsg(11, 0, append([]byte{0x44}, quotedIPv4(5, 1, quotedEcho)[1:]...)), false, parsedICMP{}},
		{"quoted IPv6 header", icmpErrorMsg(11, 0, append([]byte{0x65}, quotedIPv4(5, 1, quotedEcho)[1:]...)), false, parsedICMP{}},
		{"redirect", icmpErrorMsg(5, 0, quotedIPv4(5, 1, quotedEcho)), false, parsedICMP{}},
		{"seven bytes", []byte{0, 0, 0, 0, 0, 0, 0}, false, parsedICMP{}},
		{"empty", nil, false, parsedICMP{}},
	}
	for _, c := range cases {
		got, ok := parseICMPv4(c.b)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// onesSum is the Internet checksum's one's-complement sum; a message with a
// correct checksum sums to 0xffff.
func onesSum(b []byte) uint16 {
	var s uint32
	for i := 0; i+1 < len(b); i += 2 {
		s += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		s += uint32(b[len(b)-1]) << 8
	}
	for s>>16 != 0 {
		s = s&0xffff + s>>16
	}
	return uint16(s)
}

func TestEchoRequest(t *testing.T) {
	for _, size := range []int{0, 56, 57, 1472} {
		b, err := echoRequest(0x1234, 0x10007, size) // seq wraps to 16 bits
		if err != nil {
			t.Fatal(err)
		}
		if len(b) != 8+size {
			t.Errorf("size %d: %d bytes, want %d", size, len(b), 8+size)
		}
		if onesSum(b) != 0xffff {
			t.Errorf("size %d: bad checksum", size)
		}
		m, err := icmp.ParseMessage(1, b)
		if err != nil {
			t.Fatal(err)
		}
		e, ok := m.Body.(*icmp.Echo)
		if m.Type != ipv4.ICMPTypeEcho || !ok || e.ID != 0x1234 || e.Seq != 7 {
			t.Errorf("size %d: %+v %+v", size, m, e)
		}
		// What a router would quote back is recognised as ours.
		got, ok := parseICMPv4(icmpErrorMsg(11, 0, quotedIPv4(5, 1, b[:8])))
		if !ok || got.id != 0x1234 || got.seq != 7 {
			t.Errorf("size %d: quoted request parsed as %+v, %v", size, got, ok)
		}
	}
}
```

Create `backend/internal/nettools/icmp_unix_test.go`:

```go
//go:build !windows

package nettools

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

var loopback = net.IPv4(127, 0, 0, 1)

// openProber opens the real prober, skipping the test where this user may
// not send ICMP (no root or NET_RAW, and ping_group_range excludes it).
func openProber(t *testing.T) Prober {
	t.Helper()
	p, err := NewProber()
	if errors.Is(err, ErrICMPUnavailable) {
		t.Skip("ICMP is not available to this user")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func skipIfDenied(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		t.Skipf("sending ICMP is not permitted here: %v", err)
	}
}

func TestProberLoopbackEcho(t *testing.T) {
	p := openProber(t)
	for i := 0; i < 2; i++ {
		r, err := p.Echo(context.Background(), loopback, 64, 56, 2*time.Second)
		skipIfDenied(t, err)
		if err != nil {
			t.Fatalf("probe %d: %v", i+1, err)
		}
		if r.Kind != ReplyEcho || !r.From.Equal(loopback) || r.RTT <= 0 || r.RTT > 2*time.Second {
			t.Errorf("probe %d: %+v, want an echo reply from 127.0.0.1", i+1, r)
		}
	}
}

func TestProberConcurrentEchoes(t *testing.T) {
	p := openProber(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := p.Echo(context.Background(), loopback, 64, 16, 2*time.Second)
			if err == nil && r.Kind != ReplyEcho {
				err = errors.New("not an echo reply")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		skipIfDenied(t, err)
		if err != nil {
			t.Error(err)
		}
	}
}

func TestProberCancelledContext(t *testing.T) {
	p := openProber(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := p.Echo(ctx, loopback, 64, 56, 5*time.Second); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("took %v", d)
	}
}

func TestProberContextEndsTheWait(t *testing.T) {
	p := openProber(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	// 192.0.2.1 (TEST-NET-1) is never routed, so nothing should answer.
	_, err := p.Echo(ctx, net.IPv4(192, 0, 2, 1), 64, 56, 5*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Skipf("TEST-NET-1 gave %v here", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("the 5 s probe ignored its context: %v", d)
	}
}

func TestProberClosed(t *testing.T) {
	p := openProber(t)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := p.Echo(context.Background(), loopback, 64, 56, time.Second); err == nil {
		t.Error("Echo on a closed prober succeeded")
	}
}
```

Create `backend/internal/nettools/icmp_windows_test.go` (runs only on Windows; the cross-compiled vet in Step 6 type-checks it):

```go
package nettools

import (
	"testing"
	"unsafe"
)

// The structs passed to IcmpSendEcho must match the C layout.
func TestICMPStructLayout(t *testing.T) {
	var r icmpEchoReply
	if unsafe.Sizeof(uintptr(0)) == 8 {
		if unsafe.Offsetof(r.Data) != 16 || unsafe.Offsetof(r.Options) != 24 || unsafe.Sizeof(r) != 40 {
			t.Errorf("ICMP_ECHO_REPLY: Data at %d, Options at %d, size %d; want 16, 24, 40",
				unsafe.Offsetof(r.Data), unsafe.Offsetof(r.Options), unsafe.Sizeof(r))
		}
		if unsafe.Sizeof(ipOptionInformation{}) != 16 {
			t.Errorf("IP_OPTION_INFORMATION is %d bytes, want 16", unsafe.Sizeof(ipOptionInformation{}))
		}
		return
	}
	if unsafe.Offsetof(r.Options) != 20 || unsafe.Sizeof(r) != 28 {
		t.Errorf("32-bit ICMP_ECHO_REPLY: Options at %d, size %d; want 20, 28", unsafe.Offsetof(r.Options), unsafe.Sizeof(r))
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run (from `backend/`): `go test ./internal/nettools/ -run 'ICMP|Prober|EchoRequest' -v`
Expected: the build fails with `undefined: parsedICMP`, `undefined: parseICMPv4`, `undefined: echoRequest`, `undefined: Prober` and similar.

- [ ] **Step 3: Write the shared ICMP types and the parser**

Create `backend/internal/nettools/icmp.go`. The message-type constants are named `icmpType…` so they do not collide with the Windows `icmpEchoReply` struct:

```go
package nettools

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// ReplyKind is what answered an echo request.
type ReplyKind int

const (
	ReplyEcho         ReplyKind = iota + 1 // echo reply from the destination
	ReplyTimeExceeded                      // a router on the way: TTL ran out
	ReplyUnreachable                       // destination unreachable (Code says why)
)

// EchoReply is the answer to one echo request.
type EchoReply struct {
	Kind ReplyKind
	From net.IP
	RTT  time.Duration
	TTL  int // the reply's IP TTL, 0 when unknown
	Code int // ICMP code (unreachable)
}

var (
	ErrNoReply         = errors.New("no reply")
	ErrICMPUnavailable = errors.New("ICMP isn't available here (needs root or NET_RAW)")
)

// Prober sends ICMP echo requests. Safe for concurrent use.
type Prober interface {
	// Echo sends one echo request to dst with the given IP TTL and payload
	// size, and waits up to timeout for the matching reply: an echo reply, or
	// a time-exceeded/unreachable message quoting this request.
	// ErrNoReply when nothing matching arrives in time.
	Echo(ctx context.Context, dst net.IP, ttl, size int, timeout time.Duration) (EchoReply, error)
	// CanTrace reports whether time-exceeded messages are visible (raw ICMP
	// or the Windows ICMP API; not the unprivileged socket).
	CanTrace() bool
	Close() error
}

// ICMPv4 message types this package reads or writes.
const (
	icmpTypeEchoReply    = 0
	icmpTypeUnreachable  = 3
	icmpTypeEchoRequest  = 8
	icmpTypeTimeExceeded = 11
	ipProtocolICMP       = 1
	ipv4MinHeaderBytes   = 20
)

// parsedICMP is a received ICMP message that answers one of our echo
// requests: the echo reply itself, or an error message quoting the request.
type parsedICMP struct {
	kind    ReplyKind
	id, seq int
	code    int
}

// parseICMPv4 reads an ICMPv4 message (without its IP header). Echo replies
// carry the id and sequence in their own header; time-exceeded and
// unreachable messages quote the IPv4 header and the first 8 bytes of the
// datagram that caused them, which for our probes is the echo request's
// header. Anything else, or anything too short, is not ours: false.
func parseICMPv4(b []byte) (parsedICMP, bool) {
	if len(b) < 8 {
		return parsedICMP{}, false
	}
	switch b[0] {
	case icmpTypeEchoReply:
		return parsedICMP{
			kind: ReplyEcho,
			id:   int(binary.BigEndian.Uint16(b[4:6])),
			seq:  int(binary.BigEndian.Uint16(b[6:8])),
			code: int(b[1]),
		}, true
	case icmpTypeTimeExceeded, icmpTypeUnreachable:
		q := b[8:] // the quoted datagram
		if len(q) < ipv4MinHeaderBytes || q[0]>>4 != 4 {
			return parsedICMP{}, false
		}
		ihl := int(q[0]&0x0f) * 4
		if ihl < ipv4MinHeaderBytes || len(q) < ihl+8 || q[9] != ipProtocolICMP {
			return parsedICMP{}, false
		}
		req := q[ihl:]
		if req[0] != icmpTypeEchoRequest {
			return parsedICMP{}, false
		}
		kind := ReplyTimeExceeded
		if b[0] == icmpTypeUnreachable {
			kind = ReplyUnreachable
		}
		return parsedICMP{
			kind: kind,
			id:   int(binary.BigEndian.Uint16(req[4:6])),
			seq:  int(binary.BigEndian.Uint16(req[6:8])),
			code: int(b[1]),
		}, true
	}
	return parsedICMP{}, false
}

// echoRequest builds an ICMPv4 echo request, checksum included, with a
// payload of size bytes.
func echoRequest(id, seq, size int) ([]byte, error) {
	data := make([]byte, size)
	const fill = "SENTINEL"
	for i := range data {
		data[i] = fill[i%len(fill)]
	}
	m := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Body: &icmp.Echo{ID: id & 0xffff, Seq: seq & 0xffff, Data: data},
	}
	return m.Marshal(nil)
}
```

- [ ] **Step 4: Write the Linux/Docker prober**

Create `backend/internal/nettools/icmp_unix.go`:

```go
//go:build !windows

package nettools

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// unixProber shares one ICMP socket between all of a run's probes. A raw
// socket ("ip4:icmp", root or NET_RAW) sees time-exceeded messages, so it can
// trace. The unprivileged datagram socket ("udp4", allowed by Linux's
// net.ipv4.ping_group_range) only sees echo replies to itself, and the kernel
// rewrites the echo id, so replies are matched on the sequence alone.
type unixProber struct {
	conn *icmp.PacketConn
	p4   *ipv4.PacketConn
	raw  bool
	id   int

	writeMu sync.Mutex // SetTTL + WriteTo must not interleave

	mu      sync.Mutex
	seq     uint16
	waiters map[probeKey]chan received

	done      chan struct{}
	closeOnce sync.Once
}

type probeKey struct{ id, seq int } // id is 0 on the unprivileged socket

type received struct {
	msg  parsedICMP
	from net.IP
	ttl  int
	at   time.Time
}

// NewProber opens the platform prober; ErrICMPUnavailable if it cannot.
func NewProber() (Prober, error) {
	raw := true
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		raw = false
		if conn, err = icmp.ListenPacket("udp4", "0.0.0.0"); err != nil {
			return nil, ErrICMPUnavailable
		}
	}
	p := &unixProber{
		conn:    conn,
		p4:      conn.IPv4PacketConn(),
		raw:     raw,
		id:      1 + rand.IntN(0xfffe),
		waiters: map[probeKey]chan received{},
		done:    make(chan struct{}),
	}
	// Best effort: without it the reply TTL is reported as 0.
	_ = p.p4.SetControlMessage(ipv4.FlagTTL, true)
	go p.readLoop()
	return p, nil
}

func (p *unixProber) CanTrace() bool { return p.raw }

func (p *unixProber) Close() error {
	var err error
	p.closeOnce.Do(func() {
		close(p.done)
		err = p.conn.Close()
	})
	return err
}

// readLoop hands every reply that answers one of our probes to its waiter.
func (p *unixProber) readLoop() {
	buf := make([]byte, 1500)
	for {
		n, cm, src, err := p.p4.ReadFrom(buf)
		if err != nil {
			select {
			case <-p.done:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		at := time.Now()
		msg, ok := parseICMPv4(buf[:n])
		if !ok {
			continue
		}
		key := probeKey{seq: msg.seq}
		if p.raw {
			key.id = msg.id
		}
		p.mu.Lock()
		ch, ok := p.waiters[key]
		delete(p.waiters, key)
		p.mu.Unlock()
		if !ok {
			continue
		}
		r := received{msg: msg, from: addrIP(src), at: at}
		if cm != nil {
			r.ttl = cm.TTL
		}
		ch <- r // buffered: never blocks
	}
}

func addrIP(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPAddr:
		return v.IP
	case *net.UDPAddr:
		return v.IP
	}
	return nil
}

func (p *unixProber) Echo(ctx context.Context, dst net.IP, ttl, size int, timeout time.Duration) (EchoReply, error) {
	if err := ctx.Err(); err != nil {
		return EchoReply{}, err
	}
	dst4 := dst.To4()
	if dst4 == nil {
		return EchoReply{}, fmt.Errorf("%v is not an IPv4 address", dst)
	}

	p.mu.Lock()
	p.seq++
	seq := int(p.seq)
	key := probeKey{seq: seq}
	if p.raw {
		key.id = p.id
	}
	ch := make(chan received, 1)
	p.waiters[key] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.waiters, key)
		p.mu.Unlock()
	}()

	msg, err := echoRequest(p.id, seq, size)
	if err != nil {
		return EchoReply{}, err
	}
	var to net.Addr = &net.IPAddr{IP: dst4}
	if !p.raw {
		to = &net.UDPAddr{IP: dst4}
	}
	p.writeMu.Lock()
	if err := p.p4.SetTTL(ttl); err != nil {
		p.writeMu.Unlock()
		return EchoReply{}, fmt.Errorf("setting the TTL: %w", err)
	}
	sent := time.Now()
	_, err = p.conn.WriteTo(msg, to)
	p.writeMu.Unlock()
	if err != nil {
		return EchoReply{}, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return EchoReply{Kind: r.msg.kind, From: r.from, RTT: r.at.Sub(sent), TTL: r.ttl, Code: r.msg.code}, nil
	case <-timer.C:
		return EchoReply{}, ErrNoReply
	case <-ctx.Done():
		return EchoReply{}, ctx.Err()
	case <-p.done:
		return EchoReply{}, net.ErrClosed
	}
}
```

- [ ] **Step 5: Write the Windows prober**

Create `backend/internal/nettools/icmp_windows.go`:

```go
package nettools

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows ICMP API (iphlpapi.dll) sends echo requests without raw
// sockets or administrator rights and reports TTL-exceeded replies with the
// router's address, so it serves both ping and traceroute. It is called
// directly (no CGO); NewLazySystemDLL loads the DLL from System32 only.
var (
	modiphlpapi         = windows.NewLazySystemDLL("iphlpapi.dll")
	procIcmpCreateFile  = modiphlpapi.NewProc("IcmpCreateFile")
	procIcmpSendEcho    = modiphlpapi.NewProc("IcmpSendEcho")
	procIcmpCloseHandle = modiphlpapi.NewProc("IcmpCloseHandle")
)

// IP_STATUS values (ipexport.h).
const (
	ipSuccess             = 0
	ipDestNetUnreachable  = 11002
	ipDestHostUnreachable = 11003
	ipDestProtUnreachable = 11004
	ipDestPortUnreachable = 11005
	ipReqTimedOut         = 11010
	ipTTLExpiredTransit   = 11013
	ipTTLExpiredReassem   = 11014
)

// ipOptionInformation mirrors IP_OPTION_INFORMATION. OptionsData is a
// pointer, so on 64-bit Windows it sits at offset 8 and the struct is 16
// bytes; uintptr gives the right layout on both 32- and 64-bit.
type ipOptionInformation struct {
	TTL         uint8
	TOS         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData uintptr
}

// icmpEchoReply mirrors ICMP_ECHO_REPLY: on amd64 Address 0, Status 4,
// RoundTripTime 8, DataSize 12, Reserved 14, Data 16, Options 24 (size 40).
type icmpEchoReply struct {
	Address       uint32 // IPAddr: the address bytes in network order
	Status        uint32
	RoundTripTime uint32 // milliseconds
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       ipOptionInformation
}

type windowsProber struct{}

// NewProber opens the platform prober; ErrICMPUnavailable if it cannot.
func NewProber() (Prober, error) {
	if err := procIcmpSendEcho.Find(); err != nil {
		return nil, ErrICMPUnavailable
	}
	h, err := icmpCreateFile()
	if err != nil {
		return nil, ErrICMPUnavailable
	}
	icmpCloseHandle(h)
	return windowsProber{}, nil
}

func (windowsProber) CanTrace() bool { return true }
func (windowsProber) Close() error   { return nil }

func icmpCreateFile() (windows.Handle, error) {
	r, _, err := procIcmpCreateFile.Call()
	if h := windows.Handle(r); h != windows.InvalidHandle {
		return h, nil
	}
	return 0, err
}

func icmpCloseHandle(h windows.Handle) {
	_, _, _ = procIcmpCloseHandle.Call(uintptr(h))
}

// Echo runs the blocking IcmpSendEcho in a goroutine so ctx can end the wait
// at once; the call itself finishes on its own within timeout.
func (windowsProber) Echo(ctx context.Context, dst net.IP, ttl, size int, timeout time.Duration) (EchoReply, error) {
	if err := ctx.Err(); err != nil {
		return EchoReply{}, err
	}
	dst4 := dst.To4()
	if dst4 == nil {
		return EchoReply{}, fmt.Errorf("%v is not an IPv4 address", dst)
	}
	type result struct {
		reply EchoReply
		err   error
	}
	done := make(chan result, 1)
	go func() {
		r, err := sendEcho(dst4, ttl, size, timeout)
		done <- result{r, err}
	}()
	select {
	case res := <-done:
		return res.reply, res.err
	case <-ctx.Done():
		return EchoReply{}, ctx.Err()
	}
}

// sendEcho sends one echo request through its own ICMP handle, so concurrent
// probes never share one.
func sendEcho(dst4 net.IP, ttl, size int, timeout time.Duration) (EchoReply, error) {
	h, err := icmpCreateFile()
	if err != nil {
		return EchoReply{}, ErrICMPUnavailable
	}
	defer icmpCloseHandle(h)

	payload := make([]byte, size+1) // never empty, so &payload[0] is valid
	const fill = "SENTINEL"
	for i := range payload {
		payload[i] = fill[i%len(fill)]
	}
	opts := ipOptionInformation{TTL: uint8(ttl)}
	// Room for one reply, the echoed payload, an 8-byte ICMP error and slack.
	replySize := int(unsafe.Sizeof(icmpEchoReply{})) + size + 8 + 64
	buf := make([]uint64, (replySize+7)/8) // uint64s keep the reply 8-byte aligned
	ms := timeout.Milliseconds()
	if ms < 1 {
		ms = 1
	}

	start := time.Now()
	n, _, callErr := procIcmpSendEcho.Call(
		uintptr(h),
		uintptr(binary.LittleEndian.Uint32(dst4)),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(size),
		uintptr(unsafe.Pointer(&opts)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)*8),
		uintptr(ms),
	)
	rtt := time.Since(start)
	if n == 0 {
		var errno syscall.Errno
		if errors.As(callErr, &errno) && errno == ipReqTimedOut {
			return EchoReply{}, ErrNoReply
		}
		return EchoReply{}, fmt.Errorf("IcmpSendEcho: %w", callErr)
	}

	reply := (*icmpEchoReply)(unsafe.Pointer(&buf[0]))
	var from [4]byte
	binary.LittleEndian.PutUint32(from[:], reply.Address)
	out := EchoReply{From: net.IPv4(from[0], from[1], from[2], from[3]).To4(), RTT: rtt, TTL: int(reply.Options.TTL)}
	switch reply.Status {
	case ipSuccess:
		out.Kind = ReplyEcho
	case ipTTLExpiredTransit, ipTTLExpiredReassem:
		out.Kind = ReplyTimeExceeded
		out.Code = int(reply.Status - ipTTLExpiredTransit)
	case ipDestNetUnreachable, ipDestHostUnreachable, ipDestProtUnreachable, ipDestPortUnreachable:
		out.Kind = ReplyUnreachable
		out.Code = int(reply.Status - ipDestNetUnreachable)
	case ipReqTimedOut:
		return EchoReply{}, ErrNoReply
	default:
		return EchoReply{}, fmt.Errorf("ICMP status %d", reply.Status)
	}
	return out, nil
}
```

- [ ] **Step 6: Run the tests and the Windows cross-check**

Run (from `backend/`): `go test ./internal/nettools/ -run 'ICMP|Prober|EchoRequest' -v`
Expected: `TestParseICMPv4` and `TestEchoRequest` PASS. The `TestProber…` tests PASS where this user may send ICMP, and SKIP with "ICMP is not available to this user" where it may not. In the golang:1.26-alpine container as a non-root user they PASS over the unprivileged socket.

Run: `GOOS=windows go vet ./internal/nettools/... && go vet ./internal/nettools/... && gofmt -l ./internal/nettools`
Expected: no output.

If you can run as root (for example `docker run` without `-u`, which keeps `NET_RAW`), `go test ./internal/nettools/ -run Prober -v` exercises the raw-socket path too. This is optional.

- [ ] **Step 7: Commit**

From the repository root:

```bash
git add backend/internal/nettools/icmp.go backend/internal/nettools/icmp_unix.go backend/internal/nettools/icmp_windows.go backend/internal/nettools/icmp_test.go backend/internal/nettools/icmp_unix_test.go backend/internal/nettools/icmp_windows_test.go
git commit -m "feat(tools): ICMP echo probers for Linux/Docker (raw or unprivileged) and Windows (IcmpSendEcho)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 3: Ping and traceroute

This task builds the two ICMP tools on top of a `Prober`, plus a scripted fake prober for deterministic tests.

**Ping** sends probe *i* at `start + (i-1) × interval`, whatever happened to the earlier probes. Each probe runs in its own goroutine and waits up to the probe timeout, with TTL 64. Results go back to the ping goroutine, which emits each one as soon as it completes. Events therefore arrive in completion order, each carrying its `seq`, and `emit` is only ever called from one goroutine. The outcomes are:

- An echo reply is a `reply` event.
- `ErrNoReply` is a `timeout` event.
- An ICMP error is an `error` event, worded "host unreachable from 10.0.0.1", "port unreachable from …", "communication administratively prohibited from …" or "TTL exceeded in transit from …".
- Any other probe error is an `error` event carrying the error text.

When the context ends, the summary counts only the probes that completed; probes cut short by the cancellation are neither emitted nor counted. Loss is rounded to 1 decimal and RTTs to the microsecond (3 decimals of a millisecond). Jitter and the population stdev follow ruling 5.

**Traceroute** returns `ErrICMPUnavailable` at once when `!p.CanTrace()`, before sending anything. A round sends one probe per TTL in parallel and ends when all of them have answered or timed out (ruling 3). Rounds start at least `gap` apart; `Run` passes `traceRoundGap` (1 s) and the tests pass 0. The writer's decisions for rows (ruling 2):

- The **end of the path** is the lowest TTL answered either by the destination (an echo reply, or any reply whose `From` is the target) or by an *unreachable* message from anyone, since an unreachable probe went no further. Without the second half, a router that answers "host unreachable" for every TTL from 5 to 30 would produce 26 identical rows. `Reached` is true only when the destination itself answered.
- Once the end is known, later rounds probe only TTL 1 to the end, and rows stop there. Without an end, rows run to `min(max_hops, last TTL that ever answered + 1)`, at least 1.
- `hop` events are emitted as probes complete. In round 1, a probe for a TTL past the end is dropped if the end was already known when it arrived. Probes that arrive before the end is known (parallel probes past the destination get echo replies too) are still emitted, so the browser builds its table from `round_done`, never from `hop` events.
- After each round, `round_done` carries the cumulative `[]HopStats` (ruling 4).
- Hop names (ruling 17): each new hop address is looked up once in the background with a 1 s timeout. A name emits `hop_name` and fills `HopStats.Name` (the name of the first address of the row, in sorted order, that has one). A failed or empty lookup emits nothing. Before the summary, traceroute waits for outstanding lookups, which are bounded by the timeout.
- `hopStats` is a pure function. Addresses are de-duplicated and sorted numerically. `Last` is the most recent *answer*. `Avg` and `Stdev` are rounded to 3 decimals. `Best`, `Worst` and `Last` are the measured values. With no answers the RTT fields are nil, and `Addrs` is `[]`, never nil.

The two helpers every tool shares (`durationMS`, `round1`, `round3`, `ptr`) live at the bottom of `ping.go`; Tasks 4 and 5 use them.

**Files:**
- Create: `backend/internal/nettools/ping.go`
- Create: `backend/internal/nettools/traceroute.go`
- Test: `backend/internal/nettools/fakeprober_test.go`
- Test: `backend/internal/nettools/ping_test.go`
- Test: `backend/internal/nettools/traceroute_test.go`
- Test: `backend/internal/nettools/loopback_unix_test.go`

**Interfaces:**
- Consumes: Task 1 (`Spec`, `Params`, `Event`, `Emitter`, the event and summary types, `PingSizeDefault`; test helper `intp`); Task 2 (`Prober`, `EchoReply`, `ReplyKind`, `ErrNoReply`, `ErrICMPUnavailable`; test helper `openProber`).
- Produces (unexported; Task 5's `Runner.Run` calls these exactly):
  - `func ping(ctx context.Context, p Prober, s Spec, emit Emitter) (PingSummary, error)`
  - `type lookupFunc func(ctx context.Context, addr string) (string, error)`
  - `func traceroute(ctx context.Context, p Prober, s Spec, emit Emitter, lookup lookupFunc, gap time.Duration) (TraceSummary, error)`. `lookup` may be nil (no names).
  - `const traceRoundGap = time.Second`
  - `func pingSummary(sent int, rtts []float64) PingSummary`, `type hopSample struct{ addr string; rtt *float64 }`, `func hopStats(ttl int, probes []hopSample) HopStats`.
  - Shared helpers: `durationMS(d time.Duration) float64` (milliseconds, 3 decimals), `round1`, `round3`, `ptr(x float64) *float64`.
  - Both tools return `ctx.Err()` with the summary so far when the context ends.
  - Test helpers for Tasks 4–5: `fakeReply`, `fakeProber` (with `probesAt(ttl)`, `totalCalls()`, `sendTimes()`, `closed`), `route(target string, routers ...string)`, `recorder` (with `emit`, `ofType`, `onEmit`), `eqp(p *float64, want float64) bool`.

- [ ] **Step 1: Write the fake prober**

Create `backend/internal/nettools/fakeprober_test.go`:

```go
package nettools

import (
	"context"
	"net"
	"sync"
	"time"
)

// fakeReply scripts the answer to one probe.
type fakeReply struct {
	from  string    // who answers; "" = nobody (ErrNoReply at once)
	kind  ReplyKind // what they answer
	code  int
	rtt   time.Duration
	err   error         // returned as is when set
	block bool          // wait for the probe's context to end
	delay time.Duration // answer after this long (or when the context ends)
}

// fakeProber answers probes from a script, at once unless told otherwise.
// script gets the probe's number across the run (1-based, in call order), its
// TTL and how many probes have used that TTL so far including this one,
// which for a traceroute is the round. Like the real probers, it refuses a
// probe whose context has already ended.
type fakeProber struct {
	canTrace bool
	script   func(call, ttl, round int) fakeReply

	mu     sync.Mutex
	calls  int
	perTTL map[int]int
	starts []time.Time // when each probe was sent, in call order
	closed bool
}

func (f *fakeProber) Echo(ctx context.Context, dst net.IP, ttl, size int, timeout time.Duration) (EchoReply, error) {
	if err := ctx.Err(); err != nil {
		return EchoReply{}, err
	}
	f.mu.Lock()
	f.calls++
	if f.perTTL == nil {
		f.perTTL = map[int]int{}
	}
	f.perTTL[ttl]++
	f.starts = append(f.starts, time.Now())
	call, round := f.calls, f.perTTL[ttl]
	f.mu.Unlock()

	r := f.script(call, ttl, round)
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return EchoReply{}, ctx.Err()
		}
	}
	switch {
	case r.block:
		<-ctx.Done()
		return EchoReply{}, ctx.Err()
	case r.err != nil:
		return EchoReply{}, r.err
	case r.from == "":
		return EchoReply{}, ErrNoReply
	}
	return EchoReply{Kind: r.kind, Code: r.code, From: net.ParseIP(r.from).To4(), RTT: r.rtt, TTL: 57}, nil
}

func (f *fakeProber) CanTrace() bool { return f.canTrace }

func (f *fakeProber) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

// probesAt is how many probes used ttl.
func (f *fakeProber) probesAt(ttl int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.perTTL[ttl]
}

func (f *fakeProber) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeProber) sendTimes() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.starts...)
}

// route scripts a path: routers[i] answers TTL i+1 with time exceeded ("" =
// a router that never answers) and target answers every higher TTL with an
// echo reply. Every answer at TTL n takes n ms.
func route(target string, routers ...string) func(call, ttl, round int) fakeReply {
	return func(_, ttl, _ int) fakeReply {
		rtt := time.Duration(ttl) * time.Millisecond
		if ttl <= len(routers) {
			return fakeReply{from: routers[ttl-1], kind: ReplyTimeExceeded, rtt: rtt}
		}
		return fakeReply{from: target, kind: ReplyEcho, rtt: rtt}
	}
}

// recorder collects emitted events; it is only called from the tool's
// goroutine, as the Emitter contract says.
type recorder struct {
	events []Event
	onEmit func(Event)
}

func (r *recorder) emit(e Event) {
	r.events = append(r.events, e)
	if r.onEmit != nil {
		r.onEmit(e)
	}
}

func (r *recorder) ofType(t string) []any {
	var out []any
	for _, e := range r.events {
		if e.Type == t {
			out = append(out, e.Data)
		}
	}
	return out
}
```

- [ ] **Step 2: Write the failing tests**

Create `backend/internal/nettools/ping_test.go`. `ping` runs the spec it is given, so the tests use intervals below the 200 ms minimum and finish at once. The fake numbers probes in call order, so order-dependent figures (jitter) are pinned in `TestPingSummary` on the pure function, and the end-to-end tests check only order-independent figures:

```go
package nettools

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"
)

// eqp reports whether p holds want (to 1e-9).
func eqp(p *float64, want float64) bool { return p != nil && math.Abs(*p-want) < 1e-9 }

// pingSpec is a ping of count probes to 192.0.2.10. ping runs the spec it is
// given, so the tests use intervals below the 200 ms minimum to finish at once.
func pingSpec(count, intervalMS int) Spec {
	return Spec{Tool: ToolPing, Target: "192.0.2.10", TargetIP: "192.0.2.10",
		Params: Params{Count: count, IntervalMS: intervalMS, TimeoutMS: 100, Size: intp(56)}}
}

// seqs lists the seq of every ping event, ascending.
func seqs(r *recorder) []int {
	var out []int
	for _, e := range r.events {
		switch d := e.Data.(type) {
		case PingReply:
			out = append(out, d.Seq)
		case PingTimeout:
			out = append(out, d.Seq)
		case PingError:
			out = append(out, d.Seq)
		}
	}
	sort.Ints(out)
	return out
}

func TestPingSummary(t *testing.T) {
	s := pingSummary(4, []float64{10, 20, 15, 30})
	// Jitter: (|20-10| + |15-20| + |30-15|) / 3 = 30 / 3 = 10.
	if s.Sent != 4 || s.Received != 4 || s.LossPct != 0 || !eqp(s.MinMS, 10) || !eqp(s.AvgMS, 18.75) ||
		!eqp(s.MaxMS, 30) || !eqp(s.JitterMS, 10) {
		t.Errorf("all replies: %+v", s)
	}
	s = pingSummary(5, []float64{10.5, 12.25, 11})
	// Loss 2/5; avg 33.75/3 = 11.25; jitter (1.75 + 1.25) / 2 = 1.5.
	if s.Received != 3 || s.LossPct != 40 || !eqp(s.MinMS, 10.5) || !eqp(s.AvgMS, 11.25) || !eqp(s.MaxMS, 12.25) || !eqp(s.JitterMS, 1.5) {
		t.Errorf("some lost: %+v", s)
	}
	s = pingSummary(3, []float64{1, 2, 2})
	// Avg 5/3 rounds to 1.667; jitter (1 + 0) / 2 = 0.5; loss 0.
	if !eqp(s.AvgMS, 1.667) || !eqp(s.JitterMS, 0.5) || s.LossPct != 0 {
		t.Errorf("rounding: %+v", s)
	}
	s = pingSummary(3, []float64{7})
	// Loss 2/3 = 66.67 % rounds to 66.7; one reply has no jitter.
	if s.LossPct != 66.7 || !eqp(s.MinMS, 7) || !eqp(s.AvgMS, 7) || !eqp(s.MaxMS, 7) || s.JitterMS != nil {
		t.Errorf("one reply: %+v", s)
	}
	if s := pingSummary(2, nil); !reflect.DeepEqual(s, PingSummary{Sent: 2, LossPct: 100}) {
		t.Errorf("no replies: %+v", s)
	}
	if s := pingSummary(0, nil); !reflect.DeepEqual(s, PingSummary{}) {
		t.Errorf("nothing sent: %+v", s)
	}
}

func TestPingAllReplies(t *testing.T) {
	// The n-th probe sent takes n ms: RTTs 1, 2, 3, 4 in some order.
	f := &fakeProber{script: func(call, _, _ int) fakeReply {
		return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: time.Duration(call) * time.Millisecond}
	}}
	var r recorder
	sum, err := ping(context.Background(), f, pingSpec(4, 1), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Sent != 4 || sum.Received != 4 || sum.LossPct != 0 || !eqp(sum.MinMS, 1) || !eqp(sum.AvgMS, 2.5) ||
		!eqp(sum.MaxMS, 4) || sum.JitterMS == nil {
		t.Errorf("summary %+v", sum)
	}
	if got := seqs(&r); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Errorf("seqs %v, want 1..4 once each", got)
	}
	for _, d := range r.ofType(EventReply) {
		if rep := d.(PingReply); rep.From != "192.0.2.10" || rep.TTL != 57 || rep.RTTMS < 1 || rep.RTTMS > 4 {
			t.Errorf("reply %+v", rep)
		}
	}
}

func TestPingSomeLost(t *testing.T) {
	f := &fakeProber{script: func(call, _, _ int) fakeReply {
		if call == 2 || call == 4 {
			return fakeReply{} // nobody answers
		}
		return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: 10 * time.Millisecond}
	}}
	var r recorder
	sum, err := ping(context.Background(), f, pingSpec(5, 1), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Sent != 5 || sum.Received != 3 || sum.LossPct != 40 || !eqp(sum.MinMS, 10) || !eqp(sum.AvgMS, 10) ||
		!eqp(sum.MaxMS, 10) || !eqp(sum.JitterMS, 0) {
		t.Errorf("summary %+v", sum)
	}
	if n, m := len(r.ofType(EventReply)), len(r.ofType(EventTimeout)); n != 3 || m != 2 {
		t.Errorf("%d replies and %d timeouts, want 3 and 2", n, m)
	}
	if got := seqs(&r); !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5}) {
		t.Errorf("seqs %v", got)
	}
}

func TestPingNoReplies(t *testing.T) {
	f := &fakeProber{script: func(int, int, int) fakeReply { return fakeReply{} }}
	var r recorder
	sum, err := ping(context.Background(), f, pingSpec(3, 1), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sum, PingSummary{Sent: 3, LossPct: 100}) {
		t.Errorf("summary %+v", sum)
	}
	if len(r.ofType(EventTimeout)) != 3 {
		t.Errorf("events %+v", r.events)
	}
}

func TestPingErrors(t *testing.T) {
	f := &fakeProber{script: func(call, _, _ int) fakeReply {
		switch call {
		case 1:
			return fakeReply{from: "10.0.0.1", kind: ReplyUnreachable, code: 1}
		case 2:
			return fakeReply{from: "10.0.0.9", kind: ReplyTimeExceeded}
		}
		return fakeReply{err: errors.New("sendto: network is unreachable")}
	}}
	var r recorder
	sum, err := ping(context.Background(), f, pingSpec(3, 1), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range r.ofType(EventError) {
		msgs = append(msgs, d.(PingError).Message)
	}
	sort.Strings(msgs)
	want := []string{"TTL exceeded in transit from 10.0.0.9", "host unreachable from 10.0.0.1", "sendto: network is unreachable"}
	if !reflect.DeepEqual(msgs, want) {
		t.Errorf("messages %q, want %q", msgs, want)
	}
	if sum.Sent != 3 || sum.Received != 0 || sum.LossPct != 100 {
		t.Errorf("summary %+v", sum)
	}
}

// Probes go out on the interval, not after the previous reply.
func TestPingKeepsTheInterval(t *testing.T) {
	f := &fakeProber{script: func(int, int, int) fakeReply {
		return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: time.Millisecond, delay: 120 * time.Millisecond}
	}}
	var r recorder
	if _, err := ping(context.Background(), f, pingSpec(3, 30), r.emit); err != nil {
		t.Fatal(err)
	}
	sent := f.sendTimes()
	if len(sent) != 3 {
		t.Fatalf("%d probes", len(sent))
	}
	// On the interval: sent at 0, 30 and 60 ms. After each reply: 0, 120, 240.
	if gap := sent[2].Sub(sent[0]); gap < 55*time.Millisecond || gap > 200*time.Millisecond {
		t.Errorf("third probe sent %v after the first, want about 60 ms", gap)
	}
}

func TestPingCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeProber{script: func(call, _, _ int) fakeReply {
		if call <= 2 {
			return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: 5 * time.Millisecond}
		}
		return fakeReply{block: true}
	}}
	r := recorder{}
	r.onEmit = func(Event) {
		if len(r.ofType(EventReply)) == 2 {
			cancel()
		}
	}
	sum, err := ping(ctx, f, pingSpec(5, 1), r.emit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if sum.Sent != 2 || sum.Received != 2 || sum.LossPct != 0 || !eqp(sum.AvgMS, 5) {
		t.Errorf("partial summary %+v, want the two answered probes", sum)
	}
	if len(r.ofType(EventTimeout))+len(r.ofType(EventError)) != 0 {
		t.Errorf("cancelled probes were reported: %+v", r.events)
	}
}
```

Create `backend/internal/nettools/traceroute_test.go`. The fake answers by TTL and round, which is deterministic because rounds are sequential:

```go
package nettools

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func traceSpec(maxHops, rounds int) Spec {
	return Spec{Tool: ToolTraceroute, Target: "192.0.2.10", TargetIP: "192.0.2.10",
		Params: Params{MaxHops: maxHops, Rounds: rounds, TimeoutMS: 100}}
}

func sample(addr string, rtt float64) hopSample { return hopSample{addr: addr, rtt: ptr(rtt)} }

func TestHopStats(t *testing.T) {
	// RTTs 10, 20, 30: avg 20; stdev sqrt((100 + 0 + 100) / 3) = 8.16497 → 8.165.
	h := hopStats(4, []hopSample{sample("10.0.0.1", 10), {}, sample("10.0.0.2", 20), sample("10.0.0.1", 30)})
	if h.TTL != 4 || !reflect.DeepEqual(h.Addrs, []string{"10.0.0.1", "10.0.0.2"}) || h.Sent != 4 || h.Received != 3 ||
		h.LossPct != 25 || !eqp(h.LastMS, 30) || !eqp(h.AvgMS, 20) || !eqp(h.BestMS, 10) || !eqp(h.WorstMS, 30) ||
		!eqp(h.StdevMS, 8.165) {
		t.Errorf("mixed: %+v", h)
	}
	// Addresses sort numerically; Last is the latest answer, not a later timeout.
	// RTTs 10, 20: avg 15, stdev 5; loss 1/3 = 33.3 %.
	h = hopStats(2, []hopSample{sample("10.0.0.10", 10), sample("10.0.0.9", 20), {}})
	if !reflect.DeepEqual(h.Addrs, []string{"10.0.0.9", "10.0.0.10"}) || !eqp(h.LastMS, 20) || h.LossPct != 33.3 ||
		!eqp(h.AvgMS, 15) || !eqp(h.StdevMS, 5) {
		t.Errorf("sorting and last: %+v", h)
	}
	h = hopStats(3, []hopSample{{}, {}})
	if h.Addrs == nil || len(h.Addrs) != 0 || h.Sent != 2 || h.Received != 0 || h.LossPct != 100 ||
		h.LastMS != nil || h.AvgMS != nil || h.BestMS != nil || h.WorstMS != nil || h.StdevMS != nil {
		t.Errorf("all lost: %+v", h)
	}
	h = hopStats(1, []hopSample{sample("10.0.0.1", 4.5)})
	if !eqp(h.AvgMS, 4.5) || !eqp(h.StdevMS, 0) || h.LossPct != 0 {
		t.Errorf("one answer: %+v", h)
	}
}

func roundDones(r *recorder) []RoundDone {
	var out []RoundDone
	for _, d := range r.ofType(EventRoundDone) {
		out = append(out, d.(RoundDone))
	}
	return out
}

func hopProbes(r *recorder) []HopProbe {
	var out []HopProbe
	for _, d := range r.ofType(EventHop) {
		out = append(out, d.(HopProbe))
	}
	return out
}

func TestTracerouteReachesDestination(t *testing.T) {
	f := &fakeProber{canTrace: true, script: route("192.0.2.10", "10.0.0.1", "10.0.1.1")}
	var r recorder
	sum, err := traceroute(context.Background(), f, traceSpec(30, 3), r.emit, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Reached || sum.HopCount != 3 || len(sum.Hops) != 3 {
		t.Fatalf("summary %+v", sum)
	}
	want := []struct {
		addr string
		avg  float64
	}{{"10.0.0.1", 1}, {"10.0.1.1", 2}, {"192.0.2.10", 3}}
	for i, w := range want {
		h := sum.Hops[i]
		if h.TTL != i+1 || !reflect.DeepEqual(h.Addrs, []string{w.addr}) || h.Sent != 3 || h.Received != 3 || !eqp(h.AvgMS, w.avg) {
			t.Errorf("hop %d: %+v", i+1, h)
		}
	}
	// Round 1 probes all 30 TTLs; once the destination is known at TTL 3,
	// rounds 2 and 3 probe only TTLs 1-3.
	if f.probesAt(3) != 3 || f.probesAt(4) != 1 || f.probesAt(30) != 1 || f.totalCalls() != 36 {
		t.Errorf("probes: ttl3 %d, ttl4 %d, ttl30 %d, total %d", f.probesAt(3), f.probesAt(4), f.probesAt(30), f.totalCalls())
	}
	rounds := roundDones(&r)
	if len(rounds) != 3 {
		t.Fatalf("%d round_done events", len(rounds))
	}
	for i, rd := range rounds {
		if rd.Round != i+1 || len(rd.Hops) != 3 || rd.Hops[0].Sent != i+1 {
			t.Errorf("round_done %d: %+v (the table must be cumulative and stop at the destination)", i+1, rd)
		}
	}
	later := 0
	for _, p := range hopProbes(&r) {
		if p.Round > 1 {
			later++
			if p.TTL > 3 {
				t.Errorf("round %d probed TTL %d past the destination", p.Round, p.TTL)
			}
		}
		if (p.TTL >= 3) != p.Reached {
			t.Errorf("probe %+v: reached is true from the destination's TTL on", p)
		}
	}
	if later != 6 {
		t.Errorf("%d hop events in rounds 2-3, want 6", later)
	}
	// Each round's hop events come before its round_done.
	round := 1
	for _, e := range r.events {
		switch d := e.Data.(type) {
		case HopProbe:
			if d.Round != round {
				t.Fatalf("hop of round %d during round %d", d.Round, round)
			}
		case RoundDone:
			round++
		}
	}
}

func TestTracerouteNotReached(t *testing.T) {
	// Routers answer at TTLs 1, 2 and 4; nothing answers past 4 and the
	// target is never reached within 10 hops.
	f := &fakeProber{canTrace: true, script: route("192.0.2.10",
		"10.0.0.1", "10.0.0.2", "", "10.0.0.4", "", "", "", "", "", "")}
	var r recorder
	sum, err := traceroute(context.Background(), f, traceSpec(10, 2), r.emit, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Rows run to the last TTL that answered (4) + 1.
	if sum.Reached || sum.HopCount != 5 || len(sum.Hops) != 5 {
		t.Fatalf("summary %+v", sum)
	}
	if h := sum.Hops[2]; h.LossPct != 100 || len(h.Addrs) != 0 || h.Sent != 2 {
		t.Errorf("silent hop 3: %+v", h)
	}
	if h := sum.Hops[4]; h.TTL != 5 || h.LossPct != 100 {
		t.Errorf("row 5: %+v", h)
	}
	// Without a destination every round probes every TTL.
	if f.probesAt(10) != 2 || f.totalCalls() != 20 {
		t.Errorf("ttl10 %d, total %d", f.probesAt(10), f.totalCalls())
	}
}

func TestTracerouteSplitPathAndNames(t *testing.T) {
	f := &fakeProber{canTrace: true, script: func(call, ttl, round int) fakeReply {
		switch {
		case ttl == 1:
			return fakeReply{from: "10.0.0.1", kind: ReplyTimeExceeded, rtt: time.Millisecond}
		case ttl == 2 && round == 1:
			return fakeReply{from: "10.0.2.2", kind: ReplyTimeExceeded, rtt: 2 * time.Millisecond}
		case ttl == 2:
			return fakeReply{from: "10.0.2.1", kind: ReplyTimeExceeded, rtt: 4 * time.Millisecond}
		}
		return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: 5 * time.Millisecond}
	}}
	var mu sync.Mutex
	asked := map[string]int{}
	names := map[string]string{"10.0.0.1": "gw.lab", "10.0.2.1": "core-a.lab", "10.0.2.2": "core-b.lab"}
	lookup := func(_ context.Context, addr string) (string, error) {
		mu.Lock()
		asked[addr]++
		mu.Unlock()
		if n, ok := names[addr]; ok {
			return n, nil
		}
		return "", errors.New("no PTR record")
	}
	var r recorder
	sum, err := traceroute(context.Background(), f, traceSpec(30, 2), r.emit, lookup, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h := sum.Hops[1]; !reflect.DeepEqual(h.Addrs, []string{"10.0.2.1", "10.0.2.2"}) || h.Sent != 2 || h.Received != 2 || !eqp(h.AvgMS, 3) {
		t.Errorf("split hop: %+v", h)
	}
	mu.Lock()
	wantAsked := map[string]int{"10.0.0.1": 1, "10.0.2.1": 1, "10.0.2.2": 1, "192.0.2.10": 1}
	if !reflect.DeepEqual(asked, wantAsked) {
		t.Errorf("lookups %v, want one per address %v", asked, wantAsked)
	}
	mu.Unlock()
	got := map[string]HopName{}
	for _, d := range r.ofType(EventHopName) {
		n := d.(HopName)
		if _, dup := got[n.Addr]; dup {
			t.Errorf("hop_name for %s twice", n.Addr)
		}
		got[n.Addr] = n
	}
	wantNames := map[string]HopName{
		"10.0.0.1": {TTL: 1, Addr: "10.0.0.1", Name: "gw.lab"},
		"10.0.2.1": {TTL: 2, Addr: "10.0.2.1", Name: "core-a.lab"},
		"10.0.2.2": {TTL: 2, Addr: "10.0.2.2", Name: "core-b.lab"},
	}
	if !reflect.DeepEqual(got, wantNames) {
		t.Errorf("hop_name events %v, want %v", got, wantNames)
	}
	if sum.Hops[0].Name != "gw.lab" || sum.Hops[1].Name != "core-a.lab" || sum.Hops[2].Name != "" {
		t.Errorf("names in the table: %q %q %q", sum.Hops[0].Name, sum.Hops[1].Name, sum.Hops[2].Name)
	}
}

func TestTracerouteNeedsRawICMP(t *testing.T) {
	f := &fakeProber{canTrace: false, script: route("192.0.2.10")}
	var r recorder
	_, err := traceroute(context.Background(), f, traceSpec(30, 1), r.emit, nil, 0)
	if !errors.Is(err, ErrICMPUnavailable) || len(r.events) != 0 || f.totalCalls() != 0 {
		t.Errorf("err %v, %d events, %d probes; want ErrICMPUnavailable and nothing sent", err, len(r.events), f.totalCalls())
	}
}

func TestTracerouteUnreachableEndsThePath(t *testing.T) {
	// A router at TTL 2 answers "host unreachable" for every TTL from 2 on.
	f := &fakeProber{canTrace: true, script: func(_, ttl, _ int) fakeReply {
		if ttl == 1 {
			return fakeReply{from: "10.0.0.1", kind: ReplyTimeExceeded, rtt: time.Millisecond}
		}
		return fakeReply{from: "10.0.0.2", kind: ReplyUnreachable, code: 1, rtt: 2 * time.Millisecond}
	}}
	var r recorder
	sum, err := traceroute(context.Background(), f, traceSpec(30, 2), r.emit, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Reached || sum.HopCount != 2 || !reflect.DeepEqual(sum.Hops[1].Addrs, []string{"10.0.0.2"}) || f.probesAt(3) != 1 {
		t.Errorf("router unreachable: %+v, ttl3 probed %d times", sum, f.probesAt(3))
	}

	// The destination itself answering "port unreachable" counts as reached.
	f = &fakeProber{canTrace: true, script: func(_, ttl, _ int) fakeReply {
		if ttl < 3 {
			return fakeReply{from: "10.0.0.1", kind: ReplyTimeExceeded, rtt: time.Millisecond}
		}
		return fakeReply{from: "192.0.2.10", kind: ReplyUnreachable, code: 3, rtt: time.Millisecond}
	}}
	sum, err = traceroute(context.Background(), f, traceSpec(30, 1), r.emit, nil, 0)
	if err != nil || !sum.Reached || sum.HopCount != 3 {
		t.Errorf("destination unreachable: %+v, %v", sum, err)
	}
}

func TestTracerouteRoundsAreSpaced(t *testing.T) {
	f := &fakeProber{canTrace: true, script: route("192.0.2.10", "10.0.0.1")}
	var r recorder
	start := time.Now()
	if _, err := traceroute(context.Background(), f, traceSpec(5, 3), r.emit, nil, 60*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 120*time.Millisecond {
		t.Errorf("3 rounds took %v, want at least 2 gaps of 60 ms", d)
	}
}

func TestTracerouteCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeProber{canTrace: true, script: route("192.0.2.10", "10.0.0.1", "10.0.0.2")}
	r := recorder{}
	r.onEmit = func(e Event) {
		if e.Type == EventRoundDone {
			cancel()
		}
	}
	sum, err := traceroute(ctx, f, traceSpec(30, 3), r.emit, nil, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(roundDones(&r)) != 1 || !sum.Reached || sum.HopCount != 3 || sum.Hops[0].Sent != 1 {
		t.Errorf("partial summary %+v after %d rounds, want round 1's table", sum, len(roundDones(&r)))
	}
}
```

Create `backend/internal/nettools/loopback_unix_test.go` (real probes; they skip without ICMP, and the traceroute one also skips without raw ICMP):

```go
//go:build !windows

package nettools

import (
	"context"
	"reflect"
	"testing"
)

// Real probes to 127.0.0.1, skipped where this user may not send ICMP.

func TestPingLoopback(t *testing.T) {
	p := openProber(t)
	s := Spec{Tool: ToolPing, Target: "127.0.0.1", TargetIP: "127.0.0.1",
		Params: Params{Count: 2, IntervalMS: 200, TimeoutMS: 1000, Size: intp(56)}}
	var r recorder
	sum, err := ping(context.Background(), p, s, r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if errs := r.ofType(EventError); len(errs) > 0 {
		t.Skipf("ICMP refused here: %+v", errs[0])
	}
	if sum.Sent != 2 || sum.Received != 2 || sum.LossPct != 0 || sum.MinMS == nil {
		t.Errorf("summary %+v, events %+v", sum, r.events)
	}
}

func TestTracerouteLoopback(t *testing.T) {
	p := openProber(t)
	if !p.CanTrace() {
		t.Skip("traceroute needs raw ICMP (root or NET_RAW)")
	}
	s := Spec{Tool: ToolTraceroute, Target: "127.0.0.1", TargetIP: "127.0.0.1",
		Params: Params{MaxHops: 3, Rounds: 1, TimeoutMS: 1000}}
	var r recorder
	sum, err := traceroute(context.Background(), p, s, r.emit, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Reached || sum.HopCount != 1 || !reflect.DeepEqual(sum.Hops[0].Addrs, []string{"127.0.0.1"}) {
		t.Errorf("summary %+v", sum)
	}
}
```

- [ ] **Step 3: Run them and watch them fail**

Run (from `backend/`): `go test ./internal/nettools/ -run 'Ping|Trace|Hop' -v`
Expected: the build fails with `undefined: ping`, `undefined: pingSummary`, `undefined: traceroute`, `undefined: hopStats`, `undefined: hopSample`, `undefined: ptr` and similar.

- [ ] **Step 4: Write ping**

Create `backend/internal/nettools/ping.go`:

```go
package nettools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"time"
)

// pingTTL is the IP TTL of ping probes.
const pingTTL = 64

// ping sends s.Params.Count echo requests to s.TargetIP, probe i at
// start + (i-1) × interval whatever happened to the earlier ones, each
// waiting up to the probe timeout. Probes run concurrently; each result is
// emitted as soon as it is known, so events arrive in completion order and
// carry their seq. s must be normalized and TargetIP an IPv4 address.
func ping(ctx context.Context, p Prober, s Spec, emit Emitter) (PingSummary, error) {
	dst := net.ParseIP(s.TargetIP).To4()
	count := s.Params.Count
	interval := time.Duration(s.Params.IntervalMS) * time.Millisecond
	timeout := time.Duration(s.Params.TimeoutMS) * time.Millisecond
	size := PingSizeDefault
	if s.Params.Size != nil {
		size = *s.Params.Size
	}

	type result struct {
		seq   int
		reply EchoReply
		err   error
	}
	results := make(chan result, count) // never blocks a probe, even after we return
	rtts := make([]*float64, count+1)   // by seq; nil = no echo reply
	completed, pending, next := 0, 0, 1
	start := time.Now()
	timer := time.NewTimer(0)
	defer timer.Stop()

	for (next <= count || pending > 0) && ctx.Err() == nil {
		var tick <-chan time.Time
		if next <= count {
			tick = timer.C
		}
		select {
		case <-ctx.Done():
		case <-tick:
			seq := next
			next++
			pending++
			go func() {
				r, err := p.Echo(ctx, dst, pingTTL, size, timeout)
				results <- result{seq, r, err}
			}()
			if next <= count {
				timer.Reset(time.Until(start.Add(time.Duration(next-1) * interval)))
			}
		case res := <-results:
			pending--
			if ctx.Err() != nil && res.err != nil {
				continue // cut short by the cancellation: not a real outcome
			}
			completed++
			switch {
			case res.err == nil && res.reply.Kind == ReplyEcho:
				ms := durationMS(res.reply.RTT)
				rtts[res.seq] = &ms
				emit(Event{Type: EventReply, Data: PingReply{Seq: res.seq, RTTMS: ms, TTL: res.reply.TTL, From: res.reply.From.String()}})
			case res.err == nil:
				emit(Event{Type: EventError, Data: PingError{Seq: res.seq, Message: replyProblem(res.reply)}})
			case errors.Is(res.err, ErrNoReply):
				emit(Event{Type: EventTimeout, Data: PingTimeout{Seq: res.seq}})
			default:
				emit(Event{Type: EventError, Data: PingError{Seq: res.seq, Message: res.err.Error()}})
			}
		}
	}
	var inOrder []float64
	for _, r := range rtts {
		if r != nil {
			inOrder = append(inOrder, *r)
		}
	}
	return pingSummary(completed, inOrder), ctx.Err()
}

// replyProblem words an ICMP error that answered a ping probe.
func replyProblem(r EchoReply) string {
	if r.Kind == ReplyTimeExceeded {
		return fmt.Sprintf("TTL exceeded in transit from %s", r.From)
	}
	what := "destination unreachable"
	switch r.Code {
	case 0:
		what = "network unreachable"
	case 1:
		what = "host unreachable"
	case 2:
		what = "protocol unreachable"
	case 3:
		what = "port unreachable"
	case 9, 10, 13:
		what = "communication administratively prohibited"
	}
	return fmt.Sprintf("%s from %s", what, r.From)
}

// pingSummary sums up sent completed probes whose echo replies took rtts
// (milliseconds, in seq order).
func pingSummary(sent int, rtts []float64) PingSummary {
	s := PingSummary{Sent: sent, Received: len(rtts)}
	if sent > 0 {
		s.LossPct = round1(float64(sent-len(rtts)) / float64(sent) * 100)
	}
	if len(rtts) == 0 {
		return s
	}
	lo, hi, sum := rtts[0], rtts[0], 0.0
	for _, v := range rtts {
		lo, hi, sum = math.Min(lo, v), math.Max(hi, v), sum+v
	}
	s.MinMS, s.MaxMS, s.AvgMS = ptr(round3(lo)), ptr(round3(hi)), ptr(round3(sum/float64(len(rtts))))
	if len(rtts) >= 2 {
		var diffs float64
		for i := 1; i < len(rtts); i++ {
			diffs += math.Abs(rtts[i] - rtts[i-1])
		}
		s.JitterMS = ptr(round3(diffs / float64(len(rtts)-1)))
	}
	return s
}

// Helpers shared by the tools.

// durationMS is d in milliseconds, to the microsecond.
func durationMS(d time.Duration) float64 { return round3(float64(d) / float64(time.Millisecond)) }

func round1(x float64) float64 { return math.Round(x*10) / 10 }
func round3(x float64) float64 { return math.Round(x*1000) / 1000 }
func ptr(x float64) *float64   { return &x }
```

- [ ] **Step 5: Write traceroute**

Create `backend/internal/nettools/traceroute.go`:

```go
package nettools

import (
	"bytes"
	"context"
	"math"
	"net"
	"sort"
	"sync"
	"time"
)

const (
	// traceRoundGap is the least time between the starts of two rounds.
	traceRoundGap = time.Second
	// hopNameTimeout bounds each reverse lookup of a hop address.
	hopNameTimeout = time.Second
	// traceSize is the payload size of traceroute probes.
	traceSize = 32
)

// lookupFunc finds the name of an address ("" when it has none).
type lookupFunc func(ctx context.Context, addr string) (string, error)

// hopSample is one probe's outcome at one TTL: who answered and how fast.
// addr is "" and rtt nil when nothing answered.
type hopSample struct {
	addr string
	rtt  *float64
}

// traceState is what a traceroute has learned so far.
type traceState struct {
	maxHops      int
	samples      map[int][]hopSample // by TTL, in round order
	stopTTL      int                 // lowest TTL whose probe went no further (0 = none yet)
	reached      bool                // the destination itself answered
	lastAnswered int                 // highest TTL that ever answered
	names        map[string]string   // addr → reverse name
}

// traceroute runs an MTR-style trace to s.TargetIP: each round sends one
// echo request per TTL, all in parallel, and ends when every probe has
// answered or timed out; rounds start at least gap apart. The path ends at
// the first TTL answered by the destination (or by an unreachable message,
// which means the probe went no further); later rounds probe only up to it
// and rows past it are dropped. Without an end, rows run to the last TTL
// that ever answered + 1 (at most max hops). After each round a RoundDone
// carries the cumulative table. Hop names are looked up once per address in
// the background. s must be normalized and TargetIP an IPv4 address.
func traceroute(ctx context.Context, p Prober, s Spec, emit Emitter, lookup lookupFunc, gap time.Duration) (TraceSummary, error) {
	if !p.CanTrace() {
		return TraceSummary{}, ErrICMPUnavailable
	}
	dst := net.ParseIP(s.TargetIP).To4()
	timeout := time.Duration(s.Params.TimeoutMS) * time.Millisecond
	st := &traceState{maxHops: s.Params.MaxHops, samples: map[int][]hopSample{}, names: map[string]string{}}

	// Reverse lookups run in the background and report here; the buffer
	// holds one name per possible probe, so a lookup never blocks.
	names := make(chan HopName, s.Params.MaxHops*s.Params.Rounds)
	var lookups sync.WaitGroup
	asked := map[string]bool{}
	lookUp := func(ttl int, addr string) {
		if lookup == nil || addr == "" || asked[addr] {
			return
		}
		asked[addr] = true
		lookups.Add(1)
		go func() {
			defer lookups.Done()
			lctx, cancel := context.WithTimeout(ctx, hopNameTimeout)
			defer cancel()
			if name, err := lookup(lctx, addr); err == nil && name != "" {
				names <- HopName{TTL: ttl, Addr: addr, Name: name}
			}
		}()
	}
	gotName := func(n HopName) {
		st.names[n.Addr] = n.Name
		emit(Event{Type: EventHopName, Data: n})
	}

	type result struct {
		ttl   int
		reply EchoReply
		err   error
	}
	for round := 1; round <= s.Params.Rounds; round++ {
		roundStart := time.Now()
		limit := st.maxHops
		if st.stopTTL > 0 {
			limit = st.stopTTL
		}
		results := make(chan result, limit)
		for ttl := 1; ttl <= limit; ttl++ {
			go func() {
				r, err := p.Echo(ctx, dst, ttl, traceSize, timeout)
				results <- result{ttl, r, err}
			}()
		}
		for left := limit; left > 0; {
			select {
			case n := <-names:
				gotName(n)
			case res := <-results:
				left--
				if ctx.Err() != nil && res.err != nil {
					continue // cut short by the cancellation
				}
				probe := HopProbe{Round: round, TTL: res.ttl}
				sample := hopSample{}
				if res.err == nil {
					ms := durationMS(res.reply.RTT)
					sample = hopSample{addr: res.reply.From.String(), rtt: &ms}
					probe.Addr, probe.RTTMS = sample.addr, sample.rtt
					probe.Reached = res.reply.Kind == ReplyEcho || res.reply.From.Equal(dst)
					st.answered(res.ttl, probe.Reached || res.reply.Kind == ReplyUnreachable, probe.Reached)
				}
				st.samples[res.ttl] = append(st.samples[res.ttl], sample)
				if st.stopTTL == 0 || res.ttl <= st.stopTTL {
					emit(Event{Type: EventHop, Data: probe})
				}
				lookUp(res.ttl, sample.addr)
			}
		}
		if err := ctx.Err(); err != nil {
			return st.summary(), err
		}
		emit(Event{Type: EventRoundDone, Data: RoundDone{Round: round, Hops: st.rows()}})
		if round < s.Params.Rounds {
			wait := time.NewTimer(time.Until(roundStart.Add(gap)))
			for waiting := true; waiting; {
				select {
				case n := <-names:
					gotName(n)
				case <-wait.C:
					waiting = false
				case <-ctx.Done():
					wait.Stop()
					return st.summary(), ctx.Err()
				}
			}
		}
	}

	// Let the last lookups finish (each is bounded by hopNameTimeout).
	lookupsDone := make(chan struct{})
	go func() {
		lookups.Wait()
		close(lookupsDone)
	}()
	for waiting := true; waiting; {
		select {
		case n := <-names:
			gotName(n)
		case <-lookupsDone:
			waiting = false
		case <-ctx.Done():
			return st.summary(), ctx.Err()
		}
	}
	for len(names) > 0 {
		gotName(<-names)
	}
	return st.summary(), nil
}

// answered records an answer at ttl; ends says the path stops there.
func (st *traceState) answered(ttl int, ends, reached bool) {
	if ttl > st.lastAnswered {
		st.lastAnswered = ttl
	}
	if ends && (st.stopTTL == 0 || ttl < st.stopTTL) {
		st.stopTTL = ttl
	}
	if reached {
		st.reached = true
	}
}

// rows is the MTR table: TTL 1 to the end of the path, or, without an end,
// to the last TTL that ever answered + 1, at most max hops.
func (st *traceState) rows() []HopStats {
	n := st.stopTTL
	if n == 0 {
		n = min(st.maxHops, st.lastAnswered+1)
	}
	n = max(n, 1)
	out := make([]HopStats, 0, n)
	for ttl := 1; ttl <= n; ttl++ {
		h := hopStats(ttl, st.samples[ttl])
		for _, a := range h.Addrs {
			if name := st.names[a]; name != "" {
				h.Name = name
				break
			}
		}
		out = append(out, h)
	}
	return out
}

func (st *traceState) summary() TraceSummary {
	rows := st.rows()
	return TraceSummary{Reached: st.reached, HopCount: len(rows), Hops: rows}
}

// hopStats sums up the probes sent with one TTL (in round order): the
// addresses that answered (sorted), loss, and RTT figures over the answers.
// Last is the most recent answer; Stdev is the population standard deviation.
func hopStats(ttl int, probes []hopSample) HopStats {
	h := HopStats{TTL: ttl, Addrs: []string{}, Sent: len(probes)}
	seen := map[string]bool{}
	var rtts []float64
	for _, p := range probes {
		if p.addr != "" && !seen[p.addr] {
			seen[p.addr] = true
			h.Addrs = append(h.Addrs, p.addr)
		}
		if p.rtt != nil {
			rtts = append(rtts, *p.rtt)
		}
	}
	sort.Slice(h.Addrs, func(i, j int) bool {
		a, b := net.ParseIP(h.Addrs[i]).To4(), net.ParseIP(h.Addrs[j]).To4()
		if a == nil || b == nil {
			return h.Addrs[i] < h.Addrs[j]
		}
		return bytes.Compare(a, b) < 0
	})
	h.Received = len(rtts)
	if h.Sent > 0 {
		h.LossPct = round1(float64(h.Sent-h.Received) / float64(h.Sent) * 100)
	}
	if len(rtts) == 0 {
		return h
	}
	best, worst, sum := rtts[0], rtts[0], 0.0
	for _, v := range rtts {
		best, worst, sum = math.Min(best, v), math.Max(worst, v), sum+v
	}
	avg := sum / float64(len(rtts))
	var sq float64
	for _, v := range rtts {
		sq += (v - avg) * (v - avg)
	}
	h.LastMS = ptr(rtts[len(rtts)-1])
	h.AvgMS, h.BestMS, h.WorstMS = ptr(round3(avg)), ptr(best), ptr(worst)
	h.StdevMS = ptr(round3(math.Sqrt(sq / float64(len(rtts)))))
	return h
}
```

- [ ] **Step 6: Run the tests**

Run (from `backend/`): `go test ./internal/nettools/ -run 'Ping|Trace|Hop' -v`
Expected: PASS for `TestPingSummary`, `TestPingAllReplies`, `TestPingSomeLost`, `TestPingNoReplies`, `TestPingErrors`, `TestPingKeepsTheInterval`, `TestPingCancelled`, `TestHopStats`, `TestTracerouteReachesDestination`, `TestTracerouteNotReached`, `TestTracerouteSplitPathAndNames`, `TestTracerouteNeedsRawICMP`, `TestTracerouteUnreachableEndsThePath`, `TestTracerouteRoundsAreSpaced`, `TestTracerouteCancelled` (plus `TestNormalizePingTotal`, whose name matches). `TestPingLoopback` PASSes or SKIPs as `TestProber…` did in Task 2. `TestTracerouteLoopback` SKIPs without raw ICMP ("traceroute needs raw ICMP (root or NET_RAW)") and PASSes as root.

Run: `go test ./internal/nettools/... && GOOS=windows go vet ./internal/nettools/... && go vet ./internal/nettools/... && gofmt -l ./internal/nettools`
Expected: `ok` and no other output.

Where cgo is available (e.g. the `golang:1.26` Debian image), `CGO_ENABLED=1 go test -race -count=3 ./internal/nettools/` should also pass. This is optional.

- [ ] **Step 7: Commit**

From the repository root:

```bash
git add backend/internal/nettools/ping.go backend/internal/nettools/traceroute.go backend/internal/nettools/fakeprober_test.go backend/internal/nettools/ping_test.go backend/internal/nettools/traceroute_test.go backend/internal/nettools/loopback_unix_test.go
git commit -m "feat(tools): ping and MTR-style traceroute over the ICMP prober

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 4: DNS lookup

This task adds the DNS lookup tool and the system resolver address, using `golang.org/x/net/dns/dnsmessage` (no new library).

`dnsLookup` builds one question: a random id, recursion desired, class IN, and the type from `RecordType`. CAA is type 257, which `dnsmessage` does not name. For PTR, an IPv4 target becomes its `in-addr.arpa` name. Names are made fully qualified. The lookup has these behaviours:

- The query goes over UDP with a 4096-byte read buffer. Replies whose id, question or QR bit do not match are ignored until one does or the time runs out.
- When the matching reply has TC set, the query is repeated over TCP (2-byte length prefix), and the answer shown is the TCP one with `Truncated` and `TCP` both true. No EDNS0 is sent (ruling 7) and OPT records never appear in the sections.
- The whole exchange runs under `DNSTimeout` (5 s) inside the run's context. If the run's own context ends, its error is returned as is. If the 5 s pass, the error is "no answer from 192.0.2.53:53 within 5s". Any other failure is "asking …: …".
- An error RCODE (NXDOMAIN, SERVFAIL, …) is an answer, not an error: exactly one `answer` event is emitted. Sections are `[]`, never nil.
- Data is written out as dig writes it. TXT strings are quoted and space-joined. SOA is `ns mbox serial refresh retry expire minttl`, SRV is `priority weight port target`, CAA is `flags tag "value"`, and an unknown or malformed type is the hex of its data. Names keep their trailing dot.

`SystemResolver` reads the first IPv4 `nameserver` from `/etc/resolv.conf`; inside the backend container that is Docker's embedded DNS, `127.0.0.11`. On Windows it uses the first IPv4 DNS server of the first adapter that is up (`GetAdaptersAddresses`, the same buffer-growing loop as Go's `net` package). The address of a *named* server is chosen by the Runner in Task 5.

**Files:**
- Create: `backend/internal/nettools/dns.go`
- Create: `backend/internal/nettools/dns_system_unix.go`
- Create: `backend/internal/nettools/dns_system_windows.go`
- Test: `backend/internal/nettools/dns_test.go`
- Test: `backend/internal/nettools/dns_system_unix_test.go`

**Interfaces:**
- Consumes: Task 1 (`Spec`, `Params`, `Event`, `Emitter`, `EventAnswer`, `DNSRecord`, `DNSAnswer`, `DNSSummary`, `DNSTimeout`); Task 3 (`durationMS`; test helper `recorder`); `golang.org/x/net/dns/dnsmessage` (`NewBuilder`, `Builder.EnableCompression/StartQuestions/Question/Finish`, `Parser.Start/Question`, `Message.Unpack`, the `…Resource` body types, `UnknownResource`, `RCode…` constants, `ClassINET`, `TypeOPT`); `golang.org/x/sys/windows` (`GetAdaptersAddresses`, `IpAdapterAddresses`, `GAA_FLAG_…`, `AF_UNSPEC`, `IfOperStatusUp`, `ERROR_BUFFER_OVERFLOW`, `ERROR_NO_DATA`, `SocketAddress.IP`).
- Produces:
  - `func SystemResolver() (string, error)` (shared contract), returning `"ip:53"`.
  - `func dnsLookup(ctx context.Context, s Spec, server string, emit Emitter) (DNSSummary, error)`. `server` is `"ip:port"`; Task 5's `Runner.Run` calls it exactly.
  - Unexported, for tests: `parseResolvConf(r io.Reader) (string, error)` (non-Windows), `recordData`, `dnsTypeName`, `rcodeName`, `typeCAA`.
  - Test helpers for Task 5 (`dns_test.go`): `newDNSTestServer(t, answer func(q dnsmessage.Message, overTCP bool) []dnsmessage.Message) *dnsTestServer` (fields `addr`; methods `lastQuery()`, `tcpQueries()`), `dnsReply(q, rcode, answer, authority, additional)`, `rr(name, ttl, body)`, `mustName`, `dnsSpec(target, recordType)`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/nettools/dns_test.go`. The test server listens on one 127.0.0.1 port for both UDP and TCP, as a real server does:

```go
package nettools

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// dnsTestServer answers DNS queries over UDP and TCP on one 127.0.0.1 port.
// answer returns the messages to send back, in order (none = stay silent).
type dnsTestServer struct {
	addr   string
	udp    net.PacketConn
	tcp    net.Listener
	answer func(q dnsmessage.Message, overTCP bool) []dnsmessage.Message

	mu      sync.Mutex
	queries []dnsmessage.Message
	tcpSeen int
}

func newDNSTestServer(t *testing.T, answer func(q dnsmessage.Message, overTCP bool) []dnsmessage.Message) *dnsTestServer {
	t.Helper()
	s := &dnsTestServer{answer: answer}
	for i := 0; s.udp == nil; i++ {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		pc, err := net.ListenPacket("udp4", l.Addr().String())
		if err != nil {
			_ = l.Close()
			if i == 10 {
				t.Fatalf("no free UDP and TCP port pair: %v", err)
			}
			continue
		}
		s.tcp, s.udp, s.addr = l, pc, l.Addr().String()
	}
	go s.serveUDP()
	go s.serveTCP()
	t.Cleanup(func() {
		_ = s.udp.Close()
		_ = s.tcp.Close()
	})
	return s
}

func (s *dnsTestServer) record(q dnsmessage.Message, overTCP bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
	if overTCP {
		s.tcpSeen++
	}
}

func (s *dnsTestServer) lastQuery() dnsmessage.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queries[len(s.queries)-1]
}

func (s *dnsTestServer) tcpQueries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tcpSeen
}

func (s *dnsTestServer) serveUDP() {
	buf := make([]byte, 4096)
	for {
		n, from, err := s.udp.ReadFrom(buf)
		if err != nil {
			return
		}
		var q dnsmessage.Message
		if q.Unpack(buf[:n]) != nil {
			continue
		}
		s.record(q, false)
		for _, m := range s.answer(q, false) {
			if b, err := m.Pack(); err == nil {
				_, _ = s.udp.WriteTo(b, from)
			}
		}
	}
}

func (s *dnsTestServer) serveTCP() {
	for {
		c, err := s.tcp.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			var size [2]byte
			if _, err := io.ReadFull(c, size[:]); err != nil {
				return
			}
			b := make([]byte, binary.BigEndian.Uint16(size[:]))
			if _, err := io.ReadFull(c, b); err != nil {
				return
			}
			var q dnsmessage.Message
			if q.Unpack(b) != nil {
				return
			}
			s.record(q, true)
			for _, m := range s.answer(q, true) {
				if out, err := m.Pack(); err == nil {
					_, _ = c.Write(append(binary.BigEndian.AppendUint16(nil, uint16(len(out))), out...))
				}
			}
		}()
	}
}

// dnsReply answers q with rcode and the given sections.
func dnsReply(q dnsmessage.Message, rcode dnsmessage.RCode, answer, authority, additional []dnsmessage.Resource) dnsmessage.Message {
	return dnsmessage.Message{
		Header: dnsmessage.Header{ID: q.ID, Response: true, Authoritative: true,
			RecursionDesired: q.RecursionDesired, RecursionAvailable: true, RCode: rcode},
		Questions: q.Questions, Answers: answer, Authorities: authority, Additionals: additional,
	}
}

func rr(name string, ttl uint32, body dnsmessage.ResourceBody) dnsmessage.Resource {
	return dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(name), Class: dnsmessage.ClassINET, TTL: ttl},
		Body:   body,
	}
}

func mustName(s string) dnsmessage.Name { return dnsmessage.MustNewName(s) }

func dnsSpec(target, recordType string) Spec {
	return Spec{Tool: ToolDNS, Target: target, Params: Params{RecordType: recordType}}
}

// lookupOne runs dnsLookup against srv and returns its one answer event.
func lookupOne(t *testing.T, srv *dnsTestServer, s Spec) (DNSAnswer, DNSSummary) {
	t.Helper()
	var r recorder
	sum, err := dnsLookup(context.Background(), s, srv.addr, r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.events) != 1 || r.events[0].Type != EventAnswer {
		t.Fatalf("events %+v, want one answer", r.events)
	}
	return r.events[0].Data.(DNSAnswer), sum
}

func TestDNSLookupA(t *testing.T) {
	srv := newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeSuccess,
			[]dnsmessage.Resource{rr("www.example.org.", 300, &dnsmessage.AResource{A: [4]byte{192, 0, 2, 80}})},
			[]dnsmessage.Resource{rr("example.org.", 3600, &dnsmessage.NSResource{NS: mustName("ns1.example.org.")})},
			[]dnsmessage.Resource{rr("ns1.example.org.", 3600, &dnsmessage.AResource{A: [4]byte{192, 0, 2, 53}})})}
	})
	ans, sum := lookupOne(t, srv, dnsSpec("www.example.org", "A"))
	q := srv.lastQuery()
	if !q.RecursionDesired || len(q.Questions) != 1 || q.Questions[0].Type != dnsmessage.TypeA ||
		q.Questions[0].Class != dnsmessage.ClassINET || q.Questions[0].Name.String() != "www.example.org." {
		t.Errorf("query %+v", q)
	}
	want := DNSAnswer{
		Server: srv.addr, RCode: "NOERROR", Authoritative: true, RTTMS: ans.RTTMS,
		Answer:     []DNSRecord{{Name: "www.example.org.", Type: "A", TTL: 300, Data: "192.0.2.80"}},
		Authority:  []DNSRecord{{Name: "example.org.", Type: "NS", TTL: 3600, Data: "ns1.example.org."}},
		Additional: []DNSRecord{{Name: "ns1.example.org.", Type: "A", TTL: 3600, Data: "192.0.2.53"}},
	}
	if !reflect.DeepEqual(ans, want) {
		t.Errorf("answer\n got %+v\nwant %+v", ans, want)
	}
	if ans.RTTMS < 0 || ans.RTTMS > 1000 {
		t.Errorf("rtt %v ms", ans.RTTMS)
	}
	if sum != (DNSSummary{Server: srv.addr, RCode: "NOERROR", AnswerCount: 1, RTTMS: ans.RTTMS}) {
		t.Errorf("summary %+v", sum)
	}
}

func TestDNSLookupRecordFormats(t *testing.T) {
	bodies := map[dnsmessage.Type]dnsmessage.ResourceBody{
		dnsmessage.TypeAAAA:  &dnsmessage.AAAAResource{AAAA: [16]byte{0x20, 0x01, 0x0d, 0xb8, 15: 1}},
		dnsmessage.TypeCNAME: &dnsmessage.CNAMEResource{CNAME: mustName("target.example.org.")},
		dnsmessage.TypeMX:    &dnsmessage.MXResource{Pref: 10, MX: mustName("mail.example.org.")},
		dnsmessage.TypeNS:    &dnsmessage.NSResource{NS: mustName("ns1.example.org.")},
		dnsmessage.TypeTXT:   &dnsmessage.TXTResource{TXT: []string{"v=spf1 -all", `say "hi"`}},
		dnsmessage.TypeSOA: &dnsmessage.SOAResource{NS: mustName("ns1.example.org."), MBox: mustName("hostmaster.example.org."),
			Serial: 2026100501, Refresh: 7200, Retry: 3600, Expire: 1209600, MinTTL: 300},
		dnsmessage.TypeSRV: &dnsmessage.SRVResource{Priority: 10, Weight: 60, Port: 5060, Target: mustName("sip.example.org.")},
		dnsmessage.TypePTR: &dnsmessage.PTRResource{PTR: mustName("host.example.org.")},
		typeCAA:            &dnsmessage.UnknownResource{Type: typeCAA, Data: append([]byte{0, 5}, "issueletsencrypt.org"...)},
	}
	srv := newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		qq := q.Questions[0]
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeSuccess,
			[]dnsmessage.Resource{rr(qq.Name.String(), 60, bodies[qq.Type])}, nil, nil)}
	})
	cases := []struct{ recordType, data string }{
		{"AAAA", "2001:db8::1"},
		{"CNAME", "target.example.org."},
		{"MX", "10 mail.example.org."},
		{"NS", "ns1.example.org."},
		{"TXT", `"v=spf1 -all" "say \"hi\""`},
		{"SOA", "ns1.example.org. hostmaster.example.org. 2026100501 7200 3600 1209600 300"},
		{"SRV", "10 60 5060 sip.example.org."},
		{"PTR", "host.example.org."},
		{"CAA", `0 issue "letsencrypt.org"`},
	}
	for _, c := range cases {
		ans, _ := lookupOne(t, srv, dnsSpec("example.org", c.recordType))
		if len(ans.Answer) != 1 || ans.Answer[0].Type != c.recordType || ans.Answer[0].Data != c.data || ans.Answer[0].TTL != 60 {
			t.Errorf("%s: %+v, want data %q", c.recordType, ans.Answer, c.data)
		}
	}
}

func TestRecordDataOddCases(t *testing.T) {
	if got := recordData(&dnsmessage.UnknownResource{Type: 99, Data: []byte{0xde, 0xad}}); got != "dead" {
		t.Errorf("unknown type: %q, want hex", got)
	}
	if got := recordData(&dnsmessage.UnknownResource{Type: typeCAA, Data: []byte{0, 9, 'x'}}); got != "000978" {
		t.Errorf("malformed CAA: %q, want hex", got)
	}
	if got := dnsTypeName(99); got != "TYPE99" {
		t.Errorf("type name %q", got)
	}
	if got := rcodeName(9); got != "RCODE9" {
		t.Errorf("rcode name %q", got)
	}
}

func TestDNSLookupNXDOMAIN(t *testing.T) {
	srv := newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeNameError, nil,
			[]dnsmessage.Resource{rr("example.org.", 300, &dnsmessage.SOAResource{NS: mustName("ns1.example.org."),
				MBox: mustName("hostmaster.example.org."), Serial: 1, Refresh: 2, Retry: 3, Expire: 4, MinTTL: 5})}, nil)}
	})
	ans, sum := lookupOne(t, srv, dnsSpec("nope.example.org", "A"))
	if ans.RCode != "NXDOMAIN" || ans.Answer == nil || len(ans.Answer) != 0 || len(ans.Authority) != 1 ||
		ans.Authority[0].Data != "ns1.example.org. hostmaster.example.org. 1 2 3 4 5" {
		t.Errorf("answer %+v", ans)
	}
	if sum.RCode != "NXDOMAIN" || sum.AnswerCount != 0 {
		t.Errorf("summary %+v", sum)
	}
}

func TestDNSLookupTruncatedRetriesOverTCP(t *testing.T) {
	srv := newDNSTestServer(t, func(q dnsmessage.Message, overTCP bool) []dnsmessage.Message {
		if !overTCP {
			m := dnsReply(q, dnsmessage.RCodeSuccess, nil, nil, nil)
			m.Truncated = true
			return []dnsmessage.Message{m}
		}
		var rs []dnsmessage.Resource
		for i := byte(1); i <= 3; i++ {
			rs = append(rs, rr("big.example.org.", 60, &dnsmessage.AResource{A: [4]byte{192, 0, 2, i}}))
		}
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeSuccess, rs, nil, nil)}
	})
	ans, sum := lookupOne(t, srv, dnsSpec("big.example.org", "A"))
	if !ans.Truncated || !ans.TCP || len(ans.Answer) != 3 || ans.Answer[2].Data != "192.0.2.3" || srv.tcpQueries() != 1 {
		t.Errorf("answer %+v after %d TCP queries", ans, srv.tcpQueries())
	}
	if sum.AnswerCount != 3 {
		t.Errorf("summary %+v", sum)
	}
}

func TestDNSLookupPTRName(t *testing.T) {
	srv := newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeSuccess, nil, nil, nil)}
	})
	lookupOne(t, srv, dnsSpec("192.0.2.10", "PTR"))
	if got := srv.lastQuery().Questions[0].Name.String(); got != "10.2.0.192.in-addr.arpa." {
		t.Errorf("PTR of an address asked for %q", got)
	}
	lookupOne(t, srv, dnsSpec("10.2.0.192.in-addr.arpa", "PTR"))
	if got := srv.lastQuery().Questions[0].Name.String(); got != "10.2.0.192.in-addr.arpa." {
		t.Errorf("PTR of a name asked for %q", got)
	}
	lookupOne(t, srv, dnsSpec("192.0.2.10", "A"))
	if got := srv.lastQuery().Questions[0].Name.String(); got != "192.0.2.10." {
		t.Errorf("A of an address asked for %q", got)
	}
}

func TestDNSLookupIgnoresStrayReplies(t *testing.T) {
	srv := newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		a := func(last byte) []dnsmessage.Resource {
			return []dnsmessage.Resource{rr("www.example.org.", 60, &dnsmessage.AResource{A: [4]byte{192, 0, 2, last}})}
		}
		wrongID := dnsReply(q, dnsmessage.RCodeSuccess, a(1), nil, nil)
		wrongID.ID++
		wrongQuestion := dnsReply(q, dnsmessage.RCodeSuccess, a(2), nil, nil)
		wrongQuestion.Questions = []dnsmessage.Question{{Name: mustName("other.example.org."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}
		notAReply := dnsReply(q, dnsmessage.RCodeSuccess, a(4), nil, nil)
		notAReply.Response = false
		return []dnsmessage.Message{wrongID, wrongQuestion, notAReply, dnsReply(q, dnsmessage.RCodeSuccess, a(3), nil, nil)}
	})
	ans, _ := lookupOne(t, srv, dnsSpec("WWW.Example.org", "A"))
	if len(ans.Answer) != 1 || ans.Answer[0].Data != "192.0.2.3" {
		t.Errorf("answer %+v, want the reply that matches the query", ans.Answer)
	}
}

func TestDNSLookupSilentServer(t *testing.T) {
	srv := newDNSTestServer(t, func(dnsmessage.Message, bool) []dnsmessage.Message { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var r recorder
	start := time.Now()
	_, err := dnsLookup(ctx, dnsSpec("www.example.org", "A"), srv.addr, r.emit)
	if !errors.Is(err, context.DeadlineExceeded) || len(r.events) != 0 {
		t.Errorf("err %v, %d events; want the run's deadline and no answer", err, len(r.events))
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
}

func TestDNSLookupClosedPort(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().String()
	_ = pc.Close()
	var r recorder
	_, err = dnsLookup(context.Background(), dnsSpec("www.example.org", "A"), addr, r.emit)
	if err == nil || !strings.HasPrefix(err.Error(), "asking "+addr) && !strings.HasPrefix(err.Error(), "no answer from "+addr) {
		t.Errorf("err %v", err)
	}
}

func TestDNSLookupBadName(t *testing.T) {
	var r recorder
	_, err := dnsLookup(context.Background(), dnsSpec("a..b", "A"), "127.0.0.1:53", r.emit)
	if err == nil || err.Error() != `"a..b" is not a valid DNS name` {
		t.Errorf("err %v", err)
	}
}
```

Create `backend/internal/nettools/dns_system_unix_test.go`:

```go
//go:build !windows

package nettools

import (
	"strings"
	"testing"
)

func TestParseResolvConf(t *testing.T) {
	cases := []struct {
		name, conf, want, wantErr string
	}{
		{"first IPv4 after IPv6 ones", "# generated\nsearch lan\nnameserver fe80::1%eth0\nnameserver 2001:db8::53\n" +
			"nameserver 10.0.0.53\nnameserver 10.0.0.54\n", "10.0.0.53:53", ""},
		{"Docker's embedded DNS", "nameserver 127.0.0.11\noptions ndots:0\n", "127.0.0.11:53", ""},
		{"tabs and extra fields", "nameserver\t192.0.2.1  # office\n", "192.0.2.1:53", ""},
		{"only IPv6", "nameserver 2001:db8::53\n", "", "no IPv4 DNS server in /etc/resolv.conf"},
		{"no nameserver lines", "search lan\n", "", "no IPv4 DNS server in /etc/resolv.conf"},
		{"a bare keyword", "nameserver\n", "", "no IPv4 DNS server in /etc/resolv.conf"},
	}
	for _, c := range cases {
		got, err := parseResolvConf(strings.NewReader(c.conf))
		if got != c.want || (err == nil) != (c.wantErr == "") || err != nil && err.Error() != c.wantErr {
			t.Errorf("%s: %q, %v; want %q, %q", c.name, got, err, c.want, c.wantErr)
		}
	}
}

// The real file exists on every Unix-like test host and container.
func TestSystemResolver(t *testing.T) {
	addr, err := SystemResolver()
	if err != nil {
		t.Skipf("no usable /etc/resolv.conf here: %v", err)
	}
	if !strings.HasSuffix(addr, ":53") {
		t.Errorf("SystemResolver() = %q, want ip:53", addr)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run (from `backend/`): `go test ./internal/nettools/ -run 'DNS|Resolv|Record' -v`
Expected: the build fails with `undefined: dnsLookup`, `undefined: recordData`, `undefined: typeCAA`, `undefined: parseResolvConf` and similar.

- [ ] **Step 3: Write the lookup**

Create `backend/internal/nettools/dns.go`:

```go
package nettools

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// typeCAA is not among dnsmessage's named types.
const typeCAA dnsmessage.Type = 257

var dnsTypes = map[string]dnsmessage.Type{
	"A": dnsmessage.TypeA, "AAAA": dnsmessage.TypeAAAA, "CNAME": dnsmessage.TypeCNAME,
	"MX": dnsmessage.TypeMX, "NS": dnsmessage.TypeNS, "TXT": dnsmessage.TypeTXT,
	"SOA": dnsmessage.TypeSOA, "SRV": dnsmessage.TypeSRV, "PTR": dnsmessage.TypePTR, "CAA": typeCAA,
}

// dnsTypeName is the usual name of t ("MX"), or "TYPE<n>".
func dnsTypeName(t dnsmessage.Type) string {
	for name, v := range dnsTypes {
		if v == t {
			return name
		}
	}
	return "TYPE" + strconv.Itoa(int(t))
}

// rcodeName is the usual name of an RCODE ("NXDOMAIN"), or "RCODE<n>".
func rcodeName(r dnsmessage.RCode) string {
	switch r {
	case dnsmessage.RCodeSuccess:
		return "NOERROR"
	case dnsmessage.RCodeFormatError:
		return "FORMERR"
	case dnsmessage.RCodeServerFailure:
		return "SERVFAIL"
	case dnsmessage.RCodeNameError:
		return "NXDOMAIN"
	case dnsmessage.RCodeNotImplemented:
		return "NOTIMP"
	case dnsmessage.RCodeRefused:
		return "REFUSED"
	}
	return "RCODE" + strconv.Itoa(int(r))
}

// reverseName is the in-addr.arpa name of an IPv4 address.
func reverseName(ip net.IP) string {
	v4 := ip.To4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", v4[3], v4[2], v4[1], v4[0])
}

// dnsLookup asks server ("ip:port") for s.Target's records of type
// s.Params.RecordType, recursion desired, over UDP, and again over TCP when
// the UDP answer is truncated. It emits exactly one EventAnswer. A PTR
// lookup of an IPv4 address asks for its in-addr.arpa name. A reply with an
// error RCODE (NXDOMAIN, …) is an answer, not an error; no reply within
// DNSTimeout is. s must be normalized.
func dnsLookup(ctx context.Context, s Spec, server string, emit Emitter) (DNSSummary, error) {
	qtype := dnsTypes[s.Params.RecordType]
	qname := s.Target
	if ip := net.ParseIP(qname); qtype == dnsmessage.TypePTR && ip != nil && ip.To4() != nil {
		qname = reverseName(ip)
	}
	if !strings.HasSuffix(qname, ".") {
		qname += "."
	}
	name, err := dnsmessage.NewName(qname)
	if err != nil {
		return DNSSummary{}, fmt.Errorf("%q is not a valid DNS name", s.Target)
	}
	q := dnsmessage.Question{Name: name, Type: qtype, Class: dnsmessage.ClassINET}
	id := uint16(rand.Uint32())
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: id, RecursionDesired: true})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return DNSSummary{}, err
	}
	if err := b.Question(q); err != nil {
		return DNSSummary{}, fmt.Errorf("%q is not a valid DNS name", s.Target)
	}
	query, err := b.Finish()
	if err != nil {
		return DNSSummary{}, err
	}

	lctx, cancel := context.WithTimeout(ctx, DNSTimeout)
	defer cancel()
	resp, rtt, err := exchangeUDP(lctx, server, query, id, q)
	if err != nil {
		return DNSSummary{}, dnsError(ctx, lctx, server, err)
	}
	var p dnsmessage.Parser
	h, err := p.Start(resp)
	if err != nil {
		return DNSSummary{}, fmt.Errorf("bad answer from %s: %w", server, err)
	}
	ans := DNSAnswer{Server: server}
	if h.Truncated {
		ans.Truncated, ans.TCP = true, true
		if resp, rtt, err = exchangeTCP(lctx, server, query, id, q); err != nil {
			return DNSSummary{}, dnsError(ctx, lctx, server, err)
		}
	}
	var m dnsmessage.Message
	if err := m.Unpack(resp); err != nil {
		return DNSSummary{}, fmt.Errorf("bad answer from %s: %w", server, err)
	}
	ans.RCode = rcodeName(m.RCode)
	ans.Authoritative = m.Authoritative
	ans.RTTMS = durationMS(rtt)
	ans.Answer, ans.Authority, ans.Additional = dnsRecords(m.Answers), dnsRecords(m.Authorities), dnsRecords(m.Additionals)
	emit(Event{Type: EventAnswer, Data: ans})
	return DNSSummary{Server: server, RCode: ans.RCode, AnswerCount: len(ans.Answer), RTTMS: ans.RTTMS}, nil
}

// dnsError turns an exchange error into what the user sees: the run's own
// cancellation or deadline as is, DNSTimeout as "no answer".
func dnsError(ctx, lctx context.Context, server string, err error) error {
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		<-lctx.Done() // the connection deadline is lctx's; it ends with it
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if lctx.Err() != nil {
		return fmt.Errorf("no answer from %s within %s", server, DNSTimeout)
	}
	return fmt.Errorf("asking %s: %w", server, err)
}

// exchangeUDP sends query and returns the first reply that answers it,
// ignoring stray datagrams, and the time it took.
func exchangeUDP(ctx context.Context, server string, query []byte, id uint16, q dnsmessage.Question) ([]byte, time.Duration, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", server)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()
	stop := watchConn(ctx, conn)
	defer stop()
	start := time.Now()
	if _, err := conn.Write(query); err != nil {
		return nil, 0, err
	}
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, 0, err
		}
		if answers(buf[:n], id, q) {
			return append([]byte(nil), buf[:n]...), time.Since(start), nil
		}
	}
}

// exchangeTCP sends query with its 2-byte length prefix and reads one reply.
func exchangeTCP(ctx context.Context, server string, query []byte, id uint16, q dnsmessage.Question) ([]byte, time.Duration, error) {
	var d net.Dialer
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp4", server)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()
	stop := watchConn(ctx, conn)
	defer stop()
	msg := binary.BigEndian.AppendUint16(nil, uint16(len(query)))
	if _, err := conn.Write(append(msg, query...)); err != nil {
		return nil, 0, err
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return nil, 0, err
	}
	resp := make([]byte, binary.BigEndian.Uint16(size[:]))
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, 0, err
	}
	if !answers(resp, id, q) {
		return nil, 0, errors.New("the TCP answer does not match the question")
	}
	return resp, time.Since(start), nil
}

// watchConn gives conn ctx's deadline and ends its reads when ctx ends.
func watchConn(ctx context.Context, conn net.Conn) (stop func() bool) {
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	return context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
}

// answers reports whether msg is a reply to our query: same id, and the
// same question.
func answers(msg []byte, id uint16, q dnsmessage.Question) bool {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || !h.Response || h.ID != id {
		return false
	}
	got, err := p.Question()
	return err == nil && got.Type == q.Type && got.Class == q.Class && strings.EqualFold(got.Name.String(), q.Name.String())
}

// dnsRecords writes out one section's records (never nil; OPT left out).
func dnsRecords(rs []dnsmessage.Resource) []DNSRecord {
	out := []DNSRecord{}
	for _, r := range rs {
		if r.Header.Type == dnsmessage.TypeOPT {
			continue
		}
		out = append(out, DNSRecord{
			Name: r.Header.Name.String(),
			Type: dnsTypeName(r.Header.Type),
			TTL:  r.Header.TTL,
			Data: recordData(r.Body),
		})
	}
	return out
}

// recordData writes a record's data the way dig does.
func recordData(body dnsmessage.ResourceBody) string {
	switch b := body.(type) {
	case *dnsmessage.AResource:
		return net.IP(b.A[:]).String()
	case *dnsmessage.AAAAResource:
		return net.IP(b.AAAA[:]).String()
	case *dnsmessage.CNAMEResource:
		return b.CNAME.String()
	case *dnsmessage.NSResource:
		return b.NS.String()
	case *dnsmessage.PTRResource:
		return b.PTR.String()
	case *dnsmessage.MXResource:
		return fmt.Sprintf("%d %s", b.Pref, b.MX)
	case *dnsmessage.TXTResource:
		parts := make([]string, len(b.TXT))
		for i, s := range b.TXT {
			parts[i] = strconv.Quote(s)
		}
		return strings.Join(parts, " ")
	case *dnsmessage.SOAResource:
		return fmt.Sprintf("%s %s %d %d %d %d %d", b.NS, b.MBox, b.Serial, b.Refresh, b.Retry, b.Expire, b.MinTTL)
	case *dnsmessage.SRVResource:
		return fmt.Sprintf("%d %d %d %s", b.Priority, b.Weight, b.Port, b.Target)
	case *dnsmessage.UnknownResource:
		if b.Type == typeCAA {
			if s, ok := caaData(b.Data); ok {
				return s
			}
		}
		return hex.EncodeToString(b.Data)
	}
	return ""
}

// caaData reads a CAA record: flags, tag length, tag, value (RFC 8659).
func caaData(d []byte) (string, bool) {
	if len(d) < 2 || len(d) < 2+int(d[1]) {
		return "", false
	}
	tag := string(d[2 : 2+int(d[1])])
	value := string(d[2+int(d[1]):])
	return fmt.Sprintf("%d %s %s", d[0], tag, strconv.Quote(value)), true
}
```

- [ ] **Step 4: Write the system resolver for Linux/Docker**

Create `backend/internal/nettools/dns_system_unix.go`:

```go
//go:build !windows

package nettools

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// SystemResolver returns "ip:port" of the host's first IPv4 DNS server: the
// first IPv4 nameserver in /etc/resolv.conf (inside a Docker container, the
// embedded DNS at 127.0.0.11).
func SystemResolver() (string, error) {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return "", fmt.Errorf("reading /etc/resolv.conf: %w", err)
	}
	defer f.Close()
	return parseResolvConf(f)
}

// parseResolvConf finds the first IPv4 "nameserver" line; IPv6 servers are
// skipped.
func parseResolvConf(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		if ip := net.ParseIP(fields[1]); ip != nil && ip.To4() != nil && !strings.Contains(fields[1], ":") {
			return net.JoinHostPort(ip.To4().String(), "53"), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("reading /etc/resolv.conf: %w", err)
	}
	return "", errors.New("no IPv4 DNS server in /etc/resolv.conf")
}
```

- [ ] **Step 5: Write the system resolver for Windows**

Create `backend/internal/nettools/dns_system_windows.go`:

```go
package nettools

import (
	"errors"
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SystemResolver returns "ip:port" of the host's first IPv4 DNS server: the
// first IPv4 DNS server of the first network adapter that is up.
func SystemResolver() (string, error) {
	const flags = windows.GAA_FLAG_SKIP_UNICAST | windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_SKIP_FRIENDLY_NAME
	size := uint32(15000)
	var buf []byte
	for {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if errors.Is(err, windows.ERROR_NO_DATA) {
			return "", errors.New("no network adapters")
		}
		if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) || size <= uint32(len(buf)) {
			return "", fmt.Errorf("GetAdaptersAddresses: %w", err)
		}
	}
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		if a.OperStatus != windows.IfOperStatusUp {
			continue
		}
		for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
			if ip := d.Address.IP().To4(); ip != nil {
				return net.JoinHostPort(ip.String(), "53"), nil
			}
		}
	}
	return "", errors.New("no IPv4 DNS server on any network adapter that is up")
}
```

- [ ] **Step 6: Run the tests and the Windows cross-check**

Run (from `backend/`): `go test ./internal/nettools/ -run 'DNS|Resolv|Record' -v`
Expected: PASS for `TestDNSLookupA`, `TestDNSLookupRecordFormats`, `TestRecordDataOddCases`, `TestDNSLookupNXDOMAIN`, `TestDNSLookupTruncatedRetriesOverTCP`, `TestDNSLookupPTRName`, `TestDNSLookupIgnoresStrayReplies`, `TestDNSLookupSilentServer` (about 0.2 s), `TestDNSLookupClosedPort`, `TestDNSLookupBadName`, `TestParseResolvConf`, `TestNormalizeDNS`. `TestSystemResolver` PASSes, or SKIPs on a host without an IPv4 nameserver.

Run: `go test ./internal/nettools/... && GOOS=windows go vet ./internal/nettools/... && go vet ./internal/nettools/... && gofmt -l ./internal/nettools`
Expected: `ok` and no other output.

- [ ] **Step 7: Commit**

From the repository root:

```bash
git add backend/internal/nettools/dns.go backend/internal/nettools/dns_system_unix.go backend/internal/nettools/dns_system_windows.go backend/internal/nettools/dns_test.go backend/internal/nettools/dns_system_unix_test.go
git commit -m "feat(tools): DNS lookup over UDP with TCP retry, and the system resolver address

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 5: TCP scan and the Runner

This task adds the TCP port check/quick scan and `Runner.Run`, the one entry point the server and the agent call.

**`tcpScan`** first emits `start` with the number of ports (ruling 6). It then dials `tcp4` `TargetIP:port` from a pool of `TCPConcurrency` (50) workers, each dial with the per-port timeout. The states are:

- Connected is `open`, with the connect time as RTT.
- Refused is `closed`. On Unix that is `ECONNREFUSED`; on Windows `WSAECONNREFUSED` (10061) or `ECONNREFUSED`.
- Anything else (timeout, unreachable) is `filtered`.

Each port's `port` event, with its service name, is emitted from the scan's goroutine as soon as the port is decided. When the run's context ends, ports cut short are neither reported nor counted. The summary keeps `Total` (the ports asked for) and returns `ctx.Err()`. `OpenPorts` is ascending and `[]` when empty.

**`Runner.Run`** first checks the spec:

- An unknown tool is an error.
- For ping, traceroute and tcp, `TargetIP` must be a dotted IPv4 address (IPv4-mapped IPv6 is refused). This is checked before any prober is opened or any dial is made.

It then dispatches:

- **ping and traceroute** open a prober per run (`r.NewProber`, default `NewProber`) and close it when the run ends. `NewProber`'s error, such as `ErrICMPUnavailable`, is returned as is. Traceroute gets `r.LookupPTR` (default: the system's first reverse name, without its trailing dot) and the 1 s round gap.
- **tcp** uses `r.Dial` (default `(&net.Dialer{}).DialContext`).
- **dns** asks the system resolver (`r.SystemDNS`, default `SystemResolver`) when `Params.Server` is empty. Otherwise it asks `TargetIP` (the checked address of the named server) on the port given in `Params.Server`, or 53.

Summaries follow Contract change 2: typed on success and on a context end (except dns), nil for any other error.

**Files:**
- Create: `backend/internal/nettools/tcp.go`
- Create: `backend/internal/nettools/tcp_refused_unix.go`
- Create: `backend/internal/nettools/tcp_refused_windows.go`
- Create: `backend/internal/nettools/runner.go`
- Test: `backend/internal/nettools/tcp_test.go`
- Test: `backend/internal/nettools/runner_test.go`

**Interfaces:**
- Consumes: Task 1 (`Spec`, `Params`, `Normalize`, `ParsePorts`, `ServiceName`, `TCPConcurrency`, the tcp event and summary types); Task 2 (`Prober`, `NewProber`, `ErrICMPUnavailable`); Task 3 (`ping(ctx, p, s, emit) (PingSummary, error)`, `traceroute(ctx, p, s, emit, lookup lookupFunc, gap time.Duration) (TraceSummary, error)`, `traceRoundGap`, `durationMS`, `ptr`; test helpers `fakeProber`, `fakeReply`, `route`, `recorder`, `eqp`); Task 4 (`dnsLookup(ctx, s, server string, emit) (DNSSummary, error)`, `SystemResolver`; test helpers `newDNSTestServer`, `dnsReply`, `rr`).
- Produces (shared contract):
  - `type Runner struct { NewProber func() (Prober, error); Dial func(ctx context.Context, network, addr string) (net.Conn, error); SystemDNS func() (string, error); LookupPTR func(ctx context.Context, addr string) (string, error) }`. The zero value runs everything with the system defaults.
  - `func (r *Runner) Run(ctx context.Context, s Spec, emit Emitter) (any, error)`. The summary is a `PingSummary`, `TraceSummary`, `DNSSummary` or `TCPSummary` value (not a pointer). `emit` is called from one goroutine at a time.
  - Unexported: `type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)`, `func tcpScan(ctx context.Context, dial dialFunc, s Spec, emit Emitter) (TCPSummary, error)`, `func isRefused(err error) bool`, `func isIPv4(s string) bool`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/nettools/tcp_test.go`. `tcpScan` runs the timeout it is given, so the filtered-port case uses 50 ms. The concurrency test holds the first 50 dials until all 50 have started, so a pool of the right size peaks at exactly 50 and a larger one fails:

```go
package nettools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func tcpSpec(ports string, timeoutMS int) Spec {
	return Spec{Tool: ToolTCP, Target: "192.0.2.10", TargetIP: "192.0.2.10", Params: Params{Ports: ports, TimeoutMS: timeoutMS}}
}

// openConn is a connection that is already established.
func openConn() net.Conn {
	a, b := net.Pipe()
	_ = b.Close()
	return a
}

func refused() error {
	return &net.OpError{Op: "dial", Net: "tcp4", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
}

func portResults(r *recorder) map[int]PortResult {
	out := map[int]PortResult{}
	for _, d := range r.ofType(EventPort) {
		p := d.(PortResult)
		out[p.Port] = p
	}
	return out
}

func TestTCPScanStates(t *testing.T) {
	var mu sync.Mutex
	var dialed []string
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, network+" "+addr)
		mu.Unlock()
		switch addr {
		case "192.0.2.10:22":
			return openConn(), nil
		case "192.0.2.10:23":
			return nil, refused()
		case "192.0.2.10:24": // no answer: the per-port timeout ends it
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, &net.OpError{Op: "dial", Net: "tcp4", Err: os.NewSyscallError("connect", syscall.EHOSTUNREACH)}
	}
	var r recorder
	sum, err := tcpScan(context.Background(), dial, tcpSpec("22-25", 50), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.events) == 0 || r.events[0].Type != EventStart || r.events[0].Data != (ScanStart{Total: 4}) {
		t.Fatalf("first event %+v, want start with 4 ports", r.events)
	}
	got := portResults(&r)
	if len(got) != 4 || len(r.events) != 5 {
		t.Fatalf("results %+v", got)
	}
	if p := got[22]; p.State != PortOpen || p.Service != "ssh" || p.RTTMS == nil {
		t.Errorf("22: %+v", p)
	}
	if p := got[23]; p.State != PortClosed || p.Service != "telnet" || p.RTTMS != nil {
		t.Errorf("23: %+v", p)
	}
	if p := got[24]; p.State != PortFiltered || p.Service != "" || p.RTTMS != nil {
		t.Errorf("24: %+v", p)
	}
	if p := got[25]; p.State != PortFiltered || p.Service != "smtp" {
		t.Errorf("25: %+v", p)
	}
	want := TCPSummary{Total: 4, Open: 1, Closed: 1, Filtered: 2, OpenPorts: []int{22}}
	if !reflect.DeepEqual(sum, want) {
		t.Errorf("summary %+v, want %+v", sum, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dialed) != 4 || dialed[0][:5] != "tcp4 " {
		t.Errorf("dialed %v", dialed)
	}
}

func TestTCPScanLoopback(t *testing.T) {
	open, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	gone, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := gone.Addr().(*net.TCPAddr).Port
	_ = gone.Close()
	openPort := open.Addr().(*net.TCPAddr).Port

	s := Spec{Tool: ToolTCP, Target: "127.0.0.1", TargetIP: "127.0.0.1",
		Params: Params{Ports: fmt.Sprintf("%d,%d", openPort, closedPort), TimeoutMS: 1000}}
	var r recorder
	sum, err := tcpScan(context.Background(), (&net.Dialer{}).DialContext, s, r.emit)
	if err != nil {
		t.Fatal(err)
	}
	got := portResults(&r)
	if got[openPort].State != PortOpen || got[closedPort].State != PortClosed {
		t.Errorf("results %+v", got)
	}
	if !reflect.DeepEqual(sum.OpenPorts, []int{openPort}) || sum.Closed != 1 {
		t.Errorf("summary %+v", sum)
	}
}

// At most TCPConcurrency connects are in flight: the first 50 dials wait
// until all 50 have started, so a pool of the right size reaches exactly 50.
func TestTCPScanConcurrency(t *testing.T) {
	var inFlight, peak atomic.Int32
	full := make(chan struct{})
	var once sync.Once
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		if n == TCPConcurrency {
			once.Do(func() { close(full) })
		}
		select {
		case <-full:
		case <-time.After(time.Second):
		}
		return nil, refused()
	}
	var r recorder
	sum, err := tcpScan(context.Background(), dial, tcpSpec("1-120", 5000), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() != TCPConcurrency {
		t.Errorf("peak %d connects in flight, want %d", peak.Load(), TCPConcurrency)
	}
	if sum.Closed != 120 || len(r.ofType(EventPort)) != 120 {
		t.Errorf("summary %+v", sum)
	}
}

func TestTCPScanCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dial := func(dctx context.Context, _, addr string) (net.Conn, error) {
		switch addr {
		case "192.0.2.10:1", "192.0.2.10:2", "192.0.2.10:3":
			return nil, refused()
		}
		<-dctx.Done()
		return nil, dctx.Err()
	}
	r := recorder{}
	r.onEmit = func(Event) {
		if len(r.ofType(EventPort)) == 3 {
			cancel()
		}
	}
	start := time.Now()
	sum, err := tcpScan(ctx, dial, tcpSpec("1-10", 5000), r.emit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	want := TCPSummary{Total: 10, Closed: 3, OpenPorts: []int{}}
	if !reflect.DeepEqual(sum, want) || len(r.ofType(EventPort)) != 3 {
		t.Errorf("summary %+v, %d port events; want %+v and the cut-short ports unreported", sum, len(r.ofType(EventPort)), want)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
}

func TestTCPScanBadPorts(t *testing.T) {
	var r recorder
	if _, err := tcpScan(context.Background(), nil, tcpSpec("0", 500), r.emit); err == nil || len(r.events) != 0 {
		t.Errorf("err %v, events %+v", err, r.events)
	}
}
```

Create `backend/internal/nettools/runner_test.go`:

```go
package nettools

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// normalized is Normalize(s) with TargetIP set, failing the test on error.
func normalized(t *testing.T, s Spec, targetIP string) Spec {
	t.Helper()
	n, err := Normalize(s)
	if err != nil {
		t.Fatal(err)
	}
	n.TargetIP = targetIP
	return n
}

func proberOf(p Prober) func() (Prober, error) { return func() (Prober, error) { return p, nil } }

func TestRunnerPing(t *testing.T) {
	f := &fakeProber{script: func(int, int, int) fakeReply {
		return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: 3 * time.Millisecond}
	}}
	var r recorder
	sum, err := (&Runner{NewProber: proberOf(f)}).Run(context.Background(),
		normalized(t, Spec{Tool: ToolPing, Target: "server1", Params: Params{Count: 1}}, "192.0.2.10"), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	ps, ok := sum.(PingSummary)
	if !ok || ps.Sent != 1 || ps.Received != 1 || !eqp(ps.AvgMS, 3) {
		t.Errorf("summary %#v", sum)
	}
	if !f.closed {
		t.Error("the prober was not closed")
	}
}

func TestRunnerTraceroute(t *testing.T) {
	f := &fakeProber{canTrace: true, script: route("192.0.2.10", "10.0.0.1")}
	lookup := func(_ context.Context, addr string) (string, error) {
		if addr == "10.0.0.1" {
			return "gw.lab", nil
		}
		return "", nil
	}
	var r recorder
	sum, err := (&Runner{NewProber: proberOf(f), LookupPTR: lookup}).Run(context.Background(),
		normalized(t, Spec{Tool: ToolTraceroute, Target: "server1", Params: Params{MaxHops: 5, Rounds: 1}}, "192.0.2.10"), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	ts, ok := sum.(TraceSummary)
	if !ok || !ts.Reached || ts.HopCount != 2 || ts.Hops[0].Name != "gw.lab" {
		t.Errorf("summary %#v", sum)
	}
	if !f.closed {
		t.Error("the prober was not closed")
	}
}

func aRecordServer(t *testing.T) *dnsTestServer {
	return newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeSuccess,
			[]dnsmessage.Resource{rr(q.Questions[0].Name.String(), 60, &dnsmessage.AResource{A: [4]byte{192, 0, 2, 80}})}, nil, nil)}
	})
}

func TestRunnerDNSSystemResolver(t *testing.T) {
	srv := aRecordServer(t)
	run := &Runner{SystemDNS: func() (string, error) { return srv.addr, nil }}
	var r recorder
	sum, err := run.Run(context.Background(), normalized(t, Spec{Tool: ToolDNS, Target: "www.example.org"}, ""), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if sum != (DNSSummary{Server: srv.addr, RCode: "NOERROR", AnswerCount: 1, RTTMS: sum.(DNSSummary).RTTMS}) {
		t.Errorf("summary %#v", sum)
	}
	if len(r.ofType(EventAnswer)) != 1 {
		t.Errorf("events %+v", r.events)
	}
}

func TestRunnerDNSNamedServer(t *testing.T) {
	srv := aRecordServer(t)
	_, port, _ := net.SplitHostPort(srv.addr)
	run := &Runner{SystemDNS: func() (string, error) {
		t.Error("the system resolver was asked")
		return "", errors.New("unused")
	}}
	var r recorder
	sum, err := run.Run(context.Background(),
		normalized(t, Spec{Tool: ToolDNS, Target: "www.example.org", Params: Params{Server: "127.0.0.1:" + port}}, "127.0.0.1"), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if ds := sum.(DNSSummary); ds.Server != srv.addr || ds.AnswerCount != 1 {
		t.Errorf("summary %#v", sum)
	}
	// The named server's port defaults to 53; the address is TargetIP.
	if got, err := (&Runner{}).dnsServer(Spec{TargetIP: "192.0.2.53", Params: Params{Server: "192.0.2.53"}}); got != "192.0.2.53:53" || err != nil {
		t.Errorf("default port: %q, %v", got, err)
	}
	if _, err := (&Runner{}).dnsServer(Spec{Params: Params{Server: "192.0.2.53"}}); err == nil {
		t.Error("a named server without target_ip was accepted")
	}
}

func TestRunnerTCP(t *testing.T) {
	dial := func(_ context.Context, _, addr string) (net.Conn, error) {
		if addr == "192.0.2.10:443" {
			return openConn(), nil
		}
		return nil, refused()
	}
	var r recorder
	sum, err := (&Runner{Dial: dial}).Run(context.Background(),
		normalized(t, Spec{Tool: ToolTCP, Target: "server1", Params: Params{Ports: "443,444"}}, "192.0.2.10"), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if want := (TCPSummary{Total: 2, Open: 1, Closed: 1, OpenPorts: []int{443}}); !reflect.DeepEqual(sum, want) {
		t.Errorf("summary %#v, want %#v", sum, want)
	}
}

func TestRunnerRefusesNonIPv4Targets(t *testing.T) {
	opened, dialed := false, false
	run := &Runner{
		NewProber: func() (Prober, error) { opened = true; return &fakeProber{}, nil },
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, refused()
		},
	}
	for _, tool := range []Tool{ToolPing, ToolTraceroute, ToolTCP} {
		for _, ip := range []string{"", "server1", "2001:db8::1", "::ffff:192.0.2.1"} {
			var r recorder
			sum, err := run.Run(context.Background(), Spec{Tool: tool, Target: "server1", TargetIP: ip, Params: Params{Count: 1, Ports: "22"}}, r.emit)
			if err == nil || sum != nil || len(r.events) != 0 {
				t.Errorf("%s to %q: %v, %v, %d events", tool, ip, sum, err, len(r.events))
			}
		}
	}
	if opened || dialed {
		t.Error("a refused run still opened a prober or dialed")
	}
	if _, err := run.Run(context.Background(), Spec{Tool: "nmap", TargetIP: "192.0.2.10"}, func(Event) {}); err == nil {
		t.Error("an unknown tool ran")
	}
}

func TestRunnerICMPUnavailable(t *testing.T) {
	run := &Runner{NewProber: func() (Prober, error) { return nil, ErrICMPUnavailable }}
	var r recorder
	sum, err := run.Run(context.Background(), normalized(t, Spec{Tool: ToolPing, Target: "h"}, "192.0.2.10"), r.emit)
	if !errors.Is(err, ErrICMPUnavailable) || sum != nil || len(r.events) != 0 {
		t.Errorf("%v, %v, %d events", sum, err, len(r.events))
	}
	if err.Error() != "ICMP isn't available here (needs root or NET_RAW)" {
		t.Errorf("message %q", err.Error())
	}
	f := &fakeProber{canTrace: false}
	sum, err = (&Runner{NewProber: proberOf(f)}).Run(context.Background(),
		normalized(t, Spec{Tool: ToolTraceroute, Target: "h"}, "192.0.2.10"), r.emit)
	if !errors.Is(err, ErrICMPUnavailable) || sum != nil || !f.closed {
		t.Errorf("traceroute without raw ICMP: %v, %v, closed %v", sum, err, f.closed)
	}
}

func TestRunnerCancelledKeepsThePartialSummary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var r recorder
	sum, err := (&Runner{Dial: func(context.Context, string, string) (net.Conn, error) { return nil, refused() }}).Run(ctx,
		normalized(t, Spec{Tool: ToolTCP, Target: "h", Params: Params{Ports: "22,80"}}, "192.0.2.10"), r.emit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := sum.(TCPSummary); !ok {
		t.Errorf("summary %#v, want the TCPSummary so far", sum)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run (from `backend/`): `go test ./internal/nettools/ -run 'TCP|Runner' -v`
Expected: the build fails with `undefined: tcpScan` and `undefined: Runner`.

- [ ] **Step 3: Write the scan**

Create `backend/internal/nettools/tcp.go`:

```go
package nettools

import (
	"context"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// dialFunc opens a connection, like (*net.Dialer).DialContext.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// tcpScan connects to each port of s.Params.Ports on s.TargetIP, at most
// TCPConcurrency at a time, each with the per-port timeout: connected is
// open, refused is closed, anything else (timeout, unreachable) is
// filtered. It emits a ScanStart, then a PortResult per port as soon as its
// state is known. s must be normalized and TargetIP an IPv4 address.
func tcpScan(ctx context.Context, dial dialFunc, s Spec, emit Emitter) (TCPSummary, error) {
	ports, err := ParsePorts(s.Params.Ports)
	if err != nil {
		return TCPSummary{}, err
	}
	timeout := time.Duration(s.Params.TimeoutMS) * time.Millisecond
	emit(Event{Type: EventStart, Data: ScanStart{Total: len(ports)}})

	jobs := make(chan int)
	results := make(chan PortResult)
	var workers sync.WaitGroup
	for i := 0; i < min(TCPConcurrency, len(ports)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for port := range jobs {
				if r, ok := probePort(ctx, dial, s.TargetIP, port, timeout); ok {
					results <- r
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, p := range ports {
			select {
			case jobs <- p:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	sum := TCPSummary{Total: len(ports), OpenPorts: []int{}}
	for r := range results {
		switch r.State {
		case PortOpen:
			sum.Open++
			sum.OpenPorts = append(sum.OpenPorts, r.Port)
		case PortClosed:
			sum.Closed++
		default:
			sum.Filtered++
		}
		emit(Event{Type: EventPort, Data: r})
	}
	sort.Ints(sum.OpenPorts)
	return sum, ctx.Err()
}

// probePort tries one port. ok is false when the run's context ended first,
// so the outcome says nothing about the port.
func probePort(ctx context.Context, dial dialFunc, ip string, port int, timeout time.Duration) (PortResult, bool) {
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	conn, err := dial(pctx, "tcp4", net.JoinHostPort(ip, strconv.Itoa(port)))
	rtt := time.Since(start)
	if err == nil {
		_ = conn.Close()
	}
	if ctx.Err() != nil {
		return PortResult{}, false
	}
	r := PortResult{Port: port, Service: ServiceName(port)}
	switch {
	case err == nil:
		r.State, r.RTTMS = PortOpen, ptr(durationMS(rtt))
	case isRefused(err):
		r.State = PortClosed
	default:
		r.State = PortFiltered
	}
	return r, true
}
```

Create `backend/internal/nettools/tcp_refused_unix.go`:

```go
//go:build !windows

package nettools

import (
	"errors"
	"syscall"
)

// isRefused reports whether a dial error means the host answered with a
// reset: the port is closed.
func isRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
```

Create `backend/internal/nettools/tcp_refused_windows.go`:

```go
package nettools

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// isRefused reports whether a dial error means the host answered with a
// reset: the port is closed. Windows reports WSAECONNREFUSED (10061).
func isRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, syscall.ECONNREFUSED)
}
```

- [ ] **Step 4: Write the Runner**

Create `backend/internal/nettools/runner.go`:

```go
package nettools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Runner runs tools. Nil fields use the system's ICMP, dialer and resolvers.
type Runner struct {
	NewProber func() (Prober, error)
	Dial      func(ctx context.Context, network, addr string) (net.Conn, error)
	SystemDNS func() (string, error)
	LookupPTR func(ctx context.Context, addr string) (string, error)
}

// Run executes a normalized spec, calling emit for each event as it
// happens, and returns the tool's summary (PingSummary, TraceSummary,
// DNSSummary or TCPSummary). When ctx ends first it returns the summary of
// what completed and ctx.Err(). Any other failure returns a nil summary and
// the error (ErrICMPUnavailable as is).
func (r *Runner) Run(ctx context.Context, s Spec, emit Emitter) (any, error) {
	switch s.Tool {
	case ToolPing, ToolTraceroute, ToolTCP:
		if !isIPv4(s.TargetIP) {
			return nil, fmt.Errorf("target_ip %q is not an IPv4 address", s.TargetIP)
		}
	case ToolDNS:
	default:
		return nil, fmt.Errorf("unknown tool %q", s.Tool)
	}

	switch s.Tool {
	case ToolPing, ToolTraceroute:
		open := r.NewProber
		if open == nil {
			open = NewProber
		}
		p, err := open()
		if err != nil {
			return nil, err
		}
		defer p.Close()
		if s.Tool == ToolPing {
			return partial(ping(ctx, p, s, emit))
		}
		lookup := r.LookupPTR
		if lookup == nil {
			lookup = lookupPTR
		}
		return partial(traceroute(ctx, p, s, emit, lookup, traceRoundGap))
	case ToolTCP:
		dial := r.Dial
		if dial == nil {
			dial = (&net.Dialer{}).DialContext
		}
		return partial(tcpScan(ctx, dial, s, emit))
	}

	server, err := r.dnsServer(s)
	if err != nil {
		return nil, err
	}
	sum, err := dnsLookup(ctx, s, server, emit)
	if err != nil {
		return nil, err
	}
	return sum, nil
}

// partial passes a summary on with a nil error or ctx's error, and drops it
// for any other error.
func partial[T any](sum T, err error) (any, error) {
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	return sum, err
}

// dnsServer is the "ip:port" a lookup asks: the system resolver, or the
// named server, which the caller has checked and put in TargetIP.
func (r *Runner) dnsServer(s Spec) (string, error) {
	if s.Params.Server == "" {
		system := r.SystemDNS
		if system == nil {
			system = SystemResolver
		}
		return system()
	}
	if !isIPv4(s.TargetIP) {
		return "", fmt.Errorf("target_ip %q is not an IPv4 address", s.TargetIP)
	}
	port := "53"
	if _, p, err := net.SplitHostPort(s.Params.Server); err == nil {
		port = p
	}
	return net.JoinHostPort(s.TargetIP, port), nil
}

// isIPv4 accepts dotted-quad IPv4 only (not IPv4-mapped IPv6).
func isIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil && !strings.Contains(s, ":")
}

// lookupPTR is the system's reverse lookup: the first name, without its
// trailing dot.
func lookupPTR(ctx context.Context, addr string) (string, error) {
	names, err := net.DefaultResolver.LookupAddr(ctx, addr)
	if err != nil || len(names) == 0 {
		return "", err
	}
	return strings.TrimSuffix(names[0], "."), nil
}
```

- [ ] **Step 5: Run the tests**

Run (from `backend/`): `go test ./internal/nettools/ -run 'TCP|Runner' -v`
Expected: PASS for `TestTCPScanStates`, `TestTCPScanLoopback`, `TestTCPScanConcurrency`, `TestTCPScanCancelled`, `TestTCPScanBadPorts`, `TestRunnerPing`, `TestRunnerTraceroute`, `TestRunnerDNSSystemResolver`, `TestRunnerDNSNamedServer`, `TestRunnerTCP`, `TestRunnerRefusesNonIPv4Targets`, `TestRunnerICMPUnavailable`, `TestRunnerCancelledKeepsThePartialSummary` (plus `TestNormalizeTCPPorts` and `TestDNSLookupTruncatedRetriesOverTCP`, whose names match too).

- [ ] **Step 6: Check the whole package, its imports and the Windows build**

Run (from `backend/`): `go test ./internal/nettools/... && go vet ./internal/nettools/... && GOOS=windows go vet ./internal/nettools/... && gofmt -l ./internal/nettools`
Expected: `ok` and no other output.

Run: `go list -deps ./internal/nettools/ | grep -E '^(github\.com|golang\.org|gorm\.io)'`
Expected: only `golang.org/x/net/...`, `golang.org/x/sys/...` and `github.com/Stevy2191/Sentinel/backend/internal/nettools` itself. No gin, gorm or other Sentinel package. `git diff --stat backend/go.mod backend/go.sum` shows no change.

- [ ] **Step 7: Commit**

From the repository root:

```bash
git add backend/internal/nettools/tcp.go backend/internal/nettools/tcp_refused_unix.go backend/internal/nettools/tcp_refused_windows.go backend/internal/nettools/runner.go backend/internal/nettools/tcp_test.go backend/internal/nettools/runner_test.go
git commit -m "feat(tools): TCP port scan and the nettools Runner

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 6: Schema, models and the small service/handler additions

The migration, the `ToolRun` models, the new user and agent columns, the audit and setting constants, `SetToolsEnabled`, `SetNetTools`, the heartbeat's `tools_local`, and `net_tools` on `/me` and on the admin's user list.

**Files:**
- Create: `backend/migrations/059_network_tools.sql`
- Create: `backend/internal/models/tool_run.go`
- Modify: `backend/internal/models/user.go` (`User`: `NetTools`)
- Modify: `backend/internal/models/agent.go` (`Agent`: `ToolsEnabled`, `ToolsLocal`)
- Modify: `backend/internal/models/audit.go` (six actions, four resource types)
- Modify: `backend/internal/models/setting.go` (three keys)
- Modify: `backend/internal/services/agent_service.go` (`SetToolsEnabled`, `AgentSystemInfo.ToolsLocal`, `Heartbeat`)
- Modify: `backend/internal/services/auth_service.go` (`SetNetTools`)
- Modify: `backend/internal/api/agent_handler.go` (`heartbeatRequest.ToolsLocal`, `HeartbeatHandler`)
- Modify: `backend/internal/api/auth_handler.go` (`GetCurrentUserHandler`)
- Modify: `backend/internal/api/monitor_sharing_handler.go` (`ListUsersHandler`)
- Test: `backend/internal/models/tool_run_test.go` (new, unit)
- Test: `backend/internal/services/net_tools_db_test.go` (new, DB)
- Test: `backend/internal/api/net_tools_fields_db_test.go` (new, DB)

**Interfaces:**
- Consumes: existing `models.RawJSON` (`models/dashboard.go`): `Value()` writes `{}` for empty, and `MarshalJSON` emits the stored bytes. A NULL jsonb column read into a `*RawJSON` field stays nil; `TestDBToolRunModelRoundTrip` pins this. Also `testdb.Open`, `testdb.NewUser`, `testdb.Exec`, `testdb.Must`, `AgentService.Register/Get/Delete/Heartbeat` and `RegisterAgentIngestRoutes`.
- Produces (shared contract, "`internal/models`" and "`internal/services` additions"):
  - `models.ToolRun`, `models.ToolRunEvent` and their `TableName`s; `models.ToolRunActive(status string) bool`; the `ToolRun*` status constants and `VantageSentinel`/`VantageAgent`.
  - `models.User.NetTools` (`json:"net_tools"`); `models.Agent.ToolsEnabled` (`json:"tools_enabled"`) and `models.Agent.ToolsLocal *bool` (`json:"tools_local"`, nil = never reported).
  - `models.ActionToolRunStarted`, `ActionToolRunFinished`, `ActionToolRunRefused`, `ActionNetToolsSettingsUpdated`, `ActionUserNetToolsChanged`, `ActionAgentToolsChanged`; `models.ResourceToolRun`, `ResourceSettings`, `ResourceUser`, `ResourceAgent`.
  - `models.SettingNetToolsAllowlist`, `SettingNetToolsServerEnabled`, `SettingNetToolsRetentionDays`.
  - `services.AgentSystemInfo.ToolsLocal *bool`. Every heartbeat writes it, and nil is stored as NULL.
  - `func (s *AgentService) SetToolsEnabled(ctx context.Context, agentID string, enabled bool) (*models.Agent, error)`. It returns `ErrAgentNotFound` for an unknown readable id.
  - `func (s *AuthService) SetNetTools(ctx context.Context, userID uuid.UUID, enabled bool) (*models.User, error)`. It returns an error wrapping `gorm.ErrRecordNotFound` for an unknown user.
  - The heartbeat body accepts `"tools_local": true|false`; `GET /api/v1/auth/me` returns `net_tools`; `GET /api/v1/users` returns `net_tools` for admin callers.

- [ ] **Step 1: Confirm the migration number**

Run (repo root): `ls backend/migrations | tail -1`
Expected: `058_metric_reports.sql`, so this task's migration is `059_network_tools.sql`. If a later file exists, use the next free number and rename everywhere this task says 059.

- [ ] **Step 2: Write the failing model test**

Create `backend/internal/models/tool_run_test.go`:

```go
package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestToolRunActive(t *testing.T) {
	for status, want := range map[string]bool{
		ToolRunQueued: true, ToolRunRunning: true,
		ToolRunDone: false, ToolRunFailed: false, ToolRunRefused: false,
		ToolRunCancelled: false, ToolRunTimedOut: false, ToolRunInterrupted: false,
		"": false,
	} {
		if got := ToolRunActive(status); got != want {
			t.Errorf("ToolRunActive(%q) = %v, want %v", status, got, want)
		}
	}
}

// The API calls the readable agent id agent_id; the UUID foreign key never
// leaves the server, and a run with no summary says null rather than {}.
func TestToolRunJSON(t *testing.T) {
	agent := uuid.New()
	ref := "agent_0123456789"
	run := ToolRun{
		ID: uuid.New(), Tool: "ping", Status: ToolRunQueued, Username: "alice",
		VantageKind: VantageAgent, AgentUUID: &agent, AgentRef: &ref, VantageName: "file-server",
		Target: "10.0.0.5", Params: RawJSON(`{"count":5}`),
		CreatedAt: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC), Deadline: time.Date(2026, 10, 5, 12, 2, 30, 0, time.UTC),
	}
	b, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"agent_id":"agent_0123456789"`, `"summary":null`, `"params":{"count":5}`, `"target_ip":null`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON %s lacks %s", s, want)
		}
	}
	if strings.Contains(s, agent.String()) {
		t.Errorf("JSON %s leaks the agent's UUID", s)
	}
}
```

- [ ] **Step 3: Run it and watch it fail**

Run (from `backend/`): `go test ./internal/models/ -run 'TestToolRun' -v`
Expected: the build fails with `undefined: ToolRunActive`, `undefined: ToolRun` and `undefined: VantageAgent`.

- [ ] **Step 4: Write the models**

Create `backend/internal/models/tool_run.go`:

```go
package models

import (
	"time"

	"github.com/google/uuid"
)

// Tool run statuses. A run is queued (an agent run waiting to be claimed) or
// running until it reaches one of the final statuses.
const (
	ToolRunQueued      = "queued"
	ToolRunRunning     = "running"
	ToolRunDone        = "done"
	ToolRunFailed      = "failed"
	ToolRunRefused     = "refused"
	ToolRunCancelled   = "cancelled"
	ToolRunTimedOut    = "timed_out"
	ToolRunInterrupted = "interrupted"

	// Where a run executes.
	VantageSentinel = "sentinel"
	VantageAgent    = "agent"
)

// ToolRunActive reports whether a run in this status is still in progress
// (queued or running), which is what the active-run caps count.
func ToolRunActive(status string) bool {
	return status == ToolRunQueued || status == ToolRunRunning
}

// ToolRun is one run of a network tool (spec 2026-10-05-tools-s1).
//
// AgentUUID is the foreign key and is never serialised; AgentRef, the
// agent's readable id snapshotted at creation, is what the API calls
// agent_id. VantageName and Username are snapshots too, so history still
// reads correctly after the agent or the user is deleted.
type ToolRun struct {
	ID          uuid.UUID  `json:"id" gorm:"column:id;type:uuid;primaryKey"`
	Tool        string     `json:"tool" gorm:"column:tool"`
	Status      string     `json:"status" gorm:"column:status"`
	UserID      *uuid.UUID `json:"user_id" gorm:"column:user_id"`
	Username    string     `json:"username" gorm:"column:username"`
	VantageKind string     `json:"vantage_kind" gorm:"column:vantage_kind"`
	AgentUUID   *uuid.UUID `json:"-" gorm:"column:agent_id"`
	AgentRef    *string    `json:"agent_id" gorm:"column:agent_ref"`
	VantageName string     `json:"vantage_name" gorm:"column:vantage_name"`
	Target      string     `json:"target" gorm:"column:target"`
	TargetIP    *string    `json:"target_ip" gorm:"column:target_ip"`
	Params      RawJSON    `json:"params" gorm:"column:params;type:jsonb"`
	Summary     *RawJSON   `json:"summary" gorm:"column:summary;type:jsonb"`
	Error       *string    `json:"error" gorm:"column:error"`
	EventCount  int        `json:"event_count" gorm:"column:event_count"`
	CreatedAt   time.Time  `json:"created_at" gorm:"column:created_at"`
	StartedAt   *time.Time `json:"started_at" gorm:"column:started_at"`
	FinishedAt  *time.Time `json:"finished_at" gorm:"column:finished_at"`
	Deadline    time.Time  `json:"deadline" gorm:"column:deadline"`
}

// TableName pins the table name.
func (ToolRun) TableName() string { return "tool_runs" }

// ToolRunEvent is one event a run reported. Seq numbers a run's events from 1.
type ToolRunEvent struct {
	RunID uuid.UUID `json:"-" gorm:"column:run_id;type:uuid;primaryKey"`
	Seq   int       `json:"seq" gorm:"column:seq;primaryKey"`
	At    time.Time `json:"at" gorm:"column:at"`
	Type  string    `json:"type" gorm:"column:type"`
	Data  RawJSON   `json:"data" gorm:"column:data;type:jsonb"`
}

// TableName pins the table name.
func (ToolRunEvent) TableName() string { return "tool_run_events" }
```

In `backend/internal/models/user.go`, inside `type User struct`, after the line

```go
	UpdatedAt         time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
```

add (the blank line keeps gofmt from realigning the fields above):

```go

	// NetTools grants the network tools to a non-admin; admins may always use
	// them, whatever it says. No gorm default, for the reason IsAdmin has none.
	NetTools bool `json:"net_tools" gorm:"column:net_tools"`
```

In `backend/internal/models/agent.go`, inside `type Agent struct`, between the `DockerAvailable` line and the blank line before `CreatedAt`, so the struct reads:

```go
	GoVersion       *string `json:"go_version" gorm:"column:go_version"`
	DockerAvailable *bool   `json:"docker_available" gorm:"column:docker_available"`

	// ---- Network tools ---------------------------------------------------
	// ToolsEnabled is the admin's switch in Sentinel. ToolsLocal is the
	// agent's own ENABLE_TOOLS flag as its heartbeat last reported it; nil
	// means the agent has never reported it, so it is too old to run tools.
	// Both must be on before a run can use the agent.
	ToolsEnabled bool  `json:"tools_enabled" gorm:"column:tools_enabled"`
	ToolsLocal   *bool `json:"tools_local" gorm:"column:tools_local"`

	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
```

In `backend/internal/models/audit.go`, in the actions `const` block, after `ActionDashboardLinkRevoked = "dashboard_link_revoked"` add:

```go

	ActionToolRunStarted          = "tool_run_started"
	ActionToolRunFinished         = "tool_run_finished"
	ActionToolRunRefused          = "tool_run_refused"
	ActionNetToolsSettingsUpdated = "net_tools_settings_updated"
	ActionUserNetToolsChanged     = "user_net_tools_changed"
	ActionAgentToolsChanged       = "agent_tools_changed"
```

In the resource types block of the same file, after `ResourceDashboard = "dashboard"`, add the following. No settings, user or agent resource constant exists yet.

```go

	ResourceToolRun  = "tool_run"
	ResourceSettings = "settings"
	ResourceUser     = "user"
	ResourceAgent    = "agent"
```

In `backend/internal/models/setting.go`, in the setting keys `const` block, after `SettingUPSHighLoadPct   = "ups_high_load_pct"` add:

```go

	// Network tools (spec 2026-10-05-tools-s1). The allowlist is a JSON array
	// of strings (IPv4 addresses, CIDRs, host names, *.domain wildcards);
	// empty means no target is allowed. Read and written by
	// internal/toolruns, which holds the defaults (empty, true, 30 days).
	SettingNetToolsAllowlist     = "net_tools_allowlist"
	SettingNetToolsServerEnabled = "net_tools_server_enabled"
	SettingNetToolsRetentionDays = "net_tools_retention_days"
```

- [ ] **Step 5: Run the model tests**

Run: `go test ./internal/models/ -run 'TestToolRun' -v`
Expected: PASS for `TestToolRunActive` and `TestToolRunJSON`.

- [ ] **Step 6: Write the failing DB tests**

Create `backend/internal/services/net_tools_db_test.go`:

```go
package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// netToolsAgent registers an agent and returns it.
func netToolsAgent(t *testing.T, svc *AgentService, name string) *models.Agent {
	t.Helper()
	a := &models.Agent{Name: name, OSType: "linux", CheckInterval: 60, RetryAttempts: 3}
	testdb.Must(t, svc.Register(context.Background(), a))
	return a
}

// insertToolRun writes a minimal valid run with the given status, tool and
// vantage kind, returning the error so CHECK tests can expect one.
func insertToolRun(db *gorm.DB, id uuid.UUID, tool, status, vantage string, user, agent *uuid.UUID) error {
	now := time.Now().UTC()
	return db.Exec(`INSERT INTO tool_runs (id, tool, status, user_id, username, vantage_kind, agent_id,
			vantage_name, target, params, created_at, deadline)
		VALUES (?, ?, ?, ?, 'alice', ?, ?, 'Sentinel', '10.0.0.5', '{}', ?, ?)`,
		id, tool, status, user, vantage, agent, now, now.Add(time.Minute)).Error
}

func TestDBToolRunsSchema(t *testing.T) {
	db := testdb.Open(t)
	agents := NewAgentService(db)
	agent := netToolsAgent(t, agents, "file-server")
	user := testdb.NewUser(t, db, false)

	run := uuid.New()
	testdb.Must(t, insertToolRun(db, run, "ping", models.ToolRunRunning, models.VantageAgent, &user, &agent.ID))
	testdb.Exec(t, db, `INSERT INTO tool_run_events (run_id, seq, at, type, data) VALUES (?, 1, now(), 'reply', '{"seq":1}')`, run)

	for _, bad := range []struct{ name, tool, status, vantage string }{
		{"unknown tool", "nmap", models.ToolRunQueued, models.VantageSentinel},
		{"unknown status", "ping", "paused", models.VantageSentinel},
		{"unknown vantage", "ping", models.ToolRunQueued, "moon"},
	} {
		if err := insertToolRun(db, uuid.New(), bad.tool, bad.status, bad.vantage, nil, nil); err == nil {
			t.Errorf("%s was accepted", bad.name)
		}
	}
	if err := db.Exec(`INSERT INTO tool_runs (id, tool, status, username, vantage_kind, vantage_name, target, params, created_at, deadline)
		VALUES (?, 'ping', NULL, 'a', 'sentinel', 'Sentinel', 'x', '{}', now(), now())`, uuid.New()).Error; err == nil {
		t.Error("a NULL status was accepted")
	}
	if err := db.Exec(`INSERT INTO tool_run_events (run_id, seq, at, type, data) VALUES (?, 1, now(), 'reply', '{}')`, run).Error; err == nil {
		t.Error("a duplicate (run_id, seq) was accepted")
	}

	// Deleting the agent and the user keeps the run, unlinked.
	testdb.Must(t, agents.Delete(context.Background(), agent.AgentID))
	testdb.Exec(t, db, `DELETE FROM users WHERE id = ?`, user)
	var row struct {
		AgentID *uuid.UUID
		UserID  *uuid.UUID
	}
	testdb.Must(t, db.Raw(`SELECT agent_id, user_id FROM tool_runs WHERE id = ?`, run).Scan(&row).Error)
	if row.AgentID != nil || row.UserID != nil {
		t.Errorf("after deleting the agent and the user: agent_id %v, user_id %v, want both NULL", row.AgentID, row.UserID)
	}

	// Deleting the run takes its events with it.
	testdb.Exec(t, db, `DELETE FROM tool_runs WHERE id = ?`, run)
	var events int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM tool_run_events WHERE run_id = ?`, run).Scan(&events).Error)
	if events != 0 {
		t.Errorf("%d events outlived their run", events)
	}
}

// A ToolRun read back through GORM keeps a NULL summary as nil.
func TestDBToolRunModelRoundTrip(t *testing.T) {
	db := testdb.Open(t)
	id := uuid.New()
	testdb.Must(t, insertToolRun(db, id, "dns", models.ToolRunQueued, models.VantageSentinel, nil, nil))
	var run models.ToolRun
	testdb.Must(t, db.First(&run, "id = ?", id).Error)
	if run.Summary != nil || run.Error != nil || run.TargetIP != nil || run.AgentUUID != nil || string(run.Params) != "{}" {
		t.Errorf("run = %+v, want nil summary, error, target_ip and agent, params {}", run)
	}
	testdb.Exec(t, db, `UPDATE tool_runs SET summary = '{"sent":5}' WHERE id = ?`, id)
	testdb.Must(t, db.First(&run, "id = ?", id).Error)
	if run.Summary == nil || string(*run.Summary) != `{"sent": 5}` {
		t.Errorf("summary = %v, want {\"sent\": 5}", run.Summary)
	}
}

func TestDBHeartbeatToolsLocal(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	agents := NewAgentService(db)
	agent := netToolsAgent(t, agents, "file-server")
	yes, no := true, false

	stored := func() *bool {
		t.Helper()
		var v *bool
		testdb.Must(t, db.Raw(`SELECT tools_local FROM agents WHERE id = ?`, agent.ID).Scan(&v).Error)
		return v
	}
	if v := stored(); v != nil {
		t.Fatalf("a new agent has tools_local %v, want NULL", *v)
	}
	testdb.Must(t, agents.Heartbeat(ctx, agent, AgentSystemInfo{ToolsLocal: &yes}))
	if v := stored(); v == nil || !*v {
		t.Errorf("after tools_local true: %v", v)
	}
	testdb.Must(t, agents.Heartbeat(ctx, agent, AgentSystemInfo{ToolsLocal: &no}))
	if v := stored(); v == nil || *v {
		t.Errorf("after tools_local false: %v", v)
	}
	testdb.Must(t, agents.Heartbeat(ctx, agent, AgentSystemInfo{}))
	if v := stored(); v != nil {
		t.Errorf("after a heartbeat without the flag: %v, want NULL", *v)
	}
}

func TestDBSetToolsEnabled(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	agents := NewAgentService(db)
	agent := netToolsAgent(t, agents, "file-server")
	if agent.ToolsEnabled {
		t.Fatal("a new agent has tools enabled")
	}
	got, err := agents.SetToolsEnabled(ctx, agent.AgentID, true)
	testdb.Must(t, err)
	if !got.ToolsEnabled || got.AgentID != agent.AgentID {
		t.Errorf("SetToolsEnabled(true) = %+v", got)
	}
	got, err = agents.SetToolsEnabled(ctx, agent.AgentID, false)
	testdb.Must(t, err)
	if got.ToolsEnabled {
		t.Error("SetToolsEnabled(false) left tools on")
	}
	if _, err := agents.SetToolsEnabled(ctx, "agent_ffffffffff", true); !errors.Is(err, ErrAgentNotFound) {
		t.Errorf("unknown agent: %v, want ErrAgentNotFound", err)
	}
}

func TestDBSetNetTools(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	auth := NewAuthService(db, "0123456789abcdef0123456789abcdef")
	id := testdb.NewUser(t, db, false)

	u, err := auth.GetUserByID(ctx, id)
	testdb.Must(t, err)
	if u.NetTools {
		t.Fatal("a new user has the network tools grant")
	}
	u, err = auth.SetNetTools(ctx, id, true)
	testdb.Must(t, err)
	if !u.NetTools {
		t.Error("SetNetTools(true) did not grant")
	}
	u, err = auth.SetNetTools(ctx, id, false)
	testdb.Must(t, err)
	if u.NetTools {
		t.Error("SetNetTools(false) did not revoke")
	}
	if _, err := auth.SetNetTools(ctx, uuid.New(), true); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("unknown user: %v, want gorm.ErrRecordNotFound", err)
	}
}
```

Create `backend/internal/api/net_tools_fields_db_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// The heartbeat's tools_local reaches agents.tools_local: true, false, and
// NULL when an older agent leaves it out.
func TestDBHeartbeatHandlerToolsLocal(t *testing.T) {
	db := testdb.Open(t)
	agents := services.NewAgentService(db)
	agent := &models.Agent{Name: "file-server", OSType: "linux", CheckInterval: 60, RetryAttempts: 3}
	testdb.Must(t, agents.Register(context.Background(), agent))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterAgentIngestRoutes(r, agents)
	beat := func(body string) {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/heartbeat", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+agent.ServerToken)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("heartbeat %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	stored := func() *bool {
		t.Helper()
		var v *bool
		testdb.Must(t, db.Raw(`SELECT tools_local FROM agents WHERE id = ?`, agent.ID).Scan(&v).Error)
		return v
	}

	beat(`{"agent_id":"` + agent.AgentID + `","tools_local":true}`)
	if v := stored(); v == nil || !*v {
		t.Errorf("tools_local true stored as %v", v)
	}
	beat(`{"agent_id":"` + agent.AgentID + `","tools_local":false}`)
	if v := stored(); v == nil || *v {
		t.Errorf("tools_local false stored as %v", v)
	}
	beat(`{"agent_id":"` + agent.AgentID + `","agent_version":"1.0.0"}`)
	if v := stored(); v != nil {
		t.Errorf("a heartbeat without tools_local stored %v, want NULL", *v)
	}
}

// GET /auth/me carries net_tools, so the frontend knows whether to show the
// tools.
func TestDBMeIncludesNetTools(t *testing.T) {
	db := testdb.Open(t)
	auth := services.NewAuthService(db, "0123456789abcdef0123456789abcdef")
	id := testdb.NewUser(t, db, false)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", id)
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	r.GET("/me", GetCurrentUserHandler(auth))
	me := func() map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me", nil))
		var body struct {
			Data map[string]any `json:"data"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil {
			t.Fatalf("/me: %d %s", w.Code, w.Body.String())
		}
		return body.Data
	}

	if got := me()["net_tools"]; got != false {
		t.Errorf("net_tools = %v, want false", got)
	}
	_, err := auth.SetNetTools(context.Background(), id, true)
	testdb.Must(t, err)
	if got := me()["net_tools"]; got != true {
		t.Errorf("net_tools after the grant = %v, want true", got)
	}
}

// GET /users tells an admin who holds the network tools grant (for the
// AdminUsers checkbox) and tells a non-admin nothing about it.
func TestDBUsersListNetTools(t *testing.T) {
	db := testdb.Open(t)
	auth := services.NewAuthService(db, "0123456789abcdef0123456789abcdef")
	granted := testdb.NewUser(t, db, false)
	_, err := auth.SetNetTools(context.Background(), granted, true)
	testdb.Must(t, err)

	list := func(admin bool) []map[string]any {
		t.Helper()
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("user_id", granted)
			c.Set("username", "someone")
			c.Set("is_admin", admin)
			c.Next()
		})
		r.GET("/users", ListUsersHandler(auth))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users", nil))
		var body struct {
			Data []map[string]any `json:"data"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Data) != 1 {
			t.Fatalf("/users: %d %s", w.Code, w.Body.String())
		}
		return body.Data
	}
	if got := list(true)[0]["net_tools"]; got != true {
		t.Errorf("admin view: net_tools = %v, want true", got)
	}
	if _, ok := list(false)[0]["net_tools"]; ok {
		t.Error("a non-admin sees net_tools")
	}
}
```

- [ ] **Step 7: Run them and watch them fail**

Run: `go vet ./internal/services/ ./internal/api/`
Expected: compile errors, among them `unknown field ToolsLocal in struct literal of type AgentSystemInfo`, `agents.SetToolsEnabled undefined` and `auth.SetNetTools undefined`.

- [ ] **Step 8: Write the migration**

Create `backend/migrations/059_network_tools.sql`:

```sql
-- 059_network_tools.sql
-- Tools and security S1 (spec 2026-10-05-tools-s1-network-tools-design.md):
-- on-demand ping, traceroute, DNS lookup and TCP port checks, run from the
-- Sentinel server or an agent, kept as run history.

-- The "Network tools" grant. Admins are always permitted, whatever it says.
ALTER TABLE users ADD COLUMN IF NOT EXISTS net_tools BOOLEAN NOT NULL DEFAULT false;

-- The admin's switch for an agent, and the agent's own ENABLE_TOOLS flag as
-- its heartbeat reports it. tools_local is NULL until an agent reports the
-- flag at all: an agent too old to run tools never does.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS tools_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS tools_local BOOLEAN;

-- One run of one tool. Agent runs wait here as 'queued' until the agent
-- claims them. No CHECK ties vantage_kind to agent_id: deleting an agent
-- sets agent_id NULL on its runs, and such a CHECK would refuse that.
-- target_ip is canonical dotted IPv4 (only equality is needed); it is NULL
-- for a DNS lookup through the vantage's own resolver. agent_ref snapshots
-- the agent's readable id for links.
CREATE TABLE IF NOT EXISTS tool_runs (
    id           UUID PRIMARY KEY,
    tool         TEXT NOT NULL
        CHECK (tool IS NOT NULL AND tool IN ('ping', 'traceroute', 'dns', 'tcp')),
    status       TEXT NOT NULL
        CHECK (status IS NOT NULL AND status IN ('queued', 'running', 'done', 'failed', 'refused',
                                                 'cancelled', 'timed_out', 'interrupted')),
    user_id      UUID REFERENCES users (id) ON DELETE SET NULL,
    username     TEXT NOT NULL,
    vantage_kind TEXT NOT NULL
        CHECK (vantage_kind IS NOT NULL AND vantage_kind IN ('sentinel', 'agent')),
    agent_id     UUID REFERENCES agents (id) ON DELETE SET NULL,
    agent_ref    TEXT,
    vantage_name TEXT NOT NULL,
    target       TEXT NOT NULL,
    target_ip    TEXT,
    params       JSONB NOT NULL,
    summary      JSONB,
    error        TEXT,
    event_count  INTEGER NOT NULL DEFAULT 0 CHECK (event_count >= 0),
    created_at   TIMESTAMPTZ NOT NULL,
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    deadline     TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS tool_runs_created ON tool_runs (created_at DESC);
-- The active-run caps count these.
CREATE INDEX IF NOT EXISTS tool_runs_active_user ON tool_runs (user_id) WHERE status IN ('queued', 'running');
CREATE INDEX IF NOT EXISTS tool_runs_active_agent ON tool_runs (agent_id) WHERE status IN ('queued', 'running');
CREATE INDEX IF NOT EXISTS tool_runs_active_target ON tool_runs (target_ip) WHERE status IN ('queued', 'running');
-- An agent's job queue, oldest first.
CREATE INDEX IF NOT EXISTS tool_runs_queued ON tool_runs (agent_id, created_at) WHERE status = 'queued';
-- The daily prune.
CREATE INDEX IF NOT EXISTS tool_runs_finished ON tool_runs (finished_at) WHERE finished_at IS NOT NULL;

-- What a run reported, in order. A plain table: a run has at most 5,000.
CREATE TABLE IF NOT EXISTS tool_run_events (
    run_id UUID NOT NULL REFERENCES tool_runs (id) ON DELETE CASCADE,
    seq    INTEGER NOT NULL CHECK (seq > 0),
    at     TIMESTAMPTZ NOT NULL,
    type   TEXT NOT NULL,
    data   JSONB NOT NULL,
    PRIMARY KEY (run_id, seq)
);
```

- [ ] **Step 9: The service additions**

In `backend/internal/services/agent_service.go`, insert this function immediately before the comment line `// Delete unregisters an agent. Its metrics and container rows go with it via`:

```go
// SetToolsEnabled flips the admin's network-tools switch for an agent.
// ErrAgentNotFound when no agent has that readable id.
func (s *AgentService) SetToolsEnabled(ctx context.Context, agentID string, enabled bool) (*models.Agent, error) {
	res := s.db.WithContext(ctx).Model(&models.Agent{}).Where("agent_id = ?", agentID).
		Updates(map[string]interface{}{"tools_enabled": enabled, "updated_at": time.Now()})
	if res.Error != nil {
		return nil, fmt.Errorf("setting tools for agent %s: %w", agentID, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrAgentNotFound
	}
	return s.Get(ctx, agentID)
}

```

In the same file, in `type AgentSystemInfo struct`, after `DockerAvailable *bool` add:

```go
	// ToolsLocal is the agent's ENABLE_TOOLS flag. nil (an agent too old to
	// send it) is stored as NULL, so Sentinel can tell "too old" from "off".
	ToolsLocal *bool
```

In `Heartbeat`, directly after

```go
	if info.DockerAvailable != nil {
		updates["docker_available"] = *info.DockerAvailable
	}
```

add:

```go
	// Written every time, absent included: an agent downgraded to a version
	// without tools must stop counting as able to run them.
	updates["tools_local"] = info.ToolsLocal
```

(A nil `*bool` in a GORM `Updates` map writes NULL; `Update` already relies on this for `ip_address_override`.)

In `backend/internal/services/auth_service.go`, insert immediately before the comment `// GenerateMFAToken issues a short-lived (5-minute) token used to complete an`:

```go
// SetNetTools grants or removes a user's "Network tools" permission. An
// unknown user is an error wrapping gorm.ErrRecordNotFound.
func (s *AuthService) SetNetTools(ctx context.Context, userID uuid.UUID, enabled bool) (*models.User, error) {
	res := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).
		Updates(map[string]interface{}{"net_tools": enabled, "updated_at": time.Now()})
	if res.Error != nil {
		return nil, fmt.Errorf("setting network tools for user %s: %w", userID, res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("user %s not found: %w", userID, gorm.ErrRecordNotFound)
	}
	return s.GetUserByID(ctx, userID)
}

```

- [ ] **Step 10: The handler additions**

In `backend/internal/api/agent_handler.go`, in `type heartbeatRequest struct`, after `DockerAvailable *bool  \`json:"docker_available"\`` add:

```go
	// ToolsLocal is the agent's ENABLE_TOOLS flag. Agents built before the
	// network tools never send it, which is how Sentinel knows they are too
	// old to run them.
	ToolsLocal *bool `json:"tools_local"`
```

In `HeartbeatHandler`, in the `info := services.AgentSystemInfo{...}` literal, after `DockerAvailable: req.DockerAvailable,` add:

```go
			ToolsLocal:      req.ToolsLocal,
```

In `backend/internal/api/auth_handler.go`, in `GetCurrentUserHandler`'s `respondSuccess(c, http.StatusOK, gin.H{...})`, after `"mfa_enabled": user.MFAEnabled,` add:

```go
			"net_tools":   user.NetTools,
```

In `backend/internal/api/monitor_sharing_handler.go`, in `ListUsersHandler`, inside `if isAdmin { ... }`, after `entry["is_admin"] = u.IsAdmin` add:

```go
				entry["net_tools"] = u.NetTools
```

- [ ] **Step 11: Run the tests**

Run: `./scripts/test-db.sh -run 'TestDBToolRunsSchema|TestDBToolRunModelRoundTrip|TestDBHeartbeatToolsLocal|TestDBSetToolsEnabled|TestDBSetNetTools|TestDBHeartbeatHandlerToolsLocal|TestDBMeIncludesNetTools|TestDBUsersListNetTools' -v`
Expected: PASS for all eight. The migration applies in every test database through `testdb.Open`.

Run: `go vet ./... && go test ./internal/models/ ./internal/services/ ./internal/api/`
Expected: no vet output; PASS (DB tests skip without the database).

- [ ] **Step 12: Commit**

```bash
git add backend/migrations/059_network_tools.sql backend/internal/models/tool_run.go backend/internal/models/tool_run_test.go \
  backend/internal/models/user.go backend/internal/models/agent.go backend/internal/models/audit.go backend/internal/models/setting.go \
  backend/internal/services/agent_service.go backend/internal/services/auth_service.go backend/internal/services/net_tools_db_test.go \
  backend/internal/api/agent_handler.go backend/internal/api/auth_handler.go backend/internal/api/monitor_sharing_handler.go \
  backend/internal/api/net_tools_fields_db_test.go
git commit -m "feat(tools): tool run schema, models, grant and agent tools flags

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: `internal/stream` — the SSE writer and the hub

This is Sentinel's first push channel. The writer formats and flushes SSE frames. The hub fans messages out by key and drops a subscriber that falls behind instead of blocking the publisher.

**Files:**
- Create: `backend/internal/stream/sse.go`
- Create: `backend/internal/stream/hub.go`
- Test: `backend/internal/stream/sse_test.go`
- Test: `backend/internal/stream/hub_test.go`

**Interfaces:**
- Consumes: the standard library only.
- Produces (shared contract, "`internal/stream`"):
  - `const KeepAlive = 15 * time.Second`
  - `type Message struct{ ID, Event string; Data []byte }`
  - `func NewWriter(w http.ResponseWriter) (*Writer, error)`. It returns `ErrNoFlush` when `w` is not an `http.Flusher`.
  - `func (w *Writer) Send(m Message) error` and `func (w *Writer) Comment(text string) error`
  - `func NewHub(buffer int) *Hub`, `func (h *Hub) Subscribe(key string) *Subscription` and `func (h *Hub) Publish(key string, m Message)`
  - `func (s *Subscription) C() <-chan Message`, `Dropped() bool` and `Close()` (idempotent)
  - `var ErrNoFlush` (contract change 3)
- Frame format (exact bytes): `id: <ID>\n` when ID is set, `event: <Event>\n` when Event is set, one `data: <line>\n` per line of Data (an empty Data still writes `data: \n`), then `\n`. A comment is `: <text>\n\n`. Every write is flushed.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/stream/sse_test.go`:

```go
package stream

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewWriterHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	if _, err := NewWriter(rec); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !rec.Flushed {
		t.Errorf("code %d, flushed %v: want 200, flushed", rec.Code, rec.Flushed)
	}
	for k, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestSendFrames(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		send func() error
		want string
	}{
		{func() error { return w.Send(Message{ID: "7", Event: "event", Data: []byte(`{"seq":7}`)}) },
			"id: 7\nevent: event\ndata: {\"seq\":7}\n\n"},
		{func() error { return w.Send(Message{Data: []byte("a\nb")}) },
			"data: a\ndata: b\n\n"},
		{func() error { return w.Send(Message{Event: "end"}) },
			"event: end\ndata: \n\n"},
		{func() error { return w.Comment("keep-alive") },
			": keep-alive\n\n"},
	}
	for i, s := range steps {
		rec.Body.Reset()
		rec.Flushed = false
		if err := s.send(); err != nil {
			t.Fatal(err)
		}
		if got := rec.Body.String(); got != s.want || !rec.Flushed {
			t.Errorf("step %d wrote %q (flushed %v), want %q flushed", i, got, rec.Flushed, s.want)
		}
	}
}

// noFlush is a ResponseWriter that cannot flush.
type noFlush struct{ http.ResponseWriter }

func TestNewWriterNeedsFlusher(t *testing.T) {
	if _, err := NewWriter(noFlush{httptest.NewRecorder()}); !errors.Is(err, ErrNoFlush) {
		t.Errorf("err = %v, want ErrNoFlush", err)
	}
}
```

Create `backend/internal/stream/hub_test.go`:

```go
package stream

import (
	"strconv"
	"sync"
	"testing"
)

func msg(id int) Message { return Message{ID: strconv.Itoa(id), Event: "event"} }

// recv takes what is buffered now, without waiting.
func recv(s *Subscription) []string {
	var ids []string
	for {
		select {
		case m, ok := <-s.C():
			if !ok {
				return append(ids, "closed")
			}
			ids = append(ids, m.ID)
		default:
			return ids
		}
	}
}

func TestHubFanOutInOrder(t *testing.T) {
	h := NewHub(8)
	a, b := h.Subscribe("run-1"), h.Subscribe("run-1")
	other := h.Subscribe("run-2")
	for i := 1; i <= 3; i++ {
		h.Publish("run-1", msg(i))
	}
	for name, s := range map[string]*Subscription{"a": a, "b": b} {
		if got := recv(s); len(got) != 3 || got[0] != "1" || got[1] != "2" || got[2] != "3" {
			t.Errorf("%s got %v, want [1 2 3]", name, got)
		}
	}
	if got := recv(other); len(got) != 0 {
		t.Errorf("a subscriber of another key got %v", got)
	}
}

// A subscriber that falls behind is dropped; the others keep receiving and
// the publisher never blocks.
func TestHubDropsSlowSubscriber(t *testing.T) {
	h := NewHub(2)
	slow, fast := h.Subscribe("k"), h.Subscribe("k")
	for i := 1; i <= 3; i++ {
		h.Publish("k", msg(i))
		recv(fast) // fast keeps up
	}
	if !slow.Dropped() || fast.Dropped() {
		t.Fatalf("slow dropped %v, fast dropped %v: want true, false", slow.Dropped(), fast.Dropped())
	}
	if got := recv(slow); len(got) != 3 || got[0] != "1" || got[1] != "2" || got[2] != "closed" {
		t.Errorf("slow got %v, want its buffer [1 2] then closed", got)
	}
	h.Publish("k", msg(4))
	if got := recv(fast); len(got) != 1 || got[0] != "4" {
		t.Errorf("fast got %v after the drop, want [4]", got)
	}
}

func TestHubCloseIsIdempotentAndForgets(t *testing.T) {
	h := NewHub(1)
	s := h.Subscribe("k")
	s.Close()
	s.Close()
	if _, ok := <-s.C(); ok {
		t.Error("channel still open after Close")
	}
	if s.Dropped() {
		t.Error("a closed subscription reports dropped")
	}
	h.mu.Lock()
	n := len(h.subs)
	h.mu.Unlock()
	if n != 0 {
		t.Errorf("hub still holds %d keys after the last Close", n)
	}
	h.Publish("k", msg(1))      // no subscribers: a no-op
	h.Publish("nobody", msg(1)) // never subscribed: a no-op
}

// Run with -race: publishers, subscribers and closers at once.
func TestHubConcurrentUse(t *testing.T) {
	h := NewHub(4)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				h.Publish("k", msg(i))
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				s := h.Subscribe("k")
				recv(s)
				s.Close()
			}
		}()
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/stream/ -v`
Expected: the build fails with `undefined: NewWriter`, `undefined: Message`, `undefined: NewHub` and others.

- [ ] **Step 3: Write the writer**

Create `backend/internal/stream/sse.go`:

```go
// Package stream is Sentinel's live push channel: a Server-Sent Events
// writer and an in-memory hub that fans messages out to subscribers by key.
// The network tools stream run events through it; network phase 6 reuses it
// for live maps.
package stream

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"time"
)

// KeepAlive is how often a quiet stream sends a comment, so proxies and
// browsers do not close it as idle.
const KeepAlive = 15 * time.Second

// Message is one SSE frame. Data is already-encoded JSON.
type Message struct {
	ID    string // "" = no id line
	Event string // "" = the default "message" event
	Data  []byte
}

// Writer writes SSE frames to one response, flushing each.
type Writer struct {
	w http.ResponseWriter
	f http.Flusher
}

// ErrNoFlush is returned by NewWriter for a response that cannot flush, which
// would buffer the whole stream.
var ErrNoFlush = errors.New("stream: the response writer cannot flush")

// NewWriter starts an event stream: it sets the SSE headers (including
// X-Accel-Buffering: no, so nginx passes frames through unbuffered), writes
// 200 and flushes.
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrNoFlush
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &Writer{w: w, f: f}, nil
}

// Send writes one frame: an id line and an event line when set, one data
// line per line of Data, then the blank line that ends the frame.
func (w *Writer) Send(m Message) error {
	var b bytes.Buffer
	if m.ID != "" {
		b.WriteString("id: " + m.ID + "\n")
	}
	if m.Event != "" {
		b.WriteString("event: " + m.Event + "\n")
	}
	for _, line := range strings.Split(string(m.Data), "\n") {
		b.WriteString("data: " + line + "\n")
	}
	b.WriteString("\n")
	return w.write(b.Bytes())
}

// Comment writes a comment frame (": text"), which clients ignore; used as a
// keep-alive.
func (w *Writer) Comment(text string) error {
	return w.write([]byte(": " + text + "\n\n"))
}

func (w *Writer) write(b []byte) error {
	if _, err := w.w.Write(b); err != nil {
		return err
	}
	w.f.Flush()
	return nil
}
```

- [ ] **Step 4: Write the hub**

Create `backend/internal/stream/hub.go`:

```go
package stream

import (
	"sync"
	"sync/atomic"
)

// Hub fans messages out to subscribers by key (a tool run's id, say). It
// never blocks a publisher: a subscriber whose buffer is full is dropped,
// and its reader is expected to reconnect and catch up from the database.
type Hub struct {
	buffer int

	mu   sync.Mutex
	subs map[string]map[*Subscription]struct{}
}

// NewHub returns a hub whose subscriptions buffer up to buffer messages
// (at least 1).
func NewHub(buffer int) *Hub {
	if buffer < 1 {
		buffer = 1
	}
	return &Hub{buffer: buffer, subs: make(map[string]map[*Subscription]struct{})}
}

// Subscription is one reader of one key.
type Subscription struct {
	hub     *Hub
	key     string
	ch      chan Message
	closed  bool // guarded by hub.mu
	dropped atomic.Bool
}

// Subscribe starts receiving the messages published to key from now on.
func (h *Hub) Subscribe(key string) *Subscription {
	s := &Subscription{hub: h, key: key, ch: make(chan Message, h.buffer)}
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.subs[key]
	if set == nil {
		set = make(map[*Subscription]struct{})
		h.subs[key] = set
	}
	set[s] = struct{}{}
	return s
}

// Publish delivers m to every subscriber of key without blocking. A
// subscriber whose buffer is full is dropped: its channel is closed and
// Dropped reports true.
func (h *Hub) Publish(key string, m Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[key] {
		select {
		case s.ch <- m:
		default:
			s.dropped.Store(true)
			h.removeLocked(s)
		}
	}
}

// removeLocked closes s and forgets it. Caller holds h.mu.
func (h *Hub) removeLocked(s *Subscription) {
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
	set := h.subs[s.key]
	delete(set, s)
	if len(set) == 0 {
		delete(h.subs, s.key)
	}
}

// C delivers the messages; it is closed when the subscription is dropped
// or closed.
func (s *Subscription) C() <-chan Message { return s.ch }

// Dropped reports whether the hub dropped this subscriber for falling
// behind.
func (s *Subscription) Dropped() bool { return s.dropped.Load() }

// Close stops the subscription. Safe to call more than once, and after a
// drop.
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.hub.removeLocked(s)
}
```

The hub closes a dropped or closed subscription's channel while holding the same mutex that `Publish` sends under. A publisher therefore never sends on a closed channel. Deleting from the inner map while `Publish` ranges over it is safe in Go.

- [ ] **Step 5: Run the tests, with the race detector**

Run: `go test -race ./internal/stream/ -v`
Expected: PASS for `TestNewWriterHeaders`, `TestSendFrames`, `TestNewWriterNeedsFlusher`, `TestHubFanOutInOrder`, `TestHubDropsSlowSubscriber`, `TestHubCloseIsIdempotentAndForgets` and `TestHubConcurrentUse`, with no race reports.

- [ ] **Step 6: Vet and commit**

Run: `go vet ./internal/stream/`
Expected: no output.

```bash
git add backend/internal/stream/
git commit -m "feat(tools): SSE writer and in-memory stream hub

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: `internal/toolruns` — run creation, settings and vantages

The service skeleton and the guardrails, in the spec's order:

1. Parameters (`nettools.Normalize`).
2. The vantage: the server switch, or the agent's two switches and its recent poll.
3. Target resolution: once, IPv4 only.
4. The allowlist, which always refuses the always-blocked ranges.
5. The per-target rate.
6. The active-run caps, under a transaction-level advisory lock.
7. The insert, the audit entry, and then a launch (Sentinel) or a wake (agent).

This task also adds the settings load/save and the vantage list.

`New` sets `launch` to a function that does nothing. Task 9 replaces it with the local launcher. The tests here swap `launch` for a recorder, so they keep passing after Task 9.

Choices made here:
- The per-target bucket map is swept lazily inside `allow`, at most once per 15 minutes, so the service starts no goroutine of its own.
- `Create` resolves a host name with a 5 s timeout.
- `vantage_name` is `"Sentinel"` for server runs.
- The refusal message for an IPv6 literal is "<target> is an IPv6 address; S1 tools are IPv4 only".
- When the typed target is the address itself, the allowlist message names it once ("10.9.9.9 is not on…"); otherwise "name (address)".

**Files:**
- Create: `backend/internal/toolruns/service.go`
- Create: `backend/internal/toolruns/create.go`
- Create: `backend/internal/toolruns/vantages.go`
- Create: `backend/internal/toolruns/settings.go`
- Create: `backend/internal/toolruns/limits.go`
- Create: `backend/internal/toolruns/polls.go`
- Test: `backend/internal/toolruns/fixtures_db_test.go` (shared helpers for Tasks 8–10)
- Test: `backend/internal/toolruns/limits_test.go` (unit)
- Test: `backend/internal/toolruns/settings_db_test.go` (DB)
- Test: `backend/internal/toolruns/create_db_test.go` (DB)
- Test: `backend/internal/toolruns/vantages_db_test.go` (DB)

**Interfaces:**
- Consumes:
  - Task 6: `models.ToolRun`, the status and vantage constants, `models.Agent.ToolsEnabled/ToolsLocal`, the audit constants and the setting keys.
  - Task 7: `stream.Hub`, `stream.NewHub`.
  - Tasks 1–5 (`internal/nettools`, per the shared contract): `nettools.Tool`, `Spec`, `Params`, `Normalize` (errors are `*ParamError`), `Deadline`, `ParseAllowlist` returning `(*Allowlist, []EntryError)`, `(*Allowlist).Empty()`, `(*Allowlist).Allows(target string, ip net.IP) bool` (false for `AlwaysBlocked` addresses whatever matches), `EntryError{Entry, Message}`, `Runner` and `(*Runner).Run`, and `Event`/`Emitter`.
  - Existing: `services.SettingsService` (`GetString`, `GetBool`, `GetInt`, `SetString`, `SetBool`, `SetInt`), `services.Actor`, `services.AgentService.Register`, and `testdb`.
- Produces (shared contract, "`internal/toolruns`"):
  - The constants block (`UserRatePerMinute` … `MaxRetentionDays`).
  - `Settings`, `LoadSettings(ctx, *services.SettingsService) Settings`, `SaveSettings(ctx, *services.SettingsService, Settings) (Settings, error)` and `*SettingsError{Message string; Entries []nettools.EntryError}`.
  - `AuditRecorder`, `Deps`, `Service`, `New(Deps) *Service`, `Requester`, `Vantage`, `CreateRequest`, `Refusal{Status, Code, Message}` and the `Code*` constants.
  - `func (s *Service) Create(ctx, who Requester, req CreateRequest) (*models.ToolRun, error)`; its user-facing failures are `*Refusal`.
  - `VantageView`, the `Reason*` constants, `func (s *Service) Vantages(ctx) ([]VantageView, error)` and `func (s *Service) AllowlistEmpty(ctx) bool`.
  - Unexported, for Tasks 9–10:
    - The `Service` fields `runTool`, `launch`, `polls` (`seen`, `lastSeen`), `mu`, `wakers` and `cancels`.
    - `wakeChan(agentID uuid.UUID) chan struct{}` and `wake(agentID)`.
    - `refuse(status, code, format, args...) *Refusal`, `sentinelVantageName` and `Requester.actor() services.Actor`.
  - Test helpers for Tasks 9–10 (`fixtures_db_test.go`):
    - `newEnv(t, allowlist...)`: nil means `testAllowlist`; pass `[]string{}...` for an empty list.
    - `env.user`, `env.newAgent`, `env.readyAgent`, `env.insertRun` and `env.reload`.
    - `pingReq`, `agentPing`, `refusal`, `boolPtr`, `clock`, `fakeAudit` (`byAction`) and `fakeResolve`.
- Refusal messages (exact):
  - `runs from the Sentinel server are switched off`
  - `no such agent`
  - `vantage: kind must be sentinel or agent`
  - `<target> doesn't resolve from Sentinel — use its IP address`
  - `<target> is an IPv6 address; S1 tools are IPv4 only`
  - `<target> only has IPv6 addresses; S1 tools are IPv4 only`
  - `<target> (<ip>) is not on the network tools allowlist`, or `<ip> is not on the network tools allowlist` when the address was typed
  - `too many runs against <ip>; wait a minute`
  - `You already have 3 runs in progress`
  - `3 runs are already running against <ip>`
  - `Sentinel is already running 10 tool runs`
  - `<agent name> is already running 2 tool runs`
  - Vantage reasons:
    - `This agent's version can't run tools — update it`
    - `network tools are switched off for <name> in Sentinel`
    - `<name> doesn't have ENABLE_TOOLS=true in its config`
    - `<name> hasn't asked for jobs in the last minute (offline?)`
  - `invalid_params` carries `ParamError.Error()` (`<field>: <message>`).

- [ ] **Step 1: Write the shared test fixtures**

Create `backend/internal/toolruns/fixtures_db_test.go`:

```go
package toolruns

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// testAllowlist is what newEnv saves unless a test passes its own.
var testAllowlist = []string{"10.0.0.0/24", "fileserver.example.org", "*.lab.example.org"}

// testHosts is the fake resolver's zone.
var testHosts = map[string][]net.IP{
	"fileserver.example.org": {net.ParseIP("10.0.0.5")},
	"meta.lab.example.org":   {net.ParseIP("169.254.169.254")},
	"v6only.example.org":     {net.ParseIP("2001:db8::5")},
	"dual.lab.example.org":   {net.ParseIP("2001:db8::6"), net.ParseIP("10.0.0.6")},
}

func fakeResolve(_ context.Context, host string) ([]net.IP, error) {
	if ips, ok := testHosts[host]; ok {
		return ips, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// clock is a settable time source for Deps.Now.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Now().UTC().Truncate(time.Millisecond)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type auditEntry struct {
	actor        services.Actor
	action       string
	resourceType string
	resourceID   *uuid.UUID
	changes      models.AuditChanges
}

// fakeAudit records what the service audits.
type fakeAudit struct {
	mu      sync.Mutex
	entries []auditEntry
}

func (f *fakeAudit) Record(_ context.Context, actor services.Actor, action, resourceType string, resourceID *uuid.UUID, changes models.AuditChanges) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, auditEntry{actor, action, resourceType, resourceID, changes})
}

func (f *fakeAudit) byAction(action string) []auditEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []auditEntry
	for _, e := range f.entries {
		if e.action == action {
			out = append(out, e)
		}
	}
	return out
}

type launchCall struct {
	run  *models.ToolRun
	spec nettools.Spec
}

// env is a service under test on a fresh database. Its launch hook records
// calls instead of running anything; tests of local runs put the real
// launcher back.
type env struct {
	db       *gorm.DB
	settings *services.SettingsService
	svc      *Service
	clock    *clock
	audit    *fakeAudit
	hub      *stream.Hub
	launched chan launchCall
}

func newEnv(t *testing.T, allowlist ...string) *env {
	t.Helper()
	db := testdb.Open(t)
	ss := services.NewSettingsService(db)
	if allowlist == nil {
		allowlist = testAllowlist
	}
	_, err := SaveSettings(context.Background(), ss, Settings{Allowlist: allowlist, ServerEnabled: true, RetentionDays: DefaultRetentionDays})
	testdb.Must(t, err)
	e := &env{db: db, settings: ss, clock: newClock(), audit: &fakeAudit{}, hub: stream.NewHub(64),
		launched: make(chan launchCall, 64)}
	e.svc = New(Deps{DB: db, Settings: ss, Audit: e.audit, Hub: e.hub, Resolve: fakeResolve, Now: e.clock.Now})
	e.svc.launch = func(run *models.ToolRun, spec nettools.Spec) { e.launched <- launchCall{run, spec} }
	return e
}

// user inserts a user and returns them as a requester.
func (e *env) user(t *testing.T, admin bool) Requester {
	t.Helper()
	id := testdb.NewUser(t, e.db, admin)
	return Requester{UserID: id, Username: "u" + id.String()[:8], IsAdmin: admin, IP: "192.0.2.10"}
}

func boolPtr(b bool) *bool { return &b }

// newAgent registers an agent with the given switches; local nil means the
// agent never reported tools_local (too old).
func (e *env) newAgent(t *testing.T, name string, enabled bool, local *bool) *models.Agent {
	t.Helper()
	a := &models.Agent{Name: name, OSType: "linux", CheckInterval: 60, RetryAttempts: 3}
	testdb.Must(t, services.NewAgentService(e.db).Register(context.Background(), a))
	testdb.Exec(t, e.db, `UPDATE agents SET tools_enabled = ?, tools_local = ? WHERE id = ?`, enabled, local, a.ID)
	a.ToolsEnabled, a.ToolsLocal = enabled, local
	return a
}

// readyAgent is an agent with both switches on that polled just now.
func (e *env) readyAgent(t *testing.T, name string) *models.Agent {
	t.Helper()
	a := e.newAgent(t, name, true, boolPtr(true))
	e.svc.polls.seen(a.ID, e.clock.Now())
	return a
}

func pingReq(target string) CreateRequest {
	return CreateRequest{Tool: nettools.ToolPing, Vantage: Vantage{Kind: models.VantageSentinel}, Target: target}
}

func agentPing(a *models.Agent, target string) CreateRequest {
	return CreateRequest{Tool: nettools.ToolPing, Vantage: Vantage{Kind: models.VantageAgent, AgentID: a.AgentID}, Target: target}
}

// refusal returns err as a *Refusal, failing the test when it is not one.
func refusal(t *testing.T, err error) *Refusal {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("err = %v, want a *Refusal", err)
	}
	return r
}

// insertRun writes a run straight to the table: a running ping from the
// Sentinel server against 10.0.0.200 unless mutate says otherwise.
func (e *env) insertRun(t *testing.T, mutate func(r *models.ToolRun)) *models.ToolRun {
	t.Helper()
	now := e.clock.Now()
	target := "10.0.0.200"
	r := &models.ToolRun{
		ID: uuid.New(), Tool: string(nettools.ToolPing), Status: models.ToolRunRunning, Username: "seed",
		VantageKind: models.VantageSentinel, VantageName: sentinelVantageName,
		Target: target, TargetIP: &target, Params: models.RawJSON(`{}`),
		CreatedAt: now, StartedAt: &now, Deadline: now.Add(2 * time.Minute),
	}
	if mutate != nil {
		mutate(r)
	}
	testdb.Must(t, e.db.Create(r).Error)
	return r
}

// reload reads a run back.
func (e *env) reload(t *testing.T, id uuid.UUID) *models.ToolRun {
	t.Helper()
	var r models.ToolRun
	testdb.Must(t, e.db.First(&r, "id = ?", id).Error)
	return &r
}
```

- [ ] **Step 2: Write the failing unit and settings tests**

Create `backend/internal/toolruns/limits_test.go`:

```go
package toolruns

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// 20 runs a minute per address, burst 20: the 21st at once is refused, one
// more is allowed once a token has refilled (one every 3 s), and addresses
// do not share a bucket.
func TestTargetLimiter(t *testing.T) {
	l := newTargetLimiter()
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= TargetRatePerMinute; i++ {
		if !l.allow("10.0.0.5", t0) {
			t.Fatalf("run %d refused", i)
		}
	}
	if l.allow("10.0.0.5", t0) {
		t.Error("the 21st run in the same instant was allowed")
	}
	if !l.allow("10.0.0.6", t0) {
		t.Error("another address was refused")
	}
	if !l.allow("10.0.0.5", t0.Add(4*time.Second)) {
		t.Error("refused 4 s later, after a token refilled")
	}
	if l.allow("10.0.0.5", t0.Add(4*time.Second)) {
		t.Error("two runs allowed on one refilled token")
	}
}

// Buckets idle for longer than targetIdleTTL are swept on a later call.
func TestTargetLimiterSweepsIdleBuckets(t *testing.T) {
	l := newTargetLimiter()
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	l.allow("10.0.0.5", t0)
	l.allow("10.0.0.6", t0.Add(targetIdleTTL+2*time.Minute))
	if _, ok := l.buckets["10.0.0.5"]; ok || len(l.buckets) != 1 {
		t.Errorf("buckets after the sweep: %d, want only 10.0.0.6", len(l.buckets))
	}
}

func TestPollTracker(t *testing.T) {
	p := newPollTracker()
	a := uuid.New()
	if _, ok := p.lastSeen(a); ok {
		t.Fatal("an agent that never polled has a last poll")
	}
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	p.seen(a, t0)
	p.seen(a, t0.Add(time.Second))
	if got, ok := p.lastSeen(a); !ok || !got.Equal(t0.Add(time.Second)) {
		t.Errorf("lastSeen = %v, %v; want %v", got, ok, t0.Add(time.Second))
	}
}
```

Create `backend/internal/toolruns/settings_db_test.go`:

```go
package toolruns

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBSettingsDefaultsAndRoundTrip(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	ss := services.NewSettingsService(db)

	got := LoadSettings(ctx, ss)
	if len(got.Allowlist) != 0 || got.Allowlist == nil || !got.ServerEnabled || got.RetentionDays != 30 {
		t.Fatalf("defaults = %+v, want an empty (non-nil) allowlist, server on, 30 days", got)
	}

	saved, err := SaveSettings(ctx, ss, Settings{
		Allowlist:     []string{" 10.0.0.0/24 ", "", "FileServer.Example.ORG", "10.0.0.0/24", "*.Lab.example.org"},
		ServerEnabled: false, RetentionDays: 7,
	})
	testdb.Must(t, err)
	want := Settings{Allowlist: []string{"10.0.0.0/24", "fileserver.example.org", "*.lab.example.org"}, ServerEnabled: false, RetentionDays: 7}
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("saved = %+v, want %+v", saved, want)
	}
	if got := LoadSettings(ctx, ss); !reflect.DeepEqual(got, want) {
		t.Errorf("loaded = %+v, want %+v", got, want)
	}
	if raw := ss.GetString(ctx, models.SettingNetToolsAllowlist, ""); raw != `["10.0.0.0/24","fileserver.example.org","*.lab.example.org"]` {
		t.Errorf("stored allowlist = %s", raw)
	}
}

// One bad entry refuses the whole save, every bad entry is reported, and
// nothing is written.
func TestDBSettingsRefusesBadEntries(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	ss := services.NewSettingsService(db)
	_, err := SaveSettings(ctx, ss, Settings{
		Allowlist:     []string{"10.0.0.0/24", "10.0.0.0/7", "bad host!", "2001:db8::/32"},
		ServerEnabled: true, RetentionDays: 30,
	})
	var se *SettingsError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *SettingsError", err)
	}
	if len(se.Entries) != 3 || se.Entries[0].Entry != "10.0.0.0/7" || se.Entries[1].Entry != "bad host!" ||
		se.Entries[2].Entry != "2001:db8::/32" {
		t.Fatalf("entries = %+v, want the /7, the bad host and the IPv6 CIDR, in order", se.Entries)
	}
	if se.Entries[0].Message != "too broad: use /8 or narrower" {
		t.Errorf("the /7's message = %q", se.Entries[0].Message)
	}
	if got := LoadSettings(ctx, ss); len(got.Allowlist) != 0 {
		t.Errorf("a refused save wrote %v", got.Allowlist)
	}
}

func TestDBSettingsRetentionBounds(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	ss := services.NewSettingsService(db)
	for _, days := range []int{0, 366} {
		_, err := SaveSettings(ctx, ss, Settings{RetentionDays: days, ServerEnabled: true})
		var se *SettingsError
		if !errors.As(err, &se) || len(se.Entries) != 0 || se.Message != "retention_days must be between 1 and 365" {
			t.Errorf("retention %d: err = %v, want a SettingsError with no entries", days, err)
		}
	}
	for _, days := range []int{1, 365} {
		got, err := SaveSettings(ctx, ss, Settings{RetentionDays: days, ServerEnabled: true})
		if err != nil || got.RetentionDays != days {
			t.Errorf("retention %d: %+v, %v", days, got, err)
		}
	}
	// A value out of range in the table reads as the default.
	testdb.Must(t, ss.SetInt(ctx, models.SettingNetToolsRetentionDays, 9999))
	testdb.Must(t, ss.SetString(ctx, models.SettingNetToolsAllowlist, `not json`))
	if got := LoadSettings(ctx, ss); got.RetentionDays != 30 || len(got.Allowlist) != 0 {
		t.Errorf("bad stored values read as %+v, want 30 days and an empty allowlist", got)
	}
}
```

- [ ] **Step 3: Write the failing create and vantage tests**

Create `backend/internal/toolruns/create_db_test.go`. `TestDBCreateRefusals` pins Review Focus 3: `meta.lab.example.org` matches the `*.lab.example.org` entry but resolves to `169.254.169.254`, so the run is refused. `TestDBCreateCapsUnderConcurrency` releases 10 (then 12) creates together through a start barrier and asserts that exactly the cap succeeds.

```go
package toolruns

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A run from the Sentinel server is inserted running, with its deadline,
// normalized parameters and snapshots, audited, and handed to launch with
// the checked address as TargetIP.
func TestDBCreateSentinelRun(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	who := e.user(t, false)
	now := e.clock.Now()

	run, err := e.svc.Create(ctx, who, pingReq("fileserver.example.org"))
	testdb.Must(t, err)

	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunRunning || got.StartedAt == nil || !got.StartedAt.Equal(now) ||
		!got.Deadline.Equal(now.Add(2*time.Minute)) || !got.CreatedAt.Equal(now) {
		t.Errorf("run = %+v, want running, started now, deadline now+2m", got)
	}
	if got.Target != "fileserver.example.org" || got.TargetIP == nil || *got.TargetIP != "10.0.0.5" ||
		got.VantageKind != models.VantageSentinel || got.VantageName != "Sentinel" || got.AgentUUID != nil ||
		got.AgentRef != nil || got.Username != who.Username || got.UserID == nil || *got.UserID != who.UserID {
		t.Errorf("run = %+v, want the typed target, 10.0.0.5, the Sentinel vantage and the requester", got)
	}
	var params nettools.Params
	testdb.Must(t, json.Unmarshal(got.Params, &params))
	if params.Count != 5 || params.IntervalMS != 1000 || params.TimeoutMS != 2000 || params.Size == nil || *params.Size != 56 {
		t.Errorf("params = %+v, want the ping defaults 5 / 1000 / 2000 / 56", params)
	}

	select {
	case call := <-e.launched:
		if call.run.ID != run.ID || call.spec.TargetIP != "10.0.0.5" || call.spec.Params.Count != 5 {
			t.Errorf("launch(%v, %+v), want the run and its normalized spec with TargetIP 10.0.0.5", call.run.ID, call.spec)
		}
	default:
		t.Fatal("launch was not called")
	}

	started := e.audit.byAction(models.ActionToolRunStarted)
	if len(started) != 1 || started[0].resourceID == nil || *started[0].resourceID != run.ID ||
		started[0].actor.UserID != who.UserID || started[0].actor.IP != "192.0.2.10" ||
		started[0].changes.Summary["tool"] != "ping" || started[0].changes.Summary["target"] != "fileserver.example.org" {
		t.Errorf("audit = %+v, want one tool_run_started for the run by the requester", started)
	}
}

// An agent run is queued, keeps the agent's readable id and name, gets a
// deadline that outlasts the pickup timeout, and wakes the agent's poll.
func TestDBCreateAgentRunQueues(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	now := e.clock.Now()

	run, err := e.svc.Create(ctx, e.user(t, false), agentPing(agent, "10.0.0.5"))
	testdb.Must(t, err)
	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunQueued || got.StartedAt != nil || got.AgentUUID == nil || *got.AgentUUID != agent.ID ||
		got.AgentRef == nil || *got.AgentRef != agent.AgentID || got.VantageName != "file-server" ||
		!got.Deadline.Equal(now.Add(PickupTimeout+2*time.Minute)) {
		t.Errorf("run = %+v, want queued for file-server with deadline now+30s+2m", got)
	}
	select {
	case <-e.svc.wakeChan(agent.ID):
	default:
		t.Error("the agent's wake signal was not sent")
	}
	select {
	case <-e.launched:
		t.Error("an agent run was launched locally")
	default:
	}
}

func TestDBCreateRefusals(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	who := e.user(t, false)
	offline := e.newAgent(t, "offline-box", true, boolPtr(true)) // never polled
	tooOld := e.newAgent(t, "old-box", true, nil)

	cases := []struct {
		name   string
		req    CreateRequest
		status int
		code   string
		msg    string
	}{
		{"parameters that cannot finish in time",
			CreateRequest{Tool: nettools.ToolPing, Vantage: Vantage{Kind: models.VantageSentinel}, Target: "10.0.0.5",
				Params: nettools.Params{Count: 100, IntervalMS: 5000}},
			http.StatusUnprocessableEntity, CodeInvalidParams, "at most 115 seconds"},
		{"an unknown tool",
			CreateRequest{Tool: "nmap", Vantage: Vantage{Kind: models.VantageSentinel}, Target: "10.0.0.5"},
			http.StatusUnprocessableEntity, CodeInvalidParams, "tool"},
		{"an unknown vantage kind",
			CreateRequest{Tool: nettools.ToolPing, Vantage: Vantage{Kind: "moon"}, Target: "10.0.0.5"},
			http.StatusUnprocessableEntity, CodeInvalidParams, "vantage: kind must be sentinel or agent"},
		{"an unknown agent",
			agentPing(&models.Agent{AgentID: "agent_ffffffffff"}, "10.0.0.5"),
			http.StatusNotFound, CodeNotFound, "no such agent"},
		{"an agent that has not polled", agentPing(offline, "10.0.0.5"),
			http.StatusConflict, CodeVantageNotReady, "offline-box hasn't asked for jobs in the last minute (offline?)"},
		{"an agent too old for tools", agentPing(tooOld, "10.0.0.5"),
			http.StatusConflict, CodeVantageNotReady, "This agent's version can't run tools — update it"},
		{"an address outside the allowlist", pingReq("10.9.9.9"),
			http.StatusUnprocessableEntity, CodeTargetNotAllowed, "10.9.9.9 is not on the network tools allowlist"},
		{"a name outside the allowlist", pingReq("printer.example.org"),
			http.StatusUnprocessableEntity, CodeResolveFailed, "printer.example.org doesn't resolve from Sentinel — use its IP address"},
		{"an IPv6 address", pingReq("2001:db8::1"),
			http.StatusUnprocessableEntity, CodeIPv6Unsupported, "2001:db8::1 is an IPv6 address; S1 tools are IPv4 only"},
		{"a name with only IPv6 addresses", pingReq("v6only.example.org"),
			http.StatusUnprocessableEntity, CodeIPv6Unsupported, "v6only.example.org only has IPv6 addresses; S1 tools are IPv4 only"},
		// Review Focus 3: *.lab.example.org allows the name, but it resolves
		// to the cloud metadata address, which is always blocked.
		{"an allowed name that resolves to the metadata address", pingReq("meta.lab.example.org"),
			http.StatusUnprocessableEntity, CodeTargetNotAllowed, "meta.lab.example.org (169.254.169.254) is not on the network tools allowlist"},
	}
	for _, c := range cases {
		_, err := e.svc.Create(ctx, who, c.req)
		r := refusal(t, err)
		if r.Status != c.status || r.Code != c.code || !strings.Contains(r.Message, c.msg) {
			t.Errorf("%s: %d %s %q, want %d %s containing %q", c.name, r.Status, r.Code, r.Message, c.status, c.code, c.msg)
		}
	}
	var n int64
	testdb.Must(t, e.db.Raw(`SELECT count(*) FROM tool_runs`).Scan(&n).Error)
	if n != 0 {
		t.Errorf("%d runs were inserted by refused creates", n)
	}
	// Only the two allowlist refusals are audited.
	refused := e.audit.byAction(models.ActionToolRunRefused)
	if len(refused) != 2 || refused[0].resourceID != nil || refused[0].resourceType != models.ResourceToolRun ||
		refused[1].changes.Summary["target_ip"] != "169.254.169.254" || refused[1].changes.Summary["code"] != CodeTargetNotAllowed {
		t.Errorf("refusal audit = %+v, want two tool_run_refused entries", refused)
	}

	// A name resolving to IPv6 and IPv4 uses the IPv4 address.
	run, err := e.svc.Create(ctx, who, pingReq("dual.lab.example.org"))
	testdb.Must(t, err)
	if run.TargetIP == nil || *run.TargetIP != "10.0.0.6" {
		t.Errorf("dual-stack name probed %v, want 10.0.0.6", run.TargetIP)
	}
}

// With the server switched off, Sentinel runs are refused; an empty
// allowlist refuses every target.
func TestDBCreateServerOffAndEmptyAllowlist(t *testing.T) {
	e := newEnv(t, []string{}...)
	ctx := context.Background()
	who := e.user(t, false)
	_, err := e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Code != CodeTargetNotAllowed {
		t.Errorf("empty allowlist: %s, want target_not_allowed", r.Code)
	}
	_, err = SaveSettings(ctx, e.settings, Settings{Allowlist: testAllowlist, ServerEnabled: false, RetentionDays: 30})
	testdb.Must(t, err)
	_, err = e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Status != http.StatusConflict || r.Code != CodeVantageNotReady ||
		r.Message != "runs from the Sentinel server are switched off" {
		t.Errorf("server off: %+v", r)
	}
}

// A lookup through the vantage's own resolver contacts no target, so it
// needs no allowlist entry and has no target_ip; a named server is a target.
func TestDBCreateDNS(t *testing.T) {
	e := newEnv(t, "10.0.0.53")
	ctx := context.Background()
	who := e.user(t, false)
	dns := func(server string) CreateRequest {
		return CreateRequest{Tool: nettools.ToolDNS, Vantage: Vantage{Kind: models.VantageSentinel},
			Target: "example.com", Params: nettools.Params{RecordType: "mx", Server: server}}
	}

	run, err := e.svc.Create(ctx, who, dns(""))
	testdb.Must(t, err)
	if run.TargetIP != nil || run.Status != models.ToolRunRunning || !run.Deadline.Equal(e.clock.Now().Add(15*time.Second)) {
		t.Errorf("system-resolver lookup = %+v, want no target_ip and a 15 s deadline", run)
	}
	call := <-e.launched
	if call.spec.TargetIP != "" || call.spec.Params.RecordType != "MX" {
		t.Errorf("launched spec %+v, want no TargetIP and record type MX", call.spec)
	}

	_, err = e.svc.Create(ctx, who, dns("8.8.8.8"))
	if r := refusal(t, err); r.Code != CodeTargetNotAllowed || r.Message != "8.8.8.8 is not on the network tools allowlist" {
		t.Errorf("a server outside the allowlist: %+v", r)
	}

	run, err = e.svc.Create(ctx, who, dns("10.0.0.53:5353"))
	testdb.Must(t, err)
	if run.TargetIP == nil || *run.TargetIP != "10.0.0.53" {
		t.Errorf("named server: target_ip %v, want 10.0.0.53", run.TargetIP)
	}
	if call := <-e.launched; call.spec.TargetIP != "10.0.0.53" || call.spec.Params.Server != "10.0.0.53:5353" {
		t.Errorf("launched spec %+v, want TargetIP 10.0.0.53 and the server with its port", call.spec)
	}
}

// Each cap refuses the run that would pass it, with its own message.
func TestDBCreateCaps(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ip := func(s string) *string { return &s }
	clear := func() { testdb.Exec(t, e.db, `DELETE FROM tool_runs`) }

	// Per user: 3 in progress.
	who := e.user(t, false)
	for i := 0; i < MaxActivePerUser; i++ {
		e.insertRun(t, func(r *models.ToolRun) { r.UserID = &who.UserID; r.TargetIP = ip(fmt.Sprintf("10.0.0.%d", 100+i)) })
	}
	_, err := e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Status != http.StatusTooManyRequests || r.Code != CodeLimit || r.Message != "You already have 3 runs in progress" {
		t.Errorf("per user: %+v", r)
	}
	// A finished run does not count.
	testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'done' WHERE id = (SELECT id FROM tool_runs LIMIT 1)`)
	_, err = e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	testdb.Must(t, err)
	clear()

	// Per target address, across users.
	for i := 0; i < MaxActivePerTarget; i++ {
		e.insertRun(t, func(r *models.ToolRun) { r.TargetIP = ip("10.0.0.5") })
	}
	_, err = e.svc.Create(ctx, e.user(t, false), pingReq("fileserver.example.org"))
	if r := refusal(t, err); r.Code != CodeLimit || r.Message != "3 runs are already running against 10.0.0.5" {
		t.Errorf("per target: %+v", r)
	}
	clear()

	// Overall.
	for i := 0; i < MaxActiveOverall; i++ {
		e.insertRun(t, func(r *models.ToolRun) { r.TargetIP = ip(fmt.Sprintf("10.0.0.%d", 100+i)) })
	}
	_, err = e.svc.Create(ctx, e.user(t, false), pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Code != CodeLimit || r.Message != "Sentinel is already running 10 tool runs" {
		t.Errorf("overall: %+v", r)
	}
	clear()

	// Per agent.
	agent := e.readyAgent(t, "file-server")
	for i := 0; i < MaxActivePerAgent; i++ {
		e.insertRun(t, func(r *models.ToolRun) {
			r.VantageKind, r.AgentUUID, r.Status = models.VantageAgent, &agent.ID, models.ToolRunQueued
			r.TargetIP = ip(fmt.Sprintf("10.0.0.%d", 100+i))
		})
	}
	_, err = e.svc.Create(ctx, e.user(t, false), agentPing(agent, "10.0.0.5"))
	if r := refusal(t, err); r.Code != CodeLimit || r.Message != "file-server is already running 2 tool runs" {
		t.Errorf("per agent: %+v", r)
	}
}

// createConcurrently runs n creates at once (released together by a start
// barrier) and returns how many succeeded and the refusal codes.
func createConcurrently(t *testing.T, e *env, n int, who func(i int) Requester, req func(i int) CreateRequest) (int, map[string]int) {
	t.Helper()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, codes := 0, map[string]int{}
	for i := 0; i < n; i++ {
		w, r := who(i), req(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := e.svc.Create(context.Background(), w, r)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ok++
			} else if rf, isRefusal := err.(*Refusal); isRefusal {
				codes[rf.Code]++
			} else {
				codes["error: "+err.Error()]++
			}
		}()
	}
	close(start)
	wg.Wait()
	return ok, codes
}

// Concurrent creates cannot overshoot a cap: exactly the cap succeeds.
func TestDBCreateCapsUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	target := func(i int) CreateRequest { return pingReq(fmt.Sprintf("10.0.0.%d", 10+i)) }

	one := e.user(t, false)
	ok, codes := createConcurrently(t, e, 10, func(int) Requester { return one }, target)
	if ok != MaxActivePerUser || codes[CodeLimit] != 10-MaxActivePerUser {
		t.Errorf("one user, 10 creates: %d succeeded, refusals %v; want exactly 3 and 7 limit", ok, codes)
	}
	testdb.Exec(t, e.db, `DELETE FROM tool_runs`)

	users := make([]Requester, 12)
	for i := range users {
		users[i] = e.user(t, false)
	}
	ok, codes = createConcurrently(t, e, 12, func(i int) Requester { return users[i] }, target)
	if ok != MaxActiveOverall || codes[CodeLimit] != 12-MaxActiveOverall {
		t.Errorf("12 users, 12 targets: %d succeeded, refusals %v; want exactly 10 and 2 limit", ok, codes)
	}
	var active int64
	testdb.Must(t, e.db.Raw(`SELECT count(*) FROM tool_runs WHERE status IN ('queued','running')`).Scan(&active).Error)
	if active != MaxActiveOverall {
		t.Errorf("%d active runs in the table, want 10", active)
	}
}

// The per-target rate: after 20 runs against one address in a minute the
// next is refused until a token refills.
func TestDBCreateTargetRate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for i := 0; i < TargetRatePerMinute; i++ {
		e.svc.targets.allow("10.0.0.5", e.clock.Now())
	}
	_, err := e.svc.Create(ctx, e.user(t, false), pingReq("10.0.0.5"))
	if r := refusal(t, err); r.Status != http.StatusTooManyRequests || r.Code != CodeLimit ||
		r.Message != "too many runs against 10.0.0.5; wait a minute" {
		t.Errorf("21st run: %+v", r)
	}
	e.clock.Add(4 * time.Second)
	if _, err := e.svc.Create(ctx, e.user(t, false), pingReq("10.0.0.5")); err != nil {
		t.Errorf("after a token refilled: %v", err)
	}
}
```

Create `backend/internal/toolruns/vantages_db_test.go`:

```go
package toolruns

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBVantages(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.newAgent(t, "a-too-old", true, nil)
	e.newAgent(t, "b-tools-off", false, boolPtr(true))
	e.newAgent(t, "C-not-on-server", true, boolPtr(false))
	e.newAgent(t, "d-offline", true, boolPtr(true))
	ready := e.readyAgent(t, "e-ready")
	stale := e.readyAgent(t, "f-stale")
	e.clock.Add(ReadyWindow)
	e.svc.polls.seen(ready.ID, e.clock.Now())
	e.clock.Add(time.Second) // f-stale's poll is now 61 s old; e-ready's 1 s

	got, err := e.svc.Vantages(ctx)
	testdb.Must(t, err)
	want := []VantageView{
		{Kind: models.VantageSentinel, Name: "Sentinel", Ready: true},
		{Kind: models.VantageAgent, Name: "a-too-old", Reason: ReasonAgentTooOld},
		{Kind: models.VantageAgent, Name: "b-tools-off", Reason: ReasonToolsOff},
		{Kind: models.VantageAgent, Name: "C-not-on-server", Reason: ReasonNotEnabledOnServer},
		{Kind: models.VantageAgent, Name: "d-offline", Reason: ReasonOffline},
		{Kind: models.VantageAgent, Name: "e-ready", Ready: true},
		{Kind: models.VantageAgent, Name: "f-stale", Reason: ReasonOffline},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d vantages, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g := got[i]
		g.AgentID = ""
		if g != want[i] {
			t.Errorf("vantage %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got[5].AgentID != ready.AgentID || got[6].AgentID != stale.AgentID {
		t.Errorf("agent ids %q %q, want %q %q", got[5].AgentID, got[6].AgentID, ready.AgentID, stale.AgentID)
	}

	_, err = SaveSettings(ctx, e.settings, Settings{Allowlist: []string{}, ServerEnabled: false, RetentionDays: 30})
	testdb.Must(t, err)
	got, err = e.svc.Vantages(ctx)
	testdb.Must(t, err)
	if got[0].Ready || got[0].Reason != ReasonServerDisabled {
		t.Errorf("sentinel with the server off = %+v", got[0])
	}
	if !e.svc.AllowlistEmpty(ctx) {
		t.Error("AllowlistEmpty false with an empty list")
	}
	_, err = SaveSettings(ctx, e.settings, Settings{Allowlist: []string{"10.0.0.0/24"}, ServerEnabled: true, RetentionDays: 30})
	testdb.Must(t, err)
	if e.svc.AllowlistEmpty(ctx) {
		t.Error("AllowlistEmpty true with an entry")
	}
}
```

- [ ] **Step 4: Run them and watch them fail**

Run: `go vet ./internal/toolruns/`
Expected: compile errors such as `undefined: New`, `undefined: Deps`, `undefined: Requester` and `undefined: newTargetLimiter`.

- [ ] **Step 5: Write the service skeleton**

Create `backend/internal/toolruns/service.go`:

```go
// Package toolruns owns the lifecycle of network tool runs (spec
// 2026-10-05-tools-s1): the guardrails at creation, running on the Sentinel
// server, the agent job queue, recording events, cancelling, sweeping and
// pruning. The tool_runs table is the record; the stream hub only carries
// live events to open browsers.
package toolruns

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
)

// Limits and timings (spec "Limits" and "Timeouts and sweeper").
const (
	UserRatePerMinute   = 30
	UserRateBurst       = 10
	TargetRatePerMinute = 20
	MaxActivePerUser    = 3
	MaxActivePerTarget  = 3
	MaxActiveOverall    = 10
	MaxActivePerAgent   = 2
	MaxEventsPerRun     = 5000
	MaxAgentPostBytes   = 64 << 10
	PickupTimeout       = 30 * time.Second
	OverdueGrace        = 15 * time.Second
	ReadyWindow         = 60 * time.Second
	LongPollWait        = 25 * time.Second
	FlushEvery          = 250 * time.Millisecond
	SweepEvery          = 5 * time.Second

	DefaultRetentionDays, MinRetentionDays, MaxRetentionDays = 30, 1, 365
)

// sentinelVantageName is the vantage_name of runs on the Sentinel server.
const sentinelVantageName = "Sentinel"

// resolveTimeout bounds the one lookup Create makes for a host name.
const resolveTimeout = 5 * time.Second

// AuditRecorder is the part of *services.AuditService the runs use.
type AuditRecorder interface {
	Record(ctx context.Context, actor services.Actor, action, resourceType string, resourceID *uuid.UUID, changes models.AuditChanges)
}

// Deps are the service's collaborators. Only DB and Settings are required.
type Deps struct {
	DB       *gorm.DB
	Settings *services.SettingsService
	Audit    AuditRecorder                                            // nil → nothing is audited
	Hub      *stream.Hub                                              // nil → a private hub
	Runner   *nettools.Runner                                         // nil → &nettools.Runner{}
	Resolve  func(ctx context.Context, host string) ([]net.IP, error) // nil → net.DefaultResolver.LookupIP(ctx, "ip", host)
	Now      func() time.Time                                         // nil → time.Now().UTC()
}

// Service runs and records network tool runs.
type Service struct {
	db       *gorm.DB
	settings *services.SettingsService
	audit    AuditRecorder
	hub      *stream.Hub
	resolve  func(ctx context.Context, host string) ([]net.IP, error)
	now      func() time.Time

	// runTool executes a normalized spec: the Runner's Run. Tests replace it
	// to script a tool's events without touching the network.
	runTool func(ctx context.Context, spec nettools.Spec, emit nettools.Emitter) (any, error)
	// launch starts a run on the Sentinel server once it is inserted. Tests
	// replace it to observe or suppress local execution.
	launch func(run *models.ToolRun, spec nettools.Spec)

	targets *targetLimiter
	polls   *pollTracker

	mu      sync.Mutex
	wakers  map[uuid.UUID]chan struct{}      // per agent: a run was queued for it
	cancels map[uuid.UUID]context.CancelFunc // per local run: stops its tool
}

// New builds the service.
func New(d Deps) *Service {
	s := &Service{
		db:       d.DB,
		settings: d.Settings,
		audit:    d.Audit,
		hub:      d.Hub,
		resolve:  d.Resolve,
		now:      d.Now,
		targets:  newTargetLimiter(),
		polls:    newPollTracker(),
		wakers:   make(map[uuid.UUID]chan struct{}),
		cancels:  make(map[uuid.UUID]context.CancelFunc),
	}
	if s.audit == nil {
		s.audit = noAudit{}
	}
	if s.hub == nil {
		s.hub = stream.NewHub(256)
	}
	if s.resolve == nil {
		s.resolve = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	runner := d.Runner
	if runner == nil {
		runner = &nettools.Runner{}
	}
	s.runTool = runner.Run
	// Replaced by the local launcher in Task 9.
	s.launch = func(*models.ToolRun, nettools.Spec) {}
	return s
}

type noAudit struct{}

func (noAudit) Record(context.Context, services.Actor, string, string, *uuid.UUID, models.AuditChanges) {
}

// Requester is who asks for a run, as the API authenticated them.
type Requester struct {
	UserID   uuid.UUID
	Username string
	IsAdmin  bool
	IP       string
}

func (r Requester) actor() services.Actor {
	return services.Actor{UserID: r.UserID, Username: r.Username, IP: r.IP}
}

// Vantage is where a run executes: the Sentinel server, or an agent by its
// readable id.
type Vantage struct {
	Kind    string `json:"kind"`               // models.VantageSentinel | models.VantageAgent
	AgentID string `json:"agent_id,omitempty"` // readable agent id
}

// CreateRequest is the body of POST /tools/runs.
type CreateRequest struct {
	Tool    nettools.Tool   `json:"tool"`
	Vantage Vantage         `json:"vantage"`
	Target  string          `json:"target"`
	Params  nettools.Params `json:"params"`
}

// Refusal is a failure the user sees; the API writes it with
// respondToolError.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

func (r *Refusal) Error() string { return r.Message }

// Refusal codes.
const (
	CodeForbidden        = "forbidden"
	CodeInvalidParams    = "invalid_params"
	CodeTargetNotAllowed = "target_not_allowed"
	CodeResolveFailed    = "resolve_failed"
	CodeIPv6Unsupported  = "ipv6_unsupported"
	CodeVantageNotReady  = "vantage_not_ready"
	CodeLimit            = "limit"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
)

func refuse(status int, code, format string, args ...any) *Refusal {
	return &Refusal{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// wakeChan is agentID's wake signal: a channel of one, filled when a run is
// queued for the agent. A missed signal costs at most one long-poll cycle,
// because the table is the queue.
func (s *Service) wakeChan(agentID uuid.UUID) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.wakers[agentID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		s.wakers[agentID] = ch
	}
	return ch
}

// wake signals agentID's waiting poll, if any, without blocking.
func (s *Service) wake(agentID uuid.UUID) {
	select {
	case s.wakeChan(agentID) <- struct{}{}:
	default:
	}
}
```

- [ ] **Step 6: Write settings, polls and limits**

Create `backend/internal/toolruns/settings.go`:

```go
package toolruns

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Settings are the admin's network-tools settings.
type Settings struct {
	Allowlist     []string `json:"allowlist"`
	ServerEnabled bool     `json:"server_enabled"`
	RetentionDays int      `json:"retention_days"`
}

// SettingsError is a save refused for bad input. Entries lists each bad
// allowlist entry; it is empty when the retention was the problem.
type SettingsError struct {
	Message string
	Entries []nettools.EntryError
}

func (e *SettingsError) Error() string { return e.Message }

// LoadSettings reads the settings, with the defaults (an empty allowlist,
// runs from the server allowed, 30 days) for anything unset. A stored
// allowlist that is not a JSON array of strings reads as empty, and a
// retention outside 1-365 as the default, so a bad row can neither open the
// allowlist nor make the prune delete everything.
func LoadSettings(ctx context.Context, ss *services.SettingsService) Settings {
	list := []string{}
	raw := ss.GetString(ctx, models.SettingNetToolsAllowlist, "[]")
	if err := json.Unmarshal([]byte(raw), &list); err != nil || list == nil {
		if err != nil {
			log.Printf("[tools] stored %s is not a JSON array of strings; treating it as empty: %v",
				models.SettingNetToolsAllowlist, err)
		}
		list = []string{}
	}
	days := ss.GetInt(ctx, models.SettingNetToolsRetentionDays, DefaultRetentionDays)
	if days < MinRetentionDays || days > MaxRetentionDays {
		days = DefaultRetentionDays
	}
	return Settings{
		Allowlist:     list,
		ServerEnabled: ss.GetBool(ctx, models.SettingNetToolsServerEnabled, true),
		RetentionDays: days,
	}
}

// SaveSettings validates and stores s and returns what was stored. Entries
// are trimmed and lower-cased, blank ones dropped and repeats removed. Any
// bad entry refuses the whole save with a *SettingsError listing every bad
// entry, and nothing is written; so does a retention outside 1-365.
func SaveSettings(ctx context.Context, ss *services.SettingsService, s Settings) (Settings, error) {
	if s.RetentionDays < MinRetentionDays || s.RetentionDays > MaxRetentionDays {
		return Settings{}, &SettingsError{
			Message: fmt.Sprintf("retention_days must be between %d and %d", MinRetentionDays, MaxRetentionDays),
		}
	}
	clean := cleanAllowlist(s.Allowlist)
	if _, bad := nettools.ParseAllowlist(clean); len(bad) > 0 {
		return Settings{}, &SettingsError{Message: "some allowlist entries are not valid", Entries: bad}
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return Settings{}, fmt.Errorf("encoding the allowlist: %w", err)
	}
	if err := ss.SetString(ctx, models.SettingNetToolsAllowlist, string(encoded)); err != nil {
		return Settings{}, err
	}
	if err := ss.SetBool(ctx, models.SettingNetToolsServerEnabled, s.ServerEnabled); err != nil {
		return Settings{}, err
	}
	if err := ss.SetInt(ctx, models.SettingNetToolsRetentionDays, s.RetentionDays); err != nil {
		return Settings{}, err
	}
	return LoadSettings(ctx, ss), nil
}

// cleanAllowlist trims and lower-cases entries, drops blank ones and keeps
// the first of any repeat, in order. Never nil.
func cleanAllowlist(entries []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, e := range entries {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}
```

Create `backend/internal/toolruns/polls.go`:

```go
package toolruns

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// pollTracker remembers when each agent last asked for a job. In memory on
// purpose: after a restart every agent polls again within one long-poll
// cycle.
type pollTracker struct {
	mu   sync.Mutex
	last map[uuid.UUID]time.Time
}

func newPollTracker() *pollTracker {
	return &pollTracker{last: make(map[uuid.UUID]time.Time)}
}

// seen records a poll by agentID at now.
func (p *pollTracker) seen(agentID uuid.UUID, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last[agentID] = now
}

// lastSeen returns agentID's last poll, and false when it has not polled
// since Sentinel started.
func (p *pollTracker) lastSeen(agentID uuid.UUID) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.last[agentID]
	return t, ok
}
```

Create `backend/internal/toolruns/limits.go`. A nil `targetIP` or `agentID` binds as NULL, so `count(*) FILTER (WHERE target_ip = NULL)` counts nothing. A DNS lookup through the system resolver therefore has no per-target cap, and a Sentinel run has no per-agent cap.

```go
package toolruns

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// targetIdleTTL is how long an unused per-target bucket is kept.
const targetIdleTTL = 15 * time.Minute

// targetLimiter is the per-target-address rate limit: TargetRatePerMinute
// runs a minute against one address, across all users, with a burst of the
// same size. Checked in the service after the target is resolved, since
// middleware cannot know the address.
type targetLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*targetBucket
	lastSweep time.Time
}

type targetBucket struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

func newTargetLimiter() *targetLimiter {
	return &targetLimiter{buckets: make(map[string]*targetBucket)}
}

// allow takes one token from ip's bucket at now. Idle buckets are swept
// while holding the lock, at most once per targetIdleTTL, so the map stays
// bounded without a goroutine of its own.
func (l *targetLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) > targetIdleTTL {
		for k, b := range l.buckets {
			if now.Sub(b.lastSeen) > targetIdleTTL {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b := l.buckets[ip]
	if b == nil {
		b = &targetBucket{lim: rate.NewLimiter(rate.Every(time.Minute/TargetRatePerMinute), TargetRatePerMinute)}
		l.buckets[ip] = b
	}
	b.lastSeen = now
	return b.lim.AllowN(now, 1)
}

// activeCounts are the runs in progress (queued or running) that the caps
// compare against.
type activeCounts struct {
	Overall  int64 `gorm:"column:overall"`
	ByUser   int64 `gorm:"column:by_user"`
	ByTarget int64 `gorm:"column:by_target"`
	ByAgent  int64 `gorm:"column:by_agent"`
}

// checkCaps counts the active runs inside the caller's transaction, which
// holds the caps advisory lock, and returns a 429 Refusal for the first cap
// the new run would pass (per user, per target, overall, per agent), or nil.
// A nil targetIP or agentID counts nothing for that cap.
func checkCaps(tx *gorm.DB, userID uuid.UUID, targetIP *string, agentID *uuid.UUID, agentName string) (*Refusal, error) {
	var c activeCounts
	if err := tx.Raw(`SELECT count(*) AS overall,
			count(*) FILTER (WHERE user_id = ?) AS by_user,
			count(*) FILTER (WHERE target_ip = ?) AS by_target,
			count(*) FILTER (WHERE agent_id = ?) AS by_agent
		FROM tool_runs WHERE status IN (?, ?)`,
		userID, targetIP, agentID, models.ToolRunQueued, models.ToolRunRunning).Scan(&c).Error; err != nil {
		return nil, fmt.Errorf("counting active tool runs: %w", err)
	}
	limit := func(msg string) *Refusal {
		return &Refusal{Status: http.StatusTooManyRequests, Code: CodeLimit, Message: msg}
	}
	switch {
	case c.ByUser >= MaxActivePerUser:
		return limit(fmt.Sprintf("You already have %d runs in progress", MaxActivePerUser)), nil
	case targetIP != nil && c.ByTarget >= MaxActivePerTarget:
		return limit(fmt.Sprintf("%d runs are already running against %s", MaxActivePerTarget, *targetIP)), nil
	case c.Overall >= MaxActiveOverall:
		return limit(fmt.Sprintf("Sentinel is already running %d tool runs", MaxActiveOverall)), nil
	case agentID != nil && c.ByAgent >= MaxActivePerAgent:
		return limit(fmt.Sprintf("%s is already running %d tool runs", agentName, MaxActivePerAgent)), nil
	}
	return nil, nil
}
```

- [ ] **Step 7: Write Create**

Create `backend/internal/toolruns/create.go`:

```go
package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

// Create validates the request, applies the guardrails in order (parameters,
// vantage, target resolution, allowlist, per-target rate, active-run caps),
// inserts the run and starts it on the server or queues it for the agent.
// User-facing failures are *Refusal.
func (s *Service) Create(ctx context.Context, who Requester, req CreateRequest) (*models.ToolRun, error) {
	spec, err := nettools.Normalize(nettools.Spec{Tool: req.Tool, Target: req.Target, Params: req.Params})
	if err != nil {
		return nil, refuse(http.StatusUnprocessableEntity, CodeInvalidParams, "%s", err.Error())
	}
	settings := LoadSettings(ctx, s.settings)
	now := s.now()

	// The vantage.
	var agent *models.Agent
	vantageName := sentinelVantageName
	switch req.Vantage.Kind {
	case models.VantageSentinel:
		if !settings.ServerEnabled {
			return nil, refuse(http.StatusConflict, CodeVantageNotReady, "%s", reasonText(ReasonServerDisabled, ""))
		}
	case models.VantageAgent:
		var a models.Agent
		err := s.db.WithContext(ctx).First(&a, "agent_id = ?", req.Vantage.AgentID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, refuse(http.StatusNotFound, CodeNotFound, "no such agent")
		}
		if err != nil {
			return nil, fmt.Errorf("loading agent %s: %w", req.Vantage.AgentID, err)
		}
		if reason := s.agentReason(&a, now); reason != "" {
			return nil, refuse(http.StatusConflict, CodeVantageNotReady, "%s", reasonText(reason, a.Name))
		}
		agent, vantageName = &a, a.Name
	default:
		return nil, refuse(http.StatusUnprocessableEntity, CodeInvalidParams, "vantage: kind must be sentinel or agent")
	}

	// The address the tool will contact, checked against the allowlist. A
	// DNS lookup through the vantage's own resolver contacts no target.
	host := spec.Target
	if spec.Tool == nettools.ToolDNS {
		host = dnsServerHost(spec.Params.Server)
	}
	var targetIP *string
	if host != "" {
		ip, refusal := s.resolveTarget(ctx, host)
		if refusal != nil {
			return nil, refusal
		}
		allow, _ := nettools.ParseAllowlist(settings.Allowlist)
		if allow.Empty() || !allow.Allows(host, ip) {
			r := refuse(http.StatusUnprocessableEntity, CodeTargetNotAllowed,
				"%s is not on the network tools allowlist", describeTarget(host, ip))
			s.audit.Record(ctx, who.actor(), models.ActionToolRunRefused, models.ResourceToolRun, nil,
				models.AuditChanges{Summary: map[string]any{
					"tool": string(spec.Tool), "target": spec.Target, "target_ip": ip.String(),
					"vantage": vantageAudit(req.Vantage.Kind, agent), "code": r.Code, "message": r.Message,
				}})
			return nil, r
		}
		addr := ip.String()
		targetIP = &addr
		spec.TargetIP = addr
		if !s.targets.allow(addr, now) {
			return nil, refuse(http.StatusTooManyRequests, CodeLimit, "too many runs against %s; wait a minute", addr)
		}
	}

	params, err := json.Marshal(spec.Params)
	if err != nil {
		return nil, fmt.Errorf("encoding parameters: %w", err)
	}
	userID := who.UserID
	run := &models.ToolRun{
		ID:          uuid.New(),
		Tool:        string(spec.Tool),
		UserID:      &userID,
		Username:    who.Username,
		VantageKind: req.Vantage.Kind,
		VantageName: vantageName,
		Target:      spec.Target,
		TargetIP:    targetIP,
		Params:      models.RawJSON(params),
		CreatedAt:   now,
	}
	if agent != nil {
		// Queued until the agent claims it; the claim resets the deadline to
		// claim time + the tool's limit. Until then it is far enough out that
		// the pickup timeout, not the deadline, ends an unclaimed run.
		run.Status = models.ToolRunQueued
		run.AgentUUID, run.AgentRef = &agent.ID, &agent.AgentID
		run.Deadline = now.Add(PickupTimeout + nettools.Deadline(spec.Tool))
	} else {
		run.Status = models.ToolRunRunning
		run.StartedAt = &now
		run.Deadline = now.Add(nettools.Deadline(spec.Tool))
	}

	var capRefusal *Refusal
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialises every create's cap check and insert, so concurrent
		// creates cannot overshoot a cap. Released at commit.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('tool_runs_caps'))`).Error; err != nil {
			return fmt.Errorf("taking the tool runs lock: %w", err)
		}
		var agentID *uuid.UUID
		agentName := ""
		if agent != nil {
			agentID, agentName = &agent.ID, agent.Name
		}
		r, err := checkCaps(tx, who.UserID, targetIP, agentID, agentName)
		if err != nil {
			return err
		}
		if r != nil {
			capRefusal = r
			return r
		}
		return tx.Create(run).Error
	})
	if capRefusal != nil {
		return nil, capRefusal
	}
	if err != nil {
		return nil, fmt.Errorf("creating tool run: %w", err)
	}

	s.audit.Record(ctx, who.actor(), models.ActionToolRunStarted, models.ResourceToolRun, &run.ID,
		models.AuditChanges{Summary: map[string]any{
			"tool": run.Tool, "target": run.Target, "target_ip": targetIP,
			"vantage": vantageAudit(run.VantageKind, agent), "params": json.RawMessage(params),
		}})

	if agent != nil {
		s.wake(agent.ID)
	} else {
		s.launch(run, spec)
	}
	return run, nil
}

// dnsServerHost is the address part of a DNS lookup's server parameter
// ("a.b.c.d" or "a.b.c.d:port"), or "" for the system resolver.
func dnsServerHost(server string) string {
	if server == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(server); err == nil {
		return host
	}
	return server
}

// resolveTarget turns the typed target into the one IPv4 address the run
// will contact: a literal address as is, a name through Resolve (first IPv4
// answer). Resolved once, here: what was checked is what gets probed.
func (s *Service) resolveTarget(ctx context.Context, host string) (net.IP, *Refusal) {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
		return nil, refuse(http.StatusUnprocessableEntity, CodeIPv6Unsupported,
			"%s is an IPv6 address; S1 tools are IPv4 only", host)
	}
	rctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	ips, err := s.resolve(rctx, host)
	if err != nil || len(ips) == 0 {
		return nil, refuse(http.StatusUnprocessableEntity, CodeResolveFailed,
			"%s doesn't resolve from Sentinel — use its IP address", host)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
	}
	return nil, refuse(http.StatusUnprocessableEntity, CodeIPv6Unsupported,
		"%s only has IPv6 addresses; S1 tools are IPv4 only", host)
}

// describeTarget is "name (address)", or just the address when that is
// what was typed.
func describeTarget(host string, ip net.IP) string {
	if host == ip.String() {
		return host
	}
	return host + " (" + ip.String() + ")"
}

// vantageAudit describes the vantage for an audit entry.
func vantageAudit(kind string, agent *models.Agent) map[string]any {
	if agent == nil {
		return map[string]any{"kind": kind}
	}
	return map[string]any{"kind": kind, "agent_id": agent.AgentID, "agent_uuid": agent.ID, "name": agent.Name}
}
```

- [ ] **Step 8: Write the vantage list**

Create `backend/internal/toolruns/vantages.go`:

```go
package toolruns

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

// VantageView is one entry of GET /tools/vantages.
type VantageView struct {
	Kind    string `json:"kind"`
	AgentID string `json:"agent_id,omitempty"`
	Name    string `json:"name"`
	Ready   bool   `json:"ready"`
	Reason  string `json:"reason,omitempty"`
}

// Why a vantage is not ready.
const (
	ReasonServerDisabled     = "server_disabled"
	ReasonAgentTooOld        = "agent_too_old"
	ReasonToolsOff           = "tools_off"
	ReasonNotEnabledOnServer = "not_enabled_on_server"
	ReasonOffline            = "offline"
)

// reasonText is the refusal message for a vantage that is not ready.
func reasonText(reason, name string) string {
	switch reason {
	case ReasonServerDisabled:
		return "runs from the Sentinel server are switched off"
	case ReasonAgentTooOld:
		return "This agent's version can't run tools — update it"
	case ReasonToolsOff:
		return fmt.Sprintf("network tools are switched off for %s in Sentinel", name)
	case ReasonNotEnabledOnServer:
		return fmt.Sprintf("%s doesn't have ENABLE_TOOLS=true in its config", name)
	case ReasonOffline:
		return fmt.Sprintf("%s hasn't asked for jobs in the last minute (offline?)", name)
	}
	return reason
}

// agentReason is why a cannot run tools now, or "" when it can. Order:
// too old (never reported tools_local), switched off in Sentinel, not
// enabled on the agent's host, no poll within ReadyWindow.
func (s *Service) agentReason(a *models.Agent, now time.Time) string {
	switch {
	case a.ToolsLocal == nil:
		return ReasonAgentTooOld
	case !a.ToolsEnabled:
		return ReasonToolsOff
	case !*a.ToolsLocal:
		return ReasonNotEnabledOnServer
	}
	if last, ok := s.polls.lastSeen(a.ID); !ok || now.Sub(last) > ReadyWindow {
		return ReasonOffline
	}
	return ""
}

// Vantages lists the Sentinel server first, then every agent by name, each
// with whether it can run tools now and, when not, why.
func (s *Service) Vantages(ctx context.Context) ([]VantageView, error) {
	settings := LoadSettings(ctx, s.settings)
	out := []VantageView{{Kind: models.VantageSentinel, Name: sentinelVantageName, Ready: settings.ServerEnabled}}
	if !settings.ServerEnabled {
		out[0].Reason = ReasonServerDisabled
	}
	var agents []models.Agent
	if err := s.db.WithContext(ctx).Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	sort.Slice(agents, func(i, j int) bool {
		a, b := strings.ToLower(agents[i].Name), strings.ToLower(agents[j].Name)
		if a != b {
			return a < b
		}
		return agents[i].AgentID < agents[j].AgentID
	})
	now := s.now()
	for i := range agents {
		reason := s.agentReason(&agents[i], now)
		out = append(out, VantageView{
			Kind: models.VantageAgent, AgentID: agents[i].AgentID, Name: agents[i].Name,
			Ready: reason == "", Reason: reason,
		})
	}
	return out, nil
}

// AllowlistEmpty reports whether no target is allowed yet, for the Tools
// page's banner (grant holders cannot read the admin settings).
func (s *Service) AllowlistEmpty(ctx context.Context) bool {
	allow, _ := nettools.ParseAllowlist(LoadSettings(ctx, s.settings).Allowlist)
	return allow.Empty()
}
```

- [ ] **Step 9: Run the tests**

Run: `go test ./internal/toolruns/ -run 'TestTargetLimiter|TestPollTracker' -v`
Expected: PASS for `TestTargetLimiter`, `TestTargetLimiterSweepsIdleBuckets` and `TestPollTracker`.

Run: `./scripts/test-db.sh -run 'TestDBSettings|TestDBCreate(SentinelRun|AgentRunQueues|Refusals|ServerOff|DNS|Caps|TargetRate)|TestDBVantages' -v`
Expected: PASS for the following:
- `TestDBSettingsDefaultsAndRoundTrip`, `TestDBSettingsRefusesBadEntries` and `TestDBSettingsRetentionBounds`
- `TestDBCreateSentinelRun`, `TestDBCreateAgentRunQueues`, `TestDBCreateRefusals`, `TestDBCreateServerOffAndEmptyAllowlist`, `TestDBCreateDNS`, `TestDBCreateCaps`, `TestDBCreateCapsUnderConcurrency` and `TestDBCreateTargetRate`
- `TestDBVantages`

Then the concurrency test under the race detector: `./scripts/test-db.sh -race -run 'TestDBCreateCapsUnderConcurrency' -v`
Expected: PASS, no race reports.

- [ ] **Step 10: Vet and commit**

Run: `go vet ./internal/toolruns/`
Expected: no output.

```bash
git add backend/internal/toolruns/
git commit -m "feat(tools): tool run creation with guardrails, settings and vantages

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Recording, local runs, queries and cancel

This task adds the recorder and the Sentinel-server launcher.

`insertEvents` is the one batch-insert path. Local runs use it here and agent posts use it in Task 10. One statement inserts the batch with `ON CONFLICT (run_id, seq) DO NOTHING` and adds the number actually inserted to `event_count`. The inserted events are then published as `event` frames in seq order.

`finish` is the one way a run ends. Its UPDATE only matches queued or running runs, so the first finish wins and cancel wins (ruling 8). A later finish only fills a NULL summary. On the transition, `finish` stops a local tool, publishes `end` and audits `tool_run_finished`.

The local launcher runs the tool in a goroutine with the run's deadline on its context and flushes every 250 ms. It maps the tool's ending to a final status:
- the event cap → `failed`, "too much output"
- no error → `done`
- `DeadlineExceeded` → `timed_out`
- `Canceled` → `cancelled`
- any other error → `failed` with the error text

`New` now installs this launcher.

The audit's one-line result (`resultLine`) uses these forms: `"<loss>% loss, <avg> ms avg"` (or `"<loss>% loss"` with no replies), `"reached in N hops"` / `"not reached (N hops)"`, `"<RCODE>, N answer(s)"`, `"N open of M"`, and the error text or status for runs that did not finish done.

**Files:**
- Create: `backend/internal/toolruns/recorder.go`
- Create: `backend/internal/toolruns/local.go`
- Create: `backend/internal/toolruns/query.go`
- Modify: `backend/internal/toolruns/service.go` (`New`: the launcher)
- Test: `backend/internal/toolruns/recorder_test.go` (unit)
- Test: `backend/internal/toolruns/local_db_test.go` (DB)
- Test: `backend/internal/toolruns/query_db_test.go` (DB)

**Interfaces:**
- Consumes:
  - Task 8: the `Service` fields and helpers listed in its Produces, and the fixtures.
  - Task 7: `stream.Message`, `stream.Subscription` and `(*Hub).Publish/Subscribe`.
  - `nettools.Runner{Dial: …}.Run`, `nettools.Event`, `nettools.ErrICMPUnavailable`, the `EventReply`/`EventPort`/`EventStart` constants and `PingReply`, `PingSummary`, `TraceSummary`, `DNSSummary`, `TCPSummary`.
- Produces:
  - Shared contract:
    - `var ErrRunNotFound`
    - `func (s *Service) Get(ctx, id uuid.UUID) (*models.ToolRun, error)`
    - `func (s *Service) Events(ctx, id uuid.UUID, afterSeq int) ([]models.ToolRunEvent, error)`: seq ascending, never nil
    - `ListFilter`
    - `func (s *Service) List(ctx, f ListFilter) ([]models.ToolRun, int64, error)`: newest first (`created_at DESC, id DESC`); limit defaults to 50 and is capped at 200; a negative offset counts as 0
    - `func (s *Service) Cancel(ctx, who Requester, id uuid.UUID) (*models.ToolRun, error)`, with these refusals: 404 `not_found` "no such tool run"; 403 `forbidden` "only the user who started a run, or an admin, can cancel it"; 409 `conflict` "the run has already finished"
  - Contract change 1: `func (s *Service) Subscribe(runID uuid.UUID) *stream.Subscription`.
  - Hub frames per the contract:
    - `event`: `ID` = seq, `Data` = `models.ToolRunEvent` JSON
    - `status`: `{"status":"running"}`
    - `end`: `Data` = the final `models.ToolRun` JSON
  - For Task 10 (unexported):
    - `func (s *Service) insertEvents(ctx context.Context, runID uuid.UUID, events []models.ToolRunEvent) (int, error)`
    - `func (s *Service) finish(ctx context.Context, runID uuid.UUID, status string, summary []byte, errText string, actor services.Actor) (bool, error)`
    - `publishStatus(runID, status)`, `cancelLocal(runID)`, `ownerActor(run)`, `systemActor` (`services.Actor{Username: "system"}`, ruling 14), `msgTooMuchOutput`, `msgNotPickedUp`, `msgTimedOut` and `msgInterrupted`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/toolruns/recorder_test.go`:

```go
package toolruns

import (
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestResultLine(t *testing.T) {
	sum := func(s string) *models.RawJSON { r := models.RawJSON(s); return &r }
	text := func(s string) *string { return &s }
	cases := []struct {
		run  models.ToolRun
		want string
	}{
		{models.ToolRun{Tool: "ping", Status: "done", Summary: sum(`{"sent":5,"received":5,"loss_pct":0,"avg_ms":2.14}`)}, "0% loss, 2.1 ms avg"},
		{models.ToolRun{Tool: "ping", Status: "done", Summary: sum(`{"sent":3,"received":2,"loss_pct":33.3,"avg_ms":1.5}`)}, "33.3% loss, 1.5 ms avg"},
		{models.ToolRun{Tool: "ping", Status: "done", Summary: sum(`{"sent":5,"received":0,"loss_pct":100,"avg_ms":null}`)}, "100% loss"},
		{models.ToolRun{Tool: "traceroute", Status: "done", Summary: sum(`{"reached":true,"hop_count":9}`)}, "reached in 9 hops"},
		{models.ToolRun{Tool: "traceroute", Status: "done", Summary: sum(`{"reached":false,"hop_count":4}`)}, "not reached (4 hops)"},
		{models.ToolRun{Tool: "dns", Status: "done", Summary: sum(`{"rcode":"NOERROR","answer_count":2}`)}, "NOERROR, 2 answers"},
		{models.ToolRun{Tool: "dns", Status: "done", Summary: sum(`{"rcode":"NOERROR","answer_count":1}`)}, "NOERROR, 1 answer"},
		{models.ToolRun{Tool: "tcp", Status: "done", Summary: sum(`{"total":100,"open":3}`)}, "3 open of 100"},
		{models.ToolRun{Tool: "ping", Status: "failed", Error: text("ICMP isn't available here (needs root or NET_RAW)")}, "ICMP isn't available here (needs root or NET_RAW)"},
		{models.ToolRun{Tool: "ping", Status: "cancelled"}, "cancelled"},
		{models.ToolRun{Tool: "ping", Status: "done"}, "done"},
	}
	for _, c := range cases {
		if got := resultLine(&c.run); got != c.want {
			t.Errorf("resultLine(%s %s) = %q, want %q", c.run.Tool, c.run.Status, got, c.want)
		}
	}
}
```

Create `backend/internal/toolruns/local_db_test.go`. `scriptedTool` replaces `Service.runTool`, so no real ICMP is needed. `TestDBLocalRunThroughRunner` runs the real `nettools.Runner` with a fake `Dial`, per the contract: a `start` event first, one `port` event per port, a refused dial counts as closed and the result is a `TCPSummary`.

```go
package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// scriptedTool stands in for the Runner: it reports the spec it was given,
// waits for release, emits its events and returns its summary and error.
// With blockUntilCancel it then waits for its context instead.
type scriptedTool struct {
	events           []nettools.Event
	summary          any
	err              error
	blockUntilCancel bool
	started          chan nettools.Spec
	release          chan struct{}
}

func newScriptedTool(events ...nettools.Event) *scriptedTool {
	return &scriptedTool{events: events, started: make(chan nettools.Spec, 1), release: make(chan struct{})}
}

func (f *scriptedTool) run(ctx context.Context, spec nettools.Spec, emit nettools.Emitter) (any, error) {
	f.started <- spec
	select {
	case <-f.release:
	case <-ctx.Done():
		return f.summary, ctx.Err()
	}
	for _, ev := range f.events {
		emit(ev)
	}
	if f.blockUntilCancel {
		<-ctx.Done()
		return f.summary, ctx.Err()
	}
	return f.summary, f.err
}

// localEnv is newEnv with the real local launcher and tool.
func localEnv(t *testing.T, tool *scriptedTool) *env {
	t.Helper()
	e := newEnv(t)
	e.svc.launch = e.svc.launchLocal
	e.svc.runTool = tool.run
	return e
}

// waitFor polls cond every 20 ms for up to 5 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// framesUntilEnd reads a subscription up to and including the "end" frame,
// skipping "status" frames.
func framesUntilEnd(t *testing.T, sub *stream.Subscription) []stream.Message {
	t.Helper()
	var out []stream.Message
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m, ok := <-sub.C():
			if !ok {
				t.Fatalf("subscription closed before end (dropped %v)", sub.Dropped())
			}
			if m.Event == "status" {
				continue
			}
			out = append(out, m)
			if m.Event == "end" {
				return out
			}
		case <-timeout:
			t.Fatalf("no end frame; got %d frames", len(out))
		}
	}
}

func f64(v float64) *float64 { return &v }

func TestDBLocalRunRecordsAndStreams(t *testing.T) {
	tool := newScriptedTool(
		nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: 1, RTTMS: 1.25, TTL: 64, From: "10.0.0.5"}},
		nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: 2, RTTMS: 1.75, TTL: 64, From: "10.0.0.5"}},
		nettools.Event{Type: "timeout", Data: map[string]int{"seq": 3}},
	)
	tool.summary = nettools.PingSummary{Sent: 3, Received: 2, LossPct: 33.3, MinMS: f64(1.25), AvgMS: f64(1.5), MaxMS: f64(1.75), JitterMS: f64(0.5)}
	e := localEnv(t, tool)
	ctx := context.Background()
	who := e.user(t, false)

	run, err := e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	testdb.Must(t, err)
	if spec := <-tool.started; spec.TargetIP != "10.0.0.5" || spec.Params.Count != 5 {
		t.Errorf("the tool got %+v", spec)
	}
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()
	close(tool.release)

	frames := framesUntilEnd(t, sub)
	if len(frames) != 4 {
		t.Fatalf("frames = %+v, want three events and end", frames)
	}
	for i, wantType := range []string{"reply", "reply", "timeout"} {
		var ev models.ToolRunEvent
		testdb.Must(t, json.Unmarshal(frames[i].Data, &ev))
		if frames[i].Event != "event" || frames[i].ID != strconv.Itoa(i+1) || ev.Seq != i+1 || ev.Type != wantType {
			t.Errorf("frame %d = %s id %s %+v, want event %d of type %s", i, frames[i].Event, frames[i].ID, ev, i+1, wantType)
		}
	}
	var end models.ToolRun
	testdb.Must(t, json.Unmarshal(frames[3].Data, &end))
	if end.ID != run.ID || end.Status != models.ToolRunDone || end.EventCount != 3 || end.Summary == nil {
		t.Errorf("end frame = %+v, want the run done with 3 events and a summary", end)
	}

	got := e.reload(t, run.ID)
	var sum nettools.PingSummary
	if got.Summary != nil {
		testdb.Must(t, json.Unmarshal(*got.Summary, &sum))
	}
	if got.Status != models.ToolRunDone || got.EventCount != 3 || got.FinishedAt == nil || got.Error != nil ||
		sum.Sent != 3 || sum.AvgMS == nil || *sum.AvgMS != 1.5 {
		t.Errorf("run = %+v (summary %+v), want done, 3 events, finished, the tool's summary", got, sum)
	}
	events, err := e.svc.Events(ctx, run.ID, 0)
	testdb.Must(t, err)
	var first nettools.PingReply
	if len(events) == 3 {
		testdb.Must(t, json.Unmarshal(events[0].Data, &first))
	}
	if len(events) != 3 || events[0].Seq != 1 || events[2].Seq != 3 || events[2].Type != "timeout" ||
		first != (nettools.PingReply{Seq: 1, RTTMS: 1.25, TTL: 64, From: "10.0.0.5"}) {
		t.Errorf("stored events = %+v (first %+v)", events, first)
	}
	finished := e.audit.byAction(models.ActionToolRunFinished)
	if len(finished) != 1 || finished[0].actor.UserID != who.UserID ||
		finished[0].changes.Summary["status"] != models.ToolRunDone || finished[0].changes.Summary["result"] != "33.3% loss, 1.5 ms avg" {
		t.Errorf("finish audit = %+v", finished)
	}
	waitFor(t, "the cancel func to be forgotten", func() bool {
		e.svc.mu.Lock()
		defer e.svc.mu.Unlock()
		return len(e.svc.cancels) == 0
	})
}

// A tool error fails the run with the tool's message.
func TestDBLocalRunToolError(t *testing.T) {
	tool := newScriptedTool()
	tool.err = nettools.ErrICMPUnavailable
	e := localEnv(t, tool)
	run, err := e.svc.Create(context.Background(), e.user(t, false), pingReq("10.0.0.5"))
	testdb.Must(t, err)
	<-tool.started
	close(tool.release)
	waitFor(t, "the run to fail", func() bool { return e.reload(t, run.ID).Status == models.ToolRunFailed })
	if got := e.reload(t, run.ID); got.Error == nil || *got.Error != "ICMP isn't available here (needs root or NET_RAW)" {
		t.Errorf("error = %v", got.Error)
	}
}

// Cancel wins: the run is cancelled at once, its tool is stopped, and the
// tool's late finish only fills in the summary.
func TestDBCancelLocalRun(t *testing.T) {
	tool := newScriptedTool(nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: 1, RTTMS: 2, TTL: 64, From: "10.0.0.5"}})
	tool.blockUntilCancel = true
	tool.summary = nettools.PingSummary{Sent: 1, Received: 1}
	e := localEnv(t, tool)
	ctx := context.Background()
	who := e.user(t, false)

	run, err := e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	testdb.Must(t, err)
	<-tool.started
	close(tool.release)
	waitFor(t, "the first event", func() bool { return e.reload(t, run.ID).EventCount == 1 })

	got, err := e.svc.Cancel(ctx, who, run.ID)
	testdb.Must(t, err)
	if got.Status != models.ToolRunCancelled || got.FinishedAt == nil {
		t.Errorf("Cancel returned %+v, want cancelled and finished", got)
	}
	waitFor(t, "the tool's late finish", func() bool { return e.reload(t, run.ID).Summary != nil })
	got = e.reload(t, run.ID)
	if got.Status != models.ToolRunCancelled || got.Error != nil || string(*got.Summary) == "" {
		t.Errorf("after the late finish: %+v, want still cancelled with no error", got)
	}
	finished := e.audit.byAction(models.ActionToolRunFinished)
	if len(finished) != 1 || finished[0].changes.Summary["status"] != models.ToolRunCancelled ||
		finished[0].actor.UserID != who.UserID {
		t.Errorf("finish audit = %+v, want one entry, cancelled by the owner", finished)
	}
}

// Reaching MaxEventsPerRun stops the tool and fails the run.
func TestDBLocalRunEventCap(t *testing.T) {
	events := make([]nettools.Event, MaxEventsPerRun+10)
	for i := range events {
		events[i] = nettools.Event{Type: nettools.EventPort, Data: map[string]int{"port": i + 1}}
	}
	tool := newScriptedTool(events...)
	e := localEnv(t, tool)
	run, err := e.svc.Create(context.Background(), e.user(t, false), pingReq("10.0.0.5"))
	testdb.Must(t, err)
	<-tool.started
	close(tool.release)
	waitFor(t, "the run to end", func() bool { return !models.ToolRunActive(e.reload(t, run.ID).Status) })
	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunFailed || got.Error == nil || *got.Error != "too much output" || got.EventCount != MaxEventsPerRun {
		t.Errorf("run = status %s, error %v, %d events; want failed, too much output, 5000", got.Status, got.Error, got.EventCount)
	}
}

// The real Runner, with a fake dialer: a two-port scan from the Sentinel
// server is stored and finished like any other run.
func TestDBLocalRunThroughRunner(t *testing.T) {
	e := newEnv(t)
	e.svc.launch = e.svc.launchLocal
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == "10.0.0.5:22" {
			c, s := net.Pipe()
			go s.Close()
			return c, nil
		}
		return nil, syscall.ECONNREFUSED
	}
	e.svc.runTool = (&nettools.Runner{Dial: dial}).Run
	run, err := e.svc.Create(context.Background(), e.user(t, false), CreateRequest{
		Tool: nettools.ToolTCP, Vantage: Vantage{Kind: models.VantageSentinel}, Target: "10.0.0.5",
		Params: nettools.Params{Ports: "22,80"},
	})
	testdb.Must(t, err)
	waitFor(t, "the scan to finish", func() bool { return e.reload(t, run.ID).Status == models.ToolRunDone })
	got := e.reload(t, run.ID)
	var sum nettools.TCPSummary
	testdb.Must(t, json.Unmarshal(*got.Summary, &sum))
	events, err := e.svc.Events(context.Background(), run.ID, 0)
	testdb.Must(t, err)
	if got.EventCount != 3 || len(events) != 3 || events[0].Type != nettools.EventStart ||
		sum.Total != 2 || sum.Open != 1 || sum.Closed != 1 || fmt.Sprint(sum.OpenPorts) != "[22]" {
		t.Errorf("run %+v, events %+v, summary %+v; want start + 2 ports, 1 open (22), 1 closed", got, events, sum)
	}
}

func TestLocalOutcome(t *testing.T) {
	cases := []struct {
		full          bool
		err           error
		status, error string
	}{
		{true, context.Canceled, models.ToolRunFailed, "too much output"},
		{false, nil, models.ToolRunDone, ""},
		{false, fmt.Errorf("probe: %w", context.DeadlineExceeded), models.ToolRunTimedOut, "the run went past its time limit"},
		{false, context.Canceled, models.ToolRunCancelled, ""},
		{false, nettools.ErrICMPUnavailable, models.ToolRunFailed, "ICMP isn't available here (needs root or NET_RAW)"},
		{false, errors.New("boom"), models.ToolRunFailed, "boom"},
	}
	for _, c := range cases {
		status, text := localOutcome(c.full, c.err)
		if status != c.status || text != c.error {
			t.Errorf("localOutcome(%v, %v) = %s %q, want %s %q", c.full, c.err, status, text, c.status, c.error)
		}
	}
}
```

Create `backend/internal/toolruns/query_db_test.go`:

```go
package toolruns

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A retried batch never duplicates events: seqs already stored are skipped,
// event_count counts only what was inserted, and only new events are
// published.
func TestDBInsertEventsIgnoresDuplicates(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	run := e.insertRun(t, nil)
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()
	ev := func(seq int) models.ToolRunEvent {
		return models.ToolRunEvent{RunID: run.ID, Seq: seq, At: e.clock.Now(), Type: "reply", Data: models.RawJSON(`{"seq":1}`)}
	}

	n, err := e.svc.insertEvents(ctx, run.ID, []models.ToolRunEvent{ev(1), ev(2)})
	testdb.Must(t, err)
	if n != 2 {
		t.Errorf("first batch inserted %d, want 2", n)
	}
	n, err = e.svc.insertEvents(ctx, run.ID, []models.ToolRunEvent{ev(2), ev(3)})
	testdb.Must(t, err)
	if n != 1 {
		t.Errorf("overlapping batch inserted %d, want 1", n)
	}
	if got := e.reload(t, run.ID).EventCount; got != 3 {
		t.Errorf("event_count = %d, want 3", got)
	}
	var ids []string
	for len(ids) < 3 {
		select {
		case m := <-sub.C():
			ids = append(ids, m.ID)
		case <-time.After(time.Second):
			t.Fatalf("frames %v, want 3", ids)
		}
	}
	select {
	case m := <-sub.C():
		t.Errorf("an extra frame %s %s", m.Event, m.ID)
	default:
	}
	if ids[0] != "1" || ids[1] != "2" || ids[2] != "3" {
		t.Errorf("frames %v, want [1 2 3]", ids)
	}
}

func TestDBCancelRules(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, other, admin := e.user(t, false), e.user(t, false), e.user(t, true)
	run := e.insertRun(t, func(r *models.ToolRun) { r.UserID = &owner.UserID })

	_, err := e.svc.Cancel(ctx, other, run.ID)
	if r := refusal(t, err); r.Status != http.StatusForbidden || r.Code != CodeForbidden {
		t.Errorf("another user's cancel: %+v, want 403 forbidden", r)
	}
	if got := e.reload(t, run.ID); got.Status != models.ToolRunRunning {
		t.Errorf("a refused cancel changed the run to %s", got.Status)
	}
	got, err := e.svc.Cancel(ctx, admin, run.ID)
	testdb.Must(t, err)
	if got.Status != models.ToolRunCancelled {
		t.Errorf("admin cancel left %s", got.Status)
	}
	_, err = e.svc.Cancel(ctx, owner, run.ID)
	if r := refusal(t, err); r.Status != http.StatusConflict || r.Code != CodeConflict {
		t.Errorf("cancelling a finished run: %+v, want 409 conflict", r)
	}
	_, err = e.svc.Cancel(ctx, admin, uuid.New())
	if r := refusal(t, err); r.Status != http.StatusNotFound || r.Code != CodeNotFound {
		t.Errorf("unknown run: %+v, want 404 not_found", r)
	}
	finished := e.audit.byAction(models.ActionToolRunFinished)
	if len(finished) != 1 || finished[0].actor.UserID != admin.UserID {
		t.Errorf("finish audit = %+v, want one entry by the admin", finished)
	}
}

func TestDBGetEventsAndList(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, false), e.user(t, false)
	agent := e.newAgent(t, "file-server", true, boolPtr(true))
	base := e.clock.Now()
	at := func(min int) time.Time { return base.Add(time.Duration(min) * time.Minute) }

	r1 := e.insertRun(t, func(r *models.ToolRun) { r.UserID, r.CreatedAt, r.Status = &alice.UserID, at(1), models.ToolRunDone })
	r2 := e.insertRun(t, func(r *models.ToolRun) {
		r.UserID, r.CreatedAt, r.Tool, r.Target = &bob.UserID, at(2), "dns", "example.com"
		r.TargetIP = nil
	})
	r3 := e.insertRun(t, func(r *models.ToolRun) {
		r.UserID, r.CreatedAt, r.Status = &alice.UserID, at(3), models.ToolRunFailed
		r.VantageKind, r.AgentUUID, r.AgentRef, r.Target = models.VantageAgent, &agent.ID, &agent.AgentID, "fileserver.example.org"
	})

	if _, err := e.svc.Get(ctx, uuid.New()); !errors.Is(err, ErrRunNotFound) {
		t.Errorf("Get(unknown) = %v, want ErrRunNotFound", err)
	}
	if got, err := e.svc.Get(ctx, r2.ID); err != nil || got.Tool != "dns" {
		t.Errorf("Get = %+v, %v", got, err)
	}

	ids := func(runs []models.ToolRun) []uuid.UUID {
		out := []uuid.UUID{}
		for _, r := range runs {
			out = append(out, r.ID)
		}
		return out
	}
	cases := []struct {
		name  string
		f     ListFilter
		want  []uuid.UUID
		total int64
	}{
		{"all, newest first", ListFilter{}, []uuid.UUID{r3.ID, r2.ID, r1.ID}, 3},
		{"by tool", ListFilter{Tool: "dns"}, []uuid.UUID{r2.ID}, 1},
		{"by user", ListFilter{UserID: &alice.UserID}, []uuid.UUID{r3.ID, r1.ID}, 2},
		{"by agent", ListFilter{AgentID: agent.AgentID}, []uuid.UUID{r3.ID}, 1},
		{"by typed target", ListFilter{Target: "fileserver.example.org"}, []uuid.UUID{r3.ID}, 1},
		{"by address", ListFilter{Target: "10.0.0.200"}, []uuid.UUID{r3.ID, r1.ID}, 2},
		{"by status", ListFilter{Status: models.ToolRunRunning}, []uuid.UUID{r2.ID}, 1},
		{"paged", ListFilter{Limit: 1, Offset: 1}, []uuid.UUID{r2.ID}, 3},
		{"past the end", ListFilter{Offset: 10}, []uuid.UUID{}, 3},
	}
	for _, c := range cases {
		runs, total, err := e.svc.List(ctx, c.f)
		testdb.Must(t, err)
		got := ids(runs)
		if total != c.total || len(got) != len(c.want) {
			t.Errorf("%s: %v (total %d), want %v (total %d)", c.name, got, total, c.want, c.total)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: %v, want %v", c.name, got, c.want)
				break
			}
		}
	}

	for seq := 1; seq <= 4; seq++ {
		testdb.Exec(t, e.db, `INSERT INTO tool_run_events (run_id, seq, at, type, data) VALUES (?, ?, now(), 'reply', '{}')`, r1.ID, seq)
	}
	events, err := e.svc.Events(ctx, r1.ID, 2)
	testdb.Must(t, err)
	if len(events) != 2 || events[0].Seq != 3 || events[1].Seq != 4 {
		t.Errorf("Events after 2 = %+v, want seqs 3 and 4", events)
	}
	if none, err := e.svc.Events(ctx, r2.ID, 0); err != nil || none == nil || len(none) != 0 {
		t.Errorf("Events of a run without any = %v, %v; want an empty, non-nil slice", none, err)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go vet ./internal/toolruns/`
Expected: compile errors such as `undefined: resultLine`, `undefined: localOutcome`, `e.svc.launchLocal undefined`, `e.svc.Subscribe undefined` and `e.svc.Cancel undefined`.

- [ ] **Step 3: Write the recorder**

Create `backend/internal/toolruns/recorder.go`:

```go
package toolruns

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
)

// Messages for runs that end without the tool saying why.
const (
	msgTooMuchOutput = "too much output"
	msgNotPickedUp   = "the agent didn't pick up the job (offline?)"
	msgTimedOut      = "the run went past its time limit"
	msgInterrupted   = "Sentinel restarted while the run was in progress"
)

// systemActor audits the finishes nobody asked for: the sweeper's.
var systemActor = services.Actor{Username: "system"}

// ownerActor audits a run's own outcome as the user who started it.
func ownerActor(run *models.ToolRun) services.Actor {
	a := services.Actor{Username: run.Username}
	if run.UserID != nil {
		a.UserID = *run.UserID
	}
	return a
}

// insertBatchSize keeps one INSERT's parameters (5 per event) far below
// Postgres' 65535.
const insertBatchSize = 1000

// Subscribe returns a live feed of runID's frames ("event", "status",
// "end"). The SSE handler subscribes before replaying stored events, so
// nothing published in between is lost.
func (s *Service) Subscribe(runID uuid.UUID) *stream.Subscription {
	return s.hub.Subscribe(runID.String())
}

// insertEvents stores events for runID, ignoring any whose seq is already
// stored, adds the number actually inserted to the run's event_count, and
// publishes each inserted event as an "event" frame in seq order. Shared by
// local runs (the recorder) and agent posts.
func (s *Service) insertEvents(ctx context.Context, runID uuid.UUID, events []models.ToolRunEvent) (int, error) {
	inserted := 0
	for start := 0; start < len(events); start += insertBatchSize {
		batch := events[start:min(start+insertBatchSize, len(events))]
		bySeq := make(map[int]models.ToolRunEvent, len(batch))
		values := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*5+1)
		for _, ev := range batch {
			bySeq[ev.Seq] = ev
			values = append(values, "(?, ?, ?, ?, ?)")
			args = append(args, runID, ev.Seq, ev.At, ev.Type, ev.Data)
		}
		args = append(args, runID)
		// One statement: the insert and the count bump commit together, so
		// event_count always equals the stored events.
		var seqs []int
		err := s.db.WithContext(ctx).Raw(`WITH ins AS (
				INSERT INTO tool_run_events (run_id, seq, at, type, data)
				VALUES `+strings.Join(values, ", ")+`
				ON CONFLICT (run_id, seq) DO NOTHING
				RETURNING seq
			), bump AS (
				UPDATE tool_runs SET event_count = event_count + (SELECT count(*) FROM ins) WHERE id = ?
			)
			SELECT seq FROM ins ORDER BY seq`, args...).Scan(&seqs).Error
		if err != nil {
			return inserted, fmt.Errorf("storing tool run events: %w", err)
		}
		for _, seq := range seqs {
			s.publishEvent(bySeq[seq])
		}
		inserted += len(seqs)
	}
	return inserted, nil
}

// publishEvent sends one stored event to the run's live subscribers.
func (s *Service) publishEvent(ev models.ToolRunEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		log.Printf("[tools] encoding event %d of run %s: %v", ev.Seq, ev.RunID, err)
		return
	}
	s.hub.Publish(ev.RunID.String(), stream.Message{ID: strconv.Itoa(ev.Seq), Event: "event", Data: data})
}

// publishStatus tells subscribers a run's status changed (to running).
func (s *Service) publishStatus(runID uuid.UUID, status string) {
	data, _ := json.Marshal(map[string]string{"status": status})
	s.hub.Publish(runID.String(), stream.Message{Event: "status", Data: data})
}

// publishEnd sends the final run; streams close after it.
func (s *Service) publishEnd(run *models.ToolRun) {
	data, err := json.Marshal(run)
	if err != nil {
		log.Printf("[tools] encoding run %s: %v", run.ID, err)
		return
	}
	s.hub.Publish(run.ID.String(), stream.Message{Event: "end", Data: data})
}

// finish moves a queued or running run to a final status and reports
// whether this call did. The first finish wins (cancel wins, ruling 8):
// when the run is already final, a later finish only fills the summary if
// it is still empty and never changes the status, error or end frame. On
// the transition it stops a local run's tool, publishes "end" and audits
// tool_run_finished as actor. summary nil leaves it NULL; errText "" leaves
// error NULL.
func (s *Service) finish(ctx context.Context, runID uuid.UUID, status string, summary []byte, errText string, actor services.Actor) (bool, error) {
	var sum, errVal any
	if len(summary) > 0 && string(summary) != "null" {
		sum = string(summary)
	}
	if errText != "" {
		errVal = errText
	}
	var runs []models.ToolRun
	err := s.db.WithContext(ctx).Raw(`UPDATE tool_runs
		SET status = ?, finished_at = ?, summary = COALESCE(summary, ?::jsonb), error = COALESCE(error, ?::text)
		WHERE id = ? AND status IN (?, ?)
		RETURNING *`, status, s.now(), sum, errVal, runID, models.ToolRunQueued, models.ToolRunRunning).Scan(&runs).Error
	if err != nil {
		return false, fmt.Errorf("finishing tool run %s: %w", runID, err)
	}
	if len(runs) == 0 {
		if sum != nil {
			if err := s.db.WithContext(ctx).Exec(`UPDATE tool_runs SET summary = ?::jsonb WHERE id = ? AND summary IS NULL`,
				sum, runID).Error; err != nil {
				return false, fmt.Errorf("filling the summary of tool run %s: %w", runID, err)
			}
		}
		return false, nil
	}
	run := &runs[0]
	s.cancelLocal(runID)
	s.publishEnd(run)
	s.audit.Record(ctx, actor, models.ActionToolRunFinished, models.ResourceToolRun, &run.ID,
		models.AuditChanges{Summary: map[string]any{
			"tool": run.Tool, "target": run.Target, "status": run.Status,
			"duration_ms": runDuration(run).Milliseconds(), "result": resultLine(run),
		}})
	return true, nil
}

// cancelLocal stops runID's tool if it is running on this server.
func (s *Service) cancelLocal(runID uuid.UUID) {
	s.mu.Lock()
	cancel := s.cancels[runID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// runDuration is how long a finished run took, from its start (or its
// creation, if it never started).
func runDuration(run *models.ToolRun) time.Duration {
	if run.FinishedAt == nil {
		return 0
	}
	from := run.CreatedAt
	if run.StartedAt != nil {
		from = *run.StartedAt
	}
	return run.FinishedAt.Sub(from)
}

// resultLine is a finished run's one-line result for the audit log:
// "0% loss, 2.1 ms avg", "reached in 9 hops", "NOERROR, 2 answers",
// "3 open of 100"; for a run that did not finish done, its error or status.
func resultLine(run *models.ToolRun) string {
	if run.Status != models.ToolRunDone {
		if run.Error != nil && *run.Error != "" {
			return *run.Error
		}
		return run.Status
	}
	if run.Summary == nil {
		return run.Status
	}
	raw := []byte(*run.Summary)
	switch nettools.Tool(run.Tool) {
	case nettools.ToolPing:
		var p nettools.PingSummary
		if json.Unmarshal(raw, &p) == nil {
			loss := strconv.FormatFloat(p.LossPct, 'f', -1, 64) + "% loss"
			if p.AvgMS == nil {
				return loss
			}
			return fmt.Sprintf("%s, %.1f ms avg", loss, *p.AvgMS)
		}
	case nettools.ToolTraceroute:
		var tr nettools.TraceSummary
		if json.Unmarshal(raw, &tr) == nil {
			if tr.Reached {
				return fmt.Sprintf("reached in %d hops", tr.HopCount)
			}
			return fmt.Sprintf("not reached (%d hops)", tr.HopCount)
		}
	case nettools.ToolDNS:
		var d nettools.DNSSummary
		if json.Unmarshal(raw, &d) == nil {
			if d.AnswerCount == 1 {
				return d.RCode + ", 1 answer"
			}
			return fmt.Sprintf("%s, %d answers", d.RCode, d.AnswerCount)
		}
	case nettools.ToolTCP:
		var tc nettools.TCPSummary
		if json.Unmarshal(raw, &tc) == nil {
			return fmt.Sprintf("%d open of %d", tc.Open, tc.Total)
		}
	}
	return run.Status
}
```

- [ ] **Step 4: Write the local launcher**

Create `backend/internal/toolruns/local.go`:

```go
package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

// finalWriteTimeout bounds the last flush and the finish of a local run,
// which use a fresh context because the tool's may already be over.
const finalWriteTimeout = 10 * time.Second

// recorder buffers one local run's events, numbering them from 1, until
// the next flush. It stops accepting events at MaxEventsPerRun and stops
// the tool.
type recorder struct {
	s     *Service
	runID uuid.UUID
	stop  context.CancelFunc

	mu   sync.Mutex
	buf  []models.ToolRunEvent
	next int
	full bool
}

// emit is the tool's Emitter. Safe for concurrent use: tools emit from
// several goroutines.
func (r *recorder) emit(ev nettools.Event) {
	data, err := json.Marshal(ev.Data)
	if err != nil {
		data = []byte(`null`)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return
	}
	if r.next >= MaxEventsPerRun {
		r.full = true
		r.stop()
		return
	}
	r.next++
	r.buf = append(r.buf, models.ToolRunEvent{RunID: r.runID, Seq: r.next, At: r.s.now(), Type: ev.Type, Data: data})
}

// flush stores what is buffered.
func (r *recorder) flush(ctx context.Context) {
	r.mu.Lock()
	batch := r.buf
	r.buf = nil
	r.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	if _, err := r.s.insertEvents(ctx, r.runID, batch); err != nil {
		log.Printf("[tools] run %s: %v", r.runID, err)
	}
}

func (r *recorder) isFull() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.full
}

// launchLocal starts a Sentinel-server run in a goroutine, with the run's
// deadline on its context. Cancel and the sweeper stop it through
// s.cancels.
func (s *Service) launchLocal(run *models.ToolRun, spec nettools.Spec) {
	ctx, cancel := context.WithDeadline(context.Background(), run.Deadline)
	s.mu.Lock()
	s.cancels[run.ID] = cancel
	s.mu.Unlock()
	go s.runLocal(ctx, cancel, run, spec)
}

// runLocal runs the tool, flushing its events every FlushEvery, then
// records the outcome through finish, the same path agent runs take.
func (s *Service) runLocal(ctx context.Context, cancel context.CancelFunc, run *models.ToolRun, spec nettools.Spec) {
	defer func() {
		s.mu.Lock()
		delete(s.cancels, run.ID)
		s.mu.Unlock()
		cancel()
	}()
	s.publishStatus(run.ID, models.ToolRunRunning)
	rec := &recorder{s: s, runID: run.ID, stop: cancel}

	stopFlush, flushed := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(flushed)
		tick := time.NewTicker(FlushEvery)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				rec.flush(context.Background())
			case <-stopFlush:
				return
			}
		}
	}()
	summary, err := s.runTool(ctx, spec, rec.emit)
	close(stopFlush)
	<-flushed

	wctx, wcancel := context.WithTimeout(context.Background(), finalWriteTimeout)
	defer wcancel()
	rec.flush(wctx)
	status, errText := localOutcome(rec.isFull(), err)
	var sum []byte
	if summary != nil {
		if sum, err = json.Marshal(summary); err != nil {
			log.Printf("[tools] run %s: encoding the summary: %v", run.ID, err)
			sum = nil
		}
	}
	if _, err := s.finish(wctx, run.ID, status, sum, errText, ownerActor(run)); err != nil {
		log.Printf("[tools] %v", err)
	}
}

// localOutcome maps how a local tool ended to a final status and error
// text. Hitting the event cap wins: the recorder cancelled the tool itself.
func localOutcome(full bool, err error) (status, errText string) {
	switch {
	case full:
		return models.ToolRunFailed, msgTooMuchOutput
	case err == nil:
		return models.ToolRunDone, ""
	case errors.Is(err, context.DeadlineExceeded):
		return models.ToolRunTimedOut, msgTimedOut
	case errors.Is(err, context.Canceled):
		return models.ToolRunCancelled, ""
	default:
		return models.ToolRunFailed, err.Error()
	}
}
```

- [ ] **Step 5: Write the queries and cancel**

Create `backend/internal/toolruns/query.go`:

```go
package toolruns

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// ErrRunNotFound is returned for an unknown run id.
var ErrRunNotFound = errors.New("tool run not found")

// Get returns one run.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*models.ToolRun, error) {
	var run models.ToolRun
	err := s.db.WithContext(ctx).First(&run, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading tool run %s: %w", id, err)
	}
	return &run, nil
}

// Events returns a run's events after afterSeq, in seq order (never nil).
func (s *Service) Events(ctx context.Context, id uuid.UUID, afterSeq int) ([]models.ToolRunEvent, error) {
	events := []models.ToolRunEvent{}
	if err := s.db.WithContext(ctx).Where("run_id = ? AND seq > ?", id, afterSeq).
		Order("seq").Find(&events).Error; err != nil {
		return nil, fmt.Errorf("loading events of tool run %s: %w", id, err)
	}
	return events, nil
}

// ListFilter narrows the run history. Empty fields do not filter.
type ListFilter struct {
	Tool    string
	UserID  *uuid.UUID
	AgentID string // readable agent id (agent_ref)
	Target  string // exact match on target or target_ip
	Status  string
	Limit   int // default 50, max 200
	Offset  int
}

// List returns runs newest first and the number matching the filter.
func (s *Service) List(ctx context.Context, f ListFilter) ([]models.ToolRun, int64, error) {
	q := s.db.WithContext(ctx).Model(&models.ToolRun{})
	if f.Tool != "" {
		q = q.Where("tool = ?", f.Tool)
	}
	if f.UserID != nil {
		q = q.Where("user_id = ?", *f.UserID)
	}
	if f.AgentID != "" {
		q = q.Where("agent_ref = ?", f.AgentID)
	}
	if f.Target != "" {
		q = q.Where("(target = ? OR target_ip = ?)", f.Target, f.Target)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("counting tool runs: %w", err)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := max(f.Offset, 0)
	runs := []models.ToolRun{}
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&runs).Error; err != nil {
		return nil, 0, fmt.Errorf("listing tool runs: %w", err)
	}
	return runs, total, nil
}

// Cancel stops a run at once: its owner or an admin may. A local run's tool
// is stopped; an agent sees cancel: true on its next events post. A later
// finish never undoes it (ruling 8).
func (s *Service) Cancel(ctx context.Context, who Requester, id uuid.UUID) (*models.ToolRun, error) {
	run, err := s.Get(ctx, id)
	if errors.Is(err, ErrRunNotFound) {
		return nil, refuse(http.StatusNotFound, CodeNotFound, "no such tool run")
	}
	if err != nil {
		return nil, err
	}
	if !who.IsAdmin && (run.UserID == nil || *run.UserID != who.UserID) {
		return nil, refuse(http.StatusForbidden, CodeForbidden, "only the user who started a run, or an admin, can cancel it")
	}
	if !models.ToolRunActive(run.Status) {
		return nil, refuse(http.StatusConflict, CodeConflict, "the run has already finished")
	}
	moved, err := s.finish(ctx, id, models.ToolRunCancelled, nil, "", who.actor())
	if err != nil {
		return nil, err
	}
	if !moved {
		return nil, refuse(http.StatusConflict, CodeConflict, "the run has already finished")
	}
	return s.Get(ctx, id)
}
```

- [ ] **Step 6: Install the launcher in New**

In `backend/internal/toolruns/service.go`, in `New`, replace

```go
	// Replaced by the local launcher in Task 9.
	s.launch = func(*models.ToolRun, nettools.Spec) {}
```

with

```go
	s.launch = s.launchLocal
```

(The `models` and `nettools` imports are still used by the `Service` struct's field types.)

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/toolruns/ -run 'TestResultLine|TestLocalOutcome' -v`
Expected: PASS.

Run: `./scripts/test-db.sh -run 'TestDBLocalRun|TestDBCancelLocalRun|TestDBCancelRules|TestDBInsertEventsIgnoresDuplicates|TestDBGetEventsAndList|TestDBCreateSentinelRun' -v`
Expected: PASS for the following:
- `TestDBLocalRunRecordsAndStreams`, `TestDBLocalRunToolError`, `TestDBLocalRunEventCap` and `TestDBLocalRunThroughRunner`
- `TestDBCancelLocalRun` and `TestDBCancelRules`
- `TestDBInsertEventsIgnoresDuplicates` and `TestDBGetEventsAndList`
- `TestDBCreateSentinelRun`, which still passes because the fixtures swap `launch`

Then the concurrent paths under the race detector: `./scripts/test-db.sh -race -run 'TestDB(LocalRun|CancelLocalRun|InsertEvents|CreateCapsUnderConcurrency)' -v`
Expected: PASS, no race reports.

- [ ] **Step 8: Vet and commit**

Run: `go vet ./internal/toolruns/`
Expected: no output.

```bash
git add backend/internal/toolruns/
git commit -m "feat(tools): record events, run tools on the server, history and cancel

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Agent dispatch and the sweeper

This task adds the agent side of the queue and the sweeper.

`NextJob` records the poll and refuses when the admin switch is off. It then claims the agent's oldest queued run with one `UPDATE … WHERE id = (SELECT … FOR UPDATE SKIP LOCKED) RETURNING *`, which also resets the deadline to claim time plus the tool's limit. If nothing is queued it waits on the agent's wake signal, the hang-up or the wait. When the wait runs out it looks once more before returning `(nil, nil)`.

`AgentEvents` reuses Task 9's `insertEvents`. `AgentFinish` goes through `finish`, so cancel wins.

The sweeper:
- fails queued runs nobody claimed within 30 s
- times out running runs past deadline + 15 s
- interrupts every active run at startup
- prunes finished runs past the retention period

Every sweeper finish publishes `end` and is audited as `system`.

Choices made here:
- `AgentFinish` rejects a status other than done, failed or refused with `ErrBadFinishStatus` (400 in Task 12).
- It refuses a finish for a run never claimed (`queued`) with `ErrRunNotActive`.
- For a run that ended otherwise (timed out, interrupted, already finished), a late finish fills a NULL summary and still returns `ErrRunNotActive`; for a cancelled run it returns nil.
- `AgentEvents` drops events with a seq below 1 or no type.
- The agent's error text is clipped to 500 bytes.

**Files:**
- Create: `backend/internal/toolruns/dispatch.go`
- Create: `backend/internal/toolruns/sweeper.go`
- Test: `backend/internal/toolruns/dispatch_db_test.go` (DB)
- Test: `backend/internal/toolruns/sweeper_db_test.go` (DB)

**Interfaces:**
- Consumes:
  - Task 9: `insertEvents`, `finish`, `publishStatus`, `ownerActor`, `systemActor`, the `msg*` constants, `Get`, `Events`, `Cancel` and `Subscribe`.
  - Task 8: `polls`, `wakeChan`, `LoadSettings` and the fixtures (`newEnv`, `readyAgent`, `insertRun`, `reload`, `user`, `agentPing`, `waitFor` from Task 9).
  - `nettools.Deadline`, `nettools.Spec` and `nettools.Params`.
- Produces:
  - Shared contract:
    - `Job{RunID, Spec, Deadline}` and the errors `ErrToolsDisabled`, `ErrRunNotActive` and `ErrTooMuchOutput`
    - `func (s *Service) NextJob(ctx, agent *models.Agent, wait time.Duration) (*Job, error)`
    - `AgentEvent{Seq, At, Type, Data}` and `func (s *Service) AgentEvents(ctx, agent *models.Agent, runID uuid.UUID, events []AgentEvent) (cancel bool, err error)`
    - `AgentFinish{Status, Summary, Error}` and `func (s *Service) AgentFinish(ctx, agent *models.Agent, runID uuid.UUID, f AgentFinish) error`
    - `func (s *Service) InterruptStale(ctx) (int64, error)`, `func (s *Service) Sweep(ctx) error`, `func (s *Service) Prune(ctx) (int64, error)` and `func (s *Service) Start(ctx)`
  - Contract change 2: `var ErrBadFinishStatus`.
  - For Task 12's handler:
    - `ErrToolsDisabled` → 403 `tools_disabled`
    - `ErrRunNotActive` → 409 `conflict`
    - `ErrTooMuchOutput` → 413
    - `ErrBadFinishStatus` → 400
    - `(nil, nil)` from `NextJob` → 204
  - For Task 11: `Sweep` finishing a run publishes `end` to open subscriptions. Its tests can push a run past its deadline with a `Deps.Now` clock and call `Sweep` directly.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/toolruns/dispatch_db_test.go`. `TestDBNextJobClaimRace` releases two polls through a start barrier for each of 20 queued runs and asserts that exactly one poll claims each run. `TestDBAgentFinish` and `TestDBCancelAgentRun` pin Review Focus 5.

```go
package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// queueRun inserts a queued run for agent, created at the clock's now.
func (e *env) queueRun(t *testing.T, agent *models.Agent, mutate func(r *models.ToolRun)) *models.ToolRun {
	t.Helper()
	return e.insertRun(t, func(r *models.ToolRun) {
		r.Status, r.StartedAt = models.ToolRunQueued, nil
		r.VantageKind, r.AgentUUID, r.AgentRef, r.VantageName = models.VantageAgent, &agent.ID, &agent.AgentID, agent.Name
		r.Params = models.RawJSON(`{"count":3,"interval_ms":500,"timeout_ms":1000,"size":56}`)
		if mutate != nil {
			mutate(r)
		}
	})
}

// A claimed job carries the stored spec, the run moves to running with a
// fresh deadline, and the stream hears about it.
func TestDBNextJobClaims(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	older := e.queueRun(t, agent, func(r *models.ToolRun) { r.CreatedAt = r.CreatedAt.Add(-time.Second) })
	newer := e.queueRun(t, agent, nil)
	sub := e.svc.Subscribe(older.ID)
	defer sub.Close()
	e.clock.Add(5 * time.Second)
	claimedAt := e.clock.Now()

	job, err := e.svc.NextJob(ctx, agent, time.Second)
	testdb.Must(t, err)
	if job == nil || job.RunID != older.ID {
		t.Fatalf("job = %+v, want the older run %s", job, older.ID)
	}
	if job.Spec.Tool != nettools.ToolPing || job.Spec.Target != "10.0.0.200" || job.Spec.TargetIP != "10.0.0.200" ||
		job.Spec.Params.Count != 3 || job.Spec.Params.IntervalMS != 500 || !job.Deadline.Equal(claimedAt.Add(2*time.Minute)) {
		t.Errorf("job = %+v, want the stored ping spec and deadline claim+2m", job)
	}
	got := e.reload(t, older.ID)
	if got.Status != models.ToolRunRunning || got.StartedAt == nil || !got.StartedAt.Equal(claimedAt) || !got.Deadline.Equal(job.Deadline) {
		t.Errorf("claimed run = %+v", got)
	}
	select {
	case m := <-sub.C():
		if m.Event != "status" || string(m.Data) != `{"status":"running"}` {
			t.Errorf("frame %s %s, want status running", m.Event, m.Data)
		}
	default:
		t.Error("no status frame")
	}
	if e.reload(t, newer.ID).Status != models.ToolRunQueued {
		t.Error("the newer run was claimed too")
	}
}

// Two polls racing for one queued run: exactly one gets it, every time.
func TestDBNextJobClaimRace(t *testing.T) {
	e := newEnv(t)
	agent := e.readyAgent(t, "file-server")
	for round := 0; round < 20; round++ {
		run := e.queueRun(t, agent, nil)
		start := make(chan struct{})
		var wg sync.WaitGroup
		jobs := make([]*Job, 2)
		errs := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				jobs[i], errs[i] = e.svc.NextJob(context.Background(), agent, 0)
			}()
		}
		close(start)
		wg.Wait()
		testdb.Must(t, errors.Join(errs...))
		claimed := 0
		for _, j := range jobs {
			if j != nil {
				claimed++
				if j.RunID != run.ID {
					t.Fatalf("round %d claimed %s, want %s", round, j.RunID, run.ID)
				}
			}
		}
		if claimed != 1 {
			t.Fatalf("round %d: %d polls claimed the run, want exactly 1", round, claimed)
		}
		testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'done' WHERE id = ?`, run.ID)
	}
}

// A waiting poll answers as soon as Create queues a run for its agent.
func TestDBNextJobWakesOnCreate(t *testing.T) {
	e := newEnv(t)
	agent := e.readyAgent(t, "file-server")
	type result struct {
		job *Job
		err error
		at  time.Time
	}
	done := make(chan result, 1)
	go func() {
		job, err := e.svc.NextJob(context.Background(), agent, 2*time.Second)
		done <- result{job, err, time.Now()}
	}()
	time.Sleep(100 * time.Millisecond) // let the poll start waiting
	select {
	case r := <-done:
		t.Fatalf("the poll returned before any run existed: %+v", r)
	default:
	}
	created := time.Now()
	run, err := e.svc.Create(context.Background(), e.user(t, false), agentPing(agent, "10.0.0.5"))
	testdb.Must(t, err)
	r := <-done
	testdb.Must(t, r.err)
	if r.job == nil || r.job.RunID != run.ID {
		t.Fatalf("job = %+v, want run %s", r.job, run.ID)
	}
	if waited := r.at.Sub(created); waited > 500*time.Millisecond {
		t.Errorf("the poll answered %v after the create, want well under the 2 s wait", waited)
	}
}

func TestDBNextJobTimesOutAndRecordsPolls(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.newAgent(t, "file-server", true, boolPtr(true))
	start := time.Now()
	job, err := e.svc.NextJob(ctx, agent, 150*time.Millisecond)
	if job != nil || err != nil || time.Since(start) < 150*time.Millisecond {
		t.Errorf("empty queue: %+v, %v after %v; want nil, nil after the full wait", job, err, time.Since(start))
	}
	if _, ok := e.svc.polls.lastSeen(agent.ID); !ok {
		t.Error("the poll was not recorded")
	}

	off := e.newAgent(t, "off-box", false, boolPtr(true))
	if _, err := e.svc.NextJob(ctx, off, time.Second); !errors.Is(err, ErrToolsDisabled) {
		t.Errorf("tools off: %v, want ErrToolsDisabled", err)
	}
	if _, ok := e.svc.polls.lastSeen(off.ID); !ok {
		t.Error("a refused poll was not recorded")
	}

	// A hung-up agent ends the wait at once.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if job, err := e.svc.NextJob(cctx, agent, 5*time.Second); job != nil || err != nil {
		t.Errorf("cancelled poll: %+v, %v", job, err)
	}
}

func agentEvents(seqs ...int) []AgentEvent {
	out := make([]AgentEvent, len(seqs))
	for i, s := range seqs {
		out[i] = AgentEvent{Seq: s, At: time.Date(2026, 10, 5, 12, 0, s, 0, time.UTC), Type: "reply",
			Data: json.RawMessage(fmt.Sprintf(`{"seq":%d}`, s))}
	}
	return out
}

func TestDBAgentEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	other := e.readyAgent(t, "other-box")
	run := e.queueRun(t, agent, func(r *models.ToolRun) { r.Status = models.ToolRunRunning })
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()

	cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(1, 2))
	if cancel || err != nil {
		t.Fatalf("first batch: cancel %v, %v", cancel, err)
	}
	// A retried batch is ignored.
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(1, 2)); cancel || err != nil {
		t.Fatalf("retried batch: cancel %v, %v", cancel, err)
	}
	if got := e.reload(t, run.ID).EventCount; got != 2 {
		t.Errorf("event_count = %d after a retried batch, want 2", got)
	}
	stored, err := e.svc.Events(ctx, run.ID, 0)
	testdb.Must(t, err)
	if len(stored) != 2 || !stored[1].At.Equal(time.Date(2026, 10, 5, 12, 0, 2, 0, time.UTC)) {
		t.Errorf("stored = %+v, want 2 events at the agent's times", stored)
	}
	for _, want := range []string{"1", "2"} {
		if m := <-sub.C(); m.Event != "event" || m.ID != want {
			t.Errorf("frame %s %s, want event %s", m.Event, m.ID, want)
		}
	}

	if _, err := e.svc.AgentEvents(ctx, other, run.ID, agentEvents(3)); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("another agent's post: %v, want ErrRunNotActive", err)
	}
	if _, err := e.svc.AgentEvents(ctx, agent, uuid.New(), agentEvents(3)); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("an unknown run: %v, want ErrRunNotActive", err)
	}

	// Cancelled: tell the agent, store nothing.
	testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'cancelled' WHERE id = ?`, run.ID)
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(3)); !cancel || err != nil {
		t.Errorf("cancelled run: cancel %v, %v; want true, nil", cancel, err)
	}
	if got := e.reload(t, run.ID).EventCount; got != 2 {
		t.Errorf("a cancelled run stored events: %d", got)
	}
	// Ended any other way: 409.
	testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'interrupted' WHERE id = ?`, run.ID)
	if _, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(3)); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("interrupted run: %v, want ErrRunNotActive", err)
	}
}

// A batch that would pass MaxEventsPerRun fails the run.
func TestDBAgentEventsCap(t *testing.T) {
	e := newEnv(t)
	agent := e.readyAgent(t, "file-server")
	run := e.queueRun(t, agent, func(r *models.ToolRun) { r.Status, r.EventCount = models.ToolRunRunning, MaxEventsPerRun-1 })
	if _, err := e.svc.AgentEvents(context.Background(), agent, run.ID, agentEvents(1, 2)); !errors.Is(err, ErrTooMuchOutput) {
		t.Fatalf("err = %v, want ErrTooMuchOutput", err)
	}
	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunFailed || got.Error == nil || *got.Error != "too much output" || got.EventCount != MaxEventsPerRun-1 {
		t.Errorf("run = %s %v %d, want failed, too much output, nothing stored", got.Status, got.Error, got.EventCount)
	}
}

func TestDBAgentFinish(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	other := e.readyAgent(t, "other-box")
	running := func() *models.ToolRun {
		return e.queueRun(t, agent, func(r *models.ToolRun) { r.Status = models.ToolRunRunning })
	}

	run := running()
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()
	testdb.Must(t, e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunDone,
		Summary: json.RawMessage(`{"sent":3,"received":3,"loss_pct":0,"avg_ms":1.5}`)}))
	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunDone || got.Summary == nil || got.FinishedAt == nil || got.Error != nil {
		t.Errorf("finished run = %+v", got)
	}
	if m := <-sub.C(); m.Event != "end" || !strings.Contains(string(m.Data), `"status":"done"`) {
		t.Errorf("frame %s %s, want end with status done", m.Event, m.Data)
	}
	if f := e.audit.byAction(models.ActionToolRunFinished); len(f) != 1 || f[0].changes.Summary["result"] != "0% loss, 1.5 ms avg" {
		t.Errorf("finish audit = %+v", f)
	}

	// Review Focus 5: cancelled while the agent ran; the late finish keeps
	// it cancelled and only fills the summary.
	run = running()
	testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'cancelled', finished_at = now() WHERE id = ?`, run.ID)
	testdb.Must(t, e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunDone,
		Summary: json.RawMessage(`{"sent":1}`)}))
	got = e.reload(t, run.ID)
	if got.Status != models.ToolRunCancelled || got.Summary == nil || got.Error != nil {
		t.Errorf("finish after cancel = %+v, want still cancelled, summary filled, no error", got)
	}

	run = running()
	if err := e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunCancelled}); !errors.Is(err, ErrBadFinishStatus) {
		t.Errorf("status cancelled from an agent: %v, want ErrBadFinishStatus", err)
	}
	if err := e.svc.AgentFinish(ctx, other, run.ID, AgentFinish{Status: models.ToolRunDone}); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("another agent's finish: %v, want ErrRunNotActive", err)
	}
	testdb.Must(t, e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunRefused,
		Error: "10.0.0.200 is outside TOOLS_ALLOWED_TARGETS" + strings.Repeat("!", 600)}))
	if got := e.reload(t, run.ID); got.Status != models.ToolRunRefused || got.Error == nil || len(*got.Error) != 500 {
		t.Errorf("refused finish = %+v, want refused with the error clipped to 500 bytes", got)
	}
	if err := e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunDone}); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("a second finish: %v, want ErrRunNotActive", err)
	}
	queued := e.queueRun(t, agent, nil)
	if err := e.svc.AgentFinish(ctx, agent, queued.ID, AgentFinish{Status: models.ToolRunDone}); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("finishing an unclaimed run: %v, want ErrRunNotActive", err)
	}
}

// Review Focus 5 end to end: the user cancels while the agent runs; the
// agent's next post is told to stop and its finish cannot undo the cancel.
func TestDBCancelAgentRun(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	who := e.user(t, false)
	run, err := e.svc.Create(ctx, who, agentPing(agent, "10.0.0.5"))
	testdb.Must(t, err)
	job, err := e.svc.NextJob(ctx, agent, 0)
	testdb.Must(t, err)
	if job == nil || job.RunID != run.ID {
		t.Fatalf("job = %+v", job)
	}
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(1)); cancel || err != nil {
		t.Fatalf("before the cancel: %v, %v", cancel, err)
	}
	got, err := e.svc.Cancel(ctx, who, run.ID)
	testdb.Must(t, err)
	if got.Status != models.ToolRunCancelled {
		t.Fatalf("Cancel left %s", got.Status)
	}
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(2)); !cancel || err != nil {
		t.Errorf("after the cancel: cancel %v, %v; want true", cancel, err)
	}
	testdb.Must(t, e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunFailed, Error: "cancelled"}))
	if got := e.reload(t, run.ID); got.Status != models.ToolRunCancelled || got.Error != nil || got.EventCount != 1 {
		t.Errorf("run = %+v, want cancelled, no error, the one event from before", got)
	}
}
```

Create `backend/internal/toolruns/sweeper_db_test.go`. `TestDBSweepTimesOutVanishedAgentRun` pins Review Focus 1.

```go
package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBSweepPickupTimeout(t *testing.T) {
	e := newEnv(t)
	agent := e.readyAgent(t, "file-server")
	now := e.clock.Now()
	stale := e.queueRun(t, agent, func(r *models.ToolRun) { r.CreatedAt = now.Add(-PickupTimeout - time.Second) })
	fresh := e.queueRun(t, agent, func(r *models.ToolRun) { r.CreatedAt = now.Add(-PickupTimeout + time.Second) })

	testdb.Must(t, e.svc.Sweep(context.Background()))
	got := e.reload(t, stale.ID)
	if got.Status != models.ToolRunFailed || got.Error == nil || *got.Error != "the agent didn't pick up the job (offline?)" {
		t.Errorf("stale queued run = %s %v", got.Status, got.Error)
	}
	if e.reload(t, fresh.ID).Status != models.ToolRunQueued {
		t.Error("a run queued 29 s ago was failed")
	}
	f := e.audit.byAction(models.ActionToolRunFinished)
	if len(f) != 1 || f[0].actor.Username != "system" || f[0].actor.UserID != uuid.Nil {
		t.Errorf("finish audit = %+v, want one entry by the system", f)
	}
}

// Review Focus 1: an agent claims a run and vanishes. At deadline + 15 s the
// sweeper times the run out and any open stream gets its end frame.
func TestDBSweepTimesOutVanishedAgentRun(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	run, err := e.svc.Create(ctx, e.user(t, false), agentPing(agent, "10.0.0.5"))
	testdb.Must(t, err)
	job, err := e.svc.NextJob(ctx, agent, 0)
	testdb.Must(t, err)
	if job == nil {
		t.Fatal("no job")
	}
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()

	e.clock.Add(2*time.Minute + OverdueGrace) // exactly deadline + grace: not yet
	testdb.Must(t, e.svc.Sweep(ctx))
	if e.reload(t, run.ID).Status != models.ToolRunRunning {
		t.Fatal("timed out at deadline + grace exactly, want only after it")
	}
	e.clock.Add(time.Second)
	testdb.Must(t, e.svc.Sweep(ctx))

	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunTimedOut || got.Error == nil || *got.Error != "the run went past its time limit" {
		t.Errorf("run = %s %v, want timed_out", got.Status, got.Error)
	}
	select {
	case m := <-sub.C():
		var end models.ToolRun
		testdb.Must(t, json.Unmarshal(m.Data, &end))
		if m.Event != "end" || end.Status != models.ToolRunTimedOut {
			t.Errorf("frame %s with status %s, want end / timed_out", m.Event, end.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("no end frame reached the open stream")
	}
	// The agent's late posts are refused.
	if _, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(1)); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("late events: %v, want ErrRunNotActive", err)
	}
}

func TestDBInterruptStale(t *testing.T) {
	e := newEnv(t)
	agent := e.readyAgent(t, "file-server")
	queued := e.queueRun(t, agent, nil)
	running := e.insertRun(t, nil)
	done := e.insertRun(t, func(r *models.ToolRun) { r.Status = models.ToolRunDone })

	n, err := e.svc.InterruptStale(context.Background())
	testdb.Must(t, err)
	if n != 2 {
		t.Errorf("interrupted %d runs, want 2", n)
	}
	for _, r := range []*models.ToolRun{queued, running} {
		if got := e.reload(t, r.ID); got.Status != models.ToolRunInterrupted || got.FinishedAt == nil {
			t.Errorf("run %s = %s, want interrupted", r.ID, got.Status)
		}
	}
	if e.reload(t, done.ID).Status != models.ToolRunDone {
		t.Error("a finished run was interrupted")
	}
}

// Prune deletes finished runs past the retention with their events, and
// nothing else.
func TestDBPrune(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	now := e.clock.Now()
	finishedAt := func(days int) func(r *models.ToolRun) {
		return func(r *models.ToolRun) {
			at := now.AddDate(0, 0, -days)
			r.Status, r.CreatedAt, r.FinishedAt = models.ToolRunDone, at, &at
		}
	}
	old := e.insertRun(t, finishedAt(31))
	recent := e.insertRun(t, finishedAt(29))
	active := e.insertRun(t, func(r *models.ToolRun) { r.CreatedAt = now.AddDate(0, 0, -40) })
	testdb.Exec(t, e.db, `INSERT INTO tool_run_events (run_id, seq, at, type, data) VALUES (?, 1, now(), 'reply', '{}')`, old.ID)

	n, err := e.svc.Prune(ctx)
	testdb.Must(t, err)
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
	var left []string
	testdb.Must(t, e.db.Raw(`SELECT id::text FROM tool_runs ORDER BY created_at`).Scan(&left).Error)
	if len(left) != 2 || left[0] != active.ID.String() || left[1] != recent.ID.String() {
		t.Errorf("left %v, want the active and the recent run", left)
	}
	var events int64
	testdb.Must(t, e.db.Raw(`SELECT count(*) FROM tool_run_events`).Scan(&events).Error)
	if events != 0 {
		t.Errorf("%d events outlived their pruned run", events)
	}

	_, err = SaveSettings(ctx, e.settings, Settings{Allowlist: testAllowlist, ServerEnabled: true, RetentionDays: 7})
	testdb.Must(t, err)
	if n, err := e.svc.Prune(ctx); err != nil || n != 1 {
		t.Errorf("with 7 days: pruned %d, %v; want the 29-day-old run", n, err)
	}
}

// Start interrupts what a previous process left and stops with its context.
func TestDBStart(t *testing.T) {
	e := newEnv(t)
	left := e.insertRun(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { e.svc.Start(ctx); close(stopped) }()
	waitFor(t, "the leftover run to be interrupted", func() bool {
		return e.reload(t, left.ID).Status == models.ToolRunInterrupted
	})
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after its context ended")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go vet ./internal/toolruns/`
Expected: compile errors such as `e.svc.NextJob undefined`, `undefined: AgentEvent`, `undefined: AgentFinish`, `undefined: ErrBadFinishStatus` and `e.svc.Sweep undefined`.

- [ ] **Step 3: Write the dispatcher**

Create `backend/internal/toolruns/dispatch.go`. The claim computes the deadline in SQL from the tool column. The per-tool seconds come from `nettools.Deadline`, so the limits live in one place. The `::timestamptz` and `::float8` casts are needed because Postgres cannot infer the parameter types from `? + make_interval(…)` or from a `CASE` of bare parameters.

```go
package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

// Job is what an agent receives from GET jobs/next.
type Job struct {
	RunID    uuid.UUID     `json:"run_id"`
	Spec     nettools.Spec `json:"spec"`
	Deadline time.Time     `json:"deadline"`
}

// Agent job errors.
var (
	ErrToolsDisabled = errors.New("tools are off for this agent")
	ErrRunNotActive  = errors.New("that run is not running on this agent")
	ErrTooMuchOutput = errors.New("too much output")
	// ErrBadFinishStatus: a finish whose status is not done, failed or
	// refused. The API answers 400.
	ErrBadFinishStatus = errors.New("status must be done, failed or refused")
)

// NextJob claims the oldest queued run for agent, waiting up to wait for one
// to be queued (woken by Create). (nil, nil) when none arrived in time or
// the agent hung up. ErrToolsDisabled when the admin switch is off. Every
// call records a poll, which is what makes the agent count as ready.
func (s *Service) NextJob(ctx context.Context, agent *models.Agent, wait time.Duration) (*Job, error) {
	s.polls.seen(agent.ID, s.now())
	defer func() { s.polls.seen(agent.ID, s.now()) }()
	if !agent.ToolsEnabled {
		return nil, ErrToolsDisabled
	}
	wake := s.wakeChan(agent.ID)
	timer := time.NewTimer(max(wait, 0))
	defer timer.Stop()
	for {
		job, err := s.claim(ctx, agent)
		if err != nil || job != nil {
			return job, err
		}
		select {
		case <-wake:
		case <-timer.C:
			// One last look: a run queued as the wait ran out.
			return s.claim(ctx, agent)
		case <-ctx.Done():
			return nil, nil
		}
	}
}

// claim moves agent's oldest queued run to running in one statement, so two
// polls can never take the same run (SKIP LOCKED makes the loser find
// nothing), and resets its deadline to now + the tool's limit.
func (s *Service) claim(ctx context.Context, agent *models.Agent) (*Job, error) {
	now := s.now()
	secs := func(t nettools.Tool) float64 { return nettools.Deadline(t).Seconds() }
	var runs []models.ToolRun
	err := s.db.WithContext(ctx).Raw(`UPDATE tool_runs
		SET status = ?, started_at = ?::timestamptz,
			deadline = ?::timestamptz + make_interval(secs => CASE tool
				WHEN 'ping' THEN ?::float8 WHEN 'traceroute' THEN ?::float8
				WHEN 'dns' THEN ?::float8 ELSE ?::float8 END)
		WHERE id = (SELECT id FROM tool_runs WHERE agent_id = ? AND status = ?
		            ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING *`,
		models.ToolRunRunning, now, now,
		secs(nettools.ToolPing), secs(nettools.ToolTraceroute), secs(nettools.ToolDNS), secs(nettools.ToolTCP),
		agent.ID, models.ToolRunQueued).Scan(&runs).Error
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil
		}
		return nil, fmt.Errorf("claiming a job for %s: %w", agent.AgentID, err)
	}
	if len(runs) == 0 {
		return nil, nil
	}
	run := &runs[0]
	var params nettools.Params
	if err := json.Unmarshal(run.Params, &params); err != nil {
		return nil, fmt.Errorf("decoding the parameters of run %s: %w", run.ID, err)
	}
	spec := nettools.Spec{Tool: nettools.Tool(run.Tool), Target: run.Target, Params: params}
	if run.TargetIP != nil {
		spec.TargetIP = *run.TargetIP
	}
	s.publishStatus(run.ID, models.ToolRunRunning)
	return &Job{RunID: run.ID, Spec: spec, Deadline: run.Deadline}, nil
}

// AgentEvent is one event as an agent posts it.
type AgentEvent struct {
	Seq  int             `json:"seq"`
	At   time.Time       `json:"at"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// agentRun loads runID and checks it is agent's.
func (s *Service) agentRun(ctx context.Context, agent *models.Agent, runID uuid.UUID) (*models.ToolRun, error) {
	run, err := s.Get(ctx, runID)
	if errors.Is(err, ErrRunNotFound) {
		return nil, ErrRunNotActive
	}
	if err != nil {
		return nil, err
	}
	if run.AgentUUID == nil || *run.AgentUUID != agent.ID {
		return nil, ErrRunNotActive
	}
	return run, nil
}

// AgentEvents stores a batch from agent for runID (seqs already stored are
// ignored, so a retried post never duplicates) and publishes it. cancel is
// true when the run has been cancelled: nothing is stored and the agent
// stops the tool. ErrRunNotActive when the run is not this agent's or has
// ended otherwise. ErrTooMuchOutput when the batch would take the run past
// MaxEventsPerRun; the run is then failed. Events with a seq below 1 or no
// type are dropped.
func (s *Service) AgentEvents(ctx context.Context, agent *models.Agent, runID uuid.UUID, events []AgentEvent) (bool, error) {
	run, err := s.agentRun(ctx, agent, runID)
	if err != nil {
		return false, err
	}
	switch run.Status {
	case models.ToolRunCancelled:
		return true, nil
	case models.ToolRunRunning:
	default:
		return false, ErrRunNotActive
	}
	if run.EventCount+len(events) > MaxEventsPerRun {
		if _, err := s.finish(ctx, runID, models.ToolRunFailed, nil, msgTooMuchOutput, ownerActor(run)); err != nil {
			return false, err
		}
		return false, ErrTooMuchOutput
	}
	rows := make([]models.ToolRunEvent, 0, len(events))
	for _, ev := range events {
		if ev.Seq < 1 || ev.Type == "" {
			continue
		}
		at := ev.At.UTC()
		if ev.At.IsZero() {
			at = s.now()
		}
		data := models.RawJSON(ev.Data)
		if len(data) == 0 {
			data = models.RawJSON(`null`)
		}
		rows = append(rows, models.ToolRunEvent{RunID: runID, Seq: ev.Seq, At: at, Type: ev.Type, Data: data})
	}
	_, err = s.insertEvents(ctx, runID, rows)
	return false, err
}

// maxAgentErrorLen bounds the error text an agent may store on a run.
const maxAgentErrorLen = 500

// clip shortens s to at most n bytes without splitting a UTF-8 character.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// AgentFinish is the body of POST jobs/:run_id/finish.
type AgentFinish struct {
	Status  string          `json:"status"` // done | failed | refused
	Summary json.RawMessage `json:"summary"`
	Error   string          `json:"error"`
}

// AgentFinish records agent's outcome for runID. A run cancelled meanwhile
// stays cancelled and only gains the summary (ruling 8); that is not an
// error. ErrBadFinishStatus for another status; ErrRunNotActive when the run
// is not this agent's, was never claimed, or ended for another reason.
func (s *Service) AgentFinish(ctx context.Context, agent *models.Agent, runID uuid.UUID, f AgentFinish) error {
	switch f.Status {
	case models.ToolRunDone, models.ToolRunFailed, models.ToolRunRefused:
	default:
		return ErrBadFinishStatus
	}
	run, err := s.agentRun(ctx, agent, runID)
	if err != nil {
		return err
	}
	if run.Status == models.ToolRunQueued {
		return ErrRunNotActive
	}
	moved, err := s.finish(ctx, runID, f.Status, f.Summary, clip(f.Error, maxAgentErrorLen), ownerActor(run))
	if err != nil || moved {
		return err
	}
	if cur, err := s.Get(ctx, runID); err == nil && cur.Status == models.ToolRunCancelled {
		return nil
	}
	return ErrRunNotActive
}
```

- [ ] **Step 4: Write the sweeper**

Create `backend/internal/toolruns/sweeper.go`:

```go
package toolruns

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// pruneEvery is how often finished runs past the retention are deleted;
// the first prune runs a minute after start.
const pruneEvery = 24 * time.Hour

// finishAll finishes each run in ids as the system and counts the ones this
// call moved.
func (s *Service) finishAll(ctx context.Context, ids []uuid.UUID, status, errText string, actor services.Actor) (int64, error) {
	var n int64
	for _, id := range ids {
		moved, err := s.finish(ctx, id, status, nil, errText, actor)
		if err != nil {
			return n, err
		}
		if moved {
			n++
		}
	}
	return n, nil
}

// activeIDs returns the ids of runs matching where (plus args).
func (s *Service) activeIDs(ctx context.Context, where string, args ...any) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	if err := s.db.WithContext(ctx).Model(&models.ToolRun{}).Where(where, args...).
		Order("created_at").Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("finding tool runs to sweep: %w", err)
	}
	return ids, nil
}

// InterruptStale marks every queued or running run interrupted. Called once
// at startup: no run survives a restart, and an agent still working on one
// gets 409 on its next post and stops.
func (s *Service) InterruptStale(ctx context.Context) (int64, error) {
	ids, err := s.activeIDs(ctx, "status IN (?, ?)", models.ToolRunQueued, models.ToolRunRunning)
	if err != nil {
		return 0, err
	}
	return s.finishAll(ctx, ids, models.ToolRunInterrupted, msgInterrupted, systemActor)
}

// Sweep fails queued runs no agent picked up within PickupTimeout and times
// out running runs past their deadline plus OverdueGrace (an agent that
// vanished mid-run). Each finish ends open streams and is audited as the
// system.
func (s *Service) Sweep(ctx context.Context) error {
	now := s.now()
	stale, err := s.activeIDs(ctx, "status = ? AND created_at < ?", models.ToolRunQueued, now.Add(-PickupTimeout))
	if err != nil {
		return err
	}
	if _, err := s.finishAll(ctx, stale, models.ToolRunFailed, msgNotPickedUp, systemActor); err != nil {
		return err
	}
	overdue, err := s.activeIDs(ctx, "status = ? AND deadline < ?", models.ToolRunRunning, now.Add(-OverdueGrace))
	if err != nil {
		return err
	}
	_, err = s.finishAll(ctx, overdue, models.ToolRunTimedOut, msgTimedOut, systemActor)
	return err
}

// Prune deletes finished runs older than the retention setting; their
// events go with them. Audit entries are kept.
func (s *Service) Prune(ctx context.Context) (int64, error) {
	days := LoadSettings(ctx, s.settings).RetentionDays
	cutoff := s.now().AddDate(0, 0, -days)
	res := s.db.WithContext(ctx).
		Where("finished_at IS NOT NULL AND finished_at < ? AND status NOT IN (?, ?)",
			cutoff, models.ToolRunQueued, models.ToolRunRunning).
		Delete(&models.ToolRun{})
	if res.Error != nil {
		return 0, fmt.Errorf("pruning tool runs: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// Start interrupts the runs a previous process left behind, then sweeps
// every SweepEvery and prunes a minute after start and every 24 h after
// that, until ctx ends.
func (s *Service) Start(ctx context.Context) {
	if n, err := s.InterruptStale(ctx); err != nil {
		log.Printf("[tools] interrupting runs left from before the restart: %v", err)
	} else if n > 0 {
		log.Printf("[tools] marked %d run(s) left from before the restart as interrupted", n)
	}
	sweep := time.NewTicker(SweepEvery)
	defer sweep.Stop()
	prune := time.NewTimer(time.Minute)
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sweep.C:
			if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[tools] sweep: %v", err)
			}
		case <-prune.C:
			if n, err := s.Prune(ctx); err != nil {
				log.Printf("[tools] prune: %v", err)
			} else if n > 0 {
				log.Printf("[tools] pruned %d finished run(s) past the retention period", n)
			}
			prune.Reset(pruneEvery)
		}
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `./scripts/test-db.sh -run 'TestDBNextJob|TestDBAgentEvents|TestDBAgentFinish|TestDBCancelAgentRun|TestDBSweep|TestDBInterruptStale|TestDBPrune|TestDBStart' -v`
Expected: PASS for the following:
- `TestDBNextJobClaims`, `TestDBNextJobClaimRace`, `TestDBNextJobWakesOnCreate` and `TestDBNextJobTimesOutAndRecordsPolls`
- `TestDBAgentEvents`, `TestDBAgentEventsCap`, `TestDBAgentFinish` and `TestDBCancelAgentRun`
- `TestDBSweepPickupTimeout` and `TestDBSweepTimesOutVanishedAgentRun`
- `TestDBInterruptStale`, `TestDBPrune` and `TestDBStart`

Then under the race detector: `./scripts/test-db.sh -race -run 'TestDB(NextJob|AgentEvents|CancelAgentRun|Sweep|Start)' -v`
Expected: PASS, no race reports.

- [ ] **Step 6: Vet and commit**

Run: `go vet ./... && go build ./...`
Expected: no output.

```bash
git add backend/internal/toolruns/
git commit -m "feat(tools): agent job dispatch and the run sweeper

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 11: API — user tool routes and the live stream

`RequireNetTools` and the coded error helpers come first, then the six user routes and the SSE handler. Handler DB tests **insert runs directly with `testdb.Exec`** through the `insertToolRun` helper below, rather than going through `Create`. That way they control the status, the owner and the stored events exactly. The one test of the create path goes through the real `Create`.

How the stream avoids gaps and duplicates (Review Focus 2):
1. Subscribe to the hub first.
2. Replay the stored events after `Last-Event-ID`.
3. Re-read the run.
4. If the run has ended, replay the rest and send `end` from the database.
5. Otherwise send a `status` frame, then forward live frames, skipping any `event` whose seq is ≤ the last seq sent.

The test for this is deterministic. The handler struct carries an unexported `afterReplay` hook, which runs between steps 2 and 3. The test uses it to land events in exactly that window: a late publish of an event already replayed, and a new event stored and published. The keep-alive interval is a field on the same struct, so tests can shorten it. This is how the "unexported var" from the brief is implemented.

**Files:**
- Create: `backend/internal/api/net_tools_access.go`
- Create: `backend/internal/api/tool_runs_handler.go`
- Test: `backend/internal/api/tool_runs_handler_test.go` (new, unit)
- Test: `backend/internal/api/tool_runs_handler_db_test.go` (new, DB; also holds the DB test helpers Task 12 reuses)
- Test: `backend/internal/api/tool_runs_stream_db_test.go` (new, DB; the SSE tests over a real `httptest.Server`)

**Interfaces:**
- Consumes (shared contract):
  - `toolruns`:
    - `Service`, `New(Deps)` and `Deps{DB, Settings, Audit, Hub}`
    - `(*Service).Vantages`, `AllowlistEmpty`, `Create`, `Get`, `Events`, `List`, `Cancel`, `NextJob`, `AgentEvents`, `AgentFinish` and `Sweep`
    - `Requester`, `CreateRequest`, `ListFilter`, `Refusal`, `AgentEvent`, `AgentFinish`, `Settings` and `SaveSettings`
    - `ErrRunNotFound`, `UserRatePerMinute`, `UserRateBurst` and `CodeForbidden`/`CodeInvalidParams`/`CodeTargetNotAllowed`/`CodeNotFound`
  - `stream`: `Hub`, `NewHub`, `(*Hub).Subscribe`/`Publish`, `Subscription.C`/`Close`, `NewWriter`, `(*Writer).Send`/`Comment`, `Message`, `KeepAlive`.
  - `models`: `ToolRun`, `ToolRunEvent`, `RawJSON`, `ToolRunActive`, the `ToolRun*` and `Vantage*` constants, `User.NetTools`, `ActionToolRunRefused` and `ResourceToolRun`.
  - Existing `api` code: `adminChecker`, `GetUserFromContext`, `actorFrom` (`report_builder_handler.go`), `auditRecorder` (`backup_handler.go`), `respondSuccess`, `respondInternal`, `NewRateLimiter(…).Middleware(name, ByUser)`.
  - Existing test helpers: `stubUsers` (`auth_middleware_test.go`) and `fakeAudit` (`site_handler_test.go`).
  - The hub frames (contract "Hub keys and frames"):
    - The key is the run id string.
    - `event` has `ID` = `strconv.Itoa(seq)` and `Data` = the `models.ToolRunEvent` JSON.
    - `status` has `Data` = `{"status": …}`.
    - `end` has `Data` = the final `models.ToolRun` JSON.
- Produces:
  - `const netToolsAdminKey = "net_tools_is_admin"`, plus `netToolsRefusedKey`, `codeInvalidAllowlist = "invalid_allowlist"` and `codeToolsDisabled = "tools_disabled"`.
  - `func respondToolError(c *gin.Context, status int, code, message string)`, which aborts.
  - `func RequireNetTools(users adminChecker) gin.HandlerFunc`, which answers 403 `forbidden` "network tools access required".
  - `func requesterFrom(c *gin.Context) toolruns.Requester` and `func writeRefusal(c *gin.Context, err error) bool`.
  - `func RegisterToolRoutes(rg *gin.RouterGroup, runs *toolruns.Service, hub *stream.Hub, users adminChecker, audit auditRecorder)` (Contract change 1).
  - `type toolRunsHandler struct{ runs; hub; keepAlive time.Duration; afterReplay func(uuid.UUID) }` and `func registerToolRoutes(rg *gin.RouterGroup, h *toolRunsHandler, users adminChecker, audit auditRecorder)`.
  - Test helpers for Task 12 (package `api`):
    - `testJWTSecret`
    - `newTestToolRuns(db, hub) *toolruns.Service`
    - `toolRunRow{status, user, agent, events, created, deadline}` and `insertToolRun(t, db, toolRunRow) uuid.UUID`
    - `registerToolAgent(t, db, name, enabled) *models.Agent`, which is returned with its token
    - `grantedUser(t, db) uuid.UUID`
    - `toolRequest(r, method, path, body) *httptest.ResponseRecorder`
    - `toolErrorBody(t, body) (code, message)` and `toolData(t, body, out)`

- [ ] **Step 1: Write the failing unit tests**

Create `backend/internal/api/tool_runs_handler_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// toolErrorBody decodes the coded error envelope of the tool endpoints.
func toolErrorBody(t *testing.T, body []byte) (code, message string) {
	t.Helper()
	var env struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	if env.Success {
		t.Errorf("body %s: success should be false", body)
	}
	return env.Error.Code, env.Error.Message
}

// guardedRouter mounts RequireNetTools in front of a handler that records
// the requester it was given. The token claims claimAdmin; users is what the
// database says.
func guardedRouter(users adminChecker, claimAdmin bool, ran *bool, who *toolruns.Requester) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", claimAdmin)
		c.Next()
	})
	r.GET("/thing", RequireNetTools(users), func(c *gin.Context) {
		*ran = true
		*who = requesterFrom(c)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

// A refused request must not reach the handler it guards, and the refusal is
// the coded 403 the frontend reads.
func TestRequireNetToolsDoesNotRunHandlerWhenRefused(t *testing.T) {
	cases := []struct {
		name       string
		claimAdmin bool
		users      stubUsers
		want       int
		wantAdmin  bool
	}{
		{"neither admin nor granted", false, stubUsers{user: &models.User{Username: "u"}}, http.StatusForbidden, false},
		{"admin claim the account no longer has", true, stubUsers{user: &models.User{Username: "u"}}, http.StatusForbidden, false},
		{"account cannot be read", true, stubUsers{err: context.DeadlineExceeded}, http.StatusForbidden, false},
		{"granted", false, stubUsers{user: &models.User{Username: "u", NetTools: true}}, http.StatusOK, false},
		{"admin without the grant", false, stubUsers{user: &models.User{Username: "u", IsAdmin: true}}, http.StatusOK, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran := false
			var who toolruns.Requester
			w := httptest.NewRecorder()
			guardedRouter(tc.users, tc.claimAdmin, &ran, &who).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/thing", nil))
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
			if ran != (tc.want == http.StatusOK) {
				t.Errorf("handler ran = %v: a refused request must not reach it", ran)
			}
			if tc.want == http.StatusForbidden {
				if code, msg := toolErrorBody(t, w.Body.Bytes()); code != "forbidden" || msg != "network tools access required" {
					t.Errorf("refusal = %q %q, want forbidden / network tools access required", code, msg)
				}
				return
			}
			// The admin flag handlers see comes from the database, not the
			// token's claim.
			if who.IsAdmin != tc.wantAdmin || who.Username != "someone" {
				t.Errorf("requester = %+v, want IsAdmin %v", who, tc.wantAdmin)
			}
		})
	}
}

// A grant an admin removes stops working on the very next request, however
// fresh the user's token.
func TestRequireNetToolsRereadsTheGrantEveryRequest(t *testing.T) {
	users := &stubUsers{user: &models.User{Username: "u", NetTools: true}}
	ran := false
	var who toolruns.Requester
	r := guardedRouter(users, false, &ran, &who)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/thing", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("with the grant: status %d", w.Code)
	}
	users.user = &models.User{Username: "u", NetTools: false}
	ran = false
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/thing", nil))
	if w.Code != http.StatusForbidden || ran {
		t.Errorf("after the grant was removed: status %d, handler ran %v; want 403 and not run", w.Code, ran)
	}
}

// toolRouter mounts the tool routes for one signed-in user, with no service
// behind them: these tests only reach paths that answer before it is needed.
func toolRouter(users adminChecker, audit auditRecorder) *gin.Engine {
	gin.SetMode(gin.TestMode)
	user := uuid.New()
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", user)
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	registerToolRoutes(v1, &toolRunsHandler{}, users, audit)
	return r
}

// toolRequest sends a raw JSON body.
func toolRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Every tool route is guarded. A refused run request is audited as
// tool_run_refused; refused reads are not, so they cannot flood the log.
func TestToolRoutesRefuseUsersWithoutTheGrant(t *testing.T) {
	audit := &fakeAudit{}
	r := toolRouter(stubUsers{user: &models.User{Username: "u"}}, audit)
	id := uuid.NewString()
	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/tools/vantages"},
		{http.MethodGet, "/api/v1/tools/runs"},
		{http.MethodGet, "/api/v1/tools/runs/" + id},
		{http.MethodGet, "/api/v1/tools/runs/" + id + "/events"},
		{http.MethodPost, "/api/v1/tools/runs/" + id + "/cancel"},
	} {
		if w := toolRequest(r, rt.method, rt.path, ""); w.Code != http.StatusForbidden {
			t.Errorf("%s %s: status %d, want 403", rt.method, rt.path, w.Code)
		}
	}
	if len(audit.entries) != 0 {
		t.Errorf("refused reads were audited: %v", audit.entries)
	}
	w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs", `{"tool":"ping","vantage":{"kind":"sentinel"},"target":"10.0.0.5"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("POST /tools/runs: status %d, want 403", w.Code)
	}
	if len(audit.entries) != 1 || audit.entries[0] != models.ActionToolRunRefused {
		t.Errorf("audit = %v, want one %s", audit.entries, models.ActionToolRunRefused)
	}
}

func TestCreateToolRunRejectsABodyThatIsNotJSON(t *testing.T) {
	r := toolRouter(stubUsers{user: &models.User{Username: "u", NetTools: true}}, &fakeAudit{})
	w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs", `{"tool":`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if code, _ := toolErrorBody(t, w.Body.Bytes()); code != "invalid_params" {
		t.Errorf("code %q, want invalid_params", code)
	}
}

// 30 runs a minute per user with a burst of 10: the eleventh request in a row
// is throttled before it reaches the handler.
func TestCreateToolRunIsRateLimited(t *testing.T) {
	r := toolRouter(stubUsers{user: &models.User{Username: "u", NetTools: true}}, &fakeAudit{})
	for i := 1; i <= 10; i++ {
		// Malformed on purpose: the handler answers 400 before it needs the
		// service, but each request still spends a token.
		if w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs", `{`); w.Code != http.StatusBadRequest {
			t.Fatalf("request %d: status %d, want 400", i, w.Code)
		}
	}
	if w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs", `{`); w.Code != http.StatusTooManyRequests {
		t.Errorf("request 11: status %d, want 429", w.Code)
	}
}

func TestWriteRefusalUsesTheRefusalsStatusAndCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	refusal := &toolruns.Refusal{Status: http.StatusUnprocessableEntity, Code: toolruns.CodeTargetNotAllowed,
		Message: "10.9.9.9 (10.9.9.9) is not on the network tools allowlist"}
	if !writeRefusal(c, refusal) {
		t.Fatal("a *Refusal was not written")
	}
	if w.Code != http.StatusUnprocessableEntity || !c.IsAborted() {
		t.Errorf("status %d, aborted %v; want 422 and aborted", w.Code, c.IsAborted())
	}
	if code, msg := toolErrorBody(t, w.Body.Bytes()); code != "target_not_allowed" || msg != refusal.Message {
		t.Errorf("body = %q %q", code, msg)
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	if writeRefusal(c, errors.New("database is down")) {
		t.Error("a plain error was answered as a refusal")
	}
}

func TestLastEventID(t *testing.T) {
	for raw, want := range map[string]int{"": 0, "7": 7, " 12 ": 12, "-3": 0, "abc": 0} {
		if got := lastEventID(raw); got != want {
			t.Errorf("lastEventID(%q) = %d, want %d", raw, got, want)
		}
	}
}
```

- [ ] **Step 2: Write the failing DB tests for the routes**

Create `backend/internal/api/tool_runs_handler_db_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// testJWTSecret satisfies NewAuthService's minimum length.
const testJWTSecret = "test-secret-test-secret-test-secret-0123"

// newTestToolRuns is the run service on db, publishing to hub.
func newTestToolRuns(db *gorm.DB, hub *stream.Hub) *toolruns.Service {
	return toolruns.New(toolruns.Deps{
		DB:       db,
		Settings: services.NewSettingsService(db),
		Audit:    services.NewAuditService(db),
		Hub:      hub,
	})
}

// toolRunRow is a run inserted straight into the database. Handler tests
// insert rows rather than going through Create, so they control the status,
// the owner and the stored events exactly.
type toolRunRow struct {
	status   string
	user     uuid.UUID
	agent    *models.Agent // nil: a run on the Sentinel server
	events   int           // events 1..events are stored, and event_count matches
	created  time.Time     // zero: now
	deadline time.Time     // zero: two minutes from now
}

// insertToolRun stores a ping run against 10.0.0.5 and returns its id.
func insertToolRun(t *testing.T, db *gorm.DB, r toolRunRow) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	created, deadline := r.created, r.deadline
	if created.IsZero() {
		created = now
	}
	if deadline.IsZero() {
		deadline = now.Add(2 * time.Minute)
	}
	kind, name := models.VantageSentinel, "Sentinel"
	var agentID *uuid.UUID
	var agentRef *string
	if r.agent != nil {
		kind, name, agentID, agentRef = models.VantageAgent, r.agent.Name, &r.agent.ID, &r.agent.AgentID
	}
	var started, finished *time.Time
	if r.status != models.ToolRunQueued {
		started = &now
	}
	if !models.ToolRunActive(r.status) {
		finished = &now
	}
	testdb.Exec(t, db, `INSERT INTO tool_runs (id, tool, status, user_id, username, vantage_kind, agent_id, agent_ref,
		vantage_name, target, target_ip, params, event_count, created_at, started_at, finished_at, deadline)
		VALUES (?, 'ping', ?, ?, 'someone', ?, ?, ?, ?, '10.0.0.5', '10.0.0.5',
		'{"count":5,"interval_ms":1000,"size":56,"timeout_ms":2000}', ?, ?, ?, ?, ?)`,
		id, r.status, r.user, kind, agentID, agentRef, name, r.events, created, started, finished, deadline)
	for seq := 1; seq <= r.events; seq++ {
		testdb.Exec(t, db, `INSERT INTO tool_run_events (run_id, seq, at, type, data) VALUES (?, ?, ?, 'reply', ?)`,
			id, seq, now, fmt.Sprintf(`{"seq":%d,"rtt_ms":1.5,"ttl":64,"from":"10.0.0.5"}`, seq))
	}
	return id
}

// registerToolAgent registers an agent that reports ENABLE_TOOLS and has
// Sentinel's switch set to enabled. It is returned with its token.
func registerToolAgent(t *testing.T, db *gorm.DB, name string, enabled bool) *models.Agent {
	t.Helper()
	ctx := context.Background()
	agents := services.NewAgentService(db)
	a := &models.Agent{Name: name, OSType: "linux", CheckInterval: 60, RetryAttempts: 3}
	testdb.Must(t, agents.Register(ctx, a))
	testdb.Exec(t, db, `UPDATE agents SET tools_enabled = ?, tools_local = true WHERE id = ?`, enabled, a.ID)
	got, err := agents.Get(ctx, a.AgentID)
	testdb.Must(t, err)
	return got
}

// grantedUser makes a non-admin holding the network tools grant.
func grantedUser(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	id := testdb.NewUser(t, db, false)
	testdb.Exec(t, db, `UPDATE users SET net_tools = true WHERE id = ?`, id)
	return id
}

// toolsRig is the tool routes on a real database, as seen by one user.
type toolsRig struct {
	db   *gorm.DB
	hub  *stream.Hub
	runs *toolruns.Service
	h    *toolRunsHandler
}

func newToolsRig(t *testing.T) *toolsRig {
	t.Helper()
	db := testdb.Open(t)
	hub := stream.NewHub(64)
	runs := newTestToolRuns(db, hub)
	return &toolsRig{db: db, hub: hub, runs: runs, h: &toolRunsHandler{runs: runs, hub: hub}}
}

// routerFor mounts the routes for user, with RequireNetTools reading the
// database. isAdmin is the token's claim.
func (rig *toolsRig) routerFor(t *testing.T, user uuid.UUID, isAdmin bool) *gin.Engine {
	t.Helper()
	var name string
	testdb.Must(t, rig.db.Raw(`SELECT username FROM users WHERE id = ?`, user).Scan(&name).Error)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", user)
		c.Set("username", name)
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	registerToolRoutes(v1, rig.h, services.NewAuthService(rig.db, testJWTSecret), services.NewAuditService(rig.db))
	return r
}

// toolData decodes a success envelope's data into out.
func toolData(t *testing.T, body []byte, out any) {
	t.Helper()
	var env struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || !env.Success {
		t.Fatalf("body %s: %v", body, err)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		t.Fatalf("data %s: %v", env.Data, err)
	}
}

// A refusal from the service reaches the browser in the coded shape, with
// its own status. The allowlist is empty out of the box, so nothing may run.
func TestDBCreateToolRunAnswersRefusalsInTheCodedShape(t *testing.T) {
	rig := newToolsRig(t)
	r := rig.routerFor(t, grantedUser(t, rig.db), false)
	w := toolRequest(r, http.MethodPost, "/api/v1/tools/runs",
		`{"tool":"ping","vantage":{"kind":"sentinel"},"target":"10.0.0.5","params":{}}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", w.Code, w.Body.String())
	}
	if code, msg := toolErrorBody(t, w.Body.Bytes()); code != "target_not_allowed" || msg == "" {
		t.Errorf("refusal = %q %q, want target_not_allowed with a message", code, msg)
	}
	var runs int64
	testdb.Must(t, rig.db.Raw(`SELECT count(*) FROM tool_runs`).Scan(&runs).Error)
	if runs != 0 {
		t.Errorf("%d runs stored for a refused request", runs)
	}
}

// A run from a ready agent is queued and returned as the API shows runs: the
// readable agent id under agent_id, the resolved address, the defaults filled.
func TestDBCreateToolRunQueuesAnAgentRun(t *testing.T) {
	rig := newToolsRig(t)
	ctx := context.Background()
	_, err := toolruns.SaveSettings(ctx, services.NewSettingsService(rig.db),
		toolruns.Settings{Allowlist: []string{"10.0.0.0/24"}, ServerEnabled: true, RetentionDays: 30})
	testdb.Must(t, err)
	agent := registerToolAgent(t, rig.db, "file-server", true)
	// One poll makes the agent ready (it counts for 60 s).
	if job, err := rig.runs.NextJob(ctx, agent, time.Millisecond); err != nil || job != nil {
		t.Fatalf("NextJob = %v, %v; want an empty poll", job, err)
	}
	user := grantedUser(t, rig.db)
	w := toolRequest(rig.routerFor(t, user, false), http.MethodPost, "/api/v1/tools/runs",
		`{"tool":"ping","vantage":{"kind":"agent","agent_id":"`+agent.AgentID+`"},"target":"10.0.0.5","params":{}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", w.Code, w.Body.String())
	}
	var run struct {
		Status      string    `json:"status"`
		Tool        string    `json:"tool"`
		UserID      uuid.UUID `json:"user_id"`
		VantageKind string    `json:"vantage_kind"`
		AgentID     string    `json:"agent_id"`
		VantageName string    `json:"vantage_name"`
		TargetIP    string    `json:"target_ip"`
		Params      struct {
			Count int `json:"count"`
		} `json:"params"`
	}
	toolData(t, w.Body.Bytes(), &run)
	if run.Status != "queued" || run.Tool != "ping" || run.UserID != user || run.VantageKind != "agent" ||
		run.AgentID != agent.AgentID || run.VantageName != "file-server" || run.TargetIP != "10.0.0.5" || run.Params.Count != 5 {
		t.Errorf("run = %+v", run)
	}
}

// Grant holders see every run; mine=1 narrows to their own. Newest first,
// with the total in X-Total-Count.
func TestDBListToolRunsFiltersAndCounts(t *testing.T) {
	rig := newToolsRig(t)
	alice, bob := grantedUser(t, rig.db), grantedUser(t, rig.db)
	older := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunDone, user: alice, created: time.Now().UTC().Add(-time.Hour)})
	newer := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunDone, user: bob})
	r := rig.routerFor(t, bob, false)

	w := toolRequest(r, http.MethodGet, "/api/v1/tools/runs", "")
	var all []struct {
		ID uuid.UUID `json:"id"`
	}
	toolData(t, w.Body.Bytes(), &all)
	if len(all) != 2 || all[0].ID != newer || all[1].ID != older || w.Header().Get("X-Total-Count") != "2" {
		t.Errorf("all runs = %+v, total %q; want newer then older, 2", all, w.Header().Get("X-Total-Count"))
	}

	w = toolRequest(r, http.MethodGet, "/api/v1/tools/runs?mine=1", "")
	var mine []struct {
		ID uuid.UUID `json:"id"`
	}
	toolData(t, w.Body.Bytes(), &mine)
	if len(mine) != 1 || mine[0].ID != newer || w.Header().Get("X-Total-Count") != "1" {
		t.Errorf("mine = %+v, total %q; want only bob's run", mine, w.Header().Get("X-Total-Count"))
	}

	if w := toolRequest(r, http.MethodGet, "/api/v1/tools/runs?limit=lots", ""); w.Code != http.StatusBadRequest {
		t.Errorf("limit=lots: status %d, want 400", w.Code)
	}
}

func TestDBGetToolRunReturnsTheRunWithItsEvents(t *testing.T) {
	rig := newToolsRig(t)
	user := grantedUser(t, rig.db)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunDone, user: user, events: 3})
	r := rig.routerFor(t, user, false)

	w := toolRequest(r, http.MethodGet, "/api/v1/tools/runs/"+id.String(), "")
	var detail struct {
		Run struct {
			ID     uuid.UUID `json:"id"`
			Status string    `json:"status"`
		} `json:"run"`
		Events []struct {
			Seq  int    `json:"seq"`
			Type string `json:"type"`
		} `json:"events"`
	}
	toolData(t, w.Body.Bytes(), &detail)
	if detail.Run.ID != id || detail.Run.Status != "done" || len(detail.Events) != 3 ||
		detail.Events[0].Seq != 1 || detail.Events[2].Seq != 3 || detail.Events[1].Type != "reply" {
		t.Errorf("detail = %+v", detail)
	}
	for _, path := range []string{"/api/v1/tools/runs/" + uuid.NewString(), "/api/v1/tools/runs/not-a-run"} {
		w := toolRequest(r, http.MethodGet, path, "")
		if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusNotFound || code != "not_found" {
			t.Errorf("%s: status %d code %q, want 404 not_found", path, w.Code, code)
		}
	}
}

// Grant holders cancel their own runs only; admins cancel any run; a run
// that has ended cannot be cancelled.
func TestDBCancelToolRunChecksOwnership(t *testing.T) {
	rig := newToolsRig(t)
	owner, other := grantedUser(t, rig.db), grantedUser(t, rig.db)
	admin := testdb.NewUser(t, rig.db, true)
	agent := registerToolAgent(t, rig.db, "file-server", true)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunQueued, user: owner, agent: agent})
	path := "/api/v1/tools/runs/" + id.String() + "/cancel"

	w := toolRequest(rig.routerFor(t, other, false), http.MethodPost, path, "")
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusForbidden || code != "forbidden" {
		t.Fatalf("another user's cancel: status %d code %q, want 403 forbidden", w.Code, code)
	}
	var status string
	testdb.Must(t, rig.db.Raw(`SELECT status FROM tool_runs WHERE id = ?`, id).Scan(&status).Error)
	if status != models.ToolRunQueued {
		t.Fatalf("status %q after a refused cancel, want queued", status)
	}

	w = toolRequest(rig.routerFor(t, admin, true), http.MethodPost, path, "")
	var run struct {
		Status string `json:"status"`
	}
	toolData(t, w.Body.Bytes(), &run)
	if run.Status != models.ToolRunCancelled {
		t.Errorf("admin cancel: status %q, want cancelled", run.Status)
	}

	w = toolRequest(rig.routerFor(t, owner, false), http.MethodPost, path, "")
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusConflict || code != "conflict" {
		t.Errorf("cancelling an ended run: status %d code %q, want 409 conflict", w.Code, code)
	}
}

// The Tools page reads both lists from one call: grant holders cannot read
// the settings, so the empty-allowlist flag comes with the vantages.
func TestDBVantagesComeWithTheAllowlistFlag(t *testing.T) {
	rig := newToolsRig(t)
	w := toolRequest(rig.routerFor(t, grantedUser(t, rig.db), false), http.MethodGet, "/api/v1/tools/vantages", "")
	var got struct {
		Vantages []struct {
			Kind  string `json:"kind"`
			Ready bool   `json:"ready"`
		} `json:"vantages"`
		AllowlistEmpty bool `json:"allowlist_empty"`
	}
	toolData(t, w.Body.Bytes(), &got)
	if len(got.Vantages) == 0 || got.Vantages[0].Kind != "sentinel" || !got.Vantages[0].Ready || !got.AllowlistEmpty {
		t.Errorf("vantages = %+v, want Sentinel first and ready, and allowlist_empty", got)
	}
}
```

- [ ] **Step 3: Write the failing SSE tests (real streaming)**

These tests read a real `httptest.Server` response body frame by frame while the handler is still writing it, which a recorder inspected after the fact cannot do. Create `backend/internal/api/tool_runs_stream_db_test.go`:

```go
package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// sseFrame is one frame as a browser's EventSource would see it.
type sseFrame struct {
	id, event, data, comment string
}

// sseStream reads a real streamed response frame by frame, as the server
// flushes them, rather than inspecting a finished recording.
type sseStream struct {
	resp   *http.Response
	frames chan sseFrame
}

// openSSE connects to a run's event stream on srv.
func openSSE(t *testing.T, srv *httptest.Server, runID uuid.UUID, lastEventID string) *sseStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/tools/runs/"+runID.String()+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	s := &sseStream{resp: resp, frames: make(chan sseFrame, 64)}
	go s.read()
	return s
}

// read parses frames until the server closes the stream.
func (s *sseStream) read() {
	defer close(s.frames)
	r := bufio.NewReader(s.resp.Body)
	var f sseFrame
	seen := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			if seen {
				s.frames <- f
			}
			f, seen = sseFrame{}, false
			continue
		}
		seen = true
		if strings.HasPrefix(line, ":") {
			f.comment = strings.TrimSpace(line[1:])
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			f.id = value
		case "event":
			f.event = value
		case "data":
			if f.data != "" {
				f.data += "\n"
			}
			f.data += value
		}
	}
}

// next returns the next frame other than a keep-alive comment.
func (s *sseStream) next(t *testing.T) sseFrame {
	t.Helper()
	for {
		f := s.nextAny(t)
		if f.comment == "" {
			return f
		}
	}
}

// nextAny returns the next frame, comments included.
func (s *sseStream) nextAny(t *testing.T) sseFrame {
	t.Helper()
	select {
	case f, ok := <-s.frames:
		if !ok {
			t.Fatal("the stream closed early")
		}
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("no frame within 3 s")
	}
	return sseFrame{}
}

// closed checks the server ends the stream with nothing more sent.
func (s *sseStream) closed(t *testing.T) {
	t.Helper()
	select {
	case f, ok := <-s.frames:
		if ok {
			t.Fatalf("frame after end: %+v", f)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream stayed open after end")
	}
}

// expectEvent checks f is the event frame for seq.
func expectEvent(t *testing.T, f sseFrame, seq int) {
	t.Helper()
	var ev struct {
		Seq  int    `json:"seq"`
		Type string `json:"type"`
	}
	if f.event != "event" || f.id != strconv.Itoa(seq) || json.Unmarshal([]byte(f.data), &ev) != nil || ev.Seq != seq {
		t.Fatalf("frame %+v, want the event frame for seq %d", f, seq)
	}
}

// expectEnd checks f is the end frame of a run in status.
func expectEnd(t *testing.T, f sseFrame, status string) {
	t.Helper()
	var run struct {
		Status string `json:"status"`
	}
	if f.event != "end" || json.Unmarshal([]byte(f.data), &run) != nil || run.Status != status {
		t.Fatalf("frame %+v, want the end frame with status %s", f, status)
	}
}

// expectStatus checks f is a status frame.
func expectStatus(t *testing.T, f sseFrame, status string) {
	t.Helper()
	var body struct {
		Status string `json:"status"`
	}
	if f.event != "status" || json.Unmarshal([]byte(f.data), &body) != nil || body.Status != status {
		t.Fatalf("frame %+v, want a status frame %s", f, status)
	}
}

// streamServer serves the rig's routes for user over a real HTTP server.
func (rig *toolsRig) streamServer(t *testing.T, user uuid.UUID) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(rig.routerFor(t, user, false))
	t.Cleanup(srv.Close)
	return srv
}

// A run that has ended replays every stored event, then its end frame, and
// the server closes the stream. The headers let the stream through nginx.
func TestDBRunStreamReplaysAnEndedRunAndCloses(t *testing.T) {
	rig := newToolsRig(t)
	user := grantedUser(t, rig.db)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunDone, user: user, events: 3})
	s := openSSE(t, rig.streamServer(t, user), id, "")

	if got := s.resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}
	if got := s.resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := s.resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	for seq := 1; seq <= 3; seq++ {
		expectEvent(t, s.next(t), seq)
	}
	expectEnd(t, s.next(t), models.ToolRunDone)
	s.closed(t)
}

// A reconnecting EventSource sends Last-Event-ID; the replay starts after it.
func TestDBRunStreamResumesAfterLastEventID(t *testing.T) {
	rig := newToolsRig(t)
	user := grantedUser(t, rig.db)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunDone, user: user, events: 4})
	s := openSSE(t, rig.streamServer(t, user), id, "2")

	expectEvent(t, s.next(t), 3)
	expectEvent(t, s.next(t), 4)
	expectEnd(t, s.next(t), models.ToolRunDone)
	s.closed(t)
}

func TestDBRunStreamAnswers404ForAnUnknownRun(t *testing.T) {
	rig := newToolsRig(t)
	user := grantedUser(t, rig.db)
	srv := rig.streamServer(t, user)
	resp, err := srv.Client().Get(srv.URL + "/api/v1/tools/runs/" + uuid.NewString() + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}

// Review Focus 2: a second tab, or a reconnect, mid-run. Events land in the
// window between the replay query and the switch to live events: event 2
// was stored before the replay but its publish arrives only now, and event 3
// is stored and published after the replay. The browser sees 1, 2, 3, then
// the live 4 and the end, each exactly once and in order.
func TestDBRunStreamHasNoGapOrDuplicateAcrossTheReplay(t *testing.T) {
	rig := newToolsRig(t)
	ctx := context.Background()
	user := grantedUser(t, rig.db)
	agent := registerToolAgent(t, rig.db, "file-server", true)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: user, agent: agent, events: 2})
	event := func(seq int) toolruns.AgentEvent {
		return toolruns.AgentEvent{Seq: seq, At: time.Now().UTC(), Type: "reply",
			Data: json.RawMessage(`{"seq":` + strconv.Itoa(seq) + `,"rtt_ms":1.5,"ttl":64,"from":"10.0.0.5"}`)}
	}
	rig.h.afterReplay = func(runID uuid.UUID) {
		late, err := json.Marshal(models.ToolRunEvent{Seq: 2, At: time.Now().UTC(), Type: "reply",
			Data: models.RawJSON(`{"seq":2,"rtt_ms":1.5,"ttl":64,"from":"10.0.0.5"}`)})
		if err != nil {
			t.Error(err)
		}
		rig.hub.Publish(runID.String(), stream.Message{ID: "2", Event: "event", Data: late})
		if _, err := rig.runs.AgentEvents(ctx, agent, runID, []toolruns.AgentEvent{event(3)}); err != nil {
			t.Errorf("storing event 3: %v", err)
		}
	}
	s := openSSE(t, rig.streamServer(t, user), id, "")

	expectEvent(t, s.next(t), 1)
	expectEvent(t, s.next(t), 2)
	expectStatus(t, s.next(t), models.ToolRunRunning)
	expectEvent(t, s.next(t), 3)

	if _, err := rig.runs.AgentEvents(ctx, agent, id, []toolruns.AgentEvent{event(4)}); err != nil {
		t.Fatal(err)
	}
	expectEvent(t, s.next(t), 4)
	if err := rig.runs.AgentFinish(ctx, agent, id, toolruns.AgentFinish{Status: models.ToolRunDone,
		Summary: json.RawMessage(`{"sent":4,"received":4,"loss_pct":0}`)}); err != nil {
		t.Fatal(err)
	}
	expectEnd(t, s.next(t), models.ToolRunDone)
	s.closed(t)
}

// Review Focus 1: an agent vanishes after claiming a run. The sweeper ends
// the run as timed_out at deadline + 15 s, and the end frame reaches the
// open stream, which then closes, so the page stops showing "Running".
func TestDBRunStreamEndsWhenTheSweeperTimesTheRunOut(t *testing.T) {
	rig := newToolsRig(t)
	rig.h.keepAlive = 20 * time.Millisecond
	user := grantedUser(t, rig.db)
	agent := registerToolAgent(t, rig.db, "file-server", true)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: user, agent: agent,
		deadline: time.Now().UTC().Add(-time.Minute)})
	s := openSSE(t, rig.streamServer(t, user), id, "")

	// The status frame is sent after the handler re-read the run as running,
	// so from here on only the hub can end the stream.
	expectStatus(t, s.next(t), models.ToolRunRunning)
	if f := s.nextAny(t); f.comment != "keep-alive" {
		t.Fatalf("frame %+v, want a keep-alive comment while the run is quiet", f)
	}
	if err := rig.runs.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	expectEnd(t, s.next(t), models.ToolRunTimedOut)
	s.closed(t)
}
```

- [ ] **Step 4: Run the unit tests to verify they fail**

Run (from `backend/`): `go test ./internal/api/ -run 'TestRequireNetTools|TestToolRoutes|TestCreateToolRun|TestWriteRefusal|TestLastEventID' -v`
Expected: the build fails with `undefined: RequireNetTools`, `undefined: toolRunsHandler`, `undefined: registerToolRoutes`, `undefined: requesterFrom`, `undefined: writeRefusal` and `undefined: lastEventID`.

- [ ] **Step 5: Write the access helpers**

Create `backend/internal/api/net_tools_access.go`:

```go
package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// netToolsAdminKey holds the admin flag RequireNetTools read from the
// database, so a tool handler judges "admin" by the account as it is now
// rather than by the token's claim, which can be a day old.
const netToolsAdminKey = "net_tools_is_admin"

// netToolsRefusedKey marks a request RequireNetTools turned away, for
// auditToolRefusal.
const netToolsRefusedKey = "net_tools_refused"

// Codes the tool endpoints answer beyond the toolruns ones.
const (
	codeInvalidAllowlist = "invalid_allowlist"
	codeToolsDisabled    = "tools_disabled"
)

const msgNetToolsRequired = "network tools access required"

// respondToolError writes the coded error envelope of the tool endpoints,
// {"success": false, "error": {"code", "message"}}, and aborts, so it is safe
// in middleware as well as in handlers.
func respondToolError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"success": false,
		"error":   gin.H{"code": code, "message": message},
	})
}

// RequireNetTools lets admins and users granted network tools through, and
// answers everyone else 403 forbidden. Mount it after AuthMiddleware.
//
// The account is re-read on every request, as RequireAdmin does: a token is
// valid for a day, and a grant an admin removes must stop working at once.
// The admin flag read here is stored under netToolsAdminKey.
func RequireNetTools(users adminChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, _, ok := GetUserFromContext(c)
		if !ok {
			refuseNetTools(c)
			return
		}
		user, err := users.GetUserByID(c.Request.Context(), userID)
		if err != nil {
			// The grant cannot be confirmed, so the request does not go on:
			// failing open here would defeat the check.
			log.Printf("[tools] access check for %s failed: %v", userID, err)
			refuseNetTools(c)
			return
		}
		if !user.IsAdmin && !user.NetTools {
			refuseNetTools(c)
			return
		}
		c.Set(netToolsAdminKey, user.IsAdmin)
		c.Next()
	}
}

func refuseNetTools(c *gin.Context) {
	c.Set(netToolsRefusedKey, true)
	respondToolError(c, http.StatusForbidden, toolruns.CodeForbidden, msgNetToolsRequired)
}

// auditToolRefusal records a run request that RequireNetTools refused: the
// spec audits runs "refused for permission reasons". It is mounted ahead of
// the guard on POST /tools/runs only, so refused reads cannot flood the log.
func auditToolRefusal(audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if audit == nil || !c.GetBool(netToolsRefusedKey) {
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionToolRunRefused, models.ResourceToolRun, nil,
			models.AuditChanges{Summary: map[string]any{"reason": msgNetToolsRequired}})
	}
}

// requesterFrom is who is asking, for the tool-run service. IsAdmin is the
// flag RequireNetTools read from the database.
func requesterFrom(c *gin.Context) toolruns.Requester {
	userID, username, _, _ := GetUserFromContext(c)
	return toolruns.Requester{
		UserID:   userID,
		Username: username,
		IsAdmin:  c.GetBool(netToolsAdminKey),
		IP:       c.ClientIP(),
	}
}

// writeRefusal answers a *toolruns.Refusal in the coded shape and reports
// whether err was one.
func writeRefusal(c *gin.Context, err error) bool {
	var r *toolruns.Refusal
	if !errors.As(err, &r) {
		return false
	}
	respondToolError(c, r.Status, r.Code, r.Message)
	return true
}
```

- [ ] **Step 6: Write the routes and the stream handler**

Create `backend/internal/api/tool_runs_handler.go`:

```go
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// The SSE frames of a run stream (shared contract: "Hub keys and frames").
const (
	sseEvent  = "event"
	sseStatus = "status"
	sseEnd    = "end"
)

const msgNoSuchRun = "no such tool run"

// toolRunsHandler serves the user side of network tools.
type toolRunsHandler struct {
	runs *toolruns.Service
	// hub is the one the service publishes to; streams subscribe to it.
	hub *stream.Hub
	// keepAlive is how often an idle stream sends a comment
	// (stream.KeepAlive; tests shorten it).
	keepAlive time.Duration
	// afterReplay, when set, runs between the replay of stored events and
	// the switch to live ones. Tests use it to land events in that window.
	afterReplay func(runID uuid.UUID)
}

// RegisterToolRoutes mounts the network-tools routes on the authenticated v1
// group. Every route requires RequireNetTools; starting a run is also rate
// limited per user, and a run refused for permission reasons is audited.
func RegisterToolRoutes(rg *gin.RouterGroup, runs *toolruns.Service, hub *stream.Hub, users adminChecker, audit auditRecorder) {
	registerToolRoutes(rg, &toolRunsHandler{runs: runs, hub: hub, keepAlive: stream.KeepAlive}, users, audit)
}

func registerToolRoutes(rg *gin.RouterGroup, h *toolRunsHandler, users adminChecker, audit auditRecorder) {
	if h.keepAlive <= 0 {
		h.keepAlive = stream.KeepAlive
	}
	guard := RequireNetTools(users)
	// 30 runs a minute per user, burst 10. Its 429 carries the limiter's
	// plain error string (ruling 11); the per-target limit and the active-run
	// caps answer 429 with code "limit" from the service.
	limit := NewRateLimiter(toolruns.UserRatePerMinute, time.Minute, toolruns.UserRateBurst).Middleware("tool-runs", ByUser)

	g := rg.Group("/tools")
	g.GET("/vantages", guard, h.vantages)
	g.POST("/runs", auditToolRefusal(audit), guard, limit, h.create)
	g.GET("/runs", guard, h.list)
	g.GET("/runs/:id", guard, h.get)
	g.GET("/runs/:id/events", guard, h.follow)
	g.POST("/runs/:id/cancel", guard, h.cancel)
}

// vantages handles GET /tools/vantages: where a run can start from, and
// whether the allowlist is empty (grant holders cannot read the settings).
func (h *toolRunsHandler) vantages(c *gin.Context) {
	ctx := c.Request.Context()
	list, err := h.runs.Vantages(ctx)
	if err != nil {
		respondInternal(c, "listing tool vantages", err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"vantages": list, "allowlist_empty": h.runs.AllowlistEmpty(ctx)})
}

// create handles POST /tools/runs.
func (h *toolRunsHandler) create(c *gin.Context) {
	var req toolruns.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "invalid request body")
		return
	}
	run, err := h.runs.Create(c.Request.Context(), requesterFrom(c), req)
	if err != nil {
		if !writeRefusal(c, err) {
			respondInternal(c, "creating a tool run", err)
		}
		return
	}
	respondSuccess(c, http.StatusCreated, run)
}

// list handles GET /tools/runs: the history, newest first, with the total in
// X-Total-Count. mine=1 narrows it to the caller's own runs.
func (h *toolRunsHandler) list(c *gin.Context) {
	f := toolruns.ListFilter{
		Tool:    c.Query("tool"),
		AgentID: c.Query("agent_id"),
		Target:  strings.TrimSpace(c.Query("target")),
		Status:  c.Query("status"),
	}
	if mine := c.Query("mine"); mine == "1" || mine == "true" {
		id := requesterFrom(c).UserID
		f.UserID = &id
	} else if raw := c.Query("user_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "user_id must be a user id")
			return
		}
		f.UserID = &id
	}
	var ok bool
	if f.Limit, ok = toolQueryInt(c, "limit"); !ok {
		return
	}
	if f.Offset, ok = toolQueryInt(c, "offset"); !ok {
		return
	}
	runs, total, err := h.runs.List(c.Request.Context(), f)
	if err != nil {
		respondInternal(c, "listing tool runs", err)
		return
	}
	if runs == nil {
		runs = []models.ToolRun{}
	}
	c.Header("X-Total-Count", strconv.FormatInt(total, 10))
	respondSuccess(c, http.StatusOK, runs)
}

// toolQueryInt reads an optional whole-number query value, 0 when absent. It
// answers 400 itself when the value is malformed.
func toolQueryInt(c *gin.Context, name string) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, name+" must be a whole number")
		return 0, false
	}
	return n, true
}

// get handles GET /tools/runs/:id: the run with all its stored events.
func (h *toolRunsHandler) get(c *gin.Context) {
	id, ok := runIDParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	run, err := h.runs.Get(ctx, id)
	if err != nil {
		respondRunError(c, "reading a tool run", err)
		return
	}
	events, err := h.runs.Events(ctx, id, 0)
	if err != nil {
		respondInternal(c, "reading tool run events", err)
		return
	}
	if events == nil {
		events = []models.ToolRunEvent{}
	}
	respondSuccess(c, http.StatusOK, gin.H{"run": run, "events": events})
}

// cancel handles POST /tools/runs/:id/cancel: the owner's own run, or any
// run for an admin.
func (h *toolRunsHandler) cancel(c *gin.Context) {
	id, ok := runIDParam(c)
	if !ok {
		return
	}
	run, err := h.runs.Cancel(c.Request.Context(), requesterFrom(c), id)
	if err != nil {
		respondRunError(c, "cancelling a tool run", err)
		return
	}
	respondSuccess(c, http.StatusOK, run)
}

// runIDParam reads :id. A malformed id names no run, so it is a 404.
func runIDParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondToolError(c, http.StatusNotFound, toolruns.CodeNotFound, msgNoSuchRun)
		return uuid.Nil, false
	}
	return id, true
}

// respondRunError answers a refusal in its own shape, a missing run as 404
// and anything else as a logged 500.
func respondRunError(c *gin.Context, op string, err error) {
	if writeRefusal(c, err) {
		return
	}
	if errors.Is(err, toolruns.ErrRunNotFound) {
		respondToolError(c, http.StatusNotFound, toolruns.CodeNotFound, msgNoSuchRun)
		return
	}
	respondInternal(c, op, err)
}

// follow handles GET /tools/runs/:id/events: the run's events as
// Server-Sent Events, replayed from the database and then followed live.
//
// No gap and no duplicate (Review Focus 2). The hub subscription is taken
// before anything is read, so every event published from then on reaches
// it, and every event stored before the replay query is in the replay; an
// event in both is skipped by its seq. The run is re-read after the replay:
// a run that has ended gets the rest of its stored events and its end frame
// from the database, and one still active ends with the hub's end frame,
// which the service publishes when the run reaches a final status
// (including the sweeper's timed_out, Review Focus 1).
func (h *toolRunsHandler) follow(c *gin.Context) {
	id, ok := runIDParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if _, err := h.runs.Get(ctx, id); err != nil {
		respondRunError(c, "opening a tool run stream", err)
		return
	}
	sent := lastEventID(c.GetHeader("Last-Event-ID"))

	sub := h.hub.Subscribe(id.String())
	defer sub.Close()

	w, err := stream.NewWriter(c.Writer)
	if err != nil {
		respondInternal(c, "opening a tool run stream", err)
		return
	}
	replay := func() bool {
		events, err := h.runs.Events(ctx, id, sent)
		if err != nil {
			log.Printf("[tools] replaying run %s: %v", id, err)
			return false
		}
		for _, ev := range events {
			data, err := json.Marshal(ev)
			if err != nil || w.Send(stream.Message{ID: strconv.Itoa(ev.Seq), Event: sseEvent, Data: data}) != nil {
				return false
			}
			sent = ev.Seq
		}
		return true
	}
	if !replay() {
		return
	}
	if h.afterReplay != nil {
		h.afterReplay(id)
	}

	run, err := h.runs.Get(ctx, id)
	if err != nil {
		log.Printf("[tools] re-reading run %s: %v", id, err)
		return
	}
	if !models.ToolRunActive(run.Status) {
		if replay() {
			_ = sendSSEJSON(w, sseEnd, run)
		}
		return
	}
	// Where the run stands now, so a page that connects after the agent
	// picked the run up does not wait for a change that already happened.
	if sendSSEJSON(w, sseStatus, gin.H{"status": run.Status}) != nil {
		return
	}

	keepAlive := time.NewTicker(h.keepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			if w.Comment("keep-alive") != nil {
				return
			}
		case m, ok := <-sub.C():
			if !ok {
				// Dropped for falling behind: the browser reconnects with
				// Last-Event-ID and replays from the database.
				return
			}
			if m.Event == sseEvent {
				seq, err := strconv.Atoi(m.ID)
				if err != nil || seq <= sent {
					continue
				}
				sent = seq
			}
			if w.Send(m) != nil || m.Event == sseEnd {
				return
			}
		}
	}
}

// sendSSEJSON sends v as one frame without an id.
func sendSSEJSON(w *stream.Writer, event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.Send(stream.Message{Event: event, Data: data})
}

// lastEventID is the seq a reconnecting EventSource saw last, 0 if none.
func lastEventID(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0
	}
	return n
}
```

- [ ] **Step 7: Run the unit tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestRequireNetTools|TestToolRoutes|TestCreateToolRun|TestWriteRefusal|TestLastEventID|TestRequireAdmin' -v`
Expected: PASS. One thing to check if `TestCreateToolRunIsRateLimited` fails with "request 11: status 400": `toolRouter` must give every request the same user id, because the limiter keys on the user.

- [ ] **Step 8: Run the DB tests to verify they pass**

Run: `./scripts/test-db.sh -run 'TestDBCreateToolRun|TestDBListToolRuns|TestDBGetToolRun|TestDBCancelToolRun|TestDBVantagesComeWith|TestDBRunStream' -v`
Expected: PASS, including:
- `TestDBRunStreamHasNoGapOrDuplicateAcrossTheReplay`, which pins Review Focus 2.
- `TestDBRunStreamEndsWhenTheSweeperTimesTheRunOut`, which pins Review Focus 1.

Each stream test finishes in well under a second. If a stream test hangs for 3 s on "no frame", the cause is in Tasks 9–10: check the frame format they publish (the `event` ID must be the bare seq) and that `Sweep`/`AgentFinish` publish `end`.

- [ ] **Step 9: Vet**

Run: `go vet ./internal/api/`
Expected: no output.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/api/net_tools_access.go backend/internal/api/tool_runs_handler.go \
  backend/internal/api/tool_runs_handler_test.go backend/internal/api/tool_runs_handler_db_test.go \
  backend/internal/api/tool_runs_stream_db_test.go
git commit -m "feat(tools): tool run API, RequireNetTools and the live SSE stream

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: API — agent jobs, admin routes and wiring

The agent job routes go beside the existing ingest routes: they use the agent's bearer token (`RequireAgentToken`) and `requireOwnAgent`. A test can shorten the long-poll wait through the unexported `registerAgentJobRoutes(router, agents, runs, wait)`. The admin routes are mounted on the existing `RequireAdmin` group, and each change is audited. `main.go` does the following:
- builds the hub and the run service;
- mounts the three route sets and starts the sweeper loop;
- adds ruling 13's `BaseContext`, so open streams and long polls end at once on shutdown.

Every DB test here reuses Task 11's helpers (`insertToolRun`, `registerToolAgent`, `grantedUser`, `newTestToolRuns`, `toolRequest`, `toolErrorBody`, `toolData`, `testJWTSecret`).

**Files:**
- Create: `backend/internal/api/agent_jobs_handler.go`
- Create: `backend/internal/api/net_tools_admin_handler.go`
- Modify: `backend/cmd/sentinel/main.go` (imports, a constant, service construction after `agentService`, three route registrations, the sweeper goroutine, the HTTP server's `BaseContext`, shutdown)
- Test: `backend/internal/api/agent_jobs_handler_db_test.go` (new, DB)
- Test: `backend/internal/api/net_tools_admin_handler_db_test.go` (new, DB)

**Interfaces:**
- Consumes:
  - From the shared contract:
    - `toolruns`: `NextJob`, `AgentEvents`, `AgentFinish`, `Cancel`, `Job`, `AgentEvent`, `AgentFinish` (type), `ErrToolsDisabled`, `ErrRunNotActive`, `ErrTooMuchOutput`, `LongPollWait`, `MaxAgentPostBytes`, `LoadSettings`, `SaveSettings`, `Settings`, `SettingsError{Message, Entries}`, `New`/`Deps`, `(*Service).Start` and `CodeLimit`/`CodeConflict`/`CodeInvalidParams`/`CodeNotFound`.
    - `nettools.EntryError{Entry, Message}`.
    - `services`: `(*AuthService).SetNetTools(ctx, userID, enabled) (*models.User, error)` and `(*AgentService).SetToolsEnabled(ctx, agentID, enabled) (*models.Agent, error)` (which returns `ErrAgentNotFound`).
    - `models`: `ActionNetToolsSettingsUpdated`, `ActionUserNetToolsChanged`, `ActionAgentToolsChanged`, `ResourceSettings`, `ResourceUser`, `ResourceAgent`, `Agent.ToolsEnabled` and the `ToolRun*` status constants.
    - `stream.NewHub`.
  - From Task 11: `respondToolError`, `codeToolsDisabled`, `codeInvalidAllowlist`, `RegisterToolRoutes` and the test helpers listed there.
  - Existing `api` code: `RequireAgentToken`, `requireOwnAgent`, `RequireAdmin`, `actorFrom`, `auditRecorder`, `truncate` (`agent_handler.go`), `respondSuccess` and `respondInternal`.
- Produces:
  - `func RegisterAgentJobRoutes(router *gin.Engine, agents *services.AgentService, runs *toolruns.Service)` and the unexported `registerAgentJobRoutes(…, wait time.Duration)`.
  - The agent wire format, which Task 13 relies on:
    - `GET /api/v1/agents/:agent_id/jobs/next` answers one of:
      - `200 {"success":true,"data":{"run_id","spec","deadline"}}`
      - `204` with no body
      - `403 {"success":false,"error":{"code":"tools_disabled",…}}`
      - `403` with a numeric code when the token belongs to another agent (`requireOwnAgent`)
    - `POST /api/v1/agents/:agent_id/jobs/:run_id/events` takes the body `{"events":[{"seq","at","type","data"}…]}`; an empty list is valid. It answers one of:
      - `200 {"data":{"cancel":bool}}`
      - `409` when the run is not this agent's, or is not running
      - `413` code `limit`, for a body over 64 KB or for too much output
      - `400` for a bad body
    - `POST /api/v1/agents/:agent_id/jobs/:run_id/finish` takes the body `{"status":"done|failed|refused","summary":…,"error":"…"}`. It answers `200 {"data":{"ok":true}}`, `409` or `400`.
  - `func RegisterNetToolsAdminRoutes(admin *gin.RouterGroup, settings *services.SettingsService, auth *services.AuthService, agents *services.AgentService, audit auditRecorder)`, which serves:
    - `GET`/`PUT /settings/net-tools`; a PUT with bad entries answers `422 {"error":{"code":"invalid_allowlist",…},"entries":[{entry,message}]}`
    - `PATCH /users/:id/net-tools`, which answers `{id, net_tools}`
    - `PUT /agents/:agent_id/tools`, which answers the agent with its token hidden

- [ ] **Step 1: Write the failing agent-job DB tests**

Create `backend/internal/api/agent_jobs_handler_db_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// jobsRig is the agent job routes on a real database with a short long-poll.
// agent has Sentinel's tools switch on, other has it off.
type jobsRig struct {
	db     *gorm.DB
	runs   *toolruns.Service
	router *gin.Engine
	agent  *models.Agent
	other  *models.Agent
	user   uuid.UUID
}

func newJobsRig(t *testing.T, wait time.Duration) *jobsRig {
	t.Helper()
	db := testdb.Open(t)
	runs := newTestToolRuns(db, stream.NewHub(64))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerAgentJobRoutes(r, services.NewAgentService(db), runs, wait)
	return &jobsRig{
		db: db, runs: runs, router: r,
		agent: registerToolAgent(t, db, "file-server", true),
		other: registerToolAgent(t, db, "print-server", false),
		user:  grantedUser(t, db),
	}
}

// call sends an agent request with token.
func (rig *jobsRig) call(token, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	rig.router.ServeHTTP(w, req)
	return w
}

func jobPath(a *models.Agent, run uuid.UUID, what string) string {
	return "/api/v1/agents/" + a.AgentID + "/jobs/" + run.String() + "/" + what
}

func nextPath(a *models.Agent) string { return "/api/v1/agents/" + a.AgentID + "/jobs/next" }

// runStatus reads a run's status and error.
func (rig *jobsRig) runStatus(t *testing.T, id uuid.UUID) (string, string) {
	t.Helper()
	var row struct {
		Status string
		Error  *string
	}
	testdb.Must(t, rig.db.Raw(`SELECT status, error FROM tool_runs WHERE id = ?`, id).Scan(&row).Error)
	if row.Error == nil {
		return row.Status, ""
	}
	return row.Status, *row.Error
}

func (rig *jobsRig) eventCount(t *testing.T, id uuid.UUID) (stored, counted int) {
	t.Helper()
	testdb.Must(t, rig.db.Raw(`SELECT count(*) FROM tool_run_events WHERE run_id = ?`, id).Scan(&stored).Error)
	testdb.Must(t, rig.db.Raw(`SELECT event_count FROM tool_runs WHERE id = ?`, id).Scan(&counted).Error)
	return stored, counted
}

const twoEvents = `{"events":[
	{"seq":1,"at":"2026-10-05T12:00:00Z","type":"reply","data":{"seq":1,"rtt_ms":1.2,"ttl":64,"from":"10.0.0.5"}},
	{"seq":2,"at":"2026-10-05T12:00:01Z","type":"reply","data":{"seq":2,"rtt_ms":1.4,"ttl":64,"from":"10.0.0.5"}}]}`

// With nothing queued, a poll is held for the wait and then answered 204.
func TestDBJobsNextAnswers204AfterTheWait(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	start := time.Now()
	w := rig.call(rig.agent.ServerToken, http.MethodGet, nextPath(rig.agent), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204: %s", w.Code, w.Body.String())
	}
	if held := time.Since(start); held < 50*time.Millisecond {
		t.Errorf("the poll was answered after %s, want it held for the 50 ms wait", held)
	}
}

func TestDBJobsNextHandsOutTheQueuedRun(t *testing.T) {
	rig := newJobsRig(t, time.Second)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunQueued, user: rig.user, agent: rig.agent})
	w := rig.call(rig.agent.ServerToken, http.MethodGet, nextPath(rig.agent), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	var job toolruns.Job
	toolData(t, w.Body.Bytes(), &job)
	if job.RunID != id || job.Spec.Tool != "ping" || job.Spec.Target != "10.0.0.5" || job.Spec.TargetIP != "10.0.0.5" ||
		job.Spec.Params.Count != 5 || !job.Deadline.After(time.Now()) {
		t.Errorf("job = %+v", job)
	}
	if status, _ := rig.runStatus(t, id); status != models.ToolRunRunning {
		t.Errorf("status %q after the claim, want running", status)
	}
}

func TestDBJobsNextRefusesWhileToolsAreOffInSentinel(t *testing.T) {
	rig := newJobsRig(t, time.Second)
	w := rig.call(rig.other.ServerToken, http.MethodGet, nextPath(rig.other), "")
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusForbidden || code != "tools_disabled" {
		t.Errorf("status %d code %q, want 403 tools_disabled", w.Code, code)
	}
}

// An agent cannot read or write another agent's runs: another agent's token
// on this agent's path is refused before any handler runs, and on its own
// path the run is not its to touch.
func TestDBJobRoutesKeepAgentsToTheirOwnRuns(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	queued := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunQueued, user: rig.user, agent: rig.agent})
	running := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, nextPath(rig.agent), ""},
		{http.MethodPost, jobPath(rig.agent, running, "events"), twoEvents},
		{http.MethodPost, jobPath(rig.agent, running, "finish"), `{"status":"done","summary":{"sent":2}}`},
	} {
		if w := rig.call(rig.other.ServerToken, tc.method, tc.path, tc.body); w.Code != http.StatusForbidden {
			t.Errorf("%s %s with another agent's token: status %d, want 403", tc.method, tc.path, w.Code)
		}
	}
	if status, _ := rig.runStatus(t, queued); status != models.ToolRunQueued {
		t.Errorf("the queued run is %q: another agent's poll claimed it", status)
	}
	if stored, _ := rig.eventCount(t, running); stored != 0 {
		t.Errorf("%d events stored from another agent's token", stored)
	}

	for _, what := range []string{"events", "finish"} {
		body := twoEvents
		if what == "finish" {
			body = `{"status":"done"}`
		}
		w := rig.call(rig.other.ServerToken, http.MethodPost, jobPath(rig.other, running, what), body)
		if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusConflict || code != "conflict" {
			t.Errorf("%s for another agent's run: status %d code %q, want 409 conflict", what, w.Code, code)
		}
	}
	if status, _ := rig.runStatus(t, running); status != models.ToolRunRunning {
		t.Errorf("status %q, want the run untouched", status)
	}
}

// Events are stored once however often a post is retried; an empty batch is
// answered too (it is how the agent hears of a cancel); after a cancel the
// reply says so, and the agent's late finish leaves the run cancelled.
func TestDBJobEventsStoreOnceAndCarryTheCancel(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})
	cancelOf := func(w *httptest.ResponseRecorder) bool {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var reply struct {
			Cancel bool `json:"cancel"`
		}
		toolData(t, w.Body.Bytes(), &reply)
		return reply.Cancel
	}

	for i := 0; i < 2; i++ {
		if cancelOf(rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), twoEvents)) {
			t.Fatal("cancel before anyone cancelled")
		}
	}
	if stored, counted := rig.eventCount(t, id); stored != 2 || counted != 2 {
		t.Errorf("after a retried post: %d stored, event_count %d; want 2 and 2", stored, counted)
	}
	if cancelOf(rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), `{"events":[]}`)) {
		t.Error("an empty batch answered cancel for a running run")
	}

	_, err := rig.runs.Cancel(context.Background(), toolruns.Requester{UserID: rig.user, Username: "someone"}, id)
	testdb.Must(t, err)
	if !cancelOf(rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), `{"events":[]}`)) {
		t.Error("the reply after a cancel did not say cancel")
	}
	w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "finish"),
		`{"status":"failed","summary":{"sent":2,"received":2},"error":"cancelled"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("finish after cancel: status %d: %s", w.Code, w.Body.String())
	}
	if status, _ := rig.runStatus(t, id); status != models.ToolRunCancelled {
		t.Errorf("status %q after the agent's late finish, want cancelled", status)
	}
}

func TestDBJobEventsRefuseAPostOver64KB(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})
	big := `{"events":[{"seq":1,"at":"2026-10-05T12:00:00Z","type":"reply","data":"` + strings.Repeat("x", 70<<10) + `"}]}`
	w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), big)
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusRequestEntityTooLarge || code != "limit" {
		t.Errorf("status %d code %q, want 413 limit", w.Code, code)
	}
	if stored, _ := rig.eventCount(t, id); stored != 0 {
		t.Errorf("%d events stored from an oversized post", stored)
	}
}

// Reaching 5,000 events ends the run as failed, "too much output", and the
// agent is told with a 413.
func TestDBJobEventsEndTheRunAtTheEventCap(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})
	testdb.Exec(t, rig.db, `UPDATE tool_runs SET event_count = 5000 WHERE id = ?`, id)
	w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), twoEvents)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d, want 413", w.Code)
	}
	if status, errText := rig.runStatus(t, id); status != models.ToolRunFailed || errText != "too much output" {
		t.Errorf("run is %q %q, want failed / too much output", status, errText)
	}
}

// A run that is not running (still queued, or already ended) takes no events.
func TestDBJobEventsConflictUnlessTheRunIsRunning(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	for _, status := range []string{models.ToolRunQueued, models.ToolRunDone} {
		id := insertToolRun(t, rig.db, toolRunRow{status: status, user: rig.user, agent: rig.agent})
		if w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), twoEvents); w.Code != http.StatusConflict {
			t.Errorf("events for a %s run: status %d, want 409", status, w.Code)
		}
	}
}

func TestDBJobFinishRecordsTheOutcome(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})

	w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "finish"), `{"status":"running"}`)
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusBadRequest || code != "invalid_params" {
		t.Errorf("finish as running: status %d code %q, want 400 invalid_params", w.Code, code)
	}
	if w := rig.call(rig.agent.ServerToken, http.MethodPost, "/api/v1/agents/"+rig.agent.AgentID+"/jobs/not-a-run/finish",
		`{"status":"done"}`); w.Code != http.StatusBadRequest {
		t.Errorf("finish for a malformed run id: status %d, want 400", w.Code)
	}

	w = rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "finish"),
		`{"status":"done","summary":{"sent":5,"received":5,"loss_pct":0}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var sent int
	testdb.Must(t, rig.db.Raw(`SELECT (summary->>'sent')::int FROM tool_runs WHERE id = ?`, id).Scan(&sent).Error)
	if status, _ := rig.runStatus(t, id); status != models.ToolRunDone || sent != 5 {
		t.Errorf("run is %q with summary sent %d, want done and 5", status, sent)
	}
}
```

- [ ] **Step 2: Write the failing admin-route DB tests**

Create `backend/internal/api/net_tools_admin_handler_db_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// auditCall is one recorded audit entry, resource included.
type auditCall struct {
	action, resourceType string
	resourceID           *uuid.UUID
	changes              models.AuditChanges
}

// recordingAudit keeps every audit call.
type recordingAudit struct{ calls []auditCall }

func (a *recordingAudit) Record(_ context.Context, _ services.Actor, action, resourceType string, id *uuid.UUID, changes models.AuditChanges) {
	a.calls = append(a.calls, auditCall{action, resourceType, id, changes})
}

// netToolsAdminRouter mounts the admin routes behind RequireAdmin, as main.go
// does, for the user caller (read from the database).
func netToolsAdminRouter(t *testing.T, db *gorm.DB, audit *recordingAudit, caller uuid.UUID) *gin.Engine {
	t.Helper()
	auth := services.NewAuthService(db, testJWTSecret)
	user, err := auth.GetUserByID(context.Background(), caller)
	testdb.Must(t, err)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", user.ID)
		c.Set("username", user.Username)
		c.Set("is_admin", user.IsAdmin)
		c.Next()
	})
	admin := v1.Group("")
	admin.Use(RequireAdmin(auth))
	RegisterNetToolsAdminRoutes(admin, services.NewSettingsService(db), auth, services.NewAgentService(db), audit)
	return r
}

func storedSettings(t *testing.T, db *gorm.DB) toolruns.Settings {
	t.Helper()
	return toolruns.LoadSettings(context.Background(), services.NewSettingsService(db))
}

// Every bad line is reported against its entry, and nothing is saved.
func TestDBNetToolsSettingsRejectBadEntriesAndSaveNothing(t *testing.T) {
	db := testdb.Open(t)
	audit := &recordingAudit{}
	r := netToolsAdminRouter(t, db, audit, testdb.NewUser(t, db, true))
	w := toolRequest(r, http.MethodPut, "/api/v1/settings/net-tools",
		`{"allowlist":["10.0.0.0/8","10.0.0.0/7","not a host!"],"server_enabled":true,"retention_days":30}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", w.Code, w.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Entries []struct {
			Entry   string `json:"entry"`
			Message string `json:"message"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "invalid_allowlist" || len(body.Entries) != 2 ||
		body.Entries[0].Entry != "10.0.0.0/7" || body.Entries[0].Message != "too broad: use /8 or narrower" ||
		body.Entries[1].Entry != "not a host!" || body.Entries[1].Message != "not a valid IPv4 address, CIDR or host name" {
		t.Errorf("body = %+v", body)
	}
	if got := storedSettings(t, db); len(got.Allowlist) != 0 {
		t.Errorf("allowlist %v saved from a refused PUT", got.Allowlist)
	}
	if len(audit.calls) != 0 {
		t.Errorf("a refused save was audited: %+v", audit.calls)
	}

	w = toolRequest(r, http.MethodPut, "/api/v1/settings/net-tools", `{"allowlist":[],"server_enabled":true,"retention_days":0}`)
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusUnprocessableEntity || code != "invalid_params" {
		t.Errorf("retention 0: status %d code %q, want 422 invalid_params", w.Code, code)
	}
}

func TestDBNetToolsSettingsSaveTidyAndAudit(t *testing.T) {
	db := testdb.Open(t)
	audit := &recordingAudit{}
	r := netToolsAdminRouter(t, db, audit, testdb.NewUser(t, db, true))
	w := toolRequest(r, http.MethodPut, "/api/v1/settings/net-tools",
		`{"allowlist":["10.0.0.0/24"," Files.Example.ORG ","","10.0.0.0/24"],"server_enabled":false,"retention_days":7}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var saved toolruns.Settings
	toolData(t, w.Body.Bytes(), &saved)
	want := map[string]bool{"10.0.0.0/24": true, "files.example.org": true}
	if len(saved.Allowlist) != 2 || !want[saved.Allowlist[0]] || !want[saved.Allowlist[1]] || saved.ServerEnabled || saved.RetentionDays != 7 {
		t.Errorf("saved = %+v, want the two tidied entries, server off, 7 days", saved)
	}
	w = toolRequest(r, http.MethodGet, "/api/v1/settings/net-tools", "")
	var got toolruns.Settings
	toolData(t, w.Body.Bytes(), &got)
	if len(got.Allowlist) != 2 || got.ServerEnabled || got.RetentionDays != 7 {
		t.Errorf("GET = %+v, want what was saved", got)
	}
	if len(audit.calls) != 1 || audit.calls[0].action != models.ActionNetToolsSettingsUpdated ||
		audit.calls[0].resourceType != models.ResourceSettings || audit.calls[0].resourceID != nil ||
		audit.calls[0].changes.Before["retention_days"] != 30 || audit.calls[0].changes.After["retention_days"] != 7 {
		t.Errorf("audit = %+v", audit.calls)
	}
}

func TestDBUserNetToolsGrant(t *testing.T) {
	db := testdb.Open(t)
	audit := &recordingAudit{}
	r := netToolsAdminRouter(t, db, audit, testdb.NewUser(t, db, true))
	target := testdb.NewUser(t, db, false)

	w := toolRequest(r, http.MethodPatch, "/api/v1/users/"+target.String()+"/net-tools", `{"enabled":true}`)
	var reply struct {
		ID       uuid.UUID `json:"id"`
		NetTools bool      `json:"net_tools"`
	}
	toolData(t, w.Body.Bytes(), &reply)
	if reply.ID != target || !reply.NetTools {
		t.Errorf("reply = %+v", reply)
	}
	var stored bool
	testdb.Must(t, db.Raw(`SELECT net_tools FROM users WHERE id = ?`, target).Scan(&stored).Error)
	if !stored {
		t.Error("the grant was not stored")
	}
	if len(audit.calls) != 1 || audit.calls[0].action != models.ActionUserNetToolsChanged ||
		audit.calls[0].resourceType != models.ResourceUser || audit.calls[0].resourceID == nil || *audit.calls[0].resourceID != target ||
		audit.calls[0].changes.Before["net_tools"] != false || audit.calls[0].changes.After["net_tools"] != true {
		t.Errorf("audit = %+v", audit.calls)
	}

	if w := toolRequest(r, http.MethodPatch, "/api/v1/users/"+target.String()+"/net-tools", `{}`); w.Code != http.StatusBadRequest {
		t.Errorf("no enabled: status %d, want 400", w.Code)
	}
	for _, id := range []string{uuid.NewString(), "not-a-user"} {
		if w := toolRequest(r, http.MethodPatch, "/api/v1/users/"+id+"/net-tools", `{"enabled":true}`); w.Code != http.StatusNotFound {
			t.Errorf("user %s: status %d, want 404", id, w.Code)
		}
	}
}

func TestDBAgentToolsSwitch(t *testing.T) {
	db := testdb.Open(t)
	audit := &recordingAudit{}
	r := netToolsAdminRouter(t, db, audit, testdb.NewUser(t, db, true))
	agent := registerToolAgent(t, db, "file-server", false)

	w := toolRequest(r, http.MethodPut, "/api/v1/agents/"+agent.AgentID+"/tools", `{"enabled":true}`)
	var reply map[string]any
	toolData(t, w.Body.Bytes(), &reply)
	if reply["tools_enabled"] != true || reply["agent_id"] != agent.AgentID {
		t.Errorf("reply = %v", reply)
	}
	if _, leaked := reply["server_token"]; leaked {
		t.Error("the reply carries the agent's token")
	}
	var stored bool
	testdb.Must(t, db.Raw(`SELECT tools_enabled FROM agents WHERE id = ?`, agent.ID).Scan(&stored).Error)
	if !stored {
		t.Error("the switch was not stored")
	}
	if len(audit.calls) != 1 || audit.calls[0].action != models.ActionAgentToolsChanged ||
		audit.calls[0].resourceType != models.ResourceAgent || audit.calls[0].resourceID == nil || *audit.calls[0].resourceID != agent.ID ||
		audit.calls[0].changes.Before["tools_enabled"] != false || audit.calls[0].changes.After["tools_enabled"] != true {
		t.Errorf("audit = %+v", audit.calls)
	}
	if w := toolRequest(r, http.MethodPut, "/api/v1/agents/agent_ffffffffff/tools", `{"enabled":true}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown agent: status %d, want 404", w.Code)
	}
}

// Grant holders may run tools but change nothing: settings, grants and agent
// switches are refused by RequireAdmin before any handler runs.
func TestDBNetToolsAdminRoutesRefuseGrantHolders(t *testing.T) {
	db := testdb.Open(t)
	audit := &recordingAudit{}
	r := netToolsAdminRouter(t, db, audit, grantedUser(t, db))
	target := testdb.NewUser(t, db, false)
	agent := registerToolAgent(t, db, "file-server", false)

	for _, rt := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/settings/net-tools", ""},
		{http.MethodPut, "/api/v1/settings/net-tools", `{"allowlist":["10.0.0.0/8"],"server_enabled":true,"retention_days":30}`},
		{http.MethodPatch, "/api/v1/users/" + target.String() + "/net-tools", `{"enabled":true}`},
		{http.MethodPut, "/api/v1/agents/" + agent.AgentID + "/tools", `{"enabled":true}`},
	} {
		if w := toolRequest(r, rt.method, rt.path, rt.body); w.Code != http.StatusForbidden {
			t.Errorf("%s %s as a grant holder: status %d, want 403", rt.method, rt.path, w.Code)
		}
	}
	var granted, switched bool
	testdb.Must(t, db.Raw(`SELECT net_tools FROM users WHERE id = ?`, target).Scan(&granted).Error)
	testdb.Must(t, db.Raw(`SELECT tools_enabled FROM agents WHERE id = ?`, agent.ID).Scan(&switched).Error)
	if got := storedSettings(t, db); len(got.Allowlist) != 0 || granted || switched || len(audit.calls) != 0 {
		t.Errorf("a refused request changed something: allowlist %v, grant %v, switch %v, audit %d",
			got.Allowlist, granted, switched, len(audit.calls))
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go vet ./internal/api/`
Expected: the build fails with `undefined: registerAgentJobRoutes` and `undefined: RegisterNetToolsAdminRoutes`.

- [ ] **Step 4: Write the agent job handlers**

Create `backend/internal/api/agent_jobs_handler.go`:

```go
package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// agentJobsHandler serves the agent's side of network tools: collecting a
// job, posting its events while it runs, and reporting how it ended.
type agentJobsHandler struct {
	runs *toolruns.Service
	// wait is how long jobs/next holds a poll open (toolruns.LongPollWait;
	// tests shorten it).
	wait time.Duration
}

// agentEventsRequest is the body of an events post.
type agentEventsRequest struct {
	Events []toolruns.AgentEvent `json:"events"`
}

// agentFinishStatuses are the outcomes an agent may report.
var agentFinishStatuses = map[string]bool{
	models.ToolRunDone: true, models.ToolRunFailed: true, models.ToolRunRefused: true,
}

// maxAgentErrorLength bounds the error text an agent reports.
const maxAgentErrorLength = 2000

// RegisterAgentJobRoutes mounts the job routes beside the other agent ingest
// routes: the agent's own bearer token, never the user session, and only for
// the agent named in the path (requireOwnAgent).
func RegisterAgentJobRoutes(router *gin.Engine, agents *services.AgentService, runs *toolruns.Service) {
	registerAgentJobRoutes(router, agents, runs, toolruns.LongPollWait)
}

func registerAgentJobRoutes(router *gin.Engine, agents *services.AgentService, runs *toolruns.Service, wait time.Duration) {
	h := &agentJobsHandler{runs: runs, wait: wait}
	g := router.Group("/api/v1/agents", RequireAgentToken(agents))
	g.GET("/:agent_id/jobs/next", h.next)
	g.POST("/:agent_id/jobs/:run_id/events", h.events)
	g.POST("/:agent_id/jobs/:run_id/finish", h.finish)
}

// next handles GET /agents/:agent_id/jobs/next: the oldest queued run for
// this agent, waiting up to h.wait for one to be queued. 204 when none came;
// 403 tools_disabled while Sentinel's switch for the agent is off.
func (h *agentJobsHandler) next(c *gin.Context) {
	agent, ok := requireOwnAgent(c)
	if !ok {
		return
	}
	job, err := h.runs.NextJob(c.Request.Context(), agent, h.wait)
	switch {
	case errors.Is(err, toolruns.ErrToolsDisabled):
		respondToolError(c, http.StatusForbidden, codeToolsDisabled, "network tools are switched off for this agent in Sentinel")
	case err != nil && c.Request.Context().Err() != nil:
		// The agent hung up, or Sentinel is shutting down: nobody to answer.
		c.Status(http.StatusNoContent)
	case err != nil:
		respondInternal(c, "collecting an agent job", err)
	case job == nil:
		c.Status(http.StatusNoContent)
	default:
		respondSuccess(c, http.StatusOK, job)
	}
}

// events handles POST /agents/:agent_id/jobs/:run_id/events: a batch of the
// run's events (an empty batch is fine; its reply is how the agent learns of
// a cancel). The reply's cancel is true once the run has been cancelled.
func (h *agentJobsHandler) events(c *gin.Context) {
	agent, ok := requireOwnAgent(c)
	if !ok {
		return
	}
	runID, ok := jobRunID(c)
	if !ok {
		return
	}
	var req agentEventsRequest
	if !bindAgentBody(c, &req) {
		return
	}
	if req.Events == nil {
		req.Events = []toolruns.AgentEvent{}
	}
	for _, ev := range req.Events {
		if ev.Seq < 1 || strings.TrimSpace(ev.Type) == "" || len(ev.Data) == 0 {
			respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams,
				"each event needs a seq of 1 or more, a type and data")
			return
		}
	}
	cancel, err := h.runs.AgentEvents(c.Request.Context(), agent, runID, req.Events)
	if err != nil {
		respondJobError(c, "storing agent events", err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"cancel": cancel})
}

// finish handles POST /agents/:agent_id/jobs/:run_id/finish. A finish for a
// cancelled run only fills in its summary (ruling 8).
func (h *agentJobsHandler) finish(c *gin.Context) {
	agent, ok := requireOwnAgent(c)
	if !ok {
		return
	}
	runID, ok := jobRunID(c)
	if !ok {
		return
	}
	var req toolruns.AgentFinish
	if !bindAgentBody(c, &req) {
		return
	}
	if !agentFinishStatuses[req.Status] {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "status must be done, failed or refused")
		return
	}
	req.Error = truncate(req.Error, maxAgentErrorLength)
	if err := h.runs.AgentFinish(c.Request.Context(), agent, runID, req); err != nil {
		respondJobError(c, "finishing an agent job", err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"ok": true})
}

// jobRunID reads :run_id.
func jobRunID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("run_id"))
	if err != nil {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "run_id must be a run id")
		return uuid.Nil, false
	}
	return id, true
}

// bindAgentBody decodes an agent post of at most MaxAgentPostBytes: 413 past
// that, 400 for anything that is not the expected JSON.
func bindAgentBody(c *gin.Context, into any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, toolruns.MaxAgentPostBytes)
	if err := c.ShouldBindJSON(into); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			respondToolError(c, http.StatusRequestEntityTooLarge, toolruns.CodeLimit, "an agent post may be at most 64 KB")
			return false
		}
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "invalid request body")
		return false
	}
	return true
}

// respondJobError maps the dispatcher's errors: a run that is not running on
// this agent is 409 (the agent stops), too much output is 413.
func respondJobError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, toolruns.ErrRunNotActive):
		respondToolError(c, http.StatusConflict, toolruns.CodeConflict, "that run is not running on this agent")
	case errors.Is(err, toolruns.ErrTooMuchOutput):
		respondToolError(c, http.StatusRequestEntityTooLarge, toolruns.CodeLimit, "too much output")
	default:
		respondInternal(c, op, err)
	}
}
```

- [ ] **Step 5: Write the admin handlers**

Create `backend/internal/api/net_tools_admin_handler.go`:

```go
package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// netToolsAdmin serves what only admins may change about network tools: the
// settings (allowlist, the Sentinel vantage, retention), the per-user grant
// and the per-agent switch. Every change is audited.
type netToolsAdmin struct {
	settings *services.SettingsService
	auth     *services.AuthService
	agents   *services.AgentService
	audit    auditRecorder
}

// netToolsToggle is the body of the grant and agent-switch routes.
type netToolsToggle struct {
	Enabled *bool `json:"enabled"`
}

// RegisterNetToolsAdminRoutes mounts the admin routes on the admin group
// (RequireAdmin is the caller's).
func RegisterNetToolsAdminRoutes(admin *gin.RouterGroup, settings *services.SettingsService, auth *services.AuthService, agents *services.AgentService, audit auditRecorder) {
	h := &netToolsAdmin{settings: settings, auth: auth, agents: agents, audit: audit}
	admin.GET("/settings/net-tools", h.getSettings)
	admin.PUT("/settings/net-tools", h.putSettings)
	admin.PATCH("/users/:id/net-tools", h.setUserGrant)
	admin.PUT("/agents/:agent_id/tools", h.setAgentTools)
}

// getSettings handles GET /settings/net-tools.
func (h *netToolsAdmin) getSettings(c *gin.Context) {
	respondSuccess(c, http.StatusOK, toolruns.LoadSettings(c.Request.Context(), h.settings))
}

// putSettings handles PUT /settings/net-tools. Bad allowlist entries are
// each reported (422 invalid_allowlist with "entries") and nothing is saved.
// Saving never touches runs already in progress.
func (h *netToolsAdmin) putSettings(c *gin.Context) {
	var req toolruns.Settings
	if err := c.ShouldBindJSON(&req); err != nil {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "invalid request body")
		return
	}
	ctx := c.Request.Context()
	before := toolruns.LoadSettings(ctx, h.settings)
	saved, err := toolruns.SaveSettings(ctx, h.settings, req)
	if err != nil {
		var bad *toolruns.SettingsError
		switch {
		case !errors.As(err, &bad):
			respondInternal(c, "saving network tools settings", err)
		case len(bad.Entries) > 0:
			c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
				"success": false,
				"error":   gin.H{"code": codeInvalidAllowlist, "message": bad.Message},
				"entries": bad.Entries,
			})
		default:
			respondToolError(c, http.StatusUnprocessableEntity, toolruns.CodeInvalidParams, bad.Message)
		}
		return
	}
	h.record(c, models.ActionNetToolsSettingsUpdated, models.ResourceSettings, nil, models.AuditChanges{
		Before: netToolsSettingsAudit(before),
		After:  netToolsSettingsAudit(saved),
	})
	respondSuccess(c, http.StatusOK, saved)
}

func netToolsSettingsAudit(s toolruns.Settings) map[string]any {
	return map[string]any{"allowlist": s.Allowlist, "server_enabled": s.ServerEnabled, "retention_days": s.RetentionDays}
}

// setUserGrant handles PATCH /users/:id/net-tools. The flag is stored for an
// admin too, though an admin may always use the tools: it then says what was
// chosen should the account be demoted.
func (h *netToolsAdmin) setUserGrant(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondToolError(c, http.StatusNotFound, toolruns.CodeNotFound, "no such user")
		return
	}
	enabled, ok := bindNetToolsToggle(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	before, err := h.auth.GetUserByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondToolError(c, http.StatusNotFound, toolruns.CodeNotFound, "no such user")
		return
	}
	if err != nil {
		respondInternal(c, "reading a user", err)
		return
	}
	updated, err := h.auth.SetNetTools(ctx, id, enabled)
	if err != nil {
		respondInternal(c, "changing a network tools grant", err)
		return
	}
	h.record(c, models.ActionUserNetToolsChanged, models.ResourceUser, &id, models.AuditChanges{
		Before:  map[string]any{"net_tools": before.NetTools},
		After:   map[string]any{"net_tools": updated.NetTools},
		Summary: map[string]any{"username": updated.Username},
	})
	respondSuccess(c, http.StatusOK, gin.H{"id": updated.ID, "net_tools": updated.NetTools})
}

// setAgentTools handles PUT /agents/:agent_id/tools: Sentinel's half of the
// agent opt-in. The agent comes back with its token hidden.
func (h *netToolsAdmin) setAgentTools(c *gin.Context) {
	enabled, ok := bindNetToolsToggle(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	before, err := h.agents.Get(ctx, c.Param("agent_id"))
	if err != nil {
		respondToolAgentError(c, err)
		return
	}
	updated, err := h.agents.SetToolsEnabled(ctx, before.AgentID, enabled)
	if err != nil {
		respondToolAgentError(c, err)
		return
	}
	h.record(c, models.ActionAgentToolsChanged, models.ResourceAgent, &updated.ID, models.AuditChanges{
		Before:  map[string]any{"tools_enabled": before.ToolsEnabled},
		After:   map[string]any{"tools_enabled": updated.ToolsEnabled},
		Summary: map[string]any{"agent_id": updated.AgentID, "name": updated.Name},
	})
	updated.HideToken()
	respondSuccess(c, http.StatusOK, updated)
}

// respondToolAgentError answers a missing agent as 404 not_found.
func respondToolAgentError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrAgentNotFound) {
		respondToolError(c, http.StatusNotFound, toolruns.CodeNotFound, "no such agent")
		return
	}
	respondInternal(c, "switching network tools on an agent", err)
}

// bindNetToolsToggle reads {"enabled": true|false}; enabled is required.
func bindNetToolsToggle(c *gin.Context) (bool, bool) {
	var req netToolsToggle
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		respondToolError(c, http.StatusBadRequest, toolruns.CodeInvalidParams, "enabled (true or false) is required")
		return false, false
	}
	return *req.Enabled, true
}

// record writes one audit entry as the signed-in admin.
func (h *netToolsAdmin) record(c *gin.Context, action, resource string, id *uuid.UUID, changes models.AuditChanges) {
	if h.audit == nil {
		return
	}
	h.audit.Record(c.Request.Context(), actorFrom(c), action, resource, id, changes)
}
```

- [ ] **Step 6: Run the DB tests to verify they pass**

Run: `./scripts/test-db.sh -run 'TestDBJobs|TestDBJobRoutes|TestDBJobEvents|TestDBJobFinish|TestDBNetToolsSettings|TestDBUserNetToolsGrant|TestDBAgentToolsSwitch|TestDBNetToolsAdminRoutes' -v`
Expected: PASS. These tests also pin requirements on Task 10:
- `TestDBJobEventsStoreOnceAndCarryTheCancel` requires `AgentEvents` to accept an empty batch.
- `TestDBJobEventsEndTheRunAtTheEventCap` requires the event cap to finish the run as failed with "too much output".

If either fails, fix it in `toolruns/dispatch.go`. That is Task 10's code, and the fix goes in this task's commit.

- [ ] **Step 7: Wire everything in `main.go`**

Make these edits to `backend/cmd/sentinel/main.go`.

(a) Imports. Add `"net"` after `"log"`, and add the two packages after the `snmp` import:

```go
	"log"
	"net"
	"net/http"
```

```go
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)
```

(b) Add this after `const shutdownTimeout = 30 * time.Second`:

```go

// toolHubBuffer is how many live tool-run frames one open stream may fall
// behind before it is dropped. A port scan can publish a whole flush of
// events at once (up to 1,024 ports); a dropped browser reconnects and
// replays from the database, so this only saves the round trip.
const toolHubBuffer = 2048
```

(c) Replace `	agentService := services.NewAgentService(db)` with:

```go
	agentService := services.NewAgentService(db)
	// Network tools (S1): the run service, and the hub its runs publish live
	// events to for the SSE streams.
	toolHub := stream.NewHub(toolHubBuffer)
	toolRuns := toolruns.New(toolruns.Deps{DB: db, Settings: settingsService, Audit: auditService, Hub: toolHub})
```

(d) After `	api.RegisterAgentIngestRoutes(router, agentService)`, insert:

```go
	// Agents with ENABLE_TOOLS long-poll here for network-tool jobs and post
	// their results, with the same token as their metrics.
	api.RegisterAgentJobRoutes(router, agentService, toolRuns)
```

(e) After `	api.RegisterAgentRoutes(v1, agentService, settingsService, authService)`, insert:

```go
	// Network tools: runs, history and the live stream, for admins and users
	// granted the tools.
	api.RegisterToolRoutes(v1, toolRuns, toolHub, authService, auditService)
```

(f) After `	api.RegisterAuditRoutes(admin, auditService)`, insert:

```go
	// Network tools settings, the per-user grant and the per-agent switch.
	api.RegisterNetToolsAdminRoutes(admin, settingsService, authService, agentService, auditService)
```

(g) After `	go agentService.StartOfflineSweep(loopCtx)`, insert:

```go
	// Network tools: runs a previous process left in progress become
	// interrupted, then pickup timeouts and overdue runs are swept every few
	// seconds and old history is pruned daily.
	go toolRuns.Start(loopCtx)
```

(h) Replace the `// 9. HTTP server.` block up to the closing `}` of the `server` literal with:

```go
	// 9. HTTP server. Every request's context derives from streamsCtx, which
	// is cancelled just before Shutdown: open SSE streams and agent long-polls
	// then end at once instead of holding shutdown for its whole timeout.
	streamsCtx, cancelStreams := context.WithCancel(context.Background())
	defer cancelStreams()
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return streamsCtx },
	}
```

(i) In the graceful-shutdown part, call `cancelStreams()` between `defer cancel()` and `server.Shutdown`:

```go
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	cancelStreams()
	if err := server.Shutdown(shutdownCtx); err != nil {
```

- [ ] **Step 8: Build and vet**

Run: `go build ./cmd/sentinel && go vet ./... && gofmt -l internal cmd`
Expected: no output.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/api/agent_jobs_handler.go backend/internal/api/net_tools_admin_handler.go \
  backend/internal/api/agent_jobs_handler_db_test.go backend/internal/api/net_tools_admin_handler_db_test.go \
  backend/cmd/sentinel/main.go
git commit -m "feat(tools): agent job routes, net tools admin routes and wiring

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: The agent's job loop

`cmd/agent/jobs.go` holds the job loop. It runs only with `ENABLE_TOOLS` set, and it works as follows:
- **Polling.** The agent long-polls with a 35 s client timeout. It polls even while both job slots are busy, because Sentinel queues at most 2 runs per agent anyway, and an agent that stopped polling would show as offline.
- **Local checks.** Each job is checked before it runs:
  - `nettools.Normalize` must accept it. The agent runs the spec it normalized itself, which equals Sentinel's when the two agree. This is how the brief's "idempotent" is implemented.
  - `target_ip` must not be always-blocked.
  - `target_ip` must be inside `TOOLS_ALLOWED_TARGETS` when that is set. A lookup through the agent's own resolver has no address, and is always allowed.
  - The deadline must not have passed.

  A job that fails a check is finished as `refused` with the reason.
- **Running.** At most 2 jobs run at once. Each runs under the earlier of its deadline and `nettools.Deadline(tool)`.
- **Posting events.** While a job runs, its events are posted every 250 ms, even when there are none: the reply is how a cancel arrives. Each post holds at most 48 KB, inside Sentinel's 64 KB limit. A reply of `cancel: true` stops the tool, and the agent then finishes as `failed`/"cancelled"; Sentinel keeps the run cancelled (ruling 8). A 409 stops the tool and nothing more is sent.
- **Backoff.** After tools_disabled, the agent waits 60 s. After a network error, it waits 5 s, doubling up to 60 s.

The tool is an injectable `toolRunner`, and every wait is a field on `jobLoop`. Each test therefore runs in well under a second against an `httptest` fake of Sentinel.

**Files:**
- Create: `backend/cmd/agent/jobs_client.go` (the wire types, the HTTP client for the job routes, the buffer of events waiting to be posted)
- Create: `backend/cmd/agent/jobs.go` (the loop: polling, local checks, running, posting, reporting)
- Modify: `backend/cmd/agent/main.go` (imports; `config`; `loadConfig`; new `envTrue`; the `apiClient` literal in `main`; `run`; `apiClient`; `heartbeat`)
- Modify: `backend/Dockerfile` (`AGENT_VERSION` 1.1.0)
- Test: `backend/cmd/agent/jobs_fake_test.go` (new: the fake Sentinel and the test helpers; the package has no test files yet)
- Test: `backend/cmd/agent/jobs_test.go` (new: the loop)
- Test: `backend/cmd/agent/config_test.go` (new: the settings, the heartbeat field and `run`'s wiring)

**Interfaces:**
- Consumes:
  - The shared contract, `nettools`:
    - `Spec`, `Params`, `Event`, `Emitter`, `Normalize`, `Deadline`, `AlwaysBlocked`, `InNets`, `ParseAddressList` and `Runner` (`(*Runner).Run`)
    - `ToolPing`/`ToolDNS`/`ToolTCP`, `EventReply`/`EventPort`, `PingReply`, `PingSummary`, `DNSSummary`, `PortResult`, `PortClosed`, `TCPSummary`, `PingCountDefault` and `ErrICMPUnavailable`
  - Task 12's wire format (Contract change 2).
- Produces:
  - `config.ToolsEnabled bool` and `config.ToolsAllowed []*net.IPNet`.
  - `envTrue(string) bool`.
  - `apiClient.toolsLocal`; the heartbeat body gains `"tools_local": bool`, always present.
  - `type toolRunner interface{ Run(ctx, nettools.Spec, nettools.Emitter) (any, error) }`.
  - `newJobLoop(cfg config, runner toolRunner) *jobLoop` and `(*jobLoop).run(ctx)`.
  - `run()` starts the loop when `cfg.ToolsEnabled`, and waits for it before returning.

- [ ] **Step 1: Write the fake Sentinel and the test helpers**

Create `backend/cmd/agent/jobs_fake_test.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

const (
	testAgentID = "agent_0123456789"
	testToken   = "srv_test_token"
)

// fakeSentinel plays Sentinel's side of the job routes and the heartbeat.
type fakeSentinel struct {
	t   *testing.T
	srv *httptest.Server

	mu          sync.Mutex
	jobs        []agentJob // handed out by jobs/next in order, then 204
	nextStatus  int        // when set, jobs/next answers this status
	nextBody    string
	polls       int
	events      map[string][]jobEvent
	posts       int
	largestPost int
	cancelled   map[string]bool // events posts answer cancel: true
	gone        map[string]bool // events posts answer 409
	finishes    map[string]jobFinish
	heartbeats  []map[string]any
}

func newFakeSentinel(t *testing.T) *fakeSentinel {
	t.Helper()
	f := &fakeSentinel{
		t: t, events: map[string][]jobEvent{}, cancelled: map[string]bool{},
		gone: map[string]bool{}, finishes: map[string]jobFinish{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agents/"+testAgentID+"/jobs/next", f.next)
	mux.HandleFunc("POST /api/v1/agents/"+testAgentID+"/jobs/{run}/events", f.postEvents)
	mux.HandleFunc("POST /api/v1/agents/"+testAgentID+"/jobs/{run}/finish", f.finish)
	mux.HandleFunc("POST /api/v1/agents/heartbeat", f.heartbeat)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Errorf("%s %s without the agent's token", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func writeEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
}

func (f *fakeSentinel) next(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	switch {
	case f.nextStatus != 0:
		w.WriteHeader(f.nextStatus)
		_, _ = io.WriteString(w, f.nextBody)
	case len(f.jobs) == 0:
		w.WriteHeader(http.StatusNoContent)
	default:
		job := f.jobs[0]
		f.jobs = f.jobs[1:]
		writeEnvelope(w, job)
	}
}

func (f *fakeSentinel) postEvents(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	run := r.PathValue("run")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts++
	if len(body) > f.largestPost {
		f.largestPost = len(body)
	}
	switch {
	case len(body) > 64<<10:
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	case f.gone[run]:
		w.WriteHeader(http.StatusConflict)
		return
	case f.cancelled[run]:
		writeEnvelope(w, map[string]bool{"cancel": true})
		return
	}
	var req struct {
		Events []jobEvent `json:"events"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Events == nil {
		f.t.Errorf("events post %s: %v (events must be a list, even an empty one)", body, err)
	}
	f.events[run] = append(f.events[run], req.Events...)
	writeEnvelope(w, map[string]bool{"cancel": false})
}

func (f *fakeSentinel) finish(w http.ResponseWriter, r *http.Request) {
	var req jobFinish
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Errorf("finish body: %v", err)
	}
	f.mu.Lock()
	f.finishes[r.PathValue("run")] = req
	f.mu.Unlock()
	writeEnvelope(w, map[string]bool{"ok": true})
}

func (f *fakeSentinel) heartbeat(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.t.Errorf("heartbeat body: %v", err)
	}
	f.mu.Lock()
	f.heartbeats = append(f.heartbeats, body)
	f.mu.Unlock()
	writeEnvelope(w, map[string]string{"message": "heartbeat recorded"})
}

// set changes the fake's behaviour under its lock.
func (f *fakeSentinel) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func (f *fakeSentinel) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls
}

func (f *fakeSentinel) eventsOf(run string) []jobEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]jobEvent(nil), f.events[run]...)
}

// waitFinish waits for run's finish post and returns it.
func (f *fakeSentinel) waitFinish(t *testing.T, run string) jobFinish {
	t.Helper()
	var fin jobFinish
	waitFor(t, "the finish of "+run, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		got, ok := f.finishes[run]
		fin = got
		return ok
	})
	return fin
}

// waitFor checks cond every 5 ms for up to 3 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// runnerFunc adapts a function to toolRunner.
type runnerFunc func(ctx context.Context, s nettools.Spec, emit nettools.Emitter) (any, error)

func (f runnerFunc) Run(ctx context.Context, s nettools.Spec, emit nettools.Emitter) (any, error) {
	return f(ctx, s, emit)
}

// testLoop is a job loop against f with every wait cut to milliseconds.
func testLoop(f *fakeSentinel, runner toolRunner, allowed []*net.IPNet) *jobLoop {
	l := newJobLoop(config{ServerURL: f.srv.URL, AgentID: testAgentID, ServerToken: testToken, ToolsAllowed: allowed}, runner)
	l.flushEvery = 10 * time.Millisecond
	l.minPoll = 5 * time.Millisecond
	l.disabledWait = time.Second
	l.backoffMin = 50 * time.Millisecond
	l.backoffMax = 200 * time.Millisecond
	l.retryDelay = time.Millisecond
	return l
}

// startLoop runs l until the test ends (before the fake server closes).
func startLoop(t *testing.T, l *jobLoop) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// testJob is a ping job against ip with the default parameters.
func testJob(run, ip string) agentJob {
	return agentJob{
		RunID:    run,
		Spec:     nettools.Spec{Tool: nettools.ToolPing, Target: ip, TargetIP: ip},
		Deadline: time.Now().Add(time.Minute),
	}
}
```

- [ ] **Step 2: Write the failing loop tests**

Create `backend/cmd/agent/jobs_test.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

func TestJobLoopRunsAJobAndPostsItsEventsInOrder(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() { f.jobs = []agentJob{testJob("run-1", "10.0.0.5")} })
	specs := make(chan nettools.Spec, 1)
	runner := runnerFunc(func(_ context.Context, s nettools.Spec, emit nettools.Emitter) (any, error) {
		specs <- s
		for i := 1; i <= 5; i++ {
			emit(nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: i, RTTMS: 1.5, TTL: 64, From: s.TargetIP}})
			time.Sleep(3 * time.Millisecond)
		}
		return nettools.PingSummary{Sent: 5, Received: 5}, nil
	})
	startLoop(t, testLoop(f, runner, nil))

	fin := f.waitFinish(t, "run-1")
	var sum nettools.PingSummary
	if fin.Status != "done" || fin.Error != "" || json.Unmarshal(fin.Summary, &sum) != nil || sum.Sent != 5 || sum.Received != 5 {
		t.Errorf("finish = %+v (summary %s), want done with 5 of 5", fin, fin.Summary)
	}
	evs := f.eventsOf("run-1")
	if len(evs) != 5 {
		t.Fatalf("%d events stored before the finish, want 5", len(evs))
	}
	for i, ev := range evs {
		var reply nettools.PingReply
		if ev.Seq != i+1 || ev.Type != nettools.EventReply || json.Unmarshal(ev.Data, &reply) != nil || reply.Seq != i+1 || reply.From != "10.0.0.5" {
			t.Errorf("event %d = %+v (%s)", i, ev, ev.Data)
		}
	}
	// The agent ran the spec it normalized itself: the defaults are filled in.
	if s := <-specs; s.Params.Count != nettools.PingCountDefault || s.TargetIP != "10.0.0.5" {
		t.Errorf("ran %+v, want the default count and the job's address", s)
	}
}

// A port scan can emit hundreds of events between two posts; each post stays
// under Sentinel's 64 KB limit and none is lost.
func TestJobLoopSplitsBurstsUnderThePostLimit(t *testing.T) {
	f := newFakeSentinel(t)
	job := testJob("run-1", "10.0.0.5")
	job.Spec.Tool, job.Spec.Params = nettools.ToolTCP, nettools.Params{Ports: "1-1024"}
	f.set(func() { f.jobs = []agentJob{job} })
	const n = 1500
	runner := runnerFunc(func(_ context.Context, _ nettools.Spec, emit nettools.Emitter) (any, error) {
		for i := 1; i <= n; i++ {
			emit(nettools.Event{Type: nettools.EventPort, Data: nettools.PortResult{Port: i, State: nettools.PortClosed}})
		}
		return nettools.TCPSummary{Total: n, Closed: n, OpenPorts: []int{}}, nil
	})
	startLoop(t, testLoop(f, runner, nil))

	if fin := f.waitFinish(t, "run-1"); fin.Status != "done" {
		t.Fatalf("finish = %+v, want done", fin)
	}
	evs := f.eventsOf("run-1")
	if len(evs) != n {
		t.Fatalf("%d events stored, want %d", len(evs), n)
	}
	for i, ev := range evs {
		if ev.Seq != i+1 {
			t.Fatalf("event %d has seq %d: out of order or lost", i, ev.Seq)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.largestPost > 64<<10 || f.posts < 3 {
		t.Errorf("largest post %d bytes over %d posts; want every post within 64 KB, so several posts", f.largestPost, f.posts)
	}
}

func TestJobLoopRefusesJobsItWillNotRun(t *testing.T) {
	_, lan, _ := net.ParseCIDR("10.0.0.0/24")
	expired := testJob("run-1", "10.0.0.5")
	expired.Deadline = time.Now().Add(-time.Second)
	tooMany := testJob("run-1", "10.0.0.5")
	tooMany.Spec.Params.Count = 1000
	noAddress := testJob("run-1", "")
	noAddress.Spec.Target = "files.example.org"
	cases := []struct {
		name    string
		job     agentJob
		allowed []*net.IPNet
		reason  string
	}{
		{"outside TOOLS_ALLOWED_TARGETS", testJob("run-1", "192.168.1.5"), []*net.IPNet{lan}, "outside TOOLS_ALLOWED_TARGETS"},
		{"cloud metadata address", testJob("run-1", "169.254.169.254"), nil, "never probed"},
		{"parameters past the limits", tooMany, nil, "count"},
		{"deadline already passed", expired, nil, "deadline"},
		{"no address to probe", noAddress, nil, "no target address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSentinel(t)
			f.set(func() { f.jobs = []agentJob{tc.job} })
			var ran atomic.Int32
			runner := runnerFunc(func(context.Context, nettools.Spec, nettools.Emitter) (any, error) {
				ran.Add(1)
				return nil, nil
			})
			startLoop(t, testLoop(f, runner, tc.allowed))
			fin := f.waitFinish(t, "run-1")
			if fin.Status != "refused" || !strings.Contains(fin.Error, tc.reason) {
				t.Errorf("finish = %+v, want refused mentioning %q", fin, tc.reason)
			}
			if ran.Load() != 0 {
				t.Error("the tool ran for a refused job")
			}
		})
	}
}

// A lookup through the agent's own resolver contacts no job address, so
// TOOLS_ALLOWED_TARGETS does not stop it.
func TestJobLoopRunsSystemResolverLookupsWhateverTheAllowedList(t *testing.T) {
	_, lan, _ := net.ParseCIDR("10.0.0.0/24")
	f := newFakeSentinel(t)
	f.set(func() {
		f.jobs = []agentJob{{RunID: "run-1", Spec: nettools.Spec{Tool: nettools.ToolDNS, Target: "example.org"},
			Deadline: time.Now().Add(time.Minute)}}
	})
	runner := runnerFunc(func(context.Context, nettools.Spec, nettools.Emitter) (any, error) {
		return nettools.DNSSummary{Server: "127.0.0.53:53", RCode: "NOERROR", AnswerCount: 1}, nil
	})
	startLoop(t, testLoop(f, runner, []*net.IPNet{lan}))
	if fin := f.waitFinish(t, "run-1"); fin.Status != "done" {
		t.Errorf("finish = %+v, want done", fin)
	}
}

// Review Focus 5: a user cancels while the agent is mid-run. The next events
// post answers cancel: true; the agent stops the tool at once and reports
// failed/"cancelled", which Sentinel files under the cancelled run without
// changing its status.
func TestJobLoopStopsTheToolWhenSentinelCancels(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() { f.jobs = []agentJob{testJob("run-1", "10.0.0.5")} })
	stopped := make(chan error, 1)
	runner := runnerFunc(func(ctx context.Context, s nettools.Spec, emit nettools.Emitter) (any, error) {
		emit(nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: 1, RTTMS: 1, TTL: 64, From: s.TargetIP}})
		<-ctx.Done()
		stopped <- ctx.Err()
		return nettools.PingSummary{Sent: 1, Received: 1}, ctx.Err()
	})
	startLoop(t, testLoop(f, runner, nil))

	waitFor(t, "the first event", func() bool { return len(f.eventsOf("run-1")) == 1 })
	f.set(func() { f.cancelled["run-1"] = true })
	select {
	case err := <-stopped:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the tool stopped with %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the tool kept running after Sentinel cancelled the run")
	}
	fin := f.waitFinish(t, "run-1")
	var sum nettools.PingSummary
	if fin.Status != "failed" || fin.Error != "cancelled" || json.Unmarshal(fin.Summary, &sum) != nil || sum.Sent != 1 {
		t.Errorf("finish = %+v (summary %s), want failed / cancelled with the partial summary", fin, fin.Summary)
	}
}

// A 409 means Sentinel no longer runs the job (restarted, or the run was
// ended): the agent stops the tool and sends nothing more.
func TestJobLoopStopsWhenSentinelNoLongerRunsTheJob(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() {
		f.jobs = []agentJob{testJob("run-1", "10.0.0.5")}
		f.gone["run-1"] = true
	})
	stopped := make(chan struct{})
	runner := runnerFunc(func(ctx context.Context, _ nettools.Spec, _ nettools.Emitter) (any, error) {
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	})
	startLoop(t, testLoop(f, runner, nil))
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("the tool kept running after a 409")
	}
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.finishes) != 0 {
		t.Errorf("finish posted for a run Sentinel no longer runs: %+v", f.finishes)
	}
}

func TestJobLoopReportsAToolThatCannotRun(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() { f.jobs = []agentJob{testJob("run-1", "10.0.0.5")} })
	runner := runnerFunc(func(context.Context, nettools.Spec, nettools.Emitter) (any, error) {
		return nil, nettools.ErrICMPUnavailable
	})
	startLoop(t, testLoop(f, runner, nil))
	fin := f.waitFinish(t, "run-1")
	if fin.Status != "failed" || fin.Error != "ICMP isn't available here (needs root or NET_RAW)" {
		t.Errorf("finish = %+v", fin)
	}
}

func TestJobLoopRunsAtMostTwoJobsAtOnce(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() {
		f.jobs = []agentJob{testJob("run-1", "10.0.0.5"), testJob("run-2", "10.0.0.6"), testJob("run-3", "10.0.0.7")}
	})
	var mu sync.Mutex
	running, most := 0, 0
	counts := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return running, most
	}
	release := make(chan struct{})
	runner := runnerFunc(func(ctx context.Context, _ nettools.Spec, _ nettools.Emitter) (any, error) {
		mu.Lock()
		running++
		most = max(most, running)
		mu.Unlock()
		defer func() {
			mu.Lock()
			running--
			mu.Unlock()
		}()
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nettools.PingSummary{}, nil
	})
	startLoop(t, testLoop(f, runner, nil))

	waitFor(t, "two jobs running", func() bool { n, _ := counts(); return n == 2 })
	time.Sleep(50 * time.Millisecond)
	if n, _ := counts(); n != 2 {
		t.Errorf("%d jobs running, want 2: the third waits for a slot", n)
	}
	close(release)
	for _, run := range []string{"run-1", "run-2", "run-3"} {
		if fin := f.waitFinish(t, run); fin.Status != "done" {
			t.Errorf("%s: finish = %+v, want done", run, fin)
		}
	}
	if _, m := counts(); m != 2 {
		t.Errorf("at most %d jobs ran at once, want 2", m)
	}
}

// While Sentinel's switch is off the agent asks again only every
// disabledWait (60 s; 1 s here), not in a tight loop.
func TestJobLoopWaitsWhileToolsAreDisabled(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() {
		f.nextStatus = http.StatusForbidden
		f.nextBody = `{"success":false,"error":{"code":"tools_disabled","message":"network tools are switched off for this agent in Sentinel"}}`
	})
	startLoop(t, testLoop(f, runnerFunc(nil), nil))
	waitFor(t, "the first poll", func() bool { return f.pollCount() >= 1 })
	time.Sleep(300 * time.Millisecond)
	if n := f.pollCount(); n != 1 {
		t.Errorf("%d polls in 300 ms after tools_disabled, want 1", n)
	}
}

// After a failed poll the agent waits backoffMin (5 s; 50 ms here), doubling.
func TestJobLoopBacksOffAfterFailedPolls(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() { f.nextStatus = http.StatusBadGateway })
	startLoop(t, testLoop(f, runnerFunc(nil), nil))
	time.Sleep(120 * time.Millisecond)
	// Polls go at 0, after at least 50 ms, then after at least 100 ms more:
	// no more than two fit in the first 120 ms.
	if n := f.pollCount(); n < 1 || n > 2 {
		t.Errorf("%d polls in 120 ms against a failing server, want 1 or 2", n)
	}
}

func TestNextBackoffDoublesUpToTheCap(t *testing.T) {
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second}
	d := jobBackoffMin
	for i, w := range want {
		if d != w {
			t.Errorf("wait %d = %s, want %s", i+1, d, w)
		}
		d = nextBackoff(d, jobBackoffMax)
	}
}
```

- [ ] **Step 3: Write the failing settings, heartbeat and wiring tests**

Create `backend/cmd/agent/config_test.go`:

```go
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

func TestLoadConfigReadsTheToolSettings(t *testing.T) {
	base := func(t *testing.T) {
		t.Setenv("SENTINEL_URL", "http://10.1.20.10:3001")
		t.Setenv("AGENT_ID", testAgentID)
		t.Setenv("SERVER_TOKEN", testToken)
		t.Setenv("CHECK_INTERVAL", "")
		t.Setenv("RETRY_ATTEMPTS", "")
		t.Setenv("ENABLE_TOOLS", "")
		t.Setenv("TOOLS_ALLOWED_TARGETS", "")
	}
	for raw, want := range map[string]bool{
		"": false, "true": true, "TRUE": true, "1": true, "yes": true, "Yes": true,
		"false": false, "0": false, "no": false, "on": false,
	} {
		t.Run("ENABLE_TOOLS="+raw, func(t *testing.T) {
			base(t)
			t.Setenv("ENABLE_TOOLS", raw)
			cfg, err := loadConfig()
			if err != nil || cfg.ToolsEnabled != want {
				t.Errorf("ToolsEnabled = %v, %v; want %v", cfg.ToolsEnabled, err, want)
			}
		})
	}
	t.Run("allowed targets", func(t *testing.T) {
		base(t)
		t.Setenv("TOOLS_ALLOWED_TARGETS", "10.0.0.0/8,192.168.1.10")
		cfg, err := loadConfig()
		if err != nil || len(cfg.ToolsAllowed) != 2 {
			t.Fatalf("ToolsAllowed = %v, %v; want two networks", cfg.ToolsAllowed, err)
		}
		for ip, want := range map[string]bool{"10.1.2.3": true, "192.168.1.10": true, "192.168.1.11": false} {
			if got := nettools.InNets(net.ParseIP(ip), cfg.ToolsAllowed); got != want {
				t.Errorf("%s allowed = %v, want %v", ip, got, want)
			}
		}
	})
	t.Run("bad allowed targets", func(t *testing.T) {
		base(t)
		t.Setenv("TOOLS_ALLOWED_TARGETS", "10.0.0.0/33")
		if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "TOOLS_ALLOWED_TARGETS") {
			t.Errorf("err = %v, want one naming TOOLS_ALLOWED_TARGETS", err)
		}
	})
}

// Sentinel tells "too old to run tools" (no tools_local at all) from "tools
// off on this host" (false), so the field is always sent.
func TestHeartbeatReportsToolsLocal(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		f := newFakeSentinel(t)
		c := &apiClient{baseURL: f.srv.URL, token: testToken, agentID: testAgentID, retries: 1,
			http: &http.Client{Timeout: 5 * time.Second}, toolsLocal: enabled}
		if err := c.heartbeat(context.Background(), SystemInfo{Hostname: "files"}); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		hb := f.heartbeats
		f.mu.Unlock()
		if len(hb) != 1 {
			t.Fatalf("%d heartbeats, want 1", len(hb))
		}
		if got, ok := hb[0]["tools_local"]; !ok || got != enabled {
			t.Errorf("tools_local = %v (present %v), want %v", got, ok, enabled)
		}
	}
}

// Without ENABLE_TOOLS the agent never asks for jobs; with it, it does.
func TestRunCollectsJobsOnlyWithEnableTools(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("ENABLE_TOOLS=%v", enabled), func(t *testing.T) {
			f := newFakeSentinel(t)
			cfg := config{ServerURL: f.srv.URL, AgentID: testAgentID, ServerToken: testToken,
				CheckInterval: time.Hour, RetryAttempts: 1, ToolsEnabled: enabled}
			client := &apiClient{baseURL: cfg.ServerURL, token: testToken, agentID: testAgentID, retries: 1,
				http: &http.Client{Timeout: 5 * time.Second}, toolsLocal: enabled}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				run(ctx, cfg, NewCollector("/"), NewDockerCollector(filepath.Join(t.TempDir(), "no-docker.sock")), client)
				close(done)
			}()
			defer func() {
				cancel()
				<-done
			}()

			waitFor(t, "the first heartbeat", func() bool {
				f.mu.Lock()
				defer f.mu.Unlock()
				return len(f.heartbeats) == 1
			})
			if enabled {
				waitFor(t, "a jobs poll", func() bool { return f.pollCount() >= 1 })
				return
			}
			time.Sleep(200 * time.Millisecond)
			if n := f.pollCount(); n != 0 {
				t.Errorf("%d jobs polls without ENABLE_TOOLS, want none", n)
			}
		})
	}
}
```

- [ ] **Step 4: Run them to verify they fail**

Run: `go test ./cmd/agent/ -v`
Expected: the build fails with `undefined: agentJob`, `undefined: jobEvent`, `undefined: jobFinish`, `undefined: toolRunner`, `undefined: newJobLoop`, `undefined: nextBackoff` and `unknown field toolsLocal in struct literal of type apiClient`.

- [ ] **Step 5: Write the job client and the event buffer**

Create `backend/cmd/agent/jobs_client.go`:

```go
package main

// The agent's half of the job routes (Sentinel's api/agent_jobs_handler.go):
// the wire types, the HTTP client and the buffer of events waiting to be
// posted.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

var (
	errToolsDisabled = errors.New("network tools are switched off for this agent in Sentinel")
	errRunGone       = errors.New("this run is no longer running in Sentinel")
	errTooMuchOutput = errors.New("too much output")
)

// agentJob is a job as jobs/next sends it (toolruns.Job in Sentinel; the
// agent cannot import that package, which pulls in the database layer).
type agentJob struct {
	RunID    string        `json:"run_id"`
	Spec     nettools.Spec `json:"spec"`
	Deadline time.Time     `json:"deadline"`
}

// jobEvent is one event of an events post (toolruns.AgentEvent).
type jobEvent struct {
	Seq  int             `json:"seq"`
	At   time.Time       `json:"at"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// jobFinish is the finish post (toolruns.AgentFinish).
type jobFinish struct {
	Status  string          `json:"status"` // done | failed | refused
	Summary json.RawMessage `json:"summary,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// ---- the HTTP side ----------------------------------------------------------

// jobClient speaks the agent's half of the job routes.
type jobClient struct {
	baseURL string
	token   string
	agentID string
	poll    *http.Client
	post    *http.Client
}

func (c *jobClient) jobPath(runID, what string) string {
	return "/api/v1/agents/" + c.agentID + "/jobs/" + url.PathEscape(runID) + "/" + what
}

// call sends one request and returns the status and up to 1 MB of the body.
func (c *jobClient) call(ctx context.Context, hc *http.Client, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encoding request: %w", err)
		}
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return 0, nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "sentinel-agent/"+version)
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
}

// next collects one job; nil when the poll ended without one.
func (c *jobClient) next(ctx context.Context) (*agentJob, error) {
	status, body, err := c.call(ctx, c.poll, http.MethodGet, "/api/v1/agents/"+c.agentID+"/jobs/next", nil)
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusNoContent:
		return nil, nil
	case status == http.StatusOK:
		var env struct {
			Data agentJob `json:"data"`
		}
		if err := json.Unmarshal(body, &env); err != nil || env.Data.RunID == "" {
			return nil, fmt.Errorf("malformed job from the server: %s", bodySnippet(body))
		}
		return &env.Data, nil
	case status == http.StatusForbidden && errorCode(body) == "tools_disabled":
		return nil, errToolsDisabled
	default:
		return nil, fmt.Errorf("server answered %d: %s", status, bodySnippet(body))
	}
}

// postEvents sends a batch and reports whether Sentinel has cancelled the run.
func (c *jobClient) postEvents(ctx context.Context, runID string, events []jobEvent) (bool, error) {
	if events == nil {
		events = []jobEvent{}
	}
	status, body, err := c.call(ctx, c.post, http.MethodPost, c.jobPath(runID, "events"), map[string]any{"events": events})
	switch {
	case err != nil:
		return false, err
	case status == http.StatusOK:
		var env struct {
			Data struct {
				Cancel bool `json:"cancel"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			return false, fmt.Errorf("malformed reply from the server: %s", bodySnippet(body))
		}
		return env.Data.Cancel, nil
	case status == http.StatusConflict:
		return false, errRunGone
	case status == http.StatusRequestEntityTooLarge:
		return false, errTooMuchOutput
	default:
		return false, fmt.Errorf("server answered %d: %s", status, bodySnippet(body))
	}
}

// finish reports the run's outcome.
func (c *jobClient) finish(ctx context.Context, runID string, f jobFinish) error {
	status, body, err := c.call(ctx, c.post, http.MethodPost, c.jobPath(runID, "finish"), f)
	switch {
	case err != nil:
		return err
	case status == http.StatusOK:
		return nil
	case status == http.StatusConflict:
		return errRunGone
	default:
		return fmt.Errorf("server answered %d: %s", status, bodySnippet(body))
	}
}

// errorCode is the code of a coded error body, {"error": {"code": "…"}}.
func errorCode(body []byte) string {
	var env struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &env) != nil {
		return ""
	}
	var coded struct {
		Code any `json:"code"`
	}
	if json.Unmarshal(env.Error, &coded) != nil {
		return ""
	}
	code, _ := coded.Code.(string)
	return code
}

func bodySnippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// ---- events waiting to be posted --------------------------------------------

// eventBuffer holds a running job's events until a post stores them. Seqs
// start at 1 and never repeat, so a post retried after a lost reply is
// stored once: Sentinel ignores seqs it already has.
type eventBuffer struct {
	mu      sync.Mutex
	seq     int
	pending []jobEvent
	sizes   []int // encoded size of each pending event
}

func (b *eventBuffer) add(ev nettools.Event, at time.Time) {
	data, err := json.Marshal(ev.Data)
	if err != nil {
		data, _ = json.Marshal(map[string]string{"error": err.Error()})
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	e := jobEvent{Seq: b.seq, At: at.UTC(), Type: ev.Type, Data: data}
	encoded, _ := json.Marshal(e)
	b.pending = append(b.pending, e)
	b.sizes = append(b.sizes, len(encoded)+1)
}

// batch returns the oldest pending events that fit in budget bytes: at least
// one, so an event bigger than the budget still goes, on its own.
func (b *eventBuffer) batch(budget int) []jobEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, size := 0, 0
	for n < len(b.pending) && (n == 0 || size+b.sizes[n] <= budget) {
		size += b.sizes[n]
		n++
	}
	return append([]jobEvent(nil), b.pending[:n]...)
}

// drop forgets the oldest n events once a post has stored them.
func (b *eventBuffer) drop(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = b.pending[n:]
	b.sizes = b.sizes[n:]
}

func (b *eventBuffer) empty() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending) == 0
}
```

- [ ] **Step 6: Write the job loop**

Create `backend/cmd/agent/jobs.go`:

```go
package main

// Network tools. With ENABLE_TOOLS=true the agent collects jobs from
// Sentinel with a long poll, runs each with nettools, posts its events back
// while it runs and reports how it ended. Sentinel's own per-agent switch
// must be on too; while it is off, jobs/next answers tools_disabled.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

const (
	// maxConcurrentJobs is how many jobs run at once; Sentinel queues no
	// more than this per agent either.
	maxConcurrentJobs = 2
	// jobPollTimeout: Sentinel holds a poll for up to 25 s.
	jobPollTimeout = 35 * time.Second
	jobPostTimeout = 15 * time.Second
	// jobFlushEvery is how often a running job posts its events. The reply
	// is how a cancel reaches the agent, so a post goes even when empty.
	jobFlushEvery = 250 * time.Millisecond
	// jobMinPoll: a poll that comes back sooner than this without a job (a
	// proxy cutting it short) waits out the rest, so it cannot spin.
	jobMinPoll      = time.Second
	jobDisabledWait = 60 * time.Second
	jobBackoffMin   = 5 * time.Second
	jobBackoffMax   = 60 * time.Second
	// The last events post and the finish are tried this many times, this
	// far apart: without the finish a run only ends when Sentinel times it out.
	jobFinalTries = 3
	jobRetryDelay = 2 * time.Second
	// postBudget keeps an events post well inside Sentinel's 64 KB limit.
	postBudget = 48 << 10
)

// toolRunner runs one tool: *nettools.Runner, or a fake in tests.
type toolRunner interface {
	Run(ctx context.Context, s nettools.Spec, emit nettools.Emitter) (any, error)
}

// jobLoop collects and runs jobs until its context ends.
type jobLoop struct {
	client  *jobClient
	runner  toolRunner
	allowed []*net.IPNet // TOOLS_ALLOWED_TARGETS; empty means no local limit
	now     func() time.Time
	slots   chan struct{}
	jobs    sync.WaitGroup

	// The loop's waits (the job* constants; tests shorten them).
	flushEvery   time.Duration
	minPoll      time.Duration
	disabledWait time.Duration
	backoffMin   time.Duration
	backoffMax   time.Duration
	retryDelay   time.Duration
}

func newJobLoop(cfg config, runner toolRunner) *jobLoop {
	return &jobLoop{
		client: &jobClient{
			baseURL: cfg.ServerURL,
			token:   cfg.ServerToken,
			agentID: cfg.AgentID,
			poll:    &http.Client{Timeout: jobPollTimeout},
			post:    &http.Client{Timeout: jobPostTimeout},
		},
		runner:       runner,
		allowed:      cfg.ToolsAllowed,
		now:          time.Now,
		slots:        make(chan struct{}, maxConcurrentJobs),
		flushEvery:   jobFlushEvery,
		minPoll:      jobMinPoll,
		disabledWait: jobDisabledWait,
		backoffMin:   jobBackoffMin,
		backoffMax:   jobBackoffMax,
		retryDelay:   jobRetryDelay,
	}
}

// run polls for jobs until ctx ends, then waits for running jobs to stop.
//
// It keeps polling while both job slots are busy: Sentinel queues no more
// than two runs per agent anyway, and an agent that stopped polling would
// show as offline there.
func (l *jobLoop) run(ctx context.Context) {
	defer l.jobs.Wait()
	backoff := l.backoffMin
	disabled := false
	for ctx.Err() == nil {
		started := time.Now()
		job, err := l.client.next(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, errToolsDisabled):
			if !disabled {
				log.Printf("network tools are switched off for this agent in Sentinel; asking again every %s", l.disabledWait)
				disabled = true
			}
			backoff = l.backoffMin
			sleepCtx(ctx, l.disabledWait)
		case err != nil:
			log.Printf("collecting tool jobs failed (next try in %s): %v", backoff, err)
			sleepCtx(ctx, backoff)
			backoff = nextBackoff(backoff, l.backoffMax)
		default:
			if disabled {
				log.Println("network tools are switched on for this agent in Sentinel")
				disabled = false
			}
			backoff = l.backoffMin
			if job != nil {
				l.jobs.Add(1)
				go func() {
					defer l.jobs.Done()
					l.handle(ctx, job)
				}()
				continue
			}
			if rest := l.minPoll - time.Since(started); rest > 0 {
				sleepCtx(ctx, rest)
			}
		}
	}
}

// nextBackoff doubles a wait up to limit.
func nextBackoff(cur, limit time.Duration) time.Duration {
	if cur*2 > limit {
		return limit
	}
	return cur * 2
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// check applies the agent's own rules to a job, whatever Sentinel decided:
// the tools' parameter limits (Normalize again, here), the address rules and
// the deadline. It returns the spec to run, or why the job is refused.
func (l *jobLoop) check(job *agentJob) (nettools.Spec, string) {
	spec, err := nettools.Normalize(job.Spec)
	if err != nil {
		return spec, "the agent refused the parameters: " + err.Error()
	}
	spec.TargetIP = strings.TrimSpace(job.Spec.TargetIP)
	if !job.Deadline.After(l.now()) {
		return spec, "the job's deadline passed before it started"
	}
	if spec.TargetIP == "" {
		// Only a lookup through this host's own resolver has no address,
		// and that resolver is always allowed.
		if spec.Tool == nettools.ToolDNS && spec.Params.Server == "" {
			return spec, ""
		}
		return spec, "the job has no target address"
	}
	ip := net.ParseIP(spec.TargetIP).To4()
	if ip == nil {
		return spec, "the target address " + spec.TargetIP + " is not IPv4"
	}
	if nettools.AlwaysBlocked(ip) {
		return spec, spec.TargetIP + " is never probed (metadata, multicast or broadcast address)"
	}
	if len(l.allowed) > 0 && !nettools.InNets(ip, l.allowed) {
		return spec, spec.TargetIP + " is outside TOOLS_ALLOWED_TARGETS on this agent"
	}
	return spec, ""
}

// handle checks one job, runs it inside its deadline while its events are
// posted, and reports how it ended.
func (l *jobLoop) handle(ctx context.Context, job *agentJob) {
	spec, reason := l.check(job)
	if reason != "" {
		log.Printf("refused tool job %s: %s", job.RunID, reason)
		l.report(ctx, job.RunID, jobFinish{Status: "refused", Error: reason})
		return
	}
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-l.slots }()

	// The job's deadline, but never later than the tool's own limit.
	deadline := job.Deadline
	if limit := l.now().Add(nettools.Deadline(spec.Tool)); limit.Before(deadline) {
		deadline = limit
	}
	runCtx, stopTool := context.WithDeadline(ctx, deadline)
	defer stopTool()

	log.Printf("running %s job %s against %s", spec.Tool, job.RunID, spec.Target)
	buf := &eventBuffer{}
	toolDone := make(chan struct{})
	followed := make(chan flushResult, 1)
	go func() { followed <- l.follow(ctx, job.RunID, buf, stopTool, toolDone) }()

	summary, runErr := l.runner.Run(runCtx, spec, func(ev nettools.Event) { buf.add(ev, l.now()) })
	close(toolDone)
	result := <-followed
	if result == flushOK {
		// What the tool emitted since the last tick goes before the finish.
		result = l.flushFinal(ctx, job.RunID, buf)
	}
	switch result {
	case flushGone:
		log.Printf("tool job %s: Sentinel no longer runs it; stopped", job.RunID)
	case flushTooMuch:
		l.report(ctx, job.RunID, jobFinish{Status: "failed", Summary: marshalSummary(summary), Error: errTooMuchOutput.Error()})
	case flushCancelled:
		// Sentinel keeps the run cancelled (ruling 8); this only files the
		// partial summary.
		l.report(ctx, job.RunID, jobFinish{Status: "failed", Summary: marshalSummary(summary), Error: "cancelled"})
	default:
		l.report(ctx, job.RunID, outcome(summary, runErr))
	}
}

// flushResult is how a round of events posts went.
type flushResult int

const (
	flushOK        flushResult = iota // everything pending was stored
	flushRetry                        // a post failed; the events wait for the next one
	flushCancelled                    // Sentinel says the run was cancelled
	flushGone                         // Sentinel no longer runs it (409)
	flushTooMuch                      // Sentinel refused more output (413)
)

// follow posts the job's events every flushEvery until the tool returns
// (done closes). When Sentinel says stop, it stops the tool and says why.
func (l *jobLoop) follow(ctx context.Context, runID string, buf *eventBuffer, stopTool context.CancelFunc, done <-chan struct{}) flushResult {
	tick := time.NewTicker(l.flushEvery)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return flushOK
		case <-ctx.Done():
			return flushOK
		case <-tick.C:
			switch r := l.flush(ctx, runID, buf, true); r {
			case flushCancelled, flushGone, flushTooMuch:
				stopTool()
				return r
			}
		}
	}
}

// flush posts what is pending in batches under postBudget. With always set
// it posts even when nothing is pending, to hear about a cancel.
func (l *jobLoop) flush(ctx context.Context, runID string, buf *eventBuffer, always bool) flushResult {
	for {
		batch := buf.batch(postBudget)
		if len(batch) == 0 && !always {
			return flushOK
		}
		cancel, err := l.client.postEvents(ctx, runID, batch)
		switch {
		case errors.Is(err, errRunGone):
			return flushGone
		case errors.Is(err, errTooMuchOutput):
			return flushTooMuch
		case err != nil:
			log.Printf("tool job %s: posting events failed (will retry): %v", runID, err)
			return flushRetry
		}
		buf.drop(len(batch))
		if cancel {
			return flushCancelled
		}
		if buf.empty() {
			return flushOK
		}
		always = false
	}
}

// flushFinal posts what is left after the tool returned, trying a few times.
func (l *jobLoop) flushFinal(ctx context.Context, runID string, buf *eventBuffer) flushResult {
	r := l.flush(ctx, runID, buf, false)
	for try := 1; try < jobFinalTries && r == flushRetry && ctx.Err() == nil; try++ {
		sleepCtx(ctx, l.retryDelay)
		r = l.flush(ctx, runID, buf, false)
	}
	return r
}

// report sends the finish, trying a few times.
func (l *jobLoop) report(ctx context.Context, runID string, f jobFinish) {
	for try := 1; ; try++ {
		err := l.client.finish(ctx, runID, f)
		if err == nil {
			return
		}
		if errors.Is(err, errRunGone) || ctx.Err() != nil || try == jobFinalTries {
			log.Printf("tool job %s: reporting the result failed: %v", runID, err)
			return
		}
		sleepCtx(ctx, l.retryDelay)
	}
}

// outcome turns what the tool returned into the finish post.
func outcome(summary any, err error) jobFinish {
	f := jobFinish{Status: "done", Summary: marshalSummary(summary)}
	switch {
	case err == nil:
	case errors.Is(err, context.DeadlineExceeded):
		f.Status, f.Error = "failed", "the run reached its deadline"
	default:
		f.Status, f.Error = "failed", err.Error()
	}
	return f
}

func marshalSummary(summary any) json.RawMessage {
	if summary == nil {
		return nil
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return nil
	}
	return data
}
```

- [ ] **Step 7: Read the new settings and report `tools_local`**

Make these edits to `backend/cmd/agent/main.go`.

(a) Imports: add `"net"` after `"log"`, and the `nettools` import after the standard-library block:

```go
	"log"
	"net"
	"net/http"
```

```go
	"syscall"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)
```

(b) In `type config struct`, after `	DockerSocket  string`, add:

```go
	// ToolsEnabled (ENABLE_TOOLS) lets this agent collect network-tool jobs.
	// Sentinel's own switch for the agent must be on as well.
	ToolsEnabled bool
	// ToolsAllowed (TOOLS_ALLOWED_TARGETS) limits the addresses a job may
	// probe, whatever Sentinel asks. Empty means no local limit.
	ToolsAllowed []*net.IPNet
```

(c) At the end of `loadConfig`, replace the final `	return cfg, nil\n}` (after the `RETRY_ATTEMPTS` block) with the following. It adds `envTrue` after the function:

```go
	cfg.ToolsEnabled = envTrue(os.Getenv("ENABLE_TOOLS"))
	if raw := strings.TrimSpace(os.Getenv("TOOLS_ALLOWED_TARGETS")); raw != "" {
		nets, err := nettools.ParseAddressList(raw)
		if err != nil {
			return cfg, fmt.Errorf("TOOLS_ALLOWED_TARGETS must be comma-separated IPv4 addresses and CIDRs: %w", err)
		}
		cfg.ToolsAllowed = nets
	}
	return cfg, nil
}

// envTrue reads a yes/no setting: true, 1 or yes, in any case, mean yes.
func envTrue(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes":
		return true
	}
	return false
}
```

(d) In `main`, the `apiClient` literal becomes:

```go
	client := &apiClient{
		baseURL:    cfg.ServerURL,
		token:      cfg.ServerToken,
		agentID:    cfg.AgentID,
		retries:    cfg.RetryAttempts,
		http:       &http.Client{Timeout: 30 * time.Second},
		toolsLocal: cfg.ToolsEnabled,
	}
```

(e) In `run`, after the initial heartbeat's `if … else { log.Println("registered with server") }` block and before `metricsTicker := …`, insert:

```go

	// Network tools run beside the metrics, in their own loop, only on a
	// host that opted in. run waits for that loop before it returns.
	if cfg.ToolsEnabled {
		log.Printf("network tools enabled; collecting jobs from %s", cfg.ServerURL)
		jobsDone := make(chan struct{})
		go func() {
			defer close(jobsDone)
			newJobLoop(cfg, &nettools.Runner{}).run(ctx)
		}()
		defer func() { <-jobsDone }()
	}
```

(f) In `type apiClient struct`, after `	http    *http.Client`, add:

```go
	// toolsLocal is ENABLE_TOOLS, reported on every heartbeat as tools_local
	// so Sentinel can show both halves of the opt-in.
	toolsLocal bool
```

(g) In `heartbeat`, the body type and value gain the field:

```go
	type heartbeatBody struct {
		SystemInfo
		AgentID      string `json:"agent_id"`
		AgentVersion string `json:"agent_version"`
		ToolsLocal   bool   `json:"tools_local"`
	}
	return c.post(ctx, "/api/v1/agents/heartbeat", heartbeatBody{
		SystemInfo:   info,
		AgentID:      c.agentID,
		AgentVersion: version,
		ToolsLocal:   c.toolsLocal,
	})
```

`service_windows.go` needs no change: the Windows service calls the same `run`.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test -race -count=3 ./cmd/agent/ -v`
Expected: PASS. The whole package takes about 2 s per run; the slowest tests are `TestJobLoopWaitsWhileToolsAreDisabled` (0.3 s) and `TestRunCollectsJobsOnlyWithEnableTools` (0.2 s). The pins:
- `TestJobLoopStopsTheToolWhenSentinelCancels` pins Review Focus 5.
- `TestRunCollectsJobsOnlyWithEnableTools` pins that there is no polling without `ENABLE_TOOLS`.
- `TestJobLoopRefusesJobsItWillNotRun` pins the refusal outside `TOOLS_ALLOWED_TARGETS`.
- `TestJobLoopWaitsWhileToolsAreDisabled`, `TestJobLoopBacksOffAfterFailedPolls` and `TestNextBackoffDoublesUpToTheCap` pin the backoff.

- [ ] **Step 9: Cross-compile for Windows**

Run: `GOOS=windows go vet ./cmd/agent/... && GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/agent`
Expected: no output.

- [ ] **Step 10: Bump the agent version in the Dockerfile**

In `backend/Dockerfile`, make three changes:
- `ARG AGENT_VERSION=1.0.0` becomes `ARG AGENT_VERSION=1.1.0`.
- Both `-X main.version=${AGENT_VERSION:-1.0.0}` become `-X main.version=${AGENT_VERSION:-1.1.0}`.
- In the comment above the `ARG`, `` `${AGENT_VERSION:-1.0.0}` `` becomes `` `${AGENT_VERSION:-1.1.0}` ``.

Check: `grep -n '1\.0\.0' backend/Dockerfile` prints nothing.

- [ ] **Step 11: Commit**

```bash
git add backend/cmd/agent/jobs.go backend/cmd/agent/jobs_client.go backend/cmd/agent/jobs_test.go \
  backend/cmd/agent/jobs_fake_test.go backend/cmd/agent/config_test.go backend/cmd/agent/main.go backend/Dockerfile
git commit -m "feat(tools): agent job loop behind ENABLE_TOOLS, tools_local on the heartbeat

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: Install scripts

All three installers take `ENABLE_TOOLS` (default `false`) and `TOOLS_ALLOWED_TARGETS` (default empty) and hand them to the agent:
- **Linux:** in `/etc/sentinel/agent.conf` (spec amendment 6).
- **Docker:** as `-e` options on the container.
- **Windows:** in the service's environment. `TOOLS_ALLOWED_TARGETS` is added there only when it is set.

The bash scripts refuse a `TOOLS_ALLOWED_TARGETS` holding anything but digits, dots, slashes and commas. The Linux value is written into the config file by an expanding heredoc, so any other character could add lines to it. The tests render each template through the same `installScripts` map that `ServeInstallScriptHandler` executes. They check the new lines, and run `bash -n` on the two bash scripts when bash is installed (they skip otherwise).

**Files:**
- Modify: `backend/internal/api/agent_install_scripts.go` (`bashInstallScript`, `dockerInstallScript`, `windowsInstallScript`)
- Test: `backend/internal/api/agent_install_scripts_test.go` (new)

**Interfaces:**
- Consumes: `installScripts` (`agent_install_handler.go`, a `map[string]*template.Template`, rendered with `map[string]string{"SentinelURL": url}`).
- Produces: the install options `ENABLE_TOOLS=true` and `TOOLS_ALLOWED_TARGETS=…`, which Task 18's add-agent modal puts into the generated commands; the agent reads them (Task 13).

- [ ] **Step 1: Write the failing test**

Create `backend/internal/api/agent_install_scripts_test.go`:

```go
package api

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// renderInstallScript renders an installer the way ServeInstallScriptHandler
// does: its template with the server's address.
func renderInstallScript(t *testing.T, name string) string {
	t.Helper()
	tmpl, ok := installScripts[name]
	if !ok {
		t.Fatalf("no install script %q", name)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"SentinelURL": "http://10.1.20.10:3001"}); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// Each installer takes ENABLE_TOOLS and TOOLS_ALLOWED_TARGETS and hands them
// to the agent: the Linux one in /etc/sentinel/agent.conf, the Docker one as
// container environment, the Windows one in the service environment.
func TestInstallScriptsPassTheToolsOptions(t *testing.T) {
	cases := []struct {
		script string
		want   []string
	}{
		{"server-agent.sh", []string{
			"ENABLE_TOOLS=true",
			`ENABLE_TOOLS="${ENABLE_TOOLS:-false}"`,
			`TOOLS_ALLOWED_TARGETS="${TOOLS_ALLOWED_TARGETS:-}"`,
			`*[!0-9./,]*) die "TOOLS_ALLOWED_TARGETS must be`,
			"\nENABLE_TOOLS=$ENABLE_TOOLS\nTOOLS_ALLOWED_TARGETS=$TOOLS_ALLOWED_TARGETS\nEOF\n",
		}},
		{"server-docker-agent.sh", []string{
			"ENABLE_TOOLS=true",
			`ENABLE_TOOLS="${ENABLE_TOOLS:-false}"`,
			`TOOLS_ALLOWED_TARGETS="${TOOLS_ALLOWED_TARGETS:-}"`,
			`*[!0-9./,]*) die "TOOLS_ALLOWED_TARGETS must be`,
			`-e ENABLE_TOOLS="$ENABLE_TOOLS" \`,
			`-e TOOLS_ALLOWED_TARGETS="$TOOLS_ALLOWED_TARGETS" \`,
			"NET_RAW is in Docker's",
		}},
		{"server-agent.ps1", []string{
			`$env:ENABLE_TOOLS="true"`,
			`if (-not $env:ENABLE_TOOLS) { $env:ENABLE_TOOLS = "false" }`,
			`"ENABLE_TOOLS=$env:ENABLE_TOOLS"`,
			`if ($env:TOOLS_ALLOWED_TARGETS) { $envLines += "TOOLS_ALLOWED_TARGETS=$env:TOOLS_ALLOWED_TARGETS" }`,
		}},
	}
	for _, tc := range cases {
		script := renderInstallScript(t, tc.script)
		for _, want := range tc.want {
			if !strings.Contains(script, want) {
				t.Errorf("%s is missing %q", tc.script, want)
			}
		}
		if !strings.Contains(script, "http://10.1.20.10:3001") {
			t.Errorf("%s lost the server address", tc.script)
		}
	}
}

// The two bash installers still parse after the edits.
func TestBashInstallScriptsParse(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	for _, name := range []string{"server-agent.sh", "server-docker-agent.sh"} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(renderInstallScript(t, name)), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
			t.Errorf("bash -n %s: %v\n%s", name, err, out)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/api/ -run 'TestInstallScripts|TestBashInstallScripts' -v`
Expected: `TestInstallScriptsPassTheToolsOptions` fails, reporting each missing string (for example `server-agent.sh is missing "ENABLE_TOOLS=true"`). `TestBashInstallScriptsParse` passes, or skips without bash.

- [ ] **Step 3: Edit the Linux installer (`bashInstallScript`)**

All three scripts are Go raw strings rendered by `text/template`, so they must not contain a backtick or `{{`.

(a) In the header, after `# Installs the agent to /usr/local/bin, writes a systemd unit, and starts it.`, insert:

```bash
#
# Network tools (optional): add ENABLE_TOOLS=true to let Sentinel run ping,
# traceroute, DNS and port checks from this host once an admin also allows it
# in Sentinel. TOOLS_ALLOWED_TARGETS="10.0.0.0/8,192.168.1.10" limits what the
# agent will probe, whatever Sentinel asks.
```

(b) After `DISK_PATH="${DISK_PATH:-/}"`, insert:

```bash
ENABLE_TOOLS="${ENABLE_TOOLS:-false}"
TOOLS_ALLOWED_TARGETS="${TOOLS_ALLOWED_TARGETS:-}"
```

(c) After the line `[ -n "$SERVER_TOKEN" ] || die "SERVER_TOKEN is not set — copy the command from Sentinel"`, insert:

```bash
# Written into the config file below, so only the characters an address list
# needs: anything else could add lines to it.
case "$TOOLS_ALLOWED_TARGETS" in
  *[!0-9./,]*) die "TOOLS_ALLOWED_TARGETS must be IPv4 addresses and CIDRs separated by commas, e.g. 10.0.0.0/8,192.168.1.10" ;;
esac
```

(d) After `echo "    interval: ${CHECK_INTERVAL}s"`, insert:

```bash
echo "    tools:    $ENABLE_TOOLS"
```

(e) In the `cat > "$CONF_PATH" <<EOF` block, after `DISK_PATH=$DISK_PATH`, insert:

```bash
ENABLE_TOOLS=$ENABLE_TOOLS
TOOLS_ALLOWED_TARGETS=$TOOLS_ALLOWED_TARGETS
```

- [ ] **Step 4: Edit the Docker installer (`dockerInstallScript`)**

(a) After `#   SERVER_TOKEN="srv_..." AGENT_ID="agent_..." sudo -E bash ./server-docker-agent.sh`, insert:

```bash
#
# Network tools (optional): add ENABLE_TOOLS=true to let Sentinel run ping,
# traceroute, DNS and port checks from this host once an admin also allows it
# in Sentinel. TOOLS_ALLOWED_TARGETS="10.0.0.0/8,192.168.1.10" limits what the
# agent will probe, whatever Sentinel asks.
```

(b) After `IMAGE="${AGENT_IMAGE:-alpine:latest}"`, insert:

```bash
ENABLE_TOOLS="${ENABLE_TOOLS:-false}"
TOOLS_ALLOWED_TARGETS="${TOOLS_ALLOWED_TARGETS:-}"
```

(c) After the `SERVER_TOKEN` check line (same text as in the Linux script), insert:

```bash
case "$TOOLS_ALLOWED_TARGETS" in
  *[!0-9./,]*) die "TOOLS_ALLOWED_TARGETS must be IPv4 addresses and CIDRs separated by commas, e.g. 10.0.0.0/8,192.168.1.10" ;;
esac
```

(d) At the end of the comment above `docker run -d \`, which ends `so it can list containers without being able to control them.`, add:

```bash
# Network tools need raw ICMP for ping and traceroute: NET_RAW is in Docker's
# default capabilities, and --network host makes the probes leave from this
# host's own addresses.
```

(e) After `  -e RETRY_ATTEMPTS="$RETRY_ATTEMPTS" \`, insert:

```bash
  -e ENABLE_TOOLS="$ENABLE_TOOLS" \
  -e TOOLS_ALLOWED_TARGETS="$TOOLS_ALLOWED_TARGETS" \
```

- [ ] **Step 5: Edit the Windows installer (`windowsInstallScript`)**

(a) After `#   iwr -useb <url>/scripts/server-agent.ps1 | iex`, insert:

```powershell
#
# Network tools (optional): set $env:ENABLE_TOOLS="true" first to let Sentinel
# run ping, traceroute, DNS and port checks from this host once an admin also
# allows it in Sentinel; $env:TOOLS_ALLOWED_TARGETS="10.0.0.0/8,192.168.1.10"
# limits what the agent will probe, whatever Sentinel asks.
```

(b) After `if (-not $env:DISK_PATH) { $env:DISK_PATH = "C:\" }`, insert:

```powershell
if (-not $env:ENABLE_TOOLS) { $env:ENABLE_TOOLS = "false" }
```

(c) After `Write-Host "    interval: $($env:CHECK_INTERVAL)s"`, insert:

```powershell
Write-Host "    tools:    $env:ENABLE_TOOLS"
```

(d) The end of the `$envLines` array becomes the following. Note the comma now needed after the `DISK_PATH` line:

```powershell
    "RETRY_ATTEMPTS=$env:RETRY_ATTEMPTS",
    "DISK_PATH=$env:DISK_PATH",
    "ENABLE_TOOLS=$env:ENABLE_TOOLS"
)
# Only when set: an empty value in a service environment is best left out.
if ($env:TOOLS_ALLOWED_TARGETS) { $envLines += "TOOLS_ALLOWED_TARGETS=$env:TOOLS_ALLOWED_TARGETS" }
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestInstallScripts|TestBashInstallScripts' -v`
Expected: PASS for both (`TestBashInstallScriptsParse` may report SKIP where bash is missing, e.g. in an alpine Go container).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/agent_install_scripts.go backend/internal/api/agent_install_scripts_test.go
git commit -m "feat(tools): ENABLE_TOOLS and TOOLS_ALLOWED_TARGETS in the agent installers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

### Task 15: Frontend data layer

The browser side of the contract. This task adds the types, the hooks for every tool endpoint, the hook that follows a run over SSE, and the pure helpers the pages share: labels, the `/tools` URL, the form defaults and checks, and the one-line result of a run. It adds no components. The frontend has no unit-test runner, so a throwaway assertion script checks the helpers. It is bundled with the esbuild that Vite already installs and is not committed.

**Files:**
- Create: `frontend/src/types/netTools.ts`
- Create: `frontend/src/utils/netTools.ts`
- Create: `frontend/src/hooks/useNetTools.ts`
- Create: `frontend/src/hooks/useRunStream.ts`
- Modify: `frontend/src/services/api.ts` (export the base URL)
- Modify: `frontend/src/context/AuthContext.tsx` (`CurrentUser.net_tools`)
- Modify: `frontend/src/hooks/useAgents.ts` (`Agent.tools_enabled`, `Agent.tools_local`)
- Modify: `frontend/src/hooks/useUserManagement.ts` (`ManagedUser.net_tools`)
- Test: a throwaway `check.ts` in a temp directory (not committed), plus the frontend gate

**Interfaces:**
- Consumes (HTTP, from Tasks 6, 11 and 12). Every success body is `{success: true, data: …}`. Tool errors are `{success: false, error: {code, message}}`, and the axios interceptor turns them into `ApiError {status, message, code, details}`, where `details` is the whole body.
  - `GET /api/v1/tools/vantages` → `data: {vantages: VantageView[], allowlist_empty: boolean}`.
  - `POST /api/v1/tools/runs` with a `CreateToolRunRequest` body → 201 `data: ToolRun`. Refusals are 403/409/422/429 with a code. The per-user limiter's 429 is `{error: "too many requests; please wait a moment and try again"}` (a string, ruling 11).
  - `GET /api/v1/tools/runs?tool=&status=&mine=1&agent_id=&target=&limit=&offset=` → `data: ToolRun[]`, with the `X-Total-Count` header.
  - `GET /api/v1/tools/runs/:id` → `data: {run, events}`; `POST /api/v1/tools/runs/:id/cancel` → `data: ToolRun`.
  - `GET /api/v1/tools/runs/:id/events` (SSE). Frames: `event` (id = seq, data = a `ToolRunEvent`), `status` (data `{"status": "running"}`) and `end` (data = the final `ToolRun`). The server closes the stream after `end`. A reconnect sends `Last-Event-ID`, and the server replays everything after it.
  - `GET` and `PUT /api/v1/settings/net-tools` → `data: NetToolsSettings`. A bad allowlist answers 422 `{success: false, error: {code: "invalid_allowlist", message}, entries: [{entry, message}]}`.
  - `PATCH /api/v1/users/:id/net-tools` and `PUT /api/v1/agents/:agent_id/tools`, both with the body `{enabled}`.
  - `GET /api/v1/auth/me` → `data` gains `net_tools: boolean`. `GET /api/v1/agents` → each agent gains `tools_enabled: boolean` and `tools_local: boolean | null`.
- Produces (`@/types/netTools`): every type in the contract's "Frontend (TypeScript)" block, plus `HopProbe { round: number; ttl: number; addr?: string; rtt_ms?: number; reached: boolean }` (Contract change 4).
- Produces (`@/services/api`): `export const apiBaseURL: string`.
- Produces (`@/hooks/useNetTools`), exactly as the contract: `useVantages()`, `useToolRuns(filter)`, `useToolRun(id)`, `createToolRun(req)`, `cancelToolRun(id)`, `useNetToolsSettings()`, `setUserNetTools(userId, enabled)` and `setAgentTools(agentId, enabled)`.
- Produces (`@/hooks/useRunStream`): `interface RunStream { events: ToolRunEvent[]; status: ToolRunStatus | null; run: ToolRun | null; error: string | null }` and `useRunStream(runId: string | null): RunStream`.
- Produces (`@/utils/netTools`):
  - The contract's `canUseNetTools`, `runResultLine`, `statusLabel`, `vantageReasonText`, `TOOL_LABEL`, `parseToolsQuery`, `toolsQuery` (Contract change 3) and `isFinalStatus`.
  - Constants: `NET_TOOLS: NetTool[]`, `TOOL_RUN_STATUSES: ToolRunStatus[]`, `HISTORY_PAGE_SIZE = 50`, `RETENTION_MIN_DAYS = 1`, `RETENTION_MAX_DAYS = 365`, `DNS_RECORD_TYPES: string[]`, `DEFAULT_PARAMS: Record<NetTool, ToolParams>`, `PING_MAX_TOTAL_MS = 115000` and `MAX_PORTS = 1024`.
  - The numeric form fields: `type NumberKey`, `interface NumberField { key; label; min; max; step?; unit? }` and `NUMBER_FIELDS: Record<NetTool, NumberField[]>`.
  - Form helpers: `paramError(tool, params): string | null`, `cleanParams(tool, params): ToolParams`, `vantageKey(v: Vantage): string` and `vantageFromKey(key: string): Vantage`.
  - Targets: `monitorHost(m: {type: string; url: string}): string | null`, `interface KnownHost { value; label }`, `knownHosts(devices, monitors, agents): KnownHost[]` and `runTargetText(run): string`.
  - Status: `runStatusText(run, events): string`, `STATUS_PILL: Record<ToolRunStatus, string>` and `STATUS_TEXT: Record<ToolRunStatus, string>`.
  - Admin screens: `agentToolsStateText(local: boolean | null | undefined): string`, `allowlistErrors(details?: Record<string, unknown>): AllowlistEntryError[]`, `interface LineError { line: number | null; entry; message }` and `lineErrors(text, errors): LineError[]`.
  - Formatting: `formatMs(ms: number | null | undefined): string` and `formatLoss(pct: number): string`.
- Produces (types): `CurrentUser.net_tools?: boolean`, `Agent.tools_enabled: boolean`, `Agent.tools_local: boolean | null` and `ManagedUser.net_tools?: boolean`.

`utils/netTools.ts` must stay free of runtime imports from `services/api` or any hook: the throwaway check bundles it for Node, where `import.meta.env` does not exist. Type-only imports are fine, because esbuild erases them.

- [ ] **Step 1: Run the frontend gate on the untouched tree**

This installs `frontend/node_modules`, which the check below uses for esbuild, and shows a green baseline. From the repo root (never run npm as root):

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output.

- [ ] **Step 2: Write the helper check (throwaway, outside the repo)**

Keep this shell open: Steps 3 and 7 reuse `$CHECK_DIR`.

```bash
export CHECK_DIR=$(mktemp -d)
cat > "$CHECK_DIR/check.ts" <<'EOF'
import assert from 'node:assert/strict'
import {
  agentToolsStateText,
  allowlistErrors,
  canUseNetTools,
  cleanParams,
  DEFAULT_PARAMS,
  formatMs,
  isFinalStatus,
  knownHosts,
  lineErrors,
  monitorHost,
  paramError,
  parseToolsQuery,
  runResultLine,
  runStatusText,
  runTargetText,
  statusLabel,
  toolsQuery,
  vantageFromKey,
  vantageKey,
  vantageReasonText,
} from '@/utils/netTools'
import type { ToolRun, ToolRunEvent } from '@/types/netTools'

const base: ToolRun = {
  id: 'r1', tool: 'ping', status: 'done', user_id: 'u1', username: 'alice',
  vantage_kind: 'sentinel', agent_id: null, vantage_name: 'Sentinel',
  target: 'fileserver', target_ip: '10.0.0.5',
  params: { count: 5, interval_ms: 1000, timeout_ms: 2000, size: 56 },
  summary: null, error: null, event_count: 0,
  created_at: '2026-10-05T10:00:00Z', started_at: '2026-10-05T10:00:00Z',
  finished_at: '2026-10-05T10:00:05Z', deadline: '2026-10-05T10:02:00Z',
}
const run = (patch: Partial<ToolRun>): ToolRun => ({ ...base, ...patch })
const ev = (seq: number, type: string, data: unknown): ToolRunEvent => ({ seq, at: '2026-10-05T10:00:01Z', type, data })

// Who may use the tools: admins always, others only with the grant.
assert.equal(canUseNetTools(null), false)
assert.equal(canUseNetTools({ is_admin: true }), true)
assert.equal(canUseNetTools({ is_admin: false, net_tools: true }), true)
assert.equal(canUseNetTools({ is_admin: false }), false)

// Statuses and reasons.
assert.equal(isFinalStatus('queued'), false)
assert.equal(isFinalStatus('running'), false)
assert.equal(isFinalStatus('done'), true)
assert.equal(isFinalStatus('interrupted'), true)
assert.equal(statusLabel('timed_out'), 'Timed out')
assert.equal(vantageReasonText('offline'), 'offline (not collecting jobs)')
assert.equal(agentToolsStateText(null), "This agent's version can't run tools — update it")
assert.equal(agentToolsStateText(undefined), "This agent's version can't run tools — update it")
assert.equal(agentToolsStateText(false), 'Enabled on the server: no — add ENABLE_TOOLS=true to the agent config')
assert.equal(agentToolsStateText(true), 'Enabled on the server: yes')

// One-line results (avg to one decimal; loss to at most one decimal).
const ping = (s: object) =>
  run({ summary: { sent: 5, received: 5, loss_pct: 0, min_ms: 1.9, avg_ms: 2.06, max_ms: 2.3, jitter_ms: 0.1, ...s } as ToolRun['summary'] })
assert.equal(runResultLine(ping({})), '0% loss, 2.1 ms avg')
assert.equal(runResultLine(ping({ sent: 3, received: 2, loss_pct: 33.3, avg_ms: 10 })), '33.3% loss, 10.0 ms avg')
assert.equal(runResultLine(ping({ received: 0, loss_pct: 100, min_ms: null, avg_ms: null, max_ms: null, jitter_ms: null })), '100% loss')
assert.equal(runResultLine(run({ tool: 'traceroute', summary: { reached: true, hop_count: 9, hops: [] } })), 'reached in 9 hops')
assert.equal(runResultLine(run({ tool: 'traceroute', summary: { reached: true, hop_count: 1, hops: [] } })), 'reached in 1 hop')
assert.equal(runResultLine(run({ tool: 'traceroute', summary: { reached: false, hop_count: 12, hops: [] } })), 'not reached (12 hops)')
assert.equal(runResultLine(run({ tool: 'dns', summary: { server: '10.0.0.53:53', rcode: 'NOERROR', answer_count: 2, rtt_ms: 3 } })), 'NOERROR, 2 answers')
assert.equal(runResultLine(run({ tool: 'dns', summary: { server: '10.0.0.53:53', rcode: 'NOERROR', answer_count: 1, rtt_ms: 3 } })), 'NOERROR, 1 answer')
assert.equal(runResultLine(run({ tool: 'dns', summary: { server: '10.0.0.53:53', rcode: 'NXDOMAIN', answer_count: 0, rtt_ms: 3 } })), 'NXDOMAIN, 0 answers')
assert.equal(runResultLine(run({ tool: 'tcp', summary: { total: 100, open: 3, closed: 90, filtered: 7, open_ports: [22, 80, 443] } })), '3 open of 100')
assert.equal(runResultLine(run({ status: 'failed', error: 'boom' })), 'Failed')
assert.equal(runResultLine(run({ summary: null })), 'Done')

// The status line.
assert.equal(runStatusText(run({ status: 'queued', vantage_name: 'file-server' }), []), 'Waiting for file-server to pick up…')
const trace = run({ tool: 'traceroute', status: 'running', params: { max_hops: 30, rounds: 5, timeout_ms: 1000 } })
assert.equal(runStatusText(trace, []), 'Running — round 1 of 5')
assert.equal(
  runStatusText(trace, [ev(1, 'round_done', { round: 1, hops: [] }), ev(2, 'round_done', { round: 2, hops: [] })]),
  'Running — round 3 of 5',
)
assert.equal(
  runStatusText(run({ status: 'running' }), [ev(1, 'reply', { seq: 1, rtt_ms: 1, ttl: 64, from: '10.0.0.5' }), ev(2, 'timeout', { seq: 2 })]),
  'Running — 2 of 5 probes',
)
assert.equal(
  runStatusText(run({ tool: 'tcp', status: 'running', params: { ports: 'common', timeout_ms: 1500 } }), [
    ev(1, 'start', { total: 100 }),
    ev(2, 'port', { port: 22, state: 'open' }),
  ]),
  'Running — 1 of 100 ports',
)
assert.equal(runStatusText(ping({}), []), 'Done — 0% loss, 2.1 ms avg')
assert.equal(
  runStatusText(run({ status: 'failed', error: "the agent didn't pick up the job (offline?)" }), []),
  "Failed — the agent didn't pick up the job (offline?)",
)
assert.equal(runStatusText(run({ status: 'cancelled' }), []), 'Cancelled')

// What was targeted.
assert.equal(runTargetText(base), 'fileserver → 10.0.0.5')
assert.equal(runTargetText(run({ target: '10.0.0.5' })), '10.0.0.5')
assert.equal(runTargetText(run({ tool: 'dns', target: 'example.org', target_ip: null, params: { record_type: 'A' } })), 'example.org')
assert.equal(
  runTargetText(run({ tool: 'dns', target: 'example.org', target_ip: '10.0.0.53', params: { record_type: 'MX', server: '10.0.0.53' } })),
  'example.org @ 10.0.0.53',
)

// The /tools URL contract (ruling 18).
assert.equal(
  toolsQuery({ tool: 'ping', target: '10.0.0.1', vantage: { kind: 'agent', agent_id: 'agent_ab12' } }),
  '?tool=ping&target=10.0.0.1&from=agent%3Aagent_ab12',
)
assert.equal(toolsQuery({ vantage: { kind: 'agent', agent_id: 'agent_x' } }), '?from=agent%3Aagent_x')
assert.equal(toolsQuery({}), '')
const again = toolsQuery({ tool: 'tcp', target: 'nas', vantage: { kind: 'sentinel' }, params: { ports: '22,445', timeout_ms: 800 } })
assert.deepEqual(parseToolsQuery(new URLSearchParams(again)), {
  tool: 'tcp', target: 'nas', vantage: { kind: 'sentinel' }, params: { timeout_ms: 800, ports: '22,445' },
})
assert.deepEqual(parseToolsQuery(new URLSearchParams('tool=bogus&from=agent%3A&target=%20%20')), {})
assert.deepEqual(
  parseToolsQuery(new URLSearchParams('params=' + encodeURIComponent('{"count":10,"ports":"22","bogus":1,"interval_ms":"x"}'))),
  { params: { count: 10, ports: '22' } },
)
assert.deepEqual(parseToolsQuery(new URLSearchParams('tool=dns&params=notjson')), { tool: 'dns' })
assert.equal(vantageKey({ kind: 'sentinel' }), 'sentinel')
assert.equal(vantageKey({ kind: 'agent', agent_id: 'agent_1' }), 'agent:agent_1')
assert.deepEqual(vantageFromKey('agent:agent_1'), { kind: 'agent', agent_id: 'agent_1' })
assert.deepEqual(vantageFromKey('sentinel'), { kind: 'sentinel' })

// Known hosts: a monitor's host, IPv4 sorted numerically, one row per host.
assert.equal(monitorHost({ type: 'http', url: 'https://intranet.example.org:8443/health' }), 'intranet.example.org')
assert.equal(monitorHost({ type: 'tcp', url: '10.0.0.5:3389' }), '10.0.0.5')
assert.equal(monitorHost({ type: 'ping', url: ' 10.0.0.1 ' }), '10.0.0.1')
assert.equal(monitorHost({ type: 'dns', url: 'example.org' }), 'example.org')
assert.equal(monitorHost({ type: 'webhook', url: 'https://hooks.example.org/x' }), null)
assert.equal(monitorHost({ type: 'http', url: 'not a url' }), null)
assert.deepEqual(
  knownHosts(
    [{ name: 'core-sw', host: '10.0.0.2' }],
    [
      { name: 'Intranet', type: 'http', url: 'https://intranet.example.org/' },
      { name: 'Core ping', type: 'ping', url: '10.0.0.2' },
    ],
    [
      { name: 'file-server', ip_address: '10.0.0.10' },
      { name: 'new-box', ip_address: null },
    ],
  ),
  [
    { value: '10.0.0.2', label: 'core-sw (device), Core ping (monitor)' },
    { value: '10.0.0.10', label: 'file-server (server)' },
    { value: 'intranet.example.org', label: 'Intranet (monitor)' },
  ],
)

// Form checks mirror nettools.Normalize, ping's 115 s rule included:
// 100 x 5000 + 2000 and 23 x 5000 + 2000 = 117000 are refused, 22 x 5000 + 2000 = 112000 is fine.
assert.equal(paramError('ping', DEFAULT_PARAMS.ping), null)
assert.equal(paramError('ping', { ...DEFAULT_PARAMS.ping, count: 101 }), 'Count must be between 1 and 100')
assert.equal(paramError('ping', { ...DEFAULT_PARAMS.ping, count: undefined }), 'Count must be between 1 and 100')
assert.equal(paramError('ping', { count: 100, interval_ms: 5000, timeout_ms: 2000, size: 56 }), 'Count × interval is too long: at most 115 seconds')
assert.equal(paramError('ping', { count: 23, interval_ms: 5000, timeout_ms: 2000, size: 56 }), 'Count × interval is too long: at most 115 seconds')
assert.equal(paramError('ping', { count: 22, interval_ms: 5000, timeout_ms: 2000, size: 56 }), null)
assert.equal(paramError('ping', { ...DEFAULT_PARAMS.ping, size: 0 }), null)
assert.equal(paramError('ping', { ...DEFAULT_PARAMS.ping, size: 1473 }), 'Payload size must be between 0 and 1472 bytes')
assert.equal(paramError('ping', { ...DEFAULT_PARAMS.ping, interval_ms: 150 }), 'Interval must be between 200 and 5000 ms')
assert.equal(paramError('traceroute', { ...DEFAULT_PARAMS.traceroute, max_hops: 0 }), 'Max hops must be between 1 and 30')
assert.equal(paramError('traceroute', { ...DEFAULT_PARAMS.traceroute, rounds: 2.5 }), 'Rounds must be between 1 and 10')
const badServer = 'DNS server must be an IPv4 address, optionally with :port'
assert.equal(paramError('dns', { record_type: 'A', server: 'dns.example' }), badServer)
assert.equal(paramError('dns', { record_type: 'A', server: '256.0.0.1' }), badServer)
assert.equal(paramError('dns', { record_type: 'A', server: '10.0.0.53:0' }), badServer)
assert.equal(paramError('dns', { record_type: 'A', server: '10.0.0.53:5353' }), null)
assert.equal(paramError('dns', { record_type: 'A', server: '' }), null)
const badPorts = 'Ports must be a list like 22,80,443,8000-8100, or "common"'
assert.equal(paramError('tcp', { ports: '22,80,x', timeout_ms: 1500 }), badPorts)
assert.equal(paramError('tcp', { ports: '10-5', timeout_ms: 1500 }), badPorts)
assert.equal(paramError('tcp', { ports: '0', timeout_ms: 1500 }), badPorts)
assert.equal(paramError('tcp', { ports: '1-1025', timeout_ms: 1500 }), 'At most 1024 ports per run')
assert.equal(paramError('tcp', { ports: '1-1024, 80', timeout_ms: 1500 }), null)
assert.equal(paramError('tcp', { ports: 'common', timeout_ms: 1500 }), null)
assert.equal(paramError('tcp', { ports: '', timeout_ms: 1500 }), null)
assert.equal(paramError('tcp', { ports: 'common', timeout_ms: 400 }), 'Per-port timeout must be between 500 and 5000 ms')

// Only the tool's own parameters are sent.
assert.deepEqual(
  cleanParams('ping', { count: 5, interval_ms: 1000, timeout_ms: 2000, size: 0, ports: '22', record_type: 'A' }),
  { count: 5, interval_ms: 1000, timeout_ms: 2000, size: 0 },
)
assert.deepEqual(cleanParams('dns', { record_type: 'mx', server: ' ' }), { record_type: 'MX' })
assert.deepEqual(cleanParams('dns', { record_type: 'A', server: ' 10.0.0.53 ' }), { record_type: 'A', server: '10.0.0.53' })
assert.deepEqual(cleanParams('tcp', { ports: ' ', timeout_ms: 1500 }), { ports: 'common', timeout_ms: 1500 })
assert.deepEqual(cleanParams('tcp', { ports: 'COMMON', timeout_ms: 1500 }), { ports: 'common', timeout_ms: 1500 })

// The allowlist's 422 body, and which line each bad entry is on.
assert.deepEqual(allowlistErrors(undefined), [])
assert.deepEqual(allowlistErrors({ entries: 'x' }), [])
const bad = [
  { entry: '10.0.0.0/7', message: 'too broad: use /8 or narrower' },
  { entry: 'Bad Host!', message: 'not a valid IPv4 address, CIDR or host name' },
]
assert.deepEqual(allowlistErrors({ success: false, entries: bad }), bad)
assert.deepEqual(lineErrors('10.0.0.0/24\n\n10.0.0.0/7\n bad host! ', bad), [
  { line: 3, entry: '10.0.0.0/7', message: 'too broad: use /8 or narrower' },
  { line: 4, entry: 'Bad Host!', message: 'not a valid IPv4 address, CIDR or host name' },
])
assert.deepEqual(lineErrors('10.0.0.1', [{ entry: 'gone', message: 'x' }]), [{ line: null, entry: 'gone', message: 'x' }])

// Milliseconds.
assert.equal(formatMs(null), '—')
assert.equal(formatMs(0.054), '0.05 ms')
assert.equal(formatMs(2.06), '2.1 ms')
assert.equal(formatMs(123.4), '123 ms')

console.log('netTools helpers: all checks passed')
EOF
```

The expected values are worked out by hand:
- Ping's `avg_ms` 2.06 prints to one decimal as 2.1. A 2-of-3 loss arrives from the server already rounded to 33.3.
- `toolsQuery` uses `URLSearchParams`, which percent-encodes `:` as `%3A`. `parseToolsQuery` decodes it again. An `agent:` with no id, an unknown tool and a blank target are each dropped.
- `knownHosts` merges 10.0.0.2 from the device and the ping monitor. It sorts 10.0.0.2 before 10.0.0.10 (numerically, not as text) and names after addresses.
- `lineErrors` matches case-insensitively, so `Bad Host!` from the server finds ` bad host! ` on line 4. The blank line 2 still counts.

- [ ] **Step 3: Run it to see it fail**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app:ro -v "$CHECK_DIR":/check -w /app node:20-alpine sh -c "node_modules/.bin/esbuild /check/check.ts --bundle --platform=node --format=esm --jsx=automatic --tsconfig=/app/tsconfig.json --log-level=warning --outfile=/tmp/check.mjs && node /tmp/check.mjs"
```

Expected: FAIL, with esbuild reporting `Could not resolve "@/utils/netTools"`.

- [ ] **Step 4: Add the types**

Create `frontend/src/types/netTools.ts`:

```ts
// Network tools (S1): what the /tools API and its live stream carry. These
// mirror internal/nettools (events and summaries), internal/toolruns
// (vantages and requests) and models.ToolRun; field names are the backend's
// JSON tags. Go sends a nil slice as null, so readers guard list fields.

export type NetTool = 'ping' | 'traceroute' | 'dns' | 'tcp'

export type ToolRunStatus =
  | 'queued'
  | 'running'
  | 'done'
  | 'failed'
  | 'refused'
  | 'cancelled'
  | 'timed_out'
  | 'interrupted'

export type VantageKind = 'sentinel' | 'agent'

export type VantageReason = 'server_disabled' | 'agent_too_old' | 'tools_off' | 'not_enabled_on_server' | 'offline'

/** Every tool's parameters; a run uses only its own tool's fields. */
export interface ToolParams {
  count?: number
  interval_ms?: number
  /** Ping payload bytes; 0 is valid. */
  size?: number
  max_hops?: number
  rounds?: number
  /** Per probe: ping, traceroute and tcp. */
  timeout_ms?: number
  record_type?: string
  /** DNS: empty for the vantage's own resolver, else "a.b.c.d" or "a.b.c.d:port". */
  server?: string
  /** TCP: "22,80,443,8000-8100" or "common". */
  ports?: string
}

export interface Vantage {
  kind: VantageKind
  /** The readable agent id (agent_…) when kind is 'agent'. */
  agent_id?: string
}

export interface VantageView {
  kind: VantageKind
  agent_id?: string
  name: string
  ready: boolean
  reason?: VantageReason
}

export interface VantagesResponse {
  vantages: VantageView[]
  /** Grant holders cannot read the settings, so the banner reads this. */
  allowlist_empty: boolean
}

export interface CreateToolRunRequest {
  tool: NetTool
  vantage: Vantage
  target: string
  params: ToolParams
}

export interface ToolRun {
  id: string
  tool: NetTool
  status: ToolRunStatus
  user_id: string | null
  username: string
  vantage_kind: VantageKind
  /** The readable agent id, kept after the agent is deleted. */
  agent_id: string | null
  vantage_name: string
  /** What the user typed. */
  target: string
  /** The address probed (the named DNS server for dns); null for a lookup
   *  through the vantage's own resolver. */
  target_ip: string | null
  params: ToolParams
  summary: PingSummary | TraceSummary | DNSSummary | TCPSummary | null
  error: string | null
  event_count: number
  created_at: string
  started_at: string | null
  finished_at: string | null
  deadline: string
}

export interface ToolRunEvent {
  seq: number
  at: string
  type: string
  data: unknown
}

export interface ToolRunDetail {
  run: ToolRun
  events: ToolRunEvent[]
}

// ---- ping: reply, timeout and error events

export interface PingReply {
  seq: number
  rtt_ms: number
  ttl: number
  from: string
}

export interface PingTimeout {
  seq: number
}

export interface PingError {
  seq: number
  message: string
}

export interface PingSummary {
  sent: number
  received: number
  loss_pct: number
  min_ms: number | null
  avg_ms: number | null
  max_ms: number | null
  jitter_ms: number | null
}

// ---- traceroute: hop, round_done and hop_name events

export interface HopProbe {
  round: number
  ttl: number
  /** Absent on a timeout. */
  addr?: string
  rtt_ms?: number
  reached: boolean
}

export interface HopStats {
  ttl: number
  addrs: string[]
  name?: string
  sent: number
  received: number
  loss_pct: number
  last_ms: number | null
  avg_ms: number | null
  best_ms: number | null
  worst_ms: number | null
  stdev_ms: number | null
}

export interface RoundDone {
  round: number
  /** Cumulative after this round (ruling 4). */
  hops: HopStats[]
}

export interface HopName {
  ttl: number
  addr: string
  name: string
}

export interface TraceSummary {
  reached: boolean
  hop_count: number
  hops: HopStats[]
}

// ---- dns: one answer event

export interface DNSRecord {
  name: string
  type: string
  ttl: number
  data: string
}

export interface DNSAnswer {
  /** ip:port asked. */
  server: string
  rcode: string
  authoritative: boolean
  /** The UDP answer was truncated. */
  truncated: boolean
  /** The answer shown came over TCP. */
  tcp: boolean
  rtt_ms: number
  answer: DNSRecord[]
  authority: DNSRecord[]
  additional: DNSRecord[]
}

export interface DNSSummary {
  server: string
  rcode: string
  answer_count: number
  rtt_ms: number
}

// ---- tcp: start, then one port event per port

export type PortState = 'open' | 'closed' | 'filtered'

export interface ScanStart {
  total: number
}

export interface PortResult {
  port: number
  state: PortState
  rtt_ms?: number
  service?: string
}

export interface TCPSummary {
  total: number
  open: number
  closed: number
  filtered: number
  open_ports: number[]
}

// ---- admin

export interface NetToolsSettings {
  allowlist: string[]
  server_enabled: boolean
  retention_days: number
}

export interface AllowlistEntryError {
  entry: string
  message: string
}

export interface ToolRunFilter {
  tool?: NetTool
  /** Only the signed-in user's runs. */
  mine?: boolean
  status?: ToolRunStatus
  /** Readable agent id. */
  agent_id?: string
  /** Exact target or address. */
  target?: string
  limit?: number
  offset?: number
}
```

- [ ] **Step 5: Add the helpers**

Create `frontend/src/utils/netTools.ts`:

```ts
// Pure helpers for the network tools screens: labels, the /tools URL
// contract, the form's defaults and checks, and the one-line descriptions of
// a run. Nothing here touches the network, so the pages and the throwaway
// checks share one implementation.

import { colors } from '@/utils/colors'
import type {
  AllowlistEntryError,
  DNSSummary,
  NetTool,
  PingSummary,
  RoundDone,
  ScanStart,
  TCPSummary,
  ToolParams,
  ToolRun,
  ToolRunEvent,
  ToolRunStatus,
  TraceSummary,
  Vantage,
  VantageReason,
} from '@/types/netTools'

export const NET_TOOLS: NetTool[] = ['ping', 'traceroute', 'dns', 'tcp']

export const TOOL_LABEL: Record<NetTool, string> = {
  ping: 'Ping',
  traceroute: 'Traceroute',
  dns: 'DNS',
  tcp: 'Ports',
}

export const TOOL_RUN_STATUSES: ToolRunStatus[] = [
  'queued',
  'running',
  'done',
  'failed',
  'refused',
  'cancelled',
  'timed_out',
  'interrupted',
]

/** Runs per page of the history table. */
export const HISTORY_PAGE_SIZE = 50

/** Bounds of the run-history retention setting. */
export const RETENTION_MIN_DAYS = 1
export const RETENTION_MAX_DAYS = 365

export const DNS_RECORD_TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'NS', 'TXT', 'SOA', 'SRV', 'PTR', 'CAA']

/** The form's starting values: the backend's defaults (nettools.Normalize). */
export const DEFAULT_PARAMS: Record<NetTool, ToolParams> = {
  ping: { count: 5, interval_ms: 1000, timeout_ms: 2000, size: 56 },
  traceroute: { max_hops: 30, rounds: 5, timeout_ms: 1000 },
  dns: { record_type: 'A', server: '' },
  tcp: { ports: 'common', timeout_ms: 1500 },
}

export type NumberKey = 'count' | 'interval_ms' | 'size' | 'max_hops' | 'rounds' | 'timeout_ms'

export interface NumberField {
  key: NumberKey
  label: string
  min: number
  max: number
  step?: number
  unit?: string
}

/** Each tool's numeric options, with the backend's limits. */
export const NUMBER_FIELDS: Record<NetTool, NumberField[]> = {
  ping: [
    { key: 'count', label: 'Count', min: 1, max: 100 },
    { key: 'interval_ms', label: 'Interval', min: 200, max: 5000, step: 100, unit: 'ms' },
    { key: 'timeout_ms', label: 'Probe timeout', min: 500, max: 5000, step: 100, unit: 'ms' },
    { key: 'size', label: 'Payload size', min: 0, max: 1472, unit: 'bytes' },
  ],
  traceroute: [
    { key: 'max_hops', label: 'Max hops', min: 1, max: 30 },
    { key: 'rounds', label: 'Rounds', min: 1, max: 10 },
    { key: 'timeout_ms', label: 'Probe timeout', min: 500, max: 3000, step: 100, unit: 'ms' },
  ],
  dns: [],
  tcp: [{ key: 'timeout_ms', label: 'Per-port timeout', min: 500, max: 5000, step: 100, unit: 'ms' }],
}

/** count × interval + timeout must fit inside ping's 2-minute deadline. */
export const PING_MAX_TOTAL_MS = 115000
export const MAX_PORTS = 1024

const SERVER_MESSAGE = 'DNS server must be an IPv4 address, optionally with :port'
const PORTS_MESSAGE = 'Ports must be a list like 22,80,443,8000-8100, or "common"'
const NO_REASON = 'no reason was given'

/** Admins always; anyone else only with the grant. */
export function canUseNetTools(user: { is_admin: boolean; net_tools?: boolean } | null | undefined): boolean {
  return !!user && (user.is_admin || user.net_tools === true)
}

/** Neither queued nor running: nothing more will happen to the run. */
export function isFinalStatus(s: ToolRunStatus): boolean {
  return s !== 'queued' && s !== 'running'
}

const STATUS_LABEL: Record<ToolRunStatus, string> = {
  queued: 'Queued',
  running: 'Running',
  done: 'Done',
  failed: 'Failed',
  refused: 'Refused',
  cancelled: 'Cancelled',
  timed_out: 'Timed out',
  interrupted: 'Interrupted',
}

export function statusLabel(s: ToolRunStatus): string {
  return STATUS_LABEL[s] ?? s
}

function tone(c: { bg: string; border: string; text: string }): string {
  return `${c.bg} ${c.border} ${c.text}`
}

/** Pill classes for a status (history table, DNS rcode). */
export const STATUS_PILL: Record<ToolRunStatus, string> = {
  queued: tone(colors.neutral),
  running: tone(colors.responseTime),
  done: tone(colors.operational),
  failed: tone(colors.error),
  refused: tone(colors.warning),
  cancelled: tone(colors.neutral),
  timed_out: tone(colors.error),
  interrupted: tone(colors.warning),
}

/** Text colour of the status line. */
export const STATUS_TEXT: Record<ToolRunStatus, string> = {
  queued: 'text-slate-300',
  running: 'text-slate-300',
  done: colors.operational.text,
  failed: colors.error.text,
  refused: colors.warning.text,
  cancelled: 'text-slate-400',
  timed_out: colors.error.text,
  interrupted: colors.warning.text,
}

const REASON_TEXT: Record<VantageReason, string> = {
  server_disabled: 'runs from the Sentinel server are switched off',
  agent_too_old: "this agent's version can't run tools — update it",
  tools_off: 'network tools are not allowed for this agent (Edit → Allow network tools)',
  not_enabled_on_server: 'ENABLE_TOOLS=true is not set on the server',
  offline: 'offline (not collecting jobs)',
}

/** Why a vantage cannot run tools now, as a fragment ("… — offline (…)"). */
export function vantageReasonText(reason: VantageReason): string {
  return REASON_TEXT[reason] ?? 'not available'
}

/** The agent's own switch (ENABLE_TOOLS), for its edit form. NULL means the
 *  agent never reported it: a version too old to run tools. */
export function agentToolsStateText(local: boolean | null | undefined): string {
  if (local === true) return 'Enabled on the server: yes'
  if (local === false) return 'Enabled on the server: no — add ENABLE_TOOLS=true to the agent config'
  return "This agent's version can't run tools — update it"
}

/** "0.05 ms", "2.1 ms", "123 ms"; "—" when there is no value. */
export function formatMs(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms)) return '—'
  if (ms < 1) return `${ms.toFixed(2)} ms`
  if (ms < 100) return `${ms.toFixed(1)} ms`
  return `${Math.round(ms)} ms`
}

/** "0%", "33.3%", "100%". */
export function formatLoss(pct: number): string {
  return `${Math.round(pct * 10) / 10}%`
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`
}

/** The history's one-line result: "0% loss, 2.1 ms avg", "reached in 9
 *  hops", "NOERROR, 2 answers", "3 open of 100"; the status otherwise. */
export function runResultLine(run: ToolRun): string {
  if (run.status !== 'done') return statusLabel(run.status)
  if (!run.summary) return 'Done'
  if (run.tool === 'ping') {
    const p = run.summary as PingSummary
    if (p.received === 0 || p.avg_ms == null) return '100% loss'
    return `${formatLoss(p.loss_pct)} loss, ${p.avg_ms.toFixed(1)} ms avg`
  }
  if (run.tool === 'traceroute') {
    const t = run.summary as TraceSummary
    const hops = plural(t.hop_count, 'hop', 'hops')
    return t.reached ? `reached in ${hops}` : `not reached (${hops})`
  }
  if (run.tool === 'dns') {
    const d = run.summary as DNSSummary
    return `${d.rcode}, ${plural(d.answer_count, 'answer', 'answers')}`
  }
  const c = run.summary as TCPSummary
  return `${c.open} open of ${c.total}`
}

function runningText(run: ToolRun, events: ToolRunEvent[]): string {
  if (run.tool === 'traceroute') {
    const total = run.params.rounds ?? DEFAULT_PARAMS.traceroute.rounds ?? 5
    let done = 0
    for (const e of events) {
      if (e.type === 'round_done') done = Math.max(done, (e.data as RoundDone).round)
    }
    return `Running — round ${Math.min(done + 1, total)} of ${total}`
  }
  if (run.tool === 'ping') {
    const total = run.params.count ?? DEFAULT_PARAMS.ping.count ?? 5
    const n = events.filter((e) => e.type === 'reply' || e.type === 'timeout' || e.type === 'error').length
    return `Running — ${n} of ${total} probes`
  }
  if (run.tool === 'tcp') {
    const start = events.find((e) => e.type === 'start')
    if (!start) return 'Running…'
    const n = events.filter((e) => e.type === 'port').length
    return `Running — ${n} of ${(start.data as ScanStart).total} ports`
  }
  return 'Running…'
}

/** The live status line: who it is waiting for, how far it has got, or how
 *  it ended, in plain language. */
export function runStatusText(run: ToolRun, events: ToolRunEvent[]): string {
  switch (run.status) {
    case 'queued':
      return `Waiting for ${run.vantage_name} to pick up…`
    case 'running':
      return runningText(run, events)
    case 'done':
      return run.summary ? `Done — ${runResultLine(run)}` : 'Done'
    case 'failed':
      return `Failed — ${run.error || NO_REASON}`
    case 'refused':
      return `Refused — ${run.error || NO_REASON}`
    case 'cancelled':
      return 'Cancelled'
    case 'timed_out':
      return 'Timed out — no result before the deadline'
    default:
      return 'Interrupted — Sentinel restarted while this was running'
  }
}

/** "fileserver → 10.0.0.5"; for DNS the name and, when named, the server
 *  ("example.org @ 10.0.0.53"), since target_ip is then the server. */
export function runTargetText(run: ToolRun): string {
  if (run.tool === 'dns') return run.params.server ? `${run.target} @ ${run.params.server}` : run.target
  return run.target_ip && run.target_ip !== run.target ? `${run.target} → ${run.target_ip}` : run.target
}

/** The select's value for a vantage: "sentinel" or "agent:<agent_id>". */
export function vantageKey(v: Vantage): string {
  return v.kind === 'agent' && v.agent_id ? `agent:${v.agent_id}` : 'sentinel'
}

export function vantageFromKey(key: string): Vantage {
  return key.startsWith('agent:') && key.length > 'agent:'.length
    ? { kind: 'agent', agent_id: key.slice('agent:'.length) }
    : { kind: 'sentinel' }
}

const NUMBER_KEYS: NumberKey[] = ['count', 'interval_ms', 'size', 'max_hops', 'rounds', 'timeout_ms']
const STRING_KEYS = ['record_type', 'server', 'ports'] as const

/** Reads the /tools URL (ruling 18). Anything malformed is dropped rather
 *  than refused: these links are typed by hand as often as clicked. */
export function parseToolsQuery(search: URLSearchParams): {
  tool?: NetTool
  target?: string
  vantage?: Vantage
  params?: ToolParams
} {
  const out: { tool?: NetTool; target?: string; vantage?: Vantage; params?: ToolParams } = {}
  const tool = search.get('tool')
  if (tool && (NET_TOOLS as string[]).includes(tool)) out.tool = tool as NetTool
  const target = search.get('target')?.trim()
  if (target) out.target = target
  const from = search.get('from')
  if (from === 'sentinel') out.vantage = { kind: 'sentinel' }
  else if (from && from.startsWith('agent:') && from.length > 'agent:'.length) out.vantage = vantageFromKey(from)
  const raw = search.get('params')
  if (raw) {
    try {
      const v: unknown = JSON.parse(raw)
      if (v && typeof v === 'object' && !Array.isArray(v)) {
        const o = v as Record<string, unknown>
        const p: ToolParams = {}
        for (const k of NUMBER_KEYS) {
          const n = o[k]
          if (typeof n === 'number' && Number.isFinite(n)) p[k] = n
        }
        for (const k of STRING_KEYS) {
          const s = o[k]
          if (typeof s === 'string') p[k] = s
        }
        out.params = p
      }
    } catch {
      // A hand-edited link: keep the rest of it.
    }
  }
  return out
}

/** Builds the /tools query string ("?tool=…"), or '' when nothing is set. */
export function toolsQuery(v: { tool?: NetTool; target?: string; vantage?: Vantage; params?: ToolParams }): string {
  const q = new URLSearchParams()
  if (v.tool) q.set('tool', v.tool)
  const target = v.target?.trim()
  if (target) q.set('target', target)
  if (v.vantage) q.set('from', vantageKey(v.vantage))
  if (v.params) q.set('params', JSON.stringify(v.params))
  const s = q.toString()
  return s ? `?${s}` : ''
}

function validServer(s: string): boolean {
  const m = s.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(?::(\d{1,5}))?$/)
  if (!m) return false
  if (m.slice(1, 5).some((octet) => Number(octet) > 255)) return false
  if (m[5] === undefined) return true
  const port = Number(m[5])
  return port >= 1 && port <= 65535
}

function portsError(spec: string): string | null {
  const s = spec.trim()
  if (s === '' || s.toLowerCase() === 'common') return null
  const seen = new Set<number>()
  for (const raw of s.split(',')) {
    const m = raw.trim().match(/^(\d{1,5})(?:\s*-\s*(\d{1,5}))?$/)
    if (!m) return PORTS_MESSAGE
    const lo = Number(m[1])
    const hi = m[2] === undefined ? lo : Number(m[2])
    if (lo < 1 || hi > 65535 || lo > hi) return PORTS_MESSAGE
    for (let p = lo; p <= hi; p++) {
      seen.add(p)
      if (seen.size > MAX_PORTS) return `At most ${MAX_PORTS} ports per run`
    }
  }
  return null
}

/** The first problem with a tool's parameters, worded for the form, or null.
 *  Mirrors nettools.Normalize, which checks again on the server. */
export function paramError(tool: NetTool, p: ToolParams): string | null {
  for (const f of NUMBER_FIELDS[tool]) {
    const v = p[f.key]
    if (v === undefined || !Number.isInteger(v) || v < f.min || v > f.max) {
      return `${f.label} must be between ${f.min} and ${f.max}${f.unit ? ` ${f.unit}` : ''}`
    }
  }
  if (tool === 'ping' && (p.count ?? 0) * (p.interval_ms ?? 0) + (p.timeout_ms ?? 0) > PING_MAX_TOTAL_MS) {
    return 'Count × interval is too long: at most 115 seconds'
  }
  if (tool === 'dns') {
    const server = p.server?.trim()
    if (server && !validServer(server)) return SERVER_MESSAGE
  }
  if (tool === 'tcp') return portsError(p.ports ?? '')
  return null
}

/** The parameters to send: the tool's own fields only, strings trimmed, an
 *  empty DNS server left out (system resolver), empty ports as "common". */
export function cleanParams(tool: NetTool, p: ToolParams): ToolParams {
  const out: ToolParams = {}
  for (const f of NUMBER_FIELDS[tool]) {
    const v = p[f.key]
    if (v !== undefined && Number.isFinite(v)) out[f.key] = v
  }
  if (tool === 'dns') {
    out.record_type = (p.record_type ?? 'A').trim().toUpperCase() || 'A'
    const server = (p.server ?? '').trim()
    if (server) out.server = server
  }
  if (tool === 'tcp') {
    const ports = (p.ports ?? '').trim()
    out.ports = ports === '' || ports.toLowerCase() === 'common' ? 'common' : ports
  }
  return out
}

/** The host a monitor checks, for "Ping / Trace" and the known-hosts list:
 *  the URL's host for http, host without :port for tcp, as typed for ping
 *  and dns. Webhook monitors have nothing to probe. */
export function monitorHost(m: { type: string; url: string }): string | null {
  const raw = (m.url ?? '').trim()
  if (!raw) return null
  if (m.type === 'http') {
    try {
      return new URL(raw).hostname.replace(/^\[|\]$/g, '') || null
    } catch {
      return null
    }
  }
  if (m.type === 'tcp') {
    const i = raw.lastIndexOf(':')
    const host = i > 0 && /^\d+$/.test(raw.slice(i + 1)) ? raw.slice(0, i) : raw
    return host || null
  }
  if (m.type === 'ping' || m.type === 'dns') return raw
  return null
}

export interface KnownHost {
  value: string
  label: string
}

/** IPv4 addresses sort numerically (10.0.0.9 before 10.0.0.10), names after. */
function hostSortKey(h: string): string {
  const m = h.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/)
  return m ? m.slice(1).map((n) => n.padStart(3, '0')).join('.') : `~${h.toLowerCase()}`
}

/** The target picker's suggestions: devices, monitored hosts and servers,
 *  one entry per host with every name that uses it. */
export function knownHosts(
  devices: { name: string; host: string }[],
  monitors: { name: string; type: string; url: string }[],
  agents: { name: string; ip_address: string | null }[],
): KnownHost[] {
  const labels = new Map<string, string[]>()
  const add = (host: string | null | undefined, label: string) => {
    const h = host?.trim()
    if (!h) return
    const list = labels.get(h) ?? []
    if (!list.includes(label)) list.push(label)
    labels.set(h, list)
  }
  for (const d of devices) add(d.host, `${d.name} (device)`)
  for (const m of monitors) add(monitorHost(m), `${m.name} (monitor)`)
  for (const a of agents) add(a.ip_address, `${a.name} (server)`)
  return [...labels.entries()]
    .map(([value, l]) => ({ value, label: l.join(', ') }))
    .sort((a, b) => {
      const x = hostSortKey(a.value)
      const y = hostSortKey(b.value)
      return x < y ? -1 : x > y ? 1 : 0
    })
}

/** The bad entries of a refused allowlist save (the 422 body's `entries`). */
export function allowlistErrors(details: Record<string, unknown> | undefined): AllowlistEntryError[] {
  const raw = details?.entries
  if (!Array.isArray(raw)) return []
  return raw.filter(
    (e): e is AllowlistEntryError =>
      !!e && typeof e === 'object' && typeof e.entry === 'string' && typeof e.message === 'string',
  )
}

export interface LineError {
  /** 1-based line of the textarea, or null when the entry is not found. */
  line: number | null
  entry: string
  message: string
}

/** Places each bad entry on its line of the allowlist textarea. */
export function lineErrors(text: string, errors: AllowlistEntryError[]): LineError[] {
  const lines = text.split('\n').map((l) => l.trim().toLowerCase())
  return errors.map((e) => {
    const i = lines.indexOf(e.entry.trim().toLowerCase())
    return { line: i >= 0 ? i + 1 : null, entry: e.entry, message: e.message }
  })
}
```

- [ ] **Step 6: Run the check to see it pass**

Run the command from Step 3 again.

Expected: PASS, printing `netTools helpers: all checks passed`.

- [ ] **Step 7: Export the API base URL and add the user and agent fields**

In `frontend/src/services/api.ts`, replace:

```ts
const baseURL =
  import.meta.env.VITE_API_URL || import.meta.env.REACT_APP_API_URL || '/api/v1'

export const api: AxiosInstance = axios.create({
  baseURL,
```

with:

```ts
// Exported for clients axios cannot serve, such as the EventSource that
// follows a network tool run.
export const apiBaseURL: string =
  import.meta.env.VITE_API_URL || import.meta.env.REACT_APP_API_URL || '/api/v1'

export const api: AxiosInstance = axios.create({
  baseURL: apiBaseURL,
```

In `frontend/src/context/AuthContext.tsx`, replace:

```ts
  last_login: string | null
  theme?: UserTheme
}
```

with:

```ts
  last_login: string | null
  theme?: UserTheme
  /** Granted network tools. Admins may always use them, whatever this says. */
  net_tools?: boolean
}
```

In `frontend/src/hooks/useAgents.ts`, inside `interface Agent`, replace:

```ts
  docker_available: boolean | null

```

with:

```ts
  docker_available: boolean | null
  /** The admin switch in Sentinel: tools may run from this agent. */
  tools_enabled: boolean
  /** ENABLE_TOOLS on the host, as the agent last reported it; null when it
   *  never has (a version too old to run tools). */
  tools_local: boolean | null

```

In `frontend/src/hooks/useUserManagement.ts`, inside `interface ManagedUser`, replace:

```ts
  created_at: string
  last_login?: string | null
}
```

with:

```ts
  created_at: string
  last_login?: string | null
  /** The network tools grant. Sent to admins only; admins can always use tools. */
  net_tools?: boolean
}
```

- [ ] **Step 8: Add the API hooks**

Create `frontend/src/hooks/useNetTools.ts`:

```ts
import { useCallback, useEffect, useRef, useState } from 'react'
import api, { type ApiError } from '@/services/api'
import type { ApiResponse } from '@/types'
import type {
  CreateToolRunRequest,
  NetToolsSettings,
  ToolRun,
  ToolRunDetail,
  ToolRunFilter,
  VantagesResponse,
} from '@/types/netTools'
import { HISTORY_PAGE_SIZE } from '@/utils/netTools'

function message(err: unknown, fallback: string): string {
  return (err as ApiError)?.message || fallback
}

interface Resource<T> {
  data: T | null
  setData: (d: T) => void
  loading: boolean
  error: string | null
  reload: () => void
}

/**
 * Loads the resource named by key (null: nothing to load), optionally
 * polling. A response is applied only if it answers the latest request, so
 * a slow answer for an old filter or run never replaces a newer one, and
 * nothing lands after unmount (CLAUDE.md: guard async results). Only the
 * first load for a key shows as loading; polls refresh quietly.
 */
function useResource<T>(
  key: string | null,
  fetcher: (key: string) => Promise<T>,
  fallback: string,
  pollMs = 0,
): Resource<T> {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(key !== null)
  const [error, setError] = useState<string | null>(null)
  const latest = useRef(0)

  const reload = useCallback(() => {
    if (key === null) return
    latest.current += 1
    const mine = latest.current
    fetcher(key)
      .then(
        (d) => {
          if (mine !== latest.current) return
          setData(d)
          setError(null)
        },
        (err) => {
          if (mine === latest.current) setError(message(err, fallback))
        },
      )
      .finally(() => {
        if (mine === latest.current) setLoading(false)
      })
  }, [key, fetcher, fallback])

  useEffect(() => {
    setData(null)
    setError(null)
    setLoading(key !== null)
    reload()
    const timer = key !== null && pollMs > 0 ? window.setInterval(reload, pollMs) : 0
    return () => {
      // Whatever is still in flight belongs to the old key: drop it.
      latest.current += 1
      if (timer) window.clearInterval(timer)
    }
  }, [key, reload, pollMs])

  return { data, setData, loading, error, reload }
}

const VANTAGES_POLL_MS = 15_000
const HISTORY_POLL_MS = 15_000

async function fetchVantages(): Promise<VantagesResponse> {
  const res = await api.get<ApiResponse<VantagesResponse>>('/tools/vantages')
  return res.data.data
}

/** Where tools can run from, and whether the allowlist is empty. Polled,
 *  because an agent's readiness follows its polling. */
export function useVantages(): {
  data: VantagesResponse | null
  loading: boolean
  error: string | null
  reload: () => void
} {
  const r = useResource('vantages', fetchVantages, 'Could not load where tools can run from', VANTAGES_POLL_MS)
  return { data: r.data, loading: r.loading, error: r.error, reload: r.reload }
}

async function fetchRuns(key: string): Promise<{ runs: ToolRun[]; total: number }> {
  const f = JSON.parse(key) as ToolRunFilter
  const params: Record<string, string | number> = {
    limit: f.limit ?? HISTORY_PAGE_SIZE,
    offset: f.offset ?? 0,
  }
  if (f.tool) params.tool = f.tool
  if (f.status) params.status = f.status
  if (f.mine) params.mine = 1
  if (f.agent_id) params.agent_id = f.agent_id
  if (f.target) params.target = f.target
  const res = await api.get<ApiResponse<ToolRun[]>>('/tools/runs', { params })
  const runs = res.data.data ?? []
  const total = Number(res.headers['x-total-count'])
  return { runs, total: Number.isFinite(total) ? total : runs.length }
}

/** Run history, newest first, refreshed every 15 s (a team's runs). */
export function useToolRuns(filter: ToolRunFilter): {
  runs: ToolRun[]
  total: number
  loading: boolean
  error: string | null
  reload: () => void
} {
  const r = useResource(JSON.stringify(filter), fetchRuns, 'Could not load the run history', HISTORY_POLL_MS)
  return { runs: r.data?.runs ?? [], total: r.data?.total ?? 0, loading: r.loading, error: r.error, reload: r.reload }
}

async function fetchRun(id: string): Promise<ToolRunDetail> {
  const res = await api.get<ApiResponse<ToolRunDetail>>(`/tools/runs/${encodeURIComponent(id)}`)
  return res.data.data
}

/** One run with every stored event. Live progress comes from useRunStream. */
export function useToolRun(id: string | undefined): {
  detail: ToolRunDetail | null
  loading: boolean
  error: string | null
  reload: () => void
} {
  const r = useResource(id ?? null, fetchRun, 'Could not load the run')
  return { detail: r.data, loading: r.loading, error: r.error, reload: r.reload }
}

export async function createToolRun(req: CreateToolRunRequest): Promise<ToolRun> {
  const res = await api.post<ApiResponse<ToolRun>>('/tools/runs', req)
  return res.data.data
}

export async function cancelToolRun(id: string): Promise<ToolRun> {
  const res = await api.post<ApiResponse<ToolRun>>(`/tools/runs/${encodeURIComponent(id)}/cancel`)
  return res.data.data
}

async function fetchSettings(): Promise<NetToolsSettings> {
  const res = await api.get<ApiResponse<NetToolsSettings>>('/settings/net-tools')
  return res.data.data
}

/** Settings → Network tools (admin only). save throws the ApiError of a
 *  refused save; a bad allowlist's entries are in its `details`. */
export function useNetToolsSettings(): {
  settings: NetToolsSettings | null
  loading: boolean
  error: string | null
  save: (s: NetToolsSettings) => Promise<NetToolsSettings>
  reload: () => void
} {
  const r = useResource('net-tools-settings', fetchSettings, 'Could not load the network tools settings')
  const { setData } = r
  const save = useCallback(
    async (s: NetToolsSettings) => {
      const res = await api.put<ApiResponse<NetToolsSettings>>('/settings/net-tools', s)
      setData(res.data.data)
      return res.data.data
    },
    [setData],
  )
  return { settings: r.data, loading: r.loading, error: r.error, save, reload: r.reload }
}

/** Grants or removes a user's network tools permission (admin only). */
export async function setUserNetTools(userId: string, enabled: boolean): Promise<void> {
  await api.patch(`/users/${encodeURIComponent(userId)}/net-tools`, { enabled })
}

/** Sentinel's switch for running tools from an agent (admin only). */
export async function setAgentTools(agentId: string, enabled: boolean): Promise<void> {
  await api.put(`/agents/${encodeURIComponent(agentId)}/tools`, { enabled })
}
```

`latest.current += 1` is an assignment on purpose. The exhaustive-deps rule then treats `latest` as a ref this hook manages, rather than a DOM ref, and does not object to the cleanup touching it.

- [ ] **Step 9: Add the stream hook**

Create `frontend/src/hooks/useRunStream.ts`:

```ts
import { useEffect, useState } from 'react'
import { apiBaseURL } from '@/services/api'
import type { ToolRun, ToolRunEvent, ToolRunStatus } from '@/types/netTools'

export interface RunStream {
  /** Every event received, by seq, without duplicates. */
  events: ToolRunEvent[]
  /** The latest status a frame reported, or null before one arrives. */
  status: ToolRunStatus | null
  /** The final run, once the `end` frame has arrived. */
  run: ToolRun | null
  error: string | null
}

const EMPTY: RunStream = { events: [], status: null, run: null, error: null }

/** Events arriving in a burst (a port scan) are gathered for this long
 *  before one re-render. */
const BATCH_MS = 100

interface State extends RunStream {
  id: string | null
}

function parse<T>(e: MessageEvent): T | null {
  try {
    return JSON.parse(String(e.data)) as T
  } catch {
    return null
  }
}

/**
 * Follows a run live over SSE (GET /tools/runs/:id/events).
 *
 * EventSource reconnects by itself and resends Last-Event-ID, and the server
 * replays from there; events are merged by seq, so a replay never duplicates.
 * The stream is closed on `end`, when runId changes (switching tool or run)
 * and on unmount. Frames from a closed source are ignored, so a late event
 * can never land on another run's view.
 */
export function useRunStream(runId: string | null): RunStream {
  const [state, setState] = useState<State>({ ...EMPTY, id: null })

  useEffect(() => {
    setState({ ...EMPTY, id: runId })
    if (!runId) return
    let open = true
    let timer = 0
    const bySeq = new Map<number, ToolRunEvent>()
    const flush = () => {
      if (timer) window.clearTimeout(timer)
      timer = 0
      const events = [...bySeq.values()].sort((a, b) => a.seq - b.seq)
      setState((s) => (s.id === runId ? { ...s, events } : s))
    }

    const src = new EventSource(`${apiBaseURL}/tools/runs/${encodeURIComponent(runId)}/events`, {
      withCredentials: true,
    })
    src.addEventListener('event', (e: MessageEvent) => {
      if (!open) return
      const ev = parse<ToolRunEvent>(e)
      if (!ev || bySeq.has(ev.seq)) return
      bySeq.set(ev.seq, ev)
      if (!timer) timer = window.setTimeout(flush, BATCH_MS)
    })
    src.addEventListener('status', (e: MessageEvent) => {
      if (!open) return
      const s = parse<{ status: ToolRunStatus }>(e)
      if (s) setState((st) => (st.id === runId ? { ...st, status: s.status } : st))
    })
    src.addEventListener('end', (e: MessageEvent) => {
      if (!open) return
      open = false
      src.close()
      flush()
      const run = parse<ToolRun>(e)
      if (run) setState((st) => (st.id === runId ? { ...st, run, status: run.status, error: null } : st))
    })
    src.onerror = () => {
      // While CONNECTING the browser is retrying on its own; CLOSED means it
      // gave up (the request was refused, e.g. 403 or 404).
      if (!open || src.readyState !== EventSource.CLOSED) return
      open = false
      flush()
      setState((st) => (st.id === runId ? { ...st, error: 'Live updates stopped' } : st))
    }

    return () => {
      open = false
      if (timer) window.clearTimeout(timer)
      src.close()
    }
  }, [runId])

  // The render between a runId change and the effect must not show the
  // previous run's events.
  if (state.id !== runId) return EMPTY
  return { events: state.events, status: state.status, run: state.run, error: state.error }
}
```

The `end` frame is what moves a page off "Running", whoever finished the run: the run's own goroutine, a cancel, or the sweeper timing out a vanished agent (Review Focus 1). The page needs no timer of its own.

- [ ] **Step 10: Run the gate**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output. Nothing renders the new code yet, so there are no manual checks in this task. Run the Step 3 check once more (expected: `netTools helpers: all checks passed`), then delete it with `rm -r "$CHECK_DIR"`.

- [ ] **Step 11: Commit**

```bash
git add frontend/src/types/netTools.ts frontend/src/utils/netTools.ts frontend/src/hooks/useNetTools.ts \
  frontend/src/hooks/useRunStream.ts frontend/src/services/api.ts frontend/src/context/AuthContext.tsx \
  frontend/src/hooks/useAgents.ts frontend/src/hooks/useUserManagement.ts
git commit -m "feat(tools): network tools types, API hooks, live stream and helpers

The data layer for the Tools pages: typed hooks for every /tools route,
an EventSource hook that merges events by seq and closes on end, and the
pure helpers (the /tools URL, form checks, status and result lines).

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 16: The Tools page (form, live run, history)

This task builds the `/tools` page: tool tabs, a form with a known-hosts picker and a "Run from" select, the empty-allowlist banner, the live status line with Cancel, and the run history. It adds a "Network Tools" navigation entry for admins and grant holders.

The result panels come in Task 17. Until then, the live area shows a plain `N events received` line, which Task 17 replaces with `<RunResult />`. Task 17 also adds the `/tools/runs/:id` route. Until then the "Open run" link and the history rows lead to an empty page.

**Files:**
- Create: `frontend/src/pages/NetworkTools.tsx`
- Create: `frontend/src/components/netTools/NoToolsAccess.tsx`
- Create: `frontend/src/components/netTools/VantageSelect.tsx`
- Create: `frontend/src/components/netTools/TargetInput.tsx`
- Create: `frontend/src/components/netTools/ToolForm.tsx`
- Create: `frontend/src/components/netTools/RunStatusLine.tsx`
- Create: `frontend/src/components/netTools/RunHistory.tsx`
- Modify: `frontend/src/App.tsx` (lazy import and the `/tools` route)
- Modify: `frontend/src/components/Layout.tsx` (the nav entry)
- Test: the frontend gate

**Interfaces:**
- Consumes (Task 15):
  - `@/hooks/useNetTools`: `useVantages()`, `useToolRuns(filter)`, `createToolRun(req)` and `cancelToolRun(id)`.
  - `@/hooks/useRunStream`: `useRunStream(runId)`.
  - `@/utils/netTools`: `canUseNetTools`, `parseToolsQuery`, `vantageKey`, `vantageFromKey`, `cleanParams`, `paramError`, `knownHosts`, `isFinalStatus`, `runStatusText`, `runResultLine`, `runTargetText`, `statusLabel`, `vantageReasonText`, `STATUS_PILL`, `STATUS_TEXT`, `TOOL_LABEL`, `NET_TOOLS`, `TOOL_RUN_STATUSES`, `DEFAULT_PARAMS`, `NUMBER_FIELDS`, `DNS_RECORD_TYPES`, `HISTORY_PAGE_SIZE` and `type NumberKey`.
  - The types in `@/types/netTools`.
- Consumes (existing): `useDevices()` (`@/hooks/useDevices`, 30 s refresh), `useMonitors({ limit: 500 })` (`@/hooks/useMonitors`), `useAgents(0)` (`@/hooks/useAgents`, no polling), `useAuthContext()`, `formatDatetime` (`@/utils/formatters`) and `colors` (`@/utils/colors`).
- Produces (components, each file default-exports only its component):
  - `NoToolsAccess()`.
  - `VantageSelect({ vantages: VantageView[]; value: string; onChange: (key: string) => void; disabled?: boolean })`, where the value is a `vantageKey`.
  - `TargetInput({ tool: NetTool; value: string; onChange: (v: string) => void })`.
  - `ToolForm({ tool, target, onTarget, vantage, onVantage, vantages, params, onParams, running, busy, onRun, onCancel })`.
  - `RunStatusLine({ run: ToolRun; events: ToolRunEvent[]; streamError: string | null })`.
  - `RunHistory({ runs, total, loading, error, filter, onFilter })`.
  - The page `NetworkTools` at `/tools`. It reads `?tool=&target=&from=&params=` (ruling 18) and renders the live area as `<p className="text-sm text-slate-500">{stream.events.length} events received</p>`, which Task 17 replaces.

Decisions taken in this task:
- The DNS record type and the port list sit in the form's main row, because they are what those tools are about. Only the numeric options are in the collapsed Options section. Each option starts at the backend's default.
- Run is not disabled for an empty allowlist: a DNS lookup through the vantage's own resolver needs no allowlist entry. The banner explains, and the server refuses what it must.
- When no vantage was asked for and the Sentinel server cannot run tools, the form picks the first ready agent once, on the first answer.
- The page is keyed by its query string. Arriving from an entry point or from "Run again" starts a fresh form and closes any open stream, instead of merging into the form on screen.

- [ ] **Step 1: Add the small components**

Create `frontend/src/components/netTools/NoToolsAccess.tsx`:

```tsx
import { Link } from 'react-router-dom'

/** Shown instead of the tools to anyone who is neither an admin nor granted
 *  network tools; the API would refuse them anyway. */
export default function NoToolsAccess() {
  return (
    <div className="card p-8 text-center">
      <p className="text-slate-300">Network tools are for admins and for users an admin has given access.</p>
      <Link to="/" className="mt-2 inline-block text-sm text-primary-400 hover:underline">
        Back to the overview
      </Link>
    </div>
  )
}
```

Create `frontend/src/components/netTools/VantageSelect.tsx`:

```tsx
import { useId } from 'react'
import type { VantageView } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { vantageKey, vantageReasonText } from '@/utils/netTools'

interface Props {
  vantages: VantageView[]
  /** A vantageKey: "sentinel" or "agent:<agent_id>". */
  value: string
  onChange: (key: string) => void
  disabled?: boolean
}

/** "Run from": the Sentinel server and every agent. Agents that cannot run
 *  tools now stay listed, disabled, with the reason beside the name. */
export default function VantageSelect({ vantages, value, onChange, disabled }: Props) {
  const id = useId()
  const selected = vantages.find((v) => vantageKey(v) === value)

  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-sm font-medium text-white">
        Run from
      </label>
      <select id={id} className="w-full" value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        {!selected && <option value={value}>{value === 'sentinel' ? 'Sentinel server' : 'Unknown agent'}</option>}
        {vantages.map((v) => (
          <option key={vantageKey(v)} value={vantageKey(v)} disabled={!v.ready}>
            {v.ready ? v.name : `${v.name} — ${vantageReasonText(v.reason ?? 'offline')}`}
          </option>
        ))}
      </select>
      {selected && !selected.ready && (
        <p className={`mt-1 text-xs ${colors.warning.text}`}>
          {selected.name} can&apos;t run tools now: {vantageReasonText(selected.reason ?? 'offline')}.
        </p>
      )}
      {!selected && vantages.length > 0 && (
        <p className={`mt-1 text-xs ${colors.warning.text}`}>That agent is not available. Choose another.</p>
      )}
    </div>
  )
}
```

Create `frontend/src/components/netTools/TargetInput.tsx`:

```tsx
import { useId, useMemo } from 'react'
import { useAgents } from '@/hooks/useAgents'
import { useDevices } from '@/hooks/useDevices'
import { useMonitors } from '@/hooks/useMonitors'
import type { NetTool } from '@/types/netTools'
import { knownHosts } from '@/utils/netTools'

interface Props {
  tool: NetTool
  value: string
  onChange: (v: string) => void
}

const MONITOR_FILTER = { limit: 500 }

/** The target, with the devices, monitored hosts and servers this user can
 *  see offered as suggestions. A suggestion is only a convenience: the
 *  allowlist decides, on the server. */
export default function TargetInput({ tool, value, onChange }: Props) {
  const id = useId()
  const listId = `${id}-hosts`
  const { devices } = useDevices()
  const { monitors } = useMonitors(MONITOR_FILTER)
  const { agents } = useAgents(0)
  const hosts = useMemo(() => knownHosts(devices, monitors, agents), [devices, monitors, agents])
  const dns = tool === 'dns'

  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-sm font-medium text-white">
        {dns ? 'Name to look up' : 'Target'}
      </label>
      <input
        id={id}
        list={listId}
        className="w-full font-mono"
        value={value}
        maxLength={253}
        autoComplete="off"
        spellCheck={false}
        placeholder={dns ? 'example.org, or an IPv4 address for PTR' : '10.0.0.1 or fileserver.example.org'}
        onChange={(e) => onChange(e.target.value)}
      />
      <datalist id={listId}>
        {hosts.map((h) => (
          <option key={h.value} value={h.value} label={h.label} />
        ))}
      </datalist>
      <p className="mt-1 text-xs text-slate-500">
        {dns
          ? 'The name is looked up as typed; only a named DNS server must be on the allowlist.'
          : 'Pick a known device, monitor or server, or type an address. The allowlist still applies.'}
      </p>
    </div>
  )
}
```

Create `frontend/src/components/netTools/RunStatusLine.tsx`:

```tsx
import { Loader2 } from 'lucide-react'
import type { ToolRun, ToolRunEvent } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { isFinalStatus, runStatusText, STATUS_TEXT } from '@/utils/netTools'

interface Props {
  run: ToolRun
  events: ToolRunEvent[]
  /** useRunStream's error: the browser gave up following the run. */
  streamError: string | null
}

/** "Waiting for file-server to pick up…", "Running — round 3 of 5",
 *  "Done — 0% loss, 2.1 ms avg", or why it failed. */
export default function RunStatusLine({ run, events, streamError }: Props) {
  const live = !isFinalStatus(run.status)
  return (
    <div className="space-y-1" aria-live="polite">
      <p className={`flex items-center gap-2 text-sm font-medium ${STATUS_TEXT[run.status]}`}>
        {live && <Loader2 className="h-4 w-4 shrink-0 animate-spin" aria-hidden />}
        {runStatusText(run, events)}
      </p>
      {live && streamError && (
        <p className={`text-xs ${colors.warning.text}`}>
          {streamError}. The run carries on; open it or reload to see how it ends.
        </p>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Add the form**

Create `frontend/src/components/netTools/ToolForm.tsx`:

```tsx
import { useId } from 'react'
import { Loader2, Play, Square } from 'lucide-react'
import type { NetTool, ToolParams, VantageView } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { DNS_RECORD_TYPES, NUMBER_FIELDS, paramError, vantageKey, type NumberKey } from '@/utils/netTools'
import TargetInput from './TargetInput'
import VantageSelect from './VantageSelect'

interface Props {
  tool: NetTool
  target: string
  onTarget: (v: string) => void
  /** A vantageKey. */
  vantage: string
  onVantage: (key: string) => void
  vantages: VantageView[]
  /** This tool's parameters. */
  params: ToolParams
  onParams: (p: ToolParams) => void
  /** A run started here is queued or running. */
  running: boolean
  /** A create or cancel request is in flight. */
  busy: boolean
  onRun: () => void
  onCancel: () => void
}

/** One tool's form: target, where to run from, the tool's own options, and
 *  Run (or Cancel while a run is going). */
export default function ToolForm(props: Props) {
  const { tool, target, onTarget, vantage, onVantage, vantages, params, onParams, running, busy, onRun, onCancel } = props
  const id = useId()
  const problem = paramError(tool, params)
  const chosen = vantages.find((v) => vantageKey(v) === vantage)
  const canRun = !busy && !running && target.trim() !== '' && problem === null && !!chosen?.ready

  const setNumber = (key: NumberKey, raw: string) => {
    const next: ToolParams = { ...params }
    next[key] = raw === '' ? undefined : Number(raw)
    onParams(next)
  }

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault()
        if (canRun) onRun()
      }}
    >
      <div className="grid gap-4 md:grid-cols-2">
        <TargetInput tool={tool} value={target} onChange={onTarget} />
        <VantageSelect vantages={vantages} value={vantage} onChange={onVantage} disabled={running} />
      </div>

      {tool === 'dns' && (
        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label htmlFor={`${id}-rtype`} className="mb-1 block text-sm font-medium text-white">
              Record type
            </label>
            <select
              id={`${id}-rtype`}
              className="w-full"
              value={params.record_type ?? 'A'}
              onChange={(e) => onParams({ ...params, record_type: e.target.value })}
            >
              {DNS_RECORD_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor={`${id}-server`} className="mb-1 block text-sm font-medium text-white">
              DNS server
            </label>
            <input
              id={`${id}-server`}
              className="w-full font-mono"
              value={params.server ?? ''}
              placeholder="System resolver"
              spellCheck={false}
              onChange={(e) => onParams({ ...params, server: e.target.value })}
            />
            <p className="mt-1 text-xs text-slate-500">
              Empty asks the resolver of the machine it runs from. A named server (10.0.0.53 or 10.0.0.53:5353)
              must be on the allowlist.
            </p>
          </div>
        </div>
      )}

      {tool === 'tcp' && (
        <div>
          <label htmlFor={`${id}-ports`} className="mb-1 block text-sm font-medium text-white">
            Ports
          </label>
          <input
            id={`${id}-ports`}
            className="w-full font-mono"
            value={params.ports ?? ''}
            placeholder="common"
            spellCheck={false}
            onChange={(e) => onParams({ ...params, ports: e.target.value })}
          />
          <p className="mt-1 text-xs text-slate-500">
            &quot;common&quot; checks about 100 well-known ports. Or list ports and ranges, e.g. 22,80,443,8000-8100
            (at most 1024).
          </p>
        </div>
      )}

      {NUMBER_FIELDS[tool].length > 0 && (
        <details className="rounded-lg border border-white/10 bg-slate-900/30 p-3">
          <summary className="cursor-pointer text-sm text-slate-300">Options</summary>
          <div className="mt-3 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {NUMBER_FIELDS[tool].map((f) => (
              <div key={f.key}>
                <label htmlFor={`${id}-${f.key}`} className="mb-1 block text-xs font-medium text-slate-300">
                  {f.label}
                  {f.unit ? ` (${f.unit})` : ''}
                </label>
                <input
                  id={`${id}-${f.key}`}
                  type="number"
                  className="w-full"
                  min={f.min}
                  max={f.max}
                  step={f.step ?? 1}
                  value={params[f.key] ?? ''}
                  onChange={(e) => setNumber(f.key, e.target.value)}
                />
              </div>
            ))}
          </div>
        </details>
      )}

      {problem && <p className={`text-xs ${colors.error.text}`}>{problem}</p>}

      <div className="flex flex-wrap items-center gap-2">
        <button type="submit" className="btn-primary" disabled={!canRun}>
          {busy && !running ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />} Run
        </button>
        {running && (
          <button type="button" className="btn-secondary" disabled={busy} onClick={onCancel}>
            <Square className="h-4 w-4" /> Cancel
          </button>
        )}
      </div>
    </form>
  )
}
```

- [ ] **Step 3: Add the history table**

Create `frontend/src/components/netTools/RunHistory.tsx`:

```tsx
import { Link, useNavigate } from 'react-router-dom'
import type { NetTool, ToolRun, ToolRunFilter, ToolRunStatus } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { formatDatetime } from '@/utils/formatters'
import {
  HISTORY_PAGE_SIZE,
  NET_TOOLS,
  runResultLine,
  runTargetText,
  STATUS_PILL,
  statusLabel,
  TOOL_LABEL,
  TOOL_RUN_STATUSES,
} from '@/utils/netTools'

interface Props {
  runs: ToolRun[]
  total: number
  loading: boolean
  error: string | null
  filter: ToolRunFilter
  onFilter: (f: ToolRunFilter) => void
}

/** Everyone's runs, newest first (a team troubleshooting tool), filtered by
 *  tool, mine or all, and status. A row opens the run. */
export default function RunHistory({ runs, total, loading, error, filter, onFilter }: Props) {
  const navigate = useNavigate()
  const offset = filter.offset ?? 0
  // Any filter change starts again from the first page.
  const set = (patch: Partial<ToolRunFilter>) => onFilter({ ...filter, ...patch, offset: 0 })

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-light text-white">History</h2>
        <div className="flex flex-wrap gap-2">
          <select
            className="rd-select"
            aria-label="Filter by tool"
            value={filter.tool ?? ''}
            onChange={(e) => set({ tool: (e.target.value || undefined) as NetTool | undefined })}
          >
            <option value="">All tools</option>
            {NET_TOOLS.map((t) => (
              <option key={t} value={t}>
                {TOOL_LABEL[t]}
              </option>
            ))}
          </select>
          <select
            className="rd-select"
            aria-label="Whose runs"
            value={filter.mine ? 'mine' : 'all'}
            onChange={(e) => set({ mine: e.target.value === 'mine' || undefined })}
          >
            <option value="all">Everyone&apos;s runs</option>
            <option value="mine">My runs</option>
          </select>
          <select
            className="rd-select"
            aria-label="Filter by status"
            value={filter.status ?? ''}
            onChange={(e) => set({ status: (e.target.value || undefined) as ToolRunStatus | undefined })}
          >
            <option value="">Any status</option>
            {TOOL_RUN_STATUSES.map((s) => (
              <option key={s} value={s}>
                {statusLabel(s)}
              </option>
            ))}
          </select>
        </div>
      </div>

      {error && (
        <div className={`rounded-lg border p-3 text-sm ${colors.error.border} ${colors.error.bg} ${colors.error.text}`}>
          {error}
        </div>
      )}

      <div className="overflow-hidden rounded-lg border border-white/10 bg-slate-800/40">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                <th className="px-4 py-3 font-medium">Time</th>
                <th className="px-4 py-3 font-medium">User</th>
                <th className="px-4 py-3 font-medium">Tool</th>
                <th className="px-4 py-3 font-medium">Target</th>
                <th className="px-4 py-3 font-medium">From</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium">Result</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {runs.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-8 text-center text-slate-500">
                    {loading ? 'Loading…' : 'No runs yet.'}
                  </td>
                </tr>
              ) : (
                runs.map((r) => (
                  <tr
                    key={r.id}
                    className="cursor-pointer transition hover:bg-white/5"
                    onClick={() => navigate(`/tools/runs/${r.id}`)}
                  >
                    <td className="whitespace-nowrap px-4 py-3">
                      <Link
                        to={`/tools/runs/${r.id}`}
                        className="text-slate-300 hover:underline"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {formatDatetime(r.created_at)}
                      </Link>
                    </td>
                    <td className="px-4 py-3 text-slate-300">{r.username}</td>
                    <td className="px-4 py-3 text-slate-300">{TOOL_LABEL[r.tool]}</td>
                    <td className="px-4 py-3 font-mono text-xs text-slate-300">{runTargetText(r)}</td>
                    <td className="px-4 py-3 text-slate-400">{r.vantage_name}</td>
                    <td className="px-4 py-3">
                      <span className={`rd-pill ${STATUS_PILL[r.status]}`}>{statusLabel(r.status)}</span>
                    </td>
                    <td className="px-4 py-3 text-slate-300" title={r.error ?? undefined}>
                      {runResultLine(r)}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {total > HISTORY_PAGE_SIZE && (
        <div className="flex items-center justify-between text-sm text-slate-500">
          <span>
            {offset + 1}–{Math.min(offset + runs.length, total)} of {total}
          </span>
          <div className="flex gap-2">
            <button
              className="btn-secondary !py-1"
              disabled={offset === 0}
              onClick={() => onFilter({ ...filter, offset: Math.max(0, offset - HISTORY_PAGE_SIZE) })}
            >
              Previous
            </button>
            <button
              className="btn-secondary !py-1"
              disabled={offset + HISTORY_PAGE_SIZE >= total}
              onClick={() => onFilter({ ...filter, offset: offset + HISTORY_PAGE_SIZE })}
            >
              Next
            </button>
          </div>
        </div>
      )}
    </section>
  )
}
```

- [ ] **Step 4: Add the page**

Create `frontend/src/pages/NetworkTools.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { ExternalLink } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { cancelToolRun, createToolRun, useToolRuns, useVantages } from '@/hooks/useNetTools'
import { useRunStream } from '@/hooks/useRunStream'
import NoToolsAccess from '@/components/netTools/NoToolsAccess'
import RunHistory from '@/components/netTools/RunHistory'
import RunStatusLine from '@/components/netTools/RunStatusLine'
import ToolForm from '@/components/netTools/ToolForm'
import type { ApiError } from '@/services/api'
import type { NetTool, ToolParams, ToolRun, ToolRunFilter } from '@/types/netTools'
import { colors } from '@/utils/colors'
import {
  canUseNetTools,
  cleanParams,
  DEFAULT_PARAMS,
  HISTORY_PAGE_SIZE,
  isFinalStatus,
  NET_TOOLS,
  parseToolsQuery,
  TOOL_LABEL,
  vantageFromKey,
  vantageKey,
} from '@/utils/netTools'

const EMPTY_ALLOWLIST =
  'No targets are allowed yet. An admin adds subnets and hosts in Settings → Network tools'

/** /tools: run ping, traceroute, a DNS lookup or a port check and watch it
 *  live; the history of everyone's runs below. */
export default function NetworkTools() {
  const { currentUser } = useAuthContext()
  const { search } = useLocation()
  if (!canUseNetTools(currentUser)) return <NoToolsAccess />
  // Keyed by the query string: arriving from an entry point or "Run again"
  // starts a fresh form (and closes any open stream) rather than merging
  // into the one on screen.
  return <ToolsPage key={search} />
}

function initialParams(tool: NetTool | undefined, params: ToolParams | undefined): Record<NetTool, ToolParams> {
  const all = { ...DEFAULT_PARAMS }
  if (tool && params) all[tool] = { ...DEFAULT_PARAMS[tool], ...params }
  return all
}

function ToolsPage() {
  const { currentUser } = useAuthContext()
  const isAdmin = !!currentUser?.is_admin
  const [searchParams] = useSearchParams()
  const [prefill] = useState(() => parseToolsQuery(searchParams))

  const [tool, setTool] = useState<NetTool>(prefill.tool ?? 'ping')
  const [target, setTarget] = useState(prefill.target ?? '')
  const [vantage, setVantage] = useState(prefill.vantage ? vantageKey(prefill.vantage) : 'sentinel')
  const [params, setParams] = useState(() => initialParams(prefill.tool, prefill.params))
  const [current, setCurrent] = useState<ToolRun | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<{ text: string; limit: boolean } | null>(null)
  // Bumped by every new run and every tool switch, so a create or cancel
  // response that arrives after the user moved on is dropped.
  const attempt = useRef(0)
  const autoPicked = useRef(false)

  const { data: vantageData, error: vantageError } = useVantages()
  const stream = useRunStream(current?.id ?? null)
  const [filter, setFilter] = useState<ToolRunFilter>({ limit: HISTORY_PAGE_SIZE, offset: 0 })
  const history = useToolRuns(filter)
  const reloadHistory = history.reload

  // Nothing asked for and the Sentinel server cannot run tools: start on the
  // first agent that can, once.
  useEffect(() => {
    if (autoPicked.current || !vantageData || prefill.vantage) return
    autoPicked.current = true
    const list = vantageData.vantages ?? []
    if (list.find((v) => vantageKey(v) === 'sentinel')?.ready) return
    const firstReady = list.find((v) => v.ready)
    if (firstReady) setVantage(vantageKey(firstReady))
  }, [vantageData, prefill.vantage])

  // A finished run belongs in the history straight away.
  useEffect(() => {
    if (stream.run) reloadHistory()
  }, [stream.run, reloadHistory])

  // The run as this page knows it: the final row once the stream has ended,
  // else the created (or cancelled) row with the latest streamed status.
  const run: ToolRun | null = !current
    ? null
    : (stream.run ??
      (isFinalStatus(current.status) || !stream.status ? current : { ...current, status: stream.status }))
  const running = !!run && !isFinalStatus(run.status)

  const switchTool = (next: NetTool) => {
    if (next === tool) return
    attempt.current += 1
    setTool(next)
    setCurrent(null)
    setProblem(null)
    setBusy(false)
  }

  const start = async () => {
    attempt.current += 1
    const mine = attempt.current
    setBusy(true)
    setProblem(null)
    setCurrent(null)
    try {
      const created = await createToolRun({
        tool,
        vantage: vantageFromKey(vantage),
        target: target.trim(),
        params: cleanParams(tool, params[tool]),
      })
      if (mine !== attempt.current) return
      setCurrent(created)
      reloadHistory()
    } catch (err) {
      if (mine !== attempt.current) return
      const e = err as ApiError
      // Ruling 11: any 429 is a limit, whichever shape its body has.
      setProblem({ text: e.message || 'Could not start the run', limit: e.status === 429 })
    } finally {
      if (mine === attempt.current) setBusy(false)
    }
  }

  const cancel = async () => {
    if (!current) return
    const mine = attempt.current
    setBusy(true)
    try {
      const cancelled = await cancelToolRun(current.id)
      // The stream's end frame brings the same row; this covers a stream
      // that has already stopped.
      if (mine === attempt.current) setCurrent(cancelled)
    } catch (err) {
      if (mine === attempt.current) {
        setProblem({ text: (err as ApiError).message || 'Could not cancel the run', limit: false })
      }
    } finally {
      if (mine === attempt.current) setBusy(false)
    }
  }

  const problemTone = problem?.limit ? colors.warning : colors.error

  return (
    <div className="space-y-6">
      <div>
        <h1 className="vs-title text-4xl">Network Tools</h1>
        <p className="text-sm text-slate-400">
          Ping, trace, look up names and check ports from Sentinel or a server agent.
        </p>
      </div>

      {vantageData?.allowlist_empty && (
        <div className={`rounded-lg border p-4 text-sm ${colors.warning.border} ${colors.warning.bg} ${colors.warning.text}`}>
          <p>{EMPTY_ALLOWLIST}</p>
          {isAdmin && (
            <Link to="/settings?tab=nettools" className="mt-1 inline-block underline">
              Open Settings → Network tools
            </Link>
          )}
        </div>
      )}

      {vantageError && (
        <div className={`rounded-lg border p-3 text-sm ${colors.error.border} ${colors.error.bg} ${colors.error.text}`}>
          {vantageError}
        </div>
      )}

      <div className="card space-y-5 p-5">
        <div className="flex flex-wrap gap-2" role="tablist" aria-label="Tool">
          {NET_TOOLS.map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={tool === t}
              onClick={() => switchTool(t)}
              className={`rounded-md px-4 py-2 text-sm font-medium ${
                tool === t ? 'bg-primary-600 text-white' : 'bg-white/5 text-slate-300'
              }`}
            >
              {TOOL_LABEL[t]}
            </button>
          ))}
        </div>

        <ToolForm
          tool={tool}
          target={target}
          onTarget={setTarget}
          vantage={vantage}
          onVantage={setVantage}
          vantages={vantageData?.vantages ?? []}
          params={params[tool]}
          onParams={(p) => setParams((all) => ({ ...all, [tool]: p }))}
          running={running}
          busy={busy}
          onRun={() => void start()}
          onCancel={() => void cancel()}
        />

        {problem && (
          <div className={`rounded-lg border p-3 text-sm ${problemTone.border} ${problemTone.bg} ${problemTone.text}`}>
            {problem.text}
          </div>
        )}
      </div>

      {run && (
        <div className="card space-y-4 p-5">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <RunStatusLine run={run} events={stream.events} streamError={stream.error} />
            <Link
              to={`/tools/runs/${run.id}`}
              className="inline-flex items-center gap-1 text-sm text-primary-400 hover:underline"
            >
              Open run <ExternalLink className="h-3.5 w-3.5" />
            </Link>
          </div>
          <p className="text-sm text-slate-500">{stream.events.length} events received</p>
        </div>
      )}

      <RunHistory
        runs={history.runs}
        total={history.total}
        loading={history.loading}
        error={history.error}
        filter={filter}
        onFilter={setFilter}
      />
    </div>
  )
}
```

- [ ] **Step 5: Route the page and add the nav entry**

In `frontend/src/App.tsx`, after `const ProfileDetail = lazy(() => import('@/pages/network/ProfileDetail'))`, add:

```ts
const NetworkTools = lazy(() => import('@/pages/NetworkTools'))
```

In the same file, after `<Route path="/network/profiles/:id" element={<ProfileDetail />} />`, add:

```tsx
              {/* Network tools. The page itself tells anyone without the
                  grant that it is not for them; the API refuses them too. */}
              <Route path="/tools" element={<NetworkTools />} />
```

In `frontend/src/components/Layout.tsx`, after `import { useAppConfig } from '@/context/AppConfigContext'`, add:

```ts
import { canUseNetTools } from '@/utils/netTools'
```

Replace:

```ts
const nav = [
  { to: '/', label: 'Overview', end: true },
```

with:

```ts
const nav: { to: string; label: string; end?: boolean; netTools?: boolean }[] = [
  { to: '/', label: 'Overview', end: true },
```

Replace:

```ts
  { to: '/network', label: 'Network Monitoring' },
```

with:

```ts
  { to: '/network', label: 'Network Monitoring' },
  // Troubleshooting from Sentinel or an agent: admins and granted users only.
  { to: '/tools', label: 'Network Tools', netTools: true },
```

Replace:

```tsx
          nav.map((item) => (
```

with:

```tsx
          nav
            .filter((item) => !item.netTools || canUseNetTools(currentUser))
            .map((item) => (
```

The closing `))` after the `NavLink` stays as it is: it now closes `.map(`.

- [ ] **Step 6: Run the gate**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output. The browser checks are the owner's; Task 19 lists them.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/NetworkTools.tsx frontend/src/components/netTools/NoToolsAccess.tsx \
  frontend/src/components/netTools/VantageSelect.tsx frontend/src/components/netTools/TargetInput.tsx \
  frontend/src/components/netTools/ToolForm.tsx frontend/src/components/netTools/RunStatusLine.tsx \
  frontend/src/components/netTools/RunHistory.tsx frontend/src/App.tsx frontend/src/components/Layout.tsx
git commit -m "feat(tools): the Network Tools page: form, live status, cancel and history

Tool tabs, a target picker over known hosts, Run from with each agent's
reason when it cannot, the empty-allowlist banner, a live status line and
everyone's run history. Shown to admins and granted users only.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 17: Result panels and the run detail page

This task builds the four result panels and the dispatch between them, and the `/tools/runs/:id` page. The panels are ping (tiles, a live RTT chart and the reply list), traceroute (the MTR table), DNS (a header line and three record tables) and ports (a progress bar, open ports first, and closed and filtered counts that expand to lists). The detail page shows the stored events and follows a run that is still going. "Run again" fills the form. The panels read pure view models in a new `utils/netToolViews.ts` (Contract change 5), which a throwaway script checks the same way as Task 15.

**Files:**
- Modify: `frontend/src/utils/colors.ts` (append `chartChrome`)
- Create: `frontend/src/utils/netToolViews.ts`
- Create: `frontend/src/components/netTools/PingResult.tsx`
- Create: `frontend/src/components/netTools/TracerouteResult.tsx`
- Create: `frontend/src/components/netTools/DNSResult.tsx`
- Create: `frontend/src/components/netTools/PortsResult.tsx`
- Create: `frontend/src/components/netTools/RunResult.tsx`
- Create: `frontend/src/pages/ToolRunDetail.tsx`
- Modify: `frontend/src/pages/NetworkTools.tsx` (the live area renders `RunResult`)
- Modify: `frontend/src/App.tsx` (lazy import and the `/tools/runs/:id` route)
- Test: a throwaway `check.ts` in a temp directory (not committed), plus the frontend gate

**Interfaces:**
- Consumes (Task 15):
  - `@/hooks/useNetTools`: `useToolRun(id)` and `cancelToolRun(id)`.
  - `@/hooks/useRunStream`: `useRunStream(runId)`.
  - `@/utils/netTools`: `NUMBER_FIELDS`, `isFinalStatus`, `formatMs`, `formatLoss`, `STATUS_PILL`, `TOOL_LABEL`, `runTargetText`, `toolsQuery` and `canUseNetTools`.
  - The types in `@/types/netTools`, including `HopProbe`.
- Consumes (Task 16): `NoToolsAccess`, `RunStatusLine({ run, events, streamError })`, and the page `NetworkTools`, whose live area contains the exact line `<p className="text-sm text-slate-500">{stream.events.length} events received</p>`.
- Consumes (existing): `chartPalette` and `colors` (`@/utils/colors`), `formatDatetime` and `formatDuration` (`@/utils/formatters`), and Recharts (`LineChart`, `Line`, `XAxis`, `YAxis`, `CartesianGrid`, `Tooltip`, `ResponsiveContainer`).
- Produces (`@/utils/colors`): `chartChrome: { grid; tick; tooltipBg; tooltipBorder; tooltipText }`.
- Produces (`@/utils/netToolViews`):
  - `mergeEvents(stored, live): ToolRunEvent[]`.
  - Ping: `interface PingRow { seq; kind: 'reply' | 'timeout' | 'error'; rtt_ms; ttl; from; message }`, `interface PingPoint { seq; rtt: number | null }`, `interface PingView { rows; chart; stats: PingSummary }` and `pingView(events): PingView`.
  - Traceroute: `interface TraceView { round; hops: HopStats[]; reached }` and `traceView(events, summary: TraceSummary | null): TraceView`.
  - DNS: `dnsView(events): DNSAnswer | null`.
  - Ports: `interface PortsView { total: number | null; done; pct; open; closed; filtered }`, `portsView(events): PortsView` and `portLabel(p: PortResult): string`.
  - Shared: `lossTone(pct): string`, `paramsText(tool, params): string` and `runDuration(run): string | null`.
- Produces (components): `RunResult({ run: ToolRun; events: ToolRunEvent[] })` and one panel per tool with the same props, plus the page `ToolRunDetail` at `/tools/runs/:id`.

The panels use the server's summary once a run is final and has one. While a run is live, they use figures computed from the events (ping tiles) or the latest `round_done` (traceroute, ruling 4), so the browser never re-implements hop statistics.

- [ ] **Step 1: Write the view-model check (throwaway, outside the repo)**

This task needs `frontend/node_modules`. If it is missing, run the frontend gate from Step 9 first. Keep this shell open: Steps 2 and 5 reuse `$CHECK_DIR`.

```bash
export CHECK_DIR=$(mktemp -d)
cat > "$CHECK_DIR/check.ts" <<'EOF'
import assert from 'node:assert/strict'
import { colors } from '@/utils/colors'
import {
  dnsView,
  lossTone,
  mergeEvents,
  paramsText,
  pingView,
  portLabel,
  portsView,
  runDuration,
  traceView,
} from '@/utils/netToolViews'
import type { HopStats, ToolRunEvent } from '@/types/netTools'

const ev = (seq: number, type: string, data: unknown): ToolRunEvent => ({ seq, at: '2026-10-05T10:00:01Z', type, data })

// Ping: rows in probe order whatever order they arrived in. Live stats by hand:
// replies 2, 4, 3 ms in probe order -> min 2, avg 3, max 4,
// jitter (|4 - 2| + |3 - 4|) / 2 = 1.5; 2 of 5 lost -> 40%.
const p = pingView([
  ev(1, 'reply', { seq: 1, rtt_ms: 2, ttl: 64, from: '10.0.0.5' }),
  ev(2, 'timeout', { seq: 2 }),
  ev(3, 'reply', { seq: 4, rtt_ms: 3, ttl: 64, from: '10.0.0.5' }),
  ev(4, 'reply', { seq: 3, rtt_ms: 4, ttl: 64, from: '10.0.0.5' }),
  ev(5, 'error', { seq: 5, message: 'destination unreachable from 10.0.0.1' }),
])
assert.deepEqual(p.rows.map((r) => [r.seq, r.kind]), [[1, 'reply'], [2, 'timeout'], [3, 'reply'], [4, 'reply'], [5, 'error']])
assert.deepEqual(p.chart, [
  { seq: 1, rtt: 2 }, { seq: 2, rtt: null }, { seq: 3, rtt: 4 }, { seq: 4, rtt: 3 }, { seq: 5, rtt: null },
])
assert.deepEqual(p.stats, { sent: 5, received: 3, loss_pct: 40, min_ms: 2, avg_ms: 3, max_ms: 4, jitter_ms: 1.5 })
assert.equal(p.rows[4].message, 'destination unreachable from 10.0.0.1')
// 2 of 3 lost: 66.666… rounds to 66.7; one reply has no jitter.
const one = pingView([ev(1, 'reply', { seq: 1, rtt_ms: 7, ttl: 60, from: 'x' }), ev(2, 'timeout', { seq: 2 }), ev(3, 'timeout', { seq: 3 })])
assert.equal(one.stats.loss_pct, 66.7)
assert.equal(one.stats.jitter_ms, null)
assert.deepEqual(pingView([]).stats, { sent: 0, received: 0, loss_pct: 0, min_ms: null, avg_ms: null, max_ms: null, jitter_ms: null })

// Traceroute: the latest round_done wins; hop_name fills names by address;
// a summary (the run is over) wins over both.
const hop = (ttl: number, addrs: string[], sent: number, extra: Partial<HopStats> = {}): HopStats => ({
  ttl, addrs, sent, received: sent, loss_pct: 0, last_ms: 1, avg_ms: 1, best_ms: 1, worst_ms: 1, stdev_ms: 0, ...extra,
})
const traceEvents = [
  ev(1, 'hop', { round: 1, ttl: 1, addr: '10.0.0.1', rtt_ms: 1, reached: false }),
  ev(2, 'hop', { round: 1, ttl: 2, addr: '8.8.8.8', rtt_ms: 9, reached: true }),
  ev(3, 'round_done', { round: 1, hops: [hop(1, ['10.0.0.1'], 1), hop(2, ['8.8.8.8'], 1)] }),
  ev(4, 'hop_name', { ttl: 1, addr: '10.0.0.1', name: 'gw.lan' }),
  ev(5, 'round_done', { round: 2, hops: [hop(1, ['10.0.0.1'], 2), hop(2, ['8.8.4.4', '8.8.8.8'], 2, { received: 1, loss_pct: 50 })] }),
  ev(6, 'hop_name', { ttl: 2, addr: '8.8.8.8', name: 'dns.google' }),
]
const t = traceView(traceEvents, null)
assert.equal(t.round, 2)
assert.equal(t.reached, true)
assert.deepEqual(t.hops.map((h) => [h.ttl, h.sent, h.name]), [[1, 2, 'gw.lan'], [2, 2, 'dns.google']])
const fin = traceView(traceEvents, {
  reached: false, hop_count: 2,
  hops: [hop(1, ['10.0.0.1'], 5, { name: 'router' }), hop(2, [], 5, { received: 0, loss_pct: 100 })],
})
assert.equal(fin.reached, false)
assert.deepEqual(fin.hops.map((h) => [h.ttl, h.sent, h.name]), [[1, 5, 'router'], [2, 5, undefined]])
assert.equal(traceView([], null).hops.length, 0)

// DNS: the one answer event.
const answer = {
  server: '10.0.0.53:53', rcode: 'NOERROR', authoritative: false, truncated: false, tcp: false, rtt_ms: 3.2,
  answer: [{ name: 'example.org.', type: 'A', ttl: 300, data: '93.184.215.14' }], authority: [], additional: [],
}
assert.deepEqual(dnsView([ev(1, 'answer', answer)]), answer)
assert.equal(dnsView([]), null)

// Ports: 4 of 5 known -> 80%; each state sorted by port.
const ports = portsView([
  ev(1, 'start', { total: 5 }),
  ev(2, 'port', { port: 443, state: 'open', rtt_ms: 1.2, service: 'https' }),
  ev(3, 'port', { port: 22, state: 'open', rtt_ms: 0.8, service: 'ssh' }),
  ev(4, 'port', { port: 23, state: 'closed', service: 'telnet' }),
  ev(5, 'port', { port: 3389, state: 'filtered', service: 'rdp' }),
])
assert.equal(ports.total, 5)
assert.equal(ports.done, 4)
assert.equal(ports.pct, 80)
assert.deepEqual(ports.open.map((x) => x.port), [22, 443])
assert.deepEqual(ports.closed.map((x) => x.port), [23])
assert.deepEqual(ports.filtered.map((x) => x.port), [3389])
assert.deepEqual(portsView([]), { total: null, done: 0, pct: 0, open: [], closed: [], filtered: [] })
assert.equal(portLabel({ port: 22, state: 'open', service: 'ssh' }), '22 ssh')
assert.equal(portLabel({ port: 31337, state: 'closed' }), '31337')

// Stored and streamed events merge by seq; the stored copy is kept.
const stored = [ev(1, 'a', 1), ev(2, 'b', 2)]
assert.equal(mergeEvents(stored, []), stored)
assert.deepEqual(mergeEvents(stored, [ev(2, 'b', 'live copy'), ev(3, 'c', 3)]).map((e) => [e.seq, e.data]), [[1, 1], [2, 2], [3, 3]])

// Loss colours.
assert.equal(lossTone(0), 'text-slate-300')
assert.equal(lossTone(20), colors.warning.text)
assert.equal(lossTone(100), colors.error.text)

// The parameters line on the detail page.
assert.equal(
  paramsText('ping', { count: 5, interval_ms: 1000, timeout_ms: 2000, size: 56 }),
  'Count 5 · Interval 1000 ms · Probe timeout 2000 ms · Payload size 56 bytes',
)
assert.equal(paramsText('dns', { record_type: 'MX' }), 'Record type MX · System resolver')
assert.equal(paramsText('dns', { record_type: 'A', server: '10.0.0.53' }), 'Record type A · Server 10.0.0.53')
assert.equal(paramsText('tcp', { ports: 'common', timeout_ms: 1500 }), 'Ports common · Per-port timeout 1500 ms')

// How long a run took: milliseconds under a second, else formatDuration.
assert.equal(runDuration({ started_at: '2026-10-05T10:00:00Z', finished_at: '2026-10-05T10:00:00.250Z' }), '250 ms')
assert.equal(runDuration({ started_at: '2026-10-05T10:00:00Z', finished_at: '2026-10-05T10:01:05Z' }), '1m 5s')
assert.equal(runDuration({ started_at: '2026-10-05T10:00:00Z', finished_at: null }), null)

console.log('netToolViews: all checks passed')
EOF
```

- [ ] **Step 2: Run it to see it fail**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app:ro -v "$CHECK_DIR":/check -w /app node:20-alpine sh -c "node_modules/.bin/esbuild /check/check.ts --bundle --platform=node --format=esm --jsx=automatic --tsconfig=/app/tsconfig.json --log-level=warning --outfile=/tmp/check.mjs && node /tmp/check.mjs"
```

Expected: FAIL, with esbuild reporting `Could not resolve "@/utils/netToolViews"`.

- [ ] **Step 3: Add the chart colours**

At the end of `frontend/src/utils/colors.ts`, after `chartPalette`, append:

```ts

// Axis, grid and tooltip colours for Recharts charts. Hex, like chartPalette,
// because Recharts draws SVG; these are the values the existing charts use.
export const chartChrome = {
  grid: '#16303a',
  tick: '#7A8A94',
  tooltipBg: '#0f172a',
  tooltipBorder: 'rgba(255,255,255,0.1)',
  tooltipText: '#e2e8f0',
}
```

- [ ] **Step 4: Add the view models**

Create `frontend/src/utils/netToolViews.ts`:

```ts
// View models for the run result panels, built from a run's events. Pure, so
// the live Tools page, the run detail page and the throwaway checks agree.

import { colors } from '@/utils/colors'
import { formatDuration } from '@/utils/formatters'
import { NUMBER_FIELDS } from '@/utils/netTools'
import type {
  DNSAnswer,
  HopName,
  HopProbe,
  HopStats,
  NetTool,
  PingError,
  PingReply,
  PingSummary,
  PingTimeout,
  PortResult,
  RoundDone,
  ScanStart,
  ToolParams,
  ToolRun,
  ToolRunEvent,
  TraceSummary,
} from '@/types/netTools'

/** Stored events plus streamed ones, by seq, without duplicates. */
export function mergeEvents(stored: ToolRunEvent[], live: ToolRunEvent[]): ToolRunEvent[] {
  if (live.length === 0) return stored
  const bySeq = new Map<number, ToolRunEvent>()
  for (const e of stored) bySeq.set(e.seq, e)
  for (const e of live) if (!bySeq.has(e.seq)) bySeq.set(e.seq, e)
  return [...bySeq.values()].sort((a, b) => a.seq - b.seq)
}

// ---- ping

export interface PingRow {
  seq: number
  kind: 'reply' | 'timeout' | 'error'
  rtt_ms: number | null
  ttl: number | null
  from: string | null
  message: string | null
}

export interface PingPoint {
  seq: number
  rtt: number | null
}

export interface PingView {
  rows: PingRow[]
  /** One point per probe; a gap where there was no reply. */
  chart: PingPoint[]
  /** Computed from the events, for the tiles while the run is live. */
  stats: PingSummary
}

export function pingView(events: ToolRunEvent[]): PingView {
  const rows: PingRow[] = []
  for (const e of events) {
    if (e.type === 'reply') {
      const d = e.data as PingReply
      rows.push({ seq: d.seq, kind: 'reply', rtt_ms: d.rtt_ms, ttl: d.ttl, from: d.from, message: null })
    } else if (e.type === 'timeout') {
      const d = e.data as PingTimeout
      rows.push({ seq: d.seq, kind: 'timeout', rtt_ms: null, ttl: null, from: null, message: null })
    } else if (e.type === 'error') {
      const d = e.data as PingError
      rows.push({ seq: d.seq, kind: 'error', rtt_ms: null, ttl: null, from: null, message: d.message })
    }
  }
  rows.sort((a, b) => a.seq - b.seq)
  const rtts = rows.filter((r) => r.kind === 'reply').map((r) => r.rtt_ms as number)
  const sent = rows.length
  const received = rtts.length
  const stats: PingSummary = {
    sent,
    received,
    loss_pct: sent === 0 ? 0 : Math.round(((sent - received) / sent) * 1000) / 10,
    min_ms: received ? Math.min(...rtts) : null,
    avg_ms: received ? rtts.reduce((s, v) => s + v, 0) / received : null,
    max_ms: received ? Math.max(...rtts) : null,
    // Ruling 5: the mean absolute difference between consecutive replies.
    jitter_ms:
      received >= 2 ? rtts.slice(1).reduce((s, v, i) => s + Math.abs(v - rtts[i]), 0) / (received - 1) : null,
  }
  return { rows, chart: rows.map((r) => ({ seq: r.seq, rtt: r.rtt_ms })), stats }
}

// ---- traceroute

export interface TraceView {
  /** The last round finished (0 before the first). */
  round: number
  hops: HopStats[]
  reached: boolean
}

/** The MTR table: the summary's hops once the run is over, else the latest
 *  round_done's cumulative hops (ruling 4), with hop_name names filled in. */
export function traceView(events: ToolRunEvent[], summary: TraceSummary | null): TraceView {
  let latest: RoundDone | null = null
  let reached = false
  const names = new Map<string, string>()
  for (const e of events) {
    if (e.type === 'round_done') {
      const d = e.data as RoundDone
      if (!latest || d.round > latest.round) latest = d
    } else if (e.type === 'hop') {
      if ((e.data as HopProbe).reached) reached = true
    } else if (e.type === 'hop_name') {
      const d = e.data as HopName
      names.set(d.addr, d.name)
    }
  }
  const base = (summary ? summary.hops : latest?.hops) ?? []
  const hops = base.map((h) => {
    if (h.name) return h
    const name = (h.addrs ?? []).map((a) => names.get(a)).find((n) => !!n)
    return name ? { ...h, name } : h
  })
  return { round: latest?.round ?? 0, hops, reached: summary ? summary.reached : reached }
}

// ---- dns

export function dnsView(events: ToolRunEvent[]): DNSAnswer | null {
  const e = events.find((x) => x.type === 'answer')
  return e ? (e.data as DNSAnswer) : null
}

// ---- tcp

export interface PortsView {
  /** From the start event; null before it arrives. */
  total: number | null
  done: number
  /** 0-100. */
  pct: number
  open: PortResult[]
  closed: PortResult[]
  filtered: PortResult[]
}

export function portsView(events: ToolRunEvent[]): PortsView {
  let total: number | null = null
  const open: PortResult[] = []
  const closed: PortResult[] = []
  const filtered: PortResult[] = []
  for (const e of events) {
    if (e.type === 'start') total = (e.data as ScanStart).total
    else if (e.type === 'port') {
      const p = e.data as PortResult
      if (p.state === 'open') open.push(p)
      else if (p.state === 'closed') closed.push(p)
      else filtered.push(p)
    }
  }
  const byPort = (a: PortResult, b: PortResult) => a.port - b.port
  const done = open.length + closed.length + filtered.length
  return {
    total,
    done,
    pct: total ? Math.min(100, Math.round((done / total) * 100)) : 0,
    open: open.sort(byPort),
    closed: closed.sort(byPort),
    filtered: filtered.sort(byPort),
  }
}

/** "22 ssh", or the bare number when the port has no well-known name. */
export function portLabel(p: PortResult): string {
  return p.service ? `${p.port} ${p.service}` : `${p.port}`
}

// ---- shared

/** Colour for a loss percentage: none, some, all. */
export function lossTone(pct: number): string {
  if (pct >= 100) return colors.error.text
  if (pct > 0) return colors.warning.text
  return 'text-slate-300'
}

/** "Count 5 · Interval 1000 ms · …": what a run was asked to do. */
export function paramsText(tool: NetTool, p: ToolParams): string {
  const parts: string[] = []
  if (tool === 'dns') {
    parts.push(`Record type ${p.record_type ?? 'A'}`)
    parts.push(p.server ? `Server ${p.server}` : 'System resolver')
  }
  if (tool === 'tcp') parts.push(`Ports ${p.ports ?? 'common'}`)
  for (const f of NUMBER_FIELDS[tool]) {
    const v = p[f.key]
    if (v != null) parts.push(`${f.label} ${v}${f.unit ? ` ${f.unit}` : ''}`)
  }
  return parts.join(' · ')
}

/** "250 ms" or "1m 5s" for a finished run; null before it finishes. */
export function runDuration(run: Pick<ToolRun, 'started_at' | 'finished_at'>): string | null {
  if (!run.started_at || !run.finished_at) return null
  const ms = new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()
  if (!Number.isFinite(ms) || ms < 0) return null
  return ms < 1000 ? `${ms} ms` : formatDuration(Math.round(ms / 1000))
}
```

- [ ] **Step 5: Run the check to see it pass**

Run the command from Step 2 again.

Expected: PASS, printing `netToolViews: all checks passed`.

- [ ] **Step 6: Add the four panels and the dispatch**

Create `frontend/src/components/netTools/PingResult.tsx`:

```tsx
import { useMemo } from 'react'
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { PingSummary, ToolRun, ToolRunEvent } from '@/types/netTools'
import { chartChrome, chartPalette, colors } from '@/utils/colors'
import { formatLoss, formatMs, isFinalStatus } from '@/utils/netTools'
import { lossTone, pingView, type PingRow } from '@/utils/netToolViews'

interface Props {
  run: ToolRun
  events: ToolRunEvent[]
}

const RESULT: Record<PingRow['kind'], { label: string; tone: string }> = {
  reply: { label: 'Reply', tone: colors.operational.text },
  timeout: { label: 'Timed out', tone: colors.warning.text },
  error: { label: 'Error', tone: colors.error.text },
}

function Tile({ label, value, sub, tone = 'text-white' }: { label: string; value: string; sub?: string; tone?: string }) {
  return (
    <div className="rounded-lg border border-white/10 bg-slate-900/40 p-3">
      <p className="vs-eyebrow">{label}</p>
      <p className={`vs-readout mt-1 text-xl font-light ${tone}`}>{value}</p>
      {sub && <p className="mt-0.5 text-xs text-slate-500">{sub}</p>}
    </div>
  )
}

/** Ping: loss and RTT tiles, a live RTT chart and the reply list. The tiles
 *  show the server's summary once the run is over, the live events before. */
export default function PingResult({ run, events }: Props) {
  const view = useMemo(() => pingView(events), [events])
  const s: PingSummary = isFinalStatus(run.status) && run.summary ? (run.summary as PingSummary) : view.stats
  const count = Math.max(run.params.count ?? 0, view.chart.length, 1)

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
        <Tile
          label="Loss"
          value={s.sent ? formatLoss(s.loss_pct) : '—'}
          sub={`${s.received} of ${s.sent} replied`}
          tone={s.sent ? lossTone(s.loss_pct) : 'text-white'}
        />
        <Tile label="Min" value={formatMs(s.min_ms)} />
        <Tile label="Avg" value={formatMs(s.avg_ms)} />
        <Tile label="Max" value={formatMs(s.max_ms)} />
        <Tile label="Jitter" value={formatMs(s.jitter_ms)} />
      </div>

      {view.chart.length > 0 && (
        <div className="h-48 rounded-lg border border-white/10 bg-slate-900/30 p-3">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={view.chart} margin={{ top: 5, right: 10, bottom: 0, left: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke={chartChrome.grid} strokeOpacity={0.6} />
              <XAxis
                dataKey="seq"
                type="number"
                domain={[1, count]}
                allowDecimals={false}
                tick={{ fontSize: 10, fill: chartChrome.tick }}
              />
              <YAxis tickFormatter={(v: number) => `${v} ms`} tick={{ fontSize: 10, fill: chartChrome.tick }} width={56} />
              <Tooltip
                labelFormatter={(seq: number) => `Probe ${seq}`}
                formatter={(v: number) => [formatMs(v), 'RTT']}
                contentStyle={{
                  background: chartChrome.tooltipBg,
                  border: `1px solid ${chartChrome.tooltipBorder}`,
                  borderRadius: 8,
                  color: chartChrome.tooltipText,
                }}
              />
              <Line
                type="monotone"
                dataKey="rtt"
                stroke={chartPalette[0]}
                strokeWidth={2}
                dot={{ r: 2 }}
                connectNulls={false}
                isAnimationActive={false}
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}

      {view.rows.length > 0 && (
        <div className="max-h-72 overflow-y-auto rounded-lg border border-white/10">
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-slate-900">
              <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                <th className="px-3 py-2 font-medium">#</th>
                <th className="px-3 py-2 font-medium">Result</th>
                <th className="px-3 py-2 text-right font-medium">RTT</th>
                <th className="px-3 py-2 text-right font-medium">TTL</th>
                <th className="px-3 py-2 font-medium">From</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {view.rows.map((r) => (
                <tr key={r.seq}>
                  <td className="px-3 py-1.5 tabular-nums text-slate-400">{r.seq}</td>
                  <td className={`px-3 py-1.5 ${RESULT[r.kind].tone}`}>
                    {r.kind === 'error' && r.message ? r.message : RESULT[r.kind].label}
                  </td>
                  <td className="px-3 py-1.5 text-right tabular-nums text-slate-300">{formatMs(r.rtt_ms)}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums text-slate-400">{r.ttl ?? '—'}</td>
                  <td className="px-3 py-1.5 font-mono text-xs text-slate-400">{r.from ?? '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
```

Create `frontend/src/components/netTools/TracerouteResult.tsx`:

```tsx
import { useMemo } from 'react'
import type { ToolRun, ToolRunEvent, TraceSummary } from '@/types/netTools'
import { formatLoss, formatMs, isFinalStatus } from '@/utils/netTools'
import { lossTone, traceView } from '@/utils/netToolViews'

interface Props {
  run: ToolRun
  events: ToolRunEvent[]
}

/** Traceroute as an MTR table: one row per hop, updated after each round. */
export default function TracerouteResult({ run, events }: Props) {
  const final = isFinalStatus(run.status)
  const summary = final && run.summary ? (run.summary as TraceSummary) : null
  const view = useMemo(() => traceView(events, summary), [events, summary])
  const rounds = run.params.rounds ?? 5

  if (view.hops.length === 0) {
    return (
      <p className="text-sm text-slate-500">{final ? 'No hops were recorded.' : 'Waiting for the first round…'}</p>
    )
  }

  const lastAnswer = view.hops.filter((h) => h.received > 0).reduce((t, h) => Math.max(t, h.ttl), 0)
  let note: string
  if (!final) note = `After round ${view.round} of ${rounds}.`
  else if (view.reached) note = `Reached the destination in ${view.hops.length} ${view.hops.length === 1 ? 'hop' : 'hops'}.`
  else if (lastAnswer > 0) note = `The destination did not answer; the last hop that answered is hop ${lastAnswer}.`
  else note = 'No hop answered. ICMP time-exceeded replies may be blocked along the way.'

  const num = 'px-3 py-2 text-right tabular-nums text-slate-300'
  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-lg border border-white/10">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-white/10 text-left text-xs text-slate-400">
              <th className="px-3 py-2 font-medium">Hop</th>
              <th className="px-3 py-2 font-medium">Host</th>
              <th className="px-3 py-2 text-right font-medium">Loss</th>
              <th className="px-3 py-2 text-right font-medium">Sent</th>
              <th className="px-3 py-2 text-right font-medium">Last</th>
              <th className="px-3 py-2 text-right font-medium">Avg</th>
              <th className="px-3 py-2 text-right font-medium">Best</th>
              <th className="px-3 py-2 text-right font-medium">Worst</th>
              <th className="px-3 py-2 text-right font-medium">Stdev</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5">
            {view.hops.map((h) => {
              const addrs = h.addrs ?? []
              return (
                <tr key={h.ttl}>
                  <td className="px-3 py-2 tabular-nums text-slate-400">{h.ttl}</td>
                  <td className="px-3 py-2">
                    {h.name && <div className="text-slate-200">{h.name}</div>}
                    <div className="font-mono text-xs text-slate-400">
                      {addrs.length > 0 ? addrs.join(', ') : 'no reply'}
                    </div>
                  </td>
                  <td className={`px-3 py-2 text-right tabular-nums ${lossTone(h.loss_pct)}`}>{formatLoss(h.loss_pct)}</td>
                  <td className={num}>{h.sent}</td>
                  <td className={num}>{formatMs(h.last_ms)}</td>
                  <td className={num}>{formatMs(h.avg_ms)}</td>
                  <td className={num}>{formatMs(h.best_ms)}</td>
                  <td className={num}>{formatMs(h.worst_ms)}</td>
                  <td className={num}>{formatMs(h.stdev_ms)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <p className="text-xs text-slate-500">{note}</p>
    </div>
  )
}
```

Create `frontend/src/components/netTools/DNSResult.tsx`:

```tsx
import { useMemo } from 'react'
import type { DNSRecord, ToolRun, ToolRunEvent } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { formatMs, isFinalStatus, STATUS_PILL } from '@/utils/netTools'
import { dnsView } from '@/utils/netToolViews'

interface Props {
  run: ToolRun
  events: ToolRunEvent[]
}

function Records({ title, records }: { title: string; records: DNSRecord[] | null }) {
  const rows = records ?? []
  return (
    <section>
      <h3 className="mb-2 text-sm font-medium text-white">
        {title} <span className="text-slate-500">({rows.length})</span>
      </h3>
      {rows.length === 0 ? (
        <p className="text-sm text-slate-500">None</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-white/10">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                <th className="px-3 py-2 font-medium">Name</th>
                <th className="px-3 py-2 font-medium">Type</th>
                <th className="px-3 py-2 text-right font-medium">TTL (s)</th>
                <th className="px-3 py-2 font-medium">Data</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {rows.map((r, i) => (
                <tr key={`${r.name}-${r.type}-${i}`}>
                  <td className="px-3 py-1.5 font-mono text-xs text-slate-300">{r.name}</td>
                  <td className="px-3 py-1.5 text-slate-400">{r.type}</td>
                  <td className="px-3 py-1.5 text-right tabular-nums text-slate-400">{r.ttl}</td>
                  <td className="break-all px-3 py-1.5 font-mono text-xs text-slate-200">{r.data}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

/** DNS: who answered and how, then the answer, authority and additional
 *  sections with their TTLs. */
export default function DNSResult({ run, events }: Props) {
  const answer = useMemo(() => dnsView(events), [events])
  if (!answer) {
    return (
      <p className="text-sm text-slate-500">
        {isFinalStatus(run.status) ? 'No answer was received.' : 'Waiting for the answer…'}
      </p>
    )
  }
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
        <span className={`rd-pill ${answer.rcode === 'NOERROR' ? STATUS_PILL.done : STATUS_PILL.failed}`}>
          {answer.rcode}
        </span>
        <span className="text-slate-300">
          from <span className="font-mono">{answer.server}</span>
        </span>
        <span className="text-slate-400">{formatMs(answer.rtt_ms)}</span>
        <span className="text-slate-400">
          {answer.authoritative ? 'Authoritative answer' : 'Non-authoritative answer'}
        </span>
        {answer.tcp && (
          <span className={`text-xs ${colors.warning.text}`}>
            {answer.truncated ? 'Truncated over UDP, so this answer came over TCP' : 'Answered over TCP'}
          </span>
        )}
      </div>
      <Records title="Answer" records={answer.answer} />
      <Records title="Authority" records={answer.authority} />
      <Records title="Additional" records={answer.additional} />
    </div>
  )
}
```

Create `frontend/src/components/netTools/PortsResult.tsx`:

```tsx
import { useMemo, useState } from 'react'
import type { ToolRun, ToolRunEvent } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { formatMs, isFinalStatus } from '@/utils/netTools'
import { portLabel, portsView } from '@/utils/netToolViews'

interface Props {
  run: ToolRun
  events: ToolRunEvent[]
}

/** Ports: progress, open ports first with their service names, and closed
 *  and filtered counts that expand to the list. */
export default function PortsResult({ run, events }: Props) {
  const view = useMemo(() => portsView(events), [events])
  const [shown, setShown] = useState<'closed' | 'filtered' | null>(null)
  const final = isFinalStatus(run.status)
  const toggle = (which: 'closed' | 'filtered') => setShown((s) => (s === which ? null : which))
  const list = shown === 'closed' ? view.closed : shown === 'filtered' ? view.filtered : []

  return (
    <div className="space-y-4">
      <div>
        <div className="mb-1 flex justify-between text-xs text-slate-400">
          <span>{view.total == null ? 'Starting…' : `${view.done} of ${view.total} ports checked`}</span>
          <span>{view.pct}%</span>
        </div>
        <div className="h-2 overflow-hidden rounded-full bg-white/5">
          <div
            className={`h-full bg-gradient-to-r ${colors.operational.gradient} transition-all`}
            style={{ width: `${view.pct}%` }}
          />
        </div>
      </div>

      <section>
        <h3 className="mb-2 text-sm font-medium text-white">
          Open <span className="text-slate-500">({view.open.length})</span>
        </h3>
        {view.open.length === 0 ? (
          <p className="text-sm text-slate-500">{final ? 'No open ports.' : 'None yet.'}</p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-white/10">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10 text-left text-xs text-slate-400">
                  <th className="px-3 py-2 font-medium">Port</th>
                  <th className="px-3 py-2 font-medium">Service</th>
                  <th className="px-3 py-2 text-right font-medium">Connect time</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {view.open.map((p) => (
                  <tr key={p.port}>
                    <td className={`px-3 py-1.5 tabular-nums ${colors.operational.text}`}>{p.port}</td>
                    <td className="px-3 py-1.5 text-slate-300">{p.service || '—'}</td>
                    <td className="px-3 py-1.5 text-right tabular-nums text-slate-400">{formatMs(p.rtt_ms)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          className="btn-secondary !py-1"
          aria-expanded={shown === 'closed'}
          disabled={view.closed.length === 0}
          onClick={() => toggle('closed')}
        >
          Closed ({view.closed.length})
        </button>
        <button
          type="button"
          className="btn-secondary !py-1"
          aria-expanded={shown === 'filtered'}
          disabled={view.filtered.length === 0}
          onClick={() => toggle('filtered')}
        >
          Filtered ({view.filtered.length})
        </button>
      </div>
      {list.length > 0 && (
        <p className="font-mono text-xs leading-relaxed text-slate-400">{list.map(portLabel).join(', ')}</p>
      )}
      <p className="text-xs text-slate-500">
        Closed: the host refused the connection. Filtered: no answer within the timeout, or the host is unreachable.
      </p>
    </div>
  )
}
```

Create `frontend/src/components/netTools/RunResult.tsx`:

```tsx
import type { ToolRun, ToolRunEvent } from '@/types/netTools'
import { isFinalStatus } from '@/utils/netTools'
import DNSResult from './DNSResult'
import PingResult from './PingResult'
import PortsResult from './PortsResult'
import TracerouteResult from './TracerouteResult'

/** The result panel for a run's tool, from its events (stored, live, or
 *  both merged). A finished run with no events shows nothing here: the
 *  status line already says why. */
export default function RunResult({ run, events }: { run: ToolRun; events: ToolRunEvent[] }) {
  if (events.length === 0) {
    return isFinalStatus(run.status) ? null : <p className="text-sm text-slate-500">No results yet.</p>
  }
  if (run.tool === 'ping') return <PingResult run={run} events={events} />
  if (run.tool === 'traceroute') return <TracerouteResult run={run} events={events} />
  if (run.tool === 'dns') return <DNSResult run={run} events={events} />
  return <PortsResult run={run} events={events} />
}
```

- [ ] **Step 7: Add the run detail page**

Create `frontend/src/pages/ToolRunDetail.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft, Loader2, RotateCcw, Square } from 'lucide-react'
import { useAuthContext } from '@/context/AuthContext'
import { cancelToolRun, useToolRun } from '@/hooks/useNetTools'
import { useRunStream } from '@/hooks/useRunStream'
import NoToolsAccess from '@/components/netTools/NoToolsAccess'
import RunResult from '@/components/netTools/RunResult'
import RunStatusLine from '@/components/netTools/RunStatusLine'
import type { ApiError } from '@/services/api'
import type { ToolRun, Vantage } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { formatDatetime } from '@/utils/formatters'
import { canUseNetTools, isFinalStatus, runTargetText, TOOL_LABEL, toolsQuery } from '@/utils/netTools'
import { mergeEvents, paramsText, runDuration } from '@/utils/netToolViews'

/** /tools/runs/:id: one run's result from its stored events, followed live
 *  while it is still going. */
export default function ToolRunDetail() {
  const { id } = useParams<{ id: string }>()
  const { currentUser } = useAuthContext()
  if (!canUseNetTools(currentUser)) return <NoToolsAccess />
  // Keyed by id: moving to another run starts clean and closes the old stream.
  return id ? <RunDetail key={id} id={id} /> : null
}

function BackLink() {
  return (
    <Link to="/tools" className="inline-flex items-center gap-2 text-sm text-slate-400 transition hover:text-white">
      <ArrowLeft className="h-4 w-4" /> Back to Network Tools
    </Link>
  )
}

function RunDetail({ id }: { id: string }) {
  const navigate = useNavigate()
  const { currentUser } = useAuthContext()
  const { detail, loading, error } = useToolRun(id)
  const stored = detail?.run ?? null
  // Only an unfinished run is followed; a finished one is all in `detail`.
  // The stream replays from the start, and mergeEvents drops the overlap.
  const stream = useRunStream(stored && !isFinalStatus(stored.status) ? id : null)
  const [cancelled, setCancelled] = useState<ToolRun | null>(null)
  const [cancelling, setCancelling] = useState(false)
  const [cancelError, setCancelError] = useState<string | null>(null)
  const events = useMemo(() => mergeEvents(detail?.events ?? [], stream.events), [detail, stream.events])

  if (loading && !detail) {
    return (
      <div className="flex items-center gap-2 p-6 text-sm text-slate-400">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading run…
      </div>
    )
  }
  if (!stored) {
    return (
      <div className="space-y-4">
        <BackLink />
        <div className={`rounded-lg border p-6 text-sm ${colors.error.border} ${colors.error.bg} ${colors.error.text}`}>
          {error ?? 'That run could not be found.'}
        </div>
      </div>
    )
  }

  const run: ToolRun = stream.run ?? cancelled ?? (stream.status ? { ...stored, status: stream.status } : stored)
  const live = !isFinalStatus(run.status)
  // Grant holders cancel their own runs; admins cancel any.
  const canCancel = live && (!!currentUser?.is_admin || run.user_id === currentUser?.user_id)
  const duration = runDuration(run)

  const cancel = async () => {
    setCancelling(true)
    setCancelError(null)
    try {
      setCancelled(await cancelToolRun(run.id))
    } catch (err) {
      setCancelError((err as ApiError).message || 'Could not cancel the run')
    } finally {
      setCancelling(false)
    }
  }

  const runAgain = () => {
    const vantage: Vantage =
      run.vantage_kind === 'agent' && run.agent_id ? { kind: 'agent', agent_id: run.agent_id } : { kind: 'sentinel' }
    navigate(`/tools${toolsQuery({ tool: run.tool, target: run.target, vantage, params: run.params })}`)
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <BackLink />
          <h1 className="vs-title mt-2 break-words text-3xl">
            {TOOL_LABEL[run.tool]} · {runTargetText(run)}
          </h1>
          <p className="mt-1 text-sm text-slate-400">
            From {run.vantage_name} · by {run.username} · {formatDatetime(run.created_at)}
            {duration ? ` · took ${duration}` : ''}
          </p>
          <p className="mt-1 text-xs text-slate-500">{paramsText(run.tool, run.params)}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          {canCancel && (
            <button className="btn-secondary" disabled={cancelling} onClick={() => void cancel()}>
              <Square className="h-4 w-4" /> Cancel
            </button>
          )}
          <button className="btn-primary" onClick={runAgain}>
            <RotateCcw className="h-4 w-4" /> Run again
          </button>
        </div>
      </div>

      {cancelError && (
        <div className={`rounded-lg border p-3 text-sm ${colors.error.border} ${colors.error.bg} ${colors.error.text}`}>
          {cancelError}
        </div>
      )}

      <div className="card space-y-4 p-5">
        <RunStatusLine run={run} events={events} streamError={stream.error} />
        <RunResult run={run} events={events} />
      </div>
    </div>
  )
}
```

- [ ] **Step 8: Show the results on the Tools page and route the detail page**

In `frontend/src/pages/NetworkTools.tsx`, after `import RunHistory from '@/components/netTools/RunHistory'`, add:

```ts
import RunResult from '@/components/netTools/RunResult'
```

In the same file, replace:

```tsx
          <p className="text-sm text-slate-500">{stream.events.length} events received</p>
```

with:

```tsx
          <RunResult run={run} events={stream.events} />
```

In `frontend/src/App.tsx`, after `const NetworkTools = lazy(() => import('@/pages/NetworkTools'))`, add:

```ts
const ToolRunDetail = lazy(() => import('@/pages/ToolRunDetail'))
```

In the same file, after `<Route path="/tools" element={<NetworkTools />} />`, add:

```tsx
              <Route path="/tools/runs/:id" element={<ToolRunDetail />} />
```

- [ ] **Step 9: Run the gate**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output. Run the Step 2 check once more (expected: `netToolViews: all checks passed`), then delete it with `rm -r "$CHECK_DIR"`. The browser checks are the owner's; Task 19 lists them.

- [ ] **Step 10: Commit**

```bash
git add frontend/src/utils/colors.ts frontend/src/utils/netToolViews.ts \
  frontend/src/components/netTools/PingResult.tsx frontend/src/components/netTools/TracerouteResult.tsx \
  frontend/src/components/netTools/DNSResult.tsx frontend/src/components/netTools/PortsResult.tsx \
  frontend/src/components/netTools/RunResult.tsx frontend/src/pages/ToolRunDetail.tsx \
  frontend/src/pages/NetworkTools.tsx frontend/src/App.tsx
git commit -m "feat(tools): result panels and the run detail page

Ping tiles, RTT chart and replies; an MTR table updated per round; DNS
sections with TTLs; port progress with open ports first. The run page
shows stored events, follows a live run and fills the form to run again.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 18: Admin screens and entry points

This task covers the admin side and the ways in:
- A "Network tools" Settings tab: the allowlist editor with errors against the bad lines, "Allow runs from the Sentinel server", and retention days.
- A "Network tools" checkbox per user on the Users page. For admins it is checked and disabled with "Admins always can".
- "Allow network tools" in the agent edit form, with the agent's own state beside it.
- "Enable network tools on this server" in the add-agent install instructions.
- The entry points: "Network tools" on a server's page, and "Ping / Trace" on a device's and a monitor's page. All three show only to permitted users.

The Users page reads every user from `GET /users`; Task 6 already made that route send `net_tools` to admin callers, so this task is frontend only.

**Files:**
- Create: `frontend/src/components/settings/NetToolsSettings.tsx`
- Modify: `frontend/src/pages/Settings.tsx` (the `nettools` tab and `?tab=`)
- Modify: `frontend/src/pages/AdminUsers.tsx` (the Network tools column)
- Modify: `frontend/src/components/EditServerAgentModal.tsx` (Allow network tools)
- Modify: `frontend/src/components/AddServerAgentModal.tsx` (Enable network tools on this server)
- Modify: `frontend/src/pages/ServerDetail.tsx`, `frontend/src/pages/network/DeviceDetail.tsx` and `frontend/src/pages/MonitorDetail.tsx` (entry points)
- Test: the frontend gate

`AgentSettingsFields.tsx` is not touched. It is shared with the create form, where the agent does not exist yet and has no tools state to show.

**Interfaces:**
- Consumes (Task 6): `GET /api/v1/users` items carry `net_tools` for admin callers.
- Consumes (Task 12, HTTP): `GET`/`PUT /settings/net-tools`, `PATCH /users/:id/net-tools` and `PUT /agents/:agent_id/tools`, through Task 15's hooks.
- Consumes (Task 14): the install scripts read `ENABLE_TOOLS` from the environment (`ENABLE_TOOLS="${ENABLE_TOOLS:-false}"` on Linux and Docker, `$env:ENABLE_TOOLS` on Windows).
- Consumes (Task 15):
  - `useNetToolsSettings()`, `setUserNetTools(userId, enabled)` and `setAgentTools(agentId, enabled)` from `@/hooks/useNetTools`.
  - From `@/utils/netTools`: `canUseNetTools`, `toolsQuery`, `monitorHost`, `agentToolsStateText`, `allowlistErrors`, `lineErrors`, `RETENTION_MIN_DAYS` and `RETENTION_MAX_DAYS`.
  - `Agent.tools_enabled`, `Agent.tools_local` and `ManagedUser.net_tools`.
- Consumes (existing): `SettingsCard`, `useAuthContext`, `colors`.
- Produces:
  - `NetToolsSettings({ push })` in `@/components/settings/NetToolsSettings`.
  - `/settings?tab=nettools` opens the tab, for admins. The Tools page banner links there.

- [ ] **Step 1: Add the settings tab**

Create `frontend/src/components/settings/NetToolsSettings.tsx`:

```tsx
import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import SettingsCard from '@/components/SettingsCard'
import { useNetToolsSettings } from '@/hooks/useNetTools'
import type { ApiError } from '@/services/api'
import type { AllowlistEntryError } from '@/types/netTools'
import { colors } from '@/utils/colors'
import { allowlistErrors, lineErrors, RETENTION_MAX_DAYS, RETENTION_MIN_DAYS } from '@/utils/netTools'

interface Props {
  push: (msg: string, type?: 'success' | 'error' | 'info') => void
}

/** Settings → Network tools: what may be probed, whether the Sentinel
 *  server itself runs tools, and how long run history is kept. Admin-only,
 *  like its API. */
export default function NetToolsSettings({ push }: Props) {
  const { settings, loading, error, save } = useNetToolsSettings()
  // Seeded once from the server, then owned by the form, so nothing typed
  // is overwritten by a later answer.
  const [text, setText] = useState<string | null>(null)
  const [serverEnabled, setServerEnabled] = useState(true)
  const [retention, setRetention] = useState(30)
  const [badEntries, setBadEntries] = useState<AllowlistEntryError[]>([])
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!settings || text !== null) return
    setText((settings.allowlist ?? []).join('\n'))
    setServerEnabled(settings.server_enabled)
    setRetention(settings.retention_days)
  }, [settings, text])

  const lines = useMemo(() => lineErrors(text ?? '', badEntries), [text, badEntries])
  const retentionValid =
    Number.isInteger(retention) && retention >= RETENTION_MIN_DAYS && retention <= RETENTION_MAX_DAYS
  const errorBox = `rounded-lg border p-3 text-sm ${colors.error.border} ${colors.error.bg} ${colors.error.text}`

  if (text === null) {
    return loading ? (
      <div className="flex items-center gap-2 p-6 text-sm text-slate-400">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading network tools settings…
      </div>
    ) : (
      <div className={errorBox}>{error ?? 'Could not load the network tools settings'}</div>
    )
  }

  const submit = async () => {
    if (!retentionValid) return
    setSaving(true)
    setSaveError(null)
    try {
      const saved = await save({
        allowlist: text.split('\n').map((l) => l.trim()).filter(Boolean),
        server_enabled: serverEnabled,
        retention_days: retention,
      })
      // The server trims, de-duplicates and lower-cases host names: show
      // what it stored.
      setText((saved.allowlist ?? []).join('\n'))
      setBadEntries([])
      push('Network tools settings saved', 'success')
    } catch (err) {
      const e = err as ApiError
      setBadEntries(allowlistErrors(e.details))
      setSaveError(e.message || 'Could not save the network tools settings')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-6">
      <p className="text-sm text-slate-400">
        Admins can always run network tools; other users need it granted on the{' '}
        <Link className="text-primary-400 hover:underline" to="/admin/users">
          Users
        </Link>{' '}
        page. Each server agent is allowed from its Edit form.
      </p>

      <SettingsCard
        title="Allowed targets"
        description="One entry per line: an IPv4 address (10.0.0.5), a subnet no broader than /8 (10.0.0.0/24), a host name (fileserver.example.org) or a wildcard (*.example.org). Nothing can be probed until this list has entries."
      >
        <textarea
          rows={8}
          className="w-full font-mono"
          value={text}
          spellCheck={false}
          aria-label="Allowed targets, one per line"
          onChange={(e) => setText(e.target.value)}
        />
        {lines.length > 0 && (
          <ul className={`space-y-1 text-xs ${colors.error.text}`}>
            {lines.map((l, i) => (
              <li key={`${l.entry}-${i}`}>
                {l.line !== null && `Line ${l.line}: `}
                <span className="font-mono">{l.entry}</span> — {l.message}
              </li>
            ))}
          </ul>
        )}
        <p className="text-xs text-slate-500">
          Cloud metadata addresses, 0.0.0.0/8, multicast and broadcast are always blocked. Saving never affects runs
          already in progress.
        </p>
      </SettingsCard>

      <SettingsCard
        title="Sentinel server"
        description="Whether tools may run from the Sentinel server itself, as well as from enabled server agents."
      >
        <label className="flex cursor-pointer items-center gap-3">
          <input
            type="checkbox"
            className="h-4 w-4"
            checked={serverEnabled}
            onChange={(e) => setServerEnabled(e.target.checked)}
          />
          <span className="text-sm">Allow runs from the Sentinel server</span>
        </label>
      </SettingsCard>

      <SettingsCard title="Run history" description="Finished runs are deleted after this many days. Audit entries are kept.">
        <div className="flex items-center gap-2">
          <input
            type="number"
            min={RETENTION_MIN_DAYS}
            max={RETENTION_MAX_DAYS}
            value={retention}
            aria-label="Run history retention in days"
            className="w-32"
            onChange={(e) => setRetention(Number(e.target.value))}
          />
          <span className="text-sm text-slate-400">days</span>
        </div>
        {!retentionValid && (
          <p className={`text-xs ${colors.error.text}`}>
            Must be between {RETENTION_MIN_DAYS} and {RETENTION_MAX_DAYS} days.
          </p>
        )}
      </SettingsCard>

      {saveError && <div className={errorBox}>{saveError}</div>}

      <div className="flex justify-end border-t border-white/10 pt-4">
        <button className="btn-primary" disabled={saving || !retentionValid} onClick={() => void submit()}>
          {saving ? 'Saving…' : 'Save Network Tools Settings'}
        </button>
      </div>
    </div>
  )
}
```

In `frontend/src/pages/Settings.tsx`:

1. Replace `import { Link } from 'react-router-dom'` with `import { Link, useSearchParams } from 'react-router-dom'`.
2. After `import NotificationSettings from '@/pages/NotificationSettings'`, add `import NetToolsSettings from '@/components/settings/NetToolsSettings'`.
3. Replace `type Tab = 'system' | 'notifications' | 'about'` with `type Tab = 'system' | 'notifications' | 'nettools' | 'about'`.
4. Replace:

```ts
const TAB_LABEL: Record<Tab, string> = {
  system: 'System',
  notifications: 'Notifications',
  about: 'About',
}
```

with:

```ts
const TAB_LABEL: Record<Tab, string> = {
  system: 'System',
  notifications: 'Notifications',
  nettools: 'Network tools',
  about: 'About',
}
```

5. Replace:

```ts
  const tabs = useMemo<Tab[]>(
    () =>
      (['system', 'notifications', 'about'] as Tab[]).filter(
        (t) => (t !== 'notifications' && t !== 'system') || isAdmin
      ),
    [isAdmin]
  )
  const [tab, setTab] = useState<Tab>(() => (isAdmin ? 'system' : 'about'))
```

with:

```ts
  const tabs = useMemo<Tab[]>(
    () => (['system', 'notifications', 'nettools', 'about'] as Tab[]).filter((t) => t === 'about' || isAdmin),
    [isAdmin]
  )
  // ?tab= opens a tab directly (the Network Tools page links to
  // ?tab=nettools); a tab this user cannot see falls back to the default.
  const [searchParams] = useSearchParams()
  const [tab, setTab] = useState<Tab>(() => {
    const asked = searchParams.get('tab')
    if (asked && asked in TAB_LABEL && (asked === 'about' || isAdmin)) return asked as Tab
    return isAdmin ? 'system' : 'about'
  })
```

6. Replace `{tab === 'notifications' && isAdmin && <NotificationSettings />}` with:

```tsx
      {tab === 'notifications' && isAdmin && <NotificationSettings />}

      {tab === 'nettools' && isAdmin && <NetToolsSettings push={push} />}
```

- [ ] **Step 2: Add the Users page column**

In `frontend/src/pages/AdminUsers.tsx`:

1. After the line `} from '@/hooks/useUserManagement'`, add `import { setUserNetTools } from '@/hooks/useNetTools'`.
2. Replace `  const [modal, setModal] = useState<Modal>(null)` with:

```ts
  const [modal, setModal] = useState<Modal>(null)
  // The user whose network tools grant is being saved.
  const [toolsBusy, setToolsBusy] = useState<string | null>(null)
```

3. Directly before the line `  return (` that follows `doDelete`, add:

```ts
  const toggleNetTools = async (u: ManagedUser, enabled: boolean) => {
    setToolsBusy(u.id)
    try {
      await setUserNetTools(u.id, enabled)
      push(
        enabled ? `${u.username} can now use network tools` : `${u.username} can no longer use network tools`,
        'success'
      )
      await refetchUsers()
    } catch (err) {
      push((err as ApiError).message || 'Could not change network tools access', 'error')
    } finally {
      setToolsBusy(null)
    }
  }

```

4. Replace `                <th className="px-3 py-2 text-right font-medium">Actions</th>` with:

```tsx
                <th className="px-3 py-2 font-medium">Network tools</th>
                <th className="px-3 py-2 text-right font-medium">Actions</th>
```

5. Replace `<td colSpan={5} className="px-3 py-8 text-center text-slate-500">` with `<td colSpan={6} className="px-3 py-8 text-center text-slate-500">`.
6. Replace:

```tsx
                        {u.created_at ? format(new Date(u.created_at), 'MMM d, yyyy') : '—'}
                      </td>
```

with:

```tsx
                        {u.created_at ? format(new Date(u.created_at), 'MMM d, yyyy') : '—'}
                      </td>
                      <td className="px-3 py-2">
                        {u.is_admin ? (
                          <label className="flex items-center gap-2 text-xs text-slate-500">
                            <input type="checkbox" className="h-4 w-4" checked disabled readOnly />
                            Admins always can
                          </label>
                        ) : (
                          <label className="flex cursor-pointer items-center gap-2 text-xs text-slate-400">
                            <input
                              type="checkbox"
                              className="h-4 w-4"
                              checked={!!u.net_tools}
                              disabled={toolsBusy === u.id}
                              aria-label={`Network tools for ${u.username}`}
                              onChange={(e) => void toggleNetTools(u, e.target.checked)}
                            />
                            {u.net_tools ? 'Allowed' : 'Not allowed'}
                          </label>
                        )}
                      </td>
```

- [ ] **Step 3: Add "Allow network tools" to the agent edit form**

In `frontend/src/components/EditServerAgentModal.tsx`:

1. After `import type { ApiError } from '@/services/api'`, add:

```ts
import { setAgentTools } from '@/hooks/useNetTools'
import { colors } from '@/utils/colors'
import { agentToolsStateText } from '@/utils/netTools'
```

2. Replace `  const [error, setError] = useState<string | null>(null)` with:

```ts
  const [error, setError] = useState<string | null>(null)
  // Saved through its own audited endpoint (PUT /agents/:agent_id/tools),
  // apart from the general settings.
  const [toolsEnabled, setToolsEnabled] = useState(agent.tools_enabled)
  const [toolsSaving, setToolsSaving] = useState(false)
```

3. Replace:

```ts
    setValues(initial)
    setError(null)
    const t = window.setTimeout(() => firstFieldRef.current?.focus(), 50)
    return () => window.clearTimeout(t)
  }, [isOpen, initial])
```

with:

```ts
    setValues(initial)
    setToolsEnabled(agent.tools_enabled)
    setError(null)
    const t = window.setTimeout(() => firstFieldRef.current?.focus(), 50)
    return () => window.clearTimeout(t)
  }, [isOpen, initial, agent.tools_enabled])
```

4. Replace `  const changed =` (the line before `    values.name !== initial.name ||`) with `  const settingsChanged =`.
5. Replace:

```ts
    values.diskThresholdPercent !== initial.diskThresholdPercent

  if (!isOpen) return null
```

with:

```ts
    values.diskThresholdPercent !== initial.diskThresholdPercent
  const toolsChanged = toolsEnabled !== agent.tools_enabled
  const changed = settingsChanged || toolsChanged
  const saving = busy || toolsSaving

  if (!isOpen) return null
```

6. Replace the whole `save` function, from `  const save = async () => {` to its closing `  }`, with:

```ts
  const save = async () => {
    if (!valid || !changed) return
    setError(null)
    try {
      if (settingsChanged) {
        await update(agent.agent_id, {
          name: values.name.trim(),
          os_type: values.osType,
          check_interval: values.interval,
          retry_attempts: values.retries,
          // Sent even when empty: that is how an override is cleared and the
          // address goes back to whatever the agent detects.
          ip_address_override: values.ipOverride.trim(),
          notify_channels: notifyChannelsPayload(values),
          cpu_threshold_percent: thresholdPayload(values.cpuThresholdEnabled, values.cpuThresholdPercent),
          memory_threshold_percent: thresholdPayload(
            values.memoryThresholdEnabled,
            values.memoryThresholdPercent,
          ),
          disk_threshold_percent: thresholdPayload(values.diskThresholdEnabled, values.diskThresholdPercent),
        })
      }
      if (toolsChanged) {
        setToolsSaving(true)
        await setAgentTools(agent.agent_id, toolsEnabled)
      }
      push(`${values.name.trim()} updated`, 'success')
      onSaved()
      onClose()
    } catch (err) {
      setError((err as ApiError).message || 'Could not save the changes')
    } finally {
      setToolsSaving(false)
    }
  }
```

7. Replace:

```tsx
          <AgentSettingsFields
            values={values}
            onChange={setValues}
            errors={errors}
            firstFieldRef={firstFieldRef}
          />
```

with:

```tsx
          <AgentSettingsFields
            values={values}
            onChange={setValues}
            errors={errors}
            firstFieldRef={firstFieldRef}
          />

          <div className="border-t border-white/10" />

          {/* Both switches are needed: this one in Sentinel, and
              ENABLE_TOOLS=true on the host, which the agent reports. */}
          <section>
            <h3 className="mb-4 text-xs font-semibold uppercase tracking-widest text-slate-300">
              Network tools
            </h3>
            <label className="flex cursor-pointer items-center gap-3">
              <input
                type="checkbox"
                className="h-4 w-4 accent-emerald-500"
                checked={toolsEnabled}
                onChange={(e) => setToolsEnabled(e.target.checked)}
              />
              <span className="text-sm text-white">Allow network tools</span>
            </label>
            <p className={`mt-2 text-xs ${agent.tools_local ? 'text-slate-400' : colors.warning.text}`}>
              {agentToolsStateText(agent.tools_local)}
            </p>
            <p className="mt-1 text-xs text-slate-500">
              Lets admins and granted users run ping, traceroute, DNS lookups and port checks from this server.
            </p>
          </section>
```

8. Replace `            disabled={busy}` (the Cancel button) with `            disabled={saving}`. Replace `disabled={busy || !valid || !changed}` with `disabled={saving || !valid || !changed}`. Replace `{busy && <Loader2 className="h-4 w-4 animate-spin" />}` with `{saving && <Loader2 className="h-4 w-4 animate-spin" />}`.

- [ ] **Step 4: Add "Enable network tools on this server" to the install instructions**

In `frontend/src/components/AddServerAgentModal.tsx`, inside `InstallStep`:

1. Replace `  const { agent } = created` with:

```ts
  const { agent } = created
  // Adds ENABLE_TOOLS=true to every install command below. The admin toggle
  // in Sentinel (Edit → Allow network tools) is still needed.
  const [enableTools, setEnableTools] = useState(false)
```

2. Replace:

```ts
    `RETRY_ATTEMPTS="${agent.retry_attempts}"`,
  ].join(' \\\n  ')
```

with:

```ts
    `RETRY_ATTEMPTS="${agent.retry_attempts}"`,
    ...(enableTools ? ['ENABLE_TOOLS="true"'] : []),
  ].join(' \\\n  ')
```

3. Directly before `  const directDocker = `, add:

```ts
  const dockerToolsFlag = enableTools ? '  -e ENABLE_TOOLS="true" \\\n' : ''

```

4. Replace:

```
  -e RETRY_ATTEMPTS="${agent.retry_attempts}" \\
  sentinel-agent:local`
```

with:

```
  -e RETRY_ATTEMPTS="${agent.retry_attempts}" \\
${dockerToolsFlag}  sentinel-agent:local`
```

5. Replace:

```ts
    `$env:RETRY_ATTEMPTS="${agent.retry_attempts}"`,
  ].join('\n')
```

with:

```ts
    `$env:RETRY_ATTEMPTS="${agent.retry_attempts}"`,
    ...(enableTools ? ['$env:ENABLE_TOOLS="true"'] : []),
  ].join('\n')
```

6. Directly before `      {/* Horizontally scrollable so four tabs do not wrap on a phone. */}`, add:

```tsx
      <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-white/10 bg-slate-800/40 p-4">
        <input
          type="checkbox"
          className="mt-0.5 h-4 w-4 accent-emerald-500"
          checked={enableTools}
          onChange={(e) => setEnableTools(e.target.checked)}
        />
        <span>
          <span className="block text-sm font-medium text-white">Enable network tools on this server</span>
          <span className="mt-1 block text-xs text-slate-400">
            Adds ENABLE_TOOLS=true to the commands below, so ping, traceroute, DNS lookups and port checks can run
            from this server. Allow it in Sentinel as well, under Edit → Allow network tools.
          </span>
        </span>
      </label>

```

The `env` list feeds the one-click, Docker one-click and manual Linux commands; `windowsEnv` feeds both Windows commands; `dockerToolsFlag` covers Direct Docker Run. The install scripts write the value into the agent's config (Task 14). In the JS string `'  -e ENABLE_TOOLS="true" \\\n'`, `\\` is one backslash and `\n` a newline, matching the template's other ` \\` line ends.

- [ ] **Step 5: Add the entry points**

In `frontend/src/pages/ServerDetail.tsx`:

1. Replace `import { ArrowLeft, Server, Loader2, Terminal, Trash2, Pencil } from 'lucide-react'` with `import { ArrowLeft, Server, Loader2, Terminal, Trash2, Pencil, Radar } from 'lucide-react'`.
2. After `import type { ApiError } from '@/services/api'`, add `import { canUseNetTools, toolsQuery } from '@/utils/netTools'`.
3. Replace:

```tsx
        {isAdmin && (
          <div className="flex items-center gap-2">
            <button className="btn-secondary !py-1.5" onClick={() => setEditing(true)}>
```

with:

```tsx
        <div className="flex flex-wrap items-center gap-2">
          {canUseNetTools(currentUser) && (
            <Link
              className="btn-secondary !py-1.5"
              to={`/tools${toolsQuery({ vantage: { kind: 'agent', agent_id: agent.agent_id } })}`}
            >
              <Radar className="h-4 w-4" /> Network tools
            </Link>
          )}
          {isAdmin && (
          <>
            <button className="btn-secondary !py-1.5" onClick={() => setEditing(true)}>
```

4. Replace:

```tsx
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="inline h-4 w-4" />
            </button>
          </div>
        )}
      </div>
```

with:

```tsx
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="inline h-4 w-4" />
            </button>
          </>
          )}
        </div>
      </div>
```

In `frontend/src/pages/network/DeviceDetail.tsx`:

1. Replace `import { AlertTriangle, CheckCircle2, Loader2, Pause, Pencil, Play, RefreshCw, SlidersHorizontal, Trash2 } from 'lucide-react'` with `import { AlertTriangle, CheckCircle2, Loader2, Pause, Pencil, Play, Radar, RefreshCw, SlidersHorizontal, Trash2 } from 'lucide-react'`.
2. After `import type { ApiError } from '@/services/api'`, add:

```ts
import { useAuthContext } from '@/context/AuthContext'
import { canUseNetTools, toolsQuery } from '@/utils/netTools'
```

3. In `DeviceDetail`, replace:

```ts
  const navigate = useNavigate()
  const { device, loading, notFound, refetch } = useDevice(id)
```

with:

```ts
  const navigate = useNavigate()
  const { currentUser } = useAuthContext()
  const { device, loading, notFound, refetch } = useDevice(id)
```

4. Replace:

```tsx
        {canEdit && (
          <div className="flex flex-wrap gap-2">
            <button className="btn-secondary flex items-center gap-2" disabled={busy || !device.enabled || refreshWatch.phase === 'running'} onClick={() => void startRefresh()}>
```

with:

```tsx
        <div className="flex flex-wrap gap-2">
          {canUseNetTools(currentUser) && (
            <Link
              className="btn-secondary flex items-center gap-2"
              to={`/tools${toolsQuery({ tool: 'ping', target: device.host })}`}
            >
              <Radar className="h-4 w-4" /> Ping / Trace
            </Link>
          )}
          {canEdit && (
          <>
            <button className="btn-secondary flex items-center gap-2" disabled={busy || !device.enabled || refreshWatch.phase === 'running'} onClick={() => void startRefresh()}>
```

5. Replace:

```tsx
              <button className="btn-secondary flex items-center gap-2 text-red-400" onClick={() => setConfirmDelete(true)}>
                <Trash2 className="h-4 w-4" /> Delete
              </button>
            )}
          </div>
        )}
      </div>
```

with:

```tsx
              <button className="btn-secondary flex items-center gap-2 text-red-400" onClick={() => setConfirmDelete(true)}>
                <Trash2 className="h-4 w-4" /> Delete
              </button>
            )}
          </>
          )}
        </div>
      </div>
```

In `frontend/src/pages/MonitorDetail.tsx`:

1. At the end of the `lucide-react` import, replace:

```ts
  FileText,
} from 'lucide-react'
```

with:

```ts
  FileText,
  Radar,
} from 'lucide-react'
```

2. After `import { useAppConfig } from '@/context/AppConfigContext'`, add `import { useAuthContext } from '@/context/AuthContext'`.
3. After `import { monitorAccess } from '@/utils/monitorAccess'`, add `import { canUseNetTools, monitorHost, toolsQuery } from '@/utils/netTools'`.
4. Replace `  const { toasts, push } = useToasts()` (in `MonitorDetail`) with:

```ts
  const { toasts, push } = useToasts()
  const { currentUser } = useAuthContext()
```

5. Replace `  const isHttp = monitor.type === 'http'` with:

```ts
  const isHttp = monitor.type === 'http'
  // The host this monitor checks, for "Ping / Trace"; null for webhooks and
  // for users without network tools.
  const pingHost = canUseNetTools(currentUser) ? monitorHost(monitor) : null
```

6. Replace:

```tsx
          <button className="btn-secondary !py-1.5" onClick={() => void handleTest()}>
            <Play className="h-4 w-4" /> Test
          </button>
```

with:

```tsx
          <button className="btn-secondary !py-1.5" onClick={() => void handleTest()}>
            <Play className="h-4 w-4" /> Test
          </button>
          {pingHost && (
            <Link className="btn-secondary !py-1.5" to={`/tools${toolsQuery({ tool: 'ping', target: pingHost })}`}>
              <Radar className="h-4 w-4" /> Ping / Trace
            </Link>
          )}
```

- [ ] **Step 6: Run the gate**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output. The browser checks are the owner's; Task 19 lists them.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/settings/NetToolsSettings.tsx frontend/src/pages/Settings.tsx \
  frontend/src/pages/AdminUsers.tsx frontend/src/components/EditServerAgentModal.tsx \
  frontend/src/components/AddServerAgentModal.tsx frontend/src/pages/ServerDetail.tsx \
  frontend/src/pages/network/DeviceDetail.tsx frontend/src/pages/MonitorDetail.tsx
git commit -m "feat(tools): network tools settings, user grants, agent toggle and entry points

Settings gains a Network tools tab (allowlist with per-line errors,
server runs, retention); Users gains the grant checkbox; the agent form gains Allow network tools with the
host's own state; install commands can add ENABLE_TOOLS=true; server,
device and monitor pages link into the tools.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 19: Full suite, cross-compile and docs

This task runs every check over the whole branch, then records where S1 stands. It runs after the final whole-branch review and its fix wave, so the follow-ups doc can list what that review fixed and what it deferred. It writes no code unless a check finds a defect. A defect is fixed in the file that owns it, with a test that fails first, and committed as `fix(tools): …` before the docs commit.

**Files:**
- Modify: `docs/superpowers/STATUS.md`
- Create: `docs/superpowers/plans/2026-10-05-tools-s1-followups.md`

**Interfaces:**
- Consumes: every task's code; the rulings and contract changes in this plan; the rulings each task's report recorded while executing; the final review's findings.
- Produces: nothing that code consumes.

- [ ] **Step 1: Run the backend suite**

From `backend/` (through the project's Docker helper if Go is not installed locally):

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: `gofmt` and `vet` print nothing; every package reports `ok` or `[no test files]`, and none reports `FAIL`. The DB tests skip here.

Run: `go test -race ./internal/stream/... ./internal/nettools/... ./cmd/agent/...`
Expected: every package `ok`, with no `WARNING: DATA RACE`. The ICMP loopback tests may report `SKIP` where raw ICMP is not permitted. That is expected.

- [ ] **Step 2: Run the DB suite**

From `backend/`:

Run: `./scripts/test-db.sh`
Expected: every package `ok`, no `FAIL`. This includes the S1 DB tests (`TestDB…` in `internal/services`, `internal/toolruns` and `internal/api`) and every earlier phase's tests, which must be unchanged by S1.

- [ ] **Step 3: Cross-compile the agent and run the Windows vet**

From `backend/`:

```bash
GOOS=windows go vet ./internal/nettools/... ./cmd/agent/...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/agent
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/agent
go build -o /dev/null ./cmd/sentinel
```

Expected: no output from any of them. These are the three targets the backend `Dockerfile` builds for agents (linux amd64 and arm64, windows amd64), plus the server.

- [ ] **Step 4: Check the agent and backend packages stay apart**

From `backend/`:

Run: `go list -deps ./internal/nettools/ | grep 'Stevy2191/Sentinel' ; go list -deps ./cmd/agent/ | grep -E 'gorm|gin-gonic|internal/(models|services|toolruns|api)'`
Expected: the first command prints only `github.com/Stevy2191/Sentinel/backend/internal/nettools`. The second prints nothing: the agent binary pulls in no database, HTTP framework or server package (Global Constraints).

- [ ] **Step 5: Run the frontend gate**

From the repo root:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD/frontend":/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"
```

Expected: exits 0 with no output.

Run: `grep -rn 'dark:\|#[0-9a-fA-F]\{6\}\|window.confirm\|window.alert' frontend/src/components/netTools frontend/src/components/settings frontend/src/pages/NetworkTools.tsx frontend/src/pages/ToolRunDetail.tsx frontend/src/utils/netTools.ts frontend/src/utils/netToolViews.ts frontend/src/hooks/useNetTools.ts frontend/src/hooks/useRunStream.ts`
Expected: no output. The new screens take their colours from `utils/colors.ts` and confirm nothing through the browser.

- [ ] **Step 6: Update `STATUS.md`**

In `docs/superpowers/STATUS.md`:

1. Replace the date in `Updated 2026-10-05.` with the date this step runs (`YYYY-MM-DD`).
2. Replace `(agreed 2026-10-04; nothing designed yet)` with `(agreed 2026-10-04; S1 designed in `specs/2026-10-05-tools-s1-network-tools-design.md` and built)`.
3. Replace the S1 row with:

```markdown
| S1 | Network tools: ping, traceroute (MTR-style), DNS lookup, port check/quick scan — from Sentinel or any server agent, streamed live | Done (`feature/tools-s1`, awaiting merge to `dev`); follow-ups in `plans/2026-10-05-tools-s1-followups.md` |
```

4. In the S2, S5 and S6 rows, replace the final `| Not started |` with `| Not started; unblocked by S1 |`.
5. Directly above the line that starts `Phase 5 (after updating the work install from`, add:

```markdown
Tools S1 (after updating the work install from `dev`; Claude had no browser, so none of these has been run in one):
1. With the allowlist empty, Network Tools shows "No targets are allowed yet. An admin adds subnets and hosts in Settings → Network tools", and a ping is refused with "… is not on the network tools allowlist".
2. In Settings → Network tools, add the office subnet (e.g. a 10.x /16). A line such as 10.0.0.0/7 is named with "too broad: use /8 or narrower" and nothing is saved.
3. From the Sentinel server: ping a host on the list (replies arrive about one a second and the chart and tiles fill in), traceroute it, look up a name through the system resolver and through a named DNS server on the list, and check ports with "common".
4. Enable a Linux agent: re-run its install with "Enable network tools on this server" ticked (or add `ENABLE_TOOLS=true` to `/etc/sentinel/agent.conf` and restart it), then Edit → Allow network tools. Within a minute it can be chosen under "Run from"; run each tool from it.
5. A Windows agent, if one is to hand: the same as 4, and its traceroute shows the hops.
6. Through Caddy, watch a 10-round traceroute: the table updates round by round, not all at the end.
7. Cancel a run mid-way, once from the Sentinel server and once from an agent: it shows Cancelled within a second, and the agent stops.
8. Stop an agent's service in the middle of a traceroute: by the deadline plus 15 s the run shows Timed out and the page no longer says Running.
9. Grant a non-admin "Network tools" on the Users page: they see Network Tools in the menu, can run tools and see everyone's runs, cannot open Settings → Network tools, and get no Cancel on someone else's running run.
10. An agent that has not been updated says "This agent's version can't run tools — update it" in its Edit form and is disabled under "Run from".
11. `GET /api/v1/audit-log?resource_type=tool_run` lists `tool_run_started` and `tool_run_finished` entries, and a `tool_run_refused` for an off-list target.

```

Keep the phase 5, 4 and 3 lists that the owner has not confirmed yet.

- [ ] **Step 7: Write the follow-ups doc**

Create `docs/superpowers/plans/2026-10-05-tools-s1-followups.md`. The text below is complete except for the three sections marked "from the execution". Fill those from the task reports and the final review, one line each tagged with its task (`(T9)`), or write `None.`

```markdown
# Tools S1: rulings and deferred follow-ups

This records the decisions taken while planning and executing `2026-10-05-tools-s1-network-tools.md`, the limitations S1 ships with, and the work deliberately left for later. Done on `feature/tools-s1`, awaiting merge to `dev`.

## Rulings

Spec amendments made while planning (the spec's last section has the reasons):

- Old agents are detected by a missing flag, not by version: `agents.tools_local` is nullable, and NULL means the agent never reported it. CI stamps branch builds with the branch name, so versions cannot be compared.
- `tool_runs.target_ip` is TEXT, not INET; `tool_runs.agent_ref` snapshots the readable agent id, which the API returns as `agent_id`.
- The agent toggle has its own audited endpoint, `PUT /agents/:agent_id/tools`.
- `GET /tools/vantages` also returns `allowlist_empty`, because grant holders cannot read the settings but need the banner.
- The Linux agent's config file is `/etc/sentinel/agent.conf`.

Settled while planning (the spec left these open):

1. Ping must fit its deadline: `Normalize` refuses count × interval + timeout over 115 s ("count × interval is too long: at most 115 seconds").
2. Traceroute rows stop at the destination. When it is not reached they run to the last TTL that ever answered plus one, so the table shows where the path stops without 30 rows of timeouts.
3. Traceroute rounds start at least 1 s apart and end when every probe has answered or timed out.
4. `round_done` carries the cumulative hop statistics, so the browser never re-implements the maths.
5. Jitter is the mean absolute difference between consecutive reply RTTs; stdev is the population standard deviation.
6. TCP scans emit a `start` event with the number of ports, for the progress bar.
7. DNS sends no EDNS0 and retries a truncated UDP answer over TCP; the OPT record never appears in the sections.
8. Cancel wins: cancelling marks the run cancelled at once, and a later finish (local or agent) only fills a NULL summary. An agent posting events for a cancelled run gets `200 {"cancel": true}`.
9. The heartbeat field is `tools_local`; absent means NULL.
10. The run's JSON `agent_id` is `agent_ref`; the UUID column is not serialised.
11. The per-user rate limit is the existing `RateLimiter` (30 a minute, burst 10), so its 429 carries a plain string error; the frontend treats any 429 as a limit.
12. `RequireNetTools` answers 403 `forbidden` in the coded shape and passes the database-fresh admin flag to the handlers.
13. The HTTP server's `BaseContext` is cancelled just before `Shutdown`, so open SSE streams and long-polls end at once.
14. Finishes written by the sweeper are audited with the actor "system".
15. The run lifecycle lives in `internal/toolruns`, which imports `services`; `services` never imports it.
16. `GET /tools/runs/:id` returns `{run, events}`, `GET /tools/runs` sets `X-Total-Count`, and `GET /tools/vantages` returns `{vantages, allowlist_empty}`.
17. Hop names are looked up once per distinct address, in the background, with a 1 s timeout; a failed lookup emits nothing.
18. The Tools page URL carries the form (`/tools?tool=…&target=…&from=sentinel|agent:<agent_id>&params=…`); the entry points and "Run again" use it.

Contract changes made while planning:

- (T15) The `Agent` and `ManagedUser` fields live in `hooks/useAgents.ts` and `hooks/useUserManagement.ts`, where those types are; `types/index.ts` is only a barrel.
- (T6) `GET /users` sends `net_tools` to admin callers, because the Users page reads each user's grant there.
- (T15) `toolsQuery` takes `tool` and `target` optionally, since the server page's entry point sets only the vantage.
- (T15) `types/netTools.ts` adds `HopProbe`, the `hop` event's data.
- (T17) The result panels' view models live in `utils/netToolViews.ts`, and `utils/colors.ts` gained `chartChrome` for the ping chart.
- (T16) The DNS record type and the port list sit in the form's main row; only the numeric options are in the collapsed Options section. Run stays enabled with an empty allowlist, because a lookup through the vantage's own resolver needs no entry.

Then one line per item of the plan's "Contract changes made while writing the tasks" section (Shared contract) not already listed above, then one line per ruling the task reports recorded during execution.

## Fixed in the final whole-branch review

From the execution: one line per fix.

## Known limitations

- IPv4 only: a target that resolves only to IPv6 is refused (`ipv6_unsupported`). DNS lookups still return AAAA records.
- The limits are fixed in code: 30 runs a minute per user (burst 10), 20 a minute per target address, and at most 3 active per user, 3 per target, 10 overall and 2 per agent.
- The per-target rate buckets and the agents' last-poll times live in memory. A restart resets them, and agents show as offline until their next poll, within 25 s.
- Rate-limit refusals (429) are not audited, by design, so they cannot flood the log.
- Traceroute needs raw ICMP (root or NET_RAW, or the Windows ICMP API). The unprivileged ICMP socket serves ping only. Hosts or firewalls that drop ICMP Time Exceeded show timeouts past the first hop, which is the honest result.
- DNS has no EDNS0 and no DNSSEC. The system resolver is the first IPv4 nameserver only: no search domains and no fallback to a second server.
- The port check is a TCP connect only. "Filtered" covers both no answer and host unreachable.
- A host-name allowlist entry is matched against the typed name, and the probe goes to the address resolved when the run was created. A later DNS change does not move a run.
- The Tools page follows only the run started from it. Switching tool closes the stream, but the run carries on and appears in the history.
- Every grant holder sees every user's runs (a team troubleshooting tool, as the spec decided).

## Deferred follow-ups

Out of scope for S1 (spec):

- IPv6 targets
- subnet sweeps
- banner and version detection, and nmap (S2)
- an audit-log screen
- tunable limits
- UDP probes and UDP traceroute
- scheduled or recurring tool runs
- alerts raised from tool runs
- moving the existing monitors onto `nettools`

Found while planning:

- Network phase 6 (live site maps) should reuse `internal/stream` (the SSE writer and hub) rather than build a second push channel.

From the execution: one line per finding the final review deliberately left for later, tagged with its task.
```

Replace each "From the execution: …" paragraph with its lines before committing. Where a section has none, write `None.` The finished file has no "From the execution" text left.

- [ ] **Step 8: Commit**

```bash
git add docs/superpowers/STATUS.md docs/superpowers/plans/2026-10-05-tools-s1-followups.md
git commit -m "docs(tools): record S1 status, rulings and follow-ups

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 9: Hand over**

Report the output of Steps 1-5, and the owner checks added to `STATUS.md`. Merging `feature/tools-s1` into `dev`, pushing, and updating the sandbox or work install are the owner's call (CLAUDE.md: push and merge only when asked). Use the finishing-a-development-branch skill to present the options. The sandbox checks in the spec's Testing section are the owner checks above: real ping and traceroute from the backend container and from a Linux agent, and SSE events arriving one by one through Caddy.

