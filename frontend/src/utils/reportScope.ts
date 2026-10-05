import { describePeriod } from '@/components/PeriodSelector'
import type { PortRole } from '@/hooks/useDevices'
import {
  MAX_REPORT_METRICS,
  MAX_REPORT_SUBJECTS,
  type MetricChoice,
  type MetricSource,
  type MonitorScopeType,
  type NetworkScopeType,
  type ReportPeriod,
  type ReportScopeData,
  type ReportScopeType,
  type ScopePreview,
} from '@/types/reports'

/** Something picked for a network scope, kept with its name so the builders
 *  can describe the scope without fetching names again. */
export interface ScopeChoice {
  id: string
  name: string
}

/**
 * The network scope being edited. Each tab keeps its own picks and only the
 * active tab's are sent; the Port roles and Sites tabs share the sites.
 */
export interface NetworkScopeDraft {
  scopeType: NetworkScopeType
  /** Named "device · port (alias)". */
  ports: ScopeChoice[]
  sites: ScopeChoice[]
  roles: PortRole[]
  devices: ScopeChoice[]
}

export const EMPTY_NETWORK_SCOPE: NetworkScopeDraft = {
  scopeType: 'ports',
  ports: [],
  sites: [],
  roles: [],
  devices: [],
}

export const NETWORK_SCOPE_TABS: { value: NetworkScopeType; label: string }[] = [
  { value: 'ports', label: 'Ports' },
  { value: 'port_roles', label: 'Port roles' },
  { value: 'devices', label: 'Devices' },
  { value: 'sites', label: 'Sites' },
]

/** Port roles in the order they are offered and named. */
export const REPORT_ROLES: { value: PortRole; label: string }[] = [
  { value: 'wan', label: 'WAN' },
  { value: 'uplink', label: 'Uplink' },
  { value: 'access', label: 'Access' },
]

export const METRIC_SOURCE_LABEL: Record<MetricSource, string> = {
  builtin: 'built-in',
  profile: 'profile',
  custom: 'custom',
}

/**
 * The monitor check types an Uptime or Incident report may be scoped to, in
 * the order the builders show them. Webhook is absent: it receives rather than
 * checks, so it has no incidents.
 */
export const REPORTABLE_MONITOR_TYPES: readonly string[] = ['http', 'dns', 'ping', 'tcp']

