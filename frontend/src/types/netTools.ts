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
  /** Why a probe failed, when it did for a reason other than a timeout. */
  error?: string
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
