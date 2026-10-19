// The Monitoring page as one list: every uptime check, server and device as
// one row type, the free filters over them (as they live in the address), the
// counts each filter option shows, sorting, grouping and the filter pills.
// Only type imports from `@/` and relative value imports, so it can be
// checked on its own.
import type { Monitor, MonitorGroup, MonitorType } from '@/types'
import type { Agent, AgentMetric } from '@/hooks/useAgents'
import type { Device, DeviceType } from '@/hooks/useDevices'
import { deviceType, vendorModel } from './devices'
import { VIEW_STATUS_LABEL, deviceState, monitorState, serverState, type Section, type ViewStatus } from './monitoringView'

export type KindParent = Section
export type KindValue = KindParent | MonitorType | DeviceType

export const CHECK_LEAVES: readonly MonitorType[] = ['http', 'dns', 'ping', 'tcp', 'webhook']
export const DEVICE_LEAVES: readonly DeviceType[] = ['switch', 'router', 'access_point', 'nvr', 'ups', 'other']
const PARENTS: readonly KindParent[] = ['uptime', 'servers', 'devices']
export const KIND_PARENT_LABEL: Record<KindParent, string> = { uptime: 'Uptime checks', servers: 'Servers', devices: 'Devices' }
const CHECK_LABEL: Record<MonitorType, string> = { http: 'HTTP', dns: 'DNS', ping: 'Ping', tcp: 'TCP', webhook: 'Webhook' }
// The same labels as DEVICE_TYPE_LABEL in hooks/useDevices, which cannot be
// imported here as a value.
const DEVICE_LABEL: Record<DeviceType, string> = {
  switch: 'Switch',
  router: 'Router',
  access_point: 'Access point',
  nvr: 'NVR',
  ups: 'UPS',
  other: 'Other',
}
const KIND_VALUES: readonly string[] = [...PARENTS, ...CHECK_LEAVES, ...DEVICE_LEAVES]
const VIEW_STATUSES = Object.keys(VIEW_STATUS_LABEL) as ViewStatus[]

const isCheckLeaf = (v: string): v is MonitorType => (CHECK_LEAVES as readonly string[]).includes(v)
const isDeviceLeaf = (v: string): v is DeviceType => (DEVICE_LEAVES as readonly string[]).includes(v)

/** kindValueLabel is one Kind filter value as the menu names it. */
export function kindValueLabel(v: KindValue): string {
  if ((PARENTS as readonly string[]).includes(v)) return KIND_PARENT_LABEL[v as KindParent]
  if (isCheckLeaf(v)) return CHECK_LABEL[v]
  return DEVICE_LABEL[v as DeviceType]
}

interface RowBase {
  /** Unique across kinds: 'm:', 's:' or 'd:' and the id. */
  key: string
  name: string
  /** The muted second line: address, hostname and OS, or host and model. */
  line: string
  siteId: string | null
  siteName: string | null
  status: ViewStatus
  /** Last check, last report or last seen. */
  seenAt: string | null
}

export type ListRow =
  | (RowBase & { kind: 'uptime'; leaf: string; groupId: string | null; tags: string[]; monitor: Monitor })
  | (RowBase & { kind: 'servers'; leaf: null; groupId: null; tags: []; agent: Agent })
  | (RowBase & { kind: 'devices'; leaf: DeviceType; groupId: null; tags: []; device: Device })

/** toRows turns the three loaded lists into one, checks then servers then
 *  devices. knownGroups, once the groups have loaded, files a check whose
 *  group this user cannot see (or that is gone) as ungrouped. */
