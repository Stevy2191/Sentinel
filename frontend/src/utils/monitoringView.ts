// The Monitoring page's model: the filters as they live in the address, the
// one status scale monitors, servers and devices share, and what each section
// shows. Only type imports from `@/` and relative value imports, so it can be
// checked on its own.
import type { Monitor } from '@/types'
import type { Agent } from '@/hooks/useAgents'
import type { Device, DeviceType } from '@/hooks/useDevices'
import { deviceType, vendorModel } from './devices'

export type Section = 'uptime' | 'servers' | 'devices'
export const SECTIONS: readonly Section[] = ['uptime', 'servers', 'devices']
export const SECTION_LABEL: Record<Section, string> = { uptime: 'Uptime checks', servers: 'Servers', devices: 'Devices' }
/** The type chips' labels. */
export const SECTION_CHIP: Record<Section, string> = { uptime: 'Uptime', servers: 'Servers', devices: 'Devices' }

export type ViewStatus = 'down' | 'up' | 'pending' | 'paused' | 'maintenance' | 'error'
export const VIEW_STATUS_LABEL: Record<ViewStatus, string> = {
  down: 'Down',
  up: 'Up',
  pending: 'Pending',
  paused: 'Paused',
  maintenance: 'Maintenance',
  error: 'Error',
}
const VIEW_STATUSES = Object.keys(VIEW_STATUS_LABEL) as ViewStatus[]
const MONITOR_TYPES = ['http', 'dns', 'ping', 'tcp', 'webhook'] as const
const DEVICE_TYPES: readonly DeviceType[] = ['switch', 'router', 'access_point', 'nvr', 'ups', 'other']

export interface MonitoringFilters {
  /** One section, or null for all of them (the type chips). */
  show: Section | null
  q: string
  status: ViewStatus | null
  /** A site id, 'none' for "no site", or null for every site. */
  site: string | null
  /** A monitor group id, 'ungrouped', or null. Uptime checks only. */
  group: string | null
  /** A monitor check type. Uptime checks only. */
  type: string | null
  /** Any of these tags. Uptime checks only. */
  tags: string[]
  /** Devices only. */
  deviceType: DeviceType | null
}

export const NO_FILTERS: MonitoringFilters = {
  show: null,
  q: '',
  status: null,
  site: null,
  group: null,
  type: null,
  tags: [],
  deviceType: null,
}

function oneOf<T extends string>(value: string | null, allowed: readonly T[]): T | null {
  return value !== null && (allowed as readonly string[]).includes(value) ? (value as T) : null
}

/** parseMonitoringParams reads the filters from the address. An unknown value
 *  is ignored, as if the parameter were absent. */
export function parseMonitoringParams(params: URLSearchParams): MonitoringFilters {
  const id = (key: string) => params.get(key)?.trim() || null
  return {
    show: oneOf(params.get('show'), SECTIONS),
    q: params.get('q') ?? '',
    status: oneOf(params.get('status'), VIEW_STATUSES),
    site: id('site'),
    group: id('group'),
    type: oneOf(params.get('type'), MONITOR_TYPES),
    tags: (params.get('tags') ?? '').split(',').map((t) => t.trim()).filter(Boolean),
    deviceType: oneOf(params.get('device_type'), DEVICE_TYPES),
  }
}

/** buildMonitoringParams writes the filters back, leaving defaults out. */
export function buildMonitoringParams(f: MonitoringFilters): URLSearchParams {
  const p = new URLSearchParams()
  if (f.show) p.set('show', f.show)
  if (f.q) p.set('q', f.q)
  if (f.status) p.set('status', f.status)
  if (f.site) p.set('site', f.site)
  if (f.group) p.set('group', f.group)
  if (f.type) p.set('type', f.type)
  if (f.tags.length) p.set('tags', f.tags.join(','))
  if (f.deviceType) p.set('device_type', f.deviceType)
  return p
}

/** filtersNarrow is whether anything besides the type chips narrows the rows. */
export function filtersNarrow(f: MonitoringFilters): boolean {
  return (
    f.q.trim() !== '' ||
    f.status !== null ||
    f.site !== null ||
    f.group !== null ||
    f.type !== null ||
    f.tags.length > 0 ||
    f.deviceType !== null
  )
}

