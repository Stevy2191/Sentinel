// View models for the run result panels, built from a run's events. Pure, so
// the live Tools page, the run detail page and the throwaway checks agree.
//
// Event data arrives as unknown JSON. Each view model reads only events whose
// data is an object carrying the fields it uses, with the right types, and
// skips the rest: a null or malformed event must never crash a panel.

import { colors } from '@/utils/colors'
import { formatDuration } from '@/utils/formatters'
import { NUMBER_FIELDS } from '@/utils/netTools'
import type {
  DNSAnswer,
  DNSRecord,
  HopStats,
  NetTool,
  PingSummary,
  PortResult,
  ToolParams,
  ToolRun,
  ToolRunEvent,
  TraceSummary,
} from '@/types/netTools'

type Fields = Record<string, unknown>

/** data as an object, or null when it is not one (null, a string, a list…). */
function fields(data: unknown): Fields | null {
  return typeof data === 'object' && data !== null && !Array.isArray(data) ? (data as Fields) : null
}

const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v)
const isStr = (v: unknown): v is string => typeof v === 'string'

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
    const d = fields(e.data)
    if (!d || !isNum(d.seq)) continue
    if (e.type === 'reply') {
      if (!isNum(d.rtt_ms) || !isNum(d.ttl) || !isStr(d.from)) continue
      rows.push({ seq: d.seq, kind: 'reply', rtt_ms: d.rtt_ms, ttl: d.ttl, from: d.from, message: null })
    } else if (e.type === 'timeout') {
      rows.push({ seq: d.seq, kind: 'timeout', rtt_ms: null, ttl: null, from: null, message: null })
    } else if (e.type === 'error') {
      if (!isStr(d.message)) continue
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
  let latest: { round: number; hops: unknown[] } | null = null
  let reached = false
  const names = new Map<string, string>()
  for (const e of events) {
    const d = fields(e.data)
    if (!d) continue
    if (e.type === 'round_done') {
      if (!isNum(d.round) || !Array.isArray(d.hops)) continue
      if (!latest || d.round > latest.round) latest = { round: d.round, hops: d.hops }
    } else if (e.type === 'hop') {
      if (d.reached === true) reached = true
    } else if (e.type === 'hop_name') {
      if (isStr(d.addr) && isStr(d.name)) names.set(d.addr, d.name)
    }
  }
  const base: unknown[] = (summary ? summary.hops : latest?.hops) ?? []
  const hops: HopStats[] = []
  for (const raw of base) {
    // A row needs its TTL and counts; addrs that is not a list reads as
    // none, and a name that is not a string as no name.
    const h = fields(raw)
    if (!h || !isNum(h.ttl) || !isNum(h.sent) || !isNum(h.received) || !isNum(h.loss_pct)) continue
    const addrs = Array.isArray(h.addrs) ? h.addrs.filter(isStr) : []
    const name = (isStr(h.name) && h.name) || addrs.map((a) => names.get(a)).find((n) => !!n)
    hops.push({ ...(h as unknown as HopStats), addrs, name })
  }
  return { round: latest?.round ?? 0, hops, reached: summary ? summary.reached : reached }
}

// ---- dns

/** A record section as a list of well-formed records: anything else reads
 *  as empty, and a malformed record is dropped. */
function dnsRecords(v: unknown): DNSRecord[] {
  if (!Array.isArray(v)) return []
  return v.filter((x): x is DNSRecord => {
    const r = fields(x)
    return !!r && isStr(r.name) && isStr(r.type) && isNum(r.ttl) && isStr(r.data)
  })
}

/** The first well-formed answer event: who answered, and its sections. */
export function dnsView(events: ToolRunEvent[]): DNSAnswer | null {
  for (const e of events) {
    if (e.type !== 'answer') continue
    const d = fields(e.data)
    if (!d || !isStr(d.server) || !isStr(d.rcode)) continue
    return {
      ...(d as unknown as DNSAnswer),
      answer: dnsRecords(d.answer),
      authority: dnsRecords(d.authority),
      additional: dnsRecords(d.additional),
    }
  }
  return null
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
    const d = fields(e.data)
    if (!d) continue
    if (e.type === 'start') {
      if (isNum(d.total)) total = d.total
    } else if (e.type === 'port') {
      if (!isNum(d.port) || !isStr(d.state) || (d.service !== undefined && !isStr(d.service))) continue
      const p = d as unknown as PortResult
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