export function toRows(monitors: Monitor[], agents: Agent[], devices: Device[], knownGroups?: Set<string>): ListRow[] {
  const out: ListRow[] = []
  for (const m of monitors) {
    out.push({
      kind: 'uptime',
      key: 'm:' + m.id,
      name: m.name,
      line: m.url,
      siteId: m.site_id || null,
      siteName: m.site_name ?? null,
      status: monitorState(m),
      seenAt: m.last_check_at,
      leaf: m.type,
      groupId: m.group_id && (!knownGroups || knownGroups.has(m.group_id)) ? m.group_id : null,
      tags: m.tags ?? [],
      monitor: m,
    })
  }
  for (const a of agents) {
    out.push({
      kind: 'servers',
      key: 's:' + a.id,
      name: a.name,
      line: (a.hostname ?? a.agent_id) + (a.os_version ? ` · ${a.os_version}` : ''),
      siteId: a.site_id || null,
      siteName: a.site_name ?? null,
      status: serverState(a),
      seenAt: a.last_heartbeat,
      leaf: null,
      groupId: null,
      tags: [],
      agent: a,
    })
  }
  for (const d of devices) {
    const vm = vendorModel(d)
    out.push({
      kind: 'devices',
      key: 'd:' + d.id,
      name: d.name,
      line: d.host + (d.port !== 161 ? `:${d.port}` : '') + (vm ? ` · ${vm}` : ''),
      siteId: d.site_id || null,
      siteName: d.site_name ?? null,
      status: deviceState(d),
      seenAt: d.last_seen_at,
      leaf: deviceType(d),
      groupId: null,
      tags: [],
      device: d,
    })
  }
  return out
}

/** kindLabel is the Kind column: "HTTP check", "Server", "Switch". A check
 *  type this page does not know yet still gets a label. */
export function kindLabel(r: ListRow): string {
  if (r.kind === 'servers') return 'Server'
  if (r.kind === 'devices') return DEVICE_LABEL[r.leaf]
  return (isCheckLeaf(r.leaf) ? CHECK_LABEL[r.leaf] : r.leaf.toUpperCase()) + ' check'
}

export interface ListFilters {
  q: string
  /** Parents ('uptime') and leaves ('http', 'switch'); any of them matches. */
  kinds: KindValue[]
  statuses: ViewStatus[]
  /** Site ids, or 'none' for rows with no site. */
  sites: string[]
  /** A monitor group id or 'ungrouped'. Uptime checks only. */
  group: string | null
  /** Any of these tags. Uptime checks only. */
  tags: string[]
}

export const NO_FILTERS: ListFilters = { q: '', kinds: [], statuses: [], sites: [], group: null, tags: [] }

export type SortKey = 'status' | 'name' | 'address' | 'kind' | 'site' | 'health' | 'uptime' | 'seen'
const SORT_KEYS: readonly SortKey[] = ['status', 'name', 'address', 'kind', 'site', 'health', 'uptime', 'seen']
export interface ListSort {
  key: SortKey
  desc: boolean
}
/** Down first: status by urgency, then name. */
export const DEFAULT_SORT: ListSort = { key: 'status', desc: false }

export type GroupBy = 'none' | 'site' | 'kind' | 'group'
const GROUP_BYS: readonly GroupBy[] = ['none', 'site', 'kind', 'group']

export interface ListView {
  filters: ListFilters
  sort: ListSort
  /** Null when the address does not say, so the remembered choice applies. */
  by: GroupBy | null
}

const list = (v: string | null) =>
  (v ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)

function replaceParent(kinds: KindValue[], parent: KindParent, leaf: KindValue): KindValue[] {
  return [...kinds.filter((k) => k !== parent && k !== leaf), leaf]
}

/** parseListParams reads the view from the address. Unknown values are
 *  dropped. Old addresses (show=, type=, device_type=) are read when kind= is
 *  absent, a check or device type standing in for its whole kind. */
export function parseListParams(p: URLSearchParams): ListView {
  let kinds: KindValue[]
  if (p.has('kind')) {
    kinds = list(p.get('kind')).filter((k): k is KindValue => KIND_VALUES.includes(k))
  } else {
    const show = p.get('show')
    kinds = show && (PARENTS as readonly string[]).includes(show) ? [show as KindParent] : []
    const type = p.get('type')?.trim() ?? ''
    if (isCheckLeaf(type)) kinds = replaceParent(kinds, 'uptime', type)
    const dt = p.get('device_type')?.trim() ?? ''
    if (isDeviceLeaf(dt)) kinds = replaceParent(kinds, 'devices', dt)
  }
  const rawSort = p.get('sort') ?? ''
  const desc = rawSort.startsWith('-')
  const key = (desc ? rawSort.slice(1) : rawSort) as SortKey
  const by = p.get('by') as GroupBy | null
  return {
    filters: {
      q: p.get('q') ?? '',
      kinds: [...new Set(kinds)],
      statuses: list(p.get('status')).filter((s): s is ViewStatus => (VIEW_STATUSES as string[]).includes(s)),
      sites: list(p.get('site')),
      group: p.get('group')?.trim() || null,
      tags: list(p.get('tags')),
    },
    sort: SORT_KEYS.includes(key) ? { key, desc } : DEFAULT_SORT,
    by: by && GROUP_BYS.includes(by) ? by : null,
  }
}