/** visibleSections is which sections the chips allow, in page order. Servers
 *  and devices have no groups, so a group filter leaves uptime checks only. */
export function visibleSections(f: MonitoringFilters): Section[] {
  const shown = f.show ? [f.show] : [...SECTIONS]
  return f.group ? shown.filter((s) => s === 'uptime') : shown
}

/** ownFiltersSet is whether one of the section's own filters (the ones in its
 *  header, not the shared toolbar) is set. */
export function ownFiltersSet(section: Section, f: MonitoringFilters): boolean {
  if (section === 'uptime') return f.type !== null || f.tags.length > 0
  if (section === 'devices') return f.deviceType !== null
  return false
}

/** sectionVisible decides whether an allowed section is drawn: always while
 *  loading or failed, when it has rows, when its own filter hides every row
 *  (so that filter stays reachable), and when it is genuinely empty (no
 *  filter narrows it) and this user could add the first one. */
export function sectionVisible(s: {
  loading: boolean
  error: string | null
  total: number
  shown: number
  narrowed: boolean
  canAdd: boolean
  /** One of the section's own filters is set (see ownFiltersSet). */
  ownFilter: boolean
}): boolean {
  if (s.loading || s.error) return true
  if (s.shown > 0) return true
  if (s.ownFilter && s.total > 0) return true
  return !s.narrowed && s.total === 0 && s.canAdd
}

/** monitorState places a monitor on the shared scale. Paused and maintenance
 *  win over the last check, as the Uptime page has always shown them. */
export function monitorState(m: Pick<Monitor, 'enabled' | 'is_in_maintenance' | 'current_status'>): ViewStatus {
  if (!m.enabled) return 'paused'
  if (m.is_in_maintenance) return 'maintenance'
  if (m.current_status === 'online') return 'up'
  if (m.current_status === 'offline') return 'down'
  return 'pending'
}

export function serverState(a: Pick<Agent, 'status'>): ViewStatus {
  if (a.status === 'active') return 'up'
  if (a.status === 'offline') return 'down'
  return 'pending'
}

export function deviceState(d: Pick<Device, 'status'>): ViewStatus {
  return d.status
}

function siteMatches(filter: string | null, siteId: string | null | undefined): boolean {
  if (filter === null) return true
  if (filter === 'none') return !siteId
  return siteId === filter
}

function textMatches(q: string, values: (string | null | undefined)[]): boolean {
  const needle = q.trim().toLowerCase()
  return !needle || values.some((v) => v?.toLowerCase().includes(needle))
}

export function filterMonitors(monitors: Monitor[], f: MonitoringFilters): Monitor[] {
  return monitors.filter(
    (m) =>
      textMatches(f.q, [m.name, m.url]) &&
      (f.status === null || monitorState(m) === f.status) &&
      siteMatches(f.site, m.site_id) &&
      (f.group === null || (f.group === 'ungrouped' ? !m.group_id : m.group_id === f.group)) &&
      (f.type === null || m.type === f.type) &&
      (f.tags.length === 0 || (m.tags ?? []).some((t) => f.tags.includes(t)))
  )
}

export function filterServers(agents: Agent[], f: MonitoringFilters): Agent[] {
  return agents.filter(
    (a) =>
      textMatches(f.q, [a.name, a.hostname, a.agent_id, a.ip_address]) &&
      (f.status === null || serverState(a) === f.status) &&
      siteMatches(f.site, a.site_id)
  )
}

export function filterDevicesView(devices: Device[], f: MonitoringFilters): Device[] {
  return devices.filter(
    (d) =>
      textMatches(f.q, [d.name, d.host, vendorModel(d), d.site_name]) &&
      (f.status === null || deviceState(d) === f.status) &&
      siteMatches(f.site, d.site_id) &&
      (f.deviceType === null || deviceType(d) === f.deviceType)
  )
}

export interface SummaryCounts {
  watching: number
  down: number
  paused: number
}

/** summaryCounts is the strip under the toolbar, over everything loaded.
 *  Error and Pending are not Down; servers cannot be paused. */