function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? '' : 's'}`
}

/** scope_data for a monitor report: the one field its scope type uses. */
export function monitorScopeData(scopeType: MonitorScopeType, ids: string[]): ReportScopeData {
  switch (scopeType) {
    case 'monitors':
      return { monitor_ids: ids }
    case 'tags':
      return { tags: ids }
    case 'groups':
      return { group_ids: ids }
    case 'types':
      return { types: ids }
  }
}

/** scope_data for the draft's active tab, without the metrics. */
export function networkScopeData(d: NetworkScopeDraft): ReportScopeData {
  switch (d.scopeType) {
    case 'ports':
      return { port_ids: d.ports.map((p) => p.id) }
    case 'port_roles':
      return {
        site_ids: d.sites.map((s) => s.id),
        roles: REPORT_ROLES.map((r) => r.value).filter((r) => d.roles.includes(r)),
      }
    case 'devices':
      return { device_ids: d.devices.map((x) => x.id) }
    case 'sites':
      return { site_ids: d.sites.map((s) => s.id) }
  }
}

/** Why the draft's active tab cannot be sent yet, or null. */
export function networkScopeError(d: NetworkScopeDraft): string | null {
  switch (d.scopeType) {
    case 'ports':
      if (d.ports.length === 0) return 'Choose at least one port'
      return d.ports.length > MAX_REPORT_SUBJECTS ? `Choose at most ${MAX_REPORT_SUBJECTS} ports` : null
    case 'port_roles':
      if (d.sites.length === 0) return 'Choose at least one site'
      return d.roles.length === 0 ? 'Choose at least one port role' : null
    case 'devices':
      if (d.devices.length === 0) return 'Choose at least one device'
      return d.devices.length > MAX_REPORT_SUBJECTS ? `Choose at most ${MAX_REPORT_SUBJECTS} devices` : null
    case 'sites':
      return d.sites.length === 0 ? 'Choose at least one site' : null
  }
}

/** Names to describe a scope with. An id missing here (not loaded, hidden
 *  from this user, or deleted) is counted, never shown. */
export interface ScopeNames {
  sites: Map<string, string>
  devices: Map<string, string>
}

export function scopeNames(sites: { id: string; name: string }[], devices: { id: string; name: string }[]): ScopeNames {
  return {
    sites: new Map(sites.map((s) => [s.id, s.name])),
    devices: new Map(devices.map((d) => [d.id, d.name])),
  }
}

/** "HQ, Annex", "HQ, Annex, Depot and 2 more", or "5 sites" when no name is known. */
function nameList(ids: string[] | undefined, names: Map<string, string>, noun: string, prefix: string): string {
  const all = ids ?? []
  const known = all.map((id) => names.get(id)).filter((n): n is string => !!n)
  if (known.length === 0) return plural(all.length, noun)
  const shown = known.slice(0, 3)
  const more = all.length - shown.length
  return `${prefix}${shown.join(', ')}${more > 0 ? ` and ${more} more` : ''}`
}

/**
 * A network scope in words: "WAN, Uplink ports at HQ, Annex · 4 metrics".
 * Monitor scopes come back as their scope type, which is how the report pages
 * have always shown them.
 */
export function describeScope(scopeType: ReportScopeType, scope: ReportScopeData | undefined, names: ScopeNames): string {
  const s = scope ?? {}
  let what: string
  switch (scopeType) {
    case 'ports':
      what = plural(s.port_ids?.length ?? 0, 'port')
      break
    case 'port_roles': {
      const roles = REPORT_ROLES.filter((r) => s.roles?.includes(r.value)).map((r) => r.label)
      what = `${roles.join(', ')} ports at ${nameList(s.site_ids, names.sites, 'site', '')}`
      break
    }
    case 'devices':
      what = nameList(s.device_ids, names.devices, 'device', 'Devices: ')
      break
    case 'sites':
      what = nameList(s.site_ids, names.sites, 'site', 'Sites: ')
      break
    default:
      return scopeType
  }
  const n = s.metrics?.length ?? 0
  return n > 0 ? `${what} · ${plural(n, 'metric')}` : what
}

/** The scope picker's size line, from the preview. */
export function previewSizeLine(scopeType: NetworkScopeType, p: ScopePreview): string {
  const size =
    scopeType === 'devices'
      ? plural(p.devices, 'device')
      : scopeType === 'sites'
        ? `${plural(p.ports, 'port')} on ${plural(p.devices, 'device')}`
        : plural(p.ports, 'port')
  return p.capped
    ? `Covers ${size} — the report will include the ${MAX_REPORT_SUBJECTS} busiest`
    : `Covers ${size} right now`
}

/** "core-sw1 · Gi1/0/1 (uplink to annex)": how the report names a port row. */
export function portChoiceName(
  deviceName: string,
  p: { name: string; label: string; number: number; alias: string },
): string {
  const port = p.name || p.label || String(p.number)
  return `${deviceName} · ${port}${p.alias && p.alias !== port ? ` (${p.alias})` : ''}`
}

const DAY_MS = 86_400_000

function longDate(d: Date, utc: boolean): string {
  return d.toLocaleDateString('en-US', {
    month: 'long',
    day: 'numeric',
    year: 'numeric',
    ...(utc ? { timeZone: 'UTC' } : {}),
  })
}

/**
 * The calendar unit `offset` units before the one containing now, named as the
 * report names its period (models.PeriodLabel): "Week of September 14, 2026",
 * "August 2026", "Q2 2026", "2025". Worked out in the browser's zone; the
 * report's zone can differ, which matters only in a unit's first hours.
 */
function calendarName(unit: NonNullable<ReportPeriod['period_unit']>, offset: number, now: Date): string {
  const y = now.getFullYear()
  const m = now.getMonth()
  switch (unit) {
    case 'week': {
      const sinceMonday = (now.getDay() + 6) % 7
      return `Week of ${longDate(new Date(y, m, now.getDate() - sinceMonday - 7 * offset), false)}`
    }
    case 'month':
      return new Date(y, m - offset, 1).toLocaleDateString('en-US', { month: 'long', year: 'numeric' })
    case 'quarter': {
      const start = new Date(y, Math.floor(m / 3) * 3 - 3 * offset, 1)
      return `Q${Math.floor(start.getMonth() / 3) + 1} ${start.getFullYear()}`
    }
    case 'year':
      return String(y - offset)
  }
}

/**
 * The period a Metrics report compares with, in words: the previous calendar
 * unit for a calendar period, otherwise the same length just before the start
 * (models.Report.PreviousPeriod). Empty while a custom range is incomplete.
 */
export function comparisonLabel(p: ReportPeriod, now: Date = new Date()): string {
  if (p.period_kind === 'calendar') {
    return `Compared with ${calendarName(p.period_unit ?? 'month', (p.period_offset ?? 0) + 1, now)}`
  }
  if (p.period_kind === 'custom') {
    if (!p.period_start || !p.period_end) return ''
    const start = Date.parse(p.period_start)
    // The selector stores whole days (00:00:00 to 23:59:59), so this rounds to
    // the number of days chosen.
    const days = Math.max(1, Math.round((Date.parse(p.period_end) - start) / DAY_MS))
    const first = longDate(new Date(start - days * DAY_MS), true)
    const last = longDate(new Date(start - DAY_MS), true)
    return first === last ? `Compared with ${first}` : `Compared with ${first} to ${last}`
  }
  // Rolling: describePeriod names it "Last 7 days" or "Last 24 hours".
  return `Compared with the ${describePeriod(p).replace(/^Last /, 'previous ')}`
}

/** The Metrics step's list, the choices it was made from, and what a scope
 *  change removed. */
export interface MetricsSelection {
  /** Metric keys in order; the first ranks the rows. */
  metrics: string[]
  /** True once the user changed the list: defaults are no longer re-applied. */
  edited: boolean
  /** Labels of metrics removed because the scope no longer offers them. */
  dropped: string[]
  /** The metrics the scope offers, from the latest preview. */
  choices: MetricChoice[]
}

export const EMPTY_METRICS_SELECTION: MetricsSelection = { metrics: [], edited: false, dropped: [], choices: [] }

/**
 * Applies a new preview to the metrics list (spec section 4). An unedited list
 * becomes the scope's defaults. An edited list keeps what the scope still
 * offers, in order, and names what it lost; if it lost everything, the
 * defaults come back and the list counts as unedited again.
 */
export function reapplyMetrics(cur: MetricsSelection, next: ScopePreview): MetricsSelection {
  const defaults = next.defaults.slice(0, MAX_REPORT_METRICS)
  if (!cur.edited) return { metrics: defaults, edited: false, dropped: [], choices: next.metrics }
  const offered = new Set(next.metrics.map((m) => m.key))
  const kept = cur.metrics.filter((k) => offered.has(k))
  const dropped = cur.metrics
    .filter((k) => !offered.has(k))
    .map((k) => cur.choices.find((c) => c.key === k)?.label ?? k)
  if (kept.length === 0) return { metrics: defaults, edited: false, dropped, choices: next.metrics }
  return { metrics: kept, edited: true, dropped, choices: next.metrics }
}

/** The list with the item at index moved one place up (-1) or down (+1). */
export function moveItem<T>(list: T[], index: number, delta: -1 | 1): T[] {
  const to = index + delta
  if (index < 0 || index >= list.length || to < 0 || to >= list.length) return list
  const out = [...list]
  ;[out[index], out[to]] = [out[to], out[index]]
  return out
}