/** buildListParams writes the view back, leaving defaults out. */
export function buildListParams(v: ListView): URLSearchParams {
  const p = new URLSearchParams()
  const f = v.filters
  if (f.q) p.set('q', f.q)
  if (f.kinds.length) p.set('kind', f.kinds.join(','))
  if (f.statuses.length) p.set('status', f.statuses.join(','))
  if (f.sites.length) p.set('site', f.sites.join(','))
  if (f.group) p.set('group', f.group)
  if (f.tags.length) p.set('tags', f.tags.join(','))
  if (v.sort.key !== DEFAULT_SORT.key || v.sort.desc !== DEFAULT_SORT.desc) p.set('sort', (v.sort.desc ? '-' : '') + v.sort.key)
  if (v.by && v.by !== 'none') p.set('by', v.by)
  return p
}

/** toggleKind is a tick or untick in the Kind menu. A type ticked while its
 *  whole kind is ticked narrows to that type (otherwise the tick would change
 *  nothing); a kind ticked takes the place of its types. */
export function toggleKind(kinds: KindValue[], v: KindValue): KindValue[] {
  if (kinds.includes(v)) return kinds.filter((k) => k !== v)
  const parent = leafParent(v)
  if (parent) return [...kinds.filter((k) => k !== parent), v]
  return [...kinds.filter((k) => leafParent(k) !== v), v]
}

export function filtersActive(f: ListFilters): boolean {
  return f.q.trim() !== '' || f.kinds.length > 0 || f.statuses.length > 0 || f.sites.length > 0 || f.group !== null || f.tags.length > 0
}

/** uptimeOnly is whether a filter that only uptime checks have is set, which
 *  leaves servers and devices out. */
export function uptimeOnly(f: ListFilters): boolean {
  return f.group !== null || f.tags.length > 0
}

export type Facet = 'kind' | 'status' | 'site' | 'group' | 'tags'

function kindMatches(kinds: KindValue[], r: ListRow): boolean {
  if (kinds.length === 0) return true
  return kinds.some((k) => k === r.kind || (r.leaf !== null && k === r.leaf && leafParent(k) === r.kind))
}

function leafParent(v: string): KindParent | null {
  if (isCheckLeaf(v)) return 'uptime'
  if (isDeviceLeaf(v)) return 'devices'
  return null
}

function textOf(r: ListRow): (string | null | undefined)[] {
  const base = [r.name, r.line, r.siteName, kindLabel(r)]
  return r.kind === 'servers' ? [...base, r.agent.agent_id, r.agent.ip_address] : base
}

/** filterRows applies every filter, or every filter but `skip` (for the counts
 *  next to that filter's own options). */
export function filterRows(rows: ListRow[], f: ListFilters, skip?: Facet): ListRow[] {
  const needle = f.q.trim().toLowerCase()
  const checksOnly = uptimeOnly(f)
  return rows.filter((r) => {
    if (needle && !textOf(r).some((v) => v?.toLowerCase().includes(needle))) return false
    if (skip !== 'kind' && !kindMatches(f.kinds, r)) return false
    if (skip !== 'status' && f.statuses.length && !f.statuses.includes(r.status)) return false
    if (skip !== 'site' && f.sites.length && !f.sites.includes(r.siteId ?? 'none')) return false
    if (checksOnly && r.kind !== 'uptime') {
      // A group or tag filter leaves servers and devices out, unless it is
      // the facet being counted and the other one is not set.
      const otherSet = skip === 'group' ? f.tags.length > 0 : skip === 'tags' ? f.group !== null : true
      if (otherSet) return false
    }
    if (skip !== 'group' && f.group !== null && r.kind === 'uptime') {
      if (f.group === 'ungrouped' ? r.groupId !== null : r.groupId !== f.group) return false
    }
    if (skip !== 'tags' && f.tags.length && r.kind === 'uptime' && !r.tags.some((t) => f.tags.includes(t))) return false
    return true
  })
}

