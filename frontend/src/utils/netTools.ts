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
  agent_too_old: "This agent's version can't run tools — update it",
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