export function summaryCounts(monitors: Monitor[], agents: Agent[], devices: Device[]): SummaryCounts {
  let down = 0
  let paused = 0
  for (const m of monitors) {
    const s = monitorState(m)
    if (s === 'down') down++
    else if (s === 'paused') paused++
  }
  for (const a of agents) if (serverState(a) === 'down') down++
  for (const d of devices) {
    const s = deviceState(d)
    if (s === 'down') down++
    else if (s === 'paused') paused++
  }
  return { watching: monitors.length + agents.length + devices.length, down, paused }
}

/** statusOptions is the status select: the four everyday states, plus
 *  Maintenance and Error while something is in them (or one is selected). */
export function statusOptions(monitors: Monitor[], devices: Device[], selected: ViewStatus | null): ViewStatus[] {
  const out: ViewStatus[] = ['down', 'up', 'pending', 'paused']
  if (selected === 'maintenance' || monitors.some((m) => monitorState(m) === 'maintenance')) out.push('maintenance')
  if (selected === 'error' || devices.some((d) => d.status === 'error')) out.push('error')
  return out
}

/** siteFilterOptions is the site select: the user's sites plus any other site
 *  named on a row they can see, A-Z. */
export function siteFilterOptions(
  sites: { id: string; name: string }[],
  rows: { site_id?: string | null; site_name?: string | null }[]
): { id: string; name: string }[] {
  const byId = new Map(sites.map((s) => [s.id, s.name]))
  for (const r of rows) {
    if (r.site_id && !byId.has(r.site_id)) byId.set(r.site_id, r.site_name ?? 'Unknown site')
  }
  return [...byId].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))
}

/** unknownOption is the extra select option for a selected value no loaded row
 *  offers (a deleted group or site, a bookmarked type), so the select shows
 *  what is applied and choosing "All" is a real change. known includes the
 *  fixed values such as "none". Null label means the value upper-cased. */
export function unknownOption(
  selected: string | null,
  known: string[],
  label: string | null
): { value: string; label: string } | null {
  if (!selected || known.includes(selected)) return null
  return { value: selected, label: label ?? selected.toUpperCase() }
}

export const MONITOR_SORTS = [
  { key: 'down-first', label: 'Down first' },
  { key: 'name', label: 'Name (A–Z)' },
  { key: 'uptime', label: 'Lowest uptime' },
  { key: 'slowest', label: 'Slowest first' },
] as const
export type MonitorSortKey = (typeof MONITOR_SORTS)[number]['key']

// Sort weight for "Down first": states that need attention float up, and
// paused monitors sink below healthy ones - dormant is not urgent.
function urgency(m: Monitor): number {
  const s = monitorState(m)
  if (s === 'paused') return 4
  if (s === 'down') return 0
  if (s === 'maintenance') return 1
  if (s === 'up') return 3
  return 2
}

/** sortMonitors returns a sorted copy, ties broken by name. */
export function sortMonitors(list: Monitor[], key: MonitorSortKey, uptimeById: Map<string, number>): Monitor[] {
  const byName = (a: Monitor, b: Monitor) => a.name.localeCompare(b.name)
  return [...list].sort((a, b) => {
    switch (key) {
      case 'name':
        return byName(a, b)
      case 'uptime':
        return (uptimeById.get(a.id) ?? 100) - (uptimeById.get(b.id) ?? 100) || byName(a, b)
      case 'slowest':
        return b.last_response_time_ms - a.last_response_time_ms || byName(a, b)
      default:
        return urgency(a) - urgency(b) || byName(a, b)
    }
  })
}

/** monitoringPath is the Monitoring page, optionally opened on one section. */
export function monitoringPath(show?: Section): string {
  return show ? `/monitoring?show=${show}` : '/monitoring'
}

/** legacyMonitoringTarget is where an old list address goes: Monitoring on
 *  `show`, keeping the old address's query (so /uptime?type=http keeps its
 *  filter) and hash. */
export function legacyMonitoringTarget(location: { search: string; hash: string }, show: Section): string {
  const old = new URLSearchParams(location.search)
  const next = new URLSearchParams({ show })
  old.forEach((value, key) => {
    if (key !== 'show') next.append(key, value)
  })
  return `/monitoring?${next.toString()}${location.hash}`
}