export interface FacetCounts {
  kind: Record<string, number>
  status: Record<string, number>
  site: Record<string, number>
  group: Record<string, number>
  tags: Record<string, number>
}

const bump = (m: Record<string, number>, k: string) => {
  m[k] = (m[k] ?? 0) + 1
}

/** facetCounts is how many rows each filter option would show, counted over
 *  every other filter, so the next click's result is predictable. */
export function facetCounts(rows: ListRow[], f: ListFilters): FacetCounts {
  const out: FacetCounts = { kind: {}, status: {}, site: {}, group: {}, tags: {} }
  for (const k of KIND_VALUES) out.kind[k] = 0
  for (const s of VIEW_STATUSES) out.status[s] = 0
  for (const r of filterRows(rows, f, 'kind')) {
    bump(out.kind, r.kind)
    if (r.leaf !== null && leafParent(r.leaf) === r.kind) bump(out.kind, r.leaf)
  }
  for (const r of filterRows(rows, f, 'status')) bump(out.status, r.status)
  for (const r of filterRows(rows, f, 'site')) bump(out.site, r.siteId ?? 'none')
  for (const r of filterRows(rows, f, 'group')) if (r.kind === 'uptime') bump(out.group, r.groupId ?? 'ungrouped')
  for (const r of filterRows(rows, f, 'tags')) for (const t of r.tags) bump(out.tags, t)
  return out
}

// Needs attention first; paused sinks below healthy - dormant is not urgent.
const URGENCY: Record<ViewStatus, number> = { down: 0, error: 1, maintenance: 2, pending: 3, up: 4, paused: 5 }

/** What Health and Uptime sort by that the rows do not carry: each check's
 *  24-hour uptime and each server's latest metrics, by agent id. */
export interface SortContext {
  checkUptime: Map<string, number>
  serverLatest: Map<string, Pick<AgentMetric, 'cpu_percent' | 'memory_percent' | 'disk_percent'> | null>
}
const NO_CONTEXT: SortContext = { checkUptime: new Map(), serverLatest: new Map() }

/** rowAddress is the host a row is reached at: a check's host (from its URL
 *  or bare address), a server's IP or hostname, a device's host. */
export function rowAddress(r: ListRow): string {
  if (r.kind === 'servers') return r.agent.ip_address || r.agent.hostname || r.agent.agent_id
  if (r.kind === 'devices') return r.device.host
  const rest = r.monitor.url.trim().replace(/^[a-z][a-z0-9+.-]*:\/\//i, '').replace(/^[^@/]*@/, '')
  return rest.split(/[/?#]/)[0].replace(/:\d+$/, '').toLowerCase()
}

/** Sorts IPv4 addresses numerically (10.0.0.9 before 10.0.0.10), names after. */
function addressKey(host: string): string {
  const m = host.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/)
  return m ? m.slice(1).map((n) => n.padStart(3, '0')).join('.') : `~${host.toLowerCase()}`
}

const plainCompare = (x: string, y: string) => (x < y ? -1 : x > y ? 1 : 0)

const KIND_ORDER: Record<ListRow['kind'], number> = { uptime: 0, servers: 1, devices: 2 }

/** badness is the Health column as a number, higher being worse, compared
 *  only within a kind: a check's response time, a server's busiest of CPU,
 *  memory and disk, a device's shortness of uptime (failing worst of all).
 *  Null when there is nothing to go on. */
function badness(r: ListRow, ctx: SortContext): number | null {
  if (r.kind === 'uptime') return r.status === 'paused' || r.monitor.last_response_time_ms <= 0 ? null : r.monitor.last_response_time_ms
  if (r.kind === 'servers') {
    const m = ctx.serverLatest.get(r.agent.agent_id)
    const vals = [m?.cpu_percent, m?.memory_percent, m?.disk_percent].filter((v): v is number => v != null)
    return vals.length ? Math.max(...vals) : null
  }
  if (r.status === 'down' || r.status === 'error') return Number.POSITIVE_INFINITY
  return r.device.sys_uptime_seconds == null ? null : -r.device.sys_uptime_seconds
}

/** uptimePct is the Uptime column as a percentage: a check's 24 hours, a
 *  device's 30 days. Servers show running time, not a percentage. */
function uptimePct(r: ListRow, ctx: SortContext): number | null {
  if (r.kind === 'uptime') return ctx.checkUptime.get(r.monitor.id) ?? null
  if (r.kind === 'devices') return r.device.availability_30d ?? null
  return null
}

/** Compares two possibly-missing numbers, missing last whichever way. */
function byValue(x: number | null, y: number | null, dir: number): number | null {
  if ((x === null) !== (y === null)) return x === null ? 1 : -1
  if (x === null || y === null || x === y) return null
  return dir * (x < y ? -1 : 1)
}

const byName = (a: ListRow, b: ListRow) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })

/** sortRows returns a sorted copy. Rows with no site, never seen or with
 *  nothing to measure stay last whichever way the sort runs; ties go by name.
 *  Health keeps each kind together (checks, servers, devices), worst first;
 *  Uptime starts with the lowest percentage. */
export function sortRows(rows: ListRow[], s: ListSort, ctx: SortContext = NO_CONTEXT): ListRow[] {
  const dir = s.desc ? -1 : 1
  return [...rows].sort((a, b) => {
    switch (s.key) {
      case 'name':
        return dir * byName(a, b) || a.key.localeCompare(b.key)
      case 'address':
        // Plain comparison: localeCompare would sort the "~" before digits.
        return dir * plainCompare(addressKey(rowAddress(a)), addressKey(rowAddress(b))) || byName(a, b)
      case 'kind': {
        // Within a kind, devices go by vendor and model.
        const model = (r: ListRow) => (r.kind === 'devices' ? vendorModel(r.device).toLowerCase() : '')
        return dir * (kindLabel(a).localeCompare(kindLabel(b)) || model(a).localeCompare(model(b))) || byName(a, b)
      }
      case 'health':
        return KIND_ORDER[a.kind] - KIND_ORDER[b.kind] || (byValue(badness(a, ctx), badness(b, ctx), -dir) ?? byName(a, b))
      case 'uptime':
        return byValue(uptimePct(a, ctx), uptimePct(b, ctx), dir) ?? byName(a, b)
      case 'site': {
        if (!a.siteName !== !b.siteName) return a.siteName ? -1 : 1
        return dir * (a.siteName ?? '').localeCompare(b.siteName ?? '') || byName(a, b)
      }
      case 'seen': {
        if (!a.seenAt !== !b.seenAt) return a.seenAt ? -1 : 1
        const x = a.seenAt ? new Date(a.seenAt).getTime() : 0
        const y = b.seenAt ? new Date(b.seenAt).getTime() : 0
        return dir * (x - y) || byName(a, b)
      }
      default:
        return dir * (URGENCY[a.status] - URGENCY[b.status]) || byName(a, b)
    }
  })
}

/** nextSort is a click on a column heading: the same column reverses, a new
 *  one starts A-Z (Last seen starts newest first). */
export function nextSort(cur: ListSort, key: SortKey): ListSort {
  if (cur.key === key) return { key, desc: !cur.desc }
  return { key, desc: key === 'seen' }
}

export interface RowGroup {
  key: string
  label: string
  rows: ListRow[]
  down: number
  /** Set for a monitor group's heading (its uptime and Edit button). */
  monitorGroup: MonitorGroup | null
}

/** groupRows splits already-sorted rows into headings, keeping their order. */
export function groupRows(
  rows: ListRow[],
  by: Exclude<GroupBy, 'none'>,
  ctx: { groups: MonitorGroup[]; includeEmptyGroups: boolean }
): RowGroup[] {
  const buckets = new Map<string, RowGroup>()
  const add = (key: string, label: string, monitorGroup: MonitorGroup | null = null) => {
    let g = buckets.get(key)
    if (!g) {
      g = { key, label, rows: [], down: 0, monitorGroup }
      buckets.set(key, g)
    }
    return g
  }
  const knownGroups = new Map(ctx.groups.map((g) => [g.id, g]))
  if (by === 'group') for (const g of ctx.groups) add(g.id, g.name, g)
  if (by === 'kind') for (const k of PARENTS) add(k, KIND_PARENT_LABEL[k])
  for (const r of rows) {
    let g: RowGroup
    if (by === 'site') g = r.siteId ? add(r.siteId, r.siteName ?? 'Unknown site') : add('none', 'No site')
    else if (by === 'kind') g = add(r.kind, KIND_PARENT_LABEL[r.kind])
    else if (r.kind !== 'uptime') g = add('other', 'Other (servers and devices)')
    else if (r.groupId && knownGroups.has(r.groupId)) g = add(r.groupId, knownGroups.get(r.groupId)!.name)
    else g = add('ungrouped', 'Ungrouped')
    g.rows.push(r)
    if (r.status === 'down') g.down++
  }
  let out = [...buckets.values()]
  if (by === 'site') {
    out.sort((a, b) => (a.key === 'none' ? 1 : b.key === 'none' ? -1 : a.label.localeCompare(b.label)))
  } else if (by === 'group') {
    const tail = (k: string) => (k === 'other' ? 2 : k === 'ungrouped' ? 1 : 0)
    out = [...out.filter((g) => tail(g.key) === 0), ...out.filter((g) => tail(g.key) === 1), ...out.filter((g) => tail(g.key) === 2)]
  }
  return out.filter((g) => g.rows.length > 0 || (by === 'group' && g.monitorGroup !== null && ctx.includeEmptyGroups))
}

export interface FilterPill {
  id: string
  label: string
  remove: (f: ListFilters) => ListFilters
}

function pillKindLabel(v: KindValue): string {
  if (isCheckLeaf(v)) return `${CHECK_LABEL[v]} checks`
  return kindValueLabel(v)
}

/** filterPills is one removable pill per filter value, in the bar's order.
 *  A site or group no longer known reads as "Unknown". */
export function filterPills(
  f: ListFilters,
  names: { site: (id: string) => string | null; group: (id: string) => string | null }
): FilterPill[] {
  const out: FilterPill[] = []
  const q = f.q.trim()
  if (q) out.push({ id: 'q', label: `Search: “${q}”`, remove: (x) => ({ ...x, q: '' }) })
  for (const k of f.kinds) {
    out.push({ id: 'kind:' + k, label: 'Kind: ' + pillKindLabel(k), remove: (x) => ({ ...x, kinds: x.kinds.filter((v) => v !== k) }) })
  }
  for (const s of f.statuses) {
    out.push({ id: 'status:' + s, label: 'Status: ' + VIEW_STATUS_LABEL[s], remove: (x) => ({ ...x, statuses: x.statuses.filter((v) => v !== s) }) })
  }
  for (const s of f.sites) {
    const label = s === 'none' ? 'No site' : (names.site(s) ?? 'Unknown site')
    out.push({ id: 'site:' + s, label: 'Site: ' + label, remove: (x) => ({ ...x, sites: x.sites.filter((v) => v !== s) }) })
  }
  if (f.group !== null) {
    const g = f.group
    const label = g === 'ungrouped' ? 'Ungrouped' : (names.group(g) ?? 'Unknown group')
    out.push({ id: 'group', label: 'Group: ' + label, remove: (x) => ({ ...x, group: null }) })
  }
  for (const t of f.tags) {
    out.push({ id: 'tag:' + t, label: 'Tag: ' + t, remove: (x) => ({ ...x, tags: x.tags.filter((v) => v !== t) }) })
  }
  return out
}

/** siteFilterOptions is the Site menu: the user's sites plus any other site
 *  named on a row they can see, A-Z. */
export function siteFilterOptions(sites: { id: string; name: string }[], rows: ListRow[]): { id: string; name: string }[] {
  const byId = new Map(sites.map((s) => [s.id, s.name]))
  for (const r of rows) {
    if (r.siteId && !byId.has(r.siteId)) byId.set(r.siteId, r.siteName ?? 'Unknown site')
  }
  return [...byId].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))
}

/** legacyMonitoringTarget is where an old list address (/uptime, /servers,
 *  /network/devices) goes: Monitoring on that kind, the old query's filters
 *  carried over in the new form (so /uptime?type=http opens on HTTP checks),
 *  and the hash kept. */
export function legacyMonitoringTarget(location: { search: string; hash: string }, show: KindParent): string {
  const old = new URLSearchParams(location.search)
  if (!old.has('kind')) old.set('show', show)
  const qs = buildListParams(parseListParams(old)).toString()
  return `/monitoring${qs ? '?' + qs : ''}${location.hash}`
}
